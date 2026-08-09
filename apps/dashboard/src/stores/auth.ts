import { defineStore } from "pinia";
import { api, ApiError } from "@/api/client";
import type { SessionInfo } from "@/api/types";

interface AuthState {
  session: SessionInfo | null;
  loading: boolean;
  checked: boolean;
}

export const useAuthStore = defineStore("auth", {
  state: (): AuthState => ({ session: null, loading: false, checked: false }),
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
    async login(email: string, password: string) {
      try {
        this.session = await api.login(email, password);
        return true;
      } catch (e) {
        if (e instanceof ApiError) throw e;
        throw new ApiError(0, "NETWORK", "network error");
      }
    },
    async logout() {
      await api.logout().catch(() => undefined);
      this.session = null;
    },
  },
});
