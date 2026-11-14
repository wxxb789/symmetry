#!/usr/bin/env bash
# Shared fixed-version acquisition for installed-owner and rootless fixtures.
# Caller owns a private destination and enables errexit, nounset and pipefail.

linux_v2_fetch_pi() {
  local destination=$1
  local pi_url='https://github.com/earendil-works/pi/releases/download/v1.5.2/pi-linux-x64.tar.gz'
  local pi_archive="$destination/pi-linux-x64.tar.gz"
  local pi_archive_sha='811c6f2c9e7aaee252b5404252e556fb713aa209c1f7f924226aa77d4ef70cb5'
  local pi_executable_sha='6a2ac1c5b0d08b91ce6095591c1960b469d0665c7fc5f32812960a4a17ca0c57'
  pi_unpack="$destination/pi-release"
  pi_executable="$pi_unpack/pi/pi"
  curl --fail --silent --show-error --location --retry 5 --retry-delay 2 --retry-max-time 120 --retry-all-errors \
    "$pi_url" --output "$pi_archive"
  printf '%s  %s\n' "$pi_archive_sha" "$pi_archive" | sha256sum --check --strict >/dev/null
  mkdir "$pi_unpack"
  tar --extract --file "$pi_archive" --directory "$pi_unpack"
  test -x "$pi_executable"
  test "$(sha256sum "$pi_executable" | cut -d' ' -f1)" = "$pi_executable_sha"
  test "$(timeout 30s "$pi_executable" --version)" = '1.5.2'
}

linux_v2_fetch_native_tools() {
  local destination=$1
  linux_v2_fetch_pi "$destination"
  local opencode_url='https://github.com/anomalyco/opencode/releases/download/v1.18.32/opencode-linux-x64-baseline.tar.gz'
  local opencode_archive="$destination/opencode-linux-x64-baseline.tar.gz"
  local opencode_archive_sha='763af386ef88a8cab18df00fcf055690e5a55e31a7088beabe02307142a6adce'
  opencode_unpack="$destination/opencode-release"
  opencode_executable="$opencode_unpack/opencode"
  curl --fail --silent --show-error --location --retry 5 --retry-delay 2 --retry-max-time 120 --retry-all-errors \
    "$opencode_url" --output "$opencode_archive"
  printf '%s  %s\n' "$opencode_archive_sha" "$opencode_archive" | sha256sum --check --strict >/dev/null
  mkdir "$opencode_unpack"
  tar --extract --file "$opencode_archive" --directory "$opencode_unpack"
  test -x "$opencode_executable"
  test "$(timeout 30s "$opencode_executable" --version)" = '1.18.32'
}
