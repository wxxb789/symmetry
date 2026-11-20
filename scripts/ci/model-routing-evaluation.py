#!/usr/bin/env python3
"""Run one auditable acceptance evaluation against a frozen task and routes.

Operations:
  prepare  fresh isolated clone of --source at the frozen task base, materialized
           without host-dependent end-of-line conversion, then freeze the exact
           task and routes bytes in an external run directory; optional
           --environment-json records measured, non-secret execution-surface facts
           alongside the host environment in the run record, with every
           host-specific absolute path in a value recorded as a sha256: token
           instead of the raw path
  bind-dependencies
           record the resolved dependency tree the TypeScript gates execute from (a
           hoisted pnpm install outside every sealed checkout): verify that the
           checkout resolves each gate's cwd to exactly that tree (the ancestor
           `node_modules` walk Node performs past the empty placeholder prepare
           creates), refuse a tree no gate resolves from, copy the install log into
           the run store, bind its digest and a deterministic digest over every file
           and link of the tree (an in-tree link binds the tree-relative route it
           resolves to, so a junction hop cannot be retargeted unseen) in the
           environment block every receipt states
  seal     refuse a checkout whose Git index carries assume-unchanged,
           skip-worktree or other nonstandard entries, whose Git view carries
           checkout-local attribute/filter machinery, or whose object store
           carries replacement refs or grafts, then bind the checkout (actual
           base/head plus a deterministic manifest, refusing bound symlinks whose
           real target escapes the checkout or points under .git) into an
           evaluation Subject from a byte comparison between the object store and
           the worktree
  check    execute every frozen gate argv without a shell inside an owned process
           group (POSIX session / Windows process tree), terminate and reap that
           whole tree on timeout, apply the gate's frozen environment overrides,
           capture outputs, bind the Subject before and after, and require named
           Go tests through the existing assert-go-test-json.py event gate plus a
           derived `go test -list` run that proves the named package really lists
           them, and named ExUnit tests through a derived `mix test --trace` run
           that proves every named test finished exactly once without failures
           (and, for a named-vitest-test gate, a derived vitest JSON report run);
           the attempt records the dependency-provisioning step it executed under,
           so a tree bound after the attempt ran refuses at review, receipt and
           accept instead of being attested by an attempt that never saw it
  review   import the coordinator's independent review record for the sealed
           Subject and the latest sealed-subject check attempt; that attempt must
           be complete and passing, and a later failing or incomplete attempt
           supersedes an earlier pass
  receipt  derive the task receipt (accepted or explicitly nonaccepted) from the
           stored evidence, frozen inputs and the submitted phase usage rows
  accept   recompute the expected receipt from stored evidence and refuse any
           submitted row that differs, is not accepted, or fabricates claims

Trust boundary: the run directory is coordinator-owned evidence outside the
checkout. Re-digesting the stored bytes detects partial or accidental tampering
and refuses fabricated claims; it is not a security sandbox against a same-user
editor and never substitutes for cryptographic custody of the evidence store.

Manifest scope: a sealed Subject binds every checkout file and symlink except
.git internals, including gitignored files, so ignored source (for example a
foo.local.go that Go still compiles) cannot mutate without invalidating
evidence. Build outputs must stay outside the checkout or be cleaned before
seal; Elixir _build or other ignored artifact directories are deliberately not
exempted, and neither is tool run state: the manifest exempts nothing but .git
internals.

Tool run state: `vitest run` and `vite build` write caches into a project's own
`node_modules` directory (`node_modules/.vite`, `node_modules/.vitest-cache`) and
Vite writes its config-loader temp files next to the nearest existing
`node_modules` directory (`.vite-temp`). `check` therefore exports
SYMMETRY_TOOL_CACHE_DIR - resolved by `frontend/vite.config.ts` as Vite's
`cacheDir` and by `frontend/vitest.config.ts` as `cacheDir` plus vitest's
`fsModuleCachePath` - to every gate it executes, pointing at
`<run dir>/tool-state/<attempt>/<gate id>` in the coordinator-owned run store,
and `prepare` creates an empty `<run dir>/node_modules` so `.vite-temp` lands in
the run store too (an empty directory changes no module resolution: Node walks
past it to the hoisted dependency tree). Redirection alone is not the boundary:
the transformed-module cache would reuse a previous invocation's transformed
output for identical source bytes, keyed by a partly-hashed environment the seal
does not bind, and the results cache carries an earlier run's results (test
ordering and `--last-failed` selection), so a gate run also sets
SYMMETRY_DISABLE_TOOL_CACHES,
which `frontend/vitest.config.ts` reads as the test-level `cache: false` and
`fsModuleCache: false`, and each attempt gets a fresh state directory that
nothing from a previous attempt can be read from. A gate that still writes such
state inside the checkout fails closed: the sealed Subject changes, the attempt
fails and is preserved as failed evidence, and the receipt then refuses.
The worker's own runs need the redirect variable exported by the coordinator
(the runbook names it); they keep caches, and anything they leave in the
checkout is bound by the seal.

Change detection: the sealed change set is derived from bytes, never from Git's
filtered or cached view of them. seal refuses assume-unchanged, skip-worktree
and other nonstandard index entries; hashes base blobs straight from the object
store and worktree files straight from disk, so no clean/smudge filter,
attribute, index flag, stat cache or replacement ref can influence the
comparison; compares symlink targets by blob object id and file modes where the
host and Git track them; refuses any checkout-local
attribute or filter machinery outright even while the checkout matches; and
re-derives the changed path list whenever it re-verifies a sealed Subject.
Frozen object reads run with replacement refs disabled, and a checkout carrying
`refs/replace/*` or `.git/info/grafts` is refused outright, so no replacement or
graft can serve different bytes or ancestry under a frozen object id. Every bound
symlink must resolve inside the checkout and outside `.git`: the gates read the
real target bytes, so an escaping target is refused instead of being bound by
link text alone.
"""

from __future__ import annotations

import argparse
import fnmatch
import hashlib
import importlib.util
import json
import math
import os
import pathlib
import platform as host_platform
import re
import shutil
import signal
import stat
import subprocess
import sys
from typing import Any, Callable, Iterable

SCHEMA = "symmetry-model-routing-evaluation/v1"
SUBJECT_NAMESPACE = b"symmetry-acceptance-subject/v1"
STAGES = ("exploration", "implementation", "verification", "review", "repair", "failed_attempt")
SEVERITIES = ("critical", "major", "minor", "info")
FINDING_STATUSES = ("open", "resolved")
INCOMPLETE_CHECK_DETAIL = "not every frozen gate completed on the sealed subject"

MAX_TASK_BYTES = 1 << 20
MAX_ROUTES_BYTES = 1 << 16
MAX_RECORD_BYTES = 1 << 20
MAX_USAGE_BYTES = 8 << 20
MAX_RECEIPT_BYTES = 16 << 20
MAX_RESULT_BYTES = 16 << 20
MAX_CAPTURE_BYTES = 256 << 20
MAX_PROVISIONING_LOG_BYTES = 8 << 20
DEFAULT_TIMEOUT = 1800.0
PROCESS_TREE_REAP_SECONDS = 10.0
GO_LIST_FAILURE_PREFIX = "the package test binary did not list the named Go tests: "
EXUNIT_FAILURE_PREFIX = "the traced ExUnit run did not execute every named test: "
VITEST_FAILURE_PREFIX = "the traced vitest run did not report every named test as passed: "
SOURCE_CHANGED_SUFFIX = "source changed while executing this gate"
# Tool run state: the module docstring's tool-run-state paragraph records the
# redirect these two variables carry, which config resolves which, and why
# redirection alone is not the boundary. A gate's tool state is fresh per attempt
# (nothing a previous attempt cached can be executed), and worker runs keep the
# redirect alone: the coordinator seals what the worker leaves behind.
TOOL_CACHE_ENV = "SYMMETRY_TOOL_CACHE_DIR"
TOOL_CACHE_DISABLE_ENV = "SYMMETRY_DISABLE_TOOL_CACHES"
TOOL_CACHE_DISABLED = "1"
TOOL_STATE_DIRNAME = "tool-state"
DEPENDENCY_TREE_NAMESPACE = b"symmetry-dependency-tree/v1"
PROVISIONING_LOG_PATH = "provisioning/install.log"

COMMIT_PATTERN = re.compile(r"^[0-9a-f]{40}$")
IDENTIFIER_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$")
ATTEMPT_PATTERN = re.compile(r"^attempt-[0-9]{4,}$")
DIGEST_PATTERN = re.compile(r"^sha256:[0-9a-f]{64}$")
SECRET_KEY_PATTERN = re.compile(
    r"(^|[^a-z0-9])(api[_-]?key|apikey|secret|password|passwd|token|bearer"
    r"|private[_-]?key|access[_-]?key)([^a-z0-9]|$)",
    re.IGNORECASE,
)
GATE_ENV_NAME_PATTERN = re.compile(r"^[A-Z][A-Z0-9_]{1,63}$")
MEASURED_NAME_PATTERN = re.compile(r"^[a-z][a-z0-9_]{0,39}$")
# Elixir 1.20.4 rewrites one trace line per finished test, preceded by a carriage
# return; ANSI escapes wrap the description on color terminals. The measured shape is
# `  * test <description> (<n>.<d>ms) [L#<line>]` for a finished test and
# `  * test <description> (skipped|excluded) [L#<line>]` otherwise. The matcher
# compares the rendered description as a whole, so a test declared inside a `describe`
# block - rendered as `<describe> <name>` - matches a frozen name only when it spells
# that name in full; declaring tests at module level is the frozen convention, not a
# parser rule.
EXUNIT_ANSI_PATTERN = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")
EXUNIT_OBSERVATION_PATTERN = re.compile(
    r"^  \* test (?P<name>.+?) \((?P<descriptor>[^()]*)\) \[L#[0-9]+\]$"
)
EXUNIT_FINISHED_PATTERN = re.compile(r"^[0-9]+(?:\.[0-9]+)?ms$")
# `mix test` owns the summary, not ExUnit: the measured shapes are `Result: 3 passed`,
# `Result: 2 passed, 1 skipped`, `Result: 1/2 passed` (with a following `Failed: 1 test`)
# and `Result: 1 passed, 10 excluded`. Only the `N/M passed` form carries the failure
# count directly; the restated `Failed:` line must not be counted twice.
EXUNIT_RESULT_PATTERN = re.compile(
    r"^\s*Result:\s*(?P<passed>[0-9]+)(?:/(?P<total>[0-9]+))?\s+passed\b(?P<tail>[^\r\n]*)",
    re.MULTILINE,
)
EXUNIT_FAILED_LINE_PATTERN = re.compile(r"^\s*Failed:\s*([0-9]+)\s+tests?\b", re.MULTILINE)
EXUNIT_TAIL_FAILED_PATTERN = re.compile(r"([0-9]+)\s+failed\b")

TEST_SEGMENTS = {"test", "tests", "__tests__"}
TEST_FILE_PATTERN = re.compile(
    r"(_test\.go$|^test_.*\.py$|_test\.py$|^test_.*\.exs$|_test\.exs$"
    r"|\.test\.[^/]+$|\.spec\.[^/]+$)"
)
PROTECTED_PREFIXES = (".github/", "contracts/", ".agents/skills/")
PROTECTED_BASENAMES = {"AGENTS.md", "CLAUDE.md"}

TASK_KEYS = {
    "task_id", "base", "language", "risk_class", "workflow", "outcome",
    "explanations", "allowed_paths", "protected_paths", "gates",
}
GATE_KEYS = {
    "id", "cwd", "argv", "go_tests", "go_package", "exunit_tests", "exunit_files",
    "vitest_tests", "vitest_files", "env",
}
REQUIRED_GATE_KEYS = {"id", "cwd", "argv"}
ROUTES_KEYS = {"worker", "reviewer"}
ROLE_KEYS = {"model_id", "provenance", "credential_route", "authorization_ref"}
RUN_KEYS = {
    "schema", "task_id", "base", "source", "head", "base_tree_digest",
    "task_sha256", "routes_sha256", "environment", "review_sha256", "usage_sha256",
}
REQUIRED_RUN_KEYS = {
    "schema", "task_id", "base", "source", "head", "base_tree_digest",
    "task_sha256", "routes_sha256", "environment",
}
ENVIRONMENT_KEYS = {"platform", "machine", "python_version", "git_version"}
PROVISIONING_KEYS = {
    "tree", "tree_digest", "tree_entries", "install_log", "install_log_source",
    "install_log_sha256", "install_log_bytes",
}
SEALED_KEYS = {
    "schema", "task_id", "base", "head", "manifest_digest", "content_revision",
    "subject_hash", "file_count", "changed_paths",
}
CHANGE_KEYS = {"path", "status"}
REVIEW_KEYS = {
    "subject_hash", "routes_sha256", "check_attempt_id", "check_evidence_sha256",
    "reviewer", "worker", "verdict", "gate_weakening", "findings", "explanations",
}
REVIEWER_KEYS = {"model_id", "session_ref"}
WORKER_KEYS = {"model_id", "session_refs"}
FINDING_KEYS = {"id", "severity", "status", "description"}
USAGE_KEYS = {"phases", "repair_count", "wall_time"}
PHASE_KEYS = {
    "stage", "session_ref", "non_model_reason", "model_ids", "prompt_hashes",
    "tokens", "reported_cost", "estimated_cost", "cost_provenance",
    "price_version", "wall_time",
}
TOKEN_KEYS = {"input", "output", "cached"}
# The extractor in model-routing-omp-usage.py emits these per-field observed
# subtotals for partial sessions; keep both lists in sync with its TOKEN_FIELDS,
# COST_FIELDS and assistant-message counters.
KNOWN_SUBTOTAL_MESSAGE_KEYS = (
    "assistant_messages", "assistant_messages_with_usage", "assistant_messages_with_cost_total",
)
KNOWN_SUBTOTAL_TOKEN_FIELDS = (
    "input", "output", "cacheRead", "cacheWrite", "cached", "totalTokens", "reasoningTokens",
)
KNOWN_SUBTOTAL_COST_FIELDS = ("input", "output", "cacheRead", "cacheWrite", "total")
KNOWN_SUBTOTAL_ENTRY_KEYS = {"sum", "messages"}
PHASE_OPTIONAL_KEYS = {"metadata", "limitations", "known_subtotals"}
MODEL_ID_KEYS = {"model", "provider", "api", "assistant_messages"}
PROMPT_HASH_KEYS = {"kind", "sha256", "index"}
PROMPT_HASH_REQUIRED_KEYS = {"kind", "sha256"}
SHA256_FIELD_PATTERN = re.compile(r"^(sha256:)?[0-9a-f]{64}$")
ATTEMPT_KEYS = {
    "schema", "attempt_id", "task_id", "task_sha256", "routes_sha256",
    "subject_hash", "complete", "passed", "gates", "dependency_provisioning",
}
# The provisioning view is optional on read so an attempt written before this member
# existed still parses (its `None` then refuses any run that binds a later tree).
REQUIRED_ATTEMPT_KEYS = ATTEMPT_KEYS - {"dependency_provisioning"}
ATTEMPT_ENTRY_KEYS = {
    "id", "passed", "result_sha256", "stdout_sha256", "stderr_sha256",
    "validator_exit_code", "validator_stdout_sha256", "validator_stderr_sha256",
}
RESULT_KEYS = {
    "gate_id", "cwd", "argv", "go_tests", "go_package", "exit_code", "timed_out",
    "error", "stdout_sha256", "stderr_sha256", "stdout_bytes", "stderr_bytes",
    "tool_state_dir",
    "go_list_argv", "go_list_exit_code", "go_list_stdout_sha256", "go_list_stderr_sha256",
    "go_list_stdout_bytes", "go_list_stderr_bytes",
    "env", "exunit_tests", "exunit_files", "exunit_argv", "exunit_exit_code",
    "exunit_timed_out", "exunit_stdout_sha256", "exunit_stderr_sha256",
    "exunit_stdout_bytes", "exunit_stderr_bytes", "exunit_error",
    "vitest_tests", "vitest_files", "vitest_argv", "vitest_exit_code",
    "vitest_timed_out", "vitest_stdout_sha256", "vitest_stderr_sha256",
    "vitest_stdout_bytes", "vitest_stderr_bytes", "vitest_report_sha256",
    "vitest_report_bytes", "vitest_error",
    "validator", "subject_hash_before", "subject_hash_after", "passed",
}
VALIDATOR_KEYS = {"argv", "exit_code", "stdout_sha256", "stderr_sha256", "stdout_bytes", "stderr_bytes"}

VALIDATOR_SCRIPT = pathlib.Path(__file__).with_name("assert-go-test-json.py").resolve()
SOURCE_SUBJECT_SCRIPT = pathlib.Path(__file__).with_name("linux-v2-source-subject.py").resolve()


class Refused(Exception):
    """A refusal that must never be recorded as accepted evidence."""


# Recorded measured-surface values must not republish the operator profile or a
# disposable evaluation root, and the root-only record check must agree on what a
# host path is: both tools use the one definition `model-routing-record-sanitize.py`
# exports as `HOST_PATH_PATTERNS` (Windows drive, UNC and forward-slash authority
# paths, the POSIX home/root/temp/mount roots, `file:` URLs and their JSON-escaped
# spellings). Paths outside those roots stay readable; the token is `sha256:` here and
# `path-sha256:` in the renderer, each tool's own recorded shape. The definition loads
# lazily so a command that never redacts a value does not need the sibling file.
SANITIZE_SCRIPT = pathlib.Path(__file__).with_name("model-routing-record-sanitize.py")
_HOST_PATH_PATTERNS: tuple[re.Pattern[str], ...] | None = None


def host_path_patterns() -> tuple[re.Pattern[str], ...]:
    """The shared host-path matchers, read from the sanitize tool on first use.

    A sibling that cannot be read, parsed or executed refuses cleanly - an
    unreadable file, a syntax error, a regex literal that fails to compile
    (`re.error`) and a module body that raises (`AttributeError` included) are all
    the structured refusal, never an uncaught traceback - and so does a sibling
    that parses without defining `HOST_PATH_PATTERNS`.
    """
    global _HOST_PATH_PATTERNS
    if _HOST_PATH_PATTERNS is None:
        spec = importlib.util.spec_from_file_location("model_routing_record_sanitize", SANITIZE_SCRIPT)
        if spec is None or spec.loader is None:
            raise Refused("cannot load model-routing-record-sanitize.py for the shared host-path patterns")
        module = importlib.util.module_from_spec(spec)
        try:
            spec.loader.exec_module(module)
        except (OSError, SyntaxError, re.error, AttributeError) as error:
            raise Refused(f"cannot load model-routing-record-sanitize.py for the shared host-path patterns: {error}")
        patterns = getattr(module, "HOST_PATH_PATTERNS", None)
        if patterns is None:
            raise Refused(
                "cannot load model-routing-record-sanitize.py for the shared host-path patterns: "
                "it defines no HOST_PATH_PATTERNS"
            )
        _HOST_PATH_PATTERNS = patterns
    return _HOST_PATH_PATTERNS


# ---------------------------------------------------------------------------
# Strict JSON and scalar validation


def sha256_hex(raw: bytes) -> str:
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def strict_json(raw: bytes, source: str, limit: int) -> Any:
    if len(raw) > limit:
        raise Refused(f"{source} exceeds {limit} bytes")
    try:
        text = raw.decode("utf-8", errors="strict")
    except UnicodeDecodeError:
        raise Refused(f"{source} is not valid UTF-8")

    def object_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        value: dict[str, Any] = {}
        for key, item in pairs:
            if key in value:
                raise Refused(f"{source} repeats the JSON key {key!r}")
            value[key] = item
        return value

    def parse_number(text_value: str) -> float:
        value = float(text_value)
        if not math.isfinite(value):
            raise Refused(f"{source} contains a nonfinite number")
        return value

    def parse_constant(name: str) -> float:
        raise Refused(f"{source} contains the non-JSON constant {name}")

    try:
        return json.loads(
            text,
            object_pairs_hook=object_pairs,
            parse_float=parse_number,
            parse_constant=parse_constant,
        )
    except json.JSONDecodeError as error:
        raise Refused(f"{source} is not valid JSON: {error}")
    except ValueError:
        # `json.loads` refuses an integer literal past its conversion-digit limit
        # with a plain ValueError; that is a refusal about the document's numbers,
        # not a crash.
        raise Refused(f"{source} carries an integer outside the supported range")
    except RecursionError:
        raise Refused(f"{source} nests too deeply")


def contains_non_finite(value: Any) -> bool:
    """Whether one serializable structure holds a nan or infinity float anywhere.

    The classifier gives a non-finite value its own refusal message and keeps the
    encoder's `ValueError` from being read as one: `allow_nan=False` reports a
    nan or inf float and a finite integer past Python's conversion-digit limit
    through the same exception class.
    """

    if isinstance(value, float):
        return not math.isfinite(value)
    if isinstance(value, dict):
        return any(contains_non_finite(item) for item in value.values())
    if isinstance(value, list):
        return any(contains_non_finite(item) for item in value)
    return False


def canonical_json(value: Any) -> bytes:
    try:
        if contains_non_finite(value):
            raise Refused("value contains a non-finite number")
        text = json.dumps(value, indent=2, sort_keys=True, ensure_ascii=False, allow_nan=False)
    except RecursionError:
        raise Refused("value nests too deeply to serialize")
    except UnicodeEncodeError:
        raise Refused("value contains text that is not valid UTF-8")
    except ValueError:
        # `allow_nan=False` would report a nan or inf float here, and every one was
        # checked above; what remains is a value Python cannot serialize - an integer
        # past its conversion-digit limit - which must not be called non-finite.
        raise Refused("value contains a number outside the serializable range")
    try:
        return text.encode("utf-8") + b"\n"
    except UnicodeEncodeError:
        raise Refused("value contains text that is not valid UTF-8")


def read_bytes(path: pathlib.Path, limit: int, source: str) -> bytes:
    try:
        with open(path, "rb") as stream:
            raw = stream.read(limit + 1)
    except OSError as error:
        raise Refused(f"cannot read {source} at {path}: {error}")
    if len(raw) > limit:
        raise Refused(f"{source} at {path} exceeds {limit} bytes")
    return raw


def write_bytes(path: pathlib.Path, raw: bytes) -> None:
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "wb") as stream:
            stream.write(raw)
    except OSError as error:
        raise Refused(f"cannot write {path}: {error}")


def write_json(path: pathlib.Path, value: Any) -> bytes:
    raw = canonical_json(value)
    write_bytes(path, raw)
    return raw


