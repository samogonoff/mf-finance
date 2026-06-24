<template>
  <div class="etl-page">
    <h1 class="page-title">ETL задолженности → ClickHouse (Premaster / GLMF)</h1>

    <!-- Settings -->
    <section class="card">
      <header class="card-header">
        <h2>Инкрементальный pull</h2>
        <span v-if="settings?.incremental_last_tick_at" class="muted small">
          Последний тик: {{ formatDt(settings.incremental_last_tick_at) }}
        </span>
      </header>
      <div class="settings-row">
        <label class="toggle">
          <input
            type="checkbox"
            :checked="settings?.incremental_enabled"
            :disabled="settingsSaving"
            @change="saveSettings({ incremental_enabled: ($event.target as HTMLInputElement).checked })" />
          <span>Включён</span>
        </label>
        <label class="inline-field">
          Интервал, мин:
          <input
            type="number"
            min="1"
            max="1440"
            :value="settings?.incremental_interval_minutes"
            :disabled="settingsSaving"
            @change="saveSettings({ incremental_interval_minutes: Number(($event.target as HTMLInputElement).value) })" />
        </label>
        <span v-if="settings?.incremental_last_error" class="error-text">
          Ошибка: {{ settings.incremental_last_error }}
        </span>
      </div>
    </section>

    <!-- Companies -->
    <section class="card">
      <header class="card-header">
        <h2>Юрлица</h2>
        <div class="src-toggle">
          <button
            v-for="s in (['premaster','glmf'] as const)"
            :key="s"
            class="chip"
            :class="{ active: source === s }"
            @click="setSource(s)"
          >{{ s === 'glmf' ? 'GLMF (fact_glmf)' : 'Premaster (fact_premaster)' }}</button>
        </div>
        <button class="btn-ghost" :disabled="loading" @click="refresh">
          <Icon name="lucide:refresh-cw" /> Обновить
        </button>
      </header>
      <table class="data-table">
        <thead>
          <tr>
            <th>ЮЛ</th>
            <th>ИНН</th>
            <th class="num">Строк в CH</th>
            <th>Bootstrap</th>
            <th>Incremental</th>
            <th>Действие</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in companies" :key="row.company_id">
            <td>
              {{ row.name || '—' }}
              <span v-if="row.country" class="badge">{{ row.country }}</span>
            </td>
            <td class="mono">{{ row.company_id }}</td>
            <td class="num">{{ row.ch_rows != null ? formatNum(row.ch_rows) : '—' }}</td>
            <td>
              <span v-if="row.running" class="status running">⟳ идёт…</span>
              <span v-else-if="row.bootstrap?.finished_at" class="status ok">
                ✓ {{ formatDuration(row.bootstrap.duration_seconds) }}
              </span>
              <span v-else-if="row.bootstrap?.error_text" class="status err">
                ✗ {{ truncate(row.bootstrap.error_text, 40) }}
              </span>
              <span v-else-if="row.bootstrap" class="status pending">
                {{ formatNum(row.bootstrap.rows_loaded) }} строк…
              </span>
              <span v-else class="muted">—</span>
            </td>
            <td>
              <template v-if="row.incremental?.last_change_at">
                <span class="status ok small">
                  last: {{ formatDt(row.incremental.last_change_at) }}
                </span>
              </template>
              <span v-else class="muted">—</span>
            </td>
            <td>
              <button
                class="btn"
                :disabled="row.running || actionPending[row.company_id]"
                @click="startBootstrap(row.company_id, row.bootstrap?.finished_at != null, source)">
                {{ row.bootstrap?.finished_at ? 'Перезалить' : 'Залить' }}
              </button>
              <button
                v-if="source === 'glmf'"
                class="btn-ghost small"
                :disabled="actionPending[row.company_id]"
                title="Залить договоры (dim_contract) из субконто Premaster"
                @click="startBootstrap(row.company_id, true, 'contract')">
                + договоры
              </button>
            </td>
          </tr>
          <tr v-if="!loading && companies.length === 0">
            <td colspan="6" class="muted center">Нет данных</td>
          </tr>
        </tbody>
      </table>
      <p v-if="chError" class="error-text small">
        ClickHouse недоступен: {{ chError }} (значения «Строк в CH» не показаны)
      </p>
    </section>

    <!-- Run log -->
    <section class="card">
      <header class="card-header">
        <h2>История запусков</h2>
      </header>
      <table class="data-table">
        <thead>
          <tr>
            <th>Время</th>
            <th>ЮЛ</th>
            <th>Фаза</th>
            <th>Источник</th>
            <th class="num">Строк</th>
            <th class="num">Длит.</th>
            <th>Ошибка</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(e, i) in runLog" :key="i" :class="{ 'row-error': e.error_text }">
            <td class="mono small">{{ formatDt(e.started_at) }}</td>
            <td class="mono">{{ e.company_id }}</td>
            <td>
              <span class="phase" :class="`phase-${e.phase}`">{{ e.phase }}</span>
            </td>
            <td class="muted">{{ e.triggered_by }}</td>
            <td class="num">{{ formatNum(e.rows_loaded) }}</td>
            <td class="num">{{ formatDuration(e.duration_sec) }}</td>
            <td class="err-cell">{{ e.error_text || '' }}</td>
          </tr>
          <tr v-if="!loading && runLog.length === 0">
            <td colspan="7" class="muted center">Журнал пуст</td>
          </tr>
        </tbody>
      </table>
    </section>
  </div>
