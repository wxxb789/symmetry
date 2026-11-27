# rf-acc-pi-authentic-turn — plan (stage 1 delivered; stage 2 awaits the ruling)

Contract: the goal file under the GDD contracts root (`goal-rf-acc-pi-authentic-turn.md`), ruled by
the conductor 2026-11-26; dispatch gate #303. Branch `refactor/rf-acc-pi-authentic-turn`, base and
dispatch head `07be175e35088b4bbbaad469b796a227ad3784de`. Every `file:line` citation below binds
that revision. Shape reference: `docs/evidence/rf-acc-pi-provider-admission-2026-11-26.md` (the
merged predecessor goal) and `docs/evidence/rf-acc-migration-pi-hold-2026-11-26.md` (a stage-1
root-cause record). Measurements: `docs/evidence/rf-acc-pi-authentic-turn-2026-11-26.md`.

## 1. What this increment is

Change 1 of the contract (stage 1, root cause only) is delivered as measured evidence. Change 2
("fix on the side the ruling names") has no named side: the ruling is written after stage 1, and
the contract's own escape clause — a leg that needs a real model credential CI cannot hand it
returns `DECISION_NEEDED` with options — is the answer this leg gets today. So this increment
changes no production, fixture or Control byte; it delivers the root cause, the discriminated
candidate table, the option space with feasibility, and the ruling request (§4, record §7-§8).

## 2. Root cause, in one chain

The authentic pi turn authenticates with the run's `SYMMETRY_OPENAI_API_KEY`. The Bind harness is
the ratified scratch pattern: push-triggered, no `environment:` block, inline dummy pins including
`SYMMETRY_OPENAI_API_KEY=sk-scratch-dummy` (recovered scratch workflow bytes, sha256
`e3f526a2c06a4079a4e6b4e2ce4240574597fe593b00c5c199f7868871e70e8f`). The fixture writes that
string into B's private home as pi's `auth.json` key
(`scripts/ci/linux-v2-native-acceptance.sh:393,1218-1226`) and points `providers.openai` at the
real LiteLLM base URL (`:1225-1249`). The provider answers HTTP 401 for the placeholder key, and pi
1.5.2 turns a provider failure into the final assistant message `stopReason:"error"`, which
`daemon/internal/harness/pi/rpc.go:848-861` reports as `ErrUnexpectedAssistantStop` — the stop the
Bind logs. The leg is not broken; no run has ever handed it a real credential.

## 3. Candidate discrimination

| Candidate the contract lists | Verdict (evidence in record §3-§6) |
| --- | --- |
| missing or dummy provider credentials | root cause; three measured rows agree (401 answer, pi's message, the daemon's stop) |
| network or egress denial inside containment | no mechanism exists: the guest keeps the host netns (`:975-979`), the gate unshares only `CLONE_NEWUSER` (`daemon/native/linux-gate/c-gate/gate.c:1414`), and no unit or fixture sets an address filter |
| model or provider config | accepted by pi 1.5.2 on the fixture's exact bytes: `get_state` resolves the configured base URL, model, api and thinking level |
| tool-call policy | never reached: the failing turn has empty assistant content, no tool call and no tool results |

## 4. The decision this stage asks for

D1, which side fixes the leg (full rows in record §8):

- (a) credential the acceptance for this goal's bytes: an owner/admin adds the goal branch to the
  `linux-v2-native-acceptance` environment's custom deployment branch policy (today it admits
  `main` and `codex/goal-0006b-crash-safe-process-containment` only) and the run is dispatched, or
  a scratch-shaped workflow declaring that environment is pushed to an allowed branch. Only this
  option can make the leg green with an authentic provider turn, and it matches the repo's
  "credentialed run is the proof of record" doctrine for pi-turn legs.
- (b) a deterministic local Pi provider stub inside the guest. It advances the leg, but the
  provider turn becomes scripted, and `scripts/ci/linux-v2-native-acceptance.sh:60` admits only an
  `https://…` base URL, so a plain-HTTP loopback stub needs either a guest-trusted certificate
  (pi's launch environment cannot carry `NODE_EXTRA_CA_CERTS`) or an explicit fixture mode.
  Recommended only as an extra non-provider row, never as the provider proof.
- (c) an explicitly documented credential-gated leg: the pi arm recognises the ratified placeholder
  and records a named unproven outcome instead of aborting the set, so the dummy-credential `shared`
  set reaches `stop_readback_reader` and the export. Precedent: the owner's F2 decision
  (2026-11-22) excluded a frozen leg from a gate behind a fail-closed assertion.

D2, independent of D1: surface pi's `errorMessage` in the witness stop. The validator already holds
the full final message (`daemon/internal/harness/pi/rpc.go:748,820-828`), so today's `"error"`
cannot be told from a transport failure; both shapes were measured. Production daemon code, so it
needs the ruling.

Recommendation: D1 = (a) + (c) — (a) proves the authentic turn, (c) unblocks the dummy-credential
set and its `stop_readback_reader`/export rows without ever reporting the gated leg as passed;
D2 = yes. If (a) is chosen, the acceptance clause itself needs the amendment, because the scratch
Bind cannot carry a credential.

## 5. Evidence plan for the next stage

After the ruling: implement the named side in the owned files, keep
`scripts/ci/test-linux-v2-owner-request-keys.py` and the `daemon/e2e` compile gates green, capture
the red-first row for whichever arm changes (a placeholder-credential probe for (c); a stub
transcript for (b); for (a) the credentialed run's own rows), record the exact Bind/run ids, and
re-bind the evidence record's rows to the delivered bytes.

## 6. Out of scope, with reasons

- Production daemon code, including the `daemon/internal/harness/pi/rpc.go` error text of D2: the
  contract routes it to `DECISION_NEEDED`.
- Workflow secrets and the environment's branch policy: owner territory, and no new credential may
  enter CI by a worker decision.
- Control bytes: unchanged; nothing here needs them.
- A scratch run of my own: the acceptance clause names the conductor's Bind, and this stage must
  not push.
- `docs/archive/CLOSED-TODO.md`: a stage-1 increment closes no item, so the file is untouched and
  the archived-closure citation gate stays out of this range.
