# rf-claude-schema-argv — evidence record

Goal `rf-claude-schema-argv` (contract: the goal file under the GDD contracts root; ruling
2026-11-26, option (a); both consult reports at the GDD state root
`findings/argv-consult-{sol,ds}.md`). Branch `refactor/rf-claude-schema-argv`, base `8453da12`
(PR #293 merge, the ruling's dispatch gate). Plan:
`docs/plans/2026-11-26-rf-claude-schema-argv-plan.md`. All paths relative to the repository;
all commands ran in WSL `Ubuntu-24.04`, kernel `6.18.58.1-microsoft-standard-WSL2`, user
`accept`, `go1.27.0 linux/amd64`, `cc (Ubuntu 13.3.0-6ubuntu2~24.04.1)`, working directory
`daemon/`.

## 1. Delivered files and digests

| file | sha256 at the delivered bytes |
| --- | --- |
| `daemon/native/linux-gate/protocol/wire.go` | `0bb6b92c59ebc8b5e80b1c71add632dd7bf730cf71646ebf0fae14888bc54504` |
| `daemon/native/linux-gate/c-gate/gate.c` | `e386c7f73e6fa955bcd569490b9d7e219ae266ff02b09c45e2e95a7a8dda2f2c` |
| `daemon/native/linux-gate/protocol/wire_linux_test.go` | `efcbaa5d2ffd217b8b98f946ca89e979c542985ca9f8bb5e2504089b6336f9ce` |
| `daemon/native/linux-gate/protocol/gate_census_linux_test.go` | `3340995ba4dac6f3ae9cce67f27fb9ec748bb6806b7d0082a4c826607a29908e` |
| `daemon/internal/harness/claude/adapter_cmdline_linux_test.go` | `4e2891942dcb7ddec6611607e3d19ad155702f268f1204b7e23665210acb057a` |
| `docs/plans/2026-11-26-rf-claude-schema-argv-plan.md` | `c6f1bcb2ef8090696c9e27d01a8dffe9022c09e621b5a7dbdb8e97967be372b3` (record revision; not run-bound) |

`docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and this record are record-only and deliberately
not hashed. The delivered inventory against the base is 6 modified (`wire.go`, `gate.c`,
`wire_linux_test.go`, `gate_census_linux_test.go`, `docs/TODO.md`,
`docs/archive/CLOSED-TODO.md`) + 3 added (`adapter_cmdline_linux_test.go`, the plan, this
record); the range diff carries no other path.

The apply-stage simplification (2026-11-26) changed `gate_census_linux_test.go`,
`adapter_cmdline_linux_test.go` and one plan anchor row; the fix round (2026-11-26, review
R1/R2/R6/R7) changed `gate_census_linux_test.go`, `adapter_cmdline_linux_test.go` and the plan
again: the three digests above are re-derived at those fix-round bytes, and every §5 row was
re-run there (same counts; refreshed durations).

## 2. The change

Two lines, one change: `wire.go:53` `maxCommandString = 16 << 10` → `64 << 10`; `gate.c:91`
`MAX_COMMAND_STRING = 16 * 1024` → `64 * 1024`. Nothing else moved: `maxReleaseSize`
(`wire.go:52`, `gate.c:90`), `maxCommandVector` (`wire.go:54`, `gate.c:92`), the frame magic,
versions and layout, and the unrelated `char data[16384]` (`gate.c:946`). No third definition
site exists (`grep -rn "maxCommandString\|MAX_COMMAND_STRING" daemon` finds the two definitions
plus uses only; the helper's re-check at `daemon/internal/platform/helper_v2_server.go:1075`
calls `linuxgate.NativeCommand.Validate()` and follows the Go constant). The RELEASE v2/v3
golden-byte test (`TestWireLinuxReleaseGoldenBytes`) passes unchanged, which is the byte-level
proof of the "no protocol v1 change, no RELEASE version bump" invariant. No comments were added
to the two production constant lines.

## 3. Base red and baseline at the dispatch head

Base gates before any edit: `go test -race -count=1 ./native/linux-gate/protocol/` → `ok`
6.501s, exit 0; `go test -race -count=1 ./internal/harness/claude/` → `ok` 6.937s, exit 0.

The refusal this goal removes, run `45671784251` on the containment leg:

```
start native owner witness transport: start "/usr/local/bin/claude": validate native command
and three Runner pipes before cgroup reservation: Linux owner native argv contains an empty,
oversized, or NUL value
```

## 4. New rows and red-first observations

| Row | Test | At base (`8453da12` constants) |
| --- | --- | --- |
| G1 | `TestClaudeCandidateLinuxCommandArgvFitsOwnerWireBound` | FAIL, exit 1: `adapter_cmdline_linux_test.go:34: the fixed Claude argv is refused by the Linux owner native command contract: Linux owner native argv contains an empty, oversized, or NUL value` — the run's own refusal, reproduced by the new twin |
| G2 | `TestWireLinuxCommandStringBoundary` | PASS: the pair is cap-relative (`maxCommandString`); its red proof is M1 |
| G3 | `TestGateCommandStringBound` | PASS at base (element sizes follow the Go cap, 16 KiB both sides; the probe accepts 16384 and refuses 16385 with `EPROTO`); its red proof is M2 |
| G4 | `TestGateFDCensusAllowsStdioAndRejectsUnexpectedDescriptors` | FAIL at base constants (the probe's literal 64 KiB assert does not compile): `static assertion failed: "the gate C cap must be the ruled 64 KiB bound"`; green at the delivered bytes; red under either single-sided drift (M1/M2) |

The delivered-form behaviour at the new cap (all green, exit 0):

- G1 PASS, log line verbatim: `Claude candidate --json-schema element = 23881 bytes, guarded
  RELEASE packet = 24392 bytes` — the real fixed argv (`[program, …]`, the production
  `cmd.Args` shape) passes `Validate()` and `EncodeRelease` (R1's argv[0] adds 75 bytes; the
  packet carries the fixture's `t.TempDir()` workspace, so its size moves by a byte or two
  across runs — observed 24390–24392).
- G2 PASS: `Validate()` and `DecodeRelease` accept a `maxCommandString`-byte argv element and
  refuse `maxCommandString+1` (`argv contains an empty, oversized, or NUL value`;
  `length-prefixed command string is oversized or truncated`).
- G3 PASS, both subtests: the probe calls the gate's real `read_release` over a real
  `SOCK_SEQPACKET` release channel with a real four-FD `SCM_RIGHTS` transfer and a real parent
  pidfd; `at-cap-accepted` returns 0 only after asserting the parsed shape
  (`argc == 1`, 65536-byte `argv[0]`, `/bin/true`), `cap-plus-one-refused` returns 0 only on
  `errno == EPROTO` for 65537.
- G4 PASS: the census probe compiles gate.c and its `_Static_assert`s hold
  (`MAX_COMMAND_STRING == 64 * 1024`; `MAX_COMMAND_STRING == CENSUS_GO_MAX_COMMAND_STRING`
  where the second operand is injected from Go's `maxCommandString` by the Go test).

## 5. Delivered-state gates

| Gate | Command (WSL, `daemon/`) | Exit | Result |
| --- | --- | --- | --- |
| Format | `gofmt -l .` | 0 | empty |
| Vet, linux | `go vet ./native/linux-gate/protocol/ ./internal/harness/claude/` | 0 | no output |
| Vet, windows | `GOOS=windows GOARCH=amd64 go vet ./native/linux-gate/protocol/ ./internal/harness/claude/` | 0 | no output |
| Vet, module | `GOOS=linux GOARCH=amd64 go vet ./...` | 0 | no output |
| Gate C, production flags | `cc -O2 -static -fno-pie -no-pie -Wl,-z,relro,-z,now -Wl,-z,noexecstack -o <TMP>/rf-claude-schema-argv-apply-gate-cc native/linux-gate/c-gate/gate.c` (the installer's line, `scripts/install-linux-owner.sh:29`, retargeted to a scratch output) | 0 | no output |
| Census/bound probe compile | `compileGateProbe` (`-std=c11 -O2`, unchanged) inside G3/G4 | 0 | both probes compile the current gate.c |
| Protocol race | `go test -race -count=1 -timeout 25m -v ./native/linux-gate/protocol/` | 0 | `ok` 6.704s, 47 top-level PASS / 11 SKIP / 0 FAIL (settled bytes; subtests of both new rows PASS) |
| Claude race | `go test -race -count=1 -timeout 25m -v ./internal/harness/claude/` | 0 | `ok` 8.703s, 74 top-level PASS / 5 SKIP / 0 FAIL |
| Regression rows | `go test -race -count=1 -v -run "TestWireLinuxReleaseRefusals\|TestWireLinuxCloneBootstrapRefusals" ./native/linux-gate/protocol/` | 0 | both PASS; the dispatch-head `:233,260,372` oversized-frame and total-bound refusals are untouched (`:233` stays; `:260`/`:372` are `:307`/`:419` after the 47 inserted lines) |
| Twin runtime | `go test -race -count=1 -v -run TestClaudeCandidateLinuxCommandArgvFitsOwnerWireBound ./internal/harness/claude/` | 0 | PASS 0.11s (schema 23881 / packet ≈24392 — observed 24390–24392; the fixture temp workspace path sizes it; after R1's production-shape `Args`) |

The 11 protocol skips are the pre-existing installed-owner/credential-gated tests
(`TestInstalledLinuxOwner*Scratch`, `TestLinuxOwner*Worker`/`Helper`,
`TestLinuxOwnerNativeOwnerDeath*`, `TestLinuxOwnerCloneNativeGitHelper`); the 5 claude skips are
the pre-existing live-blob tests (`TestClaudeCandidateLive*`,
`TestNativeClaudeCandidateStructuredOutput`). Both sets skip at base and after; they are
missing evidence, not passes.

Closeout (contract acceptance, census-probe compile): the production-flag gate.c compile row
meets the clause; the census probe keeps its pre-existing `-std=c11 -O2` compile.

## 6. Mutants

M1 — `wire.go` reverted to `16 << 10`, `gate.c` left at `64 * 1024`:

| Row | Result |
| --- | --- |
| G1 | FAIL: `the fixed Claude argv is refused by the Linux owner native command contract: Linux owner native argv contains an empty, oversized, or NUL value` |
| G3 | FAIL on `cap-plus-one-refused`: `read_release accepted an argv element of 16385 bytes above the cap` (C, still 64 KiB, accepts what the Go cap refuses) |
| G4 | FAIL: `gate-census-probe.c:136:1: error: static assertion failed: "the gate C cap must track Go protocol.maxCommandString"` |
| G2 | PASS (cap-relative by design) |

M2 — `gate.c` reverted to `16 * 1024`, `wire.go` left at `64 << 10`:

| Row | Result |
| --- | --- |
| G3 | FAIL on `at-cap-accepted`: `read_release refused an argv element of 65536 bytes (the Go cap)` — this is the mixed-vintage pin: a 16 KiB-vintage gate refuses the new frame with `EPROTO` at `read_span` and cannot misparse it (position-bounded parse, unchanged layout) |
| G4 | FAIL with both asserts: `"the gate C cap must be the ruled 64 KiB bound"` and `"... must track Go protocol.maxCommandString"` |
| G1 | PASS (Go side untouched) |

Restoration: `wire.go` back to `0bb6b92c…` and `gate.c` back to `e386c7f7…`, both byte-identical
to the digests in §1, and G2-G4 plus both packages green again (`ok` 5.571s for the three
probe rows). A coordinated reversion of *both* caps to another shared value is red at G4's
literal assert and at G1's 23881-byte payload.

## 7. Records and closure

- Plan: `docs/plans/2026-11-26-rf-claude-schema-argv-plan.md` (decision record D1-D5, evidence
  plan, out-of-scope list).
- `docs/TODO.md`: the rf-acc-containment-goal-admission residual's claude clause is closed and
  moved to `docs/archive/CLOSED-TODO.md` with the base red above and this record's rows; what
  stays in the residual is the sets' remaining stops beyond both walls.
- Binding run: this goal's acceptance names the conductor's Bind scratch run of the
  `claude_containment` case set on the PR head (no "oversized" refusal; the claude transport
  reaches `system/init` or its first native stage). Its row:

| Bind run | Head | Case sets | Result |
| --- | --- | --- | --- |
| `45710455914` | `234e2fb6db48bfd3b27954cc5ab00b5e3d40c72a` (= `72f8d01e` + the scratch workflow `b687669b0826da767502464978c7af596e549e28b45a7cc54b2abc064e508c50` and shim `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`) | `claude_containment` | exited 1: 0 `oversized` occurrences and no "did not reach the credential-free native Open boundary" — the claude transport started and reached its first native stage (`native_command_verified=true`; the live B claude process with pidfd/leaf/user-namespace/cmdline verified); the remaining stop is the owner witness's durable-stage summary check (`positive owner witness lacks a durable reserve/bind/commit/lease/checkout/RELEASE stage`; the single unset flag is `initial_lease_acked`); evidence artifact `8915143ae6c6ecd05b653dd0ce90e2500b567597a5c2af65a00bb86717885ba7` |

Bind-run input equality — the scratch head carries the PR head's run-input bytes (`git cat-file`
sha256 at both revisions; `git diff --name-status 72f8d01e 234e2fb6` is exactly the two
scratch-only files below):

| File | Run-head bytes (`234e2fb6`) | PR-head bytes (`72f8d01e`) | Equal? |
| --- | --- | --- | --- |
| `daemon/native/linux-gate/protocol/wire.go` | `0bb6b92c59ebc8b5e80b1c71add632dd7bf730cf71646ebf0fae14888bc54504` | `0bb6b92c59ebc8b5e80b1c71add632dd7bf730cf71646ebf0fae14888bc54504` | yes |
| `daemon/native/linux-gate/c-gate/gate.c` | `e386c7f73e6fa955bcd569490b9d7e219ae266ff02b09c45e2e95a7a8dda2f2c` | `e386c7f73e6fa955bcd569490b9d7e219ae266ff02b09c45e2e95a7a8dda2f2c` | yes |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `19085d33007f9d79c50945a7ddb083ab4979192e75c00f57af55a3515dcd6dc1` | `19085d33007f9d79c50945a7ddb083ab4979192e75c00f57af55a3515dcd6dc1` | yes |
| `scripts/ci/linux-v2-native-acceptance.sh` | `916aed4d2864ca433844ac5fb65bc0fb926edbe59912a85483766e571f7812cb` | `916aed4d2864ca433844ac5fb65bc0fb926edbe59912a85483766e571f7812cb` | yes |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `f982ef7a59642fd4cce738bb59c4cf1ea6a4eaf79491385933b2eec494f34012` | `f982ef7a59642fd4cce738bb59c4cf1ea6a4eaf79491385933b2eec494f34012` | yes |
| `scripts/ci/register-linux-v2-claim-machine.py` | `94fccf0588b2f423ae2d59b63384ed7572514eb664950dc25aab1a95a76e42a8` | `94fccf0588b2f423ae2d59b63384ed7572514eb664950dc25aab1a95a76e42a8` | yes |
| `scripts/ci/write-linux-v2-owner-request.py` | `5b9074374fc55119e8c8254b1a61f83585c4d745e8ec801caf67252e009e4196` | `5b9074374fc55119e8c8254b1a61f83585c4d745e8ec801caf67252e009e4196` | yes |
| `scripts/ci/linux-v2-source-subject.py` | `27fed36a71d46129ce2b713d55b6615573408a37c0dc8cceb9e5918dcaacd452` | `27fed36a71d46129ce2b713d55b6615573408a37c0dc8cceb9e5918dcaacd452` | yes |
| `scripts/ci/linux-v2-native-rootfs-base.sh` | `c8e4972490151e03e6b6c9ea3fa5393a8c348ebf0c9912e1d16770842f7c5866` | `c8e4972490151e03e6b6c9ea3fa5393a8c348ebf0c9912e1d16770842f7c5866` | yes |
| `scripts/ci/linux-v2-host-prerequisite-probe.sh` | `b8b91f407dd1eb4f79c3a27ef03bb1c5ff2dad7ff5cc6911a1e1d2d7678e9fad` | `b8b91f407dd1eb4f79c3a27ef03bb1c5ff2dad7ff5cc6911a1e1d2d7678e9fad` | yes |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | `b687669b0826da767502464978c7af596e549e28b45a7cc54b2abc064e508c50` | absent (scratch-only) | scratch-only |
| `scripts/ci/scratch-gh-shim` | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` | absent (scratch-only) | scratch-only |

The fix round changes test and record bytes only: R1/R2 edit `adapter_cmdline_linux_test.go` and
`gate_census_linux_test.go`, and neither is a run input of the acceptance leg — the equality
table above lists the run inputs and all remain byte-equal — so the Bind295 row stays binding
for the production bytes (`wire.go`/`gate.c` unchanged).

- Deferred PR #198 mutant coverage: the `docs/TODO.md` clause (the item opening "Cover the
  linux-gate wire validations no test kills") is amended in the tracker (2026-11-26, review
  REV-1) so `readWireString`'s `maxCommandString` bound leaves the surviving-mutant list,
  covered by the decode row of `TestWireLinuxCommandStringBoundary`. Probed before amending: the
  single-point mutant patch `wire.go:451` `length > maxCommandString` → `length > maxReleaseSize`
  reddens exactly that row — `go test -race -count=1 ./native/linux-gate/protocol/` exits 1 with
  `wire_linux_test.go:279: decode the cap-plus-one RELEASE refusal = Linux owner native argv
  contains an empty, oversized, or NUL value` — and the restored file is byte-identical
  (`0bb6b92c…`) with the row green again. The NUL check and the item's other named checks stay
  open.

## 8. Residual risks and limits

- The C rows exercise the real `read_release` parser through a test-owned probe; the installed
  launcher rows (`TestInstalledLinuxOwner*`) still skip on this host. The e2e
  `claude_containment` leg's Bind run `45710455914` (§7) shows the argv wall removed at both
  definition sites, the transport reaching its first native stage, and the leg's next stop
  recorded in `docs/TODO.md`.
- G2/G3 are cap-relative shape pins; the absolute 64 KiB value is pinned by G4's literal assert
  (compile-time) and by G1's real 23881-byte payload crossing the boundary.
- The helper re-validation follows the Go constant automatically; no separate helper row is
  needed (grep in §2), but that is an inspection, not a runtime row.
