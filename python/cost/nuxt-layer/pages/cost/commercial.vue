<template>
  <div class="page-commercial">
    <header class="page-header">
      <div>
        <h1 class="page-title">Коммерческая эффективность и ценообразование</h1>
        <p class="page-sub">
          <span v-if="meta.cache_refreshed_at">Данные на {{ fmtDateTime(meta.cache_refreshed_at) }}</span>
          <span v-else class="warn">Свежесть кэша неизвестна</span>
          <span class="dot">·</span>
          <span :class="{ warn: coveragePct < 80 }">
            в расчёте {{ fmtInt(tiles.calc_count) }} из {{ fmtInt(meta.calc_total) }}
            калькуляций ({{ coveragePct }}%)
          </span>
          <span class="dot">·</span>
          <span>{{ fmtInt(tiles.model_count) }} моделей</span>
          <!-- Исключение из базы расчёта обязано быть видно рядом с цифрами:
               иначе дашборд читается как картина по всему ассортименту. -->
          <span class="dot">·</span>
          <span title="У полуфабрикатов цена и себестоимость в разных единицах">
            полуфабрикаты исключены
          </span>
        </p>
      </div>

      <div class="currency-switch" role="group" aria-label="Валюта">
        <button v-for="c in ['BYN', 'USD']" :key="c" class="btn btn-sm"
                :class="c === currency ? 'btn-primary' : 'btn-ghost'"
                @click="currency = c as 'BYN' | 'USD'">{{ c }}</button>
      </div>
    </header>

    <!-- Пустой объём выпуска — это не ноль, и подменять одно другим нельзя:
         показатели «выпуска» и «по калькуляциям» разные величины. -->
    <div v-if="!hasVolume" class="notice">
      Объём выпуска не заполнен, поэтому показатели считаются
      <strong>по калькуляциям</strong>, а не по выпуску: каждая калькуляция весит
      одинаково независимо от тиража. Взвешенные значения появятся после
      следующего обновления кэша.
    </div>

    <!-- В витрине две даты, и они про разное: «дата производства» — когда изделие
         выпущено (2024-2026, ровно по месяцам), «дата расчёта» — когда посчитали
         калькуляцию (в кэше это всегда последние месяцы, все 87 тыс. записей
         попадают в один-два месяца). Анализируем по дате выпуска — решение
         заказчика 14.08.2026. Подпись стоит здесь, чтобы разрез нельзя было
         спутать: выглядят два варианта одинаково, а показывают разное. -->
    <p class="basis">
      Год и месяц — по <strong>{{ meta.date_basis_label || 'дате производства' }}</strong>
    </p>

    <section class="filters">
      <div v-for="f in filterConfig" :key="f.key" class="filter-item">
        <label>
          {{ f.label }}
          <span v-if="truncatedFilters.includes(f.key)" class="trunc"
                title="Значений больше, чем показано — список обрезан, ищите поиском">неполный</span>
        </label>
        <CostMultiSelect v-model="selected[f.key]"
                         :options="optionsFor(f.key)"
                         :placeholder="`Все · ${f.label.toLowerCase()}`"
                         @change="reload" />
      </div>
      <button class="btn btn-ghost btn-sm reset" @click="resetFilters">Сбросить</button>
    </section>

    <p v-if="error" class="error">{{ error }}</p>

    <section class="tiles">
      <article v-for="t in tileList" :key="t.label" class="tile">
        <span class="tile-label">{{ t.label }}</span>
        <span class="tile-value" :class="{ empty: t.value === null }">
          {{ t.value === null ? '—' : t.format(t.value) }}
        </span>
        <span class="tile-note">{{ t.note }}</span>
      </article>
    </section>

    <section class="charts">
      <article class="card">
        <div class="ring-head">
          <h2 class="card-title">Динамика цен по месяцам</h2>
          <span class="hint">клик по месяцу — фильтр</span>
        </div>
        <p class="card-note">
          Медианы — средние здесь искажены выбросами.
          <!-- Фильтр «месяц» к этому графику не применяется намеренно: он и есть
               разрез по месяцам, иначе схлопнулся бы в одну точку. -->
          Разрез по месяцам (по {{ meta.date_basis_label || 'дате производства' }}),
          поэтому фильтр «Месяц» на график не влияет — только «Год».
        </p>
        <!-- Высоту задаёт КОНТЕЙНЕР, а не пропс графика. При
             maintainAspectRatio: false канвас растягивается на родителя, и если
             у родителя высоты нет — он растёт бесконечно: график уезжает вниз,
             ResizeObserver снова дёргает перерисовку, и страница мерцает. -->
        <div class="chart-box">
          <ClientOnly>
            <Line v-if="monthData" :data="monthData" :options="lineOptions" />
          </ClientOnly>
        </div>
      </article>

      <article class="card">
        <div class="ring-head">
          <h2 class="card-title">Структура себестоимости · {{ meta.structure_label }}</h2>
          <span v-if="meta.structure_can_drill" class="hint">клик по столбцу — провалиться</span>
        </div>
        <!-- Хлебные крошки проваливания. Уровень, на котором стоим, не кликается
             — кликаются только пройденные. -->
        <nav class="crumbs">
          <button class="crumb" :disabled="!structurePath.length" @click="drillTo(0)">Все</button>
          <template v-for="(c, i) in (meta.structure_path || [])" :key="i">
            <span class="crumb-sep">›</span>
            <button class="crumb" :disabled="i === structurePath.length - 1"
                    :title="c.label" @click="drillTo(i + 1)">{{ c.value }}</button>
          </template>
        </nav>
        <p class="card-note">
          Топ-10 по себестоимости, от большей к меньшей. Сегмент «прочее» —
          остаток до полной себестоимости: прямые затраты покрывают около 79%
        </p>
        <div class="chart-box chart-box-tall">
          <ClientOnly>
            <Bar v-if="structureData" :data="structureData" :options="structureOptions" />
          </ClientOnly>
        </div>
      </article>

      <article class="card">
        <div class="ring-head">
          <h2 class="card-title">
            Структура выпуска
            <span class="hint">— клик по сегменту фильтрует</span>
          </h2>
          <div class="ring-controls">
            <label>Измерение
              <select v-model="dimension" class="form-input" @change="reload">
                <option v-for="d in meta.dimensions" :key="d.key" :value="d.key">{{ d.label }}</option>
              </select>
            </label>
            <label>Мера
              <select v-model="measure" class="form-input" @change="reload">
                <option v-for="m in meta.measures" :key="m.key" :value="m.key">{{ m.label }}</option>
              </select>
            </label>
          </div>
        </div>
        <p class="card-note">
          Выпуск считается только по калькуляциям с признаком
          <strong>{{ meta.volume_sign || 'ФКСС' }}</strong> — остальные признаки
          плановые и предварительные, выпуском не являются
        </p>
        <p v-if="ringEmpty" class="card-note warn">
          Мера «{{ meta.measure_label }}» не заполнена в данных — график пуст, это не ноль
        </p>
        <div class="chart-box chart-box-tall">
          <ClientOnly>
            <Doughnut v-if="ringData && !ringEmpty" :data="ringData" :options="ringOptions" />
          </ClientOnly>
        </div>
      </article>
    </section>

    <CostChartInsight block="commercial_prices" block-title="Цены и себестоимость"
                      :charts="insightCharts" :context="insightContext" />

    <!-- Тот же макет, собранный в Superset — чтобы сравнить обе реализации
         на одном экране. Инструмент разработчика, не часть продукта. -->
    <section v-if="supersetUrl" class="card compare">
      <div class="compare-head">
        <div>
          <h2 class="card-title">Тот же дашборд в Superset — для сравнения</h2>
          <p class="card-note">
            Встроен без guest-токенов: iframe работает потому, что браузер уже
            залогинен в Superset на том же хосте. Прод-схема встраивания другая —
            см. <code>docs/bi/superset-prod-plan.md</code>.
            <br />
            Открытие блока запускает все запросы Superset заново — те самые
            18+ на дашборд.
          </p>
        </div>
        <div class="compare-actions">
          <button class="btn btn-ghost btn-sm" @click="showSuperset = !showSuperset">
            {{ showSuperset ? 'Скрыть' : 'Показать' }}
          </button>
          <a :href="supersetUrl" target="_blank" rel="noopener"
             class="btn btn-ghost btn-sm">Открыть отдельно</a>
        </div>
      </div>
      <!-- v-if, а не v-show: пока блок скрыт, iframe не создаётся и Superset
           не грузится. -->
      <iframe v-if="showSuperset" :src="supersetEmbedSrc" class="superset-frame"
              title="Дашборд Superset" loading="lazy" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Bar, Doughnut, Line } from 'vue-chartjs'
