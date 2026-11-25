# rf-acc-guest-native-exec — evidence record

Goal rf-acc-guest-native-exec (GDD contract, state root), branch
`refactor/rf-acc-guest-native-exec`, base `6b2d4f8f` (PR #285 merge). Slice plan
`docs/plans/2026-11-25-rf-acc-guest-native-exec-plan.md`. Every number below is at the delivered
bytes unless a row names another revision.

## 1. Clause census at the delivered bytes (re-located by content)

| Contract | Delivered | Content | Disposition |
| --- | --- | --- | --- |
| `wire_linux.go:527` (incl. `parseRights`) | `:521-528` | `oobn != 0` branch, `parseRights` `:522` | `ancillary_data` typed |
| `wire_linux.go:530` | `:530-533` | truncated/oversized | `truncated_frame` typed |
| `wire_linux.go:534` | `:534-538` | zero-length packet with `MSG_EOR` | `empty_packet` typed (input-unreachable on Linux AF_UNIX, §5) |
| `wire_linux.go:539` | `:541-559` | magic/version mismatch; v1 gate frame recognized `:545-556` | `magic_or_version` / `gate_v1_frame` typed |
| `wire_linux.go:559` (clean EOF) | `:585-586`, `:603-604`, `:621-622` | `n == 0` without `MSG_EOR` | `peer_closed_without_frame` typed, wraps `io.EOF` |
| `wire.go:486` | `:526-533` (`validateCloneStatusFrame`) | version/type/size mismatch | `frame_header` typed |
| `wire.go:492` | `:549-551` | inode 0 | `image_inode_missing` typed |
| `wire.go:619-625` | `:677-687` | kind-24 malformed / code invalid | `guest_failure_malformed` / `guest_failure_code` typed |
| `waitForStatus`/`recvmsg` | `wire_linux.go:512-520` | wait and receive errors | `wait_failed` / `recv_failed` typed |
| `gate_lifecycle_linux.go:700-707` | `:712-722` | `ReleaseSubStepError` | gains `ReadClass` |
| `gate_lifecycle_linux.go:739-751` | `:752-766` | `ReleaseWaitErrorClass` | unchanged |
| `helper_v2_protocol.go:469-505` | `:467-510` | detail + `String()` | gains `read_class` after `wait_class` |
| `helper_v2_protocol.go:508-566` | `:512-587` | `Validate()` | gains the closed read-class rule |
| `helper_v2_protocol.go:570-591` | `:589-613` | `…DetailFor` | fills `ReadClass` |
| `wire_linux.go:685`, four `stale_journal_*` rows, exit/unresolved contract, `daemon/e2e/**` | — | out of scope by contract | untouched |

## 2. Stage-1 changes (behavior-preserving diagnostics)

1. `CloneStatusReadError{Class string; Errno int; Err error}` in `wire.go`; `Error()` returns the
   cause's message verbatim, `Unwrap()` exposes it (so `errors.Is(err, io.EOF)` and every rendered
   line stay byte-identical). Closed vocabulary: `wait_failed`, `recv_failed`, `ancillary_data`,
   `truncated_frame`, `empty_packet`, `magic_or_version`, `gate_v1_frame`,
   `peer_closed_without_frame`, `frame_header`, `image_inode_missing`, `guest_failure_malformed`,
   `guest_failure_code`.
2. `receiveCloneStatusPacket` types each producer; the magic/version branch recognizes a v1 gate
   frame (`WIRE_VERSION` 1, kinds 7/8) as `gate_v1_frame` and carries the kind-8 frame's u32 errno
   (`n >= 16`), keeping the same refusal text; the three arm reads type their clean EOF.
3. `ReleaseArmShape.CloneRead` (true for `bootstrap_attempt`, `checkout_receipt`,
   `clone_exec_proof`); `NewReleaseSubStepError` extracts the sentinel and its errno through the
   wrap chain; `ReleaseSubStepError.ReadClass` carries it.
4. `LinuxOwnerHelperV2ReleaseErrorDetail.ReadClass` (`read_class`, omitempty), rendered after
   `wait_class`; `Validate()` closes the vocabulary and refuses a read class on an arm that cannot
   read the clone channel; `…DetailFor` copies it. `error_code`, arms, `wait_class` and the
   `String()` of every existing detail are unchanged.

## 3. Producer table (Change 3)

