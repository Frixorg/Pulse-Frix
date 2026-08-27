import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import { fileURLToPath, URL } from "node:url";

// Deployment mode is decided at BUILD time so the marketing bundle can be
// dropped entirely from a self-hosted image (see below). A deployment is
// self-hosted when any of these is set:
//
//   IS_SELF_HOSTED=true | APP_MODE=self_hosted | PULSE_MODE=local
//
// PULSE_MODE is the repo-wide name for the same distinction ("local" is the
// self-hosted stack, "cloud" is pulse.frix.me), so it is honoured here too.
function truthy(v: string | undefined): boolean {
  return ["1", "true", "yes", "on"].includes((v ?? "").trim().toLowerCase());
}

function isSelfHosted(env: NodeJS.ProcessEnv): boolean {
  const appMode = (env.APP_MODE ?? "").trim().toLowerCase().replace(/[-\s]/g, "_");
  if (truthy(env.IS_SELF_HOSTED)) return true;
  if (appMode === "self_hosted" || appMode === "selfhosted") return true;
  return (env.PULSE_MODE ?? "").trim().toLowerCase() === "local";
}

// The dashboard talks to the Pulse API. In dev, /api and /healthz are proxied
// to the local API (default :8080). In production the SPA and API are served
// behind the same origin (see infrastructure/ and apps/dashboard/nginx.conf).
export default defineConfig(() => {
  const selfHosted = isSelfHosted(process.env);
  return {
    plugins: [vue()],
    // __SELF_HOSTED__ is substituted as a literal, so the router's
    // `if (!__SELF_HOSTED__) { ... }` branch — and every dynamic import inside
    // it — is dead-code-eliminated from a self-hosted build. The landing and
    // marketing chunks are never emitted, which is what keeps the cold start
    // small on a small VPS.
    define: {
      __SELF_HOSTED__: JSON.stringify(selfHosted),
    },
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
  };
});
