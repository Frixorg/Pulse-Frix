<script setup lang="ts">
import { ref, watch, computed, onMounted, onUnmounted, nextTick } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type {
  SecurityAudit,
  SecurityCategory,
  SecurityCheck,
  SecurityFinding,
  ScanState,
  FindingSeverity,
} from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";

const servers = useServersStore();
const { selected } = storeToRefs(servers);

const audit = ref<SecurityAudit | null>(null);
const loading = ref(false);

// Persistent, merged view of checks + findings across full and per-category runs.
const checkMap = ref<Record<string, SecurityCheck>>({});
const findings = ref<SecurityFinding[]>([]);

// Live scan state.
const scan = ref<ScanState | null>(null);
const scanning = ref(false);
const runningCat = ref<string | null>(null); // category id of an in-flight per-category run
let pollTimer: number | null = null;

const SEVERITIES: FindingSeverity[] = ["CRITICAL", "HIGH", "MEDIUM", "LOW", "INFO"];

function stopPolling() {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer);
    pollTimer = null;
  }
}

function applyScan(s: ScanState) {
  const map = { ...checkMap.value };
  const ran = new Set<string>();
  for (const c of s.checks) {
    map[c.id] = c;
    ran.add(c.id);
  }
  checkMap.value = map;
  findings.value = [...findings.value.filter((f) => !ran.has(f.check_id)), ...s.findings];
}

async function load() {
  stopPolling();
  scanning.value = false;
  scan.value = null;
  runningCat.value = null;
  findings.value = [];
  checkMap.value = {};
  if (!selected.value) return;
  loading.value = true;
  try {
    const a = await api.security(selected.value.id);
    audit.value = a;
    // Seed catalogue statuses, then overlay the latest scan.
    const seed: Record<string, SecurityCheck> = {};
    for (const c of a.checks) seed[c.id] = c;
    checkMap.value = seed;
    if (a.latest) {
      scan.value = a.latest;
      applyScan(a.latest);
    }
  } catch {
    audit.value = null;
  } finally {
    loading.value = false;
  }
}
onMounted(load);
watch(selected, load);
onUnmounted(stopPolling);

async function runScan(mode: "full" | "active" | "passive", categories?: string[]) {
  if (!selected.value || scanning.value) return;
  const sid = selected.value.id;
  scanning.value = true;
  runningCat.value = categories && categories.length === 1 ? categories[0] : null;
  logsOpen.value = true;
  try {
    const { scan_id } = await api.startSecurityScan(sid, { mode, categories });
    stopPolling();
    pollTimer = window.setInterval(async () => {
      try {
        const s = await api.securityScan(sid, scan_id);
        scan.value = s;
        applyScan(s);
        if (s.status === "done" || s.status === "error") {
          stopPolling();
          scanning.value = false;
          runningCat.value = null;
        }
      } catch {
        stopPolling();
        scanning.value = false;
        runningCat.value = null;
      }
    }, 700);
  } catch {
    scanning.value = false;
    runningCat.value = null;
  }
}

// --- derived data ---
const categories = computed<SecurityCategory[]>(() => audit.value?.categories ?? []);
const allChecks = computed(() => Object.values(checkMap.value));

function checksOf(catId: string): SecurityCheck[] {
  return allChecks.value.filter((c) => c.category === catId);
}
function findingsOf(catId: string): SecurityFinding[] {
  return findings.value.filter((f) => f.category === catId);
}

const counts = computed(() => {
  const c: Record<FindingSeverity, number> = { CRITICAL: 0, HIGH: 0, MEDIUM: 0, LOW: 0, INFO: 0 };
  for (const f of findings.value) c[f.severity]++;
  return c;
});
const totalFindings = computed(() => findings.value.length);

// A simple, honest risk grade from the worst-severity findings present.
const grade = computed(() => {
  const c = counts.value;
  if (c.CRITICAL > 0) return { letter: "F", label: "Critical exposure", cls: "g-f" };
  if (c.HIGH > 0) return { letter: "D", label: "High risk", cls: "g-d" };
  if (c.MEDIUM > 2) return { letter: "C", label: "Needs attention", cls: "g-c" };
  if (c.MEDIUM > 0) return { letter: "B", label: "Minor issues", cls: "g-b" };
  if (c.LOW + c.INFO > 0) return { letter: "A", label: "Well hardened", cls: "g-a" };
  return { letter: "A+", label: "No issues found", cls: "g-a" };
});

