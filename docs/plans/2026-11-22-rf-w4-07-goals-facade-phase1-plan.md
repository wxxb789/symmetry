# rf-w4-07 goals.ex facade split phase 1 plan — kernel + RunFence + EventsReceipts

Contract: `goal-rf-w4-07-goals-facade-phase1.md` (GDD state root, `sha256
d1bb8556e7d4e1769e500561f1092537715133bd683e04562b1fa59e156792bc`). Binding sections: the
`arch-decision-2 — SUPERSEDES …` section, the `arch-decision-2 verification amendments`, the
`arch-decision-2, open-item rulings` section and the owner's `Comment policy (2026-11-22)`.
Baseline for every coordinate: this worktree's HEAD `b25e34aeed1381a67bc2019ba156a1d381a8e3d7`
(= `origin/main`; the four dispatch prerequisites W2-07 #184, W3-10 #202, W3-14 #211 and W3-15 #217
are merged into it and arch-decision-2 is BOUND, so the HOLD condition at the contract head is
satisfied). Base file under analysis: `git show b25e34ae:control/lib/symmetry_control/goals.ex`,
sha256 `6ad78e29ae9d5caa7392da1ce90ed541fc9de8618022e55adc5eaf5b33f7db45`, 10,606 lines.

Owned files: `control/lib/symmetry_control/goals.ex`; new
`control/lib/symmetry_control/goals/{run_fence,events_receipts,internal}.ex`; the existing
`control/lib/symmetry_control/goals/contract_validation.ex` (receives `validate_contract/3`); new
`control/test/symmetry_control/goals/{run_fence,events_receipts}_test.exs`;
`scripts/ci/omo-control-native-evidence.py` (`SOURCE_PATHS` only); this plan file. No other file is
edited.

Slice shape: the slice moves three move-only regions out of `goals.ex` behind the unchanged
`SymmetryControl.Goals` facade — the shared kernel (`Goals.Internal`, 63 functions), the run-fence
lifecycle (`Goals.RunFence`, 75 functions at the slice head; the review round 1 moves 26 of them
into `Goals.HarnessSessions`) and the event/receipt kernel (`Goals.EventsReceipts`, 19
functions) — plus `validate_contract/3` into the existing `Goals.ContractValidation`. **158
functions / 2,456 block lines move**; the apply stage records every non-verbatim deviation
(7 comment-policy trims, one `@max_safe_integer` spelling, one qualifier rewrite and the nine
contract-mandated `!` renames) and re-proves the move with the same AST tool.

## Public / API shape

- **`SymmetryControl.Goals` keeps all 31 public functions** with unchanged names, arities, default
  arguments, specs and return shapes. Nine of them — `claim/3`, `claim_with_disposition/3`,
  `attach_harness_session/5`, `fetch_harness_session_attachment/3`, `mark_harness_session_stopped/5`,
  `append_evidence/5`, `append_evidence_batch/5`, `record_usage/5`, `fetch_run_context/3` — are
  RunFence cluster members, so their bodies move and the facade keeps one-line `defdelegate` clauses
  carrying the original `@doc false`/`@spec` header and the original defaults. The other 22 public
  functions stay inline and are not edited.
- **`SymmetryControl.Goals.Internal`** (`@moduledoc false`): row-lock helpers, the map/value/
  timestamp primitives, the validators, the admission/subject predicates and the mutation-id
  builders that a moving module calls or that two or more non-moving clusters call; membership is
  the fixpoint described in "Design trade-offs" §1. Every body is the base body; the 56 names the
  facade and the movers import are `def`, the other six keep their base `defp` visibility (apply stage).
- **`SymmetryControl.Goals.RunFence`** (`@moduledoc false`): the k2 RunFence cluster plus
  `context_wire_envelope!/4`, the one ContextSnapshot-region function the contract lifts (body
  unchanged), minus the 26 harness-session functions the round-1 split moves to
  `Goals.HarnessSessions`. Its public functions are the six facade-delegated run entries (the three
  harness ones now delegate to `Goals.HarnessSessions`), `context_wire_envelope!/4` and the six
  `@doc false` cross-edge widenings HarnessSessions imports; every other moved function is `defp`,
  as at base. The implement stage's uniform `defp` → `def` transform rested on a claim the simplify
  review measured false (that goals.ex and the other moved modules call them): a name+arity census
  over `control/lib` + `control/test` finds no caller outside the file for the other 64, and the
  apply stage restored `defp` (finding F2).
- **`SymmetryControl.Goals.HarnessSessions`** (`@moduledoc false`, round 1): the 26 functions of the
  harness-session attach/fetch/stop path (`attach_harness_session/5`,
  `fetch_harness_session_attachment/3`, `mark_harness_session_stopped/5`,
  `mark_harness_session_unavailable!/2` and their 22 private helpers), moved out of RunFence
  verbatim under the 2026-11-22 review-round-1 amendment. `Goals` keeps the same three
  `defdelegate`s (retargeted) and imports `mark_harness_session_unavailable!/2` from here.
- **`SymmetryControl.Goals.EventsReceipts`** (`@moduledoc false`): the k2 EventsReceipts cluster
  (event append, receipt snapshot encode/decode/validate, RequestHash replay, wakeup/control
  enqueue).
- **`SymmetryControl.Goals.ContractValidation`** (existing module): receives `validate_contract/3`
  (the 50-line envelope validator, `@doc false`); the apply stage replaced its 12
  `ContractValidation.`-qualified calls with direct local calls and dropped the `alias __MODULE__`
  (recorded body edit, Design trade-offs §10).
- **No new public `SymmetryControl.Goals.*` surface.** The six widened helpers below are
  `@doc false def`s on the facade — the recorded internal interface the contract asks for, not part
  of the frozen 31-function API. No `Goals.ContextSnapshot` schema is involved, and no module named
  `Goals.DbLocks` or `Goals.PayloadContracts` is created.
- **Interface lines.** `goals.ex` gains exactly five `import …, only:` lines next to its existing
  `import Ecto.Query`; unlisted names resolve through them so no existing call site changes text
  (E2). The new modules carry their own kernel imports. Verbatim (line-wrapped by the formatter):


      import Ecto.Query
      import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]
      import SymmetryControl.Goals.EventsReceipts,
      import SymmetryControl.Goals.HarnessSessions,
      import SymmetryControl.Goals.Internal,
      import SymmetryControl.Goals.RunFence,

  `goals/run_fence.ex` carries
  import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]; import SymmetryControl.Goals.EventsReceipts, only: [enqueue_wakeup!: 2]; import SymmetryControl.Goals.Internal,;

  `goals/harness_sessions.ex` carries `import Ecto.Query`, the same kernel import shape for its 13
  Internal names (14 pairs) and `import SymmetryControl.Goals.RunFence, only:` for its six
  cross-edge names (all `@doc false def`s on RunFence);

  `goals/events_receipts.ex` carries the EventsReceipts-side kernel import
  (`normalize_map/1`, `now/1`, `rollback/1`, `stamp_insert/2`, `stamp_update/2`, `valid_uuid?/1`,
  `value/2`) and no ContractValidation import (it does not call `validate_contract/3`).
  `goals/internal.ex` carries `import Ecto.Query` only.
