# taiwan-weather-proxy 常用指令
# 使用 .PHONY 宣告非檔案目標。

BINARY := weather-proxy
PKG    := ./cmd/weather-proxy
BINDIR := bin

.PHONY: help build run fetch fetch-observation fetch-forecast test vet fmt tidy clean docker-build docker-up docker-down

help: ## 顯示可用指令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build: ## 編譯靜態執行檔到 bin/
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINDIR)/$(BINARY) $(PKG)

run: ## 啟動 API 服務 (serve)
	go run $(PKG) serve

fetch: ## 執行單次資料更新 (觀測 + 預報)
	go run $(PKG) fetch

fetch-observation: ## 執行單次觀測更新
	go run $(PKG) fetch observation

fetch-forecast: ## 執行單次預報更新
	go run $(PKG) fetch forecast

test: ## 執行所有測試 (含 race 偵測)
	go test -race -count=1 ./...

vet: ## 靜態檢查
	go vet ./...

fmt: ## 格式化程式碼
	gofmt -w .

tidy: ## 整理相依
	go mod tidy

clean: ## 清除編譯產物
	rm -rf $(BINDIR)

docker-build: ## 建置 Docker 映像
	docker compose build

docker-up: ## 啟動容器 (背景)
	docker compose up -d

docker-down: ## 停止容器
	docker compose down
