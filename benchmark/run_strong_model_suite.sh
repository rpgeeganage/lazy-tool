#!/usr/bin/env bash
set -euo pipefail

# Strong-model benchmark suite for lazy-tool.
#
# Runs the multi-provider benchmark across Claude Sonnet, GPT-4.1-mini, and Groq
# in all three modes (baseline, search, direct) to prove:
#   1. Direct mode: <5% overhead vs baseline on strong models
#   2. Search mode: measurable token savings on all models
#   3. Routed tasks: >90% success rate
#
# Usage:
#   ./benchmark/run_strong_model_suite.sh
#   ./benchmark/run_strong_model_suite.sh --repeat 10 --output-dir ./results
#   ./benchmark/run_strong_model_suite.sh --provider anthropic --tasks routed_echo
#
# Requirements:
#   - At least one of: ANTHROPIC_API_KEY, OPENAI_API_KEY, GROQ_API_KEY
#   - MCPJungle running locally (for baseline mode)
#   - lazy-tool built (make build) and indexed (lazy-tool reindex)
#   - Python 3.11+ (dependencies are auto-installed into benchmark/.venv)

REPEAT="${REPEAT:-5}"
REPO_ROOT=""
OUTPUT_DIR=""
LAZY_CONFIG=""
JUNGLE_URL="http://127.0.0.1:8080/mcp"
SKIP_BUILD="false"
PROVIDERS=""
TASKS=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repeat)         REPEAT="${2:?missing value}"; shift 2 ;;
    --repo-root)      REPO_ROOT="${2:?missing value}"; shift 2 ;;
    --output-dir)     OUTPUT_DIR="${2:?missing value}"; shift 2 ;;
    --lazy-config)    LAZY_CONFIG="${2:?missing value}"; shift 2 ;;
    --jungle-url)     JUNGLE_URL="${2:?missing value}"; shift 2 ;;
    --skip-build)     SKIP_BUILD="true"; shift ;;
    --provider)       PROVIDERS="${2:?missing value}"; shift 2 ;;
    --tasks)          TASKS="${2:?missing value}"; shift 2 ;;
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
  LAZY_CONFIG="$REPO_ROOT/benchmark/configs/mcpjungle-lazy-tool.yaml"
fi

# Resolve output directory
if [[ -z "$OUTPUT_DIR" ]]; then
  OUTPUT_DIR="$REPO_ROOT/benchmark-results/$(date +%Y%m%d-%H%M%S)"
fi
mkdir -p "$OUTPUT_DIR/raw"

LAZY_BINARY="$REPO_ROOT/bin/lazy-tool"
HARNESS="$REPO_ROOT/benchmark/run_multi_provider_benchmark.py"

# ── Python dependencies ──────────────────────────────────────────────────

# shellcheck source=scripts/ensure-python-deps.sh
source "$REPO_ROOT/benchmark/scripts/ensure-python-deps.sh" "$REPO_ROOT"

# ── Build ──────────────────────────────────────────────────────────────────

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

# ── Reindex ────────────────────────────────────────────────────────────────

echo "Reindexing catalog..."
LAZY_TOOL_CONFIG="$LAZY_CONFIG" "$LAZY_BINARY" reindex 2>&1 || {
  echo "Reindex failed." >&2
  exit 1
}

# ── Prepare filesystem fixture ──────────────────────────────────────────────

FS_ROOT="/tmp/lazy-tool-mcpjungle-fs"
mkdir -p "$FS_ROOT/nested"
echo "hello from lazy-tool benchmark" > "$FS_ROOT/notes.txt"
echo '{"tasks":["benchmark","search","compare"]}' > "$FS_ROOT/todo.json"
echo "nested file" > "$FS_ROOT/nested/info.txt"

# ── Detect available providers ──────────────────────────────────────────────

if [[ -z "$PROVIDERS" ]]; then
  AVAILABLE=""
  [[ -n "${ANTHROPIC_API_KEY:-}" ]] && AVAILABLE="${AVAILABLE:+$AVAILABLE,}anthropic"
  [[ -n "${OPENAI_API_KEY:-}" ]] && AVAILABLE="${AVAILABLE:+$AVAILABLE,}openai"
  [[ -n "${GROQ_API_KEY:-}" ]] && AVAILABLE="${AVAILABLE:+$AVAILABLE,}groq"
  if [[ -z "$AVAILABLE" ]]; then
    echo "No API keys found. Set at least one of:" >&2
    echo "  ANTHROPIC_API_KEY, OPENAI_API_KEY, GROQ_API_KEY" >&2
    exit 1
  fi
  PROVIDERS="$AVAILABLE"
fi

echo ""
echo "═══════════════════════════════════════════════════════════════"
echo " lazy-tool Strong Model Benchmark Suite"
echo "═══════════════════════════════════════════════════════════════"
echo " Providers:  $PROVIDERS"
echo " Repeat:     $REPEAT"
echo " Output:     $OUTPUT_DIR"
echo " Jungle:     $JUNGLE_URL"
echo " Config:     $LAZY_CONFIG"
echo "═══════════════════════════════════════════════════════════════"
echo ""

