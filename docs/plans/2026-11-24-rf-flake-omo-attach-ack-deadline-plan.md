# rf-flake-omo-attach-ack-deadline — plan

Base `9d113eb6` (`origin/main` at dispatch). Contract: `goal-rf-flake-omo-attach-ack-deadline.md`
(GDD state root, external to the repository); this plan implements it and records the choices the
contract leaves open, plus the exact commands and counts the gates produce. Evidence record:
`docs/evidence/rf-flake-omo-attach-ack-deadline-2026-11-24.md`. Shape references: the W4-13
plan/evidence pair (census, mutants, the recorded 5.2/6.3/§9 rows) and the W6-12 plan/evidence pair
(plan format, gate rows).

Owned set (STRICT): `daemon/internal/app/omo_attach_ack_test.go`, this plan, the evidence record,
`docs/TODO.md` and `docs/archive/CLOSED-TODO.md` (the tracked entry moves to the archive). Product
files (`app.go`, `goal_admission_tx.go`, `goal_delivery.go`) are read-only; the only product edits
in this slice are temporary mutants in a scratch copy, restored byte-identical.

## What lands

| file | change |
| --- | --- |
| `daemon/internal/app/omo_attach_ack_test.go` | P1 timeline probe; F1 claim-anchored `DeadlineAt`; F2 closed 2 s trajectory set with the accepted families strictly asserted (D3/D3a); the shape-B pin mode |
| `docs/plans/2026-11-24-rf-flake-omo-attach-ack-deadline-plan.md` | this plan |
| `docs/evidence/rf-flake-omo-attach-ack-deadline-2026-11-24.md` | the evidence record (commands, counts, timelines, mutant rows with restored hashes) |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | the `:1389-1405` entry moves to the archive with a pointer to the evidence |

No committed production change.

## D1. The arm's own premise is the defect: the budget must start where the product reads it (F1)

At base the `admission_deadline` arm writes `admission.Limits.DeadlineAt = now+2s` into
`fixture.control.work.Input` at subtest entry, before `fixture.run` claims. The product reads the
claim's `Work` only after `Claim` returns (`app.go:1359` parses, `goal_admission_tx.go:356-360`
compares against `daemon.now()`), so every host-dependent stage between subtest entry and the claim
— fixture setup, `app.startAssignment`, journal writes, the claim round trip — is charged against
the 2 s budget the arm races. F1 re-anchors `DeadlineAt = now+2s` inside
`omoAttachAckControl.Claim` (owned file, no product change), immediately before delegating, and
gates it behind a fixture offset field so only the `admission_deadline` mode re-anchors.

`SaveClaimGrant`'s only Work equality guard is the Linux-v2 fence branch
(`daemon/internal/state/identity.go:213`), which a fresh OmO run never arms (no
`ContainmentCgroup`/`LinuxCloneIntent`; `linuxNativeRuntimeRequiresGoalAdmission` is pi/opencode
only, `app.go:695-698`), so the claim response's re-anchored Work is the journal Work the product
reads. Verified natively on Windows and under WSL `-race` (both gates below).

## D2. The assertion branch, not the deadline branch (contract decision note, taken as written)

The recorded starvation is 6-8x and the arm's duration is production-bounded
(`controlRequestLimit = 15s`, `app.go:38`); a budget raise is still a wall clock and lengthens the
arm, and a product clock seam is outside the owned set. F2 keeps the 2 s budget and accepts exactly
the fail-closed trajectories the probe and the deterministic pins recover (D3a's amended rule);
everything else stays a failure, and no trajectory is accepted by wildcard.

## D3. The closed trajectory set and what each accepted trajectory asserts (F2)

For `caller_deadline` and `admission_deadline` only; `caller_cancel` and `request_deadline` keep
their exact `assertRequests([GET, PUT])` assertions.

| trajectory | request set | additional strict assertions |
| --- | --- | --- |
| intended | `[GET, PUT]` | the base assertions (`assertRequests`, `assertStoppedWithoutTurn`, `assertExactPending`) plus the timeline: the anchored deadline is live at the first served request and at the PUT entry, and the hold returns no earlier than the deadline (the expiry is consumed inside the PUT leg) |
| starved-A | `[]` | native session opened once and closed once, no turn, no context fetch, exact pending (compared against the delivery captured at the pre-PUT readback), terminal failure classified `unknown_outcome` at stage `goal_admission` naming `context deadline exceeded` |
| late-served | `[GET, PUT]` | the intended assertions minus the liveness predicate, which the branch *inverts* (the complement: the first served request or the PUT entry is at/after the anchored deadline); the PUT error is a lost acknowledgement (`io.EOF`) or the expired budget (`context.DeadlineExceeded`); terminal failure classified `unknown_outcome` at stage `goal_admission` naming the attach stage |
| in-flight readback | `[GET]` | the PUT was dispatched on the expired budget and refused client-side (`context.DeadlineExceeded`), so no second request reached the server; native session opened and closed, no turn, exact pending, terminal failure classified `unknown_outcome` at stage `goal_admission` naming `context deadline exceeded` |
| starved-B | `[]` | no native call at all, no goal delivery ever queued/delivered/retired, terminal failure classified `unknown_outcome` at stage `goal_admission` naming `Goal admission deadline has elapsed before native launch` |

Any other request set — a third request, a post-expiry readback, `[GET]` alone, a turn start, a
changed journal — fails with the failure detail carrying the P1 timeline and the observed
trajectory. The intended trajectory's request assertion is byte-identical to the base `:629` line,
so no assertion is removed; the two starved branches add call-set and classification equalities the
base line did not have.

### D3a. The accepted set is the trajectories P1 recovers or a deterministic P2 pin executes
### (contract F2, as amended by the conductor ruling on review r1, 2026-11-25)

P1's loaded repeat and the companion run recovered four red families at base: 21/22 of the loaded
reds were starved-A and one was the in-flight-readback shape (`requests=[GET]`, PUT refused
client-side); the settled-tree companion recovered late-served twice
(`requests=[GET, PUT]`, first served request 13 ms and 27 ms past the anchored deadline, PUT error
the hijack `EOF`, terminal failure `unknown_outcome` at `goal_admission` naming the attach stage).
All four are fail-closed: the original budget is honoured (no readback, no turn, exact pending,
unchanged journal).

The contract's F2 defined the accepted set as "exactly the trajectories P1/P2 recover" and names
the three expected shapes; the probe recovered two further fail-closed families. Review r1 held
that no accepted family may stand without a deterministic execution on the shipping bytes; the
conductor's ruling of 2026-11-25 amended F2 to the rule recorded here (the amendment and its
rationale live in this section):

