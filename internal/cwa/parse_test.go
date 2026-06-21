package cwa

import (
	"os"
	"path/filepath"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("讀取測試樣本 %s 失敗: %v", name, err)
	}
	return data
}

// TestParseObservationsSingle 驗證單一測站觀測資料的扁平化結果。
func TestParseObservationsSingle(t *testing.T) {
	records, err := ParseObservations(readFixture(t, "observation_sample.json"))
	if err != nil {
		t.Fatalf("ParseObservations 失敗: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("預期 1 筆記錄,實際 %d 筆", len(records))
	}

	r := records[0]
	if r.Location != "三重區" {
		t.Errorf("Location = %q, 預期 三重區", r.Location)
	}
	if r.StationName != "三重" {
		t.Errorf("StationName = %q, 預期 三重", r.StationName)
	}
	if r.StationID != "C0AI30" {
		t.Errorf("StationID = %q, 預期 C0AI30", r.StationID)
	}
	if r.Temperature == nil || *r.Temperature != 33.5 {
		t.Errorf("Temperature = %v, 預期 33.5", r.Temperature)
	}
	if r.Humidity == nil || *r.Humidity != 54 {
		t.Errorf("Humidity = %v, 預期 54", r.Humidity)
	}
	if r.Rainfall == nil || *r.Rainfall != 0.0 {
		t.Errorf("Rainfall = %v, 預期 0.0", r.Rainfall)
	}
	if r.Weather != "晴" {
		t.Errorf("Weather = %q, 預期 晴", r.Weather)
	}
	if r.ObsTime != "2026-06-21 11:00:00" {
		t.Errorf("ObsTime = %q, 預期 2026-06-21 11:00:00 (標準化)", r.ObsTime)
	}
}

// TestParseObservationsMulti 驗證同一行政區有多站時,皆能解析且標註正確行政區。
func TestParseObservationsMulti(t *testing.T) {
	records, err := ParseObservations(readFixture(t, "observation_sanchong_multi.json"))
	if err != nil {
		t.Fatalf("ParseObservations 失敗: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("預期 2 筆記錄,實際 %d 筆", len(records))
	}
	names := map[string]bool{}
	for _, r := range records {
		if r.Location != "三重區" {
			t.Errorf("站 %s 的 Location = %q, 預期 三重區", r.StationName, r.Location)
		}
		names[r.StationName] = true
	}
	for _, want := range []string{"三重", "國一S026K"} {
		if !names[want] {
			t.Errorf("缺少預期測站 %s", want)
		}
	}
}

// TestParseForecast 驗證預報資料的巢狀解析與每 3 小時時段對齊。
func TestParseForecast(t *testing.T) {
	result, err := ParseForecast(readFixture(t, "forecast_sample.json"))
	if err != nil {
		t.Fatalf("ParseForecast 失敗: %v", err)
	}

	records, ok := result["三重區"]
	if !ok {
		t.Fatalf("結果中找不到 三重區,實際鍵: %v", keysOf(result))
	}
	if len(records) == 0 {
		t.Fatal("三重區 預報時段為空")
	}

	first := records[0]
	if first.Location != "三重區" {
		t.Errorf("Location = %q, 預期 三重區", first.Location)
	}
	if first.StartTime != "2026-06-21 12:00:00" {
		t.Errorf("第一筆 StartTime = %q, 預期 2026-06-21 12:00:00", first.StartTime)
	}
	if first.Weather != "晴" {
		t.Errorf("第一筆 Weather = %q, 預期 晴", first.Weather)
	}
	if first.PoP == nil || *first.PoP != 0 {
		t.Errorf("第一筆 PoP = %v, 預期 0", first.PoP)
	}
	if first.Temperature == nil || *first.Temperature != 35 {
		t.Errorf("第一筆 Temperature = %v, 預期 35", first.Temperature)
	}

	// 每個時段都應有天氣現象與時間,且溫度/降雨機率多數有值。
	withTemp := 0
	for i, r := range records {
		if r.StartTime == "" {
			t.Errorf("第 %d 筆缺少 StartTime", i)
		}
		if r.Weather == "" {
			t.Errorf("第 %d 筆缺少 Weather", i)
		}
		if r.Temperature != nil {
			withTemp++
		}
	}
	if withTemp == 0 {
		t.Error("所有時段皆無溫度,溫度對齊邏輯可能有誤")
	}
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestValueHelpers 驗證數值解析對無效哨兵值與空值的處理。
func TestValueHelpers(t *testing.T) {
	// parseFloat
	if v := parseFloat("33.5"); v == nil || *v != 33.5 {
		t.Errorf("parseFloat(33.5) = %v", v)
	}
	if v := parseFloat("-99"); v != nil {
		t.Errorf("parseFloat(-99) 應為 nil (無效哨兵),實際 %v", v)
	}
	if v := parseFloat(""); v != nil {
		t.Errorf("parseFloat(空) 應為 nil")
	}
	if v := parseFloat("0.0"); v == nil || *v != 0.0 {
		t.Errorf("parseFloat(0.0) 應為 0 而非 nil (區分無資料與 0)")
	}
	// parseIntPercent
	if v := parseIntPercent("30"); v == nil || *v != 30 {
		t.Errorf("parseIntPercent(30) = %v", v)
	}
	if v := parseIntPercent("-99"); v != nil {
		t.Errorf("parseIntPercent(-99) 應為 nil")
	}
	// parseTemperatureInt
	if v := parseTemperatureInt("26"); v == nil || *v != 26 {
		t.Errorf("parseTemperatureInt(26) = %v", v)
	}
	// cleanText
	if cleanText("-99") != "" {
		t.Errorf("cleanText(-99) 應為空字串")
	}
	if cleanText(" 晴 ") != "晴" {
		t.Errorf("cleanText 應去除前後空白")
	}
}
