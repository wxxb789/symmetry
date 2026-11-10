# CI inline Go-event parser ablation experiment

Date: 2026-09-29  
Inventory candidate: tooling C4  
Subject: dirty working tree based on `d14debbf6ae3ded09e5013ef90952d347c01474d`, after C5 integration  
Decision: **replace four duplicate inline parsers with the package-scoped shared CLI; retain every native command and gate**.

## Scope and invariant

The four candidates were:

1. Linux Pi provider bridge smoke, inline Python.
2. Windows Pi provider bridge smoke, inline PowerShell.
3. Windows OpenCode synthetic integration, inline PowerShell.
4. Production Windows containment witness, inline PowerShell.

Only `.github/workflows/ci.yml` and this report are permanent writes. The helper, its six regression methods, Go/Elixir/contracts sources, and all non-candidate workflow blocks were read-only. Scratch inputs and runners are under `.omo/ablation/inline-parser/`. No dependency install, full suite, credentialed command, external service, Git/index operation, commit, or push was performed.

The required invariant is stronger than the old implementations: the selected test must have exactly one `run`, exactly one `pass`, no exact or descendant skip, and the selected event must belong to the expected package. Malformed JSON and every `Action=fail` event remain fatal globally. The original Go exit remains independently fatal and takes precedence over parser status.

## Bound hashes

C5 reported the pre-C4 workflow hash as:

- `.github/workflows/ci.yml` before C4: `3c5cf2c0976c77089b2da60c0f865d298ea04a29cab68fc24283b6fe7eec42ca`

Final C4 hashes:

- `.github/workflows/ci.yml`: `d549e76805589c93fc9d1f1fe5bc18f378861000c92f02ac4b2ba7778921a453`
- unchanged `scripts/ci/assert-go-test-json.py`: `14e658cab38af077348ff3c74f1786d38eb91b59dfee0f80e805ff51440a2e0a`
- unchanged C5 `daemon` job block, from `  daemon:` up to the next job: `cd2c3956d9e7f6430dd47b536f5d9dbe12832aecf9693fe9f64748b2cd847fbd`
- old/new detection matrix `.omo/ablation/inline-parser/matrix.tsv`: `2e4f42770380211a62e3bf5886646aad5869c56f54b58c189c61d2d47959d1b9`

The workflow decreased from 2,295 to 2,254 lines, net 41 lines. Four parser implementation blocks were removed: one Python heredoc and three PowerShell set/JSON loops. No native test command block was removed. The C5 ordinary Go command, captured stream, two package-scoped peer assertions, Go-exit handling, race command, build, vet, formatting, and fixture-contract step remain textually untouched.

## Old-parser snapshot and corpus

The scratch runners execute the extracted old parser logic rather than a prose model of it:

- `.omo/ablation/inline-parser/old-linux.py` is the Linux Pi inline Python parser.
- `.omo/ablation/inline-parser/old-windows.ps1 -Mode pi` is the Windows Pi parser.
- `.omo/ablation/inline-parser/old-windows.ps1 -Mode set-ignore-invalid` is the common parser shape used by Windows OpenCode and the production containment witness.

Each corpus stream uses selected package `example/selected` and target `TestWitness`. Parent/child skip cases include a valid run/pass as well as the skip, so they distinguish fail-closed skip handling from simple missing-pass handling. The global-fail case has a valid selected run/pass and a foreign-package failed test. Exit 0 means accepted; exit 1 means rejected.

| Corpus | Old Linux Pi | Old Windows Pi | Old Windows OpenCode/containment | Shared helper | Required |
|---|---:|---:|---:|---:|---:|
| exact pass | 0 | 0 | 0 | 0 | accept |
| missing pass | 1 | 1 | 1 | 1 | reject |
| duplicate run and pass | 0 | 0 | 0 | 1 | reject |
| parent skip plus valid pass | 0 | 0 | 0 | 1 | reject |
| child skip plus valid pass | 0 | 0 | 0 | 1 | reject |
| foreign-package global fail plus valid pass | 0 | 0 | 1 | 1 | reject |
| malformed line plus valid pass | 1 | 1 | 0 | 1 | reject |

This is deliberately **not** byte-for-byte equivalence. The old set-based parsers erased duplicate counts, all old parsers ignored skip events, Linux/Windows Pi considered only a selected-test fail, and the OpenCode/containment parsers ignored malformed lines. Consolidation corrects those cases fail-closed. The shared helper's package option also prevents a same-named event from another package satisfying the selected gate while retaining globally fatal malformed/fail handling.

## Workflow variant

All four gates call `scripts/ci/assert-go-test-json.py` with an exact package:

