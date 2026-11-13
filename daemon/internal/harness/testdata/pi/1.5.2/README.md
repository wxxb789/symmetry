# pi 1.5.2 RPC fixtures and promotion evidence

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client. The pin is pi `1.5.2`; evidence for `0.85.1` and `0.87.1`
is historical only.

## Promotion binding

The Windows `1.5.2` executable blob
`sha256:40a062bedd75767e2c3c889265de7c83d64a0ba55c74b60243c2453d97cffd85`
(129055232 bytes) promotes exactly four operations: start, events, cancel and
adapter-level retained resume, projected under implementation identity
`pi-rpc-v1`. Every other operation stays explicitly unsupported:

- handoff, native steer (guidance), safe pause, approval response, provider
  access, billable usage and hard cost limits — see the projection reasons in
  `daemon/internal/harness/pi/probe.go`.
- Linux admission, with the recorded conflict that the Linux v2 owner path is
  unavailable on the hosts this series runs on. Windows evidence is never
  promoted as Linux support.

The gate requires the anchored `1.5.2` version line, the `--mode rpc` help
transport, the resolved executable SHA-256 and the committed fixture digests in
`fixture-manifest.json`. The `mise` PATH shim
`sha256:53ca4fc6cfaf2b9b9a9635c54f17e5f4bf6ce599693dc67b17783bf382643850`
(298632 bytes) reports the same version line and is a different blob, so it
promotes nothing.

## Fixtures

- `version.txt`: the `pi --version` line of the installed `1.5.2` binary.
- `help.txt`: the verbatim `pi --help` screen (196 lines, 10883 bytes) that
  advertises `--mode <mode> … rpc`, `--session-dir`, `--tools` and
  `--no-approve`.
- `lifecycle.jsonl` and `compaction-continuation.jsonl`: synthetic models of the
  documented `pi --mode rpc` stream carried unchanged from `../0.87.1/`. They
  demonstrate a correlated `get_state`, an accepted prompt, retry-shaped
  `agent_end` events, `agent_settled` as the only final native boundary, and a
  legal continuation after an `agent_end` whose `willRetry` is false.
- `fixture-manifest.json`: SHA-256 of the executable blob, the promoted version
  line and every fixture above. `TestPiTestdataManifestBindsRecordedDigests`
  requires exactly this fixture set, so removing an evidence record fails a test.
  Recompute a committed digest with
  `git show <rev>:daemon/internal/harness/testdata/pi/1.5.2/<file> | sha256sum`.

## Evidence

[Native Windows promotion evidence](native-promotion-windows-2026-11-14.md)
records the probe promotion, the loopback repository turn, cancellation, retained
resume and the negative controls, all against the exact blob above, and lists the
prerequisites this environment could not satisfy (credentialed provider turn,
Linux runner).

Historical records keep their own scope and are not 1.5.2 promotion evidence:

- `native-windows-2026-11-11.md`: transport (`Open/get_state/Close`) and
  provider-bridge extension checks.
- `native-linux-2026-11-11.md`: the same transport check on Ubuntu 24.04 WSL2.
- `native-retained-resume-2026-11-11.md`: a deterministic loopback two-process
  resume on Windows and Linux before the promotion gate existed.

Superseded from that retained-resume record: its runs launched the configured
executable after checking only the version line, so they did not verify the
executed bytes. The opt-in evidence tests now verify the version line *and* the
recorded blob for the platform before launching anything.

## Opt-in evidence commands

The environment variables documented in
[`../0.85.1/README.md`](../0.85.1/README.md) still apply, substituting a `1.5.2`
executable. `TestNativeRepositoryTask` and
`TestNativeRepositoryTaskRetainedResume` need an operator-supplied numeric
loopback OpenAI Responses gateway through
`SYMMETRY_PI_NATIVE_REPOSITORY_TASK_BASE_URL`;
`TestNativeCancellationDrainsInFlightLoopbackRequest` starts its own in-process
gateway and needs `SYMMETRY_PI_NATIVE_CANCELLATION_EXECUTABLE`. The probe
promotion controls need `SYMMETRY_PI_NATIVE_PROBE=1`,
`SYMMETRY_PI_NATIVE_PROBE_EXECUTABLE` and, for the blob-mismatch control,
`SYMMETRY_PI_NATIVE_PROBE_SHIM`.

With the loopback configuration above, these tests produce transport-level
observations of the adapter; the recorded receipts used gateways that did not
contact an upstream provider. When the repository-task base URL is empty, the
repository-task and retained-resume tests instead use the allowlisted credential
path and may contact the configured upstream provider. Neither configuration
records provider accounting or exercises daemon/Control admission, PostgreSQL
durability or exactly-once effects.
