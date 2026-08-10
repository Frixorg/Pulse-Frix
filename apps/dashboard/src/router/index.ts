import { createRouter, createWebHistory, type RouteRecordRaw } from "vue-router";
import { useAuthStore } from "@/stores/auth";

// Public marketing landing lives at "/"; the authenticated dashboard lives
// under "/app". Views are lazy-loaded so the initial bundle stays small.
const routes: RouteRecordRaw[] = [
  { path: "/", name: "landing", component: () => import("@/views/LandingView.vue"), meta: { public: true } },
  { path: "/login", name: "login", component: () => import("@/views/LoginView.vue"), meta: { public: true } },
  { path: "/self-hosted", name: "self-hosted", component: () => import("@/views/SelfHostedView.vue"), meta: { public: true } },
  { path: "/welcome", name: "welcome", component: () => import("@/views/FirstRunView.vue"), meta: { public: true } },
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
      { path: "logs", name: "logs", component: () => import("@/views/LogsView.vue") },
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

// Auth guard: unauthenticated users hitting /app are sent to /login. Frontend
// guards are UX only; the API enforces authorization independently.
router.beforeEach(async (to) => {
  const auth = useAuthStore();
  if (!auth.checked) await auth.fetchSession();
  if (to.meta.public) return true;
  if (!auth.isAuthenticated) return { name: "login", query: { redirect: to.fullPath } };
  return true;
});
