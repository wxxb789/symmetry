#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

fail() {
  printf 'Linux v2 native acceptance setup failed: %s\n' "$1" >&2
  exit 1
}

: "${GITHUB_WORKSPACE:?GitHub workspace is required}"
: "${RUNNER_TEMP:?Runner temp directory is required}"
: "${GITHUB_EVENT_NAME:?GitHub event name is required}"
: "${GITHUB_REF:?GitHub source ref is required}"
: "${GITHUB_SHA:?GitHub source commit is required}"
: "${DATABASE_URL:?Control database URL is required}"
: "${SYMMETRY_ENROLLMENT_TOKEN:?Control machine enrollment token is required}"
: "${SYMMETRY_OPERATOR_TOKEN:?an isolated Control operator token is required}"
: "${GH_TOKEN:?a secret-backed GitHub OAuth token is required by Control}"
: "${SYMMETRY_OPENAI_API_KEY:?B-only native OpenAI credentials are required}"
: "${SYMMETRY_OPENAI_BASE_URL:?the OpenAI-compatible provider base URL must be configured}"
: "${SYMMETRY_PI_MODEL:?the pinned Pi model must be configured}"
: "${SYMMETRY_OPENCODE_MODEL:?the pinned OpenCode model must be configured}"
: "${GITHUB_REPOSITORY:?the source repository identity is required}"
: "${GITHUB_REPOSITORY_OWNER:?the source repository owner is required}"

repo=$GITHUB_WORKSPACE
source_subject_json=$(python3 "$repo/scripts/ci/linux-v2-source-subject.py" "$repo" "$GITHUB_SHA")
source_commit=$(jq --raw-output .commit <<<"$source_subject_json")
source_tree_digest=$(jq --raw-output .tree_digest <<<"$source_subject_json")

fixture_case_set=${SYMMETRY_LINUX_V2_NATIVE_CASE_SET:-}
case "$fixture_case_set" in
  shared|codex_containment|claude_containment|stage_replay_create_started|stage_replay_leaf_bound|stage_replay_launcher_bound|ack_loss_launcher_bound|stale_leaf|migration|alternate_mount_refusal|stale_journal_run_key|stale_journal_run_key_legacy|stale_journal_fence|stale_journal_subject|synthetic_goal_cleanup) ;;
  *) fail 'set SYMMETRY_LINUX_V2_NATIVE_CASE_SET to one exact shared or unresolved witness case set' ;;
esac
fixture_identity_suffix=${fixture_case_set//_/-}
native_containment_kind=''
case "$fixture_case_set" in
  codex_containment) native_containment_kind=codex ;;
  claude_containment) native_containment_kind=claude_code ;;
esac

