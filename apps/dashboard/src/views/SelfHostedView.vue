<script setup lang="ts">
import { ref, computed } from "vue";
import { RouterLink } from "vue-router";
import AtmosphereBg from "@/components/AtmosphereBg.vue";

const domain = ref("");
const copied = ref("");

const dom = computed(() => domain.value.trim() || "pulse.your-domain.com");

const cloneCmd = "git clone https://github.com/Frixorg/Pulse-Frix.git pulse && cd pulse";
const installCmd = computed(() => `sudo bash installer/install.sh --mode local --domain ${dom.value}`);

function copy(text: string, id: string) {
  navigator.clipboard?.writeText(text).then(() => {
    copied.value = id;
    setTimeout(() => (copied.value = ""), 1600);
  });
}
</script>

<template>
  <div class="sh">
    <AtmosphereBg />
    <div class="wrap">
      <header class="head">
        <RouterLink to="/" class="brand">
          <span class="brand-dot"></span><span class="brand-name">Pulse</span>
        </RouterLink>
        <RouterLink to="/login" class="back">← Back to sign in</RouterLink>
      </header>

      <section class="hero">
        <span class="eyebrow">// self-hosted</span>
        <h1 class="h1">Run Pulse on your own VPS</h1>
        <p class="lede">
          Your box, your domain, your data — nothing leaves the machine. Clone the repo, run one command,
          point a domain, and sign in with the credentials the installer prints. Discovery is read-only and
          non-destructive by default.
        </p>
      </section>

      <div class="field">
        <label class="field-l">Your domain (optional — used to fill the commands)</label>
        <input v-model="domain" class="field-i" placeholder="pulse.your-domain.com" spellcheck="false" />
      </div>

      <ol class="steps">
        <li class="step">
          <span class="n">1</span>
          <div class="body">
            <h3>Point a domain at your VPS</h3>
            <p>Create a DNS <b>A record</b> for <code>{{ dom }}</code> pointing to your server's public IP. Give it a minute to propagate.</p>
          </div>
        </li>

        <li class="step">
          <span class="n">2</span>
          <div class="body">
            <h3>Clone Pulse on the VPS</h3>
            <p>SSH into your server, then:</p>
            <button class="cmd" :class="{ ok: copied === 'clone' }" @click="copy(cloneCmd, 'clone')">
              <span class="p">$</span><span class="t">{{ cloneCmd }}</span>
              <span class="c">{{ copied === 'clone' ? 'copied' : 'copy' }}</span>
            </button>
          </div>
        </li>

        <li class="step">
          <span class="n">3</span>
          <div class="body">
            <h3>Run the self-hosted installer</h3>
            <p>It discovers your infrastructure read-only, shows a plan, then brings up the full stack (dashboard, API, database, monitoring) on an isolated network — nothing existing is touched. Requires Docker.</p>
            <button class="cmd" :class="{ ok: copied === 'install' }" @click="copy(installCmd, 'install')">
              <span class="p">$</span><span class="t">{{ installCmd }}</span>
              <span class="c">{{ copied === 'install' ? 'copied' : 'copy' }}</span>
            </button>
            <p class="note">
              When it finishes, the installer prints an <b>admin email and password</b> once — copy them somewhere safe.
            </p>
          </div>
        </li>

        <li class="step">
          <span class="n">4</span>
          <div class="body">
            <h3>Open your dashboard &amp; sign in</h3>
            <p>Visit <code>https://{{ dom }}</code> and sign in with the admin email + password from the previous step. That's it — your infrastructure appears, read-only.</p>
            <a :href="`https://${dom}`" target="_blank" rel="noopener noreferrer" class="open-btn">
              Open https://{{ dom }}
            </a>
          </div>
        </li>
      </ol>

      <p class="foot">
        Prefer the hosted version? <RouterLink to="/login" class="link">Sign in to Pulse Cloud</RouterLink> and run one agent command instead.
      </p>
    </div>
  </div>
</template>

