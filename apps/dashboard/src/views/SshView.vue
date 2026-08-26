<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from "vue";
import { storeToRefs } from "pinia";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { useServersStore } from "@/stores/servers";
import { api, sshSocketURL, ApiError } from "@/api/client";
import type { SSHCapabilities, SSHAuthMethod } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";

const servers = useServersStore();
const { selected } = storeToRefs(servers);

// --- capabilities -----------------------------------------------------------

const caps = ref<SSHCapabilities | null>(null);
const capsLoading = ref(true);

async function loadCaps() {
  capsLoading.value = true;
  try {
    caps.value = await api.sshCapabilities();
  } catch {
    caps.value = { enabled: false, reason: "Could not reach the API.", default_port: 22, can_use: false };
  } finally {
    capsLoading.value = false;
  }
}

// --- connection form --------------------------------------------------------

type Phase = "idle" | "connecting" | "connected" | "ended";

const phase = ref<Phase>("idle");
const host = ref("");
const port = ref(22);
const username = ref("root");
const authMethod = ref<SSHAuthMethod>("password");
const password = ref("");
const privateKey = ref("");
const passphrase = ref("");
const remember = ref(true);

const error = ref("");
const errorCode = ref("");
const seenFingerprint = ref(""); // what the API reported on a mismatch
const fingerprint = ref(""); // the live session's host key
const firstConnection = ref(false);

const PROFILE_KEY = "pulse-ssh-profiles";
const PIN_KEY = "pulse-ssh-hostkeys";

interface Profile {
  host: string;
  port: number;
  username: string;
  authMethod: SSHAuthMethod;
}

// Connection details (never secrets) are remembered per server so the second
// visit is one click. Passwords and keys are typed each time, by design.
function readJSON<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
}
function writeJSON(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* quota — the profile is a convenience, not state we depend on */
  }
}

function profileKeyFor(serverId: string) {
  return serverId || "default";
}
function loadProfile() {
  const all = readJSON<Record<string, Profile>>(PROFILE_KEY, {});
  const p = selected.value ? all[profileKeyFor(selected.value.id)] : undefined;
  if (p) {
    host.value = p.host;
    port.value = p.port || 22;
    username.value = p.username || "root";
    authMethod.value = p.authMethod === "key" ? "key" : "password";
    return;
  }
  // Sensible default: the hostname the agent reported for this server.
  host.value = selected.value?.hostname ?? "";
  port.value = 22;
  username.value = "root";
  authMethod.value = "password";
}
function saveProfile() {
  if (!remember.value || !selected.value) return;
  const all = readJSON<Record<string, Profile>>(PROFILE_KEY, {});
  all[profileKeyFor(selected.value.id)] = {
    host: host.value,
    port: port.value,
    username: username.value,
    authMethod: authMethod.value,
  };
  writeJSON(PROFILE_KEY, all);
}

// Host-key pinning: trust on first use, then refuse a changed key until the
// operator explicitly accepts it.
function pinName() {
  return `${host.value}:${port.value}`;
}
function pinnedKey(): string {
  return readJSON<Record<string, string>>(PIN_KEY, {})[pinName()] ?? "";
}
function pinKey(fp: string) {
  const all = readJSON<Record<string, string>>(PIN_KEY, {});
  all[pinName()] = fp;
  writeJSON(PIN_KEY, all);
}
function forgetKey() {
  const all = readJSON<Record<string, string>>(PIN_KEY, {});
  delete all[pinName()];
  writeJSON(PIN_KEY, all);
  errorCode.value = "";
  error.value = "";
}

const canSubmit = computed(() => {
  if (phase.value === "connecting") return false;
  if (!host.value.trim() || !username.value.trim()) return false;
  return authMethod.value === "password" ? password.value.length > 0 : privateKey.value.trim().length > 0;
});

// --- terminal ---------------------------------------------------------------

const termEl = ref<HTMLDivElement | null>(null);
const fullscreen = ref(false);
const fontSize = ref(readJSON<number>("pulse-ssh-fontsize", 13));
const sessionId = ref("");