evidence_directory="${SYMMETRY_LINUX_V2_NATIVE_EVIDENCE_DIR:-$RUNNER_TEMP/linux-v2-native-evidence-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}-$fixture_case_set}"
[[ "$evidence_directory" == /* && "$(realpath -m "$evidence_directory")" == "$evidence_directory" ]] ||
  fail 'sanitized evidence directory must be canonical and absolute'
mkdir -m 0700 -p "$evidence_directory"
[[ ! -L "$evidence_directory" && "$(stat -c '%u:%g:%a' "$evidence_directory")" == "$(id -u):$(id -g):700" ]] ||
  fail 'sanitized evidence directory must be private to the runner'
shopt -s nullglob
existing_evidence=("$evidence_directory"/*)
shopt -u nullglob
(( ${#existing_evidence[@]} == 0 )) || fail 'sanitized evidence directory must be empty before the fixture runs'

[[ "$GITHUB_EVENT_NAME" == workflow_dispatch ]] || fail 'credentialed Linux acceptance requires explicit workflow_dispatch'
[[ "$GITHUB_SHA" =~ ^([0-9a-f]{40}|[0-9a-f]{64})$ ]] || fail 'workflow_dispatch did not select a full Git commit SHA'
printf 'Credentialed manual acceptance selected %s at %s.\n' "$GITHUB_REF" "$GITHUB_SHA"
[[ "$GH_TOKEN" == gho_* ]] || fail 'Control requires a gho_-class GitHub OAuth token; the hosted GITHUB_TOKEN is not an accepted provider credential'
[[ "$SYMMETRY_PI_MODEL" =~ ^[A-Za-z0-9._-]+$ ]] || fail 'the Pi model name is not a supported token'
[[ "$SYMMETRY_OPENCODE_MODEL" =~ ^[A-Za-z0-9._-]+$ ]] || fail 'the OpenCode model name is not a supported token'
[[ "$SYMMETRY_OPENAI_BASE_URL" =~ ^https://[A-Za-z0-9.-]+(:[0-9]{1,5})?(/[A-Za-z0-9._/-]*)?$ ]] || fail 'the OpenAI-compatible base URL is not a supported HTTPS URL'
[[ "$(uname -m)" == x86_64 ]] || fail 'the production Linux owner runtime is amd64 only'
[[ "$(ps -p 1 -o comm= | tr -d '[:space:]')" == systemd ]] || fail 'the hosted Linux runner must use systemd as PID 1'
[[ -d /run/systemd/system && -f /sys/fs/cgroup/cgroup.controllers ]] || fail 'the hosted runner must provide systemd and unified cgroup v2'
command -v systemd-run >/dev/null || fail 'systemd-run is required for isolated transient units'
command -v systemd-nspawn >/dev/null || fail 'systemd-nspawn is required for an isolated throwaway systemd manager'
command -v setfacl >/dev/null || fail 'ACL tools are required to provision the exact A/B filesystem boundary'
command -v gh >/dev/null || fail 'Control GitHub provider actions require the gh CLI on the host Control process'
# Reading another process's namespace link needs ptrace-read access, so PID 1's
# inode is unreadable to the unprivileged runner user without sudo.
host_pid_namespace_inode=$(sudo stat -Lc '%i' /proc/1/ns/pid)
[[ "$host_pid_namespace_inode" =~ ^[1-9][0-9]*$ ]] || fail 'cannot identify the host PID namespace inode'

[[ -f "$repo/daemon/go.mod" && -f "$repo/scripts/install-linux-owner.sh" ]] || fail 'run this fixture from the checked-out repository'
control_bin="$repo/control/_build/prod/rel/symmetry_control/bin/symmetry_control"
[[ -x "$control_bin" ]] || fail 'the production Control release must be built before this fixture'
env -u GH_TOKEN -u GITHUB_TOKEN -u SYMMETRY_OPENAI_API_KEY \
  "$control_bin" eval 'SymmetryControl.Release.migrate()' >/dev/null

fixture_root=$(mktemp -d "$RUNNER_TEMP/symmetry-linux-v2-acceptance-$fixture_case_set.XXXXXX")
chmod 0700 "$fixture_root"
native_api_key_file="$fixture_root/native-api-key"
control_github_token_file="$fixture_root/control-github-token"
control_gh_config_dir="$fixture_root/control-gh-config"
rootfs="$fixture_root/rootfs"
build_stage="$fixture_root/build"
control_log="$fixture_root/control.log"
control_pid=
control_started=0
machine=
systemd_unit=
guest_unit_attempted=0
guest_control_group=
guest_cgroup_identity=
machine_run_count=0
evidence_secret_values_file="$fixture_root/evidence-secret-values"
printf '%s\0' "$GH_TOKEN" "$SYMMETRY_OPENAI_API_KEY" \
  "$SYMMETRY_OPERATOR_TOKEN" "$SYMMETRY_ENROLLMENT_TOKEN" "${GITHUB_TOKEN:-}" \
  'development-operator-token' 'development-enrollment-token' > "$evidence_secret_values_file"
chmod 0600 "$evidence_secret_values_file"

guest_stopped_proven() {
  [[ -n "$systemd_unit" ]] || return 1
  [[ -n "$guest_control_group" && -n "$guest_cgroup_identity" ]] || return 1
  local active_state main_pid cgroup_dir current_identity
  active_state=$(sudo systemctl show --property=ActiveState --value "$systemd_unit" 2>/dev/null) || return 1
  main_pid=$(sudo systemctl show --property=MainPID --value "$systemd_unit" 2>/dev/null) || return 1
  [[ "$active_state" == inactive || "$active_state" == failed ]] || return 1
  [[ "$main_pid" == 0 ]] || return 1
  cgroup_dir="/sys/fs/cgroup$guest_control_group"
  if sudo test -d "$cgroup_dir"; then
    current_identity=$(sudo stat --dereference --format='%d:%i' "$cgroup_dir" 2>/dev/null) || return 1
    [[ "$current_identity" == "$guest_cgroup_identity" ]] || return 1
    sudo grep --fixed-strings --line-regexp --quiet 'populated 0' "$cgroup_dir/cgroup.events" || return 1
  fi
  return 0
}

pin_guest_cgroup_while_active() {
  local active_state main_pid control_group cgroup_dir identity
  for _ in {1..300}; do
    active_state=$(sudo systemctl show --property=ActiveState --value "$systemd_unit" 2>/dev/null) || return 1
    main_pid=$(sudo systemctl show --property=MainPID --value "$systemd_unit" 2>/dev/null) || return 1
    control_group=$(sudo systemctl show --property=ControlGroup --value "$systemd_unit" 2>/dev/null) || return 1
    if [[ "$active_state" == active && "$main_pid" =~ ^[1-9][0-9]*$ &&
          "$control_group" == /* && "$control_group" != / ]]; then
      cgroup_dir="/sys/fs/cgroup$control_group"
      if sudo test -d "$cgroup_dir"; then
        identity=$(sudo stat --dereference --format='%d:%i' "$cgroup_dir" 2>/dev/null) || return 1
        [[ "$identity" =~ ^[0-9]+:[0-9]+$ ]] || return 1
        guest_control_group=$control_group
        guest_cgroup_identity=$identity
        return 0
      fi
    fi
    sleep 0.1
  done
  return 1
}

wait_for_guest_stop() {
  local seconds=$1
  for ((second = 0; second < seconds; second++)); do
    guest_stopped_proven && return 0
    sleep 1
  done
  guest_stopped_proven
}

stage_guest_binary() {
  local source=$1 guest=$2 expected_sha=$3 label=$4 release=$5
  sudo install -o root -g root -m 0755 "$source" "$rootfs$guest"
  [[ "$(sudo sha256sum "$rootfs$guest" | cut -d' ' -f1)" == "$expected_sha" ]] ||
    fail "staged $label digest differs from the pinned $release release"
}

# Defined before the EXIT trap's exporter: an early fixture failure must still export cleanly.
print_witness_document() {
  local document=$1
  sudo test -s "$document" || return 0
  sudo jq -c '{case, harness, native_version, owner_witness_outcome, failure_category, recovery_state,
    installed_owner_verified, native_binary, native_open_reached: .containment.native_open_reached}' \
    "$document" >&2 || true
}

# Defined before its earliest caller: the shared set's unprovisioned gate failure prints through it.
print_witness_output_lines() {
  local event_file=$1
  shift
  python3 - "$event_file" "$evidence_secret_values_file" "$@" >&2 <<'PY' || true
import json, pathlib, sys

events, secrets_path, *names = sys.argv[1:]
try:
    secrets = [value for value in pathlib.Path(secrets_path).read_bytes().split(b"\0") if value]
except OSError:
    secrets = []
lines = []
try:
    with open(events, encoding="utf-8", errors="strict") as stream:
        for line in stream:
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue
            if event.get("Action") != "output":
                continue
            name = event.get("Test")
            if not isinstance(name, str) or not any(name == target or name.startswith(target + "/") for target in names):
                continue
            text = event.get("Output") or ""
            raw = text.encode("utf-8", errors="replace")
            if len(raw) > 400 or any(secret in raw for secret in secrets):
                continue
            lines.append(text.rstrip())
except OSError:
    raise SystemExit(0)
for text in lines[-10:]:
    print("witness output: " + text)
PY
}

# One redaction stage for both line filters: `drop` skips over-bound and secret-bearing lines and
# keeps the newest; `truncate` rewrites secrets and clips, so a stalled witness's only cause survives.
redact_evidence_lines() {
  local mode=$1 bound=$2 keep=$3
  python3 -c '
import pathlib, sys

mode, bound, keep, secrets_path = sys.argv[1:5]
bound = int(bound)
keep = int(keep)
try:
    secrets = [value for value in pathlib.Path(secrets_path).read_bytes().split(b"\0") if value]
except OSError:
    secrets = []
lines = []
for line in sys.stdin.buffer:
    if mode == "drop":
        if len(line.rstrip(b"\r\n")) > bound or any(secret in line for secret in secrets):
            continue
        lines.append(line)
    else:
        for secret in secrets:
            line = line.replace(secret, b"<redacted>")
        sys.stdout.buffer.write(line[:bound].rstrip(b"\r\n") + b"\n")
if mode == "drop":
    for line in lines[-keep:]:
        sys.stdout.buffer.write(line)
' "$mode" "$bound" "$keep" "$evidence_secret_values_file"
}

# Guest daemon output is echoed only through the same secret-values filter and 400-byte bound
# as the witness event lines; each caller names the block the lines belong to.
print_filtered_output_lines() {
  redact_evidence_lines drop 400 40 "$evidence_secret_values_file" >&2 || true
}

# A stalled witness prints nothing, and its one error line can exceed the shared
# length bound, so truncate and redact instead of dropping the only named cause.
print_witness_stderr_tail() {
  local path=$1
  sudo test -s "$path" || return 0
  printf 'owner-witness command stderr (bounded last 20 lines):\n' >&2
  sudo tail --lines=20 -- "$path" |
    redact_evidence_lines truncate 400 0 "$evidence_secret_values_file" >&2 || true
}

export_native_evidence() {
  local original_status=$1
  local witness_home=${A_HOME:-/home/symmetry-a}
  local witness_root="$rootfs$witness_home/ci-fixture/witness-output"
  local receipt_root="$fixture_root"
  local run_status=failed
  if (( original_status == 0 )); then
    python3 "$repo/scripts/ci/linux-v2-source-subject.py" "$repo" "$GITHUB_SHA" >/dev/null || return 1
    run_status=passed
  fi
  local native_kind='' native_binary='' native_binary_sha='' native_archive_sha='' native_version_line=''
  local native_witness_document=''
  case "$fixture_case_set" in
    codex_containment)
      native_kind=codex
      native_binary=/usr/local/bin/codex
      native_witness_document="$witness_root/codex_containment/TestProductionLinuxV2CodexNativeContainment.json"
      native_binary_sha="sha256:${codex_executable_sha:-}"
      native_archive_sha="sha256:${codex_archive_sha:-}"
      native_version_line="${codex_version_line:-}"
      ;;
    claude_containment)
      native_kind=claude_code
      native_binary=/usr/local/bin/claude
      native_witness_document="$witness_root/claude_containment/TestProductionLinuxV2ClaudeNativeContainment.json"
      native_binary_sha="sha256:${claude_executable_sha:-}"
      native_archive_sha="sha256:${claude_archive_sha:-}"
      native_version_line="${claude_version_line:-}"
      ;;
    *)
      native_kind=pi
      native_binary=/usr/local/lib/symmetry-pi/pi
      native_binary_sha="sha256:${pi_executable_sha:-}"
      native_archive_sha="sha256:${pi_archive_sha:-}"
      native_version_line="${pi_version_line:-}"
      ;;
  esac
  local -a native_manifest_args=()
  if [[ -n "$native_kind" ]]; then
    native_manifest_args=(
      --native-kind "$native_kind"
      --native-binary "$native_binary"
      --native-binary-sha256 "$native_binary_sha"
      --native-archive-sha256 "$native_archive_sha"
      --native-version-line "$native_version_line"
      --source-tree-digest "$source_tree_digest"
    )
  fi
  local export_status=0
  sudo python3 "$repo/scripts/ci/export-linux-v2-redacted-evidence.py" \
    manual "$evidence_directory" "$GITHUB_SHA" "$run_status" \
    --witness-root "$witness_root" --receipt-root "$receipt_root" \
    --case-set "$fixture_case_set" \
    --secret-values-file "$evidence_secret_values_file" \
    --runner-uid "$(id -u)" --runner-gid "$(id -g)" \
    "${native_manifest_args[@]}" || export_status=$?
  if (( export_status != 0 )) && [[ -n "$native_witness_document" ]]; then
    # The exporter reports only the first violated field; the witness document names the stop.
    print_witness_document "$native_witness_document"
  fi
  return "$export_status"
}

cleanup() {
  status=$?
  trap - EXIT INT TERM
  set +e
  cleanup_failed=0
  guest_stopped=1
  control_stopped=1
  if (( guest_unit_attempted )); then
    if [[ -n "$machine" ]]; then
      timeout 30s sudo systemd-run --machine="$machine" --quiet --wait --pipe --collect \
        --unit="${machine}-poweroff" /usr/bin/systemctl poweroff >/dev/null 2>&1
    fi
    if ! wait_for_guest_stop 30; then
      sudo systemctl --no-block stop "$systemd_unit" >/dev/null 2>&1
      if ! wait_for_guest_stop 90; then
        sudo systemctl kill --signal=SIGKILL --kill-whom=all "$systemd_unit" >/dev/null 2>&1
        if ! wait_for_guest_stop 30; then
          guest_stopped=0
          cleanup_failed=1
          printf 'Guest stop could not be proven; retaining private rootfs and recovery material at %s\n' "$fixture_root" >&2
        fi
      fi
    fi
    if (( guest_stopped )); then
      if ! guest_stopped_proven; then
        guest_stopped=0
        cleanup_failed=1
        printf 'Guest unit/cgroup is not physically empty; retaining private rootfs at %s\n' "$fixture_root" >&2
      else
        sudo systemctl reset-failed "$systemd_unit" >/dev/null 2>&1 || true
      fi
    fi
  fi
  if (( control_started )); then
    control_stopped=0
    timeout 30s env -u GH_TOKEN -u GITHUB_TOKEN -u SYMMETRY_OPENAI_API_KEY \
      "GH_CONFIG_DIR=$control_gh_config_dir" "$control_bin" stop >/dev/null 2>&1
    control_stop_status=$?
    for _ in {1..30}; do
      if ! curl --fail --silent --show-error --max-time 1 http://127.0.0.1:4000/healthz >/dev/null 2>&1; then
        control_stopped=1
        break
      fi
      sleep 1
    done
    if (( ! control_stopped )); then
      cleanup_failed=1
      printf 'Control release did not stop within the bounded cleanup window; retaining private fixture at %s\n' "$fixture_root" >&2
    elif (( control_stop_status != 0 )); then
      cleanup_failed=1
      printf 'Control stop command failed (status=%s); retaining private fixture at %s\n' \
        "$control_stop_status" "$fixture_root" >&2
    fi
  fi

  if (( guest_stopped )); then
    if ! export_native_evidence "$status"; then
      cleanup_failed=1
      sudo rm -f -- "$evidence_directory/manifest.json" >/dev/null 2>&1
      printf 'Allowlisted evidence could not be safely exported; retaining private fixture at %s\n' "$fixture_root" >&2
    fi
  fi
  # Stopping the guest does not retire any App journal or retained checkout.
  # Keep the private source material; only allowlisted summaries leave the runner.
  if [[ -d "$fixture_root" ]]; then
    printf 'Retaining protected fixture recovery directory: %s\n' "$fixture_root" >&2
  fi
  if (( cleanup_failed )) && (( status == 0 )); then
    status=1
  fi
  if (( cleanup_failed )); then
    sudo rm -f -- "$evidence_directory/manifest.json" >/dev/null 2>&1
  fi
  if (( status == 0 && cleanup_failed == 0 )); then
    printf 'Linux amd64 installed A/B case set %s passed after verified guest/Control stop and allowlisted evidence export; private journals/checkouts remain retained, and guest teardown is not a containment StopReceipt.\n' \
      "$fixture_case_set"
  fi
  exit "$status"
}
trap cleanup EXIT INT TERM

if [[ -z "$native_containment_kind" ]]; then
  printf '%s' "$SYMMETRY_OPENAI_API_KEY" > "$native_api_key_file"
  chmod 0600 "$native_api_key_file"
fi
printf '%s' "$GH_TOKEN" > "$control_github_token_file"
chmod 0600 "$control_github_token_file"
unset SYMMETRY_OPENAI_API_KEY GH_TOKEN GITHUB_TOKEN
mkdir -m 0700 "$control_gh_config_dir"
GH_CONFIG_DIR="$control_gh_config_dir" gh auth login --hostname github.com --git-protocol https --with-token \
  < "$control_github_token_file" >/dev/null 2>&1
GH_CONFIG_DIR="$control_gh_config_dir" gh config set git_protocol https --host github.com >/dev/null 2>&1
rm -f -- "$control_github_token_file"
chmod 0600 "$control_gh_config_dir/hosts.yml"
[[ "$(stat -c '%u:%g:%a' "$control_gh_config_dir")" == "$(id -u):$(id -g):700" ]] ||
  fail 'private Control GH_CONFIG_DIR must be runner-owned mode 0700'
[[ "$(stat -c '%u:%g:%a' "$control_gh_config_dir/hosts.yml")" == "$(id -u):$(id -g):600" ]] ||
  fail 'private Control GitHub auth file must be runner-owned mode 0600'
mkdir -m 0700 "$build_stage"

printf 'Building static production Linux amd64 owner binaries.\n'
(
  cd "$repo/daemon"
  env -u GH_TOKEN -u GITHUB_TOKEN go mod download
  env -u GH_TOKEN -u GITHUB_TOKEN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$build_stage/symmetry-daemon" ./cmd/symmetry-daemon
  env -u GH_TOKEN -u GITHUB_TOKEN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$build_stage/symmetry-linux-launcher-core" ./cmd/symmetry-linux-launcher
  env -u GH_TOKEN -u GITHUB_TOKEN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$build_stage/symmetry-linux-clone-bootstrap" ./cmd/symmetry-linux-clone-bootstrap
  env -u GH_TOKEN -u GITHUB_TOKEN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$build_stage/symmetry-ci-linux-v2-readback-reader" ./internal/linuxreadback/cmd/linux-v2-readback-reader
  env -u GH_TOKEN -u GITHUB_TOKEN cc -O2 -static -fno-pie -no-pie -Wl,-z,relro,-z,now -Wl,-z,noexecstack \
    -o "$build_stage/symmetry-linux-gate" native/linux-gate/c-gate/gate.c
  env -u GH_TOKEN -u GITHUB_TOKEN cc -O2 -static -fno-pie -no-pie -Wl,-z,relro,-z,now -Wl,-z,noexecstack \
    -o "$build_stage/symmetry-linux-launcher" cmd/symmetry-linux-launcher/c-wrapper/wrapper.c
  env -u GH_TOKEN -u GITHUB_TOKEN cc -O2 -static -fno-pie -no-pie -Wl,-z,relro,-z,now -Wl,-z,noexecstack \
    -o "$build_stage/symmetry-ci-literal-pid-reuse-helper" ../scripts/ci/literal-pid-reuse-helper.c
  install -m 0644 ../scripts/systemd/symmetry-owner.service.in "$build_stage/symmetry-owner.service.in"
  install -m 0644 ../scripts/systemd/symmetry-daemon.service.in "$build_stage/symmetry-daemon.service.in"
)

source "$repo/scripts/ci/linux-v2-native-tools.sh"
linux_v2_fetch_native_tools "$fixture_root"

native_rootfs_base=${SYMMETRY_LINUX_V2_NATIVE_ROOTFS_BASE:-}
if [[ -n "$native_rootfs_base" ]]; then
  [[ "$native_rootfs_base" == /* && -d "$native_rootfs_base" && ! -L "$native_rootfs_base" ]] ||
    fail 'the shared disposable guest rootfs base must be an absolute directory'
  printf 'Cloning the shared disposable guest rootfs base for this case set: %s\n' "$native_rootfs_base"
  sudo cp -a -- "$native_rootfs_base" "$rootfs"
else
  bash "$repo/scripts/ci/linux-v2-native-rootfs-base.sh" "$rootfs"
fi
sudo chroot "$rootfs" /usr/bin/test -x /usr/bin/readelf
sudo install -d -o root -g root -m 0755 "$rootfs/root" "$rootfs/etc/symmetry" "$rootfs/usr/local/bin" \
  "$rootfs/usr/libexec" "$rootfs/usr/libexec/symmetry-test" "$rootfs/home" "$rootfs/var/lib" "$rootfs/workspace" \
  "$rootfs/opt"
sudo install -d -o root -g root -m 0755 "$rootfs/usr/local/lib" "$rootfs/usr/local/lib/symmetry-pi"
# pi reports its version from the package.json beside the executable, so the release tree ships whole.
sudo cp -a -- "$pi_unpack/pi/." "$rootfs/usr/local/lib/symmetry-pi/"
sudo chown -R root:root "$rootfs/usr/local/lib/symmetry-pi"
sudo chmod 0755 "$rootfs/usr/local" "$rootfs/usr/local/lib"
sudo find "$rootfs/usr/local/lib/symmetry-pi" -type d -exec chmod 0755 {} +
sudo find "$rootfs/usr/local/lib/symmetry-pi" -type f ! -perm -u+x -exec chmod 0644 {} +
sudo find "$rootfs/usr/local/lib/symmetry-pi" -type f -perm -u+x -exec chmod 0755 {} +
sudo chmod 0755 "$rootfs/usr/local/lib/symmetry-pi/pi"
[[ "$(sudo stat -c '%u:%g:%a' "$rootfs/usr/local/lib/symmetry-pi")" == '0:0:755' ]] ||
  fail 'staged pi share directory must be root-owned mode-0755 so A can traverse it'
[[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs/usr/local/lib/symmetry-pi/pi")" == '0:0:755:1' ]] ||
  fail 'staged pi executable must be a singly-linked root-owned mode-0755 file'
[[ "$(sudo sha256sum "$rootfs/usr/local/lib/symmetry-pi/pi" | cut -d' ' -f1)" == "$pi_executable_sha" ]] ||
  fail 'staged pi executable digest differs from the pinned 1.5.2 release archive'
[[ "$(linux_v2_pi_tree_digest "$rootfs/usr/local/lib/symmetry-pi" sudo)" == "${pi_tree_sha:-}" ]] ||
  fail 'staged pi release tree differs from the pinned 1.5.2 archive'
stage_guest_binary "$opencode_unpack/opencode" /usr/local/bin/opencode ec93043f18f8a6b4d8e71ec9260e292ba6f91b66b142782135d40f0b58b0c152 'opencode executable' opencode
if [[ "$fixture_case_set" == codex_containment ]]; then
  linux_v2_fetch_codex "$fixture_root"
  stage_guest_binary "$codex_executable" /usr/local/bin/codex "$codex_executable_sha" 'codex executable' codex
  stage_guest_binary "$code_mode_host_executable" /usr/local/bin/codex-code-mode-host "$code_mode_host_executable_sha" 'codex code-mode host' codex
  stage_guest_binary "$bwrap_executable" /usr/local/bin/bwrap "$bwrap_executable_sha" bwrap codex
elif [[ "$fixture_case_set" == claude_containment ]]; then
  linux_v2_fetch_claude "$fixture_root"
  stage_guest_binary "$claude_executable" /usr/local/bin/claude "$claude_executable_sha" 'claude executable' claude
fi
sudo install -o root -g root -m 0755 "$repo/scripts/install-linux-owner.sh" "$rootfs/root/install-linux-owner.sh"
sudo install -d -o root -g root -m 0700 "$rootfs/root/symmetry-linux-owner-stage"
sudo install -d -o root -g root -m 0700 \
  "$rootfs/root/.cache" "$rootfs/root/.cache/go-build" "$rootfs/root/.cache/tmp"
for artifact in \
  symmetry-linux-launcher symmetry-linux-launcher-core symmetry-linux-gate \
  symmetry-linux-clone-bootstrap symmetry-daemon symmetry-owner.service.in symmetry-daemon.service.in; do
  sudo install -o root -g root -m 0755 "$build_stage/$artifact" "$rootfs/root/symmetry-linux-owner-stage/$artifact"
done
sudo chmod 0644 "$rootfs/root/symmetry-linux-owner-stage/symmetry-owner.service.in" \
  "$rootfs/root/symmetry-linux-owner-stage/symmetry-daemon.service.in"
sudo sh -c ": > '$rootfs/etc/machine-id'"
printf 'symmetry-linux-v2-ci\n' | sudo tee "$rootfs/etc/hostname" >/dev/null

A_USER=symmetry-a
B_USER=symmetry-b
A_UID=52000
A_GID=52000
B_UID=52001
B_GID=52001
A_HOME="/home/$A_USER"
OWNER_TEST_ROOT=/opt/symmetry-owner-test
# The installed witness refuses a fixed-B native HOME outside its isolated owner witness root.
B_HOME="$OWNER_TEST_ROOT/fixed-b-home"
SYNTHETIC_GOAL_CLEANUP_ROOT=$OWNER_TEST_ROOT/synthetic-goal-cleanup
SYNTHETIC_GOAL_CLEANUP_SOURCE=$SYNTHETIC_GOAL_CLEANUP_ROOT/source
SYNTHETIC_GOAL_CLEANUP_SEEDS=$SYNTHETIC_GOAL_CLEANUP_ROOT/seeds
SYNTHETIC_GOAL_CLEANUP_CLONES=$SYNTHETIC_GOAL_CLEANUP_ROOT/clones
PRODUCTION_ROOT=$OWNER_TEST_ROOT/production
A_SOURCE=$PRODUCTION_ROOT/source
B_CLONE_ROOT=$PRODUCTION_ROOT/clones
SEED_ROOT=$PRODUCTION_ROOT/seeds
STATE_ROOT=$PRODUCTION_ROOT/state
DAEMON_SERVICE_STATE_DIR=/var/lib/symmetry-v2
UNPROVISIONED_STATE_DIR=$STATE_ROOT/unprovisioned
DAEMON_SERVICE_WORKSPACE_ROOT=$PRODUCTION_ROOT/daemon-service-clones
PI_SHARE=$PRODUCTION_ROOT/pi-provider-share
CLONE_TEST_ROOT=$OWNER_TEST_ROOT/clone-root
CLONE_TEST_CLEANUP_SIBLING=$CLONE_TEST_ROOT/cleanup-sibling
CLONE_TEST_CLEANUP_TARGET=$CLONE_TEST_ROOT/binding-cleanup-binding/run-cleanup-scratch/generation-1
CLONE_TEST_CLEANUP_PARENT_DEATH_TARGET=$CLONE_TEST_ROOT/binding-cleanup-binding/run-cleanup-parent-death/generation-1
CLONE_TEST_SEED_ROOT=$OWNER_TEST_ROOT/seed-root
CLONE_TEST_SOURCE_ROOT=$OWNER_TEST_ROOT/source-root
CLONE_TEST_B_HOME=$OWNER_TEST_ROOT/b-home
CLONE_TEST_ACK_ROOT=$OWNER_TEST_ROOT/ack-root
CLONE_TEST_MARKER_ROOT=$OWNER_TEST_ROOT/clone-marker-root
CLONE_TEST_MARKER=$CLONE_TEST_MARKER_ROOT/hooks
NATIVE_HOOK_MARKER_ROOT=$OWNER_TEST_ROOT/native-hook-markers
OWNER_CONFIG_ROOT=$OWNER_TEST_ROOT/config
OWNER_REQUEST_ROOT=$OWNER_TEST_ROOT/requests
OWNER_CONTROL_RECEIPT_CONTEXT_ROOT=$OWNER_REQUEST_ROOT/control-provider-receipt-contexts
OWNER_BARRIER_ROOT=$OWNER_TEST_ROOT/barriers
OWNER_STAGE_REPLAY_ROOT=$OWNER_TEST_ROOT/stage-replay
OWNER_ACK_LOSS_ROOT=$OWNER_TEST_ROOT/ack-loss
OWNER_NEGATIVE_ROOT=$OWNER_TEST_ROOT/negative-root
OWNER_UNPROVISIONED_ROOT=$OWNER_TEST_ROOT/unprovisioned
OWNER_ALTERNATE_CGROUP_MOUNT=$OWNER_TEST_ROOT/alternate_mount_cgroup
OWNER_HELPER_EXIT_CASE=helper_exit_after_release_before_register
OWNER_HELPER_EXIT_WORK_ITEM=stage_replay_helper_exit_after_release_before_native_register
OWNER_HELPER_EXIT_ROOT=$OWNER_BARRIER_ROOT/helper-exit-after-release-before-register
OWNER_HELPER_EXIT_MARKER=$NATIVE_HOOK_MARKER_ROOT/helper-exit-after-release-before-register
OWNER_PID_REUSE_ROOT=$OWNER_TEST_ROOT/pid-reuse
OWNER_PID_REUSE_HELPER=/usr/libexec/symmetry-test/literal-pid-reuse-helper
OWNER_CONTROL_RECEIPT_ROOT=$A_HOME/ci-fixture/control-receipts
PID_REUSE_WATCHER_UNIT=symmetry-ci-pid-reuse-watcher.service
READBACK_READER=/usr/libexec/symmetry-test/linux-v2-readback-reader
READBACK_CHANNEL=/run/symmetry-readback
READBACK_READER_UNIT=symmetry-ci-readback-reader.service
READBACK_LEAF_ROOT=/sys/fs/cgroup/system.slice/symmetry-owner.service/manager
OWNER_STAGE_REPLAY_HOLDS=(
  reservation_durable create_started leaf_bound helper_bound launcher_bound gate_bound
  handoff_prepared handoff_committed lease_armed checkout_ready native_release
  stop_receipt_durable leaf_removed journal_cleared
)
OWNER_ACK_LOSS_STAGES=(
  helper_bound launcher_bound gate_bound handoff_prepared handoff_committed lease_armed
  checkout_ready stop_receipt_durable native_release journal_cleared
)
# Stop(ctx) has no caller-selected PID or target: production derives the exact
# subject from the authenticated prepared gate, RunKey, pidfd, and leaf. A
# wrong-live-target Stop case is therefore structurally inapplicable; adding a
# test-only target selector would widen the production authority surface.
OWNER_NEGATIVE_CASES=(
  stale_journal_run_key stale_journal_fence stale_journal_subject receipt_persist_failure
  stale_leaf alternate_mount migration literal_pid_reuse
  helper_stop_response_loss helper_ack_response_loss helper_launcher_response_loss
  helper_exit_after_release_before_register
)
OWNER_HOSTILE_MARKER_DIRS=(hostile-marker-symlink hostile-marker-fifo hostile-marker-oversized)
OWNER_STATE_DIRS=(
  "$DAEMON_SERVICE_STATE_DIR"
  "$UNPROVISIONED_STATE_DIR"
)
OWNER_TEST_LEAF=

python3 - "$DAEMON_SERVICE_STATE_DIR" <<'PY'
import hashlib
import os
import sys

installed_state_dir = sys.argv[1]
if installed_state_dir != "/var/lib/symmetry-v2":
    raise SystemExit("installed OwnerConfig StateDir must be the fixed /var/lib/symmetry-v2 path")
run_key = "00000000-0000-4000-8000-000000000000" + chr(0) + "1"
digest = hashlib.sha256(run_key.encode("ascii")).hexdigest()
if not os.path.isabs(installed_state_dir) or os.path.normpath(installed_state_dir) != installed_state_dir:
    raise SystemExit("installed OwnerConfig StateDir must be canonical and absolute")
endpoint = os.path.join(installed_state_dir, f"owner-{digest}.sock")
endpoint_bytes = len(os.fsencode(endpoint))
if endpoint_bytes >= 100:
    raise SystemExit(
        f"installed recovery endpoint is {endpoint_bytes} bytes, exceeds the <100-byte App limit"
    )
print(f"Verified installed OwnerConfig recovery endpoint length: {endpoint_bytes} bytes")
PY

sudo install -d -o root -g root -m 0755 "$rootfs$OWNER_TEST_ROOT" "$rootfs$PRODUCTION_ROOT"
sudo install -d -o root -g "$A_GID" -m 0751 "$rootfs$SYNTHETIC_GOAL_CLEANUP_ROOT"
if [[ "$fixture_case_set" == synthetic_goal_cleanup ]]; then
  # The B clone bootstrap opens every seed ancestor O_RDONLY
  # (clonebootstrap/filesystem_linux_amd64.go openReadOnlyAPathDirectory), so B needs read here.
  sudo setfacl -m "u:$B_UID:r-x,m::r-x" "$rootfs$SYNTHETIC_GOAL_CLEANUP_ROOT"
fi
sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 \
  "$rootfs$SYNTHETIC_GOAL_CLEANUP_SOURCE" "$rootfs$SYNTHETIC_GOAL_CLEANUP_SEEDS"
sudo install -d -o "$B_UID" -g "$B_GID" -m 0700 "$rootfs$SYNTHETIC_GOAL_CLEANUP_CLONES"
sudo setfacl -m "u:$B_UID:r-x,m::r-x,d:u:$B_UID:r-x,d:m::r-x" \
  "$rootfs$SYNTHETIC_GOAL_CLEANUP_SEEDS"
# Inherited ACLs, not umask 077, govern the B bootstrap's Mkdirat(0770); mask out group write
# so the binding/run/generation modes pass the exact root-cleanup checks.
sudo setfacl -m "u:$A_UID:r-x,m::r-x,d:u:$A_UID:r-x,d:m::r-x" \
  "$rootfs$SYNTHETIC_GOAL_CLEANUP_CLONES"
sudo install -d -o root -g "$A_GID" -m 2730 "$rootfs$OWNER_PID_REUSE_ROOT"
sudo install -o root -g root -m 0555 "$build_stage/symmetry-ci-literal-pid-reuse-helper" \
  "$rootfs$OWNER_PID_REUSE_HELPER"
sudo install -o root -g root -m 0555 "$build_stage/symmetry-ci-linux-v2-readback-reader" \
  "$rootfs$READBACK_READER"
for trusted_helper_path in /usr /usr/libexec /usr/libexec/symmetry-test; do
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$trusted_helper_path")" == '0:0:755' ]] ||
    fail "literal PID-reuse helper parent must be root-owned non-writable mode 0755: $trusted_helper_path"
done
[[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$OWNER_PID_REUSE_HELPER")" == '0:0:555:1' ]] ||
  fail 'literal PID-reuse helper must be a singly-linked root-owned mode-0555 executable outside A-writable data'
[[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$READBACK_READER")" == '0:0:555:1' ]] ||
  fail 'root readback reader must be a singly-linked root-owned mode-0555 executable outside A-writable data'
[[ "$(sudo stat -c '%u:%g:%a' "$rootfs$OWNER_PID_REUSE_ROOT")" == "0:$A_GID:2730" ]] ||
  fail 'literal PID-reuse exchange directory must be root:A mode 02730'
sudo chroot "$rootfs" /usr/sbin/useradd --uid "$A_UID" --user-group --create-home --home-dir "$A_HOME" --shell /bin/bash "$A_USER"
sudo chroot "$rootfs" /usr/sbin/useradd --uid "$B_UID" --user-group --create-home --home-dir "$B_HOME" --shell /bin/bash "$B_USER"
[[ "$(sudo chroot "$rootfs" /usr/bin/id -u "$A_USER")" == "$A_UID" ]] || fail 'the native owner account did not receive its fixed A UID'
[[ "$(sudo chroot "$rootfs" /usr/bin/id -g "$A_USER")" == "$A_GID" ]] || fail 'the native owner account did not receive its dedicated A primary group'
[[ "$(sudo chroot "$rootfs" /usr/bin/id -G "$A_USER")" == "$A_GID" ]] || fail 'A must have no supplementary groups beyond its dedicated primary group'
[[ "$(sudo chroot "$rootfs" /usr/bin/id -u "$B_USER")" == "$B_UID" ]] || fail 'the native execution account did not receive its fixed B UID'
[[ "$(sudo chroot "$rootfs" /usr/bin/id -g "$B_USER")" == "$B_GID" ]] || fail 'the native execution account did not receive its dedicated B primary group'
[[ "$(sudo chroot "$rootfs" /usr/bin/id -G "$B_USER")" == "$B_GID" ]] || fail 'B must have no supplementary groups beyond its dedicated primary group'
sudo chmod 0700 "$rootfs$A_HOME" "$rootfs$B_HOME"
# install -d applies -o/-g/-m to each named directory only; the implicit .cache parent is left
# root-owned on this runner, so name .cache itself and fence all three A XDG cache directories.
sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 \
  "$rootfs$A_HOME/ci-fixture" "$rootfs$A_HOME/.cache" \
  "$rootfs$A_HOME/.cache/go-build" "$rootfs$A_HOME/.cache/tmp" \
  "$rootfs$A_HOME/ci-source" "$rootfs$A_HOME/.ci-go" \
  "$rootfs$A_HOME/.ci-gomod" "$rootfs$A_SOURCE" "$rootfs$STATE_ROOT" \
  "$rootfs$DAEMON_SERVICE_STATE_DIR" \
  "$rootfs$SEED_ROOT"
for state_dir in "${OWNER_STATE_DIRS[@]}"; do
  # The production drain verifier is read-only; provision its complete Store layout before invoking the installer.
  sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$state_dir"
  sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$state_dir/runs"
  sudo install -o "$A_UID" -g "$A_GID" -m 0600 /dev/null "$rootfs$state_dir/.symmetry-daemon.lock"
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$state_dir")" == "$A_UID:$A_GID:700" ]] ||
    fail "private Store StateDir must be A-owned mode 0700: $state_dir"
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$state_dir/runs")" == "$A_UID:$A_GID:700" ]] ||
    fail "private Store runs directory must be A-owned mode 0700: $state_dir"
  if ! sudo test -f "$rootfs$state_dir/.symmetry-daemon.lock" ||
     ! sudo test ! -L "$rootfs$state_dir/.symmetry-daemon.lock" ||
     [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$state_dir/.symmetry-daemon.lock")" != "$A_UID:$A_GID:600:1" ]]; then
    fail "private Store lock must be a singly-linked A-owned mode-0600 file: $state_dir"
  fi
done
sudo install -d -o "$B_UID" -g "$B_GID" -m 0700 "$rootfs$B_CLONE_ROOT"
sudo install -d -o "$B_UID" -g "$B_GID" -m 0700 "$rootfs$DAEMON_SERVICE_WORKSPACE_ROOT"
sudo install -d -o "$A_UID" -g "$B_GID" -m 2750 "$rootfs$PI_SHARE"
[[ "$(sudo stat -c '%u:%g:%a' "$rootfs/home")" == '0:0:755' ]] || fail 'the shared /home parent must remain root-owned and traversable'
[[ "$(sudo stat -c '%u:%g:%a' "$rootfs$A_HOME")" == "$A_UID:$A_GID:700" ]] || fail 'A passwd HOME must remain A-owned mode 0700'
for a_cache_directory in "$rootfs$A_HOME/.cache" "$rootfs$A_HOME/.cache/go-build" "$rootfs$A_HOME/.cache/tmp"; do
  [[ "$(sudo stat -c '%u:%g:%a' "$a_cache_directory")" == "$A_UID:$A_GID:700" ]] ||
    fail "A XDG cache directory must be A-owned mode 0700: $a_cache_directory"
done
[[ "$(sudo stat -c '%u:%g:%a' "$rootfs$B_HOME")" == "$B_UID:$B_GID:700" ]] || fail 'B passwd HOME must remain B-owned mode 0700'
b_passwd_home=$(sudo awk -F: -v user="$B_USER" '$1 == user {print $6}' "$rootfs/etc/passwd")
[[ "$b_passwd_home" == "$B_HOME" ]] || fail 'B passwd HOME must be the isolated owner witness root home'
b_native_credential_directories=(
  "$B_HOME/.pi" "$B_HOME/.pi/agent" "$B_HOME/.config" "$B_HOME/.config/opencode" \
  "$B_HOME/.pi/agent/sessions" \
  "$B_HOME/.local" "$B_HOME/.local/share" "$B_HOME/.local/share/opencode"
)
b_native_credential_files=(
  "$B_HOME/.pi/agent/settings.json" "$B_HOME/.pi/agent/auth.json" \
  "$B_HOME/.pi/agent/models.json" \
  "$B_HOME/.config/opencode/opencode.json" "$B_HOME/.local/share/opencode/auth.json"
)
if [[ "$native_containment_kind" == codex ]]; then
  b_native_credential_directories=("$B_HOME/.codex")
  b_native_credential_files=("$B_HOME/.codex/auth.json")
elif [[ "$native_containment_kind" == claude_code ]]; then
  b_native_credential_directories=("$B_HOME/.claude")
  b_native_credential_files=("$B_HOME/.claude/.credentials.json")
fi
# The installed owner's native preflight requires the fixed-B XDG cache before any release.
sudo install -d -o "$B_UID" -g "$B_GID" -m 0700 "$rootfs$B_HOME/hooks" \
  "$rootfs$B_HOME/.cache" \
  "${b_native_credential_directories[@]/#/$rootfs}"
for private_directory in "$B_HOME" "$B_HOME/hooks" "$B_HOME/.cache" "${b_native_credential_directories[@]}"; do
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$private_directory")" == "$B_UID:$B_GID:700" ]] ||
    fail "B private HOME directory must be B-owned mode 0700 before A search ACLs: $private_directory"
done
# A may resolve known B credential paths for installation preflight, but cannot list or modify B's private directories.
sudo setfacl -m "u:$A_UID:--x" "$rootfs$B_HOME" "${b_native_credential_directories[@]/#/$rootfs}"
sudo setfacl -m "u:$A_UID:r-x,m::r-x,d:u:$A_UID:r-x,d:u:$B_UID:rwx,d:m::rwx" "$rootfs$B_CLONE_ROOT"
sudo setfacl -m "u:$A_UID:r-x,m::r-x,d:u:$A_UID:r-x,d:u:$B_UID:rwx,d:m::rwx" \
  "$rootfs$DAEMON_SERVICE_WORKSPACE_ROOT"
sudo setfacl -m "u:$B_UID:r-x,m::r-x,d:u:$B_UID:r-x,d:m::r-x" "$rootfs$SEED_ROOT"
sudo install -d -o "$B_UID" -g "$B_GID" -m 0700 "$rootfs$CLONE_TEST_ROOT" "$rootfs$CLONE_TEST_B_HOME"
sudo install -d -o "$B_UID" -g "$B_GID" -m 0700 \
  "$rootfs$CLONE_TEST_CLEANUP_SIBLING" \
  "$rootfs$CLONE_TEST_ROOT/binding-cleanup-binding" \
  "$rootfs$CLONE_TEST_ROOT/binding-cleanup-binding/run-cleanup-scratch" \
  "$rootfs$CLONE_TEST_CLEANUP_TARGET" \
  "$rootfs$CLONE_TEST_ROOT/binding-cleanup-binding/run-cleanup-parent-death" \
  "$rootfs$CLONE_TEST_CLEANUP_PARENT_DEATH_TARGET" \
  "$rootfs$CLONE_TEST_CLEANUP_TARGET/.git" \
  "$rootfs$CLONE_TEST_CLEANUP_TARGET/.git/objects" \
  "$rootfs$CLONE_TEST_CLEANUP_TARGET/.git/objects/pack" \
  "$rootfs$CLONE_TEST_CLEANUP_TARGET/nested" \
  "$rootfs$CLONE_TEST_CLEANUP_TARGET/nested/deep"
sudo python3 - "$rootfs$CLONE_TEST_CLEANUP_SIBLING/keep" \
  "$rootfs$CLONE_TEST_CLEANUP_TARGET/.git/config" \
  "$rootfs$CLONE_TEST_CLEANUP_TARGET/.git/objects/pack/pack-test" \
  "$rootfs$CLONE_TEST_CLEANUP_TARGET/nested/deep/file" \
  "$rootfs$CLONE_TEST_CLEANUP_TARGET/outside-link" \
  "$rootfs$CLONE_TEST_CLEANUP_PARENT_DEATH_TARGET" \
  "$CLONE_TEST_CLEANUP_SIBLING/keep" "$B_UID" "$B_GID" <<'PY'
import os
import pathlib
import sys

sibling_keep, git_config, pack_file, nested_file, outside_link, parent_death_root, sibling_target, b_uid, b_gid = sys.argv[1:]
b_uid, b_gid = int(b_uid), int(b_gid)

def write_b_file(path, data):
    path = pathlib.Path(path)
    path.write_bytes(data)
    os.chown(path, b_uid, b_gid)
    os.chmod(path, 0o644)

write_b_file(pathlib.Path(sibling_keep), b"keep\n")
write_b_file(pathlib.Path(git_config), b"[core]\nrepositoryformatversion = 0\n")
write_b_file(pathlib.Path(pack_file), b"fixture pack\n")
write_b_file(pathlib.Path(nested_file), b"nested fixture\n")
link = pathlib.Path(outside_link)
link.symlink_to(sibling_target)
os.chown(link, b_uid, b_gid, follow_symlinks=False)

root = pathlib.Path(parent_death_root)
write_b_file(root / "--cleanup-first", b"first cleanup entry\n")
for index in range(20_000):
    write_b_file(root / f"z-later-{index:06d}", b"later cleanup entry\n")
PY
for cleanup_directory in \
  "$CLONE_TEST_CLEANUP_SIBLING" \
  "$CLONE_TEST_ROOT/binding-cleanup-binding" \
  "$CLONE_TEST_ROOT/binding-cleanup-binding/run-cleanup-scratch" \
  "$CLONE_TEST_CLEANUP_TARGET" \
  "$CLONE_TEST_CLEANUP_TARGET/.git" \
  "$CLONE_TEST_CLEANUP_TARGET/.git/objects" \
  "$CLONE_TEST_CLEANUP_TARGET/.git/objects/pack" \
  "$CLONE_TEST_CLEANUP_TARGET/nested" \
  "$CLONE_TEST_CLEANUP_TARGET/nested/deep" \
  "$CLONE_TEST_ROOT/binding-cleanup-binding/run-cleanup-parent-death" \
  "$CLONE_TEST_CLEANUP_PARENT_DEATH_TARGET"; do
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$cleanup_directory")" == "$B_UID:$B_GID:700" ]] ||
    fail "cleanup fixture directories must remain B-owned mode 0700: $cleanup_directory"
  sudo setfacl -m "u:$A_UID:r-x,m::r-x" "$rootfs$cleanup_directory"
done
[[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$CLONE_TEST_CLEANUP_TARGET/.git/config")" == "$B_UID:$B_GID:644:1" ]] ||
  fail 'cleanup fixture .git/config must be a singly-linked B-owned read-only-to-A file'
[[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$CLONE_TEST_CLEANUP_SIBLING/keep")" == "$B_UID:$B_GID:644:1" ]] ||
  fail 'cleanup sibling marker must be a singly-linked B-owned readable file'
sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 \
  "$rootfs$CLONE_TEST_SEED_ROOT" "$rootfs$CLONE_TEST_SOURCE_ROOT" "$rootfs$CLONE_TEST_ACK_ROOT"
sudo install -d -o "$A_UID" -g "$B_GID" -m 2770 \
  "$rootfs$CLONE_TEST_MARKER_ROOT" "$rootfs$NATIVE_HOOK_MARKER_ROOT" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/pi" "$rootfs$NATIVE_HOOK_MARKER_ROOT/opencode" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/pi-crash" "$rootfs$NATIVE_HOOK_MARKER_ROOT/pi-daemon-crash" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/literal-pid-reuse" \
  "$rootfs$OWNER_HELPER_EXIT_MARKER" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/helper-stop-response-loss" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/helper-ack-response-loss" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/helper-launcher-response-loss" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/codex-containment" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/claude-containment" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/stage-replay-native-release-codex" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/stage-replay-native-release-claude"
for hold_after in "${OWNER_STAGE_REPLAY_HOLDS[@]}"; do
  sudo install -d -o "$A_UID" -g "$B_GID" -m 2770 \
    "$rootfs$NATIVE_HOOK_MARKER_ROOT/stage-replay-$hold_after"
done
for ack_loss_stage in "${OWNER_ACK_LOSS_STAGES[@]}"; do
  sudo install -d -o "$A_UID" -g "$B_GID" -m 2770 \
    "$rootfs$NATIVE_HOOK_MARKER_ROOT/ack-loss-$ack_loss_stage"
done
for marker_case in "${OWNER_HOSTILE_MARKER_DIRS[@]}"; do
  sudo install -d -o "$A_UID" -g "$B_GID" -m 2770 \
    "$rootfs$NATIVE_HOOK_MARKER_ROOT/$marker_case"
done
for negative_case in stale_journal_run_key stale_journal_fence stale_journal_subject; do
  sudo install -d -o "$A_UID" -g "$B_GID" -m 2770 \
    "$rootfs$NATIVE_HOOK_MARKER_ROOT/negative-$negative_case-setup" \
    "$rootfs$NATIVE_HOOK_MARKER_ROOT/negative-$negative_case-stale"
done
sudo install -d -o "$A_UID" -g "$B_GID" -m 2770 \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/receipt-persist-failure"
sudo install -d -o "$A_UID" -g "$B_GID" -m 2770 \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/stale-leaf" "$rootfs$NATIVE_HOOK_MARKER_ROOT/alternate-mount"
sudo install -d -o "$A_UID" -g "$B_GID" -m 2770 "$rootfs$NATIVE_HOOK_MARKER_ROOT/migration"
sudo install -d -o "$A_UID" -g "$B_GID" -m 2770 "$rootfs$NATIVE_HOOK_MARKER_ROOT/unprovisioned"
sudo install -d -o root -g "$A_GID" -m 0750 "$rootfs$OWNER_CONFIG_ROOT" "$rootfs$OWNER_REQUEST_ROOT" \
  "$rootfs$OWNER_CONTROL_RECEIPT_CONTEXT_ROOT"
sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$OWNER_CONTROL_RECEIPT_ROOT"
for receipt_case in pi_provider_action helper_sigkill_detached_child daemon_sigkill_helper_survives_detached_child; do
  sudo install -d -o root -g "$A_GID" -m 0750 \
    "$rootfs$OWNER_CONTROL_RECEIPT_CONTEXT_ROOT/$receipt_case"
done
sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$OWNER_BARRIER_ROOT"
for barrier_case in pi_provider_action opencode_native_health helper_sigkill_detached_child daemon_sigkill_helper_survives_detached_child wsl2_denial; do
  sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$OWNER_BARRIER_ROOT/$barrier_case"
done
for barrier_case in codex_containment claude_containment; do
  sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$OWNER_BARRIER_ROOT/$barrier_case"
done
sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$OWNER_BARRIER_ROOT/stage-replay"
sudo install -d -o root -g "$A_GID" -m 0750 "$rootfs$OWNER_STAGE_REPLAY_ROOT"
sudo install -d -o root -g "$A_GID" -m 0750 "$rootfs$OWNER_STAGE_REPLAY_ROOT"
sudo install -d -o root -g "$A_GID" -m 0750 "$rootfs$OWNER_ACK_LOSS_ROOT"
sudo install -d -o root -g "$A_GID" -m 0750 "$rootfs$OWNER_NEGATIVE_ROOT"
sudo install -d -o root -g "$A_GID" -m 0750 "$rootfs$OWNER_UNPROVISIONED_ROOT"
sudo install -d -o root -g "$A_GID" -m 0750 "$rootfs$OWNER_ALTERNATE_CGROUP_MOUNT"
sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$OWNER_BARRIER_ROOT/negative-root"
sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 \
  "$rootfs$OWNER_BARRIER_ROOT/negative-root/stale_journal_run_key/setup-legacy" \
  "$rootfs$OWNER_BARRIER_ROOT/negative-root/stale_journal_run_key/stale-legacy"
sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 \
  "$rootfs$OWNER_BARRIER_ROOT/unprovisioned" \
  "$rootfs$OWNER_BARRIER_ROOT/unprovisioned/request"
for hold_after in "${OWNER_STAGE_REPLAY_HOLDS[@]}"; do
  sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$OWNER_BARRIER_ROOT/stage-replay/$hold_after"
  sudo install -d -o root -g "$A_GID" -m 0750 "$rootfs$OWNER_STAGE_REPLAY_ROOT/$hold_after"
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$OWNER_BARRIER_ROOT/stage-replay/$hold_after")" == "$A_UID:$A_GID:700" ]] ||
    fail "stage replay barrier must be A-owned mode 0700: $hold_after"
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$OWNER_STAGE_REPLAY_ROOT/$hold_after")" == "0:$A_GID:750" ]] ||
    fail "stage replay bundle directory must be root:A mode 0750: $hold_after"
done
for kind in codex claude; do
  sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$OWNER_BARRIER_ROOT/stage-replay/native_release_$kind"
  sudo install -d -o root -g "$A_GID" -m 0750 "$rootfs$OWNER_STAGE_REPLAY_ROOT/native_release_$kind"
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$OWNER_BARRIER_ROOT/stage-replay/native_release_$kind")" == "$A_UID:$A_GID:700" ]] ||
    fail "stage replay barrier must be A-owned mode 0700: native_release_$kind"
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$OWNER_STAGE_REPLAY_ROOT/native_release_$kind")" == "0:$A_GID:750" ]] ||
    fail "stage replay bundle directory must be root:A mode 0750: native_release_$kind"
done
for ack_loss_stage in "${OWNER_ACK_LOSS_STAGES[@]}"; do
  sudo install -d -o root -g "$A_GID" -m 0750 \
    "$rootfs$OWNER_ACK_LOSS_ROOT/$ack_loss_stage"
  sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 \
    "$rootfs$OWNER_BARRIER_ROOT/stage-replay/$ack_loss_stage"
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$OWNER_ACK_LOSS_ROOT/$ack_loss_stage")" == "0:$A_GID:750" ]] ||
    fail "ack-loss bundle directory must be root:A mode 0750: $ack_loss_stage"
done
for negative_case in "${OWNER_NEGATIVE_CASES[@]}"; do
  negative_bundle="$OWNER_NEGATIVE_ROOT/$negative_case"
  sudo install -d -o root -g "$A_GID" -m 0750 "$rootfs$negative_bundle"
  if [[ "$negative_case" == "$OWNER_HELPER_EXIT_CASE" ]]; then
    sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$OWNER_HELPER_EXIT_ROOT"
  else
    sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$OWNER_BARRIER_ROOT/negative-root/$negative_case"
  fi
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$negative_bundle")" == "0:$A_GID:750" ]] ||
    fail "negative witness bundle must be root:A mode 0750: $negative_case"
  if [[ "$negative_case" == "$OWNER_HELPER_EXIT_CASE" ]]; then
    :
  elif [[ "$negative_case" == migration || "$negative_case" == receipt_persist_failure ||
        "$negative_case" == stale_leaf || "$negative_case" == alternate_mount ]]; then
    sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 "$rootfs$OWNER_BARRIER_ROOT/negative-root/$negative_case/request"
  else
    sudo install -d -o "$A_UID" -g "$A_GID" -m 0700 \
      "$rootfs$OWNER_BARRIER_ROOT/negative-root/$negative_case/setup" \
      "$rootfs$OWNER_BARRIER_ROOT/negative-root/$negative_case/stale"
  fi
done
[[ "$(sudo stat -c '%u:%g:%a' "$rootfs$OWNER_NEGATIVE_ROOT")" == "0:$A_GID:750" ]] ||
  fail 'negative witness bundle root must be root:A mode 0750'
[[ "$(sudo stat -c '%u:%g:%a' "$rootfs$OWNER_STAGE_REPLAY_ROOT")" == "0:$A_GID:750" ]] ||
  fail 'stage replay bundle root must be root:A mode 0750'
sudo install -d -o "$B_UID" -g "$B_GID" -m 0700 \
  "$rootfs$CLONE_TEST_B_HOME/hooks"
sudo setfacl -m "u:$A_UID:r-x,m::r-x,d:u:$A_UID:r-x,d:u:$B_UID:rwx,d:m::rwx" "$rootfs$CLONE_TEST_ROOT"
sudo setfacl -m "u:$B_UID:r-x,m::r-x,d:u:$B_UID:r-x,d:m::r-x" \
  "$rootfs$CLONE_TEST_SEED_ROOT" "$rootfs$CLONE_TEST_ACK_ROOT"
sudo setfacl -m "u:$A_UID:--x" "$rootfs$CLONE_TEST_B_HOME"
sudo setfacl -m "u:$A_UID:r-x,m::rwx" "$rootfs$CLONE_TEST_MARKER_ROOT" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT" "$rootfs$NATIVE_HOOK_MARKER_ROOT/pi" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/opencode" "$rootfs$NATIVE_HOOK_MARKER_ROOT/pi-crash" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/pi-daemon-crash" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/literal-pid-reuse" \
  "$rootfs$OWNER_HELPER_EXIT_MARKER" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/helper-stop-response-loss" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/helper-ack-response-loss" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/helper-launcher-response-loss" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/codex-containment" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/claude-containment" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/stage-replay-native-release-codex" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/stage-replay-native-release-claude"
for hold_after in "${OWNER_STAGE_REPLAY_HOLDS[@]}"; do
  sudo setfacl -m "u:$A_UID:r-x,m::rwx" \
    "$rootfs$NATIVE_HOOK_MARKER_ROOT/stage-replay-$hold_after"
done
for ack_loss_stage in "${OWNER_ACK_LOSS_STAGES[@]}"; do
  sudo setfacl -m "u:$A_UID:r-x,m::rwx" \
    "$rootfs$NATIVE_HOOK_MARKER_ROOT/ack-loss-$ack_loss_stage"
done
for marker_case in "${OWNER_HOSTILE_MARKER_DIRS[@]}"; do
  sudo setfacl -m "u:$A_UID:r-x,m::rwx" \
    "$rootfs$NATIVE_HOOK_MARKER_ROOT/$marker_case"
done
for negative_case in stale_journal_run_key stale_journal_fence stale_journal_subject; do
  sudo setfacl -m "u:$A_UID:r-x,m::rwx" \
    "$rootfs$NATIVE_HOOK_MARKER_ROOT/negative-$negative_case-setup" \
    "$rootfs$NATIVE_HOOK_MARKER_ROOT/negative-$negative_case-stale"
done
sudo setfacl -m "u:$A_UID:r-x,m::rwx" "$rootfs$NATIVE_HOOK_MARKER_ROOT/migration"
sudo setfacl -m "u:$A_UID:r-x,m::rwx" "$rootfs$NATIVE_HOOK_MARKER_ROOT/receipt-persist-failure"
sudo setfacl -m "u:$A_UID:r-x,m::rwx" \
  "$rootfs$NATIVE_HOOK_MARKER_ROOT/stale-leaf" "$rootfs$NATIVE_HOOK_MARKER_ROOT/alternate-mount"

cat > "$fixture_root/native.gitconfig" <<EOF
[core]
 hooksPath = $B_HOME/hooks
[user]
 name = Symmetry B clone fixture
 email = b-clone@example.invalid
EOF
cat > "$fixture_root/clone.gitconfig" <<EOF
[core]
 hooksPath = $CLONE_TEST_B_HOME/hooks
[user]
 name = Symmetry B clone fixture
 email = b-clone@example.invalid
EOF
cat > "$fixture_root/a.gitconfig" <<EOF
[safe]
 directory = $A_HOME/ci-source
EOF
cat > "$fixture_root/native-hook" <<'SH'
#!/bin/sh
set -eu
hook_name=${0##*/}
marker_root=${SYMMETRY_LINUX_V2_NATIVE_HOOK_MARKER_ROOT:?}
umask 022
printf '%s\n' "$hook_name" > "$marker_root/$hook_name"
if [ "$hook_name" = post-commit ]; then
  child_pid=$(/usr/bin/python3 -c 'import os; fd = os.open("/dev/null", os.O_RDWR); pid = os.fork(); (os.setsid(), os.dup2(fd, 0), os.dup2(fd, 1), os.dup2(fd, 2), os.execv("/usr/bin/sleep", ["sleep", "120"])) if pid == 0 else print(pid)')
  printf '%s\n' "$child_pid" > "$marker_root/post-commit-child.pid"
  attempts=0
  while [ ! -e "$marker_root/post-commit-child.release" ]; do
    attempts=$((attempts + 1))
    [ "$attempts" -lt 20 ] || exit 1
    /usr/bin/sleep 1
  done
fi
SH
cat > "$fixture_root/clone-hook" <<'SH'
#!/bin/sh
set -eu
hook_name=${0##*/}
marker=${SYMMETRY_LINUX_CLONE_TEST_MARKER:?}
umask 022
printf '%s\n' "$hook_name" >> "$marker"
SH
chmod 0755 "$fixture_root/native-hook" "$fixture_root/clone-hook"
sudo install -o "$A_UID" -g "$A_GID" -m 0600 "$fixture_root/a.gitconfig" "$rootfs$A_HOME/.gitconfig"
sudo install -o "$B_UID" -g "$B_GID" -m 0600 "$fixture_root/native.gitconfig" "$rootfs$B_HOME/.gitconfig"
sudo install -o "$B_UID" -g "$B_GID" -m 0600 "$fixture_root/clone.gitconfig" "$rootfs$CLONE_TEST_B_HOME/.gitconfig"
for hook in post-checkout post-commit; do
  sudo install -o "$B_UID" -g "$B_GID" -m 0755 "$fixture_root/native-hook" "$rootfs$B_HOME/hooks/$hook"
  sudo install -o "$B_UID" -g "$B_GID" -m 0755 "$fixture_root/clone-hook" "$rootfs$CLONE_TEST_B_HOME/hooks/$hook"
done

machine="symmetry-ci-v2-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}-$fixture_identity_suffix"
systemd_unit="$machine.service"
GOROOT=$(go env GOROOT)
GOMODCACHE=$(go env GOMODCACHE)
[[ -d "$GOROOT" && -d "$GOMODCACHE" ]] || fail 'Go toolchain and module cache are required inside the A-only source mount'

# The detached transient service has no interactive stdin; keep console output without forwarding EOF as a guest shutdown.
guest_unit_attempted=1
# The guest root must load the policy interface the installed preflight verifies; the default
# capability and bounding sets omit CAP_MAC_ADMIN, the load's only missing capability.
sudo systemd-run --system --quiet --unit="$machine" \
  --property=Delegate=yes --property=KillMode=mixed --property=TimeoutStopSec=90s \
  /usr/bin/systemd-nspawn --quiet --settings=no --private-users=off --machine="$machine" \
  --directory="$rootfs" --boot --keep-unit --register=yes --console=read-only --capability=CAP_MAC_ADMIN \
  --bind-ro="$repo:$A_HOME/ci-source" \
  --bind-ro="$GOROOT:$A_HOME/.ci-go" \
  --bind-ro="$GOMODCACHE:$A_HOME/.ci-gomod"
pin_guest_cgroup_while_active || fail 'could not pin the live disposable guest unit cgroup identity'

machine_run() {
  machine_run_count=$((machine_run_count + 1))
  # A command substitution runs this in a subshell: its increment dies there, so the counter alone
  # would repeat a name; the subshell's own BASHPID keeps every call form's unit unique.
  local unit="${machine}-step-${machine_run_count}-${BASHPID}"
  sudo systemd-run --machine="$machine" --quiet --wait --pipe --collect \
    --unit="$unit" -- "$@"
}

machine_run_as() {
  local user=$1
  local group=$2
  shift 2
  local passwd_log="$fixture_root/machine-run-as-passwd.log"
  machine_run /usr/bin/getent passwd "$user" >"$passwd_log" 2>&1 ||
    fail "read the guest passwd home directory for $user"
  local home
  home=$(cut -d: -f6 <"$passwd_log")
  [[ -n "$home" && "$home" == /* ]] || fail "the guest passwd entry for $user must name an absolute home"
  machine_run_count=$((machine_run_count + 1))
  # A pipeline runs this in a subshell and loses the increment there, so the suffix keeps the name unique.
  local unit="${machine}-step-${machine_run_count}-${BASHPID}"
  sudo systemd-run --machine="$machine" --quiet --wait --pipe --collect \
    --unit="$unit" --uid="$user" --gid="$group" \
    --setenv="HOME=$home" --setenv='PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin' \
    -- "$@"
}

machine_ready=0
for _ in {1..90}; do
  if sudo systemd-run --machine="$machine" --quiet --wait --pipe --collect \
    --unit="${machine}-ready" /usr/bin/test -d /run/systemd/system >/dev/null 2>&1; then
    machine_ready=1
    break
  fi
  sleep 1
done
(( machine_ready == 1 )) || fail 'the disposable systemd guest did not boot under its delegated transient host unit'
machine_run /usr/bin/test -f /sys/fs/cgroup/cgroup.controllers
machine_run /usr/bin/id -u "$A_USER"
machine_run /usr/bin/id -u "$B_USER"
machine_run /usr/bin/id -G "$A_USER"
machine_run /usr/bin/id -G "$B_USER"
for private_directory in "$B_HOME" "${b_native_credential_directories[@]}"; do
  # /usr/bin/test is the uutils coreutils build on this image and evaluates mode bits only,
  # ignoring POSIX ACLs; the bash builtin test queries the kernel through faccessat2.
  machine_run_as "$A_USER" "$A_USER" /usr/bin/bash -c 'test -x "$1"' bash "$private_directory"
  if machine_run_as "$A_USER" "$A_USER" /usr/bin/bash -c 'test -r "$1"' bash "$private_directory"; then
    fail "A unexpectedly listed B-private native configuration directory: $private_directory"
  fi
  if machine_run_as "$A_USER" "$A_USER" /usr/bin/bash -c 'test -w "$1"' bash "$private_directory"; then
    fail "A unexpectedly modified B-private native configuration directory: $private_directory"
  fi
done

printf 'Preparing the A-owned source repository and B-only native credentials.\n'
# The staged native tools are executed by A, so their traversability is proven from A's side.
machine_run_as "$A_USER" "$A_USER" /usr/bin/bash -c 'test -x "$1"' bash /usr/local/lib/symmetry-pi/pi ||
  fail 'staged pi executable must be executable by A inside the guest'
machine_run_as "$A_USER" "$A_USER" /usr/bin/bash -c 'test -r "$1"' bash /usr/local/lib/symmetry-pi/package.json ||
  fail 'staged pi package.json must be readable by A inside the guest'
machine_run_as "$A_USER" "$A_USER" /usr/bin/bash -c 'test -x "$1"' bash /usr/local/bin/opencode ||
  fail 'staged opencode executable must be executable by A inside the guest'
# The read-only runner-owned source mount cannot be chowned for A, and git's ownership guard also
# inspects its .git, so allow any directory for this one read-only clone.
machine_run_as "$A_USER" "$A_USER" /usr/bin/git -c 'safe.directory=*' \
  clone --no-hardlinks --no-checkout "$A_HOME/ci-source" "$A_SOURCE"
machine_run_as "$A_USER" "$A_USER" /usr/bin/git -C "$A_SOURCE" checkout --quiet -B main "$GITHUB_SHA"
machine_run_as "$A_USER" "$A_USER" /usr/bin/chmod -R go-rwx "$A_SOURCE"
if [[ "$fixture_case_set" == synthetic_goal_cleanup ]]; then
  machine_run_as "$A_USER" "$A_USER" /usr/bin/git clone --no-hardlinks --no-checkout \
    "$A_SOURCE" "$SYNTHETIC_GOAL_CLEANUP_SOURCE"
  machine_run_as "$A_USER" "$A_USER" /usr/bin/git -C "$SYNTHETIC_GOAL_CLEANUP_SOURCE" \
    checkout --quiet -B main "$GITHUB_SHA"
  machine_run_as "$A_USER" "$A_USER" /usr/bin/chmod -R go-rwx "$SYNTHETIC_GOAL_CLEANUP_SOURCE"
  machine_run_as "$A_USER" "$A_USER" /usr/bin/chmod 0750 "$SYNTHETIC_GOAL_CLEANUP_SOURCE"
fi

printf '%s\n' "$source_subject_json" > "$fixture_root/source-subject.json"

[[ "$source_commit" == "$GITHUB_SHA" ]] || fail 'A-owned source checkout differs from the exact CI subject revision'
control_fixture="$fixture_root/control-fixture.json"
(
  cd "$repo/control"
  env -u GH_TOKEN -u GITHUB_TOKEN -u SYMMETRY_OPENAI_API_KEY \
    MIX_ENV=prod mix run --no-start ../scripts/ci/provision-linux-v2-control-claims.exs prepare \
      "$control_fixture" "$GITHUB_REPOSITORY" "$GITHUB_REPOSITORY_OWNER" \
      "$source_commit" "$source_tree_digest"
)

pi_repository_id=$(jq --exit-status --raw-output .pi_repository_resource_id "$control_fixture")
opencode_repository_id=$(jq --exit-status --raw-output .opencode_repository_resource_id "$control_fixture")
pi_durability_repository_id=$(jq --exit-status --raw-output .pi_durability_repository_resource_id "$control_fixture")

python3 - "$fixture_root" "$A_HOME" "$A_SOURCE" \
  "$B_CLONE_ROOT" "$SEED_ROOT" "$DAEMON_SERVICE_STATE_DIR" "$PI_SHARE" "$SYMMETRY_PI_MODEL" \
  "$SYMMETRY_OPENCODE_MODEL" "$SYMMETRY_OPENAI_BASE_URL" "$native_api_key_file" "$pi_repository_id" \
  "$opencode_repository_id" "$pi_durability_repository_id" "$fixture_identity_suffix" \
  "$SYNTHETIC_GOAL_CLEANUP_SOURCE" "$SYNTHETIC_GOAL_CLEANUP_SEEDS" \
  "$SYNTHETIC_GOAL_CLEANUP_CLONES" <<'PY'
import json
import pathlib
import sys

(
    fixture_root, a_home, repository,
    clone_root, seed_root, owner_state_dir, pi_share, pi_model, opencode_model, openai_base_url, api_key_file,
    pi_repository_id, opencode_repository_id, pi_durability_repository_id, fixture_identity_suffix,
    synthetic_source, synthetic_seeds, synthetic_clones,
) = sys.argv[1:]
fixture = pathlib.Path(fixture_root)

def config(kind, profile, model, resource_id, provider_access=None):
    provider_access = kind == "pi" if provider_access is None else provider_access
    agent = {
        "command": "/usr/local/lib/symmetry-pi/pi" if kind == "pi" else "/usr/local/bin/opencode",
        "args": ["--provider", "openai", "--model", model] if kind == "pi" else [],
        "native_model": model,
        "native_model_provider": "openai",
        "input_mode": "json",
        "provider_access": provider_access,
        "interactive": True,
        "supervisory_control": False,
        "event_format": "jsonl",
        "env_allowlist": ["SYMMETRY_LINUX_V2_NATIVE_HOOK_MARKER_ROOT"],
    }
    if kind == "codex":
        agent["command"] = "/usr/local/bin/codex"
    elif kind == "claude_code":
        agent["command"] = "/usr/local/bin/claude"
        del agent["native_model"]
        del agent["native_model_provider"]
    value = {
        "control_plane_url": "http://127.0.0.1:4000",
        "allow_insecure_http": True,
        "state_dir": owner_state_dir,
        "linux_owner_cgroup_mount": "/sys/fs/cgroup",
        "linux_owner_delegated_path": "system.slice/symmetry-owner.service/manager",
        "linux_owner_socket": "/run/symmetry-owner/owner.sock",
        "machine_name": f"symmetry-linux-v2-{kind}-{fixture_identity_suffix}",
        "cleanup_timeout_ms": 120000,
        "agent_profiles": {profile: agent},
        "workspaces": {
            "native": {
                "policy": "git_clone_v2",
                "path": "",
                "repository": repository,
                "root": clone_root,
                "seed_root": seed_root,
                "ref": "",
                "cleanup": "never",
            }
        },
        "runtime": {
            "runtime_key": f"symmetry-linux-v2-{kind}-{fixture_identity_suffix}",
            "name": f"Linux v2 {kind} native acceptance {fixture_identity_suffix}",
            "capacity": 1,
            "agent_profile": profile,
            "workspace": "native",
            "repository_resource_id": resource_id,
            "harness_kind": kind,
            "harness_version": {
                "pi": "1.5.2",
                "opencode": "2.4.1",
                "codex": "0.168.0",
                "claude_code": "2.1.323",
            }[kind],
            "adapter_version": "symmetry-daemon:ci-linux-v2",
            "adapter_protocol_version": 1,
        },
    }
    if kind == "pi":
        value["pi_provider_extension_dir"] = pi_share
    return value

configs = {
    "pi-provider-action.json": config("pi", "pi-native", pi_model, pi_repository_id),
    "pi-helper-crash.json": config("pi", "pi-native", pi_model, pi_repository_id),
    "pi-daemon-crash.json": config("pi", "pi-native", pi_model, pi_repository_id),
    "pi-durability.json": config(
        "pi", "pi-durability", pi_model, pi_durability_repository_id, provider_access=False
    ),
    "opencode-health.json": config("opencode", "opencode-native", opencode_model, opencode_repository_id),
    "opencode-wsl2.json": config("opencode", "opencode-native", opencode_model, opencode_repository_id),
    "codex-containment.json": config("codex", "codex-native", "gpt-5.1-codex", opencode_repository_id),
    "claude-containment.json": config("claude_code", "claude-native", None, opencode_repository_id),
}
for name, value in configs.items():
    (fixture / name).write_text(json.dumps(value, separators=(",", ":")), encoding="utf-8")
if fixture_identity_suffix == "synthetic-goal-cleanup":
    cleanup_config = config(
        "pi", "pi-durability", pi_model, pi_durability_repository_id, provider_access=False
    )
    cleanup_config["workspaces"]["native"]["repository"] = synthetic_source
    cleanup_config["workspaces"]["native"]["root"] = synthetic_clones
    cleanup_config["workspaces"]["native"]["seed_root"] = synthetic_seeds
    (fixture / "synthetic-goal-cleanup.json").write_text(
        json.dumps(cleanup_config, separators=(",", ":")), encoding="utf-8"
    )

private_home = fixture / "b-home"
containment_kind = {"codex-containment": "codex", "claude-containment": "claude_code"}.get(fixture_identity_suffix)
if containment_kind is not None:
    kind_directory = private_home / (".codex" if containment_kind == "codex" else ".claude")
    kind_directory.mkdir(parents=True, mode=0o700)
    if containment_kind == "codex":
        (kind_directory / "auth.json").write_text(
            json.dumps({"OPENAI_API_KEY": "containment-placeholder-no-credential"}),
            encoding="utf-8",
        )
        staged_files = [".codex/auth.json"]
    else:
        (kind_directory / ".credentials.json").write_text(
            json.dumps(
                {
                    "claudeAiOauth": {
                        "accessToken": "containment-placeholder-no-credential",
                        "refreshToken": "containment-placeholder-no-credential",
                        "expiresAt": 4102444800000,
                        "scopes": ["user:inference"],
                    }
                }
            ),
            encoding="utf-8",
        )
        staged_files = [".claude/.credentials.json"]
    recorded_files = sorted(
        path.relative_to(private_home).as_posix() for path in private_home.rglob("*") if path.is_file()
    )
    if recorded_files != staged_files:
        raise SystemExit(f"containment fixture must stage exactly {staged_files}: {recorded_files}")
else:
    api_key = pathlib.Path(api_key_file).read_text(encoding="utf-8")
    (private_home / ".pi/agent").mkdir(parents=True, mode=0o700)
    (private_home / ".config/opencode").mkdir(parents=True, mode=0o700)
    (private_home / ".local/share/opencode").mkdir(parents=True, mode=0o700)
    (private_home / ".pi/agent/settings.json").write_text(
        json.dumps({"defaultProvider": "openai", "defaultModel": pi_model, "defaultThinkingLevel": "minimal"}),
        encoding="utf-8",
    )
    (private_home / ".pi/agent/auth.json").write_text(
        json.dumps({"openai": {"type": "api_key", "key": api_key}}), encoding="utf-8"
    )
    (private_home / ".pi/agent/models.json").write_text(
        json.dumps(
            {
                "providers": {
                    "openai": {
                        "baseUrl": openai_base_url,
                        "models": [
                            {
                                "id": pi_model,
                                "api": "openai-completions",
                                "reasoning": True,
                                "thinkingLevelMap": {"minimal": "low"},
                            }
                        ],
                    }
                }
            }
        ),
        encoding="utf-8",
    )
    (private_home / ".config/opencode/opencode.json").write_text(
        json.dumps(
            {
                "$schema": "https://opencode.ai/config.json",
                "model": f"openai/{opencode_model}",
                "small_model": f"openai/{opencode_model}",
                "autoupdate": False,
                "provider": {
                    "openai": {
                        "options": {"baseURL": openai_base_url},
                        "models": {opencode_model: {"name": opencode_model}},
                    }
                },
            }
        ),
        encoding="utf-8",
    )
    pi_routing = json.loads((private_home / ".pi/agent/models.json").read_text(encoding="utf-8"))
    opencode_routing = json.loads(
        (private_home / ".config/opencode/opencode.json").read_text(encoding="utf-8")
    )
    pi_openai = pi_routing.get("providers", {}).get("openai", {})
    if pi_openai.get("baseUrl") != openai_base_url:
        raise SystemExit(
            f"pi models.json must route providers.openai.baseUrl to {openai_base_url!r}: "
            f"{pi_openai.get('baseUrl')!r}"
        )
    if not any(
        entry.get("id") == pi_model and entry.get("api") == "openai-completions"
        for entry in pi_openai.get("models", [])
    ):
        raise SystemExit(
            f"pi models.json must carry a {pi_model!r} entry with api openai-completions: "
            f"{pi_openai.get('models')!r}"
        )
    opencode_openai = opencode_routing.get("provider", {}).get("openai", {})
    if opencode_openai.get("options", {}).get("baseURL") != openai_base_url:
        raise SystemExit(
            "opencode.json must route provider.openai.options.baseURL to "
            f"{openai_base_url!r}: {opencode_openai.get('options', {}).get('baseURL')!r}"
        )
    if not opencode_routing.get("small_model"):
        raise SystemExit(f"opencode.json must set small_model: {opencode_routing.get('small_model')!r}")
    (private_home / ".local/share/opencode/auth.json").write_text(
        json.dumps({"openai": {"type": "api", "key": api_key}}), encoding="utf-8"
    )
PY
python3 - "$fixture_root/pi-durability.json" "$fixture_root/stage-replay" "$DAEMON_SERVICE_STATE_DIR" \
  "${OWNER_STAGE_REPLAY_HOLDS[@]}" <<'PY'
import json
import pathlib
import sys

template_path, output_root, owner_state_dir, *holds = sys.argv[1:]
template = json.loads(pathlib.Path(template_path).read_text(encoding="utf-8"))
root = pathlib.Path(output_root)
for hold in holds:
    stage_dir = root / hold
    stage_dir.mkdir(parents=True, mode=0o700, exist_ok=True)
    config = dict(template)
    config["state_dir"] = owner_state_dir
    (stage_dir / "config.json").write_text(json.dumps(config, separators=(",", ":")), encoding="utf-8")
PY
python3 - "$fixture_root/codex-containment.json" "$fixture_root/claude-containment.json" \
  "$fixture_root" "$DAEMON_SERVICE_STATE_DIR" <<'PY'
import json
import pathlib
import sys

codex_config_path, claude_config_path, fixture_root, owner_state_dir = sys.argv[1:]
root = pathlib.Path(fixture_root)
for kind, config_path in (("codex", codex_config_path), ("claude", claude_config_path)):
    config = json.loads(pathlib.Path(config_path).read_text(encoding="utf-8"))
    config["state_dir"] = owner_state_dir
    stage_dir = root / f"stage-replay-native-release-{kind}" / "native_release"
    stage_dir.mkdir(parents=True, mode=0o700, exist_ok=True)
    (stage_dir / "config.json").write_text(json.dumps(config, separators=(",", ":")), encoding="utf-8")
PY
python3 - "$fixture_root/pi-durability.json" "$fixture_root/ack-loss" "$DAEMON_SERVICE_STATE_DIR" \
  "${OWNER_ACK_LOSS_STAGES[@]}" <<'PY'
import json
import pathlib
import sys

template_path, output_root, owner_state_dir, *stages = sys.argv[1:]
template = json.loads(pathlib.Path(template_path).read_text(encoding="utf-8"))
root = pathlib.Path(output_root)
for stage in stages:
    stage_dir = root / stage
    stage_dir.mkdir(parents=True, mode=0o700, exist_ok=True)
    config = dict(template)
    config["state_dir"] = owner_state_dir
    (stage_dir / "config.json").write_text(json.dumps(config, separators=(",", ":")), encoding="utf-8")
PY
python3 - "$fixture_root/pi-durability.json" "$fixture_root/negative-root" "$DAEMON_SERVICE_STATE_DIR" \
  "$OWNER_ALTERNATE_CGROUP_MOUNT" \
  "${OWNER_NEGATIVE_CASES[@]}" <<'PY'
import json
import pathlib
import sys

template_path, output_root, owner_state_dir, alternate_cgroup_mount, *cases = sys.argv[1:]
template = json.loads(pathlib.Path(template_path).read_text(encoding="utf-8"))
root = pathlib.Path(output_root)
for case in cases:
    bundle = root / case
    bundle.mkdir(parents=True, mode=0o700, exist_ok=True)
    config = dict(template)
    config["state_dir"] = owner_state_dir
    if case == "alternate_mount":
        config["linux_owner_cgroup_mount"] = alternate_cgroup_mount
    (bundle / "config.json").write_text(json.dumps(config, separators=(",", ":")), encoding="utf-8")
    if case == "stale_journal_run_key":
        (bundle / "unresolved-journal-guard.json").write_text(
            json.dumps(config, separators=(",", ":")), encoding="utf-8"
        )
PY
python3 - "$fixture_root/opencode-health.json" "$fixture_root" "$STATE_ROOT" <<'PY'
import json
import pathlib
import sys

template_path, fixture_root, state_root = sys.argv[1:]
template = json.loads(pathlib.Path(template_path).read_text(encoding="utf-8"))
for name in ("unprovisioned",):
    bundle = pathlib.Path(fixture_root) / name
    bundle.mkdir(parents=True, mode=0o700, exist_ok=True)
    config = dict(template)
    config["state_dir"] = f"{state_root}/{name}"
    (bundle / "config.json").write_text(json.dumps(config, separators=(",", ":")), encoding="utf-8")
PY
daemon_service_template="$fixture_root/pi-durability.json"
case "$fixture_case_set" in
codex_containment) daemon_service_template="$fixture_root/codex-containment.json" ;;
claude_containment) daemon_service_template="$fixture_root/claude-containment.json" ;;
esac
python3 - "$daemon_service_template" "$fixture_root/daemon-service.json" \
  "$DAEMON_SERVICE_STATE_DIR" "$DAEMON_SERVICE_WORKSPACE_ROOT" "$fixture_identity_suffix" <<'PY'
import json
import pathlib
import sys

template_path, output_path, state_dir, workspace_root, fixture_identity_suffix = sys.argv[1:]
config = json.loads(pathlib.Path(template_path).read_text(encoding="utf-8"))
kind = config["runtime"]["harness_kind"]
config["state_dir"] = state_dir
config["machine_name"] = f"symmetry-linux-v2-installed-daemon-service-{fixture_identity_suffix}"
service_workspace = dict(config["workspaces"]["native"])
service_workspace["root"] = workspace_root
config["workspaces"]["daemon-service"] = service_workspace
config["runtime"]["runtime_key"] = f"symmetry-linux-v2-installed-daemon-service-{fixture_identity_suffix}"
config["runtime"]["name"] = f"Installed Linux v2 {kind} daemon service smoke {fixture_identity_suffix}"
config["runtime"]["workspace"] = "daemon-service"
pathlib.Path(output_path).write_text(json.dumps(config, separators=(",", ":")), encoding="utf-8")
PY
for private_file in "${b_native_credential_files[@]}"; do
  sudo install -o "$B_UID" -g "$B_GID" -m 0600 "$fixture_root/b-home${private_file#"$B_HOME"}" "$rootfs$private_file"
done
for private_file in "${b_native_credential_files[@]}"; do
  machine_run_as "$B_USER" "$B_USER" /usr/bin/test -r "$private_file"
  machine_run_as "$B_USER" "$B_USER" /usr/bin/test -w "$private_file"
  if machine_run_as "$A_USER" "$A_USER" /usr/bin/test -r "$private_file"; then
    fail "A unexpectedly read B-private native configuration: $private_file"
  fi
  if machine_run_as "$A_USER" "$A_USER" /usr/bin/test -w "$private_file"; then
    fail "A unexpectedly modified B-private native configuration: $private_file"
  fi
done
for private_directory in "$B_HOME" "$B_HOME/hooks" "${b_native_credential_directories[@]}"; do
  [[ "$(sudo stat -c '%u:%g' "$rootfs$private_directory")" == "$B_UID:$B_GID" ]] ||
    fail "B private HOME directory has unsafe ownership: $private_directory"
done
for private_file in "${b_native_credential_files[@]}"; do
  if ! sudo test -f "$rootfs$private_file" || ! sudo test ! -L "$rootfs$private_file" ||
     [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$private_file")" != "$B_UID:$B_GID:600:1" ]] ||
     ! sudo test -s "$rootfs$private_file"; then
    fail "B private native configuration must be a nonempty singly-linked B-owned 0600 file: $private_file"
  fi
done
if [[ -n "$native_containment_kind" ]]; then
  foreign_native_paths=("$B_HOME/.pi" "$B_HOME/.config" "$B_HOME/.local")
  if [[ "$native_containment_kind" == codex ]]; then
    foreign_native_paths+=("$B_HOME/.claude")
  else
    foreign_native_paths+=("$B_HOME/.codex")
  fi
  for foreign_path in "${foreign_native_paths[@]}"; do
    if sudo test -e "$rootfs$foreign_path"; then
      fail "containment guest B HOME must not carry a non-kind native credential path: $foreign_path"
    fi
  done
  [[ "$(sudo find "$rootfs${b_native_credential_directories[0]}" -mindepth 1 -printf '%P\n')" == "${b_native_credential_files[0]##*/}" ]] ||
    fail "containment guest B HOME credential directory must hold exactly its placeholder file: ${b_native_credential_directories[0]}"
fi
rm -f -- "$native_api_key_file"
rm -rf -- "$fixture_root/b-home"
for config in pi-provider-action.json pi-helper-crash.json pi-daemon-crash.json opencode-health.json opencode-wsl2.json codex-containment.json claude-containment.json; do
  sudo install -o root -g "$A_GID" -m 0640 "$fixture_root/$config" "$rootfs$OWNER_CONFIG_ROOT/$config"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$OWNER_CONFIG_ROOT/$config")" == "0:$A_GID:640:1" ]] ||
    fail "owner-witness config must be singly-linked root:A mode 0640: $config"
done
if [[ "$fixture_case_set" == synthetic_goal_cleanup ]]; then
  sudo install -o root -g "$A_GID" -m 0640 \
    "$fixture_root/synthetic-goal-cleanup.json" \
    "$rootfs$SYNTHETIC_GOAL_CLEANUP_ROOT/config.json"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$SYNTHETIC_GOAL_CLEANUP_ROOT/config.json")" == "0:$A_GID:640:1" ]] ||
    fail 'synthetic cleanup config must be singly-linked root:A mode 0640'
  machine_run_as "$A_USER" "$A_USER" /usr/bin/test -r "$SYNTHETIC_GOAL_CLEANUP_ROOT/config.json"
  if machine_run_as "$B_USER" "$B_USER" /usr/bin/test -r "$SYNTHETIC_GOAL_CLEANUP_ROOT/config.json"; then
    fail 'native B unexpectedly read the root-controlled synthetic cleanup config'
  fi
  machine_run_as "$B_USER" "$B_USER" /usr/bin/bash -c 'test -r "$1"' bash "$SYNTHETIC_GOAL_CLEANUP_ROOT" ||
    fail 'native B cannot read the synthetic cleanup case root the clone seed path sits under'
  machine_run_as "$B_USER" "$B_USER" /usr/bin/bash -c 'test -r "$1"' bash "$SYNTHETIC_GOAL_CLEANUP_SEEDS" ||
    fail 'native B cannot read the synthetic cleanup seed root'
  machine_run_as "$A_USER" "$A_USER" /usr/bin/bash -c '! test -w "$1"' bash "$SYNTHETIC_GOAL_CLEANUP_ROOT" ||
    fail 'native A unexpectedly gained write access to the root-controlled synthetic cleanup case root'
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$SYNTHETIC_GOAL_CLEANUP_ROOT")" == "0:$A_GID:751" ]] ||
    fail 'synthetic cleanup case root must stay root:A mode 0751'
  expected_synthetic_cleanup_acl=$'user::rwx\nuser:'"$B_UID"$':r-x\ngroup::r-x\nmask::r-x\nother::--x'
  [[ "$(sudo getfacl -cpn -- "$rootfs$SYNTHETIC_GOAL_CLEANUP_ROOT")" == "$expected_synthetic_cleanup_acl" ]] ||
    fail 'synthetic cleanup case root must have exactly the fixture B-read ACL'
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$SYNTHETIC_GOAL_CLEANUP_CLONES")" == "$B_UID:$B_GID:750" ]] ||
    fail 'synthetic cleanup clone root must stay B-owned mode 0750'
  expected_synthetic_cleanup_clone_acl=$'user::rwx\nuser:'"$A_UID"$':r-x\ngroup::---\nmask::r-x\nother::---\ndefault:user::rwx\ndefault:user:'"$A_UID"$':r-x\ndefault:group::---\ndefault:mask::r-x\ndefault:other::---'
  [[ "$(sudo getfacl -cpn -- "$rootfs$SYNTHETIC_GOAL_CLEANUP_CLONES")" == "$expected_synthetic_cleanup_clone_acl" ]] ||
    fail 'synthetic cleanup clone root must keep a non-writable default ACL mask so B-created binding/run/generation directories stay non-writable'
