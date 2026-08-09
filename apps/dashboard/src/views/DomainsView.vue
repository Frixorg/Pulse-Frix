<script setup lang="ts">
import { ref, watch, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { DomainView } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";

const servers = useServersStore();
const { selected } = storeToRefs(servers);
const rows = ref<DomainView[]>([]);
const loading = ref(false);

async function load() {
  if (!selected.value) return;
  loading.value = true;
  try {
    rows.value = (await api.domains(selected.value.id)).data ?? [];
  } catch {
    rows.value = [];
  } finally {
    loading.value = false;
  }
}
onMounted(load);
watch(selected, load);
</script>

<template>
  <div>
    <PageHeader title="Domains" subtitle="DNS, HTTP(S), latency and TLS validity — discovered from your reverse proxy." />
    <EmptyState v-if="!loading && rows.length === 0" title="No domains discovered"
      message="Domains are read from Nginx/Caddy/Apache/Traefik configuration." />
    <div v-else class="card overflow-x-auto">
      <table class="table">
        <thead>
          <tr><th>Domain</th><th>Health</th><th>TLS</th><th>Expires in</th><th>Source</th></tr>
        </thead>
        <tbody>
          <tr v-for="d in rows" :key="d.fqdn">
            <td class="font-medium">{{ d.fqdn }}</td>
            <td><HealthBadge :status="d.health" /></td>
            <td class="text-muted">{{ d.tls ? "valid" : "—" }}</td>
            <td class="text-muted">{{ d.tls_days_left !== undefined ? d.tls_days_left + " days" : "—" }}</td>
            <td class="text-muted">{{ d.source }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
