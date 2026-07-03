<!--
  Справочники модуля «Тактические планы» (ТЗ §«UI справочников», экран №5).
  Визуал управляется ТИПИЗИРОВАННОЙ схемой колонок из реестра бэка (а не сырым
  дампом payload): для каждого справочника — свои колонки/типы/фильтры/бейджи.
    • Реестр слева с фильтром по источнику и статусу синхронизации (DIR-01/03).
    • Типизированная таблица записей, поиск и фильтры по колонкам.
    • Карточка записи (drawer); ручная правка только manual (с аудитом на бэке).
    • Синхронизация Лиса/1С (DIR-03), журнал и diff (DIR-04), прогрев кэша.
    • Настройки кэша/устаревания per-справочник (DIR-05).
-->
<template>
  <div class="page-dirs">
    <header class="page-header">
      <div>
        <h1 class="page-title">Справочники</h1>
        <p class="page-subtitle">НСИ модуля — наши (редактируемые) и из Лисы / 1С (синхронизация)</p>
      </div>
      <NuxtLink to="/plans" class="btn btn-ghost"><Icon name="lucide:arrow-left" /> К модулю</NuxtLink>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>

    <div class="dir-layout">
      <!-- ЛЕВЫЙ РЕЕСТР: фильтры + сгруппированный список -->
      <aside class="dir-rail">
        <div class="rail-filters">
          <select v-model="filterSource" class="input input-sm">
            <option value="">Все источники</option>
            <option value="manual">Наши (manual)</option>
            <option value="lisa">Лиса</option>
            <option value="1c">1С</option>
            <option value="calculated">Расчётные</option>
          </select>
          <select v-model="filterStatus" class="input input-sm">
            <option value="">Любой статус</option>
            <option value="ok">Синхронизирован</option>
            <option value="stale">Устарел</option>
            <option value="error">Ошибка</option>
            <option value="never">Не синхронизирован</option>
            <option value="seed">Seed</option>
          </select>
        </div>

        <template v-for="g in groups" :key="g.key">
          <div v-if="byGroup(g.key).length" class="rail-group">{{ g.label }}</div>
          <NuxtLink
            v-for="d in byGroup(g.key)"
            :key="d.code"
            :to="`/plans/directories/${d.code}`"
            class="rail-item"
            :class="{ active: active === d.code }"
          >
            <Icon :name="d.icon || 'lucide:table'" class="rail-icon" />
            <span class="rail-body">
              <span class="rail-name">{{ d.name }}</span>
              <span class="rail-sub">
                <span class="badge badge-dot" :class="statusTone(d.sync_status)">{{ statusLabel(d.sync_status) }}</span>
                <span class="rail-count">{{ d.row_count }}</span>
              </span>
            </span>
          </NuxtLink>
        </template>
        <p v-if="!filtered.length" class="rail-empty">Ничего не найдено по фильтру.</p>
      </aside>

      <!-- ПРАВАЯ ПАНЕЛЬ -->
      <section class="dir-main">
        <div v-if="!meta" class="card empty-state">Выберите справочник слева.</div>

        <template v-else>
          <!-- Шапка справочника -->
          <div class="card dir-headcard">
            <div class="dh-top">
              <div class="dh-title">
                <Icon :name="meta.icon" class="dh-icon" />
                <div>
                  <h2 class="dh-name">{{ meta.name }}</h2>
                  <p class="dh-desc">{{ meta.description }}</p>
                </div>
              </div>
              <div class="dh-actions">
                <button v-if="isAdmin && meta.syncable" class="btn btn-sm btn-primary" :disabled="busy" @click="doSync">
                  <Icon name="lucide:refresh-cw" :class="{ spin: busy }" /> Синхронизировать
                </button>
                <button v-if="isAdmin" class="btn btn-sm btn-ghost" :disabled="busy" @click="doWarm">
                  <Icon name="lucide:flame" /> Прогреть кэш
                </button>
                <button v-if="isAdmin" class="btn btn-sm btn-ghost" :class="{ active: settingsOpen }" @click="settingsOpen = !settingsOpen">
                  <Icon name="lucide:settings-2" /> Настройки
                </button>
                <button v-if="isAdmin && meta.editable" class="btn btn-sm btn-ghost" @click="addRow">
                  <Icon name="lucide:plus" /> Добавить
                </button>
              </div>
            </div>

            <div class="dh-meta">
              <span class="chip"><span class="chip-k">Источник</span> <span class="badge" :class="sourceTone(meta.source)">{{ sourceLabel(meta.source) }}</span></span>
              <span class="chip"><span class="chip-k">Статус</span> <span class="badge badge-dot" :class="statusTone(meta.sync_status)">{{ statusLabel(meta.sync_status) }}</span></span>
              <span v-if="meta.stage" class="chip"><span class="chip-k">Этап</span> <b>{{ meta.stage }}</b></span>
              <span class="chip"><span class="chip-k">Записей</span> <b>{{ meta.row_count }}</b></span>
              <span class="chip"><span class="chip-k">Версия</span> <b>v{{ meta.version }}</b></span>
              <span v-if="meta.synced_at" class="chip"><span class="chip-k">Синхрон.</span> <b>{{ relTime(meta.synced_at) }}</b></span>
              <span v-if="meta.syncable" class="chip dh-diff">
                <span class="d-add">+{{ meta.last_sync_added }}</span>
                <span class="d-chg">~{{ meta.last_sync_changed }}</span>
                <span class="d-rem">−{{ meta.last_sync_removed }}</span>
              </span>
            </div>

            <p v-if="meta.sync_status === 'error' && meta.last_error" class="dh-error">
              <Icon name="lucide:triangle-alert" /> {{ meta.last_error }}
              <span v-if="meta.retry_count"> · попыток: {{ meta.retry_count }}</span>
            </p>

            <!-- Настройки кэша (DIR-05) -->
            <div v-if="settingsOpen" class="dh-settings">
              <div class="set-field">
                <label>TTL кэша, сек</label>
                <input v-model.number="setTtl" type="number" min="0" class="input input-sm" />
                <span class="set-hint">время жизни строк в Redis ({{ human(setTtl) }})</span>
              </div>
              <div class="set-field">
                <label>Порог устаревания, сек</label>
                <input v-model.number="setStale" type="number" min="0" class="input input-sm" />
                <span class="set-hint">после — статус «устарел» ({{ human(setStale) }})</span>
              </div>
              <button class="btn btn-sm btn-primary" @click="saveSettings"><Icon name="lucide:check" /> Сохранить</button>
            </div>
          </div>

          <!-- Табы -->
          <div class="tabs">
            <button class="tab" :class="{ active: tab === 'rows' }" @click="tab = 'rows'">Записи</button>
            <button class="tab" :class="{ active: tab === 'versions' }" @click="openVersions">Версии <span class="tab-badge">v{{ meta.version }}</span></button>
            <button v-if="meta.syncable" class="tab" :class="{ active: tab === 'log' }" @click="openLog">Журнал синхронизации</button>
          </div>

          <!-- ЗАПИСИ -->
          <div v-if="tab === 'rows'" class="card dir-records">
            <div class="rec-toolbar">
              <div class="search">
                <Icon name="lucide:search" />
                <input v-model="search" class="search-input" :placeholder="`Поиск по ${searchHint}`" />
              </div>
              <select
                v-for="fc in filterCols"
                :key="fc.key"
                v-model="colFilters[fc.key]"
                class="input input-sm"
              >
                <option value="">{{ fc.label }}: все</option>
                <option v-for="opt in distinct(fc.key)" :key="opt" :value="opt">{{ opt }}</option>
              </select>
              <div v-if="hasTree" class="view-toggle">
                <button class="vt" :class="{ on: viewMode === 'tree' }" @click="viewMode = 'tree'" title="Дерево"><Icon name="lucide:list-tree" /></button>
                <button class="vt" :class="{ on: viewMode === 'table' }" @click="viewMode = 'table'" title="Таблица"><Icon name="lucide:table" /></button>
              </div>
              <span class="rec-count">{{ visibleRows.length }} из {{ rows.length }}</span>
            </div>

            <!-- Дерево (вложенный справочник) -->
            <DirTree
              v-if="hasTree && viewMode === 'tree'"
              :rows="visibleRows"
              :group-by="meta.group_by!"
              :all-columns="meta.columns"
              :leaf-columns="leafColumns"
              @row="openRecord"
            />

            <!-- Плоская таблица -->
            <div v-else class="table-wrap">
              <table class="data-table">
                <thead>
                  <tr>
                    <th v-for="c in meta.columns" :key="c.key" :style="c.width ? { width: c.width } : {}"
                        :class="{ 'th-num': c.type === 'number' || c.type === 'money' }">{{ c.label }}</th>
                    <th class="th-act"></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="!visibleRows.length"><td :colspan="meta.columns.length + 1" class="empty-cell">Нет записей.</td></tr>
                  <tr v-for="row in visibleRows" :key="row.id" class="rec-row" @click="openRecord(row)">
                    <td v-for="c in meta.columns" :key="c.key" :class="{ 'td-num': c.type === 'number' || c.type === 'money' }">
                      <DirCell :col="c" :value="row.payload[c.key]" />
                    </td>
                    <td class="th-act"><Icon name="lucide:chevron-right" class="row-go" /></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <!-- ВЕРСИИ -->
          <div v-else-if="tab === 'versions'" class="card dir-versions">
            <p v-if="!versions.length" class="empty-state">Версий ещё нет — справочник не менялся.</p>
            <div v-for="v in versions" :key="v.id" class="ver-item">
              <span class="ver-num badge" :class="v.source === 'manual' ? 'badge-accent' : 'badge-info'">v{{ v.version }}</span>
              <span class="ver-src">{{ v.source === "manual" ? "ручная правка" : "синхронизация" }}</span>
              <span class="ver-diff">
                <span v-if="v.added" class="d-add">+{{ v.added }}</span>
                <span v-if="v.changed" class="d-chg">~{{ v.changed }}</span>
                <span v-if="v.removed" class="d-rem">−{{ v.removed }}</span>
              </span>
              <span class="ver-sum">{{ v.summary }}</span>
              <span v-if="v.by_name" class="ver-by">{{ v.by_name }}</span>
              <span class="ver-time">{{ fmtTime(v.changed_at) }}</span>
            </div>
          </div>

          <!-- ЖУРНАЛ СИНХРОНИЗАЦИИ -->
          <div v-else class="card dir-log">
            <p v-if="!log.length" class="empty-state">Синхронизаций ещё не было.</p>
            <div v-for="e in log" :key="e.id" class="log-item" :class="`log-${e.status}`">
              <span class="log-status badge badge-dot" :class="e.status === 'ok' ? 'badge-pos' : e.status === 'error' ? 'badge-neg' : 'badge-warn'">
                {{ e.status }}
              </span>
              <span class="log-time">{{ fmtTime(e.started_at) }}</span>
              <span class="log-by">{{ e.triggered_by }}</span>
              <span class="log-diff">
                <span class="d-add">+{{ e.rows_added }}</span>
                <span class="d-chg">~{{ e.rows_changed }}</span>
                <span class="d-rem">−{{ e.rows_removed }}</span>
                <span class="log-in">из {{ e.rows_in }}</span>
              </span>
              <span class="log-dur">{{ e.duration_ms }} мс</span>
              <span v-if="e.error" class="log-err">{{ e.error }}</span>
            </div>
          </div>
        </template>
      </section>
    </div>

    <!-- DRAWER: карточка записи -->
    <div v-if="record" class="drawer-overlay" @click.self="record = null">
      <aside class="drawer">
        <header class="drawer-head">
          <h3>{{ recordTitle }}</h3>
          <button class="btn btn-icon btn-sm" @click="record = null"><Icon name="lucide:x" /></button>
        </header>
        <div class="drawer-body">
          <div v-for="c in meta?.columns" :key="c.key" class="field">
            <label class="field-label">{{ c.label }}<span v-if="c.hint" class="field-hint">{{ c.hint }}</span></label>
            <template v-if="canEdit && !c.readonly">
              <!-- связь через пользователя -->
              <div v-if="c.ref === 'user'">
                <div v-if="record.payload[c.key]" class="picked-user">
                  <Icon name="lucide:user-check" /> {{ record.payload[c.key] }}
                  <button class="btn btn-icon btn-sm" @click="clearUserRef(c.key)"><Icon name="lucide:x" /></button>
                </div>
                <ClientOnly v-else>
                  <UserPicker placeholder="Фамилия…" @picked="(u) => setUserRef(c.key, u)" />
                </ClientOnly>
              </div>
              <!-- связь через справочник -->
              <select v-else-if="c.ref" v-model="record.payload[c.key]" class="input input-sm">
                <option value="">— не задано —</option>
                <option v-for="o in refOptions[c.ref] || []" :key="o.value" :value="o.value">{{ o.label }}</option>
              </select>
              <!-- свободный текст -->
              <input v-else v-model="record.payload[c.key]" class="input input-sm" />
            </template>
            <DirCell v-else :col="c" :value="record.payload[c.key]" />
          </div>
          <!-- доп. поля синхронизации, которых нет в схеме -->
          <div v-for="k in extraKeys" :key="k" class="field">
            <label class="field-label field-extra">{{ k }}</label>
            <span class="field-val">{{ record.payload[k] }}</span>
          </div>
        </div>
        <footer v-if="canEdit" class="drawer-foot">
          <button class="btn btn-sm btn-danger" @click="delRecord"><Icon name="lucide:trash-2" /> Удалить</button>
          <button class="btn btn-sm btn-primary" @click="saveRecord"><Icon name="lucide:check" /> Сохранить</button>
        </footer>
      </aside>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useDirectories, type DirMeta, type DirectoryRow, type SyncLogEntry, type DirVersion } from "~/composables/useDirectories";