# ── Manifest ───────────────────────────────────────────────────────────────

cat > "$OUTPUT_DIR/manifest.json" <<MANIFEST
{
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "repeat": $REPEAT,
  "providers": "$(echo "$PROVIDERS" | tr ',' '", "')",
  "lazy_tool_version": "$("$LAZY_BINARY" version 2>&1 || echo 'unknown')",
  "jungle_url": "$JUNGLE_URL",
  "lazy_config": "$LAZY_CONFIG"
}
MANIFEST

# ── Run benchmarks per provider ────────────────────────────────────────────

IFS=',' read -ra PROVIDER_LIST <<< "$PROVIDERS"

EXIT_CODE=0

for provider in "${PROVIDER_LIST[@]}"; do
  echo ""
  echo "─── Provider: $provider ────────────────────────────────────────"

  TASK_ARGS=""
  if [[ -n "$TASKS" ]]; then
    TASK_ARGS="--tasks $TASKS"
  fi

  JSONL_OUT="$OUTPUT_DIR/raw/${provider}.jsonl"
  CSV_OUT="$OUTPUT_DIR/raw/${provider}.csv"

  "$PYTHON" "$HARNESS" \
    --provider "$provider" \
    --mode all \
    --repeat "$REPEAT" \
    --jungle-url "$JUNGLE_URL" \
    --lazy-binary "$LAZY_BINARY" \
    --lazy-config "$LAZY_CONFIG" \
    --workdir "$REPO_ROOT" \
    --prepare-fs \
    --strict-answers \
    --jsonl-out "$JSONL_OUT" \
    --csv-out "$CSV_OUT" \
    $TASK_ARGS \
    2>&1 || {
      echo "  WARNING: $provider benchmark had failures" >&2
      EXIT_CODE=1
    }

  echo "  Results: $JSONL_OUT"
done

# ── Generate combined summary ──────────────────────────────────────────────

echo ""
echo "═══════════════════════════════════════════════════════════════"
echo " Generating combined summary..."
echo "═══════════════════════════════════════════════════════════════"

# Combine all JSONL files
cat "$OUTPUT_DIR"/raw/*.jsonl > "$OUTPUT_DIR/combined.jsonl" 2>/dev/null || true

COMBINED="$OUTPUT_DIR/combined.jsonl"
if [[ -f "$COMBINED" && -s "$COMBINED" ]]; then
  # Generate summary with Python
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

# Group by provider × mode × task
groups = defaultdict(list)
for r in rows:
    key = (r.get('provider','?'), r.get('bench_mode','?'), r.get('task','?'))
    groups[key].append(r)

print()
print(f'Total runs: {len(rows)}')
print()
print(f'{\"Provider\":<12} {\"Mode\":<10} {\"Task\":<22} {\"N\":>3} {\"Success\":>8} {\"Avg Tokens\":>10} {\"Avg Lat\":>8}')
print('-' * 78)

for (prov, mode, task), items in sorted(groups.items()):
    n = len(items)
    succ = sum(1 for x in items if x.get('task_success'))
    avg_tok = sum(x.get('total_tokens', 0) for x in items) / n
    avg_lat = sum(x.get('duration_s', 0) for x in items) / n
    print(f'{prov:<12} {mode:<10} {task:<22} {n:>3} {succ:>3}/{n:<3} {avg_tok:>10.0f} {avg_lat:>7.2f}s')

# Mode comparison per provider
print()
print('Mode comparison (per provider):')
by_prov_mode = defaultdict(list)
for r in rows:
    by_prov_mode[(r.get('provider','?'), r.get('bench_mode','?'))].append(r)

for prov in sorted(set(r.get('provider','?') for r in rows)):
    bl = by_prov_mode.get((prov, 'baseline'), [])
    sr = by_prov_mode.get((prov, 'search'), [])
    di = by_prov_mode.get((prov, 'direct'), [])
    print(f'  {prov}:')
    for label, items in [('baseline', bl), ('search', sr), ('direct', di)]:
        if not items:
            continue
        n = len(items)
        succ = sum(1 for x in items if x.get('task_success'))
        avg_tok = sum(x.get('total_tokens', 0) for x in items) / n
        avg_lat = sum(x.get('duration_s', 0) for x in items) / n
        print(f'    {label:>8}: {succ}/{n} success, avg {avg_tok:.0f} tokens, {avg_lat:.2f}s')
    if bl and di:
        bl_tok = sum(x.get('total_tokens', 0) for x in bl) / len(bl)
        di_tok = sum(x.get('total_tokens', 0) for x in di) / len(di)
        if bl_tok > 0:
            overhead = ((di_tok - bl_tok) / bl_tok) * 100
            print(f'    direct vs baseline: {overhead:+.1f}% tokens')
" 2>&1
fi

echo ""
echo "═══════════════════════════════════════════════════════════════"
echo " Done. Results in: $OUTPUT_DIR"
echo "═══════════════════════════════════════════════════════════════"

exit $EXIT_CODE
