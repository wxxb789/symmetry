# Goal 0008c: evidence-based Elixir model routing

Status: produced and awaiting the owner's verdict. This record was written by the implementing
model; it is not owner acceptance and issues no `achieve`.

> Work in progress: the surface-proof section is final; the per-run receipt sections are
> appended as each run is accepted. The final closeout section states the residual limits.

## Contract and source provenance

The binding contract is [`docs/goals/archive/0008c-elixir-model-routing.md`](../goals/archive/0008c-elixir-model-routing.md),
including its standalone Done-when and the owner-acceptance boundary after it. The increment starts
from commit `5c2e0ada6a3fd669c5ed2d2bc4b50ff9fed3b12a` (tree `54071e2b1a931e039ac3d53611e2b9fdcab962cb`) on the
existing feature branch with a clean working tree; no commit, push or history rewrite was performed
during the evaluated runs, and only this worktree was touched.

Shape reference: the merged 0008a increment (`1c6c715`, audit
`docs/evaluations/model-routing/0008a-audit.json`), reusing its runner, source seal, receipt and
manifest format. This increment adds no service, provider profile, credential, spend cap, savings
claim or routing-policy promotion. It reuses the existing configured worker/reviewer routing and
authorization (`reuse_existing`) and records the routes it observed.

Private evidence root for this run (outside the repository, coordinator-owned):
`C:/Users/lhan/AppData/Local/Temp/symmetry-0008c-eval`. Raw transcripts stay outside Git.

## Surface proof (blocking prerequisite, executed before either run)

An isolated clone of the frozen base was materialized the same way `prepare` does it
(`git clone --no-hardlinks` with `core.autocrlf=false` and `core.eol=lf`, then `git checkout
--detach 5c2e0ada…`) and the gates CI runs for Elixir were executed in it against a real
PostgreSQL:

| Command (cwd `control/`) | Observed result | Exit |
| --- | --- | --- |
| `mix deps.get` (deps materialized outside the checkout) | dependencies resolved for the pinned `mix.lock` | 0 |
| `mix format --check-formatted` | no file reported unformatted | 0 |
| `mix compile --warnings-as-errors` | project and deps compiled in 66.1 s | 0 |
| `mix test` | `851 passed, 21 skipped`, `Finished in 511.1 seconds`; wall `8m39.057s` | 0 |
| `mix test test/symmetry_control/provider_broker_e2e_test.exs` with `SYMMETRY_PROVIDER_BROKER_E2E=1` | `4 passed`, `Finished in 69.2 seconds`; wall `2m32.248s` (includes the gate's real `go build` of the daemon and fake agent) | 0 |
| `mix test test/symmetry_control_web/controllers/goal_machine_controller_test.exs --only goal_receipt_e2e` with `SYMMETRY_GOAL_RECEIPT_E2E=1` and a fresh `POSTGRES_DB` matching `symmetry_receipt_e2e_[0-9a-f]{32}` | `1 passed, 10 excluded`; wall `0m26.933s` | 0 |
| `mix test test/symmetry_control/integrations/command_test.exs` without PowerShell 7 on `PATH` | `2/3 passed`; the provider-token case fails with `{:error, {:command_unavailable, "pwsh"}}` | 2 |
| same focused file with portable PowerShell 7.7.0 first on `PATH` | `3 passed` | 0 |

Measured execution surface of that proof (the same surface every evaluation run's `environment`
field states): Windows host (NT 10.0.26300, x64); Elixir 1.20.4 on Erlang/OTP 29 (erts-17.1.1)
selected through the host toolchain manager by exporting `MISE_ELIXIR_VERSION=1.20.4-otp-29` and
`MISE_ERLANG_VERSION=29.1.2`, so the frozen gate argv stays the plain CI commands; Go 1.27.4 for
the integration gates' own `go build`; PostgreSQL 18.6 (`postgres:18.6-alpine`, the CI image) in a
dedicated container published on `127.0.0.1:55461`; `MIX_DEPS_PATH`/`MIX_BUILD_PATH` outside the
checkout so no build output enters the sealed manifest; the repository's own built frontend bundle
staged at `control/priv/static/portal/` (the portal tests read the built entry through
`Application.app_dir/2`, and the bundle is therefore an allowed path in the frozen tasks).

The suite's own environment dependency is real and was reproduced rather than assumed: on this host
`CommandTest`'s provider-token case fails without a working `pwsh` (the store alias is a 0-byte
stub) and passes with a real PowerShell 7 on `PATH`. No gate was weakened, skipped or re-scoped:
the surface is green with the real toolchain, real PostgreSQL and real bundle.

## Frozen classes and inputs

The class list was fixed before either arm ran, together with the task bytes, gates, allowed and
protected paths:

