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
  Pi leg that needs an authentic provider turn and continues to the remaining checks. Those
  checks can still stop the set before `stop_readback_reader` or a complete redacted export;
  the run never reports itself passed.
- **Slice 3 (ruling 2C1).** `daemon/internal/harness/pi/rpc.go` appends a bounded projection of
  provider `errorMessage` to the `ErrUnexpectedAssistantStop` text; stop classification and every
  fail-closed decision are unchanged.
- Committed tests for every new arm, a new exported allowlisted file for the named outcomes, and
  the evidence record.

## 2. Decisions the plan fixes

### 2.1 Which legs are credential-gated, and why all three

The fixture already marks the authentic-provider legs: `owner_witness_case_identity` carries the
receipt flag `1` for `pi_provider_action`, `helper_sigkill_detached_child` and
`daemon_sigkill_helper_survives_detached_child` (`linux-v2-native-acceptance.sh:2419-2434`). All
three need a model turn that succeeds: the provider-action leg's e2e asserts
`provider_action_succeeded` (its own witness response), and both crash legs need the detached
child that only the model's `resource.sync` + commit can produce (the `post-commit` hook writes
`post-commit-child.pid`, `:959-961`). Ruling 1B1's purpose clause — "so `shared`'s redacted export
and `stop_readback_reader` become measurable" — is met only when all three stop aborting the set,
because `validate_pi_durability_witnesses`
(`scripts/ci/export-linux-v2-redacted-evidence.py:1503-1534`) requires every other allowlisted
witness and the reader's single call site (`:3361`) is after the whole `case` block.

### 2.2 Credential mode is decided once, from the staged bytes

`: "${SYMMETRY_OPENAI_API_KEY:?...}"` already requires a non-empty value (`:19`). Immediately
before the fixture writes that value to `native_api_key_file` and unsets it (`:402-415`), it also
compares it byte-for-byte with the ratified scratch placeholder (`sk-scratch-dummy`) and sets
`placeholder_provider_credential=1` on an exact match. The comparison needs no daemon change, and
any other value — including any real secret — leaves every path below byte-identical to today.

### 2.3 Gated outcome mechanism per leg

- `pi_provider_action` runs at its original position, before the Pi peer-negative check. Its
  gated runner records the outcome once the exact Go test failure is verified. Deferring the
  run hid the outcome when replay aborted; Bind 46020141750 demonstrated that failure mode.
  A retained `RunJournal` in `<STATE_DIR>` can still fence later production witnesses through
  `newLinuxV2NativeFixture`; there is no exception to the physical-release guard.
- `helper_sigkill_detached_child` and `daemon_sigkill_helper_survives_detached_child` are NOT
  run: their witnesses additionally need the detached child born from the model's commit, so a
  placeholder run cannot exercise them at all, and their aborted journals would poison the state
  shared with the remaining legs.
- Every gated leg prints one named outcome line, e.g.
  `credential_gated_pi_provider_action`, and the fixture writes a machine-readable declaration
  (2.5). The named outcome is the single spelling used in the log, the declaration and the evidence
  record; the manifest's `credential_gated.legs` carries the bare case names, and the declaration's
  `outcome` values are validated as `credential_gated_` + those names.

### 2.4 A gated run can never report itself passed

The fixture exits non-zero whenever at least one leg was credential-gated: the set's "passed"
line, the workflow's per-set manifest requirement and the exporter's passed-run completeness check
are all gated on status 0. A failed manifest can land only if all other required checks hold;
its `credential_gated` index names what was not proven. If a later wall stops the set, the
declaration can be a partial export member; the exporter still refuses to emit a manifest if a
required durability witness is missing. The next Bind reports the case-set exit, the named Pi
outcome in log and export, the partial or full member list, the reader Result's presence or
absence, and the first unexpected failure.

### 2.5 The named-outcome declaration and its exported shape

