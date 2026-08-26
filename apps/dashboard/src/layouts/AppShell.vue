<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { RouterView, useRoute } from "vue-router";
import AppSidebar from "@/components/navigation/AppSidebar.vue";
import AppTopbar from "@/components/navigation/AppTopbar.vue";
import AtmosphereBg from "@/components/AtmosphereBg.vue";
import { useServersStore } from "@/stores/servers";

const servers = useServersStore();
const route = useRoute();
const sidebarOpen = ref(false);

// Component names, not route names.
const KEEP_ALIVE = ["SshView"];

onMounted(() => servers.load());
watch(() => route.fullPath, () => (sidebarOpen.value = false));
</script>

<template>
  <div class="shell">
    <AtmosphereBg />
    <div class="shell-in">
      <AppSidebar :open="sidebarOpen" />
      <div class="backdrop" :class="{ show: sidebarOpen }" @click="sidebarOpen = false"></div>
      <div class="col">
        <AppTopbar @menu="sidebarOpen = true" />
        <main class="main">
          <!-- The SSH console owns a live connection and a terminal buffer, so
               it must survive navigation: unmounting it would drop the session
               the moment you glance at another page. Everything else is cheap
               to rebuild and is deliberately not cached. -->
          <RouterView v-slot="{ Component }">
            <KeepAlive :include="KEEP_ALIVE">
              <component :is="Component" />
            </KeepAlive>
          </RouterView>
        </main>
      </div>
    </div>
  </div>
</template>

<style scoped>
.shell {
  position: relative;
  height: 100%;
  background: var(--pulse-bg);
  color: var(--pulse-text);
  overflow: hidden;
}
.shell-in {
  position: relative;
  z-index: 1;
  height: 100%;
  display: flex;
}
.col {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.main {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
}
.backdrop {
  display: none;
}
@media (max-width: 900px) {
  .backdrop {
    display: block;
    position: fixed;
    inset: 0;
    z-index: 45;
    background: rgba(3, 4, 6, 0.5);
    backdrop-filter: blur(2px);
    opacity: 0;
    pointer-events: none;
    transition: opacity 0.2s;
  }
  .backdrop.show {
    opacity: 1;
    pointer-events: auto;
  }
  .main {
    padding: 16px;
  }
}
</style>
