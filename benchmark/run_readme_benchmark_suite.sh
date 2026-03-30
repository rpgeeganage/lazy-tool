#!/usr/bin/env bash
set -euo pipefail

# Updated README benchmark suite for lazy-tool.
# Changes vs earlier kit:
#   - removes filesystem-related tasks
#   - runs lazy-tool reindex before benchmarks
#   - performs preflight catalog sanity checks
#
# Usage:
#   ./benchmark/run_readme_benchmark_suite.sh
#   ./benchmark/run_readme_benchmark_suite.sh --model llama-3.1-8b-instant --repeat 20
#   ./benchmark/run_readme_benchmark_suite.sh --output-dir ./benchmark-results --skip-build
#
# Assumptions:
#   - run from the lazy-tool repo root or via benchmark/ (auto-resolves repo root)
#   - GROQ_API_KEY is exported
#   - MCPJungle is already running locally and sample MCPs are registered
#   - benchmark/configs/mcpjungle-lazy-tool.yaml exists and is valid

MODEL="llama-3.1-8b-instant"
REPEAT="20"
REPO_ROOT=""
OUTPUT_DIR=""
LAZY_CONFIG=""
JUNGLE_URL="http://127.0.0.1:8080/mcp"
SKIP_BUILD="false"
STRICT_ANSWERS="true"
SKIP_PREFLIGHT="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --model)
      MODEL="${2:?missing value for --model}"
      shift 2
      ;;
    --repeat)
      REPEAT="${2:?missing value for --repeat}"
      shift 2
      ;;
    --repo-root)
      REPO_ROOT="${2:?missing value for --repo-root}"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="${2:?missing value for --output-dir}"
      shift 2
      ;;
    --lazy-config)
      LAZY_CONFIG="${2:?missing value for --lazy-config}"
      shift 2
      ;;
    --jungle-url)
      JUNGLE_URL="${2:?missing value for --jungle-url}"
      shift 2
      ;;
    --skip-build)
      SKIP_BUILD="true"
      shift
      ;;
    --no-strict-answers)
      STRICT_ANSWERS="false"
      shift
      ;;
    --skip-preflight)
      SKIP_PREFLIGHT="true"
      shift
      ;;
    -h|--help)
      sed -n '1,110p' "$0"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if [[ -z "${GROQ_API_KEY:-}" ]]; then
  echo "GROQ_API_KEY is not set." >&2
  exit 1
fi

if [[ -z "$REPO_ROOT" ]]; then
  REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
fi

if [[ ! -f "$REPO_ROOT/benchmark/run_groq_benchmark_v2.py" ]]; then
  echo "Could not find benchmark/run_groq_benchmark_v2.py under repo root: $REPO_ROOT" >&2
  exit 1
fi

if [[ -z "$LAZY_CONFIG" ]]; then
  LAZY_CONFIG="$REPO_ROOT/benchmark/configs/mcpjungle-lazy-tool.yaml"
fi

if [[ ! -f "$LAZY_CONFIG" ]]; then
  echo "Lazy config not found: $LAZY_CONFIG" >&2
  exit 1
fi

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
if [[ -z "$OUTPUT_DIR" ]]; then
  OUTPUT_DIR="$REPO_ROOT/benchmark/results/readme-clean-$TIMESTAMP"
fi

mkdir -p "$OUTPUT_DIR/raw"

if [[ "$SKIP_BUILD" != "true" ]]; then
  echo "==> Building lazy-tool"
  (cd "$REPO_ROOT" && make build)
fi

HARNESS="$REPO_ROOT/benchmark/run_groq_benchmark_v2.py"
LAZY_BINARY="$REPO_ROOT/bin/lazy-tool"

# ── Python dependencies ──────────────────────────────────────────────────

# shellcheck source=scripts/ensure-python-deps.sh
source "$REPO_ROOT/benchmark/scripts/ensure-python-deps.sh" "$REPO_ROOT"

