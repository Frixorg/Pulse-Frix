<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { api, ApiError } from "@/api/client";
import AtmosphereBg from "@/components/AtmosphereBg.vue";

// First-boot onboarding. Reached only while the API reports needs_setup, which
// is true exactly when no account exists and ADMIN_EMAIL/ADMIN_PASSWORD were
// not supplied to the installer. The API enforces the same rule, so this page
// cannot be used to add a second administrator.
const auth = useAuthStore();
const router = useRouter();

const email = ref("");
const password = ref("");
const confirm = ref("");
const minLength = ref(12);
const error = ref("");
const busy = ref(false);

onMounted(async () => {
  try {
    minLength.value = (await api.setupStatus()).min_password_length || 12;
  } catch {
    /* keep the default; the submit call surfaces any real problem */
  }
});

// Local mirror of the server-side policy, so the operator sees the rule before
// a round trip. The API is still the authority.
const problems = computed<string[]>(() => {
  const out: string[] = [];
  if (password.value && password.value.length < minLength.value) {
    out.push(`At least ${minLength.value} characters`);
  }
  if (confirm.value && confirm.value !== password.value) {
    out.push("Both passwords must match");
  }
  return out;
});

const canSubmit = computed(
  () =>
    !busy.value &&
    email.value.trim().length > 0 &&
    password.value.length >= minLength.value &&
    password.value === confirm.value,
);

async function submit() {
  if (!canSubmit.value) return;
  error.value = "";
  busy.value = true;
  try {
    await auth.completeSetup(email.value.trim(), password.value);
    router.push("/app");
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : "Setup failed";
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="setup">
    <AtmosphereBg />
    <div class="setup-in">
      <div class="brand">
        <span class="brand-dot"></span>
        <span class="brand-name">PulseFrix</span>
      </div>

      <div class="glass">
        <h1 class="title">Create your administrator</h1>
        <p class="sub">
          This is a fresh install. Choose the credentials you will use to sign in — they are stored
          on this server only, hashed, and never sent anywhere.
        </p>
        <p v-if="error" class="err">{{ error }}</p>

        <form class="form" @submit.prevent="submit">
          <label class="lbl" for="setup-email">Email</label>
          <input
            id="setup-email"
            v-model="email"
            type="email"
            autocomplete="username"
            class="fld"
            required
          />

          <label class="lbl" for="setup-password">Password</label>
          <input
            id="setup-password"
            v-model="password"
            type="password"
            autocomplete="new-password"
            class="fld"
            required
          />

          <label class="lbl" for="setup-confirm">Confirm password</label>
          <input
            id="setup-confirm"
            v-model="confirm"
            type="password"
            autocomplete="new-password"
            class="fld"
            required
          />

          <ul v-if="problems.length" class="rules">
            <li v-for="p in problems" :key="p">{{ p }}</li>
          </ul>
          <p v-else class="hint">Minimum {{ minLength }} characters.</p>

          <button class="lime-btn" :disabled="!canSubmit">
            {{ busy ? "Creating…" : "Create administrator" }}
          </button>
        </form>
      </div>

      <p class="foot-note">Observe first · Change nothing by default</p>
    </div>
  </div>
</template>

<style scoped>
.setup {
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
.setup-in {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 420px;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.brand {
  display: inline-flex;
  align-items: center;
  gap: 9px;
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
  font-size: 24px;
  font-weight: 700;
  letter-spacing: -0.02em;
  margin: 0;
}
.sub {
  color: var(--pulse-text-muted);
  font-size: 13px;
  line-height: 1.5;
  margin: 8px 0 20px;
}
.err {
  color: var(--pulse-down);
  font-size: 13px;
  margin: 0 0 16px;
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
.rules {
  list-style: none;
  margin: 0 0 14px;
  padding: 0;
  font-size: 12px;
  color: var(--pulse-down);
}
.hint {
  font-size: 12px;
  color: var(--pulse-text-muted);
  margin: 0 0 14px;
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
.lime-btn:hover:not(:disabled) {
  filter: brightness(1.04);
  transform: translateY(-1px);
}
.lime-btn:disabled {
  opacity: 0.6;
  cursor: default;
}
.foot-note {
  margin-top: 18px;
  font-size: 12px;
  color: var(--pulse-text-muted);
}
</style>
