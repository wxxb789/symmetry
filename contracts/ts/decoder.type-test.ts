import type { Effect, ParseResult } from "effect";
import type {
  SymmetryAdapterCapabilitiesV1,
  SymmetryAdmissionV1,
  SymmetryContextSnapshotV1,
  SymmetryDecisionV1,
  SymmetryEvidenceBatchConflictDetailsV1,
  SymmetryEvidenceBatchResponseV1,
  SymmetryEvidenceBatchV1,
  SymmetryEvidenceV1,
  SymmetryGoalCommandV1,
  SymmetryGoalCreateV1,
  SymmetryGoalRevisionV1,
  SymmetryPlanProposalV1,
  SymmetryTaskResultV1,
  SymmetryUsageV1,
} from "../generated/ts/index.js";
import type {
  AdapterCapabilities,
  Admission,
  ContextSnapshot,
  Decision,
  EvidenceBatchConflictDetails,
  EvidenceBatchResponse,
  EvidenceBatch,
  Evidence,
  GoalCommand,
  GoalCreate,
  GoalRevision,
  PlanProposal,
  TaskResult,
  Usage,
  SemanticError,
  WireJsonError,
} from "./index.ts";

type Equal<A, B> =
  (<T>() => T extends A ? 1 : 2) extends <T>() => T extends B ? 1 : 2 ? true : false;
type Assert<T extends true> = T;

export type DecoderOutputs = [
  Assert<
    Equal<
      Effect.Effect.Success<ReturnType<typeof AdapterCapabilities.decodeUnknown>>,
      SymmetryAdapterCapabilitiesV1
    >
  >,
  Assert<
    Equal<Effect.Effect.Success<ReturnType<typeof Admission.decodeUnknown>>, SymmetryAdmissionV1>
  >,
  Assert<
    Equal<
      Effect.Effect.Success<ReturnType<typeof ContextSnapshot.decodeUnknown>>,
      SymmetryContextSnapshotV1
    >
  >,
  Assert<
    Equal<Effect.Effect.Success<ReturnType<typeof Decision.decodeUnknown>>, SymmetryDecisionV1>
  >,
  Assert<
    Equal<
      Effect.Effect.Success<ReturnType<typeof EvidenceBatchConflictDetails.decodeUnknown>>,
      SymmetryEvidenceBatchConflictDetailsV1
    >
  >,
  Assert<
    Equal<
      Effect.Effect.Success<ReturnType<typeof EvidenceBatchResponse.decodeUnknown>>,
      SymmetryEvidenceBatchResponseV1
    >
  >,
  Assert<
    Equal<Effect.Effect.Success<ReturnType<typeof Evidence.decodeUnknown>>, SymmetryEvidenceV1>
  >,
  Assert<
    Equal<
      Effect.Effect.Success<ReturnType<typeof EvidenceBatch.decodeUnknown>>,
      SymmetryEvidenceBatchV1
    >
  >,
  Assert<
    Equal<
      Effect.Effect.Success<ReturnType<typeof GoalCommand.decodeUnknown>>,
      SymmetryGoalCommandV1
    >
  >,
  Assert<
    Equal<Effect.Effect.Success<ReturnType<typeof GoalCreate.decodeUnknown>>, SymmetryGoalCreateV1>
  >,
  Assert<
    Equal<
      Effect.Effect.Success<ReturnType<typeof GoalRevision.decodeUnknown>>,
      SymmetryGoalRevisionV1
    >
  >,
  Assert<
    Equal<
      Effect.Effect.Success<ReturnType<typeof PlanProposal.decodeUnknown>>,
      SymmetryPlanProposalV1
    >
  >,
  Assert<
    Equal<Effect.Effect.Success<ReturnType<typeof TaskResult.decodeUnknown>>, SymmetryTaskResultV1>
  >,
  Assert<Equal<Effect.Effect.Success<ReturnType<typeof Usage.decodeUnknown>>, SymmetryUsageV1>>,
  Assert<
    Equal<Effect.Effect.Success<ReturnType<typeof Admission.decodeJson>>, SymmetryAdmissionV1>
  >,
];

