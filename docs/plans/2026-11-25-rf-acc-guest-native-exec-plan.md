# rf-acc-guest-native-exec — implement plan

Contract: `goal-rf-acc-guest-native-exec.md` (GDD state root, external to the repository).
Base revision `6b2d4f8f` (PR #285 merge, this goal's dispatch gate); against `65bb41b1` that merge
re-writes two daemon paths (`git diff --stat 65bb41b1 6b2d4f8f -- daemon` = 2 files, 18 insertions(+),
38 deletions(-)): the owned `gate_lifecycle_linux.go` doc-comment rewrap and the R1–R4 fixture hoist in
`internal/app/linux_owner_witness_linux_test.go` (test-only), so six of the parent record's eight
§7.4 code rows stay EQ.
Branch `refactor/rf-acc-guest-native-exec`. Conductor rulings carried: Q-A(a) (c-gate C source and
the guest stage are owned, c-gate bytes need the Sol + DS + security-reviewer triple audit before a
run binds them), Q-B(a) (one new closed field `read_class` plus the extracted errno on the existing
bounded detail; arms and `error_code` unchanged), Q-C (a bounded numeric frame-behavior change is
pre-approved; anything beyond it is `DECISION_NEEDED`), Q-D(a) (final run = `migration` +
`stale_leaf`), Q-E(a) (close the two tracked stops only when the field sentinel names the same root).

The accepted plan is the contract; this file resolves only the implementation choices it leaves open,
the clause census at the dispatch head, the sentinel vocabulary, the producer table, the stage-1 →
stage-2 sequencing and what is out of scope.

## 1. Clause census at the dispatch head (re-located by content)

| Contract | Head `6b2d4f8f` | Content | Disposition |
| --- | --- | --- | --- |
| `gate_lifecycle_linux.go:1054-1057` | `:1054` read, `:1056-1057` wrap | `readCloneBootstrapExecAttempt` wrapped by `NewReleaseSubStepError(ReleaseSubStepCloneBootstrap, ReleaseArmBootstrapAttempt, 0, …)` | unchanged shape; the wrap now also extracts the read sentinel (§3) |
| `wire_linux.go:559` | `:559-575` | `readCloneBootstrapExecAttempt` (clean EOF `:565`, kind-24 branch `:567-573`, decode `:574`) | typed sentinels (§2) |
| `wire_linux.go:511-542` | `:511-542` | `receiveCloneStatusPacket`: `waitForStatus` `:512-515`, recvmsg `:517-520`, `parseRights`/ancillary `:521-527`, truncated `:529-530`, zero-length `:532-534`, magic/version `:538-539` | typed sentinels (§2) |
| `wire.go:486`/`:492` | `:486` call, `:473` mismatch text, `:492` inode | `validateCloneStatusFrame` + `decodeCloneExecAttempt` | typed sentinels (§2) |
| `wire.go:619-625` | `:619-625` | `DecodeCloneBootstrapFailure` malformed / code invalid | typed sentinels (§2) |
| `gate_lifecycle_linux.go:739-751` | `:739-751` | `ReleaseWaitErrorClass` | unchanged |
| `gate_lifecycle_linux.go:700-707` | `:698-708` | `ReleaseSubStepError` | gains `ReadClass` (§3) |
| `gate_lifecycle_linux.go:711-737` | `:711-737` | `LookupReleaseArmShape` | gains the clone-read shape flag (§3) |
| `helper_v2_protocol.go:469-505` | `:467-505` | `LinuxOwnerHelperV2ReleaseErrorDetail` + `String()` | gains `ReadClass` (§3) |
| `helper_v2_protocol.go:508-566` | `:508-567` | `Validate()` | gains the closed read-class rule (§3) |
| `helper_v2_protocol.go:570-591` | `:568-591` | `linuxOwnerHelperV2ReleaseErrorDetailFor` | fills `ReadClass` (§3) |
| `helper_v2_stop.go:302-304` | `:300-303` | `writeResponseWithDetail` | exempt: the detail struct is marshalled whole (§6) |
| `helper_v2_protocol.go:640` | `:640-643` | `readLinuxOwnerHelperV2Response` → `ReleaseErrorDetail.Validate()` | exempt: validation is the field's own rule (§6) |
| `helper_v2_protocol.go:653` | `:651-655` | `linuxOwnerHelperV2ResponseError` builds the typed unresolved error | exempt: it copies the whole detail (§6) |
| `wire_linux.go:527/:530/:534/:539/:559` | `:521-527/:529-530/:532-534/:538-539/:563-565` | the clone-read producers | typed sentinels (§2) |
| `clone_integration_linux_test.go:240` | `:240` | `t.Fatalf("one-use B clone RELEASE and durable checkout ACK/native exec: %v", releaseErr)` — the tracked `:539` stop | re-read against the sentinel; stage-2 §8 |
| `launcher_integration_linux_test.go:439` | `:439` | `native exec failed after RELEASE: errno=1` — the tracked `exec_errno` stop | re-read against the sentinel; stage-2 §8 |

## 2. Stage 1a — closed sentinels at the clone-read producers (Change 1)

One typed producer per site; no behavior change — every producer keeps its exact error text and the
same `wait_class` (the read still fails), only the error value is typed.

### 2.1 Carrier

In `daemon/native/linux-gate/protocol/wire.go` (cross-platform, no unix dependency; the same file
already owns `cloneStatusWireVersion` and the clone frame vocabulary):

```go
type CloneStatusReadError struct { Class string; Errno int; Err error }
```

`Error()` returns the wrapped cause's message verbatim (so every existing rendered line stays
byte-identical), `Unwrap()` exposes the cause (so `errors.Is(err, io.EOF)` still renders
`peer_closed`). Constructed by `cloneStatusReadFailure(class, err)`; the v1-frame case is the one
site that also sets `Errno`.

