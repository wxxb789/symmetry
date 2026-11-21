#!/usr/bin/env python3
"""Fail when pnpm-lock.yaml records tarball URLs outside the canonical registry.

Why this exists
---------------
`pnpm install --frozen-lockfile` fetches every package from the tarball URL the
lockfile records and rejects the lockfile (ERR_PNPM_TARBALL_URL_MISMATCH) when a
recorded URL does not belong to the registry pnpm is configured with. A lockfile
regenerated while a host-level `~/.npmrc` pointed at a package-feed mirror
therefore passed every local run and failed only in CI, inside the install the
supply chain depends on (run 44050937524, `contracts` and `compose` jobs). This
check reads the lockfile before anything installs and fails on the offending
entries instead.

How the lockfile is read
------------------------
The lockfile is parsed as a YAML stream, never scanned as text. `parse_documents`
builds mappings, sequences and scalars out of the document structure: document
separators, indentation-scoped block mappings and block sequences, flow mappings
and flow sequences, single-quoted and plain scalars. Every `tarball` entry is then
read out of that tree, and each value is split with `urllib.parse.urlsplit`, so the
host compared is the URL's own host component. No regular expression and no
substring search decides what a tarball URL is or where it points.

The parser accepts the closed subset of YAML that pnpm emits for a lockfileVersion
9 lockfile: a stream of documents, block mappings, block sequences, one flow
collection per line, single-quoted and plain scalars, no comments. Everything
outside that subset — a directive, an anchor or an alias, a tag, a block scalar, a
tab-indented line, a flow collection that does not close on its line, a key that
repeats inside one mapping, a line that carries no `key: value` separator — raises
`UnusableLockfile`. This check never guesses about a lockfile it cannot parse.

Fail-closed contract
--------------------
Exit 2, the check cannot testify about this lockfile at all: the lockfile is
missing, unreadable, empty, outside the parseable subset, its document is not a
mapping, it carries no `lockfileVersion`, or it records zero `tarball` entries.

Exit 1, the lockfile violates the policy: a recorded tarball URL is not on the
canonical registry host, or is not a non-empty string, or carries no host, or is
not `https`. Every offending entry is printed as `<entry path>: <reason>`.

Exit 0: every recorded tarball URL sits on the canonical registry host; the number
of checked entries is printed.

Deliberately not covered: integrity hashes, and entries that record no tarball at
all (those resolve through whatever registry the install is configured with). See
docs/TODO.md.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path
from urllib.parse import urlsplit

CANONICAL_REGISTRY_HOST = "registry.npmjs.org"
DEFAULT_LOCKFILE = Path(__file__).resolve().parents[2] / "pnpm-lock.yaml"

# A plain (unquoted) YAML scalar may not start with one of these; the lockfile
# quotes such values, and an unquoted one would be an anchor, an alias, a tag, a
# directive or a block scalar rather than the scalar it looks like.
PLAIN_SCALAR_RESERVED_STARTS = ("&", "*", "!", "%", "@", "`", "|", ">")


class UnusableLockfile(Exception):
    """The lockfile cannot be read as the closed YAML subset pnpm emits."""


class Mapping:
    """A YAML mapping; entries keep document order and reject repeated keys."""

    def __init__(self, line_number: int) -> None:
        self.line_number = line_number
        self.entries: dict[str, object] = {}

    def add(self, key: str, value: object, line_number: int) -> None:
        if key in self.entries:
            raise UnusableLockfile(f"line {line_number}: key {key!r} repeats inside one mapping")
        self.entries[key] = value


class Sequence:
    """A YAML sequence."""

    def __init__(self, line_number: int) -> None:
        self.line_number = line_number
        self.items: list[object] = []


class Scalar:
    """A YAML scalar, kept as the text the document carried."""

    def __init__(self, value: str) -> None:
        self.value = value


class _Reader:
    """Position in the content lines of one document."""

    def __init__(self, lines: list[tuple[int, str]]) -> None:
        self.lines = lines
        self.index = 0

    def peek(self) -> tuple[int, str] | None:
        return self.lines[self.index] if self.index < len(self.lines) else None

    def take(self) -> tuple[int, str]:
        line = self.lines[self.index]
        self.index += 1
        return line


def _indent(text: str) -> int:
    return len(text) - len(text.lstrip(" "))


def _strip_trailing_comment(text: str) -> str:
    quote = ""
    for position, character in enumerate(text):
        if quote:
            if character == quote:
                quote = ""
            continue
        if character in "'\"":
            quote = character
        elif character == "#" and (position == 0 or text[position - 1] == " "):
            return text[:position]
    return text


def _body(text: str) -> str:
    """The line's content without its indentation."""

    return _strip_trailing_comment(text).lstrip(" ").rstrip()


def _is_sequence_entry(content: str) -> bool:
    return content == "-" or content.startswith("- ")