| Class | Task file | SHA-256 |
| --- | --- | --- |
| `elixir-established-pattern` | `docs/evaluations/model-routing/tasks/0008c-elixir-sync-identity-loss.json` | `bfbb2d43a2f2e5253eaa1f00dba6b9c81352970764e60fe7ba9ca8ba7e03cf72` |
| `elixir-db-concurrency-authority` | `docs/evaluations/model-routing/tasks/0008c-elixir-sync-inflight-marker.json` | `45972aa8121ba9c0850fc73e9ef6f4d4679b8ee8c7e13fedc1a4f763de721cb7` |

Both classes cover the contract's required subjects: an established-pattern production edit against
a named database constraint (`project_resources_sync_status_check`), and a
constraint/transaction/rollback/concurrent-writer/authority change with a notification-ordering
assertion on the periodic syncer. Frozen gates per class (identical for both arms): `format`,
`compile`, `focused-tests` (with ExUnit named tests), `provider-broker-e2e`,
`goal-receipt-e2e` (with its own fresh database) and `full-suite`.

Worker prompt identity (byte-identical across the two arms of each class; the model is selected by
the harness, not by the prompt): `a-packet.md` `dd5338058c190538a31d98c017ab610517149f1895eb5628f0da6f7a860e52b4`
for the identity-loss class and `b-packet.md` `2b37954b05fdb9b01ef993a8d128eb970874c1071adba5aaaf500eec2006ed48`
for the in-flight-marker class. The in-flight-marker packet was regenerated from the amended task
file before either class-B arm was sealed; the superseded packet (`f9795c5fc7917391f478cccfd4f990b751617f3e89e527c7e1e69087caa10c24`)
belongs to the two charged class-B failed attempts described under the contract correction below.

## Runner extension (in place, with regression tests)

The 0008a runner gained exactly what the Elixir carrier needs, so no second measurement pipeline
exists:

- gate fields `exunit_tests` + `exunit_files`: the runner derives `mix test --trace --seed 0 <files>`
  in the gate's cwd and environment after a passing gate command and refuses the gate unless every
  named test is printed exactly once as a finished ExUnit observation, the derived run exits 0 and
  its summary reports zero failures. This closes the vacuous `0 tests, 0 failures` pass the plain
  exit code allowed. Its residual limit (a `test_helper.exs` that prints ExUnit lines itself) is
  recorded in the review instructions as a review duty, exactly like the Go `-list` limit;
- gate field `env`: non-secret flag overrides applied to that gate's process and recorded in the
  gate result and the receipt (the CI integration gates need `SYMMETRY_PROVIDER_BROKER_E2E=1` /
  `SYMMETRY_GOAL_RECEIPT_E2E=1`, and a skipped tagged test would otherwise be a vacuous pass);
- `prepare --environment-json PATH`: records the measured, non-secret execution surface in the run
  record's `environment.measured`, which every receipt row then states. Secret-shaped keys are
  refused in both new fields.

Two implementation assumptions were wrong until the real surface disproved them, and both were
corrected before any run started:

- Elixir 1.20.4 prints `  * test <description> (<n>.<d>ms) [L#<line>]` (the literal `test ` prefix,
  a carriage return before the line, ANSI wrapping on colour terminals) and prints
  `  * test <description> (skipped|excluded) [L#<line>]` otherwise, with a test declared inside a
  `describe` block rendering as `<describe> <name>`. A first-parser version without the `test `
  prefix matched nothing; the frozen tasks now also state that the required tests must be declared
  at module level. The corrected parser was verified against four real captured logs (passing file,
  probe with a skip and a `describe`, deliberately failing file, full suite).
- `mix test` owns the summary, not ExUnit: the measured shapes are `Result: 3 passed`,
  `Result: 2 passed, 1 skipped`, `Result: 851 passed, 21 skipped`, `Result: 1 passed, 10 excluded`
  and, on failure, `Result: 1/2 passed` plus `Failed: 1 test`. The failure count is taken from the
  `N/M passed` form when present so the restated `Failed:` line cannot double-count.

One gate is deliberately *without* the derived ExUnit cross-check: the goal-receipt integration
gate. Its test writes committed rows through `Sandbox.unboxed_run/2` and asserts that eleven tables
are empty at the start, so a second execution of the same file against the same database fails —
measured, not assumed: a repeated run of that gate returned `Result: 0/1 passed, 10 excluded`,
`Failed: 1 test`, exit 2. Vacuity is closed structurally for that gate instead: the test file is a
baseline test and `config/**` is protected, so the seal refuses any edit that could empty the
`--only goal_receipt_e2e` selection, and the frozen argv plus the recorded Mix summary
(`Result: 1 passed, 10 excluded`) are stored as gate evidence.

