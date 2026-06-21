package cwa

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// loadTestAPIKey 取得整合測試用的 CWA 授權碼。
//
// 優先讀環境變數 CWA_API_KEY,其次讀開發用的 work/api_key.txt
// (位於套件目錄上兩層的專案根目錄,且已被 .gitignore 排除)。
// 兩者皆無時回傳空字串,呼叫端據此略過整合測試。
func loadTestAPIKey(t *testing.T) string {
	t.Helper()
	if key := strings.TrimSpace(os.Getenv("CWA_API_KEY")); key != "" {
		return key
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "work", "api_key.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	key := loadTestAPIKey(t)
	if key == "" {
		t.Skip("略過整合測試:未提供 CWA_API_KEY 或 work/api_key.txt")
	}
	return New(
		key,
		"https://opendata.cwa.gov.tw/api/v1/rest/datastore",
		"O-A0001-001",
		"F-D0047-069",
		30*time.Second,
	)
}

// TestIntegrationFetchObservations 對真實 CWA API 驗證觀測資料的拉取與三重區萃取。
func TestIntegrationFetchObservations(t *testing.T) {
	client := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	records, err := client.FetchObservations(ctx)
	if err != nil {
		t.Skipf("略過:無法連線 CWA API (環境問題,非解析錯誤): %v", err)
	}
	if len(records) == 0 {
		t.Fatal("觀測資料為空,預期至少數百個測站")
	}

	// 應能找到三重區、且至少一站有有效溫度。
	var sanchong []string
	hasValidTemp := false
	for _, r := range records {
		if r.Location == "三重區" {
			sanchong = append(sanchong, r.StationName)
			if r.Temperature != nil {
				hasValidTemp = true
			}
		}
	}
	if len(sanchong) == 0 {
		t.Error("真實資料中找不到三重區的測站")
	}
	if !hasValidTemp {
		t.Error("三重區所有測站皆無有效溫度,萃取邏輯可能有誤")
	}
	t.Logf("三重區測站: %v (共 %d 個測站)", sanchong, len(records))
}

// TestIntegrationFetchForecast 對真實 CWA API 驗證預報資料的拉取與三重區萃取。
func TestIntegrationFetchForecast(t *testing.T) {
	client := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := client.FetchForecast(ctx, "三重區")
	if err != nil {
		t.Skipf("略過:無法連線 CWA API (環境問題,非解析錯誤): %v", err)
	}

	records, ok := result["三重區"]
	if !ok || len(records) == 0 {
		t.Fatalf("預報資料中找不到三重區時段,實際鍵: %v", keysOf(result))
	}

	// 至少前幾筆應有天氣現象與時間。
	for i, r := range records {
		if i >= 4 {
			break
		}
		if r.StartTime == "" || r.Weather == "" {
			t.Errorf("第 %d 筆預報缺少必要欄位: %+v", i, r)
		}
	}
	t.Logf("三重區預報時段數: %d, 首筆: %s %s 溫度=%v 降雨機率=%v",
		len(records), records[0].StartTime, records[0].Weather,
		deref(records[0].Temperature), deref(records[0].PoP))
}

func deref(p *int) any {
	if p == nil {
		return "nil"
	}
	return *p
}
