package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"lazy-tool/internal/app"
	"lazy-tool/internal/catalog"
	"lazy-tool/internal/mcpserver"
	"lazy-tool/internal/runtime"
	"lazy-tool/internal/storage"
	"lazy-tool/internal/tracing"
	"lazy-tool/pkg/models"
)

// searchJSON mirrors mcpserver search output for HTML rendering.
type searchJSONWire struct {
	CandidatePath string `json:"candidate_path,omitempty"`
	Results       []struct {
		Kind          string   `json:"kind"`
		ProxyToolName string   `json:"proxy_tool_name"`
		SourceID      string   `json:"source_id"`
		Summary       string   `json:"summary"`
		Score         float64  `json:"score"`
		WhyMatched    []string `json:"why_matched"`
	} `json:"results"`
}

type searchRowView struct {
	Kind, ProxyName, SourceID, Summary string
	Score                              float64
	Why                                string
}

type searchPageData struct {
	Query         string
	Error         string
	HasResults    bool
	Rows          []searchRowView
	CandidatePath string
}

type inspectPageData struct {
	Name          string
	JSON          string
	UserSummary   string
	SummarySaved  bool
	RecordPresent bool
}

func ListenAndServe(addr string, stack *runtime.Stack) error {
	addr = NormalizeListenAddr(addr)
	mux := newMux(stack)
	return http.ListenAndServe(addr, mux)
}

