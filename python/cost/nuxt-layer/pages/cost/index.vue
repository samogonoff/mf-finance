<template>
  <div class="page-cost">
    <header class="page-header">
      <div>
        <h1 class="page-title">Себестоимость</h1>
        <p class="page-subtitle">
          <span>Cost History · агрегаты с {{ apiHostLabel }}</span>
          <span v-if="mockMode" class="ctx-sep">·</span>
          <span v-if="mockMode" class="mock-pill">MOCK данные</span>
        </p>
      </div>
    </header>

    <!-- Фильтры -->
    <section class="card filters-card">
      <div class="card-header">
        <div>
          <div class="card-title">Фильтры</div>
          <div class="card-subtitle">Двусторонний каскад — каждый фильтр пересчитывает остальные</div>
        </div>
        <div class="card-actions">
          <button class="btn btn-ghost btn-sm" @click="resetFilters">
            <Icon name="lucide:x" /> Сбросить
          </button>
          <button class="btn btn-primary btn-sm" :disabled="loading" @click="loadData">
            <Icon name="lucide:refresh-cw" />
            {{ loading ? "Загрузка…" : "Загрузить данные" }}
          </button>
        </div>
      </div>

      <div class="card-body filters-body" :class="{ 'is-busy': cascadeBusy }">
        <div class="dates-row">
          <label>Период с
            <input v-model="dateFrom" type="date" />
          </label>
          <label>по
            <input v-model="dateTo" type="date" />
          </label>
        </div>

        <div class="filters-grid">
          <div v-for="f in filterConfig" :key="f.key" class="filter-item">
            <label>{{ f.label }}</label>
            <CostMultiSelect
              v-model="selected[f.key]"
              :options="filterOptions[f.key] || []"
              :placeholder="`Все · ${f.label.toLowerCase()}`"
              @change="onFilterChange(f.key)"
            />
          </div>
        </div>

        <div v-if="cascadeBusy" class="filters-overlay">
          <div class="loader"></div>
          <span>Обновление фильтров…</span>
        </div>
      </div>
    </section>

    <!-- Action bar -->
    <div class="cost-actions">
      <div class="cost-info">
        <template v-if="loading">
          <Icon name="lucide:loader" class="spinning" />
          <span>Загрузка данных…</span>
        </template>
        <template v-else-if="totalAllRecords">
          <Icon name="lucide:check-circle-2" class="info-ok" />
          <span>Агрегировано записей: <strong>{{ totalAllRecords.toLocaleString("ru-RU") }}</strong></span>
        </template>
        <template v-else>
          <Icon name="lucide:info" class="info-muted" />
          <span class="muted">Выберите фильтры и нажмите «Загрузить данные»</span>
        </template>
      </div>
      <div class="cost-actions-buttons">
        <button class="btn btn-ghost" :disabled="!totalAllRecords" @click="exportToExcel">
          <Icon name="lucide:download" /> Экспорт в Excel
        </button>
        <button
          class="btn btn-primary"
          :disabled="!changedRows.size || saving"
          @click="saveAllChanges"
        >
          <Icon name="lucide:save" />
          <template v-if="changedRows.size">
            {{ saving ? "Сохранение…" : `Сохранить изменения (${changedRows.size})` }}
          </template>
          <template v-else>Сохранить изменения</template>
        </button>
      </div>
    </div>

    <!-- Error banner -->
    <div v-if="lastError" class="cost-error">
      <Icon name="lucide:alert-triangle" />
      <div>
        <strong>Не удалось получить данные с API</strong>
        <p>{{ lastError }}</p>
        <p class="muted">
          Проверьте подключение к MSSQL/OLAP в <code>python/cost/.env</code>
          либо включите <code>COST_MOCK=1</code> и пересоберите контур
          (<code>make cost-up</code>).
        </p>
      </div>
      <button class="cost-error-x" @click="lastError = ''" aria-label="Закрыть">×</button>
    </div>

    <!-- Таблица 1: агрегаты -->
    <section class="card">
      <div class="card-header">
        <div>
          <div class="card-title">Агрегированные данные</div>
          <div class="card-subtitle">
            Показано {{ pageRange }} из {{ filteredAggregated.length.toLocaleString("ru-RU") }}{{ columnFiltersActive ? ' (отфильтровано из ' + totalAllRecords.toLocaleString("ru-RU") + ')' : '' }}
          </div>
        </div>
        <div class="card-actions" v-if="totalPages > 1">
          <button class="btn btn-ghost btn-sm" :disabled="currentPage === 0" @click="prevPage">
            <Icon name="lucide:chevron-left" /> Назад
          </button>
          <span class="muted">Стр. {{ currentPage + 1 }} / {{ totalPages }}</span>
          <button
            class="btn btn-ghost btn-sm"
            :disabled="currentPage >= totalPages - 1"
            @click="nextPage"
          >
            Вперёд <Icon name="lucide:chevron-right" />
          </button>
        </div>
      </div>
      <div v-if="allAggregated.length > 0" class="col-filters" :class="{ 'is-active': columnFiltersActive }">
        <div v-for="cfg in columnFilterConfig" :key="cfg.key" class="col-filter-item">
          <label>{{ cfg.label }}</label>
          <CostMultiSelect
            v-model="columnFilters[cfg.key]"
            :options="columnFilterOptions[cfg.key] || []"
            :placeholder="`Все · ${cfg.label.toLowerCase()}`"
          />
        </div>
        <a href="#" class="col-filter-reset" @click.prevent="resetColumnFilters">Сбросить фильтры колонок</a>
      </div>
      <div class="table-wrap" @keydown="onCopyShortcut" tabindex="0">
        <table id="cost-table-1" class="data-table compact">
          <thead>
            <tr>
              <th></th>
              <th>Бренд-менеджер</th>
              <th>Модель</th>
              <th>Артикул</th>
              <th>Страна</th>
              <th>Семья</th>
              <th>Сезон</th>
              <th>Дата</th>
              <th>Уровень цен</th>
              <th class="col-num">Сред. розница (руб)</th>
              <th class="col-num">Сред. опт (руб)</th>
              <th class="col-num">Сред. розница ($)</th>
              <th class="col-num">Сред. опт ($)</th>
              <th class="col-num">Осн. материалы (руб)</th>
              <th class="col-num">Осн. материалы ($)</th>
              <th class="col-num">Вспом. (руб)</th>
              <th class="col-num">Вспом. ($)</th>
              <th class="col-num">Пошив (руб)</th>
              <th class="col-num">Пошив ($)</th>
              <th class="col-num">Раскрой (руб)</th>
              <th class="col-num">Раскрой ($)</th>
              <th class="col-num">Декоры (руб)</th>
              <th class="col-num">Декоры ($)</th>
              <th class="col-num">Себест. (руб)</th>
              <th class="col-num">Себест. ($)</th>
              <th class="col-num">Наценка (руб)</th>
              <th class="col-num">Наценка (%)</th>
              <th class="col-num">Маржа (%)</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="28" class="muted" style="text-align: center; padding: 24px">
                Загрузка данных…
              </td>
            </tr>
            <tr v-else-if="!pageRows.length">
              <td colspan="28" class="muted" style="text-align: center; padding: 24px">
                Нет данных. Загрузите данные кнопкой выше.
              </td>
            </tr>
            <tr
              v-for="(row, idx) in pageRows"
              :key="idx"
              :class="{ selected: selectedRowIndex === getOriginalIndex(row) }"
              @click="selectRow(getOriginalIndex(row))"
            >
              <td><button class="btn-details" @click.stop="openDetails(row)">🔍</button></td>
              <td>{{ row['Бренд-менеджер'] || '—' }}</td>
              <td>{{ row['Модель'] || '—' }}</td>
              <td>{{ row['Артикул'] || '—' }}</td>
              <td>{{ row['Страна пр-ва'] || '—' }}</td>
              <td>{{ row['Семья'] || '—' }}</td>
              <td>{{ row['Сезон'] || '—' }}</td>
              <td class="num">{{ formatDate(row['дата расчета']) }}</td>
              <td>
                <select
                  class="price-select"
                  :value="row['Уровень цен'] || ''"
                  @click.stop
                  @change="onPriceLevelChange(getOriginalIndex(row), ($event.target as HTMLSelectElement).value)"
                >
                  <option value="">—</option>
                  <option v-for="lvl in priceLevels" :key="lvl.name" :value="lvl.name">
                    {{ lvl.name }}
                  </option>
                </select>
              </td>
              <td class="col-num num">{{ fmt(row['avg_Розничная цена по уровню, руб.']) }}</td>
              <td class="col-num num">{{ fmt(row['avg_Отпускная цена по уровню, руб']) }}</td>
              <td class="col-num num">{{ fmt(row['avg_Розничная цена по уровню, USD.']) }}</td>
              <td class="col-num num">{{ fmt(row['avg_Отпускная цена по уровню, USD.']) }}</td>
              <td class="col-num num">{{ fmt(row['sum_Основные материалы, руб.']) }}</td>
              <td class="col-num num">{{ fmt(row['sum_Основные материалы, USD.']) }}</td>
              <td class="col-num num">{{ fmt(row['sum_Вспомогательные материалы, руб.']) }}</td>
              <td class="col-num num">{{ fmt(row['sum_Вспомогательные материалы, USD.']) }}</td>
              <td class="col-num num">{{ fmt(row['avg_Пошив, руб.']) }}</td>
              <td class="col-num num">{{ fmt(row['avg_Пошив, USD.']) }}</td>
              <td class="col-num num">{{ fmt(row['avg_Раскрой, руб.']) }}</td>
              <td class="col-num num">{{ fmt(row['avg_Раскрой, USD.']) }}</td>
              <td class="col-num num">{{ fmt(row['sum_Декоры, руб.']) }}</td>
              <td class="col-num num">{{ fmt(row['sum_Декоры, USD.']) }}</td>
              <td class="col-num num-strong">{{ fmt(row['sum_Себестоимость, руб.']) }}</td>
              <td class="col-num num-strong">{{ fmt(row['sum_Себестоимость, USD.']) }}</td>
              <td class="col-num num">{{ fmt(calc(row).markupRub) }}</td>
              <td class="col-num num" :class="calc(row).markupPct >= 0 ? 'delta-pos' : 'delta-neg'">
                {{ calc(row).markupPct.toFixed(1) }}%
              </td>
              <td class="col-num num">{{ calc(row).marginPct.toFixed(1) }}%</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Details Modal -->
    <div v-if="showDetailsModal" class="modal-overlay" @click.self="showDetailsModal = false">
      <div class="modal-content modal-wide" @click.stop>
        <div class="modal-header">
          <h2>Детализация: {{ detailsModel }}</h2>
          <div class="modal-header-actions">
            <button class="btn btn-ghost btn-sm" @click="openDetailsInNewTab">Открыть в новом окне <Icon name="lucide:external-link" /></button>
            <button class="modal-close" @click="showDetailsModal = false">×</button>
          </div>
        </div>
        <!-- Filter panel -->
        <div class="details-filters">
          <label>Дата от <input v-model="detailDateFrom" type="date" class="form-input" /></label>
          <label>Дата до <input v-model="detailDateTo" type="date" class="form-input" /></label>
          <label>Признак калькуляции
            <select v-model="detailCalcSign" multiple class="form-select-multi">
              <option v-for="cs in (filterOptions['calc_sign'] || [])" :key="cs" :value="cs">{{ cs }}</option>
            </select>
          </label>
          <button class="btn btn-primary btn-sm" @click="loadDetailsData">Применить</button>
          <button class="btn btn-ghost btn-sm" @click="resetDetailsFilters">Сбросить</button>
        </div>
        <!-- Column filters row -->
        <div class="details-col-filters" v-if="detailsAllData.length">
          <div v-for="cfg in detailsFilterConfig" :key="cfg.key" class="details-col-filter-item">
            <label>{{ cfg.label }}</label>
            <select multiple v-model="detailsColumnFilters[cfg.key]" @change="applyDetailsFilters">
              <option v-for="opt in getDetailFilterOptions(cfg)" :key="opt" :value="opt">{{ opt }}</option>
            </select>
          </div>
          <a href="#" class="details-col-filter-reset" @click.prevent="resetDetailsColumnFilters">Сбросить фильтры колонок</a>
        </div>
        <!-- Details table -->
        <div class="table-wrap details-table-wrap">
          <div v-if="detailsLoading" class="muted" style="text-align:center;padding:24px">Загрузка детализации…</div>
          <div v-else-if="!detailsFilteredData.length" class="muted" style="text-align:center;padding:24px">Нет данных</div>
          <table v-else class="data-table compact">
            <thead>
              <tr>
                <th>Дата</th>
                <th>Пр.кальк</th>
                <th>Артикул</th>
                <th>Наименование</th>
                <th>№ задания</th>
                <th class="col-num">Розница (руб)</th>
                <th class="col-num">Опт (руб)</th>
                <th class="col-num">Осн. мат.</th>
                <th class="col-num">Вспом.</th>
                <th class="col-num">Пошив</th>
                <th class="col-num">Раскрой</th>
                <th class="col-num">Декор</th>
                <th class="col-num">Вязание</th>
                <th class="col-num">Себест.</th>
                <th class="col-num">Наценка</th>
                <th class="col-num">Наценка %</th>
                <th class="col-num">Маржа %</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(d, i) in detailsFilteredData" :key="i">
                <td>{{ formatDate(d['дата расчета']) }}</td>
                <td>{{ d['Признак калькуляции'] || '—' }}</td>
                <td>{{ d['Артикул'] || '—' }}</td>
                <td>{{ d['Наименование модели'] || '—' }}</td>
                <td>{{ d['Номер задания производства'] || '—' }}</td>
                <td class="col-num num">{{ fmt(d['Розничная цена, руб.']) }}</td>
                <td class="col-num num">{{ fmt(d['Оптовая цена, руб.']) }}</td>
                <td class="col-num num">{{ fmt(d['Осн. материалы, руб.']) }}</td>
                <td class="col-num num">{{ fmt(d['Вспом. материалы, руб.']) }}</td>
                <td class="col-num num">{{ fmt(d['Пошив, руб.']) }}</td>
                <td class="col-num num">{{ fmt(d['Раскрой, руб.']) }}</td>
                <td class="col-num num">{{ fmt(d['Декор, руб.']) }}</td>
                <td class="col-num num">{{ fmt(d['Вязание, руб.']) }}</td>
                <td class="col-num num-strong">{{ fmt(d['Себестоимость, руб.']) }}</td>
                <td class="col-num num">{{ fmt(d['Наценка, руб.']) }}</td>
                <td class="col-num num">{{ d['Наценка, %'] }}%</td>
                <td class="col-num num">{{ d['Маржинальность, %'] }}%</td>
              </tr>
            </tbody>
          </table>
          <div v-if="detailsFilteredData.length" class="details-count">Найдено строк: {{ detailsFilteredData.length }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";

interface PriceLevel { name: string; price_type1: number; price_type3: number }
interface FilterOption { id: string; text: string }
type FilterKey =
  | "brand_manager" | "level01" | "level02" | "level03" | "level04" | "level05"
  | "calc_sign";

const filterConfig: { key: FilterKey; label: string }[] = [
  { key: "brand_manager", label: "Бренд-менеджер" },
  { key: "level01", label: "Level 01" },
  { key: "level02", label: "Level 02" },
  { key: "level03", label: "Level 03" },
  { key: "level04", label: "Level 04" },
  { key: "level05", label: "Level 05" },
  { key: "calc_sign", label: "Признак калькуляции" },
];

const LEVEL_KEYS = ["level01", "level02", "level03", "level04", "level05"];

const config = useRuntimeConfig();
const apiBase = computed(() =>
  config.public.costOnly ? "" : ((config.public.apiBase as string) || "")
);
const apiHostLabel = computed(() => apiBase.value || "локального API");

const mockMode = ref(false);

// ── Filter state ────────────────────────────────────────────────────────────

const filterOptions = ref<Record<string, any[]>>({} as any);
const selected = reactive<Record<string, string[]>>(
  Object.fromEntries(filterConfig.map((f) => [f.key, [] as string[]])) as any
);
const dateFrom = ref("");
const dateTo = ref("");
const cascadeBusy = ref(false);
const loading = ref(false);
const lastError = ref("");

// ── Helper: normalize level options ─────────────────────────────────────────
// API returns {id, text} for levels; CostMultiSelect needs string[]
// We store text values; cascade sends id values

const levelTextToId = reactive<Record<string, Record<string, string>>>({});
for (const k of LEVEL_KEYS) {
  levelTextToId[k] = {};
}

function normalizeFilterOptions(raw: Record<string, any>): Record<string, string[]> {
  const result: Record<string, string[]> = {};
  for (const key of Object.keys(raw)) {
    const vals = raw[key];
    if (LEVEL_KEYS.includes(key) && Array.isArray(vals) && vals.length > 0 && typeof vals[0] === "object") {
      // {id, text} → string[] of text, store mapping
      const map: Record<string, string> = {};
      const texts = vals.map((v: FilterOption) => {
        map[v.id] = v.text;
        return v.text;
      });
      levelTextToId[key] = map;
      // Also store reverse mapping
      result[key] = texts;
    } else {
      result[key] = vals as string[];
    }
  }
  return result;
}

// ── Filters ─────────────────────────────────────────────────────────────────

async function loadFilters() {
  cascadeBusy.value = true;
  try {
    const raw = await $fetch<Record<string, any>>(
      `${apiBase.value}/api/cost/filter-options`,
      { headers: fetchHeaders.value }
    );
    filterOptions.value = normalizeFilterOptions(raw);
    lastError.value = "";
  } catch (e: any) {
    lastError.value = e?.data?.detail || e?.message || String(e);
    console.error("[cost] filter-options failed", e);
  } finally {
    cascadeBusy.value = false;
  }
}

async function onFilterChange(changedKey: FilterKey) {
  const params = new URLSearchParams();

  // For level filters, send id values for cascade
  for (const [key, vals] of Object.entries(selected)) {
    if (!(vals as string[]).length) continue;
    if (LEVEL_KEYS.includes(key)) {
      // Convert text → id via reverse lookup
      const idMap: Record<string, string> = {};
      for (const [id, text] of Object.entries(levelTextToId[key] || {})) {
        idMap[text] = id;
      }
      for (const v of vals as string[]) {
        const id = idMap[v] || v;
        params.append(key, id);
      }
    } else {
      for (const v of vals as string[]) {
        params.append(key, v);
      }
    }
  }

  cascadeBusy.value = true;
  try {
    const raw = await $fetch<Record<string, any>>(
      `${apiBase.value}/api/cost/filter-options?${params}`,
      { headers: fetchHeaders.value }
    );
    const normalized = normalizeFilterOptions(raw);
    for (const k of filterConfig.map((c) => c.key)) {
      if (k === changedKey) continue;
      filterOptions.value[k] = normalized[k] || [];
      selected[k] = (selected[k] || []).filter((v: string) =>
        filterOptions.value[k] && filterOptions.value[k].includes(v)
      );
    }
    lastError.value = "";
  } catch (e: any) {
    lastError.value = e?.data?.detail || e?.message || String(e);
    console.error("[cost] cascade failed", e);
  } finally {
    cascadeBusy.value = false;
  }
}

const resetFilters = async () => {
  dateFrom.value = "";
  dateTo.value = "";
  for (const k of filterConfig.map((c) => c.key)) selected[k] = [];
  allAggregated.value = [];
  totalAllRecords.value = 0;
  currentPage.value = 0;
  selectedRowIndex.value = -1;
  changedRows.clear();
  await loadFilters();
};

// ── Build filter payload ───────────────────────────────────────────────────

function buildFilters(): Record<string, any> {
  const f: Record<string, any> = {};
  if (dateFrom.value) f.date_from = dateFrom.value;
  if (dateTo.value) f.date_to = dateTo.value;
  for (const k of filterConfig.map((c) => c.key)) {
    const v = selected[k];
    if (v?.length) f[k] = v;
  }
  return f;
}

const fetchHeaders = computed(() => ({}));

// ── Data loading ────────────────────────────────────────────────────────────

const allAggregated = ref<any[]>([]);
const totalAllRecords = ref(0);
const pageSize = 50;

// ── Column filters (client-side) ────────────────────────────────────────────
const columnFilters = reactive<Record<string, string[]>>({
  country: [], family: [], season: [],
});

const columnFilterConfig = [
  { key: 'country', label: 'Страна пр-ва', field: 'Страна пр-ва' },
  { key: 'family', label: 'Семья', field: 'Семья' },
  { key: 'season', label: 'Сезон', field: 'Сезон' },
];

const columnFilterOptions = computed(() => {
  const opts: Record<string, string[]> = {};
  for (const cfg of columnFilterConfig) {
    const vals = new Set<string>();
    for (const row of allAggregated.value) {
      const v = (row[cfg.field] || '').toString().trim();
      if (v) vals.add(v);
    }
    opts[cfg.key] = Array.from(vals).sort();
  }
  return opts;
});

const filteredAggregated = computed(() => {
  return allAggregated.value.filter((row: any) => {
    return columnFilterConfig.every((cfg) => {
      const sel = columnFilters[cfg.key];
      if (!sel || sel.length === 0) return true;
      const val = (row[cfg.field] || '').toString().trim();
      return sel.includes(val);
    });
  });
});

const columnFiltersActive = computed(() => {
  return columnFilterConfig.some((cfg) => columnFilters[cfg.key].length > 0);
});

function resetColumnFilters() {
  for (const cfg of columnFilterConfig) {
    columnFilters[cfg.key] = [];
  }
  currentPage.value = 0;
}

function getOriginalIndex(row: any): number {
  return allAggregated.value.indexOf(row);
}

watch(() => filteredAggregated.value.length, (newLen) => {
  if (currentPage.value * pageSize >= newLen && newLen > 0) {
    currentPage.value = 0;
  }
});

const currentPage = ref(0);
const totalPages = computed(() => Math.max(1, Math.ceil(filteredAggregated.value.length / pageSize)));
const pageStart = computed(() => currentPage.value * pageSize);
const pageRows = computed(() => filteredAggregated.value.slice(pageStart.value, pageStart.value + pageSize));
const pageRange = computed(() => {
  const filteredCount = filteredAggregated.value.length;
  if (!filteredCount) return "0";
  const a = pageStart.value + 1;
  const b = Math.min(pageStart.value + pageSize, filteredCount);
  return `${a}–${b}`;
});

async function loadData() {
  loading.value = true;
  try {
    const result = await $fetch<{ data: any[]; count: number }>(
      `${apiBase.value}/api/cost/aggregated`,
      { method: "POST", body: buildFilters(), headers: fetchHeaders.value }
    );
    allAggregated.value = result.data || [];
    totalAllRecords.value = result.count || 0;
    currentPage.value = 0;
    selectedRowIndex.value = -1;
    changedRows.clear();
    lastError.value = "";
  } catch (e: any) {
    console.error("[cost] aggregated load failed", e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    loading.value = false;
  }
}

const prevPage = () => { if (currentPage.value > 0) currentPage.value -= 1; };
const nextPage = () => { if (currentPage.value < totalPages.value - 1) currentPage.value += 1; };

const selectedRowIndex = ref<number>(-1);

const selectRow = (absoluteIdx: number) => {
  selectedRowIndex.value = absoluteIdx;
};

// ── Details modal ────────────────────────────────────────────────────────────
const showDetailsModal = ref(false);
const detailsModel = ref('');
const detailDateFrom = ref('');
const detailDateTo = ref('');
const detailCalcSign = ref<string[]>([]);
const detailsAllData = ref<any[]>([]);
const detailsFilteredData = ref<any[]>([]);
const detailsLoading = ref(false);

const detailsFilterConfig = [
  { key: 'calc_sign', label: 'Пр.кальк', field: 'Признак калькуляции' },
  { key: 'articul', label: 'Артикул', field: 'Артикул' },
  { key: 'name', label: 'Наименование', field: 'Наименование модели' },
  { key: 'task_num', label: '№ задания', field: 'Номер задания производства' },
];

const detailsColumnFilters = reactive<Record<string, string[]>>({
  calc_sign: [], articul: [], name: [], task_num: [],
});

function getDetailFilterOptions(cfg: { key: string; field: string }): string[] {
  // Cascade: higher-order filters limit options for lower ones
  let available = detailsAllData.value;
  for (const fc of detailsFilterConfig) {
    if (fc.key === cfg.key) break; // stop before current filter
    const sel = detailsColumnFilters[fc.key];
    if (sel && sel.length > 0) {
      available = available.filter((r: any) => {
        const v = (r[fc.field] || '').toString().trim();
        return sel.includes(v);
      });
    }
  }
  const vals = new Set<string>();
  available.forEach((r: any) => {
    const v = (r[cfg.field] || '').toString().trim();
    if (v) vals.add(v);
  });
  return Array.from(vals).sort();
}

function applyDetailsFilters() {
  detailsFilteredData.value = detailsAllData.value.filter((row: any) => {
    return detailsFilterConfig.every((cfg) => {
      const sel = detailsColumnFilters[cfg.key];
      if (!sel || sel.length === 0) return true;
      const val = (row[cfg.field] || '').toString().trim();
      return sel.includes(val);
    });
  });
}

function resetDetailsColumnFilters() {
  for (const key of Object.keys(detailsColumnFilters)) {
    detailsColumnFilters[key] = [];
  }
  applyDetailsFilters();
}

async function openDetails(row: any) {
  const model = String(row['Модель'] || '').trim();
  if (!model) return;
  detailsModel.value = model;

  // Copy current main filters as defaults
  detailDateFrom.value = dateFrom.value;
  detailDateTo.value = dateTo.value;
  detailCalcSign.value = [...(selected.calc_sign || [])];

  showDetailsModal.value = true;
  await loadDetailsData();
}

async function loadDetailsData() {
  detailsLoading.value = true;
  try {
    const body: Record<string, any> = { model: detailsModel.value };
    if (detailDateFrom.value) body.date_from = detailDateFrom.value;
    if (detailDateTo.value) body.date_to = detailDateTo.value;
    if (detailCalcSign.value.length) body.calc_sign = detailCalcSign.value;

    const result = await $fetch<{ data: any[] }>(
      `${apiBase.value}/api/cost/details`,
      { method: 'POST', body }
    );
    detailsAllData.value = result.data || [];
    resetDetailsColumnFilters();
  } catch (e: any) {
    console.error('[cost] details load failed', e);
    detailsAllData.value = [];
    detailsFilteredData.value = [];
  } finally {
    detailsLoading.value = false;
  }
}

function resetDetailsFilters() {
  detailDateFrom.value = '';
  detailDateTo.value = '';
  detailCalcSign.value = [];
  loadDetailsData();
}

function openDetailsInNewTab() {
  // Open a new window with standalone details HTML
  const w = window.open('', '_blank');
  if (!w) return;

  let html = `<!DOCTYPE html><html lang="ru"><head><meta charset="utf-8"><title>Детализация: ${detailsModel.value}</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; padding: 20px; background: #f5f5f5; }
    .container { max-width: 100%; margin: 0 auto; background: white; border-radius: 8px; box-shadow: 0 2px 12px rgba(0,0,0,0.1); }
    .header { padding: 16px 24px; border-bottom: 1px solid #eee; display: flex; justify-content: space-between; align-items: center; }
    .header h1 { font-size: 18px; }
    .content { padding: 16px 24px; }
    table { width: 100%; border-collapse: collapse; font-size: 12px; white-space: nowrap; }
    th { background: #f8f9fa; border-bottom: 2px solid #dee2e6; padding: 8px; text-align: center; font-weight: 700; font-size: 11px; position: sticky; top: 0; }
    td { padding: 6px 8px; border-bottom: 1px solid #eee; text-align: right; }
    td:nth-child(-n+5) { text-align: left; }
    tbody tr:hover { background: #f1f3f5; }
    .count { padding: 8px 24px; font-size: 12px; color: #6c757d; border-top: 1px solid #eee; }
  </style></head><body><div class="container">`;
  html += `<div class="header"><h1>Детализация: ${detailsModel.value}</h1></div>`;
  html += `<div class="content"><table><thead><tr>
    <th>Дата</th><th>Пр.кальк</th><th>Артикул</th><th>Наименование</th><th>№ задания</th>
    <th>Розница</th><th>Опт</th><th>Осн.мат</th><th>Вспом.</th><th>Пошив</th><th>Раскрой</th><th>Декор</th><th>Вязание</th>
    <th>Себест.</th><th>Наценка</th><th>Наценка%</th><th>Маржа%</th>
  </tr></thead><tbody>`;

  for (const r of detailsFilteredData.value) {
    const dateVal = r['дата расчета'] ? (String(r['дата расчета']).includes('T') ? String(r['дата расчета']).split('T')[0] : String(r['дата расчета'])) : '-';
    html += '<tr>';
    html += `<td>${dateVal}</td>`;
    html += `<td>${r['Признак калькуляции'] || '-'}</td>`;
    html += `<td>${r['Артикул'] || '-'}</td>`;
    html += `<td>${r['Наименование модели'] || '-'}</td>`;
    html += `<td>${r['Номер задания производства'] || '-'}</td>`;
    html += `<td>${fmt(r['Розничная цена, руб.'])}</td>`;
    html += `<td>${fmt(r['Оптовая цена, руб.'])}</td>`;
    html += `<td>${fmt(r['Осн. материалы, руб.'])}</td>`;
    html += `<td>${fmt(r['Вспом. материалы, руб.'])}</td>`;
    html += `<td>${fmt(r['Пошив, руб.'])}</td>`;
    html += `<td>${fmt(r['Раскрой, руб.'])}</td>`;
    html += `<td>${fmt(r['Декор, руб.'])}</td>`;
    html += `<td>${fmt(r['Вязание, руб.'])}</td>`;
    html += `<td>${fmt(r['Себестоимость, руб.'])}</td>`;
    html += `<td>${fmt(r['Наценка, руб.'])}</td>`;
    html += `<td>${r['Наценка, %']}</td>`;
    html += `<td>${r['Маржинальность, %']}</td>`;
    html += '</tr>';
  }

  html += `</tbody></table></div>`;
  html += `<div class="count">Найдено строк: ${detailsFilteredData.value.length}</div>`;
  html += `</div></body></html>`;

  w.document.write(html);
  w.document.close();
}

// ── Price levels & save ─────────────────────────────────────────────────────

const priceLevels = ref<PriceLevel[]>([]);
const changedRows = reactive<Set<number>>(new Set());
const saving = ref(false);

async function loadPriceLevels() {
  try {
    priceLevels.value = await $fetch<PriceLevel[]>(
      `${apiBase.value}/api/cost/price-levels`,
      { headers: fetchHeaders.value }
    );
  } catch (e: any) {
    console.error("[cost] price-levels load failed", e);
  }
}

const onPriceLevelChange = async (absoluteIdx: number, levelName: string) => {
  if (!levelName) return;
  const level = priceLevels.value.find((l) => l.name === levelName);
  if (!level) return;
  const row = allAggregated.value[absoluteIdx];
  if (!row) return;

  row["Уровень цен"] = levelName;
  row["avg_Розничная цена по уровню, руб."] = level.price_type3;
  row["avg_Отпускная цена по уровню, руб"] = level.price_type1;

  changedRows.add(absoluteIdx);

  try {
    const r = await $fetch<{ mock?: boolean }>(`${apiBase.value}/api/cost/save-changes`, {
      method: "POST",
      body: {
        model: row["Модель"],
        articul: row["Артикул"],
        price_level: levelName,
        retail_rub: level.price_type3,
        wholesale_rub: level.price_type1,
      },
      headers: fetchHeaders.value,
    });
    if (r?.mock) mockMode.value = true;
  } catch (e: any) {
    console.error("[cost] save-changes failed", e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  }
};

const saveAllChanges = async () => {
  if (!changedRows.size) return;
  saving.value = true;
  try {
    const changes = Array.from(changedRows).map((idx) => {
      const row = allAggregated.value[idx];
      return {
        model: row["Модель"],
        articul: row["Артикул"],
        price_level: row["Уровень цен"],
        retail_rub: row["avg_Розничная цена по уровню, руб."],
        wholesale_rub: row["avg_Отпускная цена по уровню, руб"],
      };
    });
    const result = await $fetch<{ success: boolean; count: number; error?: string; mock?: boolean }>(
      `${apiBase.value}/api/cost/save-batch`,
      { method: "POST", body: { changes }, headers: fetchHeaders.value }
    );
    if (result.mock) mockMode.value = true;
    if (result.success) {
      alert(`Сохранено ${result.count} записей${result.mock ? " (mock-режим)" : ""}`);
      changedRows.clear();
    } else {
      alert("Ошибка: " + (result.error || "unknown"));
    }
  } catch (e: any) {
    console.error("[cost] save-batch failed", e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    saving.value = false;
  }
};

// ── Utilities ───────────────────────────────────────────────────────────────

const fmt = (v: any): string => {
  if (v === null || v === undefined || v === "") return "—";
  const n = Number(v);
  if (Number.isNaN(n)) return String(v);
  return n.toLocaleString("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
};

const formatDate = (v: any): string => {
  if (!v) return "—";
  const s = String(v);
  return s.includes("T") ? s.split("T")[0] : s;
};

const calc = (row: any) => {
  const wholesaleRub = Number(row["avg_Отпускная цена по уровню, руб"] || 0);
  const costRub = Number(row["sum_Себестоимость, руб."] || 0);
  const markupRub = wholesaleRub - costRub;
  const markupPct = costRub > 0 ? (markupRub / costRub) * 100 : 0;
  const marginPct = wholesaleRub > 0 ? (markupRub / wholesaleRub) * 100 : 0;
  return { markupRub, markupPct, marginPct };
};

// ── Excel export ────────────────────────────────────────────────────────────

const headers = [
  "", "Бренд-менеджер", "Модель", "Артикул", "Страна", "Семья", "Сезон",
  "Дата", "Уровень цен",
  "Сред. розница (руб)", "Сред. опт (руб)", "Сред. розница ($)", "Сред. опт ($)",
  "Осн. материалы (руб)", "Осн. материалы ($)",
  "Вспом. материалы (руб)", "Вспом. материалы ($)",
  "Пошив (руб)", "Пошив ($)", "Раскрой (руб)", "Раскрой ($)",
  "Декоры (руб)", "Декоры ($)", "Себест. (руб)", "Себест. ($)",
  "Наценка (руб)", "Наценка (%)", "Маржа (%)",
];

const exportToExcel = () => {
  if (!totalAllRecords.value) return;
  let html = '<table border="1"><tr>';
  headers.forEach((h) => (html += `<th>${h}</th>`));
  html += "</tr>";
  for (const row of allAggregated.value) {
    const c = calc(row);
    const cells = [
      "",
      row["Бренд-менеджер"] || "",
      row["Модель"] || "",
      row["Артикул"] || "",
      row["Страна пр-ва"] || "",
      row["Семья"] || "",
      row["Сезон"] || "",
      formatDate(row["дата расчета"]),
      row["Уровень цен"] || "",
      fmt(row["avg_Розничная цена по уровню, руб."]),
      fmt(row["avg_Отпускная цена по уровню, руб"]),
      fmt(row["avg_Розничная цена по уровню, USD."]),
      fmt(row["avg_Отпускная цена по уровню, USD."]),
      fmt(row["sum_Основные материалы, руб."]),
      fmt(row["sum_Основные материалы, USD."]),
      fmt(row["sum_Вспомогательные материалы, руб."]),
      fmt(row["sum_Вспомогательные материалы, USD."]),
      fmt(row["avg_Пошив, руб."]),
      fmt(row["avg_Пошив, USD."]),
      fmt(row["avg_Раскрой, руб."]),
      fmt(row["avg_Раскрой, USD."]),
      fmt(row["sum_Декоры, руб."]),
      fmt(row["sum_Декоры, USD."]),
      fmt(row["sum_Себестоимость, руб."]),
      fmt(row["sum_Себестоимость, USD."]),
      fmt(c.markupRub),
      `${c.markupPct.toFixed(1)}%`,
      `${c.marginPct.toFixed(1)}%`,
    ];
    html += "<tr>" + cells.map((v) => `<td>${v}</td>`).join("") + "</tr>";
  }
  html += "</table>";
  const blob = new Blob([html], { type: "application/vnd.ms-excel" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `CostHistory_${new Date().toISOString().slice(0, 10)}.xls`;
  a.click();
  URL.revokeObjectURL(url);
};

// Ctrl+C при пустом выделении — копируем всю таблицу как TSV
const onCopyShortcut = (e: KeyboardEvent) => {
  if (!(e.ctrlKey && (e.key === "c" || e.key === "C"))) return;
  if (window.getSelection()?.toString()) return;
  const table = document.getElementById("cost-table-1");
  if (!table) return;
  e.preventDefault();
  const rows = table.querySelectorAll("tr");
  let tsv = "";
  rows.forEach((r) => {
    const cells = r.querySelectorAll("td, th");
    const line: string[] = [];
    cells.forEach((c) => line.push((c as HTMLElement).innerText.replace(/\n/g, " ").trim()));
    tsv += line.join("\t") + "\n";
  });
  navigator.clipboard?.writeText(tsv);
};

// ── Lifecycle ───────────────────────────────────────────────────────────────

watch(currentPage, () => {
  selectedRowIndex.value = -1;
});

onMounted(async () => {
  await Promise.all([loadFilters(), loadPriceLevels()]);
});
</script>

<style scoped>
.page-cost { padding-top: var(--sp-2); }

.page-subtitle {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
}
.ctx-sep { color: var(--text-muted); }
.mock-pill {
  display: inline-flex;
  align-items: center;
  font-size: var(--fs-2xs);
  padding: 2px 8px;
  border-radius: var(--rd-pill);
  background: color-mix(in srgb, var(--warn, #d97706) 14%, var(--bg-surface));
  color: var(--warn, #b45309);
  font-weight: var(--fw-semibold);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.filters-card {
  margin-bottom: var(--sp-4);
  overflow: visible;
}
.card-actions {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
}
.filters-body { position: relative; padding: var(--sp-5) var(--sp-6); }
.filters-body.is-busy { pointer-events: none; opacity: 0.6; }
.filters-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--sp-3);
  background: rgba(255, 255, 255, 0.55);
  backdrop-filter: blur(2px);
  font-size: var(--fs-sm);
  color: var(--text-muted);
}

.dates-row {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  margin-bottom: var(--sp-5);
}
.dates-row label {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
  font-size: var(--fs-sm);
  color: var(--text-muted);
}
.dates-row input {
  height: 32px;
  padding: 0 var(--sp-3);
  border: 1px solid var(--border);
  background: var(--bg-surface);
  border-radius: var(--rd-2);
}

.filters-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: var(--sp-4);
}
.filter-item label {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  display: block;
  margin-bottom: var(--sp-2);
}

/* Action bar */
.cost-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-4);
  padding: var(--sp-3) var(--sp-5);
  background: var(--bg-surface-2, var(--bg-surface));
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  margin-bottom: var(--sp-5);
  flex-wrap: wrap;
}
.cost-info {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  font-size: var(--fs-sm);
}
.cost-info strong { color: var(--text-strong); font-weight: var(--fw-semibold); }
.cost-info .info-ok { color: var(--pos); }
.cost-info .info-muted { color: var(--text-muted); }
.cost-info .spinning { animation: cost-spin 0.9s linear infinite; }
.cost-actions-buttons {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
}

/* Modal */
.modal-wide { width: 95vw; max-width: 1600px; }
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 10000;
  display: flex;
  justify-content: center;
  align-items: center;
}
.modal-content {
  background: var(--bg-surface);
  border-radius: var(--rd-3);
  max-height: 95vh;
  display: flex;
  flex-direction: column;
}
.modal-header {
  padding: var(--sp-4) var(--sp-5);
  border-bottom: 1px solid var(--border);
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-shrink: 0;
}
.modal-header h2 { margin: 0; font-size: var(--fs-lg); }
.modal-header-actions { display: flex; gap: var(--sp-3); align-items: center; }
.modal-close {
  border: none;
  background: transparent;
  font-size: 24px;
  cursor: pointer;
  color: var(--text-muted);
  padding: 0 4px;
}
.modal-close:hover { color: var(--text-strong); }
.form-input {
  height: 36px;
  padding: 0 var(--sp-3);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  background: var(--bg-surface);
  color: var(--text-strong);
  font-size: var(--fs-sm);
}

.details-filters { padding: var(--sp-4) var(--sp-5); background: var(--bg-surface-2); border-bottom: 1px solid var(--border); display: flex; gap: var(--sp-4); align-items: flex-end; flex-wrap: wrap; flex-shrink: 0; }
.details-filters label { font-size: var(--fs-xs); font-weight: var(--fw-medium); display: flex; flex-direction: column; gap: 4px; color: var(--text-muted); }
.details-filters input[type="date"] { height: 32px; padding: 0 var(--sp-3); border: 1px solid var(--border); border-radius: var(--rd-2); background: var(--bg-surface); }
select.form-select-multi { height: 60px; min-width: 180px; border: 1px solid var(--border); border-radius: var(--rd-2); background: var(--bg-surface); font-size: var(--fs-xs); }

.details-col-filters { padding: var(--sp-3) var(--sp-5); border-bottom: 1px solid var(--border); display: flex; gap: var(--sp-3); align-items: flex-start; flex-wrap: wrap; flex-shrink: 0; }
.details-col-filter-item { display: flex; flex-direction: column; gap: 2px; }
.details-col-filter-item label { font-size: 10px; color: var(--text-muted); font-weight: var(--fw-medium); }
.details-col-filter-item select { min-width: 120px; max-width: 180px; height: 50px; border: 1px solid var(--border); border-radius: var(--rd-2); background: var(--bg-surface); font-size: 10px; }
.details-col-filter-reset { font-size: var(--fs-xs); color: var(--text-muted); padding-top: 16px; white-space: nowrap; }
.details-col-filter-reset:hover { color: var(--text-strong); }

.details-table-wrap { overflow: auto; flex: 1; }
.details-count { padding: var(--sp-2) var(--sp-5); font-size: var(--fs-xs); color: var(--text-muted); border-top: 1px solid var(--border); flex-shrink: 0; }
.btn-details { background: none; border: none; cursor: pointer; padding: 2px 6px; font-size: 14px; }
.btn-details:hover { opacity: 0.7; }

/* Error banner */
.cost-error {
  display: flex;
  align-items: flex-start;
  gap: var(--sp-3);
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-3);
  background: color-mix(in srgb, var(--neg) 8%, var(--bg-surface));
  border: 1px solid color-mix(in srgb, var(--neg) 30%, transparent);
  color: var(--text-strong);
  margin-bottom: var(--sp-5);
  position: relative;
}
.cost-error :deep(svg) { color: var(--neg); flex-shrink: 0; margin-top: 2px; }
.cost-error p { margin-top: 4px; font-size: var(--fs-sm); }
.cost-error .muted { color: var(--text-muted); font-size: var(--fs-xs); margin-top: 6px; }
.cost-error code {
  background: var(--bg-surface-3);
  padding: 1px 5px;
  border-radius: var(--rd-1);
  font-family: var(--font-mono);
  font-size: 90%;
}
.cost-error-x {
  position: absolute;
  top: 6px;
  right: 6px;
  border: none;
  background: transparent;
  font-size: 18px;
  cursor: pointer;
  color: var(--text-muted);
  padding: 4px 8px;
  border-radius: var(--rd-1);
}
.cost-error-x:hover { color: var(--text-strong); background: var(--bg-surface-3); }

/* Tables */
.data-table tr.selected { background: var(--bg-surface-3); }
.data-table tbody tr { cursor: pointer; }
.data-table .price-select {
  height: 26px;
  border: 1px solid var(--border);
  background: var(--bg-surface);
  border-radius: var(--rd-2);
  font-size: var(--fs-xs);
  padding: 0 4px;
  max-width: 180px;
  color: var(--text-strong);
}

.loader {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  border: 2px solid var(--border);
  border-top-color: var(--accent);
  animation: cost-spin 0.8s linear infinite;
}
@keyframes cost-spin {
  to { transform: rotate(360deg); }
}

/* Column filters */
.col-filters {
  display: flex;
  gap: var(--sp-3);
  align-items: flex-start;
  flex-wrap: wrap;
  padding: var(--sp-3) var(--sp-5);
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface-2, var(--bg-surface));
  flex-shrink: 0;
}
.col-filters.is-active {
  background: color-mix(in srgb, var(--accent) 6%, var(--bg-surface-2, var(--bg-surface)));
  border-left: 3px solid var(--accent);
}
.col-filter-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 140px;
  max-width: 200px;
  flex: 1;
}
.col-filter-item label {
  font-size: 10px;
  color: var(--text-muted);
  font-weight: var(--fw-medium);
}
.col-filter-reset {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  padding-top: 16px;
  white-space: nowrap;
  text-decoration: none;
}
.col-filter-reset:hover {
  color: var(--accent);
  text-decoration: underline;
}
</style>
