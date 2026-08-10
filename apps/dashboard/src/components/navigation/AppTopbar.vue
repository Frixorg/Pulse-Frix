<script setup lang="ts">
import { useServersStore } from "@/stores/servers";

const servers = useServersStore();
</script>

<template>
  <header class="topbar">
    <div class="left">
      <template v-if="servers.list.length">
        <span class="sel-k">Server</span>
        <select
          class="sel"
          :value="servers.selectedId ?? ''"
          @change="servers.select(($event.target as HTMLSelectElement).value)"
        >
          <option v-for="s in servers.list" :key="s.id" :value="s.id">
            {{ s.hostname || s.server_id }}
          </option>
        </select>
        <span class="count">{{ servers.list.length }} connected</span>
      </template>
      <span v-else class="muted">No servers yet</span>
    </div>
  </header>
</template>

<style scoped>
.topbar {
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
.sel {
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
  border-radius: 10px;
  font-size: 13px;
  padding: 6px 10px;
  font-family: var(--pulse-font-mono);
}
.count {
  font-size: 12px;
  color: var(--pulse-text-muted);
}
.muted {
  font-size: 13px;
  color: var(--pulse-text-muted);
}
</style>
