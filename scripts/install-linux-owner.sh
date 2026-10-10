#!/bin/sh
set -eu
set -f
umask 077
PATH=/usr/sbin:/usr/bin:/sbin:/bin
LC_ALL=C
export PATH
export LC_ALL
unset CDPATH ENV BASH_ENV
cd /

usage() {
  cat >&2 <<'EOF'
Usage (root): install-linux-owner.sh A_USER B_USER SETUID_WRAPPER STATIC_LAUNCHER_CORE STATIC_GATE STATIC_CLONE_BOOTSTRAP STATIC_DAEMON OWNER_UNIT_TEMPLATE DAEMON_UNIT_TEMPLATE

All nine artifacts must be direct files in the private root-owned staging directory
/root/symmetry-linux-owner-stage (mode 0700), on the root filesystem. Build only
from a reviewed root-owned, non-writable source checkout using a trusted pipeline.
The installed v2 owner config pins the canonical absolute StateDir read from the
existing /etc/symmetry/daemon.json; v1 owner configs remain readable for existing
launch/recovery but cannot authorize root clone cleanup without this field.
Never stage artifacts from /tmp, a workspace, or an A/B-writable tree. Example
native amd64 commands from that trusted checkout:
  install -d -o root -g root -m 0700 /root/symmetry-linux-owner-stage
  cd daemon
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /root/symmetry-linux-owner-stage/symmetry-daemon ./cmd/symmetry-daemon
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /root/symmetry-linux-owner-stage/symmetry-linux-launcher-core ./cmd/symmetry-linux-launcher
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /root/symmetry-linux-owner-stage/symmetry-linux-clone-bootstrap ./cmd/symmetry-linux-clone-bootstrap
  cc -O2 -static -fno-pie -no-pie -Wl,-z,relro,-z,now -Wl,-z,noexecstack -o /root/symmetry-linux-owner-stage/symmetry-linux-gate native/linux-gate/c-gate/gate.c
  cc -O2 -static -fno-pie -no-pie -Wl,-z,relro,-z,now -Wl,-z,noexecstack -o /root/symmetry-linux-owner-stage/symmetry-linux-launcher cmd/symmetry-linux-launcher/c-wrapper/wrapper.c
  install -m 0644 ../scripts/systemd/symmetry-owner.service.in /root/symmetry-linux-owner-stage/symmetry-owner.service.in
  install -m 0644 ../scripts/systemd/symmetry-daemon.service.in /root/symmetry-linux-owner-stage/symmetry-daemon.service.in

The installer refuses upgrades and markerless partial installs. It refuses an
active or enabled owner/daemon unit, and runs the supplied new daemon's read-only
-verify-legacy-owner-drained check as A once. A durable root-owned intent marker
allows an interrupted same-version first install to resume only when every staged
input and account identity still matches. It never stops, disables, enables, or
starts a service. After install, the operator must explicitly enable/start
symmetry-owner.service first and then symmetry-daemon.service; the daemon's real
-verify-linux-owner-install ExecStartPre is the second live-host gate.

The setuid wrapper is installed root:A_GID mode 04750; A_GID must be a dedicated
service group and B must not be a member. The daemon/helper remain unprivileged.
EOF
  exit 2
}

fail() {
  printf 'install-linux-owner: %s\n' "$1" >&2
  exit 1
}

check_root_directory() {
  directory=$1
  metadata=$(/usr/bin/stat -c '%u %g %a' "$directory") || fail "cannot inspect $directory"
  set -- $metadata
  [ "$#" -eq 3 ] && [ "$1" -eq 0 ] && [ "$2" -eq 0 ] || fail "$directory must be root-owned"
  permissions=$(printf '%d' "0$3") || fail "cannot parse permissions for $directory"
  [ $((permissions & 18)) -eq 0 ] || fail "$directory is group/world writable"
}

check_old_unit_inactive_disabled() {
  unit=$1
  active_state=$(/usr/bin/systemctl --system show --property=ActiveState --value "$unit" 2>/dev/null) || fail "cannot query old unit $unit"
  case "$active_state" in
    inactive|failed) ;;
    *) fail "old unit $unit must be inactive before the v2 owner cutover (state=$active_state)" ;;
  esac
  enabled_status=0
  if enabled_state=$(/usr/bin/systemctl --system is-enabled "$unit" 2>/dev/null); then
    enabled_status=0
  else
    enabled_status=$?
  fi
  case "$enabled_state" in
    disabled) ;;
    not-found) [ "$enabled_status" -ne 0 ] || fail "systemd returned an inconsistent enabled state for $unit" ;;
    *) fail "old unit $unit must be disabled before the v2 owner cutover (state=$enabled_state)" ;;
  esac
}

sha256_file() {
  line=$(/usr/bin/sha256sum "$1") || fail "cannot hash $1"
  printf '%s\n' "${line%% *}"
}

check_private_stage() {
  [ ! -L /root ] || fail '/root must not be a symlink'
  check_root_directory /
  check_root_directory /root
  [ -d "$ROOT_STAGE" ] && [ ! -L "$ROOT_STAGE" ] || fail "private root staging directory $ROOT_STAGE must pre-exist"
  metadata=$(/usr/bin/stat -c '%u %g %a' "$ROOT_STAGE") || fail 'cannot inspect private artifact staging directory'
  set -- $metadata
  [ "$#" -eq 3 ] && [ "$1" -eq 0 ] && [ "$2" -eq 0 ] && [ "$3" = 700 ] || fail 'artifact staging directory must be root:root mode 0700'
  mountpoint=$(/usr/bin/findmnt -n -o TARGET --target "$ROOT_STAGE") || fail 'cannot verify artifact staging mount'
  [ "$mountpoint" = / ] || fail 'artifact staging must be on the root filesystem, not a mount or bind-mount redirection'
}

check_expected_stage_name() {
  artifact=$1
  expected=$2
  label=$3
  case "$artifact" in "$ROOT_STAGE"/*) ;; *) fail "$label must be directly staged under $ROOT_STAGE";; esac
  case "${artifact#"$ROOT_STAGE"/}" in */*|'') fail "$label must be a direct staging-directory file";; esac
  [ "${artifact##*/}" = "$expected" ] || fail "$label must use the fixed staged name $expected"
}

