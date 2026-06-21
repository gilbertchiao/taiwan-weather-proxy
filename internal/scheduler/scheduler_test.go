package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSchedulerTicks 驗證排程器會依間隔重複觸發,且能被 context 取消而結束。
func TestSchedulerTicks(t *testing.T) {
	var count int32
	s := New("測試", 10*time.Millisecond, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Start(ctx, func(context.Context) {
			atomic.AddInt32(&count, 1)
		})
		close(done)
	}()

	// 等待數個間隔後取消。
	time.Sleep(55 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("排程器未在 context 取消後結束")
	}

	got := atomic.LoadInt32(&count)
	if got < 1 {
		t.Errorf("排程器至少應觸發 1 次,實際 %d 次", got)
	}
}

// TestSchedulerStopsImmediatelyOnCancel 驗證啟動後若 context 已取消則立即結束。
func TestSchedulerStopsImmediatelyOnCancel(t *testing.T) {
	s := New("測試", time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		s.Start(ctx, func(context.Context) { t.Error("不應觸發任務") })
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("context 已取消時排程器應立即結束")
	}
}