// --- filters ---
const sevFilter = ref<FindingSeverity | null>(null);
const catFilter = ref<string | null>(null);

function toggleSev(s: FindingSeverity) {
  sevFilter.value = sevFilter.value === s ? null : s;
}
function catName(id: string): string {
  return categories.value.find((c) => c.id === id)?.name ?? id;
}

const visibleCategories = computed(() =>
  categories.value.filter((cat) => {
    if (catFilter.value && cat.id !== catFilter.value) return false;
    return true;
  }),
);

function shownFindings(catId: string): SecurityFinding[] {
  return findingsOf(catId).filter((f) => !sevFilter.value || f.severity === sevFilter.value);
}

function catWorst(catId: string): FindingSeverity | null {
  const fs = findingsOf(catId);
  for (const s of SEVERITIES) if (fs.some((f) => f.severity === s)) return s;
  return null;
}
function catSummary(catId: string) {
  const checks = checksOf(catId);
  const issues = checks.filter((c) => c.status === "issues").length;
  const passed = checks.filter((c) => c.status === "pass").length;
  const skipped = checks.filter((c) => c.status === "skipped").length;
  return { total: checks.length, issues, passed, skipped, findings: findingsOf(catId).length };
}

// --- expand/collapse findings ---
const expanded = ref<Set<string>>(new Set());
function toggle(id: string) {
  const s = new Set(expanded.value);
  s.has(id) ? s.delete(id) : s.add(id);
  expanded.value = s;
}

// --- log console ---
const logsOpen = ref(false);
const logBox = ref<HTMLElement | null>(null);
watch(
  () => scan.value?.logs.length,
  async () => {
    if (!logsOpen.value) return;
    await nextTick();
    if (logBox.value) logBox.value.scrollTop = logBox.value.scrollHeight;
  },
);

function sevCls(s: FindingSeverity) {
  return `sev-${s.toLowerCase()}`;
}
function fmtTime(t?: string) {
  return t ? new Date(t).toLocaleTimeString() : "";
}
function fmtClock(t: string) {
  const d = new Date(t);
  return d.toLocaleTimeString([], { hour12: false }) + "." + String(d.getMilliseconds()).padStart(3, "0");
}
function kindLabel(k: string) {
  return k === "active" ? "active probe" : "passive";
}
function refHost(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}
</script>