- Pi: `github.com/wxxb789/symmetry/daemon/internal/harness/pi`
- OpenCode: `github.com/wxxb789/symmetry/daemon/internal/harness/opencode`
- containment: `github.com/wxxb789/symmetry/daemon/internal/platform`

Linux continues to invoke `python3`. It retains the existing `go test ... 2>&1 | tee` pipeline, `PIPESTATUS` capture, log append, provenance fields, tee failure, Go failure, helper failure, and success status. Because stderr remains intentionally merged, a cold non-JSON diagnostic is logged and rejected as malformed rather than discarded.

Windows resolves the first application returned by `Get-Command python`, executes `python --version`, and fails if it cannot run. The observed command was Python 3.12.10. Each Windows Go process now uses `Start-Process` with distinct stdout event and stderr diagnostic files. The untouched stdout bytes go to the helper; both files are emitted to the job log, and Pi/OpenCode also append both to their existing logs. This avoids turning cold stderr diagnostics into JSON while never hiding them. A 135-byte UTF-8 exact-pass source and the Windows redirected event file had the same SHA-256, `6800d7b8cd8a1227edd767867f6f45f672c25c01bd8600a1fd33b8d8faee7e56`.

The successful OpenCode provenance line remains `test_events=run:1;pass:1;fail:0`; on rejection it records the shared helper exit instead of reconstructing counts with another parser. Existing Pi exit/status provenance is retained. All pinned release URLs, versions, archive/executable hashes, provider opt-in flags, timeouts, package/test regexes, provider credential declarations, private-input boundaries, and artifact upload policies are unchanged.

## Controlled wrapper execution

Scratch wrappers reproduced the final Linux status ordering and Windows `Start-Process`/shared-helper ordering with controlled event bytes and fake Go exits. They were run through Bash and actual PowerShell 7 on the Windows workstation.

| Case | Linux exit | Windows exit | Meaning |
|---|---:|---:|---|
| valid pass, Go 0 | 0 | 0 | accepted |
| valid pass, Go 7 | 7 | 7 | Go exit preserved and takes precedence |
| malformed plus pass, Go 0 | 1 | 1 | helper rejection is fatal |
| child skip plus pass, Go 0 | 1 | 1 | skip is fatal |
| missing pass, Go 0 | 1 | 1 | missing target is fatal |
| valid pass plus cold stderr diagnostic, Go 0 | 1 | 0 | Linux logs/merges and rejects non-JSON; Windows logs stderr separately and validates untouched stdout |

The first wrapper run exposed that `Get-Command python` can return multiple applications on this workstation. The workflow was corrected to select the first application explicitly and then verify it with `--version`. The final controlled run passed every expected exit.

## Real production Windows witness

The credential-free production containment witness was run exactly once through the final Windows process/redirection/helper shape with `SYMMETRY_PRODUCTION_CONTAINMENT_WITNESS=1`:

```text
Python 3.12.10
TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse: PASS (12.32s)
package elapsed: 15.295s
go exit: 0
shared helper exit: 0
verified selected event: run=1 pass=1 skip=0
wrapper elapsed: 22.393s
```

This is the only native candidate command run. It is a real production Windows containment result, not a synthetic green claim.

## Validation

Commands were launched with direct `Bun.spawn` argv and produced:

- Old/new seven-case parser matrix: exit 0 after all 28 parser/case executions completed; expected-red entries are recorded in the table.
- Controlled Linux/PowerShell wrapper matrix: exit 0 on the final run with all expected statuses.
- `python scripts/ci/test-linux-v2-go-events.py`: exit 0; six tests; `OK`; 3.700s process time (3.052s unittest time).
- YAML parse using `yaml.safe_load(..., encoding='utf-8')`: exit 0; `yaml_ok`; 2.293s.
- Real production Windows wrapper: exit 0; 22.393s.

No full suite was run. The Linux Pi native provider bridge, Windows Pi native provider bridge, and Windows OpenCode synthetic native command were not run: their pinned external binaries/setup were outside this no-external-service execution, and no protected provider inputs were supplied. Their command lines, opt-ins, budgets, pin/hash checks, provider boundaries, logs, provenance, and fatal status ordering were retained and were exercised only with controlled Go/event inputs. No green native result is claimed for those three runtime gaps.

## Decision

The four inline implementations were redundant as parser code but weaker as validators. Replacing them with the already tested helper removes duplicated parsing at the correct boundary and deliberately strengthens duplicate, skip, package-collision, global-failure, malformed-input, and UTF-8 handling. Every native/platform witness remains in place. C5 remains intact.
