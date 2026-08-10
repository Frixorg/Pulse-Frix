<script setup lang="ts">
import { ref, watch, computed, onMounted } from "vue";
import { storeToRefs } from "pinia";
import { useServersStore } from "@/stores/servers";
import type { Resource } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyState from "@/components/EmptyState.vue";
import HealthBadge from "@/components/status/HealthBadge.vue";
import CustomSelect from "@/components/CustomSelect.vue";

const props = defineProps<{
  title: string;
  subtitle?: string;
  loader: (serverId: string) => Promise<{ data: Resource[] }>;
  attrKeys?: string[];
  storageKey: string; // where per-user category tweaks are persisted
}>();

const servers = useServersStore();
const { selected } = storeToRefs(servers);
const rows = ref<Resource[]>([]);
const loading = ref(false);

const UNGROUPED = "__ungrouped__";

// Persisted, per-user overrides layered on top of the automatic grouping.
const assign = ref<Record<string, string>>({}); // resource id -> group key
const rename = ref<Record<string, string>>({}); // group key -> display name
const extra = ref<string[]>([]); // user-created (possibly empty) group keys
const collapsed = ref<Set<string>>(new Set());

const manage = ref(false);
const newName = ref("");
const editing = ref<string | null>(null);
const editName = ref("");

function persistKey() {
  return `pulse-cat-${props.storageKey}`;
}
function loadPrefs() {
  try {
    const raw = localStorage.getItem(persistKey());
    if (!raw) return;
    const p = JSON.parse(raw);
    assign.value = p.assign ?? {};
    rename.value = p.rename ?? {};
    extra.value = Array.isArray(p.extra) ? p.extra : [];
    collapsed.value = new Set(Array.isArray(p.collapsed) ? p.collapsed : []);
  } catch {
    /* ignore malformed prefs */
  }
}
function savePrefs() {
  try {
    localStorage.setItem(
      persistKey(),
      JSON.stringify({
        assign: assign.value,
        rename: rename.value,
        extra: extra.value,
        collapsed: [...collapsed.value],
      }),
    );
  } catch {
    /* ignore quota */
  }
}
watch([assign, rename, extra, collapsed], savePrefs, { deep: true });

// Intelligent default: prefer the Compose project label, otherwise infer a
// project from the container/service name by dropping the replica suffix and
// the trailing role segment (book-frix-web-1 -> book-frix).
function autoGroup(r: Resource): string {
  const proj = String(r.attributes?.compose_project ?? "").trim();
  if (proj) return proj;
  const base = r.name.replace(/[-_]\d+$/, "");
  const dash = base.lastIndexOf("-");
  if (dash > 0) return base.slice(0, dash);
  const us = base.lastIndexOf("_");
  if (us > 0) return base.slice(0, us);
  return UNGROUPED;
}
function groupKeyOf(r: Resource): string {
  return assign.value[r.id] ?? autoGroup(r);
}
function displayName(key: string): string {
  if (rename.value[key]) return rename.value[key];
  if (key === UNGROUPED) return "Ungrouped";
  return key;
}

interface Group {
  key: string;
  name: string;
  items: Resource[];
}
const groups = computed<Group[]>(() => {
  const map = new Map<string, Resource[]>();
  for (const k of extra.value) if (!map.has(k)) map.set(k, []);
  for (const r of rows.value) {
    const k = groupKeyOf(r);
    let arr = map.get(k);
    if (!arr) {
      arr = [];
      map.set(k, arr);
    }
    arr.push(r);
  }
  const list: Group[] = [...map.entries()].map(([key, items]) => ({ key, name: displayName(key), items }));
  // Non-empty groups first (alpha), then empty custom groups, Ungrouped last.
  return list.sort((a, b) => {
    if (a.key === UNGROUPED) return 1;
    if (b.key === UNGROUPED) return -1;
    const ae = a.items.length === 0;
    const be = b.items.length === 0;
    if (ae !== be) return ae ? 1 : -1;
    return a.name.localeCompare(b.name);
  });
});

