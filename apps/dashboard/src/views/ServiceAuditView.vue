<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type {
  AuditCategory,
  AuditFinding,
  ServiceAuditResponse,
  ServiceNode,
} from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import StatCard from "@/components/cards/StatCard.vue";
import RefreshButton from "@/components/RefreshButton.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";
import { bytes } from "@/lib/format";

// What talks to what, and what nothing appears to need.
//
// The findings here are leads, not verdicts. Each one shows the evidence behind
// it and how sure the analysis is, and the page states its own blind spots
// up front — because acting on one of these means removing something from a
// live server. Pulse flags; the operator decides.

const servers = useServersStore();
const { selected } = storeToRefs(servers);

const data = ref<ServiceAuditResponse | null>(null);
const loading = ref(false);
const lastUpdated = ref<number | null>(null);
const expanded = ref<Record<string, boolean>>({});
const category = ref<AuditCategory | "all">("all");

async function load() {
  if (!selected.value || loading.value) return;
  loading.value = true;
  try {
    data.value = await api.serviceAudit(selected.value.id);
    lastUpdated.value = Date.now();
  } catch {
    data.value = null;
  } finally {
    loading.value = false;
  }
}
onMounted(load);
watch(selected, load);

const totals = computed(() => data.value?.totals);

const categories = computed<AuditCategory[]>(() => {
  const seen = new Set<AuditCategory>();
  for (const f of data.value?.findings ?? []) seen.add(f.category);
  return [...seen].sort();
});

const findings = computed<AuditFinding[]>(() => {
  const all = data.value?.findings ?? [];
  return category.value === "all" ? all : all.filter((f) => f.category === category.value);
});

const nodesById = computed<Record<string, ServiceNode>>(() => {
  const map: Record<string, ServiceNode> = {};
  for (const n of data.value?.nodes ?? []) map[n.id] = n;
  return map;
});

/** Services that are connected to something, so the graph reads as a list. */
const connected = computed(() => (data.value?.nodes ?? []).filter((n) => (n.peers?.length ?? 0) > 0 || (n.inbound_routes?.length ?? 0) > 0));
const isolated = computed(() => (data.value?.nodes ?? []).filter((n) => !(n.peers?.length ?? 0) && !(n.inbound_routes?.length ?? 0)));

function nodeName(id: string): string {
  return nodesById.value[id]?.name ?? id;
}

const severityClass: Record<string, string> = {
  medium: "text-degraded",
  low: "text-muted",
  info: "text-muted",
};

const categoryLabel: Record<AuditCategory, string> = {
  stopped: "Stopped",
  unrouted: "Unrouted",
  idle: "Idle",
  unreferenced: "Unreferenced",
  duplicate: "Duplicate",
  orphaned: "Orphaned storage",
};

function toggle(id: string) {
  expanded.value = { ...expanded.value, [id]: !expanded.value[id] };
}

function reclaimSummary(f: AuditFinding): string {
  const parts: string[] = [];
  if (f.reclaimable.disk_bytes > 0) parts.push(`${bytes(f.reclaimable.disk_bytes)} disk`);
  if (f.reclaimable.memory_bytes > 0) parts.push(`${bytes(f.reclaimable.memory_bytes)} memory`);
  return parts.length ? parts.join(" · ") : "no measurable resources";
}
</script>

