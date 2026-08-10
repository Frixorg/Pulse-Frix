<script setup lang="ts">
import { ref, computed } from "vue";
import { useServersStore } from "@/stores/servers";

const servers = useServersStore();
const confirming = ref(false);
const removing = ref(false);
const err = ref("");

const current = computed(() => servers.selected);

async function remove() {
  const id = servers.selectedId;
  if (!id) return;
  removing.value = true;
  err.value = "";
  try {
    await servers.remove(id);
    confirming.value = false;
  } catch (e) {
    err.value = e instanceof Error ? e.message : "failed to remove";
  } finally {
    removing.value = false;
  }
}
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

    <div v-if="current" class="right">
      <span v-if="err" class="err">{{ err }}</span>
      <button v-if="!confirming" class="btn btn-danger" @click="confirming = true">Remove server</button>
      <template v-else>
        <span class="confirm-q">Remove “{{ current.hostname || current.server_id }}”?</span>
        <button class="btn btn-glass" :disabled="removing" @click="confirming = false">Cancel</button>
        <button class="btn btn-danger" :disabled="removing" @click="remove">
          {{ removing ? "Removing…" : "Confirm" }}
        </button>
      </template>
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
.right {
  display: flex;
  align-items: center;
  gap: 10px;
}
.confirm-q {
  font-size: 13px;
}
.err {
  font-size: 12px;
  color: var(--pulse-down);
}
</style>
