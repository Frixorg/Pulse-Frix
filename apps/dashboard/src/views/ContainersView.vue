<script setup lang="ts">
import CategorizedResourceView from "@/components/CategorizedResourceView.vue";
import { api } from "@/api/client";
</script>

<template>
  <CategorizedResourceView
    title="Containers"
    subtitle="Live container inventory (read-only), grouped by project. Pulse never modifies your containers to monitor them."
    :loader="api.containers"
    :attr-keys="['image', 'state', 'cpu_percent', 'memory_bytes']"
    storage-key="containers"
  />
</template>
