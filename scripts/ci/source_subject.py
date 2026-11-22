#!/usr/bin/env python3
"""One implementation of every source-subject rule, as five named variants.

A source subject binds evidence to the bytes it was produced from, and the five
witnesses in this directory bind five different shapes of it. This module owns those
shapes; each tool keeps its CLI, its refusal text and its receipt plumbing and
delegates the rule here.

Variant A, ``index_mode_digest(repository, expected_commit)``: the committed tree of an
exactly expected commit in a clean checkout. ``rev-parse HEAD`` must equal
``expected_commit`` and ``status --porcelain=v1 --untracked-files=all`` must be empty,
before and after hashing; the tree comes from ``ls-tree --full-tree -r -z`` and is read
through one ``cat-file --batch`` process; a non-blob entry is refused, any mode is
admitted, and the entries are accumulated in path order.

Variant B, ``working_tree_digest(root)``: a working-tree walk over the declared
exclusions (``WORKING_TREE_EXCLUDED_*``): ``.git`` in either form, everything under
``docs/``, ``.tmp/``, the build and report output directories (``dist``,
``playwright-report``, ``test-results``), ``__pycache__`` bytecode caches, the whole
``node_modules`` tree and the tool caches (``.vite``, ``.vite-temp``, ``.vitest-cache``)
wherever else they appear. Only a path's parent directories decide exclusion, a symlinked
file is never an entry, directories are never entries, and the mode is the tree's own
execute bits (any of owner, group or other sets ``100755``).

Variant C, ``committed_tree_digest(records, read_object, modes=...)``: the committed tree
of one commit, read one object at a time by the caller's runner, admitting only those
modes (``FILE_MODES`` for a real source tree). Its revision provenance is
``head_revision`` (variant E), or ``declared_file_identities`` /
``verify_declared_file_set`` for a parent-observed attestation.

Variant D, ``head_with_manifest(root, config, git, base_sha=..., scope=...)``: ``rev-parse
HEAD`` must equal the declared head, the declared base must be the frozen one, every
manifest entry must name a file inside the root carrying the declared digest, and the
scoped ``status --porcelain=v1 -z --untracked-files=all`` must stay inside the manifest
(renames and copies skipped); a dirty path is refused unless the config consents, and the
manifest digest is the sha256 of the manifest's canonical bytes.

Variant E, ``head_revision(git)``: the bare head revision, as ``git rev-parse HEAD``
prints it.

Import convention: ``import source_subject`` from a sibling in this directory, resolved
from the importing script's own directory (``sys.path[0]``), the way ``revision_scope``
is imported; every repository entry point runs a ``scripts/ci`` file as a script. This
file is a library - nothing runs at import time.
"""

from __future__ import annotations

import hashlib
import json
import pathlib
import subprocess
from collections.abc import Callable, Collection, Iterable, Mapping, Sequence
from typing import NamedTuple, TypedDict

DIGEST_PREFIX = "sha256:"
MODE_REGULAR = "100644"
MODE_EXECUTABLE = "100755"
EXECUTE_BITS = 0o111
FILE_MODES = frozenset({MODE_REGULAR.encode("ascii"), MODE_EXECUTABLE.encode("ascii")})
WORKING_TREE_EXCLUDED_DIRECTORIES = {
    ".git",
    "__pycache__",
    ".tmp",
    "node_modules",
    "dist",
    "playwright-report",
    "test-results",
}
WORKING_TREE_EXCLUDED_CACHE_NAMES = {".vite", ".vite-temp", ".vitest-cache"}
WORKING_TREE_EXCLUDED_ROOTS = {".git", "docs"}

# A record is `mode kind object-id`, a tab, the path: what `ls-tree -z` prints.
TreeRecord = tuple[bytes, bytes, bytes, bytes]


class Refused(Exception):
    """A binding rule refused; `code` names the rule and `detail` the offending item."""

    def __init__(self, code: str, detail: str | None = None) -> None:
        super().__init__(code if detail is None else f"{code}: {detail}")
        self.code = code
        self.detail = detail


class SourceSubject(TypedDict):
    commit: str
    tree_digest: str


class DirtyScope(NamedTuple):
    """A declared dirty surface, in the three spellings its query and filter use."""

    pathspecs: tuple[str, ...]
    prefixes: tuple[str, ...]
    names: frozenset[str]