let term: Terminal | null = null;
let fit: FitAddon | null = null;
let socket: WebSocket | null = null;
let resizeObserver: ResizeObserver | null = null;
const encoder = new TextEncoder();

function cssVar(name: string, fallback: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
}

// The terminal borrows the dashboard palette so it does not look like a
// foreign window pasted into the app.
function themeFor() {
  const light = document.documentElement.classList.contains("light");
  return light
    ? {
        background: "#ffffff",
        foreground: "#12161f",
        cursor: cssVar("--pulse-accent", "#4f7a00"),
        cursorAccent: "#ffffff",
        selectionBackground: "rgba(79,122,0,0.20)",
        black: "#12161f",
        red: "#b91c1c",
        green: "#15803d",
        yellow: "#b45309",
        blue: "#0369a1",
        magenta: "#6d28d9",
        cyan: "#0e7490",
        white: "#e5e7eb",
      }
    : {
        background: "#06070a",
        foreground: "#eaf0f7",
        cursor: cssVar("--pulse-accent", "#c7f542"),
        cursorAccent: "#0a0c05",
        selectionBackground: "rgba(199,245,66,0.25)",
        black: "#0d1017",
        red: "#f87171",
        green: "#34d399",
        yellow: "#fbbf24",
        blue: "#38bdf8",
        magenta: "#a78bfa",
        cyan: "#22d3ee",
        white: "#eaf0f7",
      };
}

function applyTheme() {
  if (term) term.options.theme = themeFor();
}

function refit() {
  if (!fit || !term) return;
  try {
    fit.fit();
  } catch {
    /* the container can be zero-sized mid-transition */
  }
}

function createTerminal() {
  if (!termEl.value) return;
  term = new Terminal({
    fontFamily: '"JetBrains Mono", ui-monospace, SFMono-Regular, monospace',
    fontSize: fontSize.value,
    lineHeight: 1.25,
    cursorBlink: true,
    cursorStyle: "bar",
    scrollback: 10000,
    allowProposedApi: true,
    macOptionIsMeta: true,
    theme: themeFor(),
  });
  fit = new FitAddon();
  term.loadAddon(fit);
  term.loadAddon(new WebLinksAddon());
  term.open(termEl.value);
  refit();

  // Keystrokes go to the remote shell verbatim, as bytes.
  term.onData((data) => {
    if (socket?.readyState === WebSocket.OPEN) socket.send(encoder.encode(data));
  });
  // Tell the remote PTY whenever the window changes, so full-screen programs
  // (vim, htop, less) redraw at the right size.
  term.onResize(({ cols, rows }) => {
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "resize", cols, rows }));
    }
  });

  resizeObserver = new ResizeObserver(() => refit());
  resizeObserver.observe(termEl.value);
  window.addEventListener("pulse-theme", applyTheme);
}

function destroyTerminal() {
  resizeObserver?.disconnect();
  resizeObserver = null;
  window.removeEventListener("pulse-theme", applyTheme);
  term?.dispose();
  term = null;
  fit = null;
}

// The Terminal instance is intentionally not a ref (it owns its own DOM and
// must not be made reactive), so the template calls these wrappers instead.
function clearScreen() {
  term?.clear();
}
function focusTerminal() {
  term?.focus();
}

function writeNotice(text: string) {
  term?.writeln(`\r\n\x1b[38;5;244m${text}\x1b[0m`);
}

// --- connect / disconnect ---------------------------------------------------