def _split_documents(text: str) -> list[list[tuple[int, str]]]:
    documents: list[list[tuple[int, str]]] = [[]]
    for number, raw in enumerate(text.split("\n"), start=1):
        line = raw.rstrip("\r")
        indentation = line[: len(line) - len(line.lstrip(" \t"))]
        if "\t" in indentation:
            raise UnusableLockfile(f"line {number}: tab used for indentation")
        content = _body(line)
        if content.startswith("---") or content.startswith("..."):
            if content not in ("---", "..."):
                raise UnusableLockfile(f"line {number}: document marker carries inline content: {content!r}")
            documents.append([])
            continue
        if content.startswith("%"):
            raise UnusableLockfile(f"line {number}: YAML directives are not supported")
        documents[-1].append((number, line))
    return documents


def parse_documents(text: str) -> list[Mapping]:
    """Parse a YAML stream into one mapping per document."""

    documents: list[Mapping] = []
    for lines in _split_documents(text):
        content_lines = [(number, line) for number, line in lines if _body(line)]
        if not content_lines:
            continue
        reader = _Reader(content_lines)
        node = _parse_block(reader, _indent(content_lines[0][1]))
        if reader.peek() is not None:
            number, line = reader.peek()
            raise UnusableLockfile(f"line {number}: trailing content: {line.strip()!r}")
        if not isinstance(node, Mapping):
            raise UnusableLockfile(f"line {node.line_number}: document is not a mapping")
        documents.append(node)
    if not documents:
        raise UnusableLockfile("the lockfile carries no YAML document")
    return documents


def _parse_block(reader: _Reader, indent: int) -> object:
    line = reader.peek()
    assert line is not None
    if _is_sequence_entry(_body(line[1])):
        return _parse_sequence(reader, indent)
    return _parse_mapping(reader, indent)


def _parse_mapping(reader: _Reader, indent: int) -> Mapping:
    mapping = Mapping(reader.peek()[0] if reader.peek() else 0)
    while True:
        line = reader.peek()
        if line is None:
            return mapping
        number, text = line
        if _indent(text) < indent:
            return mapping
        if _indent(text) > indent:
            raise UnusableLockfile(
                f"line {number}: unexpected indentation {_indent(text)} inside a mapping at indentation {indent}"
            )
        content = _body(text)
        if _is_sequence_entry(content):
            raise UnusableLockfile(f"line {number}: sequence entry inside a mapping: {content!r}")
        key, value_text = _split_key(content, number)
        reader.take()
        mapping.add(key, _parse_value(reader, indent, value_text, number), number)


def _parse_value(reader: _Reader, indent: int, value_text: str, number: int) -> object:
    if value_text:
        return _parse_inline_value(value_text, number)
    following = reader.peek()
    if following is None:
        return Scalar("")
    if _indent(following[1]) > indent:
        return _parse_block(reader, _indent(following[1]))
    if _indent(following[1]) == indent and _is_sequence_entry(_body(following[1])):
        return _parse_sequence(reader, indent)
    return Scalar("")


def _parse_sequence(reader: _Reader, indent: int) -> Sequence:
    sequence = Sequence(reader.peek()[0] if reader.peek() else 0)
    while True:
        line = reader.peek()
        if line is None:
            return sequence
        number, text = line
        if _indent(text) != indent:
            return sequence
        content = _body(text)
        if not _is_sequence_entry(content):
            return sequence
        item_text = content[1:].strip()
        reader.take()
        if not item_text:
            following = reader.peek()
            if following is None or _indent(following[1]) <= indent:
                raise UnusableLockfile(f"line {number}: sequence entry carries no value")
            sequence.items.append(_parse_block(reader, _indent(following[1])))
            continue
        if not item_text.startswith(("'", "{", "[")) and (": " in item_text or item_text.endswith(":")):
            raise UnusableLockfile(
                f"line {number}: mappings inline in a sequence entry are outside the lockfile subset: {item_text!r}"
            )
        sequence.items.append(_parse_inline_value(item_text, number))


def _split_key(content: str, number: int) -> tuple[str, str]:
    if content.startswith("'"):
        key, remainder = _parse_single_quoted(content, number)
        if not remainder.startswith(":"):
            raise UnusableLockfile(f"line {number}: quoted key without a separator")
        return key, remainder[1:].strip()
    for position, character in enumerate(content):
        if character == ":" and (position + 1 == len(content) or content[position + 1] == " "):
            key = content[:position].strip()
            if not key:
                raise UnusableLockfile(f"line {number}: empty mapping key")
            _reject_unquoted_marker(key, number)
            return key, content[position + 1 :].strip()
    raise UnusableLockfile(f"line {number}: line carries no `key: value` separator: {content!r}")


