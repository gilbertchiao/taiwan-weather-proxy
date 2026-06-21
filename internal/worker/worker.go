// Package worker 實作排程更新的核心邏輯:
// 拉取 CWA 資料 -> 篩選目標行政區 (觀測另需挑選代表站) -> 寫入本地快取,
// 並涵蓋重試與「上游失敗保留舊快取」的防呆。
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"taiwan-weather-proxy/internal/config"
	"taiwan-weather-proxy/internal/model"
)

// Fetcher 為資料來源介面 (由 cwa.Client 實作),方便測試替換。
type Fetcher interface {
	FetchObservations(ctx context.Context) ([]model.ObservationRecord, error)
	FetchForecast(ctx context.Context, locations ...string) (map[string][]model.ForecastRecord, error)
}

// Store 為快取寫入介面 (由 storage.Store 實作),方便測試替換。
type Store interface {
	SaveObservation(rec model.ObservationRecord) error
	SaveForecasts(records []model.ForecastRecord) error
}

// Worker 負責定期更新觀測與預報資料。
type Worker struct {
	client Fetcher
	store  Store
	cfg    *config.Config
	log    *slog.Logger
}

// New 建立 Worker。
func New(client Fetcher, store Store, cfg *config.Config, log *slog.Logger) *Worker {
	return &Worker{client: client, store: store, cfg: cfg, log: log}
}

// RunObservationUpdate 執行一次觀測資料更新,內含重試機制。
func (w *Worker) RunObservationUpdate(ctx context.Context) error {
	return w.withRetry(ctx, "觀測", w.updateObservationsOnce)
}

// RunForecastUpdate 執行一次預報資料更新,內含重試機制。
func (w *Worker) RunForecastUpdate(ctx context.Context) error {
	return w.withRetry(ctx, "預報", w.updateForecastOnce)
}

// withRetry 以統一的重試策略包裝單次更新動作。
//
// 防呆設計:
//   - 失敗時最多重試 cfg.MaxRetries 次,每次間隔 cfg.RetryDelay。
//   - 重試期間若 context 被取消 (例如收到關閉訊號),立即中止並回傳。
//   - 即使全部失敗也僅回傳錯誤、不會清空本地舊資料 (寫入只做 UPSERT)。
func (w *Worker) withRetry(ctx context.Context, name string, once func(context.Context) (int, error)) error {
	attempts := w.cfg.MaxRetries + 1
	var lastErr error

	for attempt := 1; attempt <= attempts; attempt++ {
		saved, err := once(ctx)
		if err == nil {
			w.log.Info(name+"資料更新成功", "saved_count", saved, "attempt", attempt)
			return nil
		}

		lastErr = err
		w.log.Error(name+"資料更新失敗", "attempt", attempt, "max_attempts", attempts, "error", err)

		if attempt >= attempts {
			break
		}

		w.log.Warn("將於稍後重試", "kind", name, "delay", w.cfg.RetryDelay.String())
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s更新作業遭取消: %w", name, ctx.Err())
		case <-time.After(w.cfg.RetryDelay):
		}
	}

	return fmt.Errorf("%s資料更新最終失敗 (共嘗試 %d 次): %w", name, attempts, lastErr)
}

// updateObservationsOnce 執行單次「拉取觀測 -> 各目標行政區挑代表站 -> 寫入」。
func (w *Worker) updateObservationsOnce(ctx context.Context) (int, error) {
	records, err := w.client.FetchObservations(ctx)
	if err != nil {
		return 0, err
	}

	// 依行政區分組,加速各目標的挑選。
	byLocation := make(map[string][]model.ObservationRecord)
	for _, r := range records {
		if r.Location == "" {
			continue
		}
		byLocation[r.Location] = append(byLocation[r.Location], r)
	}

	savedCount := 0
	for _, location := range w.cfg.TargetLocations {
		candidates := byLocation[location]
		if len(candidates) == 0 {
			w.log.Warn("觀測資料中找不到目標行政區的測站", "location", location)
			continue
		}

		rep := selectRepresentative(candidates)
		if rep == nil {
			w.log.Warn("目標行政區無可用測站", "location", location)
			continue
		}

		if err := w.store.SaveObservation(*rep); err != nil {
			// 單一行政區寫入失敗只記錄並續處理其他行政區,不中斷整體更新。
			w.log.Error("寫入觀測資料失敗", "location", location, "error", err)
			continue
		}
		savedCount++
		w.log.Debug("已寫入觀測資料",
			"location", location, "station", rep.StationName,
			"temperature", floatOrNil(rep.Temperature), "obs_time", rep.ObsTime)
	}

	if savedCount == 0 {
		return 0, fmt.Errorf("未取得任何目標行政區的觀測資料")
	}
	return savedCount, nil
}

// updateForecastOnce 執行單次「拉取預報 -> 寫入各目標行政區的時段」。
func (w *Worker) updateForecastOnce(ctx context.Context) (int, error) {
	result, err := w.client.FetchForecast(ctx, w.cfg.TargetLocations...)
	if err != nil {
		return 0, err
	}

	savedBlocks := 0
	savedLocations := 0
	for _, location := range w.cfg.TargetLocations {
		records, ok := result[location]
		if !ok || len(records) == 0 {
			w.log.Warn("預報資料中找不到目標行政區", "location", location)
			continue
		}

		if err := w.store.SaveForecasts(records); err != nil {
			w.log.Error("寫入預報資料失敗", "location", location, "error", err)
			continue
		}
		savedLocations++
		savedBlocks += len(records)
		w.log.Debug("已寫入預報資料", "location", location, "blocks", len(records))
	}

	if savedLocations == 0 {
		return 0, fmt.Errorf("未取得任何目標行政區的預報資料")
	}
	return savedBlocks, nil
}

// floatOrNil 將 *float64 轉為可記錄的值 (nil 時回傳字串 "nil")。
func floatOrNil(v *float64) any {
	if v == nil {
		return "nil"
	}
	return *v
}
