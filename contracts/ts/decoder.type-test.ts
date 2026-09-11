import type { Effect, ParseResult } from "effect";
import type {
  SymmetryAdapterCapabilitiesV1,
  SymmetryAdmissionV1,
  SymmetryContextSnapshotV1,
  SymmetryDecisionV1,
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
    Equal<Effect.Effect.Success<ReturnType<typeof Evidence.decodeUnknown>>, SymmetryEvidenceV1>
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
