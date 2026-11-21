# rf-w4-20-linux-gate-protocol-split plan — move-only split of the linux-gate wire codec and identity verifier out of `launcher_linux.go`

Contract: `goal-rf-w4-20-linux-gate-protocol-split.md` (GDD, 2026-11-21). Base revision
`79548368` = `origin/main` = this worktree's HEAD. The subject file
`daemon/native/linux-gate/protocol/launcher_linux.go` (3,303 lines) held 230 top-level
declarations fusing four concerns; every one of them is relocated, none is edited, split,
renamed or merged. `launcher_linux.go` is deleted; its content is compiled from five files.

## Deliverable shape

| file | build constraint | lines | units | composition |
|---|---|---:|---:|---|
| `wire.go` | none | 724 | 112 | 58 const, 12 type, 3 method, 39 func |
| `wire_linux.go` | `linux && amd64` | 723 | 29 | 2 type, 3 method, 24 func |
| `owner_config_linux.go` | `linux && amd64` | 138 | 8 | 4 const, 1 method, 3 func |
| `proc_identity_linux.go` | `linux && amd64` | 530 | 29 | 5 type, 4 method, 20 func |
| `gate_lifecycle_linux.go` | `linux && amd64` | 1,251 | 52 | 11 const, 4 var, 5 type, 24 method, 8 func |
| total | | 3,366 | 230 | |

`launcher_linux.go` (3,303 lines) is gone; the largest of the five split targets is 1,251
lines (limit 1,500). The limit is scoped to the five split targets: the pre-existing
`launcher_integration_linux_test.go` (1,573 lines, identical at base and HEAD) is untouched by
this slice, as change step 4 requires. 3,098 of the original lines are the moved unit bodies;
the remaining 205 are the deleted block delimiters, the five file headers/import blocks and
the blank separators.

Concern boundaries:

- **`wire.go` — the portable frame codec.** Launch/ready/gate/exec/clone-status frame
  constants, sizes and limits; the wire payload types (`OwnerConfig`, `ProcessIdentity`,
  `LauncherIdentity`, `LauncherReady`, `LaunchRequest`, `LaunchResult`, `NativeCommand`, the
  two stage enums and their error carriers); every `Encode*`/`Decode*`/validator that only
  transforms bytes ↔ structs with no fd and no syscall — including the guarded RELEASE v2/v3
  encoder `encodeGuardedRelease`, which the fix stage moved here from the linux half per
  amendment 1. No build constraint: it compiles and is tested on every GOOS.
- **`wire_linux.go` — the fd-bearing half of the same concern.** The `EncodeRelease`/
  `DecodeRelease` wrappers and the guarded RELEASE v2/v3 decode path with its payload
  validators, the seqpacket/SCM_RIGHTS send/receive paths, the
  launcher-result/ready readers, the clone-status channel readers, `parseRights`,
  `closeFDs` and the shared poll primitive (`waitForStatus`). These cannot be portable: they
  call `unix.Recvmsg`/`SendmsgN`/`Poll`/`Fstat` or depend on `OwnerConfig.Validate`, which
  the contract assigns to `owner_config_linux.go`.
- **`owner_config_linux.go` — operator-owned config provisioning.** `OwnerConfigMaxBytes`,
  `ownerConfigPath`, the two internal limits, `ReadInstalledOwnerConfig` (the `/etc` +
  `/etc/symmetry` O_PATH walk), `parseOwnerConfig`, `OwnerConfig.Validate`,
  `validateOwnerStateDir`.
- **`proc_identity_linux.go` — /proc identity and image pinning.** The typed
  `InaccessibleUserNamespaceError` and its sentinel, `/proc/<pid>/status|stat|ns/user|
  fdinfo` parsers, `CaptureLauncherIdentity`, `probePIDFDSupport`, the trusted-parent walk
  and inode pinning (`pinNativeImage`, `openTrustedImageParent`,
  `verifyTrustedImageDirectory`, `approvedNativeImageIdentity`, `pinnedNativeImage` verify/
  close), `verifyExecAttemptImageIdentity`, the installed launcher/bootstrap verifiers and
  the cgroup-leaf/US-only-map verifiers.
- **`gate_lifecycle_linux.go` — the stateful `PreparedGate` lifecycle.** `Prepare`, the
  `PreparedGate` type and all its methods (`Release`, `Abort`, `WaitLeader`, `Close`,
  `VerifyLeafMembership`, identity/readback checks), the release guards and clone checkout
  receipt validation, the launcher outcome/exit-proof error types and `stopLauncher`/
  `waitProcessFD`/`closeFiles`/`errOr`. It also carries the package doc comment.

