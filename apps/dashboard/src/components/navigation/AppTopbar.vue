<script setup lang="ts">
import { useServersStore } from "@/stores/servers";
import ServerSelect from "@/components/navigation/ServerSelect.vue";

const servers = useServersStore();
defineEmits<{ (e: "menu"): void }>();
</script>

<template>
  <header class="topbar">
    <div class="left">
      <button class="menu-btn" aria-label="Open menu" @click="$emit('menu')">
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M3 6h18M3 12h18M3 18h18" />
        </svg>
      </button>
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
  z-index: 30;
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
  min-width: 0;
}
.menu-btn {
  display: none;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: 9px;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
  cursor: pointer;
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
@media (max-width: 900px) {
  .topbar {
    padding: 0 14px;
  }
  .menu-btn {
    display: inline-grid;
  }
  .sel-k {
    display: none;
  }
}
</style>
