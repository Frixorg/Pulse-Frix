<script setup lang="ts">
import { useServersStore } from "@/stores/servers";
import ServerSelect from "@/components/navigation/ServerSelect.vue";

const servers = useServersStore();
</script>

<template>
  <header class="topbar">
    <div class="left">
      <template v-if="servers.list.length">
        <span class="sel-k">Server</span>
        <ServerSelect />
      </template>
      <span v-else class="muted">No servers yet</span>
    </div>
  </header>
</template>

<style scoped>
.topbar {
  position: relative;
  z-index: 30; /* keep the server dropdown above the scrolling content */
  height: 60px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--pulse-border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 22px;
  background: rgba(255, 255, 255, 0.02);
  backdrop-filter: blur(14px);
  -webkit-backdrop-filter: blur(14px);
}
.left {
  display: flex;
  align-items: center;
  gap: 12px;
}
.sel-k {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--pulse-text-muted);
}
.muted {
  font-size: 13px;
  color: var(--pulse-text-muted);
}
</style>