fi
for hold_after in "${OWNER_STAGE_REPLAY_HOLDS[@]}"; do
  stage_config="$fixture_root/stage-replay/$hold_after/config.json"
  stage_guest_config="$OWNER_STAGE_REPLAY_ROOT/$hold_after/config.json"
  sudo install -o root -g "$A_GID" -m 0640 "$stage_config" "$rootfs$stage_guest_config"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$stage_guest_config")" == "0:$A_GID:640:1" ]] ||
    fail "stage replay config must be singly-linked root:A mode 0640: $hold_after"
done
for kind in codex claude; do
  stage_config="$fixture_root/stage-replay-native-release-$kind/native_release/config.json"
  stage_guest_config="$OWNER_STAGE_REPLAY_ROOT/native_release_$kind/config.json"
  sudo install -o root -g "$A_GID" -m 0640 "$stage_config" "$rootfs$stage_guest_config"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$stage_guest_config")" == "0:$A_GID:640:1" ]] ||
    fail "stage replay config must be singly-linked root:A mode 0640: native_release_$kind"
done
for ack_loss_stage in "${OWNER_ACK_LOSS_STAGES[@]}"; do
  ack_loss_config="$fixture_root/ack-loss/$ack_loss_stage/config.json"
  ack_loss_guest_config="$OWNER_ACK_LOSS_ROOT/$ack_loss_stage/config.json"
  sudo install -o root -g "$A_GID" -m 0640 "$ack_loss_config" "$rootfs$ack_loss_guest_config"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$ack_loss_guest_config")" == "0:$A_GID:640:1" ]] ||
    fail "ACK-loss config must be singly-linked root:A mode 0640: $ack_loss_stage"
