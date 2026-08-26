<script setup lang="ts">
import { ref, watch, onMounted } from "vue";
import { api } from "@/api/client";
import type { ServerSummary, Topology, MetricSeries } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import StatCard from "@/components/cards/StatCard.vue";
import MetricChart from "@/components/charts/MetricChart.vue";
import TopologyGraph from "@/components/topology/TopologyGraph.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";
import RefreshButton from "@/components/RefreshButton.vue";
import { bytes, uptime, tone } from "@/lib/format";

const props = defineProps<{ id: string }>();

const RANGES = ["1h", "6h", "24h", "7d", "30d"];

const summary = ref<ServerSummary | null>(null);
const topology = ref<Topology | null>(null);
const cpu = ref<MetricSeries[]>([]);
const net = ref<MetricSeries[]>([]);
const range = ref("6h");

const chartsLoading = ref(false);
const pageLoading = ref(false);
const pendingRange = ref<string | null>(null);
const lastUpdated = ref<number | null>(null);
let chartSeq = 0;

async function loadCharts() {
  const wanted = range.value;
  const seq = ++chartSeq;
  chartsLoading.value = true;
  pendingRange.value = wanted;
  try {
    const [cpuM, netM] = await Promise.all([
      api.metrics(props.id, "cpu", wanted).catch(() => ({ series: [] })),
      api.metrics(props.id, "network", wanted).catch(() => ({ series: [] })),
    ]);
    if (seq !== chartSeq) return; // a newer range won
    cpu.value = cpuM.series;
    net.value = netM.series;
  } finally {
    if (seq === chartSeq) {
      chartsLoading.value = false;
      pendingRange.value = null;
    }
  }
}

// Full re-check of this server: summary, topology and both charts.
async function refresh() {
  if (pageLoading.value) return;
  pageLoading.value = true;
  try {
    const [s, t] = await Promise.all([
      api.summary(props.id),
      api.topology(props.id).catch(() => ({ nodes: [], edges: [] })),
    ]);
    summary.value = s;
    topology.value = t;
    await loadCharts();
    lastUpdated.value = Date.now();
  } finally {
    pageLoading.value = false;
  }
}

onMounted(refresh);
watch(range, loadCharts);
</script>

<template>
  <div>
    <PageHeader :title="summary?.server.hostname || 'Server'" subtitle="Server detail">
      <template #actions>
        <div class="head-actions">
          <HealthBadge :status="summary?.health" />
          <div class="ranges" :class="{ busy: chartsLoading }" role="group" aria-label="Chart time range">
            <button
              v-for="r in RANGES"
              :key="r"
              class="range"
              :class="{ on: range === r, pending: pendingRange === r }"
              :disabled="chartsLoading && pendingRange !== r"
              :aria-pressed="range === r"
              @click="range = r"
            >
              <span v-if="pendingRange === r" class="range-spin" aria-hidden="true"></span>
              {{ r }}
            </button>
          </div>
          <RefreshButton
            label="Re-check"
            busy-label="Re-checking…"
            title="Re-run the check for this server: summary, topology and charts"
            :loading="pageLoading"
            :updated-at="lastUpdated"
            @refresh="refresh"
          />
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
      <MetricChart
        :title="`CPU (${range})`"
        :series="cpu"
        unit="%"
        :loading="chartsLoading"
        :loading-label="pendingRange ?? undefined"
      />
      <MetricChart
        :title="`Network RX (${range})`"
        :series="net"
        unit="B/s"
        :loading="chartsLoading"
        :loading-label="pendingRange ?? undefined"
      />
    </div>

    <TopologyGraph :topology="topology" />
  </div>
</template>

<style scoped>
.head-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.ranges {
  display: inline-flex;
  gap: 3px;
  padding: 3px;
  border-radius: 11px;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
}
.ranges.busy {
  border-color: rgba(199, 245, 66, 0.4);
}
.range {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  border: 0;
  background: transparent;
  color: var(--pulse-text-muted);
  padding: 5px 11px;
  border-radius: 8px;
  font-size: 12px;
  font-family: var(--pulse-font-mono);
  cursor: pointer;
  transition: all 0.15s;
}
.range:hover:not(:disabled) {
  color: var(--pulse-text);
}
.range:disabled {
  opacity: 0.45;
  cursor: default;
}
.range.on {
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  font-weight: 700;
}
.range-spin {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  border: 2px solid currentColor;
  border-top-color: transparent;
  animation: sd-spin 0.7s linear infinite;
}
@keyframes sd-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  .range-spin {
    animation-duration: 2.4s;
  }
}
</style>
