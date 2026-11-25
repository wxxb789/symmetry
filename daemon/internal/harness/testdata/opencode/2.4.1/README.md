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
and `version == "2.4.1"` exactly), records the route rows verbatim, replaces the credential,
the relay base URL and the relay host in memory before any write, bounds the SSE capture at
8 MiB and reaches the process/socket/reader cleanup barriers.

| receipt | mode | raw sha256 (uncommitted) | pre-compaction sha256 (uncommitted) | committed sha256 |
| --- | --- | --- | --- | --- |
| `probe-windows.json` | probe | `71516a99bf1fda1038e988e0a23850566531d7eca36335b535fa1406702f3213` | `93e3d1954a4ab1745c6d65b049e889f0057db5fbb467e1002fa12b4a626b1543` | `a1238edd7533ec89d5428313b433313254b52cc25f31d01b8da7e133dbd7ed52` |
| `probe-linux.json` | probe | `78aa649fbad114e8165485cf463a3527522ce4687868dc7a61368699fd4066c6` | `07cacda5706e344e86e1313f3176b70ee2a9d6782fe4d282e38928ad88668ab6` | `e1661d1fbc3630ab5e2d2725490a32b85b6d352c2938cfb27cfc934054f32d3b` |
| `windows-2026-11-25-r2.json` | full | `35d89994cea6129f9fcc450f231c0480095371f043aa3425b7b229e5cba210f7` | `c1d9d7713581de4da656344871964079853e338762c56d05f0f2f12f34946895` | `88a6a8c77938706fbb759f443360f94bd93370dc43cfdcb86ffaa7a66b18b564` |
| `linux-2026-11-25.json` | full | `6ab27bde8ea7556808bf841860c715ec4410e1336aa72d6a034a0009f2b4777e` | `2ede51de419d98f77e8ab38cacb5280072121b9c0f0200781c09e2683dbfee87` | `5a46da78a5c8f273ca1bd436f4cece3ec6c6f7109ad54824e5e95d351d628503` |
| `windows-2026-11-25-provider-failure.json` | provider-failure | `88d8c109576f8ced6d8a8848bb613dcb955273bfd119e4d62538ec056321488b` | `958dcb32c7ecf5fdbfe1f3ce96cabc26a80a04810022ed4d6e74cd7572928ec7` | `a1f9435fb813637a7ee7be195ae3ec6c3fcabde1af641202fdf6fc7d88818e05` |
| `linux-2026-11-25-provider-failure.json` | provider-failure | `6b2b7807360a5fdff20ff6277b1d245388671a188b044d09c96668349ea4cd71` | `e3e5d2c9a5a3bab69d74b4633d00c45d8f059d5290191fc36e52c050a01b2d00` | `55a1691d2d9ff8275fa4f500c811597d8fcf3d2b75424c9e5a8c489ec1724a90` |
| `windows-2026-11-25-truncation.json` | truncation | `02ab07840acb0329e3f7c1a63e9ecc5119c98df44388802940259142df77e20e` | `f4855a31e1f92cf21085f599ffdeb5cb02f7330e5966f13e62cbbe95e3963a33` | `64b04b4d73677c1c5d405d5ff6d76a4bc49e917a7470888b7d85068ef9be01f0` |
| `linux-2026-11-25-truncation.json` | truncation | `9489fb6d5e520e62fe03c833898258ef2af9ea5d9d1d9534060c6136aa571423` | `40e85d111afcc059043cafe451aeedba71c76280a82a52a9f514a5652c4179ae` | `ebcb186a1dd89e5597120060731c498460dead9912caba38ebf2e4aa29e2fef5` |

Every committed receipt carries the collector binding
`56d6e602631fdd8d0c3baa34dd3821df3cc0d332ee0d33aecc59afed0b126bea` for
`scripts/ci/opencode-v2-support-witness.py` and
`8df46499b267f97b0e309f456bbbf2eec5c5ce98ceef92685d7a49bd798a2f93` for
`scripts/ci/source_subject.py` in its own `binding_sha256` field, and records
`source_head 6b2d4f8f195a…` (the base commit; the collector changes are uncommitted at
collection time, which is why the binding, not the head, is the byte identity of the tool).

