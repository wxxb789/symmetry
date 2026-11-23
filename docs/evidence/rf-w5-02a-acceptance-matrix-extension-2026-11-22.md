# RF-W5-02a acceptance-matrix extension: codex and claude containment case sets — 2026-11-22

Status: **recording.** `codex_containment` and `claude_containment` exist in
`.github/workflows/linux-native-v2-acceptance.yml` and `scripts/ci/linux-v2-native-acceptance.sh`, with one
same-RunKey `native_release` crash-replay leg per kind, and the off-host gates below are green. This note
grants no acceptance: neither case set has been dispatched, so nothing here is per-kind green evidence, and
the D3 `harness.md` amendment that cites them stays blocked on a dispatched run (tracked in `docs/TODO.md`).

## Scope and authority

- Conductor decision of 2026-11-22 **supersedes the contract's kind list**: this increment covers
  `codex` and `claude_code` only. omo/omp/generic kinds move to W5-02b; `daemon/native/`,
  `daemon/internal/config/**` and `scripts/ci/linux-v2-host-prerequisite-probe.sh` are outside the changed
  set; the `shared` case set's frozen opencode leg is excluded from the per-kind green gate while W6-04/D1
  keeps its acceptance-case bytes frozen (Owner ruling F2, 2026-11-22, binding; see the plan §7) — this
  increment changes neither its bytes nor its case, and a red frozen leg in a dispatch is recorded as the
  F2-excluded leg.
- Plan: `docs/plans/2026-11-22-rf-w5-02a-acceptance-matrix-extension-plan.md` (interface freeze §2, semantic
  decisions §3, verification already performed §4, owned-set additions §6).
- Slice artifacts: the plan above, this record, and the `docs/TODO.md` entry that carries the slice's undone
  part (first dispatch, the D3 amendment citation, the opencode F2 exclusion, the AppArmor question).

## Frozen interfaces (as implemented)

