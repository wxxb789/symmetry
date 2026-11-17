# 0007i single real-time Portal cutover — 0007 series release receipt

Result: **produced, awaiting the owner's verdict.** The model does not rate its
own rendering, sign for the owner, or treat owner acceptance as a condition this
run can satisfy. The owner's review of the integrated deployed experience
(list/board/detail/chat/attention/connections/runtimes on real work at this
revision) is recorded in this receipt when it arrives, together with any
separately authorized `achieve`.

This receipt was rebased with its branch onto `main` `ca29a8e` (goal 0007g) on
2026-11-17. The Subject below binds the rebased revision; the pre-rebase
revision's own binding and runs are kept as the historical record and are
labelled as such. The rebase's resolutions, the supersession of the cutover's
own conversation surface, and the evidence re-established on the rebased
revision are in "Rebase onto `main` `ca29a8e`".

## Subject

| Item | Value |
| --- | --- |
| Repository | `Q:/repos/wt-0007i/symmetry` |
| Branch | `feat/0007i-portal-cutover` |
| Base revision | `ca29a8e8150f84a02b5bd88232e65452c09114ea` (`main`, goal 0007g: scoped engineering conversation) |
| Delivery commit | `c2eb529d743feb53a910abff42bc96b62e2a197b` (the lockfile repair) on top of `cf00b1ded9e2ac1a9fbc4a49d0dbbfeaa12be5c8` (the rebased delivery) on top of `ca29a8e`; every run on "this revision" ran on the source paths that commit carries. `git diff c2eb529..HEAD -- ':!docs'` is empty — the commit that carries this file is documentation-only and changes no executed byte — and `node .tmp/0007i-subject-digest.mjs ca29a8e` reprints the digest below on the committed tree |
| Changed paths | **58** changed paths outside `docs/**` — `git diff --name-only ca29a8e` lists 63 paths, five of them under `docs/`; 54 present, 4 deleted |
| Changed-path manifest digest | `sha256:09b79cdd37896463e0b5673452d33105abd752f4ac6a17a81bab6961600a53c3` — reproduction `node .tmp/0007i-subject-digest.mjs ca29a8e` prints `paths=58 present=54 deleted=4 crlf_folded=0`. The path set did **not** move with the lockfile repair (`pnpm-lock.yaml` and `frontend/package.json` were both already delivered paths); the digest changed because `pnpm-lock.yaml`'s bytes did (see "CI repair" below). The earlier value for the same path set before that repair was `sha256:00f8a4a6c9044095aa2069c05b4995906ed852e5353234f7a0c37b9e5e7a945d` The construction is unchanged from the 0007 series procedure (each path hashed, deletions listed as `deleted  <path>`, lines sorted in ordinal byte order, UTF-8 manifest plus trailing newline); editing this receipt or any other `docs/**` file cannot change the value |
| Served SPA document | `control/priv/static/portal/index.html` `sha256:b74ada21f606002ba24ede7736d45d06cc751acdba735211019b08895bd0daca`, assets `index-_tTQ2W1j.css` `sha256:7550bd3984d6e73a9618f11d2c58f0e8c4b7cc595b68d3af4dc41e98c0b6051e` and `index-DzOatb2U.js` `sha256:02973e5dd8e9c31e0cc5979d7b5c64b07cad5389ecc6c3a24a489839dfa9c2f0` (the bundle this revision builds; it differs from the pre-rebase bundle because 0007g's chat surface is part of the frontend now) |
| Contract bundle | `contracts/v1` 21 schemas / 240 fixtures; `daemon/internal/contracts/schema_bundle.json` regenerated and byte-identical to `main`'s |

### Subject of the pre-rebase delivery (kept as the historical record)

The first version of this receipt bound the branch's own working tree on base
`671525c7`. Those rows are kept because the runs recorded under "Verification on
this revision" and "Repair round" were produced on it; they are **not** evidence
for the rebased revision, and every row that names them now says so.

| Item | Value |
| --- | --- |
| Base revision | `671525c7249b3234b64e4a83b2f4a82fbc663b75` |
| Working tree | uncommitted by instruction (no commit, no push); **81** changed paths outside `docs/**` — 58 tracked files including the four removals named in "Removal and migration" (`+4483 −6371` from `git diff --numstat 671525c -- ':!docs'`) plus 23 new files, enumerated as `git diff --name-only 671525c` + `git ls-files --others --exclude-standard` minus `docs/**`. The default `git status` on this host prints 79 lines for the same set: it collapses two new directories (`control/lib/symmetry_control/portal/`, `frontend/src/features/chat/`) into one line each, and its cached untracked state omits `browser/tests/portal-cutover.spec.mjs` — the file is not ignored (`git check-ignore` finds no rule), and `git status -uall`, `git ls-files --others` and the digest below all list it. The path set of that delivery is the 81, not the 79 status lines |
| Changed-path manifest digest | `sha256:6f1bc7843fb30260153144cfdf5e5dae5046bcaeb1450153e999f4ed66552b21` — the 81 changed paths outside `docs/**`, each hashed and listed as `<sha256>  <path>` (a path deleted there is listed as `deleted  <path>`; 77 hashed, 4 `deleted`), sorted in ordinal byte order and SHA-256'd as a UTF-8 manifest with a trailing newline. Editing a `docs/**` file cannot change the value |
| Served SPA document | `control/priv/static/portal/index.html` `sha256:87793fb2a6395b54381799181e8f6938d7b64d5cf39968a9c9432ca72141ab62`, assets `index-DirRBnjk.css` `sha256:0124e0ecb61abfb580ef3567259ec906e232cb3ee93f73b523029a7dd57f1634` and `index-Bkcxy5j-.js` `sha256:19b1f6925f23f54e582da47b0d63208bd50415026b2dd59267c867b110127bdc`; the compiled copies the instances serve carry the same digests, and the three running gate instances (4030/4031/4032) were fetched on this host and answered `index-Bkcxy5j-.js` byte-for-byte |
| Contract bundle | `contracts/v1` 21 schemas / 239 fixtures (`chat-conversation`, the family the rebase removed as superseded) |

Evidence binds this revision. Older evidence from 0007a–0007h is reused only
where this receipt says so and only for code this revision did not touch; every
reused item names the revision or gate it came from.

The receipt file itself was written after the runs it records. The
`docs/goals/README.md` closure paragraph (written 10:12) and the
`docs/TODO.md` deferral list (written 09:06) were written *between* gate runs,
not after all of them: the control-suite run at 11:16, the browser gate at
12:00, the chat harness at 12:03 and the connection harness at 12:10 all
followed both edits. All three files are documentation-only, sit under
`docs/**` and therefore outside the manifest digest, and change no executed
byte. The gate runs recorded below are named with the revision (or the log) that
produced each number; several gates ran more than once while this revision was
repaired, and "Repeats and one failed attempt" below states every repeat instead
of claiming each gate ran once.

## Rebase onto `main` `ca29a8e` (goal 0007g delivered)

The branch was rebased from `671525c7` onto `ca29a8e`, which carries goal 0007g
(scoped, recoverable engineering conversation). 0007g is the delivery this
rebase had to preserve intact, and the cutover's own conversation surface is the
one thing it supersedes: both branches had implemented `/portal/chat`, and
keeping both would have shipped two implementations of one surface and two wire
families for one endpoint. The resolution is therefore **0007g's chat surface,
its `chat-read` contract family and its daemon envelope registration on every
path where the two deliveries overlap**, with this cutover's removals, real-time
delivery, legacy hash repair and contract-registration hygiene applied on top.

### Conflict resolutions (20 paths)

`git rebase ca29a8e` reported 20 conflicted paths. Each is resolved as follows;
"main's" means the byte-identical `ca29a8e` version and "this cutover's" means
the version the pre-rebase revision carried.

| Path | Resolution |
| --- | --- |
| `frontend/src/features/chat/chat.tsx`, `presentation.ts`, `chat.test.tsx`, `presentation.test.ts` (add/add) | **main's 0007g files**, including the scroll-offset re-assertion 0007g's receipt proves. This cutover's four files (its own implementation of the same surface) are deleted as superseded |
| `browser/tests/portal-chat.spec.mjs`, `portal-chat-live.spec.mjs` | **main's 0007g specs** — they drive `/portal/chat` against the surviving implementation and carry 0007g's interception and lifetime details. This cutover's rewrites of the same files covered its own implementation |
| `contracts/scripts/check.mjs`, `check-effect.mjs`, `contracts/ts/index.ts` | main's `chat-read` entries kept; this cutover's `chat-conversation` entries dropped with the schema they named |
| `contracts/generated/ts/index.ts`, `contracts/generated/effect/index.ts`, `daemon/internal/contracts/generated.go`, `daemon/internal/contracts/schema_bundle.json` | regenerated with `pnpm contracts:generate` after the schema set was settled; all four are byte-identical to main's (`git diff ca29a8e -- contracts/generated daemon/internal/contracts` prints nothing) |
| `control/lib/symmetry_control_web/portal_session.ex` | this cutover's version: the classic destination is removed and `/portal/chat` plus the legacy hash carrier are accepted. main's edit (adding `chat` to `@shell_path` and the same `scope`/`projectId`/`runId` chat allowlist) is **subsumed, not dropped** — this version carries the same allowlist |
| `control/lib/symmetry_control_web/router.ex` | this cutover's version (one `/portal/chat` document route, the invalidation plugs, no `/classic`). The textual merge had left main's `get "/chat", PortalController, :index` line *and* this cutover's, producing two identical document routes; the duplicate was removed |
| `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs` | main's file: the `chat-read` validator entry stays, and the `@presentation_reads` entry the auto-merge added for `chat-conversation` is removed with the schema. Byte-identical to main's afterwards |
| `control/test/symmetry_control_web/portal_session_test.exs` | this cutover's version (the bootstrap serves `socket_token`, `/portal/classic` is no longer a login destination, the legacy carrier is), with the duplicated chat/legacy list entries the auto-merge produced removed |
| `control/test/symmetry_control_web/controllers/chat_controller_test.exs` (clean merge) | this cutover's served-document check kept, but it now validates the served read against `chat-read` — the schema the surviving implementation decodes — instead of the removed family |
| `frontend/src/services/api.ts` | main's chat API (`chatConversation`, `chatMessage`, the status-carrying `decode`) plus this cutover's `socket_token` member on the session bootstrap |
| `frontend/src/app/application.ts` | auto-merged with both sides intact: this cutover's realtime handle, in-memory socket token and connect path, main's chat query options and chat message mutation |
| `frontend/src/app/shell.tsx` | main's `useProjectScope` and `ProjectPicker` (0007g's chat owns its scope selector in-surface, so the picker's chat branch goes with the superseded surface) plus this cutover's realtime connect effect and its removal of the classic footer link |
| `frontend/src/app/router.tsx` | main's route table and its chat search validator; semantically identical to this cutover's (same path, same `scope`/`projectId`/`runId` allowlist) |
| `frontend/src/app/router.test.tsx` | auto-merged: main's chat route tests plus this cutover's session fixtures, which now serve `socket_token` |
| `frontend/src/styles.css` | main's chat block kept; the auto-merged file also carried this cutover's chat block for the superseded surface — removed — while its two non-chat changes stay (`.page-heading { flex-wrap: wrap }` for the 390 px overflow this cutover fixed, and the workspace-wide `prefers-reduced-motion` rule) |
| `.github/workflows/ci.yml` | main's refactored workflow with this cutover's two changes re-applied at their new locations: `PHX_HOST=127.0.0.1` in the browser-gate step (the socket origin check is pointed at the host the gate browses, never disabled) and `tests/portal-cutover.spec.mjs` added to the focused spec list beside main's `tests/portal-chat.spec.mjs` |
| `browser/README.md` | main's 0007g sections (scoped-conversation acceptance, the rewritten live-daemon paragraphs) plus this cutover's removal/migration lists; the paragraph that described the classic chat walk is rewritten to name 0007g as the owner of the surviving chat suite |
| `docs/TODO.md` | main's blob with only this delivery's own section appended (`+46 −0`); the no-main-line-dropped check is in the run table |
| `docs/goals/README.md` | main's 0008b/0008c closures kept; this goal's closure paragraph corrected — 0007g is delivered, and the cutover's own chat surface and `chat-conversation` family are named as superseded |

### Removed as superseded (with the reason, not as a merge artefact)

`contracts/v1/chat-conversation.schema.json`, its five fixtures
(`valid/chat-conversation.project.json`, four invalid), the generated
`contracts/generated/ts/chat-conversation.ts`, the five fixture-manifest entries
and the Elixir `@presentation_reads` entry are deleted. The endpoint
`/portal/api/chat` has exactly one served shape in this revision, and that shape
is `chat-read`: two families would have made 22 v1 schemas against the
repository's own `21` assertion, and the frontend that decodes the endpoint is
0007g's, which reads `chat-read`. Nothing else in the cutover's delivery is
second-guessed by the rebase: the realtime bridge, the invalidation publishers,
the socket, the legacy hash repair, the SPA document and the classic removals
are unchanged from the pre-rebase revision.

`frontend/src/services/realtime.ts`'s chat key map needed no change for the
surviving surface: it invalidates `["chat"]`, which prefix-matches 0007g's
`["chat", { scope, projectId, runId }]` reads, and 0007g's older pages live in
the surface's own retained state rather than under their loaded cursor keys.

### A defect the rebase removed, demonstrated rather than asserted

The pre-rebase revision shipped a schema and a `valid: true` fixture for an
envelope the daemon never registered. Reproduced on `7235708` (materialized
outside the delivery with `git archive 7235708 | tar -x -C .tmp/prebase`):

```
cd .tmp/prebase/daemon && go test -count=1 -run TestGoalTransportParsesContractFixtures ./internal/protocol/
--- FAIL: TestGoalTransportParsesContractFixtures (0.79s)
    --- FAIL: TestGoalTransportParsesContractFixtures/chat-conversation.project (0.00s)
        goal_contract_fixture_test.go:51: schema validity = false, want true: unsupported Goal envelope schema "chat-conversation"
FAIL
```

Two facts explain why that revision's own gates did not show it: its
`daemon/internal/contracts/schema_test.go:18` pinned the compiled-schema count by
hand (`if len(schemas) != 20`) while its embedded bundle declared 21 schemas, so
the count check could not see a schema that no envelope registered; and the
daemon fixture test was not in that revision's gate list. `main` had already
replaced that hand-maintained count with one derived from the embedded bundle
(`declaredSchemaCount`), and the rebased revision registers the surviving family
(`EnvelopeChatRead`) through the same three places 0007g used. **No
hand-maintained envelope list or count was reintroduced by this rebase**, and the
daemon fixture test is exit 0 on this revision (run table below).

### Documentation repairs this rebase made visible

All documentation-only, and all recorded because the cutover's own claim
("the classic presentation is gone") was contradicted by them:

- `control/README.md` still described `/portal/classic` as a live entrypoint and
  the Vite proxy as serving `portal.js`/`portal.css`. It was stale at
  `7235708` as well; corrected here to the single SPA document, the removed
  classic route and the login stylesheet the proxy actually serves.
- `contracts/README.md`'s chat section now describes `chat-read` (0007g's
  family) instead of the removed `chat-conversation`, with the same capture rule.
