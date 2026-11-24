# rf-flake-omo-attach-ack-deadline — evidence record

Base `9d113eb6` (`origin/main` at dispatch). Contract: `goal-rf-flake-omo-attach-ack-deadline.md`
(GDD state root, external to the repository). Plan:
`docs/plans/2026-11-24-rf-flake-omo-attach-ack-deadline-plan.md`. Shape references: the W4-13
plan/evidence pair (the 5.2, 6.3 and §9 rows this slice closes) and the W6-12 pair (record format).

**Subject revision (binding):** the worktree at `9d113eb6` with
`daemon/internal/app/omo_attach_ack_test.go` sha256
`ff43ba46a66b00260f7a213458b1b3bbdf516b24ad89c2b0427452ed5b3186ae` (`git diff --numstat`
against the PR base `5c297c25`: 222/21). The fix r2 stage (§10) removed 14 of the 15 added comment lines and kept one
on the claim anchor, so the final bytes differ from the `24931a59…` revision (236/21) that most
rows below name only in comments; §10 re-runs the loaded repeat, the companion, the late-served
pin, the focused family, `go vet` and the hygiene gates on the final bytes. Earlier revisions: the
apply stage re-ran every row after the comment rewording and the one-log-per-trajectory change
moved the test bytes from the implement-stage revision
`d3f5001647c6e7727a4da487c90ce0cc776759117a2de0db87cfd53cd60e72c9` (234/21).

## 1. Method and toolchain

- Windows x64, `go1.27.4 windows/amd64`; WSL Ubuntu-24.04 (`accept`), `go1.27.0 linux/amd64` for
  the `-race` oracle. Storage: the goal worktree `<worktree>`; two disposable clones outside the
  repository under `<scratch>` — `before` (the P1-only arm, the pre-fix repeat, P2-before) and
  `after` (the final test file, the pins, the mutants). The repository tree holds only the final
  state; every scratch edit was restored and hash-checked (§5). `<worktree>` and `<scratch>` are the
  placeholder names for the goal worktree and the disposable scratch root; this record and the plan
  carry no absolute paths, and the commands below quote the runs with those placeholders
  substituted.
- Every command ran foreground and bounded with `timeout`; the loaded repeats ran beside one
  bounded heavy carrier started in the same shell and reaped by `wait`, never beside a full-suite
  measurement. No job was left pending. The host is shared (16 cores); wall times vary between runs
  and are quoted per row.

## 2. What changed

| file | numstat (add/del) | change |
| --- | --- | --- |
| `daemon/internal/app/omo_attach_ack_test.go` | 222 / 21 | P1 timeline; F1 claim-anchored `DeadlineAt`; F2 closed fail-closed trajectory set; the shape-B pin mode; fix r2: the 15 added rationale comment lines cut to the single claim-anchor line |
| `docs/plans/2026-11-24-rf-flake-omo-attach-ack-deadline-plan.md` | new 194 lines | the plan, decisions and the command/count table |
| `docs/evidence/rf-flake-omo-attach-ack-deadline-2026-11-24.md` | new 494 lines | this record |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | 0 / 18, 40 / 0 | the tracked entry moves to the archive with the closure statement |

Counts are `git diff --numstat 5c297c25` (the PR merge-base; `origin/main` has since advanced to
`477e4c62` by a merge this branch does not carry). No committed production change; the only
product edits were temporary mutants restored byte-identical (§5). The final test file adds one
comment line (the claim-anchor coupling); the fixture-design rationale the earlier drafts carried
inline lives in plan D1/D3/D4/D6 and §§3/8/10 (review r2 P3-6). The one-log guard is A/B-verified
on the starved-B delegation: a 2 s arm forced into the refusal case (scratch offset `-1 s`) logs
the identical trajectory line twice on the pre-apply bytes and once on the `24931a59…` bytes
(`apply-s2-delegated-{old,new}.log`).

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
(`<scratch>`). All 22 carry only `t0` and `claim` marks (no request reached
the server within the budget) except the in-flight one:
`t0=+0.000s(until=+2.000s) claim=+0.029s(until=+1.971s) first_request=+1.551s(until=+0.449s)
after_put_enter=+2.955s(until=-0.955s)` — its `first request - t0 = 1.551 s < 2 s`, so the
recorded failure cause is the post-claim pipeline wall clock, not the pre-claim stages the
contract's shape-A hypothesis named (the pipeline is starved *before* the GET is dispatched; when
the GET does get in, the PUT is refused past the deadline). Both recorded shapes are wall-clock
misses, not product defects.

