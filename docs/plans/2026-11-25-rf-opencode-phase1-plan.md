# rf-opencode-phase1 implement plan (2026-11-25)

Goal contract: `goal-rf-opencode-phase1.md` (GDD contract store, ruled L1;
gate: `rf-opencode-230-hostname-redact` merged as `9eb874bf`, `origin/main` == HEAD at start).
Proposal of record: `refactor/opencode-phase1-amendment.md` §1 P1-P6 (GDD state root).
Design text P1-P5 is applied verbatim from the contract; no wording changes.

This plan covers only what the accepted plan leaves open: the exact wiring shape, refusal
sentinels/reasons, the mechanical pin moves outside the owned file list that the contract's own
acceptance rows force, the two TODO consolidations, and the evidence plan.

## 1. Exact-binary gate (P6.1)

`daemon/internal/harness/opencode/adapter.go`, on the `pi/probe.go` shape:

- `TestedVersion = "2.4.1"`.
- `TestedExecutableHash = "sha256:f3da11bf…0e"` (Windows PE, the promoted blob) and the
  evidence-only sibling `testedLinuxExecutableHash = "sha256:ec93043f…52"` (Linux ELF). Both
  verified live on 2026-11-26 against `v2.4.1` (`opencode-linux-x64.tar.gz` `9ddd18e3…` → extracted
  `ocx/opencode` `ec93043f…`, `--version` `opencode v2.4.1`).
- Probe order after the existing `--version`/`serve --help` checks: resolve the executable digest
  (`harness.ResolveExecutableDigest`, production `osCommandRunner` gains `ExecutableDigest` like pi),
  then refuse: platform != windows → `ErrHarnessUnavailable` (see §2); digest unknown or !=
  Windows pin → `ErrNativeUnverified`. Promotion on `windows` + digest match:
  `Verified`, `Start`, `Events`, `Cancel`, `ProtocolVersion: 1`,
  `ImplementationVersion = openCodeImplementationVersion = "opencode-http-v2"` (matches the
  `pi-rpc-v1`/`codex-app-server-v1`/`omo-rpc-v1` naming), plus explicit `Unsupported` entries for
  provider access, resume, handoff, guidance, pause, approval response, usage and hard cost limit,
  using the same reason sentences `Adapter.Start` already emits for those refusals.
- The version gate keeps `ErrUnsupportedVersion`; the digest gate keeps `ErrNativeUnverified`.
  Neither touches a workspace or a process: only `--version`/`serve --help`/hashing.

## 2. Registry wiring (P6.2) — decision the contract left implicit

The native constructor is registered from the composition root **on every platform**, exactly as
`pi`/`codex`/`omp`/`omo` are (`daemon/internal/app/probe.go` `newHarnessRegistry`); Linux
fail-closed comes from the **probe platform gate**, not from a platform-conditional registration:

- `harness.Kind.UnavailableProbe()` is already true for `opencode`, so an `ErrHarnessUnavailable` probe
  error is tolerated as the fail-closed projection (`ensureHarnessProbe`), and the Linux daemon
  still registers `harness_version`/`adapter_version` `unavailable` with no operations — byte-equal
  to the refusal `daemon/e2e/production_linux_v2_rootless_compose_witness_test.go:285-289` and
  `scripts/ci/rootless-compose-native-acceptance.sh:355,411` already assert (they call
  `harness.NewOpenCodeAdapter()` directly and stay untouched).
- The Linux native platform gate returns the same `harness.ErrHarnessUnavailable` classification
  and the same reason class as `harness.NewOpenCodeAdapter()` (both name the pin, the Windows-only
  promotion and the dispatched `opencode_native_health` condition), so a Linux host with an
  installed 2.4.1 binary still gets today's sentinel.
