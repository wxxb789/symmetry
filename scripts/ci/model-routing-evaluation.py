#!/usr/bin/env python3
"""Run one auditable acceptance evaluation against a frozen task and routes.

Operations:
  prepare  fresh isolated clone of --source at the frozen task base, then freeze
           the exact task and routes bytes in an external run directory
  seal     bind the checkout (actual base/head plus a deterministic manifest of
           tracked and nonignored untracked bytes) into an evaluation Subject
  check    execute every frozen gate argv without a shell, capture outputs, bind
           the Subject before and after, and require named Go tests through the
           existing assert-go-test-json.py event gate
  review   import the coordinator's independent review record for the sealed
           Subject and the latest passing check attempt
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
exempted.
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
import stat
import subprocess
import sys
from typing import Any, Iterable

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
DEFAULT_TIMEOUT = 1800.0

COMMIT_PATTERN = re.compile(r"^[0-9a-f]{40}$")
IDENTIFIER_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$")
ATTEMPT_PATTERN = re.compile(r"^attempt-[0-9]{4,}$")
DIGEST_PATTERN = re.compile(r"^sha256:[0-9a-f]{64}$")
SECRET_KEY_PATTERN = re.compile(
    r"(^|[^a-z0-9])(api[_-]?key|apikey|secret|password|passwd|token|bearer"
    r"|private[_-]?key|access[_-]?key)([^a-z0-9]|$)",
    re.IGNORECASE,
)

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
GATE_KEYS = {"id", "cwd", "argv", "go_tests", "go_package"}
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
PHASE_OPTIONAL_KEYS = {"metadata", "limitations", "known_subtotals"}
MODEL_ID_KEYS = {"model", "provider", "api", "assistant_messages"}
PROMPT_HASH_KEYS = {"kind", "sha256", "index"}
PROMPT_HASH_REQUIRED_KEYS = {"kind", "sha256"}
SHA256_FIELD_PATTERN = re.compile(r"^(sha256:)?[0-9a-f]{64}$")
ATTEMPT_KEYS = {
    "schema", "attempt_id", "task_id", "task_sha256", "routes_sha256",
    "subject_hash", "complete", "passed", "gates",
}
ATTEMPT_ENTRY_KEYS = {
    "id", "passed", "result_sha256", "stdout_sha256", "stderr_sha256",
    "validator_exit_code", "validator_stdout_sha256", "validator_stderr_sha256",
}
RESULT_KEYS = {
    "gate_id", "cwd", "argv", "go_tests", "go_package", "exit_code", "timed_out",
    "error", "stdout_sha256", "stderr_sha256", "stdout_bytes", "stderr_bytes",
    "validator", "subject_hash_before", "subject_hash_after", "passed",
}
VALIDATOR_KEYS = {"argv", "exit_code", "stdout_sha256", "stderr_sha256", "stdout_bytes", "stderr_bytes"}

VALIDATOR_SCRIPT = pathlib.Path(__file__).with_name("assert-go-test-json.py").resolve()
SOURCE_SUBJECT_SCRIPT = pathlib.Path(__file__).with_name("linux-v2-source-subject.py").resolve()


class Refused(Exception):
    """A refusal that must never be recorded as accepted evidence."""


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
    except RecursionError:
        raise Refused(f"{source} nests too deeply")


def canonical_json(value: Any) -> bytes:
    return json.dumps(value, indent=2, sort_keys=True, ensure_ascii=False).encode("utf-8") + b"\n"


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
    if not math.isfinite(float(value)) or float(value) < minimum:
        raise Refused(f"{source} must be a finite number >= {minimum}")
    return value


def require_optional_number(value: Any, source: str, minimum: float = 0.0) -> float | int | None:
    if value is None:
        return None
    return require_number(value, source, minimum)


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
    """Match exact relative paths or gitignore-style globs with '**' support."""

    def __init__(self, patterns: Iterable[str]):
        self.patterns = [tuple(pattern.split("/")) for pattern in patterns]

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
    command = ["git", "-c", f"safe.directory={repository}", "-C", repository, *arguments]
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


def git_config_bool(checkout: pathlib.Path, key: str) -> bool:
    command = ["git", "-c", f"safe.directory={checkout}", "-C", str(checkout), "config", "--bool", key]
    try:
        completed = subprocess.run(command, capture_output=True, check=False)
    except OSError as error:
        raise Refused(f"could not read git config {key}: {error}")
    if completed.returncode != 0:
        return True  # git's own default for core.filemode is true
    return completed.stdout.decode("utf-8", "replace").strip() != "false"


def iter_source_paths(checkout: pathlib.Path) -> list[str]:
    """Every checkout file and symlink; only .git internals are excluded."""
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
        for name in filenames:
            if name == ".git":
                continue
            paths.append(pathlib.Path(root, name).relative_to(checkout).as_posix())
    return sorted(paths, key=lambda value: value.encode("utf-8"))


def read_worktree_entries(checkout: pathlib.Path) -> dict[str, tuple[str, str, str]]:
    """Bind bytes and modes of every checkout file, including ignored files."""
    executable_bits = os.name == "posix" and git_config_bool(checkout, "core.filemode")
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
    for path in sorted(entries, key=lambda value: value.encode("utf-8")):
        mode, kind, digest = entries[path]
        hasher.update(mode.encode("ascii") + b"\0" + kind.encode("ascii") + b"\0")
        hasher.update(path.encode("utf-8") + b"\0" + digest.encode("ascii") + b"\0")
    return "sha256:" + hasher.hexdigest(), len(entries)


def list_base_paths(checkout: pathlib.Path, base: str) -> set[str]:
    paths: set[str] = set()
    for record in git_output(str(checkout), "ls-tree", "--full-tree", "-r", "-z", base).split(b"\0"):
        if not record:
            continue
        header, path_bytes = record.split(b"\t", 1)
        _mode, kind, _object = header.split(b" ", 2)
        path = decode_path(path_bytes)
        if kind != b"blob":
            raise Refused(f"the base tree contains a non-blob or submodule entry: {path}")
        paths.add(path)
    return paths


def verify_base_commit(checkout: pathlib.Path, base: str) -> None:
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
    changes: dict[str, set[str]] = {}
    for arguments in (
        ("diff", "--no-renames", "--name-status", "-z", base, "--"),
        ("diff", "--cached", "--no-renames", "--name-status", "-z", base, "--"),
    ):
        tokens = [token for token in git_output(str(checkout), *arguments).split(b"\0") if token]
        if len(tokens) % 2:
            raise Refused("unexpected git diff --name-status output")
        for index in range(0, len(tokens), 2):
            changes.setdefault(decode_path(tokens[index + 1]), set()).add(
                tokens[index].decode("ascii", "replace")[:1]
            )
    base_paths = list_base_paths(checkout, base)
    worktree_paths = set(iter_source_paths(checkout))
    for path in worktree_paths - base_paths:
        changes.setdefault(path, set()).add("A")
    for path in base_paths - worktree_paths:
        changes.setdefault(path, set()).add("D")
    return changes


def validate_changes(
    task: dict[str, Any], base_files: set[str], changes: dict[str, set[str]]
) -> list[dict[str, str]]:
    allowed = PathMatcher(task["allowed_paths"])
    protected = PathMatcher(task["protected_paths"])
    sealed_changes: list[dict[str, str]] = []
    for path in sorted(changes, key=lambda value: value.encode("utf-8")):
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
    return subject


# ---------------------------------------------------------------------------
# Frozen input validation


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
    return {"id": gate_id, "cwd": cwd, "argv": argv, "go_tests": go_tests, "go_package": go_package}


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


def validate_environment(value: Any) -> dict[str, str]:
    block = require_object(value, "run record environment", ENVIRONMENT_KEYS, ENVIRONMENT_KEYS)
    return {key: require_text(block[key], f"run record environment {key}", max_length=300) for key in sorted(ENVIRONMENT_KEYS)}


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
    for key in ("review_sha256", "usage_sha256"):
        if key in value:
            run[key] = require_digest(value[key], f"run record {key}")
    return run


def save_run(run_dir: pathlib.Path, run: dict[str, Any], **updates: str) -> None:
    merged = dict(run)
    merged.update(updates)
    write_json(run_dir / "run.json", merged)


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


def validator_argv(gate_dir: pathlib.Path, gate: dict[str, Any]) -> list[str]:
    argv = [sys.executable, str(VALIDATOR_SCRIPT), str(gate_dir / "stdout.bin")]
    if gate["go_package"]:
        argv += ["--package", gate["go_package"]]
    argv += list(gate["go_tests"])
    return argv


def run_validator(gate_dir: pathlib.Path, gate: dict[str, Any]) -> dict[str, Any]:
    argv = validator_argv(gate_dir, gate)
    try:
        completed = subprocess.run(argv, capture_output=True, check=False)
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


def execute_gate(
    checkout: pathlib.Path, gate: dict[str, Any], gate_dir: pathlib.Path, timeout: float | None
) -> dict[str, Any]:
    gate_dir.mkdir(parents=True, exist_ok=True)
    cwd = pathlib.Path(checkout, *gate["cwd"].split("/"))
    stdout_raw = b""
    stderr_raw = b""
    exit_code: int | None = None
    timed_out = False
    error: str | None = None
    if not cwd.is_dir():
        error = f"gate cwd {gate['cwd']!r} is missing in the checkout"
    else:
        try:
            completed = subprocess.run(
                gate["argv"], cwd=str(cwd), capture_output=True, timeout=timeout, check=False
            )
            stdout_raw = completed.stdout
            stderr_raw = completed.stderr
            exit_code = completed.returncode
        except subprocess.TimeoutExpired as expired:
            timed_out = True
            stdout_raw = expired.stdout or b""
            stderr_raw = expired.stderr or b""
        except OSError as failure:
            error = f"could not execute gate argv: {failure}"
    write_bytes(gate_dir / "stdout.bin", stdout_raw)
    write_bytes(gate_dir / "stderr.bin", stderr_raw)
    if gate["go_tests"] is not None:
        validator = run_validator(gate_dir, gate)
    else:
        validator = None
    passed = (
        error is None
        and not timed_out
        and exit_code == 0
        and (validator is None or validator["exit_code"] == 0)
    )
    return {
        "gate_id": gate["id"],
        "cwd": gate["cwd"],
        "argv": gate["argv"],
        "go_tests": gate["go_tests"],
        "go_package": gate["go_package"],
        "exit_code": exit_code,
        "timed_out": timed_out,
        "error": error,
        "stdout_sha256": sha256_hex(stdout_raw),
        "stderr_sha256": sha256_hex(stderr_raw),
        "stdout_bytes": len(stdout_raw),
        "stderr_bytes": len(stderr_raw),
        "validator": validator,
        "subject_hash_before": None,
        "subject_hash_after": None,
        "passed": passed,
    }


def recompute_gate(gate_dir: pathlib.Path, gate: dict[str, Any], sealed_hash: str) -> dict[str, Any]:
    expected_files = {"result.json", "stdout.bin", "stderr.bin"}
    if gate["go_tests"] is not None:
        expected_files |= {"validator.stdout.bin", "validator.stderr.bin"}
    actual_files = sorted(entry.name for entry in gate_dir.iterdir())
    if actual_files != sorted(expected_files):
        raise Refused(f"the stored evidence for gate {gate['id']} contains unexpected files")
    result_raw = read_bytes(gate_dir / "result.json", MAX_RESULT_BYTES, "gate result")
    result = strict_json(result_raw, "gate result", MAX_RESULT_BYTES)
    require_object(result, "gate result", RESULT_KEYS, RESULT_KEYS)
    for key, expected in (
        ("gate_id", gate["id"]),
        ("cwd", gate["cwd"]),
        ("argv", gate["argv"]),
        ("go_tests", gate["go_tests"]),
        ("go_package", gate["go_package"]),
    ):
        if result[key] != expected:
            raise Refused(f"the stored result for gate {gate['id']} does not match the frozen gate {key}")
    subject_bound = (
        result["subject_hash_before"] == sealed_hash and result["subject_hash_after"] == sealed_hash
    )
    stdout_raw = read_bytes(gate_dir / "stdout.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} stdout")
    stderr_raw = read_bytes(gate_dir / "stderr.bin", MAX_CAPTURE_BYTES, f"gate {gate['id']} stderr")
    if result["stdout_bytes"] != len(stdout_raw) or result["stdout_sha256"] != sha256_hex(stdout_raw):
        raise Refused(f"the stored stdout for gate {gate['id']} changed after it was captured")
    if result["stderr_bytes"] != len(stderr_raw) or result["stderr_sha256"] != sha256_hex(stderr_raw):
        raise Refused(f"the stored stderr for gate {gate['id']} changed after it was captured")
    exit_code = result["exit_code"]
    if exit_code is not None and (isinstance(exit_code, bool) or not isinstance(exit_code, int)):
        raise Refused(f"the stored exit code for gate {gate['id']} is invalid")
    timed_out = require_bool(result["timed_out"], f"gate {gate['id']} timed_out")
    error = require_optional_text(result["error"], f"gate {gate['id']} error", max_length=2000)
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
        try:
            rerun = subprocess.run(validator_argv(gate_dir, gate), capture_output=True, check=False)
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
    if result["passed"] is not passed:
        raise Refused(f"the stored result for gate {gate['id']} claims passed={result['passed']} but its evidence says {passed}")
    return {
        "id": gate["id"],
        "cwd": gate["cwd"],
        "argv": gate["argv"],
        "go_tests": gate["go_tests"],
        "go_package": gate["go_package"],
        "exit_code": exit_code,
        "timed_out": timed_out,
        "error": error,
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
    require_object(value, "check attempt record", ATTEMPT_KEYS, ATTEMPT_KEYS)
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
        "gates": entries,
    }


def verify_attempt_artifacts(
    run_dir: pathlib.Path, task: dict[str, Any], sealed_hash: str, run: dict[str, Any],
    name: str, raw: bytes, attempt: dict[str, Any],
) -> dict[str, Any]:
    if attempt["task_sha256"] != run["task_sha256"] or attempt["routes_sha256"] != run["routes_sha256"]:
        raise Refused("the check attempt is not bound to the frozen task and routes of this run")
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


def validate_phase_extra(name: str, value: Any, source: str) -> Any:
    if name == "limitations":
        return require_text_list(value, source, minimum=0, unique=True, max_length=1000)
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
    tokens: dict[str, int | None] = {}
    for field in token_fields:
        values = [row["tokens"][field] for row in model_rows]
        tokens[field] = None if not values or any(value is None for value in values) else sum(values)
        tokens["known_" + field + "_subtotal"] = sum(value for value in values if value is not None)
    unknown_token_rows = sum(
        1 for row in model_rows if any(row["tokens"][field] is None for field in token_fields)
    )
    reported_values = [row["reported_cost"] for row in model_rows]
    estimated_values = [row["estimated_cost"] for row in model_rows]
    reported_total = None if not model_rows or any(value is None for value in reported_values) else sum(reported_values)
    estimated_total = None if not model_rows or any(value is None for value in estimated_values) else sum(estimated_values)
    unknown_cost_rows = sum(
        1 for row in model_rows if row["reported_cost"] is None and row["estimated_cost"] is None
    )
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
    if unknown_cost_rows:
        reasons.append(f"unknown cost in {unknown_cost_rows} model phase(s)")
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
            "exit_code": gate["exit_code"],
            "timed_out": gate["timed_out"],
            "passed": gate["passed"],
            "go_tests": gate["go_tests"],
            "go_package": gate["go_package"],
            "validator_exit_code": gate["validator_exit_code"],
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
            "known_reported_cost_subtotal": sum(value for value in reported_values if value is not None),
            "known_estimated_cost_subtotal": sum(value for value in estimated_values if value is not None),
            "price_versions": sorted({row["price_version"] for row in model_rows if row["price_version"] is not None}),
            "accounting_complete": not reasons,
            "incomplete_reasons": reasons,
        },
        "wall_time": wall_time,
        "review_time": sum(row["wall_time"] for row in rows if row["stage"] == "review"),
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
        },
        "subject_hash": sealed["subject_hash"],
        "review": review_block,
        "checks": checks,
        "evidence": {
            "tooling_sources": {
                path.name: sha256_hex(read_bytes(path, MAX_RECORD_BYTES, "evaluation tooling source"))
                for path in (pathlib.Path(__file__).resolve(), VALIDATOR_SCRIPT, SOURCE_SUBJECT_SCRIPT)
            },
            "frozen_task_sha256": run["task_sha256"],
            "frozen_routes_sha256": run["routes_sha256"],
            "frozen_review_sha256": run.get("review_sha256"),
            "frozen_usage_sha256": run["usage_sha256"],
            "check_attempt": check_attempt,
        },
        "phases": rows,
    }


def derive_receipt(run_dir: pathlib.Path) -> dict[str, Any]:
    run = load_run(run_dir)
    task = load_frozen_task(run_dir, run)
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
    if "usage_sha256" not in run:
        raise Refused("the run has no frozen usage rows; run receipt with --usage first")
    usage_raw = read_bytes(run_dir / "frozen" / "usage.json", MAX_USAGE_BYTES, "frozen usage JSON")
    if sha256_hex(usage_raw) != run["usage_sha256"]:
        raise Refused("the frozen usage rows changed after they were imported")
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

    created_here = not run_dir.exists()
    if created_here:
        try:
            run_dir.mkdir(parents=True)
        except OSError as error:
            raise Refused(f"cannot create the run directory: {error}")
    try:
        return finish_prepare(arguments, run_dir, task, task_raw, routes_raw)
    except Refused:
        if created_here:
            shutil.rmtree(run_dir, ignore_errors=True)
        raise


def finish_prepare(
    arguments: argparse.Namespace, run_dir: pathlib.Path, task: dict[str, Any],
    task_raw: bytes, routes_raw: bytes,
) -> int:
    source = str(arguments.source)
    checkout = run_dir / "checkout"
    try:
        clone = subprocess.run(
            ["git", "-c", f"safe.directory={source}", "clone", "--quiet", "--no-hardlinks", source, str(checkout)],
            capture_output=True, check=False,
        )
    except OSError as error:
        raise Refused(f"could not run git clone: {error}")
    if clone.returncode != 0:
        detail = clone.stderr.decode("utf-8", "replace").strip()
        raise Refused(f"could not clone {source}: {detail}")
    git_output(str(checkout), "checkout", "--quiet", "--detach", task["base"])
    head = require_commit(git_text(str(checkout), "rev-parse", "HEAD"), "isolated checkout HEAD")
    if head != task["base"]:
        raise Refused("the isolated checkout is not at the frozen base commit")
    try:
        base_subject = load_source_subject()(str(checkout), task["base"])
    except SystemExit as refusal:
        raise Refused(f"the isolated checkout is not a clean clone at the frozen base: {refusal}")
    except (OSError, subprocess.SubprocessError) as error:
        raise Refused(f"could not qualify the isolated checkout: {error}")

    frozen = run_dir / "frozen"
    write_bytes(frozen / "task.json", task_raw)
    write_bytes(frozen / "routes.json", routes_raw)
    run = {
        "schema": SCHEMA,
        "task_id": task["task_id"],
        "base": task["base"],
        "source": source,
        "head": task["base"],
        "base_tree_digest": base_subject["tree_digest"],
        "task_sha256": sha256_hex(task_raw),
        "routes_sha256": sha256_hex(routes_raw),
        "environment": environment_record(),
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


def command_seal(arguments: argparse.Namespace) -> int:
    run_dir = pathlib.Path(arguments.run_dir)
    run = load_run(run_dir)
    task = load_frozen_task(run_dir, run)
    checkout = run_dir / "checkout"
    verify_base_commit(checkout, task["base"])
    head = require_commit(git_text(str(checkout), "rev-parse", "HEAD"), "checkout HEAD")
    base_files = list_base_paths(checkout, task["base"])
    changes = collect_changes(checkout, task["base"])
    sealed_changes = validate_changes(task, base_files, changes)
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
    run_dir = pathlib.Path(arguments.run_dir)
    run = load_run(run_dir)
    task = load_frozen_task(run_dir, run)
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
        result = execute_gate(run_dir / "checkout", gate, attempt_dir / gate["id"], timeout)
        result["subject_hash_before"] = sealed["subject_hash"]
        after = current_subject(run_dir, task)["subject_hash"]
        result["subject_hash_after"] = after
        if after != sealed["subject_hash"]:
            result["passed"] = False
            result["error"] = (result["error"] + "; " if result["error"] else "") + \
                "source changed while executing this gate"
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
    attempt = {
        "schema": SCHEMA,
        "attempt_id": attempt_name,
        "task_id": task["task_id"],
        "task_sha256": run["task_sha256"],
        "routes_sha256": run["routes_sha256"],
        "subject_hash": sealed["subject_hash"],
        "complete": complete,
        "passed": attempt_passed and not stopped_early and complete,
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
    validate_usage(usage_raw)
    write_bytes(run_dir / "frozen" / "usage.json", usage_raw)
    save_run(run_dir, run, usage_sha256=sha256_hex(usage_raw))
    receipt = derive_receipt(run_dir)
    receipt_raw = write_json(run_dir / "receipt.json", receipt)
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
        return None if left == right else f"{path}: {left!r} != {right!r}"
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
    difference = first_difference(submitted, expected)
    if difference is not None:
        raise Refused("the submitted receipt does not match the derived evidence at " + difference)
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
    prepare.set_defaults(handler=command_prepare)

    seal = subparsers.add_parser("seal", help="bind the checkout into one exact evaluation Subject")
    seal.add_argument("--run-dir", required=True)
    seal.set_defaults(handler=command_seal)

    check = subparsers.add_parser("check", help="execute the frozen gates and require named Go tests")
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


if __name__ == "__main__":
    raise SystemExit(main())
