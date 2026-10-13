#!/usr/bin/env python3
"""Enroll generic Control claim runtimes and a private installed-daemon identity."""

import json
import os
import pathlib
import secrets
import sys
import uuid
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


def call_json(url: str, method: str, token: str, payload: dict, expected_status: int, extra_headers: dict | None = None) -> dict:
    body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    headers = {
        "Authorization": "Bearer " + token,
        "Content-Type": "application/json",
    }
    if extra_headers:
        headers.update(extra_headers)
    request = Request(
        url,
        data=body,
        method=method,
        headers=headers,
    )
    try:
        with urlopen(request, timeout=20) as response:
            if response.status != expected_status:
                raise SystemExit(f"Control claim fixture endpoint returned HTTP {response.status}")
            return json.loads(response.read().decode("utf-8", errors="strict"))
    except HTTPError as error:
        raise SystemExit(f"Control claim fixture endpoint refused setup (HTTP {error.code})")
    except URLError:
        raise SystemExit("Control claim fixture endpoint was unavailable")
    except (json.JSONDecodeError, UnicodeError):
        raise SystemExit("Control claim fixture endpoint returned invalid JSON")


def main() -> int:
    if len(sys.argv) != 8:
        print(
            "usage: register-linux-v2-claim-machine.py CONTROL_FIXTURE.json CONTROL_URL MACHINE_NAME MACHINE_TOKEN_FILE RESULT.json SERVICE_MACHINE_NAME SERVICE_IDENTITY.json",
            file=sys.stderr,
        )
        return 2

    (
        fixture_path,
        control_url,
        machine_name,
        machine_token_path,
        result_path,
        service_machine_name,
        service_identity_path,
    ) = sys.argv[1:]
    fixture = json.loads(pathlib.Path(fixture_path).read_text(encoding="utf-8"))
    enrollment_token = os.environ.get("SYMMETRY_ENROLLMENT_TOKEN", "")
    if not enrollment_token:
        raise SystemExit("test-only Control enrollment token is required")

    machine_token = secrets.token_hex(32)
    machine_idempotency_key = str(uuid.uuid4())
    enrollment = call_json(
        control_url.rstrip("/") + "/api/v1/machines",
        "POST",
        enrollment_token,
        {
            "machine": {"name": machine_name},
            "machine_token": machine_token,
        },
        201,
        {"Idempotency-Key": machine_idempotency_key},
    )
    machine_id = enrollment.get("machine_id")
    if not machine_id or enrollment.get("machine_token") != machine_token:
        raise SystemExit("Control did not enroll the exact one-time claim fixture machine")

    session_id = str(uuid.uuid4())
    response = call_json(
        control_url.rstrip("/") + f"/api/v1/machines/{machine_id}/sessions/{session_id}",
        "PUT",
        machine_token,
        {
            "runtimes": [
                {
                    "runtime_key": "owner-witness-control-pi-generic",
                    "name": "Isolated generic Control provider-claim fixture; not a native runtime",
                    "capacity": 64,
                    "agent_profile": "pi-native",
                    "workspace": "native",
                    "repository_resource_id": fixture["pi_repository_resource_id"],
                    "capabilities": {"structured_input": True, "provider_access": True},
                },
                {
                    "runtime_key": "owner-witness-control-opencode-generic",
                    "name": "Isolated generic Control health-claim fixture; not a native runtime",
                    "capacity": 64,
                    "agent_profile": "opencode-native",
                    "workspace": "native",
                    "repository_resource_id": fixture["opencode_repository_resource_id"],
                    "capabilities": {"structured_input": True},
                },
            ]
        },
        200,
    )
    runtimes = {
        runtime.get("runtime_key"): runtime
        for runtime in response.get("runtimes", [])
        if isinstance(runtime, dict)
    }
    result = {"machine_id": machine_id, "session_id": session_id, "runtimes": {}}
    for runtime_key in ("owner-witness-control-pi-generic", "owner-witness-control-opencode-generic"):
        runtime = runtimes.get(runtime_key)
        if (
            not isinstance(runtime, dict)
            or not runtime.get("runtime_id")
            or not isinstance(runtime.get("runtime_epoch"), int)
            or runtime["runtime_epoch"] <= 0
        ):
            raise SystemExit("Control did not register the two generic claim fixture runtimes")
        result["runtimes"][runtime_key] = {
            "runtime_id": runtime["runtime_id"],
            "runtime_epoch": runtime["runtime_epoch"],
        }

    token_file = pathlib.Path(machine_token_path)
    token_file.write_text(machine_token, encoding="ascii")
    token_file.chmod(0o600)
    result_file = pathlib.Path(result_path)
    result_file.write_text(json.dumps(result, separators=(",", ":")), encoding="utf-8")
    result_file.chmod(0o600)

    service_machine_token = secrets.token_hex(32)
    service_enrollment = call_json(
        control_url.rstrip("/") + "/api/v1/machines",
        "POST",
        enrollment_token,
        {
            "machine": {"name": service_machine_name},
            "machine_token": service_machine_token,
        },
        201,
        {"Idempotency-Key": str(uuid.uuid4())},
    )
    service_machine_id = service_enrollment.get("machine_id")
    if not service_machine_id or service_enrollment.get("machine_token") != service_machine_token:
        raise SystemExit("Control did not enroll the installed-daemon service identity")
    identity_file = pathlib.Path(service_identity_path)
    identity_file.write_text(
        json.dumps(
            {"machine_id": service_machine_id, "machine_token": service_machine_token},
            separators=(",", ":"),
        ),
        encoding="utf-8",
    )
    identity_file.chmod(0o600)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
