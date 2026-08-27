/// <reference types="vite/client" />

declare module "*.vue" {
  import type { DefineComponent } from "vue";
  const component: DefineComponent<Record<string, unknown>, Record<string, unknown>, unknown>;
  export default component;
}

/**
 * True when this bundle was built for a self-hosted VPS deployment
 * (IS_SELF_HOSTED / APP_MODE=self_hosted / PULSE_MODE=local). Replaced with a
 * literal at build time by Vite's `define`, so branches on it are eliminated.
 */
declare const __SELF_HOSTED__: boolean;
