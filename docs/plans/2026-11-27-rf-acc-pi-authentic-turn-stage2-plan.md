# rf-acc-pi-authentic-turn — plan (stage 2: slices 1B1 and 2C1)

Contract: the goal file under the GDD contracts root (`goal-rf-acc-pi-authentic-turn.md`), stage 1
plus the owner rulings of 2026-11-27. Branch `refactor/rf-acc-pi-authentic-turn`, dispatch base
`0a2f15efb1fd3928e88be27666e7ec9f738df0a2` (merge of `origin/main` `d45e3298`). Root cause and
candidate discrimination: `docs/evidence/rf-acc-pi-authentic-turn-2026-11-26.md` (stage 1, binding
on `07be175e`). Shape reference: `docs/evidence/rf-acc-pi-provider-admission-2026-11-26.md`
(committed, tested, allowlisted-evidence increment). This plan covers only what the accepted plan
leaves open: the exact fixture/exporter/gate shape, the named-outcome contract, the gated-leg set,
and the daemon stop-text format.

## 1. What this increment delivers

- **Slice 2 (ruling 1B1).** In the `shared` arm of `scripts/ci/linux-v2-native-acceptance.sh`, the
  fixture decides the provider credential mode from the exact bytes it writes into B's
  `auth.json`. Under the ratified placeholder it records a named credential-gated outcome for each
  pi leg that needs an authentic provider turn and CONTINUES, so the set reaches
  `stop_readback_reader` and the redacted export; the run never reports itself passed.
- **Slice 3 (ruling 2C1).** `daemon/internal/harness/pi/rpc.go` appends one bounded, redacted
  provider `errorMessage` to the `ErrUnexpectedAssistantStop` text; stop classification and every
  fail-closed decision are unchanged.
- Committed tests for every new arm, a new exported allowlisted file for the named outcomes, and
  the evidence record.

## 2. Decisions the plan fixes

### 2.1 Which legs are credential-gated, and why all three

The fixture already marks the authentic-provider legs: `provider_receipt_required=1` for
`pi_provider_action`, `helper_sigkill_detached_child` and
`daemon_sigkill_helper_survives_detached_child` (`linux-v2-native-acceptance.sh:2398-2432`). All
three need a model turn that succeeds: the provider-action leg's e2e asserts
`provider_action_succeeded` (its own witness response), and both crash legs need the detached
child that only the model's `resource.sync` + commit can produce (the `post-commit` hook writes
`post-commit-child.pid`, `:942-947`). Ruling 1B1's purpose clause — "so `shared`'s redacted export
and `stop_readback_reader` become measurable" — is met only when all three stop aborting the set,
because `validate_pi_durability_witnesses`
(`scripts/ci/export-linux-v2-redacted-evidence.py:1479-1522`) requires every other allowlisted
witness and the reader's single call site (`:3249`) is after the whole `case` block.

### 2.2 Credential mode is decided once, from the staged bytes

`: "${SYMMETRY_OPENAI_API_KEY:?...}"` already requires a non-empty value (`:19`). Immediately
before the fixture writes that value to `native_api_key_file` and unsets it (`:391-398`), it also
compares it byte-for-byte with the ratified scratch placeholder (`sk-scratch-dummy`) and sets
`placeholder_provider_credential=1` on an exact match. The comparison needs no daemon change, and
any other value — including any real secret — leaves every path below byte-identical to today.

### 2.3 Gated outcome mechanism per leg

- `pi_provider_action` still RUNS, but it runs at the tail of the `shared` arm. Reason: its
  aborted run leaves an unreleased `RunJournal` in the shared `/var/lib/symmetry-v2` StateDir, and
  `newLinuxV2NativeFixture` refuses any fresh production witness while a prior journal is not
  physically released (`daemon/e2e/production_linux_v2_native_witness_test.go:2087-2100`; the
  same guard is why `alternate_mount_refusal` owns a case set, `docs/TODO.md`). Running it last
  keeps the contract's stop text measurable while no later leg can be poisoned. Nothing else
  depends on the leg's position: each leg has its own Control claim, request, event file and
  result row.
- `helper_sigkill_detached_child` and `daemon_sigkill_helper_survives_detached_child` are NOT
  run: their witnesses additionally need the detached child born from the model's commit, so a
  placeholder run cannot exercise them at all, and their aborted journals would poison the state
  shared with the remaining legs.
- Every gated leg prints one named outcome line, e.g.
  `credential_gated_pi_provider_action`, and the fixture writes a machine-readable declaration
  (2.5). The named outcome is the single spelling used in the log, the declaration, the manifest
  and the evidence record.

### 2.4 A gated run can never report itself passed

The fixture exits non-zero whenever at least one leg was credential-gated: the set's "passed"
line, the workflow's per-set manifest requirement and the exporter's passed-run completeness check
(`:1486-1490`) are all gated on status 0, so the only honest terminal state is a failed set whose
manifest still lands (`status: "failed"`, `missing_allowlisted_files` naming what was not
produced, `credential_gated` naming what was not proven). The Bind's "named stop is gone"
therefore reads as: the set completes, the export lands, the reader's Result row runs, and the
unproven legs are named.

### 2.5 The named-outcome declaration and its exported shape

Fixture side (`$fixture_root/credential-gated-legs.json`, mode 0600, written only in placeholder
mode, only after every gated leg was recorded):

