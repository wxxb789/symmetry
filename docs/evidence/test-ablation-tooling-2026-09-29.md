# Tooling test-ablation inventory

Date: 2026-09-29  
Scope: `browser/`, `contracts/`, `scripts/`, `.github/workflows/ci.yml`, package scripts, and Compose test entrypoints. Go and Elixir test bodies are referenced only by their invocation names.  
Subject: dirty working tree at `d14debbf6ae3ded09e5013ef90952d347c01474d` on `codex/goal-0006b-crash-safe-process-containment`.  
Mode: source inventory only. No test, browser, service, credential, mutation, ablation, or removal was run. Existing dirty work was not changed.

## Executive result

The assigned source contains **13 test files**, not 12:

- 7 Playwright spec files, **33 source test declarations**.
- 2 Node `node:test` files, **27 source test declarations**.
- 3 Python `unittest` files, **16 source test methods**.
- 1 PowerShell regression-test file, one sequential suite with **7 explicit `Assert-True` calls** plus fail-closed process/identity checks.

Thus there are **76 named JS/Python tests plus one PowerShell suite**. Playwright project expansion is deliberately not counted as additional source tests: ordinary unguarded specs execute under desktop and mobile projects, while the three live/Compose specs guard themselves to desktop and environment opt-ins.

The shared contract corpus contains **184 fixture records/files**: 63 semantically valid and 121 semantically invalid; 95 structurally valid and 89 structurally invalid. The manifest distributes them across 14 envelope schemas. AJV/schema, Effect decoder, compile-time decoder typing, and generated-file drift are distinct checks and must not be collapsed by name.

No candidate is proven removable from source inspection alone. The ledger below identifies five executable ablation/refactoring experiments and records all other apparent similarities as retained distinct boundaries or blocked native/integration evidence.

Actual served model from the task environment: **provider `ghc`, model `gpt-5.6-sol-fast`, reasoning level `medium`** (`PI_PROVIDER`, `PI_MODEL`, `PI_REASONING_LEVEL`).

## Reconciliation with the parent manifest

`docs/evidence/test-ablation-manifest-2026-09-29.md` correctly lists the seven browser specs, two contract runtime test files, and three Python unittest files. Its Tooling count of 12 therefore agrees with the initial suffix-based scan, but it is not exhaustive for executable tests.

### Omissions

1. **`scripts/claude-code-local-transport-smoke.tests.ps1` is a real test file**, not merely support. It imports the smoke helper, creates live child-process fixtures, checks bounded output draining, direct-handle cleanup when identity capture is unavailable, and fail-closed behavior when an exited root leaves a pipe-holding descendant. It is one sequential suite with seven explicit `Assert-True` calls and further throw-on-failure identity/process checks. It should be added to the test-file inventory, making Tooling 13 and the project total 243 if no sibling reconciliation changes other categories.
2. **`contracts/ts/decoder.type-test.ts` is an executable compile-time validation input** to `tsc --project contracts/tsconfig.json`. It has no runtime test declaration, so it belongs in additional fixture/entrypoint candidates, not the test-file count. It asserts 15 decoder success output types, five decoder error channels, discriminated Decision narrowing, closed root/nested/union objects, and typed provider-resource maps.
3. The additional-entrypoint section omits three tracked browser harness runners:
   - `browser/scripts/run-chat-daemon-tests.ps1`
   - `browser/scripts/run-compose-daemon-tests.ps1`
   - `browser/scripts/run-live-daemon-tests.ps1`
   These are fixture owners and test entrypoints, not additional test declarations.

### Listed candidates that are not test harnesses

These are not false positives in a deliberately broad candidate manifest, but their responsibility must be narrowed before ablation:

