<script setup lang="ts">
import { computed, ref } from "vue";
import { useAuthStore } from "@/stores/auth";
import { api, ApiError } from "@/api/client";
import PageHeader from "@/components/PageHeader.vue";
import PasswordField from "@/components/PasswordField.vue";
import ThemeToggle from "@/components/ThemeToggle.vue";

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

// --- Security / account -----------------------------------------------------
// Both forms re-verify the current password server-side. Changing the password
// also rotates every session the account holds, so any other signed-in browser
// is signed out; this one gets a fresh cookie in the same response.
//
// They are shown only for accounts that HAVE a password. On Pulse Cloud people
// sign in with Google or Telegram and have none, so there is nothing here to
// verify against and nothing to change — those accounts get a pointer to the
// provider instead of a form that could only ever fail.

const MIN_PASSWORD = 12;

const hasPassword = computed(() => auth.session?.has_password ?? false);

const emailForm = ref({ email: "", current: "" });
const emailBusy = ref(false);
const emailError = ref("");
const emailDone = ref("");

const pwForm = ref({ current: "", next: "", confirm: "" });
const pwBusy = ref(false);
const pwError = ref("");
const pwDone = ref("");

const canChangeEmail = computed(
  () =>
    !emailBusy.value &&
    emailForm.value.email.trim().length > 0 &&
    emailForm.value.email.trim() !== auth.session?.email &&
    emailForm.value.current.length > 0,
);

const canChangePassword = computed(
  () =>
    !pwBusy.value &&
    pwForm.value.current.length > 0 &&
    pwForm.value.next.length >= MIN_PASSWORD &&
    pwForm.value.next === pwForm.value.confirm &&
    pwForm.value.next !== pwForm.value.current,
);

async function changeEmail() {
  if (!canChangeEmail.value) return;
  emailBusy.value = true;
  emailError.value = "";
  emailDone.value = "";
  try {
    auth.session = await api.updateEmail(emailForm.value.current, emailForm.value.email.trim());
    emailDone.value = "Email updated.";
    emailForm.value = { email: "", current: "" };
  } catch (e) {
    emailError.value = e instanceof ApiError ? e.message : "Could not update the email address";
  } finally {
    emailBusy.value = false;
  }
}

async function changePassword() {
  if (!canChangePassword.value) return;
  pwBusy.value = true;
  pwError.value = "";
  pwDone.value = "";
  try {
    auth.session = await api.updatePassword(pwForm.value.current, pwForm.value.next);
    pwDone.value = "Password updated. Other sessions have been signed out.";
    pwForm.value = { current: "", next: "", confirm: "" };
  } catch (e) {
    pwError.value = e instanceof ApiError ? e.message : "Could not update the password";
    // A 401 whose message asks for a re-login means the password DID change but
    // the replacement cookie never arrived; drop the stale session locally.
    if (e instanceof ApiError && e.status === 401 && e.message.includes("sign in again")) {
      auth.session = null;
    }
  } finally {
    pwBusy.value = false;
  }
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

      <!-- Identity-provider account: nothing here to manage locally. -->
      <div v-if="!hasPassword" class="card">
        <div class="card-title">Security</div>
        <p class="text-sm text-muted">
          You sign in through an identity provider, so this account has no Pulse password. Your
          email address and password are managed with that provider — changing them there changes
          how you sign in here.
        </p>
      </div>

      <!-- Security: the address this account signs in with. -->
      <div v-if="hasPassword" class="card">
        <div class="card-title">Security · Email</div>
        <p class="text-sm text-muted mb-3">
          The address you sign in with. Your current password confirms the change.
        </p>
        <form class="flex flex-col gap-2" @submit.prevent="changeEmail">
          <label class="flex flex-col gap-1">
            <span class="text-xs text-muted">New email</span>
            <input
              v-model="emailForm.email"
              class="input"
              type="email"
              autocomplete="email"
              :placeholder="auth.session?.email"
            />
          </label>
          <label class="flex flex-col gap-1">
            <span class="text-xs text-muted">Current password</span>
            <PasswordField
              v-model="emailForm.current"
              autocomplete="current-password"
              @submit="changeEmail"
            />
          </label>
          <button class="btn btn-primary mt-2 self-start" type="submit" :disabled="!canChangeEmail">
            {{ emailBusy ? "Saving…" : "Update email" }}
          </button>
        </form>
        <p v-if="emailError" class="text-sm text-down mt-2">{{ emailError }}</p>
        <p v-if="emailDone" class="text-sm text-healthy mt-2">{{ emailDone }}</p>
      </div>

      <!-- Security: password rotation. -->
      <div v-if="hasPassword" class="card">
        <div class="card-title">Security · Password</div>
        <p class="text-sm text-muted mb-3">
          At least {{ MIN_PASSWORD }} characters. Changing it signs out every other session.
        </p>
        <form class="flex flex-col gap-2" @submit.prevent="changePassword">
          <label class="flex flex-col gap-1">
            <span class="text-xs text-muted">Current password</span>
            <PasswordField v-model="pwForm.current" autocomplete="current-password" />
          </label>
          <label class="flex flex-col gap-1">
            <span class="text-xs text-muted">New password</span>
            <PasswordField v-model="pwForm.next" autocomplete="new-password" />
          </label>
          <label class="flex flex-col gap-1">
            <span class="text-xs text-muted">Confirm new password</span>
            <PasswordField
              v-model="pwForm.confirm"
              autocomplete="new-password"
              @submit="changePassword"
            />
          </label>
          <p
            v-if="pwForm.confirm.length > 0 && pwForm.next !== pwForm.confirm"
            class="text-xs text-down"
          >
            Both passwords must match.
          </p>
          <button
            class="btn btn-primary mt-2 self-start"
            type="submit"
            :disabled="!canChangePassword"
          >
            {{ pwBusy ? "Saving…" : "Update password" }}
          </button>
        </form>
        <p v-if="pwError" class="text-sm text-down mt-2">{{ pwError }}</p>
        <p v-if="pwDone" class="text-sm text-healthy mt-2">{{ pwDone }}</p>
      </div>

      <div class="card">
        <div class="card-title">Appearance</div>
        <div class="flex items-center justify-between">
          <span class="text-sm text-muted">Theme</span>
          <ThemeToggle />
        </div>
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
