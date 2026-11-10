# CI peer-gate test ablation experiment

Date: 2026-09-29  
Inventory candidate: tooling C5  
Subject: dirty working tree based on `d14debbf6ae3ded09e5013ef90952d347c01474d`  
Decision: **remove the two redundant focused Go executions; keep both strict peer assertions over the ordinary suite's one preserved JSON stream**.

## Scope and invariant

The daemon CI job previously ran the ordinary Linux Go suite and then executed the Pi peer test once more and four OpenCode peer parents once more. The removal condition was that the ordinary suite's real JSON stream contain every required package-specific parent/subtest exactly once with one run, one pass, and no selected parent/child skip. The surviving gate must continue to reject malformed JSON and every Go package/test failure globally, including failures outside the selected package.

This experiment changed only `.github/workflows/ci.yml`, `scripts/ci/assert-go-test-json.py`, `scripts/ci/test-linux-v2-go-events.py`, and this report. Scratch evidence is under `.omo/ablation/ci-peer`. It did not modify Go production/test sources, use credentials or native hosts, touch the index, commit, push, or delete historical evidence.

## Bound source and evidence hashes

Initial hashes before this track:

- `.github/workflows/ci.yml`: `fb370223cce033b7936d8ca7989758e1dd3563324bbe526fe77e9a9d7683e25a`
- `scripts/ci/assert-go-test-json.py`: `fe32c218429d8a86c8c91c981733a2cd73c6f8c56ae9fcd5686c1279b270f617`
- `scripts/ci/test-linux-v2-go-events.py`: `0810035700dfd3e455ad7732bb9cb37cd5b4ad5727771e982d16c78f38673965`
- captured ordinary Linux stream `.omo/verification/ablation-full-linux-normal.jsonl`: `4385ccd2363e3ce2bf48c1891848dbfb5fd06b3383c29e04c2faf1ddffc68127`
- Pi selected-test source `daemon/internal/harness/pi/provider_bridge_peer_linux_test.go`: `8afb6815d91ad842e89d6a523075d862a633ea96e2728cd1b5d7d1571df1af6d`
- OpenCode selected-test source `daemon/internal/harness/opencode/peer_linux_test.go`: `bd1bce35de45d5c2463c163b01e99047e6e3b83dc173b3f139decbec98e81f94`

Final implementation hashes before writing this report:

- `.github/workflows/ci.yml`: `3c5cf2c0976c77089b2da60c0f865d298ea04a29cab68fc24283b6fe7eec42ca`
- `scripts/ci/assert-go-test-json.py`: `14e658cab38af077348ff3c74f1786d38eb91b59dfee0f80e805ff51440a2e0a`
- `scripts/ci/test-linux-v2-go-events.py`: `dc3f4fe338de0f7460693a9d7f0057e98d2484a9d33ff32f45fe70ba7672f4d0`

The captured stream has 3,984 test pass events, 95 test skip events, zero malformed lines, zero fail events, and an overall recorded exit status of 0. The experiment's parser independently recounted the event totals and selected events rather than relying only on the supplied summary.

## Baseline

Parser regression baseline:

```text
python scripts/ci/test-linux-v2-go-events.py
exit 0; Ran 2 tests; OK
```

Backward-compatible unscoped gates against the actual ordinary stream:

```text
python scripts/ci/assert-go-test-json.py .omo/verification/ablation-full-linux-normal.jsonl TestProviderBridgeCloseReleasesBoundPeerFD
exit 0

python scripts/ci/assert-go-test-json.py .omo/verification/ablation-full-linux-normal.jsonl TestOpenCodeHealthRejectsWrongLinuxV2PeerBindingsBeforeRequest TestOpenCodeHealthRejectsWrongLinuxV2PeerBindingsBeforeRequest/mismatched_retained_pidfd TestOpenCodeHealthRejectsWrongLinuxV2PeerBindingsBeforeRequest/closed_retained_pidfd TestOpenCodeV2VerifierRejectsReleasedPeer TestOpenCodePeerCloseWaitsForActiveVerification TestOpenCodeAdapterWithoutV2OwnerFailsBeforeHealthRequest
exit 0
```

Before implementation, both corresponding commands with `--package PACKAGE` exited 1 as expected-red because the old helper had no package option. This demonstrated the missing scope constraint rather than a green acceptance result.

The actual ordinary stream contained these exact package-bound events once each (`run=1 pass=1 skip=0`):

- package `github.com/wxxb789/symmetry/daemon/internal/harness/pi`: `TestProviderBridgeCloseReleasesBoundPeerFD`
- package `github.com/wxxb789/symmetry/daemon/internal/harness/opencode`: `TestOpenCodeHealthRejectsWrongLinuxV2PeerBindingsBeforeRequest`, its `mismatched_retained_pidfd` and `closed_retained_pidfd` subtests, `TestOpenCodeV2VerifierRejectsReleasedPeer`, `TestOpenCodePeerCloseWaitsForActiveVerification`, and `TestOpenCodeAdapterWithoutV2OwnerFailsBeforeHealthRequest`

