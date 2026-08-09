<script setup lang="ts">
import { ref, watch, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { Resource } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import { bytes } from "@/lib/format";

const servers = useServersStore();
const { selected } = storeToRefs(servers);
const rows = ref<Resource[]>([]);

async function load() {
  if (!selected.value) return;
  try {
    const snap = await api.discovery(selected.value.id);
    rows.value = (snap.resources ?? []).filter((r) => r.type === "network_interface");
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
    <PageHeader title="Network" subtitle="Interfaces and traffic counters (read from /proc, read-only)." />
    <EmptyState v-if="rows.length === 0" title="No interfaces discovered" />
    <div v-else class="card overflow-x-auto">
      <table class="table">
        <thead><tr><th>Interface</th><th>State</th><th>RX</th><th>TX</th><th>RX errors</th><th>TX errors</th></tr></thead>
        <tbody>
          <tr v-for="r in rows" :key="r.id">
            <td class="font-medium">{{ r.name }}</td>
            <td class="text-muted">{{ r.attributes?.operstate ?? "—" }}</td>
            <td class="font-mono text-xs">{{ bytes(n(r, "rx_bytes")) }}</td>
            <td class="font-mono text-xs">{{ bytes(n(r, "tx_bytes")) }}</td>
            <td class="font-mono text-xs">{{ n(r, "rx_errors") }}</td>
            <td class="font-mono text-xs">{{ n(r, "tx_errors") }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
