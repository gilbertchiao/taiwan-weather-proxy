package storage

import (
	"path/filepath"
	"testing"

	"taiwan-weather-proxy/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("建立測試資料庫失敗: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func fptr(f float64) *float64 { return &f }
func iptr(i int) *int         { return &i }

// TestObservationRoundtrip 驗證觀測資料寫入後可正確讀回。
func TestObservationRoundtrip(t *testing.T) {
	store := newTestStore(t)

	rec := model.ObservationRecord{
		Location: "三重區", StationName: "三重", StationID: "C0AI30",
		Temperature: fptr(33.5), Humidity: fptr(54), Rainfall: fptr(0),
		Weather: "晴", ObsTime: "2026-06-21 11:00:00",
	}
	if err := store.SaveObservation(rec); err != nil {
		t.Fatalf("SaveObservation 失敗: %v", err)
	}

	got, err := store.LatestObservation("三重區")
	if err != nil {
		t.Fatalf("LatestObservation 失敗: %v", err)
	}
	if got == nil {
		t.Fatal("預期讀回資料,實際為 nil")
	}
	if got.StationName != "三重" || got.Temperature == nil || *got.Temperature != 33.5 {
		t.Errorf("讀回資料不符: %+v", got)
	}
	if got.FetchedAt == "" {
		t.Error("FetchedAt 應由儲存層填入")
	}
}

// TestObservationUpsertAndLatest 驗證 UPSERT 不重複、且永遠回傳最新整點。
func TestObservationUpsertAndLatest(t *testing.T) {
	store := newTestStore(t)

	base := model.ObservationRecord{Location: "三重區", StationName: "三重", ObsTime: "2026-06-21 11:00:00", Temperature: fptr(30)}
	// 同一 (location, obs_time) 重複寫入應更新而非新增。
	_ = store.SaveObservation(base)
	updated := base
	updated.Temperature = fptr(31)
	_ = store.SaveObservation(updated)

	// 另一個較新的整點。
	newer := model.ObservationRecord{Location: "三重區", StationName: "三重", ObsTime: "2026-06-21 12:00:00", Temperature: fptr(32)}
	_ = store.SaveObservation(newer)

	got, err := store.LatestObservation("三重區")
	if err != nil {
		t.Fatalf("LatestObservation 失敗: %v", err)
	}
	if got.ObsTime != "2026-06-21 12:00:00" || got.Temperature == nil || *got.Temperature != 32 {
		t.Errorf("應回傳最新整點 12:00 / 32 度,實際: %+v", got)
	}

	// 確認 11:00 那筆只有一列 (UPSERT 未產生重複),且值已更新為 31。
	var count int
	if err := store.db.QueryRow(
		`SELECT COUNT(*) FROM observations WHERE location=? AND obs_time=?`,
		"三重區", "2026-06-21 11:00:00").Scan(&count); err != nil {
		t.Fatalf("計數查詢失敗: %v", err)
	}
	if count != 1 {
		t.Errorf("11:00 應只有 1 列 (UPSERT),實際 %d 列", count)
	}
}

// TestLatestObservationNotFound 查無資料時應回傳 (nil, nil)。
func TestLatestObservationNotFound(t *testing.T) {
	store := newTestStore(t)
	got, err := store.LatestObservation("不存在區")
	if err != nil {
		t.Fatalf("不應回傳錯誤: %v", err)
	}
	if got != nil {
		t.Errorf("查無資料應回傳 nil,實際: %+v", got)
	}
}

// TestForecastsRoundtrip 驗證預報批次寫入、時間過濾與筆數限制。
func TestForecastsRoundtrip(t *testing.T) {
	store := newTestStore(t)

	records := []model.ForecastRecord{
		{Location: "三重區", StartTime: "2026-06-21 09:00:00", Weather: "晴", PoP: iptr(0), Temperature: iptr(30)},
		{Location: "三重區", StartTime: "2026-06-21 12:00:00", Weather: "多雲", PoP: iptr(20), Temperature: iptr(33)},
		{Location: "三重區", StartTime: "2026-06-21 15:00:00", Weather: "陰", PoP: iptr(40), Temperature: iptr(31)},
		{Location: "三重區", StartTime: "2026-06-21 18:00:00", Weather: "雨", PoP: iptr(70), Temperature: iptr(28)},
	}
	if err := store.SaveForecasts(records); err != nil {
		t.Fatalf("SaveForecasts 失敗: %v", err)
	}

	// 從 12:00 起、最多 2 筆。
	got, err := store.LatestForecasts("三重區", "2026-06-21 12:00:00", 2)
	if err != nil {
		t.Fatalf("LatestForecasts 失敗: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("預期 2 筆,實際 %d 筆", len(got))
	}
	if got[0].StartTime != "2026-06-21 12:00:00" || got[1].StartTime != "2026-06-21 15:00:00" {
		t.Errorf("時間過濾或排序錯誤: %v, %v", got[0].StartTime, got[1].StartTime)
	}
	if got[0].PoP == nil || *got[0].PoP != 20 {
		t.Errorf("PoP 讀回錯誤: %v", got[0].PoP)
	}

	// 重複寫入 (含更新值) 應 UPSERT 而非新增。
	records[1].Temperature = iptr(99)
	if err := store.SaveForecasts(records); err != nil {
		t.Fatalf("重複 SaveForecasts 失敗: %v", err)
	}
	all, _ := store.LatestForecasts("三重區", "2026-06-21 00:00:00", 0)
	if len(all) != 4 {
		t.Errorf("UPSERT 後應仍為 4 筆,實際 %d 筆", len(all))
	}
}
