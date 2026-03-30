#!/usr/bin/env python3
"""
Weak-model (Ollama) benchmark for lazy-tool.

Tests local models via Ollama to measure whether lazy-tool's search-first
approach actually helps small models use MCP tools. Three tiers:

  Tier 1 — Basic tool-calling ability (can the model call tools at all?)
  Tier 2 — lazy-tool search surface navigation (coached vs natural prompts)
  Tier 3 — Deterministic search quality (no LLM, direct MCP calls)

Uses PydanticAI's OpenAI provider with base_url pointed at Ollama's
OpenAI-compatible endpoint (http://localhost:11434/v1).

Default models: qwen2.5:3b, llama3.2:3b, phi3:mini, gemma2:2b

Examples:
  python benchmark/run_weak_model_benchmark.py --model qwen2.5:3b --mode search --repeat 3
  python benchmark/run_weak_model_benchmark.py --model llama3.2:3b --mode all --tier 1,2
  python benchmark/run_weak_model_benchmark.py --tier 3  # deterministic only, no LLM
  python benchmark/run_weak_model_benchmark.py --all-models --mode all
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import re
import sys
import time
from pathlib import Path
from typing import Any

_REPO_ROOT = Path(__file__).resolve().parent.parent
_BENCHMARK_DIR = Path(__file__).resolve().parent
_FS_ROOT = Path("/tmp/lazy-tool-mcpjungle-fs")

# Re-use helpers from the strong-model harness (no code duplication)
sys.path.insert(0, str(_BENCHMARK_DIR))
from run_multi_provider_benchmark import (
    _looks_like_pseudo_tool_output,
    _tool_stats,
    _write_jsonl,
    _write_csv,
    _prepare_fs_fixture,
    _INSTRUCTIONS_STRUCTURED_TOOLS_ONLY,
    _safe_int,
    _percentile,
)

# ── Default Ollama models ────────────────────────────────────────────────────

DEFAULT_MODELS = ["qwen2.5:3b", "llama3.2:3b", "phi3:mini", "gemma2:2b"]
DEFAULT_OLLAMA_URL = "http://localhost:11434/v1"
DEFAULT_TIMEOUT = 300  # seconds (vs 120 for strong models)

# ── Tasks ────────────────────────────────────────────────────────────────────

TASKS: dict[str, dict[str, Any]] = {
    # ── Tier 1: Basic tool-calling ability ───────────────────────────────
    "single_tool_call": {
        "tier": 1,
        "prompt": (
            "Call the tool named 'echo' with {\"message\": \"ping\"}. "
            "After the tool returns, reply with exactly: TOOL_OK"
        ),
        "expect_tool_calls": True,
        "expected_tool_hints": ["echo"],
        "description": "Can the model do structured tool calling at all?",
        "modes": ["direct"],
        "coached": True,
    },
    "format_compliance": {
        "tier": 1,
        "prompt": (
            "Call the tool named 'echo' with {\"message\": \"format-check\"}. "
            "You MUST use the structured tool/function-calling channel. "
            "Do NOT write tool calls as text. Reply with exactly: FORMAT_OK"
        ),
        "expect_tool_calls": True,
        "expected_tool_hints": ["echo"],
        "description": "Does the model use the API or fake it in text?",
        "modes": ["direct"],
        "coached": True,
    },

    # ── Tier 2: Search surface navigation ────────────────────────────────
    "search_coached": {
        "tier": 2,
        "prompt": (
            "Call search_tools with query 'echo' and limit 3. "
            "After the result returns, reply with exactly: SEARCH_OK"
        ),
        "expect_tool_calls": True,
        "expected_tool_hints": ["search_tools"],
        "description": "Can it call search_tools when told exactly how?",
        "modes": ["search"],
        "coached": True,
    },
    "search_natural": {
        "tier": 2,
        "prompt": (
            "Find a tool that can echo messages back. "
            "Tell me what you found."
        ),
        "expect_tool_calls": True,
        "expected_tool_hints": ["search_tools"],
        "description": "Can it figure out to use search_tools on its own?",
        "modes": ["search"],
        "coached": False,
    },
    "search_invoke_coached": {
        "tier": 2,
        "prompt": {
            "search": (
                "Step 1: Call search_tools with query 'echo' and limit 3. "
                "Step 2: Pick the best result and call invoke_proxy_tool "
                "with proxy_tool_name from step 1 and input {\"message\": \"benchmark-test\"}. "
                "Step 3: Reply with ECHO_OK followed by the echoed content."
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
        "description": "Full search-then-invoke with hand-holding.",
        "modes": ["search", "direct", "baseline"],
        "coached": True,
        "verify": lambda output: "benchmark-test" in output.lower() or "echo_ok" in output.lower(),
    },
    "search_invoke_natural": {
        "tier": 2,
        "prompt": {
            "search": (
                "Echo the word 'benchmark-test' back to me using available tools."
            ),
            "direct": (
                "Echo the word 'benchmark-test' back to me using available tools."
            ),
            "baseline": (
                "Echo the word 'benchmark-test' back to me using available tools."
            ),
        },
        "expect_tool_calls": True,
        "expected_tool_hints": ["echo", "invoke_proxy_tool", "search_tools"],
        "description": "Full flow without coaching -- the real test.",
        "modes": ["search", "direct", "baseline"],
        "coached": False,
        "verify": lambda output: "benchmark-test" in output.lower(),
    },

    # ── Tier 3: Deterministic search quality ─────────────────────────────
    "search_precision": {
        "tier": 3,
        "prompt": "",  # not used — tier 3 is LLM-free
        "expect_tool_calls": False,
        "expected_tool_hints": [],
        "description": "Deterministic search quality: precision@1, precision@3.",
        "modes": ["search"],
        "coached": False,
    },
}

# ── Search quality cases (Tier 3) ────────────────────────────────────────────

SEARCH_QUALITY_CASES = [
    {"query": "echo",          "expected_prefix": "echo"},
    {"query": "read file",     "expected_prefix": "read"},
    {"query": "list files",    "expected_prefix": "list"},
    {"query": "time",          "expected_prefix": "time"},
    {"query": "prompt",        "expected_prefix": "prompt"},
    {"query": "resource",      "expected_prefix": "resource"},
    {"query": "send message",  "expected_prefix": "echo"},
    {"query": "get contents",  "expected_prefix": "read"},
    {"query": "directory",     "expected_prefix": "list"},
    {"query": "what time",     "expected_prefix": "time"},
]

# ── Answer format evaluation ────────────────────────────────────────────────

def _evaluate_answer_format(task_name: str, output_preview: str, strict: bool) -> bool:
    output_lower = output_preview.lower()
    task = TASKS[task_name]

    if "verify" in task:
        return task["verify"](output_preview)

    if task_name == "single_tool_call":
        return "tool_ok" in output_lower
    if task_name == "format_compliance":
        return "format_ok" in output_lower
    if task_name == "search_coached":
        return "search_ok" in output_lower
    if task_name == "search_natural":
        # Natural: just needs to have found something
        return len(output_preview.strip()) > 10
    if task_name == "search_precision":
        return True  # evaluated separately
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


# ── Prompt resolution ────────────────────────────────────────────────────────

def _get_prompt(task_name: str, bench_mode: str) -> str:
    task = TASKS[task_name]
    prompt = task["prompt"]
    if isinstance(prompt, dict):
        return prompt.get(bench_mode, prompt.get("baseline", ""))
    return prompt


# ── Ollama model ID construction ─────────────────────────────────────────────

def _ollama_model_id(model_name: str) -> str:
    """Construct PydanticAI model string for Ollama via OpenAI provider."""
    if model_name.startswith("openai:"):
        return model_name
    return f"openai:{model_name}"


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
        "tier": TASKS.get(task_name, {}).get("tier", 0),
        "coached": TASKS.get(task_name, {}).get("coached", False),
    }


# ── Core agent runner (Tier 1+2) ─────────────────────────────────────────────

async def _run_agent_task(
    *,
    model_id: str,
    mcp_server: object,
    task_name: str,
    bench_mode: str,
    max_tokens: int,
    run_index: int,
    strict_answers: bool,
    debug_parts: bool,
    provider: str,
) -> dict[str, Any]:
    from pydantic_ai import Agent

    task = TASKS[task_name]
    prompt = _get_prompt(task_name, bench_mode)
    expect_tools = bool(task["expect_tool_calls"])

    agent_kw: dict[str, Any] = {"model": model_id, "toolsets": [mcp_server]}
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
    tool_exec_success = _tool_execution_success(task_name, stats["tool_names"], pseudo_tool_text)
    failure_reason_val = _failure_reason(
        task_name=task_name,
        tool_names=stats["tool_names"],
        output_preview=output_preview,
        answer_format_success=answer_format_success,
        expected_tool_family=expected_tool_family,
    )
    task_success = (
        tool_exec_success
        and answer_format_success
        and not pseudo_tool_text
        and expected_tool_family
        and failure_reason_val is None
    )

    row = {
        "run_index": run_index,
        "label": bench_mode,
        "provider": provider,
        "model": model_id,
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
        "tool_execution_success": tool_exec_success,
        "answer_format_success": answer_format_success,
        "used_expected_tool_family": expected_tool_family,
        "task_success": task_success,
        "failure_reason": failure_reason_val,
        "tier": task["tier"],
        "coached": task.get("coached", False),
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

async def _run_search(
    *,
    lazy_binary: Path,
    lazy_config: Path,
    workdir: Path,
    model_id: str,
    task_name: str,
    max_tokens: int,
    debug_parts: bool,
    run_index: int,
    strict_answers: bool,
    provider: str,
) -> dict[str, Any]:
    from pydantic_ai.mcp import MCPServerStdio

    if not lazy_binary.is_file():
        raise FileNotFoundError(f"lazy-tool binary not found: {lazy_binary}")
    if not lazy_config.is_file():
        raise FileNotFoundError(f"config not found: {lazy_config}")

    env = {**os.environ, "LAZY_TOOL_CONFIG": str(lazy_config.resolve())}
    server = MCPServerStdio(
        str(lazy_binary.resolve()),
        ["serve", "--mode", "search"],
        env=env,
        cwd=workdir,
        timeout=DEFAULT_TIMEOUT,
        read_timeout=600.0,
    )
    row = await _run_agent_task(
        model_id=model_id,
        mcp_server=server,
        task_name=task_name,
        bench_mode="search",
        max_tokens=max_tokens,
        run_index=run_index,
        strict_answers=strict_answers,
        debug_parts=debug_parts,
        provider=provider,
    )
    row["config_path"] = str(lazy_config.resolve())
    return row


async def _run_direct(
    *,
    lazy_binary: Path,
    lazy_config: Path,
    workdir: Path,
    model_id: str,
    task_name: str,
    max_tokens: int,
    debug_parts: bool,
    run_index: int,
    strict_answers: bool,
    provider: str,
) -> dict[str, Any]:
    from pydantic_ai.mcp import MCPServerStdio

    if not lazy_binary.is_file():
        raise FileNotFoundError(f"lazy-tool binary not found: {lazy_binary}")
    if not lazy_config.is_file():
        raise FileNotFoundError(f"config not found: {lazy_config}")

    env = {**os.environ, "LAZY_TOOL_CONFIG": str(lazy_config.resolve())}
    server = MCPServerStdio(
        str(lazy_binary.resolve()),
        ["serve", "--mode", "direct"],
        env=env,
        cwd=workdir,
        timeout=DEFAULT_TIMEOUT,
        read_timeout=600.0,
    )
    row = await _run_agent_task(
        model_id=model_id,
        mcp_server=server,
        task_name=task_name,
        bench_mode="direct",
        max_tokens=max_tokens,
        run_index=run_index,
        strict_answers=strict_answers,
        debug_parts=debug_parts,
        provider=provider,
    )
    row["config_path"] = str(lazy_config.resolve())
    return row


async def _run_baseline(
    *,
    jungle_url: str,
    model_id: str,
    task_name: str,
    max_tokens: int,
    debug_parts: bool,
    run_index: int,
    strict_answers: bool,
    provider: str,
) -> dict[str, Any]:
    from pydantic_ai.mcp import MCPServerStreamableHTTP

    server = MCPServerStreamableHTTP(jungle_url, timeout=DEFAULT_TIMEOUT, read_timeout=600.0)
    row = await _run_agent_task(
        model_id=model_id,
        mcp_server=server,
        task_name=task_name,
        bench_mode="baseline",
        max_tokens=max_tokens,
        run_index=run_index,
        strict_answers=strict_answers,
        debug_parts=debug_parts,
        provider=provider,
    )
    row["config_path"] = None
    return row


# ── Tier 3: Deterministic search quality ─────────────────────────────────────

async def _run_search_precision(
    *,
    lazy_binary: Path,
    lazy_config: Path,
    workdir: Path,
) -> list[dict[str, Any]]:
    """Run deterministic search quality tests — no LLM needed."""
    from pydantic_ai.mcp import MCPServerStdio

    if not lazy_binary.is_file():
        raise FileNotFoundError(f"lazy-tool binary not found: {lazy_binary}")
    if not lazy_config.is_file():
        raise FileNotFoundError(f"config not found: {lazy_config}")

    env = {**os.environ, "LAZY_TOOL_CONFIG": str(lazy_config.resolve())}
    server = MCPServerStdio(
        str(lazy_binary.resolve()),
        ["serve", "--mode", "search"],
        env=env,
        cwd=workdir,
        timeout=DEFAULT_TIMEOUT,
        read_timeout=600.0,
    )

    rows: list[dict[str, Any]] = []

    async with server:
        for case in SEARCH_QUALITY_CASES:
            query = case["query"]
            expected_prefix = case["expected_prefix"]

            started = time.perf_counter()
            try:
                result = await server.call_tool(
                    "search_tools",
                    {"query": query, "limit": 3},
                )
                duration_s = round(time.perf_counter() - started, 3)

                # Parse results from the MCP response
                result_text = ""
                if hasattr(result, "content"):
                    for part in result.content:
                        if hasattr(part, "text"):
                            result_text += part.text
                elif isinstance(result, str):
                    result_text = result

                # Try to parse as JSON to extract tool names
                tool_names_found: list[str] = []
                try:
                    parsed = json.loads(result_text)
                    if isinstance(parsed, list):
                        for item in parsed:
                            name = item.get("proxy_tool_name", "") or item.get("name", "")
                            if name:
                                tool_names_found.append(name.lower())
                    elif isinstance(parsed, dict):
                        results_list = parsed.get("results", parsed.get("tools", []))
                        if isinstance(results_list, list):
                            for item in results_list:
                                name = item.get("proxy_tool_name", "") or item.get("name", "")
                                if name:
                                    tool_names_found.append(name.lower())
                except (json.JSONDecodeError, TypeError):
                    # Fallback: look for tool names in text
                    for token in result_text.lower().split():
                        if "__" in token:
                            tool_names_found.append(token.strip('"\',:[]{}'))

                precision_at_1 = (
                    len(tool_names_found) >= 1
                    and expected_prefix in tool_names_found[0]
                )
                precision_at_3 = any(
                    expected_prefix in name
                    for name in tool_names_found[:3]
                )

                row = {
                    "run_index": 0,
                    "label": "search",
                    "provider": "deterministic",
                    "model": "none",
                    "task": "search_precision",
                    "bench_mode": "search",
                    "strict_answers": False,
                    "input_tokens": 0,
                    "output_tokens": 0,
                    "total_tokens": 0,
                    "usage_missing": True,
                    "pseudo_tool_text": False,
                    "duration_s": duration_s,
                    "output_preview": result_text[:400],
                    "tool_execution_success": True,
                    "answer_format_success": True,
                    "used_expected_tool_family": True,
                    "task_success": precision_at_1,
                    "failure_reason": None if precision_at_1 else "precision_miss",
                    "tool_call_count": 1,
                    "tool_names": ["search_tools"],
                    "config_path": str(lazy_config.resolve()),
                    "tier": 3,
                    "coached": False,
                    "precision_at_1": precision_at_1,
                    "precision_at_3": precision_at_3,
                    "search_query": query,
                    "expected_tool": expected_prefix,
                }
            except Exception as e:
                duration_s = round(time.perf_counter() - started, 3)
                row = {
                    "run_index": 0,
                    "label": "search",
                    "provider": "deterministic",
                    "model": "none",
                    "task": "search_precision",
                    "bench_mode": "search",
                    "strict_answers": False,
                    "input_tokens": 0,
                    "output_tokens": 0,
                    "total_tokens": 0,
                    "usage_missing": True,
                    "pseudo_tool_text": False,
                    "duration_s": duration_s,
                    "output_preview": "",
                    "tool_execution_success": False,
                    "answer_format_success": False,
                    "used_expected_tool_family": False,
                    "task_success": False,
                    "failure_reason": "exception",
                    "exception_type": type(e).__name__,
                    "exception_message": str(e)[:500],
                    "tool_call_count": 0,
                    "tool_names": [],
                    "config_path": str(lazy_config.resolve()),
                    "tier": 3,
                    "coached": False,
                    "precision_at_1": False,
                    "precision_at_3": False,
                    "search_query": query,
                    "expected_tool": expected_prefix,
                }

            rows.append(row)
            status = "hit" if row.get("precision_at_1") else "miss"
            print(
                f"  [T3] query={query!r} expected={expected_prefix!r} "
                f"p@1={status} p@3={'hit' if row.get('precision_at_3') else 'miss'} "
                f"{duration_s:.2f}s",
                file=sys.stderr,
            )

    return rows


# ── Mode/task compatibility ──────────────────────────────────────────────────

def _resolve_modes(mode_arg: str) -> list[str]:
    if mode_arg == "all":
        return ["baseline", "search", "direct"]
    return [mode_arg]


def _tasks_for_mode(task_name: str, bench_mode: str) -> bool:
    task = TASKS[task_name]
    supported = task.get("modes", ["search"])
    if bench_mode == "direct" and "direct" in supported:
        return True
    if bench_mode == "search" and "search" in supported:
        return True
    if bench_mode == "baseline" and "baseline" in supported:
        return True
    return False


def _tasks_for_tiers(tiers: list[int]) -> list[str]:
    return [name for name, task in TASKS.items() if task.get("tier") in tiers]


# ── Main orchestrator ────────────────────────────────────────────────────────

async def _async_main(args: argparse.Namespace) -> list[dict[str, Any]]:
    # Set up Ollama env vars for PydanticAI's OpenAI provider
    ollama_url = args.ollama_url
    os.environ["OPENAI_API_KEY"] = "ollama"
    os.environ["OPENAI_BASE_URL"] = ollama_url

    if args.prepare_fs:
        _prepare_fs_fixture()

    tiers = [int(t) for t in args.tier.split(",")] if args.tier else [1, 2, 3]
    bench_modes = _resolve_modes(args.mode)

    lazy_binary = Path(args.lazy_binary)
    lazy_config = Path(args.lazy_config)
    workdir = Path(args.workdir)

    rows: list[dict[str, Any]] = []

    # ── Tier 3: deterministic search quality (no LLM) ────────────────────
    if 3 in tiers:
        print("\n── Tier 3: Deterministic search quality ──", file=sys.stderr)
        try:
            t3_rows = await _run_search_precision(
                lazy_binary=lazy_binary,
                lazy_config=lazy_config,
                workdir=workdir,
            )
            rows.extend(t3_rows)
        except Exception as e:
            print(f"  Tier 3 failed: {type(e).__name__}: {e}", file=sys.stderr)

    # ── Tier 1+2: LLM-based tasks ───────────────────────────────────────
    llm_tiers = [t for t in tiers if t in (1, 2)]
    if not llm_tiers:
        return rows

    models = DEFAULT_MODELS if args.all_models else [args.model]
    task_names = _tasks_for_tiers(llm_tiers)

    for model_name in models:
        model_id = _ollama_model_id(model_name)
        print(f"\n── Model: {model_name} ({model_id}) ──", file=sys.stderr)

        total_runs = 0
        for task_name in task_names:
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
                                model_id=model_id,
                                task_name=task_name,
                                max_tokens=max_tokens,
                                debug_parts=args.debug_parts,
                                run_index=run_index,
                                strict_answers=args.strict_answers,
                                provider="ollama",
                            )
                        elif bench_mode == "search":
                            row = await _run_search(
                                lazy_binary=lazy_binary,
                                lazy_config=lazy_config,
                                workdir=workdir,
                                model_id=model_id,
                                task_name=task_name,
                                max_tokens=max_tokens,
                                debug_parts=args.debug_parts,
                                run_index=run_index,
                                strict_answers=args.strict_answers,
                                provider="ollama",
                            )
                        elif bench_mode == "direct":
                            row = await _run_direct(
                                lazy_binary=lazy_binary,
                                lazy_config=lazy_config,
                                workdir=workdir,
                                model_id=model_id,
                                task_name=task_name,
                                max_tokens=max_tokens,
                                debug_parts=args.debug_parts,
                                run_index=run_index,
                                strict_answers=args.strict_answers,
                                provider="ollama",
                            )
                        else:
                            continue
                    except Exception as e:
                        row = _exception_row(
                            run_index=run_index,
                            label=bench_mode,
                            provider="ollama",
                            model=model_id,
                            task_name=task_name,
                            bench_mode=bench_mode,
                            exc=e,
                            config_path=str(lazy_config.resolve()) if bench_mode != "baseline" else None,
                        )
                    rows.append(row)

                    success = "pass" if row.get("task_success") else "FAIL"
                    coached = "coached" if TASKS[task_name].get("coached") else "natural"
                    print(
                        f"  [{total_runs}] T{TASKS[task_name]['tier']} "
                        f"{bench_mode}/{task_name} ({coached}) run={run_index} "
                        f"{success} {row.get('duration_s', 0):.2f}s "
                        f"tokens={row.get('total_tokens', 0)}",
                        file=sys.stderr,
                    )

    return rows


# ── Summary output ───────────────────────────────────────────────────────────

def _print_summary(rows: list[dict[str, Any]]) -> None:
    if not rows:
        print("No results.")
        return

    # Group by tier
    tier_rows: dict[int, list[dict[str, Any]]] = {}
    for r in rows:
        tier = r.get("tier", 0)
        tier_rows.setdefault(tier, []).append(r)

    for tier in sorted(tier_rows.keys()):
        items = tier_rows[tier]
        print(f"\n{'=' * 80}")
        print(f" Tier {tier}")
        print(f"{'=' * 80}")

        if tier == 3:
            # Tier 3: precision summary
            p1_hits = sum(1 for r in items if r.get("precision_at_1"))
            p3_hits = sum(1 for r in items if r.get("precision_at_3"))
            n = len(items)
            print(f"  Search quality: {n} queries")
            print(f"  Precision@1: {p1_hits}/{n} ({p1_hits/n*100:.0f}%)" if n else "")
            print(f"  Precision@3: {p3_hits}/{n} ({p3_hits/n*100:.0f}%)" if n else "")
            continue

        # Tier 1+2: group by model
        by_model: dict[str, list[dict[str, Any]]] = {}
        for r in items:
            by_model.setdefault(r.get("model", "?"), []).append(r)

        for model, model_items in sorted(by_model.items()):
            print(f"\n  Model: {model}")
            print(f"  {'Task':<26} {'Mode':<8} {'Style':<8} {'N':>3} {'Success':>8} "
                  f"{'Avg Lat':>8}")
            print(f"  {'-' * 70}")

            # Group by (task, mode)
            groups: dict[tuple[str, str], list[dict[str, Any]]] = {}
            for r in model_items:
                key = (r.get("task", "?"), r.get("bench_mode", "?"))
                groups.setdefault(key, []).append(r)

            for (task, mode), g_items in sorted(groups.items()):
                n = len(g_items)
                succ = sum(1 for x in g_items if x.get("task_success"))
                avg_lat = sum(x.get("duration_s", 0) for x in g_items) / n
                coached = "coached" if TASKS.get(task, {}).get("coached") else "natural"
                print(
                    f"  {task:<26} {mode:<8} {coached:<8} {n:>3} "
                    f"{succ:>3}/{n:<3} {avg_lat:>7.2f}s"
                )


def _print_human(rows: list[dict[str, Any]], args: argparse.Namespace) -> None:
    if not rows:
        print("no rows")
        return

    for r in rows:
        status = "pass" if r.get("task_success") else "FAIL"
        tier = r.get("tier", "?")
        print(
            f"\n{status} [T{tier} {r.get('bench_mode', '?')}/{r.get('task', '?')} "
            f"run={r.get('run_index', 0)}] "
            f"tokens={r.get('total_tokens', 0)} "
            f"duration={r.get('duration_s', 0):.2f}s"
        )
        if r.get("failure_reason"):
            print(f"  failure: {r['failure_reason']}")
        if r.get("exception_type"):
            print(f"  exception: {r['exception_type']}: {r.get('exception_message', '')[:200]}")
        if r.get("precision_at_1") is not None:
            print(f"  query={r.get('search_query')!r} p@1={r.get('precision_at_1')} p@3={r.get('precision_at_3')}")

    _print_summary(rows)


# ── CLI ──────────────────────────────────────────────────────────────────────

def main() -> None:
    p = argparse.ArgumentParser(
        description="Weak-model (Ollama) benchmark: Tier 1-3 tool-calling evaluation.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )
    p.add_argument("--model", default="qwen2.5:3b", help="Ollama model name (default: qwen2.5:3b)")
    p.add_argument("--all-models", action="store_true", help="Run all default models")
    p.add_argument(
        "--mode", choices=("baseline", "search", "direct", "all"), default="all",
        help="Benchmark mode (default: all)",
    )
    p.add_argument("--tier", default="", help="Comma-separated tiers to run (default: 1,2,3)")
    p.add_argument("--repeat", type=int, default=3, help="Repeat each task N times (default: 3)")
    p.add_argument("--ollama-url", default=DEFAULT_OLLAMA_URL, help="Ollama OpenAI-compat URL")
    p.add_argument("--max-tokens", type=int, default=256, help="Max completion tokens")
    p.add_argument(
        "--jungle-url", default="http://127.0.0.1:8080/mcp",
        help="MCPJungle endpoint for baseline mode",
    )
    p.add_argument("--lazy-binary", default=str(_REPO_ROOT / "bin" / "lazy-tool"))
    p.add_argument("--lazy-config", default=str(_BENCHMARK_DIR / "configs" / "mcpjungle-lazy-tool-weak.yaml"))
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

    tiers_str = args.tier or "1,2,3"
    print(
        f"Benchmark: model={args.model} mode={args.mode} "
        f"tiers={tiers_str} repeat={args.repeat}",
        file=sys.stderr,
    )

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