## Variant

`assert-go-test-json.py` now accepts optional `--package PACKAGE`. Existing positional calls are unchanged. Expected run/pass/skip counts and selected descendant-skip detection are package-scoped when supplied. Malformed lines and every `Action=fail` remain fatal globally regardless of package.

The daemon job now runs exactly one ordinary command:

```text
go test -count=1 -timeout 300s -json ./... >"$RUNNER_TEMP/daemon-go-test.json" 2>&1
```

It captures the Go exit code, prints/preserves that same stream, runs the Pi and OpenCode package-scoped strict gates over it, then makes the original Go exit fatal before the summary status. The `-count=1` and 300-second ordinary timeout are unchanged. The existing race command (`go test -race -count=1 -timeout 600s ./...`), Linux build, vet, formatting, fixture-contract tests, and every other job/inline parser are unchanged by this track.

Removed executions:

```text
go test -count=1 -timeout 120s -json ./internal/harness/pi -run '^TestProviderBridgeCloseReleasesBoundPeerFD$'

go test -count=1 -timeout 120s -json ./internal/harness/opencode -run '^(TestOpenCodeHealthRejectsWrongLinuxV2PeerBindingsBeforeRequest|TestOpenCodeV2VerifierRejectsReleasedPeer|TestOpenCodePeerCloseWaitsForActiveVerification|TestOpenCodeAdapterWithoutV2OwnerFailsBeforeHealthRequest)$'
```

Only those two redundant Go executions were removed. Their strict assertions survive.

## Variant verification

Focused parser suite:

```text
python scripts/ci/test-linux-v2-go-events.py
exit 0; Ran 6 tests; OK
```

This explicitly verifies old positional compatibility; missing run/pass; duplicate run/pass; same-name collision; foreign-only same-name events; selected child skip; global failure outside the selected package; parent skip; package failure; and malformed JSON.

Actual captured-stream package gates:

```text
python scripts/ci/assert-go-test-json.py .omo/verification/ablation-full-linux-normal.jsonl --package github.com/wxxb789/symmetry/daemon/internal/harness/pi TestProviderBridgeCloseReleasesBoundPeerFD
exit 0

python scripts/ci/assert-go-test-json.py .omo/verification/ablation-full-linux-normal.jsonl --package github.com/wxxb789/symmetry/daemon/internal/harness/opencode TestOpenCodeHealthRejectsWrongLinuxV2PeerBindingsBeforeRequest TestOpenCodeHealthRejectsWrongLinuxV2PeerBindingsBeforeRequest/mismatched_retained_pidfd TestOpenCodeHealthRejectsWrongLinuxV2PeerBindingsBeforeRequest/closed_retained_pidfd TestOpenCodeV2VerifierRejectsReleasedPeer TestOpenCodePeerCloseWaitsForActiveVerification TestOpenCodeAdapterWithoutV2OwnerFailsBeforeHealthRequest
exit 0
```

Synthetic mutation commands used the form:

```text
python scripts/ci/assert-go-test-json.py .omo/ablation/ci-peer/mutations/CASE.jsonl --package example/selected TestWitness
```

Results, kept classified as expected-red:

- missing selected test: exit 1
- duplicate selected run: exit 1
- selected child skip: exit 1
- foreign-package fail event with a valid selected pass: exit 1
- foreign-only same-named test: exit 1
- malformed line with a valid selected pass: exit 1
- selected exact pass plus a foreign same-named exact pass: exit 0 (package-collision survivor assertion)

Additional checks:

```text
python -m py_compile scripts/ci/assert-go-test-json.py scripts/ci/test-linux-v2-go-events.py
exit 0

python -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml', encoding='utf-8'))"
exit 0

git diff --check -- .github/workflows/ci.yml scripts/ci/assert-go-test-json.py scripts/ci/test-linux-v2-go-events.py
exit 0
```

Python language-server diagnostics were unavailable because `basedpyright-langserver` is not installed; no dependency was installed. Compilation and the executable focused suite provide the local Python checks instead.

## Decision and remaining limits

**Proven remove:** the two later selective `go test` processes are redundant with the known-green ordinary JSON stream at the same Linux package/test boundary and can be deleted. **Proven keep:** both package-specific strict event assertions, parser regression tests, ordinary suite, race suite, build, and all native/platform/credentialed checks.

No full Go suite was rerun because the task explicitly supplied the complete green ordinary Linux stream for reuse; no selected rerun was needed because that exact stream already proves all required real peer tests and subtests. The parent owns final full-suite execution and integration. This result is Linux ordinary-suite evidence only; it does not replace race, Windows, native credentialed, host-capability, or protected-environment acceptance. Independent review must inspect the workflow diff before integration, as required by policy.
