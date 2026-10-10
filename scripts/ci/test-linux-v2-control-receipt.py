#!/usr/bin/env python3
"""Exercise the real receipt publisher CLI using disposable Linux-owned inputs."""

from __future__ import annotations

import json
import os
import pathlib
import stat
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("publish-linux-v2-control-receipt.py")
OWNER_UID = 65534
OWNER_GID = 65534


class ReceiptPublisherTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        self.directory = self.root / "pi_provider_action"
        self.directory.mkdir(mode=0o700)
        os.chown(self.directory, OWNER_UID, OWNER_GID)
        self.source = self.root / "source.json"
        self.name = "owned-run.json"
        self.document = {
            "version": 1, "operation": "resource.sync", "state": "succeeded",
            "matched_count": 1, "failure_present": False, "case": "pi_provider_action",
            "work_item_id": "owned-work-item", "task_id": "owned-task", "run_id": "owned-run",
            "generation": 1, "runtime_id": "owned-runtime", "runtime_epoch": 1,
            "claim_id": "owned-claim", "resource_id": "owned-resource", "provider": "github",
            "resource_kind": "repository", "completed_at": "2026-10-10T00:00:00Z",
            "intent_id_sha256": "1" * 64, "action_id_sha256": "2" * 64,
            "result_metadata": {
                "operation": "resource.sync", "resource_id": "owned-resource", "projected": True,
                "readback": "applied", "resource": {
                    "provider": "github", "kind": "repository", "status": "ready", "sync_status": "synced",
                },
            },
        }
        self.write_source()

    def write_source(self) -> None:
        self.contents = json.dumps(self.document, indent=2).encode() + b"\n"
        self.source.write_bytes(self.contents)
        os.chmod(self.source, 0o600)

    def publish(self, uid: int = OWNER_UID, gid: int = OWNER_GID) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(SCRIPT), str(self.source), str(self.directory), self.name, str(uid), str(gid)],
            text=True, capture_output=True, check=False, timeout=10,
        )

    def assert_rejected_without_publication(self, result: subprocess.CompletedProcess[str], reason: str) -> None:
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(reason, result.stderr)
        self.assertEqual(list(self.directory.iterdir()), [])
        self.assertEqual(self.source.read_bytes(), self.contents)

    def test_five_argument_cli_publishes_exact_read_only_receipt(self) -> None:
        result = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        target = self.directory / self.name
        info = target.stat()
        self.assertTrue(stat.S_ISREG(info.st_mode))
        self.assertEqual((info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode), info.st_nlink),
                         (0, OWNER_GID, 0o440, 1))
        self.assertEqual(target.read_bytes(), self.contents)
        self.assertEqual(self.source.read_bytes(), self.contents)
        self.assertEqual(list(self.directory.iterdir()), [target])

    def test_invalid_json_is_not_published(self) -> None:
        self.contents = b'{"version":'
        self.source.write_bytes(self.contents)
        self.assert_rejected_without_publication(self.publish(), "unreadable or invalid JSON")

    def test_invalid_readback_schema_is_not_published(self) -> None:
        self.document["failure_present"] = True
        self.write_source()
        self.assert_rejected_without_publication(self.publish(), "not a Control database readback")

    def test_nonprivate_source_is_not_published(self) -> None:
        os.chmod(self.source, 0o640)
        self.assert_rejected_without_publication(self.publish(), "host-runner private")

    def test_mismatched_directory_owner_is_not_published(self) -> None:
        self.assert_rejected_without_publication(self.publish(uid=OWNER_UID - 1), "A-owned mode 0700")

    def test_mismatched_directory_group_is_not_published(self) -> None:
        self.assert_rejected_without_publication(self.publish(gid=OWNER_GID - 1), "A-owned mode 0700")

    def test_nonprivate_directory_is_not_published(self) -> None:
        os.chmod(self.directory, 0o750)
        self.assert_rejected_without_publication(self.publish(), "A-owned mode 0700")

    def test_run_identity_mismatch_is_not_published(self) -> None:
        self.document["run_id"] = "different-owned-run"
        self.write_source()
        self.assert_rejected_without_publication(self.publish(), "do not bind to the guest path")

    def test_existing_receipt_is_never_replaced(self) -> None:
        first = self.publish()
        self.assertEqual(first.returncode, 0, first.stderr)
        target = self.directory / self.name
        original = target.read_bytes()
        original_info = target.stat()
        self.document["completed_at"] = "2026-10-10T00:00:01Z"
        self.write_source()
        second = self.publish()
        self.assertNotEqual(second.returncode, 0)
        self.assertIn("File exists", second.stderr)
        self.assertEqual(target.read_bytes(), original)
        self.assertEqual(target.stat().st_ino, original_info.st_ino)
        self.assertEqual(list(self.directory.iterdir()), [target])
        self.assertEqual(self.source.read_bytes(), self.contents)


if __name__ == "__main__":
    if sys.platform != "linux" or os.geteuid() != 0:
        raise SystemExit("receipt CLI regressions require Linux root for new disposable ownership fixtures")
    unittest.main(verbosity=2)
