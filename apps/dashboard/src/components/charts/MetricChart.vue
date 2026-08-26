<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch, computed } from "vue";
import * as echarts from "echarts";
import type { MetricSeries } from "@/api/types";

const props = defineProps<{
  title: string;
  series: MetricSeries[];
  unit?: string;
  color?: string;
  min?: number; // window start (ms)
  max?: number; // window end (ms)
  type?: "line" | "area" | "bar" | "gauge";
  names?: string[]; // legend names for multi-series
  loading?: boolean; // a fetch is in flight — show it instead of pretending
  loadingLabel?: string; // e.g. the range being fetched ("6h")
}>();

const el = ref<HTMLDivElement | null>(null);
let chart: echarts.ECharts | null = null;

// Bright on dark; deeper + saturated on light so lines don't wash out on a
// white surface. Same hue order (lime, blue, amber, red, violet) in both.
const DARK_PALETTE = ["#c7f542", "#38bdf8", "#fbbf24", "#f87171", "#a78bfa"];
const LIGHT_PALETTE = ["#4d7c0f", "#0369a1", "#b45309", "#b91c1c", "#6d28d9"];
function isLight() {
  return document.documentElement.classList.contains("light");
}
function palette() {
  return isLight() ? LIGHT_PALETTE : DARK_PALETTE;
}

function css(name: string, fallback = "") {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
}
function fmtBytes(v: number): string {
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let n = Math.abs(v);
  while (n >= 1024 && i < u.length - 1) {
    n /= 1024;
    i++;
  }
  return (n < 10 && i > 0 ? n.toFixed(1) : Math.round(n).toString()) + u[i];
}
const isBytes = () => props.unit === "B/s" || props.unit === "bytes";
function axisFmt(v: number) {
  if (isBytes()) return fmtBytes(v) + (props.unit === "B/s" ? "/s" : "");
  if (props.unit === "%") return v + "%";
  return String(v);
}

function render() {
  if (!chart) return;
  const muted = css("--pulse-text-muted", "#8b97a9");
  const border = css("--pulse-border", "rgba(255,255,255,0.1)");
  const accent = props.color || css("--pulse-accent", "#c7f542");

  if (props.type === "gauge") {
    const s = props.series[0];
    const val = s && s.points.length ? s.points[s.points.length - 1].v : 0;
    const color = val >= 90 ? css("--pulse-down", "#f87171") : val >= 75 ? css("--pulse-degraded", "#fbbf24") : accent;
    chart.setOption({
      series: [
        {
          type: "gauge",
          radius: "92%",
          startAngle: 210,
          endAngle: -30,
          min: 0,
          max: 100,
          progress: { show: true, width: 10, itemStyle: { color } },
          axisLine: { lineStyle: { width: 10, color: [[1, css("--pulse-surface-2", "rgba(255,255,255,0.07)")]] } },
          axisTick: { show: false },
          splitLine: { show: false },
          axisLabel: { show: false },
          pointer: { show: false },
          anchor: { show: false },
          title: { show: false },
          detail: {
            valueAnimation: true,
            offsetCenter: [0, 0],
            fontSize: 26,
            fontWeight: 700,
            fontFamily: "Space Grotesk, sans-serif",
            color: css("--pulse-text", "#eaf0f7"),
            formatter: (v: number) => `${Math.round(v)}%`,
          },
          data: [{ value: Math.round(val) }],
        },
      ],
    });
    return;
  }

  const pal = palette();
  const legend = props.names && props.names.length > 1;
  const chartSeries = props.series.map((s, idx) => {
    const c = props.color && props.series.length === 1 ? props.color : pal[idx % pal.length];
    const base: Record<string, unknown> = {
      name: props.names?.[idx] ?? s.name,
      type: props.type === "bar" ? "bar" : "line",
      showSymbol: false,
      smooth: props.type !== "bar",
      lineStyle: { width: 2, color: c },
      itemStyle: { color: c },
      data: s.points.map((p) => [p.t * 1000, Number(p.v.toFixed(2))]),
    };
    if (props.type === "area" || props.type === undefined || props.type === "line") {
      if (props.type === "area" || props.series.length === 1) {
        base.areaStyle = { color: c, opacity: 0.1 };
      }
    }
    return base;
  });

  chart.setOption(
    {
      grid: { left: 52, right: 14, top: legend ? 30 : 16, bottom: 24 },
      legend: legend
        ? { top: 2, right: 4, textStyle: { color: muted, fontSize: 11 }, itemWidth: 12, itemHeight: 8 }
        : undefined,
      tooltip: {
        trigger: "axis",
        backgroundColor: css("--pulse-solid", "#0d1017"),
        borderColor: border,
        textStyle: { color: css("--pulse-text", "#eaf0f7"), fontSize: 12 },
        valueFormatter: (v: number) => axisFmt(Number(v)),
      },
      xAxis: {
        type: "time",
        min: props.min,
        max: props.max,
        axisLine: { lineStyle: { color: border } },
        axisLabel: { color: muted, hideOverlap: true },
      },
      yAxis: {
        type: "value",
        splitLine: { lineStyle: { color: border, opacity: 0.4 } },
        axisLabel: { color: muted, formatter: axisFmt },
      },
      series: chartSeries,
    },
    { notMerge: true },
  );
}

