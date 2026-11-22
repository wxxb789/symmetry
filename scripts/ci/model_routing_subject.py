from __future__ import annotations

import hashlib
import importlib.util
import os
import pathlib
import re
import stat
import subprocess
from collections.abc import Iterable
from typing import Any

from model_routing_records import PathMatcher, Refused, is_protected_asset, is_test_path, require_commit

SUBJECT_NAMESPACE = b"symmetry-acceptance-subject/v1"


SOURCE_SUBJECT_SCRIPT = pathlib.Path(__file__).with_name("linux-v2-source-subject.py").resolve()


def git_output(repository: str, *arguments: str) -> bytes:
    # Replacement refs stay disabled for every read: a checkout can carry
    # refs/replace that would otherwise serve different bytes under a frozen object
    # id, and refuse_object_substitution refuses such a checkout outright.
    command = ["git", "--no-replace-objects", "-c", f"safe.directory={repository}", "-C", repository, *arguments]
    try:
        completed = subprocess.run(command, capture_output=True, check=False)
    except OSError as error:
        raise Refused(f"could not run git: {error}") from error
    if completed.returncode != 0:
        detail = completed.stderr.decode("utf-8", "replace").strip()
        raise Refused(f"git {' '.join(arguments)} failed ({completed.returncode}): {detail}")
    return completed.stdout


def git_text(repository: str, *arguments: str) -> str:
    try:
        return git_output(repository, *arguments).decode("utf-8", errors="strict").strip()
    except UnicodeDecodeError:
        raise Refused(f"git {' '.join(arguments)} did not return UTF-8 text") from None


def decode_path(raw: bytes) -> str:
    try:
        return raw.decode("utf-8", errors="strict")
    except UnicodeDecodeError:
        raise Refused("the checkout contains a path that is not valid UTF-8") from None


def utf8_path_key(path: str) -> bytes:
    """Byte sort key for a checkout path; refuse paths that are not valid UTF-8."""
    try:
        return path.encode("utf-8")
    except UnicodeEncodeError:
        raise Refused("the checkout contains a path that is not valid UTF-8") from None


def executable_bits_tracked(checkout: pathlib.Path) -> bool:
    """Whether this host and checkout can represent Git's executable bit.

    When they cannot, Git itself ignores mode-only differences, and so does the seal:
    reporting a pristine executable file as modified would be a false positive.
    """
    if os.name != "posix":
        return False
    command = ["git", "-c", f"safe.directory={checkout}", "-C", str(checkout), "config", "--bool", "core.filemode"]
    try:
        completed = subprocess.run(command, capture_output=True, check=False)
    except OSError as error:
        raise Refused(f"could not read git config core.filemode: {error}") from error
    if completed.returncode != 0:
        return True  # git's own default for core.filemode is true
    return completed.stdout.decode("utf-8", "replace").strip() != "false"


def iter_source_paths(checkout: pathlib.Path) -> list[str]:
    """Every checkout file and symlink; only .git internals are excluded.

    Tool run state is not exempt: `frontend/vite.config.ts` and
    `frontend/vitest.config.ts` resolve SYMMETRY_TOOL_CACHE_DIR (exported by
    `check` for every gate) so vitest/vite caches and Vite's config-loader temp
    files land in the run store, and a gate that still writes such state into the
    checkout changes the sealed revision and fails closed.
    """
    paths: list[str] = []
    for root, dirnames, filenames in os.walk(checkout):
        for dirname in list(dirnames):
            if dirname == ".git":
                dirnames.remove(dirname)
                continue
            candidate = pathlib.Path(root, dirname)
            if candidate.is_symlink():
                dirnames.remove(dirname)
                paths.append(candidate.relative_to(checkout).as_posix())
                continue
            try:
                info = candidate.lstat()
            except OSError as error:
                raise Refused(
                    f"cannot read source entry {candidate.relative_to(checkout).as_posix()}: {error}"
                ) from error
            if getattr(info, "st_reparse_tag", 0):
                raise Refused(
                    "unsupported reparse-point directory in the checkout: " + candidate.relative_to(checkout).as_posix()
                )
        for name in filenames:
            if name == ".git":
                continue
            paths.append(pathlib.Path(root, name).relative_to(checkout).as_posix())
    return sorted(paths, key=utf8_path_key)


