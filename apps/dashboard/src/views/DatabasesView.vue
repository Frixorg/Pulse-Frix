<script setup lang="ts">
import ResourceListView from "@/components/ResourceListView.vue";
import { api } from "@/api/client";
</script>

<template>
  <ResourceListView
    title="Databases"
    subtitle="Detected via listening ports and read-only reachability checks. Pulse never modifies database configuration."
    :loader="api.databases"
    :attr-keys="['engine', 'exposure']"
  />
</template>
