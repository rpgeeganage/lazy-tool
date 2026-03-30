package tracing

import (
	"context"
	"log/slog"
	"time"
)

// InvocationPersister can persist invocation stats (e.g. SQLiteStore).
// nil means ring-buffer-only mode (used in tests).
type InvocationPersister interface {
	RecordInvocation(ctx context.Context, canonicalName string, ok bool) error
}

// store is an optional persister set via SetPersister.
var store InvocationPersister

// SetPersister configures the optional persistence backend for invocation stats.
// Pass nil to disable persistence (ring-buffer-only mode).
func SetPersister(p InvocationPersister) {
	store = p
}

func LogInvocation(ctx context.Context, log *slog.Logger, proxyName, sourceID, tool string, err error) {
	_ = ctx
	if log == nil {
		log = slog.Default()
	}
	inv := Invocation{
		Time:      time.Now(),
		ProxyName: proxyName,
		SourceID:  sourceID,
		Tool:      tool,
		OK:        err == nil,
	}
	if err != nil {
		inv.Error = err.Error()
		log.Info("tool_invoke", "proxy", proxyName, "source", sourceID, "tool", tool, "err", err)
	} else {
		log.Info("tool_invoke", "proxy", proxyName, "source", sourceID, "tool", tool, "ok", true)
	}
	AppendInvocation(inv)

	// Persist invocation stats if a store is configured
	if store != nil && proxyName != "" {
		if persistErr := store.RecordInvocation(ctx, proxyName, err == nil); persistErr != nil {
			log.Warn("invocation_stats_persist_failed", "err", persistErr)
		}
	}
}