def refuse_escaping_symlink(checkout: pathlib.Path, path: str, raw_target: bytes) -> None:
    """Refuse a bound symlink whose real target leaves the manifest scope.

    The manifest binds the link text, but every gate reads the bytes behind the
    link. A target that leaves the checkout, or enters `.git` (which the manifest
    excludes), would let a worker change what runs without changing any bound byte,
    so such a link is refused instead of being trusted. In-tree targets stay
    legitimate: each hop and the final target are enumerated and bound as their own
    manifest entries.
    """
    try:
        resolved = os.path.realpath(pathlib.Path(checkout, *path.split("/")))
    except (OSError, RuntimeError) as error:
        raise Refused(f"cannot resolve the bound symlink {path}: {error}") from error
    try:
        relative = os.path.relpath(resolved, os.path.realpath(checkout))
    except ValueError:  # a Windows target on another drive is never in-tree
        relative = os.pardir
    display = raw_target.decode("utf-8", "replace")[:200]
    if relative == os.pardir or relative.startswith(os.pardir + os.sep):
        raise Refused(f"the bound symlink {path} escapes the checkout; the gates read its real target: -> {display!r}")
    if ".git" in pathlib.PurePath(relative).parts:
        raise Refused(f"the bound symlink {path} points into .git, which the Source manifest excludes: -> {display!r}")


def read_worktree_entries(checkout: pathlib.Path) -> dict[str, tuple[str, str, str]]:
    """Bind bytes and modes of every checkout file, including ignored files."""
    executable_bits = executable_bits_tracked(checkout)
    entries: dict[str, tuple[str, str, str]] = {}
    for path in iter_source_paths(checkout):
        target = pathlib.Path(checkout, *path.split("/"))
        try:
            info = target.lstat()
        except OSError as error:
            raise Refused(f"cannot read source entry {path}: {error}") from error
        if stat.S_ISLNK(info.st_mode):
            mode, kind = "120000", "symlink"
            try:
                data = os.fsencode(os.readlink(target))
            except OSError as error:
                raise Refused(f"cannot read symlink {path}: {error}") from error
            refuse_escaping_symlink(checkout, path, data)
            digest = hashlib.sha256(data)
        elif stat.S_ISREG(info.st_mode):
            kind = "file"
            mode = "100755" if executable_bits and info.st_mode & 0o111 else "100644"
            digest = hashlib.sha256()
            try:
                with open(target, "rb") as stream:
                    while True:
                        chunk = stream.read(1 << 20)
                        if not chunk:
                            break
                        digest.update(chunk)
            except OSError as error:
                raise Refused(f"cannot read source entry {path}: {error}") from error
        else:
            raise Refused(f"unsupported source entry (not a regular file or symlink): {path}")
        entries[path] = (mode, kind, "sha256:" + digest.hexdigest())
    if set(iter_source_paths(checkout)) != set(entries):
        raise Refused("source changed while binding its manifest")
    return entries


def manifest_digest(entries: dict[str, tuple[str, str, str]]) -> tuple[str, int]:
    hasher = hashlib.sha256()
    for path in sorted(entries, key=utf8_path_key):
        mode, kind, digest = entries[path]
        hasher.update(mode.encode("ascii") + b"\0" + kind.encode("ascii") + b"\0")
        hasher.update(utf8_path_key(path) + b"\0" + digest.encode("ascii") + b"\0")
    return "sha256:" + hasher.hexdigest(), len(entries)


