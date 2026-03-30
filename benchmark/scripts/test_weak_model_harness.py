#!/usr/bin/env python3
"""
Unit tests for weak-model benchmark harness logic.

No network calls, no Ollama needed. Tests the pure logic functions.

Usage:
  python -m pytest benchmark/scripts/test_weak_model_harness.py -v
  python benchmark/scripts/test_weak_model_harness.py
"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

# Add benchmark dir to path so we can import the harness
_REPO_ROOT = Path(__file__).resolve().parent.parent.parent
sys.path.insert(0, str(_REPO_ROOT / "benchmark"))

from run_weak_model_benchmark import (
    TASKS,
    SEARCH_QUALITY_CASES,
    DEFAULT_MODELS,
    _evaluate_answer_format,
    _failure_reason,
    _get_prompt,
    _ollama_model_id,
    _resolve_modes,
    _tasks_for_mode,
    _tasks_for_tiers,
    _tool_execution_success,
    _used_expected_tool_family,
)


class TestWeakAnswerFormat(unittest.TestCase):
    """Answer format evaluation for each weak-model task."""

    def test_single_tool_call_ok(self):
        self.assertTrue(_evaluate_answer_format("single_tool_call", "TOOL_OK", strict=False))

    def test_single_tool_call_fail(self):
        self.assertFalse(_evaluate_answer_format("single_tool_call", "I called the tool", strict=False))

    def test_format_compliance_ok(self):
        self.assertTrue(_evaluate_answer_format("format_compliance", "FORMAT_OK", strict=False))

    def test_format_compliance_fail(self):
        self.assertFalse(_evaluate_answer_format("format_compliance", "Done calling tool", strict=False))

    def test_search_coached_ok(self):
        self.assertTrue(_evaluate_answer_format("search_coached", "SEARCH_OK", strict=False))

    def test_search_coached_fail(self):
        self.assertFalse(_evaluate_answer_format("search_coached", "I searched for tools", strict=False))

    def test_search_natural_ok(self):
        self.assertTrue(_evaluate_answer_format(
            "search_natural",
            "I found an echo tool that can send messages.",
            strict=False,
        ))

    def test_search_natural_too_short(self):
        self.assertFalse(_evaluate_answer_format("search_natural", "ok", strict=False))

    def test_search_invoke_coached_ok(self):
        self.assertTrue(_evaluate_answer_format(
            "search_invoke_coached",
            "ECHO_OK benchmark-test echoed",
            strict=False,
        ))

    def test_search_invoke_coached_content_match(self):
        self.assertTrue(_evaluate_answer_format(
            "search_invoke_coached",
            "The tool returned benchmark-test",
            strict=False,
        ))

    def test_search_invoke_natural_ok(self):
        self.assertTrue(_evaluate_answer_format(
            "search_invoke_natural",
            "benchmark-test was echoed back",
            strict=False,
        ))

    def test_search_invoke_natural_fail(self):
        self.assertFalse(_evaluate_answer_format(
            "search_invoke_natural",
            "I couldn't find any tools",
            strict=False,
        ))

    def test_search_precision_always_true(self):
        self.assertTrue(_evaluate_answer_format("search_precision", "", strict=False))


class TestWeakToolFamily(unittest.TestCase):
    """Tool family matching for weak-model tasks."""

    def test_single_tool_call_echo(self):
        self.assertTrue(_used_expected_tool_family("single_tool_call", ["echo"]))

    def test_format_compliance_echo(self):
        self.assertTrue(_used_expected_tool_family("format_compliance", ["echo"]))

    def test_search_coached_match(self):
        self.assertTrue(_used_expected_tool_family("search_coached", ["search_tools"]))

    def test_search_natural_match(self):
        self.assertTrue(_used_expected_tool_family("search_natural", ["search_tools"]))

    def test_search_invoke_coached_search_mode(self):
        self.assertTrue(_used_expected_tool_family(
            "search_invoke_coached", ["search_tools", "invoke_proxy_tool"],
        ))

    def test_search_invoke_coached_direct_mode(self):
        self.assertTrue(_used_expected_tool_family("search_invoke_coached", ["echo"]))

    def test_search_invoke_natural_search_mode(self):
        self.assertTrue(_used_expected_tool_family(
            "search_invoke_natural", ["search_tools", "invoke_proxy_tool"],
        ))

    def test_search_precision_no_hints(self):
        self.assertTrue(_used_expected_tool_family("search_precision", []))

    def test_wrong_family_fails(self):
        self.assertFalse(_used_expected_tool_family("single_tool_call", ["unknown_tool"]))


class TestWeakTaskDefinitions(unittest.TestCase):
    """All tasks have required fields."""

    def test_all_tasks_have_required_fields(self):
        required = {"prompt", "expect_tool_calls", "expected_tool_hints", "description", "modes", "tier"}
        for name, task in TASKS.items():
            for field in required:
                self.assertIn(field, task, f"task '{name}' missing field '{field}'")

    def test_all_tasks_have_valid_tier(self):
        for name, task in TASKS.items():
            self.assertIn(task["tier"], (1, 2, 3), f"task '{name}' has invalid tier {task['tier']}")

    def test_all_tasks_have_coached_field(self):
        for name, task in TASKS.items():
            self.assertIn("coached", task, f"task '{name}' missing 'coached' field")
            self.assertIsInstance(task["coached"], bool, f"task '{name}' coached must be bool")

    def test_tier_1_tasks_are_direct(self):
        for name, task in TASKS.items():
            if task["tier"] == 1:
                self.assertIn("direct", task["modes"], f"tier 1 task '{name}' should support direct mode")

    def test_tier_2_tasks_have_search(self):
        for name, task in TASKS.items():
            if task["tier"] == 2:
                self.assertIn("search", task["modes"], f"tier 2 task '{name}' should support search mode")


class TestWeakPromptResolution(unittest.TestCase):
    """Per-mode prompts for routed tasks."""

    def test_simple_prompt(self):
        prompt = _get_prompt("single_tool_call", "direct")
        self.assertIn("echo", prompt.lower())

    def test_per_mode_prompt_search(self):
        prompt = _get_prompt("search_invoke_coached", "search")
        self.assertIn("search_tools", prompt)

    def test_per_mode_prompt_direct(self):
        prompt = _get_prompt("search_invoke_coached", "direct")
        self.assertNotIn("search_tools", prompt)

    def test_per_mode_prompt_baseline(self):
        prompt = _get_prompt("search_invoke_coached", "baseline")
        self.assertIn("echo", prompt.lower())

    def test_natural_prompt_same_across_modes(self):
        search = _get_prompt("search_invoke_natural", "search")
        direct = _get_prompt("search_invoke_natural", "direct")
        self.assertEqual(search, direct)


class TestSearchQualityCases(unittest.TestCase):
    """SEARCH_QUALITY_CASES structure is valid."""

    def test_cases_not_empty(self):
        self.assertGreater(len(SEARCH_QUALITY_CASES), 0)

    def test_all_cases_have_required_fields(self):
        for i, case in enumerate(SEARCH_QUALITY_CASES):
            self.assertIn("query", case, f"case {i} missing 'query'")
            self.assertIn("expected_prefix", case, f"case {i} missing 'expected_prefix'")
            self.assertIsInstance(case["query"], str)
            self.assertIsInstance(case["expected_prefix"], str)
            self.assertGreater(len(case["query"]), 0, f"case {i} has empty query")
            self.assertGreater(len(case["expected_prefix"]), 0, f"case {i} has empty expected_prefix")

    def test_cases_have_expected_count(self):
        self.assertEqual(len(SEARCH_QUALITY_CASES), 10)


class TestOllamaModelId(unittest.TestCase):
    """Model ID string construction."""

    def test_plain_model(self):
        self.assertEqual(_ollama_model_id("qwen2.5:3b"), "openai:qwen2.5:3b")

    def test_already_prefixed(self):
        self.assertEqual(_ollama_model_id("openai:qwen2.5:3b"), "openai:qwen2.5:3b")

    def test_all_defaults(self):
        for model in DEFAULT_MODELS:
            result = _ollama_model_id(model)
            self.assertTrue(result.startswith("openai:"), f"{model} -> {result}")


class TestModeResolution(unittest.TestCase):
    def test_all_mode(self):
        self.assertEqual(_resolve_modes("all"), ["baseline", "search", "direct"])

    def test_single_mode(self):
        self.assertEqual(_resolve_modes("search"), ["search"])
        self.assertEqual(_resolve_modes("direct"), ["direct"])


class TestTaskModeCompat(unittest.TestCase):
    def test_tier1_direct_only(self):
        self.assertTrue(_tasks_for_mode("single_tool_call", "direct"))
        self.assertFalse(_tasks_for_mode("single_tool_call", "search"))
        self.assertFalse(_tasks_for_mode("single_tool_call", "baseline"))

    def test_tier2_search(self):
        self.assertTrue(_tasks_for_mode("search_coached", "search"))
        self.assertFalse(_tasks_for_mode("search_coached", "direct"))

    def test_routed_all_modes(self):
        self.assertTrue(_tasks_for_mode("search_invoke_coached", "search"))
        self.assertTrue(_tasks_for_mode("search_invoke_coached", "direct"))
        self.assertTrue(_tasks_for_mode("search_invoke_coached", "baseline"))


class TestTierFiltering(unittest.TestCase):
    def test_tier_1_only(self):
        tasks = _tasks_for_tiers([1])
        self.assertIn("single_tool_call", tasks)
        self.assertIn("format_compliance", tasks)
        self.assertNotIn("search_coached", tasks)

    def test_tier_2_only(self):
        tasks = _tasks_for_tiers([2])
        self.assertIn("search_coached", tasks)
        self.assertIn("search_natural", tasks)
        self.assertNotIn("single_tool_call", tasks)

    def test_tier_3_only(self):
        tasks = _tasks_for_tiers([3])
        self.assertEqual(tasks, ["search_precision"])

    def test_multiple_tiers(self):
        tasks = _tasks_for_tiers([1, 2])
        self.assertIn("single_tool_call", tasks)
        self.assertIn("search_coached", tasks)
        self.assertNotIn("search_precision", tasks)


class TestToolExecutionSuccess(unittest.TestCase):
    def test_tool_task_with_calls(self):
        self.assertTrue(_tool_execution_success("single_tool_call", ["echo"], pseudo_tool_text=False))

    def test_tool_task_no_calls(self):
        self.assertFalse(_tool_execution_success("single_tool_call", [], pseudo_tool_text=False))

    def test_tool_task_pseudo(self):
        self.assertFalse(_tool_execution_success("single_tool_call", ["echo"], pseudo_tool_text=True))

    def test_no_tool_task(self):
        self.assertTrue(_tool_execution_success("search_precision", [], pseudo_tool_text=False))


class TestFailureReason(unittest.TestCase):
    def test_success(self):
        result = _failure_reason(
            task_name="single_tool_call",
            tool_names=["echo"],
            output_preview="TOOL_OK",
            answer_format_success=True,
            expected_tool_family=True,
        )
        self.assertIsNone(result)

    def test_no_tool_call(self):
        result = _failure_reason(
            task_name="single_tool_call",
            tool_names=[],
            output_preview="I can't do that",
            answer_format_success=False,
            expected_tool_family=False,
        )
        self.assertEqual(result, "no_tool_call")

    def test_answer_format_failed(self):
        result = _failure_reason(
            task_name="single_tool_call",
            tool_names=["echo"],
            output_preview="Wrong format",
            answer_format_success=False,
            expected_tool_family=True,
        )
        self.assertEqual(result, "answer_format_failed")


if __name__ == "__main__":
    unittest.main()
