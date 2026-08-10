<script setup lang="ts">
import { ref, watch, computed, onMounted, onBeforeUnmount } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { MetricSeries } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import MetricChart from "@/components/charts/MetricChart.vue";
import EmptyState from "@/components/EmptyState.vue";

const servers = useServersStore();
const { selected } = storeToRefs(servers);

const ALL = ["cpu", "memory", "disk", "network", "load"];
const LABELS: Record<string, string> = { cpu: "CPU", memory: "Memory", disk: "Disk", network: "Network", load: "Load average" };
const ranges = ["1h", "6h", "24h", "7d", "30d"];
const RANGE_MS: Record<string, number> = {
  "1h": 3600e3,
  "6h": 6 * 3600e3,
  "24h": 24 * 3600e3,
  "7d": 7 * 24 * 3600e3,
  "30d": 30 * 24 * 3600e3,
};

const range = ref("1h");
const order = ref<string[]>(loadOrder());
const data = ref<Record<string, { series: MetricSeries[]; degraded: boolean }>>({});
const now = ref(Date.now());
const dragging = ref<string | null>(null);

function loadOrder(): string[] {
  try {
    const saved = JSON.parse(localStorage.getItem("pulse-metric-order") || "null");
    if (Array.isArray(saved)) {
      const known = saved.filter((m: string) => ALL.includes(m));
      return [...known, ...ALL.filter((m) => !known.includes(m))];
    }
  } catch {
    /* ignore */
  }
  return [...ALL];
}
function saveOrder() {
  try {
    localStorage.setItem("pulse-metric-order", JSON.stringify(order.value));
  } catch {
    /* ignore */
  }
}

const windowStart = computed(() => now.value - RANGE_MS[range.value]);
const windowEnd = computed(() => now.value);

function unitFor(m: string) {
  if (m === "network") return "B/s";
  if (m === "load") return "";
  return "%";
}

async function load() {
  if (!selected.value) return;
  now.value = Date.now();
  const id = selected.value.id;
  const results = await Promise.all(
    ALL.map(async (m) => {
      const resp = await api.metrics(id, m, range.value).catch(() => ({ series: [], degraded: true }));
      return [m, { series: resp.series ?? [], degraded: !!resp.degraded }] as const;
    }),
  );
  data.value = Object.fromEntries(results);
}
onMounted(load);
watch([selected, range], load);

let timer: number | undefined;
onMounted(() => {
  timer = window.setInterval(load, 15000);
});
onBeforeUnmount(() => {
  if (timer) clearInterval(timer);
});

function onDrop(target: string) {
  const from = dragging.value;
  dragging.value = null;
  if (!from || from === target) return;
  const arr = [...order.value];
  arr.splice(arr.indexOf(from), 1);
  arr.splice(arr.indexOf(target), 0, from);
  order.value = arr;
  saveOrder();
}
</script>

<template>
  <div>
    <PageHeader
      title="Metrics"
      subtitle="Live host metrics — CPU, memory, disk, network and load, all at once. Drag a panel to reorder; history fills in as the agent reports."
    />
    <EmptyState v-if="!selected" title="No server selected" />
    <template v-else>
      <div class="ranges">
        <button v-for="r in ranges" :key="r" class="range" :class="{ on: range === r }" @click="range = r">{{ r }}</button>
      </div>

      <div class="grid">
        <div
          v-for="(m, i) in order"
          :key="m"
          class="panel"
          :class="{ dragging: dragging === m, wide: i === 0 }"
          draggable="true"
          @dragstart="dragging = m"
          @dragend="dragging = null"
          @dragover.prevent
          @drop="onDrop(m)"
        >
          <span v-if="data[m]?.degraded" class="deg">latest sample</span>
          <span class="grip" aria-hidden="true">⠿</span>
          <MetricChart
            :title="LABELS[m]"
            :series="data[m]?.series ?? []"
            :unit="unitFor(m)"
            :min="windowStart"
            :max="windowEnd"
          />
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.ranges {
  display: inline-flex;
  gap: 4px;
  padding: 4px;
  border-radius: 12px;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
  margin-bottom: 16px;
}
.range {
  border: 0;
  background: transparent;
  color: var(--pulse-text-muted);
  padding: 6px 14px;
  border-radius: 9px;
  font-size: 13px;
  font-family: var(--pulse-font-mono);
  cursor: pointer;
  transition: all 0.15s;
}
.range:hover {
  color: var(--pulse-text);
}
.range.on {
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  font-weight: 700;
}
.grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.panel.wide {
  grid-column: 1 / -1;
}
@media (max-width: 900px) {
  .grid {
    grid-template-columns: 1fr;
  }
}
.panel {
  position: relative;
  cursor: grab;
  border-radius: 14px;
  transition: transform 0.12s, opacity 0.12s;
}
.panel:active {
  cursor: grabbing;
}
.panel.dragging {
  opacity: 0.5;
  transform: scale(0.99);
}
.grip {
  position: absolute;
  top: 15px;
  right: 14px;
  z-index: 3;
  color: var(--pulse-text-muted);
  opacity: 0.4;
  font-size: 13px;
  pointer-events: none;
}
.deg {
  position: absolute;
  top: 16px;
  right: 36px;
  z-index: 3;
  font-size: 10px;
  color: var(--pulse-degraded);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  pointer-events: none;
}
</style>