- `compose.production.yaml` is a production deployment definition also consumed by a CI smoke; `compose.yaml` is the trusted-local stack and a browser/CI fixture. Neither is itself a test.
- `scripts/install-linux-owner.sh` and the two `scripts/systemd/*.service.in` files are production installer/unit assets exercised by native acceptance. They are not removable test support.
- `scripts/cleanup-linux-pi-processes.sh` is fail-closed operational cleanup used by Linux Pi E2E and directly verified by a workflow fixture. It is not an ordinary test runner.
- `scripts/render-seo-assets.mjs` is an asset renderer, not validation. It has no CI test invocation in the inspected workflow.
- `scripts/ci/rootless-compose-native-daemon.Dockerfile`, `linux-v2-host-peer-probe.go`, and `literal-pid-reuse-helper.c` are fixture build inputs, not independent tests.
- `contracts/scripts/generate.mjs` and `generate-effect.mjs` are generators; in `--check` mode the former is also a drift validator. `check.mjs` and `check-effect.mjs` are executable validation runners.
- The 184 contract fixture JSON files plus `manifest.json` are a shared machine-consumed corpus, not 185 tests. Their distinct expectations are described below.
- Package manifests and `pnpm-workspace.yaml` are invocation/configuration surfaces, not tests.

No manifest-listed tooling path was found to be irrelevant to all validation or fixture consumers; the items above are responsibility corrections rather than deletion recommendations.

## Test-file ledger

### Browser tests (7 files, 33 declarations)

All browser files use `browser/playwright.config.mjs`: test directory `tests`, one worker, no retries, 60-second test timeout, 10-second expectation timeout, default URL `http://127.0.0.1:4000`, desktop Chromium and Pixel 7 Chromium projects, and failure trace/screenshot/video retention. `browser/tests/fixtures.mjs` records unexpected console/page errors and asserts none after each base-page test. Tests require a running Control portal and operator token unless their API routes are mocked.

| File | Count | Category | Guarded behavior and failure boundary | Entrypoint/environment | Disposition |
|---|---:|---|---|---|---|
| `browser/tests/portal.spec.mjs` | 12 | browser validation/acceptance | Real portal project/resource/work-item CRUD and persistence; archive read-only behavior; untrusted status/link encoding; login/CSRF/logout; keyboard/focus/overflow; stale project and work-item conflicts; delayed mutation/detail/history/workspace responses; polling preserving local drawer state. One two-context stale-write test is mobile-skipped. | `cd browser && npm test`; real Control + DB, development or supplied operator token; desktop/mobile. | Retain. The 12 tests protect different security, accessibility, concurrency, and persistence failures. Two fixed post-release sleeps are noted under blockers/test quality. |
| `browser/tests/portal-connections.spec.mjs` | 5 | browser unit-style harness/validation | Mocked Portal API validates GitHub/Azure credential locality, capability normalization, provider/resource binding, external authority, single-flight create/update/delete/check/sync, dialog ownership, stale drafts, refresh/project-switch races, redacted provider failures, and overflow. | `npm test`; real static portal page, mocked `/portal/api/**`; desktop/mobile. | Retain. It is the only broad provider UI state-machine harness and does not substitute for real-provider checks. |
| `browser/tests/portal-chat.spec.mjs` | 11 | browser unit-style harness/validation | Mocked Chat API validates unsafe links/text, discussion versus start-work, full-goal preservation, run generation/transition binding, durable command acknowledgement presentation, decision packets, cross-context read/send races, idempotent action IDs after uncertain send, focus/selection/scroll preservation, project-switch ownership, per-project drafts, semantic timeline filtering, history-gap merge/deduplication, and bounded failed-run text. | `npm test`; real portal UI with mocked API; desktop/mobile. | Retain. Similar race names in other files have different owners and payloads. |
| `browser/tests/portal-chat-live.spec.mjs` | 1 | native/integration verification | Real cooperative daemon and Control: Chat-created work, discussion without command mutation, guidance persisted into artifacts, same-process pause/restart/resume, structured decision, completion, cancellation receipt/journal disappearance, invalid decision packet recovery, and optional screenshots. | `browser/scripts/run-chat-daemon-tests.ps1`; `SYMMETRY_CHAT_LIVE=1`; desktop only; Go builds, running Control/DB, isolated daemon/fake agent; optional Control container restart. | Retain; blocked unless the live fixture is available. Deterministic Chat tests are not native lifecycle evidence. |
| `browser/tests/portal-compose-daemon.spec.mjs` | 1 | smoke/integration/verification | Trusted-local Compose F1-F7 path: real bundled daemon registration, resources/health, human and agent workflows, ordering/persistence, waiting-input replay after dropped response, stale input refusal, cancellation, fail/retry across generations, agent/manual delivery evidence, archive behavior, Activity, final persisted state. | `browser/scripts/run-compose-daemon-tests.ps1` -> `npm run test:compose`; Docker Compose, local ports, built images, DB; desktop only. | Retain pending candidate C2 experiment. It proves the packaged local stack, not merely UI behavior. |
| `browser/tests/portal-compose-restart.spec.mjs` | 1 | persistence verification | Reads the exact F1-F7 project after `docker compose restart control`; asserts project/resources/order, human/wait/cancel/retry states, runtime linkage, timelines, generation evidence, and manual delivery. Runner separately compares a database fingerprint before/after restart. | Second phase of `run-compose-daemon-tests.ps1` -> `test:compose:read`; environment carries exact project ID/key/name; desktop only. | Retain pending candidate C2. Restart and post-restart read are a distinct failure mode. |
| `browser/tests/portal-live-daemon.spec.mjs` | 2 | native/integration verification | Four isolated local daemon/fake-agent runtimes prove registered references, forged runtime rejection, wait/input completion, process cancellation, fail/retry intent and generation evidence, Activity, plus real raw-history pagination. | `browser/scripts/run-live-daemon-tests.ps1` -> `npm run test:live`; running Control/DB, Go toolchain, four processes; desktop only. | Retain. Compose uses the bundled default runtime; this runner isolates runtime bindings and exercises separate modes. |

