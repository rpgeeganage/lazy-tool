#!/usr/bin/env python3
"""
Multi-provider benchmark for lazy-tool: Anthropic (Claude), OpenAI (GPT-4), Groq (Llama).

Compares three modes per model:
  1. baseline  — all upstream tools exposed directly via MCPJungle (no lazy-tool)
  2. search    — lazy-tool's 5 meta-tool surface (search → invoke flow)
  3. direct    — lazy-tool as transparent aggregator (all tools proxied as first-class)

Task categories:
  - no_tool            — baseline token cost
  - search_tools_smoke — basic search discovery (search mode only)
  - routed_echo        — search → invoke → verify (search mode); direct call (direct mode)
  - routed_file_read   — search → read file → verify content
  - routed_prompt      — search → get prompt → verify response
  - ambiguous_search   — overloaded query resolution (search mode only)

Requirements:
  - One of: ANTHROPIC_API_KEY, OPENAI_API_KEY, GROQ_API_KEY
  - pydantic-ai-slim[groq,anthropic,openai,mcp]
  - MCPJungle running for baseline mode
  - lazy-tool built and indexed for search/direct modes

Examples:
  python benchmark/run_multi_provider_benchmark.py --provider anthropic --model claude-sonnet-4-20250514 --mode search
  python benchmark/run_multi_provider_benchmark.py --provider openai --model gpt-4.1-mini --mode direct
  python benchmark/run_multi_provider_benchmark.py --provider groq --model llama-3.3-70b-versatile --mode all --repeat 5
  python benchmark/run_multi_provider_benchmark.py --provider anthropic --task routed_echo --mode all
"""

from __future__ import annotations

import argparse
import asyncio
import csv
import json
import os
import re
import statistics
import sys
import time
from pathlib import Path
from typing import Any

_REPO_ROOT = Path(__file__).resolve().parent.parent
_BENCHMARK_DIR = Path(__file__).resolve().parent
_FS_ROOT = Path("/tmp/lazy-tool-mcpjungle-fs")

# ── Provider configuration ──────────────────────────────────────────────────

PROVIDERS = {
    "anthropic": {
        "env_key": "ANTHROPIC_API_KEY",
        "default_model": "claude-sonnet-4-20250514",
        "prefix": "anthropic:",
    },
    "openai": {
        "env_key": "OPENAI_API_KEY",
        "default_model": "gpt-4.1-mini",
        "prefix": "openai:",
    },
    "groq": {
        "env_key": "GROQ_API_KEY",
        "default_model": "llama-3.3-70b-versatile",
        "prefix": "groq:",
    },
}

# ── Instructions ─────────────────────────────────────────────────────────────

_INSTRUCTIONS_STRUCTURED_TOOLS_ONLY = (
    "When you need a tool, you MUST use the model's structured tool/function-calling channel. "
    "Never simulate tools by writing tags or pseudo-syntax in plain text "
    "(for example no <function=...>, no XML tool blocks, no markdown code fences that fake a call). "
    "If tools are available and required, use a real tool call instead of describing the call."
)

# ── Tasks ────────────────────────────────────────────────────────────────────

