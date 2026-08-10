<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from "vue";
import { useServersStore } from "@/stores/servers";
import type { Server } from "@/api/types";

const servers = useServersStore();
const open = ref(false);
const root = ref<HTMLElement | null>(null);

const current = computed(() => servers.selected);

function label(s: Server) {
  return s.hostname || s.server_id;
}
function dotClass(status?: string) {
  switch (status) {
    case "HEALTHY":
      return "ok";
    case "DEGRADED":
      return "warn";
    case "DOWN":
      return "down";
    default:
      return "unknown";
  }
}
function toggle() {
  open.value = !open.value;
}
function pick(id: string) {
  servers.select(id);
  open.value = false;
}
function onDocClick(e: MouseEvent) {
  if (root.value && !root.value.contains(e.target as Node)) open.value = false;
}
watch(open, (v) => {
  if (v) document.addEventListener("click", onDocClick);
  else document.removeEventListener("click", onDocClick);
});
onBeforeUnmount(() => document.removeEventListener("click", onDocClick));
</script>

<template>
  <div ref="root" class="ss">
    <button class="ss-btn" :class="{ open }" type="button" @click.stop="toggle">
      <span class="ss-dot" :class="dotClass(current?.status)"></span>
      <span class="ss-name">{{ current ? label(current) : "No server" }}</span>
      <span class="ss-count">{{ servers.list.length }} connected</span>
      <svg class="ss-caret" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
        <path d="m6 9 6 6 6-6" />
      </svg>
    </button>

    <div v-if="open" class="ss-menu">
      <button
        v-for="s in servers.list"
        :key="s.id"
        class="ss-item"
        :class="{ sel: s.id === servers.selectedId }"
        type="button"
        @click="pick(s.id)"
      >
        <span class="ss-dot" :class="dotClass(s.status)"></span>
        <span class="ss-item-name">{{ label(s) }}</span>
        <svg v-if="s.id === servers.selectedId" class="ss-check" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M20 6 9 17l-5-5" />
        </svg>
      </button>
    </div>
  </div>
</template>

<style scoped>
.ss {
  position: relative;
}
.ss-btn {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 7px 12px;
  border-radius: 11px;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
  font-family: var(--pulse-font-mono);
  font-size: 13px;
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
  min-width: 220px;
}
.ss-btn:hover,
.ss-btn.open {
  border-color: rgba(199, 245, 66, 0.45);
}
.ss-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.ss-dot.ok {
  background: var(--pulse-accent);
  box-shadow: 0 0 8px var(--pulse-accent);
}
.ss-dot.warn {
  background: var(--pulse-degraded);
}
.ss-dot.down {
  background: var(--pulse-down);
}
.ss-dot.unknown {
  background: var(--pulse-unknown);
}
.ss-name {
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ss-count {
  margin-left: auto;
  font-size: 11px;
  color: var(--pulse-text-muted);
  white-space: nowrap;
}
.ss-caret {
  color: var(--pulse-text-muted);
  transition: transform 0.18s;
}
.ss-btn.open .ss-caret {
  transform: rotate(180deg);
}
.ss-menu {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  right: 0;
  min-width: 240px;
  z-index: 40;
  padding: 6px;
  border-radius: 12px;
  background: var(--pulse-solid);
  border: 1px solid var(--pulse-border);
  box-shadow: var(--pulse-shadow);
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
  max-height: 320px;
  overflow-y: auto;
}
.ss-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 9px 10px;
  border-radius: 9px;
  border: 0;
  background: transparent;
  color: var(--pulse-text);
  font-family: var(--pulse-font-mono);
  font-size: 13px;
  cursor: pointer;
  text-align: left;
  transition: background 0.12s;
}
.ss-item:hover {
  background: var(--pulse-surface-2);
}
.ss-item.sel {
  color: var(--pulse-accent);
}
.ss-item-name {
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ss-check {
  color: var(--pulse-accent);
  flex-shrink: 0;
}
</style>
