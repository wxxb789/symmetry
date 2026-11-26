#!/usr/bin/env bash
# Shared fixed-version acquisition for installed-owner and rootless fixtures.
# Caller owns a private destination and enables errexit, nounset and pipefail.

linux_v2_fetch_archive() {
  local url=$1 archive=$2 archive_sha=$3 unpack=$4
  curl --fail --silent --show-error --location --retry 5 --retry-delay 2 --retry-max-time 120 --retry-all-errors \
    "$url" --output "$archive"
  printf '%s  %s\n' "$archive_sha" "$archive" | sha256sum --check --strict >/dev/null
  mkdir "$unpack"
  tar --extract --file "$archive" --directory "$unpack"
}

linux_v2_pi_tree_digest() {
  local tree=$1 runner=${2:-}
  # Binds entry type, mode, ownership, path, link target and content, so an added link,
  # a chmod or a rewritten file cannot match the pinned normalized release tree.
  $runner bash -c '
    set -o pipefail
    cd "$1" || exit 1
    {
      find . -mindepth 1 -printf "%y %m %U:%G %P\t%l\0"
      find . -mindepth 1 -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | tr "\n" "\0"
    } | LC_ALL=C sort -z | sha256sum | cut -d" " -f1
  ' _ "$tree"
}

linux_v2_pin_pi_tree() {
  local tree=$1 runner=${2:-}
  # The pin binds the normalized release tree (root:root; dirs 0755; files 0644 / u+x 0755), and a
  # raw extraction's modes and ownership follow the caller's umask and uid, so normalize in place.
  $runner chown -R root:root "$tree"
  $runner find "$tree" -type d -exec chmod 0755 {} +
  $runner find "$tree" -type f ! -perm -u+x -exec chmod 0644 {} +
  $runner find "$tree" -type f -perm -u+x -exec chmod 0755 {} +
  test "$(linux_v2_pi_tree_digest "$tree" "$runner")" = "${pi_tree_sha:-}"
}

linux_v2_fetch_pi() {
  local destination=$1
  local pi_url='https://github.com/earendil-works/pi/releases/download/v1.5.2/pi-linux-x64.tar.gz'
  local pi_archive="$destination/pi-linux-x64.tar.gz"
  # Callers that publish pi evidence need the pins and the version line outside this function.
  pi_archive_sha='811c6f2c9e7aaee252b5404252e556fb713aa209c1f7f924226aa77d4ef70cb5'
  pi_executable_sha='6a2ac1c5b0d08b91ce6095591c1960b469d0665c7fc5f32812960a4a17ca0c57'
  export pi_tree_sha='f5fc36f79aa55b685295b4c8c251ed4da485e565181c22dcb16a1676d2396a68'
  pi_version_line='1.5.2'
  pi_unpack="$destination/pi-release"
  pi_executable="$pi_unpack/pi/pi"
  curl --fail --silent --show-error --location --retry 5 --retry-delay 2 --retry-max-time 120 --retry-all-errors \
    "$pi_url" --output "$pi_archive"
  printf '%s  %s\n' "$pi_archive_sha" "$pi_archive" | sha256sum --check --strict >/dev/null
  mkdir "$pi_unpack"
  tar --extract --file "$pi_archive" --directory "$pi_unpack"
  test -x "$pi_executable"
  # Every caller exposes or copies this tree, so the pin is checked on the canonicalized bytes.
  linux_v2_pin_pi_tree "$pi_unpack/pi" sudo
  test "$(sha256sum "$pi_executable" | cut -d' ' -f1)" = "$pi_executable_sha"
  test "$(timeout 30s "$pi_executable" --version)" = "$pi_version_line"
}

linux_v2_fetch_native_tools() {
  local destination=$1
  linux_v2_fetch_pi "$destination"
  local opencode_url='https://github.com/anomalyco/opencode/releases/download/v2.4.1/opencode-linux-x64.tar.gz'
  local opencode_archive="$destination/opencode-linux-x64.tar.gz"
  local opencode_archive_sha='9ddd18e3faf64eaeccf6a0fcbc96a5c7a14520ae5fd5b61e5be51d894f478b47'
  opencode_unpack="$destination/opencode-release"
  opencode_executable="$opencode_unpack/opencode"
  curl --fail --silent --show-error --location --retry 5 --retry-delay 2 --retry-max-time 120 --retry-all-errors \
    "$opencode_url" --output "$opencode_archive"
  printf '%s  %s\n' "$opencode_archive_sha" "$opencode_archive" | sha256sum --check --strict >/dev/null
  mkdir "$opencode_unpack"
  tar --extract --file "$opencode_archive" --directory "$opencode_unpack"
  test -x "$opencode_executable"
  test "$(sha256sum "$opencode_executable" | cut -d' ' -f1)" = 'ec93043f18f8a6b4d8e71ec9260e292ba6f91b66b142782135d40f0b58b0c152'
  test "$(timeout 30s "$opencode_executable" --version)" = 'opencode v2.4.1'
}