import DirCell from "~/components/plans/DirCell.vue";
import DirTree from "~/components/plans/DirTree.vue";
import UserPicker from "~/components/plans/UserPicker.vue";

definePageMeta({ middleware: "scope-guard" });

const api = useDirectories();
const route = useRoute();
const { hasRole } = useScope();
const isAdmin = computed(() => hasRole("ROLE_PLANS_ADMIN") || hasRole("ROLE_ADMIN"));

// Открытие справочника по URL: /plans/directories/<code> (прямая ссылка/новая вкладка).
watch(() => route.params.code, (c) => { if (c) open(String(c)); });

const dirs = ref<DirMeta[]>([]);
const active = ref("");
const rows = ref<DirectoryRow[]>([]);
const log = ref<SyncLogEntry[]>([]);
const error = ref("");
const busy = ref(false);
const tab = ref<"rows" | "log" | "versions">("rows");
const versions = ref<DirVersion[]>([]);
const settingsOpen = ref(false);
const setTtl = ref(3600);
const setStale = ref(86400);

const search = ref("");
const colFilters = reactive<Record<string, string>>({});
const record = ref<DirectoryRow | null>(null);
const refOptions = ref<Record<string, { value: string; label: string }[]>>({});

// Подгружает значения справочников, на которые ссылаются ref-колонки текущей записи.
const loadRefOptions = async () => {
  const refs = (meta.value?.columns ?? []).map((c) => c.ref).filter((r): r is string => !!r && r !== "user");
  for (const code of [...new Set(refs)]) {
    if (refOptions.value[code]) continue;
    try {
      const refMeta = dirs.value.find((d) => d.code === code);
      const prim = refMeta?.columns.find((c) => c.primary) ?? refMeta?.columns[0];
      // Подпись опции = человекочитаемое имя: колонка «name» (если она не сам
      // primary, как у dir_country domain→name), иначе сам primary (как у ЮЛ).
      const labelCol = refMeta?.columns.find((c) => c.key === "name" && !c.primary) ?? prim;
      const rows = await api.rows(code);
      refOptions.value[code] = rows
        .map((r) => ({ value: String(r.payload[prim?.key ?? ""] ?? ""), label: String(r.payload[labelCol?.key ?? ""] ?? "") }))
        .filter((o) => o.value !== "");
    } catch {
      refOptions.value[code] = [];
    }
  }
};

