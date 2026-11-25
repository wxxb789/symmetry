# rf-claude-schema-argv — implement plan

Contract: `goal-rf-claude-schema-argv.md` (GDD state root, external to the repository). Base
revision `8453da12` (PR #293 merge, the ruling's dispatch gate); branch
`refactor/rf-claude-schema-argv`. The accepted plan is the contract — option (a) of the ruling
2026-11-26, both consult reports at the GDD state root `findings/argv-consult-{sol,ds}.md` — and
this file resolves only the implementation choices it leaves open, plus the evidence plan.

## 1. Clause census at the dispatch head

| Contract clause | Dispatch-head location | Content |
| --- | --- | --- |
| Change 1 Go cap | `daemon/native/linux-gate/protocol/wire.go:53` | `maxCommandString = 16 << 10`; uses `:451` (`readWireString`), `wire_linux.go:424,427,450,457,470` (path/dir/argv/env/total), `proc_identity_linux.go:107` |
| Change 1 C cap | `daemon/native/linux-gate/c-gate/gate.c:91` | `MAX_COMMAND_STRING = 16 * 1024`; uses `:816` (path), `:881` (`read_span` per element and running total) |
| Change 1 same change | both files | the two lines above are the only definition sites: `grep -rn "maxCommandString\|MAX_COMMAND_STRING" daemon` finds no other definition, and the helper's re-check (`daemon/internal/platform/helper_v2_server.go:1075`) calls `linuxgate.NativeCommand.Validate()`, i.e. it follows the Go constant automatically |
| Change 2 drift oracle | `daemon/native/linux-gate/protocol/gate_census_linux_test.go` | the FD-census probe `#include`s `gate.c` (`:97`) and carries `_Static_assert`s for closed wire numbers (`:680-692`); no oracle ties the two caps today |
| Change 3 Linux argv twin | `daemon/internal/harness/claude/adapter_cmdline_windows_test.go` | the shape reference: capture the real invocation, assert the platform limit; Linux has no twin |
| Change 3 Go boundary | `daemon/native/linux-gate/protocol/wire_linux_test.go` | `TestWireLinuxReleaseRefusals` (`:237`) and the refusal helpers (`:403-500`); no row pins the per-element cap |
| Change 3 C boundary | `gate_census_linux_test.go` | `buildGateFDCensusProbe` (`:68`) and `buildGateReleaseFenceProbe` (`:271`) drive `reject_unexpected_inherited_fds` and `exec_as_b`/`exec_clone_bootstrap` directly; neither calls `read_release`, so no host-runnable row enters the parse boundary today; the installed-owner launcher rows (`launcher_integration_linux_test.go`) are operator-gated (skip without `SYMMETRY_LINUX_OWNER_TEST_SCRATCH=1`) |
| Change 4 existing refusals | `wire_linux_test.go:233,260,372` | "oversized beyond the release bound" rows and the padded-packet total-bound rows; unchanged by this change (they exceed `maxReleaseSize`, not the element cap) |
| Change 5 records | `docs/TODO.md:1896-1917`, `docs/archive/CLOSED-TODO.md` | the rf-acc-containment-goal-admission residual carries the claude stop verbatim, owner `rf-claude-schema-argv` |

Dispatch-head measurements: `go test -race -count=1 ./native/linux-gate/protocol/` → `ok`
6.501s; `go test -race -count=1 ./internal/harness/claude/` → `ok` 6.937s (WSL
`Ubuntu-24.04`, go1.27.0, user `accept`). The refusal this goal removes, run `45671784251`:

```
start native owner witness transport: start "/usr/local/bin/claude": validate native command
and three Runner pipes before cgroup reservation: Linux owner native argv contains an empty,
oversized, or NUL value
```

at `daemon/internal/execution/linux_owner_v2_linux.go:182` (`native.Validate()`), root cause the
23881-byte `--json-schema` element (`adapter.go:169-180`) against the 16 KiB cap. The real
bounds are unchanged by this goal: frame `maxReleaseSize = 128 << 10`
(`wire.go:52`, `{packet_size} > MAX_RELEASE_SIZE` at `gate.c:785`), cumulative
`total > maxReleaseSize - releaseHeaderSize - 4096` = 126916 (`wire_linux.go:470`,
`gate.c:884`), `maxCommandVector = 128` (`wire.go:54`, `gate.c:92`), and `MAX_ARG_STRLEN`
128 KiB. `char data[16384]` (`gate.c:946`) is the `/proc/self/status` buffer and stays.

## 2. Choices the contract leaves open

### D1 — drift-oracle shape (contract clause 2)

The census probe compiles with the *current* Go module, so it can carry the Go value at compile
time: both probes in `gate_census_linux_test.go` are prefixed with
`#define CENSUS_GO_MAX_COMMAND_STRING <maxCommandString printed by Go>` (string concatenation
of the constant, never a literal) and the FD-census probe carries both the contract's literal
absolute pin and the tie:

```c
_Static_assert(MAX_COMMAND_STRING == 64 * 1024, "the gate C cap must be the ruled 64 KiB bound");
_Static_assert(MAX_COMMAND_STRING == CENSUS_GO_MAX_COMMAND_STRING,
    "the gate C cap must track Go protocol.maxCommandString");
```

Changing either constant alone fails that probe's compile step: the literal assert reds a C-side
change (including a coordinated move away from the ruled value, which the cap-relative rows
alone would not catch), and the injected define reds a Go-side change; together they pin both
sides to 64 KiB.

### D2 — the C gate boundary row (contract clause 3)

A new probe builder (third in `gate_census_linux_test.go`) whose two argv scenarios call the
gate's real `read_release` over a real `AF_UNIX`/`SOCK_SEQPACKET` channel with a real four-FD
`SCM_RIGHTS` transfer and a real `pidfd_open(getppid())` for `OWNER_PIDFD`, i.e. it enters the
exact parser that refused the run (`read_release` → `read_span`). The element size is
`CENSUS_GO_MAX_COMMAND_STRING + {0,1}`, so the rows assert the cross-language boundary — the
element size the Go side admits at its cap must be admitted and parsed by the C gate, and
cap+1 must be refused with `EPROTO`. Probe reuse was rejected because the existing probes enter
post-parse (`exec_as_b` with a prepared `struct release_command`); the operator-gated launcher
rows were rejected because they cannot run on this host. The probe frames rows with the gate's
own constants (`WIRE_MAGIC`, `RELEASE_HEADER_SIZE`, `RELEASE_WIRE_VERSION`, `FRAME_RELEASE`).

Why the row sizes are not taken from `MAX_COMMAND_STRING`: with the Go value driving both the
compiled-in pin and the row sizes, a C-only revert compiles the boundary probe and turns the
*at-cap row* red (the drift assert reds separately in the census probe), while a Go-only revert
turns the argv twin and the Go rows red — each mutant clause of the contract maps to a distinct
red. The boundary probe deliberately does not repeat the assert, so the two failure modes stay
separable.

### D3 — mixed-vintage fail-closed pin (Invariants)

Frame layout, magic, versions and `maxReleaseSize` do not move, so a 16 KiB-vintage gate parses
a new frame positionally and refuses it at `read_span` with `EPROTO` — exactly what mutant M-C
measures (C reverted with Go raised → the at-cap row is refused `EPROTO`, never misparsed).
That is the mixed-vintage pin; no extra row is added.

### D4 — comments

No comments are added: both constant lines change value only, and the pinning tests carry the
hazard. (`wire.go`'s constant block and `gate.c`'s enum hold no per-constant prose today.)

### D5 — closure record (contract clause 5)

The residual moves to `docs/archive/CLOSED-TODO.md` in this stage with the base red
(`45671784251`) and the delivered evidence, and names the binding run as the conductor's Bind
scratch run of the `claude_containment` case set on the PR head — pending at the delivered
bytes, per this goal's acceptance clause. This matches the apparmor precedent, whose closure
text was re-bound with its Bind runs in later stages; nothing here claims a run that has not
happened.

## 3. Evidence plan

Red first, at the delivered test bytes with the base constants:

| Row | Command (WSL, `daemon/`) | Expected at base |
| --- | --- | --- |
| G1 Linux argv twin | `go test -race -count=1 -run TestClaudeCandidateLinuxCommandArgv ./internal/harness/claude/` | FAIL: `Validate()` refuses the 23881-byte element |
| G2 Go boundary pair | `go test -race -count=1 -run TestWireLinuxCommandStringBoundary ./native/linux-gate/protocol/` | PASS: the pair is cap-relative (`maxCommandString`); its red is M1 |
| G3 C boundary pair | `go test -race -count=1 -run TestGateCommandStringBound ./native/linux-gate/protocol/` | PASS at base (element size follows the Go cap, 16 KiB both sides); its red is M2 |
| G4 drift oracle | `go test -race -count=1 -run TestGateFDCensusAllowsStdio ./native/linux-gate/protocol/` | PASS (both sides 16 KiB); red only under drift |

The base-time red for the absolute cap is carried by G1's 23881-byte element (refusal quoted in
§1); G2/G3 pin the boundary *shape* at whichever cap is compiled, and the mutant
rows below turn each into an observed red. G4 is the compile-time absolute pin.

