#!/usr/bin/env bash
set -euo pipefail

# Weak-model (Ollama) benchmark suite for lazy-tool.
#
# Runs local models via Ollama to measure whether lazy-tool's search-first
# approach actually helps small models use MCP tools. Three tiers:
#   Tier 1 — Basic tool-calling (can the model call tools?)
#   Tier 2 — Search surface navigation (coached vs natural prompts)
#   Tier 3 — Deterministic search quality (no LLM, direct MCP calls)
#
# Usage:
#   ./benchmark/run_weak_model_suite.sh
#   ./benchmark/run_weak_model_suite.sh --models qwen2.5:3b --repeat 2
#   ./benchmark/run_weak_model_suite.sh --skip-build --tier 1,2
#
# Requirements:
#   - Ollama running locally (http://localhost:11434)
#   - At least one model pulled (e.g. ollama pull qwen2.5:3b)
#   - MCPJungle running for baseline mode
#   - lazy-tool built and indexed
#   - Python 3.11+ (dependencies are auto-installed into benchmark/.venv)

REPEAT="${REPEAT:-3}"
REPO_ROOT=""
OUTPUT_DIR=""
LAZY_CONFIG=""
JUNGLE_URL="http://127.0.0.1:8080/mcp"
OLLAMA_URL="http://localhost:11434"
SKIP_BUILD="false"
MODELS=""
TIER=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repeat)         REPEAT="${2:?missing value}"; shift 2 ;;
    --repo-root)      REPO_ROOT="${2:?missing value}"; shift 2 ;;
    --output-dir)     OUTPUT_DIR="${2:?missing value}"; shift 2 ;;
    --lazy-config)    LAZY_CONFIG="${2:?missing value}"; shift 2 ;;
    --jungle-url)     JUNGLE_URL="${2:?missing value}"; shift 2 ;;
    --ollama-url)     OLLAMA_URL="${2:?missing value}"; shift 2 ;;
    --skip-build)     SKIP_BUILD="true"; shift ;;
    --models)         MODELS="${2:?missing value}"; shift 2 ;;
    --tier)           TIER="${2:?missing value}"; shift 2 ;;
    *)
      echo "unknown flag: $1" >&2
      exit 1
      ;;
  esac
done

# Resolve repo root
if [[ -z "$REPO_ROOT" ]]; then
  REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
fi

# Resolve lazy config
if [[ -z "$LAZY_CONFIG" ]]; then
  LAZY_CONFIG="$REPO_ROOT/benchmark/configs/mcpjungle-lazy-tool-weak.yaml"
fi

# Resolve output directory
if [[ -z "$OUTPUT_DIR" ]]; then
  OUTPUT_DIR="$REPO_ROOT/benchmark-results/weak-$(date +%Y%m%d-%H%M%S)"
fi
mkdir -p "$OUTPUT_DIR/raw"

LAZY_BINARY="$REPO_ROOT/bin/lazy-tool"
HARNESS="$REPO_ROOT/benchmark/run_weak_model_benchmark.py"

# ── Python dependencies ──────────────────────────────────────────────────

# shellcheck source=scripts/ensure-python-deps.sh
source "$REPO_ROOT/benchmark/scripts/ensure-python-deps.sh" "$REPO_ROOT"

# ── Ollama detection ─────────────────────────────────────────────────────

OLLAMA_API="$OLLAMA_URL/api"

echo "Checking Ollama at $OLLAMA_URL ..."
if ! curl -sf "$OLLAMA_URL/api/version" > /dev/null 2>&1; then
  echo "ERROR: Ollama is not running at $OLLAMA_URL" >&2
  echo "Start Ollama first: ollama serve" >&2
  exit 1
fi

OLLAMA_VERSION=$(curl -sf "$OLLAMA_URL/api/version" | "$PYTHON" -c "import sys,json; print(json.load(sys.stdin).get('version','unknown'))" 2>/dev/null || echo "unknown")
echo "Ollama version: $OLLAMA_VERSION"

# ── Model discovery ──────────────────────────────────────────────────────

