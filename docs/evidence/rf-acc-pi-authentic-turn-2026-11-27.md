# rf-acc-pi-authentic-turn — evidence record (stage 2: named credential-gated pi legs and the bounded redacted provider stop)

Goal `rf-acc-pi-authentic-turn` (contract under the GDD contracts root, ruled by the conductor
2026-11-26; dispatch gate #303) plus the owner rulings of 2026-11-27 (1B1, 2C1, 3A3, 6G1). Branch
`refactor/rf-acc-pi-authentic-turn`, dispatch base `0a2f15efb1fd3928e88be27666e7ec9f738df0a2`
(`origin/main` `d45e3298` merged). Plans: `docs/plans/2026-11-26-rf-acc-pi-authentic-turn-plan.md`
(stage 1) and `docs/plans/2026-11-27-rf-acc-pi-authentic-turn-stage2-plan.md` (this stage). Stage-1
root cause, reused here and not re-measured: the fixture-staged placeholder credential, the
provider's HTTP 401, and pi's `stopReason:"error"` message
(`docs/evidence/rf-acc-pi-authentic-turn-2026-11-26.md` §2-§4). Every `file:line` citation in this
record binds the delivered revision whose digests §1 lists; the record's own bytes are uncommitted
in the goal worktree, and the gates in §8 ran on a scratch-clone commit of them.

## 1. Delivered bytes

| File | sha256 (delivered) | Change |
| --- | --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `2d9670e8427d19f4f23c5b8c68ae621970d66a0fc7b413cebb7d8b1686ca774f` | `detect_placeholder_provider_credential` decides the mode from the exact bytes the fixture writes into B; the `shared` arm records `credential_gated_*` outcomes for the two detached-child legs and defers `pi_provider_action` to the arm tail; `run_go_test` gains the `production-credential-gated` mode; the declaration writer and the gated tail leg; the gated run exits 1 after the reader |
| `scripts/ci/assert-go-test-json.py` | `d36b529f6f44101bd11f22d5b1976808787a299b0eb96bf6db864b370e7815a7` | new `--credential-gated TEST`: for that one test `run=1 skip=0 pass=0` with its own fail events is accepted and printed as `credential-gated`, never as `verified`; every other test keeps the strict rule |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `9ba5f02f367976d266423627fbdd9873dd73fa22deb08efa795ddf95e1822047` | new manual-only `--credential-gated-declaration`: exact-schema validation, published as `credential-gated-legs.json`, `manifest.credential_gated`, and the three named witnesses exempted from the pi-durability binding; refused for a passed run or any other case set |
| `daemon/internal/harness/pi/rpc.go` | `c9aa9935efc1582cabc386ee06f31face776b20f1fba4d4cd3e9eb41ab646293` | `normalAssistantStop` appends one 200-byte, whitespace-normalized, credential-redacted provider `errorMessage`; classification and every fail-closed decision unchanged |
| `daemon/internal/harness/pi/rpc_test.go` | `558fc3d1e17d7aba110bcd0859e4d991ee70d23cecc051192178ad1fd226fee2` | `TestValidatorReportsProviderStopError`: the exact 401 text, seven credential shapes redacted, UTF-8 cap, ten unchanged-shape rows, bare sentinel, normal stop |
| `scripts/ci/test-linux-v2-evidence.py` | `e32973b02a11ef0fba296c0d6da519de1a91b524101d1af1a9b7810def71d2e8` | exporter declaration rows, the constants pin against the fixture, and the `CredentialGatedFixtureTest` block harnesses (mode detection, declaration, `shared`-arm wiring) |
| `scripts/ci/test-linux-v2-go-events.py` | `8d133db9e2225ffd9ab390120fc6af37b4813617ca2a69518ce2fa2032d8821d` | the gate's `--credential-gated` acceptance/rejection rows and the default-gate strictness row |
| `docs/plans/2026-11-27-rf-acc-pi-authentic-turn-stage2-plan.md`, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | the accepted stage-2 plan; the stage-1 items move to the archive and the credentialed-proof residual stays open |

## 2. Slice 2 (1B1): the placeholder mode and the named outcomes

### 2.1 The mode is decided from the staged bytes, in the fixture

`detect_placeholder_provider_credential` (`linux-v2-native-acceptance.sh:403`) compares
`SYMMETRY_OPENAI_API_KEY` with the ratified scratch placeholder (`sk-scratch-dummy`) immediately
before the fixture writes that same value into `native_api_key_file` and B's
`~/.pi/agent/auth.json` (`:409-413`). Any other value, including every real secret, leaves
`placeholder_provider_credential=0` and every path below byte-identical to the dispatch base. The
daemon never reads the credential to classify a stop.

### 2.2 Which legs are gated, and why `pi_provider_action` runs last

The three legs the fixture already marks `provider_receipt_required=1` (`:2416-2450`) need a
successful model turn: `pi_provider_action` asserts `provider_action_succeeded` in its own witness
response, and both detached-child legs additionally need the child that only the model's
`resource.sync` plus commit can produce (`:942-947` writes `post-commit-child.pid` from the
`post-commit` hook). Ruling 1B1's purpose clause — the export and `stop_readback_reader` becoming
measurable again — needs all three to stop aborting the set, because the exporter's pi-durability
check requires every other allowlisted witness (`export-linux-v2-redacted-evidence.py:1515`) and
the reader's single call site is after the whole `case` block (`:3351`).

Under the placeholder the arm records `credential_gated_helper_sigkill_detached_child` and
`credential_gated_daemon_sigkill_helper_survives_detached_child` in place of those legs
(`:3283-3289`) and still RUNS `pi_provider_action`, at the arm's tail (`:3310-3312`), because its
aborted run retains an unreleased `RunJournal` in the single installed owner StateDir every
production witness shares with it, and
`newLinuxV2NativeFixture` refuses any fresh production witness while a prior journal is not
physically released (`daemon/e2e/production_linux_v2_native_witness_test.go:2087-2100`; the same
guard is why the alternate-mount refusal leg owns a case set, `docs/TODO.md`). The deferred leg is
the only way to keep the contract's stop text measurable without fencing the remaining legs; no
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
  mode, the credential variable NAME, and the three `case`/`test`/`outcome` rows; the manifest's
  `credential_gated` object; and `missing_allowlisted_files` naming the two unrun crash witnesses
  and the three Control provider receipts that a placeholder run cannot produce. A gated run can
  never export `status: "passed"` (refused at `export-linux-v2-redacted-evidence.py:1585-1589`),
  and the case set never prints its passed line.

### 2.4 The real-credential path is unchanged

Every new branch sits behind `(( placeholder_provider_credential ))`, which is 0 for any value
other than the ratified placeholder. In that case the arm runs the three legs at their original
positions with the original go-test mode, no declaration is written, `export_native_evidence`
passes no new argument, and the exporter's new blocks are inert (the new option is `None`, the new
manifest key is only added for a non-empty case list, and the durability call passes the default
empty exemption set). The A3 `main` dispatch therefore exercises the dispatch-base behaviour.

