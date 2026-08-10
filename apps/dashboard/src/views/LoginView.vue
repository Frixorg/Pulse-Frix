<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useRouter, useRoute, RouterLink } from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { api, ApiError } from "@/api/client";
import AtmosphereBg from "@/components/AtmosphereBg.vue";

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();

const providers = ref<{ name: string; start: string }[]>([]);
const loaded = ref(false);

const email = ref("");
const password = ref("");
const error = ref("");
const busy = ref(false);

if (route.query.error) {
  error.value = "Sign-in failed: " + String(route.query.error).replace(/_/g, " ");
}

onMounted(async () => {
  try {
    providers.value = (await api.authProviders()).providers ?? [];
  } catch {
    providers.value = [];
  } finally {
    loaded.value = true;
  }
});

function providerLabel(name: string) {
  if (name === "google") return "Continue with Google";
  if (name === "telegram") return "Continue with Telegram";
  return "Continue with " + name;
}

async function submit() {
  error.value = "";
  busy.value = true;
  try {
    await auth.login(email.value, password.value);
    const redirect = (route.query.redirect as string) || "/app";
    router.push(redirect);
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : "Login failed";
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="login">
    <AtmosphereBg />
    <div class="login-in">
      <RouterLink to="/" class="brand">
        <span class="brand-dot"></span>
        <span class="brand-name">PulseFrix</span>
      </RouterLink>

      <div class="glass">
        <h1 class="title">Welcome back</h1>
        <p class="sub">Sign in to your Pulse dashboard.</p>
        <p v-if="error" class="err">{{ error }}</p>

        <!-- Cloud: OIDC providers + a route into the self-hosted guide. -->
        <template v-if="providers.length">
          <div class="oidc">
            <a v-for="p in providers" :key="p.name" :href="p.start" class="oidc-btn">
              <svg v-if="p.name === 'google'" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
                <path fill="#4285F4" d="M23.5 12.3c0-.8-.1-1.6-.2-2.3H12v4.5h6.5a5.6 5.6 0 0 1-2.4 3.6v3h3.9c2.3-2.1 3.5-5.2 3.5-8.8z"/>
                <path fill="#34A853" d="M12 24c3.2 0 6-1.1 8-2.9l-3.9-3c-1 .7-2.4 1.1-4.1 1.1-3.1 0-5.8-2.1-6.7-5H1.3v3.1A12 12 0 0 0 12 24z"/>
                <path fill="#FBBC05" d="M5.3 14.2a7.2 7.2 0 0 1 0-4.5V6.6H1.3a12 12 0 0 0 0 10.8l4-3.2z"/>
                <path fill="#EA4335" d="M12 4.8c1.8 0 3.3.6 4.6 1.8l3.4-3.4A12 12 0 0 0 12 0 12 12 0 0 0 1.3 6.6l4 3.1C6.2 6.9 8.9 4.8 12 4.8z"/>
              </svg>
              <svg v-else-if="p.name === 'telegram'" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
                <path fill="#29a9eb" d="M12 24A12 12 0 1 0 12 0a12 12 0 0 0 0 24z"/>
                <path fill="#fff" d="M5.5 11.8 17 7.3c.5-.2 1 .1.8.9l-2 9.2c-.1.6-.5.7-1 .5l-2.8-2-1.3 1.3c-.2.2-.3.3-.6.3l.2-2.9 5.2-4.7c.2-.2 0-.3-.3-.1L8 13l-2.7-.8c-.6-.2-.6-.6.2-.9z"/>
              </svg>
              {{ providerLabel(p.name) }}
            </a>
          </div>

          <div class="or"><span></span>or<span></span></div>

          <RouterLink to="/self-hosted" class="host-btn">
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="4" width="18" height="8" rx="2" />
              <rect x="3" y="14" width="18" height="6" rx="2" />
              <path d="M7 8h.01M7 17h.01" />
            </svg>
            Self-hosted
          </RouterLink>
        </template>

        <!-- Self-hosted instance (no OIDC configured): admin credentials. -->
        <template v-else-if="loaded">
          <form class="form" @submit.prevent="submit">
            <label class="lbl">Email</label>
            <input v-model="email" type="email" autocomplete="username" class="fld" required />
            <label class="lbl">Password</label>
            <input v-model="password" type="password" autocomplete="current-password" class="fld" required />
            <button class="lime-btn" :disabled="busy">{{ busy ? "Signing in…" : "Sign in" }}</button>
          </form>
          <RouterLink to="/self-hosted" class="host-link">Setup guide</RouterLink>
        </template>
      </div>

      <p class="foot-note">Observe first · Change nothing by default</p>
    </div>
  </div>
</template>

<style scoped>
.login {
  position: relative;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: var(--pulse-bg);
  color: var(--pulse-text);
  font-family: var(--pulse-font-mono);
}
.login-in {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 400px;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.brand {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  text-decoration: none;
  color: var(--pulse-text);
  margin-bottom: 20px;
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
  font-size: 20px;
  letter-spacing: -0.02em;
}
.glass {
  width: 100%;
  padding: 28px;
  border-radius: 18px;
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.06), rgba(255, 255, 255, 0.02));
  border: 1px solid var(--pulse-border);
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
  box-shadow: var(--pulse-shadow), inset 0 1px 0 rgba(255, 255, 255, 0.08);
}
.title {
  font-family: var(--pulse-font-display);
  font-size: 26px;
  font-weight: 700;
  letter-spacing: -0.02em;
  margin: 0;
}
.sub {
  color: var(--pulse-text-muted);
  font-size: 13px;
  margin: 6px 0 20px;
}
.err {
  color: var(--pulse-down);
  font-size: 13px;
  margin: 0 0 16px;
}
.oidc {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.oidc-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 11px 16px;
  border-radius: 11px;
  background: var(--pulse-surface);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
  font-size: 13px;
  text-decoration: none;
  transition: background 0.15s, transform 0.12s, border-color 0.15s;
}
.oidc-btn:hover {
  background: var(--pulse-surface-2);
  border-color: rgba(199, 245, 66, 0.4);
  transform: translateY(-1px);
}
.or {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 18px 0;
  font-size: 12px;
  color: var(--pulse-text-muted);
}
.or span {
  height: 1px;
  flex: 1;
  background: var(--pulse-border);
}
.host-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  width: 100%;
  box-sizing: border-box;
  padding: 12px 16px;
  border-radius: 11px;
  background: var(--pulse-accent);
  border: 1px solid rgba(199, 245, 66, 0.35);
  color: var(--pulse-accent-ink);
  font-size: 13px;
  font-weight: 600;
  text-decoration: none;
  transition: filter 0.15s, transform 0.12s;
}
.host-btn:hover {
  filter: brightness(1.06);
  transform: translateY(-1px);
}
.form {
  display: flex;
  flex-direction: column;
}
.lbl {
  font-size: 12px;
  color: var(--pulse-text-muted);
  margin-bottom: 6px;
}
.fld {
  width: 100%;
  box-sizing: border-box;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 14px;
  color: var(--pulse-text);
  font-family: var(--pulse-font-mono);
  margin-bottom: 14px;
  transition: border-color 0.15s;
}
.fld:focus {
  outline: none;
  border-color: var(--pulse-accent);
}
.lime-btn {
  width: 100%;
  padding: 12px;
  border-radius: 11px;
  border: 0;
  cursor: pointer;
  background: var(--pulse-accent);
  color: var(--pulse-accent-ink);
  font-family: var(--pulse-font-mono);
  font-weight: 700;
  font-size: 14px;
  box-shadow: 0 0 0 1px rgba(199, 245, 66, 0.4), 0 10px 30px rgba(199, 245, 66, 0.16);
  transition: filter 0.15s, transform 0.12s;
}
.lime-btn:hover {
  filter: brightness(1.04);
  transform: translateY(-1px);
}
.lime-btn:disabled {
  opacity: 0.6;
  cursor: default;
  transform: none;
}
.host-link {
  display: inline-block;
  margin-top: 14px;
  font-size: 12px;
  color: var(--pulse-text-muted);
  text-decoration: none;
}
.host-link:hover {
  color: var(--pulse-accent);
}
.foot-note {
  margin-top: 18px;
  font-size: 12px;
  color: var(--pulse-text-muted);
}
</style>
