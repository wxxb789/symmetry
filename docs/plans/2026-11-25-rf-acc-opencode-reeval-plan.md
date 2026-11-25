# rf-acc-opencode-reeval — plan

Contract: `goal-rf-acc-opencode-reeval.md` (GDD state root, external to the repository),
Phase 0 of the owner decision Q6=F2 (`refactor/decision-opencode-unfreeze-2026-11-25.md`).
Branch `refactor/rf-acc-opencode-reeval`, base `6b2d4f8f` (`origin/main` at dispatch).
Shape reference: `daemon/internal/harness/testdata/opencode/2.3.0/` (README, probe file, per-pass
receipts) plus the collector's own suite, `scripts/ci/test-opencode-v2-support-witness.py`.

This plan records only the implementation choices the accepted contract leaves unresolved.

## 1. Preflight (Stage 0, no repo bytes)

Release `anomalyco/opencode` tag `v2.4.1` (published 2026-11-23T15:57:40Z, not draft, not
prerelease), downloaded to a scratch directory outside the repository. Three sources agree for
each archive: the locally computed sha256, the release's `SHA256SUMS` row for the exact asset
name, and the per-asset digest of the release metadata (`GET /repos/anomalyco/opencode/releases/
tags/v2.4.1`, saved uncommitted with its own sha256 recorded in the README):

| asset | bytes | sha256 (all three sources) |
| --- | --- | --- |
| `opencode-windows-x64.zip` | 132,524,450 | `831e00806531fabb184533ef85fead96eb92287cab2d0f21db7daaa494e75083` |
| `opencode-linux-x64.tar.gz` | 130,647,866 | `9ddd18e3faf64eaeccf6a0fcbc96a5c7a14520ae5fd5b61e5be51d894f478b47` |
| `SHA256SUMS` | 2,544 | `fa594de3bc877c0aaa7f2cf3a55c6ef28a781e9b6777bc34dcf823af47785c7e` |

`opencode-linux-x64-baseline.tar.gz` carries the same digest as `opencode-linux-x64.tar.gz` in
both `SHA256SUMS` and the metadata, as observed at draft time; the standard-build asset is the
pin and no switch to `-baseline` happens. Extracted outside the repository: Windows
`opencode.exe` (278,383,496 bytes, sha256 `f3da11bf…`, magic `MZ`, `--version` → `opencode v2.4.1`)
from the zip, Linux `opencode` (275,163,312 bytes, sha256 `ec93043f…`, magic `7f 45 4c 46`,
`--version` → `opencode v2.4.1`) extracted under WSL Ubuntu-24.04. Prerequisites actually used:
WSL distribution `Ubuntu-24.04` (PRETTY_NAME `Ubuntu 24.04.5 LTS`), kernel
`6.18.58.1-microsoft-standard-WSL2`, Python 3.12.10 (Windows) / 3.12.3 (WSL), git 2.56.1.

The WSL-side OpenCode configuration is staged by the operator at
`<A_HOME>/.config/opencode/opencode.json` as the same `tencent-litellm` provider entry (base URL,
model catalog) with `apiKey` as the environment reference `{env:TENCENT_GLM_API_KEY}`; the
collector reads that path for `provider.tencent-litellm.options.baseURL` exactly as it reads the
Windows-side config. Neither file is a record.

## 2. Live route and schema facts (probed before any pass)

