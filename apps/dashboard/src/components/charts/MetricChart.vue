<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
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
}>();

const el = ref<HTMLDivElement | null>(null);
let chart: echarts.ECharts | null = null;

const PALETTE = ["#c7f542", "#38bdf8", "#fbbf24", "#f87171", "#a78bfa"];

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

  const legend = props.names && props.names.length > 1;
  const chartSeries = props.series.map((s, idx) => {
    const c = PALETTE[idx % PALETTE.length];
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
  }
});
onBeforeUnmount(() => {
  window.removeEventListener("resize", resize);
  chart?.dispose();
});
watch(() => [props.series, props.min, props.max, props.unit, props.type], render, { deep: true });
</script>

<template>
  <div class="card">
    <div class="card-title">{{ title }}</div>
    <div ref="el" :class="type === 'gauge' ? 'gauge-h' : 'chart-h'"></div>
  </div>
</template>

<style scoped>
.chart-h {
  width: 100%;
  height: 12rem;
}
.gauge-h {
  width: 100%;
  height: 10rem;
}
</style>
