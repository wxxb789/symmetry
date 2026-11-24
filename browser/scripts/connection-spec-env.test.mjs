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
import { readFileSync } from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { navigate, navigatedAway, sessionClosed, unreadableReadFailure } from "../tests/portal-connections-recorder.mjs";

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
  assert.match(run.output, /22 skipped\b/);
});

test("an absent fixture environment skips at collection instead of failing the suite", () => {
  const run = playwright([], {});
  assert.equal(run.status, 0, run.output);
  assert.match(run.output, /22 skipped\b/);
});

test("a partial fixture identity fails at collection instead of running an unproven target", () => {
  const run = playwright(["--list"], {
    ...fixtureIdentity,
    SYMMETRY_OPERATOR_TOKEN: "documented-override-token",
  });
  assert.equal(run.status, 1, run.output);
  assert.match(run.output, /SYMMETRY_PORTAL_URL, SYMMETRY_FIXTURE_STATE_FILE must be set together/);
});

test("the complete fixture environment collects the twenty-two fixture tests", () => {
  const run = playwright(["--list"], {
    ...fixtureIdentity,
    SYMMETRY_OPERATOR_TOKEN: "test-run-operator-token",
    SYMMETRY_FIXTURE_STATE_FILE: "/tmp/symmetry-fixture-state.json",
    SYMMETRY_PORTAL_URL: "http://127.0.0.1:4021",
  });
  assert.equal(run.status, 0, run.output);
  assert.match(run.output, /Total: 22 tests in 1 file/);
});

// The recorder rules the connection spec relies on are exercised here, where
// ci.yml's "Run the browser node suites" step of browser-fixture-linux runs them.
const unreadableEntry = (method, unreadable, failure = null) => ({
  path: "/portal/api/probe",
  unreadable,
  request: { method: () => method, failure: () => failure },
});

const tick = () => new Promise((resolve) => setImmediate(resolve));

test("the unreadable-body rule tolerates only a read lost to a navigation or a teardown", () => {
  const tolerated = [
    unreadableEntry("GET", "socket hang up", "net::ERR_ABORTED"),
    unreadableEntry("HEAD", "socket hang up", "net::ERR_ABORTED"),
    unreadableEntry(
      "GET",
      `Error: response.text: Protocol error (Network.getResponseBody): No resource with given identifier found\n${navigatedAway}`,
    ),
    unreadableEntry("HEAD", `Error: response.text: ${sessionClosed}`),
  ];
  for (const entry of tolerated) {
    assert.equal(unreadableReadFailure(entry), null, `${entry.request.method()} ${entry.unreadable}`);
  }
  const refused = [
    unreadableEntry("POST", navigatedAway, "net::ERR_ABORTED"),
    unreadableEntry("DELETE", sessionClosed),
    unreadableEntry("GET", "Response body is unavailable for redirect responses"),
    unreadableEntry("GET", `${navigatedAway} (retried)`),
    unreadableEntry("HEAD", `prefix ${sessionClosed} suffix`),
    unreadableEntry("GET", navigatedAway.replace("Read response.body()", "read response.text()")),
    unreadableEntry("GET", "socket hang up"),
  ];
  for (const entry of refused) {
    const method = entry.request.method();
    const refusal = unreadableReadFailure(entry);
    assert.equal(typeof refusal, "string", `${method} ${entry.unreadable}`);
    assert.ok(refusal.includes(entry.unreadable), refusal);
    if (["GET", "HEAD"].includes(method)) {
      assert.match(refusal, /^response \/portal\/api\/probe must be readable unless its request was aborted: /);
    } else {
      assert.match(refusal, new RegExp(`^an unreadable response must belong to a read: ${method} /portal/api/probe: `));
    }
  }
});

test("navigate settles every recorded read before it navigates, including reads that land while it waits", async () => {
  const order = [];
  const page = { goto: async (url) => { order.push(`goto ${url}`); } };
  await navigate(page, { responses: [] }, "/portal/connections");
  assert.deepEqual(order, ["goto /portal/connections"], "a recorder with no pending read navigates at once");
  order.length = 0;

  const recorded = { responses: [] };
  let releaseFirst;
  recorded.responses.push(new Promise((resolve) => {
    releaseFirst = () => { order.push("first read settled"); resolve({ path: "/portal/api/first" }); };
  }));
  const navigation = navigate(page, recorded, "/portal/connections");
  await tick();
  assert.deepEqual(order, [], "the navigation waits for a read that is still pending");
  let releaseSecond;
  recorded.responses.push(new Promise((resolve) => {
    releaseSecond = () => { order.push("second read settled"); resolve({ path: "/portal/api/second" }); };
  }));
  releaseFirst();
  await tick();
  assert.deepEqual(order, ["first read settled"], "the navigation waits for the read that landed during the settle");
  releaseSecond();
  await navigation;
  assert.deepEqual(order, ["first read settled", "second read settled", "goto /portal/connections"]);
});

test("navigate refuses by name when its reads cannot settle inside the deadline", async () => {
  const order = [];
  const page = { goto: async (url) => { order.push(`goto ${url}`); } };
  await assert.rejects(
    navigate(page, { responses: [new Promise(() => {})] }, "/portal/connections", { deadlineMs: 25 }),
    /the recorder's reads did not settle within 25ms before navigating to \/portal\/connections/,
  );
  await assert.rejects(
    navigate(page, { responses: [Promise.resolve({ path: "/portal/api/probe" })] }, "/portal/connections", { deadlineMs: 0 }),
    /the recorder's reads did not settle within 0ms before navigating to \/portal\/connections/,
  );
  assert.deepEqual(order, [], "a refused navigation must not reach page.goto");
});

test("the connection spec navigates only through the shared barrier", () => {
  const source = readFileSync(path.join(browserRoot, specFile), "utf8")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/\/\/.*$/gm, "");
  assert.ok(
    !source.includes("page.goto"),
    "a bare page.goto in the spec would skip the barrier that settles its pending reads",
  );
  assert.match(source, /import \{ navigate, unreadableReadFailure \} from "\.\/portal-connections-recorder\.mjs"/);
});