| Surface | Value |
| --- | --- |
| Case sets / Control fixture cases | `codex_containment`, `claude_containment`, plus replay cases `stage_replay_native_release_codex`, `stage_replay_native_release_claude`; `@expected_fixture_runs` 44 → 48 |
| Request cases | containment cases write their own name; the replay cases write `case: "stage_replay"` with hold `native_release` |
| Profiles / Control runtimes | `codex-native`, `claude-native`; `owner-witness-control-codex-generic`, `owner-witness-control-claude-generic` |
| Env | `SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_{CODEX,CLAUDE}_{CONFIG,REQUEST,BINARY_SHA256}` |
| Marker / bundle dirs | `codex-containment`, `claude-containment`, `stage-replay-native-release-{codex,claude}`; `$OWNER_STAGE_REPLAY_ROOT/native_release_<kind>` |
| Witness summary/response | `native_binary` (absolute path), `native_binary_sha256` (`sha256:`+64 hex over the pinned executable), `containment.native_open_reached` |
| Evidence JSON | adds `native_version_line` for every native case (test-authored pin; the fixture verifies the installed CLI's verbatim `--version` against it, so it is not a daemon-observed value) |
| `manifest.json` (per-kind sets) | `native_binary{kind,path,sha256,archive_sha256,version}`, `containment{native_open_reached}`, `source_tree_digest` |

The exporter names the claude kind **`claude_code`** (matching `config.RuntimeHarnessClaudeCode`), not
`claude`: the contract's prose used the short name. The manifest `kind` value is what the judging predicate
must match.

## Pins

Verified 2026-11-22 against the GitHub release API and re-computed from the downloaded bytes in WSL
Ubuntu-24.04 (plan §4): archive SHA-256 for all four archives, executable SHA-256 after extraction, and the
`--version` line.

| kind | archive | archive sha256 | executable | executable sha256 | version line |
| --- | --- | --- | --- | --- | --- |
| codex | `codex-package-x86_64-unknown-linux-musl.tar.gz` (`rust-v0.168.0`) | `3acbff91c0a8e30767d47c8b6941279a603ab4087280aa3e546537cef3622cb8` | `bin/codex` | `a8cb58c528c76e7313d4a70294ec614757b0f4c7bf9683e11c88ecad8c3d421b` | `codex-cli 0.168.0` |
| codex helper | `bwrap-x86_64-unknown-linux-musl.tar.gz` | `992cdf4ab9775a2afcb94ed9a139dd013c116b15b23ab7922e2ca7c82c9d4757` | `bwrap-x86_64-unknown-linux-musl` | `77360cb751ccedc5971391444ac86a8a33c15b04d6b4a6fe45f5d25496e62c4c` | — |
| codex helper | `codex-code-mode-host-x86_64-unknown-linux-musl.tar.gz` | `4e00a7d5eec14b50be2f21f11feb13dcbaa6dd500b95fc1f9ba4871ec44af18e` | `codex-code-mode-host-x86_64-unknown-linux-musl` | `e1a56831a087bfeb1a91d43b7265fd1c38b898396b09ab357dc751d641ad25bd` | — |
| claude | `claude-linux-x64.tar.gz` (`v2.1.323`) | `522255678548859b9799b7b098c03a3aa286c9a9ede01957e35f4713ccc43631` | `claude` | `604d29a6869409106b6a1110bdadcc8b72e8654a5a865d23258f938a696e348e` | `2.1.323 (Claude Code)` |

The staged-guest digest assert compares the installed `/usr/local/bin/{codex,claude}` against the pinned
executable digest before the witness runs, and the e2e requires the daemon-reported
`native_binary_sha256` to equal `sha256:<pinned>`; a mismatch fails the case, not just the manifest.

## Off-host gates (commands and exits)

These are the implement-stage runs (worktree based at `6148ba6ddb7b9918a5ea5ed40df434a30953c691`,
before `origin/main` was merged at `1ac4e867`) plus that stage's change set; every later section
names the revision its own rows ran against.

| Gate | Command (working directory) | Result |
| --- | --- | --- |
| Go build (linux) | `GOOS=linux GOARCH=amd64 go build ./...` (`daemon/`) | rc=0 |
| Go vet (linux) | `GOOS=linux GOARCH=amd64 go vet ./internal/app/ ./internal/harness/claude/ ./e2e/` | rc=0 |
| Go vet (linux, contract scope) | `GOOS=linux GOARCH=amd64 go vet ./e2e/... ./internal/app/... ./internal/harness/...` | rc=0 |
| Go vet (host) | `go vet ./...` (Windows) | rc=0 |
| gofmt | `gofmt -l` on the five changed Go files + `e2e/` | no output, rc=0 |
| Go unit (host) | `go test -count=1 ./internal/app/... ./internal/harness/...` (Windows, bg_21) | rc=0; `internal/app` 591.9 s, `harness/claude` 41.3 s, `harness/codex` 31.6 s, rest `ok` |
| Go focused (WSL) | `go test -count=1 ./internal/app/ -run 'TestLinuxV2AdmissionKeepsCodexAndClaudeOnTheLegacyPath\|TestLinuxV2NativeWitnessContainment{CasesMatchTheirKind,SelectsTheOpenOnlyPath,SummaryRequiresTheOpenBoundary}\|TestLinuxV2NativeWitnessConfigRequiresThePinnedKindVersion'` and `./internal/harness/claude/` | 5 tests PASS, rc=0; claude adapter `ok` 13.8 s, rc=0 |
| e2e discovery (WSL) | `go test -list 'NativeContainment$' ./e2e/` | rc=0; lists `TestProductionLinuxV2CodexNativeContainment`, `TestProductionLinuxV2ClaudeNativeContainment` |
| Go race (WSL) | `go test -count=1 -race ./internal/app/... ./internal/harness/...` (bg_22, CGO_ENABLED=1, gcc present) | rc=0; `internal/app` 1302.7 s, `harness` 1.3 s, `harness/claude` 34.0 s, `harness/codex` 52.1 s, rest `ok` |
| Go race (WSL) | `go test -count=1 -race ./internal/app/...` re-run on the final bytes (bg_26; the only post-bg_22 source delta was a comment line in `linux_owner_witness_linux.go`, final `sha256` `5897cfe391981d207a964b9ba3ea4acd0bf6b12cab517d7d4b9ee869977e47f3`) | rc=0, `internal/app` 1074.9 s |
| Workflow lint | `actionlint` 1.7.12 `.github/workflows/linux-native-v2-acceptance.yml` | rc=0 |
| Shell syntax | `bash -n scripts/ci/linux-v2-native-{tools,acceptance}.sh` | rc=0 |
| Shell lint | `shellcheck --severity=warning scripts/ci/linux-v2-native-acceptance.sh scripts/ci/linux-v2-native-tools.sh` (repo root; shellcheck 0.9.0 at fix r1, 0.9.0/0.11.0 at fix r2) | exit 1 (findings present, advisory: `continue-on-error: true`, `ci.yml:1297`); fix-r1 counts at the fix-r1 tree: `SC1078` 2, `SC2024` 1, `SC2043` 1, `SC2174` 1, `SC2276` 3, `SC2288` 1, `SC2289` 2, `SC2154` 0. Fix-r2 re-measurement: per-code counts identical between the fix-r2 tree and an `origin/main` copy under the same invocation — `SC1078` 1, `SC2024` 1, `SC2043` 1, `SC2174` 1, `SC2276` 2, `SC2288` 1, `SC2289` 1 (§Fix r2) — so neither fix adds a finding. The `SC2154` class is an artifact of single-file invocations that cannot resolve the `source "$repo/scripts/ci/linux-v2-native-tools.sh"` directive (8 findings: `pi_unpack`, `opencode_unpack`, `codex_executable`, `code_mode_host_executable{,_sha}`, `bwrap_executable{,_sha}`, `claude_executable`). |
| Python fixtures | `python3 scripts/ci/test-linux-v2-evidence.py` (WSL; 39 tests at the implement stage, 41 after the apply added the boundary-copy and replay-binding tests, 42 after the fix r1 mutation tests) | 42 tests at the final tree, `OK`, rc=0 |
| Python fixtures | `python3 scripts/ci/test-linux-v2-control-receipt.py` (WSL root, bg_25) | 9 tests, `OK`, rc=0 |
| Python fixtures | `scripts/ci/test-linux-v2-source-subject.py`, `scripts/ci/test-linux-v2-go-events.py`, `scripts/ci/test-ci-structure-extraction.py` | 7 OK / 6 OK / 23 OK, rc=0 |
| Python lint | `python -m ruff check` + `ruff format --check` on the four changed `scripts/ci/*.py` | `All checks passed!`, `4 files already formatted`, rc=0 |
| Apply-stage re-verification (2026-11-22, post-apply worktree) | the same off-host commands, re-run on the delivered bytes | every rc=0 row re-established: `gofmt -l` no output, `go vet ./...` rc=0, `GOOS=linux GOARCH=amd64 go build ./...` rc=0, `GOOS=linux go vet ./e2e/... ./internal/app/... ./internal/harness/...` rc=0, `go test -list 'NativeContainment$' ./e2e/` rc=0 (both entries), focused WSL witness pins rc=0 (`SummaryRequiresTheOpenBoundary` now asserts a satisfied fixture and reddens on a synthetic containment gate), `test-linux-v2-evidence.py` 41 tests `OK` at the apply stage (42 at the fix r1 tree, §Fix r1), `ruff check`/`format --check` rc=0, `actionlint` rc=0, `bash -n` rc=0; `go test -count=1 ./internal/app/... ./internal/harness/...` (Windows) hit one contention flake — `internal/harness/pi` `TestNativeRepositoryTaskSubjectUsesRepositoryBaseline` deadline under load — which passes in isolation (6.2 s, rc=0) and in the WSL race run; the WSL race suite is green with `-timeout 2400s` (`internal/app` 606.1 s; the only earlier failure was the default 600 s per-package cap under `-race`, no data race) |
| Elixir fixture | row-arithmetic check over `scripts/ci/provision-linux-v2-control-claims.exs` (regex over the `@cases` expression and the two stage lists) | `declared=48`; literal rows 24 + `@stage_replay_holds` 14 + `@ack_loss_stages` 10 = 48, `match=True` (re-derived at the apply stage); no duplicate names; the four new rows are exactly `codex_containment`, `claude_containment`, `stage_replay_native_release_codex`, `stage_replay_native_release_claude`. The fixture itself only executes inside the dispatch (Control release + Postgres), recorded as such |

`test-linux-v2-evidence.py` is a Linux-only fixture (the exporter calls `os.getuid()` at HEAD too), so its
gate host is WSL; the Windows interpreter fails it wholesale for that pre-existing reason and that failure is
not part of this change's evidence.

## Expected-failure evidence (observed before/against the change)

- **Routing invariance mutant.** Adding `KindCodex` to `requiresLinuxV2NativeOwner` (i.e. routing codex to
  the native-owner path in production) reddens
  `TestLinuxV2AdmissionKeepsCodexAndClaudeOnTheLegacyPath` at
  `daemon/internal/app/linux_owner_v2_app_test.go:2690`, exit 1; the mutant is reverted and
  `native_run.go` is byte-identical to HEAD (`git status` clean for that file).
- **Exporter digest mutants.** `test_manual_containment_digest_mismatch_refuses_publication` (CLI digest
  differs from the witness document) and the `--source-tree-digest` format/required cases in
  `test_manual_native_arguments_require_all_five_and_matching_case_set` each refuse publication with
  `manifest.json` absent.
- **Claude adapter seam.** `daemon/internal/harness/claude/adapter_test.go` drives the new
  `StartProcess` seam; the app-side witness unit pins (`linux_owner_witness_linux_test.go`) pin the case map,
  version equality, containment path selection and the summary digest requirement.
- **No weakening of pi/opencode.** The config generator keeps the base `command`/`native_model` expression
  byte-identical and applies the codex/claude overrides after the agent dictionary, so `pi`/`opencode`
  JSON is unchanged; the fix-r1 fixture probe compared the `shared` B-home tree between the r1
  generator and the pre-r1-fix generator (identical at that stage, both still carrying the then-unscoped
  placeholders), and fix r2 removed that churn and proves the tree byte-identical to `origin/main`
  itself (`probe_bhome.py`, §Fix r2); the probe also compares the generated
  `pi-provider-action`/`pi-helper-crash`/`pi-daemon-crash`/`opencode-health`/
  `opencode-wsl2`/`codex-containment`/`claude-containment` config bytes for the `shared`,
  `codex-containment` and `claude-containment` suffixes (no mismatch);
  `write-linux-v2-owner-request.py` changes are pure additions plus one refusal-message rename; every
  deleted line in `daemon/e2e/` and `daemon/internal/app/` is a kind-predicate/signature generalization of
  the same assertion (e.g. the non-Pi profile check keeps `profile.ProviderAccess || len(args) != 0`).

## Fix r1 (2026-11-22, round-1 review findings; committed as `7765fa26`)

The ce-code-review receipt for `540263924e..49361f03` reported `MUST_FIX (7)` (6×P2 + 1×P3). All
seven are fixed and committed as `7765fa26` (the head the round-2 review examined); the probes and
mutants below ran on the fix-r1 worktree. Each finding maps to evidence that would redden without the change:

- **#9 frozen acceptance-case bytes.** Base `:579` (barrier-case loop) and base `:819-841` (config
  generator head, including the `command` line and its dict shape) are restored byte-identical; the
  two containment barrier directories get their own loop after the frozen one, and the codex/claude
  agent overrides run after the frozen agent dictionary. Probe: `check-frozen-ranges.py` (scratch;
  `git show 540263924e:<script>` contiguous-block compare against the worktree) reports `base:21`,
  `base:579`, `base:819-841`, `base:894-895` each identical, exit 0.
