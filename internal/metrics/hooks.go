// Package metrics exposes optional hooks for observability (spec §35).
package metrics

// ConnectorRetry is called when a connector operation will be retried.
var ConnectorRetry = func(sourceID string, attempt int, err error) {}

// ReindexSourceDone records completion of one source during reindex.
var ReindexSourceDone = func(sourceID string, toolCount int, staleRemoved int, err error) {}

// SearchExecuted records a completed search (result count after ranking cap).
var SearchExecuted = func(resultCount int) {}

// SearchCandidateGeneration records how candidates were gathered (Candidate B / part-3 search discipline).
// Mode values match pkg/models SearchCandidatePath* constants (including full_catalog_substring_disabled).
var SearchCandidateGeneration = func(mode string) {}

// McpToolCall records lazy-tool MCP tool invocations from the host.
var McpToolCall = func(toolName string, err error) {}

// UpstreamMCPConnect records the result of one upstream MCP client Connect (part-3 session visibility).
// err is nil when the handshake succeeded.
var UpstreamMCPConnect = func(sourceID string, err error) {}

// UpstreamMCPSessionClosed records Close on the upstream session (after the handler returns).
// err is nil when Close succeeded.
var UpstreamMCPSessionClosed = func(sourceID string, err error) {}

// UpstreamMCPIdleSessionRecycled is called when a reused HTTP session is closed because
// http_reuse_idle_timeout_seconds elapsed (before reconnecting on the next request).
var UpstreamMCPIdleSessionRecycled = func(sourceID string) {}

// SearchEmptyQueryScan reports empty-query ('' needle) catalog scan shape: total IDs in DB, IDs processed after optional cap, truncated.
var SearchEmptyQueryScan = func(totalIDs, processedIDs int, truncated bool) {}

// CircuitBreakerTripped is called when a source's circuit breaker transitions to open.
var CircuitBreakerTripped = func(sourceID string) {}

// CircuitBreakerReset is called when a source's circuit breaker transitions from half-open back to closed.
var CircuitBreakerReset = func(sourceID string) {}

// PassthroughFallbackActivated is called when passthrough fallback returns results (zero search results triggered it).
var PassthroughFallbackActivated = func(resultCount int) {}
