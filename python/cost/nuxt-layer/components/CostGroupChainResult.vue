<template>
  <section class="chain-result">
    <!-- Шапка: что за цепочка и как она считалась. Режим и вес — подписями от
         сервера (meta.mode_label / base.weight_label), а не своими словами:
         формулировка семантики расчёта живёт в одном месте, в app/group_pricing.py. -->
    <header class="result-head">
      <div class="head-text">
        <h3 class="result-title">Финрез · {{ chain?.name || `цепочка #${chainId}` }}</h3>
        <p v-if="result" class="result-sub">
          <span>{{ meta.mode_label }}</span>
          <span class="dot">·</span>
          <span>{{ base.weight_label }}</span>
          <template v-if="meta.cache_refreshed_at">
            <span class="dot">·</span>
            <span>данные на {{ fmtDateTime(meta.cache_refreshed_at) }}</span>
          </template>
        </p>
        <!-- Полнота обязана стоять рядом с цифрами: строки без цены (и без объёма
             при весе по выпуску) в суммы не входят, и без этой подписи «Σ по
             цепочке» читалась бы как сумма по всему ассортименту. -->
        <p v-if="result" class="result-sub">
          в расчёте {{ fmtInt(inMoney) }} из {{ fmtInt(base.rows) }} калькуляций,
          <span :class="{ warn: num(base.rows_without_price) > 0 }">без цены {{ fmtInt(base.rows_without_price) }}</span>
          <!-- Запятая вплотную к открывающему тегу: с переносом строки перед ней
               Vue сжимал отступ в пробел, и подпись читалась «без цены 0 , без объёма 0». -->
          <template v-if="base.weight === 'volume'">, <span :class="{ warn: num(base.rows_without_volume) > 0 }">без объёма {{ fmtInt(base.rows_without_volume) }}</span></template>
          <span class="dot">·</span>
          <span class="sources" title="Откуда взята цена калькуляции — приоритет тот же, что в главной таблице: заявка БМ → DWH → аудит → расчёт">
            источники цен: {{ priceSourcesLabel }}
          </span>
        </p>
      </div>

      <div class="head-actions">
        <!-- Пустая дата = как у цепочки (или сегодня) — выбор делает сервер, а мы
             показываем рядом, на какую дату курс реально взят (rate.date). -->
        <label class="rate-date" title="Дата курса НБ РБ для звеньев с валютой отгрузки ≠ BYN. Пусто — дата цепочки или сегодня">
          <span>Курс НБ РБ на</span>
          <!-- lazy: год набирается по цифрам, и без него каждая промежуточная
               дата («0002-…») улетала бы отдельным запросом за курсом. -->
          <input v-model.lazy="rateDate" type="date" class="form-input" />
        </label>
        <span v-if="rate" class="hint">взят на {{ rate.date }}</span>
        <button class="btn btn-ghost btn-sm" :disabled="!result" title="Выгрузить звенья и матрицу в Excel" @click="exportExcel">
          <Icon name="lucide:file-spreadsheet" /> Excel
        </button>
        <button class="btn btn-ghost btn-sm" @click="emit('close')">
          <Icon name="lucide:x" /> Закрыть
        </button>
      </div>
    </header>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="loading && !result" class="loading">Загрузка финреза…</p>

    <template v-if="result">
      <!-- Плитки итога. Все суммы в BYN: валюта звена — только дополнительный
           показ его сумм, база расчёта всегда рубль (заказчик, 23.09.2026). -->
      <section class="tiles" :class="{ dim: loading }">
        <article v-for="t in tiles" :key="t.key" class="tile">
          <span class="tile-label">{{ t.label }}</span>
          <span class="tile-value" :class="t.cls">{{ t.value }}</span>
          <span v-if="t.note" class="tile-note" :title="t.noteTitle">{{ t.note }}</span>
        </article>
      </section>

      <!-- Звенья -->
      <section class="block" :class="{ dim: loading }">
        <div class="block-head">
          <h4 class="block-title">Звенья цепочки</h4>
          <span class="hint">суммы — Σ по калькуляциям с ценой, BYN</span>
        </div>
        <div class="table-wrap">
          <table class="grid">
            <thead>
              <tr>
                <th class="col-num">№</th>
                <th class="col-label">Компания</th>
                <th title="Корректировка к цене продажи звена: + наценка / − скидка">% корр.</th>
                <th title="Цена продажи звена к базовой отпускной — этот процент уходит в региональные 1С">% к базовой</th>
                <th>Цена входа Σ</th>
                <th>Цена выхода Σ</th>
                <th>Доход Σ</th>
                <th title="доход звена / цена входа">Доход, % к входу</th>
                <th title="доход звена / доход группы">Доля в доходе группы</th>
                <template v-if="hasForeign">
                  <th>Валюта</th>
                  <th title="цена входа Σ в валюте отгрузки звена">Вход Σ в валюте</th>
                  <th title="цена выхода Σ в валюте отгрузки звена">Выход Σ в валюте</th>
                  <th title="доход Σ в валюте отгрузки звена">Доход Σ в валюте</th>
                </template>
              </tr>
            </thead>
            <tbody>
              <tr v-for="l in links" :key="l.seq">
                <td class="num col-num">{{ l.seq }}</td>
                <td class="col-label">
                  {{ l.company_name }}
                  <span v-if="l.seq === links.length" class="muted">→ внешний рынок</span>
                </td>
                <td class="num" :class="signCls(l.adjust_pct)">{{ fmtSignedPct(l.adjust_pct) }}</td>
                <td class="num" :class="signCls(l.eff_pct)">{{ fmtSignedPct(l.eff_pct) }}</td>
                <td class="num">{{ fmtMoney(l.in_sum) }}</td>
                <td class="num">{{ fmtMoney(l.out_sum) }}</td>
                <td class="num" :class="signCls(l.income_sum)">{{ fmtMoney(l.income_sum) }}</td>
                <td class="num" :title="`к выходу: ${fmtPct(l.income_pct_out)}`">{{ fmtPct(l.income_pct_in) }}</td>
                <td class="num">{{ fmtPct(l.income_share) }}</td>
                <template v-if="hasForeign">
                  <td class="num" :title="rateTitle(l)">{{ l.currency || 'BYN' }}</td>
                  <td class="num" :title="rateTitle(l)">{{ isForeign(l) ? fmtMoney(l.in_sum_cur) : '—' }}</td>
                  <td class="num" :title="rateTitle(l)">{{ isForeign(l) ? fmtMoney(l.out_sum_cur) : '—' }}</td>
                  <td class="num" :class="signCls(l.income_sum)" :title="rateTitle(l)">{{ isForeign(l) ? fmtMoney(l.income_sum_cur) : '—' }}</td>
                </template>
              </tr>
              <tr v-if="!links.length"><td :colspan="hasForeign ? 13 : 9" class="empty-row">У цепочки нет звеньев</td></tr>
            </tbody>
          </table>
        </div>
        <!-- Контрольная строка. Тождество §4.4: доход группы = конечная − себестоимость
             = Σ доходов звеньев. Считается сервером на float, здесь только сверка —
             расхождение больше копейки означает ошибку расчёта, а не округления. -->
        <p class="control" :class="identity.ok ? 'ok' : 'warn'">
          Контроль: доход группы {{ fmtMoney(total.income_sum) }}
          = конечная {{ fmtMoney(total.final_sum) }} − себестоимость {{ fmtMoney(total.cost_sum) }};
          Σ доходов звеньев {{ fmtMoney(identity.linksSum) }}
          <span v-if="identity.ok">— сходится</span>
          <span v-else>— расхождение {{ fmtMoney(identity.diff) }}</span>
        </p>
      </section>

      <!-- Водопад: себестоимость → доход каждого звена → конечная цена. Плавающие
           столбцы [start, end], отрицательный доход — цветом neg. -->
      <section class="block" :class="{ dim: loading }">
        <div class="block-head">
          <h4 class="block-title">Водопад дохода, BYN</h4>
          <span class="hint">себестоимость → доход по звеньям → конечная цена</span>
        </div>
        <div class="chart-box">
          <ClientOnly>
            <Bar v-if="waterfallData" :data="waterfallData" :options="waterfallOptions" />
          </ClientOnly>
        </div>
      </section>

      <!-- Матрица с проваливанием Level 01 → … → Level 05 → модель → артикул.
           Путь применяется только к матрице: плитки, звенья и водопад — по всей цепочке. -->
      <section class="block" :class="{ dim: loading }">
        <div class="block-head">
          <div>
            <h4 class="block-title">Матрица · {{ meta.label }}</h4>
            <nav class="crumbs">
              <button class="crumb" :disabled="!path.length" @click="drillTo(0)">Все</button>
              <template v-for="(c, i) in (meta.path || [])" :key="i">
                <span class="crumb-sep">›</span>
                <button class="crumb" :disabled="i === path.length - 1" :title="c.label" @click="drillTo(i + 1)">
                  {{ c.value || '(не указано)' }}
                </button>
              </template>
            </nav>
          </div>
        </div>
        <p class="block-note">
          <span v-if="meta.can_drill">Клик по строке — провалиться на уровень ниже. </span>
          Строки отсортированы по доходу группы.
          <span v-if="meta.truncated" class="warn">
            Показаны первые {{ fmtInt(meta.row_limit) }} строк — провалитесь глубже или сузьте ассортимент цепочки.
          </span>
        </p>
        <div class="table-wrap">
          <table class="grid">
            <thead>
              <tr>
                <th class="col-label">{{ meta.label }}</th>
                <th v-for="c in matrixColumns" :key="c.key" :title="c.title">{{ c.label }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in matrix" :key="row.label ?? '∅'"
                  :class="{ drillable: meta.can_drill }"
                  @click="drillInto(row.label)">
                <td class="col-label">
                  <span class="row-label">{{ row.label || '(не указано)' }}</span>
                  <span v-if="row.name" class="row-name">{{ row.name }}</span>
                </td>
                <td v-for="c in matrixColumns" :key="c.key" class="num" :class="c.cls?.(row)" :title="c.cellTitle?.(row)">
                  {{ c.fmt(row) }}
                </td>
              </tr>
              <tr v-if="!matrix.length">
                <td :colspan="matrixColumns.length + 1" class="empty-row">Нет калькуляций с ценой в этом срезе</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Bar } from 'vue-chartjs'
