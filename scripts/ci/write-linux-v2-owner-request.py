#!/usr/bin/env python3
"""Claim one isolated Control run and bind its authentic response to a v2 request."""

import json
import os
import pathlib
import sys
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

CASES = {
    "pi_provider_action": ("pi", "pi-native", "native_turn_started"),
    "opencode_native_health": ("opencode", "opencode-native", "native_release"),
    "helper_sigkill_detached_child": ("pi", "pi-native", "native_turn_started"),
    "daemon_sigkill_helper_survives_detached_child": ("pi", "pi-native", "native_turn_started"),
    "wsl2_denial": ("opencode", "opencode-native", "none"),
    "stage_replay": ("pi", "pi-durability", None),
    "codex_containment": ("codex", "codex-native", "native_release"),
    "claude_containment": ("claude_code", "claude-native", "native_release"),
    "stage_replay_native_release_codex": ("codex", "codex-native", "native_release"),
    "stage_replay_native_release_claude": ("claude_code", "claude-native", "native_release"),
    "stage_replay_unprovisioned_refusal": ("opencode", "opencode-native", "native_release"),
}
STAGE_REPLAY_NATIVE_RELEASE_CASES = {
    "stage_replay_native_release_codex",
    "stage_replay_native_release_claude",
    "stage_replay_unprovisioned_refusal",
}
PROVIDER_ACCESS_CASES = {name for name, (_, profile, _) in CASES.items() if profile == "pi-native"}
# Exactly the cases whose fixture route commissions a Control Goal (provision-linux-v2-control-claims.exs);
# the profile name is not the routing fact, so the admission branch keys on the case set.
GOAL_ADMISSION_CASES = {
    "stage_replay",
    "codex_containment",
    "claude_containment",
    "stage_replay_native_release_codex",
    "stage_replay_native_release_claude",
}
STAGE_REPLAY_HOLDS = {
    "reservation_durable",
    "create_started",
    "leaf_bound",
    "helper_bound",
    "launcher_bound",
    "gate_bound",
    "handoff_prepared",
    "handoff_committed",
    "lease_armed",
    "checkout_ready",
    "native_release",
    "stop_receipt_durable",
    "leaf_removed",
    "journal_cleared",
}
ACK_LOSS_STAGES = {
    "helper_bound",
    "launcher_bound",
    "gate_bound",
    "handoff_prepared",
    "handoff_committed",
    "lease_armed",
    "checkout_ready",
    "stop_receipt_durable",
    "native_release",
    "journal_cleared",
}


def read_json(path: str):
    with open(path, encoding="utf-8", errors="strict") as stream:
        return json.load(stream)


def require(condition: bool, message: str) -> None:
    if not condition:
        raise SystemExit(message)