- `browser/README.md` and `docs/goals/README.md` as described above.
- `docs/plans/2026-11-17-0007i-portal-cutover-plan.md` gains §15: the rebase,
  its resolution and why the cutover's chat work is superseded.
- `docs/TODO.md` gains this delivery's own deferred-findings section, carrying
  the pre-rebase revision's two intermittents with the revision they were
  recorded on, and dropping the `chat-conversation` coverage entry the surviving
  family closes (its fixtures include a run-scoped waiting-decision capture).

### CI repair: the lockfile's tarball-URL provenance (run `44050937524`)

Run `44050937524` failed the `contracts` and `compose` jobs at
`pnpm install --frozen-lockfile` with `ERR_PNPM_TARBALL_URL_MISMATCH` — `96
lockfile entries failed verification` — while `main` (`ca29a8e`) passes both jobs.

Cause, measured rather than inferred: the pre-rebase delivery regenerated
`pnpm-lock.yaml` on this host, whose user-level `~/.npmrc` sets
`registry=https://packagefeedproxy.microsoft.io/npm/`, and that feed's metadata
records tarballs at
`https://ms-feed-25.pkgs.visualstudio.com/1es-public/_packaging/npm-public/npm/registry/…`.
The branch's lockfile carried **96** such URLs (`ca29a8e`: 0, `7235708`: 96), the
CI policy admits the canonical registry host, and every rewritten entry
therefore failed verification. The delivery's *intended* dependency change is one
line — `frontend/package.json` adds `"phoenix": "1.8.16"`, the socket client
`frontend/src/services/realtime.ts:2` imports — and it does require lockfile
movement, so restoring `main`'s lockfile alone would fail a frozen install as an
outdated lockfile.

Fix: `main`'s `pnpm-lock.yaml` byte-for-byte plus exactly the entries a pinned
`12.17.0` install adds for that dependency — the importer
`phoenix: {specifier: 1.8.16, version: 1.8.16}`, the `packages` entry with
`integrity: sha512-bNNJkYBn…` and the canonical tarball
`https://registry.npmjs.org/phoenix/-/phoenix-1.8.16.tgz`, and the `snapshots`
entry `phoenix@1.8.16: {}` (no transitive entries: the package has no
dependencies). Diff against `main`'s lockfile: **8 added lines, 0 removed, no
other entry touched, no reordering**, and `grep -c ms-feed pnpm-lock.yaml` = 0.

One deviation is named rather than silent: `registry.npmjs.org` is **not
reachable from this host** — `pnpm install --lockfile-only
--registry=https://registry.npmjs.org/` fails with `received fatal alert:
HandshakeFailure` fetching `phoenix` metadata — so the canonical URL was not
re-resolved from npm here. It is npm's fixed pattern for this package
(`https://registry.npmjs.org/<name>/-/<file>.tgz`), which the 316 pre-existing
entries of `main`'s lockfile show for every other package, and the recorded
integrity is the registry's own `dist.integrity` for `phoenix@1.8.16`, which the
local install already verified (a mirror serves npm's bytes). The arbiter for the
URL form is CI's own policy check, which is why the proof below is the exact CI
command with the pinned version.

Proof, measured on this host:

1. **Structural** — every one of the lockfile's **317** `tarball:` URLs is on
   `https://registry.npmjs.org` and matches that registry's canonical
   `<name>/-/<file>.tgz` pattern (`ms-feed` and `visualstudio` occurrences: 0),
   which is the property the policy verifies.
2. **Freeze** — a `pnpm install --frozen-lockfile` attempt left `pnpm-lock.yaml`
   byte-identical (`sha256:70a58a1aee1e498054e74c10aa51d92f96d31e4df2eff6490b431af05ff5ff60`
   before and after) — the property the steer's `--lockfile-only` fallback names.
3. **The exact CI command cannot complete on this host after the repair, and the
   reason is egress, not the lockfile**: pnpm now resolves the lockfile's own
   canonical URLs, so its fresh supply-chain verification fetches
   `https://registry.npmjs.org` metadata and fails with
   `ERR_PNPM_RESOLVING_NPM_RESOLVER_NETWORK_ERROR … received fatal alert:
   HandshakeFailure` (`@exodus/bytes`), the same TLS failure a direct
   `--registry=https://registry.npmjs.org/` resolve hits. Before the repair the
   same command returned in 110 ms because the verdict was **cached** for the old
   lockfile hash — which is how this divergence reached CI in the first place.
   `main`'s own lockfile is canonical like this one and passes that check in CI,
   so the local substitute was to run the checks unwrapped (run table).

### Which revision carries each series clause's evidence

This goal's receipt is also the 0007 series release receipt, so the revision a
clause's evidence lives on is stated rather than assumed:

| Series clause | Evidence revision |
| --- | --- |
| The cutover's own clauses: real-time invalidation and reconnect catch-up, polling fallback, the one-time legacy hash repair, removal of the classic presentation after parity, one writable UI state owner | **partly this rebased revision** (the unit, contract, Elixir, Go and fence runs in the table below; the browser cases were not re-run here, so the rendered evidence for these clauses is the pre-rebase capture set — the runtime code these clauses cover is unchanged by the rebase except for the chat surface, which is 0007g's) |
| Conversation flows on real APIs (scope, drafts, retries, receipts, decisions) | **`ca29a8e` (goal 0007g)** — its own receipt, binding bytes this revision carries unchanged (`git diff ca29a8e -- frontend/src/features/chat browser/tests/portal-chat*.spec.mjs` prints nothing); the browser suite that carries its rendered evidence was **not re-run** on this revision |
| 0007a–0007f clauses (readable work views, safe planning, connections on the fixtures-only boundary, resource binding and sync, runtime control) and 0007h (goal/attention/decision workspace) | their own merge revisions (`7a92ca4`, `5c2e0ad`, `e6e4b03`, `ad1c117`, `dd2e9db`, `671525c`, all reachable from `ca29a8e`) — reused only where this revision did not touch the code they cover, and re-executed here where this cutover did (the connection, work, goal and execution-control specs) |
| The full-series acceptance contract's structural clauses (one owner per state category, the selected stack, real Goal APIs, no production Node server, no second policy engine, no production mock data, no workflow canvas/mobile app/visual self-certification) | **this rebased revision** — a structural property of the delivered tree, verified by reading the delivered paths and by the inventory of the served document set |
| Classic-presentation removal ordering ("only after parity") for the compose surface | **unmet, unchanged by the rebase** — the compose suites still have no run on any revision here; the pre-rebase record and the reason (container egress) stand |
| Owner acceptance | not a series clause this run can satisfy; recorded when the owner's verdict arrives |

### Re-established evidence on this revision (one pass per suite)

These runs are on the **lockfile-repair revision**: the repair changed only `pnpm-lock.yaml` (the contract, frontend and audit inputs are untouched), and the four contract commands and four frontend commands were re-run after it. Their pnpm-wrapped forms could not run on this host after the repair because pnpm's fresh lockfile verification needs `registry.npmjs.org`, which this host cannot reach — so they were run **unwrapped**: the same commands, binaries and versions `pnpm contracts:check` and `pnpm --filter @symmetry/frontend <script>` invoke, and CI still runs the pnpm forms.

**The browser gate is not in this table** — it was not run on this revision; "Not measured on this revision" below says why and what that leaves open.

Every command below ran on the committed source paths of `cf00b1d`; no suite was
re-run to turn a red gate green. Where a row says its process status was read
through a pipe, the printed summary quoted in that row is the evidence and the
row says so; the four frontend commands were additionally run once each with a
direct status capture (both observations agree, and both are stated). Nothing
below was re-run to make a gate green, and no row hides a red run. `pnpm` is `pnpm@12.17.0`, Elixir/Erlang are
`1.20.4-otp-29`/`29.1.2`, Go is `1.27.4`, node is `26.13.1`.

