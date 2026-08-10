<script setup lang="ts">
// Fixed, non-interactive background atmosphere shared with the landing page:
// an architectural grid, organic glowing gradients, and a subtle grain overlay.
</script>

<template>
  <div class="atmos" aria-hidden="true">
    <div class="atmos-grid"></div>
    <div class="atmos-glow atmos-glow-a"></div>
    <div class="atmos-glow atmos-glow-b"></div>
    <div class="atmos-grain"></div>
  </div>
</template>

<style scoped>
.atmos {
  position: fixed;
  inset: 0;
  z-index: 0;
  pointer-events: none;
  overflow: hidden;
}
.atmos-grid {
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(var(--pulse-grid-line) 1px, transparent 1px),
    linear-gradient(90deg, var(--pulse-grid-line) 1px, transparent 1px);
  background-size: 64px 64px;
  mask-image: radial-gradient(ellipse 90% 70% at 50% 0%, #000 40%, transparent 100%);
  -webkit-mask-image: radial-gradient(ellipse 90% 70% at 50% 0%, #000 40%, transparent 100%);
}
.atmos-glow {
  position: absolute;
  border-radius: 50%;
  filter: blur(120px);
  opacity: 0.5;
}
.atmos-glow-a {
  width: 560px;
  height: 560px;
  top: -180px;
  left: -140px;
  background: radial-gradient(circle, var(--pulse-glow-a), transparent 70%);
}
.atmos-glow-b {
  width: 620px;
  height: 620px;
  top: 200px;
  right: -220px;
  background: radial-gradient(circle, var(--pulse-glow-b), transparent 70%);
}
.atmos-grain {
  position: absolute;
  inset: 0;
  opacity: 0.05;
  mix-blend-mode: overlay;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='140' height='140'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E");
}
</style>
