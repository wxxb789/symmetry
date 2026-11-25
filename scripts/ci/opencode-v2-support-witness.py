#!/usr/bin/env python3
"""Observe the owner-pinned OpenCode blob on its native platform; never promote capabilities.

Two release blobs are pinned, one per platform: the Windows PE (`MZ`) and the
Linux ELF (`7f 45 4c 46`). Each run refuses a blob whose size, sha256 or magic
differs from the running platform's pin. The operator credential is inherited
only through TENCENT_GLM_API_KEY; `--mode provider-failure` requires it to be
absent so the relay answers with its own rejection. No proxy, synthetic
provider, credential endpoint, fault injection or remote mutation is used. The
credential, the relay base URL and its host are replaced in memory before any
write; the blob's host path comes from SYMMETRY_OPENCODE_BINARY and only its
size, digest and release asset are recorded.
"""

import argparse
import base64
import hashlib
import http.client
import json
import os
import pathlib
import platform
import secrets
import signal
import socket
import subprocess
import tempfile
import threading
import time
import urllib.parse

import source_subject

BINARY_ENVIRONMENT = "SYMMETRY_OPENCODE_BINARY"
CREDENTIAL_ENVIRONMENT = "TENCENT_GLM_API_KEY"
VERSION = "2.4.1"
MODEL = "deepseek-v4.1-flash-fast"
PROVIDER = "tencent-litellm"
CREDENTIAL_TOKEN = "<operator credential redacted>"
RELAY_BASE_TOKEN = "<RELAY-BASE-URL>"
RELAY_HOST_TOKEN = "<RELAY-HOST>"
PERMISSION_REPLY = {"decision": "once"}
TERMINALS = {"session.execution.succeeded", "session.execution.failed", "session.execution.interrupted"}
FIRST_TOKEN_KINDS = {"session.step.first-token", "session.text.delta", "session.reasoning.delta"}
POSIX_SIGTERM_SECONDS = 15
POSIX_SIGKILL_SECONDS = 5
# Per-platform release pins: the extracted blob's magic, size and sha256.
PINS = {
    "nt": {
        "label": "Windows PE",
        "asset": "opencode-windows-x64.zip",
        "magic": b"MZ",
        "size": 278383496,
        "sha256": "f3da11bf51ace25b86f52e20368d415451d200d6b48101d9f4e5d721b1d2020e",
    },
    "posix": {
        "label": "Linux ELF",
        "asset": "opencode-linux-x64.tar.gz",
        "magic": b"\x7fELF",
        "size": 275163312,
        "sha256": "ec93043f18f8a6b4d8e71ec9260e292ba6f91b66b142782135d40f0b58b0c152",
    },
}
EXECUTION_CLASS = {
    "nt": "native Windows PE process",
    "posix": "native Linux ELF process under the WSL2 kernel; not a bare-metal Linux owner containment receipt",
}
WINDOWS_ENVIRONMENT = frozenset({"PATH", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "SYSTEMDRIVE"})
POSIX_ENVIRONMENT = frozenset({"PATH"})
WINDOWS_ROOTS = {
    "HOME": "home",
    "USERPROFILE": "home",
    "APPDATA": "appdata",
    "LOCALAPPDATA": "localappdata",
    "XDG_CONFIG_HOME": "config",
    "XDG_CACHE_HOME": "cache",
    "XDG_DATA_HOME": "data",
    "XDG_STATE_HOME": "state",
    "TMP": "tmp",
    "TEMP": "tmp",
}
POSIX_ROOTS = {
    "HOME": "home",
    "XDG_CONFIG_HOME": "config",
    "XDG_CACHE_HOME": "cache",
    "XDG_DATA_HOME": "data",
    "XDG_STATE_HOME": "state",
    "TMPDIR": "tmp",
}
DISABLED_FEATURES = (
    "OPENCODE_DISABLE_PROJECT_CONFIG",
    "OPENCODE_DISABLE_MODELS_FETCH",
    "OPENCODE_DISABLE_AUTOUPDATE",
    "OPENCODE_DISABLE_DEFAULT_PLUGINS",
    "OPENCODE_DISABLE_EXTERNAL_SKILLS",
    "OPENCODE_DISABLE_CLAUDE_CODE_SKILLS",
)
# Only a prompt-schema property named among these bounds the model's output length.
BOUNDING_FIELDS = frozenset({"maxoutputtokens", "maxtokens", "max_tokens", "outputtokens", "tokenlimit", "limit"})


