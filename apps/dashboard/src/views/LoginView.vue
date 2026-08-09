<script setup lang="ts">
import { ref } from "vue";
import { useRouter, useRoute } from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { ApiError } from "@/api/client";

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();

const email = ref("");
const password = ref("");
const error = ref("");
const busy = ref(false);

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
  <div class="h-full flex items-center justify-center bg-bg text-text p-6">
    <div class="w-full max-w-sm">
      <div class="flex items-center gap-2 mb-6 justify-center">
        <span class="inline-block w-3 h-3 rounded-full bg-healthy"></span>
        <span class="text-xl font-semibold tracking-tight">Pulse</span>
      </div>
      <form class="card space-y-4" @submit.prevent="submit">
        <div>
          <label class="block text-xs text-muted mb-1">Email</label>
          <input
            v-model="email"
            type="email"
            autocomplete="username"
            class="w-full bg-surface-2 border border-border rounded-md px-3 py-2 text-sm"
            required
          />
        </div>
        <div>
          <label class="block text-xs text-muted mb-1">Password</label>
          <input
            v-model="password"
            type="password"
            autocomplete="current-password"
            class="w-full bg-surface-2 border border-border rounded-md px-3 py-2 text-sm"
            required
          />
        </div>
        <p v-if="error" class="text-sm text-down">{{ error }}</p>
        <button class="btn btn-primary w-full justify-center" :disabled="busy">
          {{ busy ? "Signing in…" : "Sign in" }}
        </button>
      </form>

      <div class="flex items-center gap-3 my-4">
        <span class="h-px flex-1 bg-border"></span>
        <span class="text-xs text-muted">or</span>
        <span class="h-px flex-1 bg-border"></span>
      </div>
      <div class="space-y-2">
        <a href="/api/v1/auth/google/start" class="btn w-full justify-center border border-border hover:bg-surface-2">
          Continue with Google
        </a>
        <a href="/api/v1/auth/telegram/start" class="btn w-full justify-center border border-border hover:bg-surface-2">
          Continue with Telegram
        </a>
      </div>

      <p class="text-center text-xs text-muted mt-4">
        New here? <RouterLink to="/welcome" class="text-accent">Set up Pulse</RouterLink>
      </p>
    </div>
  </div>
</template>
