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

    <!-- Action bar (как в оригинале — между фильтрами и таблицей) -->
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
            Показано {{ pageRange }} из {{ totalAllRecords.toLocaleString("ru-RU") }}
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
      <div class="table-wrap" @keydown="onCopyShortcut" tabindex="0">
        <table id="cost-table-1" class="data-table compact">
          <thead>
            <tr>
              <th>Бренд-менеджер</th>
              <th>Модель</th>
              <th>Артикул</th>
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
              <td colspan="24" class="muted" style="text-align: center; padding: 24px">
                Загрузка данных…
              </td>
            </tr>
            <tr v-else-if="!pageRows.length">
              <td colspan="24" class="muted" style="text-align: center; padding: 24px">
                Нет данных. Загрузите данные кнопкой выше.
              </td>
            </tr>
            <tr
              v-for="(row, idx) in pageRows"
              :key="idx"
              :class="{ selected: selectedRowIndex === pageStart + idx }"
              @click="selectRow(pageStart + idx)"
            >
              <td>{{ row['Бренд-менеджер'] || '—' }}</td>
              <td>{{ row['Модель'] || '—' }}</td>
              <td>{{ row['Артикул'] || '—' }}</td>
              <td class="num">{{ formatDate(row['дата расчета']) }}</td>
              <td>
                <select
                  class="price-select"
                  :value="row['Уровень цен'] || ''"
                  @click.stop
                  @change="onPriceLevelChange(pageStart + idx, ($event.target as HTMLSelectElement).value)"
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

    <!-- Таблица 2: детали -->
    <section class="card">
      <div class="card-header">
        <div>
          <div class="card-title">Детализация</div>
          <div class="card-subtitle">
            <template v-if="detailsContext">
              {{ detailsContext.model }} · {{ detailsContext.articul }}
            </template>
            <template v-else>Выделите строку в таблице выше</template>
          </div>
        </div>
      </div>
      <div class="table-wrap">
        <table class="data-table compact">
          <thead>
            <tr>
              <th>Модель</th>
              <th>Артикул</th>
              <th>Наименование</th>
              <th class="col-num">Цена материала (руб)</th>
              <th class="col-num">Пошив (руб)</th>
              <th class="col-num">Раскрой (руб)</th>
              <th class="col-num">Вязание (руб)</th>
              <th class="col-num">Себест. (руб)</th>
              <th class="col-num">Себест. ($)</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="detailsLoading">
              <td colspan="9" class="muted" style="text-align: center; padding: 16px">
                Загрузка деталей…
              </td>
            </tr>
            <tr v-else-if="!details.length">
              <td colspan="9" class="muted" style="text-align: center; padding: 16px">
                Нет данных
              </td>
            </tr>
            <tr v-for="(d, i) in details" :key="i">
              <td>{{ d['Модель'] || '—' }}</td>
              <td>{{ d['Артикул'] || '—' }}</td>
              <td>{{ d['Наименование модели'] || '—' }}</td>
              <td class="col-num num">{{ fmt(d['цена материала, руб.']) }}</td>
              <td class="col-num num">{{ fmt(d['Пошив, руб.']) }}</td>
              <td class="col-num num">{{ fmt(d['Раскрой, руб.']) }}</td>
              <td class="col-num num">{{ fmt(d['Вязание, руб.']) }}</td>
              <td class="col-num num-strong">{{ fmt(d['Себестоимость, руб.']) }}</td>
              <td class="col-num num">{{ fmt(d['Себестоимость, USD.']) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";

interface PriceLevel { name: string; price_type1: number; price_type3: number }
type FilterKey =
  | "brand_manager" | "level01" | "level02" | "level03" | "level04" | "level05"
  | "country" | "family" | "season" | "calc_sign" | "model" | "articul";

const filterConfig: { key: FilterKey; label: string }[] = [
  { key: "brand_manager", label: "Бренд-менеджер" },
  { key: "level01", label: "Level 01" },
  { key: "level02", label: "Level 02" },
  { key: "level03", label: "Level 03" },
  { key: "level04", label: "Level 04" },
  { key: "level05", label: "Level 05" },
  { key: "country", label: "Страна пр-ва" },
  { key: "family", label: "Семья" },
  { key: "season", label: "Сезон" },
  { key: "calc_sign", label: "Признак калькуляции" },
  { key: "model", label: "Модель" },
  { key: "articul", label: "Артикул" }
];

const config = useRuntimeConfig();
const apiBase = computed(() =>
  config.public.costOnly ? "" : ((config.public.apiBase as string) || "")
);
const apiHostLabel = computed(() => apiBase.value || "локального API");

const { user } = useAuth();
const xUsername = computed<string>(() => {
  const u: any = user.value;
  return (u?.email || u?.name || "cost-dev") as string;
});
const fetchHeaders = computed(() => ({ "X-Username": xUsername.value }));

const filterOptions = ref<Record<FilterKey, string[]>>({} as any);
const selected = reactive<Record<FilterKey, string[]>>(
  Object.fromEntries(filterConfig.map((f) => [f.key, [] as string[]])) as any
);
const dateFrom = ref("");
const dateTo = ref("");
const cascadeBusy = ref(false);
const loading = ref(false);
const lastError = ref("");
const mockMode = ref(false);

const allAggregated = ref<any[]>([]);
const totalAllRecords = ref(0);
const pageSize = 50;
const currentPage = ref(0);
const totalPages = computed(() => Math.max(1, Math.ceil(totalAllRecords.value / pageSize)));
const pageStart = computed(() => currentPage.value * pageSize);
const pageRows = computed(() => allAggregated.value.slice(pageStart.value, pageStart.value + pageSize));
const pageRange = computed(() => {
  if (!totalAllRecords.value) return "0";
  const a = pageStart.value + 1;
  const b = Math.min(pageStart.value + pageSize, totalAllRecords.value);
  return `${a}–${b}`;
});

const selectedRowIndex = ref<number>(-1);
const details = ref<any[]>([]);
const detailsContext = ref<{ model: string; articul: string } | null>(null);
const detailsLoading = ref(false);

const priceLevels = ref<PriceLevel[]>([]);
const changedRows = reactive<Set<number>>(new Set());
const saving = ref(false);

// ── Утилиты ─────────────────────────────────────────────────────────────────

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
const errMsg = (e: any) =>
  e?.data?.detail || e?.data?.error || e?.statusMessage || e?.message || String(e);

const buildFilters = () => {
  const f: any = {};
  if (dateFrom.value) f.date_from = dateFrom.value;
  if (dateTo.value) f.date_to = dateTo.value;
  for (const k of filterConfig.map((c) => c.key)) {
    const v = selected[k];
    if (v?.length) f[k] = v;
  }
  return f;
};

// ── Загрузка фильтров и каскад ──────────────────────────────────────────────

const loadFilters = async () => {
  cascadeBusy.value = true;
  try {
    filterOptions.value = await $fetch<Record<FilterKey, string[]>>(
      `${apiBase.value}/api/cost/filter-options`,
      { headers: fetchHeaders.value }
    );
    lastError.value = "";
  } catch (e: any) {
    lastError.value = errMsg(e);
    console.error("[cost] filter-options failed", e);
  } finally {
    cascadeBusy.value = false;
  }
};

const onFilterChange = async (changedKey: FilterKey) => {
  const params = new URLSearchParams();
  for (const [k, vals] of Object.entries(selected)) {
    (vals as string[]).forEach((v) => params.append(k, v));
  }
  cascadeBusy.value = true;
  try {
    const opts = await $fetch<Record<FilterKey, string[]>>(
      `${apiBase.value}/api/cost/filter-options?${params}`,
      { headers: fetchHeaders.value }
    );
    for (const k of filterConfig.map((c) => c.key)) {
      if (k === changedKey) continue;
      filterOptions.value[k] = opts[k] || [];
      selected[k] = (selected[k] || []).filter((v: string) => filterOptions.value[k].includes(v));
    }
    lastError.value = "";
  } catch (e: any) {
    lastError.value = errMsg(e);
    console.error("[cost] cascade failed", e);
  } finally {
    cascadeBusy.value = false;
  }
};

const resetFilters = async () => {
  dateFrom.value = "";
  dateTo.value = "";
  for (const k of filterConfig.map((c) => c.key)) selected[k] = [];
  allAggregated.value = [];
  totalAllRecords.value = 0;
  currentPage.value = 0;
  details.value = [];
  detailsContext.value = null;
  selectedRowIndex.value = -1;
  changedRows.clear();
  await loadFilters();
};

// ── Aggregated / details ────────────────────────────────────────────────────

const loadData = async () => {
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
    details.value = [];
    detailsContext.value = null;
    changedRows.clear();
    lastError.value = "";
  } catch (e: any) {
    console.error("[cost] aggregated load failed", e);
    lastError.value = errMsg(e);
  } finally {
    loading.value = false;
  }
};

const prevPage = () => { if (currentPage.value > 0) currentPage.value -= 1; };
const nextPage = () => { if (currentPage.value < totalPages.value - 1) currentPage.value += 1; };

const selectRow = async (absoluteIdx: number) => {
  selectedRowIndex.value = absoluteIdx;
  const row = allAggregated.value[absoluteIdx];
  if (!row) return;
  const model = String(row["Модель"] || "").trim();
  const articul = String(row["Артикул"] || "").trim();
  if (!model || !articul) return;

  detailsContext.value = { model, articul };
  detailsLoading.value = true;
  try {
    const result = await $fetch<{ data: any[] }>(
      `${apiBase.value}/api/cost/details`,
      { method: "POST", body: { model, articul }, headers: fetchHeaders.value }
    );
    details.value = result.data || [];
  } catch (e: any) {
    console.error("[cost] details load failed", e);
    details.value = [];
    lastError.value = errMsg(e);
  } finally {
    detailsLoading.value = false;
  }
};

// ── Price levels & save ─────────────────────────────────────────────────────

const loadPriceLevels = async () => {
  try {
    priceLevels.value = await $fetch<PriceLevel[]>(
      `${apiBase.value}/api/cost/price-levels`,
      { headers: fetchHeaders.value }
    );
  } catch (e: any) {
    console.error("[cost] price-levels load failed", e);
  }
};

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
        wholesale_rub: level.price_type1
      },
      headers: fetchHeaders.value
    });
    if (r?.mock) mockMode.value = true;
  } catch (e: any) {
    console.error("[cost] save-changes failed", e);
    lastError.value = errMsg(e);
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
        wholesale_rub: row["avg_Отпускная цена по уровню, руб"]
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
    lastError.value = errMsg(e);
  } finally {
    saving.value = false;
  }
};