| Sentinel | Exact site | Wire bytes |
| --- | --- | --- |
| `wait_failed` / `recv_failed` | App read `wire_linux.go:512-520` | none (no frame) |
| `ancillary_data` | App read `:521-528` | frame plus SCM_RIGHTS (or unparsable ancillary) |
| `truncated_frame` | App read `:530-533` | frame larger than 64 KiB |
| `empty_packet` | App read `:534-538` | a zero-length record (platform-unreachable, §5) |
| `magic_or_version` | App read `:541-544`, `:558` | a frame outside the v1/v3 gate families |
| `gate_v1_frame` kind 8, errno=E | gate `exec_as_b` pre-clone guard `gate.c:1273-1299` → `send_exec_failure` `:1026-1034` | 16 B: `SYMLA001`, v1, kind 8, u32 errno=E (`close` EBADF; `drop_to_b` EPERM/EINVAL; `PR_SET_PDEATHSIG` EINVAL/EPERM; `check_exec_guard` ETIMEDOUT/ESRCH/ECANCELED/EINVAL/poll errno; `check_zero_groups` sets none, so a nonzero count reports a stale errno) |
| `gate_v1_frame` kind 7 | gate `exec_as_b` attempt build/send `:1354-1362` | 28 B: v1 kind 7, dev/inode (non-clone attempt frame; unreachable for a v3 clone release) |
| `peer_closed_without_frame` | gate `gate_run` silent exits (`:1449-1465` adoption/release wait, `check_parent`, `read_release`) and any failure-send that also fails | none |
| `frame_header` | App decode `wire.go:526-533` | a v3 frame of the wrong kind/size for this arm (21/23 to the attempt reader, 20 to the receipt reader) |
| `image_inode_missing` | App decode `:549-551` | v3 kind 20 with inode 0 |
| `guest_failure_malformed` / `guest_failure_code` | App decode `:677-687` | v3 kind 24 size ≠ 20, or stage outside 1..18 / errno 0 or > u31 |
| decoded guest failure (`guest_stage`+`errno`) | gate `send_clone_bootstrap_failure` `gate.c:1036-1044` at `:1197`, `:1203`, `:1216`, `:1224`, `:1238`, `:1256`, `:1268`; guest `cmd/symmetry-linux-clone-bootstrap/main.go:22-32` for every `RunBootstrap` failure | 20 B: v3 kind 24, u32 stage 1..18 (guest 1..7, gate-side 8..18), u32 errno |
| gate pre-clone guard failure (`guest_stage` 8..18) | gate `exec_as_b` pre-clone chain `gate.c:1274-1299` → `send_preclone_failure` `:1049-1055`; the credential-fact classifier `:1061-1098` | 20 B: v3 kind 24 (clone release) with the guard stage; a non-clone release keeps the 16 B v1 kind 8 frame (`§10`) |

## 4. Local gates (delivered stage-1 bytes)

WSL Ubuntu-24.04 `-u accept`, from `daemon/`, Go 1.27.0, exit codes measured:

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| format | `gofmt -l .` | 0 | no file listed |
| linux vet | `GOOS=linux go vet ./...` | 0 | clean |
| contract gate (race) | `go test -race -count=1 -timeout 1200s ./native/linux-gate/protocol/ ./internal/platform/ ./internal/clonebootstrap/ ./internal/launchercore/ ./cmd/symmetry-linux-clone-bootstrap/ ./cmd/symmetry-linux-launcher/` | 0 | `ok protocol 12.969s`, `ok platform 79.525s`, `ok clonebootstrap 1.129s`, `ok launchercore 1.332s`, `no test files` (clone-bootstrap cmd), `ok launcher 1.071s` |
| C census probe | compiled and driven by `TestGateFDCensusAllowsStdioAndRejectsUnexpectedDescriptors` + `TestGateReleaseFenceOrdering` inside the protocol suite | 0 | `cc -std=c11 -O2 -I ../c-gate`; all four fence scenarios pass on the unmutated gate |
| production C build | `cc -O2 -static -fno-pie -no-pie -Wl,-z,relro,-z,now -Wl,-z,noexecstack -o <out> native/linux-gate/c-gate/gate.c` and the same for `cmd/symmetry-linux-launcher/c-wrapper/wrapper.c` | 0 / 0 | both are static x86-64 ELF executables |
| race re-run after the vocabulary pin | `go test -count=1 -race ./native/linux-gate/protocol/ ./internal/platform/` | 0 | `ok protocol 10.485s`, `ok platform 73.717s` |
| host vet | `go vet ./...` (Windows host, from `daemon/`) | 0 | clean |
| scripts | `git diff --stat -- scripts/ .github/` | — | empty: no script or real workflow byte changed by this goal (the scratch copy is branch-only) |

## 5. Probes (lesson 74)

- `wire_linux_test.go`, `TestCloneStatusReadClassNamesEveryProducer` (19 rows): every closed
  producer is driven through a real `AF_UNIX`/`SOCK_SEQPACKET` arm read and pins the exact class,
  the extracted errno and the unchanged rendered text; the v1 kind-8 row uses the golden literal
  `53594d4c41303031010008000d000000` and asserts it equals `EncodeExecFailure(13)`.
- `TestCloneStatusReadClassSplitsNeighbouringProducers` (9 rows): each producer must not collapse
  onto its neighbour of the same read family (v1 frame vs unrecognized version, frame header vs
  missing inode, malformed vs invalid guest code, truncation vs header, zero-length record vs peer
  close, peer close vs version mismatch).
- `TestCloneStatusReadClassAbsentOnSuccess`: a completed read carries no sentinel, and a peer close
  still renders `ReleaseWaitErrorClass` `peer_closed` (the wrapped `io.EOF` stays Is-comparable).
- `TestCloneStatusReadClassVocabularyIsPinned`: all 12 codes pinned by literal, duplicates refused
  (this row is what mutant M2 reddens).