async function connect() {
  if (!selected.value || !canSubmit.value) return;
  error.value = "";
  errorCode.value = "";
  seenFingerprint.value = "";
  phase.value = "connecting";

  // The terminal has to exist before we dial so we can send its real size.
  await nextTick();
  if (!term) createTerminal();
  refit();
  const cols = term?.cols ?? 80;
  const rows = term?.rows ?? 24;

  try {
    const session = await api.openSSHSession(selected.value.id, {
      host: host.value.trim(),
      port: Number(port.value) || 22,
      username: username.value.trim(),
      auth_method: authMethod.value,
      password: authMethod.value === "password" ? password.value : undefined,
      private_key: authMethod.value === "key" ? privateKey.value : undefined,
      passphrase: authMethod.value === "key" ? passphrase.value : undefined,
      known_fingerprint: pinnedKey() || undefined,
      cols,
      rows,
    });

    sessionId.value = session.session_id;
    fingerprint.value = session.fingerprint;
    firstConnection.value = session.first_connection && !pinnedKey();
    if (session.fingerprint) pinKey(session.fingerprint);
    saveProfile();
    // The secrets have done their job; drop them from memory and from the DOM.
    password.value = "";
    privateKey.value = "";
    passphrase.value = "";

    openSocket(selected.value.id, session.session_id);
  } catch (e) {
    phase.value = "idle";
    if (e instanceof ApiError) {
      error.value = e.message;
      errorCode.value = e.code;
      seenFingerprint.value = String(e.details?.fingerprint ?? "");
    } else {
      error.value = "Could not open the session.";
    }
  }
}

function openSocket(serverId: string, sid: string) {
  const ws = new WebSocket(sshSocketURL(serverId, sid));
  ws.binaryType = "arraybuffer";
  socket = ws;

  ws.onopen = () => {
    phase.value = "connected";
    nextTick(() => {
      refit();
      term?.focus();
      if (term) ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
    });
  };
  ws.onmessage = (ev) => {
    if (typeof ev.data === "string") {
      // Control channel: status and exit notices.
      try {
        const msg = JSON.parse(ev.data) as { type?: string; message?: string };
        if (msg.type === "exit") writeNotice(msg.message || "session ended");
      } catch {
        /* ignore anything we do not understand */
      }
      return;
    }
    term?.write(new Uint8Array(ev.data as ArrayBuffer));
  };
  ws.onerror = () => {
    if (phase.value === "connecting") error.value = "The console connection failed.";
  };
  ws.onclose = () => {
    socket = null;
    if (phase.value === "connected") {
      writeNotice("— disconnected —");
      phase.value = "ended";
    } else if (phase.value === "connecting") {
      phase.value = "idle";
      if (!error.value) error.value = "The console connection was closed before it opened.";
    }
  };
}

async function disconnect() {
  const sid = sessionId.value;
  const serverId = selected.value?.id;
  socket?.close();
  socket = null;
  if (sid && serverId) await api.closeSSHSession(serverId, sid).catch(() => undefined);
  sessionId.value = "";
  phase.value = "ended";
}

function newSession() {
  phase.value = "idle";
  error.value = "";
  errorCode.value = "";
  term?.clear();
  term?.reset();
}

// --- toolbar ----------------------------------------------------------------

function setFontSize(delta: number) {
  const next = Math.min(22, Math.max(9, fontSize.value + delta));
  fontSize.value = next;
  writeJSON("pulse-ssh-fontsize", next);
  if (term) {
    term.options.fontSize = next;
    refit();
  }
}

async function pasteFromClipboard() {
  try {
    const text = await navigator.clipboard.readText();
    if (text && socket?.readyState === WebSocket.OPEN) socket.send(encoder.encode(text));
  } catch {
    writeNotice("clipboard access was blocked by the browser — use Ctrl+Shift+V");
  }
}

function toggleFullscreen() {
  fullscreen.value = !fullscreen.value;
  nextTick(() => {
    refit();
    focusTerminal();
  });
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape" && fullscreen.value) {
    fullscreen.value = false;
    nextTick(refit);
  }
}

// --- lifecycle --------------------------------------------------------------

onMounted(() => {
  loadCaps();
  loadProfile();
  window.addEventListener("keydown", onKeydown);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  socket?.close();
  socket = null;
  if (sessionId.value && selected.value) {
    api.closeSSHSession(selected.value.id, sessionId.value).catch(() => undefined);
  }
  destroyTerminal();
});

// Switching servers ends the session: a shell on the wrong host is a hazard.
watch(selected, () => {
  if (phase.value === "connected") disconnect();
  loadProfile();
});

const statusLabel = computed(() => {
  switch (phase.value) {
    case "connecting":
      return "Connecting…";
    case "connected":
      return `${username.value}@${host.value}:${port.value}`;
    case "ended":
      return "Disconnected";
    default:
      return "Not connected";
  }
});

