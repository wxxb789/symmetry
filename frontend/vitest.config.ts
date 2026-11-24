import react from "@vitejs/plugin-react";
import path from "node:path";
import { defineConfig } from "vitest/config";

// Vitest writes its results cache under Vite's `cacheDir` and its transformed-module
// cache under `fsModuleCachePath`, both inside `node_modules` by default. A harness
// that runs the suite from a sealed checkout exports this variable to keep run state
// out of the sealed revision (the evaluator's runbook names it); with the variable
// unset both defaults are unchanged.
const toolCacheDir = process.env.SYMMETRY_TOOL_CACHE_DIR;
// A sealed-checkout harness also switches the cache channels off: the
// transformed-module cache would reuse a previous invocation's transformed output for
// identical source bytes, keyed by a partly-hashed environment, and the results cache
// would carry an earlier run's results (test ordering and `--last-failed` selection).
// Worker runs (no variable) keep the caches.
const disableToolCaches = process.env.SYMMETRY_DISABLE_TOOL_CACHES === "1";

export default defineConfig({
  plugins: [react()],
  ...(toolCacheDir ? { cacheDir: path.join(toolCacheDir, "vite") } : {}),
  test: {
    environment: "jsdom",
    environmentOptions: { jsdom: { pretendToBeVisual: true } },
    // Explicit per-test budget the suite's waits stay strictly below (goal-rf-w5-04).
    testTimeout: 15_000,
    setupFiles: ["./src/test-setup.ts"],
    restoreMocks: true,
    clearMocks: true,
    pool: "threads",
    maxWorkers: "50%",
    // `cache` is a test-level option: at the config root it is an unknown Vite key
    // that leaves vitest's results cache on.
    ...(disableToolCaches ? { cache: false, fsModuleCache: false } : {}),
    ...(toolCacheDir ? { fsModuleCachePath: path.join(toolCacheDir, "vitest-fs") } : {}),
    coverage: {
      provider: "v8",
      reporter: ["text-summary", "lcov"],
      reportsDirectory: toolCacheDir ? path.join(toolCacheDir, "coverage") : "coverage",
      // Floors (rf-w6-12): the minimum of four green main-run lcov.info totals minus one point; lcov
      // carries lines, functions and branches only, so `statements` stays unset for lack of a baseline.
      thresholds: {
        lines: 93.55,
        functions: 90.12,
        branches: 83.12,
      },
    },
  },
});
