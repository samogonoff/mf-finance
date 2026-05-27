<template>
  <div class="page-bugtracker">
    <header class="page-header">
      <div>
        <h1 class="page-title">Баг-трекер</h1>
        <p class="page-subtitle">
          <span>{{ total }} репортов</span>
          <span class="ctx-sep">·</span>
          <NuxtLink to="/admin/bugtracker/sources" class="link-muted">источники</NuxtLink>
        </p>
      </div>
    </header>

    <!-- Метрики -->
    <section class="metrics-strip" v-if="metrics">
      <div class="metric">
        <div class="metric-label">Всего</div>
        <div class="metric-value">{{ metrics.total }}</div>
      </div>
      <div class="metric">
        <div class="metric-label">Новые</div>
        <div class="metric-value">{{ metrics.by_status.new || 0 }}</div>
      </div>
      <div class="metric">
        <div class="metric-label">В работе</div>
        <div class="metric-value">{{ metrics.by_status.in_progress || 0 }}</div>
      </div>
      <div class="metric">
        <div class="metric-label">Решены</div>
        <div class="metric-value delta-pos">{{ metrics.by_status.resolved || 0 }}</div>
      </div>
      <div class="metric">
        <div class="metric-label">MTTR</div>
        <div class="metric-value">{{ formatMttr(metrics.mttr_seconds) }}</div>
        <div class="metric-sub">средн. время решения</div>
      </div>
    </section>

    <div class="filters-bar">
      <div class="search">
        <Icon name="lucide:search" class="search-icon" />
        <input v-model="filter.search" placeholder="Поиск по заголовку/описанию…" @keyup.enter="reload" />
      </div>
      <select v-model="filter.section" class="select" style="width: 160px" @change="reload">
        <option value="">Все разделы</option>
        <option v-for="s in SECTIONS" :key="s" :value="s">{{ s }}</option>
      </select>
      <select v-model="filter.status" class="select" style="width: 160px" @change="reload">
        <option value="">Все статусы</option>
        <option v-for="s in STATUSES" :key="s" :value="s">{{ s }}</option>
      </select>
      <select v-model="filter.type" class="select" style="width: 140px" @change="reload">
        <option value="">Все типы</option>
        <option v-for="t in TYPES" :key="t" :value="t">{{ t }}</option>
      </select>
      <button class="btn btn-ghost" @click="reload">
        <Icon name="lucide:refresh-cw" /> Обновить
      </button>
    </div>

    <div v-if="error" class="error-banner">{{ error }}</div>

    <div class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th class="col-w-16">ID</th>
            <th>Раздел</th>
            <th>Тип</th>
            <th>Заголовок</th>
            <th>Статус</th>
            <th>Скриншоты</th>
            <th class="col-num">Создан</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="b in reports" :key="b.id" @click="openCard(b)" class="row-click">
            <td class="num">{{ b.id }}</td>
            <td>{{ b.section }}</td>
            <td>{{ b.type }}</td>
            <td class="cell-title">{{ b.title }}</td>
            <td>
              <span class="badge" :class="statusBadge(b.status)">{{ statusLabel(b.status) }}</span>
            </td>
            <td>
              <span v-if="b.screenshots.length">{{ b.screenshots.length }} 📎</span>
              <span v-else class="text-muted">—</span>
            </td>
            <td class="num">{{ formatDate(b.created_at) }}</td>
            <td>
              <button class="btn btn-sm btn-ghost" @click.stop="del(b)">
                <Icon name="lucide:trash" />
              </button>
            </td>
          </tr>
          <tr v-if="!reports.length && !loading">
            <td colspan="8" class="cell-empty">Репортов нет</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="pagination">
      <span class="pagination-info">
        {{ offset + 1 }}–{{ Math.min(offset + reports.length, total) }} из {{ total }}
      </span>
      <div class="pagination-controls">
        <button class="btn btn-sm btn-ghost" :disabled="offset === 0" @click="prev">
          <Icon name="lucide:chevron-left" /> Назад
        </button>
        <button class="btn btn-sm btn-ghost" :disabled="offset + limit >= total" @click="next">
          Вперёд <Icon name="lucide:chevron-right" />
        </button>
      </div>
    </div>

    <!-- Карточка репорта -->
    <Teleport to="body">
      <div v-if="card" class="card-overlay" @click="card = null" />
      <aside v-if="card" class="card-drawer">
        <header class="card-head">
          <h2>Репорт #{{ card.id }}</h2>
          <button class="btn btn-ghost btn-sm" @click="card = null"><Icon name="lucide:x" /></button>
        </header>
        <div class="card-body">
          <h3>{{ card.title }}</h3>
          <p v-if="card.description" class="card-desc">{{ card.description }}</p>

          <div class="field">
            <span class="field-label">Статус</span>
            <select v-model="card.status" class="select" @change="savePatch({ status: card.status })">
              <option v-for="s in STATUSES" :key="s" :value="s">{{ statusLabel(s) }}</option>
            </select>
          </div>

          <div v-if="card.status === 'resolved'" class="field">
            <span class="field-label">Источник причины</span>
            <select :value="card.source_id ?? ''" class="select"
                    @change="onSourceChange(($event.target as HTMLSelectElement).value)">
              <option value="">—</option>
              <option v-for="s in sources" :key="s.id" :value="s.id">{{ s.name }}</option>
            </select>
          </div>

          <div class="field">
            <span class="field-label">ID задачи в Битрикс24</span>
            <input v-model="b24Edit" class="input" placeholder="например, 12345"
                   @blur="savePatch({ b24_task_id: b24Edit || null })" />
          </div>

          <div class="field">
            <span class="field-label">Комментарий админа</span>
            <textarea v-model="commentEdit" class="textarea" rows="3"
                      @blur="savePatch({ admin_comment: commentEdit })" />
          </div>

          <div v-if="card.screenshots.length" class="field">
            <span class="field-label">Скриншоты</span>
            <div class="shots-grid">
              <a v-for="(name, i) in card.screenshots" :key="i"
                 :href="`${uploadsBase}/uploads/bugtracker/${card.id}/${name}`"
                 target="_blank" rel="noopener">
                <img :src="`${uploadsBase}/uploads/bugtracker/${card.id}/${name}`" alt="" />
              </a>
            </div>
          </div>

          <details class="tech">
            <summary>Технический контекст</summary>
            <pre>{{ JSON.stringify(card.tech_context, null, 2) }}</pre>
            <summary>Route</summary>
            <pre>{{ JSON.stringify(card.route, null, 2) }}</pre>
            <summary>Console / Network / JS errors</summary>
            <pre>{{ JSON.stringify({ console: card.console_logs, net: card.network_errors, js: card.js_errors }, null, 2) }}</pre>
          </details>
        </div>
      </aside>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch } from "vue";