- Platform carry: `…ClassifiesSubSteps` gains five rows (v1 frame + errno, receipt peer close,
  proof frame header, and the shape-gate negative), `…RendersTheReadClass` pins three literal
  renderings, `…CarriesTheReadClassOverTheWire` drives the real response codec + validator +
  client, `…ValidatesClosedVocabulary` gains five rows (closed class accepted, wrong-shape arm
  refused, free-form refused, class without a sub-step refused) plus an acceptance loop over all
  twelve clone-read codes (§13), and `…RedactsFreeFormText` gains a clone-read block whose payload
  carries a path and the raw frame text.
- Platform finding (probe, throwaway program): Linux `AF_UNIX` `recvmsg` never reports `MSG_EOR`
  (empty sendmsg, peer `SHUT_WR`, peer close and an empty `SOCK_DGRAM` datagram all render
  `flags=0x40000000`, the echoed `MSG_CMSG_CLOEXEC`, with `MSG_EOR` clear), so the `:534` producer
  is input-unreachable and a zero-length record is indistinguishable from a peer close: two rows
  pin `peer_closed_without_frame` and `mustNotClass=empty_packet` for that input.
- C census probe (owned test file): `TestGateReleaseFenceOrdering` runs fifteen scenarios
  (`exec-fence`, `exec-fence-expired`, `exec-drop-denied`, `clone-fence-expired`,
  `clone-attempt-fence`, `clone-close-failed`, `clone-parent-death-failed`, and the eight
  `clone-drop-*` classifier rows) in a compiled probe that includes the current `gate.c` with
  `sendmsg`/`poll`/`prctl`/`syscall`/`open`/`_exit`/credential interceptions: it pins the v1 attempt
  frame's shape/dev/ino, `PR_SET_PDEATHSIG`/`SIGKILL` before it, at least one guard poll before it,
  the post-send guard polls strictly between the frame and the intercepted `execveat`, the
  expired-lease v1 `FRAME_EXEC_FAILURE` with `ETIMEDOUT`, the clone attempt frame (v3 kind 20,
  pinned dev/ino) with its kind-24 stage-7 failure, the expired clone release's v3 kind-24
  stage-11 `gate_fence` frame with `ETIMEDOUT`, and one row per closed credential fact (12-18)
  plus the unclassifiable fallback (stage 9) — see §10.

## 6. Mutants (M1-M8; each applied to a scratch copy of the *stage-1* delivered bytes, restored byte-identical)

The restored digests below are the stage-1 delivered bytes the mutant runs restored (the delivered
bytes have since gained the stage-2 changes of §10, whose own rows follow this table).

| id | Mutation | Red probe (verbatim line) | Restored sha256 (stage-1 delivered) |
| --- | --- | --- | --- |
| M1 | `wire_linux.go`: the `:539` v1-frame branch returns the generic `magic_or_version` class (sentinel reverted) | `TestCloneStatusReadClassNamesEveryProducer/v1_gate_failure_frame`: `clone-status read class = "magic_or_version" errno=0, want class="gate_v1_frame" errno=13` | `fa4aedf9…` |
| M2 | `wire.go`: swap the `ancillary_data`/`truncated_frame` literals | `TestCloneStatusReadClassVocabularyIsPinned`: `read class constant named "ancillary_data" carries "truncated_frame"` | `6fe7b33c…` |
| M3 | `gate_lifecycle_linux.go`: `NewReleaseSubStepError` never sets `ReadClass` | `…ClassifiesSubSteps/clone_bootstrap_attempt_names_the_v1_gate_failure_frame_and_its_errno`: `ReadClass:` empty in the rendered detail | `7fad4b4a…` |
| M4 | `gate_lifecycle_linux.go`: carry the field on every arm (`shape.CloneRead` gate removed) | `…ClassifiesSubSteps/native_exec_errno_never_carries_a_read_class` | `7fad4b4a…` |
| M5 | `wire_linux.go`: recognize the v1 frame but discard its errno | `TestCloneStatusReadClassNamesEveryProducer/v1_gate_failure_frame`: `errno=0, want … errno=13` | `fa4aedf9…` |
| M6 | `wire_linux.go` + `wire.go`: accept a v1 frame where the version contract must refuse it (`version != 3 && version != 1` at the receive and frame-header checks) | `…/v1_gate_failure_frame`: `clone-status read class = "frame_header" errno=0, want class="gate_v1_frame" errno=13` | `fa4aedf9…`, `6fe7b33c…` |
| M7 | `helper_v2_protocol.go`: `…DetailFor` fills `read_class` with the raw error text | `…RendersTheReadClass`: rendered `read_class=bounded probe wrap: clone status frame magic or version is invalid`, and `…RedactsFreeFormText` leaks the payload | `f94e2be4…` |
| M8 | `gate.c`: move the post-send exec guard before the attempt-frame send | `TestGateReleaseFenceOrdering/exec-fence`: `reason=post-send guard polls strictly between the attempt frame index 63 and execveat index 64: 0; want the check_exec_guard owner-pidfd and owner-pipe polls` | `b93d9c1b…` |