## 3. Slice 3 (2C1): the bounded, redacted provider stop

`normalAssistantStop` (`rpc.go:860-869`) still rejects any `stopReason` other than `stop` with
`ErrUnexpectedAssistantStop` wrapping and appends one field when the retained message carries a
usable `errorMessage`:

- cap: 200 bytes (`maxStopErrorMessageBytes`) after normalization, truncated on a UTF-8 boundary
  with a trailing `…`; whitespace runs and control characters become single spaces, so the witness
  line stays one line inside the fixture's 400-byte bound;
- redaction before truncation: one alternation covers `Bearer <8+>`, a bare 32+ run of
  `[A-Za-z0-9+/=_-]`, `sk-`, `gho_`, `ghp_`, `github_pat_` and `xox[baprs]-` shapes, each replaced
  by `<redacted>`;
- a message without a non-empty string `errorMessage` keeps today's exact text.

Measured, from the delivered test row (`rpc_test.go` `TestValidatorReportsProviderStopError`):

```text
pi rpc final assistant message did not stop normally: "error": provider error: 401: {"message":"Authentication Error, Invalid API key provided.","type":"token_not_found_in_db","param":"key","code":"401"}
```

That is exactly the row the stage-1 record measured from pi (`errorMessage` with the same 401 text)
and the row the conductor's Bind reports as "the pi stop text WITH the bounded error".

