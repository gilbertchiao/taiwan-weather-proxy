// Package config 負責載入與管理應用程式組態。
//
// 設計原則 (12-factor):所有可調整的參數皆透過環境變數提供,
// 並支援從專案根目錄的 .env 檔載入預設值。
// 真實的環境變數 (os 環境) 優先權高於 .env 檔,方便容器化或 systemd
// 部署時以環境覆寫設定。
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 保存所有應用程式設定。
type Config struct {
	// === 中央氣象署開放資料平臺 (CWA) ===
	CWAAPIKey  string // 授權碼 (CWA_API_KEY),必要
	CWABaseURL string // datastore API 基底網址 (CWA_BASE_URL)

	DatasetObservation string // 即時觀測資料集代碼 (DATASET_OBSERVATION),預設 O-A0001-001
	DatasetForecast    string // 短效期預報資料集代碼 (DATASET_FORECAST),預設 F-D0047-069

	// === 目標行政區 ===
	// 以逗號分隔可同時關注多個行政區 (TARGET_LOCATION);
	// 第一個為 API 未帶 location 參數時的預設值。
	TargetLocations []string

	// ForecastHours 為 /api/weather/forecast 預設回傳的未來時數 (FORECAST_HOURS)。
	ForecastHours int

	// === 排程 (各來源獨立頻率) ===
	EnableScheduler     bool          // 是否啟用內建排程器 (ENABLE_SCHEDULER)
	ObservationInterval time.Duration // 觀測資料更新間隔 (FETCH_INTERVAL_OBSERVATION 秒)
	ForecastInterval    time.Duration // 預報資料更新間隔 (FETCH_INTERVAL_FORECAST 秒)

	// === 重試 / 逾時 ===
	MaxRetries  int           // 上游失敗最大重試次數 (MAX_RETRIES)
	RetryDelay  time.Duration // 重試間隔 (RETRY_DELAY_SECONDS)
	HTTPTimeout time.Duration // HTTP 逾時 (HTTP_TIMEOUT_SECONDS)

	// === 資料過期判斷 ===
	// 觀測資料超過此門檻即視為過期 (STALE_THRESHOLD_OBSERVATION_MINUTES)。
	ObservationStaleThreshold time.Duration

	// === 儲存 ===
	DatabasePath string // SQLite 檔案路徑 (DATABASE_PATH)

	// === 日誌 ===
	LogLevel string // 日誌等級 (LOG_LEVEL)
	LogDir   string // 日誌目錄 (LOG_DIR)

	// === API 服務 ===
	APIHost      string // 監聽位址 (API_HOST)
	APIPort      int    // 監聽埠號 (PORT)
	RefreshToken string // 手動觸發更新端點的權杖 (REFRESH_TOKEN),留空則停用
}

// Load 從 .env 檔 (若存在) 與環境變數載入設定。
//
// envPath 為 .env 檔路徑,傳入空字串時預設使用 ".env"。
// 回傳組裝完成的 Config;若必要欄位 (如授權碼) 缺漏只會由呼叫端記錄警告,
// 以利在「先啟動 API 提供舊資料、稍後再補授權碼」的情境運作。
func Load(envPath string) (*Config, error) {
	if envPath == "" {
		envPath = ".env"
	}
	if err := loadDotEnv(envPath); err != nil {
		return nil, fmt.Errorf("讀取 .env (%s) 失敗: %w", envPath, err)
	}

	cfg := &Config{
		CWAAPIKey:  getEnv("CWA_API_KEY", ""),
		CWABaseURL: getEnv("CWA_BASE_URL", "https://opendata.cwa.gov.tw/api/v1/rest/datastore"),

		DatasetObservation: getEnv("DATASET_OBSERVATION", "O-A0001-001"),
		DatasetForecast:    getEnv("DATASET_FORECAST", "F-D0047-069"),

		TargetLocations: parseList(getEnv("TARGET_LOCATION", "三重區")),
		ForecastHours:   getInt("FORECAST_HOURS", 12),

		EnableScheduler:     getBool("ENABLE_SCHEDULER", true),
		ObservationInterval: time.Duration(getInt("FETCH_INTERVAL_OBSERVATION", 600)) * time.Second,
		ForecastInterval:    time.Duration(getInt("FETCH_INTERVAL_FORECAST", 3600)) * time.Second,

		MaxRetries:  getInt("MAX_RETRIES", 3),
		RetryDelay:  time.Duration(getInt("RETRY_DELAY_SECONDS", 180)) * time.Second,
		HTTPTimeout: time.Duration(getInt("HTTP_TIMEOUT_SECONDS", 30)) * time.Second,

		ObservationStaleThreshold: time.Duration(getInt("STALE_THRESHOLD_OBSERVATION_MINUTES", 30)) * time.Minute,

		DatabasePath: getEnv("DATABASE_PATH", "data/weather.db"),

		LogLevel: getEnv("LOG_LEVEL", "INFO"),
		LogDir:   getEnv("LOG_DIR", "logs"),

		APIHost:      getEnv("API_HOST", "0.0.0.0"),
		APIPort:      getInt("PORT", 8000),
		RefreshToken: getEnv("REFRESH_TOKEN", ""),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// DefaultLocation 回傳 API 未指定 location 參數時採用的預設行政區。
func (c *Config) DefaultLocation() string {
	if len(c.TargetLocations) == 0 {
		return ""
	}
	return c.TargetLocations[0]
}

// validate 檢查設定值的合法性。
func (c *Config) validate() error {
	if len(c.TargetLocations) == 0 {
		return fmt.Errorf("TARGET_LOCATION 不可為空,請至少提供一個行政區")
	}
	if c.ObservationInterval <= 0 {
		return fmt.Errorf("FETCH_INTERVAL_OBSERVATION 必須大於 0 秒")
	}
	if c.ForecastInterval <= 0 {
		return fmt.Errorf("FETCH_INTERVAL_FORECAST 必須大於 0 秒")
	}
	if c.ForecastHours <= 0 {
		return fmt.Errorf("FORECAST_HOURS 必須大於 0")
	}
	if c.MaxRetries < 0 {
		return fmt.Errorf("MAX_RETRIES 不可為負數")
	}
	if c.APIPort < 1 || c.APIPort > 65535 {
		return fmt.Errorf("PORT 必須介於 1-65535,實際為 %d", c.APIPort)
	}
	return nil
}

// loadDotEnv 解析 .env 檔並將其中的鍵值設定到環境變數。
//
// 為維持輕量,此處自行實作極簡 .env 解析,不引入外部套件:
//   - 忽略空行與以 # 開頭的註解行。
//   - 以第一個 '=' 分隔鍵與值。
//   - 去除值前後空白與成對的引號。
//   - 僅在該環境變數尚未設定時才寫入 (真實環境變數優先)。
//
// 回傳值:
//   - .env 不存在屬正常情況 (例如正式環境直接用環境變數),回傳 nil。
//   - 其他開檔錯誤 (如權限不足) 或讀取錯誤 (如單行過長 ErrTooLong) 會回傳 error,
//     避免 .env 未完整載入卻默默改用預設值。
func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`) // 去除成對引號
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
	// 回報掃描過程中的錯誤 (例如行過長),避免被默默吞掉。
	return scanner.Err()
}

// --- 取值輔助函式 ---

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func getBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

// parseList 將逗號分隔字串解析為去除空白的字串清單。
func parseList(raw string) []string {
	var result []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			result = append(result, p)
		}
	}
	return result
}