linux_v2_fetch_codex() {
  local destination=$1
  local codex_url='https://github.com/openai/codex/releases/download/rust-v0.168.0/codex-package-x86_64-unknown-linux-musl.tar.gz'
  local codex_archive="$destination/codex-package-x86_64-unknown-linux-musl.tar.gz"
  codex_archive_sha='3acbff91c0a8e30767d47c8b6941279a603ab4087280aa3e546537cef3622cb8'
  codex_executable_sha='a8cb58c528c76e7313d4a70294ec614757b0f4c7bf9683e11c88ecad8c3d421b'
  codex_version_line='codex-cli 0.168.0'
  codex_unpack="$destination/codex-release"
  codex_executable="$codex_unpack/bin/codex"
  local bwrap_url='https://github.com/openai/codex/releases/download/rust-v0.168.0/bwrap-x86_64-unknown-linux-musl.tar.gz'
  local bwrap_archive="$destination/bwrap-x86_64-unknown-linux-musl.tar.gz"
  bwrap_archive_sha='992cdf4ab9775a2afcb94ed9a139dd013c116b15b23ab7922e2ca7c82c9d4757'
  bwrap_executable_sha='77360cb751ccedc5971391444ac86a8a33c15b04d6b4a6fe45f5d25496e62c4c'
  bwrap_unpack="$destination/bwrap-release"
  bwrap_executable="$bwrap_unpack/bwrap-x86_64-unknown-linux-musl"
  local code_mode_host_url='https://github.com/openai/codex/releases/download/rust-v0.168.0/codex-code-mode-host-x86_64-unknown-linux-musl.tar.gz'
  local code_mode_host_archive="$destination/codex-code-mode-host-x86_64-unknown-linux-musl.tar.gz"
  code_mode_host_archive_sha='4e00a7d5eec14b50be2f21f11feb13dcbaa6dd500b95fc1f9ba4871ec44af18e'
  code_mode_host_executable_sha='e1a56831a087bfeb1a91d43b7265fd1c38b898396b09ab357dc751d641ad25bd'
  code_mode_host_unpack="$destination/codex-code-mode-host-release"
  code_mode_host_executable="$code_mode_host_unpack/codex-code-mode-host-x86_64-unknown-linux-musl"
  linux_v2_fetch_archive "$codex_url" "$codex_archive" "$codex_archive_sha" "$codex_unpack"
  test -x "$codex_executable"
  test "$(sha256sum "$codex_executable" | cut -d' ' -f1)" = "$codex_executable_sha"
  test "$(timeout 30s "$codex_executable" --version)" = "$codex_version_line"
  linux_v2_fetch_archive "$bwrap_url" "$bwrap_archive" "$bwrap_archive_sha" "$bwrap_unpack"
  test -x "$bwrap_executable"
  test "$(sha256sum "$bwrap_executable" | cut -d' ' -f1)" = "$bwrap_executable_sha"
  linux_v2_fetch_archive "$code_mode_host_url" "$code_mode_host_archive" "$code_mode_host_archive_sha" "$code_mode_host_unpack"
  test -x "$code_mode_host_executable"
  test "$(sha256sum "$code_mode_host_executable" | cut -d' ' -f1)" = "$code_mode_host_executable_sha"
}

linux_v2_fetch_claude() {
  local destination=$1
  local claude_url='https://github.com/anthropics/claude-code/releases/download/v2.1.323/claude-linux-x64.tar.gz'
  local claude_archive="$destination/claude-linux-x64.tar.gz"
  claude_archive_sha='522255678548859b9799b7b098c03a3aa286c9a9ede01957e35f4713ccc43631'
  claude_executable_sha='604d29a6869409106b6a1110bdadcc8b72e8654a5a865d23258f938a696e348e'
  claude_version_line='2.1.323 (Claude Code)'
  claude_unpack="$destination/claude-release"
  claude_executable="$claude_unpack/claude"
  linux_v2_fetch_archive "$claude_url" "$claude_archive" "$claude_archive_sha" "$claude_unpack"
  test -x "$claude_executable"
  test "$(sha256sum "$claude_executable" | cut -d' ' -f1)" = "$claude_executable_sha"
  test "$(timeout 30s "$claude_executable" --version)" = "$claude_version_line"
}
