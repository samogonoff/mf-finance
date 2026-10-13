<template>
  <div class="page-fot" :class="{ 'fot-dark': effective === 'dark' }">
    <header class="page-header">
      <div>
        <h1 class="page-title">ФОТ и численность</h1>
        <p class="page-sub">Отчёты / Бюджет ФОТ 2026</p>
      </div>
      <div class="header-actions">
        <span v-if="pptxNote" class="refresh-note">{{ pptxNote }}</span>
        <span v-if="refreshNote" class="refresh-note">{{ refreshNote }}</span>
        <!-- Дека собирается на бэкенде из витрин; наблюдения «Разбора ИИ»
             подставляются, если пользователь их насчитал на странице. -->
        <div class="pptx-group">
          <select v-model="pptxMode" class="select" aria-label="Режим презентации">
            <option value="full">Полная</option>
            <option value="short">Краткая</option>
          </select>
          <button class="btn btn-ghost btn-sm" :disabled="pptxBusy" @click="exportPptx">
            <Icon name="lucide:presentation" />
            {{ pptxBusy ? 'Собираю…' : 'Выгрузить презентацию' }}
          </button>
        </div>
        <!-- Обновление витрин идёт в фоне (202), прогресс виден по meta.refreshing_since. -->
        <button class="btn btn-ghost btn-sm" :disabled="refreshing" @click="startRefresh">
          <Icon name="lucide:refresh-cw" :class="{ spinning: refreshing || !!refreshingSince }" />
          {{ refreshingSince ? 'Обновляется…' : 'Обновить данные' }}
        </button>
      </div>
    </header>

    <!-- Гейт по праву раздела -->
    <div v-if="permLoading" class="fot-center muted">
      <Icon name="lucide:loader" class="spinning" /> Проверка прав доступа…
    </div>
    <div v-else-if="!can(PERM)" class="cost-error">
      <Icon name="lucide:alert-triangle" />
      <div>
        <strong>Нет доступа</strong>
        <p>Для отчёта «ФОТ и численность» требуется право <code>{{ PERM }}</code>.</p>
      </div>
    </div>

    <template v-else>
      <!-- Вкладки P9…P13 листов отчёта CFO_ALL_AGG_RS -->
      <div class="fot-tabs" role="tablist">
        <button v-for="t in TABS" :key="t.key" role="tab" class="fot-tab" type="button"
                :class="{ active: tab === t.key }" :aria-selected="tab === t.key"
                @click="setTab(t.key)">{{ t.label }}</button>
      </div>

      <!-- P10–P13 собираются по фиксированным полугодиям — срезы дат на них не влияют -->
      <p v-if="isHalfTab" class="basis">
        Вкладка не зависит от срезов дат (полугодия 2025/2026 фиксированы)
      </p>

      <div v-if="loading && !dash" class="fot-center muted">
        <Icon name="lucide:loader" class="spinning" /> Загрузка…
      </div>
      <p v-else-if="error" class="error">{{ error }}</p>

      <template v-else-if="dash">
        <!-- Предупреждение сервера (например, «источник выгрузился не полностью») -->
        <div v-if="meta.warning" class="fot-warning">
          <Icon name="lucide:alert-triangle" />
          <span>{{ meta.warning }}</span>
        </div>

        <!-- Панель фильтров — едина для всех вкладок; P10–P13 сервер игнорирует срезы сам -->
        <section class="filters">
          <div v-for="f in MULTI_FILTERS" :key="f.key" class="filter-item">
            <label>{{ f.label }}</label>
            <CostMultiSelect v-model="selected[f.key]" :options="multiOptions(f)"
                             :placeholder="`Все · ${f.label.toLowerCase()}`" @change="loadDash" />
          </div>
          <div class="filter-item">
            <label>План</label>
            <select v-model="scenarioPlan" class="select" @change="loadDash">
              <option v-for="s in scenarioOptions" :key="s.value" :value="s.value">{{ s.label }}</option>
            </select>
          </div>
          <div class="filter-item">
            <label>Валюта</label>
            <div class="seg" role="group" aria-label="Валюта">
              <button v-for="c in ['BYN', 'USD', 'RUB']" :key="c" type="button" class="btn btn-sm"
                      :class="c === currency ? 'btn-primary' : 'btn-ghost'"
                      @click="setCurrency(c)">{{ c }}</button>
            </div>
          </div>
          <div class="filter-item">
            <label>ВГО</label>
            <select v-model="intercompany" class="select" @change="loadDash">
              <option v-for="o in icOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </div>
          <div class="filter-item">
            <label>Год</label>
            <select v-model="year" class="select" @change="onYearChange">
              <option v-for="y in yearOptions" :key="y.value" :value="y.value">{{ y.label }}</option>
            </select>
          </div>
          <div class="filter-item">
            <label>Месяц</label>
            <select v-model="month" class="select" @change="loadDash">
              <option v-for="m in monthOptions" :key="m.value" :value="m.value">{{ m.label }}</option>
            </select>
          </div>
        </section>

        <!-- Вкладки без бэкенда (p12/p13 до выката шага 2c): плашку показывает
             и ответ со status='not_implemented'. -->
        <div v-if="notImplemented" class="fot-wip">
          <Icon name="lucide:construction" class="fot-wip-icon" />
          <strong>Раздел в разработке</strong>
          <p>{{ wipText }}</p>
        </div>

        <div v-else class="fot-body" :class="{ pending: loading }">
          <!-- P10/P11 — фиксированные полугодия: срезы дат и «План» сервер
               игнорирует, поэтому в строке базы показываем то, что работает. -->
          <p class="basis">
            <template v-if="isHalfTab">
              Периоды: фиксированные полугодия {{ meta.periods?.year_prev || '2025' }}/{{ meta.periods?.year_plan || '2026' }}
              <span class="dot">·</span> Валюта: {{ currency }}
              <span class="dot">·</span> ВГО: {{ icLabel }}
            </template>
            <template v-else>
              Период: <strong>{{ periodLabel }}</strong>
              <span class="dot">·</span> План: {{ scenarioLabel }}
              <span class="dot">·</span> Валюта: {{ currency }}
              <span class="dot">·</span> ВГО: {{ icLabel }}
            </template>
          </p>

          <!-- KPI-карточки вкладки: у P10 пять, у P11 четыре -->
          <section class="fot-kpis">
            <article v-for="c in cards" :key="c.key" class="fot-kpi">
              <span class="fot-kpi-label">{{ c.label }}</span>
              <span class="fot-kpi-value">{{ fmtCard(c.value) }}<span class="fot-kpi-unit">{{ c.unit === '%' ? '%' : ` ${c.unit}` }}</span></span>
            </article>
          </section>

          <!-- Комбо-график: столбцы ФОТ + линия численности на второй оси.
               Только у вкладок с данными графика (P9) — у P10/P11 charts пустой. -->
          <section v-if="chartData" class="card">
            <div class="card-head">
              <h3 class="card-title">{{ chart?.label || 'Динамика по месяцам' }}</h3>
              <span class="hint">месяцы выбранного года, с января по выбранный месяц</span>
            </div>
            <div class="chart-box">
              <ClientOnly>
                <Bar v-if="chartData" :data="chartData" :options="chartOptions" />
              </ClientOnly>
            </div>
          </section>

          <!-- Таблицы вкладки — по порядку из dash.tables: у P9 матрица с
               проваливанием, у P10 одна таблица по странам, у P11 три
               (подразделения, каналы сбыта, направления). Дрилл, крошки и
               CSV-кнопка включаются признаками из meta таблицы и вкладки. -->
          <section v-for="t in tables" :key="t.key" class="card">
            <div class="card-head">
              <div>
                <!-- Уровень в заголовке и крошки — у дриллимых таблиц, и НЕ
                     пропадают на последнем уровне: там can_drill=false, но
                     path непуст — иначе пользователь теряет кнопку «Назад» -->
                <h3 class="card-title">
                  {{ t.label }}<template v-if="t.meta?.can_drill || (t.meta?.path?.length || 0) > 0"> · {{ t.meta.level_label || t.meta.dim }}</template>
                </h3>
                <nav v-if="t.meta?.can_drill || (t.meta?.path?.length || 0) > 0" class="crumbs">
                  <button class="crumb" type="button" :disabled="!tablePath(t).length" @click="drillTo(t, 0)">Все</button>
                  <template v-for="(c, i) in (t.meta?.path || [])" :key="i">
                    <span class="crumb-sep">›</span>
                    <button class="crumb" type="button" :disabled="i === tablePath(t).length - 1"
                            :title="c.label || c.value" @click="drillTo(t, i + 1)">{{ c.value || c.label || '(не указано)' }}</button>
                  </template>
                </nav>
              </div>
              <!-- Клиентский CSV (Blob + BOM, разделитель «;») — сводным вкладкам P10/P11 -->
              <button v-if="canExport" class="btn btn-ghost btn-sm" type="button"
                      title="Скачать CSV: разделитель «;», BOM — Excel откроет без мастера импорта"
                      @click="exportCsv(t)">
                <Icon name="lucide:download" /> CSV
              </button>
            </div>
            <p v-if="t.meta?.can_drill || t.meta?.truncated" class="card-note">
              <span v-if="t.meta?.can_drill">Клик по строке — провалиться на уровень ниже. </span>
              <span v-if="t.meta?.truncated" class="warn">
                Показаны первые {{ t.meta.row_limit }} строк — сузьте фильтры.
              </span>
            </p>
            <div class="table-wrap">
              <table class="matrix">
                <thead>
                  <tr>
                    <th v-for="col in t.columns" :key="col.key" :class="{ 'col-label': col.key === 'label' }">
                      {{ col.label }}<span v-if="col.unit && col.key !== 'label'" class="th-unit">, {{ col.unit }}</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(row, i) in (t.rows || [])" :key="i"
                      :class="{ drillable: t.meta?.can_drill }"
                      @click="drill(t, row.drill_value)">
                    <!-- Светофор Δ-колонок: класс вешается по hex из row['<ключ>_color'] -->
                    <td v-for="col in t.columns" :key="col.key"
                        :class="[col.key === 'label' ? 'col-label' : 'num', cellDeltaClass(row, col)]">
                      <span v-if="col.key === 'label'" class="row-label">
                        {{ cellText(t, row, col) }}
                        <Icon v-if="t.meta?.can_drill" name="lucide:chevron-right" class="drill-icon" />
                      </span>
                      <template v-else>{{ cellText(t, row, col) }}</template>
                    </td>
                  </tr>
                  <tr v-if="!(t.rows || []).length">
                    <td :colspan="t.columns.length" class="empty-row">Нет данных по выбранным срезам</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <!-- Разбор ИИ вкладки: данные собирает сервер (fot_p9..fot_p13, до
               validate_charts), фронт отдаёт только срезы. charts компоненту
               нужен лишь как признак «есть что разбирать» — путь fot_* их
               игнорирует, поэтому на сводных вкладках передаём серию карточек. -->
          <CostChartInsight :block="`fot_${tab}`" :block-title="`${tabLabel} (${tab.toUpperCase()})`"
                            :charts="insightCharts" :context="insightContext" />
        </div>

        <!-- Методология + свежесть данных -->
        <details v-if="meta.methodology || meta.closed_period || meta.refreshed_at" class="fot-method">
          <summary>Методология и данные</summary>
          <p v-if="meta.methodology" class="fot-method-text">{{ meta.methodology }}</p>
          <p class="fot-method-meta">
            <span v-if="meta.closed_period">Закрытый период: {{ fmtDate(meta.closed_period) }}</span>
            <span v-if="meta.closed_period && meta.refreshed_at" class="dot">·</span>
            <span v-if="meta.refreshed_at">Обновлено: {{ fmtDateTime(meta.refreshed_at) }}</span>
          </p>
        </details>
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { Bar } from 'vue-chartjs'
import {
  BarController, BarElement, CategoryScale, Chart as ChartJS, Legend,
  LineController, LineElement, LinearScale, PointElement, Tooltip,
} from 'chart.js'

