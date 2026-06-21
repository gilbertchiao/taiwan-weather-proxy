package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// clearEnv 清除測試會用到的環境變數,避免測試之間互相汙染。
func clearEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"CWA_API_KEY", "CWA_BASE_URL", "DATASET_OBSERVATION", "DATASET_FORECAST",
		"TARGET_LOCATION", "FORECAST_HOURS", "ENABLE_SCHEDULER",
		"FETCH_INTERVAL_OBSERVATION", "FETCH_INTERVAL_FORECAST",
		"MAX_RETRIES", "RETRY_DELAY_SECONDS", "HTTP_TIMEOUT_SECONDS",
		"STALE_THRESHOLD_OBSERVATION_MINUTES", "DATABASE_PATH",
		"LOG_LEVEL", "LOG_DIR", "API_HOST", "PORT", "REFRESH_TOKEN",
	}
	for _, k := range keys {
		_ = os.Unsetenv(k)
	}
}

// TestLoadDefaults 確認在沒有任何環境變數與 .env 時,所有預設值正確。
func TestLoadDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.env"))
	if err != nil {
		t.Fatalf("Load 不應失敗: %v", err)
	}

	if cfg.CWABaseURL != "https://opendata.cwa.gov.tw/api/v1/rest/datastore" {
		t.Errorf("CWABaseURL 預設值錯誤: %s", cfg.CWABaseURL)
	}
	if cfg.DatasetObservation != "O-A0001-001" {
		t.Errorf("DatasetObservation 預設值錯誤: %s", cfg.DatasetObservation)
	}
	if cfg.DatasetForecast != "F-D0047-069" {
		t.Errorf("DatasetForecast 預設值錯誤: %s", cfg.DatasetForecast)
	}
	if got := cfg.DefaultLocation(); got != "三重區" {
		t.Errorf("DefaultLocation 預設值錯誤: %s", got)
	}
	if cfg.ForecastHours != 12 {
		t.Errorf("ForecastHours 預設值錯誤: %d", cfg.ForecastHours)
	}
	if cfg.ObservationInterval != 600*time.Second {
		t.Errorf("ObservationInterval 預設值錯誤: %v", cfg.ObservationInterval)
	}
	if cfg.ForecastInterval != 3600*time.Second {
		t.Errorf("ForecastInterval 預設值錯誤: %v", cfg.ForecastInterval)
	}
	if cfg.APIPort != 8000 {
		t.Errorf("APIPort 預設值錯誤: %d", cfg.APIPort)
	}
	if !cfg.EnableScheduler {
		t.Errorf("EnableScheduler 預設應為 true")
	}
}

// TestLoadDotEnv 確認 .env 檔能被正確解析,且真實環境變數優先於 .env。
func TestLoadDotEnv(t *testing.T) {
	clearEnv(t)

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := `# 這是註解
CWA_API_KEY="my-secret-key"
TARGET_LOCATION = 板橋區, 三重區 ,蘆洲區
PORT=9000
FETCH_INTERVAL_OBSERVATION=300
ENABLE_SCHEDULER=false
`
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("寫入測試 .env 失敗: %v", err)
	}

	// 真實環境變數應覆寫 .env 的同名值。
	t.Setenv("PORT", "7777")

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatalf("Load 不應失敗: %v", err)
	}

	if cfg.CWAAPIKey != "my-secret-key" {
		t.Errorf("CWAAPIKey 解析錯誤 (引號未去除?): %q", cfg.CWAAPIKey)
	}
	wantLocations := []string{"板橋區", "三重區", "蘆洲區"}
	if len(cfg.TargetLocations) != len(wantLocations) {
		t.Fatalf("TargetLocations 數量錯誤: %v", cfg.TargetLocations)
	}
	for i, want := range wantLocations {
		if cfg.TargetLocations[i] != want {
			t.Errorf("TargetLocations[%d] = %q, 預期 %q", i, cfg.TargetLocations[i], want)
		}
	}
	if cfg.DefaultLocation() != "板橋區" {
		t.Errorf("DefaultLocation 應為第一個行政區,實際: %s", cfg.DefaultLocation())
	}
	if cfg.ObservationInterval != 300*time.Second {
		t.Errorf("ObservationInterval 解析錯誤: %v", cfg.ObservationInterval)
	}
	if cfg.EnableScheduler {
		t.Errorf("ENABLE_SCHEDULER=false 應使 EnableScheduler 為 false")
	}
	// 真實環境變數優先。
	if cfg.APIPort != 7777 {
		t.Errorf("PORT 應由真實環境變數覆寫為 7777,實際: %d", cfg.APIPort)
	}
}

// TestValidate 確認非法設定會被攔截。
func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"合法設定", func(c *Config) {}, false},
		{"空行政區", func(c *Config) { c.TargetLocations = nil }, true},
		{"觀測間隔為 0", func(c *Config) { c.ObservationInterval = 0 }, true},
		{"預報間隔為負", func(c *Config) { c.ForecastInterval = -1 }, true},
		{"預報時數為 0", func(c *Config) { c.ForecastHours = 0 }, true},
		{"重試次數為負", func(c *Config) { c.MaxRetries = -1 }, true},
		{"埠號超出範圍", func(c *Config) { c.APIPort = 70000 }, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				TargetLocations:     []string{"三重區"},
				ForecastHours:       12,
				ObservationInterval: 600 * time.Second,
				ForecastInterval:    3600 * time.Second,
				MaxRetries:          3,
				APIPort:             8000,
			}
			tc.mutate(cfg)
			err := cfg.validate()
			if tc.wantErr && err == nil {
				t.Errorf("預期驗證失敗,但通過了")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("預期驗證通過,但失敗了: %v", err)
			}
		})
	}
}