TASKS: dict[str, dict[str, Any]] = {
    "no_tool": {
        "prompt": "Reply with exactly the single word: ok. Do not call any tools unless required.",
        "expect_tool_calls": False,
        "expected_tool_hints": [],
        "description": "Baseline token cost when no tool should be needed.",
        "modes": ["baseline", "search", "direct"],
    },
    "search_tools_smoke": {
        "prompt": (
            "You must call the MCP tool named search_tools. "
            "Use query \"echo\" and limit 5. "
            "After the result returns, reply with exactly one line starting with SEARCH_OK "
            "followed by a short summary of how many hits were returned."
        ),
        "expect_tool_calls": True,
        "expected_tool_hints": ["search_tools"],
        "description": "Validate lazy-tool search_tools execution.",
        "modes": ["search", "hybrid"],
    },
    "ambiguous_search": {
        "prompt": (
            "Call search_tools with query \"echo\" and limit 3. "
            "Reply with one line AMBIG_OK and the number of results returned (digit only after AMBIG_OK)."
        ),
        "expect_tool_calls": True,
        "expected_tool_hints": ["search_tools"],
        "description": "Discovery under an ambiguous query string.",
        "modes": ["search", "hybrid"],
    },
    "routed_echo": {
        "prompt": {
            "search": (
                "First, call search_tools with query \"echo\" and limit 3. "
                "Then pick the best matching tool result and call invoke_proxy_tool "
                "with the proxy_tool_name from the search result and input {\"message\": \"benchmark-test\"}. "
                "Reply with ECHO_OK followed by the echoed content."
            ),
            "direct": (
                "Call the echo tool with input {\"message\": \"benchmark-test\"}. "
                "Reply with ECHO_OK followed by the echoed content."
            ),
            "baseline": (
                "Call the echo tool with input {\"message\": \"benchmark-test\"}. "
                "Reply with ECHO_OK followed by the echoed content."
            ),
        },
        "expect_tool_calls": True,
        "expected_tool_hints": ["echo", "invoke_proxy_tool", "search_tools"],
        "description": "Routed tool: search → invoke echo → verify output contains expected text.",
        "modes": ["baseline", "search", "direct"],
        "verify": lambda output: "benchmark-test" in output.lower() or "echo_ok" in output.lower(),
    },
    "routed_file_read": {
        "prompt": {
            "search": (
                "First, call search_tools with query \"read file\" and limit 5. "
                "Then use invoke_proxy_tool to read the file /tmp/lazy-tool-mcpjungle-fs/notes.txt. "
                "Reply with FILE_OK followed by the file content."
            ),
            "direct": (
                "Read the file /tmp/lazy-tool-mcpjungle-fs/notes.txt using available tools. "
                "Reply with FILE_OK followed by the file content."
            ),
            "baseline": (
                "Read the file /tmp/lazy-tool-mcpjungle-fs/notes.txt using available tools. "
                "Reply with FILE_OK followed by the file content."
            ),
        },
        "expect_tool_calls": True,
        "expected_tool_hints": ["filesystem", "read", "file", "invoke_proxy_tool", "search_tools"],
        "description": "Routed file read: search → read known file → verify content.",
        "modes": ["baseline", "search", "direct"],
        "verify": lambda output: "hello from lazy-tool benchmark" in output.lower(),
    },
    "routed_prompt": {
        "prompt": {
            "search": (
                "First, call search_tools with query \"prompt\" and limit 5. "
                "Then use get_proxy_prompt with the best matching prompt proxy_tool_name. "
                "Reply with PROMPT_OK followed by a brief description of what you received."
            ),
            "direct": (
                "Get a prompt from the available prompts. "
                "Reply with PROMPT_OK followed by a brief description of what you received."
            ),
            "baseline": (
                "Get a prompt from the available prompts. "
                "Reply with PROMPT_OK followed by a brief description of what you received."
            ),
        },
        "expect_tool_calls": True,
        "expected_tool_hints": ["prompt", "get_proxy_prompt", "search_tools"],
        "description": "Routed prompt: search → get prompt → verify response.",
        "modes": ["baseline", "search", "direct"],
        "verify": lambda output: "prompt_ok" in output.lower(),
    },
}

# ── Pseudo-tool detection ────────────────────────────────────────────────────

_PSEUDO_TOOL_PATTERNS = [
    re.compile(r"<function=", re.IGNORECASE),
    re.compile(r"</function>", re.IGNORECASE),
    re.compile(r"assistant\s+to=functions?\.", re.IGNORECASE),
    re.compile(r"invoke_proxy_tool\s*\(", re.IGNORECASE),
    re.compile(r"search_tools\s*\(", re.IGNORECASE),
    re.compile(r"<\|tool", re.IGNORECASE),
    re.compile(r"\btool_call\b", re.IGNORECASE),
    re.compile(r'"name"\s*:\s*"[^"]+"\s*,\s*"arguments"\s*:', re.IGNORECASE),
]


def _looks_like_pseudo_tool_output(text: str) -> bool:
    s = text.strip()
    if not s:
        return False
    return any(p.search(s) for p in _PSEUDO_TOOL_PATTERNS)


def _safe_int(v: Any) -> int:
    try:
        return int(v or 0)
    except Exception:
        return 0


# ── Tool stats extraction ────────────────────────────────────────────────────