After the two-line change: G1-G4 green, both packages green, `gofmt -l .` empty,
`GOOS=linux GOARCH=amd64 go vet` and `GOOS=windows GOARCH=amd64 go vet` on both packages and
`go vet ./...` (linux) exit 0.

Mutants, each observed red and then restored byte-identically:

| Mutant | Expectation |
| --- | --- |
| M1 Go cap `16 << 10`, C untouched | G1 red (the 23881-byte element no longer fits the Go cap), G3 red on the *cap-plus-one* row (C, still 64 KiB, accepts 16385 that the Go cap refuses), G4 red (the injected tie); G2 green |
| M2 C cap `16 * 1024`, Go untouched | G3 red on the *at-cap* row (`read_release` refuses 65536 with `EPROTO`), G4 red (both asserts) — D3's mixed-vintage pin; G1 green |

Measured, both restored byte-identically afterwards (sha256 in the record). G2 stays green
under a mutant that moves *both* caps together, by design: it is a cap-relative shape pin,
while the absolute value is pinned by G4's literal assert and G1's real payload.

Regression rows re-run unchanged: `TestWireLinuxReleaseRefusals` and
`TestWireLinuxCloneBootstrapRefusals` (the `:233,260,372` oversized-frame and total-bound
refusals), plus the whole protocol package.

## 4. Out of scope

- Schema shrink (option (b)) and out-of-band schema (option (c)) — rejected by the ruling.
- `maxReleaseSize`, `maxCommandVector`, frame layout, RELEASE versions, `char data[16384]`.
- The adapter's fixed argv, prompts, fixtures, `harness.md` (no amendment needed: no fixed
  design decision covers this shape guard).
- Installed-owner launcher rows and the e2e case set (operator-gated / conductor-run).
- `docs/TODO.md:1022-1027` (deferred PR #198 mutant-coverage item): the new decode row kills
  the `maxCommandString` bound there, the other named checks stay open; the item is left as is
  save for the evidence record's note.
