#!/usr/bin/env python3
"""Observe the owner-pinned Windows OpenCode blob; never promote capabilities.

The operator credential is inherited only through TENCENT_GLM_API_KEY. Native
HTTP payloads, logs and state are checked before evidence is written. No proxy,
synthetic provider, credential endpoint or remote mutation is used.
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
import socket
import subprocess
import tempfile
import threading
import time


BINARY = pathlib.Path(r"C:\Users\lhan\AppData\Local\mise\installs\aqua-anomalyco-opencode\latest\opencode.exe")
DIGEST = "38153a142354634c82b5cfe6c00b19df15a71e6cf8c17ef5f5c1a4cc684a6917"
SIZE = 257553288
MODEL = "deepseek-v4.1-flash-fast"
PROVIDER = "tencent-litellm"


def classify(receipt):
    """Interpret captured native facts, never HTTP admission or idle as success."""
    classes = receipt["classes"]
    success = classes.get("success", {})
    terminal = success.get("terminal")
    success["status"] = "observed" if terminal and terminal.get("type") == "session.execution.succeeded" else "run and failed" if "success" in classes else "unavailable"
    classes["success"] = success
    failures = [event for event in receipt["events"] if event.get("type") == "session.execution.failed"
                and event.get("data", {}).get("error", {}).get("type", "").startswith("provider.")]
    classes["provider_failure"] = {"status": "observed", "terminals": failures} if failures else {
        "status": "unavailable", "blocker": "no real provider failure captured; synthetic substitutes excluded"}
    continuation = classes.get("continuation_after_step", {})
    terminal = continuation.get("terminal") or {}
    if "event_start" in continuation:
        window = receipt["events"][continuation["event_start"]:]
        terminal_index = next((index for index, event in enumerate(window) if terminal.get("id") and event == terminal), None)
        window = window[:terminal_index] if terminal_index is not None else []
        kinds = [event.get("type") for event in window
                 if event.get("data", {}).get("sessionID") == terminal.get("data", {}).get("sessionID")]
        continued = any("session.tool.success" in kinds[:index] and "session.step.started" in kinds[index + 1:]
                        for index, kind in enumerate(kinds) if kind == "session.step.ended")
        continuation["status"] = "observed" if continued and terminal.get("type") == "session.execution.succeeded" else "run and failed"
    else:
        continuation.update(status="unavailable", blocker="no successful tool continuation captured")
    classes["continuation_after_step"] = continuation
    interruption = classes.get("interruption", {})
    if interruption:
        interrupted = interruption.get("response") or {}
        terminal = interruption.get("terminal") or {}
        interruption["status"] = "observed" if interruption.get("first_token") and interrupted.get("interrupted") is True and terminal.get("type") == "session.execution.interrupted" else "run and failed"
    else:
        interruption = {"status": "unavailable", "blocker": "no active native interruption captured"}
    classes["interruption"] = interruption
    counters = []
    for operation in receipt["operations"]:
        if operation["method"] == "GET" and operation["status"] == 200 and operation["path"].startswith("/api/session/"):
            data = json.loads(operation["body"]).get("data", {})
            if "tokens" in data:
                counters.append({name: data.get(name) for name in ("id", "outcome", "tokens", "cost")})
    classes["usage"] = {"status": "observed" if counters else "unavailable", "native_counters": counters,
                        "limitation": "native counters only; successful/billable usage and failed-turn accounting unverified"}
    cleanup = classes.get("cleanup", {})
    cleanup["status"] = "observed" if cleanup.get("exit_code") == 0 and cleanup.get("server_exit_code") is not None and cleanup.get("stream_reader_stopped") is True and cleanup.get("output_readers_stopped") is True else "run and failed"
    classes["cleanup"] = cleanup
    classes.setdefault("truncated_response", {"status": "unavailable", "blocker": "no successful real-relay response or approved provider fault injection; synthetic substitutes excluded"})
    return receipt


def close_native(process, connection, stream_reader, output_readers):
    """Always reach process, socket and reader barriers, including tree-kill failure."""
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
    finally:
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
    result.update(server_exit_code=process.returncode,
                  stream_reader_stopped=stream_reader is None or not stream_reader.is_alive(),
                  output_readers_stopped=all(not reader.is_alive() for reader in output_readers))
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, type=pathlib.Path)
    args = parser.parse_args()
    key = os.environ.get("TENCENT_GLM_API_KEY", "")
    if not key or os.name != "nt":
        raise RuntimeError("Windows and the operator credential environment are required")
    with BINARY.open("rb") as binary:
        if os.fstat(binary.fileno()).st_size != SIZE or hashlib.file_digest(binary, "sha256").hexdigest() != DIGEST:
            raise RuntimeError("owner-pinned executable identity mismatch")
    root = pathlib.Path(tempfile.mkdtemp(prefix="symmetry-0006g-witness-"))
    repository = pathlib.Path(__file__).resolve().parents[2]
    receipt = {
        "decision": "unsupported", "binary": {"path": str(BINARY), "size": SIZE, "sha256": DIGEST},
        "platform": {"system": platform.system(), "kernel": platform.version(), "distribution": platform.platform()},
        "source_head": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=repository, text=True).strip(),
        "witness_sha256": hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),
        "command": ["python3", "scripts/ci/opencode-v2-support-witness.py", "--out", str(args.out)],
        "workspace": str(root), "operations": [], "events": [], "classes": {},
    }
    environment = {k: v for k, v in os.environ.items() if k.upper() in {
        "PATH", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "SYSTEMDRIVE"}}
    for name, directory in {"HOME": "home", "USERPROFILE": "home", "APPDATA": "appdata",
                            "LOCALAPPDATA": "localappdata", "XDG_CONFIG_HOME": "config",
                            "XDG_CACHE_HOME": "cache", "XDG_DATA_HOME": "data",
                            "XDG_STATE_HOME": "state", "TMP": "tmp", "TEMP": "tmp"}.items():
        target = root / directory
        target.mkdir(exist_ok=True)
        environment[name] = str(target)
    source_config = pathlib.Path.home() / ".config/opencode/opencode.json"
    provider = json.loads(source_config.read_text())["provider"][PROVIDER]
    endpoint = provider["options"]["baseURL"]
    if not endpoint.startswith("https://"):
        raise RuntimeError("configured relay is not HTTPS")
    # Keep only this provider/model, replacing the source key without copying it.
    provider["options"] = {"baseURL": endpoint, "apiKey": "{env:TENCENT_GLM_API_KEY}"}
    provider["models"] = {MODEL: provider["models"][MODEL]}
    config = {"model": PROVIDER + "/" + MODEL, "small_model": PROVIDER + "/" + MODEL,
              "enabled_providers": [PROVIDER], "provider": {PROVIDER: provider}}
    encoded_config = json.dumps(config)
    if key in encoded_config:
        raise RuntimeError("credential in configuration refused")
    config_path = root / "config/opencode.json"
    config_path.write_text(encoded_config)
    environment.update({"OPENCODE_CONFIG": str(config_path), "OPENCODE_CONFIG_DIR": str(config_path.parent),
                        "OPENCODE_TEST_MANAGED_CONFIG_DIR": str(root / "managed"), "TENCENT_GLM_API_KEY": key})
    (root / "managed").mkdir()
    for name in ("OPENCODE_DISABLE_PROJECT_CONFIG", "OPENCODE_DISABLE_MODELS_FETCH", "OPENCODE_DISABLE_AUTOUPDATE",
                 "OPENCODE_DISABLE_DEFAULT_PLUGINS", "OPENCODE_DISABLE_EXTERNAL_SKILLS", "OPENCODE_DISABLE_CLAUDE_CODE_SKILLS"):
        environment[name] = "1"
    password = secrets.token_urlsafe(32)
    environment.update({"OPENCODE_SERVER_USERNAME": "opencode", "OPENCODE_SERVER_PASSWORD": password})
    authorization = "Basic " + base64.b64encode(("opencode:" + password).encode()).decode()
    receipt["provider"] = {"id": PROVIDER, "model": MODEL, "base_url": endpoint,
                           "discovered_from": str(source_config), "credential": "environment:TENCENT_GLM_API_KEY"}
    (root / "baseline.txt").write_text("native continuation witness\n")
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    argv = [str(BINARY), "serve", "--hostname", "127.0.0.1", "--port", str(port)]
    receipt["server_command"] = argv
    process = subprocess.Popen(argv, cwd=root, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
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

    def request(method, path, payload=None, authenticated=True):
        body = json.dumps(payload).encode() if payload is not None else None
        if body and key.encode() in body:
            raise RuntimeError("credential request payload refused")
        headers = {"Content-Type": "application/json"}
        if authenticated:
            headers["Authorization"] = authorization
        connection = http.client.HTTPConnection("127.0.0.1", port, timeout=15)
        try:
            connection.request(method, path, body=body, headers=headers)
            response = connection.getresponse()
            raw = response.read(8 << 20).decode(errors="replace")
            redacted = key in raw
            safe_raw = raw.replace(key, "<operator credential redacted>")
            operation = {"method": method, "path": path, "authenticated": authenticated,
                         "payload": payload, "status": response.status, "body": safe_raw,
                         "credential_redacted_before_write": redacted}
            receipt["operations"].append(operation)
            return response.status, json.loads(safe_raw) if safe_raw else None
        finally:
            connection.close()

    def await_event(session_id, kinds, after, timeout=60):
        deadline = time.monotonic() + timeout
        with condition:
            while True:
                for event in receipt["events"][after:]:
                    if event.get("type") in kinds and event.get("data", {}).get("sessionID") == session_id and event.get("durable", {}).get("aggregateID", session_id) == session_id:
                        return event
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return None
                condition.wait(remaining)

    def session():
        permissions = [{"action": "*", "resource": "*", "effect": "deny"},
                       {"action": "read", "resource": str(root / "baseline.txt"), "effect": "allow"}]
        status, response = request("POST", "/api/session", {"location": {"directory": str(root)},
                                   "model": {"providerID": PROVIDER, "id": MODEL}, "permissions": permissions})
        if status != 200 or response["data"].get("permissions") != permissions:
            raise RuntimeError("native session did not retain the restrictive permission rules")
        return response["data"]["id"]

    def prompt(session_id, text):
        return request("POST", "/api/session/" + session_id + "/prompt",
                       {"id": "msg_" + secrets.token_hex(12), "text": text, "delivery": "steer", "resume": True})

    terminals = {"session.execution.succeeded", "session.execution.failed", "session.execution.interrupted"}
    try:
        deadline = time.monotonic() + 30
        while True:
            try:
                status, info = request("GET", "/api/info")
                if status != 200 or info.get("pid") != process.pid or info.get("version") != "2.3.0":
                    raise RuntimeError("native server identity mismatch")
                break
            except (OSError, http.client.HTTPException):
                if process.poll() is not None or time.monotonic() >= deadline:
                    raise RuntimeError("native server readiness unavailable") from None
                time.sleep(0.1)
        request("GET", "/api/info", authenticated=False)
        request("GET", "/api/health")
        # This native route exposes expanded provider settings; redact in memory.
        request("GET", "/api/model")
        stream_connection = http.client.HTTPConnection("127.0.0.1", port, timeout=15)
        stream_connection.request("GET", "/api/event", headers={"Authorization": authorization, "Accept": "text/event-stream"})
        response = stream_connection.getresponse()
        receipt["stream_status"] = response.status
        if response.status != 200 or "text/event-stream" not in response.getheader("Content-Type", ""):
            raise RuntimeError("native SSE unavailable")
        stream_socket = stream_connection.sock
        stream_socket.settimeout(None)

        def capture():
            count = 0
            try:
                while True:
                    line = response.fp.readline(1 << 20)
                    if not line:
                        break
                    count += len(line)
                    if count > 8 << 20:
                        raise RuntimeError("native SSE capture limit reached")
                    if line.startswith(b"data:"):
                        text = line[5:].strip().decode()
                        if key in text:
                            raise RuntimeError("credential native event refused")
                        event = json.loads(text)
                        if isinstance(event, str):
                            event = json.loads(event)
                        with condition:
                            receipt["events"].append(event)
                            condition.notify_all()
            except (OSError, ValueError, RuntimeError) as error:
                receipt["stream_end"] = type(error).__name__

        stream_reader = threading.Thread(target=capture, daemon=True)
        stream_reader.start()
        session_id = session()
        request("GET", "/api/session/" + session_id + "/event?after=0")
        start = len(receipt["events"])
        prompt(session_id, "Reply exactly OPENCODE_NATIVE_SUCCESS. Do not use tools.")
        terminal = await_event(session_id, terminals, start)
        receipt["classes"]["success"] = {"status": "run and failed", "terminal": terminal}
        request("GET", "/api/session/" + session_id)
        if terminal and terminal.get("type") == "session.execution.succeeded":
            start = len(receipt["events"])
            prompt(session_id, "Use the read tool to read baseline.txt, then reply with its contents. Do not use any other tool.")
            receipt["classes"]["continuation_after_step"] = {"event_start": start, "terminal": await_event(session_id, terminals, start)}
            request("GET", "/api/session/" + session_id)
        else:
            receipt["classes"]["continuation_after_step"] = {"status": "unavailable", "blocker": "initial success was not observed"}
        interrupt_id = session()
        start = len(receipt["events"])
        prompt(interrupt_id, "Output the integers from 1 through 10000, one per line. Do not use tools.")
        token = await_event(interrupt_id, {"session.step.first-token", "session.text.delta", "session.reasoning.delta"} | terminals, start, 30)
        if token and token.get("type") in terminals:
            token = None
        status, interruption = request("POST", "/api/session/" + interrupt_id + "/interrupt")
        receipt["classes"]["interruption"] = {"status": "observed" if token else "run and failed", "first_token": token,
                                                "http_status": status, "response": interruption,
                                                "terminal": await_event(interrupt_id, terminals, start, 15)}
        request("GET", "/api/session/" + interrupt_id)
        for session_id in (session_id, interrupt_id):
            request("DELETE", "/api/session/" + session_id)
            request("GET", "/api/session/" + session_id)
    except Exception as error:
        # Do not print exception messages: upstream errors can embed credentials.
        receipt["failure"] = type(error).__name__
    finally:
        receipt["classes"]["cleanup"] = close_native(process, stream_connection, stream_reader, readers)
        receipt["logs"] = logs
        for name in ("success", "provider_failure", "interruption", "continuation_after_step", "truncated_response", "usage"):
            receipt["classes"].setdefault(name, {"status": "unavailable", "blocker": receipt.get("failure", "no approved real-relay fault injection; synthetic substitutes excluded")})
        classify(receipt)
        if receipt["classes"]["cleanup"]["status"] != "observed":
            receipt["failure"] = "CleanupIncomplete"
        files = [p for p in root.rglob("*") if p.is_file()]
        matches = [str(p.relative_to(root)) for p in files if key.encode() in p.read_bytes()]
        receipt["credential_scan"] = {"files_scanned": len(files), "matches": matches}
        data = (json.dumps(receipt, ensure_ascii=True, indent=2) + "\n").encode()
        if matches or any(secret.encode() in data for secret in (key, password, authorization)):
            raise RuntimeError("credential locality evidence refused")
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_bytes(data)
        print(json.dumps({"artifact": str(args.out), "classes": {k: v["status"] for k, v in receipt["classes"].items()}, "failure": receipt.get("failure")}))
    return 1 if "failure" in receipt else 0


if __name__ == "__main__":
    raise SystemExit(main())