| Gate | Command (working directory) | Result |
| --- | --- | --- |
| Contract gate + generated-artifact check | `pnpm contracts:check` (root) | output: `Generated contract DTOs are up to date.`, `Validated 21 Draft 7 schemas and 240 fixtures.`, `Effect decoded all 240 raw fixtures; 236 parsed documents reached Schema validation`, node test summary `tests 33 / pass 33 / fail 0`. The status was read through a pipe; the printed summaries are the evidence |
| Contract regeneration | `pnpm contracts:generate` (root) | exit 0 — `Generated contract DTOs.`; the four generated artefacts (`contracts/generated/ts/**`, `contracts/generated/effect/index.ts`, `daemon/internal/contracts/generated.go`, `daemon/internal/contracts/schema_bundle.json`) are byte-identical to `main`'s afterwards |
| Frontend typecheck | `pnpm --filter @symmetry/frontend typecheck` (root) | **exit 0** (direct capture; second run of this suite on this revision — the first was green too) |
| Frontend lint | `pnpm --filter @symmetry/frontend lint` (root) | **exit 0** (direct capture; `--max-warnings 0`) |
| Frontend unit/router | `pnpm --filter @symmetry/frontend test` (root) | **exit 0** (direct capture) — 18 files, **312 passed** (0007g's chat suites are part of this count; the pre-rebase revision had 281–282) |
| Frontend build + staged assets | `pnpm --filter @symmetry/frontend build` (root) | **exit 0** (direct capture) — rebuilds the identical bundle `index-DzOatb2U.js` (877 453 bytes, `sha256:02973e5d…`) and restages `control/priv/static/portal` |
| Control format (test env) | `MIX_ENV=test mix format --check-formatted` (control) | exit 0 — no file reported |
| Control compile (test env) | `MIX_ENV=test mix compile --warnings-as-errors` (control) | exit 0 — `Compiling 6 files (.ex)` / `Generated symmetry_control app` |
| Focused Control tests (this cutover's files + 0007g's chat suite) | `POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55463 POSTGRES_DB=symmetry_control_test mix test test/symmetry_control_web/portal_session_test.exs test/symmetry_control_web/portal_html_test.exs test/symmetry_control_web/portal_realtime_test.exs test/symmetry_control_web/controllers/portal_controller_test.exs test/symmetry_control_web/controllers/chat_controller_test.exs test/symmetry_control/goals/contract_fixture_manifest_test.exs test/symmetry_control/workspaces_read_model_test.exs test/symmetry_control/chat_test.exs` (control) | `Result: 60 passed` in 15.0 s — includes the served-document check against `chat-read`, the realtime channel/token, the SPA document, the login template and 0007g's chat suite. The status was read through a pipe; the ExUnit summary is the evidence |
| Full Control suite (second invocation; the first was stopped while still progressing and produced no summary) | `POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55463 POSTGRES_DB=symmetry_control_test mix test` (control; MIX_ENV=test, log `.tmp/gates/rebase-control-full2.log`) | **exit 2** (direct capture) — `897/898 passed, 21 skipped`, 621.9 s, **one failure: `test/symmetry_control/integrations/command_test.exs:18` `provider token commands can keep stderr diagnostics out of stdout` → `{:error, {:command_unavailable, "pwsh"}}`**, the pre-existing bare-`pwsh` host limitation `docs/TODO.md` already names. The load-sensitive `GoalsTest` checkout timeout the pre-rebase record carries did **not** reproduce. Totals moved 886→898 because 0007g's chat suite is part of this revision |
| Contract gate, unwrapped (the four commands `pnpm contracts:check` runs) | `node_modules/.bin/tsc --project contracts/tsconfig.json`; `node --test contracts/scripts/*.test.mjs`; `node contracts/scripts/check.mjs`; `node contracts/scripts/check-effect.mjs` (root) | **exit 0, 0, 0, 0** (direct capture) — `ℹ pass 33 / fail 0`; `Generated contract DTOs are up to date.`; `Validated 21 Draft 7 schemas and 240 fixtures.`; `Effect decoded all 240 raw fixtures; 236 parsed documents reached Schema validation` |
| Frontend typecheck / lint / test / build, unwrapped | `frontend/node_modules/.bin/tsc --noEmit`; `eslint . --max-warnings 0`; `vitest run`; `vite build` + `node scripts/stage-assets.mjs` (frontend) | **exit 0, 0, 0, 0** and stage 0 (direct capture) — 18 files, **312 passed**; the rebuilt bundle restages to the same `index-DzOatb2U.js` (`sha256:02973e5d…`) |
| Go build / vet | `go build ./...`, `go vet ./...` (daemon) | exit 0 — no output on either |
| Go contract fixtures (the daemon's envelope registry) | `go test -count=1 -run TestGoalTransportParsesContractFixtures ./...` (daemon) | `ok github.com/wxxb789/symmetry/daemon/internal/protocol 18.859s`, every other package `no tests to run`; the status was read through a pipe, which is why the failing pre-rebase run above is quoted by its own output rather than by a status |
| Go contracts package | `go test -count=1 ./internal/contracts/...` (daemon) | `ok github.com/wxxb789/symmetry/daemon/internal/contracts 21.504s` (status read through a pipe; `go test` prints `ok` only on success) |
| Legacy-route audit fence (the CI job's own command) | `scripts/audit-no-legacy-routes.sh` (root, run with Git Bash) | **exit 0** (direct capture) — `No U2 legacy action endpoints found in production contract surfaces: 1298 tracked files audited, 4 intentional exclusions` |
| Legacy-route audit's own suite (the same job's second step) | `python scripts/ci/test-no-legacy-routes-audit.py` (root, under Git Bash) | **exit 0** (direct capture) — `Ran 6 tests in 18.132s` / `OK` |
| Legacy-route audit fence under this session's nested bash | `bash scripts/audit-no-legacy-routes.sh` (root) | exit 2 — the script's own fail-closed check: `pwd -P` resolves the worktree to `/mnt/q/...`, which the Windows `git` cannot resolve, so `git ls-files` fails and the fence refuses to audit a tree it cannot enumerate. A harness path-mapping limit, not a fence or tree defect; the row above is the same command under a real Git Bash |
| No main line dropped | `git diff origin/main..HEAD -- docs/TODO.md | rg '^-' | rg -v '^---'` (root) | prints nothing (`docs/TODO.md` is main's blob plus this delivery's own section, `+46 −0`) |
| Subject digest (before and after the lockfile repair) | `node .tmp/0007i-subject-digest.mjs ca29a8e` (root) | `paths=58 present=54 deleted=4 crlf_folded=0` both times; digest `sha256:00f8a4a6…` before and `sha256:09b79cdd37896463e0b5673452d33105abd752f4ac6a17a81bab6961600a53c3` after the `pnpm-lock.yaml` repair — the path set did not move |

### Not measured on this revision

- **The browser gate (both Chromium projects, all specs)**: not run. Its
  environment is three sequentially started `mix phx.server` instances on the
  production configuration plus the staged bundle the Subject names, and the
  run budget for this session ended before that gate could be started; nothing
  in this receipt reports it as passing or failing on this revision. What it
  would have re-run is named per clause in "Which revision carries each series
  clause's evidence" above, and the pre-rebase capture set stays labelled with
  its own revision.
- **Nothing else from the Elixir side**: the full Control suite did complete on
  this revision (run table: exit 2, 897/898, the single `pwsh` host limitation),
  so its verdict is claimed with its one failure named rather than waived.
- **The compose harness** (`run-compose-daemon-tests.ps1` and the two compose
  specs): unchanged from the pre-rebase delivery, still unexecuted here; its
  parity claim remains unmet rather than passed.
- **The planning, live-daemon and chat-live harnesses**: not re-run on this
  revision. Their code paths are this cutover's plus 0007's, and the rebase
  changed none of the four frontend surfaces they drive except through the
  shared realtime bridge — but "not re-run" is what this receipt says.
- **Hosted CI**: this branch has no remote yet, so no CI run exists for any
  revision of it; the workflow this revision changes is validated by reading and
  by the local runs below, not by a hosted run.
- **The packaged release binary**: still cannot boot on this Windows host (the
  `erts-*/bin/erl` shim calls `erlexec` without the `.exe` name); the browser
  gate runs the production configuration through `mix phx.server`.
- **The owner's rendered review**: out of scope and not claimed.

## Owner authorization recorded (fixed-design amendment)

The root `AGENTS.md` forbids silent changes to fixed design decisions, so the
amendment the owner directed is recorded here and in the PR body: **legacy is not
preserved and no backward compatibility is required.** That authorizes, in this
same PR, the removal of the classic routes, the legacy `portal.js`/CSS
presentation, the EEx workspace renderer, duplicate legacy presentation and their
classic acceptance specs.

Two limits were kept:

1. **Ordering — kept for every surface whose migrated suite executed, unmet for
   the compose surface.** No legacy surface was deleted before its migrated
   suites demonstrated parity on this revision *except* the compose surface:
   this revision replaces the classic halves of `portal-compose-daemon.spec.mjs`
   and `portal-compose-restart.spec.mjs` with SPA-half cases that have never
   run, because their harness (`run-compose-daemon-tests.ps1`) never executed a
   spec here — its own `docker compose up --build` dies in the image build
   because this host's containers cannot reach the npm registry (the exact
   error is in the gate table below). The compose surface therefore has **no
   parity run on this revision**; its rows in "Removal and migration" are
   supported by inspection of the migrated assertions only, and this receipt
   does not claim otherwise anywhere. Every
   other removed surface's migrated suite executed on this revision (gate table
   and "Repeats" section). Parity is a correctness gate, not backward
   compatibility; the goal's own out-of-bounds clause keeps it. The parity
   evidence for the executed surfaces is the "removed presentation" and
   "migrated suites" columns below.
   The chat surface is the one removal whose ordering the rebase re-attributes:
   on the rebased revision it is 0007g's delivered conversation surface (its
   suite ran on `main` and is re-run here) rather than the cutover's own
   migration, which the rebase removed as superseded.
2. **Fences.** `scripts/audit-no-legacy-routes.sh` and the job that runs it, the
   protocol/wire contracts, the fixtures-only provider boundary (0007d/0007e),
   credential locality and every fail-closed gate are untouched and still run
   (audit row in the gate table). The one-time legacy `/portal#...` hash redirect
   is kept: it is a deep-link repair, not legacy presentation. No second
   writable UI state owner exists — the only remaining server-rendered document
   is the login template with one stylesheet and no script.

## Repair round after the independent audit (2026-11-17, pre-rebase revision `7235708`)

An independent audit of the first version of this receipt found ten defects,
read-only against this tree: a dropped control-suite failure (P1-1), an ordering
claim that did not hold for the compose surface (P1-2), a mobile connection case
mislabelled as a locator artifact (P1-3), two over-claiming validity cells
(P2-1), an unbound Subject and a wrong mtime sentence (P2-2), an implicit
protocol deviation (P2-3), an invalidation case that could not distinguish a
push from the polling fallback (P3-1), an attention-read invalidation gap
(P3-2), a capture-count error (P3-3) and a "ran once" claim that was not literal
(P3-4). This section is the repair, in the working tree only (no commit, no
push, no PR). The repaired tree is bound by the manifest digest in "Subject"
(`sha256:6f1bc784…`, 81 paths); the rows above that name a log or a revision are
the pre-repair record and are left standing as such.

### Subject digest construction

The 0007 series' manifest-and-digest procedure applied to this revision's
changed path set: `git diff --name-only 671525c` (deletions included) plus
`git ls-files --others --exclude-standard`, minus every path under `docs/**`;
each path hashed and listed as `<lowercase sha256>  <path>`, or
`deleted  <path>` for a path this revision removed; the lines sorted by the
path's ordinal (byte) order; the UTF-8 manifest plus its trailing newline
SHA-256'd. Reproduction: `node .tmp/0007i-subject-digest.mjs` prints
`paths=81 present=77 deleted=4 crlf_folded=6` and the digest. One deviation from
the earlier receipts' exact procedure is named rather than silent: content is
hashed with CRLF folded to LF, because six in-scope paths hold CRLF in this
working tree under `.gitattributes` `* text=auto` and LF in their committed
blobs, and a binding that changed with the checkout's line endings would not be
reproducible. The counted path set is 81, not the 79 lines the default
`git status` prints (see the Subject table).

### P1-1 — the dropped control-suite failure, and a third revision-valid run

The receipt's first version reported the control suite as an earlier run with
one `pwsh` failure. The audit read `.tmp/gates/control-test-final.log` and found
the final-bytes run at 11:16 was **884/886, 21 skipped, exit 2, two failures**,
the second being `GoalsTest` "control recovery selects pending actions after
more than one page of terminal no-op Goals"
(`control/test/symmetry_control/goals_test.exs:4753`) with a
`DBConnection.ConnectionError` sandbox-checkout timeout. Both runs are now named
in the gate table with their revision and both failures; the `GoalsTest` failure
is an open entry in `docs/TODO.md` with its coordinates, the log line range and
the reason it is not the already-recorded `integrations_test.exs:1161` flake
(different test, different call site). It is not waived: the post-repair run is
the current state and is recorded under "Repair-round verification" below — run
3 of 3 is `885/886 passed, 21 skipped`, exit 2, with the `pwsh` host limitation
as its only failure, so the `GoalsTest` timeout did not reproduce on a dedicated
database; that is a repeat datum, not a fix, and the entry stays open in
`docs/TODO.md` with the not-reproduced observation added. What could not be
shown: that the `GoalsTest` case is the same flake as the recorded integration
one — it is not, by test, file and call site.