def sha256_hex(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def sha256_digest(data: bytes) -> str:
    return DIGEST_PREFIX + sha256_hex(data)


def file_sha256_hex(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def file_digest(path: pathlib.Path) -> str:
    return DIGEST_PREFIX + file_sha256_hex(path)


def file_identity(path: pathlib.Path) -> dict[str, object]:
    digest = file_sha256_hex(path)
    return {"path": str(path), "bytes": path.stat().st_size, "sha256": DIGEST_PREFIX + digest}


def canonical_bytes(value: object) -> bytes:
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode()


def manifest_digest(entries: object) -> str:
    return sha256_hex(canonical_bytes(entries))


def head_revision(git: Callable[..., bytes]) -> str:
    return git("rev-parse", "HEAD").decode("ascii", "replace").strip()


def declared_file_identities(root: pathlib.Path, paths: Sequence[str]) -> dict[str, dict[str, object]]:
    return {name: file_identity(root / name) for name in paths}


def verify_declared_file_set(
    root: pathlib.Path,
    paths: Sequence[str],
    declared: Mapping[str, Mapping[str, object]],
) -> dict[str, dict[str, object]]:
    """Return the observed identities, refusing a declaration that decides otherwise."""
    if set(declared) != set(paths):
        raise Refused("declared_path_set_mismatch")
    observed = declared_file_identities(root, paths)
    for name in paths:
        expected = declared[name]
        if not isinstance(expected, dict) or set(expected) != {"bytes", "sha256"} or type(expected["bytes"]) is not int:
            raise Refused("declared_entry_shape", name)
        if expected["bytes"] != observed[name]["bytes"] or expected["sha256"] != observed[name]["sha256"]:
            raise Refused("declared_content_mismatch", name)
    return observed


def tree_records(listing: bytes) -> list[TreeRecord]:
    records = []
    for record in listing.split(b"\0"):
        if not record:
            continue
        header, separator, path = record.partition(b"\t")
        fields = header.split(b" ")
        if not separator or len(fields) != 3:
            raise Refused("invalid_tree_record", record[:200].decode("utf-8", "replace"))
        records.append((fields[0], fields[1], path, fields[2]))
    return records


def tree_entry(mode: bytes, kind: bytes, path: bytes, blob: bytes) -> bytes:
    digest = sha256_hex(blob).encode("ascii")
    return b"\0".join((mode, kind, path, DIGEST_PREFIX.encode("ascii") + digest)) + b"\0"


def committed_tree_digest(
    records: Iterable[TreeRecord],
    read_object: Callable[[bytes], bytes],
    *,
    modes: Collection[bytes] | None,
) -> str:
    entries = []
    for mode, kind, path, object_id in records:
        if kind != b"blob" or (modes is not None and mode not in modes):
            raise Refused("tree_entry_not_a_file", path.decode("utf-8", "replace"))
        entries.append((path, tree_entry(mode, kind, path, read_object(object_id))))
    return sha256_digest(b"".join(entry for _path, entry in sorted(entries)))


def working_tree_paths(root: pathlib.Path) -> list[str]:
    paths = []
    for path in root.rglob("*"):
        parts = path.relative_to(root).parts
        if not parts or parts[0] in WORKING_TREE_EXCLUDED_ROOTS:
            continue
        if any(
            part in WORKING_TREE_EXCLUDED_DIRECTORIES or part in WORKING_TREE_EXCLUDED_CACHE_NAMES
            for part in parts[:-1]
        ):
            continue
        if path.is_file() and not path.is_symlink():
            paths.append(path.relative_to(root).as_posix())
    return sorted(paths, key=lambda value: value.encode("utf-8"))


def working_tree_entry(root: pathlib.Path, relative: str) -> bytes:
    target = root / relative
    mode = MODE_EXECUTABLE if target.stat().st_mode & EXECUTE_BITS else MODE_REGULAR
    digest = file_sha256_hex(target).encode("ascii")
    return b"\0".join((mode.encode("ascii"), DIGEST_PREFIX.encode("ascii") + digest, relative.encode("utf-8"))) + b"\0"


def working_tree_digest(root: pathlib.Path) -> tuple[int, str]:
    paths = working_tree_paths(root)
    digest = hashlib.sha256()
    for relative in paths:
        digest.update(working_tree_entry(root, relative))
    return len(paths), DIGEST_PREFIX + digest.hexdigest()


def working_tree_executable_paths(root: pathlib.Path) -> list[str]:
    return [relative for relative in working_tree_paths(root) if (root / relative).stat().st_mode & EXECUTE_BITS]


class _BatchReader:
    """One `cat-file --batch` process, asked for one object at a time."""

    def __init__(self, batch: subprocess.Popen[bytes]) -> None:
        if batch.stdin is None or batch.stdout is None:
            raise Refused("batch_unavailable")
        self._batch = batch
        self._stdin = batch.stdin
        self._stdout = batch.stdout

    def read(self, object_id: bytes) -> bytes:
        self._stdin.write(object_id + b"\n")
        self._stdin.flush()
        header = self._stdout.readline().split()
        if len(header) != 3 or header[1] != b"blob":
            raise Refused("invalid_source_object")
        size = int(header[2])
        blob = self._stdout.read(size)
        if len(blob) != size or self._stdout.read(1) != b"\n":
            raise Refused("truncated_source_blob")
        return blob

    def finish(self) -> None:
        self._stdin.close()
        if self._batch.wait() != 0:
            raise Refused("batch_failed")


def index_mode_digest(repository: str, expected_commit: str) -> SourceSubject:
    command = ["git", "-c", f"safe.directory={repository}", "-C", repository]

    def git(*arguments: str) -> bytes:
        return subprocess.check_output([*command, *arguments], stderr=subprocess.PIPE)

    commit = git("rev-parse", "HEAD").decode("ascii").strip()
    if commit != expected_commit:
        raise Refused("head_mismatch")
    if git("status", "--porcelain=v1", "--untracked-files=all"):
        raise Refused("dirty_checkout")
    records = sorted(tree_records(git("ls-tree", "--full-tree", "-r", "-z", commit)), key=lambda record: record[2])
    with subprocess.Popen(
        [*command, "cat-file", "--batch"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
    ) as batch:
        reader = _BatchReader(batch)
        tree_digest = committed_tree_digest(records, reader.read, modes=None)
        reader.finish()

    if git("rev-parse", "HEAD").decode("ascii").strip() != commit or git(
        "status", "--porcelain=v1", "--untracked-files=all"
    ):
        raise Refused("checkout_changed")
    return {"commit": commit, "tree_digest": tree_digest}


def head_with_manifest(
    root: pathlib.Path,
    config: Mapping[str, object],
    git: Callable[..., bytes],
    *,
    base_sha: str,
    scope: DirtyScope,
) -> dict[str, object]:
    if git("rev-parse", "HEAD").decode().strip() != config["code_head_sha"]:
        raise Refused("head_mismatch")
    if config["code_base_sha"] != base_sha:
        raise Refused("base_mismatch")
    entries = config["changed_source_manifest"]
    if not isinstance(entries, list) or not entries:
        raise Refused("manifest_required")
    paths = set()
    for entry in entries:
        relative = entry["path"]
        path = (root / relative).resolve()
        if not path.is_relative_to(root) or not path.is_file() or relative in paths:
            raise Refused("manifest_path_invalid")
        paths.add(relative)
        if file_sha256_hex(path) != entry["sha256"].removeprefix(DIGEST_PREFIX):
            raise Refused("manifest_digest_mismatch")
    dirty = git("status", "--porcelain=v1", "-z", "--untracked-files=all", "--", *scope.pathspecs).split(b"\0")
    relevant = set()
    index = 0
    while index < len(dirty):
        record = dirty[index]
        index += 1
        if not record:
            continue
        status, name = record[:2], record[3:].decode()
        if b"R" in status or b"C" in status:
            index += 1
        if name.startswith(scope.prefixes) or name in scope.names:
            relevant.add(name)
    if not relevant <= paths:
        raise Refused("dirty_source_not_declared")
    if relevant and config.get("allow_manifest_bound_dirty") is not True:
        raise Refused("dirty_source_not_consented")
    return {
        "head_sha": config["code_head_sha"],
        "base_sha": base_sha,
        "changed_source_manifest": entries,
        "manifest_bound_dirty_paths": sorted(relevant),
        "manifest_sha256": manifest_digest(entries),
    }
