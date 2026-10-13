#!/usr/bin/env python3
"""Validate the live-B checkpoint before removing the exact test selector."""

from __future__ import annotations

import json
import os
import pathlib
import re
import stat
import sys


CHECKPOINT_FIELDS = {
    "version",
    "case",
    "helper_action",
    "run_id",
    "generation",
    "gate_pid",
    "gate_identity_sha256",
    "host_uid",
    "host_gid",
    "uid_map",
    "gid_map",
    "setgroups",
    "cap_eff",
    "no_new_privs",
    "leaf_device",
    "leaf_inode",
    "leaf_membership",
    "witness_action_observed",
}
SELECTOR_FIELDS = {"version", "run_id", "generation", "helper_action", "hold_for_release_file"}
ACTION = "crash_after_release_before_register"
CASE = "helper_exit_after_release_before_register"
CAP_EFF_ZERO = "0000000000000000"


def fail(stage: str) -> None:
    raise SystemExit("live-B owner witness checkpoint failed: " + stage)


def reject_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON member")
        result[key] = value
    return result


def read_strict_json(path: pathlib.Path, expected_info: os.stat_result) -> dict:
    try:
        fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
        try:
            opened = os.fstat(fd)
            if not stat.S_ISREG(opened.st_mode) or \
                (opened.st_dev, opened.st_ino, opened.st_mode, opened.st_uid, opened.st_gid, opened.st_nlink, opened.st_size) != \
                (expected_info.st_dev, expected_info.st_ino, expected_info.st_mode, expected_info.st_uid, expected_info.st_gid, expected_info.st_nlink, expected_info.st_size) or \
                opened.st_size > 8192:
                fail("checkpoint/selector changed during no-follow open")
            chunks = []
            remaining = 8193
            while remaining:
                chunk = os.read(fd, min(4096, remaining))
                if not chunk:
                    break
                chunks.append(chunk)
                remaining -= len(chunk)
            if remaining == 0:
                fail("checkpoint/selector size bound")
            raw = b"".join(chunks)
        finally:
            os.close(fd)
        value = json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=reject_duplicate_keys)
    except (OSError, UnicodeError, ValueError, json.JSONDecodeError):
        fail("strict checkpoint/selector JSON")
    if not isinstance(value, dict):
        fail("checkpoint/selector JSON object")
    return value


def one_status_field(status: dict[str, str], name: str) -> list[str]:
    value = status.get(name)
    if value is None:
        fail("live process status " + name)
    return value.split()


def process_status(pid: int) -> dict[str, str]:
    result = {}
    try:
        for line in pathlib.Path(f"/proc/{pid}/status").read_text(encoding="ascii").splitlines():
            key, separator, value = line.partition(":")
            if separator:
                result[key] = value.strip()
    except OSError:
        fail("exact B gate process remains live")
    return result


def process_identity_sha256(pid: int) -> str:
    try:
        boot_id = pathlib.Path("/proc/sys/kernel/random/boot_id").read_text(encoding="ascii").strip()
        stat_text = pathlib.Path(f"/proc/{pid}/stat").read_text(encoding="ascii")
        closing = stat_text.rfind(")")
        if closing < 0 or closing + 1 >= len(stat_text) or stat_text[closing + 1] != " ":
            fail("exact B process stat identity")
        fields = stat_text[closing + 1 :].split()
        if len(fields) <= 19 or not re.fullmatch(r"[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}", boot_id):
            fail("canonical B process boot/start identity")
        start_time = int(fields[19], 10)
        target_link = os.readlink(f"/proc/{pid}/ns/pid")
        self_link = os.readlink("/proc/self/ns/pid")
        target_match = re.fullmatch(r"pid:\[([1-9][0-9]*)\]", target_link)
        self_match = re.fullmatch(r"pid:\[([1-9][0-9]*)\]", self_link)
        target_inode = int(target_match.group(1), 10) if target_match else 0
        self_inode = int(self_match.group(1), 10) if self_match else 0
        target_stat_inode = os.stat(f"/proc/{pid}/ns/pid").st_ino
        self_stat_inode = os.stat("/proc/self/ns/pid").st_ino
    except (OSError, ValueError):
        fail("exact B process creation identity is unavailable")
    if start_time <= 0 or target_link != self_link or target_inode != self_inode or \
        target_inode != target_stat_inode or self_inode != self_stat_inode:
        fail("exact B process PID namespace/start identity")
    identity = f"linux:v2:{boot_id}:{target_inode}:{pid}:{start_time}"
    import hashlib
    return hashlib.sha256(identity.encode("ascii")).hexdigest()