done
for negative_case in "${OWNER_NEGATIVE_CASES[@]}"; do
  negative_config="$fixture_root/negative-root/$negative_case/config.json"
  negative_guest_config="$OWNER_NEGATIVE_ROOT/$negative_case/config.json"
  sudo install -o root -g "$A_GID" -m 0640 "$negative_config" "$rootfs$negative_guest_config"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$negative_guest_config")" == "0:$A_GID:640:1" ]] ||
    fail "negative witness config must be singly-linked root:A mode 0640: $negative_case"
  if [[ "$negative_case" == stale_journal_run_key ]]; then
    sudo install -o root -g "$A_GID" -m 0640 \
      "$fixture_root/negative-root/$negative_case/unresolved-journal-guard.json" \
      "$rootfs$OWNER_NEGATIVE_ROOT/$negative_case/unresolved-journal-guard.json"
    [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$OWNER_NEGATIVE_ROOT/$negative_case/unresolved-journal-guard.json")" == "0:$A_GID:640:1" ]] ||
      fail 'unresolved-journal-guard config must be singly-linked root:A mode 0640'
  fi
done
for bundle_name in unprovisioned; do
  bundle_config="$fixture_root/$bundle_name/config.json"
  bundle_guest_config="$OWNER_TEST_ROOT/$bundle_name/config.json"
  sudo install -o root -g "$A_GID" -m 0640 "$bundle_config" "$rootfs$bundle_guest_config"
  [[ "$(sudo stat -c '%u:%g:%a' "$rootfs$OWNER_TEST_ROOT/$bundle_name")" == "0:$A_GID:750" ]] ||
    fail "$bundle_name bundle directory must be root:A mode 0750"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$bundle_guest_config")" == "0:$A_GID:640:1" ]] ||
    fail "$bundle_name config must be singly-linked root:A mode 0640"
