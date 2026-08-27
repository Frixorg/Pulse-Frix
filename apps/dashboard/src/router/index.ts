import { createRouter, createWebHistory, type RouteRecordRaw } from "vue-router";
import { useAuthStore } from "@/stores/auth";

// Route table.
//
// Cloud build: a public marketing landing lives at "/" and the authenticated
// dashboard lives under "/app".
//
// Self-hosted build (__SELF_HOSTED__): there is no marketing surface. "/"
// redirects straight to "/app", which the auth guard resolves to the dashboard
// for a signed-in user and to "/login" for everyone else. The marketing views
// are behind a `!__SELF_HOSTED__` branch, so Vite replaces the flag with a
// literal and Rollup drops both the branch and its dynamic imports — the
// landing chunks are never emitted into a self-hosted image.
//
// Views are lazy-loaded so the initial bundle stays small.
const publicRoutes: RouteRecordRaw[] = [];

if (!__SELF_HOSTED__) {
  publicRoutes.push(
    { path: "/", name: "landing", component: () => import("@/views/LandingView.vue"), meta: { public: true } },
    { path: "/self-hosted", name: "self-hosted", component: () => import("@/views/SelfHostedView.vue"), meta: { public: true } },
    { path: "/welcome", name: "welcome", component: () => import("@/views/FirstRunView.vue"), meta: { public: true } },
  );
} else {
  publicRoutes.push({ path: "/", name: "root", redirect: { name: "dashboard" } });
}

const routes: RouteRecordRaw[] = [
  ...publicRoutes,
  { path: "/login", name: "login", component: () => import("@/views/LoginView.vue"), meta: { public: true } },
  // First-boot admin provisioning. Reachable only while the control plane
  // reports that no account exists yet; the API enforces the same rule.
  { path: "/setup", name: "setup", component: () => import("@/views/SetupView.vue"), meta: { public: true } },
  {
    path: "/app",
    component: () => import("@/layouts/AppShell.vue"),
    children: [
      { path: "", name: "dashboard", component: () => import("@/views/DashboardView.vue") },
      { path: "servers", name: "servers", component: () => import("@/views/ServersView.vue") },
      { path: "servers/:id", name: "server-detail", component: () => import("@/views/ServerDetailView.vue"), props: true },
      { path: "services", name: "services", component: () => import("@/views/ServicesView.vue") },
      { path: "containers", name: "containers", component: () => import("@/views/ContainersView.vue") },
      { path: "runtimes", name: "runtimes", component: () => import("@/views/ApplicationsView.vue") },
      { path: "domains", name: "domains", component: () => import("@/views/DomainsView.vue") },
      { path: "network", name: "network", component: () => import("@/views/NetworkView.vue") },
      { path: "storage", name: "storage", component: () => import("@/views/StorageView.vue") },
      { path: "databases", name: "databases", component: () => import("@/views/DatabasesView.vue") },
      { path: "inventory", name: "inventory", component: () => import("@/views/InventoryView.vue") },
      { path: "logs", name: "logs", component: () => import("@/views/LogsView.vue") },
      { path: "ssh", name: "ssh", component: () => import("@/views/SshView.vue") },
      { path: "alerts", name: "alerts", component: () => import("@/views/AlertsView.vue") },
      { path: "metrics", name: "metrics", component: () => import("@/views/MetricsView.vue") },
      { path: "infrastructure", name: "infrastructure", component: () => import("@/views/InfrastructureView.vue") },
      { path: "security", name: "security", component: () => import("@/views/SecurityView.vue") },
      { path: "integrations", name: "integrations", component: () => import("@/views/IntegrationsView.vue") },
      { path: "settings", name: "settings", component: () => import("@/views/SettingsView.vue") },
    ],
  },
  { path: "/:pathMatch(.*)*", redirect: "/" },
];

export const router = createRouter({
  history: createWebHistory(),
  routes,
});

// Auth guard. Frontend guards are UX only; the API enforces authorization and
// the first-boot rule independently.
router.beforeEach(async (to) => {
  const auth = useAuthStore();
  if (!auth.checked) await auth.fetchSession();

  // First boot: no administrator exists yet, so everything funnels into the
  // provisioning wizard until one does.
  if (!auth.isAuthenticated) {
    if (!auth.setupChecked) await auth.fetchSetupStatus();
    if (auth.setupRequired) return to.name === "setup" ? true : { name: "setup" };
  }
  if (to.name === "setup") {
    return auth.isAuthenticated ? { name: "dashboard" } : { name: "login" };
  }

  if (to.meta.public) return true;
  if (!auth.isAuthenticated) return { name: "login", query: { redirect: to.fullPath } };
  return true;
});