check_source_artifact() {
  artifact=$1
  label=$2
  case "$artifact" in "$ROOT_STAGE"/*) ;; *) fail "$label must be directly staged under $ROOT_STAGE";; esac
  case "${artifact#"$ROOT_STAGE"/}" in */*|'') fail "$label must be a direct staging-directory file";; esac
  [ -f "$artifact" ] && [ -x "$artifact" ] && [ ! -L "$artifact" ] || fail "$label must be a regular executable file, not a symlink"
  metadata=$(/usr/bin/stat -c '%u %g %a %h' "$artifact") || fail "cannot inspect staged $label"
  set -- $metadata
  [ "$#" -eq 4 ] && [ "$1" -eq 0 ] && [ "$2" -eq 0 ] && [ "$4" -eq 1 ] || fail "$label must be root-owned, singly-linked, and private from A/B"
  permissions=$(printf '%d' "0$3") || fail "cannot parse staged $label permissions"
  [ $((permissions & 18)) -eq 0 ] || fail "$label source is group/world writable"
  [ $((permissions & 3072)) -eq 0 ] || fail "$label staging file must not carry set-ID bits"
  mountpoint=$(/usr/bin/findmnt -n -o TARGET --target "$artifact") || fail "cannot verify staged $label mount"
  [ "$mountpoint" = / ] || fail "$label source crosses a mount/bind-mount boundary"
  printf '%s\n' "$metadata"
}

check_static_elf() {
  artifact=$1
  label=$2
  identity_before=$(check_source_artifact "$artifact" "$label") || fail "$label source is not trusted"
  sha_before=$(sha256_file "$artifact")
  header=$(/usr/bin/readelf -h "$artifact") || fail "$label is not a readable ELF executable"
  case "$header" in *'Class:'*'ELF64'*) ;; *) fail "$label is not ELF64";; esac
  case "$header" in *'Machine:'*"$ELF_MACHINE"*) ;; *) fail "$label does not match native Linux amd64";; esac
  case "$header" in *'Type:'*'EXEC ('*|*'Type:'*'DYN ('*) ;; *) fail "$label is not an ELF executable image";; esac
  program_headers=$(/usr/bin/readelf -l "$artifact") || fail "$label program headers could not be inspected"
  case "$program_headers" in *INTERP*) fail "$label is dynamically linked; all staged launch images must be static";; esac
  identity_after=$(check_source_artifact "$artifact" "$label") || fail "$label staging identity changed during inspection"
  sha_after=$(sha256_file "$artifact")
  [ "$identity_before" = "$identity_after" ] && [ "$sha_before" = "$sha_after" ] || fail "$label changed during inspection"
  printf '%s\n' "$sha_before"
}

check_source_template() {
  template=$1
  expected=$2
  label=$3
  check_expected_stage_name "$template" "$expected" "$label"
  [ -f "$template" ] && [ ! -L "$template" ] || fail "$label must be a regular non-symlink file"
  metadata=$(/usr/bin/stat -c '%u %g %a %h' "$template") || fail "cannot inspect staged $label"
  set -- $metadata
  [ "$#" -eq 4 ] && [ "$1" -eq 0 ] && [ "$2" -eq 0 ] && [ "$3" = 644 ] && [ "$4" -eq 1 ] || fail "$label must be root:root mode 0644 and singly linked"
  mountpoint=$(/usr/bin/findmnt -n -o TARGET --target "$template") || fail "cannot verify staged $label mount"
  [ "$mountpoint" = / ] || fail "$label source crosses a mount/bind-mount boundary"
  printf '%s\n' "$metadata"
}

fixed_line_count() {
  file=$1
  line=$2
  count=$(/usr/bin/grep -Fxc -- "$line" "$file" || true)
  printf '%s\n' "${count:-0}"
}

validate_unit_template() {
  template=$1
  kind=$2
  send_count=$(/usr/bin/grep -Ec '^[[:space:]]*SendSIGKILL[[:space:]]*=' "$template" || true)
  [ "${send_count:-0}" -eq 1 ] || fail "$kind template must contain exactly one SendSIGKILL directive"
  [ "$(fixed_line_count "$template" 'SendSIGKILL=yes')" -eq 1 ] || fail "$kind template must pin the exact SendSIGKILL=yes directive"
  if /usr/bin/grep -Fq 'SendSIGKILL=no' "$template"; then
    fail "$kind template may never contain SendSIGKILL=no"
  fi
  [ "$(fixed_line_count "$template" 'KillMode=control-group')" -eq 1 ] || fail "$kind template must pin KillMode=control-group"
  [ "$(fixed_line_count "$template" 'User=@A_USER@')" -eq 1 ] || fail "$kind template must contain exactly the @A_USER@ placeholder"
  [ "$(fixed_line_count "$template" 'Group=@A_GROUP@')" -eq 1 ] || fail "$kind template must contain exactly the @A_GROUP@ placeholder"
  [ "$(fixed_line_count "$template" 'NoNewPrivileges=no')" -eq 1 ] || fail "$kind template must not enable NoNewPrivileges"
  if [ "$kind" = owner ]; then
    [ "$(fixed_line_count "$template" 'Type=notify')" -eq 1 ] || fail 'owner template must wait for real anchor readiness'
    [ "$(fixed_line_count "$template" 'NotifyAccess=main')" -eq 1 ] || fail 'owner template notify access must be main-only'
    [ "$(fixed_line_count "$template" 'Delegate=yes')" -eq 1 ] || fail 'owner template must delegate its manager cgroup'
    [ "$(fixed_line_count "$template" 'DelegateSubgroup=manager')" -eq 1 ] || fail 'owner template must use the fixed manager subgroup'
    [ "$(fixed_line_count "$template" 'TimeoutStopSec=12min')" -eq 1 ] || fail 'owner template stop timeout must cover bounded recovery'
    [ "$(fixed_line_count "$template" 'ExecStart=/usr/libexec/symmetry/symmetry-daemon -linux-owner-anchor -cgroup-mount /sys/fs/cgroup -delegated-path system.slice/symmetry-owner.service/manager -socket /run/symmetry-owner/owner.sock')" -eq 1 ] || fail 'owner template launch command differs from the fixed anchor contract'
    [ "$(fixed_line_count "$template" 'ExecStop=/usr/libexec/symmetry/symmetry-daemon -recover-containment-only -config /etc/symmetry/daemon.json')" -eq 1 ] || fail 'owner template recovery ExecStop differs from the fixed recovery-only CLI'
  else
    [ "$(fixed_line_count "$template" 'Type=simple')" -eq 1 ] || fail 'daemon template must use truthful simple-process readiness'
    [ "$(fixed_line_count "$template" 'After=local-fs.target symmetry-owner.service')" -eq 1 ] || fail 'daemon template must order after the owner anchor'
    [ "$(fixed_line_count "$template" 'BindsTo=symmetry-owner.service')" -eq 1 ] || fail 'daemon template must bind to the owner service'
    [ "$(fixed_line_count "$template" 'TimeoutStopSec=2min')" -eq 1 ] || fail 'daemon template stop timeout must be bounded'
    [ "$(fixed_line_count "$template" 'ExecStartPre=/usr/libexec/symmetry/symmetry-daemon -verify-linux-owner-install -config /etc/symmetry/daemon.json')" -eq 1 ] || fail 'daemon template must run the fixed read-only owner installation preflight'
    [ "$(fixed_line_count "$template" 'ExecStart=/usr/libexec/symmetry/symmetry-daemon -config /etc/symmetry/daemon.json')" -eq 1 ] || fail 'daemon template ExecStart differs from the installed daemon/config paths'
  fi
}