Before any accepted work, the first launch of both class-A worker sessions was aborted because the
frozen task bytes and the worker packet were still being corrected; the two killed sessions
(`01a1e61e-8b7e-706f-b279-5bf1f6d7e2d0`, `01a1e61e-8e8e-75b3-bfb3-25d7ac114cdc`) produced no source
change and are charged in the ledger as failed attempts with their observed usage and harness
estimates (USD `0.033549` and `0.2531725`, 91.9 s and 104.3 s), never as zero and never as a bill.

The extended tooling is byte-frozen for the whole evaluation: `scripts/ci/model-routing-evaluation.py`
`sha256:932290d7f76f5bd18e2778cd468b88b6852ba9a10721ae997bc10325db190ef5`,
`scripts/ci/test-model-routing-evaluation.py` `sha256:0ef724556bdf604cc334742fed44d0d5ee882f5aabbc8b3d5398c725c34c87ea`,
`scripts/ci/assert-go-test-json.py` `sha256:14e658cab38af077348ff3c74f1786d38eb91b59dfee0f80e805ff51440a2e0a` and
`scripts/ci/linux-v2-source-subject.py` `sha256:18ed26fdc97316ea3361cc05f8ac855836e4da130e2284c826d45c6ae2b5294e`.
Each receipt records those digests in `evidence.tooling_sources`, the digest on disk still matches
every accepted receipt, and the runner digest named there is the same file that executed all four
runs — no gate was re-run under different tooling bytes.

## Runs

## Closure and simplification

The contract moved to `docs/goals/archive/0008c-elixir-model-routing.md` without a text change:
5,528 bytes, SHA-256 `376e1b3c76ff636a22abd7013b5a4b3e58adc572370369d068a1bdd70c5ef1d8`, byte
identical to the active copy (the same digest and size before and after the move, and the active
path is now absent). The goal index row points at the archived file, and the four documents that
referenced the active path — this record, the candidate policy, the plan and the index — were
updated in the same change so no link dangles.

The accepted strong-only artifacts were integrated as the policy's fixed rule requires: the two
sealed diffs (`sha256:345ca2bd8a6f7bd0cd8fd55b7c8d671fbd61d0faa9fe7d4af76fe2f554769bb3` for class A,
`sha256:c9d05cc34ee88cc5ba5db34fcbb75d3695470634a64a4c0486f9ac2a7950ea62` for class B) applied in
order, the two accepted focused test files copied at their sealed digests
(`sha256:69abc923dde6a55d6636aa62869855194adbf6f640269c6ddebce4f2f3531418` and
`sha256:8e03ed4230156a0b323809add0caaa4496ff79982fcac0c07efd484ebd0cc2c8`), and the provisioned
frontend bundle staged with the index digest the sealed runs declared (`6ac43ef4…`). The mixed arms'
revisions stay sealed evaluation evidence with their own Subjects and receipts, exactly as the rule
says.

Scoped simplification: the only repository code this increment owns is the Elixir extension of the
0008a runner, its regression tests and three instruction documents. That extension was simplified
before the runs (the first parser version that matched no real ExUnit line was replaced by the
measured shape, and the vacuous-pass closure was merged into the existing gate pipeline instead of a
second runner); after the runs it cannot be simplified further without changing bytes whose digests
every accepted receipt records, which would invalidate the evidence. The integrated artifacts are
accepted subjects and are likewise frozen. No wrapper layer, duplicate cache or second mechanism was
added: the runner, the source-subject seal, the receipt/manifest format and the review route are the
0008a ones.

### Contract correction before any class-B acceptance

The first class-B strong-arm session did not implement: it escalated with failing evidence, and the
escalation was correct. The frozen class-B requirement (commit the `syncing` marker before the
provider call) conflicts with the existing protected baseline test
`control/test/symmetry_control/integrations_test.exs` "a failed provider call cannot update resource
health after the project is archived" (lines 1061-1109): that test blocks the provider call, archives
the project, releases the provider, expects `{:error, :state_conflict}` and then asserts the resource
still has `status == "unknown"`, `sync_status == "unknown"` and `status_message == nil`. A marker
committed before the provider call would leave the row at `"syncing"`, so the task as first frozen
was unsatisfiable without weakening a protected test — which the contract forbids. The coordinator
verified the conflict by reading the protected test and the production path
(`require_active_project/1` refuses the finalization transaction, so the marker would survive).

Because no class-B result had been accepted, the class-B task was corrected (the class list and both
arms are unchanged, and both arms of class B run under the corrected bytes):

- the amended task adds one rule with its own named test: a finalization refused because the project
  is no longer active must roll the marker back to the resource's own pre-sync `sync_status` and
  `last_checked_at` under the same optimistic-lock/CAS rules, without touching any other field, so
  the protected archive-race test keeps passing. That is a real correctness property (a resource
  must not advertise an in-flight state for a sync that can never reach a verdict), not only a
  conflict workaround;
