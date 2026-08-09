<script setup lang="ts">
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
</script>

<template>
  <div>
    <PageHeader title="Logs" subtitle="Docker, system service, and monitoring logs — searchable, filterable, timestamped, redacted." />
    <EmptyState
      title="Logs stream coming from the agent"
      message="v1 prioritizes Docker, system service, and monitoring logs (not a large centralized logging platform). Logs are always redacted and escaped — never rendered as raw HTML."
    />
  </div>
</template>
