<script setup lang="ts">
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

function open(id: string) {
  servers.select(id);
  router.push({ name: "server-detail", params: { id } });
}
</script>

<template>
  <div>
    <PageHeader title="Servers" subtitle="Every VPS connected to Pulse." />
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
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in list" :key="s.id" class="cursor-pointer hover:bg-surface-2" @click="open(s.id)">
            <td class="font-medium">{{ s.hostname || "—" }}</td>
            <td class="font-mono text-xs text-muted">{{ s.server_id }}</td>
            <td class="text-muted">{{ s.mode }}</td>
            <td><HealthBadge :status="s.status" /></td>
            <td class="text-muted">{{ timeAgo(s.last_seen_at) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