render_unit_template() {
  source=$1
  destination=$2
  expected=$3
  label=$4
  before=$(check_source_template "$source" "$expected" "$label") || fail "$label source is not trusted"
  source_hash=$(sha256_file "$source")
  /usr/bin/sed -e "s/@A_USER@/$A_USER/g" -e "s/@A_GROUP@/$A_GROUP_NAME/g" "$source" > "$destination" || fail "render $label"
  /usr/bin/chown root:root "$destination"
  /usr/bin/chmod 0644 "$destination"
  after=$(check_source_template "$source" "$expected" "$label") || fail "$label source changed during rendering"
  [ "$before" = "$after" ] && [ "$(sha256_file "$source")" = "$source_hash" ] || fail "$label source changed during rendering"
  if /usr/bin/grep -Fq '@' "$destination"; then
    fail "$label has an unresolved or unsupported template placeholder"
  fi
  if /usr/bin/grep -Fq 'SendSIGKILL=no' "$destination"; then
    fail "$label rendered a forbidden SendSIGKILL=no directive"
  fi
  validate_rendered_unit "$destination" "$label"
  RENDERED_TEMPLATE_HASH=$source_hash
}

validate_rendered_unit() {
  file=$1
  label=$2
  [ "$(fixed_line_count "$file" "User=$A_USER")" -eq 1 ] || fail "$label rendered User differs from passwd-resolved A"
  [ "$(fixed_line_count "$file" "Group=$A_GROUP_NAME")" -eq 1 ] || fail "$label rendered Group differs from A's primary group"
  [ "$(fixed_line_count "$file" 'SendSIGKILL=yes')" -eq 1 ] || fail "$label did not retain SendSIGKILL=yes"
  [ "$(fixed_line_count "$file" 'KillMode=control-group')" -eq 1 ] || fail "$label did not retain KillMode=control-group"
  if /usr/bin/grep -Fq 'SendSIGKILL=no' "$file"; then
    fail "$label rendered the forbidden SendSIGKILL=no directive"
  fi
}

verify_artifact_copy() {
  source=$1
  destination=$2
  expected_hash=$3
  label=$4
  expected_gid=$5
  expected_mode=$6
  source_identity=$(check_source_artifact "$source" "$label") || fail "$label source changed before copy verification"
  source_hash=$(sha256_file "$source")
  destination_hash=$(sha256_file "$destination")
  [ "$source_hash" = "$expected_hash" ] && [ "$destination_hash" = "$expected_hash" ] || fail "$label copied bytes differ from inspected staged ELF"
  metadata=$(/usr/bin/stat -c '%u %g %a %h' "$destination") || fail "cannot inspect copied $label"
  set -- $metadata
  [ "$#" -eq 4 ] && [ "$1" -eq 0 ] && [ "$2" = "$expected_gid" ] && [ "$3" = "$expected_mode" ] && [ "$4" -eq 1 ] || fail "copied $label has unexpected owner, group, mode, or link count"
}

stage_artifact_if_missing() {
  source=$1
  temporary=$2
  destination=$3
  hash=$4
  label=$5
  expected_gid=$6
  expected_mode=$7
  if [ -e "$destination" ] || [ -L "$destination" ]; then
    verify_published_file "$destination" "$hash" "$expected_gid" "$expected_mode" "$label"
    return
  fi
  [ -f "$temporary" ] && [ ! -L "$temporary" ] || fail "missing root-created staging file for $label"
  /usr/bin/install -o root -g "$expected_gid" -m "$expected_mode" "$source" "$temporary"
  verify_artifact_copy "$source" "$temporary" "$hash" "$label" "$expected_gid" "$expected_mode"
}

stage_wrapper_if_missing() {
  source=$1
  temporary=$2
  destination=$3
  hash=$4
  expected_gid=$5
  if [ -e "$destination" ] || [ -L "$destination" ]; then
    verify_published_file "$destination" "$hash" "$expected_gid" 4750 'setuid launcher'
    return
  fi
  [ -f "$temporary" ] && [ ! -L "$temporary" ] || fail 'missing root-created staging file for setuid launcher'
  /usr/bin/install -o root -g "$expected_gid" -m 0750 "$source" "$temporary"
  verify_artifact_copy "$source" "$temporary" "$hash" 'setuid launcher' "$expected_gid" 750
  /usr/bin/chmod 4750 "$temporary"
  metadata=$(/usr/bin/stat -c '%u %g %a %h' "$temporary") || fail 'cannot inspect setuid launcher staging copy'
  set -- $metadata
  [ "$#" -eq 4 ] && [ "$1" -eq 0 ] && [ "$2" = "$expected_gid" ] && [ "$3" = 4750 ] && [ "$4" -eq 1 ] || fail 'setuid launcher staging copy has unexpected ownership, mode, or link count'
}

verify_published_file() {
  path=$1
  hash=$2
  expected_gid=$3
  expected_mode=$4
  label=$5
  [ -f "$path" ] && [ ! -L "$path" ] || fail "published $label is not a regular non-symlink file"
  # Every install parent was checked root-owned and non-writable by A/B. Reject
  # a symlink before opening, then pin one root-open FD for metadata and hashing.
  exec 8< "$path" || fail "root-open published $label"
  metadata=$(/usr/bin/stat -L -c '%u %g %a %h' "/proc/$$/fd/8") || fail "cannot inspect pinned published $label"
  set -- $metadata
  [ "$#" -eq 4 ] && [ "$1" -eq 0 ] && [ "$2" = "$expected_gid" ] && [ "$3" = "$expected_mode" ] && [ "$4" -eq 1 ] || fail "published $label has unexpected owner, group, mode, or link count"
  hash_line=$(/usr/bin/sha256sum <&8) || fail "hash pinned published $label"
  exec 8<&-
  [ "${hash_line%% *}" = "$hash" ] || fail "published $label digest differs from the inspected stage"
}

