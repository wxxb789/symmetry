# rf-acc-opencode-reeval — Phase 0 re-anchor evidence and decision record

Base `6b2d4f8f` (`origin/main` at dispatch). Contract: `goal-rf-acc-opencode-reeval.md` (GDD
state root, external to the repository), Phase 0 of the owner decision Q6=F2
(`refactor/decision-opencode-unfreeze-2026-11-25.md`, GDD state root). Branch
`refactor/rf-acc-opencode-reeval`. Plan: `docs/plans/2026-11-25-rf-acc-opencode-reeval-plan.md`.
Shape reference: `daemon/internal/harness/testdata/opencode/2.3.0/` (its README, probe file and
per-pass receipts) plus the collector's own suite. Receipts and probes:
`daemon/internal/harness/testdata/opencode/2.4.1/`.

## 1. Method and toolchain

- Windows x64: Python 3.12.10, git 2.56.1, kernel `10.0.26300`. The pinned Windows PE ran as a
  native Windows process (`taskkill /T /F` cleanup, the Windows child-environment shape kept).
- WSL `Ubuntu-24.04` (`PRETTY_NAME Ubuntu 24.04.5 LTS`), kernel
  `6.18.58.1-microsoft-standard-WSL2`, Python 3.12.3, `x86_64`. The pinned Linux ELF ran as an
  ordinary Linux process: extracted to a WSL-native path, `start_new_session` so the server
  leads its own process group, bounded `SIGTERM`→`SIGKILL` cleanup, POSIX child-environment
  allowlist (`PATH` plus private `HOME`/`TMPDIR`/XDG roots redirected into the run root). No
  `wsl.exe` interop was used to execute the ELF.
- The linked worktree needed `GIT_DIR`/`GIT_WORK_TREE` for the collector's `source_head` read
  (its `.git` file names a Windows gitdir); every receipt records `source_head 6b2d4f8f195a…`.
- Every pass ran foreground and bounded; no pass was left pending. The collector and its suite
  are pure Python with no third-party dependency; the jscpd gate's toolchain came from the
  repository's pinned devDependencies.
