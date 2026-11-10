# Contract C1 test-ablation experiment

Date: 2026-09-29  
Task: `st_01a0ed80`  
Scope: `contracts/scripts/boundary.test.mjs` versus manifest execution through `contracts/scripts/check.mjs` and `contracts/scripts/check-effect.mjs`.

## Disposition

**Retain all C1 boundary tests. No assertion or test was removed.**

The manifest runners do reject many related documents, but they do not preserve the same boundary evidence for the candidate cases. In particular:

- no manifest fixture exercises escaped duplicate member aliases;
- no manifest fixture exercises a nested raw duplicate member;
- no manifest fixture supplies malformed UTF-8 bytes;
- the Effect fixture runner does not assert `noncanonical_number` for unsafe numeric lexemes;
- the hash-mismatch fixtures assert semantic rejection but not the `semantic_mismatch` code.

Therefore removing the direct tests would permit regressions in raw-parser behavior or typed error classification to survive the fixture gate. A schema-level rejection is not an equivalent replacement for the raw `WireJsonError` class/code before schema validation.

No production or test source remains changed. The only permanent write from this experiment is this report.

## Baseline

The subject was the existing dirty worktree at the task start; no Git/index operation, install, service, fixture, generated-file, package, or lockfile mutation was performed.

Initial SHA-256 values:

| Path | SHA-256 |
|---|---|
| `contracts/ts/wire-json.ts` | `c0041d55293ffc6b22e5bdd823146f5b5ab187debdc02e67bcc7ce0da71d47a2` |
| `contracts/ts/semantics.ts` | `f907125057feb0b75220eee13386ceaae8ff5d4c2fb6342616b18557f21d01a0` |
| `contracts/ts/schema-helpers.ts` | `1c7cf65493ec3feab8b5ba359c2c1b1fb74e22579e77924dd7df7be0f54b4388` |
| `contracts/scripts/boundary.test.mjs` | `986b5f3b4a28e629900f32cb8f7a63f14379ea8b11b7c1e9606183a1c494c488` |

Known-green commands:

1. `node --test contracts/scripts/*.test.mjs` -> exit 0; 27 passed, 0 failed.
2. `pnpm contracts:check` -> exit 0. This ran TypeScript typecheck, the same 27 Node tests, generated-file drift, 14 Draft 7 schemas / 184 AJV fixtures, and all 184 Effect raw decodes (180 documents reached schema validation).

`pnpm` emitted an unrelated `mise` deferred-uninstall warning because a Codex installation directory was in use; the contract command itself completed with exit 0. No retry was used.

## Method

Each mutation changed one implementation behavior only; expected assertions and fixture data were never changed. For every mutation, the focused direct boundary test and the two fixture runners were executed separately. Expected-red runs were not treated as passing verification. The implementation was restored immediately after each completed experiment.

The requested shell `apply_patch` executable was not installed (`apply_patch: command not found`, exit 127) during the first no-op attempt. That attempt changed no bytes and all three commands stayed green. The actual experiments therefore used the harness's exact-text patch operation, with unique preimages and an exact inverse after each run. Final hashes below prove byte restoration.

One parallel M2 launch exceeded its 60-second bound in all three processes and was discarded; the source was restored immediately. M2 was then run once sequentially with a 180-second bound and produced the recorded deterministic result. There was no retry-to-green.

## Kill matrix

Exit 1 is the expected mutation kill. Exit 0 means the mutation survived that check.