def running_platform():
    return "nt" if os.name == "nt" else "posix"


def pinned_binary():
    platform_key = running_platform()
    pin = PINS[platform_key]
    configured = os.environ.get(BINARY_ENVIRONMENT, "")
    if not configured:
        raise RuntimeError(
            f"{BINARY_ENVIRONMENT} must name the owner-pinned {pin['label']} executable for platform {platform_key}"
        )
    binary = pathlib.Path(configured).resolve()
    if not binary.is_file():
        raise RuntimeError(BINARY_ENVIRONMENT + " must name an existing file")
    with binary.open("rb") as stream:
        magic = stream.read(len(pin["magic"]))
        stream.seek(0)
        size = os.fstat(stream.fileno()).st_size
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
    if not magic.startswith(pin["magic"]):
        raise RuntimeError(
            f"owner-pinned executable magic mismatch for platform {platform_key}: "
            f"expected {pin['magic'].hex()} ({pin['label']}), observed {magic.hex()}"
        )
    if size != pin["size"] or digest != pin["sha256"]:
        raise RuntimeError(
            f"owner-pinned executable identity mismatch for platform {platform_key}: "
            f"expected size {pin['size']} sha256 {pin['sha256']}"
        )
    return binary


def readiness_identity(info, pid):
    """Exact identity: the readiness route must report this server's pid and the pinned version."""
    return isinstance(info, dict) and info.get("pid") == pid and info.get("version") == VERSION


def bounding_field(properties):
    """The first prompt-schema property that bounds output length, or None."""
    return next((name for name in properties if name.lower() in BOUNDING_FIELDS), None)


def resolve_prompt_schema(spec):
    """The live prompt request schema: only a bounding property would enable a truncated-response attempt."""
    operation = spec.get("paths", {}).get("/api/session/{sessionID}/prompt", {}).get("post", {})
    body = (
        operation.get("requestBody", {}).get("content", {}).get("application/json", {}).get("schema", {})
        if isinstance(operation, dict)
        else {}
    )
    properties = sorted(body.get("properties", {}))
    return {
        "properties": properties,
        "required": sorted(body.get("required", [])),
        "bounding_field": bounding_field(properties),
    }


def truncation_marker(events):
    """A native output bound in the recorded evidence; HTTP status, idle and EOF never count."""
    for event in events:
        text = json.dumps(event.get("data", {}))
        if '"truncated": true' in text or '"finish_reason": "length"' in text or '"finishReason": "length"' in text:
            return True
    return False


def relay_replacements(base_url):
    host = urllib.parse.urlsplit(base_url).netloc
    replacements = {base_url: RELAY_BASE_TOKEN} if base_url else {}
    if host:
        replacements[host] = RELAY_HOST_TOKEN
    return replacements


def redact(text, replacements):
    """Replace every non-empty secret spelling; return the redacted text and the removal counts."""
    counts = {}
    for value, token in replacements.items():
        if value and value in text:
            counts[token] = text.count(value)
            text = text.replace(value, token)
    return text, counts


def receipt_bytes(receipt, replacements, secrets):
    """The receipt's write bytes: a secret spelling stops the write, and the replacements apply to what is written."""
    text = json.dumps(receipt, ensure_ascii=True, indent=2) + "\n"
    data = text.encode()
    if any(secret.encode() in data for secret in secrets if secret):
        raise RuntimeError("credential locality evidence refused")
    return redact(text, replacements)[0].encode()


def carries_credential(data, key):
    """An unset credential matches nothing; a set one refuses any carrier that holds it."""
    if not key:
        return False
    return key in (data.decode("utf-8", "replace") if isinstance(data, bytes) else data)


def child_environment(root, platform_key, environ):
    """The allowlisted child environment; every private root redirects into the run root."""
    keep = WINDOWS_ENVIRONMENT if platform_key == "nt" else POSIX_ENVIRONMENT
    roots = WINDOWS_ROOTS if platform_key == "nt" else POSIX_ROOTS
    environment = {name: value for name, value in environ.items() if name.upper() in keep}
    for name, directory in roots.items():
        target = root / directory
        target.mkdir(exist_ok=True)
        environment[name] = str(target)
    return environment