const moveOptions = computed(() =>
  groups.value
    .map((g) => ({ value: g.key, label: g.name }))
    .concat(groups.value.some((g) => g.key === UNGROUPED) ? [] : [{ value: UNGROUPED, label: "Ungrouped" }]),
);

const allCollapsed = computed(() => groups.value.length > 0 && groups.value.every((g) => collapsed.value.has(g.key)));

function toggle(key: string) {
  const s = new Set(collapsed.value);
  if (s.has(key)) s.delete(key);
  else s.add(key);
  collapsed.value = s;
}
function toggleAll() {
  collapsed.value = allCollapsed.value ? new Set() : new Set(groups.value.map((g) => g.key));
}
function addGroup() {
  const name = newName.value.trim();
  if (!name) return;
  const key = `custom:${name.toLowerCase()}`;
  if (!extra.value.includes(key)) extra.value = [...extra.value, key];
  rename.value = { ...rename.value, [key]: name };
  newName.value = "";
}
function startRename(g: Group) {
  editing.value = g.key;
  editName.value = g.name;
}
function commitRename(key: string) {
  if (editing.value !== key) return;
  const name = editName.value.trim();
  if (name) rename.value = { ...rename.value, [key]: name };
  editing.value = null;
}
function removeGroup(key: string) {
  if (key === UNGROUPED) return;
  // Reassign every member to Ungrouped, then forget the group.
  const next = { ...assign.value };
  for (const r of rows.value) if (groupKeyOf(r) === key) next[r.id] = UNGROUPED;
  assign.value = next;
  extra.value = extra.value.filter((k) => k !== key);
  const rn = { ...rename.value };
  delete rn[key];
  rename.value = rn;
}
function moveItem(r: Resource, key: string) {
  assign.value = { ...assign.value, [r.id]: key };
}

function attr(r: Resource, key: string): string {
  const v = r.attributes?.[key];
  if (v === undefined || v === null || v === "") return "—";
  return String(v);
}

async function load() {
  if (!selected.value) {
    rows.value = [];
    return;
  }
  loading.value = true;
  try {
    const page = await props.loader(selected.value.id);
    rows.value = page.data ?? [];
  } catch {
    rows.value = [];
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  loadPrefs();
  load();
});
watch(selected, load);
</script>

<template>
  <div>
    <PageHeader :title="title" :subtitle="subtitle" />
    <EmptyState v-if="!selected" title="No server selected" message="Connect a server to see this view." />
    <EmptyState
      v-else-if="!loading && rows.length === 0"
      :title="`No ${title.toLowerCase()} discovered`"
      message="Pulse only shows what it actually found — it never invents resources."
    />
    <template v-else>
      <div class="bar">
        <span class="summary">{{ rows.length }} items · {{ groups.length }} groups</span>
        <div class="spacer"></div>
        <button class="ctl" @click="toggleAll">{{ allCollapsed ? "Expand all" : "Collapse all" }}</button>
        <button class="ctl" :class="{ on: manage }" @click="manage = !manage">
          {{ manage ? "Done" : "Manage groups" }}
        </button>
      </div>

      <div v-if="manage" class="addbar">
        <span class="hint">Rename groups, move items between them, or add your own. Saved on this device.</span>
        <div class="spacer"></div>
        <input
          v-model="newName"
          class="ctl inp"
          placeholder="New group name"
          spellcheck="false"
          @keyup.enter="addGroup"
        />
        <button class="ctl add" :disabled="!newName.trim()" @click="addGroup">Add group</button>
      </div>

      <div v-for="g in groups" :key="g.key" class="group">
        <div class="group-head">
          <button class="chev" :class="{ open: !collapsed.has(g.key) }" @click="toggle(g.key)" aria-label="Toggle group">
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.5">
              <path d="m9 18 6-6-6-6" />
            </svg>
          </button>
          <input
            v-if="manage && editing === g.key"
            v-model="editName"
            class="ren"
            spellcheck="false"
            @keyup.enter="commitRename(g.key)"
            @blur="commitRename(g.key)"
          />
          <button v-else class="gname" @click="toggle(g.key)">{{ g.name }}</button>
          <span class="count">{{ g.items.length }}</span>
          <div class="spacer"></div>
          <template v-if="manage">
            <button class="mini" @click="startRename(g)">rename</button>
            <button v-if="g.key !== UNGROUPED" class="mini danger" @click="removeGroup(g.key)">remove</button>
          </template>
        </div>

        <div v-if="!collapsed.has(g.key)" class="group-body card overflow-x-auto">
          <div v-if="g.items.length === 0" class="empty-group">
            Empty group. In manage mode, use the group dropdown on any row to move items here.
          </div>
          <table v-else class="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Health</th>
                <th v-for="k in attrKeys ?? []" :key="k">{{ k.replace(/_/g, " ") }}</th>
                <th>Detected by</th>
                <th v-if="manage">Group</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in g.items" :key="r.id">
                <td class="font-medium">{{ r.name }}</td>
                <td><HealthBadge :status="r.health" /></td>
                <td v-for="k in attrKeys ?? []" :key="k" class="text-muted font-mono text-xs">{{ attr(r, k) }}</td>
                <td class="text-muted">{{ r.detected_by }}</td>
                <td v-if="manage" class="move-cell">
                  <CustomSelect
                    :model-value="groupKeyOf(r)"
                    :options="moveOptions"
                    min-width="150px"
                    @update:model-value="(v: string) => moveItem(r, v)"
                  />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}
