package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lazy-tool/internal/app"
	"lazy-tool/internal/cache"
	"lazy-tool/internal/config"
	"lazy-tool/internal/connectors"
	"lazy-tool/internal/embeddings"
	"lazy-tool/internal/search"
	"lazy-tool/internal/storage"
	"lazy-tool/internal/summarizer"
	"lazy-tool/internal/tracing"
	"lazy-tool/internal/vector"
)

type Stack struct {
	Cfg        *config.Config
	Store      *storage.SQLiteStore
	Vec        *vector.Index
	Registry   *app.SourceRegistry
	Factory    connectors.Factory
	Search     *search.Service
	Summarizer summarizer.Summarizer
	Embedder   embeddings.Embedder
	Cache      *cache.Cache // nil when caching is disabled
	telemetryStop func()
}

func OpenStack(cfgPath string) (*Stack, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	if cfg.Storage.SQLitePath == "" {
		return nil, fmt.Errorf("storage.sqlite_path is required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Storage.SQLitePath), 0o755); err != nil {
		return nil, err
	}
	vecPath := cfg.Storage.VectorPath
	if vecPath == "" {
		vecPath = filepath.Join(filepath.Dir(cfg.Storage.SQLitePath), "vector")
	}
	if err := os.MkdirAll(vecPath, 0o755); err != nil {
		return nil, err
	}
	st, err := storage.OpenSQLite(cfg.Storage.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("sqlite: %w", err)
	}
	// Enable invocation stats persistence
	tracing.SetPersister(st)
	vi, err := vector.Open(vecPath)
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("vector: %w", err)
	}
	srcs, err := config.NormalizeSources(cfg.Sources)
	if err != nil {
		_ = vi.Close()
		_ = st.Close()
		return nil, err
	}
	reg, err := app.NewSourceRegistry(srcs)
	if err != nil {
		_ = vi.Close()
		_ = st.Close()
		return nil, err
	}
	cbCooldown := time.Duration(cfg.Connectors.CircuitBreakerCooldownSeconds) * time.Second
	if cbCooldown <= 0 && cfg.Connectors.CircuitBreakerMaxFailures > 0 {
		cbCooldown = 30 * time.Second
	}
	fact := connectors.NewFactory(connectors.FactoryOpts{
		HTTPReuseUpstreamSession: cfg.Connectors.HTTPReuseUpstreamSession,
		HTTPReuseIdleTimeout:     time.Duration(cfg.Connectors.HTTPReuseIdleTimeoutSeconds) * time.Second,
		CircuitBreaker: connectors.CircuitBreakerOpts{
			MaxFailures:  cfg.Connectors.CircuitBreakerMaxFailures,
			OpenDuration: cbCooldown,
		},
	})
	sum := summarizer.New(cfg)
	emb := embeddings.New(cfg)
	svc := search.NewService(st, vi, emb, search.ScoreWeights{
		ExactCanonical:   cfg.Search.Scoring.ExactCanonical,
		ExactName:        cfg.Search.Scoring.ExactName,
		Substring:        cfg.Search.Scoring.Substring,
		VectorMultiplier: cfg.Search.Scoring.VectorMultiplier,
		UserSummary:      cfg.Search.Scoring.UserSummary,
		Favorite:         cfg.Search.Scoring.Favorite,
		InvocationBoost:  cfg.Search.Scoring.InvocationBoost,
	}, cfg.Search.LexicalOnly)
	svc.FullCatalogSubstring = !cfg.Search.DisableFullCatalogSubstring
	svc.EmptyQueryIDBatch = cfg.Search.EmptyQueryIDBatch
	svc.EmptyQueryMaxCatalogIDs = cfg.Search.EmptyQueryMaxCatalogIDs

	var c *cache.Cache
	if cfg.Cache.Enabled {
		maxEntries := cfg.Cache.MaxEntries
		if maxEntries <= 0 {
			maxEntries = 500
		}
		ttl := time.Duration(cfg.Cache.TTLSeconds) * time.Second
		if ttl <= 0 {
			ttl = 5 * time.Minute
		}
		c = cache.New(maxEntries, ttl, cfg.Cache.ExcludeSources)
	}
	stopTelemetry := startTelemetryPurger(st, cfg)

	return &Stack{
		Cfg:        cfg,
		Store:      st,
		Vec:        vi,
		Registry:   reg,
		Factory:    fact,
		Search:     svc,
		Summarizer: sum,
		Embedder:   emb,
		Cache:      c,
		telemetryStop: stopTelemetry,
	}, nil
}

func (s *Stack) Close() error {
	var first error
	if s.Factory != nil {
		if err := s.Factory.Close(); err != nil {
			first = err
		}
	}
	if s.telemetryStop != nil {
		s.telemetryStop()
	}
	if s.Vec != nil {
		if err := s.Vec.Close(); err != nil && first == nil {
			first = err
		}
	}
	if s.Store != nil {
		if err := s.Store.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func startTelemetryPurger(st *storage.SQLiteStore, cfg *config.Config) func() {
	if st == nil {
		return nil
	}
	purgeCfg := storage.TelemetryPurgeConfig{
		RetentionDays: cfg.Telemetry.RetentionDays,
		MaxRows:       cfg.Telemetry.MaxRows,
	}
	if purgeCfg.RetentionDays <= 0 && purgeCfg.MaxRows <= 0 {
		return nil
	}
	_, _ = st.PurgeOperationLog(context.Background(), purgeCfg)
	if cfg.Telemetry.PurgeIntervalHours <= 0 {
		return nil
	}
	interval := time.Duration(cfg.Telemetry.PurgeIntervalHours) * time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = st.PurgeOperationLog(context.Background(), purgeCfg)
			}
		}
	}()
	return func() {
		once.Do(cancel)
	}
}
