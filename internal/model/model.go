// Package model 定義跨層共用的資料結構。
//
// 設計原則:可能缺值的數值欄位 (溫度、濕度、雨量、降雨機率…) 一律使用
// 指標型別,以區分「值為 0」與「無資料 (null)」兩種情況;CWA 來源常以
// "-99" 表示無效值,經解析後會轉為 nil。
package model

// === 內部表示 (儲存層與業務邏輯共用) ===

// ObservationRecord 為單筆即時觀測資料的內部表示 (對應一個測站)。
type ObservationRecord struct {
	Location    string   // 所屬行政區 (GeoInfo.TownName,例如 三重區)
	StationName string   // 測站名稱 (例如 三重)
	StationID   string   // 測站代碼 (例如 C0AI30),供挑選代表站之用
	Temperature *float64 // 氣溫 (°C),無資料時為 nil
	Humidity    *float64 // 相對濕度 (%),無資料時為 nil
	Rainfall    *float64 // 當下雨量 (mm,對應 Now.Precipitation),無資料時為 nil
	Weather     string   // 天氣現象文字 (例如「晴」),無資料時為空字串
	ObsTime     string   // 觀測時間,標準化為 "YYYY-MM-DD HH:MM:SS"
	RawJSON     string   // 原始資料 JSON,保留備查
	FetchedAt   string   // 本筆寫入快取的時間 (RFC3339),由儲存層填入
}

// ForecastRecord 為單一 3 小時預報時段的內部表示。
type ForecastRecord struct {
	Location    string // 所屬行政區 (例如 三重區)
	StartTime   string // 時段起點,標準化為 "YYYY-MM-DD HH:MM:SS"
	Weather     string // 天氣現象文字 (例如「多雲」)
	PoP         *int   // 降雨機率 (%),無資料時為 nil
	Temperature *int   // 預測溫度 (°C),無資料時為 nil
	RawJSON     string // 原始資料 JSON,保留備查
	FetchedAt   string // 本筆寫入快取的時間 (RFC3339),由儲存層填入
}

// === API 回應結構 (對應系統規格指定的輸出格式) ===

// CurrentData 為 GET /api/weather/current 的 data 欄位內容。
type CurrentData struct {
	Location    string   `json:"location"`
	Temperature *float64 `json:"temperature"`
	Humidity    *float64 `json:"humidity"`
	Rainfall1hr *float64 `json:"rainfall_1hr"`
	Weather     string   `json:"weather,omitempty"`
	ObsTime     string   `json:"obs_time"`
	IsStale     bool     `json:"is_stale"`
}

// ForecastItem 為單筆未來預報 (每 3 小時)。
type ForecastItem struct {
	Time        string `json:"time"`
	Weather     string `json:"weather"`
	PoP         *int   `json:"pop"`
	Temperature *int   `json:"temperature"`
}

// ForecastData 為 GET /api/weather/forecast 的 data 欄位內容。
type ForecastData struct {
	Location  string         `json:"location"`
	Forecasts []ForecastItem `json:"forecasts"`
}

// APIResponse 為 API 標準回應外層結構 (對應規格的 status / updated_at / data)。
//
// Data 使用 any 以容納 CurrentData 或 ForecastData;錯誤情境則以 Message 說明。
type APIResponse struct {
	Status    string `json:"status"`               // "success" 或 "error"
	UpdatedAt string `json:"updated_at,omitempty"` // 資料更新時間 (ISO8601)
	Data      any    `json:"data,omitempty"`
	Message   string `json:"message,omitempty"`
}
