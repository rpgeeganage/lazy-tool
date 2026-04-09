package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"lazy-tool/internal/app"
	"lazy-tool/internal/cache"
	"lazy-tool/internal/runtime"
	"lazy-tool/internal/storage"
	"lazy-tool/pkg/models"
)

func TestStatsEndpoints(t *testing.T) {
	ctx := context.Background()
	st, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "web-stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	reg, err := app.NewSourceRegistry([]models.Source{{ID: "src1", Type: models.SourceTypeGateway, Transport: models.TransportHTTP}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordOperation(ctx, storage.OperationLogEvent{Operation: "search", DurationMS: 11, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordOperation(ctx, storage.OperationLogEvent{Operation: "proxy_invoke", SourceID: "src1", DurationMS: 7, Metadata: map[string]any{"cached": true}, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSourceHealth(ctx, "src1", true, "ok"); err != nil {
		t.Fatal(err)
	}
	c := cache.New(10, time.Minute, nil)
	c.Put("k", []byte("v"))
	_, _ = c.Get("k")
	_, _ = c.Get("missing")
	stack := &runtime.Stack{Store: st, Registry: reg, Cache: c}
	mux := newMux(stack)

	t.Run("stats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/stats", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if _, ok := out["operations"]; !ok {
			t.Fatal("missing operations summary")
		}
		if _, ok := out["cache"]; !ok {
			t.Fatal("missing cache summary")
		}
	})

	t.Run("search timeline", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/stats/search", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("source stats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/stats/sources", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
		}
		var out []map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out) != 1 {
			t.Fatalf("sources = %d want 1", len(out))
		}
	})

	t.Run("cache stats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/cache/stats", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out["enabled"] != true {
			t.Fatalf("expected enabled cache stats: %#v", out)
		}
	})
}
