#!/usr/bin/env python3
"""
Schema validation for weak-model benchmark JSONL output.

Validates that each row has the expected fields and types.
Used in CI to catch harness regressions.

Usage:
  python benchmark/scripts/validate_weak_model_jsonl.py benchmark/golden/weak_model_sample_rows.jsonl
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

REQUIRED_FIELDS = {
    "run_index": int,
    "label": str,
    "provider": str,
    "model": str,
    "task": str,
    "bench_mode": str,
    "input_tokens": int,
    "output_tokens": int,
    "total_tokens": int,
    "usage_missing": bool,
    "pseudo_tool_text": bool,
    "duration_s": (int, float),
    "output_preview": str,
    "tool_execution_success": bool,
    "answer_format_success": bool,
    "used_expected_tool_family": bool,
    "task_success": bool,
    "tool_call_count": int,
    "tool_names": list,
    "tier": int,
    "coached": bool,
}

VALID_BENCH_MODES = {"baseline", "search", "direct"}
VALID_PROVIDERS = {"ollama", "deterministic"}

VALID_TASKS = {
    "single_tool_call", "format_compliance",
    "search_coached", "search_natural",
    "search_invoke_coached", "search_invoke_natural",
    "search_precision",
}

# Extra fields required for search_precision rows
PRECISION_FIELDS = {
    "precision_at_1": bool,
    "precision_at_3": bool,
    "search_query": str,
    "expected_tool": str,
}


def validate_row(idx: int, row: dict) -> list[str]:
    errors: list[str] = []

    for field, expected_type in REQUIRED_FIELDS.items():
        if field not in row:
            errors.append(f"row {idx}: missing required field '{field}'")
            continue
        val = row[field]
        if val is None and field == "failure_reason":
            continue
        if not isinstance(val, expected_type):
            errors.append(
                f"row {idx}: field '{field}' has type {type(val).__name__}, "
                f"expected {expected_type}"
            )

    bench_mode = row.get("bench_mode", "")
    if bench_mode not in VALID_BENCH_MODES:
        errors.append(f"row {idx}: invalid bench_mode '{bench_mode}'")

    provider = row.get("provider", "")
    if provider not in VALID_PROVIDERS:
        errors.append(f"row {idx}: invalid provider '{provider}'")

    task = row.get("task", "")
    if task not in VALID_TASKS:
        errors.append(f"row {idx}: unknown task '{task}'")

    # Consistency checks
    if row.get("task_success") and row.get("failure_reason"):
        errors.append(f"row {idx}: task_success=true but failure_reason is set")

    # search_precision rows need extra fields
    if task == "search_precision":
        for field, expected_type in PRECISION_FIELDS.items():
            if field not in row:
                errors.append(f"row {idx}: search_precision row missing '{field}'")
                continue
            val = row[field]
            if not isinstance(val, expected_type):
                errors.append(
                    f"row {idx}: precision field '{field}' has type {type(val).__name__}, "
                    f"expected {expected_type.__name__}"
                )

    return errors


def main() -> None:
    if len(sys.argv) < 2:
        print("Usage: validate_weak_model_jsonl.py <path.jsonl>", file=sys.stderr)
        sys.exit(2)

    path = Path(sys.argv[1])
    if not path.exists():
        print(f"File not found: {path}", file=sys.stderr)
        sys.exit(1)

    all_errors: list[str] = []
    row_count = 0

    for line_no, line in enumerate(path.read_text().strip().split("\n"), start=1):
        if not line.strip():
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError as e:
            all_errors.append(f"line {line_no}: invalid JSON: {e}")
            continue
        row_count += 1
        all_errors.extend(validate_row(line_no, row))

    if all_errors:
        print(f"FAIL: {len(all_errors)} validation error(s) in {row_count} rows:", file=sys.stderr)
        for err in all_errors:
            print(f"  {err}", file=sys.stderr)
        sys.exit(1)

    print(f"OK: {row_count} rows validated successfully.")


if __name__ == "__main__":
    main()
