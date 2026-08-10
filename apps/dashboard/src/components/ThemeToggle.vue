<script setup lang="ts">
import { ref, onMounted } from "vue";

const isLight = ref(false);

function apply(light: boolean) {
  const root = document.documentElement;
  root.classList.toggle("light", light);
  root.classList.toggle("dark", !light);
  try {
    localStorage.setItem("pulse-theme", light ? "light" : "dark");
  } catch {
    /* ignore */
  }
}

function toggle() {
  isLight.value = !isLight.value;
  apply(isLight.value);
}

onMounted(() => {
  isLight.value = document.documentElement.classList.contains("light");
});
</script>

<template>
  <button
    class="switch"
    :class="{ on: isLight }"
    role="switch"
    :aria-checked="isLight"
    aria-label="Toggle light and dark theme"
    type="button"
    @click="toggle"
  >
    <span class="track">
      <!-- moon (dark) -->
      <svg class="ic ic-moon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
      </svg>
      <!-- sun (light) -->
      <svg class="ic ic-sun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M19 5l-1.5 1.5M6.5 17.5 5 19" />
      </svg>
      <span class="knob"></span>
    </span>
    <span class="switch-label">{{ isLight ? "Light" : "Dark" }}</span>
  </button>
</template>

<style scoped>
.switch {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  background: transparent;
  border: 0;
  cursor: pointer;
  padding: 0;
  color: var(--pulse-text-muted);
  font: inherit;
  font-size: 13px;
}
.switch:hover {
  color: var(--pulse-text);
}
.track {
  position: relative;
  width: 48px;
  height: 26px;
  border-radius: 999px;
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  display: inline-flex;
  align-items: center;
  transition: background 0.2s, border-color 0.2s;
  flex-shrink: 0;
}
.switch.on .track {
  border-color: rgba(199, 245, 66, 0.5);
}
.ic {
  position: absolute;
  width: 13px;
  height: 13px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--pulse-text-muted);
}
.ic-moon {
  left: 6px;
  opacity: 1;
}
.ic-sun {
  right: 6px;
  opacity: 0.5;
}
.switch.on .ic-moon {
  opacity: 0.5;
}
.switch.on .ic-sun {
  opacity: 1;
  color: var(--pulse-accent);
}
.knob {
  position: absolute;
  left: 3px;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: var(--pulse-text);
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.4);
  transition: transform 0.22s cubic-bezier(0.34, 1.56, 0.64, 1), background 0.2s;
}
.switch.on .knob {
  transform: translateX(22px);
  background: var(--pulse-accent);
  box-shadow: 0 0 10px rgba(199, 245, 66, 0.6);
}
.switch-label {
  min-width: 34px;
  text-align: left;
}
</style>