<template>
  <div>
    <PageHeader
      title="Security"
      subtitle="Non-destructive assessment — passive checks over discovery data plus safe, read-only probes of your public endpoints. Pulse reports; it never changes your configuration or exploits anything."
    >
      <template #actions>
        <div class="actions">
          <button class="run primary" :disabled="scanning || !selected" @click="runScan('full')">
            <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" :class="{ spin: scanning && !runningCat }">
              <path d="M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6" />
            </svg>
            {{ scanning && !runningCat ? "Scanning…" : "Run full scan" }}
          </button>
          <button class="run ghost" :disabled="scanning || !selected" @click="runScan('passive')" title="Re-run only the passive (no-network) checks">
            Passive re-check
          </button>
        </div>
      </template>
    </PageHeader>

    <EmptyState v-if="!selected" title="No server selected" />
    <template v-else>
      <!-- Live scan progress + log console -->
      <div v-if="scanning || scan" class="progress-card" :class="{ live: scanning }">
        <div class="pc-head">
          <div class="pc-left">
            <span class="pc-dot" :class="{ pulse: scanning }"></span>
            <span class="pc-status">
              <template v-if="scanning">Scanning · {{ scan?.current || "starting…" }}</template>
              <template v-else-if="scan?.status === 'error'">Scan failed</template>
              <template v-else>Last scan complete</template>
            </span>
            <span v-if="scan" class="pc-meta">
              {{ scan.completed }}/{{ scan.total }} checks
              <template v-if="scan.mode"> · {{ scan.mode }}</template>
              <template v-if="scan.targets && scan.targets.length"> · {{ scan.targets.length }} target(s)</template>
            </span>
          </div>
          <button class="pc-toggle" @click="logsOpen = !logsOpen">
            {{ logsOpen ? "Hide log" : "Show log" }}
          </button>
        </div>
        <div class="bar-track">
          <div class="bar-fill" :style="{ width: `${Math.round((scan?.progress ?? 0) * 100)}%` }"></div>
        </div>
        <div v-if="logsOpen" ref="logBox" class="console">
          <div v-for="(l, i) in scan?.logs ?? []" :key="i" class="log" :class="`lvl-${l.level}`">
            <span class="log-t">{{ fmtClock(l.t) }}</span>
            <span class="log-lvl">{{ l.level }}</span>
            <span v-if="l.check" class="log-chk">{{ l.check }}</span>
            <span class="log-msg">{{ l.msg }}</span>
          </div>
          <div v-if="!(scan?.logs?.length)" class="log lvl-info"><span class="log-msg">Waiting for output…</span></div>
        </div>
      </div>

      <!-- Summary -->
      <div v-if="scan" class="summary">
        <div class="grade" :class="grade.cls">
          <span class="grade-letter">{{ grade.letter }}</span>
          <span class="grade-label">{{ grade.label }}</span>
        </div>
        <div class="sev-pills">
          <button
            v-for="s in SEVERITIES"
            :key="s"
            class="sev-pill"
            :class="[sevCls(s), { active: sevFilter === s, muted: counts[s] === 0 }]"
            @click="toggleSev(s)"
          >
            <span class="sp-n">{{ counts[s] }}</span>
            <span class="sp-l">{{ s.toLowerCase() }}</span>
          </button>
          <span class="ran">{{ totalFindings }} findings · last run {{ fmtTime(scan.finished_at || scan.started_at) }}</span>
        </div>
      </div>

      <div v-if="catFilter || sevFilter" class="filter-note">
        Filtering
        <b v-if="catFilter">{{ catName(catFilter) }}</b>
        <b v-if="sevFilter">{{ sevFilter }}</b>
        · <button class="clear" @click="catFilter = null; sevFilter = null">clear</button>
      </div>

      <!-- Category sections -->
      <div class="cats">
        <section v-for="cat in visibleCategories" :key="cat.id" class="cat" :class="catWorst(cat.id) ? sevCls(catWorst(cat.id)!) : 'clean'">
          <header class="cat-head">
            <div class="cat-title-wrap">
              <span class="cat-bar"></span>
              <div>
                <h3 class="cat-title">
                  {{ cat.name }}
                  <span v-if="catSummary(cat.id).findings" class="cat-count">{{ catSummary(cat.id).findings }}</span>
                  <span v-else class="cat-ok">clear</span>
                </h3>
                <p class="cat-desc">{{ cat.description }}</p>
              </div>
            </div>
            <button class="cat-run" :disabled="scanning" :title="`Run ${cat.name} checks`" @click="runScan('full', [cat.id])">
              <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2.2" :class="{ spin: runningCat === cat.id }">
                <path d="M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6" />
              </svg>
              {{ runningCat === cat.id ? "Running" : "Run" }}
            </button>
          </header>

          <!-- Checks in this category -->
          <div class="chk-row">
            <span
              v-for="c in checksOf(cat.id)"
              :key="c.id"
              class="chk"
              :class="`st-${c.status}`"
              :title="`${c.description} · ${kindLabel(c.kind)}${c.duration_ms ? ' · ' + c.duration_ms + 'ms' : ''}`"
            >
              <span class="chk-dot"></span>
              <span class="chk-name">{{ c.name }}</span>
              <span class="chk-kind">{{ c.kind === "active" ? "◉" : "○" }}</span>
              <span v-if="c.status === 'issues'" class="chk-n">{{ c.count }}</span>
              <span v-else-if="c.status === 'pass'" class="chk-ok">✓</span>
              <span v-else-if="c.status === 'skipped'" class="chk-sk">skipped</span>
              <span v-else-if="c.status === 'error'" class="chk-er">error</span>
              <span v-else class="chk-nr">—</span>
            </span>
          </div>

          <!-- Findings in this category -->
          <div v-if="shownFindings(cat.id).length" class="findings">
            <div v-for="f in shownFindings(cat.id)" :key="f.id" class="finding" :class="sevCls(f.severity)">
              <button class="f-head" @click="toggle(f.id)">
                <span class="sev" :class="sevCls(f.severity)">{{ f.severity }}</span>
                <span class="f-title">{{ f.title }}</span>
                <span v-if="f.cvss" class="f-cvss">CVSS {{ f.cvss.toFixed(1) }}</span>
                <span v-if="f.resource" class="f-res">{{ f.resource }}</span>
                <svg class="f-caret" :class="{ open: expanded.has(f.id) }" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 9l6 6 6-6" /></svg>
              </button>
              <div v-show="expanded.has(f.id)" class="f-body">
                <div class="f-tags">
                  <span v-if="f.owasp" class="tag owasp">{{ f.owasp }}</span>
                  <span v-if="f.cwe" class="tag cwe">{{ f.cwe }}</span>
                </div>
                <p class="f-detail">{{ f.detail }}</p>
                <div v-if="f.evidence" class="f-evidence"><span class="ev-k">evidence</span><code>{{ f.evidence }}</code></div>
                <p class="f-rec"><span class="f-rec-k">Remediation</span> {{ f.recommendation }}</p>
                <div v-if="f.references && f.references.length" class="f-refs">
                  <a v-for="(r, i) in f.references" :key="i" :href="r" target="_blank" rel="noopener noreferrer">{{ refHost(r) }} ↗</a>
                </div>
              </div>
            </div>
          </div>
          <div v-else-if="catSummary(cat.id).findings && sevFilter" class="cat-empty">
            No {{ sevFilter?.toLowerCase() }} findings in this category.
          </div>
          <div v-else-if="!catSummary(cat.id).findings" class="cat-empty ok">
            <span class="ce-badge">✓</span> All {{ catSummary(cat.id).total }} checks passed — nothing to flag.
            <span v-if="catSummary(cat.id).skipped" class="ce-note">({{ catSummary(cat.id).skipped }} skipped — no public target)</span>
          </div>
        </section>
      </div>
    </template>
  </div>
