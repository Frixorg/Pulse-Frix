<script setup lang="ts">
import { ref, watch, computed, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { Resource } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import StatCard from "@/components/cards/StatCard.vue";
import { bytes } from "@/lib/format";

const servers = useServersStore();
const { selected } = storeToRefs(servers);
const fsRows = ref<Resource[]>([]);
const projRows = ref<Resource[]>([]);
const dockerSum = ref<Resource | null>(null);

function num(r: Resource, k: string): number {
  return Number(r.attributes?.[k] ?? 0);
}

async function load() {
  if (!selected.value) return;
  try {
    const snap = await api.discovery(selected.value.id);
    const all = snap.resources ?? [];
    fsRows.value = all
      .filter((r) => r.type === "filesystem" && num(r, "total_bytes") > 0)
      .sort((a, b) => num(b, "total_bytes") - num(a, "total_bytes"));
    projRows.value = all
      .filter((r) => r.type === "storage_group" && num(r, "total_bytes") > 0)
      .sort((a, b) => num(b, "total_bytes") - num(a, "total_bytes"));
    dockerSum.value = all.find((r) => r.type === "docker_storage") ?? null;
  } catch {
    fsRows.value = [];
    projRows.value = [];
    dockerSum.value = null;
  }
}
onMounted(load);
watch(selected, load);

const primary = computed(() => fsRows.value.find((r) => r.name === "/") ?? fsRows.value[0] ?? null);
const primaryUsed = computed(() => (primary.value ? num(primary.value, "used_pct") : 0));
const totalCap = computed(() => fsRows.value.reduce((s, r) => s + num(r, "total_bytes"), 0));
const totalUsed = computed(() => fsRows.value.reduce((s, r) => s + num(r, "used_bytes"), 0));
const totalFree = computed(() => Math.max(0, totalCap.value - totalUsed.value));

const projMax = computed(() => projRows.value.reduce((m, r) => Math.max(m, num(r, "total_bytes")), 0));
function projPct(r: Resource, key: string) {
  return projMax.value > 0 ? (num(r, key) / projMax.value) * 100 : 0;
}
function prettyProject(name: string) {
  return name.replace(/[-_]/g, " ");
}

function barTone(pct: number) {
  return pct >= 90 ? "down" : pct >= 75 ? "warn" : "ok";
}
</script>

<template>
  <div>
    <PageHeader
      title="Storage"
      subtitle="Filesystems and per-project disk usage. Pulse caps its own storage and never fills your disk."
    />
    <EmptyState v-if="fsRows.length === 0 && projRows.length === 0" title="No filesystems discovered" />
    <template v-else>
      <div class="top">
        <div class="card donut-card">
          <div class="card-title">{{ primary?.name || "Root" }} usage</div>
          <div class="donut-wrap">
            <svg viewBox="0 0 42 42" class="donut">
              <circle class="ring" cx="21" cy="21" r="15.9155" fill="transparent" stroke-width="4" />
              <circle
                class="seg"
                :class="barTone(primaryUsed)"
                cx="21"
                cy="21"
                r="15.9155"
                fill="transparent"
                stroke-width="4"
                :stroke-dasharray="`${primaryUsed} ${100 - primaryUsed}`"
                stroke-dashoffset="25"
                stroke-linecap="round"
              />
              <text x="21" y="20.5" class="donut-num">{{ primaryUsed }}%</text>
              <text x="21" y="26" class="donut-sub">used</text>
            </svg>
          </div>
          <div class="legend">
            <span><i class="dot used"></i>Used {{ bytes(primary ? num(primary, "used_bytes") : 0) }}</span>
            <span><i class="dot free"></i>Free {{ bytes(primary ? num(primary, "total_bytes") - num(primary, "used_bytes") : 0) }}</span>
          </div>
        </div>

        <div class="cards">
          <StatCard label="Total capacity" :value="bytes(totalCap)" />
          <StatCard
            label="Used"
            :value="bytes(totalUsed)"
            :percent="totalCap ? (totalUsed / totalCap) * 100 : 0"
            :tone="barTone(totalCap ? (totalUsed / totalCap) * 100 : 0) === 'ok' ? 'healthy' : barTone(totalCap ? (totalUsed / totalCap) * 100 : 0) === 'warn' ? 'degraded' : 'down'"
          />
          <StatCard label="Free" :value="bytes(totalFree)" />
          <StatCard label="Filesystems" :value="fsRows.length" />
        </div>
      </div>

      <!-- Docker footprint (from `docker system df`) -->
      <div v-if="dockerSum" class="docker-strip">
        <StatCard label="Docker images" :value="bytes(num(dockerSum, 'images_bytes'))" />
        <StatCard label="Docker volumes" :value="bytes(num(dockerSum, 'volume_bytes'))" />
        <StatCard label="Container layers" :value="bytes(num(dockerSum, 'writable_bytes'))" />
        <StatCard label="Docker total" :value="bytes(num(dockerSum, 'total_bytes'))" />
      </div>

      <!-- Storage by project: attributes disk use to real Compose projects -->
      <div v-if="projRows.length" class="card mb">
        <div class="card-title-row">
          <div class="card-title">Storage by project</div>
          <span class="legend2"><i class="sw vol"></i>volumes <i class="sw wr"></i>container layers</span>
        </div>
        <table class="table">
          <thead>
            <tr><th>Project</th><th>Usage</th><th>Volumes</th><th>Layers</th><th>Total</th><th>Containers</th></tr>
          </thead>
          <tbody>
            <tr v-for="r in projRows" :key="r.id">
              <td class="font-medium">{{ prettyProject(r.name) }}</td>
              <td class="usage-cell">
                <div class="sbar">
                  <i class="vol" :style="{ width: projPct(r, 'volume_bytes') + '%' }"></i>
                  <i class="wr" :style="{ width: projPct(r, 'writable_bytes') + '%' }"></i>
                </div>
              </td>
              <td class="font-mono text-xs">{{ bytes(num(r, "volume_bytes")) }}</td>
              <td class="font-mono text-xs">{{ bytes(num(r, "writable_bytes")) }}</td>
              <td class="font-mono text-xs font-medium">{{ bytes(num(r, "total_bytes")) }}</td>
              <td class="font-mono text-xs text-muted">{{ num(r, "container_count") }}</td>
            </tr>
          </tbody>
        </table>
        <p class="note">Volume + writable-layer sizes come from Docker. Bind-mounted data dirs are counted under the filesystem they live on, above.</p>
      </div>

      <div class="card overflow-x-auto">
        <div class="card-title">Filesystems</div>
        <table class="table">
          <thead>
            <tr><th>Mount</th><th>Usage</th><th>Used</th><th>Total</th><th>Used %</th><th>Inodes %</th></tr>
          </thead>
          <tbody>
            <tr v-for="r in fsRows" :key="r.id">
              <td class="font-medium">{{ r.name }}</td>
              <td class="usage-cell">
                <div class="bar"><i :class="barTone(num(r, 'used_pct'))" :style="{ width: Math.min(100, num(r, 'used_pct')) + '%' }"></i></div>
              </td>
              <td class="font-mono text-xs">{{ bytes(num(r, "used_bytes")) }}</td>
              <td class="font-mono text-xs">{{ bytes(num(r, "total_bytes")) }}</td>
              <td class="font-mono text-xs" :class="{ 'text-down': num(r, 'used_pct') >= 90, 'text-degraded': num(r, 'used_pct') >= 75 && num(r, 'used_pct') < 90 }">{{ num(r, "used_pct") }}%</td>
              <td class="font-mono text-xs" :class="{ 'text-degraded': num(r, 'inode_pct') >= 80 }">{{ num(r, "inode_pct") }}%</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>

<style scoped>
.top {
  display: grid;
  grid-template-columns: 260px 1fr;
  gap: 14px;
  margin-bottom: 14px;
}
.donut-card {
  display: flex;
  flex-direction: column;
  align-items: center;
}
.donut-wrap {
  width: 160px;
  margin: 8px auto 4px;
}
.donut {
  width: 100%;
}
.ring {
  stroke: var(--pulse-surface-2);
}
.seg {
  transition: stroke-dasharray 0.5s ease;
}
.seg.ok {
  stroke: var(--pulse-accent);
}
.seg.warn {
  stroke: var(--pulse-degraded);
}
.seg.down {
  stroke: var(--pulse-down);
}
.donut-num {
  font-family: var(--pulse-font-display);
  font-size: 8px;
  font-weight: 700;
  fill: var(--pulse-text);
  text-anchor: middle;
}
.donut-sub {
  font-size: 3px;
  fill: var(--pulse-text-muted);
  text-anchor: middle;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}
.legend {
  display: flex;
  gap: 16px;
  font-size: 12px;
  color: var(--pulse-text-muted);
  margin-top: 6px;
}
.legend .dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 6px;
}
.dot.used {
  background: var(--pulse-accent);
}
.dot.free {
  background: var(--pulse-surface-2);
  border: 1px solid var(--pulse-border);
}
.cards {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 12px;
}
.docker-strip {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 14px;
}
.mb {
  margin-bottom: 14px;
}
.card-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.legend2 {
  font-size: 11px;
  color: var(--pulse-text-muted);
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.legend2 .sw {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 3px;
  margin: 0 2px 0 8px;
}
.legend2 .sw.vol {
  background: var(--pulse-accent);
}
.legend2 .sw.wr {
  background: #38bdf8;
}
.usage-cell {
  width: 200px;
}
.bar {
  height: 7px;
  border-radius: 4px;
  background: var(--pulse-surface-2);
  overflow: hidden;
}
.bar i {
  display: block;
  height: 100%;
  border-radius: 4px;
}
.bar i.ok {
  background: var(--pulse-accent);
}
.bar i.warn {
  background: var(--pulse-degraded);
}
.bar i.down {
  background: var(--pulse-down);
}
.sbar {
  display: flex;
  height: 7px;
  border-radius: 4px;
  background: var(--pulse-surface-2);
  overflow: hidden;
}
.sbar i {
  display: block;
  height: 100%;
}
.sbar i.vol {
  background: var(--pulse-accent);
}
.sbar i.wr {
  background: #38bdf8;
}
.note {
  font-size: 11.5px;
  color: var(--pulse-text-muted);
  margin-top: 10px;
}
@media (max-width: 900px) {
  .top {
    grid-template-columns: 1fr;
  }
  .cards {
    grid-template-columns: repeat(2, 1fr);
  }
  .docker-strip {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
