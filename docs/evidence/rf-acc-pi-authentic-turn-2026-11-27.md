# rf-acc-pi-authentic-turn — evidence record (stage 2: named credential-gated pi legs and the bounded redacted provider stop)

Goal `rf-acc-pi-authentic-turn` (contract under the GDD contracts root, ruled by the conductor
2026-11-26; dispatch gate #303) plus the owner rulings of 2026-11-27 (1B1, 2C1, 3A3, 6G1). Branch
`refactor/rf-acc-pi-authentic-turn`, dispatch base `0a2f15efb1fd3928e88be27666e7ec9f738df0a2`
(`origin/main` `d45e3298` merged). Plans: `docs/plans/2026-11-26-rf-acc-pi-authentic-turn-plan.md`
(stage 1) and `docs/plans/2026-11-27-rf-acc-pi-authentic-turn-stage2-plan.md` (this stage). Stage-1
root cause, reused here and not re-measured: the fixture-staged placeholder credential, the
provider's HTTP 401, and pi's `stopReason:"error"` message
(`docs/evidence/rf-acc-pi-authentic-turn-2026-11-26.md` §2–§4). This record covers the consolidated
fix. The source digests below bind the repaired bytes; the handoff receipt binds the scratch
commit including this record. The original, unsuccessful Bind is retained verbatim at the end.

## 1. Delivered bytes

| File | sha256 (delivered) | Change |
| --- | --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `42177bb8ef119545997dcf3ba274d2a67e1a97abe0741d724c13b9cefcc6b305` | Pi runs in place; the declaration is staged before the later witnesses can abort. |
| `scripts/ci/assert-go-test-json.py` | `859f230b502eabf965b28de2702dc4a5962a4482942fbfa7d1e1d77b68c08348` | Gated test and package failures must be terminal and ordered. |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `4a5d4098ad7b696208e649c4928f983503eb0b1efa9edb7cd6cb7ea355b66306` | Exact declaration; no conflicting receipts, prefilled targets or positive witness outcomes. |
| `daemon/internal/harness/pi/rpc.go` | `d785c8c34d32af0c265b6933262b320133bc006a39b0a05e8ae91507b488e58f` | The suffix carries only a fixed marker and an optional three-digit status. The sentinel and stop reason are unchanged. |
| `daemon/internal/harness/pi/rpc_test.go` | `fe8cd7f9c1a3d35bd2dd40cf472e33084d3b4c9c6422167addb50556484ecae0` | Validator rows pin the measured 401, reflected credential probes, bounded output and unchanged stop shapes. |
| `scripts/ci/test-linux-v2-evidence.py` | `01e1fcc21f2fa06e7a725c8a9bb22209b4f92389a6b0de36b75d31a9cdc10e1e` | Production handoffs, original order, partial export, declarations and both exporter boundaries. |
| `scripts/ci/test-linux-v2-go-events.py` | `637dc46d977133e66a0ee3a3d30031cdb9021255f68de354bf5b325ef36037df` | Adds out-of-order and post-terminal events to the gate rows. |

The pre-apply digest for `scripts/ci/test-linux-v2-evidence.py`,
`e32973b02a11ef0fba296c0d6da519de1a91b524101d1af1a9b7810def71d2e8`, bound pre-normalization bytes:
the conductor converted 2665 CRLF pairs to LF before committing `829c93bd` (the file's `text=auto`
attribute had let the `eol=lf` step pass it through), so it bound no committed revision. The
previous table also mistyped the fixture hash: the historical Bind fixture blob's SHA-256 is
`066d4062239f02e47a142bd83cddae8790624bf7a7eeb3a5a4cc252941fa8`. The revised rows above, not the
historical Bind, describe the consolidated fix.

## 2. Slice 2 (1B1): the placeholder mode and the named outcomes

### 2.1 The mode is decided from the staged bytes, in the fixture

`detect_placeholder_provider_credential` compares
`SYMMETRY_OPENAI_API_KEY` with the ratified scratch placeholder (`sk-scratch-dummy`) immediately
before the fixture writes that same value into `native_api_key_file` and B's
`~/.pi/agent/auth.json`. A non-placeholder value leaves `placeholder_provider_credential=0`.
The daemon never reads the credential to classify a stop.

### 2.2 Which legs are gated, and why the declaration precedes replay

The three receipt-required legs need a successful provider turn. The two crash legs also need
the detached child spawned after the model's commit. The placeholder path runs `pi_provider_action`
in its original slot after the marker-reader checks, then runs the Pi peer-negative check. It
records both unrun crash legs in their original order and writes the declaration before replay.

This changes what can be recovered after an abort, not the abort itself. The peer-negative test
uses the harness test, not the production fixture's journal scan. The crash-leg records only
append and log. Stage replay and ack loss, helper exit and response loss, receipt persist, PID
reuse, legacy replay, OpenCode health and its peer-negative check retain their order and inputs;
production witnesses can still be fenced by the shared StateDir's physical-release guard. Reader
stop and leaf removal still follow the case arm and are not reached on an earlier failure. Guest
teardown still gates export; the exporter can copy the validated declaration before refusing a
missing required durability witness. Nothing retires the Pi journal or fixes `reservation_durable`.

### 2.3 What a reviewer sees, in log, declaration and exported evidence

- Log: one line per gated leg —
  `credential-gated: <case> (credential_gated_<case>) is NOT proven: the ratified placeholder
  provider credential cannot produce an authentic Pi provider turn, so this leg is not counted as
  passed.` If the rest of the set completes, the closing line says it is not passed, then exits 1.
- The gated leg's event gate row reads
  `credential-gated TestProductionLinuxV2PiNativeProviderAction: run=1 pass=0 skip=0`, never
  `verified … pass=1`; the empty-column form does not exist.
- The declaration `credential-gated-legs.json` binds mode, credential variable name, Pi harness,
  tree digest, and each leg's exact test, named outcome and test result (`failed` for the attempted
  Pi leg, `not_run` for both crash legs). A complete failed export indexes the declaration and
  names the absent crash witnesses and provider receipts. A partial export may include the
  declaration without a manifest. Neither form proves provider success or a typed refusal.