import {
  ArcElement, BarElement, CategoryScale, Chart as ChartJS, Legend,
  LineElement, LinearScale, PointElement, Tooltip,
} from 'chart.js'

ChartJS.register(ArcElement, BarElement, CategoryScale, Legend, LineElement,
                 LinearScale, PointElement, Tooltip)

const config = useRuntimeConfig()
const apiBase = computed(() =>
  config.public.costOnly ? '' : ((config.public.apiBase as string) || '')
)

/** Пользователя раздела бэкенд берёт из заголовка X-Cost-User — ни nginx-cost,
 * ни фасад основного контура его не подставляют, и глобального перехватчика
 * $fetch в проекте нет. Каждая страница раздела шлёт заголовок сама (тот же
 * приём в index.vue). Без него любой вызов возвращает 401. */
const { user } = useAuth()
const fetchHeaders = computed(() => {
  const email = user.value?.email || ''
  return email ? { 'X-Cost-User': email } : {}
})

/** Фильтры дашборда. Ключи совпадают с белым списком FILTERS в
 * app/commercial.py — сервер игнорирует всё, чего в нём нет.
 *
 * Год и месяц — вместо интервала дат (просьба заказчика 14.08.2026): дата
 * расчёта не бизнес-дата, и интервал по ней всё равно выставляли по месяцам.
 * «Уровень цен» убран, «Модель» и «Артикул» добавлены. Их списки длинные
 * (4 255 и 12 020 значений), поэтому сервер отдаёт первую тысячу и помечает
 * список неполным, а искать в нём нужно поиском внутри мультиселекта. */
