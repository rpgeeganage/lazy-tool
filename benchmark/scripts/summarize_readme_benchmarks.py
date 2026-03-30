#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import math
from collections import defaultdict
from pathlib import Path
from statistics import mean, median

def load_jsonl(path: Path) -> list[dict]:
    rows: list[dict] = []
    with path.open("r", encoding="utf-8") as f:
        for line in f:
            s = line.strip()
            if not s:
                continue
            rows.append(json.loads(s))
    return rows

def pct(n: int, d: int) -> float:
    return round((n / d) * 100, 1) if d else 0.0

def avg(values: list[float | int]) -> float:
    if not values:
        return 0.0
    return round(mean(values), 3)

def p95(values: list[float | int]) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    idx = max(0, math.ceil(0.95 * len(ordered)) - 1)
    return round(float(ordered[idx]), 3)

def summarize_group(rows: list[dict]) -> dict:
    runs = len(rows)
    success = sum(1 for r in rows if r.get("task_success"))
    pseudo = sum(1 for r in rows if r.get("pseudo_tool_text"))
    exceptions = sum(1 for r in rows if r.get("failure_reason") == "exception")
    usage_missing = sum(1 for r in rows if r.get("usage_missing"))
    input_tokens = [int(r.get("input_tokens", 0) or 0) for r in rows]
    output_tokens = [int(r.get("output_tokens", 0) or 0) for r in rows]
    total_tokens = [int(r.get("total_tokens", 0) or 0) for r in rows]
    durations = [float(r.get("duration_s", 0.0) or 0.0) for r in rows]
    tool_calls = [int(r.get("tool_call_count", 0) or 0) for r in rows]

    return {
        "runs": runs,
        "success_count": success,
        "success_rate_pct": pct(success, runs),
        "pseudo_tool_text_count": pseudo,
        "exception_count": exceptions,
        "usage_missing_count": usage_missing,
        "avg_input_tokens": avg(input_tokens),
        "avg_output_tokens": avg(output_tokens),
        "avg_total_tokens": avg(total_tokens),
        "median_total_tokens": round(float(median(total_tokens)), 3) if total_tokens else 0.0,
        "avg_duration_s": avg(durations),
        "p95_duration_s": p95(durations),
        "avg_tool_call_count": avg(tool_calls),
    }

def markdown_table(headers: list[str], rows: list[list[str]]) -> str:
    head = "| " + " | ".join(headers) + " |"
    sep = "| " + " | ".join(["---"] * len(headers)) + " |"
    body = "\n".join("| " + " | ".join(r) + " |" for r in rows)
    return "\n".join([head, sep, body]) if rows else "\n".join([head, sep])