done
sudo install -o root -g root -m 0555 "$build_stage/symmetry-daemon" \
  "$rootfs$OWNER_UNPROVISIONED_ROOT/symmetry-daemon"
[[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$OWNER_UNPROVISIONED_ROOT/symmetry-daemon")" == '0:0:555:1' ]] ||
  fail 'unprovisioned production daemon must be a singly-linked root-owned mode-0555 executable'
sudo chroot "$rootfs" /usr/bin/test -x "$OWNER_UNPROVISIONED_ROOT/symmetry-daemon"
[[ "$(sudo stat -c '%u:%g:%a' "$rootfs$OWNER_NEGATIVE_ROOT")" == "0:$A_GID:750" ]] ||
  fail 'negative witness bundle root must be root:A mode 0750'
sudo chown -R "$A_UID:$A_GID" "$rootfs$SEED_ROOT"
sudo chown "$A_UID:$B_GID" "$rootfs$PI_SHARE"
sudo chmod 2750 "$rootfs$PI_SHARE"
sudo setfacl -m "u:$B_UID:r-x,m::r-x" "$rootfs$SEED_ROOT"
sudo setfacl -m "u:$A_UID:r-x,m::r-x" "$rootfs$B_CLONE_ROOT"
sudo chmod 0750 "$rootfs$A_SOURCE"

# Claim fixture setup/test runs before owner installation.
printf 'Starting isolated production Control for real claim fixtures.\n'
env -u GH_TOKEN -u GITHUB_TOKEN -u SYMMETRY_OPENAI_API_KEY \
  "GH_CONFIG_DIR=$control_gh_config_dir" "$control_bin" start >"$control_log" 2>&1 &
control_pid=$!
control_started=1
printf '%s\n' "$control_pid" > "$fixture_root/control.pid"
for _ in {1..60}; do
  if curl --fail --silent --show-error http://127.0.0.1:4000/healthz >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl --fail --silent --show-error http://127.0.0.1:4000/healthz >/dev/null
control_url=http://127.0.0.1:4000
machine_token_file="$fixture_root/control-machine-token"
machine_registration_file="$fixture_root/control-machine-registration.json"
daemon_service_identity_file="$fixture_root/daemon-service-identity.json"
# Give the installed daemon a separate enrolled Machine so it cannot replace the generic claim-fixture Session.
python3 "$repo/scripts/ci/register-linux-v2-claim-machine.py" \
  "$control_fixture" "$control_url" \
  "linux-v2-claim-fixture-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}-$fixture_identity_suffix" \
  "$machine_token_file" "$machine_registration_file" \
  "symmetry-linux-v2-installed-daemon-service-$fixture_identity_suffix" "$daemon_service_identity_file"
python3 - "$machine_token_file" "$machine_registration_file" \
  "$daemon_service_identity_file" "$evidence_secret_values_file" <<'PY'
import json
import pathlib
import sys

token_path, registration_path, identity_path, values_path = map(pathlib.Path, sys.argv[1:])
token = token_path.read_bytes().strip()
if not token:
    raise SystemExit("machine registration token is empty")
additional = [token]
for path in (registration_path, identity_path):
    value = json.loads(path.read_text(encoding="utf-8"))
    for key, item in value.items():
        if "token" in key.lower() and isinstance(item, str) and item:
            additional.append(item.encode("utf-8"))
with values_path.open("ab") as stream:
    for secret in additional:
        stream.write(secret + b"\0")
PY
sudo install -o "$A_UID" -g "$A_GID" -m 0600 "$daemon_service_identity_file" \
  "$rootfs$DAEMON_SERVICE_STATE_DIR/identity.json"
[[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$DAEMON_SERVICE_STATE_DIR/identity.json")" == "$A_UID:$A_GID:600:1" ]] ||
  fail 'installed daemon service machine identity must be a singly-linked A-owned 0600 file'
rm -f -- "$daemon_service_identity_file"

if [[ "$fixture_case_set" == shared ]]; then
unprovisioned_run_metadata="$fixture_root/run-stage_replay_unprovisioned_refusal.json"
unprovisioned_request_source="$fixture_root/request-stage_replay_unprovisioned_refusal.json"
(
  cd "$repo/control"
  env -u GH_TOKEN -u GITHUB_TOKEN -u SYMMETRY_OPENAI_API_KEY MIX_ENV=prod \
    mix run --no-start ../scripts/ci/provision-linux-v2-control-claims.exs schedule \
      "$control_fixture" stage_replay_unprovisioned_refusal "$unprovisioned_run_metadata"
)
python3 "$repo/scripts/ci/write-linux-v2-owner-request.py" \
  stage_replay_unprovisioned_refusal "$unprovisioned_run_metadata" "$fixture_root/unprovisioned/config.json" \
  "$OWNER_BARRIER_ROOT/unprovisioned/request" "$control_url" "$machine_token_file" \
  "$unprovisioned_request_source"
sudo install -o root -g "$A_GID" -m 0440 "$unprovisioned_request_source" \
  "$rootfs$OWNER_UNPROVISIONED_ROOT/request.json"
if ! sudo test -f "$rootfs$OWNER_UNPROVISIONED_ROOT/request.json" ||
   ! sudo test ! -L "$rootfs$OWNER_UNPROVISIONED_ROOT/request.json" ||
   [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$OWNER_UNPROVISIONED_ROOT/request.json")" != "0:$A_GID:440:1" ]]; then
  fail 'unprovisioned witness request must be a singly-linked root:A 0440 file'
fi
rm -f -- "$unprovisioned_request_source" "$unprovisioned_run_metadata"
machine_run_as "$A_USER" "$A_USER" /usr/bin/test -r "$OWNER_UNPROVISIONED_ROOT/request.json"
if machine_run_as "$B_USER" "$B_USER" /usr/bin/test -r "$OWNER_UNPROVISIONED_ROOT/request.json"; then
  fail 'native B unexpectedly read the unprovisioned owner-witness request'
fi

unprovisioned_output_directory="$A_HOME/ci-fixture/witness-output/unprovisioned"
machine_run_as "$A_USER" "$A_USER" /usr/bin/mkdir -m 0700 -p "$unprovisioned_output_directory"
machine_run_count=$((machine_run_count + 1))
unprovisioned_unit="${machine}-go-test-${machine_run_count}"
unprovisioned_events="$fixture_root/linux-v2-unprovisioned-refusal.json"
unprovisioned_status=0
sudo systemd-run --machine="$machine" --quiet --wait --pipe --collect \
  --unit="$unprovisioned_unit" --uid="$A_USER" --gid="$A_USER" \
  --property="WorkingDirectory=$A_HOME/ci-source/daemon" \
  --setenv="HOME=$A_HOME" \
  --setenv="PATH=$A_HOME/.ci-go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
  --setenv="GOROOT=$A_HOME/.ci-go" \
  --setenv="GOMODCACHE=$A_HOME/.ci-gomod" \
  --setenv="GOCACHE=$A_HOME/.cache/go-build" \
  --setenv="GOTMPDIR=$A_HOME/.cache/tmp" \
  --setenv=CGO_ENABLED=0 --setenv=GOFLAGS=-mod=readonly \
  --setenv=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNPROVISIONED_WITNESS=1 \
  --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNPROVISIONED_ROOT=$OWNER_UNPROVISIONED_ROOT" \
  --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNPROVISIONED_DAEMON=$OWNER_UNPROVISIONED_ROOT/symmetry-daemon" \
  --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT=$NATIVE_HOOK_MARKER_ROOT" \
  --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS_OUTPUT_DIR=$unprovisioned_output_directory" \
  -- "$A_HOME/.ci-go/bin/go" test -count=1 -timeout 900s -json ./e2e \
     -run '^TestProductionLinuxV2UnprovisionedHostRefusal$' \
  >"$unprovisioned_events" 2>&1 || unprovisioned_status=$?
if ! python3 "$repo/scripts/ci/assert-go-test-json.py" \
  "$unprovisioned_events" TestProductionLinuxV2UnprovisionedHostRefusal; then
  printf 'Unprovisioned production witness did not produce exact run/pass/skip=0 events (go test exit=%s).\n' \
    "$unprovisioned_status" >&2
  # The JSON gate reports counts only; the non-JSON lines name the invocation failure.
  grep --invert-match --extended-regexp '^\{' "$unprovisioned_events" | head -n 5 | print_filtered_output_lines || true
  print_witness_output_lines "$unprovisioned_events" TestProductionLinuxV2UnprovisionedHostRefusal
  exit 1
fi
if (( unprovisioned_status != 0 )); then
  fail "unprovisioned production witness exited with status $unprovisioned_status"
fi
machine_run /usr/bin/rm -f -- "$OWNER_UNPROVISIONED_ROOT/request.json"
fi
# The staged image needs a valid read-only drain config before it can be installed.
python3 - "$fixture_root/installer-daemon.json" "$A_HOME" "$A_SOURCE" "$DAEMON_SERVICE_STATE_DIR" <<'PY'
import json
import sys
path, home, repository, state_dir = sys.argv[1:]
value = {
    "control_plane_url": "http://127.0.0.1:4000",
    "allow_insecure_http": True,
    "state_dir": state_dir,
    "machine_name": "symmetry-linux-v2-installer",
    "cleanup_timeout_ms": 120000,
    "agent_profiles": {"legacy": {"command": "/usr/bin/true", "args": [], "input_mode": "goal", "provider_access": False, "interactive": False, "supervisory_control": False, "event_format": "jsonl", "env_allowlist": []}},
    "workspaces": {"legacy": {"policy": "existing_checkout", "path": repository, "repository": "", "root": "", "ref": "", "cleanup": "never"}},
    "runtime": {"runtime_key": "installer", "name": "installer", "capacity": 1, "agent_profile": "legacy", "workspace": "legacy"},
}
with open(path, "w", encoding="utf-8") as stream:
    json.dump(value, stream, separators=(",", ":"))
PY
sudo install -o root -g root -m 0644 "$fixture_root/installer-daemon.json" "$rootfs/etc/symmetry/daemon.json"
sudo install -o root -g root -m 0644 "$repo/scripts/systemd/symmetry-owner.service.in" \
  "$rootfs/root/symmetry-linux-owner-stage/symmetry-owner.service.in"
sudo install -o root -g root -m 0644 "$repo/scripts/systemd/symmetry-daemon.service.in" \
  "$rootfs/root/symmetry-linux-owner-stage/symmetry-daemon.service.in"

# The installer loads its policy through the guest's kernel security filesystem: never remount an
# already present mount (EBUSY aborts the fixture); the glob keeps the module name from the policy census.
userns_restriction=$(machine_run /bin/sh -c 'cat /proc/sys/kernel/*_restrict_unprivileged_userns') ||
  fail 'cannot read the AppArmor unprivileged user-namespace restriction for this acceptance'
userns_restriction=$(printf '%s' "$userns_restriction" | /usr/bin/tr -d '[:space:]')
[[ "$userns_restriction" == 1 ]] ||
  fail "the AppArmor unprivileged user-namespace restriction must be 1 for this acceptance (read $userns_restriction)"
if [ "$(machine_run /usr/bin/findmnt --noheadings --output TARGET --target /sys/kernel/security)" != "/sys/kernel/security" ]; then
  machine_run /usr/bin/mount -t securityfs securityfs /sys/kernel/security ||
    fail 'mount the guest kernel security filesystem for the installed userns exception'
fi

printf 'Installing production owner artifacts inside the disposable systemd guest.\n'
installer_log="$fixture_root/install-linux-owner.log"
installer_status=0
machine_run /root/install-linux-owner.sh \
  "$A_USER" "$B_USER" \
  /root/symmetry-linux-owner-stage/symmetry-linux-launcher \
  /root/symmetry-linux-owner-stage/symmetry-linux-launcher-core \
  /root/symmetry-linux-owner-stage/symmetry-linux-gate \
  /root/symmetry-linux-owner-stage/symmetry-linux-clone-bootstrap \
  /root/symmetry-linux-owner-stage/symmetry-daemon \
  /root/symmetry-linux-owner-stage/symmetry-owner.service.in \
  /root/symmetry-linux-owner-stage/symmetry-daemon.service.in >"$installer_log" 2>&1 || installer_status=$?
print_filtered_output_lines <"$installer_log"
(( installer_status == 0 )) ||
  fail "install-linux-owner.sh did not install the staged owner artifacts (status=$installer_status)"
grep --quiet --extended-regexp 'userns=per_program$' "$installer_log" || {
  printf 'The in-guest installer did not report the loaded userns exception (expected userns=per_program).\n' >&2
  print_filtered_output_lines <"$installer_log"
  exit 1
}
machine_run /usr/bin/systemctl enable symmetry-owner.service
machine_run /usr/bin/systemctl start symmetry-owner.service
machine_run /usr/bin/systemctl is-active --quiet symmetry-owner.service
machine_run /usr/bin/test -S /run/symmetry-owner/owner.sock
sudo install -o root -g root -m 0644 "$fixture_root/daemon-service.json" \
  "$rootfs/etc/symmetry/daemon.json"
[[ "$(sudo stat -c '%u:%g:%a' "$rootfs/etc/symmetry/daemon.json")" == '0:0:644' ]] ||
  fail 'installed daemon service config must be root-owned mode 0644'
daemon_verify_log="$fixture_root/daemon-verify-linux-owner-install.log"
daemon_verify_status=0
machine_run_as "$A_USER" "$A_USER" /usr/libexec/symmetry/symmetry-daemon \
  -verify-linux-owner-install -config /etc/symmetry/daemon.json >"$daemon_verify_log" 2>&1 ||
  daemon_verify_status=$?
print_filtered_output_lines <"$daemon_verify_log"
(( daemon_verify_status == 0 )) ||
  fail 'installed daemon -verify-linux-owner-install refused the staged daemon config'
grep --quiet --extended-regexp 'userns=per_program$' "$daemon_verify_log" || {
  printf 'The installed daemon preflight did not report the loaded userns exception (expected userns=per_program).\n' >&2
  print_filtered_output_lines <"$daemon_verify_log"
  exit 1
}
# The preflight package's AppArmor rows run as root against the guest's real policy tree, so the
# root identity arms execute in CI; each named row must run and pass exactly once, never skip.
userns_preflight_events="$fixture_root/userns-preflight-events.json"
userns_preflight_status=0
machine_run_count=$((machine_run_count + 1))
sudo systemd-run --machine="$machine" --quiet --wait --pipe --collect \
  --unit="${machine}-userns-preflight-${machine_run_count}" \
  --property="WorkingDirectory=$A_SOURCE/daemon" \
  --setenv="HOME=/root" \
  --setenv="PATH=$A_HOME/.ci-go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
  --setenv="GOROOT=$A_HOME/.ci-go" \
  --setenv="GOMODCACHE=$A_HOME/.ci-gomod" \
  --setenv="GOCACHE=/root/.cache/go-build" \
  --setenv="GOTMPDIR=/root/.cache/tmp" \
  --setenv=CGO_ENABLED=0 \
  --setenv=GOFLAGS=-mod=readonly \
  -- "$A_HOME/.ci-go/bin/go" test -count=1 -timeout 600s -json ./internal/linuxpreflight \
  -run '^TestLinuxAppArmor' >"$userns_preflight_events" 2>&1 || userns_preflight_status=$?
if ! python3 "$repo/scripts/ci/assert-go-test-json.py" "$userns_preflight_events" \
  TestLinuxAppArmorUsernsDecisionRows \
  TestLinuxAppArmorProfileTextMatchesTheInstaller \
  TestLinuxAppArmorLoadedProfileScan \
  TestLinuxAppArmorHostStateRows \
  TestLinuxAppArmorCompileRows \
  TestLinuxAppArmorAttachmentSensitivePolicyHash \
  TestLinuxAppArmorDecisionResetsPerVerify; then
  printf 'The root-identity AppArmor preflight rows did not run and pass exactly once inside the guest (go test exit=%s).\n' \
    "$userns_preflight_status" >&2
  grep --invert-match --extended-regexp '^\{' "$userns_preflight_events" | head -n 5 | print_filtered_output_lines || true
  exit 1
fi
(( userns_preflight_status == 0 )) ||
  fail "the in-guest AppArmor preflight rows exited with status $userns_preflight_status"
daemon_service_start_status=0
if machine_run /usr/bin/systemctl start symmetry-daemon.service; then
  daemon_service_start_status=0
else
  daemon_service_start_status=$?
fi
if (( daemon_service_start_status != 0 )); then
  daemon_failure_log="$fixture_root/daemon-service-failure.log"
  machine_run /usr/bin/systemctl show --no-pager \
    --property=Result,ExecMainStatus,ExecMainCode,ActiveState,SubState,ControlGroup,NRestarts,After,BindsTo,ExecStartPre,ExecStart \
    symmetry-daemon.service >"$daemon_failure_log" 2>&1 || true
  print_filtered_output_lines <"$daemon_failure_log"
  # systemd materializes the inverse of a unit's ordering edge on the other unit, so the
  # owner's effective edges name the source of any owner/daemon relation the preflight rejects.
  machine_run /usr/bin/systemctl show --no-pager \
    --property=Before,After,BindsTo,Requires,Requisite,Wants,PartOf,Conflicts \
    symmetry-owner.service >"$daemon_failure_log" 2>&1 || true
  print_filtered_output_lines <"$daemon_failure_log"
  machine_run /usr/bin/journalctl --no-pager --output=cat -n 80 \
    --grep='-verify-linux-owner-install:|load config:|run daemon:|probe runtime harness:|build runtime registration:|register daemon session:|registration did not return|enroll machine:|load machine identity:|Failed at step|Main process exited|Failed to start|Start request repeated' \
    -u symmetry-daemon.service >"$daemon_failure_log" 2>&1 || true
  print_filtered_output_lines <"$daemon_failure_log"
  fail "installed daemon.service could not pass ExecStartPre/start (status=$daemon_service_start_status)"
fi
sleep 6
machine_run /usr/bin/systemctl is-active --quiet symmetry-daemon.service
daemon_service_state=$(machine_run /usr/bin/systemctl show --property=ActiveState --value symmetry-daemon.service)
daemon_service_main_pid=$(machine_run /usr/bin/systemctl show --property=MainPID --value symmetry-daemon.service)
daemon_service_restarts=$(machine_run /usr/bin/systemctl show --property=NRestarts --value symmetry-daemon.service)
[[ "$daemon_service_state" == active && "$daemon_service_main_pid" =~ ^[1-9][0-9]*$ && "$daemon_service_restarts" == 0 ]] ||
  fail 'installed daemon.service did not remain active with a real MainPID and successful Control enrollment'