const setUserRef = (key: string, u: { id: number; name: string }) => {
  if (!record.value) return;
  record.value.payload[key] = u.name;
  record.value.payload[`${key}_user_id`] = u.id;
};
const clearUserRef = (key: string) => {
  if (!record.value) return;
  record.value.payload[key] = "";
  record.value.payload[`${key}_user_id`] = null;
};
const viewMode = ref<"tree" | "table">("tree");

const hasTree = computed(() => (meta.value?.group_by?.length ?? 0) > 0);
const leafColumns = computed(() => {
  const gb = new Set(meta.value?.group_by ?? []);
  return meta.value?.columns.filter((c) => !gb.has(c.key)) ?? [];
});

const groups = [
  { key: "manual", label: "Наши (редактируемые)" },
  { key: "lisa", label: "Из Лисы" },
  { key: "1c", label: "Из 1С" },
  { key: "calculated", label: "Расчётные" }
];
const filterSource = ref("");
const filterStatus = ref("");

const filtered = computed(() =>
  dirs.value.filter(
    (d) => (!filterSource.value || d.source === filterSource.value) && (!filterStatus.value || d.sync_status === filterStatus.value)
  )
);
const byGroup = (g: string) => filtered.value.filter((d) => d.group === g);
const meta = computed(() => dirs.value.find((d) => d.code === active.value) || null);
const canEdit = computed(() => isAdmin.value && !!meta.value?.editable);