### 2.2 Closed vocabulary (exact literals; the plan pins them)

| Code | Producer (head) |
| --- | --- |
| `wait_failed` | `wire_linux.go:512-515` `waitForStatus` (deadline, canceled context, poll error) |
| `recv_failed` | `wire_linux.go:517-520` `recvmsg` error |
| `ancillary_data` | `wire_linux.go:521-527` `oobn != 0` (including the `parseRights` failure) |
| `truncated_frame` | `wire_linux.go:529-530` `MSG_TRUNC`/`MSG_CTRUNC` or `n > maxCloneStatusFrameSize` |
| `empty_packet` | `wire_linux.go:532-534` `n == 0` with `MSG_EOR` |
| `magic_or_version` | `wire_linux.go:538-539` bad magic, or a version outside {1-recognized, 3} |
| `gate_v1_frame` | `wire_linux.go:538-539` a v1 gate frame (§2.3); `Errno` for kind 8 |
| `peer_closed_without_frame` | `wire_linux.go:563-565`, `:581-583`, `:599-601` `n == 0` without `MSG_EOR` (one semantic producer at each arm's EOF site; wraps `io.EOF`) |
| `frame_header` | `wire.go:469-473` `validateCloneStatusFrame` version/type/size mismatch; the `decodeCloneCheckoutReceipt` refusals (round-1 review F1) |
| `image_inode_missing` | `wire.go:486-492` kind-20 frame with inode 0 |
| `guest_failure_malformed` | `wire.go:619-620` kind-24 frame whose size is not 20 |
| `guest_failure_code` | `wire.go:622-625` kind-24 frame with an invalid stage or errno |

Census (Change 7) exemptions with reason: `readCloneNativeExecResult`'s `wire.go`-adjacent
`errors.New("clone status channel carried data after native exec attempt")` (`wire_linux.go:626`) is
not an enumerated producer — it is a post-attempt follow-up anomaly, not the first read of an arm
(recorded as a residual); the `sendCloneStatusPacket` refusals are write-side; the non-clone
`receiveStatusPacket`/`receiveStatusPacketOrEOF` producers (`wire_linux.go:676-714`) are out of
scope by contract.

### 2.3 v1 gate-frame recognition (Q-C bounded, `wire_linux.go:538-539`)

A frame whose magic matches, whose u16 at 8 is `wireVersion` (1), and whose u16 at 10 is
`frameExecAttempt` (7) or `frameExecFailure` (8) is the gate's non-clone family on the clone
channel: the pre-clone guard's `FRAME_EXEC_FAILURE` (`gate.c:1015-1023`, sent at `gate.c:1215`) or
the non-clone attempt frame (`gate.c:1273-1279`). It renders `read_class=gate_v1_frame` with the
same text as today's mismatch; `Errno` is the u32 at offset 12 for kind 8 with `n >= 16` (16 is
`execFailureSize`), else 0. Every other version/magic mismatch stays `magic_or_version`: both
versions are never accepted as a valid attempt frame (`validateCloneStatusFrame` is untouched), so
the refusal behavior is unchanged.