if [[ -z "$MODELS" ]]; then
  echo "Discovering installed Ollama models..."
  MODELS=$(ollama list 2>/dev/null | tail -n +2 | awk '{print $1}' | tr '\n' ',' | sed 's/,$//')
  if [[ -z "$MODELS" ]]; then
    echo "ERROR: No Ollama models found. Pull at least one model:" >&2
    echo "  ollama pull qwen2.5:3b" >&2
    exit 1
  fi
fi

echo "Models: $MODELS"

# ── Build ────────────────────────────────────────────────────────────────

if [[ "$SKIP_BUILD" != "true" ]]; then
  echo "Building lazy-tool..."
  (cd "$REPO_ROOT" && make build 2>&1) || {
    echo "Build failed. Run 'make build' or pass --skip-build." >&2
    exit 1
  }
fi

if [[ ! -f "$LAZY_BINARY" ]]; then
  echo "Binary not found: $LAZY_BINARY" >&2
  echo "Run 'make build' first." >&2
  exit 1
fi

# ── Reindex ──────────────────────────────────────────────────────────────

echo "Reindexing catalog..."
LAZY_TOOL_CONFIG="$LAZY_CONFIG" "$LAZY_BINARY" reindex 2>&1 || {
  echo "Reindex failed." >&2
  exit 1
}

# ── Prepare filesystem fixture ───────────────────────────────────────────

FS_ROOT="/tmp/lazy-tool-mcpjungle-fs"
mkdir -p "$FS_ROOT/nested"
echo "hello from lazy-tool benchmark" > "$FS_ROOT/notes.txt"
echo '{"tasks":["benchmark","search","compare"]}' > "$FS_ROOT/todo.json"
echo "nested file" > "$FS_ROOT/nested/info.txt"

# ── Display banner ───────────────────────────────────────────────────────

TIER_ARG=""
if [[ -n "$TIER" ]]; then
  TIER_ARG="--tier $TIER"
fi

echo ""
echo "================================================================"
echo " lazy-tool Weak Model (Ollama) Benchmark Suite"
echo "================================================================"
echo " Ollama:     $OLLAMA_URL (v$OLLAMA_VERSION)"
echo " Models:     $MODELS"
echo " Repeat:     $REPEAT"
echo " Tiers:      ${TIER:-1,2,3}"
echo " Output:     $OUTPUT_DIR"
echo " Jungle:     $JUNGLE_URL"
echo " Config:     $LAZY_CONFIG"
echo "================================================================"
echo ""

# ── Manifest ─────────────────────────────────────────────────────────────

cat > "$OUTPUT_DIR/manifest.json" <<MANIFEST
{
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "repeat": $REPEAT,
  "models_tested": "$(echo "$MODELS" | tr ',' '", "')",
  "ollama_version": "$OLLAMA_VERSION",
  "lazy_tool_version": "$("$LAZY_BINARY" version 2>&1 || echo 'unknown')",
  "jungle_url": "$JUNGLE_URL",
  "lazy_config": "$LAZY_CONFIG",
  "tiers": "${TIER:-1,2,3}"
}
MANIFEST

EXIT_CODE=0

# ── Tier 3 first (model-independent, one run) ───────────────────────────

if [[ -z "$TIER" || "$TIER" == *"3"* ]]; then
  echo ""
  echo "--- Tier 3: Deterministic search quality (no LLM) ----"

  T3_JSONL="$OUTPUT_DIR/raw/search_quality.jsonl"

  "$PYTHON" "$HARNESS" \
    --tier 3 \
    --lazy-binary "$LAZY_BINARY" \
    --lazy-config "$LAZY_CONFIG" \
    --workdir "$REPO_ROOT" \
    --jungle-url "$JUNGLE_URL" \
    --ollama-url "${OLLAMA_URL}/v1" \
    --jsonl-out "$T3_JSONL" \
    2>&1 || {
      echo "  WARNING: Tier 3 had failures" >&2
      EXIT_CODE=1
    }

  echo "  Results: $T3_JSONL"
fi

# ── Per-model loop: Tier 1+2 ─────────────────────────────────────────────

LLM_TIERS=""
if [[ -z "$TIER" ]]; then
  LLM_TIERS="1,2"
