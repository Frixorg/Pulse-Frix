<script setup lang="ts">
import { ref, onMounted } from "vue";
import { api } from "@/api/client";
import type { AlertInstance } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";
import { timeAgo } from "@/lib/format";

const rows = ref<AlertInstance[]>([]);
const loading = ref(false);

onMounted(async () => {
  loading.value = true;
  try {
    rows.value = (await api.alertInstances()).data ?? [];
  } catch {
    rows.value = [];
  } finally {
    loading.value = false;
  }
});

function sev(s: string) {
  return s === "CRITICAL" ? "DOWN" : s === "WARNING" ? "DEGRADED" : "HEALTHY";
}
</script>

<template>
  <div>
    <PageHeader title="Alerts" subtitle="Debounced, deduplicated, dependency-aware. Root causes are surfaced over symptoms." />
    <EmptyState v-if="!loading && rows.length === 0" title="No active alerts"
      message="When thresholds are breached you'll see grouped, correlated alerts here — with recovery notifications." />
    <div v-else class="space-y-2">
      <div v-for="a in rows" :key="a.id" class="card flex items-start justify-between">
        <div>
          <div class="flex items-center gap-2">
            <HealthBadge :status="sev(a.severity)" />
            <span class="font-medium">{{ a.name }}</span>
            <span class="text-xs text-muted">{{ a.state }}</span>
          </div>
          <p v-if="a.root_cause" class="text-sm text-muted mt-1">
            Root cause: {{ a.root_cause }}
            <span v-if="a.affected?.length"> — affected: {{ a.affected.join(", ") }}</span>
          </p>
        </div>
        <span class="text-xs text-muted">{{ timeAgo(a.started_at) }}</span>
      </div>
    </div>
  </div>
</template>
