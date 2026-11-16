// The connection fixture spec may only run against the harness-started fixture,
// so how it classifies its environment is part of the acceptance boundary:
// without the fixture identity it skips at collection (the default target and
// the README's documented SYMMETRY_PORTAL_URL / SYMMETRY_OPERATOR_TOKEN
// override are normal custom targets and must not fail the repository-wide
// suite), while a run that carries part of the fixture identity is a
// half-configured harness and fails at collection. Every case below drives the
// real Playwright CLI, because the contract is the CLI's collection outcome:
// a module-scope throw fails the whole suite before any test runs.
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const browserRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const playwrightCli = path.join(browserRoot, "node_modules", "@playwright", "test", "cli.js");
const specFile = "tests/portal-connections.spec.mjs";
const fixtureVariables = [
  "SYMMETRY_OPERATOR_TOKEN",
  "SYMMETRY_FIXTURE_IDENTITY",
  "SYMMETRY_FIXTURE_IDENTITY_FILE",
  "SYMMETRY_FIXTURE_STATE_FILE",
  "SYMMETRY_PORTAL_URL",
];
const fixtureIdentity = {
  SYMMETRY_FIXTURE_IDENTITY: "test-run-identity",
  SYMMETRY_FIXTURE_IDENTITY_FILE: "/tmp/symmetry-fixture-identity.json",
};

// The spec reads exactly these five variables, so a developer shell that
// already carries one of them must not change what a case measures.
function environment(overrides = {}) {
  const env = { ...process.env };
  for (const name of fixtureVariables) delete env[name];
  return { ...env, ...overrides };
}

function playwright(args, overrides) {
  const result = spawnSync(process.execPath, [playwrightCli, "test", specFile, ...args], {
    cwd: browserRoot,
    env: environment(overrides),
    encoding: "utf8",
  });
  assert.equal(result.error, undefined, `spawning the Playwright CLI failed: ${result.error}`);
  return { status: result.status, output: `${result.stdout}\n${result.stderr}` };
}

test("the documented custom-target override collects the suite and skips the fixture spec", () => {
  const run = playwright([], {
    SYMMETRY_PORTAL_URL: "http://127.0.0.1:4001",
    SYMMETRY_OPERATOR_TOKEN: "documented-override-token",
  });
  assert.equal(run.status, 0, run.output);
  assert.match(run.output, /20 skipped\b/);
});

test("an absent fixture environment skips at collection instead of failing the suite", () => {
  const run = playwright([], {});
  assert.equal(run.status, 0, run.output);
  assert.match(run.output, /20 skipped\b/);
});

test("a partial fixture identity fails at collection instead of running an unproven target", () => {
  const run = playwright(["--list"], {
    ...fixtureIdentity,
    SYMMETRY_OPERATOR_TOKEN: "documented-override-token",
  });
  assert.equal(run.status, 1, run.output);
  assert.match(run.output, /SYMMETRY_PORTAL_URL, SYMMETRY_FIXTURE_STATE_FILE must be set together/);
});

test("the complete fixture environment collects the twenty fixture tests", () => {
  const run = playwright(["--list"], {
    ...fixtureIdentity,
    SYMMETRY_OPERATOR_TOKEN: "test-run-operator-token",
    SYMMETRY_FIXTURE_STATE_FILE: "/tmp/symmetry-fixture-state.json",
    SYMMETRY_PORTAL_URL: "http://127.0.0.1:4021",
  });
  assert.equal(run.status, 0, run.output);
  assert.match(run.output, /Total: 20 tests in 1 file/);
});
