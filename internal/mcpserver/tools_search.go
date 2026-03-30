package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"lazy-tool/internal/config"
	"lazy-tool/internal/metrics"
	"lazy-tool/internal/runtime"
	"lazy-tool/internal/search"
	"lazy-tool/pkg/models"
)

type searchToolsArgs struct {
	Query         string   `json:"query" jsonschema:"natural language query"`
	Limit         int      `json:"limit,omitempty" jsonschema:"max results (default 10)"`
	SourceIDs     []string `json:"source_ids,omitempty" jsonschema:"filter to these upstream source ids"`
	GroupBySource bool     `json:"group_by_source,omitempty" jsonschema:"group hits by upstream source id"`
	LexicalOnly   bool     `json:"lexical_only,omitempty" jsonschema:"skip embeddings and vector retrieval for this query"`
	ExplainScores bool     `json:"explain_scores,omitempty" jsonschema:"include pre-ranker score_breakdown per hit"`
}

// SearchCallOpts configures CLI/Web search (P2.3/P2.4).
type SearchCallOpts struct {
	GroupBySource bool
	LexicalOnly   bool
	ExplainScores bool
}

type anthropicBlock struct {
	SuggestedToolUse *anthropicToolUse `json:"suggested_tool_use,omitempty"`
}

// anthropicToolUse mirrors a Claude-style tool_use block for lazy-tool proxy MCP tools.
type anthropicToolUse struct {
	Type  string         `json:"type"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type searchHit struct {
	Kind                string             `json:"kind,omitempty"`
	ProxyToolName       string             `json:"proxy_tool_name"`
	SourceID            string             `json:"source_id"`
	Summary             string             `json:"summary"`
	Score               float64            `json:"score"`
	ScoreBreakdown      map[string]float64 `json:"score_breakdown,omitempty"`
	WhyMatched          []string           `json:"why_matched"`
	NextToolName        string          `json:"next_tool_name,omitempty"`
	NextInputExample    map[string]any  `json:"next_input_example,omitempty"`
	RequiredInputFields []string        `json:"required_input_fields,omitempty"`
	NextStep            string          `json:"next_step,omitempty"`
	Anthropic           *anthropicBlock `json:"anthropic,omitempty"`
}

func anthropicHint(kindStr, proxy string) *anthropicBlock {
	var in map[string]any
	switch models.CapabilityKind(kindStr) {
	case models.CapabilityKindTool:
		in = map[string]any{"proxy_tool_name": proxy, "input": map[string]any{}}
		return &anthropicBlock{SuggestedToolUse: &anthropicToolUse{Type: "tool_use", Name: "invoke_proxy_tool", Input: in}}
	case models.CapabilityKindPrompt:
		in = map[string]any{"proxy_tool_name": proxy, "arguments": map[string]any{}}
		return &anthropicBlock{SuggestedToolUse: &anthropicToolUse{Type: "tool_use", Name: "get_proxy_prompt", Input: in}}
	case models.CapabilityKindResource:
		in = map[string]any{"proxy_tool_name": proxy}
		return &anthropicBlock{SuggestedToolUse: &anthropicToolUse{Type: "tool_use", Name: "read_proxy_resource", Input: in}}
	default:
		return nil
	}
}

func nextStepHint(kindStr, proxy string) (string, map[string]any, string) {
	switch models.CapabilityKind(kindStr) {
	case models.CapabilityKindTool:
		return "invoke_proxy_tool",
			map[string]any{"proxy_tool_name": proxy, "input": map[string]any{}},
			"Call invoke_proxy_tool with the exact proxy_tool_name above. Put upstream arguments inside input. Do not invent proxy_tool_name values."
	case models.CapabilityKindPrompt:
		return "get_proxy_prompt",
			map[string]any{"proxy_tool_name": proxy, "arguments": map[string]any{}},
			"Call get_proxy_prompt with the exact proxy_tool_name above. Put prompt arguments inside arguments."
	case models.CapabilityKindResource:
		return "read_proxy_resource",
			map[string]any{"proxy_tool_name": proxy},
			"Call read_proxy_resource with the exact proxy_tool_name above. Do not pass a raw file path."
	default:
		return "", nil, ""
	}
}

type jsonSchema struct {
	Type       string                     `json:"type"`
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
}

type jsonSchemaProp struct {
	Type   string `json:"type"`
	Format string `json:"format"`
	Items  any    `json:"items"`
	Enum   []any  `json:"enum"`
}

func exampleValueForField(name string, prop jsonSchemaProp) any {
	lname := strings.ToLower(name)
	if len(prop.Enum) > 0 {
		return prop.Enum[0]
	}
	switch prop.Type {
	case "string":
		switch {
		case strings.Contains(lname, "message"), strings.Contains(lname, "text"), strings.Contains(lname, "echo"):
			return "Hello World"
		case strings.Contains(lname, "query"), strings.Contains(lname, "search"):
			return "echo"
		case strings.Contains(lname, "path"), strings.Contains(lname, "file"):
			return "/tmp/example.txt"
		case strings.Contains(lname, "uri"), strings.Contains(lname, "url"):
			return "file:///tmp/example.txt"
		case strings.Contains(lname, "name"), strings.Contains(lname, "title"):
			return "example"
		default:
			return "example"
		}
	case "integer", "number":
		return 1
	case "boolean":
		return true
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	default:
		return "example"
	}
}

func findPropertyKeyCaseInsensitive(props map[string]json.RawMessage, want string) string {
	for k := range props {
		if strings.EqualFold(k, want) {
			return k
		}
	}
	return ""
}

func nextInputExampleFromSchema(proxy string, schemaJSON string) (map[string]any, []string) {
	out := map[string]any{"proxy_tool_name": proxy}
	if strings.TrimSpace(schemaJSON) == "" || strings.TrimSpace(schemaJSON) == "{}" {
		out["input"] = map[string]any{}
		return out, nil
	}

	var sch jsonSchema
	if err := json.Unmarshal([]byte(schemaJSON), &sch); err != nil || len(sch.Properties) == 0 {
		out["input"] = map[string]any{}
		return out, nil
	}

	selected := make([]string, 0, 4)
	if len(sch.Required) > 0 {
		for _, name := range sch.Required {
			if key := findPropertyKeyCaseInsensitive(sch.Properties, name); key != "" {
				selected = append(selected, key)
			}
		}
	} else {
		preferred := []string{"message", "text", "query", "prompt", "input", "name", "title", "path", "file", "uri", "url"}
		for _, want := range preferred {
			if key := findPropertyKeyCaseInsensitive(sch.Properties, want); key != "" {
				selected = append(selected, key)
				break
			}
		}
		if len(selected) == 0 {
			for key := range sch.Properties {
				selected = append(selected, key)
				break
			}
		}
	}

	input := map[string]any{}
	for _, key := range selected {
		var prop jsonSchemaProp
		_ = json.Unmarshal(sch.Properties[key], &prop)
		input[key] = exampleValueForField(key, prop)
	}

	out["input"] = input
	return out, selected
}

func searchHitsFromRanked(stack *runtime.Stack, ranked models.RankedResults) []searchHit {
	anth := stack.Cfg != nil && stack.Cfg.Search.AnthropicToolRefs
	out := make([]searchHit, 0, len(ranked.Results))
	for _, r := range ranked.Results {
		schemaJSON := ""
		if stack != nil && stack.Store != nil {
			if r.CapabilityID != "" {
				if rec, err := stack.Store.GetCapability(context.Background(), r.CapabilityID); err == nil {
					schemaJSON = rec.InputSchemaJSON
				}
			}
			if schemaJSON == "" && r.ProxyToolName != "" {
				if rec, err := stack.Store.GetByCanonicalName(context.Background(), r.ProxyToolName); err == nil {
					schemaJSON = rec.InputSchemaJSON
				}
			}
		}

		nextTool, _, nextStep := nextStepHint(string(r.Kind), r.ProxyToolName)
		nextInput, requiredFields := nextInputExampleFromSchema(r.ProxyToolName, schemaJSON)
		if nextTool == "get_proxy_prompt" {
			nextInput = map[string]any{"proxy_tool_name": r.ProxyToolName, "arguments": map[string]any{}}
		}
		if nextTool == "read_proxy_resource" {
			nextInput = map[string]any{"proxy_tool_name": r.ProxyToolName}
		}

		h := searchHit{
			Kind:                string(r.Kind),
			ProxyToolName:       r.ProxyToolName,
			SourceID:            r.SourceID,
			Summary:             r.Summary,
			Score:               r.Score,
			ScoreBreakdown:      r.ScoreBreakdown,
			WhyMatched:          r.WhyMatched,
			NextToolName:        nextTool,
			NextInputExample:    nextInput,
			RequiredInputFields: requiredFields,
			NextStep:            nextStep,
		}
		if anth {
			h.Anthropic = anthropicHint(string(r.Kind), r.ProxyToolName)
		}
		out = append(out, h)
	}
	return out
}

type searchGroupWire struct {
	SourceID string      `json:"source_id"`
	Results  []searchHit `json:"results"`
}

type searchToolsOut struct {
	CandidatePath string            `json:"candidate_path,omitempty"`
	Results       []searchHit       `json:"results"`
	Grouped       []searchGroupWire `json:"grouped,omitempty"`
}

func registerSearchTools(server *mcp.Server, stack *runtime.Stack) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_tools",
		Description: "Search the local MCP catalog: tools, prompts, and resources (lexical FTS5 + optional vector boost). Then use next_tool_name with next_input_example from a result; do not invent proxy_tool_name values.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchToolsArgs) (*mcp.CallToolResult, searchToolsOut, error) {
		lim := in.Limit
		if lim <= 0 {
			lim = 10
		}
		ranked, err := runSearchPipeline(ctx, stack, in.Query, lim, in.SourceIDs, in.GroupBySource, in.LexicalOnly, in.ExplainScores)
		metrics.McpToolCall("search_tools", err)
		if err != nil {
			return nil, searchToolsOut{}, err
		}
		appendSearchHistory(stack, in.Query)
		out := buildSearchToolsOut(stack, ranked)
		return nil, out, nil
	})
}

func SearchToolsResultJSON(ctx context.Context, stack *runtime.Stack, query string, limit int, sourceIDs []string, opts *SearchCallOpts) ([]byte, error) {
	if limit <= 0 {
		limit = 10
	}
	group := false
	lex := false
	explain := false
	if opts != nil {
		group = opts.GroupBySource
		lex = opts.LexicalOnly
		explain = opts.ExplainScores
	}
	ranked, err := runSearchPipeline(ctx, stack, query, limit, sourceIDs, group, lex, explain)
	if err != nil {
		return nil, err
	}
	appendSearchHistory(stack, query)
	out := buildSearchToolsOut(stack, ranked)
	return json.MarshalIndent(out, "", "  ")
}

func runSearchPipeline(ctx context.Context, stack *runtime.Stack, query string, limit int, sourceIDs []string, groupBySource, lexicalOnce, explainScores bool) (models.RankedResults, error) {
	var cfg *config.Config
	if stack != nil {
		cfg = stack.Cfg
	}
	query = applySearchAliases(cfg, query)
	fav := favoriteSet(ctx, stack)
	ranked, err := stack.Search.Search(ctx, models.SearchQuery{
		Text:          query,
		Limit:         limit,
		SourceIDs:     sourceIDs,
		GroupBySource: groupBySource,
		LexicalOnly:   lexicalOnce,
		FavoriteIDs:   fav,
		ExplainScores: explainScores,
	})
	if err != nil {
		return ranked, err
	}
	ranked = filterRankedByRegistry(stack, ranked)
	// Passthrough fallback: when search returns zero results, check for sources with fallback=passthrough
	// and return their full catalog entries as a safety net.
	if len(ranked.Results) == 0 && strings.TrimSpace(query) != "" {
		ranked = applyPassthroughFallback(ctx, stack, ranked, limit, sourceIDs)
	}
	if groupBySource && len(ranked.Results) > 0 {
		ranked.Grouped = search.GroupResultsBySource(ranked.Results)
	}
	return ranked, nil
}

// applyPassthroughFallback returns catalog entries from sources configured with fallback=passthrough
// when search returned zero results. This prevents the agent from being completely blind.
func applyPassthroughFallback(ctx context.Context, stack *runtime.Stack, ranked models.RankedResults, limit int, sourceIDs []string) models.RankedResults {
	if stack == nil || stack.Registry == nil || stack.Store == nil {
		return ranked
	}
	passthroughSources := stack.Registry.SourcesWithFallback("passthrough")
	if len(passthroughSources) == 0 {
		return ranked
	}
	remaining := limit
	if remaining <= 0 {
		remaining = 10
	}
	var results []models.SearchResult
	for _, src := range passthroughSources {
		if !sourceAllowedPassthrough(src.ID, sourceIDs) {
			continue
		}
		recs, err := stack.Store.ListBySourceWithLimit(ctx, src.ID, remaining)
		if err != nil {
			continue
		}
		for _, rec := range recs {
			results = append(results, models.SearchResult{
				Kind:          rec.Kind,
				ProxyToolName: rec.CanonicalName,
				SourceID:      rec.SourceID,
				Summary:       rec.EffectiveSummary(),
				Score:         0.01,
				WhyMatched:    []string{"fallback:passthrough"},
				CapabilityID:  rec.ID,
			})
		}
		remaining = limit - len(results)
		if remaining <= 0 {
			break
		}
	}
	if len(results) == 0 {
		return ranked
	}
	metrics.PassthroughFallbackActivated(len(results))
	ranked.Results = results
	ranked.CandidatePath = "passthrough_fallback"
	return ranked
}

func sourceAllowedPassthrough(sourceID string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, id := range filter {
		if id == sourceID {
			return true
		}
	}
	return false
}

func applySearchAliases(cfg *config.Config, q string) string {
	if cfg == nil || len(cfg.Search.Aliases) == 0 {
		return q
	}
	t := strings.TrimSpace(q)
	if t == "" {
		return q
	}
	if rep, ok := cfg.Search.Aliases[t]; ok && strings.TrimSpace(rep) != "" {
		return rep
	}
	if rep, ok := cfg.Search.Aliases[strings.ToLower(t)]; ok && strings.TrimSpace(rep) != "" {
		return rep
	}
	return q
}

func favoriteSet(ctx context.Context, stack *runtime.Stack) map[string]struct{} {
	if stack == nil || stack.Store == nil {
		return nil
	}
	ids, err := stack.Store.ListFavoriteIDs(ctx)
	if err != nil || len(ids) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

func filterRankedByRegistry(stack *runtime.Stack, ranked models.RankedResults) models.RankedResults {
	if stack == nil || stack.Registry == nil {
		return ranked
	}
	kept := ranked.Results[:0]
	for _, r := range ranked.Results {
		if stack.Registry.SourceEnabled(r.SourceID) {
			kept = append(kept, r)
		}
	}
	ranked.Results = kept
	ranked.Grouped = nil
	return ranked
}

func appendSearchHistory(stack *runtime.Stack, query string) {
	if stack == nil || stack.Cfg == nil {
		return
	}
	p := strings.TrimSpace(stack.Cfg.Storage.HistoryPath)
	if p == "" {
		return
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "%s\t%s\n", time.Now().UTC().Format(time.RFC3339), strings.TrimSpace(query))
}

func buildSearchToolsOut(stack *runtime.Stack, ranked models.RankedResults) searchToolsOut {
	out := searchToolsOut{CandidatePath: ranked.CandidatePath, Results: searchHitsFromRanked(stack, ranked)}
	if len(ranked.Grouped) == 0 {
		return out
	}
	out.Grouped = make([]searchGroupWire, 0, len(ranked.Grouped))
	for _, g := range ranked.Grouped {
		rr := models.RankedResults{Results: g.Results}
		out.Grouped = append(out.Grouped, searchGroupWire{
			SourceID: g.SourceID,
			Results:  searchHitsFromRanked(stack, rr),
		})
	}
	return out
}