def _tool_stats(result: object) -> dict[str, Any]:
    """Inspect PydanticAI run history for real structured tool calls."""
    from pydantic_ai.messages import ModelResponse, ToolCallPart

    names: list[str] = []
    for msg in result.all_messages():
        if isinstance(msg, ModelResponse):
            for part in getattr(msg, "parts", []):
                if isinstance(part, ToolCallPart):
                    names.append(part.tool_name)

    return {
        "tool_call_count": len(names),
        "tool_names": names,
    }


# ── Answer format evaluation ────────────────────────────────────────────────

def _evaluate_answer_format(task_name: str, output_preview: str, strict: bool) -> bool:
    output_lower = output_preview.lower()
    task = TASKS[task_name]

    # Custom verify function
    if "verify" in task:
        return task["verify"](output_preview)

    if task_name == "no_tool":
        ok = output_lower.strip().startswith("ok")
        return ok and (not strict or len(output_lower.strip()) <= 8)
    if task_name == "search_tools_smoke":
        ok = "search_ok" in output_lower
        if strict and ok:
            return bool(re.search(r"search_ok\D", output_preview, re.IGNORECASE))
        return ok
    if task_name == "ambiguous_search":
        ok = "ambig_ok" in output_lower
        if strict and ok:
            return bool(re.search(r"ambig_ok\D+\d", output_preview, re.IGNORECASE))
        return ok
    if task_name == "routed_echo":
        return "echo_ok" in output_lower or "benchmark-test" in output_lower
    if task_name == "routed_file_read":
        return "file_ok" in output_lower and "hello from lazy-tool benchmark" in output_lower
    if task_name == "routed_prompt":
        return "prompt_ok" in output_lower
    return True


def _used_expected_tool_family(task_name: str, tool_names: list[str]) -> bool:
    cfg = TASKS[task_name]
    expect_tool_calls = bool(cfg["expect_tool_calls"])
    expected_tool_hints: list[str] = list(cfg["expected_tool_hints"])
    tool_names_lower = [t.lower() for t in tool_names]

    if not expected_tool_hints:
        return not expect_tool_calls

    return any(
        hint.lower() in tool_name
        for hint in expected_tool_hints
        for tool_name in tool_names_lower
    )


def _tool_execution_success(task_name: str, tool_names: list[str], pseudo_tool_text: bool) -> bool:
    expect_tool_calls = bool(TASKS[task_name]["expect_tool_calls"])
    if expect_tool_calls:
        return len(tool_names) > 0 and not pseudo_tool_text
    return len(tool_names) == 0 and not pseudo_tool_text


def _failure_reason(
    *,
    task_name: str,
    tool_names: list[str],
    output_preview: str,
    answer_format_success: bool,
    expected_tool_family: bool,
) -> str | None:
    expect_tool_calls = bool(TASKS[task_name]["expect_tool_calls"])
    pseudo_tool_text = _looks_like_pseudo_tool_output(output_preview)

    if expect_tool_calls and len(tool_names) == 0 and pseudo_tool_text:
        return "pseudo_tool_call_text"
    if expect_tool_calls and len(tool_names) == 0:
        return "no_tool_call"
    if expect_tool_calls and len(tool_names) > 0 and not expected_tool_family:
        return "unexpected_tool_family"
    if not answer_format_success:
        return "answer_format_failed"
    return None


# ── Exception row builder ────────────────────────────────────────────────────

def _exception_row(
    *,
    run_index: int,
    label: str,
    provider: str,
    model: str,
    task_name: str,
    bench_mode: str,
    exc: Exception,
    config_path: str | None = None,
) -> dict[str, Any]:
    return {
        "run_index": run_index,
        "label": label,
        "provider": provider,
        "model": model,
        "task": task_name,
        "bench_mode": bench_mode,
        "input_tokens": 0,
        "output_tokens": 0,
        "total_tokens": 0,
        "usage_missing": True,
        "pseudo_tool_text": False,
        "duration_s": 0.0,
        "output_preview": "",
        "tool_execution_success": False,
        "answer_format_success": False,
        "used_expected_tool_family": False,
        "task_success": False,
        "failure_reason": "exception",
        "exception_type": type(exc).__name__,
        "exception_message": str(exc)[:500],
        "tool_call_count": 0,
        "tool_names": [],
        "config_path": config_path,
    }


# ── Core agent runner ────────────────────────────────────────────────────────