function resize() {
  chart?.resize();
}
onMounted(() => {
  if (el.value) {
    chart = echarts.init(el.value);
    render();
    window.addEventListener("resize", resize);
    window.addEventListener("pulse-theme", render);
  }
});
onBeforeUnmount(() => {
  window.removeEventListener("resize", resize);
  window.removeEventListener("pulse-theme", render);
  chart?.dispose();
});
watch(() => [props.series, props.min, props.max, props.unit, props.type], render, { deep: true });

// True only when we have finished loading and genuinely have nothing to draw —
// so an empty chart is never mistaken for stale data (and vice versa).
const isEmpty = computed(() => !props.series.some((s) => s.points.length > 0));
</script>

<template>
  <div class="card">
    <div class="card-head">
      <div class="card-title mb-0">{{ title }}</div>
      <span v-if="loading" class="live-chip">
        <span class="live-dot"></span>
        {{ loadingLabel ? `Loading ${loadingLabel}…` : "Loading…" }}
      </span>
    </div>

    <div class="chart-wrap" :class="type === 'gauge' ? 'gauge-h' : 'chart-h'">
      <!-- The canvas stays mounted (echarts owns it) and is dimmed while a
           fetch is in flight, so stale numbers are never shown as if fresh. -->
      <div ref="el" class="canvas" :class="{ stale: loading }"></div>

      <div v-if="loading" class="overlay" role="status" aria-live="polite">
        <div class="shimmer"></div>
        <div class="spinner" aria-hidden="true"></div>
        <span class="overlay-text">
          {{ loadingLabel ? `Fetching ${loadingLabel} of data…` : "Fetching data…" }}
        </span>
      </div>

      <div v-else-if="isEmpty" class="overlay empty">
        <span class="overlay-text">No data in this range</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 8px;
  min-height: 18px;
}
.live-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 10.5px;
  font-family: var(--pulse-font-mono);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--pulse-accent);
  white-space: nowrap;
}
.live-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--pulse-accent);
  animation: mc-blink 1s ease-in-out infinite;
}
.chart-wrap {
  position: relative;
  width: 100%;
}
.canvas {
  width: 100%;
  height: 100%;
  transition: opacity 0.18s ease, filter 0.18s ease;
}
.canvas.stale {
  opacity: 0.22;
  filter: saturate(0.4) blur(1px);
  pointer-events: none;
}
.chart-h {
  height: 12rem;
}
.gauge-h {
  height: 10rem;
}
.overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  border-radius: 10px;
  overflow: hidden;
  pointer-events: none;
}
.overlay.empty .overlay-text {
  color: var(--pulse-text-muted);
}
.overlay-text {
  position: relative;
  font-size: 11.5px;
  font-family: var(--pulse-font-mono);
  color: var(--pulse-text-muted);
  letter-spacing: 0.02em;
}
.spinner {
  position: relative;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 2px solid var(--pulse-border);
  border-top-color: var(--pulse-accent);
  animation: mc-spin 0.75s linear infinite;
}
/* A sweeping highlight makes "work is happening" obvious at a glance. */
.shimmer {
  position: absolute;
  inset: 0;
  background: linear-gradient(
    100deg,
    transparent 20%,
    rgba(199, 245, 66, 0.07) 45%,
    rgba(199, 245, 66, 0.12) 50%,
    rgba(199, 245, 66, 0.07) 55%,
    transparent 80%
  );
  background-size: 250% 100%;
  animation: mc-sweep 1.25s ease-in-out infinite;
}
@keyframes mc-spin {
  to {
    transform: rotate(360deg);
  }
}
@keyframes mc-sweep {
  0% {
    background-position: 140% 0;
  }
  100% {
    background-position: -40% 0;
  }
}
@keyframes mc-blink {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.25;
  }
}
@media (prefers-reduced-motion: reduce) {
  .shimmer,
  .live-dot {
    animation: none;
  }
  .spinner {
    animation-duration: 2.4s;
  }
}
</style>
