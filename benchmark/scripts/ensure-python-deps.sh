#!/usr/bin/env bash
# Ensures Python benchmark dependencies are installed in a local venv.
#
# Sourced by suite scripts — not run directly.
#
# Behavior:
#   1. Creates benchmark/.venv if it doesn't exist
#   2. Uses uv if available, falls back to pip
#   3. Skips install if deps are already satisfied
#   4. Exports PYTHON pointing to the venv interpreter
#
# After sourcing, use $PYTHON instead of python3:
#   source benchmark/scripts/ensure-python-deps.sh "$REPO_ROOT"
#   $PYTHON benchmark/run_weak_model_benchmark.py ...

_REPO_ROOT="${1:?usage: source ensure-python-deps.sh <repo-root>}"
_VENV_DIR="$_REPO_ROOT/benchmark/.venv"
_REQ_FILE="$_REPO_ROOT/benchmark/requirements.txt"

# Create venv if missing
if [[ ! -d "$_VENV_DIR" ]]; then
  echo "Creating Python venv at $_VENV_DIR ..."
  python3 -m venv "$_VENV_DIR"
fi

PYTHON="$_VENV_DIR/bin/python"

if [[ ! -f "$PYTHON" ]]; then
  echo "ERROR: venv python not found at $PYTHON" >&2
  echo "Delete $_VENV_DIR and retry." >&2
  return 1 2>/dev/null || exit 1
fi

_py_bench_deps_ok() {
  # pydantic_core ships a native module; metadata can exist without the .so (broken/wrong arch).
  "$PYTHON" -c "import pydantic_core._pydantic_core; import pydantic_ai" 2>/dev/null
}

# Check if deps are already installed (fast path)
if _py_bench_deps_ok; then
  return 0 2>/dev/null || true
fi

echo "Installing Python dependencies..."

if command -v uv &>/dev/null; then
  echo "  (using uv)"
  uv pip install --python "$PYTHON" -r "$_REQ_FILE"
else
  echo "  (using pip)"
  "$PYTHON" -m pip install --quiet -r "$_REQ_FILE"
fi

# uv/pip may "audit" satisfied metadata without restoring missing wheels; force reinstall once.
if ! _py_bench_deps_ok; then
  echo "  deps still broken or incomplete; retrying with forced reinstall..."
  if command -v uv &>/dev/null; then
    uv pip install --python "$PYTHON" --reinstall -r "$_REQ_FILE"
  else
    "$PYTHON" -m pip install --force-reinstall --no-cache-dir -r "$_REQ_FILE"
  fi
fi

if ! _py_bench_deps_ok; then
  echo "ERROR: pydantic_ai still not importable after install." >&2
  echo "Check $_REQ_FILE and your Python environment." >&2
  echo "Hint: rm -rf $_VENV_DIR and retry." >&2
  return 1 2>/dev/null || exit 1
fi

echo "Python dependencies ready."
