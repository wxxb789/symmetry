#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

: "${RUNNER_TEMP:?runner temporary directory is required}"
: "${GITHUB_WORKSPACE:?GitHub workspace is required}"
: "${GITHUB_RUN_ID:?workflow run ID is required}"
: "${GITHUB_RUN_ATTEMPT:?workflow attempt is required}"
: "${GITHUB_SHA:?workflow commit is required}"

output="$RUNNER_TEMP/linux-v2-host-prerequisite-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}.json"
unit="symmetry-ci-v2-prerequisite-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}.service"
peer_unit="symmetry-ci-v2-peer-prerequisite-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}.service"
peer_source="$RUNNER_TEMP/linux-v2-host-peer-probe.go"
peer_binary="$RUNNER_TEMP/cgroup-v2-delegation-probe"
unit_attempted=0
peer_unit_attempted=0
probe_passed=0
peer_probe_passed=0
cleanup_failed=0
unit_cleanup_proven=0
peer_unit_cleanup_proven=0
cross_uid_capability=not-proven
apparmor_exception=not_needed
apparmor_profile=
apparmor_parser=
systemd_pid1=0
unified_cgroup_v2=0
failure_stage=not_started
unit_log="$RUNNER_TEMP/linux-v2-host-prerequisite-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}.log"
peer_log="$RUNNER_TEMP/linux-v2-host-peer-prerequisite-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}.log"
unit_cgroup=
peer_unit_cgroup=

# systemd reports an empty ControlGroup after unit exit; the payload records its live cgroup path.
recorded_unit_cgroup() {
  sed -n 's/^cgroup_path=//p' "$1" | tail -n1
}

