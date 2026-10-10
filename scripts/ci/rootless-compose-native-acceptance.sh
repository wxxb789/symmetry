#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

fail() {
  printf 'Rootless Compose native acceptance failed: %s\n' "$1" >&2
  exit 1
}

: "${GITHUB_WORKSPACE:?GitHub workspace is required}"
: "${RUNNER_TEMP:?runner temporary directory is required}"
: "${GITHUB_SHA:?source commit is required}"
: "${GITHUB_REPOSITORY:?source repository is required}"
: "${GITHUB_REPOSITORY_OWNER:?source repository owner is required}"
: "${SYMMETRY_LINUX_V2_ROOTLESS_EVIDENCE_DIR:?private rootless evidence directory is required}"

[[ $# == 2 ]] || fail 'expected the running local Compose project name and private temporary directory'
local_project=$1
temporary_dir=$2
repo=$GITHUB_WORKSPACE
source_subject_json=$(python3 "$repo/scripts/ci/linux-v2-source-subject.py" "$repo" "$GITHUB_SHA")
control_api_url=http://127.0.0.1:4000
rootless_app_state_dir=/var/lib/symmetry
pi_inspection_state_dir=/var/lib/sym-pi
opencode_inspection_state_dir=/var/lib/sym-oc
portal_operator_token=development-operator-token
enrollment_token=development-enrollment-token
native_root="$temporary_dir/rootless-native"
image_context="$native_root/daemon-image"
owner_fixture="$native_root/owner-fixture"
native_override="$native_root/compose.native.yaml"
event_file="$native_root/rootless-compose-e2e.json"
container_image="symmetry-rootless-native-probe:${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}"
local_compose=(docker compose --project-name "$local_project" --file "$repo/compose.yaml")
evidence_directory=$SYMMETRY_LINUX_V2_ROOTLESS_EVIDENCE_DIR
[[ "$evidence_directory" == /* && "$(realpath -m "$evidence_directory")" == "$evidence_directory" ]] ||
  fail 'rootless evidence directory must be canonical and absolute'
[[ "$evidence_directory" != "$temporary_dir" && "$evidence_directory" != "$temporary_dir"/* ]] ||
  fail 'rootless evidence directory must survive temporary fixture cleanup'
mkdir -m 0700 -p "$evidence_directory"
[[ ! -L "$evidence_directory" && "$(stat -c '%u:%g:%a' "$evidence_directory")" == "$(id -u):$(id -g):700" ]] ||
  fail 'rootless evidence directory must be private to the runner'
shopt -s nullglob
existing_evidence=("$evidence_directory"/*)
shopt -u nullglob
(( ${#existing_evidence[@]} == 0 )) || fail 'rootless evidence directory must be empty before the fixture runs'
evidence_source="$native_root/witness-output"

python3 - "$rootless_app_state_dir" "$pi_inspection_state_dir" \
  "$opencode_inspection_state_dir" <<'PY'
import hashlib
import os
import sys

run_key = "00000000-0000-4000-8000-000000000000" + chr(0) + "1"
digest = hashlib.sha256(run_key.encode("ascii")).hexdigest()
for state_dir in sys.argv[1:]:
    if not os.path.isabs(state_dir) or os.path.normpath(state_dir) != state_dir:
        raise SystemExit("rootless StateDir must be canonical and absolute")
    endpoint = os.path.join(state_dir, f"owner-{digest}.sock")
    endpoint_bytes = len(os.fsencode(endpoint))
    if endpoint_bytes >= 100:
        raise SystemExit(f"rootless recovery endpoint is {endpoint_bytes} bytes, exceeds the <100-byte App limit")
PY

mkdir -m 0700 -p "$native_root" "$image_context"
mkdir -m 0700 "$evidence_source"
sudo chown 65532:65532 "$evidence_source"

cleanup_native_services() {
  status=$?
  cleanup_failed=0
  trap - EXIT INT TERM
  if [[ -f "$native_override" ]]; then
    "${local_compose[@]}" --file "$native_override" stop --timeout 20 \
      rootless-native-pi rootless-native-opencode transition-proxy rootless-portal-edge >/dev/null 2>&1 || true
    "${local_compose[@]}" --file "$native_override" rm --force \
      rootless-native-pi rootless-native-opencode transition-proxy rootless-portal-edge >/dev/null 2>&1 || true
  fi
  docker image rm "$container_image" >/dev/null 2>&1 || true
  if [[ -d "$evidence_source" && -d "$evidence_directory" ]]; then
    if ! sudo chown -R "$(id -u):$(id -g)" "$evidence_source" ||
       ! sudo find "$evidence_source" -type d -exec chmod 0700 {} + ||
       ! sudo find "$evidence_source" -type f -exec chmod 0600 {} +; then
      cleanup_failed=1
      printf 'Rootless evidence staging ownership could not be returned to the runner: %s\n' "$evidence_source" >&2
    else
      evidence_status=failed
      if (( status == 0 && cleanup_failed == 0 )); then
        if ! python3 "$repo/scripts/ci/linux-v2-source-subject.py" "$repo" "$GITHUB_SHA" >/dev/null; then
          cleanup_failed=1
        fi
      fi
      if (( status == 0 && cleanup_failed == 0 )); then evidence_status=passed; fi
      if ! python3 "$repo/scripts/ci/export-linux-v2-redacted-evidence.py" \
        compose "$evidence_directory" "$GITHUB_SHA" "$evidence_status" \
        --witness-root "$evidence_source"; then
        cleanup_failed=1
        rm -f -- "$evidence_directory/manifest.json"
        printf 'Rootless Compose evidence did not pass the exact allowlist/redaction gate.\n' >&2
      fi
    fi
  fi
  if (( cleanup_failed )) && (( status == 0 )); then
    status=1
  fi
  exit "$status"
}
trap cleanup_native_services EXIT INT TERM

source "$repo/scripts/ci/linux-v2-native-tools.sh"
linux_v2_fetch_native_tools "$native_root"

source_tree_digest=$(python3 -c 'import json, sys; print(json.load(sys.stdin)["tree_digest"])' <<< "$source_subject_json")

native_daemon="$native_root/symmetry-daemon"
(
  cd "$repo/daemon"
  env -u GH_TOKEN -u GITHUB_TOKEN -u SYMMETRY_OPENAI_API_KEY \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
      -ldflags='-s -w -buildid=' -o "$native_daemon" ./cmd/symmetry-daemon
)
chmod 0555 "$native_daemon"
cp -- "$native_daemon" "$image_context/symmetry-daemon"
chmod 0555 "$image_context/symmetry-daemon"
docker build --pull --tag "$container_image" \
  --file "$repo/scripts/ci/rootless-compose-native-daemon.Dockerfile" "$image_context"

pi_state="$native_root/pi-state"
opencode_state="$native_root/opencode-state"
pi_workspace="$native_root/pi-workspaces"
opencode_workspace="$native_root/opencode-workspaces"
mkdir -m 0700 "$pi_state" "$opencode_state" "$pi_workspace" "$opencode_workspace"
sudo chown -R 65532:65532 "$pi_state" "$opencode_state" "$pi_workspace" "$opencode_workspace"
sudo chmod 0700 "$pi_state" "$opencode_state" "$pi_workspace" "$opencode_workspace"

python3 - "$native_root/pi-daemon.json" "$native_root/opencode-daemon.json" \
  "$GITHUB_REPOSITORY" "$rootless_app_state_dir" <<'PY'
import json
import pathlib
import sys

pi_path, opencode_path, repository, rootless_app_state_dir = sys.argv[1:]

def config(kind, profile, command, version, model, state_dir):
    agent = {
        "command": command,
        "args": ["--provider", "openai", "--model", model] if kind == "pi" else [],
        "native_model": model,
        "native_model_provider": "openai",
        "input_mode": "goal",
        "provider_access": False,
        "interactive": True,
        "supervisory_control": False,
        "event_format": "jsonl",
        "env_allowlist": [],
    }
    return {
        "control_plane_url": "http://transition-proxy:4000",
        "allow_insecure_http": True,
        "state_dir": state_dir,
        "cleanup_timeout_ms": 120000,
        "machine_name": f"rootless-compose-native-{kind}",
        "agent_profiles": {profile: agent},
        "workspaces": {
            "native": {
                "policy": "existing_checkout",
                "path": "/workspaces",
                "repository": "",
                "root": "",
                "ref": "",
                "cleanup": "never",
            }
        },
        "runtime": {
            "runtime_key": f"rootless-compose-native-{kind}",
            "name": f"Rootless Compose native {kind} refusal witness",
            "capacity": 1,
            "agent_profile": profile,
            "workspace": "native",
            "harness_kind": kind,
            "harness_version": version,
            "adapter_version": "symmetry-daemon:rootless-compose-witness",
            "adapter_protocol_version": 1,
        },
    }

pathlib.Path(pi_path).write_text(
    json.dumps(config("pi", "pi-native", "/usr/local/bin/pi", "0.87.1", "gpt-4.1-mini", rootless_app_state_dir), separators=(",", ":")),
    encoding="utf-8",
)
pathlib.Path(opencode_path).write_text(
    json.dumps(config("opencode", "opencode-native", "/usr/local/bin/opencode", "1.18.32", "openai/gpt-4.1-mini", rootless_app_state_dir), separators=(",", ":")),
    encoding="utf-8",
)
PY
chmod 0644 "$native_root/pi-daemon.json" "$native_root/opencode-daemon.json"

tls_root="$native_root/portal-tls"
mkdir -m 0755 "$tls_root"
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -subj '/CN=localhost' -addext 'subjectAltName=DNS:localhost,DNS:rootless-portal-edge' \
  -keyout "$tls_root/tls.key" -out "$tls_root/tls.crt" >/dev/null 2>&1
chmod 0600 "$tls_root/tls.key"
chmod 0644 "$tls_root/tls.crt"
# Reuse production forwarding and TLS policy, admitting only this fixture's two DNS names.
python3 - "$repo/docker/edge-nginx.conf" "$tls_root/edge-nginx.conf" <<'PY'
import pathlib
import sys

source = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
source = source.replace("server_name ${PHX_HOST};", "server_name localhost rootless-portal-edge;")
pathlib.Path(sys.argv[2]).write_text(source, encoding="utf-8")
PY
chmod 0644 "$tls_root/edge-nginx.conf"

cat > "$native_override" <<EOF
services:
  rootless-portal-edge:
    image: nginx:1.27.5-alpine
    depends_on:
      control:
        condition: service_started
    environment:
      PHX_HOST: localhost
    ports:
      - "127.0.0.1::443"
    volumes:
      - type: bind
        source: $tls_root/tls.crt
        target: /run/symmetry/tls.crt
        read_only: true
      - type: bind
        source: $tls_root/tls.key
        target: /run/symmetry/tls.key
        read_only: true
      - type: bind
        source: $tls_root/edge-nginx.conf
        target: /etc/nginx/templates/default.conf.template
        read_only: true
    restart: "no"
  transition-proxy:
    image: python:3.12-slim-bookworm
    user: "65532:65532"
    entrypoint: ["python3", "/opt/symmetry-ci/rootless-compose-transition-proxy.py"]
    command: ["--listen", "0.0.0.0:4000", "--upstream", "http://control:4000"]
    depends_on:
      control:
        condition: service_started
    volumes:
      - type: bind
        source: $repo/scripts/ci/rootless-compose-transition-proxy.py
        target: /opt/symmetry-ci/rootless-compose-transition-proxy.py
        read_only: true
    healthcheck:
      test: ["CMD", "python3", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:4000/healthz', timeout=2).read()"]
      interval: 1s
      timeout: 3s
      retries: 30
    restart: "no"
  rootless-native-pi:
    image: $container_image
    depends_on:
      transition-proxy:
        condition: service_healthy
    environment:
      SYMMETRY_ENROLLMENT_TOKEN: $enrollment_token
    volumes:
      - type: bind
        source: $pi_state
        target: /var/lib/symmetry
      - type: bind
        source: $pi_workspace
        target: /workspaces
      - type: bind
        source: $native_root/pi-daemon.json
        target: /etc/symmetry/daemon.json
        read_only: true
      - type: bind
        source: $pi_executable
        target: /usr/local/bin/pi
        read_only: true
    restart: "no"
  rootless-native-opencode:
    image: $container_image
    depends_on:
      transition-proxy:
        condition: service_healthy
    environment:
      SYMMETRY_ENROLLMENT_TOKEN: $enrollment_token
    volumes:
      - type: bind
        source: $opencode_state
        target: /var/lib/symmetry
      - type: bind
        source: $opencode_workspace
        target: /workspaces
      - type: bind
        source: $native_root/opencode-daemon.json
        target: /etc/symmetry/daemon.json
        read_only: true
      - type: bind
        source: $opencode_executable
        target: /usr/local/bin/opencode
        read_only: true
    restart: "no"
EOF

"${local_compose[@]}" --file "$native_override" up --detach rootless-portal-edge
portal_address=$("${local_compose[@]}" --file "$native_override" port rootless-portal-edge 443)
portal_port=${portal_address##*:}
[[ "$portal_port" =~ ^[0-9]+$ ]] || fail 'TLS Portal fixture did not publish a decimal loopback port'
control_url="https://localhost:$portal_port"
portal_ready=false
for _ in {1..30}; do
  if curl --fail --silent --show-error --cacert "$tls_root/tls.crt" "$control_url/healthz" >/dev/null; then
    portal_ready=true
    break
  fi
  sleep 1
done
[[ "$portal_ready" == true ]] || fail 'trusted TLS Portal fixture did not become ready'

owner_fixture="$native_root/owner-fixture"
sudo install -d -o root -g root -m 0755 \
  "$owner_fixture" "$owner_fixture/unprovisioned" "$owner_fixture/production" \
  "$owner_fixture/production/state" "$owner_fixture/production/clones" \
  "$owner_fixture/production/seeds" "$owner_fixture/barriers" \
  "$owner_fixture/barriers/unprovisioned" "$owner_fixture/native-hook-markers" \
  "$owner_fixture/native-hook-markers/opencode"
sudo install -d -o 65532 -g 65532 -m 0700 \
  "$owner_fixture/production/state/unprovisioned" \
  "$owner_fixture/production/state/unprovisioned/runs" \
  "$owner_fixture/barriers/unprovisioned/request"
sudo install -o 65532 -g 65532 -m 0600 /dev/null \
  "$owner_fixture/production/state/unprovisioned/.symmetry-daemon.lock"
sudo chown -R 65532:65532 "$owner_fixture/native-hook-markers"
sudo chmod 0700 "$owner_fixture/native-hook-markers" "$owner_fixture/native-hook-markers/opencode"
sudo install -o root -g root -m 0555 "$native_daemon" "$owner_fixture/unprovisioned/symmetry-daemon"
[[ "$(sudo stat -c '%u:%g:%a:%h' "$owner_fixture/unprovisioned/symmetry-daemon")" == '0:0:555:1' ]] ||
  fail 'hidden refusal daemon must be root:root 0555 and singly-linked'

control_machine_token="$native_root/owner-machine-token"
owner_run_metadata="$native_root/owner-run.json"
owner_request_source="$native_root/owner-request.json"
python3 "$repo/scripts/ci/prepare-rootless-compose-owner-claim.py" \
  "$control_url" "$portal_operator_token" "$enrollment_token" \
  "rootless-compose-unprovisioned-opencode-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}" \
  "$owner_run_metadata" "$control_machine_token" \
  "$GITHUB_REPOSITORY" "$GITHUB_SHA" "$source_tree_digest" "$tls_root/tls.crt"

python3 - "$owner_run_metadata" "$native_root/unprovisioned-config.json" \
  "$GITHUB_REPOSITORY" <<'PY'
import json
import pathlib
import sys

run_path, config_path, repository = sys.argv[1:]
run = json.loads(pathlib.Path(run_path).read_text(encoding="utf-8"))
model = "openai/gpt-4.1-mini"
config = {
    "control_plane_url": "http://control:4000",
    "allow_insecure_http": True,
    "state_dir": "/opt/symmetry-owner-test/production/state/unprovisioned",
    "linux_owner_cgroup_mount": "/sys/fs/cgroup",
    "linux_owner_delegated_path": "system.slice/symmetry-owner.service/manager",
    "linux_owner_socket": "/run/symmetry-owner/owner.sock",
    "machine_name": "rootless-compose-unprovisioned-owner-refusal",
    "cleanup_timeout_ms": 120000,
    "agent_profiles": {
        "opencode-native": {
            "command": "/usr/local/bin/opencode",
            "args": [],
            "native_model": model,
            "native_model_provider": "openai",
            "input_mode": "goal",
            "provider_access": False,
            "interactive": True,
            "supervisory_control": False,
            "event_format": "jsonl",
            "env_allowlist": ["SYMMETRY_LINUX_V2_NATIVE_HOOK_MARKER_ROOT"],
        }
    },
    "workspaces": {
        "native": {
            "policy": "git_clone_v2",
            "path": "",
            "repository": "https://github.com/" + repository + ".git",
            "root": "/opt/symmetry-owner-test/production/clones",
            "seed_root": "/opt/symmetry-owner-test/production/seeds",
            "ref": "",
            "cleanup": "never",
        }
    },
    "runtime": {
        "runtime_key": "rootless-compose-unprovisioned-opencode-claim",
        "name": "Rootless Compose hidden OpenCode owner refusal",
        "capacity": 1,
        "agent_profile": "opencode-native",
        "workspace": "native",
        "repository_resource_id": run["repository_resource_id"],
        "harness_kind": "opencode",
        "harness_version": "1.18.32",
        "adapter_version": "symmetry-daemon:rootless-compose-witness",
        "adapter_protocol_version": 1,
    },
}
pathlib.Path(config_path).write_text(json.dumps(config, separators=(",", ":")), encoding="utf-8")
PY
chmod 0600 "$native_root/unprovisioned-config.json"
python3 "$repo/scripts/ci/write-linux-v2-owner-request.py" \
  stage_replay "$owner_run_metadata" "$native_root/unprovisioned-config.json" \
  "$owner_fixture/barriers/unprovisioned/request" "$control_api_url" \
  "$control_machine_token" "$owner_request_source" native_release
sudo install -o root -g 65532 -m 0640 "$native_root/unprovisioned-config.json" \
  "$owner_fixture/unprovisioned/config.json"
sudo install -o root -g 65532 -m 0440 "$owner_request_source" \
  "$owner_fixture/unprovisioned/request.json"
[[ "$(sudo stat -c '%u:%g:%a:%h' "$owner_fixture/unprovisioned/config.json")" == '0:65532:640:1' ]] ||
  fail 'hidden owner refusal config must be root-owned, test-group readable, and singly-linked'
[[ "$(sudo stat -c '%u:%g:%a:%h' "$owner_fixture/unprovisioned/request.json")" == '0:65532:440:1' ]] ||
  fail 'hidden owner refusal request must be root-owned, test-group readable, and singly-linked'
rm -f -- "$control_machine_token" "$owner_run_metadata" "$owner_request_source" "$native_root/unprovisioned-config.json"

native_override="$native_root/compose.native.yaml"
"${local_compose[@]}" --file "$native_override" up --build --detach transition-proxy rootless-native-pi rootless-native-opencode
network="$local_project"_default
docker network inspect "$network" >/dev/null

event_file="$native_root/rootless-compose-e2e.json"
container_test_status=0
docker run --rm --network "$network" --user 65532:65532 \
  --workdir /workspace/daemon \
  --mount "type=bind,source=$repo,target=/workspace,readonly" \
  --mount "type=bind,source=$pi_state,target=/var/lib/sym-pi,readonly" \
  --mount "type=bind,source=$opencode_state,target=/var/lib/sym-oc,readonly" \
  --mount "type=bind,source=$owner_fixture,target=/opt/symmetry-owner-test" \
  --mount "type=bind,source=$evidence_source,target=/tmp/rootless-compose-output" \
  --mount "type=bind,source=$pi_executable,target=/usr/local/bin/pi,readonly" \
  --mount "type=bind,source=$opencode_executable,target=/usr/local/bin/opencode,readonly" \
  --mount "type=bind,source=$tls_root/tls.crt,target=/opt/symmetry-ci/portal-ca.pem,readonly" \
  --env HOME=/tmp/rootless-compose-home \
  --env GOCACHE=/tmp/rootless-compose-go-cache \
  --env GOMODCACHE=/tmp/rootless-compose-go-mod-cache \
  --env GOTMPDIR=/tmp/rootless-compose-go-tmp \
  --env CGO_ENABLED=0 \
  --env GOFLAGS=-mod=readonly \
  --env SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_WITNESS=1 \
  --env SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_CONTROL_URL=https://rootless-portal-edge \
  --env SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_CA_FILE=/opt/symmetry-ci/portal-ca.pem \
  --env SYMMETRY_PRODUCTION_LINUX_V2_COMPOSE_TRANSITION_RELEASE_URL=http://transition-proxy:4000/__test/release \
  --env SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_OPERATOR_TOKEN="$portal_operator_token" \
  --env SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_PI_STATE_DIR=/var/lib/sym-pi \
  --env SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_OPENCODE_STATE_DIR=/var/lib/sym-oc \
  --env SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_PI_EXECUTABLE=/usr/local/bin/pi \
  --env SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_OPENCODE_EXECUTABLE=/usr/local/bin/opencode \
  --env SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_PI_RUNTIME_KEY=rootless-compose-native-pi \
  --env SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_OPENCODE_RUNTIME_KEY=rootless-compose-native-opencode \
  --env SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_WITNESS_OUTPUT_DIR=/tmp/rootless-compose-output \
  --env SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNPROVISIONED_WITNESS=1 \
  --env SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNPROVISIONED_ROOT=/opt/symmetry-owner-test/unprovisioned \
  --env SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNPROVISIONED_DAEMON=/opt/symmetry-owner-test/unprovisioned/symmetry-daemon \
  --env SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_HOOK_MARKER_ROOT=/opt/symmetry-owner-test/native-hook-markers \
  --env SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS_OUTPUT_DIR=/tmp/rootless-compose-output \
  --entrypoint /bin/bash golang:1.27.0-bookworm \
  -euc 'mkdir -p "$HOME" "$GOCACHE" "$GOMODCACHE" "$GOTMPDIR" "$SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_WITNESS_OUTPUT_DIR"; cd /workspace/daemon; go mod download; go test -count=1 -timeout 900s -json ./e2e -run "^(TestProductionLinuxV2RootlessComposeNativeWorkItemRefusal|TestProductionLinuxV2UnprovisionedHostRefusal)$"' \
  >"$event_file" || container_test_status=$?
if ! python3 "$repo/scripts/ci/assert-go-test-json.py" "$event_file" \
  TestProductionLinuxV2RootlessComposeNativeWorkItemRefusal \
  TestProductionLinuxV2RootlessComposeNativeWorkItemRefusal/pi \
  TestProductionLinuxV2RootlessComposeNativeWorkItemRefusal/opencode \
  TestProductionLinuxV2UnprovisionedHostRefusal; then
  fail "rootless Compose native refusal E2E did not produce exact run/pass/skip=0 events (go test exit=$container_test_status)"
fi
if (( container_test_status != 0 )); then
  fail "rootless Compose native refusal E2E exited with status $container_test_status"
fi