- Off Windows the platform refusal also carries every version-class refusal
  (`openCodeVersionRefusal`), so an absent, wrong-version or exact binary all keep today's
  tolerated `ErrHarnessUnavailable` class; only Windows keeps the `ErrUnsupportedVersion`
  daemon-startup contract. On a Linux host with an installed binary the runtime registers the
  probed `harness_version` with `adapter_version` `unavailable` and zero operations (the compose
  fixture installs none, so its asserted row stays `unavailable`/`unavailable`); the registered
  *unavailable* metadata below holds for the no-executable case.
- Rationale: the contract's acceptance row 4 and P1's "Linux admission … flips mechanically"
  require `daemon.harnessRegistry.Lookup(KindOpenCode)` to reach a session-capable adapter on
  Linux (the witness at `daemon/internal/app/linux_owner_witness_linux.go:3407` starts the real
  session); `requiresLinuxV2NativeOwner` (`native_run.go:144`) already codes opencode as a Linux v2
  owner kind, and legacy Linux assignments already fail at `linux_v2_required`
  (`app.go:1416`). The unavailable adapter would make the row unreachable forever.
- `harness.NewRegistry()` stops registering `KindOpenCode` (P6.2). Consequence owned here:
  `daemon/internal/harness/harness_test.go` (`TestRegistryKeepsUnavailableCapabilitiesExplicit`)
  registers `NewOpenCodeAdapter()` explicitly, every assertion unchanged — a mechanical consequence
  of P6.2, flagged in the record because the file is outside the contract's owned list.
- `harness.NewOpenCodeAdapter()` stays as the explicit fail-closed projection (compose rows, e2e
  seam, tests); its reason moves to the 2.4.1/Linux text.

## 3. Mechanical pin moves the contract's rows force outside the owned list

- `daemon/internal/harness/opencode/adapter_native_work_linux_test.go` (+ the
  `nativeOpenCodeRepositoryTaskCheckVersion` helper in `adapter_native_work_test.go`): the Linux
  refusal leg must verify the binary it is handed, so the check takes the expected version line;
  the historical V1 tests keep `historicalNativeTestVersion` unchanged.