def scan_no_secret_keys(value: Any, source: str, path: str = "$") -> None:
    if isinstance(value, dict):
        for key, item in value.items():
            if SECRET_KEY_PATTERN.search(key):
                raise Refused(
                    f"{source} contains a secret-like key at {path}.{key}; "
                    "evaluation metadata records routes and provenance, never secrets"
                )
            scan_no_secret_keys(item, source, f"{path}.{key}")
    elif isinstance(value, list):
        for index, item in enumerate(value):
            scan_no_secret_keys(item, source, f"{path}[{index}]")


def require_object(value: Any, source: str, allowed: set[str], required: set[str]) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise Refused(f"{source} must be a JSON object")
    unexpected = sorted(set(value) - allowed)
    if unexpected:
        raise Refused(f"{source} has unexpected keys: {', '.join(unexpected)}")
    missing = sorted(required - set(value))
    if missing:
        raise Refused(f"{source} is missing keys: {', '.join(missing)}")
    return value


def require_text(value: Any, source: str, max_length: int = 4096) -> str:
    if not isinstance(value, str) or not value:
        raise Refused(f"{source} must be a non-empty string")
    if len(value) > max_length:
        raise Refused(f"{source} exceeds {max_length} characters")
    return value


def require_optional_text(value: Any, source: str, max_length: int = 4096) -> str | None:
    if value is None:
        return None
    return require_text(value, source, max_length)


def require_identifier(value: Any, source: str) -> str:
    text = require_text(value, source, 128)
    if not IDENTIFIER_PATTERN.fullmatch(text):
        raise Refused(f"{source} must match {IDENTIFIER_PATTERN.pattern}")
    return text


def require_commit(value: Any, source: str) -> str:
    if not isinstance(value, str) or not COMMIT_PATTERN.fullmatch(value):
        raise Refused(f"{source} must be a 40-hex commit id")
    return value


def require_digest(value: Any, source: str) -> str:
    if not isinstance(value, str) or not DIGEST_PATTERN.fullmatch(value):
        raise Refused(f"{source} must be a sha256:<64 hex> digest")
    return value


def require_optional_digest(value: Any, source: str) -> str | None:
    if value is None:
        return None
    return require_digest(value, source)


def require_bool(value: Any, source: str) -> bool:
    if not isinstance(value, bool):
        raise Refused(f"{source} must be a JSON boolean")
    return value


def require_integer(value: Any, source: str, minimum: int = 0) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < minimum:
        raise Refused(f"{source} must be an integer >= {minimum}")
    return value


def require_optional_integer(value: Any, source: str, minimum: int = 0) -> int | None:
    if value is None:
        return None
    return require_integer(value, source, minimum)


def require_number(value: Any, source: str, minimum: float = 0.0) -> float | int:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise Refused(f"{source} must be a number >= {minimum}")
    try:
        number = float(value)
    except OverflowError:
        # A JSON integer beyond float range must refuse as a value: converting it for
        # the finiteness check must not escape validation as an uncaught traceback.
        raise Refused(f"{source} must be a finite number >= {minimum}")
    if not math.isfinite(number) or number < minimum:
        raise Refused(f"{source} must be a finite number >= {minimum}")
    return value


def require_optional_number(value: Any, source: str, minimum: float = 0.0) -> float | int | None:
    if value is None:
        return None
    return require_number(value, source, minimum)


def number_sum(values: Iterable[int | float], source: str) -> int | float:
    """Sum validated numbers, refusing an int/float mix beyond the float range.

    Every addend passed a finite-number check on its own, but Python converts an int
    to float when it meets a float total, and an int or int total beyond float range
    raises OverflowError there; it must refuse as a value instead of escaping as an
    uncaught traceback. The built-in `sum` is the summation: its own semantics (the
    compensated float path on this interpreter) are what every derived total has
    always been computed with, and only its OverflowError becomes a refusal. A float
    total that reaches infinity is refused later by canonical_json when the receipt
    is serialized.
    """
    try:
        return sum(values)
    except OverflowError:
        raise Refused(f"{source} exceeds the finite number range")


def require_value_in(value: Any, source: str, allowed: Iterable[str]) -> str:
    options = tuple(allowed)
    if not isinstance(value, str) or value not in options:
        raise Refused(f"{source} must be one of: {', '.join(options)}")
    return value


def require_text_list(
    value: Any, source: str, minimum: int, unique: bool, max_length: int = 4096
) -> list[str]:
    if not isinstance(value, list) or len(value) < minimum:
        raise Refused(f"{source} must be a list with at least {minimum} entries")
    items = [require_text(item, f"{source} entry", max_length) for item in value]
    if unique and len(set(items)) != len(items):
        raise Refused(f"{source} entries must be unique")
    return items


def require_string_mapping(value: Any, source: str) -> dict[str, str]:
    if not isinstance(value, dict):
        raise Refused(f"{source} must be a JSON object")
    result: dict[str, str] = {}
    for key, item in value.items():
        if not isinstance(key, str) or not key or not isinstance(item, str):
            raise Refused(f"{source} must map non-empty strings to strings")
        result[key] = item
    return result


# ---------------------------------------------------------------------------
# Relative paths, globs and protected assets


def require_relative_path(value: Any, source: str, allow_glob: bool) -> str:
    text = require_text(value, source, 512)
    if "\\" in text:
        raise Refused(f"{source} must use forward slashes: {text!r}")
    if text.startswith("/") or re.match(r"^[A-Za-z]:", text):
        raise Refused(f"{source} must be relative: {text!r}")
    if any(part in ("", ".", "..") for part in text.split("/")):
        raise Refused(f"{source} must not contain empty, '.' or '..' segments: {text!r}")
    if not allow_glob and any(character in text for character in "*?[]"):
        raise Refused(f"{source} must be an exact path without glob characters: {text!r}")
    return text


def require_path_list(value: Any, source: str, allow_glob: bool) -> list[str]:
    if not isinstance(value, list):
        raise Refused(f"{source} must be a list of relative paths")
    paths = [require_relative_path(item, f"{source} entry", allow_glob) for item in value]
    if len(set(paths)) != len(paths):
        raise Refused(f"{source} entries must be unique")
    return paths


def segments_match(pattern: tuple[str, ...], parts: tuple[str, ...]) -> bool:
    if not pattern:
        return not parts
    head = pattern[0]
    if head == "**":
        return any(segments_match(pattern[1:], parts[index:]) for index in range(len(parts) + 1))
    if not parts:
        return False
    if not fnmatch.fnmatchcase(parts[0], head):
        return False
    return segments_match(pattern[1:], parts[1:])


class PathMatcher:
    """Match gitignore-style patterns against forward-slash relative paths.

    A pattern is matched segment by segment: an exact segment matches itself and
    each segment supports the ``*``/``?``/``[..]`` fnmatch wildcards, while ``**``
    matches any number of segments at any depth (including none). A pattern that
    contains a ``/`` is anchored at the checkout root; a pattern without a ``/``
    matches the basename at any depth, so ``notes.md`` matches both ``notes.md``
    and ``docs/notes.md``.
    """

    def __init__(self, patterns: Iterable[str]):
        self.patterns = []
        for pattern in patterns:
            parts = tuple(pattern.split("/"))
            if "/" not in pattern:
                parts = ("**",) + parts
            self.patterns.append(parts)

    def matches(self, path: str) -> bool:
        parts = tuple(path.split("/"))
        return any(segments_match(pattern, parts) for pattern in self.patterns)


def is_test_path(path: str) -> bool:
    parts = path.split("/")
    if any(segment in TEST_SEGMENTS for segment in parts[:-1]):
        return True
    return bool(TEST_FILE_PATTERN.search(parts[-1]))


def is_protected_asset(path: str) -> bool:
    if any(path.startswith(prefix) for prefix in PROTECTED_PREFIXES):
        return True
    return path.rsplit("/", 1)[-1] in PROTECTED_BASENAMES


# ---------------------------------------------------------------------------
# Git and source binding


def git_output(repository: str, *arguments: str) -> bytes:
    # Replacement refs stay disabled for every read: a checkout can carry
    # refs/replace that would otherwise serve different bytes under a frozen object
    # id, and refuse_object_substitution refuses such a checkout outright.
    command = ["git", "--no-replace-objects", "-c", f"safe.directory={repository}", "-C", repository, *arguments]
    try:
        completed = subprocess.run(command, capture_output=True, check=False)
    except OSError as error:
        raise Refused(f"could not run git: {error}")
    if completed.returncode != 0:
        detail = completed.stderr.decode("utf-8", "replace").strip()
        raise Refused(f"git {' '.join(arguments)} failed ({completed.returncode}): {detail}")
    return completed.stdout


def git_text(repository: str, *arguments: str) -> str:
    try:
        return git_output(repository, *arguments).decode("utf-8", errors="strict").strip()
    except UnicodeDecodeError:
        raise Refused(f"git {' '.join(arguments)} did not return UTF-8 text")


def decode_path(raw: bytes) -> str:
    try:
        return raw.decode("utf-8", errors="strict")
    except UnicodeDecodeError:
        raise Refused("the checkout contains a path that is not valid UTF-8")


def utf8_path_key(path: str) -> bytes:
    """Byte sort key for a checkout path; refuse paths that are not valid UTF-8."""
    try:
        return path.encode("utf-8")
    except UnicodeEncodeError:
        raise Refused("the checkout contains a path that is not valid UTF-8")


def executable_bits_tracked(checkout: pathlib.Path) -> bool:
    """Whether this host and checkout can represent Git's executable bit.

    When they cannot, Git itself ignores mode-only differences, and so does the seal:
    reporting a pristine executable file as modified would be a false positive.
    """
    if os.name != "posix":
        return False
    command = ["git", "-c", f"safe.directory={checkout}", "-C", str(checkout), "config", "--bool", "core.filemode"]
    try:
        completed = subprocess.run(command, capture_output=True, check=False)
    except OSError as error:
        raise Refused(f"could not read git config core.filemode: {error}")
    if completed.returncode != 0:
        return True  # git's own default for core.filemode is true
    return completed.stdout.decode("utf-8", "replace").strip() != "false"


def iter_source_paths(checkout: pathlib.Path) -> list[str]:
    """Every checkout file and symlink; only .git internals are excluded.

    Tool run state is not exempt: `frontend/vite.config.ts` and
    `frontend/vitest.config.ts` resolve SYMMETRY_TOOL_CACHE_DIR (exported by
    `check` for every gate) so vitest/vite caches and Vite's config-loader temp
    files land in the run store, and a gate that still writes such state into the
    checkout changes the sealed revision and fails closed.
    """
    paths: list[str] = []
    for root, dirnames, filenames in os.walk(checkout):
        for dirname in list(dirnames):
            if dirname == ".git":
                dirnames.remove(dirname)
                continue
            candidate = pathlib.Path(root, dirname)
            if candidate.is_symlink():
                dirnames.remove(dirname)
                paths.append(candidate.relative_to(checkout).as_posix())
                continue
            try:
                info = candidate.lstat()
            except OSError as error:
                raise Refused(f"cannot read source entry {candidate.relative_to(checkout).as_posix()}: {error}")
            if getattr(info, "st_reparse_tag", 0):
                raise Refused(
                    "unsupported reparse-point directory in the checkout: "
                    + candidate.relative_to(checkout).as_posix()
                )
        for name in filenames:
            if name == ".git":
                continue
            paths.append(pathlib.Path(root, name).relative_to(checkout).as_posix())
    return sorted(paths, key=utf8_path_key)


def refuse_escaping_symlink(checkout: pathlib.Path, path: str, raw_target: bytes) -> None:
    """Refuse a bound symlink whose real target leaves the manifest scope.

    The manifest binds the link text, but every gate reads the bytes behind the
    link. A target that leaves the checkout, or enters `.git` (which the manifest
    excludes), would let a worker change what runs without changing any bound byte,
    so such a link is refused instead of being trusted. In-tree targets stay
    legitimate: each hop and the final target are enumerated and bound as their own
    manifest entries.
    """
    try:
        resolved = os.path.realpath(pathlib.Path(checkout, *path.split("/")))
    except (OSError, RuntimeError) as error:
        raise Refused(f"cannot resolve the bound symlink {path}: {error}")
    try:
        relative = os.path.relpath(resolved, os.path.realpath(checkout))
    except ValueError:  # a Windows target on another drive is never in-tree
        relative = os.pardir
    display = raw_target.decode("utf-8", "replace")[:200]
    if relative == os.pardir or relative.startswith(os.pardir + os.sep):
        raise Refused(
            f"the bound symlink {path} escapes the checkout; the gates read its real target:"
            f" -> {display!r}"
        )
    if ".git" in pathlib.PurePath(relative).parts:
        raise Refused(
            f"the bound symlink {path} points into .git, which the Source manifest excludes:"
            f" -> {display!r}"
        )


def read_worktree_entries(checkout: pathlib.Path) -> dict[str, tuple[str, str, str]]:
    """Bind bytes and modes of every checkout file, including ignored files."""
    executable_bits = executable_bits_tracked(checkout)
    entries: dict[str, tuple[str, str, str]] = {}
    for path in iter_source_paths(checkout):
        target = pathlib.Path(checkout, *path.split("/"))
        try:
            info = target.lstat()
        except OSError as error:
            raise Refused(f"cannot read source entry {path}: {error}")
        if stat.S_ISLNK(info.st_mode):
            mode, kind = "120000", "symlink"
            try:
                data = os.fsencode(os.readlink(target))
            except OSError as error:
                raise Refused(f"cannot read symlink {path}: {error}")
            refuse_escaping_symlink(checkout, path, data)
            digest = hashlib.sha256(data)
        elif stat.S_ISREG(info.st_mode):
            kind = "file"
            mode = "100755" if executable_bits and info.st_mode & 0o111 else "100644"
            digest = hashlib.sha256()
            try:
                with open(target, "rb") as stream:
                    while True:
                        chunk = stream.read(1 << 20)
                        if not chunk:
                            break
                        digest.update(chunk)
            except OSError as error:
                raise Refused(f"cannot read source entry {path}: {error}")
        else:
            raise Refused(f"unsupported source entry (not a regular file or symlink): {path}")
        entries[path] = (mode, kind, "sha256:" + digest.hexdigest())
    if set(iter_source_paths(checkout)) != set(entries):
        raise Refused("source changed while binding its manifest")
    return entries


def manifest_digest(entries: dict[str, tuple[str, str, str]]) -> tuple[str, int]:
    hasher = hashlib.sha256()
    for path in sorted(entries, key=utf8_path_key):
        mode, kind, digest = entries[path]
        hasher.update(mode.encode("ascii") + b"\0" + kind.encode("ascii") + b"\0")
        hasher.update(utf8_path_key(path) + b"\0" + digest.encode("ascii") + b"\0")
    return "sha256:" + hasher.hexdigest(), len(entries)


def read_base_tree(checkout: pathlib.Path, base: str) -> dict[str, tuple[str, str]]:
    """Base commit blob entries: path -> (mode, object id)."""
    entries: dict[str, tuple[str, str]] = {}
    for record in git_output(str(checkout), "ls-tree", "--full-tree", "-r", "-z", base).split(b"\0"):
        if not record:
            continue
        header, path_bytes = record.split(b"\t", 1)
        fields = header.split(b" ", 2)
        if len(fields) != 3:
            raise Refused("unexpected git ls-tree output")
        mode, kind, object_id = fields
        path = decode_path(path_bytes)
        if kind != b"blob":
            raise Refused(f"the base tree contains a non-blob or submodule entry: {path}")
        try:
            mode_text = mode.decode("ascii")
            object_text = object_id.decode("ascii")
        except UnicodeDecodeError:
            raise Refused(f"unexpected git ls-tree output for {path}")
        if not re.fullmatch(r"[0-7]{6}", mode_text) or not re.fullmatch(r"[0-9a-f]{40,64}", object_text):
            raise Refused(f"unexpected git ls-tree output for {path}")
        entries[path] = (mode_text, object_text)
    return entries


def list_base_paths(checkout: pathlib.Path, base: str) -> set[str]:
    return set(read_base_tree(checkout, base))


def refuse_index_hacks(checkout: pathlib.Path) -> None:
    """Refuse index entries whose flags can hide worktree content from git diff."""
    suspect: list[str] = []
    for record in git_output(str(checkout), "ls-files", "-v", "-z").split(b"\0"):
        if not record:
            continue
        if record[:1] == b"H":
            continue
        suspect.append(decode_path(record[2:] if record[1:2] == b" " else record[1:]))
    if suspect:
        raise Refused(
            "seal refuses a checkout whose Git index carries assume-unchanged, skip-worktree or other "
            "nonstandard entries; re-create the isolated checkout: " + ", ".join(suspect[:5])
        )


def refuse_object_substitution(checkout: pathlib.Path) -> None:
    """Refuse a checkout whose Git object view is rewritten.

    A replacement ref serves different bytes under a frozen object id, and a
    `.git/info/grafts` file rewrites commit ancestry; either mechanism invalidates
    the byte and object-id binding the seal derives, even while the worktree still
    matches. Object reads additionally run with replacement refs disabled, so this
    check is the fail-closed boundary rather than the only defense.
    """
    replacements = git_output(str(checkout), "for-each-ref", "refs/replace").decode("utf-8", "replace").strip()
    if replacements:
        names = sorted(line.split("\t", 1)[1] for line in replacements.splitlines() if "\t" in line)
        raise Refused(
            "the checkout carries Git replacement refs, which substitute object bytes under frozen "
            "ids; re-create the isolated checkout from an unrewritten source: " + ", ".join(names[:5])
        )
    resolved = git_output(str(checkout), "rev-parse", "--git-path", "info/grafts").decode("utf-8", "replace").strip()
    grafts = pathlib.Path(resolved)
    if not grafts.is_absolute():
        grafts = checkout / grafts
    if grafts.exists():
        raise Refused(
            "the checkout rewrites Git ancestry with .git/info/grafts; remove it and re-create the "
            "isolated checkout"
        )


def read_base_blob_digests(checkout: pathlib.Path, objects: Iterable[str]) -> dict[str, str]:
    """Hash base blob bytes straight out of the object store.

    Object-store content is what the base commit stores: no clean/smudge filter,
    attribute, index flag, stat cache or replacement ref can influence it. The whole
    request and its output are held in memory, which is bounded by the base tree size.
    """
    ordered = sorted(set(objects))
    if not ordered:
        return {}
    request = "".join(object_id + "\n" for object_id in ordered).encode("ascii")
    command = [
        "git", "--no-replace-objects", "-c", f"safe.directory={checkout}", "-C", str(checkout),
        "cat-file", "--batch",
    ]
    try:
        completed = subprocess.run(command, input=request, capture_output=True, check=False)
    except OSError as error:
        raise Refused(f"could not run git cat-file: {error}")
    if completed.returncode != 0:
        detail = completed.stderr.decode("utf-8", "replace").strip()
        raise Refused(f"git cat-file --batch failed ({completed.returncode}): {detail}")
    payload = completed.stdout
    digests: dict[str, str] = {}
    offset = 0
    for object_id in ordered:
        newline = payload.find(b"\n", offset)
        if newline < 0:
            raise Refused("git cat-file ended before every base blob was read")
        header = payload[offset:newline].split(b" ")
        if len(header) != 3 or header[0] != object_id.encode("ascii") or header[1] != b"blob":
            raise Refused(f"unexpected git cat-file output for the base blob {object_id}")
        try:
            size = int(header[2])
        except ValueError:
            raise Refused(f"unexpected git cat-file size for the base blob {object_id}")
        start = newline + 1
        end = start + size
        if size < 0 or end >= len(payload) or payload[end] != 0x0A:
            raise Refused(f"git cat-file truncated the base blob {object_id}")
        digests[object_id] = "sha256:" + hashlib.sha256(payload[start:end]).hexdigest()
        offset = end + 1
    if offset != len(payload):
        raise Refused("git cat-file returned more output than the base blobs requested")
    return digests


def config_origin_inside_checkout(checkout: pathlib.Path, origin: str) -> bool:
    """Whether a `git config --show-origin` origin names state inside the checkout.

    Unknown or non-file origins fail closed. Origins outside the checkout are not
    worker-writable through the isolated checkout and never reach this seal anyway,
    because the byte comparison does not consult Git's filtered view.
    """
    if not origin.startswith("file:"):
        return True
    candidate = pathlib.Path(origin[len("file:"):])
    if not candidate.is_absolute():
        candidate = checkout / candidate
    # Normalize lexically instead of resolving: a symlinked config file inside the
    # checkout must stay classified as checkout-local, not escape through resolve().
    root = os.path.normcase(os.path.normpath(str(checkout.absolute())))
    text = os.path.normcase(os.path.normpath(str(candidate.absolute())))
    return text == root or text.startswith(root + os.sep)


def refuse_filter_machinery(checkout: pathlib.Path, bound_paths: list[str]) -> None:
    """Refuse checkout-local attribute or filter machinery outright.

    collect_changes already reads object-store and worktree bytes directly, so no
    filter can influence that comparison; this check exists so no later Git command,
    gate or reader ever has to reason about which of them trusted rewritten content.
    """
    refusal_prefix = "seal refuses a checkout with checkout-local attribute or filter machinery: "

    def run(*arguments: str, stdin: bytes | None = None) -> subprocess.CompletedProcess:
        command = ["git", "-c", f"safe.directory={checkout}", "-C", str(checkout), *arguments]
        try:
            return subprocess.run(command, input=stdin, capture_output=True, check=False)
        except OSError as error:
            raise Refused(f"could not run git {arguments[0]}: {error}")

    configured = run("config", "--show-origin", "--get-regexp", r"^filter\.")
    if configured.returncode not in (0, 1):
        detail = configured.stderr.decode("utf-8", "replace").strip()
        raise Refused(f"git config failed ({configured.returncode}): {detail}")
    keys: set[str] = set()
    for line in configured.stdout.decode("utf-8", "replace").splitlines():
        origin, _, entry = line.partition("\t")
        if entry and config_origin_inside_checkout(checkout, origin):
            keys.add(entry.split(" ", 1)[0])
    if keys:
        raise Refused(refusal_prefix + "Git filter configuration [" + ", ".join(sorted(keys)) + "]")

    resolved = run("rev-parse", "--git-path", "info/attributes")
    if resolved.returncode != 0:
        raise Refused(refusal_prefix + "cannot resolve .git/info/attributes")
    attributes = pathlib.Path(resolved.stdout.decode("utf-8", "replace").strip())
    if not attributes.is_absolute():
        attributes = checkout / attributes
    if attributes.exists():
        raise Refused(refusal_prefix + ".git/info/attributes exists")

    attributes_file = run("config", "--get", "core.attributesFile")
    if attributes_file.returncode == 0 and attributes_file.stdout.strip():
        value = attributes_file.stdout.decode("utf-8", "replace").strip()
        raise Refused(refusal_prefix + f"core.attributesFile is set ({value})")
    if attributes_file.returncode not in (0, 1):
        raise Refused(refusal_prefix + f"git config failed ({attributes_file.returncode})")

    request = b"".join(utf8_path_key(path) + b"\0" for path in bound_paths)
    assigned = run("check-attr", "-a", "--stdin", "-z", "--", stdin=request)
    if assigned.returncode != 0:
        raise Refused(refusal_prefix + f"git check-attr failed ({assigned.returncode})")
    tokens = assigned.stdout.split(b"\0")
    rewrites: list[str] = []
    for index in range(0, len(tokens) - 2, 3):
        path_bytes, attribute_bytes, value_bytes = tokens[index], tokens[index + 1], tokens[index + 2]
        if not path_bytes:
            continue
        attribute = attribute_bytes.decode("utf-8", "replace")
        value = value_bytes.decode("utf-8", "replace")
        if attribute in ("filter", "working-tree-encoding") and value not in ("unspecified", "unset"):
            rewrites.append(f"{decode_path(path_bytes)} {attribute}={value}")
    if rewrites:
        raise Refused(refusal_prefix + "attributes assign content rewrites [" + ", ".join(rewrites[:5]) + "]")


