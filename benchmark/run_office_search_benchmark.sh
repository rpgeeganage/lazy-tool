#!/usr/bin/env bash
set -euo pipefail

# Deterministic Office search benchmark for lazy-tool-x.
#
# Runs a fixed set of Office-oriented queries against lazy-tool-x search and
# checks whether the expected capability appears in the top K results.
#
# Usage:
#   ./benchmark/run_office_search_benchmark.sh
#   ./benchmark/run_office_search_benchmark.sh --config ~/.config/lazy-tool-x/config.yaml
#   ./benchmark/run_office_search_benchmark.sh --limit 5 --output-dir ./benchmark/results/office-search

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TOOL_BIN="${TOOL_BIN:-/home/cjnova/.local/bin/lazy-tool-x}"
TOOL_CONFIG="${TOOL_CONFIG:-/home/cjnova/.config/lazy-tool-x/config.yaml}"
LIMIT="${LIMIT:-5}"
OUTPUT_DIR=""
SKIP_REINDEX="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tool-bin)
      TOOL_BIN="${2:?missing value for --tool-bin}"
      shift 2
      ;;
    --config)
      TOOL_CONFIG="${2:?missing value for --config}"
      shift 2
      ;;
    --limit)
      LIMIT="${2:?missing value for --limit}"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="${2:?missing value for --output-dir}"
      shift 2
      ;;
    --skip-reindex)
      SKIP_REINDEX="true"
      shift
      ;;
    -h|--help)
      sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if [[ ! -f "$TOOL_CONFIG" ]]; then
  echo "Config not found: $TOOL_CONFIG" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq not found in PATH" >&2
  exit 1
fi

if [[ -z "$OUTPUT_DIR" ]]; then
  OUTPUT_DIR="$REPO_ROOT/benchmark/results/office-search-$(date -u +%Y%m%dT%H%M%SZ)"
fi
mkdir -p "$OUTPUT_DIR"

export LAZY_TOOL_CONFIG="$TOOL_CONFIG"

if [[ "$SKIP_REINDEX" != "true" ]]; then
  echo "==> Reindexing"
  "$TOOL_BIN" --config "$TOOL_CONFIG" reindex >/dev/null
fi

echo "==> Running deterministic Office search benchmark"

queries_json='[
  {
    "name": "sow_from_markdown",
    "query": "fill statement of work from markdown template",
    "expected": "office__word_create_sow_from_markdown"
  },
  {
    "name": "pptx_notes",
    "query": "read powerpoint speaker notes",
    "expected_any": ["office__pptx_get_notes", "office__pptx_set_notes"]
  },
  {
    "name": "excel_patch",
    "query": "patch excel workbook cells safely",
    "expected": "office__office_patch"
  },
  {
    "name": "word_anchor_insert",
    "query": "insert paragraphs at heading anchor in word document",
    "expected": "office__word_insert_at_anchor"
  },
  {
    "name": "table_append",
    "query": "append staffing row to office table",
    "expected": "office__office_table"
  },
  {
    "name": "document_audit",
    "query": "audit word document placeholders and completion",
    "expected": "office__office_audit"
  },
  {
    "name": "deck_from_markdown",
    "query": "create powerpoint deck from markdown with notes",
    "expected": "office__pptx_from_markdown"
  },
  {
    "name": "azure_cost",
    "query": "calculate azure vm monthly cost from cached prices",
    "expected": "office__azure_calculate_cost"
  },
  {
    "name": "template_inspect",
    "query": "inspect word template placeholders sections tables",
    "expected": "office__office_template"
  },
  {
    "name": "conversation_summary_doc",
    "query": "create a new Word document populated with a summary of this conversation",
    "expected": "office__word_from_markdown"
  }
]'

results='[]'
pass_count=0
total_count=0

while IFS= read -r item; do
  name="$(jq -r '.name' <<<"$item")"
  query="$(jq -r '.query' <<<"$item")"
  expected="$(jq -r '.expected // empty' <<<"$item")"
  raw="$OUTPUT_DIR/${name}.json"

  "$TOOL_BIN" --config "$TOOL_CONFIG" search "$query" --limit "$LIMIT" > "$raw"

  top_hits="$(jq '[.results[].proxy_tool_name]' "$raw")"
  matched="false"
  rank="null"

  if [[ -n "$expected" ]]; then
    if jq -e --arg expected "$expected" '.results | map(.proxy_tool_name) | index($expected) != null' "$raw" >/dev/null; then
      matched="true"
      rank="$(jq -r --arg expected "$expected" '.results | map(.proxy_tool_name) | index($expected) + 1' "$raw")"
    fi
  else
    if jq -e --argjson exp "$(jq '.expected_any' <<<"$item")" '.results | map(.proxy_tool_name) as $hits | any($exp[]; $hits | index(.) != null)' "$raw" >/dev/null; then
      matched="true"
      rank="$(jq -r --argjson exp "$(jq '.expected_any' <<<"$item")" '.results | map(.proxy_tool_name) as $hits | [ $exp[] | ($hits | index(.)) ] | map(select(. != null)) | min + 1' "$raw")"
    fi
  fi

  total_count=$((total_count + 1))
  if [[ "$matched" == "true" ]]; then
    pass_count=$((pass_count + 1))
  fi

  results="$(jq \
    --arg name "$name" \
    --arg query "$query" \
    --argjson top_hits "$top_hits" \
    --arg matched "$matched" \
    --arg rank "$rank" \
    '. + [{name: $name, query: $query, matched: ($matched == "true"), rank: (if $rank == "null" then null else ($rank|tonumber) end), top_hits: $top_hits}]' \
    <<<"$results")"
done < <(jq -c '.[]' <<<"$queries_json")

summary="$(jq \
  --argjson passed "$pass_count" \
  --argjson total "$total_count" \
  --arg tool_bin "$TOOL_BIN" \
  --arg config "$TOOL_CONFIG" \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson results "$results" \
  '{generated_at: $generated_at, tool_bin: $tool_bin, config: $config, passed: $passed, total: $total, pass_rate: ($passed / $total), results: $results}' \
  <<<"{}")"

printf '%s
' "$summary" > "$OUTPUT_DIR/office_search_benchmark.json"

echo "==> Results"
jq -r '.results[] | "- " + .name + ": matched=" + (.matched|tostring) + ", rank=" + ((.rank // "null")|tostring)' "$OUTPUT_DIR/office_search_benchmark.json"
echo "Pass rate: $pass_count/$total_count"
echo "Saved: $OUTPUT_DIR/office_search_benchmark.json"