COMMON_ARGS=(
  --model "$MODEL"
  --repeat "$REPEAT"
  --lazy-binary "$LAZY_BINARY"
  --lazy-config "$LAZY_CONFIG"
  --jungle-url "$JUNGLE_URL"
  --workdir "$REPO_ROOT"
)

if [[ "$STRICT_ANSWERS" == "true" ]]; then
  COMMON_ARGS+=(--strict-answers)
fi

run_task() {
  local name="$1"
  shift
  local jsonl="$OUTPUT_DIR/raw/${name}.jsonl"
  local csv="$OUTPUT_DIR/raw/${name}.csv"

  echo "==> Running task: $name"
  "$PYTHON" "$HARNESS" \
    "${COMMON_ARGS[@]}" \
    "$@" \
    --jsonl-out "$jsonl" \
    --csv-out "$csv"
}

preflight_check() {
  echo "==> Preflight: reindex"
  export LAZY_TOOL_CONFIG="$LAZY_CONFIG"
  "$LAZY_BINARY" reindex

  echo "==> Preflight: source health"
  "$LAZY_BINARY" sources --status | tee "$OUTPUT_DIR/preflight_sources_status.json"

  echo "==> Preflight: catalog sanity"
  "$LAZY_BINARY" search "echo" --limit 10 | tee "$OUTPUT_DIR/preflight_search_echo.json"
  "$LAZY_BINARY" search "prompt" --limit 10 | tee "$OUTPUT_DIR/preflight_search_prompt.json"
  "$LAZY_BINARY" search "resource" --limit 10 | tee "$OUTPUT_DIR/preflight_search_resource.json"

  "$PYTHON" "$(dirname "$0")/scripts/validate_preflight.py" \
    --echo "$OUTPUT_DIR/preflight_search_echo.json" \
    --prompt "$OUTPUT_DIR/preflight_search_prompt.json" \
    --resource "$OUTPUT_DIR/preflight_search_resource.json"
}

if [[ "$SKIP_PREFLIGHT" != "true" ]]; then
  preflight_check
fi

# Core README tasks
run_task "no_tool_both" \
  --task no_tool \
  --mode both

# Lazy-only discovery/search tasks
run_task "search_tools_smoke_lazy" \
  --task search_tools_smoke \
  --mode lazy

run_task "ambiguous_search_lazy" \
  --task ambiguous_search \
  --mode lazy

run_task "search_tools_prompt_lazy" \
  --task search_tools_prompt \
  --mode lazy

run_task "search_tools_resource_lazy" \
  --task search_tools_resource \
  --mode lazy

# Optional routed tool task: useful, but only publish if stable
run_task "everything_echo_both" \
  --task everything_echo \
  --mode both

# Persist run metadata
cat > "$OUTPUT_DIR/manifest.json" <<EOF
{
  "timestamp_utc": "$TIMESTAMP",
  "repo_root": "$REPO_ROOT",
  "model": "$MODEL",
  "repeat": $REPEAT,
  "lazy_config": "$LAZY_CONFIG",
  "jungle_url": "$JUNGLE_URL",
  "strict_answers": $([[ "$STRICT_ANSWERS" == "true" ]] && echo true || echo false),
  "skip_preflight": $([[ "$SKIP_PREFLIGHT" == "true" ]] && echo true || echo false),
  "suite_type": "readme-clean"
}
EOF

echo "==> Summarizing results"
"$PYTHON" "$(dirname "$0")/scripts/summarize_readme_benchmarks.py" \
  --input-dir "$OUTPUT_DIR/raw" \
  --manifest "$OUTPUT_DIR/manifest.json" \
  --markdown-out "$OUTPUT_DIR/README_BENCHMARK_SNIPPET.md" \
  --json-out "$OUTPUT_DIR/README_BENCHMARK_SUMMARY.json"

echo
echo "Done."
echo "Raw files:     $OUTPUT_DIR/raw"
echo "Markdown:      $OUTPUT_DIR/README_BENCHMARK_SNIPPET.md"
echo "JSON summary:  $OUTPUT_DIR/README_BENCHMARK_SUMMARY.json"
