<script setup lang="ts">
import { computed } from "vue";
import { RouterLink, useRouter } from "vue-router";
import { useServersStore } from "@/stores/servers";
import { useAuthStore } from "@/stores/auth";
import ThemeToggle from "@/components/ThemeToggle.vue";

const servers = useServersStore();
const auth = useAuthStore();
const router = useRouter();

// Navigation mirrors docs/UI_IA.md. Settings + account live in the footer.
const nav: { name: string; label: string }[] = [
  { name: "dashboard", label: "Dashboard" },
  { name: "servers", label: "Servers" },
  { name: "services", label: "Services" },
  { name: "containers", label: "Containers" },
  { name: "applications", label: "Applications" },
  { name: "domains", label: "Domains" },
  { name: "network", label: "Network" },
  { name: "storage", label: "Storage" },
  { name: "databases", label: "Databases" },
  { name: "logs", label: "Logs" },
  { name: "alerts", label: "Alerts" },
  { name: "metrics", label: "Metrics" },
  { name: "infrastructure", label: "Infrastructure" },
  { name: "security", label: "Security" },
  { name: "integrations", label: "Integrations" },
];

// Until the first server connects, keep the nav focused on getting set up.
const visible = computed(() =>
  servers.list.length > 0 ? nav : nav.filter((i) => i.name === "dashboard"),
);

const email = computed(() => auth.session?.email ?? "");
const role = computed(() => auth.session?.role ?? "");
const initial = computed(() => (email.value ? email.value[0]!.toUpperCase() : "P"));

async function logout() {
  await auth.logout();
  router.push({ name: "login" });
}
</script>

<template>
  <aside class="sidebar">
    <div class="brand-row">
      <span class="brand-dot"></span>
      <span class="brand-name">Pulse</span>
    </div>

    <nav class="nav">
      <RouterLink
        v-for="item in visible"
        :key="item.name"
        :to="{ name: item.name }"
        class="nav-link"
        active-class="nav-link-active"
      >
        {{ item.label }}
      </RouterLink>
    </nav>

    <div class="foot">
      <RouterLink :to="{ name: 'settings' }" class="nav-link" active-class="nav-link-active">
        Settings
      </RouterLink>

      <div class="foot-row">
        <span class="foot-k">Appearance</span>
        <ThemeToggle />
      </div>

      <div class="profile">
        <span class="avatar">{{ initial }}</span>
        <div class="who">
          <span class="who-email" :title="email">{{ email || "Signed in" }}</span>
          <span class="who-role">{{ role }}</span>
        </div>
        <button class="signout" title="Sign out" aria-label="Sign out" @click="logout">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
            <path d="M16 17l5-5-5-5M21 12H9" />
          </svg>
        </button>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  width: 236px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--pulse-border);
  background: rgba(255, 255, 255, 0.02);
  backdrop-filter: blur(14px);
  -webkit-backdrop-filter: blur(14px);
}
.brand-row {
  height: 60px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 18px;
  border-bottom: 1px solid var(--pulse-border);
}
.brand-dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--pulse-accent);
  box-shadow: 0 0 12px var(--pulse-accent);
}
.brand-name {
  font-family: var(--pulse-font-display);
  font-weight: 700;
  font-size: 18px;
  letter-spacing: -0.02em;
}
.nav {
  flex: 1;
  overflow-y: auto;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.foot {
  border-top: 1px solid var(--pulse-border);
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.foot-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 2px 12px 6px;
}
.foot-k {
  font-size: 12px;
  color: var(--pulse-text-muted);
}
.profile {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 12px;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
}
.avatar {
  width: 30px;
  height: 30px;
  border-radius: 9px;
  display: grid;
  place-items: center;
  background: rgba(199, 245, 66, 0.14);
  color: var(--pulse-accent);
  border: 1px solid rgba(199, 245, 66, 0.3);
  font-family: var(--pulse-font-display);
  font-weight: 700;
  font-size: 14px;
  flex-shrink: 0;
}
.who {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1;
}
.who-email {
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.who-role {
  font-size: 11px;
  color: var(--pulse-text-muted);
  text-transform: capitalize;
}
.signout {
  background: transparent;
  border: 0;
  color: var(--pulse-text-muted);
  cursor: pointer;
  padding: 6px;
  border-radius: 8px;
  display: grid;
  place-items: center;
}
.signout:hover {
  color: var(--pulse-down);
  background: rgba(248, 113, 113, 0.1);
}
</style>