### 4.2 Post-fix repeats (apply-stage revision `24931a59…`; the fix-r2 re-runs on the final bytes are §10.3)

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

The fix-r2 re-runs of both commands on the final bytes are §10.3 (0 red / 200 each; the same
196/4/200 companion split). The 4 starved arm iterations on a nominally idle host are the direct evidence that the 2 s budget
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

Table columns: `CI job that must redden` names the job whose run of the committed test family
catches the mutated state (every job runs as the runner's non-root user, so no row depends on
root); rows marked `oracle gap` live only in the scratch copies — no CI job executes them today,
and the committed M1-M3 rows carry the CI oracle for their families.

| id | scratch edit | command (`after` clone unless noted) | CI job that must redden (runner user) | observed | restore |
| --- | --- | --- | --- | --- | --- |
| P2-before | `harness_test.go` fake `Open` sleeps 2.5 s | `-count=1 -v -run '…/admission_deadline'` (`before`) | `daemon`, `daemon-windows` (non-root) | exit 1, `attachment requests=[], want [GET PUT]`, `native_calls=[start details open details close wait]`, `context deadline exceeded`, timeline `t0/claim` only | sha256 `b95b4f69…` |
| P2-after | same edit | same | n/a — pin (green by design) | exit 0, accepted starved-A: `requests=[] native_calls=[start details open details close wait]`, timeline `t0/claim` only | sha256 `b95b4f69…` |
| shape-B pin | committed `admission_deadline_refused` mode (offset `-1 s`) | `-count=1 -v -run '…/admission_deadline_refused'` | n/a — pin; a removal reddens through M3 | exit 0, `requests=[] native_calls=[]`, refusal classification asserted; green in every 200-count repeat (200 pins per run) | committed |
| in-flight pin | fixture transport sleeps 2.1 s before the PUT round trip | `-count=1 -v -run '…/admission_deadline$'` | oracle gap (scratch pin) | exit 0, accepted in-flight readback: `requests=[GET]`, `first_request=+0.058 s(until=+1.944 s)`, `after_put_return=+2.159 s`, PUT refused by the expired budget | binding bytes |
| late-served pin (fix r1) | fixture holds the PUT's server-side handling to 50 ms past the anchored deadline (body buffered at handler entry; `<scratch>/fix-pin-lateserved.patch`, pin bytes sha256 `2ed354019cd225f4c5a3e75e23da717c28b93785a0b90a389cf08daa4c860d7d`) | `GOMAXPROCS=8 -count=200 -timeout 1500s -v -run '…/admission_deadline_lateserved'` beside one carrier at a time | oracle gap (scratch pin); M4/M5 mutate its expectations | exit 0, 200/200 green: 197 late-served with the PUT entry 50-55 ms past the anchored deadline (`until=-0.050..-0.055 s`; first request live by +1.709..+1.966 s) + 3 accepted starved-A (`requests=[]`, native open+close) | `24931a59…`; the final-bytes rebuild and re-run are §10.1-§10.2 |
| M1 | arm's `afterPut` returns immediately | `-count=1 -v -run '…/admission_deadline$'` ×2 | `daemon`, `daemon-windows` (non-root) | exit 1 ×2, `requests=[GET PUT GET]`, `native_calls=[… start_turn wait_turn …]`, `terminal_state=completed`, the classifier naming the trajectory outside the closed set | binding bytes |
| M2 | product `goal_delivery.go` uncertain readback on `controlContext(context.WithoutCancel(ctx))` (guard included) | same ×2 | `daemon`, `daemon-windows` (non-root) | exit 1 ×2, `requests=[GET PUT GET]`, `start_turn`, `terminal_state=completed` | sha256 `89a9857d…` |
| M3 | product `goal_admission_tx.go` elapsed-deadline refusal removed | `-count=1 -v -run '…/admission_deadline_refused'` | `daemon`, `daemon-windows` (non-root) | exit 1, the pin's own refusal assertion: `deadline refusal did not precede native launch: requests=[] native_calls=[start details open details close wait]` | sha256 `76bcca94…` |
| M1′ | M1 plus a 30 s anchor (removes host luck) | same | oracle gap (scratch variant); M1 carries the CI jobs | exit 1, `requests=[GET PUT GET]`: the readback appears whenever the budget is live, so the missing hold cannot hide behind starvation | binding bytes |
| M4 (fix r1) | late-served branch: the PUT-error allowance narrowed to `io.EOF` only | `-count=1 -v -run '…/admission_deadline_lateserved'` | oracle gap (mutates the scratch pin) | exit 1, `late-served trajectory did not carry a lost-acknowledgement or expired PUT: PUT error=perform request: Put "…/session": context deadline exceeded` — the class the pin serves | binding bytes |
| M5 (fix r1) | late-served branch: the terminal expectation changed to `attach native Goal session refused` | same | oracle gap (mutates the scratch pin) | exit 1, `terminal classification = {"error":"attach native Goal session: perform request: Put "…/session": context deadline exceeded","reason":"unknown_outcome","stage":"goal_admission","summary":"daemon failed during goal_admission"}, want … carrying "attach native Goal session refused"` | binding bytes |
| readback-crossing probe (fix r1) | the same hold moved onto the first served request (the readback GET) | same | oracle gap (characterization, not a mutant) | exit 1 by design: `in-flight readback trajectory did not fail the PUT on the expired budget: PUT error=<nil>`, `first_request=+2.054s(until=-0.050s)` | binding bytes |

