#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

: "${RUNNER_TEMP:?runner temporary directory is required}"
: "${GITHUB_PATH:?GitHub PATH output is required}"
: "${GITHUB_SHA:?workflow source revision is required}"
: "${GITHUB_RUN_ID:?workflow run ID is required}"
: "${GITHUB_RUN_ATTEMPT:?workflow attempt is required}"
: "${SYMMETRY_CI_RUNNER_ENVIRONMENT:?runner environment is required}"
[[ "${GITHUB_ACTIONS:-}" == true && "$SYMMETRY_CI_RUNNER_ENVIRONMENT" == github-hosted ]] || {
  printf 'Private namespace preparation is restricted to disposable GitHub-hosted CI jobs.\n' >&2
  exit 1
}
(( EUID != 0 )) || { printf 'Namespace tests must run as an ordinary unprivileged user.\n' >&2; exit 1; }
[[ "$RUNNER_TEMP" =~ ^/[a-zA-Z0-9_./-]+$ && "$GITHUB_SHA" =~ ^[0-9a-f]{40}$ ]] || exit 1
[[ "$GITHUB_RUN_ID" =~ ^[0-9]+$ && "$GITHUB_RUN_ATTEMPT" =~ ^[0-9]+$ ]] || exit 1

evidence="$RUNNER_TEMP/ordinary-private-namespace-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}.json"
stage=tools
exception=not_required
write_failure() {
  local status=$?
  if (( status != 0 )); then
    python3 - "$evidence" "$GITHUB_SHA" "$stage" <<'PY'
import json
import os
import sys

path, revision, stage = sys.argv[1:]
with open(path + ".tmp", "w", encoding="utf-8") as output:
    json.dump({"version": 1, "case": "ordinary_private_namespace", "source_commit": revision,
               "status": "failed", "failure_stage": stage}, output)
    output.write("\n")
os.replace(path + ".tmp", path)
PY
  fi
}
trap write_failure EXIT

unshare_source=$(readlink -f "$(type -P unshare)")
[[ "$unshare_source" == /usr/bin/unshare && -f "$unshare_source" ]] || exit 1
[[ "$(stat -c %u "$unshare_source")" == 0 ]] || exit 1
source_mode=$(stat -c %a "$unshare_source")
(( (8#$source_mode & 0022) == 0 )) || exit 1
command -v python3 >/dev/null
command -v timeout >/dev/null
root=$(mktemp -d "$RUNNER_TEMP/symmetry-ci-private-userns.XXXXXX")
root=$(readlink -f "$root")
[[ "$root" =~ ^/[a-zA-Z0-9_./-]+$ ]] || exit 1
mkdir -m 0700 "$root/bin"
stage=copy_helper
# A fresh executable path avoids replacing a system-wide AppArmor profile.
install -m 0755 -- "$unshare_source" "$root/bin/unshare"

stage=profile
restriction=0
if [[ -r /proc/sys/kernel/apparmor_restrict_unprivileged_userns ]]; then
  read -r restriction < /proc/sys/kernel/apparmor_restrict_unprivileged_userns
fi
if [[ "$restriction" == 1 ]]; then
  parser=$(command -v apparmor_parser)
  profile="$root/private-userns.profile"
  cat > "$profile" <<EOF
abi <abi/4.0>,
include <tunables/global>
profile symmetry-ci-private-userns-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT} "$root/bin/unshare" flags=(unconfined) {
  userns,
}
EOF
  # This is a per-program userns exception, not a sandbox or native qualification.
  # Keep global policy intact and never run the tests or product daemon with sudo.
  sudo -n "$parser" --replace "$profile"
  exception=per_program
fi

stage=private_namespace
parent_namespaces=$(python3 - <<'PY'
import os
print(os.stat("/proc/self/ns/pid").st_ino, os.stat("/proc/self/ns/net").st_ino)
PY
)
read -r parent_pid_namespace parent_network_namespace <<< "$parent_namespaces"
timeout --signal=KILL 15s "$root/bin/unshare" \
  --user --map-root-user --pid --fork --mount-proc \
  python3 - "$parent_pid_namespace" "$parent_network_namespace" "$evidence" "$GITHUB_SHA" "$exception" <<'PY'
import json
import os
import sys

parent_pid, parent_network, path, revision, exception = sys.argv[1:]
assert os.getpid() == 1, "readiness child is not private PID 1"
assert os.getuid() == 0 and os.getgid() == 0, "caller mapping to namespace root failed"
assert os.stat("/proc/self/ns/pid").st_ino != int(parent_pid), "PID namespace did not change"
assert os.stat("/proc/self/ns/net").st_ino == int(parent_network), "network namespace changed"
with open(path + ".tmp", "w", encoding="utf-8") as output:
    json.dump({"version": 1, "case": "ordinary_private_namespace", "source_commit": revision,
               "status": "passed", "ordinary_parent_uid": True, "private_pid_namespace": True,
               "private_namespace_root_mapping": True, "network_namespace_unchanged": True,
               "apparmor_exception": exception}, output)
    output.write("\n")
os.replace(path + ".tmp", path)
PY

stage=path_export
printf '%s\n' "$root/bin" >> "$GITHUB_PATH"
printf 'Private user/PID namespace readiness passed as an ordinary UID; native qualification is not implied.\n'
