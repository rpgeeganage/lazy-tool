package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"lazy-tool/pkg/models"
)

// FTS5 virtual table for lexical retrieval. Columns (left-to-right) bias BM25 toward identifiers and names.
// id and source_id are UNINDEXED (stored for lookup/filtering, not tokenized).
const ftsCreateSQL = `
CREATE VIRTUAL TABLE IF NOT EXISTS capabilities_fts USING fts5(
	id UNINDEXED,
	canonical_name,
	original_name,
	summary,
	source_id UNINDEXED,
	tags,
	search_text,
	tokenize = 'porter unicode61'
);
`

func effectiveSummary(rec models.CapabilityRecord) string {
	if strings.TrimSpace(rec.UserSummary) != "" {
		return strings.TrimSpace(rec.UserSummary)
	}
	if rec.GeneratedSummary != "" {
		return rec.GeneratedSummary
	}
	return ""
}

func tagsJoined(rec models.CapabilityRecord) string {
	return strings.Join(rec.Tags, " ")
}

// ftsTokenize splits a query into FTS-safe tokens (letters/digits runs, min length 2).
// Single-char tokens are dropped because they produce excessive FTS matches across the
// entire catalog without adding discriminative value. The FTS5 porter unicode61 tokenizer
// does index single-char tokens, but querying on them returns too many false positives.
// When ftsTokenize returns no tokens (e.g. single-letter query), BuildFTSMatchQuery returns
// "" and the search pipeline falls back to substring scan, which handles short queries fine.
func ftsTokenize(s string) []string {
	s = strings.ToLower(s)
	var cur strings.Builder
	var out []string
	flush := func() {
		if cur.Len() < 2 {
			cur.Reset()
			return
		}
		out = append(out, cur.String())
		cur.Reset()
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// ftsQuoteToken wraps a token in double quotes for MATCH; internal " doubled per SQLite FTS5 rules.
func ftsQuoteToken(t string) string {
	t = strings.ReplaceAll(t, `"`, `""`)
	return `"` + t + `"`
}

// BuildFTSMatchQuery returns an FTS5 MATCH string (token AND ...), or "" if nothing to match.
func BuildFTSMatchQuery(query string) string {
	toks := ftsTokenize(query)
	if len(toks) == 0 {
		return ""
	}
	parts := make([]string, 0, len(toks))
	for _, t := range toks {
		parts = append(parts, ftsQuoteToken(t))
	}
	return strings.Join(parts, " AND ")
}

func syncFTSRow(ctx context.Context, tx *sql.Tx, rec models.CapabilityRecord) error {
	tj := tagsJoined(rec)
	sum := effectiveSummary(rec)
	_, err := tx.ExecContext(ctx, `DELETE FROM capabilities_fts WHERE id=?`, rec.ID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO capabilities_fts(id, canonical_name, original_name, summary, source_id, tags, search_text)
VALUES(?,?,?,?,?,?,?)`,
		rec.ID, rec.CanonicalName, rec.OriginalName, sum, rec.SourceID, tj, rec.SearchText,
	)
	return err
}

func deleteFTSRow(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM capabilities_fts WHERE id=?`, id)
	return err
}

func (s *SQLiteStore) ensureFTS(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, ftsCreateSQL); err != nil {
		return err
	}
	return s.backfillMissingFTS(ctx)
}

func (s *SQLiteStore) backfillMissingFTS(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM capabilities WHERE id NOT IN (SELECT id FROM capabilities_fts)`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		rec, err := s.GetCapability(ctx, id)
		if err != nil {
			return err
		}
		if err := syncFTSRow(ctx, tx, rec); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SearchFTSCandidateIDs returns capability IDs ordered by BM25 (best first), filtered by source IDs when non-empty.
func (s *SQLiteStore) SearchFTSCandidateIDs(ctx context.Context, match string, sourceIDs []string, limit int) ([]string, error) {
	if match == "" || limit <= 0 {
		return nil, nil
	}
	var q string
	var args []any
	if len(sourceIDs) == 0 {
		q = `SELECT id FROM capabilities_fts WHERE capabilities_fts MATCH ? ORDER BY bm25(capabilities_fts) LIMIT ?`
		args = []any{match, limit}
	} else {
		ph := placeholders(len(sourceIDs))
		q = `SELECT id FROM capabilities_fts WHERE capabilities_fts MATCH ? AND source_id IN (` + ph + `) ORDER BY bm25(capabilities_fts) LIMIT ?`
		args = append([]any{match}, anySlice(sourceIDs)...)
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListIDsByOriginalNameFold returns capability IDs where lower(original_name) = lower(needle), optional source filter.
func (s *SQLiteStore) ListIDsByOriginalNameFold(ctx context.Context, needle string, sourceIDs []string) ([]string, error) {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return nil, nil
	}
	var q string
	var args []any
	if len(sourceIDs) == 0 {
		q = `SELECT id FROM capabilities WHERE lower(original_name) = lower(?)`
		args = []any{needle}
	} else {
		ph := placeholders(len(sourceIDs))
		q = `SELECT id FROM capabilities WHERE lower(original_name) = lower(?) AND source_id IN (` + ph + `)`
		args = append([]any{needle}, anySlice(sourceIDs)...)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := strings.Builder{}
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("?")
	}
	return b.String()
}

func anySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i := range ss {
		out[i] = ss[i]
	}
	return out
}

// GetCapabilitiesByIDs loads records for the given IDs; missing IDs are omitted.
func (s *SQLiteStore) GetCapabilitiesByIDs(ctx context.Context, ids []string) (map[string]models.CapabilityRecord, error) {
	if len(ids) == 0 {
		return map[string]models.CapabilityRecord{}, nil
	}
	ph := placeholders(len(ids))
	q := `SELECT id, kind, source_id, source_type, canonical_name, original_name, original_description,
	generated_summary, user_summary, search_text, input_schema_json, metadata_json, tags_json,
	embedding_model, embedding_vector, embedding_text_hash, version_hash, last_seen_at
FROM capabilities WHERE id IN (` + ph + `)`
	args := anySlice(ids)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]models.CapabilityRecord, len(ids))
	for rows.Next() {
		var rec models.CapabilityRecord
		var tagsJSON string
		var last int64
		var emb []byte
		if err := rows.Scan(
			&rec.ID, &rec.Kind, &rec.SourceID, &rec.SourceType, &rec.CanonicalName,
			&rec.OriginalName, &rec.OriginalDescription, &rec.GeneratedSummary, &rec.UserSummary, &rec.SearchText,
			&rec.InputSchemaJSON, &rec.MetadataJSON, &tagsJSON, &rec.EmbeddingModel, &emb,
			&rec.EmbeddingTextHash, &rec.VersionHash, &last,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &rec.Tags)
		rec.EmbeddingVector = decodeFloat32(emb)
		rec.LastSeenAt = time.Unix(last, 0)
		out[rec.ID] = rec
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