const filterConfig = [
  { key: 'year', label: 'Год' },
  { key: 'month', label: 'Месяц' },
  { key: 'brand_manager', label: 'Бренд-менеджер' },
  { key: 'level01', label: 'Level 01' },
  { key: 'level02', label: 'Level 02' },
  { key: 'level03', label: 'Level 03' },
  { key: 'model_name', label: 'Наименование товара' },
  { key: 'model', label: 'Модель' },
  { key: 'articul', label: 'Артикул' },
  { key: 'country', label: 'Страна пр-ва' },
  { key: 'season', label: 'Сезон' },
  { key: 'calc_sign', label: 'Признак кальк.' },
]

const selected = reactive<Record<string, string[]>>(
  Object.fromEntries(filterConfig.map(f => [f.key, [] as string[]])) as any
)
const filterOptions = ref<Record<string, string[]>>({})
const truncatedFilters = ref<string[]>([])

const currency = ref<'BYN' | 'USD'>('BYN')
const dimension = ref('model_name')
const measure = ref('volume_pcs')
/** Путь проваливания по иерархии «структуры себестоимости»: бренд-менеджер →
 * Level 01…05 → товар. Это НЕ общий фильтр — он применяется только к этому
 * графику, иначе клик по столбцу менял бы и плитки, и динамику, и кольцо. */
const structurePath = ref<string[]>([])

const loading = ref(false)
const error = ref('')

/** Встроенный для сравнения дашборд Superset. Пустая строка в конфиге — блока
 * нет вовсе (см. nuxt-layer/nuxt.config.ts). */
const supersetUrl = computed(() => (config.public.supersetEmbedUrl as string) || '')
const showSuperset = ref(true)
/** standalone=1 убирает шапку и меню Superset — во встроенном виде они лишние
 * и мешают сравнивать сами графики. */
const supersetEmbedSrc = computed(() => {
  const u = supersetUrl.value
  if (!u) return ''
  return u + (u.includes('?') ? '&' : '?') + 'standalone=1'
})
const tiles = ref<Record<string, any>>({})
const months = ref<any[]>([])
const structure = ref<any[]>([])
const ring = ref<any[]>([])
const meta = ref<Record<string, any>>({ dimensions: [], measures: [], structure_path: [] })

// ── Загрузка ────────────────────────────────────────────────────────────────
//
// ОДИН запрос на весь дашборд: плитки, три серии и метаданные. Тот же макет в
// Superset разошёлся на двадцать с лишним запросов — по одному на виджет и на
// каждый фильтр. Переключение валюты сюда НЕ ходит: обе валюты уже в ответе.

function buildParams(): string {
  const p = new URLSearchParams()
  for (const f of filterConfig) {
    for (const v of selected[f.key] || []) p.append(f.key, v)
  }
  for (const v of structurePath.value) p.append('structure_path', v)
  p.set('dimension', dimension.value)
  p.set('measure', measure.value)
  return p.toString()
}

