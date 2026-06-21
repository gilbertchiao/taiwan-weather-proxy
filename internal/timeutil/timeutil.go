// Package timeutil 提供全專案統一的時區處理。
//
// 設計理念:
//
//	本服務所有面向使用者的時間 (資料觀測/發佈時間、抓取時間戳記、日誌日界線)
//	一律以台灣時間 (Asia/Taipei) 為準,「不」依賴執行環境的系統時區。如此即使
//	容器或主機未設定 TZ (Go 預設會落在 UTC),程式行為仍與台灣一致,避免時間
//	戳記與日誌切割日界線出現 8 小時偏移。
//
//	執行檔已於 main 套件內嵌 time/tzdata,故即使在不含系統 tzdata 的精簡映像
//	(alpine/scratch) 中,LoadLocation("Asia/Taipei") 仍可成功。
package timeutil

import (
	"strings"
	"time"
)

// StdLayout 為標準化後的時間字串格式 (台灣時間)。
const StdLayout = "2006-01-02 15:04:05"

// taipei 為快取的 Asia/Taipei 時區,於套件載入時解析一次後重複使用,
// 避免每次取用都重新載入。
var taipei = mustTaipei()

// inputLayouts 為來源時間可能出現的格式 (依序嘗試)。
var inputLayouts = []string{
	time.RFC3339,          // 2026-06-21T11:00:00+08:00 (CWA 主要格式)
	"2006-01-02 15:04:05", // 已標準化格式
	"2006-01-02 15:04",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
}

// mustTaipei 載入 Asia/Taipei 時區;萬一載入失敗 (理論上不會,因已內嵌
// tzdata),則退回固定 +08:00 (CST) 作為保底,確保服務不致因時區問題中斷。
func mustTaipei() *time.Location {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

// TaipeiLocation 回傳 Asia/Taipei 時區 (載入失敗時為固定 +08:00)。
func TaipeiLocation() *time.Location {
	return taipei
}

// Now 回傳「台灣時區」的目前時間。
//
// 等同 time.Now().In(TaipeiLocation()),但語意更明確:凡是需要「現在是台灣
// 幾點」的場合 (時間戳記、日誌輪替) 都應使用本函式,而非直接 time.Now(),
// 以免在未設定 TZ 的環境落入 UTC。
func Now() time.Time {
	return time.Now().In(taipei)
}

// Normalize 將來源時間字串標準化為台灣時間的 "YYYY-MM-DD HH:MM:SS"。
//
// 若所有已知格式皆無法解析,則原樣回傳 (去除前後空白),確保至少保留原始資訊。
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range inputLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			// 一律換算到台灣時間後再輸出,確保跨來源的時間基準一致。
			return t.In(taipei).Format(StdLayout)
		}
	}
	return s
}

// ParseStd 將標準化字串 (StdLayout) 解析為台灣時間的 time.Time。
//
// 供「資料是否過期」等需要時間運算的情境使用。
func ParseStd(s string) (time.Time, error) {
	return time.ParseInLocation(StdLayout, strings.TrimSpace(s), taipei)
}