const blocked = computed(() => !capsLoading.value && (!caps.value?.enabled || !caps.value?.can_use));
const isViewer = computed(() => caps.value?.enabled === true && caps.value?.can_use === false);
</script>

<template>
  <div :class="{ 'ssh-fs': fullscreen }">
    <PageHeader
      v-if="!fullscreen"
      title="SSH"
      subtitle="A real terminal on your server, in the browser. Pulse's agent is not involved and stays read-only — this is a separate, opt-in SSH connection made with credentials you type here."
    />

    <EmptyState
      v-if="!selected"
      title="No server selected"
      message="Pick a server to open a console on it."
    />

    <div v-else-if="capsLoading" class="card gate">
      <div class="spinner" aria-hidden="true"></div>
      <span class="gate-text">Checking whether the console is available…</span>
    </div>

    <!-- Console unavailable: say exactly what has to change, and by whom. -->
    <div v-else-if="blocked" class="card gate">
      <div class="gate-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8">
          <rect x="3" y="11" width="18" height="10" rx="2" />
          <path d="M7 11V7a5 5 0 0 1 10 0v4" />
        </svg>
      </div>
      <div>
        <h2 class="gate-title">
          {{ isViewer ? "Your role cannot open a console" : "The SSH console is not available" }}
        </h2>
        <p class="gate-text">
          <template v-if="isViewer">
            The console is limited to owners and admins. Viewers keep full read-only access to every
            other page. Ask an owner to change your role if you need shell access.
          </template>
          <template v-else>{{ caps?.reason }}</template>
        </p>
        <div v-if="!isViewer" class="gate-steps">
          <div class="gate-step">
            <span class="gate-n">1</span>
            <div>
              <div class="gate-st">Build the API with an SSH client</div>
              <code class="gate-code">docker compose build --build-arg TAGS=ssh pulse-api</code>
            </div>
          </div>
          <div class="gate-step">
            <span class="gate-n">2</span>
            <div>
              <div class="gate-st">Turn the console on</div>
              <code class="gate-code">PULSE_SSH_CONSOLE=true</code>
            </div>
          </div>
        </div>
        <button class="btn-line" @click="loadCaps">Check again</button>
      </div>
    </div>

    <template v-else>
      <!-- Connection form -->
      <div v-if="phase === 'idle'" class="card form">
        <div class="form-head">
          <h2 class="form-title">New session</h2>
          <span class="form-sub">on {{ selected.hostname || selected.server_id }}</span>
        </div>

        <div class="grid-3">
          <label class="field host-field">
            <span class="lbl">Host or IP</span>
            <input v-model="host" class="input" placeholder="203.0.113.10" autocomplete="off" spellcheck="false" />
          </label>
          <label class="field">
            <span class="lbl">Port</span>
            <input v-model.number="port" class="input" type="number" min="1" max="65535" />
          </label>
          <label class="field">
            <span class="lbl">Username</span>
            <input v-model="username" class="input" placeholder="root" autocomplete="off" spellcheck="false" />
          </label>
        </div>

        <div class="auth-row">
          <div class="seg" role="group" aria-label="Authentication method">
            <button class="seg-b" :class="{ on: authMethod === 'password' }" @click="authMethod = 'password'">
              Password
            </button>
            <button class="seg-b" :class="{ on: authMethod === 'key' }" @click="authMethod = 'key'">
              Private key
            </button>
          </div>
          <label class="remember">
            <input v-model="remember" type="checkbox" />
            Remember host &amp; username on this device
          </label>
        </div>

        <label v-if="authMethod === 'password'" class="field">
          <span class="lbl">Password</span>
          <input
            v-model="password"
            class="input"
            type="password"
            autocomplete="off"
            placeholder="Sent once to open the session — never stored"
            @keyup.enter="connect"
          />
        </label>

        <template v-else>
          <label class="field">
            <span class="lbl">Private key (OpenSSH format)</span>
            <textarea
              v-model="privateKey"
              class="input key"
              rows="6"
              spellcheck="false"
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
            ></textarea>
          </label>
          <label class="field">
            <span class="lbl">Key passphrase <span class="opt">(if the key is encrypted)</span></span>
            <input v-model="passphrase" class="input" type="password" autocomplete="off" @keyup.enter="connect" />
          </label>
        </template>

        <p class="privacy">
          Credentials are used to open this one connection and are never written to the database, the
          logs or an audit record. The audit trail records who connected, to which host, and when.
        </p>

        <!-- Host key changed: this is the one error worth stopping on. -->
        <div v-if="errorCode === 'SSH_HOST_KEY_MISMATCH'" class="alert danger">
          <div class="alert-t">The host key for {{ host }}:{{ port }} has changed</div>
          <p class="alert-d">
            This browser trusted a different key for this host. That happens after a legitimate
            rebuild or reinstall — and it is also what a machine-in-the-middle looks like. Verify the
            new fingerprint on the server itself
            (<code>ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub</code>) before continuing.
          </p>
          <dl class="fp-pair">
            <div><dt>Trusted</dt><dd>{{ pinnedKey() || "—" }}</dd></div>
            <div><dt>Offered now</dt><dd>{{ seenFingerprint || "—" }}</dd></div>
          </dl>
          <button class="btn-line danger" @click="forgetKey">Forget the old key and try again</button>
        </div>
        <div v-else-if="error" class="alert">{{ error }}</div>

        <div class="form-actions">
          <button class="connect" :disabled="!canSubmit" @click="connect">
            <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2.2">
              <path d="M4 17l6-5-6-5M12 19h8" />
            </svg>
            Connect
          </button>
        </div>
      </div>

      <!-- Live console -->
      <div v-show="phase !== 'idle'" class="console" :class="{ fs: fullscreen }">
        <div class="bar">
          <span class="dot" :class="phase"></span>
          <span class="who">{{ statusLabel }}</span>
          <span v-if="fingerprint" class="fp" :title="`Host key ${fingerprint}`">
            {{ firstConnection ? "new host key" : "host key ok" }} · {{ fingerprint.slice(0, 22) }}…
          </span>

          <div class="bar-sp"></div>

          <button class="tool" title="Decrease font size" @click="setFontSize(-1)">A−</button>
          <button class="tool" title="Increase font size" @click="setFontSize(1)">A+</button>
          <button class="tool" title="Clear the screen" :disabled="phase !== 'connected'" @click="clearScreen">
            Clear
          </button>
          <button class="tool" title="Paste from clipboard" :disabled="phase !== 'connected'" @click="pasteFromClipboard">
            Paste
          </button>
          <button class="tool" :title="fullscreen ? 'Exit fullscreen (Esc)' : 'Fullscreen'" @click="toggleFullscreen">
            {{ fullscreen ? "Exit" : "Fullscreen" }}
          </button>
          <button v-if="phase === 'connected'" class="tool danger" @click="disconnect">Disconnect</button>
          <button v-else-if="phase === 'ended'" class="tool accent" @click="newSession">New session</button>
        </div>

        <div class="screen">
          <div ref="termEl" class="xterm-host"></div>
          <div v-if="phase === 'connecting'" class="screen-overlay">
            <div class="spinner" aria-hidden="true"></div>
            <span>Opening a shell on {{ host }}…</span>
          </div>
        </div>

        <div v-if="firstConnection && phase === 'connected'" class="tofu">
          First connection to {{ host }}:{{ port }} — Pulse trusted and pinned this host key:
          <code>{{ fingerprint }}</code>. Verify it on the server with
          <code>ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub</code>.
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
/* Fullscreen lifts the console out of the scrolling page. */
.ssh-fs {
  position: fixed;
  inset: 0;
  z-index: 70;
  display: flex;
  flex-direction: column;
  background: var(--pulse-bg);
  padding: 12px;
}
.ssh-fs .console {
  flex: 1;
  min-height: 0;
}

