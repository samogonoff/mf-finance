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
        <label class="filter-label">Валюта</label>
        <div class="chip-row" role="radiogroup" aria-label="Линза представления суммы">
          <button
            v-for="l in options?.lenses || []"
            :key="l"
            type="button"
            role="radio"
            :aria-checked="filters.lens === l"
            class="chip"
            :class="{ active: filters.lens === l }"
            :title="l"
            @click="filters.lens = l"
          >{{ lensLabel(l) }}</button>
        </div>
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
import { useDebtReport, DEBT_LENS_DEFAULT, type DebtFilterOptions, type DebtReportResponse, type DebtReportFilters } from "~/composables/useDebtReport";
import { useDebtFilters, type DebtSavedFilter } from "~/composables/useDebtFilters";
import DebtTable from "~/components/reports/DebtTable.vue";
import DebtMultiSelect from "~/components/reports/DebtMultiSelect.vue";

const { filterOptions, report: fetchReport, drilldown } = useDebtReport();
const { list: listPresets, create: createPreset, remove: removePreset } = useDebtFilters();

// Deep-link: состояние отчёта живёт в URL, чтобы перезагрузка/шаринг открывали
// тот же экран. Пишем канонический query при каждом «Сформировать», читаем — на mount.
const route = useRoute();
const router = useRouter();

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
  // lens — линза представления суммы; дефолт «В валюте договора» (native).
  lens: DEBT_LENS_DEFAULT,
  // Отчёт всегда ВГО — ВГО-фильтр теперь безусловный на бэке (vgoMSSQLClause/
  // vgoCHClause), галки в UI нет. Поле оставлено для совместимости API/пресетов
  // и бэком игнорируется.
  only_ico: true
});

// Короткие подписи для кнопок линзы (в источнике строки длинные).
const lensLabel = (l: string): string => {
  switch (l) {
    case "В валюте договора": return "Валюта договора";
    case "В бел. рублях": return "BYN";
    case "В долларах США": return "USD";
    default: return l;
  }
};

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
      lens: DEBT_LENS_DEFAULT,
      only_ico: true
    }
  }
];
const isSystemPreset = computed(() => Number(selectedPreset.value) < 0);

const report = ref<DebtReportResponse | null>(null);
const loading = ref(false);
const errorMessage = ref<string>("");

const syncDates = () => {
  filters.date_from = dateFrom.value;
  filters.date_to = dateTo.value;
};

// --- URL <-> filters (deep-link) -------------------------------------------
const csv = (v: unknown): string[] =>
  typeof v === "string" && v.length
    ? v.split(",").map((s) => s.trim()).filter(Boolean)
    : [];

// Гидратация фильтров из query при заходе по ссылке/перезагрузке. Возвращает
// true, если в URL были параметры (чтобы не перетирать их дефолтами).
const hydrateFromQuery = (): boolean => {
  const q = route.query;
  if (Object.keys(q).length === 0) return false;
  if (typeof q.from === "string" && q.from) dateFrom.value = q.from;
  if (typeof q.to === "string" && q.to) dateTo.value = q.to;
  syncDates();
  if ("entities" in q) filters.entity_inns = csv(q.entities);
  if ("accounts" in q) filters.accounts = csv(q.accounts);
  if (typeof q.lens === "string" && q.lens) filters.lens = q.lens;
  return true;
};

// Запись текущих фильтров в URL (replace — без лишних записей в history).
// keepExpansion=true (заход по ссылке/перезагрузка) сохраняет exp-параметр
// раскрытия дерева — его владелец DebtTable восстановит при монтировании.
// Ручное «Сформировать» строит новое дерево, поэтому exp сбрасывается.
const writeQuery = (keepExpansion: boolean) => {
  const q: Record<string, string> = {
    from: filters.date_from,
    to: filters.date_to
  };
  if (filters.entity_inns.length) q.entities = filters.entity_inns.join(",");
  if (filters.accounts.length) q.accounts = filters.accounts.join(",");
  if (filters.lens && filters.lens !== DEBT_LENS_DEFAULT) q.lens = filters.lens;
  if (keepExpansion && typeof route.query.exp === "string") q.exp = route.query.exp;
  // duplicate-navigation отвергается роутером — гасим, это не ошибка.
  router.replace({ query: q }).catch(() => {});
};

// keepExpansion прокидываем только из onMounted; из @click приходит MouseEvent
// (не объект с keepExpansion) → раскрытие сбрасывается, как и задумано.
const runReport = async (opts?: { keepExpansion?: boolean }) => {
  syncDates();
  writeQuery(opts?.keepExpansion === true);
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

// Документы drill-down тянем в той же линзе, что и свод — иначе суммы разъедутся.
const drilldownFn = (q: any) => drilldown({ ...q, lens: filters.lens });

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
  // Старые пресеты не имеют lens (был мультивыбор currencies) — дефолтим.
  filters.lens = (p.payload as { lens?: string }).lens || DEBT_LENS_DEFAULT;
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
  // Сначала восстанавливаем состояние из URL (deep-link), потом грузим справочники.
  hydrateFromQuery();
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
  await runReport({ keepExpansion: true });
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
