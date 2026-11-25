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
| `daemon/native/linux-gate/protocol/gate_census_linux_test.go` | `f9a771bb95b39a86d0d1b58ae2509b075e6dffb16aa1a6378a1d1bc087648064` |
| `daemon/internal/harness/claude/adapter_cmdline_linux_test.go` | `e36b255e27363511a5231efba29427fd87c1e66c8a4140033b459e6dbcadca16` |
| `docs/plans/2026-11-26-rf-claude-schema-argv-plan.md` | `ab4822753d83a0d7d1b3388d7233cff9b48c388fc1fa9d07938f4561506425d0` (record revision; not run-bound) |

`docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and this record are record-only and deliberately
not hashed. The working-tree inventory is exactly these files plus the two record edits
(`git status --porcelain -uall --no-renames`: 4 modified, 2 untracked).

The apply-stage simplification (2026-11-26) then changed `gate_census_linux_test.go`,
`adapter_cmdline_linux_test.go` and one plan anchor row: the three digests above are re-derived
at those apply bytes, and every §5 row was re-run there (same counts; refreshed durations).

## 2. The change

Two lines, one change: `wire.go:53` `maxCommandString = 16 << 10` → `64 << 10`; `gate.c:91`
`MAX_COMMAND_STRING = 16 * 1024` → `64 * 1024`. Nothing else moved: `maxReleaseSize`
(`wire.go:52`, `gate.c:90`), `maxCommandVector` (`wire.go:54`, `gate.c:92`), the frame magic,
versions and layout, and the unrelated `char data[16384]` (`gate.c:946`). No third definition
site exists (`grep -rn "maxCommandString\|MAX_COMMAND_STRING" daemon` finds the two definitions
plus uses only; the helper's re-check at `daemon/internal/platform/helper_v2_server.go:1075`
calls `linuxgate.NativeCommand.Validate()` and follows the Go constant). The RELEASE v2/v3
golden-byte test (`TestWireLinuxReleaseGoldenBytes`) passes unchanged, which is the byte-level
proof of the "no protocol v1 change, no RELEASE version bump" invariant. No comments were
added.

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
| G1 | `TestClaudeCandidateLinuxCommandArgvFitsOwnerWireBound` | FAIL, exit 1: `adapter_cmdline_linux_test.go:33: the fixed Claude argv is refused by the Linux owner native command contract: Linux owner native argv contains an empty, oversized, or NUL value` — the run's own refusal, reproduced by the new twin |
| G2 | `TestWireLinuxCommandStringBoundary` | PASS: the pair is cap-relative (`maxCommandString`); its red proof is M1 |
| G3 | `TestGateCommandStringBound` | PASS at base (element sizes follow the Go cap, 16 KiB both sides; the probe accepts 16384 and refuses 16385 with `EPROTO`); its red proof is M2 |
| G4 | `TestGateFDCensusAllowsStdioAndRejectsUnexpectedDescriptors` | PASS: 16 KiB == 16 KiB; compile-time pin, red only under drift |

The delivered-form behaviour at the new cap (all green, exit 0):

- G1 PASS, log line verbatim: `Claude candidate --json-schema element = 23881 bytes, guarded
  RELEASE packet = 24317 bytes` — the real fixed argv passes `Validate()` and `EncodeRelease`.
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
| Protocol race | `go test -race -count=1 -timeout 25m -v ./native/linux-gate/protocol/` | 0 | `ok` 7.843s, 47 top-level PASS / 11 SKIP / 0 FAIL (settled bytes; subtests of both new rows PASS) |
| Claude race | `go test -race -count=1 -timeout 25m -v ./internal/harness/claude/` | 0 | `ok` 6.907s, 74 top-level PASS / 5 SKIP / 0 FAIL |
| Regression rows | `go test -race -count=1 -v -run "TestWireLinuxReleaseRefusals\|TestWireLinuxCloneBootstrapRefusals" ./native/linux-gate/protocol/` | 0 | both PASS; the dispatch-head `:233,260,372` oversized-frame and total-bound refusals are untouched (`:233` stays; `:260`/`:372` are `:307`/`:419` after the 47 inserted lines) |
| Twin runtime | `go test -race -count=1 -v -run TestClaudeCandidateLinuxCommandArgvFitsOwnerWireBound ./internal/harness/claude/` | 0 | PASS 0.10s (schema 23881 / packet 24317) |

The 11 protocol skips are the pre-existing installed-owner/credential-gated tests
(`TestInstalledLinuxOwner*Scratch`, `TestLinuxOwner*Worker`/`Helper`,
`TestLinuxOwnerNativeOwnerDeath*`, `TestLinuxOwnerCloneNativeGitHelper`); the 5 claude skips are
the pre-existing live-blob tests (`TestClaudeCandidateLive*`,
`TestNativeClaudeCandidateStructuredOutput`). Both sets skip at base and after; they are
missing evidence, not passes.

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
  reaches `system/init` or its first native stage). That run has not happened at these delivered
  bytes — the closure text names it as pending, and a later stage re-binds the text (the
  apparmor precedent re-bound its closure after its Bind runs). This record claims no run that
  has not executed.
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
  launcher rows (`TestInstalledLinuxOwner*`) still skip on this host, and the e2e
  `claude_containment` leg is the conductor's Bind run. The argv wall is removed at both
  definition sites; no claim is made about walls beyond it.
- G2/G3 are cap-relative shape pins; the absolute 64 KiB value is pinned by G4's literal assert
  (compile-time) and by G1's real 23881-byte payload crossing the boundary.
- The helper re-validation follows the Go constant automatically; no separate helper row is
  needed (grep in §2), but that is an inspection, not a runtime row.