Earlier attempts on superseded revisions (audit trail): M1 also redded 3/3 with the same
`[GET PUT GET] + turn` shape; one M1 attempt on a very slow host served no request at all and
presented as the accepted starved-A shape (7.46 s arm) — which is why M1′ exists; M2 redded 3/3.
The fix-r1 rows (the late-served pin, M4, M5, the probe) run on the shipping bytes plus
`<scratch>/fix-pin-lateserved.patch` (§9.1); CI on dedicated runners (`daemon` and `daemon-windows`)
remains the oracle for the M1-M3 rows, as the contract's mutant table states.

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

Apply-stage rows on `24931a59…`; the fix-r2 re-runs of the loaded repeat, the companion, the
focused family and `go vet` on the final bytes are §10.3 (the other rows were not re-run: the byte
delta is comments only).

| gate | command (cwd) | exit | result |
| --- | --- | ---: | --- |
| loaded repeat | `GOMAXPROCS=8 timeout 30m go test -count=200 -timeout 1500s -run 'TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline' ./internal/app/` + carrier (`daemon/`, worktree) | 0 | 0 red / 200, 482.614 s, carrier overlap full window |
| companion | same command plus `-v`, no carrier | 0 | 0 red / 200, 439.569 s; 196 intended with margins, 4 starved-A, 200 pins |
| WSL `-race` | `timeout 30m wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd <worktree>/daemon && go test -race -count=1 -timeout 1200s -run TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget ./internal/app/'` | 0 | `ok … 22.846s`, 59 s wall, no race report |
| focused family | `go test -count=1 -timeout 600s -run 'TestOmOAttachAckRegression' ./internal/app/` (`daemon/`) | 0 | `ok … 24.778s` (every test in the owned file, including the unmodified attach-ack family) |
| vet | `go vet ./...` (`daemon/`) | 0 | clean |
| format | `gofmt -l internal/app/omo_attach_ack_test.go` | 0 | empty |
| diff hygiene | `git diff --check` (worktree root) | 0 | clean |
| census | §6 | — | no assertion removed; every accepted trajectory strict |
| citation existence | emulation repo of the final tree: `python3 scripts/ci/check-citation-existence.py --repo . --base 28b471f --head HEAD` | 0 | `1 changed document(s) pass the path and symbol check (580 path citation(s))`; three pre-existing ignore-covered notices on `docs/archive/CLOSED-TODO.md` (fix r2 re-measurement; §10.4) |
| changed-file clone gate | `python3 scripts/ci/jscpd-changed.py --repo . --base 9d113eb6 --head HEAD` | 2 | **not run locally**: the pinned `jscpd` is not installed in this worktree (no `node_modules`); the CI `contracts` job owns the gate. The added blocks are ≤6 lines against the 14-line/275-token threshold, and the plan and this record share no verbatim block; recorded as a verification limit, not a pass |

Not run here: CI `daemon` / `daemon-windows` on a PR head (the stage must not push; conductor-owned
measurement), and the full Windows `go test ./...` (the contract's gates are the focused repeat, the
pins, the WSL `-race` oracle and CI; the touched package's focused family and `go vet ./...` are
green).