- (a) Accept only the three named shapes — rejected: the arm stays red at roughly 1 % on a busy host
  (2/200 in the companion run) for trajectories that show no product defect, i.e. the goal's own
  promise — deterministic under host load — stays unmet.
- (b) **Adopted (amended rule):** accept exactly the families P1 recovers or a deterministic P2 pin
  executes, each strictly pinned (D3) with an executed pin or a loaded red on the binding bytes. The
  set stays closed: every branch asserts an exact request set, an exact native call log, the
  fail-closed postconditions, the terminal classification, and — for the served pair — the
  *complementary* liveness predicate, so the intended and late-served branches cannot absorb each
  other. No wildcard acceptance, no budget raise, no retry, no skip, no removed assertion; the
  mutants below re-verify that defect detection is unchanged (M1/M2/M3 still red).

The late-served family's deterministic pin is recorded in the evidence record §9 (fix r1): the
fixture holds the PUT's server-side handling until 50 ms past the anchored deadline, and the branch
executes green on the shipping bytes under load (197/200 runs took the late branch; the other 3
were the accepted starved-A family), plus once under WSL `-race`. Reverting to (a) remains a
two-branch deletion; it is the recorded rejected alternative, not an open question.

## D4. Starved-A's exact-pending capture

Base captures `fixture.original` in `serveHTTP`'s PUT branch — unavailable when no PUT is served.
The fixture now also captures the single pending goal delivery from the store inside
`omoAttachAckControl.FetchHarnessSessionAttachment` (immediately before the readback GET is
issued), only when no capture exists. The PUT branch keeps overwriting it at PUT time, so the
intended trajectory is unchanged and starved-A still compares the final journal against an
independently captured delivery (not against itself).

## D5. Starved-B is pinned deterministically (the shape-B pin)

The `admission_deadline_refused` mode sets the F1 offset to a negative duration, so the claim
anchors an already-elapsed `DeadlineAt` and the product's `:358` refusal fires before any native
launch. The mode asserts the starved-B postconditions and terminal classification exactly; with
the `:358` check removed (mutant M3) the same pin reds because the run reaches the fake `open`
and records starved-A instead. The pin runs inside the loaded repeat's `-run` pattern (its name
carries the arm's prefix) and costs milliseconds.

## D6. P1 timeline (permanent probe, no semantic change when green)

The fixture records monotonic stage marks — subtest entry (`t0`), `Claim` return, first served
request, PUT entry, `afterPut` entry/return — plus the anchored deadline; `format()` prints each
mark as `stage=+elapsed(until margin)`, the margin being `deadline - mark`. The failure detail of
`assertRequests` appends the timeline, and the 2 s trajectory assertion and the pin log it under
`-v`, so the unloaded companion run carries the intended trajectory's `deadline - first request`
margin as data. The served pair's liveness predicates read the same values (D3). The first
companion run proved `afterPut`'s *entry* is not a liveness witness: the PUT leg's resolution can
itself be starved past the deadline, so the placement assertion binds the served requests and the
hold's return, not the hold's entry (recorded in the evidence record §4).

## D7. Command and load recipe

