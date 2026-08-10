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

interface Panel {
  id: string;
  title: string;
  type: "line" | "area" | "bar";
  metrics: string[];
  unit?: string;
  names?: string[];
}

const GAUGES = [
  { id: "cpu", title: "CPU" },
  { id: "memory", title: "Memory" },
  { id: "disk", title: "Disk" },
];
const DEFAULT_PANELS: Panel[] = [
  { id: "cpu", title: "CPU usage", type: "area", metrics: ["cpu"], unit: "%" },
  { id: "cpu_detail", title: "CPU — user / system / iowait", type: "line", metrics: ["cpu_user", "cpu_system", "cpu_iowait"], unit: "%", names: ["user", "system", "iowait"] },
  { id: "load", title: "Load average — 1 / 5 / 15m", type: "line", metrics: ["load", "load5", "load15"], names: ["1m", "5m", "15m"] },
  { id: "memory", title: "Memory used", type: "area", metrics: ["memory"], unit: "%" },
  { id: "swap", title: "Swap used", type: "line", metrics: ["swap"], unit: "%" },
  { id: "disk_io", title: "Disk I/O — read / write", type: "area", metrics: ["disk_read", "disk_write"], unit: "B/s", names: ["read", "write"] },
  { id: "network", title: "Bandwidth — in / out", type: "area", metrics: ["net_in", "net_out"], unit: "B/s", names: ["in", "out"] },
];

const ranges = ["1h", "6h", "24h", "7d", "30d"];
const RANGE_MS: Record<string, number> = { "1h": 3600e3, "6h": 6 * 3600e3, "24h": 24 * 3600e3, "7d": 7 * 24 * 3600e3, "30d": 30 * 24 * 3600e3 };
const range = ref("1h");
const data = ref<Record<string, MetricSeries[]>>({});
const now = ref(Date.now());
const dragging = ref<string | null>(null);
const order = ref<string[]>(loadOrder());

const allMetrics = [...new Set([...GAUGES.map((g) => g.id), ...DEFAULT_PANELS.flatMap((p) => p.metrics)])];

function loadOrder(): string[] {
  const ids = DEFAULT_PANELS.map((p) => p.id);
  try {
    const saved = JSON.parse(localStorage.getItem("pulse-metric-panels") || "null");
    if (Array.isArray(saved)) {
      const known = saved.filter((x: string) => ids.includes(x));
      return [...known, ...ids.filter((id) => !known.includes(id))];
    }
  } catch {
    /* ignore */
  }
  return ids;
}
function saveOrder() {
  try {
    localStorage.setItem("pulse-metric-panels", JSON.stringify(order.value));
  } catch {
    /* ignore */
  }
}

const panelsOrdered = computed(() => order.value.map((id) => DEFAULT_PANELS.find((p) => p.id === id)).filter((p): p is Panel => !!p));
const windowStart = computed(() => now.value - RANGE_MS[range.value]);
const windowEnd = computed(() => now.value);

async function load() {
  if (!selected.value) return;
  now.value = Date.now();
  const id = selected.value.id;
  const results = await Promise.all(
    allMetrics.map(async (m) => {
      const resp = await api.metrics(id, m, range.value).catch(() => ({ series: [] as MetricSeries[] }));
      return [m, resp.series ?? []] as const;
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

function seriesFor(p: Panel): MetricSeries[] {
  return p.metrics.map((m) => (data.value[m] ?? [])[0]).filter((s): s is MetricSeries => !!s);
}
function gaugeSeries(metric: string): MetricSeries[] {
  const s = (data.value[metric] ?? [])[0];
  return s ? [s] : [{ name: metric, points: [] }];
}
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
      subtitle="Live host metrics — CPU (incl. user/system/iowait), load, memory, swap, disk usage + I/O, and bandwidth. Drag panels to reorder."
    />
    <EmptyState v-if="!selected" title="No server selected" />
    <template v-else>
      <div class="ranges">
        <button v-for="r in ranges" :key="r" class="range" :class="{ on: range === r }" @click="range = r">{{ r }}</button>
      </div>

      <div class="gauges">
        <div v-for="g in GAUGES" :key="g.id" class="gauge-card">
          <MetricChart :title="`${g.title} (now)`" :series="gaugeSeries(g.id)" type="gauge" unit="%" />
        </div>
      </div>

      <div class="grid">
        <div
          v-for="(p, i) in panelsOrdered"
          :key="p.id"
          class="panel"
          :class="{ dragging: dragging === p.id, wide: i === 0 }"
          draggable="true"
          @dragstart="dragging = p.id"
          @dragend="dragging = null"
          @dragover.prevent
          @drop="onDrop(p.id)"
        >
          <span class="grip" aria-hidden="true">⠿</span>
          <MetricChart
            :title="p.title"
            :series="seriesFor(p)"
            :type="p.type"
            :unit="p.unit"
            :names="p.names"
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
.gauges {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 14px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
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
.panel.wide {
  grid-column: 1 / -1;
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
@media (max-width: 900px) {
  .gauges {
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }
  .grid {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 560px) {
  .gauges {
    grid-template-columns: 1fr;
  }
}
</style>
