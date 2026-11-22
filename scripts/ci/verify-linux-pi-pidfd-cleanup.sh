#!/usr/bin/env bash
set -Eeuo pipefail
fixture_root="$RUNNER_TEMP/pi-pidfd-cleanup-fixture"
mkdir -p "$fixture_root/runner-temp" "$fixture_root/tmp"
export RUNNER_TEMP="$fixture_root/runner-temp"
export TMPDIR="$fixture_root/tmp"
python3 - ../scripts/cleanup-linux-pi-processes.sh <<'PY'
import json
import os
import pathlib
import platform
import signal
import subprocess
import sys
import tempfile
import threading

cleanup_script = sys.argv[1]
groups = []

def process_stat(pid):
    with open(f"/proc/{pid}/stat", encoding="ascii") as stream:
        _, fields = stream.read().rsplit(") ", 1)
    values = fields.split()
    return {
        "state": values[0],
        "pgrp": int(values[2]),
        "session": int(values[3]),
        "start": values[19],
    }

def launch_group(label):
    marker_dir = pathlib.Path(
        tempfile.mkdtemp(
            prefix=f"symmetry-pi-control-run-{label}-",
            dir=os.environ["TMPDIR"],
        )
    )
    group = {
        "label": label,
        "marker": marker_dir / "process-marker.json",
        "processes": [],
        "pidfds": [],
        "reaped": [],
    }
    groups.append(group)

    leader = subprocess.Popen(
        [sys.executable, "-c", "import signal; signal.pause()"],
        process_group=0,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    group["processes"].append(leader)
    group["pidfds"].append(None)
    leader_reaped = threading.Event()
    group["reaped"].append(leader_reaped)
    threading.Thread(
        target=lambda: (leader.wait(), leader_reaped.set()),
        daemon=True,
    ).start()
    group["pidfds"][-1] = os.pidfd_open(leader.pid, 0)

    child = subprocess.Popen(
        [sys.executable, "-c", "import signal; signal.pause()"],
        process_group=leader.pid,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    group["processes"].append(child)
    group["pidfds"].append(None)
    child_reaped = threading.Event()
    group["reaped"].append(child_reaped)
    threading.Thread(
        target=lambda: (child.wait(), child_reaped.set()),
        daemon=True,
    ).start()
    group["pidfds"][-1] = os.pidfd_open(child.pid, 0)

    leader_stat = process_stat(leader.pid)
    child_stat = process_stat(child.pid)
    if (
        leader_stat["state"] in ("Z", "X", "x")
        or child_stat["state"] in ("Z", "X", "x")
        or leader_stat["pgrp"] != leader.pid
        or leader_stat["session"] == leader.pid
        or child_stat["pgrp"] != leader.pid
        or child_stat["session"] != leader_stat["session"]
    ):
        raise RuntimeError("fixture processes are not a live same-session process group")

    with open("/proc/sys/kernel/random/boot_id", encoding="ascii") as stream:
        boot_id = stream.read().strip()
    namespace_link = os.readlink(f"/proc/{leader.pid}/ns/pid")
    namespace_inode = os.stat(f"/proc/{leader.pid}/ns/pid").st_ino
    if namespace_link != f"pid:[{namespace_inode}]":
        raise RuntimeError("fixture PID namespace identity did not match its proc link")
    group["identity"] = (
        f"linux:v2:{boot_id}:{namespace_inode}:{leader.pid}:{leader_stat['start']}"
    )
    return group

def write_marker(group, identity):
    group["marker"].write_text(
        json.dumps({"pid": group["processes"][0].pid, "process_identity": identity}),
        encoding="utf-8",
    )

def run_cleanup():
    result = subprocess.run(
        ["bash", cleanup_script],
        capture_output=True,
        check=False,
        text=True,
    )
    sys.stdout.write(result.stdout)
    sys.stderr.write(result.stderr)
    return result

def wait_for_stopped_group(group):
    for reaped in group["reaped"]:
        if not reaped.wait(timeout=5):
            raise RuntimeError(f"{group['label']} fixture process did not exit")

def assert_group_live(group):
    if any(process.poll() is not None for process in group["processes"]):
        raise RuntimeError(f"{group['label']} fixture process was signalled")

def signal_fixture_process(group, index, signal_number):
    process = group["processes"][index]
    pidfd = group["pidfds"][index]
    if pidfd is None:
        if process.poll() is None:
            process.send_signal(signal_number)
    else:
        try:
            signal.pidfd_send_signal(pidfd, signal_number)
        except ProcessLookupError:
            pass

def stop_fixture_group(group):
    for index in range(len(group["processes"])):
        signal_fixture_process(group, index, signal.SIGTERM)
    if not all(reaped.wait(timeout=5) for reaped in group["reaped"]):
        for index in range(len(group["processes"])):
            signal_fixture_process(group, index, signal.SIGKILL)
        if not all(reaped.wait(timeout=5) for reaped in group["reaped"]):
            raise RuntimeError(f"could not stop {group['label']} fixture processes")

try:
    if platform.machine() not in ("x86_64", "amd64"):
        raise RuntimeError(f"fixture requires x86_64, got {platform.machine()!r}")

    matching = launch_group("matching")
    write_marker(matching, matching["identity"])
    matching_result = run_cleanup()
    if matching_result.returncode != 0:
        raise RuntimeError(
            f"matching identity cleanup failed with status {matching_result.returncode}"
        )
    if f"cleanup stopped process group={matching['processes'][0].pid}" not in matching_result.stdout:
        raise RuntimeError("matching identity cleanup did not report stopped process group")
    wait_for_stopped_group(matching)
    matching["marker"].unlink()
    print("verified matching v2 identity stopped and proved leader plus same-group child")

    mismatched = launch_group("wrong-start")
    _, _, boot_id, namespace, pid, start = mismatched["identity"].split(":")
    wrong_identity = (
        f"linux:v2:{boot_id}:{namespace}:{pid}:{int(start) + 1}"
    )
    write_marker(mismatched, wrong_identity)
    mismatch_result = run_cleanup()
    if mismatch_result.returncode == 0:
        raise RuntimeError("wrong-start identity was accepted")
    if "exact process identity is not live" not in mismatch_result.stdout:
        raise RuntimeError("wrong-start identity was not refused for the identity mismatch")
    assert_group_live(mismatched)
    mismatched["marker"].unlink()
    print("verified wrong-start identity was refused without signalling either process")
finally:
    cleanup_errors = []
    for group in groups:
        try:
            group["marker"].unlink(missing_ok=True)
            stop_fixture_group(group)
        except Exception as error:
            cleanup_errors.append(f"{group['label']}: {error}")
        finally:
            for pidfd in group["pidfds"]:
                if pidfd is not None:
                    os.close(pidfd)
            for process in group["processes"]:
                if process.stdout is not None:
                    process.stdout.close()
                if process.stderr is not None:
                    process.stderr.close()
    if cleanup_errors:
        raise RuntimeError("fixture cleanup failed: " + "; ".join(cleanup_errors))
PY