// Смешанный график (столбцы ФОТ + линия численности на второй оси) собирается
// компонентом Bar с датасетом type:'line' — поэтому нужен LineController.
ChartJS.register(BarController, BarElement, CategoryScale, Legend,
                 LineController, LineElement, LinearScale, PointElement, Tooltip)

definePageMeta({ middleware: 'scope-guard' })

// ── Права, тема, API ─────────────────────────────────────────────────────────

const PERM = 'cost:fot'
const { can, loading: permLoading } = useCostPermission()
const { effective } = useTheme()

const config = useRuntimeConfig()
const apiBase = computed(() => (config.public.costOnly ? '' : ((config.public.apiBase as string) || '')))
const user = useState<any>('auth-user')
const fetchHeaders = computed<Record<string, string>>(() => (user.value?.email ? { 'X-Cost-User': user.value.email } : {}))

function api<T = any>(path: string, opts: Record<string, any> = {}): Promise<T> {
  return $fetch<T>(`${apiBase.value}/api/cost${path}`, { ...opts, headers: fetchHeaders.value })
}
function errText(e: any): string {
  return e?.data?.detail || e?.message || String(e)
}

// ── Вкладки ──────────────────────────────────────────────────────────────────

const TABS = [
  { key: 'p9', label: 'ФОТ и численность' },
  { key: 'p10', label: 'ФОТ свод по группе' },
  { key: 'p11', label: 'Направления и подразделения' },
  { key: 'p12', label: 'Численность план и факт' },
  { key: 'p13', label: 'HR-бюджет и отделы' },
] as const
const tab = ref<string>('p9')

