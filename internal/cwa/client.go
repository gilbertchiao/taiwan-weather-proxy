// Package cwa 封裝對中央氣象署 (CWA) 開放資料平臺 datastore API 的存取與解析。
//
// 介接資料集:
//   - O-A0001-001:自動氣象站-氣象觀測資料 (即時觀測)。
//   - F-D0047-069:鄉鎮天氣預報-新北市未來 2 天 (每 3 小時短效期預報)。
//
// 請求形式: GET {base}/{dataset}?Authorization=...&format=JSON[&LocationName=...]
//
// 安全性:授權碼位於查詢字串,任何對外回傳或記錄的錯誤訊息都會先經 redact
// 遮蔽授權碼,避免金鑰透過日誌外洩。
package cwa

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"taiwan-weather-proxy/internal/model"
)

// maxResponseBytes 限制單次回應讀取大小,避免異常回應耗盡記憶體。
const maxResponseBytes = 20 << 20 // 20MB (全台測站觀測資料約 1MB,預留充足餘裕)

// Client 為 CWA datastore API 用戶端。
type Client struct {
	apiKey          string
	baseURL         string
	datasetObs      string
	datasetForecast string
	httpClient      *http.Client
}

// New 建立 CWA API 用戶端。
func New(apiKey, baseURL, datasetObs, datasetForecast string, timeout time.Duration) *Client {
	return &Client{
		apiKey:          apiKey,
		baseURL:         strings.TrimRight(baseURL, "/"),
		datasetObs:      datasetObs,
		datasetForecast: datasetForecast,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// FetchObservations 拉取並解析即時觀測資料 (所有測站)。
//
// 任何網路錯誤、非 2xx 狀態碼或 JSON 解析失敗都會回傳 error (授權碼已遮蔽)。
func (c *Client) FetchObservations(ctx context.Context) ([]model.ObservationRecord, error) {
	body, err := c.fetchRaw(ctx, c.datasetObs, nil)
	if err != nil {
		return nil, err
	}
	return ParseObservations(body)
}

// FetchForecast 拉取並解析短效期預報資料,回傳「行政區 -> 預報時段清單」對應。
//
// 可選擇性帶入 locations 以縮小回應範圍 (CWA 支援重複的 LocationName 參數);
// 未提供時則取回整個資料集 (該縣市所有行政區)。
func (c *Client) FetchForecast(ctx context.Context, locations ...string) (map[string][]model.ForecastRecord, error) {
	var q url.Values
	if len(locations) > 0 {
		q = url.Values{}
		for _, loc := range locations {
			if loc = strings.TrimSpace(loc); loc != "" {
				q.Add("LocationName", loc)
			}
		}
	}

	body, err := c.fetchRaw(ctx, c.datasetForecast, q)
	if err != nil {
		return nil, err
	}
	return ParseForecast(body)
}

// fetchRaw 對指定資料集發出 GET 請求並回傳原始內容。
func (c *Client) fetchRaw(ctx context.Context, dataset string, extra url.Values) ([]byte, error) {
	endpoint, err := c.buildURL(dataset, extra)
	if err != nil {
		return nil, fmt.Errorf("組裝請求網址失敗: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("建立 HTTP 請求失敗: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// 注意:net/http 的 *url.Error 會在訊息中夾帶完整請求網址 (含授權碼),
		// 故務必先 redact 再回傳,避免金鑰外洩到日誌。
		return nil, fmt.Errorf("呼叫 CWA API 失敗 (dataset=%s): %s", dataset, c.redact(err.Error()))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("讀取回應內容失敗 (dataset=%s): %s", dataset, c.redact(err.Error()))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 將上游錯誤視為失敗 (含 5xx),交由 worker 進行重試。
		preview := string(body)
		if len(preview) > 300 {
			preview = preview[:300]
		}
		return nil, fmt.Errorf("CWA API 回傳非 2xx 狀態 (dataset=%s, code=%d): %s",
			dataset, resp.StatusCode, c.redact(preview))
	}

	return body, nil
}

// buildURL 組裝帶有授權碼與查詢參數的完整請求網址。
func (c *Client) buildURL(dataset string, extra url.Values) (string, error) {
	base, err := url.Parse(fmt.Sprintf("%s/%s", c.baseURL, dataset))
	if err != nil {
		return "", err
	}
	q := base.Query()
	q.Set("Authorization", c.apiKey)
	q.Set("format", "JSON")
	for k, vs := range extra {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	base.RawQuery = q.Encode()
	return base.String(), nil
}

// redact 將字串中的授權碼遮蔽,避免透過錯誤訊息或日誌外洩。
// 同時處理授權碼以 URL 編碼形式出現在查詢字串中的情況。
func (c *Client) redact(s string) string {
	if c.apiKey == "" {
		return s
	}
	s = strings.ReplaceAll(s, c.apiKey, "***REDACTED***")
	if encoded := url.QueryEscape(c.apiKey); encoded != c.apiKey {
		s = strings.ReplaceAll(s, encoded, "***REDACTED***")
	}
	return s
}