def _get_prompt(task_name: str, bench_mode: str) -> str:
    """Resolve the prompt for a task, supporting per-mode prompts."""
    task = TASKS[task_name]
    prompt = task["prompt"]
    if isinstance(prompt, dict):
        return prompt.get(bench_mode, prompt.get("baseline", ""))
    return prompt


async def _run_agent(
    *,
    label: str,
    model: str,
    prompt: str,
    mcp_server: object,
    max_tokens: int,
    task_name: str,
    debug_parts: bool,
    run_index: int,
    strict_answers: bool,
    bench_mode: str,
    provider: str,
) -> dict[str, Any]:
    from pydantic_ai import Agent

    expect_tools = bool(TASKS[task_name]["expect_tool_calls"])
    agent_kw: dict[str, Any] = {"model": model, "toolsets": [mcp_server]}
    if expect_tools:
        agent_kw["instructions"] = _INSTRUCTIONS_STRUCTURED_TOOLS_ONLY
        agent_kw["retries"] = 2

    agent = Agent(**agent_kw)

    started = time.perf_counter()
    async with agent:
        result = await agent.run(prompt, model_settings={"max_tokens": max_tokens})
    duration_s = round(time.perf_counter() - started, 3)

    usage = result.usage()
    inp = _safe_int(getattr(usage, "input_tokens", 0))
    out = _safe_int(getattr(usage, "output_tokens", 0))
    usage_missing = inp == 0 and out == 0

    stats = _tool_stats(result)
    output_preview = (str(result.output) if result.output is not None else "")[:800]
    pseudo_tool_text = _looks_like_pseudo_tool_output(output_preview)

    answer_format_success = _evaluate_answer_format(task_name, output_preview, strict_answers)
    expected_tool_family = _used_expected_tool_family(task_name, stats["tool_names"])
    tool_execution_success = _tool_execution_success(task_name, stats["tool_names"], pseudo_tool_text)
    failure_reason_val = _failure_reason(
        task_name=task_name,
        tool_names=stats["tool_names"],
        output_preview=output_preview,
        answer_format_success=answer_format_success,
        expected_tool_family=expected_tool_family,
    )
    task_success = (
        tool_execution_success
        and answer_format_success
        and not pseudo_tool_text
        and expected_tool_family
        and failure_reason_val is None
    )

    row = {
        "run_index": run_index,
        "label": label,
        "provider": provider,
        "model": model,
        "task": task_name,
        "bench_mode": bench_mode,
        "strict_answers": strict_answers,
        "input_tokens": inp,
        "output_tokens": out,
        "total_tokens": inp + out,
        "usage_missing": usage_missing,
        "pseudo_tool_text": pseudo_tool_text,
        "duration_s": duration_s,
        "output_preview": output_preview,
        "tool_execution_success": tool_execution_success,
        "answer_format_success": answer_format_success,
        "used_expected_tool_family": expected_tool_family,
        "task_success": task_success,
        "failure_reason": failure_reason_val,
        **stats,
    }

    if debug_parts:
        from pydantic_ai.messages import ModelRequest, ModelResponse
        parts_debug = []
        for idx, msg in enumerate(result.all_messages()):
            entry = {"index": idx, "message_class": type(msg).__name__, "parts": []}
            if isinstance(msg, (ModelRequest, ModelResponse)):
                for part in getattr(msg, "parts", []):
                    part_info = {"part_class": type(part).__name__}
                    if hasattr(part, "tool_name"):
                        part_info["tool_name"] = getattr(part, "tool_name")
                    if hasattr(part, "content"):
                        part_info["content_preview"] = str(getattr(part, "content"))[:200]
                    entry["parts"].append(part_info)
            parts_debug.append(entry)
        row["debug_message_parts"] = parts_debug

    return row


# ── Mode runners ─────────────────────────────────────────────────────────────

async def _run_baseline(
    *,
    jungle_url: str,
    model: str,
    task_name: str,
    max_tokens: int,
    debug_parts: bool,
    run_index: int,
    strict_answers: bool,
    provider: str,
) -> dict[str, Any]:
    """Baseline: all upstream tools exposed directly via MCPJungle (no lazy-tool)."""
    from pydantic_ai.mcp import MCPServerStreamableHTTP

    prompt = _get_prompt(task_name, "baseline")
    server = MCPServerStreamableHTTP(jungle_url, timeout=120.0, read_timeout=600.0)
    row = await _run_agent(
        label="baseline",
        model=model,
        prompt=prompt,
        mcp_server=server,
        max_tokens=max_tokens,
        task_name=task_name,
        debug_parts=debug_parts,
        run_index=run_index,
        strict_answers=strict_answers,
        bench_mode="baseline",
        provider=provider,
    )
    row["config_path"] = None
    return row