- the focused acceptance now names five tests instead of four.

The amended class-B task file's SHA-256 is
`4bf7eb0c7bb81dc73cd535e714ee067afd006b175c2c88fff6184742094bb7b3`; its worker packet is
`b-packet.md` `2b37954b05fdb9b01ef993a8d128eb970874c1071adba5aaaf500eec2006ed48`
(byte-identical for both class-B arms). The superseded sessions are charged
as failed attempts with their observed usage: `01a1e626-92b8-7355-937f-121ecd3260ee` (lightweight,
aborted by the coordinator when the task changed: USD `0.0893787` estimate, 231.8 s) and
`01a1e626-9776-7152-9583-c918dd0137ed` (strong, the escalation itself: USD `0.2991183` estimate,
171.6 s). Neither produced an accepted change. The strong session's transcript ends with a
stop-and-escalate answer that names the protected test's own assertions
(`integrations_test.exs:1062-1107`, `assert unchanged.sync_status == "unknown"`) and the
`update_project/2` path that never clears a resource marker, and it modified no file; the
lightweight session was aborted before it produced an answer, so its abort is charged as a failed
attempt but is not evidence about that model's behaviour.

Per-run sections with the executed gate tables and the accepted receipts follow; each was written
only after that run's `accept` returned 0.

### Observed negative controls

These were run as disposable coordinator controls; they are not additional accepted samples.

| Control | Observation |
| --- | --- |
| Prepared checkout with no worker change (`dryrun-control`, Subject `sha256:9c73313f04ed0691f5295b1b5bead62410728718b15baa08be619dbc5035419e`) | `format` exit 0, `compile` exit 0, `focused-tests` exit 1 with the derived `mix test --trace --seed 0 test/symmetry_control/resource_sync_identity_loss_test.exs` recorded and no trace evidence (the gate command itself failed first); attempt `complete=false`, `check` exit 1 |
| Goal-receipt gate repeated against the same (already populated) database | `Result: 0/1 passed, 10 excluded`, `Failed: 1 test`, exit 2 — the gate is not vacuous, and it is why each check drops that gate's database first |
| Focused gate whose required test file omits the named tests | A prepared checkout carrying only a stub test with a different name and a true assertion (`control/test/symmetry_control/resource_sync_identity_loss_test.exs`, Subject `sha256:c5d4643aa22fbf8fb281e0a73b55e8659585e7084df112f6527407a8e570c2a9`) had the gate command itself pass (`exit_code: 0`, stored stdout `Result: 1 passed`) and the derived trace pass (`exunit_exit_code: 0`, `Result: 1 passed`), and the harness still refused the attempt: `passed: false`, `exunit_error: "the traced ExUnit run did not execute every named test: missing a not-found provider response persists stale synchronization with the not-found message"` (attempt `attempt-0002`, evidence `sha256:fc100c624dc22da273bcfe201865bd782a8b456b3433b7b9f8a5b5c96d549150`) |

### Sealed revisions and observed model usage

All four revisions were sealed from their own isolated checkout at the frozen base, each carrying
the staged `control/priv/static/portal/**` bundle (index.html
`sha256:6ac43ef4f6ab4b35e8af21136b8c02f6b7765e52aae3ad0d1a65f0147b75a463`) and the same changed-path
shape: the production file, the new focused test file and the three bundle files.

| Run | Task SHA-256 | Routes SHA-256 | Subject | Changed production artifact |
| --- | --- | --- | --- | --- |
| class A mixed | `bfbb2d43a2f2e5253eaa1f00dba6b9c81352970764e60fe7ba9ca8ba7e03cf72` | `sha256:a4dd5ffc9630b5df55f45a339e79816b3f45c5fa06c5f7cfdca58f18ba24fcf8` | `sha256:5c8b1a800a17acb7231545ce3168b04f84a3fc3d21f6608e6ea6077f105d2ddc` | `integrations.ex` +11/−1 (helper `failure_sync_status/1`), 338-line test file |
| class A strong | same | `sha256:7e43051f313012e4804dbd4a3adffc4d85576d1137a37421de157ce353a327eb` | `sha256:09dd5f8840f3d3fed81cc4c22e1fe7f9a981c78c941b27de8309f7b5643999af` | `integrations.ex` +1/−1 (inline `if code == :not_found`), 201-line test file |
| class B mixed | `4bf7eb0c7bb81dc73cd535e714ee067afd006b175c2c88fff6184742094bb7b3` | `sha256:a4dd5ffc9630b5df55f45a339e79816b3f45c5fa06c5f7cfdca58f18ba24fcf8` | `sha256:986bbd6e8818d128a96dc84eb38f1267e134041e63dd5f562674e27217386864` | `integrations.ex` +76/−11, test file with a provider stub |
| class B strong | same | `sha256:7e43051f313012e4804dbd4a3adffc4d85576d1137a37421de157ce353a327eb` | `sha256:2d98eaec565495be3bafc12559b99f9bc4beb51837fc8faf2ab3b0630bf3f2e1` | `integrations.ex` +62, 270-line test file |

