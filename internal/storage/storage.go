// Package storage 提供基於 SQLite 的本地快取層。
//
// 採用純 Go 的 modernc.org/sqlite driver (免 CGo),以維持「單一靜態執行檔」
// 的部署優勢。
//
// 設計重點:
//   - 觀測資料以 (location, obs_time)、預報資料以 (location, start_time) 為唯一鍵,
//     確保同一時刻不重複,同時保留不同時刻的資料。
//   - 寫入一律採 UPSERT,絕不執行 DELETE,確保上游異常時舊快取不會被清空。
//   - 啟用 WAL 模式以提升 API 讀取與 worker 寫入的併發。
package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"taiwan-weather-proxy/internal/model"
	"taiwan-weather-proxy/internal/timeutil"

	_ "modernc.org/sqlite"
)

// schema 為資料表與索引定義。
const schema = `
CREATE TABLE IF NOT EXISTS observations (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    location     TEXT    NOT NULL,
    station_name TEXT,
    station_id   TEXT,
    temperature  REAL,
    humidity     REAL,
    rainfall     REAL,
    weather      TEXT,
    obs_time     TEXT    NOT NULL,
    raw_json     TEXT,
    fetched_at   TEXT    NOT NULL,
    UNIQUE(location, obs_time)
);
CREATE INDEX IF NOT EXISTS idx_obs_location_time ON observations(location, obs_time DESC);

CREATE TABLE IF NOT EXISTS forecasts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    location    TEXT    NOT NULL,
    start_time  TEXT    NOT NULL,
    weather     TEXT,
    pop         INTEGER,
    temperature INTEGER,
    raw_json    TEXT,
    fetched_at  TEXT    NOT NULL,
    UNIQUE(location, start_time)
);
CREATE INDEX IF NOT EXISTS idx_fc_location_time ON forecasts(location, start_time);
`

// Store 封裝資料庫連線與相關操作。
type Store struct {
	db *sql.DB
}

// New 開啟 (或建立) SQLite 資料庫,套用 PRAGMA 並初始化資料表。
func New(dbPath string) (*Store, error) {
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("建立資料庫目錄失敗: %w", err)
		}
	}

	// _pragma 以連線字串參數設定,確保每條連線都套用。
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=foreign_keys(ON)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("開啟資料庫失敗: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("連線資料庫失敗: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化資料表失敗: %w", err)
	}

	return &Store{db: db}, nil
}

// Close 關閉資料庫連線。
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping 確認資料庫連線正常。
func (s *Store) Ping() error {
	return s.db.Ping()
}

// === 觀測資料 ===

// SaveObservation 以 UPSERT 寫入單筆觀測資料。
// 同一 (location, obs_time) 已存在時會更新該筆,不會新增重複資料;
// 整個操作只 INSERT/UPDATE,絕不 DELETE,確保舊資料不被清空。
func (s *Store) SaveObservation(rec model.ObservationRecord) error {
	const query = `
INSERT INTO observations (location, station_name, station_id, temperature, humidity, rainfall, weather, obs_time, raw_json, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(location, obs_time) DO UPDATE SET
    station_name = excluded.station_name,
    station_id   = excluded.station_id,
    temperature  = excluded.temperature,
    humidity     = excluded.humidity,
    rainfall     = excluded.rainfall,
    weather      = excluded.weather,
    raw_json     = excluded.raw_json,
    fetched_at   = excluded.fetched_at;
`
	fetchedAt := nowRFC3339()
	_, err := s.db.Exec(query,
		rec.Location, nullStr(rec.StationName), nullStr(rec.StationID),
		nullFloat(rec.Temperature), nullFloat(rec.Humidity), nullFloat(rec.Rainfall),
		nullStr(rec.Weather), rec.ObsTime, nullStr(rec.RawJSON), fetchedAt,
	)
	if err != nil {
		return fmt.Errorf("寫入觀測資料失敗 (location=%s, obs_time=%s): %w", rec.Location, rec.ObsTime, err)
	}
	return nil
}

