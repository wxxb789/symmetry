#!/usr/bin/env python3
"""Atomically publish one sanitized Control receipt to the A-readable guest fixture."""

import ctypes
import errno
import json
import os
import re
import secrets
import stat
import sys

RENAME_NOREPLACE = 1


def fail(message: str) -> None:
    raise SystemExit(f"Control receipt publication failed: {message}")


def reject_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON member")
        result[key] = value
    return result


def write_all(fd: int, contents: bytes) -> None:
    view = memoryview(contents)
    while view:
        written = os.write(fd, view)
        if written <= 0:
            raise OSError(errno.EIO, "short receipt write")
        view = view[written:]


def main() -> int:
    if len(sys.argv) != 6:
        fail("expected source JSON, target directory, filename, and A UID/GID")
    source, directory, name, uid_text, gid_text = sys.argv[1:]
    if not name or name in (".", "..") or "/" in name or "\\" in name:
        fail("receipt filename is not a single path component")
    try:
        owner_uid, owner_gid = int(uid_text), int(gid_text)
        with open(source, "rb") as stream:
            source_stat = os.fstat(stream.fileno())
            contents = stream.read(65537)
        document = json.loads(contents, object_pairs_hook=reject_duplicate_keys)
    except (OSError, ValueError, json.JSONDecodeError):
        fail("source receipt is unreadable or invalid JSON")
    if not stat.S_ISREG(source_stat.st_mode) or source_stat.st_size != len(contents) or not contents or len(contents) > 65536:
        fail("source receipt must be a bounded regular file")
    if stat.S_IMODE(source_stat.st_mode) != 0o600:
        fail("source receipt must remain host-runner private")
    if not isinstance(document, dict) or document.get("version") != 1 or \
            document.get("operation") != "resource.sync" or document.get("state") != "succeeded" or \
            document.get("matched_count") != 1 or document.get("failure_present") is not False:
        fail("source receipt is not a Control database readback")
    required = (
        "case", "work_item_id", "task_id", "run_id", "generation", "runtime_id",
        "runtime_epoch", "claim_id", "resource_id", "provider", "resource_kind",
        "completed_at", "intent_id_sha256", "action_id_sha256", "result_metadata",
    )
    if any(key not in document for key in required):
        fail("receipt omitted an exact WorkItem/Run/fence/resource identity")
    if document["case"] not in (
        "pi_provider_action", "helper_sigkill_detached_child",
        "daemon_sigkill_helper_survives_detached_child",
    ) or document["provider"] != "github" or document["resource_kind"] != "repository":
        fail("receipt case/provider/resource kind is outside the Pi provider witness")
    if document["case"] != os.path.basename(directory) or document["run_id"] != name.removesuffix(".json"):
        fail("receipt case/RunID do not bind to the guest path")
    for key in ("work_item_id", "task_id", "run_id", "runtime_id", "claim_id", "resource_id", "completed_at"):
        if not isinstance(document[key], str) or not document[key]:
            fail(f"receipt {key} is absent")
    for key in ("generation", "runtime_epoch"):
        if not isinstance(document[key], int) or document[key] <= 0:
            fail(f"receipt {key} is invalid")
    for key in ("intent_id_sha256", "action_id_sha256"):
        if not isinstance(document[key], str) or re.fullmatch(r"[0-9a-f]{64}", document[key]) is None:
            fail(f"receipt {key} is not a lowercase SHA-256 digest")
    metadata = document["result_metadata"]
    resource = metadata.get("resource") if isinstance(metadata, dict) else None
    if not isinstance(metadata, dict) or not isinstance(resource, dict) or \
            metadata.get("operation") != "resource.sync" or metadata.get("resource_id") != document["resource_id"] or \
            not isinstance(metadata.get("projected"), bool) or resource.get("provider") != "github" or \
            resource.get("kind") != "repository" or not isinstance(resource.get("status"), str) or \
            not resource["status"] or not isinstance(resource.get("sync_status"), str) or not resource["sync_status"]:
        fail("receipt lacks sanitized terminal resource.sync result metadata")
    if "readback" in metadata and metadata["readback"] not in ("applied", "unconfirmed", "not_applied"):
        fail("receipt readback status is unsupported")

    directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC | os.O_NOFOLLOW)
    temporary = f".control-receipt-{secrets.token_hex(16)}.tmp"
    fd = -1
    try:
        directory_stat = os.fstat(directory_fd)
        if not stat.S_ISDIR(directory_stat.st_mode) or directory_stat.st_uid != owner_uid or directory_stat.st_gid != owner_gid or stat.S_IMODE(directory_stat.st_mode) != 0o700:
            fail("target directory must be A-owned mode 0700")
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600, dir_fd=directory_fd)
        os.fchown(fd, 0, owner_gid)
        write_all(fd, contents)
        os.fsync(fd)
        os.fchmod(fd, 0o440)
        os.fsync(fd)
        expected = os.fstat(fd)
        if not stat.S_ISREG(expected.st_mode) or expected.st_uid != 0 or expected.st_gid != owner_gid or stat.S_IMODE(expected.st_mode) != 0o440 or expected.st_nlink != 1 or expected.st_size != len(contents):
            fail("temporary receipt ownership/mode changed")

        libc = ctypes.CDLL(None, use_errno=True)
        renameat2 = libc.renameat2
        renameat2.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
        renameat2.restype = ctypes.c_int
        if renameat2(directory_fd, os.fsencode(temporary), directory_fd, os.fsencode(name), RENAME_NOREPLACE) != 0:
            error = ctypes.get_errno()
            raise OSError(error, os.strerror(error))
        published = os.stat(name, dir_fd=directory_fd, follow_symlinks=False)
        if not stat.S_ISREG(published.st_mode) or published.st_uid != 0 or published.st_gid != owner_gid or stat.S_IMODE(published.st_mode) != 0o440 or published.st_nlink != 1 or published.st_size != len(contents) or published.st_dev != expected.st_dev or published.st_ino != expected.st_ino:
            fail("published receipt ownership/mode/inode changed")
        os.fsync(directory_fd)
    except OSError as error:
        fail(f"{error.strerror or error}")
    finally:
        if fd >= 0:
            os.close(fd)
        try:
            os.unlink(temporary, dir_fd=directory_fd)
        except FileNotFoundError:
            pass
        os.close(directory_fd)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