import { useBugTrackerAdmin, type BugReport, type BugMetrics, type BugSource } from "~/composables/useBugTrackerAdmin";

definePageMeta({ middleware: "scope-guard" });

const api = useBugTrackerAdmin();
const config = useRuntimeConfig();
const uploadsBase = config.public.apiBase;

const STATUSES = ["new", "in_progress", "resolved", "duplicate", "rejected"];
const TYPES = ["bug", "data", "ui", "performance", "other"];
const SECTIONS = ["finance", "cost", "operations", "reports", "counterparties", "analytics", "account", "admin", "other"];

const reports = ref<BugReport[]>([]);
const total = ref(0);
const limit = ref(50);
const offset = ref(0);
const metrics = ref<BugMetrics | null>(null);
const sources = ref<BugSource[]>([]);
const loading = ref(false);
const error = ref<string | null>(null);
const card = ref<BugReport | null>(null);
const b24Edit = ref("");
const commentEdit = ref("");

const filter = ref<{ search: string; section: string; status: string; type: string }>({
  search: "", section: "", status: "", type: ""
});

const reload = async () => {
  loading.value = true;
  error.value = null;
  try {
    const r = await api.list({
      search: filter.value.search || undefined,
      section: filter.value.section || undefined,
      status: filter.value.status || undefined,
      type: filter.value.type || undefined,
      limit: limit.value,
      offset: offset.value
    });
    reports.value = r.data;
    total.value = r.total;
    metrics.value = await api.metrics();
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Ошибка загрузки";
  } finally {
    loading.value = false;
  }
};

const prev = () => { offset.value = Math.max(0, offset.value - limit.value); reload(); };
const next = () => { offset.value += limit.value; reload(); };

const openCard = async (b: BugReport) => {
  card.value = await api.get(b.id);
  b24Edit.value = card.value?.b24_task_id ?? "";
  commentEdit.value = card.value?.admin_comment ?? "";
  if (!sources.value.length) sources.value = await api.sources();
};

const savePatch = async (body: Partial<BugReport>) => {
  if (!card.value) return;
  try {
    const updated = await api.patch(card.value.id, body as any);
    Object.assign(card.value, updated);
    // обновим в списке тоже.
    const idx = reports.value.findIndex((r) => r.id === card.value!.id);
    if (idx >= 0) reports.value[idx] = updated;
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Ошибка сохранения";
  }
};

const onSourceChange = (v: string) => {
  const id = v ? Number(v) : null;
  savePatch({ source_id: id as any });
};

