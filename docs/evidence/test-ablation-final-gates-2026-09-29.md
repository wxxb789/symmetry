# Final local project-ablation verification

Date: 2026-09-29. Branch: `codex/goal-0006b-crash-safe-process-containment`; HEAD `d14debbf6ae3ded09e5013ef90952d347c01474d`, dirty. No commit or push was made. This records local source snapshots, not exact-commit CI or native-provider acceptance.

## Bound final source

| Path | SHA-256 |
|---|---|
| `daemon/internal/app/app_test.go` | `cc79ef49f5da5d056e980bdbeea3b01e866f609c5242b70824f0276e835b9c85` |
| `control/test/symmetry_control/goals/contract_validation_test.exs` | `3d71579ebda251a4f20b4902f63620f7dae8669293d4997f0bdef2ca175b9100` |
| `control/test/symmetry_control_web/controllers/goal_controller_test.exs` | `d81e5f32e0a7b5f3d51b2e574b4aa10e4260afb515fb265bc6564698a32f3c4e` |
| `scripts/ci/assert-go-test-json.py` | `14e658cab38af077348ff3c74f1786d38eb91b59dfee0f80e805ff51440a2e0a` |
| `scripts/ci/test-linux-v2-go-events.py` | `dc3f4fe338de0f7460693a9d7f0057e98d2484a9d33ff32f45fe70ba7672f4d0` |
| `.github/workflows/ci.yml` | `d549e76805589c93fc9d1f1fe5bc18f378861000c92f02ac4b2ba7778921a453` |
| `control/lib/symmetry_control/goals/contract_validation.ex` | `5b5170dd95621b43c1eb0b3ddc36ab5bf8a65017af8747ebbffcbf4640e5e755` |
| `control/lib/symmetry_control_web/plugs/strict_evidence_json.ex` | `8d52c3c189e3d2cd7f33089f66965e0a7fbd888400da96c31285cfece5c05ab9` |

Both Control production modules remained byte-identical to their pre-ablation hashes. The final classification mutant existed only in isolated BEAM processes; no production source file or fixture corpus was changed by that experiment.

## Final Go App package checks

The only Go test edit in the ablation phase was the startup-test consolidation and its bounded context guard. Other Go packages retain the earlier complete ordinary/race evidence and source boundaries in [the resumed report](goal-0006b-resumed-go-gates-2026-09-29.md). No old full-suite result is relabeled as a fresh exact-commit run.

Native Windows, from `daemon/`, with `GOMAXPROCS=2`:

```text
go test -p 2 -count=1 -timeout 1200s -json ./internal/app
```

Exit 0: 897 test pass events, one platform skip, final package pass in 193.001 s. No fail events, malformed lines or stderr.

WSL `symmetry-race`, from `/mnt/q/repos/symmetry/daemon`, with `PATH=/opt/go/bin:/usr/bin:/bin` and `GOMAXPROCS=2`:

```text
/opt/go/bin/go test -p 2 -race -count=1 -timeout 1200s -json ./internal/app
```

Exit 0: 1,041 test pass events, eight prerequisite/platform skips, final package pass in 812.237 s. No fail events, malformed lines, race report or stderr. Counts include table/parent events.

Windows skip: `TestPreparePiProviderBridgeFailsClosedWithoutLinuxShare`. Linux skips: the five Pi retained-session resume cases, both legacy-owner-drain preflight cases that intentionally reject root, and `TestPreparePiProviderBridgeRunsBoundPeerAndPersistsAction`. These are not passes; their exact names also appear in the preceding complete-suite skip inventory.

## Control error-contract correction and full suite

Independent review found that ten removed assertions had required exact public error tags and tuple details while the shared manifest merely required rejection. The correction retains one explicit table for four hash/identity error tuples, three exact `{:duplicate_predicate_id, "tests"}` results, and the three GoalCreate/provider-scope/equal-branch atom reasons. The two otherwise-valid HTTP survivors use canonical routes; the portal body retains an escaped duplicate member name.

The counterexample used the original post-ablation test snapshot SHA-256 `596086083e84a3ee24076c18b31db8717e377c3f3df5f893531b6386277c5ee1`, reconstructed by removing only the new table. Its three fixture-root constants were made equivalent absolute paths for the temporary copy's location. A scratch runner used `Code.compile_string` to make only `validate_goal_create/2` return `{:error, :invalid_goal_create}` instead of `{:error, :deterministic_acceptance_contract}`. Every other result was unchanged.

With `MIX_ENV=test`, private `MIX_BUILD_PATH`, and `ERL_FLAGS='+S 4:4'`, the driver ran:

