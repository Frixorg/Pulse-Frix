<script setup lang="ts">
import { ref, watch, computed, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { ServerSummary, EventItem, MetricSeries } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import StatCard from "@/components/cards/StatCard.vue";
import MetricChart from "@/components/charts/MetricChart.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";
import OnboardingConnect from "@/components/OnboardingConnect.vue";
import { bytes, uptime, tone, timeAgo } from "@/lib/format";

const servers = useServersStore();
const { selected } = storeToRefs(servers);

const summary = ref<ServerSummary | null>(null);
const events = ref<EventItem[]>([]);
const cpu = ref<MetricSeries[]>([]);
const mem = ref<MetricSeries[]>([]);
const loading = ref(false);

const c = computed(() => summary.value?.counts);

async function load() {
  if (!selected.value) return;
  loading.value = true;
  const id = selected.value.id;
  try {
    const [s, ev, cpuM, memM] = await Promise.all([
      api.summary(id),
      api.events(8),
      api.metrics(id, "cpu", "1h").catch(() => ({ series: [] })),
      api.metrics(id, "memory", "1h").catch(() => ({ series: [] })),
    ]);
    summary.value = s;
    events.value = ev.data;
    cpu.value = cpuM.series;
    mem.value = memM.series;
  } finally {
    loading.value = false;
  }
}

onMounted(load);
watch(selected, load);
</script>

<template>
  <div>
    <OnboardingConnect v-if="!selected" />

    <template v-else>
      <PageHeader title="Dashboard" subtitle="Is my infrastructure healthy?" />
      <div class="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-6 gap-3 mb-3">
        <div class="card">
          <div class="card-title">VPS Health</div>
          <HealthBadge :status="summary?.health" />
        </div>
        <StatCard label="CPU" :value="(summary?.cpu_percent ?? 0).toFixed(0)" suffix="%"
          :percent="summary?.cpu_percent ?? 0" :tone="tone(summary?.cpu_percent ?? 0)" />
        <StatCard label="RAM" :value="summary?.mem_used_pct ?? 0" suffix="%"
          :percent="summary?.mem_used_pct ?? 0" :tone="tone(summary?.mem_used_pct ?? 0)" />
        <StatCard label="Disk" :value="summary?.disk_used_pct ?? 0" suffix="%"
          :percent="summary?.disk_used_pct ?? 0" :tone="tone(summary?.disk_used_pct ?? 0)" />
        <StatCard label="Network RX" :value="bytes(summary?.net_rx_bytes ?? 0)" />
        <StatCard label="Uptime" :value="uptime(summary?.uptime_sec ?? 0)" />
      </div>

      <div class="grid grid-cols-1 md:grid-cols-4 gap-3 mb-3">
        <div class="card">
          <div class="card-title">Services</div>
          <div class="text-sm space-y-0.5">
            <div><span class="text-healthy font-medium">{{ c?.services_healthy ?? 0 }}</span> healthy</div>
            <div><span class="text-degraded font-medium">{{ c?.services_degraded ?? 0 }}</span> degraded</div>
            <div><span class="text-down font-medium">{{ c?.services_down ?? 0 }}</span> down</div>
          </div>
        </div>
        <div class="card">
          <div class="card-title">Containers</div>
          <div class="text-sm space-y-0.5">
            <div><span class="text-healthy font-medium">{{ c?.containers_running ?? 0 }}</span> running</div>
            <div><span class="text-down font-medium">{{ c?.containers_unhealthy ?? 0 }}</span> unhealthy</div>
          </div>
        </div>
        <div class="card">
          <div class="card-title">Domains</div>
          <div class="text-sm space-y-0.5">
            <div><span class="text-healthy font-medium">{{ c?.domains_online ?? 0 }}</span> online</div>
            <div><span class="text-degraded font-medium">{{ c?.domains_ssl_expiring ?? 0 }}</span> SSL expiring</div>
          </div>
        </div>
        <div class="card">
          <div class="card-title">Alerts</div>
          <div class="text-sm space-y-0.5">
            <div><span class="text-down font-medium">{{ c?.alerts_critical ?? 0 }}</span> critical</div>
            <div><span class="text-degraded font-medium">{{ c?.alerts_warning ?? 0 }}</span> warning</div>
          </div>
        </div>
      </div>

      <div class="grid grid-cols-1 lg:grid-cols-2 gap-3 mb-3">
        <MetricChart title="CPU (1h)" :series="cpu" />
        <MetricChart title="Memory (1h)" :series="mem" />
      </div>

      <div class="card">
        <div class="card-title">Recent events</div>
        <div v-if="events.length === 0" class="text-sm text-muted py-4">No recent events.</div>
        <table v-else class="table">
          <tbody>
            <tr v-for="e in events" :key="e.id">
              <td class="w-24 text-muted">{{ timeAgo(e.ts) }}</td>
              <td class="w-24">
                <HealthBadge :status="e.severity === 'CRITICAL' ? 'DOWN' : e.severity === 'WARNING' ? 'DEGRADED' : 'HEALTHY'" />
              </td>
              <td class="text-muted">{{ e.source }}</td>
              <td>{{ e.event }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>