- A present Pi witness must match the test, harness and source tree, have a boolean false for
  `provider_action_succeeded`, and carry no contradictory completed-owner or succeeded-provider
  outcome at the checked response fields. The exporter rejects both crash witnesses and all
  three provider receipt sources in gated mode; none can be matched to a valid receipt for
  an unproven leg. Pre-existing destinations for the three receipts and three gated witnesses
  fail before copying. The output check is exporter-level evidence, not a Bind observation.

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

The validator supplies this proof; the guest log is not the oracle for this text.

## 4. Measured rows (command → exit code)

| Command (working directory) | Exit | Note |
| --- | --- | --- |
| `go test ./internal/harness/pi/... -count=1` (`daemon/`) | 0 each | Windows and WSL Noble |
| `go test ./internal/app -run '^(TestNativeTerminalPayload\|Test.*OutputSettlement)' -count=1` (`daemon/`) | 0 | Windows, targeted terminal-payload consumers |
| `gofmt` + byte comparison (the two Go files in §1) | 0 | Windows, no diff |
| `go vet ./internal/harness/pi/... ./internal/app`; `go vet ./internal/harness/pi/...` | 0 each | Windows and Noble respectively |
| `python3 -B scripts/ci/test-linux-v2-go-events.py` | 0 | 10 tests, Resolute |
| `python3 -B scripts/ci/test-linux-v2-evidence.py` | 0 | 73 tests, Resolute; no skips |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | Resolute |

## 5. Red/green controls

Each named overlay changes one source file in a private copy of the §1 scripts; its test uses
the unchanged suite from that table. The focused `python3 -B <suite> <test>` commands returned
1 on the overlay and 0 on the restored source, in Resolute (see §6).

