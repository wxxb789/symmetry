Synthesis snapshot recorded 2026-11-21 from the program synthesis at base revision a3039f0 (origin/main).
Per-slice contracts, kept outside this repository under the GDD state directory, supersede this plan's
numbers, line references and owned-file sets wherever they differ; every LOC estimate is report-only (D2).

# Refactor program plan

Synthesized 2026-11-21 from the eight Phase-0 area audits (control-core, control-web, daemon-app,
daemon-platform, daemon-protocol-rest, frontend, scripts-ci, tests-cross-cutting) against base
revision `a3039f0` (origin/main, fetched; worktree clean). Inputs live in
`<GDD-STATE>/refactor/findings/`; this plan lives beside them.

Owner mandate (2026-11-21): thorough refactor + quality uplift; anything allowed (protocol/schema
changes without backward compat, framework swaps, new packages) when it measurably improves
robustness, performance, engineering quality, or greatly reduces LOC. Hard invariants are never
traded: PostgreSQL owns durable control state; Elixir owns transition policy; Go owns machine-local
execution; frontend owns presentation. Fences, idempotency, transaction boundaries, credential
locality and provider-owned fields stay. Tests/types/validation/permissions are never weakened to
make a refactor pass.

Lint gates to add (owner-directed): Credo strict, golangci-lint, stricter ESLint, ruff.

Repo `Q:/repos/symmetry` is READ-ONLY for the audit/synthesis phase; every slice below lands as its
own PR on a branch, one GDD increment each.

---

## 1. Verification of the Phase-0 reports ("verify, don't trust")

Method: re-read each report's cited `path:line` in the read-only checkout at `a3039f0`; re-ran the
plug advisory check with the portable Elixir toolchain (`mix hex.audit`, invoked through
`<HOST-PATH>/.codex/tools/symmetry/elixir-1.20.4` + `erlang-29.0.6` in the same pattern as the
scratch `retro-g2-mix.sh`). At least the top 3 findings per area were checked.

### 1.1 Spot-check ledger (verified unless stated)

