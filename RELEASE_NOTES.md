# Release Notes

## v1.0.7

### Highlights

- Batched Ollama embeddings with legacy fallback for older Ollama versions.
- Local telemetry foundation with operation logging, retention, and purge lifecycle.
- Hot-path duration tracking for search, vector queries, embedding batches, proxy calls, and per-source reindex.
- New local observability endpoints: `/stats`, `/stats/search`, `/stats/sources`, and `/cache/stats`.
- Embedding retry with configurable exponential backoff.
- Persistent circuit-breaker state across restarts.
- Search-quality improvements:
  - deterministic vagueness scoring
  - schema-derived enrichment and tags
  - configurable embedding text strategy
  - optional `exec`-based vague-tool refinement during reindex
- Upstream proxy error classification in telemetry and source stats.
- Reindex dry-run now reports per-record `NEW`, `UPDATED`, and `STALE` changes.

### Configuration Additions

- `telemetry.retention_days`
- `telemetry.purge_interval_hours`
- `telemetry.max_rows`
- `embeddings.text_strategy`
- `embeddings.retry_attempts`
- `embeddings.retry_backoff_ms`
- `summary.provider: exec`
- `summary.command`
- `summary.args`
- `summary.timeout_seconds`
- `summary.auto_refine`
- `summary.vagueness_threshold`
- `summary.schema_enrichment`

### Operational Notes

- Release artifacts are published as `lazy-tool-x` for Linux, macOS, and Windows.
- Documentation was updated for the new config fields, stats endpoints, refinement workflow, and dry-run diff output.
