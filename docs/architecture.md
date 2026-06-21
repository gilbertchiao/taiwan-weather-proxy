# 系統架構

本文件說明 `taiwan-weather-proxy` 的技術選型、模組職責、資料流與容錯設計。

## 設計目標

1. **保護上游**：前端高頻查詢不可直接打到 CWA，必須由本服務代理並快取，避免觸發氣象署 Rate Limit。
2. **高可用**：CWA 暫時異常時，服務仍須以最後一份成功資料回應。
3. **易部署**：目標環境為 Ubuntu 主機（systemd 服務）與邊緣設備，偏好單一靜態執行檔、零外部 runtime 相依。
4. **資料簡化**：將 CWA 多層巢狀 JSON 扁平化為前端可直接使用的結構。

## 技術選型

| 面向 | 選擇 | 理由 |
| --- | --- | --- |
| 語言 | Go | 編譯為單一靜態執行檔，部署簡單、資源占用低 |
| HTTP | 標準函式庫 `net/http` + Go 1.22 `ServeMux` | 不需 web 框架即可滿足路由需求，減少相依 |
| 快取 | SQLite（`modernc.org/sqlite`，純 Go） | 免 CGo 維持靜態執行檔；UPSERT 確保資料一致 |
| 排程 | 自製固定間隔排程器 | 觀測與預報頻率不同，輕量實作即可 |
| 設定 | 自製極簡 `.env` 解析 + 環境變數 | 12-factor，無外部套件 |
| 日誌 | `log/slog` + 自製每日輪替 | 標準函式庫，附壓縮與保留天數管理 |
| 時區 | 內嵌 `time/tzdata` | 精簡環境也能正確處理 Asia/Taipei |

外部相依僅 SQLite driver 一項，其餘皆標準函式庫。

## 模組職責

| 模組 | 職責 |
| --- | --- |
| `config` | 載入 `.env` 與環境變數，驗證設定合法性 |
| `logging` | 設定 slog；日誌每日輪替、舊檔壓縮、逾期清除 |
| `model` | 跨層共用資料結構與 API 回應模型 |
| `timeutil` | 來源時間（RFC3339）標準化為台灣時間字串 |
| `cwa` | 介接 CWA datastore API，解析並扁平化兩支資料集 |
| `storage` | SQLite 快取讀寫（UPSERT，絕不 DELETE） |
| `worker` | 拉取 → 篩目標行政區（觀測另挑代表站）→ 寫快取，含重試與容錯 |
| `scheduler` | 固定間隔觸發 worker |
| `server` | HTTP handlers，只從快取讀取並回應（CORS） |

## 資料流

```
                    ┌─────────── scheduler (觀測 10 分 / 預報 1 小時) ───────────┐
                    ▼                                                            │
   CWA API ──► cwa.Client ──► (解析扁平化) ──► worker ──► storage(SQLite)        │
 O-A0001-001                                   │            ▲                    │
 F-D0047-069                                   └── 重試/容錯 ┘                    │
                                                                                 │
   前端設備 ──► GET /api/weather/* ──► server ──► storage(只讀) ──► JSON 回應 ◄──┘
```

關鍵原則：**寫入路徑（worker）與讀取路徑（server）透過 SQLite 解耦**。前端永遠只讀快取，不會觸發對 CWA 的請求。

## 快取與容錯設計

- **唯一鍵**：觀測 `(location, obs_time)`、預報 `(location, start_time)`。同一時刻重複寫入會 UPSERT 更新，不產生重複列。
- **絕不 DELETE**：所有寫入只 INSERT/UPDATE。即使上游連續失敗，本地舊資料原封不動，API 仍可回應。
- **重試**：worker 對上游失敗重試 `MAX_RETRIES` 次、間隔 `RETRY_DELAY_SECONDS`，期間尊重關閉訊號。
- **過期標記**：觀測資料超過 `STALE_THRESHOLD_OBSERVATION_MINUTES` 時，回應 `is_stale=true`，由前端決定是否提示。
- **批次交易**：預報一次數十個時段以單一交易寫入，任一筆失敗即回滾，不留半套資料。

## 邊界條件與防呆

- **無效值**：CWA 以 `-99` 等負哨兵表示無資料，解析後轉為 `null`（與「值為 0」區分）。
- **代表站挑選**：一個行政區可能同時有正規氣象站與國道交通站。worker 以評分挑選（有效溫度、濕度、純中文站名、天氣文字），優先採用正規氣象站。
- **時間排序防呆**：`obs_time` 以標準格式儲存（字典序即時間序）；查詢最新時先以 GLOB 驗證格式，避免畸形字串被誤判為最新。
- **時間對齊**：預報的溫度為瞬時值（`DataTime`）、天氣與降雨機率為時段值（`StartTime`）；以原始時間字串精確比對對齊，再標準化輸出。
- **金鑰遮蔽**：授權碼位於查詢字串，所有對外錯誤訊息都先遮蔽（含 URL 編碼形式），避免外洩到日誌。

## 排程部署模式

支援兩種模式（擇一）：

1. **內建排程（推薦）**：`ENABLE_SCHEDULER=true`，`serve` 程序內建觀測與預報兩個獨立排程，無需額外設定。
2. **外部排程**：`ENABLE_SCHEDULER=false`，API 只負責查詢，改由 systemd timer 或 cron 呼叫 `fetch observation` 與 `fetch forecast`（見 `deploy/`）。
