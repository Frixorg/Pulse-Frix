<script setup lang="ts">
import { ref, watch, computed, onMounted, onBeforeUnmount } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { MetricSeries } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import MetricChart from "@/components/charts/MetricChart.vue";
import EmptyState from "@/components/EmptyState.vue";
import RefreshButton from "@/components/RefreshButton.vue";

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

// `loading` drives the per-chart overlays. It is only set for user-initiated
// loads (mount, server switch, range change, manual refresh) — the 15s silent
// poll must never flash the UI. `pendingRange` is the range being fetched, so
// the range switcher can show which button you are waiting on.
const loading = ref(false);
const pendingRange = ref<string | null>(null);
const lastUpdated = ref<number | null>(null);
const loadError = ref("");
let reqSeq = 0;

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

async function load(silent = false) {
  if (!selected.value) return;
  const id = selected.value.id;
  const wanted = range.value;
  const seq = ++reqSeq;

  if (!silent) {
    loading.value = true;
    pendingRange.value = wanted;
    loadError.value = "";
  }
  try {
    const results = await Promise.all(
      allMetrics.map(async (m) => {
        const resp = await api.metrics(id, m, wanted).catch(() => ({ series: [] as MetricSeries[] }));
        return [m, resp.series ?? []] as const;
      }),
    );
    // A slower earlier request must not overwrite a newer one (rapid range
    // clicking would otherwise land you on the wrong data).
    if (seq !== reqSeq) return;
    now.value = Date.now();
    data.value = Object.fromEntries(results);
    lastUpdated.value = Date.now();
  } catch (e) {
    if (seq === reqSeq && !silent) loadError.value = e instanceof Error ? e.message : "failed to load metrics";
  } finally {
    if (seq === reqSeq && !silent) {
      loading.value = false;
      pendingRange.value = null;
    }
  }
}

function setRange(r: string) {
  if (r === range.value || loading.value) return;
  range.value = r;
}

onMounted(() => load());
watch([selected, range], () => load());
let timer: number | undefined;
onMounted(() => {
  // Background poll: refreshes numbers without ever showing a loading state.
  timer = window.setInterval(() => load(true), 15000);
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
    >
      <template #actions>
        <RefreshButton :loading="loading" :updated-at="lastUpdated" :disabled="!selected" @refresh="load()" />
      </template>
    </PageHeader>
    <EmptyState v-if="!selected" title="No server selected" />
    <template v-else>
      <div class="range-row">
        <div class="ranges" :class="{ busy: loading }" role="group" aria-label="Time range">
          <button
            v-for="r in ranges"
            :key="r"
            class="range"
            :class="{ on: range === r, pending: pendingRange === r }"
            :disabled="loading && pendingRange !== r"
            :aria-pressed="range === r"
            @click="setRange(r)"
          >
            <span v-if="pendingRange === r" class="range-spin" aria-hidden="true"></span>
            {{ r }}
          </button>
        </div>
        <span v-if="loading" class="range-note">Loading {{ pendingRange }} of history…</span>
        <span v-else-if="loadError" class="range-err">{{ loadError }}</span>
      </div>

      <div class="gauges">
        <div v-for="g in GAUGES" :key="g.id" class="gauge-card">
          <MetricChart
            :title="`${g.title} (now)`"
            :series="gaugeSeries(g.id)"
            type="gauge"
            unit="%"
            :loading="loading"
            :loading-label="pendingRange ?? undefined"
          />
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
            :loading="loading"
            :loading-label="pendingRange ?? undefined"
          />
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.range-row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}
.ranges {
  display: inline-flex;
  gap: 4px;
  padding: 4px;
  border-radius: 12px;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
}
.ranges.busy {
  border-color: rgba(199, 245, 66, 0.4);
}
.range {
  display: inline-flex;
  align-items: center;
  gap: 6px;
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
/* The range you just clicked keeps its own spinner, so it is obvious which
   window is being fetched — not just that "something" is loading. */
.range.pending {
  opacity: 1;
}
.range-spin {
  width: 11px;
  height: 11px;
  border-radius: 50%;
  border: 2px solid currentColor;
  border-top-color: transparent;
  opacity: 0.85;
  animation: range-spin 0.7s linear infinite;
}
@keyframes range-spin {
  to {
    transform: rotate(360deg);
  }
}
.range-note {
  font-size: 12px;
  font-family: var(--pulse-font-mono);
  color: var(--pulse-accent);
}
.range-err {
  font-size: 12px;
  font-family: var(--pulse-font-mono);
  color: var(--pulse-down);
}
@media (prefers-reduced-motion: reduce) {
  .range-spin {
    animation-duration: 2.4s;
  }
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
