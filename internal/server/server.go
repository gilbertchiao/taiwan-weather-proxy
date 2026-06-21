// Package server 實作 API 服務模組 (HTTP handlers)。
//
// 採用標準函式庫 net/http 與 Go 1.22 的 ServeMux 路由樣式,不依賴 web 框架。
// 對外端點:
//
//	GET  /api/weather/current   即時天氣 (參數 location,預設見設定)
//	GET  /api/weather/forecast  未來預報 (參數 location、可選 hours)
//	GET  /health                健康檢查
//	POST /api/v1/refresh        手動觸發更新 (需 REFRESH_TOKEN)
//
// 所有回應皆支援 CORS;且 API 一律只從本地快取讀取,不會即時觸發向 CWA 的請求。
package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"taiwan-weather-proxy/internal/config"
	"taiwan-weather-proxy/internal/model"
	"taiwan-weather-proxy/internal/timeutil"
)

// store 為伺服器所需的快取讀取介面 (由 storage.Store 實作)。
type store interface {
	LatestObservation(location string) (*model.ObservationRecord, error)
	LatestForecasts(location, fromStd string, limit int) ([]model.ForecastRecord, error)
	Ping() error
}

// updater 為手動觸發更新所需的介面 (由 worker.Worker 實作)。
type updater interface {
	RunObservationUpdate(ctx context.Context) error
	RunForecastUpdate(ctx context.Context) error
}

// Server 持有相依元件並提供 HTTP handler。
type Server struct {
	store   store
	updater updater
	cfg     *config.Config
	log     *slog.Logger
}

// New 建立 Server。
func New(s store, u updater, cfg *config.Config, log *slog.Logger) *Server {
	return &Server{store: s, updater: u, cfg: cfg, log: log}
}

// Handler 組裝路由並回傳含 CORS 與日誌中介層的 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /api/weather/current", s.handleCurrent)
	mux.HandleFunc("GET /api/weather/forecast", s.handleForecast)
	mux.HandleFunc("POST /api/v1/refresh", s.handleRefresh)
	return s.withCORS(s.withLogging(mux))
}

// handleCurrent 處理 GET /api/weather/current。
func (s *Server) handleCurrent(w http.ResponseWriter, r *http.Request) {
	location := s.resolveLocation(r)

	rec, err := s.store.LatestObservation(location)
	if err != nil {
		s.log.Error("查詢觀測資料發生錯誤", "location", location, "error", err)
		s.writeError(w, http.StatusInternalServerError, "內部錯誤,查詢資料失敗")
		return
	}
	if rec == nil {
		s.writeError(w, http.StatusNotFound, "查無此行政區的觀測資料: "+location)
		return
	}

	data := model.CurrentData{
		Location:    rec.Location,
		Temperature: rec.Temperature,
		Humidity:    rec.Humidity,
		Rainfall1hr: rec.Rainfall,
		Weather:     rec.Weather,
		ObsTime:     rec.ObsTime,
		IsStale:     s.isObservationStale(rec.ObsTime),
	}
	s.writeSuccess(w, rec.FetchedAt, data)
}

// handleForecast 處理 GET /api/weather/forecast。
func (s *Server) handleForecast(w http.ResponseWriter, r *http.Request) {
	location := s.resolveLocation(r)
	hours := s.resolveHours(r)

	// 自「目前所在的 3 小時時段起點」開始取,確保涵蓋正在進行中的時段。
	loc := timeutil.TaipeiLocation()
	now := time.Now().In(loc)
	blockHour := now.Hour() - (now.Hour() % 3)
	from := time.Date(now.Year(), now.Month(), now.Day(), blockHour, 0, 0, 0, loc)
	fromStd := from.Format(timeutil.StdLayout)

	// 每 3 小時一筆;+1 以涵蓋進行中的時段,確保回傳「未來 hours 小時內」完整覆蓋。
	limit := hours/3 + 1

	records, err := s.store.LatestForecasts(location, fromStd, limit)
	if err != nil {
		s.log.Error("查詢預報資料發生錯誤", "location", location, "error", err)
		s.writeError(w, http.StatusInternalServerError, "內部錯誤,查詢資料失敗")
		return
	}
	if len(records) == 0 {
		s.writeError(w, http.StatusNotFound, "查無此行政區的預報資料: "+location)
		return
	}

	forecasts := make([]model.ForecastItem, 0, len(records))
	for _, rec := range records {
		forecasts = append(forecasts, model.ForecastItem{
			Time:        rec.StartTime,
			Weather:     rec.Weather,
			PoP:         rec.PoP,
			Temperature: rec.Temperature,
		})
	}

	data := model.ForecastData{Location: location, Forecasts: forecasts}
	// 同一次更新的時段共用 fetched_at,取首筆即代表本份預報的更新時間。
	s.writeSuccess(w, records[0].FetchedAt, data)
}