.summary {
  font-size: 12px;
  color: var(--pulse-text-muted);
}
.spacer {
  flex: 1;
}
.ctl {
  background: var(--pulse-solid-2);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text);
  border-radius: 10px;
  padding: 7px 12px;
  font-size: 13px;
  font-family: var(--pulse-font-mono);
  cursor: pointer;
}
.ctl.on {
  border-color: rgba(199, 245, 66, 0.45);
  color: var(--pulse-accent);
}
.ctl:disabled {
  opacity: 0.5;
  cursor: default;
}
.inp {
  cursor: text;
  min-width: 180px;
}
.add {
  background: rgba(199, 245, 66, 0.12);
  border-color: rgba(199, 245, 66, 0.35);
  color: var(--pulse-accent);
}
.addbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}
.hint {
  font-size: 12px;
  color: var(--pulse-text-muted);
}
.group {
  margin-bottom: 12px;
}
.group-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 4px;
}
.chev {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: 7px;
  background: transparent;
  border: 0;
  color: var(--pulse-text-muted);
  cursor: pointer;
  transition: transform 0.18s;
}
.chev.open {
  transform: rotate(90deg);
}
.gname {
  background: transparent;
  border: 0;
  color: var(--pulse-text);
  font-family: var(--pulse-font-display);
  font-weight: 700;
  font-size: 15px;
  letter-spacing: -0.01em;
  cursor: pointer;
  padding: 0;
}
.ren {
  background: var(--pulse-solid-2);
  border: 1px solid rgba(199, 245, 66, 0.4);
  color: var(--pulse-text);
  border-radius: 8px;
  padding: 4px 9px;
  font-family: var(--pulse-font-display);
  font-weight: 700;
  font-size: 15px;
}
.count {
  display: inline-grid;
  place-items: center;
  min-width: 22px;
  height: 20px;
  padding: 0 7px;
  border-radius: 999px;
  background: var(--pulse-surface-2);
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text-muted);
  font-size: 11px;
}
.mini {
  background: transparent;
  border: 1px solid var(--pulse-border);
  color: var(--pulse-text-muted);
  border-radius: 8px;
  padding: 4px 9px;
  font-size: 12px;
  font-family: var(--pulse-font-mono);
  cursor: pointer;
}
.mini:hover {
  color: var(--pulse-text);
}
.mini.danger:hover {
  color: var(--pulse-down);
  border-color: rgba(248, 113, 113, 0.4);
}
.empty-group {
  padding: 16px;
  font-size: 12.5px;
  color: var(--pulse-text-muted);
}
.move-cell {
  width: 170px;
}
</style>