def platform_facts(platform_key):
    facts = {
        "system": platform.system(),
        "kernel": platform.version() if platform_key == "nt" else platform.release(),
        "distribution": platform.platform(),
        "python": platform.python_version(),
        "machine": platform.machine(),
        "execution_class": EXECUTION_CLASS[platform_key],
    }
    os_release = pathlib.Path("/etc/os-release")
    if platform_key != "nt" and os_release.is_file():
        for line in os_release.read_text(errors="replace").splitlines():
            if line.startswith("PRETTY_NAME="):
                facts["distribution_pretty_name"] = line.partition("=")[2].strip().strip('"')
            elif line.startswith("VERSION_ID="):
                facts["distribution_version_id"] = line.partition("=")[2].strip().strip('"')
    return facts


def binding_sha256(repository):
    return {
        path.resolve().relative_to(repository).as_posix(): source_subject.file_sha256_hex(path)
        for path in (pathlib.Path(__file__), pathlib.Path(source_subject.__file__))
    }


def classify(receipt):
    """Interpret captured native facts, never HTTP admission or idle as success."""
    classes = receipt["classes"]
    success = classes.get("success", {})
    terminal = success.get("terminal")
    if terminal or any(
        operation["method"] == "POST"
        and operation["path"].startswith("/api/session/")
        and operation["path"].endswith("/prompt")
        for operation in receipt["operations"]
    ):
        success["status"] = (
            "observed" if terminal and terminal.get("type") == "session.execution.succeeded" else "run and failed"
        )
    else:
        success["status"] = "unavailable"
        success.setdefault("blocker", "no native execution request or terminal captured")
    classes["success"] = success
    failures = [
        event
        for event in receipt["events"]
        if event.get("type") == "session.execution.failed"
        and event.get("data", {}).get("error", {}).get("type", "").startswith("provider.")
    ]
    classes["provider_failure"] = (
        {"status": "observed", "terminals": failures}
        if failures
        else {"status": "unavailable", "blocker": "no real provider failure captured; synthetic substitutes excluded"}
    )
    continuation = classes.get("continuation_after_step", {})
    terminal = continuation.get("terminal") or {}
    reply = continuation.get("reply") or {}
    if "event_start" in continuation:
        window = receipt["events"][continuation["event_start"] :]
        terminal_index = next(
            (index for index, event in enumerate(window) if terminal.get("id") and event == terminal), None
        )
        window = window[:terminal_index] if terminal_index is not None else []
        kinds = [
            event.get("type")
            for event in window
            if event.get("data", {}).get("sessionID") == terminal.get("data", {}).get("sessionID")
        ]
        continued = any(
            "session.tool.success" in kinds[:index] and "session.step.started" in kinds[index + 1 :]
            for index, kind in enumerate(kinds)
            if kind == "session.step.ended"
        )
        continuation["status"] = (
            "observed"
            if reply.get("status") == 204
            and (reply.get("body") or {}).get("decision") == "once"
            and continued
            and terminal.get("type") == "session.execution.succeeded"
            else "run and failed"
        )
    else:
        continuation.update(status="unavailable", blocker="no successful tool continuation captured")
    classes["continuation_after_step"] = continuation
    interruption = classes.get("interruption", {})
    if interruption.get("terminal") or any(
        operation["method"] == "POST"
        and operation["path"].startswith("/api/session/")
        and operation["path"].endswith("/interrupt")
        for operation in receipt["operations"]
    ):
        interrupted = interruption.get("response") or {}
        terminal = interruption.get("terminal") or {}
        interruption["status"] = (
            "observed"
            if interruption.get("first_token")
            and interrupted.get("interrupted") is True
            and terminal.get("type") == "session.execution.interrupted"
            else "run and failed"
        )
    else:
        interruption["status"] = "unavailable"
        interruption.setdefault("blocker", "no active native interruption captured")
    classes["interruption"] = interruption
    counters = []
    for operation in receipt["operations"]:
        if (
            operation["method"] == "GET"
            and operation["status"] == 200
            and operation["path"].startswith("/api/session/")
            and operation.get("body")
        ):
            try:
                data = json.loads(operation["body"]).get("data", {})
            except ValueError:
                continue
            if isinstance(data, dict) and "tokens" in data:
                counters.append({name: data.get(name) for name in ("id", "outcome", "tokens", "cost")})
    classes["usage"] = {
        "status": "observed" if counters else "unavailable",
        "native_counters": counters,
        "limitation": "native counters only; successful/billable usage and failed-turn accounting unverified",
    }
    cleanup = classes.get("cleanup", {})
    cleanup["status"] = (
        "observed"
        if cleanup.get("exit_code") == 0
        and cleanup.get("server_exit_code") is not None
        and cleanup.get("stream_reader_stopped") is True
        and cleanup.get("output_readers_stopped") is True
        else "run and failed"
    )
    classes["cleanup"] = cleanup
    classes.setdefault(
        "truncated_response",
        {
            "status": "unavailable",
            "blocker": (
                "no successful real-relay response or approved provider fault injection; synthetic substitutes excluded"
            ),
        },
    )
    return receipt


