<template>
  <div class="debt-report">
    <div class="filters-bar">
      <div class="filter">
        <label class="filter-label">Период</label>
        <div class="period-pickers">
          <input v-model="dateFrom" type="date" class="select" />
          <span class="period-dash">—</span>
          <input v-model="dateTo" type="date" class="select" />
        </div>
      </div>

      <div class="filter filter-grow">
        <label class="filter-label">Юрлица ({{ filters.entity_inns.length || "все" }})</label>
        <DebtMultiSelect
          v-model="filters.entity_inns"
          :options="entityOptions"
          placeholder="Все юрлица ГК МФ"
        />
      </div>

      <div class="filter filter-grow">
        <label class="filter-label">Счета БУ ({{ filters.accounts.length || "все" }})</label>
        <DebtMultiSelect
          v-model="filters.accounts"
          :options="accountOptions"
          placeholder="Все счета"
        />
      </div>

      <div class="filter">
        <label class="filter-label">Валюты</label>
        <div class="chip-row">
          <button
            v-for="c in options?.currencies || []"
            :key="c"
            type="button"
            class="chip"
            :class="{ active: filters.currencies.includes(c) }"
            @click="toggleCurrency(c)"
          >{{ c }}</button>
        </div>
      </div>

      <div class="filter filter-ico">
        <label class="filter-label">Сегмент</label>
        <label class="checkbox-row" title="Premaster.ICO = 1 — операции между компаниями ГК">
          <input v-model="filters.only_ico" type="checkbox" />
          <span>Только внутригрупповые (ВГО)</span>
        </label>
      </div>
    </div>

    <div class="actions-bar">
      <button class="btn btn-primary" :disabled="loading" @click="runReport">
        <Icon name="lucide:search" /> Сформировать
      </button>

      <div class="presets">
        <select v-model="selectedPreset" class="select" style="width: 260px">
          <option value="">— сохранённые пресеты —</option>
          <optgroup label="Шаблоны (CH-снэпшот)">
            <option v-for="p in systemPresets" :key="p.id" :value="String(p.id)">{{ p.name }}</option>
          </optgroup>
          <optgroup v-if="presets.length" label="Мои пресеты">
            <option v-for="p in presets" :key="p.id" :value="String(p.id)">{{ p.name }}</option>
          </optgroup>
        </select>
        <button class="btn btn-ghost" :disabled="!selectedPreset" @click="applyPreset">Применить</button>
        <button class="btn btn-ghost" :disabled="!selectedPreset || isSystemPreset" @click="deletePreset">Удалить</button>
        <button class="btn btn-ghost" @click="savePresetPrompt">
          <Icon name="lucide:save" /> Сохранить как…
        </button>
      </div>
    </div>

    <div v-if="errorMessage" class="alert alert-error">{{ errorMessage }}</div>
    <div v-if="loading" class="loading">Загружаем отчёт…</div>

    <DebtTable
      v-if="report && !loading"
      :rows="report.rows"
      :report-date="report.report_date"
      :date-from="filters.date_from"
      :date-to="filters.date_to"
      :drilldown="drilldownFn"
    />
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, computed } from "vue";
import { useDebtReport, type DebtFilterOptions, type DebtReportResponse, type DebtReportFilters } from "~/composables/useDebtReport";
import { useDebtFilters, type DebtSavedFilter } from "~/composables/useDebtFilters";
import DebtTable from "~/components/reports/DebtTable.vue";
import DebtMultiSelect from "~/components/reports/DebtMultiSelect.vue";

const { filterOptions, report: fetchReport, drilldown } = useDebtReport();
const { list: listPresets, create: createPreset, remove: removePreset } = useDebtFilters();

// период по умолчанию — последние 2 дня (today-1 → today). Premaster1C тяжело
// отдаёт большие диапазоны, дефолт держим минимальным, чтобы первый запрос
// гарантированно отвечал; пользователь расширит при необходимости.
const today = new Date();
const yyyymmdd = (d: Date) => d.toISOString().slice(0, 10);
const defaultDateFrom = new Date(today);
defaultDateFrom.setDate(defaultDateFrom.getDate() - 1);

const dateFrom = ref(yyyymmdd(defaultDateFrom));
const dateTo = ref(yyyymmdd(today));

const filters = reactive<DebtReportFilters>({
  date_from: dateFrom.value,
  date_to: dateTo.value,
  entity_inns: [],
  accounts: [],
  currencies: [],
  // Level 1 MVP: дефолт «только ВГО» включён до подтверждения от автора ТЗ
  // (см. docs/reports/debt/open-questions.md §A3).
  only_ico: true
});

const options = ref<DebtFilterOptions | null>(null);
const entityOptions = computed(() =>
  (options.value?.entities || []).map((e) => ({
    value: e.inn,
    label: `${e.name} (${e.country})`
  }))
);
const accountOptions = computed(() =>
  (options.value?.accounts || []).map((a) => ({
    value: a.code,
    label: `${a.code} — ${a.name}${a.country ? ` (${a.country})` : ""}`
  }))
);

const presets = ref<DebtSavedFilter[]>([]);
const selectedPreset = ref<string>("");

