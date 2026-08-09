<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
import * as echarts from "echarts";
import type { Topology } from "@/api/types";

const props = defineProps<{ topology: Topology | null }>();
const el = ref<HTMLDivElement | null>(null);
let chart: echarts.ECharts | null = null;

const healthColor: Record<string, string> = {
  HEALTHY: "#34d399",
  DEGRADED: "#fbbf24",
  DOWN: "#f87171",
  UNKNOWN: "#6b7280",
};

function render() {
  if (!chart || !props.topology) return;
  const nodes = props.topology.nodes.map((n) => ({
    id: n.id,
    name: n.label,
    symbolSize: n.type === "internet" ? 34 : n.type === "reverse_proxy" ? 30 : 24,
    itemStyle: { color: healthColor[n.health ?? "UNKNOWN"] },
    category: n.type,
  }));
  const links = props.topology.edges.map((e) => ({ source: e.from, target: e.to }));
  chart.setOption({
    tooltip: {},
    series: [
      {
        type: "graph",
        layout: "force",
        roam: true,
        draggable: true,
        label: { show: true, color: "#e6ebf2", position: "right" },
        force: { repulsion: 220, edgeLength: 90 },
        lineStyle: { color: "#3a4457", curveness: 0.1 },
        data: nodes,
        links,
      },
    ],
  });
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
watch(() => props.topology, render, { deep: true });
</script>

<template>
  <div class="card">
    <div class="card-title">Service topology</div>
    <div class="relative">
      <div ref="el" class="w-full h-80"></div>
      <div
        v-if="!topology || topology.nodes.length === 0"
        class="absolute inset-0 flex items-center justify-center text-sm text-muted text-center px-6"
      >
        No topology discovered yet. Relationships are generated only from real
        discovery data — never invented.
      </div>
    </div>
  </div>
</template>