| Mutation | Exact implementation change | Focused direct command | Direct | `node contracts/scripts/check.mjs` | `node contracts/scripts/check-effect.mjs` | Disposition |
|---|---|---|---:|---:|---:|---|
| M1 escaped duplicate alias | `scanJsonString` retained the six-character `\\uXXXX` spelling instead of decoding it for duplicate comparison | `node --test --test-name-pattern='escaped member names|public boundary rejects duplicate' contracts/scripts/boundary.test.mjs` | 1 (2 failures) | 0 | 0 | Retain both escaped-alias assertions. Both fixture runners missed the accepted duplicate alias, including the public `GoalCreate.decodeJson` regression. |
| M2 nested duplicate propagation | `scanJsonArray` stopped propagating a duplicate found inside an array element | `node --test --test-name-pattern='duplicate nested members' contracts/scripts/boundary.test.mjs` | 1 | 0 | 0 | Retain. Both fixture runners missed the nested duplicate acceptance. |
| M3 malformed UTF-8 | changed the UTF-8 decoder from fatal to replacement decoding | `node --test --test-name-pattern='malformed UTF-8' contracts/scripts/boundary.test.mjs` | 1 (`invalid_json` observed instead of `invalid_utf8`) | 0 | 0 | Retain. Neither corpus runner supplies malformed bytes or checks the raw error code. |
| M4 numeric lexeme classification | changed the pre-schema error code from `noncanonical_number` to `invalid_json` | `node --test --test-name-pattern='unsafe numeric lexemes' contracts/scripts/boundary.test.mjs` | 1 | 1 | 0 | Retain. AJV runner handling noticed the code change at `goal-revision.safe-integer-overflow`, but the public Effect runner accepted the changed classification. This is not equivalent two-runner coverage of the public raw boundary. |
| M5a semantic mismatch class | `assertEqual` threw plain `Error` instead of `SemanticError` | `node --test --test-name-pattern='hash mismatches remain typed semantic failures' contracts/scripts/boundary.test.mjs` | 0 | 0 | 0 | Not a useful mutant: the public decoder normalized this internal throw back to typed `SemanticError`. It was restored and replaced by M5b; no removal inference was drawn. |
| M5b semantic mismatch code | changed `assertEqual` default code from `semantic_mismatch` to `mutated_semantic_mismatch` | same focused direct command | 1 | 0 | 0 | Retain. `task-result.subject-hash-mismatch` and related fixtures require semantic failure but do not assert this code. |

M1 exact observed direct failures were “Missing expected exception” for raw parsing and an unexpected `Right` from the public decoder. M2 likewise produced “Missing expected exception.” M3 and M4 failed on the exact code comparisons. M5b failed on `mutated_semantic_mismatch` versus `semantic_mismatch`.

## Fixture counterexamples and boundary comparison

- **Escaped duplicate aliases:** `String.raw` input with `"payload"` and `"\\u0070ayload"` became accepted under M1. There is no raw duplicate-member fixture. Similar semantic duplicate IDs/keys are parsed documents and a different boundary.
- **Nested duplicate members:** `{"items":[{"id":"first","id":"second"}]}` became accepted under M2. A semantic duplicate evidence key is not a duplicate JSON object member.
- **Malformed UTF-8:** bytes `c0 af` changed from `WireJsonError/invalid_utf8` to replacement decoding followed by `WireJsonError/invalid_json`. All fixture files are readable UTF-8, so both fixture runners remained green.
- **Numeric classification:** the corpus includes fraction/unsafe-integer records, and `check.mjs` has special handling for `noncanonical_number`; `check-effect.mjs` checks only invalidity for these records. M4 proved that public decoder classification could regress while the Effect corpus remained green.
- **Semantic mismatch:** the corpus contains `task-result.subject-hash-mismatch`, `evidence.subject-hash-mismatch`, and `context-snapshot.content-hash-mismatch`, but those manifest entries do not specify `semantic_error`. M5b proved all fixture checks remained green with the wrong semantic code.

The direct malformed-JSON, top-level duplicate, same-name-in-different-object, numeric-looking-string, unpaired-surrogate, schema-shape, and Draft 7 parity assertions were not candidates for removal after these results: no tested candidate established same-boundary equivalence, and this task did not broaden into unrelated mutation work.

## Restoration and final verification

After all inverses, SHA-256 values exactly matched the initial preimages:

| Path | Final SHA-256 | Match |
|---|---|---|
| `contracts/ts/wire-json.ts` | `c0041d55293ffc6b22e5bdd823146f5b5ab187debdc02e67bcc7ce0da71d47a2` | yes |
| `contracts/ts/semantics.ts` | `f907125057feb0b75220eee13386ceaae8ff5d4c2fb6342616b18557f21d01a0` | yes |
| `contracts/ts/schema-helpers.ts` | `1c7cf65493ec3feab8b5ba359c2c1b1fb74e22579e77924dd7df7be0f54b4388` | yes (never mutated) |
| `contracts/scripts/boundary.test.mjs` | `986b5f3b4a28e629900f32cb8f7a63f14379ea8b11b7c1e9606183a1c494c488` | yes (never mutated) |

Final restored verification:

1. `node --test contracts/scripts/*.test.mjs` -> exit 0; 27 passed, 0 failed.
2. `pnpm contracts:check` -> exit 0; typecheck passed, 27 tests passed, generated DTOs were current, 14 schemas / 184 AJV fixtures passed, and 184 Effect raw decodes passed (180 reached schema validation).

Since no candidate met the removal condition, there is no boundary-test edit and no weakening of C1 coverage.

## Environment

Actual served model from the task environment:

- provider: `ghc`
- model: `gpt-5.6-sol-fast`
- reasoning level: `medium`

Runtime observed in command output: Node.js `v24.18.0`.