- **#8 containment credential scope.** Containment case sets no longer write the real
  `SYMMETRY_OPENAI_API_KEY` (no `native-api-key` file is created), the fixture generator stages
  exactly the kind's placeholder, and the guest check fails on any foreign native credential path
  (`.pi`, `.config/opencode`, `.local/share/opencode`, the other kind) or on an extra entry in the
  kind directory. Probes at the final tree: the fixture probe extracts the real generator from the
  script and runs it in a scratch root — codex/claude stage exactly `.codex/auth.json` /
  `.claude/.credentials.json`, no key sentinel, no foreign directories; the pre-fix generator
  (the `git show HEAD:` bytes) stages all six files with the sentinel in `.pi/agent/auth.json`
  and `.local/share/opencode/auth.json` (mutant red). The shell-guard probe extracts the real
  credential arrays, key-write guard and absence block: containment arrays hold only the kind path,
  the non-containment arrays keep only the pi/opencode entries (the fix-r2 change set removes the
  r1-stage placeholder churn and re-proves the generated `shared` tree against `origin/main`,
  §Fix r2), the contained key file stays absent (unguarded pre-fix write: present, mutant red), and
  the absence block passes clean while reddening on a planted
  `.pi/agent/auth.json` and on a planted `.codex/extra.json`.