<style scoped>
.sh {
  position: relative;
  min-height: 100%;
  background: var(--pulse-bg);
  color: var(--pulse-text);
  font-family: var(--pulse-font-mono);
  overflow-y: auto;
}
.wrap {
  position: relative;
  z-index: 1;
  max-width: 760px;
  margin: 0 auto;
  padding: 22px 24px 60px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 44px;
}
.brand {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  text-decoration: none;
  color: var(--pulse-text);
}
.brand-dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--pulse-accent);
  box-shadow: 0 0 12px var(--pulse-accent);
}
.brand-name {
  font-family: var(--pulse-font-display);
  font-weight: 700;
  font-size: 18px;
}
.back {
  font-size: 13px;
  color: var(--pulse-text-muted);
  text-decoration: none;
}
.back:hover {
  color: var(--pulse-text);
}
.hero {
  padding: 30px 0 20px;
}
.eyebrow {
  font-size: 12px;
  color: var(--pulse-accent);
  letter-spacing: 0.06em;
}
.h1 {
  font-family: var(--pulse-font-display);
  font-weight: 700;
  font-size: clamp(30px, 5vw, 46px);
  letter-spacing: -0.03em;
  margin: 12px 0 0;
}
.lede {
  color: var(--pulse-text-muted);
  font-size: 15px;
  line-height: 1.7;
  margin: 16px 0 0;
}
.field {
  margin: 22px 0 8px;
}
.field-l {
  display: block;
  font-size: 12px;
  color: var(--pulse-text-muted);
  margin-bottom: 8px;
}
.field-i {
  width: 100%;
  box-sizing: border-box;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  border-radius: 11px;
  padding: 12px 14px;
  font-family: var(--pulse-font-mono);
  font-size: 15px;
  color: var(--pulse-text);
  transition: border-color 0.15s;
}
.field-i:focus {
  outline: none;
  border-color: var(--pulse-accent);
}
.steps {
  list-style: none;
  margin: 20px 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.step {
  display: flex;
  gap: 14px;
  padding: 20px;
  border-radius: 16px;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
}
.n {
  display: inline-grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 10px;
  background: rgba(199, 245, 66, 0.14);
  color: var(--pulse-accent);
  border: 1px solid rgba(199, 245, 66, 0.3);
  font-family: var(--pulse-font-display);
  font-weight: 700;
  flex-shrink: 0;
}
.body {
  flex: 1;
  min-width: 0;
}
.body h3 {
  font-family: var(--pulse-font-display);
  font-size: 17px;
  margin: 2px 0 8px;
}
.body p {
  font-size: 13.5px;
  color: var(--pulse-text-muted);
  line-height: 1.65;
  margin: 0 0 10px;
}
.body code {
  color: var(--pulse-text);
  background: var(--pulse-solid-2);
  padding: 1px 6px;
  border-radius: 6px;
  font-size: 12.5px;
}
.note {
  font-size: 12.5px;
}
.cmd {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 11px 13px;
  border-radius: 11px;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  font-family: var(--pulse-font-mono);
  font-size: 12.5px;
  color: var(--pulse-text);
  cursor: pointer;
  text-align: left;
  transition: border-color 0.15s;
}
.cmd:hover {
  border-color: rgba(199, 245, 66, 0.5);
}
.cmd.ok {
  border-color: var(--pulse-accent);
}
.cmd .p {
  color: var(--pulse-accent);
}
.cmd .t {
  flex: 1;
  overflow-x: auto;
  white-space: nowrap;
}
.cmd .c {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--pulse-text-muted);
}
.open-btn {
  display: inline-block;
  padding: 10px 16px;
  border-radius: 11px;
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  font-weight: 700;
  font-size: 13px;
  text-decoration: none;
  box-shadow: 0 0 0 1px rgba(199, 245, 66, 0.4), 0 10px 30px rgba(199, 245, 66, 0.14);
}
.foot {
  margin-top: 26px;
  font-size: 13px;
  color: var(--pulse-text-muted);
  text-align: center;
}
.link {
  color: var(--pulse-accent);
  text-decoration: none;
}
</style>