Browser declaration total: **33**.

### Contract tests (2 runtime files, 27 declarations; 1 compile-time validation input)

| File | Count | Category | Guarded behavior | Entrypoint | Disposition |
|---|---:|---|---|---|---|
| `contracts/scripts/boundary.test.mjs` | 18 | unit/boundary validation | Raw numeric lexeme and malformed JSON classification; duplicate keys including nesting/escaped aliases; UTF-8 and surrogate rejection; wire-value preservation; typed semantic/hash failures; TaskResult/admission shapes; handcrafted Effect-versus-AJV agreement for oneOf/allOf, additionalProperties/property bounds, Unicode/date-time, array bounds/JSON equality. | `pnpm contracts:test` (`node --test contracts/scripts/*.test.mjs`) and `pnpm contracts:check`. | Retain pending C1 mutations. Exact error classes and parser edge shapes exceed manifest fixture pass/fail checks. |
| `contracts/scripts/generate-effect.test.mjs` | 9 | generator unit validation | Generated reusable graph/facade shape, root/common-name separation, required-only/not behavior, exact optional rejection, undeclared required names, unsupported keywords/reference scope, legal root refs, canonical Evidence root refs. Some cases execute generated graphs against AJV after in-memory TS transpilation. | Same Node test entrypoint. | Retain. This protects generator behavior before committed-output drift and fixture execution can diagnose it. |
| `contracts/ts/decoder.type-test.ts` | 15 success + 5 error type equalities, plus narrowing/exactness checks | compile-time type validation | Decoder DTO output/error channels, discriminated decision branches, exact object surfaces and typed maps. | `pnpm contracts:typecheck`; transitively `pnpm contracts:check`. | Retain and add to manifest additional candidates. Runtime schema checks cannot detect public TypeScript type widening. |

Contract runtime declaration total: **27**.

### Python tests (3 files, 16 methods)

