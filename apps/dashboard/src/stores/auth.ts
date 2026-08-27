import { defineStore } from "pinia";
import { api, ApiError } from "@/api/client";
import type { SessionInfo, SetupStatus } from "@/api/types";

interface AuthState {
  session: SessionInfo | null;
  loading: boolean;
  checked: boolean;
  /** True while the control plane reports that no administrator exists yet. */
  setupRequired: boolean;
  setupChecked: boolean;
  /** Deployment mode reported by the API ("local" | "cloud"). */
  mode: string;
}

export const useAuthStore = defineStore("auth", {
  state: (): AuthState => ({
    session: null,
    loading: false,
    checked: false,
    setupRequired: false,
    setupChecked: false,
    mode: "",
  }),
  getters: {
    isAuthenticated: (s) => s.session !== null,
    can: (s) => (perm: string) => s.session?.permissions.includes(perm) ?? false,
  },
  actions: {
    async fetchSession() {
      this.loading = true;
      try {
        this.session = await api.session();
      } catch {
        this.session = null;
      } finally {
        this.checked = true;
        this.loading = false;
      }
    },
    async fetchSetupStatus() {
      try {
        const status: SetupStatus = await api.setupStatus();
        this.setupRequired = status.needs_setup;
        this.mode = status.mode;
      } catch {
        // A control plane that cannot answer is treated as already provisioned;
        // the login form then surfaces the real error.
        this.setupRequired = false;
      } finally {
        this.setupChecked = true;
      }
    },
    async login(email: string, password: string) {
      try {
        this.session = await api.login(email, password);
        this.setupRequired = false;
        return true;
      } catch (e) {
        if (e instanceof ApiError) throw e;
        throw new ApiError(0, "NETWORK", "network error");
      }
    },
    /** First-boot provisioning: creates the admin account and signs it in. */
    async completeSetup(email: string, password: string) {
      this.session = await api.completeSetup(email, password);
      this.setupRequired = false;
      this.setupChecked = true;
      this.checked = true;
    },
    async logout() {
      await api.logout().catch(() => undefined);
      this.session = null;
    },
  },
});
