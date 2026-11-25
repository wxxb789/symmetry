# rf-opencode-phase1 — OpenCode V2 2.4.1 scoped Phase-1 promotion (Windows surface; Linux fail-closed)

Delivered 2026-11-26 on branch `refactor/rf-opencode-phase1`, base/gate revision
`9eb874bf22eace98399e54b25d00634b2bed5418` (`origin/main`, the merge of
`rf-opencode-230-hostname-redact` #291, the contract's dispatch gate). Contract
`goal-rf-opencode-phase1.md`; accepted plan `docs/plans/2026-11-25-rf-opencode-phase1-plan.md`
(sha256 `701398cd…`); proposal of record `refactor/opencode-phase1-amendment.md` (GDD state root).

No credential, relay host, machine hostname or host absolute path appears here. Receipts are cited
by repository path and sha256, never copied. The amendment **merge** revision is not knowable before
the merge: this record binds the base revision plus the delivered-file digests in §5, and row 6
re-binds or re-runs against the merge revision.

## 1. What was delivered

- Design text P1-P5, verbatim per the contract, with the `Before` spans checked against the base
  revision before editing and the `After` spans independently re-verified byte-exact
  (whitespace-normalized) at the delivered bytes: 7/7 `Before` spans against the base revision,
  7/7 `After` spans at the delivered bytes. `docs/design/harness.md` (OpenCode V2 row + the 2.3.0
  paragraph → 2.4.1 evidence), `docs/goals/0010-refactor-program.md` (D1 row, W6-04 row/bullet),
  `docs/goals/README.md` (two appends), `docs/TODO.md` (two consolidations moved out; the
  F2-exclusion entry re-anchored for the conductor's D3 amendment).
- P6.1 pin + exact-blob gate (`daemon/internal/harness/opencode/adapter.go`): `TestedVersion =
  "2.4.1"`, `TestedExecutableHash` (Windows PE `f3da11bf…`), the evidence-only sibling
  `testedLinuxExecutableHash` (Linux ELF `ec93043f…`), a platform+blob refusal branch on the pi
  shape (`rejectOpenCode` validates the refused projection), the fenced reasons are
  operation-specific on the promoted projection and the promoted keys are removed from the
  `Unsupported` map. The version gate keeps `ErrUnsupportedVersion` **on Windows**; off Windows
  `openCodeVersionRefusal` reports the platform refusal so Linux keeps today's tolerated
  `ErrHarnessUnavailable` class (see §7.5 for the recorded ruling this needs). Launch corrections
  verified against the exact pinned bytes: `--pure` is gone (2.4.1 rejects it, live exit 1), the
  readiness regex accepts the bare `server listening on http://127.0.0.1:PORT` line every 2.4.1
  receipt records, `--port 0` is live-verified (ephemeral port served; the receipts used explicit
  ports), and the probe bound is five seconds (pi's value; the pinned client answers `--version`
  and `serve --help` in ~0.2 s). A canceled/expired probe now returns its seeded projection so the
  daemon reports the real cause instead of an empty capability map.
- P6.2 registry wiring: `harness.NewRegistry()` no longer resolves OpenCode;
  `daemon/internal/app/probe.go` registers `opencode.NewAdapter(profile.Command)` as the other
  native kinds are; the Linux refusal is the probe platform gate with
  `harness.NewOpenCodeAdapter()`'s byte-identical reason sentence.
- P6.3 CI assertions: the six `--assert` rows and the race row are byte-unchanged; they were re-run
  on the delivered bytes (§4). New adapter digest/version refusal rows and truth-table rows carry
  the acceptance.
- P6.4 V1 `1.18.32` drift fix: `scripts/ci/linux-v2-native-tools.sh` (URL → `opencode-linux-x64.tar.gz`
  v2.4.1, archive sha256 `9ddd18e3…`, extracted-ELF sha256 `ec93043f…`, version line
  `opencode v2.4.1`), `scripts/ci/linux-v2-native-acceptance.sh` (`harness_version` map → `2.4.1`),
  `.github/workflows/ci.yml` (folded prepare step: URL, archive name, sha256, extracted-ELF
  assertion, `version_expected`/`version_actual`, executable path). `go-test-json-gate.py` performs
  no version comparison, so the space-bearing provenance values are safe.
- P6.5 witness legs: `daemon/internal/app/linux_owner_witness_linux.go` is byte-unchanged; its
  tested-release check reads `opencode.TestedVersion` and now accepts `2.4.1` (`:3217`); the health
  assertions row 4 binds (`:826,:828-831`) and the no-native-turn-stage refusal (`:2505`) are
  re-quoted unchanged.
- P6.6 TODO consolidations: `daemon/internal/harness/opencode/inprocess_containment_test.go` added
  on the pi shape (observable install/restore); the frozen OpenCode duplicates moved onto
  `harness.IsNilInterface`, `harness.ProcessForwarder`, `harness.DeliverEvent`, `harness.CloseGate`
  and `harness.WaitResult`, exactly as the other native adapters use them.
- Scope extensions outside the contract's owned list (each forced by a clause; see §6):
  `daemon/internal/harness/harness_test.go`, `daemon/internal/harness/opencode/adapter_native_work_linux_test.go`
  + `adapter_native_work_test.go`. The e2e pinned-version map edit was **reverted** (§6/§7.4).

## 2. Acceptance rows

Row 1 (Windows receipts): unmodified Phase-0 bytes under
`daemon/internal/harness/testdata/opencode/2.4.1/` (9 files; `README.md` sha256 `5581c088…`; this
change touches none of them). Their `binding_sha256` is the collector
`087e07a8acaab7c9d07c169acdbd70342e651112e66d0d2a4bdfb3d00c98b0f5` plus `source_subject.py`
`8df46499b267f97b0e309f456bbbf2eec5c5ce98ceef92685d7a49bd798a2f93` and `source_head
49a606dd346fa9ed0a5f1175c784934d11710223`; the route rows this change binds to are the README §4
table (live statuses: `/api/info` 200/401, `/api/health` 404, `/api/model` 200, `/api/event` 200,
session create/prompt/permission/interrupt/delete rows). Re-verified by content, not edited.

Row 2 (exact binary): `TestProbeRejectsWrongVersionAndIncompleteHelp` (a wrong `--version` refuses —
`ErrUnsupportedVersion` on Windows, the platform class off Windows; M2 reddens it),
`TestProbeRejectsWrongBlobDigest` (a foreign blob refuses with `ErrNativeUnverified`; M1 reddens it),
`TestOpenCodeForeignBlobGoalAssignmentFailsBeforeWorkspaceProcessSessionAndEventSideEffects` (a real
assignment under the foreign-blob projection fails at `goal_admission` with the blob reason and
workspace/process/session/event side effects all zero), `TestProbePromotesOnlyTheExactTestedBlobOnWindows`
(promotion on Windows, refusal off Windows), and the six CI assertions incl. the race row (§4).

Row 3 (wiring): `TestNewHarnessRegistryBindsNativeAdapters` asserts `*opencode.Adapter` from the
composition root; `TestComposedOpenCodeProbeStaysFailClosedThroughTheRegistry` pins the composed
native adapter's tolerated refusal and `unavailable` no-operation registration on every platform;
`TestStartupProbeRegistersUnavailableNativeProjections/opencode` keeps the unavailable seam pinned;
`adapter_policy_truth_table_test.go` carries the promoted/refused/foreign-blob rows;
`inprocess_containment_test.go` installs `execution.UseInProcessContainmentSupervisorForTesting` and
restores it after `m.Run`.

Row 4 (Linux containment; standing condition, **recorded unmet**): no dispatched
`linux-native-v2-acceptance` `opencode_native_health` run was attempted — the conductor ruled "no
dispatched Linux run in this goal; do not attempt it" (contract, Conductor review), and no
qualifying-host decision exists. No run id or subject sha256 is recorded; no Linux promotion is
claimed. The witness's session path is blocked by §7.1 until the client re-anchor lands.

Row 5 (pin drift): no `1.18.32` pin literal survives in the changed files; `linux-v2-native-tools.sh`
names `opencode-linux-x64.tar.gz` `9ddd18e3…` → ELF `ec93043f…` → `opencode v2.4.1`; the acceptance
fixture's `harness_version` map names `2.4.1`; the witness tested-release check (`:3217`) accepts
`2.4.1` via `opencode.TestedVersion`.

Row 6 (revision binding): §1 base revision + §5 digest table; a later `main` re-binds or re-runs.

## 3. Mutants (scratch copies, restored byte-identical)

| Mutant | Mutation (scratch) | Check that reddened | Restored |
| --- | --- | --- | --- |
| M1 wrong blob digest | `TestedExecutableHash` ← the Linux hash | `TestProbeRejectsWrongBlobDigest` — `Probe() error = <nil>, want ErrNativeUnverified` | `adapter.go` sha256 `bac1ffd3…ab92` |
| M2 wrong `--version` | the `version != TestedVersion` branch deleted | `TestProbeRejectsWrongVersionAndIncompleteHelp` — `V1_version`, `historical_V1_pin`, `superseded_V2_pin` rows failed with the help/`ErrNativeUnverified` path instead of `ErrUnsupportedVersion` | `adapter.go` sha256 `bac1ffd3…ab92` |
| M3 Linux admission forced to promote | platform gate condition forced false | `TestProbeRecordsExactTransportButRemainsUnverified` (Linux branch), `TestProbePromotesOnlyTheExactTestedBlobOnWindows` (Linux branch), `TestAdapterPolicyTruthTablePromotedOpenCode` (Linux branch) all failed with `Probe() error = <nil>` / blob reason, want the Linux platform refusal | `adapter.go` sha256 `bac1ffd3…ab92` |

The compose refusal rows are constructor-seam assertions (`NewOpenCodeAdapter()`), so a gate
mutation cannot redden them; they are not locally runnable without a compose host. Recorded, not
silently skipped.
The restored digest is the file's digest at mutation time; two later comment-only trims moved
`adapter.go` to its delivered digest (§5), and the post-trim Windows/Linux suites were re-run.

## 4. Gates (exact commands; cwd; exit)

| Command | cwd | Exit |
| --- | --- | --- |
| `gofmt -l .` | `daemon/` | 0 (empty) |
| `go vet ./...` | `daemon/` | 0 |
| `go test -count=1 ./internal/harness/opencode/ ./internal/harness/ ./internal/app/` | `daemon/` | 0 (app 154 s) |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...` (WSL `Ubuntu-24.04`) | `daemon/` | 0 |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` (WSL) | `daemon/` | 0 |
| `go test -count=1 ./internal/harness/opencode/ ./internal/harness/` (WSL) | `daemon/` | 0 |
| the six `--assert` rows via `go-test-json-gate.py --stdio-boundary` (WSL) | `daemon/` | 0 — all 7 rows `run=1 pass=1 skip=0` |
| the race row (`--assert … TestOpenCodeV2VerifierRejectsReleasedPeer -- go test -race -count=1 -timeout 600s -json ./...`, WSL) | `daemon/` | 1 — see the note below; the named assertion row is green and the touched packages pass |
| `bash -n scripts/ci/linux-v2-native-tools.sh scripts/ci/linux-v2-native-acceptance.sh` | repo | 0 |
| `python3 -c "import yaml; yaml.safe_load(…ci.yml)"` (WSL) | repo | 0 |
| `python3 scripts/ci/test-linux-v2-evidence.py` (WSL) | repo | 0 — 55 tests OK |
| docs P1-P5 whitespace-normalized check (Before vs base `9eb874bf`, After at delivered bytes) | repo | 0 — 7/7 + 7/7 |
| mutants M1/M2 (Windows) and M3 (WSL) per §3 | `daemon/` | expected red; restores verified |

A 600 s `-race` attempt before the two fixes above failed on this WSL/9p host at the `internal/app`
package boundary with a probe-deadline flake (the masked-empty-projection defect fixed in §1); the
row above is the honest re-run on the frozen bytes. On this host the row cannot reproduce the
CI runner end to end, because the witness suites it contains demand provisioned environments:

- `internal/platform` and `internal/launchercore` fail as root (`wire harness requires an
  unprivileged A process`) and as an unprivileged user (`requires the test process to run as the
  installed A UID/GID`, `cleanup lock proof requires the explicit installed-owner scratch
  environment as root`); `internal/workspace` fails without `SYMMETRY_SUBJECT_WITNESS_*`;
  `internal/linuxpreflight` needs the legacy-owner store environment. All four fail identically at
  base `9eb874bf` under the same unprivileged identity (34 test reds; `platform` 102.0 s,
  `launchercore` 0.0 s, `workspace` 36.6 s, `linuxpreflight` 0.2 s), so they are environment-gated,
  not regressions.
- `internal/app`: 551.2 s pass inside the unprivileged full row, 526.9 s pass alone; base `9eb874bf`
  alone 570.3 s. The one 600.1 s red above ran concurrently with two other suites on the same host,
  which is why it crossed the 600 s budget.
- `TestOpenCodeV2VerifierRejectsReleasedPeer` `run=1 pass=1 skip=0`, and `harness`/`opencode` are
  green, in both identities.

## 5. Delivered-file sha256 equality table

Computed at the delivered bytes after the final gate sweep (the record itself is omitted by
construction; the PR merge revision re-binds them, row 6).

| File | sha256 |
| --- | --- |
| `.github/workflows/ci.yml` | `96c25e4c9eb27c66f9142b305385459d00a536b1e4fc63afe353515c0e86ccb5` |
| `daemon/internal/app/adapter_policy_truth_table_test.go` | `c77a352e29f1edd9b07bbcfcaf58bf8280761fc134d5d8b2d763811fb9eb06d7` |
| `daemon/internal/app/harness_test.go` | `1b975be94686cd656d172fc1588d6a773b379ab9e1d826ee608bd8c4e0fd4f22` |
| `daemon/internal/app/probe.go` | `e8681b18a49070617765d804b9c9ad6e4aac1798716208215137c081f4daba65` |
| `daemon/internal/harness/harness_test.go` | `3cb64f1c20af0d5063272e63e1a4ae2f9751687826b67c25ff060ce55cc76759` |
| `daemon/internal/harness/opencode/adapter.go` | `873b8655510d6e711a13f4614a851870bb707982eca223c7f128557ad6aef59b` |
| `daemon/internal/harness/opencode/adapter_native_work_linux_test.go` | `89541009b365a6163a52898ce704a5dc65814a50bed63b9cfd65c60174d08c40` |
| `daemon/internal/harness/opencode/adapter_native_work_test.go` | `5ba83f16505e0f2bae2a85adf2f8e55a62f7360bf624fee5d9a24b025314eda9` |
| `daemon/internal/harness/opencode/adapter_test.go` | `fd066dd9841071ee84d838bc24a4279352bd23df980bc745ac5a7fd02e264162` |
| `daemon/internal/harness/opencode/inprocess_containment_test.go` (new) | `9f3bdff150af99fc3775468105ed4270261ced19ef0c20b6ba19382275ace7d7` |
| `daemon/internal/harness/registry.go` | `c8049e6eced97a11d325b78aff7ec9c6aa77102f1397f799164a8646d6e2ddcc` |
| `docs/TODO.md` | `89d147c7df18f33f15bdb63daca12dab207dc44b8f626a955299fe2a2f7e17cb` |
| `docs/archive/CLOSED-TODO.md` | `c65a474ef7ecd6d54abeaec2d2013d25358032bdc835cca2d7031e2f5e6d9a2a` |
| `docs/design/harness.md` | `7369e7570d5a61cf366b6d42188da3d0fe9b5f7514e868175def634ded012667` |
| `docs/goals/0010-refactor-program.md` | `49a9f0335020773716d2f884bd6f904fb78c81bfa2b63ba72ae3b1d24be22e37` |
| `docs/goals/README.md` | `5f2a6292408648d87d50f78a07d3c4c623bb3f87fa49ee22b4bf377e7bc6edd4` |
| `docs/plans/2026-11-25-rf-opencode-phase1-plan.md` (new) | `300ed1a7e5647f284097faf89621a4b8d8e0c46b430b00fbd717cfdcf0a9de85` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `6e623ca08a8753eb0be647e44a5bb54e0459e24cedd2a2523e0cdaf4b2b5cfa1` |
| `scripts/ci/linux-v2-native-tools.sh` | `500ac7b47dd9a9b0f5aea2d67dafe4a0131dfd10726b0dc4b0215f4f1b410c69` |

## 6. Scope notes (files outside the contract's owned list)

- `daemon/internal/harness/harness_test.go` — forced by P6.2's "stops resolving OpenCode"; the test
  registers `NewOpenCodeAdapter()` explicitly, no assertion changed.
- `daemon/internal/harness/opencode/adapter_native_work_linux_test.go` +
  `adapter_native_work_test.go` — forced by P6.4: the folded CI leg now installs the 2.4.1 ELF and
  runs `TestLinuxLegacyOpenCodeRefusesBeforeHealthRequest`, whose helper must verify the binary it
  is handed. `historicalNativeTestVersion` stays for the historical V1 tests.
- `daemon/e2e/production_linux_v2_native_witness_test.go` — the pinned-version map edit was made and
  then **reverted**: the contract names `daemon/e2e/**` byte-unchanged and no required row cites it.
  The one-line map change (`{"1.18.32","1.18.32"}` → `{opencode.TestedVersion, "opencode v"+…}`) is
  a recorded carry for row 4's dispatch (§7.4); row 4 is unmet either way.
- `daemon/internal/app/linux_owner_witness_linux.go`, the compose witness and
  `scripts/ci/rootless-compose-native-acceptance.sh` are byte-unchanged.

## 7. Unclosed items (verbatim) and residual risk

1. **The adapter's transport still speaks the pre-2.4 route set** (discovered here; the smallest
   proposed amendment). Verified against the committed receipts and live: `GET /api/health` is 404
   on the exact 2.4.1 bytes while `client.go:116-117` calls it and `openOnce` fails the session on
   its error; `GET /api/session/{id}/event?after=0` is 404 while `client.go:226` uses it and
   `StartTurn` fails the turn; `POST …/interrupt?resume=false` answers 200 `{"interrupted":true}`
   while `client.go:198-202` expects 204 with an empty body; `POST /api/session` returns a
   server-assigned `ses_…` id and `POST …/prompt` takes a flat `text` member while the adapter's
   decode expects its own id and a nested `prompt.text`. Consequence: on Windows with the exact
   blob the probe promotes, the control plane schedules onto the advertised runtime, `Start`
   launches a real server, and `Open` fails at the health call — today's clean refusal before
   workspace/process becomes an advertised-then-dead run. The smallest amendment: re-anchor
   `client.go` (+ its fixtures) to the re-anchored route set, keep every fence, and gate acceptance
   on a live E2E against the digest-pinned PE (Start → Open → StartTurn). Row 4's witness drives
   the same session path (`linux_owner_witness_linux.go:3407`), so the amendment precedes any
   dispatch. Two further gaps belong to it: the adapter never implements the contract's own
   readiness-identity invariant (no `/api/info` call checks `pid == server pid` and
   `version == "2.4.1"`), and the `PromptAdmission`/per-session SSE cursor model is a 2.3-era
   fiction the global `/api/event` stream does not carry.
   **Merge disposition:** the registration flip must not reach `main` without that re-anchor or an
   explicit conductor acceptance of an advertised-but-dead Windows surface; this stage ships the
   ruled rows and escalates, it does not authorise the merge. A cheaper in-scope option if the
   conductor prefers it: keep the pin/gates/registration but return a fail-closed projection (no
   Start/Events/Cancel) until the re-anchor lands.
2. Row 4 remains unmet: no dispatched `opencode_native_health` run, no run id, no subject sha256; no
   Linux promotion claimed; the e2e pinned-version carry of §6 belongs to that dispatch.
3. The D3 text re-anchor is the conductor's (`refactor/d3-amendment-draft.md` §1 P7); this goal owns
   only `docs/TODO.md`'s F2-exclusion text update.
4. The compose refusal rows (`daemon/e2e/production_linux_v2_rootless_compose_witness_test.go:285-289,556`;
   `scripts/ci/rootless-compose-native-acceptance.sh:112,355,411`) are byte-unchanged and stay
   green: they exercise the `harness.NewOpenCodeAdapter()` constructor seam, whose sentinel,
   projection shape and reason sentence are unchanged. They cannot detect the registry-path change
   — `TestComposedOpenCodeProbeStaysFailClosedThroughTheRegistry` is the new registry-path pin.
5. **P6.1 vs the conductor review on the version gate (needs a recorded ruling).** P6.1 says the
   version gate keeps its `ErrUnsupportedVersion` contract; the conductor's review says Linux keeps
   exactly today's `ErrHarnessUnavailable` classification and reason class. Both cannot hold on the
   Linux registry path. Resolved toward the conductor review: off Windows every version-class
   refusal is the platform refusal, so an absent, wrong-version or exact Linux binary all stay
   tolerated; Windows keeps `ErrUnsupportedVersion` and M2 still reddens the version rows there.
   Recorded here as the escalation the contract asked for rather than silently implied.
6. Residuals: the digest binds the executable at probe time, not at launch (the pi/codex shape; a
   local writer could swap the blob between startup and assignment); on a Linux host with an
   installed binary the runtime registers the probed `harness_version` with `adapter_version`
   `unavailable` and zero operations (pi's shape; the compose fixture installs none, so its
   asserted row stays `unavailable`/`unavailable`); `--port 0` is live-verified but absent from the
   committed receipts; the `NewOpenCodeAdapter()` seam sentence names the Windows-only promotion
   even where the seam is reached on Windows in tests (it is not a production path there). None of
   these weakens a promoted operation.
7. Fenced, unchanged: truncated responses stay `unavailable` (no schema bound), usage stays
   unreconciled, and SSE reconnect/replay, retained-session recovery, resume, handoff,
   guidance/steer, safe pause, the approval bridge, hard cost limits and provider brokering stay
   unsupported; Linux containment is not promoted by any Windows or WSL2 receipt; V1.x stays skipped.

## 8. Review corrections (2026-11-26, before the final gate sweep)

Three read-only reviews ran on this diff (Sol review, security pass, model-diverse audit); their
findings changed these bytes:

- Sol review P3: the e2e pinned-version hunk was reverted (non-goal); the promoted projection now
  deletes the promoted keys from the `Unsupported` map; the refusal paths validate their projection;
  the readiness widening gained `TestReadinessAcceptsBothRecordedListeningLines`; the
  non-Windows probe row uses `!= "windows"`; the version-sentinel split is escalated (§7.5).
- Security pass: the promoted projection now carries operation-specific fenced reasons (handoff,
  guidance, pause, approval response, usage); the new app test drives the full assignment path and
  asserts zero side effects for the foreign-blob projection; the digest-at-launch residual is
  recorded (§7.6).
- Model-diverse audit: the Linux parity gap was closed (`openCodeVersionRefusal`, a registry-path
  pin via `TestComposedOpenCodeProbeStaysFailClosedThroughTheRegistry`), the record's own
  self-contradiction and stale digest table were fixed in this rewrite, and the merge disposition
  was made explicit (§7.1).
- Root-cause fix from the first race attempt: a probe deadline returned an empty capability map,
  which the daemon reported as "capability kind must not be empty"; the seeded projection is kept,
  and the probe bound follows pi (five seconds).
- Comment-only trims after those reviews: three new comments exceeded the contract's two-line
  bound and were shortened; the post-trim Windows and Linux suites and the six assertion rows were
  re-run on the delivered digests, and the §5 table lists those digests.
