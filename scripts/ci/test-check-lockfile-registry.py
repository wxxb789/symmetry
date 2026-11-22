#!/usr/bin/env python3
"""Unit tests for the lockfile registry provenance check.

The two lockfile fixtures reproduce the shape pnpm 12 writes for a
lockfileVersion 9 lockfile: a multi-document stream (the `---` split), block
mappings whose keys are plain or single-quoted, one flow collection per line, and
block sequences. Their tarball URLs are the only difference between the canonical
fixture and the mirror fixture, so a test that fails for one of them fails on
registry provenance and nothing else.

The differential test at the end cross-checks the check's own YAML reader against
PyYAML when that module is importable. PyYAML is an extra oracle.
"""

import contextlib
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

import citest

SCRIPT = pathlib.Path(__file__).with_name("check-lockfile-registry.py")
CHECK = citest.load_gate(SCRIPT, "check_lockfile_registry")

CANONICAL = "registry.npmjs.org"
MIRROR = "ms-feed.pkgs.example"

CANONICAL_LOCKFILE = f"""\
---
lockfileVersion: '9.0'

importers:

  .:
    devDependencies:
      left-pad:
        specifier: 1.3.0
        version: 1.3.0
    packageManagerDependencies:
      pnpm:
        specifier: 12.17.0
        version: 12.17.0

packages:

  left-pad@1.3.0:
    resolution: {{integrity: sha512-aaa==, tarball: https://{CANONICAL}/left-pad/-/left-pad-1.3.0.tgz}}
    engines: {{node: '>= 0.10.0'}}

---
lockfileVersion: '9.0'

settings:
  autoInstallPeers: false
  excludeLinksFromLockfile: false

onlyBuiltDependencies:
  - esbuild
  - '@napi-rs/wasm-runtime'

importers:

  .:
    dependencies:
      right-pad:
        specifier: 2.0.0
        version: 2.0.0

packages:

  right-pad@2.0.0:
    resolution: {{integrity: sha512-bbb==, tarball: https://{CANONICAL}/right-pad/-/right-pad-2.0.0.tgz}}
    cpu: [x64]
    os: [linux, darwin]

snapshots:

  right-pad@2.0.0: {{}}
"""

MIRROR_LOCKFILE = f"""\
---
lockfileVersion: '9.0'

packages:

  left-pad@1.3.0:
    resolution: {{integrity: sha512-aaa==, tarball: https://{CANONICAL}/left-pad/-/left-pad-1.3.0.tgz}}

---
lockfileVersion: '9.0'

packages:

  right-pad@2.0.0:
    resolution: {{integrity: sha512-bbb==, tarball: https://{MIRROR}/right-pad/-/right-pad-2.0.0.tgz}}

  '@scope/pkg@3.1.0':
    resolution: {{integrity: sha512-ccc==, tarball: https://{MIRROR}/@scope/pkg/-/pkg-3.1.0.tgz}}

snapshots:

  right-pad@2.0.0: {{}}
"""


# Availability handles: the switch a job sets once PyYAML is pinned, the exact stderr notice a run
# without the module prints, and the phrase the switch-forced failure carries (asserted by tests).
REQUIRE_PYYAML_ENV = "SYMMETRY_REQUIRE_PYYAML"
ABSENCE_NOTICE = "PyYAML differential oracle skipped: module not importable"
SKIP_REASON = "PyYAML is the optional cross-check oracle, not the contract"
REQUIRE_NOTICE = "requires the PyYAML differential oracle"


class RecordedStderr:
    """A process cannot read back its own stderr, so the module-level probe writes through this wrapper."""

    def __init__(self, stream):
        self.text = ""
        self._stream = stream

    def write(self, text):
        self.text += text
        return self._stream.write(text)

    def flush(self):
        self._stream.flush()


def load_optional_pyyaml():

    try:
        import yaml
    except ImportError:
        print(ABSENCE_NOTICE, file=sys.stderr)
        return None
    return yaml


ABSENCE_REPORT = RecordedStderr(sys.stderr)
with contextlib.redirect_stderr(ABSENCE_REPORT):
    PYYAML = load_optional_pyyaml()
REQUIRE_PYYAML = os.environ.get(REQUIRE_PYYAML_ENV) == "1"


def pyyaml_tarballs(text):
    """The same `(entry path, value)` extraction, done by PyYAML."""

    found = []

    def collect(node, trail):
        if trail[-1] == "tarball":
            found.append((".".join(trail), str(node)))
            return
        if isinstance(node, dict):
            for key, value in node.items():
                collect(value, [*trail, str(key)])
        elif isinstance(node, list):
            for index, value in enumerate(node):
                collect(value, [*trail, f"[{index}]"])

    for index, document in enumerate(PYYAML.safe_load_all(text)):
        collect(document, [f"document {index}"])
    return found


