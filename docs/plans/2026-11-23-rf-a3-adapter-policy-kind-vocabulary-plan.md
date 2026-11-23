# rf-a3 adapter policy + kind vocabulary — plan

Contract: `goal-rf-a3-adapter-policy-kind-vocabulary.md` (GDD state root, external to the
repository), with the 2026-11-23 conductor rulings Q1=a, Q2=a, Q3=a binding over it. Base revision:
`b9d4b4a4cfff1467ce9bbd24c127107c884fd077` (#243 W4-15b, #265 W5-16, #266 W4-22 merged).
Branch: `refactor/rf-a3-adapter-policy-kind-vocabulary`. Owned files are the contract's strict set,
re-derived in §1, plus this plan.

Goal shape: the harness-kind vocabulary gets one Go owner (`internal/protocol`) for the seven wire
strings and one internal mapping owner (`internal/harness`) for wire→`harness.Kind` plus the two
predicates; `config` keeps its constant names as compiled aliases; the contract schema enumerates
the wire values and every remaining copy is drift-bound to it by a red-able test; `generate.mjs
--check` runs inside `contracts:check`; with Q1=a the two app policy sites
(`admissionLaunchFailure`'s provider-access and retained-resume kind lists) answer only through the
capability projection, and the base-vs-HEAD truth table differs only in the authorized Q1 sentences
and the fabricated-projection rows. No wire byte, persisted byte, reason code, error type, write
order or CI-pinned name changes.

## 1. Owned-file re-derivation at dispatch (HEAD `b9d4b4a4`)

| Contract entry | HEAD reality | Action |
| --- | --- | --- |
| `internal/protocol/protocol.go` `validateRuntimeMetadata` `:210-224` | seven-value switch `:216-220`, text `harness_kind %q is invalid` `:219` | add `HarnessKind`/`ParseHarnessKind`/`HarnessKinds`; switch to `ParseHarnessKind`; text unchanged |
| new `internal/protocol/harness_kind_test.go` | absent | add: reads `contracts/v1/common.schema.json`, set equality with `HarnessKinds()` (`runtime.Caller`+three-up, `internal/contracts/schema_test.go:528-534` pattern) |
| new `internal/harness/kind.go` + `kind_test.go` | absent; `Kind` constants at `harness/types.go:22-30` | add `ParseKind` (7 rows, `claude_code`→`KindClaude`, error `runtime.harness_kind %q is unsupported`), `Kind.NativeGoal()`, `Kind.UnavailableProbe()`; test: accepts exactly `protocol.HarnessKinds()`, every parsed kind distinct, wire-only spellings refused |
| new `internal/harness/control_capability_test.go` | absent | pin `ControlCapability` per `ControlKind` |
| `internal/harness/generic.go` (export + call site only) | unexported `controlCapability` `:252-265`; call site `:201` | export as `ControlCapability`; repoint `:201` |
| `internal/harness/claude/adapter.go` (delete copy + call site only) | `controlCapability` copy `:582-595`; call site `:538` | delete copy; call `harness.ControlCapability` |
| `internal/config/config.go` | seven constants `:58-72`; validation switch `:320-325`, text `runtime.harness_kind %q is unsupported` `:323` | constants `= string(protocol.HarnessKindX)`; switch → `protocol.ParseHarnessKind`; text unchanged |
| `internal/app/probe.go` | `configuredHarnessKind` `:122-141`; `unavailableRuntimeKind` `:143-150`; `nativeGoalHarnessKind` `:152-161`; `:49` consumes the second | `configuredHarnessKind` → one-line `harness.ParseKind` delegate; delete the two bodies; `:49` → `kind.UnavailableProbe()` |
| `internal/app/goal_admission.go` | provider-access kind list `:49-55`; resume kind list `:71-75` | delete both lists; keep `Require` at `:56-58` and `:76-78` (Q1=a) |
| `internal/app/goal_admission_tx.go` (`:277` call site only) | `if !nativeGoalHarnessKind(configuredKind)` `:277` | `if !configuredKind.NativeGoal()` |
| new `internal/app/adapter_policy_truth_table_test.go` | absent | seven-row base-vs-HEAD table; reuses `testVerifiedNativeCapabilities` (`harness_test.go:5017`), the production registry probe (`newHarnessRegistry`) with an absent binary, and the promoted-pi seam (`pi.NewAdapterWithRunner` + `nativeProbeDigestFixtures`, precedent `harness_test.go:465-520`) |
| `internal/control/validation.go` | attach list `:569-573`, text `harness_kind %q is invalid` `:572` | `protocol.ParseHarnessKind`; text unchanged. Recorded: `generic` becomes locally acceptable (the shared parser is the seven wire values; the daemon never sends it and Control still refuses it) |
| `internal/control/validation_harness_kind_test.go` (repoint only) | literal six-kind iteration `:9-22` | iterate `protocol.HarnessKinds()`; keep the unknown-kind rejection arm |
| `internal/linuxpreflight/cgroup_linux.go` | `configuredHarnessKind` copy `:23-42`; `nativeGoalHarnessKind` copy `:44-53`; gate `:55-62` (incl. `kind == "generic"`) | delete both copies; `harness.ParseKind` + `kind.NativeGoal()`; the redundant `generic` disjunct drops |
| `internal/linuxpreflight/preflight_linux_test.go` (repoint only) | `TestNativeRuntimeSelectionVocabularyMatchesTheConfiguredKinds` `:326-348` | same literal policy table, read through `harness.ParseKind`/`NativeGoal` |
| `contracts/v1/common.schema.json` | `Adapter.kind` = `$ref ShortIdentifier` `:943-954` | inline enum of the seven wire values |
| new `contracts/fixtures/invalid/adapter.unknown-kind.json` + one manifest row | absent (`adapter.unknown-enum.json` is the neighbouring shape) | `kind: "foreign_harness"`, everything else valid; row `valid: false` |
| `package.json` (one script member) | `contracts:check` `:8` = typecheck + tests + `check.mjs` + `check-effect.mjs` | append `&& node contracts/scripts/generate.mjs --check` |
| regenerated artifacts | `contracts/generated/ts/{adapter-capabilities,workspace-read}.ts`, `ts/index.ts`, `effect/index.ts`, `daemon/internal/contracts/generated.go`, `daemon/internal/contracts/schema_bundle.json` | `node contracts/scripts/generate.mjs` |
| `control/test/symmetry_control/native_harness_kind_binding_test.exs` | Elixir-internal bindings only | one assertion: `Orchestration.native_harness_kinds() ++ ["generic"]` ≡ the schema enum (file-reading precedent `goals/contract_validation_test.exs:11`) |
| `docs/TODO.md` / `docs/archive/CLOSED-TODO.md` | entries `:88-93` (controlCapability, assigned to A3), `:1209-1230` (three vocabulary copies), `:1439-1442` (linuxpreflight copy) | close all three into CLOSED-TODO with closure text naming this slice; add the Q2 claude-attach-spelling entry |
| this plan | absent | this file |

## 2. Public/API shape

```go
// internal/protocol
type HarnessKind string
const (
    HarnessKindGeneric    HarnessKind = "generic"
    HarnessKindCodex      HarnessKind = "codex"
    HarnessKindClaudeCode HarnessKind = "claude_code"
    HarnessKindPi         HarnessKind = "pi"
    HarnessKindOMP        HarnessKind = "omp"
    HarnessKindOmO        HarnessKind = "omo"
    HarnessKindOpenCode   HarnessKind = "opencode"
)
func ParseHarnessKind(value string) (HarnessKind, bool) // 7-row membership
func HarnessKinds() []HarnessKind                       // wire order, fresh copy

// internal/harness
func ParseKind(value string) (Kind, error) // single wire→internal table; claude_code→KindClaude
func (kind Kind) NativeGoal() bool         // staged family = all but generic (allowlist, fail-closed)
func (kind Kind) UnavailableProbe() bool   // claude, pi, omp, omo, opencode
func ControlCapability(kind ControlKind) Capability // exported root helper

// internal/config — names unchanged, values are aliases
RuntimeHarnessGeneric = string(protocol.HarnessKindGeneric) // … ×7
```

- `ParseKind` is built on `protocol.ParseHarnessKind`, so the wire set cannot drift inside the
  mapping; its error text is byte-identical to the two copies it replaces.
- `NativeGoal`/`UnavailableProbe` are explicit allowlists, so an unknown `Kind` stays false exactly
  as the deleted switches answered.
- `app.configuredHarnessKind` stays as the app-local delegate (`linux_owner_v2_linux.go:112`,
  `linux_owner_witness_linux.go:2841`, `goal_admission_tx.go:270`, `probe.go:38,177` call it).
- `linuxpreflight` and `control` consume the shared parser directly; no local copy remains.

## 3. Edge-case semantics the contract demands

1. **Q1 decision positions.** `admissionLaunchFailure` order is unchanged: observe-purpose →
   `Require(start, events, cancel [, handoff])` → provider access → hard cost → session mode. Only
   the two kind lists inside are deleted; the retained `Require` calls and the
   `taskResultFailure(resume_rejected, err)` wrapper stay. So decisions, reason codes, error types
   (`*harness.CapabilityError` inside the `taskResultReasonError` wrapper) and positions are
   byte-identical; sentences for list-refused kinds become the projection's own reason.
2. **Truth-table projections.** Per kind the table records (a) `configuredHarnessKind` answer, (b)
   `NativeGoal()`, (c) `UnavailableProbe()`, and for `admissionLaunchFailure`: (d) provider access
   for the verified-minimal projection, for the same projection with `provider_access=true`, and for
   the production projection (composition-root registry probe with an absent binary — the daemon's
   real fail-closed state on a host without the harness), (e) resume for the production projection,
   the verified-minimal projection and the same projection with `resume=true`, (f) linuxpreflight
   selection (pinned by the repointed linuxpreflight test; linux-tagged, so recorded from the WSL
   run). Promoted-pi rows use `pi.NewAdapterWithRunner` with the repo's
   `nativeProbeDigestFixtures`, so the one production resume advertiser is exercised promoted
   (Windows) and fail-closed (other platforms).
3. **Golden shape.** For projection-driven rows the expected sentence is derived from the projection
   itself (`Require`'s reason = `Unsupported[capability]` or `capability is not verified`) and the
   expected decision/type are literal; for the verified-minimal rows the sentence is the literal
   default. The base column is produced by the same test file with only the two predicate calls
   mechanically shimmed to the base functions (documented diff in `answer.md`, since
   `Kind.NativeGoal`/`UnavailableProbe` do not exist at base); the base run is expected red on
   exactly the Q1 text rows plus the fabricated-projection rows.
4. **`control` attach validation.** `ParseHarnessKind` admits `generic` locally where the old
   literal list did not. Authorized by contract Change 1/4 (the test iterates `HarnessKinds()`); it
   cannot change a wire byte (the daemon never sends `generic` on attach) and Control's own gates
   still refuse it. Recorded as a finding.
5. **Schema enum propagation.** The inline enum on `Adapter.kind` reaches `adapter-capabilities` and
   `workspace-read` through the existing `$ref`; the Effect generator already handles inline enums
   (`guidance`, `pause`, `usage`); the Go enum types quicktype emits are generated and not imported
   by hand-written sources.
6. **Out-of-scope surfaces stay.** The three argv validators and `ErrResumeRejected` disjunction,
   the Linux-v2-owner predicates, workspace-policy-per-kind, credential paths, witness kind/version
   pins, Elixir omp/omo claim rules, `scripts/ci` kind maps, the claude attach spelling leak (Q2=a:
   recorded, not fixed), `opencode/**`, `staged_driver.go`, `StartRequest`, W6-03/W4-13/W5-10
   files, `control/client.go` decode entries (A6).

## 4. Evidence plan

- Red→green: truth table (base red on the named rows), `kind_test.go`, `harness_kind_test.go`,
  `control_capability_test.go`, the new invalid fixture via `pnpm contracts:check` (red at base: row
  claims `valid: false` while the unconstrained `ShortIdentifier` still accepts it), the Elixir
  binding assertion (red at base without the enum).
- Mutants M1–M9 exactly as the contract lists them (M9 two-step: drop a schema enum value →
  `--check` red; regenerate → Go schema test + Elixir assertion red).
- Grep gates: old sentence texts absent in `daemon`/`control`/`frontend`/`scripts/ci`; no
  `case`/`==` wire-kind literal in `internal/{app,linuxpreflight,control}`; only the accounted
  `app/pi_provider_bridge.go:209` path-segment hit in the broad literal grep; no second
  wire→internal mapping.
- Generator: `node contracts/scripts/generate.mjs --check` (now inside `contracts:check`) and
  `pnpm contracts:check` green, manifest row count 261 → 262.
- Go: `gofmt -l .`, `go vet ./...`, `GOOS=windows/linux/darwin go build ./...`, Windows
  `go test -count=1` over protocol/config/harness/app/control, WSL `-race` over the same + the
  linuxpreflight package; SKIP lists before/after; no added time-waiting tests.
- Frontend: `pnpm contracts:check`, `pnpm --filter @symmetry/frontend typecheck`, `... test`.
- Elixir: `MIX_ENV=test mix compile --warnings-as-errors` + the binding test where Postgres exists;
  otherwise recorded as environment-absent with the CI `control` job as the oracle.
- `answer.md` carries the full base-vs-HEAD truth table, grep outputs, mutant runs, base runs of
  every pin, generator/fixture outputs, suite outputs with SKIP lists and the dLOC line.

## 5. Out of scope (with reason)

- The claude attach spelling leak (Q2=a): record in `docs/TODO.md`; fixing it changes a wire value
  and enables a previously refused flow — its own authorization, Control-side test and review.
- Moving argv validators / resume sentinels behind the adapters (clause 4): changes app-framed
  sentences and error identities (R2 frozen).
- Full per-language codegen (Q3=b rejected): quicktype's mangled package-scope constants, no Elixir
  generator path; the drift-binding tests deliver the binding at this size.
- `internal/harness/omo/adapter.go`'s ControlCapability copy: not identical (default is
  `CapabilityCancel`) — stays; recorded in the TODO closure.

## 6. Risks

- **Truth-table projection drift**: the production-probe rows depend on adapters' fail-closed
  projections with an absent binary; the expectations for those rows are derived from the returned
  projection, so content drift in an adapter's reason text cannot make this slice's test red — the
  reason *content* is owned and pinned by each adapter's own package tests.
- **Base-run shim**: the base column needs a two-call mechanical shim (Go has no feature detection);
  the shim diff ships inside `answer.md` and touches nothing else.
- **Elixir environment**: Postgres/`deps` may be absent locally; the binding assertion then has a
  standalone red/green check plus the CI oracle, stated as such.

## 7. As-built note (implement stage, 2026-11-23)

1. **Truth-table production column.** It is built through each adapter's exported probe seam with
   an absent command, not through a live `newHarnessRegistry(...).Probe` against a missing binary:
   the live probe's bounded-probe classification of a missing executable is timing-dependent on
   Linux (the joined supervisor error can carry the 1 s probe deadline, and codex then keeps its
   general unverified reason instead of `codex version probe failed`). The seam-built projections
   matched the observed live ones on Windows and are deterministic and fast on both test hosts.
   Per kind: generic and opencode use their pure probes; codex and pi get an injected
   absent-executable runner; claude and omp are pointed at an absolute absent path (claude still
   reports `Claude Code executable is unavailable` because `probeNativeEvidence` wraps every
   non-context run error as `AvailabilityError`); omo keeps the bare absent name because its
   adapter reports `OmO executable is unavailable` only through the PATH-resolution failure.
2. **`control` attach validation widening.** `ParseHarnessKind` admits `generic` locally where the
   literal six-kind list refused it — authorized by Change 1/4 (the repointed test iterates
   `protocol.HarnessKinds()`); no wire byte changes, the daemon never sends `generic` on attach,
   and Control's own gates still refuse it.
3. **Generator wiring.** `contracts/scripts/check.mjs` already spawned `generate.mjs --check`
   (`runGeneratorCheck`) before this slice, so `pnpm contracts:check` already gated the generator
   transitively; C6's "no CI caller exists" held only for a direct `.github/**` caller. Q3=a is
   implemented literally anyway: `package.json`'s `contracts:check` now names the check
   explicitly, so the wiring is visible and mutant-checkable at the script level.
4. **Frontend gate environment.** The worktree had no `node_modules` and the registry was
   unreachable, so the contracts tooling and frontend dependencies were borrowed read-only from
   two worktrees with byte-identical `pnpm-lock.yaml` (root/contracts from the primary checkout,
   frontend from `wt-w5-08-jsdom-browser-split`) through directory junctions; `pnpm --filter`
   refuses a junctioned `node_modules`, so the frontend gates ran as
   `frontend/node_modules/.bin/tsc --noEmit` and `…/.bin/vitest run`. TypeScript resolves
   `@symmetry/contracts` lexically to this worktree, while Vite resolves the borrowed workspace
   link by realpath into the other worktree; the test gate therefore ran with one throwaway
   config that aliases `@symmetry/contracts` to this worktree's `contracts/` (deleted after the
   run). The deviation and its diagnosis are recorded in the stage answer artifact; CI's
   frontend job remains the oracle.
