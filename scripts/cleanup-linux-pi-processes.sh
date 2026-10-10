#!/usr/bin/env bash
set -uo pipefail
log="$RUNNER_TEMP/pi-control-e2e-linux.log"
if ! mkdir -p "$TMPDIR" || ! : >> "$log"; then
  printf 'cleanup could not initialize TMPDIR or log\n' >&2
  exit 1
fi
if ! cleanup_process_group=$(python3 -c 'import os; print(os.getpgrp())') || ! [[ "$cleanup_process_group" =~ ^[0-9]+$ ]]; then
  printf 'cleanup could not determine its own process group\n' | tee -a "$log"
  exit 1
fi
cleanup_failed=0

for marker in "$TMPDIR"/symmetry-pi-control-run-*/process-marker.json; do
  [ -f "$marker" ] || continue
  printf 'cleanup marker=%s\n' "$marker" | tee -a "$log"

  if ! marker_contents=$(<"$marker"); then
    printf 'cleanup failed to read marker=%s\n' "$marker" | tee -a "$log"
    cleanup_failed=1
    continue
  fi
  marker_state=$(printf '%s' "$marker_contents" | tr -d '[:space:]')
  case "$marker_state" in
    not_started)
      printf 'cleanup marker=%s has no native process\n' "$marker" | tee -a "$log"
      continue
      ;;
    starting)
      printf 'cleanup cannot prove native process state from marker=%s\n' "$marker" | tee -a "$log"
      cleanup_failed=1
      continue
      ;;
  esac

  marker_data=$(python3 - "$marker" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
try:
    value = json.loads(path.read_text())
except Exception as exc:
    print(f"error={exc}")
    raise SystemExit(2)
pid = value.get("pid")
identity = value.get("process_identity")
if isinstance(pid, bool) or not isinstance(pid, int) or pid <= 0 or not isinstance(identity, str) or not identity:
    print("error=marker must contain a positive integer pid and non-empty process_identity")
    raise SystemExit(2)
print(f"{pid}\t{identity}")
PY
  )
  if [ $? -ne 0 ]; then
    printf 'cleanup failed to parse marker=%s\n' "$marker" | tee -a "$log"
    cleanup_failed=1
    continue
  fi

  IFS=$'\t' read -r pid identity <<< "$marker_data"
  if identity_fields=$(python3 - "$identity" "$pid" <<'PY'
import re
import sys

identity, marker_pid = sys.argv[1:]
parts = identity.split(":")
boot_pattern = r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"
if len(parts) != 6 or parts[:2] != ["linux", "v2"] or re.fullmatch(boot_pattern, parts[2]) is None:
    raise SystemExit(2)

try:
    numeric = [int(value) for value in parts[3:]]
except ValueError:
    raise SystemExit(2)
if any(re.fullmatch(r"[1-9][0-9]*", value) is None or number > 18446744073709551615 for value, number in zip(parts[3:], numeric)):
    raise SystemExit(2)
if numeric[1] > sys.maxsize or str(numeric[1]) != marker_pid or str(int(marker_pid)) != marker_pid:
    raise SystemExit(2)
print("\t".join(parts[2:]))
PY
  ); then
    IFS=$'\t' read -r identity_boot identity_namespace identity_pid identity_start <<< "$identity_fields"
  else
    printf 'cleanup refused marker=%s identity=%s\n' "$marker" "$identity" | tee -a "$log"
    cleanup_failed=1
    continue
  fi

  if [ "$pid" = "$cleanup_process_group" ]; then
    printf 'cleanup refused process group=%s because it owns the cleanup shell\n' "$pid" | tee -a "$log"
    cleanup_failed=1
    continue
  fi

  python3 - "$pid" "$identity_boot" "$identity_namespace" "$identity_start" <<'PY' 2>&1 | tee -a "$log"
import ctypes
import errno
import os
import platform
import re
import signal
import sys
import time

# os.pidfd_open wraps SYS_PIDFD_OPEN=434. The signal syscall and flag
# match daemon's pinned golang.org/x/sys v0.48.0
# unix/zsysnum_linux_amd64.go and unix/ztypes_linux.go.
SYS_PIDFD_SEND_SIGNAL = 424
PIDFD_SIGNAL_PROCESS_GROUP = 0x4
GROUP_EXIT_TIMEOUT_SECONDS = 5.0
GROUP_EXIT_POLL_SECONDS = 0.05

pid_text, expected_boot, expected_namespace, expected_start = sys.argv[1:]
pid = int(pid_text)
libc = ctypes.CDLL(None, use_errno=True)
libc.syscall.restype = ctypes.c_long

def pidfd_send_group_signal(pidfd, signal_number):
    ctypes.set_errno(0)
    result = libc.syscall(
        ctypes.c_long(SYS_PIDFD_SEND_SIGNAL),
        ctypes.c_int(pidfd),
        ctypes.c_int(signal_number),
        ctypes.c_void_p(),
        ctypes.c_uint(PIDFD_SIGNAL_PROCESS_GROUP),
    )
    return None if result == 0 else ctypes.get_errno()

def read_process_stat():
    with open(f"/proc/{pid}/stat", encoding="ascii") as stream:
        contents = stream.read()
    prefix, fields = contents.rsplit(") ", 1)
    process_pid = prefix.split(" ", 1)[0]
    values = fields.split()
    if len(values) < 20:
        raise ValueError("/proc stat has too few fields")
    return process_pid, values[0], values[2], values[3], values[19]