async function reload() {
  loading.value = true
  error.value = ''
  try {
    const res = await $fetch<any>(`${apiBase.value}/api/cost/commercial?${buildParams()}`,
                                  { headers: fetchHeaders.value })
    tiles.value = res.tiles || {}
    months.value = res.months || []
    structure.value = res.structure || []
    ring.value = res.ring || []
    meta.value = res.meta || { dimensions: [], measures: [], structure_path: [] }
    // Каскад: варианты пересчитаны под текущий выбор и приходят вместе с
    // данными. Отдельного запроса за ними нет — иначе списки успевали бы
    // разъехаться с цифрами, которые рядом.
    filterOptions.value = res.options || {}
    truncatedFilters.value = res.meta?.options_truncated || []
  } catch (e: any) {
    console.error('[cost] commercial dashboard failed', e)
    error.value = e?.data?.detail || e?.message || 'Не удалось загрузить дашборд'
  } finally {
    loading.value = false
  }
}

function resetFilters() {
  for (const f of filterConfig) selected[f.key] = []
  structurePath.value = []
  reload()
}

onMounted(reload)

// ── Фильтры: месяцы подписываем словами ─────────────────────────────────────

const MONTHS_FULL = ['январь', 'февраль', 'март', 'апрель', 'май', 'июнь',
                     'июль', 'август', 'сентябрь', 'октябрь', 'ноябрь', 'декабрь']
const MONTHS_SHORT = ['янв', 'фев', 'мар', 'апр', 'май', 'июн',
                      'июл', 'авг', 'сен', 'окт', 'ноя', 'дек']

/** Значение месяца в фильтре — '01'…'12' (так его понимает SQL). Показывать
 * пользователю номер незачем, поэтому мультиселекту отдаём {value,label}. */
function optionsFor(key: string): any[] {
  const raw = filterOptions.value[key] || []
  if (key !== 'month') return raw
  return raw.map(v => ({ value: v, label: MONTHS_FULL[Number(v) - 1] || v }))
}

// ── Проваливание по структуре себестоимости ─────────────────────────────────

function drillInto(value: string) {
  if (!meta.value.structure_can_drill || !value) return
  structurePath.value = [...structurePath.value, value]
  reload()
}

/** Обрезать путь до `depth` уровней: 0 — вернуться на самый верх. */
function drillTo(depth: number) {
  if (depth >= structurePath.value.length) return
  structurePath.value = structurePath.value.slice(0, depth)
  reload()
}

// ── Кросфильтрация ──────────────────────────────────────────────────────────
//
// Клик по элементу графика становится обычным фильтром — тем же, что в панели
// сверху. Так выбор ВИДЕН и снимается штатно (крестиком в мультиселекте), а не
// живёт отдельным невидимым состоянием, про которое пользователь забыл.
// Повторный клик по тому же значению снимает фильтр.

function toggleFilter(key: string, value: string) {
  if (!value || !(key in selected)) return
  const cur = selected[key] || []
  selected[key] = cur.includes(value) ? cur.filter(v => v !== value) : [...cur, value]
}

/** Клик по сегменту кольца → фильтр по текущему измерению кольца. Все измерения
 * есть и в фильтрах (для этого «Семья» и добавлена), так что записать выбор
 * всегда есть куда. */
function crossFilterRing(index: number) {
  const label = ring.value[index]?.label
  if (label === undefined || label === null) return
  toggleFilter(dimension.value, String(label))
  reload()
}

/** Клик по точке динамики → фильтр по этому месяцу. Год выставляется тоже:
 * без него «июнь» означал бы все июни всех лет. Сам график на месяц не
 * реагирует (он и есть разрез по месяцам), но реагирует на год — окно графика
 * сузится до выбранного года, а остальные виджеты до месяца. */
function crossFilterMonth(index: number) {
  const ym = months.value[index]?.ym
  if (!ym) return
  const [year, month] = String(ym).split('-')
  const already = (selected.year || []).includes(year)
    && (selected.month || []).includes(month)
  if (already) {
    selected.year = (selected.year || []).filter(v => v !== year)
    selected.month = (selected.month || []).filter(v => v !== month)
  } else {
    selected.year = [year]
    selected.month = [month]
  }
  reload()
}

// ── Форматтеры ──────────────────────────────────────────────────────────────

const nf2 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const nf0 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 0 })
const fmtMoney = (v: number) => nf2.format(v)
const fmtPct = (v: number) => `${nf2.format(v)}%`
const fmtInt = (v: any) => (v === null || v === undefined ? '—' : nf0.format(Number(v)))
const fmtDateTime = (iso: string) =>
  new Date(iso).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })

const coveragePct = computed(() => {
  const a = Number(tiles.value.calc_count || 0)
  const b = Number(meta.value.calc_total || 0)
  return b ? Math.round((100 * a) / b) : 0
})

/** Есть ли объём выпуска. От этого зависит, взвешенные показатели или нет —
 * и подписи плиток обязаны это отражать. */
