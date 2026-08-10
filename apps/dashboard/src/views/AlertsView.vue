<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { Alert, AlertInstance } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";
import { timeAgo } from "@/lib/format";

const servers = useServersStore();
const { selected } = storeToRefs(servers);

const rules = ref<Alert[]>([]);
const instances = ref<AlertInstance[]>([]);
const containers = ref<string[]>([]);

const showForm = ref(false);
const saving = ref(false);
const formErr = ref("");
const blank = () => ({ name: "", kind: "metric", metric: "cpu", op: ">", threshold: 80, container: "", forSeconds: 60, severity: "WARNING" });
const f = ref(blank());

const toasts = ref<AlertInstance[]>([]);
const seen = new Set<string>();
let poll: number | undefined;

function fmtExpr(expr: string): string {
  const p = expr.trim().split(/\s+/);
  if (p[0] === "container_down") return `Container "${p[1]}" not running`;
  if (p.length === 3) return `${p[0].toUpperCase()} ${p[1]} ${p[2]}${p[0] === "load" ? "" : "%"}`;
  return expr;
}
function sev(s: string) {
  return s === "CRITICAL" ? "DOWN" : s === "WARNING" ? "DEGRADED" : "HEALTHY";
}

async function loadRules() {
  try {
    rules.value = (await api.alerts()).data ?? [];
  } catch {
    rules.value = [];
  }
}
async function loadInstances() {
  try {
    const list = (await api.alertInstances()).data ?? [];
    for (const inst of list) {
      if (!seen.has(inst.dedup_key)) {
        seen.add(inst.dedup_key);
        toasts.value.push(inst);
        setTimeout(() => dismiss(inst.dedup_key), 9000);
      }
    }
    const active = new Set(list.map((i) => i.dedup_key));
    for (const k of [...seen]) if (!active.has(k)) seen.delete(k);
    instances.value = list;
  } catch {
    instances.value = [];
  }
}
function dismiss(dedup: string) {
  toasts.value = toasts.value.filter((t) => t.dedup_key !== dedup);
}
async function loadContainers() {
  if (!selected.value) {
    containers.value = [];
    return;
  }
  try {
    containers.value = ((await api.containers(selected.value.id)).data ?? []).map((r) => r.name).sort();
  } catch {
    containers.value = [];
  }
}

function buildExpr(): string {
  return f.value.kind === "container_down"
    ? `container_down ${f.value.container}`
    : `${f.value.metric} ${f.value.op} ${f.value.threshold}`;
}
function openForm() {
  f.value = blank();
  formErr.value = "";
  showForm.value = true;
}
async function save() {
  formErr.value = "";
  if (!f.value.name.trim()) {
    formErr.value = "Give the alert a name.";
    return;
  }
  if (f.value.kind === "container_down" && !f.value.container) {
    formErr.value = "Pick a container.";
    return;
  }
  saving.value = true;
  try {
    await api.createAlert({
      name: f.value.name.trim(),
      expr: buildExpr(),
      severity: f.value.severity as Alert["severity"],
      for_seconds: Number(f.value.forSeconds) || 0,
      cooldown_seconds: 0,
      enabled: true,
    });
    showForm.value = false;
    await loadRules();
  } catch (e) {
    formErr.value = e instanceof Error ? e.message : "Could not save";
  } finally {
    saving.value = false;
  }
}
async function toggle(a: Alert) {
  try {
    await api.updateAlert(a.id, { ...a, enabled: !a.enabled });
    await loadRules();
  } catch {
    /* ignore */
  }
}
async function remove(a: Alert) {
  try {
    await api.deleteAlert(a.id);
    await loadRules();
  } catch {
    /* ignore */
  }
}

onMounted(async () => {
  await Promise.all([loadRules(), loadInstances(), loadContainers()]);
  poll = window.setInterval(loadInstances, 15000);
});
onBeforeUnmount(() => {
  if (poll) clearInterval(poll);
});
watch(selected, loadContainers);
</script>