// LatestObservation 取得指定行政區的最新一筆觀測資料 (依 obs_time 降冪)。
// 查無資料時回傳 (nil, nil)。
func (s *Store) LatestObservation(location string) (*model.ObservationRecord, error) {
	// 排序防呆:obs_time 為標準格式 (YYYY-MM-DD HH:MM:SS) 時字典序即時間序;
	// 先以 GLOB 確保格式正確者優先,避免畸形字串被誤判為最新。
	const query = `
SELECT location, COALESCE(station_name,''), COALESCE(station_id,''),
       temperature, humidity, rainfall, COALESCE(weather,''),
       obs_time, COALESCE(raw_json,''), fetched_at
FROM observations
WHERE location = ?
ORDER BY
    (obs_time GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]') DESC,
    obs_time DESC,
    fetched_at DESC
LIMIT 1;`

	row := s.db.QueryRow(query, location)

	var (
		rec               model.ObservationRecord
		temp, humid, rain sql.NullFloat64
	)
	err := row.Scan(&rec.Location, &rec.StationName, &rec.StationID,
		&temp, &humid, &rain, &rec.Weather, &rec.ObsTime, &rec.RawJSON, &rec.FetchedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查詢最新觀測資料失敗 (location=%s): %w", location, err)
	}

	if temp.Valid {
		rec.Temperature = &temp.Float64
	}
	if humid.Valid {
		rec.Humidity = &humid.Float64
	}
	if rain.Valid {
		rec.Rainfall = &rain.Float64
	}
	return &rec, nil
}

// === 預報資料 ===

// SaveForecasts 以單一交易批次 UPSERT 多筆預報時段。
// 任一筆失敗即回滾整個交易,確保不會留下半套資料;成功的舊資料始終保留。
func (s *Store) SaveForecasts(records []model.ForecastRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("開啟預報寫入交易失敗: %w", err)
	}
	// 若中途 return err 未 Commit,defer 會回滾 (已 Commit 後的 Rollback 為無操作)。
	defer func() { _ = tx.Rollback() }()

	const query = `
INSERT INTO forecasts (location, start_time, weather, pop, temperature, raw_json, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(location, start_time) DO UPDATE SET
    weather     = excluded.weather,
    pop         = excluded.pop,
    temperature = excluded.temperature,
    raw_json    = excluded.raw_json,
    fetched_at  = excluded.fetched_at;
`
	fetchedAt := nowRFC3339()
	stmt, err := tx.Prepare(query)
	if err != nil {
		return fmt.Errorf("準備預報寫入語句失敗: %w", err)
	}
	defer stmt.Close()

	for _, rec := range records {
		if _, err := stmt.Exec(
			rec.Location, rec.StartTime, nullStr(rec.Weather),
			nullInt(rec.PoP), nullInt(rec.Temperature), nullStr(rec.RawJSON), fetchedAt,
		); err != nil {
			return fmt.Errorf("寫入預報資料失敗 (location=%s, start_time=%s): %w",
				rec.Location, rec.StartTime, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交預報寫入交易失敗: %w", err)
	}
	return nil
}

// LatestForecasts 取得指定行政區、起點時間 >= fromStd 的預報時段,
// 依 start_time 升冪排序,最多回傳 limit 筆 (limit <= 0 表示不限筆數)。
//
// fromStd 為標準格式時間字串 ("YYYY-MM-DD HH:MM:SS");因該格式字典序即時間序,
// 可直接用字串比較過濾。
func (s *Store) LatestForecasts(location, fromStd string, limit int) ([]model.ForecastRecord, error) {
	query := `
SELECT location, start_time, COALESCE(weather,''), pop, temperature, COALESCE(raw_json,''), fetched_at
FROM forecasts
WHERE location = ? AND start_time >= ?
ORDER BY start_time ASC`
	args := []any{location, fromStd}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢預報資料失敗 (location=%s): %w", location, err)
	}
	defer rows.Close()

	var result []model.ForecastRecord
	for rows.Next() {
		var (
			rec       model.ForecastRecord
			pop, temp sql.NullInt64
		)
		if err := rows.Scan(&rec.Location, &rec.StartTime, &rec.Weather,
			&pop, &temp, &rec.RawJSON, &rec.FetchedAt); err != nil {
			return nil, fmt.Errorf("讀取預報資料列失敗: %w", err)
		}
		if pop.Valid {
			v := int(pop.Int64)
			rec.PoP = &v
		}
		if temp.Valid {
			v := int(temp.Int64)
			rec.Temperature = &v
		}
		result = append(result, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("走訪預報資料時發生錯誤: %w", err)
	}
	return result, nil
}

// === 內部輔助 ===

func nowRFC3339() string {
	// 以台灣時間記錄抓取時刻,RFC3339 會帶 +08:00 時區後綴,
	// 與資料庫中以台灣時間儲存的 obs_time / start_time 保持一致,
	// 不受系統時區影響 (容器未設 TZ 時 time.Now() 預設為 UTC)。
	return timeutil.Now().Format(time.RFC3339)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
