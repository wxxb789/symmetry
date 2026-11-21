#!/usr/bin/env python3
"""Prove the legacy-route audit derives its targets from the tree.

The audit used to enumerate thirteen targets by hand, so five placements of the
same retired literal were reported as "No U2 legacy action endpoints found":
a workflow file, a `docs/evidence` page, a file whose extension its globs did
not name inside a listed directory, an unlisted controller, and the root
`compose.yaml`. This test copies the real audit script into a scratch git
repository, plants each placement as a tracked file, and requires the audit to
fail on every one of them, while the negative control it excludes by name stays
quiet and a file the old list did cover still fails.

The fixtures live in a temporary repository, never in this tree, so no tracked
file gains a retired route literal.
"""

import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest

REPO = pathlib.Path(__file__).resolve().parents[2]
AUDIT = REPO / "scripts" / "audit-no-legacy-routes.sh"
BASH = shutil.which("bash") or "/bin/bash"

# The literal the committed evidence for the previous revision's gap used:
# adding it to a copy of docs/protocol-v1.md was what made the audit exit 1.
RETIRED = "/api/v1/tasks/example/cancel"

POSITIVE_CONTROL = "docs/protocol-v1.md"
NEGATIVE_CONTROLS = (
    "control/test/symmetry_control_web/controllers/protocol_controller_test.exs",
    "docs/evidence/ci-process-trim-2026-11-15.md",
    "docs/goals/archive/0005-REMEDIATION_PLAN.md",
    "scripts/ci/test-no-legacy-routes-audit.py",
)

# Filenames a retired literal can hide in, keyed by the reason the previous
# hand-enumerated target list missed them.
MISSED_PLACEMENTS = {
    "unlisted workflow file": ".github/workflows/fence-fixture.yml",
    "unlisted docs/evidence page": "docs/evidence/fence-fixture.md",
    "glob-unmatched extension in a listed directory": ("daemon/cmd/symmetry-daemon/testdata/fence-fixture.txt"),
    "extensionless file in a listed directory": ("daemon/cmd/symmetry-daemon/testdata/fence-fixture"),
    "unlisted controller": "control/lib/symmetry_control_web/controllers/fence_fixture_controller.ex",
    "root compose.yaml": "compose.yaml",
}


def fixture_body(path):
    comment = "" if path.endswith(".md") else "# "
    return (
        f"{comment}Retired U2 action endpoint, planted by "
        f"scripts/ci/test-no-legacy-routes-audit.py.\n"
        f"{comment}{RETIRED}\n"
    )


class LegacyRouteAuditTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.directory.name)
        (self.root / "scripts").mkdir()
        shutil.copy2(AUDIT, self.root / "scripts" / AUDIT.name)
        # The audit refuses to run when an exclusion no longer matches a literal
        # or names an untracked file, so every case starts with the real negative
        # controls in place.
        for control in NEGATIVE_CONTROLS:
            self.copy_from_repo(control)
        self.git("init", "--quiet")

    def tearDown(self):
        self.directory.cleanup()

    def git(self, *arguments):
        return subprocess.run(
            ["git", "-C", str(self.root), *arguments],
            capture_output=True,
            text=True,
            check=True,
        )

    def write(self, relative, text):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        return path

    def plant(self, relative):
        self.write(relative, fixture_body(relative))
        return relative

    def copy_from_repo(self, relative, suffix=""):
        target = self.root / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(REPO.joinpath(relative).read_text(encoding="utf-8") + suffix, encoding="utf-8")
        return relative

    def track_everything(self):
        self.git("add", "--all", "--force")

    def audit(self):
        return subprocess.run(
            [BASH, (self.root / "scripts" / AUDIT.name).as_posix()],
            capture_output=True,
            text=True,
            check=False,
            cwd=self.root.as_posix(),
        )

    def assert_audit_fails_on(self, planted):
        self.track_everything()
        result = self.audit()
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn(planted, result.stderr)
        self.assertIn(RETIRED, result.stderr)

    def test_every_previously_unscanned_placement_fails_the_audit(self):
        for reason, relative in MISSED_PLACEMENTS.items():
            with self.subTest(reason=reason):
                planted = self.plant(relative)
                self.assert_audit_fails_on(planted)
                self.git("rm", "--quiet", "--force", relative)

    def test_positive_control_that_the_old_target_list_covered_still_fails(self):
        planted = self.copy_from_repo(POSITIVE_CONTROL, suffix=f"\n{RETIRED}\n")
        self.assert_audit_fails_on(planted)

    def test_negative_control_is_excluded_and_left_alone(self):
        self.track_everything()
        result = self.audit()
        self.assertEqual(result.returncode, 0, result.stderr)
        for control in NEGATIVE_CONTROLS:
            self.assertNotIn(control, result.stderr)
        self.assertIn("No U2 legacy action endpoints found", result.stdout)

    def test_exclusion_that_no_longer_matches_a_literal_fails_the_audit(self):
        # An exclusion that excludes nothing is an unreachable rule: the audit
        # must refuse to run rather than silently widen its surface, which is
        # exactly what the retired `docs/plans/**` exclusion did.
        self.write(NEGATIVE_CONTROLS[0], "defmodule TrimmedProtocolTest do\nend\n")
        self.track_everything()
        result = self.audit()
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("carries no retired literal", result.stderr)

    def test_untracked_placement_is_out_of_scope(self):
        # The audit surface is the tracked tree, so an untracked scratch file is
        # deliberately invisible: this is the boundary the derivation relies on.
        self.track_everything()
        planted = self.plant(".github/workflows/untracked-fixture.yml")
        result = self.audit()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn(planted, result.stderr)

    def test_audit_reports_the_number_of_files_it_derived(self):
        self.copy_from_repo(POSITIVE_CONTROL)
        self.track_everything()
        result = self.audit()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("tracked files audited", result.stdout)
        # Bound the expectation to the list this suite declares, so adding a
        # negative control cannot leave a stale hard-coded count behind.
        self.assertIn(f"{len(NEGATIVE_CONTROLS)} intentional exclusions", result.stdout)


if __name__ == "__main__":
    sys.exit(unittest.main())