## 3. Stage 1b — carry-through (Change 2)

1. `ReleaseSubStepError` gains `ReadClass string` (after `WaitClass`).
2. `ReleaseArmShape` gains `CloneRead bool`, true for exactly `bootstrap_attempt`,
   `checkout_receipt`, `clone_exec_proof` — the three arms whose read consumes the clone channel
   (the other arms that read the exec-status channel, `await_result`, stay false: the non-clone read
   is out of scope).
3. `NewReleaseSubStepError`: when the shape allows it, `errors.As(err, &readError)` sets
   `ReadClass`, and sets `Errno` from `readError.Errno` only when the caller's errno is zero
   (the decoded-guest branch keeps its precedence and is disjoint).
4. `LinuxOwnerHelperV2ReleaseErrorDetail` gains
   `ReadClass string \`json:"read_class,omitempty"\``; `String()` renders it after `wait_class` and
   before `guest_stage`; `Validate()` closes the vocabulary, refuses a read class on an arm that
   cannot read a clone frame (M4) and refuses one without a sub-step; `For` copies it.
5. `error_code`, every existing field, its exact-match tests and `ReleaseWaitErrorClass` are
   unchanged. No free text, no path, no raw error text enters the field.

## 4. Stage 1c — producer table (Change 3)

| Sentinel | Exact site | Wire bytes it carries |
| --- | --- | --- |
| `wait_failed` / `recv_failed` | App read `wire_linux.go:512-520` | none (no frame received) |
| `ancillary_data` | App read `:521-527` | a frame plus SCM_RIGHTS (or unparsable ancillary) |
| `truncated_frame` | App read `:529-530` | a frame larger than 64 KiB |
| `empty_packet` | App read `:532-534` | a zero-length seqpacket record |
| `magic_or_version` | App read `:538-539` | a frame outside the v1/v3 gate families |
| `gate_v1_frame` kind 8, errno=E | gate `exec_as_b` pre-clone guard `gate.c:1211-1217` → `send_exec_failure` `:1015-1023` | 16 B: `SYMLA001`, v1, kind 8, u32 errno=E (`close` EBADF; `drop_to_b` EPERM/EINVAL/other; `PR_SET_PDEATHSIG` EINVAL/EPERM; `check_exec_guard` ETIMEDOUT/ESRCH/ECANCELED/EINVAL/poll errno) |
| `gate_v1_frame` kind 7 | gate `exec_as_b` `:1273-1279` (non-clone attempt) | 28 B: v1 kind 7, dev/inode — reachable only when the gate parsed a non-clone release; a clone release encodes v3 |
| `peer_closed_without_frame` | gate `gate_run` silent exits (`:1367-1384`: adoption/release wait, `check_parent`, `read_release`) and any `send_packet` failure that also fails its failure frame | none |
| `frame_header` | App decode `wire.go:469-473` and `decodeCloneCheckoutReceipt` | a v3 frame the arm's own decoder refuses: wrong kind/size for this arm (21/23 to the attempt reader, 20 to the receipt reader), bad field framing, a non-positive generation, a truncated or trailing image identity, or a receipt `Validate()` refusal |
| `image_inode_missing` | App decode `wire.go:486-492` | v3 kind 20 with inode 0 |
| `guest_failure_malformed` / `guest_failure_code` | App decode `wire.go:677-687` | v3 kind 24 with size ≠ 20, or stage outside 1..18 / errno 0 or > u31 |
| decoded guest failure (`guest_stage`+`errno`) | gate `send_clone_bootstrap_failure` `gate.c:1036-1044` called at `:1197`, `:1203`, `:1216`, `:1224`, `:1238`, `:1256`, `:1268`; guest `cmd/symmetry-linux-clone-bootstrap/main.go:22-32` for every `RunBootstrap` failure | 20 B: v3 kind 24, u32 stage 1..18 (guest stages 1..7, gate-side guard and credential facts 8..18 per §8.1), u32 errno |

