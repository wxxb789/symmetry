---
name: symmetry-acceptance-workflow
description: Execute or independently review a frozen model-routing evaluation task in an isolated Symmetry checkout, bind checks to its exact source Subject, and escalate scope or acceptance boundaries. Not ordinary feature implementation or routing-policy promotion.
---

# Frozen acceptance workflow

Outcome: a reproducible task receipt or a refusal with preserved work and failing evidence.
The next consumer is the coordinating operator, not the implementing model.

1. Read the frozen task and [workflow instructions](../../../docs/evaluations/model-routing/task-instructions.md). Locate its base, allowed paths, protected assets and required gates before editing. Missing configured worker/reviewer authorization at startup stops the run, without substitution.
2. Implement only within that scope. Preserve existing tests, types, permissions, authority and recovery guards. Explain the invariants requested by the task using actual code and evidence.
3. If an invariant cannot be explained or tested, the diff crosses scope, the same failure recurs without a changed diagnosis, or a guard would need weakening, preserve correct work and return an escalation with the exact failing command/result and proposed next owner. Do not retry to green.
4. Hand source back to the coordinator for sealing and executable verification. A successful command is insufficient if a required named test did not run, skipped, or used a different Subject.
5. For independent review, use [review instructions](../../../docs/evaluations/model-routing/review-instructions.md), read the frozen requirement and actual diff yourself, and bind the verdict to the sealed Subject. Return unresolved defects or missing evidence; do not approve your own implementation.

Complete only when the coordinator derives and verifies the receipt against executed gates and independent review, or records a nonaccepted escalation. Unknown billing remains unknown; raw model/tool transcripts remain outside Git. See the [runbook](../../../docs/evaluations/model-routing/README.md) for executable commands and the local trust boundary.