verify_existing_or_absent() {
  path=$1
  hash=$2
  expected_gid=$3
  expected_mode=$4
  label=$5
  if [ -e "$path" ] || [ -L "$path" ]; then
    verify_published_file "$path" "$hash" "$expected_gid" "$expected_mode" "$label"
  fi
}

require_absent_install_path() {
  path=$1
  label=$2
  [ ! -e "$path" ] && [ ! -L "$path" ] || fail "$label exists without a matching durable install-intent marker"
}

check_installed_directory() {
  directory=$1
  [ -d "$directory" ] && [ ! -L "$directory" ] || fail "installed directory $directory is not a real directory"
  metadata=$(/usr/bin/stat -c '%u %g %a' "$directory") || fail "cannot inspect installed directory $directory"
  set -- $metadata
  [ "$#" -eq 3 ] && [ "$1" -eq 0 ] && [ "$2" -eq 0 ] && [ "$3" = 755 ] || fail "installed directory $directory must be root:root mode 0755"
}

# Temporary files live beside their final path. On supported Linux hosts, GNU
# mv's no-clobber rename publishes atomically without replacing an existing
# path; this avoids the link-then-unlink crash window during resume.
publish_noreplace() {
  source=$1
  destination=$2
  label=$3
  [ ! -e "$destination" ] && [ ! -L "$destination" ] || fail "refusing to replace existing $label"
  /usr/bin/mv --no-clobber --no-target-directory "$source" "$destination" || fail "atomically publish $label without replacement"
  [ ! -e "$source" ] && [ ! -L "$source" ] || fail "refusing a concurrent existing destination while publishing $label"
}

publish_or_verify() {
  source=$1
  destination=$2
  hash=$3
  expected_gid=$4
  expected_mode=$5
  label=$6
  if [ -e "$destination" ] || [ -L "$destination" ]; then
    verify_published_file "$destination" "$hash" "$expected_gid" "$expected_mode" "$label"
    if [ -n "$source" ] && { [ -e "$source" ] || [ -L "$source" ]; }; then
      /usr/bin/rm -f "$source" || fail "remove redundant staged $label"
    fi
    return
  fi
  [ -n "$source" ] && [ -f "$source" ] && [ ! -L "$source" ] || fail "missing staged file for $label"
  publish_noreplace "$source" "$destination" "$label"
  verify_published_file "$destination" "$hash" "$expected_gid" "$expected_mode" "$label"
}

check_old_unit_state() {
  [ -d /run/systemd/system ] || fail 'a running systemd system manager is required for the owner cutover'
  [ -x /usr/bin/systemctl ] || fail 'systemctl is required for the owner cutover'
  for unit in symmetry-owner.service symmetry-daemon.service; do
    active_state=$(/usr/bin/systemctl --system show --property=ActiveState --value "$unit" 2>/dev/null) || fail "cannot query old unit $unit"
    case "$active_state" in inactive|failed) ;; *) fail "old unit $unit must be inactive before installer checks (state=$active_state)";; esac
    enabled_status=0
    if enabled_state=$(/usr/bin/systemctl --system is-enabled "$unit" 2>/dev/null); then
      enabled_status=0
    else
      enabled_status=$?
    fi
    case "$enabled_state" in
      disabled) ;;
      not-found) [ "$enabled_status" -ne 0 ] || fail "systemd returned inconsistent enabled state for $unit" ;;
      *) fail "old unit $unit must be disabled before installer checks (state=$enabled_state)" ;;
    esac
  done
}

check_unsupported_systemd_overrides_absent() {
  for unit_dir in /usr/lib/systemd/system /lib/systemd/system /run/systemd/system; do
    for unit in symmetry-owner.service symmetry-daemon.service; do
      path=$unit_dir/$unit
      [ ! -e "$path" ] && [ ! -L "$path" ] || fail "existing systemd unit $path is an unsupported upgrade"
    done
  done
  for unit_dir in /etc/systemd/system /usr/lib/systemd/system /lib/systemd/system /run/systemd/system; do
    for unit in symmetry-owner.service symmetry-daemon.service; do
      dropins=$unit_dir/$unit.d
      [ ! -e "$dropins" ] && [ ! -L "$dropins" ] || fail "existing systemd drop-ins $dropins are an unsupported upgrade"
    done
  done
  for unit in symmetry-owner.service symmetry-daemon.service; do
    for unit_dir in /etc/systemd/system /usr/lib/systemd/system /run/systemd/system; do
      if [ -d "$unit_dir" ]; then
        links=$(/usr/bin/find "$unit_dir" -type l -name "$unit" -print) || fail "cannot inspect existing systemd enablement links for $unit"
        [ -z "$links" ] || fail "existing systemd enablement link for $unit is an unsupported upgrade: $links"
      fi
    done
  done
}

prepare_drain_daemon() {
  [ -x /usr/bin/setpriv ] || fail 'setpriv is required to run trusted daemon config probes as A'
  if [ -n "$TMP_DRAIN_DAEMON" ]; then return; fi
  TMP_DRAIN_DIR=$(/usr/bin/mktemp -d /usr/libexec/.symmetry-linux-owner-drain.XXXXXX) || fail 'create unique temporary daemon-probe directory'
  TMP_DRAIN_DAEMON=$TMP_DRAIN_DIR/symmetry-daemon
  CREATED_DRAIN_DIR=1
  /usr/bin/chown root:root "$TMP_DRAIN_DIR"
  /usr/bin/chmod 0711 "$TMP_DRAIN_DIR"
  check_root_directory "$TMP_DRAIN_DIR"
  /usr/bin/install -o root -g root -m 0755 "$DAEMON" "$TMP_DRAIN_DAEMON"
  verify_artifact_copy "$DAEMON" "$TMP_DRAIN_DAEMON" "$DAEMON_HASH" 'daemon probe image' 0 755
}

release_drain_daemon() {
  if [ -z "$TMP_DRAIN_DIR" ]; then return; fi
  /usr/bin/rm -f "$TMP_DRAIN_DAEMON"
  /usr/bin/rmdir "$TMP_DRAIN_DIR" || fail 'remove temporary daemon-probe directory'
  CREATED_DRAIN_DIR=0
  TMP_DRAIN_DAEMON=
  TMP_DRAIN_DIR=
}

