import { Effect, Schema } from "effect";
import {
  AdapterCapabilitiesSchema,
  AdmissionSchema,
  ConnectionsReadSchema,
  ContextSnapshotSchema,
  DecisionSchema,
  EvidenceBatchConflictDetailsSchema,
  EvidenceBatchResponseSchema,
  EvidenceBatchSchema,
  EvidenceSchema,
  GoalCommandSchema,
  GoalCreateSchema,
  GoalRevisionSchema,
  PlanProposalSchema,
  TaskResultSchema,
  UsageSchema,
  WorkDetailReadSchema,
  WorkTimelineReadSchema,
  WorkspaceReadSchema,
} from "../generated/effect/index.ts";
import {
  SemanticError,
  validateAdmissionSemantics,
  validateContextSnapshotSemantics,
  validateDecisionSemantics,
  validateEvidenceBatchSemantics,
  validateEvidenceSemantics,
  validateGoalCommandSemantics,
  validateGoalCreateSemantics,
  validateGoalRevisionSemantics,
  validatePlanProposalSemantics,
  validateTaskResultSemantics,
} from "./semantics.ts";
import { assertUnicode, parsePresentationJson, parseWireJson, WireJsonError } from "./wire-json.ts";

export {
  AdapterCapabilitiesSchema,
  AdmissionSchema,
  ConnectionsReadSchema,
  ContextSnapshotSchema,
  DecisionSchema,
  EvidenceBatchConflictDetailsSchema,
  EvidenceBatchResponseSchema,
  EvidenceBatchSchema,
  EvidenceSchema,
  GoalCommandSchema,
  GoalCreateSchema,
  GoalRevisionSchema,
  PlanProposalSchema,
  TaskResultSchema,
  UsageSchema,
  WorkDetailReadSchema,
  WorkTimelineReadSchema,
  WorkspaceReadSchema,
};
export type * from "../generated/ts/index.js";
export { SemanticError, WireJsonError };

function boundary<A>(
  schema: Schema.Schema<A, unknown>,
  validateSemantics?: (value: A) => void | Promise<void>,
  parseJson = parseWireJson,
) {
  const decodeUnknown = (input: unknown) =>
    Schema.decodeUnknown(schema)(input).pipe(
      Effect.flatMap((value) =>
        Effect.tryPromise({
          try: async () => {
            assertUnicode(value);
            await validateSemantics?.(value);
            return value;
          },
          catch: (cause) =>
            cause instanceof SemanticError
              ? cause
              : new SemanticError("semantic_mismatch", "contract semantic validation failed"),
        }),
      ),
    );

  // Canonical boundaries check number lexemes before JSON.parse can round them.
  const decodeJson = (input: string | Uint8Array) =>
    Effect.try({
      try: () => parseJson(input),
      catch: (cause) =>
        cause instanceof WireJsonError
          ? cause
          : new WireJsonError("invalid_json", "invalid JSON input"),
    }).pipe(Effect.flatMap(decodeUnknown));

  return { decodeUnknown, decodeJson };
}

export const AdapterCapabilities = boundary(AdapterCapabilitiesSchema);
export const Admission = boundary(AdmissionSchema, validateAdmissionSemantics);
export const ConnectionsRead = boundary(ConnectionsReadSchema, undefined, parsePresentationJson);
export const ContextSnapshot = boundary(ContextSnapshotSchema, validateContextSnapshotSemantics);
export const Decision = boundary(DecisionSchema, validateDecisionSemantics);
export const EvidenceBatchConflictDetails = boundary(EvidenceBatchConflictDetailsSchema);
export const EvidenceBatchResponse = boundary(EvidenceBatchResponseSchema);
export const Evidence = boundary(EvidenceSchema, validateEvidenceSemantics);
export const EvidenceBatch = boundary(EvidenceBatchSchema, validateEvidenceBatchSemantics);
export const GoalCommand = boundary(GoalCommandSchema, validateGoalCommandSemantics);
export const GoalCreate = boundary(GoalCreateSchema, validateGoalCreateSemantics);
export const GoalRevision = boundary(GoalRevisionSchema, validateGoalRevisionSemantics);
export const PlanProposal = boundary(PlanProposalSchema, validatePlanProposalSemantics);
export const TaskResult = boundary(TaskResultSchema, validateTaskResultSemantics);
export const Usage = boundary(UsageSchema);
export const WorkDetailRead = boundary(WorkDetailReadSchema, undefined, parsePresentationJson);
export const WorkTimelineRead = boundary(WorkTimelineReadSchema, undefined, parsePresentationJson);
export const WorkspaceRead = boundary(WorkspaceReadSchema, undefined, parsePresentationJson);