### Stage-2 mutants (M9-M14; the same procedure against the current delivered bytes)

| id | Mutation | Red probe (verbatim line) | Restored sha256 (delivered) |
| --- | --- | --- | --- |
| M9 | `gate.c`: `send_preclone_failure` always sends the 16 B v1 frame (the family fix reverted) | `TestGateReleaseFenceOrdering/clone-fence-expired`: `reason=expired clone release sent version=1 kind=8 size=16; want version=3 kind=24 size=20` | `2bf552b4761c…` |
| M10 | `gate.c`: the fence guard reports the drop guard's stage (guard stages collapsed) | `…/clone-fence-expired`: `reason=expired clone release stage=9; want the fence guard stage 11` | `2bf552b4761c…` |
| M11 | `gate.c`: `classify_drop_failure` skips the groups fact | `…/clone-drop-groups`: `reason=credential fact 2 sent version=3 kind=24 size=20 stage=9 errno=1; want version=3 kind=24 size=20 stage=12 errno=1` | `2bf552b4761c…` |
| M12 | `gate.c`: the classifier also runs on the non-clone path | `…/exec-drop-denied`: `reason=the credential-fact classifier ran on a non-clone release` | `2bf552b4761c…` |
| M13 | `wire.go`: the kind-24 decoder range stops at the fence stage | `TestWireFrameRoundTrips`: seven failures, `clone_bootstrap_failure_gate_guard_12` … `_18` | `771632ba99bd…` |
| M14 | `helper_v2_protocol.go`: the validator drops the credential-fact names | `TestLinuxOwnerHelperV2PrecloneGuardFailureNamesTheGateStage`: `pre-clone guard stage "gate_drop_credentials" is not closed-vocabulary: Linux owner helper release error detail has an unknown guest bootstrap stage "gate_drop_credentials"` | `4ac73de34f90…` |

## 7. Scratch runs (dispatch-run rule: every listed case must pass; the runner verdict is global)

Scratch transport: branch-only `scratch/rf-acc-guest-native-exec` in a disposable clone; the
workflow copy and its log shim are scratch-only artifacts (workflow sha256 `2200b0b6…`, shim
`507340af…`). The branch is deleted and `ls-remote` proven empty at the end of the goal.

| Run | Head | Case sets | gh verdict | First failing check (verbatim) |
| --- | --- | --- | --- | --- |
| 1 | `6dd5867c` | `contracts-build`, `migration` | contracts success, acceptance failure (expected at stage 1) | `release_outcome_unknown; stage=clone_bootstrap arm=bootstrap_attempt wait_class=other read_class=gate_v1_frame errno=1` |
| 2 | `57e5ed3e` | `contracts-build`, `migration` | contracts success (1m33s), acceptance failure | `production_linux_v2_native_witness_test.go:613: wait for exact native_release boundary: owner-witness command did not reach its requested durable stage`; release detail `release_outcome_unknown; stage=clone_bootstrap arm=bootstrap_attempt wait_class=other guest_stage=gate_drop_bounding_set errno=1` |
| 3 | `74efe855` | `contracts-build`, `migration`, `stale_leaf` | contracts success (1m22s); `Case set migration exited 1`, **`Case set stale_leaf exited 0`** | `production_linux_v2_native_witness_test.go:613: wait for exact native_release boundary: owner-witness command did not reach its requested durable stage`; release detail `release_outcome_unknown; stage=clone_bootstrap arm=bootstrap_attempt wait_class=other guest_stage=gate_drop_bounding_set errno=1` |

Run 3 evidence artifacts: `linux-v2-native-evidence-45602546196-1` (id `15017315263`, sha256
`c9fa79cd86c564fff99ffbf97c7acdad421c496498640c731b39069a9c8e4042`, 4359 B — the two case sets'
manifests) and `linux-v2-host-prerequisite-45602546196-1` (id `15016576502`, sha256
`cb7af6930a0c5c44b78ba2a2eaa2dd7d943df25bc24397a36db7096998673647`, 675 B); run head
`74efe85562d734576912249f5b78f78ba6edabbe` carries the delivered bytes (§11).

Run 3 is the final literal run and the run that states the named root cause, verbatim:
`release_outcome_unknown; stage=clone_bootstrap arm=bootstrap_attempt wait_class=other
guest_stage=gate_drop_bounding_set errno=1` — the same named fact as run 2 — and the `stale_leaf`
no-regression row is green. Nothing after run 3 changed a code byte.

Run 2 evidence artifacts: `linux-v2-native-evidence-45601287283-1` (id `15016660780`, sha256
`c17ea0605086cf454de7c066bf79524aaae1795a7bf152e1871d8878d6044855`, 1568 B) and
`linux-v2-host-prerequisite-45601287283-1` (id `15015961117`, sha256
`a9e52a9af69cb8e5fd74596b4028ba5ae1b331aed10c3d8a6e3b7826599b2c31`, 674 B);
`Case set migration exited 1` / `Failed case sets: migration`.

