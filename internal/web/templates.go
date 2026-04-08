package web

import "html/template"

// UI follows .agents/skill.md (Uncodixify): muted dark surfaces, no gradient/pill/hero patterns,
// system typography, 8px radius max, simple borders, functional nav.
const uiTemplates = `
{{define "nav"}}
<header class="site-header">
  <a href="/" class="brand">lazy-tool-x</a>
  <nav class="nav-links" aria-label="Main">
    <a href="/search">Search</a>
    <a href="/inspect">Inspect</a>
    <a href="/reindex">Reindex</a>
    <a href="/sources">Sources</a>
    <a href="/capabilities">Capabilities</a>
    <a href="/settings">Settings</a>
    <a href="/traces">Traces</a>
  </nav>
</header>
{{end}}

{{define "styles"}}
<style>
:root {
  --bg: #141414;
  --surface: #1a1a1a;
  --border: #2e2e2e;
  --text: #e8e8e8;
  --muted: #8a8a8a;
  --link: #c9a87a;
  --link-hover: #dcc29a;
  --focus: #a68b52;
}
* { box-sizing: border-box; }
html { font-size: 16px; }
body {
  margin: 0;
  font-family: ui-sans-serif, system-ui, -apple-system, sans-serif;
  background: var(--bg);
  color: var(--text);
  line-height: 1.5;
  min-height: 100vh;
}
.site-header {
  display: flex;
  align-items: center;
  gap: 24px;
  padding: 0 24px;
  height: 52px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.brand {
  font-weight: 600;
  color: var(--text);
  text-decoration: none;
  letter-spacing: -0.02em;
}
.brand:hover { color: var(--link-hover); }
.nav-links {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 20px;
  align-items: center;
}
.nav-links a {
  color: var(--muted);
  text-decoration: none;
  font-size: 14px;
}
.nav-links a:hover { color: var(--link); }
.wrap {
  max-width: 1200px;
  margin: 0 auto;
  padding: 24px;
}
h1 {
  font-size: 1.25rem;
  font-weight: 600;
  margin: 0 0 16px 0;
  letter-spacing: -0.02em;
}
p { margin: 0 0 12px 0; color: var(--muted); font-size: 14px; }
.muted { color: var(--muted); font-size: 13px; }
a { color: var(--link); }
a:hover { color: var(--link-hover); }
.route-list {
  list-style: none;
  padding: 0;
  margin: 0;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  overflow: hidden;
}
.route-list li {
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: stretch;
}
.route-list li:last-child { border-bottom: 0; }
.route-list a {
  display: block;
  padding: 12px 16px;
  color: var(--text);
  text-decoration: none;
  flex: 1;
  font-size: 14px;
  font-family: ui-monospace, monospace;
}
.route-list a:hover { background: rgba(255,255,255,0.04); color: var(--link); }
.route-list span.hint {
  padding: 12px 16px;
  font-size: 12px;
  color: var(--muted);
  border-left: 1px solid var(--border);
  white-space: nowrap;
}
label { display: block; font-size: 13px; color: var(--muted); margin-bottom: 6px; }
input[type="text"] {
  width: 100%;
  max-width: 560px;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
  color: var(--text);
  font-size: 14px;
}
input[type="text"]:focus {
  outline: none;
  border-color: var(--focus);
  box-shadow: 0 0 0 1px var(--focus);
}
textarea {
  width: 100%;
  max-width: 720px;
  min-height: 72px;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
  color: var(--text);
  font-size: 14px;
  font-family: inherit;
}
textarea:focus {
  outline: none;
  border-color: var(--focus);
  box-shadow: 0 0 0 1px var(--focus);
}
button, .btn {
  display: inline-block;
  padding: 8px 14px;
  font-size: 14px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
  cursor: pointer;
}
button:hover, .btn:hover {
  background: #222;
  border-color: #3a3a3a;
}
button:focus-visible, .btn:focus-visible, a:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 2px;
}
.form-row { margin-bottom: 16px; }
.form-actions { margin-top: 12px; }
table.results {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
  background: var(--surface);
  margin-top: 20px;
}
table.results th,
table.results td {
  text-align: left;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  vertical-align: top;
}
table.results th {
  font-weight: 600;
  color: var(--muted);
  font-size: 12px;
  background: var(--bg);
}
table.results tr:last-child td { border-bottom: 0; }
table.results tr:hover td { background: rgba(255,255,255,0.03); }
td.mono { font-family: ui-monospace, monospace; font-size: 12px; word-break: break-all; }
td.kind { font-size: 12px; color: var(--muted); text-transform: lowercase; }
.score { font-variant-numeric: tabular-nums; color: var(--muted); }
pre.code {
  margin: 16px 0 0 0;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
  overflow: auto;
  font-size: 12px;
  line-height: 1.45;
  color: var(--muted);
}
dl.meta { margin: 0; font-size: 14px; }
dl.meta dt { color: var(--muted); font-size: 12px; margin-top: 12px; }
dl.meta dt:first-child { margin-top: 0; }
dl.meta dd { margin: 4px 0 0 0; font-family: ui-monospace, monospace; font-size: 13px; }
.footer-nav { margin-top: 32px; font-size: 13px; }
code { font-family: ui-monospace, monospace; font-size: 12px; color: var(--muted); }
</style>
{{end}}

{{define "home"}}
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>lazy-tool-x</title>
  {{template "styles"}}
</head>
<body>
{{template "nav" .}}
<main class="wrap">
  <h1>lazy-tool-x</h1>
  <p>Local MCP discovery proxy. JSON routes open raw data; Search and Inspect are HTML forms.</p>
  <ul class="route-list">
    <li><a href="/search">/search</a><span class="hint">hybrid search</span></li>
    <li><a href="/inspect">/inspect</a><span class="hint">capability + source config + last reindex</span></li>
    <li><a href="/reindex">/reindex</a><span class="hint">POST to run</span></li>
    <li><a href="/sources">/sources</a><span class="hint">JSON</span> · <a href="/sources?health=1">?health=1</a><span class="hint">last reindex per source</span></li>
    <li><a href="/capabilities">/capabilities</a><span class="hint">JSON</span></li>
    <li><a href="/settings">/settings</a><span class="hint">JSON</span></li>
    <li><a href="/traces">/traces</a><span class="hint">JSON</span></li>
  </ul>
  <p class="footer-nav muted">CLI and MCP setup: see repository README.</p>
</main>
</body>
</html>
{{end}}

{{define "search"}}
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Search — lazy-tool-x</title>
  {{template "styles"}}
</head>
<body>
{{template "nav" .}}
<main class="wrap">
  <h1>Search</h1>
  <form method="GET" action="/search">
    <div class="form-row">
      <label for="q">Query</label>
      <input id="q" type="text" name="q" value="{{.Query}}" autocomplete="off" placeholder="natural language" />
    </div>
    <div class="form-actions">
      <button type="submit">Search</button>
    </div>
  </form>
  {{if .Error}}
  <p class="muted" style="margin-top:16px;color:#c9a090;">{{.Error}}</p>
  {{end}}
  {{if ne .CandidatePath ""}}
  <p class="mono muted" style="margin-top:12px;font-size:12px;">Lexical candidates: <code>{{.CandidatePath}}</code></p>
  {{end}}
  {{if .HasResults}}
  <table class="results">
    <thead>
      <tr>
        <th>Kind</th>
        <th>Canonical name</th>
        <th>Source</th>
        <th>Score</th>
        <th>Why matched</th>
        <th>Summary</th>
      </tr>
    </thead>
    <tbody>
      {{range .Rows}}
      <tr>
        <td class="kind">{{.Kind}}</td>
        <td class="mono">{{.ProxyName}}</td>
        <td class="mono">{{.SourceID}}</td>
        <td class="score">{{printf "%.2f" .Score}}</td>
        <td class="mono" style="font-size:12px;color:var(--muted);">{{.Why}}</td>
        <td>{{.Summary}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
  {{else}}{{if ne .Query ""}}
  <p class="muted" style="margin-top:20px;">No results.</p>
  {{end}}{{end}}
  <p class="footer-nav"><a href="/">Home</a> · <span class="muted">Same URL with <code>Accept: application/json</code> returns raw JSON. Add <code>explain=1</code> for <code>score_breakdown</code> per hit.</span></p>
</main>
</body>
</html>
{{end}}

{{define "reindex"}}
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Reindex — lazy-tool-x</title>
  {{template "styles"}}
</head>
<body>
{{template "nav" .}}
<main class="wrap">
  <h1>Reindex</h1>
  <p>{{.Msg}}</p>
  <dl class="meta">
    <dt>Last run</dt>
    <dd>{{.LastAt}} · {{.LastOK}} — {{.LastMsg}}</dd>
  </dl>
  <form method="POST" action="/reindex" class="form-actions" style="margin-top:20px;">
    <button type="submit">Run reindex</button>
  </form>
  <p class="footer-nav"><a href="/">Home</a></p>
</main>
</body>
</html>
{{end}}

{{define "inspect"}}
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Inspect — lazy-tool-x</title>
  {{template "styles"}}
</head>
<body>
{{template "nav" .}}
<main class="wrap">
  <h1>Inspect</h1>
  <form method="GET" action="/inspect">
    <div class="form-row">
      <label for="name">Canonical name (<code>proxy_tool_name</code> from search)</label>
      <input id="name" type="text" name="name" value="{{.Name}}" autocomplete="off" placeholder="source__p_name or source__tool" size="60" />
    </div>
    <div class="form-actions">
      <button type="submit">Load</button>
    </div>
  </form>
  {{if .JSON}}
  <pre class="code">{{.JSON}}</pre>
  {{end}}
  {{if .RecordPresent}}
  {{if .SummarySaved}}<p class="muted" style="margin-top:12px;">Manual summary saved.</p>{{end}}
  <form method="POST" action="/inspect/summary" class="form-row" style="margin-top:16px;">
    <input type="hidden" name="canonical_name" value="{{.Name}}" />
    <label for="user_summary">Manual summary override (empty clears; shown in search)</label>
    <textarea id="user_summary" name="user_summary" rows="3" spellcheck="true">{{.UserSummary}}</textarea>
    <div class="form-actions"><button type="submit">Save summary</button></div>
  </form>
  {{end}}
  <p class="footer-nav"><a href="/">Home</a></p>
</main>
</body>
</html>
{{end}}
`

var rootTmpl = template.Must(template.New("root").Parse(uiTemplates))
