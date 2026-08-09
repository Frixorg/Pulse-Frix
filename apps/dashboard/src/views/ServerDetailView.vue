<script setup lang="ts">
import { ref, onMounted } from "vue";
import { api } from "@/api/client";
import type { ServerSummary, Topology, MetricSeries } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import StatCard from "@/components/cards/StatCard.vue";
import MetricChart from "@/components/charts/MetricChart.vue";
import TopologyGraph from "@/components/topology/TopologyGraph.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";
import { bytes, uptime, tone } from "@/lib/format";

const props = defineProps<{ id: string }>();

const summary = ref<ServerSummary | null>(null);
const topology = ref<Topology | null>(null);
const cpu = ref<MetricSeries[]>([]);
const net = ref<MetricSeries[]>([]);
const range = ref("6h");

async function loadCharts() {
  const [cpuM, netM] = await Promise.all([
    api.metrics(props.id, "cpu", range.value).catch(() => ({ series: [] })),
    api.metrics(props.id, "network", range.value).catch(() => ({ series: [] })),
  ]);
  cpu.value = cpuM.series;
  net.value = netM.series;
}

onMounted(async () => {
  const [s, t] = await Promise.all([
    api.summary(props.id),
    api.topology(props.id).catch(() => ({ nodes: [], edges: [] })),
  ]);
  summary.value = s;
  topology.value = t;
  await loadCharts();
});
</script>

<template>
  <div>
    <PageHeader :title="summary?.server.hostname || 'Server'" subtitle="Server detail">
      <template #actions>
        <div class="flex items-center gap-2">
          <HealthBadge :status="summary?.health" />
          <select v-model="range" class="bg-surface-2 border border-border rounded-md text-xs px-2 py-1"
            @change="loadCharts">
            <option>1h</option><option>6h</option><option>24h</option><option>7d</option><option>30d</option>
          </select>
        </div>
      </template>
    </PageHeader>

    <div class="grid grid-cols-2 md:grid-cols-5 gap-3 mb-3">
      <StatCard label="CPU" :value="(summary?.cpu_percent ?? 0).toFixed(0)" suffix="%"
        :percent="summary?.cpu_percent ?? 0" :tone="tone(summary?.cpu_percent ?? 0)" />
      <StatCard label="Memory" :value="summary?.mem_used_pct ?? 0" suffix="%"
        :percent="summary?.mem_used_pct ?? 0" :tone="tone(summary?.mem_used_pct ?? 0)" />
      <StatCard label="Disk" :value="summary?.disk_used_pct ?? 0" suffix="%"
        :percent="summary?.disk_used_pct ?? 0" :tone="tone(summary?.disk_used_pct ?? 0)" />
      <StatCard label="Net RX" :value="bytes(summary?.net_rx_bytes ?? 0)" />
      <StatCard label="Uptime" :value="uptime(summary?.uptime_sec ?? 0)" />
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-3 mb-3">
      <MetricChart :title="`CPU (${range})`" :series="cpu" />
      <MetricChart :title="`Network RX (${range})`" :series="net" />
    </div>

    <TopologyGraph :topology="topology" />
  </div>
</template>
