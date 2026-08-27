// Deployment-mode flags for the SPA.
//
// SELF_HOSTED is fixed at build time (see vite.config.ts) so the marketing
// bundle can be dropped from a self-hosted image. Prefer the raw
// `__SELF_HOSTED__` literal in code paths where dead-code elimination matters
// (the router); import this constant everywhere else.
export const SELF_HOSTED: boolean = __SELF_HOSTED__;
