<script setup lang="ts">
import CategorizedResourceView from "@/components/CategorizedResourceView.vue";
import { api } from "@/api/client";
</script>

<template>
  <CategorizedResourceView
    title="Services"
    subtitle="Every detected service with its health, grouped by project: HEALTHY / DEGRADED / DOWN / UNKNOWN."
    :loader="api.services"
    :attr-keys="['engine', 'image', 'state']"
    storage-key="services"
  />
</template>
