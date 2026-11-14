import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const target = process.env["SYMMETRY_PHOENIX_ORIGIN"] ?? "http://127.0.0.1:4000";

export default defineConfig({
  base: "/portal/",
  plugins: [react(), tailwindcss()],
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: {
      "/portal/api": { target, changeOrigin: false },
      "/portal/login": { target, changeOrigin: false },
      "/portal/logout": { target, changeOrigin: false },
      "/socket": { target, changeOrigin: false, ws: true },
    },
  },
});
