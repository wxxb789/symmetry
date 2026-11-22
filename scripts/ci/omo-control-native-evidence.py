#!/usr/bin/env python3
"""Local exact-binary OmO + production daemon + HTTP Control evidence.

No fake-agent transport, new admissions, paid retries, Control lifecycle command,
operator configuration loading, or credential-bearing argv. The supplied fixture
has already passed real Goal admission. All failure artifacts are retained.
"""

import argparse
import base64
import contextlib
import ctypes
import hashlib
import http.client
import http.server
import json
import os
import pathlib
import platform
import re
import shutil
import socket
import subprocess
import sys
import threading
import time
import urllib.parse
import uuid

import source_subject

BASELINE = {
    "commit": "6adc9ef1a2f704590ee4172cecb4e0bb6c70408c",
    "tree_digest": "sha256:1831626e62b934f63cf3abd59d3e5ed73e5a21d898eccd6e7d349ae116a39d92",
}
PINS = {
    "Windows": (202352128, "04338dd63b24c53156492e1a0777d996034416c4131a26e1ba48eb07b5429464"),
    "Linux": (211998896, "3a74aba613cf02e740073b302171c0fb9d9155f5ff6069f91bea23972ffa1444"),
}
FIXED_ARGS = [
    "--mode",
    "rpc",
    "--no-extensions",
    "--no-skills",
    "--no-context-files",
    "--no-prompt-templates",
    "--no-themes",
    "--permission-preset",
    "workspace",
    "--tools",
    "read,write",
]
MAX_BODY = 16 << 20
SECRET_FIELDS = {
    "authorization",
    "machine_token",
    "lease_token",
    "enrollment_token",
    "operator_token",
    "api_key",
    "apikey",
    "secret",
    "pipe_token",
}
# `go version -m` build records the daemon identity is read from.
GO_BUILD_SETTINGS = ("vcs", "vcs.revision", "vcs.time", "vcs.modified")
# The registration endpoint Control exposes for one machine session. The
# identifiers are parsed and validated as UUIDs below; a pattern that only
# matched `[0-9a-f-]+` accepted `----` and `-` as a machine and session.
MACHINE_SESSION_ENDPOINT = re.compile(r"/api/v1/machines/([^/]+)/sessions/([^/]+)")
SOURCE_PATHS = [
    "scripts/ci/omo-control-native-evidence.py",
    "scripts/ci/source_subject.py",
    "scripts/ci/omo-control-native-fixture.exs",
    "daemon/cmd/symmetry-daemon/main.go",
    "daemon/internal/app/app.go",
    "daemon/internal/app/cleanup_glue.go",
    "daemon/internal/app/commands.go",
    "daemon/internal/app/events.go",
    "daemon/internal/app/goal_admission.go",
    "daemon/internal/app/goal_delivery.go",
    "daemon/internal/app/lease.go",
    "daemon/internal/app/native_run.go",
    "daemon/internal/app/outbox.go",
    "daemon/internal/app/probe.go",
    "daemon/internal/app/recovery_glue.go",
    "daemon/internal/config/config.go",
    "daemon/internal/control/client.go",
    "daemon/internal/control/validation.go",
    "daemon/internal/protocol/protocol.go",
    "daemon/internal/state/state.go",
    "daemon/internal/state/goal_session.go",
    "daemon/internal/harness/omo/adapter.go",
    "daemon/internal/harness/omo/probe.go",
    "daemon/internal/harness/omo/rpc.go",
    "daemon/internal/harness/omo/profile.go",
    "daemon/internal/harness/omo/validator.go",
    "daemon/internal/linuxpreflight/preflight_linux.go",
    "daemon/internal/linuxpreflight/preflight_unsupported.go",
    "daemon/internal/workspace/subject.go",
    "control/lib/symmetry_control/goals.ex",
    "control/lib/symmetry_control/goals/admission.ex",
    "control/lib/symmetry_control/goals/budget.ex",
    "control/lib/symmetry_control/goals/commands.ex",
    "control/lib/symmetry_control/goals/context_assembly.ex",
    "control/lib/symmetry_control/goals/contract_validation.ex",
    "control/lib/symmetry_control/goals/decisions.ex",
    "control/lib/symmetry_control/goals/events_receipts.ex",
    "control/lib/symmetry_control/goals/goal_control.ex",
    "control/lib/symmetry_control/goals/handoff.ex",
    "control/lib/symmetry_control/goals/harness_sessions.ex",
    "control/lib/symmetry_control/goals/internal.ex",
    "control/lib/symmetry_control/goals/plan_contracts.ex",
    "control/lib/symmetry_control/goals/reconciliation.ex",
    "control/lib/symmetry_control/goals/recovery.ex",
    "control/lib/symmetry_control/goals/run_fence.ex",
    "control/lib/symmetry_control/goals/settlement.ex",
    "control/lib/symmetry_control/goals/validation_outcome.ex",
    "contracts/v1/task-result.schema.json",
    "daemon/internal/app/command_queue.go",
    "daemon/internal/app/run_lifecycle.go",
    "daemon/internal/app/goal_admission_tx.go",
]


class Rejected(Exception):
    pass


def require(condition, reason):
    if not condition:
        raise Rejected(reason)


def canonical_uuid(value, label):
    """Return `value` when it is the canonical hyphenated UUID Control emits."""
    try:
        parsed = str(uuid.UUID(value))
    except (AttributeError, TypeError, ValueError):
        raise Rejected(
            f"successful registration has an invalid daemon-session endpoint: {label} is not a UUID"
        ) from None
    require(
        parsed == value.lower(),
        f"successful registration has an invalid daemon-session endpoint: {label} is not a canonical hyphenated UUID",
    )
    return parsed


def machine_session_ids(path):
    """Return the validated machine and daemon-session identifiers of `path`."""
    match = MACHINE_SESSION_ENDPOINT.fullmatch(path)
    require(match is not None, "successful registration has an invalid daemon-session endpoint")
    return canonical_uuid(match.group(1), "machine id"), canonical_uuid(match.group(2), "session id")


sha = source_subject.sha256_digest
file_identity = source_subject.file_identity


def workspace_files(workspace, git, env, receipt):
    names = command_bytes(
        [str(git), "-C", str(workspace), "ls-files", "--cached", "--others", "-z"], env, workspace, receipt
    )
    files = {}
    for raw in names.split(b"\0"):
        if not raw:
            continue
        name = raw.decode("utf-8", "strict")
        path = workspace / name
        require(
            path.resolve().is_relative_to(workspace) and path.is_file() and not path.is_symlink(),
            "workspace inventory contains missing, escaped or nonregular file",
        )
        identity = file_identity(path)
        files[name] = {key: identity[key] for key in ("bytes", "sha256")}
    return files


def parse_go_build_settings(output):
    """Return the embedded settings `go version -m` printed for one executable.

    Every record is `<kind>\\t<key>[=<value>]`. Only the parsed `build` records
    Go itself reports count, so a decoy `vcs.revision=` blob elsewhere in the
    image cannot win the way it won a byte search. A setting reported twice with
    different values is ambiguous evidence and is rejected, not resolved by
    position.
    """
    fields = {}
    for line in output.splitlines():
        # The tool indents every record line (`\tbuild\tvcs.revision=...`); the
        # header line and any blank line carry no tab-separated record.
        kind, tab, record = line.strip().partition("\t")
        if not tab or kind != "build" or "=" not in record:
            continue
        key, _, value = record.partition("=")
        if key not in GO_BUILD_SETTINGS:
            continue
        if key in fields and fields[key] != value:
            raise Rejected(f"go version -m reported conflicting {key} records for the production daemon binary")
        fields[key] = value
    return fields


def go_build_source(path):
    # `go version -m` is the supported reader for the module and build-setting
    # records Go embeds in ordinary production executables. Absence of a VCS
    # revision stays explicitly unverified, never inferred from the file name.
    go = shutil.which("go")
    require(go is not None, "explicit local Go executable unavailable; daemon build identity is unverified")
    result = subprocess.run([go, "version", "-m", str(path)], capture_output=True, text=True)
    require(result.returncode == 0, "go version -m refused the production daemon binary; build identity is unverified")
    fields = parse_go_build_settings(result.stdout)
    return {
        "embedded_build_info": fields,
        "source_revision": fields.get("vcs.revision", "unverified: production binary lacks embedded VCS revision"),
    }


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode("utf-8")


