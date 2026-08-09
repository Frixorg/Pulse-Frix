<script setup lang="ts">
import ResourceListView from "@/components/ResourceListView.vue";
import { api } from "@/api/client";
</script>

<template>
  <ResourceListView
    title="Services"
    subtitle="Every detected service with its health: HEALTHY / DEGRADED / DOWN / UNKNOWN."
    :loader="api.services"
    :attr-keys="['engine', 'image', 'state']"
  />
</template>