- `daemon/e2e/production_linux_v2_native_witness_test.go` `linuxV2PinnedNativeVersions`:
  `opencode` → `opencode.TestedVersion` / `"opencode v" + opencode.TestedVersion`. Without this the
  dispatched row 4 can never bind `opencode v2.4.1` (row 4's own acceptance text) while row 5
  requires the fixture to name 2.4.1. No other e2e byte moves.

## 4. TODO consolidations (P6.6)

- `opencode/inprocess_containment_test.go` added on the `pi` shape; both TODO entries move to
  `docs/archive/CLOSED-TODO.md` with a closure note naming this goal and the new file.
- The frozen `opencode` duplicates move onto the shared helpers (`harness.IsNilInterface`,
  `harness.ProcessForwarder`, `harness.DeliverEvent`) exactly as `pi`/`codex`/`omp`/`omo` already
  do; `Wait`/Close join behavior is unchanged (the shared surfaces do not cover those, so only the
  listed duplicates collapse).
- `docs/TODO.md:1911-1916` F2-exclusion entry: text update only, owned by this goal (the D3
  re-anchor text belongs to the conductor's amendment).

## 5. Evidence plan

`docs/evidence/rf-opencode-phase1-2026-11-25.md` records: base/head revisions, the P1-P5 before/after
verification method (whitespace-normalized byte checks), per-row commands with exit codes, the
delivered-file sha256 equality table, mutation rows M1-M3 (scratch copies restored byte-identical,
each naming the check that reddens), row 4 recorded unmet with the reason (conductor: do not
attempt), and the unclosed items verbatim. No credential, relay host, machine hostname or absolute
host path; collector bytes `087e07a8…` and `source_subject.py 8df46499…` are cited by value.

## 6. Gates (run after every slice lands; never concurrent with a measurement harness)

- From `daemon/`: `gofmt -l .`; `go vet ./...`; `go test -count=1 ./internal/harness/opencode/ ./internal/harness/ ./internal/app/`.
- Linux rows via `wsl.exe -d Ubuntu-24.04`: `GOOS`-native `go vet`/`go build ./...` and
  `go test -count=1 ./internal/harness/opencode/ ./internal/harness/`.
- The six CI `--assert` invocations and the race row from `.github/workflows/ci.yml:1081-1104`
  locally (Linux rows via WSL where the tests are `//go:build linux`).
- `bash -n` on changed scripts; `python3 scripts/ci/test-linux-v2-evidence.py`.
- Docs: P1-P5 spans byte-exact before edit (whitespace-normalized), anchors re-measured by content.
- Mutants: M1 wrong blob digest; M2 `--version` 2.3.0; M3 Linux admission forced to promote.
- Review: Sol review + DS audit over the diff; `security-reviewer` pass because the diff moves the
  refusing sentinel and the registry registration.

## 7. Out of scope (reaffirmed)

No second amendment; row 4 not attempted; no capability text for the fenced list; no change to the
2.3.0/2.4.1 testdata bytes, the witness collector/suite, the D3 design text, the rootless-compose
script, or the 2.3.0 receipts' digests; no WSL2-as-Linux-containment claim.

## 8. Discovered conflict: the adapter's transport still speaks the pre-2.4 route set (escalation)

While implementing P6.1/P6.2 the following was verified against the committed 2.4.1 receipts and
live against the pinned Linux ELF (WSL) — and it disproves the transport assumption behind P1/P2's
"the Windows surface promotes":

- `GET /api/health` is **404** on the exact 2.4.1 bytes (every receipt; `2.4.1/README.md` §4), while
  `client.go:117` `Health` calls it and `adapter.go` `openOnce` fails the session when it errors.
- `GET /api/session/{id}/event?after=0` is **404** (every receipt); the global `GET /api/event`
  carries the events. `client.go:226` `OpenSessionEvents` uses the per-session route and
  `StartTurn` fails the turn when it cannot open.
- `POST /api/session/{id}/interrupt?resume=false` answers **200 `{"interrupted":true}`**;
  `client.go:202` expects **204** with an empty body.
- The pinned bytes print `server listening on http://127.0.0.1:PORT` (the adapter's regex required
  the `opencode ` prefix) and reject the retired `--pure` flag (live exit 1) — both are now fixed in
  `adapter.go` so the launch and readiness bind the exact release; the route/shape defects live in
  `client.go` and remain.
- `POST /api/session` returns a server-assigned `ses_…` id and `POST …/prompt` takes a flat
  `text` member; the adapter's create/prompt decode expects its own id and nested `prompt.text`.

Consequence if the contract's rows ship unchanged: on Windows with the exact blob the probe
promotes, the daemon admits an assignment, launches the server, and fails at `Open` — today's clean
refusal before workspace/process becomes an admitted-then-dead run. The six CI assertions and the
compose rows are route-agnostic and stay green either way.

Options: (A) ship rows 2/3 as ruled and escalate the transport gap as an unclosed conflict;
(B) re-anchor the transport inside this change (`client.go`, its fixtures, StartTurn's stream and
create/prompt shapes) — a fresh amendment's worth of live re-derivation, with `client.go` outside
the contract's owned set; (C) hold the Windows registration and the P1/P2 sentences — fails
acceptance row 3 and contradicts the conductor's verbatim-text ruling.
Recommendation and choice: **A**. Rows 2/3 are explicit acceptance clauses; the conductor ruled the
text verbatim; the owner's L1 ruling rejects "Windows stays dark with complete evidence". The
conflict, the live probes, and the smallest amendment (client re-anchor, acceptance = a live E2E
against the digest-pinned PE) are recorded in the evidence record §"Unclosed items" and the stage
report; row 4's premise is affected too, because the witness drives the same session path.
The record states the merge disposition explicitly: **the registration flip must not reach `main`
without the client re-anchor or an explicit conductor acceptance**, because the promoted projection
advertises `start/events/cancel` while the first `Open` call cannot survive the 2.4.1 route set;
this stage ships the ruled rows and escalates, it does not authorise the merge.
