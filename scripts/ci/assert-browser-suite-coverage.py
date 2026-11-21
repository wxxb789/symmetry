#!/usr/bin/env python3
"""Require every browser suite to be wired into a workflow or listed as manual.

Findings T4 and F4: thirteen Playwright specs and three node test files live
under `browser/`, and six of the specs plus all three node tests are reachable
only through local driver scripts - roughly 4,600 lines of test code with no CI
verdict and no explicit label. This gate makes the classification total:

* every `<root>/browser/tests/**/*.spec.mjs` and
  `<root>/browser/scripts/**/*.test.mjs` must be **wired** - some
  `.github/workflows/*.yml`/`*.yaml` names the file's
  browser-relative (`tests/foo.spec.mjs`) or repository-relative
  (`browser/tests/foo.spec.mjs`) path as a token - or **manual** - named by a
  backticked path in the suite column of a table row of the
  `## Manual-only suite matrix` section of `browser/README.md`. Only the first
  cell of a table row in that one section counts, so a row's explanation may
  mention sibling suites freely; the README's prose throughout is not consulted;
* a file in both classes, an unclassified file, and a matrix entry with no file
  on disk are each a failure, so wiring a spec (refactor slice W6-02) must also
  drop its matrix row, and a rename cannot leave a stale label behind;
* the classification table and counts print on every run.

For a wired file this gate proves it is *named*, not that the naming step runs
it: for the render family, `assert-playwright-json.py`'s executed-set assertion
is the execution proof and is deliberately not duplicated here. `--root`
overrides the repository root so the gate itself can be exercised against
synthetic fixture trees.
"""

import argparse
import pathlib
import re
import sys

# Browser-relative suite patterns. SUITE_PATTERNS (`**` crosses directories, so
# a nested family is discovered like a flat one) and SUITE_SHAPE (the
# manual-matrix shape, a regex independently written over the same families and
# file names at any depth) are two spellings of one constraint, so a new family
# belongs in both; an entry the shape does not match is skipped by the shape
# check in main rather than rot-checked.
SUITE_PATTERNS = ("tests/**/*.spec.mjs", "scripts/**/*.test.mjs")
SUITE_SHAPE = re.compile(r"^(?:browser/)?(?:tests/(?:[^/]+/)*[^/]+\.spec\.mjs|scripts/(?:[^/]+/)*[^/]+\.test\.mjs)$")
WORKFLOW_PATTERNS = (".github/workflows/*.yml", ".github/workflows/*.yaml")
MATRIX_HEADING = "## Manual-only suite matrix"
README = "browser/README.md"
PATH_TOKEN = re.compile(r"[\w./+-]+")
BACKTICKED = re.compile(r"`([^`]+)`")


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--root",
        metavar="DIR",
        help="repository root to check (default: the root this script lives under)",
    )
    return parser.parse_args(argv)


def repository_root(args: argparse.Namespace) -> pathlib.Path:
    if args.root is None:
        return pathlib.Path(__file__).resolve().parents[2]
    root = pathlib.Path(args.root).resolve()
    if not root.is_dir():
        raise SystemExit(f"assert-browser-suite-coverage.py: --root {args.root!r} is not a directory")
    return root


def discover_suites(root: pathlib.Path) -> list[str]:
    """The repository-relative paths of every browser suite file."""
    found = set()
    for pattern in SUITE_PATTERNS:
        for path in root.joinpath("browser").glob(pattern):
            if path.is_file():
                found.add(path.relative_to(root).as_posix())
    return sorted(found)


def names_suite(token: str, suite: str) -> bool:
    """Whether a workflow token names the suite file.

    Accepts the repository-relative path (`browser/tests/foo.spec.mjs`), the
    browser-relative path (`tests/foo.spec.mjs`, the render step's spelling) and
    either of those at the end of a longer token (`/tmp/x/browser/tests/...`).
    """
    browser_relative = suite[len("browser/") :]
    return any(token == candidate or token.endswith("/" + candidate) for candidate in (suite, browser_relative))