const filterCols = computed(() => meta.value?.columns.filter((c) => c.filterable) ?? []);
const searchCols = computed(() => meta.value?.columns.filter((c) => c.search) ?? []);
const searchHint = computed(() => searchCols.value.map((c) => c.label.toLowerCase()).join(", ") || "записям");

const distinct = (key: string): string[] => {
  const set = new Set<string>();
  rows.value.forEach((r) => {
    const v = r.payload[key];
    if (v !== null && v !== undefined && v !== "") set.add(String(v));
  });
  return [...set].sort();
};

const visibleRows = computed(() => {
  const q = search.value.trim().toLowerCase();
  return rows.value.filter((r) => {
    for (const c of filterCols.value) {
      const f = colFilters[c.key];
      if (f && String(r.payload[c.key] ?? "") !== f) return false;
    }
    if (!q) return true;
    return searchCols.value.some((c) => String(r.payload[c.key] ?? "").toLowerCase().includes(q));
  });
});

const recordTitle = computed(() => {
  if (!record.value || !meta.value) return "Запись";
  const prim = meta.value.columns.find((c) => c.primary);
  return prim ? String(record.value.payload[prim.key] ?? "Новая запись") : "Запись";
});
const extraKeys = computed(() => {
  if (!record.value || !meta.value) return [];
  const known = new Set(meta.value.columns.map((c) => c.key));
  return Object.keys(record.value.payload).filter((k) => !known.has(k));
});

