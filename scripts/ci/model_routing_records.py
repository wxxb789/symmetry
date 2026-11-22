from __future__ import annotations

import fnmatch
import hashlib
import importlib.util
import json
import math
import pathlib
import re
from collections.abc import Iterable
from typing import Any

SCHEMA = "symmetry-model-routing-evaluation/v1"

STAGES = ("exploration", "implementation", "verification", "review", "repair", "failed_attempt")
SEVERITIES = ("critical", "major", "minor", "info")
FINDING_STATUSES = ("open", "resolved")


MAX_TASK_BYTES = 1 << 20
MAX_ROUTES_BYTES = 1 << 16
MAX_RECORD_BYTES = 1 << 20
MAX_USAGE_BYTES = 8 << 20
MAX_RECEIPT_BYTES = 16 << 20


MAX_PROVISIONING_LOG_BYTES = 8 << 20


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


TEST_SEGMENTS = {"test", "tests", "__tests__"}
TEST_FILE_PATTERN = re.compile(
    r"(_test\.go$|^test_.*\.py$|_test\.py$|^test_.*\.exs$|_test\.exs$"
    r"|\.test\.[^/]+$|\.spec\.[^/]+$)"
)
PROTECTED_PREFIXES = (".github/", "contracts/", ".agents/skills/")
PROTECTED_BASENAMES = {"AGENTS.md", "CLAUDE.md"}

TASK_KEYS = {
    "task_id",
    "base",
    "language",
    "risk_class",
    "workflow",
    "outcome",
    "explanations",
    "allowed_paths",
    "protected_paths",
    "gates",
}
GATE_KEYS = {
    "id",
    "cwd",
    "argv",
    "go_tests",
    "go_package",
    "exunit_tests",
    "exunit_files",
    "vitest_tests",
    "vitest_files",
    "env",
}
REQUIRED_GATE_KEYS = {"id", "cwd", "argv"}
ROUTES_KEYS = {"worker", "reviewer"}
ROLE_KEYS = {"model_id", "provenance", "credential_route", "authorization_ref"}
RUN_KEYS = {
    "schema",
    "task_id",
    "base",
    "source",
    "head",
    "base_tree_digest",
    "task_sha256",
    "routes_sha256",
    "environment",
    "review_sha256",
    "usage_sha256",
}
REQUIRED_RUN_KEYS = {
    "schema",
    "task_id",
    "base",
    "source",
    "head",
    "base_tree_digest",
    "task_sha256",
    "routes_sha256",
    "environment",
}
ENVIRONMENT_KEYS = {"platform", "machine", "python_version", "git_version"}
PROVISIONING_KEYS = {
    "tree",
    "tree_digest",
    "tree_entries",
    "install_log",
    "install_log_source",
    "install_log_sha256",
    "install_log_bytes",
}
SEALED_KEYS = {
    "schema",
    "task_id",
    "base",
    "head",
    "manifest_digest",
    "content_revision",
    "subject_hash",
    "file_count",
    "changed_paths",
}
CHANGE_KEYS = {"path", "status"}
REVIEW_KEYS = {
    "subject_hash",
    "routes_sha256",
    "check_attempt_id",
    "check_evidence_sha256",
    "reviewer",
    "worker",
    "verdict",
    "gate_weakening",
    "findings",
    "explanations",
}
REVIEWER_KEYS = {"model_id", "session_ref"}
WORKER_KEYS = {"model_id", "session_refs"}
FINDING_KEYS = {"id", "severity", "status", "description"}
USAGE_KEYS = {"phases", "repair_count", "wall_time"}
PHASE_KEYS = {
    "stage",
    "session_ref",
    "non_model_reason",
    "model_ids",
    "prompt_hashes",
    "tokens",
    "reported_cost",
    "estimated_cost",
    "cost_provenance",
    "price_version",
    "wall_time",
}
TOKEN_KEYS = {"input", "output", "cached"}
# The extractor in model-routing-omp-usage.py emits these per-field observed
# subtotals for partial sessions; keep both lists in sync with its TOKEN_FIELDS,
# COST_FIELDS and assistant-message counters.
KNOWN_SUBTOTAL_MESSAGE_KEYS = (
    "assistant_messages",
    "assistant_messages_with_usage",
    "assistant_messages_with_cost_total",
)
KNOWN_SUBTOTAL_TOKEN_FIELDS = (
    "input",
    "output",
    "cacheRead",
    "cacheWrite",
    "cached",
    "totalTokens",
    "reasoningTokens",
)
KNOWN_SUBTOTAL_COST_FIELDS = ("input", "output", "cacheRead", "cacheWrite", "total")
KNOWN_SUBTOTAL_ENTRY_KEYS = {"sum", "messages"}
PHASE_OPTIONAL_KEYS = {"metadata", "limitations", "known_subtotals"}
MODEL_ID_KEYS = {"model", "provider", "api", "assistant_messages"}
PROMPT_HASH_KEYS = {"kind", "sha256", "index"}
PROMPT_HASH_REQUIRED_KEYS = {"kind", "sha256"}
SHA256_FIELD_PATTERN = re.compile(r"^(sha256:)?[0-9a-f]{64}$")
ATTEMPT_KEYS = {
    "schema",
    "attempt_id",
    "task_id",
    "task_sha256",
    "routes_sha256",
    "subject_hash",
    "complete",
    "passed",
    "gates",
    "dependency_provisioning",
}
# The provisioning view is optional on read so an attempt written before this member
# existed still parses (its `None` then refuses any run that binds a later tree).
REQUIRED_ATTEMPT_KEYS = ATTEMPT_KEYS - {"dependency_provisioning"}
ATTEMPT_ENTRY_KEYS = {
    "id",
    "passed",
    "result_sha256",
    "stdout_sha256",
    "stderr_sha256",
    "validator_exit_code",
    "validator_stdout_sha256",
    "validator_stderr_sha256",
}


