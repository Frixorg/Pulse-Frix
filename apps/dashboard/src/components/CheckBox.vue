<script setup lang="ts">
// A checkbox drawn in Pulse's own palette. The real <input> stays in the DOM
// (hidden but focusable) so keyboard, screen readers and form semantics all
// behave exactly as they would natively.
const model = defineModel<boolean>({ default: false });

withDefaults(defineProps<{ label?: string; disabled?: boolean }>(), {
  label: "",
  disabled: false,
});
</script>

<template>
  <label class="cb" :class="{ disabled }">
    <input v-model="model" type="checkbox" class="cb-input" :disabled="disabled" />
    <span class="cb-box" aria-hidden="true">
      <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="3.2"
        stroke-linecap="round" stroke-linejoin="round">
        <path d="M20 6 9 17l-5-5" />
      </svg>
    </span>
    <span class="cb-label"><slot>{{ label }}</slot></span>
  </label>
</template>

<style scoped>
.cb {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  cursor: pointer;
  user-select: none;
}
.cb.disabled {
  opacity: 0.55;
  cursor: default;
}
/* Kept in the layout for focus and a11y, painted by .cb-box instead. */
.cb-input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  margin: 0;
  pointer-events: none;
}
.cb-box {
  display: grid;
  place-items: center;
  width: 17px;
  height: 17px;
  flex-shrink: 0;
  border-radius: 5px;
  border: 1px solid var(--pulse-border);
  background: var(--pulse-solid-2);
  color: transparent;
  transition: background 0.14s, border-color 0.14s, color 0.14s, box-shadow 0.14s;
}
.cb:hover:not(.disabled) .cb-box {
  border-color: var(--pulse-text-muted);
}
.cb-input:checked + .cb-box {
  background: var(--pulse-accent);
  border-color: var(--pulse-accent);
  color: var(--pulse-accent-ink);
}
.cb-input:focus-visible + .cb-box {
  box-shadow: 0 0 0 3px rgba(199, 245, 66, 0.25);
  border-color: var(--pulse-accent);
}
.cb-label {
  font-size: 12px;
  line-height: 1.45;
  color: var(--pulse-text-muted);
}
.cb:hover:not(.disabled) .cb-label {
  color: var(--pulse-text);
}
</style>
