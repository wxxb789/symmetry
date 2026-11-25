# rf-flake-containment-replay-nslink — plan

Base `3b23dead` (dispatch HEAD; `origin/main` tip at dispatch). Contract:
`goal-rf-flake-containment-replay-nslink.md` (GDD state root, external to the repository); this plan
implements it and records the choices the contract leaves open, plus the exact commands and counts
the gates produce. Evidence record: `docs/evidence/rf-flake-containment-replay-nslink-2026-11-25.md`.
Shape references: the `rf-flake-omo-attach-ack-deadline` plan/evidence pair (plan format, gate rows,
mutant rows with restored hashes) and the `rf-flake-stdout-drain-64kib` plan/evidence pair (test-only
increment, red-before pin).

Owned set (STRICT): `daemon/e2e/production_linux_containment_witness_test.go`, this plan, the
evidence record. Not owned and untouched: `daemon/internal/platform/*`, every other e2e file,
`ci.yml`, `docs/TODO.md` (no entry exists for this flake; the evidence record is the closure
artifact). The only product-file edits in this slice are the temporary mutants in the scratch copy,
restored byte-identical (`sha256 8db3a21e…` verified after each).

## What lands

| file | change |
| --- | --- |
| `daemon/e2e/production_linux_containment_witness_test.go` | F1 EACCES decision in the shared identity-gone wait; P1 probe record on the unexpected-error return; F2 `waitForLinuxWitnessIdentityGoneWith` seam; one parse path for the `/proc/<pid>/stat` snapshot; pins K1/K2 with the re-exec child mode |
| `docs/plans/2026-11-25-rf-flake-containment-replay-nslink-plan.md` | this plan |
| `docs/evidence/rf-flake-containment-replay-nslink-2026-11-25.md` | the evidence record (commands, census, hashes, verbatim reds/greens, mutant rows) |

No committed production change; the diff is one test file (`471` insertions, `20` deletions vs
`3b23dead`; the 2026-11-25 apply pass is the last `103`/`60` of it, subject sha256
`8db3a21e6993d34a7889b994de1b082229e20a43d73c182cecd266f468cdbed7`).

## D1. The decision (F1): absence from the recorded starttime, never from the errno

`waitForLinuxWitnessIdentityGone` collapsed every `ProcessIdentity` error other than ENOENT/ESRCH
into an immediate failure, so the kernel's `release_task` window — detach done, `/proc/<pid>` dentry
still reachable — reds the witness when a poll straddles a reap. The kernel reuses `-EACCES` for
that window and for a ptrace denial on a live process, so the errno cannot decide absence.

On `errors.Is(err, syscall.EACCES)` the loop now takes exactly one `/proc/<pid>/stat` probe and, in
order:

