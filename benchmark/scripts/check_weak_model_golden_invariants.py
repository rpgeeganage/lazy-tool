#!/usr/bin/env python3
"""Semantic checks for weak-model benchmark golden JSONL.

Complements validate_weak_model_jsonl schema checks.
Every non-empty row must declare a task that has a rule below -- no silent skips.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any, Callable


def _err(msg: str) -> None:
    print(msg, file=sys.stderr)


def _expect_single_tool_call(r: dict[str, Any]) -> str | None:
    if r.get("tool_call_count", 0) < 1:
        return "single_tool_call expects tool_call_count >= 1"
    if r.get("task_success") is not True:
        return "single_tool_call expects task_success = true"
    return None


def _expect_format_compliance(r: dict[str, Any]) -> str | None:
    if r.get("tool_call_count", 0) < 1:
        return "format_compliance expects tool_call_count >= 1"
    if r.get("task_success") is not True:
        return "format_compliance expects task_success = true"
    return None


def _expect_search_coached(r: dict[str, Any]) -> str | None:
    if r.get("tool_call_count", 0) < 1:
        return "search_coached expects tool_call_count >= 1"
    tool_names = [t.lower() for t in (r.get("tool_names") or [])]
    if not any("search_tools" in t for t in tool_names):
        return "search_coached expects 'search_tools' in tool_names"
    return None


def _expect_search_natural(r: dict[str, Any]) -> str | None:
    if r.get("tool_call_count", 0) < 1:
        return "search_natural expects tool_call_count >= 1"
    if r.get("task_success") is not True:
        return "search_natural expects task_success = true"
    return None


def _expect_search_invoke_coached(r: dict[str, Any]) -> str | None:
    if r.get("tool_call_count", 0) < 2:
        return "search_invoke_coached expects tool_call_count >= 2"
    if r.get("task_success") is not True:
        return "search_invoke_coached expects task_success = true"
    return None


def _expect_search_invoke_natural(r: dict[str, Any]) -> str | None:
    if r.get("tool_call_count", 0) < 1:
        return "search_invoke_natural expects tool_call_count >= 1"
    if r.get("task_success") is not True:
        return "search_invoke_natural expects task_success = true"
    return None


def _expect_search_precision(r: dict[str, Any]) -> str | None:
    if not isinstance(r.get("precision_at_1"), bool):
        return "search_precision expects precision_at_1 to be bool"
    query = r.get("search_query", "")
    if not isinstance(query, str) or not query:
        return "search_precision expects search_query to be non-empty string"
    return None


RULES: dict[str, Callable[[dict[str, Any]], str | None]] = {
    "single_tool_call": _expect_single_tool_call,
    "format_compliance": _expect_format_compliance,
    "search_coached": _expect_search_coached,
    "search_natural": _expect_search_natural,
    "search_invoke_coached": _expect_search_invoke_coached,
    "search_invoke_natural": _expect_search_invoke_natural,
    "search_precision": _expect_search_precision,
}


def _check_row(row: Any, line_no: int) -> list[str]:
    errs: list[str] = []
    if not isinstance(row, dict):
        return [f"line {line_no}: expected object"]
    task = row.get("task")
    if not isinstance(task, str):
        return [f"line {line_no}: task must be str"]
    fn = RULES.get(task)
    if fn is None:
        return [
            f"line {line_no}: unknown task {task!r} -- add a rule in RULES or drop the row from golden",
        ]
    msg = fn(row)
    if msg:
        errs.append(f"line {line_no} task={task}: {msg}")
    if row.get("task_success") is True and row.get("failure_reason") is not None:
        errs.append(
            f"line {line_no}: task_success true requires failure_reason null, got {row.get('failure_reason')!r}",
        )
    if row.get("task_success") is False and row.get("failure_reason") is None:
        errs.append(f"line {line_no}: task_success false requires failure_reason string (golden failure rows)")
    return errs


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("file", type=Path, help="JSONL golden file")
    args = ap.parse_args()
    path: Path = args.file
    if not path.is_file():
        _err(f"{path}: not a file")
        return 1
    text = path.read_text(encoding="utf-8")
    all_errs: list[str] = []
    any_row = False
    for i, line in enumerate(text.splitlines(), start=1):
        line = line.strip()
        if not line:
            continue
        any_row = True
        try:
            obj = json.loads(line)
        except json.JSONDecodeError as e:
            all_errs.append(f"{path}:{i}: {e}")
            continue
        all_errs.extend(_check_row(obj, i))
    if not any_row:
        _err(f"{path}: no JSONL rows")
        return 1
    if all_errs:
        for e in all_errs:
            _err(e)
        return 1
    print(f"OK: {sum(1 for l in text.splitlines() if l.strip())} rows pass semantic checks.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
