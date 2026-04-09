package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// SourceHealthRow is last-known reindex outcome per configured source (P1.4).
type SourceHealthRow struct {
	SourceID            string
	OK                  bool
	Message             string
	UpdatedAt           time.Time
	CircuitState        string
	CircuitFailures     int
	CircuitLastFailedAt *time.Time
}

func (s *SQLiteStore) ensureSourceHealth(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS source_health (
			source_id TEXT PRIMARY KEY,
			ok INTEGER NOT NULL,
			message TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			circuit_state TEXT NOT NULL DEFAULT '',
			circuit_failures INTEGER NOT NULL DEFAULT 0,
			circuit_last_failed_at INTEGER NOT NULL DEFAULT 0
		)`,
		`ALTER TABLE source_health ADD COLUMN circuit_state TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE source_health ADD COLUMN circuit_failures INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE source_health ADD COLUMN circuit_last_failed_at INTEGER NOT NULL DEFAULT 0`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil &&
			!strings.Contains(err.Error(), "duplicate column name") &&
			!strings.Contains(err.Error(), "no such table") {
			return err
		}
	}
	return nil
}

// UpsertSourceHealth records the outcome of indexing one source (empty message when ok).
func (s *SQLiteStore) UpsertSourceHealth(ctx context.Context, sourceID string, ok bool, message string) error {
	if err := s.ensureSourceHealth(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO source_health(source_id, ok, message, updated_at) VALUES(?,?,?,?)
ON CONFLICT(source_id) DO UPDATE SET
	ok=excluded.ok,
	message=excluded.message,
	updated_at=excluded.updated_at
`, sourceID, boolAsInt(ok), message, time.Now().Unix())
	return err
}

func (s *SQLiteStore) UpdateCircuitState(ctx context.Context, sourceID string, state string, failures int, lastFailedAt *time.Time) error {
	if err := s.ensureSourceHealth(ctx); err != nil {
		return err
	}
	updatedAt := time.Now().Unix()
	lastFailedUnix := int64(0)
	if lastFailedAt != nil {
		lastFailedUnix = lastFailedAt.UTC().Unix()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO source_health(source_id, ok, message, updated_at, circuit_state, circuit_failures, circuit_last_failed_at)
VALUES(?, 1, '', ?, ?, ?, ?)
ON CONFLICT(source_id) DO UPDATE SET
	updated_at=excluded.updated_at,
	circuit_state=excluded.circuit_state,
	circuit_failures=excluded.circuit_failures,
	circuit_last_failed_at=excluded.circuit_last_failed_at
`, sourceID, updatedAt, state, failures, lastFailedUnix)
	return err
}

func boolAsInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GetSourceHealth returns the last-known reindex row for sourceID, if any.
func (s *SQLiteStore) GetSourceHealth(ctx context.Context, sourceID string) (SourceHealthRow, bool, error) {
	if err := s.ensureSourceHealth(ctx); err != nil {
		return SourceHealthRow{}, false, err
	}
	var r SourceHealthRow
	var ts int64
	var okInt int
	var circuitLastFailed int64
	err := s.db.QueryRowContext(ctx, `
SELECT source_id, ok, message, updated_at, circuit_state, circuit_failures, circuit_last_failed_at FROM source_health WHERE source_id = ?`, sourceID).
		Scan(&r.SourceID, &okInt, &r.Message, &ts, &r.CircuitState, &r.CircuitFailures, &circuitLastFailed)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceHealthRow{}, false, nil
	}
	if err != nil {
		return SourceHealthRow{}, false, err
	}
	r.OK = okInt != 0
	r.UpdatedAt = time.Unix(ts, 0).UTC()
	if circuitLastFailed > 0 {
		tm := time.Unix(circuitLastFailed, 0).UTC()
		r.CircuitLastFailedAt = &tm
	}
	return r, true, nil
}

// ListSourceHealth returns persisted rows (may be empty before first reindex).
func (s *SQLiteStore) ListSourceHealth(ctx context.Context) ([]SourceHealthRow, error) {
	if err := s.ensureSourceHealth(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, ok, message, updated_at, circuit_state, circuit_failures, circuit_last_failed_at FROM source_health ORDER BY source_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SourceHealthRow
	for rows.Next() {
		var r SourceHealthRow
		var ts int64
		var okInt int
		var circuitLastFailed int64
		if err := rows.Scan(&r.SourceID, &okInt, &r.Message, &ts, &r.CircuitState, &r.CircuitFailures, &circuitLastFailed); err != nil {
			return nil, err
		}
		r.OK = okInt != 0
		r.UpdatedAt = time.Unix(ts, 0).UTC()
		if circuitLastFailed > 0 {
			tm := time.Unix(circuitLastFailed, 0).UTC()
			r.CircuitLastFailedAt = &tm
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
