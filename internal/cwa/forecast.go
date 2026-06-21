package cwa

import (
	"encoding/json"
	"fmt"

	"taiwan-weather-proxy/internal/model"
	"taiwan-weather-proxy/internal/timeutil"
)

// CWA F-D0047-069 中本專案需要的天氣要素名稱。
const (
	elemTemperature = "溫度"
	elemWeather     = "天氣現象"
	elemPoP         = "3小時降雨機率"
)

// === F-D0047-069 (鄉鎮天氣預報) 的原始 JSON 結構 ===

type fcResponse struct {
	Success string    `json:"success"`
	Records fcRecords `json:"records"`
}

type fcRecords struct {
	Locations []fcLocations `json:"Locations"`
}

type fcLocations struct {
	LocationsName string       `json:"LocationsName"` // 縣市名 (例如 新北市)
	Location      []fcLocation `json:"Location"`
}

type fcLocation struct {
	LocationName   string             `json:"LocationName"` // 行政區名 (例如 三重區)
	WeatherElement []fcWeatherElement `json:"WeatherElement"`
}

type fcWeatherElement struct {
	ElementName string   `json:"ElementName"`
	Time        []fcTime `json:"Time"`
}

type fcTime struct {
	DataTime     string              `json:"DataTime"`  // 瞬時值 (溫度) 使用
	StartTime    string              `json:"StartTime"` // 時段值 (天氣現象 / 降雨機率) 使用
	EndTime      string              `json:"EndTime"`
	ElementValue []map[string]string `json:"ElementValue"`
}

// ParseForecast 將 F-D0047-069 的回應內容解析為「行政區 -> 預報時段清單」對應。
//
// 解析策略:以「天氣現象」每 3 小時的時段為主軸 (spine),
// 再依時段起點 (StartTime) 對齊「降雨機率」與「溫度」:
//   - 天氣現象 / 降雨機率:以 StartTime 為時段鍵。
//   - 溫度:為瞬時值,以 DataTime 為鍵,取與時段起點相同者。
//
// 對齊以「原始時間字串」精確比對 (CWA 同一時刻的字串完全一致),
// 比對完成後才標準化為台灣時間輸出,避免時區換算造成鍵值不一致。
func ParseForecast(body []byte) (map[string][]model.ForecastRecord, error) {
	var resp fcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析預報資料 JSON 失敗: %w", err)
	}

	result := make(map[string][]model.ForecastRecord)

	for _, locs := range resp.Records.Locations {
		for _, loc := range locs.Location {
			records := buildLocationForecast(loc)
			if len(records) > 0 {
				result[loc.LocationName] = records
			}
		}
	}
	return result, nil
}

// buildLocationForecast 為單一行政區組裝其每 3 小時的預報清單。
func buildLocationForecast(loc fcLocation) []model.ForecastRecord {
	var (
		weatherEl *fcWeatherElement
		tempByKey = map[string]string{} // DataTime  -> 溫度字串
		popByKey  = map[string]string{} // StartTime -> 降雨機率字串
	)

	// 先建立溫度與降雨機率的查找表,並找出作為主軸的天氣現象要素。
	for i := range loc.WeatherElement {
		el := &loc.WeatherElement[i]
		switch el.ElementName {
		case elemWeather:
			weatherEl = el
		case elemTemperature:
			for _, t := range el.Time {
				if v := firstValue(t.ElementValue, "Temperature"); v != "" {
					tempByKey[t.DataTime] = v
				}
			}
		case elemPoP:
			for _, t := range el.Time {
				if v := firstValue(t.ElementValue, "ProbabilityOfPrecipitation"); v != "" {
					popByKey[t.StartTime] = v
				}
			}
		}
	}

	if weatherEl == nil {
		return nil
	}

	records := make([]model.ForecastRecord, 0, len(weatherEl.Time))
	for _, t := range weatherEl.Time {
		rec := model.ForecastRecord{
			Location:    loc.LocationName,
			StartTime:   timeutil.Normalize(t.StartTime),
			Weather:     cleanText(firstValue(t.ElementValue, "Weather")),
			PoP:         parseIntPercent(popByKey[t.StartTime]),
			Temperature: parseTemperatureInt(tempByKey[t.StartTime]),
		}
		// 保留本時段的精簡原始資訊備查。
		raw, _ := json.Marshal(map[string]string{
			"StartTime":   t.StartTime,
			"EndTime":     t.EndTime,
			"Weather":     firstValue(t.ElementValue, "Weather"),
			"WeatherCode": firstValue(t.ElementValue, "WeatherCode"),
			"PoP":         popByKey[t.StartTime],
			"Temperature": tempByKey[t.StartTime],
		})
		rec.RawJSON = string(raw)
		records = append(records, rec)
	}
	return records
}

// firstValue 從 ElementValue 陣列中取出第一個含有指定鍵的值;找不到時回傳空字串。
func firstValue(values []map[string]string, key string) string {
	for _, v := range values {
		if s, ok := v[key]; ok {
			return s
		}
	}
	return ""
}