import {
  BarController, BarElement, CategoryScale, Chart as ChartJS, Legend, LinearScale, Tooltip,
} from 'chart.js'
import { useChartTheme } from '~/composables/useChartTheme'

// Финрез цепочки поставки группы (контракт 23.09.2026, §6.2). Компонент
// самостоятелен: сам ходит за GET /group/chains/{id}/result, страница
// /cost/group-pricing только монтирует его и слушает close.
ChartJS.register(BarController, BarElement, CategoryScale, Legend, LinearScale, Tooltip)

const props = defineProps<{ chainId: number }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const config = useRuntimeConfig()
const apiBase = computed(() => (config.public.costOnly ? '' : ((config.public.apiBase as string) || '')))
/** Бэкенд берёт пользователя из X-Cost-User — заголовок шлёт каждая страница
 * и компонент раздела сам (образец reg713.vue). */
const user = useState<any>('auth-user')
const fetchHeaders = computed(() => {
  const email = user.value?.email || ''
  return email ? { 'X-Cost-User': email } : {}
})

const { ink, grid, accent, pos, neg, withAlpha } = useChartTheme()

// ── Состояние ────────────────────────────────────────────────────────────────

const result = ref<any>(null)
const loading = ref(false)
const error = ref('')
/** Путь проваливания по матрице: значения Level 01 → … → артикул. Пустая
 * строка — калькуляции без уровня (у 31 721 калькуляции нет Level 01). */