```sh
mix run --no-start /mnt/q/repos/symmetry/.omo/verification/run-control-wrong-tag.exs \
  test/symmetry_control/goals/contract_fixture_manifest_test.exs \
  /mnt/q/repos/symmetry/.omo/verification/control-pre-error-restore_test.exs
mix run --no-start /mnt/q/repos/symmetry/.omo/verification/run-control-wrong-tag.exs \
  test/symmetry_control/goals/contract_fixture_manifest_test.exs \
  test/symmetry_control/goals/contract_validation_test.exs
```

Each runner invoked `Mix.Task.run("test", ["--no-compile", "--seed", "0" | System.argv()])` after installing the in-memory mutant. The prior checks missed the defect: 12 tests, 0 failures, exit 0. The corrected checks detected the wrong public tag: 13 tests, one failure, exit 2, with actual `{:error, :invalid_goal_create}` versus expected `:deterministic_acceptance_contract`. This is expected-red mutation evidence, not a passing production test.

A preliminary scratch-runner attempt had a raw-sigil delimiter syntax error and exited 1 before any mutant ran; its fixture was disposed. The delimiter was fixed and `Code.string_to_quoted!` returned `SYNTAX_OK` before the accepted experiment. No failed setup was counted as a mutation kill.

A fresh unmutated BEAM process then ran the final complete Control suite:

```sh
mix format --check-formatted
mix compile --warnings-as-errors
mix test --seed 0
```

All exited 0. Final ExUnit result: **800 tests, 0 failures, 20 skipped**, 366.0 s. The earlier pre-correction 799-test pass is historical and did not settle the review finding. The final environment was Elixir 1.19.6, OTP 28 and PostgreSQL 17.11 in WSL, not CI's newer runtime matrix.

The cluster was created exclusively for this run at `/tmp/symmetry-ablation-final.Cgnkmf`, listened only on `127.0.0.1:55443`, and used database `symmetry_ablation_final_01a0ed48` plus a private Mix build directory. The process stopped successfully and the owned directory was removed; `CONTROL_FIXTURE_DISPOSED` was observed. No existing database/service was modified.

Warnings were visible: in-memory module redefinition during the two mutation processes, plus pre-existing unused variables/helper defaults and repeated migration-module loading in unchanged tests. Production `mix compile --warnings-as-errors` passed. No warnings or tests were suppressed.

The three parent-owned temporary files used by this final experiment were removed after their contents were checked: the relocated prior test copy (`3a79d44b409355d91c53fa0f47117f2fb4f167c409b78b01e58e9050adc6ddac`), in-memory mutant runner (`e42158c003bb7c083a20603510fccf35426af114e63fa96618007fb486574024`), and database/test driver (`0c8f2d20cb89aad2fc00827b536ab02876aa213c692622c71e20630e3dc9f9a9`). Commands above record the actual experiment; those scratch paths are not shipped harnesses. Existing historical evidence and user-owned artifacts were not cleaned up.

The final [independent re-review](test-ablation-final-review-2026-09-29.md) matches these source hashes and reports no remaining local finding. Its scope does not waive the environment and acceptance limits below.

## Other completed checks

- Parent `python -X utf8 scripts/ci/test-linux-v2-go-events.py`: six tests, exit 0, 4.295 s.
- Complete contract gate after exact mutation restoration: `pnpm contracts:check`, exit 0; typecheck, 27 Node tests, generated drift, 14 schemas/184 AJV fixtures and 184 Effect raw decodes passed. Direct `node --test contracts/scripts/*.test.mjs` also passed 27 tests.
- Both package-scoped peer gates passed the actual recorded ordinary Go stream, with strict synthetic missing/duplicate/skip/failure/malformed/wrong-package cases rejected.
- Four inline-parser replacements passed controlled Bash/PowerShell exit and event matrices. The actual production Windows containment witness ran once: Go/helper exit 0, exact run=1/pass=1/skip=0, test elapsed 12.32 s. The other credentialed native commands were not run or claimed passed.
- YAML parsing and `git diff --check` passed. Missing Go/Elixir/Python/YAML/Markdown language servers are not reported as successful diagnostics; compilers, executable checks and review provide the recorded local evidence.

## Remaining boundaries

Docker engine absence prevents Compose/browser runtime ablation; those checks were retained. Qualified A/B systemd/native-provider inputs, the CI-version platform matrix, remote CI and an authorized clean exact commit remain unavailable. Historical evidence, samples of unresolved ownership, process journals, physical stop receipts, platform distinctions and all native witnesses were preserved. Goal 0006b remains active and is not accepted by this report.

