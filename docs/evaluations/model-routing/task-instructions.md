# Evaluated task instructions

Use the frozen task JSON as the acceptance contract, not a suggestion.
The coordinator records the actual configured worker/reviewer routes and authorization before creating the isolated run; a missing required route stops the run before implementation.
No new profile, credential, spend cap or model override is implied by these instructions.

## Worker packet

The coordinator supplies:

- frozen task file and exact base commit;
- isolated checkout, source Subject rules and exclusive allowed paths;
- relevant design and directory contributor instructions;
- required executable gates and explanations;
- coordinator-owned evidence directory, permitted effects and verification owner.

Read relevant source and existing consumer tests before editing.
Implement the smallest complete change, using existing libraries and repository patterns.
Keep existing tests, types, acceptance, authority, recovery and permissions intact.
Do not edit the frozen task, runner, review instructions or evidence to make your work pass.
Do not commit, push or mutate the canonical checkout when the packet forbids it.

For an Elixir task, run `mix` from `control/` with the environment the coordinator exported (`MISE_ELIXIR_VERSION`/`MISE_ERLANG_VERSION` for the pinned toolchain, `MIX_DEPS_PATH`/`MIX_BUILD_PATH` outside the checkout, `POSTGRES_*` for the run's own database, which `mix test` creates and migrates). The checkout carries a staged frontend bundle under `control/priv/static/portal/` that the suite requires and that must not be modified. Exercise the frozen acceptance through focused files only; the full suite, the provider-broker and goal-receipt integration gates and Go builds belong to the coordinator, and heavy jobs must not run concurrently on one host.

The coordinator executes the frozen gates after sealing the source and invokes the separately configured reviewer.
Worker-written tests are not the sole acceptance evidence: retain existing contract checks, named-test execution assertions and semantic review.

## Escalation

Escalate when an invariant cannot be explained/tested, the diff would cross scope, failures recur without a changed diagnosis, or success would require weakening a guard.
Preserve all already-correct source and evidence.
Return the Subject, affected invariant, exact command and exit code, relevant output reference, files preserved, diagnosis and smallest next action.
A missing Linux execution surface is not permission to label a Windows no-test run as Linux qualification.
An implementing model can propose a repair or amendment; it cannot change the frozen acceptance contract.

## Completion

Return changed paths, required explanations, checks actually executed and unresolved limits.
Do not write a successful receipt yourself.
The coordinator derives it from observed verification, review and all phase costs, including failed attempts.
