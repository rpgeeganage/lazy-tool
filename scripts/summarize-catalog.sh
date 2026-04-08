#!/usr/bin/env bash
# summarize-catalog.sh
#
# Generates LLM-powered summaries for a lazy-tool-style catalog using opencode
# and writes them via `catalog set-summary`.
#
# Requirements:
#   - lazy-tool-x or compatible binary in PATH
#   - opencode
#   - jq
#
# Usage:
#   ./scripts/summarize-catalog.sh
#   ./scripts/summarize-catalog.sh --force
#   ./scripts/summarize-catalog.sh --only <proxy_name>
#   ./scripts/summarize-catalog.sh --dry-run
#
# Environment:
#   TOOL_BIN        binary to use (default: lazy-tool-x)
#   TOOL_CONFIG     config path to pass with --config (optional)
#   MODEL           model to use (default: github-copilot/gpt-5.4-mini)
#   DELAY           seconds between LLM calls (default: 2)
#   OVERRIDES_FILE  optional JSON map of canonical_name -> exact summary

set -euo pipefail

MODEL="${MODEL:-github-copilot/gpt-5.4-mini}"
DELAY="${DELAY:-2}"
FORCE=false
DRY_RUN=false
ONLY=""
TOOL_BIN="${TOOL_BIN:-lazy-tool-x}"
TOOL_CONFIG="${TOOL_CONFIG:-${LAZY_TOOL_CONFIG:-}}"
SCRIPT_DIR="$(cd -- "$(dirname -- "$0")" && pwd)"
OVERRIDES_FILE="${OVERRIDES_FILE:-$SCRIPT_DIR/summarize-catalog.overrides.json}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --force)        FORCE=true; shift ;;
    --dry-run)      DRY_RUN=true; shift ;;
    --only)         ONLY="$2"; shift 2 ;;
    --model)        MODEL="$2"; shift 2 ;;
    --delay)        DELAY="$2"; shift 2 ;;
    --tool-bin)     TOOL_BIN="$2"; shift 2 ;;
    --config)       TOOL_CONFIG="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,/^$/p' "$0" | sed 's/^# \?//'
      exit 0
      ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

for cmd in "$TOOL_BIN" opencode jq; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "ERROR: $cmd not found in PATH" >&2
    exit 1
  fi
done

TOOL_ARGS=()
if [[ -n "$TOOL_CONFIG" ]]; then
  TOOL_ARGS+=(--config "$TOOL_CONFIG")
  export LAZY_TOOL_CONFIG="$TOOL_CONFIG"
fi

if ! "$TOOL_BIN" health "${TOOL_ARGS[@]}" &>/dev/null; then
  echo "ERROR: $TOOL_BIN health check failed." >&2
  if [[ -n "$TOOL_CONFIG" ]]; then
    echo "  Config: $TOOL_CONFIG" >&2
  else
    echo "  Pass --config or set TOOL_CONFIG / LAZY_TOOL_CONFIG" >&2
  fi
  exit 1
fi

TMPDIR_WORK="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_WORK"' EXIT

COUNT_DONE_FILE="$TMPDIR_WORK/count_done"
COUNT_SKIP_FILE="$TMPDIR_WORK/count_skip"
COUNT_FAIL_FILE="$TMPDIR_WORK/count_fail"
echo 0 > "$COUNT_DONE_FILE"
echo 0 > "$COUNT_SKIP_FILE"
echo 0 > "$COUNT_FAIL_FILE"

inc_done() { echo $(( $(cat "$COUNT_DONE_FILE") + 1 )) > "$COUNT_DONE_FILE"; }
inc_skip() { echo $(( $(cat "$COUNT_SKIP_FILE") + 1 )) > "$COUNT_SKIP_FILE"; }
inc_fail() { echo $(( $(cat "$COUNT_FAIL_FILE") + 1 )) > "$COUNT_FAIL_FILE"; }

extract_json() {
  sed '/^#/d'
}