def blob_object_id(payload: bytes, object_format: str) -> str:
    hasher = hashlib.new(object_format)
    hasher.update(b"blob " + str(len(payload)).encode("ascii") + b"\0")
    hasher.update(payload)
    return hasher.hexdigest()


def symlink_blob_entry(path: str, target: pathlib.Path, object_format: str) -> tuple[str, str]:
    """Blob (mode, object id) of one worktree symlink, for hidden target-swap checks."""
    try:
        payload = os.fsencode(os.readlink(target))
    except OSError as error:
        raise Refused(f"cannot read symlink {path}: {error}")
    return "120000", blob_object_id(payload, object_format)


def verify_base_commit(checkout: pathlib.Path, base: str) -> None:
    refuse_object_substitution(checkout)
    # Replacement refs are disabled for this peel, so the frozen base resolves to the
    # exact commit whose stored tree every later read binds.
    resolved = git_text(str(checkout), "rev-parse", "--verify", base + "^{commit}")
    if resolved != base:
        raise Refused("the frozen base commit is not the resolved checkout base")


def compute_subject(checkout: pathlib.Path, task_id: str, base: str, head: str) -> dict[str, Any]:
    entries = read_worktree_entries(checkout)
    manifest_hash, file_count = manifest_digest(entries)
    if git_text(str(checkout), "rev-parse", "HEAD") != head:
        raise Refused("checkout HEAD changed while binding its subject")
    hasher = hashlib.sha256()
    for field in (SUBJECT_NAMESPACE, task_id.encode("utf-8"), base.encode("ascii"),
                  head.encode("ascii"), manifest_hash.encode("ascii")):
        hasher.update(field + b"\0")
    return {
        "base": base,
        "head": head,
        "manifest_digest": manifest_hash,
        "content_revision": "uncommitted:" + manifest_hash,
        "subject_hash": "sha256:" + hasher.hexdigest(),
        "file_count": file_count,
    }


def collect_changes(checkout: pathlib.Path, base: str) -> dict[str, set[str]]:
    refuse_index_hacks(checkout)
    base_tree = read_base_tree(checkout, base)
    entries = read_worktree_entries(checkout)
    changes: dict[str, set[str]] = {}
    for path in entries:
        if path not in base_tree:
            changes.setdefault(path, set()).add("A")
    for path in base_tree:
        if path not in entries:
            changes.setdefault(path, set()).add("D")
    # Content is compared as bytes: the base side comes from the object store and the
    # worktree side was read directly, so no Git filter, attribute, index flag or stat
    # cache can make a changed file look unchanged.
    object_format = git_text(str(checkout), "rev-parse", "--show-object-format")
    if object_format not in ("sha1", "sha256"):
        raise Refused(f"unsupported Git repository object format: {object_format}")
    mode_tracked = executable_bits_tracked(checkout)
    content_objects = {
        base_object
        for path, (base_mode, base_object) in base_tree.items()
        if base_mode != "120000" and path in entries and entries[path][1] == "file"
    }
    base_digests = read_base_blob_digests(checkout, content_objects)
    for path in sorted(base_tree, key=utf8_path_key):
        base_mode, base_object = base_tree[path]
        entry = entries.get(path)
        if entry is None:
            continue  # a missing entry is already reported by the D name-set difference
        worktree_mode, kind, worktree_digest = entry
        if base_mode == "120000" and kind == "symlink":
            target = pathlib.Path(checkout, *path.split("/"))
            if symlink_blob_entry(path, target, object_format) != (base_mode, base_object):
                changes.setdefault(path, set()).add("M")
            continue
        if base_mode != "120000" and kind == "file":
            mode_changed = mode_tracked and worktree_mode != base_mode
            if mode_changed or worktree_digest != base_digests[base_object]:
                changes.setdefault(path, set()).add("M")
            continue
        changes.setdefault(path, set()).add("M")  # file<->symlink type change
    return changes


def validate_changes(
    checkout: pathlib.Path, task: dict[str, Any], base_files: set[str], changes: dict[str, set[str]]
) -> list[dict[str, str]]:
    """Apply the seal's path rules, then refuse checkout-local filter machinery.

    The path rules run first so a hidden edit is refused as the edit it is; the
    machinery check then refuses every filtered or attribute-driven checkout, whether
    or not it currently matches the base, because the whole run must proceed on a
    checkout whose Git view no clean/smudge filter can rewrite.
    """
    allowed = PathMatcher(task["allowed_paths"])
    protected = PathMatcher(task["protected_paths"])
    sealed_changes: list[dict[str, str]] = []
    for path in sorted(changes, key=utf8_path_key):
        status = "".join(sorted(changes[path]))
        baseline = path in base_files
        if is_protected_asset(path) or protected.matches(path):
            raise Refused(f"seal refused a change to the protected asset {path} ({status})")
        if baseline and is_test_path(path):
            raise Refused(f"seal refused a modification or deletion of the baseline test {path} ({status})")
        if not baseline and is_test_path(path) and not allowed.matches(path):
            raise Refused(f"seal refused a new test outside the explicitly scoped allowed_paths: {path}")
        if not allowed.matches(path):
            raise Refused(f"seal refused a changed path outside allowed_paths: {path} ({status})")
        sealed_changes.append({"path": path, "status": status})
    refuse_filter_machinery(checkout, sorted(base_files | set(changes), key=utf8_path_key))
    return sealed_changes


def current_subject(run_dir: pathlib.Path, task: dict[str, Any]) -> dict[str, Any]:
    checkout = run_dir / "checkout"
    verify_base_commit(checkout, task["base"])
    head = require_commit(git_text(str(checkout), "rev-parse", "HEAD"), "checkout HEAD")
    return compute_subject(checkout, task["task_id"], task["base"], head)


def require_current_subject(
    run_dir: pathlib.Path, task: dict[str, Any], sealed: dict[str, Any]
) -> dict[str, Any]:
    subject = current_subject(run_dir, task)
    if any(sealed[field] != value for field, value in subject.items()):
        raise Refused(
            "current source does not match the sealed subject; re-run seal and its checks before continuing"
        )
    checkout = run_dir / "checkout"
    recomputed = validate_changes(
        checkout, task, list_base_paths(checkout, task["base"]), collect_changes(checkout, task["base"])
    )
    if recomputed != sealed["changed_paths"]:
        raise Refused("the sealed changed paths no longer match the checkout changes; re-run seal")
    return subject


# ---------------------------------------------------------------------------
# Frozen input validation


def validate_gate_env(value: Any, source: str) -> dict[str, str]:
    """Validate one gate's frozen environment overrides; refuse secret-shaped names."""
    if not isinstance(value, dict):
        raise Refused(f"{source} env must be a JSON object")
    if len(value) > 8:
        raise Refused(f"{source} env must hold at most 8 entries")
    env: dict[str, str] = {}
    for name, item in value.items():
        if not isinstance(name, str) or not GATE_ENV_NAME_PATTERN.fullmatch(name):
            raise Refused(f"{source} env names must match {GATE_ENV_NAME_PATTERN.pattern}")
        if SECRET_KEY_PATTERN.search(name):
            raise Refused(f"{source} env name {name!r} looks like a secret")
        if not isinstance(item, str) or not item or len(item) > 128 or "\0" in item:
            raise Refused(f"{source} env {name} must be a non-empty string without NUL, at most 128 characters")
        env[name] = item
    return {name: env[name] for name in sorted(env)}


def validate_engine_test_fields(
    value: dict[str, Any], source: str, engine: str
) -> tuple[list[str] | None, list[str] | None]:
    """Validate one engine's `*_tests`/`*_files` gate pair.

    `exunit_*` and `vitest_*` carry the same caps, pairing and NUL rules; the
    entry-point check stays with the caller because `mix` names and vitest entry
    detection differ.
    """
    tests = None
    if f"{engine}_tests" in value:
        tests = require_text_list(
            value[f"{engine}_tests"], f"{source} {engine}_tests", minimum=1, unique=True, max_length=300
        )
        if len(tests) > 50:
            raise Refused(f"{source} {engine}_tests must list at most 50 tests")
        if any("\0" in name for name in tests):
            raise Refused(f"{source} {engine}_tests must not contain NUL")
        # The ExUnit trace matcher counts `name.replace("\n", " ")` against rendered
        # trace lines, so two frozen names rendering identically would both be
        # satisfied by one executed test; the vitest report compares names as written,
        # and this shared helper freezes both engines to unique rendered spellings.
        rendered = [name.replace("\n", " ") for name in tests]
        if len(set(rendered)) != len(rendered):
            raise Refused(f"{source} {engine}_tests entries must stay unique after newlines render as spaces")
    files = None
    if f"{engine}_files" in value:
        files = require_path_list(value[f"{engine}_files"], f"{source} {engine}_files", allow_glob=False)
        if not files:
            raise Refused(f"{source} {engine}_files must list at least 1 file")
        if len(files) > 20:
            raise Refused(f"{source} {engine}_files must list at most 20 files")
    if files is not None and tests is None:
        raise Refused(f"{source} {engine}_files requires {engine}_tests")
    if tests is not None and files is None:
        raise Refused(f"{source} {engine}_tests requires {engine}_files")
    return tests, files


def validate_gate(value: Any, index: int) -> dict[str, Any]:
    source = f"task JSON gate #{index + 1}"
    require_object(value, source, GATE_KEYS, REQUIRED_GATE_KEYS)
    gate_id = require_identifier(value["id"], source + " id")
    cwd = "." if value["cwd"] == "." else require_relative_path(value["cwd"], source + " cwd", allow_glob=False)
    argv_value = value["argv"]
    if not isinstance(argv_value, list) or not argv_value:
        raise Refused(f"{source} argv must be a non-empty list")
    argv: list[str] = []
    for item in argv_value:
        if not isinstance(item, str) or not item or "\0" in item:
            raise Refused(f"{source} argv entries must be non-empty strings without NUL")
        argv.append(item)
    go_tests = None
    if "go_tests" in value:
        go_tests = require_text_list(value["go_tests"], source + " go_tests", minimum=1, unique=True, max_length=300)
    go_package = None
    if "go_package" in value:
        go_package = require_text(value["go_package"], source + " go_package", max_length=500)
        if go_tests is None:
            raise Refused(f"{source} go_package requires go_tests")
    exunit_tests, exunit_files = validate_engine_test_fields(value, source, "exunit")
    if exunit_tests is not None and pathlib.PurePath(argv[0]).name.lower() not in (
        "mix", "mix.exe", "mix.bat", "mix.cmd"
    ):
        raise Refused(f"{source} exunit_tests requires a mix gate command")
    vitest_tests, vitest_files = validate_engine_test_fields(value, source, "vitest")
    if vitest_tests is not None and not is_vitest_entry(argv):
        raise Refused(f"{source} vitest_tests requires a vitest entry-point gate command")
    return {
        "id": gate_id,
        "cwd": cwd,
        "argv": argv,
        "go_tests": go_tests,
        "go_package": go_package,
        "exunit_tests": exunit_tests,
        "exunit_files": exunit_files,
        "vitest_tests": vitest_tests,
        "vitest_files": vitest_files,
        "env": validate_gate_env(value["env"], source) if "env" in value else {},
    }


def validate_task(value: Any) -> dict[str, Any]:
    require_object(value, "task JSON", TASK_KEYS, TASK_KEYS)
    task_id = require_identifier(value["task_id"], "task JSON task_id")
    base = require_commit(value["base"], "task JSON base")
    for key in ("language", "risk_class", "workflow", "outcome"):
        require_text(value[key], f"task JSON {key}")
    explanations = require_text_list(value["explanations"], "task JSON explanations", minimum=0, unique=True)
    allowed_paths = require_path_list(value["allowed_paths"], "task JSON allowed_paths", allow_glob=True)
    protected_paths = require_path_list(value["protected_paths"], "task JSON protected_paths", allow_glob=True)
    gate_values = value["gates"]
    if not isinstance(gate_values, list) or not gate_values:
        raise Refused("task JSON gates must be a non-empty list")
    gates = [validate_gate(gate, index) for index, gate in enumerate(gate_values)]
    gate_ids = [gate["id"] for gate in gates]
    if len(set(gate_ids)) != len(gate_ids):
        raise Refused("task JSON gate ids must be unique")
    return {
        "task_id": task_id,
        "base": base,
        "language": value["language"],
        "risk_class": value["risk_class"],
        "workflow": value["workflow"],
        "outcome": value["outcome"],
        "explanations": explanations,
        "allowed_paths": allowed_paths,
        "protected_paths": protected_paths,
        "gates": gates,
    }


def validate_routes(value: Any) -> dict[str, dict[str, str]]:
    require_object(value, "routes JSON", ROUTES_KEYS, ROUTES_KEYS)
    routes: dict[str, dict[str, str]] = {}
    for role in ("worker", "reviewer"):
        block = require_object(value[role], f"routes JSON {role}", ROLE_KEYS, ROLE_KEYS)
        routes[role] = {
            key: require_text(block[key], f"routes JSON {role} {key}", max_length=300)
            for key in ("model_id", "provenance", "credential_route", "authorization_ref")
        }
    return routes


def redact_host_paths(value: str) -> str:
    """Replace every host-specific absolute path in a recorded value with a hashed token.

    The token is ``sha256:<digest of the matched path bytes>``: two records that name
    the same path still agree, and no operator profile or disposable root is published
    by the record itself.
    """
    for pattern in host_path_patterns():
        value = pattern.sub(lambda match: sha256_hex(match.group(0).encode("utf-8")), value)
    return value


def sanitize_source_digest() -> str:
    """The digest of the sanitize tool's current bytes, as `tooling_sources` binds them."""
    return sha256_hex(read_bytes(SANITIZE_SCRIPT, MAX_RECORD_BYTES, "evaluation tooling source"))


def require_recorded_redaction(run: dict[str, Any]) -> None:
    """Refuse a derivation whose redacted values were rendered by other tool bytes.

    A receipt binds the current ``model-routing-record-sanitize.py`` in
    ``evidence.tooling_sources``, so it must not be derived for values those bytes
    did not render: the digest each redaction recorded at its own moment (prepare's
    ``measured``, bind's ``install_log_source``) is compared with the current bytes
    here, before the attestation exists (STD-A02 - the review probe mutated the tool
    between prepare and receipt and the derivation still accepted the old values as
    the new tool's product). A run whose record predates the binding records no
    digest and keeps its previous behavior.
    """
    environment = run["environment"]
    provisioning = environment.get("provisioning") or {}
    recorded = [
        environment.get("measured_sanitizer_sha256"),
        provisioning.get("install_log_source_sanitizer_sha256"),
    ]
    current = None
    for digest in recorded:
        if digest is None:
            continue
        if current is None:
            current = sanitize_source_digest()
        if digest != current:
            raise Refused(
                "the run's recorded redaction does not match the current "
                "model-routing-record-sanitize.py bytes: the value was rendered by "
                f"{digest} but the receipt would bind {current}"
            )


def validate_measured_mapping(value: Any, source: str, redact: bool = False) -> dict[str, str]:
    """Validate one measured execution-surface mapping (run record or environment JSON).

    ``redact`` is set on the recording path (``prepare --environment-json``), so raw
    host paths never enter the run record or the receipts derived from it. Re-reading a
    run record validates recorded values unchanged, which keeps run directories written
    before this extension re-derivable byte for byte.
    """
    if not isinstance(value, dict):
        raise Refused(f"{source} must be a JSON object")
    if len(value) > 24:
        raise Refused(f"{source} must hold at most 24 entries")
    measured: dict[str, str] = {}
    for name, item in value.items():
        if not isinstance(name, str) or not MEASURED_NAME_PATTERN.fullmatch(name):
            raise Refused(f"{source} names must match {MEASURED_NAME_PATTERN.pattern}")
        if SECRET_KEY_PATTERN.search(name):
            raise Refused(f"{source} name {name!r} looks like a secret")
        if (
            not isinstance(item, str) or not item or len(item) > 300
            or "\0" in item or "\r" in item or "\n" in item
        ):
            raise Refused(
                f"{source} {name} must be a non-empty single-line string without NUL, at most 300 characters"
            )
        measured[name] = redact_host_paths(item) if redact else item
    return {name: measured[name] for name in sorted(measured)}


def validate_provisioning(value: Any, source: str) -> dict[str, Any]:
    """Validate the recorded provisioning step of the resolved dependency tree."""
    block = require_object(
        value, source, PROVISIONING_KEYS | {"install_log_source_sanitizer_sha256"}, PROVISIONING_KEYS
    )
    provisioning: dict[str, Any] = {
        "tree": require_text(block["tree"], source + " tree", max_length=300),
        "tree_digest": require_digest(block["tree_digest"], source + " tree_digest"),
        "tree_entries": require_integer(block["tree_entries"], source + " tree_entries", 1),
        "install_log": require_text(block["install_log"], source + " install_log", max_length=300),
        "install_log_source": require_text(
            block["install_log_source"], source + " install_log_source", max_length=300
        ),
        "install_log_sha256": require_digest(block["install_log_sha256"], source + " install_log_sha256"),
        "install_log_bytes": require_integer(block["install_log_bytes"], source + " install_log_bytes", 1),
    }
    if "install_log_source_sanitizer_sha256" in block:
        # The bytes that rendered `install_log_source` at the bind moment, additive:
        # a provisioned block recorded before this binding carries none.
        provisioning["install_log_source_sanitizer_sha256"] = require_digest(
            block["install_log_source_sanitizer_sha256"], source + " install_log_source_sanitizer_sha256"
        )
    if provisioning["install_log"] != PROVISIONING_LOG_PATH:
        raise Refused(f"{source} install_log must be {PROVISIONING_LOG_PATH!r}")
    route = provisioning["tree"]
    if (
        route.startswith("/") or "\\" in route or re.match(r"^[A-Za-z]:", route)
        or any(part == "" for part in route.split("/"))
    ):
        raise Refused(
            f"{source} tree must be a run-relative path so a reader can recompute the digest "
            "from the stored run directory"
        )
    return provisioning


def provisioning_view(provisioning: dict[str, Any]) -> dict[str, Any]:
    """The identity of one recorded provisioning step, as an attempt records it."""
    return {
        "tree": provisioning["tree"],
        "tree_digest": provisioning["tree_digest"],
        "tree_entries": provisioning["tree_entries"],
    }


def validate_attempt_provisioning(value: Any, source: str) -> dict[str, Any] | None:
    """One check attempt's view of the bound dependency-provisioning step, if any."""
    if value is None:
        return None
    keys = {"tree", "tree_digest", "tree_entries"}
    block = require_object(value, source, keys, keys)
    return {
        "tree": require_text(block["tree"], source + " tree", max_length=300),
        "tree_digest": require_digest(block["tree_digest"], source + " tree_digest"),
        "tree_entries": require_integer(block["tree_entries"], source + " tree_entries", 1),
    }


def verify_dependency_tree(run_dir: pathlib.Path, provisioning: dict[str, Any]) -> None:
    """Recompute the recorded dependency-tree digest and refuse a missing or changed tree.

    The recorded route is run-relative, so this re-verification works from the stored run
    directory alone: a tree that was edited, or pruned after the run, refuses instead of
    leaving the recorded binding unverifiable.
    """
    tree = run_dir / provisioning["tree"]
    if not tree.is_dir():
        raise Refused(f"the recorded dependency tree {provisioning['tree']!r} is missing")
    digest, entries = dependency_tree_digest(tree)
    if digest != provisioning["tree_digest"] or entries != provisioning["tree_entries"]:
        raise Refused(
            "the recorded dependency tree changed after it was recorded: "
            f"{digest} over {entries} entries != {provisioning['tree_digest']} over "
            f"{provisioning['tree_entries']} entries"
        )


def requires_dependency_provisioning(task: dict[str, Any]) -> bool:
    """True when the frozen task executes Node tooling that resolves a dependency tree.

    The gate's argv is part of the claim: a task that runs a vitest entry point
    resolves the same tree whatever its language field says, so the binding cannot be
    skipped by declaring a different language.
    """
    # `.get` keeps the predicate total for a caller that holds a raw task document (the
    # regression suite calls it with one); validated tasks carry every key.
    return task.get("language") == "typescript" or any(
        gate.get("vitest_tests") is not None or is_vitest_entry(gate.get("argv") or [])
        for gate in task.get("gates", ())
    )


def require_dependency_provisioning(
    run_dir: pathlib.Path, run: dict[str, Any], task: dict[str, Any]
) -> None:
    """Require the recorded dependency provisioning to still be the executed tree.

    The module's gates resolve their dependencies from the checkout at execution
    time, so the recorded binding is re-verified here, on both callers: `check`,
    before any gate runs, and receipt derivation, before the attestation is written.
    A `node_modules` planted or retargeted after the bind (the F01 review probe's
    check-side arm: a junction retarget at the run-store placeholder made the gates
    execute a tree the receipt never attests) is refused instead of executed.
    """
    if not requires_dependency_provisioning(task):
        return
    provisioning = run["environment"].get("provisioning")
    if provisioning is None:
        raise Refused(
            "this task executes Node tooling from a dependency tree; record it with "
            "bind-dependencies before check or receipt"
        )
    bound = run_dir / provisioning["tree"]
    if not bound.is_dir():
        raise Refused(f"the recorded dependency tree {provisioning['tree']!r} is missing")
    try:
        bound_resolved = bound.resolve()
    except (OSError, RuntimeError) as error:
        raise Refused(f"the recorded dependency tree {provisioning['tree']!r} does not resolve: {error}")
    require_resolved_dependency_tree(run_dir, run, task, bound_resolved)