const hasVolume = computed(() => tiles.value.volume_total !== null
  && tiles.value.volume_total !== undefined)

const cur = computed(() => (currency.value === 'BYN' ? 'byn' : 'usd'))
const curLabel = computed(() => currency.value)

function pick(base: string): number | null {
  const v = tiles.value[`${base}_${cur.value}`]
  return v === null || v === undefined ? null : Number(v)
}

/** Плитки. Взвешенные показатели берём только когда объём есть — иначе честно
 * подписываем «по калькуляциям» и показываем невзвешенные. */
const tileList = computed(() => {
  const marginKey = hasVolume.value ? 'margin_pct_w' : 'margin_pct'
  const profitKey = hasVolume.value ? 'profit_pct_w' : 'profit_pct'
  const sign = meta.value.volume_sign || 'ФКСС'
  const scope = hasVolume.value ? `взвешено по выпуску, ${sign}` : 'по калькуляциям, без веса'
  const num = (k: string) => {
    const v = tiles.value[k]
    return v === null || v === undefined ? null : Number(v)
  }
  return [
    { label: `Маржинальность${hasVolume.value ? ' выпуска' : ''}, %`, value: num(marginKey),
      format: fmtPct, note: `наценка / отпускная · ${scope}` },
    { label: `Рентабельность${hasVolume.value ? ' выпуска' : ''}, %`, value: num(profitKey),
      format: fmtPct, note: `наценка / себестоимость · ${scope}` },
    { label: 'Выпуск, шт', value: num('volume_total'),
      format: (v: number) => nf0.format(v), note: `сумма тиражей · ${sign}` },
    { label: `Себестоимость, ${curLabel.value}`, value: pick('cost'),
      format: fmtMoney, note: 'медиана' },
    { label: `Отпускная цена, ${curLabel.value}`, value: pick('price'),
      format: fmtMoney, note: 'медиана по уровню цен' },
    { label: `Розничная цена, ${curLabel.value}`, value: pick('retail'),
      format: fmtMoney, note: 'медиана, в наценку не входит' },
    { label: `Наценка, ${curLabel.value}`, value: pick('markup'),
      format: fmtMoney, note: 'отпускная − себестоимость, медиана' },
  ]
})

// ── Цвета из дизайн-системы ─────────────────────────────────────────────────
//
// Хардкодить цвета запрещено — берём токены из :root в рантайме. Категориальной
// палитры в дизайн-системе НЕТ (есть только accent/info/pos/neg), поэтому для
// семи серий стека строим рамп из этих четырёх с прозрачностями. Если палитра
// появится, менять надо здесь.

const palette = ref<string[]>([])
const ink = ref('#6b7280')
const grid = ref('rgba(127,127,127,.2)')

/** #rrggbb → rgba(). Canvas НЕ понимает color-mix() и прочий современный CSS:
 * такие значения молча игнорируются, и серия рисуется чёрной либо исчезает.
 * Токены дизайн-системы заданы обычным hex, поэтому альфа-варианты считаем сами. */
