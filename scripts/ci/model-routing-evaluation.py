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
import json
import os
import pathlib
import platform as host_platform
import shutil
import subprocess
import sys
from typing import Any

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

from model_routing_gates import (
    DEFAULT_TIMEOUT,
    SOURCE_CHANGED_SUFFIX,
    _dependency_tree,
    dependency_tree_digest,
    execute_gate,
    require_resolved_dependency_tree,
    verify_dependency_tree,
)
from model_routing_receipts import (
    ENTRY_SCRIPT,
    SPLIT_SCRIPTS,
    attempt_names,
    derive_receipt,
    load_frozen_routes,
    load_frozen_task,
    load_run,
    load_sealed,
    provisioning_view,
    require_dependency_provisioning,
    require_passing_attempt,
    run_record_bytes,
    save_run,
)
from model_routing_records import (
    MAX_PROVISIONING_LOG_BYTES,
    MAX_RECEIPT_BYTES,
    MAX_RECORD_BYTES,
    MAX_ROUTES_BYTES,
    MAX_TASK_BYTES,
    MAX_USAGE_BYTES,
    PROVISIONING_LOG_PATH,
    SCHEMA,
    Refused,
    canonical_json,
    read_bytes,
    redact_host_paths,
    require_commit,
    review_semantic_blockers,
    sanitize_source_digest,
    scan_no_secret_keys,
    sha256_hex,
    strict_json,
    validate_measured_mapping,
    validate_provisioning,
    validate_review_structure,
    validate_routes,
    validate_task,
    write_bytes,
    write_json,
)
from model_routing_subject import (
    collect_changes,
    compute_subject,
    current_subject,
    git_output,
    git_text,
    list_base_paths,
    load_source_subject,
    refuse_object_substitution,
    require_current_subject,
    validate_changes,
    verify_base_commit,
)

_TOOL_DIRECTORY = pathlib.Path(__file__).resolve().parent
_TOOL_MODULES = tuple(path.stem for path in SPLIT_SCRIPTS)


def executed_matches_source(module: Any) -> bool:
    cached = module.__spec__.cached
    if cached is None or not pathlib.Path(cached).is_file():
        return True
    source = pathlib.Path(module.__file__)
    if source.suffix != ".py":
        return False
    executed = module.__spec__.loader.get_code(module.__spec__.name)
    return executed == compile(source.read_bytes(), module.__file__, "exec", dont_inherit=True)


def tooling_binding_problem() -> str | None:
    directories = sorted({str(pathlib.Path(sys.modules[name].__file__).resolve().parent) for name in _TOOL_MODULES})
    if [str(_TOOL_DIRECTORY)] != directories:
        return "the sibling modules did not resolve beside the entry: " + ", ".join(directories)
    if pathlib.Path(__file__).resolve() != ENTRY_SCRIPT:
        return f"the entry is {pathlib.Path(__file__).resolve()} but the receipt binds {ENTRY_SCRIPT}"
    for name in _TOOL_MODULES:
        module = sys.modules[name]
        if not executed_matches_source(module):
            return f"{pathlib.Path(module.__file__).name} was executed from bytecode that does not match its source"
    return None


_binding_problem = tooling_binding_problem()
if _binding_problem is not None:
    print(f"model-routing-evaluation refused: {_binding_problem}", file=sys.stderr)
    raise SystemExit(1)


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
    measured = None
    if arguments.environment_json is not None:
        environment_raw = read_bytes(pathlib.Path(arguments.environment_json), MAX_RECORD_BYTES, "environment JSON")
        measured = validate_measured_mapping(
            strict_json(environment_raw, "environment JSON", MAX_RECORD_BYTES),
            "environment JSON",
            redact=True,
        )

    created_here = not run_dir.exists()
    if created_here:
        try:
            run_dir.mkdir(parents=True)
        except OSError as error:
            raise Refused(f"cannot create the run directory: {error}") from error
    try:
        return finish_prepare(arguments, run_dir, task, task_raw, routes_raw, measured)
    except Refused:
        if created_here:
            shutil.rmtree(run_dir, ignore_errors=True)
        raise


