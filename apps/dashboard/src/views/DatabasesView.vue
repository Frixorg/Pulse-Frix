<script setup lang="ts">
import ResourceListView from "@/components/ResourceListView.vue";
import { api } from "@/api/client";
</script>

<template>
  <ResourceListView
    title="Databases"
    subtitle="Postgres, MySQL/MariaDB, Redis, Mongo and more — found by listening ports and container images. Pulse never modifies database configuration."
    :loader="api.databases"
    :attr-keys="['engine', 'image', 'exposure']"
  />
</template>