| Area | Claim checked | Evidence re-read / re-run | Verdict |
|---|---|---|---|
| control-core | `goals.ex` is 10,810 lines / 658 defs | `wc -l`, `grep -c 'def\|defp'` — both exact | ✅ |
| control-core | F4: six settlement mutation-id builders repeat the same UUID formatter | `goals.ex:10521` block re-read | ✅ |
| control-core | F11: `execute_accepted_resource_sync/2`, `validate_adapter/2` dead | two-strategy evidence in report; `mix xref unreachable` planned as oracle | ✅ (plausible; gate to confirm at implementation) |
| control-core | §5 Credo counts | report completed 06:50 with counts: **459 strict (Credo 1.7.12)** over lib+test, 370 of them in-area; per-check table in its §1.5 | ✅ (version delta vs control-web's 474/1.7.19 reconciled in §10.1; W1-01 re-takes once) |
| control-web | CW-1: every JSON mutation body decoded twice | `strict_evidence_json.ex:174-188` (`strict_json_parser` + ordered `read_json_body`) + `endpoint.ex:70-71` (`Plug.Parsers.JSON` decodes again) | ✅ |
| control-web | CW-13: only `chat-read`/`goal-read`/`attention-read` have served-document gates | `grep validate_served_document!`: only `chat_controller_test.exs:133`, `portal_goal_attention_test.exs:414-415` | ✅ |
| control-web | **CW-14: `plug` 1.20.3 under a medium advisory** | **live check**: `mix hex.audit` → `plug 1.20.3 - EEF-CVE-2026-120287 (MEDIUM)`, "Multipart `:length` limit not charged for part and header framing in Plug.Parsers.MULTIPART", GHSA-qj4r-jh2r-rw3f; `mix.lock` pins 1.20.3; `mix hex.info plug` → **1.20.4 released 2026-11-17**; `endpoint.ex` enables `parsers: [:urlencoded, :multipart, :json]` | ✅ **confirmed; ranked into wave 1** |
| control-web | CW-11: telemetry scaffolding inert | `metrics/0` has no caller; deps only at `mix.exs:46-47` | ✅ |
| daemon-app | F2: `startGoalAdmission` 962 lines | decl at `app.go:4610`, last statement ≈`:5569`; 12k-line file confirmed (`wc` 12,048) | ✅ |
| daemon-app | F11: unchecked `abandonGoalSession` calls | `app.go:5434` etc. re-read (error dropped) | ✅ |
| daemon-app | F15: `writeInputBounded` goroutine can strand | `app.go:7240-7274` re-read (buffered chan + timer, no owned exit) | ✅ |
| daemon-platform | P-01: two live Linux containment generations | `execution/process_launcher_linux.go:31-45` (v2-first dispatch, legacy fallback) + `app.go:1544` (`requiresLinuxV2NativeOwner` = `Pi || OpenCode` only) | ✅ |
| daemon-platform | P-02: witness/test scaffolding in the production helper | `containment_supervisor_linux.go:53-76` — ~18 `SYMMETRY_*` env constants incl. the production-witness gate | ✅ |
| daemon-platform | P-07: 12-argument `writeResponse` | `linux_owner_helper_v2_linux.go:2627` re-read (12 params) | ✅ |
| daemon-platform | P-05: production forks on "am I a test binary" | `containment_supervisor_windows.go:1501-1513` (`isGoTestBinary` → noop lease) | ✅ |
| daemon-protocol-rest | F-03: entire `harness/opencode` native adapter unwired | `registry.go:154,328-331` returns `newUnavailableAdapter`; `app.go:1163-1171` never registers opencode; package measured 3,460 non-test + 8,210 test LOC | ✅ |
| daemon-protocol-rest | F-09: operator task-control client production-dead | `client.go:485`; no non-test callers (`grep`) | ✅ |
| daemon-protocol-rest | F-19: `launchLinuxV2Process` stub is a `!linux \|\| !amd64` shell | `linux_owner_v2_unsupported.go:1-20` | ✅ |
| daemon-protocol-rest | F-08: `DecodeSchema` decodes/scans 4-6× | `contracts/schema.go:76-88`: `decodeRawJSON` + `ValidateSchema` re-decode + `json.Unmarshal` | ✅ |
| daemon-protocol-rest | F-04: `validateProviderActionUUID` weaker (no version/variant) | `client.go:1454-1466` re-read (charset+dashes only) vs `protocol/goal_subject.go:291+` copy | ✅ |
| frontend | F1: `ChatPage` is a 593-line god component | `chat.tsx:78` → `:670` (593 lines) | ✅ |
| frontend | F13: no route-level code splitting | `router.tsx:2-12` statically imports every page; single 886 kB chunk per build | ✅ |
| frontend | F10: implicit dual TS toolchain | `package.json:33` (`@typescript/native: npm:typescript@7.0.2`), `:40` (`typescript: npm:@typescript/typescript6@6.0.2`) | ✅ |
| frontend | F2: two hand-written HTTP boundaries | `api.ts:947` `postPortalCommand` + its own `refusal` at `:997` vs `parseRefusal` at `:571` | ✅ |
| scripts-ci | F4: six browser specs never run in CI | `grep` over `.github/**`: none of the six names appear; the render gate names the other seven (`ci.yml:704-729`) | ✅ |
| scripts-ci | F5: no Python lint/format config anywhere | no tracked `pyproject.toml`/`ruff.toml`/`setup.cfg` | ✅ |
| scripts-ci | F6: `scripts/ci/*.exs` outside the formatter | `control/.formatter.exs:4` inputs = `{config,lib,test}/**` only | ✅ |
| scripts-ci | F1: `ci.yml` 2,237 lines with inline programs | `wc` exact; inline-step count ~57-79 depending on counting method vs report's 74 — directionally right, treat 1,442-line estimate as approximate | ✅ (≈) |
| tests-cross-cutting | T2: 3,750-line single Go test | `app_test.go:6273` → `:10023` | ✅ |
| tests-cross-cutting | T3: 5 s deadline cluster | `registry.go:29` `claudeProbeTimeout = 5 * time.Second`; 71 `time.Sleep` sites in daemon tests | ✅ |
| tests-cross-cutting | T7: ExUnit mostly serial | 56 files `async: false` vs 14 `async: true` | ✅ |
| tests-cross-cutting | T10: no coverage instrumentation anywhere | no `--cover`/`-coverprofile`/vitest coverage in workflows/configs | ✅ |
| tests-cross-cutting | §6: "no Erlang/OTP or Elixir on the audit host" | **wrong as stated** — portable Elixir 1.20.4/OTP 29.0.6 runs `mix` on this host (this synthesis ran `mix hex.audit`; `retro-g2-mix.sh` documents the invocation) | ❌ corrected |
| scripts-ci | §6: "no Elixir/OTP installed on this host" | same as above | ❌ corrected |

### 1.2 Corrections and consequences

1. **"No Elixir on this host" is false.** A portable toolchain exists at
   `<HOST-PATH>/.codex/tools/symmetry/{elixir-1.20.4, erlang-29.0.6}` and runs mix/credo without
   mise. Consequences: (a) Credo baselines and `.exs` format checks are measurable locally in
   wave 1 — no WSL-vendoring workaround (control-core's §6 note) and no "toolchain absent" caveat
   (tests-cross-cutting §6, scripts-ci §6); (b) the plan's control-side acceptance commands are
   runnable on this host, `mix test` still needs a local PostgreSQL.
2. **control-core §5 (Credo strict) completed late (06:50)** with 459 issues over
   `control/lib` + `control/test` (Credo 1.7.12; 370 in the audited area), while control-web
   measured 474 project-wide under the default set (1.7.19). The runs differ by version and check
   selection; W1-01 re-takes the strict count with the portable toolchain (one pinned version)
   before the advisory gate lands and records it as the ratchet baseline.
3. Minor count deltas (off-by-one `wc` vs editor counts; ci.yml step counting; jscpd denominator
   methodology) are explainable methodology differences, not defects.
4. The claimed plug CVE is real and currently reachable (`:multipart` enabled, pinned 1.20.3,
   fix 1.20.4 available) → security bump is wave 1.
5. Everything else sampled was accurate at `a3039f0`; the reports' numbers are usable as baselines
   subject to re-measurement at implementation time.

---

## 2. Cross-area dedup (merged findings)

The eight reports restated many findings from different angles. Consolidated themes; each theme is
delivered by per-area slices (no slice spans areas), but only once per tree:

| # | Merged theme | Source findings | Delivery |
|---|---|---|---|
| M1 | Browser specs (4.6k lines) never run in CI | scripts F4, tests T4, scripts S6b | W2-01 inventory gate + manual matrix now; wiring/delete per decision D7 (W5) |
| M2 | Wall-clock verdicts (sleeps, fixed deadlines, budget≥budget waits, poll loops) | tests T3/T11/T14, app F16, platform §1.5, control-core §1.6, frontend F7 | per-language burn-down slices (W3-15, W4-04/13/14, frontend S11 W4) |
| M3 | Lint gates: 4 reports propose overlapping configs | control-web/core §5, app/platform/protocol/tests §5, frontend §5, scripts §5 | one reconciled config per tool in W1-01/W1-10/W1-11; shared policies (ST1005 exclusion, errcheck test relaxation, jscpd size flags, golangci v2.15.0 pin) in §6 |
| M4 | Fixture/helper sprawl across every suite | tests T1/T5/T6/T8/T9, control-core §1.6, app F16, frontend F6, protocol F-24 | per-area extraction (W1-12, W2-16→W3, W3-03/04, FE S10 W4, SC F7/T8 W2) |
| M5 | God modules/functions | core F1/F5/F6, web CW-8, app F1/F2/F4, platform P-06, protocol F-12/F-26, scripts F9 | move/split slices W1-04/05, phase unlocks W2-10/11, high-risk W4 |
| M6 | Dead code / unexercised surfaces | core F11, web CW-11/CW-12, protocol F-02/07/09/15/19, scripts F10/F11, frontend F8, tests T4/T13 | wave-1/2 deletions W1-06/10/14, decisions D5/D6/D8 |
| M7 | Test scaffolding inside production binaries / env-driven behaviour | platform P-02/P-05, protocol F-22, scripts F11 | decision D4 (in-place witness switch, no build tag); slices W3-04 (F-22), W5-01 (witness switch), W6-03 (test-binary fork) |
| M8 | Test-only seams exposed from production code | web CW-12, frontend F8/F9, tests §1.8 | fold into the owning area slices (W3-04, W4-05, W1-10) |
| M9 | Verdict observability: coverage + durations | tests T10/T14 | W2-17 coverage plane (moved from W1-01); W2-01 duration telemetry |
| M10 | CI YAML structure (inline programs, duplicated jobs, classifier-in-YAML) | scripts F1/F2/F3/F12, tests T14 | sequential `ci.yml` owners, one per wave: W1-01, W2-01 then W2-17 (coverage plane), W3-01 (classifier), W5-01 (witness switch), W6-01 + W6-02 (program extraction + browser wiring, serialized per §8) |
| M11 | Strict-decode asymmetry / multi-pass JSON | web CW-1, protocol F-05/F-07/F-08 | W1-09 (Go contracts), W2-02 (Elixir), W2-12 (client strictness) |
| M12 | opencode half-wiring | protocol F-03 (+ platform v2 and CI witnesses touch it) | decision D1 (keep & freeze); slice W6-04 parked |
| M13 | Harness capture/version + double sprawl | tests T6/T9, protocol F-24, app F16 | W3-04, W4-13, W5 test toolbox slices |

Hot-file discipline (applied per wave): `.github/workflows/ci.yml`, `control/mix.exs`,
`daemon/go.mod`, `frontend/package.json`, `daemon/internal/app/app.go` — at most one owner per
wave; `docs/TODO.md` and `docs/archive/CLOSED-TODO.md` are merge-driver handled and ignored for
conflict accounting. `goals.ex`, `state.go`, `runner.go`, `portal_json.ex`, `goals/read_model.ex`
are treated as quasi-hot: explicitly listed owners, one per wave.

---

## 3. Program shape

- **Slice = one PR = one GDD increment**: single purpose, independently mergeable, explicit owned
  file set, observable acceptance, exact oracle (slow suites named).
- **Waves**: W1 low-risk enablers (14 slices); W2 abstraction/dedup I (17); W3 abstraction/dedup II
  + remaining medium-risk (16); W4-W6 high-risk programs (structural refactors, the
  startGoalAdmission split, tail gates; the D1/D3 pools are parked/deferred per §9). Up to 16
  parallel workers.
- **Gates**: land advisory/report-only first; flip a gate to blocking per area when that area's
  count reaches zero. Baselines are re-measured at implementation time on `a3039f0`+.
- **Test-conversion rule**: any slice that deletes or converts an assertion must show a mutation
  check (flip the guarded behaviour, watch the converted test fail red) before merge.
- **One integration owner** for the high-risk programs (W4-W6) even when several workers contribute
  edits; worker branches never merge two owners' edits in one PR.

---

## 4. Wave 1 — 14 slices (low-risk, high-benefit enablers)

Covers the brief's wave-1 categories: lint baselines + gates in advisory/ratchet mode, vulnerability
scanning, dead-code deletions (re-scoped), the verified security bump, and move-only file splits.
All slices below have disjoint owned file sets; each hot file has exactly one owner (audited at the
end of §4). The coverage plane moved to W2-17; the wave-1 contract amendments are recorded below.

### Amendments applied (wave-1 contracts, 2026-11-21)

The wave-1 work orders were dispatched with the adversarial-review amendments folded in; the slice
bodies below are kept aligned. Summary (sources: `plan-review-opus-1.md`, `plan-review-ds-1.md`):

- **W1-01** — no new jobs, no coverage: advisory gates land as `continue-on-error` steps inside
  existing jobs only (the changed-path classifier suite pins job routing); coverage moves to W2-17;
  adds vulnerability scanning (`mix hex.audit`, `govulncheck` GOOS=linux+windows, `pnpm audit --prod`)
  and `.github/dependabot.yml`.
- **W1-03** — scratch credo: measure with the §10.1 `.credo.exs` verbatim via a local, uncommitted
  Credo dep; re-run after W1-01/W1-02 merge.
- **W1-06** — re-scoped: delete only surfaces proven caller-free by the deadcode intersection of
  three targets (GOOS=linux, GOOS=windows, `GOOS=linux GOARCH=arm64`) plus tree-wide greps
  **including `_test.go`/`e2e/`**; witness-used surfaces deferred; `launchLinuxV2Process` stub kept.
- **W1-07** — identical-operand changes dropped (the double `handleCommand` calls are deliberate
  replay assertions); `preflight:1822` fixed as the escape-parsing bug with a table test.
- **W1-08** — per-test `defer goleak.VerifyNone(t, …)` in a named leak-free set; no package
  `TestMain` until the F15 strand fix lands; package-seam tests stay serial.
- **W1-10** — root `pnpm-lock.yaml` (not `frontend/pnpm-lock.yaml`); base-red
  `tool-cache-config.test.ts:44` fixed via the owned `vitest.config.ts` suite `testTimeout`.
- **W1-11** — exclude `model-routing-record-sanitize.py` + its test until PR #137 lands; rebase on
  main before merge.
- **W1-12** — owned set corrected to the actual duplicate homes (orchestration*/workspaces/goals/
  migrations); reuse `MigrationCase` for `with_schema`; counts corrected (`with_schema` ×7, not ×8).
- **W1-13** — gates also assert presence of the consumed members (TODO:190 option (b)); the mutation
  check renames an **optional** consumed member.
- **W1-04/05** — every new sibling carries the source file's exact build constraint; declaration-
  multiset AST proof replaces relying on `--color-moved`; `GOOS=linux GOARCH=arm64` + `GOOS=darwin`
  builds and `go vet` per target join the oracle; bound doc citations updated.

### W1-01 — CI quality-instrumentation plane (advisory gates + vuln scanning + dependabot)

- **Area**: scripts-ci / cross-cutting | **Risk**: low | **Protocol/schema**: none | **LOC**: +250…+350 (CI/config; coverage steps moved to W2-17)
- **Owned files**: `.github/workflows/ci.yml`, `daemon/.golangci.yml` (new), `control/.credo.exs` (new),
  `.github/dependabot.yml` (new)
- **Change**: add **non-blocking** `continue-on-error` steps **inside existing jobs — no new jobs** (the
  changed-path classifier suite pins job routing): (a) golangci-lint **v2.15.0** (pinned — ≤2.6.1 fails
  to type-check Go 1.27 export data) run twice (`GOOS=linux`, `GOOS=windows`) from the merged
  `daemon/.golangci.yml` (§10.2) with uncapped counts reported as a metric; publish the per-linter,
  per-package measured baseline as the ratchet; (b) `mix credo --strict` with `control/.credo.exs`
  (§10.1; effective once W1-02 adds the dep); (c) `ruff check` + `ruff format --check` for `scripts/ci`
  (§10.4; config from W1-11); (d) `shellcheck --severity=warning` for `scripts/ci/*.sh` + `actionlint`
  (shellcheck off) for workflows — both on the ubuntu runner, where they terminate (the local
  Windows-host actionlint hang is an environment fact); (e) vulnerability scanning: `mix hex.audit`
  (flips blocking once W1-02 lands clean), `govulncheck ./...` for `GOOS=linux`+`GOOS=windows`,
  `pnpm audit --prod` (optionally Sobelow); (f) `.github/dependabot.yml` for security updates
  (gomod/mix/npm/github-actions). Re-take the Credo strict counts (portable Elixir toolchain) and
  record the baseline in the PR description. Nothing existing becomes blocking.
- **Acceptance**: on a draft PR every new step runs (as `continue-on-error` placeholders until its
  config/dep slice — W1-02/W1-11 — lands) and prints counts; all pre-existing jobs keep their
  current pass/fail semantics and routing.
- **Oracle**: draft-PR CI run (`gh run view --log`); locally `cd daemon && golangci-lint run` (both
  GOOS), `cd control && mix credo --strict`, `ruff check scripts/ci`,
  `python3 scripts/ci/test-ci-changed-path-classifier.py`.
- **Dependencies**: none. Credo/ruff steps tolerate the missing deps until W1-02/W1-11 merge.

### W1-02 — control dependency hygiene: Credo dep + telemetry deps removal + plug bump + multipart drop

- **Area**: control-web (+ control app bootstrap) | **Risk**: low (lock bump: low-medium — full `mix test`) | **Protocol/schema**: none | **LOC**: ≈−80 (+1 dev dep)
- **Owned files**: `control/mix.exs`, `control/mix.lock`, `control/lib/symmetry_control_web/endpoint.ex`,
  `control/lib/symmetry_control_web/telemetry.ex` (delete), `control/lib/symmetry_control/application.ex`
- **Change**: (a) add `{:credo, "~> 1.7", only: [:dev, :test], runtime: false}`; (b) delete the inert
  telemetry scaffolding (`metrics/0`, empty `periodic_measurements/0`, poller child) and drop
  `telemetry_metrics`, `telemetry_poller` from deps; (c) bump `plug` to **1.20.4** (fixes
  EEF-CVE-2026-120287 — verified live) and remove `:multipart` from the endpoint parser list (no client
  posts multipart — statically verified).
- **Acceptance**: `mix hex.audit` reports no advisories; `mix deps.unlock --unused` empty; app boots in
  `mix test`; no `:multipart` parser; telemetry module and its two deps gone.
- **Oracle**: full `mix test` (CI control job; locally: portable Elixir + PostgreSQL),
  `mix compile --warnings-as-errors`, `mix hex.audit`, `mix format --check-formatted`.
- **Dependencies**: none; enables W1-01's credo step.

### W1-03 — control-web Credo strict clean-up (lib subtree)

- **Area**: control-web | **Risk**: low | **Protocol/schema**: none | **LOC**: ≈−40
- **Owned files**: `control/lib/symmetry_control_web/{plugs/auth.ex, controllers/daemon_controller.ex, controllers/goal_controller.ex, plugs/portal_auth.ex, controllers/portal_session_controller.ex, channels/daemon_channel.ex, controllers/health_controller.ex, controllers/task_controller.ex, protocol.ex, user_socket.ex}`
- **Change**: fix the 15 strict-Credo issues in these files (Nesting by extraction, AliasOrder,
  WithSingleClause, ModuleDoc, logger metadata); no suppressions, no behaviour change. Test-file lint
  issues stay out (web test files are owned by W1-13 and W3 batches).
- **Acceptance**: strict run shows zero issues in the 10 files; project count drops by ≥15.
- **Oracle**: `mix credo --strict` (portable toolchain), `mix compile --warnings-as-errors`,
  `mix test test/symmetry_control_web/` (+ full `mix test` in CI).
- **Dependencies**: none for the edits. The oracle's `.credo.exs`/dep come from W1-01/W1-02 — measure
  with the §10.1 config verbatim via a local, uncommitted Credo dep and re-run after they merge.

### W1-04 — daemon-platform god-file split (move-only)

- **Area**: daemon-platform | **Risk**: low | **Protocol/schema**: none | **LOC**: 0
- **Owned files**: `daemon/internal/platform/{linux_owner_anchor_linux.go, linux_owner_helper_v2_linux.go, containment_supervisor_linux.go, containment_supervisor_windows.go}`
  + new same-package siblings (`anchor_{server,run,gate,proc,wire,types}.go`, `helper_v2_{server,protocol,run,stop,recovery}.go`, `supervisor_{linux,windows}_{client,helper}.go`);
  every new sibling carries the source file's exact build constraint (`//go:build linux && amd64` for
  anchor/helper — `_linux.go` alone would silently widen to all arches); citing docs join the owned set
- **Change**: script-generated, re-runnable pure declaration moves by role; zero logic edits; no symbol
  renames (P-06's prerequisite). Update symbol↔path citations bound in docs
  (`check-citation-existence.py` run over the full audited corpus at the slice head).
- **Acceptance**: declaration-multiset AST check (each top-level decl normalised through `go/printer`,
  sorted hashes identical before/after) shows move-only; all target builds green; package tests green;
  gofmt clean.
- **Oracle**: `gofmt -l daemon/internal/platform`; `GOOS=linux go build ./... && GOOS=windows go build ./...`
  + `GOOS=linux GOARCH=arm64 go build ./...` + `GOOS=darwin go build ./...`, `go vet` per target;
  `go test -count=1 ./internal/platform/` (~7 s); CI daemon jobs (race, slow).
- **Note**: merge first in wave 1; `git diff --color-moved` is a reviewer aid, not the proof.
- **Dependencies**: none; blocks W2-11/W2-12/W3-02 (same regions).

### W1-05 — daemon-app: split `app.go` into concern files (move-only)

- **Area**: daemon-app | **Risk**: low | **Protocol/schema**: none | **LOC**: 0
- **Owned files**: `daemon/internal/app/app.go` + new siblings `native_run.go`, `goal_admission.go`,
  `goal_delivery.go`, `commands.go`, `lease.go`, `outbox.go`, `events.go`, `cleanup_glue.go`,
  `recovery_glue.go`, `options.go`, `probe.go`
- **Change**: F1 script-generated, re-runnable move-only split (419 prod functions / 12,048 lines
  across ≥12 concerns). No exported symbol changes. Update symbol↔path citations bound in docs
  (`check-citation-existence.py` over the full audited corpus at the slice head; citing docs join the
  owned set). After this, review of F2/F3/F12/F13 becomes tractable.
- **Acceptance**: declaration-multiset AST check (normalised `go/printer` decls, sorted hashes
  identical) shows move-only; `gofmt -l` clean; `go build ./...`; `go test -count=1 ./internal/app/...` green.
- **Oracle**: `go build ./...` (also `GOOS=linux GOARCH=arm64`, `GOOS=darwin`), `go vet` per target;
  `go test -count=1 ./internal/app/...` (≈150 s local; CI race ≈6.5 min).
- **Note**: merge first in wave 1; `git diff --color-moved` is a reviewer aid, not the proof.
- **Dependencies**: none; unlocks W2-10 and the W4 app splits.

### W1-06 — daemon dead-code sweep (re-scoped: caller-free surfaces only)

- **Area**: daemon-protocol-rest | **Risk**: low | **Protocol/schema**: none | **LOC**: small (the −1.3…−1.6k sweep moved to the follow-up below)
- **Owned files**: `daemon/internal/harness/registry.go`, plus any test file that names a deleted
  surface (none expected). Confirmed remainder: `Registry.ValidateRequired` (zero callers anywhere).
- **Change**: delete only surfaces proven caller-free by the **intersection of three `deadcode` runs —
  GOOS=linux, GOOS=windows, `GOOS=linux GOARCH=arm64` — plus `git grep -nw <sym> -- daemon` with no
  `_test.go` exclusion (covers `e2e/`)**. The originally listed sweep targets are witness/e2e-consumed
  or compile-critical and are deferred (below); the `launchLinuxV2Process` stub stays — deleting it
  breaks `linux/arm64` builds (F-19's `linux && amd64` branch move is a follow-up). No new `//nolint`.
- **Acceptance**: every deleted symbol has zero references on all three targets; `deadcode` re-run
  shows the findings gone; linux/windows/arm64 builds green; suites green.
- **Oracle**: `go install golang.org/x/tools/cmd/deadcode@latest`; deadcode for the three targets;
  `GOOS=linux go build ./... && GOOS=windows go build ./... && GOOS=linux GOARCH=arm64 go build ./...`;
  `go vet ./...`; `go test -count=1 ./internal/harness/... ./internal/control/...`.
- **Deferred (per review — not this slice)**: `NewOperatorClient` + its methods, legacy task validators,
  v1 `Task`/`TaskWaiting` decoders, `WithMaxResponseBytes`, and the root probe paths are consumed by
  `daemon/e2e/**` witnesses and other packages' tests (non-test greps miss them by construction); they
  move to the W2-12 follow-up, which adds `daemon/e2e/**`, `internal/control/*_test.go`,
  `internal/app/harness_test.go` and `internal/harness/*_test.go` to its owned set and states, per
  surface, what replaces the e2e coverage. `NewClaudeAdapterWithRunner` and root probes stay until W3-04.
- **Dependencies**: none. Exclusions: the goal-envelope family goes to decision D5 (separate slice);
  ack stages to W3.

### W1-07 — daemon-app/state hygiene batch (dead stores + preflight escape-parsing bug)

- **Area**: daemon-app | **Risk**: low | **Protocol/schema**: none | **LOC**: ≈−10
- **Owned files**: `daemon/internal/app/recovery.go`, `daemon/internal/app/linux_owner_recovery_linux.go`,
  `daemon/internal/app/linux_owner_preflight_linux.go`,
  `daemon/internal/app/linux_owner_preflight_linux_test.go` (added for the bug test)
- **Change**: dead stores: **delete-only** (removal/`_ =`) at recovery.go:299,
  linux_owner_recovery_linux.go:481, and the `filepath.Join` misuse at preflight:1876-1877; any store
  that is actually a bug (e.g. recovery.go:334, reassigned-then-unused) becomes its own bug slice with
  a regression test, not hygiene. `preflight:1822` is fixed as a bug: `unescapeLinuxMountPath` iterates
  `for i := range len(value)`, so `i += 3` is ineffectual and `\040`-style escapes mis-parse — fix the
  loop and add a table test. Identical-operand assertions are **left alone** (control_failure_test.go:35 /
  supervisory_test.go:73 are deliberate replay assertions — both `handleCommand` calls must stay).
- **Acceptance**: dead stores gone; the `unescapeLinuxMountPath` table test is red on the pre-fix loop;
  suites green.
- **Oracle**: `golangci-lint run ./internal/app/` (ineffassign/staticcheck — `go vet` cannot see these);
  `go test -count=1 ./internal/app/... ./internal/state/...`.
- **Dependencies**: none.

### W1-08 — daemon test enablers: goleak + state parallelism + short-wait folding

- **Area**: daemon-app / tests | **Risk**: low-medium (flake surface; enable on Linux first) | **Protocol/schema**: none | **LOC**: +60 test code
- **Owned files**: `daemon/go.mod`, `daemon/go.sum`, `daemon/internal/app/app_test.go` (5 ad-hoc wait
  sites: :5620, :5764, :6946, :7129; only wave-1 `go.mod` owner),
  `daemon/internal/app/native_settlement_regression_test.go` (:1392), `daemon/internal/state/*_test.go`
  (parallel-safe subset only)
- **Change**: add `go.uber.org/goleak` as **per-test `defer goleak.VerifyNone(t, …)` in a named set of
  tests that are leak-free today** — no package `TestMain` yet (F15's `writeInputBounded` strand,
  app.go:7240-7274, would fail it; the package-level TestMain waits for that fix, and any starting
  ignore list must name a TODO per entry — no wildcard ignores). Fold the 5 short pinned waits into
  `testHangGuard`-style waits (30 s). Add `t.Parallel()` only for state tests that use `t.TempDir()`
  alone — keep serial everything touching package seams (e.g. `applyDirectorySecurity`/`applyFileSecurity`,
  `renameStateFileOS`) and all `t.Setenv` files.
- **Acceptance**: state package wall time ≈45 s → ≤25 s on the Linux runner; the per-test goleak set
  catches a planted goroutine leak (red shown); CI daemon jobs green under race.
- **Oracle**: `go test -count=1 -race ./internal/state/... ./internal/app/...` (both runners); CI
  `go test -race ./...` (600 s step — the slow oracle); planted-leak mutation check.
- **Dependencies**: none.

### W1-09 — contracts single-pass decode

- **Area**: daemon-protocol-rest | **Risk**: low | **Protocol/schema**: none | **LOC**: ≈−30…−60
- **Owned files**: `daemon/internal/contracts/schema.go` (+ `schema_test.go`)
- **Change**: F-08 — stop decoding/scanning the same bytes 4-6×: decode once, validate the decoded
  instance (or reuse the raw scan), unmarshal the typed DTO from the same bytes.
- **Acceptance**: contract + protocol transport tests green; benchmark shows one traversal removed per envelope.
- **Oracle**: `go test ./internal/contracts/`; protocol transport tests; CI race (daemon job).
- **Dependencies**: none.

### W1-10 — frontend static-quality baseline: strict ESLint + dead-surface trim + coverage config

- **Area**: frontend | **Risk**: low | **Protocol/schema**: none | **LOC**: ≈−40…−80 (+1 devDep)
- **Owned files**: `frontend/eslint.config.js`, `frontend/package.json`, root `pnpm-lock.yaml`
  (`frontend/pnpm-lock.yaml` does not exist — the workspace root lockfile is shared with `contracts`
  and `browser`; no other wave-1 slice owns it), `frontend/vitest.config.ts`,
  `frontend/src/**` (autofix + dead-export deletions), `frontend/knip.json` (new)
- **Change**: adopt `strictTypeChecked` + `stylisticTypeChecked` + react-hooks `recommended-latest`;
  `eslint --fix` the 515 mechanical findings **per rule in separate commits**, reviewing hunks of the
  type-trusting fixers (`no-unnecessary-type-assertion`, `no-unnecessary-boolean-literal-compare`,
  `non-nullable-type-assertion-style`) under `src/services/**` and decode modules — served-JSON values
  keep their runtime defensive checks; downgrade the residual non-fixable rules to warnings with a
  `--max-warnings` ratchet in `lint`; add `@vitest/coverage-v8` + coverage config (enables the W2-17
  coverage plane); set a suite-level `testTimeout` in `vitest.config.ts` so the base-red
  `tool-cache-config.test.ts:44` passes; delete the 3 proven-dead exports and un-export internals; add
  knip config (<HOST-PATH>-free, gate wiring later).
- **Acceptance**: `pnpm lint` exits 0 at the recorded warning ceiling; typecheck/test/build green
  (509 tests, no failure — the former base failure is fixed by the `testTimeout`); knip runs.
- **Oracle**: `pnpm --filter @symmetry/frontend {typecheck,lint,test,build}` (test ≈114 s cold).
- **Dependencies**: none. This is the only wave-1 slice owning `frontend/src/**`.

### W1-11 — scripts/ci ruff baseline (config + format + autofix)

- **Area**: scripts-ci | **Risk**: low | **Protocol/schema**: none | **LOC**: format churn; ≈−20 net
- **Owned files**: `pyproject.toml` (new), `scripts/ci/*.py` (51 files) — **excluding
  `model-routing-record-sanitize.py` and its test until PR #137 (`fix/sanitizer-json-escape`) lands**;
  rebase on main immediately before merge
- **Change**: add the reconciled ruff config (§10.4); `ruff format` (50/51 files) + `ruff check --fix`
  (42 default fixes); fix remaining default-set findings to zero.
- **Acceptance**: `ruff format --check scripts/ci` clean; `ruff check scripts/ci` 0 under the config;
  all 24 `test-*.py` suites green.
- **Oracle**: `ruff format --check scripts/ci`; `ruff check scripts/ci`; `python3 scripts/ci/test-*.py`
  (24 suites, seconds); CI python steps.
- **Dependencies**: none; W1-01's ruff step uses this config (tolerates its absence).

### W1-12 — Elixir fixture extraction batch 1 (control tests)

- **Area**: tests-cross-cutting | **Risk**: low | **Protocol/schema**: none | **LOC**: ≈−700…−900
- **Owned files**: `control/test/support/test_fixtures.ex`, `control/test/support/orchestration_case.ex` (new);
  `control/test/symmetry_control/orchestration*_test.exs`, `control/test/symmetry_control/orchestration/**`,
  `workspaces_*_test.exs`, `goals_test.exs`, `goals/{dispatch_notification,workers}_test.exs`,
  `migrations/*` (convert 2-3 files per commit)
- **Change**: T1 — add the data builders + `ExUnit.CaseTemplate`; convert the duplicated helpers to
  imports (`register_runtime` ×11, `claim` ×9, `command_current` ×7, `task_attrs` ×5; `with_schema` ×7 —
  **reuse the existing `MigrationCase`** rather than adding a new copy).
- **Acceptance**: full `mix test` green with identical test names; one definition per helper **within
  the owned set**; the web/e2e copies are listed for W3-03; no web-controller test files touched
  (`provider_broker_e2e_test.exs` stays out — W1-14's oracle).
- **Oracle**: full `mix test` (CI control job, ≈102 s); per-file spot runs.
- **Dependencies**: none.

### W1-13 — control-web served-document gates (4 ungated reads)

- **Area**: control-web / tests | **Risk**: low | **Protocol/schema**: none (test-only) | **LOC**: +60…+100
- **Owned files**: `control/test/symmetry_control_web/controllers/{portal_resources_test.exs, portal_workflow_test.exs, portal_integrations_test.exs}`;
  small fixture additions pinned to these three files
- **Change**: CW-13 — serve workspace-read, work-detail-read, work-timeline-read, connections-read
  through the real controllers and validate with the existing `validate_served_document!/2`; each new
  gate additionally **asserts presence of the consumed members** (workspace-read list at
  `docs/TODO.md:206-221`; derive the others with the same `jq`-over-`required` walk + a `frontend/src`
  read-site grep) — this implements TODO:190 option (b); move that entry to `docs/archive/CLOSED-TODO.md`
  when it lands.
- **Acceptance**: a throwaway rename of an **optional consumed** member makes a new gate fail red (the
  target schemas are permissive: `additionalProperties: true` throughout, zero `false`); suites green.
- **Oracle**: `mix test test/symmetry_control_web/controllers/portal_resources_test.exs test/symmetry_control_web/controllers/portal_workflow_test.exs test/symmetry_control_web/controllers/portal_integrations_test.exs`;
  mutation check; `pnpm contracts:check`.
- **Dependencies**: must merge before W2-03 (controller consolidation) and W2-04 (portal_json) so those
  are covered. Contingency: if a gate is red at base, the fix surface is `portal_json.ex` — coordinate
  with W2-04's owner.

### W1-14 — control-core dead-surface deletion (F11)

- **Area**: control-core | **Risk**: low | **Protocol/schema**: none | **LOC**: ≈−40
- **Owned files**: `control/lib/symmetry_control/integrations.ex`,
  `control/lib/symmetry_control/integrations/provider_access.ex`,
  `control/lib/symmetry_control/goals/contract_validation.ex` (+ their tests)
- **Change**: delete `execute_accepted_resource_sync/2` + `record_provider_action_failure/2`, the
  `validate_adapter/2` alias and the unreachable 3-tuple `provider_failure/1` clause (F11; revalidated
  at a3039f0, satisfies the open TODO entry).
- **Acceptance**: warnings-as-errors clean; `mix xref unreachable` drops toward 0 (was 26 lines);
  provider suites green.
- **Oracle**: `mix compile --warnings-as-errors`; `mix xref unreachable`;
  `mix test test/symmetry_control/integrations*` + `provider_broker_e2e_test.exs`.
- **Dependencies**: none.

### Wave-1 conflict audit (asserted, per the brief)

| Hot/shared file | Wave-1 owner | Other wave-1 slices touching it |
|---|---|---|
| `.github/workflows/ci.yml` | W1-01 | none |
| `control/mix.exs` / `mix.lock` | W1-02 | none |
| `daemon/go.mod` / `go.sum` | W1-08 | none |
| `frontend/package.json` / root `pnpm-lock.yaml` | W1-10 | none |
| `daemon/internal/app/app.go` | W1-05 | none (W1-07 touches other app files; W1-08 test files only) |
| `control/test/**` | W1-12 | W1-13 (web controller tests only — disjoint paths); W1-03 (lib only) |
| `daemon/internal/platform/**` | W1-04 | none |
| `daemon/internal/{protocol,control,harness,execution}` (re-scoped dead-code set) | W1-06 | none |
| `scripts/ci/*.py` | W1-11 | none |
| `frontend/src/**` | W1-10 | none |

`docs/TODO.md` / `docs/archive/CLOSED-TODO.md` updates may be made by any slice (merge-driver handled,
ignored here).

---

## 5. Wave 2 — abstraction/dedup I (17 slices)

**Note (re-derive owned sets):** wave-2+ owned file sets must be re-derived after the W1-04/W1-05
move-only splits land — rows naming `app.go` regions (W2-10, W3-09, W4-01) and the pre-split platform
filenames (W2-11, W3-02, W4-02, W5-01, W5-09) must be re-mapped onto the new sibling files, and the
one-owner-per-wave accounting re-run, before that wave is dispatched.

Columns per slice: **id / slice / area / owned files / change · acceptance · oracle / risk /
protocol flag / LOC delta / dependencies**. Hot-file owners this wave: `ci.yml` to W2-01, then W2-17
(serialized, coverage jobs only), `goals.ex` to W2-07, `state.go` to W2-09, `app.go` to W2-10,
`authority.go` to W2-14.

| id | slice | area | owned files | change · acceptance · oracle | risk | proto | dLOC | deps |
|---|---|---|---|---|---|---|---|---|
| W2-01 | CI test observability: durations + browser-suite inventory | scripts-ci / tests | `.github/workflows/ci.yml`, `scripts/ci/go-test-durations.py` (new), `scripts/ci/assert-browser-suite-coverage.py` (new), `browser/README.md` | `mix test --slowest 20`; feed `go test -json` into a duration summarizer (fail >30 s/test or p95 +20 % vs committed baseline); inventory gate: every `browser/tests/*.spec.mjs` and `browser/scripts/*.test.mjs` must be named by a workflow step or listed in the README manual matrix. Accept: green run prints the table; planted 40 s test + unlisted spec both fail red. Oracle: CI run; synthetic fixtures for both scripts. Slow: full CI. | low | none | +150 | W1-01 |
| W2-02 | Strict JSON single-pass decoder (CW-1) | control-web | `..._web/plugs/strict_evidence_json.ex`, `endpoint.ex`, `strict_evidence_json_test.exs` | One `StrictJSONDecoder` passed as `json_decoder:` MFA (ordered decode + duplicate-key rejection in one pass); delete `read_json_body/3` and the strict-parser plumbing; add empty-body/array/`+json` cases. Accept: duplicate-key rejection unchanged; ~2.3x to ~1x JSON work per mutation. Oracle: `strict_evidence_json_test.exs` + `protocol_controller_test.exs` + full `mix test`. | med | none (rejections preserved) | -70 | W1-02, W1-13 |
| W2-03 | Controller plumbing consolidation (CW-2/3/4/5/6/7/10) | control-web | `plugs/auth.ex`, `router.ex`, `goal_controller.ex`, `chat_controller.ex`, `portal_api_controller.ex`, `task_controller.ex`, `daemon_controller.ex`, `query_params.ex`, `protocol.ex` + controller tests | One `CredentialAuth` plug (3 pipelines), one `action_receipt/3`, one validation-error envelope, `QueryParams` integer family with explicit bounds, one fence spec, explicit `:surface` assign. Accept: controller suites byte-identical; new assertion that the operator API never serves the portal projection; 401/403 mapping unchanged. Oracle: `protocol_controller_test.exs`, `provider_action_controller_test.exs`, `operator_read_controller_test.exs`, `portal_*`, `router_invalidation_coverage_test.exs`; full `mix test`. | low-med | none | -250 | W1-03, W1-13 |
| W2-04 | `PortalJSON` consolidation (CW-8a/8b, CW-15) | control-web | `portal_json.ex`, portal/attention tests | Keyed evidence getter replaces the 8-clause `value/2`; delivery as field descriptors + one resolver keeping provider/manual/agent/none precedence and every served member; optional URL-allowlist table. Accept: served members identical (W1-13 gates + fixtures). Oracle: `portal_resources_test.exs`, `portal_workflow_test.exs`, `portal_integrations_test.exs`, `portal_goal_attention_test.exs`, `pnpm contracts:check`. | low / med (8b) | none | -45 | W1-13 |
| W2-05 | Goal schema validators (CCS1) | control-core | `goals/schemas.ex`, `goals/changeset_validators.ex` (new), `goals_schema_test.exs`, `goals_test.exs` | Shared validators (`validate_digest` x6, `validate_map` x4, parameterized `reject_changes`, `immutable_changeset` cast-list kept); byte-identical messages. Accept: schema/migration suites green; duplicate-def count drops. Oracle: `goals_schema_test.exs`, `goals_test.exs`, migration tests (full `mix test`). | low | none | -120..-150 | none |
| W2-06 | Provider shared support (CCS2) | control-core | `integrations/providers/{github,azure_devops}.ex`, `providers/support.ex` (new), provider tests | Extract the 19 byte-identical helpers + pagination/ref/URL shaping; keep per-provider URL/body mapping and error atoms. Accept: both provider suites green; parity pinned. Oracle: `integrations/github_test.exs`, `integrations/azure_devops_test.exs`, `change_action_test.exs`. | low | none | -80..-120 | none |
| W2-07 | Mutation-id + versioned-changeset helpers (CCS3) | control-core | `goals.ex` (mutation-id region), `integrations.ex`, `workspaces.ex`, `schemas/versioned_changeset.ex` (new), related tests | One `mutation_id/2` for the 6 settlement ids (byte-identical output); one optimistic-locking helper set for the 409/stale contract. Accept: ids and 409 bodies byte-identical. Oracle: `goals_test.exs` settlement/usage tests, `integrations_test.exs`, `workspaces_test.exs`, `orchestration_review_test.exs`. | low | none | -120 | W1-14 |
| W2-08 | Read-model projection context (CCS4) | control-core | `goals/read_model.ex`, `goals_read_model_test.exs` | One `%Projection{}` threaded through `project/2`; `blockers/20` to `blockers/1`. Accept: block/attention output unchanged. Oracle: `goals_read_model_test.exs` (50 tests), full `mix test`. | low-med | none | -100 | none |
| W2-09 | State validator section helpers (DA S2) | daemon-app | `state/state.go`, `state/goal_session.go`, `state/provider_action.go` + tests | Extract per-section validators in the same order with identical first-error strings. Accept: top-level validators <=40 lines / CC<=25 each; suites green. Oracle: `go test -count=1 ./internal/state/...`; CI race (slow). | low | none | ~0 | W1-07 |
| W2-10 | Callback factory dedup (DA S3) | daemon-app | `app.go` (callback regions), app tests | Generic store-callback helper kills the mark/clear twins and 4 dupl blocks (dupl@100). Accept: dupl pair count drops; app suite green. Oracle: `go test -count=1 ./internal/app/...`; CI race. | low | none | -60 | W1-05 |
| W2-11 | Typed error identities (DP S-3) | daemon-platform | `linux_owner_anchor_linux.go`, `linux_owner_helper_v2_linux.go`, `containment_errors.go` + tests | Sentinels for the repeated mismatch classes; delete `errOr` at identification sites; convert text-matching tests to `errors.Is`. Accept: callers distinguish identity-change vs transport loss; `errorlint` 0 for these files. Oracle: `go test ./internal/platform/...` both GOOS; `execution`/`app` suites. | low | none | -120 | W1-04 |
| W2-12 | Wire primitives + strict-decode parity (DR S3) | daemon-protocol-rest | `protocol/goal_subject.go`, `protocol/protocol.go`, `config/config.go`, `control/validation.go`, `control/client.go` + tests | One validator family, exported/delegated; fix `validateProviderActionUUID` (version/variant); add duplicate-member rejection client-side. **W1-06 follow-up:** the deferred daemon dead-code surfaces land here — owned set adds `daemon/e2e/**`, `internal/control/*_test.go`, `internal/app/harness_test.go`, `internal/harness/*_test.go`, with a per-surface statement of what replaces the e2e coverage (never delete witness code). Accept: new rejections visible; suites green. Oracle: `internal/control/*_test.go`, `config_test.go`, `protocol/goal_*_test.go`, full `go test -count=1 ./...`. | low-med | strictness only | -280 | W1-06 |
| W2-13 | Harness shared session scaffolding 5a (DR S5a) | daemon-protocol-rest | `internal/harness` root helpers + adapter files (`pi`, `codex`, `omo`, `omp`, `claude`, `opencode`) + tests | `processForwarder`, close-gate, wait barrier, emit sink wrapper, nil-process helper, result clone, bounded JSONL decoder. Accept: fixture replays + native acceptance green; ~-500 non-test LOC. Oracle: per-adapter `adapter_test.go`, `probeconformance`, fixture replays, `go test ./internal/harness/...`. | low | none | -500 | none |
| W2-14 | authority.go split + clone/equal unification (DR S8) | daemon-protocol-rest | `authority/authority.go` -> `clone.go`/`cgroup.go`/`supervisor.go`, tests | Split by family; one `supervisorCore` for clone/equal/validFor/conversions; JSON field order/tags literal. Accept: durable bytes identical. Oracle: `authority_test.go`, `authority_v2_test.go`, `authority_linux_clone_test.go`. | low-med | none (durable bytes literal) | -150..-200 | none |
| W2-15 | Served-value helper module (FE S2) | frontend | `work/presentation.ts`, `chat/presentation.ts`, `connections/resources.tsx`, `goals/served-text.ts` + tests | One `services/served.ts` (`asText/asRecord/safeUrl/failureText`); delete the 4 drifting copies. Accept: labelled-unknown rendering unchanged. Oracle: `work/presentation.test.ts`, `chat/presentation.test.ts`, resources DOM tests, `pnpm typecheck`. | low | none | -80..-120 | W1-10 |
| W2-16 | Clone-aliasing reflection test (DP S-9) | daemon-platform | `clone_aliasing_test.go` (new) | Reflection test: fully populate each evidence struct with pointer/slice/map fields, clone, mutate original, assert clone independent. Accept: a new uncovered field fails red; no production change. Oracle: `go test ./internal/platform/...`. | low | none | +80 test | W1-04 |
| W2-17 | Coverage plane (moved from W1-01; D14) | scripts-ci / cross-cutting | `.github/workflows/ci.yml` (serialized after W2-01), `control/mix.exs` (`test_coverage: [summary: [threshold: 0]]`), `frontend/vitest.config.ts`, `scripts/ci/test-ci-changed-path-classifier.py` | New `continue-on-error: true` coverage jobs gated `needs: changes` + `if: needs.changes.outputs.run-heavy == 'true'`: `mix test --cover` (threshold 0), a **separate** `go test -coverprofile` job (the existing race/`mix test` steps stay untouched — they sit at the 600 s package timeout), `vitest --coverage` (provider dep from W1-10); upload artifacts; floors stay advisory. Accept: draft-PR run shows the three artifacts; pre-existing jobs keep their semantics; the classifier suite passes with the added jobs (update its fixtures in the same PR). Oracle: draft-PR CI (`gh run view --log`); `python3 scripts/ci/test-ci-changed-path-classifier.py`. | low | none | ~+120 CI | W1-02, W1-10, W2-01 |

## 6. Wave 3 — abstraction/dedup II + medium-risk (16 slices)

Hot-file owners this wave: `ci.yml` to W3-01, `goals.ex` to W3-10, `state.go` to W3-07.

| id | slice | area | owned files | change · acceptance · oracle | risk | proto | dLOC | deps |
|---|---|---|---|---|---|---|---|---|
| W3-01 | Changed-path classifier extraction + corpus single source (SC S2) | scripts-ci | `ci.yml:50-98`, `scripts/ci/classify-changed-paths.py` (new), `test-ci-changed-path-classifier.py`, `check-citation-existence.py` (import) | Move the heredoc program out of YAML; one `AUDITED_PATHS` source; the test runs the real file. Accept: all routing/rename/delete fixture cases pass; one real CI run routes identically. Oracle: `python3 scripts/ci/test-ci-changed-path-classifier.py`; CI run. | med | none | -170 | W2-01 |
| W3-02 | Response builder + request-shape table (DP S-4) | daemon-platform | `linux_owner_anchor_linux.go`, `linux_owner_helper_v2_linux.go` + tests | One declarative per-operation contract table (required/forbidden fields, FD counts, timeout) used by client, server and helper; one `writeResponse(conn, response)`; delete the 8 anchor variants. Accept: per-operation accept/reject sets byte-identical (new table test); wire JSON identical. Oracle: package protocol tests, `go test ./internal/execution -run Linux`, v2 witness jobs. | low-med | none (field sets identical) | -400..-500 | W1-04, W2-11 |
| W3-03 | Elixir fixture extraction batch 2 (web + integrations) (T1 cont.) | tests | `control/test/symmetry_control_web/**` fixture conversions, integration fixture files | Continue the fixture extraction into web/integration tests; delete local copies. Accept: full `mix test` green; no local fixture clones remain outside support modules. Oracle: full `mix test` (CI control job). | low | none | -300 | W1-12, W1-13 |
| W3-04 | Probe/registry dedup (DR S6) | daemon-protocol-rest | `harness/registry.go`, `harness/*/probe.go`, probe tests | One `RunBoundedProbe[T]` + `ProbeRunner`/`DigestResolver`; root codex/claude probes delegate to the subpackages (keeps the e2e witness binary honest). Accept: probe conformance green; ~-350..-400 LOC. Oracle: `codex_probe_test.go`, `claude_probe_test.go`, `harness_test.go`, `probeconformance`. | low | none | -350..-400 | W1-06 |
| W3-05 | Notification + fake-agent error/goroutine fixes (DR S11) | daemon-protocol-rest | `notification/client.go`, `cmd/symmetry-fake-agent/autonomous.go` + tests | Surface reconnect errors (log/metric) instead of discarding; give the fake-agent reader an exit path (`select` on done / close stdin). Accept: budgets unchanged; no unowned goroutine. Oracle: `notification/client_test.go` (budget tests), `autonomous_test.go`, `main_test.go`. | low | none | ~+30 | none |
| W3-06 | Witness-writer error propagation (DR S12) | daemon-protocol-rest | `cmd/symmetry-daemon/preauthority_witness_command_windows.go` + tests | Return errors from the report writer; fatal vs best-effort decided by the caller; crash-hook codes preserved. Accept: a write failure cannot produce a green witness (mutation check). Oracle: `cmd/symmetry-daemon/main_test.go`, e2e witness matrix. | low-med | none | ~+20 | none |
| W3-07 | Error-cause wrapping + capacity split (DA S4) | daemon-app | `state/state.go`, `state/provider_action.go` + capacity tests | `%w`-wrap static errors; split serialize-error vs capacity-error paths. Accept: diagnosable causes; `ErrProviderActionCapacity` semantics unchanged; text-asserting tests updated deliberately. Oracle: `state_test.go`, `provider_action_test.go` (capacity), `linux_clone_journal_test.go`; CI race. | low | none | ~+40 | W2-09 |
| W3-08 | Unexport unused surface + test-only options (DA S8) | daemon-app | `state/*.go` (type declarations), `app/export_test.go` + tests | Unexport 10 types with no out-of-package refs; move test-only `With*` options into `export_test.go`. Accept: compiler-enforced boundary; build + both test packages green. Oracle: `go build ./...`, `go test ./internal/state/... ./internal/app/...`. | low | none | -10 | W2-09 |
| W3-09 | Errcheck triage part 1 — app.go recovery paths (DA S7a) | daemon-app | `app.go` (abandonGoalSession / Close sites) | Defer-close with stated reason where unactionable; `errors.Join` into returned errors where recovery meaning exists. Accept: no silent loss on abandon paths; errcheck prod count drops. Oracle: `native_settlement_*`, `goal_delivery_test.go`, `omo_cleanup_test.go`, `process_cleanup_test.go`; CI race. | low-med | none | ~+30 | W2-10 |
| W3-10 | Result dispatch flattening (CCS5) | control-core | `goals.ex` (dispatch regions), related tests | Replace the 12-clause unused-parameter mega-lists with a `case` over live inputs; settlement semantics unchanged. Accept: settlement/terminal-result tests green. Oracle: `goals_test.exs`, `orchestration_review_test.exs`, `orchestration_supervision_test.exs`. | med | none | -100..-150 | W2-07 |
| W3-11 | Narrow HTTP rescue (CCS9b) | control-core | `integrations/http.ex`, provider tests | Move body encoding outside the whole-function rescue; keep the exception-to-transport mapping around the socket call only (Req migration stays optional/later). Accept: providers green; programming errors no longer classified `transport`. Oracle: `github_test.exs`, `azure_devops_test.exs`. | low | none | ~0 | none |
| W3-12 | UI primitives + resource-dialog collapse (FE S4+S9) | frontend | `components/*`, `connections/resources.tsx`, `planning/editors.tsx`, `app/shell.tsx` + tests | Single `StatusBadge`/`FieldErrors`; one `useDialogOpener`; three resource dialogs to one `ResourceDialog` discriminated union; shared editor field grid. Accept: DOM tests green with pinned selectors/aria names (keep data-* hooks). Oracle: `connections.test.tsx` (1,519 L), `planning.test.tsx`, `control.test.tsx`, full `pnpm test`. | low-med | none | -150..-250 | W2-15 |
| W3-13 | HTTP boundary unify (FE S5) | frontend | `services/api.ts`, `services/commands.ts`, `goals/detail.tsx`, `attention.tsx` + tests | Route command POSTs through the shared `request()` boundary; delete `postPortalCommand`/`CommandErrorBody`/`refusal`; one fatal body decode. Accept: session-expiry/refusal semantics preserved exactly (tests pin them). Oracle: `api.test.ts` refusal/redirect cases, `goal-attention.test.tsx`, `application.test.ts` retry classification. | med | none | -120..-180 | W1-10 |
| W3-14 | ExUnit async conversion (TC-E) | tests | `control/test/test_helper.exs`, `control/config/test.exs`, ~20 DB-only test files, `migrations/*` | Convert DB-only files to `async: true` (per-test sandbox owner mode); fold migration scaffolding; raise `max_cases`. Accept: `mix test` ~102 s to <=65 s; stable under 3 seeds; new flakes treated as findings. Oracle: full `mix test` x3 seeds (CI control job). | low-med | none | ~0 | W3-03 |
| W3-15 | Giant-test split (TC-H) | tests | `daemon/internal/app/app_test.go`, `harness/codex/adapter_test.go`, `state/state_test.go`, `harness/opencode/adapter_native_work_test.go`, `state/linux_cgroup_v2_test.go`, `e2e/production_linux_v2_cleanup_witness_test.go`, `pi_control_e2e_test.exs`, `goals_test.exs`, `migrations/goal_0006_migration_test.exs` | Split the >300-line units into per-scenario functions/subtests, preserving every subtest name (CI `--assert` gates). Accept: `go test -run '<old names>'` and `mix test --only` green; CI named-assertion gates unchanged. Oracle: affected suites + CI daemon/control jobs. | low | none | ~0 | W1-08, W3-14 |
| W3-16 | scripts/ci small defects from ruff/shellcheck (SC S8) | scripts-ci | `export-linux-v2-redacted-evidence.py`, `test-check-citation-existence.py`, `test-model-routing-evaluation.py`, `test-omo-native-evidence.py`, `opencode-v2-support-witness.py`, `verify-linux-v2-helper-exit-checkpoint.py`, `assert-go-test-json.py`, `test-check-lockfile-registry.py` | Fix unused locals/imports, loop-variable closure captures (B023/B020), and the silently-skipping PyYAML oracle (pin/install or explicit visible skip). Accept: affected suites green; loop captures explicit. Oracle: the 8 affected `python3 scripts/ci/test-*.py` suites. | low | none | ~-30 | W1-11 |

## 7. Wave 4 — structural refactors (16 slices)

Hot-file owners this wave: `app.go` to W4-01, `runner.go` to W4-03, `frontend/package.json` to W4-11.

| id | slice | area | owned files | change · acceptance · oracle | risk | proto | dLOC | deps |
|---|---|---|---|---|---|---|---|---|
| W4-01 | `startGoalAdmission` phase split (DA S13) | daemon-app | `app.go` (goal admission region), `goal_admission.go` | Extract 5 ordered phase methods with explicit inputs/outputs; keep the single defer compensation chain and durable-write order; top function <=150 L / CC~30. Accept: native settlement + admission + race suites green; no ordering change (reviewed diff). Oracle: `app_test.go`, `native_settlement_*`, `harness_test.go`, `goal_delivery_test.go`, `linux_owner_v2_app_test.go`; CI race (slow). | high | none | -60..-100 | W1-05, W2-10, W3-09 |
| W4-02 | Session objects for supervisor/anchor loops (DP S-5) | daemon-platform | `containment_supervisor_linux.go`, `linux_owner_anchor_linux.go` | `linuxSupervisorSession` / `anchorConnection` own the 11-12 params + local flags; per-operation handlers replace `serveConnection`'s 493 lines; validation order preserved exactly. Accept: package + witness suites green; no naked returns. Oracle: `go test ./internal/platform/...` both GOOS; v2 witness jobs; CI race. | med-high | none | -300..-600 | W3-02 |
| W4-03 | Execution `Start`/`closeContainmentAttempt` decomposition (DR S7) | daemon-protocol-rest | `execution/runner.go` | Named phase functions for Start (persist-commit-lease-arming-resume-input; `startPipes` value with one `Close()`); shared `failContainment`/`persistReceiptOrAbort`/`closeOwnerOnce` for both close paths; exact text/join order kept. Accept: lifecycle/termination/receipt suites green. Oracle: `runner_lifecycle_test.go`, `runner_*`, `linux_owner_v2_test.go`, `runner_stop_receipt_p1_test.go`; CI race. | med-high | none | -200..-300 | W1-06 |
| W4-04 | Workspace walkers/subject guards (DR S10) | daemon-protocol-rest | `workspace/linux_clone_fs_linux.go`, `workspace/subject.go`, `workspace/workspace.go`, `workspace/fingerprint.go` + tests | `openLinuxCloneDirAtFlags` + a walk helper with policy callbacks (trusted vs owned); one `materializeAdmittedSubject`; `requireManaged()` guards. `RESOLVE_BENEATH|NO_SYMLINKS|NO_MAGICLINKS|NO_XDEV`, `O_CLOEXEC|O_NOFOLLOW`, S_IFDIR checks byte-identical. Accept: parity tests green; error strings unchanged. Oracle: `linux_clone_linux_test.go`, subject/workspace/fingerprint tests; CI race Linux. | med-high | none | -130..-175 | none |
| W4-05 | Linux v2 finalize pipeline unification (DR S9) | daemon-protocol-rest | `execution/linux_owner_v2_linux.go` | One persist-ack-remove-clear-release-retire pipeline with injected promotion hop; one `retryMutation` honoring the ack-lost no-replay policy; keep "never release before clear", mirror-stop ack skip, receipt Equal-reconcile. **D6 note:** before shrinking the ack-stage table, prove no persisted v2 journal/receipt can carry a removed stage name — otherwise keep decode tolerance for removed names. Accept: each fence invariant asserted green. Oracle: `linux_owner_v2_test.go`, `runner_stop_receipt_p1_test.go`, witness jobs; CI race. | high | local-protocol/evidence (D6) | -150..-260 | W4-03 |
| W4-06 | provider_access intent-context unification (CCS8) | control-core | `integrations/provider_access.ex` + tests | One `IntentContext` builder + legacy/goal scope tag; lock ordering (data.md) encoded once; thin legacy entry points. Accept: broker/claim/lock tests green; advisory-lock order unchanged. Oracle: `provider_broker_e2e_test.exs`, `integrations/syncer_test`, provider-access claim tests, `resource_sync_*`. | med-high | none | -150..-250 | W2-06 |
| W4-07 | `goals.ex` facade split phase 1 (CCS7) | control-core | `goals.ex` -> `Goals.MutationId`, `Goals.Payload`, `Goals.ContextSnapshot` modules | Move pure regions behind delegating facade functions (public API unchanged). Accept: full `mix test` green; `mix xref` clean. Oracle: `goals_test.exs` (146), `orchestration_goal_admission_test` (77), `goals_read_model_test` (50); full `mix test`. | med | none | ~0 | W2-07, W3-10 |
| W4-08 | ChatPage decomposition (FE S7) | frontend | `features/chat/chat.tsx` + new siblings | 3 hooks (`useChatScopeState`, `useChatScrollRestoration`, `useOlderPageLoading`) + 4 sections; `ChatPage` wiring only; cx 80 to <20 per unit. Accept: `chat.test.tsx` (2,087 L) green unchanged. Oracle: `chat.test.tsx`, `presentation.test.ts`, full `pnpm test`. | med | none | -450 in file | none |
| W4-09 | Api reads/writes split (FE S6) | frontend | `services/api.ts`, `app/application.ts`, 6 feature files + tests | `Reads` (signal required) vs `Writes` (no signal) or a real bounded write policy; delete 24 throwaway `AbortController` constructions. Accept: call-shape tests updated deliberately; no ceremonial signal remains. Oracle: `api.test.ts`, DOM write tests, `pnpm typecheck`. | low-med | none | -24 sites | W3-13 |
| W4-10 | Refusal/outcome classification module (FE S3) | frontend | `chat.tsx`, `goals/detail.tsx`, `planning/write-policy.ts`, `connections/resources.tsx`, `work/assignment.tsx` + tests | One `services/refusals.ts` (`isDefinitive/isUnknownOutcome/isStale`, one `refusalNotice`, one guidance map + per-surface extensions). Accept: rendered refusal sentences unchanged. Oracle: `chat/presentation.test.ts`, `work/receipts.test.ts`, `planning.test.tsx` refusal cases. | low-med | none | -60..-100 | W3-12 |
| W4-11 | Frontend test-harness consolidation + MSW + vitest pool (FE S10, D13) | frontend | all `*.test.*`, `app/test-harness.tsx`, `api.fixtures.ts`, `vitest.config.ts`, `frontend/package.json` (wave-4 owner) | One in-memory portal double (MSW handlers) replacing 4 private doubles; `isolate:false` / pool split; `scrollTo` stub kills 348 jsdom noise lines. Accept: 509 tests green with identical names; wall time -30..-50 %. Oracle: full `pnpm test` (target <80 s from ~114 s), per-file durations compared. | med | none | -400..-600 test | W3-12 |
| W4-12 | Go test-double extraction part 1 (TC-G) | tests | `daemon/internal/app/apptest` (new pkg), `daemon/internal/app/*_test.go` (file groups) | Extract the configurable control client / recording sink / fake process doubles; per-file fakes become options; one focused unit test per shared double. Accept: app suite green; duplicated method count drops; extraction in two commits. Oracle: `go test -count=1 ./internal/app/...`, CI race (slow). | med | none | -400 test | W3-15 |
| W4-13 | Wall-clock burn-down part 1 — Go deadlines (TC-F) | tests | `daemon/internal/app/omo_attach_ack_test.go`, `output_settlement_test.go`, `harness/*_test.go`, `harness/codex/*_test.go`, `harness/probeconformance/*`, `e2e/*` (bounded polls only) | `testing/synctest` / fake clocks for the 20 >=4.9 s tests (start with the 5.00 s probe cluster + the 4x2 s attach deadlines); bounded poll-with-predicate in e2e. Accept: each converted test still fails on a deadline-handling mutation; race-suite floor -40..-90 s. Oracle: `go test -race ./...`; mutation checks per conversion. | med | none | -150 test | W4-12 |
| W4-14 | Browser fixture consolidation (TC-I) | tests | `browser/tests/readable-fixtures.mjs`, `goal-context-fixtures.mjs`, `portal-cutover.spec.mjs`, `portal-goal-attention.spec.mjs`, `portal-connections.spec.mjs`, `workspace-shell.spec.mjs`, `portal-compose-{daemon,restart}.spec.mjs` | Move the plan-proposal builder, `executionPolicy()`, `recordHistoryWrites()`, `capture()`, `deviceContextOptions()` into shared modules; delete local `login`/`api` copies. Accept: specs green (CI-run + wired set); ~-250 LOC. Oracle: `npx playwright test` render-gate specs; the W5-03 wired jobs. | low | none | -200..-250 | none |
| W4-15 | Harness staged driver 5b (DR S5b) | daemon-protocol-rest | `harness` root + adapters (staged driver), tests | `stagedDriver` owns Start-persist-Open-StartTurn with per-adapter transport/framing hooks; adapter-specific state machines (codex cancel attempts, omp stopping latch, claude non-retryable close, opencode SSE) as configuration. Accept: fixture replays + native acceptance green; error strings/retry semantics unchanged. Oracle: per-adapter suites, fixture replays, `go test ./internal/harness/...`. | med | none | -600..-800 | W2-13, W3-04 |
| W4-16 | Python test harness: git fixture + citest (SC S5 + T8) | scripts-ci | `scripts/ci/testsupport/gitfixture.py` (new), `scripts/ci/citest.py` (new), `test-check-*.py`, `test-model-routing-evaluation.py`, `test-assert-*.py`, `test-credentialed-provider-witness.py`, `test-linux-v2-source-subject.py`, `test-no-legacy-routes-audit.py`, `test-rootless-compose-transition-proxy.py` | One `FixtureRepo` + `load_gate`/`run_gate`/`write_json`/`strict_json` helpers; delete the in-test proxy stub in favour of the production proxy module. Accept: gate CLI outputs byte-identical on fixtures; ~-250..-350 LOC; all 24 suites green. Oracle: `python3 scripts/ci/test-*.py`; CI python steps. | low | none | -250..-350 | W3-16 |

## 8. Wave 5 and the tail (W6+) — high-risk programs

High-risk rows carry one integration owner each and an independent reviewer; merge gates are the
slow oracles below, not local runs. **Tail (W6+) rows run as micro-waves**: rows that touch
`ci.yml` (W6-01, W6-02, W6-08, W6-12) are serialized in the order shown — at most one `ci.yml` owner
per wave, per the brief.

### Wave 5 (13 slices)

| id | slice | area | owned files | change · acceptance · oracle | risk | proto | dLOC | deps |
|---|---|---|---|---|---|---|---|---|
| W5-01 | Witness scaffolding converge-in-place: one switch, no build tag (DP S-7, D4) | daemon-platform | `containment_supervisor_{linux,windows}.go`, `linux_owner_anchor_linux.go`, `linux_owner_helper_v2_linux.go`, `cmd/symmetry-daemon/*`, witness tests, `ci.yml` (wave-5 owner), `linux-native-v2-acceptance.yml` | One witness-config struct parsed once behind the single `SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS` gate (today ~18 scattered `SYMMETRY_*` constants at `containment_supervisor_linux.go:53-76`); privileged writes confined to a witness-owned directory; no witness hook reachable while the gate is unset; **no build tag** — witness evidence binds the production binary's SHA-256, so the witnessed blob must be the shipped blob. Accept: witness jobs green against the production build; negative test red on a gate-unset hook; LOC delta re-derived (the −1,000..−1,200 estimate assumed build-tag no-op twins). Oracle: `production_linux_v2_*` + `production_pre_authority_*` witness jobs, `go test ./internal/platform/`, new negative test. | med | local helper protocol (witness config consolidated) | re-derive | D4, W3-02 |
| W5-02 | Containment convergence phase 1: v2 mandatory on Linux (DP S-11a, D3) — **BLOCKED** | daemon-platform + app | `execution/process_launcher_linux.go`, `app.go` (wave-5 owner), e2e witness dispatch, `linux-native-v2-acceptance.yml` | **Blocked (D3): do not dispatch** until (1) an owner-approved design amendment updates `harness.md` (OmO/OMP use the ordinary Linux owner — :82/:119-120; "legacy Linux workspace behavior remains unchanged" :197; "Preserve the legacy generic path" :277) and the host prerequisites, and (2) a dispatched `linux-native-v2-acceptance` run is green for each kind's exact binary on a qualifying host (zero dispatches today; last hosted attempt failed `unified_cgroup_v2_mounted=false`, WSL2 at `transient_unit_cleanup`). Then: `LinuxV2Required` for every Linux harness kind; acceptance matrix extended to codex/claude/generic; legacy path retained but unreachable; platform test asserts no Linux invocation reaches legacy. Accept: acceptance matrix green for all kinds; witness evidence recorded; soak period before any W6-05 deletion. Oracle: `linux-native-v2-acceptance` (dispatch), `production_linux_v2_*`, `go test -race ./...`. | high | local containment protocol | ~+50 | W5-01, owner amendment |
| W5-03 | ChatPage/scope-hook follow-ups (FE S7b + S8) | frontend | `chat.tsx` (post W4-08), `app/shell.tsx`, `attention.tsx`, `runtimes.tsx`, `connections.tsx`, `work/detail.tsx`, `projects.tsx` | One `useSelectedProject()` context hook fed by the router match; pages stop rebuilding the fallback chain. Accept: `router.test.tsx` (42 tests) green; resolution order identical. Oracle: `router.test.tsx`, chat/attention scope tests. | med | none | -60..-100 | W4-08 |
| W5-04 | Goal-settle poll removal + wait budgets (FE S11 + TC-J) | frontend | `goals/detail.tsx`, `goal-attention.test.tsx`, `tool-cache-config.test.ts`, `vitest.config.ts`, 12 wait-budget sites | Drive settle from the query cache/realtime epoch instead of the 120 ms / 2,500 ms poll; explicit `testTimeout` (15 s) with waits strictly smaller; the config test stops being wall-clock-ordered. Accept: goal-attention file <5 s; budget>=budget contradiction gone. Oracle: `goal-attention.test.tsx` (29), `tool-cache-config.test.ts`, full `pnpm test`. | med | none | -40 | W4-11 |
| W5-05 | Error-reason coverage + fallback counter (CW-9) | control-web | `protocol.ex`, new coverage test, controller tests as needed | Pin the domain reasons to the status table; counter on the unmapped fallback (default flip to `:internal_error` stays a separate, approved slice). Accept: every reachable reason asserted; counter fires in a test. Oracle: new test, `protocol_controller_test.exs`, `goal_controller_test.exs`, full `mix test`. | low | none (behaviour-preserving) | ~+40 | W2-03 |
| W5-06 | Test-only seam removal (CW-12 remainder, M8) | control-web | `strict_evidence_json.ex`, `daemon_controller.ex`, 2 tests | Replace `raw_body_private_key/0` with a test constant; keep or explicitly document the clock seam. Accept: no test-only accessor on the request path. Oracle: the two plug/controller test files. | low | none | -10 | W2-02, W2-03 |
| W5-07 | Descendant-monitor state machine (DP S-10) | daemon-platform | `process_unix.go` + tests | Single `monitorState` enum + one `terminal()` predicate; **this is the live path while D3 is deferred** — not "skip/auto-close if W5-02/W6-05 converge" (those are blocked); only a future convergence program can obsolete it. Accept: seal-ordering suite green; invariant by construction. Oracle: `process_unix_linux_test.go` (3,704 L), `containment_supervisor_linux_test.go`, CI race. | high | none | -100..-200 | D3 (deferral makes this the live path) |
| W5-08 | jsdom/browser layer split (TC-K, D15) | tests | `frontend/src/features/{chat,goals,connections}/*.test.tsx`, `browser/tests/portal-*.spec.mjs`, `frontend/AGENTS.md` | Delete jsdom assertions duplicating browser-owned invariants (scroll/layout/focus) one red/green pair at a time; record the split. Accept: each deletion shows the surviving browser test failing on an invariant mutation. Oracle: `pnpm test` + Playwright render gate. | med | none | -1,000..-2,000 test | W4-14 |
| W5-09 | Platform dead test-only helpers (DP S-8) | daemon-platform | `containment_supervisor_linux.go`, `containment_supervisor_windows.go`, `linux_owner_helper_v2_linux.go`, `native_launch_windows.go` + 4 test files | Move `installLinuxSupervisorScanFailureInjection`, `containmentEndpointForIdentity`, `readLinuxOwnerChildPIDs`, `nativeHandleInformation` into the tests that need them. Accept: -45 prod lines; no behaviour. Oracle: `go test ./internal/platform/...` both GOOS. | low | none | -45 | W1-04 |
| W5-10 | Harness capture fixture consolidation (TC-M, D10) | tests | `daemon/internal/harness/testdata/**`, readers in `internal/harness/*_test.go` | Keep one canonical capture + per-version deltas (or generate from a frame list); retire versions older than the two most recent after the runbook check. Accept: fixture tree -40..-60 %; adapter decode tests green. Oracle: `go test ./internal/harness/...`. | low-med | none | deltas replace copies | runbook check |
| W5-11 | Source-subject single implementation (SC S9) | scripts-ci | `scripts/ci/source_subject.py` (new), `linux-v2-source-subject.py`, `model-routing-source-subject.py`, `omo-control-native-evidence.py`, `credentialed-provider-witness.py`, `opencode-v2-support-witness.py`, 4 test suites | One implementation with named variants (`working_tree_digest`, `index_mode_digest`); digests pinned by existing suites. Accept: exact digests unchanged. Oracle: `test-linux-v2-source-subject.py`, `test-model-routing-source-subject.py`, `test-omo-control-native-evidence.py`, fixture record check. | med-high | evidence-artifact digests (pinned) | -200..-300 | W3-16 |
| W5-12 | model-routing module split (SC S10) | scripts-ci | `model-routing-evaluation.py` -> `records.py`/`subject.py`/`gates.py`/`cli.py`, test imports | No rule change; 4k-line file -> 5 modules <1k. Accept: every refusal message and exit code pinned by the existing suite. Oracle: `test-model-routing-evaluation.py`, `test-model-routing-omp-usage.py`, CI model-routing step (146 s). | med | none | ~0 | W5-11 |
| W5-13 | ST1005 genuine-case mechanical slice (optional, D8) | daemon-wide | ~134 prod literals + 18 test assertion files | Lowercase genuine style cases (keep proper nouns); isolated mechanical PR. Accept: lint counts drop; suites green. Oracle: full `go test -count=1 ./...`; lint. | med (churn) | none | ~0 | gate green |

### Tail (W6+) — micro-waves, serialized where `ci.yml` is shared

| id | slice | area | owned files | change · acceptance · oracle | risk | proto | dLOC | deps |
|---|---|---|---|---|---|---|---|---|
| W6-01 | CI structure finalization: inline program extraction + contracts composite action | scripts-ci | `ci.yml` (wave-6 owner), `linux-native-v2-acceptance.yml`, `.github/actions/contracts-build/action.yml` (new), new `scripts/ci/*` programs | Extract the 5 largest inline programs (~-1.4k YAML lines); one composite action for the duplicated contracts job. **Opencode CI assertions and the pinned witness stay** (D1: keep & freeze). Accept: all jobs keep current semantics and names; `ci.yml` ~800 lines. Oracle: full CI run; each extracted program's fixture suite. | med | none | -1,400 YAML | W1-01 |
| W6-02 | Browser spec wiring per D7 | scripts-ci / tests | `ci.yml`, new job(s), `browser/tests/*`, `browser/scripts/run-*.ps1`, `browser/README.md` | Wire the compose-family specs into the existing compose job; add the Linux fixture job for connections/planning; mark live-daemon/chat-live manual; the inventory gate flips to "wired or explicitly manual". Accept: >=3 of the 5 unwired specs gain a CI verdict. Oracle: the new jobs' runs; `python3 scripts/ci/assert-browser-suite-coverage.py`. | med | none | ~+200 CI | W2-01, D7 |
| W6-03 | `isGoTestBinary` fork removal (DP P-05, D4) | daemon-platform | `containment_supervisor_{linux,windows}.go`, `execution/process_launcher_{linux,windows}.go`, the two tests asserting the fork | Delete the test-binary fork; tests exec the real daemon binary or take an explicit injected double; a test asserts no `test.v`-driven branch. Accept: in-process containment tests exercise the shipped path. Oracle: `go test ./internal/platform/ ./internal/execution/ ./e2e/` (CI Windows job). | med | none | -40 | W5-01 (file overlap); independent of D1/D3 |
| W6-04 | opencode promote-or-drop (D1: **keep & freeze — parked, not dispatched**) | daemon-protocol-rest | `daemon/internal/harness/opencode/**` (any future deletion), `app.go`, `app/linux_owner_witness_linux.go`, harness fixtures | **Parked (D1).** Retain the package, its six CI assertions and the release-acceptance case; invest nothing further. Any drop requires an owner-approved design amendment editing `docs/design/harness.md:15`, `docs/goals/README.md:264`, the acceptance script/inputs and `linux_owner_witness_linux.go` together. Keep `testdata/opencode/2.3.0/README.md` in every variant (cited at `harness.md:239`). | — | none | not counted (D1) | D1 |
| W6-05 | Containment convergence phase 2: delete the legacy generation (DP S-11b, D3) — **BLOCKED** | daemon-platform + execution | legacy platform deletion (`containment_supervisor_linux.go`, `linux_ptrace_launch_linux.go`, `linux_supervisor_protocol_linux.go`, legacy monitor/tcp-peer paths), `execution`, `app/*`, e2e, `docs/runbooks/daemon-process-stop.md` | **Blocked (D3): do not dispatch** until W5-02's owner amendment + a green per-kind `linux-native-v2-acceptance` pass exist and the phase-1 soak completed. Then: delete the legacy generation + its suites; keep pidfd/processGroup primitives where v2 consumes them. Accept: acceptance + witness jobs green without legacy. Oracle: `linux-native-v2-acceptance` (all kinds), `production_linux_v2_*`, `go test -race ./...`. | high | local containment protocol | -7,000..-8,000 prod / -6,000 test (decision-gated; not counted) | W5-02 soak, W5-07 |
| W6-06 | cmd extraction: launcher handshake + clone-bootstrap phases (DR S13) | daemon-protocol-rest | `cmd/symmetry-linux-launcher/main.go` -> internal package, `cmd/symmetry-linux-clone-bootstrap/*` -> `internal/clonebootstrap`, tests | Move the handshake state machine + cleanup walker + bootstrap phases over explicit syscall/fd interfaces; argv/FD ABI, static-A identity, `runtime.LockOSThread` and execveat ordering preserved. Accept: moved code unit-tested off-host; e2e witnesses green. Oracle: `main_test.go`, `production_linux_v2_native_witness_test.go`, cleanup witness. | high | none | ~0 (move) | W6-05 |
| W6-07 | Frontend route code splitting (FE S13) | frontend | `router.tsx` + heavy feature entry files | TanStack Router lazy routes for goals/chat/connections/planning; shell eager. Accept: initial chunk ~886 kB -> ~450 kB; router tests green. Oracle: `pnpm build` chunk list; `router.test.tsx`; browser render gate. | low | none | ~0 | W4-08 |
| W6-08 | Dead protocol API deletion: goal-envelope family (DR F-07, D5) | daemon-protocol-rest | `protocol/goal_schema.go`, `goal_subject.go`, `goal_results.go`, `goal_admission.go`, `goal_evidence_batch*.go`, `protocol.go` (legacy decoders), fixture suites | Delete the decode/validate family with no `cmd/...`-reachable caller; retarget fixture suites to the production entry points (`ParseAdmissionInput`, `ParseTaskResult`, `ValidateMicroUSD`, `ValidateTaskResultForAdmission`, `DecodeContextSnapshot`). Accept: fixture suites green against the production entry points; `deadcode` count drops further. Oracle: `goal_contract_fixture_test.go`, `goal_contract_semantics_test.go`, `goal_evidence_batch*_test.go`, `goal_admission_input_test.go`; full `go test`. | low-med | none (no wire field removed) | -400..-900 | W1-06 |
| W6-09 | Remove always-null `GoalAttentionFeed.unavailable` (CW S12, D2 — narrowed) | control-web + frontend + contracts | `portal_json.ex`, `contracts/v1/attention-read.schema.json` (`GoalAttentionFeed` member only), `contracts/fixtures/*` (goal-attention), generated TS/Effect DTOs, `frontend/src/**` decoder | Remove **`GoalAttentionFeed.unavailable` only**, end to end (schema member, fixtures, projection, DTOs, frontend). `ProjectBlockedFeed.unavailable` stays — it is live (`portal_json.ex:137-140` populates it on workspace-read failure; `frontend/src/features/attention/attention.tsx:38, :65` renders it). Accept: served-document gates + contracts check + frontend tests green; **a test forces a workspace read error and asserts the notice**; the project-blocked schema/projection/rendering are unchanged. Oracle: `pnpm contracts:check`, attention tests, `pnpm --filter @symmetry/frontend test`. | med | **wire/schema change (no back-compat required)** | -30 | W5-05 |
| W6-10 | Elixir lint/coverage flips | control | `ci.yml` (serialized after W6-02), `control/.credo.exs` | Flip `mix credo --strict` blocking when the count reaches zero; coverage floor (W2-17) blocking. Accept: gates live; tree green. Oracle: CI control job. | low | none | ~0 | W6-01 |
| W6-11 | Styler adoption + remaining duplication burn-down (D12, D16) | control + all | `control/.formatter.exs`, mechanical format diff; changed-file jscpd config | `plugins: [Styler]`, reviewed first-run diff (especially test stub modules); tighten jscpd changed-file gate. Accept: alias-class findings drop; format diff reviewed. Oracle: `mix format --check-formatted`, full `mix test`; jscpd on a changed file. | low | none | -55 findings | tail of control slices |
| W6-12 | Wall-clock burn-down part 2 + duration ratchet blocking (TC-F/TC-B cont.) | tests | remaining Go/ExUnit/vitest wall-clock sites; `ci.yml` (serialized), threshold config | Finish the burn-down; flip duration thresholds/ratchets to blocking. Accept: race-suite floor and mix wall-time targets met; thresholds live. Oracle: CI runs. | med | none | -150 test | W4-13, W2-01 |

---

## 9. Owner decisions (recommended defaults — adopted without asking)

The owner mandate says adopt the default when the call is ours to make. These are the policy calls
the audits surfaced; each names the default this program adopts and why. Decisions marked
"policy call" still deserve a one-line acknowledgement in the first PR that implements them.
**D1–D4 were re-decided at the wave-1 dispatch gate (2026-11-21)** as recorded in the rows below;
D1's keep-and-freeze and D3's deferral are owner-amendment-gated relative to the earlier draft.

| # | Decision | Default adopted | Rationale |
|---|---|---|---|
| D1 | opencode adapter fate (11.7k LOC, half-wired) | **Keep and freeze** — retain `harness/opencode/**`, its six CI assertions and the Linux v2 release-acceptance case; invest nothing further (W6-04 parked). Any drop requires an owner-approved design amendment editing `docs/design/harness.md:15`, `docs/goals/README.md:264`, `scripts/ci/linux-v2-native-acceptance.sh` + workflow inputs, and `app/linux_owner_witness_linux.go` together | Corrected: its tests **do** run in CI (`ci.yml:1363-1371`, `:1429-1446`), the production witness file imports it (`linux_owner_witness_linux.go:33`), and OpenCode is a case in the release acceptance (`linux-v2-native-acceptance.sh:21, :579, :819-841`) and an owner goal (`docs/goals/README.md:261-265`). Deleting it changes an acceptance contract — not ours to make. Keep `testdata/opencode/2.3.0/README.md` regardless (cited at `harness.md:239`). |
| D2 | `attention.unavailable` (always-null wire member) | **Remove `GoalAttentionFeed.unavailable` only** (schema member, fixtures, portal projection, DTOs, frontend) in W6-09; `ProjectBlockedFeed.unavailable` stays | Only the goal-attention member is always null (`portal_json.ex:109-123`); the project-blocked member is live — `portal_json.ex:137-140` populates `%{code, message}` on workspace-read failure and `frontend/src/features/attention/attention.tsx:38, :65` renders it. W6-09's acceptance asserts that failure surface is unchanged. |
| D3 | Dual Linux containment generations (legacy supervisor + cgroup-v2 owner) | **Deferred / amendment-gated**: W5-02 and W6-05 are blocked until (1) an owner-approved design amendment updates `harness.md` for OmO/OMP/generic/workspace policy and the host prerequisites, and (2) a dispatched `linux-native-v2-acceptance` run on a qualifying host is green for each kind's exact binary | Fixed design text keeps the ordinary Linux owner for OmO (`harness.md:82`) and OMP (`:119-120`), "legacy Linux workspace behavior remains unchanged" (`:197`), "Preserve the legacy generic path" (`:277`); v2 implies distinct host UIDs + the disposable B clone instead of `git_worktree` (`:186-197`). No usable oracle exists today (zero dispatches; last hosted attempt failed `unified_cgroup_v2_mounted=false`, WSL2 at `transient_unit_cleanup`). Until then W5-07 is the **live path**, not conditional; the −7..−8k LOC is not counted as high-confidence. |
| D4 | Test-only witness authority in production binaries; `isGoTestBinary` fork | **Consolidate witness support in place** (W5-01): one witness-config struct parsed once behind the single `SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS` gate; privileged writes confined to a witness-owned directory; negative test that no hook is reachable while the gate is unset. **No build tag.** Delete the self-inspection fork (W6-03, independent of D3) | Witness evidence is **production-binary** evidence with the daemon's SHA-256 bound into it (`docs/evidence/goal-0006-linux-owner-design-2026-09-22.md:424-434`; exact-binary promotion rule `harness.md:35, :42, :66, :111`) — a witness build would attest a blob that never ships. A build tag is allowed only through an owner-approved amendment that also says how the shipped blob is witnessed. |
| D5 | Dead goal-envelope decode/validate family in `internal/protocol` | **Delete**, retarget fixture suites to the production entry points (W6-08) | No `cmd/...`-reachable caller; fixtures keep the wire authority. If a future consumer appears, it should call the production parsers. |
| D6 | Linux v2 ack stages (10 declared, 2 emitted) | **Shrink the table to the emitted stages** and derive app validation from it (folded into W4-05); first prove no persisted journal/receipt can carry a removed stage name — otherwise keep decode tolerance for removed names | Unreachable witness stages mislead reviewers; a shorter table is enforceable. The change is local-protocol/evidence vocabulary, so witness-job reviewers must see it (see W4-05). |
| D7 | Browser specs never run in CI | **Keep and wire by fixture family**: compose specs → existing compose job; connections/planning → a new Linux fixture job; live-daemon/chat-live → explicit manual matrix; inventory gate prevents regression (W2-01 gate, W6-02 wiring) | The specs encode acceptance for areas the refactor touches; the matrix makes the remaining manual gap explicit instead of silent. If wiring a spec family proves impossible in W6, the default flips to deleting that spec + its ps1 driver. |
| D8 | ST1005 (584 capitalized Go error strings) | **Exclude ST1005 from the golangci gate**; optional mechanical slice for the ~134 genuine cases (W5-13) | 380/514 measured first words are proper nouns/initialisms; gating on this freezes the tree on domain vocabulary. The exclusion is recorded in the config, not via blanket nolint. |
| D9 | ExUnit serialization | **Convert DB-only files to `async: true`**, keep `async:false` for app-env/migration files; treat new flakes as findings (W3-14) | 89.5 s of the 102 s `mix test` is the sync half; the files already isolate by unique keys. |
| D10 | Harness capture fixture versions (114 files) | **Keep one canonical capture per harness + deltas**; retire versions older than the two most recent after checking `docs/runbooks` (W5-10) | A harness bump must be a reviewable delta, not a 20-file copy; pinned versions in use stay covered. |
| D11 | pytest vs unittest for `scripts/ci` | **Keep unittest**; revisit only if parametrization need grows | 1,495 `PT009` findings would be style churn, not defects. |
| D12 | Styler as a `mix format` plugin | **Adopt** in a standalone mechanical PR after the control-web lint clean-up (W6-11), with a reviewed first run | Deletes ~55 alias-class Credo findings and prevents recurrence; the only risk (behaviour-changing rewrites) is handled by review, especially in test stub modules. |
| D13 | MSW for frontend test doubles | **Adopt** in the test-harness slice (W4-11) | Replaces 400-600 lines of hand-rolled request routing while keeping the existing path/method assertions; the suite already funnels requests through one fetcher. |
| D14 | Coverage ratchet | Artifacts advisory in **W2-17** (coverage plane, moved from W1-01); floors **blocking at W6-10/W6-12** once baselines are stable | Coverage's job here is to make deletion slices safe; hard floors before baselines stabilize would create noise. |
| D15 | jsdom vs browser invariant split | **Adopt the split** (jsdom = state/contracts, browser = layout/scroll/focus), deletions one red/green pair at a time (W5-08) | Removes duplicated assertions and the weaker twin; the discipline prevents silent coverage loss. |
| D16 | Duplication gate | **Changed-file jscpd with explicit `--max-lines/--max-size`** (defaults silently skip the largest files); repo-wide numbers trend-only (W6-11) | The measured duplication is dominated by legacy fixtures; a changed-file gate ratchets new code. |
| D17 | `os.Root` for path confinement | **Bounded trial in the Windows private-path check only** (backlog) | openat2/fsync semantics in the Linux cleanup reader are load-bearing; the stdlib replacement is safe only where the requirement is plain no-traversal. |
| D18 | happy-dom / react-hook-form | **Defer** (no measured win demonstrated) | Environment/form-state swaps without evidence would trade known behavior for unknown flake surface. |

## 10. Consolidated lint and coverage plans

Reconciled from the four area plans (control-web/core §5, daemon-app/platform/protocol/tests §5,
frontend §5, scripts-ci §5). All gate additions land advisory first and flip to blocking per area
when that area's count reaches zero. Baselines are re-measured at implementation time on
`a3039f0`+; the counts below are the audits' measurements.

### 10.1 Credo (control)

- Dependency: `{:credo, "~> 1.7", only: [:dev, :test], runtime: false}` (W1-02); config
  `control/.credo.exs` (W1-01). Start from `mix credo gen.config` output; **do not carry a
  `checks: %{}` key** — verified trap: it silently disables AliasOrder/AliasUsage/PreferImplicitTry/
  StringSigils/Apply (403 vs 474 issues). Enable `Credo.Check.Design.DuplicatedCode` (mass 40) and
  `Readability.MaxLineLength` (98, matching the formatter default).
- Baselines (two runs, versions differ — reconcile into one re-measurement in W1-01):
  control-core **strict 459** over lib+test (Credo 1.7.12; 370 in-area; CyclomaticComplexity 149,
  Nesting 124, FunctionArity 29, mechanical-class 132, `DuplicatedCode` 0 blocks at mass 40 — the
  real duplication is structural and below its threshold); control-web **default set 474
  project-wide** (1.7.19; 387 lib / 87 test; Nesting 162, AliasUsage 33). Re-take with the
  portable toolchain (`<HOST-PATH>/.codex/tools/symmetry`), pin one version, and record that
  number as the ratchet baseline.
- Staging: advisory `mix credo --strict` (optionally `--min-priority 0` first, per control-core) →
  burn down per area (web lib in W1-03; slices carry their own findings) → bare `--strict` blocking
  at zero (W6-10). Credo has no autofix; Styler (D12) covers the mechanical alias share.

### 10.2 golangci-lint (daemon)

- Config `daemon/.golangci.yml`, run twice (`GOOS=linux`, `GOOS=windows`) because build tags change
  `unused`/`gocyclo`/errcheck results; pin **v2.15.0** (≤2.6.1 fails to type-check Go 1.27 export
  data); uncapped counting (`--max-issues-per-linter=0 --max-same-issues=0`).

```yaml
version: "2"
run: {timeout: 10m}
linters:
  default: standard
  enable: [bodyclose, copyloopvar, dupl, errorlint, exhaustive, funlen, gocritic,
           gocyclo, misspell, nilerr, unconvert, unparam, whitespace]
  settings:
    dupl: {threshold: 150}          # 4 blocks at 150 today; drop toward 100 after W2-W4
    funlen: {lines: 120, statements: 60}
    gocyclo: {min-complexity: 25}
    gocritic: {enabled-tags: [diagnostic]}   # style/perf tags are +1000 noise
    staticcheck:
      checks: ["all", "-ST1000", "-ST1003", "-ST1005", "-ST1016", "-ST1020", "-ST1021", "-ST1022"]
  exclusions:
    presets: [comments, common-false-positives, legacy, std-error-handling]
    rules:
      - {path: _test\.go, linters: [errcheck, funlen, gocyclo, dupl]}
formatters:
  enable: [gofmt]
```

- Baselines: the daemon-app area measured **linux 253 (207 prod / 46 test), windows 172 (130 / 42)**
  under its area config; the module-wide run under this merged config measures **1,029 linux**
  (gocyclo 268, errorlint 195, unconvert 134, gocritic 103, staticcheck 77, errcheck 58, unparam 56,
  exhaustive 56, ineffassign 19, funlen 17, copyloopvar 13, bodyclose 12, nilerr 11, dupl 4,
  misspell 2, whitespace 2, govet 1, unused 1). ~2.1 % auto-fixable — rollout is triage-based, not
  `--fix`-based. W1-01 publishes the per-linter, per-package measured baseline as the ratchet.
  `dupl` at 150 is **4 blocks today** (`authority/authority.go:1620-1636 ↔ :1788-1804`,
  `control/client.go:775-797 ↔ :861-883`); at 100 it catches the ~9 listed blocks (largest share
  fixed by W2-10).
- Staging: advisory W1-01 → mechanical groups burned into area slices → blocking per area; gosec
  and style/perf gocritic tags stay out until after W4 (deliberate post-refactor decision).

### 10.3 ESLint (frontend + browser)

- Adopt `strictTypeChecked` + `stylisticTypeChecked` + react-hooks `recommended-latest` (W1-10):
  autofix 515/713 (~72 %), downgrade the remaining ~198 to warnings with a `--max-warnings`
  ratchet; then burn down by rule (`restrict-template-expressions`, `no-invalid-void-type`,
  `no-non-null-assertion`, `prefer-nullish-coalescing`, …), deprecations (13), and the design-
  dependent `no-unnecessary-condition` (64 src) only after FE F4/F11 remove double validation.
  Size caps `complexity: 25` / `max-lines-per-function: 150` with a shrink-only allowlist.
- Add knip (0 unused files, 3 unused exports today; `@symmetry/contracts` and `tailwindcss` are
  known false positives until knip follows the workspace exports layout) and a `browser/**` flat
  config block (9.2k unparsed `.mjs` lines) at W6-01.

### 10.4 ruff (scripts/ci)

- `pyproject.toml` (W1-11): target py312, `line-length = 120`, select
  `["E","F","W","I","B","UP","SIM","C4","PERF","RUF","ARG"]`, ignore `TRY003`/`BLE001` (the
  toolbelt's deliberate long-message refusal style), per-file ignores `ARG001/ARG005` for
  `test-*.py`, double quotes. `ruff format` first (50/51 files), then `ruff check --fix` (42 of 94
  default findings), fix the rest to zero. Baseline at length 120: E501 221, B904 62, I001 29,
  SIM105 12, ARG001 11. Gate: `ruff check` + `ruff format --check` in the advisory job (W1-01),
  blocking once W1-11 lands clean.

### 10.5 Coverage plane

- W2-17 (moved from W1-01): `continue-on-error` coverage jobs gated `needs: changes` +
  `run-heavy` — `mix test --cover` (threshold 0), a separate `go test -coverprofile` job (the
  existing race/`mix test` steps stay untouched — they sit at the 600 s package timeout),
  `vitest --coverage` (provider dep from W1-10); artifacts uploaded.
- W2-01 adds durations (ExUnit `--slowest`, go-test-json summary, thresholds). Floors become
  blocking at W6-10/W6-12 with a baseline−1 % ratchet. Deletion slices must show coverage does not
  drop.

### 10.6 Duplication

- jscpd everywhere with **explicit** `--max-lines 5000 --max-size 1mb` (defaults skip the six
  largest files, silently reporting 0). Measured: frontend src 0.39 % / with tests 2.29 %; Go tests
  8.81 %; platform 0.88 % textual (structural duplication is the real cost there); scripts 0.78 %.
  Gate on changed files (W6-11) after the W2-W4 dedup slices; `dupl` threshold drop to 100 as a
  follow-up once the known blocks are gone.

---

## 11. Working agreements and dispatch

**Base and branches.** Every slice branches from `origin/main` at its start and rebases before
merge. `Q:/repos/symmetry` stays read-only until a slice is being implemented; implementers use
their own worktrees/branches.

**Oracles.** Slow oracles are named per slice; the CI-side ones are: `mix test` (~102 s, control),
`go test -race -count=1 -timeout 600s ./...` (~449 s step, daemon — internal/app alone ~386 s),
`pnpm --filter @symmetry/frontend test` (~114 s cold), the Playwright render gate (~2.6 min), the
model-routing workflow step (~146 s), and the dispatch-only acceptance workflows
(`linux-native-v2-acceptance`, witness jobs — host-provisioned; schedule deliberately, they have
no scheduled runs; `linux-native-v2-acceptance` has zero recorded dispatches as of this plan —
see D3).

**Local control runs (this host).** Use the portable toolchain, e.g. in the pattern of the scratch
`retro-g2-mix.sh` (`<HOST-PATH>/.codex/tools/symmetry/elixir-1.20.4` +
`erlang-29.0.6/bin/erl.exe`, `erl -noshell -elixir_root <root>/lib -pa <root>/lib/elixir/ebin
-s elixir start_cli -extra <root>/bin/mix <args>`); `mix test` additionally needs a local
PostgreSQL. Do not use the WSL-vendoring workaround (control-core §6) — it was based on the
incorrect "no Elixir on this host" assumption (see §1.2).

**Move/delete slices.** Run `scripts/ci/check-citation-existence.py` over the full audited corpus at
the slice head, update the bound symbol↔path citations, and add the citing docs to the owned set.

**docs/TODO.md conflicts.** The `docsunion` merge driver exists only in local git config
(`.git/info/attributes`); GitHub still reports conflicts on `docs/TODO.md`, so resolve them by local
rebase.

**Test-change discipline.** No test/type/validation/permission weakening. Deletions and conversions
of assertions require a mutation check (flip the guarded behaviour, watch the converted test fail).
CI `--assert` gates that pin subtest names must keep passing with identical names.

**Per-slice PR body** carries: plan id (e.g. W1-04), owned file set, acceptance result, oracle
output, risk/proto/LOC lines from this plan, and the decision ids it implements.

**Wave-1 dispatch summary** (10-16 parallel workers; 14 slices):

| id | one-liner |
|---|---|
| W1-01 | CI quality-instrumentation plane: advisory golangci (2 GOOS) / credo / ruff / shellcheck+actionlint steps + vulnerability scanning + dependabot (no new jobs, no coverage) |
| W1-02 | control dep hygiene: Credo dep, drop telemetry deps, plug 1.20.4 (CVE fix), drop `:multipart` |
| W1-03 | control-web Credo strict clean-up (10 lib files, 15 issues) |
| W1-04 | platform god-file split (move-only: anchor/helper/supervisor) |
| W1-05 | app.go split into 11 concern files (move-only) |
| W1-06 | daemon dead-code sweep, re-scoped: caller-free surfaces only; witness-used deletions deferred |
| W1-07 | daemon-app/state hygiene: delete-only dead stores + preflight escape-parsing bug fix |
| W1-08 | daemon test enablers: per-test goleak, state `t.Parallel()`, short-wait folding |
| W1-09 | contracts single-pass decode |
| W1-10 | frontend strict-ESLint baseline + autofix + dead-export trim + vitest coverage config |
| W1-11 | scripts/ci ruff baseline (config + format + autofix) |
| W1-12 | Elixir fixture extraction batch 1 (TestFixtures + OrchestrationCase) |
| W1-13 | control-web served-document gates for the 4 ungated reads |
| W1-14 | control-core dead-surface deletion (F11) |

Total expected LOC across the program (rough, from the reports): high-confidence now ≈ **−4k..−5k
production** plus test-side dedup (dead-code sweeps −2k, dedup/extraction −2..−3k;
witness-consolidation delta re-derived — the old −1k assumed build-tag no-op twins). The two
largest pools are **decision-gated and excluded from the totals**: opencode −11.7k incl. tests
(D1: keep & freeze) and legacy containment −7..−8k prod / −6k test (D3: deferred until an owner
amendment and a green `linux-native-v2-acceptance` pass exist). Medium-confidence −5..−8k from the
W2-W5 extraction slices is unchanged.