Each committed record is produced from its raw receipt by two recorded steps (Q-RENDER a):
the repository renderer's root-only pass, then a one-line JSON pass.

```text
python3 scripts/ci/model-routing-record-sanitize.py --root-only < <raw>/<record>.json > <render>/<record>.json
python3 -c "import json,pathlib,sys; source=pathlib.Path(sys.argv[1]); target=pathlib.Path(sys.argv[2]); target.write_bytes(json.dumps(json.loads(source.read_text()), ensure_ascii=True, separators=(',',':')).encode() + b'\n')" <render>/<record>.json daemon/internal/harness/testdata/opencode/2.4.1/<record>.json
python3 scripts/ci/model-routing-record-sanitize.py --check --root-only < daemon/internal/harness/testdata/opencode/2.4.1/<record>.json
python3 -c "import json,sys; assert json.load(open(sys.argv[1])) == json.load(open(sys.argv[2]))" <render>/<record>.json daemon/internal/harness/testdata/opencode/2.4.1/<record>.json
```

The one-line form is deliberate and format-only: the changed-file clone gate
(`scripts/ci/jscpd-changed.py` over the `--head` tree with `.jscpd.json`'s 10-line/50-token
thresholds — the scope is read from the comparison base, so a change cannot exempt itself)
reads machine-generated records as duplication, because the same collector writes every
receipt and they share their structural blocks by construction. A record committed as a single
JSON object on one line is below that threshold; the gate reports it as a notice rather than
silently skipping it, and no record can embed a run of another record's lines. The parsed
equality check above proves the compaction changed whitespace only; the data is exactly the
renderer's output, and the raw and pre-compaction sha256 values stay in the table so both
earlier forms can be reproduced byte-for-byte. `.gitattributes` pins `*.json text eol=lf`, so
that step writes bytes (LF) instead of letting the platform translate newlines; a CRLF spelling
would be refused by `git add` under the pin.

**Pass accounting.** Three pass roles per platform: `full` (Pass 1), `provider-failure`
(Pass 2), `truncation` (Pass 3). The `truncation` role resolved to a schema-grounded
`unavailable` on both platforms, so the spare slot was never needed for a class gap. The
readiness loop first shipped with a startup-race defect: the Linux server answers `503` from
its listening socket before the API is ready, which the first revision treated as an identity
mismatch instead of a readiness wait. The Windows roles were therefore re-run once under the
final revision (binding `56d6e602…`); their first Revision-A receipts (binding
`3598d6a1afe62305c571047e9ca9bf6028732802bac5b7fbf976ae9816ee600f`) are retained
uncommitted in the operator's scratch directory and are labelled here as superseded, not as
current evidence: `probe-windows.json 45630f8e…`, `windows-2026-11-25.json 80151a58…`,
`windows-2026-11-25-provider-failure.json 11338717…`, `windows-2026-11-25-truncation.json
394d5702…`, `probe-linux.json 9300275d…` (the last one is the failed Linux preflight probe,
which recorded a `503` readiness row and no receipt of record). No fourth pass role was run on
either platform, and no pass was left pending.

## 3. Per-platform class matrix

Windows (`nt`), decisive receipt named per class; Linux (`posix`) the same, with the WSL
containment caveat of §1.

