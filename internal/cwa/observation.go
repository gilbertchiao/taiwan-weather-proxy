package cwa

import (
	"encoding/json"
	"fmt"

	"taiwan-weather-proxy/internal/model"
	"taiwan-weather-proxy/internal/timeutil"
)

// === O-A0001-001 (自動氣象站-氣象觀測資料) 的原始 JSON 結構 ===
//
// 僅宣告本專案需要的欄位;CWA 回應中其餘欄位會被忽略。

type obsResponse struct {
	Success string     `json:"success"`
	Records obsRecords `json:"records"`
}

type obsRecords struct {
	Station []obsStation `json:"Station"`
}

type obsStation struct {
	StationName string `json:"StationName"`
	StationID   string `json:"StationId"`
	ObsTime     struct {
		DateTime string `json:"DateTime"`
	} `json:"ObsTime"`
	GeoInfo struct {
		CountyName string `json:"CountyName"`
		TownName   string `json:"TownName"`
	} `json:"GeoInfo"`
	WeatherElement obsWeatherElement `json:"WeatherElement"`
}

type obsWeatherElement struct {
	Weather string `json:"Weather"`
	Now     struct {
		Precipitation string `json:"Precipitation"`
	} `json:"Now"`
	AirTemperature   string `json:"AirTemperature"`
	RelativeHumidity string `json:"RelativeHumidity"`
}

// ParseObservations 將 O-A0001-001 的回應內容解析為觀測記錄清單。
//
// 回傳「所有測站」的記錄 (每站一筆),其所屬行政區取自 GeoInfo.TownName;
// 挑選某行政區的代表站、過濾目標行政區等業務邏輯由上層 (worker) 處理。
func ParseObservations(body []byte) ([]model.ObservationRecord, error) {
	var resp obsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析觀測資料 JSON 失敗: %w", err)
	}

	records := make([]model.ObservationRecord, 0, len(resp.Records.Station))
	for _, st := range resp.Records.Station {
		we := st.WeatherElement
		// 保留該站原始 JSON 以利備查 (序列化失敗則留空,不影響主流程)。
		rawJSON, _ := json.Marshal(st)

		records = append(records, model.ObservationRecord{
			Location:    cleanText(st.GeoInfo.TownName),
			StationName: cleanText(st.StationName),
			StationID:   cleanText(st.StationID),
			Temperature: parseFloat(we.AirTemperature),
			Humidity:    parseFloat(we.RelativeHumidity),
			Rainfall:    parseFloat(we.Now.Precipitation),
			Weather:     cleanText(we.Weather),
			ObsTime:     timeutil.Normalize(st.ObsTime.DateTime),
			RawJSON:     string(rawJSON),
		})
	}
	return records, nil
}
