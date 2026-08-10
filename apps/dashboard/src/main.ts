import { createApp } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import { router } from "./router";
import "./styles/tokens.css";
import "./styles/main.css";

// Apply the saved theme before mount so there's no flash of the wrong theme.
try {
  const light = localStorage.getItem("pulse-theme") === "light";
  document.documentElement.classList.toggle("light", light);
  document.documentElement.classList.toggle("dark", !light);
} catch {
  /* ignore */
}

createApp(App).use(createPinia()).use(router).mount("#app");