def credential_scan(root, secret):
    needles = [
        item.encode()
        for item in (
            secret,
            urllib.parse.quote(secret, safe=""),
            base64.b64encode(secret.encode()).decode(),
            json.dumps(secret)[1:-1],
        )
        if item
    ]
    if not needles:
        return {"classification": "not_exercised", "reason": "no provider credential supplied"}
    tail_size = max(map(len, needles)) - 1
    files, exposed = 0, []
    for path in root.rglob("*"):
        if not path.is_file() or path.is_symlink():
            continue
        files += 1
        carry = b""
        with path.open("rb") as stream:
            for chunk in iter(lambda: stream.read(1 << 20), b""):
                joined = carry + chunk
                if any(needle in joined for needle in needles):
                    exposed.append({"path": str(path), "contents": "withheld"})
                    break
                carry = joined[-tail_size:] if tail_size else b""
    return {
        "classification": "failed" if exposed else "passed",
        "files_scanned": files,
        "exposures": exposed,
        "scope": "private runner files after production daemon drain; never an environment dump",
    }


def parse_json(data, source):
    def object_pairs(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, source + ": duplicate JSON field rejected; raw contents withheld")
            result[key] = value
        return result

    def reject_constant(_value):
        raise Rejected(source + ": non-finite JSON value rejected; raw contents withheld")

    try:
        text = data.decode("utf-8", "strict") if isinstance(data, bytes) else data
        value = json.loads(text, object_pairs_hook=object_pairs, parse_constant=reject_constant)
        require(isinstance(value, dict), source + ": authoritative JSON object required")
        canonical(value).decode("utf-8", "strict")
        return value
    except (UnicodeError, ValueError):
        raise Rejected(source + ": invalid UTF-8/JSON rejected; raw contents withheld") from None


def read_json(path):
    require(path.stat().st_size <= MAX_BODY, "JSON artifact exceeds bounded size")
    with path.open("rb") as stream:
        data = stream.read(MAX_BODY + 1)
    require(len(data) <= MAX_BODY, "JSON artifact exceeds bounded size")
    return parse_json(data, "local JSON artifact")


class Receipt:
    def __init__(self, path, secrets):
        self.path = path
        self.lock = threading.RLock()
        self.secrets = set()
        for secret in secrets:
            if secret:
                self.secrets.update(
                    (
                        secret,
                        urllib.parse.quote(secret, safe=""),
                        base64.b64encode(secret.encode()).decode(),
                        json.dumps(secret)[1:-1],
                    )
                )
        self.value = {
            "schema_version": 1,
            "classification": "running",
            "passed": False,
            "events": [],
            "usage": {"classification": "unknown", "billing_verified": False, "hard_cost_verified": False},
            "recovery_classes": {},
            "failures": [],
        }

    def clean(self, value):
        if isinstance(value, dict):
            return {
                key: (
                    {"redacted": True, "sha256": sha(str(item).encode())}
                    if key.lower() in SECRET_FIELDS
                    else self.clean(item)
                )
                for key, item in value.items()
            }
        if isinstance(value, list):
            return [self.clean(item) for item in value]
        if isinstance(value, str):
            for secret in sorted(self.secrets, key=len, reverse=True):
                value = value.replace(secret, "[REDACTED]")
        return value

    def save(self):
        with self.lock:
            encoded = json.dumps(self.clean(self.value), indent=2, ensure_ascii=False) + "\n"
            tmp = self.path.with_suffix(".pending")
            tmp.write_text(encoded, encoding="utf-8")
            os.replace(tmp, self.path)

    def event(self, kind, **fields):
        with self.lock:
            self.value["events"].append(self.clean({"at": time.time(), "kind": kind, **fields}))
            self.save()
            if kind in ("external_control_restart_requested", "external_db_readback_requested"):
                print(json.dumps(self.clean({"event": kind, **fields})), flush=True)

    def snapshot(self, name, value):
        with self.lock:
            path = self.path.parent / (name + ".json")
            path.write_text(json.dumps(self.clean(value), indent=2) + "\n", encoding="utf-8")
            self.value.setdefault("artifacts", {})[name] = file_identity(path)
            self.save()


def local_url(value):
    parsed = urllib.parse.urlsplit(value)
    require(
        parsed.scheme == "http"
        and parsed.hostname
        and parsed.port
        and not parsed.username
        and not parsed.password
        and not parsed.query
        and not parsed.fragment
        and parsed.path in ("", "/"),
        "Control requires explicit plain local HTTP origin, no credential/path/query",
    )
    # Windows loopback and parent-selected WSL host/gateway are local transport;
    # public addresses and DNS-based proxy redirection are never accepted.
    import ipaddress

    if parsed.hostname != "localhost":
        address = ipaddress.ip_address(parsed.hostname)
        require(address.is_loopback or address.is_private, "Control target is not local/private")
    return parsed


def http_request(origin, method, path, token, body=None):
    connection = http.client.HTTPConnection(origin.hostname, origin.port, timeout=12)
    headers = {"Authorization": "Bearer " + token, "Accept": "application/json"}
    if body is not None:
        headers["Content-Type"] = "application/json"
    try:
        connection.request(method, path, body, headers)
        response = connection.getresponse()
        payload = response.read(MAX_BODY + 1)
        require(len(payload) <= MAX_BODY, "Control response exceeds bound")
        return response.status, payload
    finally:
        connection.close()


