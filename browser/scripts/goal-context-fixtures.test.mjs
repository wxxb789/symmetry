// The 0007h snapshot fixture answers on the same stdout as the application it
// starts (`mix run --no-start` boots the whole control plane, whose dev logger
// prints every SQL statement), and a log line can land after the fixture's own
// answer because the logger's handler writes asynchronously. The helper
// therefore recognises the answer by the fixture's own `snapshot_id=` marker
// and never by the shape of a bare id — the hazard this file pins: a
// UUID-shaped log line must not become the read snapshot id.
import assert from "node:assert/strict";
import { test } from "node:test";
import { parseSnapshotAnswer } from "../tests/goal-context-fixtures.mjs";

const snapshotId = "9f0b2c1e-5f7a-4c3b-8d2e-1a2b3c4d5e6f";
const logLine = `[debug] QUERY OK source="provider_action_intents" db=0.5ms queue=0.1ms\n`;

test("the fixture answer is recognised by its marker, never by an id-shaped log line", () => {
  // A log line that names an id is not an answer, and a bare answer is not
  // readable either: only the marker names the snapshot.
  assert.equal(parseSnapshotAnswer(`${logLine}${snapshotId}\n`), null);
  assert.equal(parseSnapshotAnswer(logLine), null);
  assert.equal(parseSnapshotAnswer(""), null);
  assert.equal(parseSnapshotAnswer(`${logLine}snapshot_id=${snapshotId}\n`), snapshotId);
  assert.equal(parseSnapshotAnswer(`snapshot_id=none\n`), "none");
  // A boot line that flushes after the answer cannot win it back: the answer is
  // the last marked line.
  assert.equal(parseSnapshotAnswer(`snapshot_id=${snapshotId}\n${logLine}${snapshotId}\n`), snapshotId);
});