class Refused(Exception):
    """A refusal that must never be recorded as accepted evidence."""


# Recorded measured-surface values must not republish the operator profile or a
# disposable evaluation root; the shared host-path definition loads lazily from
# `model-routing-record-sanitize.py`, and a sibling that cannot be read, parsed or
# executed refuses cleanly instead of raising.


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
            raise Refused(
                f"cannot load model-routing-record-sanitize.py for the shared host-path patterns: {error}"
            ) from error
        patterns = getattr(module, "HOST_PATH_PATTERNS", None)
        if patterns is None:
            raise Refused(
                "cannot load model-routing-record-sanitize.py for the shared host-path patterns: "
                "it defines no HOST_PATH_PATTERNS"
            )
        _HOST_PATH_PATTERNS = patterns
    return _HOST_PATH_PATTERNS


def sha256_hex(raw: bytes) -> str:
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def strict_json(raw: bytes, source: str, limit: int) -> Any:
    if len(raw) > limit:
        raise Refused(f"{source} exceeds {limit} bytes")
    try:
        text = raw.decode("utf-8", errors="strict")
    except UnicodeDecodeError:
        raise Refused(f"{source} is not valid UTF-8") from None

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
        raise Refused(f"{source} is not valid JSON: {error}") from error
    except ValueError:
        # `json.loads` refuses an integer literal past its conversion-digit limit
        # with a plain ValueError; that is a refusal about the document's numbers,
        # not a crash.
        raise Refused(f"{source} carries an integer outside the supported range") from None
    except RecursionError:
        raise Refused(f"{source} nests too deeply") from None


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
        raise Refused("value nests too deeply to serialize") from None
    except UnicodeEncodeError:
        raise Refused("value contains text that is not valid UTF-8") from None
    except ValueError:
        # `allow_nan=False` would report a nan or inf float here, and every one was
        # checked above; what remains is a value Python cannot serialize - an integer
        # past its conversion-digit limit - which must not be called non-finite.
        raise Refused("value contains a number outside the serializable range") from None
    try:
        return text.encode("utf-8") + b"\n"
    except UnicodeEncodeError:
        raise Refused("value contains text that is not valid UTF-8") from None