Run 2 read of the sentinel: the clone read decoded the gate's v3 kind-24 guard frame (the family fix
works: `read_class` is gone, `guest_stage` is present) and the classifier named the drop's first
unsatisfied fact: the **bounding set** is still non-zero after `PR_CAPBSET_DROP` returned `EPERM`.
An ns-root gate (the userns creator holds every capability in that namespace) that is refused a
capability *use* is refused by policy, not by a missing capability: the guest's AppArmor
`unprivileged_userns` transition denies the gate's capability use after its `unshare(CLONE_NEWUSER)`
— the mechanism `docs/TODO.md` already records for exactly this launcher/gate pair (per-program
`userns` profile for `scripts/install-linux-owner.sh`; same exception needed by
`scripts/ci/linux-v2-native-acceptance.sh`), and the shape the host-prerequisite probe excludes from
its own evidence (`not_evidence_of: production launcher/gate under the AppArmor
unprivileged-userns restriction`).

That fix is outside this goal's owned set (`scripts/ci/**`, `.github/workflows/**` and the
installer are explicitly not owned), and no owned change can substitute for it: the invariant
`CapBnd == 0` is load-bearing (`drop_to_b` reads it back), the denial is a capability *use* denial
that re-ordering cannot avoid, and skipping or tolerating the bounding-set drop would weaken a
credential invariant. The BLOCKED report carried `DECISION_NEEDED` with the verbatim sentinel
above; the conductor ruling (2026-11-25, on that report) accepts the stage-1 sentinels and the
stage-2 diagnostics as this goal's deliverable, routes the remaining fix to follow-up goal
`rf-acc-apparmor-userns-profile`, and directs the residual to stand under unmet criteria (DONE
with residual; no further code byte, no further scratch run).

Run 1 evidence artifacts: `linux-v2-native-evidence-45593856716-1` (id `15012631530`, sha256
`05ead60baddf3e4be3ef25d9548b231fb29ce6911750df7dff4f2078a2a7ffc7`, 1571 B; members `manifest`
`c4f191ea…` and `migration-denial.json` `025e85f0…`) and `linux-v2-host-prerequisite-45593856716-1`
(id `15012526444`, sha256 `983aa6242c861e66e8466e997ec46651fdcc27acf5f1156ac974e4c12a6adef4`, 675 B).
The prerequisite probe reports `apparmor_userns_exception=per_program` and lists
`production launcher/gate under the AppArmor unprivileged-userns restriction` in `not_evidence_of`:
the runner host is not evidence about the guest's own policy.

Run 1 read of the sentinel: `read_class=gate_v1_frame` selects §8.1 of the plan (the pre-clone guard
failed and reported in the v1 family a v3 read refuses). `errno=1` is the credential-drop family:
`drop_to_b` returning `-1`, `check_zero_groups` surfacing a stale errno, or `read_self_status`'s
`seen != 511` gate, which fabricates `EPERM` — three causes one errno cannot separate, which is why
§8.1 adds the read-only credential-fact classifier before run 2.

Also observed in run 1: the parent's `stale_leaf` journal carries `has_release_proof=false` and
`has_checkout_receipt=false`, so that leg's release fails at the same guard and its denial test
tolerates it; the guard failure is systematic for every release in this guest, while the migration
leg is the only leg whose success the contract requires.

## 8. Lesson-83 assertion trace (assertions after the changed check)

- `migration` (`production_linux_v2_native_witness_test.go:613` holds `native_release`): after the
  release completes, `linuxV2ValidateStageJournalBoundary`, the external live-process capture,
  `linuxV2CgroupContainsPID`/migration, the fail-closed recovery restart, the response-state check
  and the retained-journal check (`production_linux_v2_owner_cli_test.go:3890-3998`) execute for
  the first time in any run of this goal or its parent.
- `stale_leaf`: green since run `45557621798` (parent); re-run as this goal's no-regression row.

## 9. Residuals and limits

- `readCloneNativeExecResult`'s post-attempt follow-up text
  (`clone status channel carried data after native exec attempt`) stays untyped: it is not an
  enumerated producer of the split (recorded here, not fixed).
- The `empty_packet` producer is unreachable from a Linux AF_UNIX send (§5): a zero-length record
  is indistinguishable from a peer close and renders `peer_closed_without_frame`.
- **AppArmor-userns wall (conductor ruling 2026-11-25).** Three legs are recorded as residuals
  behind the named cause: `migration`'s `native_release` hold (still unreached on the final bytes,
  run `45602546196`, verbatim in §7); `synthetic_goal_cleanup`, which the credentialed `main` run
  `45573253632` reads as `production_linux_v2_cleanup_witness_test.go:499: start
  controlled B Git worker through the real Linux v2 launcher/anchor/helper: resume process
  "/bin/sh": one-use Linux v2 RELEASE result is unresolved: Linux owner helper rejected operation
  (release_outcome_unknown)` — the same release class as `migration`'s arm; and `shared`'s
  `clone_integration_linux_test.go:240` / `launcher_integration_linux_test.go:439`.
  Ruling question answered from the logs: neither `:240` nor `:439` shows a `guest_stage` — the
  `shared` set was never executed on the fixed bytes (runs `45593856716`/`45601287283`/`45602546196`
  ran `migration`; run 3 also `stale_leaf` — and contain no hit for either test name); their
  last observed lines are pre-fix, run `45573253632`: `clone_integration_linux_test.go:240: one-use
  B clone RELEASE and durable checkout ACK/native exec: Linux owner launch outcome is unknown;
  retain the pinned leaf and do not retry: await guarded native exec result: receive same-PID B
  bootstrap attempt: clone status frame magic or version is invalid` and
  `launcher_integration_linux_test.go:439: one-use fixed-B RELEASE and same-PID exec: native exec
  failed after RELEASE: errno=1`. Both consume the pre-clone guard family this goal's split names;
  their closure belongs to `rf-acc-apparmor-userns-profile`.