def _parse_single_quoted(text: str, number: int) -> tuple[str, str]:
    value: list[str] = []
    position = 1
    while position < len(text):
        character = text[position]
        if character == "'":
            if position + 1 < len(text) and text[position + 1] == "'":
                value.append("'")
                position += 2
                continue
            return "".join(value), text[position + 1 :].strip()
        value.append(character)
        position += 1
    raise UnusableLockfile(f"line {number}: single-quoted scalar is not closed")


def _reject_unquoted_marker(text: str, number: int) -> None:
    if text.startswith(PLAIN_SCALAR_RESERVED_STARTS):
        raise UnusableLockfile(f"line {number}: unquoted scalar starts with a YAML indicator: {text!r}")


def _parse_inline_value(text: str, number: int) -> object:
    value = text.strip()
    if not value:
        raise UnusableLockfile(f"line {number}: value is empty after the separator")
    if value.startswith("{"):
        node, remainder = _parse_flow_mapping(value, number)
        _reject_trailing(remainder, number)
        return node
    if value.startswith("["):
        node, remainder = _parse_flow_sequence(value, number)
        _reject_trailing(remainder, number)
        return node
    if value.startswith(("|", ">")):
        raise UnusableLockfile(f"line {number}: block scalars are outside the lockfile subset")
    if value.startswith("'"):
        scalar, remainder = _parse_single_quoted(value, number)
        _reject_trailing(remainder, number)
        return Scalar(scalar)
    if ": " in value or value.endswith(":"):
        raise UnusableLockfile(f"line {number}: unquoted scalar is ambiguous with a mapping: {value!r}")
    _reject_unquoted_marker(value, number)
    return Scalar(value)


def _reject_trailing(remainder: str, number: int) -> None:
    if remainder.strip():
        raise UnusableLockfile(f"line {number}: unexpected content after a value: {remainder.strip()!r}")


def _skip_spaces(text: str, position: int) -> int:
    while position < len(text) and text[position] == " ":
        position += 1
    return position


def _parse_flow_mapping(text: str, number: int) -> tuple[Mapping, str]:
    mapping = Mapping(number)
    position = _skip_spaces(text, 1)
    if position < len(text) and text[position] == "}":
        return mapping, text[position + 1 :]
    while True:
        if position >= len(text):
            raise UnusableLockfile(f"line {number}: flow mapping does not close on its line")
        if text[position] == "'":
            key, remainder = _parse_single_quoted(text[position:], number)
            position = len(text) - len(remainder)
        else:
            separator = None
            for candidate in range(position, len(text)):
                if text[candidate] == ":" and (candidate + 1 == len(text) or text[candidate + 1] in " ,}"):
                    separator = candidate
                    break
            if separator is None:
                raise UnusableLockfile(
                    f"line {number}: flow mapping entry carries no key separator: {text[position:]!r}"
                )
            key = text[position:separator].strip()
            position = separator
            if not key:
                raise UnusableLockfile(f"line {number}: empty flow mapping key")
            _reject_unquoted_marker(key, number)
        if position >= len(text) or text[position] != ":":
            raise UnusableLockfile(f"line {number}: flow mapping key is not followed by its separator")
        position = _skip_spaces(text, position + 1)
        value, position = _parse_flow_value(text, position, number)
        mapping.add(key, value, number)
        position = _skip_spaces(text, position)
        if position >= len(text):
            raise UnusableLockfile(f"line {number}: flow mapping does not close on its line")
        if text[position] == ",":
            position = _skip_spaces(text, position + 1)
            if position < len(text) and text[position] == "}":
                return mapping, text[position + 1 :]
            continue
        if text[position] == "}":
            return mapping, text[position + 1 :]
        raise UnusableLockfile(f"line {number}: flow mapping entry is not followed by `,` or `}}`: {text[position:]!r}")


def _parse_flow_sequence(text: str, number: int) -> tuple[Sequence, str]:
    sequence = Sequence(number)
    position = _skip_spaces(text, 1)
    if position < len(text) and text[position] == "]":
        return sequence, text[position + 1 :]
    while True:
        value, position = _parse_flow_value(text, position, number)
        sequence.items.append(value)
        position = _skip_spaces(text, position)
        if position >= len(text):
            raise UnusableLockfile(f"line {number}: flow sequence does not close on its line")
        if text[position] == ",":
            position = _skip_spaces(text, position + 1)
            if position < len(text) and text[position] == "]":
                return sequence, text[position + 1 :]
            continue
        if text[position] == "]":
            return sequence, text[position + 1 :]
        raise UnusableLockfile(f"line {number}: flow sequence entry is not followed by `,` or `]`: {text[position:]!r}")


