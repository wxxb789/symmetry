#!/usr/bin/env python3
"""Parser regressions for the OMP usage-extraction CLI.

Every record written by this test file is synthetic. These tests exercise
parsing, refusal and redaction behavior only: they are parser regression
evidence, not live acceptance evidence for any real session or route.
"""

import hashlib
import importlib.util
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("model-routing-omp-usage.py")
CANARY = "CANARY-SECRET-9f3a"
COST_PROVENANCE = "observed_omp_session_harness_cost_total_sum_estimate_no_provider_billing_proof"
ROW_FIELDS = {"stage", "session_ref", "non_model_reason", "model_ids", "prompt_hashes",
              "tokens", "reported_cost", "estimated_cost", "cost_provenance",
              "price_version", "wall_time", "metadata", "limitations", "known_subtotals"}
METADATA_FIELDS = frozenset({
    "source_format", "source_sha256", "credential_route", "routing_provenance",
    "accounting_complete", "session_ref_source", "records", "session_records",
    "session_init_records", "session_init", "resolved_model_identity", "model_changes",
    "thinking_level_changes", "fallback_records", "entry_types", "assistant_messages",
    "assistant_messages_with_usage", "assistant_messages_with_cost_total",
    "assistant_messages_without_model_identity", "user_messages", "custom_messages",
    "usage_includes_all_assistant_attempts", "served_model_differs_from_resolved_model",
})


def prompt_hash(text):
    return "sha256:" + hashlib.sha256(text.encode("utf-8")).hexdigest()