read_configured_state_dir() {
  prepare_drain_daemon
  STATE_DIR=$(/usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LANG=C HOME=/nonexistent \
    /usr/bin/setpriv --reuid="$A_UID" --regid="$A_GID" --clear-groups \
      --inh-caps=-all --ambient-caps=-all --bounding-set=-all --no-new-privs -- \
      "$TMP_DRAIN_DAEMON" -print-state-dir -config /etc/symmetry/daemon.json) || \
      fail 'the staged daemon could not read its configured StateDir as A'
  case "$STATE_DIR" in /*) ;; *) fail 'the staged daemon returned a non-absolute StateDir' ;; esac
  [ -n "$STATE_DIR" ] && [ "$STATE_DIR" != / ] && [ "${#STATE_DIR}" -le 4095 ] || fail 'the staged daemon returned an invalid StateDir'
  case "$STATE_DIR" in
    *//*|*/./*|*/../*|*/.|*/..|*/) fail 'the staged daemon returned a noncanonical StateDir' ;;
  esac
}

run_legacy_drain() {
  [ -f /etc/symmetry/daemon.json ] && [ ! -L /etc/symmetry/daemon.json ] || fail 'the existing /etc/symmetry/daemon.json is required for the one-time legacy drain'
  prepare_drain_daemon
  /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LANG=C HOME=/nonexistent \
    /usr/bin/setpriv --reuid="$A_UID" --regid="$A_GID" --clear-groups \
      --inh-caps=-all --ambient-caps=-all --bounding-set=-all --no-new-privs -- \
      "$TMP_DRAIN_DAEMON" -verify-legacy-owner-drained -config /etc/symmetry/daemon.json || \
      fail 'the new pinned daemon refused the one-time read-only legacy drain proof'
  release_drain_daemon
}

[ "$(/usr/bin/id -u)" -eq 0 ] || fail 'must run as root'
HOST_ARCH=$(/usr/bin/uname -m)
[ "$HOST_ARCH" = x86_64 ] || fail "unsupported host architecture $HOST_ARCH (supported: Linux amd64 only)"
ELF_MACHINE='Advanced Micro Devices X86-64'
[ "$#" -eq 9 ] || usage

A_USER=$1
B_USER=$2
WRAPPER=$3
CORE=$4
GATE=$5
BOOTSTRAP=$6
DAEMON=$7
OWNER_TEMPLATE=$8
DAEMON_TEMPLATE=$9
ROOT_STAGE=/root/symmetry-linux-owner-stage
DEST=/usr/libexec/symmetry
ETC=/etc/symmetry
SYSTEMD_DIR=/etc/systemd/system
INSTALL_MARKER=$ETC/linux-owner-install.intent
INSTALL_INTENT_EXISTS=0
LEGACY_DRAIN_THIS_RUN=0
CREATED_DRAIN_DIR=0
TMP_DRAIN_DIR=
TMP_DRAIN_DAEMON=
STATE_DIR=
TMP_WRAPPER=
TMP_CORE=
TMP_GATE=
TMP_BOOTSTRAP=
TMP_DAEMON=
TMP_CONFIG=
TMP_OWNER_UNIT=
TMP_DAEMON_UNIT=
TMP_INTENT=

check_old_unit_state
case "$A_USER" in [A-Za-z0-9_]*) ;; *) fail 'A_USER must start with a simple account-name character';; esac
case "$A_USER" in *[!A-Za-z0-9_.-]*) fail 'A_USER must be a simple local account name';; esac
case "$B_USER" in [A-Za-z0-9_]*) ;; *) fail 'B_USER must start with a simple account-name character';; esac
case "$B_USER" in *[!A-Za-z0-9_.-]*) fail 'B_USER must be a simple local account name';; esac
[ "$A_USER" != "$B_USER" ] || fail 'A and B accounts must differ'

passwd_fields() { /usr/bin/getent passwd "$1" || fail "account $1 does not exist"; }
A_ENTRY=$(passwd_fields "$A_USER")
B_ENTRY=$(passwd_fields "$B_USER")
[ "$(printf '%s\n' "$A_ENTRY" | /usr/bin/cut -d: -f1)" = "$A_USER" ] || fail 'A account lookup was ambiguous'
[ "$(printf '%s\n' "$B_ENTRY" | /usr/bin/cut -d: -f1)" = "$B_USER" ] || fail 'B account lookup was ambiguous'
A_UID=$(printf '%s\n' "$A_ENTRY" | /usr/bin/cut -d: -f3)
A_GID=$(printf '%s\n' "$A_ENTRY" | /usr/bin/cut -d: -f4)
B_UID=$(printf '%s\n' "$B_ENTRY" | /usr/bin/cut -d: -f3)
B_GID=$(printf '%s\n' "$B_ENTRY" | /usr/bin/cut -d: -f4)
for value in "$A_UID" "$A_GID" "$B_UID" "$B_GID"; do
  case "$value" in ''|*[!0-9]*) fail 'account lookup returned a non-numeric UID/GID';; esac
done
[ "$A_UID" -gt 0 ] && [ "$B_UID" -gt 0 ] || fail 'A and B must be non-root accounts'
[ "$A_GID" -gt 0 ] && [ "$B_GID" -gt 0 ] || fail 'A and B primary groups must be non-root'
[ "$A_UID" != "$B_UID" ] || fail 'A and B UIDs must differ'
[ "$A_GID" != "$B_GID" ] || fail 'A and B primary GIDs must differ'
A_GROUP_ENTRY=$(/usr/bin/getent group "$A_GID") || fail 'A primary group does not exist'
A_GROUP_NAME=$(printf '%s\n' "$A_GROUP_ENTRY" | /usr/bin/cut -d: -f1)
[ "$(printf '%s\n' "$A_GROUP_ENTRY" | /usr/bin/cut -d: -f3)" = "$A_GID" ] || fail 'A primary group lookup was ambiguous'
case "$A_GROUP_NAME" in [A-Za-z0-9_]*) ;; *) fail 'A primary group name must start with a simple account-name character';; esac
case "$A_GROUP_NAME" in *[!A-Za-z0-9_.-]*) fail 'A primary group name is not a simple local name';; esac
PASSWD_ENTRIES=$(/usr/bin/getent passwd) || fail 'cannot enumerate passwd entries to verify dedicated A group'
other_primary_member=0
while IFS=: read -r account _ uid gid rest; do
  if [ "$gid" = "$A_GID" ] && [ "$account" != "$A_USER" ]; then other_primary_member=1; fi
done <<EOF
$PASSWD_ENTRIES
EOF
[ "$other_primary_member" -eq 0 ] || fail 'A primary group must not be another account primary group; use a dedicated service group'
A_GROUP_MEMBERS=$(printf '%s\n' "$A_GROUP_ENTRY" | /usr/bin/cut -d: -f4)
for group_member in $(printf '%s' "$A_GROUP_MEMBERS" | /usr/bin/tr ',' ' '); do
  [ "$group_member" = "$A_USER" ] || fail "A service group has another supplementary member: $group_member"