def main() -> int:
    if len(sys.argv) not in (8, 9):
        print(
            "usage: write-linux-v2-owner-request.py CASE RUN.json CONFIG.json BARRIER_DIR CONTROL_URL "
            "MACHINE_TOKEN_FILE OUTPUT.json [STAGE_HOLD_AFTER]",
            file=sys.stderr,
        )
        return 2

    case, run_path, config_path, barrier_dir, control_url, machine_token_path, output_path = sys.argv[1:8]
    if case not in CASES:
        print("unknown owner witness case", file=sys.stderr)
        return 2

    harness, profile_name, default_hold_after = CASES[case]
    drop_ack_after = ""
    if case == "stage_replay":
        require(len(sys.argv) == 9, "stage replay request requires an explicit barrier hold")
        selector = sys.argv[8]
        if selector.startswith("drop_ack_after="):
            drop_ack_after = selector.removeprefix("drop_ack_after=")
        hold_after = drop_ack_after or selector
        require(
            selector in STAGE_REPLAY_HOLDS or (drop_ack_after != "" and drop_ack_after in ACK_LOSS_STAGES),
            "stage replay hold is unsupported",
        )
    elif case in STAGE_REPLAY_NATIVE_RELEASE_CASES:
        require(len(sys.argv) == 8, "native-release stage replay requests use their fixed hold")
        hold_after = default_hold_after
    else:
        require(len(sys.argv) == 8, "only stage replay requests accept a hold override")
        hold_after = default_hold_after
    request_case = "stage_replay" if case in STAGE_REPLAY_NATIVE_RELEASE_CASES else case
    run = read_json(run_path)
    config = read_json(config_path)
    runtime = config.get("runtime", {})
    profiles = config.get("agent_profiles", {})
    profile = profiles.get(profile_name)
    require(isinstance(profile, dict), "per-harness daemon profile is absent")
    require(runtime.get("harness_kind") == harness, "request harness differs from daemon config")
    require(runtime.get("agent_profile") == profile_name, "request profile differs from daemon config")
    require(runtime.get("workspace") == "native", "request workspace differs from the A/B clone fixture")
    require(
        runtime.get("repository_resource_id") == run.get("repository_resource_id"),
        "request Subject ResourceID differs from config",
    )
    require(
        run.get("agent_profile") == profile_name and run.get("workspace") == "native",
        "Control Run profile/workspace differs from config",
    )
    require(
        run.get("case") == case and run.get("harness") == harness, "Control Run fixture does not match the witness case"
    )
    require(
        "SYMMETRY_LINUX_V2_NATIVE_HOOK_MARKER_ROOT" in profile.get("env_allowlist", []),
        "native profile omits the non-secret hook marker path",
    )
    require(isinstance(run.get("run_id"), str) and run["run_id"], "Control Run ID is missing")
    require(isinstance(run.get("generation"), int) and run["generation"] > 0, "Control Run generation is invalid")
    require(isinstance(run.get("runtime_id"), str) and run["runtime_id"], "Control Runtime ID is missing")
    require(isinstance(run.get("runtime_epoch"), int) and run["runtime_epoch"] > 0, "Control Runtime epoch is invalid")
    require(isinstance(run.get("claim_id"), str) and run["claim_id"], "Control Claim ID is missing")

    try:
        machine_token = pathlib.Path(machine_token_path).read_text(encoding="ascii").strip()
    except OSError:
        raise SystemExit("test machine Claim credential is unavailable") from None
    require(bool(machine_token), "test machine Claim credential is empty")

    claim_url = control_url.rstrip("/") + "/api/v1/runs/" + run["run_id"] + "/claims/" + run["claim_id"]
    body = json.dumps(
        {
            "runtime_id": run["runtime_id"],
            "runtime_epoch": run["runtime_epoch"],
            "generation": run["generation"],
        }
    ).encode("utf-8")
    claim_request = Request(
        claim_url,
        data=body,
        method="PUT",
        headers={
            "Authorization": "Bearer " + machine_token,
            "Content-Type": "application/json",
        },
    )
    try:
        with urlopen(claim_request, timeout=15) as response:
            if response.status != 200:
                raise SystemExit("Control Claim endpoint returned a non-success status")
            claim = json.loads(response.read().decode("utf-8", errors="strict"))
    except HTTPError as error:
        raise SystemExit(f"Control Claim endpoint refused the exact fixture (HTTP {error.code})") from error
    except URLError:
        raise SystemExit("Control Claim endpoint was unavailable for the exact fixture") from None
    except (json.JSONDecodeError, UnicodeError):
        raise SystemExit("Control Claim endpoint returned an invalid ClaimResponse") from None

    require(isinstance(claim, dict), "Control ClaimResponse is not a JSON object")
    require(claim.get("run_id") == run["run_id"], "ClaimResponse RunKey does not match the fixture RunKey")
    require(claim.get("generation") == run["generation"], "ClaimResponse generation does not match the fixture RunKey")
    require(claim.get("claim_id") == run["claim_id"], "ClaimResponse ClaimID does not match the fixture fence")
    require(
        isinstance(claim.get("lease_token"), str) and claim["lease_token"],
        "Control ClaimResponse omitted its lease token",
    )
    work = claim.get("work")
    require(isinstance(work, dict), "Control ClaimResponse omitted Work")
    require(
        work.get("agent_profile") == profile_name and work.get("workspace") == "native",
        "ClaimResponse Work binding differs from the daemon config",
    )
    provider_case = case in PROVIDER_ACCESS_CASES
    expected_capabilities = {"provider_access": True} if provider_case else {}
    require(
        work.get("required_capabilities", {}) == expected_capabilities,
        "ClaimResponse Work required capabilities differ from the exact generic harness fixture",
    )
    work_input = work.get("input")
    require(isinstance(work_input, dict), "Control ClaimResponse Work input is invalid")
    if case in GOAL_ADMISSION_CASES:
        # The goal-routed Work is a Control-composed admission whose Subject is the exact
        # fence the App's clone reserve parses (linux_clone_journal.go:864).
        require(
            work_input.get("schema_version") == "symmetry.admission.v1",
            "goal-routed ClaimResponse Work input is not a goal admission",
        )
        require(
            work_input.get("subject")
            == {
                "resource_id": run["repository_resource_id"],
                "commit": run["commit"],
                "tree_digest": run["tree_digest"],
            },
            "goal-routed ClaimResponse admission Subject differs from the requested Subject",
        )
        require(
            "provider_scope" in work_input and work_input["provider_scope"] is None,
            "goal-routed ClaimResponse admission carries a provider scope",
        )
    else:
        require(
            work_input.get("repository_resource_id") == run["repository_resource_id"],
            "ClaimResponse Work repository differs from the requested Subject",
        )

    if provider_case:
        access = claim.get("provider_access")
        require(isinstance(access, dict), "Pi ClaimResponse must carry authentic Control provider access")
        require(access.get("path") == "/api/v1/provider-actions", "Pi ClaimResponse provider path is invalid")
        require(isinstance(access.get("token"), str) and access["token"], "Pi ClaimResponse provider token is missing")
        grants = access.get("grants")
        require(isinstance(grants, list), "Pi ClaimResponse provider grants are invalid")
        if not any(
            isinstance(grant, dict)
            and grant.get("resource_id") == run["repository_resource_id"]
            and grant.get("provider") == "github"
            and grant.get("kind") == "repository"
            and "resource.sync" in grant.get("operations", [])
            for grant in grants
        ):
            raise SystemExit("Pi ClaimResponse has no real GitHub resource.sync grant for the exact Subject")
    else:
        require("provider_access" not in claim, "provider-free ClaimResponse must omit provider_access")

    directory = pathlib.Path(barrier_dir)
    require(
        directory.is_absolute() and str(directory) == os.path.normpath(str(directory)),
        "barrier directory must be canonical",
    )
    barrier = {"directory": str(directory), "timeout_seconds": 30, "hold_after": hold_after}
    if hold_after != "none":
        barrier["release_file"] = str(directory / "release")

    request = {
        "version": 1,
        "case": request_case,
        "harness": harness,
        "run_key": {"run_id": run["run_id"], "generation": run["generation"]},
        "fence": {
            "runtime_id": run["runtime_id"],
            "runtime_epoch": run["runtime_epoch"],
            "generation": run["generation"],
            "claim_id": run["claim_id"],
            "lease_token": claim["lease_token"],
        },
        # Preserve all keys/values in the authentic Control ClaimResponse.
        "claim": claim,
        "subject": {
            "resource_id": run["repository_resource_id"],
            "commit": run["commit"],
            "tree_digest": run["tree_digest"],
        },
        "barrier": barrier,
    }
    require(
        request["subject"]["resource_id"] == runtime.get("repository_resource_id"),
        "request Subject does not bind to the exact daemon repository resource",
    )
    if drop_ack_after:
        request["drop_ack_after"] = drop_ack_after

    output = pathlib.Path(output_path)
    output.write_text(json.dumps(request, separators=(",", ":")), encoding="utf-8")
    output.chmod(0o600)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