- **#2 EXIT-trap unbound variable.** `source_commit`/`source_tree_digest` are derived from
  `$source_subject_json` at the script top (before the `trap cleanup EXIT INT TERM` at `:278`); the
  later re-derivation from `source-subject.json` is dropped. Probe: the trap probe (bash
  `set -Eeuo pipefail`, EXIT trap, the same `native_manifest_args` expansion) aborts at
  `source_tree_digest: unbound variable` in the pre-fix shape and reaches the evidence export plus
  the retained-fixture diagnostic in the final shape, preserving the original exit status.
- **#5 exporter refusal branches.** `test_manual_containment_witness_mutations_refuse_publication`
  mutates the fresh witness document six ways (`native_binary`, `native_version_line`,
  `subject.tree_digest`, `containment` absent / `{}` / non-boolean) and requires refusal with the
  matching stderr and no `manifest.json`. Mutant probe on a scratch copy of `scripts/ci`: forcing
  each guard's condition to `if False:` reddens exactly its subtest(s) (the three containment-shape
  mutations redden together), and the restored copy is green.
- **#14 refusal text.** `write-linux-v2-owner-request.py` now refuses non-Pi provider access with
  `non-Pi ClaimResponse must omit provider_access`, matching the daemon-side wording
  (`linux_owner_witness_linux.go:2311`); verified by `grep` at the final tree.