def exact_positive(value, stage: str) -> bool:
    if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
        fail(stage)
    return True


def live_unified_cgroup(pid: int) -> pathlib.Path:
    try:
        entries = [
            line.rstrip("\n").split(":", 2)[2]
            for line in pathlib.Path(f"/proc/{pid}/cgroup").read_text(encoding="ascii").splitlines()
            if line.startswith("0::")
        ]
    except OSError:
        fail("exact B gate cgroup membership")
    if len(entries) != 1 or not entries[0].startswith("/") or entries[0] == "/" or \
        ".." in pathlib.PurePosixPath(entries[0]).parts:
        fail("single canonical unified cgroup membership")
    leaf = pathlib.Path("/sys/fs/cgroup") / entries[0].lstrip("/")
    try:
        info = leaf.stat()
        processes = [int(value) for value in (leaf / "cgroup.procs").read_text(encoding="ascii").split()]
        events = (leaf / "cgroup.events").read_text(encoding="ascii").splitlines()
    except (OSError, ValueError):
        fail("pinned live cgroup leaf inspection")
    if pid not in processes or "populated 1" not in events:
        fail("exact B gate is live in populated pinned leaf")
    return leaf


def require_no_writable_cgroup_fds(pid: int) -> None:
    fd_root = pathlib.Path(f"/proc/{pid}/fd")
    try:
        descriptors = list(fd_root.iterdir())
    except OSError:
        fail("live gate FD inventory")
    for descriptor in descriptors:
        try:
            target = os.readlink(descriptor)
        except FileNotFoundError:
            continue
        except OSError:
            fail("live gate FD target inspection")
        if not target.startswith("/sys/fs/cgroup/"):
            continue
        try:
            fields = pathlib.Path(f"/proc/{pid}/fdinfo/{descriptor.name}").read_text(encoding="ascii").splitlines()
        except OSError:
            fail("live gate cgroup FD flags")
        flags_line = next((line.split(":", 1)[1].strip() for line in fields if line.startswith("flags:")), None)
        if flags_line is None:
            fail("live gate cgroup FD flags")
        try:
            flags = int(flags_line, 8)
        except ValueError:
            fail("live gate cgroup FD flags")
        if flags & os.O_ACCMODE != os.O_RDONLY:
            fail("no writable cgroup control descriptors in B gate")