def validate_environment(value: Any) -> dict[str, Any]:
    block = require_object(
        value,
        "run record environment",
        ENVIRONMENT_KEYS | {"measured", "provisioning", "measured_sanitizer_sha256"},
        ENVIRONMENT_KEYS,
    )
    environment: dict[str, Any] = {
        key: require_text(block[key], f"run record environment {key}", max_length=300)
        for key in sorted(ENVIRONMENT_KEYS)
    }
    if "measured" in block:
        environment["measured"] = validate_measured_mapping(
            block["measured"], "run record environment measured"
        )
    if "measured_sanitizer_sha256" in block:
        # The bytes that rendered `measured` at the prepare moment, additive: a run
        # recorded before this binding carries none.
        environment["measured_sanitizer_sha256"] = require_digest(
            block["measured_sanitizer_sha256"], "run record environment measured_sanitizer_sha256"
        )
    if "provisioning" in block:
        environment["provisioning"] = validate_provisioning(
            block["provisioning"], "run record environment provisioning"
        )
    return environment


def load_run(run_dir: pathlib.Path) -> dict[str, Any]:
    raw = read_bytes(run_dir / "run.json", MAX_RECORD_BYTES, "run record")
    value = strict_json(raw, "run record", MAX_RECORD_BYTES)
    require_object(value, "run record", RUN_KEYS, REQUIRED_RUN_KEYS)
    if value["schema"] != SCHEMA:
        raise Refused("run record schema mismatch")
    run = {
        "schema": SCHEMA,
        "task_id": require_identifier(value["task_id"], "run record task_id"),
        "base": require_commit(value["base"], "run record base"),
        "source": require_text(value["source"], "run record source", max_length=2000),
        "head": require_commit(value["head"], "run record head"),
        "base_tree_digest": require_digest(value["base_tree_digest"], "run record base_tree_digest"),
        "task_sha256": require_digest(value["task_sha256"], "run record task_sha256"),
        "routes_sha256": require_digest(value["routes_sha256"], "run record routes_sha256"),
        "environment": validate_environment(value["environment"]),
    }
    if "provisioning" in run["environment"]:
        # The recorded log digest is checked against the stored copy on every load, so a
        # rewritten or truncated provisioning log is refused before it reaches a receipt.
        stored_log = read_bytes(
            run_dir / run["environment"]["provisioning"]["install_log"],
            MAX_PROVISIONING_LOG_BYTES, "stored provisioning log",
        )
        provisioning = run["environment"]["provisioning"]
        if (
            len(stored_log) != provisioning["install_log_bytes"]
            or sha256_hex(stored_log) != provisioning["install_log_sha256"]
        ):
            raise Refused("the stored provisioning log changed after it was recorded")
        verify_dependency_tree(run_dir, provisioning)
    for key in ("review_sha256", "usage_sha256"):
        if key in value:
            run[key] = require_digest(value[key], f"run record {key}")
    return run


def run_record_bytes(run: dict[str, Any], **updates: str) -> bytes:
    merged = dict(run)
    merged.update(updates)
    return canonical_json(merged)


def save_run(run_dir: pathlib.Path, run: dict[str, Any], **updates: str) -> None:
    write_bytes(run_dir / "run.json", run_record_bytes(run, **updates))


def load_frozen_task(run_dir: pathlib.Path, run: dict[str, Any]) -> dict[str, Any]:
    raw = read_bytes(run_dir / "frozen" / "task.json", MAX_TASK_BYTES, "frozen task JSON")
    if sha256_hex(raw) != run["task_sha256"]:
        raise Refused("frozen task bytes changed after prepare")
    value = strict_json(raw, "frozen task JSON", MAX_TASK_BYTES)
    scan_no_secret_keys(value, "frozen task JSON")
    task = validate_task(value)
    if task["task_id"] != run["task_id"] or task["base"] != run["base"]:
        raise Refused("frozen task does not match the run record")
    return task


def load_frozen_routes(run_dir: pathlib.Path, run: dict[str, Any]) -> dict[str, dict[str, str]]:
    raw = read_bytes(run_dir / "frozen" / "routes.json", MAX_ROUTES_BYTES, "frozen routes JSON")
    if sha256_hex(raw) != run["routes_sha256"]:
        raise Refused("frozen routes bytes changed after prepare")
    value = strict_json(raw, "frozen routes JSON", MAX_ROUTES_BYTES)
    scan_no_secret_keys(value, "frozen routes JSON")
    return validate_routes(value)


def load_sealed(run_dir: pathlib.Path, task: dict[str, Any]) -> dict[str, Any]:
    raw = read_bytes(run_dir / "sealed.json", MAX_RECORD_BYTES, "sealed subject")
    value = strict_json(raw, "sealed subject", MAX_RECORD_BYTES)
    require_object(value, "sealed subject", SEALED_KEYS, SEALED_KEYS)
    if value["schema"] != SCHEMA:
        raise Refused("sealed subject schema mismatch")
    if value["task_id"] != task["task_id"] or value["base"] != task["base"]:
        raise Refused("sealed subject does not match the frozen task")
    sealed = {
        "schema": SCHEMA,
        "task_id": task["task_id"],
        "base": task["base"],
        "head": require_commit(value["head"], "sealed subject head"),
        "manifest_digest": require_digest(value["manifest_digest"], "sealed subject manifest_digest"),
        "content_revision": require_text(value["content_revision"], "sealed subject content_revision"),
        "subject_hash": require_digest(value["subject_hash"], "sealed subject subject_hash"),
        "file_count": require_integer(value["file_count"], "sealed subject file_count"),
        "changed_paths": [],
    }
    if sealed["content_revision"] != "uncommitted:" + sealed["manifest_digest"]:
        raise Refused("sealed subject content revision does not match its manifest digest")
    changes = value["changed_paths"]
    if not isinstance(changes, list):
        raise Refused("sealed subject changed_paths must be a list")
    for index, change in enumerate(changes):
        source = f"sealed subject change #{index + 1}"
        entry = require_object(change, source, CHANGE_KEYS, CHANGE_KEYS)
        path = require_text(entry["path"], source + " path", max_length=512)
        status = require_text(entry["status"], source + " status", max_length=8)
        if any(character not in "AMDTUX" for character in status):
            raise Refused(f"{source} status must use git status letters")
        sealed["changed_paths"].append({"path": path, "status": status})
    return sealed


# ---------------------------------------------------------------------------
# Review record


def validate_review_structure(value: Any) -> dict[str, Any]:
    require_object(value, "review record", REVIEW_KEYS, REVIEW_KEYS)
    attempt_id = value["check_attempt_id"]
    if not isinstance(attempt_id, str) or not ATTEMPT_PATTERN.fullmatch(attempt_id):
        raise Refused("review record check_attempt_id must name a check attempt like attempt-0001")
    reviewer_block = require_object(value["reviewer"], "review record reviewer", REVIEWER_KEYS, REVIEWER_KEYS)
    worker_block = require_object(value["worker"], "review record worker", WORKER_KEYS, WORKER_KEYS)
    findings_value = value["findings"]
    if not isinstance(findings_value, list):
        raise Refused("review record findings must be a list")
    findings: list[dict[str, str]] = []
    identifiers: set[str] = set()
    for index, finding in enumerate(findings_value):
        source = f"review record finding #{index + 1}"
        entry = require_object(finding, source, FINDING_KEYS, FINDING_KEYS)
        finding_id = require_identifier(entry["id"], source + " id")
        if finding_id in identifiers:
            raise Refused("review record finding ids must be unique")
        identifiers.add(finding_id)
        findings.append({
            "id": finding_id,
            "severity": require_value_in(entry["severity"], source + " severity", SEVERITIES),
            "status": require_value_in(entry["status"], source + " status", FINDING_STATUSES),
            "description": require_text(entry["description"], source + " description", max_length=2000),
        })
    return {
        "subject_hash": require_digest(value["subject_hash"], "review record subject_hash"),
        "routes_sha256": require_digest(value["routes_sha256"], "review record routes_sha256"),
        "check_attempt_id": attempt_id,
        "check_evidence_sha256": require_digest(value["check_evidence_sha256"], "review record check_evidence_sha256"),
        "reviewer": {
            "model_id": require_text(reviewer_block["model_id"], "review record reviewer model_id", max_length=300),
            "session_ref": require_text(reviewer_block["session_ref"], "review record reviewer session_ref", max_length=300),
        },
        "worker": {
            "model_id": require_text(worker_block["model_id"], "review record worker model_id", max_length=300),
            "session_refs": require_text_list(
                worker_block["session_refs"], "review record worker session_refs",
                minimum=1, unique=True, max_length=300,
            ),
        },
        "verdict": require_value_in(value["verdict"], "review record verdict", ("pass", "fail")),
        "gate_weakening": require_bool(value["gate_weakening"], "review record gate_weakening"),
        "findings": findings,
        "explanations": require_string_mapping(value["explanations"], "review record explanations"),
    }


def review_semantic_blockers(
    review: dict[str, Any],
    task: dict[str, Any],
    routes: dict[str, dict[str, str]],
    routes_sha256: str,
    subject_hash: str,
    attempt_id: str | None,
    attempt_sha256: str | None,
) -> list[str]:
    blockers: list[str] = []
    if review["subject_hash"] != subject_hash:
        blockers.append("the review record does not match the sealed subject")
    if review["routes_sha256"] != routes_sha256:
        blockers.append("the review record does not match the frozen routes")
    if attempt_id is None:
        blockers.append("no check attempt is bound to the sealed subject, so the review cannot bind executed evidence")
    else:
        if review["check_attempt_id"] != attempt_id:
            blockers.append(
                f"the review record binds {review['check_attempt_id']} but the latest sealed-subject attempt is {attempt_id}"
            )
        if review["check_evidence_sha256"] != attempt_sha256:
            blockers.append("the review record does not match the stored check attempt evidence")
    if review["reviewer"]["model_id"] != routes["reviewer"]["model_id"]:
        blockers.append("the review reviewer model does not match the frozen reviewer route")
    if review["worker"]["model_id"] != routes["worker"]["model_id"]:
        blockers.append("the review worker model does not match the frozen worker route")
    if review["reviewer"]["session_ref"] in review["worker"]["session_refs"]:
        blockers.append("the review session is not independent from the worker sessions")
    if review["verdict"] != "pass":
        blockers.append(f"the review verdict is {review['verdict']}")
    if review["gate_weakening"] is not False:
        blockers.append("the review reports gate weakening")
    open_findings = sorted(finding["id"] for finding in review["findings"] if finding["status"] == "open")
    if open_findings:
        blockers.append("the review has unresolved findings: " + ", ".join(open_findings))
    required = set(task["explanations"])
    attested = set(review["explanations"])
    missing = sorted(required - attested, key=lambda value: value.encode("utf-8"))
    extra = sorted(attested - required, key=lambda value: value.encode("utf-8"))
    if missing:
        blockers.append(f"the review does not attest {len(missing)} required task explanation(s)")
    if extra:
        blockers.append(f"the review attests {len(extra)} explanation(s) not required by the task")
    empty = sum(1 for text in required & attested if not review["explanations"][text].strip())
    if empty:
        blockers.append(f"the review records {empty} empty explanation(s) for required task explanations")
    return blockers


# ---------------------------------------------------------------------------
# Gate execution and stored evidence verification


def go_list_argv(gate: dict[str, Any]) -> list[str] | None:
    """Derive the `go test -list` cross-check argv for a named-Go-test gate, if any."""
    if not gate["go_tests"] or not gate["go_package"]:
        return None
    if pathlib.PurePath(gate["argv"][0]).name.lower() not in ("go", "go.exe"):
        return None
    if any("/" in name for name in gate["go_tests"]):
        return None
    pattern = "^(" + "|".join(re.escape(name) for name in gate["go_tests"]) + ")$"
    return ["go", "test", "-list", pattern, gate["go_package"]]


def go_list_detail(expected: list[str], exit_code: int | None, stdout_raw: bytes) -> str | None:
    """First missing or duplicated expected test name in `go test -list` stdout."""
    if exit_code is None:
        return "the list command timed out"
    if exit_code != 0:
        return f"the list command exited with {exit_code}"
    lines = [line.strip() for line in stdout_raw.decode("utf-8", "replace").splitlines()]
    for name in expected:
        count = lines.count(name)
        if count == 0:
            return f"missing {name}"
        if count > 1:
            return f"{name} was listed {count} times"
    return None


def exunit_trace_argv(gate: dict[str, Any]) -> list[str] | None:
    """Derive the `mix test --trace` cross-check argv for a named-ExUnit-test gate, if any."""
    if not gate["exunit_tests"]:
        return None
    return [gate["argv"][0], "test", "--trace", "--seed", "0", *gate["exunit_files"]]


# The launcher flags that consume the argument after them: a vitest entry spelled
# past one of them (`node --require ./setup.cjs vitest.mjs run`) is still the entry
# the interpreter runs, so the value must not hide it - the same spelling the
# F09 `vitest_entry_index` fix must see through (STD-A04). A flag written with its
# value joined (`--require=./setup.cjs`) is a single flag argument, so the plain
# flag skip below already covers it.
NODE_VALUE_FLAGS = ("-r", "--require", "--import", "--loader", "--experimental-loader")


def vitest_entry_index(argv: list[str]) -> int | None:
    """The argv index of the vitest entry point it runs, or None.

    The launcher's own flags stand between the interpreter and the entry
    (`node --no-warnings x/vitest.mjs run`), so the entry is the first argument that
    is not a flag and not a value-taking flag's value: a spelling whose entry hides
    behind either would otherwise skip the dependency-provisioning requirement the
    entry point implies and the derived report command would run the wrong file.
    """
    index = 1
    while index < len(argv):
        argument = argv[index]
        if argument.startswith("-"):
            index += 2 if argument in NODE_VALUE_FLAGS else 1
            continue
        name = pathlib.PurePath(argument).name.lower()
        return index if name.startswith("vitest") and name.endswith((".mjs", ".cjs", ".js")) else None
    return None


def is_vitest_entry(argv: list[str]) -> bool:
    """True when argv runs a vitest entry point as `<interpreter> [flags] <vitest entry>`."""
    return vitest_entry_index(argv) is not None


def vitest_report_argv(gate: dict[str, Any], gate_dir: pathlib.Path) -> list[str] | None:
    """Derive the structured vitest cross-check argv for a named-vitest-test gate, if any.

    The derived run writes vitest's own JSON report next to the gate evidence, so
    the check parses a structured document instead of matching printed text. The
    command reuses the gate's own launcher and flags up to its vitest entry (the
    same shape gate validation required), so a flagged spelling reruns vitest
    instead of handing the flags to a different file.
    """
    if not gate["vitest_tests"]:
        return None
    entry_index = vitest_entry_index(gate["argv"])
    if entry_index is None:
        raise Refused(
            f"gate {gate['id']} declares vitest tests but its argv names no recognizable "
            "vitest entry point, so the derived report command cannot be built"
        )
    return [
        *gate["argv"][:entry_index + 1],
        "run",
        "--reporter=json",
        "--outputFile",
        str(gate_dir / "vitest-report.json"),
        *gate["vitest_files"],
    ]


def vitest_report_detail(expected: list[str], exit_code: int | None, report_raw: bytes) -> str | None:
    """First reason the derived vitest report is not acceptance evidence, or None.

    The report is vitest's JSON reporter document: every named test must appear
    exactly once with `status: "passed"` at module level (no `ancestorTitles`, so a
    frozen name binds one top-level declaration), the reporter's own failure count
    must be zero and its success flag true. A missing, empty, unparsable or
    summary-less report fails closed instead of reading as a pass.

    The refusals are ordered so the most specific defect is the one reported: an
    unreadable or non-object document, a non-success flag, a summary whose total is
    not an integer, a non-zero failure count, an empty result list, then the named
    tests themselves - while walking the assertions, a name declared inside a
    describe block or finished with a status other than `passed`, and afterwards a
    name reported zero times or more than once - and last a total smaller than the
    frozen named list. The walk interleaves its structural refusals in that order
    too: a file result that is not an object, a file result without an assertions
    list, an assertion that is not an object, and an assertion without a name,
    status or ancestor list each refuse before the per-name checks reach that entry,
    and the exit-code refusals are checked first of all. This paragraph is the order,
    not an inventory: every branch in this function refuses, and the order only
    decides which reason a reader sees first.
    """
    if exit_code is None:
        return "the derived vitest run timed out"
    if exit_code != 0:
        return f"the derived vitest run exited with code {exit_code}"
    try:
        report = strict_json(report_raw, "derived vitest report", MAX_CAPTURE_BYTES)
    except Refused:
        return "the derived vitest run wrote no readable JSON report"
    if not isinstance(report, dict):
        return "the derived vitest report is not a JSON object"
    if report.get("success") is not True:
        return "the derived vitest report does not record a successful run"
    total = report.get("numTotalTests")
    failed = report.get("numFailedTests")
    if isinstance(total, bool) or not isinstance(total, int):
        return "the derived vitest report carries no complete test summary"
    if isinstance(failed, bool) or not isinstance(failed, int) or failed != 0:
        return "the derived vitest report records failing tests"
    test_results = report.get("testResults")
    if not isinstance(test_results, list) or not test_results:
        return "the derived vitest report carries no test results"
    counts = dict.fromkeys(expected, 0)
    for file_result in test_results:
        if not isinstance(file_result, dict):
            return "the derived vitest report carries a malformed file result"
        assertions = file_result.get("assertionResults")
        if not isinstance(assertions, list):
            return "the derived vitest report carries a file result without assertions"
        for assertion in assertions:
            if not isinstance(assertion, dict):
                return "the derived vitest report carries a malformed assertion"
            full_name = assertion.get("fullName")
            status = assertion.get("status")
            ancestors = assertion.get("ancestorTitles")
            if not isinstance(full_name, str) or not isinstance(status, str) or not isinstance(ancestors, list):
                return "the derived vitest report carries an assertion without a name, status or ancestor list"
            if full_name not in counts:
                continue
            if ancestors:
                return f"the named test {full_name!r} is declared inside a describe block"
            if status != "passed":
                return f"the named test {full_name!r} finished with status {status!r}"
            counts[full_name] += 1
    for name in expected:
        if counts[name] == 0:
            return f"missing {name}"
        if counts[name] > 1:
            return f"{name} was reported {counts[name]} times"
    if total < len(expected):
        return "the derived vitest report carries no complete test summary"
    return None


def exunit_trace_observations(expected: list[str], stdout_raw: bytes) -> list[dict[str, Any]]:
    """Count finished `mix test --trace` observations for each expected test name.

    ANSI escapes are stripped and CR-separated progress lines are split apart first,
    because ExUnit rewrites one trace line per finished test. A passing or failing
    test prints `* <name> (<n>.<d>ms) [L#<line>]`; `(skipped)` and `(excluded)`
    lines are not finished observations.
    """
    text = EXUNIT_ANSI_PATTERN.sub("", stdout_raw.decode("utf-8", "replace"))
    observed: list[str] = []
    for line in text.splitlines():
        match = EXUNIT_OBSERVATION_PATTERN.fullmatch(line)
        if match and EXUNIT_FINISHED_PATTERN.fullmatch(match.group("descriptor")):
            observed.append(match.group("name"))
    return [
        {"name": name, "count": observed.count(name.replace("\n", " "))}
        for name in expected
    ]


def exunit_trace_failures(stdout_raw: bytes) -> int | None:
    """Failure count from the last `mix test` result summary, or None without one."""
    text = EXUNIT_ANSI_PATTERN.sub("", stdout_raw.decode("utf-8", "replace"))
    matches = list(EXUNIT_RESULT_PATTERN.finditer(text))
    if not matches:
        return None
    last = matches[-1]
    if last.group("total") is not None:
        # `Result: N/M passed` is a complete count; the restated `Failed: K test` line
        # below it must not be added again.
        return int(last.group("total")) - int(last.group("passed"))
    tail = EXUNIT_TAIL_FAILED_PATTERN.search(last.group("tail"))
    if tail is not None:
        return int(tail.group(1))
    failed_lines = EXUNIT_FAILED_LINE_PATTERN.findall(text[last.end():])
    return int(failed_lines[-1]) if failed_lines else 0


def exunit_trace_detail(expected: list[str], exit_code: int | None, stdout_raw: bytes) -> str | None:
    """First problem with one derived `mix test --trace` run, or None when acceptable."""
    if exit_code is None:
        return "the traced run timed out"
    if exit_code != 0:
        return f"the traced run exited with {exit_code}"
    for observation in exunit_trace_observations(expected, stdout_raw):
        if observation["count"] == 0:
            return f"missing {observation['name']}"
        if observation["count"] > 1:
            return f"{observation['name']} was observed {observation['count']} times"
    failures = exunit_trace_failures(stdout_raw)
    if failures is None:
        return "the traced run reported no ExUnit summary"
    if failures != 0:
        return f"the traced run reported {failures} failures"
    return None


def validator_argv(gate_dir: pathlib.Path, gate: dict[str, Any]) -> list[str]:
    argv = [sys.executable, str(VALIDATOR_SCRIPT), str(gate_dir / "stdout.bin")]
    if gate["go_package"]:
        argv += ["--package", gate["go_package"]]
    argv += list(gate["go_tests"])
    return argv


def run_validator(
    gate_dir: pathlib.Path, gate: dict[str, Any], env: dict[str, str] | None = None
) -> dict[str, Any]:
    argv = validator_argv(gate_dir, gate)
    try:
        completed = subprocess.run(argv, capture_output=True, check=False, env=env)
    except OSError as error:
        raise Refused(f"could not run the Go event validator: {error}")
    write_bytes(gate_dir / "validator.stdout.bin", completed.stdout)
    write_bytes(gate_dir / "validator.stderr.bin", completed.stderr)
    return {
        "argv": argv,
        "exit_code": completed.returncode,
        "stdout_sha256": sha256_hex(completed.stdout),
        "stderr_sha256": sha256_hex(completed.stderr),
        "stdout_bytes": len(completed.stdout),
        "stderr_bytes": len(completed.stderr),
    }


def terminate_owned_process_tree(process: subprocess.Popen) -> None:
    """Terminate one owned gate process group and all its descendants.

    POSIX gates start in their own session, so one SIGKILL to the process group
    reaches every descendant. Windows has no portable group kill; `taskkill /T`
    walks the process tree. A failure is reported instead of continuing, because a
    surviving gate process can still mutate the checkout or hold resources.
    """
    if os.name == "nt":
        killed = subprocess.run(
            ["taskkill", "/F", "/T", "/PID", str(process.pid)], capture_output=True, check=False
        )
        if killed.returncode != 0 and process.poll() is None:
            raise Refused(f"could not terminate the gate process tree (taskkill exit {killed.returncode})")
        return
    try:
        os.killpg(os.getpgid(process.pid), signal.SIGKILL)
    except OSError:
        process.kill()