## Decisions taken where the contract left latitude (no silent guesses)

1. **Two wire files instead of one.** Change step 1 allows "`wire.go` or `wire_linux.go`";
   step 2 requires the pure codec in a file *without* the linux constraint. Both are
   satisfied by the portable half in `wire.go` and the fd/SCM half in `wire_linux.go` — the
   concern count stays four, the file count is five.
2. **The gate-map codec stays in the linux half.** `DecodeGateMap` calls `OwnerConfig.Validate`,
   and the contract's step 1 assigns `owner_config_linux.go` = "OwnerConfig load/validate".
   Keeping the frame pair together (`EncodeGateMap` next to it) is more readable than
   splitting one frame across two files, so the gate-map frame is exercised by
   `wire_linux_test.go`. The alternative (Validate in `wire.go`) would contradict step 1.
3. **Package doc comment on `gate_lifecycle_linux.go`.** The package doc describes
   `Prepare`/`Release`, so it travels with them; `wire.go` carries no package comment (no
   second doc comment in the package). `go doc` therefore reports the doc on every target
   that builds the lifecycle files.
4. **`const`/`var` blocks are partitioned by concern.** One block cannot serve two files
   whose concerns differ (`launcherPath`/timeouts vs the wire sizes). Each spec moves
   verbatim inside a regenerated `kind (...)` block; iota/implicit-value blocks are never
   split (the splitter refuses that) — the `LauncherReadyFailure*`, `LaunchStatus*`,
   `CloneFailure*` and `gate*` blocks each moved whole.
5. **A type's methods stay with the type.** `PreparedGate.verifyIdentity`/`verifyNativeImage`
   are methods of the state machine and stay in the lifecycle file even though they call the
   /proc verifiers; the free `/proc` and pinning functions are in `proc_identity_linux.go`.
6. **No existing test is moved.** None of the 11 launcher-integration tests exercises only the
   portable codec: the codec-only release test
   (`TestLinuxOwnerReleaseV2RejectsLegacyAndWrongDescriptorCount`) needs real FIFO pipes and
   `unix`, so it stays in `launcher_integration_linux_test.go` unchanged.
7. **Two new test files.** `wire_test.go` (no constraint, runs on Windows) covers the
   portable frames and decoders and, since the fix stage, the portable guarded-RELEASE
   encoder; `wire_linux_test.go` (`linux && amd64`) covers the descriptor-validating RELEASE
   paths and the bootstrap-request decoder, which have no portable entry point (decision 2).
8. **Exported API unchanged.** Every exported declaration (`Encode*`, `Decode*`, `Validate*`,
   `Prepare`, `PreparedGate` and its methods, `OwnerConfig`, `NativeCommand`, `LaunchRequest`,
   `LaunchResult`, `LauncherIdentity`, `LauncherReady`, `CaptureLauncherIdentity`, the error
   types, `LaunchRequestSize`, `OwnerConfigMaxBytes`, …) keeps its exact name, signature and
   receiver; only its file changed. `go doc -all` output is compared before/after.

### Fix stage (2026-11-21, review r1)

- **R01 — the pure RELEASE encoder joins the untagged file.** `encodeGuardedRelease` (pure
  bytes: no fd, no syscall, no Linux-only symbol) moved verbatim from `wire_linux.go` to
  `wire.go`; `EncodeRelease` stays the `linux && amd64` wrapper that keeps the descriptor
  validation and `monotonicDeadlineNS` read in the same order. `wire.go` is now 724 lines /
  112 units, `wire_linux.go` 723 lines / 29 units, total 3,366 (the move adds one blank
  separator); the moved text is byte-verified against the base function. `wire_test.go` pins
  the portable v2 frame (`TestWireGuardedReleaseV2Encode`, also green against the base
  revision's encoders; mutant M16 covers its zero-deadline refusal) and
  `FuzzEncodeGuardedRelease` fuzzes the pure encoder on the host.
- **R02 — both RELEASE-family size bounds now have killing cases.** `DecodeLauncherReady`
  refuses a frame one full `maxLauncherReadySize` past its bound and `decodeReleaseFrame`
  refuses one a full `maxReleaseSize` past its bound (exercised through both RELEASE entry
  points); mutants M13-M15 die on exactly those cases.

## Exact semantics the contract demands (and how they are preserved)

- **Move-only.** Each unit is extracted as a byte range (doc comment cluster through the
  declaration end, plus a same-line trailing comment) from the base file and re-emitted
  verbatim; the splitter copies the original source lines. Verification re-parses the base
  file and the five targets and compares per-unit text keyed by `kind|receiver|name(s)`:
  230/230 units present, 0 missing, 0 extra, 221 byte-identical, 9 differing **only in
  gofmt's block-local `=` alignment** (9 `const` specs whose new block has a shorter longest
  name; space runs only, verified by whitespace-normalized equality *and* token-stream
  equality, both 100%). The 9 are listed in the answer record with their verbatim before/after
  text. No unit's token stream, comment text or literal changed.
