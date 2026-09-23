<template>
  <div class="page-bugtracker">
    <header class="page-header">
      <div>
        <h1 class="page-title">Баг-трекер</h1>
        <p class="page-subtitle">
          <span>{{ total }} репортов</span>
          <span class="ctx-sep">·</span>
          <NuxtLink to="/admin/bugtracker/sources" class="link-muted">источники проблем</NuxtLink>
        </p>
      </div>
    </header>

    <!-- Метрики (за выбранный период) -->
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

    <details v-if="metrics?.top_urls?.length" class="top-urls">
      <summary>Страницы с наибольшим числом жалоб</summary>
      <ol>
        <li v-for="u in metrics.top_urls" :key="u.url">
          <a :href="u.url" target="_blank" rel="noopener" class="url" :title="u.url">{{ u.url }}</a>
          <span class="cnt">{{ u.count }}</span>
        </li>
      </ol>
    </details>

    <div class="filters-bar">
      <div class="search">
        <Icon name="lucide:search" class="search-icon" />
        <input v-model="filter.search" placeholder="Поиск по заголовку, описанию, URL…" @keyup.enter="applyFilters" />
      </div>
      <select v-model="filter.section" class="select" style="width: 160px" @change="applyFilters">
        <option value="">Все разделы</option>
        <option v-for="(label, key) in BT_SECTION_LABELS" :key="key" :value="key">{{ label }}</option>
      </select>
      <select v-model="filter.status" class="select" style="width: 150px" @change="applyFilters">
        <option value="">Все статусы</option>
        <option v-for="(label, key) in BT_STATUS_LABELS" :key="key" :value="key">{{ label }}</option>
      </select>
      <select v-model="filter.type" class="select" style="width: 170px" @change="applyFilters">
        <option value="">Все типы</option>
        <option v-for="(label, key) in BT_TYPE_LABELS" :key="key" :value="key">{{ label }}</option>
      </select>
      <span class="filter-label">с</span>
      <input v-model="filter.date_from" type="date" class="select date" @change="applyFilters" />
      <span class="filter-label">по</span>
      <input v-model="filter.date_to" type="date" class="select date" @change="applyFilters" />
      <button class="btn btn-ghost" @click="applyFilters">
        <Icon name="lucide:refresh-cw" /> Обновить
      </button>
      <button class="btn btn-ghost" @click="resetFilters">Сбросить</button>
    </div>

    <div v-if="error" class="error-banner">{{ error }}</div>

    <div class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th class="col-w-16">ID</th>
            <th class="col-num">Создан</th>
            <th>Раздел</th>
            <th>Тип</th>
            <th>Статус</th>
            <th>Автор</th>
            <th class="col-desc">Заголовок</th>
            <th>Скрины</th>
            <th>MR</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="b in reports" :key="b.id" @click="openCard(b.id)" class="row-click">
            <td class="num">{{ b.id }}</td>
            <td class="num">{{ formatDate(b.created_at) }}</td>
            <td>{{ btLabel(BT_SECTION_LABELS, b.section) }}</td>
            <td>{{ btLabel(BT_TYPE_LABELS, b.type) }}</td>
            <td>
              <span class="badge" :class="btStatusBadge(b.status)">{{ btLabel(BT_STATUS_LABELS, b.status) }}</span>
              <div v-if="b.claimed_by" class="cell-sub" :title="`Взят в работу: ${b.claimed_by}`">
                <Icon name="lucide:bot" /> {{ b.claimed_by }}
              </div>
            </td>
            <td>
              <span v-if="b.user">{{ b.user.name || b.user.email }}</span>
              <span v-else class="text-muted">автоматика</span>
            </td>
            <td class="cell-title col-desc">
              {{ b.title }}
              <div v-if="b.url" class="cell-sub" :title="b.url">{{ b.url }}</div>
            </td>
            <td>
              <span v-if="b.screenshots.length"><Icon name="lucide:image" /> {{ b.screenshots.length }}</span>
              <span v-else class="text-muted">—</span>
            </td>
            <td>
              <a v-if="b.mr_url" :href="b.mr_url" target="_blank" rel="noopener" class="mr-link" @click.stop>!{{ b.mr_iid }}</a>
              <span v-else class="text-muted">—</span>
            </td>
          </tr>
          <tr v-if="!reports.length && !loading">
            <td colspan="9" class="cell-empty">Репортов нет</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="pagination">
      <span class="pagination-info">
        {{ total ? offset + 1 : 0 }}–{{ Math.min(offset + reports.length, total) }} из {{ total }}
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

    <BugReportCard
      v-if="card"
      :report="card"
      :sources="sources"
      @close="closeCard"
      @updated="onUpdated"
      @deleted="onDeleted"
      @sources-changed="loadSources"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from "vue";
