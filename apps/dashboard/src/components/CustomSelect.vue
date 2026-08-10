<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from "vue";

type Opt = { value: string; label: string };
const props = defineProps<{
  modelValue: string;
  options: (Opt | string)[];
  placeholder?: string;
  minWidth?: string;
}>();
const emit = defineEmits<{ (e: "update:modelValue", v: string): void }>();

const open = ref(false);
const root = ref<HTMLElement | null>(null);

const norm = computed<Opt[]>(() => props.options.map((o) => (typeof o === "string" ? { value: o, label: o } : o)));
const current = computed(() => norm.value.find((o) => o.value === props.modelValue));

function pick(v: string) {
  emit("update:modelValue", v);
  open.value = false;
}
function onDoc(e: MouseEvent) {
  if (root.value && !root.value.contains(e.target as Node)) open.value = false;
}
watch(open, (v) => {
  if (v) document.addEventListener("click", onDoc);
  else document.removeEventListener("click", onDoc);
});
onBeforeUnmount(() => document.removeEventListener("click", onDoc));
</script>

<template>
  <div ref="root" class="cs" :style="{ minWidth: minWidth || '180px' }">
    <button class="cs-btn" :class="{ open }" type="button" @click.stop="open = !open">
      <span class="cs-label">{{ current?.label ?? placeholder ?? "Select…" }}</span>
      <svg class="cs-caret" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
        <path d="m6 9 6 6 6-6" />
      </svg>
    </button>
    <div v-if="open" class="cs-menu">
      <button
        v-for="o in norm"
        :key="o.value"
        class="cs-item"
        :class="{ sel: o.value === modelValue }"
        type="button"
        @click="pick(o.value)"
      >
        <span class="cs-item-label">{{ o.label }}</span>
        <svg v-if="o.value === modelValue" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M20 6 9 17l-5-5" />
        </svg>
      </button>
    </div>
  </div>
</template>

<style scoped>
.cs {
  position: relative;
}
.cs-btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  box-sizing: border-box;
  padding: 7px 12px;
  border-radius: 10px;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
  font-family: var(--pulse-font-mono);
  font-size: 13px;
  cursor: pointer;
  transition: border-color 0.15s;
}
.cs-btn:hover,
.cs-btn.open {
  border-color: rgba(199, 245, 66, 0.45);
}
.cs-label {
  flex: 1;
  text-align: left;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.cs-caret {
  color: var(--pulse-text-muted);
  transition: transform 0.18s;
  flex-shrink: 0;
}
.cs-btn.open .cs-caret {
  transform: rotate(180deg);
}
.cs-menu {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  right: 0;
  z-index: 40;
  padding: 6px;
  border-radius: 12px;
  background: var(--pulse-solid);
  border: 1px solid var(--pulse-border);
  box-shadow: var(--pulse-shadow);
  max-height: 320px;
  overflow-y: auto;
}
.cs-item {
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
.cs-item:hover {
  background: var(--pulse-surface-2);
}
.cs-item.sel {
  color: var(--pulse-accent);
}
.cs-item-label {
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
