<script setup lang="ts">
import ResourceListView from "@/components/ResourceListView.vue";
import { api } from "@/api/client";
</script>

<template>
  <ResourceListView
    title="Runtimes"
    subtitle="Language runtimes and app frameworks detected from running processes (Node.js, PHP, Python…). Detection is read-only and never modifies application files."
    :loader="api.applications"
    :attr-keys="['process_count', 'signal']"
  />
</template>