const path = ref<string[]>([])
/** Дата курса; пусто — сервер берёт дату цепочки или сегодня. */
const rateDate = ref('')

const chain = computed<any>(() => result.value?.chain || null)
const rate = computed<any>(() => result.value?.rate || null)
const base = computed<any>(() => result.value?.base || {})
const links = computed<any[]>(() => result.value?.links || [])
const total = computed<any>(() => result.value?.total || {})
const matrix = computed<any[]>(() => result.value?.matrix || [])
const meta = computed<any>(() => result.value?.meta || { path: [] })

// ── Загрузка ─────────────────────────────────────────────────────────────────
// Ответы могут приходить не по порядку (быстрые клики по крошкам) — поздний
// ответ на ранний запрос отбрасывается по счётчику.

let reqSeq = 0
/** prevPath — путь до того, как его поменяло проваливание: при ошибке
 * возвращаемся к нему, иначе крошки показывали бы срез, которого сервер не
 * отдал. По умолчанию — текущий путь (перезапрос по дате курса путь не меняет). */
async function load(prevPath: string[] = path.value) {
  const my = ++reqSeq
  loading.value = true
  error.value = ''
  try {
    const p = new URLSearchParams()
    for (const v of path.value) p.append('path', v)
    if (rateDate.value) p.set('rate_date', rateDate.value)
    const qs = p.toString()
    const res = await $fetch<any>(
      `${apiBase.value}/api/cost/group/chains/${props.chainId}/result${qs ? '?' + qs : ''}`,
      { headers: fetchHeaders.value },
    )
    if (my !== reqSeq) return
    result.value = res
    // Путь сверяем с серверным: он обрезает лишние уровни (глубже артикула
    // проваливаться некуда), и локальный путь не должен расходиться с крошками.
    // path не в watch — присваивание не порождает повторного запроса.
    const serverPath = (res?.meta?.path || []).map((x: any) => String(x?.value ?? ''))
    if (serverPath.join('\u0000') !== path.value.join('\u0000')) path.value = serverPath
  } catch (e: any) {
    if (my !== reqSeq) return
    path.value = prevPath
    console.error('[cost] group chain result failed', e)
    error.value = e?.data?.detail || e?.message || 'Не удалось загрузить финрез цепочки'
  } finally {
    if (my === reqSeq) loading.value = false
  }
}

