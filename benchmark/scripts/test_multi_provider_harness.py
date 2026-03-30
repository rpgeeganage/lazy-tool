#!/usr/bin/env python3
"""
Unit tests for multi-provider benchmark harness logic.

No network calls, no API keys needed. Tests the pure logic functions.

Usage:
  python -m pytest benchmark/scripts/test_multi_provider_harness.py -v
  python benchmark/scripts/test_multi_provider_harness.py
"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

# Add benchmark dir to path so we can import the harness
_REPO_ROOT = Path(__file__).resolve().parent.parent.parent
sys.path.insert(0, str(_REPO_ROOT / "benchmark"))

from run_multi_provider_benchmark import (
    TASKS,
    PROVIDERS,
    _evaluate_answer_format,
    _failure_reason,
    _get_prompt,
    _looks_like_pseudo_tool_output,
    _resolve_modes,
    _tasks_for_mode,
    _tool_execution_success,
    _used_expected_tool_family,
)


class TestAnswerFormatEvaluation(unittest.TestCase):
    def test_no_tool_ok(self):
        self.assertTrue(_evaluate_answer_format("no_tool", "ok", strict=False))
        self.assertTrue(_evaluate_answer_format("no_tool", "ok", strict=True))

    def test_no_tool_long_fails_strict(self):
        self.assertFalse(_evaluate_answer_format("no_tool", "ok this is long", strict=True))

    def test_search_smoke_ok(self):
        self.assertTrue(_evaluate_answer_format("search_tools_smoke", "SEARCH_OK 5 hits", strict=False))
        self.assertTrue(_evaluate_answer_format("search_tools_smoke", "SEARCH_OK 5", strict=True))

    def test_ambiguous_ok(self):
        self.assertTrue(_evaluate_answer_format("ambiguous_search", "AMBIG_OK 2", strict=False))
        self.assertTrue(_evaluate_answer_format("ambiguous_search", "AMBIG_OK 2", strict=True))

    def test_routed_echo_ok(self):
        self.assertTrue(_evaluate_answer_format("routed_echo", "ECHO_OK benchmark-test response", strict=False))

    def test_routed_echo_content_match(self):
        self.assertTrue(_evaluate_answer_format("routed_echo", "The echo returned benchmark-test", strict=False))

    def test_routed_file_read_ok(self):
        self.assertTrue(_evaluate_answer_format(
            "routed_file_read",
            "FILE_OK hello from lazy-tool benchmark",
            strict=False,
        ))

    def test_routed_file_read_missing_content(self):
        self.assertFalse(_evaluate_answer_format("routed_file_read", "FILE_OK something else", strict=False))

    def test_routed_prompt_ok(self):
        self.assertTrue(_evaluate_answer_format("routed_prompt", "PROMPT_OK got a test prompt", strict=False))

    def test_routed_prompt_fail(self):
        self.assertFalse(_evaluate_answer_format("routed_prompt", "I don't know", strict=False))


class TestToolFamilyMatching(unittest.TestCase):
    def test_search_tools_match(self):
        self.assertTrue(_used_expected_tool_family("search_tools_smoke", ["search_tools"]))

    def test_routed_echo_search_mode(self):
        self.assertTrue(_used_expected_tool_family("routed_echo", ["search_tools", "invoke_proxy_tool"]))

    def test_routed_echo_direct_mode(self):
        self.assertTrue(_used_expected_tool_family("routed_echo", ["echo"]))

    def test_no_tool_no_calls(self):
        self.assertTrue(_used_expected_tool_family("no_tool", []))

    def test_routed_file_read_match(self):
        self.assertTrue(_used_expected_tool_family("routed_file_read", ["search_tools", "invoke_proxy_tool"]))
        self.assertTrue(_used_expected_tool_family("routed_file_read", ["read_file"]))


class TestToolExecutionSuccess(unittest.TestCase):
    def test_no_tool_task_no_calls(self):
        self.assertTrue(_tool_execution_success("no_tool", [], pseudo_tool_text=False))

    def test_no_tool_task_with_calls(self):
        self.assertFalse(_tool_execution_success("no_tool", ["search_tools"], pseudo_tool_text=False))

    def test_echo_task_with_calls(self):
        self.assertTrue(_tool_execution_success("routed_echo", ["echo"], pseudo_tool_text=False))

    def test_echo_task_pseudo_tool(self):
        self.assertFalse(_tool_execution_success("routed_echo", ["echo"], pseudo_tool_text=True))


class TestFailureReason(unittest.TestCase):
    def test_success_no_reason(self):
        result = _failure_reason(
            task_name="routed_echo",
            tool_names=["echo"],
            output_preview="ECHO_OK benchmark-test",
            answer_format_success=True,
            expected_tool_family=True,
        )
        self.assertIsNone(result)

    def test_no_tool_call(self):
        result = _failure_reason(
            task_name="routed_echo",
            tool_names=[],
            output_preview="I can't do that",
            answer_format_success=False,
            expected_tool_family=False,
        )
        self.assertEqual(result, "no_tool_call")

    def test_answer_format_failed(self):
        result = _failure_reason(
            task_name="routed_echo",
            tool_names=["echo"],
            output_preview="Wrong format response",
            answer_format_success=False,
            expected_tool_family=True,
        )
        self.assertEqual(result, "answer_format_failed")


class TestPseudoToolDetection(unittest.TestCase):
    def test_normal_text(self):
        self.assertFalse(_looks_like_pseudo_tool_output("ok"))

    def test_function_tag(self):
        self.assertTrue(_looks_like_pseudo_tool_output('<function=search_tools{"query": "echo"}>'))

    def test_tool_call_text(self):
        self.assertTrue(_looks_like_pseudo_tool_output("I'll use tool_call to search"))


class TestPromptResolution(unittest.TestCase):
    def test_simple_prompt(self):
        prompt = _get_prompt("no_tool", "baseline")
        self.assertIn("ok", prompt.lower())

    def test_per_mode_prompt(self):
        search_prompt = _get_prompt("routed_echo", "search")
        direct_prompt = _get_prompt("routed_echo", "direct")
        self.assertIn("search_tools", search_prompt)
        self.assertNotIn("search_tools", direct_prompt)

    def test_baseline_prompt(self):
        prompt = _get_prompt("routed_echo", "baseline")
        self.assertIn("echo", prompt.lower())


class TestModeResolution(unittest.TestCase):
    def test_all_mode(self):
        self.assertEqual(_resolve_modes("all"), ["baseline", "search", "direct"])

    def test_single_mode(self):
        self.assertEqual(_resolve_modes("search"), ["search"])
        self.assertEqual(_resolve_modes("direct"), ["direct"])
        self.assertEqual(_resolve_modes("baseline"), ["baseline"])


class TestTaskModeCompat(unittest.TestCase):
    def test_no_tool_all_modes(self):
        self.assertTrue(_tasks_for_mode("no_tool", "baseline"))
        self.assertTrue(_tasks_for_mode("no_tool", "search"))
        self.assertTrue(_tasks_for_mode("no_tool", "direct"))

    def test_search_only_task(self):
        self.assertTrue(_tasks_for_mode("search_tools_smoke", "search"))
        self.assertFalse(_tasks_for_mode("search_tools_smoke", "baseline"))
        self.assertFalse(_tasks_for_mode("search_tools_smoke", "direct"))

    def test_routed_all_modes(self):
        self.assertTrue(_tasks_for_mode("routed_echo", "baseline"))
        self.assertTrue(_tasks_for_mode("routed_echo", "search"))
        self.assertTrue(_tasks_for_mode("routed_echo", "direct"))


class TestTaskDefinitionCompleteness(unittest.TestCase):
    """Ensure all tasks have required fields."""

    def test_all_tasks_have_required_fields(self):
        required = {"prompt", "expect_tool_calls", "expected_tool_hints", "description", "modes"}
        for name, task in TASKS.items():
            for field in required:
                self.assertIn(field, task, f"task '{name}' missing field '{field}'")

    def test_all_routed_tasks_have_per_mode_prompts(self):
        for name, task in TASKS.items():
            if name.startswith("routed_"):
                self.assertIsInstance(
                    task["prompt"], dict,
                    f"routed task '{name}' should have per-mode prompts (dict)",
                )
                for mode in ["search", "direct", "baseline"]:
                    self.assertIn(
                        mode, task["prompt"],
                        f"routed task '{name}' missing prompt for mode '{mode}'",
                    )


class TestProviderConfig(unittest.TestCase):
    def test_all_providers_have_required_fields(self):
        for name, cfg in PROVIDERS.items():
            self.assertIn("env_key", cfg, f"provider '{name}' missing env_key")
            self.assertIn("default_model", cfg, f"provider '{name}' missing default_model")
            self.assertIn("prefix", cfg, f"provider '{name}' missing prefix")


if __name__ == "__main__":
    unittest.main()