### P1-3 — the mobile connections notice loss was a real regression

The audit's finding stands and the earlier label ("an empty status element",
"needs its owner's diagnosis") was wrong. Diagnosis from the failing run's
retained trace (extracted to `.tmp/trace-mobile`):

- the case deletes the connection **through the API** while the page is open
  (DELETE 200 at 04:08:35.606, `portal-connections.spec.mjs:521`);
- the server publishes the invalidation, this revision's realtime client
  batches it and refetches `["connections"]`; that read answers at
  ≈35.974+193 ms (04:08:36.17) and its body is byte-identical to the page's
  first, pre-creation read (`resources/764fb6ca…`, no "Stale fixture" row), so
  the reconcile effect at `connections.tsx:378-381` navigates to
  `/portal/connections` and clears `selectionRef.current`;
- the operator's own stale DELETE answers 404 at ≈36.319 and `remove.onError`
  runs (`onNotice` then `refresh()`, whose re-read is the GET at 36.323) — but
  `noticeFor` then compared `selectionRef.current` (already `null`) with the
  refused connection and dropped the notice (36.32);
- nothing else writes the notice, and the dialog that would show
  `remove.error` was already closed, so no `role="status"` element existed for
  the case's 30 s wait: 59 polls resolved to the `sr-only` announcer at
  `connections.tsx:492` only.

So the *change-discovery path this cutover added* changed the outcome of an
untouched spec on an untouched page: the external deletion now removes the row
from the served list before the page's own refusal arrives, and the guard that
exists to drop continuations for connections the operator *moved to* also
dropped the refusal for the connection the operator was still acting on.

Fix (`frontend/src/features/connections/connections.tsx:414-423`): an empty
selection is no longer treated as "moved to another connection" — the refusal
the operator's own command produced is stated when the selection is empty,
while the guard still drops a continuation once another connection is selected.

Demonstration: a new case in `connections.test.tsx` ("states a removal refusal
whose connection the change-discovery re-read removed first") reproduces the
ordering deterministically — it releases a refused DELETE only after the
background re-read has removed the row and the page has reconciled away the
selection. Red on the unfixed guard: `1 failed | 50 passed`, exit 1
(`.tmp/gates/red-demo-connections.log`). Green with the fix, no other case
moving: 18 files, `282 passed`, exit 0. Browser demonstration: the same harness
that had failed the case re-ran it on the repaired bytes — "stale updates and
concurrent deletion reconcile to the server's current state" **passed on both
projects** (desktop 5.3 s, mobile 5.5 s) in the run recorded below, and the
case's assertions were not touched.

### P2-1 and P1-2 — validity and ordering claims scoped

The resource-connections validity cell now states the truth (19 of 22 harness
cases, three failures, two open instrumentation findings, the third repaired),
and the provider-evidence cell no longer cites the compose specs, which never
ran. The ordering limit carries a named exception: the compose surface's
migrated halves have no parity run on this revision, its "Removal and migration"
rows are inspection-only, and the two half-parity cells say "Partly". The
compose build could not be made to work here — container egress to
`registry.npmjs.org` is blocked (`ERR_SSL_SSL/TLS_ALERT_HANDSHAKE_FAILURE`) — so
this is stated, not repaired.

### P2-2 — the Subject binds content

The Subject table now carries the changed-path manifest digest, the corrected
path count (81, not 79 status lines) and the delivered bundle digests; the
sentence about `docs/goals/README.md` is corrected to its measured mtime (10:12,
before the 11:16 control run, the 12:00 browser gate, the 12:03 chat harness and
the 12:10 connection harness), so it was written *between* gate runs rather than
after all of them. `docs/TODO.md` (09:06) is named the same way.

### P2-3 — the protocol deviation's status is explicit

The finding keeps the exact conflict (`docs/design/protocol.md:332` requires a
"latest version"; none of the seven `Invalidations.publish/1` call sites sets
one) and the smallest proposed amendment, and now states plainly that the clause
is met **only if the owner adopts that amendment**. The same note is added to
`docs/design/protocol.md` in the repository's existing style for such notes
(`quality.md`/`harness.md`: a factual paragraph saying what is not delivered and
what the amendment is). No other `docs/design/**` byte is touched.

### P3-1 — the invalidation case now proves a push

The case now routes the reader's socket through the spec's recording handler
before it connects, asserts the socket was established, that it received the
server's invalidation frame between the write and the read (first frame
timestamped) and that the read followed inside the batch window
(`readAfterWriteMs` < 3000, below the 5 s fallback cadence). Observed on both
projects: `pushedInvalidations: 2`, `readAfterWriteMs: 300` (desktop) / `297`
(mobile). The earlier version of the case asserted only a workspace response and
an unchanged `timeOrigin`, which a fallback tick could also have produced; the
receipt now states what it proves. Red/green: with the push assertions pointed
at a value that cannot occur (`toBeGreaterThan(3)`) the case fails; the green
re-run on the final bytes is recorded below.

### P3-2 — attention reads follow work-item changes (closed)

`realtime.ts`'s `invalidationKeys` mapped a work-item change to
`work-item`/`work-item-timeline`/`workspace` and never to `attention`, while the
Attention page's "Blocked work" section reads
`["attention", { projectId, cursor }]` (`application.ts:158-161`,
`attention.tsx:19-26`). A focused Attention page therefore kept the blocked rows
it had read before a blocker resolved. Fix: the work-item kind now also
invalidates `["attention"]` (`frontend/src/services/realtime.ts:267-272`), with
the key pinned in `realtime.test.ts` ("maps a resource invalidation onto the
query keys that read it"). Red without the fix: `1 failed | 10 passed`, exit 1
(`.tmp/gates/red-demo-realtime.log`).

### P3-3 and P3-4 — wording

The capture-set paragraph now counts correctly: 76 entries = 75 case
directories plus Playwright's `.last-run.json`; `attachments/` is per case, not
a top-level entry (every case directory holds its own). The "every gate ran
once" claim is replaced by per-gate revisions and a "Repeats and one failed
attempt" section that names the second control run, the seven browser-gate runs,
the twice-run chat harness including the **11:21 failure** and its repeat, the
twice-run connection harness and the compose harness that executed nothing.

### A failure the repair round's own verification observed (open, not caused by this repair)

The post-repair connection-harness run failed one case that had passed on both
projects in the 12:10 run:
`portal-connections.spec.mjs:1439` "a failed workspace re-read after a write
surfaces the failure instead of the stale rows", **mobile only** (34.9 s;
desktop passed in the same run in 6.9 s). The case injects a failure by routing
`/portal/api/workspace` to `route.abort("internetdisconnected")` and then clicks
the page-level Refresh, expecting "Refresh failed — showing last loaded data".
The retained trace (extracted to `.tmp/trace-reread`) shows:

- **the injected abort never fired**: no request and no
  `net::ERR_INTERNET_DISCONNECTED` (or any `_failureText`) appears for
  `/portal/api/workspace` after the route was installed;
- the click's `refresh()` refetched `["connections"]` (a request 1 ms after the
  click, 200) but **not** `["workspace"]`, and the page's own guard is
  `if (!workspace.isFetching) void workspace.refetch()`
  (`connections.tsx:351-352`, `:501`);
- the workspace query's `isFetching` is **never read during render** (both reads
  are inside callbacks), so React Query's automatic change tracking does not
  watch that property and the page does not re-render when a read's
  `isFetching` flips by itself; a render taken while a read is in flight leaves
  the guard's snapshot at `true` until something else re-renders the page.

The mechanism is therefore a state-snapshot race in the page's Refresh control
(pre-existing: `connections.tsx` is not part of this cutover's diff, and the
case passed on both projects in the 12:10 run on the pre-repair bundle): when
the click lands while `workspace.isFetching` is true in that snapshot, the
operator's refresh silently skips the workspace re-read. The repair did not
touch `refresh()`, the workspace query, or the ProjectResources error path, and
nothing in the repair's runtime changes can act in this case (its writes are
`project`/`connection`/`resource`; no `work_item` invalidation is published, and
the notice it shows comes from `saved()`, not `noticeFor`). What could not be
shown: which state change was last before the click in that run — the trace
records no render boundary, so the snapshot's exact producer is inferred from
the absence of any other explanation for `isFetching` being true.

Recorded rather than repaired here, in line with the repository's handling of
intermittents: it is an open entry in `docs/TODO.md` with the coordinates, the
trace evidence and the smallest proposed corrections (read the query's live
`fetchStatus` at click time instead of the render snapshot, or decide
deliberately that a user-initiated refresh supersedes an in-flight read). The
case's assertions are untouched. The repeat datum is the repository's usual one
for an intermittent: the same harness on the same bytes passed the case on both
projects in the next run (desktop 6.2 s, mobile 7.8 s; that run is 20 of 22 with
only the two credential-scan failures), and both runs are recorded rather than
one being dropped. Across the repair round's two post-repair harness runs the
already-repaired P1-3 case passed on both projects in both runs
(5.3 s / 5.5 s and 4.7 s / 5.7 s).

### Repair-round verification (commands and observed exit codes)

Every command below ran on the repaired bytes this section binds, sequentially
(no measurement harness concurrent), with its exit code captured without reading
it through a pipe. Toolchain: `elixir@1.20.4-otp-29`/`erlang@29.1.2` and
`pnpm@12.17.0` through `mise`.

| Gate | Command (working directory) | Result |
| --- | --- | --- |
| Control format | `MIX_ENV=test mix format --check-formatted` (`control`) | exit 0 (`.tmp/gates/repair-format-test.log`) |
| Control compile | `MIX_ENV=test mix compile --warnings-as-errors` (`control`) | exit 0 (`.tmp/gates/repair-compile-test.log`) |
| Focused Control tests | `POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55463 POSTGRES_DB=symmetry_control_test_repair mix test test/symmetry_control_web/portal_realtime_test.exs test/symmetry_control_web/controllers/portal_controller_test.exs test/symmetry_control_web/portal_html_test.exs` (`control`; realtime channel/token, the SPA document and staged-asset contract) | exit 0 — `24 passed` (`.tmp/gates/repair-control-focused.log`) |
| Frontend typecheck | `pnpm --filter @symmetry/frontend typecheck` (root) | exit 0 (`.tmp/gates/repair-typecheck.log`) |
| Frontend lint | `pnpm --filter @symmetry/frontend lint` (root) | exit 0 (`.tmp/gates/repair-lint.log`) |
| Frontend unit/router | `pnpm --filter @symmetry/frontend test` (root) | exit 0 — 18 files, `282 passed` (281 before; the new P1-3 case) (`.tmp/gates/repair-frontend-test.log`) |
| Frontend build + staged copies | `pnpm --filter @symmetry/frontend build`, then `MIX_ENV=prod mix compile` and `MIX_ENV=dev mix compile` (`control`) | exit 0 — `index-Bkcxy5j-.js` staged; the compiled prod and dev `priv` copies carry the same digests, and all three running gate instances answered the new asset byte-for-byte before the browser runs |
| Subject digest | `node .tmp/0007i-subject-digest.mjs` (root) | exit 0 — `paths=81 present=77 deleted=4 crlf_folded=6`, digest as recorded in "Subject" |
| P1-3 red demonstration | `vitest run src/features/connections/connections.test.tsx` with the guard reverted to its pre-repair form | exit 1 — `1 failed | 50 passed`; the failing case is the new one (`.tmp/gates/red-demo-connections.log`) |
| P3-2 red demonstration | `vitest run src/services/realtime.test.ts` with `["attention"]` removed | exit 1 — `1 failed | 10 passed` (`.tmp/gates/red-demo-realtime.log`) |
| P3-1 red demonstration | cutover spec with the push assertion pointed at an impossible value (`toBeGreaterThan(3)`) | exit 1 — `2 failed` (both projects), `Expected: > 3, Received: 2` (`.tmp/gates/red-demo-push.log`) |
| Cutover spec, repaired assertions | `SYMMETRY_PORTAL_URL=http://localhost:4030 … pnpm --filter symmetry-browser-acceptance test tests/portal-cutover.spec.mjs` (root; gate instances restarted on the new bundle) | exit 0 — `12 passed (47.0 s)`, both projects, including the strengthened invalidation case (`.tmp/gates/repair-cutover.log`) |
| Control suite, run 3 of 3 | `POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55463 POSTGRES_DB=symmetry_control_test_repair mix test` (`control`, dedicated database created and migrated for this run; `MIX_ENV=test`) | **exit 2 — `885/886 passed, 21 skipped`, one failure**, the pre-existing `pwsh` host limitation; the run-2 `GoalsTest` checkout timeout did not reproduce (`.tmp/gates/repair-control-full.log`) |
| Browser gate, delivered bundle | `SYMMETRY_PORTAL_URL=http://localhost:4030 SYMMETRY_SHELL_EXPIRY_URL=http://localhost:4031 SYMMETRY_GOALS_OFF_URL=http://localhost:4032 … pnpm --filter symmetry-browser-acceptance test` (root; both Chromium projects, all specs) | exit 0 — `105 passed, 77 skipped, 0 failed (5.5 min)` on `index-Bkcxy5j-.js`, the same per-spec counts as the pre-repair run (`.tmp/gates/repair-browser-gate.log`) |