onMounted(load)
// Смена цепочки — другой ассортимент, путь проваливания теряет смысл; старый
// результат убираем сразу, чтобы под новым именем не мигали чужие цифры.
watch(() => props.chainId, () => {
  path.value = []
  result.value = null
  load()
})
watch(rateDate, () => load())

// Пока идёт запрос, проваливание не принимаем: второй клик по приглушённой
// таблице уводил бы путь на два уровня от того, что пользователь видит.
function drillInto(value: any) {
  if (!meta.value.can_drill || loading.value) return
  const prev = path.value
  path.value = [...path.value, value ?? '']
  load(prev)
}

function drillTo(depth: number) {
  if (loading.value || depth >= path.value.length) return
  const prev = path.value
  path.value = path.value.slice(0, depth)
  load(prev)
}

// ── Форматтеры ───────────────────────────────────────────────────────────────
// Суммы — с двумя знаками всегда (tabular-nums выравнивает колонку), проценты —
// с одним, «—» для null.

const nf0 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 0 })
const nf1 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 1, maximumFractionDigits: 1 })
const nf1s = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 1, maximumFractionDigits: 1, signDisplay: 'exceptZero' })
const nf2 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const nfRate = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 6 })
const isNum = (v: any) => v !== null && v !== undefined && v !== '' && Number.isFinite(Number(v))
const num = (v: any) => (isNum(v) ? Number(v) : 0)
const fmtInt = (v: any) => (isNum(v) ? nf0.format(Number(v)) : '—')
const fmtMoney = (v: any) => (isNum(v) ? nf2.format(Number(v)) : '—')
const fmtPct = (v: any) => (isNum(v) ? `${nf1.format(Number(v))} %` : '—')
const fmtSignedPct = (v: any) => (isNum(v) ? `${nf1s.format(Number(v))} %` : '—')
const fmtDateTime = (iso: string) =>
  new Date(iso).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })
const signCls = (v: any) => (!isNum(v) || Number(v) === 0 ? '' : Number(v) > 0 ? 'pos' : 'neg')
const pctOf = (a: any, b: any) => (isNum(a) && isNum(b) && Number(b) !== 0 ? (Number(a) / Number(b)) * 100 : null)

/** Сколько калькуляций реально вошло в суммы. При весе по выпуску сервер
 * считает rows_without_volume внутри rows_priced (цена есть, объёма нет) —
 * такие строки в деньги не попадают, и «в расчёте» обязано их вычесть. */
const inMoney = computed(() => {
  const b = base.value
  const priced = num(b.rows_priced)
  return b.weight === 'volume' ? priced - num(b.rows_without_volume) : priced
})