Reading (stage-1 hypothesis marker, never a fix basis): `gate_v1_frame` with kind 8 is the only
sentinel that yields a bounded errno without a `guest_stage`; every other `wait_class=other` row
refutes the v1-frame hypothesis and names its own producer.

## 5. Local probes (Change 6, lesson 74) and mutants

- `wire_linux_test.go`: one table test drives every producer against a real `AF_UNIX` seqpacket pair
  (a fork-child producer or in-process sends) — one positive row per sentinel (the code fires) and
  one negative row that pins the neighbouring producer (a v2 frame is `magic_or_version`, not
  `gate_v1_frame`; a v1 kind 8 is `gate_v1_frame`, not `magic_or_version`; a kind-23 frame read by
  the attempt arm is `frame_header`, not `image_inode_missing`; …). Golden 16-byte kind-8 frame
  bytes with errno 13 pin the recognition and the errno. Every row also asserts the rendered text is
  byte-identical to today's text and that a successful read carries no sentinel.
- `linux_owner_helper_v2_linux_test.go`: wire-rendering rows for the carry (`String()` literal
  `… read_class=gate_v1_frame errno=13`, M3/M5/M7), the validator's positive and negative rows
  (unknown code, wrong-shape arm M4, `read_class` without a sub-step), a redaction row (a payload
  carrying a path/secret renders no fragment), and one row per arm shape.
- `gate_census_linux_test.go`: a compiled-C fence probe (scenarios `exec-fence`,
  `exec-fence-expired`, `clone-fence-expired`, `clone-attempt-fence`) that pins
  `check_exec_guard` before and after every v1 `exec_as_b` attempt-frame send (delivered
  `gate.c:1347`, `:1367`) and `PR_SET_PDEATHSIG SIGKILL`; on the clone path it pins only the pre-send
  poll (residual, §10). The `clone-drop-capabilities` row also pins the drop's ordered credential
  steps (delivered `gate.c:1009-1013`), so deleting `setresgid` or `PR_SET_DUMPABLE` reddens it. M8 (`gate.c`: move
  the post-send exec guard before the frame send) reddens `exec-fence`.
- Mutants M1-M8 are applied to a copy of the delivered bytes and restored byte-identical (§ records).

## 6. Grep census (Change 7)

Every clone-read producer site (§2.2 table), every detail consumer (`helper_v2_stop.go:300-303`
writer, `helper_v2_protocol.go:640` validator, `:651-655` client) and every `wait_class`/`other`
renderer (`ReleaseWaitErrorClass`, the detail `String()`, the arm-table test literals) is listed in
the evidence record with converted/exempt and the reason.

## 7. Stage sequencing

1. Stage 1 (Changes 1-2 + probes + the census probe) → local probes → scratch run 1
   (`migration` only): the log must name the sentinel verbatim.
2. Stage 2 from that sentinel (§8), with stage-2 probes; the triple audit (Sol + DS +
   security-reviewer) before any run binds a `gate.c` byte; scratch run 2 (`migration`).
3. Final bytes, run 3 (`migration` + `stale_leaf`), sha256 equality table; records.

