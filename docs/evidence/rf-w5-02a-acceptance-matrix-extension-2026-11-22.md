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
  set; the `shared` case set's opencode re-pin leg stays frozen on the F2 owner ruling (W6-04/D1).
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
| Env | `SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_{CODEX,CLAUDE}_{CONFIG,REQUEST,BINARY_SHA256,ARCHIVE_SHA256}` |
| Marker / bundle dirs | `codex-containment`, `claude-containment`, `stage-replay-native-release-{codex,claude}`; `$OWNER_STAGE_REPLAY_ROOT/native_release_<kind>` |
| Witness summary/response | `native_binary` (absolute path), `native_binary_sha256` (`sha256:`+64 hex over the pinned executable), `containment.native_open_reached` |
| Evidence JSON | adds `native_version_line` for every native case |
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

All runs on the worktree at HEAD `6148ba6ddb7b9918a5ea5ed40df434a30953c691` plus the uncommitted change set.

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
| Shell lint | `shellcheck --severity=warning` on both scripts | exit 1 (findings present); `acceptance.sh` 15 findings (HEAD 9), `tools.sh` 0; the six new ones are `SC2154` "referenced but not assigned" for `codex_executable`, `claude_executable`, `code_mode_host_executable{,_sha}`, `bwrap_executable{,_sha}` — the same class as the pre-existing `pi_unpack`/`opencode_unpack` globals assigned inside the fetch functions. The CI step is advisory (`continue-on-error: true`, `ci.yml:1297`), so exit 1 does not fail the job |
| Python fixtures | `python3 scripts/ci/test-linux-v2-evidence.py` (WSL, bg_24) | 39 tests, `OK`, rc=0 |
| Python fixtures | `python3 scripts/ci/test-linux-v2-control-receipt.py` (WSL root, bg_25) | 9 tests, `OK`, rc=0 |
| Python fixtures | `scripts/ci/test-linux-v2-source-subject.py`, `scripts/ci/test-linux-v2-go-events.py`, `scripts/ci/test-ci-structure-extraction.py` | 7 OK / 6 OK / 23 OK, rc=0 |
| Python lint | `python -m ruff check` + `ruff format --check` on the four changed `scripts/ci/*.py` | `All checks passed!`, `4 files already formatted`, rc=0 |
| Elixir fixture | row-arithmetic check over `scripts/ci/provision-linux-v2-control-claims.exs` (regex over the `@cases` expression and the two stage lists) | `declared=48`; literal rows 24 + 2 rows inside the two `Enum.map` bodies + `@stage_replay_holds` 14 + `@ack_loss_stages` 10 = 48, `match=True`; no duplicate names; the four new rows are exactly `codex_containment`, `claude_containment`, `stage_replay_native_release_codex`, `stage_replay_native_release_claude`. The fixture itself only executes inside the dispatch (Control release + Postgres), recorded as such |

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
- **No weakening of pi/opencode.** The config generator's `kind` map and `native_model_keys` produce
  byte-identical JSON for `pi`/`opencode` (`command`, `args`, `native_model`/`native_model_provider` present
  and in the same key order); `write-linux-v2-owner-request.py` changes are pure additions; every deleted
  line in `daemon/e2e/` and `daemon/internal/app/` is a kind-predicate/signature generalization of the same
  assertion (e.g. the non-Pi profile check keeps `profile.ProviderAccess || len(args) != 0`).

## Base-vs-HEAD control-flow audit

105 hunks over 18 changed files (`git diff -U0 | grep -c '^@@'`), 974 insertions / 60 deletions
(`git diff --numstat`, plus two new untracked files: the plan and this record).

| Decision point | Base | HEAD | Effect on existing kinds |
| --- | --- | --- | --- |
| `validateLinuxV2NativeWitnessConfig` kind whitelist | pi, opencode, codex | + `claude_code` | addition only; omo/omp/generic still rejected |
| version equality chain | pi/opencode/codex against their `TestedVersion` | + claude against the new `claude.TestedVersion` | same fail-closed shape |
| non-Pi profile check | `profile.ProviderAccess \|\| len(profile.Args) != 0` → "OpenCode owner witness…" | same condition, message "non-Pi owner witness…" | assertion unchanged |
| `linuxV2NativeWitnessCaseMatches` | 6 rows | + `codex_containment`→codex, `claude_containment`→`claude_code`; `stage_replay` accepts codex/claude | cross-kind requests still rejected |
| `launchNative` launch-transfer arm | `kind == KindCodex` | `kind == KindCodex \|\| KindClaude` (same wrapper) | claude gains the seam; codex body unchanged |
| `launchNative` turn arm | `else` ran a Pi-style turn for every non-OpenCode kind | `else if witness.containmentCase()` → `Open` + `recordContainmentBoundary()`; the Pi `else` unchanged | containment and per-kind replay legs never start a turn or request `provider_access` |
| post-Open codex gates (wait, result, artifact, usage, health) | `kind == KindCodex` | `witness.request.Case == "codex_native_turn"` | `codex_native_turn` keeps every gate; containment skips them |
| health-peer assert | all non-codex cases | actively excluded for the two containment cases | pi/opencode asserts unchanged |
| `verifyNativeExecutable` | `(profile, kind)`, 3 versions | `(ctx, profile, kind)` + claude version + executable SHA-256 and path recorded in the summary | adds the digest binding |
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
   `daemon/internal/app/harness_test.go` (claude caps test-enabler), this evidence record.
5. The dispatch is **not run** (no qualifying host here) and is recorded as not-run in the dispatch section
   above, never as passed.

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
- The six new `SC2154` shellcheck findings stay unrepaired (advisory step); they are the existing pattern of
  globals assigned in the sourced fetch functions.