const PRICE_SOURCE_LABELS: Record<string, string> = {
  pending: 'заявки БМ', dwh: 'DWH', audit: 'аудит', calc: 'расчёт', none: 'без цены',
}
const priceSourcesLabel = computed(() => {
  const ps = base.value.price_sources || {}
  const parts = Object.keys(PRICE_SOURCE_LABELS)
    .filter(k => num(ps[k]) > 0)
    .map(k => `${PRICE_SOURCE_LABELS[k]} ${fmtInt(ps[k])}`)
  return parts.length ? parts.join(' · ') : '—'
})

// ── Валюта звена ─────────────────────────────────────────────────────────────
// Колонки валюты появляются, только если хоть у одного звена отгрузка не в BYN:
// у цепочки целиком в рублях они были бы столбцом прочерков.

const isForeign = (l: any) => !!l.currency && l.currency !== 'BYN'
const hasForeign = computed(() => links.value.some(isForeign))
function rateTitle(l: any): string {
  if (!isForeign(l)) return 'BYN — валюта расчёта'
  const r = rate.value
  const asOf = r?.as_of?.[l.currency]
  let s = `1 ${l.currency} = ${nfRate.format(num(l.rate))} BYN`
  if (r?.date) s += ` на ${r.date}`
  if (asOf && asOf !== r?.date) s += `, взят ближайший предыдущий (${asOf})`
  return s
}

// ── Плитки ───────────────────────────────────────────────────────────────────

const tiles = computed(() => {
  const t = total.value, b = base.value
  const last = links.value[links.value.length - 1]
  return [
    { key: 'cost', label: 'Себестоимость Σ, BYN', value: fmtMoney(t.cost_sum), cls: '',
      note: `Σ шести статей по ${fmtInt(inMoney.value)} калькуляциям`,
      noteTitle: 'Пошив + раскрой + декоры + вязание + основные + вспомогательные материалы, как в главной таблице' },
    { key: 'base', label: 'Базовая отпускная Σ, BYN', value: fmtMoney(b.base_sum), cls: '',
      note: 'отпускная «как в главной таблице»',
      noteTitle: 'Отпускная цена по уровню с наложением утверждённых цен' },
    { key: 'final', label: 'Конечная цена Σ, BYN', value: fmtMoney(t.final_sum), cls: '',
      note: last ? `${last.company_name} → внешний рынок` : '', noteTitle: 'Цена продажи последнего звена' },
    { key: 'income', label: 'Доход группы Σ, BYN', value: fmtMoney(t.income_sum), cls: signCls(t.income_sum),
      note: `маржа ${fmtPct(t.margin_pct)} · рентабельность ${fmtPct(t.markup_pct)}`,
      noteTitle: 'маржа = доход / конечная цена; рентабельность = доход / себестоимость' },
  ]
})

/** Сверка тождества доход = конечная − себестоимость = Σ доходов звеньев.
 * Допуск — копейка: суммы считаются на float, но расхождение сверх этого
 * значит ошибку в расчёте, и его надо видеть, а не сглаживать. */
const identity = computed(() => {
  const t = total.value
  const linksSum = links.value.reduce((s, l) => s + num(l.income_sum), 0)
  const byTotal = num(t.final_sum) - num(t.cost_sum)
  const diff = Math.max(Math.abs(num(t.income_sum) - byTotal), Math.abs(num(t.income_sum) - linksSum))
  return { linksSum, diff, ok: diff < 0.01 }
})

// ── Водопад ──────────────────────────────────────────────────────────────────

const waterfall = computed(() => {
  if (!result.value) return null
  const cost = num(total.value.cost_sum), final = num(total.value.final_sum)
  const labels: string[] = ['Себестоимость']
  const data: [number, number][] = [[0, cost]]
  const colors: string[] = [withAlpha(accent.value, 0.45)]
  const values: number[] = [cost]
  let run = cost
  for (const l of links.value) {
    const inc = num(l.income_sum)
    labels.push(l.company_name)
    data.push([Math.min(run, run + inc), Math.max(run, run + inc)])
    colors.push(inc < 0 ? neg.value : pos.value)
    values.push(inc)
    run += inc
  }
  labels.push('Конечная цена')
  data.push([0, final])
  colors.push(accent.value)
  values.push(final)
  return { labels, data, colors, values }
})