- **Alias block.** The facade's alias list is rebuilt from the base list: an alias is dropped only
  when the compiler reports it unused at the slice head (base-alias list → compile → drop exactly the
  three the compiler names → compile clean). Measured: `HarnessSessionAttachReceipt`,
  `HarnessSessionStopReceipt`, `GoalControlWorker` are dropped; the other 27 stay. Remaining aliases
  are ordered case-insensitively (Credo's `AliasOrder`), which also removes the base's
  `GoalExternalWait` ordering finding. No other top-matter line changes.
- **Module attributes travel with their users** (compile-time constants, contract-sanctioned):
  RunFence carries `@nonterminal_task_states` and `@max_evidence_batch_items`; HarnessSessions
  carries `@terminal_run_states` and `@accounting_terminal_run_states` (the only staying users of
  the latter two, so the round-1 split moves both definitions out of RunFence —
  `--warnings-as-errors`; the copies' drift risk is recorded in `docs/TODO.md`); Internal carries
  `@nonterminal_task_states` and `@max_safe_integer`; EventsReceipts needs none. All of them are
  still used by the staying code, so they remain defined in `goals.ex` as well —
  `@max_evidence_batch_items` is the one that is *not* used by the staying code any more and is
  therefore removed from `goals.ex` (forced by `--warnings-as-errors`).

## Re-derived partition at HEAD (method)

1. **Declaration census** (`parse_defs.exs`, AST, `token_metadata: true`) over the base blob: 647
   `def`/`defp` clauses = 461 distinct functions, each with its exact clause span (last line from the
   node's `end` metadata, falling back to the maximum child line, then the clause's own line);
   attached `@doc`/`@spec`/`@typedoc`/`@type` spans come from the same AST, extended over heredocs and
   bracket-balanced continuation lines.
2. **Line ownership**: each function owns the contiguous non-blank run of attribute/comment lines
   above its first clause plus its clause spans, allowing one blank-separated attribute block
   (`@typedoc false` + `@type new_claim_authority_envelope` + `@spec`), never a second. Verified: 461
   blocks tile lines 131–10,606 with **0 duplicate lines and exactly one non-blank gap** (the
   module's own closing `end`); lines 1–120 are the module body.
3. **Compile tracer** (`trace_goals.exs`, the arch-k2 method re-run at HEAD): `Code.compile_file/1`
   with `Code.put_compiler_option(:tracers, …)` against `_build/dev`; 1,916 local-call events and
   4,354 remote-call events, each line attributed to its owning block. Callee arities are canonicalised
   to the defined function only when the called arity is *not* itself defined (default-argument
   expansion); an explicit lower-arity definition maps to itself. Result: **1,238 caller→callee pairs,
   0 unresolved local callees**.
4. **Cluster membership** = the arch-k2 name lists (`refactor/scratch-k2/clusters.py`) at HEAD: 0
   names missing, 0 names duplicated across lists, plus the two functions added since by W2-07/W3-10
   (`mutation_id/2` → PayloadContracts, `dispatch_result/10` → Settlement).
5. **Move-set derivation**: RunFence/EventsReceipts are their k2 clusters (RunFence plus
   `context_wire_envelope!/4`); `Goals.Internal` is the fixpoint of "DbLocks member except the two
   Settlement-only locks, or PayloadContracts member called by a moving module or by ≥2 non-moving
   clusters, closed over every kernel member those call"; `validate_contract/3` goes to
   ContractValidation and `context_digest/1` is excluded from Internal (ContextSnapshot region).
6. The per-function HEAD spans below are the authoritative line map (each function's whole block,
   header included, moves to its destination).


### RunFence move set (75 functions)

At the round-1 head 26 of these rows live in `Goals.HarnessSessions` (see "Fix round 1" below);
their base line map is unchanged.

| function | HEAD lines | LOC | kind |
| --- | --- | --- | --- |
| `new_claim_authority/1` | 121–162 | 42 | defp |
| `claim/3` | 164–180 | 17 | def |
| `claim_with_disposition/3` | 182–207 | 26 | def |
| `claim_with_disposition_in_transaction/3` | 209–272 | 64 | defp |
| `attach_harness_session/5` | 462–535 | 74 | def |
| `fetch_harness_session_attachment/3` | 537–559 | 23 | def |
| `mark_harness_session_stopped/5` | 561–635 | 75 | def |
| `append_evidence/5` | 637–714 | 78 | def |
| `append_evidence_batch/5` | 716–746 | 31 | def |
| `classify_and_insert_evidence_batch!/7` | 748–878 | 131 | defp |
| `evidence_batch_receipt/1` | 880–885 | 6 | defp |
| `evidence_batch_conflict!/2` | 887–902 | 16 | defp |
| `record_usage/5` | 904–968 | 65 | def |
| `fetch_run_context/3` | 970–1020 | 51 | def |
| `lock_goal_run!/5` | 1022–1052 | 31 | defp |
| `ensure_machine_fence!/7` | 1054–1077 | 24 | defp |
| `valid_fence/1` | 1079–1089 | 11 | defp |
| `session_attrs/1` | 1091–1128 | 38 | defp |
| `session_stop_attrs/1` | 1130–1146 | 17 | defp |
| `session_stop_request/2` | 1148–1159 | 12 | defp |
| `session_attach_request/2` | 1161–1180 | 20 | defp |
| `ensure_stoppable_session!/5` | 1182–1192 | 11 | defp |
| `stop_receipt_matches?/4` | 1194–1197 | 4 | defp |
| `ensure_session_scope!/6` | 1199–1212 | 14 | defp |
| `ensure_current_attachment!/7` | 1214–1221 | 8 | defp |
| `ensure_attachment_binding_shape!/3` | 1223–1242 | 20 | defp |
| `attach_fresh_session!/7` | 1244–1247 | 4 | defp |
| `attach_handoff_session!/7` | 1249–1252 | 4 | defp |
| `reject_existing_handle!/3` | 1254–1263 | 10 | defp |
| `create_fresh_harness_session!/7` | 1265–1296 | 32 | defp |
| `attach_reserved_session!/6` | 1298–1320 | 23 | defp |
| `attach_session_to_run!/3` | 1322–1339 | 18 | defp |
| `lock_attach_receipt/1` | 1341–1348 | 8 | defp |
| `attach_receipt_matches?/4` | 1350–1353 | 4 | defp |
| `attach_receipt_fence_matches?/3` | 1355–1362 | 8 | defp |
| `create_attach_receipt!/8` | 1364–1385 | 22 | defp |
| `attachment_receipt_response/1` | 1387–1423 | 37 | defp |
| `mark_harness_session_unavailable!/2` | 1425–1448 | 24 | defp |
| `session_receipt/5` | 1450–1473 | 24 | defp |
| `session_stop_receipt/4` | 1475–1488 | 14 | defp |
| `evidence_attrs/2` | 1490–1543 | 54 | defp |
| `evidence_batch_attrs/2` | 1545–1589 | 45 | defp |
| `evidence_matches?/2` | 1591–1600 | 10 | defp |
| `validation_evidence_closed?/4` | 1602–1613 | 12 | defp |
| `ensure_evidence_identity!/6` | 1615–1661 | 47 | defp |
| `snapshot_acceptance_contract!/1` | 1663–1671 | 9 | defp |
| `ensure_validation_binding!/6` | 1673–1691 | 19 | defp |
| `binding_matches_evidence?/6` | 1693–1719 | 27 | defp |
| `validator_profile_for!/2` | 1721–1728 | 8 | defp |
| `source_matches_evidence?/4` | 1730–1742 | 13 | defp |
| `source_matches_evidence_kind?/5` | 1744–1791 | 48 | defp |
| `evidence_receipt/1` | 1793–1805 | 13 | defp |
| `usage_attrs/2` | 1807–1862 | 56 | defp |
| `usage_matches?/2` | 1864–1871 | 8 | defp |
| `usage_receipt/1` | 1873–1883 | 11 | defp |
| `microusd_wire_value/1` | 1885–1886 | 2 | defp |
| `ensure_usage_supersedes_run!/2` | 1888–1900 | 13 | defp |
| `reconcile_reservation_and_arm!/3` | 1932–1946 | 15 | defp |
| `evidence_validator_profile/2` | 1962–1966 | 5 | defp |
| `valid_usage_cost?/2` | 2016–2021 | 6 | defp |
| `required_datetime/2` | 2023–2037 | 15 | defp |
| `context_wire_envelope!/4` | 2097–2105 | 9 | defp |
| `machine_write_result/1` | 2718–2726 | 9 | defp |
| `machine_session_stop_result/1` | 2728–2737 | 10 | defp |
| `claim_task_current?/2` | 8519–8523 | 5 | defp |
| `claim_planning_task?/1` | 8525–8532 | 8 | defp |
| `claim_identity_current?/3` | 8534–8538 | 5 | defp |
| `notify_claim_authority_routed/2` | 8540–8545 | 6 | defp |
| `claim_work_item_owned?/4` | 8547–8557 | 11 | defp |
| `task_repository_resource_id!/2` | 8758–8770 | 13 | defp |
| `validate_evidence_batch_contract/2` | 10101–10106 | 6 | defp |
| `validate_evidence_contract!/2` | 10108–10114 | 7 | defp |
| `validate_new_evidence_batch!/3` | 10116–10127 | 12 | defp |
| `same_uuid?/2` | 10182–10191 | 10 | defp |
| `required_sha256_digest/2` | 10278–10286 | 9 | defp |

### EventsReceipts move set (19 functions)

| function | HEAD lines | LOC | kind |
| --- | --- | --- | --- |
| `replay_create/2` | 8143–8148 | 6 | defp |
| `replay_command/3` | 8150–8156 | 7 | defp |
| `replay_command_after_conflict/3` | 8158–8163 | 6 | defp |
| `replay_event/2` | 8165–8180 | 16 | defp |
| `append_event!/8` | 8182–8263 | 82 | defp |
| `enqueue_wakeup!/2` | 8265–8280 | 16 | defp |
| `enqueue_goal_control!/2` | 8282–8290 | 9 | defp |
| `receipt!/2` | 8292–8297 | 6 | defp |
| `immutable_receipt_snapshot/9` | 8299–8328 | 30 | defp |
| `encode_receipt_snapshot/1` | 8330–8330 | 1 | defp |
| `decode_receipt_snapshot/3` | 8332–8375 | 44 | defp |
| `validate_receipt_goal_snapshot/2` | 8377–8391 | 15 | defp |
| `validate_receipt_event_snapshot/2` | 8393–8409 | 17 | defp |
| `receipt_timestamps/2` | 8411–8421 | 11 | defp |
| `valid_receipt_utc?/1` | 8423–8430 | 8 | defp |
| `nullable_datetime/1` | 8432–8433 | 2 | defp |
| `response_for/2` | 8435–8445 | 11 | defp |
| `event_next_wake_at/3` | 8447–8466 | 20 | defp |
| `ensure_preconditions!/2` | 8468–8482 | 15 | defp |

### Internal move set (63 functions)

| function | HEAD lines | LOC | kind |
| --- | --- | --- | --- |
| `nullable_string/2` | 1948–1960 | 13 | defp |
| `nullable_safe_integer/2` | 1968–1979 | 12 | defp |
| `nullable_microusd/2` | 1981–1999 | 19 | defp |
| `only_known_keys?/2` | 2709–2716 | 8 | defp |
| `lock_goal/1` | 8484–8487 | 4 | defp |
| `lock_command_goal/2` | 8489–8494 | 6 | defp |
| `command_goal_lock_telemetry/3` | 8496–8502 | 7 | defp |
| `goal_project_id!/1` | 8504–8507 | 4 | defp |
| `lock_project!/1` | 8509–8514 | 6 | defp |
| `ensure_project_active!/1` | 8516–8517 | 2 | defp |
| `try_lock_active_goal_project/1` | 8559–8569 | 11 | defp |
| `lock_work_item/1` | 8571–8574 | 4 | defp |
| `lock_external_wait/1` | 8576–8579 | 4 | defp |
| `lock_context_snapshot/2` | 8581–8588 | 8 | defp |
| `lock_repository_resource!/1` | 8590–8597 | 8 | defp |
| `lock_repository_resources!/1` | 8599–8618 | 20 | defp |
| `lock_work_items/1` | 8620–8632 | 13 | defp |
| `lock_task/1` | 8634–8637 | 4 | defp |
| `lock_run/1` | 8702–8704 | 3 | defp |
| `lock_decision/1` | 8706–8709 | 4 | defp |
| `current_revision!/1` | 8711–8717 | 7 | defp |
| `active_task_for_item?/1` | 8729–8735 | 7 | defp |
| `plan_task?/1` | 8737–8738 | 2 | defp |
| `parse_subject!/1` | 9555–9560 | 6 | defp |
| `parse_subject/1` | 9562–9574 | 13 | defp |
| `valid_commit?/1` | 9854–9855 | 2 | defp |
| `required_sha256/2` | 9859–9869 | 11 | defp |
| `required_uuid/2` | 10146–10154 | 9 | defp |
| `required_uuid!/2` | 10156–10156 | 1 | defp |
| `optional_uuid/2` | 10158–10169 | 12 | defp |
| `optional_uuid!/2` | 10171–10171 | 1 | defp |
| `optional_uuid!/1` | 10172–10177 | 6 | defp |
| `valid_uuid/1` | 10178–10178 | 1 | defp |
| `valid_uuid?/1` | 10179–10180 | 2 | defp |
| `required_string/2` | 10193–10202 | 10 | defp |
| `required_string!/2` | 10204–10204 | 1 | defp |
| `required_map/2` | 10211–10216 | 6 | defp |
| `required_map!/2` | 10218–10218 | 1 | defp |
| `required_list!/2` | 10219–10219 | 1 | defp |
| `optional_list/3` | 10221–10226 | 6 | defp |
| `required_positive_integer/2` | 10244–10249 | 6 | defp |
| `required_safe_positive_integer/2` | 10251–10258 | 8 | defp |
| `parse_digest/1` | 10265–10276 | 12 | defp |
| `is_short_identifier?/1` | 10288–10289 | 2 | defp |
| `policy_integer/3` | 10300–10306 | 7 | defp |
| `allowed_values/2` | 10362–10370 | 9 | defp |
| `settlement_mutation_id/3` | 10372–10374 | 3 | defp |
| `mutation_id/2` | 10411–10426 | 16 | defp |
| `plan_action_hash/2` | 10471–10478 | 8 | defp |
| `work_item_acceptance_action_hash/3` | 10480–10488 | 9 | defp |
| `admission_enabled?/1` | 10533–10539 | 7 | defp |
| `value/3` | 10541–10543 | 3 | defp |
| `has_key?/2` | 10545–10546 | 2 | defp |
| `normalize_map/1` | 10548–10549 | 2 | defp |
| `normalize_value/1` | 10551–10553 | 3 | defp |
| `stamp_insert/2` | 10555–10559 | 5 | defp |
| `maybe_put_timestamp/3` | 10561–10565 | 5 | defp |
| `stamp_update/2` | 10567–10568 | 2 | defp |
| `now/1` | 10570–10571 | 2 | defp |
| `map_has_key?/2` | 10580–10585 | 6 | defp |
| `valid_long_text?/1` | 10587–10588 | 2 | defp |
| `unwrap!/1` | 10603–10604 | 2 | defp |
| `rollback/1` | 10605–10605 | 1 | defp |

Nine of the DbLocks rows carry the contract's `!`-suffix rename at the slice head (apply stage,
finding D3): `lock_goal!`, `lock_command_goal!`, `lock_work_item!`, `lock_external_wait!`,
`lock_context_snapshot!`, `lock_work_items!`, `lock_task!`, `lock_run!`, `lock_decision!`. Spans,
bodies and calls are otherwise unchanged (name-only, recorded).

### ContractValidation move set (1) and stay-in-goals.ex set (19)

| function | HEAD lines | LOC | kind |
| --- | --- | --- | --- |
| `validate_contract/3` | 10050–10099 | 50 | defp |

| function | HEAD lines | LOC | kind |
| --- | --- | --- | --- |
| `context_digest/1` | 2609–2616 | 8 | defp |
| `lock_settlement_tasks!/2` | 8639–8658 | 20 | defp |
| `lock_settlement_runs!/3` | 8660–8700 | 41 | defp |
| `valid_event_options/1` | 10042–10048 | 7 | defp |
| `validate_goal_contract/3` | 10129–10134 | 6 | defp |
| `valid_attention_options/1` | 10136–10144 | 9 | defp |
| `optional_string/2` | 10206–10207 | 2 | defp |
| `require_reason!/1` | 10209–10209 | 1 | defp |
| `required_nonnegative_integer/2` | 10228–10233 | 6 | defp |
| `required_safe_nonnegative_integer/2` | 10235–10242 | 8 | defp |
| `required_safe_positive_integer!/2` | 10260–10261 | 2 | defp |
| `required_digest!/2` | 10263–10263 | 1 | defp |
| `optional_digest!/2` | 10291–10291 | 1 | defp |
| `optional_digest!/1` | 10292–10293 | 2 | defp |
| `digest!/1` | 10294–10298 | 5 | defp |
| `policy_nonnegative_integer/3` | 10308–10314 | 7 | defp |
| `dependency_action_hash/4` | 10490–10498 | 9 | defp |
| `valid_actor/1` | 10573–10578 | 6 | defp |
| `optional_string_strict!/2` | 10590–10601 | 12 | defp |

Totals:

| destination | functions | block LOC | contiguous runs | first–last HEAD line |
| --- | --- | --- | --- | --- |
| `Goals.RunFence` | 49 | 1149 | 49 | 131–10286 |
| `Goals.HarnessSessions` | 26 | 548 | 26 | 462–1488 |
| `Goals.EventsReceipts` | 19 | 322 | 19 | 8143–8482 |
| `Goals.Internal` | 63 | 387 | 60 | 1948–10606 |
| `Goals.ContractValidation` | 1 | 50 | 1 | 10050–10099 |
| stay in `goals.ex` | 19 | 153 | 18 | 2609–10601 |
| **moved total** | **158** | **2456** | | (row sums of the three move-set tables above; base `b25e34ae` `goals.ex` 10,606 lines → 8,121 at the round-1 head; `Goals.HarnessSessions` is the review-round-1 sub-split of the RunFence rows, so the two rows sum to the pre-round-1 RunFence figure 1,697) |
## R9 evidence-freeze mapping (moved declaration → `SOURCE_PATHS` path)

`scripts/ci/omo-control-native-evidence.py` `SOURCE_PATHS` gains the five files that receive
`goals.ex` code (`harness_sessions.ex` in review round 1); every one of the 158 moved declarations maps
to exactly one of them, and a
definition census over the slice tree shows each moved function defined exactly once, with **0**
moved functions still defined in `goals.ex`:


| moved declaration | SOURCE_PATHS destination |
| --- | --- |
| `append_evidence/5` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `append_evidence_batch/5` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `attach_fresh_session!/7` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `attach_handoff_session!/7` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `attach_harness_session/5` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `attach_receipt_fence_matches?/3` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `attach_receipt_matches?/4` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `attach_reserved_session!/6` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `attach_session_to_run!/3` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `attachment_receipt_response/1` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `binding_matches_evidence?/6` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `claim/3` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `claim_identity_current?/3` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `claim_planning_task?/1` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `claim_task_current?/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `claim_with_disposition/3` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `claim_with_disposition_in_transaction/3` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `claim_work_item_owned?/4` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `classify_and_insert_evidence_batch!/7` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `context_wire_envelope!/4` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `create_attach_receipt!/8` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `create_fresh_harness_session!/7` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `ensure_attachment_binding_shape!/3` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `ensure_current_attachment!/7` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `ensure_evidence_identity!/6` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `ensure_machine_fence!/7` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `ensure_session_scope!/6` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `ensure_stoppable_session!/5` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `ensure_usage_supersedes_run!/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `ensure_validation_binding!/6` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `evidence_attrs/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `evidence_batch_attrs/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `evidence_batch_conflict!/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `evidence_batch_receipt/1` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `evidence_matches?/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `evidence_receipt/1` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `evidence_validator_profile/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `fetch_harness_session_attachment/3` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `fetch_run_context/3` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `lock_attach_receipt/1` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `lock_goal_run!/5` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `machine_session_stop_result/1` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `machine_write_result/1` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `mark_harness_session_stopped/5` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `mark_harness_session_unavailable!/2` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `microusd_wire_value/1` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `new_claim_authority/1` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `notify_claim_authority_routed/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `reconcile_reservation_and_arm!/3` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `record_usage/5` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `reject_existing_handle!/3` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `required_datetime/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `required_sha256_digest/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `same_uuid?/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `session_attach_request/2` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `session_attrs/1` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `session_receipt/5` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `session_stop_attrs/1` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `session_stop_receipt/4` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `session_stop_request/2` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `snapshot_acceptance_contract!/1` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `source_matches_evidence?/4` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `source_matches_evidence_kind?/5` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `stop_receipt_matches?/4` | `control/lib/symmetry_control/goals/harness_sessions.ex` |
| `task_repository_resource_id!/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `usage_attrs/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `usage_matches?/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `usage_receipt/1` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `valid_fence/1` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `valid_usage_cost?/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `validate_evidence_batch_contract/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `validate_evidence_contract!/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `validate_new_evidence_batch!/3` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `validation_evidence_closed?/4` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `validator_profile_for!/2` | `control/lib/symmetry_control/goals/run_fence.ex` |
| `append_event!/8` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `decode_receipt_snapshot/3` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `encode_receipt_snapshot/1` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `enqueue_goal_control!/2` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `enqueue_wakeup!/2` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `ensure_preconditions!/2` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `event_next_wake_at/3` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `immutable_receipt_snapshot/9` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `nullable_datetime/1` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `receipt!/2` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `receipt_timestamps/2` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `replay_command/3` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `replay_command_after_conflict/3` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `replay_create/2` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `replay_event/2` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `response_for/2` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `valid_receipt_utc?/1` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `validate_receipt_event_snapshot/2` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `validate_receipt_goal_snapshot/2` | `control/lib/symmetry_control/goals/events_receipts.ex` |
| `active_task_for_item?/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `admission_enabled?/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `allowed_values/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `command_goal_lock_telemetry/3` | `control/lib/symmetry_control/goals/internal.ex` |
| `current_revision!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `ensure_project_active!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `goal_project_id!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `has_key?/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `is_short_identifier?/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_command_goal!/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_context_snapshot!/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_decision!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_external_wait!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_goal!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_project!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_repository_resource!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_repository_resources!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_run!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_task!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_work_item!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `lock_work_items!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `map_has_key?/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `maybe_put_timestamp/3` | `control/lib/symmetry_control/goals/internal.ex` |
| `mutation_id/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `normalize_map/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `normalize_value/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `now/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `nullable_microusd/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `nullable_safe_integer/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `nullable_string/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `only_known_keys?/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `optional_list/3` | `control/lib/symmetry_control/goals/internal.ex` |
| `optional_uuid!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `optional_uuid!/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `optional_uuid/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `parse_digest/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `parse_subject!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `parse_subject/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `plan_action_hash/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `plan_task?/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `policy_integer/3` | `control/lib/symmetry_control/goals/internal.ex` |
| `required_list!/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `required_map!/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `required_map/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `required_positive_integer/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `required_safe_positive_integer/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `required_sha256/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `required_string!/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `required_string/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `required_uuid!/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `required_uuid/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `rollback/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `settlement_mutation_id/3` | `control/lib/symmetry_control/goals/internal.ex` |
| `stamp_insert/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `stamp_update/2` | `control/lib/symmetry_control/goals/internal.ex` |
| `try_lock_active_goal_project/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `unwrap!/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `valid_commit?/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `valid_long_text?/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `valid_uuid/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `valid_uuid?/1` | `control/lib/symmetry_control/goals/internal.ex` |
| `value/3` | `control/lib/symmetry_control/goals/internal.ex` |
| `work_item_acceptance_action_hash/3` | `control/lib/symmetry_control/goals/internal.ex` |
| `validate_contract/3` | `control/lib/symmetry_control/goals/contract_validation.ex` |


Census summary (generated by `def-census.txt`): 158 moved declarations, 157 with exactly one
definition site; the sole two-site name is `value/3`, which also exists as an unrelated private
helper of the existing `ContractValidation` module in the base revision (the ContractValidation copy
is byte-identical base vs head).

## Interface and cross-edge table

The movers' whole outbound interface — every crossing of the module boundary is here, and nothing
else leaves a moved module:


### Facade widenings: `defp` → `@doc false def` in goals.ex (called from a moved module)

| function (widened on the facade) | caller functions in the moved modules | call sites |
| --- | --- | --- |
| `reconcile_reservation!/3` | reconcile_reservation_and_arm!/3 | 1 |
| `safe_document?/1` | evidence_attrs/2 | 2 |
| `safe_evidence_document/1` | append_evidence/5, append_evidence_batch/5 | 2 |
| `context_wire_envelope/4` | context_wire_envelope!/4 | 1 |
| `context_digest/1` | evidence_receipt/1 | 1 |
| `settlement_task_owned_by_goal?/3` | lock_goal_run!/5 | 1 |

| edge | functions (name/arity pairs as emitted) | mechanism |
| --- | --- | --- |
| RunFence/EventsReceipts → `Goals.Internal` | RunFence 29 names / 30 pairs (`value: 2` + `value: 3`); EventsReceipts 7 | kernel `import …, only:` in each module |
| HarnessSessions → `Goals.Internal` | 13 names / 14 pairs (`value: 2` + `value: 3`) | kernel `import …, only:` in HarnessSessions |
| HarnessSessions → `Goals.RunFence` | 6 names / 6 pairs (`ensure_machine_fence!: 7`, `lock_goal_run!: 5`, `machine_session_stop_result: 1`, `machine_write_result: 1`, `task_repository_resource_id!: 2`, `valid_fence: 1`) | `import …, only:` in HarnessSessions; the six are `@doc false def`s in RunFence |
| remaining `goals.ex` → `Goals.Internal` | 52 names / 54 pairs (`value` and `policy_integer` each at two arities) | kernel `import …, only:` in the facade |
| remaining `goals.ex` → `Goals.EventsReceipts` | 11 | `import …, only:` in the facade |
| remaining `goals.ex` → `Goals.RunFence` | 1 (`context_wire_envelope!/4` for the staying `fetch_context/2`) | `import …, only:` in the facade |
| remaining `goals.ex` → `Goals.HarnessSessions` | 1 (`mark_harness_session_unavailable!/2` for `settle_task/4`) | `import …, only:` in the facade |
| remaining `goals.ex` → `Goals.ContractValidation` | 1 (`validate_contract/3`) | `import …, only:` in the facade |
| RunFence → `Goals.EventsReceipts` | 1 (`enqueue_wakeup!/2`) | `import …, only:` in RunFence |
| movers → `Goals.ContractValidation` | 1 (`validate_contract/3`) | `import …, only:` in RunFence |
| movers → facade `@doc false def`s (widenings) | 6 names, 8 call sites in 7 functions | qualified `Goals.<name>(…)` calls |
| `Goals.Internal` → PlanContracts | 1 (`parse_subject/1` → `valid_commit?/1`) | resolved by moving `valid_commit?/1` into `Goals.Internal` with its sole caller |

`Goals.Internal` and `Goals.EventsReceipts` call nothing in the facade; `Goals.RunFence` reaches the
facade only through the six recorded widenings above, and the AST comparison shows exactly those
eight `Goals.` insertions as the only differences between base and head inside moved bodies
(`evidence_attrs/2` contributes two, the other six functions one each).


### movers → Internal (kernel imports)

| callee | call-site arities |
| --- | --- |
| `has_key?/2` | 2 |
| `is_short_identifier?/1` | 1 |
| `lock_context_snapshot!/2` | 2 |
| `lock_goal!/1` | 1 |
| `lock_project!/1` | 1 |
| `lock_run!/1` | 1 |
| `lock_task!/1` | 1 |
| `lock_work_item!/1` | 1 |
| `normalize_map/1` | 1 |
| `now/1` | 1 |
| `nullable_microusd/2` | 2 |
| `nullable_safe_integer/2` | 2 |
| `nullable_string/2` | 2 |
| `only_known_keys?/2` | 2 |
| `optional_uuid/2` | 2 |
| `parse_digest/1` | 1 |
| `parse_subject/1` | 1 |
| `parse_subject!/1` | 1 |
| `plan_task?/1` | 1 |
| `required_map/2` | 2 |
| `required_string/2` | 2 |
| `required_string!/2` | 2 |
| `required_uuid/2` | 2 |
| `rollback/1` | 1 |
| `settlement_mutation_id/3` | 3 |
| `stamp_insert/2` | 2 |
| `stamp_update/2` | 2 |
| `valid_uuid/1` | 1 |
| `valid_uuid?/1` | 1 |
| `value/2/3` | 2, 3 |

### remaining goals.ex → Internal (kernel import, 52 functions)

| callee | call-site arities |
| --- | --- |
| `active_task_for_item?/1` | 1 |
| `admission_enabled?/1` | 1 |
| `allowed_values/2` | 2 |
| `current_revision!/1` | 1 |
| `ensure_project_active!/1` | 1 |
| `goal_project_id!/1` | 1 |
| `is_short_identifier?/1` | 1 |
| `lock_command_goal!/2` | 2 |
| `lock_context_snapshot!/2` | 2 |
| `lock_decision!/1` | 1 |
| `lock_external_wait!/1` | 1 |
| `lock_goal!/1` | 1 |
| `lock_project!/1` | 1 |
| `lock_repository_resource!/1` | 1 |
| `lock_repository_resources!/1` | 1 |
| `lock_run!/1` | 1 |
| `lock_task!/1` | 1 |
| `lock_work_item!/1` | 1 |
| `lock_work_items!/1` | 1 |
| `map_has_key?/2` | 2 |
| `mutation_id/2` | 2 |
| `normalize_map/1` | 1 |
| `now/1` | 1 |
| `nullable_microusd/2` | 2 |
| `only_known_keys?/2` | 2 |
| `optional_list/3` | 3 |
| `optional_uuid!/2` | 2 |
| `parse_digest/1` | 1 |
| `parse_subject/1` | 1 |
| `parse_subject!/1` | 1 |
| `plan_action_hash/2` | 2 |
| `plan_task?/1` | 1 |
| `policy_integer/2/3` | 2, 3 |
| `required_list!/2` | 2 |
| `required_map/2` | 2 |
| `required_map!/2` | 2 |
| `required_safe_positive_integer/2` | 2 |
| `required_string/2` | 2 |
| `required_string!/2` | 2 |
| `required_uuid/2` | 2 |
| `required_uuid!/2` | 2 |
| `rollback/1` | 1 |
| `settlement_mutation_id/3` | 3 |
| `stamp_insert/2` | 2 |
| `stamp_update/2` | 2 |
| `try_lock_active_goal_project/1` | 1 |
| `unwrap!/1` | 1 |
| `valid_long_text?/1` | 1 |
| `valid_uuid/1` | 1 |
| `valid_uuid?/1` | 1 |
| `value/2/3` | 2, 3 |
| `work_item_acceptance_action_hash/3` | 3 |

### remaining goals.ex → EventsReceipts

| callee | call-site arities |
| --- | --- |
| `append_event!/8` | 8 |
| `enqueue_goal_control!/2` | 2 |
| `enqueue_wakeup!/2` | 2 |
| `ensure_preconditions!/2` | 2 |
| `nullable_datetime/1` | 1 |
| `receipt!/2` | 2 |
| `replay_command/3` | 3 |
| `replay_command_after_conflict/3` | 3 |
| `replay_create/2` | 2 |
| `replay_event/2` | 2 |
| `response_for/2` | 2 |

### remaining goals.ex → RunFence

| callee | call-site arities |
| --- | --- |
| `context_wire_envelope!/4` | 4 |
| `mark_harness_session_unavailable!/2` | 2 |

### RunFence → EventsReceipts

| callee | call-site arities |
| --- | --- |
| `enqueue_wakeup!/2` | 2 |

### movers → ContractValidation

| callee | call-site arities |
| --- | --- |
| `validate_contract/3` | 3 |

## Edge-case semantics the contract demands

- **Move-only with one definition site.** Every moved function keeps its base body text; the only
  transformation is `defp` → `def` (plus the eight recorded `Goals.` prefixes). Proof: a canonical
  per-function AST comparison (line metadata stripped, `defp` folded to `def`) of the base blob
  against the slice tree (re-run in the apply stage after the recorded edits) — **418 base
  functions byte-identical, 43 identical under a recorded edit, 0 missing**. The 43: the 7
  `Goals.`-prefix functions (`append_evidence/5`, `append_evidence_batch/5`,
  `context_wire_envelope!/4`, `evidence_attrs/2`, `evidence_receipt/1`, `lock_goal_run!/5`,
  `reconcile_reservation_and_arm!/3`), the 9 contract-mandated `!` renames (bodies identical with
  the base name restored), the 25 callers those renames retarget (bodies identical with the `!`
  tokens restored), `nullable_safe_integer/2` (the `@max_safe_integer` spelling, finding F5) and
  `validate_contract/3` (the 12-qualifier rewrite, finding F6). No message, atom, digest, SQL
  text or ordering token changes anywhere.
- **No function lost or duplicated.** Base census 461 functions + the existing ContractValidation
  module's 77 = 538; slice census `goals.ex` 303 + RunFence 49 + HarnessSessions 26 + EventsReceipts 19 + Internal 63 +
  ContractValidation 78 (its 77 plus the moved `validate_contract/3`) = 538. The ContractValidation
  comparison is `added ['validate_contract/3'] changed [] gone []`. Three names exist twice across
  the tree (`canonical_plan_proposal/1`, `valid_commit_path?/1`, `value/3`) — all three already exist
  twice in the base (ContractValidation has its own private helpers of those names) and the
  ContractValidation copies are unchanged; no *moved* function is duplicated.
- **Control flow** (conductor lesson 32). The per-entry audit below is generated from the same AST
  comparison, so every `case`/`cond`/`with`/`if` arm, guard, default-argument head, fall-through
  clause and `rollback/1` short-circuit is preserved by construction; the entries marked "no" are the
  ones carrying a recorded rewrite — the `Goals.` prefixes, `lock_context_snapshot!` retargets and
  `validate_contract/3`'s qualifier rewrite — and their base/head structural counts are equal.
- **`Repo.rollback` stays inside the same function.** `Repo.rollback/1` is called in exactly one
  place — `rollback/1`, which moves to `Goals.Internal` (in `internal.ex`, body unchanged). Every
  moved function that aborts a transaction calls `rollback/…` at the same point inside its own body;
  the audit table counts those call sites per entry (identical base vs head). Raising
  `Ecto.Rollback` from a different module has the same effect: the exception unwinds the caller's
  `Repo.transaction/2` frame.
- **Transaction boundaries, lock order, Oban order.** The transaction-opening functions
  (`claim_with_disposition_in_transaction/3`, `attach_*`, `mark_harness_session_stopped/5`,
  `append_evidence/5`, `append_evidence_batch/5`, `record_usage/5`, `fetch_run_context/3`,
  `append_event!/8`, `replay_command/3`, `validate_contract/3` callers) keep their
  `Repo.transaction`/`Repo.in_transaction?` call sites verbatim (AST-identical); the row-lock
  helpers (`lock_goal!/1`, `lock_project!/1`, `lock_work_item!/1`, `lock_task!/1`, `lock_run!/1`,
  `lock_context_snapshot!/2`) keep their bodies and are still called in the same imperative order
  inside `lock_goal_run!/5` (`lock_goal!` → `lock_work_item!` → `lock_task!` → `lock_run!`) — that
  function is AST-identical except for the recorded widening. The `!` suffixes on the nine DbLocks
  helpers (`lock_command_goal!`, `lock_external_wait!`, `lock_work_items!` and `lock_decision!`
  included) are the contract's arch-decision-2 rename clause, applied in the apply stage (finding
  D3) as a recorded name-only change. `enqueue_wakeup!/2`,
  `enqueue_goal_control!/2`, `append_event!/8` keep their bodies and their relative order inside
  each entry.
- **The two ContextSnapshot-region widenings.** `context_wire_envelope/4` and `context_digest/1`
  keep their bodies inside the region (AST-identical, table above); only `defp` → `@doc false def`
  changes, exactly as the contract's "cross through one recorded `@doc false def` each" sentence
  requires. `context_wire_envelope!/4` moves out byte-identically as authorised.
- **Kernel identity of the six widenings.** All six are AST-identical base vs head (table above).
- **Formatting-only differences.** `mix format` reflowed the new `import …, only:` lists and the
  continuation indentation of multi-line `def` heads that were `defp` heads before (the formatter's
  own normalization); `git diff -w` and the AST comparison level both, and no expression changed.

| entry (destination) | base case/cond/with/if | base rollback short-circuits | head case/cond/with/if | head rollback short-circuits | base AST == head AST |
| --- | --- | --- | --- | --- | --- |
| `claim/3` → RunFence | 1/0/0/1 | 0 | 1/0/0/1 | 0 | yes |
| `claim_with_disposition/3` → RunFence | 1/0/0/2 | 0 | 1/0/0/2 | 0 | yes |
| `attach_harness_session/5` → RunFence | 2/0/1/1 | 2 | 2/0/1/1 | 2 | yes |
| `fetch_harness_session_attachment/3` → RunFence | 1/0/1/1 | 2 | 1/0/1/1 | 2 | yes |
| `mark_harness_session_stopped/5` → RunFence | 1/0/1/1 | 3 | 1/0/1/1 | 3 | yes |
| `append_evidence/5` → RunFence | 2/0/1/3 | 6 | 2/0/1/3 | 6 | no — the recorded `Goals.` + `lock_context_snapshot!` rewrites only |
| `append_evidence_batch/5` → RunFence | 0/0/1/0 | 0 | 0/0/1/0 | 0 | no — the recorded `Goals.` rewrite only |
| `record_usage/5` → RunFence | 2/0/1/2 | 3 | 2/0/1/2 | 3 | yes |
| `fetch_run_context/3` → RunFence | 2/0/1/1 | 2 | 2/0/1/1 | 2 | no — the recorded `lock_context_snapshot!` rename only |
| `validate_contract/3` → ContractValidation | 1/0/0/0 | 0 | 1/0/0/0 | 0 | no — the recorded qualifier rewrite only |
| `safe_document?/1` → Goals (widened) | 1/0/0/0 | 0 | 1/0/0/0 | 0 | yes |
| `safe_evidence_document/1` → Goals (widened) | 0/0/0/1 | 0 | 0/0/0/1 | 0 | yes |
| `context_wire_envelope/4` → Goals (widened) | 0/0/1/1 | 0 | 0/0/1/1 | 0 | yes |
| `context_digest/1` → Goals (widened) | 0/0/0/1 | 0 | 0/0/0/1 | 0 | yes |
| `settlement_task_owned_by_goal?/3` → Goals (widened) | 0/0/0/0 | 0 | 0/0/0/0 | 0 | yes |
| `reconcile_reservation!/3` → Goals (widened) | 0/0/0/2 | 0 | 0/0/0/2 | 0 | yes |

## Evidence plan (all gates FOREGROUND)

Recipe: `sh <HOST-PATH>/.local/state/gdd/scratch/rf-w4-07-mix.sh <mix args>` — portable Elixir 1.20.4 /
OTP 29 from `<HOST-PATH>/.codex/tools/symmetry/`, `MIX_ENV=test`, `MIX_TEST_PARTITION=rf-w4-07`
(own database `symmetry_control_testrf-w4-07`), `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR`
unset, one mix run at a time (the full suite alone).

| gate | command | expected |
| --- | --- | --- |
| G1 format | `mix format --check-formatted` | exit 0 |
| G2 compile | `MIX_ENV=test mix compile --warnings-as-errors` | exit 0, no warnings |
| G3 xref | `MIX_ENV=test mix xref graph --format cycles` and `--label compile-connected` | plain cycles recorded; no new compile-connected cycle versus base |
| G4 move-only | canonical per-function AST comparison base ↔ slice (`dump_fns.exs` + `compare3.py`) | superseded by the Results run below: 418 byte-identical + 77 unchanged `ContractValidation` + 43 under recorded edits, 0 missing |
| G5 targeted suites | `mix test test/symmetry_control/goals_test.exs test/symmetry_control/orchestration_goal_admission_test.exs test/symmetry_control/goals_read_model_test.exs` | exit 0; 147 + 77 + 51 = 275 tests |
| G6 new module tests | `mix test test/symmetry_control/goals/run_fence_test.exs test/symmetry_control/goals/events_receipts_test.exs` | exit 0; 6 cases; 6 recorded red mutants (the fix stage adds `goals/internal_test.exs`: 2 cases, 2 recorded red mutants) |
| G7 credo | `mix credo --strict` at the base blob and at the slice head | total count does not rise; no new `credo:disable` |
| G8 evidence freeze | `python scripts/ci/test-omo-control-native-evidence.py` | exit 0 with the four new `SOURCE_PATHS` entries |
| G9 citations | `python scripts/ci/check-citation-existence.py` | exit 0 (path-level; the CLOSED-TODO citations into the unmoved context region stay valid) |
| G10 whitespace / host paths | `git diff --check` over the committed range; host-path scan | exit 0; 0 hits |
| G11 full control suite | `mix test` (own partition, alone) | exit 0; counts recorded |

Caller coverage: the three oracle suites cover the facade-level behaviour; the two new focused files
pin the concept modules' own interfaces; the full control suite is the gate for every other `Goals.*`
caller (`daemon_controller.ex`, the three workers, the portal/goal/task controllers,
`orchestration_goal_admission_test.exs`, `goal_machine_controller_test.exs`,
`portal_goal_attention_test.exs`).

## Results (measured at the slice head)

### Move-only (G4)

Canonical per-function AST comparison, base blob `6ad78e29…` ↔ slice tree (line metadata stripped,
`defp` folded to `def`, newlines escaped):

| check | result |
| --- | --- |
| base functions compared | 461 (`goals.ex`) + 77 (existing `ContractValidation`) |
| byte-identical at the slice head (apply stage) | **418 + 77** |
| identical under a recorded edit | **43** — 7 with the `Goals.` prefixes (8 insertions), 9 contract-mandated `!` renames (bodies identical under the base name), 25 callers retargeted by those renames (identical under the `!` tokens), `nullable_safe_integer/2` (`@max_safe_integer` spelling, F5), `validate_contract/3` (qualifier rewrite, F6) |
| missing moved functions | **0** |
| ContractValidation diff | `added ['validate_contract/3']` (its recorded qualifier rewrite); base 77 unchanged, `changed []`, `gone []` |
| slice census | `goals.ex` 303 + RunFence 49 + HarnessSessions 26 + EventsReceipts 19 + Internal 63 + ContractValidation 78 = 538 = 461 + 77 |
| definition census | 158 moved declarations, 157 single-site, `value/3` two-site (pre-existing ContractValidation helper), **0** moved functions left in `goals.ex` |

### Control-flow audit (lesson 32)

| entry (destination) | base case/cond/with/if | base rollback short-circuits | head case/cond/with/if | head rollback short-circuits | base AST == head AST |
| --- | --- | --- | --- | --- | --- |
| `claim/3` → RunFence | 1/0/0/1 | 0 | 1/0/0/1 | 0 | yes |
| `claim_with_disposition/3` → RunFence | 1/0/0/2 | 0 | 1/0/0/2 | 0 | yes |
| `attach_harness_session/5` → RunFence | 2/0/1/1 | 2 | 2/0/1/1 | 2 | yes |
| `fetch_harness_session_attachment/3` → RunFence | 1/0/1/1 | 2 | 1/0/1/1 | 2 | yes |
| `mark_harness_session_stopped/5` → RunFence | 1/0/1/1 | 3 | 1/0/1/1 | 3 | yes |
| `append_evidence/5` → RunFence | 2/0/1/3 | 6 | 2/0/1/3 | 6 | no — the recorded `Goals.` + `lock_context_snapshot!` rewrites only |
| `append_evidence_batch/5` → RunFence | 0/0/1/0 | 0 | 0/0/1/0 | 0 | no — the recorded `Goals.` rewrite only |
| `record_usage/5` → RunFence | 2/0/1/2 | 3 | 2/0/1/2 | 3 | yes |
| `fetch_run_context/3` → RunFence | 2/0/1/1 | 2 | 2/0/1/1 | 2 | no — the recorded `lock_context_snapshot!` rename only |
| `validate_contract/3` → ContractValidation | 1/0/0/0 | 0 | 1/0/0/0 | 0 | no — the recorded qualifier rewrite only |
| `safe_document?/1` → Goals (widened) | 1/0/0/0 | 0 | 1/0/0/0 | 0 | yes |
| `safe_evidence_document/1` → Goals (widened) | 0/0/0/1 | 0 | 0/0/0/1 | 0 | yes |
| `context_wire_envelope/4` → Goals (widened) | 0/0/1/1 | 0 | 0/0/1/1 | 0 | yes |
| `context_digest/1` → Goals (widened) | 0/0/0/1 | 0 | 0/0/0/1 | 0 | yes |
| `settlement_task_owned_by_goal?/3` → Goals (widened) | 0/0/0/0 | 0 | 0/0/0/0 | 0 | yes |
| `reconcile_reservation!/3` → Goals (widened) | 0/0/0/2 | 0 | 0/0/0/2 | 0 | yes |

### Red mutants for the six new test cases (G6)

| mutant | edit at the slice head | test that fails |
| --- | --- | --- |
| M1 | `claim_with_disposition(_, _, _)` verdict `:invalid_request` → `:invalid_call` (`run_fence.ex`) | `claim_with_disposition rejects malformed inputs` |
| M2 | the ownership lookup's `rollback(:not_found)` → `rollback(:invalid_request)` (`run_fence.ex`) | `claim_with_disposition reports an unknown run` |
| M3 | `fetch_run_context`'s `:ok <- valid_fence(fence)` → `:ok <- :ok` (`run_fence.ex`) | `fetch_run_context rejects malformed fences` |
| M4 | `replay_command`'s `nil -> :missing` → `nil -> {:error, :not_found}` (`events_receipts.ex`) | `replay_command reports a missing mutation` |
| M5 | `nullable_datetime(nil)` → `""` (`events_receipts.ex`) | `nullable_datetime renders absent and present timestamps` |
| M6 | `valid_receipt_utc?`'s Z-suffix test → `true` (`events_receipts.ex`) | `valid_receipt_utc? accepts only Z-suffixed UTC instants` |

Each mutant was applied alone to the slice tree, the focused file was run, the failing case was
recorded, and the file was restored (all six runs exit 2; the restored tree re-runs exit 0).

### File-size cap (contract gate "new modules ≤ 1,500 L")

Met at the round-1 head (`83a924d4` + the review-round-1 split recorded below). Sizes as `wc -l`
of the delivered files:

| file | pre-round-1 head `83a924d4` | round-1 head | moved block LOC | note |
| --- | --- | --- | --- | --- |
| `control/lib/symmetry_control/goals.ex` | 8,119 | **8,121** | — | shrinks from base 10,606 by 2,485 (gate: "must not grow" ✓); +2 at the round-1 head from the split's import/alias/delegate lines |
| `control/lib/symmetry_control/goals/run_fence.ex` | 1,823 (1,828 before apply) | **1,257** | 1,149 | harness-session split (round 1): 563 lines out; the six `@doc false` widenings in |
| `control/lib/symmetry_control/goals/harness_sessions.ex` | — | **610** | 548 | new in round 1: the 26 harness-session functions, 15 blank separators, 47 header lines |
| `control/lib/symmetry_control/goals/internal.ex` | 477 (480 before apply) | 477 | 387 | under |
| `control/lib/symmetry_control/goals/events_receipts.ex` | 362 (366 before apply) | 362 | 322 | under |
| `control/lib/symmetry_control/goals/contract_validation.ex` | 1,103 → 1,155 | 1,155 | +50 | under |

History: at `83a924d4` `run_fence.ex` was 1,823 lines — 323 over the cap. The bound RunFence
membership is the k2 RunFence cluster (74 functions) plus `context_wire_envelope!/4`; measured on
block spans that is 1,697 lines before the module header, imports, aliases and inter-function
blanks, so no formatting or comment trim could close the gap, and the contract's k2 estimate
("~1,461 L") was a clause-span count that excludes the attached `@spec`/comment headers and blank
lines. The pre-round-1 owned-files list named exactly three new module files, so the cap could not
be met without a fourth module; the slice recorded the conflict instead of resolving it silently.

The conductor's `Conductor amendment (2026-11-22, PR #233 review round 1)` authorises the recorded
split: the harness-session family of RunFence becomes
`SymmetryControl.Goals.HarnessSessions` — the 26 functions
`attach_harness_session/5`, `fetch_harness_session_attachment/3`,
`mark_harness_session_stopped/5`, `session_attrs/1`, `session_stop_attrs/1`, `session_stop_request/2`,
`session_attach_request/2`, `ensure_stoppable_session!/5`, `stop_receipt_matches?/4`,
`ensure_session_scope!/6`, `ensure_current_attachment!/7`, `ensure_attachment_binding_shape!/3`,
`attach_fresh_session!/7`, `attach_handoff_session!/7`, `reject_existing_handle!/3`,
`create_fresh_harness_session!/7`, `attach_reserved_session!/6`, `attach_session_to_run!/3`,
`lock_attach_receipt/1`, `attach_receipt_matches?/4`, `attach_receipt_fence_matches?/3`,
`create_attach_receipt!/8`, `attachment_receipt_response/1`, `mark_harness_session_unavailable!/2`,
`session_receipt/5`, `session_stop_receipt/4` (548 block lines plus their blank separators, a named
k2 sub-group: everything the session attach/stop receipt path owns), move-only, with its
`HarnessSessions → Goals.RunFence` interface recorded in the cross-edge table. Measured sizes
1,257 / 610 both sit under the cap; see "Fix round 1 (2026-11-22, PR #233 review) — the
HarnessSessions split" below for the move proof.

### Row-lock sequence (static, base ↔ head)

| entry | lock helper calls inside its own body (base) | (head) | identical |
| --- | --- | --- | --- |
| `claim/3` | — | — | yes |
| `claim_with_disposition/3` | — | — | yes |
| `attach_harness_session/5` | — | — | yes |
| `fetch_harness_session_attachment/3` | — | — | yes |
| `mark_harness_session_stopped/5` | — | — | yes |
| `append_evidence/5` | lock_context_snapshot! | lock_context_snapshot! | yes |
| `append_evidence_batch/5` | — | — | yes |
| `record_usage/5` | — | — | yes |
| `fetch_run_context/3` | lock_context_snapshot! | lock_context_snapshot! | yes |
| `settlement_task_owned_by_goal?/3` | — | — | yes |
| `reconcile_reservation!/3` | — | — | yes |
| `context_wire_envelope/4` | — | — | yes |
| `context_digest/1` | — | — | yes |
| `lock_goal_run!/5` (the acquisition choreographer the entries call) | lock_goal! → lock_work_item! → lock_task! → lock_run! | lock_goal! → lock_work_item! → lock_task! → lock_run! | yes |

The ordered acquisition of `docs/design/data.md` (`Project → Goal → WorkItem → Task → Run`) is owned by
`lock_goal_run!/5`, whose body is AST-identical base vs head apart from the recorded widening; every moved
entry's own lock call is listed above and is identical on both sides. `append_event!/8` acquires its
receipt/row locks through inline `lock: "FOR UPDATE"` selects whose text is part of its AST-identical body.

### Credo (G7)

| revision | warnings | refactoring | readability | design | total |
| --- | --- | --- | --- | --- | --- |
| base `b25e34ae` (fresh worktree of the base blob, dev env, 163 files) | 5 | 357 | 77 | 53 | **492** |
| slice head `936cbde7` (168 files: the 5 new files) | 5 | 357 | 74 | 54 | **490** |
| shipped head `0f80b4ac` (168 files; 169 with the fix-stage `internal_test.exs`, which adds 0 issues) | 5 | 357 | 82 | 54 | **498** |
| pre-round-1 head `83a924d4` (fresh worktree of the commit, dev env, 170 files) | 5 | 357 | 82 | 56 | **500** |
| round-1 head (this tree, 171 files: `harness_sessions.ex` added) | 5 | 357 | 82 | 56 | **500** |

The last four rows are measured runs on fresh worktrees of those revisions (base and slice head
re-run in the fix stage); the round-1 head is measured in the delivered tree. Credo's own exit code
saturates (30 on every revision); the summary line is the count. The slice-attributable delta is
base → slice head: **−2** (77 → 74 readability). The review-round-1 split's own delta is **0**:
`83a924d4` and the round-1 head both report 500 (5W/357F/82R/56D), with the same 24 `DuplicateCode`
pairs and the same 32 nested-alias findings; only three reported `DuplicateCode` locations shift
(`goals.ex` 3361→3363 and 5752→5754 from the +2 facade import lines, `run_fence.ex` 159→152 from the
−7 lines above the site). The +2 design between the 498 at `0f80b4ac` and the 500 at `83a924d4` is
merge-attributable (main commits in between). The shipped
head is 8 higher than the slice head in readability (net of +9 in merged-in files and −1 in
`portal_resources_test.exs`), all of it in non-slice files the final main merge brought in —
`+1` `test/support/slowest_formatter.ex`, `+1`
`test/support/test_fixtures.ex`, `+1` `goals/changeset_validators_test.exs`, `+2`
`integrations_test.exs`, `+1` `provider_broker_e2e_test.exs`, `+3`
`goal_controller_test.exs`, `−1` `portal_resources_test.exs`; none of those files is in the slice
diff, so the shipped-head change is merge-attributable, not slice-attributable. The per-message delta
for base → slice head is fully enumerated by the two runs:

- `[R]` 77 → 74: three `Credo.Check.Readability.AliasOrder` findings that lived in `goals.ex`'s alias
  block disappear (`SymmetryControl.Goals.GoalExternalWait`, one of five
  `SymmetryControl.Orchestration.RunTransition`, `SymmetryControl.Orchestration.Scheduler`) because the
  rebuilt alias list is sorted case-insensitively; no new `[R]` finding appears in that step (the
  shipped-head additions are the merge-attributable files listed above).
- `[D]` 53 → 54: `Credo.Check.Design.DuplicateCode` re-attributes one pair location
  (`goals.ex:210`/`10416`/`7358`/`8560` → `goals.ex:3363`/`5754`, `internal.ex:123`/`392`,
  `run_fence.ex:159`; re-run in the apply stage: same pairs and counts). The duplicated text is the
  same base text — every moved body is AST-identical — so the extra location is a report artifact
  of the pair now spanning two files, not new duplication.
- `[W]` and `[F]` are unchanged at 5 and 357.

No `credo:disable` or any other suppression is added: the diff contains no new suppression comment
(checked by scanning the changed and new files for `credo:disable`).

### Evidence freeze and citations (G8, G9)

- `python scripts/ci/test-omo-control-native-evidence.py` → 10 tests, `OK`, exit 0, with
  `control/lib/symmetry_control/goals/{run_fence,events_receipts,internal,contract_validation}.ex`
  added to `SOURCE_PATHS`.
- `python scripts/ci/check-citation-existence.py` → exit 0 with the printed notice
  `no audited document added or modified between 4fdea989a698 and 2a3fb894b124; nothing to check`
  (the audit is path-level and `control/lib/symmetry_control/goals.ex` exists at the slice head;
  the notice above is the apply-stage re-run at the committed range).

### Whitespace and host paths (G10)

- `git diff --check 403142114..0f80b4ac` → exit 2 at review time: `plan:72` trailing whitespace and
  a blank line at EOF (`plan:1103`); both deleted in the fix stage, so `git diff --check 403142114`
  (base ↔ fix-stage tree) → exit 0. The argument-less form recorded by the apply stage ran on a
  clean committed worktree and was vacuously empty.
- host-path scan of every changed and new file (drive-letter Users-directory spellings, per the
  program rule; re-run in the apply stage on the working tree) → 0 hits; the plan and the slice
  files use `<HOST-PATH>` for toolchain coordinates.
### Format, compile and xref (G1, G2, G3)

| gate | command | result |
| --- | --- | --- |
| G1 | `mix format --check-formatted` | exit 0 |
| G2 | `MIX_ENV=test mix compile --warnings-as-errors` | exit 0, no warnings |
| G3 plain | `MIX_ENV=test mix xref graph --format cycles` | exit 0, 3 cycles: the pre-existing 25-module web cycle, the 4-module schemas cycle, and the expected runtime cycle `goals.ex ↔ goals/{run_fence,events_receipts}.ex ↔ goals/workers/* ↔ orchestration* ↔ portal/invalidations.ex` (10 modules, 2 export edges) |
| G3 compile-connected | `MIX_ENV=test mix xref graph --format cycles --label compile-connected` | exit 0, **No cycles found** at the slice head and at a fresh base worktree of `b25e34ae` (both `No cycles found`) — no new compile-connected cycle |
| apply re-run | G1–G3 re-ran green after the apply-stage edits: format exit 0, compile `--warnings-as-errors` exit 0 (0 warnings), plain xref cycles unchanged (3, same members), compile-connected `No cycles found` |

The runtime cycle is the one the contract anticipates: `goals.ex` imports `Internal`/`EventsReceipts`/
`RunFence`, and those modules call back through the facade only at the six recorded widenings, so the
cycle exists at runtime (exports) but not at compile time.

### Behaviour suites (G5, G6)

| gate | command | result |
| --- | --- | --- |
| G5 targeted | `mix test test/symmetry_control/goals_test.exs test/symmetry_control/orchestration_goal_admission_test.exs test/symmetry_control/goals_read_model_test.exs` | exit 0, **275 passed** (147 + 77 + 51; the contract's "273 at head / 274 post-#162" baseline is stale at this HEAD — measured 275) |
| G6 new tests | `mix test test/symmetry_control/goals/run_fence_test.exs test/symmetry_control/goals/events_receipts_test.exs` | exit 0, **6 passed** |
| apply re-run | G5 and G6 re-ran after the apply-stage edits: **275 passed** and **6 passed**, exit 0 |

The oracle files are byte-identical to the base blob (no test was edited, moved, renamed or
weakened): `git diff` shows no change to `goals_test.exs`, `orchestration_goal_admission_test.exs` or
`goals_read_model_test.exs`.

### Full control suite (G11)

`mix test` (own partition, run alone): **996/1000 passed, 21 skipped, 4 failed**, exit 2. All four
failures are `SymmetryControlWeb.PortalControllerTest` `:portal_assets` cases and fail in `ConnCase`
setup with `the portal bundle that priv/static/portal/ must hold is not usable: the staged entry
document is missing` — the documented host prerequisite for this fresh worktree (the gitignored
frontend bundle; the failure text itself prescribes `mix test --exclude portal_assets` on a host
without Node/pnpm, and CI stages the bundle in the `control` job). The same four cases are the known
host artifact recorded for rf-w2-08 (`docs/plans/2026-11-21-rf-w2-08-read-model-projection-plan.md`).

`mix test --exclude portal_assets` (the prescribed host remedy, run alone): **996 passed, 21 skipped,
4 excluded**, exit 0 — no failure outside the four documented `:portal_assets` prerequisite cases.


### Apply stage (2026-11-22): accepted simplifications, refreshed numbers

The apply stage executed the simplify review's accepted findings: F1 (7 comment blocks trimmed to the
owner's ≤2-line policy; −12 lines), F2/F3/F4 (77 functions / 97 clause heads restored to their base
`defp` visibility — 64 in `run_fence.ex`, 6 in `internal.ex`, 7 in `events_receipts.ex`; a
name+arity census over `control/lib` + `control/test` finds no caller outside each file), F5
(`nullable_safe_integer/2` spells `@max_safe_integer`), F6 (`validate_contract/3`: `alias __MODULE__`
and its 12 qualifiers removed) and D3 (the nine contract-mandated `!` lock-helper renames — name-only,
across 77 lines: 9 heads, 14 `import …, only:` entries, 54 call sites) plus the record corrections
D2/D5 (this file) / D6 (Out of scope). Not applied at the apply stage: F7 (then outside the owned
set; applied in the fix stage under the conductor's 2026-11-22 amendment, see the fix-stage section)
and D1 (needs a conductor ruling or a fourth module). Sizes at the apply head `936cbde7`:
`goals.ex` **8,121** (8,119 at the shipped head, −2 from the final main merge); `run_fence.ex`
**1,823**; `internal.ex` **477**; `events_receipts.ex` **362**; `contract_validation.ex` **1,155**.

Re-run gates at the apply tree (all foreground, serialized): format exit 0; `compile
--warnings-as-errors` exit 0 (0 warnings); xref plain exit 0 (same 3 cycles) and compile-connected
`No cycles found`; targeted suites **275 passed**; new module tests **6 passed**; `credo --strict`
**490** (5W/357F/74R/54D) at `936cbde7` versus base 492, no new issue
kinds (shipped head `0f80b4ac`: **498** = 5W/357F/82R/54D — see Credo (G7)); R9 evidence test `OK`;
citation check `nothing to check`; `git diff --check` 0 on the worktree; host-path
scan 0 hits (the G10 row's literal drive-path spelling is replaced by a descriptive one);
backslash-u scan 0; per-function AST comparison with recorded-edit normalisation: **418
byte-identical + 43 under recorded edits, 0 missing, 0 duplicated**.

R2 per-file line coverage (`mix test --cover`, full suite, base worktree `4fdea989` vs the apply
tree; the CI coverage plane is advisory by construction, `ci.yml`):

| module (file) | base `4fdea989` | apply tree |
| --- | --- | --- |
| `SymmetryControl.Goals` (`goals.ex`) | 86.49% (3,234/3,739) | **86.32% (2,392/2,771)** |
| `SymmetryControl.Goals.RunFence` (`run_fence.ex`) | — (new file) | **87.29% (611/700)** |
| `SymmetryControl.Goals.EventsReceipts` (`events_receipts.ex`) | — (new file) | **93.58% (102/109)** |
| `SymmetryControl.Goals.Internal` (`internal.ex`) | — (new file) | **79.61% (121/152)** |
| `SymmetryControl.Goals.ContractValidation` (`contract_validation.ex`) | 84.15% (276/328) | **84.59% (291/344)** |
| suite total | 87.17% | **87.17%** |

Moved-code accounting: the base `goals.ex` covered **817** of the 2,406 relevant net lines inside the
moved spans; the three new modules cover **834** of their 2,662 relevant net lines (the extra
relevant lines are each module's own header/import scaffolding), and `validate_contract/3` is
15-covered on both sides (base span 10050–10099: 15/50; head: 15/52). `contract_validation.ex`
closes exactly (276 base-covered + 15 = 291); `goals.ex` itself moves 86.49% → 86.32% (−0.17 pt) —
a denominator/attribution effect of the 2,456 moved block lines, not a lost test (the oracle suites
are unchanged and green, and the suite total is identical at 87.17% on both sides). This is the
per-file R2 measurement the implement stage left unimplemented.

Full suite with cover on the apply tree: **996/1000 passed, 21 skipped** (exit 2; the same
four documented `:portal_assets` host-prerequisite cases). Base `4fdea989`:
990/994 passed, 21 skipped (exit 2, the same four cases).

### Fix stage (2026-11-22, review r1 findings)

All ten review findings on the plan/records are applied: sizes and totals re-derived at the shipped
tree (STD-A01/A02/A03), the G4 row replaced by the measured counts (STD-A04), the committed-range
whitespace errors deleted with G10 recorded as a range check (STD-A05), the apply-stage records
corrected to −12 comment lines and 77 restored functions (STD-A06/A07), the literal backslash-u
phrase removed (STD-A08), the D6 line references refreshed and `docs/TODO.md:1237` dropped
(STD-A09), and the credo table re-measured on fresh worktrees of base, slice head and shipped head
(STD-A10).

F7 (conductor amendment 2026-11-22) is applied: `control/test/symmetry_control/goals/internal_test.exs`
pins the `has_key?/2` vs `map_has_key?/2` direction asymmetry in `Goals.Internal`; each case carries
its red mutant in the case name, and every mutant was applied alone and reverted (`internal.ex` is
byte-identical to `936cbde7` afterwards):

| case | red mutant | observed |
| --- | --- | --- |
| `has_key?/2 resolves an atom key against a string-keyed map` | the two bodies swapped | both cases fail, exit 2 |
| `map_has_key?/2 resolves a string key against an atom-keyed map` | the two bodies swapped | both cases fail, exit 2 |
| both | merged (both bodies = `has_key?/2`'s) | the second case fails, exit 2 |

Fix-stage gates (all foreground, serialized, shipped tree): `mix format --check-formatted` exit 0;
`MIX_ENV=test mix compile --warnings-as-errors` exit 0 (0 warnings); `MIX_ENV=test mix xref graph
--format cycles` exit 0 (same 3 cycles, same members) and `--label compile-connected` exit 0
(`No cycles found`); targeted suites **275 passed** exit 0; new-module tests (the two files above
plus `internal_test.exs`) **8 passed** exit 0; `mix credo --strict` **498** (5W/357F/82R/54D) exit
30, no new `credo:disable`; `python scripts/ci/test-omo-control-native-evidence.py` exit 0 (10 tests,
`OK`); `python scripts/ci/check-citation-existence.py --base 403142114 --head 0f80b4ac` exit 0
(`nothing to check`); `git diff --check 403142114` exit 0; host-path scan of the branch diff 0 hits;
backslash-u scan of the added lines 0 hits. The full control suite (G11) is the conductor's gate and
was not re-run here (documentation/records fix plus one unit test file).

### Fix round 1 (2026-11-22, PR #233 review) — the HarnessSessions split

Applied per the conductor's `Conductor amendment (2026-11-22, PR #233 review round 1)`: the review's
P1-1 (the `run_fence.ex` 1,823 > 1,500 cap), P2-1 (this record and the PR body regenerated from the
delivered bytes), P2-2 (R2 coverage judged over the moved code's union), P3-1 (the cross-edge table
above now cites symbols, not base line numbers), P3-2 (the `reconcile_reservation!/3` comment is two
lines) and P3-3 (the state-set duplication is recorded in `docs/TODO.md`). No behavioural change, no
test change, no public `Goals` API change; the three new test files, the oracle files and the
`SOURCE_PATHS` gate are untouched apart from the new `harness_sessions.ex` path.

**The split (move-only).** `Control/lib/symmetry_control/goals/harness_sessions.ex` (610 lines)
receives the 26 harness-session attach/fetch/stop functions and their private helpers from
`run_fence.ex` (1,823 → 1,257 lines), verbatim. Base-source spans 223–390 (the three public
entries) and 837–1231 (the 23 helpers) move; the two new blank-line junctions are the only line
edits. Interface the new module needs:

| edge | functions (name/arity pairs as emitted) | mechanism |
| --- | --- | --- |
| `HarnessSessions` → `Goals.RunFence` | 6 (`ensure_machine_fence!: 7`, `lock_goal_run!: 5`, `machine_session_stop_result: 1`, `machine_write_result: 1`, `task_repository_resource_id!: 2`, `valid_fence: 1`) | `import …, only:` in HarnessSessions; the six are `@doc false def`s in RunFence |
| `HarnessSessions` → `Goals.Internal` | 13 names / 14 pairs (`value: 2` + `value: 3`) | kernel `import …, only:` |
| `goals.ex` → `Goals.HarnessSessions` | 1 (`mark_harness_session_unavailable!/2` for `settle_task/4`) | `import …, only:` in the facade |
| `goals.ex` → `Goals.HarnessSessions` (public) | 3 `defdelegate`s retargeted (`attach_harness_session/5`, `fetch_harness_session_attachment/3`, `mark_harness_session_stopped/5`) | facade delegates unchanged in name/arity/defaults |
| `Goals.RunFence` → `Goals.HarnessSessions` | none | no staying RunFence function calls into the new module |

The plan's pre-round-1 estimate ("three more recorded cross edges, `RunFence → HarnessSessions`")
was direction-inverted and undercounted; the measured interface is the six `HarnessSessions →
RunFence` names above, and the review's own P3-3 note (attributes travelling) applies:
`@terminal_run_states` / `@accounting_terminal_run_states` travel into HarnessSessions and are
removed from RunFence (they had no staying user there), forced by `--warnings-as-errors`.

**Census (same tool/method as G4, re-run at the round-1 tree).** `parse_defs.exs` (clause spans) plus
the canonical per-function AST dump (`dump_fns.exs`, line metadata stripped, `defp` folded to
`def`) over the base blob `6ad78e29…` (`b25e34ae` `goals.ex`, 461 functions) + the base
`ContractValidation` (77) and the six head files:

| check | result |
| --- | --- |
| head file census | `goals.ex` 303 + `run_fence.ex` 49 + `harness_sessions.ex` 26 + `events_receipts.ex` 19 + `internal.ex` 63 + `contract_validation.ex` 78 = **538 = 461 + 77** |
| base functions accounted for | **418 byte-identical + 43 identical under a recorded edit = 461**; 0 missing after the rename map, 0 moved functions duplicated |
| the 43 | 7 `Goals.`-prefix widenings (8 insertions), 9 contract-mandated `!` lock renames, 25 callers retargeted by those renames, `nullable_safe_integer/2` (`@max_safe_integer` spelling), `validate_contract/3` (qualifier rewrite) |
| moved declarations | **158** (157 single-site; `value/3` two-site — the pre-existing `ContractValidation` private helper, byte-identical base ↔ head); destinations 63 Internal / 49 RunFence / 26 HarnessSessions / 19 EventsReceipts / 1 ContractValidation |
| the 26 harness functions | **26/26 byte-identical to their base blobs**; the set removed from `run_fence.ex` equals the set present in `harness_sessions.ex` exactly |
| ContractValidation | base 77: `changed []`, `gone []`, `added ['validate_contract/3']` |

**Control-flow audit (lesson 32), entries whose body text changed.** None: the 26 moved bodies are
AST-identical to base (the six cross-edge callees resolve through `import`, so not one call site
changes text), and the six `@doc false` widenings only change visibility. Every early return,
fall-through, `Repo.rollback` short-circuit and default clause therefore carries over 1:1; the
pre-round-1 per-entry table above stays the audit for the other entries.

**File sizes.** `goals.ex` 8,119 → 8,121; `run_fence.ex` 1,823 → **1,257**; new
`harness_sessions.ex` **610**; `internal.ex` 477; `events_receipts.ex` 362; `contract_validation.ex`
1,155. All new modules ≤ 1,500 lines (P1-1 closed).

**R2 coverage union (P2-2).** `mix test --cover` (full suite, own partition), base worktree at
`108b2d2b` (the merge-base with origin/main) vs the round-1 tree; the union is `goals.ex` plus every
`goals/*.ex` module that receives `goals.ex` code (`contract_validation.ex` exists at base; the four
new modules do not, so their base share is the goals.ex lines they later receive):

| module (file) | base `108b2d2b` | round-1 head |
| --- | --- | --- |
| `SymmetryControl.Goals` (`goals.ex`) | 86.47% (3,233/3,739) | 86.32% (2,392/2,771) |
| `SymmetryControl.Goals.RunFence` (`run_fence.ex`) | — (new file) | 84.84% (414/488) |
| `SymmetryControl.Goals.HarnessSessions` (`harness_sessions.ex`) | — (new file) | 92.92% (197/212) |
| `SymmetryControl.Goals.EventsReceipts` (`events_receipts.ex`) | — (new file) | 93.58% (102/109) |
| `SymmetryControl.Goals.Internal` (`internal.ex`) | — (new file) | 79.61% (121/152) |
| `SymmetryControl.Goals.ContractValidation` (`contract_validation.ex`) | 84.15% (276/328) | 84.59% (291/344) |
| **union** | **86.28% (3,509/4,067)** | **86.29% (3,517/4,076)** |
| suite total | 87.15% (999/1003 passed, 21 skipped, the 4 documented `:portal_assets` host-prerequisite failures) | 87.19% (1,007/1,011 passed, 21 skipped, the same 4 failures) |

Moved-code subset, counted over the same 158 declarations' own clause spans (base
`goals.ex` vs their head modules): 850/977 → 849/977 covered. Nine lines across seven functions
flip class — eight `def` clause-header lines (the default-argument header and terminal fallback
clauses of `append_evidence/5`, `append_evidence_batch/5`, `attach_harness_session/5`,
`claim_with_disposition/3`, `mark_harness_session_stopped/5`, `record_usage/5`) and one `_ -> false`
case arm of `valid_receipt_utc?/1` — while the relevant-line sets are identical (977 = 977) and every
moved *body* line keeps its class: a cover line-marker attribution artefact of the module move, not
lost coverage. The union does not drop, so no test was added (R2 met); the four new modules' scaffold
lines (module header, imports, aliases) are the only new uncovered lines in the union.


**Round-1 gates (all foreground, serialized; the full suite is the conductor's).** format exit 0;
`MIX_ENV=test mix compile --warnings-as-errors --force` exit 0 (0 warnings); `mix xref graph
--format cycles` exit 0 (same three cycles, HarnessSessions joins the expected runtime cycle) and
`--label compile-connected` exit 0 (`No cycles found`; plain run: the expected runtime cycle is
now 11 modules / 3 export edges with `harness_sessions.ex` joining it, the 25-module web cycle and
the 4-module schemas cycle unchanged); targeted oracle suites
`goals_test.exs`+`orchestration_goal_admission_test.exs`+`goals_read_model_test.exs` **275 passed**
exit 0; the three new test files **8 passed** exit 0; `mix credo --strict` **500** (5W/357F/82R/56D) exit 30; the pre-round-1 commit `83a924d4` measures
the same 500 with the same 24 `DuplicateCode` pairs, so the split's own credo delta is 0 and no new
`credo:disable` appears; `python scripts/ci/test-omo-control-native-evidence.py` 10 tests `OK` exit 0
with the `harness_sessions.ex` path added; `python scripts/ci/check-record-cleanliness.py --base
108b2d2b… --head <round-1 head>` exit 0; `git diff --check` exit 0; host-path and backslash-u scans
of the branch diff 0 hits; full `mix test` 1,007/1,011 passed, 21 skipped, exit 2 — the same four documented `:portal_assets` cases
(no staged portal bundle on this host; CI stages it in the `control` job); the `mix test --exclude
portal_assets` host remedy (and the base-worktree cover run, 999/1003) is recorded in the round-1
findings report.

## Design trade-offs (ambiguity resolution)

1. **"the map/rollback half of PayloadContracts" is a description, not a line range.** The binding
   phrase does not name members, so membership is derived mechanically instead: a
   PayloadContracts/DbLocks member enters `Goals.Internal` iff (a) a moving module calls it, or (b)
   two or more non-moving clusters call it, closed over (c) every kernel member those call, minus the
   two functions the contract places elsewhere (`validate_contract/3` → ContractValidation,
   `context_digest/1` → ContextSnapshot region). Result: 63 functions. The narrower reading ("only
   `value`/`rollback`/`stamp_*`/`now`") would turn ~250 call sites inside the moving modules into
   facade back-edges — larger and strictly worse than the six recorded widenings this rule produces.
   The measured fixpoint closes with exactly one residual edge, the contract's recorded
   `parse_subject/1` → `valid_commit?/1`.
2. **`valid_commit?/1` moves with its sole caller.** Its only caller is `parse_subject/1`, which is
   kernel by (b) (six clusters call it). Moving both into `Goals.Internal` resolves the recorded
   kernel → PlanContracts edge; W5-14 re-derives its PlanContracts membership without it, exactly as
   its own contract instructs. Leaving it behind would add a seventh widening for a 6-line predicate.
3. **The two Settlement-coupled helpers stay in `goals.ex` and are widened**
   (`settlement_task_owned_by_goal?/3`, `reconcile_reservation!/3`): each has a Settlement-only caller
   beside the RunFence one, so W5-14's Settlement membership stays intact and the contract's "every
   other RunFence ↔ Settlement edge crosses through one recorded `@doc false def`" sentence is
   followed literally.
4. **Import, not qualified calls, for kernel access.** E2 allows callers outside the owned regions to
   gain only an `import …, only:`/alias line, and every moved body must stay byte-identical; both
   force `import` in `goals.ex` and in the new modules. The cost is that ownership of a call is less
   visible at the call site; the alternative (rewriting ~300 call sites inside moved bodies) would
   destroy the move-only property this slice is judged on.
5. **The 19 STAY helpers keep their bodies and their `defp` visibility** — `context_digest/1`,
   `lock_settlement_tasks!/2`, `lock_settlement_runs!/3`, `valid_event_options/1`,
   `valid_attention_options/1`, `validate_goal_contract/3`, `optional_digest!/1`, `optional_digest!/2`,
   `optional_string/2`, `require_reason!/1`, `required_nonnegative_integer/2`,
   `required_safe_nonnegative_integer/2`, `required_safe_positive_integer!/2`, `required_digest!/2`,
   `digest!/1`, `policy_nonnegative_integer/3`, `dependency_action_hash/4`, `valid_actor/1`,
   `optional_string_strict!/2` (exactly the STAY table above). Each is called from exactly one
   non-moving region, so it stays that region's private helper until W5-14 moves that region.
6. **No dead-code deletion, and no dead code to delete.** Measured at HEAD: 0 private functions have
   zero inbound edges (the tracer's arity canonicalisation matters here — `optional_uuid!/1` and
   `optional_digest!/1` look unreferenced only if a call to an explicitly defined lower arity is
   folded into the higher one). Nothing is deleted, so no `docs/TODO.md` entry is opened or closed by
   this slice.
7. **Facade top matter changes only where the compiler demands it**: four `import …, only:` lines
   added; three aliases dropped because the compiler reports them unused; `@max_evidence_batch_items`
   dropped because it is unused after the move; alias order made case-insensitive (Credo
   `AliasOrder`). Recorded here because E2's letter says "only an `import …, only:`/alias line".
8. **Tests.** The oracle suites are unchanged (`goals_test.exs` 147, `orchestration_goal_admission_test.exs`
   77, `goals_read_model_test.exs` 51 = 275 tests, all green at the slice head, same names). The two
   new focused files drive `Goals.RunFence` and `Goals.EventsReceipts` through their own interfaces at
   their own seam (Ecto sandbox) with six cases and six red mutants recorded (M1–M6, one per case).
   No existing case moved, so `goals_test.exs`'s writer chain is untouched and W5-14's title multiset
   is unaffected.
9. **W5-14 boundary.** W5-14 consumes `Goals.Internal`, `Goals.EventsReceipts` and `Goals.RunFence`;
   the widenings and the STAY set are its own to re-derive when it moves the remaining regions. Its
   `defp` → `@doc false def` widening allowance applies to the three new modules.
10. **`validate_contract/3` qualifier rewrite (superseded in the apply stage).** The moved body
    originally kept its `ContractValidation.validate_*` text and resolved it with `alias __MODULE__`.
    The apply stage accepted the simplify finding (F6): the alias and the 12 qualifiers are replaced
    by direct local calls (−2 lines, −12 qualifiers), recorded as a body edit like the `Goals.`
    insertions; all 12 callees are defined in the same module, so resolution is unchanged.

## Out of scope (with reasons)

- Settlement, Recovery, Reconciliation, Admission, Commands, Decisions, PlanContracts, ContextBuild
  and Reads bodies — W5-14. This slice touches them only through `import …, only:` lines.
- The ContextSnapshot region as code: all its bodies stay; only the visibility keyword on
  `context_wire_envelope/4` and `context_digest/1` changes, and `context_wire_envelope!/4` moves out
  byte-identically as the contract authorises.
- Every external caller (`daemon_controller.ex`, the three workers, the portal/goal/task
  controllers), the Go daemon, `goals/read_model.ex`, `goals/schemas.ex`, `orchestration/*`,
  `goal_machine_controller_test.exs` and the CI pins: untouched; they keep calling the facade.
- No behaviour change, no new feature, no test rewrite, no `credo:disable`, no `SOURCE_PATHS` entry
  beyond the four files R9 lists.
- Known residual rot (recorded, not fixed because the owned set is strict): `docs/TODO.md` lines
  141–142 and 1231 cite `goals.ex` line numbers that shift with this move (path-level citations
  stay valid; the archived `docs/archive/CLOSED-TODO.md` citations are path-level by the citation
  gate's own rule and stay valid too).
- Cross-module duplicates reachable from the moved code (recorded, not fixed — outside the owned
  set; apply-stage finding D6): `same_uuid?/2` (`run_fence.ex:1803-1812` vs `read_model.ex:2200-2207`
  vs `schemas.ex:1283-1292`; the `run_fence` copy's extra `nil` clause is equivalent),
  `microusd_wire_value/1` (`run_fence.ex:1628-1629` — `nil → nil` — vs `read_model.ex:2578-2579`,
  value-passthrough fallback), `claim/3` (`orchestration.ex:1948`) and `valid_fence?/1`
  (`orchestration.ex:4264`) as orchestration-side twins. The simplify review's claim that the
  `schemas.ex` `same_uuid?/2` copy and the `orchestration.ex` `valid_fence?/1` twin do not exist is
  itself wrong — both were re-verified present. The `has_key?/2` vs `map_has_key?/2` direction
  asymmetry is pinned by `control/test/symmetry_control/goals/internal_test.exs` (fix stage, the
  conductor's 2026-11-22 F7 amendment; each case records its red mutant); do not merge the two
  predicates.
