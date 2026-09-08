---
name: symmetry-simplification-review
description: Perform a scoped ablation review of a completed Symmetry change to remove unnecessary abstractions while preserving named contracts and verified behavior.
---

# Simplification review

Review the current diff and its acceptance contract, not the entire repository.
Identify abstractions added or materially extended by this change. For each
candidate, name what duplication it removes or invariant it protects.

Try a concrete deletion or direct-library alternative when the candidate lacks
that justification. Compare clarity, state ownership, error handling and changed
behavior; line count alone does not decide. Preserve transaction boundaries,
resource cleanup, compatibility and domain-specific validation.

Keep the simpler version if affected checks and requirements still hold. Otherwise
restore the necessary abstraction and record the specific responsibility it owns.
Do not create wrapper layers to conceal the same complexity or delete tests to
make a shorter implementation pass. Avoid broad unrelated refactors.

Report the useful removal, or the concrete reason a candidate was retained, plus
affected verification. "Nothing unnecessary found" is valid; do not force a diff.