unit_stopped_proven() {
  local unit_name=$1 recorded_cgroup=$2
  local load_state active_state main_pid recorded_directory cgroup_dir
  load_state=$(sudo systemctl show --property=LoadState --value "$unit_name" 2>/dev/null) || return 1
  active_state=$(sudo systemctl show --property=ActiveState --value "$unit_name" 2>/dev/null) || return 1
  main_pid=$(sudo systemctl show --property=MainPID --value "$unit_name" 2>/dev/null) || return 1
  if [[ "$load_state" != not-found ]]; then
    [[ "$active_state" == inactive || "$active_state" == failed ]] || return 1
  fi
  [[ "$main_pid" == 0 ]] || return 1
  recorded_directory=${recorded_cgroup%/*}
  [[ "$recorded_cgroup" == /* && "$recorded_cgroup" != *..* && "${recorded_cgroup##*/}" == "$unit_name" ]] || return 1
  [[ "$recorded_directory" == /* && "${recorded_directory##*/}" == *.slice ]] || return 1
  cgroup_dir="/sys/fs/cgroup$recorded_cgroup"
  if [[ -d "$cgroup_dir" ]]; then
    sudo grep --fixed-strings --line-regexp --quiet 'populated 0' "$cgroup_dir/cgroup.events" || return 1
  fi
  return 0
}

write_evidence() {
  local status=$1
  python3 - "$output" "$GITHUB_SHA" "$(uname -m)" "$failure_stage" \
    "$systemd_pid1" "$unified_cgroup_v2" "$probe_passed" "$unit_cleanup_proven" \
    "$peer_probe_passed" "$peer_unit_cleanup_proven" "$cross_uid_capability" \
    "$apparmor_exception" "$status" <<'PY'
import json
import os
import sys
import tempfile

(
    path, commit, architecture, failure_stage, systemd_pid1, cgroup_v2,
    probe_passed, unit_cleanup_proven, peer_probe_passed, peer_unit_cleanup_proven,
    cross_uid_capability, apparmor_exception, exit_status,
) = sys.argv[1:]
not_evidence_of = ["production owner installation", "native Pi/OpenCode execution", "WSL2 positive support"]
if apparmor_exception == "per_program":
    not_evidence_of.append("production launcher/gate under the AppArmor unprivileged-userns restriction")
evidence = {
    "version": 1,
    "case": "linux_v2_host_prerequisite_probe",
    "source_commit": commit,
    "architecture": architecture,
    "scope": "systemd, delegated cgroup-v2 leaf, and credential-free cross-UID userns/fdinfo/pidfd_getfd host prerequisites only",
    "not_evidence_of": not_evidence_of,
    "systemd_is_pid1": systemd_pid1 == "1",
    "unified_cgroup_v2_mounted": cgroup_v2 == "1",
    "delegated_leaf_probe_passed": probe_passed == "1",
    "transient_unit_stop_proven": unit_cleanup_proven == "1",
    "cross_uid_peer_probe_passed": peer_probe_passed == "1",
    "cross_uid_capability": cross_uid_capability,
    "apparmor_userns_exception": apparmor_exception,
    "host_a_pidfd_getfd": "passed" if peer_probe_passed == "1" else "not-proven",
    "peer_transient_unit_stop_proven": peer_unit_cleanup_proven == "1",
    "status": "passed" if exit_status == "0" else "failed",
    "failure_stage": "" if exit_status == "0" else failure_stage,
}
directory = os.path.dirname(path)
os.makedirs(directory, mode=0o700, exist_ok=True)
fd, temporary = tempfile.mkstemp(prefix=".linux-v2-host-prerequisite-", dir=directory)
try:
    os.fchmod(fd, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        json.dump(evidence, stream, separators=(",", ":"), sort_keys=True)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)
except BaseException:
    try:
        os.unlink(temporary)
    except FileNotFoundError:
        pass
    raise
PY
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  set +e
  local target_unit target_cgroup
  for target_unit in "$unit" "$peer_unit"; do
    if [[ "$target_unit" == "$unit" ]]; then
      target_cgroup=$unit_cgroup
    else
      target_cgroup=$peer_unit_cgroup
    fi
    if [[ "$target_unit" == "$unit" && $unit_attempted -eq 0 ]] ||
       [[ "$target_unit" == "$peer_unit" && $peer_unit_attempted -eq 0 ]]; then
      continue
    fi
    if ! unit_stopped_proven "$target_unit" "$target_cgroup"; then
      sudo systemctl --no-block stop "$target_unit" >/dev/null 2>&1
      for _ in {1..30}; do
        unit_stopped_proven "$target_unit" "$target_cgroup" && break
        sleep 1
      done
      if ! unit_stopped_proven "$target_unit" "$target_cgroup"; then
        sudo systemctl kill --signal=SIGKILL --kill-whom=all "$target_unit" >/dev/null 2>&1
        for _ in {1..30}; do
          unit_stopped_proven "$target_unit" "$target_cgroup" && break
          sleep 1
        done
      fi
    fi
    if unit_stopped_proven "$target_unit" "$target_cgroup"; then
      if [[ "$target_unit" == "$unit" ]]; then
        unit_cleanup_proven=1
      else
        peer_unit_cleanup_proven=1
      fi
      sudo systemctl reset-failed "$target_unit" >/dev/null 2>&1 || true
    else
      cleanup_failed=1
      failure_stage=transient_unit_cleanup
      printf 'Credential-free prerequisite unit stop could not be proven; unit=%s\n' "$target_unit" >&2
    fi
  done
  if [[ -n "$apparmor_profile" ]]; then
    if ! sudo -n "$apparmor_parser" -R "$apparmor_profile" >/dev/null 2>&1; then
      cleanup_failed=1
      printf 'AppArmor userns exception unload failed; profile=%s\n' "$apparmor_profile" >&2
    fi
    rm -f -- "$apparmor_profile"
  fi
  if (( cleanup_failed )) && (( status == 0 )); then
    status=1
  fi
  write_evidence "$status" || status=1
  rm -f -- "$unit_log" "$peer_log" "$peer_source" "$peer_binary"
  exit "$status"
}
trap cleanup EXIT INT TERM

failure_stage=host_systemd_cgroup_v2_precondition
[[ "$(uname -m)" == x86_64 ]] || exit 1
[[ "$(ps -p 1 -o comm= | tr -d '[:space:]')" == systemd ]] || exit 1
systemd_pid1=1
# The root cgroup may lack cgroup.events (cgroup-v2.rst); cgroup2fs mount type plus cgroup.controllers prove unification.
[[ -d /run/systemd/system && -f /sys/fs/cgroup/cgroup.controllers ]] || exit 1
[[ "$(stat -fc %T /sys/fs/cgroup)" == cgroup2fs ]] || exit 1
unified_cgroup_v2=1
command -v systemd-run >/dev/null || exit 1
command -v systemctl >/dev/null || exit 1
command -v python3 >/dev/null || exit 1
command -v go >/dev/null || exit 1
sudo -n true

failure_stage=unprivileged_delegated_leaf_and_cgroup_kill
probe_code=$(cat <<'PY'
import os
import pathlib
import signal
import subprocess
import time

def require(condition, stage):
    if not condition:
        raise SystemExit("prerequisite probe failed: " + stage)

entries = [line.rstrip("\n").split(":", 2)[2] for line in open("/proc/self/cgroup", encoding="ascii") if line.startswith("0::")]
# Recorded before the first strict check, while the unit runs; systemd reports an empty ControlGroup after exit.
if len(entries) == 1:
    print("cgroup_path=" + entries[0], flush=True)
require(os.geteuid() == 65534 and os.getegid() == 65534, "fixed unprivileged service identity")
require(len(entries) == 1, "unified cgroup-v2 membership")
parent = pathlib.Path("/sys/fs/cgroup") / entries[0].lstrip("/")
require(parent.is_dir() and os.access(parent / "cgroup.procs", os.W_OK), "delegated parent cgroup access")
require((parent / "cgroup.kill").is_file(), "cgroup.kill availability")
leaf = parent / ("symmetry-ci-child-" + str(os.getpid()))
leaf.mkdir(mode=0o700)
child = None
try:
    child = subprocess.Popen(["/bin/sleep", "60"], close_fds=True, start_new_session=True)
    with open(leaf / "cgroup.procs", "w", encoding="ascii") as stream:
        stream.write(str(child.pid))
    expected = entries[0].rstrip("/") + "/" + leaf.name
    observed = [line.rstrip("\n").split(":", 2)[2] for line in open(f"/proc/{child.pid}/cgroup", encoding="ascii") if line.startswith("0::")]
    require(observed == [expected], "exact child migration into delegated leaf")
    with open(leaf / "cgroup.kill", "w", encoding="ascii") as stream:
        stream.write("1\n")
    result = child.wait(timeout=10)
    require(result == -signal.SIGKILL, "cgroup.kill physically stopped the exact child")
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        try:
            populated = next(line.split()[1] for line in (leaf / "cgroup.events").read_text(encoding="ascii").splitlines() if line.startswith("populated "))
        except (OSError, StopIteration):
            populated = "unknown"
        if populated == "0":
            break
        time.sleep(0.05)
    require(populated == "0", "delegated leaf returned to populated=0")
finally:
    if child is not None and child.poll() is None:
        child.kill()
        child.wait(timeout=5)
    try:
        leaf.rmdir()
    except FileNotFoundError:
        pass
PY
)

unit_attempted=1
if timeout 90s sudo systemd-run --system --quiet --wait --pipe --unit="$unit" \
  --property=Delegate=yes --property=KillMode=mixed --property=TimeoutStopSec=30s \
  --uid=65534 --gid=65534 --setenv=PATH=/usr/bin:/bin \
  -- /usr/bin/python3 -c "$probe_code" >"$unit_log" 2>&1; then
  unit_cgroup=$(recorded_unit_cgroup "$unit_log") || unit_cgroup=
  if unit_stopped_proven "$unit" "$unit_cgroup"; then
    probe_passed=1
    unit_cleanup_proven=1
  else
    failure_stage=transient_unit_cleanup
    exit 1
  fi
else
  unit_cgroup=$(recorded_unit_cgroup "$unit_log") || unit_cgroup=
  failure_stage=unprivileged_delegated_leaf_and_cgroup_kill
  exit 1
fi

failure_stage=build_cross_uid_userns_fdinfo_pidfd_getfd_probe
install -m 0600 "$GITHUB_WORKSPACE/scripts/ci/linux-v2-host-peer-probe.go" "$peer_source"
(
  cd "$GITHUB_WORKSPACE/daemon"
  env -u GH_TOKEN -u GITHUB_TOKEN -u SYMMETRY_OPENAI_API_KEY \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS=-mod=readonly \
      go build -trimpath -o "$peer_binary" "$peer_source"
)
chmod 0555 "$peer_binary"

restriction=0
if [[ -r /proc/sys/kernel/apparmor_restrict_unprivileged_userns ]]; then
  read -r restriction < /proc/sys/kernel/apparmor_restrict_unprivileged_userns
fi
if [[ "$restriction" == 1 ]]; then
  failure_stage=apparmor_userns_exception
  apparmor_exception=required
  apparmor_parser=$(command -v apparmor_parser) || {
    printf 'AppArmor unprivileged user-namespace restriction is active but apparmor_parser is unavailable.\n' >&2
    exit 1
  }
  apparmor_profile="$RUNNER_TEMP/symmetry-ci-v2-host-probe-userns-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}.profile"
  cat > "$apparmor_profile" <<EOF
abi <abi/4.0>,
include <tunables/global>
profile symmetry-ci-v2-host-probe-userns-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT} "$peer_binary" flags=(unconfined) {
  userns,
}
EOF
  # This per-program userns exception is not a sandbox and never touches the global restriction.
  sudo -n "$apparmor_parser" --replace "$apparmor_profile"
  apparmor_exception=per_program
