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
import net from "node:net";
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

// Each harness owns one instance name and starts it with one of these two mix
// invocations.
const harnesses = {
  "run-planning-tests.ps1": {
    instance: "planning",
    serverInvocation: /-- mix phx\.server$/
  },
  "run-connection-fixture-tests.ps1": {
    instance: "connections",
    serverInvocation: /-- mix run --no-halt --no-start [^\s]*portal-connection-fixture\.exs$/
  }
};
const databaseInvocations = ["mix ecto.drop", "mix ecto.create", "mix ecto.migrate"];

const delay = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));

// A bounded wait that never keeps the test process alive once its race is won.
function rejectAfter(milliseconds, message) {
  return new Promise((_, reject) => {
    setTimeout(() => reject(new Error(message)), milliseconds).unref();
  });
}

// A port nothing is serving right now: each harness refuses a port that already
// answers, and each run needs its own. The OS picks it and it is released again
// immediately, so the harness still owns its check-then-start window.
async function freePort() {
  const probe = net.createServer();
  await new Promise((resolve, reject) => {
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", resolve);
  });
  const { port } = probe.address();
  await new Promise((resolve) => probe.close(resolve));
  return port;
}

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

// The recorded MIX_BUILD_PATH belongs to the recorded child cwd (the control
// project the mix invocation ran in), not to this test process's cwd: resolve
// it against call.cwd so a spelling the harness made relative to where its mix
// invocation ran is read the way that child resolved it, not the way this test
// process happens to resolve it.
function resolveRecordedBuildPath(call) {
  return normalize(path.resolve(call.cwd, call.mixBuildPath));
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

// Spawns the real harness with the recording stub first on PATH, collecting
// both of its output streams. Nothing is waited on or stopped here: each caller
// waits for what its assertions need (a recorded invocation, or the exit).
function spawnHarness({ script, port, inheritedBuildPath }) {
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
  let closed = false;
  let exitCode = null;
  child.stdout.setEncoding("utf8").on("data", (chunk) => (output += chunk));
  child.stderr.setEncoding("utf8").on("data", (chunk) => (output += chunk));
  const closedPromise = new Promise((resolve) => {
    child.on("close", (code) => {
      closed = true;
      exitCode = code;
      resolve();
    });
  });
  return {
    recorder,
    child,
    closedPromise,
    get output() {
      return output;
    },
    get closed() {
      return closed;
    },
    get exitCode() {
      return exitCode;
    }
  };
}

// Starts the real harness with the recording stub first on PATH and returns
// once its Control instance invocation has been recorded (or the harness ended
// on its own, which the assertions then report with the harness output).
async function startAndObserveInstance({ script, port, inheritedBuildPath }) {
  const harness = spawnHarness({ script, port, inheritedBuildPath });
  await waitFor(
    () => harness.closed || recordedCalls(harness.recorder.logFile).some((call) => harnesses[script].serverInvocation.test(call.argv)),
    180_000
  );
  // Stop this run's process tree - the harness, its cmd launcher and the stub -
  // the moment the recorded invocation is enough to read the contract back.
  spawnSync("taskkill", ["/PID", String(harness.child.pid), "/T", "/F"], { stdio: "ignore" });
  await Promise.race([
    harness.closedPromise,
    rejectAfter(
      10_000,
      `${script} left its process tree open 10s after taskkill /T /F on PID ${harness.child.pid} (harness -> cmd launcher -> recording stub):\n${harness.output}`
    )
  ]);
  const calls = recordedCalls(harness.recorder.logFile);
  fs.rmSync(harness.recorder.root, { recursive: true, force: true });
  return { calls, output: harness.output };
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
    calls.some((call) => harnesses[script].serverInvocation.test(call.argv)),
    `${script} never started its Control instance through mise:\n${calls.map((call) => call.argv).join("\n")}`
  );

  const expectedBuildPath = normalize(path.join(instanceRoot, `${harnesses[script].instance}-${port}`));
  const buildPaths = new Set();
  for (const call of calls) {
    // The pinned toolchain every invocation runs under: this contract is about
    // which mix runs where, so a pin edit must not silently change every
    // invocation it records.
    assert.match(
      call.argv,
      /^exec elixir@1\.20\.4-otp-29 erlang@29\.1\.2 -- mix /,
      `${script} ran '${call.argv}' outside the pinned elixir@1.20.4-otp-29 / erlang@29.1.2 toolchain:\n${output}`
    );
    assert.equal(
      normalize(call.cwd),
      normalize(controlRoot),
      `${script} ran '${call.argv}' from ${call.cwd}, not from the control project (${controlRoot}):\n${output}`
    );
    assert.ok(
      call.mixBuildPath.length > 0,
      `${script} ran '${call.argv}' with no MIX_BUILD_PATH set for it:\n${output}`
    );
    const buildPath = resolveRecordedBuildPath(call);
    assert.equal(
      buildPath,
      expectedBuildPath,
      `${script} ran '${call.argv}' with MIX_BUILD_PATH ${call.mixBuildPath}, not this instance's own build path ${expectedBuildPath}:\n${output}`
    );
    if (inheritedBuildPath !== undefined) {
      assert.notEqual(
        buildPath,
        normalize(inheritedBuildPath),
        `${script} let the inherited MIX_BUILD_PATH ${inheritedBuildPath} decide where its instance compiles:\n${output}`
      );
      // Overriding is only half the contract: the operator sees the override in the
      // run output, with the value that was ignored (STD-A13 - the report line could
      // be deleted while every recorded-argv assertion stayed green).
      assert.ok(
        output.includes(`ignoring inherited MIX_BUILD_PATH '${inheritedBuildPath}'`),
        `${script} did not report the overridden MIX_BUILD_PATH ${inheritedBuildPath} in its output:\n${output}`
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

// The resolution base is the recorded child cwd: a harness-relative spelling
// ("control/_build/...") must not be read as if this test process had resolved
// it, which would accept it as the instance's own build path even though the
// child resolved it under its own cwd (control/control/_build/...) and loaded
// a different instance.
test("resolveRecordedBuildPath resolves a recorded build path against the recorded child cwd", () => {
  const repoRootRelative = "control/_build/instances/planning-57001";
  assert.equal(
    resolveRecordedBuildPath({ cwd: controlRoot, mixBuildPath: repoRootRelative }),
    normalize(path.resolve(controlRoot, repoRootRelative)),
    "a relative spelling must resolve against the recorded child cwd"
  );
  assert.notEqual(
    resolveRecordedBuildPath({ cwd: controlRoot, mixBuildPath: repoRootRelative }),
    normalize(path.join(instanceRoot, "planning-57001")),
    "a repo-root-relative spelling is not this instance's build path once resolved under control/"
  );
  const absolute = normalize(path.join(instanceRoot, "planning-57001"));
  assert.equal(
    resolveRecordedBuildPath({ cwd: controlRoot, mixBuildPath: absolute }),
    absolute,
    "an absolute spelling must pass through unchanged"
  );
});

for (const script of Object.keys(harnesses)) {
  test(`${script} gives the Control instance it starts its own build path`, { skip: skipOnNonWindows }, async () => {
    const inherited = "Q:\\foreign-worktree\\symmetry\\control\\_build\\dev";
    const firstPort = await freePort();
    const first = assertInstanceBuildPath(
      await startAndObserveInstance({ script, port: firstPort, inheritedBuildPath: inherited }),
      { script, port: firstPort, inheritedBuildPath: inherited }
    );
    let secondPort = await freePort();
    while (secondPort === firstPort) secondPort = await freePort();
    const second = assertInstanceBuildPath(
      await startAndObserveInstance({ script, port: secondPort }),
      { script, port: secondPort }
    );
    assert.notEqual(
      first,
      second,
      `${script} gave two runs on ports ${firstPort} and ${secondPort} the same build path ${first}`
    );
  });

  test(`${script} refuses a port another harness instance already owns`, { skip: skipOnNonWindows }, async () => {
    const port = await freePort();
    const holderRoot = fs.mkdtempSync(path.join(os.tmpdir(), "symmetry-port-lock-holder-"));
    const markerFile = path.join(holderRoot, "held.marker");
    const holderScript = path.join(holderRoot, "hold-port-lock.ps1");
    fs.writeFileSync(
      holderScript,
      [
        "param([int]$Port, [string]$Marker)",
        '$ErrorActionPreference = "Stop"',
        "# Holds the same per-port named mutex the harnesses take, so a harness on",
        "# this port must refuse before any mix invocation. An abandoned mutex",
        "# means the previous owner died without releasing it: acquiring it is",
        "# safe, exactly as in the harnesses.",
        '$mutex = New-Object System.Threading.Mutex($false, "Local\\symmetry-browser-harness-port-$Port")',
        "$held = $false",
        "try { $held = $mutex.WaitOne(0) } catch [System.Threading.AbandonedMutexException] { $held = $true }",
        'if (-not $held) { throw "lock holder could not acquire port $Port" }',
        'Set-Content -LiteralPath $Marker -Value "held"',
        "Start-Sleep -Seconds 120",
        ""
      ].join("\r\n")
    );
    const holder = spawn(
      "pwsh",
      ["-NoProfile", "-File", holderScript, "-Port", String(port), "-Marker", markerFile],
      { stdio: ["ignore", "pipe", "pipe"] }
    );
    const holderClosed = new Promise((resolve) => holder.on("close", resolve));
    let holderOutput = "";
    holder.stdout.setEncoding("utf8").on("data", (chunk) => (holderOutput += chunk));
    holder.stderr.setEncoding("utf8").on("data", (chunk) => (holderOutput += chunk));
    let harness = null;
    try {
      assert.ok(
        await waitFor(() => fs.existsSync(markerFile), 30_000),
        `the pwsh lock holder never acquired port ${port}'s lock:\n${holderOutput}`
      );
      harness = spawnHarness({ script, port });
      // Refusal precedes every mix invocation, so the harness must close long
      // before this bound; a harness still running here proceeded instead.
      await waitFor(() => harness.closed, 60_000);
      const calls = recordedCalls(harness.recorder.logFile);
      assert.ok(
        harness.closed,
        `${script} did not exit within 60s while another harness instance held port ${port} (recorded mix calls: ${calls.length}):\n${harness.output}`
      );
      assert.notEqual(
        harness.exitCode,
        0,
        `${script} exited 0 although another harness instance held port ${port}:\n${harness.output}`
      );
      assert.match(
        harness.output,
        /already owned by another harness instance/,
        `${script} went ahead on port ${port} held by another harness instance (recorded mix calls: ${calls.length}):\n${harness.output}`
      );
      assert.equal(
        calls.length,
        0,
        `${script} ran mix invocations before refusing port ${port}:\n${calls.map((call) => call.argv).join("\n")}`
      );
    } finally {
      spawnSync("taskkill", ["/PID", String(holder.pid), "/T", "/F"], { stdio: "ignore" });
      if (holder.exitCode === null && holder.signalCode === null) {
        await Promise.race([holderClosed, rejectAfter(10_000, `lock holder PID ${holder.pid} did not close after taskkill /T /F`)]);
      }
      if (harness) {
        spawnSync("taskkill", ["/PID", String(harness.child.pid), "/T", "/F"], { stdio: "ignore" });
        if (!harness.closed) {
          await Promise.race([
            harness.closedPromise,
            rejectAfter(10_000, `${script} PID ${harness.child.pid} did not close after taskkill /T /F:\n${harness.output}`)
          ]);
        }
        fs.rmSync(harness.recorder.root, { recursive: true, force: true });
      }
      fs.rmSync(holderRoot, { recursive: true, force: true });
    }
  });

  test(`${script} holds its port lock while it awaits readiness`, { skip: skipOnNonWindows }, async () => {
    // A lock the harness releases at its first mix call is no lock at all: the fake
    // holder above proves the challenger *tries* the lock, not that a live instance
    // holds it while it runs. Here the holder is the real harness itself, driven by
    // the recording stub and kept alive inside its readiness wait (the stub never
    // answers healthz), and the challenger must refuse while it lives. Moving either
    // harness's ReleaseMutex before its readiness wait fails this case (STD-A11).
    const port = await freePort();
    const holder = spawnHarness({ script, port });
    let challenger = null;
    try {
      await waitFor(
        () => holder.closed || recordedCalls(holder.recorder.logFile).some((call) => harnesses[script].serverInvocation.test(call.argv)),
        180_000
      );
      assert.ok(
        !holder.closed,
        `${script} ended before it could hold port ${port} through its readiness wait:\n${holder.output}`
      );
      challenger = spawnHarness({ script, port });
      await waitFor(() => challenger.closed, 60_000);
      const calls = recordedCalls(challenger.recorder.logFile);
      assert.ok(
        challenger.closed,
        `${script} did not exit within 60s while a live instance held port ${port} (recorded mix calls: ${calls.length}):\n${challenger.output}`
      );
      assert.notEqual(
        challenger.exitCode,
        0,
        `${script} exited 0 although a live instance held port ${port}:\n${challenger.output}`
      );
      assert.match(
        challenger.output,
        /already owned by another harness instance/,
        `${script} went ahead on port ${port} held by a live instance (recorded mix calls: ${calls.length}):\n${challenger.output}`
      );
      assert.equal(
        calls.length,
        0,
        `${script} ran mix invocations before refusing port ${port}:\n${calls.map((call) => call.argv).join("\n")}`
      );
      assert.ok(
        !holder.closed,
        `${script} released its port lock while its readiness wait was still pending:\n${holder.output}`
      );
    } finally {
      for (const harness of [challenger, holder]) {
        if (!harness) continue;
        spawnSync("taskkill", ["/PID", String(harness.child.pid), "/T", "/F"], { stdio: "ignore" });
      }
      for (const harness of [challenger, holder]) {
        if (!harness) continue;
        if (!harness.closed) {
          await Promise.race([
            harness.closedPromise,
            rejectAfter(10_000, `${script} PID ${harness.child.pid} did not close after taskkill /T /F:\n${harness.output}`)
          ]);
        }
        fs.rmSync(harness.recorder.root, { recursive: true, force: true });
      }
    }
  });
}