`GET /api/info` → `{"version":"2.4.1","pid":<pid>,…}`; unauthenticated → 401
(`UnauthorizedError`). `GET /api/health` → 404. `GET /api/model` → `{"location":…,"data":[Model.Info]}`.
`POST /api/session` accepts `{location,model,permissions}` and retains
`effect` ∈ {`allow`,`deny`,`ask`} verbatim (an unknown effect is refused 400). The per-session
`GET /api/session/{id}/event?after=0` → 404 (as at 2.3.0). `GET /api/event` → 200
`text/event-stream`. `POST /api/session/{id}/prompt` → 200 with
`{id,text,files,agents,skills,metadata,delivery,resume}` and no output-length field.
`GET /api/session/{id}/permission` → `{"data":[Permission.Request]}`;
`POST …/permission/{requestID}/reply` takes `{"decision":"once"}` → 204.
`POST /api/session/{id}/interrupt?resume=false` → `{"interrupted":true}` (top-level).
`DELETE /api/session/{id}` → 204, readback `GET /api/session/{id}` → 404 `SessionNotFoundError`.
`GET /openapi.json` → 200, 467,460 bytes at probe time, 146 paths; its resolution is recorded by
digest, never committed. `session.tool.success`, `session.step.started`, `permission.asked` and
`permission.replied` arrive on the global stream; `session.permissions` was not emitted.

These facts are probe results, not records; every pass re-records its own status rows verbatim.

## 3. Protocol — passes, order, commands, environment

`scripts/ci/opencode-v2-support-witness.py` gains `--mode {probe,full,provider-failure,truncation}`.
All modes share: the per-platform pin (size, sha256, magic) and its refusals, the child
environment allowlist with private-root overrides, readiness identity (`pid == server pid` and
`version == "2.4.1"` exactly), the route rows, the in-memory credential/relay-origin redaction,
the 8 MiB SSE bound, `credential_scan`, the cleanup barriers and `classify()`.

- `--mode probe` writes `probe-<platform>.json`: blob identity + magic + release asset,
  `--version` and `serve --help` observations with exit codes, platform facts (kernel,
  distribution, execution class), readiness, the route rows and cleanup. **Not a pass**: it
  collects no class and is the Stage 0/Stage 3 probe artifact the contract names.
- `--mode full` = Pass 1 (working credential): success, continuation with the permission reply,
  interruption, usage readback, cleanup.
- `--mode provider-failure` = Pass 2, **no credential in the environment** (the collector refuses
  a set one): the relay answers its own 401 and the native provider-failure terminal is captured.
- `--mode truncation` = Pass 3, the truncation attempt: it resolves the live prompt schema from
  `GET /openapi.json`, and only a property bounding output length would start a bounded turn;
  with none, the class is `unavailable` with the resolved property list and document digest as
  the schema reason. No fault injection, no synthetic stream.
- Budget: at most 3 passes per platform, foreground, one receipt per pass; a re-run of a pass
  writes `-r2` and the superseded receipt stays. If Pass 1 leaves a class gap, Pass 3 is re-run
  as the spare and consumes its slot; otherwise Pass 3 is the truncation attempt. Never leave a
  pass pending.

Commands (from the repository root; `$KEY` is read at run time from the host's
`providers.tencent-litellm.apiKey` entry in `~/.omp/agent/models.yml`, never printed, never
written; `$BIN` is the extracted blob):

```text
TENCENT_GLM_API_KEY="$KEY" SYMMETRY_OPENCODE_BINARY="$BIN" python3 scripts/ci/opencode-v2-support-witness.py --mode probe --out <raw>/probe-<platform>.json
TENCENT_GLM_API_KEY="$KEY" SYMMETRY_OPENCODE_BINARY="$BIN" python3 scripts/ci/opencode-v2-support-witness.py --mode full --out <raw>/<platform>-2026-11-25.json
SYMMETRY_OPENCODE_BINARY="$BIN" python3 scripts/ci/opencode-v2-support-witness.py --mode provider-failure --out <raw>/<platform>-2026-11-25-provider-failure.json
TENCENT_GLM_API_KEY="$KEY" SYMMETRY_OPENCODE_BINARY="$BIN" python3 scripts/ci/opencode-v2-support-witness.py --mode truncation --out <raw>/<platform>-2026-11-25-truncation.json
```