Worker sessions and their observed usage (harness estimates, never provider bills; price versions
unknown; `cached` is `cacheRead + cacheWrite` and is not a subset of `input`):

| Run | Implementer session | Served identity | input / output / cached tokens | Harness estimate | Wall time | Observed prompt hash |
| --- | --- | --- | --- | --- | --- | --- |
| A mixed | `01a1e620-dbb2-744e-a30c-f69dba8051bc` | `deepseek-v4.1-flash-fast` (tencent-litellm, openai-completions) | 108,909 / 47,162 / 2,981,760 | USD `0.0892671` | 438.6 s | `sha256:b2e5f319…` |
| A strong | `01a1e620-dfc8-71f6-9cd4-dc552215c9e5` | `gpt-6.1-sol` (ghc, openai-responses) | 48 / 9,891 / 848,804 | USD `0.321512` | 293.2 s | `sha256:b2e5f319…` (identical) |
| B mixed | `01a1e62a-7c6b-72b5-a9f2-6a5d55d78a02` | `deepseek-v4.1-flash-fast` | 268,410 / 95,175 / 3,320,128 | USD `0.194733` | 525.7 s | (same packet, see below) |
| B strong | `01a1e62a-7f60-76c6-8df5-1cb45f5cf059` | `gpt-6.1-sol` | 54 / 17,325 / 1,130,699 | USD `0.4618031` | 428.6 s | (same packet) |

The class-A prompt identity is byte-identical across both arms: the harness's first user message
digest is `sha256:b2e5f31905bb78eafe964d54f264afd589f6ab8667f3ff84d863deea9ebfc46c` for both the
lightweight and the strong session, and the packet file itself is
`a-packet.md  dd5338058c190538a31d98c017ab610517149f1895eb5628f0da6f7a860e52b4`; the two are
different digests because the harness envelopes the message content as
`[{"type": "text", "text": "<packet>"}]`, which the coordinator reproduced to confirm the packet
bytes are what the session received.

One recording asymmetry between the arms is real and stays visible: the lightweight class-A session
log has entry types `custom_message: 2` and exports three prompt records (the packet user message
plus two `custom_message` digests), while the strong session log has no `custom_message` entry at
all and exports only the packet user message. The packet digest itself is identical and both arms
were dispatched through the same launcher with the same packet file, so the difference is in what
each provider path records, not in what the coordinator sent; the goal's prompt-identity evidence is
the packet digest, and the missing `custom_message` records in the strong arm are recorded here as
an unverified context-envelope difference rather than claimed equal.

### Run: class A mixed (task `0008c-elixir-sync-identity-loss`, route `task.agentModelOverrides.sonic` -> `modelRoles.flash_max`)

Sealed Subject `sha256:5c8b1a800a17acb7231545ce3168b04f84a3fc3d21f6608e6ea6077f105d2ddc` (1,198 files;
changed: `integrations.ex` modified by adding a `failure_sync_status/1` helper, the new focused test
file and the staged bundle added). The mixed arm factored the decision into a helper while the
strong arm inlined it, and both revisions are accepted on their own evidence — the contract fixes
behaviour, not code shape, so the two arms are not required to produce identical diffs.

`attempt-0001` is both the first and the accepted attempt: `complete=true`, `passed=true`, attempt
evidence `sha256:0207588d45c03ae0ace87c3eb4873c49f703a914c81eb60de5db1d9c8101eab6`, `check` exit 0
in 747.7 s. One operational note: while this check was mid-flight the coordinator briefly moved the
in-progress attempt directory outside the store as part of quarantining crash remnants and restored
it in the same minute; the gate that was running at that moment (`provider-broker-e2e`) completed
with its full evidence, the attempt record digests recompute cleanly, and no gate result was lost
or rewritten.

