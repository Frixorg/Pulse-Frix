<script setup lang="ts">
import { ref } from "vue";

// A password input with a reveal toggle. Secrets stay masked by default and the
// reveal is per-field and never persisted, so switching pages re-masks it.
const model = defineModel<string>({ default: "" });

withDefaults(
  defineProps<{ placeholder?: string; autocomplete?: string; disabled?: boolean }>(),
  { placeholder: "", autocomplete: "off", disabled: false },
);

const emit = defineEmits<{ (e: "submit"): void }>();

const revealed = ref(false);
</script>

<template>
  <div class="pw">
    <input
      v-model="model"
      class="input pw-input"
      :type="revealed ? 'text' : 'password'"
      :placeholder="placeholder"
      :autocomplete="autocomplete"
      :disabled="disabled"
      spellcheck="false"
      @keyup.enter="emit('submit')"
    />
    <button
      class="pw-toggle"
      type="button"
      :disabled="disabled"
      :title="revealed ? 'Hide' : 'Show'"
      :aria-label="revealed ? 'Hide the value' : 'Show the value'"
      :aria-pressed="revealed"
      @click="revealed = !revealed"
    >
      <svg
        v-if="!revealed"
        viewBox="0 0 24 24"
        width="16"
        height="16"
        fill="none"
        stroke="currentColor"
        stroke-width="1.9"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7-10-7-10-7z" />
        <circle cx="12" cy="12" r="3" />
      </svg>
      <svg
        v-else
        viewBox="0 0 24 24"
        width="16"
        height="16"
        fill="none"
        stroke="currentColor"
        stroke-width="1.9"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <path d="M10.6 5.2A10.9 10.9 0 0 1 12 5c6.4 0 10 7 10 7a17.6 17.6 0 0 1-3.2 4.2M6.5 6.6A17.4 17.4 0 0 0 2 12s3.6 7 10 7a10.7 10.7 0 0 0 4.4-.9" />
        <path d="M9.9 9.9a3 3 0 0 0 4.2 4.2" />
        <path d="M3 3l18 18" />
      </svg>
    </button>
  </div>
</template>

<style scoped>
.pw {
  position: relative;
  display: block;
}
/* Room for the toggle so a long secret never slides under it. */
.pw-input {
  padding-right: 42px;
}
.pw-toggle {
  position: absolute;
  top: 50%;
  right: 6px;
  transform: translateY(-50%);
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  padding: 0;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--pulse-text-muted);
  cursor: pointer;
  transition: color 0.14s, background 0.14s;
}
.pw-toggle:hover:not(:disabled) {
  color: var(--pulse-text);
  background: var(--pulse-surface-2);
}
.pw-toggle:focus-visible {
  outline: none;
  color: var(--pulse-accent);
  box-shadow: 0 0 0 2px rgba(199, 245, 66, 0.35);
}
.pw-toggle:disabled {
  opacity: 0.5;
  cursor: default;
}
</style>