class CheckTests(unittest.TestCase):
    def write_lockfile(self, text, name="pnpm-lock.yaml"):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        path = pathlib.Path(directory.name, name)
        path.write_bytes(text.encode("utf-8"))
        return path

    def assert_failed_closed(self, result, reason):
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("cannot verify lockfile registry provenance", result.stderr)
        self.assertIn(reason, result.stderr)
        self.assertEqual(result.stdout, "")

    def test_canonical_registry_fixture_passes(self):
        path = self.write_lockfile(CANONICAL_LOCKFILE)
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, "")
        self.assertEqual(
            result.stdout,
            f"verified every recorded tarball URL in {path} sits on {CANONICAL}: 2 entries checked\n",
        )

    def test_mirror_host_fixture_fails_and_names_every_offending_entry(self):
        path = self.write_lockfile(MIRROR_LOCKFILE)
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertEqual(result.stdout, "")
        self.assertIn(f"2 of 3 recorded tarball URL(s) in {path} are not on {CANONICAL}", result.stderr)
        self.assertIn("document 1.packages.right-pad@2.0.0.resolution.tarball", result.stderr)
        self.assertIn(f"https://{MIRROR}/right-pad/-/right-pad-2.0.0.tgz", result.stderr)
        self.assertIn("document 1.packages.@scope/pkg@3.1.0.resolution.tarball", result.stderr)
        self.assertIn(f"https://{MIRROR}/@scope/pkg/-/pkg-3.1.0.tgz", result.stderr)
        self.assertNotIn(f"https://{CANONICAL}/left-pad/-/left-pad-1.3.0.tgz", result.stderr)

    def test_missing_lockfile_fails_closed(self):
        missing = pathlib.Path(tempfile.gettempdir(), "absent-pnpm-lock.yaml")
        missing.unlink(missing_ok=True)
        result = citest.run_gate(SCRIPT, "--lockfile", str(missing))
        self.assert_failed_closed(result, "cannot read")

    def test_empty_lockfile_fails_closed(self):
        path = self.write_lockfile("")
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assert_failed_closed(result, "is empty")

    def test_lockfile_without_any_tarball_entry_fails_closed(self):
        path = self.write_lockfile(
            "---\nlockfileVersion: '9.0'\n\npackages:\n\n  left-pad@1.3.0:\n    resolution: {integrity: sha512-aaa==}\n"
        )
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assert_failed_closed(result, "records no tarball URL")

    def test_lockfile_without_lockfile_version_fails_closed(self):
        path = self.write_lockfile(
            f"packages:\n\n  left-pad@1.3.0:\n    resolution: "
            f"{{tarball: https://{CANONICAL}/left-pad/-/left-pad-1.3.0.tgz}}\n"
        )
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assert_failed_closed(result, "carries no lockfileVersion")

    def test_non_mapping_document_fails_closed(self):
        path = self.write_lockfile("---\n- left-pad\n")
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assert_failed_closed(result, "document is not a mapping")

    def test_tab_indentation_fails_closed(self):
        path = self.write_lockfile("lockfileVersion: '9.0'\n\tpackages: {}\n")
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assert_failed_closed(result, "tab used for indentation")

    def test_anchor_fails_closed(self):
        path = self.write_lockfile("lockfileVersion: '9.0'\n\npackages:\n\n  left-pad@1.3.0: &left-pad\n")
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assert_failed_closed(result, "unquoted scalar starts with a YAML indicator")

    def test_block_scalar_fails_closed(self):
        path = self.write_lockfile("lockfileVersion: '9.0'\n\npackages:\n\n  left-pad@1.3.0:\n    resolution: |\n")
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assert_failed_closed(result, "block scalars are outside the lockfile subset")

    def test_duplicated_key_fails_closed(self):
        path = self.write_lockfile(
            "lockfileVersion: '9.0'\n\npackages:\n\n  left-pad@1.3.0:\n"
            "    resolution: {}\n  left-pad@1.3.0:\n    resolution: {}\n"
        )
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assert_failed_closed(result, "repeats inside one mapping")

    def test_unclosed_flow_collection_fails_closed(self):
        path = self.write_lockfile(
            "lockfileVersion: '9.0'\n\npackages:\n\n  left-pad@1.3.0:\n    resolution: {integrity: sha512-aaa==\n"
        )
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assert_failed_closed(result, "flow mapping does not close on its line")

    def test_line_without_key_separator_fails_closed(self):
        path = self.write_lockfile("lockfileVersion: '9.0'\n\npackages:\n  left-pad\n")
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assert_failed_closed(result, "carries no `key: value` separator")

    def test_non_https_tarball_fails(self):
        path = self.write_lockfile(
            "lockfileVersion: '9.0'\n\npackages:\n\n  left-pad@1.3.0:\n"
            f"    resolution: {{tarball: http://{CANONICAL}/left-pad/-/left-pad-1.3.0.tgz}}\n"
        )
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("is not https", result.stderr)

    def test_tarball_on_the_canonical_host_with_another_port_fails(self):
        path = self.write_lockfile(
            "lockfileVersion: '9.0'\n\npackages:\n\n  left-pad@1.3.0:\n"
            f"    resolution: {{tarball: https://{CANONICAL}:8443/left-pad/-/left-pad-1.3.0.tgz}}\n"
        )
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn(f"{CANONICAL}:8443", result.stderr)

    def test_tarball_without_a_host_fails(self):
        path = self.write_lockfile(
            "lockfileVersion: '9.0'\n\npackages:\n\n  left-pad@1.3.0:\n"
            "    resolution: {tarball: https:///left-pad-1.3.0.tgz}\n"
        )
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("<none>", result.stderr)

    def test_tarball_that_is_a_collection_fails(self):
        path = self.write_lockfile(
            "lockfileVersion: '9.0'\n\npackages:\n\n  left-pad@1.3.0:\n"
            "    resolution: {tarball: {integrity: sha512-aaa==}}\n"
        )
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("is a collection, not a URL", result.stderr)

    def test_empty_tarball_fails(self):
        path = self.write_lockfile(
            "lockfileVersion: '9.0'\n\npackages:\n\n  left-pad@1.3.0:\n    resolution: {tarball: ''}\n"
        )
        result = citest.run_gate(SCRIPT, "--lockfile", str(path))
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("recorded tarball is empty", result.stderr)

    def test_repository_lockfile_passes_the_default_invocation(self):
        result = citest.run_gate(SCRIPT)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f"sits on {CANONICAL}:", result.stdout)
        self.assertIn("entries checked\n", result.stdout)


