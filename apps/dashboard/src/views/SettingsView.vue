<script setup lang="ts">
import { ref } from "vue";
import { useAuthStore } from "@/stores/auth";
import { api } from "@/api/client";
import PageHeader from "@/components/PageHeader.vue";

const auth = useAuthStore();
const token = ref("");
const tokenError = ref("");
const generating = ref(false);

async function generateToken() {
  generating.value = true;
  tokenError.value = "";
  try {
    const r = await api.createEnrollmentToken();
    token.value = r.enrollment_token;
  } catch (e) {
    tokenError.value = e instanceof Error ? e.message : "failed";
  } finally {
    generating.value = false;
  }
}

function toggleTheme() {
  document.documentElement.classList.toggle("light");
}
</script>

<template>
  <div>
    <PageHeader title="Settings" />

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-3">
      <div class="card">
        <div class="card-title">Account</div>
        <p class="text-sm">{{ auth.session?.email }}</p>
        <p class="text-sm text-muted">Role: {{ auth.session?.role }}</p>
        <p class="text-xs text-muted mt-2">Permissions: {{ auth.session?.permissions.join(", ") }}</p>
      </div>

      <div class="card">
        <div class="card-title">Enroll a VPS (cloud)</div>
        <p class="text-sm text-muted mb-2">
          Generate a short-lived, single-use enrollment token, then run the installer on the VPS.
        </p>
        <button class="btn btn-primary" :disabled="generating" @click="generateToken">
          {{ generating ? "Generating…" : "Generate enrollment token" }}
        </button>
        <p v-if="tokenError" class="text-sm text-down mt-2">{{ tokenError }}</p>
        <pre v-if="token" class="bg-surface-2 border border-border rounded-md p-3 text-xs mt-2 overflow-x-auto">./installer/install.sh --mode cloud --enrollment-token {{ token }}</pre>
      </div>

      <div class="card">
        <div class="card-title">Appearance</div>
        <button class="btn btn-ghost" @click="toggleTheme">Toggle light / dark</button>
      </div>

      <div class="card">
        <div class="card-title">Safety</div>
        <p class="text-sm text-muted">
          Pulse observes first and changes nothing by default. Config mutation, auto-TLS, remote actions and
          auto-update are all disabled by default and gated behind explicit feature flags.
        </p>
      </div>
    </div>
  </div>
</template>
