# rf-flake-omo-attach-ack-deadline — evidence record

Base `9d113eb6` (`origin/main` at dispatch). Contract: `goal-rf-flake-omo-attach-ack-deadline.md`
(GDD state root, external to the repository). Plan:
`docs/plans/2026-11-24-rf-flake-omo-attach-ack-deadline-plan.md`. Shape references: the W4-13
plan/evidence pair (the 5.2, 6.3 and §9 rows this slice closes) and the W6-12 pair (record format).

**Subject revision (binding):** the worktree at `9d113eb6` with
`daemon/internal/app/omo_attach_ack_test.go` sha256
`24931a59649d80e92c5e58bb4566072b0e949ad2d9a1a34cb9381f9143006a19` (`git diff --numstat` 236/21).
Every gate, pin and mutant row below was executed on these bytes unless the row says otherwise; the
apply stage re-ran every row after the comment rewording and the one-log-per-trajectory change
moved the test bytes from the implement-stage revision
`d3f5001647c6e7727a4da487c90ce0cc776759117a2de0db87cfd53cd60e72c9` (234/21).

## 1. Method and toolchain

- Windows x64, `go1.27.4 windows/amd64`; WSL Ubuntu-24.04 (`accept`), `go1.27.0 linux/amd64` for
  the `-race` oracle. Storage: the goal worktree `Q:/repos/wt-flake-omo-attach/symmetry`; two
  disposable clones outside the repository under `Q:/scratch-rf-flake-omo-attach/` — `before` (the
  P1-only arm, the pre-fix repeat, P2-before) and `after` (the final test file, the pins, the
  mutants). The repository tree holds only the final state; every scratch edit was restored and
  hash-checked (§5).
- Every command ran foreground and bounded with `timeout`; the loaded repeats ran beside one
  bounded heavy carrier started in the same shell and reaped by `wait`, never beside a full-suite
  measurement. No job was left pending. The host is shared (16 cores); wall times vary between runs
  and are quoted per row.

## 2. What changed

| file | numstat (add/del) | change |
| --- | --- | --- |
| `daemon/internal/app/omo_attach_ack_test.go` | 236 / 21 | P1 timeline; F1 claim-anchored `DeadlineAt`; F2 closed fail-closed trajectory set; the shape-B pin mode; apply stage: comment rewrite, one trajectory log line per run |
| `docs/plans/2026-11-24-rf-flake-omo-attach-ack-deadline-plan.md` | new 163 lines | the plan, decisions and the command/count table |
| `docs/evidence/rf-flake-omo-attach-ack-deadline-2026-11-24.md` | new 207 lines | this record |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | 0 / 18, 39 / 0 | the tracked entry moves to the archive with the closure statement |

No committed production change; the only product edits were temporary mutants restored
byte-identical (§5). The added comments are all ≤2 lines (one four-line comment block was trimmed
before the final gate runs, which is why the earlier intermediate runs below belong to a superseded
revision). The one-log guard is A/B-verified on the starved-B delegation: a 2 s arm forced into the
refusal case (scratch offset `-1 s`) logs the identical trajectory line twice on the pre-apply
bytes and once on the binding bytes (`apply-s2-delegated-{old,new}.log`).

## 3. P1 timeline and the intended trajectory

Focused run on the binding bytes, exit 0 (apply stage, `20.968s`):

```
--- PASS: .../caller_cancel (0.46s)
--- PASS: .../caller_deadline (2.20s)
    attachment trajectory: requests=[GET PUT] ... timeline=t0=+0.000s(until=+2.000s) claim=+0.003s(until=+1.997s) first_request=+0.109s(until=+1.891s) put=+0.110s(until=+1.890s) after_put_enter=+0.112s(until=+1.888s) after_put_return=+2.002s(until=-0.002s)
--- PASS: .../admission_deadline (2.04s)
    attachment trajectory: requests=[GET PUT] ... timeline=t0=+0.000s(until=+2.002s) claim=+0.002s(until=+2.000s) first_request=+0.092s(until=+1.910s) put=+0.092s(until=+1.910s) after_put_enter=+0.099s(until=+1.903s) after_put_return=+2.003s(until=-0.001s)
--- PASS: .../admission_deadline_refused (0.02s)
    attachment trajectory: requests=[] native_calls=[] timeline=t0=+0.000s(until=-0.998s) claim=+0.002s(until=-1.000s)
--- PASS: .../request_deadline (15.78s)
```