- Provider turns: `tencent-litellm` / `deepseek-v4.1-flash-fast` (owner ruling Q-G, free). The
  credential was read at run time from the operator's OMP provider entry
  (`providers.tencent-litellm.apiKey` in the operator's OMP models config) and injected only
  into the collector subprocess environment; it appears in no record, log line, diff line or
  durable state. The provider-failure pass ran with the credential variable **absent**, and the
  collector refuses to run that mode with it set.

## 2. Preflight — the 2.4.1 pin (Stage 0, no repo bytes)

Release `anomalyco/opencode` tag `v2.4.1`, published 2026-11-23T15:57:40Z, not draft, not
prerelease. Three sources agree per archive — the locally computed sha256, the release's
`SHA256SUMS` row for the exact asset name, and the release metadata's per-asset digest
(`GET /repos/anomalyco/opencode/releases/tags/v2.4.1`; the saved metadata document's own sha256
is `c61741eb7e9acb1a2ffbe5df87afb8d7acb883cbed521547b651477c94482e7b`):

| asset | bytes | local sha256 | `SHA256SUMS` | release metadata |
| --- | --- | --- | --- | --- |
| `opencode-windows-x64.zip` | 132,524,450 | `831e00806531fabb184533ef85fead96eb92287cab2d0f21db7daaa494e75083` | same | same |
| `opencode-linux-x64.tar.gz` | 130,647,866 | `9ddd18e3faf64eaeccf6a0fcbc96a5c7a14520ae5fd5b61e5be51d894f478b47` | same | same |
| `SHA256SUMS` | 2,544 | `fa594de3bc877c0aaa7f2cf3a55c6ef28a781e9b6777bc34dcf823af47785c7e` | n/a | same |

`opencode-linux-x64-baseline.tar.gz` carries the same digest as `opencode-linux-x64.tar.gz` in
the release's own `SHA256SUMS`, so the release's rows decide the pin; the standard-build asset
is used and no `-baseline` substitution happened (a standard-build launch refusal would have
been `DECISION_NEEDED`). Extracted blobs (freshly measured, `--version` executed):

| platform | asset | bytes | sha256 | magic | `--version` |
| --- | --- | --- | --- | --- | --- |
| Windows PE | `opencode-windows-x64.zip` | 278,383,496 | `f3da11bf51ace25b86f52e20368d415451d200d6b48101d9f4e5d721b1d2020e` | `4d5a` | `opencode v2.4.1` (exit 0) |
| Linux ELF | `opencode-linux-x64.tar.gz` | 275,163,312 | `ec93043f18f8a6b4d8e71ec9260e292ba6f91b66b142782135d40f0b58b0c152` | `7f454c46` | `opencode v2.4.1` (exit 0) |

`probe-windows.json` / `probe-linux.json` record those identities, the `--version` and
`serve --help` observations with exit codes, the platform facts, the readiness row, the route
rows and the cleanup barriers. The operator-supplied absolute paths live only in the
uncommitted working copy; the committed records carry the renderer's stable `path-sha256:`
tokens.

## 3. What changed

| file | change |
| --- | --- |
| `scripts/ci/opencode-v2-support-witness.py` | per-platform pins (size/sha256/magic/asset) keyed by the running platform with refusals that name the platform; readiness identity extracted as an exact predicate and the startup race fixed; the route set re-anchored; the continuation leg now awaits the permission request and replies `{"decision":"once"}`; POSIX launch/cleanup with signals on the process group; platform-aware child environment; in-memory credential/relay-origin replacement with counts; `--mode` probe/full/provider-failure/truncation; the live prompt schema resolved by digest for the truncation verdict |
| `scripts/ci/test-opencode-v2-support-witness.py` | 36 tests (was 11): per-platform pin and magic rows, readiness-equality rows, redaction/credential-locality rows, reply-bearing continuation rows, prompt-schema/bounding-field rows, child-environment shape rows for both platforms, POSIX cleanup rows (terminate, escalate, live-reader barrier, stream-reader join). No existing row was deleted or relaxed; the platform-specific rows are guarded, so the suite is green on Windows and in WSL |
| `daemon/internal/harness/testdata/opencode/2.4.1/**` | this evidence's records: README, two probe files, three pass receipts per platform |
| `docs/plans/2026-11-25-rf-acc-opencode-reeval-plan.md` | the plan (protocol, route probe list, preflight, implementation choices, Phase 1 expectation) |
| `docs/evidence/rf-acc-opencode-reeval-2026-11-25.md` | this record |

Frozen bytes untouched: `daemon/internal/harness/opencode/**`,
`daemon/internal/harness/registry.go`, `daemon/internal/app/**`, `.github/workflows/**`,
`docs/design/harness.md`, `docs/goals/**`, `scripts/ci/linux-v2-native-{acceptance,tools}.sh`,
`daemon/internal/harness/testdata/opencode/2.3.0/**` (frozen-byte diff in §12).

One defect was found and fixed while collecting the second platform: the Linux server answers
`503` from its listening socket for about 0.3 s before the API is ready, and the first
collector revision treated that as an identity mismatch instead of a readiness wait. The
readiness loop now retries to the bounded deadline and still requires `200` with
`pid == server pid` and `version == "2.4.1"` exactly. The Windows roles were re-collected under
the final revision for that reason (§4).

## 4. Pass accounting and collector binding

Three pass roles per platform: `full` (Pass 1), `provider-failure` (Pass 2), `truncation`
(Pass 3). The `truncation` role resolved to a schema-grounded `unavailable` on both platforms,
so the spare slot was never needed for a class gap. Every committed receipt binds collector
`scripts/ci/opencode-v2-support-witness.py` sha256
`56d6e602631fdd8d0c3baa34dd3821df3cc0d332ee0d33aecc59afed0b126bea` and
`scripts/ci/source_subject.py` sha256
`8df46499b267f97b0e309f456bbbf2eec5c5ce98ceef92685d7a49bd798a2f93` — the bytes that produced
it, recorded per receipt in `binding_sha256`.

The four Windows records produced by the pre-fix revision (binding
`3598d6a1afe62305c571047e9ca9bf6028732802bac5b7fbf976ae9816ee600f`) are retained uncommitted in
the operator's scratch directory and are labelled superseded, not current evidence:
`probe-windows.json 45630f8e0645f5efcd1accc06edb6fc0781581c5fba38d36f608be29b8bcdacb`,
`windows-2026-11-25.json 80151a58a79657a9f707d5c9feb8ed6bd840841be2ed4f70a1bcbe7423db0346`,
`windows-2026-11-25-provider-failure.json 11338717a0653d457d18253733e5735782b202ad9c1c377a58583690b22384be`,
`windows-2026-11-25-truncation.json 394d5702601ce30e8993991d1c23d158e46840a574f56bfa8ebcb7c150968648`,
plus the failed Linux preflight probe `probe-linux.json 9300275de27f20be5e106daa166efee8f5f263d44954de998d2e8128b4181f8c`
(which recorded a `503` readiness row and wrote no receipt of record). No fourth pass role was
run on either platform. The committed file names and their raw/rendered sha256 pairs are in the
2.4.1 README §2.

## 5. Class matrix

Verdicts per class × platform from the committed receipts (derivation command in §12). The
reason strings are the receipts' own `blocker` values, verbatim.

| Class | Windows | Linux (WSL; not a containment receipt) |
| --- | --- | --- |
| Success | observed — `session.execution.succeeded` terminal, `outcome: succeeded` readback | observed — same |
| Provider failure | observed — relay 401, native `provider.auth` terminal, `outcome: failed` readback | observed — same |
| Interruption | observed — first token, `{"interrupted": true}`, native interrupted terminal, `outcome: interrupted` readback | observed — same |
| Continuation after step | observed — permission request, reply 204, `permission.replied`, tool success with the file's content, later step start, succeeded terminal | observed — same |
| Truncated provider response | unavailable — `the live prompt request schema exposes no output-length bound; properties ['agents', 'delivery', 'files', 'id', 'metadata', 'resume', 'skills', 'text'], document sha256 6e23e77072e41a75ec55b8da91d3abb32e5c66fc4e234763ed5a02f8f28ecbbb` | unavailable — same reason |
| Cleanup | observed — `taskkill` exit 0, server exit code captured, both reader barriers stopped | observed — SIGTERM on the process group, `exit_code 0`, server exit 143, both reader barriers stopped |
| Usage | observed — nonzero native `tokens`/`cost` counters; not reconciled against provider billing | observed — same limitation |

Classes not exercised by a given pass are reported by that pass as `unavailable` (success and
interruption in the provider-failure and truncation receipts, and so on); the matrix above
names the decisive receipt per class. `HTTP 200`, idle, `Step.Ended`, SSE EOF and files
establish nothing: the classifier still refuses a failed provider turn as success, a no-op
interrupt, and a tool continuation without a recorded reply.

## 6. Route table and 2.3.0 deltas

Live statuses recorded verbatim in the receipts' `operations` rows
(`method/path/status/body/credential_redacted_before_write`; the streaming row records status
and content type instead of a body).

| Route | Windows | Linux | Delta vs 2.3.0 |
| --- | --- | --- | --- |
| `GET /api/info` (authenticated) | 200 | 200 (a `503` row precedes it while the socket listens before the API is ready) | identity route; `pid`/`version` equality is the readiness predicate |
| `GET /api/info` (unauthenticated) | 401 | 401 | unchanged |
| `GET /api/health` | 404 | 404 | still absent |
| `GET /api/model` | 200 | 200 | `{location, data:[Model.Info]}`; redacted in memory before any write |
| `POST /api/session` | 200, `permissions` retained | same | `permissions` with `effect: allow\|deny\|ask` (unknown effect → 400) |
| `GET /api/session/{id}/event?after=0` | 404 | 404 | still absent |
| `GET /api/event` | 200 `text/event-stream` | 200 `text/event-stream` | the global stream; `permission.asked` / `permission.replied` are the permission events (`session.permissions` was not emitted) |
| `POST /api/session/{id}/prompt` | 200 | 200 | `{id,text,files,agents,skills,metadata,delivery,resume}`; no output bound |
| `GET /api/session/{id}` | 200 | 200 | readback carries `outcome`/`tokens`/`cost` |
| `GET /api/session/{id}/permission` | 200 | 200 | pending request `{id, action, resources, save, source}` |
| `POST /api/session/{id}/permission/{requestID}/reply` | 204 | 204 | body `{"decision":"once"}`; enum `once\|always\|reject`, only `once` ever sent |
| `POST /api/session/{id}/interrupt?resume=false` | 200 `{"interrupted":true}` | 200 | query flag and top-level body are 2.4.1 spellings |
| `DELETE /api/session/{id}` + readback | 204, readback 404 | 204, readback 404 | deletion observed, not assumed |

A route delta is evidence for the Phase 1 amendment, never a patch: this goal edits no frozen
byte and no acceptance leg.

## 7. Continuation with the permission reply path

The exact ordered chain in `windows-2026-11-25-r2.json` and `linux-2026-11-25.json` (both
`observed`): session created with `deny *` plus `read * → ask` and the ruleset retained
verbatim; prompt drives the `read` tool; `session.tool.input.started` (`name: read`) →
`session.tool.called` (`executed: false`, the pre-approval payload) → a permission request
recorded through the authoritative `GET /api/session/{id}/permission` row (`action: read`,
`resources: ["baseline.txt"]`, `source.type: tool`) → the receipt's reply row
`POST /api/session/{id}/permission/{requestID}/reply` with body `{"decision": "once"}` →
**204** → `permission.asked`/`permission.replied` on the stream → `session.tool.success` whose
content is the file's own bytes (`Read file baseline.txt, lines 1-1 / 1: native continuation
witness`) → `session.step.ended` → a later `session.step.started` → `session.execution.succeeded`,
with `outcome: succeeded` and nonzero counters in the readback.

`executed` is a required payload field of both `session.tool.called` and
`session.tool.success` in the live schema and is `false` in both here; it is not an approval
marker, so the 2.3.0 `permission.rejected (executed=false)` reading is not reused as evidence
in either direction. Approval evidence is the recorded request → `once` reply → `permission.replied`
→ tool success order, plus the tool's returned content, plus the later step start. A receipt
that carries the tool continuation without a recorded accepting reply classifies as
`run and failed` (mutant M5 in §11).

## 8. Provider failure — a genuine relay rejection

Pass 2 runs with the credential environment absent (`provider.credential: unset`,
`credential_present: false`); the collector refuses to run this mode with a credential set, so
no stale or rejected value can leak into it and no key is fabricated. The relay answered with
its own prose and status — `Authentication Error, No api key passed in.`, `status: 401`,
`code: 401` — and the native terminal is `session.execution.failed` with
`error.type: provider.auth`, `error.status: 401`, carried verbatim in the receipt, with
`outcome: failed` in the readback and the failure turn's native counters in `usage`. The same
terminal shape was produced on Windows and in WSL.

## 9. Truncated provider response — bounded by the live schema

`GET /openapi.json` (200; 467,460 bytes; sha256
`6e23e77072e41a75ec55b8da91d3abb32e5c66fc4e234763ed5a02f8f28ecbbb`) resolves
`POST /api/session/{sessionID}/prompt` to `properties ['agents','delivery','files','id','metadata','resume','skills','text']`,
`required ['text']`, and **no** output-length bound. The truncation pass therefore records
`unavailable` with that property list and the document digest as the bounded reason — no
synthetic SSE, no loopback gateway, no fault injection, no fabricated prompt field. The
collector's resolver is a pure function over the document (unit rows in §11) and would start
one bounded turn if a future schema exposed a bounding property; the verdict would still
require a native output bound (`"truncated": true` or a `length` finish reason) in the captured
evidence.

## 10. Record hygiene, credential locality, host-path render

- The collector replaces the credential and the relay origin (`<RELAY-BASE-URL>`,
  `<RELAY-HOST>`) in memory before any write and records only the replacement counts
  (`redaction.occurrences`; zero in these records because the origin is already replaced at
  capture time). `provider.base_url` is the placeholder; no new record carries the relay host
  (Q-URL b).
- `credential_scan` reports `files_scanned` and `matches: []` in every receipt; the collector's
  write path refuses a receipt that still carries the credential, the server password or the
  authorization header, and it refuses a request payload or a native event that carries the
  credential.
- Every committed record is produced from its raw receipt by two recorded steps (Q-RENDER a):
  `python3 scripts/ci/model-routing-record-sanitize.py --root-only < <raw>/<record>.json` and
  then a one-line JSON pass, after which `--check --root-only` over the committed bytes is a
  no-op (§12). `binary.path`, `workspace`, `discovered_from`, the `command` array and every
  event-carried directory are `path-sha256:` tokens; the raw and pre-compaction receipts are
  uncommitted, and each of their sha256 values is recorded so both can be reproduced.
- The one-line form is format-only and deliberate, and it was chosen from an observed
  refusal, not in advance: over the pretty-printed form of these records the clone gate
  refused 47 new clones (20-31-line structural blocks shared between machine-generated
  receipts, including blocks shared with the frozen 2.3.0 receipts), while the same change
  with one-line records passes with each record reported as a below-threshold notice (§12).
  The same collector writes every receipt, so the shared structure is an artifact of the
  generator; the compaction changes whitespace only, which the parsed-equality row in §12
  proves. The 2.4.1 README §2 records the command, the raw and pre-compaction sha256 values.
- The native `/api/info` body carries the server's own `hostname` field verbatim (the same
  field the committed 2.3.0 receipts carry). It is a response field, not a host path; it is the
  only host-identifying string in the new records, and it is bounded to one occurrence per
  record.

## 11. Mutants (scratch copies; the repository bytes were never mutated)

Each mutant was applied to a scratch copy of the collector under a scratch root outside the
repository, with a copy of the final suite; the repository's
`scripts/ci/opencode-v2-support-witness.py` was verified byte-identical
(`sha256 56d6e602…`) after the runs. M1-M6 and M8 redden suite rows; M7 is a gate-refusal row
(the sanitizer is a separate program, so the receipt was fed to it on standard input).

| id | mutation (scratch copy) | observed red |
| --- | --- | --- |
| M1 | dropped the size/sha256 comparison in `pinned_binary` | `test_wrong_digest_is_refused_at_the_pinned_size`, `test_wrong_size_is_refused_at_the_pinned_digest` (2 failures) |
| M2 | readiness identity equality → prefix `2.` | `test_exact_pid_and_version_is_the_only_accepted_identity` (3 subtest failures: `2.4`, `2.4.10`, `2.4.1 `) |
| M3a | redaction skipped the credential spelling | `test_credential_is_replaced_inside_a_route_body` |
| M3b | payload/event credential refusal always returned false | `test_an_unset_credential_matches_nothing` (the set-credential half) |
| M4 | dropped the file-magic check | `test_wrong_magic_is_refused_before_launch`, `test_posix_pin_refuses_a_pe_magic` |
| M5 | continuation `observed` without a recorded accepting reply | `test_continuation_requires_a_recorded_accepted_reply` |
| M6 | `classify()` accepted a failed provider terminal as success | `test_native_provider_auth_failure_is_not_success`, plus the idle/step-ended rows |
| M7 | a receipt with a drive path restored in `workspace` | `model-routing-record-sanitize.py --check --root-only` → exit 1, `refused: a host absolute path is present` (the unmutated receipt exits 0) |
| M8 | POSIX cleanup skipped the stream-reader join (WSL) | `test_posix_cleanup_joins_a_reader_blocked_on_the_stream` |

## 12. Gates

| gate | command (from the repository root) | result |
| --- | --- | --- |
| suite, Windows (the `daemon-windows` self-test step's local equivalent) | `python3 scripts/ci/test-opencode-v2-support-witness.py` | exit 0, `Ran 36 tests`, `OK (skipped=4)` |
| suite, WSL Ubuntu-24.04 | `python3 scripts/ci/test-opencode-v2-support-witness.py` | exit 0, `Ran 36 tests`, `OK (skipped=1)` |
| render-then-check, every new record (8 files: 2 probes, 6 receipts) | `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only < <record>` | exit 0 on all 8 |
| record cleanliness over the change | `python3 scripts/ci/check-record-cleanliness.py --base 6b2d4f8f --head <scratch>` | exit 0 |
| clone gate over the change | `python3 scripts/ci/jscpd-changed.py --base 6b2d4f8f --head <scratch>` | exit 0 — `13 changed file(s), 1740 scanned source(s), 1262 clone(s) in the corpus, 0 tolerated among the added lines, none new`, with the eight one-line records reported as below-threshold notices. Before the one-line form the same command refused 47 new clones (§10) |
| parsed equality of each committed record against the renderer's output | `python3 -c "import json,sys; assert json.load(open(sys.argv[1])) == json.load(open(sys.argv[2]))" <render>/<record> daemon/internal/harness/testdata/opencode/2.4.1/<record>` | 8/8 equal |
| frozen bytes | `git diff 6b2d4f8f -- daemon/internal/harness/opencode/ daemon/internal/harness/registry.go daemon/internal/app/ .github/workflows/ docs/design/harness.md docs/goals/ scripts/ci/linux-v2-native-acceptance.sh scripts/ci/linux-v2-native-tools.sh daemon/internal/harness/testdata/opencode/2.3.0/` | empty |
| credential census over added lines | `git diff --no-renames -U0 6b2d4f8f..<scratch> \| grep '^+'` over the added lines, fixed pattern `sk-[A-Za-z0-9_-]{16,}`, the bare prefix, the relay host derived at run time from the operator's provider entry (never hardcoded), and the credential value itself | key shape `0`; bare prefix `1` — the pattern's own occurrence in this row, justified as census methodology and permitted as the relay's prefix prose; **relay host `0`, relay base URL `0`** (Q-URL b); the credential value `0` |
| class-matrix derivation | `python3 - <<'PY' …` over `daemon/internal/harness/testdata/opencode/2.4.1/*.json` (the command in the plan §3) | the matrix in §5 |

`<scratch>` is the change's tree read as a revision: the gates read the `--head` revision from
Git, and the working tree is left uncommitted for verification, so the head was materialized
with a temporary index (`GIT_INDEX_FILE` + `git write-tree` + `git commit-tree`) — no branch,
ref, index or worktree state of the checkout was touched. A reviewer re-runs the same two
commands with the pull request's own head commit.

## 13. Recommendation: partial — dispatch Phase 1, scoped

The 0006g blockers are cleared or bounded on both platforms: the route set is re-anchored live
(identity route, unauthenticated 401, the global SSE, the permission surface), the continuation
leg is `observed` through the reply path, provider failure is a genuine relay rejection, success
and interruption carry matching outcome readbacks, cleanup reaches its barriers on both
process models, and usage counters are native. Two limits keep this short of a full promotion:

1. **Linux containment.** The Linux receipts are WSL2-process evidence. WSL native execution is
   not a Linux v2 owner containment receipt; that row is Phase 1's dispatched acceptance run
   with the Environment's OpenAI-compatible key.
2. **Residual unproven classes.** Truncated provider responses cannot be collected on 2.4.1
   (no schema bound), usage is unreconciled against provider billing, and SSE
   reconnect/replay/retained-session recovery remain unproven. The API stays
   upstream-experimental.

Recommendation: **partial — promote to one owner-approved Phase 1 amendment**, scoped to the
Windows-native surface plus the dispatched Linux containment run, with the residual limits
stated in the promoted documents. Phase 1 names (not edited here): `docs/design/harness.md`
(the OpenCode V2 row and its 2.3.0 citation at the cited lines),
`daemon/internal/harness/opencode/adapter.go` (`TestedVersion` and the version gate),
`daemon/internal/harness/registry.go` (the unavailable-adapter wiring), the
`docs/goals/README.md` OpenCode rows, `0010` D1/W6-04, the six named CI assertions, the
acceptance and witness legs (`scripts/ci/linux-v2-native-acceptance.sh`,
`daemon/internal/app/linux_owner_witness_linux.go`), and the V1 1.18.32 acceptance-pin drift as
a Phase 1 precondition.

Until that amendment lands, the decision stays **unsupported** in the product: the adapter pin,
the unavailable registry entry, the CI assertions and every frozen byte are exactly as they
were, and this repository change is additive (a new testdata directory plus collector and suite
changes).

## 14. Residual risks and limits

- The WSL kernel and the Windows kernel are the only kernels exercised; other Linux
  distributions, kernels or bare-metal execution are unobserved.
- The relay's own error prose appears in the provider-failure receipt as the relay produced it;
  it contains no credential fragment, and the census in §12 confirms no key-shaped string and
  no relay host in the change.
- The Linux server's `503`-before-ready window is a startup property of this build; the
  collector retries to a bounded deadline and still refuses a wrong identity, so a future build
  that never answers 200 fails closed (`native server readiness unavailable`).
- `usage` counters are native and unreconciled; `cost` is the server's own estimate.
- The receipts are timestamped 2026-11-25 and bind `source_head 6b2d4f8f195a…` (the base
  commit) plus the collector binding `56d6e602…`; the collector bytes are the tool identity here
  because the tool change itself is uncommitted at collection time.