machine_run_as "$A_USER" "$A_USER" /usr/bin/test -s "$DAEMON_SERVICE_STATE_DIR/identity.json"
printf 'Stopping installed daemon.service before direct witnesses to release its exclusive StateDir Store lock; owner.service remains active.\n'
if [[ -n "$(machine_run /usr/bin/find "$DAEMON_SERVICE_STATE_DIR/runs" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
  fail 'installed daemon.service smoke unexpectedly created a RunJournal before its direct-witness shutdown'
fi
machine_run /usr/bin/systemctl stop symmetry-daemon.service
daemon_service_stop_state=$(machine_run /usr/bin/systemctl show --property=ActiveState --value symmetry-daemon.service)
daemon_service_stop_pid=$(machine_run /usr/bin/systemctl show --property=MainPID --value symmetry-daemon.service)
daemon_service_stop_result=$(machine_run /usr/bin/systemctl show --property=Result --value symmetry-daemon.service)
[[ "$daemon_service_stop_state" == inactive && "$daemon_service_stop_pid" == 0 && "$daemon_service_stop_result" == success ]] ||
  fail "installed daemon.service did not stop cleanly: state=$daemon_service_stop_state pid=$daemon_service_stop_pid result=$daemon_service_stop_result"
machine_run /usr/bin/systemctl is-active --quiet symmetry-owner.service
machine_run /usr/bin/flock --nonblock "$DAEMON_SERVICE_STATE_DIR/.symmetry-daemon.lock" /usr/bin/true
owner_leaf_parent=/sys/fs/cgroup/system.slice/symmetry-owner.service/manager
OWNER_TEST_LEAF="$owner_leaf_parent/symmetry-owner-test-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}-$fixture_identity_suffix"
machine_run_as "$A_USER" "$A_USER" /usr/bin/mkdir "$OWNER_TEST_LEAF"
machine_run /usr/bin/mount --bind /sys/fs/cgroup "$OWNER_ALTERNATE_CGROUP_MOUNT"
machine_run /usr/bin/test -f "$OWNER_ALTERNATE_CGROUP_MOUNT/cgroup.controllers"
machine_run /bin/bash -c \
  'test "$(/usr/bin/findmnt --noheadings --output ID --target /sys/fs/cgroup)" != "$(/usr/bin/findmnt --noheadings --output ID --target "$1")"' \
  bash "$OWNER_ALTERNATE_CGROUP_MOUNT"

# A failed witness is named from its document, journals, markers and configs, and from the
# bounded private stderr log the e2e keeps beside its witnesses.
print_witness_failure_context() {
  local output_directory=$1
  shift
  local name document
  local probe_log="$fixture_root/witness-config-probe.log"
  if [[ -n "$output_directory" ]]; then
    for name in "$@"; do
      document="$rootfs$output_directory/${name//\//_}.json"
      print_witness_document "$document"
    done
    print_witness_stderr_tail "$rootfs$output_directory/owner-witness.stderr"
  fi
  # A witness that stalls before its requested boundary leaves its progress only in the
  # journal, the stage markers and the owner service journal; print all three, redacted.
  printf 'owner witness RunJournal progress (secret-free projection):\n' >&2
  sudo bash -c '
for file in "$1"/runs/journal-*.json; do
  [ -f "$file" ] || continue
  printf "%s " "${file##*/}"
  jq -c "{local_state, has_claim_grant: (.lease_token != null), has_clone_intent: (.linux_clone_intent != null), has_seed_receipt: (.linux_clone_seed_receipt != null), recovery_required: (.workspace_recovery_required // false)}" "$file" 2>/dev/null ||
    printf "%s\n" "{unreadable}"
done' bash "$rootfs$DAEMON_SERVICE_STATE_DIR" >&2 || true
  printf 'owner witness stage markers (type mode size name):\n' >&2
  sudo find "$rootfs$OWNER_BARRIER_ROOT" -mindepth 1 -maxdepth 3 -printf '%y %m %s %P\n' 2>/dev/null |
    sort | head -n 60 >&2 || true
  printf 'A-owned Git source metadata created by the witness (type mode owner path):\n' >&2
  machine_run_as "$A_USER" "$A_USER" /usr/bin/find "$A_SOURCE/.git/refs" "$A_SOURCE/.git/logs" \
    -mindepth 1 -maxdepth 4 -printf '%y %m %u:%g %P\n' 2>/dev/null | sort | head -n 40 >&2 || true
  printf 'owner service journal (bounded, filtered):\n' >&2
  machine_run /usr/bin/journalctl --no-pager --output=cat -n 40 -u symmetry-owner.service 2>&1 |
    print_filtered_output_lines || true
  # The staged native tools run as A, so a permission fault names itself only from the guest view.
  printf 'guest view of the staged native tools (as A):\n' >&2
  machine_run_as "$A_USER" "$A_USER" /usr/bin/ls -ld \
    /usr/local /usr/local/lib /usr/local/lib/symmetry-pi /usr/local/bin 2>&1 || true
  machine_run_as "$A_USER" "$A_USER" /usr/bin/stat -c '%F %U:%G %a %h %n' \
    /usr/local/lib/symmetry-pi/pi /usr/local/bin/opencode 2>&1 || true
  # Names and sizes only: journals and staged requests can hold claim credentials.
  printf 'daemon StateDir inventory (names and sizes):\n' >&2
  sudo find "$rootfs$DAEMON_SERVICE_STATE_DIR" -mindepth 1 -maxdepth 3 -printf '%y %P %s\n' 2>/dev/null |
    sort | head -n 40 >&2 || true
  printf 'owner witness fixture inventory (names and sizes):\n' >&2
  sudo find "$rootfs$OWNER_TEST_ROOT" -mindepth 1 -maxdepth 4 -type f -printf '%P %s\n' 2>/dev/null |
    sort | head -n 80 >&2 || true
  # Witness configs are staged root:A mode 0640; the 0440 request files are excluded.
  local -a witness_configs=()
  mapfile -t witness_configs < <(
    sudo find "$rootfs$OWNER_TEST_ROOT" -maxdepth 3 -type f -name '*.json' -perm -0640 2>/dev/null |
      sort | head -n 20
  )
  local guest_config
  for guest_config in "${witness_configs[@]}"; do
    [[ -n "$guest_config" ]] || continue
    guest_config=${guest_config#"$rootfs"}
    printf 'witness config owner preflight %s:\n' "$guest_config" >&2
    machine_run_as "$A_USER" "$A_USER" /usr/libexec/symmetry/symmetry-daemon \
      -verify-linux-owner-install -config "$guest_config" >"$probe_log" 2>&1 || true
    print_filtered_output_lines <"$probe_log"
    # The witness gate runs before any mutation and before the request is read, so a missing
    # request path names the config refusal without touching the barrier or StateDir.
    printf 'witness config native gate %s:\n' "$guest_config" >&2
    machine_run_as "$A_USER" "$A_USER" /usr/bin/env SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 \
      /usr/libexec/symmetry/symmetry-daemon -linux-v2-native-owner-witness \
      -config "$guest_config" -request "$OWNER_TEST_ROOT/missing-witness-gate-probe.json" >"$probe_log" 2>&1 || true
    print_filtered_output_lines <"$probe_log"
  done
}

# inside A's mode-0700 HOME; B cannot inspect A's checkout or test environment.
run_go_test() {
  local event_file=$1
  local go_package=$2
  local test_pattern=$3
  local test_mode=$4
  local request_environment=$5
  local request_path=$6
  local output_directory=$7
  shift 7
  [[ "${1:-}" == -- ]] || fail 'each exact Go witness invocation requires -- before its named test events'
  shift
  local -a expected_tests=("$@")
  (( ${#expected_tests[@]} > 0 )) || fail 'each exact Go witness invocation requires named test events'
  case "$test_mode" in
    pi-baseline|opencode-baseline|scratch|owner-fd-scratch|cleanup-root|cleanup-mounts-root|production|stage-replay|negative|synthetic-cleanup|peer-negative|legacy-v1|marker-reader) ;;
    *) fail "unknown Linux v2 acceptance Go test mode: $test_mode" ;;
  esac
  if [[ "$test_mode" == production ]]; then
    [[ -n "$request_environment" && -n "$request_path" && -n "$output_directory" ]] ||
      fail 'each production owner witness requires its exact request and private output directory'
  elif [[ "$test_mode" == stage-replay || "$test_mode" == negative ]]; then
    [[ -z "$request_environment" && -z "$request_path" && -n "$output_directory" ]] ||
      fail 'bundle-based witnesses require their root and private output directory, not a single request'
  elif [[ "$test_mode" == synthetic-cleanup ]]; then
    [[ -z "$request_environment" && -z "$request_path" && -n "$output_directory" ]] ||
      fail 'synthetic cleanup requires its dedicated root and private output directory, not a Control request'
  elif [[ "$test_mode" == peer-negative ]]; then
    [[ -z "$request_environment" && -z "$request_path" && -z "$output_directory" ]] ||
      fail 'in-package peer-negative tests must not receive owner requests, output paths, or Control claims'
  else
    [[ -z "$request_environment" && -z "$request_path" && -z "$output_directory" ]] ||
      fail 'non-production witness modes must not receive owner requests or output directories'
  fi
  machine_run_count=$((machine_run_count + 1))
  local unit="${machine}-go-test-${machine_run_count}"
  local status=0
  local test_home=$A_HOME
  local test_gocache=$A_HOME/.cache/go-build
  local test_gotmp=$A_HOME/.cache/tmp
  local -a identity_args=(--uid="$A_USER" --gid="$A_USER")
  if [[ "$test_mode" == cleanup-root || "$test_mode" == cleanup-mounts-root ]]; then
    test_home=/root
    test_gocache=/root/.cache/go-build
    test_gotmp=/root/.cache/tmp
    identity_args=()
  fi
  local -a command=(
    sudo systemd-run --machine="$machine" --quiet --wait --pipe --collect
    --unit="$unit" "${identity_args[@]}"
    --property="WorkingDirectory=$A_HOME/ci-source/daemon"
    --setenv="HOME=$test_home"
    --setenv="PATH=$A_HOME/.ci-go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    --setenv="GOROOT=$A_HOME/.ci-go"
    --setenv="GOMODCACHE=$A_HOME/.ci-gomod"
    --setenv="GOCACHE=$test_gocache"
    --setenv="GOTMPDIR=$test_gotmp"
    --setenv=CGO_ENABLED=0
    --setenv=GOFLAGS=-mod=readonly
    --setenv="SYMMETRY_LINUX_OWNER_READBACK_CHANNEL=$READBACK_CHANNEL"
  )
  # The bundle-based witnesses stand in for the installed daemon, whose units set UMask=0077;
  # without it the clone seed's new A-owned refs are B-accessible and its own revalidation refuses.
  case "$test_mode" in
    stage-replay|negative|production) command+=(--property=UMask=0077) ;;
  esac
  case "$test_mode" in
    pi-baseline)
      command+=(
        --setenv=SYMMETRY_PI_PROVIDER_ACTION_SMOKE=1
        --setenv=SYMMETRY_PI_NATIVE_SMOKE_EXECUTABLE=/usr/local/lib/symmetry-pi/pi
      )
      ;;
    opencode-baseline)
      command+=(
        --setenv=SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL=1
        --setenv=SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL_EXECUTABLE=/usr/local/bin/opencode
      )
      ;;
    scratch)
      command+=(
        --setenv=SYMMETRY_LINUX_OWNER_TEST_SCRATCH=1
        --setenv="SYMMETRY_LINUX_OWNER_TEST_LEAF=$OWNER_TEST_LEAF"
        --setenv=SYMMETRY_LINUX_CLONE_TEST_SCRATCH=1
        --setenv="SYMMETRY_LINUX_CLONE_TEST_ROOT=$CLONE_TEST_ROOT"
        --setenv="SYMMETRY_LINUX_CLONE_TEST_SEED_ROOT=$CLONE_TEST_SEED_ROOT"
        --setenv="SYMMETRY_LINUX_CLONE_TEST_SOURCE_ROOT=$CLONE_TEST_SOURCE_ROOT"
        --setenv="SYMMETRY_LINUX_CLONE_TEST_B_HOME=$CLONE_TEST_B_HOME"
        --setenv="SYMMETRY_LINUX_CLONE_TEST_ACK_ROOT=$CLONE_TEST_ACK_ROOT"
        --setenv="SYMMETRY_LINUX_CLONE_TEST_MARKER=$CLONE_TEST_MARKER"
        --setenv="SYMMETRY_LINUX_OWNER_CLEANUP_ROOT=$CLONE_TEST_ROOT"
        --setenv="SYMMETRY_LINUX_OWNER_CLEANUP_SIBLING=$CLONE_TEST_CLEANUP_SIBLING"
        --setenv="SYMMETRY_LINUX_OWNER_CLEANUP_PARENT_DEATH_TARGET=$CLONE_TEST_CLEANUP_PARENT_DEATH_TARGET"
      )
      ;;
    owner-fd-scratch)
      command+=(
        --setenv=SYMMETRY_LINUX_OWNER_TEST_SCRATCH=1
        --setenv="SYMMETRY_LINUX_OWNER_TEST_LEAF=$OWNER_TEST_LEAF"
      )
      ;;
    cleanup-root)
      command+=(--setenv=SYMMETRY_LINUX_OWNER_TEST_SCRATCH=1)
      ;;
    cleanup-mounts-root)
      command+=(--setenv=SYMMETRY_LINUX_OWNER_TEST_MOUNTS=1)
      ;;
    production)
      machine_run_as "$A_USER" "$A_USER" /usr/bin/mkdir -m 0700 -p "$output_directory"
      command+=(
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT=$NATIVE_HOOK_MARKER_ROOT"
        --setenv=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_CONFIG=$OWNER_CONFIG_ROOT/pi-provider-action.json"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_CRASH_CONFIG=$OWNER_CONFIG_ROOT/pi-helper-crash.json"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_DAEMON_CRASH_CONFIG=$OWNER_CONFIG_ROOT/pi-daemon-crash.json"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_OPENCODE_CONFIG=$OWNER_CONFIG_ROOT/opencode-health.json"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_CONFIG=$OWNER_CONFIG_ROOT/opencode-wsl2.json"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_CODEX_CONFIG=$OWNER_CONFIG_ROOT/codex-containment.json"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_CLAUDE_CONFIG=$OWNER_CONFIG_ROOT/claude-containment.json"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_CONTROL_RECEIPT_ROOT=$OWNER_CONTROL_RECEIPT_ROOT"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_CONTROL_RECEIPT_CONTEXT_ROOT=$OWNER_CONTROL_RECEIPT_CONTEXT_ROOT"
        "--setenv=$request_environment=$request_path"
        "--setenv=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS_OUTPUT_DIR=$output_directory"
      )
      if [[ -n "${codex_executable_sha:-}" ]]; then
        command+=(
          --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_CODEX_BINARY_SHA256=sha256:$codex_executable_sha"
        )
      fi
      if [[ -n "${claude_executable_sha:-}" ]]; then
        command+=(
          --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_CLAUDE_BINARY_SHA256=sha256:$claude_executable_sha"
        )
      fi
      ;;
    stage-replay)
      machine_run_as "$A_USER" "$A_USER" /usr/bin/mkdir -m 0700 -p "$output_directory"
      command+=(
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT=$NATIVE_HOOK_MARKER_ROOT"
        --setenv=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_STAGE_REPLAY_ROOT=$OWNER_STAGE_REPLAY_ROOT"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_ACK_LOSS_ROOT=$OWNER_ACK_LOSS_ROOT"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS_OUTPUT_DIR=$output_directory"
      )
      ;;
    negative)
      machine_run_as "$A_USER" "$A_USER" /usr/bin/mkdir -m 0700 -p "$output_directory"
      command+=(
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT=$NATIVE_HOOK_MARKER_ROOT"
        --setenv=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_NEGATIVE_ROOT=$OWNER_NEGATIVE_ROOT"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNRESOLVED_JOURNAL_GUARD_FRESH_REQUEST=$OWNER_NEGATIVE_ROOT/stale_journal_run_key/fresh_request.json"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS_OUTPUT_DIR=$output_directory"
      )
      ;;
    synthetic-cleanup)
      machine_run_as "$A_USER" "$A_USER" /usr/bin/mkdir -m 0700 -p "$output_directory"
      command+=(
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_SYNTHETIC_GOAL_CLEANUP_ROOT=$SYNTHETIC_GOAL_CLEANUP_ROOT"
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS_OUTPUT_DIR=$output_directory"
      )
      ;;
    peer-negative)
      command+=(--setenv=SYMMETRY_LINUX_V2_ADAPTER_PEER_HOST_GATE=1)
      ;;
    legacy-v1)
      command+=(
        --setenv=SYMMETRY_E2E=1
        --setenv=SYMMETRY_E2E_URL=http://127.0.0.1:4000
        --setenv=SYMMETRY_ENROLLMENT_TOKEN=development-enrollment-token
        --setenv=SYMMETRY_OPERATOR_TOKEN=development-operator-token
        --setenv=SYMMETRY_PRODUCTION_LINUX_V2_LEGACY_V1_REPLAY_WITNESS=1
      )
      ;;
    marker-reader)
      command+=(
        --setenv=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1
        --setenv="SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT=$NATIVE_HOOK_MARKER_ROOT"
      )
      ;;
  esac
  command+=(-- "$A_HOME/.ci-go/bin/go" test -count=1 -timeout 900s -json "$go_package" -run "$test_pattern")
  "${command[@]}" >"$event_file" 2>&1 || status=$?
  if ! python3 "$repo/scripts/ci/assert-go-test-json.py" "$event_file" "${expected_tests[@]}"; then
    printf 'Required native Go witness did not produce exact run/pass/skip=0 events (go test exit=%s).\n' "$status" >&2
    # The JSON gate reports counts only; the non-JSON lines name the invocation failure.
    grep --invert-match --extended-regexp '^\{' "$event_file" | head -n 5 | print_filtered_output_lines || true
    print_witness_output_lines "$event_file" "${expected_tests[@]}"
    print_witness_failure_context "$output_directory" "${expected_tests[@]}"
    return 1
  fi
  if (( status != 0 )); then
    printf 'Required native Go witness exited with status %s.\n' "$status" >&2
    print_witness_output_lines "$event_file" "${expected_tests[@]}"
    print_witness_failure_context "$output_directory" "${expected_tests[@]}"
    return "$status"
  fi
}

run_unexpected_cgroup_procs_fd_witness() {
  local event_file=$1
  local test_name=TestInstalledLinuxOwnerRejectsUnexpectedCgroupProcsFD
  machine_run /usr/bin/test -d "$OWNER_TEST_LEAF"
  machine_run /usr/bin/test ! -s "$OWNER_TEST_LEAF/cgroup.procs"
  machine_run /usr/bin/grep --fixed-strings --line-regexp --quiet \
    'populated 0' "$OWNER_TEST_LEAF/cgroup.events"

  run_go_test "$event_file" ./native/linux-gate/protocol "^${test_name}$" \
    owner-fd-scratch '' '' '' -- "$test_name"

  machine_run /usr/bin/test ! -s "$OWNER_TEST_LEAF/cgroup.procs"
  machine_run /usr/bin/grep --fixed-strings --line-regexp --quiet \
    'populated 0' "$OWNER_TEST_LEAF/cgroup.events"
}

publish_control_provider_receipt() {
  local source=$1
  local case_name=$2
  local run_id=$3
  local target_directory="$OWNER_CONTROL_RECEIPT_ROOT/$case_name"
  local target="$target_directory/$run_id.json"
  sudo test ! -e "$rootfs$target" || fail "Control receipt target already exists for $case_name"
  sudo python3 "$repo/scripts/ci/publish-linux-v2-control-receipt.py" \
    "$source" "$rootfs$target_directory" "$run_id.json" "$A_UID" "$A_GID"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$target")" == "0:$A_GID:440:1" ]] ||
    fail "published Control receipt must be root:A mode 0440 singly-linked: $case_name"
}

start_literal_pid_reuse_watcher() {
  # Probe clone3(set_tid) in the guest PID namespace; never write ns_last_pid,
  # remount procfs, retry the target PID, or substitute numeric-PID observation.
  local watcher_log="$fixture_root/literal-pid-reuse-watcher.log"
  local watcher_status=0
  machine_run /usr/bin/systemd-run --no-block --quiet --collect \
    --unit="$PID_REUSE_WATCHER_UNIT" --service-type=exec \
    --property=Restart=no --property=KillMode=control-group --property=TimeoutStopSec=30s \
    -- "$OWNER_PID_REUSE_HELPER" --watch --directory "$OWNER_PID_REUSE_ROOT" \
    --host-pidns-inode "$host_pid_namespace_inode" --a-gid "$A_GID"

  machine_run /bin/bash -c '
    set -Eeuo pipefail
    unit=$1
    directory=$2
    helper=$3
    gid=$4
    owner_manager=/system.slice/symmetry-owner.service/manager
    for _ in {1..300}; do
      state=$(/usr/bin/systemctl show --property=ActiveState --value "$unit" 2>/dev/null || true)
      if [[ "$state" == failed ]]; then
        /usr/bin/journalctl --no-pager --output=cat --grep='literal-pid-reuse-helper:|^probe=passed' -n 20 -u "$unit" >&2 || true
        exit 1
      fi
      main_pid=$(/usr/bin/systemctl show --property=MainPID --value "$unit" 2>/dev/null || true)
      ready="$directory/ready"
      if [[ "$state" == active && "$main_pid" =~ ^[1-9][0-9]*$ && ! -L "$ready" && -f "$ready" ]]; then
        [[ "$(/usr/bin/stat -c %u:%g:%a:%h "$ready")" == "0:$gid:440:1" ]]
        /usr/bin/cmp --silent "$ready" <(printf "ready\n")
        helper_identity=$(/usr/bin/stat -Lc %d:%i:%u:%g:%a "$helper")
        main_identity=$(/usr/bin/stat -Lc %d:%i:%u:%g:%a "/proc/$main_pid/exe")
        [[ "$main_identity" == "$helper_identity" ]]
        control_group=$(/usr/bin/systemctl show --property=ControlGroup --value "$unit")
        [[ "$control_group" != "$owner_manager" && "$control_group" != "$owner_manager/"* ]]
        printf "literal PID-reuse watcher ready: unit=%s MainPID=%s cgroup=%s\n" "$unit" "$main_pid" "$control_group"
        exit 0
      fi
      /usr/bin/sleep 0.1
    done
    /usr/bin/journalctl --no-pager --output=cat --grep='literal-pid-reuse-helper:|^probe=passed' -n 20 -u "$unit" >&2 || true
    printf "literal PID-reuse watcher did not become ready within 30 seconds\n" >&2
    exit 1
  ' bash "$PID_REUSE_WATCHER_UNIT" "$OWNER_PID_REUSE_ROOT" \
    "$OWNER_PID_REUSE_HELPER" "$A_GID" >"$watcher_log" 2>&1 || watcher_status=$?
  print_filtered_output_lines <"$watcher_log"
  (( watcher_status == 0 )) || fail 'the literal PID-reuse watcher did not become ready'
}

# The root readback reader serves A's converted assertions without granting A
# any capability: only B-credentialed members of the operator leaf are read.
start_readback_reader() {
  [[ "$owner_leaf_parent" == "$READBACK_LEAF_ROOT" ]] ||
    fail 'the readback leaf root must equal the installed owner manager cgroup'
  local reader_log="$fixture_root/readback-reader.log"
  local capability_log="$fixture_root/readback-reader-a-capabilities.log"
  local reader_status=0
  machine_run /usr/bin/mkdir -m 0770 "$READBACK_CHANNEL"
  machine_run /usr/bin/chown root:"$A_GID" "$READBACK_CHANNEL"
  machine_run /usr/bin/bash -c \
    'test "$(/usr/bin/stat -c %u:%g:%a "$1")" = "0:$2:770"' \
    bash "$READBACK_CHANNEL" "$A_GID" ||
    fail 'the readback channel must be a root:A mode-0770 directory'
  machine_run_as "$A_USER" "$A_GID" /usr/bin/bash -c 'test -w "$1"' bash "$READBACK_CHANNEL" ||
    fail 'A cannot write the readback channel it must request through'
  # A diagnostic row for the census: A's effective capability set decides which
  # ptrace-gated reads a guest happens to allow; the converted legs never use it.
  machine_run_as "$A_USER" "$A_GID" /usr/bin/grep --extended-regexp \
    '^(NoNewPrivs|CapEff|CapPrm|CapBnd|CapAmb):' /proc/self/status \
    >"$capability_log" 2>&1 || fail 'the A capability readback row did not run'
  print_filtered_output_lines <"$capability_log"

  machine_run /usr/bin/systemd-run --no-block --quiet \
    --unit="$READBACK_READER_UNIT" --service-type=exec \
    --property=Restart=no --property=KillMode=control-group --property=TimeoutStopSec=30s \
    -- "$READBACK_READER" serve \
    --channel "$READBACK_CHANNEL" --leaf-root "$READBACK_LEAF_ROOT" \
    --a-uid "$A_UID" --a-gid "$A_GID" --b-uid "$B_UID" --b-gid "$B_GID"

  machine_run /bin/bash -c '
    set -Eeuo pipefail
    unit=$1
    channel=$2
    helper=$3
    gid=$4
    for _ in {1..300}; do
      state=$(/usr/bin/systemctl show --property=ActiveState --value "$unit" 2>/dev/null || true)
      if [[ "$state" == failed ]]; then
        /usr/bin/journalctl --no-pager --output=cat --grep="readback-reader:" -n 20 -u "$unit" >&2 || true
        exit 1
      fi
      main_pid=$(/usr/bin/systemctl show --property=MainPID --value "$unit" 2>/dev/null || true)
      ready="$channel/ready"
      if [[ "$state" == active && "$main_pid" =~ ^[1-9][0-9]*$ && ! -L "$ready" && -f "$ready" ]]; then
        [[ "$(/usr/bin/stat -c %u:%g:%a:%h "$ready")" == "0:$gid:440:1" ]]
        /usr/bin/cmp --silent "$ready" <(printf "ready\n")
        helper_identity=$(/usr/bin/stat -Lc %d:%i:%u:%g:%a "$helper")
        main_identity=$(/usr/bin/stat -Lc %d:%i:%u:%g:%a "/proc/$main_pid/exe")
        [[ "$main_identity" == "$helper_identity" ]]
        printf "root readback reader ready: unit=%s MainPID=%s\n" "$unit" "$main_pid"
        exit 0
      fi
      /usr/bin/sleep 0.1
    done
    /usr/bin/journalctl --no-pager --output=cat --grep="readback-reader:" -n 20 -u "$unit" >&2 || true
    printf "root readback reader did not become ready within 30 seconds\n" >&2
    exit 1
  ' bash "$READBACK_READER_UNIT" "$READBACK_CHANNEL" "$READBACK_READER" "$A_GID" \
    >"$reader_log" 2>&1 || reader_status=$?
  print_filtered_output_lines <"$reader_log"
  (( reader_status == 0 )) || fail 'the root readback reader did not become ready'
}

stop_readback_reader() {
  local reader_log="$fixture_root/readback-reader-stop.log"
  local reader_status=0
  # systemctl stop is the only terminator: SIGTERM is the reader's clean stop,
  # so no root write (and no A-planted name) ever enters the channel.
  machine_run /usr/bin/systemctl stop "$READBACK_READER_UNIT"
  machine_run /bin/bash -c '
    set -Eeuo pipefail
    unit=$1
    channel=$2
    for _ in {1..300}; do
      state=$(/usr/bin/systemctl show --property=ActiveState --value "$unit" 2>/dev/null || true)
      [[ "$state" == inactive || "$state" == failed ]] && break
      /usr/bin/sleep 0.1
    done
    state=$(/usr/bin/systemctl show --property=ActiveState --value "$unit")
    result=$(/usr/bin/systemctl show --property=Result --value "$unit")
    status=$(/usr/bin/systemctl show --property=ExecMainStatus --value "$unit")
    [[ "$state" == inactive && "$result" == success && "$status" == 0 ]]
    [[ ! -e "$channel/ready" ]]
    /usr/bin/journalctl --no-pager --output=cat --grep="readback-reader:" -n 40 -u "$unit" || true
  ' bash "$READBACK_READER_UNIT" "$READBACK_CHANNEL" >"$reader_log" 2>&1 || reader_status=$?
  print_filtered_output_lines <"$reader_log"
  (( reader_status == 0 )) || fail 'the root readback reader did not stop cleanly'
}