Reading: F1 moved the anchor to `Claim` — the `admission_deadline` arm's `claim` mark carries the
full `+2.000s` margin (the pre-fix arm charged fixture setup and the claim round trip against the
budget; the loaded reds' `claim` marks show 18-183 ms lost there). The intended trajectory's
`deadline - first request` margin on an idle host is **1.910 s** (admission) and **1.891 s**
(caller); the hold returns at the deadline. `admission_deadline_refused` is the shape-B pin.

## 4. The bounded repeats and the recovered shapes

### 4.1 Pre-fix loaded repeat (`before` clone, P1-only arm)

```
GOMAXPROCS=8 timeout 30m go test -count=200 -timeout 1500s -run 'TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline' ./internal/app/
```
beside `timeout 25m go test -count=1 -timeout 1400s ./internal/state/ ./internal/harness/... ./internal/execution ./internal/notification` (carrier exit 0, 38 s of the 623 s window).

| run | exit | wall | reds / 200 | red families |
| --- | ---: | ---: | ---: | --- |
| pre-fix loaded | 1 | 622.797 s | 22 | 21 starved-A (`requests=[]`, `native_calls=[start details open details close wait]`, `context deadline exceeded`) + 1 in-flight readback (`requests=[GET]`, PUT refused client-side: `perform request: Put …: context deadline exceeded`) |

Every red's P1 timeline is in `red-timelines-before.txt` beside the run log
(`Q:/scratch-rf-flake-omo-attach/`). All 22 carry only `t0` and `claim` marks (no request reached
the server within the budget) except the in-flight one:
`t0=+0.000s(until=+2.000s) claim=+0.029s(until=+1.971s) first_request=+1.551s(until=+0.449s)
after_put_enter=+2.955s(until=-0.955s)` — its `first request - t0 = 1.551 s < 2 s`, so the
recorded failure cause is the post-claim pipeline wall clock, not the pre-claim stages the
contract's shape-A hypothesis named (the pipeline is starved *before* the GET is dispatched; when
the GET does get in, the PUT is refused past the deadline). Both recorded shapes are wall-clock
misses, not product defects.

### 4.2 Post-fix repeats on the binding revision

| run | exit | wall | reds / 200 | note |
| --- | ---: | ---: | ---: | --- |
| loaded | 0 | 482.614 s | 0 | apply-stage re-run; carrier rounds ended 07:45:43–07:51:50, covering the measurement 07:43:52–07:51:58 |
| companion with `-v`, no carrier | 0 | 439.569 s | 0 | apply-stage re-run; 400 trajectory lines: 196 intended, 4 starved-A, 200 shape-B pins |

Companion trajectory histogram (`-v`, binding revision):

| trajectory | count | margin `deadline - first request` |
| --- | ---: | --- |
| intended `[GET PUT]` | 196 | min **0.923 s**, max 1.968 s, mean 1.915 s |
| starved-A `[]` with `open`+`close` | 4 | — |
| shape-B pin (`admission_deadline_refused`) | 200 | — |

The 4 starved arm iterations on a nominally idle host are the direct evidence that the 2 s budget
is not a property the host honours; the closed set is what makes those outcomes green without
weakening any postcondition. Superseded intermediate runs (extension/relaxation and comment states,
kept for the audit trail): a 585.260 s loaded run 0 red/200 with 5 of 11 minutes of carrier; a
598.8 s companion run 2 red/200 (both late-served: `[GET, PUT]` with the first served request
13 ms / 27 ms past the anchored deadline and the PUT error the hijack `EOF`) plus one false red at
the intended branch's `afterPut` *entry* — the entry mark is not a liveness witness (the PUT leg's
resolution can itself be starved past the deadline), so the assertion binds the served requests and
the hold's return (plan D6). The later 2-red family is what plan D3a accepts under its own strict
predicates.

## 5. Pins and mutants (scratch copies, restored byte-identical)