Not re-run in the repair round, and therefore not re-gated on the delivered
bytes (their last runs are the pre-repair rows above): the planning, live-daemon
and chat-live harnesses and the compose harness. The repair's reach is two files
— `connections.tsx`'s notice guard (covered by the connection harness and the
unit/router suite) and `realtime.ts`'s added `["attention"]` key for work-item
invalidations (covered by `realtime.test.ts`, the unit/router suite, and the
browser gate's realtime cases, all green on the delivered bundle) — plus the
spec strengthening, so those harnesses' code paths are not changed by it; this
is stated rather than claimed.

## What changed

### Control (Phoenix)

- `lib/symmetry_control/portal/invalidations.ex` (new): `publish/1` +
  `subscribe/0` on the operator topic `portal:invalidations`, the known kind set
  (`connection project resource work_item goal chat run runtime workspace`) and
  the payload member policy (`kind`, identity, `project_id`/`work_item_id`/
  `goal_id`/`run_id`, an optional served `version`, an optional durable `cursor`).
  A goal change is enriched with `Goals.current_event_sequence/1`; a failed
  lookup publishes without a cursor rather than failing a committed request.
- `lib/symmetry_control_web/portal_socket.ex`, `channels/portal_channel.ex`,
  `portal_socket_token.ex` (new): a separate `/portal/socket` mount, `auth_token:
  true` **as a socket option** (declared inside the `websocket:` transport options
  the endpoint overwrites it with `nil`, and the parsed subprotocol token never
  reaches `connect/3` — the transport then refuses every handshake with 403), the
  `base64url.bearer.phx.` WebSocket subprotocol as the only credential, a
  short-lived (`:portal_socket_token_max_age_seconds`, default 300) audience-scoped
  token (`portal:operator`), join-time topic authorization and a receive-only
  channel.
- `lib/symmetry_control_web/plugs/portal_invalidations.ex` (new): publishes on the
  write pipelines `:portal_api`, `:machine` and `:operator` after the response
  committed (`status < 400 or >= 500`), from a path→kind table.
- `lib/symmetry_control_web/controllers/portal_session_controller.ex`: the
  authenticated same-origin bootstrap additionally serves `socket_token`
  (minted per request); `portal_session.ex` keeps the legacy search carrier
  (`project_id`, `run_id`, `chat_scope`) on `/portal` and accepts the chat search
  state, so a signed-out legacy deep link still lands on its destination.
- `endpoint.ex`: socket mount + `Plug.Static` reduced to `login.css`; the SPA
  document and assets keep their own mount.
- `router.ex`: classic route removed; `/portal/chat` added as an SPA document
  route; the invalidation plugs added to the three write pipelines.
- Worker/reconciler/syncer publishers: `settle_task_worker` (goal + run),
  `goal_control_worker` (goal), `reconciler` (run, runtime), `syncer` (resource).

### Contracts

**Superseded by the rebase:** everything in this subsection describes the
pre-rebase revision `7235708`. On the rebased revision the chat contract family
is 0007g's `chat-read` (byte-identical to `main`'s) and the `chat-conversation`
schema, fixtures, generated DTOs, manifest entries and the
`@presentation_reads` entry are removed; see "Rebase onto `main` `ca29a8e`".

- `contracts/v1/chat-conversation.schema.json` (new) plus its generated
  TypeScript and Effect decoders, `contracts/fixtures/valid/chat-conversation.project.json`
  (a sanitized capture of a real served conversation, captured by
  `.tmp/capture-chat-conversation.exs`, sanitized by
  `.tmp/sanitize-chat-conversation.mjs`), four invalid fixtures and their
  manifest entries; `contracts/ts/index.ts` declares the boundary. Schema and
  fixture counts moved 20→21 and 234→239.
- `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs`:
  `chat-conversation` added to `@presentation_reads` (validated against the
  canonical bundle, as the other presentation reads are).

### Frontend

- `src/services/realtime.ts` (new): the single change-discovery owner — operator
  topic subscription, 200 ms invalidation batching, per-kind TanStack Query
  invalidation (`invalidationKeys`), the snapshot refetch on every (re)connect,
  the establishment epoch surfaces use for cursor catch-up, the 5 s polling
  fallback while disconnected, a **visibility refresh that invalidates every read
  when a returning tab becomes visible (whatever the socket status)**, the
  short-lived token refresh before a retry, and the session read excluded from a
  broad invalidation. No second browser event store: invalidations carry no
  state, only "this read may have changed".
- `src/app/application.ts`: `realtime` handle, in-memory-only socket token
  (`rememberSocketToken`, refreshed from the bootstrap), `safeReturnTo` accepts
  `/portal/chat`.
- `src/app/realtime.tsx` (new): `useRealtimeEpoch`/`useRealtimeStatus` over
  `useSyncExternalStore`.
- `src/app/shell.tsx`: connects the socket once the bootstrap token arrives,
  Chat in the navigation and palette, project-scoped chat link, classic footer
  link removed.
- `src/app/router.tsx`, `src/app/legacy.ts`, `src/main.tsx`: `/portal/chat`
  route (validated `scope`/`projectId`/`runId` search) and the one-time legacy
  hash repair applied before the router mounts with a single
  `history.replaceState`.
- `src/features/goals/detail.tsx`: the receipt list continues from its last
  rendered `sequence` through an ordinary read keyed by the connection epoch
  (a reconnect issues exactly one continuation; duplicates are filtered by
  sequence), and `["goal-events-catch-up"]` is invalidated with the goal kind.
  A command's acceptance notice waits (bounded, 2.5 s) for the served goal to
  settle on the version, state and revision the applied write produced before it
  announces acceptance, because the server acknowledges a command before its
  worker applies it and every command envelope carries the versions the page
  holds; without that wait the operator's next command can carry the version its
  own previous command already replaced (observed as a real 409 on this host).
**Superseded by the rebase:** the two chat bullets below describe the
pre-rebase revision's own `/portal/chat` implementation, which the rebase
removes in favour of 0007g's. The `shell.tsx` bullet above keeps its two
cutover-specific halves (the realtime connect effect and the classic footer
link removal) and takes main's `ProjectPicker`, because 0007g's chat owns its
scope selection in-surface.

- `src/features/chat/chat.tsx`, `presentation.ts` (new): the `/portal/chat`
  conversation surface and its pure presentation helpers (see the chat spec and
  its unit tests for the encoded contract). A run-scoped conversation with no
  composer notice of its own states the served delivery receipt of that run's
  newest command, so a conversation opened on a fresh page shows the receipt the
  server serves instead of the idle line.
