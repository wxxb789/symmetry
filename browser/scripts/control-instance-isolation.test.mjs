// Both browser harnesses that start a Control instance from a worktree must
// pin that instance's own MIX_BUILD_PATH. Two instances started from one
// worktree otherwise share control/_build, and a compile one instance runs
// there (or the dev code reloader that compile feeds) decides what the other
// one loads and reloads - observed 2026-11-17 as a foreign session's server
// answering this worktree's browser runs with `# unhandled` error pages.
//
// The contract is the environment the harness hands to the processes it
// starts, so each case below drives the real harness script with `mise`
// replaced by a recording stub on PATH: every `mise exec … -- mix …`
// invocation - the database tasks and the Control instance itself - is
// recorded with the MIX_BUILD_PATH the harness set for it. No Control
// instance, database, lockfile check or browser starts here, and the harness
// is stopped as soon as the recorded server invocation proves what it was
// started with.
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const browserRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const controlRoot = path.join(browserRoot, "..", "control");
const instanceRoot = path.join(controlRoot, "_build", "instances");
// The harnesses start cmd launchers, enumerate Windows TCP listeners and stop
// process trees with taskkill, so this contract is asserted on Windows only.
const skipOnNonWindows = process.platform === "win32" ? false : "the harnesses and their recording stub are Windows-only";

// Each harness starts its instance with one of these two mix invocations.
const serverInvocations = {
  "run-planning-tests.ps1": /-- mix phx\.server$/,
  "run-connection-fixture-tests.ps1": /-- mix run --no-halt --no-start [^\s]*portal-connection-fixture\.exs$/
};
const databaseInvocations = ["mix ecto.drop", "mix ecto.create", "mix ecto.migrate"];

const delay = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));

async function waitFor(predicate, timeoutMilliseconds) {
  const deadline = Date.now() + timeoutMilliseconds;
  while (true) {
    if (predicate()) return true;
    if (Date.now() >= deadline) return predicate();
    await delay(250);
  }
}

function normalize(candidate) {
  return path.resolve(candidate).replace(/\\/g, "/").toLowerCase();
}

// A real harness run only ever needs `mise exec … -- mix …` to exist: the stub
// records what the harness set for the invocation and succeeds, which lets the
// harness reach its own server start without compiling or connecting anything.
function createRecorder() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "symmetry-mise-recorder-"));
  const logFile = path.join(root, "calls.log");
  fs.writeFileSync(
    path.join(root, "mise.cmd"),
    [
      "@echo off",
      // `^|` escapes the pipe; the fields are what this invocation received.
      '>>"%MISE_STUB_LOG%" echo %MIX_BUILD_PATH%^|%CD%^|%*',
      "exit /b 0",
      ""
    ].join("\r\n")
  );
  return { root, logFile };
}

function recordedCalls(logFile) {
  if (!fs.existsSync(logFile)) return [];
  const raw = fs.readFileSync(logFile, "utf8");
  // The harness is killed as soon as its server invocation is recorded, so a
  // final line can be cut mid-write; only complete lines are read back.
  const complete = raw.endsWith("\n") || raw.endsWith("\r\n") ? raw : raw.slice(0, raw.lastIndexOf("\n") + 1);
  return complete
    .split(/\r?\n/)
    .filter((line) => line.length > 0)
    .map((line) => {
      const [mixBuildPath = "", cwd = "", argv = ""] = line.split("|");
      return { mixBuildPath, cwd, argv };
    });
}