<template>
  <div>
    <PageHeader
      title="Service Audit"
      subtitle="How your services connect, and which ones nothing appears to need."
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

    <EmptyState v-if="!data" title="No discovery data yet" />

    <template v-else>
      <div class="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-3">
        <StatCard label="Services" :value="String(totals?.services ?? 0)" />
        <StatCard label="Connections" :value="String(totals?.relations ?? 0)" />
        <StatCard label="Flagged" :value="String(totals?.flagged ?? 0)" />
        <StatCard
          label="Reclaimable"
          :value="bytes((totals?.reclaimable_disk_bytes ?? 0) + (totals?.reclaimable_memory_bytes ?? 0))"
        />
      </div>

      <!-- Stated before the findings, not after: acting on one of these means
           removing something from a live server. -->
      <div class="card mb-3">
        <div class="card-title">Read this before acting on anything below</div>
        <p class="text-sm text-muted mb-2">
          These are leads, not verdicts. Pulse flags what it cannot find a use for — it never
          removes anything, and it cannot see everything.
        </p>
        <ul class="text-sm text-muted list-disc list-inside space-y-1">
          <li v-for="l in data.limitations" :key="l">{{ l }}</li>
        </ul>
      </div>

      <div v-if="findings.length || categories.length" class="flex flex-wrap items-center gap-2 mb-3">
        <button
          class="btn"
          :class="category === 'all' ? 'btn-primary' : 'btn-ghost'"
          @click="category = 'all'"
        >
          All
        </button>
        <button
          v-for="c in categories"
          :key="c"
          class="btn"
          :class="category === c ? 'btn-primary' : 'btn-ghost'"
          @click="category = c"
        >
          {{ categoryLabel[c] }}
        </button>
      </div>

      <EmptyState
        v-if="findings.length === 0"
        title="Nothing flagged"
        message="Every discovered service is connected to something, running, and doing measurable work."
      />

      <div v-for="f in findings" :key="f.id" class="card mb-2">
        <button class="w-full text-left" @click="toggle(f.id)">
          <div class="flex items-start justify-between gap-3">
            <div>
              <div class="font-medium" :class="severityClass[f.severity]">{{ f.title }}</div>
              <div class="text-xs text-muted mt-1">
                {{ categoryLabel[f.category] }} · {{ f.confidence }} confidence ·
                {{ reclaimSummary(f) }}
              </div>
            </div>
            <span class="text-xs text-muted shrink-0">{{ expanded[f.id] ? "−" : "+" }}</span>
          </div>
        </button>

        <div v-if="expanded[f.id]" class="mt-3 space-y-3">
          <p class="text-sm text-muted">{{ f.detail }}</p>
          <div>
            <div class="text-xs text-muted mb-1">What was observed</div>
            <ul class="text-sm list-disc list-inside space-y-1">
              <li v-for="e in f.evidence" :key="e">{{ e }}</li>
            </ul>
          </div>
          <div>
            <div class="text-xs text-muted mb-1">Before you act</div>
            <p class="text-sm">{{ f.recommendation }}</p>
          </div>
        </div>
      </div>

      <!-- The relationship map, as a per-service list: which domains reach it
           and which services it is wired to. -->
      <div class="card mt-4 overflow-x-auto">
        <div class="card-title">Connections</div>
        <table class="table">
          <thead>
            <tr>
              <th>Service</th>
              <th>Where</th>
              <th>Health</th>
              <th>Routed from</th>
              <th>Connected to</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="n in connected" :key="n.id">
              <td class="font-medium">
                {{ n.name }}
                <span v-if="n.engine" class="text-xs text-muted block">{{ n.engine }}</span>
              </td>
              <td class="text-xs">{{ n.placement }}</td>
              <td><HealthBadge :status="n.health ?? 'UNKNOWN'" /></td>
              <td class="text-xs">{{ n.inbound_routes?.join(", ") || "—" }}</td>
              <td class="text-xs text-muted">
                {{ n.peers?.map(nodeName).join(", ") || "—" }}
              </td>
            </tr>
            <tr v-if="connected.length === 0">
              <td colspan="5" class="text-muted text-sm">
                No relationship could be derived. That usually means no reverse proxy and no
                user-defined Docker networks, not that nothing is connected.
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="isolated.length" class="card mt-3 overflow-x-auto">
        <div class="card-title">No connection discovered</div>
        <p class="text-sm text-muted mb-2">
          Pulse found no proxy route, shared network or Compose relationship for these. Some are
          genuinely standalone; some are just talking over a channel Pulse cannot see.
        </p>
        <table class="table">
          <thead>
            <tr>
              <th>Service</th>
              <th>Where</th>
              <th>Status</th>
              <th>Ports</th>
              <th>Memory</th>
              <th>Disk</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="n in isolated" :key="n.id">
              <td class="font-medium">
                {{ n.name }}
                <span v-if="n.essential" class="text-xs text-muted block">core infrastructure</span>
              </td>
              <td class="text-xs">{{ n.placement }}</td>
              <td class="text-xs">{{ n.status || "—" }}</td>
              <td class="font-mono text-xs">{{ n.ports?.join(", ") || "—" }}</td>
              <td class="font-mono text-xs">{{ n.usage.memory_bytes ? bytes(n.usage.memory_bytes) : "—" }}</td>
              <td class="font-mono text-xs">{{ n.usage.disk_bytes ? bytes(n.usage.disk_bytes) : "—" }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>