def _parse_flow_value(text: str, position: int, number: int) -> tuple[object, int]:
    if position >= len(text):
        raise UnusableLockfile(f"line {number}: flow collection entry carries no value")
    character = text[position]
    if character == "{":
        node, remainder = _parse_flow_mapping(text[position:], number)
        return node, len(text) - len(remainder)
    if character == "[":
        node, remainder = _parse_flow_sequence(text[position:], number)
        return node, len(text) - len(remainder)
    if character == "'":
        value, remainder = _parse_single_quoted(text[position:], number)
        return Scalar(value), len(text) - len(remainder)
    stop = len(text)
    for candidate in range(position, len(text)):
        if text[candidate] in ",]}":
            stop = candidate
            break
    value = text[position:stop].strip()
    if not value:
        raise UnusableLockfile(f"line {number}: flow collection entry carries no value")
    if value.startswith(PLAIN_SCALAR_RESERVED_STARTS):
        raise UnusableLockfile(f"line {number}: unquoted scalar starts with a YAML indicator: {value!r}")
    if ": " in value:
        raise UnusableLockfile(f"line {number}: unquoted flow scalar is ambiguous with a mapping: {value!r}")
    return Scalar(value), stop


def recorded_tarballs(documents: list[Mapping]) -> list[tuple[str, object]]:
    """Return `(entry path, node)` for every `tarball` key, in document order."""

    found: list[tuple[str, object]] = []
    for index, document in enumerate(documents):
        _collect_tarballs(document, [f"document {index}"], found)
    return found


def load_lockfile(path: Path) -> list[Mapping]:
    """Read and structurally validate the lockfile, or raise `UnusableLockfile`."""

    try:
        text = path.read_text(encoding="utf-8")
    except OSError as error:
        raise UnusableLockfile(f"cannot read {path}: {error}") from error
    except UnicodeDecodeError as error:
        raise UnusableLockfile(f"{path} is not UTF-8 text: {error}") from error
    if not text.strip():
        raise UnusableLockfile(f"{path} is empty")
    documents = parse_documents(text)
    if not any(
        isinstance(document.entries.get("lockfileVersion"), Scalar)
        and document.entries["lockfileVersion"].value.strip()
        for document in documents
    ):
        raise UnusableLockfile(f"{path} carries no lockfileVersion, so it is not a pnpm lockfile")
    return documents


def classify(entry_path: str, node: object, canonical_host: str = CANONICAL_REGISTRY_HOST) -> str | None:
    """Return why this recorded tarball URL is off-registry, or `None` when it is not."""

    if not isinstance(node, Scalar):
        return f"{entry_path}: recorded tarball is a collection, not a URL"
    value = node.value.strip()
    if not value:
        return f"{entry_path}: recorded tarball is empty"
    try:
        parts = urlsplit(value)
    except ValueError as error:
        return f"{entry_path}: recorded tarball does not parse as a URL: {value} ({error})"
    if parts.scheme != "https":
        return f"{entry_path}: recorded tarball is not https: {value}"
    if parts.hostname != canonical_host or parts.port is not None:
        host = parts.hostname or "<none>"
        port = f":{parts.port}" if parts.port is not None else ""
        return f"{entry_path}: recorded tarball host {host}{port} is not {canonical_host}: {value}"
    return None


def _collect_tarballs(node: object, trail: list[str], found: list[tuple[str, object]]) -> None:
    if trail[-1] == "tarball":
        found.append((".".join(trail), node))
        return
    if isinstance(node, Mapping):
        for key, value in node.entries.items():
            _collect_tarballs(value, [*trail, key], found)
    elif isinstance(node, Sequence):
        for index, item in enumerate(node.items):
            _collect_tarballs(item, [*trail, f"[{index}]"], found)


def _parse_arguments(argv: list[str] | None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--lockfile",
        type=Path,
        default=DEFAULT_LOCKFILE,
        help="pnpm lockfile to verify (default: the repository's pnpm-lock.yaml)",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = _parse_arguments(argv)
    try:
        entries = recorded_tarballs(load_lockfile(args.lockfile))
    except UnusableLockfile as error:
        print(f"cannot verify lockfile registry provenance: {error}", file=sys.stderr)
        return 2
    if not entries:
        print(
            f"cannot verify lockfile registry provenance: {args.lockfile} records no "
            "tarball URL, so this check cannot testify about its registry provenance",
            file=sys.stderr,
        )
        return 2
    problems = [
        problem for problem in (classify(entry_path, node) for entry_path, node in entries) if problem is not None
    ]
    if problems:
        print(
            f"{len(problems)} of {len(entries)} recorded tarball URL(s) in "
            f"{args.lockfile} are not on {CANONICAL_REGISTRY_HOST}:",
            file=sys.stderr,
        )
        for problem in problems:
            print(f"  {problem}", file=sys.stderr)
        return 1
    print(
        f"verified every recorded tarball URL in {args.lockfile} sits on "
        f"{CANONICAL_REGISTRY_HOST}: {len(entries)} entries checked"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
