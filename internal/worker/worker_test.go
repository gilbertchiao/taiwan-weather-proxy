package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"taiwan-weather-proxy/internal/config"
	"taiwan-weather-proxy/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig() *config.Config {
	return &config.Config{
		TargetLocations: []string{"三重區"},
		MaxRetries:      2,
		RetryDelay:      time.Millisecond, // 測試用極短間隔
	}
}

func fptr(f float64) *float64 { return &f }

// --- 測試替身 ---

type fakeFetcher struct {
	obs      []model.ObservationRecord
	forecast map[string][]model.ForecastRecord
	obsErr   error
	fcErr    error
	obsCalls int
	fcCalls  int
	// failTimes:前幾次呼叫回傳錯誤,之後成功 (用於測試重試)。
	failTimes int
}

func (f *fakeFetcher) FetchObservations(ctx context.Context) ([]model.ObservationRecord, error) {
	f.obsCalls++
	if f.obsCalls <= f.failTimes {
		return nil, errors.New("模擬上游失敗")
	}
	if f.obsErr != nil {
		return nil, f.obsErr
	}
	return f.obs, nil
}

func (f *fakeFetcher) FetchForecast(ctx context.Context, locations ...string) (map[string][]model.ForecastRecord, error) {
	f.fcCalls++
	if f.fcErr != nil {
		return nil, f.fcErr
	}
	return f.forecast, nil
}

type fakeStore struct {
	obs       []model.ObservationRecord
	forecasts [][]model.ForecastRecord
}

func (s *fakeStore) SaveObservation(rec model.ObservationRecord) error {
	s.obs = append(s.obs, rec)
	return nil
}

func (s *fakeStore) SaveForecasts(records []model.ForecastRecord) error {
	s.forecasts = append(s.forecasts, records)
	return nil
}

// --- 代表站挑選 ---

func TestSelectRepresentative(t *testing.T) {
	candidates := []model.ObservationRecord{
		{Location: "三重區", StationName: "國一S026K", StationID: "CAA020", Temperature: fptr(34.1), Humidity: fptr(55), Weather: "晴"},
		{Location: "三重區", StationName: "三重", StationID: "C0AI30", Temperature: fptr(33.5), Humidity: fptr(54), Weather: "晴"},
	}
	rep := selectRepresentative(candidates)
	if rep == nil || rep.StationName != "三重" {
		t.Fatalf("應挑選正規氣象站「三重」,實際: %+v", rep)
	}
}

func TestIsProperStationName(t *testing.T) {
	cases := map[string]bool{
		"三重": true, "板橋": true, "淡水": true,
		"國一S026K": false, "F0AH40": false, "": false,
	}
	for name, want := range cases {
		if got := isProperStationName(name); got != want {
			t.Errorf("isProperStationName(%q) = %v, 預期 %v", name, got, want)
		}
	}
}

// --- 觀測更新 ---

func TestObservationUpdatePicksRepresentative(t *testing.T) {
	fetcher := &fakeFetcher{
		obs: []model.ObservationRecord{
			{Location: "三重區", StationName: "國一S026K", StationID: "CAA020", Temperature: fptr(34.1), Humidity: fptr(55), Weather: "晴", ObsTime: "2026-06-21 11:00:00"},
			{Location: "三重區", StationName: "三重", StationID: "C0AI30", Temperature: fptr(33.5), Humidity: fptr(54), Weather: "晴", ObsTime: "2026-06-21 11:00:00"},
			{Location: "板橋區", StationName: "板橋", StationID: "C0AC80", Temperature: fptr(34), ObsTime: "2026-06-21 11:00:00"},
		},
	}
	store := &fakeStore{}
	w := New(fetcher, store, testConfig(), testLogger())

	if err := w.RunObservationUpdate(context.Background()); err != nil {
		t.Fatalf("RunObservationUpdate 失敗: %v", err)
	}
	if len(store.obs) != 1 {
		t.Fatalf("應只寫入 1 筆 (目標僅三重區),實際 %d 筆", len(store.obs))
	}
	if store.obs[0].StationName != "三重" {
		t.Errorf("應寫入代表站「三重」,實際: %s", store.obs[0].StationName)
	}
}

func TestObservationUpdateRetryThenSucceed(t *testing.T) {
	fetcher := &fakeFetcher{
		failTimes: 2, // 前兩次失敗,第三次成功
		obs: []model.ObservationRecord{
			{Location: "三重區", StationName: "三重", StationID: "C0AI30", Temperature: fptr(33.5), ObsTime: "2026-06-21 11:00:00"},
		},
	}
	store := &fakeStore{}
	w := New(fetcher, store, testConfig(), testLogger())

	if err := w.RunObservationUpdate(context.Background()); err != nil {
		t.Fatalf("重試後應成功,實際失敗: %v", err)
	}
	if fetcher.obsCalls != 3 {
		t.Errorf("應呼叫 3 次 (2 失敗 + 1 成功),實際 %d 次", fetcher.obsCalls)
	}
}

func TestObservationUpdateAllFail(t *testing.T) {
	fetcher := &fakeFetcher{obsErr: errors.New("持續失敗")}
	store := &fakeStore{}
	w := New(fetcher, store, testConfig(), testLogger())

	err := w.RunObservationUpdate(context.Background())
	if err == nil {
		t.Fatal("持續失敗時應回傳錯誤")
	}
	if len(store.obs) != 0 {
		t.Error("失敗時不應寫入任何資料 (保留舊快取)")
	}
	// MaxRetries=2 => 共 3 次嘗試。
	if fetcher.obsCalls != 3 {
		t.Errorf("應嘗試 3 次,實際 %d 次", fetcher.obsCalls)
	}
}

func TestObservationUpdateCancelled(t *testing.T) {
	fetcher := &fakeFetcher{obsErr: errors.New("失敗以觸發重試等待")}
	store := &fakeStore{}
	cfg := testConfig()
	cfg.RetryDelay = time.Hour // 確保卡在等待,讓 cancel 生效
	w := New(fetcher, store, cfg, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	err := w.RunObservationUpdate(ctx)
	if err == nil {
		t.Fatal("context 已取消時應回傳錯誤")
	}
}

// --- 預報更新 ---

func TestForecastUpdate(t *testing.T) {
	fetcher := &fakeFetcher{
		forecast: map[string][]model.ForecastRecord{
			"三重區": {
				{Location: "三重區", StartTime: "2026-06-21 12:00:00", Weather: "晴"},
				{Location: "三重區", StartTime: "2026-06-21 15:00:00", Weather: "多雲"},
			},
		},
	}
	store := &fakeStore{}
	w := New(fetcher, store, testConfig(), testLogger())

	if err := w.RunForecastUpdate(context.Background()); err != nil {
		t.Fatalf("RunForecastUpdate 失敗: %v", err)
	}
	if len(store.forecasts) != 1 || len(store.forecasts[0]) != 2 {
		t.Errorf("應寫入三重區 2 個時段,實際: %+v", store.forecasts)
	}
}

func TestForecastUpdateMissingLocation(t *testing.T) {
	fetcher := &fakeFetcher{forecast: map[string][]model.ForecastRecord{}} // 無目標行政區
	store := &fakeStore{}
	w := New(fetcher, store, testConfig(), testLogger())

	if err := w.RunForecastUpdate(context.Background()); err == nil {
		t.Fatal("找不到任何目標行政區時應回傳錯誤")
	}
}