</template>

<style scoped>
.actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.run {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 8px 14px;
  border-radius: 10px;
  font-family: var(--pulse-font-mono);
  font-weight: 700;
  font-size: 13px;
  cursor: pointer;
  border: 1px solid var(--pulse-border);
}
.run.primary {
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  border: 0;
}
.run.ghost {
  background: var(--pulse-surface);
  color: var(--pulse-text);
}
.run:disabled {
  opacity: 0.55;
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

/* Progress card + console */
.progress-card {
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
  border-radius: 14px;
  padding: 14px 16px;
  margin-bottom: 16px;
}
.progress-card.live {
  border-color: rgba(199, 245, 66, 0.4);
}
.pc-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 10px;
}
.pc-left {
  display: flex;
  align-items: center;
  gap: 9px;
  flex-wrap: wrap;
}
.pc-dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--pulse-accent);
  flex-shrink: 0;
}
.pc-dot.pulse {
  animation: pulse 1.1s ease-in-out infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.35; transform: scale(0.8); }
}
.pc-status {
  font-weight: 600;
  font-size: 13.5px;
}
.pc-meta {
  font-family: var(--pulse-font-mono);
  font-size: 11.5px;
  color: var(--pulse-text-muted);
}
.pc-toggle {
  background: transparent;
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text-muted);
  border-radius: 8px;
  padding: 4px 10px;
  font-size: 11.5px;
  cursor: pointer;
}
.bar-track {
  height: 6px;
  border-radius: 999px;
  background: var(--pulse-surface-2);
  overflow: hidden;
}
.bar-fill {
  height: 100%;
  background: var(--pulse-accent);
  border-radius: 999px;
  transition: width 0.4s ease;
}
.console {
  margin-top: 12px;
  max-height: 220px;
  overflow-y: auto;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  border-radius: 10px;
  padding: 8px 10px;
  font-family: var(--pulse-font-mono);
  font-size: 11.5px;
  line-height: 1.7;
}
.log {
  display: flex;
  gap: 8px;
  white-space: nowrap;
}
.log-t {
  color: var(--pulse-text-muted);
  opacity: 0.7;
  flex-shrink: 0;
}
.log-lvl {
  flex-shrink: 0;
  width: 52px;
  text-transform: uppercase;
  font-size: 10px;
  letter-spacing: 0.04em;
  opacity: 0.9;
}
.log-chk {
  flex-shrink: 0;
  color: var(--pulse-text-muted);
}
.log-msg {
  white-space: normal;
}
.lvl-info .log-lvl { color: var(--pulse-text-muted); }
.lvl-success .log-lvl, .lvl-success .log-msg { color: var(--pulse-accent); }
.lvl-warn .log-lvl, .lvl-warn .log-msg { color: var(--pulse-degraded); }
.lvl-error .log-lvl, .lvl-error .log-msg { color: var(--pulse-down); }