const del = async (b: BugReport) => {
  if (!confirm(`Удалить репорт #${b.id}?`)) return;
  try {
    await api.remove(b.id);
    reports.value = reports.value.filter((r) => r.id !== b.id);
    total.value = Math.max(0, total.value - 1);
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Ошибка удаления";
  }
};

const statusLabel = (s: string) => ({
  new: "Новый", in_progress: "В работе", resolved: "Решён",
  duplicate: "Дубликат", rejected: "Отклонён"
} as Record<string, string>)[s] ?? s;

const statusBadge = (s: string) => ({
  new: "badge-info", in_progress: "badge-warn", resolved: "badge-pos",
  duplicate: "badge-info", rejected: "badge-info"
} as Record<string, string>)[s] ?? "badge-info";

const formatDate = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString("ru-RU", { dateStyle: "short", timeStyle: "short" });
};
const formatMttr = (s: number) => {
  if (!s) return "—";
  if (s < 3600) return `${Math.round(s / 60)} мин`;
  if (s < 86400) return `${(s / 3600).toFixed(1)} ч`;
  return `${(s / 86400).toFixed(1)} д`;
};

watch(() => filter.value.search, () => { offset.value = 0; });

onMounted(reload);
</script>

<style scoped>
.metrics-strip {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-5);
  margin-bottom: var(--sp-6);
  overflow: hidden;
}
.metric { padding: var(--sp-4) var(--sp-5); border-right: 1px solid var(--border); }
.metric:last-child { border-right: none; }
.metric-label {
  font-size: var(--fs-xs); color: var(--text-muted); text-transform: uppercase;
  letter-spacing: 0.06em; margin-bottom: 4px;
}
.metric-value {
  font-family: var(--font-mono); font-variant-numeric: tabular-nums;
  font-size: var(--fs-xl); font-weight: var(--fw-semibold); color: var(--text-strong);
}
.metric-sub { font-size: var(--fs-xs); color: var(--text-muted); margin-top: 2px; }

.filters-bar {
  display: flex; align-items: center; gap: var(--sp-4); padding: var(--sp-3) 0;
  margin-bottom: var(--sp-4); flex-wrap: wrap;
}
.filters-bar .search { max-width: 320px; flex: 1 1 280px; }

.row-click { cursor: pointer; }
.row-click:hover { background: var(--bg-hover); }
.cell-title { max-width: 460px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.cell-empty { text-align: center; color: var(--text-muted); padding: var(--sp-6); }
.text-muted { color: var(--text-muted); }
.link-muted { color: var(--text-muted); }
.link-muted:hover { color: var(--accent); }

.error-banner {
  background: var(--neg-soft); color: var(--neg-strong);
  padding: var(--sp-3) var(--sp-4); border-radius: var(--rd-3);
  margin-bottom: var(--sp-4); font-size: var(--fs-sm);
}

.pagination {
  display: flex; align-items: center; gap: var(--sp-5);
  padding: var(--sp-5) 0; font-size: var(--fs-sm);
}
.pagination-controls { display: flex; gap: var(--sp-3); margin-left: auto; }
.pagination-info { color: var(--text-muted); }

.card-overlay { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.32); z-index: 1100; }
.card-drawer {
  position: fixed; top: 0; right: 0; bottom: 0;
  width: 560px; max-width: 100vw;
  background: var(--bg-surface);
  border-left: 1px solid var(--border);
  z-index: 1101;
  display: flex; flex-direction: column;
  overflow: hidden;
}
.card-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: var(--sp-4) var(--sp-5);
  border-bottom: 1px solid var(--border);
}
.card-head h2 { margin: 0; font-size: var(--fs-md); }
.card-body { padding: var(--sp-5); overflow-y: auto; }
.card-desc { color: var(--text-secondary); white-space: pre-wrap; margin: 0 0 var(--sp-4); }

.field { display: block; margin-bottom: var(--sp-4); }
.field-label {
  display: block; font-size: var(--fs-xs); font-weight: var(--fw-semibold);
  color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.06em;
  margin-bottom: 4px;
}
.input, .select, .textarea {
  width: 100%; padding: 6px 10px;
  border: 1px solid var(--border); border-radius: var(--rd-3);
  background: var(--bg-base); color: var(--text-strong); font: inherit;
}
.textarea { resize: vertical; }

.shots-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: var(--sp-3); }
.shots-grid a { display: block; border: 1px solid var(--border); border-radius: var(--rd-3); overflow: hidden; }
.shots-grid img { width: 100%; height: auto; display: block; }

.tech { margin-top: var(--sp-5); }
.tech summary { cursor: pointer; font-weight: var(--fw-semibold); margin-top: var(--sp-3); }
.tech pre { font-size: var(--fs-xs); background: var(--bg-subtle); padding: var(--sp-3); border-radius: var(--rd-3); overflow-x: auto; }
</style>