Rows actually run: (1) RED on the pre-change bytes — `go test ./internal/harness/pi/ -run
'^TestValidatorReportsProviderStopError$' -count=1` failed on provider_401, redacts_token_shapes and
caps_at_utf8_boundary with the old text; (2) GREEN after the change — `go test
./internal/harness/pi/ -count=1` → `ok`, exit 0; (3) redaction-removal mutant — deleting
`message = stopErrorCredentials.ReplaceAllString(message, "<redacted>")` reddened exactly the
`redacts_token_shapes` row (the token-shaped values appear verbatim in the failure diff) while every
other row stayed green; the mutant was then restored and the package re-run green.

## 4. Measured rows (command → exit code)

| Command (working directory) | Exit | Note |
| --- | --- | --- |
| `go test ./internal/harness/pi/ -run '^TestValidatorReportsProviderStopError$' -count=1` (pre-change `rpc.go`, `daemon/`) | 1 | the red-first row: 3 subtests fail with the old stop text |
| `go test ./internal/harness/pi/ -count=1` (delivered, `daemon/`) | 0 | `ok … 2.953s` |
| same, with the redaction line deleted (mutant, restored afterwards) | 1 | only `redacts_token_shapes` fails |
| `gofmt -l internal/harness/pi` (`daemon/`) | 0 | empty |
| `go vet ./...` (`daemon/`) | 0 | empty output |
| `python3 scripts/ci/test-linux-v2-go-events.py` (root, Windows and WSL Noble) | 0 | 9 tests; the three new `--credential-gated` rows included |
| `python3 scripts/ci/test-linux-v2-evidence.py` (WSL Noble) | 0 | 64 tests; the three fixture-harness rows and four exporter rows included |
| `python3 scripts/ci/test-linux-v2-{source-subject,host-prerequisite-probe,owner-request-keys}.py` (WSL Noble) | 0 each | 7 / 17 / 10 tests |
| `sudo python3 scripts/ci/test-linux-v2-control-receipt.py` (WSL Noble) | 0 | 9 tests |
| `go test ./... -count=1` (`daemon/`, Windows) | 0 | every package `ok` (139.42 s wall) |
| `go test ./... -count=1` (`daemon/`, WSL Noble) | 1 | only `internal/platform` fails, on 13 pre-existing root-only `wire harness requires an unprivileged A process: uid=0 …` refusals; `internal/harness/pi` (`ok`, 1.740 s), `internal/state`, `internal/workspace` and `native/linux-gate/protocol` are green |
| `go list -deps ./internal/platform` (WSL) | 0 | `harness/pi` occurrences: 0 — the failing package's build closure is unchanged by this increment |
| `go vet ./...` (`daemon/`, WSL Noble) | 0 | Linux compile check of the changed package and its consumers |

## 5. Red-capable rows and mutants (inputs actually run)