done
B_GROUPS=$(/usr/bin/id -G "$B_USER") || fail 'cannot enumerate B supplementary groups'
for group_id in $B_GROUPS; do
  [ "$group_id" != "$A_GID" ] || fail 'B must not be a member of the dedicated A service group'
done

check_private_stage
LOCK_FILE=$ROOT_STAGE/.linux-owner-installer.lock
if [ -e "$LOCK_FILE" ] || [ -L "$LOCK_FILE" ]; then
  [ -f "$LOCK_FILE" ] && [ ! -L "$LOCK_FILE" ] || fail 'installer lock path is not a regular root file'
  lock_metadata=$(/usr/bin/stat -c '%u %g %a %h' "$LOCK_FILE") || fail 'cannot inspect installer lock file'
  set -- $lock_metadata
  [ "$#" -eq 4 ] && [ "$1" -eq 0 ] && [ "$2" -eq 0 ] && [ "$3" = 600 ] && [ "$4" -eq 1 ] || fail 'installer lock file must be root:root mode 0600 and singly linked'
else
  /usr/bin/install -o root -g root -m 0600 /dev/null "$LOCK_FILE"
fi
exec 9>>"$LOCK_FILE"
/usr/bin/flock -n 9 || fail 'another Linux owner installer is active'

for path in /usr /etc /usr/libexec /etc/systemd /etc/systemd/system /etc/symmetry; do
  [ ! -L "$path" ] || fail "$path must not be a symlink"
done
check_root_directory /etc
check_root_directory /usr
if [ ! -e /usr/libexec ]; then
  /usr/bin/install -d -o root -g root -m 0755 /usr/libexec
fi
check_root_directory /usr/libexec
check_root_directory "$SYSTEMD_DIR"
[ -d "$ETC" ] && [ ! -L "$ETC" ] || fail 'the existing root-owned /etc/symmetry config directory is required'
check_root_directory "$ETC"
[ -f /etc/symmetry/daemon.json ] && [ ! -L /etc/symmetry/daemon.json ] || fail 'the existing daemon config must be a regular non-symlink file'
config_metadata=$(/usr/bin/stat -c '%u %g %a %h' /etc/symmetry/daemon.json) || fail 'cannot inspect the daemon config'
set -- $config_metadata
[ "$#" -eq 4 ] && { [ "$1" -eq 0 ] || [ "$1" -eq "$A_UID" ]; } &&
	{ [ "$2" -eq 0 ] || [ "$2" -eq "$A_GID" ]; } && [ "$4" -eq 1 ] || fail 'daemon config must be singly-linked and owned by root or A'
config_permissions=$(printf '%d' "0$3") || fail 'cannot parse daemon config permissions'
[ $((config_permissions & 18)) -eq 0 ] || fail 'daemon config is group/world writable'
DAEMON_CONFIG_METADATA=$config_metadata
DAEMON_CONFIG_HASH=$(sha256_file /etc/symmetry/daemon.json)
if [ -e "$INSTALL_MARKER" ] || [ -L "$INSTALL_MARKER" ]; then
  INSTALL_INTENT_EXISTS=1
fi
if [ -L /run/symmetry-owner ]; then
  fail 'the v2 owner runtime directory must not be a symlink'
fi
if [ -e /run/symmetry-owner/owner.sock ] || [ -L /run/symmetry-owner/owner.sock ]; then
  fail 'a live or stale v2 owner socket prevents installer resume'
fi
check_unsupported_systemd_overrides_absent

[ "${WRAPPER##*/}" = symmetry-linux-launcher ] || fail 'staged setuid wrapper must be named symmetry-linux-launcher'
[ "${CORE##*/}" = symmetry-linux-launcher-core ] || fail 'staged launcher core must be named symmetry-linux-launcher-core'
[ "${GATE##*/}" = symmetry-linux-gate ] || fail 'staged A gate must be named symmetry-linux-gate'
[ "${BOOTSTRAP##*/}" = symmetry-linux-clone-bootstrap ] || fail 'staged B clone bootstrap must use its fixed installed name'
[ "${DAEMON##*/}" = symmetry-daemon ] || fail 'staged daemon must be named symmetry-daemon'
[ "${OWNER_TEMPLATE##*/}" = symmetry-owner.service.in ] || fail 'staged owner template must be symmetry-owner.service.in'
[ "${DAEMON_TEMPLATE##*/}" = symmetry-daemon.service.in ] || fail 'staged daemon template must be symmetry-daemon.service.in'

WRAPPER_HASH=$(check_static_elf "$WRAPPER" 'setuid wrapper')
CORE_HASH=$(check_static_elf "$CORE" 'launcher core')
GATE_HASH=$(check_static_elf "$GATE" 'A gate')
BOOTSTRAP_HASH=$(check_static_elf "$BOOTSTRAP" 'B clone bootstrap')
DAEMON_HASH=$(check_static_elf "$DAEMON" 'daemon')
check_source_template "$OWNER_TEMPLATE" 'symmetry-owner.service.in' 'owner unit template' >/dev/null
check_source_template "$DAEMON_TEMPLATE" 'symmetry-daemon.service.in' 'daemon unit template' >/dev/null
validate_unit_template "$OWNER_TEMPLATE" owner
validate_unit_template "$DAEMON_TEMPLATE" daemon

