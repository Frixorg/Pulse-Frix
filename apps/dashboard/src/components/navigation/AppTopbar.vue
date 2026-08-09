<script setup lang="ts">
import { computed } from "vue";
import { useRouter } from "vue-router";
import { useAuthStore } from "@/stores/auth";
import { useServersStore } from "@/stores/servers";

const auth = useAuthStore();
const servers = useServersStore();
const router = useRouter();

const email = computed(() => auth.session?.email ?? "");

async function logout() {
  await auth.logout();
  router.push({ name: "login" });
}
</script>

<template>
  <header class="h-14 shrink-0 border-b border-border bg-surface flex items-center justify-between px-6">
    <div class="flex items-center gap-3">
      <select
        v-if="servers.list.length"
        class="bg-surface-2 border border-border rounded-md text-sm px-2 py-1"
        :value="servers.selectedId ?? ''"
        @change="servers.select(($event.target as HTMLSelectElement).value)"
      >
        <option v-for="s in servers.list" :key="s.id" :value="s.id">{{ s.hostname || s.server_id }}</option>
      </select>
      <span v-else class="text-sm text-muted">No servers yet</span>
    </div>
    <div class="flex items-center gap-4">
      <span class="text-sm text-muted">{{ email }}</span>
      <button class="btn btn-ghost" @click="logout">Sign out</button>
    </div>
  </header>
</template>
