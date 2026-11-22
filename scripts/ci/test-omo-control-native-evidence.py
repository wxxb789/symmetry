#!/usr/bin/env python3
"""Reject hostile registration paths and decoy build identity in the witness.

`omo-control-native-evidence.py` used to accept a machine/session path made of
`[0-9a-f-]+` (so `/api/v1/machines/----/sessions/-` passed), and it read the Go
build revision by first-match byte search over the image (so a decoy
`vcs.revision=` blob earlier in the binary won). These tests pin the structural
replacements: UUID-validated path segments and the parsed `go version -m`
records, both against hostile input.
"""

import pathlib
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from unittest import mock

import citest

SCRIPT = pathlib.Path(__file__).with_name("omo-control-native-evidence.py")
EVIDENCE = citest.load_gate(SCRIPT, "omo_control_native_evidence")

GO = shutil.which("go")

MACHINE_ID = "6c1f0a3e-1c0d-4d3a-9d1f-0e1b2a3c4d5e"
SESSION_ID = "b2a4c6d8-0f1e-4a3b-8c7d-6e5f4a3b2c1d"
MINUS_ZERO_REVISION = "0000000000000000000000000000000000000000"

# A production binary built with `-buildvcs=false`, carrying the same record
# shape a byte-scraping reader matched, from ordinary string data.
DECOY_SOURCE = textwrap.dedent(
    f"""
    package main

    import "fmt"

    var decoy = "\\nbuild\\tvcs.revision={MINUS_ZERO_REVISION}\\nbuild\\tvcs.time=1999-01-01T00:00:00Z\\n"

    func main() {{
    \tfmt.Print(decoy)
    }}
    """
)

VCS_SOURCE = textwrap.dedent(
    """
    package main

    func main() {}
    """
)


def write_module(directory, source):
    (directory / "go.mod").write_text("module fencefixture\n\ngo 1.21\n", encoding="utf-8")
    (directory / "main.go").write_text(source, encoding="utf-8")


