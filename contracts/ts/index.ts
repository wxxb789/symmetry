import { Effect, Schema } from "effect";
import {
  AdapterCapabilitiesSchema,
  AdmissionSchema,
  ContextSnapshotSchema,
  DecisionSchema,
  EvidenceSchema,
  GoalCommandSchema,
  GoalCreateSchema,
  GoalRevisionSchema,
  PlanProposalSchema,
  TaskResultSchema,
  UsageSchema,
} from "../generated/effect/index.ts";
import {
  SemanticError,
  validateAdmissionSemantics,
  validateContextSnapshotSemantics,
  validateDecisionSemantics,
  validateEvidenceSemantics,
  validateGoalCommandSemantics,
  validateGoalCreateSemantics,
  validateGoalRevisionSemantics,
  validatePlanProposalSemantics,
  validateTaskResultSemantics,
} from "./semantics.ts";
import { assertUnicode, parseWireJson, WireJsonError } from "./wire-json.ts";

export {
  AdapterCapabilitiesSchema,
  AdmissionSchema,
  ContextSnapshotSchema,
  DecisionSchema,
  EvidenceSchema,
  GoalCommandSchema,
  GoalCreateSchema,
  GoalRevisionSchema,
  PlanProposalSchema,
  TaskResultSchema,
  UsageSchema,
};
export type * from "../generated/ts/index.js";
export { SemanticError, WireJsonError };

function boundary<A>(
  schema: Schema.Schema<A, unknown>,
  validateSemantics?: (value: A) => void | Promise<void>,
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

  // Raw HTTP bodies must pass the lexical checks before JSON.parse can round numbers.
  const decodeJson = (input: string | Uint8Array) =>
    Effect.try({
      try: () => parseWireJson(input),
      catch: (cause) =>
        cause instanceof WireJsonError
          ? cause
          : new WireJsonError("invalid_json", "invalid JSON input"),
    }).pipe(Effect.flatMap(decodeUnknown));

  return { decodeUnknown, decodeJson };
}

export const AdapterCapabilities = boundary(AdapterCapabilitiesSchema);
export const Admission = boundary(AdmissionSchema, validateAdmissionSemantics);
export const ContextSnapshot = boundary(ContextSnapshotSchema, validateContextSnapshotSemantics);
export const Decision = boundary(DecisionSchema, validateDecisionSemantics);
export const Evidence = boundary(EvidenceSchema, validateEvidenceSemantics);
export const GoalCommand = boundary(GoalCommandSchema, validateGoalCommandSemantics);
export const GoalCreate = boundary(GoalCreateSchema, validateGoalCreateSemantics);
export const GoalRevision = boundary(GoalRevisionSchema, validateGoalRevisionSemantics);
export const PlanProposal = boundary(PlanProposalSchema, validatePlanProposalSemantics);
export const TaskResult = boundary(TaskResultSchema, validateTaskResultSemantics);
export const Usage = boundary(UsageSchema);
