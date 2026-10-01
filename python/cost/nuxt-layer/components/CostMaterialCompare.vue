<template>
  <h2 class="section-title">Материалы: ФКСС против ПФКСС</h2>
  <section class="card">
    <div class="card-head">
      <div>
        <p class="card-note">
          Строка нижнего уровня — <strong>задание ФКСС</strong> выборки дашборда. К нему подобрана ПФКСС
          той же модели, артикула и плана: с тем же номером задания, а если такой нет — подставлена
          ПФКСС артикула (<span class="match-tag">≈</span>; при нескольких — самая дорогая, как при
          простановке цены). Суммы материалов на <strong>единицу</strong> в BYN, сложены по строкам
          калькуляции; на уровнях бренд-менеджера и артикула — средневзвешенные по выпуску ФКСС.
          Отклонение — «ФКСС / ПФКСС − 1», выше нуля — материалы в факте дороже.
        </p>
        <p v-if="coverage" class="card-note">
          ПФКСС есть у <strong>{{ fmtInt(coverage.pair_tasks) }}</strong> из
          {{ fmtInt(coverage.fk_tasks) }} заданий ФКСС выборки
          ({{ fmtPct1(share(coverage.pair_vol, coverage.fk_vol)) }} выпуска):
          с тем же заданием — {{ fmtInt(coverage.exact) }}, подставлено — {{ fmtInt(coverage.fallback) }}.
          Предварительный расчёт ведут не для всего ассортимента — остальные задания сравнивать не с чем.
          <span v-if="suspectCount" class="warn">
            {{ suspectCount }} {{ plural(suspectCount, 'задание', 'задания', 'заданий') }} с расхождением
            материалов больше чем в {{ suspectRatio }} раз — помечены ⚠ и в итоги групп не входят.</span>
          <span v-if="truncated" class="warn">Список обрезан до {{ fmtInt(rowLimit) }} заданий — сузьте фильтры.</span>
        </p>
      </div>
      <div class="mc-actions">
        <label class="mc-check" title="Скрыть задания, к которым ПФКСС подставлена с другого задания артикула">
          <input v-model="exactOnly" type="checkbox" /> только с тем же заданием
        </label>
        <!-- Пока дашборд перегружается после смены фильтров, строки блока ещё от
             прошлой выборки — выгрузка дала бы старые строки под новой шапкой. -->
        <button class="btn btn-ghost btn-sm" :disabled="busy || stale || !visibleRows.length"
                :title="stale ? 'Данные обновляются под новые фильтры — подождите'
                              : 'Выгрузить все задания выборки (не только показанный уровень) в Excel'"
                @click="exportExcel">
          <Icon name="lucide:download" /> Excel
        </button>
      </div>
    </div>

    <nav class="crumbs">
      <button class="crumb" :disabled="!path.length" @click="drillTo(0)">Все бренд-менеджеры</button>
      <template v-for="(c, i) in crumbLabels" :key="i">
        <span class="crumb-sep">›</span>
        <button class="crumb" :disabled="i === path.length - 1" :title="c" @click="drillTo(i + 1)">{{ c }}</button>
      </template>
    </nav>

    <p v-if="error" class="error">{{ error }}</p>

    <div class="table-wrap">
      <table class="matrix">
        <thead>
          <tr>
            <th rowspan="2" class="col-label sortable" @click="sortBy('label')">
              {{ levelLabel }}<span v-if="sort.key === 'label'">{{ sort.asc ? ' ▲' : ' ▼' }}</span>
            </th>
            <th rowspan="2" class="sortable" @click="sortBy('vol')">
              Выпуск, шт<span v-if="sort.key === 'vol'">{{ sort.asc ? ' ▲' : ' ▼' }}</span>
            </th>
            <th colspan="3" class="grp">Основные материалы</th>
            <th colspan="3" class="grp">Вспомогательные материалы</th>
            <th colspan="4" class="grp">Материалы итого</th>
            <th rowspan="2" class="sortable" title="Разница материалов × выпуск ФКСС — во что расхождение обошлось в деньгах"
                @click="sortBy('d_mat_vol')">
              Откл. на выпуск, BYN<span v-if="sort.key === 'd_mat_vol'">{{ sort.asc ? ' ▲' : ' ▼' }}</span>
            </th>
          </tr>
          <tr>
            <th v-for="c in valueCols" :key="c.key" class="sortable" @click="sortBy(c.key)">
              {{ c.label }}<span v-if="sort.key === c.key">{{ sort.asc ? ' ▲' : ' ▼' }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="(busy || !loadedOnce) && !levelRows.length && !error">
            <td :colspan="totalCols" class="empty-row">Загрузка…</td>
          </tr>
          <template v-for="row in levelRows" :key="row.key">
            <tr :class="{ drillable: true, suspect: row.suspect, open: openKey === row.key }"
                @click="onRowClick(row)">
              <td class="col-label">
                <span class="row-label">
                  <span v-if="row.suspect" class="warn"
                        :title="level === 2 ? `Расхождение материалов больше чем в ${suspectRatio} раз — в итоги не входит`
                                            : `Все задания группы с расхождением больше чем в ${suspectRatio} раз — раскройте, чтобы увидеть`">⚠ </span>
                  <span v-if="level === 2" class="caret">{{ openKey === row.key ? '▾' : '▸' }}</span>
                  {{ row.label }}
                </span>
                <span v-if="row.sub" class="row-name" :title="row.subTitle">{{ row.sub }}</span>
              </td>
              <td class="num" :title="volTitle(row)">{{ fmtInt(row.vol) }}</td>
              <td v-for="c in valueCols" :key="c.key" class="num" :class="c.cls?.(row)">{{ c.fmt(row) }}</td>
              <td class="num" :class="devCls(row.d_mat_vol)">{{ fmtSigned2(row.d_mat_vol) }}</td>
            </tr>
            <tr v-if="level === 2 && openKey === row.key" class="lines-row">
              <td :colspan="totalCols">
                <div v-if="linesState.loading" class="muted">Загрузка материалов…</div>
                <p v-else-if="linesState.error" class="error">{{ linesState.error }}</p>
                <template v-else-if="lines">
                  <p class="card-note">
                    ФКСС задания {{ row.task.zadanie || '—' }} от {{ fmtDate(lines.pair?.calc_date) }} против
                    ПФКСС задания {{ lines.pair?.pf_zadanie || '—' }} от {{ fmtDate(lines.pair?.pf_calc_date) }}<template
                      v-if="lines.pair?.match === 'fallback'"> (подставлена<template
                      v-if="(lines.pair?.pf_tasks || 0) > 1">, самая дорогая из {{ lines.pair.pf_tasks }}</template>)</template>.
                    Материалы сопоставлены по наименованию и артикулу материала; цена — сумма / норма
                    (колонка цены источника у ФКСС Узбекистана в сумах).
                    <span v-if="lines.rows.some((m: any) => m.dual)" class="warn">
                      ◐ — в источнике у строки заполнены обе статьи, «Основные» и «Вспомогательные»: суммы
                      разнесены по статьям, как в строке списка.</span>
                  </p>
                  <table class="matrix lines">
                    <thead>
                      <tr>
                        <th class="col-label">Материал</th>
                        <th class="col-label">Свойство ПФКСС</th>
                        <th class="col-label">Свойство ФКСС</th>
                        <th>Норма ПФКСС</th>
                        <th>Норма ФКСС</th>
                        <th>Цена ПФКСС</th>
                        <th>Цена ФКСС</th>
                        <th>Сумма ПФКСС</th>
                        <th>Сумма ФКСС</th>
                        <th>Откл., BYN</th>
                        <th>Откл., %</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="(m, mi) in lines.rows" :key="mi">
                        <td class="col-label">
                          <span class="kind">{{ m.kind === 'osn' ? 'осн' : 'всп' }}</span>
                          {{ m.name || '—' }}<span v-if="m.art" class="muted"> · {{ m.art }}</span>
                          <span v-if="m.dual" class="warn" title="В источнике у строки заполнены обе статьи — «Основные» и «Вспомогательные»"> ◐</span>
                          <span v-if="m.status === 'fk_only'" class="badge">только в ФКСС</span>
                          <span v-else-if="m.status === 'pf_only'" class="badge">только в ПФКСС</span>
                          <span v-else-if="m.status === 'split'" class="badge badge-split"
                                :title="`Материал есть на обоих этапах, но в ${m.fk_sum === null ? 'ФКСС' : 'ПФКСС'} — только в другой статье`">в {{ m.fk_sum === null ? 'ФКСС' : 'ПФКСС' }} — в другой статье</span>
                        </td>
                        <td class="col-label muted">{{ m.pf_props || '—' }}</td>
                        <td class="col-label" :class="{ changed: m.pf_props !== m.fk_props && m.status === 'both' }">{{ m.fk_props || '—' }}</td>
                        <td class="num">{{ fmtNorm(m.pf_norm) }}</td>
                        <td class="num" :class="{ changed: differs(m.fk_norm, m.pf_norm) }">{{ fmtNorm(m.fk_norm) }}</td>
                        <td class="num">{{ fmt4(m.pf_price) }}</td>
                        <td class="num" :class="{ changed: differs(m.fk_price, m.pf_price) }">{{ fmt4(m.fk_price) }}</td>
                        <td class="num">{{ fmt4(m.pf_sum) }}</td>
                        <td class="num">{{ fmt4(m.fk_sum) }}</td>
                        <td class="num" :class="devCls(m.d_sum)">{{ fmtSigned4(m.d_sum) }}</td>
                        <td class="num" :class="devCls(m.d_sum_pct)">{{ fmtSignedPct(m.d_sum_pct) }}</td>
                      </tr>
                      <tr v-if="!lines.rows.length">
                        <td colspan="11" class="empty-row">Строк материалов нет ни на одном этапе</td>
                      </tr>
                    </tbody>
                    <!-- Итоги раскрытия — те же суммы, что в строке списка: расхождение
                         здесь значило бы ошибку разноса. -->
                    <tfoot v-if="lines.totals && lines.rows.length">
                      <tr v-for="t in linesTotals(lines.totals)" :key="t.kind">
                        <td class="col-label" colspan="7">Итого {{ t.label }}</td>
                        <td class="num">{{ fmt4(t.pf) }}</td>
                        <td class="num">{{ fmt4(t.fk) }}</td>
                        <td class="num" :class="devCls(t.fk - t.pf)">{{ fmtSigned4(t.fk - t.pf) }}</td>
                        <td class="num" :class="devCls(rel(t.fk, t.pf))">{{ fmtSignedPct(rel(t.fk, t.pf)) }}</td>
                      </tr>
                    </tfoot>
                  </table>
                </template>
              </td>
            </tr>
          </template>
          <tr v-if="!busy && loadedOnce && !error && !levelRows.length">
            <td :colspan="totalCols" class="empty-row">
              {{ exactOnly && rows.length ? 'Нет заданий с тем же номером у ПФКСС — снимите галочку'
                                          : 'Ни у одного задания ФКСС выборки нет ПФКСС' }}
            </td>
          </tr>
        </tbody>
        <tfoot v-if="levelRows.length > 1">
          <tr class="total-row">
            <td class="col-label">Итого{{ totalAgg.suspect_n ? ' (без ⚠)' : '' }}</td>
            <td class="num" :title="volTitle(totalAgg)">{{ fmtInt(totalAgg.vol) }}</td>
            <td v-for="c in valueCols" :key="c.key" class="num" :class="c.cls?.(totalAgg)">{{ c.fmt(totalAgg) }}</td>
            <td class="num" :class="devCls(totalAgg.d_mat_vol)">{{ fmtSigned2(totalAgg.d_mat_vol) }}</td>
          </tr>
        </tfoot>
      </table>
    </div>
  </section>
</template>

<script setup lang="ts">
/**
 * Материалы ФКСС против ПФКСС — блок дашборда «Маржа выпуска» (задача Б24
 * 661229). Сервер (GET /margin/materials, app/material_compare.py) отдаёт пары
 * «задание ФКСС ↔ ПФКСС» выборки; матрица бренд-менеджер → артикул → задание и
 * выгрузка в Excel собираются здесь из тех же строк — пар сотни, второй раз на
 * сервер ходить незачем. Раскрытие задания до материалов — отдельный запрос
 * (/margin/materials/lines), по клику.
 */
import { computed, onMounted, reactive, ref, watch } from 'vue'

const props = defineProps<{
  /** Выбранные значения фильтров дашборда (reactive из margin.vue). */
  filters: Record<string, string[]>
  /** Ключи и подписи фильтров — для запроса и шапки Excel. */
  filterConfig: { key: string; label: string }[]
  /** Идёт загрузка дашборда: запрос блока ждёт её конца, чтобы взять год по
   * умолчанию, который дашборд подставляет при первой загрузке. */
  loading: boolean
}>()

const config = useRuntimeConfig()
const apiBase = computed(() => (config.public.costOnly ? '' : ((config.public.apiBase as string) || '')))
const { user } = useAuth()
const fetchHeaders = computed(() => {
  const email = user.value?.email || ''
  return email ? { 'X-Cost-User': email } : {}
})

const rows = ref<any[]>([])
const coverage = ref<Record<string, number> | null>(null)
const truncated = ref(false)
const rowLimit = ref(0)
const suspectRatio = ref(20)
const refreshedAt = ref<string | null>(null)
const busy = ref(false)
const error = ref('')
const exactOnly = ref(false)

const query = computed(() => {
  const p = new URLSearchParams()
  for (const f of props.filterConfig) for (const v of props.filters[f.key] || []) p.append(f.key, v)
  return p.toString()
})

// Без immediate: при монтировании дашборд ещё не загружен и год по умолчанию не
// подставлен — запрос «за все годы» был бы лишним. Первый запрос уходит, когда
// загрузка дашборда закончилась; дальше — при смене фильтров.
/** Под какие фильтры загружены строки (null — не загружены или запрос упал). */
const loadedQuery = ref<string | null>(null)
/** Фильтры на момент загрузки — для шапки Excel: шапка обязана описывать те
 * строки, что в файле, а не фильтры, которые уже поменяли. */
const loadedFilters = ref<Record<string, string[]>>({})
/** Хотя бы одна загрузка завершилась — до неё «нет ПФКСС» было бы ложью. */
const loadedOnce = ref(false)
/** Строки не соответствуют текущим фильтрам: дашборд перегружается или запрос
 * блока ещё не ушёл / упал. */
const stale = computed(() => props.loading || loadedQuery.value !== query.value)
let seq = 0
watch(() => [query.value, props.loading] as const, ([q, l]) => {
  if (!l && q !== loadedQuery.value) load(q)
})
// Блок смонтирован, когда дашборд уже загружен (горячая перезагрузка, условный
// показ): загрузка дашборда больше не переключится, и watch не сработает.
// Признак, что дашборд загружался, — подставленные фильтры (хотя бы год).
onMounted(() => {
  if (!props.loading && query.value) load(query.value)
})

async function load(q: string) {
  loadedQuery.value = q
  const filtersAtStart = Object.fromEntries(
    props.filterConfig.map(f => [f.key, [...(props.filters[f.key] || [])]]))
  const my = ++seq
  busy.value = true
  error.value = ''
  try {
    const res = await $fetch<any>(`${apiBase.value}/api/cost/margin/materials${q ? '?' + q : ''}`,
                                  { headers: fetchHeaders.value })
    if (my !== seq) return
    rows.value = res.rows || []
    coverage.value = res.coverage || null
    truncated.value = !!res.truncated
    rowLimit.value = res.row_limit || 0
    suspectRatio.value = res.suspect_ratio || 20
    refreshedAt.value = res.cache_refreshed_at || null
    loadedFilters.value = filtersAtStart
    // Путь мог перестать существовать в новой выборке — обрезаем до уровня,
    // который в ней есть: иначе висела бы пустая таблица со старыми крошками.
    if (path.value.length && !rows.value.some(r => r.brand_manager === path.value[0])) {
      path.value = []
    } else if (path.value.length > 1
               && !rows.value.some(r => r.brand_manager === path.value[0] && articulKey(r) === path.value[1])) {
      path.value = path.value.slice(0, 1)
    }
    openKey.value = ''
  } catch (e: any) {
    if (my !== seq) return
    console.error('[cost] material compare failed', e)
    error.value = e?.data?.detail || e?.message || 'Не удалось загрузить сравнение материалов'
    loadedQuery.value = null
    // Строки прошлой выборки под чипами новых фильтров читались бы как ответ
    // на новые — убираем их, остаётся только ошибка.
    rows.value = []
    coverage.value = null
    truncated.value = false
    path.value = []
    openKey.value = ''
  } finally {
    if (my === seq) {
      busy.value = false
      loadedOnce.value = true
    }
  }
}

// ── Матрица ──────────────────────────────────────────────────────────────────

const visibleRows = computed(() => (exactOnly.value ? rows.value.filter(r => r.match === 'exact') : rows.value))
const suspectCount = computed(() => visibleRows.value.filter(r => r.suspect).length)

/** Путь проваливания: [бренд-менеджер] или [бренд-менеджер, «модель|артикул»]. */
const path = ref<string[]>([])
const level = computed(() => path.value.length)
const levelLabel = computed(() => ['Бренд-менеджер', 'Артикул', '№ задания ФКСС'][level.value])
const articulKey = (r: any) => `${r.model}|${r.articul}`
const crumbLabels = computed(() => path.value.map((v, i) => (i === 1 ? v.split('|')[1] : v) || '(не указан)'))

function drillTo(n: number) {
  path.value = path.value.slice(0, n)
  openKey.value = ''
}

const VALUES = ['fk_osn', 'pf_osn', 'fk_vsp', 'pf_vsp', 'fk_mat', 'pf_mat'] as const

/** Свод группы: себестоимость единицы средневзвешенно по выпуску ФКСС, без
 * несопоставимых пар (suspect) — одна такая пара перекашивает всю группу. */
function agg(list: any[]): any {
  const ok = list.filter(r => !r.suspect)
  const vol = ok.reduce((s, r) => s + (r.vol || 0), 0)
  const volAll = list.reduce((s, r) => s + (r.vol || 0), 0)
  const allSuspect = list.length > 0 && ok.length === 0
  const out: any = {
    tasks: list.length,
    suspect_n: list.length - ok.length,
    // Флаг — только когда несопоставимы ВСЕ задания группы: тогда сравнивать
    // её не по чему, и она должна быть заметна (⚠), а не выглядеть нулём.
    suspect: allSuspect,
    // Выпуск — тот же, по которому взвешены суммы (без ⚠), иначе «Итого (без ⚠)»
    // противоречило бы само себе. У группы из одних ⚠ — весь её выпуск.
    vol: allSuspect ? volAll : vol,
    vol_all: volAll,
    d_mat_vol: ok.length ? ok.reduce((s, r) => s + (r.d_mat_vol || 0), 0) : null,
  }
  for (const k of VALUES) out[k] = vol ? ok.reduce((s, r) => s + (r.vol || 0) * (r[k] || 0), 0) / vol : null
  for (const k of ['osn', 'vsp', 'mat']) {
    const fk = out[`fk_${k}`], pf = out[`pf_${k}`]
    out[`d_${k}`] = fk === null || pf === null ? null : fk - pf
    out[`d_${k}_pct`] = fk === null || !pf ? null : 100 * (fk / pf - 1)
  }
  return out
}

function groupBy(list: any[], key: (r: any) => string): Map<string, any[]> {
  const m = new Map<string, any[]>()
  for (const r of list) {
    const k = key(r)
    if (!m.has(k)) m.set(k, [])
    m.get(k)!.push(r)
  }
  return m
}

const levelRows = computed<any[]>(() => {
  let list = visibleRows.value
  if (level.value >= 1) list = list.filter(r => r.brand_manager === path.value[0])
  if (level.value >= 2) list = list.filter(r => articulKey(r) === path.value[1])
  let out: any[]
  const tasksNote = (a: any) => `${a.tasks} ${plural(a.tasks, 'задание', 'задания', 'заданий')}`
    + (a.suspect_n ? ` · ⚠ ${a.suspect_n} не в итоге` : '')
  if (level.value === 0) {
    out = [...groupBy(list, r => r.brand_manager)].map(([bm, g]) => {
      const a = agg(g)
      return { ...a, key: bm, label: bm || '(не указан)', sub: tasksNote(a) }
    })
  } else if (level.value === 1) {
    out = [...groupBy(list, articulKey)].map(([k, g]) => {
      const a = agg(g)
      return { ...a, key: k, label: g[0].articul, sub: `${g[0].name || ''} · модель ${g[0].model} · ${tasksNote(a)}` }
    })
  } else {
    out = list.map(r => ({
      ...r,
      key: `${r.plan_id}|${r.zadanie}|${r.calc_date}`,
      label: r.zadanie || '(без задания)',
      sub: r.match === 'exact'
        ? `план ${r.plan_id} · ПФКСС того же задания`
        : `план ${r.plan_id} · ≈ ПФКСС задания ${r.pf_zadanie || '—'}${r.pf_tasks > 1 ? ` (самая дорогая из ${r.pf_tasks})` : ''}`,
      subTitle: `ФКСС от ${fmtDate(r.calc_date)}, ПФКСС от ${fmtDate(r.pf_calc_date)}`,
      task: r,
    }))
  }
  return sortRows(out)
})

const totalAgg = computed(() => {
  let list = visibleRows.value
  if (level.value >= 1) list = list.filter(r => r.brand_manager === path.value[0])
  if (level.value >= 2) list = list.filter(r => articulKey(r) === path.value[1])
  return agg(list)
})

/** Сортировка по умолчанию — по модулю отклонения на выпуск: список читают как
 * «где расхождение стоило больше всего денег». */
const sort = reactive<{ key: string; asc: boolean }>({ key: 'd_mat_vol', asc: false })
function sortBy(key: string) {
  if (sort.key === key) sort.asc = !sort.asc
  else { sort.key = key; sort.asc = key === 'label' }
}
function sortRows(list: any[]): any[] {
  const dir = sort.asc ? 1 : -1
  const val = (r: any) => (sort.key === 'd_mat_vol' ? Math.abs(r.d_mat_vol || 0) : r[sort.key])
  return [...list].sort((a, b) => {
    const x = val(a), y = val(b)
    if (sort.key === 'label') return dir * String(x).localeCompare(String(y), 'ru')
    if (!isNum(x) && !isNum(y)) return 0
    if (!isNum(x)) return 1
    if (!isNum(y)) return -1
    return dir * (Number(x) - Number(y))
  })
}

function onRowClick(row: any) {
  if (level.value === 0) path.value = [row.key]
  else if (level.value === 1) path.value = [path.value[0], row.key]
  else toggleLines(row)
}

// ── Материалы задания ────────────────────────────────────────────────────────

const openKey = ref('')
/** Состояние раскрытия ОДНОЙ строки — той, что открыта (key). Ответ, ошибка и
 * «загрузка» чужого запроса сюда не попадают: открыли другую строку, пока шёл
 * запрос, — его результат только ложится в кэш. */
const linesState = reactive<{ key: string; loading: boolean; error: string; data: any }>(
  { key: '', loading: false, error: '', data: null })
const lines = computed(() => (linesState.key === openKey.value ? linesState.data : null))
const linesCache = new Map<string, any>()
let linesSeq = 0

async function toggleLines(row: any) {
  if (openKey.value === row.key) { openKey.value = ''; return }
  openKey.value = row.key
  const t = row.task
  // Дата расчёта — часть ключа: у задания бывает несколько дат, и строка списка —
  // это задание НА ДАТУ.
  const ck = `${loadedQuery.value}|${t.model}|${t.articul}|${t.plan_id}|${t.zadanie}|${t.calc_date}`
  const my = ++linesSeq
  Object.assign(linesState, { key: row.key, error: '', data: linesCache.get(ck) ?? null,
                              loading: !linesCache.has(ck) })
  if (linesCache.has(ck)) return
  try {
    const p = new URLSearchParams({ model: t.model, articul: t.articul, plan_id: t.plan_id,
                                    zadanie: t.zadanie, calc_date: t.calc_date || '' })
    const res = await $fetch<any>(`${apiBase.value}/api/cost/margin/materials/lines?${p}`,
                                  { headers: fetchHeaders.value })
    linesCache.set(ck, res)
    if (my === linesSeq) Object.assign(linesState, { data: res, loading: false })
  } catch (e: any) {
    console.error('[cost] material lines failed', e)
    if (my === linesSeq) {
      Object.assign(linesState, { error: e?.data?.detail || e?.message || 'Не удалось загрузить материалы',
                                  loading: false })
    }
  }
}

/** Подсказка к выпуску группы, из которой исключены ⚠-задания. */
const volTitle = (r: any) => (r?.suspect_n && !r.suspect
  ? `Без заданий с ⚠; всего с ними — ${fmtInt(r.vol_all)} шт` : '')

/** Строки «Итого» раскрытия: основные, вспомогательные, все материалы. */
function linesTotals(t: Record<string, number>) {
  const n = (v: any) => Number(v || 0)
  return [
    { kind: 'osn', label: 'основные', fk: n(t.fk_osn), pf: n(t.pf_osn) },
    { kind: 'vsp', label: 'вспомогательные', fk: n(t.fk_vsp), pf: n(t.pf_vsp) },
    { kind: 'mat', label: 'материалы', fk: n(t.fk_osn) + n(t.fk_vsp), pf: n(t.pf_osn) + n(t.pf_vsp) },
  ]
}
const rel = (fk: number, pf: number) => (pf ? 100 * (fk / pf - 1) : null)

// ── Колонки и форматирование ─────────────────────────────────────────────────

const nf0 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 0 })
const nf1 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 1, maximumFractionDigits: 1 })
const nf2 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const nf4 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 4, maximumFractionDigits: 4 })
const nfNorm = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 6 })
const isNum = (v: any) => v !== null && v !== undefined && Number.isFinite(Number(v))
const fmtInt = (v: any) => (isNum(v) ? nf0.format(Number(v)) : '—')
const fmt4 = (v: any) => (isNum(v) ? nf4.format(Number(v)) : '—')
const fmtNorm = (v: any) => (isNum(v) ? nfNorm.format(Number(v)) : '—')
const fmtPct1 = (v: any) => (isNum(v) ? `${nf1.format(Number(v))}%` : '—')
const sign = (v: number) => (v > 0 ? '+' : v < 0 ? '−' : '')
const fmtSigned4 = (v: any) => (isNum(v) ? sign(Number(v)) + nf4.format(Math.abs(Number(v))) : '—')
const fmtSigned2 = (v: any) => (isNum(v) ? sign(Number(v)) + nf2.format(Math.abs(Number(v))) : '—')
const fmtSignedPct = (v: any) => (isNum(v) ? `${sign(Number(v))}${nf1.format(Math.abs(Number(v)))}%` : '—')
const fmtDate = (v: any) => (v ? String(v).slice(0, 10).split('-').reverse().join('.') : '—')
/** Отклонение выше нуля — материалы в факте дороже, это плохо. */
const devCls = (v: any) => (!isNum(v) || Math.abs(Number(v)) < 1e-9 ? '' : Number(v) > 0 ? 'neg' : 'pos')
/** Подсветка изменившейся нормы или цены. Допуск 0,1%: цена здесь — сумма /
 * норма, и округление сумм в источнике даёт 17,5762 против 17,5761. */