const open = async (code: string) => {
  active.value = code;
  error.value = "";
  tab.value = "rows";
  settingsOpen.value = false;
  search.value = "";
  Object.keys(colFilters).forEach((k) => delete colFilters[k]);
  if (meta.value) {
    setTtl.value = meta.value.cache_ttl_seconds;
    setStale.value = meta.value.stale_after_seconds;
  }
  try {
    rows.value = await api.rows(code);
  } catch (e) {
    error.value = msg(e, "Ошибка загрузки записей");
    rows.value = [];
  }
};

const refreshRegistry = async () => {
  dirs.value = await api.list();
  if (meta.value) {
    setTtl.value = meta.value.cache_ttl_seconds;
    setStale.value = meta.value.stale_after_seconds;
  }
};

const doSync = async () => {
  if (!meta.value) return;
  busy.value = true;
  error.value = "";
  try {
    const res = await api.sync(meta.value.code);
    if (res.status !== "ok") error.value = `Синхронизация: ${res.error || "ошибка"}`;
    await refreshRegistry();
    rows.value = await api.rows(active.value);
  } catch (e) {
    error.value = msg(e, "Ошибка синхронизации");
  } finally {
    busy.value = false;
  }
};

const doWarm = async () => {
  if (!meta.value) return;
  busy.value = true;
  try {
    await api.warm(meta.value.code);
  } catch (e) {
    error.value = msg(e, "Ошибка прогрева");
  } finally {
    busy.value = false;
  }
};

const openVersions = async () => {
  tab.value = "versions";
  if (!meta.value) return;
  try {
    versions.value = await api.versions(meta.value.code);
  } catch (e) {
    error.value = msg(e, "Ошибка загрузки версий");
  }
};

