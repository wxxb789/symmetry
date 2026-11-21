# rf-w5-01 witness converge-in-place plan — one Linux witness config, parsed per use, no build tag

Contract: the GDD goal contract `goal-rf-w5-01-witness-converge-in-place.md` (2026-11-21, with the
W5-review reconcile decisions A/B/F and the conductor's program-wide rules). Baseline for every
coordinate below: `origin/main` = `12de9428a4db5cbe67461ed8511c08978ea3364e` (the dispatch head of
this worktree; `git rev-parse origin/main` = `git rev-parse HEAD`). Branch:
`refactor/rf-w5-01-witness-converge-in-place`. Area: `daemon/internal/platform` (Linux).

The contract was re-derived against `6a815c0f`/`c9ed23b4`; every coordinate it carries was
re-measured here. Deltas against the contract text, re-derived at this head:

- `supervisor_linux_helper.go` is **794 L**, not 1,228 L (W4-02 `9f82fd8e` moved the session loop
  into `supervisor_session.go`); the live call site `installLinuxSupervisorScanFailureInjectionAfterInitial`
  is still at `supervisor_linux_helper.go:615`.
- `containment_supervisor_linux.go` is 964 L (W5-09 `0132bd2b` removed four test-only wrappers).
- The 18 witness env constants are `containment_supervisor_linux.go:46-67`; the env reads are
  21 in that file and 8 in `supervisor_linux_client.go` (29 total), matching the contract's
  post-split count.
- `ci.yml` step coordinates moved with #212/W3-01, re-measured at `95498fdf` (`.github/workflows`
  is untouched by the branch, so they hold for every revision of this range): daemon job `:1493`
  (race step name `:1650`; the `go test -race -count=1` command sits at `:1658`), integration
  witness matrix `:2322`, compose rootless refusal step `:2649`, Windows containment witness step
  `:1988`, Windows pre-authority matrix step `:2007`, daemon crash response-loss witness step
  `:2210` (renamed region; the contract's `:2058` coordinate). No step, job, test-name or
  artifact-name change is planned, so those coordinates stay verification surfaces only.

## Owned set (STRICT, re-derived at this head)

Edits:

- `daemon/internal/platform/containment_supervisor_linux.go` (const block removed; five family
  readers re-pointed at the parser; three injection marker writes routed through the validating
  writer; `publishLinuxSupervisorWitnessFile` gains the parent validation);
- `daemon/internal/platform/supervisor_linux_client.go` (three read sites re-pointed at the parser);
- **new** `daemon/internal/platform/supervisor_linux_witness.go` (`//go:build linux`, the source
  file's exact constraint): the 18 env-name constants, the config struct, the parser, the marker
  parent validation and the validating marker writer;
- **new** `daemon/internal/platform/supervisor_linux_witness_test.go` (`//go:build linux`): the
  per-use toggle test, the gate-unset negative test, the marker-parent refusal test;
- this plan file; `docs/TODO.md` (one entry for the Windows residual, decision A).

Unchanged, named for the record: `supervisor_linux_helper.go` (calls the family readers, zero env
reads of its own — nothing to re-point), `supervisor_session.go` (ditto), the e2e witness files
(the per-matrix opt-ins stay test-side constants), `cmd/symmetry-daemon/*` (the v2 witness command
reads the app-owned `LinuxV2NativeOwnerWitnessEnvironment` gate; no platform reader moves),
`daemon/internal/app/linux_owner_witness_linux.go`, `daemon/internal/app/cleanup_glue.go`,
`daemon/internal/execution/runner.go` (read-only consumers/producers of the frozen gate name),
`.github/workflows/*` (no workflow edit: the PR-gated `daemon` job runs `go test -race ./...`,
which covers the new Linux-tagged platform tests, and every CI-gated step/env/artifact name is
frozen), the Windows files (decision A).

## Inventory: 18 env names, base read sites, config field

Measured with `grep -n 'os.Getenv|os.LookupEnv'` over the Linux production files at this head
(21 + 8 + 0 + 0 = 29 reads). Field names are the delivered parser's.

| # | env name | base read sites | field |
| ---: | --- | --- | --- |
| 1 | `SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS` | `containment_supervisor_linux.go:122,160,197,339,377`; `supervisor_linux_client.go:1578,1616` | `productionWitness` |
| 2 | `SYMMETRY_LINUX_SUPERVISOR_TEST_INJECT_WRONG_HELLO_SESSION` | `containment_supervisor_linux.go:200` | `helloSessionInjection` |
| 3 | `SYMMETRY_LINUX_SUPERVISOR_TEST_WRONG_HELLO_SESSION_FIRED_PATH` | `:207` | `helloSessionFiredPath` |
| 4 | `SYMMETRY_LINUX_SUPERVISOR_TEST_WRONG_HELLO_SESSION_RELEASE_PATH` | `:211` | `helloSessionReleasePath` |
| 5 | `SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_STOP_BEFORE_PROOF_REACHED_FILE` | `:391` | `ownerLossProofReachedPath` / `…Set` |
| 6 | `SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_STOP_BEFORE_PROOF_RELEASE_FILE` | `:391` | `ownerLossProofReleasePath` / `…Set` |
| 7 | `SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_STOP_PROOF_RESULT_FILE` | `:391` | `ownerLossProofResultPath` / `…Set` |
| 8 | `SYMMETRY_LINUX_SUPERVISOR_TEST_OWNER_RECOVERY_AUTHENTICATED_FILE` | `:335` | `ownerRecoveryAuthenticatedPath` / `…Set` |
| 9 | `SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_REACHED_FILE` | `supervisor_linux_client.go:1432` | `leaseArmedBarrierReachedPath` |
| 10 | `SYMMETRY_LINUX_SUPERVISOR_DROP_RESPONSE_ONCE` | `supervisor_linux_client.go:1581,1617` | `dropResponseOnce` |
| 11 | `SYMMETRY_LINUX_SUPERVISOR_DROP_RESPONSE_FIRED_PATH` | `supervisor_linux_client.go:1599` | `dropResponseFiredPath` |
| 12 | `SYMMETRY_LINUX_SUPERVISOR_RELEASE_RESPONSE_LOSS_BARRIER_REACHED_FILE` | `supervisor_linux_client.go:1627` | `releaseBarrierReachedPath` / `…Set` |
| 13 | `SYMMETRY_LINUX_SUPERVISOR_RELEASE_RESPONSE_LOSS_BARRIER_RELEASE_FILE` | `supervisor_linux_client.go:1628` | `releaseBarrierReleasePath` / `…Set` |
| 14 | `SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_ONCE` | `containment_supervisor_linux.go:123,126,163` | `scanFailureMode` |
| 15 | `SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_TRIGGER_PATH` | `:131,171` | `scanFailureTriggerPath` |
| 16 | `SYMMETRY_LINUX_SUPERVISOR_INJECT_SCAN_FAILURE_FIRED_PATH` | `:132,172` | `scanFailureFiredPath` |
| 17 | `SYMMETRY_LINUX_SUPERVISOR_SCAN_PENDING_RELEASE_PATH` | `:136,177` | `scanPendingReleasePath` |
| 18 | `SYMMETRY_LINUX_SUPERVISOR_SCAN_SEAL_OWNER` | `:143,183` | `scanSealOwner` |

## Change 1 — one parser, called per use, no cache (decision F)

`linuxSupervisorWitnessConfigFromEnv()` returns a `linuxSupervisorWitnessConfig`: one
`os.Getenv`/`os.LookupEnv` read per name, in the single new file. Every existing family reader
keeps its name, signature and body structure and calls the parser at its own entry, so each hook
entry re-reads the current environment — required because the app witness installs and restores
the gate and barrier names inside the running daemon
(`daemon/internal/app/linux_owner_witness_linux.go:3365-3445`). No `sync.Once`, no `init()`, no
package-level cache; a cache is the mutation the per-use test reddens.

Values are recorded only while the gate reads armed (`strings.TrimSpace(gate) == "1"`), so a
gate-unset config carries no action value. The three `LookupEnv` families (six names) record value+presence;
the owner-recovery presence flag is recorded even when the gate is unset, because that reader's
base behavior is a fail-closed error for "configured without the gate" and must stay byte-exact
(clause 3's control-flow rule; the error text is base-identical). `productionWitness` keeps its raw
value because base gate checks differ in trimming (`:122` trims; `:160`, `:197`, `:339`, `:377`,
client `:1578`, `:1616` do not) and each consumer keeps its own check verbatim.

e2e-only opt-ins stay out of the struct: `…_NEGATIVE_WITNESS`, `…_REPLAY_WITNESS`,
`…_NEGATIVE_PID_REUSE_*` (8 names), `…_WITNESS_OUTPUT_DIR` remain test-side constants in their
`daemon/e2e` files; the Windows raw `SYMMETRY_PREAUTHORITY_*` literals stay untouched (decision A).

## Change 2 — no hook acts while the gate is unset

With the gate unset the config's action values are empty and every hook derived from it is inert:
scan-failure injection and clean-scan barriers, hello-session injection, owner-loss proof barrier,
lease-armed mirror seal, drop-response, release response-loss barrier, and every marker write they
gate. Marker and selector readers cannot arm from an env var alone. The single fail-closed
exception is preserved, not created: `linuxSupervisorOwnerRecoveryAuthenticatedMarkerFromEnv`
still errors when its marker is configured while the gate is unset (base
`containment_supervisor_linux.go:339-341`), which is stricter than a no-op and leaves the helper
start refused, exactly as before.

## Change 3 — marker parent validation (decision B), no new env name

New `validateLinuxSupervisorWitnessMarkerParent(path)`: the marker's parent directory must exist
(`Lstat`), be a real directory (a symlink parent is refused), be owned by the uid performing the
write (`os.Getuid()` — the uid that armed the gate, since the gate and the marker paths travel by
inheritance into the daemon and its helper; inside a mapped user namespace the owner and the
writer map together, which the PR-gated PID-reuse case requires), and not be group- or
other-writable. A root-owned parent is refused whenever the writer is not root. Failure wraps one
unexported sentinel so the refusal tests pin the validation and not a filesystem `EACCES`.

Both witness write funnels validate before every write:

- `publishLinuxSupervisorWitnessFile` (absent-destination `Lstat`, `CreateTemp` + rename kept
  byte-identical otherwise) — covers the drop-response marker, the release response-loss reached
  marker, the hello-session witness, the owner-loss reached/result markers and the owner-recovery
  authenticated marker;
- new `writeLinuxSupervisorScanWitnessMarker` (`os.WriteFile`, same mode/flags as base) — the
  three scan-injection writes at `containment_supervisor_linux.go:547,611,628`.

No env-supplied absolute path reaches `os.Create`/`os.WriteFile`/`os.Rename` in the privileged
helper unvalidated. The existing absent-destination checks stay; no new env var is added, so
`ci.yml` env blocks and the e2e files keep their bytes.

## Change 4 — no build tag (D4)

The witness code stays in the shipped files behind the runtime switch; the finding's
`//go:build linux && symmetry_witness` proposal is overridden. Rationale recorded here and in the
report: witness evidence binds the production binary's SHA-256
(`docs/evidence/goal-0006-linux-owner-design-2026-09-22.md:424-434`; `docs/design/harness.md:35,
:42, :66, :111`), so a witness build would attest a blob that never ships.

## Frozen surfaces

- Witness artifact paths, names and formats consumed by
  `scripts/ci/export-linux-v2-redacted-evidence.py` and the CI evidence gates: unchanged (no
  literal path/name exists in the changed code; the env names that carry them are byte-identical).
- CI-gated job, step and test names: unchanged. The new test names are additive in the PR-gated
  `daemon` job's `go test -race ./...`; the four PR-gated witness steps keep their assert lists.
- Non-witness wire/persisted/error/log text: unchanged. The only new error text is on witness
  refusal paths, which did not exist before.
- No `//nolint`/`credo:disable` added; comment policy applied (the only new comments are the
  config struct's per-use why and the unarmed branch's owner-recovery presence-flag exception).

## Public / API shape

Unchanged: no exported declaration is added, removed or re-signatured; the config type, the parser
(`linuxSupervisorWitnessConfigFromEnv`), the parent validation
(`validateLinuxSupervisorWitnessMarkerParent`) and the scan-marker writer
(`writeLinuxSupervisorScanWitnessMarker`) are package-private, and their new sentinel error is
unexported. The 18 env-name constants move to the new file with identical names, values and
package scope, so every in-package test reference still resolves. No protocol frame, JSON
document, journal field or CLI surface changes; the only build constraints are the two new files'
`//go:build linux`, matching the source file they came from. The package's exported surface is
unchanged, measured per file (`git show HEAD:<file> | grep -E '^(func|type|var|const) [A-Z]'` vs
the working tree, `exported-surface.txt`): `containment_supervisor_linux.go` has none on either
side and `supervisor_linux_client.go` contributes the same eight declarations; the new production
file contributes none and the new test file only its three `Test…` entry points.

## Exact edge-case semantics

- **Per use.** Every family reader re-invokes the parser; the per-use test toggles the gate between
  two reads and asserts armed → empty, and empty → armed (the app witness's install direction).
- **Trimming parity.** Each consumer trims exactly where base trimmed: `TrimSpace` for the scan
  injection values and the marker paths, raw comparison for the drop-response operation and the
  untrimmed gate checks.
- **Presence parity.** The three `LookupEnv` families keep absent ≠ present-but-empty: zero of
  three configured → inert; one or two → the base error; three → parsed. `…_RELEASE_*` barrier:
  neither set → nil, one set → the base error; both set → validated.
- **Write ordering.** Parent validation runs before the absent-destination check and before any
  temporary file is created, so a refusal leaves the directory untouched.
- **Refusal shape.** A symlinked, foreign-owned, root-owned (non-root writer) or group/other-writable
  parent is refused with `errors.Is(err, errLinuxSupervisorWitnessMarkerParentUnsafe)`, and no
  marker byte is written.

## Tests added

`daemon/internal/platform/supervisor_linux_witness_test.go`:

1. `TestLinuxSupervisorWitnessConfigIsReadPerUse` — gate on → config armed with values; gate off →
   armed=false, action values empty; gate re-set → armed again. Red mutation: a `sync.Once`
   snapshot in the parser makes the second read stale.
2. `TestLinuxSupervisorWitnessHooksAreInertWithoutTheGate` — every hook env name set with the gate
   off: the config is unarmed, each family reader returns nothing, the injection hook leaves the
   real monitor reader installed (`skipInitial=false`, so a mutant that installs the injection
   reddens on the first read), `dropResponseTransportOnce` /
   `waitForLinuxSupervisorReleaseResponseLossBarrier` are inert against a live harness (open
   `responseRead`, receipt present; the release harness carries `responseDropTriggered`), the
   owner-recovery reader keeps its fail-closed error with the base refusal text pinned, and no
   configured marker path exists afterwards. A second phase sets the gate to a raw `" 1 "` (the
   parser arms via `TrimSpace` while every raw-comparing hook check stays false) with a bad scan
   mode and partial owner-loss / barrier sets, so deleting the raw gate check of the scan-failure
   validator, hello-session injection, owner-loss reader/validator, owner-recovery reader,
   drop-response or release-barrier hook reddens at that hook's own checkpoint. Red mutation:
   delete the parser's unarmed early return (M2/M3); the scan-injection reader trims exactly like
   the parser, so its own check is implied by the armed rule and has no independent mutant.
3. `TestLinuxSupervisorWitnessMarkerParentRefusal` — the three write funnels accept an owned private
   directory (happy-path pin), and refuse a symlinked parent, a group/other-writable parent and a
   root-owned parent (when the writer is not root), with no marker byte written and no directory
   entry added to the refused symlinked or group-writable parent (validation precedes `CreateTemp`):
   mutant "always refuse" reddens the happy path, mutant "no validation" reddens the refusals,
   mutant "validate after `CreateTemp`" reddens the entry-count pins.

## Evidence plan (exact commands; carriers: this plan + the stage report = `answer.md`)

| Gate | Command | Carrier |
| --- | --- | --- |
| format | `gofmt -l daemon/internal/platform` | host |
| vet | `go vet ./...` (from `daemon/`) | host, windows/amd64 |
| build (windows) | `GOOS=windows go build ./...` | host |
| build (linux) | `GOOS=linux go build ./...` | host |
| build (linux/arm64) | `GOOS=linux GOARCH=arm64 go build ./...` | host |
| build (gate consumers) | `GOOS=linux go build ./internal/execution/... ./internal/app/...` | host |
| package tests (Windows subset) | `go test -count=1 ./internal/platform/... ./cmd/symmetry-daemon/...` | host |
| package tests (Linux, contract command) | `wsl -d Ubuntu-24.04 -u accept` bash: `cd /mnt/q/repos/wt-w5-01-witness-converge/symmetry/daemon && go test -count=1 -v ./internal/platform/` | WSL |
| new tests focused | WSL: `go test -count=1 -v -run 'TestLinuxSupervisorWitness' ./internal/platform/` | WSL |
| per-use red mutation | scratch edit: `sync.Once` cache in the parser → the toggle test fails; revert | WSL |
| gate-unset red mutation | scratch edit: gate check removed from a hook → the negative test fails; revert | WSL |
| refusal red mutation | scratch edit: validation removed → the refusal test fails; revert | WSL |
| e2e witness subset | `go test -count=1 -run '^TestProductionLinuxContainment' ./e2e/` under the gate envs | WSL if the host can supply a Control plane; else CI-only (recorded) |
| env-name set | `grep` of `SYMMETRY_*` literals over the daemon tree, base vs HEAD: set identical | host |
| parser-only grep | `grep -n 'os.Getenv|os.LookupEnv'` + env-name grep over non-test `daemon/internal/platform` non-Windows files | host |
| witness-name/artifact diff | the e2e + `scripts/ci` + `.github/workflows` consumed names, base vs HEAD: identical | host |
| whitespace | `git diff --check` | host |
| host path leak | profile-path grep over the branch diff and the new files | host |

## Control-flow audit (base → HEAD, per hook family; lesson 32)

Every base early return, fall-through, error short-circuit and default clause, read from
`git diff -U3` at this head (`diff-containment.txt`/`diff-client.txt` in the stage artifact
directory):

| family | base guards (`containment_supervisor_linux.go` / `supervisor_linux_client.go`) | HEAD equivalent | preserved |
| --- | --- | --- | --- |
| F1 scan-failure injection reader `:121→103` | gate-trim ≠ "1" or mode == "" → (∅, false); bad mode → (∅, false); empty/non-abs trigger or fired → (∅, false); barrier mode with empty/non-abs release → (∅, false); seal mode with owner ∉ {both, helper, mirror} → (∅, false); else (injection, true) | same five guards, same order, values from the config | yes ×5 |
| F2 scan-failure validation `:159→142` | gate ≠ "1" (untrimmed) → nil; mode == "" → nil; bad mode → error; empty/non-abs trigger or fired → error; barrier mode with bad release → error; seal mode with bad owner → error; else nil | same six guards, same messages | yes ×6 |
| F3 hello-session injection reader `:196→180` | gate ≠ "1" → (∅, false, nil); mode == "" → (∅, false, nil); mode ≠ "1" → error; bad fired → error; non-empty non-abs release → error; else (injection, true, nil) | same five guards, same messages | yes ×5 |
| F4 owner-recovery marker reader `:334→319` | marker absent (`LookupEnv` !ok) → (∅, false, nil); gate ≠ "1" → error; empty/non-abs path → error; destination present → wrapped error; else (marker, true, nil) | presence flag from the parser (recorded before the armed gate), path from the armed config; same four guards, same messages | yes ×4 |
| F5 owner-loss proof barrier reader `:376→361` | gate ≠ "1" → (∅, false, nil); 0 of 3 configured → (∅, false, nil); 1–2 of 3 → error; any empty/non-abs → error; duplicate cleaned paths → error; destination present → wrapped error; else (barrier, true, nil) | configured/raw pairs come from the parser's three `LookupEnv`s; same six guards, same messages | yes ×6 |
| F6 lease-armed mirror seal `client:1430` | `!enabled` or empty lease-armed path → nil; committed/resumed/already-triggered → nil; nil mirror → wrapped error; scan request errors → wrapped errors; seal/verify errors → wrapped errors | the same expression and body; the lease path now comes from a parser read on the same short-circuit arm; `enabled` still comes from F1 | yes, all |
| F7 drop-response hook `client:1577` | nil supervisor → false; gate ≠ "1" → false; operation ∉ {stop, release} or ≠ operation → false; already triggered / nil response transport / release without receipt → false; marker path trimmed non-empty → publish (error ignored); close reader/writer; true | nil check and gate check split into two statements in the same order; every later guard byte-identical | yes ×4 |
| F8 release response-loss barrier `client:1619` | gate ≠ "1" or operation ≠ release → nil; not drop-triggered → nil; neither barrier name set → nil; exactly one set → error; empty after trim → error; identical cleaned paths → error; reached/release absent checks → wrapped errors; no receipt → error; publish failure → wrapped error; poll timeout/error → error; else nil | same guards and messages; the two `LookupEnv` pairs now come from the parser | yes, all |
| F9 marker publishers | `publishLinuxSupervisorWitnessFile :242`: absent-destination `Lstat` → error; `CreateTemp`/`Write`/`Sync`/`Close`/`Rename` failures → error; success → nil. The three scan writes were bare `os.WriteFile(…, 0o600)` whose errors each site handled (pause → `return false`; seal → ignored; injection → `restore()` + `%w`-wrapped injection error with `%v` marker text) | one new guard inserted first (parent validation, decision B); everything after it byte-identical. The scan writes call `writeLinuxSupervisorScanWitnessMarker` (validation + `os.WriteFile` 0o600) with each site's error handling unchanged | yes ×5 + 3, plus the new guard |

Not consolidated, by scope: the v2 selector read (`anchor_types.go:51`, `anchor_gate.go`, D3) and
the app-side stop-receipt barrier (`internal/app/cleanup_glue.go:18-20`, out of the owned set);
both keep their bytes.

CI-side oracles (named; not runnable from this stage, which neither commits nor pushes — recorded
in the report): the four PR-gated witness steps — daemon-windows containment `ci.yml:1988`,
daemon-windows pre-authority matrix `:2007`, integration "Run production Linux containment witness
matrix" `:2322` (the Linux-family oracle; also the per-use negative/toggle tests via the daemon
job's `go test -race ./...` at `:1658`), compose "Run rootless Compose native refusal witnesses"
`:2649` (also runs the two `TestProductionLinuxV2*` refusal tests); daemon crash response-loss
witness `:2210` named as unaffected. The dispatch-only `linux-native-v2-acceptance` workflow is
not run (D3); its consumed env and artifact names are unchanged.

## Executed gates (this tree, 2026-11-22; artifacts in the stage artifact directory)

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| format | `gofmt -l internal/platform` | 0 | 0 paths |
| vet (host, windows/amd64) | `go vet ./...` | 0 | 0 diagnostics |
| build windows | `GOOS=windows GOARCH=amd64 go build ./...` | 0 | 0 diagnostics |
| build linux | `GOOS=linux GOARCH=amd64 go build ./...` | 0 | 0 diagnostics |
| build linux/arm64 | `GOOS=linux GOARCH=arm64 go build ./...` | 0 | 0 diagnostics |
| build gate consumers | `GOOS=linux GOARCH=amd64 go build ./internal/execution/... ./internal/app/...` | 0 | 0 diagnostics |
| package tests (Windows subset) | `go test -count=1 ./internal/platform/... ./cmd/symmetry-daemon/...` | 0 | `ok internal/platform 7.047s`, `ok cmd/symmetry-daemon 0.848s` |
| gate-consumer suites (host) | `go test -count=1 -timeout 600s ./internal/execution/... ./internal/app/...` | 0 | `ok internal/execution 12.765s`, `ok internal/app 210.020s`, unedited |
| new tests focused (WSL) | `go test -count=1 -v -run "TestLinuxSupervisorWitness" ./internal/platform/` (uid 1000) | 0 | 4 PASS (the three new tests plus `TestLinuxSupervisorWitnessPublishKeepsDirectoryClean`) |
| platform suite, contract form (WSL) | `go test -count=1 -v ./internal/platform/` | 1 | 231 top-level PASS, 184 subtest PASS, 4 FAIL, 1 SKIP, then `panic: test timed out after 10m0s` (`FAIL … 600.069s`). The four failures are this host's documented loopback limitation in untouched files: `TestVerifyLoopbackTCPPeerWaitsForDeferredAcceptWithoutWriting` (133.92s), `TestVerifyLoopbackTCPPeerDoesNotExtendCallerDeadlineDuringDeferredAccept` (135.16s), `TestLinuxTCPPeerV2RejectsDifferentNetworkCookie` (135.15s), `TestVerifyLoopbackTCPPeerV2UsesRetainedPIDFDAndPinnedNamespace` (135.17s), each `dial tcp4 127.0.0.1:<port>: connect: connection timed out`; the hang is `TestVerifyLoopbackTCPPeerV2WaitsForPendingAcceptOnSameConnectionWithoutWriting` |
| platform suite, host pattern (WSL) | `go test -count=1 -v -skip "Loopback\|TCPPeer" ./internal/platform/` | 0 | `ok … 21.495s`; 228 top-level PASS, 171 subtest PASS, 0 FAIL, 1 SKIP |
| Windows containment witness (CI step `:1988`, run locally) | `SYMMETRY_PRODUCTION_CONTAINMENT_WITNESS=1 go test -count=1 -timeout 180s ./internal/platform -run '^TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse$'` | 0 | `--- PASS … (6.80s)`, `ok … 6.987s` |
| Windows pre-authority matrix (CI step `:2007`, run locally) | `SYMMETRY_PRODUCTION_PREAUTHORITY_WITNESS=1 go test -count=1 -timeout 600s ./e2e -run '^TestProductionPreAuthority(CrashWitnessMatrix\|WrongSecretFirstClientRace)$'` | 0 | `ok e2e 49.549s`; both named tests PASS (`…CrashWitnessMatrix` incl. `clear` 1.83s, `…WrongSecretFirstClientRace` 29.04s) |
| witness e2e compile (linux) | `GOOS=linux GOARCH=amd64 go vet ./e2e/` | 0 | three containment witness files type-check against the new platform code |
| platform/cmd vet (linux) | `GOOS=linux GOARCH=amd64 go vet ./internal/platform/ ./cmd/symmetry-daemon/` | 0 | 0 diagnostics |
| env-name set | `git grep -h -o -E 'SYMMETRY_[A-Z0-9_]+' <rev> -- '*.go' '*.yml' '*.yaml' '*.sh' '*.py' \| sort -u`, `de526861` vs `95498fdf` | 0 | 270 = 270 names, `diff` empty |
| parser-only read grep | `grep -n 'os\.Getenv\|os\.LookupEnv' internal/platform/*.go` filtered to non-test, non-Windows | 0 (grep) | only `supervisor_linux_witness.go` (18 reads) plus the unrelated `NOTIFY_SOCKET` read in `anchor_wire.go:909` |
| parser-per-use grep | `grep -rn 'linuxSupervisorWitnessConfigFromEnv()' internal/platform/*.go` filtered to non-test | 0 (grep) | 1 definition (`supervisor_linux_witness.go:74`) and 8 call sites — `containment_supervisor_linux.go:104,143,181,323,365` (the five family readers) and `supervisor_linux_client.go:1432,1581,1620` (mirror seal, drop-response, release barrier), i.e. every base read-site function re-reads per use (`parser-per-use-grep.txt`) |
| witness-name coverage | parser name set vs every e2e-set name in the `SYMMETRY_LINUX_SUPERVISOR_*`/`SYMMETRY_RUNNER_LEASE_ARMED_*` namespace | 0 | 3 e2e names have no platform reader and must not: `…_STOP_RECEIPT_BARRIER_{REACHED,RELEASE}_FILE` (app `cleanup_glue.go`) and `SYMMETRY_RUNNER_LEASE_ARMED_RESUME_BARRIER_RELEASE_FILE` (`execution/runner.go`) |
| build constraints | `git diff -U0 de526861..95498fdf -- daemon .github \| grep -c '^[+-]//go:build'` | 0 | 2 hits, both `+//go:build linux` on the two new files (expected); 0 elsewhere |
| suppressions | `git diff de526861..95498fdf -- daemon .github \| grep -c 'nolint\|credo:disable'` | 1 (no match) | 0 in code/CI; the 2 whole-range hits are this plan's own prose |
| whitespace | `git diff --check de526861..95498fdf` | 0 | — |
| host path leak | `git diff origin/main...HEAD` plus profile-path / account-name grep over the range diff and the new files | 1 (no match) | 0 hits |

SKIP list, verbatim (identical in both WSL runs): one entry —
`--- SKIP: TestLinuxOwnerAnchorRunReaperRejectsForeignChildBeforeAdoptingIt`
(`linux_owner_helper_v2_linux_test.go:95: requires the test process to run as the installed A UID/GID`).
No new skip is added by this slice; the contract's delegated-cgroup skip does not trigger on this
kernel, and its named `/etc/symmetry/linux-owner.conf` skip is the same pre-existing UID/GID entry.

## Mutation records (each mutant applied to this tree, run in WSL, then reverted; file hashes
re-checked against `pristine-hashes.txt` = OK ×4)

| mutant | edit | test | exit | observed red |
| --- | --- | ---: | ---: | --- |
| M1 process-lifetime cache | parser memoized behind `sync.Once` (Decision F's named failure control) | `TestLinuxSupervisorWitnessConfigIsReadPerUse` + the negative test | 1 | after the gate was cleared the stale config still reads `productionWitness:1 armed:true scanFailureMode:children_after_initial`; both tests FAIL |
| M2 gate removed at the parser | the config's "empty while unarmed" early return deleted | `TestLinuxSupervisorWitnessHooksAreInertWithoutTheGate` | 1 | `gate-unset witness config = {… scanFailureMode:children_after_initial …}`, want empty |
| M3 M2 + hook gate removed | additionally the scan-failure reader's gate check deleted | same test | 1 | reddens at the same config assertion (the hook's own gate is the second layer) |
| M4 validation removed | `validateLinuxSupervisorWitnessMarkerParent` returns nil | `TestLinuxSupervisorWitnessMarkerParentRefusal` | 1 | `publish behind a symlinked parent = <nil>, want the marker parent refusal` |
| M5 always refuse | validation returns the sentinel for every path | same test | 1 | `owned-directory witness marker = … not a safe witness directory`, want published |

M3 notes the layering the implementation chose: because the parser empties the config while the
gate is unset, a *single* hook losing its own gate check cannot act at all — the clause's
invariant ("no witness hook is reachable while the gate is unset") holds one layer earlier than
the per-hook check, and the recorded red mutation is the switch itself (M2), not an inert hook.

## Local e2e oracle status

The containment witness e2e subset cannot run on this host: the tests require `SYMMETRY_E2E=1`
plus a reachable Control plane, and the WSL probe returns HTTP `000` on `127.0.0.1:4000` with
`postgresql inactive` (`environment.txt`). The subset is therefore recorded CI-only, as the
contract's gate line allows. What is verified locally instead:

- the three containment witness e2e files compile against the new platform code:
  `GOOS=linux GOARCH=amd64 go vet ./e2e/` exit 0;
- the platform package's own helper-process tests exercise the changed witness paths in WSL
  (`TestLinuxSupervisorWrongHelloSessionRejectsBeforeTargetExec`,
  `TestLinuxSupervisorOwnerLossProofBarrierIsPostReapAndOptIn`,
  `TestLinuxSupervisorStopWriteFailureAfterHelperExitUsesSealedMirror`,
  `TestLinuxSupervisorScanFailureInjection*`, `TestLinuxSupervisorResponseDrop*`,
  `TestLinuxSupervisorWitnessPublishKeepsDirectoryClean`, plus the three new tests);
- the four PR-gated witness steps: the two Windows ones ran locally green; the two Linux ones are
  CI-only in this stage, which neither commits nor pushes.

### The four PR-gated witness steps

| step | CI coordinate | status in this stage |
| --- | --- | --- |
| daemon-windows "Run production Windows containment witness" | `ci.yml:1988` | run locally, exit 0 (6.80s) |
| daemon-windows "Run production pre-authority crash witness matrix" | `ci.yml:2007` | run locally, exit 0 (49.55s) |
| integration "Run production Linux containment witness matrix" | `ci.yml:2322` | CI-only: needs `SYMMETRY_E2E=1` + a live Control plane and Postgres; the WSL probes return `000`/inactive, so the conclusion belongs to the draft PR |
| compose "Run rootless Compose native refusal witnesses" | `ci.yml:2649` | CI-only: needs the Docker rootless Compose stack (35-minute step); the conclusion belongs to the draft PR |

## Simplification review (repo contract)

- **`armed` field.** Considered dropping it (production never reads it after the early return).
  Kept: it names the switch the tests assert in both directions, and removing it would spread the
  same emptiness assertion across the field list in two tests.
- **Two marker write funnels.** Considered merging `publishLinuxSupervisorWitnessFile` and
  `writeLinuxSupervisorScanWitnessMarker`. Rejected: the publisher's absent-destination
  create-temp+rename contract is what the witness readers rely on, while the scan-fired markers are
  written repeatedly and may already exist; the shared parent validation is the only common
  behavior, and it is factored once.
- **Config struct vs per-reader `os.Getenv`.** Rejected: per-reader reads are the pre-change state
  and contradict the single-switch requirement.
- **No new abstraction beyond the module**: re-measured after the apply-stage simplify pass, the
  net production delta is +125 lines (130-line witness module minus 10 lines removed from
  `containment_supervisor_linux.go` plus 5 in `supervisor_linux_client.go`); the tracked diff is
  +83/−77 (now including the shared counting-fake call-site swap in
  `containment_supervisor_linux_test.go`) and the new test file is +230. D2 keeps dLOC report-only.

## Dependency / rebase note

`origin/main` advanced to `394859a9` (#213 rf-w4-17b, #215 W3-07 residual) while this stage ran.
Neither commit touches `daemon/internal/platform` or `.github/workflows`; both append sections to
`docs/TODO.md`, so the rebase's only expected textual overlap is this slice's EOF section in that
file. The slice's own base stays `12de9428` as dispatched.

## Design trade-offs

- **Snapshot-with-gate vs snapshot-always.** Values-only-when-armed matches the contract's "config
  is empty" wording while remaining behavior-identical, because every consumer checks the gate
  itself (verified in the control-flow audit). The gate's raw value and the `LookupEnv` presence
  flags stay readable so the untrimmed checks and the fail-closed owner-recovery error survive.
- **Entry snapshot vs later intra-call re-read (decision F).** Each hook snapshots the parsed
  config once at its entry; the base code's later intra-call re-reads — the drop-response marker
  path at its publish point (base `supervisor_linux_client.go:1599`) and the release-barrier
  `LookupEnv` pair after the drop check (base `:1627-1628`) — are folded into that entry
  snapshot. Static environments are equivalent (verified per read site); a concurrent in-process
  restore racing an in-flight hook is accepted as the decision-F per-use boundary, not a defect.
- **Trim in the parser vs at the site.** Nothing is normalized in the parser: the drop-response
  operation comparison and the untrimmed gate checks would change behavior under parser-side
  trimming.
- **Validate at write time vs at parse time.** Write-time validation covers every write, including
  the three injection markers that no parser owns, and cannot be bypassed by a parse path that the
  flag later crosses.
- **Two write funnels.** The rename-based publisher and `os.WriteFile` are kept as-is: the
  publisher's absent-destination contract is load-bearing for the witness readers (the CI run
  44133750246 note at `containment_supervisor_linux.go:232-241`), and the scan-fired markers are
  written more than once by design.
- **Evidence carrier.** The STRICT owned set has no `docs/evidence/` file, so the record lives in
  this plan file plus the stage report, following rf-w5-09 (the most recently merged sibling) and
  rf-w2-09's precedent note. Conductor can rule otherwise.

## Out of scope (with reasons)

Conductor decision A/B/F scope, restated: the Windows raw literals and hooks (W6-03);
`daemon/internal/app/linux_owner_witness_linux.go`, `daemon/internal/execution/runner.go`,
`scripts/ci/export-linux-v2-redacted-evidence.py`; the v2 selector/bootstrap path
(`anchor_gate.go`, `helper_v2_*.go`, `anchor_types.go`, D3); the v2 finalize pipeline (W4-05);
`isGoTestBinary` fork removal (W6-03); `cmd/symmetry-daemon/*` internals; CI workflow edits.

- **Clause-1 scope (CR-06 residual).** Marker-parent validation covers the two write funnels in
  the privileged Linux helper (`daemon/internal/platform`). Two daemon-side witness writers still
  publish env-supplied marker paths unvalidated: the stop-receipt barrier reached marker
  (`daemon/internal/app/cleanup_glue.go:232-233` reads
  `SYMMETRY_LINUX_SUPERVISOR_STOP_RECEIPT_BARRIER_{REACHED,RELEASE}_FILE`, written at `:259`
  through `publishLinuxStopReceiptBarrierMarker`) and the commit-resume barrier reached marker
  (`daemon/internal/execution/runner.go:1084-1085` reads
  `SYMMETRY_RUNNER_COMMIT_RESUME_BARRIER_{REACHED,RELEASE}_FILE`, written at `:1114` through the
  publisher at `:1046`). Both are pre-existing, outside this slice's owned set, and recorded as
  residual debt in `docs/TODO.md`.

## Ambiguities resolved

1. **Decision A option (a) rationale (recorded as the contract's clause-2 note).** Option (b) would
   gate the Windows hooks on the argv witness mode propagated to the helper as an explicit launch
   field: a helper-launch protocol change that needs its own pinned test and a witness-reviewer
   note, for a family whose only oracle is the two Windows PR-gated steps. Option (a) keeps the
   frozen helper protocol byte-identical, leaves the Windows files untouched for W6-03 (which
   already owns the same files and dispatches after this slice), and records the residual in
   `docs/TODO.md`. Chosen: (a), as the conductor decided.
2. **"Config is empty" vs the preserved owner-recovery error.** Options: silently disarm the
   marker when the gate is off (behavior change, loses a fail-closed check) or keep base's error
   through the presence flag (stricter, no witness action). Chosen: keep base's error; recorded
   above and in the report.
3. **Evidence record location.** See "Design trade-offs"; the strict owned set decides it.
