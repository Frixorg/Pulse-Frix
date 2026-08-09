<script setup lang="ts">
import { computed } from "vue";
import type { Health } from "@/api/types";

const props = defineProps<{ status?: Health | string }>();

const map: Record<string, { label: string; cls: string; dot: string }> = {
  HEALTHY: { label: "Healthy", cls: "text-healthy bg-healthy/10", dot: "bg-healthy" },
  DEGRADED: { label: "Degraded", cls: "text-degraded bg-degraded/10", dot: "bg-degraded" },
  DOWN: { label: "Down", cls: "text-down bg-down/10", dot: "bg-down" },
  UNKNOWN: { label: "Unknown", cls: "text-unknown bg-unknown/10", dot: "bg-unknown" },
};

const style = computed(() => map[props.status ?? "UNKNOWN"] ?? map.UNKNOWN);
</script>

<template>
  <span
    class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium"
    :class="style.cls"
  >
    <span class="w-1.5 h-1.5 rounded-full" :class="style.dot"></span>
    {{ style.label }}
  </span>
</template>