.gate {
  display: flex;
  gap: 16px;
  align-items: flex-start;
  max-width: 720px;
}
.gate-icon {
  display: grid;
  place-items: center;
  width: 42px;
  height: 42px;
  flex-shrink: 0;
  border-radius: 12px;
  background: var(--pulse-surface-2);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text-muted);
}
.gate-title {
  font-family: var(--pulse-font-display);
  font-size: 16px;
  font-weight: 700;
  margin: 2px 0 6px;
}
.gate-text {
  font-size: 13px;
  line-height: 1.6;
  color: var(--pulse-text-muted);
  margin: 0 0 14px;
}
.gate-steps {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-bottom: 14px;
}
.gate-step {
  display: flex;
  gap: 10px;
  align-items: flex-start;
}
.gate-n {
  display: inline-grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border-radius: 7px;
  background: rgba(199, 245, 66, 0.14);
  color: var(--pulse-accent);
  border: 1px solid rgba(199, 245, 66, 0.3);
  font-size: 12px;
  font-weight: 700;
  flex-shrink: 0;
}
.gate-st {
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 4px;
}
.gate-code {
  display: inline-block;
  font-family: var(--pulse-font-mono);
  font-size: 12px;
  padding: 5px 9px;
  border-radius: 8px;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
}

/* --- form --- */
.form {
  max-width: 760px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.form-head {
  display: flex;
  align-items: baseline;
  gap: 10px;
}
.form-title {
  font-family: var(--pulse-font-display);
  font-size: 16px;
  font-weight: 700;
  margin: 0;
}
.form-sub {
  font-size: 12px;
  color: var(--pulse-text-muted);
}
.grid-3 {
  display: grid;
  grid-template-columns: 1fr 110px 1fr;
  gap: 12px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.lbl {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--pulse-text-muted);
  font-weight: 600;
}
.opt {
  text-transform: none;
  letter-spacing: 0;
  font-weight: 400;
}
.input.key {
  font-family: var(--pulse-font-mono);
  font-size: 12px;
  resize: vertical;
}
.auth-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.seg {
  display: inline-flex;
  gap: 3px;
  padding: 3px;
  border-radius: 11px;
  background: var(--pulse-surface-2);
  border: 1px solid var(--pulse-border);
}
.seg-b {
  border: 0;
  background: transparent;
  color: var(--pulse-text-muted);
  padding: 6px 14px;
  border-radius: 8px;
  font-size: 12.5px;
  font-family: var(--pulse-font-mono);
  cursor: pointer;
}
.seg-b.on {
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  font-weight: 700;
}
.remember {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--pulse-text-muted);
  cursor: pointer;
}
.privacy {
  font-size: 12px;
  line-height: 1.6;
  color: var(--pulse-text-muted);
  margin: 0;
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid var(--pulse-border);
  background: var(--pulse-surface-2);
}
.alert {
  font-size: 12.5px;
  line-height: 1.55;
  padding: 10px 12px;
  border-radius: 10px;
  color: var(--pulse-down);
  background: rgba(248, 113, 113, 0.1);
  border: 1px solid rgba(248, 113, 113, 0.35);
}
.alert-t {
  font-weight: 700;
  margin-bottom: 6px;
}
.alert-d {
  margin: 0 0 10px;
  color: var(--pulse-text);
}
.alert-d code,
.tofu code {
  font-family: var(--pulse-font-mono);
  font-size: 11.5px;
  background: var(--pulse-solid-2);
  padding: 1px 5px;
  border-radius: 5px;
  word-break: break-all;
}
.fp-pair {
  display: grid;
  gap: 6px;
  margin: 0 0 10px;
}
.fp-pair div {
  display: flex;
  gap: 10px;
  align-items: baseline;
}
.fp-pair dt {
  font-size: 10.5px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--pulse-text-muted);
  width: 92px;
  flex-shrink: 0;
}
.fp-pair dd {
  margin: 0;
  font-family: var(--pulse-font-mono);
  font-size: 11.5px;
  color: var(--pulse-text);
  word-break: break-all;
}
.form-actions {
  display: flex;
  justify-content: flex-end;
}
.connect {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 9px 18px;
  border-radius: 10px;
  border: 0;
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  font-family: var(--pulse-font-mono);
  font-weight: 700;
  font-size: 13px;
  cursor: pointer;
}
.connect:disabled {
  opacity: 0.5;
  cursor: default;
}
.btn-line {
  padding: 7px 13px;
  border-radius: 9px;
  background: transparent;
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
  font-family: var(--pulse-font-mono);
  font-size: 12px;
  cursor: pointer;
}
.btn-line:hover {
  background: var(--pulse-surface-2);
}
.btn-line.danger {
  color: #fca5a5;
  border-color: rgba(248, 113, 113, 0.4);
}

