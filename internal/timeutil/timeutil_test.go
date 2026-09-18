package timeutil

import (
	"testing"
	"time"
)

// TestTaipeiLocationOffset 確認時區為 +08:00 (台灣不實施日光節約)。
func TestTaipeiLocationOffset(t *testing.T) {
	// 取一個固定時刻,確認其在台灣時區的 UTC 偏移為 +8 小時。
	ref := time.Date(2026, 6, 21, 12, 0, 0, 0, TaipeiLocation())
	_, offset := ref.Zone()
	if offset != 8*3600 {
		t.Errorf("台灣時區偏移 = %d 秒, 預期 28800 (+08:00)", offset)
	}
}

// TestNowIsTaipei 確認 Now() 回傳的時間綁定台灣時區,不受系統時區影響。
func TestNowIsTaipei(t *testing.T) {
	// 即使測試在 UTC 環境執行,Now() 的 Location 仍應為台灣。
	t.Setenv("TZ", "UTC")

	now := Now()
	_, offset := now.Zone()
	if offset != 8*3600 {
		t.Errorf("Now() 偏移 = %d 秒, 預期 28800 (+08:00);應綁定台灣而非系統時區", offset)
	}

	// Now() 與 time.Now() 應為同一時刻 (僅時區呈現不同),差距極小。
	if diff := time.Since(now); diff < -time.Second || diff > time.Second {
		t.Errorf("Now() 與系統現在時刻差距過大: %v", diff)
	}
}

// TestNormalizeToTaipei 確認帶時區的來源時間會換算為台灣時間後輸出。
func TestNormalizeToTaipei(t *testing.T) {
	cases := map[string]string{
		// CWA 原生格式 (已是 +08:00) → 維持台灣時間。
		"2026-06-21T11:00:00+08:00": "2026-06-21 11:00:00",
		// UTC 時間 → 應換算為台灣時間 (+8 小時)。
		"2026-06-21T03:00:00Z": "2026-06-21 11:00:00",
		// 空字串 → 空字串。
		"": "",
	}
	for input, want := range cases {
		if got := Normalize(input); got != want {
			t.Errorf("Normalize(%q) = %q, 預期 %q", input, got, want)
		}
	}
}

// TestParseStdRoundtrip 確認標準格式可被解析回台灣時區的時刻。
func TestParseStdRoundtrip(t *testing.T) {
	parsed, err := ParseStd("2026-06-21 12:00:00")
	if err != nil {
		t.Fatalf("ParseStd 失敗: %v", err)
	}
	if _, offset := parsed.Zone(); offset != 8*3600 {
		t.Errorf("ParseStd 結果時區偏移 = %d, 預期 28800", offset)
	}
}

// TestForecastBlockStart 驗證 3 小時預報時段起點的計算,且不受輸入時區影響。
func TestForecastBlockStart(t *testing.T) {
	loc := TaipeiLocation()
	cases := []struct {
		name string
		in   time.Time
		want string
	}{
		{"時段中間", time.Date(2026, 6, 21, 13, 47, 5, 0, loc), "2026-06-21 12:00:00"},
		{"恰為時段起點", time.Date(2026, 6, 21, 12, 0, 0, 0, loc), "2026-06-21 12:00:00"},
		{"時段最後一秒", time.Date(2026, 6, 21, 14, 59, 59, 0, loc), "2026-06-21 12:00:00"},
		{"午夜後", time.Date(2026, 6, 21, 1, 30, 0, 0, loc), "2026-06-21 00:00:00"},
		// UTC 20:30 = 台灣隔日 04:30,應落在台灣時間 03:00 的時段。
		{"UTC 輸入跨日", time.Date(2026, 6, 20, 20, 30, 0, 0, time.UTC), "2026-06-21 03:00:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ForecastBlockStart(c.in).Format(StdLayout); got != c.want {
				t.Errorf("ForecastBlockStart = %q, 預期 %q", got, c.want)
			}
		})
	}
}
