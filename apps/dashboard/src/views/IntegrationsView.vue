<script setup lang="ts">
import PageHeader from "@/components/PageHeader.vue";

const stack = [
  { name: "Prometheus", role: "Metrics storage + scraping", note: "internal" },
  { name: "node-exporter", role: "Host system metrics", note: "internal" },
  { name: "cAdvisor", role: "Per-container metrics", note: "internal" },
  { name: "Alertmanager", role: "Alert routing / grouping", note: "internal" },
  { name: "Grafana", role: "Visualization engine", note: "internal (not user-facing)" },
];
</script>

<template>
  <div>
    <PageHeader title="Integrations" subtitle="The monitoring stack is isolated on pulse-net and never replaces existing monitoring." />
    <div class="card overflow-x-auto">
      <table class="table">
        <thead><tr><th>Component</th><th>Role</th><th>Exposure</th></tr></thead>
        <tbody>
          <tr v-for="c in stack" :key="c.name">
            <td class="font-medium">{{ c.name }}</td>
            <td class="text-muted">{{ c.role }}</td>
            <td class="text-muted">{{ c.note }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p class="text-xs text-muted mt-3">
      Config-mutating integrations are gated behind <code>ENABLE_CONFIG_MUTATION</code> and follow the
      backup → generate → diff → validate → apply → health-check → rollback pipeline.
    </p>
  </div>
</template>
