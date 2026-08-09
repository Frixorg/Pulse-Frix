<script setup lang="ts">
import ResourceListView from "@/components/ResourceListView.vue";
import { api } from "@/api/client";
</script>

<template>
  <ResourceListView
    title="Applications"
    subtitle="Heuristically detected runtimes and frameworks. Detection never modifies application files."
    :loader="api.applications"
    :attr-keys="['process_count', 'signal']"
  />
</template>
