<script setup lang="ts">
import { ref, watch, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { DetectorResult } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";

const servers = useServersStore();
const { selected } = storeToRefs(servers);
const detectors = ref<DetectorResult[]>([]);

async function load() {
  if (!selected.value) return;
  try {
    detectors.value = (await api.discovery(selected.value.id)).detectors ?? [];
  } catch {
    detectors.value = [];
  }
}
onMounted(load);
watch(selected, load);
</script>

<template>
  <div>
    <PageHeader title="Infrastructure" subtitle="Detector health — Pulse degrades gracefully when an integration is unavailable." />
    <EmptyState v-if="detectors.length === 0" title="No discovery data yet" />
    <div v-else class="card overflow-x-auto">
      <table class="table">
        <thead><tr><th>Detector</th><th>Status</th><th>Found</th><th>Duration</th><th>Note</th></tr></thead>
        <tbody>
          <tr v-for="d in detectors" :key="d.id">
            <td class="font-medium">{{ d.name }}</td>
            <td>
              <span :class="d.available ? 'text-healthy' : 'text-unknown'">{{ d.available ? "available" : "unavailable" }}</span>
            </td>
            <td class="font-mono text-xs">{{ d.count }}</td>
            <td class="font-mono text-xs">{{ d.duration_ms }}ms</td>
            <td class="text-muted text-xs">{{ d.error || d.reason || "" }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