- `src/features/work/control.tsx`: the surface-local 5 s poll removed (the query
  client's invalidation path and the visibility refresh replace it).
- `src/styles.css`: workspace-wide `prefers-reduced-motion` rule, the appended
  chat block, and `flex-wrap: wrap` on `.page-heading` (the connections heading's
  two controls overflowed a 390 px viewport by 1 px before this).
- `vite.config.ts`: proxies for `login.css` and the `/portal/socket` WebSocket;
  the classic asset proxies removed.

### Removal and migration (after parity)

Removed in this revision:

| Removed | Served before | Migrated suite that now covers it |
| --- | --- | --- |
| `control/lib/symmetry_control_web/portal_html/index.html.eex` (EEx workspace renderer) | every classic view | the SPA surfaces' own specs (`workspace-shell`, `portal-work-views`, `portal-work-planning`, `portal-connections`, `portal-goal-attention`, `portal-execution-control`, `portal-goal-managed`) |
| `control/priv/portal_assets/portal.js`, `portal.css` | classic workspace behaviour and styling | the same SPA specs; `login.css` extracted for the retained login template |
| `PortalController.classic/2` and the `/portal/classic` route | the classic document | an explicit 404 is asserted by `workspace-shell.spec.mjs` and `portal-cutover.spec.mjs` |
| `browser/tests/portal.spec.mjs` | classic shell, keyboard walk, session boundary | migration list at the top of `browser/README.md`: `workspace-shell.spec.mjs`, `portal-work-planning.spec.mjs`, `portal-work-views.spec.mjs`, `portal-compose-daemon.spec.mjs`, `portal-connections.spec.mjs` |
| the classic halves of `portal-compose-daemon.spec.mjs`, `portal-compose-restart.spec.mjs`, `portal-connections.spec.mjs`, `workspace-shell.spec.mjs` | classic board/detail/connection document assertions | the same specs on `/portal/projects/:projectId`, `/portal/work/:workItemId`, `/portal/connections?projectId=` |
| `portal.js`/`portal.css` pins in `portal_controller_test.exs`, `portal_html_test.exs`, `portal_session_test.exs`, `workspaces_read_model_test.exs` | the classic asset serving contract | replaced by login-template and SPA-document assertions in the same files |
| `portal-chat.spec.mjs`, `portal-chat-live.spec.mjs` classic half | `/portal/classic#chat` | retired by 0007g on `main` (`ca29a8e`): both specs already drove `/portal/chat` before this cutover, and this revision carries 0007g's versions byte-for-byte. The rebase section records the pre-rebase revision's own rewrites of these two files as superseded |

Kept: the login template (`login.html.eex` with `login.css`, no script), the
one-time hash repair, `scripts/audit-no-legacy-routes.sh` and its CI job, the
protocol/wire contracts and the fixtures-only provider boundary.

### Documentation

- `docs/plans/2026-11-17-0007i-portal-cutover-plan.md` (new): the increment plan,
  its mapping table (corrected here for the `attention`/`health` scope carriage)
  and the recorded authorization.
- `contracts/README.md`: the chat conversation boundary and the capture rule.
- `browser/README.md`: the cutover removal/migration lists, the new
  `portal-cutover.spec.mjs` cases, the migrated chat suites, the updated
  release-shell gate command.
- `docs/TODO.md`: deferred findings (doc drift in `docs/design/quality.md`, the
  two host limits the gates worked around, the chat contract's unconsumed
  waiting-input members).
- `docs/goals/README.md`: the 0007i closure paragraph pointing at this receipt.

## Done-when clause → evidence

| Goal clause | Evidence on this revision |
| --- | --- |
| One writable UI state owner serves every supported workflow | The only server-rendered document left is the login template (one stylesheet, no script); every workspace route is the SPA document (`PortalController`); the classic asset mount is reduced to `login.css`; the audit fence passes; the SPA owns every writable workflow, including chat, which the classic renderer served before this cutover. |
| Full frontend, contract, browser, accessibility and Phoenix release gates pass on a clean revision, with commands, counts and exit codes | Gate table below. No dedicated axe/automated accessibility suite exists in this repository; accessibility evidence is the keyboard/focus/reduced-motion/narrow cases and their rendered captures, recorded as such instead of being called an a11y gate. |
| Real-time invalidation and reconnect snapshot/cursor catch-up lose no state | `portal-cutover.spec.mjs` "a connected surface refreshes from the operator invalidation channel without a reload" — as strengthened in the repair round below, the reader's socket is routed through the recording handler, the case asserts the reader's socket received the server's invalidation frame between the write and the read (`pushedInvalidations`, first frame timestamped) and that the read followed inside the batch window (297–300 ms observed on the two projects), so a push is proved rather than assumed: the earlier version of this case asserted only a workspace response and an unchanged `timeOrigin`, which a 5 s fallback tick could also have produced. "A reconnect refetches the snapshot and continues the receipts from their last sequence" (blocked socket, one accepted command recorded while disconnected, then the continuation read `after=<last rendered sequence>`, unique ascending sequences and the resolved decision rendered from the server) and "the polling fallback keeps a surface converging while the socket is unavailable" (no socket ever established; the reader converges through its own polling read). Control-side proof: `portal_realtime_test.exs` (topic authorization, receive-only channel, token mint/verify/tamper/expiry, committed-write publication, kind classification, goal cursor). |
| Expired authentication, deep links, focus restoration, reduced motion and narrow screens work end to end, with observed flows and rendered captures | `workspace-shell.spec.mjs` "expired authentication clears private content and recovers the deep link" against a real two-second session-lifetime instance, "the legacy project board hash landed on `/portal/projects/<id>` …", "navigation … reload/back/forward recover, menu Escape returns focus", "signed-out" and "keyboard-focus" captures; `portal-cutover.spec.mjs` "reduced motion renders the workspace without running animation" and "the workspace stays within a narrow viewport and restores focus on Escape". Captures are listed below. |
| Legacy hash redirects land on the new routes, and the classic routes, `portal.js`/CSS and EEx workspace renderer are gone after parity | `portal-cutover.spec.mjs` "every documented legacy hash location lands on its new route exactly once" (per-anchor table, one document load, stable URL after settle, and `/portal/classic` plus `/assets/portal.js` answering 404 inside the same case), `workspace-shell.spec.mjs` legacy-hash and legacy-login cases (one document load, one history replacement, the same two 404s), the "Removal and migration" table above, and the audit fence. The "after parity" half of this clause holds for the surfaces whose migrated suites executed; for the compose halves it is unmet (ordering exception above), and those rows are inspection-only. |
| One series release receipt binds one exact Subject and maps every clause of the full-series acceptance contract to current executable or rendered evidence | This document: the Subject table, the verbatim contract below, the clause map with its evidence-validity column, the rendered capture set and the unresolved findings. Reused 0007a–0007h evidence is named per clause and only used where this revision did not touch the code it covers. |
| Reused evidence is valid for the reviewed revision; missing or stale evidence and unmet requirements leave the receipt incomplete | Evidence-validity column below; the lines that are not current-revision evidence are marked and name the revision they came from. The conversation surface on this revision is **goal 0007g's**, delivered on `main` (`ca29a8e`): its code and its spec files are byte-identical to `main`'s in this revision (`git diff ca29a8e -- frontend/src/features/chat browser/tests/portal-chat*.spec.mjs` prints nothing), so 0007g's own receipt binds the same bytes for that surface, and the chat rows of the pre-rebase revision — which covered this cutover's superseded implementation — are marked superseded. |
| Preserve the contract text below verbatim | The section below is copied unchanged from `docs/goals/0007i-portal-cutover.md` §"Full-series acceptance contract". |
| Provider evidence as real Portal API/database behaviour with controlled provider fixtures; never contact real providers | Connection and resource behaviour is exercised through the real Portal API and PostgreSQL with the fixture transport that 0007d/0007e established: `portal-connections.spec.mjs` through its own harness, and the planning/live-daemon harnesses for the resource reads they drive. No provider host is contacted and no fixture result is promoted to live connectivity proof. The earlier version of this row also cited `portal-compose-*.spec.mjs`; that citation was wrong — neither compose spec executed on this revision (see the gate table and the ordering exception), so they are not provider-behaviour evidence here. |

## Full-series acceptance contract (verbatim)

Replace Symmetry's current portal frontend with a modern TypeScript and Effect
workspace integrated with Phoenix, within the fixed architecture and interaction
contract in [frontend](../design/frontend.md) and [protocol](../design/protocol.md).
Preserve the current project's useful behavior while making progress, results,
blockers, control receipts and necessary decisions clear in actual engineering work.

Completion requires working project/list/board, work detail, GitHub/ADO resource
connections, runtimes, conversation and run-control flows on real APIs; repeatable
typed build and validation; and production delivery with the Phoenix release.
The project owner accepts rendered views for clarity, consistency, information
density and interaction polish, using Linear and Attio as design references.

Verification demonstrates that repeated commands, stale responses, navigation,
expired authentication, refresh, disconnection and reconnection do not corrupt
visible state, lose drafts or misrepresent requested control as applied control.
Keyboard interaction, focus restoration, deep links, pagination and error states
remain usable. Existing acceptance behavior is preserved through the full UI
replacement, and duplicate legacy presentation code is removed after parity.

The workspace has one owner per category of state and follows the selected React,
Vite/pnpm, TanStack and Effect stack. It adds no production Node server or second
business-policy engine. Goal-specific screens use real 0006 capabilities when
available; finishing the existing-product frontend does not depend on inventing
those capabilities or implementing 0006. No production mock data, workflow canvas,
mobile application or visual self-certification is included in completion.

## Contract clause → evidence (with validity for this revision)

| Contract clause | Evidence | Valid for this revision? |
| --- | --- | --- |
| Modern TypeScript/Effect workspace integrated with Phoenix | The SPA in `frontend/` (React, Vite/pnpm, TanStack Query/Router, Effect Schema decoders for the served contracts) served by the Phoenix release document route; `pnpm contracts:check` validates the Effect decoders against every fixture. | Yes — current-revision gate. |
| Project/list/board, work detail flows on real APIs | `portal-work-views.spec.mjs` and `portal-work-planning.spec.mjs` against the running release-config instance, real PostgreSQL, real APIs (both Chromium projects). | Yes — this revision's runs (the specs are unchanged by this cutover except where the migration list says so). |
| GitHub/ADO resource connections on real APIs, controlled provider fixtures only | `portal-connections.spec.mjs` (11 cases × 2 projects) run through its own harness with the fixture provider transport and the real Portal API/database. | **Partly — the gate does not pass on this revision: 12:10 run 19/22 (three failures), post-repair runs 19/22 and 20/22.** Two credential-scan instrumentation findings are open against 0007e's rule, the mobile refusal case was this cutover's regression and is repaired (both post-repair runs green on it), and one mobile case is an intermittent recorded open in `docs/TODO.md`. Provider hosts were never contacted. |
| Runtimes and run control | `portal-execution-control.spec.mjs`, `portal-goal-attention.spec.mjs`, `portal-goal-managed.spec.mjs` and the live-daemon harness on the release-config instance. | Yes — current-revision runs recorded below. |
| Conversation flows on real APIs | **0007g's** `portal-chat.spec.mjs` (deterministic scoped-conversation cases through the real control plane with the five named interception mechanisms its own receipt lists) and `portal-chat-live.spec.mjs` (real daemon lifecycle through `run-chat-daemon-tests.ps1`). Both files are byte-identical to `main`'s on this revision. | **Not measured on this revision**: the browser gate was not run (see "Not measured on this revision" at the end of the rebase section), so this row is carried by 0007g's receipt on byte-identical files plus the frontend unit/integration suite this revision did run. **The live-daemon chat harness was not re-run on this revision**; its evidence is 0007g's own receipt, which binds the identical bytes (`git diff ca29a8e -- browser/tests/portal-chat-live.spec.mjs` prints nothing). The cutover adds no chat implementation of its own: the surface this goal migrated was superseded by 0007g's delivery (see "Rebase onto `main`"). |
| Repeatable typed build and validation; production delivery with the Phoenix release | `pnpm contracts:check`, frontend `typecheck`/`lint`/`test`/`build`, control `mix format --check-formatted`/`compile --warnings-as-errors`/`test`, `MIX_ENV=prod mix release`; the browser gate is run against the release configuration with the packaged listener and the compiled bundle. | Yes — gate table below. The packaged binary itself cannot boot on this Windows host (see limits); the same production configuration runs through `mix phx.server`, as the 0007h receipt also recorded. |
| Repeated commands, stale responses, navigation, expiry, refresh, disconnection/reconnection do not corrupt visible state, lose drafts or misrepresent requested control as applied control | 0007g's chat spec (per-scope draft/selection/scroll with a held late read, the uncertain-send retry under one action identity, the changed replay refused with the server's own conflict), the goal surface's stale-conflict case (`portal-goal-attention.spec.mjs`), `portal-execution-control.spec.mjs`'s receipt and background-refresh cases, `portal-work-views.spec.mjs`'s disconnect recovery, and the cutover spec's reconnect/polling cases. | **Partly — verified by the frontend unit/router suite on this revision (312 passed, exit 0) and by 0007g's runs for the chat cases; the browser cases were not run here.** The background-refresh path runs through the visibility refresh this receipt records (the surface-local poll it replaced is named in "What changed"). |
| Keyboard interaction, focus restoration, deep links, pagination, error states | `workspace-shell.spec.mjs` (keyboard palette walk, Escape focus return, deep links, loading/error captures, signed-out), `portal-cutover.spec.mjs` (narrow viewport and Escape focus, reduced motion), plus the pagination cases in the work/goal/chat specs. 0007g's chat surface carries its own keyboard and long-conversation focus cases in `chat.test.tsx`, which is byte-identical to `main`'s. | **Partly — the frontend unit/router suite ran on this revision (18 files, 312 passed, exit 0); the browser cases were not run here**, so the shell-level rendered evidence is the pre-rebase capture set plus 0007g's runs on the identical chat files. The pre-rebase revision's chat keyboard and 390 px reduced-motion cases belonged to the superseded implementation; the shell-level narrow/reduced-motion cases are this cutover's own and still run. |
| Existing acceptance behavior preserved; duplicate legacy presentation removed after parity | The "Removal and migration" table with its migrated suite column, the audit fence, and the migrated suites' current-revision runs. | **Partly — for every removed surface whose migrated suite executed on this revision; not for the compose surface**, whose two specs never ran (ordering exception and gate table above). |
| One owner per category of state; no production Node server; no second business-policy engine | PostgreSQL owns durable control state, Elixir owns transition policy, the SPA owns presentation, TanStack Query owns read cache; `realtime.ts` is the only change-discovery owner; the app has no production Node server (Vite is build-time only) and no policy evaluation outside the control plane. | Yes — structural property of the revision, verified by reading the changed code and by the SPA document/asset inventory. |
| No production mock data, workflow canvas, mobile application, visual self-certification | The fixtures the specs serve are published wire shapes used as controlled test doubles with the real control plane behind the writes; no mock ships in `control/priv/static`; no canvas or native mobile app was added; this receipt does not rate its own rendering. | Yes. |

## Verification on the pre-rebase revision (`7235708`)

These are the runs the pre-rebase delivery produced; they are **not** evidence
for the rebased revision, whose own runs are the run table in "Rebase onto
`main` `ca29a8e`". Each row below states the run whose counts it records, and no row claims a gate
ran only once: several ran more than once while this revision was being
repaired, and "Repeats and one failed attempt" below names every repeat and the
one attempt that failed before it was repeated green. `pnpm` is the pinned
`pnpm@12.17.0`; Elixir/Erlang are `1.20.4-otp-29`/`29.1.2`.