const waterfallData = computed(() => {
  const w = waterfall.value
  if (!w) return null
  return {
    labels: w.labels,
    datasets: [{ label: 'BYN', data: w.data, backgroundColor: w.colors, borderWidth: 0, borderSkipped: false }],
  }
})

const waterfallOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        // У плавающего столбца Chart.js показал бы «[start, end]» — показываем
        // саму величину: доход звена или сумму крайних столбцов.
        label: (ctx: any) => {
          const w = waterfall.value
          const v = w ? w.values[ctx.dataIndex] : null
          const isDelta = ctx.dataIndex > 0 && w && ctx.dataIndex < w.values.length - 1
          return ` ${isDelta ? 'доход ' : ''}${fmtMoney(v)} BYN`
        },
      },
    },
  },
  scales: {
    x: { ticks: { color: ink.value, font: { size: 11 } }, grid: { display: false } },
    y: { ticks: { color: ink.value }, grid: { color: grid.value }, title: { display: true, text: 'BYN', color: ink.value } },
  },
}))

// ── Матрица ──────────────────────────────────────────────────────────────────

type Col = {
  key: string; label: string; title?: string
  fmt: (r: any) => string; cls?: (r: any) => string; cellTitle?: (r: any) => string
}
const linkOf = (r: any, seq: number) => (r.links || []).find((x: any) => x.seq === seq)
const matrixColumns = computed<Col[]>(() => {
  const cols: Col[] = [
    { key: 'rows', label: 'Калькуляций', fmt: r => fmtInt(r.rows) },
  ]
  // При весе по выпуску Σ без объёма не читаются — показываем и объём.
  if (base.value.weight === 'volume') {
    cols.push({ key: 'w_sum', label: 'Выпуск, шт', title: 'Σ «выпуск шт» по калькуляциям с ценой и объёмом', fmt: r => fmtInt(r.w_sum) })
  }
  cols.push(
    { key: 'cost', label: 'Σ себестоимость', fmt: r => fmtMoney(r.cost) },
    { key: 'base', label: 'Σ базовая', fmt: r => fmtMoney(r.base) },
    { key: 'final', label: 'Σ конечная', fmt: r => fmtMoney(r.final) },
    { key: 'income', label: 'Доход группы', fmt: r => fmtMoney(r.income), cls: r => signCls(r.income) },
    { key: 'income_pct', label: 'Доход, %', title: 'доход группы / Σ конечная', fmt: r => fmtPct(pctOf(r.income, r.final)) },
  )
  for (const l of links.value) {
    cols.push({
      key: `link_${l.seq}`,
      label: `Доход · ${l.company_name}`,
      title: `доход звена ${l.seq} (${l.company_name}); в подсказке ячейки — его цена выхода Σ`,
      fmt: r => fmtMoney(linkOf(r, l.seq)?.income),
      cls: r => signCls(linkOf(r, l.seq)?.income),
      cellTitle: r => `цена выхода Σ: ${fmtMoney(linkOf(r, l.seq)?.out)}`,
    })
  }
  return cols
})

// ── Excel ────────────────────────────────────────────────────────────────────
/** Как в остальном разделе (reg713.vue): HTML-таблицы под application/vnd.ms-excel
 * с BOM — настоящий .xlsx в разделе не собирается нигде. Выгружаются шапка,
 * звенья и матрица текущего среза (с путём проваливания в заголовке). */