- **#1 plan rows.** The workflow row now states the committed `<evidence root>/*/*.json` upload glob
  (probe: `git diff 540263924e..HEAD -- .github/workflows/linux-native-v2-acceptance.yml`), and the
  Go row splits `verifyNativeExecutable` from the sole digest writer `recordNativeExecutableDigest`
  (probe: `grep -n recordNativeExecutableDigest` → definition `:2964`, call `:2909` inside `runFresh`).
- **#3 F2 exclusion recorded.** The plan header, plan §7, `docs/TODO.md` and this record state the
  exclusion, the fail-closed assertion and the F2-excluded leg naming instead of a pending ruling.
  Probe for the cited assertion:
  `go test -count=1 ./internal/app -run 'TestNewHarnessRegistryBindsNativeAdaptersAndUnsupportedOpenCode|TestStartupProbeRegistersUnavailableNativeProjections'`
  → `PASS` (1.7 s), including the opencode subtest that asserts the refused start.

Fix-stage gate re-run (worktree = `49361f03` + the fix change set, committed as `7765fa26`): `bash -n` on both scripts rc=0
(WSL bash and Windows bash); `shellcheck` per the Shell lint row; `actionlint` rc=0;
`test-linux-v2-evidence.py` 42 tests `OK` rc=0 (WSL); `test-linux-v2-source-subject.py` 7 OK,
`test-linux-v2-go-events.py` 6 OK, `test-ci-structure-extraction.py` 23 OK (3 skipped),
`test-linux-v2-control-receipt.py` (root) 9 OK; `ruff check` + `ruff format --check` on the changed
`scripts/ci/*.py` rc=0; `gofmt -l` (Windows and WSL) no output; `GOOS=linux GOARCH=amd64 go build
./...` rc=0; `go vet ./...` (host) and `GOOS=linux GOARCH=amd64 go vet ./e2e/... ./internal/app/...
./internal/harness/...` rc=0; `go test -list 'NativeContainment$' ./e2e/` (WSL) rc=0 listing both
entries; Elixir row arithmetic re-derived (6 + 18 literal rows + `@stage_replay_holds` 14 +
`@ack_loss_stages` 10 = 48 = `@expected_fixture_runs`, no duplicate names, the four new rows exact).
The heavy `go test` suites are unchanged (no Go file is touched by the fix) and are not re-run here.

## Fix r2 (2026-11-22, PR #256 round-2 review findings fixed; conductor rulings)

The independent round-2 review (`256-opus-review-1`) reported `Fix first`: P1 (a dispatch could never
run the new case sets or publish their evidence), two P2s (scope, frozen churn) and two P3s (stale
wording, stale revision labels). The conductor rulings of 2026-11-22 at the contract's end are binding
for the fixes. Every row below ran on the fix-r2 worktree (`7765fa26` + the fix-r2 change set).

Behavioral probes (a local scratch directory outside the repository):

- **P1 loop: one red set must not hide the others.** The workflow step's `run:` block was extracted
  verbatim (indentation stripped) into a scratch `workflow-run-block.sh` and exercised in a scratch
  `RUNNER_TEMP` against a stub acceptance script that reds only `shared` (the frozen F2-excluded leg):
  all 14 sets ran (`14` `Case set … exited` lines, 13× `exit 0`), stderr carried
  `Failed case sets: shared`, the job exited 1 at the end, and manifests existed for sets after the
  failure (`codex_containment`, `claude_containment`, `stale_leaf`, `stale_journal_subject`) while
  `shared` published none. Red mutant: dropping `|| status=$?` aborts at `shared` after 3 stub lines,
  no later manifest, no failed-set summary. Both shapes exit 1; the assertion set above (14 sets vs 3,
  later manifests present vs absent, the failed-set summary line) distinguishes them.
