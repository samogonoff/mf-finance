<template>
  <div class="page-approvals">
    <header class="page-header">
      <h1 class="page-title">Согласование цен</h1>
      <p class="page-subtitle">Утверждение изменений, предложенных бренд-менеджерами</p>
    </header>

    <!-- Action bar -->
    <div class="approval-actions">
      <div class="approval-info">
        <template v-if="loading">
          <span>Загрузка…</span>
        </template>
        <template v-else>
          <span>Всего ожидает согласования: <strong>{{ pendingChanges.length }}</strong></span>
        </template>
      </div>
      <div class="approval-buttons">
        <button class="btn btn-primary" :disabled="!selectedIds.length || applying" @click="applySelected">
          <Icon name="lucide:check-circle" />
          {{ applying ? 'Установка…' : `Установить цены (${selectedIds.length})` }}
        </button>
        <button class="btn btn-ghost" :disabled="!pendingChanges.length || clearing" @click="confirmClear">
          <Icon name="lucide:trash-2" /> Очистить таблицу
        </button>
        <button class="btn btn-ghost" @click="loadData">
          <Icon name="lucide:refresh-cw" /> Обновить
        </button>
      </div>
    </div>

    <!-- Filter bar -->
    <section class="card">
      <div class="approval-filters" v-if="!loading">
        <div class="approval-fields">
          <div class="af-search">
            <input v-model="approvalQuery" type="text" placeholder="Поиск по модели, артикулу…" @input="onFilterChangeDelayed" />
          </div>
          <div v-for="fk in approvalFilterKeys" :key="fk.key" class="af-item" :class="{ locked: isLocked(fk.key) }">
            <CostMultiSelect
              v-model="selected[fk.key]"
              :options="enrichFilterOptions(fk.key, filterOptions[fk.key] || [])"
              :placeholder="isLocked(fk.key) ? '—' : fk.label"
              :disabled="isLocked(fk.key)"
              @change="onFilterChange(fk.key)"
            />
          </div>
          <button class="btn btn-ghost btn-xs" @click="resetFilters" :disabled="filterBusy">Сбросить</button>
        </div>
        <div v-if="filterBusy" class="af-busy">Обновление…</div>
      </div>
    </section>

    <!-- Table -->
    <section class="card">
      <div class="table-wrap">
        <table class="data-table compact">
          <thead>
            <!-- Group headers -->
            <tr class="group-row">
              <th rowspan="2" class="chk-col"><input type="checkbox" :checked="allSelected" @change="toggleSelectAll" /></th>
              <th colspan="7" class="group-header">Основное</th>
              <th colspan="6" class="group-header">Иерархия</th>
              <th colspan="2" class="group-header">Параметры</th>
              <th colspan="11" class="group-header">Цены</th>
              <th colspan="7" class="group-header">Себестоимость, руб.</th>
              <th colspan="7" class="group-header">Себестоимость, USD</th>
              <th colspan="4" class="group-header">Маржа / Отклонение</th>
              <th colspan="2" class="group-header">Служебное</th>
            </tr>
            <!-- Column headers -->
            <tr>
              <th class="col-min">Модель</th>
              <th class="col-min">Артикул</th>
              <th>Наименование</th>
              <th class="col-min">Призн. кальк.</th>
              <th class="col-min">Менеджер</th>
              <th class="col-min">PLAN</th>
              <th class="col-min">З/П</th>

              <th class="col-min">Ур01</th>
              <th class="col-min">Ур02</th>
              <th class="col-min">Ур03</th>
              <th class="col-min">Ур04</th>
              <th class="col-min">Ур05</th>
              <th class="col-min">Страна</th>

              <th class="col-num date-col">Дата расчёта</th>
              <th class="col-num">Курс</th>

              <th class="col-min">Уровень</th>
              <th class="col-num">Розн., руб</th>
              <th class="col-num">Опт., руб</th>
              <th class="col-num">Себест., руб</th>
              <th class="col-num">Розн., USD</th>
              <th class="col-num">Опт., USD</th>
              <th class="col-num">Цена РФ</th>
              <th class="col-num">Цена КЗ</th>
              <th class="col-num">Цена УЗ</th>
              <th>Комментарий</th>

              <th class="col-num">Осн. мат.</th>
              <th class="col-num">Всп. мат.</th>
              <th class="col-num">Пошив</th>
              <th class="col-num">Раскрой</th>
              <th class="col-num">Декоры</th>
              <th class="col-num">Вязание</th>
              <th class="col-num">Себест.</th>

              <th class="col-num">Осн. мат.</th>
              <th class="col-num">Всп. мат.</th>
              <th class="col-num">Пошив</th>
              <th class="col-num">Раскрой</th>
              <th class="col-num">Декоры</th>
              <th class="col-num">Вязание</th>
              <th class="col-num">Себест.</th>

              <th class="col-num">Рентабельность, руб</th>
              <th class="col-num">Рентабельность, %</th>
              <th class="col-num">Маржа, %</th>
              <th class="col-num">Откл. %</th>

              <th>Автор</th>
              <th class="date-col">Дата</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="46" class="muted" style="text-align:center;padding:24px">Загрузка…</td>
            </tr>
            <tr v-else-if="!pendingChanges.length">
              <td colspan="46" class="muted" style="text-align:center;padding:24px">Нет ожидающих согласования изменений</td>
            </tr>
            <tr v-for="pc in pendingChanges" :key="pc.id" :class="approvalRowClass(pc)">
              <td class="chk-col"><input type="checkbox" :value="pc.id" v-model="selectedIds" /></td>
              <td>{{ pc['Модель'] || '—' }}</td>
              <td>{{ pc['Артикул'] || '—' }}</td>
              <td class="cell-ellipsis" :title="pc['Наименование модели']">{{ pc['Наименование модели'] || '—' }}</td>
              <td>{{ pc['Признак калькуляции'] || '—' }}</td>
              <td class="cell-ellipsis" :title="pc['Бренд-менеджер']">{{ pc['Бренд-менеджер'] || '—' }}</td>
              <td>{{ pc['PLAN_ID'] || '—' }}</td>
              <td>{{ pc['Номер задания производства'] || '—' }}</td>

              <td class="cell-ellipsis" :title="pc['Level 01']">{{ pc['Level 01'] || '—' }}</td>
              <td class="cell-ellipsis" :title="pc['Level 02']">{{ pc['Level 02'] || '—' }}</td>
              <td class="cell-ellipsis" :title="pc['Level 03']">{{ pc['Level 03'] || '—' }}</td>
              <td>{{ pc['Level 04'] || '—' }}</td>
              <td>{{ pc['Level 05'] || '—' }}</td>
              <td>{{ pc['Страна пр-ва'] || '—' }}</td>

              <td class="col-num">{{ formatDate(pc['дата расчета']) }}</td>
              <td class="col-num num">{{ fmt(pc['Курс на дату расчета']) }}</td>

              <td>{{ pc['Уровень цен'] || '—' }}</td>
              <td class="col-num num">{{ fmt(pc['Розничная цена по уровню, руб.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Отпускная цена по уровню, руб']) }}</td>
              <td class="col-num num num-strong">{{ fmt(pc['Себестоимость, руб.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Розничная цена по уровню, USD.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Отпускная цена по уровню, USD.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Цена РФ']) }}</td>
              <td class="col-num num">{{ fmt(pc['Цена КЗ']) }}</td>
              <td class="col-num num">{{ fmt(pc['Цена УЗ']) }}</td>
              <td>{{ pc['Комментарий'] || '—' }}</td>

              <td class="col-num num">{{ fmt(pc['Основные материалы, руб.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Вспомогательные материалы, руб.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Пошив, руб.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Раскрой, руб.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Декоры, руб.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Вязание, руб.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Себестоимость, руб.']) }}</td>

              <td class="col-num num">{{ fmt(pc['Основные материалы, USD.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Вспомогательные материалы, USD.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Пошив, USD.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Раскрой, USD.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Декоры, USD.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Вязание, USD.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Себестоимость, USD.']) }}</td>

              <td class="col-num num">{{ fmt(calcApproval(pc).markupRub) }}</td>
              <td class="col-num num">{{ calcApproval(pc).markupPct.toFixed(1) }}%</td>
              <td class="col-num num">{{ calcApproval(pc).marginPct.toFixed(1) }}%</td>
              <td class="col-num num">{{ marginDevText(pc) }}</td>

              <td>{{ pc['username'] }}</td>
              <td class="col-num date-col">{{ formatDate(pc['created_at']) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Error banner -->
    <div v-if="error" class="approval-error">
      <span>{{ error }}</span>
      <button class="approval-error-x" @click="error = ''">×</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onMounted } from 'vue'

const config = useRuntimeConfig()
const apiBase = computed(() =>
  config.public.costOnly ? '' : ((config.public.apiBase as string) || '')
)

// Пользователь раздела — тот же useState, что заполняет OAuth-колбэк (и
// cost-bypass в изолированном контуре). Заголовок X-Cost-User обязателен для
// всех защищённых эндпоинтов: require_perm (app/middleware.py) без него
// отвечает 401 «Не передан заголовок X-Cost-User».
//
// До 02.09.2026 страница ходила в API вообще без заголовка. Чтение
// (GET /pending-changes) это переживало — там прав не проверяют, — а
// «Установить цены» падало на 401 (прод, ELK 09:43 UTC: два POST
// /pending-changes/apply без пользователя). Модалка на /cost той же кнопкой
// работала, потому что заголовок передавала — отсюда «в одной форме
// устанавливается, в другой ошибка».
const user = useState<any>('auth-user')
const fetchHeaders = computed(() => {
  const email = user.value?.email || ''
  return email ? { 'X-Cost-User': email } : {}
})

const pendingChanges = ref<any[]>([])
const selectedIds = ref<number[]>([])
const loading = ref(false)
const applying = ref(false)
const clearing = ref(false)
const error = ref('')
const marginTargetByLevel01 = ref<Record<string, number | null>>({})

// ── Filter bar: keys & cascade ───────────────────────────────────────────────

const APPROVAL_LEVEL_KEYS = ["level01", "level02", "level03", "level04", "level05"];

/** Расшифровки признаков калькуляции */
const CALC_SIGN_DESCRIPTIONS: Record<string, string> = {
  'ПКПСС': 'новая разработка',
  'КПСС': 'плановая калькуляция',
  'ПФКСС': 'фактическая расценка ассортимента',
  'ФКСС': 'история себестоимости',
};

/** Обогатить плоский список опций расшифровками для calc_sign */
function enrichFilterOptions<T>(key: string, raw: T[]): T[] {
  if (key !== 'calc_sign') return raw;
  return raw.map((v) => {
    const val = String(v);
    return { value: val, label: val, description: CALC_SIGN_DESCRIPTIONS[val] } as any;
  });
}

const approvalFilterKeys = [
  { key: "brand_manager", label: "Бренд-менеджер" },
  { key: "level01", label: "Level 01" },
  { key: "level02", label: "Level 02" },
  { key: "level03", label: "Level 03" },
  { key: "level04", label: "Level 04" },
  { key: "level05", label: "Level 05" },
  { key: "calc_sign", label: "Призн. кальк." },
  { key: "plan_id", label: "План" },
];

const filterOptions = ref<Record<string, string[]>>({});
const selected = reactive<Record<string, string[]>>(
  Object.fromEntries(approvalFilterKeys.map((f) => [f.key, [] as string[]])) as any
);
const approvalQuery = ref("");
const filterBusy = ref(false);
let filterTimeout: ReturnType<typeof setTimeout> | null = null;

const allSelected = computed(() =>
  pendingChanges.value.length > 0 && selectedIds.value.length === pendingChanges.value.length
)

function toggleSelectAll(e: Event) {
  const checked = (e.target as HTMLInputElement).checked
  if (checked) {
    selectedIds.value = pendingChanges.value.map(p => p.id)
  } else {
    selectedIds.value = []
  }
}

function isLocked(key: string): boolean {
  let lowestIdx = -1;
  for (let i = APPROVAL_LEVEL_KEYS.length - 1; i >= 0; i--) {
    if (selected[APPROVAL_LEVEL_KEYS[i]]?.length > 0) {
      lowestIdx = i;
      break;
    }
  }
  if (lowestIdx === -1) return false;
  if (key === "brand_manager") return true;
  const keyIdx = APPROVAL_LEVEL_KEYS.indexOf(key as any);
  if (keyIdx === -1) return false;
  return keyIdx < lowestIdx;
}

function buildFilterParams(): URLSearchParams {
  const params = new URLSearchParams();
  for (const [key, vals] of Object.entries(selected)) {
    if (!(vals as string[]).length) continue;
    for (const v of vals as string[]) params.append(key, v);
  }
  if (approvalQuery.value.trim()) params.set("q", approvalQuery.value.trim());
  return params;
}

async function loadFilterOptions() {
  filterBusy.value = true;
  try {
    const params = buildFilterParams();
    const raw = await $fetch<Record<string, string[]>>(
      `${apiBase.value}/api/cost/pending-changes/filter-options?${params}`,
      { headers: fetchHeaders.value }
    );
    filterOptions.value = raw;
  } catch (e: any) {
    console.error("[cost] load approval filter-options failed", e);
  } finally {
    filterBusy.value = false;
  }
}

async function onFilterChange(changedKey: string) {
  if (changedKey === "brand_manager") {
    for (const k of APPROVAL_LEVEL_KEYS) selected[k] = [];
  } else if (APPROVAL_LEVEL_KEYS.includes(changedKey)) {
    const idx = APPROVAL_LEVEL_KEYS.indexOf(changedKey);
    for (let i = idx + 1; i < APPROVAL_LEVEL_KEYS.length; i++) {
      selected[APPROVAL_LEVEL_KEYS[i]] = [];
    }
  }
  await loadFilterOptions();
  await loadData();
}

function onFilterChangeDelayed() {
  if (filterTimeout) clearTimeout(filterTimeout);
  filterTimeout = setTimeout(async () => {
    await loadData();
  }, 300);
}

async function resetFilters() {
  approvalQuery.value = "";
  for (const k of approvalFilterKeys) selected[k.key] = [];
  await loadFilterOptions();
  await loadData();
}

async function loadData() {
  loading.value = true
  error.value = ''
  try {
    const params = buildFilterParams();
    const res = await $fetch<{ data: any[] }>(
      `${apiBase.value}/api/cost/pending-changes?${params}`,
      { headers: fetchHeaders.value }
    )
    pendingChanges.value = res.data || []
    selectedIds.value = []
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || String(e)
  } finally {
    loading.value = false
  }
}

async function applySelected() {
  if (!selectedIds.value.length) return
  applying.value = true
  error.value = ''
  try {
    // Build JSON for SQL procedure [createPriceList_inFox]
    const selected = pendingChanges.value.filter((pc: any) =>
      selectedIds.value.includes(pc.id)
    );
    const procPayload = selected
      .map((pc: any) => {
        const calcSign = pc['Признак калькуляции'] ?? pc.calc_sign ?? '';
        let priceType = 0;
        if (calcSign === 'КПСС') priceType = 3;
        else if (calcSign === 'ПФКСС') priceType = 1;
        if (!priceType) return null;
        return {
          model: pc['Модель'] ?? pc.model ?? '',
          articul: pc['Артикул'] ?? pc.articul ?? '',
          plan_id: String(pc['PLAN_ID'] ?? pc.plan_id ?? ''),
          wholesale_rub: Number(pc['Отпускная цена по уровню, руб'] ?? pc.wholesale_rub ?? 0),
          calc_sign: calcSign,
          price_type: priceType,
          author_name: 'system',
          cost_rub: Number(pc['Себестоимость, руб.'] ?? pc.cost_rub ?? 0),
        };
      })
      .filter(Boolean);
    console.log('[cost] SQL procedure payload:', JSON.stringify(procPayload));

    await $fetch(`${apiBase.value}/api/cost/pending-changes/apply`, {
      method: 'POST',
      headers: fetchHeaders.value,
      body: {
        ids: selectedIds.value,
        // Кто установил цены — в аудит уходит реальный пользователь, как и из
        // модалки на /cost; 'system' здесь скрывал бы автора.
        reviewed_by: user.value?.name || user.value?.email || 'system',
        proc_payload: procPayload,
      },
    })
    await loadData()
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || String(e)
  } finally {
    applying.value = false
  }
}

function confirmClear() {
  if (!confirm('Очистить таблицу согласования? Все необработанные изменения будут удалены.')) return
  clearAll()
}

async function clearAll() {
  clearing.value = true
  error.value = ''
  try {
    await $fetch(`${apiBase.value}/api/cost/pending-changes/clear`, {
      method: 'POST',
      headers: fetchHeaders.value,
    })
    await loadData()
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || String(e)
  } finally {
    clearing.value = false
  }
}

function fmt(v: any): string {
  if (v === null || v === undefined || v === '') return '—'
  const n = Number(v)
  if (isNaN(n)) return String(v)
  return n.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function formatDate(v: any): string {
  if (!v) return '—'
  const s = String(v)
  return s.includes('T') ? s.split('T')[0] : s
}

// ── Calculated fields (margin/markup) ────────────────────────────────────────

const calcApproval = (row: any) => {
  const wholesale = Number(row['Отпускная цена по уровню, руб'] || 0)
  const cost = Number(row['Себестоимость, руб.'] || 0)
  const markup = wholesale - cost
  const markupPct = cost > 0 ? (markup / cost) * 100 : 0
  const marginPct = wholesale > 0 ? (markup / wholesale) * 100 : 0
  return { markupRub: markup, markupPct, marginPct }
}

const marginDeviationApproval = (row: any): number => {
  const target = marginTargetByLevel01.value[row['Level 01'] ?? ''] ?? 0
  const { marginPct } = calcApproval(row)
  return marginPct - target
}

const marginDevText = (row: any): string => {
  const dev = marginDeviationApproval(row)
  return (dev >= 0 ? '+' : '') + dev.toFixed(1) + '%'
}

const approvalRowClass = (row: any): Record<string, boolean> => ({
  'approval-row-ok': marginDeviationApproval(row) >= 0,
  'approval-row-bad': marginDeviationApproval(row) < 0,
})

// ── Load margin targets ─────────────────────────────────────────────────────

async function loadMarginTargets() {
  try {
    const targets = await $fetch<{ level1: string; target_margin_pct: number | null }[]>(
      `${apiBase.value}/api/cost/margin-targets`,
      { headers: fetchHeaders.value }
    )
    const map: Record<string, number | null> = {}
    for (const t of targets) {
      map[t.level1] = t.target_margin_pct
    }
    marginTargetByLevel01.value = map
  } catch {
    marginTargetByLevel01.value = {}
  }
}

onMounted(async () => {
  await loadData()
  await loadFilterOptions()
  loadMarginTargets()
})
</script>

<style scoped>
.page-approvals { padding-top: var(--sp-2); }
.page-subtitle { color: var(--text-muted); font-size: var(--fs-sm); margin-top: 4px; }

.approval-actions {
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
.approval-info { font-size: var(--fs-sm); }
.approval-info strong { color: var(--text-strong); }
.approval-buttons { display: inline-flex; gap: var(--sp-3); align-items: center; }

.table-wrap {
  overflow-x: auto;
  max-width: 100%;
}

/* ── Table base ── */
.data-table {
  border-collapse: collapse;
  white-space: nowrap;
  font-size: var(--fs-sm);
  width: 100%;
  border: 1px solid var(--border);
}
.data-table th,
.data-table td {
  padding: var(--sp-1) var(--sp-2);
  border: 1px solid var(--border);
  text-align: left;
}
.data-table th {
  background: var(--bg-surface);
}
.data-table td {
  background: var(--bg-surface);
}
.data-table thead { position: sticky; top: 0; z-index: 2; }

/* ── Group header row ── */
.group-row th {
  background: var(--bg-surface-2, #f4f4f5);
  font-weight: 600;
  font-size: var(--fs-xs);
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: var(--text-muted);
  text-align: center;
  padding: var(--sp-1) var(--sp-2);
}
.group-header {
  border-bottom: 2px solid var(--border);
}

/* ── Column header row ── */
.data-table thead tr:not(.group-row) th {
  background: var(--bg-surface);
  font-weight: 600;
  font-size: var(--fs-xs);
  color: var(--text-strong);
  vertical-align: bottom;
  line-height: 1.2;
}

/* ── Checkbox column ── */
.chk-col {
  width: 32px;
  text-align: center;
  vertical-align: middle;
}
.chk-col input[type="checkbox"] { width: 15px; height: 15px; cursor: pointer; }

/* ── Column sizing ── */
.col-min { max-width: 100px; overflow: hidden; text-overflow: ellipsis; }
.col-num { text-align: right; }
.date-col { min-width: 90px; }

/* ── Cell styles ── */
.cell-ellipsis {
  max-width: 110px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.num {
  font-variant-numeric: tabular-nums;
  font-family: 'JetBrains Mono', 'Consolas', monospace;
  text-align: right;
}
.data-table tbody tr:hover { background: color-mix(in srgb, var(--accent) 6%, transparent); }
.muted { color: var(--text-muted); }

/* ── Row-level conditional formatting (deviation from target margin) ── */
.approval-row-ok {
  background-color: color-mix(in srgb, var(--pos, #16a34a) 8%, transparent) !important;
}
.approval-row-ok:hover {
  background-color: color-mix(in srgb, var(--pos, #16a34a) 14%, transparent) !important;
}
.approval-row-bad {
  background-color: color-mix(in srgb, var(--neg, #dc2626) 8%, transparent) !important;
}
.approval-row-bad:hover {
  background-color: color-mix(in srgb, var(--neg, #dc2626) 14%, transparent) !important;
}
.approval-row-ok td,
.approval-row-bad td {
  background-color: transparent !important;
}

/* ── Error banner ── */
.approval-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--sp-3) var(--sp-4);
  margin-top: var(--sp-4);
  border-radius: var(--rd-3);
  background: color-mix(in srgb, var(--neg) 8%, var(--bg-surface));
  border: 1px solid color-mix(in srgb, var(--neg) 30%, transparent);
  color: var(--text-strong);
  font-size: var(--fs-sm);
}
.approval-error-x {
  border: none;
  background: transparent;
  font-size: 18px;
  cursor: pointer;
  color: var(--text-muted);
  padding: 0 4px;
}
.approval-error-x:hover { color: var(--text-strong); }

/* ── Filter bar ─────────────────────────────────────────────────────────────── */
.approval-filters {
  padding: var(--sp-2) var(--sp-3);
  font-size: var(--fs-xs);
}
.approval-fields {
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-2);
  align-items: center;
}
.af-search input {
  width: 180px;
  padding: var(--sp-1) var(--sp-2);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  font-size: var(--fs-xs);
  background: var(--bg-surface);
  color: var(--text-strong);
}
.af-item { width: 140px; }
.af-item.locked { opacity: 0.4; pointer-events: none; }
.af-busy { margin-top: var(--sp-1); color: var(--text-muted); font-size: var(--fs-xs); }

/* Standalone cost column — bolder to contrast with prices */
.num-strong {
  font-weight: var(--fw-bold, 700);
  color: var(--text-strong);
}

/* Filter card must not clip the absolutely-positioned dropdown */
.page-approvals > section.card:first-of-type {
  overflow: visible;
}
</style>