def read_pid_namespace(path):
    link = os.readlink(path)
    match = re.fullmatch(r"pid:\[([1-9][0-9]*)\]", link)
    if match is None:
        raise ValueError(f"non-canonical PID namespace link: {link!r}")
    link_inode = int(match.group(1))
    stat_inode = os.stat(path).st_ino
    if stat_inode != link_inode:
        raise ValueError(f"PID namespace link/stat inode mismatch for {path}")
    return link, str(stat_inode)

def verify_live_identity():
    leader_pid, leader_state, leader_group, leader_session, leader_start = read_process_stat()
    if (
        leader_pid != pid_text
        or leader_state in ("Z", "X", "x")
        or not leader_group.isdecimal()
        or leader_group != pid_text
        or not leader_session.isdecimal()
        or leader_session == pid_text
        or leader_start != expected_start
    ):
        raise ValueError("leader PID, state, process group, session, or start time changed")
    with open("/proc/sys/kernel/random/boot_id", encoding="ascii") as stream:
        actual_boot = stream.read().strip()
    if actual_boot != expected_boot:
        raise ValueError("boot ID changed")
    target_link, target_inode = read_pid_namespace(f"/proc/{pid}/ns/pid")
    self_link, self_inode = read_pid_namespace("/proc/self/ns/pid")
    if (
        target_link != self_link
        or target_inode != expected_namespace
        or self_inode != expected_namespace
    ):
        raise ValueError("PID namespace changed or does not match the cleanup process")

def require_live_identity(stage):
    try:
        verify_live_identity()
    except Exception as error:
        raise RuntimeError(
            f"cleanup refused pid={pid_text} at {stage}; exact process identity is not live: {error}"
        ) from error

def group_has_exited(pidfd):
    error_number = pidfd_send_group_signal(pidfd, 0)
    if error_number == errno.ESRCH:
        return True
    if error_number is not None:
        raise OSError(error_number, os.strerror(error_number))
    return False

def wait_for_group_exit(pidfd):
    deadline = time.monotonic() + GROUP_EXIT_TIMEOUT_SECONDS
    while True:
        if group_has_exited(pidfd):
            return True
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return False
        time.sleep(min(GROUP_EXIT_POLL_SECONDS, remaining))

def fail(message):
    print(message)
    raise SystemExit(2)

if platform.machine() not in ("x86_64", "amd64"):
    fail(f"cleanup requires the x86_64 pidfd syscall ABI, got {platform.machine()!r}")

try:
    require_live_identity("before pidfd_open")
    pidfd = os.pidfd_open(pid, 0)
except Exception as error:
    fail(f"cleanup could not open an identity-bound pidfd for pid={pid_text}: {error}")

try:
    try:
        require_live_identity("after pidfd_open")
    except Exception as error:
        fail(
            f"cleanup refused to use pidfd for pid={pid_text} because exact process identity changed: {error}"
        )
    # Signal 0 is side-effect-free and also verifies runner support.
    capability_error = pidfd_send_group_signal(pidfd, 0)
    if capability_error is not None:
        if capability_error == errno.ESRCH:
            fail(
                f"cleanup could not prove process group={pid_text} before TERM; pidfd group probe found no target"
            )
        fail(
            f"cleanup requires pidfd_send_signal(PIDFD_SIGNAL_PROCESS_GROUP) support (Linux 6.9+ and permitted by runner policy): {os.strerror(capability_error)}"
        )

    try:
        require_live_identity("before SIGTERM")
    except Exception as error:
        fail(
            f"cleanup refused to signal process group={pid_text} because exact process identity changed: {error}"
        )
    term_error = pidfd_send_group_signal(pidfd, signal.SIGTERM)
    if term_error is not None and term_error != errno.ESRCH:
        fail(f"cleanup could not send pidfd-bound SIGTERM to process group={pid_text}: {os.strerror(term_error)}")

    try:
        if wait_for_group_exit(pidfd):
            print(f"cleanup stopped process group={pid_text}")
            raise SystemExit(0)
    except Exception as error:
        fail(f"cleanup could not prove process group={pid_text} after TERM: {error}")

    print(f"cleanup escalating process group={pid_text} after TERM timeout")
    try:
        require_live_identity("before SIGKILL")
    except Exception as error:
        fail(
            f"cleanup refused to escalate process group={pid_text} because the original leader exited or exact identity changed: {error}"
        )
    kill_error = pidfd_send_group_signal(pidfd, signal.SIGKILL)
    if kill_error is not None and kill_error != errno.ESRCH:
        fail(f"cleanup could not send pidfd-bound SIGKILL to process group={pid_text}: {os.strerror(kill_error)}")
    try:
        if wait_for_group_exit(pidfd):
            print(f"cleanup stopped process group={pid_text}")
            raise SystemExit(0)
    except Exception as error:
        fail(f"cleanup could not prove process group={pid_text} after SIGKILL: {error}")
    fail(f"cleanup left live process-group members for group={pid_text} after pidfd-bound SIGKILL")
finally:
    os.close(pidfd)
PY
  if [ "$?" -ne 0 ]; then
    cleanup_failed=1
  fi
done

exit "$cleanup_failed"