async def _run_search(
    *,
    lazy_binary: Path,
    lazy_config: Path,
    workdir: Path,
    model: str,
    task_name: str,
    max_tokens: int,
    debug_parts: bool,
    run_index: int,
    strict_answers: bool,
    provider: str,
) -> dict[str, Any]:
    """Search mode: lazy-tool's 5 meta-tool surface via stdio."""
    from pydantic_ai.mcp import MCPServerStdio

    if not lazy_binary.is_file():
        raise FileNotFoundError(f"lazy-tool binary not found: {lazy_binary}")
    if not lazy_config.is_file():
        raise FileNotFoundError(f"config not found: {lazy_config}")

    prompt = _get_prompt(task_name, "search")
    env = {**os.environ, "LAZY_TOOL_CONFIG": str(lazy_config.resolve())}
    server = MCPServerStdio(
        str(lazy_binary.resolve()),
        ["serve", "--mode", "search"],
        env=env,
        cwd=workdir,
        timeout=120.0,
        read_timeout=600.0,
    )
    row = await _run_agent(
        label="search",
        model=model,
        prompt=prompt,
        mcp_server=server,
        max_tokens=max_tokens,
        task_name=task_name,
        debug_parts=debug_parts,
        run_index=run_index,
        strict_answers=strict_answers,
        bench_mode="search",
        provider=provider,
    )
    row["config_path"] = str(lazy_config.resolve())
    return row


async def _run_direct(
    *,
    lazy_binary: Path,
    lazy_config: Path,
    workdir: Path,
    model: str,
    task_name: str,
    max_tokens: int,
    debug_parts: bool,
    run_index: int,
    strict_answers: bool,
    provider: str,
) -> dict[str, Any]:
    """Direct mode: lazy-tool as transparent aggregator (all tools proxied first-class)."""
    from pydantic_ai.mcp import MCPServerStdio

    if not lazy_binary.is_file():
        raise FileNotFoundError(f"lazy-tool binary not found: {lazy_binary}")
    if not lazy_config.is_file():
        raise FileNotFoundError(f"config not found: {lazy_config}")

    prompt = _get_prompt(task_name, "direct")
    env = {**os.environ, "LAZY_TOOL_CONFIG": str(lazy_config.resolve())}
    server = MCPServerStdio(
        str(lazy_binary.resolve()),
        ["serve", "--mode", "direct"],
        env=env,
        cwd=workdir,
        timeout=120.0,
        read_timeout=600.0,
    )
    row = await _run_agent(
        label="direct",
        model=model,
        prompt=prompt,
        mcp_server=server,
        max_tokens=max_tokens,
        task_name=task_name,
        debug_parts=debug_parts,
        run_index=run_index,
        strict_answers=strict_answers,
        bench_mode="direct",
        provider=provider,
    )
    row["config_path"] = str(lazy_config.resolve())
    return row


# ── Filesystem fixture ───────────────────────────────────────────────────────

def _prepare_fs_fixture() -> None:
    _FS_ROOT.mkdir(parents=True, exist_ok=True)
    (_FS_ROOT / "notes.txt").write_text("hello from lazy-tool benchmark\n", encoding="utf-8")
    (_FS_ROOT / "todo.json").write_text(
        json.dumps({"tasks": ["benchmark", "search", "compare"]}, indent=2) + "\n",
        encoding="utf-8",
    )
    nested = _FS_ROOT / "nested"
    nested.mkdir(exist_ok=True)
    (nested / "info.txt").write_text("nested file\n", encoding="utf-8")


# ── Main orchestrator ────────────────────────────────────────────────────────

def _resolve_modes(mode_arg: str) -> list[str]:
    """Map the --mode flag to actual benchmark modes."""
    if mode_arg == "all":
        return ["baseline", "search", "direct"]
    return [mode_arg]


