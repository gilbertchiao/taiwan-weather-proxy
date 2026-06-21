package cwa

import (
	"strconv"
	"strings"
)

// invalidSentinel 為 CWA 用來表示「無效 / 無資料」的數值門檻。
//
// CWA 觀測資料常以 -99、-99.0、-990 等負哨兵值代表感測器無資料;
// 由於台灣地表氣象觀測值不可能低於 -90,故凡解析後小於等於此門檻者一律視為無效。
const invalidSentinel = -90.0

// parseFloat 將 CWA 的字串數值轉為 *float64。
//
// 空字串、無法解析、或屬於負哨兵無效值 (<= invalidSentinel) 時回傳 nil,
// 以 nil 明確表達「無資料」,與「值為 0」區分。
func parseFloat(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	if f <= invalidSentinel {
		return nil
	}
	return &f
}

// parseIntPercent 將降雨機率等百分比字串轉為 *int。
//
// 空字串、無法解析、或負值 (含 -99 無效哨兵) 時回傳 nil。
func parseIntPercent(s string) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		// 可能是 "0.0" 之類的浮點字串,退一步以 float 解析後取整。
		if f, ferr := strconv.ParseFloat(s, 64); ferr == nil {
			n = int(f)
		} else {
			return nil
		}
	}
	if n < 0 {
		return nil
	}
	return &n
}

// parseTemperatureInt 將預測溫度字串轉為 *int。
//
// 空字串、無法解析、或屬於負哨兵無效值時回傳 nil。
// 由於台灣氣溫不會低於 -90,故沿用 invalidSentinel 判斷。
func parseTemperatureInt(s string) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	if f <= invalidSentinel {
		return nil
	}
	n := int(f)
	return &n
}

// cleanText 清理天氣現象等文字欄位;若為空或無效哨兵則回傳空字串。
func cleanText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "-99" {
		return ""
	}
	return s
}
