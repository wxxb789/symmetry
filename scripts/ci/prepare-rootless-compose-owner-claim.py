#!/usr/bin/env python3
"""Create one real Portal WorkItem and generic OpenCode Claim for Compose refusal evidence."""

import http.cookiejar
import json
import pathlib
import re
import secrets
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

LOGIN_CSRF = re.compile(r'name="_csrf_token"\s+value="([^"]+)"')
PAGE_CSRF = re.compile(r'<meta name="csrf-token" content="([^"]+)"')


def fail(message: str) -> None:
    raise SystemExit(f"rootless Compose Control fixture failed: {message}")


def read_json_response(response, expected_status: int):
    if response.status != expected_status:
        fail(f"Control fixture endpoint returned HTTP {response.status}")
    try:
        return json.loads(response.read().decode("utf-8", errors="strict"))
    except (UnicodeError, json.JSONDecodeError):
        fail("Control fixture endpoint returned invalid JSON")


def json_request(client, base_url, method, path, payload, token=None, csrf=None, idempotency=None):
    body = None if payload is None else json.dumps(payload, separators=(",", ":")).encode("utf-8")
    headers = {"Accept": "application/json"}
    if body is not None:
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    if csrf:
        headers.update({"x-csrf-token": csrf, "Origin": base_url, "Referer": base_url + "/portal/"})
    if idempotency:
        headers["Idempotency-Key"] = idempotency
    request = urllib.request.Request(base_url + path, data=body, method=method, headers=headers)
    expected_status = 202 if method == "POST" and path.endswith("/run") else 201 if method == "POST" else 200
    try:
        with client.open(request, timeout=20) as response:
            return read_json_response(response, expected_status)
    except urllib.error.HTTPError as error:
        fail(f"Control fixture endpoint refused {method} {path} (HTTP {error.code})")
    except urllib.error.URLError:
        fail(f"Control fixture endpoint unavailable for {method} {path}")


def portal_client(base_url: str, operator_token: str, ca_file: str):
    jar = http.cookiejar.CookieJar()
    tls = urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=ca_file))
    client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar), tls)
    try:
        with client.open(base_url + "/portal/login", timeout=20) as response:
            login_page = response.read(1 << 20).decode("utf-8", errors="strict")
            if response.status != 200:
                fail("Control Portal login page returned a non-success status")
    except (urllib.error.URLError, UnicodeError):
        fail("Control Portal login page was unavailable")
    token_match = LOGIN_CSRF.search(login_page)
    if not token_match:
        fail("Control Portal login page omitted CSRF token")
    form = urllib.parse.urlencode({"_csrf_token": token_match.group(1), "operator_token": operator_token}).encode("utf-8")
    request = urllib.request.Request(
        base_url + "/portal/login", data=form, method="POST",
        headers={
            "Content-Type": "application/x-www-form-urlencoded",
            "Accept": "text/html",
            "Origin": base_url,
            "Referer": base_url + "/portal/login",
        },
    )
    try:
        with client.open(request, timeout=20) as response:
            portal_page = response.read(1 << 20).decode("utf-8", errors="strict")
            if response.status != 200:
                fail("Control Portal login did not establish an operator session")
    except (urllib.error.URLError, UnicodeError):
        fail("Control Portal operator login failed")
    page_token = PAGE_CSRF.search(portal_page)
    if not page_token:
        fail("authenticated Control Portal page omitted CSRF token")
    return client, page_token.group(1)


