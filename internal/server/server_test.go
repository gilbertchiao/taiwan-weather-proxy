package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"taiwan-weather-proxy/internal/config"
	"taiwan-weather-proxy/internal/model"
	"taiwan-weather-proxy/internal/timeutil"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig() *config.Config {
	return &config.Config{
		TargetLocations:           []string{"三重區"},
		ForecastHours:             12,
		ObservationStaleThreshold: 30 * time.Minute,
	}
}

func fptr(f float64) *float64 { return &f }
func iptr(i int) *int         { return &i }

// --- 測試替身 ---

type fakeStore struct {
	obs     *model.ObservationRecord
	obsErr  error
	fcs     []model.ForecastRecord
	fcErr   error
	pingErr error
}

func (s *fakeStore) LatestObservation(location string) (*model.ObservationRecord, error) {
	return s.obs, s.obsErr
}
func (s *fakeStore) LatestForecasts(location, fromStd string, limit int) ([]model.ForecastRecord, error) {
	return s.fcs, s.fcErr
}
func (s *fakeStore) Ping() error { return s.pingErr }

type fakeUpdater struct{}

func (fakeUpdater) RunObservationUpdate(ctx context.Context) error { return nil }
func (fakeUpdater) RunForecastUpdate(ctx context.Context) error    { return nil }

func newTestServer(s store) http.Handler {
	return New(s, fakeUpdater{}, testConfig(), testLogger()).Handler()
}

// --- 即時天氣 ---

func TestHandleCurrentSuccess(t *testing.T) {
	store := &fakeStore{
		obs: &model.ObservationRecord{
			Location: "三重區", StationName: "三重",
			Temperature: fptr(25.4), Humidity: fptr(78), Rainfall: fptr(0),
			// 以台灣時間建構觀測時間,與 handler 的台灣時區解讀一致,
			// 使測試不受執行環境系統時區影響 (CI 為 UTC)。
			Weather: "晴", ObsTime: timeutil.Now().Format("2006-01-02 15:04:05"),
			FetchedAt: "2026-06-21T11:05:00+08:00",
		},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/weather/current", nil)
	newTestServer(store).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("狀態碼 = %d, 預期 200", rr.Code)
	}

	var resp struct {
		Status    string            `json:"status"`
		UpdatedAt string            `json:"updated_at"`
		Data      model.CurrentData `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析回應失敗: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, 預期 success", resp.Status)
	}
	if resp.Data.Location != "三重區" || resp.Data.Temperature == nil || *resp.Data.Temperature != 25.4 {
		t.Errorf("data 內容不符: %+v", resp.Data)
	}
	if resp.Data.IsStale {
		t.Error("觀測時間為現在,不應被標記為過期")
	}
	if resp.UpdatedAt == "" {
		t.Error("updated_at 不應為空")
	}
}

func TestHandleCurrentStale(t *testing.T) {
	store := &fakeStore{
		obs: &model.ObservationRecord{
			Location: "三重區", Temperature: fptr(25),
			ObsTime: "2020-01-01 00:00:00", // 久遠過去
		},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/weather/current", nil)
	newTestServer(store).ServeHTTP(rr, req)

	var resp struct {
		Data model.CurrentData `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if !resp.Data.IsStale {
		t.Error("久遠的觀測時間應被標記為 is_stale=true")
	}
}

func TestHandleCurrentNotFound(t *testing.T) {
	store := &fakeStore{obs: nil} // 查無資料
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/weather/current?location=不存在區", nil)
	newTestServer(store).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("狀態碼 = %d, 預期 404", rr.Code)
	}
	var resp model.APIResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Status != "error" {
		t.Errorf("status = %q, 預期 error", resp.Status)
	}
}

// --- 即時天氣附帶的目前時段預報 (forecast_now) ---

// currentObs 回傳一筆觀測時間為現在的觀測資料,供 forecast_now 測試共用。
func currentObs() *model.ObservationRecord {
	return &model.ObservationRecord{
		Location: "三重區", Temperature: fptr(35.4), Humidity: fptr(46), Rainfall: fptr(0),
		Weather: "晴", ObsTime: timeutil.Now().Format(timeutil.StdLayout),
		FetchedAt: "2026-06-21T12:05:00+08:00",
	}
}

// currentBlockStd 回傳目前 3 小時預報時段起點的標準格式字串。
func currentBlockStd() string {
	return timeutil.ForecastBlockStart(time.Now()).Format(timeutil.StdLayout)
}

// getCurrentRaw 呼叫 /api/weather/current 並以 map 解析 data,
// 以便區分「欄位為 null」與「欄位不存在」。
func getCurrentRaw(t *testing.T, s store) (int, map[string]json.RawMessage) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/weather/current", nil)
	newTestServer(s).ServeHTTP(rr, req)

	var resp struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析回應失敗: %v", err)
	}
	return rr.Code, resp.Data
}

