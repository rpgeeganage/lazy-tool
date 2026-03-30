package connectors

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"lazy-tool/pkg/models"
)

type FactoryOpts struct {
	Timeout time.Duration
	// HTTPReuseUpstreamSession keeps one MCP session per source id for streamable HTTP transports (stdio unchanged).
	HTTPReuseUpstreamSession bool
	// HTTPReuseIdleTimeout closes the session after this duration elapses since the last successful request (sliding window). Zero disables.
	HTTPReuseIdleTimeout time.Duration
	// CircuitBreaker configures per-source circuit breaking. Zero MaxFailures disables.
	CircuitBreaker CircuitBreakerOpts
}

func NewFactory(opts FactoryOpts) Factory {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	return &factoryImpl{opts: opts}
}

type factoryImpl struct {
	opts       FactoryOpts
	holdersMu  sync.Mutex
	holders    map[string]*httpSessionHolder
	breakersMu sync.Mutex
	breakers   map[string]*CircuitBreaker
}

func (f *factoryImpl) New(ctx context.Context, src models.Source) (Connector, error) {
	_ = ctx
	if src.Adapter != "" && src.Adapter != "default" {
		return nil, fmt.Errorf("unsupported source adapter %q", src.Adapter)
	}
	hc := &http.Client{Timeout: f.opts.Timeout}
	var reuse httpSessionRunner
	if f.opts.HTTPReuseUpstreamSession && src.Transport == models.TransportHTTP {
		reuse = f.runnerFor(src, hc)
	}
	bc := &baseConnector{src: src, httpClient: hc, httpReuse: reuse}
	switch src.Type {
	case models.SourceTypeGateway, models.SourceTypeServer:
		return bc, nil
	default:
		return nil, fmt.Errorf("unknown source type %q", src.Type)
	}
}

func (f *factoryImpl) runnerFor(src models.Source, hc *http.Client) httpSessionRunner {
	f.holdersMu.Lock()
	defer f.holdersMu.Unlock()
	if f.holders == nil {
		f.holders = make(map[string]*httpSessionHolder)
	}
	if h, ok := f.holders[src.ID]; ok {
		return h
	}
	h := &httpSessionHolder{src: src, hc: hc, idleTTL: f.opts.HTTPReuseIdleTimeout}
	f.holders[src.ID] = h
	return h
}

func (f *factoryImpl) CircuitBreakerFor(sourceID string) *CircuitBreaker {
	if f.opts.CircuitBreaker.MaxFailures <= 0 {
		return nil
	}
	f.breakersMu.Lock()
	defer f.breakersMu.Unlock()
	if f.breakers == nil {
		f.breakers = make(map[string]*CircuitBreaker)
	}
	if cb, ok := f.breakers[sourceID]; ok {
		return cb
	}
	cb := NewCircuitBreaker(f.opts.CircuitBreaker)
	f.breakers[sourceID] = cb
	return cb
}

func (f *factoryImpl) Close() error {
	f.holdersMu.Lock()
	defer f.holdersMu.Unlock()
	for _, h := range f.holders {
		h.close()
	}
	f.holders = nil
	return nil
}