// handleHealth 處理 GET /health,確認服務與資料庫狀態。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(); err != nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "error", "database": "unreachable",
		})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleRefresh 處理 POST /api/v1/refresh,提供手動觸發更新 (需權杖)。
//
// 安全考量:僅在設定了 REFRESH_TOKEN 時啟用;呼叫需於標頭帶 X-Refresh-Token
// 且與設定相符。未設定權杖則一律回 404,不洩漏端點存在。
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.cfg.RefreshToken == "" {
		http.NotFound(w, r)
		return
	}
	if !tokenEqual(r.Header.Get("X-Refresh-Token"), s.cfg.RefreshToken) {
		s.writeError(w, http.StatusUnauthorized, "未授權")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	obsErr := s.updater.RunObservationUpdate(ctx)
	fcErr := s.updater.RunForecastUpdate(ctx)
	if obsErr != nil || fcErr != nil {
		// 詳細錯誤僅記錄於伺服器端,對外回傳靜態訊息,避免洩漏內部細節。
		s.log.Error("手動觸發更新失敗", "observation_error", obsErr, "forecast_error", fcErr)
		s.writeError(w, http.StatusBadGateway, "更新失敗,請查看伺服器日誌")
		return
	}
	s.writeJSON(w, http.StatusOK, model.APIResponse{Status: "success", Message: "更新完成"})
}

// resolveLocation 取得請求的目標行政區;未提供 location 參數時採用設定的預設值。
func (s *Server) resolveLocation(r *http.Request) string {
	if loc := strings.TrimSpace(r.URL.Query().Get("location")); loc != "" {
		return loc
	}
	return s.cfg.DefaultLocation()
}

// resolveHours 取得預報時數;未提供或非法時採用設定的預設值,並限制在合理範圍。
func (s *Server) resolveHours(r *http.Request) int {
	hours := s.cfg.ForecastHours
	if v := strings.TrimSpace(r.URL.Query().Get("hours")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			hours = n
		}
	}
	if hours < 3 {
		hours = 3
	}
	if hours > 72 { // F-D0047-069 僅含未來 2 天,上限取 72 小時
		hours = 72
	}
	return hours
}

// isObservationStale 判斷觀測資料是否過期 (距今超過設定門檻)。
// 無法解析觀測時間時保守地視為過期,避免顯示來路不明的舊資料。
func (s *Server) isObservationStale(obsTime string) bool {
	t, err := timeutil.ParseStd(obsTime)
	if err != nil {
		return true
	}
	return time.Since(t) > s.cfg.ObservationStaleThreshold
}

// === 回應輔助 ===

// writeSuccess 輸出標準成功回應 (status=success + updated_at + data)。
func (s *Server) writeSuccess(w http.ResponseWriter, updatedAt string, data any) {
	s.writeJSON(w, http.StatusOK, model.APIResponse{
		Status:    "success",
		UpdatedAt: updatedAt,
		Data:      data,
	})
}

// writeError 輸出標準錯誤回應 (status=error + message)。
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, model.APIResponse{Status: "error", Message: message})
}

// writeJSON 以 JSON 格式輸出回應。
func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.log.Error("輸出 JSON 失敗", "error", err)
	}
}

// === 中介層 ===

// withCORS 為所有回應加上 CORS 標頭,並處理 OPTIONS 預檢請求。
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, X-Refresh-Token")
		h.Set("Access-Control-Max-Age", "86400")

		// 預檢請求直接回 204,不進入後續處理。
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withLogging 為每個請求記錄方法、路徑、狀態碼與耗時。
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("HTTP 請求",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"status", rec.status,
			"duration", time.Since(start).String(),
			"remote", r.RemoteAddr,
		)
	})
}

// statusRecorder 包裝 ResponseWriter 以擷取實際回應狀態碼。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// tokenEqual 以定時 (constant-time) 方式比對權杖,防止計時攻擊。
// 先將兩端各自雜湊為固定長度再比對,即使長度不同也不洩漏長度資訊。
func tokenEqual(provided, expected string) bool {
	p := sha256.Sum256([]byte(provided))
	e := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(p[:], e[:]) == 1
}
