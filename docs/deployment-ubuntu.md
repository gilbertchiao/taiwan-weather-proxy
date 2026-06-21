# Ubuntu 部署指南（systemd）

本文件說明如何在 Ubuntu 主機上以 systemd 部署 `taiwan-weather-proxy`。

部署路徑以 `/opt/taiwan-weather-proxy` 為例，服務以專用使用者 `weather` 執行。

## 1. 前置準備

```bash
# 建立專用系統使用者（不可登入）
sudo useradd --system --no-create-home --shell /usr/sbin/nologin weather

# 建立目錄
sudo mkdir -p /opt/taiwan-weather-proxy/{bin,data,logs}
```

## 2. 編譯與佈署執行檔

在開發機（已安裝 Go 1.26+）編譯靜態執行檔：

```bash
make build
# 產出 bin/weather-proxy
```

將執行檔複製到目標主機：

```bash
sudo cp bin/weather-proxy /opt/taiwan-weather-proxy/bin/
```

## 3. 設定 .env

```bash
sudo cp .env.example /opt/taiwan-weather-proxy/.env
sudo nano /opt/taiwan-weather-proxy/.env   # 填入 CWA_API_KEY 等設定
```

設定目錄擁有者（讓服務可寫入 data/ 與 logs/）：

```bash
sudo chown -R weather:weather /opt/taiwan-weather-proxy
sudo chmod 640 /opt/taiwan-weather-proxy/.env
```

## 4. 安裝服務

### 模式 A：內建排程（推薦）

確認 `.env` 內 `ENABLE_SCHEDULER=true`，安裝主服務：

```bash
sudo cp deploy/taiwan-weather-proxy.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now taiwan-weather-proxy
```

服務啟動後會先暖機更新一次，並依設定的間隔自動更新觀測與預報。

### 模式 B：外部 timer 排程

設定 `.env` 內 `ENABLE_SCHEDULER=false`（API 只負責查詢），安裝主服務與兩組 timer：

```bash
# 主服務（查詢用）
sudo cp deploy/taiwan-weather-proxy.service /etc/systemd/system/

# 觀測：每 10 分鐘
sudo cp deploy/taiwan-weather-proxy-fetch-observation.service /etc/systemd/system/
sudo cp deploy/taiwan-weather-proxy-fetch-observation.timer   /etc/systemd/system/

# 預報：每小時
sudo cp deploy/taiwan-weather-proxy-fetch-forecast.service /etc/systemd/system/
sudo cp deploy/taiwan-weather-proxy-fetch-forecast.timer   /etc/systemd/system/

sudo systemctl daemon-reload
sudo systemctl enable --now taiwan-weather-proxy
sudo systemctl enable --now taiwan-weather-proxy-fetch-observation.timer
sudo systemctl enable --now taiwan-weather-proxy-fetch-forecast.timer
```

## 5. 驗證

```bash
# 服務狀態
sudo systemctl status taiwan-weather-proxy

# 即時日誌
sudo journalctl -u taiwan-weather-proxy -f

# 健康檢查與 API
curl http://localhost:8000/health
curl http://localhost:8000/api/weather/current

# 檢視 timer 排程（模式 B）
systemctl list-timers 'taiwan-weather-proxy-*'
```

## 6. 升級

```bash
sudo systemctl stop taiwan-weather-proxy
sudo cp bin/weather-proxy /opt/taiwan-weather-proxy/bin/
sudo systemctl start taiwan-weather-proxy
```

## 7. 移除

```bash
sudo systemctl disable --now taiwan-weather-proxy
sudo systemctl disable --now taiwan-weather-proxy-fetch-observation.timer 2>/dev/null
sudo systemctl disable --now taiwan-weather-proxy-fetch-forecast.timer 2>/dev/null
sudo rm /etc/systemd/system/taiwan-weather-proxy*.service /etc/systemd/system/taiwan-weather-proxy*.timer
sudo systemctl daemon-reload
# 如需一併移除資料：sudo rm -rf /opt/taiwan-weather-proxy
```

## 安全性說明

`deploy/` 內的 service 單元已套用 systemd 沙箱強化：

- `NoNewPrivileges`、`ProtectSystem=strict`、`ProtectHome`：限制檔案系統存取，僅 `ReadWritePaths` 指定的 data/、logs/ 可寫。
- `RestrictNamespaces`、`RestrictSUIDSGID`、`ProtectKernel*`、`LockPersonality`：縮小攻擊面。
- 以專用非特權使用者 `weather` 執行。
- `.env` 權限設為 `640` 並由 `weather` 擁有，避免授權碼外洩。