Fixture side (`$fixture_root/credential-gated-legs.json`, mode 0600): the fixture writes version 1,
mode `placeholder_provider_credential`, the credential variable name `SYMMETRY_OPENAI_API_KEY`,
`harness: "pi"`, and the fixture's `source_tree_digest`. It writes after the two crash-leg
records and before replay, with one `case`/`test`/`outcome`/`test_result` row per leg:

```json
{"case":"pi_provider_action","test":"TestProductionLinuxV2PiNativeProviderAction",
 "outcome":"credential_gated_pi_provider_action","test_result":"failed"}
```

Both crash legs record `test_result: "not_run"`.

Exporter side: new manual-only option `--credential-gated-declaration <absolute path>`. When it
is present the exporter requires manual `shared`, `failed`, the Pi pins, the exact declaration
schema and leg set (no duplicates), and equality with `--source-tree-digest`. A present provider
action witness must match the declared test, harness and source tree and record the boolean false
for `provider_action_succeeded` without a contradictory success outcome. The unrun crash legs
must not have documents. All three Control provider receipt sources are forbidden, as are
pre-existing output destinations for the three receipts or three gated witnesses. Neither a
declaration nor a failure witness is staged-Pi proof. Every other witness keeps its existing
check. Without the option the exporter behaves as before, including in a credentialed dispatch.

### 2.6 The event gate expresses "credential-gated"

The gated runner selects the exact `./e2e` package at the event gate. For the named test,
`--credential-gated TEST` requires one `run`, one fail for the test itself, no pass or skip,
and one matching package fail. The parent run precedes its fail, and the package fail closes
the selected stream. A child fail may accompany, but cannot replace, the parent's fail.
The only accepted message is `credential-gated TEST: run=1 pass=0 skip=0`. Without the flag the
original `run=1 pass=1 skip=0` gate remains.

### 2.7 Daemon stop text (slice 3)

`normalAssistantStop` keeps its sentinel and quoted reason. A nonempty string `errorMessage`
yields `HTTP 401; [details redacted]` for a leading `401: `, likewise for exact ASCII prefixes
400–599; otherwise it yields only `[details redacted]`. The longest diagnostic is 28 bytes
(46 with `: provider error: `), all ASCII; no arbitrary body or header can reach a sink through
this suffix. A missing, non-string or blank error field does not change the stop text.

## 3. Edge cases the contract demands

- Real credential: no gated path, no declaration, no extra exit; logs, exports and the workflow's
  passed-run checks are as today.
- Placeholder + an absent, skipped or incomplete failure stream: the gate refuses it. This is
  not a live check for a provider unexpectedly accepting the placeholder: the gated invocation
  does not wire the Control receipt, so the witness could still fail later at receipt readback.
  The shape gate rejects a synthetic pass stream, but the live classification is unproven, not a
  typed credential refusal or a proof that the provider refused the request.
- Placeholder + an earlier leg fails for its own reason: a failure before all three gated records
  prevents the declaration. Later aborts preserve it as partial evidence without bypassing the
  exporter refusal.
- Placeholder + a later real-credential `shared` wall: with a real key nothing changes, so the
  gated declaration never appears in a credentialed run.
- The three Pi legs under a real credential keep their position and code path. The skips and
  gated mode exist only inside `if (( placeholder_provider_credential ))`.

## 4. Tests and evidence plan

| New behaviour | Committed red-capable test | Runs where |
| --- | --- | --- |
| gate `--credential-gated` (completed failure, incomplete or foreign events) | `scripts/ci/test-linux-v2-go-events.py` rows | `Check Linux v2 fixture contracts` (CI `daemon` job) and locally |
| exporter declaration (accepted schema, passed-run refusal, wrong case set, wrong leg set, wrong outcome, non-strict file) | `scripts/ci/test-linux-v2-evidence.py` rows | same |
| production credential staging, gated position, abort before reader stop, private declaration write and exporter argument | `scripts/ci/test-linux-v2-evidence.py` extracted-block harness rows | same; permissions checked separately in Resolute userland |
| daemon bounded/redacted stop text | validator rows in `daemon/internal/harness/pi/rpc_test.go`; the record names the exact overlays | `daemon` job and locally |

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
