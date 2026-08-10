<script setup lang="ts">
import { ref, watch, computed, onMounted, onBeforeUnmount, nextTick } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import { api } from "@/api/client";
import type { LogEntry } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";

const servers = useServersStore();
const { selected } = storeToRefs(servers);

const sources = ref<string[]>([]);
const source = ref("");
const q = ref("");
const entries = ref<LogEntry[]>([]);
const live = ref(true);
const loading = ref(false);
const copied = ref(false);
const logEl = ref<HTMLElement | null>(null);
let poll: number | undefined;
let searchTimer: number | undefined;

async function scrollToBottom() {
  await nextTick();
  if (logEl.value) logEl.value.scrollTop = logEl.value.scrollHeight;
}

async function load(scroll = true) {
  if (!selected.value) return;
  loading.value = true;
  try {
    const resp = await api.logs(selected.value.id, source.value, q.value);
    sources.value = resp.sources ?? [];
    entries.value = resp.entries ?? [];
    if (scroll && live.value) await scrollToBottom();
  } catch {
    entries.value = [];
  } finally {
    loading.value = false;
  }
}

function setupPoll() {
  if (poll) {
    clearInterval(poll);
    poll = undefined;
  }
  if (live.value) poll = window.setInterval(() => load(true), 4000);
}
function toggleLive() {
  live.value = !live.value;
  setupPoll();
  if (live.value) load(true);
}

function fmtTime(t: string) {
  if (!t) return "";
  const d = new Date(t);
  return isNaN(d.getTime()) ? t : d.toLocaleTimeString();
}

const asText = computed(() => entries.value.map((e) => `${e.time} [${e.source}] ${e.message}`).join("\n"));
function copy() {
  navigator.clipboard?.writeText(asText.value).then(() => {
    copied.value = true;
    setTimeout(() => (copied.value = false), 1500);
  });
}
function exportLogs() {
  const blob = new Blob([asText.value], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `pulse-logs-${source.value || "all"}-${new Date().toISOString().replace(/[:.]/g, "-")}.log`;
  a.click();
  URL.revokeObjectURL(url);
}

onMounted(() => {
  load();
  setupPoll();
});
onBeforeUnmount(() => {
  if (poll) clearInterval(poll);
});
watch([selected, source], () => load(true));
watch(q, () => {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = window.setTimeout(() => load(true), 300);
});
</script>

<template>
  <div>
    <PageHeader
      title="Logs"
      subtitle="Live container logs from the agent — pick a container and watch it stream. Always redacted and escaped, never rendered as HTML."
    />
    <EmptyState v-if="!selected" title="No server selected" />
    <template v-else>
      <div class="controls">
        <select v-model="source" class="ctl">
          <option value="">All containers</option>
          <option v-for="s in sources" :key="s" :value="s">{{ s }}</option>
        </select>
        <input v-model="q" class="ctl search" placeholder="Filter…" spellcheck="false" />
        <button class="ctl live" :class="{ on: live }" @click="toggleLive">
          <span class="live-dot" :class="{ on: live }"></span>
          {{ live ? "Live" : "Paused" }}
        </button>
        <div class="spacer"></div>
        <button class="ctl" :disabled="entries.length === 0" @click="copy">{{ copied ? "Copied" : "Copy" }}</button>
        <button class="ctl" :disabled="entries.length === 0" @click="exportLogs">Export</button>
      </div>

      <div ref="logEl" class="term">
        <div v-if="entries.length === 0" class="term-empty">
          {{ loading ? "Loading…" : "No logs yet — the agent ships container logs every ~20s." }}
        </div>
        <div v-for="(e, i) in entries" :key="i" class="line" :class="{ err: e.stream === 'stderr' }">
          <span class="ts">{{ fmtTime(e.time) }}</span>
          <span v-if="!source" class="src">{{ e.source }}</span>
          <span class="msg">{{ e.message }}</span>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.controls {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}
.ctl {
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
  border-radius: 10px;
  padding: 7px 12px;
  font-size: 13px;
  font-family: var(--pulse-font-mono);
  cursor: pointer;
}
.ctl:disabled {
  opacity: 0.5;
  cursor: default;
}
.search {
  min-width: 200px;
  cursor: text;
}
.live {
  display: inline-flex;
  align-items: center;
  gap: 7px;
}
.live.on {
  border-color: rgba(199, 245, 66, 0.4);
  color: var(--pulse-accent);
}
.live-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--pulse-text-muted);
}
.live-dot.on {
  background: var(--pulse-accent);
  box-shadow: 0 0 8px var(--pulse-accent);
  animation: pulse-dot 1.6s ease-in-out infinite;
}
.spacer {
  flex: 1;
}
.term {
  height: 62vh;
  overflow-y: auto;
  border-radius: 14px;
  background: var(--pulse-solid);
  border: 1px solid var(--pulse-border);
  padding: 12px 14px;
  font-family: var(--pulse-font-mono);
  font-size: 12.5px;
  line-height: 1.55;
}
.term-empty {
  color: var(--pulse-text-muted);
  padding: 20px 4px;
}
.line {
  display: flex;
  gap: 10px;
  white-space: pre-wrap;
  word-break: break-word;
  padding: 1px 0;
}
.line.err .msg {
  color: #fca5a5;
}
.ts {
  color: var(--pulse-text-muted);
  flex-shrink: 0;
  opacity: 0.7;
}
.src {
  color: var(--pulse-accent);
  flex-shrink: 0;
  opacity: 0.85;
}
.msg {
  color: var(--pulse-text);
  flex: 1;
}
</style>
