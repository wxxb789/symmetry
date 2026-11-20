// Repository-side fixture helpers for the 0007c planning suite.
//
// The two unwritable item kinds cannot be produced through the portal API: a
// Goal-managed row comes from Goal admission (0007h's surface) and an
// external-tracker binding comes from provider sync (0007e's surface). This
// helper applies the same Control changesets those paths use, against the same
// database the running control plane serves, with `mix run` from `control/`.
// Every browser interaction in the suite stays real HTTP/DB.
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const controlRoot = fileURLToPath(new URL("../../control/", import.meta.url));
const fixtureScript = fileURLToPath(new URL("../../scripts/ci/portal-planning-fixture.exs", import.meta.url));

// The fixture's own subprocess is a real `mix run` against the running control
// plane's database: this is the backstop that decides when a wedged fixture run
// is declared failed, never an observation of the fixture's outcome (the
// changeset's effect is read back over HTTP by the case that uses it).
const FIXTURE_GUARD_MS = 180_000;

// Locally the fixture runs under the pinned Elixir through `mise exec`. A CI
// render job installs that same Elixir directly (erlef/setup-beam) and has no
// mise, so it sets this to the mix executable on its PATH; the pinned versions
// stay the local default.
const mixInvocation = process.env.SYMMETRY_FIXTURE_MIX
  ? [process.env.SYMMETRY_FIXTURE_MIX]
  : ["mise", "exec", "elixir@1.20.4-otp-29", "erlang@29.1.2", "--", "mix"];

function runFixture(args) {
  // A release-shell harness exports PHX_SERVER for the release processes it
  // starts. The fixture starts a second copy of the application to write the
  // row and must not bind the port the release under test already owns, so the
  // flag is dropped from its environment.
  const { PHX_SERVER: _phxServer, ...fixtureEnvironment } = process.env;
  // File mode: `mix run file.exs -- args` would put the literal "--" into
  // System.argv/0, so the fixture args follow the script path directly.
  const output = execFileSync(
    mixInvocation[0],
    [...mixInvocation.slice(1), "run", "--no-start", fixtureScript, ...args],
    {
      cwd: controlRoot,
      env: fixtureEnvironment,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "inherit"],
      timeout: FIXTURE_GUARD_MS
    }
  );
  return output.trim();
}

/**
 * Marks an existing work item as Goal-owned through WorkItem.goal_membership_changeset/2.
 * `repositoryResourceId` is the project repository the admission baseline is frozen from.
 */
export function applyGoalManaged(workItemId, repositoryResourceId) {
  return runFixture(["--goal-managed", workItemId, repositoryResourceId]);
}

/** Binds an existing work item to an external tracker through WorkItem.provider_changeset/2. */
export function applyProviderOwned(workItemId, resourceId, provider, externalId) {
  return runFixture(["--provider-owned", workItemId, resourceId, provider, externalId]);
}