- **Wire frame bytes.** A scratch dump test (`.agents`-external, in the run scratch dir)
  encodes every frame type from fixed fixtures with the base revision and with the split
  revision: 100/100 lines identical — 16 frame encodes (the 15 kinds, with RELEASE v2 and a
  v3 frame carrying a clone grant encoded through `encodeGuardedRelease` with an explicit
  monotonic deadline so the bytes cannot depend on host clock state), 7 successful decodes
  (the exec-attempt, ack and clone-request frames have no successful decode line), 72
  refusal lines (70 uniform mutations across 12 frames plus a labelled v2-packet and
  stale-token probe) and 5 encoder refusals.
- **Refusal/error strings.** Byte-identical by the same 100-line comparison; the new refusal
  tests additionally pin them (exact `err.Error()` equality) for truncated, oversized,
  unknown-tag and frame-specific mutations.
- **Syscall order and fd handling.** Unchanged: every function body is a verbatim move, the
  declaration set is identical, so the call graph (who calls `unix.Recvmsg`, with which
  flags, in which order) is identical. No `unix` call, flag literal, `closeFDs` path or fd
  ownership transfer was added, removed or reordered.
- **No new `//nolint`, no test renamed or weakened, no behaviour change.**

## Evidence plan (exact commands; all foreground, serially)

1. `gofmt -l ./native/linux-gate/protocol/` (from `daemon/`) — empty.
2. `go vet ./...` and `GOOS=linux go vet ./native/linux-gate/protocol/` (the second
   type-checks the linux files and both new test files from this Windows host).
3. `GOOS=windows go build ./...`, `GOOS=linux go build ./...`,
   `GOOS=linux GOARCH=arm64 go build ./...`.
4. Windows-local `go test -count=1 ./native/linux-gate/protocol/...` — proves the codec tests
   run off-Linux (the package compiles from `wire.go` + `wire_test.go` there).
5. WSL `wsl -d Ubuntu-24.04 -u accept` in `daemon/`:
   `go test -count=1 -race ./native/linux-gate/... ./internal/platform/...` with the SKIP
   list recorded.
6. `go test -run=^$ -fuzz='^FuzzDecode<one>$' -fuzztime=30s` per fuzz target, on the
   platform where the target compiles. The anchor is required: an unanchored name also
   matches its longer sibling (`FuzzDecodeLauncherReady` selects
   `FuzzDecodeLauncherReadyFailure` too) and the run aborts.
7. Declaration-set comparison with `--moves` per-unit table and the 9 whitespace deltas
   (scratch tool `psplit`, `go/ast` byte-range extraction, stdlib only); plus
   `git diff --color-moved=dimmed-zebra` over the base file against a concatenation of the
   five targets.
8. Frame-bytes golden comparison base vs HEAD (the 100-line dump).
9. Control-flow audit: the token streams of all 230 units are identical, and the 52 lifecycle
   units (including `Prepare`, `Release`, `WaitLeader`, `Close`) carry the same statement
   sequence; `PreparedGate`'s call sites and lock order are unchanged because no body changed.
10. Red mutants: for each new refusal test function, the corresponding guard in a
    byte-identical scratch copy of the tree is flipped and the test is shown failing (exit
    code 1 + the failure message), then the copy is restored and hash-verified.