// Описания не-P9 — по листам презентации CFO_ALL_AGG_RS (слайды 22–27, 34–35).
const WIP_TEXT: Record<string, string> = {
  p10: 'Свод ФОТ по группе за фиксированные полугодия: факт 1ПГ 2025 и 1ПГ 2026, тактика 2ПГ 2026 и ожидание года — с динамикой к 2025-му, долей ФОТ в выручке группы и численностью отделов (плановая/фактическая, % укомплектования).',
  p11: 'Анализ по направлениям и подразделениям: ФОТ по странам, доля ФОТ в выручке и динамика к 2025 году.',
  p12: 'Численность: утверждённая численность стратегии-2026 против факта на конец 1ПГ 2025 и 1ПГ 2026 — % укомплектования и отклонения по странам и подразделениям.',
  p13: 'HR-бюджет: статьи 65 «Расходы на персонал» и 66 «Прочие расходы» по странам — факт полугодий 2025/2026, тактика 2ПГ 2026 и ожидание года.',
}
const wipText = computed(() => WIP_TEXT[tab.value] || 'Раздел готовится.')

// ── Фильтры ──────────────────────────────────────────────────────────────────

/** Ключи — белый список срезов app/fot.py; лишнее сервер игнорирует. */
const MULTI_FILTERS = [
  { key: 'direction', label: 'Направление', opt: 'directions' },
  { key: 'cfo_group', label: 'Группа ЦФО', opt: 'cfo_groups' },
  { key: 'cfo_subgroup', label: 'Подгруппа ЦФО', opt: 'cfo_subgroups' },
  { key: 'cfo', label: 'ЦФО', opt: 'cfos' },
  { key: 'legal_entity', label: 'Юрлицо', opt: 'legal_entities' },
  { key: 'country', label: 'Страна', opt: 'countries' },
]

const selected = reactive<Record<string, string[]>>(
  Object.fromEntries(MULTI_FILTERS.map((f) => [f.key, [] as string[]])) as any,
)
const scenarioPlan = ref('тактика')
const currency = ref('BYN')
// «все» — значение по умолчанию на фронте: справочник сервера знает только 0/1.
const intercompany = ref('all')
const year = ref('')
const month = ref('')

const filterOptions = ref<Record<string, any>>({})
const filtersMeta = ref<any>({})

const MONTHS_FULL = ['январь', 'февраль', 'март', 'апрель', 'май', 'июнь',
                     'июль', 'август', 'сентябрь', 'октябрь', 'ноябрь', 'декабрь']
const MONTHS_SHORT = ['янв', 'фев', 'мар', 'апр', 'май', 'июн',
                      'июл', 'авг', 'сен', 'окт', 'ноя', 'дек']

const asOptions = (raw: any): { value: string; label: string }[] =>
  (Array.isArray(raw) ? raw : [])
    .map((o: any) => (typeof o === 'string' ? { value: o, label: o } : { value: String(o.id), label: String(o.text ?? o.id) }))

const multiOptions = (f: { opt: string }) => asOptions(filterOptions.value[f.opt])
const scenarioOptions = computed(() => asOptions(filterOptions.value.scenarios))
const icOptions = computed(() => [{ value: 'all', label: 'Все' }, ...asOptions(filterOptions.value.intercompany)])
const yearOptions = computed(() => asOptions(filterOptions.value.years))
const monthOptions = computed(() =>
  (filterOptions.value.months || []).map((m: any) => ({
    value: String(m.id),
    label: MONTHS_FULL[Number(m.id) - 1] || String(m.text ?? m.id),
  })))

const scenarioLabel = computed(() =>
  scenarioOptions.value.find((o) => o.value === scenarioPlan.value)?.label || scenarioPlan.value || '—')
const icLabel = computed(() => icOptions.value.find((o) => o.value === intercompany.value)?.label || intercompany.value || '—')

