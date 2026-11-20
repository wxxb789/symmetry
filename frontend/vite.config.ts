import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import path from "node:path";
import { defineConfig } from "vite";

const target = process.env["SYMMETRY_PHOENIX_ORIGIN"] ?? "http://127.0.0.1:4000";
// Vite's dependency/optimizer cache defaults to `node_modules/.vite` inside the
// tree. A harness that runs the build from a sealed checkout exports this variable
// to keep run state out of the sealed revision (the evaluator's runbook names it);
// with the variable unset the default location is unchanged. A production build has
// no cache channel that executes previously transformed code (the optimizer cache is
// a dev-server concern), so only `frontend/vitest.config.ts` has options to switch
// off.
const toolCacheDir = process.env["SYMMETRY_TOOL_CACHE_DIR"];

export default defineConfig({
  base: "/portal/",
  plugins: [react(), tailwindcss()],
  ...(toolCacheDir ? { cacheDir: path.join(toolCacheDir, "vite") } : {}),
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: {
      "/portal/api": { target, changeOrigin: false },
      "/portal/login": { target, changeOrigin: false },
      "/portal/logout": { target, changeOrigin: false },
      "/assets/login.css": { target, changeOrigin: false },
      // The operator invalidation socket, proxied to Phoenix with the WebSocket
      // upgrade; the SPA and Phoenix share the dev origin, so cookies apply.
      "/portal/socket": { target, changeOrigin: false, ws: true },
    },
  },
});