def read_base_tree(checkout: pathlib.Path, base: str) -> dict[str, tuple[str, str]]:
    """Base commit blob entries: path -> (mode, object id)."""
    entries: dict[str, tuple[str, str]] = {}
    for record in git_output(str(checkout), "ls-tree", "--full-tree", "-r", "-z", base).split(b"\0"):
        if not record:
            continue
        header, path_bytes = record.split(b"\t", 1)
        fields = header.split(b" ", 2)
        if len(fields) != 3:
            raise Refused("unexpected git ls-tree output")
        mode, kind, object_id = fields
        path = decode_path(path_bytes)
        if kind != b"blob":
            raise Refused(f"the base tree contains a non-blob or submodule entry: {path}")
        try:
            mode_text = mode.decode("ascii")
            object_text = object_id.decode("ascii")
        except UnicodeDecodeError:
            raise Refused(f"unexpected git ls-tree output for {path}") from None
        if not re.fullmatch(r"[0-7]{6}", mode_text) or not re.fullmatch(r"[0-9a-f]{40,64}", object_text):
            raise Refused(f"unexpected git ls-tree output for {path}")
        entries[path] = (mode_text, object_text)
    return entries


def list_base_paths(checkout: pathlib.Path, base: str) -> set[str]:
    return set(read_base_tree(checkout, base))


def refuse_index_hacks(checkout: pathlib.Path) -> None:
    """Refuse index entries whose flags can hide worktree content from git diff."""
    suspect: list[str] = []
    for record in git_output(str(checkout), "ls-files", "-v", "-z").split(b"\0"):
        if not record:
            continue
        if record[:1] == b"H":
            continue
        suspect.append(decode_path(record[2:] if record[1:2] == b" " else record[1:]))
    if suspect:
        raise Refused(
            "seal refuses a checkout whose Git index carries assume-unchanged, skip-worktree or other "
            "nonstandard entries; re-create the isolated checkout: " + ", ".join(suspect[:5])
        )


def refuse_object_substitution(checkout: pathlib.Path) -> None:
    """Refuse a checkout whose Git object view is rewritten.

    A replacement ref serves different bytes under a frozen object id, and a
    `.git/info/grafts` file rewrites commit ancestry; either mechanism invalidates
    the byte and object-id binding the seal derives, even while the worktree still
    matches. Object reads additionally run with replacement refs disabled, so this
    check is the fail-closed boundary rather than the only defense.
    """
    replacements = git_output(str(checkout), "for-each-ref", "refs/replace").decode("utf-8", "replace").strip()
    if replacements:
        names = sorted(line.split("\t", 1)[1] for line in replacements.splitlines() if "\t" in line)
        raise Refused(
            "the checkout carries Git replacement refs, which substitute object bytes under frozen "
            "ids; re-create the isolated checkout from an unrewritten source: " + ", ".join(names[:5])
        )
    resolved = git_output(str(checkout), "rev-parse", "--git-path", "info/grafts").decode("utf-8", "replace").strip()
    grafts = pathlib.Path(resolved)
    if not grafts.is_absolute():
        grafts = checkout / grafts
    if grafts.exists():
        raise Refused(
            "the checkout rewrites Git ancestry with .git/info/grafts; remove it and re-create the isolated checkout"
        )


def read_base_blob_digests(checkout: pathlib.Path, objects: Iterable[str]) -> dict[str, str]:
    """Hash base blob bytes straight out of the object store.

    Object-store content is what the base commit stores: no clean/smudge filter,
    attribute, index flag, stat cache or replacement ref can influence it. The whole
    request and its output are held in memory, which is bounded by the base tree size.
    """
    ordered = sorted(set(objects))
    if not ordered:
        return {}
    request = "".join(object_id + "\n" for object_id in ordered).encode("ascii")
    command = [
        "git",
        "--no-replace-objects",
        "-c",
        f"safe.directory={checkout}",
        "-C",
        str(checkout),
        "cat-file",
        "--batch",
    ]
    try:
        completed = subprocess.run(command, input=request, capture_output=True, check=False)
    except OSError as error:
        raise Refused(f"could not run git cat-file: {error}") from error
    if completed.returncode != 0:
        detail = completed.stderr.decode("utf-8", "replace").strip()
        raise Refused(f"git cat-file --batch failed ({completed.returncode}): {detail}")
    payload = completed.stdout
    digests: dict[str, str] = {}
    offset = 0
    for object_id in ordered:
        newline = payload.find(b"\n", offset)
        if newline < 0:
            raise Refused("git cat-file ended before every base blob was read")
        header = payload[offset:newline].split(b" ")
        if len(header) != 3 or header[0] != object_id.encode("ascii") or header[1] != b"blob":
            raise Refused(f"unexpected git cat-file output for the base blob {object_id}")
        try:
            size = int(header[2])
        except ValueError:
            raise Refused(f"unexpected git cat-file size for the base blob {object_id}") from None
        start = newline + 1
        end = start + size
        if size < 0 or end >= len(payload) or payload[end] != 0x0A:
            raise Refused(f"git cat-file truncated the base blob {object_id}")
        digests[object_id] = "sha256:" + hashlib.sha256(payload[start:end]).hexdigest()
        offset = end + 1
    if offset != len(payload):
        raise Refused("git cat-file returned more output than the base blobs requested")
    return digests


