<script setup lang="ts">
import { ref, watch, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { MetricSeries } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import MetricChart from "@/components/charts/MetricChart.vue";
import EmptyState from "@/components/EmptyState.vue";

const servers = useServersStore();
const { selected } = storeToRefs(servers);

const metric = ref("cpu");
const range = ref("6h");
const series = ref<MetricSeries[]>([]);
const degraded = ref(false);
const metrics = ["cpu", "memory", "disk", "network", "load"];

async function load() {
  if (!selected.value) return;
  const resp = await api.metrics(selected.value.id, metric.value, range.value).catch(() => ({ series: [], degraded: true }));
  series.value = resp.series;
  degraded.value = !!resp.degraded;
}
onMounted(load);
watch([selected, metric, range], load);
</script>

<template>
  <div>
    <PageHeader title="Metrics" subtitle="The dashboard never speaks PromQL — the API abstracts the metrics backend." />
    <EmptyState v-if="!selected" title="No server selected" />
    <template v-else>
      <div class="flex gap-2 mb-3">
        <select v-model="metric" class="bg-surface-2 border border-border rounded-md text-sm px-2 py-1">
          <option v-for="m in metrics" :key="m" :value="m">{{ m }}</option>
        </select>
        <select v-model="range" class="bg-surface-2 border border-border rounded-md text-sm px-2 py-1">
          <option>1h</option><option>6h</option><option>24h</option><option>7d</option><option>30d</option>
        </select>
      </div>
      <p v-if="degraded" class="text-xs text-degraded mb-2">
        Metrics backend unavailable — showing the latest stored sample. Only real data is ever shown.
      </p>
      <MetricChart :title="`${metric} (${range})`" :series="series" />
    </template>
  </div>
</template>