- **P2 frozen churn: the non-containment B-HOME is byte-identical to `origin/main`.** A scratch probe
  extracts the real fixture-generator heredoc from `origin/main` and from the final tree, runs both in
  scratch roots for the `shared` suffix, and compares the generated `b-home` tree path-by-path with
  per-file SHA-256: identical (4 files + 7 directories), and the containment suffixes stage exactly
  `.codex/auth.json` / `.claude/.credentials.json` (1 directory + 1 file each). A second extraction
  evaluates the final tree's credential arrays for the non-containment case and diffs the effective
  `$B_HOME` token set (arrays + `hooks` + `$B_HOME`) against every `$B_HOME`-prefixed token in the
  `origin/main` region: `diff` exit 0. `python <scratch>/probe_bhome.py` → `PROBE OK`, rc=0.
- **P2 seam neutrality.** `TestCandidateAdapterStartWithoutOwnerSeamUsesAdapterStarter` (new) drives
  `Start` with `request.StartProcess == nil` and an adapter-configured starter and asserts the adapter
  starter ran — the pre-change selection. Production wiring audit:
  `grep -rn 'harness.StartRequest{' daemon --include='*.go' | grep -v _test` → only
  `goal_admission_tx.go:911` (no `StartProcess` field, nil on every ordinary daemon start) and
  `linux_owner_witness_linux.go:3239` (the seam, for codex/claude only), so no production claude start
  reaches the new branch.
- **P3 wording.** `grep -n 'OpenCode owner witness\|non-Pi owner witness' daemon/internal/app/linux_owner_witness_linux.go`
  → the two non-Pi request refusals now read `non-Pi owner witness …` (`:2437`, `:2439`); the
  remaining `OpenCode owner witness` strings are kind-specific arms (`:790`, `:2378`, `:2940`,
  `:3317`). No test or doc pins the old strings (`grep` over `*_test.go` → none).
- **P3 revision labels.** This record's implement-stage runs, §Fix r1 and §Fix r2 each name their
  revision; the plan header names `540263924e`, `1ac4e867` and `7765fa26` and the fix-r2 change set;
  the PR body's fix-r1 sentence names `7765fa26` and no longer claims an uncommitted fix.

Fix-r2 gate re-run:

| Gate | Command | Result |
| --- | --- | --- |
| Shell syntax | `bash -n scripts/ci/linux-v2-native-acceptance.sh scripts/ci/linux-v2-native-tools.sh` | rc=0 |
| Workflow run block | `bash -n` + `shellcheck -s bash` on the extracted step block | rc=0 / rc=0 |
| Workflow lint | `actionlint .github/workflows/linux-native-v2-acceptance.yml` | rc=0 |
| Shell lint (advisory) | `shellcheck --severity=warning scripts/ci/linux-v2-native-acceptance.sh scripts/ci/linux-v2-native-tools.sh` | exit 1 (advisory; repo `ci.yml:1297` `continue-on-error: true`); per-code counts at the fix-r2 tree and for an `origin/main` copy under the same invocation are identical (WSL 0.9.0 and host 0.11.0): `SC1078` 1, `SC2024` 1, `SC2043` 1, `SC2174` 1, `SC2276` 2, `SC2288` 1, `SC2289` 1 — no new finding |
| Python fixtures | `python3 scripts/ci/test-linux-v2-evidence.py` (WSL) | 42 tests `OK`, rc=0 |
| Python fixtures | `python3 scripts/ci/test-linux-v2-source-subject.py` (WSL) | 7 tests `OK`, rc=0 |
| Python fixtures | `python3 scripts/ci/test-linux-v2-go-events.py` (WSL) | 6 tests `OK`, rc=0 |
| Python fixtures | `python3 scripts/ci/test-ci-structure-extraction.py` (WSL) | 23 tests `OK` (3 skipped), rc=0 |
| Python fixtures | `python3 scripts/ci/test-linux-v2-control-receipt.py` (WSL root) | 9 tests `OK`, rc=0 |
| gofmt | `gofmt -l internal/app e2e internal/harness/claude` (`daemon/`) | no output, rc=0 |
| Go vet (host) | `go vet ./...` (`daemon/`) | rc=0 |
| Go build (linux) | `GOOS=linux GOARCH=amd64 go build ./...` (`daemon/`) | rc=0 |
| Go vet (linux) | `GOOS=linux GOARCH=amd64 go vet ./e2e/... ./internal/app/... ./internal/harness/...` | rc=0 |
| Go claude (host) | `go test -count=1 ./internal/harness/claude/...` | `ok` 15.1 s, rc=0 (both seam tests included) |
| e2e discovery (WSL) | `go test -list 'NativeContainment$' ./e2e/` | rc=0, lists both containment entry points |
| Focused app tests (WSL) | `go test -count=1 ./internal/app/ -run 'TestLinuxV2AdmissionKeepsCodexAndClaudeOnTheLegacyPath\|TestLinuxV2NativeWitness' -v` | all matched tests `PASS`, rc=0 (containment case-map/open-path/summary pins and the pinned-kind-version config pin included) |
| Routing invariance (WSL) | `go test -count=1 ./internal/app/ -run TestLinuxV2AdmissionKeepsCodexAndClaudeOnTheLegacyPath -v` | `PASS` 0.71 s, rc=0 |
| Go race (WSL) | `go test -count=1 -timeout 40m -race ./internal/app/... ./internal/harness/...` | rc=0; `internal/app` `ok` 623.7 s, `harness/claude` `ok` 21.9 s, all other harness packages `ok`. The first invocation used Go's default 10 m package timeout and was killed mid-suite at 600 s (the `internal/app` suite runs just past it under `-race` on WSL); re-run with `-timeout 40m`, no race reported |

