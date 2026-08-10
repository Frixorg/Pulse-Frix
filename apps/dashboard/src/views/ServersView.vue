<script setup lang="ts">
import { ref } from "vue";
import { storeToRefs } from "pinia";
import { useRouter } from "vue-router";
import { useServersStore } from "@/stores/servers";
import type { Server } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";
import OnboardingConnect from "@/components/OnboardingConnect.vue";
import { timeAgo } from "@/lib/format";

const servers = useServersStore();
const { list, loading } = storeToRefs(servers);
const router = useRouter();

const target = ref<Server | null>(null);
const removing = ref(false);
const err = ref("");
const copied = ref(false);
const showAdd = ref(false);

// Self-contained cleanup: removes only the Pulse agent + its data. Safe — it
// never touches the user's own containers, networks, databases or config.
const cleanupCmd =
  'sudo docker rm -f pulse-agent 2>/dev/null; sudo docker volume rm pulse_pulse-agent-data 2>/dev/null; sudo rm -rf /opt/pulse && echo "Pulse agent removed — your services are untouched."';

function open(id: string) {
  servers.select(id);
  router.push({ name: "server-detail", params: { id } });
}

function copy() {
  navigator.clipboard?.writeText(cleanupCmd).then(() => {
    copied.value = true;
    setTimeout(() => (copied.value = false), 1600);
  });
}

async function removeFromDashboard() {
  if (!target.value) return;
  removing.value = true;
  err.value = "";
  try {
    await servers.remove(target.value.id);
    target.value = null;
    if (servers.list.length === 0) router.push({ name: "dashboard" });
  } catch (e) {
    err.value = e instanceof Error ? e.message : "failed to remove";
  } finally {
    removing.value = false;
  }
}
</script>

<template>
  <div>
    <PageHeader
      title="Servers"
      subtitle="Every VPS connected to Pulse. Removing one first cleans it off the VPS, then clears it from your dashboard."
    >
      <template #actions>
        <button class="add-btn" @click="showAdd = true">
          <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2.2">
            <path d="M12 5v14M5 12h14" />
          </svg>
          Add server
        </button>
      </template>
    </PageHeader>
    <EmptyState
      v-if="!loading && list.length === 0"
      title="No servers yet"
      message="Run the installer on a VPS. In cloud mode the agent dials out and appears here automatically."
    />
    <div v-else class="card overflow-x-auto">
      <table class="table">
        <thead>
          <tr>
            <th>Hostname</th>
            <th>Server ID</th>
            <th>Mode</th>
            <th>Status</th>
            <th>Last seen</th>
            <th class="text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in list" :key="s.id" class="row" @click="open(s.id)">
            <td class="font-medium">{{ s.hostname || "—" }}</td>
            <td class="font-mono text-xs text-muted">{{ s.server_id }}</td>
            <td class="text-muted">{{ s.mode }}</td>
            <td><HealthBadge :status="s.status" /></td>
            <td class="text-muted">{{ timeAgo(s.last_seen_at) }}</td>
            <td class="text-right whitespace-nowrap" @click.stop>
              <button class="rm-btn rm-danger" @click="target = s; err = ''">
                <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2m2 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
                  <path d="M10 11v6M14 11v6" />
                </svg>
                Remove
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Remove flow: clean the VPS, then remove from the dashboard. -->
    <div v-if="target" class="overlay" @click.self="target = null">
      <div class="modal">
        <div class="modal-head">
          <h2 class="modal-title">Remove {{ target.hostname || target.server_id }}</h2>
          <button class="x" aria-label="Close" @click="target = null">✕</button>
        </div>

        <div class="step">
          <span class="step-n">1</span>
          <div class="step-body">
            <div class="step-t">Clean it off the VPS</div>
            <p class="step-d">
              Run this on the server to remove the Pulse agent and its data. It only touches Pulse —
              your containers, databases, Nginx and apps are left untouched.
            </p>
            <button class="code" :class="{ ok: copied }" @click="copy">
              <span class="code-prompt">$</span>
              <span class="code-text">{{ cleanupCmd }}</span>
              <span class="code-copy">{{ copied ? "copied" : "copy" }}</span>
            </button>
          </div>
        </div>

        <div class="step">
          <span class="step-n">2</span>
          <div class="step-body">
            <div class="step-t">Remove it from the dashboard</div>
            <p class="step-d">
              Clears this server and its stored data here. If the agent is still running it will reconnect,
              so run step 1 first.
            </p>
            <p v-if="err" class="err">{{ err }}</p>
            <div class="actions">
              <button class="rm-btn" @click="target = null">Cancel</button>
              <button class="rm-btn rm-confirm" :disabled="removing" @click="removeFromDashboard">
                {{ removing ? "Removing…" : "Remove from dashboard" }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Add-server modal: generate a key + step-by-step commands. -->
    <div v-if="showAdd" class="overlay" @click.self="showAdd = false">
      <div class="modal wide">
        <div class="modal-head">
          <h2 class="modal-title">Add another server</h2>
          <button class="x" aria-label="Close" @click="showAdd = false">✕</button>
        </div>
        <OnboardingConnect />
      </div>
    </div>
  </div>
</template>

<style scoped>
.row {
  cursor: pointer;
  transition: background 0.12s;
}
.row:hover {
  background: var(--pulse-surface-2);
}
.rm-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border-radius: 9px;
  font-size: 12px;
  font-family: var(--pulse-font-mono);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text-muted);
  transition: all 0.14s;
}
.rm-btn:hover {
  color: var(--pulse-text);
  border-color: var(--pulse-text-muted);
}
.rm-btn:disabled {
  opacity: 0.5;
  cursor: default;
}
.rm-danger {
  color: #fca5a5;
  border-color: transparent;
}
.rm-danger:hover {
  color: #fecaca;
  background: rgba(248, 113, 113, 0.12);
  border-color: rgba(248, 113, 113, 0.35);
}
.rm-confirm {
  color: #fca5a5;
  background: rgba(248, 113, 113, 0.14);
  border-color: rgba(248, 113, 113, 0.4);
}
.rm-confirm:hover {
  background: rgba(248, 113, 113, 0.24);
}