func TestHandleCurrentForecastNow(t *testing.T) {
	block := currentBlockStd()
	store := &fakeStore{
		obs: currentObs(),
		fcs: []model.ForecastRecord{
			{Location: "三重區", StartTime: block, Weather: "多雲時晴", WeatherCode: "03", PoP: iptr(20), Temperature: iptr(34)},
		},
	}
	code, data := getCurrentRaw(t, store)
	if code != http.StatusOK {
		t.Fatalf("狀態碼 = %d, 預期 200", code)
	}

	// 以原始 JSON 比對,確認 weather_code 為保留前導零的字串、pop 為整數。
	want := `{"time":"` + block + `","weather":"多雲時晴","weather_code":"03","pop":20}`
	if got := string(data["forecast_now"]); got != want {
		t.Errorf("forecast_now = %s, 預期 %s", got, want)
	}
	// 既有觀測欄位不受影響 (weather 仍為測站觀測文字)。
	if got := string(data["weather"]); got != `"晴"` {
		t.Errorf("weather = %s, 預期觀測值 \"晴\"", got)
	}
}

// TestHandleCurrentForecastNowNullFields CWA 未提供代碼或降雨機率時,個別欄位應為 null。
func TestHandleCurrentForecastNowNullFields(t *testing.T) {
	block := currentBlockStd()
	store := &fakeStore{
		obs: currentObs(),
		fcs: []model.ForecastRecord{{Location: "三重區", StartTime: block, Weather: "晴"}},
	}
	_, data := getCurrentRaw(t, store)

	want := `{"time":"` + block + `","weather":"晴","weather_code":null,"pop":null}`
	if got := string(data["forecast_now"]); got != want {
		t.Errorf("forecast_now = %s, 預期 %s", got, want)
	}
}

// TestHandleCurrentForecastNowNull 查無目前時段預報時,forecast_now 應為 null 且觀測照常回 200。
func TestHandleCurrentForecastNowNull(t *testing.T) {
	futureBlock := timeutil.ForecastBlockStart(time.Now()).
		Add(timeutil.ForecastBlockHours * time.Hour).Format(timeutil.StdLayout)

	cases := map[string]*fakeStore{
		"無任何預報":  {obs: currentObs()},
		"預報查詢失敗": {obs: currentObs(), fcErr: errors.New("db 故障")},
		// 目前時段缺漏時,儲存層回傳的第一筆會是未來時段,不可冒充為目前。
		"僅有未來時段": {obs: currentObs(), fcs: []model.ForecastRecord{
			{Location: "三重區", StartTime: futureBlock, Weather: "雨", WeatherCode: "08", PoP: iptr(80)},
		}},
	}
	for name, store := range cases {
		t.Run(name, func(t *testing.T) {
			code, data := getCurrentRaw(t, store)
			if code != http.StatusOK {
				t.Fatalf("狀態碼 = %d, 預期 200 (預報缺漏不可影響觀測回應)", code)
			}
			raw, ok := data["forecast_now"]
			if !ok {
				t.Fatal("forecast_now 欄位應存在 (值為 null),不可省略")
			}
			if string(raw) != "null" {
				t.Errorf("forecast_now = %s, 預期 null", raw)
			}
			if string(data["temperature"]) != "35.4" {
				t.Errorf("temperature = %s, 觀測資料應照常回傳", data["temperature"])
			}
		})
	}
}

// --- 未來預報 ---

func TestHandleForecastSuccess(t *testing.T) {
	store := &fakeStore{
		fcs: []model.ForecastRecord{
			{Location: "三重區", StartTime: "2026-06-21 12:00:00", Weather: "多雲", WeatherCode: "04", PoP: iptr(30), Temperature: iptr(26), FetchedAt: "2026-06-21T11:00:00+08:00"},
			{Location: "三重區", StartTime: "2026-06-21 15:00:00", Weather: "晴", PoP: iptr(10), Temperature: iptr(28), FetchedAt: "2026-06-21T11:00:00+08:00"},
		},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/weather/forecast", nil)
	newTestServer(store).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("狀態碼 = %d, 預期 200", rr.Code)
	}
	var resp struct {
		Status string             `json:"status"`
		Data   model.ForecastData `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析回應失敗: %v", err)
	}
	if resp.Status != "success" || len(resp.Data.Forecasts) != 2 {
		t.Fatalf("回應不符: %+v", resp)
	}
	if resp.Data.Forecasts[0].Time != "2026-06-21 12:00:00" || resp.Data.Forecasts[0].PoP == nil || *resp.Data.Forecasts[0].PoP != 30 {
		t.Errorf("第一筆預報不符: %+v", resp.Data.Forecasts[0])
	}
	if code := resp.Data.Forecasts[0].WeatherCode; code == nil || *code != "04" {
		t.Errorf("第一筆 weather_code = %v, 預期 \"04\"", code)
	}
	// 第二筆未提供代碼,應輸出 null。
	if code := resp.Data.Forecasts[1].WeatherCode; code != nil {
		t.Errorf("第二筆 weather_code = %q, 預期 null", *code)
	}
}

func TestHandleForecastNotFound(t *testing.T) {
	store := &fakeStore{fcs: nil}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/weather/forecast", nil)
	newTestServer(store).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("狀態碼 = %d, 預期 404", rr.Code)
	}
}

// --- 健康檢查 ---

func TestHandleHealth(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	newTestServer(&fakeStore{}).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("健康檢查狀態碼 = %d, 預期 200", rr.Code)
	}
}

// --- CORS ---

func TestCORSHeaders(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/weather/current", nil)
	newTestServer(&fakeStore{obs: &model.ObservationRecord{Location: "三重區", ObsTime: "2026-06-21 11:00:00"}}).ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, 預期 *", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/weather/current", nil)
	newTestServer(&fakeStore{}).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("OPTIONS 預檢狀態碼 = %d, 預期 204", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("預檢回應應包含 Access-Control-Allow-Methods")
	}
}