/** Дефолт периода — закрытый период (meta.closed_period, «2026-08-31»).
 *  Применяется один раз: дальше пользователь меняет сам. */
const defaultsApplied = ref(false)
function applyDefaultsFromClosed() {
  if (defaultsApplied.value) return
  const m = String(filtersMeta.value.closed_period || '').match(/^(\d{4})-(\d{2})/)
  if (m) {
    year.value = m[1]
    month.value = String(Number(m[2]))
    defaultsApplied.value = true
  }
}

async function loadFilters() {
  try {
    const res = await api<any>('/fot/filters', { query: year.value ? { year: year.value } : {} })
    filterOptions.value = res || {}
    filtersMeta.value = res?.meta || {}
    applyDefaultsFromClosed()
  } catch (e) {
    // Сбой справочника не должен прятать дашборд — сервер подставит свои дефолты.
    console.error('[fot] filters failed', e)
  }
}

function onYearChange() {
  // Подгруппы и ЦФО отбираются по году («ЦФО активно» в году) — перечитываем списки.
  loadFilters()
  loadDash()
}
function setCurrency(c: string) {
  if (currency.value === c) return
  currency.value = c
  loadDash()
}

// ── Дашборд ──────────────────────────────────────────────────────────────────

const dash = ref<any>(null)
const loading = ref(false)
const error = ref('')
/** Пути проваливания по таблицам: ключ таблицы → пройденные уровни.
 *  Имя повторяющегося query-параметра — по контракту app/fot.py: матрица
 *  P9/P11 — matrix_path, каналы P11 — channel_path; у остальных таблиц
 *  дрилла нет, путь им не нужен. */
const tablePaths = ref<Record<string, string[]>>({})
const PATH_PARAM: Record<string, string> = { by_department: 'matrix_path', by_channel: 'channel_path' }
const tablePath = (t: any): string[] => tablePaths.value[t.key] || []

function buildQuery(): string {
  const p = new URLSearchParams()
  p.set('tab', tab.value)
  for (const f of MULTI_FILTERS) for (const v of selected[f.key] || []) p.append(f.key, v)
  for (const [key, path] of Object.entries(tablePaths.value)) {
    for (const v of path) p.append(PATH_PARAM[key] || 'matrix_path', v)
  }
  if (scenarioPlan.value) p.set('scenario_plan', scenarioPlan.value)
  if (intercompany.value) p.set('intercompany', intercompany.value)
  if (year.value) p.set('year', year.value)
  if (month.value) p.set('month', month.value)
  if (currency.value) p.set('currency', currency.value)
  return p.toString()
}

async function loadDash() {
  loading.value = true
  error.value = ''
  try {
    dash.value = await api<any>(`/fot/dashboard?${buildQuery()}`)
  } catch (e: any) {
    console.error('[fot] dashboard failed', e)
    error.value = errText(e)
  } finally {
    loading.value = false
  }
}

function setTab(key: string) {
  if (tab.value === key) return
  tab.value = key
  tablePaths.value = {} // пути проваливания между вкладками не переезжают
  loadDash()
}

const meta = computed<any>(() => dash.value?.meta || {})
const notImplemented = computed(() => meta.value.status === 'not_implemented')

const cards = computed<any[]>(() => dash.value?.cards || [])
const chart = computed<any>(() =>
  (dash.value?.charts || []).find((c: any) => c.key === 'monthly') || (dash.value?.charts || [])[0] || null)
/** Все таблицы вкладки — по порядку API: у P9 матрица, у P10 одна, у P11 три. */
const tables = computed<any[]>(() => dash.value?.tables || [])
/** CSV-кнопка в заголовке каждой таблицы — только сводным вкладкам P10/P11. */
const canExport = computed(() => tab.value === 'p10' || tab.value === 'p11')
/** P10–P13 — фиксированные полугодия: срезы дат и «План» сервер игнорирует. */
const isHalfTab = computed(() => ['p10', 'p11', 'p12', 'p13'].includes(tab.value))
/** Метка активной вкладки — заголовок блока ИИ и нотификации. */
const tabLabel = computed(() => TABS.find((t) => t.key === tab.value)?.label || tab.value)

// ── Разбор ИИ (CostChartInsight) ─────────────────────────────────────────────

/** Срезы для ИИ — те же, что уходят в /fot/dashboard: списки списками,
 *  скаляры строками (контракт _params_from_context в app/fot_insights.py).
 *  Пути дрилла входят туда же: без них сервер соберёт вкладку свёрнутой. */
const insightContext = computed<Record<string, any>>(() => ({
  direction: [...(selected.direction || [])],
  cfo_group: [...(selected.cfo_group || [])],
  cfo_subgroup: [...(selected.cfo_subgroup || [])],
  cfo: [...(selected.cfo || [])],
  legal_entity: [...(selected.legal_entity || [])],
  country: [...(selected.country || [])],
  matrix_path: [...(tablePaths.value.by_department || [])],
  channel_path: [...(tablePaths.value.by_channel || [])],
  scenario_plan: scenarioPlan.value || '',
  intercompany: intercompany.value || '',
  year: year.value || '',
  month: month.value || '',
  currency: currency.value || '',
}))

/** charts компоненту нужен только как признак «есть что разбирать»
 *  (hasData смотрит на datasets[].data): серверный fot_*-путь их не читает.
 *  У P9 берём настоящий график, на сводных вкладках — серию из карточек. */
const insightCharts = computed<any[]>(() => {
  if (chartData.value) {
    return [{ title: chart.value?.label || 'Динамика по месяцам', series: chartData.value }]
  }
  const cardsNum = cards.value.filter((c: any) => isNum(c.value))
  return [{
    title: 'Карточки вкладки',
    series: {
      labels: cardsNum.map((c: any) => c.label),
      datasets: [{ label: 'Значения карточек', data: cardsNum.map((c: any) => Number(c.value)) }],
    },
  }]
})