| Overlay | Changed input | Red result |
| --- | --- | --- |
| `u1-pi` / `u1-write` | Move the Pi call back to the tail / move the declaration write back past the reader | `test_shared_arm_gates_only_the_provider_legs` differs first at the Pi slot / lacks `write-declaration` before `stage-replay`. The forced replay exit 42 still bypasses reader stop. |
| `u1-export` | Move the declaration copy below durability validation | `test_partial_export_keeps_the_declaration_without_waiving_durability` gets `FileNotFoundError` on the exported member. On restored bytes the exporter exits 1 for the missing `ack-loss-replay-checkout_ready.json`, retains the exact declaration and emits no manifest. |
| `r9-input` / `r9-nested` | Delete the source-receipt rejection / delete the three positive owner/response outcome checks | The source-receipt and positive-outcome rows get exporter exit `0 != 1`. Inputs were the three stale `succeeded` receipts with unrelated run ID, or separate owner `completed`, nested owner `completed` and nested provider `succeeded` outcomes. |
| `r9b-receipt` / `r9b-witness` | Omit receipt names / witness names from the output preflight | The prefilled-target row gets exporter exit `0 != 1` for the corresponding three destinations without sources. |
| `r10-stage` / `r10-export` | Delete the live detector call / omit `credential_gated_args` from the exporter invocation | The staged placeholder is misclassified (`'0' != '1'`) / the declaration flag is absent (`0 != 1`). |
| `v3-harness` / `v3-result` / `v3-schema` | Drop the Pi harness check / drop the result check / default a missing result | Wrong harness, unrun attempted Pi, falsely failed crash leg or missing result passes the mutated exporter. |
| `v3-writer` | Swap the writer's `failed` and `not_run` | `test_declaration_helper_records_the_named_outcomes` gets an unequal declaration. |
| `v4-parent` / `v4-package` | Delete the run-before-fail guard / allow selected-package events after the package fail | Parent fail-before-run is accepted / early package fail and appended unrelated run or package output are accepted. |
| `v5` | Allow Python equality with 1 without the integer type check | Both `true` and `1.0` pass the mutated exporter. |
| `s1-read` / `s1-write` | Admit 0644 in `read_regular` / write the declaration with 0644 | `test_credential_gated_declaration_refuses_non_private_mode` sees exporter exit `0 != 1`; the writer row sees `'0o644' != '0o600'`. |

The historical 2C1 Windows overlay against `rpc.go` from `637f810a` exited 1 because
`TestValidatorReportsProviderStopError` lacked the 401 suffix. Replacing the projection call with
raw `message` also exited 1 in that row: reflected credentials and long multiline content reached
the text. The restored row exited 0. The Go source and test are unchanged by this consolidated fix
(§1); the current targeted suite result is in §4. Earlier shell and Python gate receipts are
partial and superseded by the consolidated §1 inputs and §4–§6 results.

## 6. Which userland each property needs (standing rule)

The unprivileged shell/Python runs used an Ubuntu Base 26.04 chroot on WSL; `/etc/os-release`
inside it returned `VERSION_CODENAME=resolute`, and the interpreter reported Python 3.14.4.
For S1, the writer used umask 022 so an ambient private default could not hide the writer
mutation. The exporter mode row used an otherwise valid document explicitly set to 0644.
These are real filesystem modes, not mocked ACL decisions. This closes the userland test gap
after the PR opened, not retroactively before opening. The Go tests ran on Windows and WSL
Noble, not in the chroot. Neither execution was a hosted acceptance run.

## 7. Does the conductor's Bind execute it?

The next `shared` Bind has not run. The two Python suites in §4 run all of the following unit
rows in the `daemon` job's **Check Linux v2 fixture contracts** step:

| Ruling | What `shared` exercises if reached | Only injected by the Python rows |
| --- | --- | --- |
| U1 | Placeholder order, named nonpass, early declaration and export after guest teardown; the actual reader/replay boundary | Forced replay exit 42 and a missing durability witness without a Pi witness file; reader after a stubbed complete arm |
| R9/R9b | Clean receipt/output boundary; Pi failure identity only if a witness file exists | Stale source receipts, prefilled targets without sources and positive witness response fields |
| R10 | Placeholder detection, key staging and export argument if the declaration was written | Non-placeholder test values, staged byte equality and invocation arguments with and without a declaration |
| V3/V5 | Correctly shaped Pi declaration, including results, source and integer version | Wrong harness, missing or inverted results, `true` and `1.0` |
| V4 | A normally ordered Go test failure terminal and closing package fail | Reverse ordering and events appended after the package fail |
| S1 | Writer mode and private-file reader when the declaration is exported | Mode 0644 and the altered writer |

