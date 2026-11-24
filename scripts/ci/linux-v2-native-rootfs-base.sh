#!/usr/bin/env bash
# Builds the one shared disposable Ubuntu guest rootfs per job; each case set clones it.
set -Eeuo pipefail
umask 077

fail() {
  printf 'Linux v2 native rootfs base build failed: %s\n' "$1" >&2
  exit 1
}

base=${1:?the shared disposable guest rootfs base directory is required}
[[ "$base" == /* && "$(realpath -m "$base")" == "$base" ]] || fail 'the base directory must be canonical and absolute'

. /etc/os-release
codename=${VERSION_CODENAME:?Ubuntu release codename is unavailable}
# The public archive drops individual packages from the hosted runner network; mirror
# the first archive URI in the image's own apt mirrorlist, as debootstrap's source.
ubuntu_mirror=
apt_mirror_uri=$(awk '/^[[:space:]]*https?:\/\/[^ ]*\/ubuntu\/?[[:space:]]/ { sub(/\/$/, "", $1); print $1; exit }' /etc/apt/apt-mirrors.txt 2>/dev/null || true)
case "$apt_mirror_uri" in
http://*/ubuntu|https://*/ubuntu) ubuntu_mirror=$apt_mirror_uri ;;
*)
  sources_uri=$(awk '/^URIs:/ { print $2; exit }' /etc/apt/sources.list.d/*.sources 2>/dev/null || true)
  case "$sources_uri" in
  http://*/ubuntu|https://*/ubuntu|http://*/ubuntu/|https://*/ubuntu/) ubuntu_mirror=${sources_uri%/} ;;
  esac
  ;;
esac
[[ -n "$ubuntu_mirror" ]] || fail 'the image apt mirrorlist and apt sources name no Ubuntu archive for debootstrap'
printf 'Using the image apt archive mirror for the shared disposable rootfs base: %s\n' "$ubuntu_mirror"

# Only the one base build downloads the pinned package set; bound each attempt and retry so a
# stalled mirror cannot hang the job, with timeout owning the root debootstrap directly.
debootstrap_acquire_failure() {
  grep --extended-regexp --quiet \
    "Couldn.t download package|Failed to fetch|Temporary failure resolving|Cannot initiate the connection|Connection (timed out|reset|refused)|Read error|Hash Sum mismatch" \
    "$1"
}

sudo rm -rf -- "$base"
attempt=1
attempt_timeout_seconds=600
while true; do
  attempt_log="${base}.debootstrap-attempt-${attempt}.log"
  status=0
  sudo timeout --signal=TERM --kill-after=30s "$attempt_timeout_seconds" \
    debootstrap --variant=minbase \
      --include=acl,binutils,bash,ca-certificates,coreutils,curl,dbus,git,libacl1,libcap2-bin,libgcc-s1,libstdc++6,passwd,procps,python3,sudo,systemd,systemd-sysv,tar,util-linux \
      "$codename" "$base" "$ubuntu_mirror" 2>&1 | tee "$attempt_log" || status=${PIPESTATUS[0]}
  if (( status == 0 )); then
    break
  fi
  retryable=0
  if (( status == 124 || status == 137 )); then
    retryable=1
  elif debootstrap_acquire_failure "$attempt_log"; then
    retryable=1
  fi
  if (( attempt >= 5 || retryable == 0 )); then
    printf 'debootstrap exited %d without a retryable acquire failure; last 40 lines:\n' "$status" >&2
    tail -n 40 "$attempt_log" >&2 || true
    sudo rm -rf -- "$base"
    fail "debootstrap against $ubuntu_mirror failed after $attempt attempt(s)"
  fi
  printf 'debootstrap attempt %d hit a retryable acquire failure; retrying after backoff.\n' "$attempt" >&2
  sudo rm -rf -- "$base"
  sleep $((attempt * 30))
  attempt=$((attempt + 1))
done
sudo chroot "$base" /usr/bin/test -x /usr/bin/readelf
printf 'Shared disposable guest rootfs base ready: %s\n' "$base"