// ── Excel export ────────────────────────────────────────────────────────────

const headers = [
  "Бренд-менеджер", "Модель", "Артикул", "Дата", "Уровень цен",
  "Сред. розница (руб)", "Сред. опт (руб)", "Сред. розница ($)", "Сред. опт ($)",
  "Осн. материалы (руб)", "Осн. материалы ($)",
  "Вспом. материалы (руб)", "Вспом. материалы ($)",
  "Пошив (руб)", "Пошив ($)", "Раскрой (руб)", "Раскрой ($)",
  "Декоры (руб)", "Декоры ($)", "Себест. (руб)", "Себест. ($)",
  "Наценка (руб)", "Наценка (%)", "Маржа (%)"
];

const exportToExcel = () => {
  if (!totalAllRecords.value) return;
  let html = '<table border="1"><tr>';
  headers.forEach((h) => (html += `<th>${h}</th>`));
  html += "</tr>";
  for (const row of allAggregated.value) {
    const c = calc(row);
    const cells = [
      row["Бренд-менеджер"] || "",
      row["Модель"] || "",
      row["Артикул"] || "",
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
      `${c.marginPct.toFixed(1)}%`
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
  if (
    selectedRowIndex.value < pageStart.value ||
    selectedRowIndex.value >= pageStart.value + pageSize
  ) {
    selectedRowIndex.value = -1;
  }
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

.filters-card { margin-bottom: var(--sp-4); }
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

/* Action bar — между фильтрами и таблицей, как в оригинале */
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
</style>