</template>

<script setup lang="ts">
definePageMeta({ middleware: ["scope-guard"] });

interface CompanyRow {
  company_id: string;
  name?: string;
  country?: string;
  ch_rows?: number;
  running: boolean;
  bootstrap?: {
    rows_loaded: number;
    started_at: string;
    updated_at: string;
    finished_at?: string;
    duration_seconds?: number;
    error_text?: string;
  };
  incremental?: {
    rows_loaded: number;
    last_change_at?: string;
    finished_at?: string;
  };
}

interface Settings {
  incremental_enabled: boolean;
  incremental_interval_minutes: number;
  incremental_last_tick_at?: string;
  incremental_last_error?: string;
}

interface LogEntry {
  company_id: string;
  phase: string;
  triggered_by: string;
  started_at: string;
  finished_at: string;
  duration_sec: number;
  rows_loaded: number;
  last_change_at?: string;
  error_text?: string;
}

const config = useRuntimeConfig();
const apiBase = config.public.apiBase as string;

const companies = ref<CompanyRow[]>([]);
const runLog = ref<LogEntry[]>([]);
const settings = ref<Settings | null>(null);
const chError = ref<string>("");
const loading = ref(false);
const settingsSaving = ref(false);
const actionPending = reactive<Record<string, boolean>>({});

const authHeader = () => {
  const t = process.client ? localStorage.getItem("auth_token") : null;
  return t ? { Authorization: `Bearer ${t}` } : {};
};

// Источник прогресса/заливки: premaster (fact_premaster) | glmf (fact_glmf).
const source = ref<"premaster" | "glmf">("premaster");

const fetchStatus = async () => {
  const data = await $fetch<{ items: CompanyRow[]; ch_error?: string }>(
    `${apiBase}/api/admin/etl/debt/status?source=${source.value}`,
    { headers: authHeader() }
  );
  companies.value = (data.items || []).sort((a, b) =>
    (a.company_id || "").localeCompare(b.company_id || "")
  );
  chError.value = data.ch_error || "";
};

const setSource = (s: "premaster" | "glmf") => {
  if (source.value === s) return;
  source.value = s;
  fetchStatus();
};

const fetchSettings = async () => {
  settings.value = await $fetch<Settings>(`${apiBase}/api/admin/etl/debt/settings`, {
    headers: authHeader()
  });
};

const fetchLog = async () => {
  const data = await $fetch<{ items: LogEntry[] }>(
    `${apiBase}/api/admin/etl/debt/log?limit=50`,
    { headers: authHeader() }
  );
  runLog.value = data.items || [];
};

const refresh = async () => {
  loading.value = true;
  try {
    await Promise.all([fetchStatus(), fetchSettings(), fetchLog()]);
  } finally {
    loading.value = false;
  }
};

const saveSettings = async (patch: Partial<Settings>) => {
  settingsSaving.value = true;
  try {
    settings.value = await $fetch<Settings>(`${apiBase}/api/admin/etl/debt/settings`, {
      method: "PUT",
      body: patch,
      headers: authHeader()
    });
  } finally {
    settingsSaving.value = false;
  }
};

const startBootstrap = async (inn: string, isReload: boolean, src: "premaster" | "glmf" | "contract") => {
  const verb = isReload ? "перезалить" : "залить";
  const what = src === "contract" ? "договоры (dim_contract)" : `данные (${src})`;
  if (!confirm(`Точно ${verb} ${what} для ${inn}? Это перельёт строки в CH заново.`)) return;
  actionPending[inn] = true;
  try {
    await $fetch(`${apiBase}/api/admin/etl/debt/bootstrap`, {
      method: "POST",
      body: { company_id: inn, source: src },
      headers: authHeader()
    });
    await fetchStatus();
  } catch (e: any) {
    alert(`Не удалось запустить: ${e?.data?.error || e?.message || "ошибка"}`);
  } finally {
    actionPending[inn] = false;
  }
};

// Авто-обновление статуса каждые 5 секунд, если есть активный bootstrap;
// иначе раз в 30 секунд. Лог обновляем реже (раз в 30s).
let statusTimer: any = null;
let logTimer: any = null;

const anyRunning = computed(() => companies.value.some((c) => c.running));

const pollStatus = async () => {
  try {
    await fetchStatus();
  } catch (_e) { /* ignore */ }
  const interval = anyRunning.value ? 5000 : 30000;
  statusTimer = setTimeout(pollStatus, interval);
};

const pollLog = async () => {
  try {
    await fetchLog();
  } catch (_e) { /* ignore */ }
  logTimer = setTimeout(pollLog, 30000);
};

onMounted(() => {
  refresh().then(() => {
    statusTimer = setTimeout(pollStatus, 5000);
    logTimer = setTimeout(pollLog, 30000);
  });
});