| Gate | argv | Exit | Observed |
| --- | --- | --- | --- |
| `goal-receipt-e2e` | `mix test test/symmetry_control_web/controllers/goal_machine_controller_test.exs --only goal_receipt_e2e` (`POSTGRES_DB` from the task, `SYMMETRY_GOAL_RECEIPT_E2E=1`) | 0 | stored stdout `Result: 1 passed, 10 excluded` |
| `format` | `mix format --check-formatted` | 0 | empty stdout, no unformatted file |
| `compile` | `mix compile --warnings-as-errors` | 0 | clean |
| `focused-tests` | `mix test test/symmetry_control/resource_sync_identity_loss_test.exs` + derived `mix test --trace --seed 0 …` | 0 / 0 | all three frozen test names printed exactly once with timings, `Result: 3 passed`; trace stdout digest `sha256:f7e02f560a232deee4d3e1258e3a75ddace43642b76d23c14b73b36c3c9f16eb` |
| `provider-broker-e2e` | `mix test test/symmetry_control/provider_broker_e2e_test.exs` (`SYMMETRY_PROVIDER_BROKER_E2E=1`) + derived trace | 0 / 0 | all four frozen names observed, `Result: 4 passed`; trace stdout digest `sha256:a975ba0100b1d35a45adcf2e12842098e301fc7141f33c6384070abcb6a0fbcc` |
| `full-suite` | `mix test` | 0 | `854 passed, 21 skipped`, `Finished in 513.2 seconds` — the base suite's 851 passes plus the three new named tests |

