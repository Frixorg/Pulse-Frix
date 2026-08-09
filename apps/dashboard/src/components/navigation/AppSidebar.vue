<script setup lang="ts">
import { computed } from "vue";
import { RouterLink } from "vue-router";
import { useServersStore } from "@/stores/servers";

const servers = useServersStore();

// Navigation mirrors docs/UI_IA.md. Icons are inline SVG (no icon dependency).
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
  { name: "settings", label: "Settings" },
];

// Until the first server connects, keep the nav focused on getting set up;
// reveal the full infrastructure tabs once there's something to show.
const visible = computed(() =>
  servers.list.length > 0 ? nav : nav.filter((i) => i.name === "dashboard" || i.name === "settings"),
);
</script>

<template>
  <aside class="w-56 shrink-0 border-r border-border bg-surface flex flex-col">
    <div class="h-14 flex items-center gap-2 px-4 border-b border-border">
      <span class="inline-block w-2.5 h-2.5 rounded-full bg-healthy animate-pulse"></span>
      <span class="font-semibold tracking-tight">Pulse</span>
    </div>
    <nav class="flex-1 overflow-y-auto p-2 space-y-0.5">
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
    <div class="p-3 text-[11px] text-muted border-t border-border">
      Observe first. Change nothing by default.
    </div>
  </aside>
</template>
