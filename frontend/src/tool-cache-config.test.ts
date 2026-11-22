import path from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";

// The evaluation runner exports SYMMETRY_TOOL_CACHE_DIR / SYMMETRY_DISABLE_TOOL_CACHES
// into every gate run so a sealed checkout neither reads nor executes tool run state a
// previous run wrote (the tool-run-state contract in scripts/ci/model-routing-evaluation.py).
// The runner-side tests assert the environment it exports; these cases assert the
// consumer side, at the level vitest actually honours: a `cache: false` placed at the
// config root instead of under `test` is silently ignored by vitest, so the assertions
// read the parsed option instead of the file text.
const TOOL_CACHE_DIR = "SYMMETRY_TOOL_CACHE_DIR";
const DISABLE_TOOL_CACHES = "SYMMETRY_DISABLE_TOOL_CACHES";
const SEALED_CACHE = "/sealed/tool-cache";

// The first import pays the process's cold `vite.config` module graph: the 2026-11-21
// audit measured 8.2 s against vitest's 5 s default (30 s is 3.7x it).
const CONFIG_IMPORT_TIMEOUT_MS = 30_000;

// A filtered run can start at the 'unset' case alone, and the ambient environment
// can export the harness variables (the evaluation runner itself does), so capture
// the ambient values at module load; afterEach restores them and each case sets or
// deletes what it needs from a known state.
const AMBIENT_TOOL_CACHE_DIR = process.env[TOOL_CACHE_DIR];
const AMBIENT_DISABLE_TOOL_CACHES = process.env[DISABLE_TOOL_CACHES];

async function loadConfigs() {
  vi.resetModules();
  const vite = (await import("../vite.config")).default;
  const vitest = (await import("../vitest.config")).default;
  return { vite, vitest };
}

afterEach(() => {
  if (AMBIENT_TOOL_CACHE_DIR === undefined) {
    delete process.env[TOOL_CACHE_DIR];
  } else {
    process.env[TOOL_CACHE_DIR] = AMBIENT_TOOL_CACHE_DIR;
  }
  if (AMBIENT_DISABLE_TOOL_CACHES === undefined) {
    delete process.env[DISABLE_TOOL_CACHES];
  } else {
    process.env[DISABLE_TOOL_CACHES] = AMBIENT_DISABLE_TOOL_CACHES;
  }
  vi.resetModules();
});

describe("frontend tool-cache environment", () => {
  it("redirects both Vite caches and switches the vitest caches off under the harness variables", { timeout: CONFIG_IMPORT_TIMEOUT_MS }, async () => {
    process.env[TOOL_CACHE_DIR] = SEALED_CACHE;
    process.env[DISABLE_TOOL_CACHES] = "1";
    const { vite, vitest } = await loadConfigs();
    expect(vite.cacheDir).toBe(path.join(SEALED_CACHE, "vite"));
    expect(vitest.cacheDir).toBe(path.join(SEALED_CACHE, "vite"));
    expect(vitest.test?.cache).toBe(false);
    expect(vitest.test?.fsModuleCachePath).toBe(path.join(SEALED_CACHE, "vitest-fs"));
    expect(vitest.test?.fsModuleCache).toBe(false);
  });

  it("leaves every cache default unchanged when the harness variables are unset", { timeout: CONFIG_IMPORT_TIMEOUT_MS }, async () => {
    delete process.env[TOOL_CACHE_DIR];
    delete process.env[DISABLE_TOOL_CACHES];
    expect(process.env[TOOL_CACHE_DIR]).toBeUndefined();
    expect(process.env[DISABLE_TOOL_CACHES]).toBeUndefined();
    const { vite, vitest } = await loadConfigs();
    expect(vite.cacheDir).toBeUndefined();
    expect(vitest.cacheDir).toBeUndefined();
    expect(vitest.test?.cache).toBeUndefined();
    expect(vitest.test?.fsModuleCachePath).toBeUndefined();
    expect(vitest.test?.fsModuleCache).toBeUndefined();
  });
});
