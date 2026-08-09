<script setup lang="ts">
defineProps<{
  label: string;
  value: string | number;
  suffix?: string;
  percent?: number;
  tone?: "healthy" | "degraded" | "down" | "neutral";
}>();

const barColor: Record<string, string> = {
  healthy: "bg-healthy",
  degraded: "bg-degraded",
  down: "bg-down",
  neutral: "bg-accent",
};
</script>

<template>
  <div class="card">
    <div class="card-title">{{ label }}</div>
    <div class="flex items-baseline gap-1">
      <span class="metric-value">{{ value }}</span>
      <span v-if="suffix" class="text-sm text-muted">{{ suffix }}</span>
    </div>
    <div v-if="percent !== undefined" class="mt-3 h-1.5 rounded-full bg-surface-2 overflow-hidden">
      <div
        class="h-full rounded-full transition-all"
        :class="barColor[tone ?? 'neutral']"
        :style="{ width: Math.min(100, Math.max(0, percent)) + '%' }"
      ></div>
    </div>
  </div>
</template>