def config_origin_inside_checkout(checkout: pathlib.Path, origin: str) -> bool:
    """Whether a `git config --show-origin` origin names state inside the checkout.

    Unknown or non-file origins fail closed. Origins outside the checkout are not
    worker-writable through the isolated checkout and never reach this seal anyway,
    because the byte comparison does not consult Git's filtered view.
    """
    if not origin.startswith("file:"):
        return True
    candidate = pathlib.Path(origin[len("file:") :])
    if not candidate.is_absolute():
        candidate = checkout / candidate
    # Normalize lexically instead of resolving: a symlinked config file inside the
    # checkout must stay classified as checkout-local, not escape through resolve().
    root = os.path.normcase(os.path.normpath(str(checkout.absolute())))
    text = os.path.normcase(os.path.normpath(str(candidate.absolute())))
    return text == root or text.startswith(root + os.sep)


def refuse_filter_machinery(checkout: pathlib.Path, bound_paths: list[str]) -> None:
    """Refuse checkout-local attribute or filter machinery outright.

    collect_changes already reads object-store and worktree bytes directly, so no
    filter can influence that comparison; this check exists so no later Git command,
    gate or reader ever has to reason about which of them trusted rewritten content.
    """
    refusal_prefix = "seal refuses a checkout with checkout-local attribute or filter machinery: "

    def run(*arguments: str, stdin: bytes | None = None) -> subprocess.CompletedProcess:
        command = ["git", "-c", f"safe.directory={checkout}", "-C", str(checkout), *arguments]
        try:
            return subprocess.run(command, input=stdin, capture_output=True, check=False)
        except OSError as error:
            raise Refused(f"could not run git {arguments[0]}: {error}") from error

    configured = run("config", "--show-origin", "--get-regexp", r"^filter\.")
    if configured.returncode not in (0, 1):
        detail = configured.stderr.decode("utf-8", "replace").strip()
        raise Refused(f"git config failed ({configured.returncode}): {detail}")
    keys: set[str] = set()
    for line in configured.stdout.decode("utf-8", "replace").splitlines():
        origin, _, entry = line.partition("\t")
        if entry and config_origin_inside_checkout(checkout, origin):
            keys.add(entry.split(" ", 1)[0])
    if keys:
        raise Refused(refusal_prefix + "Git filter configuration [" + ", ".join(sorted(keys)) + "]")

    resolved = run("rev-parse", "--git-path", "info/attributes")
    if resolved.returncode != 0:
        raise Refused(refusal_prefix + "cannot resolve .git/info/attributes")
    attributes = pathlib.Path(resolved.stdout.decode("utf-8", "replace").strip())
    if not attributes.is_absolute():
        attributes = checkout / attributes
    if attributes.exists():
        raise Refused(refusal_prefix + ".git/info/attributes exists")

    attributes_file = run("config", "--get", "core.attributesFile")
    if attributes_file.returncode == 0 and attributes_file.stdout.strip():
        value = attributes_file.stdout.decode("utf-8", "replace").strip()
        raise Refused(refusal_prefix + f"core.attributesFile is set ({value})")
    if attributes_file.returncode not in (0, 1):
        raise Refused(refusal_prefix + f"git config failed ({attributes_file.returncode})")

    request = b"".join(utf8_path_key(path) + b"\0" for path in bound_paths)
    assigned = run("check-attr", "-a", "--stdin", "-z", "--", stdin=request)
    if assigned.returncode != 0:
        raise Refused(refusal_prefix + f"git check-attr failed ({assigned.returncode})")
    tokens = assigned.stdout.split(b"\0")
    rewrites: list[str] = []
    for index in range(0, len(tokens) - 2, 3):
        path_bytes, attribute_bytes, value_bytes = tokens[index], tokens[index + 1], tokens[index + 2]
        if not path_bytes:
            continue
        attribute = attribute_bytes.decode("utf-8", "replace")
        value = value_bytes.decode("utf-8", "replace")
        if attribute in ("filter", "working-tree-encoding") and value not in ("unspecified", "unset"):
            rewrites.append(f"{decode_path(path_bytes)} {attribute}={value}")
    if rewrites:
        raise Refused(refusal_prefix + "attributes assign content rewrites [" + ", ".join(rewrites[:5]) + "]")