def read_bytes(path: pathlib.Path, limit: int, source: str) -> bytes:
    try:
        with open(path, "rb") as stream:
            raw = stream.read(limit + 1)
    except OSError as error:
        raise Refused(f"cannot read {source} at {path}: {error}") from error
    if len(raw) > limit:
        raise Refused(f"{source} at {path} exceeds {limit} bytes")
    return raw


def write_bytes(path: pathlib.Path, raw: bytes) -> None:
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "wb") as stream:
            stream.write(raw)
    except OSError as error:
        raise Refused(f"cannot write {path}: {error}") from error


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
        raise Refused(f"{source} must be a finite number >= {minimum}") from None
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
        raise Refused(f"{source} exceeds the finite number range") from None


def require_value_in(value: Any, source: str, allowed: Iterable[str]) -> str:
    options = tuple(allowed)
    if not isinstance(value, str) or value not in options:
        raise Refused(f"{source} must be one of: {', '.join(options)}")
    return value


def require_text_list(value: Any, source: str, minimum: int, unique: bool, max_length: int = 4096) -> list[str]:
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
                parts = ("**", *parts)
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
        "mix",
        "mix.exe",
        "mix.bat",
        "mix.cmd",
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
        if not isinstance(item, str) or not item or len(item) > 300 or "\0" in item or "\r" in item or "\n" in item:
            raise Refused(f"{source} {name} must be a non-empty single-line string without NUL, at most 300 characters")
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
        "install_log_source": require_text(block["install_log_source"], source + " install_log_source", max_length=300),
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
        route.startswith("/")
        or "\\" in route
        or re.match(r"^[A-Za-z]:", route)
        or any(part == "" for part in route.split("/"))
    ):
        raise Refused(
            f"{source} tree must be a run-relative path so a reader can recompute the digest "
            "from the stored run directory"
        )
    return provisioning


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
        environment["measured"] = validate_measured_mapping(block["measured"], "run record environment measured")
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
        findings.append(
            {
                "id": finding_id,
                "severity": require_value_in(entry["severity"], source + " severity", SEVERITIES),
                "status": require_value_in(entry["status"], source + " status", FINDING_STATUSES),
                "description": require_text(entry["description"], source + " description", max_length=2000),
            }
        )
    return {
        "subject_hash": require_digest(value["subject_hash"], "review record subject_hash"),
        "routes_sha256": require_digest(value["routes_sha256"], "review record routes_sha256"),
        "check_attempt_id": attempt_id,
        "check_evidence_sha256": require_digest(value["check_evidence_sha256"], "review record check_evidence_sha256"),
        "reviewer": {
            "model_id": require_text(reviewer_block["model_id"], "review record reviewer model_id", max_length=300),
            "session_ref": require_text(
                reviewer_block["session_ref"], "review record reviewer session_ref", max_length=300
            ),
        },
        "worker": {
            "model_id": require_text(worker_block["model_id"], "review record worker model_id", max_length=300),
            "session_refs": require_text_list(
                worker_block["session_refs"],
                "review record worker session_refs",
                minimum=1,
                unique=True,
                max_length=300,
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
                f"the review record binds {review['check_attempt_id']} but the latest sealed-subject "
                f"attempt is {attempt_id}"
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
                field: validate_known_subtotal_entry(block[field], f"{source} cost.{field}") for field in sorted(block)
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
        non_model_reason = require_optional_text(
            phase["non_model_reason"], source + " non_model_reason", max_length=1000
        )
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
                raise Refused(
                    f"{source} duplicates a model session; duplicate model session costs cannot be counted twice"
                )
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
