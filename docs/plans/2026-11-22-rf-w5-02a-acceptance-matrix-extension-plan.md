# rf-w5-02a acceptance-matrix extension — plan

Contract: `goal-rf-w5-02a-acceptance-matrix-extension.md` (GDD state root; external to the
repository). Base revision: `6148ba6ddb7b9918a5ea5ed40df434a30953c691` (= `origin/main`,
2026-11-22; W6-06a #252 and the host-probe fix #247 already merged). Branch:
`refactor/rf-w5-02a-acceptance-matrix-extension`. Owned set is the contract's strict list plus
this plan, `docs/TODO.md` and the evidence record `docs/evidence/rf-w5-02a-acceptance-matrix-extension-2026-11-22.md`.

Conductor decision (2026-11-22, F3/F9 ruling) SUPERSEDES the five-kind scope: this slice covers
**codex and claude only**. `validateRuntimeWorkspacePolicy` already accepts `git_clone_v2` for
both (`daemon/internal/config/config.go:467-483`), so no `daemon/internal/config/**` or
`daemon/internal/linuxpreflight/**` change is in scope; omo/omp/generic move to W5-02b. Owner
ruling F2 (2026-11-22, binding) excludes the frozen opencode leg from the per-kind green gate while
W6-04/D1 keeps its acceptance-case bytes frozen; this slice leaves those bytes untouched and
records the exclusion in §7.

## 1. Delivered shape

| file | change |
| --- | --- |
| `scripts/ci/linux-v2-native-tools.sh` | `linux_v2_fetch_codex` (codex package + `bwrap` + `codex-code-mode-host` archives, archive/executable SHA-256 and `codex-cli 0.168.0` asserts) and `linux_v2_fetch_claude` (`claude-linux-x64.tar.gz`, executable SHA-256, `2.1.323 (Claude Code)`); `linux_v2_fetch_native_tools` keeps its pi+opencode contract, new fetches are called per case set |
| `scripts/ci/linux-v2-native-acceptance.sh` | case-set whitelist +2; codex/claude guest installs (root-owned 0755, staged-digest assert), a B-owned containment credential tree holding only the kind's placeholder (the W6-04/D1-frozen acceptance-case bytes stay byte-identical; new-kind handling sits after them), per-kind containment configs, request/barrier/marker wiring, `codex_containment`/`claude_containment` dispatch arms, per-kind `native_release` replay leg, per-kind manifest CLI args |
| `.github/workflows/linux-native-v2-acceptance.yml` | `case_sets` +2; the twelve per-case-set artifact allowlist entries are replaced by one `<evidence root>/*/*.json` upload glob (committed fact, verified with `git diff 540263924e..HEAD -- .github/workflows/linux-native-v2-acceptance.yml`; recorded as a divergence in the evidence record); trigger/environment/timeout untouched |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `@expected_fixture_runs` 44 → 48; `@cases` +2 containment +2 per-kind `native_release` replay rows; per-kind replay names keep their own `case` |
| `scripts/ci/register-linux-v2-claim-machine.py` | +2 generic Control claim runtimes (`codex-native`, `claude-native`, capabilities `{structured_input: true}`) — scope addition §6 |
| `scripts/ci/write-linux-v2-owner-request.py` | `CASES` +4 rows; per-kind `native_release` replay rows write request `case: stage_replay` |
| `daemon/internal/app/linux_owner_witness_linux.go` | kind whitelist + version equality (codex, `claude_code`); case map +2 + `stage_replay` for codex/claude; credential-free containment path (Open without a turn); codex turn/artifact/usage gates case-scoped to `codex_native_turn`; `verifyNativeExecutable` extended for claude's release equality; new `recordNativeExecutableDigest` (sole writer, called only from `runFresh`, call-site audit `:2909`/`:2964`) records the new summary fields `native_binary`/`native_binary_sha256`; `containment.native_open_reached` |
| `daemon/internal/harness/claude/probe.go` | export `TestedVersion = testedVersion` (one line) |
| `daemon/internal/harness/claude/adapter.go` | optional `StartProcess` launch seam, mirroring `codex/adapter.go:185-195` — scope addition §6 |
| `daemon/internal/harness/claude/adapter_test.go` | seam test |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `TestProductionLinuxV2CodexNativeContainment`, `…ClaudeNativeContainment`; kind-aware version map/CLI check; `linuxV2ValidateFixedBSecretFiles` +2; `linuxV2NativeCommandMatches` +2; per-kind binary-digest env assert; evidence `native_binary`/`native_binary_sha256`/`containment`; per-kind replay sub-cases |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `runProductionLinuxV2StageCrashReplay` generalized to an explicit case; containment boundary/digest asserts in `linuxV2ValidateOwnerWitnessResponse`; opencode-only health-peer assert in `runProductionLinuxV2NativeOwnerWitness` |
| `daemon/internal/app/linux_owner_v2_app_test.go` | routing invariance assertion (codex/claude Linux configs stay legacy, `LinuxV2Required == false`) with a red mutant |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `MANUAL_WITNESSES`/`MANUAL_CASE_SET_FILES` + replay-leg rows; `--native-kind/-binary/-binary-sha256/-archive-sha256/-version-line/-source-tree-digest`; manifest gains `native_binary`, `containment` and `source_tree_digest`, cross-checked against the fresh witness document |
| `scripts/ci/test-linux-v2-evidence.py` | containment manifest tests (publish + digest mismatch refusal) |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | unit pins for the new witness gates (case map, version equality, containment path selection, summary digest) |

## 2. Interface freeze (all workers implement against this)

Pins (tools.sh globals; values verified 2026-11-22 against the release API and re-computed from
the downloaded bytes in WSL Ubuntu-24.04):

| kind | archive | archive sha256 | executable (in archive) | executable sha256 | version line |
| --- | --- | --- | --- | --- | --- |
| codex | `codex-package-x86_64-unknown-linux-musl.tar.gz` (`rust-v0.168.0`) | `3acbff91c0a8e30767d47c8b6941279a603ab4087280aa3e546537cef3622cb8` | `bin/codex` | `a8cb58c528c76e7313d4a70294ec614757b0f4c7bf9683e11c88ecad8c3d421b` | `codex-cli 0.168.0` |
| codex helper | `bwrap-x86_64-unknown-linux-musl.tar.gz` | `992cdf4ab9775a2afcb94ed9a139dd013c116b15b23ab7922e2ca7c82c9d4757` | `bwrap-x86_64-unknown-linux-musl` | `77360cb751ccedc5971391444ac86a8a33c15b04d6b4a6fe45f5d25496e62c4c` | — |
| codex helper | `codex-code-mode-host-x86_64-unknown-linux-musl.tar.gz` | `4e00a7d5eec14b50be2f21f11feb13dcbaa6dd500b95fc1f9ba4871ec44af18e` | `codex-code-mode-host-x86_64-unknown-linux-musl` | `e1a56831a087bfeb1a91d43b7265fd1c38b898396b09ab357dc751d641ad25bd` | — |
| claude | `claude-linux-x64.tar.gz` (`v2.1.323`) | `522255678548859b9799b7b098c03a3aa286c9a9ede01957e35f4713ccc43631` | `claude` | `604d29a6869409106b6a1110bdadcc8b72e8654a5a865d23258f938a696e348e` | `2.1.323 (Claude Code)` |

Naming (frozen):

- Case sets: `codex_containment`, `claude_containment`; Control fixture cases `codex_containment`,
  `claude_containment`, `stage_replay_native_release_codex`, `stage_replay_native_release_claude`
  (+2 = 46 fresh rows, +2 replay = 48; `@expected_fixture_runs = 48`).
- Request cases: containment cases use their own name; per-kind replay cases write
  `case: "stage_replay"` with hold `native_release` (the replay machinery keys on that name).
- Profiles: `codex-native`, `claude-native`; Control runtimes `owner-witness-control-codex-generic`,
  `owner-witness-control-claude-generic` (repo = the opencode repository resource).
- Env: `SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_CODEX_CONFIG|_REQUEST|_BINARY_SHA256`,
  `…_CLAUDE_CONFIG|_REQUEST|_BINARY_SHA256` (the archive digest reaches the manifest through
  `--native-archive-sha256`, not through a witness environment variable).
- Marker dirs: `codex-containment`, `claude-containment`, `stage-replay-native-release-codex`,
  `stage-replay-native-release-claude`; stage-replay bundle dirs
  `$OWNER_STAGE_REPLAY_ROOT/native_release_<kind>`.
- Witness summary/response additions: `native_binary` (absolute path), `native_binary_sha256`
  (`sha256:`+64 hex, computed by the witness over the pinned executable), `containment`
  (`{"native_open_reached": true}`, present for the containment-capable kinds; only `true` is
  reachable, see §3.6).
- Manifest additions for the two per-kind sets: `native_binary` object
  (`kind`, `path`, `sha256`, `archive_sha256`, `version`) and `containment`
  (`native_open_reached`), both cross-checked against the exported witness document, plus
  `source_tree_digest` (the `linux-v2-source-subject.py` tree digest, cross-checked against the
  witness document's `subject.tree_digest`) so each per-kind manifest binds the exact tree it ran
  against.
- Codex containment model: `gpt-5.1-codex` / provider `openai`, credential-free; the pinned
  app-server echoes both at `thread/start` (verified in WSL, §4).

## 3. Semantic decisions the contract left open

1. **Claude reaches its native Open as "process readiness".** `candidateSession.Open` returns once
   the process is live (`claude/adapter.go:349-383`); the containment boundary for claude is
   therefore `native_open_reached` = the session opened, not a model round-trip. Verified in WSL:
   with a placeholder `.claude/.credentials.json` the pinned binary stays live ≥10 s with the
   adapter's exact argv and no other credential (§4).
2. **Codex containment holds at `native_release` and never starts a turn.** The existing
   `else` branch ran a Pi-style turn for every non-OpenCode kind; the containment cases (and the
   codex/claude replay legs) select the Open-only path, and `codex_native_turn` keeps the
   turn/result/artifact/usage gates unchanged. No case calls `StartTurn`, requests
   `provider_access`, or touches a provider credential.
3. **Placeholder credentials carry no secret.** `VerifyLinuxOwnerInstallation` (reached through the
   witness's `prepareLinuxV2NativeOwner` → `Preflight`) requires a nonempty B-owned credential file
   per kind (`linuxNativeCredentialPath`); the fixture writes the literal placeholders
   `containment-placeholder-no-credential` (codex `~/.codex/auth.json`,
   claude `~/.claude/.credentials.json`), which promote nothing and are never sent to a provider
   because no turn exists.
   A containment case set stages exactly that one placeholder file: the real
   `SYMMETRY_OPENAI_API_KEY` is never written for it (no `native-api-key` fixture file, no
   pi/opencode fixture entries), and the guest check proves B's HOME carries no `.pi`,
   `.config/opencode` or `.local/share/opencode` path and the kind directory holds exactly its
   placeholder (probes in the evidence record §Fix r1).
4. **Per-kind replay legs stay** (the conductor did not drop them): each containment set runs the
   fresh case plus one same-RunKey `native_release` crash-replay leg for that kind, so
   launch/stop/replay/recovery is proven per kind. Cost: one extra Control fixture Run and one
   bounded crash/replay cycle per kind (F5 lever (b) remains available to the conductor).
5. **The digest chain is closed by construction.** tools.sh pins the archive and executable bytes;
   the script asserts the staged guest file equals the pinned executable digest and passes
   `sha256:<pinned>` to the e2e; the e2e requires the daemon-reported `native_binary_sha256` to
   equal it; the exporter requires the manifest's `native_binary.sha256` to equal both the CLI value
   and the witness document's value. A mismatch anywhere fails closed.
6. **`native_open_reached` cannot be recorded `false`.** `recordContainmentBoundary()`
   (`linux_owner_witness_linux.go`) is the only writer of the boundary and always records `true`;
   an unreachable credential-free `Open` fails the case with an error and writes no summary, so
   there is no launch-only report path. The witness *requires* `true` for these cases so a
   regression cannot silently downgrade the proof.

## 4. Off-host verification already performed (evidence for §5)

- Pins re-derived from the GitHub release API and from the downloaded bytes: archive SHA-256 checks
  passed for all four archives in WSL Ubuntu-24.04; `bin/codex --version` = `codex-cli 0.168.0`,
  `claude --version` = `2.1.323 (Claude Code)`; helper shas equal their standalone archive shas.
- Codex credential-free `initialize` + `thread/start` against the pinned app-server reproduced the
  daemon's exact request shape: response carries `model: gpt-5.1-codex`, `modelProvider: openai`,
  `approvalPolicy: on-request`, `approvalsReviewer: user`, sandbox `workspaceWrite` with the exact
  writable-root/network/exclude flags, `thread.ephemeral=false` — every `threadStartResponse`
  post-condition `codex/adapter.go:462-476` asserts.
- Claude stays live with the adapter's fixed argv and a placeholder credentials file (10 s probe,
  no `.claude.json` needed).

## 5. Evidence plan (judging artifact: `answer.md` in the GDD run dir)

- Contract-clause table with the exact command, exit code, and the evidence row/proof for each
  clause; the dispatch is recorded as **not run** at this stage (implementation stage cannot
  dispatch; the post-merge stage owns it) — never as passed.
- Gate matrix (off-host, recorded with real output and exit codes): `bash -n`, `shellcheck
  --severity=warning`, `actionlint` (record absence otherwise), the five `python3
  scripts/ci/test-linux-v2-*.py` fixtures + `test-ci-structure-extraction.py`, Elixir fixture
  arithmetic (44 → 48 with the row-count derivation), `gofmt -l`, `go vet ./...`,
  `GOOS=linux GOARCH=amd64 go build ./...` + `go vet` on the three surfaces,
  `GOOS=linux GOARCH=amd64 go test -list 'NativeContainment$' ./e2e/` (compiles the linux-only
  e2e files and lists the two new entry points), `go test -count=1 ./internal/app/...
  ./internal/harness/...`, and the WSL race run `go test -count=1 -race ./internal/app/...
  ./internal/harness/...` plus `-run TestLinuxV2NativeWitness` focused runs.
- Red mutant: add `harness.KindCodex` to `requiresLinuxV2NativeOwner` → the new invariance
  assertion reddens (observed output recorded), predicate restored byte-identical afterwards.
  Second mutant on the Python side: wrong `--native-binary-sha256` → exporter refuses to publish.
- Base-vs-HEAD control-flow audit for the touched witness/script paths: decision-point table for
  `validateLinuxV2NativeWitnessConfig`, `linuxV2NativeWitnessCaseMatches`, the `launchNative`
  branch tree, the codex gates, `verifyNativeExecutable`, `run_stage_replay_case`, and the
  case-dispatch arms, with the `git diff` hunk count.
- The per-kind dispatch command sequence (for the post-merge stage): pre-check the protected
  environment, `gh workflow run linux-native-v2-acceptance.yml --repo wxxb789/symmetry --ref main`,
  the `jq -e` predicates from the contract, recorded as the next stage's work.

## 6. Scope additions (recorded, with alternatives)

1. **`daemon/internal/harness/claude/adapter.go` (+ its test).** The witness's exact-target gate
   (`validateNativeTargetSummary`, `linux_owner_witness_linux.go:969-976`) requires
   `Target.PIDFDVerified`/namespaces, which only the launch-transfer seam inside `daemon.start`
   produces; only the codex adapter honors `StartRequest.StartProcess` today
   (`codex/adapter.go:185-195`), so a claude containment case cannot be green without the same
   optional seam. Alternatives rejected: a witness-side pidfd capture (no process handle reaches
   the witness, and reopening a pidfd cannot prove launch transfer); weakening the target gate for
   containment cases (contradicts contract clause 4 and quality policy). The addition is the
   codex-shaped 12-line seam plus one focused test; it changes no production behavior (the seam is
   nil outside the witness).
2. **`scripts/ci/register-linux-v2-claim-machine.py`.** The Control scheduler can only assign the
   new Work items to registered runtimes; the fixture's `schedule` requires a runtime whose
   `agent_profile`/`repository_resource_id` match the case, so two generic claim runtimes
   (`codex-native`, `claude-native`, opencode repository resource, no `provider_access`/`adapter`
   capability) must be registered with the existing two. Alternative rejected: reusing the
   opencode profile name for a codex runtime would keep the file untouched but record a kind
   lie in every fixture request and evidence row.

Both additions are CI/witness-support surfaces inside the conductor's codex+claude containment
scope; neither touches `daemon/internal/config/**`, `daemon/internal/linuxpreflight/**`,
production routing, admission, composition, or any frozen opencode byte.

## 7. Out of scope (with reasons)

- opencode re-pin or adapter registration — not authorized by this slice. Owner ruling F2
  (2026-11-22, binding) excludes the frozen opencode leg from the per-kind green gate of the D3
  merge train while W6-04/D1 freezes its acceptance-case bytes: this slice changes neither the
  leg's bytes nor its case, and a red frozen opencode leg in a dispatched run is recorded as the
  F2-excluded leg instead of being edited. Its fail-closed state is existing production behaviour
  asserted by `TestNewHarnessRegistryBindsNativeAdaptersAndUnsupportedOpenCode`
  (`harness.KindOpenCode` resolves to `*harness.UnavailableAdapter`) and
  `TestStartupProbeRegistersUnavailableNativeProjections` (a start is refused with
  `ErrHarnessUnavailable`) in `daemon/internal/app/harness_test.go`.
- omo/omp/generic case sets, `validateRuntimeWorkspacePolicy` changes, linuxpreflight hand-over
  (conductor split → W5-02b).
- The predicate flip, legacy-unreachable proof, platform reachability test (W5-02); legacy
  deletion (W6-05); `docs/design/harness.md` (the amendment PR); capability promotion; Windows
  surfaces; rootless-compose witnesses; `daemon/internal/harness/testdata/**`.
- The root `timeout-minutes` stays 180 unless the first dispatch measures otherwise (F5 lever (a))
  — a merge-train decision recorded with the run's per-set timings.

## 8. Verification limits at this stage

The only full proof is the dispatched workflow on a qualifying host after merge + rebase; this
stage records it as not-run. The witness suite itself is env-gated and is exercised only by the
dispatch. WSL (Ubuntu-24.04, Go 1.27.0) is the Linux-surface oracle; the Windows host is 1.27.4.
`actionlint` availability is recorded when the gate runs.