def native_kwargs(platform_key):
    return {"start_new_session": True} if platform_key == "posix" else {}


def close_native(process, connection, stream_reader, output_readers):
    """Always reach process, socket and reader barriers, including kill failure."""
    if running_platform() == "posix":
        result = {"command": ["SIGTERM", "process-group", str(process.pid)], "exit_code": None}
        try:
            os.killpg(os.getpgid(process.pid), signal.SIGTERM)
            try:
                process.wait(timeout=POSIX_SIGTERM_SECONDS)
            except subprocess.TimeoutExpired:
                # A native server that ignores SIGTERM is escalated once, still bounded.
                result["command"].append("SIGKILL")
                result["escalated"] = True
                os.killpg(os.getpgid(process.pid), signal.SIGKILL)
                process.wait(timeout=POSIX_SIGKILL_SECONDS)
            result["exit_code"] = 0
        except (OSError, subprocess.TimeoutExpired) as error:
            result["signal_error"] = type(error).__name__
            result["exit_code"] = 1
    else:
        result = {"command": ["taskkill", "/PID", str(process.pid), "/T", "/F"], "exit_code": None}
        try:
            result["exit_code"] = subprocess.run(result["command"], capture_output=True, timeout=15).returncode
        except (OSError, subprocess.TimeoutExpired) as error:
            result["tree_kill_error"] = type(error).__name__
        try:
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
        except (OSError, subprocess.TimeoutExpired) as error:
            result["process_stop_error"] = type(error).__name__
    try:
        if connection is not None:
            try:
                if connection.sock is not None:
                    connection.sock.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            try:
                connection.close()
            except OSError as error:
                result["stream_close_error"] = type(error).__name__
        if stream_reader is not None:
            stream_reader.join(timeout=5)
        for reader in output_readers:
            reader.join(timeout=5)
    finally:
        result.update(
            server_exit_code=process.returncode,
            stream_reader_stopped=stream_reader is None or not stream_reader.is_alive(),
            output_readers_stopped=all(not reader.is_alive() for reader in output_readers),
        )
    return result


def capture_events(response, key, receipt, condition):
    count = 0
    try:
        while True:
            line = response.readline(1 << 20)
            if not line:
                break
            count += len(line)
            if count > 8 << 20:
                raise RuntimeError("native SSE capture limit reached")
            if line.startswith(b"data:"):
                text = line[5:].strip().decode()
                if carries_credential(text, key):
                    raise RuntimeError("credential native event refused")
                event = json.loads(text)
                if isinstance(event, str):
                    event = json.loads(event)
                with condition:
                    receipt["events"].append(event)
                    condition.notify_all()
    except (OSError, ValueError, RuntimeError) as error:
        receipt["stream_end"] = type(error).__name__


def command_observation(binary, arguments, environment):
    completed = subprocess.run(
        [str(binary), *arguments],
        capture_output=True,
        timeout=60,
        env=environment,
        cwd=environment.get("HOME"),
    )
    return {
        "command": [str(binary), *arguments],
        "exit_code": completed.returncode,
        "stdout": completed.stdout[:65536].decode(errors="replace"),
        "stderr": completed.stderr[:65536].decode(errors="replace"),
    }