Run budget: 3 scratch runs. If two runs clear no named class, stop.

## 8. Stage 2 decision tree (Change 5; data-dependent, decided by run 1's verbatim sentinel)

- `read_class=gate_v1_frame` (errno E): the pre-clone guard failed. Two bounded parts, both inside
  the owned set:
  1. **Protocol mismatch at its root (Q-C)**: the gate's pre-clone failure must speak the family the
     clone read binds to. The gate is the side that owns the version contract for a clone release:
     when `command->clone_mode` is set, the failure is sent as the existing v3
     `FRAME_CLONE_BOOTSTRAP_FAILURE` (kind 24, stage 7 EXEC, errno E) instead of the v1
     `FRAME_EXEC_FAILURE`; the non-clone branch keeps the v1 frame byte-identical. No new field, no
     new kind, no new fd, nothing beyond the bounded numeric change Q-C pre-approves. The clone read
     then decodes it and the detail names `guest_stage=exec errno=E`.
  2. **The real root cause behind the mismatch**: the errno names the guard — `EPERM` (1) is
     `drop_to_b`/`PR_SET_PDEATHSIG` (or a stale errno from `check_zero_groups`, which sets none),
     `ETIMEDOUT` (110) is the lease deadline, `ESRCH` (3) the owner pidfd, `ECANCELED` (125) the
     owner pipe or pending status data, `EBADF` (9) the release close. The fix then addresses that
     guard's own cause (gate-side or caller-side inside the owned set). If the errno cannot
     discriminate the step and the cause needs bytes outside the owned set, the stage stops with
     `DECISION_NEEDED` and the verbatim sentinel; stage 1 merges.
- `read_class=magic_or_version`/`frame_header`/`image_inode_missing`/`truncated_frame`/
  `ancillary_data`/`empty_packet`/`wait_failed`/`recv_failed`: the producer is an App read or a
  frame-producer mismatch; fix the read or the named producer inside the owned set, keeping the
  frame contract.
- `read_class=guest_failure_code`/`guest_failure_malformed`: the guest/gate failure encoder is
  malformed; fix the encoder inside the owned set.
- `read_class=peer_closed_without_frame`: a silent exit; the owning path (gate `read_release`, the
  guest) emits the existing bounded failure frame on that path (numeric only). A named cause outside
  the owned set stops with `DECISION_NEEDED`.
- Q-E: the two tracked stops close only when run 1's sentinel names the same producer
  (`clone_integration_linux_test.go:240` = `:539`/`gate_v1_frame` or another read-class row;
  `launcher_integration_linux_test.go:439` = the `exec_errno` arm, i.e. the same `send_exec_failure`
  producer when run 1's errno matches 1); otherwise they stay residuals. The same-class question
  (`errno=1` leg vs the migration arm) is answered verbatim by run 1's `read_class`+errno.

### 8.1 Run 1's sentinel, and the stage-2 shape it selects

Run 1 (scratch run `45593856716`, head `6dd5867c`) returned, verbatim:

```
release_outcome_unknown; stage=clone_bootstrap arm=bootstrap_attempt wait_class=other read_class=gate_v1_frame errno=1
```

That is the first bullet of §8 above: the clone release's pre-clone guard failed and reported in the
v1 family the v3 read refuses. Stage 2 therefore implements both parts, and adds the smallest
bounded instrument that can name *which* step failed, because `errno=1` alone cannot:

1. **Family fix (Q-C)**: `exec_as_b`'s pre-clone chain reports through
   `send_preclone_failure(command, stage, errno)`; a clone release (`command->clone_mode`) sends the
   existing v3 `FRAME_CLONE_BOOTSTRAP_FAILURE` (kind 24, size 20) with the guard's own errno, and the
   non-clone path keeps `FRAME_EXEC_FAILURE` (kind 8, size 16) byte-identical. The four guards were
   split into separate `if` blocks so each reports its own errno: close / B credential drop /
   `PR_SET_PDEATHSIG` / exec fence. Stage codes 8-11 name the guard (`gate_close`, `gate_drop`,
   `gate_parent_death`, `gate_fence`).
2. **Step naming without touching the credential path**: `errno=1` reaches `exec_as_b` from
   `drop_to_b`'s steps, from `check_zero_groups` (which sets no errno, so a stale errno can surface
   as EPERM), and from `read_self_status`'s own `seen != 511` gate, which fabricates `EPERM`. No
   errno can separate those. `classify_drop_failure` therefore re-derives, **read-only**, the first
   unsatisfied credential fact in the drop's own order — groups, bounding set, namespace IDs,
   effective/permitted/inheritable capabilities, ambient, `NoNewPrivs`, then the status-shape check
   itself — and reports it as a closed numeric stage (12-18). It runs only on the failure path of a
   clone release, after `drop_to_b` has already failed; the drop's calls, order, decisions and
   fence are untouched, and the non-clone path does not call it at all.