| Gate | Command (working directory) | Result |
| --- | --- | --- |
| Contract gate | `pnpm contracts:check` (root) | exit 0 — `Validated 21 Draft 7 schemas and 239 fixtures.`; `Effect decoded all 239 raw fixtures; 235 parsed documents reached Schema validation`; `Generated contract DTOs are up to date.` |
| Frontend typecheck | `pnpm --filter @symmetry/frontend typecheck` (root) | exit 0 |
| Frontend lint | `pnpm --filter @symmetry/frontend lint` (root) | exit 0 (0 warnings allowed) |
| Frontend unit/router | `pnpm --filter @symmetry/frontend test` (root) | exit 0 — 18 files, 281 passed (final run, after the chat-surface fixes) |
| Frontend build | `pnpm --filter @symmetry/frontend build` (root) | exit 0 — bundle staged into `control/priv/static/portal`; followed by `MIX_ENV=prod mix compile` so the compiled `priv` copy the instances serve matches (digest equality recorded in "Subject") |
| Control format | `mix format --check-formatted` (`control`) | exit 0 (re-run on the final bytes, after the last control file was written) |
| Control compile | `mix compile --warnings-as-errors` (`control`) | exit 0 (re-run on the final bytes) |
| Control tests — run 1 of 3, 09:44 (`control-test.log`) | `POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55463 mix test` (`control`) | exit 2 — `885/886 passed, 21 skipped`; the single failure is the pre-existing `pwsh` host limitation (`Integrations.CommandTest` "provider token commands can keep stderr diagnostics out of stdout") |
| Control tests — run 2 of 3, 11:16, the final-bytes run this receipt binds (`control-test-final.log`) | same command | **exit 2 — `884/886 passed, 21 skipped`, two failures**: the same `pwsh` host limitation, **and** `GoalsTest` "control recovery selects pending actions after more than one page of terminal no-op Goals" (`control/test/symmetry_control/goals_test.exs:4753`) with `** (DBConnection.ConnectionError) could not checkout the connection owned by … ({:sql_sandbox_owner, …}) … request was dropped from queue after 111ms` raised from `Ecto.Repo.Preloader`'s supervised task (`control-test-final.log:2751-2767`). The earlier version of this receipt reported this run as "the single failure the pre-existing `pwsh` host limitation" and named no revision, which hid the second failure; both failures are now stated, the `GoalsTest` one is listed in `docs/TODO.md`, and run 3 below re-establishes the current state |
| Control tests — run 3 of 3, post-repair, delivered bytes (`repair-control-full.log`) | `POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55463 POSTGRES_DB=symmetry_control_test_repair mix test` (`control`, dedicated database created and migrated for this run) | **exit 2 — `885/886 passed, 21 skipped`, one failure**: the pre-existing `pwsh` host limitation only. The `GoalsTest` sandbox checkout timeout of run 2 did **not** reproduce on this run or database; it is not claimed fixed, and stays open in `docs/TODO.md`. This is the run that keeps the current state from resting on run 2's red log alone |
| Realtime control tests | `POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55463 mix test test/symmetry_control_web/portal_realtime_test.exs` (`control`) | exit 0 — 9 passed |
| Go vet | `go vet ./...` (`daemon`) | exit 0 |
| Go contract mirror | `go test ./internal/contracts/` (`daemon`) | exit 0 |
| Legacy-route audit fence | `bash scripts/audit-no-legacy-routes.sh` (Debian container with the repo mounted; the script needs `bash` and ripgrep or GNU grep) | exit 0 — `No U2 legacy action endpoints found in production contract surfaces.` |
| Browser gate (all specs, both Chromium projects) — 12:00 run (`browser-final6.log`), the last pre-repair run | `SYMMETRY_PORTAL_URL=http://localhost:4030 SYMMETRY_SHELL_EXPIRY_URL=http://localhost:4031 SYMMETRY_GOALS_OFF_URL=http://localhost:4032 SYMMETRY_OPERATOR_TOKEN=… SYMMETRY_PLAYWRIGHT_OUTPUT=<repo>/.tmp/0007i-captures pnpm --filter symmetry-browser-acceptance test` (root) | exit 0 — 105 passed, 77 skipped, 0 failed (5.6 min), on the bundle this receipt's first version described (`index-Cfe2xkX1.js`, `sha256:6a3e4c92…`); the delivered bundle (`index-Bkcxy5j-.js`) was gated by the repair-round run recorded below. Per spec, both projects: `workspace-shell` 18, `portal-chat` 26, `portal-cutover` 12, `portal-execution-control` 10, `portal-goal-attention` 18, `portal-work-views` 21. The 77 skips are the specs whose own harness is their only executor (`portal-connections` 22, `portal-work-planning` 36, `portal-live-daemon` 10, the compose and live-chat specs, `portal-goal-managed` 2); each is run by its harness below, and a standalone skip never counts as a pass |
| Browser gate (all specs, both Chromium projects) — repair round, the delivered bundle (`repair-browser-gate.log`) | same command with the same three restarted instances and `SYMMETRY_PLAYWRIGHT_OUTPUT=<repo>/.tmp/0007i-captures-repair` | **exit 0 — 105 passed, 77 skipped, 0 failed (5.5 min)** on the delivered bundle (`index-Bkcxy5j-.js`, `sha256:19b1f692…`): the same per-spec counts as the pre-repair run, so the repair (the two changed frontend files) changed no case's outcome; the cutover spec's strengthened invalidation case is inside these 105. The 77 skips are the same harness-only specs as above |
| Chat daemon harness — 12:03 run (`harness-chat2.log`) | `pwsh -NoProfile -File browser/scripts/run-chat-daemon-tests.ps1 -ControlUrl http://localhost:4030` (root) | exit 0 — `1 passed (1.2m)`, "Chat live acceptance passed": the real fake-agent daemon drove questions, guidance, decisions and controls through one live run history on `/portal/chat`. The 11:21 attempt on the then-current repair bytes failed and is itemised under "Repeats and one failed attempt" rather than dropped |
| Connection fixture harness — run 1 of 3, 12:10 (`harness-connection.log`) | `pwsh -NoProfile -File browser/scripts/run-connection-fixture-tests.ps1 -PortalUrl http://localhost:4021` (root) | **exit 1 — 19 of 22 cases passed** (`19 passed (5.5m)`, the harness's own summary guard expects 22 with no skips). The fixture transport, the credential-canary scan of the control logs and the provider-call log passed. Three cases failed: `portal-connections.spec.mjs` "external work items keep their identity…" on both projects (the spec's credential-scan rule "an unreadable response must belong to an aborted request" meets the cutover's superseded reads: Chromium reports a client-aborted-after-headers read as a response with no `request.failure()`) — recorded as an open 0007e instrumentation finding below — and "stale updates and concurrent deletion reconcile…" on mobile, **not** a locator artifact: the audit's read of the retained trace established that the page held no notice element for the whole 30 s because the change-discovery re-read dropped the selection before the refusal arrived. That is a real regression of this cutover's own change-discovery path, diagnosed and repaired in the repair round; runs 2 and 3 below re-ran this same harness after the repair |
| Connection fixture harness — run 2 of 3, post-repair (`.tmp/gates/repair-connection-harness.log`) | same command | **exit 1 — 19 of 22 passed**: the two credential-scan findings and one mobile intermittent (`portal-connections.spec.mjs:1439`, diagnosed in the repair round below and recorded open in `docs/TODO.md`). The mobile refusal case this repair fixed ("stale updates and concurrent deletion…") passed on both projects (desktop 5.3 s, mobile 5.5 s) |
| Connection fixture harness — run 3 of 3, post-repair repeat (`.tmp/gates/repair-connection-harness2.log`) | same command | **exit 1 — 20 of 22 passed (3.4 min)**: only the two credential-scan findings failed; the `:1439` intermittent passed on both projects (6.2 s / 7.8 s) and the repaired case passed on both again (4.7 s / 5.7 s). Two runs on the same bytes are recorded; neither is dropped |
| Planning harness | `pwsh -NoProfile -File browser/scripts/run-planning-tests.ps1 -Port 4040 -PortalUrl http://localhost:4040` (root) | exit 0 — `36 passed (3.1m)`: the real control plane this harness starts on its own port and database drove every planning case on both projects, including the two unwritable item kinds and the archived-project read-only case |
| Live daemon harness | `pwsh -NoProfile -File browser/scripts/run-live-daemon-tests.ps1 -ControlUrl http://localhost:4030` (root) | exit 0 — `6 passed, 4 skipped (43.9s)`, "Live daemon browser tests passed": real daemon runtimes drove the execution-control surface, the runtime row compared with the daemon's own registration record, cancellation, retry and the real raw-history cursor; the four skips are the spec's documented desktop-only race/capability cases on the mobile project |
| Compose harness | `pwsh -NoProfile -File browser/scripts/run-compose-daemon-tests.ps1 -ControlUrl http://localhost:4000` (root; both compose specs) | **not executed in this environment** — the harness's own `docker compose up --build` fails before any spec runs: the `node:24-bookworm-slim` builder cannot install pnpm (`npm error code ERR_SSL_SSL/TLS_ALERT_HANDSHAKE_FAILURE … request to https://registry.npmjs.org/pnpm failed, reason: … ssl/tls alert handshake failure: SSL alert number 40`), i.e. container egress to the npm registry is blocked on this host while the host's own pnpm reaches it through its proxy. The compose specs (and their migrated classic halves) are therefore **unverified on this revision**; their migration is covered only by inspection and the audit fence, and this is a preserved limit, not a pass |
| Phoenix release build | `MIX_ENV=prod mix release --overwrite` (`control`) | exit 0 — packaged after the last frontend build, so the release carries the bundle digest recorded in "Subject" |

### Repeats and one failed attempt

This section exists because the receipt's earlier version claimed "every gate
below ran once", which was not literal. The repeats, named per gate:

- **Control suite: twice on the pre-repair bytes** — `.tmp/gates/control-test.log`
  (09:44, `885/886 passed, 21 skipped`, exit 2, the `pwsh` failure alone) and
  `.tmp/gates/control-test-final.log` (11:16, `884/886 passed, 21 skipped`,
  exit 2, `pwsh` **and** the `GoalsTest` sandbox checkout timeout), plus the
  third, post-repair run recorded in "Repair round" below on the delivered
  bytes. The 11:16 run is the one whose counts the gate table used; its second
  failure was missing from the earlier version of this receipt and is now also
  in `docs/TODO.md`.
- **Browser gate: seven times** — `.tmp/gates/browser-full.log` (09:56,
  `70 passed / 77 skipped / 35 failed`, the run whose failures
  "Findings returned and fixed" lists) and the six `browser-final*.log` runs
  (`103/77/2`, `104/77/1`, `105/77/0`, `102/77/3`, `104/77/1`, `105/77/0`; the
  last at 12:00 is the run the gate table cites). The repair round re-ran the
  full gate once more (below) because the repair changed the shipped bundle.
- **Chat daemon harness: twice** — `.tmp/gates/harness-chat.log` (11:21)
  **failed** on the then-current repair bytes: after selecting a completed run,
  `[data-chat-send-status]` kept the idle line "Messages are saved to this
  context." instead of the run's served delivery receipt "Applied at a safe
  boundary" (10 s expect, 23 polls; screenshot and video retained under the
  run's own `symmetry-playwright-results` directory). It passed on the next
  attempt after the chat-surface fixes landed: `.tmp/gates/harness-chat2.log`
  (12:03, `1 passed (1.2m)`, "Chat live acceptance passed"), which is the run
  the gate table cites.
- **Connection fixture harness: twice** — the run the gate table cites
  (`.tmp/gates/harness-connection.log`, 12:10, `19 passed`, three failures) and
  the repair round's re-run of the same harness after the mobile refusal case
  was repaired (below).
- **Compose harness: once, executing no spec** — `.tmp/gates/harness-compose.log`
  (12:18) dies in `docker compose up --build` before any spec starts.
- The contract, frontend (`typecheck`/`lint`/`test`/`build`), Go, format,
  compile, release-build and audit-fence rows each record their last run on the
  bytes described in "Subject"; those gates were also re-run after earlier fix
  rounds during the repair.

Host-limit notes for the gate set: the three gate instances are started
sequentially (three concurrent `mix phx.server` boots contend for Mix's
compilation lock and one dies silently, which is how an earlier diagnostic run
lost the expiry instance and produced a misleading `ERR_CONNECTION_REFUSED`);
each instance must answer `/healthz` before the gate runs. The operator socket
keeps Phoenix's default origin check, which compares the `Origin` host with the
endpoint's configured url host: every browser gate, harness and deployment must
serve the host it is browsed at (`PHX_HOST` here; `compose.yaml` sets its own;
the CI release gate was pointed at `127.0.0.1`, the host it drives). The check is
not disabled anywhere — the alternative to the mismatch is a refused handshake,
not a weakened gate.

The acceptance database (`symmetry_control_dev`) was dropped, recreated and
migrated before the recorded browser run, the same "dedicated database recreated
on every run" rule the connection and planning harnesses already apply: two
earlier runs of this revision were distorted by rows the live-daemon and chat
harnesses had registered (the runtimes list's served registry count drifted
between the spec's own read and the rendered page, whose reconciler had marked
the dead harness runtimes offline) and by an unrelated refused subresource at
teardown. Neither was a product path; the recreated database removes the first
class, and the second is recorded above with its trace evidence.

## Rendered capture set (pre-rebase revision `7235708`)

Full-page screenshots plus observation JSON are written through the shared
`capture` helper into `SYMMETRY_PLAYWRIGHT_OUTPUT`. The acceptance run's
directory is `<repo>/.tmp/0007i-captures` (kept in the working tree, outside the
committed files); the recorded run left 76 entries there — 75 directories, one
per captured case, plus Playwright's `.last-run.json` file. `attachments/` is
not a top-level entry: each case directory holds its own `attachments/`
subdirectory beside that case's screenshots and observation JSON:

The rebased revision's own capture set is in a separate directory and is listed
in "Rebase onto `main` `ca29a8e`"; the table below is the pre-rebase set, whose
chat entries belong to the superseded implementation.

| Spec | Captures |
| --- | --- |
| `workspace-shell.spec.mjs` | `signed-out`, `empty`, `keyboard-focus`, `navigation`, `loading`, `error`, `attention-scope`, `fresh-csrf`, `legacy-hash-repair`, `legacy-login-scope`, `expired-auth` |
| `portal-cutover.spec.mjs` | `legacy-hash-routes`, `realtime-invalidation`, `reconnect-catch-up`, `polling-fallback`, `reduced-motion`, `narrow-focus` |
| `portal-chat.spec.mjs` | `chat-unsafe-pr`, `chat-start-work`, `chat-controls`, `chat-decision`, `chat-retry-refresh`, `chat-history-gap`, `chat-keyboard`, `chat-narrow-reduced-motion` |
| `portal-chat-live.spec.mjs` | the live-daemon capture set recorded by the chat harness |

Failure artifacts (screenshots, video, trace, error context) are retained beside
them for any case that failed during diagnosis.

## Unresolved limits and findings

Everything in this section is the **pre-rebase revision `7235708`'s** record
unless a bullet says otherwise; the rebased revision's own limits — including
what could not be re-measured — are listed at the end of "Rebase onto
`main` `ca29a8e`".

- **Findings returned to the responsible surface and fixed in this revision.**
  The first browser gate run on this revision was 70 passed / 77 skipped / 35
  failed. Every failure was diagnosed to a cause and fixed; none was waived:
  - *Gate setup, not code*: the expiry instance had died before the run — three
    concurrent `mix phx.server` boots contend for Mix's lock and one exits
    silently — and the nine other harness instances must serve the host they are
    browsed at (origin check). The instance supervisor now starts them
    sequentially, waits for each `/healthz`, and every harness is invoked with
    the host its endpoint serves.
  - *Removed surface poll*: `portal-execution-control.spec.mjs`'s background
    refresh case and `portal-work-views.spec.mjs`'s disconnected-read case
    pinned a refresh on a `visibilitychange` signal the retired surface-local
    poll used to produce. The realtime client now refreshes every read when a
    returning tab becomes visible, whatever the socket status, with a unit case
    of its own; the specs were left unchanged.
  - *Narrow-screen defect*: `portal-cutover.spec.mjs`'s 390 px case found the
    connections heading overflowing by 1 px (its two controls did not fit the
    non-wrapping heading row). `.page-heading` now wraps.
  - *Chat spec fixtures (superseded surface)*: 20 chat cases failed because the intercepted fixture
    served incomplete wire shapes (work-detail outcome members, the raw timeline
    item, `waiting`, the command projection, history message members) that the
    surface's strict decoders refused, plus two spec-side surface corrections
    (the board view URL, and the classic raw-payload step replaced by the SPA's
    own contract — no raw payload is rendered anywhere in the SPA). Fixtures were
    completed from the published contracts; no assertion was relaxed.
  - *Chat surface defects (superseded surface)*: the scope switch dropped the selected run (its draft,
    notice and scroll were lost on return) — the URL now keeps the run and a
    scope re-entry re-reads its conversation; the stale-receipt rule was
    unreachable because the receipt index spanned every loaded page instead of
    the newest page plus the pages its cursor chain still links; the load-older
    control had no accessible name; and a chat-scoped media rule made the
    conversation region un-scrollable at mobile widths. All four are fixed in
    `frontend/src/features/chat/**` with unit/integration cases of their own, and
    the chat spec was left untouched by the fix (26 cases: 13 + 13).
  - *Goal command race*: the goal surface announced a command's acceptance before
    the worker's applied write landed, so the operator's next command could carry
    the version its own previous command had already replaced (a real 409 in the
    suite). It now waits (bounded) for the served goal to settle on the applied
    version, state and revision before stating acceptance; the unit case's wait
    for the notice was extended to cover that bound, with the assertion
    unchanged.
  The final gate run recorded below is the repeat of the affected validation on
  the fixed revision.

- **Unverified in this environment: the compose specs** (and with them the
  migrated classic halves of `portal-compose-daemon.spec.mjs` and
  `portal-compose-restart.spec.mjs`). Their harness builds the control image
  inside Docker, and this host's containers cannot reach the npm registry
  (`ERR_SSL_SSL/TLS_ALERT_HANDSHAKE_FAILURE` against `registry.npmjs.org` while
  the host's own pnpm succeeds through its proxy). The command, the exact error
  and the consequence are recorded in the gate table; the compose specs are not
  reported as passing anywhere in this receipt.

- **One connection-suite finding returned to 0007e's evidence; the second was
  this cutover's own regression and is repaired.** The connection fixture
  harness run of 12:10 is the only gate that did not pass on the pre-repair
  bytes (19 of 22 cases; see the table):
  1. `portal-connections.spec.mjs`'s credential-scan rule accepts an unreadable
     response body only when the request itself failed. The cutover's
     invalidate-and-refetch pattern supersedes in-flight reads, and Chromium
     reports a read aborted *after* its headers arrived as a response with no
     `request.failure()`, so the rule now fails on `/portal/api/work-items/<id>`
     and `/portal/api/workspace` even though the response headers were scanned
     and no other response body was readable-with-a-credential. Amending that
     rule is a security-instrumentation decision for its owner (0007e), not a
     cutover change: the product behaviour it reacts to — a superseded read being
     aborted, authoritative refetch instead of a stale answer — is the fixed
     design's own. **Open**, for 0007e's owner.
  2. The same spec's mobile "stale updates and concurrent deletion reconcile…"
     case is **not a locator artifact**: it is a real behavioural regression of
     this cutover's own change-discovery path, diagnosed from the retained trace
     (`.tmp/trace-mobile`, the failing run's `trace.zip`) and repaired in the
     repair round below. The earlier version of this receipt recorded it as
     "the first `role=status` element … was empty" and left the cause to
     "its owner's diagnosis against the rendered page"; that was wrong. The
     spec and the page are untouched by the original cutover, and 0007e's
     receipt records this spec 22/22 at `38d19ce`, which is what made the
     regression visible.

- **One host-level browser wedge, recorded rather than hidden.** On the first
  full-suite run of the final revision the only failure was
  `workspace-shell.spec.mjs`'s mobile `signed-out legacy project board link…`
  case: the SPA document stayed in its "Checking your session…" state for 13 s
  although the trace shows that session request answered HTTP 200 in 2.6 ms, no
  console or page error was raised, and the browser-context teardown then also
  hung past the test timeout — a wedged renderer, not an application path. The
  case passed 3/3 in isolation (3.0–3.3 s each) on the same bytes, and the suite
  was then repeated once with nothing else running; both runs, the isolation
  runs and the trace evidence are recorded here rather than reported as one
  clean pass.

- **Owner review is not part of this run.** The rendered views' clarity,
  consistency, information density and interaction polish are the owner's verdict;
  this receipt states the result as produced.
- **No automated accessibility gate exists in this repository.** Accessibility
  evidence is the keyboard, focus-restoration, reduced-motion and narrow-viewport
  cases and their captures; calling that an a11y gate would overstate it.
- **The packaged release binary cannot boot on this Windows host**: the generated
  `erts-*/bin/erl` shim calls `erlexec` without the Windows `.exe` name. The
  browser gate therefore runs the production configuration through
  `mix phx.server` (same release-style instance, compiled bundle and listener),
  the same limit the 0007h receipt records. The release *build* itself is a gate.
- **Real-time payload wording.** `docs/design/protocol.md`'s realtime section
  names "resource type/id, latest version, event cursor" in the payload. The
  implementation publishes the kind, the identity when the publisher knows it,
  the served version when the changed stream exposes one (no current publisher
  does) and the durable goal event cursor; no consumer reads a version, and a
  notification is never business truth. Proposed smallest amendment: replace
  "latest version" with "the served version marker when the changed stream
  exposes one". Recorded as a finding rather than changed silently.
  **Status: open, and the delivery does not satisfy the clause as currently
  written.** The exact conflict: `docs/design/protocol.md:332` requires the
  invalidation payload to carry a "latest version", and none of the seven
  `Invalidations.publish/1` call sites (`goal_control_worker.ex:31`,
  `settle_task_worker.ex:35-36`, `reconciler.ex:22-23`, `syncer.ex:32`,
  `plugs/portal_invalidations.ex:29`) sets a `version` member — the member
  exists in the payload policy but every publisher leaves it absent, because no
  changed stream exposes a version to send. The goal's clause "fixed design
  decisions are not changed silently" is met by this record; the clause is met
  *only if the owner adopts the amendment above* (or directs a different one).
  Until then this receipt claims the deviation, not conformance.
- **`docs/design/quality.md` drift** (pre-existing): it still says the frontend
  commands "do not exist until goal 0007 builds `frontend/`" while CI has run
  them since 0007a. Listed in `docs/TODO.md`.
- **Chat conversation contract coverage (closed by the rebase)**: the
  `chat-conversation` schema's captured fixture exercised no waiting-for-input
  run row. The surviving family does: `chat-read` requires `waiting` and
  `latest_command` on a run row and its manifest carries
  `valid/chat-read.run-waiting-decision.json` (tags `run-scope`,
  `waiting-decision`, `command-receipt`), so the entry was dropped from
  `docs/TODO.md`.
- **The chat spec's history-gap case changed shape (pre-rebase revision only)**:
  this describes the superseded implementation's spec. 0007g's spec, which this
  revision carries, names the same contract its own receipt records; the SPA keeps the newest
  page as one query key (a moving window) instead of the classic client-side
  merged map, so its intermediate visible count differs from the classic
  assertion while the asserted contract (every message reachable through the
  cursor chain, no duplicates, receipts refreshed) is unchanged. Recorded here
  because it is a visible-count change, not a weakened assertion.
- **Provider evidence boundary**: connections/resources run against the real
  Portal API and database with controlled provider fixtures (`fixtures_only`);
  no provider host is contacted, and no fixture result is promoted to live
  connectivity proof. Live provider credentials remain unverified.
- **Host limits the gates worked around** (also in `docs/TODO.md`): the audit
  fence needs `bash` plus ripgrep or GNU grep and was executed in a Debian
  container — measured again on the rebased revision, the fence and its own test
  suite both pass here under Git Bash (`1298 tracked files audited, 4
  intentional exclusions`, and `Ran 6 tests ... OK`), while this session's nested
  bash gives the fence the MSYS-style root `/mnt/q/...`, which the Windows `git`
  cannot resolve, so the script's own tracked-file check fails closed there
  (exit 2) rather than auditing a wrong tree; the control suite's run-2 failures are the `pwsh` shim host
  limitation **and** the load-sensitive `GoalsTest` sandbox checkout timeout
  (both named in the gate table, the second also in `docs/TODO.md`); the
  instances must be started sequentially as described above. The `GoalsTest`
  case is not waived: run 3 in the repair round is the current state.