const differs = (a: any, b: any) =>
  isNum(a) && isNum(b) && Math.abs(Number(a) - Number(b)) > 1e-3 * Math.max(Math.abs(Number(a)), Math.abs(Number(b)))
const share = (a: any, b: any) => (isNum(a) && Number(b) ? (100 * Number(a)) / Number(b) : null)
function plural(n: number, one: string, few: string, many: string) {
  const m10 = n % 10, m100 = n % 100
  if (m10 === 1 && m100 !== 11) return one
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few
  return many
}

type Col = { key: string; label: string; fmt: (r: any) => string; cls?: (r: any) => string }
const valueCols: Col[] = [
  { key: 'pf_osn', label: 'ПФКСС', fmt: r => fmt4(r.pf_osn) },
  { key: 'fk_osn', label: 'ФКСС', fmt: r => fmt4(r.fk_osn) },
  { key: 'd_osn_pct', label: 'Откл., %', fmt: r => fmtSignedPct(r.d_osn_pct), cls: r => devCls(r.d_osn_pct) },
  { key: 'pf_vsp', label: 'ПФКСС', fmt: r => fmt4(r.pf_vsp) },
  { key: 'fk_vsp', label: 'ФКСС', fmt: r => fmt4(r.fk_vsp) },
  { key: 'd_vsp_pct', label: 'Откл., %', fmt: r => fmtSignedPct(r.d_vsp_pct), cls: r => devCls(r.d_vsp_pct) },
  { key: 'pf_mat', label: 'ПФКСС', fmt: r => fmt4(r.pf_mat) },
  { key: 'fk_mat', label: 'ФКСС', fmt: r => fmt4(r.fk_mat) },
  { key: 'd_mat', label: 'Откл., BYN', fmt: r => fmtSigned4(r.d_mat), cls: r => devCls(r.d_mat) },
  { key: 'd_mat_pct', label: 'Откл., %', fmt: r => fmtSignedPct(r.d_mat_pct), cls: r => devCls(r.d_mat_pct) },
]
const totalCols = 3 + valueCols.length

