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

function runFixture(args) {
  // File mode: `mix run file.exs -- args` would put the literal "--" into
  // System.argv/0, so the fixture args follow the script path directly.
  const output = execFileSync(
    "mise",
    ["exec", "elixir@1.20.4-otp-29", "erlang@29.1.2", "--", "mix", "run", "--no-start", fixtureScript, ...args],
    {
      cwd: controlRoot,
      env: process.env,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "inherit"],
      timeout: 180_000
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
