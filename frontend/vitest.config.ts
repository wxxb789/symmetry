import react from "@vitejs/plugin-react";
import path from "node:path";
import { defineConfig } from "vitest/config";

// Vitest writes its results cache under Vite's `cacheDir` and its transformed-module
// cache under `fsModuleCachePath`, both inside `node_modules` by default. A harness
// that runs the suite from a sealed checkout exports this variable to keep run state
// out of the sealed revision (the evaluator's runbook names it); with the variable
// unset both defaults are unchanged.
const toolCacheDir = process.env["SYMMETRY_TOOL_CACHE_DIR"];
// A sealed-checkout harness also switches the cache channels off: the
// transformed-module cache would execute code from an earlier run unvalidated, and the
// results cache would order and select this run's tests from an earlier run's results.
// Worker runs (no variable) keep the caches.
const disableToolCaches = process.env["SYMMETRY_DISABLE_TOOL_CACHES"] === "1";

export default defineConfig({
  plugins: [react()],
  ...(toolCacheDir ? { cacheDir: path.join(toolCacheDir, "vite") } : {}),
  test: {
    environment: "jsdom",
    environmentOptions: { jsdom: { pretendToBeVisual: true } },
    setupFiles: ["./src/test-setup.ts"],
    restoreMocks: true,
    clearMocks: true,
    // `cache` is a test-level option: at the config root it is an unknown Vite key
    // that leaves vitest's results cache on.
    ...(disableToolCaches ? { cache: false, fsModuleCache: false } : {}),
    ...(toolCacheDir ? { fsModuleCachePath: path.join(toolCacheDir, "vitest-fs") } : {}),
  },
});