const openLog = async () => {
  tab.value = "log";
  if (!meta.value) return;
  try {
    log.value = await api.log(meta.value.code);
  } catch (e) {
    error.value = msg(e, "Ошибка журнала");
  }
};

const saveSettings = async () => {
  if (!meta.value) return;
  try {
    await api.saveSettings(meta.value.code, setTtl.value, setStale.value);
    settingsOpen.value = false;
    await refreshRegistry();
  } catch (e) {
    error.value = msg(e, "Ошибка настроек");
  }
};

const addRow = () => {
  const payload: Record<string, unknown> = {};
  meta.value?.columns.forEach((c) => (payload[c.key] = ""));
  record.value = { id: 0, payload };
  void loadRefOptions();
};
const openRecord = (row: DirectoryRow) => {
  record.value = { id: row.id, payload: { ...row.payload } };
  void loadRefOptions();
};
const saveRecord = async () => {
  if (!record.value || !meta.value) return;
  try {
    const res = await api.upsert(meta.value.code, record.value.id, record.value.payload);
    record.value = null;
    await refreshRegistry();
    rows.value = await api.rows(active.value);
    void res;
  } catch (e) {
    error.value = msg(e, "Ошибка сохранения");
  }
};
const delRecord = async () => {
  if (!record.value || !meta.value || record.value.id === 0) {
    record.value = null;
    return;
  }
  try {
    await api.remove(meta.value.code, record.value.id);
    record.value = null;
    await refreshRegistry();
    rows.value = await api.rows(active.value);
  } catch (e) {
    error.value = msg(e, "Ошибка удаления");
  }
};

// --- helpers ---
const msg = (e: unknown, def: string) => (e instanceof Error ? e.message : def);
const STATUS: Record<string, { l: string; t: string }> = {
  ok: { l: "синхронизирован", t: "badge-pos" },
  stale: { l: "устарел", t: "badge-warn" },
  error: { l: "ошибка", t: "badge-neg" },
  never: { l: "не синхронизирован", t: "" },
  seed: { l: "seed", t: "badge-info" }
};
const statusLabel = (s: string) => STATUS[s]?.l ?? s;
const statusTone = (s: string) => STATUS[s]?.t ?? "";
const sourceLabel = (s: string) => ({ manual: "наш", lisa: "Лиса", "1c": "1С", calculated: "расчёт" }[s] ?? s);
const sourceTone = (s: string) => ({ manual: "badge-pos", lisa: "badge-info", "1c": "badge-accent", calculated: "badge-warn" }[s] ?? "");

const human = (sec: number) => {
  if (!sec) return "—";
  if (sec % 86400 === 0) return `${sec / 86400} дн`;
  if (sec % 3600 === 0) return `${sec / 3600} ч`;
  if (sec % 60 === 0) return `${sec / 60} мин`;
  return `${sec} с`;
};
const relTime = (iso: string) => {
  const d = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (d < 60) return "только что";
  if (d < 3600) return `${Math.floor(d / 60)} мин назад`;
  if (d < 86400) return `${Math.floor(d / 3600)} ч назад`;
  return `${Math.floor(d / 86400)} дн назад`;
};
const fmtTime = (iso: string) => new Date(iso).toLocaleString("ru-RU", { dateStyle: "short", timeStyle: "short" });

onMounted(async () => {
  try {
    await refreshRegistry();
    if (route.params.code) await open(String(route.params.code));
  } catch (e) {
    error.value = msg(e, "Ошибка загрузки реестра");
  }
});
</script>

<style scoped>
.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-4, 6px); margin-bottom: var(--sp-5); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }

.dir-layout { display: grid; grid-template-columns: 280px 1fr; gap: var(--sp-5); align-items: start; }

