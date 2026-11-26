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


def call_json(
    url: str, method: str, token: str, payload: dict, expected_status: int, extra_headers: dict | None = None
) -> dict:
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
        raise SystemExit(f"Control claim fixture endpoint refused setup (HTTP {error.code})") from error
    except URLError:
        raise SystemExit("Control claim fixture endpoint was unavailable") from None
    except (json.JSONDecodeError, UnicodeError):
        raise SystemExit("Control claim fixture endpoint returned invalid JSON") from None


def goal_assigned_runtime_spec(
    runtime_key: str,
    name: str,
    agent_profile: str,
    repository_resource_id: str,
    harness_kind: str,
    harness_version: str,
    provider_access: bool = False,
) -> dict:
    # Goal assignment admits only native-harness runtimes with adapter operations
    # (assignment.ex:332,:519-530), so the goal-assigned claim fixtures carry native metadata.
    spec = {
        "runtime_key": runtime_key,
        "name": name,
        "capacity": 64,
        "agent_profile": agent_profile,
        "workspace": "native",
        "repository_resource_id": repository_resource_id,
        "harness_kind": harness_kind,
        "harness_version": harness_version,
        "adapter_version": "symmetry-daemon:ci-linux-v2",
        "adapter_protocol_version": 1,
        "capabilities": {
            "structured_input": True,
            "adapter": {
                "kind": harness_kind,
                "native_version": harness_version,
                "implementation_version": "symmetry-daemon:ci-linux-v2",
                "protocol_version": 1,
                "operations": {
                    "start": True,
                    "events": True,
                    "cancel": True,
                    "resume": False,
                    "handoff": False,
                    "guidance": "unsupported",
                    "pause": "unsupported",
                    "approval_response": False,
                    "usage": "unknown",
                    "hard_cost_limit": False,
                },
            },
        },
    }
    if provider_access:
        # The provider legs must satisfy the admission's own provider_access requirement.
        spec["capabilities"]["provider_access"] = True
    return spec


def main() -> int:
    if len(sys.argv) != 8:
        print(
            "usage: register-linux-v2-claim-machine.py CONTROL_FIXTURE.json CONTROL_URL MACHINE_NAME "
            "MACHINE_TOKEN_FILE RESULT.json SERVICE_MACHINE_NAME SERVICE_IDENTITY.json",
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
    runtime_specs = (
        goal_assigned_runtime_spec(
            "owner-witness-control-pi-generic",
            "Isolated generic Control provider-claim fixture; goal-assigned native Pi",
            "pi-native",
            fixture["pi_repository_resource_id"],
            "pi",
            "1.5.2",
            provider_access=True,
        ),
        {
            "runtime_key": "owner-witness-control-opencode-generic",
            "name": "Isolated generic Control health-claim fixture; not a native runtime",
            "capacity": 64,
            "agent_profile": "opencode-native",
            "workspace": "native",
            "repository_resource_id": fixture["opencode_repository_resource_id"],
            "capabilities": {"structured_input": True},
        },
        goal_assigned_runtime_spec(
            "owner-witness-control-pi-durability-generic",
            "Isolated generic Control provider-free Pi durability claim fixture; goal-assigned",
            "pi-durability",
            fixture["pi_durability_repository_resource_id"],
            "pi",
            "1.5.2",
        ),
        goal_assigned_runtime_spec(
            "owner-witness-control-codex-generic",
            "Isolated generic Control provider-free codex containment-claim fixture; goal-assigned",
            "codex-native",
            fixture["opencode_repository_resource_id"],
            "codex",
            "0.168.0",
        ),
        goal_assigned_runtime_spec(
            "owner-witness-control-claude-generic",
            "Isolated generic Control provider-free claude containment-claim fixture; goal-assigned",
            "claude-native",
            fixture["opencode_repository_resource_id"],
            "claude_code",
            "2.1.323",
        ),
    )
    response = call_json(
        control_url.rstrip("/") + f"/api/v1/machines/{machine_id}/sessions/{session_id}",
        "PUT",
        machine_token,
        {"runtimes": list(runtime_specs)},
        200,
    )
    runtimes = {
        runtime.get("runtime_key"): runtime for runtime in response.get("runtimes", []) if isinstance(runtime, dict)
    }
    result = {"machine_id": machine_id, "session_id": session_id, "runtimes": {}}
    for spec in runtime_specs:
        runtime_key = spec["runtime_key"]
        runtime = runtimes.get(runtime_key)
        if (
            not isinstance(runtime, dict)
            or not runtime.get("runtime_id")
            or not isinstance(runtime.get("runtime_epoch"), int)
            or runtime["runtime_epoch"] <= 0
        ):
            raise SystemExit("Control did not register the generic claim fixture runtimes")
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
