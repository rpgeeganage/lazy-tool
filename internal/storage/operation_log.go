package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type OperationLogRow struct {
	ID           int64
	Operation    string
	SourceID     string
	DurationMS   int64
	MetadataJSON string
	Error        string
	CreatedAt    time.Time
}

type OperationLogEvent struct {
	Operation  string
	SourceID   string
	DurationMS int64
	Metadata   map[string]any
	Error      string
	CreatedAt  time.Time
}

type TelemetryPurgeConfig struct {
	RetentionDays int
	MaxRows       int
}

type OperationSummary struct {
	Count         int64
	ErrorCount    int64
	AverageMS     float64
	LatestAt      *time.Time
	CacheHits     int64
	CacheMisses   int64
	ReindexCount  int64
	SearchCount   int64
	ProxyCount    int64
	VectorCount   int64
	EmbedCount    int64
	ErrorClasses  map[string]int64
}

type SearchTimelinePoint struct {
	BucketStart time.Time
	Count       int64
	AverageMS   float64
	Errors      int64
}

type SourceOperationStats struct {
	SourceID     string
	Count        int64
	ErrorCount   int64
	AverageMS    float64
	LatestError  string
	LatestAt     *time.Time
	LastReindexOK *bool
	LastReindexMessage string
	LastReindexAt *time.Time
	ErrorClasses map[string]int64
}

func (s *SQLiteStore) ensureOperationLog(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS operation_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	operation TEXT NOT NULL,
	source_id TEXT,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	error TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_operation_log_created_at ON operation_log(created_at);
CREATE INDEX IF NOT EXISTS idx_operation_log_operation_created_at ON operation_log(operation, created_at);
CREATE INDEX IF NOT EXISTS idx_operation_log_source_created_at ON operation_log(source_id, created_at);
`)
	return err
}

func (s *SQLiteStore) RecordOperation(ctx context.Context, event OperationLogEvent) error {
	if err := s.ensureOperationLog(ctx); err != nil {
		return err
	}
	if event.Operation == "" {
		return fmt.Errorf("operation is required")
	}
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	metadataJSON := "{}"
	if len(event.Metadata) > 0 {
		b, err := json.Marshal(event.Metadata)
		if err != nil {
			return err
		}
		metadataJSON = string(b)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO operation_log(operation, source_id, duration_ms, metadata_json, error, created_at)
VALUES(?,?,?,?,?,?)
`, event.Operation, nullIfEmpty(event.SourceID), event.DurationMS, metadataJSON, event.Error, createdAt.UnixMilli())
	return err
}