| Class | Windows | Linux |
| --- | --- | --- |
| Success | observed — native `session.execution.succeeded` terminal and `outcome: succeeded` readback (`windows-2026-11-25-r2.json`) | observed — same terminal and readback (`linux-2026-11-25.json`) |
| Provider failure | observed — genuine relay 401 with the native `provider.auth` terminal and `outcome: failed` readback (`windows-2026-11-25-provider-failure.json`) | observed (`linux-2026-11-25-provider-failure.json`) |
| Interruption | observed — first token, `interrupt?resume=false` → `{"interrupted": true}`, native `session.execution.interrupted`, `outcome: interrupted` (`windows-2026-11-25-r2.json`) | observed (`linux-2026-11-25.json`) |
| Continuation after step | observed — permission request on the read tool, `{"decision": "once"}` reply 204, `permission.replied`, tool success carrying the file's content, a later `session.step.started`, succeeded terminal (`windows-2026-11-25-r2.json`) | observed (`linux-2026-11-25.json`) |
| Truncated provider response | unavailable — the live prompt request schema exposes no output-length bound; properties `['agents','delivery','files','id','metadata','resume','skills','text']`, document sha256 `6e23e77072e41a75ec55b8da91d3abb32e5c66fc4e234763ed5a02f8f28ecbbb` (`windows-2026-11-25-truncation.json`) | unavailable — same reason (`linux-2026-11-25-truncation.json`) |
| Cleanup | observed — `taskkill` exit 0, server exit code captured, both reader barriers stopped (all Windows receipts) | observed — SIGTERM on the process group, `exit_code 0`, server exit 143, both reader barriers stopped (all Linux receipts) |
| Usage | observed — nonzero native `tokens`/`cost` counters in the readbacks (`windows-2026-11-25-r2.json`, and zero-cost failure counters in the provider-failure receipt); not reconciled against provider billing | observed — same limitation (`linux-2026-11-25.json`, `linux-2026-11-25-provider-failure.json`) |

`HTTP 200`, idle, `Step.Ended`, SSE EOF and files still do not establish success; the
classifier that decides each row is unchanged in that respect and still refuses a failed
provider turn, a no-op interrupt and an unapproved tool continuation.

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
| `POST /api/session/{id}/permission/{requestID}/reply` | 204 (`{"decision":"once"}`) | 204 | reply enum `once\|always\|reject`; only `once` is ever sent |
| `POST /api/session/{id}/interrupt?resume=false` | 200 `{"interrupted":true}` | 200 | query flag and top-level body are 2.4.1 spellings |
| `DELETE /api/session/{id}` + readback | 204, readback 404 | 204, readback 404 | unchanged |

## 5. Decisive observations

- **The continuation leg finally has the reply path.** The session is created with `deny *`
  plus `read * → ask`; the prompt drives the `read` tool; `session.tool.called` (pre-approval,
  `executed:false`) is followed by a permission request (`action: read`, `resources:
  ["baseline.txt"]` or the absolute path), the receipt's reply row is `POST …/reply` with
  `{"decision":"once"}` → **204**, `permission.replied` follows, then `session.tool.success`
  carries the file's own bytes (`Read file baseline.txt, lines 1-1 / 1: native continuation
  witness`), then `session.step.ended` and a later `session.step.started`, then
  `session.execution.succeeded`. The class is `observed` only with that recorded reply — a
  tool call without the reply is `run and failed`.
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
  `version == "2.4.1"`; the Linux receipts additionally record the `503`-then-`200` startup
  sequence, because the server answers from its listening socket before the API is ready.
- **Usage counters are native only.** They are nonzero in the successful readbacks and
  zero-cost in the failure receipt; successful/billable usage and failed-turn accounting stay
  unreconciled against the provider.
- **Deleted sessions read back.** `DELETE` answers 204 and the following `GET` answers 404
  `SessionNotFoundError` on both platforms, so session removal is observed rather than
  assumed.
- The `/api/info` body carries the server's own `hostname` field verbatim (the same field the
  committed 2.3.0 receipts carry). It is a response field, not a host path.

## 6. Credential and record locality

The configured provider is `tencent-litellm/deepseek-v4.1-flash-fast`. The credential is read
at run time from the operator's OMP provider entry (`providers.tencent-litellm.apiKey` in
`~/.omp/agent/models.yml`) and injected only into the collector subprocess environment as
`TENCENT_GLM_API_KEY`; it is never in argv, a file, a receipt, a log line or durable state.
The receipts keep `credential_scan` (`files_scanned`, `matches: []`), the in-memory
credential/relay-origin replacement with its counts, and the config/payload/event refusals.
The literal relay origin is replaced before any write (Q-URL b: `provider.base_url` is
`<RELAY-BASE-URL>`, the host is `<RELAY-HOST>`), and the repository renderer then removes every
host absolute path. The credential/key census over the change and the renderer's root-only
gate are reported with their exact commands in
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