## Base-vs-HEAD control-flow audit

126 hunks over 20 changed files (`git diff -U0 540263924e | grep -c '^@@'`), 1800 insertions / 144
deletions (`git diff --numstat 540263924e`, counting the new plan and this record; re-derived at the
fix-r2 tree, where the fix-r2 change set alone is 38 hunks, +194/−73 over 6 files).

| Decision point | Base | HEAD | Effect on existing kinds |
| --- | --- | --- | --- |
| `validateLinuxV2NativeWitnessConfig` kind whitelist | pi, opencode, codex | + `claude_code` | addition only; omo/omp/generic still rejected |
| version equality chain | pi/opencode/codex against their `TestedVersion` | + claude against the new `claude.TestedVersion` | same fail-closed shape |
| non-Pi profile check | `profile.ProviderAccess \|\| len(profile.Args) != 0` → "OpenCode owner witness…" | same condition, message "non-Pi owner witness…" | assertion unchanged |
| non-Pi request refusals (`linux_owner_witness_linux.go`) | both arms said "OpenCode owner witness…" for every non-Pi kind | "non-Pi owner witness…" (fix r2) | same conditions; codex/claude refusals are kind-accurate |
| `linuxV2NativeWitnessCaseMatches` | 6 rows | + `codex_containment`→codex, `claude_containment`→`claude_code`; `stage_replay` accepts codex/claude | cross-kind requests still rejected |
| `launchNative` launch-transfer arm | `kind == KindCodex` | `kind == KindCodex \|\| KindClaude` (same wrapper) | claude gains the seam; codex body unchanged |
| `launchNative` turn arm | `else` ran a Pi-style turn for every non-OpenCode kind | `else if witness.containmentCase()` → `Open` + `recordContainmentBoundary()`; the Pi `else` unchanged | containment and per-kind replay legs never start a turn or request `provider_access` |
| post-Open codex gates (wait, result, artifact, usage, health) | `kind == KindCodex` | `witness.request.Case == "codex_native_turn"` | `codex_native_turn` keeps every gate; containment skips them |
| health-peer assert | all non-codex cases | actively excluded for the two containment cases | pi/opencode asserts unchanged |
| `verifyNativeExecutable` | `(profile, kind)`, 3 versions | `(profile, kind)` + claude version, both call sites kept; the executable SHA-256 and path are recorded by `recordNativeExecutableDigest` at the post-prepare call site only | adds the digest binding; the digest is hashed once, before clone/launch |
| `validateFinalSummary` containment block | — | requires `containment.native_open_reached == true`, an absolute `native_binary` and a `sha256:`+64-hex digest for the two cases | a regression cannot downgrade to launch-only |
| `linuxV2ValidateOwnerWitnessResponse` (e2e) | — | containment + digest + `native_version_line` asserts for the two fresh cases | daemon-reported digest must equal the env-pinned digest |
| `run_stage_replay_case` (script) | pi/opencode legs | wrapper over `run_stage_replay_leg`; the legacy legs pass identical arguments | pi/opencode replay bytes unchanged |
| case-dispatch arms (script) | pi/opencode/synthetic arms | + `codex_containment`, `claude_containment` arms and the two per-kind replay calls | addition only |
| `harness_test.go` native-caps helper | pi/codex/opencode arms | + claude arm (test enabler) | no production effect |

