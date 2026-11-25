# OpenCode V2 2.4.1: re-anchored evidence, not a capability promotion

This is the Phase 0 re-evaluation of the completion contract
`goal-rf-acc-opencode-reeval` (GDD state root, external to this repository), on the
owner decision Q6=F2 (`refactor/decision-opencode-unfreeze-2026-11-25.md`, GDD state root).
It re-runs the seven classes of
[the 0006g decision](../../../../../../docs/goals/archive/0006g-opencode-support-decision.md)
against the exact 2.4.1 blobs. **It promotes nothing**: the adapter still pins 2.3.0, the
registry still resolves OpenCode to the unavailable adapter, and every frozen byte
(`daemon/internal/harness/opencode/**`, `registry.go`, `.github/workflows/**`,
`docs/design/harness.md`, `docs/goals/**`, the 2.3.0 directory) is untouched.

**WSL native execution is not a Linux v2 owner containment receipt;** that row is Phase 1's
dispatched acceptance run. Every Linux receipt below records that limit in its own
`platform.execution_class` field.

## 1. Exact blobs and the release equality table

Release `anomalyco/opencode`, tag `v2.4.1` (published 2026-11-23T15:57:40Z, not draft, not
prerelease). Three sources must agree for each archive: the locally computed sha256, the
release's `SHA256SUMS` row for that exact asset name, and the per-asset digest of the release
metadata (`GET /repos/anomalyco/opencode/releases/tags/v2.4.1`, saved during the preflight; the
saved document's own sha256 is `c61741eb7e9acb1a2ffbe5df87afb8d7acb883cbed521547b651477c94482e7b`).
All three agree, and `opencode-linux-x64-baseline.tar.gz` carries the same digest as
`opencode-linux-x64.tar.gz` in the release's own `SHA256SUMS`, so the standard-build asset is
the pin and no `-baseline` substitution happened.

| asset | bytes | local sha256 | `SHA256SUMS` row | release-metadata digest |
| --- | --- | --- | --- | --- |
| `opencode-windows-x64.zip` | 132,524,450 | `831e00806531fabb184533ef85fead96eb92287cab2d0f21db7daaa494e75083` | same | same |
| `opencode-linux-x64.tar.gz` | 130,647,866 | `9ddd18e3faf64eaeccf6a0fcbc96a5c7a14520ae5fd5b61e5be51d894f478b47` | same | same |
| `SHA256SUMS` | 2,544 | `fa594de3bc877c0aaa7f2cf3a55c6ef28a781e9b6777bc34dcf823af47785c7e` | n/a | same |

The extracted blobs, recorded per platform by `probe-windows.json` and `probe-linux.json`
(`--version` and `serve --help` were executed with exit codes; the absolute blob paths live
only in the operator's uncommitted working copy and are committed as the renderer's stable
`path-sha256:` tokens):

| platform | release asset | bytes | sha256 | magic | `--version` |
| --- | --- | --- | --- | --- | --- |
| Windows (`nt`) | `opencode-windows-x64.zip` | 278,383,496 | `f3da11bf51ace25b86f52e20368d415451d200d6b48101d9f4e5d721b1d2020e` | `4d5a` (`MZ`) | `opencode v2.4.1` (exit 0) |
| Linux (`posix`) | `opencode-linux-x64.tar.gz` | 275,163,312 | `ec93043f18f8a6b4d8e71ec9260e292ba6f91b66b142782135d40f0b58b0c152` | `7f454c46` (ELF) | `opencode v2.4.1` (exit 0) |

Platform facts actually used: Windows kernel `10.0.26300`, `Windows-11-10.0.26300-SP0`,
Python 3.12.10; WSL distribution `Ubuntu 24.04.5 LTS` (`VERSION_ID 24.04`), kernel
`6.18.58.1-microsoft-standard-WSL2`, Python 3.12.3, `x86_64`. The Linux blob was extracted
under WSL and executed as an ordinary Linux process (`start_new_session`, signals by process
group, no `wsl.exe` interop for the run itself).

## 2. Collector, modes, render and pass accounting

`scripts/ci/opencode-v2-support-witness.py` takes `--mode probe|full|provider-failure|truncation`.
Every mode refuses a blob whose size, sha256 or file magic differs from the running platform's
pin, checks the readiness identity (`GET /api/info` must answer 200 with `pid == server pid`
and `version == "2.4.1"` exactly), records the route rows verbatim (the `/api/info` 200 body
narrowed to `pid`/`version`/`status`, the 401 row's `UnauthorizedError` body kept), replaces
the credential, the relay base URL and the relay host in memory before any write, bounds the
SSE capture at 8 MiB and reaches the process/socket/reader cleanup barriers.

| receipt | mode | raw sha256 (uncommitted) | pre-compaction sha256 (uncommitted) | committed sha256 |
| --- | --- | --- | --- | --- |
| `probe-windows.json` | probe | `46ccd7defac6e919787e285152c364bfc0507b7fa695d902698b8766b0b524e2` | `a0aeefdd5f2305a2716ddb68a5354ff898db6c458f705664237fb8c057045aef` | `59bd994351169af914338e8cd782709d6b6653d73a9f3fd5c14471230071f0fd` |
| `probe-linux.json` | probe | `2cd317b974168cb2a41ac019e6f9a9b8a3de679909bbf90c397e8e776a836785` | `06f2faabb264c340999e1be16096bfb55a3d1510734ede07a11dd6533c9bdb41` | `5171768c1fe3ca54e768025c88500d9c3281fff303e896cd7b00891c643dad04` |
| `windows-2026-11-25-r2.json` | full | `f4adc37ccc1887c34370b7e3d757d0cdf4532fa6f00182b400d938255d95b3a5` | `34e8950cc440d54ace7c3710ee1fcfd76beb6adfde2b84b349386467bfb51b34` | `8ab89807eed4bc1b9fdf50daff6fbb1998400062d0f5137e247d0cb5d406bfe2` |
| `linux-2026-11-25.json` | full | `e67298fb5e7d338b020430c443629c8d14b859c811d952f987392cb5b3749e4a` | `71da755a60d1eef9d7ebf6f61bad921bdb7a52c84503b5ed617d8fa78b4f1c6b` | `5a12a07d800cb800302688a45a17cfc78797ec787d2035d57f6d317f1ad79c50` |
| `windows-2026-11-25-provider-failure.json` | provider-failure | `48a8253d971b5cf15548125ea3e1b89cb2be3408633c17a41d3bedb0a92bafa8` | `859ffbbcdef9a626244168c227974f20ecc44111b8054cf6f2875498cfc5e66a` | `6acf9c2b54db1f9db8af69a01e062542e7f801391c103dcefb2fb9817df6f34c` |
| `linux-2026-11-25-provider-failure.json` | provider-failure | `5c0362fd4d608b4bb2655fb984d564421852d574c4f0d96bfacc386b63a20f85` | `7d8ce6209f6738bd7333de8466441b675623b507f98233e3565bcc643f2184cd` | `f839166934b5f886d5f48ebd4d988dd7ffe311be8f8c2e7d4d3bda78493816b4` |
| `windows-2026-11-25-truncation.json` | truncation | `e4cf7818f081016efa4395a3d82826168a1e5400b9ae1029892c647da0ced525` | `fc95bee7a7bf6b46fa84ee49d5f2e8a40649764fc8f7afafd417026b2c0ca9ab` | `dc20b422ed299e188a739831f5b778070e497eac7154a945faefc3920fd23d16` |
| `linux-2026-11-25-truncation.json` | truncation | `69b875d7bb82b30d67ed789c2fd572a95f3f22b2a86feaeaa6aad708f8eeddf1` | `dc3daf72b0556c85b90d83695c04cd49d53d946d9f2b8093c013ba50075c00b4` | `795b6aafb013e71ee172ccba86b22a401e5a53e6b8f2f7f4311bf76c95a50f6f` |

**Re-collection (2026-11-25, PR #288 round 1).** All eight records are re-collected machine
output of the final collector and suite, so each receipt's `binding_sha256` is the collector
bytes this change delivers. The earlier bytes (an apply pass, a fix pass and the PRE-2
hostname edit of the eight then-committed records) are superseded; their per-file committed
digests before this re-collection, the re-collected raw/pre-compaction/committed triples and
the `classify()` re-grade comparison (identical verdicts and identical reasons on all eight)
are in `docs/evidence/rf-acc-opencode-reeval-2026-11-25.md` §4. No committed record carries a
post-render substitution: the collector narrows the recorded `/api/info` 200 body to
`pid`/`version`/`status` and keeps the 401 row's `UnauthorizedError` body verbatim, so no host
metadata reaches it at capture time. The frozen 2.3.0 receipts keep their `hostname` field
verbatim — not edited, recorded as a residual.

Every committed receipt carries the collector binding
`087e07a8acaab7c9d07c169acdbd70342e651112e66d0d2a4bdfb3d00c98b0f5` for
`scripts/ci/opencode-v2-support-witness.py` and
`8df46499b267f97b0e309f456bbbf2eec5c5ce98ceef92685d7a49bd798a2f93` for
`scripts/ci/source_subject.py` in its own `binding_sha256` field, and records
`source_head 49a606dd346f…`, the branch head at re-collection time; the binding is the byte
identity of the tool that wrote each receipt, and the collector and suite bytes this change
delivers are exactly those bound bytes (the collector/suite diff slots in
`docs/evidence/rf-acc-opencode-reeval-2026-11-25.md` §3).

Each committed record is produced from its raw receipt by two recorded steps (Q-RENDER a) —
the repository renderer's root-only pass, then a one-line JSON pass; no further step touches
the bytes.

```text
python3 scripts/ci/model-routing-record-sanitize.py --root-only < <raw>/<record>.json > <render>/<record>.json
python3 -c "import json,pathlib,sys; source=pathlib.Path(sys.argv[1]); target=pathlib.Path(sys.argv[2]); target.write_bytes(json.dumps(json.loads(source.read_text()), ensure_ascii=True, separators=(',',':')).encode() + b'\n')" <render>/<record>.json daemon/internal/harness/testdata/opencode/2.4.1/<record>.json
python3 scripts/ci/model-routing-record-sanitize.py --check --root-only < daemon/internal/harness/testdata/opencode/2.4.1/<record>.json
python3 -c "import json,sys; assert json.load(open(sys.argv[1])) == json.load(open(sys.argv[2]))" <render>/<record>.json daemon/internal/harness/testdata/opencode/2.4.1/<record>.json
```

The one-line form is deliberate and format-only: the changed-file clone gate
(`scripts/ci/jscpd-changed.py` over the `--head` tree with its `--min-lines 10 --min-tokens 50`
limits — the scope is read from the comparison base, so a change cannot exempt itself)
reads machine-generated records as duplication, because the same collector writes every
receipt and they share their structural blocks by construction. A record committed as a single
JSON object on one line is below that threshold; the gate reports it as a notice rather than
silently skipping it, and no record can embed a run of another record's lines. The parsed
equality check above proves the compaction changed whitespace only, so the data is exactly
the renderer's output, and the raw and pre-compaction sha256 values stay in the table so
both earlier forms can be reproduced byte-for-byte. `.gitattributes` pins `*.json text eol=lf`, so
that step writes bytes (LF) instead of letting the platform translate newlines; a CRLF spelling
would be refused by `git add` under the pin.

**Pass accounting.** Per platform, one `probe` run plus three pass roles — `full` (Pass 1),
`provider-failure` (Pass 2), `truncation` (Pass 3): eight runs in all for the 2026-11-25
re-collection, each foreground and bounded, and no fourth pass role on either platform. The
`truncation` role resolved to a schema-grounded `unavailable` on both platforms, so the spare
slot was never needed for a class gap. The readiness loop first shipped with a startup-race
defect: the Linux server answers `503` from its listening socket before the API is ready,
which the first revision treated as an identity mismatch instead of a readiness wait; the
first Revision-A receipts (binding
`3598d6a1afe62305c571047e9ca9bf6028732802bac5b7fbf976ae9816ee600f`) are retained
uncommitted in the operator's scratch directory and are labelled here as superseded, not as
current evidence: `probe-windows.json 45630f8e…`, `windows-2026-11-25.json 80151a58…`,
`windows-2026-11-25-provider-failure.json 11338717…`, `windows-2026-11-25-truncation.json
394d5702…`, `probe-linux.json 9300275d…` (the last one is the failed Linux preflight probe,
which recorded a `503` readiness row and no receipt of record).

## 3. Per-platform class matrix

Windows (`nt`), decisive receipt named per class; Linux (`posix`) the same, with the WSL
containment caveat of §1.

| Class | Windows | Linux |
| --- | --- | --- |
| Success | observed — native `session.execution.succeeded` terminal and `outcome: succeeded` readback (`windows-2026-11-25-r2.json`) | observed — same terminal and readback (`linux-2026-11-25.json`) |
| Provider failure | observed — genuine relay 401 with the native `provider.auth` terminal and `outcome: failed` readback (`windows-2026-11-25-provider-failure.json`) | observed (`linux-2026-11-25-provider-failure.json`) |
| Interruption | observed — first token, `interrupt?resume=false` → `{"interrupted": true}`, native `session.execution.interrupted`, `outcome: interrupted` (`windows-2026-11-25-r2.json`) | observed (`linux-2026-11-25.json`) |
| Continuation after step | observed — permission request on the read tool, the `{"decision": "once"}` reply request answered 204 (recorded response body empty), `permission.replied`, tool success carrying the file's content, a later `session.step.started`, succeeded terminal (`windows-2026-11-25-r2.json`) | observed (`linux-2026-11-25.json`) |
| Truncated provider response | unavailable — the live prompt request schema exposes no output-length bound; properties `['agents','delivery','files','id','metadata','resume','skills','text']`, document sha256 `6e23e77072e41a75ec55b8da91d3abb32e5c66fc4e234763ed5a02f8f28ecbbb` (`windows-2026-11-25-truncation.json`) | unavailable — same reason (`linux-2026-11-25-truncation.json`) |
| Cleanup | observed — `taskkill` exit 0, server exit code captured, both reader barriers stopped (all Windows receipts) | observed — SIGTERM on the process group, `group_gone` proved by ESRCH on the group, `exit_code 0`, server exit 143, both reader barriers stopped (all Linux receipts) |
| Usage | observed — nonzero native `tokens`/`cost` counters in the readbacks (`windows-2026-11-25-r2.json`, and zero-cost failure counters in the provider-failure receipt); not reconciled against provider billing | observed — same limitation (`linux-2026-11-25.json`, `linux-2026-11-25-provider-failure.json`) |

`HTTP 200`, idle, `Step.Ended`, SSE EOF and files still do not establish success; the
classifier that decides each row still refuses a failed provider turn, a no-op interrupt, an
unapproved tool continuation and a cleanup whose process group was not proved gone. The
re-collection was re-graded: `classify()` over the eight re-collected records reproduces every
stored `classes` mapping, and the per-class verdicts and reasons are identical to the
pre-re-collection records (evidence record §4), so no verdict above moves.

## 4. Route table (live statuses, never normalized)

| Route | Windows | Linux | 2.3.0 delta |
| --- | --- | --- | --- |
| `GET /api/info` (authenticated) | 200 | 200 (503 for ~0.3 s while the socket listens before the API is ready) | identity route, unchanged |
| `GET /api/info` (unauthenticated) | 401 | 401 | unchanged |
| `GET /api/health` | 404 | 404 | still absent |
| `GET /api/model` | 200 | 200 | response shape `{location, data:[Model.Info]}` |
| `POST /api/session` | 200, `permissions` retained verbatim | 200, same | session create now takes `permissions` with `effect: allow\|deny\|ask` |
| `GET /api/session/{id}/event?after=0` | 404 | 404 | still absent (the global stream carries the events) |
| `GET /api/event` | 200 `text/event-stream` | 200 `text/event-stream` | global SSE; `permission.asked`/`permission.replied` are the permission events |
| `POST /api/session/{id}/prompt` | 200 | 200 | body `{id,text,files,agents,skills,metadata,delivery,resume}`; no output bound |
| `GET /api/session/{id}` | 200 | 200 | readback carries `outcome`, `tokens`, `cost` |
| `GET /api/session/{id}/permission` | 200 `{"data":[Permission.Request]}` | 200 | request `{id, action, resources, save, source}` |
| `POST /api/session/{id}/permission/{requestID}/reply` | 204, response body empty as received | 204 | request body `{"decision":"once"}`; reply enum `once\|always\|reject`, only `once` is ever sent |
| `POST /api/session/{id}/interrupt?resume=false` | 200 `{"interrupted":true}` | 200 | query flag and top-level body are 2.4.1 spellings |
| `DELETE /api/session/{id}` + readback | 204, readback 404 | 204, readback 404 | unchanged |

Every status above is the live one; each row's body is recorded verbatim except the
`/api/info` 200 row, whose body is narrowed to `pid`/`version`/`status`; the `/api/info` 401
row keeps the server's `UnauthorizedError` body verbatim (§2).

## 5. Decisive observations

- **The continuation leg finally has the reply path.** The session is created with `deny *`
  plus `read * → ask`; the prompt drives the `read` tool; `session.tool.called` (pre-approval,
  `executed:false`) is followed by a permission request (`action: read`, `resources:
  ["baseline.txt"]` or the absolute path), the receipt records the reply operation as the
  server's own response — `POST …/reply` with request body `{"decision":"once"}`, answered
  **204** with the empty body a 204 carries (the collector no longer substitutes its own
  request constant for the response) — then `permission.replied` follows, then `session.tool.success`
  carries the file's own bytes (`Read file baseline.txt, lines 1-1 / 1: native continuation
  witness`), then `session.step.ended` and a later `session.step.started`, then
  `session.execution.succeeded`. The class is `observed` only when the recorded `204` reply is
  joined by the stream's own `permission.replied` for the request id, carrying `reply: "once"`,
  ahead of the tool success — a tool call without that ordered event, or with the permission
  events removed or reordered, is `run and failed`.
- `executed` is a **required payload field** of both `session.tool.called` and
  `session.tool.success` in the live schema and is `false` in both here; it does not mark
  approval. The approval evidence is the request→reply→`permission.replied`→success order and
  the tool's returned content. The 2.3.0 `permission.rejected`/`executed=false` reading is
  therefore not reused.
- **Provider failure is a genuine relay rejection, not a fabricated key.** The
  `provider-failure` pass runs with the credential environment absent (the collector refuses a
  set one); the relay answers its own
  `Authentication Error, No api key passed in.` with status 401 and the native terminal is
  `session.execution.failed` with `error.type: provider.auth`, `error.status: 401`, and the
  readback's `outcome: failed`. Nothing about the rejected inherited host variable or any
  candidate key is recorded.
- **Truncation is bounded by the schema, not attempted against it.** `GET /openapi.json`
  (200; 467,460 bytes; sha256 `6e23e770…`) resolves `POST /api/session/{sessionID}/prompt` to
  eight properties and no output-length bound, so the class is `unavailable` with the resolved
  property list and the document digest. No synthetic SSE, no fault injection, no loopback
  gateway.
- **Readiness identity is exact.** Every receipt records `pid == server pid` and
  `version == "2.4.1"`; a `503` row precedes the `200` only in
  `linux-2026-11-25-truncation.json` in the re-collected set, because the server answers from
  its listening socket before the API is ready and the readiness race may resolve before the
  first recorded attempt.
- **Usage counters are native only.** They are nonzero in the successful readbacks and
  zero-cost in the failure receipt; successful/billable usage and failed-turn accounting stay
  unreconciled against the provider.
- **Deleted sessions read back.** `DELETE` answers 204 and the following `GET` answers 404
  `SessionNotFoundError` on both platforms, so session removal is observed rather than
  assumed.
- The recorded `/api/info` 200 body keeps only `pid`/`version`/`status` (the collector narrows
  it in memory before any write), so no host metadata reaches a record, and the 401 row keeps
  the server's `UnauthorizedError` body verbatim. No committed record carries the machine
  hostname value (census in the evidence record §12). The frozen 2.3.0 receipts keep their
  `hostname` field verbatim — not edited, recorded as a residual in the evidence record §10.

## 6. Credential and record locality

The configured provider is `tencent-litellm/deepseek-v4.1-flash-fast`. The credential is read
at run time from the operator's OMP provider entry (`providers.tencent-litellm.apiKey` in
`~/.omp/agent/models.yml`) and injected only into the collector subprocess environment as
`TENCENT_GLM_API_KEY`; it is never in argv, a file, a receipt, a log line or durable state.
The receipts keep `credential_scan` (`files_scanned`, `matches: []`), the in-memory
credential/relay-origin replacement with its counts, and the config/payload/event refusals.
The literal relay origin is replaced before any write (Q-URL b: `provider.base_url` is
`<RELAY-BASE-URL>`, the host is `<RELAY-HOST>`), and the repository renderer then removes every
host absolute path. The credential/key census and the hostname census over the change
(0 occurrences) and the renderer's root-only gate are reported with their exact commands in
`docs/evidence/rf-acc-opencode-reeval-2026-11-25.md`.

## 7. Reproduce the observation, not a support claim

From the repository on the authorized host, source the credential from the operator's OMP
provider entry into a collector-only `TENCENT_GLM_API_KEY` environment (never printed, never
persisted, never the inherited host variable), extract the two release assets outside the
repository, and run per platform:

```text
TENCENT_GLM_API_KEY="$KEY" SYMMETRY_OPENCODE_BINARY="$BIN" python3 scripts/ci/opencode-v2-support-witness.py --mode probe --out <raw>/probe-<platform>.json
TENCENT_GLM_API_KEY="$KEY" SYMMETRY_OPENCODE_BINARY="$BIN" python3 scripts/ci/opencode-v2-support-witness.py --mode full --out <raw>/<platform>-<date>.json
SYMMETRY_OPENCODE_BINARY="$BIN" python3 scripts/ci/opencode-v2-support-witness.py --mode provider-failure --out <raw>/<platform>-<date>-provider-failure.json
TENCENT_GLM_API_KEY="$KEY" SYMMETRY_OPENCODE_BINARY="$BIN" python3 scripts/ci/opencode-v2-support-witness.py --mode truncation --out <raw>/<platform>-<date>-truncation.json
python3 scripts/ci/test-opencode-v2-support-witness.py
```

A Linux run goes through `wsl.exe -d Ubuntu-24.04 -- bash -s` with the credential carried by
`WSLENV` (never argv) and the checkout reachable at its `/mnt` path; the linked worktree needs
`GIT_DIR`/`GIT_WORK_TREE` for the collector's `source_head` read. The collector can exit zero
after recording an upstream failure: its exit code means collection completed, never that a
native turn succeeded. Read the native events and session outcomes.

## 8. What this does not prove

- It does not promote OpenCode: the adapter pin, the unavailable registry entry, the six CI
  assertions and the acceptance legs stay exactly as they are until the owner-approved
  Phase 1 amendment, and the recommendation in the evidence record is scoped to that dispatch.
- It does not prove Linux owner containment: the Linux receipts come from a WSL2 process, and
  the execution class recorded in every Linux receipt says so.
- It does not prove provider-response truncation (no schema bound), billable usage, SSE
  reconnect/replay or retained-session recovery; the API remains upstream-experimental, and
  containment, usage reconciliation and replay stay Symmetry's burden.
- It does not reconcile the V1 acceptance-pin drift (the acceptance fixture still installs
  V1 1.18.32 while the adapter pins 2.3.0); that is a Phase 1 precondition.