| File | Count | Category | Guarded behavior | Entrypoint | Disposition |
|---|---:|---|---|---|---|
| `scripts/ci/test-linux-v2-evidence.py` | 10 | unit/fixture validation | Executes the real evidence exporter against private temporary trees: all 16 legacy shapes project without source mutation or secret/private leakage; empty/null proof lists fail incomplete; truncated nested proof fails; unknown credential fields and configured secret values fail; symlinks fail; missing evidence produces incomplete manifest; Compose allowlist copies exact bytes; manual mode requires exact case set. | CI daemon job: `python3 ../scripts/ci/test-linux-v2-evidence.py`. | Retain. This is the only direct regression suite for redacted evidence completeness and credential exclusion. |
| `scripts/ci/test-linux-v2-go-events.py` | 2 | unit/validation | Real `assert-go-test-json.py` subprocess rejects absent/missing/duplicate run/pass, package fail, parent/child skip, and malformed JSON despite a pass. | Same CI fixture-contract step. | Retain pending C4 parser-consolidation experiment. |
| `scripts/ci/test-linux-v2-source-subject.py` | 4 | unit/integration validation | Real temporary Git repository proves exact clean subject/digest, wrong commit refusal, staged/unstaged/untracked refusal without mutation, and non-repository refusal. | Same CI fixture-contract step. | Retain. Git/index behavior is its contract and is not covered by hashing helpers alone. |

Python method total: **16**.

### PowerShell test omitted by the parent manifest

| File | Count | Category | Guarded behavior | Entrypoint | Disposition |
|---|---:|---|---|---|---|
| `scripts/claude-code-local-transport-smoke.tests.ps1` | 1 sequential suite; 7 explicit assertions | Windows unit/process verification | Bounded pending-output failure, successful output preservation, task-fault closure, identity-free direct-handle tree termination, and fail-closed exited-root/pipe-holding-descendant cleanup; source checks forbid unbounded `GetAwaiter().GetResult()` and require the `cleanup unproven` path. | Direct: `pwsh -NoProfile -File scripts/claude-code-local-transport-smoke.tests.ps1`. It is not invoked by current CI. | Retain and add to manifest test files. Missing CI invocation is a coverage gap, not redundancy. |

## Contract fixture and validator graph

### Fixture corpus

`contracts/fixtures/manifest.json` has 184 unique fixture records matching 184 JSON files:

| Schema | Fixtures |
|---|---:|
| adapter-capabilities | 3 |
| admission | 34 |
| context-snapshot | 8 |
| decision | 17 |
| evidence | 10 |
| evidence-batch | 4 |
| evidence-batch-conflict-details | 4 |
| evidence-batch-response | 4 |
| goal-command | 48 |
| goal-create | 3 |
| goal-revision | 13 |
| plan-proposal | 6 |
| task-result | 18 |
| usage | 12 |

The corpus is not duplicated merely because two runners consume it:

- `check.mjs` is the **Draft 7/AJV oracle plus semantic validator**. It enforces exactly 14 schemas, strict envelopes, fixture IDs/schema references, structural and semantic expectations/error codes, 29 required coverage tags, canonical raw number/Unicode behavior, and finally invokes `generate.mjs --check`.
- `check-effect.mjs` is the **public Effect boundary**. Every fixture enters `decodeJson`; parsed documents separately enter `Schema.is`; valid values must round-trip unchanged through both `decodeJson` and `decodeUnknown`; semantic-only failures must be typed `SemanticError`; expected error codes are checked.
- `generate.mjs --check` is **generated artifact drift** across TypeScript DTOs, one Effect graph, Go DTOs, and the Go schema bundle, including missing/unexpected files and exact bytes.
- `generate-effect.mjs` validates the supported Draft 7 subset and renders Effect graphs; its own nine tests use synthetic schemas that the production fixture set cannot replace.
- `decoder.type-test.ts` is the **compiler contract** and catches type widening that runtime validators cannot.

Removing AJV, Effect, typecheck, or drift would remove a distinct contract boundary. A fixture accepted/rejected by both implementations is useful differential evidence, not duplicate execution.

## Support and entrypoint ledger

### Browser support and runners

