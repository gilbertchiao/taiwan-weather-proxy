package server

import (
	"context"
	"encoding/json"
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

// --- 未來預報 ---

func TestHandleForecastSuccess(t *testing.T) {
	store := &fakeStore{
		fcs: []model.ForecastRecord{
			{Location: "三重區", StartTime: "2026-06-21 12:00:00", Weather: "多雲", PoP: iptr(30), Temperature: iptr(26), FetchedAt: "2026-06-21T11:00:00+08:00"},
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