class ReaderTests(unittest.TestCase):
    def test_reader_parses_the_pnpm_lockfile_shape(self):
        documents = CHECK.parse_documents(CANONICAL_LOCKFILE)
        self.assertEqual(len(documents), 2)
        first, second = documents
        self.assertEqual(
            first.entries["lockfileVersion"].value,
            "9.0",
        )
        resolution = first.entries["packages"].entries["left-pad@1.3.0"].entries["resolution"]
        self.assertEqual(resolution.entries["integrity"].value, "sha512-aaa==")
        self.assertEqual(
            resolution.entries["tarball"].value,
            f"https://{CANONICAL}/left-pad/-/left-pad-1.3.0.tgz",
        )
        self.assertEqual(
            second.entries["onlyBuiltDependencies"].items[0].value,
            "esbuild",
        )
        self.assertEqual(
            second.entries["onlyBuiltDependencies"].items[1].value,
            "@napi-rs/wasm-runtime",
        )
        self.assertEqual(
            [item.value for item in second.entries["packages"].entries["right-pad@2.0.0"].entries["os"].items],
            ["linux", "darwin"],
        )

    def test_reader_returns_recorded_tarballs_in_document_order(self):
        self.assertEqual(
            [(path, node.value) for path, node in CHECK.recorded_tarballs(CHECK.parse_documents(CANONICAL_LOCKFILE))],
            [
                (
                    "document 0.packages.left-pad@1.3.0.resolution.tarball",
                    f"https://{CANONICAL}/left-pad/-/left-pad-1.3.0.tgz",
                ),
                (
                    "document 1.packages.right-pad@2.0.0.resolution.tarball",
                    f"https://{CANONICAL}/right-pad/-/right-pad-2.0.0.tgz",
                ),
            ],
        )

    def test_reader_accepts_the_repository_lockfile(self):
        documents = CHECK.parse_documents(CHECK.DEFAULT_LOCKFILE.read_text(encoding="utf-8"))
        self.assertGreaterEqual(len(documents), 2)
        self.assertGreater(len(CHECK.recorded_tarballs(documents)), 0)


class OracleVisibilityTests(unittest.TestCase):
    def test_the_absence_of_the_differential_oracle_is_reported(self):

        if PYYAML is None:
            self.assertEqual(ABSENCE_REPORT.text, f"{ABSENCE_NOTICE}\n")
        else:
            self.assertNotIn(ABSENCE_NOTICE, ABSENCE_REPORT.text)

    def test_the_require_switch_turns_absence_into_a_failure(self):
        """`-S` hides site-packages, so the switched subprocess runs without PyYAML."""
        result = subprocess.run(
            [sys.executable, "-S", __file__, "DifferentialTests"],
            capture_output=True,
            text=True,
            check=False,
            env={**os.environ, REQUIRE_PYYAML_ENV: "1"},
        )
        self.assertNotEqual(result.returncode, 0, result.stderr)
        self.assertIn(REQUIRE_NOTICE, result.stderr)


@unittest.skipUnless(PYYAML is not None or REQUIRE_PYYAML, SKIP_REASON)
class DifferentialTests(unittest.TestCase):
    def test_reader_and_pyyaml_extract_the_same_entries(self):
        self.assertIsNotNone(
            PYYAML,
            f"{REQUIRE_PYYAML_ENV}=1 {REQUIRE_NOTICE}, and it is not importable",
        )
        fixtures = {
            "canonical": CANONICAL_LOCKFILE,
            "mirror": MIRROR_LOCKFILE,
            "repository": CHECK.DEFAULT_LOCKFILE.read_text(encoding="utf-8"),
        }
        for name, text in fixtures.items():
            with self.subTest(fixture=name):
                self.assertEqual(
                    [(path, node.value) for path, node in CHECK.recorded_tarballs(CHECK.parse_documents(text))],
                    pyyaml_tarballs(text),
                )


if __name__ == "__main__":
    unittest.main()
