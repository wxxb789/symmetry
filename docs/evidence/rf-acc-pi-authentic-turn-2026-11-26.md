# rf-acc-pi-authentic-turn — evidence record (stage 1: root cause)

Goal `rf-acc-pi-authentic-turn` (contract: the goal file under the GDD contracts root, ruled by the
conductor 2026-11-26; dispatch gate #303). Branch `refactor/rf-acc-pi-authentic-turn`, base and
dispatch head `07be175e35088b4bbbaad469b796a227ad3784de`. Plan:
`docs/plans/2026-11-26-rf-acc-pi-authentic-turn-plan.md`. Every `file:line` citation binds that
revision; the record's own bytes are uncommitted in the goal worktree and are measured by the gate
rows in §8 on a scratch-clone commit. This record carries stage-1 evidence only: the goal's
acceptance row (the conductor's Bind) is the bind stage's, and stage 2 waits for the ruling §7 asks
for.

## 1. The stop under analysis, verbatim

Three consecutive Binds report one stop, byte-identical; Bind 1 below (`shared`, run `45811007804`
on the PR head plus the two scratch-only files; `gdd/findings/303-binding-run.md` §3.3, with
`303-binding-run-3.md` §2 reporting the same text on the merged head):

```
witness output:     production_linux_v2_native_witness_test.go:91: wait for isolated production owner-witness command: hidden owner-witness command exited unsuccessfully: exit status 1
owner-witness command stderr (bounded last 20 lines):
-linux-v2-native-owner-witness: wait for authentic Pi owner witness turn: pi rpc final assistant message did not stop normally: "error"
```

The third line is the daemon's `ErrUnexpectedAssistantStop` (`daemon/internal/harness/pi/rpc.go:53`).
Chain, at the cited revision: the witness sends the claim's Goal as the pi prompt
(`daemon/internal/app/linux_owner_witness_linux.go:3848`), takes the turn result (`:3865`) and asks
the session to wait (`:3871-3872`, the wrapper line of the stderr above). The wait resolves through
the validator's `Finish` (`daemon/internal/harness/pi/rpc.go:820-828`), which hands the retained
final assistant message (`:748`) to `normalAssistantStop` (`:848-861`) and accepts only
`stopReason == "stop"`. The same leg's provider-action assertions and the fixture's receipt check
are downstream of that acceptance, so the turn's provider error aborts the leg.

Same Bind, the consequences the goal's problem statement names:

- `stop_readback_reader` has one call site (`scripts/ci/linux-v2-native-acceptance.sh:3226`; the
  function is defined at `:2202`), after the `case`/`esac` that dispatches the pi leg at `:3160`.
  The leg's failure under `set -Eeuo pipefail` ends the script before `:3226`, so the reader's
  Result row is unmeasured, not failed.
- The `shared` allowlist (`scripts/ci/export-linux-v2-redacted-evidence.py:293-312`) needs every
  leg's document; with the abort, the export refuses at `:1486-1490` and no manifest exists.

## 2. The credential the leg actually received

The Bind harness is the ratified scratch pattern: a push-triggered copy of the credentialed
workflow with **no** `environment:` block and inline dummy pins. Bind 1 recovered the scratch copy
byte-exact from `b0ba2694` (git blob `275b31ae6fed3fb3f6c2907958ae6ff3914bc690`, sha256
`e3f526a2c06a4079a4e6b4e2ce4240574597fe593b00c5c199f7868871e70e8f`) and edited only the
`case_sets` array (run copy `bb6dcaf5e216aa7273dc472d76851fce2eb29cfaa7c335b6939f6053db948e0b`;
`gdd/findings/303-binding-run.md` §1-§2). Its acceptance job env:

```
          GH_TOKEN: gho_scratchdummy0000000000000000000000000000
          SYMMETRY_OPENAI_API_KEY: sk-scratch-dummy
          SYMMETRY_PI_MODEL: deepseek-v4.1-flash-fast
          SYMMETRY_OPENAI_BASE_URL: https://litellm-proxy-hk.graydune-4a6be1e2.eastasia.azurecontainerapps.io/v1
```

What the fixture does with those values, at the cited revision:

| Step | Site | Effect on the failing run |
| --- | --- | --- |
| require a non-empty provider key | `scripts/ci/linux-v2-native-acceptance.sh:19` | `sk-scratch-dummy` satisfies it; no format check exists |
| write the B-only credential file | `:393-394` (path `:81`), unset in the runner shell `:398` | the placeholder is the only key the guest ever holds |
| stage B's private pi home from it | `:1218-1221` settings, `:1222-1224` `auth.json` key, `:1225-1249` `models.json` routing `providers.openai.baseUrl` at the run's base URL for the `deepseek-v4.1-flash-fast` entry (`api: openai-completions`, `reasoning: true`, `thinkingLevelMap.minimal: low`) | pi authenticates and routes exactly as the fixture intends |
| B-only file and directory guards | `:658-665`, plus the A-side read/write refusals at `:1025-1035` | credential locality held in the failing run: nothing echoed the key |

The three config shapes are the pinned release's own documented ones: `auth.json` as the agent
directory's saved-API-key file, `defaultProvider`/`defaultModel`/`defaultThinkingLevel` in
`settings.json`, and `providers.<id>.baseUrl` + `models[].id/api/reasoning/thinkingLevelMap` in
`models.json` (the release tree's own `docs/configuration.md`, `docs/settings.md` and
`docs/models-configuration.md`, read from the pinned tarball; no install was run). §4.2 measures
that pi 1.5.2 resolves them.

## 3. The contract's candidate list, discriminated

| Candidate | Verdict | Deciding row |
| --- | --- | --- |
| missing or dummy provider credentials | **root cause** | §4.1 the base URL answers 401 for the placeholder; §4.2 pi turns that answer into this exact stop; §2 shows the placeholder is what the fixture staged |
| network or egress denial inside containment | **excluded** | §4.3: no namespace or address filter exists on the path, and a transport failure's retry schedule cannot fit the leg's wall time |
| model or provider config | **correct as configured** | §4.2 `get_state` resolves the fixture's own base URL, model, api and thinking level |
| tool-call policy | **not reached** | §4.2: the failing message has empty content, no tool call and no tool results; §4.4 shows what a passing turn needs |

## 4. Measured rows

### 4.1 The configured provider rejects the placeholder key

Run 2026-11-26 against the exact base URL the scratch run pinned, with the placeholder key the
scratch workflow passed (the key is a literal in the recovered workflow bytes, not a secret):

| Command | Result |
| --- | --- |
| `curl -sS -m 25 -o models_dummy.json -w 'HTTP %{http_code}' -H "Authorization: Bearer sk-scratch-dummy" <base>/models` | `HTTP 401` |
| `curl -sS -m 40 -H "Authorization: Bearer sk-scratch-dummy" -H 'Content-Type: application/json' -d '{"model":"deepseek-v4.1-flash-fast","messages":[{"role":"user","content":"reply with the single word ok"}],"max_tokens":8}' <base>/chat/completions` | `HTTP 401` |

Both bodies:
`{"error":{"message":"Authentication Error, Invalid API key provided.","type":"token_not_found_in_db","param":"key","code":"401"}}`.
`param:"key"` means a key was presented and unknown — not an absent one.

### 4.2 The pinned pi release maps that answer to the retained stop

The CI bytes are pinned at `scripts/ci/linux-v2-native-tools.sh:41-46` (url, archive sha256
`811c6f2c9e7aaee252b5404252e556fb713aa209c1f7f924226aa77d4ef70cb5`, normalized tree digest, version
line). A copy of that archive was fetched and re-hashed: same sha256. Code excerpts read from the
released Linux binary:

- the provider-failure arm sets `stopReason = options?.signal?.aborted ? "aborted" : "error"` and
  `errorMessage = formatProviderError(normalizeProviderError(error))`;
- `mapChatStopReason` maps a provider-reported `finish_reason` of `error` to the same stop reason
  with `errorMessage: "Provider stopped with: error (server error)"`.

Measured behaviour, on the release's Windows blob (asset digest
`e6adb8cb8c84eca14391ad24b363de9d2be98a89ca3ba3fdee1fa4de32b7f2e8`, version line `1.5.2`, built
from the same release source as the Linux bytes quoted above), driven in `--mode rpc` with
`--provider openai --model deepseek-v4.1-flash-fast` and a probe home holding §2's three files
verbatim; one `get_state` command then one prompt:

| Row | Configuration | Assistant `message_end` |
| --- | --- | --- |
| A (the run's own shape) | the run's base URL, the placeholder key | `{"content":[],"api":"openai-completions","provider":"openai","model":"deepseek-v4.1-flash-fast","stopReason":"error","errorMessage":"401: {\"message\":\"Authentication Error, Invalid API key provided.\",\"type\":\"token_not_found_in_db\",\"param\":\"key\",\"code\":\"401\"}","durationMs":523}`, one attempt, then `agent_settled` |
| B (control) | same files, base URL `http://127.0.0.1:9/v1` | the same `stopReason`, `"errorMessage": "Connection error.: ECONNREFUSED: Unable to connect. Is the computer able to access the url?"`, four attempts with 2/4/8 s backoff, then `agent_settled` |

Row A's `get_state` response resolved `{"id":"deepseek-v4.1-flash-fast","api":"openai-completions",
"provider":"openai","baseUrl":"<the run's base URL>","reasoning":true,
"thinkingLevelMap":{"minimal":"low"}}` — i.e. the fixture's configuration chain is accepted
end-to-end, and only the credential fails. Row A is the exact message the daemon's
`normalAssistantStop` rejects, which produces §1's stderr byte-for-byte.

### 4.3 Why a transport failure does not explain the Bind rows

- Row A settles after one attempt in 523 ms; row B needs 2 + 4 + 8 s of retry backoff. The Bind leg
  is inside a hidden command whose whole test wall time is 5.31 s (Bind 1), 5.46 s (Bind 2) and
  5.38 s (Bind 3), printed in the same leg's failure block, and its stop follows the previous
  verified row by ~8 s of log time (`gdd/scratch/bind303/run-45811007804.log`, 11:33:54.28 →
  11:34:02.38). A retrying transport failure cannot fit either bound.
- No denial mechanism sits on that path: the guest boots on the host network (the nspawn invocation
  at `scripts/ci/linux-v2-native-acceptance.sh:975-979` passes no network-namespace option), the
  gate unshares `CLONE_NEWUSER` alone (`daemon/native/linux-gate/c-gate/gate.c:1414`), and
  `IPAddressDeny`/`IPAddressAllow`/`PrivateNetwork` occur nowhere in the acceptance script or
  `scripts/install-linux-owner.sh`.
- The runner itself has egress: the job's own steps fetch actions, apt packages and GitHub
  resources in the same run, and this host reached the same base URL (§4.1).

### 4.4 What a passing turn requires

Beyond settlement, the leg demands a Control provider-action receipt for the model's brokered
`resource.sync` call: the e2e's summary carries `provider_action_succeeded`
(`daemon/e2e/production_linux_v2_native_witness_test.go:1380`), the provider-free/provider-scoped
admission pairs are asserted at `:1706-1712`, and the fixture refuses with
`Control did not persist a succeeded provider_action_intents receipt for the exact Pi
Claim/Run/resource/operation` (`scripts/ci/linux-v2-native-acceptance.sh:2465`). A model request
that is rejected before any token is produced cannot satisfy it, whatever the tool policy is.

## 5. The real (non-scratch) acceptance workflow: the credential path it has

- `.github/workflows/linux-native-v2-acceptance.yml` is `workflow_dispatch:` only, and its
  acceptance job declares `environment: linux-v2-native-acceptance` (`:19-20`) with
  `secrets.SYMMETRY_LINUX_V2_TEST_GITHUB_OAUTH_TOKEN` /
  `secrets.SYMMETRY_LINUX_V2_TEST_OPENAI_API_KEY` and the three `vars.SYMMETRY_LINUX_V2_TEST_*`
  model/base-URL variables (`:111-115`). The `case_sets` array in the same step runs all fifteen
  sets and continues past a failing set.
- Read with the repository owner's `gh` identity on 2026-11-26 (names and metadata only; no secret
  value was printed, logged or committed):
  - the environment carries a required reviewer, `prevent_self_review: false`,
    `can_admins_bypass: true`, and a **custom** deployment branch policy naming exactly
    `main` and `codex/goal-0006b-crash-safe-process-containment`;
  - the two environment secrets above exist (name + updated-at read, e.g.
    `SYMMETRY_LINUX_V2_TEST_OPENAI_API_KEY` updated 2026-11-22);
  - the environment variables hold the same base URL and model ids the scratch pattern pins;
  - the workflow's run history is five dispatches, all on `main` (four failing, one cancelled by
    the operator), the newest one recorded as "No provider turn executed in any set"
    (`gdd/findings/acceptance-45573253632-summary.md`).
- So CI does have a real credential path, but it cannot bind this goal's branch: a PR head is not
  in the branch policy, and the ratified scratch pattern is deliberately dummy — the GDD contracts
  root records that with dummy credentials the provider turn must fail and only the case set's stop
  is recorded (`goal-rf-acc-native-hosted-run.md`, §3-4), a rule the later goals repeat. Under
  today's rules the scratch Bind can never turn this leg green, and no run on either path has ever
  produced a green authentic pi turn.

## 6. Diagnosability gap: the field that names the cause is dropped

`daemon/internal/harness/pi/rpc.go:748` retains the whole final assistant message, and `:848-861`
reads only `stopReason`; the provider's `errorMessage` never reaches the witness error. Rows A and
B above carry identical stop text for two different causes, and the retained Bind logs therefore
cannot discriminate them — a one-field omission, not a protocol problem: the message object the
validator already holds contains `"errorMessage":"401: …Invalid API key provided…"`. The package's
only existing row for a non-`stop` reason pins the `"aborted"` arm
(`daemon/internal/harness/pi/rpc_test.go:345-347`), so the changed arm needs its own red-first row.
This is production daemon code, so the contract routes it through the ruling (§7, D2).

## 7. DECISION_NEEDED — the ruling this stage requests

The contract's clause is met: this leg needs a real model credential that the goal's CI mechanism
cannot hand it (§5), so no side of the fix can be chosen silently. Options, each with its
feasibility evidence, cost and what it changes about what the leg proves:

**D1a — credential the acceptance for this goal's bytes.** Add the goal branch (or a staging
branch) to the environment's custom deployment branch policy, then either dispatch the real
workflow there (all fifteen sets, ~3 h — the 2026-11-25 credentialed run took 3 h 6 min and the
loop continues past failing sets) or push a scratch-shaped workflow that declares the same
environment and runs only `shared` (~4 min; Bind 1's acceptance job took 3 m 41 s). Feasible today:
the secrets, variables, self-approvable reviewer rule and admin bypass all exist; the `gh` identity
used for §5 is the repo owner's. Cost:
one owner/admin policy change plus real provider spend (precedent: the owner accepted pi-turn spend
for the durability legs). Effect: the only option that can make this leg green with an authentic
provider turn; the dummy-credential scratch red stays exactly as §1 records it.

**D1b — deterministic local Pi provider stub inside the guest.** Makes the leg advance and lets the
dummy-credential set complete, but replaces the authentic provider with a scripted one. Feasibility
obstacle measured here: `scripts/ci/linux-v2-native-acceptance.sh:60` admits only an `https://…`
base URL, so a plain-HTTP loopback stub cannot be routed through the env var, and pi's launch
environment cannot carry `NODE_EXTRA_CA_CERTS` (the profile's env allowlist is the hook-marker
variable alone, `:1106`), so the stub needs either a guest-trusted certificate or an explicit
fixture mode in the script's pi arm. The shape itself exists only as unit scaffolding
(`daemon/internal/harness/pi/adapter_native_work_test.go`'s loopback gateway), and repo doctrine
refuses synthetic provider evidence for a capability class
(`goal-rf-acc-opencode-reeval.md`: no dummy key, loopback gateway or synthetic SSE may fill a
class). Recommendation: usable only as an extra non-provider row, never as this leg's proof.

**D1c — an explicitly documented credential-gated leg.** The pi arm recognises the ratified
placeholder (or an equivalent explicit credential mode), records a named unproven outcome, and does
not abort the set; the authentic-turn proof then lives in D1a's row. Feasible in the owned files
(the script's pi arm and the e2e's expectation) with precedent: the owner's F2 decision
(2026-11-22) took a frozen leg out of a gate behind a fail-closed assertion. Cost: the dummy run
would no longer exercise the turn at all, so the gated outcome must stay visible in the witness
summary and the manifest, and no report may read the gated leg as passed. Effect: reachable
`stop_readback_reader` Result row and a passing export in the scratch Bind.

**D2 — surface pi's `errorMessage` in the witness stop** (independent of D1). Also a production
daemon change; its red-first row is §6's, and it removes the next cryptic stop of this class.

Recommendation: **D1a + D1c + D2**. D1a is the only authentic proof; D1c unblocks the
dummy-credential set's export and `stop_readback_reader` rows without claiming the turn passed; D2
makes any future provider failure self-describing. If D1a is chosen, the goal's acceptance clause
needs its own amendment, because the conductor's scratch Bind cannot carry a credential.

## 8. Gates

This increment adds three Markdown files; per the quality policy's documentation-only exemption the
platform suites are not path-triggered. The gates below ran on a scratch clone whose only commit
carries the final bytes of `docs/plans/2026-11-26-rf-acc-pi-authentic-turn-plan.md`,
`docs/evidence/rf-acc-pi-authentic-turn-2026-11-26.md` (this file) and the `docs/TODO.md` section,
against base `07be175e35088b4bbbaad469b796a227ad3784de`; the clone carries the repository's pinned
`node_modules/jscpd` through a link, and the pinned jscpd was never fetched.

| Command (working directory: the scratch clone root) | Exit | Row |
| --- | --- | --- |
| `python scripts/ci/check-record-cleanliness.py --base 07be175e35088b4bbbaad469b796a227ad3784de --head HEAD` | 0 | this record carries no host path; the gate audited the record this range adds |
| `python scripts/ci/check-citation-existence.py --base 07be175e35088b4bbbaad469b796a227ad3784de --head HEAD` | 0 | the range touches none of `docs/runbooks/**`, `docs/design/quality.md` or `docs/archive/CLOSED-TODO.md`, so the gate reports nothing to audit |
| `python scripts/ci/jscpd-changed.py --base 07be175e35088b4bbbaad469b796a227ad3784de --head HEAD` | 0 | no added line of the three changed files is part of a clone (the plan and this record share no copied block; the only verbatim spans are the quoted workflow env lines and the pi message rows) |

Not run, with reasons: the Elixir, Go, frontend, browser and wire-contract suites (no changed path
under `control/`, `daemon/`, `frontend/`, `browser/` or `contracts/`); the credential-gated native
acceptance (out of this stage; §5); `scripts/ci/test-linux-v2-owner-request-keys.py` and any mutant
row (no behavior changed, so no pin and no mutant apply).

## 9. Retained artifacts and commands

Probe scripts and raw outputs are retained outside the repository (relative to the GDD state root,
`scratch/authentic-turn/`); the goal worktree carries none of them:

| Artifact | sha256 |
| --- | --- |
| `probe-pi-win.py` (the rpc driver, §4.2) | `20e91f16081c719175008924e1f1fe08479760c34966e631fe8c93822ae89a92` |
| `rpc-litellm-dummy.stdout.jsonl` (row A) | `90bc0410a3ed5333c323ae0f59dd584823bf97c279f9d308a4fd0d4eb33fbc14` |
| `rpc-unreachable.stdout.jsonl` (row B) | `5ec66921da963f18831a507589f4c279322ef2f0925bc0ab458da38589cc1086` |
| `probe-pi-rpc.sh` (the Linux-blob attempt) | `95bb5ebda7f3cfb66edca7f2dbb01242c661e9695a9837851b413191357bee4d` |

The Linux-blob attempt could not start on this host: the archived executable needs a glibc runtime,
and the host's WSL distribution carries musl (`__pthread_key_create: symbol not found`), so the
release's Windows blob carried the measured row above while the Linux bytes contributed the code
excerpts. The scratch directory also holds `results.txt` with the two `curl` statuses and the two
`message_end` rows as printed.

Input digests the rows ran on: the pi Linux archive `811c6f2c…` (equal to
`scripts/ci/linux-v2-native-tools.sh:44`), the pi Windows archive `e6adb8cb…` (equal to the GitHub
release asset digest), the recovered scratch workflow `e3f526a2…`, and the run's own edited copy
`bb6dcaf5…` (both from `gdd/findings/303-binding-run.md` §1). Nothing in this record is quoted from
a secret; §5 names environment-variable and secret names only, and no secret value was read,
printed or stored by this stage.