| probe result | outcome |
| --- | --- |
| read error ENOENT/ESRCH | `nil` (gone; dentry retired) |
| any other read error, or a payload that does not parse | classified fail-closed error |
| readable, state `Z` | `nil` (gone; unreaped zombie) |
| readable, `identity == ""` | keep polling; the deadline error is unchanged |
| readable, recorded starttime differs (v2 identity's trailing field) | `nil` (the pid no longer carries the recorded identity) |
| readable, recorded starttime equal, or the identity is not a parseable `linux:v2:` marker | classified fail-closed error |

The classified error is
`process <pid> identity "<identity>" is unreadable after a denied identity read: /proc/<pid>/stat shows state=<S> starttime=<N>`
(or `… probe failed: <err>` when the probe itself fails): it never returns `nil`, never repeats the
raw syscall error, and never claims the process `remained live`. Every other branch, both `25 ms`
sleeps, and the deadline message (`process %d with identity %q remained live`) are byte-for-byte
unchanged. One 1-2 line comment sits beside the decision (the kernel ambiguity and the starttime
rule); no other comment was added.

Product semantics are untouched (conductor Q1 (a)): `ProcessIdentity` still reports EACCES as
`ErrPersistedProcessGroupUnproven`, and `ProvePersistedProcessGroupAbsent` never reads the target's
ns link.

## D2. The probe record (P1) rides the unexpected-error return

The remaining `if err != nil` return (non-ENOENT/ESRCH/EACCES identity read failures) now takes one
`/proc/<pid>/stat` probe and returns the original error wrapped with the record:

`<original error> (identity read failure: leg=<readlink|stat namespace link|read process start time> errno=<n>(<text>) poll=<n> elapsed=<duration>; /proc/<pid>/stat probe: exists=<bool> state=<S> starttime=<N> err=<nil|error>)`

The leg comes from the failing wrapper's message, the errno from `errors.As(syscall.Errno)`, the
iteration/elapsed from the loop, and the probe fields from the same probe seam F1 uses. The original
error stays wrapped (`errors.Is` preserved), so a residual red is judged on the record. The helper
has no `*testing.T`; the record travels in the returned error, which callers surface through
`t.Fatalf`, and the pins `t.Logf` it (`non_denial_error_carries_the_probe_record` the
record-carrying error, K1 the classified error) — recorded as such because the F2 seam signature is
fixed.

## D3. The seam (F2) mirrors the house pattern without call-site churn

`waitForLinuxWitnessIdentityGone(pid, identity, timeout)` is now a one-line wrapper over
`waitForLinuxWitnessIdentityGoneWith(pid, identity, timeout, read func(int) (string, error), probe
func(int) (linuxWitnessProcessSnapshot, error))`, passing `platform.ProcessIdentity` and
`readLinuxWitnessProcessSnapshot`. This mirrors `retiredProcessIdentityError(journal, read)`; all
`38` direct call sites plus the `25` `waitForLinuxWitnessProcessStopped` call sites are untouched
(of those `25`, only the `5` with an empty identity route into the wait; the other `20` prove
absence through `ProvePersistedProcessGroupAbsent` and never read the ns link). The
`/proc/<pid>/stat` parse moved into `readLinuxWitnessProcessSnapshot` and
`linuxWitnessProcessSnapshotAt` reuses it, so the probe and every existing snapshot share one
parsing path (error strings `malformed /proc stat` / `short /proc stat` and all snapshot fields
unchanged).

## D4. The pins (K1, K2)

Both pins re-exec the test binary (`os.Args[0] -test.run=^TestLinuxWitnessIdentityGoneDeniedChildProcess$`
plus `SYMMETRY_LINUX_WITNESS_DENIED_IDENTITY_CHILD=1`, the `peer_linux_test.go:152` pattern): the
child prints `READY`, and on the `deny` line calls `prctl(PR_SET_DUMPABLE, 0)` and prints
`DUMPABLE 0`. The parent builds the child's canonical identity from the boot ID, the self PID
namespace inode and the child's `/proc/<pid>/stat` starttime, and cross-checks it against
`platform.ProcessIdentity` before the transition — so the identity is a wire-format fixture
independent of the product builder.

- K1 `TestLinuxWitnessIdentityGoneUnreadableLiveProcessFailsClosed`: after the denial the prerequisite
  check requires `platform.ProcessIdentity` to fail with EACCES; a successful read skips (privileged
  host — the denial is unavailable, recorded via `t.Skipf`), and any other error fails instead of
  skipping (apply pass). The WSL gate runs as `accept`, non-root, so the pin runs. Then a `3 s`
  wait must fail *before* the deadline with the classified message naming the identity, the
  `/proc/<pid>/stat` line, the observed state and the recorded starttime, and must not repeat
  `permission denied` or claim `remained live`.
- K2 `TestLinuxWitnessIdentityGoneDeniedReadDecisions`: injected reader, real probe. Rows:
  `reaped_pid_probe_missing_is_gone` (stat-leg EACCES over a reaped pid → `nil`, asserted inside
  `1 s` — measured `0.03 s`; the bound is recorded here per the apply pass),
  `zombie_state_is_gone` (injected EACCES + visible zombie + empty identity → `nil`, pinning the Z
  rule's precedence over the empty-identity poll), `doctored_starttime_is_gone` (readlink-leg EACCES +
  a starttime bumped by one → `nil`), both legs with the real identity → classified failure inside
  the `10 s` timeout without waiting it out (`elapsed < 5 s`),
  `non_denial_error_carries_the_probe_record` (EIO from the start-time leg → still `errors.Is(EIO)` +
  the full P1 record, `t.Logf`'d), `denied_read_probe_failure_fails_closed` (EACCES + a failing
  `/proc` probe → classified `probe failed: input/output error`, never `nil`; apply-pass coverage of
  the contract's probe-failure arm), `non_denial_error_records_probe_failure` (EIO read + failing
  probe → the P1 record renders `exists=false state= starttime=0 err=input/output error`; apply-pass
  coverage), `empty_identity_keeps_polling` (`nil`-free deadline error, exact base text,
  `elapsed ≥ 400 ms`, and a counting reader proves ≥ `2` identity reads; apply-pass coverage).

## D5. Class census (conductor ruling Q2, lesson 85)

Criterion: a poll that *waits for disappearance* repeatedly reads identity across a reap and can
straddle the detach window; those route through the helper. Every other identity read reads once
from a vantage point outside the window — before it (live-process assertion: EACCES must stay
fail-closed) or after it (post-reap assertion: `release_task` retired the dentries before the read
started, so ENOENT is deterministic). Census of all `58` `platform.ProcessIdentity(` call sites in
`daemon/**/*_test.go` (`38` in `e2e`, `20` in `daemon/internal`), plus the two function-value
passes (`witness_test:3664`, `core_test:1359`), with per-site reason:

| site (file:line) | function | decision |
| --- | --- | --- |
| witness_test:3664 | `platform.ProcessIdentity` value pass into `waitForLinuxWitnessIdentityGone` (`:3663`) | routed: the one repeated-read disappearance wait; now carries the decision |
| witness_test:3649 | `assertLinuxWitnessProcessLive` | stays: live assertion, EACCES must fail closed |
| witness_test:4157, 4206 | `waitForLinuxWitnessAllThreadsStopped` | stays: stop proof across a paused process, never a reap; identity revalidation gates the success return |
| witness_test:2750, 2761 | `openLinuxWitnessPIDFD` | stays: pidfd binding before/after, one-shot |
| witness_test:1586 | `runLinuxHelperCrashMirrorWitness` | stays: daemon creation identity, one-shot |
| witness_test:3902 | `linuxWitnessCanonicalIdentity` (pin) | stays: fixture cross-check before the denial, one-shot |
| witness_test:3959 | K1 prerequisite (pin) | stays: requires EACCES, one-shot |
| core_test:1348 | `assertLivePersistedProcess` | stays: live assertion |
| core_test:1359 | `platform.ProcessIdentity` value pass into `retiredProcessIdentityError` (`:1364`, from `assertRetiredProcessIdentity` `:1357`) | stays: one-shot retirement assertion whose `isProcessIdentityNotFoundError` predicate is `os.ErrNotExist || syscall.Errno(87)` (Windows `ERROR_INVALID_PARAMETER`; no ESRCH) — house precedent cited by the contract; not a wait and not a poll |
| core_test:1441 | `assertStoppedProcessOrRecoveryConstraint` | stays: one-shot read beside the recovery-constraint assertion; not a wait |
| negative_witness:735 | `runLinuxProductionPIDReuseNamespaceCase` | stays: post-reap absence assertion, dentry retired before the read |
| negative_witness:794, 1027, 1235, 1553 | same file, captures/pidfd/live anchor | stays: one-shot capture |
| replay_witness:1072 | `runLinuxHelperCrashReplayWitness` | stays: daemon creation identity |
| v2_helper_exit:427 | `linuxV2ObserveReleasedBHelperExit` | stays: released-gate revalidation, one-shot |
| v2_native:2347, 2403, 2421, 2556, 2603, 2879, 2969, 3028, 3038, 3092, 3122, 3131, 3152, 3355 | v2 native snapshots / pidfd / FD audit | stays: one-shot captures and audits with their own ENOENT/ESRCH classification; never a disappearance wait |
| v2_owner_cli:82, 2505, 2603, 2685, 3890 | owner CLI witness | stays: creation/reuse/migration identity assertions |
| pre_authority_crash_witness:417, 445 | `assertPreAuthorityProcessStopped` | stays: Windows handle-based stop assertion with its own classification; no procfs straddle class |
| internal/*_test.go (20 sites: app `pi_provider_bridge_peer_linux_test.go:27`/`pi_provider_bridge_peer_nonlinux_test.go:15`; execution `identity_linux_test.go:84`/`identity_windows_test.go:28`; harness/omp `native_acceptance_test.go:1186,1231,1268,1281,1295,1310,1324,1416`; harness/opencode `adapter_native_work_linux_test.go:147`, `peer_linux_test.go:194`, `peer_test.go:25,50`; harness/pi `provider_bridge_peer_linux_test.go:284`, `provider_bridge_peer_test.go:18,64`, `provider_extension_native_linux_test.go:152`) | peers, snapshots, bindings | stays: identity captures/bindings and one-shot post-`Wait` survival checks; the post-`Wait` reads start after `release_task`, so ENOENT is deterministic |

No site outside the helper is a disappearance poll, so the ruling's routing needs no edit outside the
owned file; the two borderline one-shots (`retiredProcessIdentityError`, `assertPreAuthorityProcessStopped`)
are recorded above rather than routed (both are assertions, not waits; routing them through a
timeout wait would change their semantics).

## D6. The oracle: mutants, not a forced witness red

The flake cannot be forced to red on demand — the reap straddle is sub-microsecond and not
reproducible (contract probe matrix: ~390k kill/reap polls, 0 EACCES). The mutant oracle is
therefore the pins; this plan claims no witness repeat. The `integration` witness matrix
(`ci.yml:1659`) runs on the PR head and is CI-owned; the `daemon` job runs the pins inside
`go test -race ./...`.

| id | mutation | expected red |
| --- | --- | --- |
| M1 | delete the EACCES decision (base `:3664` behavior) | K1 + every decision-dependent K2 row |
| M2 | `return nil` on EACCES without the probe | K1 + K2 real-identity rows + empty-identity row |
| M3 | match `stat namespace link` text instead of `errors.Is(syscall.EACCES)` | K1 + K2 readlink-leg rows (the real denial is the readlink leg — the base census proves it) |
| M4 | invert the starttime rule (equal → gone) | K1 + K2 real-identity rows |
| M5 | `identity == ""` + readable live + EACCES → `nil` | K2 empty-identity row |

## D7. Out of scope

- Product files (`daemon/internal/platform/*`), other waits/tests, `ci.yml`, other flakes (contract
  clause 4). No product finding was made: the product stays fail-closed by design.
- No timeout/budget raise, no retry, no skip, no `-run` narrowing in CI paths (the gates use `-run`
  only to select the pins).
- Sibling direct `ProcessIdentity` polls stay on base behavior (census above), per the ruling.

## D8. Commands and results

All Go gates run in WSL `Ubuntu-24.04` as user `accept` (uid 1000, non-root, 16 CPUs). Scratch root
`<scratch>` with `base/` (base tree + K1 only), `head/` (implement-stage head tree), `apply/`
(apply-stage copy of the post-apply tree plus the `.pristine` test-file blob), `logs/`, `scripts/`;
the worktree is `<worktree>`. Runs are foreground and sequential (the sequencing rule: no
measurement harness beside the suite; no benchmark harness exists in this increment). The rows
below are the post-apply bytes (`8db3a21e…`) except the base census; the apply-stage re-run
replaced the implement-stage figures, which survive only in the GDD run records.

| row | command | exit | result |
| --- | --- | ---: | --- |
| base census | `<scratch>/base/daemon: go test -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v` | 1 | exactly one red: K1 on the message assertion, verbatim base error `read process 94044 PID namespace: readlink /proc/94044/ns/pid: permission denied`; the child-mode test skips |
| pins (worktree, post-apply) | `<worktree>/daemon: same command` | 0 | K1 green with the classified message; K2 + 9 rows green, `ok … 0.647s`; 1 child-mode skip |
| hygiene | `gofmt -l .`; `go vet ./...` | 0 | 0 files listed; 0 findings |
| daemon suite | `go test -count=1 -timeout 900s ./...` (WSL, worktree) | 0 | 30 `ok`, 0 `FAIL` (e2e `1.628s`, `internal/app` `241.007s`, `internal/platform` `74.034s`) — the local equivalent of the `daemon` job aggregate |
| race, changed package | `go test -race -count=1 -timeout 600s ./e2e`; pins with `-race … -run "^TestLinuxWitnessIdentityGone" -v` | 0 / 0 | `ok … 1.944s`; pins `ok … 1.784s`, K1 `0.05s`, K2 `0.68s`, 0 red, 0 stray children |
| race, full module | `go test -race -count=1 -timeout 900s ./...` | 1 | per-binary budget expired in the untouched `internal/app` while it was still progressing (race overhead ~5-9x on this host); not re-run in the apply pass; the `daemon` CI job owns the full `-race ./...` verdict |
| mutants M1-M5 | `apply-mutant.sh <id>` on `<scratch>/apply/daemon`, file restored from `.pristine` after each | 1 | red rows M1 `9` / M2 `6` / M3 `4` / M4 `5` / M5 `2` (rows as D6); the file's sha256 after every restore is `8db3a21e…` (byte-identical) |
| diff hygiene | `git diff --check` (worktree) | 0 | clean; `git diff --numstat 3b23dead` = `471 20` (`103 60` of it the apply pass) |

CI-owned rows (not runnable here; no push): the `daemon` job (pins inside `go test -race ./...`,
`ci.yml:1096-1105`), the `integration` job's witness matrix (`ci.yml:1659`).

## D9. Decision note (contract clause 5)

The decision rule recorded in D1 is the contract's F1 as written, with one implementation choice the
contract leaves open: a non-empty identity that is not a parseable `linux:v2:` marker (parseable per
`parseLinuxNegativePIDReuseIdentity`, shared with the negative witness since the apply pass) has no
starttime, so it takes the fail-closed branch instead of `nil`. Rationale: the contract makes the
starttime the only absence rule and requires the classified error when the starttime cannot be read
(`unavailable when identity == ""` is the empty case it names; a malformed marker is the same
information gap); fail-closed cannot mask a live process. All non-empty identities at the helper's
call sites are canonical v2 markers, so the branch is unreachable from the witnesses and is pinned
only indirectly (K2's real-identity rows use parseable markers).

## D10. Apply-stage simplifications (2026-11-25)

Dispositioned by the conductor's apply policy (accept every simplify finding inside the owned set
that does not weaken tests, types, validation or permissions). Applied to the subject bytes:
finding 1 (zero-value child fields removed), 2 (`setLinuxWitnessEnvironment` for the re-exec child
env), 3 (starttime rule reuses `parseLinuxNegativePIDReuseIdentity` — closes the D9 partial-marker
gap), 4 (shared `requireLinuxWitnessUnreadableIdentityError` for the K1/K2 classified-message
assertions), 5 (`linuxWitnessProcessProbeText` renders `%v` directly), 7 (shared
`linuxWitnessProcStatFields`), 8 (`denied_read_probe_failure_fails_closed` +
`non_denial_error_records_probe_failure` cover the contract's probe-failure arm and the P1
`err=<non-nil>` rendering), 9 (`empty_identity_keeps_polling` counts reads, ≥ `2`), PD-1 (a
non-decimal `/proc` starttime now returns `malformed /proc stat` instead of a zero starttime),
PD-2 (`non_denial_error_carries_the_probe_record` `t.Logf`s the record-carrying error), PD-3 (K1
prerequisite skips only when the read succeeds). Rejected: finding 6 (would drop the `pid:[N]` link
self-check in `linuxWitnessCanonicalIdentity` — a validation reduction the apply rules bar).
Record-only: PD-4 (the `1 s` reaped-pid bound is recorded in D4), PD-5 (census counts, line refs and
the `isProcessIdentityNotFoundError` predicate corrected here and in the evidence record), PD-6 (the
leg-classifier limit is disclosed in evidence section 9). Gate re-runs on these bytes: D8/evidence
section 7; the mutant oracle re-ran on these bytes (evidence section 4).