Product files at base: `goal_delivery.go` sha256 `89a9857de64dab9ff580829d39217aa0e6ca57ea0a75fb842170d4eb35f5ab07`,
`goal_admission_tx.go` sha256 `76bcca94a0e0d264077df4d80a2a11bec0858f710065e1e8a263264894793e92`,
`harness_test.go` sha256 `b95b4f6976872760d8d92c9ceb0211c3b5e395f5694c11565ce9a5ab44ee944a`; every
restore below was verified against those values (or a restored copy of the binding bytes for the
test file). All rows except P2-before were executed on the binding revision (the apply stage re-ran
every row on the post-apply bytes); P2-before's subject is the pre-fix arm in the `before` clone
(its assertion path is the base `:629` line, which the binding revision keeps verbatim in the
intended branch).

| id | scratch edit | command (`after` clone unless noted) | observed | restore |
| --- | --- | --- | --- | --- |
| P2-before | `harness_test.go` fake `Open` sleeps 2.5 s | `-count=1 -v -run '…/admission_deadline'` (`before`) | exit 1, `attachment requests=[], want [GET PUT]`, `native_calls=[start details open details close wait]`, `context deadline exceeded`, timeline `t0/claim` only | sha256 `b95b4f69…` |
| P2-after | same edit | same | exit 0, accepted starved-A: `requests=[] native_calls=[start details open details close wait]`, timeline `t0/claim` only | sha256 `b95b4f69…` |
| shape-B pin | committed `admission_deadline_refused` mode (offset `-1 s`) | `-count=1 -v -run '…/admission_deadline_refused'` | exit 0, `requests=[] native_calls=[]`, refusal classification asserted; green in every 200-count repeat (200 pins per run) | committed |
| in-flight pin | fixture transport sleeps 2.1 s before the PUT round trip | `-count=1 -v -run '…/admission_deadline$'` | exit 0, accepted in-flight readback: `requests=[GET]`, `first_request=+0.058 s(until=+1.944 s)`, `after_put_return=+2.159 s`, PUT refused by the expired budget | binding bytes |
| M1 | arm's `afterPut` returns immediately | `-count=1 -v -run '…/admission_deadline$'` ×2 | exit 1 ×2, `requests=[GET PUT GET]`, `native_calls=[… start_turn wait_turn …]`, `terminal_state=completed`, the classifier naming the trajectory outside the closed set | binding bytes |
| M2 | product `goal_delivery.go` uncertain readback on `controlContext(context.WithoutCancel(ctx))` (guard included) | same ×2 | exit 1 ×2, `requests=[GET PUT GET]`, `start_turn`, `terminal_state=completed` | sha256 `89a9857d…` |
| M3 | product `goal_admission_tx.go` elapsed-deadline refusal removed | `-count=1 -v -run '…/admission_deadline_refused'` | exit 1, the pin's own refusal assertion: `deadline refusal did not precede native launch: requests=[] native_calls=[start details open details close wait]` | sha256 `76bcca94…` |
| M1′ | M1 plus a 30 s anchor (removes host luck) | same | exit 1, `requests=[GET PUT GET]`: the readback appears whenever the budget is live, so the missing hold cannot hide behind starvation | binding bytes |

Earlier attempts on superseded revisions (audit trail): M1 also redded 3/3 with the same
`[GET PUT GET] + turn` shape; one M1 attempt on a very slow host served no request at all and
presented as the accepted starved-A shape (7.46 s arm) — which is why M1′ exists; M2 redded 3/3.
CI on dedicated runners (`daemon` and `daemon-windows`) remains the oracle for the M1-M3 rows, as
the contract's mutant table states.

## 6. Assertion census (base `9d113eb6` vs HEAD, same file)

| metric | base | HEAD |
| --- | ---: | ---: |
| failure sites (`t.Fatalf`/`t.Errorf`) | 28 | 40 |
| `assertRequests(` call sites | 8 | 9 |
| `assertStoppedWithoutTurn(` call sites | 4 | 7 |
| `assertExactPending(` call sites | 6 | 9 |
| `assertTerminalFailure(` call sites | 0 | 4 (new helper) |
| `assertRefusedBeforeNativeLaunch(` / `assertServedBudgetPlacement(` | 0 / 0 | 2 / 1 (new) |