// ── Проваливание ─────────────────────────────────────────────────────────────

function drill(t: any, value: string | null | undefined) {
  if (!t?.meta?.can_drill || value === null || value === undefined || value === '') return
  tablePaths.value = { ...tablePaths.value, [t.key]: [...tablePath(t), String(value)] }
  loadDash()
}
function drillTo(t: any, depth: number) {
  const path = tablePath(t)
  if (depth >= path.length) return
  tablePaths.value = { ...tablePaths.value, [t.key]: path.slice(0, depth) }
  loadDash()
}

// ── Загрузка при наличии права ───────────────────────────────────────────────

watch(permLoading, (l) => {
  if (!l && can(PERM)) {
    loadFilters().then(loadDash)
  }
}, { immediate: true })

// ── Обновление витрин ────────────────────────────────────────────────────────

const refreshing = ref(false)
const refreshNote = ref('')
let pollTimer: ReturnType<typeof setInterval> | null = null

const refreshingSince = computed(() => filtersMeta.value.refreshing_since || meta.value.refreshing_since || null)

async function startRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  refreshNote.value = ''
  try {
    await api('/fot/refresh', { method: 'POST', body: {} })
    refreshNote.value = 'Обновление запущено — данные перечитаются автоматически'
    schedulePoll()
  } catch (e: any) {
    refreshNote.value = `Не удалось запустить обновление: ${errText(e)}`
  } finally {
    refreshing.value = false
  }
}

/** Пока витрины считаются (meta.refreshing_since не пуст), опрашиваем раз в 5 с,
 *  не дольше двух минут; по готовности перечитываем фильтры и вкладку. */
function schedulePoll() {
  stopPoll()
  let left = 24
  pollTimer = setInterval(() => {
    left -= 1
    loadFilters()
      .then(async () => {
        if (!refreshingSince.value) {
          await loadDash()
          refreshNote.value = 'Данные обновлены'
          stopPoll()
        }
      })
      .catch(() => { /* сеть моргнула — попробуем на следующем тике */ })
    if (left <= 0) {
      stopPoll()
      refreshNote.value = 'Обновление ещё идёт — повторите попытку через пару минут'
    }
  }, 5000)
}
function stopPoll() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
}
onBeforeUnmount(stopPoll)

// ── График ───────────────────────────────────────────────────────────────────

// Канвас не понимает CSS-переменные — палитру вкладки держим в константах,
// в браузере выбираем по активной теме (те же значения, что в scoped CSS).
// Токены сняты с деки «Бюджет ФОТ 2026»: столбцы ФОТ — винный, линия
// численности — светлый винный, сетка и подписи осей — из палитры слайдов.
const ACCENT_LIGHT = '#6B3347'
const ACCENT_DARK = '#C98BA1'
const LINE_LIGHT = '#9B5A70'
const LINE_DARK = '#DCAABB'
const GRID_LIGHT = '#E0CBD3'
const GRID_DARK = '#4A353C'
const MUTED_LIGHT = '#6B7280'
const MUTED_DARK = '#9C9296'
const isDarkTheme = computed(() => effective.value === 'dark')
const accentColor = computed(() => (isDarkTheme.value ? ACCENT_DARK : ACCENT_LIGHT))
const lineColor = computed(() => (isDarkTheme.value ? LINE_DARK : LINE_LIGHT))
const gridColor = computed(() => (isDarkTheme.value ? GRID_DARK : GRID_LIGHT))
const mutedColor = computed(() => (isDarkTheme.value ? MUTED_DARK : MUTED_LIGHT))

const chartData = computed(() => {
  const ch = chart.value
  if (!ch?.labels?.length || !ch.series?.length) return null
  const multiYear = new Set(ch.labels.map((ym: string) => String(ym).split('-')[0])).size > 1
  const labels = ch.labels.map((ym: string) => {
    const [y, m] = String(ym).split('-')
    const name = MONTHS_SHORT[Number(m) - 1]
    return multiYear ? `${name} ${String(y).slice(2)}` : (name || String(ym))
  })
  const datasets = ch.series.map((s: any) => {
    if (s.type === 'line') {
      // Линия — светлый винный (#9B5A70 в светлой теме), вторая ось справа.
      return {
        type: 'line' as const, label: s.label, data: s.values,
        yAxisID: s.axis || 'y2', borderColor: lineColor.value, backgroundColor: lineColor.value,
        borderWidth: 2, pointRadius: 2, tension: 0.25, order: 1,
      }
    }
    return { type: 'bar' as const, label: s.label, data: s.values, backgroundColor: accentColor.value, order: 2 }
  })
  return { labels, datasets }
})

const chartOptions = computed(() => {
  const series = chart.value?.series || []
  const barUnit = series.find((s: any) => s.type !== 'line')?.unit || ''
  const lineUnit = series.find((s: any) => s.type === 'line')?.unit || ''
  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: 'index' as const, intersect: false },
    plugins: {
      legend: { position: 'bottom' as const, labels: { color: mutedColor.value, boxWidth: 10, font: { size: 11 } } },
    },
    scales: {
      x: { ticks: { color: mutedColor.value }, grid: { display: false } },
      y: {
        ticks: { color: mutedColor.value }, grid: { color: gridColor.value },
        title: { display: !!barUnit, text: barUnit, color: mutedColor.value },
      },
      y2: {
        position: 'right' as const,
        ticks: { color: mutedColor.value }, grid: { drawOnChartArea: false },
        title: { display: !!lineUnit, text: lineUnit, color: mutedColor.value },
      },
    },
  }
})

// ── Форматтеры (ru-RU, mono/tabular-nums задаёт CSS) ─────────────────────────

const nf0 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 0 })
const nf2 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const isNum = (v: any) => v !== null && v !== undefined && v !== '' && Number.isFinite(Number(v))