def build(directory, binary, *flags):
    result = subprocess.run(
        ["go", "build", *flags, "-o", str(binary), "."],
        cwd=str(directory),
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise AssertionError(f"go build failed: {result.stderr}")
    return binary


def git(directory, *arguments):
    result = subprocess.run(["git", "-C", str(directory), *arguments], capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise AssertionError(f"git {arguments} failed: {result.stderr}")
    return result.stdout.strip()


class MachineSessionPathTest(unittest.TestCase):
    """`----` and `-` are not identifiers, and neither is a missing UUID."""

    def test_placeholder_segments_are_rejected(self):
        paths = [
            "/api/v1/machines/----/sessions/-",
            f"/api/v1/machines/{MACHINE_ID}/sessions/-",
            f"/api/v1/machines/-/sessions/{SESSION_ID}",
            f"/api/v1/machines/{MACHINE_ID}/sessions/",
            "/api/v1/machines/not-a-uuid/sessions/" + SESSION_ID,
            f"/api/v1/machines/{MACHINE_ID}-/sessions/{SESSION_ID}",
        ]
        for path in paths:
            with self.subTest(path=path), self.assertRaises(EVIDENCE.Rejected):
                EVIDENCE.machine_session_ids(path)

    def test_dashless_uuid_segment_is_not_the_canonical_form(self):
        with self.assertRaises(EVIDENCE.Rejected):
            EVIDENCE.machine_session_ids(f"/api/v1/machines/{MACHINE_ID.replace('-', '')}/sessions/{SESSION_ID}")

    def test_canonical_endpoint_is_accepted_and_normalised(self):
        self.assertEqual(
            EVIDENCE.machine_session_ids(f"/api/v1/machines/{MACHINE_ID}/sessions/{SESSION_ID}"),
            (MACHINE_ID, SESSION_ID),
        )
        self.assertEqual(
            EVIDENCE.machine_session_ids(f"/api/v1/machines/{MACHINE_ID.upper()}/sessions/{SESSION_ID}"),
            (MACHINE_ID, SESSION_ID),
        )

    def test_endpoint_with_a_query_or_extra_segment_is_rejected(self):
        for path in (
            f"/api/v1/machines/{MACHINE_ID}/sessions/{SESSION_ID}?x=1",
            f"/api/v1/machines/{MACHINE_ID}/sessions/{SESSION_ID}/extra",
        ):
            with self.subTest(path=path), self.assertRaises(EVIDENCE.Rejected):
                EVIDENCE.machine_session_ids(path)


class GoBuildSettingsTest(unittest.TestCase):
    def test_conflicting_duplicate_records_are_rejected(self):
        output = (
            "/tmp/daemon: go1.27.4\n"
            f"\tbuild\tvcs.revision={MINUS_ZERO_REVISION}\n"
            "\tbuild\tvcs.revision=1111111111111111111111111111111111111111\n"
        )
        with self.assertRaises(EVIDENCE.Rejected):
            EVIDENCE.parse_go_build_settings(output)

    def test_repeated_identical_record_is_not_conflicting(self):
        output = "\tbuild\tvcs.revision=abc\n\tbuild\tvcs.revision=abc\n"
        self.assertEqual(EVIDENCE.parse_go_build_settings(output), {"vcs.revision": "abc"})

    def test_only_parsed_build_records_are_read(self):
        output = (
            "/tmp/daemon: go1.27.4\n"
            "\tpath\tgithub.com/wxxb789/symmetry/daemon/cmd/symmetry-daemon\n"
            "\tmod\tgithub.com/wxxb789/symmetry/daemon\t(devel)\t\n"
            "\tdep\tgithub.com/foo/bar\tv1.0.0\th1:abc\n"
            "\tbuild\t-buildvcs=true\n"
            "\tbuild\tvcs=git\n"
            "\tbuild\tvcs.revision=deadbeef\n"
            "\tbuild\tvcs.modified=true\n"
        )
        self.assertEqual(
            EVIDENCE.parse_go_build_settings(output),
            {"vcs": "git", "vcs.revision": "deadbeef", "vcs.modified": "true"},
        )

    def test_missing_go_toolchain_is_refused_rather_than_guessed(self):
        with (
            mock.patch.object(EVIDENCE.shutil, "which", return_value=None),
            self.assertRaises(EVIDENCE.Rejected),
        ):
            EVIDENCE.go_build_source(pathlib.Path("unbuilt-daemon"))

    @unittest.skipUnless(GO, "the Go toolchain is required to build the fixture binaries")
    def test_decoy_revision_in_the_image_never_wins(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            write_module(root, DECOY_SOURCE)
            binary = build(root, root / "decoy", "-buildvcs=false")

            self.assertIn(f"build\tvcs.revision={MINUS_ZERO_REVISION}".encode(), binary.read_bytes())
            source = EVIDENCE.go_build_source(binary)

            # `-buildvcs=false` records no vcs.* setting at all, so the only
            # revision-shaped record in the image is the decoy string data.
            self.assertEqual(source["embedded_build_info"], {})
            self.assertEqual(
                source["source_revision"],
                "unverified: production binary lacks embedded VCS revision",
            )

    @unittest.skipUnless(GO, "the Go toolchain is required to build the fixture binaries")
    def test_recorded_revision_is_the_commit_the_binary_was_built_from(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            write_module(root, VCS_SOURCE)
            git(root, "init", "--quiet")
            git(root, "add", "--all")
            git(
                root,
                "-c",
                "user.email=fence@example.invalid",
                "-c",
                "user.name=Fence Fixture",
                "commit",
                "--quiet",
                "-m",
                "fixture",
            )
            commit = git(root, "rev-parse", "HEAD")
            binary = build(root, root / "committed")

            source = EVIDENCE.go_build_source(binary)

            self.assertEqual(source["embedded_build_info"]["vcs"], "git")
            self.assertEqual(source["embedded_build_info"]["vcs.revision"], commit)
            self.assertEqual(source["source_revision"], commit)


if __name__ == "__main__":
    unittest.main()
