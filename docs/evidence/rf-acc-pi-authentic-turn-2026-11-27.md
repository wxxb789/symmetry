# rf-acc-pi-authentic-turn — evidence record (stage 2: named credential-gated pi legs and the bounded redacted provider stop)

Goal `rf-acc-pi-authentic-turn` (contract under the GDD contracts root, ruled by the conductor
2026-11-26; dispatch gate #303) plus the owner rulings of 2026-11-27 (1B1, 2C1, 3A3, 6G1). Branch
`refactor/rf-acc-pi-authentic-turn`, dispatch base `0a2f15efb1fd3928e88be27666e7ec9f738df0a2`
(`origin/main` `d45e3298` merged). Plans: `docs/plans/2026-11-26-rf-acc-pi-authentic-turn-plan.md`
(stage 1) and `docs/plans/2026-11-27-rf-acc-pi-authentic-turn-stage2-plan.md` (this stage). Stage-1
root cause, reused here and not re-measured: the fixture-staged placeholder credential, the
provider's HTTP 401, and pi's `stopReason:"error"` message
(`docs/evidence/rf-acc-pi-authentic-turn-2026-11-26.md` §2–§4). This record supersedes the
apply-stage gate receipt for the files changed in review round 1. The source digests below bind
the repaired bytes; the external handoff receipt binds the scratch commit including this record.

## 1. Delivered bytes

| File | sha256 (delivered) | Change |
| --- | --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `066d4062239f02e47a142bd83cddae8790624bf7a7eeb3a5a4cc252941fa8` | The fixture owns placeholder detection and gated leg ordering, passes the exact Go package to the event gate, and writes the source digest into the declaration. |
| `scripts/ci/assert-go-test-json.py` | `c249020da047104508dc9a43aee859979290097c1a6fa46a4664aba79e4f838e` | Gated tests require exact test and package failure terminals; the ordinary exact-pass gate is unchanged. |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `e5274c43e2d36195971359e5fd89790711d2b1e05e6983fc0edd9a0038ecfa08` | The declaration binds the source tree. Published gated evidence must match the Pi provider failure identity; crash-leg documents are forbidden in gated mode. |
| `daemon/internal/harness/pi/rpc.go` | `d785c8c34d32af0c265b6933262b320133bc006a39b0a05e8ae91507b488e58f` | The suffix carries only a fixed marker and an optional three-digit status. The sentinel and stop reason are unchanged. |
| `daemon/internal/harness/pi/rpc_test.go` | `fe8cd7f9c1a3d35bd2dd40cf472e33084d3b4c9c6422167addb50556484ecae0` | Validator rows pin the measured 401, reflected credential probes, bounded output and unchanged stop shapes. |
| `scripts/ci/test-linux-v2-evidence.py` | `11fcbed87b4b963568e51a50a908a42ac912fa174039d0d17c6362915742fb78` | The fixture tests cover both shared-arm modes, its real tail and gated runner logic. Exporter rows pin subject and failure identity. |
| `scripts/ci/test-linux-v2-go-events.py` | `1ff5ece7ee62035029fe204294d72f5316f9242c7fdc5278c61a632d651c48ef` | Completed gated streams, incomplete and foreign events, and unchanged default strictness. |
| `docs/plans/2026-11-27-rf-acc-pi-authentic-turn-stage2-plan.md`, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | the accepted stage-2 plan; the stage-1 items move to the archive and the credentialed-proof residual stays open |

The pre-apply digest for `scripts/ci/test-linux-v2-evidence.py`,
`e32973b02a11ef0fba296c0d6da519de1a91b524101d1af1a9b7810def71d2e8`, bound pre-normalization bytes:
the conductor converted 2665 CRLF pairs to LF before committing `829c93bd` (the file's `text=auto`
attribute had let the `eol=lf` step pass it through), so it bound no committed revision; the row
above binds the round-1 LF bytes.

## 2. Slice 2 (1B1): the placeholder mode and the named outcomes

### 2.1 The mode is decided from the staged bytes, in the fixture

`detect_placeholder_provider_credential` (`linux-v2-native-acceptance.sh:403`) compares
`SYMMETRY_OPENAI_API_KEY` with the ratified scratch placeholder (`sk-scratch-dummy`) immediately
before the fixture writes that same value into `native_api_key_file` and B's
`~/.pi/agent/auth.json` (`:409-411`). Any other value, including every real secret, leaves
`placeholder_provider_credential=0` and every path below byte-identical to the dispatch base. The
daemon never reads the credential to classify a stop.

### 2.2 Which legs are gated, and why `pi_provider_action` runs last

The three legs the fixture marks as provider-receipt-required (the `1` in the receipt-flag column
of `owner_witness_case_identity`, `:2419-2434`) need a successful model turn: `pi_provider_action`
asserts `provider_action_succeeded` in its own witness response, and both detached-child legs
additionally need the child that only the model's
`resource.sync` plus commit can produce (`:959-961` writes `post-commit-child.pid` from the
`post-commit` hook). Ruling 1B1's purpose clause — the export and `stop_readback_reader` becoming
measurable again — needs all three to stop aborting the set, because the exporter's pi-durability
check requires every other allowlisted witness (`export-linux-v2-redacted-evidence.py:1515`) and
the reader's single call site is after the whole `case` block (`:3361`).

Under the placeholder the arm records `credential_gated_helper_sigkill_detached_child` and
`credential_gated_daemon_sigkill_helper_survives_detached_child` in place of those legs
(`:3289-3297`) and still RUNS `pi_provider_action`, at the arm's tail (`:3318-3320`), because its
aborted run retains an unreleased `RunJournal` in the single installed owner StateDir every
production witness shares with it, and
`newLinuxV2NativeFixture` refuses any fresh production witness while a prior journal is not
physically released (`daemon/e2e/production_linux_v2_native_witness_test.go:2087-2100`; the same
guard is why the alternate-mount refusal leg owns a case set, `docs/TODO.md`). The deferred leg is
the only way to keep the contract's gated pi leg measurable without fencing the remaining legs; no
other leg depends on its position (each leg has its own Control claim, request, event file and
result row).

### 2.3 What a reviewer sees, in log, declaration and exported evidence

- Log: one line per gated leg —
  `credential-gated: <case> (credential_gated_<case>) is NOT proven: the ratified placeholder
  provider credential cannot produce an authentic Pi provider turn, so this leg is not counted as
  passed.` — plus the deferral line at the leg's original position and the closing
  `… completed with 3 credential-gated authentic-provider legs (…)` line before `exit 1`.
- The gated leg's event gate row reads
  `credential-gated TestProductionLinuxV2PiNativeProviderAction: run=1 pass=0 skip=0`, never
  `verified … pass=1`; the empty-column form does not exist.
- Exported evidence: `credential-gated-legs.json` (the fixture declaration, mode 0600) with the
  mode, the credential variable name, the fixture's source-tree digest, and the three
  `case`/`test`/`outcome` rows; the manifest's
  `credential_gated` object; and `missing_allowlisted_files` naming the two unrun crash witnesses
  and the three Control provider receipts that a placeholder run cannot produce. A gated run can
  never export `status: "passed"`, and the case set never prints its success line. With the
  declaration, a stale document from either unrun crash leg cannot be published. If present, the
  provider-action document must identify Pi, the exact test and source tree, and report a boolean
  false for `provider_action_succeeded`; a matching filename or a success-shaped document is
  not enough. This is a failure artifact, not proof of the staged executable or a live turn.

### 2.4 The real-credential path is unchanged

With a non-placeholder credential the fixture runs all three legs in their original positions
with the original go-test mode. It writes no declaration, so the exporter uses the original
completeness and Pi witness binding. The projected daemon stop diagnostic in §3 applies to any
Pi error stop, including a failure in the later credentialed `main` dispatch.

## 3. Slice 3 (2C1): the bounded, redacted provider stop

`normalAssistantStop` still rejects a non-normal stop with `ErrUnexpectedAssistantStop` and keeps
the existing quoted stop reason. For a nonempty string `errorMessage`, the only copied bytes are
three ASCII digits from an exact leading HTTP error prefix (400–599 followed by `: `). All other
bytes are dropped, not scrubbed. The largest diagnostic is 28 ASCII bytes, 46 with the appended
label. A usable message without that prefix gets only `[details redacted]`; malformed or empty
fields leave the old text intact. The status is a hint from the provider, not an independently
verified HTTP result. No response body, header, error class or arbitrary token survives; this
deliberately loses the ability to diagnose a specific auth failure or transport cause from text.

Exact expectation in `TestValidatorReportsProviderStopError/provider_401`:

```text
pi rpc final assistant message did not stop normally: "error": provider error: HTTP 401; [details redacted]
```

The new contribution is already bounded at its origin, before it goes to any consumer:

- Pi carries the same suffix in local `TaskResult.Summary` and the error returned by `WaitTurn`,
  without emitting a semantic `task_result` event on this path
  (`daemon/internal/harness/pi/adapter.go`). The unchanged quoted reason is separately bounded
  by the RPC record limit and Go's escaping; it is not part of the 46-byte limit.
- The daemon copies the same bounded suffix into terminal payload `summary` and (on the wait
  error path) `error`, including the persisted native-usage terminal-recovery payload and the
  RunJournal's terminal-transition/outbox data. Replays use that payload; JSON escaping does not
  introduce any provider bytes. See `daemon/internal/app/goal_delivery.go`,
  `daemon/internal/app/native_run.go` and `daemon/internal/app/outbox.go`.
- Control can durably store those same fields in run failure, task failure and transition history
  (`control/lib/symmetry_control/orchestration/transitions.ex`). Each copy carries at most the
  same 46-byte diagnostic. The witness CLI wraps the same bounded error in its stderr; the e2e
  copies it into private Go events and the fixture can print its filtered tail, not upload the
  raw captures as evidence. The wrapper prefixes are not provider text.

Amendment A1 keeps this text out of the Bind acceptance report: the validator row supplies the
proof, not a guest log line.

## 4. Measured rows (command → exit code)

| Command (working directory) | Exit | Note |
| --- | --- | --- |
| `go test ./internal/harness/pi/... -count=1` (`daemon/`) | 0 | Windows and WSL Noble |
| `go test ./internal/app -run '^(TestNativeTerminalPayload\|Test.*OutputSettlement)' -count=1` (`daemon/`) | 0 | Windows, targeted terminal-payload consumers |
| `gofmt` + byte comparison (two changed Go files) | 0 | no diff |
| `go vet ./internal/harness/pi/... ./internal/app`; `go vet ./internal/harness/pi/...` | 0 each | Windows and Noble respectively |
| `python3 scripts/ci/test-linux-v2-go-events.py` | 0 | 10 tests, WSL Noble |
| `python3 scripts/ci/test-linux-v2-evidence.py` | 0 | 67 tests, WSL Noble |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | WSL Noble |

## 5. Red-capable rows and mutants (inputs actually run)

| Row | Input actually run | Result |
| --- | --- | --- |
| 401 diagnostic and credential probes (R1/R2) | Windows `go test -overlay … ./internal/harness/pi -run '^TestValidatorReportsProviderStopError$' -count=1`: pre-change `rpc.go` from `637f810a`; separately, final `rpc.go` with the projection call replaced by `message` | Each overlay exits 1: old output lacks the suffix; bypass output includes free text, including the case-variant Bearer, Basic, header, `api_key=`, short quoted value, `token` and long Bearer rows, plus `token=`/`key=` and oversized multiline detail. The unoverlaid row exits 0. |
| Go event identity (R3) | Noble shadow: remove the required `--package` argument guard, foreign-package rejection, contradictory package terminal rejection and exact fail counts | Test script 1 → 0 on restored source. Red rows: missing package option, child-only fail, missing/duplicate package fail, contradictory package pass, duplicate parent fail, foreign package terminal/child fail/run and unattributed parent fail. |
| Fixture flow (R4/R5) | Noble shadow: insert a stray named `record_credential_gated_leg` in the real arm, delete the tail's writer call and remove gated status tolerance | Exit 1: mode 0 reports a nonempty record list, mode 1 omits `write-declaration`, and the gated runner returns 1 (`Required native Go witness exited with status 1`) instead of 0. In a separate overlay deleting the tail `exit 1` and the real runner's record call, the mode-1 test sees status 0 and the runner test loses its named result (exit 1). Restored tests exit 0. |
| Export identity (R7) | Noble shadow: bypass the native-Pi requirement and all gated witness identity checks; in a separate shadow run leave everything intact except the declaration/tree equality check | Each test invocation exits 1. Without the guards the exporter returns 0 instead of 1 for the missing pin, wrong harness/case/subject, true or numeric-zero success flag, either skipped crash document, and (separately) the mismatched declaration digest. Restored tests exit 0. |
| gate default strictness | the same failing stream without `--credential-gated` | exit 1, no `credential-gated` text |
| exporter declaration | the exact declaration; extra field; version 2; wrong mode; wrong variable; missing leg; wrong outcome; wrong test; leg extra field; duplicate leg; a passed run; `--case-set stale_leaf`; missing file; mode-0644 file | exit 0 for the exact document (manifest `credential_gated` + published declaration), exit 1 with the named refusal for every other input |
| exporter exemption | the same witness root WITHOUT the declaration | exit 1, `pi durability witness is unavailable: pi-daemon-sigkill-helper-survives.json` — the exemption is declaration-driven, not presence-driven; mutant M3 (the `credential_gated_targets` argument removed from the pi-durability call, scratch clone only) reddens `ExporterTest.test_credential_gated_declaration_publishes_named_unproven_legs` with that same line |
| fixture mode detection | values `sk-scratch-dummy`, `sk-live-provider-credential-000000`, `sk-scratch-dummy-extra`, empty | `1 / 0 / 0 / 0`; mutant M2 (the comparison inverted to `!=`, scratch clone only) reddens all four subtests of `test_mode_detection_matches_only_the_ratified_placeholder` (expected `1`, got `0`) |
| fixture declaration | the three legs recorded in scrambled order | the exact schema (validated by the exporter's own validator), mode 0600, one named-outcome line per leg |
| `shared`-arm wiring | the extracted arm run with every leg runner stubbed, `placeholder_provider_credential` 0 then 1 | mode 0: the three legs run in the original order, no gated call; mode 1: the two crash legs are recorded instead of run, `pi_provider_action` is not run in place, the gated tail run is the arm's last call (`gated-pi-run:v2_pi_events`), and the recorded `(case, test)` pairs equal the two crash-leg rows of `CREDENTIAL_GATED_LEGS`; mutant M1 (the gated `record_credential_gated_leg helper_sigkill_detached_child …` call replaced by a no-op, `/tmp` shadow tree of the apply-stage bytes) reddens that row — the recorded pair list loses `helper_sigkill_detached_child TestProductionLinuxV2HelperSIGKILLAnchorReapsDetachedChild`; mutant M4 (that leg's test name in `owner_witness_case_identity` replaced by `TestWrongNameForThisLeg`, same shadow tree) reddens the pair pin with the same diff on the other side |

## 6. Which userland each property needs (standing rule)

The Go projection logic ran on Windows and Ubuntu 24.04 WSL (`VERSION_CODENAME=noble`); the Go
red-first overlays ran on Windows. The shell ordering/status and Python JSON logic rows ran on
that Noble userland. The exporter suite needs Linux UID/mode support for its existing private
file checks; none of the new digests, event identities or shell branches depends on the runner's
ACL handling, `/usr/bin/test`, or apt. No Resolute or hosted acceptance run is claimed here.

## 7. Does the conductor's Bind execute it?

The reviewed head was `19ec0b6ce5e46d11a1afa229fb1fa834510d2746` (baseline for this fix);
the superseded two document-gate counts came from `77fd2886`. The round-1 source digests are in
§1. The conductor has not yet run the Bind on the final PR head.

The `shared` Bind exercises the placeholder arm, named test and package failure terminals,
status tolerance, reader-before-declaration ordering, nonzero exit, and the declaration's source
binding. If the provider witness file exists it also exercises the failure identity check; the
two absent crash files exercise its normal no-document path. The validator table is not run by
the Bind; it lives in the Go tests in the `daemon` job. No Bind row injects missing/foreign test
events, a synthetic pass, a stray real-arm gated record, a false/stale declaration, or a stale or
success-shaped witness. These assertions and the extracted arm/runner checks run in **Check Linux
v2 fixture contracts** (`daemon` job, the two Python suites in §4). The real arm is for the
owner-approved credentialed dispatch, not the placeholder Bind.

A4: F6's pair pin still stubs `run_credential_gated_pi_provider_leg` in the extracted arm; it
does not pin the real runner's call site. The separate runner test extracts that function and
`run_go_test` and uses the real event gate, but stubs `sudo`/guest execution, request preparation
and cleanup. Neither harness exercises a live guest, relay, receipt or Control readback.

## 8. Gates

| Command (working directory) | Exit | Scope |
| --- | --- | --- |
| `python scripts/ci/check-record-cleanliness.py` (dispatch base → reviewed head in §7) | 0 | 3 changed records, including the newly merged sibling record |
| `python scripts/ci/check-citation-existence.py` (same range) | 0 | 1 changed audited document, 736 path citations, 3 archive ignore-rule notes |

The apply-stage counts of 2 and 729 are superseded; they were recorded before the origin/main
merge and were not reproduced on the reviewed head. The follow-on scratch commit of the round-1
bytes is the subject of the final document and duplication gate receipt in the handoff, not those
older counts. No full Go suite is claimed for this round.

## 9. Contract-clause matrix

| Clause | Green command (exit) | Evidence row | Red-first / baseline |
| --- | --- | --- | --- |
| 1B1: the fixture recognises the placeholder and records a NAMED outcome for the pi provider leg | `CredentialGatedFixtureTest` (0) | §2.1–§2.3, §5 | the real-arm stray record mutant fails §5 |
| 1B1: the case set CONTINUES, the export and `stop_readback_reader` become measurable | `python3 scripts/ci/test-linux-v2-evidence.py ExporterTest.test_credential_gated_declaration_publishes_named_unproven_legs` (0) | §2.2-§2.3, §5 exporter rows | without the declaration the same root refuses, naming `pi-daemon-sigkill-helper-survives.json` (the dispatch-base behaviour) |
| 1B1: never greener — the gate cannot count it as a pass; a reviewer can tell | `python3 scripts/ci/test-linux-v2-go-events.py` (0); exporter refusal rows (0) | §2.3, §5 gate + refusal rows | gated+pass and gated+skip both exit 1; a passed run with a declaration is refused |
| 1B1: with a real credential BYTE-IDENTICAL behaviour | `python3 scripts/ci/test-linux-v2-evidence.py` (0); the arm harness mode 0 | §2.4, §5 arm row | the arm harness runs the base order with no gated call for any non-placeholder value |
| 2C1: bounded provider diagnostic; classification unchanged | Go rows in §4 (0) | §3 and §5 | the raw-projection bypass and old-RPC overlays fail the named validator rows |
| Invariants: credentials B-only and never echoed | §4 Go and exporter tests (0) | the declaration names the variable only; the new failure-text path carries a bounded status hint and fixed marker, never the body or headers | §3 lists the durable sinks and the intentional diagnostic loss |
| Bind acceptance (amendment A1): the green legs run, the credential-gated outcome is read verbatim from the log and the redacted export, the export lands, the reader's Result row runs, and the bounded provider error is proven by its red-first unit row | conductor's `shared` Bind (owner gate) | `rpc_test.go` `TestValidatorReportsProviderStopError`; §2.3, §5 | this increment cannot run the Bind; the fixture-side rows above are the pre-Bind proof; the Bind is the acceptance |

## 10. Residuals and known limits

- The placeholder Bind cannot prove the authentic turn, the two detached-child legs or their
  Control provider receipts; the declaration and the manifest name them unproven. The owner-approved
  A3 `main` dispatch is their proof path (ruling 3A3; runbook
  `refactor/pi-authentic-turn-dispatch-runbook.md`), grouped per ruling 6G1.
- Test and package failure do not prove a typed credential refusal; another error in the same
  test can satisfy the gate, including receipt readback failure after a successful provider
  turn. If that path recorded provider success, the exporter refuses the conflicting document.
  The source binding does not identify a particular attempt on the same tree, and a gated
  provider document may be absent. No export can mark those legs passed; an authentic turn
  and receipt still require the credentialed run.
- The `shared` set's non-provider legs (stage-replay, ack-loss, response-loss, receipt-persist,
  pid-reuse, legacy replay, OpenCode health) have no green `shared` baseline in the merged history;
  the gated run is their first complete `shared` execution, and its result is the Bind's to record.

## 11. Retained artifacts

No raw event stream or credential is retained in the repository. §5 names the source overlays;
the final gate receipt records the scratch revision without putting a self-referential commit
hash in this file.