function withAlpha(hex: string, alpha: number): string {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim())
  if (!m) return hex
  const n = parseInt(m[1], 16)
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`
}

function readTokens() {
  if (typeof window === 'undefined') return
  const cs = getComputedStyle(document.documentElement)
  const tok = (n: string, fb: string) => (cs.getPropertyValue(n) || '').trim() || fb
  const accent = tok('--accent', '#4338ca')
  const info = tok('--info', '#0b5cad')
  const pos = tok('--pos', '#0a7f3f')
  const neg = tok('--neg', '#b42318')
  ink.value = tok('--text-muted', '#6b7280')
  grid.value = withAlpha(tok('--border', '#e3e6ea'), 0.9)
  // Категориальной палитры в дизайн-системе нет — только accent/info/pos/neg.
  // Семь серий стека набираем рампом: сначала базовые цвета, затем те же с
  // прозрачностью. Появится палитра — менять здесь.
  const base = [accent, info, pos, neg]
  palette.value = [
    ...base,
    ...base.map((c) => withAlpha(c, 0.6)),
    ...base.map((c) => withAlpha(c, 0.35)),
  ]
}

// Тему кабинета можно переключить на ходу, а токены прочитаны один раз — без
// этого графики остались бы в светлых цветах на тёмном фоне. Следим за
// атрибутами <html>, каким бы способом тема ни переключалась.
let themeObserver: MutationObserver | null = null
onMounted(() => {
  readTokens()
  themeObserver = new MutationObserver(readTokens)
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['class', 'data-theme', 'style'],
  })
})
onBeforeUnmount(() => themeObserver?.disconnect())

const color = (i: number) => palette.value[i % (palette.value.length || 1)] || '#888'

// ── Данные графиков ─────────────────────────────────────────────────────────
// Переключение валюты пересобирает серии из уже загруженного ответа — без
// обращения к серверу.

/** Подпись месяца: внутри одного года достаточно названия, на нескольких годах
 * без года подписи станут неразличимы (два «июня» подряд). */
function monthLabel(ym: string, multiYear: boolean): string {
  const [y, m] = String(ym).split('-')
  const name = MONTHS_SHORT[Number(m) - 1] || m
  return multiYear ? `${name} ${y}` : name
}

const monthData = computed(() => {
  if (!months.value.length) return null
  const k = cur.value
  const multiYear = new Set(months.value.map(m => String(m.ym).slice(0, 4))).size > 1
  return {
    labels: months.value.map(m => monthLabel(m.ym, multiYear)),
    datasets: [
      { label: `Себестоимость, ${curLabel.value}`, data: months.value.map(m => Number(m[`cost_${k}`])),
        borderColor: color(0), backgroundColor: color(0), tension: 0.25 },
      { label: `Отпускная, ${curLabel.value}`, data: months.value.map(m => Number(m[`price_${k}`])),
        borderColor: color(1), backgroundColor: color(1), tension: 0.25 },
      { label: `Розничная, ${curLabel.value}`, data: months.value.map(m => Number(m[`retail_${k}`])),
        borderColor: color(2), backgroundColor: color(2), tension: 0.25 },
    ],
  }
})

const STRUCTURE_PARTS = [
  ['mat_main', 'Основные материалы'],
  ['mat_aux', 'Вспомогательные'],
  ['sewing', 'Пошив'],
  ['cutting', 'Раскрой'],
  ['decor', 'Декоры'],
  ['knitting', 'Вязание'],
  ['other', 'Прочее'],
] as const

const structureData = computed(() => {
  if (!structure.value.length) return null
  return {
    labels: structure.value.map(r => r.label),
    datasets: STRUCTURE_PARTS.map(([key, label], i) => ({
      label, data: structure.value.map(r => Number(r[key] || 0)), backgroundColor: color(i),
    })),
  }
})

const ringEmpty = computed(() =>
  !ring.value.length || ring.value.every(r => r.value === null || Number(r.value) === 0))

const ringData = computed(() => {
  if (!ring.value.length) return null
  return {
    labels: ring.value.map(r => r.label),
    datasets: [{
      data: ring.value.map(r => Number(r.value || 0)),
      backgroundColor: ring.value.map((_, i) => color(i)),
      borderWidth: 0,
    }],
  }
})

// ── Настройки графиков ──────────────────────────────────────────────────────

const baseOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: { position: 'bottom' as const, labels: { color: ink.value, boxWidth: 10, font: { size: 11 } } },
  },
}))

/** Курсор-указатель поверх канваса: «кликабельность» иначе никак не видна —
 * Chart.js рисует в канвасе, а не в DOM. */
function pointerOnHover(e: any, elements: any[]) {
  const canvas = e?.native?.target
  if (canvas) canvas.style.cursor = elements.length ? 'pointer' : 'default'
}

const lineOptions = computed(() => ({
  ...baseOptions.value,
  // mode: 'index' — клик и подсказка ловятся по всей вертикали месяца, а не
  // только точным попаданием в точку линии.
  interaction: { mode: 'index' as const, intersect: false },
  onClick: (_e: any, elements: any[]) => {
    const idx = elements?.[0]?.index
    if (idx !== undefined && idx !== null) crossFilterMonth(idx)
  },
  onHover: pointerOnHover,
  scales: {
    x: { ticks: { color: ink.value }, grid: { color: grid.value } },
    y: { ticks: { color: ink.value }, grid: { color: grid.value }, title: { display: true, text: curLabel.value } },
  },
}))

const stackedOptions = computed(() => ({
  ...baseOptions.value,
  scales: {
    x: { stacked: true, ticks: { color: ink.value, maxRotation: 60, minRotation: 30 }, grid: { display: false } },
    y: { stacked: true, ticks: { color: ink.value }, grid: { color: grid.value }, title: { display: true, text: 'BYN' } },
  },
}))

/** Стек себестоимости + проваливание по клику. Курсор меняем сами: Chart.js
 * рисует в канвасе, и «кликабельность» столбца иначе никак не видна. */
const structureOptions = computed(() => ({
  ...stackedOptions.value,
  onClick: (_e: any, elements: any[]) => {
    const idx = elements?.[0]?.index
    if (idx === undefined || idx === null) return
    drillInto(structure.value[idx]?.label)
  },
  onHover: (e: any, elements: any[]) => {
    const canvas = e?.native?.target
    if (canvas) {
      canvas.style.cursor =
        elements.length && meta.value.structure_can_drill ? 'pointer' : 'default'
    }
  },
}))

const ringOptions = computed(() => ({
  ...baseOptions.value,
  cutout: '55%',
  onClick: (_e: any, elements: any[]) => {
    const idx = elements?.[0]?.index
    if (idx !== undefined && idx !== null) crossFilterRing(idx)
  },
  onHover: pointerOnHover,
  plugins: {
    ...baseOptions.value.plugins,
    legend: { position: 'right' as const, labels: { color: ink.value, boxWidth: 10, font: { size: 11 } } },
    // Штуки выпуска — это миллионы, и без доли в процентах сегменты нечитаемы.
    tooltip: {
      callbacks: {
        label: (ctx: any) => {
          const v = Number(ctx.parsed) || 0
          const total = (ctx.dataset?.data || []).reduce((s: number, x: any) => s + Number(x || 0), 0)
          const share = total ? ` · ${nf2.format((100 * v) / total)}%` : ''
          return ` ${ctx.label}: ${nf0.format(v)}${share}`
        },
      },
    },
  },
}))

// ── Данные для блока «Разбор ИИ» ───────────────────────────────────────────
// Блок ставится под секцией графиков целиком: связка «себестоимость растёт
// быстрее отпускной цены» видна только на нескольких графиках сразу.

/** Из данных Chart.js оставляем подписи и числа.
 *
 * Цвета и оформление в промпте бесполезны, зато раздувают его: каждая серия
 * тащит массивы backgroundColor на все точки. Имя серии передаём явно —
 * у кольца датасет без label, и в фактах он выглядел бы как «серия». */
function slim(data: any, fallbackLabel?: string): any | null {
  if (!data?.datasets?.length) return null
  return {
    labels: (data.labels || []).map((l: any) => String(l)),
    datasets: data.datasets.map((ds: any, i: number) => ({
      label: String(ds.label ?? (i === 0 && fallbackLabel ? fallbackLabel : 'серия')),
      data: (ds.data || []).map((v: any) => (typeof v === 'number' ? v : (v == null ? null : Number(v)))),
    })),
  }
}

function chartsOf(items: Array<{ title: string; note?: string; raw: any; seriesName?: string }>) {
  return items
    .map(i => ({ title: i.title, note: i.note, series: slim(i.raw, i.seriesName) }))
    .filter(c => c.series !== null)
}

/** Что человек видел на экране: период, валюта, признак, разрез, фильтры. */
const insightContext = computed(() => {
  const active: Record<string, string> = {}
  for (const f of filterConfig) {
    const vals = selected[f.key] || []
    if (vals.length) active[f.label] = vals.join(', ')
  }
  const dimLabel = (meta.value.dimensions || [])
    .find((d: any) => d.key === dimension.value)?.label || dimension.value
  // Та же ловушка, что на «Марже выпуска»: график динамики цен игнорирует
  // фильтр месяца (он и есть разрез по месяцам), и без этой оговорки модель
  // считала бы, что видит выбранный месяц.
  const monthsPicked = (selected.month || []).length > 0
  return {
    'Фильтр периода': [
      (selected.year || []).join(', ') || 'все годы',
      monthsPicked ? (selected.month || []).join(', ') : 'все месяцы',
    ].join(' · '),
    'Что показано на графиках': monthsPicked
      ? 'График динамики цен показывает ВСЕ месяцы выбранного года — фильтр '
        + 'месяца на него не влияет (он применён к плиткам и структуре). '
        + 'Выводы о динамике делай по месяцам, показанным на графике.'
      : 'График динамики цен показывает все месяцы выбранного года.',
    'Валюта': currency.value,
    'Разрез структуры выпуска': dimLabel,
    ...(meta.value.structure_label ? { 'Разрез структуры себестоимости': meta.value.structure_label } : {}),
    ...active,
  }
})

const insightCharts = computed(() => chartsOf([
  { title: `Динамика цен по месяцам, ${curLabel.value}`, raw: monthData.value,
    note: 'Себестоимость против отпускной и розничной цены по месяцам' },
  { title: `Структура себестоимости · ${meta.value.structure_label || ''}`,
    raw: structureData.value,
    note: 'Статьи затрат: материалы, пошив, раскрой, декоры, вязание, прочее' },
  { title: 'Структура выпуска', raw: ringEmpty.value ? null : ringData.value,
    seriesName: 'Доля в выпуске',
    note: `Доли по измерению «${(meta.value.dimensions || []).find((d: any) => d.key === dimension.value)?.label || dimension.value}»` },
]))
</script>

<style scoped>
/* Только токены дизайн-системы: кабинет позже получит брендинг через них. */
.page-commercial { display: flex; flex-direction: column; gap: var(--sp-5); }

.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-4); }
.page-title { font-size: var(--fs-xl); font-weight: var(--fw-semibold); margin: 0; }
.page-sub { margin: var(--sp-1) 0 0; font-size: var(--fs-xs); color: var(--text-muted); }
.dot { margin: 0 var(--sp-2); opacity: .5; }
.warn { color: var(--neg); }

.currency-switch { display: flex; gap: var(--sp-1); }

.notice {
  border: 1px solid var(--border); border-radius: var(--rd-3);
  background: var(--bg-surface-2); padding: var(--sp-3) var(--sp-4);
  font-size: var(--fs-xs); color: var(--text-muted);
}

.filters {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
  gap: var(--sp-3); align-items: end;
}
.basis { margin: 0; font-size: var(--fs-2xs); color: var(--text-muted); }
.filter-item { display: flex; flex-direction: column; gap: var(--sp-1); }
.filter-item label { font-size: var(--fs-2xs); color: var(--text-muted); display: flex; gap: var(--sp-1); align-items: baseline; }
.trunc { color: var(--neg); font-size: var(--fs-2xs); }
.reset { align-self: end; }

.error { color: var(--neg); font-size: var(--fs-sm); }

.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: var(--sp-3); }
.tile {
  border: 1px solid var(--border); border-radius: var(--rd-3);
  background: var(--bg-surface); padding: var(--sp-3) var(--sp-4);
  display: flex; flex-direction: column; gap: var(--sp-1);
}
.tile-label { font-size: var(--fs-2xs); color: var(--text-muted); text-transform: uppercase; letter-spacing: .04em; }
/* Числа моно + tabular-nums — правило дизайн-системы. */
.tile-value { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: var(--fs-2xl); font-weight: var(--fw-semibold); }
.tile-value.empty { color: var(--text-muted); font-weight: var(--fw-regular); }
.tile-note { font-size: var(--fs-2xs); color: var(--text-muted); }

.charts { display: grid; grid-template-columns: repeat(auto-fit, minmax(360px, 1fr)); gap: var(--sp-4); }
.card {
  border: 1px solid var(--border); border-radius: var(--rd-3);
  background: var(--bg-surface); padding: var(--sp-4);
  display: flex; flex-direction: column; gap: var(--sp-2); min-width: 0;
}
.card-title { font-size: var(--fs-md); font-weight: var(--fw-medium); margin: 0; }
.card-note { font-size: var(--fs-2xs); color: var(--text-muted); margin: 0; }
.hint { font-size: var(--fs-2xs); color: var(--text-muted); font-style: italic; }

/* Хлебные крошки проваливания. */
.crumbs { display: flex; align-items: center; gap: var(--sp-1); flex-wrap: wrap; }
.crumb {
  border: none; background: transparent; padding: 0 2px; cursor: pointer;
  font-size: var(--fs-2xs); color: var(--accent); max-width: 200px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.crumb:disabled { color: var(--text-muted); cursor: default; }
.crumb-sep { font-size: var(--fs-2xs); color: var(--text-muted); }

/* Высота графика задаётся здесь и только здесь. Chart.js с
   maintainAspectRatio: false тянется на родителя — без явной высоты канвас
   растёт бесконечно, а ResizeObserver зацикливает перерисовку. */
.chart-box { position: relative; height: 260px; min-width: 0; }
.chart-box-tall { height: 320px; }
.chart-box :deep(canvas) { max-height: 100%; }

.compare-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-4); flex-wrap: wrap; }
.compare-actions { display: flex; gap: var(--sp-2); flex-shrink: 0; }
/* Высота под полный дашборд Superset: он длинный, а вложенная полоса прокрутки
   для сравнения хуже — глаз теряет соответствие блоков. */
.superset-frame {
  width: 100%; height: 1400px; border: 1px solid var(--border);
  border-radius: var(--rd-3); background: var(--bg-surface-2);
}

.ring-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-3); flex-wrap: wrap; }
.ring-controls { display: flex; gap: var(--sp-3); }
.ring-controls label { display: flex; flex-direction: column; gap: var(--sp-1); font-size: var(--fs-2xs); color: var(--text-muted); }
</style>
