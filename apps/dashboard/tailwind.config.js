/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{vue,ts}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        // Semantic tokens map to CSS variables defined in styles/tokens.css so
        // the whole UI themes from one place.
        bg: "var(--pulse-bg)",
        surface: "var(--pulse-surface)",
        "surface-2": "var(--pulse-surface-2)",
        border: "var(--pulse-border)",
        text: "var(--pulse-text)",
        muted: "var(--pulse-text-muted)",
        accent: "var(--pulse-accent)",
        healthy: "var(--pulse-healthy)",
        degraded: "var(--pulse-degraded)",
        down: "var(--pulse-down)",
        unknown: "var(--pulse-unknown)",
      },
      fontFamily: {
        sans: ["Inter", "system-ui", "sans-serif"],
        mono: ["JetBrains Mono", "ui-monospace", "monospace"],
      },
      borderRadius: {
        card: "10px",
      },
    },
  },
  plugins: [],
};