<template>
  <div>
    <PageHeader title="Alerts" subtitle="Define thresholds on metrics and containers. Breaches fire here with a live pop-up." />

    <div class="bar">
      <button class="new-alert" type="button" @click="openForm">+ Define an alert</button>
      <div class="tg" aria-label="Connect to Telegram channel — coming soon">
        <div class="tg-inner">
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
            <path fill="#29a9eb" d="M12 24A12 12 0 1 0 12 0a12 12 0 0 0 0 24z" />
            <path fill="#fff" d="M5.5 11.8 17 7.3c.5-.2 1 .1.8.9l-2 9.2c-.1.6-.5.7-1 .5l-2.8-2-1.3 1.3c-.2.2-.3.3-.6.3l.2-2.9 5.2-4.7c.2-.2 0-.3-.3-.1L8 13l-2.7-.8c-.6-.2-.6-.6.2-.9z" />
          </svg>
          Connect to Telegram channel
        </div>
        <span class="tg-badge">Coming soon</span>
      </div>
    </div>

    <!-- Firing now -->
    <h3 class="sec-title">Firing now</h3>
    <EmptyState v-if="instances.length === 0" title="Nothing firing" message="All clear — breaches will appear here and pop up in the corner." />
    <div v-else class="list">
      <div v-for="a in instances" :key="a.id" class="card flex items-start justify-between">
        <div>
          <div class="flex items-center gap-2">
            <HealthBadge :status="sev(a.severity)" />
            <span class="font-medium">{{ a.name }}</span>
            <span class="state">{{ a.state }}</span>
          </div>
          <p v-if="a.root_cause" class="text-sm text-muted mt-1">{{ a.root_cause }}</p>
        </div>
        <span class="text-xs text-muted">{{ timeAgo(a.started_at) }}</span>
      </div>
    </div>

    <!-- Rules -->
    <h3 class="sec-title mt">Rules</h3>
    <EmptyState v-if="rules.length === 0" title="No rules yet" message="Define an alert to start watching a metric or container." />
    <div v-else class="list">
      <div v-for="a in rules" :key="a.id" class="card rule">
        <div class="rule-main">
          <span class="sevdot" :class="a.severity.toLowerCase()"></span>
          <div>
            <div class="font-medium">{{ a.name }}</div>
            <div class="rule-cond">{{ fmtExpr(a.expr) }} · for {{ a.for_seconds }}s</div>
          </div>
        </div>
        <div class="rule-actions">
          <button class="switch" :class="{ on: a.enabled }" :title="a.enabled ? 'Enabled' : 'Disabled'" @click="toggle(a)">
            <span class="knob"></span>
          </button>
          <button class="del" title="Delete" @click="remove(a)">
            <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2m2 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
            </svg>
          </button>
        </div>
      </div>
    </div>

    <!-- Define-alert modal -->
    <div v-if="showForm" class="overlay" @click.self="showForm = false">
      <div class="modal">
        <div class="modal-head">
          <h2 class="modal-title">Define an alert</h2>
          <button class="x" @click="showForm = false">✕</button>
        </div>

        <label class="lbl">Name</label>
        <input v-model="f.name" class="fld" placeholder="High CPU on web" />

        <label class="lbl">Condition</label>
        <div class="seg">
          <button :class="{ on: f.kind === 'metric' }" @click="f.kind = 'metric'">Metric threshold</button>
          <button :class="{ on: f.kind === 'container_down' }" @click="f.kind = 'container_down'">Container down</button>
        </div>

        <div v-if="f.kind === 'metric'" class="grid3">
          <select v-model="f.metric" class="fld">
            <option value="cpu">CPU %</option>
            <option value="memory">Memory %</option>
            <option value="disk">Disk %</option>
            <option value="load">Load (1m)</option>
          </select>
          <select v-model="f.op" class="fld">
            <option value=">">is above</option>
            <option value="<">is below</option>
          </select>
          <input v-model.number="f.threshold" type="number" class="fld" />
        </div>
        <div v-else class="grid1">
          <select v-model="f.container" class="fld">
            <option value="" disabled>Select a container…</option>
            <option v-for="c in containers" :key="c" :value="c">{{ c }}</option>
          </select>
        </div>

        <div class="grid2">
          <div>
            <label class="lbl">For (seconds)</label>
            <input v-model.number="f.forSeconds" type="number" class="fld" />
          </div>
          <div>
            <label class="lbl">Severity</label>
            <select v-model="f.severity" class="fld">
              <option value="INFO">Info</option>
              <option value="WARNING">Warning</option>
              <option value="CRITICAL">Critical</option>
            </select>
          </div>
        </div>

        <p v-if="formErr" class="err">{{ formErr }}</p>
        <div class="actions">
          <button class="ghost" @click="showForm = false">Cancel</button>
          <button class="save" :disabled="saving" @click="save">{{ saving ? "Saving…" : "Create alert" }}</button>
        </div>
      </div>
    </div>

    <!-- Live pop-ups -->
    <div class="toasts">
      <div v-for="t in toasts" :key="t.dedup_key" class="toast" :class="t.severity.toLowerCase()">
        <span class="toast-dot"></span>
        <div class="toast-body">
          <div class="toast-title">{{ t.name }}</div>
          <div class="toast-detail">{{ t.root_cause }}</div>
        </div>
        <button class="toast-x" @click="dismiss(t.dedup_key)">✕</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 18px;
  flex-wrap: wrap;
}
.new-alert {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 9px 15px;
  border-radius: 11px;
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  border: 0;
  font-family: var(--pulse-font-mono);
  font-weight: 700;
  font-size: 13px;
  cursor: pointer;
}
.tg {
  position: relative;
  border-radius: 11px;
  overflow: hidden;
  user-select: none;
}
.tg-inner {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  padding: 9px 15px;
  border-radius: 11px;
  background: rgba(41, 169, 235, 0.12);
  border: 1px solid rgba(41, 169, 235, 0.3);
  color: #7ec8f2;
  font-family: var(--pulse-font-mono);
  font-size: 13px;
  filter: blur(1.4px);
  opacity: 0.75;
}
.tg-badge {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--pulse-text);
}
.sec-title {
  font-family: var(--pulse-font-display);
  font-size: 14px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--pulse-text-muted);
  margin: 0 0 10px;
}
.sec-title.mt {
  margin-top: 22px;
}
.list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.state {
  font-size: 11px;
  text-transform: uppercase;
  color: var(--pulse-down);
  letter-spacing: 0.05em;
}
.rule {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.rule-main {
  display: flex;
  align-items: center;
  gap: 12px;
}
.sevdot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
}
.sevdot.info {
  background: var(--pulse-unknown);
}
.sevdot.warning {
  background: var(--pulse-degraded);
}
.sevdot.critical {
  background: var(--pulse-down);
}
.rule-cond {
  font-size: 12.5px;
  color: var(--pulse-text-muted);
  font-family: var(--pulse-font-mono);
}
.rule-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}
.switch {
  width: 40px;
  height: 22px;
  border-radius: 999px;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  position: relative;
  cursor: pointer;
}
.switch .knob {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--pulse-text-muted);
  transition: transform 0.2s, background 0.2s;
}
.switch.on {
  border-color: rgba(199, 245, 66, 0.5);
}
.switch.on .knob {
  transform: translateX(18px);
  background: var(--pulse-accent);
}
.del {
  background: transparent;
  border: 0;
  color: var(--pulse-text-muted);
  cursor: pointer;
  padding: 5px;
  border-radius: 7px;
}
.del:hover {
  color: var(--pulse-down);
  background: rgba(248, 113, 113, 0.1);
}

