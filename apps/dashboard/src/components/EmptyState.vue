<script setup lang="ts">
defineProps<{ title: string; message?: string }>();
</script>

<template>
  <div class="card text-center py-12">
    <div class="text-sm font-medium text-text">{{ title }}</div>
    <p v-if="message" class="text-sm text-muted mt-1 max-w-md mx-auto">{{ message }}</p>
    <div class="mt-3"><slot /></div>
  </div>
</template>
