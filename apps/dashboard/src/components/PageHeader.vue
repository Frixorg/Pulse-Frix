<script setup lang="ts">
defineProps<{ title: string; subtitle?: string }>();
</script>

<template>
  <div class="mb-5">
    <h1 class="text-lg font-semibold tracking-tight">{{ title }}</h1>
    <p v-if="subtitle" class="text-sm text-muted mt-0.5">{{ subtitle }}</p>
    <div class="mt-3"><slot name="actions" /></div>
  </div>
</template>
