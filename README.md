# taiwan-weather-proxy

輕量的天氣資料代理伺服器。定時向中央氣象署 (CWA) 開放資料平臺撈取「即時觀測」與「短效期預報」，整理、扁平化並快取後，提供簡潔的 RESTful API 給前端設備（例如停車場顯示螢幕）調用。

## 功能特色

- **只讀快取**：前端請求一律只從本地快取讀取，絕不即時觸發向 CWA 的請求，避免超過氣象署 API Rate Limit。
- **上游異常容錯**：CWA 異常或逾時時，保留並回傳上一份成功的快取資料（`is_stale` 旗標供前端判斷），確保系統可用性。
- **雙頻率排程**：觀測（預設每 10 分）與預報（預設每小時）各自獨立更新。
- **資料扁平化**：將 CWA 冗長且多層的巢狀 JSON 簡化為前端易用的扁平結構。
- **單一靜態執行檔**：純 Go 實作（含純 Go 的 SQLite driver），無 CGo、無外部 runtime 相依，複製一個檔案即可部署。
- **安全**：授權碼自動於日誌中遮蔽；可選的手動更新端點以權杖保護。

## 資料來源

介接中央氣象署開放資料平臺（<https://opendata.cwa.gov.tw/>）兩支資料集：

| 用途 | 資料集代碼 | 說明 |
| --- | --- | --- |
| 即時觀測 | `O-A0001-001` | 自動氣象站觀測：溫度、相對濕度、當下雨量 |
| 短效期預報 | `F-D0047-069` | 新北市鄉鎮天氣預報：每 3 小時的天氣現象、降雨機率、溫度 |

> 預報資料集 `F-D0047-069` 為「新北市」專用。若要關注其他縣市，請更換 `DATASET_FORECAST` 為對應縣市的資料集代碼。

## 快速開始

### 1. 取得授權碼

至 <https://opendata.cwa.gov.tw/> 註冊並取得授權碼（格式形如 `CWA-XXXXXXXX-...`）。

### 2. 本機執行

```bash
# 準備設定
cp .env.example .env
# 編輯 .env，填入 CWA_API_KEY

# 啟動 API 服務（含內建排程器）
make run
# 或： go run ./cmd/weather-proxy serve
```

服務預設監聽 `0.0.0.0:8000`。啟動時會先暖機更新一次資料。

### 3. Docker 執行

```bash
cp .env.example .env   # 填入 CWA_API_KEY
docker compose up -d --build
```

## 環境變數

| 變數 | 預設值 | 說明 |
| --- | --- | --- |
| `CWA_API_KEY` | （必填） | 中央氣象署授權碼 |
| `CWA_BASE_URL` | `https://opendata.cwa.gov.tw/api/v1/rest/datastore` | datastore API 基底網址 |
| `DATASET_OBSERVATION` | `O-A0001-001` | 即時觀測資料集代碼 |
| `DATASET_FORECAST` | `F-D0047-069` | 短效期預報資料集代碼 |
| `TARGET_LOCATION` | `三重區` | 預設關注的行政區（可逗號分隔多個；第一個為 API 預設值） |
| `FORECAST_HOURS` | `12` | `/forecast` 預設回傳的未來時數 |
| `ENABLE_SCHEDULER` | `true` | 是否啟用內建排程器 |
| `FETCH_INTERVAL_OBSERVATION` | `600` | 觀測更新間隔（秒） |
| `FETCH_INTERVAL_FORECAST` | `3600` | 預報更新間隔（秒） |
| `MAX_RETRIES` | `3` | 上游失敗最大重試次數 |
| `RETRY_DELAY_SECONDS` | `180` | 每次重試間隔（秒） |
| `HTTP_TIMEOUT_SECONDS` | `30` | 單次 HTTP 請求逾時（秒） |
| `STALE_THRESHOLD_OBSERVATION_MINUTES` | `30` | 觀測資料過期門檻（分鐘） |
| `DATABASE_PATH` | `data/weather.db` | SQLite 快取檔案路徑 |
| `LOG_LEVEL` | `INFO` | 日誌等級（DEBUG/INFO/WARN/ERROR） |
| `LOG_DIR` | `logs` | 日誌目錄（每日輪替、自動壓縮、保留 30 天） |
| `API_HOST` | `0.0.0.0` | 監聽位址 |
| `PORT` | `8000` | 監聽埠號 |
| `REFRESH_TOKEN` | （空） | 手動更新端點權杖；留空則停用該端點 |

