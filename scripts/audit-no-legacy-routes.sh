#!/usr/bin/env bash
set -Eeuo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/.." && pwd -P)
cd "$repo_root"

# The audit surface is derived from the tree instead of enumerated by hand. The
# previous revision listed thirteen targets, so every file added after it stayed
# unscanned: `.github/workflows/linux-native-v2-acceptance.yml`, the prepare-pi
# composite action, the root `compose.yaml`, unlisted controllers, unlisted
# `docs/evidence` pages and every file whose extension its globs did not name
# were silently out of scope. Deriving the list from `git ls-files` means a new
# file is audited the moment it is tracked.
#
# This is the only extension filter, and it is a deny list: formats whose bytes
# are containers or assets, which no reviewer reads a route literal out of.
# Every other tracked file is audited, including files with no extension at all,
# so an unlisted extension is audited rather than skipped.
unaudited_extensions=(
  png jpg jpeg gif ico webp bmp avif
  woff woff2 ttf otf eot
  zip gz tgz bz2 xz zst 7z tar
  pdf so dylib dll exe wasm jar class
)

# Tracked files that intentionally carry the retired literals as negative
# controls. These exclusions are re-checked below: an exclusion that no longer
# matches a literal is an unreachable rule that silently widens the audit
# surface, which is the defect this derivation replaced.
negative_controls=(
  # The Phoenix protocol rejection test asserts that every retired U2 action
  # endpoint is refused, so it has to name them.
  control/test/symmetry_control_web/controllers/protocol_controller_test.exs
  # Committed evidence for the reviewed copied-target fixture that added the
  # retired endpoint to a copy of docs/protocol-v1.md to prove this audit fails.
  docs/evidence/ci-process-trim-2026-11-15.md
  # The archived 0005 remediation plan quotes the forbidden action path shape in
  # the pitfall that tells authors not to introduce example route literals.
  docs/goals/archive/0005-REMEDIATION_PLAN.md
  # This audit's own suite has to carry the retired literal to prove that
  # copying it into a scanned surface makes the audit fail, so the fixture is
  # an intentional negative control by construction.
  scripts/ci/test-no-legacy-routes-audit.py
)

# U2 replaced these action endpoints with resource-oriented routes. The pattern
# intentionally permits surrounding source syntax so it catches route literals,
# Go path fragments, curl commands, and JSON fixtures.
legacy_route_pattern='(/api/)?v1/(daemon/(enroll|sessions)([^[:alpha:]]|$)|runtimes/[^/[:space:]]+/(heartbeat|work|reconcile)([^[:alpha:]]|$)|runs/[^/[:space:]]+/(claim|heartbeat|state)([^[:alpha:]]|$)|commands/[^/[:space:]]+/ack([^[:alpha:]]|$)|tasks/[^/[:space:]]+/(cancel|input)([^[:alpha:]]|$))|/(daemon/(enroll|sessions)([^[:alpha:]]|$)|runtimes/[^/[:space:]]+/(heartbeat|work|reconcile)([^[:alpha:]]|$)|runs/[^/[:space:]]+/(claim|heartbeat|state)([^[:alpha:]]|$)|commands/[^/[:space:]]+/ack([^[:alpha:]]|$)|tasks/[^/[:space:]]+/(cancel|input)([^[:alpha:]]|$))'

if command -v rg >/dev/null 2>&1; then
  # --no-ignore keeps the search honouring the file list this script derived
  # from git rather than ripgrep's own ignore rules.
  audit_command=(rg --line-number --color=never --no-heading --no-ignore --hidden
    --regexp "$legacy_route_pattern" --)
else
  audit_command=(grep -ERn -- "$legacy_route_pattern")
fi

# True when the matcher reported a match in every argument, false when it
# reported none. Any other status is a failure of the audit itself.
matches() {
  local status=0
  "${audit_command[@]}" "$@" >/dev/null 2>&1 || status=$?
  case "$status" in
    0) return 0 ;;
    1) return 1 ;;
    *)
      printf 'Legacy route matcher failed with status %d on: %s\n' "$status" "$*" >&2
      exit 2
      ;;
  esac
}

# The derived list is longer than a single argument vector allows on hosts with a
# small ARG_MAX, so it is scanned in batches. Each batch keeps its own status, so
# "this batch had no match" stays distinguishable from "the matcher failed".
audit_batch_size=200
audit_report=$(mktemp)
trap 'rm -f "$audit_report"' EXIT

scan_batch() {
  local status=0
  "${audit_command[@]}" "$@" >>"$audit_report" 2>&1 || status=$?
  return "$status"
}

# Returns 0 when any batch matched, 1 when none did, and the matcher's status
# when a batch failed.
scan_targets() {
  local -a batch=()
  local target status matched=1

  for target in "${audit_targets[@]}"; do
    batch+=("$target")
    if ((${#batch[@]} < audit_batch_size)); then
      continue
    fi

    status=0
    scan_batch "${batch[@]}" || status=$?
    batch=()

    if ((status == 0)); then
      matched=0
    elif ((status != 1)); then
      return "$status"
    fi
  done

  if ((${#batch[@]} > 0)); then
    status=0
    scan_batch "${batch[@]}" || status=$?

    if ((status == 0)); then
      matched=0
    elif ((status != 1)); then
      return "$status"
    fi
  fi

  return "$matched"
}

is_unaudited_extension() {
  local extension=${1##*.} candidate
  for candidate in "${unaudited_extensions[@]}"; do
    [[ $extension == "$candidate" ]] && return 0
  done
  return 1
}

is_negative_control() {
  local path=$1 candidate
  for candidate in "${negative_controls[@]}"; do
    [[ $path == "$candidate" ]] && return 0
  done
  return 1
}

for control in "${negative_controls[@]}"; do
  if ! git ls-files --error-unmatch -- "$control" >/dev/null 2>&1; then
    printf 'Legacy route exclusion is not a tracked file: %s\n' "$control" >&2
    exit 2
  fi
  if ! matches "$control"; then
    printf 'Legacy route exclusion carries no retired literal, so it excludes nothing: %s\n' \
      "$control" >&2
    exit 2
  fi
done

audit_targets=()
while IFS= read -r -d '' tracked; do
  is_unaudited_extension "$tracked" && continue
  is_negative_control "$tracked" && continue
  audit_targets+=("$tracked")
done < <(git ls-files -z)

if ((${#audit_targets[@]} == 0)); then
  printf 'Audit target list is empty: git ls-files named no auditable tracked file\n' >&2
  exit 2
fi

scan_status=0
scan_targets || scan_status=$?

if ((scan_status == 0)); then
  cat "$audit_report" >&2
  printf '%s\n' 'U2 legacy action endpoints are forbidden in production contract surfaces.' >&2
  exit 1
fi

if ((scan_status != 1)); then
  cat "$audit_report" >&2
  exit 2
fi

printf 'No U2 legacy action endpoints found in production contract surfaces: %d tracked files audited, %d intentional exclusions.\n' \
  "${#audit_targets[@]}" "${#negative_controls[@]}"