const fmtCard = (v: any) => (isNum(v) ? nf2.format(Number(v)) : '—')

/** Единицы с двумя знаками — проценты, п.п. и ставки; деньги (в col.unit
 *  валюта витрины: BYN/USD/RUB, суммы в тыс.) — целые. Список снят с
 *  фактических col.unit API (app/fot.py), фронт ничего не угадывает. */
const UNITS_2DP = new Set(['%', 'п.п.', 'ставок'])

/** Таблицы с деньгами в копейках: HR-бюджет P13 бэкенд округляет до двух
 *  знаков (_r2) при unit=валюта — точность в col.unit не выражена, поэтому
 *  исключение держим картой по ключу таблицы (контроль деки — «592,29»). */
const MONEY_2DP_TABLES = new Set(['hr_budget'])

/** Ячейка таблицы: пустое значение — прочерк, никогда не NaN.
 *  Точность — по col.unit; отрицательные Δ ru-RU пишет со знаком минус. */
function cellText(t: any, row: any, col: any): string {
  const v = row?.[col.key]
  if (v === null || v === undefined || v === '') return '—'
  if (col.key === 'label' || !isNum(v)) return String(v)
  const n = Number(v)
  if (UNITS_2DP.has(String(col.unit || '').trim())) return nf2.format(n)
  return MONEY_2DP_TABLES.has(t?.key) ? nf2.format(n) : nf0.format(n)
}

/** Светофор Δ-ячеек: цвет считает сервер и кладёт рядом со значением —
 *  row['<ключ колонки>_color'], hex палитры деки (#1C8A5A/#9E6B0F/#C0392B)
 *  или пусто. Фронт только маппит hex на класс с CSS-переменной темы,
 *  логику покраски не повторяет. */
const DELTA_COLOR_CLASS: Record<string, string> = {
  '#1C8A5A': 'delta-pos', // var(--fot-pos) — рост в пределах порога
  '#9E6B0F': 'delta-mid', // var(--fot-mid) — рост выше порога без роста доли
  '#C0392B': 'delta-neg', // var(--fot-neg) — зона контроля
}
function cellDeltaClass(row: any, col: any): string {
  const hex = String(row?.[col.key + '_color'] || '').trim().toUpperCase()
  return DELTA_COLOR_CLASS[hex] || ''
}

// ── CSV-экспорт таблиц (клиентский, без библиотек) ───────────────────────────

/** Выгрузка одной таблицы: BOM + разделитель «;» — Excel с ру-локалью
 *  открывает файл двойным кликом. Числа уходят сырыми значениями JSON
 *  с десятичной запятой; экранное ру-форматирование в файл не тащим. */