def _tasks_for_mode(task_name: str, bench_mode: str) -> bool:
    """Check if a task should run in a given benchmark mode."""
    task = TASKS[task_name]
    supported = task.get("modes", ["baseline", "search", "direct"])
    # "hybrid" means search+direct meta-tools are present
    if bench_mode == "direct" and "direct" in supported:
        return True
    if bench_mode == "search" and ("search" in supported or "hybrid" in supported):
        return True
    if bench_mode == "baseline" and "baseline" in supported:
        return True
    return False


async def _async_main(args: argparse.Namespace) -> list[dict[str, Any]]:
    provider_cfg = PROVIDERS[args.provider]
    env_key = provider_cfg["env_key"]
    if not os.environ.get(env_key):
        print(f"{env_key} is not set", file=sys.stderr)
        raise SystemExit(1)

    if args.prepare_fs:
        _prepare_fs_fixture()

    model_id = args.model or provider_cfg["default_model"]
    prefix = provider_cfg["prefix"]
    if not model_id.startswith(prefix):
        model_id = f"{prefix}{model_id}"

    bench_modes = _resolve_modes(args.mode)
    tasks = args.tasks.split(",") if args.tasks else list(TASKS.keys())

    for t in tasks:
        if t not in TASKS:
            print(f"unknown task: {t}", file=sys.stderr)
            raise SystemExit(2)

    lazy_binary = Path(args.lazy_binary)
    lazy_config = Path(args.lazy_config)
    workdir = Path(args.workdir)

    rows: list[dict[str, Any]] = []
    total_runs = 0

    for task_name in tasks:
        for bench_mode in bench_modes:
            if not _tasks_for_mode(task_name, bench_mode):
                continue
            for run_index in range(1, args.repeat + 1):
                total_runs += 1
                task_cfg = TASKS[task_name]
                max_tokens = args.max_tokens
                if task_cfg["expect_tool_calls"]:
                    max_tokens = max(max_tokens, 512)

                try:
                    if bench_mode == "baseline":
                        row = await _run_baseline(
                            jungle_url=args.jungle_url,
                            model=model_id,
                            task_name=task_name,
                            max_tokens=max_tokens,
                            debug_parts=args.debug_parts,
                            run_index=run_index,
                            strict_answers=args.strict_answers,
                            provider=args.provider,
                        )
                    elif bench_mode == "search":
                        row = await _run_search(
                            lazy_binary=lazy_binary,
                            lazy_config=lazy_config,
                            workdir=workdir,
                            model=model_id,
                            task_name=task_name,
                            max_tokens=max_tokens,
                            debug_parts=args.debug_parts,
                            run_index=run_index,
                            strict_answers=args.strict_answers,
                            provider=args.provider,
                        )
                    elif bench_mode == "direct":
                        row = await _run_direct(
                            lazy_binary=lazy_binary,
                            lazy_config=lazy_config,
                            workdir=workdir,
                            model=model_id,
                            task_name=task_name,
                            max_tokens=max_tokens,
                            debug_parts=args.debug_parts,
                            run_index=run_index,
                            strict_answers=args.strict_answers,
                            provider=args.provider,
                        )
                    else:
                        continue
                except Exception as e:
                    row = _exception_row(
                        run_index=run_index,
                        label=bench_mode,
                        provider=args.provider,
                        model=model_id,
                        task_name=task_name,
                        bench_mode=bench_mode,
                        exc=e,
                        config_path=str(lazy_config.resolve()) if bench_mode != "baseline" else None,
                    )
                rows.append(row)

                # Progress indicator
                success = "✓" if row.get("task_success") else "✗"
                print(
                    f"  [{total_runs}] {bench_mode}/{task_name} run={run_index} "
                    f"{success} {row.get('duration_s', 0):.2f}s "
                    f"tokens={row.get('total_tokens', 0)}",
                    file=sys.stderr,
                )

    return rows


# ── Output helpers ───────────────────────────────────────────────────────────

def _write_jsonl(path: Path, rows: list[dict[str, Any]]) -> None:
    with path.open("w", encoding="utf-8") as f:
        for row in rows:
            # Remove non-serializable verify lambdas if somehow present
            clean = {k: v for k, v in row.items() if not callable(v)}
            f.write(json.dumps(clean, ensure_ascii=False, default=str) + "\n")