def blob_object_id(payload: bytes, object_format: str) -> str:
    hasher = hashlib.new(object_format)
    hasher.update(b"blob " + str(len(payload)).encode("ascii") + b"\0")
    hasher.update(payload)
    return hasher.hexdigest()


def symlink_blob_entry(path: str, target: pathlib.Path, object_format: str) -> tuple[str, str]:
    """Blob (mode, object id) of one worktree symlink, for hidden target-swap checks."""
    try:
        payload = os.fsencode(os.readlink(target))
    except OSError as error:
        raise Refused(f"cannot read symlink {path}: {error}") from error
    return "120000", blob_object_id(payload, object_format)


def verify_base_commit(checkout: pathlib.Path, base: str) -> None:
    refuse_object_substitution(checkout)
    # Replacement refs are disabled for this peel, so the frozen base resolves to the
    # exact commit whose stored tree every later read binds.
    resolved = git_text(str(checkout), "rev-parse", "--verify", base + "^{commit}")
    if resolved != base:
        raise Refused("the frozen base commit is not the resolved checkout base")


def compute_subject(checkout: pathlib.Path, task_id: str, base: str, head: str) -> dict[str, Any]:
    entries = read_worktree_entries(checkout)
    manifest_hash, file_count = manifest_digest(entries)
    if git_text(str(checkout), "rev-parse", "HEAD") != head:
        raise Refused("checkout HEAD changed while binding its subject")
    hasher = hashlib.sha256()
    for field in (
        SUBJECT_NAMESPACE,
        task_id.encode("utf-8"),
        base.encode("ascii"),
        head.encode("ascii"),
        manifest_hash.encode("ascii"),
    ):
        hasher.update(field + b"\0")
    return {
        "base": base,
        "head": head,
        "manifest_digest": manifest_hash,
        "content_revision": "uncommitted:" + manifest_hash,
        "subject_hash": "sha256:" + hasher.hexdigest(),
        "file_count": file_count,
    }


