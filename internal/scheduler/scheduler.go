// Package scheduler 提供輕量的固定間隔排程器 (免外部套件)。
//
// 與整點觸發不同,本專案的觀測 (預設 10 分) 與預報 (預設 1 小時) 各有不同的
// 更新頻率,因此採「固定間隔」模式:每隔 interval 觸發一次任務。
// 每個資料來源各自建立一個 Scheduler 獨立運作。
package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// Job 為排程要執行的任務函式。
type Job func(ctx context.Context)

// Scheduler 為固定間隔觸發的排程器。
type Scheduler struct {
	name     string        // 名稱 (用於日誌,例如「觀測」)
	interval time.Duration // 觸發間隔
	log      *slog.Logger
}

// New 建立排程器。
func New(name string, interval time.Duration, log *slog.Logger) *Scheduler {
	return &Scheduler{name: name, interval: interval, log: log}
}

// Start 以阻塞方式啟動排程迴圈,每隔 interval 觸發一次 job,直到 context 被取消。
//
// 注意:Start 不會在啟動當下立即執行 job (首次觸發在一個 interval 之後);
// 啟動時的暖機更新由呼叫端 (main) 另行處理,以區分「暖機」與「排程」兩種語意。
//
// 通常以 goroutine 呼叫: go sched.Start(ctx, job)。
func (s *Scheduler) Start(ctx context.Context, job Job) {
	s.log.Info("排程器啟動", "kind", s.name, "interval", s.interval.String())

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Info("排程器收到停止訊號,結束", "kind", s.name)
			return
		case <-ticker.C:
			s.log.Info("排程觸發,開始執行任務", "kind", s.name)
			job(ctx)
		}
	}
}