Linux runs inside WSL as an ordinary process (`wsl.exe -d Ubuntu-24.04 -- bash -s`, the blob and
the checkout reached through the mounted checkout drive), with `TENCENT_GLM_API_KEY` carried by
`WSLENV` (never argv) and `GIT_DIR`/`GIT_WORK_TREE` set to the linked worktree's gitdir and root
(its `.git` file names a Windows path, so git cannot resolve the worktree otherwise) so the
collector's `source_head` read works. The ELF blob is copied to a WSL-native path outside the
repository, mode 0755, because the mounted-drive metadata cannot be relied on for the
executable bit; the receipt records the release asset, and the absolute path is rendered out
before commit.

Records: raw receipts stay in the scratch directory; each committed record is rendered with the
repository tool (Q-RENDER a) and the README records the render command and each raw receipt's
sha256:

```text
python3 scripts/ci/model-routing-record-sanitize.py --root-only < <raw>/<record>.json > <render>/<record>.json
python3 -c "import json,pathlib,sys; source=pathlib.Path(sys.argv[1]); target=pathlib.Path(sys.argv[2]); target.write_bytes(json.dumps(json.loads(source.read_text()), ensure_ascii=True, separators=(',',':')).encode() + b'\n')" <render>/<record>.json daemon/internal/harness/testdata/opencode/2.4.1/<record>.json
python3 -c "import json,sys; assert json.load(open(sys.argv[1])) == json.load(open(sys.argv[2]))" <render>/<record>.json daemon/internal/harness/testdata/opencode/2.4.1/<record>.json
```

The first command renders host paths (Q-RENDER a); the second writes the committed one-line
form over its output (see §5, record serialization); the third proves the two are the same
data. Each record's raw, pre-compaction and committed sha256 values are recorded in the 2.4.1
README.

The collector itself replaces the literal credential, relay base URL and relay host in memory
before writing (Q-URL b: every new record carries `<RELAY-BASE-URL>` / `<RELAY-HOST>`), and
records the replacement counts under `redaction.occurrences`.

Verdict derivation (the class matrix and the evidence record) reads the committed receipts only:

```text
python3 - <<'PY'
import json, pathlib
for path in sorted(pathlib.Path("daemon/internal/harness/testdata/opencode/2.4.1").glob("*.json")):
    receipt = json.loads(path.read_text())
    print(path.name, {name: value["status"] for name, value in receipt.get("classes", {}).items()},
          {name: value.get("blocker") for name, value in receipt.get("classes", {}).items() if value.get("blocker")})
PY
```

## 4. Route probe list (each pass records live status verbatim)