func (s *SQLiteStore) ListRecentOperations(ctx context.Context, limit int) ([]OperationLogRow, error) {
	if err := s.ensureOperationLog(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, operation, COALESCE(source_id, ''), duration_ms, metadata_json, error, created_at
FROM operation_log
ORDER BY id DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OperationLogRow
	for rows.Next() {
		var r OperationLogRow
		var createdAtMS int64
		if err := rows.Scan(&r.ID, &r.Operation, &r.SourceID, &r.DurationMS, &r.MetadataJSON, &r.Error, &createdAtMS); err != nil {
			return nil, err
		}
		r.CreatedAt = time.UnixMilli(createdAtMS).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) CountOperationLog(ctx context.Context) (int, error) {
	if err := s.ensureOperationLog(ctx); err != nil {
		return 0, err
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM operation_log`).Scan(&count)
	return count, err
}

func (s *SQLiteStore) PurgeOperationLog(ctx context.Context, cfg TelemetryPurgeConfig) (int64, error) {
	if err := s.ensureOperationLog(ctx); err != nil {
		return 0, err
	}
	var totalDeleted int64
	if cfg.RetentionDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -cfg.RetentionDays).UnixMilli()
		res, err := s.db.ExecContext(ctx, `DELETE FROM operation_log WHERE created_at < ?`, cutoff)
		if err != nil {
			return totalDeleted, err
		}
		deleted, err := res.RowsAffected()
		if err != nil {
			return totalDeleted, err
		}
		totalDeleted += deleted
	}
	if cfg.MaxRows > 0 {
		res, err := s.db.ExecContext(ctx, `
DELETE FROM operation_log
WHERE id IN (
	SELECT id FROM operation_log
	ORDER BY id DESC
	LIMIT -1 OFFSET ?
)
`, cfg.MaxRows)
		if err != nil {
			return totalDeleted, err
		}
		deleted, err := res.RowsAffected()
		if err != nil {
			return totalDeleted, err
		}
		totalDeleted += deleted
	}
	if totalDeleted > 0 {
		if _, err := s.db.ExecContext(ctx, `PRAGMA incremental_vacuum`); err != nil {
			return totalDeleted, err
		}
	}
	return totalDeleted, nil
}

func (s *SQLiteStore) SummarizeOperations(ctx context.Context) (OperationSummary, error) {
	if err := s.ensureOperationLog(ctx); err != nil {
		return OperationSummary{}, err
	}
	var out OperationSummary
	var avg sql.NullFloat64
	var latest sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*),
	SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END),
	AVG(duration_ms),
	MAX(created_at),
	SUM(CASE WHEN operation = 'proxy_invoke' AND json_extract(metadata_json, '$.cached') = 1 THEN 1 ELSE 0 END),
	SUM(CASE WHEN operation = 'proxy_invoke' AND json_extract(metadata_json, '$.cached') = 0 THEN 1 ELSE 0 END),
	SUM(CASE WHEN operation = 'reindex' THEN 1 ELSE 0 END),
	SUM(CASE WHEN operation = 'search' THEN 1 ELSE 0 END),
	SUM(CASE WHEN operation = 'proxy_invoke' THEN 1 ELSE 0 END),
	SUM(CASE WHEN operation = 'vector_query' THEN 1 ELSE 0 END),
	SUM(CASE WHEN operation = 'embed' THEN 1 ELSE 0 END)
FROM operation_log
`).Scan(
		&out.Count,
		&out.ErrorCount,
		&avg,
		&latest,
		&out.CacheHits,
		&out.CacheMisses,
		&out.ReindexCount,
		&out.SearchCount,
		&out.ProxyCount,
		&out.VectorCount,
		&out.EmbedCount,
	)
	if err != nil {
		return OperationSummary{}, err
	}
	if avg.Valid {
		out.AverageMS = avg.Float64
	}
	if latest.Valid {
		ts := time.UnixMilli(latest.Int64).UTC()
		out.LatestAt = &ts
	}
	classes, err := s.errorClassCounts(ctx, "")
	if err != nil {
		return OperationSummary{}, err
	}
	out.ErrorClasses = classes
	return out, nil
}

func (s *SQLiteStore) SearchTimeline(ctx context.Context, bucketMinutes int, limit int) ([]SearchTimelinePoint, error) {
	if err := s.ensureOperationLog(ctx); err != nil {
		return nil, err
	}
	if bucketMinutes <= 0 {
		bucketMinutes = 60
	}
	if limit <= 0 {
		limit = 24
	}
	bucketMS := int64(bucketMinutes) * int64(time.Minute/time.Millisecond)
	rows, err := s.db.QueryContext(ctx, `
SELECT (created_at / ?) * ? AS bucket_start,
	COUNT(*),
	AVG(duration_ms),
	SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END)
FROM operation_log
WHERE operation = 'search'
GROUP BY bucket_start
ORDER BY bucket_start DESC
LIMIT ?
`, bucketMS, bucketMS, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SearchTimelinePoint
	for rows.Next() {
		var p SearchTimelinePoint
		var bucketStart int64
		if err := rows.Scan(&bucketStart, &p.Count, &p.AverageMS, &p.Errors); err != nil {
			return nil, err
		}
		p.BucketStart = time.UnixMilli(bucketStart).UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BucketStart.Before(out[j].BucketStart) })
	return out, nil
}

func (s *SQLiteStore) SourceOperationSummaries(ctx context.Context) ([]SourceOperationStats, error) {
	if err := s.ensureOperationLog(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT source_id,
	COUNT(*),
	SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END),
	AVG(duration_ms),
	MAX(created_at)
FROM operation_log
WHERE COALESCE(source_id, '') <> ''
GROUP BY source_id
ORDER BY source_id
`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SourceOperationStats
	for rows.Next() {
		var stat SourceOperationStats
		var latest sql.NullInt64
		if err := rows.Scan(&stat.SourceID, &stat.Count, &stat.ErrorCount, &stat.AverageMS, &latest); err != nil {
			return nil, err
		}
		if latest.Valid {
			ts := time.UnixMilli(latest.Int64).UTC()
			stat.LatestAt = &ts
		}
		latestErr, latestErrAt, err := s.latestSourceError(ctx, stat.SourceID)
		if err != nil {
			return nil, err
		}
		stat.LatestError = latestErr
		if latestErrAt != nil {
			stat.LatestAt = latestErrAt
		}
		classes, err := s.errorClassCounts(ctx, stat.SourceID)
		if err != nil {
			return nil, err
		}
		stat.ErrorClasses = classes
		h, ok, err := s.GetSourceHealth(ctx, stat.SourceID)
		if err != nil {
			return nil, err
		}
		if ok {
			okCopy := h.OK
			stat.LastReindexOK = &okCopy
			stat.LastReindexMessage = h.Message
			ts := h.UpdatedAt.UTC()
			stat.LastReindexAt = &ts
		}
		out = append(out, stat)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) latestSourceError(ctx context.Context, sourceID string) (string, *time.Time, error) {
	var errText sql.NullString
	var ts sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT error, created_at
FROM operation_log
WHERE source_id = ? AND error <> ''
ORDER BY id DESC
LIMIT 1
`, sourceID).Scan(&errText, &ts)
	if err == sql.ErrNoRows {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if !ts.Valid {
		return errText.String, nil, nil
	}
	tm := time.UnixMilli(ts.Int64).UTC()
	return errText.String, &tm, nil
}

func (s *SQLiteStore) errorClassCounts(ctx context.Context, sourceID string) (map[string]int64, error) {
	query := `
SELECT COALESCE(json_extract(metadata_json, '$.error_class'), ''), COUNT(*)
FROM operation_log
WHERE error <> ''`
	args := []any{}
	if sourceID != "" {
		query += ` AND source_id = ?`
		args = append(args, sourceID)
	}
	query += ` GROUP BY COALESCE(json_extract(metadata_json, '$.error_class'), '')`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int64{}
	for rows.Next() {
		var class string
		var count int64
		if err := rows.Scan(&class, &count); err != nil {
			return nil, err
		}
		if class == "" {
			class = "unknown"
		}
		out[class] = count
	}
	return out, rows.Err()
}

func nullIfEmpty(v string) any {
	if v == "" {
		return sql.NullString{}
	}
	return v
}
