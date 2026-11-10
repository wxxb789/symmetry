# Goal 0006b convergence safety/recovery review

Date: 2026-09-29. Independent read-only review of the named convergence edits, direct callers, and minimum authority/recovery contracts.

## Observed subject

The tree was not a clean exact subject. HEAD was `d14debbf6ae3ded09e5013ef90952d347c01474d`; the unstaged diff stream hashed to `91817e967377efdb587c97ecb1e6a19d11949b04`. The owner CLI test was untracked, so neither identifier binds all reviewed bytes.

| Path | Git blob | SHA-256 |
|---|---|---|
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `ab2bb5919fd97582fc3d803521979e5e56c04506` | `532d75779ad24ae5b9f4b7fac80292443f55f0a27bffb2f1079042125c6640ce` |
| `daemon/internal/state/state.go` | `87396b6fe3fadcbc7b587bff7371f95fb0f6e652` | `d744fe4a00decb24e643907f2e7670f756aa394a015fb2f58af696fbd9d97def` |
| `daemon/internal/state/linux_clone_journal_test.go` | `7c92a48ade3832d39b972f229d2274f252732dbe` | `3c0b062246cbb3548d2b5a4f118eee82110a0d55a67cf28dddfaf513198721cb` |
| `daemon/internal/state/linux_cgroup_v2_test.go` | `162a93b22ae305c60553b4c2f20e06a3d4ed5e08` | `b54459e532494aafe3055419be8c292e39f9461e9e609213886bfb6e6310fe2d` |
| `daemon/internal/platform/linux_owner_anchor_linux_test.go` | `95602aafa0c124aeed973d4457262856ff2f53e1` | `2b63ca9ab72c5ce96bdd10304fd5f6d9bd739b8d5ca69d66a672a46465fef306` |

The combined tracked diff for the four tracked files hashed to `9d3d0c400cd0f45ccba5f4e8df25e01ffb4c61d5`. These identify this review snapshot, not an accepted commit.

## Prioritized findings

### P0/P1 - none in this convergence slice

No reviewed path converts terminal state, PID absence, normal release, or a synthetic identity into stop/no-target authority. The tests do not delete journals, clear uncertainty, or bypass fences.

### P2 - the eight-case predicate test is narrower than the production gate

`isStoppedRetained` accepts nonterminal rows only with valid v2 release and no process details, reservation, authority, pending containment, or unproven marker (`daemon/e2e/production_linux_v2_owner_cli_test.go:2020-2030`). Removing `TerminalState` is correct: physical release and task settlement are separate facts. Its eight cases (`:2031-2085`) cover that distinction and unresolved flags, but alone do not prove allowed leaf disposition, bound cgroup identity, or exact leaf absence.

The real fresh-run gate does: `linuxV2RequireSafelyReleasedPriorJournal` validates the full DTO/v2 StopReceipt, requires cgroup identity, allows only removed/absent-after-receipt, and `Lstat`s the exact leaf (`:2089-2110`). The shared fixture applies it to every prior journal before a new RunKey (`daemon/e2e/production_linux_v2_native_witness_test.go:1737-1746`). History checks preserve prior raw bytes and summaries (`production_linux_v2_owner_cli_test.go:2112-2139`).

One stale-request setup assertion uses only the weaker summary (`production_linux_v2_native_witness_test.go:244-252`). It cannot grant runtime authority because setup already used production replay and later requires unchanged bytes, but alone would miss malformed disposition or an extant leaf. Non-blocking recommendation: call the strong helper there too.

## Authority/recovery analysis

### Normal release remains unresolved, not no-target evidence

`linuxCloneAbortHistoryMatches` rejects every proof carrying `NormalReleaseProof` (`daemon/internal/state/state.go:4766-4787`). Safe-retirement classification (`:455-471`), abort fencing (`:1545-1565`), completion, and retention clear (`:1584-1643`) remain closed. Legacy normal-release shapes load only through `linuxCloneAbortPendingReleaseUnresolved`; their durable DTO must match while the row remains unresolved (`:4650-4676,4789-4798`). The regression (`linux_clone_journal_test.go:383-458`) checks effect rejection, legacy readability, cleanup ineligibility, and missing tombstone. Compatibility is retained without fabricated no-gate authority.

### Preclaim ordering now matches production

The test creates the unresolved foreign row/leaf before any target, runs preclaim, then inserts the target and checks post-claim and reservation CAS paths (`linux_cgroup_v2_test.go:478-548`). This matches `scanLinuxV2NativeAdmissionLocked`: preclaim has no target and rejects every unsafe foreign row; post-claim also binds the target fence (`state.go:363-433`). Both journals' bytes remain unchanged. This exposes rather than masks preclaim regressions.

### Terminal replay preserves effect boundaries

The replay matrix rejects exact intent and promotion-intent replay after terminal fencing with byte-unchanged assertions, while exact seed, checkout, and promotion receipts remain replayable (`linux_clone_journal_test.go:1241-1273`). Production agrees: clone-intent admission precedes exact replay (`state.go:1319-1362`), and promotion-intent replay requires an effect-ready lifecycle (`:1419-1468`). Receipt writers require prior exact intent/evidence and do not initiate the external effect. This is lost-response recovery, not terminal reauthorization.

### Anchor fixtures preserve authority

- Namespace ancestry uses an independently opened Anchor userns pin, separate from gate/sibling pins (`linux_owner_anchor_linux_test.go:632-726`).
- `registered_current` uses a real helper-owned child, creation identities, retained helper/child pidfds, and bounded context. It keeps the foreign process live outside the target leaf and rejects target-leaf presence before kill (`:1245-1335`).
- Reap evidence uses protocol envelope version 1 while nested process/exit authority identity stays version 2, matching production validation (`linux_owner_anchor_linux.go:37,821-841`; test `:1570-1607`). This is not an authority downgrade.

These are unit/kernel fixtures, not installed A/B evidence. Synthetic grants/local children cannot prove distinct UIDs, delegation, root launcher behavior, or real provider execution.

## Review limits

- Per instruction, no test, build, installer, workflow, binary, guest, or witness ran. Findings are static review only.
- This was not a whole-repository audit. Extensive unrelated work and the untracked owner CLI file prevent commit binding.
- No qualified amd64 systemd A/B host, protected Pi/OpenCode credentials, authentic provider run, or clean exact-subject CI evidence was available. That is missing external evidence, not a demonstrated defect.
- WSL2/rootless refusal and local tests cannot substitute for positive distinct-UID evidence or promote capability.

## Verdict

**Convergence slice: APPROVE, with the non-blocking P2 assertion hardening.** The edits preserve RunKey/fence, exact StopReceipt, unresolved-state, replay, and process-identity authority; they correct false fixtures rather than hide recovery failures.

**Original Goal 0006b: CHANGES/EVIDENCE NEEDED; not accepted.** It still needs a clean exact commit and qualified-host exact-subject positives for real Pi/OpenCode, authentic claim-scoped provider action, crash/replay, platform negatives, and independent review of that exact subject.
