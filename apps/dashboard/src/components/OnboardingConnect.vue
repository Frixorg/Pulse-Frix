<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from "vue";
import { api } from "@/api/client";
import { useServersStore } from "@/stores/servers";
import PageHeader from "@/components/PageHeader.vue";

const servers = useServersStore();

const token = ref("");
const expiresAt = ref("");
const generating = ref(false);
const genError = ref("");
const copiedId = ref("");
let poll: number | undefined;

// The cloud API is wherever this dashboard is served from.
const apiUrl = window.location.origin;
const repo = "https://github.com/Frixorg/Pulse-Frix.git";

function cloneCmd() {
  return `git clone ${repo} pulse && cd pulse`;
}
function installCmd() {
  const key = token.value || "<your-key>";
  return `sudo PULSE_API_URL=${apiUrl} ./installer/install.sh --mode cloud --enrollment-token ${key}`;
}

async function generate() {
  generating.value = true;
  genError.value = "";
  try {
    const r = await api.createEnrollmentToken();
    token.value = r.enrollment_token;
    expiresAt.value = r.expires_at;
  } catch (e) {
    genError.value = e instanceof Error ? e.message : "Could not generate a key";
  } finally {
    generating.value = false;
  }
}

function copy(text: string, id: string) {
  navigator.clipboard?.writeText(text).then(() => {
    copiedId.value = id;
    setTimeout(() => (copiedId.value = ""), 1600);
  });
}

onMounted(() => {
  // Poll for the first server to appear; the parent view switches to the
  // dashboard automatically once servers.list fills.
  poll = window.setInterval(() => servers.load(), 4000);
});
onBeforeUnmount(() => {
  if (poll) window.clearInterval(poll);
});
</script>

<template>
  <div>
    <PageHeader
      title="Connect your first server"
      subtitle="Two minutes: generate a key, run one command on your VPS, and your infrastructure appears here — read-only, non-destructive."
    />

    <div class="space-y-3 max-w-3xl">
      <!-- Step 1 -->
      <div class="card">
        <div class="flex items-start gap-3">
          <span class="step">1</span>
          <div class="flex-1">
            <div class="font-medium">Generate a tracking key</div>
            <p class="text-sm text-muted mt-0.5">Short-lived and single-use. It links your VPS to this account.</p>

            <div v-if="!token" class="mt-3">
              <button class="btn btn-primary" :disabled="generating" @click="generate">
                {{ generating ? "Generating…" : "Generate key" }}
              </button>
              <p v-if="genError" class="text-sm text-down mt-2">{{ genError }}</p>
            </div>

            <div v-else class="mt-3">
              <div class="code" :class="{ ok: copiedId === 'key' }" @click="copy(token, 'key')">
                <span class="code-text">{{ token }}</span>
                <span class="code-copy">{{ copiedId === 'key' ? 'copied' : 'copy' }}</span>
              </div>
              <p class="text-xs text-muted mt-1">Expires soon — generate a fresh one if it doesn't connect in time.</p>
            </div>
          </div>
        </div>
      </div>

      <!-- Step 2 -->
      <div class="card" :class="{ dim: !token }">
        <div class="flex items-start gap-3">
          <span class="step">2</span>
          <div class="flex-1 min-w-0">
            <div class="font-medium">Run these on the VPS you want to monitor</div>
            <p class="text-sm text-muted mt-0.5">The agent dials out to {{ apiUrl }} — no inbound port is opened.</p>

            <div class="code block mt-3" :class="{ ok: copiedId === 'clone' }" @click="copy(cloneCmd(), 'clone')">
              <span class="code-prompt">$</span><span class="code-text">{{ cloneCmd() }}</span>
              <span class="code-copy">{{ copiedId === 'clone' ? 'copied' : 'copy' }}</span>
            </div>
            <div class="code block mt-2" :class="{ ok: copiedId === 'install' }" @click="copy(installCmd(), 'install')">
              <span class="code-prompt">$</span><span class="code-text">{{ installCmd() }}</span>
              <span class="code-copy">{{ copiedId === 'install' ? 'copied' : 'copy' }}</span>
            </div>
            <p class="text-xs text-muted mt-2">
              Requires Docker. The installer discovers your infrastructure read-only and shows a plan before it changes anything.
            </p>
          </div>
        </div>
      </div>

      <!-- Step 3 -->
      <div class="card" :class="{ dim: !token }">
        <div class="flex items-center gap-3">
          <span class="step">3</span>
          <div class="flex items-center gap-2">
            <span class="spinner"></span>
            <span class="text-sm">Waiting for your server to connect… this page updates automatically.</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.step {
  display: inline-grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--pulse-accent) 15%, transparent);
  color: var(--pulse-accent);
  font-weight: 700;
  font-size: 13px;
  flex-shrink: 0;
}
.card.dim { opacity: 0.6; }
.code {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--pulse-surface-2);
  border: 1px solid var(--pulse-border);
  font-family: "JetBrains Mono", ui-monospace, monospace;
  font-size: 12.5px;
  cursor: pointer;
  transition: border-color 0.15s;
}
.code:hover { border-color: var(--pulse-accent); }
.code.ok { border-color: var(--pulse-healthy); }
.code.block { align-items: flex-start; }
.code-prompt { color: var(--pulse-accent); }
.code-text { flex: 1; overflow-x: auto; white-space: pre; }
.code-copy { font-size: 10px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--pulse-text-muted); }
.spinner {
  width: 16px; height: 16px; border-radius: 50%;
  border: 2px solid var(--pulse-border);
  border-top-color: var(--pulse-accent);
  animation: spin 0.8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }
</style>