func newMux(stack *runtime.Stack) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = rootTmpl.ExecuteTemplate(w, "home", nil)
	})
	mux.HandleFunc("/sources", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("health") == "1" {
			rows, err := stack.Store.ListSourceHealth(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			byID := make(map[string]storage.SourceHealthRow)
			for _, row := range rows {
				byID[row.SourceID] = row
			}
			type srcHealth struct {
				models.Source      `json:",inline"`
				LastReindexOK      *bool   `json:"last_reindex_ok,omitempty"`
				LastReindexMessage string  `json:"last_reindex_message,omitempty"`
				LastReindexAt      *string `json:"last_reindex_at,omitempty"`
			}
			out := make([]srcHealth, 0, len(stack.Registry.All()))
			for _, src := range stack.Registry.All() {
				sh := srcHealth{Source: src}
				if h, ok := byID[src.ID]; ok {
					okCopy := h.OK
					sh.LastReindexOK = &okCopy
					sh.LastReindexMessage = h.Message
					s := h.UpdatedAt.UTC().Format(time.RFC3339)
					sh.LastReindexAt = &s
				}
				out = append(out, sh)
			}
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		_ = json.NewEncoder(w).Encode(stack.Registry.All())
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			_ = rootTmpl.ExecuteTemplate(w, "search", searchPageData{})
			return
		}
		opts := &mcpserver.SearchCallOpts{
			GroupBySource: r.URL.Query().Get("group") == "1",
			LexicalOnly:   r.URL.Query().Get("lexical") == "1",
			ExplainScores: r.URL.Query().Get("explain") == "1",
		}
		b, err := mcpserver.SearchToolsResultJSON(r.Context(), stack, q, 20, nil, opts)
		if err != nil {
			_ = rootTmpl.ExecuteTemplate(w, "search", searchPageData{Query: q, Error: err.Error()})
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
			return
		}
		var wire searchJSONWire
		if err := json.Unmarshal(b, &wire); err != nil {
			_ = rootTmpl.ExecuteTemplate(w, "search", searchPageData{Query: q, Error: err.Error()})
			return
		}
		rows := make([]searchRowView, 0, len(wire.Results))
		for _, r0 := range wire.Results {
			why := strings.Join(r0.WhyMatched, ", ")
			rows = append(rows, searchRowView{
				Kind:      r0.Kind,
				ProxyName: r0.ProxyToolName,
				SourceID:  r0.SourceID,
				Summary:   r0.Summary,
				Score:     r0.Score,
				Why:       why,
			})
		}
		_ = rootTmpl.ExecuteTemplate(w, "search", searchPageData{
			Query:         q,
			HasResults:    len(rows) > 0,
			Rows:          rows,
			CandidatePath: wire.CandidatePath,
		})
	})
	mux.HandleFunc("/inspect/summary", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.PostFormValue("canonical_name"))
		if name == "" {
			http.Error(w, "canonical_name required", http.StatusBadRequest)
			return
		}
		text := strings.TrimSpace(r.PostFormValue("user_summary"))
		if err := catalog.SetUserSummary(r.Context(), stack.Store, name, text); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/inspect?name="+url.QueryEscape(name)+"&summary_saved=1", http.StatusSeeOther)
	})
	mux.HandleFunc("/inspect", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		data := inspectPageData{Name: name, SummarySaved: r.URL.Query().Get("summary_saved") == "1"}
		if name != "" {
			v, err := catalog.BuildInspectView(r.Context(), stack.Store, stack.Registry, name)
			if err != nil {
				data.JSON = err.Error()
			} else {
				data.RecordPresent = true
				data.UserSummary = v.Record.UserSummary
				b, err := json.MarshalIndent(v, "", "  ")
				if err != nil {
					data.JSON = err.Error()
					data.RecordPresent = false
				} else {
					data.JSON = string(b)
				}
			}
		}
		if strings.Contains(r.Header.Get("Accept"), "application/json") && name != "" {
			v, err := catalog.BuildInspectView(r.Context(), stack.Store, stack.Registry, name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
			return
		}
		_ = rootTmpl.ExecuteTemplate(w, "inspect", data)
	})
	mux.HandleFunc("/capabilities", func(w http.ResponseWriter, r *http.Request) {
		all, err := stack.Store.ListAll(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(all)
	})
	mux.HandleFunc("/reindex", func(w http.ResponseWriter, r *http.Request) {
		at, ok, msg := app.GetReindexStatus()
		lastAt := at.Format(time.RFC3339)
		if at.IsZero() {
			lastAt = "never"
		}
		lastOK := "ok"
		if !ok {
			lastOK = "error"
		}
		if r.Method != http.MethodPost {
			_ = rootTmpl.ExecuteTemplate(w, "reindex", map[string]any{
				"Msg":     "POST runs a full reindex (upstream MCP, optional LLM/embed APIs).",
				"LastAt":  lastAt,
				"LastOK":  lastOK,
				"LastMsg": msg,
			})
			return
		}
		ix := &catalog.Indexer{
			Registry: stack.Registry,
			Factory:  stack.Factory,
			Summary:  stack.Summarizer,
			Embed:    stack.Embedder,
			EmbeddingTextStrategy: stack.Cfg.Embeddings.TextStrategy,
			AutoRefineVague: stack.Cfg.Summary.AutoRefine,
			VaguenessThreshold: stack.Cfg.Summary.VaguenessThreshold,
			SchemaEnrichment: stack.Cfg.Summary.SchemaEnrichment,
			Store:    stack.Store,
			Vec:      stack.Vec,
			Log:      slog.Default(),
		}
		if err := ix.Run(context.Background()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/settings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stack.Cfg)
	})
	mux.HandleFunc("/traces", func(w http.ResponseWriter, r *http.Request) {
		b, err := tracing.SnapshotJSON()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		summary, err := stack.Store.SummarizeOperations(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cacheStats := map[string]any{"enabled": stack.Cache != nil}
		if stack.Cache != nil {
			hits, misses, size := stack.Cache.Stats()
			hitRate := 0.0
			if total := hits + misses; total > 0 {
				hitRate = float64(hits) / float64(total)
			}
			cacheStats = map[string]any{
				"enabled":   true,
				"hits":      hits,
				"misses":    misses,
				"size":      size,
				"max":       stack.Cache.MaxEntries(),
				"hit_rate":  hitRate,
			}
		}
		out := map[string]any{
			"operations": summary,
			"cache":      cacheStats,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/stats/search", func(w http.ResponseWriter, r *http.Request) {
		bucketMinutes := 60
		limit := 24
		points, err := stack.Store.SearchTimeline(r.Context(), bucketMinutes, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bucket_minutes": bucketMinutes,
			"points":         points,
		})
	})
	mux.HandleFunc("/stats/sources", func(w http.ResponseWriter, r *http.Request) {
		stats, err := stack.Store.SourceOperationSummaries(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	})
	mux.HandleFunc("/cache/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if stack.Cache == nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"enabled":  false,
				"hits":     0,
				"misses":   0,
				"size":     0,
				"max":      0,
				"hit_rate": 0.0,
			})
			return
		}
		hits, misses, size := stack.Cache.Stats()
		hitRate := 0.0
		if total := hits + misses; total > 0 {
			hitRate = float64(hits) / float64(total)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled":  true,
			"hits":     hits,
			"misses":   misses,
			"size":     size,
			"max":      stack.Cache.MaxEntries(),
			"hit_rate": hitRate,
		})
	})
	return mux
}

const defaultWebListenPort = "8765"

// NormalizeListenAddr returns a safe listen address for the local web UI.
func NormalizeListenAddr(addr string) string {
	if addr == "" {
		return net.JoinHostPort("127.0.0.1", defaultWebListenPort)
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, defaultWebListenPort)
}