Gates run foreground (`pty`, `timeout 0`, each bounded by `timeout`), never with a measurement
beside the full suite. The load carrier for the bounded repeats is one other bounded heavy job —
`timeout 25m go test -count=4 -timeout 1400s -run '^TestOutputBackpressureRecoversWithOneTruncationMarker$' ./internal/app/`
plus `timeout 25m go test -count=2 -timeout 1400s ./internal/state/` when the first finishes early
— chosen over a full-suite companion because the sequencing rule forbids the full suite beside a
measurement harness, and recorded with its measured wall time in the evidence record.

## D8. Out of scope

- Product files, other tests, other flakes, `ci.yml` (contract clause 4); a product finding would
  be DECISION_NEEDED, and none was found (the refusal and the budget guard behave as designed).
- The `request_deadline` arm's 15 s production-bound duration (W4-13 §5.2, recorded).
- A product clock seam or budget raise (D2).

## Results (commands, counts, hashes — full rows in the evidence record)

Subject: worktree `9d113eb6` + `daemon/internal/app/omo_attach_ack_test.go` sha256
`24931a59649d80e92c5e58bb4566072b0e949ad2d9a1a34cb9381f9143006a19` (`git diff --numstat` 236/21;
`git diff --check` clean). Scratch clones `<scratch>/{before,after}` — `<worktree>` and `<scratch>`
are the placeholder names for the goal worktree and the disposable scratch root; this plan and the
evidence record carry no absolute paths. Each row below is the apply-stage re-run on the binding
bytes unless it names the pre-fix arm; the implement-stage rows for the intermediate revisions are
in the evidence record §4.2.

| row | command | exit | result |
| --- | --- | ---: | --- |
| pre-fix loaded repeat | `GOMAXPROCS=8 timeout 30m go test -count=200 -timeout 1500s -run 'TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline' ./internal/app/` (`before`, P1-only arm) | 1 | 22 red / 200 in 622.797 s: 21 starved-A + 1 in-flight readback, each with its P1 timeline |
| post-fix loaded repeat | same command (worktree, binding bytes) beside 7 bounded rounds of `go test -count=4 -run '^TestOutputBackpressureRecoversWithOneTruncationMarker$' ./internal/app/` | 0 | 0 red / 200 in 482.614 s; the carrier rounds covered the whole measurement |
| post-fix companion | same command plus `-v`, no carrier | 0 | 0 red / 200; 400 trajectory lines: 196 intended (margin min 0.923 s / max 1.968 s / mean 1.915 s), 4 starved-A, 200 pins |
| WSL `-race` | `timeout 30m wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd <worktree>/daemon && go test -race -count=1 -timeout 1200s -run TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget ./internal/app/'` | 0 | `ok … 22.846s`, no race report |
| focused family | `go test -count=1 -run 'TestOmOAttachAckRegression' ./internal/app/` | 0 | `ok … 24.778s` |
| vet / format | `go vet ./...`; `gofmt -l internal/app/omo_attach_ack_test.go` | 0 | clean, empty |
| pins and mutants | §5 of the evidence record | P2-before 1, P2-after 0, in-flight pin 0, M1 1 (×2; M1′ 1, deterministic), M2 1 (×2), M3 1 | product files restored to sha256 `89a9857d…` (`goal_delivery.go`), `76bcca94…` (`goal_admission_tx.go`), `b95b4f69…` (`harness_test.go`) |
| citation existence (emulation) | `python3 scripts/ci/check-citation-existence.py --repo . --base 9d113eb6 --head HEAD` | 0 | 573 path citations pass |
| changed-file clone gate | `python3 scripts/ci/jscpd-changed.py …` | 2 | not run: pinned `jscpd` not installed locally (verification limit; CI owns it) |

Fix-stage rows (review r1, 2026-11-25; the test bytes are unchanged — binding sha256 re-verified
`24931a59…`; full rows in the evidence record §9):

| row | command | exit | result |
| --- | --- | ---: | --- |
| late-served pin | `GOMAXPROCS=8 go test -count=200 -timeout 1500s -v -run '…/admission_deadline_lateserved' ./internal/app/` (`after` clone + `<scratch>/fix-pin-lateserved.patch`, pin bytes sha256 `2ed35401…`) beside bounded carrier rounds (one at a time) | 0 | 200/200 green: 197 late-served (PUT entry 50-55 ms past the anchored deadline) + 3 accepted starved-A |
| pin `-race` | `timeout 30m wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd <scratch>/after/daemon && go test -race -count=1 -timeout 1200s -run …/admission_deadline_lateserved ./internal/app/'` | 0 | `ok … 4.412s`, no race report |
| pin-expectation mutants M4/M5, readback-crossing probe | the same pin command (M4: EOF-only allowance; M5: wrong terminal message; probe: the hold moved to the GET) | 1 / 1 / 1 | M4 names the observed `context deadline exceeded`, M5 the real terminal classification; the probe reds by design as the recorded `[GET]`/`putErr=nil` residual shape |