.overlay {
  position: fixed;
  inset: 0;
  z-index: 60;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(3, 4, 6, 0.6);
  backdrop-filter: blur(4px);
}
.modal {
  width: 100%;
  max-width: 480px;
  border-radius: 18px;
  background: var(--pulse-solid);
  border: 1px solid var(--pulse-border);
  box-shadow: var(--pulse-shadow);
  padding: 22px;
}
.modal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.modal-title {
  font-family: var(--pulse-font-display);
  font-size: 18px;
  font-weight: 700;
  margin: 0;
}
.x {
  background: transparent;
  border: 0;
  color: var(--pulse-text-muted);
  cursor: pointer;
}
.lbl {
  display: block;
  font-size: 12px;
  color: var(--pulse-text-muted);
  margin: 12px 0 6px;
}
.fld {
  width: 100%;
  box-sizing: border-box;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 14px;
  color: var(--pulse-text);
  font-family: var(--pulse-font-mono);
}
.fld:focus {
  outline: none;
  border-color: var(--pulse-accent);
}
.seg {
  display: flex;
  gap: 6px;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  border-radius: 10px;
  padding: 4px;
}
.seg button {
  flex: 1;
  border: 0;
  background: transparent;
  color: var(--pulse-text-muted);
  padding: 7px;
  border-radius: 7px;
  font-family: var(--pulse-font-mono);
  font-size: 12.5px;
  cursor: pointer;
}
.seg button.on {
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  font-weight: 700;
}
.grid3 {
  display: grid;
  grid-template-columns: 1.2fr 1fr 0.8fr;
  gap: 8px;
  margin-top: 8px;
}
.grid1 {
  margin-top: 8px;
}
.grid2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.err {
  color: var(--pulse-down);
  font-size: 13px;
  margin: 12px 0 0;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 18px;
}
.ghost {
  padding: 9px 15px;
  border-radius: 10px;
  background: transparent;
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text-muted);
  cursor: pointer;
  font-family: var(--pulse-font-mono);
  font-size: 13px;
}
.save {
  padding: 9px 15px;
  border-radius: 10px;
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  border: 0;
  font-weight: 700;
  cursor: pointer;
  font-family: var(--pulse-font-mono);
  font-size: 13px;
}
.save:disabled {
  opacity: 0.6;
  cursor: default;
}

.toasts {
  position: fixed;
  right: 20px;
  bottom: 20px;
  z-index: 70;
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-width: 360px;
}
.toast {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 14px 14px;
  border-radius: 13px;
  background: var(--pulse-solid);
  border: 1px solid var(--pulse-border);
  box-shadow: var(--pulse-shadow);
  border-left: 3px solid var(--pulse-degraded);
  animation: slidein 0.25s ease;
}
.toast.critical {
  border-left-color: var(--pulse-down);
}
.toast.info {
  border-left-color: var(--pulse-unknown);
}
.toast-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-top: 5px;
  background: var(--pulse-degraded);
  flex-shrink: 0;
}
.toast.critical .toast-dot {
  background: var(--pulse-down);
}
.toast-body {
  flex: 1;
  min-width: 0;
}
.toast-title {
  font-weight: 600;
  font-size: 13px;
}
.toast-detail {
  font-size: 12px;
  color: var(--pulse-text-muted);
  margin-top: 2px;
}
.toast-x {
  background: transparent;
  border: 0;
  color: var(--pulse-text-muted);
  cursor: pointer;
  font-size: 12px;
}
@keyframes slidein {
  from {
    transform: translateX(20px);
    opacity: 0;
  }
  to {
    transform: translateX(0);
    opacity: 1;
  }
}
</style>