- `browser/playwright.config.mjs`: project matrix, artifact policy, base URL and deterministic serial execution.
- `browser/tests/fixtures.mjs`: shared automatic browser-error monitor; support, not a test declaration.
- `browser/package.json`: `test` all specs; focused `test:compose`, `test:compose:read`, `test:live`; headed/report utilities.
- `browser/scripts/run-chat-daemon-tests.ps1`: builds isolated daemon/fake-agent, creates one cooperative runtime, exports exact Chat paths, runs the one desktop live spec, restores process environment, stops its process.
- `browser/scripts/run-live-daemon-tests.ps1`: builds binaries, creates four isolated runtime configs/processes (wait/cancel/retry/history), runs two desktop live tests, stops all owned processes.
- `browser/scripts/run-compose-daemon-tests.ps1`: starts/builds the local Compose stack, runs F1-F7, queries an exact DB fingerprint, restarts Control, proves fingerprint equality, then runs the read-after-restart spec. This is the sole browser Compose orchestration entrypoint.
- `browser/README.md`: documentation only.

### Root package scripts

- `pnpm contracts:generate`: mutating generator; not used in this read-only wave.
- `pnpm contracts:typecheck`: strict TypeScript compiler including `decoder.type-test.ts`.
- `pnpm contracts:test`: 27 Node tests.
- `pnpm contracts:check`: typecheck, Node tests, AJV/semantic fixtures + generated drift, then Effect fixtures.
- `contracts/package.json`: package export/dependency metadata, no separate scripts.

### Scripts responsibility classification

**Independent validation/smoke/verification entrypoints**

- `scripts/audit-no-legacy-routes.sh`: deterministic source audit over an explicit production contract surface; current CI `legacy-route-audit` entrypoint.
- `scripts/claude-code-local-transport-smoke.ps1`: credential-scrubbed, loopback-only Claude transport smoke pinned to Claude 2.1.281 and target model label; validates exact JSON `OK`, bounded execution, and proven cleanup. It is not currently in CI and does not prove native lifecycle/provider capability.
- `scripts/ci/linux-v2-host-prerequisite-probe.sh`: credential-free hosted-Linux prerequisite probe for systemd PID 1, delegated cgroup v2 kill, cross-UID user namespace/fdinfo/pidfd_getfd, and cleanup evidence. It explicitly is not production owner/native support evidence.
- `scripts/ci/linux-v2-native-acceptance.sh`: credentialed manual, protected-environment A/B native acceptance orchestrator. It owns 12 exact case-set invocations, source binding, fixed native binaries/models, installed owner boundary, retained recovery material, physical cleanup proof, Control receipts, and allowlisted export. Retain as blocked where credentials/protected environment are unavailable.
- `scripts/ci/rootless-compose-native-acceptance.sh`: rootless Compose refusal witness; builds native fixture image, uses the transition proxy and authentic Control claim, runs exact Go witness names, gates events, exports three allowlisted documents. This negative/refusal boundary does not replace installed A/B native acceptance.
- `scripts/ci/wsl2-v2-denial-witness.sh`: installed WSL2 denial-only witness. Absence of WSL2 prerequisites is not redundancy.
- `scripts/ci/verify-linux-v2-helper-exit-checkpoint.py`: destructive-to-owned-selector verification after exact live-B checkpoint, UID/GID, PID namespace/start identity, cgroup leaf and FD checks; helper for a native case, not an ordinary unit test.

**Validation helpers directly tested or gated**

- `scripts/ci/assert-go-test-json.py`: strict exact-run/pass/no-skip parser, tested by two Python methods and called throughout CI/native runners.
- `scripts/ci/export-linux-v2-redacted-evidence.py`: strict allowlist/projector/atomic manifest exporter, tested by ten Python methods; preserves redacted evidence completeness.
- `scripts/ci/linux-v2-source-subject.py`: clean exact Git source binder, tested by four Python methods and rechecked before successful evidence export.
- `scripts/ci/linux-v2-native-tools.sh`: pinned checksum/version acquisition shared by installed and rootless native fixtures.
- `scripts/cleanup-linux-pi-processes.sh`: exact Linux process-identity/pidfd group cleanup fallback used by Pi Control E2E; workflow includes an independent matching/wrong-start live-process verification before E2E.

**Fixture construction/support, not independent tests**

