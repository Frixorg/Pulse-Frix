import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import { fileURLToPath, URL } from "node:url";

// The dashboard talks to the Pulse API. In dev, /api and /healthz are proxied
// to the local API (default :8080). In production the SPA and API are served
// behind the same origin (see infrastructure/ and apps/dashboard/nginx.conf).
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      // ws:true is required for the SSH console: it upgrades
      // /api/v1/servers/*/ssh/sessions/*/attach to a WebSocket.
      "/api": { target: "http://localhost:8080", changeOrigin: true, ws: true },
      "/healthz": { target: "http://localhost:8080", changeOrigin: true },
    },
  },
  build: {
    outDir: "dist",
    sourcemap: false,
  },
});
