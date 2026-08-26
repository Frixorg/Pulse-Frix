<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from "vue";
import { storeToRefs } from "pinia";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { useServersStore } from "@/stores/servers";
import { api, sshSocketURL, sshStreamURL, sshInputURL, ApiError } from "@/api/client";
import type { SSHCapabilities, SSHAuthMethod, SSHSetupResult, SSHSetupStep, Resource } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import CheckBox from "@/components/CheckBox.vue";
import PasswordField from "@/components/PasswordField.vue";

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

// "pulse" means: use the key Pulse installed on this host during setup, so the
// operator types nothing at all.
type AuthMode = "pulse" | "password" | "key";

const phase = ref<Phase>("idle");
const host = ref("");
const port = ref(22);
const username = ref("root");
const authMode = ref<AuthMode>("password");
const password = ref("");
const privateKey = ref("");
const passphrase = ref("");
const remember = ref(true);

// What actually goes on the wire: a Pulse-installed key is still key auth.
const authMethod = computed<SSHAuthMethod>(() => (authMode.value === "password" ? "password" : "key"));

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
  authMethod: AuthMode;
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
    authMode.value = p.authMethod === "key" || p.authMethod === "pulse" ? p.authMethod : "password";
    if (authMode.value === "pulse" && !storedKey.value) authMode.value = "password";
    return;
  }
  // Sensible default: the hostname the agent reported for this server.
  host.value = selected.value?.hostname ?? "";
  port.value = 22;
  username.value = "root";
  authMode.value = "password";
}
function saveProfile() {
  if (!remember.value || !selected.value) return;
  const all = readJSON<Record<string, Profile>>(PROFILE_KEY, {});
  all[profileKeyFor(selected.value.id)] = {
    host: host.value,
    port: port.value,
    username: username.value,
    authMethod: authMode.value,
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

// --- keys Pulse installed on the host -----------------------------------------

const KEYS_KEY = "pulse-ssh-keys";

// The private key Pulse generated during setup lives in this browser only. It
// is what makes later logins one click; a different browser sets up again.
const keyStoreVersion = ref(0); // bumped to re-read localStorage reactively

function keyName() {
  return `${username.value}@${host.value}:${port.value}`;
}
const storedKey = computed(() => {
  keyStoreVersion.value; // dependency
  if (!host.value || !username.value) return "";
  return readJSON<Record<string, string>>(KEYS_KEY, {})[keyName()] ?? "";
});
function saveKey(pem: string) {
  const all = readJSON<Record<string, string>>(KEYS_KEY, {});
  all[keyName()] = pem;
  writeJSON(KEYS_KEY, all);
  keyStoreVersion.value++;
}
function forgetStoredKey() {
  const all = readJSON<Record<string, string>>(KEYS_KEY, {});
  delete all[keyName()];
  writeJSON(KEYS_KEY, all);
  keyStoreVersion.value++;
  if (authMode.value === "pulse") authMode.value = "password";
}
function downloadKey() {
  const blob = new Blob([storedKey.value], { type: "application/x-pem-file" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `pulse-${username.value}-${host.value}.key`;
  a.click();
  URL.revokeObjectURL(url);
}

// Prefer the installed key whenever one exists for these exact details.
watch(storedKey, (key) => {
  if (key && authMode.value === "password" && !password.value) authMode.value = "pulse";
  if (!key && authMode.value === "pulse") authMode.value = "password";
});

const canSubmit = computed(() => {
  if (phase.value === "connecting") return false;
  if (!host.value.trim() || !username.value.trim()) return false;
  switch (authMode.value) {
    case "pulse":
      return storedKey.value.length > 0;
    case "password":
      return password.value.length > 0;
    default:
      return privateKey.value.trim().length > 0;
  }
});

// --- one-click setup ----------------------------------------------------------

const setupOpen = ref(false);
const setupRunning = ref(false);
const setupResult = ref<SSHSetupResult | null>(null);
const setupError = ref("");
const setupSteps = ref<SSHSetupStep[]>([]);
const saveKeyHere = ref(true);

// Read-only facts Pulse already has from discovery — no connection needed, and
// they usually explain why a login is being refused.
const precheck = ref<{ port?: string; passwordAuth?: string; rootLogin?: string; emptyPass?: string } | null>(null);

async function loadPrecheck() {
  precheck.value = null;
  if (!selected.value) return;
  try {
    const snap = await api.discovery(selected.value.id);
    const resources: Resource[] = snap.resources ?? [];
    const cfg = resources.find((r) => r.type === "ssh_config");
    const listener = resources.find(
      (r) => r.type === "listening_port" && String(r.attributes?.process ?? r.name ?? "").includes("ssh"),
    );
    if (!cfg && !listener) return;
    precheck.value = {
      port: listener ? String(listener.attributes?.port ?? "") : "",
      passwordAuth: cfg ? String(cfg.attributes?.password_authentication ?? "") : "",
      rootLogin: cfg ? String(cfg.attributes?.permit_root_login ?? "") : "",
      emptyPass: cfg ? String(cfg.attributes?.permit_empty_passwords ?? "") : "",
    };
  } catch {
    /* the precheck is a bonus; setup does not depend on it */
  }
}

// canSetUp deliberately requires a password or a key the operator typed: Pulse
// has no other way onto the box. The agent is read-only and never runs commands.
const canSetUp = computed(() => {
  if (setupRunning.value || !host.value.trim() || !username.value.trim()) return false;
  return authMode.value === "password" ? password.value.length > 0 : privateKey.value.trim().length > 0;
});

async function runSetup() {
  if (!selected.value || !canSetUp.value) return;
  setupRunning.value = true;
  setupError.value = "";
  setupResult.value = null;
  setupSteps.value = [];
  setupOpen.value = true;
  try {
    const result = await api.setupSSH(selected.value.id, {
      host: host.value.trim(),
      port: Number(port.value) || 22,
      username: username.value.trim(),
      auth_method: authMethod.value,
      password: authMode.value === "password" ? password.value : undefined,
      private_key: authMode.value === "key" ? privateKey.value : undefined,
      passphrase: authMode.value === "key" ? passphrase.value : undefined,
      known_fingerprint: pinnedKey() || undefined,
      cols: 80,
      rows: 24,
    });
    setupResult.value = result;
    setupSteps.value = result.steps ?? [];
    if (result.fingerprint) pinKey(result.fingerprint);
    if (saveKeyHere.value && result.private_key) {
      saveKey(result.private_key);
      authMode.value = "pulse";
      password.value = "";
    }
    saveProfile();
  } catch (e) {
    if (e instanceof ApiError) {
      setupError.value = e.message;
      const steps = e.details?.steps;
      if (Array.isArray(steps)) setupSteps.value = steps as SSHSetupStep[];
    } else {
      setupError.value = "Setup could not run.";
    }
  } finally {
    setupRunning.value = false;
  }
}

// --- terminal ---------------------------------------------------------------

const termEl = ref<HTMLDivElement | null>(null);
const fullscreen = ref(false);
const fontSize = ref(readJSON<number>("pulse-ssh-fontsize", 13));
const sessionId = ref("");

let term: Terminal | null = null;
let fit: FitAddon | null = null;
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
    transport?.sendBytes(encoder.encode(data));
  });
  // Tell the remote PTY whenever the window changes, so full-screen programs
  // (vim, htop, less) redraw at the right size.
  term.onResize(({ cols, rows }) => {
    transport?.sendResize(cols, rows);
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
      password: authMode.value === "password" ? password.value : undefined,
      private_key: authMode.value === "pulse" ? storedKey.value : authMode.value === "key" ? privateKey.value : undefined,
      passphrase: authMode.value === "key" ? passphrase.value : undefined,
      known_fingerprint: pinnedKey() || undefined,
      cols,
      rows,
    });

    sessionId.value = session.session_id;
    fingerprint.value = session.fingerprint;
    firstConnection.value = session.first_connection && !pinnedKey();
    if (session.fingerprint) pinKey(session.fingerprint);
    saveProfile();
    // Secrets are cleared by onTransportOpen, once a transport is actually
    // live — clearing them here would strand a retry with nothing to send.
    connectTransport(selected.value.id, session.session_id);
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

// --- transports ---------------------------------------------------------------
//
// The WebSocket is the fast path. Plenty of deployments sit behind a reverse
// proxy whose Content-Security-Policy looks permissive but omits `connect-src`:
//
//   default-src https: data: blob: 'unsafe-inline' 'unsafe-eval'
//
// `https:` does not match `wss:`, so the browser refuses the socket before it
// leaves the machine, and a CSP set upstream cannot be overridden from here.
// Rather than send every operator off to edit their proxy, the console falls
// back to server-sent events plus ordinary POSTs, which that policy allows.

interface Transport {
  kind: "websocket" | "http";
  sendBytes(bytes: Uint8Array): void;
  sendResize(cols: number, rows: number): void;
  close(): void;
}

let transport: Transport | null = null;
const transportKind = ref<"websocket" | "http">("websocket");

const TRANSPORT_KEY = "pulse-ssh-transport";

// Once a WebSocket has been refused on this deployment, remember it: retrying
// on every connect costs a second and a scary console error each time.
function preferHTTP(): boolean {
  return readJSON<string>(TRANSPORT_KEY, "") === "http";
}

function bytesToBase64(bytes: Uint8Array): string {
  let out = "";
  for (let i = 0; i < bytes.length; i++) out += String.fromCharCode(bytes[i]);
  return btoa(out);
}
function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function onTransportOpen(kind: "websocket" | "http") {
  transportKind.value = kind;
  phase.value = "connected";
  // The secrets have done their job now that a transport is actually live.
  password.value = "";
  privateKey.value = "";
  passphrase.value = "";
  nextTick(() => {
    refit();
    focusTerminal();
    if (term) transport?.sendResize(term.cols, term.rows);
  });
}

function onTransportEnd(notice: string) {
  transport = null;
  if (phase.value === "connected") {
    writeNotice(notice);
    phase.value = "ended";
  }
}

function connectTransport(serverId: string, sid: string) {
  if (preferHTTP()) {
    openHTTPTransport(serverId, sid);
    return;
  }
  openWebSocket(serverId, sid);
}

// --- WebSocket ---

let cspListener: ((e: SecurityPolicyViolationEvent) => void) | null = null;

function stopWatchingCSP() {
  if (cspListener) document.removeEventListener("securitypolicyviolation", cspListener);
  cspListener = null;
}

function openWebSocket(serverId: string, sid: string) {
  let settled = false;
  // Any failure before the socket opens drops straight to the HTTP transport,
  // so a blocked socket never leaves the user staring at "Connecting…".
  const fallback = () => {
    if (settled) return;
    settled = true;
    stopWatchingCSP();
    writeJSON(TRANSPORT_KEY, "http");
    openHTTPTransport(serverId, sid);
  };

  stopWatchingCSP();
  cspListener = (e: SecurityPolicyViolationEvent) => {
    const blocked = e.blockedURI || "";
    if (e.effectiveDirective?.includes("connect-src") || blocked.startsWith("ws")) fallback();
  };
  document.addEventListener("securitypolicyviolation", cspListener);

  let ws: WebSocket;
  try {
    ws = new WebSocket(sshSocketURL(serverId, sid));
  } catch {
    // Some browsers throw here instead of firing a violation event.
    fallback();
    return;
  }
  ws.binaryType = "arraybuffer";

  transport = {
    kind: "websocket",
    sendBytes: (bytes) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(bytes);
    },
    sendResize: (cols, rows) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "resize", cols, rows }));
    },
    close: () => ws.close(),
  };

  ws.onopen = () => {
    settled = true;
    stopWatchingCSP();
    onTransportOpen("websocket");
  };
  ws.onmessage = (ev) => {
    if (typeof ev.data === "string") {
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
  ws.onerror = () => fallback();
  ws.onclose = () => {
    stopWatchingCSP();
    if (!settled) {
      fallback();
      return;
    }
    onTransportEnd("— disconnected —");
  };
}

// --- server-sent events + POST ---

function openHTTPTransport(serverId: string, sid: string) {
  const es = new EventSource(sshStreamURL(serverId, sid));
  let opened = false;
  let finished = false;

  // Keystrokes are coalesced into roughly one POST per frame: a request per
  // character would be pointless traffic, and a fast typist would outrun it.
  let pending: number[] = [];
  let flushTimer: number | undefined;

  const post = (body: unknown) =>
    fetch(sshInputURL(serverId, sid), {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }).catch(() => undefined);

  const flush = () => {
    flushTimer = undefined;
    if (!pending.length) return;
    const bytes = Uint8Array.from(pending);
    pending = [];
    post({ type: "data", data: bytesToBase64(bytes) });
  };

  const finish = (notice: string) => {
    if (finished) return;
    finished = true;
    es.close();
    onTransportEnd(notice);
  };

  transport = {
    kind: "http",
    sendBytes: (bytes) => {
      for (let i = 0; i < bytes.length; i++) pending.push(bytes[i]);
      if (flushTimer === undefined) flushTimer = window.setTimeout(flush, 12);
    },
    sendResize: (cols, rows) => {
      post({ type: "resize", cols, rows });
    },
    close: () => {
      finished = true;
      es.close();
    },
  };

  es.addEventListener("status", () => {
    if (opened) return;
    opened = true;
    onTransportOpen("http");
  });
  es.addEventListener("data", (ev) => {
    term?.write(base64ToBytes((ev as MessageEvent<string>).data));
  });
  es.addEventListener("exit", () => {
    finish("— session ended —");
  });
  es.onerror = () => {
    // EventSource retries on its own, but a console session can be attached
    // only once, so a retry would be refused. Close it and report instead.
    if (!opened) {
      es.close();
      finished = true;
      transport = null;
      phase.value = "idle";
      error.value =
        "Could not open the terminal stream. If Pulse sits behind a reverse proxy, check " +
        "that it does not buffer or time out streaming responses — see docs/SSH_CONSOLE.md.";
      return;
    }
    finish("— connection lost —");
  };
}

async function disconnect() {
  const sid = sessionId.value;
  const serverId = selected.value?.id;
  transport?.close();
  transport = null;
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
    if (text) transport?.sendBytes(encoder.encode(text));
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
  loadPrecheck();
  window.addEventListener("keydown", onKeydown);
});