- `prepare-rootless-compose-owner-claim.py`: creates a real Portal project/resource/work item and generic claim fixture.
- `provision-linux-v2-control-claims.exs`: creates 44 isolated Control fixture runs and reads exact provider receipts.
- `publish-linux-v2-control-receipt.py`: validates and atomically publishes a sanitized root-owned receipt.
- `register-linux-v2-claim-machine.py`: enrolls exact generic claim and service identities.
- `write-linux-v2-owner-request.py`: claims one exact run and binds authentic response/fence/subject/barrier.
- `rootless-compose-transition-proxy.py`: disposable credential-suppressing transition-response hold/release proxy.
- `linux-v2-host-peer-probe.go`, `literal-pid-reuse-helper.c`: compiled native fixture probes.
- `rootless-compose-native-daemon.Dockerfile`: fixture image definition.

**Production/operations or unrelated generation**

- `install-linux-owner.sh` and `systemd/*.service.in`: production installation assets.
- `render-seo-assets.mjs`: mutating visual asset generator, outside test ablation.

### Compose entrypoints

- `compose.yaml`: local Postgres/migrate/Control/daemon stack. Used by the browser Compose runner and CI Compose smoke.
- `compose.production.yaml`: production Control/migrate/TLS edge topology with required environment and internal backend. CI supplies temporary Postgres and verifies missing required variables, absence of development credentials, network/port topology, HTTP redirect, SNI/Host rejection, health, and HSTS.
- `.github/workflows/ci.yml` Compose job first validates both stack surfaces and a real local fake-agent task, then calls `rootless-compose-native-acceptance.sh`. These are distinct positive local/production-edge smoke and rootless native-refusal checks.

## Workflow invocation inventory

Current CI has 13 jobs: host prerequisite, protected Linux native, contracts, Control, Linux Pi Control E2E, Windows Pi Control E2E, Linux daemon, Linux OpenCode synthetic, Windows OpenCode synthetic, legacy-route audit, Windows daemon, integration, and Compose.

Relevant invocation facts:

- Contract checks run once through `pnpm contracts:check` after frozen install.
- The daemon job runs all three Python tooling suites once, ordinary Go tests once, focused exact-event peer-negative gates, and a race suite. Race and ordinary execution are not interchangeable.
- Linux and Windows Pi jobs each run the same named native provider-bridge smoke on their own OS and then the same tagged Control E2E on their own OS. These are platform gates, not duplicate evidence.
- Linux and Windows OpenCode synthetic jobs run the same named native-tool integration against different native binaries/platform behavior.
- Control `mix test` is followed by environment-enabled provider broker and receipt E2Es whose default suite behavior is guarded/skipped; they are not established duplicate invocations.
- Integration runs selected real daemon/Control scenarios, response-loss, Control restart, and production Linux containment/evidence export.
- Protected Linux native acceptance loops over 12 disjoint case sets and requires one manifest per set; unavailable secrets, environment approval, native binaries, or systemd/cgroup prerequisites block execution and do not support removal.
- Existing ordinary-command consolidation is accepted as current state. This report makes no stale-doc claim that CI still redundantly runs an already-consolidated ordinary command.

## Candidate and experiment ledger

No experiment was run in this child task. Parent owns admission and execution.

### C1 - Boundary parser tests versus manifest fixtures

**Apparent overlap:** `boundary.test.mjs` directly tests noncanonical numbers, duplicate members, unpaired surrogates, reason/admission shapes and hash mismatch; related negative manifest fixtures pass through both `check.mjs` and `check-effect.mjs`.

**Why names are insufficient:** boundary tests assert exact `WireJsonError`/`SemanticError` classes and codes, nested and escaped duplicate aliases, malformed JSON strings, same-name-in-different-object acceptance, numeric-looking escaped strings, malformed UTF-8 bytes, and hand-built AJV parity. The manifest generally asserts structural/semantic booleans and selected codes against production envelopes.