A4: F6's pair pin still stubs the gated runner in the arm. The separate runner test uses the real
event gate and runner functions, not a live guest, request preparation, relay or Control readback.
The Pi validator is a Go test in `daemon`, not a Bind leg. The real-credential arm is reserved for
the approved `main` dispatch.

## 8. Gates

The handoff gate receipt identifies the exact scratch commit, parent, changed files, digests and
exit codes. Its record gates compare the dispatch base in the header to that scratch commit;
its comment and duplication checks cover the PR diff. The document counts previously attributed
to the apply and review stages are historical and superseded. No full Go suite is claimed.

## 9. Next Bind (U3)

Report the `shared` exit status; the gated Pi outcome verbatim in the log and exported member;
the actual `shared/*` members and whether the reader emitted its Result; and the first unexpected
failure. `reservation_durable` is a separate goal and still unmeasured on these bytes. An earlier
physical-release guard can also stop the run; report the observed failure, not the expected one.

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
- The historical Bind did not complete the non-provider legs after `reservation_durable`.
  The consolidated fix makes no claim that any later row runs or passes.

## 11. Retained artifacts

No raw event stream or credential is retained in the repository. The section below is the
verbatim historical Bind that **did not meet A1**. It supports U1, not acceptance on the fixed
bytes. Its §1 reference denotes the input table at the old run, not the revised digests above.

## Bind

[Run 46020141750](https://github.com/wxxb789/symmetry/actions/runs/46020141750) was the sole
placeholder-credential run of `shared` on PR #308 (attempt 1). The
[partial evidence ZIP](https://github.com/wxxb789/symmetry/actions/runs/46020141750/artifacts/15208232294)
has SHA-256 `fcebc8e405d028de4e6d03cea5a46721ce31342de6497c21c03dca48e3f599b9`.
The scratch branch was deleted; the Actions API still returned `total_count=1` for the run head.

| Input | Run head `ad8f6557ff567454e4917f82ce716979a97719c6` | PR head `db213f88c7e55352151cb13ce17c19596366b563` |
| --- | --- | --- |
| All PR-tracked paths (including the §1 inputs) | Byte-identical to parent | Same bytes |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | Scratch-only blob `3e85d193704205f9e1ba1736b13867f6727939cc` | Absent |
| `scripts/ci/scratch-gh-shim` | Scratch-only blob `6c2598c146e85875c1868515280fcaee8bec110d` | Absent |

1. **Case set:** `Case set shared exited 1`; 22 exact `verified … run=1 pass=1 skip=0` rows. The first failing witness line (log 3109) was `production_linux_v2_native_witness_test.go:224: stage reservation_durable recovery did not preserve the correct physical Journal state: pre-create abort tombstone reports a target or leaf creation`. The first 21 green rows match Bind #303 in order. With Pi deferred, the Pi peer-negative check was newly green and the first replay stage became reachable; there is no prior replay pass in that Bind.
2. **Credential-gated outcome:** The log named `credential_gated_helper_sigkill_detached_child` and `credential_gated_daemon_sigkill_helper_survives_detached_child` (2530–2531), neither counted as a pass. The failure preceded the deferred Pi leg: `credential_gated_pi_provider_action` was not emitted, and none of the named outcomes was published in the partial ZIP.
3. **Export and reader:** The full redacted export did not land (3389). The ZIP contains only `shared/stage-crash-replay-reservation_durable.json` (5728 B) and `shared/unprovisioned-owner-refusal.json` (691 B); it has no `shared/manifest.json` or `shared/synthetic-goal-cleanup.json`. The reader started, but `stop_readback_reader` and its Result row were not reached.
4. **Bounded provider error:** The proof is the red-first `TestValidatorReportsProviderStopError` unit row in `daemon/internal/harness/pi/rpc_test.go` (see §5), not the Bind log. The placeholder Bind did not meet A1's export or reader conditions.
