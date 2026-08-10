<script setup lang="ts">
import { onMounted } from "vue";
import { RouterView } from "vue-router";
import AppSidebar from "@/components/navigation/AppSidebar.vue";
import AppTopbar from "@/components/navigation/AppTopbar.vue";
import AtmosphereBg from "@/components/AtmosphereBg.vue";
import { useServersStore } from "@/stores/servers";

const servers = useServersStore();
onMounted(() => servers.load());
</script>

<template>
  <div class="shell">
    <AtmosphereBg />
    <div class="shell-in">
      <AppSidebar />
      <div class="col">
        <AppTopbar />
        <main class="main">
          <RouterView />
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
</style>