## 8. Residual risks and known limits

- **The accepted set is the recovered inventory** (plan D3/D3a, amended by the conductor ruling on
  review r1): intended, late-served, starved-A, in-flight readback, starved-B — each accepted family
  carrying an executed pin or a binding-bytes loaded red. A starved host can in principle recover a
  further fail-closed micro-shape — a readback GET aborted by the budget before any PUT attempt
  (`requests=[GET]`, `putErr == nil`) — and such a shape stays red by design, needing the same
  treatment as the recovered families. The fix-stage readback-crossing probe (§9) executes that
  residual shape deterministically: exit 1 with `PUT error=<nil>`. Reverting to the narrower
  three-shape reading of the contract is a two-branch deletion (plan D3a, rejected alternative).
- **Late-served coverage (closed by review r1):** the late-served branch is executed
  deterministically on the shipping bytes by the fix-stage pin (§9): the fixture holds the PUT's
  server-side handling to 50 ms past the anchored deadline and the pin runs green under load
  (200/200, 197 of them taking the late branch), plus once under WSL `-race`; the pin's own
  expectation mutants (M4/M5) red, naming the observed PUT error class (`context deadline exceeded`)
  and the real terminal classification. The two natural observations from the superseded
  intermediate run remain the only unforced reds of the family.
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
- **Late-branch PUT-error allowance (review r2 P3-4):** the late branch admits `io.EOF` or
  `context.DeadlineExceeded`; the deterministic pin and the M4 control both serve only
  `context.DeadlineExceeded` (M4 reds under an EOF-only allowance), so the EOF alternative is
  accepted but executed by no pin. Both are lost-acknowledgement classes, not a wildcard; recorded
  rather than tightened because the unforced superseded late-served reds carried the hijack `EOF`
  class.
