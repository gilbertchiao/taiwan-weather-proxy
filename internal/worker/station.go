package worker

import (
	"unicode"

	"taiwan-weather-proxy/internal/model"
)

// selectRepresentative 從同一行政區的多個測站中挑選最具代表性的一站。
//
// 背景:一個行政區可能同時有正規氣象站 (例如「三重」C0AI30) 與交通/國道自動站
// (例如「國一S026K」CAA020)。後者雖然也回報溫度,但對「該行政區現在天氣如何」
// 的代表性較低,因此以下列評分挑選,分數最高者勝出,同分時以測站代碼排序求穩定:
//
//   - 有有效溫度:+8 (沒有溫度的站幾乎無用)
//   - 有有效濕度:+2
//   - 站名為正規氣象站樣式 (純中文,如「三重」「板橋」):+4
//   - 有天氣現象文字:+1
//
// 回傳 nil 代表 candidates 為空。
func selectRepresentative(candidates []model.ObservationRecord) *model.ObservationRecord {
	var best *model.ObservationRecord
	bestScore := -1

	for i := range candidates {
		c := &candidates[i]
		score := scoreStation(c)
		if score > bestScore || (score == bestScore && best != nil && c.StationID < best.StationID) {
			best = c
			bestScore = score
		}
	}
	return best
}

// scoreStation 計算單一測站的代表性分數。
func scoreStation(r *model.ObservationRecord) int {
	score := 0
	if r.Temperature != nil {
		score += 8
	}
	if r.Humidity != nil {
		score += 2
	}
	if isProperStationName(r.StationName) {
		score += 4
	}
	if r.Weather != "" {
		score += 1
	}
	return score
}

// isProperStationName 判斷站名是否為正規氣象站樣式。
//
// 正規鄉鎮氣象站站名多為純中文 (例如「三重」「淡水」);交通/國道自動站
// 則常含英數字 (例如「國一S026K」)。因此「不含任何 ASCII 英數字」者視為正規站。
func isProperStationName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