/* Левый реестр */
.dir-rail { display: flex; flex-direction: column; gap: 3px; }
.rail-filters { display: flex; gap: var(--sp-3); margin-bottom: var(--sp-3); }
.rail-filters .input { flex: 1; min-width: 0; }
.rail-group { font-size: var(--fs-2xs); text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); margin: var(--sp-4) 0 var(--sp-2); }
.rail-item { display: flex; align-items: center; gap: var(--sp-3); text-align: left; padding: var(--sp-3) var(--sp-4); border: 1px solid var(--border); border-radius: var(--rd-4, 6px); background: var(--bg-surface); cursor: pointer; text-decoration: none; color: inherit; }
.rail-item:hover { border-color: var(--border-strong, var(--accent)); }
.rail-item.active { border-color: var(--accent); box-shadow: var(--shadow-focus); }
.rail-icon { color: var(--text-secondary); flex-shrink: 0; }
.rail-body { display: flex; flex-direction: column; gap: 3px; min-width: 0; flex: 1; }
.rail-name { font-weight: var(--fw-semibold); font-size: var(--fs-sm); }
.rail-sub { display: flex; align-items: center; gap: var(--sp-3); }
.rail-count { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); margin-left: auto; }
.rail-empty { color: var(--text-muted); font-size: var(--fs-sm); padding: var(--sp-4); }

/* Шапка справочника */
.dir-main { display: flex; flex-direction: column; gap: var(--sp-4); min-width: 0; }
.dir-headcard { padding: var(--sp-5); }
.dh-top { display: flex; justify-content: space-between; gap: var(--sp-4); align-items: flex-start; }
.dh-title { display: flex; gap: var(--sp-4); }
.dh-icon { font-size: 28px; color: var(--accent); flex-shrink: 0; }
.dh-name { font-size: var(--fs-lg); font-weight: var(--fw-bold); margin: 0; }
.dh-desc { color: var(--text-secondary); font-size: var(--fs-sm); margin: 2px 0 0; max-width: 70ch; }
.dh-actions { display: flex; gap: var(--sp-3); flex-wrap: wrap; justify-content: flex-end; }
.dh-meta { display: flex; flex-wrap: wrap; gap: var(--sp-3) var(--sp-5); margin-top: var(--sp-4); padding-top: var(--sp-4); border-top: 1px solid var(--border); }
.chip { display: inline-flex; align-items: center; gap: var(--sp-2); font-size: var(--fs-sm); }
.chip-k { color: var(--text-muted); font-size: var(--fs-xs); }
.dh-diff { gap: var(--sp-3); font-family: var(--font-mono); font-size: var(--fs-xs); }
.d-add { color: var(--pos-strong); }
.d-chg { color: var(--warn); }
.d-rem { color: var(--neg-strong); }
.dh-error { display: flex; align-items: center; gap: var(--sp-2); margin: var(--sp-4) 0 0; color: var(--neg-strong); font-size: var(--fs-sm); background: var(--neg-soft); padding: var(--sp-3) var(--sp-4); border-radius: var(--rd-3); }
.dh-settings { display: flex; align-items: flex-end; gap: var(--sp-5); margin-top: var(--sp-4); padding-top: var(--sp-4); border-top: 1px dashed var(--border); }
.set-field { display: flex; flex-direction: column; gap: 4px; }
.set-field label { font-size: var(--fs-xs); color: var(--text-muted); }
.set-field .input { width: 130px; }
.set-hint { font-size: var(--fs-2xs); color: var(--text-muted); }

/* Табы */
.tabs { display: flex; gap: var(--sp-2); border-bottom: 1px solid var(--border); }
.tab { padding: var(--sp-3) var(--sp-4); border: none; background: none; cursor: pointer; color: var(--text-secondary); font-size: var(--fs-sm); border-bottom: 2px solid transparent; margin-bottom: -1px; }
.tab.active { color: var(--accent); border-bottom-color: var(--accent); font-weight: var(--fw-semibold); }

