#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

fail() {
  printf 'WSL2 Linux v2 denial witness failed: %s\n' "$1" >&2
  exit 1
}

[[ "${SYMMETRY_PRODUCTION_LINUX_V2_WSL2_DENIAL_WITNESS:-}" == 1 ]] || {
  printf 'Opt in with SYMMETRY_PRODUCTION_LINUX_V2_WSL2_DENIAL_WITNESS=1; this script proves denial only.\n' >&2
  exit 2
}

kernel=$(cat /proc/sys/kernel/osrelease)
kernel_lower=${kernel,,}
if [[ "$kernel_lower" != *microsoft* || "$kernel_lower" != *wsl2* ]]; then
  fail 'the host is not a detected WSL2 kernel'
fi
[[ -d /run/systemd/system && -f /sys/fs/cgroup/cgroup.controllers ]] || fail 'the provisioned WSL2 instance must run systemd on unified cgroup v2'
systemctl --system is-active --quiet symmetry-owner.service || fail 'the isolated A-owned symmetry-owner.service must already be active'
[[ -f /etc/symmetry/linux-owner.conf ]] || fail 'the root-installed fixed A/B owner configuration is required'

: "${GITHUB_WORKSPACE:?run from an installed repository checkout}"
: "${SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_CONFIG:?root-installed WSL2 OpenCode config path is required}"
: "${SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_REQUEST:?authentic Control Claim request path is required}"
: "${SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT:?A-readable/B-writable hook marker root is required}"

repo=$GITHUB_WORKSPACE
[[ -f "$repo/daemon/go.mod" && -f "$repo/scripts/ci/assert-go-test-json.py" ]] || fail 'repository paths are not the expected Symmetry checkout'
owner_uid=
owner_gid=
while IFS='=' read -r key value; do
  case "$key" in
    a_uid) owner_uid=$value ;;
    a_gid) owner_gid=$value ;;
  esac
done < /etc/symmetry/linux-owner.conf
[[ "$owner_uid" =~ ^[0-9]+$ && "$owner_gid" =~ ^[0-9]+$ ]] || fail 'the installed fixed A/B identity config is malformed'
[[ "$EUID" == "$owner_uid" && "$(id -g)" == "$owner_gid" ]] || fail 'run this witness as the installed owner A user and primary group'
[[ -f "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_CONFIG" && -r "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_CONFIG" && ! -L "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_CONFIG" ]] || fail 'the root-owned WSL2 daemon config is unavailable to A'
[[ "$(stat -c '%u:%g:%a' "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_CONFIG")" == "0:$owner_gid:640" ]] || fail 'the WSL2 daemon config must be root-owned, A-readable, and mode 0640'
[[ "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_REQUEST" == /opt/symmetry-owner-test/requests/* ]] || fail 'the authentic WSL2 request must be staged in the root-owned owner-test request directory'
[[ -f "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_REQUEST" && -r "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_REQUEST" && ! -L "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_REQUEST" ]] || fail 'the authentic WSL2 Claim request must be a readable regular file for A'
[[ "$(stat -c '%u:%g:%a' "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_REQUEST")" == "0:$owner_gid:440" ]] || fail 'the WSL2 Claim request must be root-owned, A-readable, and mode 0440'
[[ -d "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT/opencode" ]] || fail 'the OpenCode marker directory is unavailable'
if find "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT/opencode" -mindepth 1 -maxdepth 1 -print -quit | grep --quiet .; then
  fail 'the WSL2 OpenCode hook marker directory must be empty before the denial witness'
fi

GOROOT=$(go env GOROOT)
GOMODCACHE=$(go env GOMODCACHE)
GOCACHE=$(go env GOCACHE)
GOTMPDIR=${GOTMPDIR:-${TMPDIR:-/tmp}}
events=$(mktemp "${TMPDIR:-/tmp}/symmetry-wsl2-denial.XXXXXX.json")
chmod 0600 "$events"
trap 'rm -f -- "$events"' EXIT INT TERM

status=0
(
  cd "$repo/daemon"
  env -i \
    PATH="$PATH" HOME="$HOME" GOROOT="$GOROOT" GOMODCACHE="$GOMODCACHE" \
    GOCACHE="$GOCACHE" GOTMPDIR="$GOTMPDIR" GOFLAGS=-mod=readonly CGO_ENABLED=0 \
    SYMMETRY_PRODUCTION_LINUX_V2_WSL2_DENIAL_WITNESS=1 \
    SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 \
    SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_CONFIG="$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_CONFIG" \
    SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_REQUEST="$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WSL2_REQUEST" \
    SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT="$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT" \
    go test -count=1 -timeout 900s -json ./e2e \
      -run '^TestProductionLinuxV2WSL2DenialWitness$' >"$events" 2>&1
) || status=$?

if ! python3 "$repo/scripts/ci/assert-go-test-json.py" "$events" TestProductionLinuxV2WSL2DenialWitness; then
  printf 'WSL2 negative production witness did not produce exact run/pass/skip=0 events (go test exit=%s).\n' "$status" >&2
  exit 1
fi
if (( status != 0 )); then
  printf 'WSL2 negative production witness exited with status %s.\n' "$status" >&2
  exit "$status"
fi
if find "$SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT/opencode" -mindepth 1 -maxdepth 1 -print -quit | grep --quiet .; then
  fail 'the WSL2 denial witness unexpectedly reached the native OpenCode hook'
fi
printf 'WSL2 denying-host negative witness passed; this is not positive native WSL2 support evidence.\n'