stage_owner_request_file() {
  local request_source=$1
  local guest_request=$2
  local label=$3
  sudo install -o root -g "$A_GID" -m 0440 "$request_source" "$rootfs$guest_request"
  if ! sudo test -f "$rootfs$guest_request" || ! sudo test ! -L "$rootfs$guest_request" ||
     [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$guest_request")" != "0:$A_GID:440:1" ]]; then
    fail "owner-witness request must be a singly-linked root:A 0440 file: $label"
  fi
  rm -f -- "$request_source"
  machine_run_as "$A_USER" "$A_USER" /usr/bin/test -r "$guest_request"
  if machine_run_as "$B_USER" "$B_USER" /usr/bin/test -r "$guest_request"; then
    fail "native B unexpectedly read the root-owned Claim request for $label"
  fi
}

prepare_owner_request() {
  local fixture_case=$1
  local request_case=$2
  local config_source=$3
  local barrier_directory=$4
  local guest_request=$5
  local hold_after=${6:-}
  local keep_source=${7:-}
  local receipt_context_output=${8:-}
  local replay_required=${9:-false}
  local fresh_guest_request=${10:-}
  local run_metadata="$fixture_root/run-$fixture_case.json"
  local request_source="$fixture_root/request-$fixture_case.json"

  [[ -f "$config_source" ]] || fail "missing per-case production config: $config_source"
  (
    cd "$repo/control"
    env -u GH_TOKEN -u GITHUB_TOKEN -u SYMMETRY_OPENAI_API_KEY MIX_ENV=prod \
      mix run --no-start ../scripts/ci/provision-linux-v2-control-claims.exs schedule \
        "$control_fixture" "$fixture_case" "$run_metadata"
  )
  local -a writer=(
    python3 "$repo/scripts/ci/write-linux-v2-owner-request.py"
    "$request_case" "$run_metadata" "$config_source" "$barrier_directory"
    "$control_url" "$machine_token_file" "$request_source"
  )
  if [[ -n "$hold_after" ]]; then
    writer+=("$hold_after")
  fi
  "${writer[@]}"
  if [[ -n "$fresh_guest_request" ]]; then
    local fresh_request_source="$fixture_root/request-$fixture_case-fresh.json"
    cp -- "$request_source" "$fresh_request_source"
    chmod 0600 "$fresh_request_source"
    stage_owner_request_file "$fresh_request_source" "$fresh_guest_request" "$fixture_case-fresh"
  fi
  if [[ "$replay_required" == true ]]; then
    python3 - "$request_source" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
request = json.loads(path.read_text(encoding="utf-8"))
if request.get("replay_required", False) is not False:
    raise SystemExit("new owner request unexpectedly requires replay")
request["replay_required"] = True
path.write_text(json.dumps(request, separators=(",", ":")), encoding="utf-8")
path.chmod(0o600)
PY
  elif [[ "$replay_required" != false ]]; then
    fail "invalid replay-required owner request selector: $replay_required"
  fi
  python3 - "$request_source" "$evidence_secret_values_file" <<'PY'
import json
import pathlib
import sys

request_path, values_path = map(pathlib.Path, sys.argv[1:])
request = json.loads(request_path.read_text(encoding="utf-8"))
claim = request.get("claim")
if not isinstance(claim, dict) or not isinstance(claim.get("lease_token"), str) or not claim["lease_token"]:
    raise SystemExit("owner witness request omitted its exact lease credential")
secrets = [claim["lease_token"]]
provider_access = claim.get("provider_access")
if isinstance(provider_access, dict):
    token = provider_access.get("token")
    if isinstance(token, str) and token:
        secrets.append(token)
with values_path.open("ab") as stream:
    for secret in secrets:
        stream.write(secret.encode("utf-8") + b"\0")
PY
  chmod 0600 "$evidence_secret_values_file"
  if [[ -n "$keep_source" ]]; then
    cp -- "$request_source" "$keep_source"
    chmod 0600 "$keep_source"
  fi
  if [[ -n "$receipt_context_output" ]]; then
    python3 - "$run_metadata" "$receipt_context_output" <<'PY'
import json
import pathlib
import sys

source, destination = sys.argv[1:]
record = json.loads(pathlib.Path(source).read_text(encoding="utf-8"))
context = {
    "version": 1,
    "case": record["fixture_case"],
    "work_item_id": record["work_item_id"],
    "task_id": record["task_id"],
    "run_id": record["run_id"],
    "generation": record["generation"],
    "runtime_id": record["runtime_id"],
    "runtime_epoch": record["runtime_epoch"],
    "claim_id": record["claim_id"],
    "resource_id": record["repository_resource_id"],
}
path = pathlib.Path(destination)
path.write_text(json.dumps(context, separators=(",", ":")), encoding="utf-8")
path.chmod(0o600)
PY
    local context_run_id
    context_run_id=$(python3 - "$receipt_context_output" <<'PY'
import json
import pathlib
import sys

print(json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))["run_id"])
PY
)
    local guest_context="$OWNER_CONTROL_RECEIPT_CONTEXT_ROOT/$fixture_case/$context_run_id.json"
    sudo install -o root -g "$A_GID" -m 0440 "$receipt_context_output" "$rootfs$guest_context"
    [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$guest_context")" == "0:$A_GID:440:1" ]] ||
      fail "Control receipt binding must be singly-linked root:A mode 0440: $fixture_case"
    machine_run_as "$A_USER" "$A_USER" /usr/bin/test -r "$guest_context"
    if machine_run_as "$B_USER" "$B_USER" /usr/bin/test -r "$guest_context"; then
      fail "native B unexpectedly read the root-owned Control receipt binding: $fixture_case"
    fi
  fi
  stage_owner_request_file "$request_source" "$guest_request" "$fixture_case"
  rm -f -- "$run_metadata"
}

run_native_owner_witness() {
  local case_name=$1
  local event_file=$2
  local config_file
  local request_environment
  local test_name
  local provider_receipt_required=0
  case "$case_name" in
    pi_provider_action)
      config_file=pi-provider-action.json
      request_environment=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_REQUEST
      test_name=TestProductionLinuxV2PiNativeProviderAction
      provider_receipt_required=1
      ;;
    opencode_native_health)
      config_file=opencode-health.json
      request_environment=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_OPENCODE_REQUEST
      test_name=TestProductionLinuxV2OpenCodeNativeHealth
      ;;
    helper_sigkill_detached_child)
      config_file=pi-helper-crash.json
      request_environment=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_CRASH_REQUEST
      test_name=TestProductionLinuxV2HelperSIGKILLAnchorReapsDetachedChild
      provider_receipt_required=1
      ;;
    daemon_sigkill_helper_survives_detached_child)
      config_file=pi-daemon-crash.json
      request_environment=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_PI_DAEMON_CRASH_REQUEST
      test_name=TestProductionLinuxV2DaemonSIGKILLHelperSurvivesDetachedChild
      provider_receipt_required=1
      ;;
    codex_containment)
      config_file=codex-containment.json
      request_environment=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_CODEX_REQUEST
      test_name=TestProductionLinuxV2CodexNativeContainment
      ;;
    claude_containment)
      config_file=claude-containment.json
      request_environment=SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_CLAUDE_REQUEST
      test_name=TestProductionLinuxV2ClaudeNativeContainment
      ;;
    *) fail "unknown production Linux v2 owner witness case: $case_name" ;;
  esac

  local config_source="$fixture_root/$config_file"
  local guest_request="$OWNER_REQUEST_ROOT/$case_name.json"
  local barrier_directory="$OWNER_BARRIER_ROOT/$case_name"
  local output_directory="$A_HOME/ci-fixture/witness-output/$case_name"
  local status=0

  if (( provider_receipt_required )); then
    local receipt_context_file="$fixture_root/control-provider-receipt-context-$case_name.json"
    local receipt_host_file="$fixture_root/control-provider-action-$case_name.json"
    local receipt_cancel_file="$fixture_root/control-provider-action-$case_name.cancel"
    local receipt_query_log="$fixture_root/control-provider-action-readback-$case_name.log"
    local go_test_pid receipt_query_pid receipt_run_id
    local go_status=0 receipt_status=0
    prepare_owner_request \
      "$case_name" "$case_name" "$config_source" "$barrier_directory" "$guest_request" \
      '' '' "$receipt_context_file"
    receipt_run_id=$(python3 - "$receipt_context_file" <<'PY'
import json
import pathlib
import sys

print(json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))["run_id"])
PY
)
    machine_run_as "$A_USER" "$A_USER" /usr/bin/mkdir -m 0700 -p \
      "$OWNER_CONTROL_RECEIPT_ROOT/$case_name"
    machine_run /usr/bin/test ! -e "$OWNER_CONTROL_RECEIPT_ROOT/$case_name/$receipt_run_id.json"
    rm -f -- "$receipt_host_file" "$receipt_cancel_file"

    (
      if run_go_test "$event_file" ./e2e "^${test_name}$" production \
        "$request_environment" "$guest_request" "$output_directory" -- "$test_name"; then
        exit 0
      else
        test_status=$?
        : > "$receipt_cancel_file"
        exit "$test_status"
      fi
    ) &
    go_test_pid=$!
    (
      cd "$repo/control"
      exec env -u GH_TOKEN -u GITHUB_TOKEN -u SYMMETRY_OPENAI_API_KEY \
        MIX_ENV=prod mix run --no-start ../scripts/ci/provision-linux-v2-control-claims.exs \
          read-provider-action "$receipt_context_file" "$receipt_host_file" "$receipt_cancel_file"
    ) >"$receipt_query_log" 2>&1 &
    receipt_query_pid=$!

    if wait "$receipt_query_pid"; then
      receipt_status=0
      publish_control_provider_receipt "$receipt_host_file" "$case_name" "$receipt_run_id"
    else
      receipt_status=$?
      printf 'Control did not persist a succeeded provider_action_intents receipt for the exact Pi Claim/Run/resource/operation (case=%s).\n' \
        "$case_name" >&2
    fi
    if wait "$go_test_pid"; then
      go_status=0
    else
      go_status=$?
    fi
    if (( receipt_status != 0 )); then
      machine_run /usr/bin/rm -f -- "$guest_request" \
        "$OWNER_CONTROL_RECEIPT_CONTEXT_ROOT/$case_name/$receipt_run_id.json" \
        "$OWNER_CONTROL_RECEIPT_ROOT/$case_name/$receipt_run_id.json"
      return 1
    fi
    status=$go_status
  else
    prepare_owner_request "$case_name" "$case_name" "$config_source" "$barrier_directory" "$guest_request"
    if run_go_test "$event_file" ./e2e "^${test_name}$" production \
      "$request_environment" "$guest_request" "$output_directory" -- "$test_name"; then
      status=0
    else
      status=$?
    fi
  fi
  machine_run /usr/bin/rm -f -- "$guest_request"
  if (( provider_receipt_required )); then
    machine_run /usr/bin/rm -f -- \
      "$OWNER_CONTROL_RECEIPT_CONTEXT_ROOT/$case_name/$receipt_run_id.json" \
      "$OWNER_CONTROL_RECEIPT_ROOT/$case_name/$receipt_run_id.json"
  fi
  if (( status != 0 )); then
    return "$status"
  fi
}

run_stage_replay_leg() {
  local event_file=$1
  local hold_after=$2
  local stage_name=$3
  local fixture_case=$4
  local request_case=$5
  local config_source=$6
  local test_name=TestProductionLinuxV2StageCrashReplayMatrix
  local output_directory="$A_HOME/ci-fixture/witness-output/stage-replay"
  local status=0
  local stage_directory="$OWNER_STAGE_REPLAY_ROOT/$stage_name"
  local -a expected_tests=("$test_name" "$test_name/$stage_name")
  local -a prepare_args=(
    "$fixture_case" "$request_case" "$config_source"
    "$OWNER_BARRIER_ROOT/stage-replay/$stage_name"
    "$stage_directory/request.json"
  )
  if [[ "$request_case" == stage_replay ]]; then
    prepare_args+=("$hold_after")
  fi
  prepare_owner_request "${prepare_args[@]}"

  if run_go_test "$event_file" ./e2e "^${test_name}/${stage_name}$" stage-replay \
    '' '' "$output_directory" -- "${expected_tests[@]}"; then
    status=0
  else
    status=$?
  fi
  machine_run /usr/bin/rm -f -- "$stage_directory/request.json"
  if (( status != 0 )); then
    return "$status"
  fi
}

run_stage_replay_case() {
  local event_file=$1
  local hold_after=$2
  run_stage_replay_leg "$event_file" "$hold_after" "$hold_after" \
    "stage_replay_$hold_after" stage_replay "$fixture_root/stage-replay/$hold_after/config.json"
}

run_kind_stage_replay_case() {
  local event_file=$1
  local kind=$2
  run_stage_replay_leg "$event_file" native_release "native_release_$kind" \
    "stage_replay_native_release_$kind" "stage_replay_native_release_$kind" \
    "$fixture_root/stage-replay-native-release-$kind/native_release/config.json"
}

run_stage_replay_cases() {
  local event_file=$1
  shift
  local stage
  for stage in "$@"; do
    run_stage_replay_case "$event_file" "$stage"
  done
}

run_ack_loss_replay_case() {
  local event_file=$1
  local stage=$2
  local test_name=TestProductionLinuxV2AckLossReplayMatrix
  local output_directory="$A_HOME/ci-fixture/witness-output/ack-loss-replay"
  local status=0
  local fixture_case="stage_replay_ack_loss_$stage"
  local stage_directory="$OWNER_ACK_LOSS_ROOT/$stage"
  local first_request_source="$fixture_root/ack-loss-first-$stage.json"
  local replay_request_source="$fixture_root/ack-loss-replay-$stage.json"
  local -a expected_tests=("$test_name" "$test_name/$stage")
  prepare_owner_request \
    "$fixture_case" stage_replay \
    "$fixture_root/ack-loss/$stage/config.json" \
    "$OWNER_BARRIER_ROOT/stage-replay/$stage" \
    "$stage_directory/request.json" "drop_ack_after=$stage" "$first_request_source"
  python3 - "$first_request_source" "$replay_request_source" "$stage" <<'PY'
import json
import pathlib
import sys

source, destination, stage = sys.argv[1:]
request = json.loads(pathlib.Path(source).read_text(encoding="utf-8"))
if request["barrier"]["hold_after"] != stage or request.get("drop_ack_after") != stage:
    raise SystemExit("ACK-loss request does not hold its exact drop boundary")
del request["drop_ack_after"]
path = pathlib.Path(destination)
path.write_text(json.dumps(request, separators=(",", ":")), encoding="utf-8")
path.chmod(0o600)
PY
  stage_owner_request_file \
    "$replay_request_source" "$stage_directory/replay_request.json" "$fixture_case-replay"
  rm -f -- "$first_request_source"

  if run_go_test "$event_file" ./e2e "^${test_name}/${stage}$" stage-replay \
    '' '' "$output_directory" -- "${expected_tests[@]}"; then
    status=0
  else
    status=$?
  fi
  machine_run /usr/bin/rm -f -- "$stage_directory/request.json" "$stage_directory/replay_request.json"
  if (( status != 0 )); then
    return "$status"
  fi
}

run_ack_loss_replay_cases() {
  local event_file=$1
  shift
  local stage
  for stage in "$@"; do
    run_ack_loss_replay_case "$event_file" "$stage"
  done
}

mutate_negative_owner_request() {
  local source_request=$1
  local destination=$2
  local mutation=$3
  local barrier_directory=$4
  python3 - "$source_request" "$destination" "$mutation" "$barrier_directory" <<'PY'
import json
import pathlib
import sys

source, destination, mutation, barrier_directory = sys.argv[1:]
request = json.loads(pathlib.Path(source).read_text(encoding="utf-8"))
request["barrier"]["directory"] = barrier_directory
request["barrier"]["release_file"] = str(pathlib.Path(barrier_directory) / "release")
request["replay_required"] = True
if mutation == "fence":
    request["fence"]["runtime_epoch"] += 1
elif mutation == "subject":
    subject = request["subject"]
    original_commit = subject["commit"]
    replacement = "0" if original_commit[0] != "0" else "1"
    alternative_commit = replacement + original_commit[1:]
    original_tree = subject["tree_digest"]
    replacement = "0" if original_tree[7] != "0" else "1"
    alternative_tree = original_tree[:7] + replacement + original_tree[8:]
    subject["commit"] = alternative_commit
    subject["tree_digest"] = alternative_tree
else:
    raise SystemExit("unsupported stale owner-witness request mutation")

path = pathlib.Path(destination)
path.write_text(json.dumps(request, separators=(",", ":")), encoding="utf-8")
path.chmod(0o600)
PY
}

run_stale_owner_request_denial() {
  local event_file=$1
  local case_name=$2
  local test_name=TestProductionLinuxV2StaleOwnerRequestDenials
  local request_case=$case_name
  [[ "$case_name" == stale_journal_run_key_legacy ]] && request_case=stale_journal_run_key
  local bundle="$OWNER_NEGATIVE_ROOT/$request_case"
  local config_source="$fixture_root/negative-root/$request_case/config.json"
  local host_setup_request="$fixture_root/negative-root/$request_case/setup_request.json"
  local host_stale_request="$fixture_root/negative-root/$request_case/request.json"

  if [[ "$case_name" == stale_journal_run_key ]]; then
    local unresolved_test_name=TestProductionLinuxV2UnresolvedJournalBlocksFreshRun
    local output_directory="$A_HOME/ci-fixture/witness-output/unresolved-journal-guard"
    prepare_owner_request \
      stage_replay_stale_journal_run_key_setup stage_replay "$config_source" \
      "$OWNER_BARRIER_ROOT/negative-root/$case_name/setup" \
      "$bundle/setup_request.json" handoff_committed
    prepare_owner_request \
      stage_replay_stale_journal_run_key_request stage_replay "$config_source" \
      "$OWNER_BARRIER_ROOT/negative-root/$case_name/stale" \
      "$bundle/request.json" handoff_committed '' '' true "$bundle/fresh_request.json"
    run_go_test "$event_file" ./e2e "^${unresolved_test_name}$" negative \
      '' '' "$output_directory" -- "$unresolved_test_name"
    machine_run /usr/bin/rm -rf -- "$bundle"
    return
  fi
  if [[ "$case_name" == stale_journal_run_key_legacy ]]; then
    local output_directory="$A_HOME/ci-fixture/witness-output/stale-owner-request-denials"
    local -a expected_tests=("$test_name" "$test_name/stale_journal_run_key")
    prepare_owner_request \
      stage_replay_stale_journal_run_key_legacy_setup stage_replay "$config_source" \
      "$OWNER_BARRIER_ROOT/negative-root/$request_case/setup-legacy" \
      "$bundle/setup_request.json" handoff_committed
    prepare_owner_request \
      stage_replay_stale_journal_run_key_legacy_request stage_replay "$config_source" \
      "$OWNER_BARRIER_ROOT/negative-root/$request_case/stale-legacy" \
      "$bundle/request.json" handoff_committed '' '' true
    run_go_test "$event_file" ./e2e "^${test_name}/stale_journal_run_key$" negative \
      '' '' "$output_directory" -- "${expected_tests[@]}"
    machine_run /usr/bin/rm -rf -- "$bundle"
    return
  fi

  local mutation
  case "$case_name" in
    stale_journal_fence) mutation=fence ;;
    stale_journal_subject) mutation=subject ;;
    *) fail "unsupported stale-request case set: $case_name" ;;
  esac
  local output_directory="$A_HOME/ci-fixture/witness-output/stale-owner-request-denials"
  local -a expected_tests=("$test_name" "$test_name/$case_name")
  local fixture_case="stage_replay_${case_name}_setup"
  prepare_owner_request \
    "$fixture_case" stage_replay "$config_source" \
    "$OWNER_BARRIER_ROOT/negative-root/$case_name/setup" \
    "$bundle/setup_request.json" handoff_committed "$host_setup_request"
  mutate_negative_owner_request "$host_setup_request" "$host_stale_request" "$mutation" \
    "$OWNER_BARRIER_ROOT/negative-root/$case_name/stale"
  stage_owner_request_file "$host_stale_request" "$bundle/request.json" "$case_name"
  rm -f -- "$host_setup_request"

  run_go_test "$event_file" ./e2e "^${test_name}/${case_name}$" negative \
    '' '' "$output_directory" -- "${expected_tests[@]}"
  machine_run /usr/bin/rm -rf -- "$bundle"
}

run_migration_denial_witness() {
  local event_file=$1
  local test_name=TestProductionLinuxV2MigrationDenialRetainsJournal
  local case_name=migration
  local bundle="$OWNER_NEGATIVE_ROOT/$case_name"
  local output_directory="$A_HOME/ci-fixture/witness-output/migration-denial"
  prepare_owner_request \
    stage_replay_migration stage_replay \
    "$fixture_root/negative-root/$case_name/config.json" \
    "$OWNER_BARRIER_ROOT/negative-root/$case_name/request" \
    "$bundle/request.json" native_release

  if run_go_test "$event_file" ./e2e "^${test_name}$" negative \
    '' '' "$output_directory" -- "$test_name"; then
    machine_run /usr/bin/rm -rf -- "$OWNER_NEGATIVE_ROOT"
  else
    local status=$?
    machine_run /usr/bin/rm -rf -- "$OWNER_NEGATIVE_ROOT"
    return "$status"
  fi
}

run_receipt_persist_failure_witness() {
  local event_file=$1
  local test_name=TestProductionLinuxV2StopReceiptPersistenceFailureRetainsJournal
  local case_name=receipt_persist_failure
  local bundle="$OWNER_NEGATIVE_ROOT/$case_name"
  local output_directory="$A_HOME/ci-fixture/witness-output/receipt-persist-failure"
  prepare_owner_request \
    stage_replay_receipt_persist_failure stage_replay \
    "$fixture_root/negative-root/$case_name/config.json" \
    "$OWNER_BARRIER_ROOT/negative-root/$case_name/request" \
    "$bundle/request.json" native_release

  run_go_test "$event_file" ./e2e "^${test_name}$" negative \
    '' '' "$output_directory" -- "$test_name"
  machine_run /usr/bin/rm -rf -- "$bundle"
}

run_stale_leaf_identity_witness() {
  local event_file=$1
  local test_name=TestProductionLinuxV2StaleLeafIdentityRetainsJournal
  local case_name=stale_leaf
  local bundle="$OWNER_NEGATIVE_ROOT/$case_name"
  local output_directory="$A_HOME/ci-fixture/witness-output/stale-leaf"
  prepare_owner_request \
    stage_replay_stale_leaf stage_replay \
    "$fixture_root/negative-root/$case_name/config.json" \
    "$OWNER_BARRIER_ROOT/negative-root/$case_name/request" \
    "$bundle/request.json" stop_receipt_durable

  run_go_test "$event_file" ./e2e "^${test_name}$" negative \
    '' '' "$output_directory" -- "$test_name"
  machine_run /usr/bin/rm -rf -- "$bundle"
}

run_negative_stage_replay_witness() {
  local event_file=$1
  local test_name=$2
  local case_name=$3
  local fixture_case=$4
  local hold_after=$5
  local output_name=$6
  local bundle="$OWNER_NEGATIVE_ROOT/$case_name"
  local output_directory="$A_HOME/ci-fixture/witness-output/$output_name"

  prepare_owner_request \
    "$fixture_case" stage_replay \
    "$fixture_root/negative-root/$case_name/config.json" \
    "$OWNER_BARRIER_ROOT/stage-replay/$hold_after" \
    "$bundle/request.json" "$hold_after"
  run_go_test "$event_file" ./e2e "^${test_name}$" negative \
    '' '' "$output_directory" -- "$test_name"
  machine_run /usr/bin/rm -rf -- "$bundle"
}