/* Записи */
.dir-records { padding: var(--sp-4) var(--sp-5) var(--sp-5); }
.rec-toolbar { display: flex; align-items: center; gap: var(--sp-2) var(--sp-3); margin-bottom: var(--sp-4); flex-wrap: wrap; }
.search { display: flex; align-items: center; gap: var(--sp-2); border: 1px solid var(--border); border-radius: var(--rd-4); padding: 0 var(--sp-3); color: var(--text-muted); width: 220px; flex: 0 0 auto; }
.search-input { border: none; background: none; padding: 6px 0; font: inherit; font-size: var(--fs-sm); flex: 1; min-width: 0; outline: none; color: var(--text-primary); }
.rec-toolbar .input { width: 150px; flex: 0 0 auto; }
.view-toggle { display: inline-flex; border: 1px solid var(--border); border-radius: var(--rd-4); overflow: hidden; }
.vt { border: none; background: var(--bg-surface); color: var(--text-muted); padding: 5px 9px; cursor: pointer; display: inline-flex; }
.vt.on { background: var(--accent-soft); color: var(--accent); }
.rec-count { margin-left: auto; font-size: var(--fs-xs); color: var(--text-muted); font-family: var(--font-mono); }
.table-wrap { overflow-x: auto; }
.th-num, .td-num { text-align: right; }
.th-act { width: 1%; white-space: nowrap; text-align: right; }
.rec-row { cursor: pointer; }
.rec-row:hover { background: var(--bg-tonal); }
.row-go { color: var(--text-muted); }
.empty-cell { color: var(--text-muted); text-align: center; padding: var(--sp-6) 0; }
.empty-state { color: var(--text-muted); padding: var(--sp-7); text-align: center; }

/* Журнал */
.tab-badge { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.dir-versions { padding: var(--sp-4) var(--sp-5); display: flex; flex-direction: column; gap: 2px; }
.ver-item { display: grid; grid-template-columns: 60px 130px 100px 1fr auto auto; align-items: center; gap: var(--sp-4); padding: var(--sp-3) var(--sp-2); border-bottom: 1px solid var(--border); font-size: var(--fs-sm); }
.ver-num { justify-self: start; font-family: var(--font-mono); }
.ver-src { color: var(--text-secondary); font-size: var(--fs-xs); }
.ver-diff { display: flex; gap: var(--sp-2); font-family: var(--font-mono); font-size: var(--fs-xs); }
.ver-sum { color: var(--text-primary); }
.ver-by { color: var(--text-secondary); font-size: var(--fs-xs); }
.ver-time { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.dir-log { padding: var(--sp-4) var(--sp-5); display: flex; flex-direction: column; gap: 2px; }
.log-item { display: grid; grid-template-columns: 130px 150px 120px 1fr auto; align-items: center; gap: var(--sp-4); padding: var(--sp-3) var(--sp-2); border-bottom: 1px solid var(--border); font-size: var(--fs-sm); }
.log-time { font-family: var(--font-mono); font-size: var(--fs-xs); }
.log-by { color: var(--text-muted); font-size: var(--fs-xs); }
.log-diff { display: flex; gap: var(--sp-3); font-family: var(--font-mono); font-size: var(--fs-xs); }
.log-in { color: var(--text-muted); }
.log-dur { font-family: var(--font-mono); font-size: var(--fs-xs); color: var(--text-muted); }
.log-err { grid-column: 1 / -1; color: var(--neg-strong); font-size: var(--fs-xs); }

/* Drawer */
.drawer-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.28); display: flex; justify-content: flex-end; z-index: 50; }
.drawer { width: 440px; max-width: 92vw; background: var(--bg-surface); height: 100%; display: flex; flex-direction: column; box-shadow: -8px 0 28px rgba(0,0,0,0.18); }
.drawer-head { display: flex; align-items: center; justify-content: space-between; padding: var(--sp-5); border-bottom: 1px solid var(--border); }
.drawer-head h3 { margin: 0; font-family: var(--font-mono); font-size: var(--fs-md); }
.drawer-body { padding: var(--sp-5); overflow-y: auto; flex: 1; display: flex; flex-direction: column; gap: var(--sp-4); }
.field { display: flex; flex-direction: column; gap: 4px; }
.field-label { font-size: var(--fs-xs); color: var(--text-muted); display: flex; justify-content: space-between; }
.field-hint { color: var(--text-muted); font-style: italic; }
.field-extra { font-family: var(--font-mono); }
.field-val { font-size: var(--fs-sm); }
.picked-user { display: flex; align-items: center; gap: var(--sp-2); padding: var(--sp-2) var(--sp-3); border: 1px solid var(--border); border-radius: var(--rd-4); font-size: var(--fs-sm); }
.picked-user .btn-icon { margin-left: auto; }
.drawer-foot { display: flex; justify-content: space-between; padding: var(--sp-4) var(--sp-5); border-top: 1px solid var(--border); }

.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
