#!/usr/bin/env bash
set -Eeuo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/.." && pwd -P)
cd "$repo_root"

# Deliberately enumerate contract surfaces. In particular, docs/plans is not a
# production source of truth, and the Phoenix rejection test intentionally
# contains the retired paths so it is not an audit target.
audit_targets=(
  README.md
  control/README.md
  docs/goal-01-runbook.md
  docs/protocol-v1.md
  compose.production.yaml
  docker/edge-nginx.conf
  docker/daemon-config.json
  daemon/cmd/symmetry-daemon/testdata
  control/lib/symmetry_control_web/router.ex
  daemon/internal/control/client.go
  daemon/e2e/core_test.go
  .github/workflows/ci.yml
)

for target in "${audit_targets[@]}"; do
  if [[ ! -e "$target" ]]; then
    printf 'Audit target is missing: %s\n' "$target" >&2
    exit 2
  fi
done

# U2 replaced these action endpoints with resource-oriented routes. The pattern
# intentionally permits surrounding source syntax so it catches route literals,
# Go path fragments, curl commands, and JSON fixtures.
legacy_route_pattern='(/api/)?v1/(daemon/(enroll|sessions)([^[:alpha:]]|$)|runtimes/[^/[:space:]]+/(heartbeat|work|reconcile)([^[:alpha:]]|$)|runs/[^/[:space:]]+/(claim|heartbeat|state)([^[:alpha:]]|$)|commands/[^/[:space:]]+/ack([^[:alpha:]]|$)|tasks/[^/[:space:]]+/(cancel|input)([^[:alpha:]]|$))|/(daemon/(enroll|sessions)([^[:alpha:]]|$)|runtimes/[^/[:space:]]+/(heartbeat|work|reconcile)([^[:alpha:]]|$)|runs/[^/[:space:]]+/(claim|heartbeat|state)([^[:alpha:]]|$)|commands/[^/[:space:]]+/ack([^[:alpha:]]|$)|tasks/[^/[:space:]]+/(cancel|input)([^[:alpha:]]|$))'

if audit_output=$(rg --line-number --color=never --glob '*.go' --glob '*.json' --glob '*.md' \
  --glob '*.ex' --glob '*.exs' --glob '*.yaml' --glob '*.yml' --glob '*.conf' \
  --glob '!docs/plans/**' --regexp "$legacy_route_pattern" -- "${audit_targets[@]}" 2>&1); then
  printf '%s\n' "$audit_output" >&2
  printf '%s\n' 'U2 legacy action endpoints are forbidden in production contract surfaces.' >&2
  exit 1
else
  status=$?
  if ((status != 1)) || [[ -n "$audit_output" ]]; then
    printf '%s\n' "$audit_output" >&2
    if ((status == 1)); then
      status=2
    fi
    exit "$status"
  fi
fi

printf '%s\n' 'No U2 legacy action endpoints found in production contract surfaces.'