fi
printf 'apparmor_userns_exception=%s\n' "$apparmor_exception"

failure_stage=cross_uid_userns_fdinfo_pidfd_getfd
peer_unit_attempted=1
if timeout --signal=TERM --kill-after=5s 120s sudo -n systemd-run \
  --quiet --system --wait --pipe --unit="$peer_unit" \
  --property=Delegate=yes --property=KillMode=mixed --property=TimeoutStopSec=30s \
  --property=RuntimeMaxSec=90s --property="User=$(id -un)" \
  --property="WorkingDirectory=$GITHUB_WORKSPACE/daemon" --setenv=PATH=/usr/bin:/bin \
  "$peer_binary" >"$peer_log" 2>&1; then
  peer_unit_cgroup=$(recorded_unit_cgroup "$peer_log") || peer_unit_cgroup=
  if ! unit_stopped_proven "$peer_unit" "$peer_unit_cgroup"; then
    cat "$peer_log" >&2 || true
    failure_stage=peer_probe_unit_cleanup
    exit 1
  fi
  peer_unit_cleanup_proven=1
else
  cat "$peer_log" >&2 || true
  peer_unit_cgroup=$(recorded_unit_cgroup "$peer_log") || peer_unit_cgroup=
  failure_stage=cross_uid_userns_fdinfo_pidfd_getfd
  exit 1
fi
if ! python3 - "$peer_log" <<'PY'
import pathlib
import sys

output = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8", errors="replace")
required = (
    "cross_uid_capability=fixed-b-child-userns-fdinfo-pidfd-getfd",
    "host_a_pidfd_getfd=passed",
    "namespace-proof=passed",
    "fdinfo_inode=",
    "tcp_state=ESTABLISHED",
    "netns_cookie=",
    "pidfd_getfd=passed",
    "host_cap_eff=0000000000000000",
    "status=passed",
)
missing = [marker for marker in required if marker not in output]
if missing:
    raise SystemExit("cross-UID peer probe omitted required proof: " + ", ".join(missing))
PY
then
  cat "$peer_log" >&2 || true
  failure_stage=cross_uid_userns_fdinfo_pidfd_getfd_output
  exit 1
fi
peer_probe_passed=1
cross_uid_capability=fixed-b-child-userns-fdinfo-pidfd-getfd
failure_stage=complete

printf 'Credential-free systemd/cgroup-v2 and cross-UID userns/fdinfo/pidfd_getfd prerequisites passed; this is not production A/B or native Pi/OpenCode support evidence.\n'