**Smallest experiment:** baseline `pnpm contracts:test` and `pnpm contracts:check`; then, one test at a time, temporarily remove only a candidate boundary assertion and inject a controlled mutation into the owning parser/semantic helper (for example: stop unescaping member names before duplicate detection; accept a nested duplicate; decode malformed UTF-8 with replacement; count JS UTF-16 units rather than Unicode points). Run the two focused commands once per mutation and restore experiment-owned edits.

**Removal condition/surviving coverage:** remove a boundary test only if a named existing fixture in both AJV and Effect runners fails for that exact mutation and still preserves the same error classification required by callers. Otherwise retain. Initial source comparison predicts the escaped-alias, same-name-different-object, malformed UTF-8, and AJV parity tests survive uniquely.

### C2 - Compose F1-F7 final assertions versus restart-read assertions

**Apparent overlap:** `portal-compose-daemon.spec.mjs` ends by asserting the complete project state; `portal-compose-restart.spec.mjs` reasserts much of that state after Control restart, while the runner also compares a database fingerprint.

**Distinct setup/failure mode:** the first test creates and mutates state through the full stack. The second proves the browser/API can read the same state after process restart. A DB fingerprint alone cannot prove application rehydration/read-model behavior; the first test alone cannot prove restart durability.

**Smallest experiment:** preserve the creation test and pre/post fingerprint. Ablate one repeated post-restart assertion group at a time and introduce one reversible database/read-model mutation after fingerprint capture but before the restart read (resource health projection, done-item position, runtime link, command timeline, generation-2 event, or manual delivery source). The restart test must fail for every omitted semantic group through a surviving assertion.

**Removal condition/surviving coverage:** only collapse an assertion already caught by another post-restart assertion at the same API/UI boundary. Never remove the post-restart project identity, resource matrix, ordering, runtime linkage, wait/cancel timelines, generation set/event kinds, or manual delivery provenance merely because the pre-restart test saw them.

### C3 - Live-daemon versus Compose daemon lifecycle matrix

**Apparent overlap:** both cover waiting input, cancellation, fail/retry, generations, timeline evidence and Activity.

**Distinct setup/platform:** live-daemon uses four uniquely bound host processes/profiles and tests forged runtime references plus raw-history pagination. Compose uses the packaged default daemon, dropped input-response replay/stale transition refusal, five resource kinds, human workflow, archive behavior and manual-over-agent delivery. Their failure boundaries differ.

**Smallest experiment:** mutate one packaging/binding edge at a time: wrong Compose daemon config/default workspace should fail only Compose; wrong unique runtime suffix or registered runtime should fail live-daemon; suppress a paged history record should fail only live-daemon; break replay idempotence after an accepted input should fail Compose. Run only the two focused desktop entrypoints, not the full browser suite.

**Disposition:** retain both unless every mutation for a proposed shared assertion is detected by the intended survivor. Native/integration prerequisites being unavailable is a blocker, not evidence of redundancy.

### C4 - Repeated selective Go-event parsers in CI

**Candidate:** Linux uses tested `assert-go-test-json.py`; several Windows workflow steps contain inline PowerShell run/pass/fail parsing, and the early Linux Pi smoke contains an inline Python parser. The intent repeats, but implementations are not equivalent: the shared helper requires exact counts, rejects skips and invalid JSON; some inline parsers use sets or ignore malformed lines.

**Smallest experiment:** save representative JSONL containing exact pass, duplicate run/pass, parent/child skip, package fail, malformed line plus pass, and missing test. Baseline each current inline parser on its platform. Replace only one inline parser with `python3 scripts/ci/assert-go-test-json.py EVENTS EXPECTED...`; replay the corpus and then run the one affected selective command. Surviving coverage is the native test exit status plus strict helper enforcement and existing provenance writes.

**Disposition:** candidate for parser refactoring, not test removal. Do not ablate `test-linux-v2-go-events.py`; it is what proves the shared parser fails closed.

### C5 - Ordinary Go suite followed by focused peer-negative exact-event gates

**Candidate:** the daemon job runs `go test ./...` and later reruns selected Pi/OpenCode peer-negative names with JSON event validation.

**Unknown requiring sibling/parent experiment:** this report does not inspect Go bodies. The focused gate adds exact execution/no-skip evidence that plain ordinary output does not provide; build tags, host capabilities, subtest selection and environment guards may differ.

