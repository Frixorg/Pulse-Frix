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
    <PageHeader
      title="Domains"
      subtitle="Virtual hosts discovered from your reverse proxy — proxy target, TLS and certificate validity. Click a domain to open it."
    />
    <EmptyState
      v-if="!loading && rows.length === 0"
      title="No domains discovered"
      message="Domains are read from Nginx / Caddy / Apache / Traefik configuration."
    />
    <div v-else class="card overflow-x-auto">
      <table class="table">
        <thead>
          <tr>
            <th>Domain</th>
            <th>Proxies to</th>
            <th>Health</th>
            <th>TLS</th>
            <th>Expires in</th>
            <th>Source</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in rows" :key="d.fqdn" class="row">
            <td>
              <a :href="d.url" target="_blank" rel="noopener noreferrer" class="dlink">
                {{ d.fqdn }}
                <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
                  <path d="M15 3h6v6M10 14 21 3" />
                </svg>
              </a>
            </td>
            <td class="font-mono text-xs text-muted">{{ d.upstream || "—" }}</td>
            <td><HealthBadge :status="d.health" /></td>
            <td>
              <span v-if="d.tls" class="tag tag-ok">valid</span>
              <span v-else-if="d.ssl" class="tag">configured</span>
              <span v-else class="text-muted">—</span>
            </td>
            <td class="text-muted">
              <span v-if="d.tls_days_left !== undefined" :class="{ 'text-degraded': d.tls_days_left < 30, 'text-down': d.tls_days_left < 0 }">
                {{ d.tls_days_left }} days
              </span>
              <span v-else>—</span>
            </td>
            <td class="text-muted text-xs">{{ d.source }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.row {
  transition: background 0.12s;
}
.row:hover {
  background: var(--pulse-surface-2);
}
.dlink {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--pulse-text);
  text-decoration: none;
  font-weight: 500;
}
.dlink:hover {
  color: var(--pulse-accent);
}
.dlink svg {
  opacity: 0.5;
}
.dlink:hover svg {
  opacity: 1;
}
.tag {
  display: inline-block;
  padding: 2px 9px;
  border-radius: 999px;
  font-size: 11px;
  color: var(--pulse-text-muted);
  border: 1px solid var(--pulse-border);
}
.tag-ok {
  color: var(--pulse-accent);
  border-color: rgba(199, 245, 66, 0.35);
  background: rgba(199, 245, 66, 0.1);
}
</style>