| Row | Input actually run | Result |
| --- | --- | --- |
| 401 stop text | pre-change `rpc.go` + the new row | red: old text lacks `provider error:` |
| credential redaction | mutant removing the `ReplaceAllString` line | red: `sk-`/`gho_`/`ghp_`/`github_pat_`/`xoxb-`/`Bearer`/32+-run values appear verbatim |
| gate gated mode | event streams: expected failure with its package fail; subtest failure; pass; missing run; missing fail; skip; foreign named fail | 0 / 0 / 1 / 1 / 1 / 1 / 1 — and only the accepted pair prints the `credential-gated` line |
| gate default strictness | the same failing stream without `--credential-gated` | exit 1, no `credential-gated` text |
| exporter declaration | the exact declaration; extra field; version 2; wrong mode; wrong variable; missing leg; wrong outcome; wrong test; leg extra field; duplicate leg; a passed run; `--case-set stale_leaf`; missing file; mode-0644 file | exit 0 for the exact document (manifest `credential_gated` + published declaration), exit 1 with the named refusal for every other input |
| exporter exemption | the same witness root WITHOUT the declaration | exit 1, `pi durability witness is unavailable: pi-daemon-sigkill-helper-survives.json` — the exemption is declaration-driven, not presence-driven; mutant M3 (the `credential_gated_targets` argument removed from the pi-durability call, scratch clone only) reddens `ExporterTest.test_credential_gated_declaration_publishes_named_unproven_legs` with that same line |
| fixture mode detection | values `sk-scratch-dummy`, `sk-live-provider-credential-000000`, `sk-scratch-dummy-extra`, empty | `1 / 0 / 0 / 0`; mutant M2 (the comparison inverted to `!=`, scratch clone only) reddens all four subtests of `test_mode_detection_matches_only_the_ratified_placeholder` (expected `1`, got `0`) |
| fixture declaration | the three legs recorded in scrambled order | the exact schema (validated by the exporter's own validator), mode 0600, one named-outcome line per leg |
| `shared`-arm wiring | the extracted arm run with every leg runner stubbed, `placeholder_provider_credential` 0 then 1 | mode 0: the three legs run in the original order, no gated call; mode 1: the two crash legs are recorded instead of run, `pi_provider_action` is not run in place, and the gated tail run is the arm's last call (`gated-pi-run:v2_pi_events`); mutant M1 (the gated `record_credential_gated_leg helper_sigkill_detached_child …` call replaced by a no-op, scratch clone only) reddens that row with `record:helper_sigkill_detached_child not found in […]` |

## 6. Which userland each property needs (standing rule)

The new rows are Go unit rows, Python file/logic rows and bash-logic rows. No new assertion's
outcome depends on the runner's coreutils, POSIX ACL handling or the runner's POSIX `test`
semantics: the
permission-shaped check that is new is the exporter reading the fixture declaration as a bounded
regular mode-0600 file, exercised by the mode-0644 refusal row, which is plain `stat`/`open`
semantics on any Linux userland (run on WSL Ubuntu-24.04, `VERSION_CODENAME=noble`, and by the
existing CI job on the runner image). No `sudo`-stubbed ACL row was added, so this increment needs
no Resolute-userland RED/GREEN demonstration beyond the standing rule's Go/shell-logic carve-out in
the owner rulings of 2026-11-27. The Windows-local evidence suite still fails wholesale on
`os.getuid`-dependent rows exactly as the stage-1 record documents for the base bytes; the suite is
run in WSL for every gate here.

## 7. Does the conductor's Bind execute it?

The Bind's `shared` run on the PR head executes: the mode detection, the two gated records, the
deferred gated `pi_provider_action` run and its gate row, the declaration write, the gated exit 1,
and the exporter's declaration path with `credential-gated-legs.json` in the uploaded artifact.
The Bind does NOT execute: the declaration refusals (they need a hostile/mismatched declaration),
the mode-match negative rows or the arm harness rows — those live only in
`Check Linux v2 fixture contracts` (`daemon` job, `python3 ../scripts/ci/test-linux-v2-{go-events,evidence}.py`).

## 8. Gates

| Command (working directory) | Exit | Scope |
| --- | --- | --- |
| `go test ./... -count=1` (`daemon/`, Windows, delivered bytes) | 0 | full daemon suite, 20 packages `ok` |
| `go test ./... -count=1` (`daemon/`, WSL Noble, delivered bytes) | 1 | only `internal/platform`'s pre-existing root-only refusals (13 occurrences, the same class the stage-1 record measured at the base bytes); every other package `ok`, including `internal/harness/pi` |
| `go vet ./...` (`daemon/`, WSL Noble) | 0 | Linux compile check |
| the six `scripts/ci/test-linux-v2-*.py` suites (§4) | 0 each | fixture contracts and the new rows, re-run on the delivered bytes |
| `python scripts/ci/jscpd-changed.py --base 0a2f15efb1fd3928e88be27666e7ec9f738df0a2 --head HEAD` (scratch-clone commit of the delivered bytes) | 0 | 11 changed file(s), 0 tolerated among the added lines, none new; it notes that `docs/archive/CLOSED-TODO.md` is outside its configured scope, so clones inside that file are not checked |
| `python …/comment-cap.py <scratch clone>` (committed diff of the scratch clone) | 0 | zero doc comments, hazard comments at most two lines |
| `python scripts/ci/check-record-cleanliness.py --base 0a2f15ef… --head HEAD` (scratch clone) | 0 | this record and the updated docs carry no host path |
| `python scripts/ci/check-citation-existence.py --base 0a2f15ef… --head HEAD` (scratch clone) | 0 | every added citation target exists |

## 9. Contract-clause matrix

| Clause | Green command (exit) | Evidence row | Red-first / baseline |
| --- | --- | --- | --- |
| 1B1: the fixture recognises the placeholder and records a NAMED outcome for the pi provider leg | `python3 scripts/ci/test-linux-v2-evidence.py CredentialGatedFixtureTest` (0) | §5 mode detection + declaration + arm wiring; §2.1-§2.3 | the mode-detection row reddens if the comparison is swapped or removed; the arm row reddens if a gated branch is deleted |
| 1B1: the case set CONTINUES, the export and `stop_readback_reader` become measurable | `python3 scripts/ci/test-linux-v2-evidence.py ExporterTest.test_credential_gated_declaration_publishes_named_unproven_legs` (0) | §2.2-§2.3, §5 exporter rows | without the declaration the same root refuses, naming `pi-daemon-sigkill-helper-survives.json` (the dispatch-base behaviour) |
| 1B1: never greener — the gate cannot count it as a pass; a reviewer can tell | `python3 scripts/ci/test-linux-v2-go-events.py` (0); exporter refusal rows (0) | §2.3, §5 gate + refusal rows | gated+pass and gated+skip both exit 1; a passed run with a declaration is refused |
| 1B1: with a real credential BYTE-IDENTICAL behaviour | `python3 scripts/ci/test-linux-v2-evidence.py` (0); the arm harness mode 0 | §2.4, §5 arm row | the arm harness runs the base order with no gated call for any non-placeholder value |
| 2C1: bounded, redacted provider error appended; classification unchanged | `go test ./internal/harness/pi/ -count=1` (0) | §3 exact text; §4-§5 rows | RED on the pre-change bytes; the redaction-removal mutant reddens `redacts_token_shapes`; every row asserts `errors.Is(…, ErrUnexpectedAssistantStop)` |
| Invariants: credentials B-only and never echoed | — (no new secret path) | the declaration names the variable, never a value; the exporter's credential scan runs on it like on every source | the mode detection compares in-fixture; no new log line prints the key |
| Bind acceptance: the recorded stop is gone and the gated outcome is named | conductor's `shared` Bind (owner gate) | this increment cannot run the Bind | the fixture-side rows above are the pre-Bind proof; the Bind is the acceptance |

## 10. Residuals and known limits

- The placeholder Bind cannot prove the authentic turn, the two detached-child legs or their
  Control provider receipts; the declaration and the manifest name them unproven. The owner-approved
  A3 `main` dispatch is their proof path (ruling 3A3; runbook
  `refactor/pi-authentic-turn-dispatch-runbook.md`), grouped per ruling 6G1.
- Under the placeholder, a genuine defect in the three provider legs would also be recorded as
  `credential_gated_*` rather than aborting the set; the leg still runs (for
  `pi_provider_action`), its stop lines still print, and nothing is claimed as passed, but the
  distinction between "unproven for lack of a credential" and "broken" needs the credentialed run.
- The `shared` set's non-provider legs (stage-replay, ack-loss, response-loss, receipt-persist,
  pid-reuse, legacy replay, OpenCode health) have no green `shared` baseline in the merged history;
  the gated run is their first complete `shared` execution, and its result is the Bind's to record.

## 11. Retained artifacts

None in the repository beyond the delivered files above. The probe and mutant outputs quoted in §3
and §5 are the commands' own output; the scratch clone used by §8 (jscpd, comment cap, record and
citation gates) and the fixture-harness temp directories are outside the repository and are removed
after the gates.
