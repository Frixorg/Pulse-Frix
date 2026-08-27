<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { InventoryItem, InventoryResponse } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import StatCard from "@/components/cards/StatCard.vue";
import RefreshButton from "@/components/RefreshButton.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";

// One correlated answer to "what is running on this box?" — containers, host
// services, databases and proxies, with every listening socket attributed to
// whatever owns it. The correlation happens in the API from the agent's
// snapshot, so this view is a single request.

const servers = useServersStore();
const { selected } = storeToRefs(servers);

const data = ref<InventoryResponse | null>(null);
const loading = ref(false);
const lastUpdated = ref<number | null>(null);
const placement = ref<"all" | "host" | "container">("all");

async function load() {
  if (!selected.value || loading.value) return;
  loading.value = true;
  try {
    data.value = await api.inventory(selected.value.id);
    lastUpdated.value = Date.now();
  } catch {
    data.value = null;
  } finally {
    loading.value = false;
  }
}
onMounted(load);
watch(selected, load);

const rows = computed<InventoryItem[]>(() => {
  const items = data.value?.items ?? [];
  if (placement.value === "all") return items;
  return items.filter((i) => i.placement === placement.value);
});

const totals = computed(() => data.value?.totals);

function portLabel(item: InventoryItem): string {
  if (!item.ports?.length) return "—";
  return item.ports.map((p) => `${p.port}/${p.protocol}`).join(", ");
}

function owner(item: InventoryItem): string {
  if (item.unit) return item.unit;
  if (item.container_id) return item.container_id;
  if (item.pid) return `pid ${item.pid}`;
  return "—";
}
</script>

<template>
  <div>
    <PageHeader
      title="Inventory"
      subtitle="Every workload on this server, host and containerised, with its listening ports."
    >
      <template #actions>
        <RefreshButton
          :loading="loading"
          :updated-at="lastUpdated"
          :disabled="!selected"
          @refresh="load"
        />
      </template>
    </PageHeader>

    <EmptyState v-if="!data" title="No inventory data yet" />

    <template v-else>
      <div class="grid grid-cols-2 lg:grid-cols-5 gap-3 mb-3">
        <StatCard label="Host workloads" :value="String(totals?.host_workloads ?? 0)" />
        <StatCard label="Containers" :value="String(totals?.container_workloads ?? 0)" />
        <StatCard label="Listening ports" :value="String(totals?.listening_ports ?? 0)" />
        <StatCard label="Publicly bound" :value="String(totals?.public_ports ?? 0)" />
        <StatCard label="Databases" :value="String(totals?.databases ?? 0)" />
      </div>

      <div class="flex items-center gap-2 mb-3">
        <button
          v-for="opt in (['all', 'host', 'container'] as const)"
          :key="opt"
          class="btn"
          :class="placement === opt ? 'btn-primary' : 'btn-ghost'"
          @click="placement = opt"
        >
          {{ opt === "all" ? "All" : opt === "host" ? "Host" : "Containers" }}
        </button>
      </div>

      <div class="card overflow-x-auto">
        <table class="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Kind</th>
              <th>Where</th>
              <th>Health</th>
              <th>Ports</th>
              <th>Owner</th>
              <th>Detected by</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in rows" :key="item.id">
              <td class="font-medium">
                {{ item.name }}
                <span v-if="item.image" class="text-xs text-muted block">{{ item.image }}</span>
              </td>
              <td class="text-xs">{{ item.engine || item.kind }}</td>
              <td>
                <span :class="item.placement === 'container' ? 'text-accent' : 'text-muted'">
                  {{ item.placement }}
                </span>
              </td>
              <td><HealthBadge :status="item.health ?? 'UNKNOWN'" /></td>
              <td class="font-mono text-xs">{{ portLabel(item) }}</td>
              <td class="font-mono text-xs text-muted">{{ owner(item) }}</td>
              <td class="text-xs text-muted">{{ item.detected_by }}</td>
            </tr>
            <tr v-if="rows.length === 0">
              <td colspan="7" class="text-muted text-sm">Nothing matches this filter.</td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- An open port with no readable owner is a real finding, not an empty
           result: it usually means the agent cannot read /proc/<pid>/fd. -->
      <div v-if="data.unattributed.length" class="card mt-3">
        <div class="card-title">Unattributed listening ports</div>
        <p class="text-sm text-muted mb-2">
          These sockets are open but the owning process could not be read. Run the agent with
          permission to read <code>/proc/&lt;pid&gt;/fd</code> (and <code>pid: host</code> when
          containerised) to attribute them.
        </p>
        <p class="font-mono text-xs">
          {{ data.unattributed.map((p) => `${p.port}/${p.protocol}`).join(", ") }}
        </p>
      </div>
    </template>
  </div>
</template>