import {
  useBugTrackerAdmin, BT_STATUS_LABELS, BT_TYPE_LABELS, BT_SECTION_LABELS, btStatusBadge, btLabel,
  type BugReport, type BugMetrics, type BugSource
} from "~/composables/useBugTrackerAdmin";

definePageMeta({ middleware: "scope-guard" });

const api = useBugTrackerAdmin();
const route = useRoute();
const router = useRouter();

const reports = ref<BugReport[]>([]);
const total = ref(0);
const limit = ref(50);
const offset = ref(0);
const metrics = ref<BugMetrics | null>(null);
const sources = ref<BugSource[]>([]);
const loading = ref(false);
const error = ref<string | null>(null);
const card = ref<BugReport | null>(null);

const emptyFilter = () => ({ search: "", section: "", status: "", type: "", date_from: "", date_to: "" });
const filter = ref(emptyFilter());

const reload = async () => {
  loading.value = true;
  error.value = null;
  const f = filter.value;
  try {
    const [r, m] = await Promise.all([
      api.list({
        search: f.search || undefined,
        section: f.section || undefined,
        status: f.status || undefined,
        type: f.type || undefined,
        date_from: f.date_from || undefined,
        date_to: f.date_to || undefined,
        limit: limit.value,
        offset: offset.value
      }),
      api.metrics({ date_from: f.date_from || undefined, date_to: f.date_to || undefined })
    ]);
    reports.value = r.data;
    total.value = r.total;
    metrics.value = m;
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Ошибка загрузки";
  } finally {
    loading.value = false;
  }
};

const applyFilters = () => { offset.value = 0; reload(); };
const resetFilters = () => { filter.value = emptyFilter(); applyFilters(); };
const prev = () => { offset.value = Math.max(0, offset.value - limit.value); reload(); };
const next = () => { offset.value += limit.value; reload(); };

const loadSources = async () => {
  try {
    sources.value = await api.sources();
  } catch { /* справочник не критичен для просмотра */ }
};

// ?id=N — ссылка из уведомления о новом репорте / готовом MR открывает карточку.
const openCard = async (id: number) => {
  try {
    card.value = await api.get(id);
    if (Number(route.query.id) !== id) router.replace({ query: { ...route.query, id: String(id) } });
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || `Не удалось открыть репорт #${id}`;
  }
};
const closeCard = () => {
  card.value = null;
  const { id: _omit, ...rest } = route.query;
  router.replace({ query: rest });
};

const onUpdated = (updated: BugReport) => {
  card.value = updated;
  const idx = reports.value.findIndex((r) => r.id === updated.id);
  if (idx >= 0) reports.value[idx] = updated;
  api.metrics({ date_from: filter.value.date_from || undefined, date_to: filter.value.date_to || undefined })
    .then((m) => { metrics.value = m; }).catch(() => {});
};

const onDeleted = (id: number) => {
  closeCard();
  reports.value = reports.value.filter((r) => r.id !== id);
  total.value = Math.max(0, total.value - 1);
};

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

onMounted(() => {
  reload();
  loadSources();
  const id = Number(route.query.id);
  if (id > 0) openCard(id);
});
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
.row-click:hover { background: var(--bg-surface-2); }
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

.col-desc { max-width: 420px; }
.cell-sub { color: var(--text-muted); font-size: var(--fs-xs); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.filters-bar .date { width: 150px; }
.filter-label { font-size: var(--fs-xs); color: var(--text-muted); }
.mr-link { font-family: var(--font-mono); font-size: var(--fs-xs); }
.top-urls { margin-bottom: var(--sp-6); font-size: var(--fs-sm); }
.top-urls summary { cursor: pointer; color: var(--text-secondary); }
.top-urls ol { margin: var(--sp-3) 0 0; padding-left: var(--sp-6); }
.top-urls li { display: flex; gap: var(--sp-3); padding: 2px 0; }
.top-urls .url { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.top-urls .cnt { font-family: var(--font-mono); font-variant-numeric: tabular-nums; color: var(--text-muted); }
</style>
