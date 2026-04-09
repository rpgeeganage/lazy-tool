package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"lazy-tool/internal/app"
	"lazy-tool/internal/catalog"
	"lazy-tool/internal/mcpserver"
	"lazy-tool/internal/runtime"
	"lazy-tool/internal/storage"
	"lazy-tool/internal/tracing"
	"lazy-tool/pkg/models"
)

var titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
var helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

type viewKind int

const (
	viewMenu viewKind = iota
	viewSources
	viewCapabilities
	viewSearch
	viewReindex
	viewInspect
	viewTraces
)

type Model struct {
	stack           *runtime.Stack
	view            viewKind
	input           textinput.Model
	body            string
	status          string
	err             error
	running         bool
	lastInspectName string
	overrideMode    bool
	searchExplain   bool // search view: request score_breakdown (toggle with e)
}

func NewModel(stack *runtime.Stack) Model {
	ti := textinput.New()
	ti.Placeholder = "query or proxy_tool_name"
	ti.Focus()
	return Model{
		stack:  stack,
		view:   viewMenu,
		input:  ti,
		status: "m menu • 1 sources • 2 capabilities • 3 search • 4 reindex • 5 inspect • 6 traces • q quit",
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

type reindexMsg struct{ err error }
type searchMsg struct {
	out string
	err error
}
type inspectMsg struct {
	out string
	err error
}
type summarySaveMsg struct {
	err       error
	canonical string
}

func runReindex(stack *runtime.Stack) tea.Cmd {
	return func() tea.Msg {
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
		err := ix.Run(context.Background())
		return reindexMsg{err: err}
	}
}

func runSearch(stack *runtime.Stack, q string, explainScores bool) tea.Cmd {
	return func() tea.Msg {
		var opts *mcpserver.SearchCallOpts
		if explainScores {
			opts = &mcpserver.SearchCallOpts{ExplainScores: true}
		}
		b, err := mcpserver.SearchToolsResultJSON(context.Background(), stack, q, 12, nil, opts)
		if err != nil {
			return searchMsg{err: err}
		}
		return searchMsg{out: string(b)}
	}
}

func runInspect(stack *runtime.Stack, name string) tea.Cmd {
	return func() tea.Msg {
		v, err := catalog.BuildInspectView(context.Background(), stack.Store, stack.Registry, name)
		if err != nil {
			return inspectMsg{err: err}
		}
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return inspectMsg{err: err}
		}
		return inspectMsg{out: string(b)}
	}
}

func runSetUserSummary(stack *runtime.Stack, canonical, text string) tea.Cmd {
	return func() tea.Msg {
		err := catalog.SetUserSummary(context.Background(), stack.Store, canonical, text)
		return summarySaveMsg{err: err, canonical: canonical}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "m":
			m.view = viewMenu
			m.body = ""
			m.err = nil
			m.running = false
			return m, nil
		case "1":
			m.view = viewSources
			m.body = formatSources(m.stack)
			m.err = nil
			return m, nil
		case "2":
			m.view = viewCapabilities
			m.body, m.err = loadCapabilities(m.stack)
			return m, nil
		case "3":
			m.view = viewSearch
			m.searchExplain = false
			m.status = searchViewStatus(false)
			m.input.Focus()
			m.body = ""
			m.err = nil
			return m, textinput.Blink
		case "4":
			m.view = viewReindex
			at, ok, s := app.GetReindexStatus()
			line := "never"
			if !at.IsZero() {
				line = at.Format(time.RFC3339) + " — " + fmt.Sprint(ok) + " — " + s
			}
			m.body = "Last reindex: " + line + "\n\nPress R to run reindex."
			m.err = nil
			return m, nil
		case "5":
			m.view = viewInspect
			m.overrideMode = false
			m.lastInspectName = ""
			m.input.Focus()
			m.body = "Enter proxy_tool_name, then Enter. After load: O = edit manual summary."
			m.err = nil
			return m, textinput.Blink
		case "o", "O":
			if m.view == viewInspect && m.lastInspectName != "" && !m.overrideMode && m.body != "" && m.err == nil {
				m.overrideMode = true
				m.input.SetValue("")
				m.body = fmt.Sprintf("Manual summary for %q — type text and Enter (empty clears). Esc cancels.", m.lastInspectName)
				return m, textinput.Blink
			}
		case "esc":
			if m.overrideMode {
				m.overrideMode = false
				m.body = "Enter proxy_tool_name, then Enter. After load: O = edit manual summary."
				m.input.SetValue(m.lastInspectName)
				return m, nil
			}
		case "6":
			m.view = viewTraces
			b, err := tracing.SnapshotJSON()
			if err != nil {
				m.err = err
				m.body = ""
			} else {
				m.err = nil
				m.body = string(b)
			}
			return m, nil
		case "r", "R":
			if m.view == viewReindex && !m.running {
				m.running = true
				m.status = "reindexing…"
				return m, runReindex(m.stack)
			}
		case "e", "E":
			if m.view == viewSearch {
				m.searchExplain = !m.searchExplain
				m.status = searchViewStatus(m.searchExplain)
				return m, nil
			}
		}
	case reindexMsg:
		m.running = false
		if msg.err != nil {
			m.err = msg.err
			m.status = "reindex failed"
		} else {
			m.err = nil
			m.status = "reindex done"
			_, _, s := app.GetReindexStatus()
			m.body = "Last reindex: " + s
		}
		return m, nil
	case searchMsg:
		if msg.err != nil {
			m.err = msg.err
			m.body = ""
		} else {
			m.err = nil
			m.body = formatSearchResults(msg.out)
		}
		return m, nil
	case inspectMsg:
		if msg.err != nil {
			m.err = msg.err
			m.body = ""
		} else {
			m.err = nil
			m.body = msg.out
		}
		return m, nil
	case summarySaveMsg:
		m.overrideMode = false
		if msg.err != nil {
			m.err = msg.err
			m.status = "summary save failed"
			return m, nil
		}
		m.err = nil
		m.status = "summary saved"
		m.input.SetValue(msg.canonical)
		return m, runInspect(m.stack, msg.canonical)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "enter" {
		q := strings.TrimSpace(m.input.Value())
		if m.view == viewSearch && q != "" {
			m.status = "searching…"
			return m, tea.Batch(cmd, runSearch(m.stack, q, m.searchExplain))
		}
		if m.view == viewInspect && m.overrideMode {
			m.status = "saving summary…"
			return m, tea.Batch(cmd, runSetUserSummary(m.stack, m.lastInspectName, q))
		}
		if m.view == viewInspect && q != "" {
			m.lastInspectName = q
			m.status = "loading…"
			return m, tea.Batch(cmd, runInspect(m.stack, q))
		}
	}
	return m, cmd
}

func formatSearchResults(rawJSON string) string {
	var v struct {
		CandidatePath string `json:"candidate_path,omitempty"`
		Results       []struct {
			ProxyToolName  string             `json:"proxy_tool_name"`
			SourceID       string             `json:"source_id"`
			Score          float64            `json:"score"`
			WhyMatched     []string           `json:"why_matched"`
			Summary        string             `json:"summary"`
			ScoreBreakdown map[string]float64 `json:"score_breakdown,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &v); err != nil {
		return rawJSON
	}
	if len(v.Results) == 0 {
		if v.CandidatePath != "" {
			return "Lexical candidates: " + v.CandidatePath + "\n\n(no hits)"
		}
		return rawJSON
	}
	var b strings.Builder
	if v.CandidatePath != "" {
		fmt.Fprintf(&b, "Lexical candidates: %s\n\n", v.CandidatePath)
	}
	for _, r := range v.Results {
		why := strings.Join(r.WhyMatched, ", ")
		if why == "" {
			why = "—"
		}
		fmt.Fprintf(&b, "%s  (%s)  score=%.2f\n  why: %s\n  %s", r.ProxyToolName, r.SourceID, r.Score, why, r.Summary)
		if len(r.ScoreBreakdown) > 0 {
			keys := make([]string, 0, len(r.ScoreBreakdown))
			for k := range r.ScoreBreakdown {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			fmt.Fprintf(&b, "\n  breakdown:")
			for _, k := range keys {
				fmt.Fprintf(&b, " %s=%.3f", k, r.ScoreBreakdown[k])
			}
		}
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func formatSources(stack *runtime.Stack) string {
	ctx := context.Background()
	srcs := stack.Registry.All()
	rows, err := stack.Store.ListSourceHealth(ctx)
	if err != nil {
		b, e := json.MarshalIndent(srcs, "", "  ")
		if e != nil {
			return e.Error()
		}
		return string(b)
	}
	byID := make(map[string]storage.SourceHealthRow, len(rows))
	for _, r := range rows {
		byID[r.SourceID] = r
	}
	type row struct {
		models.Source
		LastReindexOK      *bool   `json:"last_reindex_ok,omitempty"`
		LastReindexMessage string  `json:"last_reindex_message,omitempty"`
		LastReindexAt      *string `json:"last_reindex_at,omitempty"`
	}
	out := make([]row, 0, len(srcs))
	for _, src := range srcs {
		r := row{Source: src}
		if h, ok := byID[src.ID]; ok {
			okCopy := h.OK
			r.LastReindexOK = &okCopy
			r.LastReindexMessage = h.Message
			s := h.UpdatedAt.UTC().Format(time.RFC3339)
			r.LastReindexAt = &s
		}
		out = append(out, r)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func loadCapabilities(stack *runtime.Stack) (string, error) {
	all, err := stack.Store.ListAll(context.Background())
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return "", err
	}
	s := string(b)
	if len(s) > 12000 {
		s = s[:12000] + "\n… truncated (use CLI or web for full JSON) …"
	}
	return s, nil
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("lazy-tool-x"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(viewTitle(m.view)))
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render(m.status))
	b.WriteString("\n\n")
	if m.view == viewSearch || m.view == viewInspect {
		b.WriteString(m.input.View())
		b.WriteString("\n\n")
	}
	if m.err != nil {
		b.WriteString("error: " + m.err.Error() + "\n\n")
	}
	if m.body != "" {
		b.WriteString(m.body)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	footer := "m menu • 1–6 views • q quit • Enter runs search/inspect • inspect: O override summary • Esc cancel override"
	if m.view == viewSearch {
		footer += " • search: e explain scores"
	}
	b.WriteString(helpStyle.Render(footer))
	return b.String()
}

func searchViewStatus(explainOn bool) string {
	if explainOn {
		return "search • explain scores ON (e toggles) • m menu • 1 sources • 2 capabilities • 3 search • 4 reindex • 5 inspect • 6 traces • q quit"
	}
	return "search • explain scores off (e toggles) • m menu • 1 sources • 2 capabilities • 3 search • 4 reindex • 5 inspect • 6 traces • q quit"
}

func viewTitle(v viewKind) string {
	switch v {
	case viewMenu:
		return "Menu"
	case viewSources:
		return "Source list"
	case viewCapabilities:
		return "Capability list"
	case viewSearch:
		return "Search view"
	case viewReindex:
		return "Reindex status"
	case viewInspect:
		return "Tool inspector"
	case viewTraces:
		return "Invocation logs"
	default:
		return ""
	}
}