build_prompt() {
  local kind="$1"
  local orig_name="$2"
  local orig_desc="$3"
  local input_schema="$4"
  local metadata="$5"

  cat <<PROMPT
You summarize MCP capabilities for LLM tool discovery.
Return your answer in EXACTLY this format with no other text:

SUMMARY: <one sentence, 15-30 words>
KEYWORDS: <comma-separated list of 10-20 domain terms users might search for but are NOT in the description>

Rules for SUMMARY:
- One sentence only, 15-30 words
- Must include: primary action, target domain/objects
- Include key inputs only if they help distinguish this tool from others
- No hype, no filler, no implementation details
- If the tool searches by topic/query, describe it as discovery/search and mention returned excerpts, titles, or links
- If the tool reads one known URL/page, describe it as fetching full content after discovery, not as general documentation search

Rules for KEYWORDS:
- Terms a user would search for when they need this tool
- Include specific product names, services, protocols, and concepts the tool covers
- Do NOT repeat words already in the tool name or description
- Focus on the DOMAIN the tool serves, not the tool's mechanics

Capability kind: ${kind}
Capability name: ${orig_name}
Original description: ${orig_desc}
Input schema: ${input_schema}
Additional metadata: ${metadata}
PROMPT
}

parse_summary() {
  local response="$1"
  echo "$response" | grep -i '^SUMMARY:' | head -1 | sed 's/^[Ss][Uu][Mm][Mm][Aa][Rr][Yy]:[[:space:]]*//'
}

parse_keywords() {
  local response="$1"
  echo "$response" | grep -i '^KEYWORDS:' | head -1 | sed 's/^[Kk][Ee][Yy][Ww][Oo][Rr][Dd][Ss]:[[:space:]]*//'
}

get_override_summary() {
  local proxy_name="$1"
  if [[ ! -f "$OVERRIDES_FILE" ]]; then
    return 1
  fi
  jq -r --arg key "$proxy_name" '.[$key] // empty' "$OVERRIDES_FILE"
}

process_capability() {
  local proxy_name="$1"

  local raw_output details
  raw_output="$("$TOOL_BIN" inspect "$proxy_name" "${TOOL_ARGS[@]}" 2>/dev/null)" || {
    echo "  FAIL: could not inspect $proxy_name" >&2
    inc_fail
    return
  }
  details="$(echo "$raw_output" | extract_json)"

  local existing_summary
  existing_summary="$(echo "$details" | jq -r '.record.user_summary // empty')"
  if [[ -n "$existing_summary" && "$FORCE" != "true" ]]; then
    echo "  SKIP $proxy_name (has user_summary, use --force to overwrite)"
    inc_skip
    return
  fi

  local kind orig_name orig_desc input_schema metadata
  kind="$(echo "$details" | jq -r '.record.Kind // "tool"')"
  orig_name="$(echo "$details" | jq -r '.record.OriginalName // empty')"
  orig_desc="$(echo "$details" | jq -r '.record.OriginalDescription // empty')"
  input_schema="$(echo "$details" | jq -r '.record.InputSchemaJSON // "{}"')"
  metadata="$(echo "$details" | jq -r '.record.MetadataJSON // "{}"')"

  if [[ -z "$orig_desc" ]]; then
    echo "  SKIP $proxy_name (no description)"
    inc_skip
    return
  fi

  local override_summary
  override_summary="$(get_override_summary "$proxy_name" || true)"
  if [[ -n "$override_summary" ]]; then
    if [[ "$DRY_RUN" == "true" ]]; then
      echo "  DRY-RUN OVERRIDE: $proxy_name"
      echo "    summary:  $override_summary"
      inc_done
      return
    fi

    "$TOOL_BIN" catalog set-summary "$proxy_name" "$override_summary" "${TOOL_ARGS[@]}" || {
      echo "  FAIL: set-summary failed for override $proxy_name" >&2
      inc_fail
      return
    }

    echo "  DONE OVERRIDE $proxy_name"
    echo "    -> $override_summary"
    inc_done
    sleep "$DELAY"
    return
  fi

  local prompt
  prompt="$(build_prompt "$kind" "$orig_name" "$orig_desc" "$input_schema" "$metadata")"

  echo "  Summarizing $proxy_name ..."
  local response
  response="$(opencode run -m "$MODEL" --pure "$prompt" </dev/null 2>/dev/null)" || {
    echo "  FAIL: opencode run failed for $proxy_name" >&2
    inc_fail
    return
  }

  local summary keywords
  summary="$(parse_summary "$response")"
  keywords="$(parse_keywords "$response")"

  if [[ -z "$summary" ]]; then
    echo "  FAIL: no SUMMARY line in LLM response for $proxy_name" >&2
    inc_fail
    return
  fi

  local full_summary="$summary"
  if [[ -n "$keywords" ]]; then
    full_summary="${summary} [keywords: ${keywords}]"
  fi

  if [[ "$DRY_RUN" == "true" ]]; then
    echo "  DRY-RUN: $proxy_name"
    echo "    summary:  $full_summary"
    echo "    keywords: $keywords"
    inc_done
    return
  fi

  "$TOOL_BIN" catalog set-summary "$proxy_name" "$full_summary" "${TOOL_ARGS[@]}" || {
    echo "  FAIL: set-summary failed for $proxy_name" >&2
    inc_fail
    return
  }

  echo "  DONE $proxy_name"
  echo "    -> $full_summary"
  inc_done
  sleep "$DELAY"
}