3. **Boundary**: if the named fact is environmental (host or guest policy denying a capability,
   process state the gate cannot change), the fix is outside the owned set: stage 2 stops with the
   verbatim sentinel and `DECISION_NEEDED`, and stage 1 merges with that record. If it is
   gate-side, part 1 already carries the fix and run 2 shows the hold.

### 8.2 What the classifier can and cannot name

- The closed facts are exactly the drop's observable post-conditions in the drop's own order.
  `PR_SET_KEEPCAPS, 0` has no observable post-condition for the constant arguments the gate passes
  and cannot fail with EPERM; a failed `PR_SET_DUMPABLE, 0` (also unreachable for a constant valid
  argument) would be reported as `gate_drop_capabilities`. Neither can hide a failure: the frame,
  the failing call's errno and `_exit(1)` are unconditional, and the classifier only selects a
  label.
- `gate_drop_status` is the label for the `read_self_status` path, including its `seen != 511`
  gate, which fabricates `EPERM`; it is reachable only through the drop's own final verification.
- The census rows drive each fact through its own failing call (the drop dies at the step the row
  names), and the bounding-set row leaves the namespace IDs untouched, so two facts are unsatisfied
  and the row pins the order (bounding set before credentials).

## 9. Records (Change 8)

`docs/evidence/rf-acc-guest-native-exec-<date>.md` (per-run table, producer table, probe outputs,
mutant rows with restored digests, sha256 equality table of run 3's head vs the delivered files),
a bounded `docs/TODO.md` update (consume the parent's `migration`-arm residual, split/close the
three-product-test-stops rows this goal closes) and `docs/archive/CLOSED-TODO.md` entries only for
what closes. Records carry no host path with a user profile name, no `docs/TODO.md` sha256, and no
verbatim repo-file copies.

## 10. Out of scope (with reason)

- The non-clone native-status read (`wire_linux.go:676-714`, its conflated text) — out of root cause
  by contract; the four `stale_journal_*` owner-loss rows (`rf-acc-owner-loss-semantics`); the
  exit/unresolved contract; `daemon/e2e/**`; `daemon/internal/app/**`;
  `daemon/internal/execution/**`; `daemon/internal/platform/{helper_v2_server.go,helper_v2_stop.go,
  linux_owner_helper_v2_linux.go}` (unchanged: the carry is `helper_v2_protocol.go` only);
  `scripts/ci/**` and the real workflow (the scratch copy is branch-only and disposable);
  `control/**`; the `shared`/`synthetic_goal_cleanup` sets' green rows.
- `readCloneNativeExecResult`'s post-attempt follow-up text stays untyped (not an enumerated
  producer; recorded as a residual in the evidence record).
- Residual (round-1 review F2): the clone path has no post-send fence. `exec_clone_bootstrap` guards
  at delivered `gate.c:1220-1221` and then sends the kind-20 attempt frame (`:1235`) straight into
  `execveat` (`:1265`); only the v1 `exec_as_b` path re-checks after its send (`:1367-1372`). The
  census pins the clone path's pre-send poll only (`clone-attempt-fence`), and this increment does
  not touch the clone send.
