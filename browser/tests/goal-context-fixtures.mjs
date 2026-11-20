// Goal-side fixture helper for the 0007h goal-attention suite.
//
// A ContextSnapshot is written by the server itself when a Goal admits its
// planning task, and no HTTP read exposes its id. This helper reads the id
// from the same database the running control plane serves, with `mix run
// --no-start` from `control/`; the case that uses it still asserts every
// effect over real HTTP.
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const controlRoot = fileURLToPath(new URL("../../control/", import.meta.url));
const fixtureScript = fileURLToPath(new URL("../../scripts/ci/portal-goal-context-fixture.exs", import.meta.url));

// The fixture's own subprocess is a real `mix run` against the running control
// plane's database: this is the backstop that decides when a wedged fixture run
// is declared failed, never an observation of the fixture's outcome (the
// snapshot's identity is read back over HTTP by the case that uses it).
const FIXTURE_GUARD_MS = 180_000;

// Locally the fixture runs under the pinned Elixir through `mise exec`. A CI
// render job installs that same Elixir directly (erlef/setup-beam) and has no
// mise, so it sets this to the mix executable on its PATH; the pinned versions
// stay the local default.
const mixInvocation = process.env.SYMMETRY_FIXTURE_MIX
  ? [process.env.SYMMETRY_FIXTURE_MIX]
  : ["mise", "exec", "elixir@1.20.4-otp-29", "erlang@29.1.2", "--", "mix"];

// A release-shell harness exports PHX_SERVER for the release processes it
// starts. The fixture starts a second copy of the application to read the row
// and must not bind the port the release under test already owns, so the flag
// is dropped from its environment.
const { PHX_SERVER: _phxServer, ...fixtureEnvironment } = process.env;

const snapshotAnswerPattern = /^snapshot_id=(.*)$/;
const snapshotIdPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * The fixture's marked answer out of its stdout, or null when the output
 * carries no marked line. The application the fixture starts logs to the same
 * stdout and its handler writes asynchronously, so the answer is recognised by
 * its own `snapshot_id=` marker — the last marked line — and never by the shape
 * of a bare id a log line could carry too.
 */
export function parseSnapshotAnswer(output) {
  const answers = output
    .split(/\r?\n/)
    .map((line) => line.trim())
    .map((line) => snapshotAnswerPattern.exec(line)?.[1] ?? null)
    .filter((answer) => answer !== null);
  return answers.at(-1) ?? null;
}

/**
 * Reads the newest ContextSnapshot id the server wrote for `goalId`, or throws
 * when the Goal admitted no planning task (the fixture answers
 * `snapshot_id=none`) or the fixture did not answer with the marker.
 */
export function readGoalContextSnapshot(goalId) {
  // File mode: `mix run file.exs -- args` would put the literal "--" into
  // System.argv/0, so the fixture args follow the script path directly.
  const output = execFileSync(
    mixInvocation[0],
    [...mixInvocation.slice(1), "run", "--no-start", fixtureScript, "--snapshot-id", goalId],
    {
      cwd: controlRoot,
      env: fixtureEnvironment,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "inherit"],
      timeout: FIXTURE_GUARD_MS
    }
  ).trim();

  const answer = parseSnapshotAnswer(output);
  if (answer !== null && answer !== "none") {
    if (!snapshotIdPattern.test(answer)) {
      throw new Error(`the context fixture answered an unreadable snapshot id for goal ${goalId}: ${JSON.stringify(answer)}`);
    }
    return answer;
  }
  if (answer === "none") {
    throw new Error(`goal ${goalId} admitted no ContextSnapshot: the fixture answered ${JSON.stringify(answer)}`);
  }
  throw new Error(`the context fixture did not answer with a snapshot id for goal ${goalId}: ${JSON.stringify(output)}`);
}
