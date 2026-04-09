package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

func nullIfEmpty(v string) any {
	if v == "" {
		return sql.NullString{}
	}
	return v
}