// ── Excel ────────────────────────────────────────────────────────────────────
// Как в остальном разделе: HTML-таблица под application/vnd.ms-excel с BOM.
// Выгружаются ВСЕ задания выборки плоским списком (с учётом галочки «только с
// тем же заданием»), а не показанный уровень матрицы: в Excel их сводят и
// фильтруют сами. Числа — без разделителя тысяч, с запятой, чтобы Excel принял
// их за числа.

const MONTHS = ['январь', 'февраль', 'март', 'апрель', 'май', 'июнь',
                'июль', 'август', 'сентябрь', 'октябрь', 'ноябрь', 'декабрь']

function exportExcel() {
  const esc = (v: any) => String(v ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  const xnum = (v: any, d: number) => (isNum(v) ? Number(v).toFixed(d).replace('.', ',') : '')
  const txt = (v: any) => `<td style="mso-number-format:'\\@'">${esc(v)}</td>`
  const num = (v: any, d: number) => `<td>${xnum(v, d)}</td>`
  const head = ['Бренд-менеджер', 'Модель', 'Артикул', 'Наименование', '№ плана', 'Задание ФКСС',
    'Задание ПФКСС', 'Сопоставление', 'Дата расчёта ФКСС', 'Дата расчёта ПФКСС', 'Выпуск ФКСС, шт',
    'Основные ПФКСС', 'Основные ФКСС', 'Основные откл., BYN', 'Основные откл., %',
    'Вспомогательные ПФКСС', 'Вспомогательные ФКСС', 'Вспомогательные откл., BYN', 'Вспомогательные откл., %',
    'Материалы ПФКСС', 'Материалы ФКСС', 'Материалы откл., BYN', 'Материалы откл., %',
    'Откл. на выпуск, BYN', 'Несопоставимо']
  const list = [...visibleRows.value].sort((a, b) =>
    a.brand_manager.localeCompare(b.brand_manager, 'ru') || a.articul.localeCompare(b.articul, 'ru')
    || a.zadanie.localeCompare(b.zadanie, 'ru'))
  const body = list.map(r => '<tr>' + [
    txt(r.brand_manager), txt(r.model), txt(r.articul), txt(r.name), txt(r.plan_id), txt(r.zadanie),
    txt(r.pf_zadanie),
    txt(r.match === 'exact' ? 'то же задание' : (r.pf_tasks > 1 ? `подставлено (самое дорогое из ${r.pf_tasks})` : 'подставлено')),
    txt(fmtDate(r.calc_date)), txt(fmtDate(r.pf_calc_date)), num(r.vol, 0),
    num(r.pf_osn, 4), num(r.fk_osn, 4), num(r.d_osn, 4), num(r.d_osn_pct, 1),
    num(r.pf_vsp, 4), num(r.fk_vsp, 4), num(r.d_vsp, 4), num(r.d_vsp_pct, 1),
    num(r.pf_mat, 4), num(r.fk_mat, 4), num(r.d_mat, 4), num(r.d_mat_pct, 1),
    num(r.d_mat_vol, 2), txt(r.suspect ? `да, расхождение больше чем в ${suspectRatio.value} раз` : ''),
  ].join('') + '</tr>').join('')

  // Фильтры — те, под которые загружены строки (снимок в load), а не текущие.
  const lf = loadedFilters.value
  const filters = props.filterConfig
    .filter(f => (lf[f.key] || []).length)
    .map(f => `${f.label}: ${(f.key === 'month'
      ? lf[f.key].map(m => MONTHS[Number(m) - 1] || m)
      : lf[f.key]).join(', ')}`)
  const span = head.length
  const info = [
    'Материалы: ФКСС против ПФКСС',
    filters.length ? `Фильтры: ${filters.join('; ')}` : 'Фильтры: не заданы',
    exactOnly.value ? 'Только задания, у которых есть ПФКСС с тем же номером задания' : '',
    'Суммы материалов на единицу, BYN, по строкам калькуляции. Отклонение — «ФКСС / ПФКСС − 1»; выше нуля — материалы в факте дороже.',
    'Сопоставление: ПФКСС того же задания, иначе ПФКСС артикула и плана подставлена (при нескольких — самая дорогая, как при простановке цены).',
    `Строк: ${list.length}` + (truncated.value ? ` — выборка обрезана до ${rowLimit.value}, сузьте фильтры` : ''),
    refreshedAt.value ? `Данные на ${new Date(refreshedAt.value).toLocaleString('ru-RU')}` : '',
  ].filter(Boolean)
  const html = '<html><head><meta charset="utf-8"></head><body><table border="1">'
    + info.map((s, i) => `<tr><td colspan="${span}">${i ? esc(s) : `<b>${esc(s)}</b>`}</td></tr>`).join('')
    + `<tr><td colspan="${span}"></td></tr>`
    + '<tr>' + head.map(h => `<th>${esc(h)}</th>`).join('') + '</tr>' + body + '</table></body></html>'

  const blob = new Blob(['\uFEFF' + html], { type: 'application/vnd.ms-excel' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `Материалы_ФКСС_ПФКСС_${new Date().toISOString().slice(0, 10)}.xls`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
</script>

<style scoped>
.section-title { font-size: var(--fs-lg); font-weight: var(--fw-medium); margin: var(--sp-2) 0 0; }
.card {
  border: 1px solid var(--border); border-radius: var(--rd-3);
  background: var(--bg-surface); padding: var(--sp-4);
  display: flex; flex-direction: column; gap: var(--sp-2); min-width: 0;
}
.card-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-3); flex-wrap: wrap; }
.card-note { font-size: var(--fs-2xs); color: var(--text-muted); margin: 0; }
.warn { color: var(--neg); }
.error { color: var(--neg); font-size: var(--fs-sm); margin: 0; }
.muted { color: var(--text-muted); }
.pos { color: var(--pos); }
.neg { color: var(--neg); }

.mc-actions { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.mc-check { display: flex; align-items: center; gap: var(--sp-1); font-size: var(--fs-xs); color: var(--text-muted); cursor: pointer; }
.match-tag { font-family: var(--font-mono); }

.crumbs { display: flex; align-items: center; gap: var(--sp-1); flex-wrap: wrap; }
.crumb {
  border: none; background: transparent; padding: 0 2px; cursor: pointer;
  font-size: var(--fs-2xs); color: var(--accent); max-width: 260px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.crumb:disabled { color: var(--text-muted); cursor: default; }
.crumb-sep { font-size: var(--fs-2xs); color: var(--text-muted); }

.table-wrap { overflow-x: auto; }
.matrix { width: 100%; border-collapse: collapse; font-size: var(--fs-xs); }
.matrix th, .matrix td { padding: var(--sp-1) var(--sp-2); border-bottom: 1px solid var(--border); white-space: nowrap; }
.matrix th { text-align: right; font-weight: var(--fw-medium); color: var(--text-muted); font-size: var(--fs-2xs); }
.matrix th.grp { text-align: center; border-bottom: none; }
.matrix th.sortable { cursor: pointer; user-select: none; }
.matrix th.sortable:hover { color: var(--accent); }
.matrix th.col-label, .matrix td.col-label { text-align: left; }
.matrix td.num { text-align: right; font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.matrix tbody tr.drillable { cursor: pointer; }
.matrix tbody tr.drillable:hover, .matrix tbody tr.open { background: var(--bg-surface-2); }
.matrix tbody tr.suspect td { opacity: 0.6; }
.matrix tfoot td { font-weight: var(--fw-medium); border-top: 1px solid var(--border); }
.row-label { display: block; }
.row-name { display: block; font-size: var(--fs-2xs); color: var(--text-muted); }
.caret { display: inline-block; width: 1em; color: var(--text-muted); }
.empty-row { text-align: center; color: var(--text-muted); padding: var(--sp-4) !important; }

.lines-row > td { background: var(--bg-surface-2); padding: var(--sp-2) var(--sp-3) var(--sp-3); white-space: normal; }
.matrix.lines { margin-top: var(--sp-2); background: var(--bg-surface); }
.matrix.lines td.col-label { white-space: normal; min-width: 120px; }
.kind {
  display: inline-block; min-width: 2.4em; margin-right: var(--sp-1);
  font-size: var(--fs-2xs); color: var(--text-muted); font-family: var(--font-mono);
}
.badge {
  display: inline-block; margin-left: var(--sp-1); padding: 0 var(--sp-1);
  border: 1px solid var(--border); border-radius: var(--rd-2);
  font-size: var(--fs-2xs); color: var(--neg);
}
.badge-split { color: var(--text-muted); }
.changed { color: var(--accent); }
</style>