// Starts the real harness with the recording stub first on PATH and returns
// once its Control instance invocation has been recorded (or the harness ended
// on its own, which the assertions then report with the harness output).
async function startAndObserveInstance({ script, port, inheritedBuildPath }) {
  const recorder = createRecorder();
  const environment = { ...process.env, MISE_STUB_LOG: recorder.logFile };
  environment.PATH = `${recorder.root}${path.delimiter}${process.env.PATH ?? ""}`;
  if (inheritedBuildPath === undefined) {
    delete environment.MIX_BUILD_PATH;
  } else {
    environment.MIX_BUILD_PATH = inheritedBuildPath;
  }
  const child = spawn(
    "pwsh",
    [
      "-NoProfile",
      "-File",
      path.join(browserRoot, "scripts", script),
      "-PortalUrl",
      `http://127.0.0.1:${port}`,
      "-Port",
      String(port)
    ],
    { env: environment, stdio: ["ignore", "pipe", "pipe"] }
  );
  let output = "";
  child.stdout.setEncoding("utf8").on("data", (chunk) => (output += chunk));
  child.stderr.setEncoding("utf8").on("data", (chunk) => (output += chunk));
  let closed = null;
  const closedPromise = new Promise((resolve) => {
    child.on("close", (code, signal) => {
      closed = { code, signal };
      resolve(closed);
    });
  });

  const observed = await waitFor(
    () => closed !== null || recordedCalls(recorder.logFile).some((call) => serverInvocations[script].test(call.argv)),
    180_000
  );
  // Stop this run's process tree - the harness, its cmd launcher and the stub -
  // the moment the recorded invocation is enough to read the contract back.
  spawnSync("taskkill", ["/PID", String(child.pid), "/T", "/F"], { stdio: "ignore" });
  await closedPromise;
  const calls = recordedCalls(recorder.logFile);
  fs.rmSync(recorder.root, { recursive: true, force: true });
  return { calls, output, closed, observed };
}

function assertInstanceBuildPath(run, { script, port, inheritedBuildPath }) {
  const { calls, output } = run;
  assert.ok(calls.length > 0, `${script} recorded no mix invocation at all:\n${output}`);
  for (const invocation of databaseInvocations) {
    assert.ok(
      calls.some((call) => call.argv.includes(invocation)),
      `${script} never ran ${invocation} through mise:\n${calls.map((call) => call.argv).join("\n")}`
    );
  }
  assert.ok(
    calls.some((call) => serverInvocations[script].test(call.argv)),
    `${script} never started its Control instance through mise:\n${calls.map((call) => call.argv).join("\n")}`
  );

  const buildPaths = new Set();
  for (const call of calls) {
    assert.equal(
      normalize(call.cwd),
      normalize(controlRoot),
      `${script} ran '${call.argv}' from ${call.cwd}, not from the control project (${controlRoot}):\n${output}`
    );
    assert.ok(
      call.mixBuildPath.length > 0,
      `${script} ran '${call.argv}' with no MIX_BUILD_PATH set for it:\n${output}`
    );
    const buildPath = normalize(call.mixBuildPath);
    assert.ok(
      buildPath.startsWith(`${normalize(instanceRoot)}/`),
      `${script} ran '${call.argv}' with MIX_BUILD_PATH ${call.mixBuildPath}, outside this worktree's own instance build paths (${instanceRoot}):\n${output}`
    );
    assert.ok(
      buildPath.split("/").pop().endsWith(`-${port}`),
      `${script} ran '${call.argv}' with MIX_BUILD_PATH ${call.mixBuildPath}, which does not name its own port ${port}:\n${output}`
    );
    if (inheritedBuildPath !== undefined) {
      assert.notEqual(
        buildPath,
        normalize(inheritedBuildPath),
        `${script} let the inherited MIX_BUILD_PATH ${inheritedBuildPath} decide where its instance compiles:\n${output}`
      );
    }
    buildPaths.add(buildPath);
  }
  assert.equal(
    buildPaths.size,
    1,
    `${script} used ${buildPaths.size} build paths for one instance: ${[...buildPaths].join(", ")}`
  );
  return [...buildPaths][0];
}

// Ports this test owns; the harnesses refuse any port that is already served.
const freePort = () => 4600 + Math.floor(Math.random() * 340);

for (const script of Object.keys(serverInvocations)) {
  test(`${script} gives the Control instance it starts its own build path`, { skip: skipOnNonWindows }, async () => {
    const inherited = "Q:\\foreign-worktree\\symmetry\\control\\_build\\dev";
    const firstPort = freePort();
    const first = assertInstanceBuildPath(
      await startAndObserveInstance({ script, port: firstPort, inheritedBuildPath: inherited }),
      { script, port: firstPort, inheritedBuildPath: inherited }
    );
    let secondPort = freePort();
    if (secondPort === firstPort) secondPort = firstPort + 1;
    const second = assertInstanceBuildPath(
      await startAndObserveInstance({ script, port: secondPort }),
      { script, port: secondPort }
    );
    assert.notEqual(
      first,
      second,
      `${script} gave two concurrent instances on ports ${firstPort} and ${secondPort} the same build path ${first}`
    );
  });
}
