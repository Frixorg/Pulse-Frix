<script setup lang="ts">
import { ref, watch, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { SecurityFinding } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";

const servers = useServersStore();
const { selected } = storeToRefs(servers);
const rows = ref<SecurityFinding[]>([]);
const loading = ref(false);

async function load() {
  if (!selected.value) return;
  loading.value = true;
  try {
    rows.value = (await api.security(selected.value.id)).data ?? [];
  } catch {
    rows.value = [];
  } finally {
    loading.value = false;
  }
}
onMounted(load);
watch(selected, load);

function sev(s: string) {
  return s === "CRITICAL" ? "DOWN" : s === "WARNING" ? "DEGRADED" : "HEALTHY";
}
</script>

<template>
  <div>
    <PageHeader title="Security" subtitle="Read-only risk findings. Pulse reports — it never changes your security configuration." />
    <EmptyState v-if="!loading && rows.length === 0" title="No obvious risks detected"
      message="Pulse flags obvious infrastructure risks (public DB ports, exposed Docker, weak/expired TLS). It is not a full vulnerability scanner." />
    <div v-else class="space-y-2">
      <div v-for="(f, i) in rows" :key="i" class="card">
        <div class="flex items-center gap-2">
          <HealthBadge :status="sev(f.severity)" />
          <span class="font-medium">{{ f.title }}</span>
        </div>
        <p class="text-sm text-muted mt-1">{{ f.detail }}</p>
        <p class="text-sm mt-1"><span class="text-muted">Recommendation:</span> {{ f.recommendation }}</p>
      </div>
    </div>
  </div>
</template>