**Smallest experiment:** capture a known-green ordinary suite as JSON once (not an additional full-suite rerun) and feed its event stream to `assert-go-test-json.py` with the exact focused parent/subtest names. Inject/force each relevant skip guard or peer failure. If the ordinary stream proves each required test runs exactly once and detects the same mutations, ablate the later focused rerun while retaining strict event validation over the ordinary stream. If ordinary execution skips or does not expose equivalent failure, retain the focused gate.

**Disposition:** candidate, blocked on parent/sibling Go-body and baseline evidence. No duplicate claim is made now.

## Similar-looking checks retained as distinct

- AJV fixture oracle vs Effect public decoder vs TypeScript typecheck vs generated drift.
- Desktop vs mobile portal behavior.
- Linux vs Windows native Pi/OpenCode checks.
- Ordinary vs race Go execution.
- Deterministic mocked Chat/Connections versus live daemon/provider boundaries.
- Local Compose HTTP task smoke, production TLS/Host/network smoke, browser F1-F7, restart durability, and rootless native refusal.
- Credential-free host prerequisite probe, protected credentialed installed A/B acceptance, rootless denial, and WSL2 denial.
- Source-subject validation and evidence redaction/completeness; a clean commit does not prove safe evidence export.
- `portal.spec.mjs` unsafe detail links and `portal-chat.spec.mjs` unsafe Chat links/text: separate renderers/surfaces.
- Project-switch and stale-response tests in portal, provider connections/resources, and Chat: separate state owners and response paths.

## Blockers and quality observations

- No running Control/Postgres fixture, protected GitHub/OpenAI credentials, approved GitHub environment, Linux systemd/cgroup-v2 A/B host, WSL2 installed owner, or native browser service was exercised. Those checks remain retained.
- Read-only probes found Node `v24.18.0`, pnpm `12.6.0`, Python `3.12.10`, PowerShell `7.6.6`, Playwright `1.62.1`, and Docker Compose `v5.5.1`. Browser binaries and service health were not probed by launch. The pnpm probe emitted unrelated `mise` deferred-uninstall warnings before its version; no dependency was installed.
- `portal.spec.mjs` has three deterministic release promises but uses fixed `waitForTimeout(300/250/250)` after releasing delayed responses. The exact response/state signal should replace those sleeps before relying on ablation timing. This is a test-quality finding, not a redundancy claim.
- `portal-chat-live.spec.mjs` intentionally waits 1,000 ms for autonomous step intervals and 750 ms after cancellation to prove artifact stability. Time progression is part of that fixture’s behavior, but these remain cost/flakiness risks and should not be used as evidence that another test is redundant.
- `scripts/ci/linux-v2-host-prerequisite-probe.sh`, native acceptance, and Control fixture code contain bounded polling/sleeps around external systemd/process/DB behavior. They are verification harnesses, not unit tests; replacement requires exact event/state mechanisms where available, not deletion.
- The PowerShell smoke regression suite is not in current CI. Its absence from CI and the parent manifest is a coverage omission.
- No candidate removal can be admitted from unavailable prerequisites. Redacted evidence manifests must continue to prove required allowlisted files, completeness, credential exclusion and source binding.

## Verification performed for this inventory

- Read the project ablation authorization, root `AGENTS.md`, and `docs/design/quality.md`.
- Enumerated tracked/current scoped files and preserved the pre-existing dirty tree.
- Parsed declarations from all seven browser specs, both Node test files and all three Python test files; inspected the omitted PowerShell test suite.
- Parsed all 184 contract manifest records and reconciled structural/semantic counts and per-schema distribution.
- Read package scripts, browser runners/config/support, contract validators/generators/type test, Compose definitions, CI workflow invocations, smoke scripts, native validation helpers, and production/support responsibilities.
- Performed version-only environment probes; did not run tests, browsers, Compose, services, credentials, generators, or mutations.

This report is inventory and experiment design only. It authorizes no deletion or implementation.