export type DecoderErrors = [
  Assert<
    Equal<
      Effect.Effect.Error<ReturnType<typeof Decision.decodeJson>>,
      ParseResult.ParseError | SemanticError | WireJsonError
    >
  >,
  Assert<
    Equal<
      Effect.Effect.Error<ReturnType<typeof Evidence.decodeJson>>,
      ParseResult.ParseError | SemanticError | WireJsonError
    >
  >,
  Assert<
    Equal<
      Effect.Effect.Error<ReturnType<typeof Admission.decodeUnknown>>,
      ParseResult.ParseError | SemanticError
    >
  >,
  Assert<
    Equal<
      Effect.Effect.Error<ReturnType<typeof Admission.decodeJson>>,
      ParseResult.ParseError | SemanticError | WireJsonError
    >
  >,
  Assert<
    Equal<
      Effect.Effect.Error<ReturnType<typeof TaskResult.decodeUnknown>>,
      ParseResult.ParseError | SemanticError
    >
  >,
];

export function useAdmission(
  value: Effect.Effect.Success<ReturnType<typeof Admission.decodeJson>>,
) {
  const dto: SymmetryAdmissionV1 = value;
  if (dto.session_mode === "handoff") {
    const sourceRunID: string = dto.handoff_source_run_id;
    const workItemID: string = dto.work_item_id;
    return [sourceRunID, workItemID];
  }
  if (dto.provider_scope !== null) {
    const firstResourceID: string = dto.provider_scope.resource_ids[0];
    return [firstResourceID];
  }
  return [];
}

export function useDecision(value: SymmetryDecisionV1) {
  switch (value.kind) {
    case "plan": {
      const workItemID: null = value.work_item_id;
      const subjectHash: null = value.subject_hash;
      // @ts-expect-error plan decisions cannot bind a WorkItem.
      const invalidWorkItemID: string = value.work_item_id;
      return [workItemID, subjectHash, invalidWorkItemID];
    }
    case "scope": {
      const workItemID: string = value.work_item_id;
      const subjectHash: null = value.subject_hash;
      // @ts-expect-error scope decisions cannot bind a Subject hash.
      const invalidSubjectHash: string = value.subject_hash;
      return [workItemID, subjectHash, invalidSubjectHash];
    }
    case "review": {
      const workItemID: string = value.work_item_id;
      const subjectHash: string = value.subject_hash;
      // @ts-expect-error review decisions require a Subject hash.
      const invalidSubjectHash: null = value.subject_hash;
      return [workItemID, subjectHash, invalidSubjectHash];
    }
    case "completion": {
      const workItemID: null = value.work_item_id;
      const subjectHash: string = value.subject_hash;
      // @ts-expect-error completion decisions cannot bind a WorkItem.
      const invalidWorkItemID: string = value.work_item_id;
      return [workItemID, subjectHash, invalidWorkItemID];
    }
    case "budget":
    case "external_action": {
      const workItemID: string | null = value.work_item_id;
      const subjectHash: string | null = value.subject_hash;
      return [workItemID, subjectHash];
    }
  }
}

declare const usageForExactnessTest: SymmetryUsageV1;

export const rootExtraFieldIsRejected: SymmetryUsageV1 = {
  ...usageForExactnessTest,
  // @ts-expect-error closed root DTOs must not expose a catch-all index signature.
  unexpected_root_field: true,
};

declare const goalCreateForExactnessTest: SymmetryGoalCreateV1;

export const nestedExtraFieldIsRejected: SymmetryGoalCreateV1 = {
  ...goalCreateForExactnessTest,
  initial_revision: {
    ...goalCreateForExactnessTest.initial_revision,
    execution_policy: {
      ...goalCreateForExactnessTest.initial_revision.execution_policy,
      automatic_execution: false,
      budget_mode: "soft",
      // @ts-expect-error closed nested objects must reject unknown fields.
      unexpected_nested_field: true,
    },
  },
};

declare const admitTaskForExactnessTest: Extract<
  SymmetryGoalCommandV1,
  { kind: "admit_task" }
>;

export const unionPayloadExtraFieldIsRejected: Extract<
  SymmetryGoalCommandV1,
  { kind: "admit_task" }
> = {
  ...admitTaskForExactnessTest,
  payload: {
    work_item_id: "resource-id",
    purpose: "implement",
    model_profile: "model",
    session_mode: "fresh",
    requested_session_id: null,
    validation_of_task_id: null,
    // @ts-expect-error union payloads must reject unknown fields.
    unexpected_payload_field: true,
  },
};

export const typedMapAllowsResourceKeys: NonNullable<SymmetryAdmissionV1["provider_scope"]> = {
  resource_ids: ["resource-id"],
  operations_by_resource: {
    "resource-id": ["resource.sync"],
  },
  change_target: null,
};
