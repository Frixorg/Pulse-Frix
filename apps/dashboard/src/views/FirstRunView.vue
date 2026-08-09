<script setup lang="ts">
import { ref } from "vue";

// First-run experience (docs/UI_IA.md#first-run-experience). This is a guided
// explainer; actual enrollment/self-host setup is driven by the installer CLI.
const mode = ref<"choose" | "cloud" | "selfhost">("choose");
const domain = ref("");
</script>

<template>
  <div class="h-full flex items-center justify-center bg-bg text-text p-6">
    <div class="w-full max-w-lg">
      <div class="flex items-center gap-2 mb-6 justify-center">
        <span class="inline-block w-3 h-3 rounded-full bg-healthy"></span>
        <span class="text-xl font-semibold tracking-tight">Welcome to Pulse</span>
      </div>

      <div v-if="mode === 'choose'" class="grid grid-cols-1 gap-3">
        <button class="card text-left hover:border-accent transition-colors" @click="mode = 'cloud'">
          <div class="font-medium">Connect to Pulse Cloud</div>
          <p class="text-sm text-muted mt-1">
            Your VPS dials out to pulse.frix.me. No inbound port is ever opened.
          </p>
        </button>
        <button class="card text-left hover:border-accent transition-colors" @click="mode = 'selfhost'">
          <div class="font-medium">Self-host Pulse</div>
          <p class="text-sm text-muted mt-1">Run the dashboard on your own VPS under your own domain.</p>
        </button>
      </div>

      <div v-else-if="mode === 'cloud'" class="card space-y-3">
        <div class="font-medium">Connect this VPS to Pulse Cloud</div>
        <ol class="text-sm text-muted list-decimal list-inside space-y-1">
          <li>Sign in to your dashboard.</li>
          <li>Generate a short-lived enrollment token (Settings → Agents).</li>
          <li>On the VPS run the installer with the token:</li>
        </ol>
        <pre class="bg-surface-2 border border-border rounded-md p-3 text-xs overflow-x-auto">./installer/install.sh --mode cloud --enrollment-token pst_xxx</pre>
        <div class="flex gap-2">
          <button class="btn btn-ghost" @click="mode = 'choose'">Back</button>
          <RouterLink class="btn btn-primary" to="/login">Continue to sign in</RouterLink>
        </div>
      </div>

      <div v-else class="card space-y-3">
        <div class="font-medium">Self-host Pulse</div>
        <label class="block text-xs text-muted">Domain</label>
        <input
          v-model="domain"
          placeholder="monitor.example.com"
          class="w-full bg-surface-2 border border-border rounded-md px-3 py-2 text-sm"
        />
        <p class="text-xs text-muted">
          Pulse validates DNS and, if Nginx already exists, proposes a previewed,
          validated, reversible config — it never overwrites yours.
        </p>
        <pre class="bg-surface-2 border border-border rounded-md p-3 text-xs overflow-x-auto">./installer/install.sh --mode local --domain {{ domain || "monitor.example.com" }}</pre>
        <div class="flex gap-2">
          <button class="btn btn-ghost" @click="mode = 'choose'">Back</button>
          <RouterLink class="btn btn-primary" to="/login">Continue to sign in</RouterLink>
        </div>
      </div>
    </div>
  </div>
</template>