def api_document(port, authorization):
    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=30)
    try:
        connection.request("GET", "/openapi.json", headers={"Authorization": authorization})
        response = connection.getresponse()
        raw = response.read(32 << 20)
        return response.status, raw
    finally:
        connection.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, type=pathlib.Path)
    parser.add_argument("--mode", choices=("probe", "full", "provider-failure", "truncation"), default="full")
    args = parser.parse_args()
    mode = args.mode
    platform_key = running_platform()
    pin = PINS[platform_key]
    binary = pinned_binary()
    key = os.environ.get(CREDENTIAL_ENVIRONMENT, "")
    if mode == "provider-failure" and key:
        raise RuntimeError("the provider-failure pass must run without the credential environment")
    if mode == "full" and not key:
        raise RuntimeError("the collection pass requires the credentialed provider environment")
    root = pathlib.Path(tempfile.mkdtemp(prefix="symmetry-opencode-witness-"))
    repository = pathlib.Path(__file__).resolve().parents[2]
    receipt = {
        "decision": "unsupported",
        "mode": mode,
        "binary": {
            "path": str(binary),
            "size": pin["size"],
            "sha256": pin["sha256"],
            "magic": pin["magic"].hex(),
            "asset": pin["asset"],
        },
        "platform": platform_facts(platform_key),
        "source_head": source_subject.head_revision(
            lambda *arguments: subprocess.check_output(["git", *arguments], cwd=repository)
        ),
        "binding_sha256": binding_sha256(repository),
        "command": ["python3", "scripts/ci/opencode-v2-support-witness.py", "--mode", mode, "--out", str(args.out)],
        "workspace": str(root),
        "operations": [],
        "events": [],
        "classes": {},
    }
    environment = child_environment(root, platform_key, os.environ)
    environment["OPENCODE_TEST_MANAGED_CONFIG_DIR"] = str(root / "managed")
    (root / "managed").mkdir(exist_ok=True)
    for name in DISABLED_FEATURES:
        environment[name] = "1"
    source_config = pathlib.Path.home() / ".config/opencode/opencode.json"
    provider = json.loads(source_config.read_text())["provider"][PROVIDER]
    endpoint = provider["options"]["baseURL"]
    if not endpoint.startswith("https://"):
        raise RuntimeError("configured relay is not HTTPS")
    # Keep only this provider/model, replacing the source key without copying it.
    provider["options"] = {"baseURL": endpoint, "apiKey": "{env:" + CREDENTIAL_ENVIRONMENT + "}"}
    provider["models"] = {MODEL: provider["models"][MODEL]}
    config = {
        "model": PROVIDER + "/" + MODEL,
        "small_model": PROVIDER + "/" + MODEL,
        "enabled_providers": [PROVIDER],
        "provider": {PROVIDER: provider},
    }
    encoded_config = json.dumps(config)
    if key and key in encoded_config:
        raise RuntimeError("credential in configuration refused")
    config_path = root / "config/opencode.json"
    config_path.parent.mkdir(exist_ok=True)
    config_path.write_text(encoded_config)
    environment["OPENCODE_CONFIG"] = str(config_path)
    environment["OPENCODE_CONFIG_DIR"] = str(config_path.parent)
    if key:
        environment[CREDENTIAL_ENVIRONMENT] = key
    replacements = {key: CREDENTIAL_TOKEN} if key else {}
    replacements.update(relay_replacements(endpoint))
    password = secrets.token_urlsafe(32)
    environment.update({"OPENCODE_SERVER_USERNAME": "opencode", "OPENCODE_SERVER_PASSWORD": password})
    authorization = "Basic " + base64.b64encode(("opencode:" + password).encode()).decode()
    receipt["provider"] = {
        "id": PROVIDER,
        "model": MODEL,
        "base_url": RELAY_BASE_TOKEN,
        "base_url_redaction": "the literal relay origin is replaced in memory before any write",
        "discovered_from": str(source_config),
        "credential": ("environment:" + CREDENTIAL_ENVIRONMENT) if key else "unset",
        "credential_present": bool(key),
    }
    (root / "baseline.txt").write_text("native continuation witness\n")
    if mode == "probe":
        receipt["observations"] = [
            command_observation(binary, ["--version"], environment),
            command_observation(binary, ["serve", "--help"], environment),
        ]
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    argv = [str(binary), "serve", "--hostname", "127.0.0.1", "--port", str(port)]
    receipt["server_command"] = argv
    process = subprocess.Popen(
        argv,
        cwd=environment["HOME"],
        env=environment,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        **native_kwargs(platform_key),
    )
    receipt["server_pid"] = process.pid
    logs = {}
    readers = []
    for name in ("stdout", "stderr"):

        def drain(name=name):
            logs[name] = getattr(process, name).read(2 << 20).decode(errors="replace")

        reader = threading.Thread(target=drain, daemon=True)
        reader.start()
        readers.append(reader)
    condition = threading.Condition()
    stream_connection = None
    stream_reader = None

    def request(method, path, payload=None, authenticated=True, record=True):
        body = json.dumps(payload).encode() if payload is not None else None
        if body and carries_credential(body, key):
            raise RuntimeError("credential request payload refused")
        headers = {"Content-Type": "application/json"}
        if authenticated:
            headers["Authorization"] = authorization
        connection = http.client.HTTPConnection("127.0.0.1", port, timeout=30)
        try:
            connection.request(method, path, body=body, headers=headers)
            response = connection.getresponse()
            raw = response.read(8 << 20).decode(errors="replace")
            safe_raw, redacted = redact(raw, replacements)
            if record:
                receipt["operations"].append(
                    {
                        "method": method,
                        "path": path,
                        "authenticated": authenticated,
                        "payload": payload,
                        "status": response.status,
                        "body": safe_raw,
                        "credential_redacted_before_write": bool(redacted),
                    }
                )
            try:
                parsed = json.loads(safe_raw) if safe_raw else None
            except ValueError:
                parsed = None
            return response.status, parsed
        finally:
            connection.close()

    def await_event(session_id, kinds, after, timeout=60):
        deadline = time.monotonic() + timeout
        with condition:
            while True:
                for event in receipt["events"][after:]:
                    if (
                        event.get("type") in kinds
                        and event.get("data", {}).get("sessionID") == session_id
                        and event.get("durable", {}).get("aggregateID", session_id) == session_id
                    ):
                        return event
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return None
                condition.wait(remaining)

    def session(permissions=None):
        payload = {"location": {"directory": str(root)}, "model": {"providerID": PROVIDER, "id": MODEL}}
        if permissions is not None:
            payload["permissions"] = permissions
        status, response = request("POST", "/api/session", payload)
        if status != 200:
            raise RuntimeError("native session create refused")
        if permissions is not None and response["data"].get("permissions") != permissions:
            raise RuntimeError("native session did not retain the restrictive permission rules")
        return response["data"]["id"]

    def prompt(session_id, text):
        return request(
            "POST",
            "/api/session/" + session_id + "/prompt",
            {"id": "msg_" + secrets.token_hex(12), "text": text, "delivery": "steer", "resume": True},
        )

    def await_permission_request(session_id):
        """Poll the authoritative request route; the recorded row is the one that found the request."""
        deadline = time.monotonic() + 45
        while True:
            status, listing = request("GET", "/api/session/" + session_id + "/permission", record=False)
            if status == 200 and isinstance(listing, dict) and listing.get("data"):
                _, recorded = request("GET", "/api/session/" + session_id + "/permission")
                return (recorded.get("data") or [None])[0]
            if time.monotonic() >= deadline:
                request("GET", "/api/session/" + session_id + "/permission")
                return None
            time.sleep(0.5)

    def tool_outcome(session_id, after):
        for event in receipt["events"][after:]:
            if event.get("type") == "session.tool.success" and event.get("data", {}).get("sessionID") == session_id:
                data = event.get("data", {})
                return {
                    "event": "session.tool.success",
                    "id": data.get("id"),
                    "executed": data.get("executed"),
                }
        return None

    def delete_session(session_id):
        request("DELETE", "/api/session/" + session_id)
        request("GET", "/api/session/" + session_id)

    allow_read = [
        {"action": "*", "resource": "*", "effect": "deny"},
        {"action": "read", "resource": str(root / "baseline.txt"), "effect": "allow"},
    ]
    ask_read = [
        {"action": "*", "resource": "*", "effect": "deny"},
        {"action": "read", "resource": "*", "effect": "ask"},
    ]
    try:
        deadline = time.monotonic() + 60
        identity_seen = False
        while True:
            try:
                status, info = request("GET", "/api/info")
                if status == 200 and readiness_identity(info, process.pid):
                    break
                identity_seen = identity_seen or status == 200
            except (OSError, http.client.HTTPException):
                pass
            if process.poll() is not None or time.monotonic() >= deadline:
                # The Linux server answers 503 from its listening socket before the API is ready.
                raise RuntimeError(
                    "native server identity mismatch" if identity_seen else "native server readiness unavailable"
                ) from None
            time.sleep(0.1)
        receipt["readiness"] = {"route": "GET /api/info", "status": status, "pid": info.get("pid"), "version": info.get("version")}
        request("GET", "/api/info", authenticated=False)
        request("GET", "/api/health")
        # This native route can expose expanded provider settings; redact in memory.
        request("GET", "/api/model")
        stream_connection = http.client.HTTPConnection("127.0.0.1", port, timeout=30)
        stream_connection.request(
            "GET", "/api/event", headers={"Authorization": authorization, "Accept": "text/event-stream"}
        )
        response = stream_connection.getresponse()
        stream_type = response.getheader("Content-Type", "")
        receipt["stream"] = {"route": "GET /api/event", "status": response.status, "content_type": stream_type}
        receipt["operations"].append(
            {
                "method": "GET",
                "path": "/api/event",
                "authenticated": True,
                "payload": None,
                "status": response.status,
                "body": "<event stream captured into events>",
                "content_type": stream_type,
                "credential_redacted_before_write": False,
            }
        )
        if response.status != 200 or "text/event-stream" not in stream_type:
            raise RuntimeError("native SSE unavailable")
        stream_socket = stream_connection.sock
        stream_socket.settimeout(None)

        stream_reader = threading.Thread(target=capture_events, args=(response, key, receipt, condition), daemon=True)
        stream_reader.start()
        if mode in ("full", "truncation"):
            document_status, document = api_document(port, authorization)
            schema = {"route": "GET /openapi.json", "status": document_status}
            if document_status == 200:
                schema.update(
                    {
                        "bytes": len(document),
                        "sha256": hashlib.sha256(document).hexdigest(),
                        **resolve_prompt_schema(json.loads(document)),
                    }
                )
            else:
                schema.update({"properties": [], "bounding_field": None, "blocker": "the API document is unavailable"})
            receipt["prompt_schema"] = schema
        if mode == "full":
            success_id = session(allow_read)
            request("GET", "/api/session/" + success_id + "/event?after=0")
            start = len(receipt["events"])
            prompt(success_id, "Reply exactly OPENCODE_NATIVE_SUCCESS. Do not use tools.")
            terminal = await_event(success_id, TERMINALS, start)
            receipt["classes"]["success"] = {"status": "run and failed", "terminal": terminal}
            request("GET", "/api/session/" + success_id)
            continuation_id = None
            if terminal and terminal.get("type") == "session.execution.succeeded":
                continuation_id = session(ask_read)
                start = len(receipt["events"])
                prompt(
                    continuation_id,
                    "Use the read tool to read baseline.txt, then reply with its contents. Do not use any other tool.",
                )
                permission = await_permission_request(continuation_id)
                continuation = {"event_start": start, "request": permission}
                if permission:
                    continuation["request_id"] = permission.get("id")
                    status, _ = request(
                        "POST",
                        "/api/session/" + continuation_id + "/permission/" + permission["id"] + "/reply",
                        dict(PERMISSION_REPLY),
                    )
                    continuation["reply"] = {"status": status, "body": dict(PERMISSION_REPLY)}
                continuation["terminal"] = await_event(continuation_id, TERMINALS, start)
                continuation["tool_outcome"] = tool_outcome(continuation_id, start)
                request("GET", "/api/session/" + continuation_id)
                receipt["classes"]["continuation_after_step"] = continuation
            else:
                receipt["classes"]["continuation_after_step"] = {
                    "status": "unavailable",
                    "blocker": "initial success was not observed",
                }
            interrupt_id = session(allow_read)
            start = len(receipt["events"])
            prompt(interrupt_id, "Output the integers from 1 through 10000, one per line. Do not use tools.")
            token = await_event(interrupt_id, FIRST_TOKEN_KINDS | TERMINALS, start, 30)
            if token and token.get("type") in TERMINALS:
                token = None
            status, interruption = request("POST", "/api/session/" + interrupt_id + "/interrupt?resume=false")
            receipt["classes"]["interruption"] = {
                "status": "observed" if token else "run and failed",
                "first_token": token,
                "http_status": status,
                "response": interruption,
                "terminal": await_event(interrupt_id, TERMINALS, start, 30),
            }
            request("GET", "/api/session/" + interrupt_id)
            for cleanup_session_id in (success_id, continuation_id, interrupt_id):
                if cleanup_session_id:
                    delete_session(cleanup_session_id)
        elif mode == "provider-failure":
            session_id = session()
            start = len(receipt["events"])
            prompt(session_id, "Reply with the single word PROBE. Do not use tools.")
            terminal = await_event(session_id, TERMINALS, start)
            receipt["classes"]["provider_failure"] = {"status": "run and failed", "terminal": terminal}
            request("GET", "/api/session/" + session_id)
            delete_session(session_id)
        elif mode == "probe":
            session_id = session()
            request("GET", "/api/session/" + session_id + "/event?after=0")
            delete_session(session_id)
        else:
            schema = receipt.get("prompt_schema", {})
            field = schema.get("bounding_field")
            if field:
                session_id = session(allow_read)
                start = len(receipt["events"])
                request(
                    "POST",
                    "/api/session/" + session_id + "/prompt",
                    {
                        "id": "msg_" + secrets.token_hex(12),
                        "text": "Output the integers from 1 through 1000000, one per line. Do not use tools.",
                        "delivery": "steer",
                        "resume": True,
                        field: 1,
                    },
                )
                terminal = await_event(session_id, TERMINALS, start)
                request("GET", "/api/session/" + session_id)
                receipt["classes"]["truncated_response"] = {
                    "status": (
                        "observed"
                        if truncation_marker(receipt["events"][start:])
                        and terminal
                        and terminal.get("type") == "session.execution.succeeded"
                        else "run and failed"
                    ),
                    "field": field,
                    "terminal": terminal,
                    "blocker": None if truncation_marker(receipt["events"][start:]) else "no native output bound in the captured response",
                }
                delete_session(session_id)
            else:
                receipt["classes"]["truncated_response"] = {
                    "status": "unavailable",
                    "blocker": (
                        "the live prompt request schema exposes no output-length bound; "
                        f"properties {schema.get('properties')}, document sha256 {schema.get('sha256')}"
                    ),
                }
    except Exception as error:
        # Do not print exception messages: upstream errors can embed credentials.
        receipt["failure"] = type(error).__name__
    finally:
        receipt["classes"]["cleanup"] = close_native(process, stream_connection, stream_reader, readers)
        receipt["logs"] = logs
        for name in (
            "success",
            "provider_failure",
            "interruption",
            "continuation_after_step",
            "truncated_response",
            "usage",
        ):
            receipt["classes"].setdefault(
                name,
                {
                    "status": "unavailable",
                    "blocker": receipt.get(
                        "failure", "no approved real-relay fault injection; synthetic substitutes excluded"
                    ),
                },
            )
        classify(receipt)
        if receipt["classes"]["cleanup"]["status"] != "observed":
            receipt["failure"] = "CleanupIncomplete"
        files = [p for p in root.rglob("*") if p.is_file()]
        matches = [str(p.relative_to(root)) for p in files if key and key.encode() in p.read_bytes()]
        receipt["credential_scan"] = {"files_scanned": len(files), "matches": matches}
        _, counts = redact(json.dumps(receipt, ensure_ascii=True, indent=2), replacements)
        receipt["redaction"] = {"occurrences": counts}
        if matches:
            raise RuntimeError("credential locality evidence refused")
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_bytes(receipt_bytes(receipt, replacements, (key, password, authorization, endpoint)))
        print(
            json.dumps(
                {
                    "artifact": str(args.out),
                    "mode": mode,
                    "classes": {k: v["status"] for k, v in receipt["classes"].items()},
                    "failure": receipt.get("failure"),
                }
            )
        )
    return 1 if "failure" in receipt else 0


if __name__ == "__main__":
    raise SystemExit(main())
