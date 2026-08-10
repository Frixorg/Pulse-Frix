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
}>();

const el = ref<HTMLDivElement | null>(null);
let chart: echarts.ECharts | null = null;

function render() {
  if (!chart) return;
  const color = props.color ?? getCss("--pulse-accent");
  const unit = props.unit ?? "";
  chart.setOption({
    grid: { left: 48, right: 14, top: 20, bottom: 24 },
    tooltip: {
      trigger: "axis",
      backgroundColor: getCss("--pulse-solid") || "#0d1017",
      borderColor: getCss("--pulse-border"),
      textStyle: { color: getCss("--pulse-text") },
    },
    xAxis: {
      type: "time",
      min: props.min,
      max: props.max,
      axisLine: { lineStyle: { color: getCss("--pulse-border") } },
      axisLabel: { color: getCss("--pulse-text-muted"), hideOverlap: true },
    },
    yAxis: {
      type: "value",
      splitLine: { lineStyle: { color: getCss("--pulse-border"), opacity: 0.4 } },
      axisLabel: {
        color: getCss("--pulse-text-muted"),
        formatter: unit === "%" ? "{value}%" : "{value}",
      },
    },
    series: props.series.map((s) => ({
      name: s.name,
      type: "line",
      showSymbol: false,
      smooth: true,
      lineStyle: { width: 2, color },
      areaStyle: { color, opacity: 0.08 },
      data: s.points.map((p) => [p.t * 1000, Number(p.v.toFixed(2))]),
    })),
  });
}

function getCss(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || "#4f8cff";
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
watch(() => [props.series, props.min, props.max, props.unit], render, { deep: true });
</script>

<template>
  <div class="card">
    <div class="card-title">{{ title }}</div>
    <div ref="el" class="w-full h-48"></div>
  </div>
</template>