## API 文件

所有回應皆為 JSON 且支援 CORS。

### 取得即時天氣

```
GET /api/weather/current?location=三重區
```

`location` 參數可省略（採 `TARGET_LOCATION` 第一個值）。

```json
{
  "status": "success",
  "updated_at": "2026-06-21T12:10:46+08:00",
  "data": {
    "location": "三重區",
    "temperature": 35.4,
    "humidity": 46,
    "rainfall_1hr": 0,
    "weather": "晴",
    "obs_time": "2026-06-21 12:00:00",
    "is_stale": false
  }
}
```

- `temperature` / `humidity` / `rainfall_1hr`：無資料時為 `null`（與「值為 0」區分）。
- `is_stale`：觀測時間超過 `STALE_THRESHOLD_OBSERVATION_MINUTES` 時為 `true`。

### 取得未來預報

```
GET /api/weather/forecast?location=三重區&hours=12
```

`hours` 參數可省略（採 `FORECAST_HOURS`）。回傳自「目前所在 3 小時時段」起、每 3 小時一筆。

```json
{
  "status": "success",
  "updated_at": "2026-06-21T12:10:46+08:00",
  "data": {
    "location": "三重區",
    "forecasts": [
      { "time": "2026-06-21 12:00:00", "weather": "晴",   "pop": 0,  "temperature": 35 },
      { "time": "2026-06-21 15:00:00", "weather": "晴",   "pop": 0,  "temperature": 34 },
      { "time": "2026-06-21 18:00:00", "weather": "晴",   "pop": 0,  "temperature": 31 }
    ]
  }
}
```

### 健康檢查

```
GET /health
```

```json
{ "status": "ok" }
```

### 手動觸發更新（選用）

僅在設定 `REFRESH_TOKEN` 時啟用；需於標頭帶 `X-Refresh-Token`。

```bash
curl -X POST http://localhost:8000/api/v1/refresh -H "X-Refresh-Token: <你的權杖>"
```

> 查無資料的行政區會回傳 HTTP 404 與 `{"status":"error", ...}`。

## 部署

- 推薦以 systemd `serve` 模式部署（內建雙排程）。詳見 [docs/deployment-ubuntu.md](docs/deployment-ubuntu.md)。
- 系統架構與設計說明見 [docs/architecture.md](docs/architecture.md)。

## 常用指令

```bash
make help    # 列出所有指令
make build   # 編譯靜態執行檔到 bin/
make run     # 啟動 API 服務
make fetch   # 執行單次資料更新（觀測 + 預報）
make test    # 執行所有測試（含 race 偵測）
```

## 專案結構

```
taiwan-weather-proxy/
├── cmd/weather-proxy/        # 進入點（serve / fetch / version）
├── internal/
│   ├── config/               # 設定載入（.env + 環境變數）
│   ├── logging/              # 日誌（每日輪替 + 壓縮）
│   ├── model/                # 共用資料結構與 API 回應模型
│   ├── timeutil/             # 時間標準化（台灣時區）
│   ├── cwa/                  # CWA API client 與巢狀 JSON 解析
│   ├── storage/              # SQLite 快取層（UPSERT）
│   ├── worker/               # 拉取→挑代表站→寫快取（重試 + 容錯）
│   ├── scheduler/            # 固定間隔排程器
│   └── server/               # HTTP handlers（CORS、只讀快取）
├── deploy/                   # systemd service / timer / crontab 範例
├── docs/                     # 架構與部署文件
├── Dockerfile · docker-compose.yml · Makefile
└── .env.example
```

## 授權

MIT License。