function exportExcel() {
  if (!result.value) return
  const esc = (s: any) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  const xn = (v: any) => (isNum(v) ? Number(v).toFixed(2).replace('.', ',') : '')
  const xp = (v: any) => (isNum(v) ? Number(v).toFixed(1).replace('.', ',') : '')
  const xr = (v: any) => (isNum(v) ? String(Number(v)).replace('.', ',') : '')
  const tr = (cells: any[], tag = 'td') => '<tr>' + cells.map(v => `<${tag}>${esc(v)}</${tag}>`).join('') + '</tr>'
  const c = chain.value || {}, b = base.value, t = total.value, m = meta.value

  const info = [
    ['Цепочка', c.name], ['Режим', m.mode_label], ['Вес', b.weight_label],
    ['Курс НБ РБ на', rate.value?.date || '—'],
    ['Калькуляций', b.rows], ['С ценой', b.rows_priced], ['Без цены', b.rows_without_price], ['Без объёма', b.rows_without_volume],
    ['Себестоимость Σ, BYN', xn(t.cost_sum)], ['Базовая отпускная Σ, BYN', xn(b.base_sum)],
    ['Конечная цена Σ, BYN', xn(t.final_sum)], ['Доход группы Σ, BYN', xn(t.income_sum)],
    ['Маржа, %', xp(t.margin_pct)], ['Рентабельность, %', xp(t.markup_pct)],
  ]
  const linksHead = ['№', 'Компания', '% корр.', '% к базовой', 'Цена входа Σ', 'Цена выхода Σ', 'Доход Σ',
    'Доход, % к входу', 'Доход, % к выходу', 'Доля в доходе группы, %', 'Валюта', 'Курс, BYN за 1',
    'Вход Σ в валюте', 'Выход Σ в валюте', 'Доход Σ в валюте']
  const linksRows = links.value.map(l => tr([
    l.seq, l.company_name, xp(l.adjust_pct), xp(l.eff_pct), xn(l.in_sum), xn(l.out_sum), xn(l.income_sum),
    xp(l.income_pct_in), xp(l.income_pct_out), xp(l.income_share), l.currency || 'BYN', xr(l.rate),
    isForeign(l) ? xn(l.in_sum_cur) : '', isForeign(l) ? xn(l.out_sum_cur) : '', isForeign(l) ? xn(l.income_sum_cur) : '',
  ])).join('')
  const control = tr(['Контроль', 'доход группы = конечная − себестоимость', '', '', xn(t.cost_sum), xn(t.final_sum), xn(t.income_sum),
    '', '', 'Σ доходов звеньев', xn(identity.value.linksSum)])

  const cols = matrixColumns.value
  const matrixHead = [m.label, 'Наименование', ...cols.map(x => x.label)]
  const matrixRows = matrix.value.map(r => tr([
    r.label || '(не указано)', r.name || '',
    ...cols.map(x => {
      if (x.key === 'rows' || x.key === 'w_sum') return isNum(r[x.key]) ? String(r[x.key]) : ''
      if (x.key === 'income_pct') return xp(pctOf(r.income, r.final))
      if (x.key.startsWith('link_')) return xn(linkOf(r, Number(x.key.slice(5)))?.income)
      return xn(r[x.key])
    }),
  ])).join('')
  const crumbs = ['Все', ...(m.path || []).map((p: any) => p.value || '(не указано)')].join(' › ')

  const html = `<html><head><meta charset="utf-8"></head><body>`
    + `<h3>Финрез · ${esc(c.name)}</h3><table border="1">${info.map(r => tr(r)).join('')}</table><br>`
    + `<h3>Звенья</h3><table border="1">${tr(linksHead, 'th')}${linksRows}${control}</table><br>`
    + `<h3>Матрица · ${esc(m.label)} · ${esc(crumbs)}</h3><table border="1">${tr(matrixHead, 'th')}${matrixRows}</table>`
    + `</body></html>`
  const blob = new Blob(['﻿' + html], { type: 'application/vnd.ms-excel' })
  const url = URL.createObjectURL(blob)
  const slug = String(c.name || props.chainId).trim().replace(/[\\/:*?"<>|]+/g, '_').replace(/\s+/g, '_').slice(0, 60) || String(props.chainId)
  const a = document.createElement('a')
  a.href = url
  a.download = `finrez-${slug}-${new Date().toISOString().slice(0, 10)}.xls`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
</script>

<style scoped>
/* Только токены дизайн-системы; тёмная тема — через них. */
.chain-result {
  border: 1px solid var(--border); border-radius: var(--rd-3);
  background: var(--bg-surface); padding: var(--sp-4);
  display: flex; flex-direction: column; gap: var(--sp-4); min-width: 0;
}

.result-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-3); flex-wrap: wrap; }
.head-text { min-width: 0; }
.result-title { font-size: var(--fs-lg); font-weight: var(--fw-semibold); margin: 0; }
.result-sub { margin: var(--sp-1) 0 0; font-size: var(--fs-xs); color: var(--text-muted); }
.dot { margin: 0 var(--sp-2); opacity: .5; }
.warn { color: var(--neg); }
.muted { color: var(--text-muted); font-size: var(--fs-2xs); }
.sources { font-size: var(--fs-2xs); }
.head-actions { display: flex; gap: var(--sp-2); align-items: center; flex-wrap: wrap; }
.rate-date { display: flex; gap: var(--sp-2); align-items: center; font-size: var(--fs-2xs); color: var(--text-muted); }
.rate-date .form-input { width: auto; font-size: var(--fs-xs); }
.hint { font-size: var(--fs-2xs); color: var(--text-muted); font-style: italic; }
.error { color: var(--neg); font-size: var(--fs-sm); margin: 0; }
.loading { color: var(--text-muted); font-size: var(--fs-sm); margin: 0; }

/* Пока идёт перезапрос (проваливание, дата курса), прежние цифры остаются на
   экране приглушёнными — таблица не прыгает на пустое состояние. */
.dim { opacity: .55; transition: opacity var(--t-fast); pointer-events: none; }

.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: var(--sp-3); }
.tile {
  border: 1px solid var(--border); border-radius: var(--rd-3);
  background: var(--bg-surface-2); padding: var(--sp-3) var(--sp-4);
  display: flex; flex-direction: column; gap: var(--sp-1); min-width: 0;
}
.tile-label { font-size: var(--fs-2xs); color: var(--text-muted); text-transform: uppercase; letter-spacing: .04em; }
.tile-value { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: var(--fs-2xl); font-weight: var(--fw-semibold); }
.tile-note { font-size: var(--fs-2xs); color: var(--text-muted); }
.pos { color: var(--pos); }
.neg { color: var(--neg); }