def main() -> int:
    if len(sys.argv) != 11:
        fail("expected Control URL, operator/enrollment tokens, machine name, two output files, repository, commit, tree digest, and fixture CA file")
    (
        control_url, operator_token, enrollment_token, machine_name,
        run_output, machine_token_output, repository, commit, tree_digest, ca_file,
    ) = sys.argv[1:]
    parsed = urllib.parse.urlparse(control_url)
    if parsed.scheme != "https" or not parsed.netloc or parsed.path not in ("", "/"):
        fail("Control Portal URL must be an absolute HTTPS origin")
    base_url = control_url.rstrip("/")
    if not operator_token or not enrollment_token or not machine_name:
        fail("development operator/enrollment fixture values are required")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        fail("repository identity is invalid")
    if not re.fullmatch(r"(?:[0-9a-f]{40}|[0-9a-f]{64})", commit):
        fail("repository commit must be a full lowercase SHA")
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", tree_digest):
        fail("repository tree digest is invalid")

    client, csrf = portal_client(base_url, operator_token, ca_file)
    api_client = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=ca_file)))
    suffix = secrets.token_hex(6).upper()
    project = json_request(client, base_url, "POST", "/portal/api/projects", {
        "name": "Rootless Compose hidden owner witness " + suffix,
        "key": "RCH" + suffix[:5],
        "description": "Isolated non-Goal hidden owner-config refusal fixture.",
        "default_agent_profile": "opencode-native",
        "default_workspace": "native",
    }, csrf=csrf)["project"]
    project_id = project.get("id")
    if not project_id:
        fail("Control did not return the hidden-witness project ID")
    resource = json_request(client, base_url, "POST", f"/portal/api/projects/{project_id}/resources", {
        "kind": "repository",
        "name": "Rootless Compose hidden owner witness repository " + suffix,
    }, csrf=csrf)["resource"]
    resource_id = resource.get("id")
    if not resource_id:
        fail("Control did not return the hidden-witness repository resource ID")

    machine_token = secrets.token_hex(32)
    machine = json_request(api_client, base_url, "POST", "/api/v1/machines", {
        "machine": {"name": machine_name}, "machine_token": machine_token,
    }, token=enrollment_token, idempotency=str(uuid.uuid4()))
    machine_id = machine.get("machine_id")
    if not machine_id or machine.get("machine_token") != machine_token:
        fail("Control did not enroll the exact generic owner-claim fixture machine")
    session_id = str(uuid.uuid4())
    session = json_request(api_client, base_url, "PUT", f"/api/v1/machines/{machine_id}/sessions/{session_id}", {
        "runtimes": [{
            "runtime_key": "rootless-compose-unprovisioned-opencode-claim",
            "name": "Rootless Compose hidden owner claim fixture; no native adapter",
            "capacity": 1,
            "agent_profile": "opencode-native",
            "workspace": "native",
            "repository_resource_id": resource_id,
            "capabilities": {"structured_input": True},
        }],
    }, token=machine_token)
    runtimes = [runtime for runtime in session.get("runtimes", []) if isinstance(runtime, dict)]
    runtime = next((runtime for runtime in runtimes if runtime.get("runtime_key") == "rootless-compose-unprovisioned-opencode-claim"), None)
    if not runtime or not runtime.get("runtime_id") or not isinstance(runtime.get("runtime_epoch"), int) or runtime["runtime_epoch"] <= 0:
        fail("Control did not register the generic hidden-witness OpenCode runtime")

    work_item = json_request(client, base_url, "POST", f"/portal/api/projects/{project_id}/work-items", {
        "title": "Rootless Compose hidden owner refusal " + suffix,
        "description": "Use the real OpenCode runtime for the rootless Compose hidden owner-config refusal witness. Do not publish.",
        "status": "ready",
        "priority": "high",
        "assignee_type": "agent",
        "agent_profile": "opencode-native",
        "workspace": "native",
        "repository_resource_id": resource_id,
    }, csrf=csrf)["work_item"]
    work_item_id = work_item.get("id")
    if not work_item_id:
        fail("Control did not return the hidden-witness WorkItem ID")
    json_request(client, base_url, "POST", f"/portal/api/work-items/{work_item_id}/run", {
        "action_id": str(uuid.uuid4()),
    }, csrf=csrf)

    deadline = time.monotonic() + 60
    execution = None
    while time.monotonic() < deadline:
        detail = json_request(client, base_url, "GET", f"/portal/api/work-items/{work_item_id}", None)
        current = detail.get("work_item", {}).get("execution")
        if isinstance(current, dict) and all(current.get(key) for key in ("task_id", "run_id", "runtime_id")) and current.get("generation", 0) > 0:
            execution = current
            break
        time.sleep(0.1)
    if execution is None or execution["runtime_id"] != runtime["runtime_id"]:
        fail("Control did not assign the hidden OpenCode Run to its exact generic claim fixture runtime")

    run_metadata = {
        "version": 1,
        "case": "stage_replay",
        "fixture_case": "rootless_compose_unprovisioned_owner_witness",
        "harness": "opencode",
        "run_id": execution["run_id"],
        "task_id": execution["task_id"],
        "generation": execution["generation"],
        "runtime_id": runtime["runtime_id"],
        "runtime_epoch": runtime["runtime_epoch"],
        "claim_id": str(uuid.uuid4()),
        "work_item_id": work_item_id,
        "repository_resource_id": resource_id,
        "commit": commit,
        "tree_digest": tree_digest,
        "agent_profile": "opencode-native",
        "workspace": "native",
    }
    for path, value in ((run_output, run_metadata), (machine_token_output, machine_token)):
        output = pathlib.Path(path)
        output.write_text(json.dumps(value, separators=(",", ":")) if isinstance(value, dict) else value, encoding="utf-8")
        output.chmod(0o600)
    print("prepared separate authentic rootless Compose OpenCode WorkItem/Run/Claim fixture")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
