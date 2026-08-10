<script setup lang="ts">
import { ref, watch, computed, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { Resource, MetricSeries } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import StatCard from "@/components/cards/StatCard.vue";
import MetricChart from "@/components/charts/MetricChart.vue";
import { bytes } from "@/lib/format";

const servers = useServersStore();
const { selected } = storeToRefs(servers);
const all = ref<Resource[]>([]);
const showAll = ref(false);
const series = ref<MetricSeries[]>([]);
const now = ref(Date.now());

function num(r: Resource, k: string): number {
  return Number(r.attributes?.[k] ?? 0);
}
function kind(name: string): "physical" | "bridge" | "virtual" | "other" {
  if (/^(en|eth|eno|enp|ens|wl)/.test(name)) return "physical";
  if (name === "docker0" || name.startsWith("br-")) return "bridge";
  if (name.startsWith("veth")) return "virtual";
  return "other";
}

async function load() {
  if (!selected.value) return;
  now.value = Date.now();
  try {
    const snap = await api.discovery(selected.value.id);
    all.value = (snap.resources ?? []).filter((r) => r.type === "network_interface");
  } catch {
    all.value = [];
  }
  try {
    series.value = (await api.metrics(selected.value.id, "network", "1h")).series ?? [];
  } catch {
    series.value = [];
  }
}
onMounted(load);
watch(selected, load);

const rows = computed(() => {
  const list = [...all.value].sort((a, b) => num(b, "rx_bytes") - num(a, "rx_bytes"));
  return showAll.value ? list : list.filter((r) => kind(r.name) !== "virtual");
});
const totalRx = computed(() => all.value.reduce((s, r) => s + num(r, "rx_bytes"), 0));
const totalTx = computed(() => all.value.reduce((s, r) => s + num(r, "tx_bytes"), 0));
const primary = computed(() => {
  const phys = all.value.filter((r) => kind(r.name) === "physical").sort((a, b) => num(b, "rx_bytes") - num(a, "rx_bytes"));
  return phys[0]?.name ?? "—";
});
const virtualCount = computed(() => all.value.filter((r) => kind(r.name) === "virtual").length);
</script>

<template>
  <div>
    <PageHeader title="Network" subtitle="Interfaces, throughput and traffic counters — read from /proc, read-only." />
    <EmptyState v-if="all.length === 0" title="No interfaces discovered" />
    <template v-else>
      <div class="stats">
        <StatCard label="Primary interface" :value="primary" />
        <StatCard label="Total received" :value="bytes(totalRx)" />
        <StatCard label="Total sent" :value="bytes(totalTx)" />
        <StatCard label="Interfaces" :value="all.length" :suffix="`(${virtualCount} virtual)`" />
      </div>

      <div class="chart">
        <MetricChart title="Receive throughput (1h)" :series="series" unit="B/s" :min="now - 3600000" :max="now" />
      </div>

      <div class="thead">
        <h3 class="th-title">Interfaces</h3>
        <label class="toggle">
          <input type="checkbox" v-model="showAll" />
          Show virtual (veth) interfaces
        </label>
      </div>

      <div class="card overflow-x-auto">
        <table class="table">
          <thead>
            <tr><th>Interface</th><th>Type</th><th>State</th><th>RX</th><th>TX</th><th>RX errors</th><th>TX errors</th></tr>
          </thead>
          <tbody>
            <tr v-for="r in rows" :key="r.id">
              <td class="font-medium">{{ r.name }}</td>
              <td><span class="kind" :class="kind(r.name)">{{ kind(r.name) }}</span></td>
              <td class="text-muted">{{ r.attributes?.operstate ?? "—" }}</td>
              <td class="font-mono text-xs">{{ bytes(num(r, "rx_bytes")) }}</td>
              <td class="font-mono text-xs">{{ bytes(num(r, "tx_bytes")) }}</td>
              <td class="font-mono text-xs" :class="{ 'text-degraded': num(r, 'rx_errors') > 0 }">{{ num(r, "rx_errors") }}</td>
              <td class="font-mono text-xs" :class="{ 'text-degraded': num(r, 'tx_errors') > 0 }">{{ num(r, "tx_errors") }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>

<style scoped>
.stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 14px;
}
.chart {
  margin-bottom: 18px;
}
.thead {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}
.th-title {
  font-family: var(--pulse-font-display);
  font-size: 15px;
  margin: 0;
}
.toggle {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  color: var(--pulse-text-muted);
  cursor: pointer;
}
.kind {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  padding: 2px 8px;
  border-radius: 999px;
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text-muted);
}
.kind.physical {
  color: var(--pulse-accent);
  border-color: rgba(199, 245, 66, 0.3);
  background: rgba(199, 245, 66, 0.08);
}
.kind.bridge {
  color: #7ec8f2;
  border-color: rgba(41, 169, 235, 0.3);
}
@media (max-width: 900px) {
  .stats {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