cleanup() {
  for temp_path in "$TMP_WRAPPER" "$TMP_CORE" "$TMP_GATE" "$TMP_BOOTSTRAP" "$TMP_DAEMON" \
    "$TMP_CONFIG" "$TMP_OWNER_UNIT" "$TMP_DAEMON_UNIT" "$TMP_INTENT"; do
    [ -z "$temp_path" ] || /usr/bin/rm -f "$temp_path"
  done
  if [ "${CREATED_DRAIN_DIR:-0}" -eq 1 ]; then
    /usr/bin/rm -f "$TMP_DRAIN_DAEMON"
    /usr/bin/rmdir "$TMP_DRAIN_DIR" 2>/dev/null || true
  fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

read_configured_state_dir
TMP_CONFIG=$(/usr/bin/mktemp "$ETC/.linux-owner-config.XXXXXX") || fail 'create temporary fixed owner config'
TMP_OWNER_UNIT=$(/usr/bin/mktemp "$SYSTEMD_DIR/.symmetry-owner-unit.XXXXXX") || fail 'create temporary owner unit'
TMP_DAEMON_UNIT=$(/usr/bin/mktemp "$SYSTEMD_DIR/.symmetry-daemon-unit.XXXXXX") || fail 'create temporary daemon unit'
TMP_INTENT=$(/usr/bin/mktemp "$ETC/.linux-owner-intent.XXXXXX") || fail 'create temporary install-intent manifest'

{
  printf 'version=2\n'
  printf 'a_uid=%s\n' "$A_UID"
  printf 'a_gid=%s\n' "$A_GID"
  printf 'b_uid=%s\n' "$B_UID"
  printf 'b_gid=%s\n' "$B_GID"
  printf 'state_dir=%s\n' "$STATE_DIR"
} > "$TMP_CONFIG"
/usr/bin/chown root:root "$TMP_CONFIG"
/usr/bin/chmod 0644 "$TMP_CONFIG"
render_unit_template "$OWNER_TEMPLATE" "$TMP_OWNER_UNIT" 'symmetry-owner.service.in' 'owner unit template'
OWNER_TEMPLATE_HASH=$RENDERED_TEMPLATE_HASH
render_unit_template "$DAEMON_TEMPLATE" "$TMP_DAEMON_UNIT" 'symmetry-daemon.service.in' 'daemon unit template'
DAEMON_TEMPLATE_HASH=$RENDERED_TEMPLATE_HASH
CONFIG_HASH=$(sha256_file "$TMP_CONFIG")
OWNER_UNIT_HASH=$(sha256_file "$TMP_OWNER_UNIT")
DAEMON_UNIT_HASH=$(sha256_file "$TMP_DAEMON_UNIT")
CURRENT_CONFIG_METADATA=$(/usr/bin/stat -c '%u %g %a %h' /etc/symmetry/daemon.json) || fail 'cannot re-inspect daemon config before install intent'
CURRENT_CONFIG_HASH=$(sha256_file /etc/symmetry/daemon.json)
[ "$CURRENT_CONFIG_METADATA" = "$DAEMON_CONFIG_METADATA" ] && [ "$CURRENT_CONFIG_HASH" = "$DAEMON_CONFIG_HASH" ] || fail 'daemon config changed while preparing the owner install'
[ "$(sha256_file "$OWNER_TEMPLATE")" = "$OWNER_TEMPLATE_HASH" ] &&
	[ "$(sha256_file "$DAEMON_TEMPLATE")" = "$DAEMON_TEMPLATE_HASH" ] || fail 'unit template changed while preparing the owner install'

{
  printf 'format=linux-owner-install-intent-v2\n'
  printf 'install_root=%s\n' "$DEST"
  printf 'config_root=%s\n' "$ETC"
  printf 'systemd_root=%s\n' "$SYSTEMD_DIR"
  printf 'a_user=%s\n' "$A_USER"
  printf 'a_group=%s\n' "$A_GROUP_NAME"
  printf 'a_uid=%s\n' "$A_UID"
  printf 'a_gid=%s\n' "$A_GID"
  printf 'b_user=%s\n' "$B_USER"
  printf 'b_uid=%s\n' "$B_UID"
  printf 'b_gid=%s\n' "$B_GID"
  printf 'state_dir=%s\n' "$STATE_DIR"
  printf 'daemon_config_metadata=%s\n' "$DAEMON_CONFIG_METADATA"
  printf 'daemon_config_sha256=%s\n' "$DAEMON_CONFIG_HASH"
  printf 'wrapper_sha256=%s\n' "$WRAPPER_HASH"
  printf 'launcher_core_sha256=%s\n' "$CORE_HASH"
  printf 'gate_sha256=%s\n' "$GATE_HASH"
  printf 'clone_bootstrap_sha256=%s\n' "$BOOTSTRAP_HASH"
  printf 'daemon_sha256=%s\n' "$DAEMON_HASH"
  printf 'owner_template_sha256=%s\n' "$OWNER_TEMPLATE_HASH"
  printf 'daemon_template_sha256=%s\n' "$DAEMON_TEMPLATE_HASH"
  printf 'owner_config_sha256=%s\n' "$CONFIG_HASH"
  printf 'owner_unit_sha256=%s\n' "$OWNER_UNIT_HASH"
  printf 'daemon_unit_sha256=%s\n' "$DAEMON_UNIT_HASH"
} > "$TMP_INTENT"
/usr/bin/chown root:root "$TMP_INTENT"
/usr/bin/chmod 0600 "$TMP_INTENT"
MANIFEST_HASH=$(sha256_file "$TMP_INTENT")
[ -x /usr/bin/sync ] || fail 'coreutils sync is required to durably publish the install-intent marker'

if [ "$INSTALL_INTENT_EXISTS" -eq 0 ]; then
  require_absent_install_path "$DEST" 'owner asset directory'
  require_absent_install_path "$ETC/linux-owner.conf" 'fixed A/B owner config'
  require_absent_install_path "$SYSTEMD_DIR/symmetry-owner.service" 'owner unit'
  require_absent_install_path "$SYSTEMD_DIR/symmetry-daemon.service" 'daemon unit'
fi

if [ "$INSTALL_INTENT_EXISTS" -eq 1 ]; then
  verify_published_file "$INSTALL_MARKER" "$MANIFEST_HASH" 0 600 'root-owned install-intent marker'
  /usr/bin/cmp -s "$INSTALL_MARKER" "$TMP_INTENT" || fail 'install-intent marker differs from the staged version, daemon config, or A/B identities'
else
  run_legacy_drain
  CURRENT_CONFIG_METADATA=$(/usr/bin/stat -c '%u %g %a %h' /etc/symmetry/daemon.json) || fail 'cannot re-inspect daemon config after legacy drain'
  CURRENT_CONFIG_HASH=$(sha256_file /etc/symmetry/daemon.json)
  [ "$CURRENT_CONFIG_METADATA" = "$DAEMON_CONFIG_METADATA" ] && [ "$CURRENT_CONFIG_HASH" = "$DAEMON_CONFIG_HASH" ] || fail 'daemon config changed during the one-time legacy drain'
  /usr/bin/sync -f "$TMP_INTENT" || fail 'fsync the root-owned install-intent manifest before publication'
  publish_noreplace "$TMP_INTENT" "$INSTALL_MARKER" 'root-owned install-intent marker'
  /usr/bin/sync -f "$ETC" || fail 'fsync the install-intent marker directory before asset publication'
  verify_published_file "$INSTALL_MARKER" "$MANIFEST_HASH" 0 600 'root-owned install-intent marker'
  LEGACY_DRAIN_THIS_RUN=1
fi

if [ -e "$DEST" ] || [ -L "$DEST" ]; then
  check_installed_directory "$DEST"
fi
verify_existing_or_absent "$DEST/symmetry-linux-launcher-core" "$CORE_HASH" 0 755 'launcher core'
verify_existing_or_absent "$DEST/symmetry-linux-gate" "$GATE_HASH" 0 755 'A gate'
verify_existing_or_absent "$DEST/symmetry-linux-clone-bootstrap" "$BOOTSTRAP_HASH" 0 755 'B clone bootstrap'
verify_existing_or_absent "$DEST/symmetry-daemon" "$DAEMON_HASH" 0 755 'daemon image'
verify_existing_or_absent "$DEST/symmetry-linux-launcher" "$WRAPPER_HASH" "$A_GID" 4750 'setuid launcher'
verify_existing_or_absent "$ETC/linux-owner.conf" "$CONFIG_HASH" 0 644 'fixed A/B config'
verify_existing_or_absent "$SYSTEMD_DIR/symmetry-owner.service" "$OWNER_UNIT_HASH" 0 644 'owner unit'
verify_existing_or_absent "$SYSTEMD_DIR/symmetry-daemon.service" "$DAEMON_UNIT_HASH" 0 644 'daemon unit'

if [ ! -e "$DEST" ] && [ ! -L "$DEST" ]; then
  /usr/bin/install -d -o root -g root -m 0755 "$DEST" || fail 'create fixed owner asset directory after durable install intent'
fi
check_installed_directory "$DEST"
TMP_WRAPPER=$(/usr/bin/mktemp "$DEST/.linux-owner-wrapper.XXXXXX") || fail 'create temporary setuid launcher'
TMP_CORE=$(/usr/bin/mktemp "$DEST/.linux-owner-core.XXXXXX") || fail 'create temporary launcher core'
TMP_GATE=$(/usr/bin/mktemp "$DEST/.linux-owner-gate.XXXXXX") || fail 'create temporary A gate'
TMP_BOOTSTRAP=$(/usr/bin/mktemp "$DEST/.linux-owner-bootstrap.XXXXXX") || fail 'create temporary B clone bootstrap'
TMP_DAEMON=$(/usr/bin/mktemp "$DEST/.linux-owner-daemon.XXXXXX") || fail 'create temporary daemon image'

stage_artifact_if_missing "$CORE" "$TMP_CORE" "$DEST/symmetry-linux-launcher-core" "$CORE_HASH" 'launcher core' 0 755
stage_artifact_if_missing "$GATE" "$TMP_GATE" "$DEST/symmetry-linux-gate" "$GATE_HASH" 'A gate' 0 755
stage_artifact_if_missing "$BOOTSTRAP" "$TMP_BOOTSTRAP" "$DEST/symmetry-linux-clone-bootstrap" "$BOOTSTRAP_HASH" 'B clone bootstrap' 0 755
stage_artifact_if_missing "$DAEMON" "$TMP_DAEMON" "$DEST/symmetry-daemon" "$DAEMON_HASH" 'daemon' 0 755
stage_wrapper_if_missing "$WRAPPER" "$TMP_WRAPPER" "$DEST/symmetry-linux-launcher" "$WRAPPER_HASH" "$A_GID"

publish_or_verify "$TMP_CORE" "$DEST/symmetry-linux-launcher-core" "$CORE_HASH" 0 755 'launcher core'
publish_or_verify "$TMP_GATE" "$DEST/symmetry-linux-gate" "$GATE_HASH" 0 755 'A gate'
publish_or_verify "$TMP_BOOTSTRAP" "$DEST/symmetry-linux-clone-bootstrap" "$BOOTSTRAP_HASH" 0 755 'B clone bootstrap'
publish_or_verify "$TMP_DAEMON" "$DEST/symmetry-daemon" "$DAEMON_HASH" 0 755 'daemon image'
publish_or_verify "$TMP_CONFIG" "$ETC/linux-owner.conf" "$CONFIG_HASH" 0 644 'fixed A/B config'
publish_or_verify "$TMP_WRAPPER" "$DEST/symmetry-linux-launcher" "$WRAPPER_HASH" "$A_GID" 4750 'setuid launcher'
publish_or_verify "$TMP_OWNER_UNIT" "$SYSTEMD_DIR/symmetry-owner.service" "$OWNER_UNIT_HASH" 0 644 'owner unit'
publish_or_verify "$TMP_DAEMON_UNIT" "$SYSTEMD_DIR/symmetry-daemon.service" "$DAEMON_UNIT_HASH" 0 644 'daemon unit'
verify_published_file "$DEST/symmetry-linux-launcher-core" "$CORE_HASH" 0 755 'launcher core'
verify_published_file "$DEST/symmetry-linux-gate" "$GATE_HASH" 0 755 'A gate'
verify_published_file "$DEST/symmetry-linux-clone-bootstrap" "$BOOTSTRAP_HASH" 0 755 'B clone bootstrap'
verify_published_file "$DEST/symmetry-daemon" "$DAEMON_HASH" 0 755 'daemon'
verify_published_file "$DEST/symmetry-linux-launcher" "$WRAPPER_HASH" "$A_GID" 4750 'setuid launcher'
verify_published_file "$ETC/linux-owner.conf" "$CONFIG_HASH" 0 644 'fixed A/B config'
verify_published_file "$SYSTEMD_DIR/symmetry-owner.service" "$OWNER_UNIT_HASH" 0 644 'owner unit'
verify_published_file "$SYSTEMD_DIR/symmetry-daemon.service" "$DAEMON_UNIT_HASH" 0 644 'daemon unit'

# Unit files are inert until an operator explicitly enables/starts them. A
# manager reload makes the published templates visible without activation.
/usr/bin/systemctl --system daemon-reload || fail 'systemd daemon-reload failed after publishing fixed units'
release_drain_daemon
trap - EXIT HUP INT TERM
printf 'Verified fixed Linux owner assets for A=%s(%s:%s) B=%s(%s:%s).\n' \
  "$A_USER" "$A_UID" "$A_GID" "$B_USER" "$B_UID" "$B_GID"
if [ "$LEGACY_DRAIN_THIS_RUN" -eq 1 ]; then
  printf 'The one-time legacy drain proof passed and its install-intent marker is durable.\n'
else
  printf 'The matching install-intent marker was verified; no second legacy drain was run.\n'
fi
printf 'No unit was enabled, started, stopped, or disabled. After operator review, enable both units, start symmetry-owner.service, then start symmetry-daemon.service.\n'