write_helper_response_loss_selector() {
  local guest_request=$1
  local drop_response=$2
  local selector_source="$fixture_root/helper-response-loss-selector-$drop_response.json"
  local selector_path=/etc/symmetry/linux-owner-witness.json
  local selector_temporary="$rootfs$selector_path.tmp"
  machine_run /usr/bin/test ! -e "$selector_path"

  sudo python3 - "$rootfs$guest_request" "$selector_source" "$drop_response" <<'PY'
import json
import pathlib
import sys

request_path, output_path, drop_response = sys.argv[1:]
request = json.loads(pathlib.Path(request_path).read_text(encoding="utf-8"))
run_key = request.get("run_key")
if not isinstance(run_key, dict) or not isinstance(run_key.get("run_id"), str) or not isinstance(run_key.get("generation"), int):
    raise SystemExit("owner request does not contain an exact RunKey for the helper selector")
if drop_response not in ("stop", "ack-stop-receipt", "launcher-ready"):
    raise SystemExit("unsupported owner-helper response-loss selector")
selector = {
    "version": 1,
    "run_id": run_key["run_id"],
    "generation": run_key["generation"],
    "drop_response": drop_response,
}
path = pathlib.Path(output_path)
path.write_text(json.dumps(selector, separators=(",", ":")), encoding="utf-8")
path.chmod(0o600)
PY
  sudo rm -f -- "$selector_temporary"
  sudo install -o root -g "$A_GID" -m 0440 "$selector_source" "$selector_temporary"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$selector_temporary")" == "0:$A_GID:440:1" ]] ||
    fail "helper response-loss selector staging file has unsafe ownership: $drop_response"
  sudo mv -T -- "$selector_temporary" "$rootfs$selector_path"
  sudo sync -f "$rootfs/etc/symmetry"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$selector_path")" == "0:$A_GID:440:1" ]] ||
    fail "helper response-loss selector must be root:A 0440 singly-linked: $drop_response"
}

write_helper_exit_before_register_selector() {
  local guest_request=$1
  local selector_source="$fixture_root/helper-exit-before-register-selector.json"
  local selector_path=/etc/symmetry/linux-owner-witness.json
  local selector_temporary="$rootfs$selector_path.tmp"
  machine_run /usr/bin/test ! -e "$selector_path"

  sudo python3 - "$rootfs$guest_request" "$selector_source" "$OWNER_HELPER_EXIT_ROOT" <<'PY'
import json
import pathlib
import sys

request_path, output_path, expected_barrier = sys.argv[1:]
request = json.loads(pathlib.Path(request_path).read_text(encoding="utf-8"))
run_key = request.get("run_key")
barrier = request.get("barrier")
if request.get("case") != "stage_replay" or request.get("harness") != "pi":
    raise SystemExit("helper-exit request must be an authentic Pi stage_replay Claim")
if not isinstance(run_key, dict) or not isinstance(run_key.get("run_id"), str) or not run_key["run_id"]:
    raise SystemExit("helper-exit request omitted its exact Control RunID")
if not isinstance(run_key.get("generation"), int) or isinstance(run_key["generation"], bool) or run_key["generation"] <= 0:
    raise SystemExit("helper-exit request omitted its positive RunKey generation")
if not isinstance(barrier, dict) or barrier.get("directory") != expected_barrier or \
        barrier.get("hold_after") != "checkout_ready" or barrier.get("release_file") != expected_barrier + "/release":
    raise SystemExit("helper-exit request must hold at the exact checkout_ready A barrier")
selector = {
    "version": 1,
    "run_id": run_key["run_id"],
    "generation": run_key["generation"],
    "helper_action": "crash_after_release_before_register",
    "hold_for_release_file": True,
}
path = pathlib.Path(output_path)
path.write_text(json.dumps(selector, separators=(",", ":")), encoding="utf-8")
path.chmod(0o600)
PY
  sudo rm -f -- "$selector_temporary"
  sudo install -o root -g "$A_GID" -m 0440 "$selector_source" "$selector_temporary"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$selector_temporary")" == "0:$A_GID:440:1" ]] ||
    fail 'helper-exit selector staging file has unsafe ownership'
  sudo mv -T -- "$selector_temporary" "$rootfs$selector_path"
  sudo sync -f "$rootfs/etc/symmetry"
  [[ "$(sudo stat -c '%u:%g:%a:%h' "$rootfs$selector_path")" == "0:$A_GID:440:1" ]] ||
    fail 'helper-exit selector must be root:A 0440 singly-linked'
  rm -f -- "$selector_source"
}

helper_exit_control_count=0
machine_run_helper_exit_control() {
  helper_exit_control_count=$((helper_exit_control_count + 1))
  local unit="${machine}-helper-exit-control-${helper_exit_control_count}"
  sudo systemd-run --machine="$machine" --quiet --wait --pipe --collect \
    --unit="$unit" -- "$@"
}

run_helper_exit_after_release_before_native_register_witness() {
  local event_file=$1
  local test_name=TestProductionLinuxV2HelperExitAfterReleaseBeforeNativeRegister
  local case_name=$OWNER_HELPER_EXIT_CASE
  local fixture_case=$OWNER_HELPER_EXIT_WORK_ITEM
  local bundle="$OWNER_NEGATIVE_ROOT/$case_name"
  local config_source="$fixture_root/negative-root/$case_name/config.json"
  local guest_request="$bundle/request.json"
  local output_directory="$A_HOME/ci-fixture/witness-output/helper-exit-after-release-before-register"
  local checkpoint="$OWNER_HELPER_EXIT_ROOT/live-b-observed.json"
  local host_checkpoint="$rootfs$checkpoint"
  local selector=/etc/symmetry/linux-owner-witness.json
  local go_test_pid go_status=0 selector_unlinked=0 checkout_released=0 checkpoint_seen=0

  prepare_owner_request "$fixture_case" stage_replay "$config_source" \
    "$OWNER_HELPER_EXIT_ROOT" "$guest_request" checkout_ready
  write_helper_exit_before_register_selector "$guest_request"

  (
    run_go_test "$event_file" ./e2e "^${test_name}$" negative \
      '' '' "$output_directory" -- "$test_name"
  ) &
  go_test_pid=$!

  # Wait for the ordinary A-owned checkout_ready release before starting the
  # short helper-action window; the checkpoint itself is polled at 100 ms.
  for _ in {1..900}; do
    if sudo test -f "$rootfs$OWNER_HELPER_EXIT_ROOT/release" &&
       sudo test ! -L "$rootfs$OWNER_HELPER_EXIT_ROOT/release"; then
      checkout_released=1
      break
    fi
    if ! kill -0 "$go_test_pid" 2>/dev/null ||
       [[ "$(ps -p "$go_test_pid" -o stat= 2>/dev/null)" == *Z* ]]; then
      break
    fi
    sleep 1
  done
  if (( checkout_released )); then
    for _ in {1..140}; do
      if sudo test -f "$host_checkpoint" && sudo test ! -L "$host_checkpoint"; then
        checkpoint_seen=1
        break
      fi
      if ! kill -0 "$go_test_pid" 2>/dev/null ||
         [[ "$(ps -p "$go_test_pid" -o stat= 2>/dev/null)" == *Z* ]]; then
        break
      fi
      sleep 0.1
    done
  fi

  if (( checkpoint_seen )); then
    if [[ "$(sudo stat -c '%u:%g:%a:%h' "$host_checkpoint")" == "$A_UID:$A_GID:600:1" ]] &&
       [[ "$(sudo stat -c '%s' "$host_checkpoint")" =~ ^[1-9][0-9]{0,11}$ ]] &&
       (( $(sudo stat -c '%s' "$host_checkpoint") <= 4096 )); then
      if machine_run_helper_exit_control /usr/bin/python3 \
        "$A_HOME/ci-source/scripts/ci/verify-linux-v2-helper-exit-checkpoint.py" \
        "$checkpoint" "$selector" "$A_UID" "$A_GID" "$B_UID" "$B_GID"; then
        selector_unlinked=1
      fi
    fi
  fi

  if wait "$go_test_pid"; then
    go_status=0
  else
    go_status=$?
  fi

  if (( ! selector_unlinked )); then
    printf 'Helper-exit witness did not prove and release the exact live-B selector; Go test status=%s\n' "$go_status" >&2
    # Failure cleanup only: do not accept this run as stop-receipt evidence.
    if machine_run_helper_exit_control /usr/bin/rm -f -- "$selector"; then
      machine_run_helper_exit_control /usr/bin/sync -f /etc/symmetry || true
    fi
    return 1
  fi
  if (( go_status != 0 )); then
    machine_run_helper_exit_control /usr/bin/test ! -e "$selector"
    return "$go_status"
  fi
  machine_run_helper_exit_control /usr/bin/test ! -e "$selector"
  machine_run_helper_exit_control /usr/bin/rm -f -- \
    "$checkpoint" "$OWNER_HELPER_EXIT_ROOT/release" "$guest_request"
  machine_run_helper_exit_control /usr/bin/rm -rf -- "$bundle"
}

run_helper_response_loss_subtest() {
  local event_file=$1
  local case_name=$2
  local fixture_case=$3
  local hold_after=$4
  local drop_response=$5
  local subtest=$6
  local test_name=TestProductionLinuxV2HelperResponseLossReplayMatrix
  local bundle="$OWNER_NEGATIVE_ROOT/$case_name"
  local guest_request="$bundle/request.json"
  local output_directory="$A_HOME/ci-fixture/witness-output/helper-response-loss-$subtest"
  local status=0

  prepare_owner_request \
    "$fixture_case" stage_replay \
    "$fixture_root/negative-root/$case_name/config.json" \
    "$OWNER_BARRIER_ROOT/stage-replay/$hold_after" \
    "$guest_request" "$hold_after"
  write_helper_response_loss_selector "$guest_request" "$drop_response"

  if run_go_test "$event_file" ./e2e "^${test_name}/${subtest}$" negative \
    '' '' "$output_directory" -- "$test_name" "$test_name/$subtest"; then
    status=0
  else
    status=$?
  fi
  machine_run /usr/bin/rm -f -- "$guest_request" /etc/symmetry/linux-owner-witness.json
  if (( status != 0 )); then
    return "$status"
  fi
}

run_helper_response_loss_witnesses() {
  local stop_events=$1
  local ack_stop_events=$2
  local launcher_ready_events=$3
  run_helper_response_loss_subtest \
    "$stop_events" helper_stop_response_loss stage_replay_helper_stop_response_loss \
    native_release stop stop
  run_helper_response_loss_subtest \
    "$ack_stop_events" helper_ack_response_loss \
    stage_replay_helper_ack_stop_receipt_response_loss \
    stop_receipt_durable ack-stop-receipt ack_stop_receipt
  run_helper_response_loss_subtest \
    "$launcher_ready_events" helper_launcher_response_loss \
    stage_replay_helper_launcher_response_loss \
    launcher_bound launcher-ready launcher_ready
}

run_alternate_mount_refusal_witness() {
  local event_file=$1
  local test_name=TestProductionLinuxV2AlternateCgroupMountRefusal
  local case_name=alternate_mount
  local bundle="$OWNER_NEGATIVE_ROOT/$case_name"
  local output_directory="$A_HOME/ci-fixture/witness-output/alternate-mount"
  local status=0
  # This refusal retains a claim-only RunJournal no product path retires; its own case set (own
  # guest and Store) is the only isolation, so no other set can observe the row.
  prepare_owner_request \
    stage_replay_alternate_mount stage_replay \
    "$fixture_root/negative-root/$case_name/config.json" \
    "$OWNER_BARRIER_ROOT/negative-root/$case_name/request" \
    "$bundle/request.json" reservation_durable

  if run_go_test "$event_file" ./e2e "^${test_name}$" negative \
    '' '' "$output_directory" -- "$test_name"; then
    status=0
  else
    status=$?
  fi
  machine_run /usr/bin/umount "$OWNER_ALTERNATE_CGROUP_MOUNT"
  machine_run /usr/bin/rm -rf -- "$bundle"
  if (( status != 0 )); then
    return "$status"
  fi
}

run_literal_pid_reuse_witness() {
  local event_file=$1
  local test_name=TestProductionLinuxV2LiteralPIDReuseAfterStopReceipt
  local case_name=literal_pid_reuse
  local bundle="$OWNER_NEGATIVE_ROOT/$case_name"
  local output_directory="$A_HOME/ci-fixture/witness-output/literal-pid-reuse"
  local status=0

  prepare_owner_request \
    stage_replay_literal_pid_reuse stage_replay \
    "$fixture_root/negative-root/$case_name/config.json" \
    "$OWNER_BARRIER_ROOT/stage-replay/leaf_removed" \
    "$bundle/request.json" leaf_removed
  start_literal_pid_reuse_watcher

  if run_go_test "$event_file" ./e2e "^${test_name}$" negative \
    '' '' "$output_directory" -- "$test_name"; then
    status=0
  else
    status=$?
  fi

  if (( status == 0 )); then
    machine_run /usr/bin/systemctl is-active --quiet "$PID_REUSE_WATCHER_UNIT"
    machine_run /usr/bin/test ! -L "$OWNER_PID_REUSE_ROOT/ready"
    machine_run /usr/bin/test -f "$OWNER_PID_REUSE_ROOT/ready"
    machine_run /usr/bin/bash -c \
      'test "$(/usr/bin/stat -c %u:%g:%a:%h "$1")" = "0:$2:440:1"' \
      bash "$OWNER_PID_REUSE_ROOT/ready" "$A_GID"
    machine_run /usr/bin/bash -c \
      'printf "ready\n" | /usr/bin/cmp --silent "$1" -' \
      bash "$OWNER_PID_REUSE_ROOT/ready"
  fi
  machine_run /usr/bin/systemctl stop "$PID_REUSE_WATCHER_UNIT"
  machine_run /usr/bin/rm -f -- "$OWNER_PID_REUSE_ROOT/ready"
  machine_run /usr/bin/rm -rf -- "$bundle"
  if (( status != 0 )); then
    return "$status"
  fi
}

run_legacy_v1_replay_witness() {
  local event_file=$1
  local test_name=TestProductionLinuxV2LegacyV1ReplayNoV2Receipt
  run_go_test "$event_file" ./e2e "^${test_name}$" legacy-v1 \
    '' '' '' -- "$test_name"
}

pi_legacy_refusal_events="$fixture_root/pi-legacy-provider-refusal.json"
opencode_legacy_refusal_events="$fixture_root/opencode-legacy-health-refusal.json"
owner_events="$fixture_root/linux-v2-peer-scratch.json"
v2_pi_events="$fixture_root/linux-v2-native-pi-e2e.json"
v2_opencode_events="$fixture_root/linux-v2-native-opencode-e2e.json"
v2_crash_events="$fixture_root/linux-v2-native-detached-crash.json"
v2_daemon_crash_events="$fixture_root/linux-v2-native-daemon-crash.json"
v2_stage_replay_events="$fixture_root/linux-v2-native-stage-replay.json"
v2_ack_loss_events="$fixture_root/linux-v2-native-ack-loss-replay.json"
v2_helper_stop_loss_events="$fixture_root/linux-v2-native-helper-stop-response-loss.json"
v2_helper_ack_loss_events="$fixture_root/linux-v2-native-helper-ack-response-loss.json"
v2_helper_launcher_loss_events="$fixture_root/linux-v2-native-helper-launcher-response-loss.json"
v2_helper_exit_before_register_events="$fixture_root/linux-v2-native-helper-exit-before-register.json"
v2_negative_events="$fixture_root/linux-v2-native-stale-owner-requests.json"
v2_migration_events="$fixture_root/linux-v2-native-migration-denial.json"
v2_receipt_persist_events="$fixture_root/linux-v2-native-receipt-persist-failure.json"
v2_stale_leaf_events="$fixture_root/linux-v2-native-stale-leaf.json"
v2_alternate_mount_events="$fixture_root/linux-v2-native-alternate-mount.json"
v2_pid_reuse_events="$fixture_root/linux-v2-native-literal-pid-reuse.json"
v2_b_cleanup_denial_events="$fixture_root/linux-owner-b-cleanup-denial.json"
v2_cgroup_mount_cleanup_events="$fixture_root/linux-owner-cross-mount-cleanup.json"
v2_unexpected_cgroup_procs_fd_events="$fixture_root/linux-owner-unexpected-cgroup-procs-fd.json"
v2_v1_replay_events="$fixture_root/linux-v2-native-v1-replay.json"
v2_marker_reader_events="$fixture_root/linux-v2-native-marker-reader.json"
v2_synthetic_goal_cleanup_events="$fixture_root/linux-v2-synthetic-goal-cleanup.json"
v2_peer_negative_pi_events="$fixture_root/linux-v2-peer-negative-pi.json"
v2_peer_negative_opencode_events="$fixture_root/linux-v2-peer-negative-opencode.json"
v2_codex_containment_events="$fixture_root/linux-v2-codex-containment.json"
v2_codex_stage_replay_events="$fixture_root/linux-v2-codex-stage-replay.json"
v2_claude_containment_events="$fixture_root/linux-v2-claude-containment.json"
v2_claude_stage_replay_events="$fixture_root/linux-v2-claude-stage-replay.json"
start_readback_reader
case "$fixture_case_set" in
  shared)
    # Legacy Linux owners must fail before effects; installed-v2 positives run separately below.
    run_go_test "$pi_legacy_refusal_events" ./internal/harness/pi '^TestLinuxLegacyPiProviderBridgeRefusesBeforeProviderAction$' \
      pi-baseline '' '' '' -- TestLinuxLegacyPiProviderBridgeRefusesBeforeProviderAction
    run_go_test "$opencode_legacy_refusal_events" ./internal/harness/opencode '^TestLinuxLegacyOpenCodeRefusesBeforeReadinessRequest$' \
      opencode-baseline '' '' '' -- TestLinuxLegacyOpenCodeRefusesBeforeReadinessRequest
    run_go_test "$v2_b_cleanup_denial_events" ./cmd/symmetry-linux-launcher \
      '^TestInstalledLinuxOwnerCleanupRejectsBCallerScratch$' cleanup-root \
      '' '' '' -- TestInstalledLinuxOwnerCleanupRejectsBCallerScratch
    run_go_test "$v2_cgroup_mount_cleanup_events" ./internal/launchercore \
      '^TestCloneCleanupRejectsCrossMountEntry$' cleanup-mounts-root \
      '' '' '' -- TestCloneCleanupRejectsCrossMountEntry
    run_unexpected_cgroup_procs_fd_witness "$v2_unexpected_cgroup_procs_fd_events"
    run_go_test "$owner_events" ./native/linux-gate/protocol \
      '^TestInstalledLinuxOwner(BoundaryScratch|CloneCheckoutScratch|CloneCleanupScratch|CloneCleanupParentDeathScratch)$' \
      scratch '' '' '' -- \
      TestInstalledLinuxOwnerBoundaryScratch \
      TestInstalledLinuxOwnerCloneCheckoutScratch \
      TestInstalledLinuxOwnerCloneCleanupScratch \
      TestInstalledLinuxOwnerCloneCleanupParentDeathScratch
    run_go_test "$v2_marker_reader_events" ./e2e '^TestProductionLinuxV2HookMarkerReaderRejectsHostileEntries$' \
      marker-reader '' '' '' -- \
      TestProductionLinuxV2HookMarkerReaderRejectsHostileEntries \
      TestProductionLinuxV2HookMarkerReaderRejectsHostileEntries/symlink \
      TestProductionLinuxV2HookMarkerReaderRejectsHostileEntries/fifo \
      TestProductionLinuxV2HookMarkerReaderRejectsHostileEntries/oversized

    run_native_owner_witness pi_provider_action "$v2_pi_events"
    run_go_test "$v2_peer_negative_pi_events" ./internal/harness/pi \
      '^TestProviderBridgeQualifiedNonDumpablePeerFailsClosedBeforeProviderAction$' peer-negative \
      '' '' '' -- TestProviderBridgeQualifiedNonDumpablePeerFailsClosedBeforeProviderAction
    run_native_owner_witness helper_sigkill_detached_child "$v2_crash_events"
    run_native_owner_witness daemon_sigkill_helper_survives_detached_child "$v2_daemon_crash_events"
    run_stage_replay_cases "$v2_stage_replay_events" \
      reservation_durable helper_bound gate_bound handoff_prepared handoff_committed lease_armed \
      checkout_ready native_release stop_receipt_durable leaf_removed journal_cleared
    run_ack_loss_replay_cases "$v2_ack_loss_events" \
      helper_bound gate_bound handoff_prepared handoff_committed lease_armed checkout_ready \
      stop_receipt_durable native_release journal_cleared
    run_helper_exit_after_release_before_native_register_witness "$v2_helper_exit_before_register_events"
    run_helper_response_loss_witnesses "$v2_helper_stop_loss_events" \
      "$v2_helper_ack_loss_events" "$v2_helper_launcher_loss_events"
    run_receipt_persist_failure_witness "$v2_receipt_persist_events"
    run_literal_pid_reuse_witness "$v2_pid_reuse_events"
    run_legacy_v1_replay_witness "$v2_v1_replay_events"
    run_native_owner_witness opencode_native_health "$v2_opencode_events"
    run_go_test "$v2_peer_negative_opencode_events" ./internal/harness/opencode \
      '^(TestOpenCodeQualifiedStartNamespacePeerDenials|TestOpenCodeQualifiedNonDumpablePeerDeniedBeforeHTTP)$' \
      peer-negative '' '' '' -- \
      TestOpenCodeQualifiedStartNamespacePeerDenials \
      TestOpenCodeQualifiedStartNamespacePeerDenials/changed_start_identity \
      TestOpenCodeQualifiedStartNamespacePeerDenials/wrong_user_namespace \
      TestOpenCodeQualifiedNonDumpablePeerDeniedBeforeHTTP
    ;;
  stage_replay_create_started)
    run_stage_replay_case "$v2_stage_replay_events" create_started
    ;;
  stage_replay_leaf_bound)
    run_stage_replay_case "$v2_stage_replay_events" leaf_bound
    ;;
  stage_replay_launcher_bound)
    run_stage_replay_case "$v2_stage_replay_events" launcher_bound
    ;;
  ack_loss_launcher_bound)
    run_ack_loss_replay_case "$v2_ack_loss_events" launcher_bound
    ;;
  codex_containment)
    run_native_owner_witness codex_containment "$v2_codex_containment_events"
    run_kind_stage_replay_case "$v2_codex_stage_replay_events" codex
    ;;
  claude_containment)
    run_native_owner_witness claude_containment "$v2_claude_containment_events"
    run_kind_stage_replay_case "$v2_claude_stage_replay_events" claude
    ;;
  stale_leaf)
    run_stale_leaf_identity_witness "$v2_stale_leaf_events"
    ;;
  migration)
    run_migration_denial_witness "$v2_migration_events"
    ;;
  alternate_mount_refusal)
    run_alternate_mount_refusal_witness "$v2_alternate_mount_events"
    ;;
  stale_journal_run_key|stale_journal_run_key_legacy|stale_journal_fence|stale_journal_subject)
    run_stale_owner_request_denial "$v2_negative_events" "$fixture_case_set"
    ;;
  synthetic_goal_cleanup)
    run_go_test "$v2_synthetic_goal_cleanup_events" ./e2e \
      '^TestProductionLinuxV2CloneCleanupAuthenticatedScratch$' synthetic-cleanup \
      '' '' "$A_HOME/ci-fixture/witness-output/synthetic-goal-cleanup" -- \
      TestProductionLinuxV2CloneCleanupAuthenticatedScratch
    ;;
esac
stop_readback_reader
machine_run_as "$A_USER" "$A_USER" /usr/bin/rmdir "$OWNER_TEST_LEAF"

printf 'Linux amd64 A/B case set %s completed; verifying guest/Control teardown and exporting allowlisted evidence. This witness does not establish Goal acceptance or promote native capabilities.\n' \
  "$fixture_case_set"
