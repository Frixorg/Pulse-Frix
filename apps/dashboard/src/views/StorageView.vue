<script setup lang="ts">
import { ref, watch, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { Resource } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";
import { bytes } from "@/lib/format";

const servers = useServersStore();
const { selected } = storeToRefs(servers);
const rows = ref<Resource[]>([]);

async function load() {
  if (!selected.value) return;
  try {
    const snap = await api.discovery(selected.value.id);
    rows.value = (snap.resources ?? []).filter((r) => r.type === "filesystem");
  } catch {
    rows.value = [];
  }
}
onMounted(load);
watch(selected, load);

function n(r: Resource, k: string): number {
  return Number(r.attributes?.[k] ?? 0);
}
</script>

<template>
  <div>
    <PageHeader title="Storage" subtitle="Filesystems, capacity, inodes. Pulse caps its own storage and never fills your disk." />
    <EmptyState v-if="rows.length === 0" title="No filesystems discovered" />
    <div v-else class="card overflow-x-auto">
      <table class="table">
        <thead><tr><th>Mount</th><th>Health</th><th>Used</th><th>Total</th><th>Used %</th><th>Inodes %</th></tr></thead>
        <tbody>
          <tr v-for="r in rows" :key="r.id">
            <td class="font-medium">{{ r.name }}</td>
            <td><HealthBadge :status="r.health" /></td>
            <td class="font-mono text-xs">{{ bytes(n(r, "used_bytes")) }}</td>
            <td class="font-mono text-xs">{{ bytes(n(r, "total_bytes")) }}</td>
            <td class="font-mono text-xs">{{ n(r, "used_pct") }}%</td>
            <td class="font-mono text-xs">{{ n(r, "inode_pct") }}%</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
