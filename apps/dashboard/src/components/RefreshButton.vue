<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from "vue";

// A single, consistent "re-run the check" control. Pulse cannot push a command
// to an agent (agents never accept commands — see docs/SAFETY_MODEL.md), so
// this re-queries the control plane for the freshest data the agent has
// reported. `updatedAt` keeps the user honest about how old that data is.
const props = withDefaults(
  defineProps<{
    loading?: boolean;
    updatedAt?: number | null;
    label?: string;
    busyLabel?: string;
    title?: string;
    variant?: "solid" | "ghost";
    showAge?: boolean;
    disabled?: boolean;
  }>(),
  {
    loading: false,
    updatedAt: null,
    label: "Refresh",
    busyLabel: "Refreshing…",
    title: "Re-run the check and pull the latest data",
    variant: "ghost",
    showAge: true,
    disabled: false,
  },
);

const emit = defineEmits<{ (e: "refresh"): void }>();

// Ticks once a second so "Updated 12s ago" stays truthful without the parent
// having to re-render.
const now = ref(Date.now());
let timer: number | undefined;
onMounted(() => {
  timer = window.setInterval(() => (now.value = Date.now()), 1000);
});
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer);
});

const age = computed(() => {
  if (!props.updatedAt) return "";
  const s = Math.max(0, Math.round((now.value - props.updatedAt) / 1000));
  if (s < 5) return "just now";
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  return `${Math.floor(s / 3600)}h ago`;
});
</script>

<template>
  <div class="rf-wrap">
    <span v-if="showAge && updatedAt" class="rf-age" :class="{ live: loading }">
      {{ loading ? "Checking…" : `Updated ${age}` }}
    </span>
    <button
      class="rf"
      :class="[variant, { busy: loading }]"
      type="button"
      :disabled="loading || disabled"
      :title="title"
      :aria-label="title"
      :aria-busy="loading ? 'true' : 'false'"
      @click="emit('refresh')"
    >
      <svg
        viewBox="0 0 24 24"
        width="15"
        height="15"
        fill="none"
        stroke="currentColor"
        stroke-width="2.1"
        stroke-linecap="round"
        stroke-linejoin="round"
        :class="{ spin: loading }"
        aria-hidden="true"
      >
        <path d="M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6" />
      </svg>
      <span>{{ loading ? busyLabel : label }}</span>
    </button>
  </div>
</template>

<style scoped>
.rf-wrap {
  display: inline-flex;
  align-items: center;
  gap: 10px;
}
.rf-age {
  font-size: 11.5px;
  color: var(--pulse-text-muted);
  font-family: var(--pulse-font-mono);
  white-space: nowrap;
}
.rf-age.live {
  color: var(--pulse-accent);
}
.rf {
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
  transition: background 0.14s, color 0.14s, border-color 0.14s, transform 0.12s;
}
.rf.ghost {
  background: var(--pulse-surface);
  color: var(--pulse-text);
}
.rf.ghost:hover:not(:disabled) {
  background: var(--pulse-surface-2);
  border-color: var(--pulse-accent);
}
.rf.solid {
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  border-color: transparent;
}
.rf.solid:hover:not(:disabled) {
  filter: brightness(1.05);
}
.rf:active:not(:disabled) {
  transform: translateY(1px);
}
.rf:disabled {
  opacity: 0.6;
  cursor: default;
}
.spin {
  animation: rf-spin 0.85s linear infinite;
}
@keyframes rf-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  .spin {
    animation-duration: 2.4s;
  }
}
</style>