def collect_changes(checkout: pathlib.Path, base: str) -> dict[str, set[str]]:
    refuse_index_hacks(checkout)
    base_tree = read_base_tree(checkout, base)
    entries = read_worktree_entries(checkout)
    changes: dict[str, set[str]] = {}
    for path in entries:
        if path not in base_tree:
            changes.setdefault(path, set()).add("A")
    for path in base_tree:
        if path not in entries:
            changes.setdefault(path, set()).add("D")
    # Content is compared as bytes: the base side comes from the object store and the
    # worktree side was read directly, so no Git filter, attribute, index flag or stat
    # cache can make a changed file look unchanged.
    object_format = git_text(str(checkout), "rev-parse", "--show-object-format")
    if object_format not in ("sha1", "sha256"):
        raise Refused(f"unsupported Git repository object format: {object_format}")
    mode_tracked = executable_bits_tracked(checkout)
    content_objects = {
        base_object
        for path, (base_mode, base_object) in base_tree.items()
        if base_mode != "120000" and path in entries and entries[path][1] == "file"
    }
    base_digests = read_base_blob_digests(checkout, content_objects)
    for path in sorted(base_tree, key=utf8_path_key):
        base_mode, base_object = base_tree[path]
        entry = entries.get(path)
        if entry is None:
            continue  # a missing entry is already reported by the D name-set difference
        worktree_mode, kind, worktree_digest = entry
        if base_mode == "120000" and kind == "symlink":
            target = pathlib.Path(checkout, *path.split("/"))
            if symlink_blob_entry(path, target, object_format) != (base_mode, base_object):
                changes.setdefault(path, set()).add("M")
            continue
        if base_mode != "120000" and kind == "file":
            mode_changed = mode_tracked and worktree_mode != base_mode
            if mode_changed or worktree_digest != base_digests[base_object]:
                changes.setdefault(path, set()).add("M")
            continue
        changes.setdefault(path, set()).add("M")  # file<->symlink type change
    return changes


def validate_changes(
    checkout: pathlib.Path, task: dict[str, Any], base_files: set[str], changes: dict[str, set[str]]
) -> list[dict[str, str]]:
    """Apply the seal's path rules, then refuse checkout-local filter machinery.

    The path rules run first so a hidden edit is refused as the edit it is; the
    machinery check then refuses every filtered or attribute-driven checkout, whether
    or not it currently matches the base, because the whole run must proceed on a
    checkout whose Git view no clean/smudge filter can rewrite.
    """
    allowed = PathMatcher(task["allowed_paths"])
    protected = PathMatcher(task["protected_paths"])
    sealed_changes: list[dict[str, str]] = []
    for path in sorted(changes, key=utf8_path_key):
        status = "".join(sorted(changes[path]))
        baseline = path in base_files
        if is_protected_asset(path) or protected.matches(path):
            raise Refused(f"seal refused a change to the protected asset {path} ({status})")
        if baseline and is_test_path(path):
            raise Refused(f"seal refused a modification or deletion of the baseline test {path} ({status})")
        if not baseline and is_test_path(path) and not allowed.matches(path):
            raise Refused(f"seal refused a new test outside the explicitly scoped allowed_paths: {path}")
        if not allowed.matches(path):
            raise Refused(f"seal refused a changed path outside allowed_paths: {path} ({status})")
        sealed_changes.append({"path": path, "status": status})
    refuse_filter_machinery(checkout, sorted(base_files | set(changes), key=utf8_path_key))
    return sealed_changes


def current_subject(run_dir: pathlib.Path, task: dict[str, Any]) -> dict[str, Any]:
    checkout = run_dir / "checkout"
    verify_base_commit(checkout, task["base"])
    head = require_commit(git_text(str(checkout), "rev-parse", "HEAD"), "checkout HEAD")
    return compute_subject(checkout, task["task_id"], task["base"], head)


def require_current_subject(run_dir: pathlib.Path, task: dict[str, Any], sealed: dict[str, Any]) -> dict[str, Any]:
    subject = current_subject(run_dir, task)
    if any(sealed[field] != value for field, value in subject.items()):
        raise Refused("current source does not match the sealed subject; re-run seal and its checks before continuing")
    checkout = run_dir / "checkout"
    recomputed = validate_changes(
        checkout, task, list_base_paths(checkout, task["base"]), collect_changes(checkout, task["base"])
    )
    if recomputed != sealed["changed_paths"]:
        raise Refused("the sealed changed paths no longer match the checkout changes; re-run seal")
    return subject


def load_source_subject():
    spec = importlib.util.spec_from_file_location("linux_v2_source_subject", SOURCE_SUBJECT_SCRIPT)
    if spec is None or spec.loader is None:
        raise Refused("cannot load linux-v2-source-subject.py for clean-base qualification")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.source_subject
