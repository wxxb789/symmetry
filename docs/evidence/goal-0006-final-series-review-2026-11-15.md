# Goal 0006-final: durable engineering work series review — 2026-11-15

Status: **review recording closed; full-series acceptance incomplete.** This is
the documentation-only review of the unchanged [full-series contract][final],
not an implementation, capability promotion, release certificate or new
`achieve`. The [0006i receipt][release] owns release validation and the recorded
authenticated acceptance. Its later achieve record supersedes its earlier
pending-operator wording; its hosted-attempt section supersedes pending dispatch.
Neither update proves the missing native, accounting or recovery predicates.

The contract says missing or stale evidence keeps the goal incomplete and that
the review runs after the lettered goals. The requested archive/index closure is
therefore **administrative recording closure only**: 0006k has no closure record,
and the unmet requirements below remain unmet. This receipt returns findings to
their responsible goals without repeating those implementations or changing any
acceptance condition. A model cannot convert the local achieved Goal into
acceptance of the entire series.

**Annotation (recorded 2026-11-20):** "0006k has no closure record" above held at recording
time; 0006k was administratively archived 2026-11-17 (#55 `e72290a`), so
`docs/goals/archive/0006k-omo-native-capability.md` exists and the goal index carries its
closure paragraph. The sentence above is kept as recorded.

## Subject record

| Field | Re-derived value / provenance | Validity boundary |
| --- | --- | --- |
| Reviewed release commit | `8ded77150ec0ae49d52355670fdad78a348bf1d5` | Frozen Subject; not this documentation increment's HEAD. |
| Git tree OID | `3a1dec551b0e2c630f6720d8a542343648270793` | `git rev-parse 8ded77150ec0ae49d52355670fdad78a348bf1d5^{tree}`, exit 0. |
| Protocol `tree_digest` | `sha256:fafc72dc79a081c0e23bf08d812462f33b6c36cc247d580eab643ee5f215de9c` | Recomputed from 1,091 tracked blobs using Git objects and the algorithm in [the source binder][binder], exit 0; not the Git tree OID. |
| Reviewed implementation head | `109f6037a34ad340219fa6c07c3db4145e2377a1` | Its `^{tree}` independently resolves to the same Git tree OID, exit 0. |
| Merge provenance | [PR #22][pr22], squash merge at `2026-11-14T19:46:57Z` | GitHub API confirms merge commit `8ded771…`, exit 0. |
| Documentation baseline | `89bd8a0662b214c25122bce8e16a0f8c2fd69b5c`, branch `feat/0006-final-series-review` | `git rev-parse HEAD`, `git branch --show-current`, exit 0; initial `git status --porcelain` empty, exit 0. |
| Post-Subject changes before this review | One new 0006i receipt and plan, the final contract's 0006i link and goal-index edits, unchanged 0006i archive move | `git diff --name-status 8ded77150ec0ae49d52355670fdad78a348bf1d5 HEAD`, exit 0; documentation only, **not** tree-identical to the release. The two incoming final-contract references changed in this increment are listed below, not attributed to this baseline delta. |
| Local acceptance Subject | resource `eb968617-36be-48ce-8321-b6b20f7ce77f`, same release commit and protocol digest; `subject_hash` `sha256:db8f19f427cdf77b8038e9042bc5091bf782a7f97b820924f195f9517ec2e7b4` | Recorded by [0006i, Achieve record][release]; local acceptance repository identity, not a production repository identity. |

The digest computation sorted `git ls-tree --full-tree -r -z <commit>` entries by
raw path, read each blob with `git cat-file --batch`, hashed its contents, then
hashed `mode NUL kind NUL path NUL sha256:<blob-digest> NUL` for each entry, as
[the binder][binder] specifies. It read the frozen commit from the object store;
it did **not** run the binder's clean-HEAD check against the newer documentation
checkout or claim that checkout had the frozen tree.

### Evidence validity rules used in this review

- **CI-current:** execution on `8ded771…`, or on the independently verified
  tree-identical `109f6037…`; valid for the exercised path only. A successful
  aggregate job does not turn its opt-in skips into native evidence.
- **Pinned characterization:** committed exact-binary fixtures and native
  receipts remain useful for their recorded version, platform, tool profile and
  fixture Subject. Being committed *in* the release tree does not mean they
  executed *against* its release Subject. Fixture observations are not silently
  rebound to the release repository resource or subject hash.
- **Recorded local acceptance:** the IDs in [0006i][release] attest that local
  experiment, not every series predicate. This review did not query its database
  or recover its private logs; those facts are repository-recorded, not freshly
  re-executed or independently authenticated here.
- **Historical / unavailable:** older dirty-tree receipts, absent private roots,
  skipped native tests and unrun witness scripts cannot satisfy a current
  acceptance predicate. Fixed design documents describe requirements, not proof.

## CI and independent implementation review

GitHub API run metadata and job conclusions were read again during this review;
no suites or workflows were dispatched. Each API request below exited 0.

| Recorded run | Rechecked revision | Rechecked result | What it does not prove |
| --- | --- | --- | --- |
| [43633014138][ci-pr] | `109f6037…`, tree-identical PR #22 head | 12 jobs: 11 success, 1 skipped (`linux-native-v2-acceptance`, 0 steps). | No installed-owner native-v2 qualification. |
| [43633946120][ci-subject] | `8ded771…`, push | 12 jobs: 11 success, 1 skipped (same job, 0 steps). | Same native gap; no new native run inferred from a green aggregate. |
| [43673581776][ci-native] | `8ded771…`, manual dispatch | 12 jobs: 11 success, native-v2 job failure. | The host prerequisite failed; the 12 native case sets did not pass. |

The [0006i receipt][release] records the passing-job detail: contract tests
31 passed / 0 failed; Control 826 passed / 21 skipped / 0 failed, provider broker
E2E 4 passed; Linux Go ordinary and race suites each 24 test packages; Windows
22 package entries including the no-test sentinel; Windows Pi E2E 5 passed /
3 excluded. These are **cited recorded counts**, not local test executions or
fresh log recounts. Builds, production Control release, compose HTTP/HTTPS,
restart witnesses and legacy-route audit are covered there. Skips/exclusions
remain unavailable proof for their respective paths.

[Codex review comment 6883104913][codex-review] was re-read through the API,
exit 0: `status=completed`, `has_any_findings=false`,
`has_unresolved_findings=false`, `commit_sha=109f6037…`. It is independent
implementation-head review, not review of this receipt or operator acceptance.

## Full-series requirement matrix

Every row names the requirement, evidence and its validity for the frozen
Subject. Test references identify executable witnesses already present in the
Subject tree; execution scope comes from [the CI record][release], not merely
from test-file existence. The native table and explicit unproved list are part
of this matrix, not exceptions to the contract.

| Requirement cluster | Evidence | Current and valid for the reviewed Subject? |
| --- | --- | --- |
| Durable Goal/Task/Run/lease/decision/recovery transitions across sessions and interruption | [Core ownership][core], [data transactions][data]; [Goals tests][goals-tests] (`creation and command receipts replay before stale preconditions`, `terminal producer, validation evidence, accepted outcome, and achieve close the Goal loop`); [Orchestration tests][orchestration-tests]; Subject-tree Control/daemon/integration jobs. | **Yes for exercised kernel transitions and restart witnesses.** Not proof of every native crash boundary or power-loss durability. |
| Approved intent, useful context and preserved artifacts survive compatible resume/handoff | [Context/handoff contract][protocol], [continuity receipt][continuity]; [admission tests][admission-tests] (`Goal resume assignment is pinned to its retained compatible runtime`, `Goal resume with no viable retained runtime remains queued without fallback`); [OmO pinned receipts][omo]. | **Partial.** Current CI supports admission/fencing; continuity receipt is historical local evidence, OmO receipts use a fixture Subject. Retained native support is only as listed below; no cross-machine or proprietary-format portability claim. |
| Reject stale authority and stale generations | [Orchestration tests][orchestration-tests], `fences reject stale epoch, generation, and lease`; [Goals tests][goals-tests], `an amended Goal rejects post-claim session attachment and context retrieval`; [admission tests][admission-tests], frozen runtime and validation binding checks. | **Yes for deterministic stale-fence/revision paths in Subject CI.** Qualified Linux-v2 stale-leaf/journal matrices remain unproved. |
| Stop for consequential decisions; resume only after scoped resolution | [Core governance][core], [typed Decisions][contracts]; [Goals tests][goals-tests], dependency scope/action decisions and `Goal resume waits for an old amendment cancellation to settle`; [admission tests][admission-tests], pause/resume and superseded-control checks; local plan/completion decision IDs below. | **Yes for scheduling/authority paths; bounded local acceptance recorded.** Scheduling pause is not native process suspension; unsupported native pause/approval bridges remain unsupported. |
| Run success, accepted work and achieved goal remain distinct | [Goals tests][goals-tests], `a completed Run is not accepted work and a producer cannot validate itself`, complete/incomplete validation and achieve-loop tests; [0006i achieved Goal record][release]. | **Yes for kernel separation and the recorded local Goal.** That one achieved Goal is not full-series or production acceptance. |
| Duplicate/replayed delivery is not new work | [Goals tests][goals-tests], receipt replay before stale preconditions and replay after Goal lock; [admission tests][admission-tests], exact claim replay; [delivery tests][delivery-tests], attachment/stop outboxes. | **Yes for current deterministic receipt/outbox paths.** Installed-owner native-v2 delivery matrices did not run successfully. |
| Lost acknowledgements preserve original identity and do not duplicate execution | [admission tests][admission-tests], `lost claim acknowledgement replays after a runtime affinity change`; [delivery tests][delivery-tests], `TestAttachReplayBarrierPreservesStoppedNativeSessionUntilMappingRecovers`; Subject-tree Windows lost-recover-response and Linux containment replay witnesses cited by [0006i][release]. | **Yes within those paths.** OmO direct discarded-ACK fixtures are characterization, not credentialed Control crash-recovery proof. |
| Invalid dependencies cannot progress | [Goals tests][goals-tests], `dependency changes require a scope decision bound to its operation and target`, `same Goal dependency commands retain an acyclic DAG across inverse additions`; [data same-goal DAG constraints][data]. | **Yes for Subject-tree Control tests.** Board state or a model claim is not dependency acceptance. |
| Outdated or oversized mandatory context blocks admission | [Goals tests][goals-tests], `automatic context admission rejection commits only its blocker receipt`, `context manifest rejects missing sources and mandatory budget overflow before insertion`, current-fence context retrieval checks; [protocol context rules][protocol]. | **Yes for tested admission/snapshot checks.** Historical context is not silently refreshed into authority. |
| Mismatched validation cannot accept a result | [Goals tests][goals-tests], `failed validation evidence cannot reject a candidate that differs from the frozen producer Subject`, incomplete evidence-set test; [admission tests][admission-tests], `validation binding mismatch blocks a new claim but preserves an exact claim replay`. | **Yes for Subject CI.** A wrong Subject remains wrong even when the producing Run succeeded. |
| Unknown external effects are distinguished from confirmed effects | [0006h archived contract][provider-goal] and its [README closure][goal-index]; [provider witness][provider-witness] and current broker tests under Control CI; [delivery tests][delivery-tests], `TestRecoveryClosedGoalSessionWritesUnknownOutcomeForNonterminalRun`. | **Partial.** Live GitHub timeout/readback is recorded historically; no credentialed production-provider effect proof bound to this release Subject. Unsupported external checkers remain `unsupported_external_check`, not an observe-task fallback ([contracts][contracts]). |
| Budget exhaustion is a blocker, not valid progression | [admission tests][admission-tests], `strict-budget Goal assignment and new claim require native hard cost limit support`; [Goals tests][goals-tests], unknown-accounting/strict-resume and reservation tests; [delivery tests][delivery-tests], `TestNativeGoalUsageRetryExhaustionBlocksRenewalAndRetainsEvidence`. | **Yes for deterministic accounting/admission checks.** No billable-provider hard-cap or zero-overshoot claim. |
| Recovery does not re-accept results or repeat confirmed effects | [Goals tests][goals-tests], `the global wakeup recovers a terminal settlement for a paused Goal exactly once`; [delivery tests][delivery-tests], stopped delivery/released-session/projection-first crash recovery; [0006h closure][goal-index], cached terminal result and unknown-effect readback. | **Yes for current kernel/outbox tests; partial for live effects.** Historical provider witness and native adapter restarts do not establish all production recovery paths. |
| Failed/cancelled/rejected work's actual usage remains accounted | [Goals tests][goals-tests], `late usage remains fenced to the original machine and replays by exact usage key`, effective correction leaves and terminal unknown accounting; [settlement regressions][usage-tests], `TestCancelledNativeStartCloseFailureQueuesExplicitUsageBeforeTerminal`, `TestNativeUsageRetryExhaustionKeepsAtomicCancelOutcomeAndOutputEvidence`. | **Yes for ingestion, fencing, reservation and unknown-value behavior in Subject CI.** Actual billable amounts remain unverified, never substituted with zero. |
| Completion traces exact artifacts, required checks and authorized acceptance; blocked/continuing states have reasons | [Acceptance contract][contracts], [Goals tests][goals-tests], validation/achieve tests, [0006i evidence/outcome/Decision/event IDs][release]; [core derived blockers][core]. | **Partial at full-series level.** Local exact-Subject artifact acceptance is recorded below; hosted native, provider and omitted recovery evidence prevent series acceptance. |
| Goal-less workflows and protocol v1 survive additive migration | [admission tests][admission-tests], `legacy submit_task preserves its request hash and creates no Goal settlement`, legacy metadata replay and no Goal bypass; [migration tests][migration-tests]; Subject-tree legacy-route-audit, Control and integration jobs; [protocol additive migration][protocol]. | **Yes for exercised compatibility/migration paths.** Fake-agent/generic compatibility is not native capability evidence. |
| Existing authentication and operator/machine separation remain binding | [Goal machine tests][machine-tests], `foreign machines cannot read or mutate a Goal run, and context is sanitized`; [Goal operator tests][operator-tests], `portal routes share the Goal command implementation and retain portal authentication`; [protocol controller tests][protocol-tests], `auth class and machine ownership distinguish unauthenticated from forbidden`. | **Yes for the tested Subject-tree Control paths.** Foreign-machine and wrong-auth-class refusal are executable checks, not inferred from the design. |
| Provider-owned presentation remains provider-owned without becoming acceptance authority | [Integration tests][integration-tests], `duplicate provider work items select the freshest item independent of page order`, checks stored provider title/revision; [migration tests][migration-tests], `freezes resources bound by any runtime while permitting presentation updates and safe rollback`; [core Workspaces boundary][core]. | **Partial.** Current tests support provider snapshot/presentation behavior and immutable resource identity. No end-to-end provider-import-versus-approved-Goal-acceptance receipt was identified in these cited witnesses; do not extend their coverage to that unproved combination. |
| PostgreSQL is the only durable control authority; LoopX contributes concepts only | [Core ownership][core], [data constraints/transactions][data], [migration tests][migration-tests] and Subject-tree Control/restart jobs; existing Oban wake/settlement evidence in [0006i][release]. | **Yes at the tested persistence/transaction level.** Notifications and local native journals are not Goal truth; no second scheduler is authorized. Production backup/restore remains unproved. |
| No native-session-format portability promise or excluded scope expansion | [Harness boundary][harness], [core governance][core], [design decisions][design]. macOS daemon, general workflow DSL, multi-tenant RBAC and autonomous unapproved expansion remain excluded. | **Valid scope constraints, not new runtime evidence.** This increment changes documentation only and grants no new operation. |

## Native capability matrix: exact pins, platforms and evidence ranges

These are the lettered goals' recorded pins, not a fresh upstream latest-version
survey. Unknown versions/bytes stay unsupported per [the harness contract][harness].
No pin has been advanced by this review. A Windows result is never Linux proof.

| Kind / pin | Recorded tested capability range | Evidence and current-Subject validity | Unproved / unsupported range |
| --- | --- | --- | --- |
| Codex `0.168.0` | Windows exact-binary start, events, cancel and repository result/artifact. | [Pinned Codex evidence][codex]; Subject-tree daemon regression suites are current. Native capture is pinned characterization, not a new release-Subject run. | Linux native admission fail-closed; retained resume, handoff, steer, safe pause, approval response, provider broker, billable usage and hard cost limits not promoted. |
| Claude Code `2.1.323` | Windows exact-binary start/events/cancel, structured result/artifact; fixed `--permission-mode acceptEdits`, deferred matching `system/init`. | [Pinned Claude evidence][claude]; current daemon suites validate the implemented bounds. Native permission observations are tool-specific characterization, not a sandbox or Linux evidence. | Linux fail-closed; resume/handoff/guidance/pause/approval bridge/provider broker/billable usage/hard cost limits unsupported. |
| Pi `1.5.2` | Windows exact-binary start/events/cancel and adapter-level retained resume. | [Pinned Pi evidence][pi], [0006f closure][goal-index]; Subject-tree Windows Pi E2E recorded 5 passed / 3 excluded; Linux job proves legacy provider-bridge refusal, not positive native support. | Linux native-v2 positive evidence missing; handoff, guidance, pause, approvals, provider access, billable usage and hard cost limits unsupported. |
| OMP `18.20.0` | **No operational capability promoted on either OS.** Separate `omp-rpc-v2` transport, not inherited Pi capability. | [0006j receipt][omp] contains both the initial zero-spend blocker and later authorized native observations; [probe][omp-probe] still returns `ErrNativeUnverified` and unsupported operational capabilities at the frozen Subject. Current suites prove fail-closed behavior, not positive Goal admission. | Zero-spend is the recorded original blocker, **not a valid blanket explanation that no corrected-key generation ever ran**; see conflict F3 below. Linux in-flight physical-stop proof remains unproved (`observed_escape`). No provider/billing/hard-limit promotion. |
| OmO `5.1.69` | Windows and WSL Ubuntu-24.04 exact-binary start/events/cancel/retained resume/**same-machine** fresh handoff with only `--tools read,write`. | [Committed native and public-adapter receipts][omo]; both adapter receipts bind fixture commit `6adc9ef1a2f704590ee4172cecb4e0bb6c70408c`, resource `00000000-0000-4000-8000-00000000006b`, hash `sha256:927cb2355fb4d9179dfb6b84bb24d467808299fdbe4dac8a799178ee00490cb8`, **not** the release Subject. The [0006i producer Run][release] separately records real native work on the release Subject. | Credentialed daemon/Control recovery witness scripts exist, but their execution is not recorded. No cross-machine handoff, shell/delegation tools, native steer/pause/approval bridge/provider broker/billable usage/hard cap; rule-driven interventions unverified. |
| OpenCode V2 `2.3.0` | **Unsupported by decision**; production refuses Goal and legacy assignments before workspace/process effects. V1.x skipped, not maintained as supported. | [0006g decision][opencode-goal], [2.3.0 observations][opencode]; current Linux legacy-refusal and Windows production-unsupported CI jobs prove refusal. | No V2 capability promotion. Windows PE via WSL interop is not native Linux evidence; missing continuation/failure/truncation/usage/retained recovery classes remain missing. |

The six-kind repository-work requirement is therefore **not fully demonstrated**:
three kinds have Windows-only promotion, OMP has none, and OpenCode V2 is
unsupported. Visible unsupported operations satisfy the refusal boundary, not
the missing positive-work requirement of [0006-final][final]. OmO's restricted
Linux profile uses the ordinary Runner owner; it does not qualify the distinct
Pi/OpenCode Linux-v2 A/B owner path ([harness][harness]).

## Recorded authorized acceptance and product friction

[0006i, Achieve record][release] records one local revision-1 Goal
`0b1c2512-5feb-48b2-bb01-55e80cda81a8`, state `achieved`, sequence 12:

- Required integration WorkItem `c32d69f1-56cd-4348-80d5-e4d56f79db8e`;
  plan Decision `b45435c8-41bd-4dcf-91f3-9aaaef04cd84` (resolved/accept).
- Real native OmO producer Task `b3b6fa0d-2e36-4020-a3a6-ba57afa2b20c`,
  Run `332e8267-d3fd-40b9-beab-e71b041947af`, runtime `accept-omo`,
  `omo 5.1.69 / omo-rpc-v1`, 329 recorded events.
- Deterministic artifact validator Task `1934e7d1-171a-4472-b732-288bc53347f1`,
  Run `9af2376a-5207-465f-b674-a308528e7dc6`; passed Evidence
  `bcbbbff1-6ec5-4009-a983-841a404d3c92`, key
  `artifact:c7c5c1d70c5dec4416ab6158afd0b223ef40c29b1dc1f97ed9428b94d4cadb1c`,
  bound to the exact release Subject/hash above.
- Accepted outcome `01f9166f-80e4-4777-813e-26cd11917cf4`, reason
  `validation_passed`; completion Decision
  `8a37fb14-f921-4d8a-afdd-a8bf4a8de552` (resolved/accept).
- `achieve` HTTP 200, event `7a9c86ee-d600-4b2f-b9c7-a56257617a97`;
  same-`mutation_id` replay returned the same receipt without another event.

These recorded IDs establish traceability; their private database/log payloads
were not independently queried here. The Goal checks the predicates it actually
configured; it does not prove all requirements in this series review. No new
`achieve`, authority resolution or provider operation was issued in this increment.

The same committed [operator runbook observations][release] record three product
friction points, retained as limits rather than silently repaired:

1. Project `default_workspace` must equal the runtime's workspace binding name;
   inherited plan items and assignment require equality.
2. OmO reads `$HOME/.omo/agent/models.json`, not `$HOME/agent/…`; the isolated
   config references `$SYMMETRY_OMO_PROBE_KEY`, never its value.
3. The producing turn must avoid interactive/question tools; the OmO validator
   accepts only `notify/setStatus/setWidget/setTitle/set_editor_text`. The daemon
   launches the harness with `--tools read,write`, so that turn cannot compute
   canonical `subject_hash` itself; the task must supply the exact contract values.

## Explicit unproved requirements, conflicts and responsible goals

| ID / owner | Finding, evidence and disposition |
| --- | --- |
| F1 — 0006b/0006i | **Hosted `linux-native-v2-acceptance` is environment-limited, not passed.** [0006i hosted attempts][release]: run `43641770027` was rejected before steps by environment branch policy; [43673581776][ci-native] reached the host probe and failed. Artifact [14145314081][probe-artifact], member `linux-v2-host-prerequisite-43673581776-1.json`, was downloaded/read in memory here: `source_commit=8ded771…`, `systemd_is_pid1=true`, `unified_cgroup_v2_mounted=false`, `failure_stage=host_systemd_cgroup_v2_precondition`. The committed receipt separately records the local WSL2 Subject-bound probe `path-sha256:97206ac5d9cb2031277197f74084da7dc002fdf043a0cfecd8af4b429a5801ec`: cgroup-v2 mounted, but `failure_stage=transient_unit_cleanup` because the stopped transient unit's cgroup disappears rather than remaining readable as `populated 0`. That local file is not committed/available here; its contents and manual reproduction are **recorded, not independently reverified**. The hosted probe is direct evidence for that runner, not every possible GitHub host. No qualified host or passing A/B matrix is established. |
| F2 — 0006h/0006i | **Billable provider accounting is unverified.** The owner ruling is in [the archived 0006h closure paragraph in README][goal-index], not inside the [archived contract][provider-goal]: a provider exposing no monetary surface satisfies the explicitly-unavailable branch. `cost_microusd: null`, `zero_or_not_billed_established: false`; [the provider witness's money record][provider-witness] agrees. Azure DevOps effects were not run. Historical credentialed GitHub readback/replay does not prove production provider identity/billing for this Subject. Provider snapshot/presentation tests in the matrix also do not establish the combined import-versus-approved-Goal-acceptance path; that evidence remains unproved here. |
| F3 — 0006j/0006i | **OMP evidence conflicts with its zero-spend summary.** [0006j][omp] initially records zero spend and no corrected-key generation; its later `Operator audit, spend authorization, and native acceptance receipts` section records owner authorization, a US$2 self-cap, Windows acceptance (28.44 s, repaired rerun 31.02 s) and Linux partial acceptance (restart/lost ACK, retained resume, handoff; cancellation unproved). [The compiled witness ceiling][omp-spend] is `2_000_000` microusd at the Subject, independently inspected. Private roots/receipt IDs `windows-omp-native-4078422222`, `windows-omp-native-3859809959`, `linux-omp-native-4172995585`, `linux-omp-native-585780460` are cited in the committed report, not available as raw acceptance receipts here. Thus later observations exist, but their complete exact-source binding cannot be reproduced from those absent roots. [0006i][release] and [harness][harness] still describe the zero-spend blocker as current; that explanation is stale in part. **OMP remains unpromoted** by [the Subject's probe][omp-probe], and Linux `observed_escape` remains unresolved. Record the contradiction; do not erase observations, promote support or amend the contract. |
| F4 — 0006k | **No closure paragraph or archived goal file.** At documentation baseline `89bd8a0`, tracked archive inventory and a separate README paragraph search both found no 0006k closure; the [active contract][omo-goal] still exists, and no `docs/evidence/goal-0006k-*` receipt was found in the evidence inventory. Evidence that does exist: [PR #22][pr22]; its operator-variant review (189 turns, exit 0, `Ready with fixes`, one P1 F1); commit `109f6037a34ad340219fa6c07c3db4145e2377a1` repairs the four legacy-refusal sites, while `8c98426f9beac5c52de84c98f2ba26cf5e31ee30` repairs migration/simplification findings. PR body records red-before/green-after and a post-rebase refusal check. Current source/CI and [Codex review][codex-review] cover the final tree, but are not a missing closure receipt. The PR body says its operator review examined `400d182d..42325e5`; the post-rebase CI/review binds `109f6037…`. Credentialed Control/daemon witness execution remains unrecorded ([OmO receipt boundary][omo]). |
| F5 — 0006d/0006e/0006f/0006g | **No all-six-kind, two-platform positive release-native matrix.** The native table lists exact promoted ranges. Windows Codex/Claude/Pi evidence cannot establish Linux support; OpenCode refusal is not successful native repository work. The unavailable environment remains incomplete, not fake-agent success. |
| F6 — 0006c/0006k/0006i | **Not every native recovery boundary is proved.** Historical [0006c][continuity] is not exact-Subject acceptance. [OmO adapter receipts][omo] explicitly limit journals to flush/reopen, not power loss, Control admission/stop certificate or daemon crash. Committed `omo-control-native-evidence.py`/fixture scripts are instructions, not execution receipts. Active-turn native reattachment after daemon restart and cross-machine handoff remain unsupported ([harness][harness]). No repeat acceptance or effect replay is inferred. |
| F7 — 0006i | **Production backup/restore is not proved.** [0006i][release] records migration, Control restart, daemon crash/reconnect and deployable artifacts, but explicitly no hosted `pg_dump`/`pg_restore` disaster-recovery evidence. Restart is not database restore. Local repository identity is not production identity. No release marker is invented; the existing recorded convention is pre-0.0.1. |
| F8 — 0006-final documentation | **Recording closure does not satisfy the completion contract.** Missing/stale evidence above keeps full-series acceptance incomplete. Existing [0006i][release] pending matrix/header/README text conflicts with its later achieve/hosted-attempt addenda; this receipt uses the later, narrower facts without rewriting that historical record. The final contract is archived byte-for-byte, so its five relative Markdown targets retain their original `docs/goals/` base, not their new archive-directory base. Canonical targets are the [0006i contract][0006i-goal], [core][core], [data][data], [protocol][protocol] and [harness][harness]; no forwarding contract or acceptance rewrite is introduced. The pre-existing 0006h table link in [the index][goal-index] also still targets its former active path; it is not evidence that the archived contract is missing. **Repaired (2026-11-18, #66 `c514bd9`):** the index's 0006h table link now targets `archive/0006h-credentialed-provider-accounting.md` and resolves; the sentence above is kept as the recording-time state. |

Other recorded limits: [0006i][release] lists existing Control test unused-variable
warnings and the unresolved OMP descendant-monitor stop gap; [docs/TODO.md][todo]
tracks the existing maintenance candidates (its historical line numbers are not
new proof). [0006c][continuity] records the retained-session path-replacement
window: point-in-time path validation is not atomic exclusion of a same-principal
writer. This review establishes no fix or new exploit evidence for those items.

**Annotation (recorded 2026-11-20):** the tracking pointer above no longer holds for either
named candidate: the Control test unused-variable warnings item closed to
`docs/archive/CLOSED-TODO.md` on 2026-11-19, and the OMP descendant-monitor stop gap is
recorded only in the [0006j receipt][omp] — it has no `docs/TODO.md` entry. The sentence above
is kept as recorded; the underlying limits are unchanged.

## Review method, commands and scope

Shape reference: documentation commit
`dd5facf60d8f6ffee43c06f3bd821e6ff878fb7f` (PR #23), [0006i receipt][release],
its unchanged goal archive/index closure and recorded CI. No implementation
plan is needed: no executable package, migration, protocol or acceptance change
is made. No measurement, benchmark, baseline or harness manifest is changed;
no test suite or measurement ran, concurrently or otherwise.

| Command / inspection actually executed in this review | Result / exit |
| --- | --- |
| `git status --porcelain`; `git branch --show-current`; `git rev-parse HEAD` (before edits) | Empty initial status; branch and documentation baseline above; each exit 0. Not a post-edit clean-tree claim. |
| `git rev-parse <release>^{tree}` and `git rev-parse <reviewed-head>^{tree}` | Both tree OIDs equal `3a1dec55…`; each exit 0. |
| Inline Python Git-object protocol digest derivation, matching [binder][binder] | 1,091 blobs, exact protocol digest above, exit 0. No checkout or ref mutation. |
| `git diff --name-status <release> HEAD`; `git show --stat --oneline dd5facf…` | Prior documentation-only delta and shape reference, exit 0. |
| `git show <release>:<path>` comparisons | OMP probe, spend witness, 0006j receipt and harness design each byte-identical to the inspected files at the release; exit 0. README differs and 0006i receipt is post-Subject documentation, explicitly not treated as source-tree equivalence. |
| `gh api repos/wxxb789/symmetry/actions/runs/{43633014138,43633946120,43673581776}` and each `/jobs?per_page=100` | 6 requests, each exit 0; counted job conclusions above. |
| `gh api repos/wxxb789/symmetry/actions/artifacts/14145314081` and `/zip` | 2 requests, each exit 0; metadata binds `8ded771…`; one JSON member decoded in memory and reported F1. Artifact ZIP size 611 bytes; not confused with the receipt's 717-byte uncompressed JSON. |
| `gh api repos/wxxb789/symmetry/pulls/22`; `gh pr view 22 --repo wxxb789/symmetry --json body,comments,reviews,commits`; `gh api …/issues/comments/6883104913` | Each exit 0; merge, operator-variant/fix provenance and Codex state above. No new review/PR mutation. |
| Archive inventory and README closure search, plus direct directory/paragraph inspection | 0 archived 0006k goal files, 0 0006k closure paragraphs. No claim that its implementation/evidence is absent. |

No configured Markdown formatter or linter was found among tracked configuration
paths or the inspected root package scripts/CI commands. `prettier@3.9.10` exists
as a transitive lockfile dependency, not a configured documentation gate. Do not
run unrelated Elixir/Go formatters on Markdown or invent a new repository style.
The documentation checks for this increment use the existing `.editorconfig`
UTF-8/LF/final-newline/no-trailing-whitespace rules, unchanged archive bytes,
resolved new/moved references and a documentation-only write boundary. These
checks do not qualify a host or replace any missing implementation gate.

### Documentation gate execution

Executed with Python 3.12 as `python -c <inline documentation gate>`, with the
pre-edit inventory supplied on stdin; no script file or dependency was added.
The gate compares tracked non-context files with their pre-edit contents,
normalizes Windows inventory paths to `/`, checks the five delivered Markdown
files and resolves newly added link targets. The reference check is deliberately
scoped: it is **not** a claim that historical repository links are all valid.
The 1,091-blob release inventory and the 1,089-file later documentation-baseline
snapshot differ in revision and context-file exclusions; they are not the same
population. This increment's three edited documents are `docs/goals/README.md`
and the incoming-reference repairs in `docs/goal-opencode-peer-testing.md` and
`docs/plans/2026-11-14-0006e-claude-native-capability-plan.md`.

Gate output before the independent review's citation corrections:

```text
TEXT_RULES files=5 violations=0
ARCHIVE bytes=3248 sha256=a6cbf0803217224c195b0928e099de5078fb34a69e91c6e789f2f483c53e2676 text_unchanged=true links_rebased=5
WRITE_BOUNDARY tracked_noncontext=1089 intended_path_changes=6 out_of_scope=0
NEW_LINKS local_resolved=38 external_cited=6 unresolved=0
FROZEN_ARCHIVE moved_base_resolved=5 unresolved=0
STALE_INCOMING_FINAL_REFERENCES matches=0
MATRIX_INVENTORY core=20
MATRIX_INVENTORY native=6
MATRIX_INVENTORY findings=8
DOCUMENTATION_GATE exit=0
```

The child process exited **0**. Six path changes mean one new receipt, three
edited documents and the old/new paths of one archive move whose text is
unchanged. Integration rebased the moved contract's five relative targets for
the archive base and verified each one resolves; only link targets changed, no
contract wording or acceptance condition. Six external citations are not new
external link availability probes; GitHub queries actually performed are listed
above.

Integration note (supersedes the byte-for-byte sentence in F8): the archived
contract's text and acceptance conditions are unchanged, and its five relative
Markdown targets were rebased so they resolve from `docs/goals/archive/`
(`0006i-exact-subject-release-acceptance.md` and
`../../design/{core,data,protocol,harness}.md`). The archive is now 3,248 bytes
with SHA-256
`a6cbf0803217224c195b0928e099de5078fb34a69e91c6e789f2f483c53e2676`, and the gate
line above records the rebase. No contract wording or acceptance condition
changed, and F8's remaining findings stand.

Checker-development failures are retained: the first child process exited 1
because its inventory compared Windows `\\` paths with `/` paths; two partial
resumptions exited 1 on missing local variables (`b`, then `current_docs`). The
self-contained corrected gate above exited 0. None of those failures involved
a repository-source change, test failure or waived predicate. No repository
Markdown formatter/linter command was available to run; that limitation remains
explicit rather than calling this inline check the repository's formatter.

### Independent receipt review

Read-only review `FinalReceiptReview` examined the contract, brief, receipt,
closure and incoming links, with no suite execution or write authority. It
returned two confirmed P3 citation findings, one speculative P3 provenance
ambiguity, no P0/P1/P2 finding, and an optional inventory-denominator
clarification. The parent separated the legacy-refusal fix `109f6037…` from
the migration/simplification fix `8c98426f…`, added the exact archived 0006i
contract target, clarified baseline-versus-current incoming-reference edits,
and explained the different inventory populations. This is the parent's bounded
integration of that review, not a claim that the reviewer reran the gate or
reviewed the corrected text again. Private logs/database rows remain outside
its independent verification boundary.

After those corrections, the parent reran the self-contained documentation
gate: child process exit **0**, five Markdown files / zero text-rule violations,
3,248-byte archive whose text is unchanged and whose five relative targets were
rebased so they resolve from the archive directory (new digest `a6cbf080…`),
1,089 snapshotted tracked
non-context files / six intended path changes / zero out-of-scope changes,
**39 resolved new local link targets** / six external citations / zero unresolved
new links, zero stale incoming references, and the same 20 requirement rows /
six native rows / eight findings. The local-link count increased from 38 to 39
because review added the exact 0006i contract target. The moved archive's
rebased targets all resolve, so no unresolved archive link remains.

### Scoped simplification

No source abstraction, alternate convention, new plan, duplicate acceptance
action or validation harness is introduced. The requirement and native matrices
remain separate because kernel execution, exact-binary characterization and
exact-Subject acceptance have different validity boundaries; combining them
would hide the fixture-versus-release distinction. Reference definitions reuse
the owning artifacts without copying their historical receipts. No unrelated
maintenance or formatter changes are included.

## Invalidation rules

1. A source/test/workflow/binary/profile change creates a different Subject and
   invalidates affected release proof. Bind fresh evidence to its full repository
   resource, commit, protocol digest and canonical subject hash. A Git tree OID
   alone is not the protocol digest or complete Subject.
2. A new native pin, executable blob, OS/owner path or tool/permission profile
   needs its own affected native evidence. Neither matching version text nor
   fixture storage in a newer tree transfers promotion.
3. A changed Goal revision, artifact or validation contract invalidates prior
   acceptance for that changed subject; recorded IDs remain historical, not
   reusable authority. Unknown provider effects require readback, not redispatch.
4. Documentation-only archive/index/receipt changes do not rebind the frozen
   release or create a release-Subject check on their newer tree. An amended
   evidence claim requires explicit new review, not removal of an unproved row.
5. A qualified host, recovered private receipt or later lettered-goal closure
   can resolve only the predicate it actually proves. Until then, F1–F8 remain
   recorded limits; no model, green aggregate or local `achieve` rounds them up.

[final]: ../goals/archive/0006-final.md
[0006i-goal]: ../goals/archive/0006i-exact-subject-release-acceptance.md
[release]: goal-0006i-exact-subject-release-acceptance-2026-11-15.md
[goal-index]: ../goals/README.md
[design]: ../design/README.md
[core]: ../design/core.md
[data]: ../design/data.md
[protocol]: ../design/protocol.md
[contracts]: ../design/contracts.md
[harness]: ../design/harness.md
[todo]: ../TODO.md
[binder]: ../../scripts/ci/linux-v2-source-subject.py
[goals-tests]: ../../control/test/symmetry_control/goals_test.exs
[orchestration-tests]: ../../control/test/symmetry_control/orchestration_test.exs
[admission-tests]: ../../control/test/symmetry_control/orchestration_goal_admission_test.exs
[machine-tests]: ../../control/test/symmetry_control_web/controllers/goal_machine_controller_test.exs
[operator-tests]: ../../control/test/symmetry_control_web/controllers/goal_controller_test.exs
[protocol-tests]: ../../control/test/symmetry_control_web/controllers/protocol_controller_test.exs
[integration-tests]: ../../control/test/symmetry_control/integrations_test.exs
[delivery-tests]: ../../daemon/internal/app/goal_delivery_test.go
[usage-tests]: ../../daemon/internal/app/native_settlement_regression_test.go
[migration-tests]: ../../control/test/symmetry_control/migrations/goal_0006_migration_test.exs
[continuity]: goal-0006c-retained-session-continuity-2026-11-11.md
[provider-goal]: ../goals/archive/0006h-credentialed-provider-accounting.md
[provider-witness]: ../../scripts/ci/credentialed-provider-witness.exs
[codex]: ../../daemon/internal/harness/testdata/codex/0.168.0/README.md
[claude]: ../../daemon/internal/harness/testdata/claude/2.1.323/README.md
[pi]: ../../daemon/internal/harness/testdata/pi/1.5.2/README.md
[omp]: goal-0006j-omp-native-capability-2026-11-14.md
[omp-probe]: ../../daemon/internal/harness/omp/probe.go
[omp-spend]: ../../daemon/internal/harness/omp/native_acceptance_test.go
[omo]: ../../daemon/internal/harness/testdata/omo/5.1.69/README.md
[omo-goal]: ../goals/archive/0006k-omo-native-capability.md
[opencode-goal]: ../goals/archive/0006g-opencode-support-decision.md
[opencode]: ../../daemon/internal/harness/testdata/opencode/2.3.0/README.md
[pr22]: https://github.com/wxxb789/symmetry/pull/22
[ci-pr]: https://github.com/wxxb789/symmetry/actions/runs/43633014138
[ci-subject]: https://github.com/wxxb789/symmetry/actions/runs/43633946120
[ci-native]: https://github.com/wxxb789/symmetry/actions/runs/43673581776
[codex-review]: https://github.com/wxxb789/symmetry/pull/22#issuecomment-6883104913
[probe-artifact]: https://github.com/wxxb789/symmetry/actions/runs/43673581776/artifacts/14145314081

- **0006a — completion evidence not found (recorded 2026-11-17).** A search for 0006a's own closure paragraph, receipt or merge evidence across `docs/goals/README.md`, `docs/evidence/`, `docs/plans/` and the repository for the string `0006a` returned nothing beyond the index's parenthetical that 0006a, 0006b and 0006c are archived. This line records the absence; no evidence is invented, and the goal's own completion predicates remain the only thing that could demonstrate it.
