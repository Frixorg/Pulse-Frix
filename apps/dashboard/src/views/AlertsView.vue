<script setup lang="ts">
import { ref, onMounted } from "vue";
import { api } from "@/api/client";
import type { AlertInstance } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";
import { timeAgo } from "@/lib/format";

const rows = ref<AlertInstance[]>([]);
const loading = ref(false);

onMounted(async () => {
  loading.value = true;
  try {
    rows.value = (await api.alertInstances()).data ?? [];
  } catch {
    rows.value = [];
  } finally {
    loading.value = false;
  }
});

function sev(s: string) {
  return s === "CRITICAL" ? "DOWN" : s === "WARNING" ? "DEGRADED" : "HEALTHY";
}
</script>

<template>
  <div>
    <PageHeader title="Alerts" subtitle="Debounced, deduplicated, dependency-aware. Root causes are surfaced over symptoms." />

    <div class="bar">
      <button class="new-alert" type="button" disabled>
        + Define an alert
        <span class="soon">soon</span>
      </button>

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

    <EmptyState
      v-if="!loading && rows.length === 0"
      title="No active alerts"
      message="When thresholds are breached you'll see grouped, correlated alerts here — with recovery notifications. Custom alert rules and live pop-ups are coming next."
    />
    <div v-else class="space-y-2">
      <div v-for="a in rows" :key="a.id" class="card flex items-start justify-between">
        <div>
          <div class="flex items-center gap-2">
            <HealthBadge :status="sev(a.severity)" />
            <span class="font-medium">{{ a.name }}</span>
            <span class="text-xs text-muted">{{ a.state }}</span>
          </div>
          <p v-if="a.root_cause" class="text-sm text-muted mt-1">
            Root cause: {{ a.root_cause }}
            <span v-if="a.affected?.length"> — affected: {{ a.affected.join(", ") }}</span>
          </p>
        </div>
        <span class="text-xs text-muted">{{ timeAgo(a.started_at) }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}
.new-alert {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 9px 15px;
  border-radius: 11px;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text-muted);
  font-family: var(--pulse-font-mono);
  font-size: 13px;
  cursor: not-allowed;
}
.soon {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--pulse-accent);
  border: 1px solid rgba(199, 245, 66, 0.3);
  background: rgba(199, 245, 66, 0.1);
  padding: 1px 7px;
  border-radius: 999px;
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
  background: rgba(6, 7, 10, 0.15);
}
</style>