def load_runner_module():
    """Import the acceptance runner so exported rows can be validated in-process."""
    path = pathlib.Path(__file__).with_name("model-routing-evaluation.py")
    spec = importlib.util.spec_from_file_location("model_routing_evaluation", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class OmpUsageExtractionTest(unittest.TestCase):
    def run_arguments(self, arguments):
        return subprocess.run([sys.executable, str(SCRIPT), *arguments],
                              capture_output=True, encoding="utf-8")

    def run_cli(self, records=None, *, raw_lines=None, stage="implementation",
                credential_route="safe-metadata", provenance="synthetic-parser-regression",
                price_version=None):
        with tempfile.TemporaryDirectory() as directory:
            session = pathlib.Path(directory) / "session.jsonl"
            lines = list(raw_lines) if raw_lines is not None else [
                json.dumps(record, ensure_ascii=False) for record in records]
            session.write_text("\n".join(lines) + "\n", encoding="utf-8")
            arguments = ["--session", str(session), "--stage", stage,
                         "--credential-route", credential_route, "--provenance", provenance]
            if price_version is not None:
                arguments += ["--price-version", price_version]
            return self.run_arguments(arguments)

    def row(self, records, **options):
        completed = self.run_cli(records, **options)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(completed.stderr, "")
        return json.loads(completed.stdout)

    def export_id_less_session(self, path, prompt="synthetic user request"):
        """Write one ID-less OMP export at path and return the extractor row."""
        lines = [
            json.dumps({"type": "message", "timestamp": "2026-11-15T10:00:00Z",
                        "message": {"role": "user", "content": prompt}}),
            json.dumps({"type": "message", "timestamp": "2026-11-15T10:00:05Z",
                        "message": {"role": "assistant", "model": "m",
                                    "provider": "safe-provider", "api": "safe-api",
                                    "usage": self.usage()}}),
        ]
        path.write_text("\n".join(lines) + "\n", encoding="utf-8")
        completed = self.run_arguments(["--session", str(path), "--stage", "implementation",
                                        "--credential-route", "safe-metadata",
                                        "--provenance", "synthetic-parser-regression"])
        self.assertEqual(completed.returncode, 0, completed.stderr)
        return json.loads(completed.stdout)

    def phases_with(self, *model_rows):
        """Cover all six stages around the supplied model rows in stage order.

        The second row is imported under the review stage, the exact double-import the
        finding reproduced: one session charged once per phase.
        """
        usage = {"repair_count": 0, "wall_time": 12.0, "phases": [
            self.non_model_phase("exploration"),
            model_rows[0],
            self.non_model_phase("verification"),
            dict(model_rows[1], stage="review"),
            self.non_model_phase("repair"),
            self.non_model_phase("failed_attempt"),
        ]}
        return usage

    def session_record(self, **overrides):
        record = {"type": "session", "id": "synthetic-session-1",
                  "timestamp": "2026-11-15T10:00:00Z",
                  "cwd": "C:/private/synthetic-work", "parentSession": None}
        record.update(overrides)
        return record

    def init_record(self, **overrides):
        record = {"type": "session_init", "timestamp": "2026-11-15T10:00:01Z",
                  "agent": "scout", "modelRole": "lightweight",
                  "resolvedModel": "deepseek-v4-flash", "task": "synthetic task text",
                  "systemPrompt": "synthetic system prompt", "readOnly": True}
        record.update(overrides)
        return record

    def user_record(self, content="synthetic user request", timestamp="2026-11-15T10:00:03Z"):
        return {"type": "message", "timestamp": timestamp,
                "message": {"role": "user", "content": content}}

    def assistant_record(self, timestamp="2026-11-15T10:00:05Z", **payload_overrides):
        payload = {"role": "assistant", "model": "deepseek-v4.1-flash-fast",
                   "provider": "tencent-litellm", "api": "openai-completions",
                   "usage": self.usage()}
        payload.update(payload_overrides)
        return {"type": "message", "timestamp": timestamp, "message": payload}

    def usage(self, **overrides):
        usage = {"input": 100, "output": 20, "cacheRead": 5, "cacheWrite": 2,
                 "totalTokens": 127, "reasoningTokens": 3,
                 "cost": {"input": 0.001, "output": 0.002, "cacheRead": 0.0001,
                          "cacheWrite": 0.0001, "total": 0.0032}}
        usage.update(overrides)
        return usage

    def test_complete_session_row_records_allowlisted_metadata(self):
        records = [
            self.session_record(),
            self.init_record(),
            {"type": "model_change", "timestamp": "2026-11-15T10:00:02Z",
             "model": "deepseek-v4.1-flash-fast"},
            self.user_record("first request"),
            self.user_record("second request", timestamp="2026-11-15T10:00:04Z"),
            self.assistant_record(),
        ]
        row = self.row(records)
        self.assertEqual(set(row), ROW_FIELDS)
        self.assertEqual(row["stage"], "implementation")
        self.assertEqual(row["session_ref"], "synthetic-session-1")
        self.assertIsNone(row["non_model_reason"])
        self.assertEqual(row["model_ids"], [{
            "model": "deepseek-v4.1-flash-fast", "provider": "tencent-litellm",
            "api": "openai-completions", "assistant_messages": 1}])
        self.assertEqual(row["prompt_hashes"], [
            {"kind": "system_prompt", "sha256": prompt_hash("synthetic system prompt")},
            {"kind": "task", "sha256": prompt_hash("synthetic task text")},
            {"kind": "user_message", "index": 0, "sha256": prompt_hash("first request")},
            {"kind": "user_message", "index": 1, "sha256": prompt_hash("second request")},
        ])
        self.assertEqual(row["tokens"], {"input": 100, "output": 20, "cached": 7})
        self.assertIsNone(row["reported_cost"])
        self.assertAlmostEqual(row["estimated_cost"], 0.0032)
        self.assertEqual(row["cost_provenance"], COST_PROVENANCE)
        self.assertIsNone(row["price_version"])
        self.assertEqual(row["wall_time"], 5.0)
        metadata = row["metadata"]
        self.assertEqual(set(metadata), METADATA_FIELDS)
        self.assertEqual(metadata["source_format"], "omp-session-jsonl")
        self.assertEqual(metadata["credential_route"], "safe-metadata")
        self.assertEqual(metadata["routing_provenance"], "synthetic-parser-regression")
        self.assertFalse(metadata["accounting_complete"])
        self.assertEqual(metadata["session_ref_source"], "observed_id")
        self.assertEqual(metadata["session_init"], {
            "agent": "scout", "model_role": "lightweight",
            "resolved_model": "deepseek-v4-flash", "read_only": True})
        self.assertEqual(metadata["session_init_records"], 1)
        self.assertTrue(metadata["usage_includes_all_assistant_attempts"])
        self.assertEqual(metadata["model_changes"],
                         [{"record": 2, "model": "deepseek-v4.1-flash-fast"}])
        self.assertEqual(metadata["entry_types"],
                         {"session": 1, "session_init": 1, "model_change": 1, "message": 3})
        self.assertEqual(metadata["assistant_messages"], 1)
        self.assertEqual(sorted(row["limitations"]),
                         ["price_version_absent", "provider_billing_not_observed"])
        self.assertEqual(row["known_subtotals"]["input"], {"sum": 100, "messages": 1})
        self.assertEqual(row["known_subtotals"]["cost"]["total"]["messages"], 1)
        self.assertAlmostEqual(row["known_subtotals"]["cost"]["total"]["sum"], 0.0032)

    def test_all_six_stages_are_accepted(self):
        stages = ("exploration", "implementation", "verification", "review", "repair",
                  "failed_attempt")
        for stage in stages:
            with self.subTest(stage=stage):
                row = self.row([self.session_record(), self.init_record()], stage=stage)
                self.assertEqual(row["stage"], stage)

    def test_usage_sums_include_failed_and_aborted_attempts(self):
        records = [
            self.session_record(),
            self.init_record(),
            self.assistant_record(timestamp="2026-11-15T10:00:02Z", failed=True,
                                  usage=self.usage(input=10, output=1, cacheRead=0, cacheWrite=0,
                                                   cost={"total": 0.0001})),
            self.assistant_record(timestamp="2026-11-15T10:00:03Z", aborted=True,
                                  usage=self.usage(input=20, output=2, cacheRead=1, cacheWrite=0,
                                                   cost={"total": 0.0002})),
            self.assistant_record(timestamp="2026-11-15T10:00:04Z",
                                  usage=self.usage(input=30, output=3, cacheRead=2, cacheWrite=1,
                                                   cost={"total": 0.0003})),
        ]
        row = self.row(records)
        self.assertEqual(row["tokens"], {"input": 60, "output": 6, "cached": 4})
        self.assertAlmostEqual(row["estimated_cost"], 0.0006)
        self.assertEqual(row["metadata"]["assistant_messages"], 3)
        self.assertTrue(row["metadata"]["usage_includes_all_assistant_attempts"])
        self.assertEqual(row["known_subtotals"]["cached"], {"sum": 4, "messages": 3})
        self.assertEqual(row["model_ids"][0]["assistant_messages"], 3)

    def test_missing_usage_and_cost_remain_unknown_with_known_subtotals(self):
        records = [
            self.session_record(),
            self.init_record(),
            self.assistant_record(timestamp="2026-11-15T10:00:02Z",
                                  usage=self.usage(input=7, output=8, cacheRead=1, cacheWrite=1,
                                                   cost={"total": 0.01})),
            self.assistant_record(timestamp="2026-11-15T10:00:03Z", usage=None),
        ]
        row = self.row(records)
        self.assertEqual(row["tokens"], {"input": None, "output": None, "cached": None})
        self.assertIsNone(row["estimated_cost"])
        self.assertIsNone(row["cost_provenance"])
        self.assertEqual(row["known_subtotals"]["input"], {"sum": 7, "messages": 1})
        self.assertEqual(row["known_subtotals"]["cost"]["total"], {"sum": 0.01, "messages": 1})
        self.assertIn("assistant_usage_missing", row["limitations"])
        self.assertIn("assistant_cost_missing", row["limitations"])
        self.assertFalse(row["metadata"]["accounting_complete"])

    def test_partial_usage_fields_leave_only_that_total_unknown(self):
        usage = {"input": 10, "output": 4, "cost": {"total": 0.5}}
        row = self.row([self.session_record(), self.init_record(), self.assistant_record(usage=usage)])
        self.assertEqual(row["tokens"]["input"], 10)
        self.assertEqual(row["tokens"]["output"], 4)
        self.assertIsNone(row["tokens"]["cached"])
        self.assertAlmostEqual(row["estimated_cost"], 0.5)
        self.assertIn("assistant_usage_fields_missing", row["limitations"])

    def test_observed_zero_usage_is_known_zero_not_missing(self):
        zero_usage = {"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0,
                      "totalTokens": 0, "reasoningTokens": 0,
                      "cost": {"input": 0, "output": 0, "cacheRead": 0,
                               "cacheWrite": 0, "total": 0}}
        row = self.row([self.session_record(), self.init_record(),
                        self.assistant_record(usage=zero_usage)])
        self.assertEqual(row["tokens"], {"input": 0, "output": 0, "cached": 0})
        self.assertEqual(row["estimated_cost"], 0)
        self.assertEqual(row["cost_provenance"], COST_PROVENANCE)
        self.assertNotIn("assistant_usage_missing", row["limitations"])

    def test_supplied_price_version_is_echoed_without_inventing_prices(self):
        row = self.row([self.session_record(), self.init_record(), self.assistant_record()],
                       price_version="provider-prices-2026-11")
        self.assertEqual(row["price_version"], "provider-prices-2026-11")
        self.assertNotIn("price_version_absent", row["limitations"])
        self.assertAlmostEqual(row["estimated_cost"], 0.0032)
        self.assertIsNone(row["reported_cost"])

    def test_price_version_without_observed_cost_is_marked(self):
        row = self.row([self.session_record(), self.init_record()],
                       price_version="provider-prices-2026-11")
        self.assertEqual(row["price_version"], "provider-prices-2026-11")
        self.assertIsNone(row["estimated_cost"])
        self.assertIn("price_version_without_observed_cost", row["limitations"])

    def test_no_assistant_messages_keeps_totals_unknown(self):
        row = self.row([self.session_record(), self.init_record()])
        self.assertEqual(row["tokens"], {"input": None, "output": None, "cached": None})
        self.assertIsNone(row["estimated_cost"])
        self.assertEqual(row["model_ids"], [])
        self.assertIn("no_assistant_messages", row["limitations"])
        self.assertEqual(row["prompt_hashes"], [
            {"kind": "system_prompt", "sha256": prompt_hash("synthetic system prompt")},
            {"kind": "task", "sha256": prompt_hash("synthetic task text")},
        ])

    def test_served_model_difference_from_resolved_model_is_visible(self):
        row = self.row([self.session_record(), self.init_record(), self.assistant_record()])
        self.assertEqual(row["metadata"]["session_init"]["resolved_model"], "deepseek-v4-flash")
        self.assertEqual(row["metadata"]["resolved_model_identity"],
                         {"provider": None, "model": "deepseek-v4-flash"})
        self.assertEqual([item["model"] for item in row["model_ids"]],
                         ["deepseek-v4.1-flash-fast"])
        self.assertTrue(row["metadata"]["served_model_differs_from_resolved_model"])

    def test_matching_served_and_resolved_model_is_reported_as_matching(self):
        records = [self.session_record(),
                   self.init_record(resolvedModel="deepseek-v4.1-flash-fast"),
                   self.assistant_record()]
        row = self.row(records)
        self.assertFalse(row["metadata"]["served_model_differs_from_resolved_model"])

    def test_distinct_served_identities_are_aggregated(self):
        records = [
            self.session_record(),
            self.init_record(),
            self.assistant_record(timestamp="2026-11-15T10:00:02Z"),
            self.assistant_record(timestamp="2026-11-15T10:00:03Z", model="gpt-5.6-luna",
                                  provider="openai", api="responses",
                                  usage=self.usage(input=1, output=2, cacheRead=0,
                                                   cacheWrite=0, cost={"total": 0.1})),
        ]
        row = self.row(records)
        self.assertEqual([item["model"] for item in row["model_ids"]],
                         ["deepseek-v4.1-flash-fast", "gpt-5.6-luna"])
        self.assertEqual([item["assistant_messages"] for item in row["model_ids"]], [1, 1])
        self.assertEqual(row["tokens"]["input"], 101)

    def test_missing_model_identity_is_counted_and_not_invented(self):
        row = self.row([self.session_record(), self.init_record(),
                        self.assistant_record(model=None)])
        self.assertEqual(row["model_ids"], [])
        self.assertEqual(row["metadata"]["assistant_messages_without_model_identity"], 1)
        self.assertIn("assistant_model_identity_missing", row["limitations"])

    def test_secret_canary_never_reaches_stdout_or_stderr(self):
        records = [
            self.session_record(cwd="C:/private/" + CANARY, parentSession=CANARY),
            self.init_record(task=CANARY + " task body", systemPrompt=CANARY + " system body"),
            self.user_record(CANARY + " user body"),
            self.assistant_record(content=CANARY, error=CANARY,
                                  tool={"payload": CANARY}, usage=self.usage()),
            {"type": "tool_result", "timestamp": "2026-11-15T10:00:06Z", "content": CANARY},
            {"type": "config", "timestamp": "2026-11-15T10:00:07Z", "apiKey": CANARY,
             "credentials": {"token": CANARY}},
            {"type": "document", "timestamp": "2026-11-15T10:00:08Z", "text": CANARY},
        ]
        completed = self.run_cli(records)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertNotIn(CANARY, completed.stdout)
        self.assertNotIn(CANARY, completed.stderr)
        row = json.loads(completed.stdout)
        self.assertEqual(row["metadata"]["entry_types"]["tool_result"], 1)
        self.assertEqual(row["metadata"]["entry_types"]["config"], 1)
        digests = [item["sha256"] for item in row["prompt_hashes"]]
        self.assertEqual(len(digests), 3)
        self.assertNotIn(CANARY, "".join(digests))

    def test_thinking_level_and_fallback_records_capture_safe_metadata_only(self):
        records = [
            self.session_record(),
            self.init_record(),
            {"type": "thinking_level_change", "timestamp": "2026-11-15T10:00:02Z",
             "level": "high", "text": CANARY},
            {"type": "model_fallback", "timestamp": "2026-11-15T10:00:03Z",
             "model": "fallback-model", "provider": "fallback-provider", "reason": CANARY},
            self.assistant_record(),
        ]
        completed = self.run_cli(records)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertNotIn(CANARY, completed.stdout)
        row = json.loads(completed.stdout)
        self.assertEqual(row["metadata"]["thinking_level_changes"],
                         [{"record": 2, "level": "high"}])
        self.assertEqual(row["metadata"]["fallback_records"],
                         [{"record": 3, "model": "fallback-model",
                           "provider": "fallback-provider"}])

    def test_merged_exports_and_repeated_entry_ids_are_refused(self):
        def export(session):
            records = [
                self.session_record(id=session),
                self.init_record(),
                self.user_record("request for " + session),
                self.assistant_record(),
            ]
            for index, record in enumerate(records):
                if record["type"] != "session":
                    record["id"] = f"{session}:entry-{index}"
            return records

        merged = self.run_cli(export("synthetic-session-a") + export("synthetic-session-b"))
        self.assertEqual(merged.returncode, 1, merged.stdout)
        self.assertEqual(merged.stderr,
                         "refused: session file contains more than one session id; "
                         "import one private session file per usage row\n")
        self.assertEqual(merged.stdout, "")

        doubled = self.run_cli(export("synthetic-session-a") + export("synthetic-session-a"))
        self.assertEqual(doubled.returncode, 1, doubled.stdout)
        self.assertEqual(doubled.stderr,
                         "refused: session record stream repeats an entry id; "
                         "a duplicated or merged export cannot be counted once\n")
        self.assertEqual(doubled.stdout, "")

        records = export("synthetic-session-a")
        records[-1]["message"]["usage"] = {"input": 5, "output": 1, "cacheRead": 0, "cacheWrite": 0}
        row = self.row(records)
        self.assertEqual(row["session_ref"], "synthetic-session-a")
        self.assertEqual(row["tokens"], {"input": 5, "output": 1, "cached": 0})
        self.assertIsNone(row["estimated_cost"])
        self.assertIsNone(row["cost_provenance"])
        self.assertIn("assistant_cost_missing", row["limitations"])

    def test_duplicate_json_keys_and_malformed_records_are_refused(self):
        cases = {
            "duplicate key": ['{"type":"session","id":"a","id":"b"}'],
            "duplicate nested key": ['{"type":"message","message":{"role":"assistant","role":"user"}}'],
            "malformed json": ['{"type":"session",'],
            "non object": ["[1, 2]"],
            "unsafe type": ['{"type":"two words"}'],
            "unsafe role": ['{"type":"message","message":{"role":"two words"}}'],
            "non-finite number": ['{"type":"message","message":{"role":"assistant","usage":{"input":NaN}}}'],
            "usage not an object": ['{"type":"message","message":{"role":"assistant","usage":"lots"}}'],
        }
        for label, lines in cases.items():
            with self.subTest(case=label):
                completed = self.run_cli(raw_lines=lines)
                self.assertEqual(completed.returncode, 1, completed.stdout)
                self.assertTrue(completed.stderr.startswith("refused: "), completed.stderr)
                self.assertEqual(completed.stdout, "")

    def test_invalid_numeric_and_identity_values_are_refused_without_echoing_them(self):
        cases = [
            ("negative tokens", {"input": -1}),
            ("boolean tokens", {"input": True}),
        ]
        for label, overrides in cases:
            with self.subTest(case=label):
                usage = {"input": 1, "output": 1, "cacheRead": 0, "cacheWrite": 0,
                         "cost": {"total": 0.0}}
                usage.update(overrides)
                completed = self.run_cli([self.session_record(), self.init_record(),
                                          self.assistant_record(usage=usage)])
                self.assertEqual(completed.returncode, 1, completed.stdout)
                self.assertTrue(completed.stderr.startswith("refused: "), completed.stderr)
                self.assertEqual(completed.stdout, "")
        for label, cost in (("negative cost", {"total": -0.5}), ("unsafe cost", "0.5")):
            with self.subTest(case=label):
                usage = {"input": 1, "output": 1, "cacheRead": 0, "cacheWrite": 0, "cost": cost}
                completed = self.run_cli([self.session_record(), self.init_record(),
                                          self.assistant_record(usage=usage)])
                self.assertEqual(completed.returncode, 1, completed.stdout)
                self.assertEqual(completed.stdout, "")
        completed = self.run_cli([self.session_record(), self.init_record(),
                                  self.assistant_record(model="deepseek flash " + CANARY)])
        self.assertEqual(completed.returncode, 1)
        self.assertEqual(completed.stderr,
                         "refused: session assistant record has an unsafe model identity\n")
        self.assertNotIn(CANARY, completed.stderr)

    def test_cli_usage_errors_are_rejected(self):
        cases = {
            "unknown stage": ["--session", "unused.jsonl", "--stage", "polishing",
                              "--credential-route", "safe-metadata",
                              "--provenance", "synthetic"],
            "missing stage": ["--session", "unused.jsonl",
                              "--credential-route", "safe-metadata",
                              "--provenance", "synthetic"],
            "missing session": ["--stage", "review", "--credential-route", "safe-metadata",
                                "--provenance", "synthetic"],
            "unsafe credential route": ["--session", "unused.jsonl", "--stage", "review",
                                        "--credential-route", "token=abc",
                                        "--provenance", "synthetic"],
            "unsafe provenance": ["--session", "unused.jsonl", "--stage", "review",
                                  "--credential-route", "safe-metadata",
                                  "--provenance", "two words"],
            "unsafe price version": ["--session", "unused.jsonl", "--stage", "review",
                                     "--credential-route", "safe-metadata",
                                     "--provenance", "synthetic",
                                     "--price-version", "price version"],
            "removed failed alias": ["--session", "unused.jsonl", "--stage", "failed",
                                     "--credential-route", "safe-metadata",
                                     "--provenance", "synthetic"],
        }
        for label, arguments in cases.items():
            with self.subTest(case=label):
                completed = self.run_arguments(arguments)
                self.assertEqual(completed.returncode, 2, completed.stderr)
                self.assertEqual(completed.stdout, "")

    def test_session_reference_stays_opaque_without_a_safe_id(self):
        raw_lines = [json.dumps({"type": "message", "timestamp": "2026-11-15T10:00:00Z",
                                 "message": {"role": "assistant", "model": "m",
                                             "usage": self.usage()}})]
        completed = self.run_cli(raw_lines=raw_lines)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        row = json.loads(completed.stdout)
        self.assertTrue(row["session_ref"].startswith("sha256:"))
        self.assertEqual(row["metadata"]["session_ref_source"], "source_sha256")
        self.assertIn("session_entry_absent", row["limitations"])
        self.assertIn("session_id_absent", row["limitations"])
        row = self.row([self.session_record(id="session id with spaces/path"),
                        self.assistant_record()])
        self.assertTrue(row["session_ref"].startswith("sha256:"))
        self.assertEqual(row["metadata"]["session_ref_source"], "hashed_id")
        self.assertIn("session_id_hashed", row["limitations"])

    def test_byte_identical_id_less_exports_share_one_session_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            first_path = pathlib.Path(directory) / "first-session.jsonl"
            second_path = pathlib.Path(directory) / "nested" / "copy-session.jsonl"
            first = self.export_id_less_session(first_path)
            second_path.parent.mkdir()
            second_path.write_bytes(first_path.read_bytes())
            second = self.export_id_less_session(second_path)
        self.assertEqual(first["metadata"]["session_ref_source"], "source_sha256")
        self.assertEqual(second["metadata"]["session_ref_source"], "source_sha256")
        self.assertEqual(first["metadata"]["source_sha256"], second["metadata"]["source_sha256"])
        self.assertEqual(first["session_ref"], second["session_ref"])

        # The runner must refuse to count the same evidence twice.
        module = load_runner_module()
        usage = self.phases_with(first, second)
        with self.assertRaises(module.Refused) as refusal:
            module.validate_usage(json.dumps(usage).encode("utf-8"))
        self.assertIn("duplicates a model session", str(refusal.exception))

    def test_distinct_id_less_sessions_keep_distinct_identities(self):
        with tempfile.TemporaryDirectory() as directory:
            first = self.export_id_less_session(pathlib.Path(directory) / "first.jsonl")
            second = self.export_id_less_session(pathlib.Path(directory) / "second.jsonl",
                                                 prompt="a genuinely different request")
        self.assertNotEqual(first["metadata"]["source_sha256"],
                            second["metadata"]["source_sha256"])
        self.assertNotEqual(first["session_ref"], second["session_ref"])

        module = load_runner_module()
        rows, repair_count, task_wall_time = module.validate_usage(
            json.dumps(self.phases_with(first, second)).encode("utf-8"))
        self.assertEqual(len(rows), 6)
        self.assertEqual(repair_count, 0)
        self.assertEqual(task_wall_time, 12.0)

    def test_wall_time_and_prompt_absence_are_limitations_not_values(self):
        records = [self.session_record(timestamp=None),
                   self.init_record(timestamp=None, systemPrompt=None, task=None),
                   self.assistant_record(timestamp=None)]
        row = self.row(records)
        self.assertIsNone(row["wall_time"])
        self.assertIn("wall_time_unavailable", row["limitations"])
        self.assertIn("system_prompt_absent", row["limitations"])
        self.assertIn("task_prompt_absent", row["limitations"])
        self.assertEqual(row["prompt_hashes"], [])
        row = self.row([self.session_record(), self.init_record(),
                        self.user_record(content=None), self.assistant_record()])
        self.assertIn("user_message_content_absent", row["limitations"])
        self.assertEqual([item["kind"] for item in row["prompt_hashes"]],
                         ["system_prompt", "task"])
        row = self.row([self.init_record(timestamp="2026-11-15T10:00:00Z"),
                        self.assistant_record(timestamp="2026-11-15T10:00:30Z")])
        self.assertEqual(row["wall_time"], 30.0)
        self.assertIn("session_entry_absent", row["limitations"])

    def test_repeated_session_init_uses_first_and_records_the_repetition(self):
        records = [self.session_record(), self.init_record(),
                   self.init_record(resolvedModel="other-model", systemPrompt="second prompt")]
        row = self.row(records)
        self.assertEqual(row["metadata"]["session_init_records"], 2)
        self.assertEqual(row["metadata"]["session_init"]["resolved_model"], "deepseek-v4-flash")
        self.assertIn("session_init_repeated", row["limitations"])
        self.assertEqual(row["prompt_hashes"], [
            {"kind": "system_prompt", "sha256": prompt_hash("synthetic system prompt")},
            {"kind": "task", "sha256": prompt_hash("synthetic task text")},
        ])

    def test_prompt_digests_are_deterministic_for_structured_content(self):
        content = [{"type": "text", "text": "structured request"}]
        canonical = json.dumps(content, sort_keys=True, separators=(",", ":"),
                               ensure_ascii=False).encode("utf-8")
        row = self.row([self.session_record(), self.user_record(content=content),
                        self.assistant_record()])
        self.assertEqual(row["prompt_hashes"][-1],
                         {"kind": "user_message", "index": 0,
                          "sha256": "sha256:" + hashlib.sha256(canonical).hexdigest()})

    def test_configured_provider_model_identity_ignores_only_known_thinking_suffix(self):
        def differs(resolved_model):
            records = [self.session_record(),
                       self.init_record(resolvedModel=resolved_model),
                       self.assistant_record()]
            return self.row(records)["metadata"]["served_model_differs_from_resolved_model"]

        self.assertFalse(differs("tencent-litellm/deepseek-v4.1-flash-fast:high"))
        self.assertFalse(differs("tencent-litellm/deepseek-v4.1-flash-fast"))
        self.assertTrue(differs("other-relay/deepseek-v4.1-flash-fast:high"))
        self.assertTrue(differs("tencent-litellm/other-model:high"))
        self.assertTrue(differs("tencent-litellm/deepseek-v4.1-flash-fast:unknown-effort"))
        row = self.row([
            self.session_record(),
            self.init_record(resolvedModel="tencent-litellm/deepseek-v4.1-flash-fast:high"),
            self.assistant_record(),
        ])
        self.assertEqual(row["metadata"]["session_init"]["resolved_model"],
                         "tencent-litellm/deepseek-v4.1-flash-fast:high")
        self.assertEqual(row["metadata"]["resolved_model_identity"],
                         {"provider": "tencent-litellm", "model": "deepseek-v4.1-flash-fast"})

    def test_custom_message_instructions_are_hashed_but_never_exported(self):
        content = CANARY + " parent follow-up instruction"
        records = [
            self.session_record(),
            self.init_record(),
            {"type": "custom_message", "timestamp": "2026-11-15T10:00:02Z",
             "customType": CANARY, "content": content, "display": CANARY,
             "details": {"secret": CANARY}, "attribution": CANARY,
             "id": "custom-1", "parentId": "custom-0",
             "runtime": {"secret": CANARY}},
            self.user_record("synthetic user request"),
            self.assistant_record(),
        ]
        completed = self.run_cli(records)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertNotIn(CANARY, completed.stdout)
        self.assertNotIn(CANARY, completed.stderr)
        row = json.loads(completed.stdout)
        self.assertEqual([item["kind"] for item in row["prompt_hashes"]],
                         ["system_prompt", "task", "custom_message", "user_message"])
        self.assertIn({"kind": "custom_message", "index": 0, "sha256": prompt_hash(content)},
                      row["prompt_hashes"])
        self.assertEqual(row["metadata"]["entry_types"]["custom_message"], 1)
        self.assertEqual(row["metadata"]["custom_messages"], 1)
        self.assertEqual(row["metadata"]["user_messages"], 1)
        self.assertEqual(row["metadata"]["assistant_messages"], 1)
        self.assertEqual(row["tokens"], {"input": 100, "output": 20, "cached": 7})

    def test_source_sha256_binds_the_exact_private_session_bytes(self):
        records = [
            self.session_record(),
            self.init_record(),
            {"type": "unrecognized_private_event", "timestamp": "2026-11-15T10:00:02Z",
             "payload": CANARY},
            self.assistant_record(),
        ]
        text = "\n".join(json.dumps(record) for record in records) + "\n"
        with tempfile.TemporaryDirectory() as directory:
            session = pathlib.Path(directory) / "session.jsonl"
            session.write_text(text, encoding="utf-8")
            raw = session.read_bytes()
            completed = self.run_arguments(["--session", str(session), "--stage", "review",
                                            "--credential-route", "safe-metadata",
                                            "--provenance", "synthetic"])
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertNotIn(CANARY, completed.stdout)
        row = json.loads(completed.stdout)
        self.assertEqual(row["metadata"]["source_sha256"],
                         "sha256:" + hashlib.sha256(raw).hexdigest())
        self.assertNotIn(str(session), completed.stdout)
        self.assertNotIn("session.jsonl", completed.stdout)

    def non_model_phase(self, stage):
        return {
            "stage": stage,
            "session_ref": None,
            "non_model_reason": "synthetic parser-regression phase without a model session",
            "model_ids": [],
            "prompt_hashes": [],
            "tokens": {"input": None, "output": None, "cached": None},
            "reported_cost": None,
            "estimated_cost": None,
            "cost_provenance": "not-applicable",
            "price_version": None,
            "wall_time": 1.0,
        }

    def test_secret_like_entry_type_names_are_redacted_and_still_valid(self):
        records = [
            self.session_record(),
            self.init_record(),
            self.user_record("request with secret-like record types"),
            self.assistant_record(),
            {"type": "token_usage", "timestamp": "2026-11-15T10:00:06Z", "input": 1},
            {"type": CANARY, "timestamp": "2026-11-15T10:00:07Z"},
        ]
        completed = self.run_cli(records)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertNotIn("token_usage", completed.stdout)
        self.assertNotIn(CANARY, completed.stdout)
        self.assertNotIn(CANARY, completed.stderr)
        row = json.loads(completed.stdout)

        def digest(name):
            return "sha256:" + hashlib.sha256(name.encode("utf-8", "surrogatepass")).hexdigest()

        self.assertEqual(row["metadata"]["entry_types"],
                         {digest(CANARY): 1, digest("token_usage"): 1,
                          "message": 2, "session": 1, "session_init": 1})
        self.assertIn("secret_like_entry_type_redacted", row["limitations"])

        # The runner refuses secret-like keys anywhere in a usage row; the exported
        # row must still pass its own validator unchanged.
        usage = {"repair_count": 0, "wall_time": 12.0, "phases": [
            self.non_model_phase("exploration"),
            row,
            self.non_model_phase("verification"),
            self.non_model_phase("review"),
            self.non_model_phase("repair"),
            self.non_model_phase("failed_attempt"),
        ]}
        module = load_runner_module()
        rows, repair_count, task_wall_time = module.validate_usage(
            json.dumps(usage).encode("utf-8"))
        self.assertEqual(len(rows), 6)
        self.assertEqual(repair_count, 0)
        self.assertEqual(task_wall_time, 12.0)

    def test_unrecognized_record_types_make_the_all_attempts_claim_false(self):
        records = [
            self.session_record(),
            self.init_record(),
            self.assistant_record(),
            {"type": "model_usage", "timestamp": "2026-11-15T10:00:06Z", "model": "m"},
        ]
        row = self.row(records)
        self.assertFalse(row["metadata"]["usage_includes_all_assistant_attempts"])
        self.assertIn("unrecognized_record_types", row["limitations"])
        self.assertEqual(row["metadata"]["entry_types"]["model_usage"], 1)
        complete = self.row([self.session_record(), self.init_record(), self.assistant_record()])
        self.assertTrue(complete["metadata"]["usage_includes_all_assistant_attempts"])

    def test_path_like_session_identifiers_are_hashed(self):
        def digest(value):
            return "sha256:" + hashlib.sha256(value.encode("utf-8", "surrogatepass")).hexdigest()

        agent = "C:/Users/alice/private-project/notes"
        resolved = "D:/models/weights-v3"
        records = [
            self.session_record(),
            self.init_record(agent=agent, resolvedModel=resolved),
            self.assistant_record(),
            {"type": "model_change", "timestamp": "2026-11-15T10:00:06Z",
             "model": "C:/models/x"},
        ]
        completed = self.run_cli(records)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        for value in (agent, resolved, "C:/models/x"):
            self.assertNotIn(value, completed.stdout)
            self.assertNotIn(value, completed.stderr)
        row = json.loads(completed.stdout)
        self.assertEqual(row["metadata"]["session_init"],
                         {"agent": digest(agent), "model_role": "lightweight",
                          "resolved_model": digest(resolved), "read_only": True})
        self.assertEqual(row["metadata"]["model_changes"],
                         [{"record": 3, "model": digest("C:/models/x")}])
        self.assertIsNone(row["metadata"]["resolved_model_identity"])
        self.assertIsNone(row["metadata"]["served_model_differs_from_resolved_model"])
        self.assertIn("session_init_identifier_redacted", row["limitations"])
        self.assertIn("model_identity_redacted", row["limitations"])

        provider_qualified = self.row([
            self.session_record(),
            self.init_record(resolvedModel="tencent-litellm/deepseek-v4.1-flash-fast:high"),
            self.assistant_record(),
        ])
        self.assertEqual(provider_qualified["metadata"]["session_init"]["resolved_model"],
                         "tencent-litellm/deepseek-v4.1-flash-fast:high")
        self.assertEqual(provider_qualified["metadata"]["resolved_model_identity"],
                         {"provider": "tencent-litellm", "model": "deepseek-v4.1-flash-fast"})
        self.assertNotIn("session_init_identifier_redacted", provider_qualified["limitations"])

        served_path = "E:/private/model-weights"
        completed = self.run_cli([
            self.session_record(),
            self.init_record(),
            self.assistant_record(model=served_path, provider="safe-provider", api="safe-api"),
        ])
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertNotIn(served_path, completed.stdout)
        row = json.loads(completed.stdout)
        self.assertEqual(row["model_ids"], [{
            "model": digest(served_path), "provider": "safe-provider",
            "api": "safe-api", "assistant_messages": 1}])
        self.assertIn("model_identity_redacted", row["limitations"])


if __name__ == "__main__":
    unittest.main()