echo "=== catalog summarizer ==="
echo "Tool: $TOOL_BIN"
echo "Model: $MODEL"
echo "Config: ${TOOL_CONFIG:-<default>}"
echo "Dry run: $DRY_RUN"
echo "Force: $FORCE"
echo ""

if [[ -n "$ONLY" ]]; then
  process_capability "$ONLY"
else
  CATALOG="$("$TOOL_BIN" catalog export "${TOOL_ARGS[@]}" 2>/dev/null)" || {
    echo "ERROR: $TOOL_BIN catalog export failed" >&2
    exit 1
  }

  TOTAL="$(echo "$CATALOG" | jq 'length')"
  echo "Found $TOTAL capabilities in catalog"
  echo ""

  if [[ "$TOTAL" -eq 0 ]]; then
    echo "No capabilities indexed. Run: $TOOL_BIN reindex${TOOL_CONFIG:+ --config $TOOL_CONFIG}"
    exit 0
  fi

  mapfile -t PROXY_NAMES < <(echo "$CATALOG" | jq -r '.[].CanonicalName')
  for proxy_name in "${PROXY_NAMES[@]}"; do
    process_capability "$proxy_name"
  done
fi

echo ""
echo "=== Results ==="
echo "  Done: $(cat "$COUNT_DONE_FILE")"
echo "  Skipped: $(cat "$COUNT_SKIP_FILE")"
echo "  Failed: $(cat "$COUNT_FAIL_FILE")"

if [[ "$DRY_RUN" == "true" ]]; then
  echo "(dry-run: nothing was written)"
  exit 0
fi

DONE_COUNT="$(cat "$COUNT_DONE_FILE")"
if [[ "$DONE_COUNT" -eq 0 ]]; then
  echo ""
  echo "Nothing to do."
  exit 0
fi

echo ""
echo "=== Running $TOOL_BIN reindex ==="
echo "  Rebuilding SearchText, FTS5, and embeddings with new user_summary values..."
if "$TOOL_BIN" reindex "${TOOL_ARGS[@]}" 2>&1; then
  echo "  Reindex complete."
else
  echo "  WARNING: Reindex failed. Run manually: $TOOL_BIN reindex${TOOL_CONFIG:+ --config $TOOL_CONFIG}" >&2
fi

echo ""
echo "Done."