`GET /api/info` (authenticated), `GET /api/info` (unauthenticated → 401 row), `GET /api/health`,
`GET /api/model` (redacted in memory), `POST /api/session`,
`GET /api/session/{id}/event?after=0`, `GET /api/event` (status + content type),
`POST /api/session/{id}/prompt`, `GET /api/session/{id}`,
`GET /api/session/{id}/permission`,
`POST /api/session/{id}/permission/{requestID}/reply` (continuation),
`POST /api/session/{id}/interrupt?resume=false`, `DELETE /api/session/{id}` with a `GET` readback.
Each row keeps `method/path/status/body/credential_redacted_before_write`; the streaming row
records status and content type instead of a body, and the `/api/info` rows' bodies are
narrowed to `pid`/`version`/`status` on the 200 row, while the 401 row keeps the server's
`UnauthorizedError` body (PRE-2; narrowed to the 200 row by PR #288 round 1, §5). A 404 or a delta is evidence for Phase 1,
never a patch.

## 5. Implementation choices left open by the contract

- **POSIX cleanup accounting.** `close_native` on POSIX signals the process group
  (`start_new_session=True`) with a bounded SIGTERM grace; a group still holding a process
  after the grace is SIGKILLed once (`escalated`) and probed with `killpg(pgid, 0)` until ESRCH
  (`group_gone`; `group_survived` if a process outlives even the SIGKILL), so a surviving
  same-group descendant cannot be graded as cleanup. `exit_code = 0` records a completed arm
  and `1` a signal error; an already-reaped child leaves no group to signal (`group_gone`), and
  the Windows arm keeps `taskkill` unchanged. `classify()` keeps its four keys (`exit_code`,
  `server_exit_code`, `stream_reader_stopped`, `output_readers_stopped`) and adds the
  `group_gone` requirement where the POSIX arm records it, so a cleanup verdict cannot hide a
  live group member.
- **Probe mode is not a pass.** It produces the Stage 3 probe artifact and validates the
  platform launch/cleanup path before a receipt is spent; it collects no class and writes no
  receipt filename.
- **Recorded `/api/info` body (PRE-2).** The collector narrows the recorded `/api/info` 200 body to
  `pid`/`version`/`status` before any write, so the server's own host metadata never reaches a
  record; the eight receipts committed before the ruling were edited by a recorded byte-level
  substitution of the value with the constant token `<HOSTNAME>`, their committed sha256 values
  were re-derived, and the `classify()` verdicts were re-graded identical (evidence record §4,
  §12). PR #288 round 1 re-collected all eight receipts under the final collector, so the
  substitution now applies to the superseded bytes only and no committed record carries the
  hostname or a token for it.
- **Redaction split.** The collector refuses and replaces both the plain and the JSON-escaped
  spelling of every secret across the receipt bytes, the payload/event carriers, the
  configuration and the run-root scan, and removes the credential and the relay origin (Q-URL
  b); the repository renderer removes host absolute paths (Q-RENDER a), so `binary.path`,
  `workspace`, `discovered_from` and `command` are tokens in the committed record and literal
  only in the raw working copy.
- **Truncation resolution.** The prompt schema is resolved from the live document; a
  `BOUNDING_FIELDS` match would start one bounded turn and require a native output bound for
  `observed`, otherwise the class is `unavailable` with the property list and the document
  digest as the bounded reason.
- **Evidence gates over an uncommitted tree.** `jscpd-changed.py` and
  `check-record-cleanliness.py` read the `--head` revision from Git, so the gates run against a
  throwaway commit built with a temporary index (`GIT_INDEX_FILE` + `git write-tree` +
  `git commit-tree`) — no branch, ref, index or worktree state of the checkout is touched, and
  the working tree stays uncommitted for verification. The frozen-byte diff and the credential
  census use the same throwaway commit as their head.
- **Record serialization.** The received shape (the 2.3.0 directory) is pretty-printed JSON;
  the changed-file clone gate's line corpus then reads the structural blocks every receipt
  shares with its siblings as new duplication, which its own rules refuse. The committed
  records are therefore one-line JSON (a single object per line, the JSONL shape the repository
  sanitizer already parses): the gate notices them as below-threshold instead of scanning them,
  and the renderer's output is preserved value-for-value, proved by a parsed-equality row per
  record. Rejected alternative: a leaner receipt schema written by the collector — it would
  have required re-collecting every pass (the collector's bytes are the receipt's binding) and
  would still have left shared blocks inside the event streams.
- **Out of scope.** The Phase 1 promotion amendment; every frozen path; `docs/TODO.md` and
  CLOSED-TODO moves; the V1 1.18.32 acceptance-pin drift; the D3 amendment.

## 6. Phase 1 expectation (named, not edited)

If the evidence supports promotion, the amendment edits `docs/design/harness.md` (the OpenCode V2
row and its 2.3.0 citation), the `docs/goals/README.md` OpenCode rows, `0010` D1/W6-04,
`daemon/internal/harness/opencode/adapter.go` (`TestedVersion` and the version gate),
`daemon/internal/harness/registry.go` (the `UnavailableAdapter` wiring), the six named CI
assertions and the acceptance/witness legs in `scripts/ci/linux-v2-native-acceptance.sh` and
`scripts/ci/linux_owner_witness_linux.go`, plus a Linux run with the owner containment receipt
that WSL native execution is not. Phase 0 edits none of them.