.overlay {
  position: fixed;
  inset: 0;
  z-index: 60;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(3, 4, 6, 0.6);
  backdrop-filter: blur(4px);
}
.modal {
  width: 100%;
  max-width: 560px;
  border-radius: 18px;
  background: var(--pulse-solid);
  border: 1px solid var(--pulse-border);
  box-shadow: var(--pulse-shadow);
  padding: 22px;
}
.modal.wide {
  max-width: 720px;
  max-height: 86vh;
  overflow-y: auto;
}
.add-btn {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 8px 14px;
  border-radius: 10px;
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  border: 0;
  font-family: var(--pulse-font-mono);
  font-weight: 700;
  font-size: 13px;
  cursor: pointer;
}
.modal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 18px;
}
.modal-title {
  font-family: var(--pulse-font-display);
  font-size: 18px;
  font-weight: 700;
  margin: 0;
}
.x {
  background: transparent;
  border: 0;
  color: var(--pulse-text-muted);
  cursor: pointer;
  font-size: 14px;
}
.x:hover {
  color: var(--pulse-text);
}
.step {
  display: flex;
  gap: 12px;
  padding: 12px 0;
}
.step + .step {
  border-top: 1px solid var(--pulse-border);
}
.step-n {
  display: inline-grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border-radius: 8px;
  background: rgba(199, 245, 66, 0.14);
  color: var(--pulse-accent);
  border: 1px solid rgba(199, 245, 66, 0.3);
  font-weight: 700;
  font-size: 13px;
  flex-shrink: 0;
}
.step-body {
  flex: 1;
  min-width: 0;
}
.step-t {
  font-weight: 600;
  font-size: 14px;
}
.step-d {
  font-size: 12.5px;
  color: var(--pulse-text-muted);
  margin: 4px 0 10px;
  line-height: 1.55;
}
.code {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  width: 100%;
  padding: 10px 12px;
  border-radius: 10px;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  font-family: var(--pulse-font-mono);
  font-size: 12px;
  color: var(--pulse-text);
  cursor: pointer;
  text-align: left;
  transition: border-color 0.15s;
}
.code:hover {
  border-color: rgba(199, 245, 66, 0.5);
}
.code.ok {
  border-color: var(--pulse-accent);
}
.code-prompt {
  color: var(--pulse-accent);
}
.code-text {
  flex: 1;
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
.code-copy {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--pulse-text-muted);
}
.actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}
.err {
  color: var(--pulse-down);
  font-size: 13px;
  margin: 0 0 8px;
}
</style>