def main() -> int:
    if len(sys.argv) != 7:
        fail("expected checkpoint, selector, and fixed A/B UID/GID")
    checkpoint_path, selector_path = map(pathlib.Path, sys.argv[1:3])
    try:
        a_uid, a_gid, b_uid, b_gid = (int(value, 10) for value in sys.argv[3:7])
    except ValueError:
        fail("fixed A/B identity arguments")

    checkpoint_info = checkpoint_path.lstat()
    if not stat.S_ISREG(checkpoint_info.st_mode) or stat.S_ISLNK(checkpoint_info.st_mode) or \
        (checkpoint_info.st_uid, checkpoint_info.st_gid, stat.S_IMODE(checkpoint_info.st_mode), checkpoint_info.st_nlink) != \
        (a_uid, a_gid, 0o600, 1):
        fail("A-owned mode-0600 singly-linked live-B checkpoint")
    checkpoint = read_strict_json(checkpoint_path, checkpoint_info)
    if set(checkpoint) != CHECKPOINT_FIELDS or not isinstance(checkpoint.get("version"), int) or \
        isinstance(checkpoint.get("version"), bool) or checkpoint.get("version") != 1 or \
        checkpoint.get("case") != CASE or checkpoint.get("helper_action") != ACTION:
        fail("exact checkpoint schema/case")

    selector_info = selector_path.lstat()
    if not stat.S_ISREG(selector_info.st_mode) or stat.S_ISLNK(selector_info.st_mode) or \
        (selector_info.st_uid, selector_info.st_gid, stat.S_IMODE(selector_info.st_mode), selector_info.st_nlink) != \
        (0, a_gid, 0o440, 1):
        fail("root-owned A-readable singly-linked selector")
    selector = read_strict_json(selector_path, selector_info)
    if set(selector) != SELECTOR_FIELDS or not isinstance(selector.get("version"), int) or \
        isinstance(selector.get("version"), bool) or selector.get("version") != 1 or \
        selector.get("helper_action") != ACTION or selector.get("hold_for_release_file") is not True:
        fail("exact root-owned helper action selector")
    if not isinstance(selector.get("run_id"), str) or not selector["run_id"] or \
        not isinstance(selector.get("generation"), int) or isinstance(selector.get("generation"), bool) or \
        selector["generation"] <= 0:
        fail("selector RunKey fields")
    if not isinstance(checkpoint.get("run_id"), str) or not checkpoint["run_id"] or \
        not isinstance(checkpoint.get("generation"), int) or isinstance(checkpoint.get("generation"), bool) or \
        checkpoint["generation"] <= 0:
        fail("checkpoint RunKey types")
    if checkpoint.get("run_id") != selector["run_id"] or checkpoint.get("generation") != selector["generation"]:
        fail("checkpoint RunKey matches root selector")

    gate_pid = checkpoint.get("gate_pid")
    if not exact_positive(gate_pid, "positive B gate PID"):
        fail("positive B gate PID")
    if checkpoint.get("host_uid") != b_uid or checkpoint.get("host_gid") != b_gid:
        fail("checkpoint host B credentials")
    if checkpoint.get("uid_map") != f"1 {b_uid} 1" or checkpoint.get("gid_map") != f"1 {b_gid} 1" or \
        checkpoint.get("setgroups") != "deny" or checkpoint.get("cap_eff") != CAP_EFF_ZERO or \
        checkpoint.get("no_new_privs") is not True or checkpoint.get("leaf_membership") is not True or \
        checkpoint.get("witness_action_observed") is not True:
        fail("checkpoint UID/GID maps, setgroups, capabilities, NNP, leaf, and action facts")
    if not isinstance(checkpoint.get("gate_identity_sha256"), str) or \
        re.fullmatch(r"[0-9a-f]{64}", checkpoint["gate_identity_sha256"]) is None:
        fail("hashed exact B gate process identity")
    exact_positive(checkpoint.get("leaf_device"), "positive pinned leaf device")
    exact_positive(checkpoint.get("leaf_inode"), "positive pinned leaf inode")

    status = process_status(gate_pid)
    try:
        actual_uids = [int(value) for value in one_status_field(status, "Uid")]
        actual_gids = [int(value) for value in one_status_field(status, "Gid")]
        actual_groups = [int(value) for value in one_status_field(status, "Groups")]
        actual_cap_eff = int(one_status_field(status, "CapEff")[0], 16)
        actual_no_new_privs = int(one_status_field(status, "NoNewPrivs")[0], 10)
    except ValueError:
        fail("parse live gate credentials/capabilities")
    if actual_uids != [b_uid] * 4 or actual_gids != [b_gid] * 4 or actual_groups or \
        actual_cap_eff != 0 or actual_no_new_privs != 1:
        fail("live gate B UID/GID, empty supplementary groups, CapEff, and NoNewPrivs")
    if process_identity_sha256(gate_pid) != checkpoint["gate_identity_sha256"]:
        fail("live B PID/start/PID-namespace identity matches the authenticated Status")
    process_root = pathlib.Path(f"/proc/{gate_pid}")
    try:
        uid_map = (process_root / "uid_map").read_text(encoding="ascii").strip()
        gid_map = (process_root / "gid_map").read_text(encoding="ascii").strip()
        setgroups = (process_root / "setgroups").read_text(encoding="ascii").strip()
    except OSError:
        fail("live gate user namespace map inspection")
    if uid_map != f"1 {b_uid} 1" or gid_map != f"1 {b_gid} 1" or setgroups != "deny":
        fail("live gate exact UID/GID maps and setgroups policy")

    leaf = live_unified_cgroup(gate_pid)
    leaf_info = leaf.stat()
    if leaf_info.st_dev != checkpoint["leaf_device"] or leaf_info.st_ino != checkpoint["leaf_inode"]:
        fail("live gate remains in the exact checkpointed pinned leaf")
    require_no_writable_cgroup_fds(gate_pid)

    parent_fd = os.open(selector_path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        current = os.stat(selector_path.name, dir_fd=parent_fd, follow_symlinks=False)
        if current.st_ino != selector_info.st_ino or current.st_dev != selector_info.st_dev:
            fail("selector inode remains exact before unlink")
        os.unlink(selector_path.name, dir_fd=parent_fd)
        os.fsync(parent_fd)
    finally:
        os.close(parent_fd)
    if selector_path.exists() or selector_path.is_symlink():
        fail("single root-owned selector is durably unlinked")
    print("verified exact live-B gate and removed its matching root-owned selector")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