elif [[ "$TIER" == *"1"* || "$TIER" == *"2"* ]]; then
  # Extract tier 1 and 2 from the tier flag
  LLM_TIERS=""
  [[ "$TIER" == *"1"* ]] && LLM_TIERS="1"
  [[ "$TIER" == *"2"* ]] && LLM_TIERS="${LLM_TIERS:+$LLM_TIERS,}2"
fi

if [[ -n "$LLM_TIERS" ]]; then
  IFS=',' read -ra MODEL_LIST <<< "$MODELS"

  for model in "${MODEL_LIST[@]}"; do
    # Sanitize model name for filename
    SAFE_MODEL=$(echo "$model" | tr '/:' '-')
    echo ""
    echo "--- Model: $model ----"

    JSONL_OUT="$OUTPUT_DIR/raw/${SAFE_MODEL}.jsonl"
    CSV_OUT="$OUTPUT_DIR/raw/${SAFE_MODEL}.csv"

    "$PYTHON" "$HARNESS" \
      --model "$model" \
      --mode all \
      --tier "$LLM_TIERS" \
      --repeat "$REPEAT" \
      --jungle-url "$JUNGLE_URL" \
      --lazy-binary "$LAZY_BINARY" \
      --lazy-config "$LAZY_CONFIG" \
      --workdir "$REPO_ROOT" \
      --ollama-url "${OLLAMA_URL}/v1" \
      --prepare-fs \
      --strict-answers \
      --jsonl-out "$JSONL_OUT" \
      --csv-out "$CSV_OUT" \
      2>&1 || {
        echo "  WARNING: $model benchmark had failures" >&2
        EXIT_CODE=1
      }

    echo "  Results: $JSONL_OUT"
  done
fi

# ── Combine JSONL, generate summary ──────────────────────────────────────

echo ""
echo "================================================================"
echo " Generating combined summary..."
echo "================================================================"

cat "$OUTPUT_DIR"/raw/*.jsonl > "$OUTPUT_DIR/combined.jsonl" 2>/dev/null || true

COMBINED="$OUTPUT_DIR/combined.jsonl"
if [[ -f "$COMBINED" && -s "$COMBINED" ]]; then
  "$PYTHON" -c "
import json, sys
from pathlib import Path
from collections import defaultdict

rows = []
for line in Path('$COMBINED').read_text().strip().split('\n'):
    if line.strip():
        rows.append(json.loads(line))

if not rows:
    print('No data.')
    sys.exit(0)

# Separate by tier
by_tier = defaultdict(list)
for r in rows:
    by_tier[r.get('tier', 0)].append(r)

print()
print(f'Total runs: {len(rows)}')

# Tier 3 summary
t3 = by_tier.get(3, [])
if t3:
    p1 = sum(1 for r in t3 if r.get('precision_at_1'))
    p3 = sum(1 for r in t3 if r.get('precision_at_3'))
    n = len(t3)
    print(f'\nTier 3 - Search Quality ({n} queries):')
    print(f'  Precision@1: {p1}/{n} ({p1/n*100:.0f}%)')
    print(f'  Precision@3: {p3}/{n} ({p3/n*100:.0f}%)')

# Tier 1+2 summary per model
for tier in [1, 2]:
    items = by_tier.get(tier, [])
    if not items:
        continue
    print(f'\nTier {tier}:')

    by_model = defaultdict(list)
    for r in items:
        by_model[r.get('model', '?')].append(r)

    for model in sorted(by_model):
        model_items = by_model[model]
        coached = [r for r in model_items if r.get('coached')]
        natural = [r for r in model_items if not r.get('coached')]

        c_succ = sum(1 for r in coached if r.get('task_success'))
        n_succ = sum(1 for r in natural if r.get('task_success'))

        print(f'  {model}:')
        if coached:
            print(f'    Coached:  {c_succ}/{len(coached)} success')
        if natural:
            print(f'    Natural:  {n_succ}/{len(natural)} success')
" 2>&1
fi

echo ""
echo "================================================================"
echo " Done. Results in: $OUTPUT_DIR"
echo "================================================================"

exit $EXIT_CODE
