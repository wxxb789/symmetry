import react from "@vitejs/plugin-react";
import path from "node:path";
import { defineConfig } from "vitest/config";

// Vitest writes its results cache under Vite's `cacheDir` and its transformed-module
// cache under `fsModuleCachePath`, both inside `node_modules` by default. A harness
// that runs the suite from a sealed checkout exports this variable to keep run state
// out of the sealed revision (the evaluator's runbook names it); with the variable
// unset both defaults are unchanged.
const toolCacheDir = process.env["SYMMETRY_TOOL_CACHE_DIR"];

export default defineConfig({
  plugins: [react()],
  ...(toolCacheDir ? { cacheDir: path.join(toolCacheDir, "vite") } : {}),
  test: {
    environment: "jsdom",
    environmentOptions: { jsdom: { pretendToBeVisual: true } },
    setupFiles: ["./src/test-setup.ts"],
    restoreMocks: true,
    clearMocks: true,
    ...(toolCacheDir ? { fsModuleCachePath: path.join(toolCacheDir, "vitest-fs") } : {}),
  },
});