/* Summary */
.summary {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}
.grade {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 16px 10px 12px;
  border-radius: 12px;
  border: 1px solid var(--pulse-border);
  background: var(--pulse-surface);
}
.grade-letter {
  font-size: 26px;
  font-weight: 800;
  font-family: var(--pulse-font-mono);
  line-height: 1;
}
.grade-label {
  font-size: 12.5px;
  color: var(--pulse-text-muted);
}
.g-a .grade-letter { color: var(--pulse-accent); }
.g-b .grade-letter { color: #7dd3fc; }
.g-c .grade-letter { color: var(--pulse-degraded); }
.g-d .grade-letter { color: #fb923c; }
.g-f .grade-letter { color: var(--pulse-down); }
.sev-pills {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.sev-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 11px;
  border-radius: 999px;
  border: 1px solid var(--pulse-border);
  background: var(--pulse-surface);
  cursor: pointer;
  font-family: var(--pulse-font-mono);
}
.sev-pill.active {
  box-shadow: 0 0 0 2px var(--pulse-accent) inset;
}
.sev-pill.muted {
  opacity: 0.5;
}
.sp-n {
  font-weight: 800;
  font-size: 13px;
}
.sp-l {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--pulse-text-muted);
}
.sev-critical .sp-n, .sev-pill.sev-critical .sp-n { color: #fca5a5; }
.sev-high .sp-n { color: #fb923c; }
.sev-medium .sp-n { color: var(--pulse-degraded); }
.sev-low .sp-n { color: #7dd3fc; }
.sev-info .sp-n { color: var(--pulse-text-muted); }
.ran {
  font-size: 11.5px;
  color: var(--pulse-text-muted);
  margin-left: 4px;
}
.filter-note {
  font-size: 12.5px;
  color: var(--pulse-text-muted);
  margin-bottom: 12px;
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}
.filter-note b {
  color: var(--pulse-text);
}
.clear {
  background: none;
  border: 0;
  color: var(--pulse-accent);
  cursor: pointer;
  font-size: 12.5px;
}

/* Category sections */
.cats {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.cat {
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
  border-radius: 14px;
  padding: 16px;
}
.cat-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.cat-title-wrap {
  display: flex;
  gap: 12px;
}
.cat-bar {
  width: 3px;
  border-radius: 2px;
  background: var(--pulse-border);
  flex-shrink: 0;
}
.cat.sev-critical .cat-bar { background: var(--pulse-down); }
.cat.sev-high .cat-bar { background: #fb923c; }
.cat.sev-medium .cat-bar { background: var(--pulse-degraded); }
.cat.sev-low .cat-bar { background: #7dd3fc; }
.cat.sev-info .cat-bar { background: var(--pulse-unknown); }
.cat.clean .cat-bar { background: var(--pulse-accent); }
.cat-title {
  font-size: 14.5px;
  font-weight: 650;
  margin: 0 0 3px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.cat-count {
  font-family: var(--pulse-font-mono);
  font-size: 11px;
  font-weight: 700;
  color: #fca5a5;
  background: rgba(248, 113, 113, 0.12);
  padding: 1px 8px;
  border-radius: 999px;
}
.cat-ok {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--pulse-accent);
}
.cat-desc {
  font-size: 12.5px;
  color: var(--pulse-text-muted);
  margin: 0;
  line-height: 1.5;
  max-width: 62ch;
}
.cat-run {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: var(--pulse-surface-2);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
  border-radius: 8px;
  padding: 6px 11px;
  font-size: 12px;
  font-family: var(--pulse-font-mono);
  cursor: pointer;
  flex-shrink: 0;
}
.cat-run:disabled {
  opacity: 0.5;
  cursor: default;
}

/* Check chips */
.chk-row {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
  margin-bottom: 12px;
}
.chk {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 6px 10px;
  border-radius: 9px;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  font-family: var(--pulse-font-mono);
  font-size: 11.5px;
}
.chk-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--pulse-unknown);
  flex-shrink: 0;
}
.st-pass .chk-dot { background: var(--pulse-accent); }
.st-issues .chk-dot { background: var(--pulse-down); }
.st-skipped .chk-dot { background: var(--pulse-unknown); }
.st-error .chk-dot { background: var(--pulse-degraded); }
.st-not_run .chk-dot { background: var(--pulse-border); }
.chk-kind {
  font-size: 9px;
  color: var(--pulse-text-muted);
  opacity: 0.7;
}
.chk-n {
  font-weight: 700;
  color: #fca5a5;
}
.chk-ok { color: var(--pulse-accent); }
.chk-sk, .chk-nr {
  font-size: 10px;
  color: var(--pulse-text-muted);
}
.chk-er {
  font-size: 10px;
  color: var(--pulse-degraded);
}

/* Findings */
.findings {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.finding {
  border-radius: 11px;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  border-left: 3px solid var(--pulse-border);
  overflow: hidden;
}
.finding.sev-critical { border-left-color: var(--pulse-down); }
.finding.sev-high { border-left-color: #fb923c; }
.finding.sev-medium { border-left-color: var(--pulse-degraded); }
.finding.sev-low { border-left-color: #7dd3fc; }
.finding.sev-info { border-left-color: var(--pulse-unknown); }
.f-head {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 11px 13px;
  background: transparent;
  border: 0;
  cursor: pointer;
  text-align: left;
  color: var(--pulse-text);
  flex-wrap: wrap;
}
.sev {
  font-size: 9.5px;
  font-weight: 800;
  letter-spacing: 0.05em;
  padding: 2px 7px;
  border-radius: 999px;
  flex-shrink: 0;
}
.sev.sev-critical { color: #fca5a5; background: rgba(248, 113, 113, 0.14); }
.sev.sev-high { color: #fb923c; background: rgba(251, 146, 60, 0.14); }
.sev.sev-medium { color: var(--pulse-degraded); background: rgba(251, 191, 36, 0.14); }
.sev.sev-low { color: #7dd3fc; background: rgba(125, 211, 252, 0.14); }
.sev.sev-info { color: var(--pulse-text-muted); background: var(--pulse-surface-2); }
.f-title {
  font-weight: 600;
  font-size: 13.5px;
  flex: 1;
}
.f-cvss {
  font-family: var(--pulse-font-mono);
  font-size: 10.5px;
  color: var(--pulse-text-muted);
  border: 1px solid var(--pulse-border);
  padding: 1px 7px;
  border-radius: 6px;
}
.f-res {
  font-family: var(--pulse-font-mono);
  font-size: 10.5px;
  color: var(--pulse-text-muted);
  background: var(--pulse-surface-2);
  padding: 2px 7px;
  border-radius: 6px;
  max-width: 240px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.f-caret {
  color: var(--pulse-text-muted);
  transition: transform 0.2s;
  flex-shrink: 0;
}
.f-caret.open {
  transform: rotate(180deg);
}
.f-body {
  padding: 0 13px 13px 13px;
}
.f-tags {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}
.tag {
  font-size: 10.5px;
  font-family: var(--pulse-font-mono);
  padding: 2px 8px;
  border-radius: 6px;
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text-muted);
}
.tag.owasp { color: var(--pulse-accent); border-color: rgba(199, 245, 66, 0.3); }
.f-detail {
  font-size: 13px;
  color: var(--pulse-text-muted);
  line-height: 1.6;
  margin: 0 0 8px;
}
.f-evidence {
  display: flex;
  gap: 8px;
  align-items: baseline;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
  border-radius: 8px;
  padding: 7px 10px;
  margin-bottom: 8px;
  overflow-x: auto;
}
.ev-k {
  font-size: 9.5px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--pulse-text-muted);
  flex-shrink: 0;
}
.f-evidence code {
  font-family: var(--pulse-font-mono);
  font-size: 11.5px;
  color: var(--pulse-text);
  white-space: pre;
}
.f-rec {
  font-size: 13px;
  line-height: 1.55;
  margin: 0;
}
.f-rec-k {
  color: var(--pulse-accent);
  font-weight: 700;
  margin-right: 6px;
}
.f-refs {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin-top: 8px;
}
.f-refs a {
  font-size: 11.5px;
  color: var(--pulse-text-muted);
  text-decoration: none;
}
.f-refs a:hover {
  color: var(--pulse-accent);
}
.cat-empty {
  font-size: 12.5px;
  color: var(--pulse-text-muted);
  padding: 4px 2px;
}
.cat-empty.ok {
  display: flex;
  align-items: center;
  gap: 8px;
}
.ce-badge {
  display: inline-grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border-radius: 7px;
  background: rgba(199, 245, 66, 0.14);
  color: var(--pulse-accent);
  border: 1px solid rgba(199, 245, 66, 0.3);
  font-size: 12px;
}
.ce-note {
  opacity: 0.7;
}
</style>