11. `python3 scripts/ci/check-citation-existence.py` over the changed documents; no host path
    containing a user profile name and no backslash-letter-u escape sequence in any written
    file (the conductor's DLP filter rule).
12. Contract arch-decision-2 R9 record: no `SOURCE_PATHS` entry of
    `scripts/ci/omo-control-native-evidence.py` is touched — the 28 listed paths have no
    intersection with the five split targets, the two new test files or this plan (checked
    against the branch diff at the fix revision).

## Design trade-offs

- **Scratch generator over hand edits.** A 3.3 kLOC move by hand cannot be proved move-only.
  The splitter (`psplit`, outside the repository, stdlib only) copies original line ranges;
  its inputs are the ordered rule list in the plan (below) and the base file. Cost: the tool
  is not committed; the durable outputs are the per-unit move table, the deltas file and this
  plan's rule list, and the split re-runs from the same two commands after a rebase.
- **Whitespace-only reflow accepted in 9 specs** instead of forcing byte identity by putting
  a concern's constants in the wrong file or keeping a fused block. The alternative would
  either duplicate constants or move lifecycle paths into the codec module; both are worse
  than a gofmt alignment delta that is provably token-free.
- **`wire_linux_test.go` as a second new test file** rather than forcing the RELEASE frame
  into a portable file whose fixtures would then need FIFO pipes and `unix` (the property the
  split exists to remove).
- **Classification is rule-driven and re-runnable**, not a hand-maintained list: the ordered
  rules and the resolved 230-row assignment table are recorded, so a rebase or a review can
  re-derive every placement.

### Classification rules (ordered; first match wins; every unit matched exactly once)

Targets: `W`=`wire.go`, `WL`=`wire_linux.go`, `OC`=`owner_config_linux.go`,
`PI`=`proc_identity_linux.go`, `GL`=`gate_lifecycle_linux.go`.

| rule (regex on `kind\|receiver\|name`) | target |
|---|---|
| `^type\|OwnerConfig$` | W |
| `^const\|(launchMagic\|wireVersion)$` | W |
| `^const\|frame[A-Z]` | W |
| `^const\|(releaseWireVersion\|releaseCloneWireVersion\|cloneStatusWireVersion)$` | W |
| `^const\|(launchRequestSize\|…\|maxCommandVector\|LaunchRequestSize)$` (the 19 wire sizes/bounds) | W |
| `^const\|LauncherReadyFailure[A-Z]`, `^const\|LaunchStatus[A-Z]`, `^const\|CloneFailure[A-Z]` | W |
| `^type\|(ProcessIdentity\|LauncherIdentity\|LauncherReady\|LauncherReadyFailureStage\|LauncherReadyFailureError\|GateProductionAuth\|LaunchRequest\|LaunchResult\|NativeCommand\|CloneBootstrapFailureStage\|CloneBootstrapFailureError)$` | W |
| `^method\|(LauncherReadyFailureStage\|LauncherReadyFailureError\|CloneBootstrapFailureError)\|` | W |
| the 38 portable frame functions (`encodeLaunchRequest`, `EncodeLauncherReady`, …, `DecodeCloneBootstrapFailure`) | W |
| `^type\|(ReleaseGuard\|execStatus)$`, `^method\|(NativeCommand\|GateProductionAuth)\|` | WL |
| the 24 fd/SCM functions (`sendPacket`, `receiveLauncherResult`, `parseRights`, `waitForStatus`, `EncodeRelease`, `DecodeRelease`, `decodeReleaseFrame`, `EncodeGateMap`, `DecodeGateMap`, `receiveExecStatus`, `closeFDs`, …; the fix stage moved the pure `encodeGuardedRelease` from this set to `W`) | WL |
| `^const\|(OwnerConfigMaxBytes\|maxOwnerConfigBytes\|maxOwnerStateDirBytes\|ownerConfigPath)$`, `^method\|OwnerConfig\|`, `^func\|\|(ReadInstalledOwnerConfig\|parseOwnerConfig\|validateOwnerStateDir)$` | OC |
| `^type\|(InaccessibleUserNamespaceError\|nativeImageIdentity\|pinnedNativeImage\|procStatus\|namespaceStat)$`, `^method\|(InaccessibleUserNamespaceError\|pinnedNativeImage)\|`, the 20 `/proc`+pinning functions | PI |
| `^const\|(launcherPath\|cloneBootstrapPath\|bootstrapTimeout\|execAckTimeout\|cloneBootstrapTimeout)$`, `^var\|(ErrOutcomeUnknown\|ErrReleaseUsed\|ErrGateNeverProduced\|ErrInaccessibleUserNamespace)$`, `^const\|gate[A-Z]`, `^type\|(GateNeverProducedError\|LauncherOutcomeError\|PreparedGate\|gateState\|LauncherStartHook)$`, `^method\|(GateNeverProducedError\|LauncherOutcomeError\|PreparedGate)\|`, `^func\|\|(launcherExitProof\|Prepare\|validateCloneCheckoutReceipt\|validateReleaseGuardLive\|waitProcessFD\|stopLauncher\|closeFiles\|errOr)$` | GL |

Block exception (same mechanism as decision 4): the `var ( … )` block holding
`ErrOutcomeUnknown`, `ErrReleaseUsed` and `ErrInaccessibleUserNamespace`
(`gate_lifecycle_linux.go:34-38`) moved whole because the splitter never splits a block, so
`ErrInaccessibleUserNamespace` ships in `GL` although the typed error it belongs to
(`InaccessibleUserNamespaceError`) is in `PI`. `ErrGateNeverProduced` is the fourth `GL`
`var` unit; `PI` carries no `var`.

## Out of scope

- `clone_cleanup_linux.go`, `clone_cleanup_linux_test.go`, `gate_production_linux.go` and
  `cmd/symmetry-linux-launcher` (W6-06) — untouched.
- Any behaviour change: no error string, frame byte, syscall order, fd ownership, validation
  rule, timeout, test name or test assertion is modified.
- Any test move: none of the existing launcher-integration tests qualifies (decision 6).
- The arm64 `go vet` gap recorded in `docs/TODO.md` ("Build-tag parity (arm64 oracle)") is in
  `internal/state`, not this package; this slice only adds the arm64 `go build` target.

## Measured outcome (delivered revision)

Full command logs, exit codes and the scratch-tool inputs live in the stage's `answer.md`; the
run's scratch directory holds the whole artifacts. Summary of what the delivered tree measured:

| check | result |
|---|---|
| file sizes | `wire.go` 724, `wire_linux.go` 723, `owner_config_linux.go` 138, `proc_identity_linux.go` 530, `gate_lifecycle_linux.go` 1,251; `launcher_linux.go` deleted |
| declaration set | 230/230 units, 0 missing, 0 extra, 221 byte-identical, 9 whitespace-only (gofmt `=` alignment in the two `const` blocks whose longest name changed), 0 token differences, import set 21 = 21 |
| `go doc -all` | byte-identical base vs HEAD (508 lines) |
| frame/refusal golden | base vs HEAD dump identical (fix-stage re-run), 100/100 lines: 16 frame encodes (all 15 kinds incl. RELEASE v2 and a v3 clone frame), 7 decoded forms, 72 decode-refusal lines (70 uniform mutations + 2 labelled probes), 5 encoder-refusal strings |
| builds | `GOOS=linux`, `GOOS=windows`, `GOOS=linux GOARCH=arm64`: exit 0 each |
| vet | `go vet ./...` (host) and `GOOS=linux go vet ./native/linux-gate/protocol/`: exit 0 |
| gofmt | `gofmt -l ./native/linux-gate/protocol/`: empty |
| host tests | `go test -count=1 -v ./native/linux-gate/protocol/...`: ok 0.121 s; 19 top-level PASS (7 Test + 12 fuzz seed), 114 subtest PASS, 0 FAIL — the portable codec and the portable RELEASE encoder run off-Linux |
| WSL tests | `go test -count=1 -race -v ./native/linux-gate/...`: ok 3.680 s; 236 RUN / 225 `--- PASS` (36 top-level, 171 subtest) / 11 SKIP / 0 FAIL at the fix revision; the 11 skips are the pre-existing integration scratch helpers declared in files this slice did not touch. The full pair run `... ./internal/platform/...` additionally records `internal/platform` failing its 4 pre-existing loopback-TCP tests and its 600 s package budget, identically at the base revision (impl/apply stage artifacts) |
| fuzzing | 15 fuzz targets, anchored (`-fuzz='^Name$'`), `-fuzztime=30s` each: 12 portable on the host (2.24M-4.89M execs each) and 3 linux-only in WSL (7,981-66,959 execs each); 0 failures (fix-stage run). The impl stage's unanchored host sweep aborted on two name matches (`portable_fuzz_failures=2`, two prefixes selecting two targets each); this anchored rerun supersedes it |
| red mutants | 16/16 caught (control-green, mutant-red on the intended test function, file restored byte-identically): the 12 mutants of the applied revision (M1-M12, covering 8 refusal test functions, 5 portable + 3 linux) re-run at the fix revision, plus M13 (`maxLauncherReadySize` bound, portable), M14 and M15 (`maxReleaseSize` bound through `TestWireLinuxReleaseRefusals` and `TestWireLinuxCloneBootstrapRefusals`, WSL) and M16 (zero-deadline encoder guard, portable) |
| citation gate | `python3 scripts/ci/check-citation-existence.py --base 79548368 --head HEAD`: exit 0, 1 changed document passes the path and symbol check (453 path citations); the audited corpus is `docs/runbooks/**`, `docs/design/quality.md` and `docs/archive/CLOSED-TODO.md`, so this plan is outside its scope |