def build_markdown(summary: dict, manifest: dict) -> str:
    no_tool = summary.get("no_tool_comparison", {})
    sections: list[str] = []

    sections.append("## Benchmark results")
    sections.append("")
    sections.append(
        f"_Model_: `{manifest.get('model', 'unknown')}`  \n"
        f"_Repeats per task_: `{manifest.get('repeat', 'unknown')}`  \n"
        f"_Generated at (UTC)_: `{manifest.get('timestamp_utc', 'unknown')}`"
    )
    sections.append("")

    if no_tool:
        sections.append("### Baseline token / latency comparison (`no_tool`)")
        rows = [[
            no_tool.get("model", ""),
            str(no_tool.get("runs", "")),
            f"{no_tool.get('jungle_avg_input_tokens', 0)}",
            f"{no_tool.get('lazy_avg_input_tokens', 0)}",
            f"{no_tool.get('input_token_reduction_pct', 0)}%",
            f"{no_tool.get('jungle_avg_duration_s', 0)}s",
            f"{no_tool.get('lazy_avg_duration_s', 0)}s",
            f"{no_tool.get('duration_reduction_pct', 0)}%",
        ]]
        sections.append(markdown_table(
            ["Model", "Runs", "Direct MCP avg input tokens", "lazy-tool avg input tokens", "Input token reduction", "Direct MCP avg latency", "lazy-tool avg latency", "Latency reduction"],
            rows,
        ))
        sections.append("")

    core_rows = []
    grouped = summary.get("grouped", {})
    preferred_order = [
        "no_tool",
        "search_tools_smoke",
        "ambiguous_search",
        "search_tools_prompt",
        "search_tools_resource",
        "everything_echo",
    ]
    task_order = [t for t in preferred_order if t in grouped] + [t for t in sorted(grouped) if t not in preferred_order]

    for task_name in task_order:
        labels = grouped[task_name]
        for label, stats in sorted(labels.items()):
            core_rows.append([
                task_name,
                label,
                str(stats["runs"]),
                f"{stats['success_count']}/{stats['runs']} ({stats['success_rate_pct']}%)",
                f"{stats['avg_total_tokens']}",
                f"{stats['avg_duration_s']}s",
                f"{stats['avg_tool_call_count']}",
                str(stats["exception_count"]),
            ])

    sections.append("### Task summary")
    sections.append(markdown_table(
        ["Task", "Mode", "Runs", "Success", "Avg total tokens", "Avg latency", "Avg tool calls", "Exceptions"],
        core_rows,
    ))
    sections.append("")

    sections.append("### Notes")
    sections.append("- Publish `no_tool` as the main token-saving claim.")
    sections.append("- Publish discovery/search tasks only if their success rates are clean enough for the README.")
    sections.append("- Treat `everything_echo` as optional until it is stable.")
    sections.append("- Record the repo commit when copying this into the README.")
    sections.append("")
    return "\n".join(sections)

def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--input-dir", required=True)
    ap.add_argument("--manifest", required=True)
    ap.add_argument("--markdown-out", required=True)
    ap.add_argument("--json-out", required=True)
    args = ap.parse_args()

    input_dir = Path(args.input_dir)
    manifest = json.loads(Path(args.manifest).read_text(encoding="utf-8"))

    rows: list[dict] = []
    for path in sorted(input_dir.glob("*.jsonl")):
        rows.extend(load_jsonl(path))

    grouped_rows: dict[str, dict[str, list[dict]]] = defaultdict(lambda: defaultdict(list))
    for row in rows:
        grouped_rows[row["task"]][row["label"]].append(row)

    grouped_summary: dict[str, dict[str, dict]] = {}
    for task, labels in grouped_rows.items():
        grouped_summary[task] = {}
        for label, items in labels.items():
            grouped_summary[task][label] = summarize_group(items)

    no_tool_comparison = {}
    if "no_tool" in grouped_summary:
        jungle = grouped_summary["no_tool"].get("mcpjungle_direct")
        lazy = grouped_summary["no_tool"].get("lazy_tool_stdio")
        if jungle and lazy:
            jungle_tokens = float(jungle["avg_input_tokens"])
            lazy_tokens = float(lazy["avg_input_tokens"])
            jungle_dur = float(jungle["avg_duration_s"])
            lazy_dur = float(lazy["avg_duration_s"])

            token_reduction = round(((jungle_tokens - lazy_tokens) / jungle_tokens) * 100, 1) if jungle_tokens else 0.0
            dur_reduction = round(((jungle_dur - lazy_dur) / jungle_dur) * 100, 1) if jungle_dur else 0.0

            no_tool_comparison = {
                "model": manifest.get("model"),
                "runs": min(jungle["runs"], lazy["runs"]),
                "jungle_avg_input_tokens": jungle["avg_input_tokens"],
                "lazy_avg_input_tokens": lazy["avg_input_tokens"],
                "input_token_reduction_pct": token_reduction,
                "jungle_avg_duration_s": jungle["avg_duration_s"],
                "lazy_avg_duration_s": lazy["avg_duration_s"],
                "duration_reduction_pct": dur_reduction,
            }

    summary = {
        "manifest": manifest,
        "grouped": grouped_summary,
        "no_tool_comparison": no_tool_comparison,
        "row_count": len(rows),
    }

    Path(args.json_out).write_text(json.dumps(summary, indent=2, ensure_ascii=False), encoding="utf-8")
    Path(args.markdown_out).write_text(build_markdown(summary, manifest), encoding="utf-8")

if __name__ == "__main__":
    main()
