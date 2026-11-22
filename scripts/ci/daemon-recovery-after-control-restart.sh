#!/usr/bin/env bash
set -Eeuo pipefail

control_log=/tmp/symmetry-control.log
control_pid_file=/tmp/symmetry-control.pid
control_bin=../control/_build/prod/rel/symmetry_control/bin/symmetry_control
marker=$SYMMETRY_E2E_RESTART_MARKER
restarter_pid=
rm -f "$marker"

cleanup() {
  status=$?
  if [[ -n "$restarter_pid" ]] && kill -0 "$restarter_pid" 2>/dev/null; then
    kill "$restarter_pid" 2>/dev/null || true
    wait "$restarter_pid" 2>/dev/null || true
  fi
  if (( status != 0 )); then
    echo 'Control release log:' >&2
    cat "$control_log" >&2 || true
  fi
  exit "$status"
}
trap cleanup EXIT

wait_for_marker() {
  for ((attempt = 1; attempt <= 60; attempt++)); do
    [[ -e "$marker" ]] && return 0
    sleep 1
  done
  echo "Timed out waiting for restart marker $marker" >&2
  return 1
}

wait_for_control_down() {
  for ((attempt = 1; attempt <= 60; attempt++)); do
    if ! curl --fail --silent --show-error http://127.0.0.1:4000/healthz >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo 'Timed out waiting for control health endpoint to stop' >&2
  return 1
}

wait_for_control_up() {
  for ((attempt = 1; attempt <= 60; attempt++)); do
    if curl --fail --silent --show-error http://127.0.0.1:4000/healthz >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo 'Timed out waiting for control health endpoint to recover' >&2
  return 1
}

restart_control() {
  wait_for_marker
  control_pid=$(<"$control_pid_file")
  kill -TERM "$control_pid"
  wait_for_control_down
  sleep 1
  "$control_bin" start >> "$control_log" 2>&1 &
  echo "$!" > "$control_pid_file"
  wait_for_control_up
}

restart_control &
restarter_pid=$!
set +e
python3 ../scripts/ci/go-test-json-gate.py \
  --events "$RUNNER_TEMP/daemon-control-restart.json" \
  --assert '*' TestDaemonSurvivesControlRestart \
  -- "$(command -v go)" test -count=1 -timeout 150s -json ./e2e \
    -run '^TestDaemonSurvivesControlRestart$'
gate_status=$?
set -e

set +e
wait "$restarter_pid"
restart_status=$?
set -e
restarter_pid=

if (( gate_status != 0 )); then
  exit "$gate_status"
fi
if (( restart_status != 0 )); then
  exit "$restart_status"
fi