```json
{"version":1,"mode":"placeholder_provider_credential",
 "credential_environment_variable":"SYMMETRY_OPENAI_API_KEY",
 "legs":[{"case":"daemon_sigkill_helper_survives_detached_child","test":"TestProductionLinuxV2DaemonSIGKILLHelperSurvivesDetachedChild","outcome":"credential_gated_daemon_sigkill_helper_survives_detached_child"},
         {"case":"helper_sigkill_detached_child","test":"TestProductionLinuxV2HelperSIGKILLAnchorReapsDetachedChild","outcome":"credential_gated_helper_sigkill_detached_child"},
         {"case":"pi_provider_action","test":"TestProductionLinuxV2PiNativeProviderAction","outcome":"credential_gated_pi_provider_action"}]}
```

Exporter side: new manual-only option `--credential-gated-declaration <absolute path>`. When it
is present the exporter (a) refuses any run status other than `failed` and any case set other than
`shared`; (b) requires the exact schema above (exact keys, exact three legs, exact test names,
`outcome == "credential_gated_" + case`, sorted, no duplicates); (c) publishes it as
`credential-gated-legs.json`; (d) adds the `credential_gated` manifest object (mode, declaration
target, case names); and
(e) exempts exactly those three witnesses from the pi-durability binding, because a gated leg's
document is a failure artifact, not a staged-pi proof — every other required witness keeps the
unchanged check. Without the option the exporter behaves byte-identically, so the real-credential
run and the A3 `main` dispatch are untouched.

### 2.6 The event gate expresses "credential-gated"

`assert-go-test-json.py` gains `--credential-gated TEST`: for that one named test the gate
requires `run=1`, `skip=0`, `pass=0`, and that every test-named `fail` event is that test or its
subtest, instead of the strict `run=1 pass=1 skip=0`. It prints
`credential-gated TEST: run=1 pass=0 skip=0` and exits 0 only on those counts. Every other
expected test keeps the strict rule; a gated leg that PASSES fails this gate (a placeholder
credential cannot produce an authentic turn, so a pass is an inconsistency, not a green). The
fixture calls it only through the new `production-credential-gated` go-test mode; without the flag
the gate is byte-identical. This is the visible, named format change the ruling allows; no check
is relaxed.

### 2.7 Daemon stop text (slice 3)

`normalAssistantStop` keeps its classification and appends at most one bounded provider error when
the retained final assistant message carries a usable `errorMessage`:

- cap: 200 bytes after whitespace normalization (control characters and newlines become single
  spaces, so the witness line stays one line inside the fixture's 400-byte line bound), truncated
  on a UTF-8 boundary with a trailing `…`;
- redaction: runs matching `sk-…`, `gho_…`, `ghp_…`, `github_pat_…`, `xoxb-…`, `Bearer …` or a
  bare 32+-character hex/base64 blob become `<redacted>`;
- a message without `errorMessage`, or with a non-string one, keeps today's exact text
  (`pi rpc final assistant message did not stop normally: "error"`);
- format: `pi rpc final assistant message did not stop normally: "error": provider error: 401:
  {"message":"Authentication Error, …"}` — the stop reason stays verbatim and the provider text is
  only appended.

## 3. Edge cases the contract demands

- Real credential: no gated path, no declaration, no extra exit; logs, exports and the workflow's
  passed-run checks are as today.
- Placeholder + a gated leg that does not behave as expected (test absent, skipped, passed, or the
  go test exits without the gated counts): the gate fails, the fixture treats it as a hard wall,
  and the set aborts with the existing failure reporting.
- Placeholder + an earlier leg fails for its own reason: the set aborts before the gated tail leg;
  the export keeps today's refusal semantics (no declaration is written, because not every gated
  leg was recorded — the declaration is written only when all three are).
- Placeholder + a later real-credential `shared` wall: with a real key nothing changes, so the
  gated declaration never appears in a credentialed run.
- The three pi legs under a real credential keep their position and code path: the deferral, the
  skips and the gated mode exist only inside `if (( placeholder_provider_credential ))`.

## 4. Tests and evidence plan

| New behaviour | Committed red-capable test | Runs where |
| --- | --- | --- |
| gate `--credential-gated` (accepted counts, absent test, skip, pass, foreign fail) | `scripts/ci/test-linux-v2-go-events.py` rows | `linux-v2 scripts` step (CI, ubuntu-26.04) and locally |
| exporter declaration (accepted schema, passed-run refusal, wrong case set, wrong leg set, wrong outcome, non-strict file) | `scripts/ci/test-linux-v2-evidence.py` rows | same |
| fixture gated mode/shape (mode detection, named outcome, skip, deferral, gated-mode wiring, declaration write) | `scripts/ci/test-linux-v2-evidence.py` extracted-block harness rows | same (Python logic; no userland-dependent property) |
| daemon bounded/redacted stop text | `daemon/internal/harness/pi/rpc_test.go` rows (red on pre-change bytes; redaction-removal mutant reddens a named row) | `daemon` job and locally |

Evidence plan: run every row red-first where it is practical (new daemon rows on the pre-change
bytes; the shell harness pins redden when the gated arm is deleted or swapped), capture the exact
commands and exit codes, and record per clause whether the conductor's Bind `shared` run executes
it (the gated fixture arm, the declaration export and the gate flag) or whether it lives only in
CI (the exporter unit rows).

## 5. Out of scope, with reasons

- Any change to secrets, the environment's deployment branch policy or the credentialed dispatch:
  owner territory (ruling A3, `refactor/pi-authentic-turn-dispatch-runbook.md`).
- The two crash legs' authentic proof: it needs the same real credential; the gated declaration
  names them unproven, and the A3 dispatch is their proof path.
- Control bytes and the daemon's witness flow: unchanged; the fixture decides the mode.
- `.github/workflows/*`: no new step; the new rows ride the existing `Check Linux v2 fixture
  contracts` step, so no workflow expression changes.