def finish_prepare(
    arguments: argparse.Namespace,
    run_dir: pathlib.Path,
    task: dict[str, Any],
    task_raw: bytes,
    routes_raw: bytes,
    measured: dict[str, str] | None = None,
) -> int:
    source = str(arguments.source)
    checkout = run_dir / "checkout"
    # The seal compares worktree bytes with object-store bytes, so the isolated
    # checkout must materialize the stored blob bytes: never apply a host-dependent
    # end-of-line rendering during prepare.
    eol_overrides = ["-c", "core.autocrlf=false", "-c", "core.eol=lf"]
    try:
        clone = subprocess.run(
            [
                "git",
                "-c",
                f"safe.directory={source}",
                *eol_overrides,
                "clone",
                "--quiet",
                "--no-hardlinks",
                source,
                str(checkout),
            ],
            capture_output=True,
            check=False,
        )
    except OSError as error:
        raise Refused(f"could not run git clone: {error}") from error
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
        raise Refused(f"the isolated checkout is not a clean clone at the frozen base: {refusal}") from refusal
    except (OSError, subprocess.SubprocessError) as error:
        raise Refused(f"could not qualify the isolated checkout: {error}") from error

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
    print(
        json.dumps(
            {
                "run_dir": str(run_dir),
                "task_id": task["task_id"],
                "base": task["base"],
                "head": task["base"],
                "task_sha256": run["task_sha256"],
                "routes_sha256": run["routes_sha256"],
                "base_tree_digest": run["base_tree_digest"],
            },
            indent=2,
            sort_keys=True,
        )
    )
    return 0


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
        require_resolved_dependency_tree(run_dir, task, tree)
        print(
            json.dumps(
                {
                    "verified": True,
                    "tree": recorded["tree"],
                    "tree_digest": digest,
                    "tree_entries": entries,
                },
                indent=2,
                sort_keys=True,
            )
        )
        return 0
    if recorded is not None:
        raise Refused("the run already records a provisioning step")
    if not arguments.log:
        raise Refused("bind-dependencies requires --log to record the provisioning log (or --check)")
    tree = _dependency_tree(arguments.tree)
    checkout = run_dir / "checkout"
    if checkout.exists() and (tree == checkout.resolve() or checkout.resolve() in tree.parents):
        raise Refused("the bound dependency tree must live outside the sealed checkout")
    require_resolved_dependency_tree(run_dir, task, tree)
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
        ) from None
    provisioning = validate_provisioning(
        {
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
        },
        "provisioning step",
    )
    environment = dict(run["environment"], provisioning=provisioning)
    # The log copy and the run record are both written only after every value that will
    # be recorded has been serialized by validate_provisioning, so a refusal leaves the
    # run unbound to a partial provisioning step.
    write_bytes(run_dir / PROVISIONING_LOG_PATH, log_raw)
    save_run(run_dir, run, environment=environment)
    print(
        json.dumps(
            {
                "tree": provisioning["tree"],
                "tree_digest": tree_digest,
                "tree_entries": tree_entries,
                "install_log": PROVISIONING_LOG_PATH,
                "install_log_sha256": provisioning["install_log_sha256"],
                "install_log_bytes": provisioning["install_log_bytes"],
            },
            indent=2,
            sort_keys=True,
        )
    )
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
            result["error"] = (result["error"] + "; " if result["error"] else "") + SOURCE_CHANGED_SUFFIX
            stopped_early = True
        result_raw = write_json(attempt_dir / gate["id"] / "result.json", result)
        verified.append(
            {
                "id": gate["id"],
                "passed": result["passed"],
                "result_sha256": sha256_hex(result_raw),
                "stdout_sha256": result["stdout_sha256"],
                "stderr_sha256": result["stderr_sha256"],
                "validator_exit_code": result["validator"]["exit_code"] if result["validator"] else None,
                "validator_stdout_sha256": result["validator"]["stdout_sha256"] if result["validator"] else None,
                "validator_stderr_sha256": result["validator"]["stderr_sha256"] if result["validator"] else None,
            }
        )
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
    print(
        json.dumps(
            {
                "attempt_id": attempt_name,
                "attempt_sha256": sha256_hex(attempt_raw),
                "complete": attempt["complete"],
                "passed": attempt["passed"],
                "subject_hash": sealed["subject_hash"],
            },
            indent=2,
            sort_keys=True,
        )
    )
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
        review,
        task,
        routes,
        run["routes_sha256"],
        sealed["subject_hash"],
        attempt["attempt_id"],
        attempt["sha256"],
    )
    if blockers:
        raise Refused("the review record is not acceptable: " + "; ".join(blockers))
    write_bytes(run_dir / "frozen" / "review.json", review_raw)
    save_run(run_dir, run, review_sha256=sha256_hex(review_raw))
    print(
        json.dumps(
            {
                "subject_hash": sealed["subject_hash"],
                "check_attempt_id": attempt["attempt_id"],
                "review_sha256": sha256_hex(review_raw),
                "verdict": review["verdict"],
            },
            indent=2,
            sort_keys=True,
        )
    )
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
    print(
        json.dumps(
            {
                "accepted": receipt["accepted"],
                "acceptance_blockers": receipt["acceptance_blockers"],
                "accounting_complete": receipt["cost"]["accounting_complete"],
                "receipt_sha256": sha256_hex(receipt_raw),
                "subject_hash": receipt["subject_hash"],
                "task_id": receipt["task_id"],
            },
            indent=2,
            sort_keys=True,
        )
    )
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
        for index, (left_item, right_item) in enumerate(zip(left, right, strict=True)):
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
    print(
        json.dumps(
            {
                "accepted": True,
                "receipt_sha256": sha256_hex(submitted_raw),
                "subject_hash": expected["subject_hash"],
                "task_id": expected["task_id"],
            },
            indent=2,
            sort_keys=True,
        )
    )
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
    bind.add_argument(
        "--check",
        action="store_true",
        help="recompute the tree digest and compare it with the recorded provisioning step",
    )
    bind.set_defaults(handler=command_bind_dependencies)

    seal = subparsers.add_parser("seal", help="bind the checkout into one exact evaluation Subject")
    seal.add_argument("--run-dir", required=True)
    seal.set_defaults(handler=command_seal)

    check = subparsers.add_parser("check", help="execute the frozen gates and require the named Go and ExUnit tests")
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