## 10. Stage-2 changes: the clone-family pre-clone frame and the closed credential facts

Run 1's sentinel (`read_class=gate_v1_frame errno=1`) selected plan §8.1. The delivery is:

1. **Family fix (Q-C, bounded numeric).** `exec_as_b`'s pre-clone guards were split into four
   blocks — `close(RELEASE_FD)`, `drop_to_b`, `prctl(PR_SET_PDEATHSIG, SIGKILL)`,
   `check_exec_guard` — in the original order with the original short-circuit semantics and the
   same `_exit(1)` on each. `send_preclone_failure(command, stage, errno)` sends the existing v3
   `FRAME_CLONE_BOOTSTRAP_FAILURE` (kind 24, 20 B) for a clone release and the existing v1
   `FRAME_EXEC_FAILURE` (kind 8, 16 B) for every other release, with the failing call's own errno
   (no fabricated errno; the senders' `errno == 0 → EPERM` mapping is unchanged and sits in the
   same place as before).
2. **Guard stages 8-11** name the failing guard: `gate_close`, `gate_drop`, `gate_parent_death`,
   `gate_fence`.
3. **Credential facts 12-18.** `classify_drop_failure` runs only after `drop_to_b` failed, only for
   a clone release, and only after the errno was captured. It re-derives read-only (getgroups,
   `PR_CAPBSET_READ` 0..63, getresuid/getresgid, `SYS_capget`, `PR_CAP_AMBIENT` `IS_SET`,
   `PR_GET_NO_NEW_PRIVS`, then the drop's own `read_self_status`) the first unsatisfied fact in the
   drop's order: `gate_drop_groups` (12), `gate_drop_bounding_set` (13), `gate_drop_credentials`
   (14), `gate_drop_capabilities` (15), `gate_drop_ambient` (16), `gate_drop_no_new_privs` (17),
   `gate_drop_status` (18), and `gate_drop` (9) when no listed fact is unsatisfied or the facts
   cannot be read. The drop's calls, order, decisions and fence are untouched; the non-clone path
   does not call the classifier at all (probe-pinned).
4. **Vocabulary wiring.** `wire.go` extends the closed stage enum to 18 with both range checks;
   `ReleaseGuestStageName` names all 18 stages; `helper_v2_protocol.go`'s validator accepts the 11
   gate names; the App renders the detail verbatim, e.g.
   `stage=clone_bootstrap arm=bootstrap_attempt wait_class=other guest_stage=gate_drop_credentials errno=1`.
5. **Tests and probes.** `wire_test.go` round-trips stages 8-18 and refuses 19; a Go test pins the
   numeric values 1-18 so the C enum and the Go decoder cannot diverge silently; the C census probe
   has 15 scenarios (10 of them class fact/guard rows, listed in §5); the errno read from a v1
   kind-8 frame is bounded to the signed 31-bit range at the recognition site (an out-of-range
   value renders as `errno` 0 instead of wrapping negative on a 32-bit build).

Known labelling limits (recorded, not fixed): `PR_SET_KEEPCAPS,0` cannot fail for the constant
arguments the gate passes and no read-only probe can reveal it, and a failed `PR_SET_DUMPABLE,0`
would be labelled `gate_drop_capabilities`; both are unreachable in the delivered chain and neither
can hide a failure (the frame, the errno and `_exit(1)` are unconditional). A gate and a daemon of
different vintages would render `guest_failure_code` for stages 8-18 (no version negotiation); the
installer ships both together.

### 10.1 Local gates at the delivered bytes (stage 2)

WSL Ubuntu-24.04 `-u accept`, from `daemon/`, Go 1.27.0, exit codes measured:

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| format | `gofmt -l .` | 0 | no file listed |
| linux vet | `GOOS=linux go vet ./...` | 0 | clean |
| contract gate (race) | `go test -race -count=1 -timeout 1200s ./native/linux-gate/protocol/ ./internal/platform/ ./internal/clonebootstrap/ ./internal/launchercore/ ./cmd/symmetry-linux-clone-bootstrap/ ./cmd/symmetry-linux-launcher/` | 0 | `ok protocol 7.728s`, `ok platform 66.885s`, `ok clonebootstrap 1.027s`, `ok launchercore 1.160s`, `no test files` (clone-bootstrap cmd), `ok launcher 1.023s` |
| C census probe (15 scenarios) | `TestGateReleaseFenceOrdering` inside the protocol suite | 0 | all 15 scenarios pass on the unmutated gate |
| production C build | `cc -O2 -static -fno-pie -no-pie -Wl,-z,relro,-z,now -Wl,-z,noexecstack -o <out> native/linux-gate/c-gate/gate.c` and the same for `cmd/symmetry-linux-launcher/c-wrapper/wrapper.c` | 0 / 0 | both static x86-64 ELF executables |
| host vet | `go vet ./...` (Windows host, from `daemon/`) | 0 | clean |
| scripts | `git diff --stat -- scripts/ .github/` | — | empty: this goal changed no script or real workflow byte |

Audit trail at these bytes: one Sol reviewer, one security reviewer and one independent
DeepSeek-V4.1 auditor ran read-only over the delta (c-gate change reviewed before the run that binds
it). Their findings — the probe rows pairing a fact stage with another call's failure, the missing
`gate_close`/`gate_parent_death` rows, the order pin, the unbounded v1 errno copy and the
`PR_SET_KEEPCAPS`/`PR_SET_DUMPABLE` labelling limits — are all resolved above (§5, §6, §10) or
recorded as limits; no finding was left unchecked.

This slice ships the family fix and the closed classifier. Run 2's named fact
(`guest_stage=gate_drop_bounding_set errno=1`), reconfirmed by the final run 3 (`45602546196`),
routed the remaining cause to `DECISION_NEEDED` (§7): the guest's AppArmor `unprivileged_userns`
policy denies the gate's capability use, and its fix sits in the installer/fixture that this
contract does not own (follow-up goal `rf-acc-apparmor-userns-profile`).

## 11. Delivered-bytes equality (run 3's head vs the goal worktree)

| File | sha256 (first 12 hex) |
| --- | --- |
| `daemon/native/linux-gate/c-gate/gate.c` | `2bf552b4761c` |
| `daemon/native/linux-gate/protocol/wire.go` | `771632ba99bd` |
| `daemon/native/linux-gate/protocol/wire_linux.go` | `6205fe6310a2` |
| `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go` | `76f8a0ff803a` |
| `daemon/native/linux-gate/protocol/wire_linux_test.go` | `1ae0604034d4` |
| `daemon/native/linux-gate/protocol/wire_test.go` | `14f62c91bd51` |
| `daemon/native/linux-gate/protocol/gate_census_linux_test.go` | `2cb30641fabe` |
| `daemon/internal/platform/helper_v2_protocol.go` | `4ac73de34f90` |
| `daemon/internal/platform/linux_owner_helper_v2_linux_test.go` | `7d76dc67f7cc` |
| `docs/plans/2026-11-25-rf-acc-guest-native-exec-plan.md` | `c6391c0a8e15` |
| `docs/evidence/rf-acc-guest-native-exec-2026-11-25.md` | `5d3d5f174b6c` (the revision run 3's head carries; this record is finalized after run 3 by design) |

Run 3's head is the scratch commit `74efe85562d734576912249f5b78f78ba6edabbe`; `git show
<head>:<path>` reproduces every digest above byte-for-byte. Every `daemon/**`, plan and script
path in the worktree is byte-identical to that commit, except the three test files the apply stage
rewrote (§13); the scratch workflow copy and its `gh` shim live only in that commit and are
removed from the worktree (§7).

After run 3 — the last code-affecting action — only records changed, per the conductor ruling of
2026-11-25 (the AppArmor-userns residual update): `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`
and this record were finalized. The apply stage then rewrote three test files (scaffolding and
coverage only; §13) and refreshed this record's §1, §3 and §5 numbering; no production byte, wire
byte, `gate.c` byte or plan byte changed after run 3, and run 3 remains the bound revision for
every production row above. The two TODO files' digests are not recorded here, per the owner
comment policy.

Scratch cleanup proof (contract): `git ls-remote origin scratch/rf-acc-guest-native-exec` prints
nothing; the only remaining `refs/heads/scratch/*` ref is `scratch/rf-acc-owner-loss-semantics`,
another goal's.

## 12. Grep census (Change 7)

Commands run from the repo root at the delivered bytes; every hit is converted or exempted with its
reason:

| Census | Hits | Disposition |
| --- | --- | --- |
| `rg -n "read_class\|ReadClass" daemon --glob '!*_test.go'` | exactly 4 files: `wire.go`, `wire_linux.go`, `gate_lifecycle_linux.go`, `helper_v2_protocol.go` | all four owned and converted (vocabulary type, producers, carry + arm shape, bounded detail/validator) |
| `rg -c "cloneStatusReadFailure\|CloneStatusReadError"` | `wire.go` 10, `wire_linux.go` 12 | every clone-read producer is typed; `wire_linux.go` additionally bounds the v1 frame's errno to the signed 31-bit range |
| `rg -n "exec-status channel closed or carried ancillary data before a frame" daemon` | `wire_linux.go:706` plus two platform rows that pin the resulting `wait_class` | exempt: the non-clone native-status read is out of this goal's root cause by contract; its bytes and text are unchanged |
| `rg -n "guest_stage\|GuestStage" daemon/internal/platform` | `helper_v2_protocol.go` (field, `String()`, `Validate()`, `DetailFor`) and its test file | converted: the closed stage vocabulary is validated on receipt and rendered verbatim |
| `git diff --stat -- scripts/ .github/` | 0 lines | no script or real workflow byte changed by this goal (the scratch copy is branch-only) |

## 13. Apply stage — accepted simplify findings (test-only delta after run 3)

The goal's apply stage applied the accepted simplify findings. No production byte changed:
`gate.c` `2bf552b4761c`, `wire.go` `771632ba99bd`, `wire_linux.go` `6205fe6310a2`,
`gate_lifecycle_linux.go` `76f8a0ff803a`, `helper_v2_protocol.go` `4ac73de34f90` and
`wire_test.go` `14f62c91bd51` stay at their §11 run-3 digests. Three test files changed;
post-apply sha256, first 12 hex:

| File | Change | sha256 |
| --- | --- | --- |
| `daemon/native/linux-gate/protocol/wire_linux_test.go` | the probe harness returns the read end from `prepare` (the fd-rewriting swap and the single-use `Keep` wrapper are deleted); the duplicated `attempt` closure is one shared helper | `d536eceda115` |
| `daemon/native/linux-gate/protocol/gate_census_linux_test.go` | one shared `compileGateProbe` fixture replaces the two copied compile blocks; four comment blocks compressed to the two-line policy; the clone-close, clone-parent-death and post-`execveat` rows now assert the failing call's own errno | `0ca1c1132f7e` |
| `daemon/internal/platform/linux_owner_helper_v2_linux_test.go` | the closed-vocabulary validator test gains an acceptance loop over all twelve clone-read classes; one comment block compressed | `dbd644ae4968` |

Applied: simplify findings 2, 3 (scaffolding), 4, 5 (coverage), 6, 7, 8 (the §1/§3/§5
re-derivations above), plus finding 1's five test-file comment blocks. Not applied: finding 1's
`gate.c:1057-1060` block — a `gate.c` byte change is frozen by the conductor ruling ("no further
code change" after run 3) and would re-baseline the §11 digest of the bytes the run-3 audit trail
binds.

Every-class census for the new loop (the only new "every X" clause): the twelve
`CloneReadClass*` constants are defined at `wire.go:494-505`; `Validate()`'s switch spans
`helper_v2_protocol.go:560-567` with the twelve constants across four case lines; the loop binds
all twelve against a `bootstrap_attempt` detail; no producer, literal or other consumer changed.

### 13.1 Mutation row (scratch copy, restored byte-identical)

| id | Mutation | Red probe (verbatim line) | Restored sha256 |
| --- | --- | --- | --- |
| M15 | `helper_v2_protocol.go`: delete `linuxgate.CloneReadClassTruncated` from `Validate()`'s read-class switch | `linux_owner_helper_v2_linux_test.go:1343: clone-read class "truncated_frame" is not closed-vocabulary: Linux owner helper release error detail has an unknown read class "truncated_frame"` | `4ac73de34f901c91a3cef10e3704e9530f520db321deb6af4e38669bc6eb9d75` (mutated scratch digest `37ee37418c19bc03913d3ac5957d9e7c13bae34cc129dd8f813ef391be8f4fca`) |

### 13.2 Local gates at the post-apply bytes

WSL Ubuntu-24.04 `-u accept`, from `daemon/`, Go 1.27.0, exit codes measured:

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| format | `gofmt -l .` | 0 | no file listed |
| linux vet | `GOOS=linux go vet ./...` | 0 | clean |
| contract gate (race) | `go test -race -count=1 -timeout 1200s ./native/linux-gate/protocol/ ./internal/platform/ ./internal/clonebootstrap/ ./internal/launchercore/ ./cmd/symmetry-linux-clone-bootstrap/ ./cmd/symmetry-linux-launcher/` | 0 | `ok protocol 6.926s`, `ok platform 65.448s`, `ok clonebootstrap 1.029s`, `ok launchercore 1.100s`, `no test files` (clone-bootstrap cmd), `ok launcher 1.028s` |
| C census probe (15 scenarios) | `TestGateReleaseFenceOrdering` and the stdio census inside the protocol suite | 0 | all scenarios pass, including the three new errno assertions (`EBADF`, `EINVAL`, `ENOEXEC`) |
| production C build | `cc -O2 -static -fno-pie -no-pie -Wl,-z,relro,-z,now -Wl,-z,noexecstack -o <out> native/linux-gate/c-gate/gate.c` and the same for `cmd/symmetry-linux-launcher/c-wrapper/wrapper.c` | 0 / 0 | both static x86-64 ELF executables |
| host vet | `go vet ./...` (Windows host, Go 1.27.4) | 0 | clean |
| scripts | `git diff --stat -- scripts/ .github/` | — | empty |

No new scratch run and no measurement harness: the run budget is closed by the conductor ruling,
and this goal records gates and runs, not a benchmark. Run 3 remains the bound run for every
production row; the apply delta is test scaffolding, coverage and this record.
