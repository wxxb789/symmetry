#!/usr/bin/env python3
"""Opt-in real GitHub/PostgreSQL Control-broker witness; never retrieves credentials.

Run from the repository root, after migrating the isolated DB:
  python3 scripts/ci/credentialed-provider-witness.py \
    --config .tmp/0006h-witness-config.json --evidence-dir .tmp/0006h-live-UNIQUE \
    --allow-live-fixture

Environment names: MIX_ENV (forced test), POSTGRES_HOST, POSTGRES_PORT (55436),
POSTGRES_DB (symmetry_0006h_test), POSTGRES_USER, POSTGRES_PASSWORD, PATH.
PATH must resolve gh to the operator's Windows gh.exe keyring wrapper. Do not set
GH_TOKEN/GITHUB_TOKEN; production Auth alone obtains transient authorization.

Config JSON: namespace (fresh, safe slug), resource_id (fresh UUID), success_subject
and failure_subject (complete Subjects with that resource_id), source_branch,
base_branch (main), failure_branch (same commit as main), code_base_sha,
code_head_sha, changed_source_manifest (list of {path, sha256}), and
allow_manifest_bound_dirty:true when using an uncommitted reviewed patch. The
manifest must include every dirty production file under control/lib, control/priv,
control/config, control/mix.exs, control/mix.lock and these two scripts.
Parent/operator creates the authorized branches/commit and exact Subjects first.
Optional subject_preparation_evidence links the parent's independently generated
GitHub tree/blob digest evidence; this runner verifies the exact provider refs.
An existing evidence directory is refused. --recover-existing only resumes the
read-only recovery/final evidence phases of a preserved crashed run; never setup.
The validated checkout is force-compiled before any phase starts; compilation
failure prevents broker startup and is recorded without raw compiler output.
Those phases seed separate accepted intents to prove the global broker sweep
cannot send POST/PATCH through the real transport; only GET is permitted.
An expired origin lease still prevents exact terminal replay; this runner never
renews the original fence and does not support that delayed-recovery case.

Artifacts: input.json, source.json, commands.jsonl, http.jsonl (flushed provider
acknowledgements, safe request bindings and rate limits), state.json (IDs only),
phase-*.json, snapshots-*.json, barrier-*.json, receipt.json. No remote cleanup.
Failure leaves artifacts and durable unknown outcomes intact. A held real HTTPS
response simulates local response loss, not an actual upstream network timeout.
Native harness/daemon behavior and monetary billing are not proved by this script.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time


FIXTURE = "wxxb789/symmetry-0006h-provider-fixture"
BASE = "551e0057e4829816587eaa8efb39f0b18eaa6445"
SECRET = re.compile(rb"(?:gh[pousr]_[A-Za-z0-9_]{8,}|github_pat_[A-Za-z0-9_]+|Bearer\s+[A-Za-z0-9._~+/-]{12,})", re.I)


def require(value, code):
    if not value:
        raise RuntimeError(code)


def encoded(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode()


def safe_write(path, value):
    data = encoded(value)
    require(not SECRET.search(data), "credential_pattern_in_evidence")
    with open(path, "xb") as stream:
        os.chmod(path, 0o600)
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())


def git(root, *args):
    result = subprocess.run(["git", *args], cwd=root, stdout=subprocess.PIPE,
                            stderr=subprocess.DEVNULL, check=False)
    require(result.returncode == 0, "source_inspection_failed")
    return result.stdout


def source(root, config):
    require(git(root, "rev-parse", "HEAD").decode().strip() == config["code_head_sha"],
            "code_head_mismatch")
    require(config["code_base_sha"] == BASE, "code_base_mismatch")
    entries = config["changed_source_manifest"]
    require(isinstance(entries, list) and entries, "source_manifest_required")
    paths = set()
    for entry in entries:
        rel = entry["path"]
        path = (root / rel).resolve()
        require(path.is_relative_to(root) and path.is_file() and rel not in paths,
                "invalid_source_manifest_path")
        paths.add(rel)
        require(hashlib.sha256(path.read_bytes()).hexdigest() == entry["sha256"].removeprefix("sha256:"),
                "changed_source_digest_mismatch")
    dirty = git(root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--",
                "control/lib", "control/priv", "control/config", "control/mix.exs", "control/mix.lock",
                "scripts/ci/credentialed-provider-witness.py",
                "scripts/ci/credentialed-provider-witness.exs").split(b"\0")
    relevant = set()
    i = 0
    while i < len(dirty):
        record = dirty[i]
        i += 1
        if not record:
            continue
        status, name = record[:2], record[3:].decode()
        if b"R" in status or b"C" in status:
            i += 1
        if name.startswith(("control/lib/", "control/priv/", "control/config/")) or name in {
            "control/mix.exs", "control/mix.lock",
            "scripts/ci/credentialed-provider-witness.py",
            "scripts/ci/credentialed-provider-witness.exs",
        }:
            relevant.add(name)
    require(relevant <= paths, "dirty_source_missing_from_manifest")
    require(not relevant or config.get("allow_manifest_bound_dirty") is True,
            "dirty_source_requires_explicit_manifest_consent")
    return {"head_sha": config["code_head_sha"], "base_sha": BASE,
            "changed_source_manifest": entries, "manifest_bound_dirty_paths": sorted(relevant),
            "manifest_sha256": hashlib.sha256(encoded(entries)).hexdigest()}


def scan_evidence(directory):
    count = 0
    for path in directory.iterdir():
        if path.is_file():
            require(not SECRET.search(path.read_bytes()), "credential_pattern_in_evidence")
            count += 1
    return count


def safe_diagnostic(value):
    names = {"exception_module", "stack_module", "stack_function"}
    keys = names | {"stack_arity", "source_file", "source_line"}
    if not isinstance(value, dict) or set(value) != keys:
        return None
    for key in names:
        item = value[key]
        if item is not None and not (isinstance(item, str) and len(item) <= 160 and
                                     re.fullmatch(r"[A-Za-z0-9_.!?]+", item)):
            return None
    file = value["source_file"]
    if file is not None and not (isinstance(file, str) and len(file) <= 200 and
                                 re.fullmatch(r"[A-Za-z0-9_.-]+", file)):
        return None
    for key, lower, upper in [("stack_arity", 0, 255), ("source_line", 1, 1_000_000)]:
        item = value[key]
        if item is not None and not (type(item) is int and lower <= item <= upper):
            return None
    return value


def compile_validated_source(root, config, observed_source, directory, environment, recovering):
    require(source(root, config) == observed_source, "source_changed_before_compile")
    command = ["mix", "compile", "--force"]
    started = time.monotonic()
    try:
        result = subprocess.run(command, cwd=root / "control", env=environment,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180, check=False)
        require(not SECRET.search(result.stdout + result.stderr), "credential_pattern_in_process_output")
        code = result.returncode
    except subprocess.TimeoutExpired as error:
        require(not SECRET.search((error.stdout or b"") + (error.stderr or b"")),
                "credential_pattern_in_process_output")
        code = -1
    safe_write(directory / ("compile-recovery.json" if recovering else "compile.json"), {
        "command": command, "exit_code": code,
        "elapsed_seconds": round(time.monotonic() - started, 3),
        "manifest_sha256": observed_source["manifest_sha256"],
        "process_output": "discarded; credential-pattern checked before discard",
    })
    require(code == 0, "validated_source_compile_failed")
    require(source(root, config) == observed_source, "source_changed_during_compile")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--config", required=True, type=Path)
    parser.add_argument("--evidence-dir", required=True, type=Path)
    parser.add_argument("--allow-live-fixture", action="store_true", required=True)
    parser.add_argument("--recover-existing", action="store_true")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    directory = args.evidence_dir.resolve()
    config = json.loads(args.config.read_text(encoding="utf-8"))
    require(re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{7,79}", config["namespace"]), "invalid_namespace")
    require(config["base_branch"] == "main", "invalid_base_branch")
    require(not os.environ.get("GH_TOKEN") and not os.environ.get("GITHUB_TOKEN"), "environment_provider_token_forbidden")
    require(os.environ.get("POSTGRES_PORT") == "55436" and
            os.environ.get("POSTGRES_DB") == "symmetry_0006h_test" and
            os.environ.get("POSTGRES_HOST", "localhost") in {"localhost", "127.0.0.1"},
            "isolated_postgresql_required")
    observed_source = source(root, config)
    if args.recover_existing:
        require(directory.is_dir(), "missing_recovery_evidence")
        require(json.loads((directory / "input.json").read_text()) == config, "recovery_input_mismatch")
        require((directory / "barrier-crash.json").is_file(), "missing_crash_barrier")
        require(not (directory / "restart-before-broker.json").exists(), "recovery_already_attempted")
        phases = ["recover", "finish"]
    else:
        directory.mkdir(mode=0o700, parents=True, exist_ok=False)
        safe_write(directory / "input.json", config)
        safe_write(directory / "source.json", observed_source)
        phases = ["setup", "timeout", "crash", "recover", "finish"]
    environment = os.environ.copy()
    environment["MIX_ENV"] = "test"
    environment["SYMMETRY_CREDENTIALED_PROVIDER_WITNESS"] = "approved-fixture-only"
    script = root / "scripts/ci/credentialed-provider-witness.exs"
    compile_validated_source(root, config, observed_source, directory, environment, args.recover_existing)
    for phase in phases:
        require(source(root, config) == observed_source, "source_changed_between_phases")
        command = ["mix", "run", "--no-start", "--no-compile", str(script), phase, str(directory)]
        started = time.monotonic()
        try:
            result = subprocess.run(command, cwd=root / "control", env=environment,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180, check=False)
            # Raw process output is never written or printed: exceptions may contain headers.
            require(not SECRET.search(result.stdout + result.stderr), "credential_pattern_in_process_output")
            code = result.returncode
            failure_reason = None
            diagnostic = None
            if code not in {0, 86}:
                for line in result.stderr.splitlines():
                    try:
                        failure = json.loads(line)
                    except (ValueError, UnicodeError):
                        continue
                    reason = failure.get("reason") if isinstance(failure, dict) else None
                    if isinstance(reason, str) and re.fullmatch(r"[a-z_]+", reason):
                        failure_reason = reason
                        diagnostic = safe_diagnostic(failure.get("diagnostic"))
        except subprocess.TimeoutExpired:
            code = -1
            failure_reason = "phase_deadline_expired"
            diagnostic = None
        row = {"phase": phase, "command": command, "exit_code": code,
               "elapsed_seconds": round(time.monotonic() - started, 3),
               "expected_exit_code": 86 if phase == "crash" else 0,
               "process_output": "discarded; credential-pattern checked before discard",
               "failure_reason": failure_reason, "diagnostic": diagnostic}
        with open(directory / "commands.jsonl", "ab") as stream:
            os.chmod(directory / "commands.jsonl", 0o600)
            stream.write(json.dumps(row, sort_keys=True).encode() + b"\n")
            stream.flush()
            os.fsync(stream.fileno())
        scan_evidence(directory)
        require(code == row["expected_exit_code"], "phase_failed_" + phase)
        require((directory / ("barrier-crash.json" if phase == "crash" else "phase-" + phase + ".json")).is_file(),
                "phase_receipt_missing_" + phase)
    require(source(root, config) == observed_source, "source_changed_during_witness")
    scanned = scan_evidence(directory)
    print(json.dumps({"status": "passed_with_limitations", "surface": "Control broker; standalone Linux BEAM processes",
                      "fixture": FIXTURE, "receipt": str(directory / "receipt.json"),
                      "redaction_files_scanned": scanned}, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # Only fixed local reason codes are exposed; never stringify arbitrary exceptions.
        code = str(error) if type(error) is RuntimeError and re.fullmatch(r"[a-z_]+", str(error)) else "witness_failed"
        print(json.dumps({"status": "failed", "reason": code}), file=sys.stderr)
        sys.exit(1)