onBeforeUnmount(() => {
  stopWatchingCSP();
  window.removeEventListener("keydown", onKeydown);
  transport?.close();
  transport = null;
  if (sessionId.value && selected.value) {
    api.closeSSHSession(selected.value.id, sessionId.value).catch(() => undefined);
  }
  destroyTerminal();
});

// Switching servers ends the session: a shell on the wrong host is a hazard.
watch(selected, () => {
  if (phase.value === "connected") disconnect();
  loadProfile();
  loadPrecheck();
  setupResult.value = null;
  setupSteps.value = [];
  setupError.value = "";
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
        <p v-if="!isViewer" class="gate-text">
          The console ships enabled by default, so this deployment has switched it off on purpose.
          Remove <code class="gate-code">PULSE_SSH_CONSOLE=false</code> from the API's environment
          (or drop the <code class="gate-code">nosshconsole</code> build tag) and restart it.
        </p>
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

        <!-- Once setup has run, this host needs no credentials at all. -->
        <div v-if="storedKey" class="key-strip">
          <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
            <path d="M20 6L9 17l-5-5" />
          </svg>
          <span class="key-strip-t">
            Pulse set this host up — <strong>{{ keyName() }}</strong> signs in with a key, no password.
          </span>
          <div class="key-strip-actions">
            <button class="link-btn" @click="downloadKey">Download key</button>
            <button class="link-btn danger" @click="forgetStoredKey">Forget</button>
          </div>
        </div>

        <div class="auth-row">
          <div class="seg" role="group" aria-label="Authentication method">
            <button v-if="storedKey" class="seg-b" :class="{ on: authMode === 'pulse' }" @click="authMode = 'pulse'">
              Pulse key
            </button>
            <button class="seg-b" :class="{ on: authMode === 'password' }" @click="authMode = 'password'">
              Password
            </button>
            <button class="seg-b" :class="{ on: authMode === 'key' }" @click="authMode = 'key'">
              Private key
            </button>
          </div>
          <CheckBox v-model="remember">Remember host &amp; username on this device</CheckBox>
        </div>

        <p v-if="authMode === 'pulse'" class="pulse-auth">
          Using the key Pulse installed on this host. It is stored in this browser only — nothing to type.
        </p>

        <label v-if="authMode === 'password'" class="field">
          <span class="lbl">Password</span>
          <PasswordField
            v-model="password"
            placeholder="Sent once to open the session — never stored"
            @submit="connect"
          />
        </label>

        <template v-else-if="authMode === 'key'">
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
            <PasswordField v-model="passphrase" placeholder="Leave empty if the key has none" @submit="connect" />
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
          <button class="setup-btn" :disabled="!canSetUp" @click="runSetup">
            <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z" />
              <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1A1.7 1.7 0 0 0 9 19.4a1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1A1.7 1.7 0 0 0 4.6 9a1.7 1.7 0 0 0-.3-1.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.9-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
            </svg>
            {{ setupRunning ? "Setting up…" : "Set up SSH on my VPS" }}
          </button>
          <button class="connect" :disabled="!canSubmit" @click="connect">
            <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2.2">
              <path d="M4 17l6-5-6-5M12 19h8" />
            </svg>
            Connect
          </button>
        </div>

        <!-- One-click setup: Pulse signs in once and authorises its own key, so
             every later session needs nothing typed. -->
        <div class="setup" :class="{ open: setupOpen }">
          <button class="setup-head" @click="setupOpen = !setupOpen">
            <span class="setup-caret" :class="{ open: setupOpen }" aria-hidden="true">›</span>
            <span class="setup-title">Set up SSH on my VPS</span>
            <span class="setup-hint">what it does, and what Pulse already sees</span>
          </button>

          <div v-if="setupOpen" class="setup-body">
            <p class="setup-p">
              Pulse signs in once with the details above, then authorises a key it generates for you.
              <strong>You do not run anything on the VPS.</strong> After that, connecting is one click —
              no password.
            </p>

            <ul class="setup-list">
              <li><span class="tick">✓</span> Creates <code>~/.ssh</code> (mode 700) and <code>authorized_keys</code> (600) if missing</li>
              <li><span class="tick">✓</span> Appends one public key — a no-op if it is already there</li>
              <li><span class="tick">✓</span> Restores the SELinux context where that applies</li>
              <li><span class="tick">✓</span> Signs in again with the new key to prove it works</li>
              <li><span class="cross">✗</span> Never edits <code>sshd_config</code>, the firewall, or any other file</li>
            </ul>

            <!-- Read-only facts the agent already reported: usually the answer
                 to "why is it refusing me?". -->
            <div v-if="precheck" class="pre">
              <div class="pre-h">What Pulse already sees on this VPS</div>
              <dl class="pre-grid">
                <div v-if="precheck.port"><dt>sshd port</dt><dd>{{ precheck.port }}</dd></div>
                <div v-if="precheck.passwordAuth">
                  <dt>PasswordAuthentication</dt>
                  <dd :class="{ bad: precheck.passwordAuth === 'no' }">{{ precheck.passwordAuth }}</dd>
                </div>
                <div v-if="precheck.rootLogin">
                  <dt>PermitRootLogin</dt>
                  <dd :class="{ bad: precheck.rootLogin === 'no' }">{{ precheck.rootLogin }}</dd>
                </div>
              </dl>
              <p v-if="precheck.passwordAuth === 'no' && authMode === 'password'" class="pre-warn">
                This host has password logins switched off — use a private key for the one-off setup login.
              </p>
            </div>

            <CheckBox v-model="saveKeyHere">
              Save the key in this browser so future sessions need no password
            </CheckBox>

            <p class="setup-note">
              Pulse needs one working login to do this — it is an SSH client, not an agent command.
              The agent stays read-only and never runs anything. If you cannot log in at all yet, use
              your provider's rescue console once to enable SSH, then come back here.
            </p>

            <button class="setup-run" :disabled="!canSetUp" @click="runSetup">
              {{ setupRunning ? "Setting up…" : "Configure now" }}
            </button>

            <!-- Outcome -->
            <div v-if="setupError" class="alert">{{ setupError }}</div>
            <ol v-if="setupSteps.length" class="steps">
              <li v-for="(st, i) in setupSteps" :key="i" class="step-row" :class="st.status">
                <span class="step-badge">{{ st.status }}</span>
                <span class="step-detail">{{ st.detail }}</span>
              </li>
            </ol>
            <div v-if="setupResult" class="result" :class="{ ok: setupResult.verified }">
              <div class="result-t">
                {{ setupResult.verified ? "Ready — this VPS now signs in with a key" : "Key installed, but the test login did not succeed" }}
              </div>
              <p v-for="(wmsg, i) in setupResult.warnings ?? []" :key="i" class="result-w">{{ wmsg }}</p>
              <code class="result-key">{{ setupResult.public_key }}</code>
            </div>
          </div>
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
          <span
            v-if="phase === 'connected' && transportKind === 'http'"
            class="fp alt"
            title="This browser could not open a WebSocket — most often a reverse-proxy Content-Security-Policy without connect-src. The terminal is running over server-sent events instead, which works but adds a little latency. See docs/SSH_CONSOLE.md."
          >
            http fallback
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
  gap: 10px;
  flex-wrap: wrap;
}

/* --- the key Pulse installed --- */
.key-strip {
  display: flex;
  align-items: center;
  gap: 9px;
  flex-wrap: wrap;
  padding: 9px 12px;
  border-radius: 10px;
  background: rgba(199, 245, 66, 0.08);
  border: 1px solid rgba(199, 245, 66, 0.35);
  color: var(--pulse-accent);
}
.key-strip-t {
  flex: 1;
  min-width: 200px;
  font-size: 12.5px;
  color: var(--pulse-text);
}
.key-strip-actions {
  display: flex;
  gap: 10px;
}
.link-btn {
  background: transparent;
  border: 0;
  padding: 0;
  font-family: var(--pulse-font-mono);
  font-size: 11.5px;
  color: var(--pulse-text-muted);
  cursor: pointer;
  text-decoration: underline;
  text-underline-offset: 3px;
}
.link-btn:hover {
  color: var(--pulse-text);
}
.link-btn.danger:hover {
  color: var(--pulse-down);
}
.pulse-auth {
  margin: 0;
  font-size: 12px;
  color: var(--pulse-text-muted);
}

/* --- setup panel --- */
.setup-btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 9px 16px;
  border-radius: 10px;
  background: var(--pulse-surface-2);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
  font-family: var(--pulse-font-mono);
  font-weight: 700;
  font-size: 13px;
  cursor: pointer;
  margin-right: auto;
}
.setup-btn:hover:not(:disabled) {
  border-color: var(--pulse-accent);
}
.setup-btn:disabled {
  opacity: 0.5;
  cursor: default;
}
.setup {
  border-top: 1px solid var(--pulse-border);
  margin-top: 2px;
  padding-top: 12px;
}
.setup-head {
  display: flex;
  align-items: baseline;
  gap: 9px;
  width: 100%;
  background: transparent;
  border: 0;
  padding: 0;
  cursor: pointer;
  text-align: left;
  color: var(--pulse-text);
}
.setup-caret {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--pulse-text-muted);
}
.setup-caret.open {
  transform: rotate(90deg);
}
.setup-title {
  font-size: 13px;
  font-weight: 600;
}
.setup-hint {
  font-size: 11.5px;
  color: var(--pulse-text-muted);
}
.setup-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px 0 2px 20px;
}
.setup-p {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.65;
  color: var(--pulse-text-muted);
}
.setup-p strong {
  color: var(--pulse-text);
}
.setup-list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12.5px;
  color: var(--pulse-text-muted);
}
.setup-list li {
  display: flex;
  gap: 9px;
  align-items: baseline;
}
.setup-list code {
  font-family: var(--pulse-font-mono);
  font-size: 11.5px;
  background: var(--pulse-solid-2);
  padding: 1px 5px;
  border-radius: 5px;
  color: var(--pulse-text);
}
.tick {
  color: var(--pulse-healthy);
}
.cross {
  color: var(--pulse-down);
}
.pre {
  padding: 11px 13px;
  border-radius: 10px;
  background: var(--pulse-surface-2);
  border: 1px solid var(--pulse-border);
}
.pre-h {
  font-size: 10.5px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--pulse-text-muted);
  font-weight: 600;
  margin-bottom: 8px;
}
.pre-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 24px;
  margin: 0;
}
.pre-grid div {
  display: flex;
  gap: 8px;
  align-items: baseline;
}
.pre-grid dt {
  font-size: 11.5px;
  color: var(--pulse-text-muted);
}
.pre-grid dd {
  margin: 0;
  font-family: var(--pulse-font-mono);
  font-size: 11.5px;
  color: var(--pulse-text);
}
.pre-grid dd.bad {
  color: var(--pulse-degraded);
}
.pre-warn {
  margin: 9px 0 0;
  font-size: 11.5px;
  color: var(--pulse-degraded);
}
.setup-note {
  margin: 0;
  font-size: 11.5px;
  line-height: 1.6;
  color: var(--pulse-text-muted);
}
.setup-run {
  align-self: flex-start;
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
.setup-run:disabled {
  opacity: 0.5;
  cursor: default;
}
.steps {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.step-row {
  display: flex;
  gap: 10px;
  align-items: baseline;
  font-size: 12.5px;
  color: var(--pulse-text);
}
.step-badge {
  flex-shrink: 0;
  width: 62px;
  font-family: var(--pulse-font-mono);
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  text-align: center;
  padding: 2px 0;
  border-radius: 999px;
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text-muted);
}
.step-row.ok .step-badge {
  color: var(--pulse-healthy);
  border-color: rgba(52, 211, 153, 0.4);
}
.step-row.warn .step-badge {
  color: var(--pulse-degraded);
  border-color: rgba(251, 191, 36, 0.4);
}
.step-row.error .step-badge {
  color: var(--pulse-down);
  border-color: rgba(248, 113, 113, 0.4);
}
.result {
  padding: 11px 13px;
  border-radius: 10px;
  border: 1px solid var(--pulse-border);
  background: var(--pulse-surface-2);
}
.result.ok {
  border-color: rgba(199, 245, 66, 0.4);
  background: rgba(199, 245, 66, 0.07);
}
.result-t {
  font-size: 13px;
  font-weight: 700;
  margin-bottom: 6px;
}
.result-w {
  margin: 0 0 8px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--pulse-degraded);
}
.result-key {
  display: block;
  font-family: var(--pulse-font-mono);
  font-size: 10.5px;
  color: var(--pulse-text-muted);
  word-break: break-all;
  line-height: 1.5;
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
.fp.alt {
  color: var(--pulse-degraded);
  border-color: rgba(251, 191, 36, 0.4);
  cursor: help;
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
