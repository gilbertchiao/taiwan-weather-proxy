// Command weather-proxy 為 taiwan-weather-proxy 的進入點。
//
// 子命令:
//
//	serve    啟動 API 服務 (預設),並依設定啟動內建排程器與啟動時暖機更新。
//	fetch    執行單次資料更新後結束,適合搭配外部 Cron / systemd timer 使用。
//	version  顯示版本資訊。
//
// 用法範例:
//
//	weather-proxy            # 等同 weather-proxy serve
//	weather-proxy serve
//	weather-proxy fetch
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// 內嵌時區資料庫,讓 Asia/Taipei 時區在不含 tzdata 的精簡環境
	// (例如 scratch 映像或邊緣裝置) 也能正確運作,維持單一執行檔的可攜性。
	_ "time/tzdata"

	"taiwan-weather-proxy/internal/config"
	"taiwan-weather-proxy/internal/cwa"
	"taiwan-weather-proxy/internal/logging"
	"taiwan-weather-proxy/internal/scheduler"
	"taiwan-weather-proxy/internal/server"
	"taiwan-weather-proxy/internal/storage"
	"taiwan-weather-proxy/internal/worker"
)

// version 於建置時可透過 -ldflags 覆寫。
var version = "1.0.0"

func main() {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	if command == "version" {
		fmt.Printf("taiwan-weather-proxy %s\n", version)
		return
	}

	// 載入設定。
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "載入設定失敗: %v\n", err)
		os.Exit(1)
	}

	// 初始化日誌。
	logger, closer, err := logging.Setup(cfg.LogDir, cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化日誌失敗: %v\n", err)
		os.Exit(1)
	}
	defer closer.Close()

	if cfg.CWAAPIKey == "" {
		logger.Warn("未設定 CWA_API_KEY,資料更新將會失敗;API 仍可提供既有舊資料")
	}

	// 初始化快取 (儲存層)。
	store, err := storage.New(cfg.DatabasePath)
	if err != nil {
		logger.Error("初始化快取失敗", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// 組裝 CWA client 與 worker。
	client := cwa.New(cfg.CWAAPIKey, cfg.CWABaseURL, cfg.DatasetObservation, cfg.DatasetForecast, cfg.HTTPTimeout)
	wk := worker.New(client, store, cfg, logger)

	switch command {
	case "fetch":
		// 可選目標:observation / forecast / (省略=兩者)。
		target := "all"
		if len(os.Args) > 2 {
			target = os.Args[2]
		}
		runFetch(wk, logger, target)
	case "serve":
		runServe(cfg, store, wk, logger)
	default:
		fmt.Fprintf(os.Stderr, "未知的子命令: %s (可用: serve, fetch, version)\n", command)
		os.Exit(2)
	}
}

// runFetch 執行單次資料更新,供外部 Cron / systemd timer 呼叫。
//
// target 可為:
//
//	observation  只更新即時觀測
//	forecast     只更新預報
//	all (或空值) 兩者都更新
//
// 觀測與預報更新頻率不同 (預設 10 分 / 1 小時),分開觸發可讓外部排程
// 分別以各自的頻率呼叫,避免低頻資料被過度抓取。
func runFetch(wk *worker.Worker, logger *slog.Logger, target string) {
	ctx, cancel := signalContext()
	defer cancel()

	if target == "" {
		target = "all"
	}
	logger.Info("執行單次資料更新 (fetch 模式)", "target", target)

	var obsErr, fcErr error
	switch target {
	case "observation":
		obsErr = wk.RunObservationUpdate(ctx)
	case "forecast":
		fcErr = wk.RunForecastUpdate(ctx)
	case "all":
		obsErr = wk.RunObservationUpdate(ctx)
		fcErr = wk.RunForecastUpdate(ctx)
	default:
		fmt.Fprintf(os.Stderr, "未知的 fetch 目標: %s (可用: observation, forecast, 或省略代表兩者)\n", target)
		os.Exit(2)
	}

	if obsErr != nil {
		logger.Error("觀測資料單次更新失敗", "error", obsErr)
	}
	if fcErr != nil {
		logger.Error("預報資料單次更新失敗", "error", fcErr)
	}
	if obsErr != nil || fcErr != nil {
		os.Exit(1)
	}
	logger.Info("單次更新完成")
}

// runServe 啟動 API 服務、雙排程器與啟動暖機更新,並支援優雅關閉。
func runServe(cfg *config.Config, store *storage.Store, wk *worker.Worker, logger *slog.Logger) {
	ctx, cancel := signalContext()
	defer cancel()

	// 啟動時先暖機更新一次,確保快取有最新資料 (非阻塞)。
	go warmUp(ctx, wk, logger)

	// 啟動雙排程器 (觀測與預報各自獨立頻率)。
	if cfg.EnableScheduler {
		obsSched := scheduler.New("觀測", cfg.ObservationInterval, logger)
		go obsSched.Start(ctx, func(c context.Context) {
			if err := wk.RunObservationUpdate(c); err != nil {
				logger.Error("觀測排程更新失敗", "error", err)
			}
		})

		fcSched := scheduler.New("預報", cfg.ForecastInterval, logger)
		go fcSched.Start(ctx, func(c context.Context) {
			if err := wk.RunForecastUpdate(c); err != nil {
				logger.Error("預報排程更新失敗", "error", err)
			}
		})
	} else {
		logger.Info("內建排程器已停用 (ENABLE_SCHEDULER=false),請改用外部 Cron / systemd timer 觸發 fetch")
	}

	// 建立 HTTP 伺服器。
	srv := server.New(store, wk, cfg, logger)
	addr := fmt.Sprintf("%s:%d", cfg.APIHost, cfg.APIPort)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 於背景啟動 HTTP 服務。
	go func() {
		logger.Info("API 服務啟動", "addr", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP 服務異常結束", "error", err)
			cancel()
		}
	}()

	// 等待停止訊號。
	<-ctx.Done()
	logger.Info("收到關閉訊號,開始優雅關閉")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("優雅關閉逾時", "error", err)
	}
	logger.Info("服務已關閉")
}

// warmUp 於服務啟動時先各更新一次觀測與預報資料。
func warmUp(ctx context.Context, wk *worker.Worker, logger *slog.Logger) {
	logger.Info("啟動暖機:嘗試先更新一次資料")
	if err := wk.RunObservationUpdate(ctx); err != nil {
		logger.Error("啟動暖機-觀測更新失敗 (將於下次排程重試)", "error", err)
	}
	if err := wk.RunForecastUpdate(ctx); err != nil {
		logger.Error("啟動暖機-預報更新失敗 (將於下次排程重試)", "error", err)
	}
}

// signalContext 回傳一個會在收到 SIGINT/SIGTERM 時取消的 context。
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}
