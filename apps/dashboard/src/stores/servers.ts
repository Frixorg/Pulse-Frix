import { defineStore } from "pinia";
import { api } from "@/api/client";
import type { Server } from "@/api/types";

interface ServersState {
  list: Server[];
  loading: boolean;
  error: string | null;
  selectedId: string | null;
}

export const useServersStore = defineStore("servers", {
  state: (): ServersState => ({ list: [], loading: false, error: null, selectedId: null }),
  getters: {
    selected: (s) => s.list.find((x) => x.id === s.selectedId) ?? s.list[0] ?? null,
  },
  actions: {
    async load() {
      this.loading = true;
      this.error = null;
      try {
        const page = await api.servers();
        this.list = page.data;
        if (!this.selectedId && this.list.length) this.selectedId = this.list[0].id;
      } catch (e) {
        this.error = e instanceof Error ? e.message : "failed to load servers";
      } finally {
        this.loading = false;
      }
    },
    select(id: string) {
      this.selectedId = id;
    },
    async remove(id: string) {
      await api.deleteServer(id);
      this.list = this.list.filter((s) => s.id !== id);
      if (this.selectedId === id) this.selectedId = this.list[0]?.id ?? null;
    },
  },
});