def _write_csv(path: Path, rows: list[dict[str, Any]]) -> None:
    flat_rows: list[dict[str, Any]] = []
    for row in rows:
        flat = {k: v for k, v in row.items() if not callable(v)}
        if "tool_names" in flat and isinstance(flat["tool_names"], list):
            flat["tool_names"] = ",".join(flat["tool_names"])
        if "debug_message_parts" in flat:
            flat["debug_message_parts"] = json.dumps(flat["debug_message_parts"], ensure_ascii=False)
        flat_rows.append(flat)

    fieldnames: list[str] = []
    for row in flat_rows:
        for key in row.keys():
            if key not in fieldnames:
                fieldnames.append(key)

    with path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(flat_rows)


def _percentile(data: list[float], pct: float) -> float:
    """Simple percentile (linear interpolation)."""
    if not data:
        return 0.0
    sorted_data = sorted(data)
    idx = (len(sorted_data) - 1) * pct / 100
    lower = int(idx)
    upper = lower + 1
    if upper >= len(sorted_data):
        return sorted_data[-1]
    frac = idx - lower
    return sorted_data[lower] + frac * (sorted_data[upper] - sorted_data[lower])


def _print_summary(rows: list[dict[str, Any]]) -> None:
    """Print a comprehensive summary table."""
    # Group by (bench_mode, task)
    groups: dict[tuple[str, str], list[dict[str, Any]]] = {}
    for row in rows:
        key = (row.get("bench_mode", "?"), row.get("task", "?"))
        groups.setdefault(key, []).append(row)

    print("\n" + "=" * 90)
    print(f"{'Mode':<10} {'Task':<22} {'N':>3} {'Success':>8} "
          f"{'Tokens p50':>10} {'Tokens p95':>10} "
          f"{'Lat p50':>8} {'Lat p95':>8}")
    print("-" * 90)

    mode_order = ["baseline", "search", "direct"]
    for mode in mode_order:
        for (m, task), items in sorted(groups.items(), key=lambda x: (mode_order.index(x[0][0]) if x[0][0] in mode_order else 99, x[0][1])):
            if m != mode:
                continue
            n = len(items)
            successes = sum(1 for x in items if x.get("task_success"))
            tokens = [x.get("total_tokens", 0) for x in items]
            latencies = [x.get("duration_s", 0) for x in items]

            tok_p50 = _percentile(tokens, 50)
            tok_p95 = _percentile(tokens, 95)
            lat_p50 = _percentile(latencies, 50)
            lat_p95 = _percentile(latencies, 95)

            print(
                f"{m:<10} {task:<22} {n:>3} {successes:>3}/{n:<3} "
                f"{tok_p50:>10.0f} {tok_p95:>10.0f} "
                f"{lat_p50:>7.2f}s {lat_p95:>7.2f}s"
            )
        if any(m == mode for m, _ in groups):
            print("-" * 90)

    # Overall per-mode summary
    print(f"\n{'Mode':<10} {'Total':>6} {'Success':>8} {'Avg Tokens':>10} {'Avg Latency':>12}")
    print("-" * 50)
    for mode in mode_order:
        mode_rows = [r for r in rows if r.get("bench_mode") == mode]
        if not mode_rows:
            continue
        n = len(mode_rows)
        successes = sum(1 for x in mode_rows if x.get("task_success"))
        avg_tokens = sum(x.get("total_tokens", 0) for x in mode_rows) / n
        avg_lat = sum(x.get("duration_s", 0) for x in mode_rows) / n
        print(f"{mode:<10} {n:>6} {successes:>3}/{n:<3} {avg_tokens:>10.0f} {avg_lat:>11.2f}s")

    # Mode comparison
    baseline_rows = [r for r in rows if r.get("bench_mode") == "baseline"]
    direct_rows = [r for r in rows if r.get("bench_mode") == "direct"]
    if baseline_rows and direct_rows:
        bl_avg_tok = sum(r.get("total_tokens", 0) for r in baseline_rows) / len(baseline_rows)
        di_avg_tok = sum(r.get("total_tokens", 0) for r in direct_rows) / len(direct_rows)
        bl_avg_lat = sum(r.get("duration_s", 0) for r in baseline_rows) / len(baseline_rows)
        di_avg_lat = sum(r.get("duration_s", 0) for r in direct_rows) / len(direct_rows)
        if bl_avg_tok > 0:
            overhead_tok = ((di_avg_tok - bl_avg_tok) / bl_avg_tok) * 100
            print(f"\nDirect mode vs baseline: {overhead_tok:+.1f}% tokens")
        if bl_avg_lat > 0:
            overhead_lat = ((di_avg_lat - bl_avg_lat) / bl_avg_lat) * 100
            print(f"Direct mode vs baseline: {overhead_lat:+.1f}% latency")


