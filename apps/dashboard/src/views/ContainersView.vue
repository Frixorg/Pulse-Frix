<script setup lang="ts">
import ResourceListView from "@/components/ResourceListView.vue";
import { api } from "@/api/client";
</script>

<template>
  <ResourceListView
    title="Containers"
    subtitle="Live container inventory (read-only). Pulse never modifies your containers to monitor them."
    :loader="api.containers"
    :attr-keys="['image', 'state', 'cpu_percent', 'memory_bytes']"
  />
</template>