## Dispatch (post-merge stage, recorded not-run)

No qualifying host exists in this stage. The next-stage sequence is the one the workflow already encodes:
pre-check the protected environment, `gh workflow run linux-native-v2-acceptance.yml --repo wxxb789/symmetry
--ref main`, then apply the contract's `jq -e` predicates to each per-kind `manifest.json`
(`status`, `case_set`, `source_commit`, `source_tree_digest`, `native_binary.*`, `containment`). Recorded as
not run, never as passed.

## Divergences from the contract

1. Kinds limited to codex + claude (conductor decision; §Scope).
2. Export kind name `claude_code`, not `claude` (§Frozen interfaces).
3. The contract's gofmt scope named `internal/e2e`; the real path is `daemon/e2e`.
4. Owned-set additions beyond the contract's file list (plan §6): `daemon/internal/harness/claude/adapter.go`
   (+ alias and `StartProcess` seam and test), `scripts/ci/register-linux-v2-claim-machine.py`,
   `daemon/internal/app/harness_test.go` (claude caps test-enabler), this evidence record. The
   conductor rulings of 2026-11-22 acknowledge these three files into the owned set and require the
   claude seam's behavior-neutrality proof (§Fix r2).
5. The dispatch is **not run** (no qualifying host here) and is recorded as not-run in the dispatch section
   above, never as passed.
6. Contract Change item 6 asked for the new case sets to be added to the workflow's per-case-set artifact
   allowlist; the commit instead replaced the twelve named entries with one `<evidence root>/*/*.json`
   upload glob (`.github/workflows/linux-native-v2-acceptance.yml`, upload step). Per the conductor P1
   ruling, the step's loop also orders the new per-kind sets before `shared`, runs every case set,
   records each set's exit status, fails the job only after all sets ran, and the allowlisted upload is
   gated `if: always()` instead of `if: success()`. The glob covers the same per-case-set evidence files
   and the upload stays allowlist-only; the behavioural deltas are recorded here rather than
   re-litigated, and the plan's workflow row states the committed bytes.

## Not evidence of

Per-kind green acceptance runs for codex or claude (only a dispatch proves those), the D3 `harness.md`
amendment's prerequisite, codex/claude capability promotion, opencode or omo/omp/generic containment, or the
`shared` set. The pins and the containment boundary behaviour were verified off-host (plan §4: codex
`initialize` + `thread/start` against the pinned app-server echoed `gpt-5.1-codex`/`openai` and the adapter's
post-conditions; claude stayed live ≥10 s with a placeholder credentials file), which is pre-dispatch
evidence, not acceptance.

## Residual risk

- The AppArmor `unprivileged_userns` restriction recorded in
  `docs/evidence/linux-v2-host-probe-fix-2026-11-22.md` is **unverified for this fixture's guest launcher**:
  the launcher/gate creates its user namespace as privileged root, unlike the probe's ambient-cap worker, so
  no exception branch was added here. The first dispatch records the real behaviour. [INFERENCE: those two
  namespace creations differ, so the probe's failure mode is not assumed to transfer.]
- `native_open_reached` is the adapter's established Open boundary: for claude that is process readiness
  (`claude/adapter.go` `candidateSession.Open`), not a model round-trip; the witness requires it `true` for
  both containment cases so a regression cannot silently downgrade the proof.
- The shellcheck advisory step stays at exit 1 (see the Shell lint gate row for the measured per-code counts at
  the final tree and the invocation the counts depend on); the `SC2154` class is an artifact of single-file
  invocations that cannot resolve the `source "$repo/scripts/ci/linux-v2-native-tools.sh"` directive.
- The P1 aggregate loop means a red set no longer short-circuits the job: the worst-case runtime is the sum of
  all 14 sets even when an early one fails (the reason the loop exists — one red set must not hide the others'
  evidence). The first dispatch measures per-set runtimes before any `timeout-minutes` change (F5 lever (a)).
