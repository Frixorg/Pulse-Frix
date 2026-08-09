<script setup lang="ts">
import { ref, watch, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import type { Resource } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";

const props = defineProps<{
  title: string;
  subtitle?: string;
  loader: (serverId: string) => Promise<{ data: Resource[] }>;
  attrKeys?: string[];
}>();

const servers = useServersStore();
const { selected } = storeToRefs(servers);
const rows = ref<Resource[]>([]);
const loading = ref(false);
const error = ref("");

async function load() {
  if (!selected.value) {
    rows.value = [];
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    const page = await props.loader(selected.value.id);
    rows.value = page.data ?? [];
  } catch (e) {
    error.value = e instanceof Error ? e.message : "failed to load";
  } finally {
    loading.value = false;
  }
}

function attr(r: Resource, key: string): string {
  const v = r.attributes?.[key];
  if (v === undefined || v === null) return "—";
  return String(v);
}

onMounted(load);
watch(selected, load);
</script>

<template>
  <div>
    <PageHeader :title="title" :subtitle="subtitle" />
    <EmptyState v-if="!selected" title="No server selected" message="Connect a server to see this view." />
    <EmptyState v-else-if="!loading && rows.length === 0" :title="`No ${title.toLowerCase()} discovered`"
      message="Pulse only shows what it actually found — it never invents resources." />
    <div v-else class="card overflow-x-auto">
      <table class="table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Health</th>
            <th v-for="k in attrKeys ?? []" :key="k">{{ k.replace(/_/g, " ") }}</th>
            <th>Detected by</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in rows" :key="r.id">
            <td class="font-medium">{{ r.name }}</td>
            <td><HealthBadge :status="r.health" /></td>
            <td v-for="k in attrKeys ?? []" :key="k" class="text-muted font-mono text-xs">{{ attr(r, k) }}</td>
            <td class="text-muted">{{ r.detected_by }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
