# Harness capture fixtures

`testdata/<harness>/<version>/` holds one directory per captured native-harness version.
Readers live in `daemon/internal/harness/<harness>/*_test.go` and open a version by name
(`TestedVersion` or an explicit string), never by iterating this tree.

## What stays

Per harness: the two most recent captures, plus any older directory that is

- digest-bound by a `fixture-manifest.json` (`TestClaudeTestdataManifestBindsRecordedDigests`,
  `TestPiTestdataManifestBindsRecordedDigests`),
- named by a `docs/design/harness.md` pin or a `docs/runbooks/` procedure,
- the only carrier of a distinct capture a test replays (the claude `2.1.281`
  structured-output captures, read by `claude/stream_test.go`).

Retire an older version by deleting its whole directory (a `schema-manifest.json` retires with
it), after checking `docs/runbooks/` and `docs/design/harness.md` for pins on that version.
`testdata/opencode/` is frozen: its `2.3.0/README.md` is cited by `docs/design/harness.md`.

## Adding a version

1. Copy the harness's canonical directory (`<harness>/<TestedVersion>/`) to
   `testdata/<harness>/<new-version>/`; replace `version.txt` with the new `--version` output
   where the directory carries one.
2. Capture the new evidence into that directory and update its `README.md`: what was captured,
   on which platform, and what it does not prove.
3. If the directory carries a `fixture-manifest.json`, recompute every fixture digest and the
   executable digest once the capture is final; the manifest is promotion evidence and must
   describe the committed bytes exactly.
4. Update the adapter's `TestedVersion` (and the executable or schema digests its promotion
   binds) and the `docs/design/harness.md` pin in the same change, so a reviewer sees the version
   bump, the new evidence and the pin together.
5. From `daemon/`, run `go test -count=1 ./internal/harness/...`.

A per-directory delta may replace a full copy only for a directory outside the frozen and
digest-bound sets above, and only with a recorded admission: it passes the deletion test
(removing the delta would re-spread an invariant or force callers to know more), records the
size-policy reason, repoints its reader in the same change, and re-proves decoded-frame equality.