def run_owned_command(
    argv: list[str], cwd: str, timeout: float | None, env: dict[str, str] | None = None
) -> tuple[int | None, bytes, bytes, bool]:
    """Run one command in an owned process group; return (exit_code, stdout, stderr, timed_out).

    A given `env` replaces the inherited environment for the command; None means the
    command inherits the runner's own environment.
    On timeout the whole tree is terminated and reaped before returning, so a later
    gate never runs next to survivors that could write into the checkout. A
    descendant that escaped the owned group (or the platform process tree) and still
    holds a pipe cannot be reached; that case marks the run timed out instead of
    hanging the runner, keeping the output the POSIX reap read before the kill (a
    Windows reader publishes pipe data only at EOF, so there the capture a survivor
    keeps open comes back empty).
    """
    created: dict[str, Any] = {}
    if os.name == "nt":
        created["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP
    else:
        created["start_new_session"] = True
    process = subprocess.Popen(
        argv, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, **created
    )
    try:
        stdout, stderr = process.communicate(timeout=timeout)
        return process.returncode, stdout, stderr, False
    except subprocess.TimeoutExpired as expired:
        stdout = expired.stdout or b""
        stderr = expired.stderr or b""
        terminate_owned_process_tree(process)
        try:
            stdout, stderr = process.communicate(timeout=PROCESS_TREE_REAP_SECONDS)
        except subprocess.TimeoutExpired as lingering:
            # POSIX `communicate` keeps every window's bytes in one buffer and reports
            # them all from the second timeout, so the capture is assigned, never
            # appended; the first window's value is the fallback for a platform whose
            # timeout carries no partial output (Windows).
            stdout = lingering.stdout or stdout
            stderr = lingering.stderr or stderr
            # A descendant the tree kill missed can still hold the pipe write ends,
            # so the reap never waits on the pipes: only the direct child is killed
            # and waited on, within its own bounded window. POSIX then closes the
            # read ends, because its `communicate` reads on this thread and the close
            # cannot queue behind the survivor, while the finalizer close at return
            # would emit `ResourceWarning: unclosed file`. Windows keeps them open:
            # a close there queues behind the reader thread parked in the pipe, and
            # that thread closes the stream once the survivor releases the write
            # ends, or process exit does.
            try:
                process.kill()
            except OSError:
                pass
            try:
                process.wait(timeout=PROCESS_TREE_REAP_SECONDS)
            except subprocess.TimeoutExpired:
                pass
            if os.name != "nt":
                for stream in (process.stdout, process.stderr):
                    try:
                        stream.close()
                    except OSError:
                        pass
        return None, stdout, stderr, True


def tool_state_relative(gate_dir: pathlib.Path) -> str:
    """The run-relative directory a gate's tool run state is redirected to.

    The path carries the attempt (`tool-state/<attempt>/<gate>`), so a later attempt
    never executes run state written by an earlier one.
    """
    return f"{TOOL_STATE_DIRNAME}/{gate_dir.parent.name}/{gate_dir.name}"


def execute_gate(
    checkout: pathlib.Path, gate: dict[str, Any], gate_dir: pathlib.Path, timeout: float | None,
    run_dir: pathlib.Path,
) -> dict[str, Any]:
    gate_dir.mkdir(parents=True, exist_ok=True)
    cwd = pathlib.Path(checkout, *gate["cwd"].split("/"))
    gate_env = dict(os.environ)
    gate_env.update(gate["env"])
    # Run state a vitest/vite gate would write into the checkout is redirected into
    # the coordinator-owned run store, outside the sealed Subject; the recorded value
    # stays run-relative so a receipt states the redirect without a store path. See the
    # module docstring's tool-run-state paragraph. The directory is composed once, by
    # the same function that records it, so a caller cannot export a different path
    # than the one the result binds.
    tool_state_dir = run_dir / tool_state_relative(gate_dir)
    shutil.rmtree(tool_state_dir, ignore_errors=True)
    gate_env[TOOL_CACHE_ENV] = str(tool_state_dir)
    gate_env[TOOL_CACHE_DISABLE_ENV] = TOOL_CACHE_DISABLED
    stdout_raw = b""
    stderr_raw = b""
    exit_code: int | None = None
    timed_out = False
    error: str | None = None
    if not cwd.is_dir():
        error = f"gate cwd {gate['cwd']!r} is missing in the checkout"
    else:
        try:
            exit_code, stdout_raw, stderr_raw, timed_out = run_owned_command(
                gate["argv"], str(cwd), timeout, gate_env
            )
        except OSError as failure:
            error = f"could not execute gate argv: {failure}"
    # The derived cross-checks below run exactly when the gate command itself ran and
    # exited 0 and the gate's fields derive that cross-check's argv: a derivable
    # `go test -list` needs `go_tests` with `go_package`, a `go` argv and no `/` in a
    # test name (a subtest name has no `-list` spelling), the ExUnit trace run needs
    # `exunit_tests`, the vitest structured report needs `vitest_tests`; a plain
    # exit-0 gate derives none. Keep its error before a reported list failure is
    # appended.
    gate_command_error = error
    write_bytes(gate_dir / "stdout.bin", stdout_raw)
    write_bytes(gate_dir / "stderr.bin", stderr_raw)
    list_argv = go_list_argv(gate)
    list_ran = list_argv is not None and gate_command_error is None and not timed_out and exit_code == 0
    list_exit_code: int | None = None
    list_stdout_raw = b""
    list_stderr_raw = b""
    if list_ran:
        # The event stream alone is not execution-bound: a worker-supplied TestMain can
        # print fake pass frames and exit without running anything. The package binary
        # must also list every named test when asked for it.
        try:
            list_exit_code, list_stdout_raw, list_stderr_raw, _list_timed_out = run_owned_command(
                list_argv, str(cwd), timeout, gate_env
            )
        except OSError as failure:
            raise Refused(f"could not run the Go package test list for gate {gate['id']}: {failure}")
        write_bytes(gate_dir / "go-list.stdout.bin", list_stdout_raw)
        write_bytes(gate_dir / "go-list.stderr.bin", list_stderr_raw)
        list_detail = go_list_detail(gate["go_tests"], list_exit_code, list_stdout_raw)
        if list_detail is not None:
            error = (error + "; " if error else "") + GO_LIST_FAILURE_PREFIX + list_detail
    exunit_argv = exunit_trace_argv(gate)
    exunit_ran = (
        exunit_argv is not None and gate_command_error is None and not timed_out and exit_code == 0
    )
    exunit_exit_code: int | None = None
    exunit_timed_out = False
    exunit_stdout_raw = b""
    exunit_stderr_raw = b""
    exunit_error: str | None = None
    if exunit_ran:
        # A module can print a passing test name and exit before running it, so the
        # derived `mix test --trace` run must finish every named test exactly once,
        # in the same cwd and environment as the gate command.
        try:
            exunit_exit_code, exunit_stdout_raw, exunit_stderr_raw, exunit_timed_out = run_owned_command(
                exunit_argv, str(cwd), timeout, gate_env
            )
        except OSError as failure:
            raise Refused(f"could not run the ExUnit trace for gate {gate['id']}: {failure}")
        write_bytes(gate_dir / "exunit.stdout.bin", exunit_stdout_raw)
        write_bytes(gate_dir / "exunit.stderr.bin", exunit_stderr_raw)
        detail = exunit_trace_detail(gate["exunit_tests"], exunit_exit_code, exunit_stdout_raw)
        if detail is not None:
            exunit_error = EXUNIT_FAILURE_PREFIX + detail
    vitest_argv = vitest_report_argv(gate, gate_dir)
    vitest_ran = (
        vitest_argv is not None and gate_command_error is None and not timed_out and exit_code == 0
    )
    vitest_exit_code: int | None = None
    vitest_timed_out = False
    vitest_stdout_raw = b""
    vitest_stderr_raw = b""
    vitest_report_raw = b""
    vitest_error: str | None = None
    if vitest_ran:
        # A test file can print a passing name and exit before running anything, so the
        # derived run writes vitest's own JSON report and every named test must appear
        # in it exactly once with a passed status.
        try:
            vitest_exit_code, vitest_stdout_raw, vitest_stderr_raw, vitest_timed_out = run_owned_command(
                vitest_argv, str(cwd), timeout, gate_env
            )
        except OSError as failure:
            raise Refused(f"could not run the vitest structured report for gate {gate['id']}: {failure}")
        write_bytes(gate_dir / "vitest.stdout.bin", vitest_stdout_raw)
        write_bytes(gate_dir / "vitest.stderr.bin", vitest_stderr_raw)
        report_path = gate_dir / "vitest-report.json"
        if report_path.is_file():
            vitest_report_raw = read_bytes(report_path, MAX_CAPTURE_BYTES, "derived vitest report")
        write_bytes(report_path, vitest_report_raw)
        detail = vitest_report_detail(gate["vitest_tests"], vitest_exit_code, vitest_report_raw)
        if detail is not None:
            vitest_error = VITEST_FAILURE_PREFIX + detail
    if gate["go_tests"] is not None:
        validator = run_validator(gate_dir, gate, gate_env)
    else:
        validator = None
    passed = (
        error is None
        and not timed_out
        and exit_code == 0
        and (validator is None or validator["exit_code"] == 0)
        and exunit_error is None
        and vitest_error is None
    )
    return {
        "gate_id": gate["id"],
        "cwd": gate["cwd"],
        "argv": gate["argv"],
        "tool_state_dir": tool_state_relative(gate_dir),
        "go_tests": gate["go_tests"],
        "go_package": gate["go_package"],
        "env": gate["env"],
        "exunit_tests": gate["exunit_tests"],
        "exunit_files": gate["exunit_files"],
        "vitest_tests": gate["vitest_tests"],
        "vitest_files": gate["vitest_files"],
        "exit_code": exit_code,
        "timed_out": timed_out,
        "error": error,
        "stdout_sha256": sha256_hex(stdout_raw),
        "stderr_sha256": sha256_hex(stderr_raw),
        "stdout_bytes": len(stdout_raw),
        "stderr_bytes": len(stderr_raw),
        "go_list_argv": list_argv,
        "go_list_exit_code": list_exit_code if list_ran else None,
        "go_list_stdout_sha256": sha256_hex(list_stdout_raw) if list_ran else None,
        "go_list_stderr_sha256": sha256_hex(list_stderr_raw) if list_ran else None,
        "go_list_stdout_bytes": len(list_stdout_raw) if list_ran else None,
        "go_list_stderr_bytes": len(list_stderr_raw) if list_ran else None,
        "exunit_argv": exunit_argv,
        "exunit_exit_code": exunit_exit_code if exunit_ran else None,
        "exunit_timed_out": exunit_timed_out,
        "exunit_stdout_sha256": sha256_hex(exunit_stdout_raw) if exunit_ran else None,
        "exunit_stderr_sha256": sha256_hex(exunit_stderr_raw) if exunit_ran else None,
        "exunit_stdout_bytes": len(exunit_stdout_raw) if exunit_ran else None,
        "exunit_stderr_bytes": len(exunit_stderr_raw) if exunit_ran else None,
        "exunit_error": exunit_error,
        "vitest_argv": vitest_argv,
        "vitest_exit_code": vitest_exit_code if vitest_ran else None,
        "vitest_timed_out": vitest_timed_out,
        "vitest_stdout_sha256": sha256_hex(vitest_stdout_raw) if vitest_ran else None,
        "vitest_stderr_sha256": sha256_hex(vitest_stderr_raw) if vitest_ran else None,
        "vitest_stdout_bytes": len(vitest_stdout_raw) if vitest_ran else None,
        "vitest_stderr_bytes": len(vitest_stderr_raw) if vitest_ran else None,
        "vitest_report_sha256": sha256_hex(vitest_report_raw) if vitest_ran else None,
        "vitest_report_bytes": len(vitest_report_raw) if vitest_ran else None,
        "vitest_error": vitest_error,
        "validator": validator,
        "subject_hash_before": None,
        "subject_hash_after": None,
        "passed": passed,
    }


def recompute_gate(gate_dir: pathlib.Path, gate: dict[str, Any], sealed_hash: str) -> dict[str, Any]:
    result_raw = read_bytes(gate_dir / "result.json", MAX_RESULT_BYTES, "gate result")
    result = strict_json(result_raw, "gate result", MAX_RESULT_BYTES)
    require_object(result, "gate result", RESULT_KEYS, RESULT_KEYS)
    for key, expected in (
        ("gate_id", gate["id"]),
        ("cwd", gate["cwd"]),
        ("argv", gate["argv"]),
        ("go_tests", gate["go_tests"]),
        ("go_package", gate["go_package"]),
        ("env", gate["env"]),
        ("tool_state_dir", tool_state_relative(gate_dir)),
        ("exunit_tests", gate["exunit_tests"]),
        ("exunit_files", gate["exunit_files"]),
        ("vitest_tests", gate["vitest_tests"]),
        ("vitest_files", gate["vitest_files"]),
    ):
        if result[key] != expected:
            raise Refused(f"the stored result for gate {gate['id']} does not match the frozen gate {key}")
    exit_code = result["exit_code"]
    if exit_code is not None and (isinstance(exit_code, bool) or not isinstance(exit_code, int)):
        raise Refused(f"the stored exit code for gate {gate['id']} is invalid")
    timed_out = require_bool(result["timed_out"], f"gate {gate['id']} timed_out")
    error = require_optional_text(result["error"], f"gate {gate['id']} error", max_length=2000)
    list_argv = go_list_argv(gate)
    if result["go_list_argv"] != list_argv:
        raise Refused(f"the stored result for gate {gate['id']} does not match the derived Go test list command")
    exunit_argv = exunit_trace_argv(gate)
    if result["exunit_argv"] != exunit_argv:
        raise Refused(f"the stored result for gate {gate['id']} does not match the derived ExUnit trace command")
    vitest_argv = vitest_report_argv(gate, gate_dir)
    if result["vitest_argv"] != vitest_argv:
        raise Refused(f"the stored result for gate {gate['id']} does not match the derived vitest report command")
    # The stored error conflates the gate command error, a reported list failure and the
    # post-execution "source changed" note, so peel those off before deciding. The note is
    # appended bare when the gate command itself reported no error.
    gate_command_error = error
    if gate_command_error is not None:
        without_note = gate_command_error
        if without_note.endswith(SOURCE_CHANGED_SUFFIX):
            without_note = without_note[: -len(SOURCE_CHANGED_SUFFIX)]
            if without_note.endswith("; "):
                without_note = without_note[:-2]
        if not without_note or without_note.startswith(GO_LIST_FAILURE_PREFIX):
            gate_command_error = None
    list_expected = list_argv is not None and exit_code == 0 and gate_command_error is None and not timed_out
    exunit_expected = exunit_argv is not None and exit_code == 0 and gate_command_error is None and not timed_out
    vitest_expected = vitest_argv is not None and exit_code == 0 and gate_command_error is None and not timed_out
    expected_files = {"result.json", "stdout.bin", "stderr.bin"}
    if gate["go_tests"] is not None:
        expected_files |= {"validator.stdout.bin", "validator.stderr.bin"}
    if list_expected:
        expected_files |= {"go-list.stdout.bin", "go-list.stderr.bin"}
    if exunit_expected:
        expected_files |= {"exunit.stdout.bin", "exunit.stderr.bin"}
    if vitest_expected:
        expected_files |= {"vitest.stdout.bin", "vitest.stderr.bin", "vitest-report.json"}
    actual_files = sorted(entry.name for entry in gate_dir.iterdir())
    if actual_files != sorted(expected_files):
        raise Refused(f"the stored evidence for gate {gate['id']} contains unexpected files")
    subject_bound = (
        result["subject_hash_before"] == sealed_hash and result["subject_hash_after"] == sealed_hash
    )
    stdout_raw = read_bytes(gate_dir / "stdout.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} stdout")
    stderr_raw = read_bytes(gate_dir / "stderr.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} stderr")
    if result["stdout_bytes"] != len(stdout_raw) or result["stdout_sha256"] != sha256_hex(stdout_raw):
        raise Refused(f"the stored stdout for gate {gate['id']} changed after it was captured")
    if result["stderr_bytes"] != len(stderr_raw) or result["stderr_sha256"] != sha256_hex(stderr_raw):
        raise Refused(f"the stored stderr for gate {gate['id']} changed after it was captured")
    passed = subject_bound and error is None and not timed_out and exit_code == 0
    validator_exit: int | None = None
    validator_stdout: str | None = None
    validator_stderr: str | None = None
    if gate["go_tests"] is not None:
        validator = require_object(result["validator"], f"gate {gate['id']} validator", VALIDATOR_KEYS, VALIDATOR_KEYS)
        stored_stdout = read_bytes(gate_dir / "validator.stdout.bin", MAX_CAPTURE_BYTES, "validator stdout")
        stored_stderr = read_bytes(gate_dir / "validator.stderr.bin", MAX_CAPTURE_BYTES, "validator stderr")
        if (
            validator["stdout_bytes"] != len(stored_stdout)
            or validator["stdout_sha256"] != sha256_hex(stored_stdout)
            or validator["stderr_bytes"] != len(stored_stderr)
            or validator["stderr_sha256"] != sha256_hex(stored_stderr)
        ):
            raise Refused(f"the stored Go event validation for gate {gate['id']} changed after it ran")
        gate_env = dict(os.environ)
        gate_env.update(gate["env"])
        try:
            rerun = subprocess.run(validator_argv(gate_dir, gate), capture_output=True, check=False, env=gate_env)
        except OSError as failure:
            raise Refused(f"could not re-run the Go event validator: {failure}")
        if (
            rerun.returncode != validator["exit_code"]
            or sha256_hex(rerun.stdout) != validator["stdout_sha256"]
            or sha256_hex(rerun.stderr) != validator["stderr_sha256"]
        ):
            raise Refused(f"the stored Go event validation for gate {gate['id']} does not reproduce")
        validator_exit = require_integer(validator["exit_code"], f"gate {gate['id']} validator exit_code", -4096)
        passed = passed and validator_exit == 0
        validator_stdout = validator["stdout_sha256"]
        validator_stderr = validator["stderr_sha256"]
    elif result["validator"] is not None:
        raise Refused(f"gate {gate['id']} stores Go event validation without go_tests")
    if list_expected:
        list_exit_code = require_optional_integer(
            result["go_list_exit_code"], f"gate {gate['id']} go_list_exit_code", -4096
        )
        list_stdout_raw = read_bytes(
            gate_dir / "go-list.stdout.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} Go test list stdout"
        )
        list_stderr_raw = read_bytes(
            gate_dir / "go-list.stderr.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} Go test list stderr"
        )
        if (
            result["go_list_stdout_sha256"] is None
            or result["go_list_stdout_bytes"] is None
            or result["go_list_stderr_sha256"] is None
            or result["go_list_stderr_bytes"] is None
        ):
            raise Refused(f"gate {gate['id']} does not record the executed Go test list output")
        if (
            require_digest(result["go_list_stdout_sha256"], f"gate {gate['id']} go_list_stdout_sha256")
            != sha256_hex(list_stdout_raw)
            or require_integer(result["go_list_stdout_bytes"], f"gate {gate['id']} go_list_stdout_bytes")
            != len(list_stdout_raw)
            or require_digest(result["go_list_stderr_sha256"], f"gate {gate['id']} go_list_stderr_sha256")
            != sha256_hex(list_stderr_raw)
            or require_integer(result["go_list_stderr_bytes"], f"gate {gate['id']} go_list_stderr_bytes")
            != len(list_stderr_raw)
        ):
            raise Refused(f"the stored Go test list output for gate {gate['id']} changed after it was captured")
        if go_list_detail(gate["go_tests"], list_exit_code, list_stdout_raw) is not None:
            passed = False
    else:
        for key in (
            "go_list_exit_code",
            "go_list_stdout_sha256",
            "go_list_stderr_sha256",
            "go_list_stdout_bytes",
            "go_list_stderr_bytes",
        ):
            if result[key] is not None:
                raise Refused(f"gate {gate['id']} records Go test list evidence without an executed list run")
    exunit_error: str | None = None
    exunit_exit_code: int | None = None
    if exunit_expected:
        exunit_stdout_raw = read_bytes(
            gate_dir / "exunit.stdout.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} ExUnit trace stdout"
        )
        exunit_stderr_raw = read_bytes(
            gate_dir / "exunit.stderr.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} ExUnit trace stderr"
        )
        if (
            result["exunit_stdout_sha256"] is None
            or result["exunit_stdout_bytes"] is None
            or result["exunit_stderr_sha256"] is None
            or result["exunit_stderr_bytes"] is None
        ):
            raise Refused(f"gate {gate['id']} does not record the executed ExUnit trace output")
        if (
            require_digest(result["exunit_stdout_sha256"], f"gate {gate['id']} exunit_stdout_sha256")
            != sha256_hex(exunit_stdout_raw)
            or require_integer(result["exunit_stdout_bytes"], f"gate {gate['id']} exunit_stdout_bytes")
            != len(exunit_stdout_raw)
            or require_digest(result["exunit_stderr_sha256"], f"gate {gate['id']} exunit_stderr_sha256")
            != sha256_hex(exunit_stderr_raw)
            or require_integer(result["exunit_stderr_bytes"], f"gate {gate['id']} exunit_stderr_bytes")
            != len(exunit_stderr_raw)
        ):
            raise Refused(f"the stored ExUnit trace output for gate {gate['id']} changed after it was captured")
        exunit_exit_code = require_optional_integer(
            result["exunit_exit_code"], f"gate {gate['id']} exunit_exit_code", -4096
        )
        exunit_timed_out = require_bool(result["exunit_timed_out"], f"gate {gate['id']} exunit_timed_out")
        if exunit_timed_out != (exunit_exit_code is None):
            raise Refused(f"the stored ExUnit trace run for gate {gate['id']} disagrees with its exit code")
        detail = exunit_trace_detail(gate["exunit_tests"], exunit_exit_code, exunit_stdout_raw)
        exunit_error = None if detail is None else EXUNIT_FAILURE_PREFIX + detail
        passed = passed and exunit_error is None
    else:
        for key in (
            "exunit_exit_code",
            "exunit_stdout_sha256",
            "exunit_stderr_sha256",
            "exunit_stdout_bytes",
            "exunit_stderr_bytes",
            "exunit_error",
        ):
            if result[key] is not None:
                raise Refused(f"gate {gate['id']} records ExUnit trace evidence without an executed trace run")
        if result["exunit_timed_out"] is not False:
            raise Refused(f"gate {gate['id']} records ExUnit trace evidence without an executed trace run")
    vitest_error: str | None = None
    vitest_exit_code: int | None = None
    if vitest_expected:
        vitest_stdout_raw = read_bytes(
            gate_dir / "vitest.stdout.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} vitest report stdout"
        )
        vitest_stderr_raw = read_bytes(
            gate_dir / "vitest.stderr.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} vitest report stderr"
        )
        vitest_report_raw = read_bytes(
            gate_dir / "vitest-report.json", MAX_CAPTURE_BYTES, f"gate {gate['id']} vitest report"
        )
        if (
            result["vitest_stdout_sha256"] is None
            or result["vitest_stdout_bytes"] is None
            or result["vitest_stderr_sha256"] is None
            or result["vitest_stderr_bytes"] is None
            or result["vitest_report_sha256"] is None
            or result["vitest_report_bytes"] is None
        ):
            raise Refused(f"gate {gate['id']} does not record the executed vitest report output")
        if (
            require_digest(result["vitest_stdout_sha256"], f"gate {gate['id']} vitest_stdout_sha256")
            != sha256_hex(vitest_stdout_raw)
            or require_integer(result["vitest_stdout_bytes"], f"gate {gate['id']} vitest_stdout_bytes")
            != len(vitest_stdout_raw)
            or require_digest(result["vitest_stderr_sha256"], f"gate {gate['id']} vitest_stderr_sha256")
            != sha256_hex(vitest_stderr_raw)
            or require_integer(result["vitest_stderr_bytes"], f"gate {gate['id']} vitest_stderr_bytes")
            != len(vitest_stderr_raw)
            or require_digest(result["vitest_report_sha256"], f"gate {gate['id']} vitest_report_sha256")
            != sha256_hex(vitest_report_raw)
            or require_integer(result["vitest_report_bytes"], f"gate {gate['id']} vitest_report_bytes")
            != len(vitest_report_raw)
        ):
            raise Refused(f"the stored vitest report for gate {gate['id']} changed after it was captured")
        vitest_exit_code = require_optional_integer(
            result["vitest_exit_code"], f"gate {gate['id']} vitest_exit_code", -4096
        )
        vitest_timed_out = require_bool(result["vitest_timed_out"], f"gate {gate['id']} vitest_timed_out")
        if vitest_timed_out != (vitest_exit_code is None):
            raise Refused(f"the stored vitest report run for gate {gate['id']} disagrees with its exit code")
        detail = vitest_report_detail(gate["vitest_tests"], vitest_exit_code, vitest_report_raw)
        vitest_error = None if detail is None else VITEST_FAILURE_PREFIX + detail
        passed = passed and vitest_error is None
    else:
        for key in (
            "vitest_exit_code",
            "vitest_stdout_sha256",
            "vitest_stderr_sha256",
            "vitest_stdout_bytes",
            "vitest_stderr_bytes",
            "vitest_report_sha256",
            "vitest_report_bytes",
            "vitest_error",
        ):
            if result[key] is not None:
                raise Refused(f"gate {gate['id']} records vitest report evidence without an executed report run")
        if result["vitest_timed_out"] is not False:
            raise Refused(f"gate {gate['id']} records vitest report evidence without an executed report run")
    if result["passed"] is not passed:
        raise Refused(f"the stored result for gate {gate['id']} claims passed={result['passed']} but its evidence says {passed}")
    return {
        "id": gate["id"],
        "cwd": gate["cwd"],
        "argv": gate["argv"],
        "tool_state_dir": tool_state_relative(gate_dir),
        "go_tests": gate["go_tests"],
        "go_package": gate["go_package"],
        "env": gate["env"],
        "exunit_tests": gate["exunit_tests"],
        "exunit_files": gate["exunit_files"],
        "vitest_tests": gate["vitest_tests"],
        "vitest_files": gate["vitest_files"],
        "exit_code": exit_code,
        "timed_out": timed_out,
        "error": error,
        "exunit_exit_code": exunit_exit_code,
        "exunit_error": exunit_error,
        "vitest_exit_code": vitest_exit_code,
        "vitest_error": vitest_error,
        "subject_bound": subject_bound,
        "passed": passed,
        "stdout_sha256": result["stdout_sha256"],
        "stderr_sha256": result["stderr_sha256"],
        "validator_exit_code": validator_exit,
        "validator_stdout_sha256": validator_stdout,
        "validator_stderr_sha256": validator_stderr,
        "result_sha256": sha256_hex(result_raw),
    }


def attempt_names(run_dir: pathlib.Path) -> list[str]:
    checks = run_dir / "checks"
    if not checks.exists():
        return []
    if not checks.is_dir():
        raise Refused("the checks path exists but is not a directory")
    names: list[str] = []
    for entry in sorted(checks.iterdir(), key=lambda item: item.name):
        if not entry.is_dir() or not ATTEMPT_PATTERN.fullmatch(entry.name):
            raise Refused(f"unexpected entry in the checks directory: {entry.name}")
        names.append(entry.name)
    return sorted(names, key=lambda name: int(name.split("-", 1)[1]))


def read_attempt(run_dir: pathlib.Path, name: str) -> tuple[bytes, dict[str, Any]]:
    raw = read_bytes(run_dir / "checks" / name / "attempt.json", MAX_RECORD_BYTES, "check attempt record")
    value = strict_json(raw, "check attempt record", MAX_RECORD_BYTES)
    require_object(value, "check attempt record", ATTEMPT_KEYS, REQUIRED_ATTEMPT_KEYS)
    if value["schema"] != SCHEMA:
        raise Refused("check attempt record schema mismatch")
    if value["attempt_id"] != name:
        raise Refused("check attempt record does not match its directory name")
    entries_value = value["gates"]
    if not isinstance(entries_value, list):
        raise Refused("check attempt record gates must be a list")
    entries: list[dict[str, Any]] = []
    identifiers: set[str] = set()
    for index, entry in enumerate(entries_value):
        source = f"check attempt gate #{index + 1}"
        item = require_object(entry, source, ATTEMPT_ENTRY_KEYS, ATTEMPT_ENTRY_KEYS)
        gate_id = require_identifier(item["id"], source + " id")
        if gate_id in identifiers:
            raise Refused("check attempt record lists a gate twice")
        identifiers.add(gate_id)
        entries.append({
            "id": gate_id,
            "passed": require_bool(item["passed"], source + " passed"),
            "result_sha256": require_digest(item["result_sha256"], source + " result_sha256"),
            "stdout_sha256": require_digest(item["stdout_sha256"], source + " stdout_sha256"),
            "stderr_sha256": require_digest(item["stderr_sha256"], source + " stderr_sha256"),
            "validator_exit_code": require_optional_integer(item["validator_exit_code"], source + " validator_exit_code", -4096),
            "validator_stdout_sha256": require_optional_digest(item["validator_stdout_sha256"], source + " validator_stdout_sha256"),
            "validator_stderr_sha256": require_optional_digest(item["validator_stderr_sha256"], source + " validator_stderr_sha256"),
        })
    return raw, {
        "schema": SCHEMA,
        "attempt_id": name,
        "task_id": require_identifier(value["task_id"], "check attempt record task_id"),
        "task_sha256": require_digest(value["task_sha256"], "check attempt record task_sha256"),
        "routes_sha256": require_digest(value["routes_sha256"], "check attempt record routes_sha256"),
        "subject_hash": require_digest(value["subject_hash"], "check attempt record subject_hash"),
        "complete": require_bool(value["complete"], "check attempt record complete"),
        "passed": require_bool(value["passed"], "check attempt record passed"),
        "dependency_provisioning": validate_attempt_provisioning(
            value.get("dependency_provisioning"), "check attempt record dependency_provisioning"
        ),
        "gates": entries,
    }


def verify_attempt_artifacts(
    run_dir: pathlib.Path, task: dict[str, Any], sealed_hash: str, run: dict[str, Any],
    name: str, raw: bytes, attempt: dict[str, Any],
) -> dict[str, Any]:
    if attempt["task_sha256"] != run["task_sha256"] or attempt["routes_sha256"] != run["routes_sha256"]:
        raise Refused("the check attempt is not bound to the frozen task and routes of this run")
    # The attempt attests only what it executed under: the run's bound provisioning
    # step must be exactly the one the attempt saw, so a tree bound after the attempt
    # ran (a late `bind-dependencies`) can never be attested by it and instead
    # requires a new check.
    provisioning = run["environment"].get("provisioning")
    bound_view = None if provisioning is None else provisioning_view(provisioning)
    if attempt["dependency_provisioning"] != bound_view:
        raise Refused(
            f"the check attempt {name} does not match the dependency provisioning this run binds: "
            "a tree bound after the attempt ran requires a new check"
        )
    attempt_dir = run_dir / "checks" / name
    gate_lookup = {gate["id"]: gate for gate in task["gates"]}
    known_order = [gate["id"] for gate in task["gates"]]
    entry_ids = [entry["id"] for entry in attempt["gates"]]
    unknown = sorted(set(entry_ids) - set(known_order))
    if unknown:
        raise Refused("the check attempt records gates outside the frozen task: " + ", ".join(unknown))
    if entry_ids != [gate_id for gate_id in known_order if gate_id in set(entry_ids)]:
        raise Refused("the check attempt records gates out of frozen task order")
    listed = sorted(
        entry.name for entry in attempt_dir.iterdir() if entry.name != "attempt.json"
    )
    if listed != sorted(entry_ids):
        raise Refused("the check attempt directory contents do not match its record")
    verified = []
    for entry in attempt["gates"]:
        recomputed = recompute_gate(attempt_dir / entry["id"], gate_lookup[entry["id"]], sealed_hash)
        if entry["result_sha256"] != recomputed["result_sha256"]:
            raise Refused(f"the check attempt record digest for gate {entry['id']} does not match its result")
        for key in ("passed", "stdout_sha256", "stderr_sha256", "validator_exit_code",
                    "validator_stdout_sha256", "validator_stderr_sha256"):
            if entry[key] != recomputed[key]:
                raise Refused(f"the check attempt record for gate {entry['id']} disagrees with its evidence on {key}")
        verified.append(recomputed)
    complete = set(entry_ids) == set(known_order) and all(gate["subject_bound"] for gate in verified)
    if attempt["complete"] is not complete:
        raise Refused("the check attempt completeness claim does not match its gate record")
    passed = complete and all(item["passed"] for item in verified)
    if attempt["passed"] is not passed:
        raise Refused("the check attempt result claim does not match its gate evidence")
    return {
        "attempt_id": name,
        "sha256": sha256_hex(raw),
        "task_sha256": attempt["task_sha256"],
        "routes_sha256": attempt["routes_sha256"],
        "dependency_provisioning": attempt["dependency_provisioning"],
        "complete": complete,
        "passed": passed,
        "gates": verified,
    }


def inspect_attempt(
    run_dir: pathlib.Path, task: dict[str, Any], sealed_hash: str, run: dict[str, Any]
) -> dict[str, Any] | None:
    selected: tuple[str, bytes, dict[str, Any]] | None = None
    for name in attempt_names(run_dir):
        raw, attempt = read_attempt(run_dir, name)
        if attempt["task_id"] == task["task_id"] and attempt["subject_hash"] == sealed_hash:
            selected = (name, raw, attempt)
    if selected is None:
        return None
    return verify_attempt_artifacts(run_dir, task, sealed_hash, run, *selected)


def require_passing_attempt(
    run_dir: pathlib.Path, task: dict[str, Any], sealed_hash: str, run: dict[str, Any]
) -> dict[str, Any]:
    attempt = inspect_attempt(run_dir, task, sealed_hash, run)
    if attempt is None:
        raise Refused("no check attempt is bound to the sealed subject; run check first")
    if not attempt["complete"]:
        raise Refused(f"check attempt {attempt['attempt_id']} is incomplete: {INCOMPLETE_CHECK_DETAIL}")
    if not attempt["passed"]:
        failed = ", ".join(item["id"] for item in attempt["gates"] if not item["passed"])
        raise Refused(f"check attempt {attempt['attempt_id']} did not pass its gates: {failed}")
    return attempt


# ---------------------------------------------------------------------------
# Usage rows and receipt derivation


def validate_model_ids(value: Any, source: str) -> list[dict[str, Any]]:
    if not isinstance(value, list):
        raise Refused(f"{source} must be a list of served model identities")
    identities: list[dict[str, Any]] = []
    seen: set[tuple[str, str, str]] = set()
    for index, item in enumerate(value):
        item_source = f"{source} entry #{index + 1}"
        if not isinstance(item, dict):
            raise Refused(f"{item_source} must be a served model identity object")
        entry = require_object(item, item_source, MODEL_ID_KEYS, MODEL_ID_KEYS)
        identity = {
            "model": require_text(entry["model"], item_source + " model", max_length=300),
            "provider": require_text(entry["provider"], item_source + " provider", max_length=300),
            "api": require_text(entry["api"], item_source + " api", max_length=300),
            "assistant_messages": require_integer(entry["assistant_messages"], item_source + " assistant_messages"),
        }
        key = (identity["model"], identity["provider"], identity["api"])
        if key in seen:
            raise Refused(f"{source} repeats a model identity")
        seen.add(key)
        identities.append(identity)
    return identities


def validate_prompt_hashes(value: Any, source: str) -> list[dict[str, Any]]:
    if not isinstance(value, list):
        raise Refused(f"{source} must be a list of prompt hash records")
    records: list[dict[str, Any]] = []
    seen: set[tuple[str, str, int | None]] = set()
    for index, item in enumerate(value):
        item_source = f"{source} entry #{index + 1}"
        entry = require_object(item, item_source, PROMPT_HASH_KEYS, PROMPT_HASH_REQUIRED_KEYS)
        sha256 = entry["sha256"]
        if not isinstance(sha256, str) or not SHA256_FIELD_PATTERN.fullmatch(sha256):
            raise Refused(f"{item_source} sha256 must be a sha256 hex digest")
        record = {"kind": require_text(entry["kind"], item_source + " kind", max_length=200), "sha256": sha256}
        if "index" in entry:
            record["index"] = require_integer(entry["index"], item_source + " index")
        key = (record["kind"], sha256, record.get("index"))
        if key in seen:
            raise Refused(f"{source} repeats a prompt hash")
        seen.add(key)
        records.append(record)
    return records


def validate_known_subtotal_entry(entry: Any, source: str) -> dict[str, int | float]:
    record = require_object(entry, source, KNOWN_SUBTOTAL_ENTRY_KEYS, KNOWN_SUBTOTAL_ENTRY_KEYS)
    return {
        "sum": require_number(record["sum"], source + " sum"),
        "messages": require_integer(record["messages"], source + " messages", minimum=1),
    }


def validate_known_subtotals(value: Any, source: str) -> dict[str, Any]:
    """Validate one phase's observed partial-session subtotals.

    The receipt includes these validated sums in its known totals, so the shape and
    numbers are checked here instead of being trusted as opaque metadata.
    """
    if not isinstance(value, dict):
        raise Refused(f"{source} must be a JSON object")
    subtotals: dict[str, Any] = {}
    for key in sorted(value):
        entry = value[key]
        if key in KNOWN_SUBTOTAL_MESSAGE_KEYS:
            subtotals[key] = require_integer(entry, f"{source} {key}")
        elif key == "cost":
            block = require_object(entry, f"{source} cost", set(KNOWN_SUBTOTAL_COST_FIELDS), set())
            subtotals["cost"] = {
                field: validate_known_subtotal_entry(block[field], f"{source} cost.{field}")
                for field in sorted(block)
            }
        elif key in KNOWN_SUBTOTAL_TOKEN_FIELDS:
            subtotals[key] = validate_known_subtotal_entry(entry, f"{source} {key}")
        else:
            raise Refused(f"{source} has an unexpected subtotal key: {key}")
    return subtotals


def validate_phase_extra(name: str, value: Any, source: str) -> Any:
    if name == "limitations":
        return require_text_list(value, source, minimum=0, unique=True, max_length=1000)
    if name == "known_subtotals":
        return validate_known_subtotals(value, source)
    if not isinstance(value, dict):
        raise Refused(f"{source} must be a JSON object")
    return value


def validate_usage(raw: bytes) -> tuple[list[dict[str, Any]], int, float | int]:
    value = strict_json(raw, "usage JSON", MAX_USAGE_BYTES)
    scan_no_secret_keys(value, "usage JSON")
    require_object(value, "usage JSON", USAGE_KEYS, USAGE_KEYS)
    repair_count = require_integer(value["repair_count"], "usage JSON repair_count")
    task_wall_time = require_number(value["wall_time"], "usage JSON wall_time")
    phases = value["phases"]
    if not isinstance(phases, list) or not phases:
        raise Refused("usage JSON phases must be a non-empty list")
    rows: list[dict[str, Any]] = []
    sessions: set[str] = set()
    for index, phase in enumerate(phases):
        source = f"usage phase #{index + 1}"
        require_object(phase, source, PHASE_KEYS | PHASE_OPTIONAL_KEYS, PHASE_KEYS)
        stage = require_value_in(phase["stage"], source + " stage", STAGES)
        session_ref = require_optional_text(phase["session_ref"], source + " session_ref", max_length=300)
        non_model_reason = require_optional_text(phase["non_model_reason"], source + " non_model_reason", max_length=1000)
        if (session_ref is None) == (non_model_reason is None):
            raise Refused(f"{source} requires exactly one of session_ref or non_model_reason")
        model_ids = validate_model_ids(phase["model_ids"], source + " model_ids")
        prompt_hashes = validate_prompt_hashes(phase["prompt_hashes"], source + " prompt_hashes")
        tokens_block = require_object(phase["tokens"], source + " tokens", TOKEN_KEYS, TOKEN_KEYS)
        token_values = {
            key: require_optional_integer(tokens_block[key], f"{source} tokens.{key}")
            for key in ("input", "output", "cached")
        }
        reported_cost = require_optional_number(phase["reported_cost"], source + " reported_cost")
        estimated_cost = require_optional_number(phase["estimated_cost"], source + " estimated_cost")
        cost_provenance = require_optional_text(phase["cost_provenance"], source + " cost_provenance", max_length=300)
        price_version = require_optional_text(phase["price_version"], source + " price_version", max_length=200)
        wall_time = require_number(phase["wall_time"], source + " wall_time")
        if session_ref is None:
            if model_ids or prompt_hashes:
                raise Refused(f"{source} must not list model ids or prompt hashes without a session_ref")
            if any(item is not None for item in token_values.values()):
                raise Refused(f"{source} must not report token usage without a session_ref")
            if reported_cost is not None or estimated_cost is not None:
                raise Refused(f"{source} must not report cost without a session_ref")
            if price_version is not None:
                raise Refused(f"{source} must not report a price version without a session_ref")
            if "known_subtotals" in phase:
                raise Refused(f"{source} must not report known subtotals without a session_ref")
        elif not model_ids:
            raise Refused(f"{source} must list the served model ids for its session")
        elif not prompt_hashes:
            raise Refused(f"{source} must include an observed prompt hash for its model session")
        if session_ref is not None:
            if session_ref in sessions:
                raise Refused(f"{source} duplicates a model session; duplicate model session costs cannot be counted twice")
            sessions.add(session_ref)
        row: dict[str, Any] = {
            "stage": stage,
            "session_ref": session_ref,
            "non_model_reason": non_model_reason,
            "model_ids": model_ids,
            "prompt_hashes": prompt_hashes,
            "tokens": {key: token_values[key] for key in ("input", "output", "cached")},
            "reported_cost": reported_cost,
            "estimated_cost": estimated_cost,
            "cost_provenance": cost_provenance,
            "price_version": price_version,
            "wall_time": wall_time,
        }
        for optional in sorted(PHASE_OPTIONAL_KEYS):
            if optional in phase:
                row[optional] = validate_phase_extra(optional, phase[optional], f"{source} {optional}")
        known_subtotals = row.get("known_subtotals")
        if known_subtotals:
            # A validated known subtotal feeds the receipt, so a complete session total
            # and its observed subtotal must agree instead of surviving as two versions
            # of the same number.
            for field in ("input", "output", "cached"):
                complete = token_values[field]
                partial = known_subtotals.get(field)
                if complete is not None and partial is not None and partial["sum"] != complete:
                    raise Refused(
                        f"{source} known {field} subtotal {partial['sum']!r} disagrees with the "
                        f"complete session total {complete!r}"
                    )
            partial_cost = known_subtotals.get("cost", {}).get("total")
            if estimated_cost is not None and partial_cost is not None and partial_cost["sum"] != estimated_cost:
                raise Refused(
                    f"{source} known cost.total subtotal {partial_cost['sum']!r} disagrees with the "
                    f"complete session cost {estimated_cost!r}"
                )
        rows.append(row)
    missing = sorted(set(STAGES) - {row["stage"] for row in rows})
    if missing:
        raise Refused("usage JSON must cover every evaluation stage; missing: " + ", ".join(missing))
    return rows, repair_count, task_wall_time


def validate_usage_against_review(rows: list[dict[str, Any]], review: dict[str, Any] | None) -> None:
    if review is None:
        return
    reviewer_session = review["reviewer"]["session_ref"]
    reviewer_model = review["reviewer"]["model_id"]
    review_rows = [row for row in rows if row["stage"] == "review"]
    if not any(
        row["session_ref"] == reviewer_session
        and any(identity["model"] == reviewer_model for identity in row["model_ids"])
        for row in review_rows
    ):
        raise Refused("usage review phases do not include the independent review session and reviewer model")
    for row in rows:
        if row["stage"] != "review" and row["session_ref"] == reviewer_session:
            raise Refused("the reviewer session also appears in a worker phase; the review is not independent")
    worker_sessions = set(review["worker"]["session_refs"])
    observed_workers = {
        row["session_ref"]
        for row in rows
        if row["stage"] != "review"
        and any(identity["model"] == review["worker"]["model_id"] for identity in row["model_ids"])
    }
    if not worker_sessions.issubset(observed_workers):
        raise Refused("usage phases omit an implementing worker session required by the review")


def build_receipt(
    run: dict[str, Any],
    task: dict[str, Any],
    routes: dict[str, dict[str, str]],
    sealed: dict[str, Any],
    review: dict[str, Any] | None,
    attempt: dict[str, Any] | None,
    rows: list[dict[str, Any]],
    blockers: list[str],
    repair_count: int,
    wall_time: float | int,
) -> dict[str, Any]:
    model_rows = [row for row in rows if row["session_ref"] is not None]
    token_fields = ("input", "output", "cached")

    def known_subtotal(
        value_of: Callable[[dict[str, Any]], Any],
        partial_of: Callable[[dict[str, Any]], dict[str, Any] | None],
        source: str,
    ) -> int | float:
        # A complete row contributes its session total; a row whose session total is
        # unknown contributes its validated partial-session observation, if any, so
        # observed usage from a mixed session never collapses to zero.
        contributions: list[int | float] = []
        for row in model_rows:
            value = value_of(row)
            if value is not None:
                contributions.append(value)
                continue
            partial = partial_of(row)
            if partial is not None:
                contributions.append(partial["sum"])
        return number_sum(contributions, source)

    def known_token_total(field: str) -> int | float:
        return known_subtotal(
            lambda row: row["tokens"][field],
            lambda row: (row.get("known_subtotals") or {}).get(field),
            f"the known {field} token subtotal",
        )

    tokens: dict[str, int | None] = {}
    for field in token_fields:
        values = [row["tokens"][field] for row in model_rows]
        tokens[field] = (
            None if not values or any(value is None for value in values)
            else number_sum(values, f"the {field} token total")
        )
        tokens["known_" + field + "_subtotal"] = known_token_total(field)
    unknown_token_rows = sum(
        1 for row in model_rows if any(row["tokens"][field] is None for field in token_fields)
    )
    reported_values = [row["reported_cost"] for row in model_rows]
    estimated_values = [row["estimated_cost"] for row in model_rows]
    reported_total = (
        None if not model_rows or any(value is None for value in reported_values)
        else number_sum(reported_values, "the reported cost total")
    )
    estimated_total = (
        None if not model_rows or any(value is None for value in estimated_values)
        else number_sum(estimated_values, "the estimated cost total")
    )
    # Only observed provider-reported costs feed the reported channel; the extractor's
    # validated cost subtotals are harness estimates, so they feed the estimated channel
    # (never provider billing) when a mixed session has no complete cost total.
    known_reported_cost = number_sum(
        (value for value in reported_values if value is not None), "the known reported cost subtotal"
    )
    known_estimated_cost: int | float = known_subtotal(
        lambda row: row["estimated_cost"],
        lambda row: (row.get("known_subtotals") or {}).get("cost", {}).get("total"),
        "the known estimated cost subtotal",
    )
    unknown_reported_rows = sum(1 for row in model_rows if row["reported_cost"] is None)
    unknown_estimated_rows = sum(1 for row in model_rows if row["estimated_cost"] is None)
    unknown_price_rows = sum(1 for row in model_rows if row["price_version"] is None)
    unknown_provenance_rows = sum(1 for row in model_rows if row["cost_provenance"] is None)
    reasons: list[str] = []
    if not model_rows:
        reasons.append("no model usage evidence was recorded")
    if unknown_token_rows:
        reasons.append(f"unknown token usage in {unknown_token_rows} model phase(s)")
    if unknown_provenance_rows:
        reasons.append(f"missing cost provenance in {unknown_provenance_rows} model phase(s)")
    if unknown_price_rows:
        reasons.append(f"missing price version in {unknown_price_rows} model phase(s)")
    if unknown_reported_rows:
        reasons.append(f"no provider-reported cost in {unknown_reported_rows} model phase(s)")
    if unknown_estimated_rows:
        reasons.append(f"unknown estimated cost in {unknown_estimated_rows} model phase(s)")
    findings: list[dict[str, str]] = []
    reviewed_explanations = None
    review_block = None
    if review is not None:
        findings = sorted(review["findings"], key=lambda item: item["id"])
        reviewed_explanations = {key: review["explanations"][key] for key in sorted(review["explanations"])}
        review_block = {
            "verdict": review["verdict"],
            "gate_weakening": review["gate_weakening"],
            "model_id": review["reviewer"]["model_id"],
            "session_ref": review["reviewer"]["session_ref"],
            "routes_sha256": review["routes_sha256"],
            "subject_hash": review["subject_hash"],
            "check_attempt_id": review["check_attempt_id"],
            "evidence_sha256": run.get("review_sha256"),
        }
    checks = [
        {
            "id": gate["id"],
            "cwd": gate["cwd"],
            "argv": gate["argv"],
            "tool_state_dir": gate["tool_state_dir"],
            "exit_code": gate["exit_code"],
            "timed_out": gate["timed_out"],
            "passed": gate["passed"],
            "go_tests": gate["go_tests"],
            "go_package": gate["go_package"],
            "validator_exit_code": gate["validator_exit_code"],
            "env": gate["env"],
            "exunit_tests": gate["exunit_tests"],
            "exunit_files": gate["exunit_files"],
            "exunit_exit_code": gate["exunit_exit_code"],
            "exunit_error": gate["exunit_error"],
            "vitest_tests": gate["vitest_tests"],
            "vitest_files": gate["vitest_files"],
            "vitest_exit_code": gate["vitest_exit_code"],
            "vitest_error": gate["vitest_error"],
        }
        for gate in (attempt["gates"] if attempt is not None else [])
    ]
    check_attempt = None
    if attempt is not None:
        check_attempt = {
            "attempt_id": attempt["attempt_id"],
            "attempt_sha256": attempt["sha256"],
            "task_sha256": attempt["task_sha256"],
            "routes_sha256": attempt["routes_sha256"],
            "subject_hash": sealed["subject_hash"],
            "dependency_provisioning": attempt["dependency_provisioning"],
            "complete": attempt["complete"],
            "passed": attempt["passed"],
            "gates": [
                {
                    "id": gate["id"],
                    "passed": gate["passed"],
                    "result_sha256": gate["result_sha256"],
                    "stdout_sha256": gate["stdout_sha256"],
                    "stderr_sha256": gate["stderr_sha256"],
                    "validator_exit_code": gate["validator_exit_code"],
                    "validator_stdout_sha256": gate["validator_stdout_sha256"],
                    "validator_stderr_sha256": gate["validator_stderr_sha256"],
                }
                for gate in attempt["gates"]
            ],
        }
    return {
        "schema": SCHEMA,
        "task_id": task["task_id"],
        "base": sealed["base"],
        "head": sealed["head"],
        "language": task["language"],
        "risk_class": task["risk_class"],
        "workflow": task["workflow"],
        "outcome": task["outcome"],
        "explanations": list(task["explanations"]),
        "reviewed_explanations": reviewed_explanations,
        "routes": {"worker": dict(routes["worker"]), "reviewer": dict(routes["reviewer"])},
        "model_ids": [
            {"model": model, "provider": provider, "api": api}
            for model, provider, api in sorted({
                (identity["model"], identity["provider"], identity["api"])
                for row in rows
                for identity in row["model_ids"]
            })
        ],
        "prompt_hashes": [
            {"kind": kind, "sha256": sha256}
            for kind, sha256 in sorted({
                (record["kind"], record["sha256"])
                for row in rows
                for record in row["prompt_hashes"]
            })
        ],
        "tokens": tokens,
        "cost": {
            "reported_cost": reported_total,
            "estimated_cost": estimated_total,
            "known_reported_cost_subtotal": known_reported_cost,
            "known_estimated_cost_subtotal": known_estimated_cost,
            "price_versions": sorted({row["price_version"] for row in model_rows if row["price_version"] is not None}),
            "accounting_complete": not reasons,
            "incomplete_reasons": reasons,
        },
        "wall_time": wall_time,
        "review_time": number_sum(
            (row["wall_time"] for row in rows if row["stage"] == "review"), "the review time total"
        ),
        "repair_count": repair_count,
        "accepted": not blockers,
        "acceptance_blockers": blockers,
        "reviewer_findings": findings,
        "later_defects": [],
        "environment": run["environment"],
        "subject": {
            "base": sealed["base"],
            "head": sealed["head"],
            "manifest_digest": sealed["manifest_digest"],
            "content_revision": sealed["content_revision"],
            "subject_hash": sealed["subject_hash"],
            "changed_paths": [dict(change) for change in sealed["changed_paths"]],
        },
        "subject_hash": sealed["subject_hash"],
        "review": review_block,
        "checks": checks,
        "evidence": {
            # SANITIZE_SCRIPT executes nothing here, but its `HOST_PATH_PATTERNS`
            # render recorded measured values (`redact_host_paths`), so a receipt
            # binds the pattern set that produced them too.
            "tooling_sources": {
                path.name: sha256_hex(read_bytes(path, MAX_RECORD_BYTES, "evaluation tooling source"))
                for path in (
                    pathlib.Path(__file__).resolve(), VALIDATOR_SCRIPT, SOURCE_SUBJECT_SCRIPT,
                    SANITIZE_SCRIPT,
                )
            },
            "frozen_task_sha256": run["task_sha256"],
            "frozen_routes_sha256": run["routes_sha256"],
            "frozen_review_sha256": run.get("review_sha256"),
            "frozen_usage_sha256": run["usage_sha256"],
            "check_attempt": check_attempt,
        },
        "phases": rows,
    }


def derive_receipt(run_dir: pathlib.Path, candidate_usage: bytes | None = None) -> dict[str, Any]:
    """Derive the receipt for the frozen usage rows or for one unevaluated candidate.

    A candidate row set is validated and derived before anything is published, so a
    refused import leaves the previously frozen usage rows, run record and receipt
    untouched and still verifiable.
    """
    run = load_run(run_dir)
    task = load_frozen_task(run_dir, run)
    require_dependency_provisioning(run_dir, run, task)
    require_recorded_redaction(run)
    routes = load_frozen_routes(run_dir, run)
    sealed = load_sealed(run_dir, task)
    require_current_subject(run_dir, task, sealed)
    attempt = inspect_attempt(run_dir, task, sealed["subject_hash"], run)
    blockers: list[str] = []
    if attempt is None:
        blockers.append("no check attempt is bound to the sealed subject")
    else:
        if not attempt["complete"]:
            blockers.append(f"check attempt {attempt['attempt_id']} is incomplete: {INCOMPLETE_CHECK_DETAIL}")
        for gate in attempt["gates"]:
            if not gate["passed"]:
                if not gate["subject_bound"]:
                    detail = "the result is not bound to the sealed subject (source changed during execution)"
                elif gate["timed_out"]:
                    detail = "the gate timed out"
                elif gate["validator_exit_code"] is not None and gate["validator_exit_code"] != 0:
                    detail = f"the named Go event gate refused it (validator exit code {gate['validator_exit_code']})"
                elif gate["error"] is not None:
                    detail = gate["error"]
                elif gate["exunit_error"] is not None:
                    detail = gate["exunit_error"]
                elif gate["vitest_error"] is not None:
                    detail = gate["vitest_error"]
                else:
                    detail = f"exit code {gate['exit_code']}"
                blockers.append(f"check attempt {attempt['attempt_id']} gate {gate['id']} did not pass ({detail})")
    review = None
    review_path = run_dir / "frozen" / "review.json"
    if review_path.exists():
        if "review_sha256" not in run:
            raise Refused("the frozen review record is not bound by the run record")
        review_raw = read_bytes(review_path, MAX_RECORD_BYTES, "frozen review record")
        if sha256_hex(review_raw) != run["review_sha256"]:
            raise Refused("the frozen review record changed after it was imported")
        value = strict_json(review_raw, "frozen review record", MAX_RECORD_BYTES)
        scan_no_secret_keys(value, "frozen review record")
        review = validate_review_structure(value)
        blockers.extend(review_semantic_blockers(
            review,
            task,
            routes,
            run["routes_sha256"],
            sealed["subject_hash"],
            attempt["attempt_id"] if attempt is not None else None,
            attempt["sha256"] if attempt is not None else None,
        ))
    else:
        blockers.append("no independent review record was imported")
        if task["explanations"]:
            blockers.append("task explanations are not attested by an independent review")
    if candidate_usage is None:
        if "usage_sha256" not in run:
            raise Refused("the run has no frozen usage rows; run receipt with --usage first")
        usage_raw = read_bytes(run_dir / "frozen" / "usage.json", MAX_USAGE_BYTES, "frozen usage JSON")
        if sha256_hex(usage_raw) != run["usage_sha256"]:
            raise Refused("the frozen usage rows changed after they were imported")
    else:
        if "usage_sha256" in run:
            # The candidate may replace the frozen rows, but only after the previously
            # frozen usage bytes are verified: a tampered snapshot must never be
            # overwritten and silently accepted as a new import.
            previous = read_bytes(run_dir / "frozen" / "usage.json", MAX_USAGE_BYTES, "frozen usage JSON")
            if sha256_hex(previous) != run["usage_sha256"]:
                raise Refused("the frozen usage rows changed after they were imported")
        usage_raw = candidate_usage
        run = dict(run, usage_sha256=sha256_hex(candidate_usage))
    rows, repair_count, wall_time = validate_usage(usage_raw)
    validate_usage_against_review(rows, review)
    return build_receipt(run, task, routes, sealed, review, attempt, rows, blockers, repair_count, wall_time)


# ---------------------------------------------------------------------------
# Commands


def load_source_subject():
    spec = importlib.util.spec_from_file_location("linux_v2_source_subject", SOURCE_SUBJECT_SCRIPT)
    if spec is None or spec.loader is None:
        raise Refused("cannot load linux-v2-source-subject.py for clean-base qualification")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.source_subject


def environment_record() -> dict[str, str]:
    try:
        completed = subprocess.run(["git", "--version"], capture_output=True, check=False)
        git_version = completed.stdout.decode("utf-8", "replace").strip() or "unknown"
    except OSError:
        git_version = "unknown"
    return {
        "platform": sys.platform,
        "machine": host_platform.machine(),
        "python_version": host_platform.python_version(),
        "git_version": git_version,
    }


def _is_junction(path: pathlib.Path) -> bool:
    """True for a Windows junction, which is a link but not `path.is_symlink()`."""
    isjunction = getattr(os.path, "isjunction", None)
    return bool(isjunction is not None and isjunction(path))


def _file_content_digest(path: pathlib.Path) -> bytes:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest().encode("ascii")


def _within(root: pathlib.Path, candidate: pathlib.Path) -> bool:
    try:
        candidate.relative_to(root)
    except ValueError:
        return False
    return True


def _walk_error(error: OSError) -> None:
    # os.walk swallows a directory it cannot list unless onerror refuses: a silently
    # skipped subtree would be an unbound hole in the digest.
    raise Refused(f"cannot walk the dependency tree at {error.filename}: {error}")


def _link_payload(link: pathlib.Path, root: pathlib.Path, seen: frozenset) -> bytes:
    """The digest payload of one link: its text, where it resolves, and any outside content.

    A link inside the tree is bound by the walked entries the target names, and by the
    tree-relative route its chain resolves to: a link that leaves the tree and comes
    back (a junction or symlink hop) can be retargeted to another in-tree directory
    without changing either link's text, and the gates would then execute different
    bytes under the same recorded digest. A link whose resolved target leaves the tree
    is what pnpm's workspace links look like (`@symmetry/contracts -> ../../contracts`):
    the target's own content is executed by the gates, so it is digested here instead
    of being left unbound; a link cycle refuses rather than recursing forever.
    """
    try:
        target_text = os.readlink(link).encode("utf-8", "surrogateescape")
    except OSError as error:
        raise Refused(f"cannot read dependency tree link {link}: {error}")
    try:
        resolved = link.resolve(strict=True)
    except (OSError, RuntimeError) as error:
        # `RuntimeError` is a link (or junction) chain that loops: the target never
        # resolves, so the tree cannot be bound and the run is refused, not crashed.
        raise Refused(f"the dependency tree link {link} does not resolve: {error}")
    if _within(root, resolved):
        relative = resolved.relative_to(root).as_posix()
        return b"in-tree\0" + target_text + b"\0" + relative.encode("utf-8")
    if resolved in seen:
        raise Refused(f"the dependency tree link {link} resolves into a cycle at {resolved}")
    return b"out-of-tree\0" + target_text + b"\0" + _target_content_digest(resolved, seen | {resolved})


def _target_content_digest(path: pathlib.Path, seen: frozenset) -> bytes:
    """Content digest of one out-of-tree link target (a file, or a directory tree)."""
    if path.is_dir():
        digest = hashlib.sha256()
        digest.update(b"directory\0")
        for relative, kind, payload in _tree_entries(path, seen):
            digest.update(kind.encode("ascii") + b"\0" + relative.encode("utf-8") + b"\0" + payload + b"\n")
        return digest.hexdigest().encode("ascii")
    try:
        return _file_content_digest(path)
    except OSError as error:
        raise Refused(f"cannot read the dependency tree link target {path}: {error}")


def _tree_entries(root: pathlib.Path, seen: frozenset) -> list[tuple[str, str, bytes]]:
    """Every file and link under ``root`` as sorted (relative path, kind, payload)."""
    entries: list[tuple[str, str, bytes]] = []
    for walk_root, dirnames, filenames in os.walk(root, followlinks=False, onerror=_walk_error):
        for dirname in list(dirnames):
            candidate = pathlib.Path(walk_root, dirname)
            relative = candidate.relative_to(root).as_posix()
            if candidate.is_symlink() or _is_junction(candidate):
                dirnames.remove(dirname)
                entries.append((relative, "link", _link_payload(candidate, root, seen)))
                continue
            try:
                info = candidate.lstat()
            except OSError as error:
                raise Refused(f"cannot read dependency tree entry {relative}: {error}")
            if getattr(info, "st_reparse_tag", 0):
                # A reparse point that is neither a symlink nor a detectable junction
                # cannot be classified, so it refuses instead of being walked into.
                raise Refused(f"unsupported reparse-point directory in the dependency tree: {relative}")
        for name in filenames:
            candidate = pathlib.Path(walk_root, name)
            relative = candidate.relative_to(root).as_posix()
            if candidate.is_symlink():
                entries.append((relative, "link", _link_payload(candidate, root, seen)))
            else:
                try:
                    entries.append((relative, "file", _file_content_digest(candidate)))
                except OSError as error:
                    raise Refused(f"cannot read dependency tree entry {relative}: {error}")
    try:
        entries.sort(key=lambda entry: entry[0].encode("utf-8"))
    except UnicodeEncodeError:
        raise Refused("the dependency tree contains a path that is not valid UTF-8")
    return entries


def dependency_tree_digest(tree: pathlib.Path) -> tuple[str, int]:
    """Deterministic digest of every file and link of a resolved dependency tree.

    Each entry contributes its relative path (UTF-8 bytes), its kind and its payload in
    one sorted stream, so two trees with the same entries digest identically regardless
    of traversal order: a file contributes its content digest, a link its target text
    plus the tree-relative route it resolves to (an in-tree junction hop cannot be
    retargeted unseen) - and, when the target resolves outside the tree, that target's
    own content digest, because the gates execute it (pnpm's workspace links point at
    source outside the hoisted tree). Directory links are pruned instead of followed (a
    cycle refuses); empty directories are not entries.
    """
    try:
        resolved_tree = tree.resolve()
    except (OSError, RuntimeError) as error:
        # A tree path that is itself a link loop never resolves: refuse, never crash.
        raise Refused(f"the dependency tree is not a readable directory: {error}")
    entries = _tree_entries(resolved_tree, frozenset({resolved_tree}))
    digest = hashlib.sha256()
    digest.update(DEPENDENCY_TREE_NAMESPACE + b"\0")
    for relative, kind, payload in entries:
        digest.update(kind.encode("ascii") + b"\0" + relative.encode("utf-8") + b"\0" + payload + b"\n")
    return sha256_hex(digest.digest()), len(entries)


def command_prepare(arguments: argparse.Namespace) -> int:
    run_dir = pathlib.Path(arguments.run_dir)
    if run_dir.exists():
        if not run_dir.is_dir():
            raise Refused("the run directory path is not a directory")
        if any(run_dir.iterdir()):
            raise Refused("the run directory must be empty before prepare")
    task_raw = read_bytes(pathlib.Path(arguments.task), MAX_TASK_BYTES, "task JSON")
    task_value = strict_json(task_raw, "task JSON", MAX_TASK_BYTES)
    scan_no_secret_keys(task_value, "task JSON")
    task = validate_task(task_value)
    routes_raw = read_bytes(pathlib.Path(arguments.routes), MAX_ROUTES_BYTES, "routes JSON")
    routes_value = strict_json(routes_raw, "routes JSON", MAX_ROUTES_BYTES)
    scan_no_secret_keys(routes_value, "routes JSON")
    validate_routes(routes_value)
    measured = None
    if arguments.environment_json is not None:
        environment_raw = read_bytes(
            pathlib.Path(arguments.environment_json), MAX_RECORD_BYTES, "environment JSON"
        )
        measured = validate_measured_mapping(
            strict_json(environment_raw, "environment JSON", MAX_RECORD_BYTES), "environment JSON",
            redact=True,
        )

    created_here = not run_dir.exists()
    if created_here:
        try:
            run_dir.mkdir(parents=True)
        except OSError as error:
            raise Refused(f"cannot create the run directory: {error}")
    try:
        return finish_prepare(arguments, run_dir, task, task_raw, routes_raw, measured)
    except Refused:
        if created_here:
            shutil.rmtree(run_dir, ignore_errors=True)
        raise


def finish_prepare(
    arguments: argparse.Namespace, run_dir: pathlib.Path, task: dict[str, Any],
    task_raw: bytes, routes_raw: bytes, measured: dict[str, str] | None = None,
) -> int:
    source = str(arguments.source)
    checkout = run_dir / "checkout"
    # The seal compares worktree bytes with object-store bytes, so the isolated
    # checkout must materialize the stored blob bytes: never apply a host-dependent
    # end-of-line rendering during prepare.
    eol_overrides = ["-c", "core.autocrlf=false", "-c", "core.eol=lf"]
    try:
        clone = subprocess.run(
            ["git", "-c", f"safe.directory={source}", *eol_overrides,
             "clone", "--quiet", "--no-hardlinks", source, str(checkout)],
            capture_output=True, check=False,
        )
    except OSError as error:
        raise Refused(f"could not run git clone: {error}")
    if clone.returncode != 0:
        detail = clone.stderr.decode("utf-8", "replace").strip()
        raise Refused(f"could not clone {source}: {detail}")
    git_output(str(checkout), *eol_overrides, "checkout", "--quiet", "--detach", task["base"])
    head = require_commit(git_text(str(checkout), "rev-parse", "HEAD"), "isolated checkout HEAD")
    if head != task["base"]:
        raise Refused("the isolated checkout is not at the frozen base commit")
    refuse_object_substitution(checkout)
    try:
        base_subject = load_source_subject()(str(checkout), task["base"])
    except SystemExit as refusal:
        raise Refused(f"the isolated checkout is not a clean clone at the frozen base: {refusal}")
    except (OSError, subprocess.SubprocessError) as error:
        raise Refused(f"could not qualify the isolated checkout: {error}")

    frozen = run_dir / "frozen"
    write_bytes(frozen / "task.json", task_raw)
    write_bytes(frozen / "routes.json", routes_raw)
    # Vite writes its config-loader temp files next to the nearest existing
    # `node_modules` directory. The run store carries an empty one so those files land
    # in run state instead of inside an enclosing sealed checkout or the shared hoisted
    # dependency tree; an empty directory changes no module resolution, because Node
    # keeps walking ancestors past it.
    (run_dir / "node_modules").mkdir(exist_ok=True)
    environment = environment_record()
    if measured is not None:
        # The measured values were rendered by the sanitize tool's bytes at this
        # moment; binding the digest here is what lets a later derivation refuse a
        # receipt that would name other bytes as the producer.
        environment["measured"] = measured
        environment["measured_sanitizer_sha256"] = sanitize_source_digest()
    run = {
        "schema": SCHEMA,
        "task_id": task["task_id"],
        "base": task["base"],
        "source": source,
        "head": task["base"],
        "base_tree_digest": base_subject["tree_digest"],
        "task_sha256": sha256_hex(task_raw),
        "routes_sha256": sha256_hex(routes_raw),
        "environment": environment,
    }
    write_json(run_dir / "run.json", run)
    print(json.dumps({
        "run_dir": str(run_dir),
        "task_id": task["task_id"],
        "base": task["base"],
        "head": task["base"],
        "task_sha256": run["task_sha256"],
        "routes_sha256": run["routes_sha256"],
        "base_tree_digest": run["base_tree_digest"],
    }, indent=2, sort_keys=True))
    return 0


def _dependency_tree(value: str) -> pathlib.Path:
    tree = pathlib.Path(value)
    try:
        resolved = tree.resolve(strict=True)
    except (OSError, RuntimeError) as error:
        # A tree whose own path is a link loop never resolves; refuse it as unreadable.
        raise Refused(f"the dependency tree is not a readable directory: {error}")
    if not resolved.is_dir():
        raise Refused("--tree must name a directory")
    return resolved


def holds_a_package_entry(node_modules: pathlib.Path) -> bool:
    """True when a `node_modules` holds an entry Node could resolve a package from.

    Node walks past a `node_modules` that lacks the package it needs, so "has any
    entry" is not "the tree the gates execute from". Vite's config loader writes its
    temp files into the nearest existing `node_modules` - the placeholder `prepare`
    creates - so a dot-only directory such as `.vite-temp` must not turn that
    placeholder into the resolved tree and refuse the true hoisted binding
    (STD-A03). A package entry is a non-dot directory, the scoped `@name` directory
    a package installs under included; dot entries (`.bin`, `.vite-temp`, `.vite`,
    `.modules.yaml`, ...) and plain files answer for no package.
    """
    try:
        with os.scandir(node_modules) as entries:
            for entry in entries:
                if entry.name.startswith("."):
                    continue
                if entry.is_dir():
                    return True
    except OSError as error:
        raise Refused(f"cannot list the dependency-tree candidate {node_modules}: {error}")
    return False


def resolved_dependency_tree(checkout: pathlib.Path, gate_cwd: str) -> pathlib.Path | None:
    """The `node_modules` tree Node reaches first from one gate's cwd, if any.

    Node resolves a module by walking the requiring directory's ancestors and trying
    each `node_modules` directory it passes; a directory holding no package entry
    answers for no package, so the first such directory on the chain is the tree the
    gates execute from (the runbook's hoisted install sits one level above the run
    directory, past the placeholder `prepare` creates - which Vite later fills with
    dot-only temp state). `None` means nothing on the chain holds a package entry,
    so no tree can be bound as the executed one.
    """
    directory = pathlib.Path(checkout, *gate_cwd.split("/"))
    while True:
        candidate = directory / "node_modules"
        if candidate.is_dir() and holds_a_package_entry(candidate):
            try:
                return candidate.resolve()
            except (OSError, RuntimeError) as error:
                raise Refused(f"the dependency tree at {candidate} does not resolve: {error}")
        parent = directory.parent
        if parent == directory:
            return None
        directory = parent


def require_resolved_dependency_tree(
    run_dir: pathlib.Path, run: dict[str, Any], task: dict[str, Any], tree: pathlib.Path
) -> None:
    """Refuse a bound tree the checkout's gates do not resolve their modules from.

    The recorded binding attests the tree the gates executed from, so the tree must
    be the one each gate's cwd resolves first: a receipt that bound a decoy tree
    would attest a tree no gate ever executed (the F01 review probe). This runs on
    the bind path (where the binding is created) and on `--check` (where a stale
    binding would otherwise keep verifying a tree the gates no longer use).
    """
    checkout = run_dir / "checkout"
    if not checkout.is_dir():
        raise Refused("the run has no checkout to resolve the dependency tree from; run prepare first")
    for gate in task["gates"]:
        resolved = resolved_dependency_tree(checkout, gate["cwd"])
        if resolved is None:
            raise Refused(
                f"the checkout resolves no dependency tree from gate {gate['id']}'s cwd, so "
                f"--tree {tree} is not the tree the gates execute from"
            )
        if resolved != tree:
            raise Refused(
                f"--tree {tree} is not the dependency tree the gates execute from: gate "
                f"{gate['id']} (cwd {gate['cwd']!r}) resolves {resolved} first"
            )


def command_bind_dependencies(arguments: argparse.Namespace) -> int:
    run_dir = pathlib.Path(arguments.run_dir)
    run = load_run(run_dir)
    task = load_frozen_task(run_dir, run)
    recorded = run["environment"].get("provisioning")
    if arguments.check:
        if recorded is None:
            raise Refused("the run records no provisioning step to check")
        tree = _dependency_tree(arguments.tree)
        digest, entries = dependency_tree_digest(tree)
        if (digest, entries) != (recorded["tree_digest"], recorded["tree_entries"]):
            raise Refused(
                "the dependency tree no longer matches the recorded provisioning step: "
                f"{digest} over {entries} entries != {recorded['tree_digest']} over "
                f"{recorded['tree_entries']} entries"
            )
        require_resolved_dependency_tree(run_dir, run, task, tree)
        print(json.dumps({
            "verified": True,
            "tree": recorded["tree"],
            "tree_digest": digest,
            "tree_entries": entries,
        }, indent=2, sort_keys=True))
        return 0
    if recorded is not None:
        raise Refused("the run already records a provisioning step")
    if not arguments.log:
        raise Refused("bind-dependencies requires --log to record the provisioning log (or --check)")
    tree = _dependency_tree(arguments.tree)
    checkout = run_dir / "checkout"
    if checkout.exists() and (tree == checkout.resolve() or checkout.resolve() in tree.parents):
        raise Refused("the bound dependency tree must live outside the sealed checkout")
    require_resolved_dependency_tree(run_dir, run, task, tree)
    tree_digest, tree_entries = dependency_tree_digest(tree)
    log_path = pathlib.Path(arguments.log)
    log_raw = read_bytes(log_path, MAX_PROVISIONING_LOG_BYTES, "dependency provisioning log")
    if not log_raw:
        raise Refused("the dependency provisioning log is empty")
    # The tree route is run-relative, so a reader can recompute the digest from the
    # stored run directory (a redacted absolute route would make that impossible); the
    # log's source route keeps the measured-surface redaction, because it names where
    # the log came from rather than a re-verifiable location.
    try:
        relative_tree = os.path.relpath(str(tree), str(run_dir.resolve()))
    except ValueError:
        raise Refused(
            "the dependency tree must live on the same drive or root as the run directory "
            "so the recorded digest stays recomputable"
        )
    provisioning = validate_provisioning({
        "tree": pathlib.PurePath(relative_tree).as_posix(),
        "tree_digest": tree_digest,
        "tree_entries": tree_entries,
        "install_log": PROVISIONING_LOG_PATH,
        "install_log_source": redact_host_paths(str(log_path)),
        # The sanitize tool's bytes rendered `install_log_source` at this moment;
        # binding the digest here is what lets a later derivation refuse a receipt
        # that would name other bytes as the producer (STD-A02).
        "install_log_source_sanitizer_sha256": sanitize_source_digest(),
        "install_log_sha256": sha256_hex(log_raw),
        "install_log_bytes": len(log_raw),
    }, "provisioning step")
    environment = dict(run["environment"], provisioning=provisioning)
    # The log copy and the run record are both written only after every value that will
    # be recorded has been serialized by validate_provisioning, so a refusal leaves the
    # run unbound to a partial provisioning step.
    write_bytes(run_dir / PROVISIONING_LOG_PATH, log_raw)
    save_run(run_dir, run, environment=environment)
    print(json.dumps({
        "tree": provisioning["tree"],
        "tree_digest": tree_digest,
        "tree_entries": tree_entries,
        "install_log": PROVISIONING_LOG_PATH,
        "install_log_sha256": provisioning["install_log_sha256"],
        "install_log_bytes": provisioning["install_log_bytes"],
    }, indent=2, sort_keys=True))
    return 0


def command_seal(arguments: argparse.Namespace) -> int:
    run_dir = pathlib.Path(arguments.run_dir)
    run = load_run(run_dir)
    task = load_frozen_task(run_dir, run)
    # Both frozen inputs are bound before anything is sealed: a tampered routes file
    # must be refused here, not only when a receipt is derived.
    load_frozen_routes(run_dir, run)
    checkout = run_dir / "checkout"
    verify_base_commit(checkout, task["base"])
    head = require_commit(git_text(str(checkout), "rev-parse", "HEAD"), "checkout HEAD")
    base_files = list_base_paths(checkout, task["base"])
    changes = collect_changes(checkout, task["base"])
    sealed_changes = validate_changes(checkout, task, base_files, changes)
    subject = compute_subject(checkout, task["task_id"], task["base"], head)
    if collect_changes(checkout, task["base"]) != changes:
        raise Refused("source changed while sealing its subject")
    sealed = {
        "schema": SCHEMA,
        "task_id": task["task_id"],
        "base": task["base"],
        "head": head,
        "manifest_digest": subject["manifest_digest"],
        "content_revision": subject["content_revision"],
        "subject_hash": subject["subject_hash"],
        "file_count": subject["file_count"],
        "changed_paths": sealed_changes,
    }
    write_json(run_dir / "sealed.json", sealed)
    print(json.dumps(sealed, indent=2, sort_keys=True))
    return 0


def command_check(arguments: argparse.Namespace) -> int:
    if arguments.timeout < 0:
        raise Refused("--timeout must be >= 0; 0 disables the per-gate timeout")
    run_dir = pathlib.Path(arguments.run_dir).resolve()
    run = load_run(run_dir)
    task = load_frozen_task(run_dir, run)
    require_dependency_provisioning(run_dir, run, task)
    sealed = load_sealed(run_dir, task)
    require_current_subject(run_dir, task, sealed)
    unknown = [gate_id for gate_id in (arguments.gate or []) if gate_id not in {gate["id"] for gate in task["gates"]}]
    if unknown:
        raise Refused("unknown gate ids: " + ", ".join(sorted(unknown)))
    gates = [gate for gate in task["gates"] if not arguments.gate or gate["id"] in set(arguments.gate)]
    checks_dir = run_dir / "checks"
    checks_dir.mkdir(parents=True, exist_ok=True)
    numbers = [int(name.split("-", 1)[1]) for name in attempt_names(run_dir)]
    attempt_name = f"attempt-{(max(numbers) if numbers else 0) + 1:04d}"
    attempt_dir = checks_dir / attempt_name
    attempt_dir.mkdir()
    timeout = arguments.timeout if arguments.timeout > 0 else None
    verified: list[dict[str, Any]] = []
    attempt_passed = True
    stopped_early = False
    for gate in gates:
        result = execute_gate(run_dir / "checkout", gate, attempt_dir / gate["id"], timeout, run_dir)
        result["subject_hash_before"] = sealed["subject_hash"]
        after = current_subject(run_dir, task)["subject_hash"]
        result["subject_hash_after"] = after
        if after != sealed["subject_hash"]:
            result["passed"] = False
            result["error"] = (result["error"] + "; " if result["error"] else "") + \
                SOURCE_CHANGED_SUFFIX
            stopped_early = True
        result_raw = write_json(attempt_dir / gate["id"] / "result.json", result)
        verified.append({
            "id": gate["id"],
            "passed": result["passed"],
            "result_sha256": sha256_hex(result_raw),
            "stdout_sha256": result["stdout_sha256"],
            "stderr_sha256": result["stderr_sha256"],
            "validator_exit_code": result["validator"]["exit_code"] if result["validator"] else None,
            "validator_stdout_sha256": result["validator"]["stdout_sha256"] if result["validator"] else None,
            "validator_stderr_sha256": result["validator"]["stderr_sha256"] if result["validator"] else None,
        })
        if not result["passed"]:
            attempt_passed = False
        if stopped_early:
            break
    complete = len(verified) == len(task["gates"]) and not stopped_early
    provisioning = run["environment"].get("provisioning")
    dependency_provisioning = None
    if provisioning is not None:
        # A gate that wrote into the shared dependency tree would change what later
        # gates execute; the post-attempt digest refuses that instead of binding it, and
        # it runs before the attempt record exists so a refused attempt is not evidence.
        verify_dependency_tree(run_dir, provisioning)
        # The attempt records the provisioning step it executed under, so a receipt can
        # refuse a binding made after the attempt ran (a late bind requires a re-check).
        dependency_provisioning = provisioning_view(provisioning)
    attempt = {
        "schema": SCHEMA,
        "attempt_id": attempt_name,
        "task_id": task["task_id"],
        "task_sha256": run["task_sha256"],
        "routes_sha256": run["routes_sha256"],
        "subject_hash": sealed["subject_hash"],
        "complete": complete,
        "passed": attempt_passed and not stopped_early and complete,
        "dependency_provisioning": dependency_provisioning,
        "gates": verified,
    }
    attempt_raw = write_json(attempt_dir / "attempt.json", attempt)
    print(json.dumps({
        "attempt_id": attempt_name,
        "attempt_sha256": sha256_hex(attempt_raw),
        "complete": attempt["complete"],
        "passed": attempt["passed"],
        "subject_hash": sealed["subject_hash"],
    }, indent=2, sort_keys=True))
    if not attempt["passed"]:
        print(f"check attempt {attempt_name} failed; evidence preserved under {attempt_dir}", file=sys.stderr)
        return 1
    return 0


def command_review(arguments: argparse.Namespace) -> int:
    run_dir = pathlib.Path(arguments.run_dir)
    run = load_run(run_dir)
    task = load_frozen_task(run_dir, run)
    routes = load_frozen_routes(run_dir, run)
    sealed = load_sealed(run_dir, task)
    require_current_subject(run_dir, task, sealed)
    attempt = require_passing_attempt(run_dir, task, sealed["subject_hash"], run)
    review_raw = read_bytes(pathlib.Path(arguments.review), MAX_RECORD_BYTES, "review record")
    value = strict_json(review_raw, "review record", MAX_RECORD_BYTES)
    scan_no_secret_keys(value, "review record")
    review = validate_review_structure(value)
    blockers = review_semantic_blockers(
        review, task, routes, run["routes_sha256"], sealed["subject_hash"],
        attempt["attempt_id"], attempt["sha256"],
    )
    if blockers:
        raise Refused("the review record is not acceptable: " + "; ".join(blockers))
    write_bytes(run_dir / "frozen" / "review.json", review_raw)
    save_run(run_dir, run, review_sha256=sha256_hex(review_raw))
    print(json.dumps({
        "subject_hash": sealed["subject_hash"],
        "check_attempt_id": attempt["attempt_id"],
        "review_sha256": sha256_hex(review_raw),
        "verdict": review["verdict"],
    }, indent=2, sort_keys=True))
    return 0


def command_receipt(arguments: argparse.Namespace) -> int:
    run_dir = pathlib.Path(arguments.run_dir)
    run = load_run(run_dir)
    usage_raw = read_bytes(pathlib.Path(arguments.usage), MAX_USAGE_BYTES, "usage JSON")
    # Derive the receipt for the candidate rows before publishing anything: a refused
    # import must leave the previous frozen usage rows, run record and receipt intact.
    receipt = derive_receipt(run_dir, usage_raw)
    # Serialize every document this command publishes before the first write. Deriving
    # the receipt is not enough: a value the encoder refuses must not leave frozen
    # usage rows and run record at the candidate revision while receipt.json stays at
    # the previous one. No write happens unless the receipt for the candidate and the
    # record that binds it have both been serialized.
    receipt_raw = canonical_json(receipt)
    run_raw = run_record_bytes(run, usage_sha256=sha256_hex(usage_raw))
    write_bytes(run_dir / "frozen" / "usage.json", usage_raw)
    write_bytes(run_dir / "run.json", run_raw)
    write_bytes(run_dir / "receipt.json", receipt_raw)
    print(json.dumps({
        "accepted": receipt["accepted"],
        "acceptance_blockers": receipt["acceptance_blockers"],
        "accounting_complete": receipt["cost"]["accounting_complete"],
        "receipt_sha256": sha256_hex(receipt_raw),
        "subject_hash": receipt["subject_hash"],
        "task_id": receipt["task_id"],
    }, indent=2, sort_keys=True))
    return 0


def first_difference(left: Any, right: Any, path: str = "$") -> str | None:
    if isinstance(left, bool) or isinstance(right, bool):
        return None if left is right else f"{path}: {left!r} != {right!r}"
    if isinstance(left, (int, float)) and isinstance(right, (int, float)):
        # JSON number classes are part of the compared document: `accept` prints the
        # submitted bytes' digest, so an int spelling and a float spelling of the same
        # mathematical value are different documents even though Python's `==` says
        # they are equal.
        if type(left) is not type(right) or left != right:
            return f"{path}: {left!r} != {right!r}"
        return None
    if isinstance(left, dict) and isinstance(right, dict):
        if set(left) != set(right):
            missing = sorted(set(left) - set(right))
            extra = sorted(set(right) - set(left))
            parts = []
            if missing:
                parts.append("missing in submitted: " + ", ".join(missing))
            if extra:
                parts.append("not expected: " + ", ".join(extra))
            return f"{path}: key sets differ ({'; '.join(parts)})"
        for key in sorted(left):
            difference = first_difference(left[key], right[key], f"{path}.{key}")
            if difference is not None:
                return difference
        return None
    if isinstance(left, list) and isinstance(right, list):
        if len(left) != len(right):
            return f"{path}: length {len(left)} != {len(right)}"
        for index, (left_item, right_item) in enumerate(zip(left, right)):
            difference = first_difference(left_item, right_item, f"{path}[{index}]")
            if difference is not None:
                return difference
        return None
    return None if left == right else f"{path}: {left!r} != {right!r}"


def command_accept(arguments: argparse.Namespace) -> int:
    run_dir = pathlib.Path(arguments.run_dir)
    submitted_path = pathlib.Path(arguments.receipt) if arguments.receipt else run_dir / "receipt.json"
    submitted_raw = read_bytes(submitted_path, MAX_RECEIPT_BYTES, "submitted receipt")
    submitted = strict_json(submitted_raw, "submitted receipt", MAX_RECEIPT_BYTES)
    scan_no_secret_keys(submitted, "submitted receipt")
    expected = derive_receipt(run_dir)
    expected_raw = canonical_json(expected)
    if submitted_raw != expected_raw:
        difference = first_difference(submitted, expected)
        if difference is not None:
            raise Refused("the submitted receipt does not match the derived evidence at " + difference)
        raise Refused(
            "the submitted receipt is not the canonical serialization of the derived receipt "
            "(the bytes differ with no structural difference: number spelling or formatting)"
        )
    if not expected["accepted"]:
        raise Refused("the submitted receipt is a nonaccepted row: " + "; ".join(expected["acceptance_blockers"]))
    print(json.dumps({
        "accepted": True,
        "receipt_sha256": sha256_hex(submitted_raw),
        "subject_hash": expected["subject_hash"],
        "task_id": expected["task_id"],
    }, indent=2, sort_keys=True))
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="model-routing-evaluation.py",
        description=(
            "Auditable acceptance evaluation: freeze a task and routes, bind one exact source Subject, "
            "execute frozen gates, import an independent review, derive a receipt and verify it."
        ),
    )
    subparsers = parser.add_subparsers(dest="operation", required=True)

    prepare = subparsers.add_parser("prepare", help="clone the frozen base into an empty external run directory")
    prepare.add_argument("--run-dir", required=True, help="empty coordinator-owned run directory")
    prepare.add_argument("--task", required=True, help="frozen task JSON file")
    prepare.add_argument("--routes", required=True, help="worker/reviewer route metadata JSON file")
    prepare.add_argument("--source", required=True, help="git repository path or URL to clone")
    prepare.add_argument("--environment-json", help="measured non-secret execution-surface facts JSON file")
    prepare.set_defaults(handler=command_prepare)

    bind = subparsers.add_parser(
        "bind-dependencies",
        help="record the resolved dependency tree and its provisioning log in the run environment",
    )
    bind.add_argument("--run-dir", required=True)
    bind.add_argument("--tree", required=True, help="resolved dependency tree outside every sealed checkout")
    bind.add_argument("--log", help="provisioning install log to copy into the run store and bind")
    bind.add_argument("--check", action="store_true",
                      help="recompute the tree digest and compare it with the recorded provisioning step")
    bind.set_defaults(handler=command_bind_dependencies)

    seal = subparsers.add_parser("seal", help="bind the checkout into one exact evaluation Subject")
    seal.add_argument("--run-dir", required=True)
    seal.set_defaults(handler=command_seal)

    check = subparsers.add_parser(
        "check", help="execute the frozen gates and require the named Go and ExUnit tests"
    )
    check.add_argument("--run-dir", required=True)
    check.add_argument("--gate", action="append", help="run only this gate id (repeatable; attempt becomes incomplete)")
    check.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT, help="per-gate timeout seconds; 0 disables")
    check.set_defaults(handler=command_check)

    review = subparsers.add_parser("review", help="import the independent review record for the sealed Subject")
    review.add_argument("--run-dir", required=True)
    review.add_argument("--review", required=True, help="reviewer-session review JSON file")
    review.set_defaults(handler=command_review)

    receipt = subparsers.add_parser("receipt", help="derive the task receipt from stored evidence and usage rows")
    receipt.add_argument("--run-dir", required=True)
    receipt.add_argument("--usage", required=True, help="phase usage JSON file covering all six stages")
    receipt.set_defaults(handler=command_receipt)

    accept = subparsers.add_parser("accept", help="recompute the receipt and refuse any mismatch or nonaccepted row")
    accept.add_argument("--run-dir", required=True)
    accept.add_argument("--receipt", help="submitted receipt path (default: <run-dir>/receipt.json)")
    accept.set_defaults(handler=command_accept)

    arguments = parser.parse_args(argv)
    try:
        return arguments.handler(arguments)
    except Refused as refusal:
        print(f"model-routing-evaluation refused: {refusal}", file=sys.stderr)
        return 1
    except OSError as error:
        print(f"model-routing-evaluation refused: filesystem error: {error}", file=sys.stderr)
        return 1
    except (RecursionError, UnicodeError) as error:
        print(f"model-routing-evaluation refused: {type(error).__name__}: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