No assertion was removed or weakened: the two 2 s arms' base trio
(`assertRequests([GET, PUT])`, `assertStoppedWithoutTurn`, `assertExactPending`) survives verbatim
in the intended branch, and every other accepted branch calls `assertStoppedWithoutTurn` (or the
refusal branch's stricter native-call-set equality) plus `assertExactPending`/the delivery-set
equality plus `assertTerminalFailure`, so each accepted trajectory asserts strictly more than the
base line did. The request set is closed to the five recovered families (plan D3/D3a); a third
request, a post-expiry readback, a turn start, a changed journal, or a `[GET]` with no PUT attempt
stays red.

## 7. Gates

| gate | command (cwd) | exit | result |
| --- | --- | ---: | --- |
| loaded repeat | `GOMAXPROCS=8 timeout 30m go test -count=200 -timeout 1500s -run 'TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline' ./internal/app/` + carrier (`daemon/`, worktree) | 0 | 0 red / 200, 482.614 s, carrier overlap full window |
| companion | same command plus `-v`, no carrier | 0 | 0 red / 200, 439.569 s; 196 intended with margins, 4 starved-A, 200 pins |
| WSL `-race` | `timeout 30m wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd /mnt/q/repos/wt-flake-omo-attach/symmetry/daemon && go test -race -count=1 -timeout 1200s -run TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget ./internal/app/'` | 0 | `ok … 22.846s`, 59 s wall, no race report |
| focused family | `go test -count=1 -timeout 600s -run 'TestOmOAttachAckRegression' ./internal/app/` (`daemon/`) | 0 | `ok … 24.778s` (every test in the owned file, including the unmodified attach-ack family) |
| vet | `go vet ./...` (`daemon/`) | 0 | clean |
| format | `gofmt -l internal/app/omo_attach_ack_test.go` | 0 | empty |
| diff hygiene | `git diff --check` (worktree root) | 0 | clean |
| census | §6 | — | no assertion removed; every accepted trajectory strict |
| citation existence | emulation commit in the `after` clone: `python3 scripts/ci/check-citation-existence.py --repo . --base 9d113eb6 --head HEAD` | 0 | `1 changed document(s) pass the path and symbol check (573 path citation(s))`; three pre-existing ignore-covered notices on `docs/archive/CLOSED-TODO.md` |
| changed-file clone gate | `python3 scripts/ci/jscpd-changed.py --repo . --base 9d113eb6 --head HEAD` | 2 | **not run locally**: the pinned `jscpd` is not installed in this worktree (no `node_modules`); the CI `contracts` job owns the gate. The added blocks are ≤6 lines against the 14-line/275-token threshold, and the plan and this record share no verbatim block; recorded as a verification limit, not a pass |

Not run here: CI `daemon` / `daemon-windows` on a PR head (the stage must not push; conductor-owned
measurement), and the full Windows `go test ./...` (the contract's gates are the focused repeat, the
pins, the WSL `-race` oracle and CI; the touched package's focused family and `go vet ./...` are
green).

## 8. Residual risks and known limits

- **The accepted set is the recovered inventory** (plan D3/D3a): intended, late-served, starved-A,
  in-flight readback, starved-B. A starved host can in principle recover a further fail-closed
  micro-shape — for example a readback GET aborted by the budget with no PUT attempt
  (`requests=[GET]`, `putErr == nil`) — and such a shape stays red by design, needing the same
  treatment as the recovered families. None was observed in the runs recorded here. Reverting to the
  narrower three-shape reading of the contract is a two-branch deletion (plan D3a).
- **Late-served coverage:** no run on the binding revision has executed the late-served branch —
  the companion histogram above is 196 intended / 4 starved-A / 200 pins with no late-served, and
  the only two late-served observations remain the reds of the superseded intermediate run. The
  shape is the net/http write-after-expiry race, so a deterministic scratch pin would have to
  suppress the platform's own expired-context rejection; none was constructed. The branch stands as
  a reasoned acceptance under plan D3a pending the owner adjudication of the accepted-set width
  (D3a's reading (b) against the contract's three named shapes).
- The `[GET]` family is accepted only when the PUT was actually attempted and refused by the expired
  budget (`errors.Is(putErr, context.DeadlineExceeded)`), so a product that skips the PUT after the
  readback stays red.
- M1's own row needs a host that serves the requests inside the budget; on a starved host the
  no-hold fixture presents as the accepted starved-A shape. The mutant rows' oracle is CI, as the
  contract states; M1′ proves the detection deterministically.
- The loaded repeat runs the `admission_deadline` subtest only; `request_deadline` remains a 15 s
  production-bound arm (W4-13 §5.2) and was not converted.
- The `afterPut` *entry* mark is recorded but not asserted: the PUT leg's resolution can be starved
  past the deadline (observed once), so liveness binds the served requests and the hold's return.