// Системные пресеты — захардкожены под текущее наполнение CH-снэпшота. ID
// отрицательные, чтобы не путаться с user-saved (BIGSERIAL → положительные).
// Если в bootstrap'е залиты другие ЮЛ — расширь список вручную.
const systemPresets = [
  {
    id: -1,
    name: "ПТИР + ТЭКС (CH-снэпшот)",
    payload: {
      date_from: "2025-01-01",
      date_to: dateTo.value,
      entity_inns: ["9731039708", "5031159833"],
      accounts: [] as string[],
      currencies: [] as string[],
      only_ico: true
    }
  }
];
const isSystemPreset = computed(() => Number(selectedPreset.value) < 0);

const report = ref<DebtReportResponse | null>(null);
const loading = ref(false);
const errorMessage = ref<string>("");

const toggleCurrency = (c: string) => {
  if (filters.currencies.includes(c)) {
    filters.currencies = filters.currencies.filter((x) => x !== c);
  } else {
    filters.currencies = [...filters.currencies, c];
  }
};

const syncDates = () => {
  filters.date_from = dateFrom.value;
  filters.date_to = dateTo.value;
};

const runReport = async () => {
  syncDates();
  loading.value = true;
  errorMessage.value = "";
  try {
    report.value = await fetchReport({ ...filters });
  } catch (e: any) {
    errorMessage.value = e?.data?.error || e?.message || "Не удалось загрузить отчёт";
    report.value = null;
  } finally {
    loading.value = false;
  }
};

const drilldownFn = (q: any) => drilldown(q);

const refreshPresets = async () => {
  try {
    presets.value = await listPresets();
  } catch (e) {
    presets.value = [];
  }
};

const applyPreset = () => {
  // системные пресеты ищем по отрицательному id, user-saved — по положительному
  const all = [...systemPresets, ...presets.value];
  const p = all.find((x) => String(x.id) === selectedPreset.value);
  if (!p) return;
  dateFrom.value = p.payload.date_from;
  dateTo.value = p.payload.date_to;
  filters.date_from = p.payload.date_from;
  filters.date_to = p.payload.date_to;
  filters.entity_inns = [...(p.payload.entity_inns || [])];
  filters.accounts = [...(p.payload.accounts || [])];
  filters.currencies = [...(p.payload.currencies || [])];
  // Старые пресеты могут не иметь only_ico — подставляем Level 1 дефолт true.
  filters.only_ico = typeof p.payload.only_ico === "boolean" ? p.payload.only_ico : true;
};

const deletePreset = async () => {
  const id = Number(selectedPreset.value);
  if (!id) return;
  if (!window.confirm("Удалить пресет?")) return;
  try {
    await removePreset(id);
    selectedPreset.value = "";
    await refreshPresets();
  } catch (e: any) {
    errorMessage.value = e?.data?.error || "Не удалось удалить пресет";
  }
};

const savePresetPrompt = async () => {
  const name = window.prompt("Название пресета:");
  if (!name) return;
  syncDates();
  try {
    await createPreset(name, { ...filters });
    await refreshPresets();
  } catch (e: any) {
    errorMessage.value = e?.data?.error || "Не удалось сохранить пресет";
  }
};

onMounted(async () => {
  try {
    options.value = await filterOptions();
    // Level 1 MVP: бэк требует обязательный entity_inns, без него Report → 400.
    // По дефолту — только ТД («ООО ТД Марк Формэль», ИНН 6950135110): минимальная
    // нагрузка на Premaster1C, чтобы первый запрос отвечал быстро. Остальные
    // юрлица пользователь добавляет вручную.
    const DEFAULT_INN = "6950135110";
    if (options.value && filters.entity_inns.length === 0) {
      const hasTD = options.value.entities.some((e) => e.inn === DEFAULT_INN);
      filters.entity_inns = hasTD
        ? [DEFAULT_INN]
        : options.value.entities.map((e) => e.inn).slice(0, 1);
    }
  } catch (e: any) {
    errorMessage.value = e?.data?.error || "Не удалось загрузить справочники фильтров";
  }
  await refreshPresets();
  await runReport();
});
</script>

<style scoped>
.debt-report { display: flex; flex-direction: column; gap: var(--sp-4); }

.filters-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--sp-4);
  padding: var(--sp-4);
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
}
.filter { display: flex; flex-direction: column; gap: 4px; min-width: 200px; }
.filter-grow { flex: 1 1 280px; }
.filter-label {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.period-pickers { display: flex; align-items: center; gap: var(--sp-2); }
.period-dash { color: var(--text-muted); }
.chip-row { display: flex; flex-wrap: wrap; gap: 4px; }
.chip {
  background: transparent;
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  padding: 4px 10px;
  font-size: var(--fs-xs);
  cursor: pointer;
  font-family: inherit;
  color: var(--text-secondary);
}
.chip.active {
  background: var(--accent);
  color: var(--accent-on);
  border-color: var(--accent);
}
.chip:hover:not(.active) { background: var(--bg-surface-2); }

.filter-ico { min-width: 240px; }
.checkbox-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  font-size: var(--fs-sm);
  color: var(--text-strong);
  cursor: pointer;
  user-select: none;
}
.checkbox-row input[type="checkbox"] { cursor: pointer; }

.actions-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--sp-3);
}
.presets { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: wrap; }

.alert {
  padding: var(--sp-3) var(--sp-4);
  border-radius: var(--rd-2);
  font-size: var(--fs-sm);
}
.alert-error {
  background: rgba(239, 68, 68, 0.08);
  color: var(--neg);
  border: 1px solid rgba(239, 68, 68, 0.2);
}
.loading {
  padding: var(--sp-6);
  text-align: center;
  color: var(--text-muted);
  font-style: italic;
}
</style>