**Acceptance.** The independent review recorded against that exact Subject returned `verdict: pass`,
`gate_weakening: false` and four `info` findings, all `resolved` — including a named-test integrity
check and two classification observations the policy record carries as negatives (the widened
provider-action projection and the fact that no consumer currently distinguishes `stale` from
`failed` in the read model's attention summary). Reviewer session
`01a1e643-f00e-73cc-9416-1285963e95ed` (`tencent-litellm/deepseek-v4.1-flash-fast`, thinking `max`);
frozen review `docs/evaluations/model-routing/0008c-a-mixed-review.json`
(`sha256:2ef575e2a939cd88e88971e148119883995fe4b7941b8723a22bc9d5e94f0175`).
Receipt `docs/evaluations/model-routing/0008c-a-mixed.json`
(`sha256:9432545d54eca14346e24a42c37821915dc663e0bd5c6b89e36571e242a01e31`) with
`accepted: true`, no blockers, no later defects: implementation
`deepseek-v4.1-flash-fast` 108,909 / 47,162 / 2,981,760 tokens and USD 0.0892671 estimated; review
112,426 / 36,428 / 1,284,992 tokens and USD 0.0774414; one failed attempt
39,094 / 18,184 / 837,952 tokens and USD 0.033549; run totals 260,429 input /
101,774 output / 5,104,704 cached tokens and USD 0.2002575 estimated, `accounting_complete: false`.

### Run: class A strong-only (task `0008c-elixir-sync-identity-loss`, route `modelRoles.sol_high`)

Sealed Subject `sha256:09dd5f8840f3d3fed81cc4c22e1fe7f9a981c78c941b27de8309f7b5643999af` (1,198 files;
changed: `integrations.ex` modified, the new focused test file and the staged bundle added).
Three check attempts exist in the store: `attempt-0001` (five of six gates, its coordinator process
was lost when the host dropped two background processes — no gate failure), `attempt-0002` (its
`goal-receipt-e2e` failed with `Result: 0/1 passed, 10 excluded`, `Failed: 1 test`, exit 2 because
that gate's database had not been recreated for the new attempt) and `attempt-0003`, the accepted
attempt: `complete=true`, `passed=true`, attempt evidence
`sha256:001828a548ace6cb0e434e45559269ca1d15bc92257edd8762fa55f87107d3dc`, `check` exit 0 in 772 s.

| Gate | argv | Exit | Observed |
| --- | --- | --- | --- |
| `goal-receipt-e2e` | `mix test test/symmetry_control_web/controllers/goal_machine_controller_test.exs --only goal_receipt_e2e` (`POSTGRES_DB` from the task, `SYMMETRY_GOAL_RECEIPT_E2E=1`) | 0 | stored stdout `Result: 1 passed, 10 excluded` |
| `format` | `mix format --check-formatted` | 0 | no unformatted file |
| `compile` | `mix compile --warnings-as-errors` | 0 | clean |
| `focused-tests` | `mix test test/symmetry_control/resource_sync_identity_loss_test.exs` + derived `mix test --trace --seed 0 …` | 0 / 0 | all three frozen test names printed exactly once with timings, `Result: 3 passed`; trace stdout digest `sha256:04c99a4237b43b83cceda1066bfb44f06b63bce7c067b0676a0c4e3c247bb703` |
| `provider-broker-e2e` | `mix test test/symmetry_control/provider_broker_e2e_test.exs` (`SYMMETRY_PROVIDER_BROKER_E2E=1`) + derived trace | 0 / 0 | all four frozen names observed, `Result: 4 passed` |
| `full-suite` | `mix test` | 0 | `854 passed, 21 skipped`, `Finished in 550.8 seconds` — exactly the base suite's 851 passes plus the three new named tests |

**Acceptance.** `attempt-0003` is the accepted attempt; the independent semantic review recorded
against that exact Subject returned `verdict: pass`, `gate_weakening: false` and four `info`
findings, all `resolved` — the reviewer re-ran the frozen focused gate itself, reverted the changed
line in a byte-identical scratch copy to confirm two of the three tests fail without it, and checked
that the staged portal bundle is provisioned rather than authored. The review session
`01a1e638-4f0a-7600-a0a9-328d243a9c2e` ran `tencent-litellm/deepseek-v4.1-flash-fast`
(`openai-completions`, thinking `max`) — a different provider from the implementing model — and its
frozen record is `docs/evaluations/model-routing/0008c-a-strong-review.json`
(`sha256:9a7127385da08ef1cf76e86906eb539002bfb02948b1a6c03d9b162b150cb85d`).
The receipt `docs/evaluations/model-routing/0008c-a-strong.json` (`sha256:26d8e003419e3e985e29b5a065b8ce4d178a8402c276855a39fdab6998040d7b`)
records `accepted: true`, `acceptance_blockers: []`, `later_defects: []` and the usage ledger:
implementation `gpt-6.1-sol` 48 / 9,891 / 848,804 tokens and USD 0.321512 estimated; review
58,248 / 40,245 / 1,754,240 tokens and USD 0.0657684 estimated; one failed attempt `gpt-6.1-sol`
24 / 6,093 / 354,697 tokens and USD 0.2531725; exploration, verification (772 s of local gate
runtime) and repair carried no model session. Run totals 58,320 input / 56,229 output / 2,957,741
cached tokens and USD 0.6404529 estimated with `accounting_complete: false` (no provider-reported
cost, no price version) — an estimate, never a bill.

### Run: class B mixed (task `0008c-elixir-sync-inflight-marker`, route `task.agentModelOverrides.sonic` -> `modelRoles.flash_max`)

Sealed Subject `sha256:986bbd6e8818d128a96dc84eb38f1267e134041e63dd5f562674e27217386864` under the
amended frozen task `4bf7eb0c7bb81dc73cd535e714ee067afd006b175c2c88fff6184742094bb7b3`:
`integrations.ex` +76/−11 (marker commit under the resource's optimistic lock, one finalization,
rollback of a refused finalization) and a new focused test file carrying a provider stub.
`attempt-0001` was accepted: `complete=true`, `passed=true`, attempt evidence
`sha256:ef343531aee485497dc64f8de8ca08f7cea76753ebea8878c3de22ca9a16df97`, `check` exit 0 in
761.4 s.

| Gate | argv | Exit | Observed |
| --- | --- | --- | --- |
| `goal-receipt-e2e` | `mix test test/symmetry_control_web/controllers/goal_machine_controller_test.exs --only goal_receipt_e2e` (`POSTGRES_DB` from the task, `SYMMETRY_GOAL_RECEIPT_E2E=1`) | 0 | stored stdout `Result: 1 passed, 10 excluded` |
| `format` | `mix format --check-formatted` | 0 | no unformatted file |
| `compile` | `mix compile --warnings-as-errors` | 0 | clean |
| `focused-tests` | `mix test test/symmetry_control/resource_sync_inflight_marker_test.exs` + derived `mix test --trace --seed 0 …` | 0 / 0 | all five frozen names printed exactly once with timings, `Result: 5 passed`; trace stdout digest `sha256:0a55526dd3142caee5052a6d38eda78d2d1aefbbe435773cd3bc3f00d04e4216` |
| `provider-broker-e2e` | `mix test test/symmetry_control/provider_broker_e2e_test.exs` (`SYMMETRY_PROVIDER_BROKER_E2E=1`) + derived trace | 0 / 0 | all four frozen names observed, `Result: 4 passed`; trace stdout digest `sha256:f0f7f0f68451aff4dfbf6f373d02958931f53f59b983ed5527af49626a879a6d` |
| `full-suite` | `mix test` | 0 | `856 passed, 21 skipped` — the base suite's 851 passes plus the five new named tests |

**Acceptance.** The independent review for that exact Subject returned `verdict: pass`,
`gate_weakening: false` and three `info` findings, all `resolved` — `marker-non-authority-by-design`
(the marker never consumes the optimistic-lock version and no reaper/timeout was added),
`rollback-write-scope` (a refused finalization restores only the marker's own two fields) and
`portal-bundle-provenance`. Reviewer session `01a1e64f-c3dc-738a-b363-ca04a808657c`
(`tencent-litellm/deepseek-v4.1-flash-fast`, thinking `max`); frozen review
`docs/evaluations/model-routing/0008c-b-mixed-review.json`
(`sha256:1c9c68cdcdadbbcd7a92bde3afb1f94ffb8184eae150aad859c445d4cd99e310`).
Receipt `docs/evaluations/model-routing/0008c-b-mixed.json`
(`sha256:e79fe17327fe756f2af5580e61a64404c8a6a1b3d8bac4ed3bf15db48a078e40`) with
`accepted: true`, no blockers, no later defects: implementation `deepseek-v4.1-flash-fast`
268,410 / 95,175 / 3,320,128 tokens and USD 0.194733 estimated; review 49,060 / 66,814 / 2,538,688
tokens and USD 0.0948948; one failed attempt 115,573 / 45,589 / 1,566,080 tokens and USD 0.0893787;
run totals 433,043 input / 207,578 output / 7,424,896 cached tokens and USD 0.3790065 estimated,
`accounting_complete: false` (no provider billing, no price version).

### Run: class B strong-only (task `0008c-elixir-sync-inflight-marker`, route `modelRoles.sol_high`)

Sealed Subject `sha256:2d98eaec565495be3bafc12559b99f9bc4beb51837fc8faf2ab3b0630bf3f2e1` under the
same amended frozen task `4bf7eb0c7bb81dc73cd535e714ee067afd006b175c2c88fff6184742094bb7b3`:
`integrations.ex` +62 with no deletions (marker under a project-level `FOR SHARE` plus a resource
`FOR UPDATE` lock, provider call outside every transaction, one finalization, rollback of a refused
finalization, and a module-doc statement that markers are presentation-only and reaper-free) plus a
new focused test file (`sha256:8e03ed4230156a0b323809add0caaa4496ff79982fcac0c07efd484ebd0cc2c8`).
`attempt-0001` was accepted: `complete=true`, `passed=true`, attempt evidence
`sha256:0cc6cb621c0a52e9f40c8002827225fa6c3f7b83c8f23b3506f2326244628bc1`, `check` exit 0 in
672.1 s.

| Gate | argv | Exit | Observed |
| --- | --- | --- | --- |
| `goal-receipt-e2e` | `mix test test/symmetry_control_web/controllers/goal_machine_controller_test.exs --only goal_receipt_e2e` (`POSTGRES_DB` from the task, `SYMMETRY_GOAL_RECEIPT_E2E=1`) | 0 | stored stdout `Result: 1 passed, 10 excluded` |
| `format` | `mix format --check-formatted` | 0 | no unformatted file |
| `compile` | `mix compile --warnings-as-errors` | 0 | clean |
| `focused-tests` | `mix test test/symmetry_control/resource_sync_inflight_marker_test.exs` + derived `mix test --trace --seed 0 …` | 0 / 0 | all five frozen names printed exactly once with timings, `Result: 5 passed`; trace stdout digest `sha256:11e9fdaedb030fbb3c0427632fce115cfbb0fc7f2567243594bda608f8735c23` |
| `provider-broker-e2e` | `mix test test/symmetry_control/provider_broker_e2e_test.exs` (`SYMMETRY_PROVIDER_BROKER_E2E=1`) + derived trace | 0 / 0 | all four frozen names observed, `Result: 4 passed`; trace stdout digest `sha256:06bec132b8cce2db0234c424f9eaa9f3a5b1273259fb2834a0c422aecb72d127` |
| `full-suite` | `mix test` | 0 | `856 passed, 21 skipped`, `Finished in 446.1 seconds` — the base suite's 851 passes plus the five new named tests |

**Acceptance.** The independent review for that exact Subject returned `verdict: pass`,
`gate_weakening: false` and four `info` findings, all `resolved`: `accepted-sync-boundary`,
`marker-rollback-cas` (the rollback is a compare-and-set write on the version the marker observed),
`raw-update-all-bypass` (the marker deliberately bypasses the changeset path, which is what keeps it
from consuming the optimistic-lock version — reviewed and accepted as designed) and `subject-scope`.
Reviewer session `01a1e658-7809-7394-8127-72dcb770fca6` (`tencent-litellm/deepseek-v4.1-flash-fast`,
thinking `max`); frozen review `docs/evaluations/model-routing/0008c-b-strong-review.json`
(`sha256:2918a91d424e19957edde4df87317aa90b73b1bcd4f6562c5e939367c722136f`).
Receipt `docs/evaluations/model-routing/0008c-b-strong.json`
(`sha256:1be7b41985540705653c763f57cfe98f31b219a077f4812a1afe04747b863bf7`) with
`accepted: true`, no blockers, no later defects: implementation `gpt-6.1-sol`
54 / 17,325 / 1,130,699 tokens and USD 0.4618031 estimated; review 58,667 / 48,547 / 2,583,296
tokens and USD 0.0758565; one failed attempt 42 / 4,986 / 752,223 tokens and USD 0.2991183 (the
escalation); run totals 58,763 input / 70,858 output / 4,466,218 cached tokens and USD 0.8367779
estimated, `accounting_complete: false` (no provider billing, no price version).