- **`caller_deadline` has no own pin (review r2 P3-5):** every recorded pin and repeat selects the
  `admission_deadline` subtests (Go's unanchored `-run` never matches `caller_deadline`). Review r2's
  scratch probe applied the same stall to `caller_deadline` and took the same branch green 3/3
  (`put=+2.05s(until=-0.050s)`) with the same classification, so that arm's adoption of the closed
  set is low-risk but unpinned; recorded as a residual.
- **Starved-B wording (review r2 P3-7):** the contract's F2 bullet says starved-B asserts "no turn,
  exact pending"; the shipped `assertRefusedBeforeNativeLaunch` instead requires zero
  pending/delivered/retired deliveries, which is the correct reading — the product queues the
  session-attach delivery only after `resolveGoalAdapter` (`goal_admission_tx.go:216`), so an
  already-elapsed deadline refuses before any delivery exists and "exact pending" would have nothing
  to compare against. Plan D3/D5 and the shipped assertion match the product; the contract sentence
  is the stale one, recorded so the ruling is not misread as a weakening.

## 9. Fix stage (review r1 findings, 2026-11-25)

Review r1 held `DECISION_NEEDED` on the accepted-set width (SPEC-A01) and the missing late-served
execution on the shipping bytes (STD-A01), with three P3 record defects. The conductor's ruling of
2026-11-25 settled all five: keep the five closed families with F2 amended to "the accepted set is
the trajectories P1 recovers or that a deterministic P2 pin executes", pin late-served
deterministically or delete the branch, and correct the records. The amended rule and its rationale
are recorded once, in the plan's D3a; this section carries the fix stage's evidence.

### 9.1 Late-served pin (STD-A01)

Scratch edit on the shipping bytes (`<scratch>/fix-pin-lateserved.patch`, numstat vs the shipping
file 43/1; pin file sha256 `2ed354019cd225f4c5a3e75e23da717c28b93785a0b90a389cf08daa4c860d7d`,
patch sha256 `316ad1a7819d8d11ce8ff170ff89fcaa9eb39e1e5cd674823ef81f3b9f1cff55`): a fixture hook stalls a PUT's
server-side handling before the `put` mark — the handler buffers the request body at entry and waits
for a release at the anchored deadline + 50 ms — so the recorded PUT entry lands past the deadline
while the readback GET stays live and the product serves both requests. The test waits on a channel
the handler closes right after the mark (bounded at 5 s, after which the ordinary trajectory
assertions run and name whatever shape occurred), so no assertion races the stall. The arm then
selects the late branch (the complement predicate) and asserts the branch's expectations against
the real product output.

| run | command (cwd `<scratch>/after/daemon`) | exit | result |
| --- | --- | ---: | --- |
| single | `go test -count=1 -timeout 300s -v -run '…/admission_deadline_lateserved' ./internal/app/` | 0 | `requests=[GET PUT]`, `first_request=+0.084s(until=+1.920s)`, `put=+2.054s(until=-0.051s)`, `after_put_return=+2.004s` |
| 40 repeats, 3-process oversubscription | `GOMAXPROCS=8 go test -count=40 -timeout 900s -v -run '…/admission_deadline_lateserved' ./internal/app/` beside the 25-round backpressure carrier and two full `internal/state` runs | 0 | 40/40 late-served with the PUT entry 50-56 ms past the anchored deadline (`until=-0.050..-0.056 s`; first request live by +1.634..+1.957 s); carriers exit 0 |
| 200 under the one-carrier recipe | `GOMAXPROCS=8 go test -count=200 -timeout 1500s -v -run '…/admission_deadline_lateserved' ./internal/app/` beside 25 backpressure carrier rounds then two full `internal/state` runs, one carrier at a time | 0 | 200/200 green: 197 late-served with the PUT entry 50-55 ms past the anchored deadline (`until=-0.050..-0.055 s`; first request live by +1.709..+1.966 s) + 3 accepted starved-A (`requests=[]`, native open+close, `t0/claim` only); carriers exit 0 |
| WSL `-race` | `timeout 30m wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd <scratch>/after/daemon && go test -race -count=1 -timeout 1200s -run TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline_lateserved ./internal/app/'` | 0 | `ok … 4.412s`, `put=+2.063s(until=-0.053s)`, no race report |

Readings: 197 of the 200 recorded runs executed the late branch (the other 3 served no request at
all: the accepted starved-A family, whose wait expiry the bounded fall-through routes to the
ordinary assertions instead of a red). The observed PUT error is `context deadline exceeded` and the
observed terminal payload is `{"error":"attach native Goal session: …","reason":"unknown_outcome",
"stage":"goal_admission",…}` — each named by a failing expectation mutant (§5's M4/M5), so both of
the branch's expectations are served by the product's real fail-closed output. Acceptance clause 2's
late-served gap is closed: the histogram now carries the pin's late-served executions with their
timelines.

Boundary probe (recorded; not a pass): moving the same hold onto the first served request produces
the §8 residual shape — exit 1, `requests=[GET]`, `PUT error=<nil>` — because the client's readback
call resolves on the expired budget and no PUT is attempted. The two natural late-served reds of the
superseded run therefore crossed the deadline where the pin crosses it: at the PUT entry, not in the
readback.

Audit trail (pin revision v1, superseded): the first pin revision ended its wait with a 20 s hard
`t.Fatal` and reported `the stalled PUT never reached the server` in 10 of 200 iterations run
beside two additional carriers (three test processes on the shared host). Each red subtest had
stretched to 27-33 s against the ~2.1 s nominal, i.e. the process was starved several times past
the pin's release schedule; the wait redded before any product assertion ran, and no further cause
is claimed from that log. The bounded-wait revision above then ran 40/40 late-served green under
the identical three-process load, so the shape is not reproducible there; the 200-count row above uses
the contract's one-carrier recipe.

### 9.2 Expectation controls

M4 (the late branch's PUT-error allowance narrowed to `io.EOF` only), M5 (its terminal expectation
changed to a wrong message) and the readback-crossing probe are rows of §5; each ran on the pin
bytes and was restored afterwards. M4 and M5 red, naming respectively the observed PUT error class
and the real terminal classification; the probe reds by design as the recorded residual shape.

### 9.3 Record edits (fix stage)

- plan D3: the late-served trigger is stated as the complement predicate (STD-A02).
- plan D3a: the amended accepted-set rule and rationale, with the pin's pointer (SPEC-A01); the plan's
  `What lands` and D2 wording aligned to the same rule.
- §5: the `CI job that must redden` column with the runner-user note and the oracle gaps (SPEC-A03).
- `docs/archive/CLOSED-TODO.md`: the closure paragraph carries "CI `daemon`/`daemon-windows` on the
  PR head: pending (conductor-owned)." (SPEC-A02).
- Host paths in these records were replaced by the `<worktree>`/`<scratch>` placeholders (§1); no
  verbatim log text was rewritten and no pre-existing entry was touched.
- No test byte changed at the r1 stage: the file was `24931a59…` before and after it, so §4/§7's
  numbers stand as recorded for that revision (re-verified, not re-measured); fix r2 changes the
  bytes (§10) and re-runs the loaded repeat, the companion, the pin, the focused family and `go vet`.

### 9.4 Fix-stage gates (document delta; no test byte changed)

| gate | command (cwd `<worktree>` unless noted) | exit | result |
| --- | --- | ---: | --- |
| focused family | `go test -count=1 -timeout 600s -run 'TestOmOAttachAckRegression' ./internal/app/` (`daemon/`) | 0 | `ok … 23.582s`; the 8 test functions of the owned file, unchanged in count |
| format / hygiene | `gofmt -l internal/app/omo_attach_ack_test.go`; `git diff --check` | 0 | empty / clean |
| binding re-verify | `sha256sum daemon/internal/app/omo_attach_ack_test.go` (and the restored scratch copy) | 0 | `24931a59…` both; the pin copy was `2ed354019cd225f4c5a3e75e23da717c28b93785a0b90a389cf08daa4c860d7d` before the restore |
| late-served pin | §9.1's runs (`<scratch>/after/daemon`) | 0 | 200/200 green: 197 late-served + 3 accepted starved-A; 40/40 under 3-process load; WSL `-race` green |
| expectation controls | §5's M4/M5 rows and the readback-crossing probe | 1 / 1 / 1 | each red for its named expectation; the probe red by design |
| citation existence | emulation repo of the final tree (`git archive HEAD` plus the fix-stage documents, committed; `python3 scripts/ci/check-citation-existence.py --repo . --base <base> --head <head>`) | 0 | `1 changed document(s) pass the path and symbol check (580 path citation(s))`; the three pre-existing ignore-covered notices unchanged |
| record path scans | the added lines of `git diff origin/main -- docs`, the two new records read whole, and the committed branch diff, each checked for drive-letter, guest-mount, temp, home and profile-directory tokens (patterns described, not pasted, per the stage brief) | 0 | no hits; no host path and no account name in any of the records |
| comment runs | added Go comment lines in `git diff origin/main -- daemon/` (longest consecutive run) | — | 2 at the r1 stage; 1 after fix r2 (§10.3) |

Not re-run at this stage: CI `daemon`/`daemon-windows` on a PR head (conductor-owned) and the
changed-file clone gate (pinned `jscpd` unavailable locally; CI's `contracts` job owns it); fix r2
re-runs the loaded repeat, the companion, the pin, the focused family and `go vet` on the final
bytes (§10.3), and the remaining `24931a59…` rows stand because the byte delta is comments only.

## 10. Fix stage 2 (review r2: P2-1, P3-2..P3-7, 2026-11-25)

Review r2 (`279-opus-review-1.md`) **shipped** the PR with one P2 and five P3 record findings: the
late-served pin was not retained as a verifiable artifact and its recorded hashes were truncated
(P2-1); the plan/§7 citation count and §2's numstat rows were stale (P3-2, P3-3); and three
accepted-set / wording residuals were to be recorded (P3-4, P3-5, P3-7). The fix r2 stage drops
the eight fixture-rationale comment blocks — 14 of the 15 added comment lines — and keeps the
single line on the claim-anchor coupling (P3-6), then re-runs the key gates on the final
comment-trimmed bytes and inlines the pin below. No non-comment line changed; no product, budget,
timeout, retry, skip or wildcard; no assertion removed.

### 10.1 The late-served pin artifact (P2-1)

The pin is a scratch copy of the shipping test file plus one added subtest mode
(`admission_deadline_lateserved`): the fixture holds a PUT's server-side handling (body buffered at
handler entry) until 50 ms past the anchored deadline, waits (bounded at 5 s) for the handler to
record the PUT, and then runs the ordinary closed-set assertion, whose complement predicate
selects the late-served branch.

- r1 artifact (build on `24931a59…`, superseded by the comment trim): pin file sha256
  `2ed354019cd225f4c5a3e75e23da717c28b93785a0b90a389cf08daa4c860d7d`; patch sha256
  `316ad1a7819d8d11ce8ff170ff89fcaa9eb39e1e5cd674823ef81f3b9f1cff55`.
- r2 artifact (build on the final bytes `ff43ba46…`): pin file sha256
  `84540b3257145f342b555205c41aeb3dcfa18c2fc7464ee27b9f50fc8a4ce923`; canonical patch (context 3)
  sha256 `7a20ae37a8f7629d38c2353a532f260960ce92a3bfe9b5b3225ef9dfa234b4a0`. The block below renders
  the same patch with context 0 so the record's whitespace gate stays clean: every changed line is
  byte-identical and only the context lines are omitted (sha256
  `bbbbdc730c9efc7ba1e8253bcc62fe34e3d35a325111938ee9fbe42331cff043`); it applies with `patch -p1`
  and reproduces the pin file byte-for-byte (verified).

Rebuild, re-apply and run through `go test -overlay` (placeholders per §1; the overlay JSON paths
must be absolute for the go command):

```
mkdir -p <scratch>/r2/daemon/internal/app
cp <worktree>/daemon/internal/app/omo_attach_ack_test.go <scratch>/r2/daemon/internal/app/
cd <scratch>/r2 && patch -p1 < fix-pin-lateserved-r2-u0.patch
sha256sum daemon/internal/app/omo_attach_ack_test.go   # 84540b32… (the pin file)
diff -u --label a/daemon/internal/app/omo_attach_ack_test.go \
  --label b/daemon/internal/app/omo_attach_ack_test.go \
  <worktree>/daemon/internal/app/omo_attach_ack_test.go daemon/internal/app/omo_attach_ack_test.go \
  | sha256sum    # 7a20ae37… (regenerates the canonical context-3 patch)
printf '%s\n' '{"Replace":{"<worktree>/daemon/internal/app/omo_attach_ack_test.go":"<scratch>/r2/daemon/internal/app/omo_attach_ack_test.go"}}' > overlay.json
cd <worktree>/daemon && GOMAXPROCS=8 timeout 30m go test -overlay <scratch>/r2/overlay.json \
  -count=200 -timeout 1500s -v \
  -run 'TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline_lateserved' \
  ./internal/app/
```

```diff
--- a/daemon/internal/app/omo_attach_ack_test.go
+++ b/daemon/internal/app/omo_attach_ack_test.go
@@ -3,0 +4 @@
+	"bytes"
@@ -196 +197,5 @@
-	timeline            omoAttachAckTimeline
+	// Scratch pin (fix r1): stall the PUT's server-side handling across the anchored
+	// deadline; putReached closes once the stalled handler has recorded the PUT.
+	putStall   func(*http.Request)
+	putReached chan struct{}
+	timeline   omoAttachAckTimeline
@@ -254,0 +260,3 @@
+	if request.Method == http.MethodPut && fixture.putStall != nil {
+		fixture.putStall(request)
+	}
@@ -263,0 +272,4 @@
+		if fixture.putReached != nil {
+			close(fixture.putReached)
+			fixture.putReached = nil
+		}
@@ -798 +810 @@
-	for _, mode := range []string{"caller_cancel", "caller_deadline", "admission_deadline", "admission_deadline_refused", "request_deadline"} {
+	for _, mode := range []string{"caller_cancel", "caller_deadline", "admission_deadline", "admission_deadline_refused", "admission_deadline_lateserved", "request_deadline"} {
@@ -803,0 +816 @@
+			var putServed chan struct{}
@@ -818,0 +832,23 @@
+			case "admission_deadline_lateserved":
+				fixture.claimDeadlineOffset = 2 * time.Second
+				fixture.afterPut = func(requestContext context.Context) { <-requestContext.Done() }
+				release := make(chan struct{})
+				putServed = make(chan struct{})
+				fixture.putStall = func(request *http.Request) {
+					body, err := io.ReadAll(request.Body)
+					if err == nil {
+						request.Body = io.NopCloser(bytes.NewReader(body))
+					}
+					<-release
+				}
+				fixture.putReached = putServed
+				go func() {
+					for {
+						deadline := fixture.timeline.deadlineSnapshot()
+						if !deadline.IsZero() && time.Now().After(deadline.Add(50*time.Millisecond)) {
+							close(release)
+							return
+						}
+						time.Sleep(5 * time.Millisecond)
+					}
+				}()
@@ -825,0 +862,6 @@
+			case "admission_deadline_lateserved":
+				select {
+				case <-putServed:
+				case <-time.After(5 * time.Second):
+				}
+				fixture.assertBudgetArmTrajectory(journal)
```

### 10.2 Pin runs on the final bytes

| run | command (cwd `<worktree>/daemon`) | exit | result |
| --- | --- | ---: | --- |
| single | `go test -overlay <scratch>/r2/overlay.json -count=1 -timeout 300s -v -run '…/admission_deadline_lateserved' ./internal/app/` | 0 | late branch: `requests=[GET PUT]`, `first_request=+0.049s(until=+1.955s)`, `put=+2.054s(until=-0.050s)`, 2.27 s arm |
| 200 repeats | `GOMAXPROCS=8 timeout 30m go test -overlay <scratch>/r2/overlay.json -count=200 -timeout 1500s -v -run '…/admission_deadline_lateserved' ./internal/app/` beside the D7 carrier pair (exit 0: 39.696 s backpressure + 5.488 s `internal/state`) | 0 | 448.542 s; 200 `RUN` / 200 `PASS` / 0 red: **198 late-served** (`requests=[GET PUT]`; PUT entry 0.050-0.055 s past the anchored deadline, mean 0.053 s; first request live by 1.670-1.966 s, mean 1.943 s) + **2 accepted starved-A** (`requests=[]`, native open+close, `t0`/`claim`-only timelines) |

Histogram (200 iterations): 198 late-served / 2 starved-A / 0 other / 0 red. The PUT-error class
the pin serves is `context.DeadlineExceeded`; the M4/M5 expectation controls (§5) still red under
their altered branch expectations. The carrier pair finished early in this window (39.7 s + 5.5 s);
the pin's hold forces the shape regardless of host load.

### 10.3 Final-bytes gate re-runs

| gate | command (cwd `<worktree>/daemon` unless noted) | exit | result |
| --- | --- | ---: | --- |
| loaded repeat | `GOMAXPROCS=8 timeout 30m go test -count=200 -timeout 1500s -run 'TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline' ./internal/app/` beside 25 backpressure carrier rounds (322.748 s) then 2 `internal/state` runs (7.213 s), carrier exit 0 | 0 | 0 red / 200 in 468.956 s |
| companion (`-v`, no carrier) | the same command plus `-v` | 0 | 0 red / 200 in 468.213 s; 400 trajectory lines: 196 intended (margin min 1.140 s / max 1.965 s / mean 1.935 s), 4 starved-A, 200 shape-B pins |
| focused family | `go test -count=1 -timeout 600s -run 'TestOmOAttachAckRegression' ./internal/app/` | 0 | `ok … 64.030s` (every test of the owned file) |
| vet | `go vet ./internal/app/...` and `go vet ./...` (the module root is `<worktree>/daemon`, so the vet pattern is module-relative) | 0 / 0 | clean, empty output |
| format / hygiene | `gofmt -l daemon/internal/app/omo_attach_ack_test.go`; `git diff --check` (worktree root) | 0 | empty / clean |
| comment census | `git diff 5c297c25 -- daemon/internal/app/omo_attach_ack_test.go | grep -cE '^\+[[:space:]]*//'` | — | 1 (the claim-anchor line); the working-tree delta vs the tip is +1/−15, all comments |

### 10.4 Record corrections (P3-2, P3-3)

- The plan's citation row and §7's: 573 → **580** path citations, re-measured in the emulation repo
  of the final tree (`28b471f`..`HEAD`).
- §2's numstat rows restated against the PR merge-base (`git diff --numstat 5c297c25`, working
  tree): test file 222/21, plan 194/0, record 494/0; `docs/TODO.md` 0/18 and
  `docs/archive/CLOSED-TODO.md` 40/0 (the earlier 163/207/39 numbers were the apply-stage interim
  state).
- §5/§9.1/§9.4's truncated pin hashes replaced by the full 64-hex digests (§10.1).

### 10.5 Residuals recorded (P3-4, P3-5, P3-7)

The three residuals are recorded in §8: the late branch's unexecuted `io.EOF` alternative (P3-4),
the `caller_deadline` arm's lack of its own pin (P3-5; review r2's scratch probe reproduced the
same late branch there 3/3), and the starved-B contract wording (P3-7; the shipped zero-delivery
assertion is the correct one).

Not run at this stage: CI `daemon`/`daemon-windows` on a PR head (conductor-owned), the WSL `-race`
oracle and the mutant rows re-run on the final bytes (the byte delta is comments only, so their
`24931a59…`/pin-build results stand), and the changed-file clone gate (pinned `jscpd` unavailable
locally; CI's `contracts` job owns it).
