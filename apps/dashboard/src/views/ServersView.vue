<script setup lang="ts">
import { ref } from "vue";
import { storeToRefs } from "pinia";
import { useRouter } from "vue-router";
import { useServersStore } from "@/stores/servers";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";
import { timeAgo } from "@/lib/format";

const servers = useServersStore();
const { list, loading } = storeToRefs(servers);
const router = useRouter();

const confirmId = ref<string | null>(null);
const removingId = ref<string | null>(null);
const err = ref("");

function open(id: string) {
  servers.select(id);
  router.push({ name: "server-detail", params: { id } });
}

async function remove(id: string) {
  removingId.value = id;
  err.value = "";
  try {
    await servers.remove(id);
    confirmId.value = null;
    // Back to onboarding when nothing is left to show.
    if (servers.list.length === 0) router.push({ name: "dashboard" });
  } catch (e) {
    err.value = e instanceof Error ? e.message : "failed to remove";
  } finally {
    removingId.value = null;
  }
}
</script>

<template>
  <div>
    <PageHeader
      title="Servers"
      subtitle="Every VPS connected to Pulse. Removing one clears its data; if the agent is still running it will reconnect within a minute — uninstall the agent on that VPS to remove it for good."
    />
    <EmptyState
      v-if="!loading && list.length === 0"
      title="No servers yet"
      message="Run the installer on a VPS. In cloud mode the agent dials out and appears here automatically."
    />
    <div v-else class="card overflow-x-auto">
      <table class="table">
        <thead>
          <tr>
            <th>Hostname</th>
            <th>Server ID</th>
            <th>Mode</th>
            <th>Status</th>
            <th>Last seen</th>
            <th class="text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in list" :key="s.id" class="row" @click="open(s.id)">
            <td class="font-medium">{{ s.hostname || "—" }}</td>
            <td class="font-mono text-xs text-muted">{{ s.server_id }}</td>
            <td class="text-muted">{{ s.mode }}</td>
            <td><HealthBadge :status="s.status" /></td>
            <td class="text-muted">{{ timeAgo(s.last_seen_at) }}</td>
            <td class="text-right whitespace-nowrap" @click.stop>
              <template v-if="confirmId === s.id">
                <span class="text-xs text-muted mr-2">Remove this server?</span>
                <button class="rm-btn" :disabled="removingId === s.id" @click="confirmId = null">Cancel</button>
                <button class="rm-btn rm-confirm" :disabled="removingId === s.id" @click="remove(s.id)">
                  {{ removingId === s.id ? "Removing…" : "Confirm" }}
                </button>
              </template>
              <button v-else class="rm-btn rm-danger" @click="confirmId = s.id">
                <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2m2 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
                  <path d="M10 11v6M14 11v6" />
                </svg>
                Remove
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="err" class="text-sm text-down mt-3">{{ err }}</p>
    </div>
  </div>
</template>

<style scoped>
.row {
  cursor: pointer;
  transition: background 0.12s;
}
.row:hover {
  background: var(--pulse-surface-2);
}
.rm-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 11px;
  border-radius: 9px;
  font-size: 12px;
  font-family: var(--pulse-font-mono);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text-muted);
  transition: all 0.14s;
}
.rm-btn:hover {
  color: var(--pulse-text);
  border-color: var(--pulse-text-muted);
}
.rm-btn:disabled {
  opacity: 0.5;
  cursor: default;
}
.rm-danger {
  color: #fca5a5;
  border-color: transparent;
}
.rm-danger:hover {
  color: #fecaca;
  background: rgba(248, 113, 113, 0.12);
  border-color: rgba(248, 113, 113, 0.35);
}
.rm-confirm {
  color: #fca5a5;
  background: rgba(248, 113, 113, 0.14);
  border-color: rgba(248, 113, 113, 0.4);
  margin-left: 4px;
}
.rm-confirm:hover {
  background: rgba(248, 113, 113, 0.24);
}
</style>