.block { display: flex; flex-direction: column; gap: var(--sp-2); min-width: 0; }
.block-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-3); flex-wrap: wrap; }
.block-title { font-size: var(--fs-md); font-weight: var(--fw-medium); margin: 0; }
.block-note { font-size: var(--fs-2xs); color: var(--text-muted); margin: 0; }

.control { margin: 0; font-size: var(--fs-2xs); font-family: var(--font-mono); font-variant-numeric: tabular-nums; color: var(--text-muted); }
.control.ok { color: var(--pos); }
.control.warn { color: var(--neg); }

.crumbs { display: flex; align-items: center; gap: var(--sp-1); flex-wrap: wrap; margin-top: var(--sp-1); }
.crumb {
  border: none; background: transparent; padding: 0 2px; cursor: pointer;
  font-size: var(--fs-2xs); color: var(--accent); max-width: 200px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.crumb:disabled { color: var(--text-muted); cursor: default; }
.crumb-sep { font-size: var(--fs-2xs); color: var(--text-muted); }

/* Высота графика задаётся здесь и только здесь: Chart.js с maintainAspectRatio:
   false тянется на родителя, без явной высоты канвас растёт бесконечно. */
.chart-box { position: relative; height: 260px; min-width: 0; }
.chart-box :deep(canvas) { max-height: 100%; }

/* Таблицы: hairline-сетка токеном, числа моно, без внешней рамки — уже в карточке. */
.table-wrap { overflow-x: auto; }
.grid { width: 100%; border-collapse: collapse; font-size: var(--fs-xs); }
.grid th, .grid td { padding: var(--sp-1) var(--sp-2); border-bottom: 1px solid var(--border); white-space: nowrap; }
.grid th { text-align: right; font-weight: var(--fw-medium); color: var(--text-muted); font-size: var(--fs-2xs); }
.grid th.col-label, .grid td.col-label { text-align: left; }
.grid th.col-num, .grid td.col-num { width: 2.5em; text-align: right; }
.grid td.num { text-align: right; font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.grid tbody tr.drillable { cursor: pointer; }
.grid tbody tr.drillable:hover { background: var(--bg-surface-2); }
.row-label { display: block; }
.row-name { display: block; font-size: var(--fs-2xs); color: var(--text-muted); white-space: normal; max-width: 320px; }
.empty-row { text-align: center; color: var(--text-muted); padding: var(--sp-4) !important; }
</style>