def workflow_tokens(root: pathlib.Path) -> dict[str, list[str]]:
    """Every path-like token of every workflow file, keyed by workflow path."""
    tokens: dict[str, list[str]] = {}
    for pattern in WORKFLOW_PATTERNS:
        for path in sorted(root.joinpath(".github/workflows").glob(pathlib.PurePath(pattern).name)):
            if not path.is_file():
                continue
            tokens[path.relative_to(root).as_posix()] = PATH_TOKEN.findall(path.read_text(encoding="utf-8"))
    return tokens


def wired_by(suite: str, tokens: dict[str, list[str]]) -> list[str]:
    """The sorted workflow paths whose tokens name the suite."""
    return sorted(source for source, values in tokens.items() if any(names_suite(token, suite) for token in values))


def manual_entries(readme_path: pathlib.Path) -> list[str]:
    """The backticked tokens of the manual matrix's suite column.

    Only the section's rows count - the heading opens it and the next level-2
    heading closes it - and only a row's first cell is read, so the reason and
    driver columns can name other files without labelling them.
    """
    entries: list[str] = []
    in_section = False
    seen_heading = False
    for line in readme_path.read_text(encoding="utf-8").splitlines():
        if line.strip() == MATRIX_HEADING:
            in_section = True
            seen_heading = True
            continue
        if not in_section:
            continue
        if line.startswith("## "):
            break
        if line.lstrip().startswith("|"):
            suite_cell = line.split("|")[1]
            entries.extend(span.strip() for span in BACKTICKED.findall(suite_cell))
    if not seen_heading:
        raise SystemExit(f"assert-browser-suite-coverage.py: {readme_path} carries no `{MATRIX_HEADING}` section")
    return entries


def normalize_entry(entry: str) -> str:
    return entry if entry.startswith("browser/") else "browser/" + entry


def main(argv: list[str] | None = None) -> int:
    args = parse_args(list(sys.argv[1:] if argv is None else argv))
    root = repository_root(args)

    suites = discover_suites(root)
    if not suites:
        print(f"assert-browser-suite-coverage.py: found no browser suite files under {root}/browser", file=sys.stderr)
        return 2

    tokens = workflow_tokens(root)
    if not tokens:
        print(
            f"assert-browser-suite-coverage.py: found no workflow files under {root}/.github/workflows", file=sys.stderr
        )
        return 2

    manual: set[str] = set()
    unknown_entries: list[str] = []
    for entry in manual_entries(root / README):
        if SUITE_SHAPE.fullmatch(entry) is None:
            continue
        normalized = normalize_entry(entry)
        if normalized in suites:
            manual.add(normalized)
        else:
            unknown_entries.append(entry)

    wired = {suite: wired_by(suite, tokens) for suite in suites}
    wired_set = {suite for suite, sources in wired.items() if sources}
    unclassified = [suite for suite in suites if suite not in wired_set and suite not in manual]
    overlap = sorted(wired_set & manual)

    print(f"Browser suite inventory (root={root}):")
    for suite in suites:
        if wired[suite]:
            print(f"  wired   {suite}  (named by {', '.join(wired[suite])})")
        elif suite in manual:
            print(f"  manual  {suite}  ({README} manual matrix)")
        else:
            print(f"  UNCLASSIFIED  {suite}")
    print(
        f"browser_suite_coverage: suites={len(suites)} wired={len(wired_set)} manual={len(manual)} "
        f"unclassified={len(unclassified)}"
    )

    verdict = 0
    for suite in unclassified:
        print(
            f"browser_suite_coverage: FAIL: {suite} is neither named by a workflow nor listed in the "
            f"{README} manual matrix",
        )
        verdict = 1
    for entry in sorted(unknown_entries):
        print(f"browser_suite_coverage: FAIL: the {README} manual matrix lists `{entry}`, which does not exist")
        verdict = 1
    for suite in overlap:
        print(
            f"browser_suite_coverage: FAIL: {suite} is both named by {', '.join(wired[suite])} and listed in the "
            f"{README} manual matrix",
        )
        verdict = 1
    if verdict == 0:
        print("browser_suite_coverage: PASS: every browser suite is wired or explicitly manual")
    return verdict


if __name__ == "__main__":
    raise SystemExit(main())