/* --- console --- */
.console {
  display: flex;
  flex-direction: column;
  border-radius: 14px;
  border: 1px solid var(--pulse-border);
  background: var(--pulse-solid);
  overflow: hidden;
  box-shadow: var(--pulse-shadow);
}
.console.fs {
  height: 100%;
}
.bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--pulse-border);
  background: var(--pulse-surface-2);
  flex-wrap: wrap;
}
.bar-sp {
  flex: 1;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--pulse-unknown);
  flex-shrink: 0;
}
.dot.connected {
  background: var(--pulse-healthy);
  box-shadow: 0 0 8px var(--pulse-healthy);
}
.dot.connecting {
  background: var(--pulse-degraded);
  animation: ssh-blink 1s ease-in-out infinite;
}
.dot.ended {
  background: var(--pulse-down);
}
.who {
  font-family: var(--pulse-font-mono);
  font-size: 12.5px;
  font-weight: 700;
}
.fp {
  font-family: var(--pulse-font-mono);
  font-size: 10.5px;
  color: var(--pulse-text-muted);
  padding: 2px 8px;
  border-radius: 999px;
  border: 1px solid var(--pulse-border);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 260px;
}
.tool {
  padding: 5px 10px;
  border-radius: 8px;
  border: 1px solid var(--pulse-border);
  background: transparent;
  color: var(--pulse-text-muted);
  font-family: var(--pulse-font-mono);
  font-size: 11.5px;
  cursor: pointer;
  transition: all 0.14s;
}
.tool:hover:not(:disabled) {
  color: var(--pulse-text);
  background: var(--pulse-surface);
}
.tool:disabled {
  opacity: 0.45;
  cursor: default;
}
.tool.danger {
  color: #fca5a5;
  border-color: rgba(248, 113, 113, 0.35);
}
.tool.danger:hover {
  background: rgba(248, 113, 113, 0.12);
  color: #fecaca;
}
.tool.accent {
  color: var(--pulse-accent-ink);
  background: var(--pulse-accent);
  border-color: transparent;
  font-weight: 700;
}
.screen {
  position: relative;
  flex: 1;
  min-height: 0;
  padding: 8px 4px 8px 10px;
  background: var(--pulse-bg);
}
.console:not(.fs) .screen {
  height: clamp(340px, calc(100vh - 330px), 820px);
}
.xterm-host {
  width: 100%;
  height: 100%;
}
.screen-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  background: color-mix(in srgb, var(--pulse-bg) 88%, transparent);
  font-family: var(--pulse-font-mono);
  font-size: 12.5px;
  color: var(--pulse-text-muted);
}
.spinner {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  border: 2px solid var(--pulse-border);
  border-top-color: var(--pulse-accent);
  animation: ssh-spin 0.75s linear infinite;
}
.tofu {
  padding: 9px 12px;
  border-top: 1px solid var(--pulse-border);
  background: rgba(199, 245, 66, 0.07);
  font-size: 11.5px;
  line-height: 1.6;
  color: var(--pulse-text-muted);
}
@keyframes ssh-spin {
  to {
    transform: rotate(360deg);
  }
}
@keyframes ssh-blink {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.3;
  }
}
@media (prefers-reduced-motion: reduce) {
  .spinner {
    animation-duration: 2.4s;
  }
  .dot.connecting {
    animation: none;
  }
}
@media (max-width: 720px) {
  .grid-3 {
    grid-template-columns: 1fr;
  }
  .console:not(.fs) .screen {
    height: clamp(300px, calc(100vh - 380px), 600px);
  }
}
</style>