function exportCsv(t: any) {
  const cols: any[] = t.columns || []
  const q = (v: any): string => {
    const s = v === null || v === undefined ? '' : String(v)
    return /[";\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
  }
  const cell = (row: any, col: any): string => {
    const v = row?.[col.key]
    if (v === null || v === undefined || v === '') return ''
    if (col.key === 'label' || !isNum(v)) return String(v)
    return String(Number(v)).replace('.', ',')
  }
  const head = cols.map((c) => q(c.label + (c.unit && c.key !== 'label' ? `, ${c.unit}` : '')))
  const lines = [head.join(';')]
  for (const row of t.rows || []) lines.push(cols.map((c) => q(cell(row, c))).join(';'))
  const BOM = String.fromCharCode(0xFEFF) // U+FEFF — Excel по нему узнаёт UTF-8
  const blob = new Blob([BOM + lines.join('\r\n')], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `fot_${tab.value}_${t.key}.csv`
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

// ── Выгрузка презентации (POST /fot/export/pptx) ─────────────────────────────

/** Режим деки: full — все слайды, short — сводный. Значения — контракт
 *  эндпоинта (mode: "full" | "short"), имя файла строит и сервер. */
const pptxMode = ref<'full' | 'short'>('full')
const pptxBusy = ref(false)
const pptxNote = ref('')

/** Наблюдения «Разбора ИИ» для слайда ИИ в деке. Компонент ответ наружу не
 *  отдаёт (событий нет, python/cost править нельзя), поэтому берём тексты
 *  обзорных наблюдений (result.observations) из отрендеренных блоков
 *  активной вкладки: ul.insight-list — прямой ребёнок секции блока, списки
 *  «углублённого исследования» сидят глубже и не попадают. Маркер «расчёт
 *  модели» (obs-warn) в текст не входит. Нет блоков — пустой список, деки
 *  слайд ИИ покажет с заглушкой. */
function collectObservations(): string[] {
  const texts: string[] = []
  for (const el of document.querySelectorAll('.page-fot .insight > ul.insight-list > li.obs > .obs-text')) {
    const text = String(el.childNodes[0]?.textContent || '').trim()
    if (text) texts.push(text)
  }
  return texts
}

async function exportPptx() {
  if (pptxBusy.value) return
  pptxBusy.value = true
  pptxNote.value = ''
  try {
    const blob = await api<Blob>('/fot/export/pptx', {
      method: 'POST',
      responseType: 'blob',
      body: {
        mode: pptxMode.value,
        filters: insightContext.value,
        observations: collectObservations(),
      },
    })
    const now = new Date()
    const stamp = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}`
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `ФОТ_2026_${pptxMode.value}_${stamp}.pptx`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
    pptxNote.value = 'Презентация собрана'
  } catch (e: any) {
    pptxNote.value = `Не удалось собрать презентацию: ${errText(e)}`
  } finally {
    pptxBusy.value = false
  }
}

const periodLabel = computed(() => {
  const p = meta.value.period || {}
  if (!p.year && !p.month) return '—'
  const name = p.month ? MONTHS_FULL[Number(p.month) - 1] || `мес. ${p.month}` : ''
  return `${name} ${p.year}`.trim()
})

const dtDate = new Intl.DateTimeFormat('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric' })
const dtFull = new Intl.DateTimeFormat('ru-RU', { dateStyle: 'short', timeStyle: 'short' })
const fmtDate = (iso: any) => {
  const d = new Date(String(iso))
  return Number.isNaN(d.getTime()) ? String(iso) : dtDate.format(d)
}
const fmtDateTime = (iso: any) => {
  const d = new Date(String(iso))
  return Number.isNaN(d.getTime()) ? String(iso) : dtFull.format(d)
}
</script>

<style scoped>
/* Палитра раздела — page-scoped, снята с XML деки «Бюджет ФОТ 2026»:
   визуал — реплика слайдов презентации, не дашборда. На другие страницы
   не подтягивается, глобальные токены design-system.css не трогаем. */
.page-fot {
  --fot-accent: #6B3347;        /* винный — шапки таблиц, основной акцент */
  --fot-accent-ink: #fff;
  --fot-accent-light: #9B5A70;  /* светлый винный — заголовки слайдов/вкладок */
  --fot-card: #F8EDF0;          /* заливка карточек */
  --fot-strip: #E6D2D9;         /* зебра нечётных строк таблиц */
  --fot-border: #E0CBD3;        /* бордеры и разделители */
  --fot-ink: #3F3F46;           /* основной текст */
  --fot-muted: #6B7280;         /* подписи, вторичный текст */
  --fot-pos: #1C8A5A;
  --fot-mid: #9E6B0F;
  --fot-neg: #C0392B;

  /* Белый «слайд» поверх серой подложки .app-main: компенсируем её padding. */
  --fot-page-pad: var(--sp-7);

  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
  background: #fff;
  margin: calc(-1 * var(--fot-page-pad));
  padding: var(--fot-page-pad);
}
@media (min-width: 1920px) {
  .page-fot { --fot-page-pad: var(--sp-9); }
}
.page-fot.fot-dark {
  --fot-accent: #C98BA1;
  --fot-accent-light: #DCAABB;
  --fot-card: #2A1E22;
  --fot-strip: #3A2A30;
  --fot-border: #4A353C;
  --fot-ink: #E8E2E4;
  --fot-muted: #9C9296;

  background: var(--bg-app); /* в тёмной теме сливаемся с подложкой */
}

.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-4); flex-wrap: wrap; }
/* Заголовок слайда: Cambria bold, светлый винный — как в деке. */
.page-title {
  font-family: 'Cambria', 'Times New Roman', serif;
  font-size: 22px;
  font-weight: var(--fw-bold, 700);
  color: var(--fot-accent-light);
  margin: 0;
  line-height: 1.2;
}
.page-sub { margin: var(--sp-1) 0 0; font-size: 14px; color: var(--fot-muted); }
.header-actions { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
/* Селект режима деки + кнопка выгрузки — одна группа в шапке */
.pptx-group { display: flex; align-items: center; gap: var(--sp-2); }
.refresh-note { font-size: 12px; color: var(--fot-muted); max-width: 340px; }

.fot-center { display: flex; align-items: center; justify-content: center; gap: var(--sp-2); padding: var(--sp-6) 0; }
.muted { color: var(--fot-muted); font-size: 13px; }
.error { color: var(--fot-neg); font-size: 13px; margin: 0; }
.basis { margin: 0; font-size: 12px; color: var(--fot-muted); }
.dot { margin: 0 var(--sp-2); opacity: 0.5; }
.warn { color: var(--fot-mid); }

/* Гейт доступа */
.cost-error {
  display: flex; align-items: flex-start; gap: var(--sp-3);
  padding: var(--sp-4) var(--sp-5); border-radius: 5px;
  background: color-mix(in srgb, var(--fot-neg) 8%, var(--fot-card));
  border: 1px solid color-mix(in srgb, var(--fot-neg) 30%, transparent);
  color: var(--fot-ink);
}
.cost-error :deep(svg) { color: var(--fot-neg); flex-shrink: 0; margin-top: 2px; }
.cost-error p { margin-top: 4px; font-size: 13px; }
.cost-error code {
  background: var(--fot-card); padding: 1px 5px; border-radius: 4px;
  font-family: var(--font-mono); font-size: 90%;
}

/* Вкладки */
/* Вкладки — светлый винный активной, как заголовки слайдов деки. */
.fot-tabs { display: flex; gap: var(--sp-2); border-bottom: 1px solid var(--fot-border); flex-wrap: wrap; }
.fot-tab {
  border: none; background: transparent; cursor: pointer;
  color: var(--fot-muted); font-size: 14px;
  padding: var(--sp-3) var(--sp-4);
  border-bottom: 2px solid transparent; margin-bottom: -1px;
  transition: color var(--t-fast, 0.15s), border-color var(--t-fast, 0.15s);
}
.fot-tab:hover { color: var(--fot-ink); }
.fot-tab.active { color: var(--fot-accent-light); border-bottom-color: var(--fot-accent-light); font-weight: var(--fw-bold, 700); }

/* Фильтры */
.filters {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
  gap: var(--sp-3); align-items: end;
}
.filter-item { display: flex; flex-direction: column; gap: var(--sp-1); min-width: 0; }
.filter-item > label { font-size: 12px; color: var(--fot-muted); }
.seg { display: flex; gap: var(--sp-1); }

/* Предупреждение сервера — над карточками */
.fot-warning {
  display: flex; align-items: flex-start; gap: var(--sp-2);
  padding: var(--sp-3) var(--sp-4); border-radius: 5px;
  background: color-mix(in srgb, var(--fot-mid) 10%, var(--fot-card));
  border: 1px solid color-mix(in srgb, var(--fot-mid) 35%, transparent);
  color: var(--fot-mid); font-size: 13px;
}
.fot-warning :deep(svg) { flex-shrink: 0; margin-top: 1px; }

/* Плашка «Раздел в разработке» */
.fot-wip {
  display: flex; flex-direction: column; align-items: center; gap: var(--sp-2);
  text-align: center; padding: var(--sp-8, 48px) var(--sp-5);
  border: 1px dashed var(--fot-border); border-radius: 5px;
  background: var(--fot-card); color: var(--fot-ink);
}
.fot-wip-icon { width: 28px; height: 28px; color: var(--fot-accent); }
.fot-wip strong { font-size: 15px; font-weight: var(--fw-bold, 700); }
.fot-wip p { margin: 0; max-width: 640px; font-size: 13px; color: var(--fot-muted); }

.fot-body { display: flex; flex-direction: column; gap: var(--sp-4); }
.fot-body.pending { opacity: 0.55; pointer-events: none; }

/* KPI-карточки: 5 в ряд. Как карточки слайдов деки: плоская заливка,
   без тени и бордера, скругление 5px, подпись сверху мелко серым. */
.fot-kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: var(--sp-3); }
.fot-kpi {
  background: var(--fot-card);
  border: none; border-radius: 5px;
  padding: var(--sp-3) var(--sp-4);
  display: flex; flex-direction: column; gap: var(--sp-1);
  min-width: 0;
}
/* Подпись карточки — обычный регистр, как в деке (не дашбордный uppercase). */
.fot-kpi-label {
  font-size: 12px; color: var(--fot-muted);
}
.fot-kpi-value {
  font-family: var(--font-mono); font-variant-numeric: tabular-nums;
  font-size: 20px; font-weight: var(--fw-bold, 700);
  color: var(--fot-ink); white-space: nowrap;
}
.fot-kpi-unit { font-size: 12px; font-weight: var(--fw-regular); color: var(--fot-muted); margin-left: 2px; }

/* Секция графика/таблицы — без плашки-подложки, как на слайде:
   заголовок и содержимое прямо на белом листе. */
.card {
  border: none; background: transparent; padding: 0;
  display: flex; flex-direction: column; gap: var(--sp-2); min-width: 0;
}
.card-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-3); flex-wrap: wrap; }
/* Секционный подзаголовок — 15px bold, как в деке. */
.card-title { font-size: 15px; font-weight: var(--fw-bold, 700); color: var(--fot-ink); margin: 0; }
.card-note { font-size: 12px; color: var(--fot-muted); margin: 0; }
.hint { font-size: 12px; color: var(--fot-muted); font-style: italic; }

/* Высота графика задаётся здесь: Chart.js с maintainAspectRatio:false без явной
   высоты родителя растёт бесконечно. */
.chart-box { position: relative; height: 300px; min-width: 0; }
.chart-box :deep(canvas) { max-height: 100%; }

/* Крошки пути проваливания */
.crumbs { display: flex; align-items: center; gap: var(--sp-1); flex-wrap: wrap; margin-top: var(--sp-1); }
.crumb {
  border: none; background: transparent; padding: 0 2px; cursor: pointer;
  font-size: 12px; color: var(--fot-accent-light);
  max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.crumb:disabled { color: var(--fot-muted); cursor: default; }
.crumb-sep { font-size: 12px; color: var(--fot-muted); }

/* Таблица — как в деке: шапка на винном с белым текстом, зебра нечётных
   строк, тонкие горизонтальные разделители, без тяжёлых рамок и теней. */
.table-wrap { overflow-x: auto; }
.matrix { width: 100%; border-collapse: collapse; font-size: 13px; color: var(--fot-ink); }
.matrix th, .matrix td { padding: 7px 10px; white-space: nowrap; }
.matrix th {
  background: var(--fot-accent); color: var(--fot-accent-ink);
  text-align: right; font-weight: var(--fw-bold, 700); font-size: 12px;
}
.matrix td { border-bottom: 1px solid var(--fot-border); }
.matrix th.col-label, .matrix td.col-label { text-align: left; }
.th-unit { font-weight: var(--fw-regular); opacity: 0.8; }
.matrix td.num { text-align: right; font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.matrix tbody tr:nth-child(odd) { background: var(--fot-strip); }
.matrix tbody tr:nth-child(even) { background: transparent; }
.matrix tbody tr.drillable { cursor: pointer; }
.matrix tbody tr.drillable:hover { background: color-mix(in srgb, var(--fot-accent) 10%, transparent); }
.row-label { display: inline-flex; align-items: center; gap: var(--sp-1); }
.drill-icon { width: 12px; height: 12px; color: var(--fot-accent-light); opacity: 0.8; }
.empty-row { text-align: center; color: var(--fot-muted); padding: var(--sp-4) !important; }
/* Светофор Δ-ячеек — классы вешает cellDeltaClass по hex из API,
   красим через токены: в тёмной теме значения те же (палитра деки). */
.matrix td.delta-pos { color: var(--fot-pos); }
.matrix td.delta-mid { color: var(--fot-mid); }
.matrix td.delta-neg { color: var(--fot-neg); }
/* В тёмной теме шапка на светлом винном — белый текст не читается, берём тёмный. */
.fot-dark .matrix th { color: #2A1E22; }

/* Методология */
.fot-method {
  border: 1px solid var(--fot-border); border-radius: 5px;
  background: transparent; padding: var(--sp-3) var(--sp-4);
}
.fot-method summary {
  cursor: pointer; user-select: none;
  font-size: 14px; font-weight: var(--fw-bold, 700); color: var(--fot-ink);
}
.fot-method-text { margin: var(--sp-3) 0 0; font-size: 13px; color: var(--fot-muted); line-height: 1.55; white-space: pre-line; }
.fot-method-meta { margin: var(--sp-2) 0 0; font-size: 12px; color: var(--fot-muted); }

.spinning { animation: fot-spin 0.9s linear infinite; }
@keyframes fot-spin { to { transform: rotate(360deg); } }

@media (max-width: 720px) {
  .fot-kpi-value { font-size: 18px; }
}
</style>
