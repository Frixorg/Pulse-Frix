<script setup lang="ts">
import { ref, watch, computed, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { SecurityAudit, SecurityCheck } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";

const servers = useServersStore();
const { selected } = storeToRefs(servers);

const audit = ref<SecurityAudit | null>(null);
const loading = ref(false);
const activeCat = ref<string | null>(null);

async function load() {
  if (!selected.value) return;
  loading.value = true;
  try {
    audit.value = await api.security(selected.value.id);
  } catch {
    audit.value = null;
  } finally {
    loading.value = false;
  }
}
onMounted(load);
watch(selected, load);

const checks = computed(() => audit.value?.checks ?? []);
const findings = computed(() => audit.value?.findings ?? []);
const shown = computed(() =>
  activeCat.value ? findings.value.filter((f) => f.category === activeCat.value) : findings.value,
);
const counts = computed(() => {
  const c: Record<string, number> = { CRITICAL: 0, WARNING: 0, INFO: 0 };
  for (const f of findings.value) c[f.severity] = (c[f.severity] ?? 0) + 1;
  return c;
});

function chipClass(c: SecurityCheck) {
  return c.status === "issues" ? "issues" : c.status === "pass" ? "pass" : "soon";
}
function toggleCat(c: SecurityCheck) {
  if (c.status !== "issues") return;
  activeCat.value = activeCat.value === c.id ? null : c.id;
}

const rerunning = ref<string | null>(null);
async function rerunCheck(c: SecurityCheck, e: Event) {
  e.stopPropagation();
  if (!selected.value || c.status === "not_assessed") return;
  rerunning.value = c.id;
  try {
    const res = await api.security(selected.value.id, c.id);
    if (audit.value) {
      const nc = res.checks[0];
      audit.value.checks = audit.value.checks.map((x) => (x.id === c.id ? nc ?? x : x));
      const others = audit.value.findings.filter((f) => f.category !== c.id);
      audit.value.findings = [...others, ...res.findings];
    }
  } catch {
    /* ignore */
  } finally {
    rerunning.value = null;
  }
}
function sevClass(s: string) {
  return s === "CRITICAL" ? "crit" : s === "WARNING" ? "warn" : "info";
}
function fmtTime(t?: string) {
  return t ? new Date(t).toLocaleTimeString() : "";
}
</script>

<template>
  <div>
    <PageHeader title="Security" subtitle="Read-only risk findings. Pulse reports — it never changes your security configuration." />
    <EmptyState v-if="!selected" title="No server selected" />
    <template v-else>
      <!-- Controls -->
      <div class="bar">
        <div class="summary">
          <span class="pill crit">{{ counts.CRITICAL }} critical</span>
          <span class="pill warn">{{ counts.WARNING }} warning</span>
          <span class="pill info">{{ counts.INFO }} info</span>
          <span v-if="audit" class="ran">last run {{ fmtTime(audit.generated_at) }}</span>
        </div>
        <button class="rerun" :disabled="loading" @click="load">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" :class="{ spin: loading }">
            <path d="M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6" />
          </svg>
          {{ loading ? "Running…" : "Re-run checks" }}
        </button>
      </div>

      <!-- Check catalogue -->
      <div class="checks">
        <div
          v-for="c in checks"
          :key="c.id"
          class="chk"
          :class="[chipClass(c), { active: activeCat === c.id }]"
          :title="c.note || ''"
          @click="toggleCat(c)"
        >
          <span class="chk-dot"></span>
          <span class="chk-name">{{ c.name }}</span>
          <span v-if="c.status === 'issues'" class="chk-n">{{ c.count }}</span>
          <span v-else-if="c.status === 'pass'" class="chk-ok">✓</span>
          <span v-else class="chk-soon">soon</span>
          <button
            v-if="c.status !== 'not_assessed'"
            class="chk-rerun"
            :title="`Re-run ${c.name}`"
            @click="rerunCheck(c, $event)"
          >
            <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2.2" :class="{ spin: rerunning === c.id }">
              <path d="M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6" />
            </svg>
          </button>
        </div>
      </div>

      <!-- Findings -->
      <div v-if="activeCat" class="filter-note">
        Showing <b>{{ checks.find((c) => c.id === activeCat)?.name }}</b> ·
        <button class="clear" @click="activeCat = null">clear filter</button>
      </div>

      <div v-if="shown.length === 0" class="clean">
        <div class="clean-badge">✓</div>
        <div>
          <div class="clean-t">No issues in this view</div>
          <p class="clean-d">Pulse flags the risks it can see from read-only discovery. Deeper host checks (SSH, headers, privileges) are coming soon.</p>
        </div>
      </div>

      <div v-else class="findings">
        <div v-for="f in shown" :key="f.id" class="finding" :class="sevClass(f.severity)">
          <div class="f-head">
            <span class="sev" :class="sevClass(f.severity)">{{ f.severity }}</span>
            <span class="f-title">{{ f.title }}</span>
            <span v-if="f.resource" class="f-res">{{ f.resource }}</span>
          </div>
          <p class="f-detail">{{ f.detail }}</p>
          <p class="f-rec"><span class="f-rec-k">Fix</span> {{ f.recommendation }}</p>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
.summary {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.pill {
  font-size: 12px;
  padding: 4px 11px;
  border-radius: 999px;
  border: 1px solid var(--pulse-border);
}
.pill.crit {
  color: #fca5a5;
  border-color: rgba(248, 113, 113, 0.35);
  background: rgba(248, 113, 113, 0.1);
}
.pill.warn {
  color: var(--pulse-degraded);
  border-color: rgba(251, 191, 36, 0.35);
  background: rgba(251, 191, 36, 0.1);
}
.pill.info {
  color: var(--pulse-text-muted);
}
.ran {
  font-size: 12px;
  color: var(--pulse-text-muted);
  margin-left: 4px;
}
.rerun {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 8px 14px;
  border-radius: 10px;
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  border: 0;
  font-family: var(--pulse-font-mono);
  font-weight: 700;
  font-size: 13px;
  cursor: pointer;
}
.rerun:disabled {
  opacity: 0.6;
  cursor: default;
}
.spin {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
.checks {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 8px;
  margin-bottom: 18px;
}
.chk {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 10px 12px;
  border-radius: 11px;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
  font-family: var(--pulse-font-mono);
  font-size: 12.5px;
  cursor: default;
  text-align: left;
}
.chk.issues {
  cursor: pointer;
}
.chk.active {
  border-color: rgba(199, 245, 66, 0.5);
}
.chk-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.chk.pass .chk-dot {
  background: var(--pulse-accent);
}
.chk.issues .chk-dot {
  background: var(--pulse-down);
}
.chk.soon .chk-dot {
  background: var(--pulse-unknown);
}
.chk-name {
  flex: 1;
}
.chk.soon .chk-name {
  color: var(--pulse-text-muted);
}
.chk-n {
  font-weight: 700;
  color: #fca5a5;
}
.chk-ok {
  color: var(--pulse-accent);
}
.chk-soon {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--pulse-text-muted);
}
.chk-rerun {
  margin-left: 2px;
  background: transparent;
  border: 0;
  color: var(--pulse-text-muted);
  cursor: pointer;
  padding: 3px;
  border-radius: 6px;
  display: inline-grid;
  place-items: center;
}
.chk-rerun:hover {
  color: var(--pulse-accent);
  background: var(--pulse-surface-2);
}
.filter-note {
  font-size: 12.5px;
  color: var(--pulse-text-muted);
  margin-bottom: 10px;
}
.clear {
  background: none;
  border: 0;
  color: var(--pulse-accent);
  cursor: pointer;
  font-size: 12.5px;
}
.clean {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 22px;
  border-radius: 14px;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
}
.clean-badge {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  display: grid;
  place-items: center;
  background: rgba(199, 245, 66, 0.14);
  color: var(--pulse-accent);
  border: 1px solid rgba(199, 245, 66, 0.3);
  font-size: 18px;
  flex-shrink: 0;
}
.clean-t {
  font-weight: 600;
}
.clean-d {
  font-size: 13px;
  color: var(--pulse-text-muted);
  margin: 3px 0 0;
}
.findings {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.finding {
  padding: 16px;
  border-radius: 14px;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
  border-left: 3px solid var(--pulse-border);
}
.finding.crit {
  border-left-color: var(--pulse-down);
}
.finding.warn {
  border-left-color: var(--pulse-degraded);
}
.finding.info {
  border-left-color: var(--pulse-unknown);
}
.f-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.sev {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.05em;
  padding: 2px 8px;
  border-radius: 999px;
}
.sev.crit {
  color: #fca5a5;
  background: rgba(248, 113, 113, 0.14);
}
.sev.warn {
  color: var(--pulse-degraded);
  background: rgba(251, 191, 36, 0.14);
}
.sev.info {
  color: var(--pulse-text-muted);
  background: var(--pulse-surface-2);
}
.f-title {
  font-weight: 600;
  font-size: 14px;
}
.f-res {
  font-family: var(--pulse-font-mono);
  font-size: 11px;
  color: var(--pulse-text-muted);
  background: var(--pulse-solid-2);
  padding: 2px 8px;
  border-radius: 6px;
}
.f-detail {
  font-size: 13px;
  color: var(--pulse-text-muted);
  line-height: 1.6;
  margin: 8px 0 6px;
}
.f-rec {
  font-size: 13px;
  line-height: 1.55;
}
.f-rec-k {
  color: var(--pulse-accent);
  font-weight: 700;
  margin-right: 6px;
}
</style>
