from __future__ import annotations

import pathlib
from collections.abc import Callable
from typing import Any

from model_routing_gates import (
    VALIDATOR_SCRIPT,
    recompute_gate,
    require_resolved_dependency_tree,
    verify_dependency_tree,
)
from model_routing_records import (
    ATTEMPT_ENTRY_KEYS,
    ATTEMPT_KEYS,
    ATTEMPT_PATTERN,
    CHANGE_KEYS,
    MAX_PROVISIONING_LOG_BYTES,
    MAX_RECORD_BYTES,
    MAX_ROUTES_BYTES,
    MAX_TASK_BYTES,
    MAX_USAGE_BYTES,
    REQUIRED_ATTEMPT_KEYS,
    REQUIRED_RUN_KEYS,
    RUN_KEYS,
    SANITIZE_SCRIPT,
    SCHEMA,
    SEALED_KEYS,
    Refused,
    canonical_json,
    number_sum,
    read_bytes,
    require_bool,
    require_commit,
    require_digest,
    require_identifier,
    require_integer,
    require_object,
    require_optional_digest,
    require_optional_integer,
    require_recorded_redaction,
    require_text,
    requires_dependency_provisioning,
    review_semantic_blockers,
    scan_no_secret_keys,
    sha256_hex,
    strict_json,
    validate_attempt_provisioning,
    validate_environment,
    validate_review_structure,
    validate_routes,
    validate_task,
    validate_usage,
    validate_usage_against_review,
    write_bytes,
)
from model_routing_subject import SOURCE_SUBJECT_SCRIPT, require_current_subject

INCOMPLETE_CHECK_DETAIL = "not every frozen gate completed on the sealed subject"


ENTRY_SCRIPT = pathlib.Path(__file__).with_name("model-routing-evaluation.py").resolve()
SPLIT_SCRIPTS = tuple(
    pathlib.Path(__file__).with_name(name)
    for name in (
        "model_routing_records.py",
        "model_routing_subject.py",
        "model_routing_gates.py",
        "model_routing_receipts.py",
    )
)


def provisioning_view(provisioning: dict[str, Any]) -> dict[str, Any]:
    """The identity of one recorded provisioning step, as an attempt records it."""
    return {
        "tree": provisioning["tree"],
        "tree_digest": provisioning["tree_digest"],
        "tree_entries": provisioning["tree_entries"],
    }


def require_dependency_provisioning(run_dir: pathlib.Path, run: dict[str, Any], task: dict[str, Any]) -> None:
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
        raise Refused(f"the recorded dependency tree {provisioning['tree']!r} does not resolve: {error}") from error
    require_resolved_dependency_tree(run_dir, task, bound_resolved)


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
            MAX_PROVISIONING_LOG_BYTES,
            "stored provisioning log",
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
        entries.append(
            {
                "id": gate_id,
                "passed": require_bool(item["passed"], source + " passed"),
                "result_sha256": require_digest(item["result_sha256"], source + " result_sha256"),
                "stdout_sha256": require_digest(item["stdout_sha256"], source + " stdout_sha256"),
                "stderr_sha256": require_digest(item["stderr_sha256"], source + " stderr_sha256"),
                "validator_exit_code": require_optional_integer(
                    item["validator_exit_code"], source + " validator_exit_code", -4096
                ),
                "validator_stdout_sha256": require_optional_digest(
                    item["validator_stdout_sha256"], source + " validator_stdout_sha256"
                ),
                "validator_stderr_sha256": require_optional_digest(
                    item["validator_stderr_sha256"], source + " validator_stderr_sha256"
                ),
            }
        )
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
    run_dir: pathlib.Path,
    task: dict[str, Any],
    sealed_hash: str,
    run: dict[str, Any],
    name: str,
    raw: bytes,
    attempt: dict[str, Any],
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
    listed = sorted(entry.name for entry in attempt_dir.iterdir() if entry.name != "attempt.json")
    if listed != sorted(entry_ids):
        raise Refused("the check attempt directory contents do not match its record")
    verified = []
    for entry in attempt["gates"]:
        recomputed = recompute_gate(attempt_dir / entry["id"], gate_lookup[entry["id"]], sealed_hash)
        if entry["result_sha256"] != recomputed["result_sha256"]:
            raise Refused(f"the check attempt record digest for gate {entry['id']} does not match its result")
        for key in (
            "passed",
            "stdout_sha256",
            "stderr_sha256",
            "validator_exit_code",
            "validator_stdout_sha256",
            "validator_stderr_sha256",
        ):
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
            None
            if not values or any(value is None for value in values)
            else number_sum(values, f"the {field} token total")
        )
        tokens["known_" + field + "_subtotal"] = known_token_total(field)
    unknown_token_rows = sum(1 for row in model_rows if any(row["tokens"][field] is None for field in token_fields))
    reported_values = [row["reported_cost"] for row in model_rows]
    estimated_values = [row["estimated_cost"] for row in model_rows]
    reported_total = (
        None
        if not model_rows or any(value is None for value in reported_values)
        else number_sum(reported_values, "the reported cost total")
    )
    estimated_total = (
        None
        if not model_rows or any(value is None for value in estimated_values)
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
            for model, provider, api in sorted(
                {
                    (identity["model"], identity["provider"], identity["api"])
                    for row in rows
                    for identity in row["model_ids"]
                }
            )
        ],
        "prompt_hashes": [
            {"kind": kind, "sha256": sha256}
            for kind, sha256 in sorted(
                {(record["kind"], record["sha256"]) for row in rows for record in row["prompt_hashes"]}
            )
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
                    ENTRY_SCRIPT,
                    *SPLIT_SCRIPTS,
                    VALIDATOR_SCRIPT,
                    SOURCE_SUBJECT_SCRIPT,
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
        blockers.extend(
            review_semantic_blockers(
                review,
                task,
                routes,
                run["routes_sha256"],
                sealed["subject_hash"],
                attempt["attempt_id"] if attempt is not None else None,
                attempt["sha256"] if attempt is not None else None,
            )
        )
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