def _print_human(rows: list[dict[str, Any]], args: argparse.Namespace) -> None:
    if not rows:
        print("no rows")
        return

    print(f"\nprovider={args.provider}")
    print(f"model={rows[0].get('model', '?')}")
    print(f"modes={args.mode}")
    print(f"repeat={args.repeat}")

    for r in rows:
        status = "✓" if r.get("task_success") else "✗"
        print(
            f"\n{status} [{r.get('bench_mode', '?')}/{r.get('task', '?')} run={r.get('run_index', 0)}] "
            f"tokens={r.get('input_tokens', 0)}+{r.get('output_tokens', 0)}={r.get('total_tokens', 0)} "
            f"duration={r.get('duration_s', 0):.2f}s"
        )
        tc = r.get("tool_call_count", 0)
        tnames = r.get("tool_names") or []
        if tnames:
            print(f"  tools: {tc} ({', '.join(tnames)})")
        if r.get("failure_reason"):
            print(f"  failure: {r['failure_reason']}")
        if r.get("exception_type"):
            print(f"  exception: {r['exception_type']}: {r.get('exception_message', '')[:200]}")

    _print_summary(rows)


# ── CLI ──────────────────────────────────────────────────────────────────────

def main() -> None:
    p = argparse.ArgumentParser(
        description="Multi-provider benchmark: Anthropic/OpenAI/Groq × baseline/search/direct.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    p.add_argument("--provider", choices=list(PROVIDERS.keys()), required=True, help="LLM provider")
    p.add_argument("--model", default="", help="Model ID (default: provider default)")
    p.add_argument(
        "--mode", choices=("baseline", "search", "direct", "all"), default="all",
        help="Benchmark mode (default: all three)",
    )
    p.add_argument(
        "--tasks", default="",
        help="Comma-separated task names (default: all tasks). E.g. routed_echo,routed_file_read",
    )
    p.add_argument("--repeat", type=int, default=5, help="Repeat each mode×task N times (default: 5)")
    p.add_argument("--max-tokens", type=int, default=256, help="Max completion tokens per step")
    p.add_argument(
        "--jungle-url", default="http://127.0.0.1:8080/mcp",
        help="MCPJungle endpoint for baseline mode",
    )
    p.add_argument("--lazy-binary", default=str(_REPO_ROOT / "bin" / "lazy-tool"))
    p.add_argument("--lazy-config", default=str(_BENCHMARK_DIR / "configs" / "mcpjungle-lazy-tool.yaml"))
    p.add_argument("--workdir", default=str(_REPO_ROOT))
    p.add_argument("--prepare-fs", action="store_true", help="Create filesystem fixture")
    p.add_argument("--debug-parts", action="store_true", help="Include raw message parts")
    p.add_argument("--strict-answers", action="store_true", help="Strict answer format checks")
    p.add_argument("--json", action="store_true", help="Output as JSON array")
    p.add_argument("--jsonl-out", default="", help="Write JSONL to path")
    p.add_argument("--csv-out", default="", help="Write CSV to path")
    args = p.parse_args()

    if args.repeat < 1:
        print("--repeat must be >= 1", file=sys.stderr)
        raise SystemExit(2)

    print(f"Benchmark: provider={args.provider} model={args.model or PROVIDERS[args.provider]['default_model']} "
          f"mode={args.mode} repeat={args.repeat}", file=sys.stderr)

    rows = asyncio.run(_async_main(args))

    if args.jsonl_out:
        _write_jsonl(Path(args.jsonl_out), rows)
        print(f"Wrote {len(rows)} rows to {args.jsonl_out}", file=sys.stderr)
    if args.csv_out:
        _write_csv(Path(args.csv_out), rows)
        print(f"Wrote {len(rows)} rows to {args.csv_out}", file=sys.stderr)

    if args.json:
        clean = [{k: v for k, v in r.items() if not callable(v)} for r in rows]
        print(json.dumps(clean, indent=2, ensure_ascii=False, default=str))
        return

    _print_human(rows, args)


if __name__ == "__main__":
    main()