onBeforeUnmount(() => {
  if (statusTimer) clearTimeout(statusTimer);
  if (logTimer) clearTimeout(logTimer);
});

// --- Formatters ---
const formatNum = (n?: number | null) =>
  n == null ? "—" : n.toLocaleString("ru-RU");
const formatDt = (s?: string) => {
  if (!s) return "—";
  const d = new Date(s);
  if (isNaN(d.getTime())) return s;
  return d.toLocaleString("ru-RU", { hour12: false });
};
const formatDuration = (sec?: number | null) => {
  if (sec == null) return "—";
  if (sec < 60) return `${sec.toFixed(1)} с`;
  const m = Math.floor(sec / 60);
  const s = Math.round(sec - m * 60);
  if (m < 60) return `${m}м ${s}с`;
  const h = Math.floor(m / 60);
  return `${h}ч ${m - h * 60}м`;
};
const truncate = (s: string, n: number) => (s.length > n ? s.slice(0, n) + "…" : s);
</script>

<style scoped>
.etl-page { padding: 1.5rem; max-width: 1400px; }
.page-title { font-size: 1.5rem; font-weight: 600; margin-bottom: 1.5rem; }
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  margin-bottom: 1.5rem;
  overflow: hidden;
}
.card-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 0.75rem 1rem;
  border-bottom: 1px solid var(--border);
  background: var(--surface-2);
}
.card-header h2 { font-size: 1rem; font-weight: 500; margin: 0; }
.settings-row {
  display: flex; align-items: center; gap: 1.5rem;
  padding: 1rem; flex-wrap: wrap;
}
.toggle { display: flex; align-items: center; gap: 0.5rem; cursor: pointer; }
.toggle input { width: 16px; height: 16px; }
.inline-field { display: flex; align-items: center; gap: 0.5rem; font-size: 0.875rem; }
.inline-field input {
  width: 4em; padding: 0.25rem 0.5rem;
  border: 1px solid var(--border); border-radius: 4px;
  background: var(--surface); color: inherit;
  font-family: var(--font-mono); text-align: right;
}
.data-table { width: 100%; border-collapse: collapse; font-size: 0.875rem; }
.data-table th, .data-table td {
  padding: 0.5rem 0.75rem;
  text-align: left; border-bottom: 1px solid var(--border);
}
.data-table th { font-weight: 500; background: var(--surface-2); color: var(--text-muted); }
.data-table tr:last-child td { border-bottom: none; }
.data-table .num { text-align: right; font-variant-numeric: tabular-nums; font-family: var(--font-mono); }
.mono { font-family: var(--font-mono); font-size: 0.85em; }
.small { font-size: 0.8em; }
.muted { color: var(--text-muted); }
.center { text-align: center; }
.badge {
  display: inline-block; margin-left: 0.5rem;
  padding: 0.05rem 0.4rem;
  font-size: 0.7em; font-weight: 500;
  background: var(--surface-2); color: var(--text-muted);
  border-radius: 3px;
}
.src-toggle { display: flex; gap: 6px; margin-left: auto; margin-right: var(--sp-2, 12px); }
.src-toggle .chip {
  background: transparent;
  border: 1px solid var(--border);
  border-radius: var(--rd-2, 6px);
  padding: 4px 10px;
  font-size: 0.8rem;
  cursor: pointer;
  font-family: inherit;
  color: var(--text-secondary);
}
.src-toggle .chip.active {
  background: var(--accent, #4338ca);
  color: #fff;
  border-color: var(--accent, #4338ca);
}
.btn-ghost.small { font-size: 0.78rem; padding: 2px 8px; margin-left: 6px; }
.status { font-size: 0.875rem; }
.status.ok { color: var(--success, #16a34a); }
.status.err { color: var(--danger, #dc2626); }
.status.running { color: var(--accent, #4338ca); }
.status.pending { color: var(--text-muted); }
.error-text { color: var(--danger, #dc2626); }
.btn {
  padding: 0.35rem 0.85rem;
  background: var(--accent, #4338ca); color: white;
  border: none; border-radius: 4px; cursor: pointer;
  font-size: 0.875rem;
}
.btn:disabled { background: var(--text-muted); cursor: not-allowed; opacity: 0.5; }
.btn-ghost {
  display: inline-flex; align-items: center; gap: 0.35rem;
  padding: 0.25rem 0.65rem;
  background: transparent; color: var(--text);
  border: 1px solid var(--border); border-radius: 4px;
  cursor: pointer; font-size: 0.85rem;
}
.row-error { background: rgba(220, 38, 38, 0.04); }
.err-cell { color: var(--danger, #dc2626); font-size: 0.8em; max-width: 320px; overflow: hidden; text-overflow: ellipsis; }
.phase { padding: 0.05rem 0.45rem; border-radius: 3px; font-size: 0.75em; font-weight: 500; }
.phase-bootstrap { background: rgba(67, 56, 202, 0.1); color: var(--accent, #4338ca); }
.phase-incremental { background: rgba(22, 163, 74, 0.1); color: var(--success, #16a34a); }
</style>