class LossProxy:
    """Drop only the first committed attach response; gate one stop ACK.

    Requests/fences are kept in RAM to replay exact bytes. Auth headers are
    never serialized. The stop response gate is not a fabricated failure or
    second dropped-attach injection: it locates a committed stop/unknown ACK
    crash window after the original real paid turn has physically stopped.
    """

    def __init__(self, origin, receipt, boundary):
        self.origin, self.receipt, self.boundary = origin, receipt, boundary
        self.lock = threading.RLock()
        self.attach = None
        self.stop = None
        self.attach_committed = threading.Event()
        self.attach_dropped = threading.Event()
        self.attach_readback = threading.Event()
        self.attach_observed = threading.Event()
        self.stop_committed = threading.Event()
        self.stop_replayed = threading.Event()
        self.release = threading.Event()
        self.registrations = []
        self.calls = []
        self.errors = []
        owner = self

        class Handler(http.server.BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *_args):
                pass

            def handle(self):
                try:
                    super().handle()
                except (ConnectionResetError, BrokenPipeError):
                    self.close_connection = True

            def do_GET(self):
                owner.handle(self)

            do_POST = do_PUT = do_PATCH = do_GET

        class Server(http.server.ThreadingHTTPServer):
            def handle_error(self, _request, _client_address):
                # socketserver's default prints a raw traceback outside
                # owner.handle; record only a bounded sanitized failure.
                exc = sys.exc_info()[1]
                if isinstance(exc, (ConnectionResetError, BrokenPipeError)):
                    return
                with owner.lock:
                    owner.errors.append(
                        {
                            "type": type(exc).__name__,
                            "stage": "proxy_handler",
                            "detail": "unexpected request-handler failure; contents withheld",
                        }
                    )

        self.server = Server(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = "http://127.0.0.1:" + str(self.server.server_port)

    def handle(self, handler):
        try:
            require(not handler.headers.get("Transfer-Encoding"), "unexpected chunked request")
            length = int(handler.headers.get("Content-Length", "0"))
            require(0 <= length <= MAX_BODY, "proxy request size exceeds bound")
            body = handler.rfile.read(length)
            require(len(body) == length, "short proxy request")
            path = handler.path
            require(path.startswith("/api/v1/") or path.startswith("/socket/"), "unexpected proxy endpoint")
            if "Upgrade" in handler.headers:
                # An explicitly unsupported websocket transport is not malformed
                # authoritative evidence; the real daemon owns polling fallback.
                handler.close_connection = True
                return
            if self.attach_dropped.is_set() and re.fullmatch(r"/api/v1/runs/[^/]+/session(?:\?.*)?", path):
                gate = self.release if self.boundary == "attach-commit" else self.attach_observed
                require(gate.wait(120), "attach observation/readback barrier deadline expired")
            token = handler.headers.get("Authorization", "")
            # Headers stay in RAM only. Forward no ambient cookie/proxy headers.
            conn = http.client.HTTPConnection(self.origin.hostname, self.origin.port, timeout=12)
            try:
                headers = {
                    key: value
                    for key, value in handler.headers.items()
                    if key.lower() in {"authorization", "content-type", "accept", "idempotency-key"}
                }
                conn.request(handler.command, path, body if length else None, headers)
                response = conn.getresponse()
                payload = response.read(MAX_BODY + 1)
                status = response.status
                require(len(payload) <= MAX_BODY, "proxy response size exceeds bound")
            finally:
                conn.close()
            attach_path = bool(re.fullmatch(r"/api/v1/runs/[^/]+/session", path.split("?")[0]))
            stop_path = bool(re.fullmatch(r"/api/v1/runs/[^/]+/session/stopped", path))
            call = {
                "method": handler.command,
                "path": path.split("?")[0],
                "status": status,
                "request_digest": sha(body),
                "response_digest": sha(payload),
            }
            drop = hold = False
            with self.lock:
                self.calls.append(call)
                if handler.command == "PUT" and "/machines/" in path and "/sessions/" in path and 200 <= status < 300:
                    machine_id, session_id = machine_session_ids(path)
                    registration = {
                        "path": path,
                        "machine_id": machine_id,
                        "session_id": session_id,
                        "request": parse_json(body, "registration request"),
                        "response": parse_json(payload, "registration response"),
                    }
                    require(
                        isinstance(registration["request"], dict) and isinstance(registration["response"], dict),
                        "successful registration lacks authoritative JSON objects",
                    )
                    self.registrations.append(registration)
                    self.receipt.event("production_registration_committed", **call, registration=registration)
                if attach_path and handler.command == "PUT" and status in (200, 201) and self.attach is None:
                    self.attach = {"path": path, "body": body, "authorization": token, "response": payload}
                    drop = True
                    self.receipt.event(
                        "attach_committed_response_held_for_marker",
                        **call,
                        request=parse_json(body, "attach request"),
                        response=parse_json(payload, "attach response"),
                    )
                    self.attach_committed.set()
                if attach_path and handler.command == "GET" and status == 200:
                    self.receipt.event(
                        "daemon_attachment_readback", **call, response=parse_json(payload, "attach readback")
                    )
                    self.attach_readback.set()
                if stop_path and handler.command == "PUT" and status in (200, 201):
                    if self.stop is None:
                        self.stop = {"path": path, "body": body, "authorization": token, "response": payload}
                        self.receipt.event(
                            "stop_certificate_committed_before_ack",
                            **call,
                            request=parse_json(body, "stop request"),
                            response=parse_json(payload, "stop response"),
                        )
                        self.stop_committed.set()
                        hold = self.boundary == "stop-ack"
                    else:
                        require(body == self.stop["body"], "daemon stop replay changed original exact fence/body")
                        require(
                            parse_json(payload, "stop replay")
                            == parse_json(self.stop["response"], "initial stop response"),
                            "daemon stop replay changed immutable receipt",
                        )
                        self.receipt.event(
                            "daemon_stop_replay",
                            **call,
                            same_request=body == self.stop["body"],
                            response=parse_json(payload, "stop replay response"),
                        )
                        self.stop_replayed.set()
                        hold = not self.release.is_set() and self.boundary == "stop-ack"
                if (
                    "/transitions/" in path
                    or path.endswith("/context")
                    or path.endswith("/evidence")
                    or path.endswith("/usage")
                ):
                    self.receipt.event(
                        "control_execution_http",
                        **call,
                        request=parse_json(body, "execution request") if body else None,
                        response=parse_json(payload, "execution response") if payload else None,
                    )
            if drop:
                require(self.attach_observed.wait(120), "pre-loss native marker observation deadline expired")
                handler.close_connection = True
                handler.connection.shutdown(socket.SHUT_RDWR)
                handler.connection.close()
                self.receipt.event("attach_committed_response_lost", **call)
                self.attach_dropped.set()
                return
            if hold:
                require(self.release.wait(120), "stop ACK gate deadline exceeded")
            handler.send_response(status)
            handler.send_header("Content-Type", "application/json")
            handler.send_header("Content-Length", str(len(payload)))
            handler.end_headers()
            handler.wfile.write(payload)
        except (BrokenPipeError, ConnectionResetError, OSError):
            handler.close_connection = True
        except Exception as exc:
            with self.lock:
                self.errors.append(
                    {"type": type(exc).__name__, "detail": self.receipt.clean(str(exc)), "raw_contents": "withheld"}
                )
            handler.close_connection = True

    def close(self, release_barriers=True):
        if release_barriers:
            self.attach_observed.set()
            self.release.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(5)


def observe(reason, condition, seconds, process=None):
    deadline = time.monotonic() + seconds
    delay = 0.05
    while True:
        value = condition()
        if value:
            return value
        if process is not None:
            require(process.poll() is None, reason + ": production daemon exited")
        remaining = deadline - time.monotonic()
        require(remaining > 0, reason + ": deadline expired")
        threading.Event().wait(min(delay, remaining))
        delay = min(delay * 1.4, 0.75)


def isolated_environment(root, git):
    allowed = {"SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT"} if os.name == "nt" else set()
    env = {key: value for key, value in os.environ.items() if key.upper() in allowed}
    home, temp, agent = root / "home", root / "temp", root / "agent"
    env.update(
        HOME=str(home),
        USERPROFILE=str(home),
        APPDATA=str(home / "AppData/Roaming"),
        LOCALAPPDATA=str(home / "AppData/Local"),
        XDG_CONFIG_HOME=str(home / ".config"),
        XDG_CACHE_HOME=str(home / ".cache"),
        XDG_DATA_HOME=str(home / ".local/share"),
        XDG_STATE_HOME=str(home / ".local/state"),
        TEMP=str(temp),
        TMP=str(temp),
        TMPDIR=str(temp),
        OMO_CODING_AGENT_DIR=str(agent),
        DO_NOT_TRACK="1",
        OMO_DISABLE_POSTHOG="1",
        LANG="C.UTF-8",
        LC_ALL="C.UTF-8",
        GIT_CONFIG_NOSYSTEM="1",
        GIT_CONFIG_GLOBAL=os.devnull,
        GIT_TERMINAL_PROMPT="0",
        GIT_NO_REPLACE_OBJECTS="1",
    )
    system_path = (
        str(pathlib.Path(env.get("SystemRoot", "C:/Windows")) / "System32") if os.name == "nt" else "/usr/bin:/bin"
    )
    env["PATH"] = str(git.parent) + os.pathsep + system_path
    for directory in [
        home,
        temp,
        agent,
        root / "sessions",
        root / "state",
        root / "workspaces",
        pathlib.Path(env["APPDATA"]),
        pathlib.Path(env["LOCALAPPDATA"]),
        pathlib.Path(env["XDG_CONFIG_HOME"]),
        pathlib.Path(env["XDG_CACHE_HOME"]),
        pathlib.Path(env["XDG_DATA_HOME"]),
        pathlib.Path(env["XDG_STATE_HOME"]),
    ]:
        directory.mkdir(parents=True, mode=0o700, exist_ok=True)
    for name in ("SYMMETRY_OMO_PROBE_KEY", "SYMMETRY_OMO_PROBE_URL", "SYMMETRY_ENROLLMENT_TOKEN"):
        env[name] = os.environ[name]
    return env


def command_bytes(argv, env, cwd, receipt):
    # Only local Git/version metadata commands, never a shell, model turn,
    # provider probe, or remote fetch. Capture in RAM before sanitization.
    result = subprocess.run(argv, env=env, cwd=cwd, capture_output=True, timeout=20, check=False)
    receipt.event(
        "metadata_command",
        argv=argv,
        cwd=str(cwd),
        returncode=result.returncode,
        stdout_digest=sha(result.stdout),
        stderr_digest=sha(result.stderr),
    )
    require(result.returncode == 0, "local metadata command failed; output contents withheld")
    require(len(result.stdout) <= MAX_BODY, "local metadata output exceeds bound")
    return result.stdout


ATTESTATION_REFUSALS = {
    "declared_path_set_mismatch": "invalid parent source attestation identity or path set",
    "declared_entry_shape": "parent source attestation differs from actual source blob: {detail}",
    "declared_content_mismatch": "parent source attestation differs from actual source blob: {detail}",
}
TREE_REFUSALS = {
    "tree_entry_not_a_file": "fixture tree contains non-file effects",
    "invalid_tree_record": "fixture tree listing is unreadable",
}


def source_revision(source, git, env, receipt, attestation=None):
    files = source_subject.declared_file_identities(source, SOURCE_PATHS)
    if attestation is None:
        revision = source_subject.head_revision(
            lambda *arguments: command_bytes([str(git), "-C", str(source), *arguments], env, source, receipt)
        )
        provenance = {"kind": "local_git_head", "platform": platform.system()}
    else:
        # A Windows worktree's gitdir spelling is not a Linux Git checkout.
        # The parent supplies its observed HEAD and freezes every relevant
        # source blob; this runner verifies content, never a claimed pass.
        value = read_json(attestation)
        require(
            set(value) == {"source_revision", "source_files", "origin"}
            and value["origin"] == "parent-observed-Windows-Git-HEAD-and-source-blobs"
            and isinstance(value["source_revision"], str)
            and re.fullmatch(r"[0-9a-f]{40}", value["source_revision"]),
            "invalid parent source attestation identity or path set",
        )
        try:
            source_subject.verify_declared_file_set(SOURCE_PATHS, value["source_files"], files)
        except source_subject.Refused as refusal:
            raise Rejected(ATTESTATION_REFUSALS[refusal.code].format(detail=refusal.detail)) from refusal
        revision = value["source_revision"]
        provenance = {
            "kind": value["origin"],
            "attestation": file_identity(attestation),
            "content_verified_on": platform.system(),
            "local_git_head": "not exercised",
        }
    receipt.value["source_revision"] = revision
    receipt.value["source_revision_provenance"] = provenance
    receipt.value["source_files"] = files


def tree_digest(repository, commit, git, env, receipt):
    listing = command_bytes(
        [str(git), "-C", str(repository), "ls-tree", "-r", "-z", "--full-tree", commit], env, repository, receipt
    )

    def read_object(object_id):
        return command_bytes(
            [str(git), "-C", str(repository), "cat-file", "blob", object_id.decode()], env, repository, receipt
        )

    try:
        return source_subject.committed_tree_digest(
            source_subject.tree_records(listing), read_object, modes=source_subject.FILE_MODES
        )
    except source_subject.Refused as refusal:
        raise Rejected(TREE_REFUSALS[refusal.code]) from refusal


def process_marker(pid):
    if os.name == "nt":
        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel.OpenProcess.restype = ctypes.c_void_p
        handle = kernel.OpenProcess(0x1000, False, pid)
        require(handle, "cannot read Windows process creation identity")
        try:
            values = [ctypes.c_uint64() for _ in range(4)]
            require(
                kernel.GetProcessTimes(ctypes.c_void_p(handle), *(ctypes.byref(value) for value in values)),
                "cannot read Windows process times",
            )

            class UnicodeString(ctypes.Structure):
                _fields_ = [("length", ctypes.c_ushort), ("maximum", ctypes.c_ushort), ("buffer", ctypes.c_void_p)]

            storage = ctypes.create_string_buffer(128 << 10)
            returned = ctypes.c_ulong()
            status = ctypes.WinDLL("ntdll").NtQueryInformationProcess(
                ctypes.c_void_p(handle), 60, ctypes.byref(storage), ctypes.sizeof(storage), ctypes.byref(returned)
            )
            require(status == 0, "cannot query actual Windows process command line")
            command = UnicodeString.from_buffer(storage)
            address = ctypes.addressof(storage)
            require(
                command.buffer
                and address <= command.buffer
                and command.length % 2 == 0
                and command.buffer + command.length <= address + ctypes.sizeof(storage),
                "Windows process command line response is invalid",
            )
            try:
                command_line = ctypes.string_at(command.buffer, command.length).decode("utf-16-le", "strict")
            except UnicodeDecodeError:
                raise Rejected(
                    "Windows process command line identity is invalid UTF-16; raw contents withheld"
                ) from None
            return {"pid": pid, "creation_filetime": values[0].value, "actual_command_line": command_line}
        finally:
            kernel.CloseHandle(ctypes.c_void_p(handle))
    stat = pathlib.Path("/proc") / str(pid) / "stat"
    raw = stat.read_text(encoding="utf-8", errors="strict")
    start_ticks = raw[raw.rfind(")") + 2 :].split()[19]
    executable = pathlib.Path("/proc") / str(pid) / "exe"
    image = file_identity(executable)
    target = os.readlink(executable)
    argv = [
        arg.decode("utf-8", "strict")
        for arg in (pathlib.Path("/proc") / str(pid) / "cmdline").read_bytes().split(b"\0")
        if arg
    ]
    after = stat.read_text(encoding="utf-8", errors="strict")
    require(
        after[after.rfind(")") + 2 :].split()[19] == start_ticks,
        "process identity changed while capturing executed image",
    )
    return {
        "pid": pid,
        "start_ticks": start_ticks,
        "actual_executable": image,
        "actual_executable_target": target,
        "actual_argv": argv,
        "kernel_boot_id": pathlib.Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
    }


class DaemonProcess:
    def __init__(self, binary, config, root, env, receipt, label):
        self.receipt, self.label = receipt, label
        self.threads = []
        self.capture_errors = []
        self.capture_lock = threading.Lock()
        self.process = subprocess.Popen(
            [str(binary), "-config", str(config)],
            cwd=root,
            env=env,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        for name, pipe in [("stdout", self.process.stdout), ("stderr", self.process.stderr)]:
            thread = threading.Thread(
                target=self.capture, args=(pipe, root / (label + "." + name + ".log"), name), daemon=True
            )
            thread.start()
            self.threads.append(thread)
        try:
            receipt.event(
                "production_daemon_started",
                label=label,
                argv=[str(binary), "-config", str(config)],
                process=process_marker(self.process.pid),
                cwd=str(root),
                environment_names=sorted(env),
            )
        except Exception:
            self.stop(hard=True)
            raise

    def capture_failure(self, stream, reason):
        error = {"label": self.label, "stream": stream, "reason": reason, "raw_contents": "withheld"}
        with self.capture_lock:
            if error in self.capture_errors:
                return
            self.capture_errors.append(error)
        with self.receipt.lock:
            self.receipt.value["passed"] = False
            self.receipt.value["failures"].append({"stage": "daemon_capture", **error})
        # The in-memory failure still survives finalization; sink failure
        # cannot stop either native-output pipe from being drained.
        with contextlib.suppress(OSError):
            self.receipt.event("daemon_capture_failed", **error)

    def capture(self, pipe, path, stream):
        log = None
        try:
            try:
                log = path.open("w", encoding="utf-8", errors="strict")
            except OSError:
                self.capture_failure(stream, "log sink could not be opened")
            while True:
                line = pipe.readline((1 << 20) + 1)
                if not line:
                    break
                if len(line) > 1 << 20:
                    self.capture_failure(stream, "oversized log record rejected")
                    while line and not line.endswith(b"\n"):
                        line = pipe.readline((1 << 20) + 1)
                    continue
                try:
                    text = line.decode("utf-8", "strict")
                except UnicodeDecodeError:
                    self.capture_failure(stream, "invalid UTF-8 log record rejected")
                    continue
                if log is not None:
                    try:
                        log.write(self.receipt.clean(text))
                        log.flush()
                    except OSError:
                        self.capture_failure(stream, "log sink write failed")
                        try:
                            log.close()
                        except OSError:
                            self.capture_failure(stream, "log sink close failed")
                        log = None
        except Exception:
            self.capture_failure(stream, "log pipe drain failed")
        finally:
            if log is not None:
                try:
                    log.close()
                except OSError:
                    self.capture_failure(stream, "log sink close failed")
            pipe.close()

    def stop(self, hard=False):
        if self.process.poll() is None:
            if hard:
                self.process.kill()  # Kill the daemon only; its production owner reconciles native containment.
            elif os.name != "nt":
                self.process.terminate()
            else:
                self.process.kill()
            self.process.wait(timeout=20)
        for thread in self.threads:
            thread.join(20)
        require(not any(thread.is_alive() for thread in self.threads), "daemon log pipes did not drain")
        self.receipt.event(
            "production_daemon_stopped",
            label=self.label,
            hard=hard,
            returncode=self.process.returncode,
            pid=self.process.pid,
            capture_failed=bool(self.capture_errors),
        )
        require(not self.capture_errors, self.label + ": strict stream capture failed; raw contents withheld")


def snapshots(root, task_id):
    sessions = [read_json(path) for path in (root / "state" / "sessions").rglob("session-*.json")]
    for item in sessions:
        require(
            all(
                isinstance(item.get(key), str) and item[key]
                for key in ("task_id", "goal_id", "run_id", "local_handle_id", "admission_id", "launch_intent_id")
            )
            and type(item.get("generation")) is int
            and item["generation"] > 0
            and isinstance(item.get("session_state"), str)
            and item["session_state"]
            and item["task_id"] == task_id,
            "authoritative session journal lacks required durable identity",
        )
    runs = [read_json(path) for path in (root / "state" / "runs").glob("journal-*.json")]
    for item in runs:
        require(
            isinstance(item.get("run_id"), str)
            and item["run_id"]
            and type(item.get("generation")) is int
            and item["generation"] > 0
            and isinstance(item.get("work"), dict)
            and isinstance(item["work"].get("input"), dict)
            and item["work"]["input"].get("schema_version") == "symmetry.admission.v1"
            and isinstance(item["work"]["input"].get("admission_id"), str)
            and item["work"]["input"]["admission_id"],
            "authoritative run journal lacks required durable admission identity",
        )
    return {"sessions": sessions, "runs": runs}


def native_readback(session, root, receipt):
    path = pathlib.Path(session["native_session_filename"]).resolve()
    require(path.is_relative_to((root / "sessions").resolve()), "native session escaped isolated directory")
    require(path.is_file() and path.stat().st_size <= MAX_BODY, "native session missing or oversized")
    records = [
        parse_json(line, "native session record")
        for line in path.read_text(encoding="utf-8", errors="strict").splitlines()
        if line
    ]
    require(
        records
        and records[0].get("type") == "session"
        and records[0].get("version") == 3
        and records[0].get("id") == session["native_session_id"]
        and pathlib.Path(records[0]["cwd"]).resolve() == pathlib.Path(session["workspace_path"]).resolve(),
        "native header differs from actual durable session/workspace identity",
    )
    require(
        all(isinstance(entry.get("type"), str) and entry["type"] for entry in records),
        "native session record lacks required record identity",
    )
    require(
        all(isinstance(entry.get("message"), dict) for entry in records if entry["type"] == "message"),
        "native session contains a malformed authoritative message record",
    )
    messages = [entry["message"] for entry in records if entry["type"] == "message"]
    require(
        all(isinstance(message.get("role"), str) and message["role"] for message in messages),
        "native session message lacks required role identity",
    )
    users = [message for message in messages if message.get("role") == "user"]
    tools = [message for message in messages if message.get("role") == "toolResult"]
    assistants = [message for message in messages if message.get("role") == "assistant"]
    value = {
        "file": file_identity(path),
        "header": records[0],
        "user_input_count": len(users),
        "tool_results": tools,
        "assistant_messages": assistants,
        "client_message_id": "symmetry-input-" + hashlib.sha256(session["admission_id"].encode()).hexdigest(),
        "client_turn_id": "symmetry-turn-" + hashlib.sha256(session["admission_id"].encode()).hexdigest(),
        "identity_derivation": (
            "actual Control admission_id persisted in GoalSessionJournal; not runner-generated authority"
        ),
        "custom_records": [entry for entry in records if entry.get("type") == "custom"],
    }
    receipt.snapshot("native-session-readback", value)
    return value


def api_json(origin, method, path, token, receipt, body=None):
    status, data = http_request(origin, method, path, token, body)
    value = parse_json(data, "Control HTTP response") if data else None
    receipt.event(
        "operator_or_exact_machine_readback",
        method=method,
        path=path.split("?")[0],
        status=status,
        request_digest=sha(body or b""),
        response=value,
    )
    require(status in (200, 201), "real Control readback rejected HTTP " + str(status))
    return value


def history(origin, task_id, surface, token, receipt):
    values, cursor = [], None
    for _ in range(100):
        query = {"limit": "100"}
        if cursor:
            query["after"] = cursor
        page = api_json(
            origin,
            "GET",
            "/api/v1/tasks/" + task_id + "/" + surface + "?" + urllib.parse.urlencode(query),
            token,
            receipt,
        )
        require(isinstance(page.get(surface), list), "invalid real Control history page")
        values.extend(page[surface])
        next_cursor = page.get("next_after")
        if not next_cursor:
            return values
        require(next_cursor != cursor, "history cursor did not advance")
        cursor = next_cursor
    raise Rejected("history page bound exceeded")


def replay_attachment(proxy, origin, receipt, label):
    attach = proxy.attach
    require(attach is not None, "no actual committed attach request to replay")
    body = parse_json(attach["body"], "historical attach request")
    token = attach["authorization"].removeprefix("Bearer ")
    fence = {key: body[key] for key in ("runtime_id", "runtime_epoch", "generation", "claim_id", "lease_token")}
    query = urllib.parse.urlencode(fence)
    readback = api_json(origin, "GET", attach["path"] + "?" + query, token, receipt)
    replay = api_json(origin, "PUT", attach["path"], token, receipt, attach["body"])
    initial = parse_json(attach["response"], "historical attach response")
    require(readback == initial and replay == initial, "immutable historical attachment readback/replay differs")
    receipt.snapshot(
        label,
        {
            "initial": initial,
            "readback": readback,
            "replay": replay,
            "exact_request_digest": sha(attach["body"]),
            "same_response": True,
        },
    )


def witness(args, receipt):
    root, binary, daemon = args.out.resolve(), args.native_binary.resolve(), args.daemon.resolve()
    repository, source = args.repository.resolve(), pathlib.Path(__file__).resolve().parents[2]
    require(root.is_dir() and not root.is_symlink(), "invalid private output root")
    origin = local_url(args.control_url)
    require(platform.system() in PINS, "only independently qualified Windows and named WSL Linux hosts are supported")
    require(
        binary.is_file() and daemon.is_file() and repository.is_dir(), "required real executable/repository missing"
    )
    native_identity = file_identity(binary)
    expected_size, expected_hash = PINS[platform.system()]
    receipt.value["executables"] = {
        "native": native_identity,
        "daemon": file_identity(daemon),
        "python": file_identity(pathlib.Path(sys.executable).resolve()),
    }
    require(
        (native_identity["bytes"], native_identity["sha256"]) == (expected_size, "sha256:" + expected_hash),
        "native executable does not match this OS exact pin",
    )
    receipt.value["host"] = {
        "system": platform.system(),
        "release": platform.release(),
        "version": platform.version(),
        "machine": platform.machine(),
        "host_label": args.host_label,
    }
    if platform.system() == "Linux":
        distro = pathlib.Path("/etc/os-release").read_text()
        receipt.value["host"]["os_release"] = distro
        receipt.value["host"]["wsl_distribution"] = os.environ.get("WSL_DISTRO_NAME")
        require(
            args.host_label == "Ubuntu-24.04"
            and os.environ.get("WSL_DISTRO_NAME") == "Ubuntu-24.04"
            and "ID=ubuntu" in distro
            and 'VERSION_ID="24.04"' in distro
            and "microsoft" in platform.release().lower(),
            "Linux evidence must execute directly in named WSL Ubuntu-24.04; no Windows qualification inheritance",
        )
    else:
        require(args.host_label == "Windows", "Windows host label required")
    for name in (
        "SYMMETRY_OMO_PROBE_KEY",
        "SYMMETRY_OMO_PROBE_URL",
        "SYMMETRY_ENROLLMENT_TOKEN",
        "SYMMETRY_OPERATOR_TOKEN",
    ):
        require(bool(os.environ.get(name)), "missing named environment " + name)
    relay = urllib.parse.urlsplit(os.environ["SYMMETRY_OMO_PROBE_URL"])
    require(
        relay.scheme == "https"
        and relay.hostname
        and not relay.username
        and not relay.password
        and not relay.query
        and not relay.fragment,
        "relay requires HTTPS without credential/query/fragment",
    )
    git = pathlib.Path(args.git or shutil.which("git") or "").resolve()
    require(git.is_file(), "explicit local Git executable unavailable")
    receipt.value["executables"]["git"] = file_identity(git)
    env = isolated_environment(root, git)
    source_revision(source, git, env, receipt, args.source_attestation)
    receipt.value["daemon_build_source"] = go_build_source(daemon)
    receipt.value["acceptance_authority"] = (
        "parent; this receipt qualifies only its observed platform and selected recovery boundary"
    )
    fixture = read_json(args.fixture_json)
    subject = fixture.get("subject", {})
    require(
        set(subject) == {"resource_id", "commit", "tree_digest"}
        and {key: subject[key] for key in BASELINE} == BASELINE
        and str(uuid.UUID(subject["resource_id"])) == subject["resource_id"],
        "actual domain Subject identity/baseline mismatch",
    )
    require(
        fixture.get("classification") == "operator_fixture_admitted"
        and fixture.get("repository_resource_id") == subject["resource_id"]
        and fixture.get("user_goal_acceptance") is False
        and fixture.get("authorized_origin") == "wxxb789/symmetry-0006h-provider-fixture",
        "fixture authority/Subject mismatch",
    )
    admission = fixture["admission"]
    require(
        admission["subject"] == subject
        and admission["admission_id"] == fixture["admission_id"]
        and admission["session_mode"] == "fresh"
        and admission["limits"]["max_turns"] == 1
        and admission["provider_scope"] is None
        and admission["limits"]["max_cost_microusd"] is None,
        "fixture lacks exact bounded native Goal admission",
    )
    receipt.snapshot("operator-fixture", fixture)
    require(
        tree_digest(repository, subject["commit"], git, env, receipt) == subject["tree_digest"],
        "actual committed fixture tree differs from approved Subject",
    )
    origin_url = (
        command_bytes([str(git), "-C", str(repository), "remote", "get-url", "origin"], env, repository, receipt)
        .decode()
        .strip()
    )
    require(
        origin_url
        in (
            "https://github.com/wxxb789/symmetry-0006h-provider-fixture.git",
            "https://github.com/wxxb789/symmetry-0006h-provider-fixture",
        ),
        "repository origin is not the approved credential-free clone",
    )
    version = command_bytes([str(binary), "--version"], env, root, receipt).decode().strip()
    receipt.value["native_version_line"] = version
    require(
        version == "omo 5.1.69 (engine: senpi 2026.11.10-2; scheme nodef)", "complete native version identity mismatch"
    )
    # Endpoint is public owner-approved dynamic configuration, not guessed.
    # Only apiKey uses native-supported named environment-reference semantics.
    models = {
        "providers": {
            "tencent-litellm": {
                "api": "openai-completions",
                "baseUrl": os.environ["SYMMETRY_OMO_PROBE_URL"],
                "apiKey": "$SYMMETRY_OMO_PROBE_KEY",
                "models": [
                    {"id": "deepseek-v4.1-flash-fast", "reasoning": False, "contextWindow": 1000000, "maxTokens": 4096}
                ],
            }
        }
    }
    (root / "agent" / "models.json").write_text(json.dumps(models, indent=2) + "\n", encoding="utf-8")
    receipt.value["model_configuration"] = models
    profile_args = [
        "--session-dir",
        str(root / "sessions"),
        "--provider",
        "tencent-litellm",
        "--model",
        "deepseek-v4.1-flash-fast",
        "--thinking",
        "off",
    ]
    receipt.value["native_argv_contract"] = [str(binary), *FIXED_ARGS, *profile_args]
    receipt.value["native_argv_provenance"] = (
        "production omo/profile.go fixed RPC arguments plus exact daemon profile; "
        "process marker/header readback corroborates launch"
    )
    proxy = LossProxy(origin, receipt, args.restart_boundary)
    processes = []
    try:
        config = {
            "control_plane_url": proxy.url,
            "allow_insecure_http": True,
            "state_dir": str(root / "state"),
            "machine_name": "omo-control-" + fixture["fixture_label"],
            "cleanup_timeout_ms": 15000,
            "agent_profiles": {
                fixture["model_profile"]: {
                    "command": str(binary),
                    "args": profile_args,
                    "native_model": "deepseek-v4.1-flash-fast",
                    "native_model_provider": "tencent-litellm",
                    "input_mode": "json",
                    "provider_access": False,
                    "interactive": False,
                    "supervisory_control": False,
                    "event_format": "jsonl",
                    "env_allowlist": [name for name in sorted(env) if name != "SYMMETRY_ENROLLMENT_TOKEN"],
                }
            },
            "workspaces": {
                fixture["workspace"]: {
                    "policy": "git_worktree",
                    "repository": str(repository),
                    "root": str(root / "workspaces"),
                    "ref": subject["commit"],
                    "cleanup": "never",
                }
            },
            "runtime": {
                "runtime_key": "omo-control-" + fixture["fixture_label"],
                "name": "OmO Control evidence",
                "capacity": 1,
                "agent_profile": fixture["model_profile"],
                "workspace": fixture["workspace"],
                "repository_resource_id": subject["resource_id"],
                "harness_kind": "omo",
                "harness_version": "5.1.69",
                "adapter_version": "omo-rpc-v1",
                "adapter_protocol_version": 1,
            },
        }
        config_path = root / "daemon.json"
        config_path.write_text(json.dumps(config, indent=2) + "\n", encoding="utf-8")
        receipt.value["daemon_configuration"] = config
        receipt.value["paths"] = {
            "private_root": str(root),
            "repository": str(repository),
            "fixture": str(args.fixture_json.resolve()),
            "config": str(config_path),
        }
        first = DaemonProcess(daemon, config_path, root, env, receipt, "first-daemon")
        processes.append(first)
        observe("first registration", lambda: proxy.registrations, 45, first.process)
        require((root / "state" / "identity.json").is_file(), "production daemon machine identity not persisted")
        identity = read_json(root / "state" / "identity.json")
        receipt.snapshot("machine-identity", identity)
        observe("committed attach before response loss", proxy.attach_committed.is_set, 90, first.process)
        initial = snapshots(root, fixture["task_id"])
        receipt.snapshot("pre-recovery-journals", initial)
        native_markers = []
        for journal in initial["runs"]:
            if journal.get("pid", 0) > 0:
                marker = process_marker(journal["pid"])
                marker["journal_process_identity"] = journal["process_identity"]
                marker["run_id"] = journal["run_id"]
                marker["generation"] = journal["generation"]
                native_markers.append(marker)
        receipt.snapshot("native-process-markers", native_markers)
        require(len(native_markers) == 1, "one exact native process marker was not observable at committed attach")
        receipt.event(
            "native_process_marker_observed",
            boundary="attach committed; response still held before intentional loss",
            run_id=native_markers[0]["run_id"],
            generation=native_markers[0]["generation"],
        )
        if platform.system() == "Linux":
            # The pinned self-contained client may re-exec its own cached image.
            # Bind the running executable's bytes, not argv[0]'s path spelling;
            # every actual argument still must equal the admitted fixed profile.
            actual = native_markers[0]
            require(
                actual["actual_executable"]["bytes"] == native_identity["bytes"]
                and actual["actual_executable"]["sha256"] == native_identity["sha256"]
                and actual["actual_argv"][1:] == receipt.value["native_argv_contract"][1:],
                "actual Linux executed native image or arguments differ from the pinned admitted profile",
            )
        receipt.value["native_effect_identity"] = {
            "admission_id": admission["admission_id"],
            "client_message_id": "symmetry-input-" + hashlib.sha256(admission["admission_id"].encode()).hexdigest(),
            "client_turn_id": "symmetry-turn-" + hashlib.sha256(admission["admission_id"].encode()).hexdigest(),
            "source": "actual Control admission and daemon Goal session journal",
        }
        require(len(initial["sessions"]) == 1, "preinput fixture must have one actual native session")
        initial_workspace = pathlib.Path(initial["sessions"][0]["workspace_path"]).resolve()
        require(
            initial_workspace.is_relative_to((root / "workspaces").resolve()),
            "preinput workspace escaped isolated daemon root",
        )
        before_files = workspace_files(initial_workspace, git, env, receipt)
        require(
            fixture["expected_artifact"]["path"] not in before_files,
            "authorized output already exists before native input",
        )
        receipt.snapshot(
            "workspace-before-native-input",
            {"files": before_files, "boundary": "attach committed; response held before native input"},
        )
        proxy.attach_observed.set()
        observe("committed attach response loss", proxy.attach_dropped.is_set, 15, first.process)
        if args.restart_boundary == "attach-commit":
            first.stop(hard=True)
            receipt.value["recovery_classes"]["active_turn_resume"] = (
                "not requested; production restart is conservative stop/reconcile, no new paid admission"
            )
        else:

            def attachment_continued():
                if proxy.attach_readback.is_set():
                    return True
                data = snapshots(root, fixture["task_id"])
                if any(item.get("terminal_state") in ("failed", "cancelled") for item in data["runs"]):
                    receipt.snapshot("attach-loss-terminal-journals", data)
                    receipt.value["result_class"] = "original_run_settled_before_attach_readback"
                    raise Rejected(
                        "production daemon settled the original Run before same-daemon attach readback; "
                        "preserved native state requires parent inspection, never a new paid admission"
                    )
                return False

            observe("daemon attachment readback before actual turn", attachment_continued, 45, first.process)
            observe("committed physical-stop certificate", proxy.stop_committed.is_set, args.deadline, first.process)
            before = snapshots(root, fixture["task_id"])
            receipt.snapshot("pre-crash-journals", before)
            require(len(before["sessions"]) == 1, "fixture must have one actual native session")
            require(
                before["sessions"][0].get("session_state") == "unavailable",
                "committed stop lacks local known-stopped session state",
            )
            original_run = parse_json(proxy.attach["response"], "committed attach identity")["session"]["run_id"]
            before_runs = [item for item in before["runs"] if item["run_id"] == original_run]
            require(
                len(before_runs) == 1
                and before_runs[0].get("pid", 0) == 0
                and not before_runs[0].get("process_identity")
                and not before_runs[0].get("containment_unproven"),
                "native physical-stop marker was not durably cleared before committed stop ACK crash",
            )
            attach_body = parse_json(proxy.attach["body"], "committed attach request")
            stop_body = parse_json(proxy.stop["body"], "committed stop request")
            require(
                all(
                    attach_body[key] == stop_body[key]
                    for key in ("runtime_id", "runtime_epoch", "generation", "claim_id", "lease_token")
                ),
                "stop certificate changed the original exact attachment fence",
            )
            receipt.value["native_before_restart"] = native_readback(before["sessions"][0], root, receipt)
            first.stop(hard=True)
        receipt.value["recovery_classes"]["restart_boundary"] = args.restart_boundary
        replay_attachment(proxy, origin, receipt, "attachment-after-crash")
        # No Control command is executed here. Optional parent-trigger receipt
        # must attest a real externally owned restart before we reconnect.
        if args.control_restart_trigger:
            request_id = str(uuid.uuid4())
            request_path = root / "parent-control-restart-request.json"
            request_path.write_text(
                json.dumps(
                    {"request_id": request_id, "operation": "restart-task-owned-Control", "daemon_stopped": True}
                )
                + "\n",
                encoding="utf-8",
            )
            receipt.event("external_control_restart_requested", path=str(request_path), request_id=request_id)
            trigger = pathlib.Path(args.control_restart_trigger)
            attest = observe(
                "parent-owned Control restart receipt", lambda: read_json(trigger) if trigger.is_file() else None, 120
            )
            require(
                attest.get("request_id") == request_id
                and attest.get("restarted") is True
                and attest.get("before_identity")
                and attest.get("after_identity")
                and attest["before_identity"] != attest["after_identity"],
                "external Control restart lacks distinct service identity attestation",
            )
            receipt.snapshot("parent-control-restart", attest)
            replay_attachment(proxy, origin, receipt, "attachment-after-control-restart")
            receipt.value["recovery_classes"]["control_restart"] = (
                "external parent identity-attested restart plus real HTTP durable attachment readback"
            )
        else:
            receipt.value["recovery_classes"]["control_restart"] = "not exercised; no external parent trigger supplied"
        env.pop("SYMMETRY_ENROLLMENT_TOKEN", None)
        proxy.release.set()
        second = DaemonProcess(daemon, config_path, root, env, receipt, "second-daemon")
        processes.append(second)
        observe(
            "restart registration", lambda: len({item["path"] for item in proxy.registrations}) >= 2, 45, second.process
        )
        if args.restart_boundary == "stop-ack":
            observe("production daemon exact stopped receipt replay", proxy.stop_replayed.is_set, 75, second.process)
        machine_after = read_json(root / "state" / "identity.json")
        require(machine_after == identity, "hard restart changed durable machine identity")
        task_id = fixture["task_id"]
        operator = os.environ["SYMMETRY_OPERATOR_TOKEN"]

        def settled_task():
            status, raw = http_request(origin, "GET", "/api/v1/tasks/" + task_id, operator)
            require(status == 200, "real task polling failed HTTP " + str(status))
            value = parse_json(raw, "authoritative Task readback")
            require(
                value.get("task_id") == task_id and isinstance(value.get("state"), str) and value["state"],
                "Task readback lacks the original task identity/state",
            )
            return value if value.get("state") in ("completed", "failed", "cancelled") else None

        task = observe("original task terminal recovery", settled_task, 90, second.process)
        receipt.snapshot("task-readback", task)
        final = observe(
            "local exact stopped certificate after restart",
            lambda: (
                (data if len(data["sessions"]) == 1 and data["sessions"][0].get("stop_certificate") else None)
                if (data := snapshots(root, task_id))
                else None
            ),
            75,
            second.process,
        )
        receipt.snapshot("post-restart-journals", final)
        session = final["sessions"][0]
        stop = proxy.stop
        require(stop is not None, "Control never accepted exact stop certificate")
        stop_response = parse_json(stop["response"], "committed stop receipt")["session_stopped"]
        require(
            all(
                session["stop_certificate"][key] == stop_response[key]
                for key in ("receipt_id", "run_id", "session_id", "local_handle_id", "binding_id")
            )
            and stop_response["state"] == "available"
            and stop_response["active_run_id"] is None,
            "local exact stop certificate differs from committed released Control identity",
        )
        require(
            session["run_id"] == task["run_id"] and session["generation"] == task["generation"],
            "recovery produced another run/generation",
        )
        replay_attachment(proxy, origin, receipt, "attachment-after-reconnect")
        transitions = history(origin, task_id, "transitions", operator, receipt)
        events = history(origin, task_id, "events", operator, receipt)
        receipt.snapshot("control-task-history", {"transitions": transitions, "events": events})
        terminal = [item for item in transitions if item.get("state") in ("completed", "failed", "cancelled")]
        require(len(terminal) == 1, "terminal history is not exactly one immutable original-run result")
        receipt.snapshot(
            "goal-readback", api_json(origin, "GET", "/api/v1/goals/" + fixture["goal_id"], operator, receipt)
        )
        context = api_json(
            origin,
            "GET",
            "/api/v1/goals/" + fixture["goal_id"] + "/contexts/" + fixture["context_snapshot_id"],
            operator,
            receipt,
        )
        receipt.snapshot("context-readback", context)
        require(
            context.get("subject") == subject
            and context.get("content_hash") == admission["context_hash"]
            and context.get("snapshot_id") == admission["context_snapshot_id"],
            "real authorized context readback differs from admitted immutable identity",
        )
        runtime_page = api_json(origin, "GET", "/api/v1/runtimes", operator, receipt)
        selected = [
            item for item in runtime_page["runtimes"] if item["runtime_key"] == config["runtime"]["runtime_key"]
        ]
        require(
            len(selected) == 1
            and selected[0]["machine_id"] == identity["machine_id"]
            and selected[0]["repository_resource_id"] == subject["resource_id"],
            "real registered runtime Subject/machine mismatch",
        )
        receipt.snapshot("runtime-readback", selected[0])
        receipt.snapshot("registrations", proxy.registrations)
        require(len(proxy.registrations) >= 2, "daemon restart lacks two actual successful registrations")
        registrations = []
        for item in proxy.registrations:
            machine_id, daemon_instance_id = machine_session_ids(item["path"])
            require(machine_id == identity["machine_id"], "registration changed durable machine identity")
            returned = item["response"].get("runtimes")
            declared = item["request"].get("runtimes")
            require(
                isinstance(returned, list)
                and len(returned) == 1
                and isinstance(returned[0], dict)
                and isinstance(declared, list)
                and len(declared) == 1
                and isinstance(declared[0], dict),
                "registration lacks the exact declared/returned runtime identity",
            )
            runtime = returned[0]
            require(
                runtime.get("runtime_key") == config["runtime"]["runtime_key"] == declared[0].get("runtime_key")
                and runtime.get("runtime_id") == selected[0]["runtime_id"]
                and type(runtime.get("runtime_epoch")) is int
                and runtime["runtime_epoch"] > 0,
                "registration returned a missing/mismatched runtime identity or epoch",
            )
            registrations.append(
                {
                    "machine_id": machine_id,
                    "daemon_instance_id": daemon_instance_id,
                    "path": item["path"],
                    "runtime_id": runtime["runtime_id"],
                    "runtime_epoch": runtime["runtime_epoch"],
                }
            )
        require(
            registrations[0]["path"] != registrations[-1]["path"]
            and registrations[-1]["runtime_epoch"] > registrations[0]["runtime_epoch"],
            "hard restart lacks distinct daemon-session endpoints and advancing Control runtime epoch",
        )
        receipt.snapshot("registration-recovery-proof", registrations)
        require(
            session["control_attachment_receipt_id"]
            == parse_json(proxy.attach["response"], "committed attachment receipt")["session"]["attachment_receipt_id"],
            "local native session lost the original immutable Control attachment identity",
        )
        native = (
            native_readback(session, root, receipt)
            if session.get("native_session_filename") and pathlib.Path(session["native_session_filename"]).is_file()
            else None
        )
        receipt.value["recovery_classes"].update(
            {
                "attach_committed_response_loss": proxy.attach_dropped.is_set(),
                "daemon_hard_restart_same_state": True,
                "historical_attach_exact_readback_replay": True,
                "restart_boundary": args.restart_boundary,
                "retained_resume_new_admission": "not exercised",
                "fresh_handoff": "not exercised",
                "paid_retry": "never performed",
            }
        )
        receipt.value["authority_boundary"] = (
            "local test-fixture plan approvals only; native progress is not acceptance of the user's requested Goal"
        )
        if args.db_readback_trigger:
            request_path = root / "parent-db-readback-request.json"
            request_path.write_text(
                json.dumps(
                    {
                        "operation": "read-only-paired-fixture-readback",
                        "task_id": task_id,
                        "goal_id": fixture["goal_id"],
                        "run_id": task["run_id"],
                        "fixture_path": str(args.fixture_json.resolve()),
                    }
                )
                + "\n",
                encoding="utf-8",
            )
            receipt.event("external_db_readback_requested", path=str(request_path), task_id=task_id)
            trigger = pathlib.Path(args.db_readback_trigger)
            db = observe(
                "parent-owned authoritative DB readback",
                lambda: read_json(trigger) if trigger.is_file() else None,
                120,
                second.process,
            )
            receipt.snapshot("authoritative-db-readback", db)
            require(
                db.get("classification") == "authoritative_db_readback"
                and db.get("task_id") == task_id
                and db.get("admission") == admission
                and db.get("task_admission_count") == 1
                and len(db.get("runs", [])) == 1,
                "authoritative DB readback lacks one exact task/admission/run",
            )
            db_run = db["runs"][0]
            require(
                db_run["run_id"] == task["run_id"]
                and db_run["generation"] == task["generation"]
                and db_run["harness_session_id"] == session["control_session_id"]
                and db_run["harness_binding_id"] == session["binding_id"]
                and len(db_run["attach_receipts"]) == 1
                and len(db_run["stop_receipts"]) == 1
                and db_run["stop_receipts"][0]["id"] == session["stop_certificate"]["receipt_id"],
                "DB readback changed durable attachment/stop identity or duplicated effects",
            )
            receipt.value["db_evidence"] = (
                "parent-produced direct read-only PostgreSQL rows cross-checked with actual daemon and HTTP identities"
            )
        else:
            receipt.value["db_evidence"] = (
                "not included; parent must run paired fixture readback and integrate; no direct SQL claim"
            )
        if args.restart_boundary == "attach-commit":
            receipt.value["classification"] = "conservative_attach_crash_recovery"
            receipt.value["result_class"] = task["state"]
            require(
                task["state"] in ("failed", "cancelled"),
                "attach-boundary restart unexpectedly reported semantic success",
            )
            require(
                native is None or native["user_input_count"] == 0,
                "attach-boundary crash admitted paid input unexpectedly",
            )
            require(
                not (pathlib.Path(session["workspace_path"]) / fixture["expected_artifact"]["path"]).exists(),
                "attach-boundary crash performed the paid artifact effect",
            )
            require(
                len(list((root / "sessions").rglob("*.jsonl"))) <= 1,
                "attach-boundary recovery created duplicate native sessions",
            )
            receipt.value["passed"] = True
            receipt.value["limitations"] = [
                "No provider turn can be claimed at a pre-StartTurn attach crash; "
                "active native turn reattachment is not implemented."
            ]
            return
        require(task["state"] == "completed", "original real provider task did not complete: " + task["state"])
        require(
            len(list((root / "state" / "sessions").glob("session-*.json"))) == 1
            and len(list((root / "sessions").rglob("*.jsonl"))) == 1,
            "recovery created a duplicate native session/effect owner",
        )
        require(
            native is not None and native["user_input_count"] == 1,
            "native session lacks exactly one real admitted user input",
        )
        require(
            sum(1 for item in native["tool_results"] if item.get("toolName") == "write" and not item.get("isError"))
            == 1
            and any(item.get("toolName") == "read" and not item.get("isError") for item in native["tool_results"]),
            "provider-mediated read/write effect missing or duplicated",
        )
        require(
            any(
                item.get("provider") == "tencent-litellm" and item.get("model") == "deepseek-v4.1-flash-fast"
                for item in native["assistant_messages"]
            ),
            "actual native messages do not attest approved provider/model",
        )
        result = task.get("result", {}).get("task_result")
        require(
            isinstance(result, dict)
            and result.get("kind") == "progress"
            and result.get("subject") == subject
            and result.get("subject_hash") == sha(canonical(subject)),
            "actual Control TaskResult is missing or Subject differs",
        )
        workspace = pathlib.Path(session["workspace_path"]).resolve()
        require(workspace == initial_workspace, "post-recovery workspace differs from preinput session")
        artifact = workspace / fixture["expected_artifact"]["path"]
        require(artifact.is_file() and not artifact.is_symlink(), "real repository artifact missing")
        actual = artifact.read_bytes()
        require(
            actual == fixture["expected_artifact"]["utf8"].encode()
            and sha(actual) == fixture["expected_artifact"]["content_digest"],
            "actual artifact bytes differ from approved bounded work",
        )
        receipt.snapshot(
            "artifact-readback",
            {
                "file": file_identity(artifact),
                "utf8": actual.decode(),
                "admitted_committed_subject": subject,
                "uncommitted_local_effect": True,
                "accepted_outcome": False,
            },
        )
        require(
            tree_digest(workspace, subject["commit"], git, env, receipt) == subject["tree_digest"],
            "baseline committed Subject changed",
        )
        dirty = command_bytes(
            [str(git), "-C", str(workspace), "status", "--porcelain=v1", "--untracked-files=all"],
            env,
            workspace,
            receipt,
        )
        receipt.snapshot("workspace-status", {"porcelain": dirty.decode()})
        after_files = workspace_files(workspace, git, env, receipt)
        expected_files = dict(before_files)
        expected_files[fixture["expected_artifact"]["path"]] = {"bytes": len(actual), "sha256": sha(actual)}
        receipt.snapshot("workspace-after-native-input", {"files": after_files})
        require(after_files == expected_files, "native turn changed files outside one authorized artifact")
        require(not proxy.errors, "proxy encountered malformed or invalid authoritative evidence; contents withheld")
        receipt.value["classification"] = "real_native_goal_daemon_control_recovery"
        receipt.value["result_class"] = "completed_progress_not_goal_acceptance"
        receipt.value["passed"] = True
        receipt.value["recovery_classes"]["stop_committed_ack_loss_exact_daemon_replay"] = (
            proxy.stop_replayed.is_set() if args.restart_boundary == "stop-ack" else "not injected"
        )
        receipt.value["limitations"] = [
            "Active native turn reattachment is not exercised or claimed.",
            "Artifact is a preserved uncommitted local file; "
            "progress references the exact committed baseline, not a fabricated candidate commit.",
            "Authoritative DB readback uses a separately invoked parent-owned fixture script, "
            "never a runner-owned service command.",
            "Usage/billing and hard-cost enforcement remain unknown/unverified.",
        ]
    finally:
        for process in reversed(processes):
            try:
                process.stop()
            except Exception as exc:
                receipt.value["failures"].append(
                    {"stage": "daemon_drain", "type": type(exc).__name__, "detail": receipt.clean(str(exc))}
                )
                receipt.value["passed"] = False
                receipt.value["classification"] = "witness_failed"
                if process.process.poll() is None:
                    try:
                        process.stop(hard=True)
                    except Exception as stop_exc:
                        receipt.value["failures"].append(
                            {
                                "stage": "daemon_hard_drain",
                                "type": type(stop_exc).__name__,
                                "detail": receipt.clean(str(stop_exc)),
                            }
                        )
        all_drained = all(
            process.process.poll() is not None and not any(thread.is_alive() for thread in process.threads)
            for process in processes
        )
        if not all_drained:
            receipt.value["passed"] = False
            receipt.value["classification"] = "witness_failed"
            receipt.value["failures"].append(
                {"stage": "cleanup_barrier", "detail": "daemon stop/drain unproven; proxy barriers remain closed"}
            )
        proxy.close(release_barriers=all_drained)
        receipt.snapshot(
            "proxy-http-calls",
            {"calls": proxy.calls, "errors": proxy.errors, "barriers_released_after_daemon_drain": all_drained},
        )
        if proxy.errors:
            receipt.value["passed"] = False
            receipt.value["classification"] = "witness_failed"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for option in ("daemon", "native-binary", "repository", "fixture-json", "out"):
        parser.add_argument("--" + option, required=True, type=pathlib.Path)
    parser.add_argument("--control-url", required=True)
    parser.add_argument("--host-label", required=True, choices=("Windows", "Ubuntu-24.04"))
    parser.add_argument("--git", help="explicit local Git executable; never an agent shim")
    parser.add_argument(
        "--source-attestation",
        type=pathlib.Path,
        help="parent-observed HEAD and exact source blobs for cross-platform worktree metadata",
    )
    parser.add_argument("--deadline", type=int, default=240, choices=range(30, 601), metavar="30..600")
    parser.add_argument("--restart-boundary", choices=("stop-ack", "attach-commit"), default="stop-ack")
    parser.add_argument(
        "--control-restart-trigger", help="parent-written JSON receipt path; runner never executes a service command"
    )
    parser.add_argument(
        "--db-readback-trigger",
        help=(
            "parent-written paired fixture readback JSON, requested after reconnect; "
            "no DB connection string or runner-owned SQL process"
        ),
    )
    args = parser.parse_args()
    # New output roots prevent a previous partial paid effect being overwritten
    # or replayed as a new admission. State remains private and never exported.
    args.out.mkdir(parents=True, mode=0o700, exist_ok=False)
    receipt = Receipt(
        args.out.resolve() / "receipt.json",
        [
            os.environ.get(name, "")
            for name in ("SYMMETRY_OMO_PROBE_KEY", "SYMMETRY_ENROLLMENT_TOKEN", "SYMMETRY_OPERATOR_TOKEN")
        ],
    )
    receipt.value["invocation"] = [sys.executable, str(pathlib.Path(__file__).resolve()), *sys.argv[1:]]
    try:
        witness(args, receipt)
    except Exception as exc:
        receipt.value["classification"] = "witness_failed"
        receipt.value["passed"] = False
        receipt.value["failures"].append(
            {"type": type(exc).__name__, "detail": receipt.clean(str(exc)), "credential_bearing_traceback": "withheld"}
        )
    finally:
        try:
            scan = credential_scan(args.out.resolve(), os.environ.get("SYMMETRY_OMO_PROBE_KEY", ""))
            receipt.value["credential_scan"] = scan
            if scan["classification"] == "failed":
                receipt.value["classification"] = "credential_exposure_detected"
                receipt.value["passed"] = False
        except Exception as exc:
            receipt.value["credential_scan"] = {"classification": "failed", "error_type": type(exc).__name__}
            receipt.value["classification"] = "witness_failed"
            receipt.value["passed"] = False
        receipt.value["retained_private_root"] = str(args.out.resolve())
        receipt.save()
    print(
        json.dumps(
            {
                "classification": receipt.value["classification"],
                "passed": receipt.value["passed"],
                "receipt": str(receipt.path),
                "usage": "unknown",
            }
        )
    )
    return 0 if receipt.value["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
