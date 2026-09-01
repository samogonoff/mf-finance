<template>
  <div class="page-margin">
    <header class="page-header">
      <div>
        <h1 class="page-title">Маржа выпуска</h1>
        <p class="page-sub">
          <span v-if="meta.cache_refreshed_at">Данные на {{ fmtDateTime(meta.cache_refreshed_at) }}</span>
          <span v-else class="warn">Свежесть кэша неизвестна</span>
          <span class="dot">·</span>
          <span :class="{ warn: coveragePct < 80 }">
            в расчёте {{ fmtInt(tiles.calc_count) }} из {{ fmtInt(meta.calc_total) }}
            калькуляций {{ meta.volume_sign || 'ФКСС' }} ({{ coveragePct }}%)
          </span>
          <span class="dot">·</span>
          <span>{{ fmtInt(tiles.model_count) }} моделей</span>
          <span class="dot">·</span>
          <!-- Что считается выпуском, обязано быть видно рядом с цифрами. -->
          <span :title="`Выпуск — только калькуляции ${meta.volume_sign || 'ФКСС'} с объёмом; полуфабрикаты исключены`">
            только {{ meta.volume_sign || 'ФКСС' }}, полуфабрикаты исключены
          </span>
        </p>
      </div>

      <div class="header-actions">
        <NuxtLink to="/cost/commercial" class="btn btn-ghost btn-sm">
          <Icon name="lucide:chart-pie" /> Коммерческая эффективность
        </NuxtLink>
        <!-- База себестоимости: факт (стоимость минуты, которую Лиса ведёт с
             января 2026) или норматив. Ходит на сервер — по базе считаются суммы. -->
        <div class="currency-switch" role="group" aria-label="База себестоимости" title="Себестоимость: по фактической или нормативной стоимости минуты">
          <button v-for="b in (meta.cost_bases || [])" :key="b.key" class="btn btn-sm"
                  :class="b.key === costBasis ? 'btn-primary' : 'btn-ghost'"
                  @click="setBasis(b.key)">{{ b.key === 'fact' ? 'Факт' : 'Норматив' }}</button>
        </div>
        <div class="currency-switch" role="group" aria-label="Валюта">
          <button v-for="c in ['BYN', 'USD']" :key="c" class="btn btn-sm"
                  :class="c === currency ? 'btn-primary' : 'btn-ghost'"
                  @click="currency = c as 'BYN' | 'USD'">{{ c }}</button>
        </div>
      </div>
    </header>

    <!-- Период и его сравнения. «Прошлый месяц» и «прошлый год» — это тот же
         набор месяцев, сдвинутый назад (как DATEADD в Power BI): для «2026 года»
         прошлый месяц — это декабрь 2025…июль 2026, а не один месяц. Без этой
         подписи сравнение читается неверно. -->
    <p class="basis">
      Период: <strong>{{ periodLabel }}</strong>
      <span class="dot">·</span>
      сравнение с прошлым месяцем и годом — тот же набор месяцев, сдвинутый назад,
      не дальше последнего месяца с выпуском
      <span class="dot">·</span>
      по дате производства
      <span class="dot">·</span>
      проценты — в BYN при любой валюте
      <span class="dot">·</span>
      <!-- Факт есть не везде: у части фирм ставка минуты в Лисе не заведена, и
           источник считает по ней ноль. Такие строки — норматив, и доля годного
           факта обязана стоять рядом с базой. -->
      себестоимость — по <strong>{{ meta.cost_basis_label || 'фактической стоимости минуты' }}</strong>
      <template v-if="costBasis === 'fact'">
        <span :class="{ warn: factCoverage !== null && factCoverage < 100 }">
          (факт годен у {{ factCoverage === null ? '—' : fmtPct(factCoverage, 0) }} выпуска периода,
          остальное — норматив)
        </span>
      </template>
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
                         @change="reload()" />
      </div>
      <button class="btn btn-ghost btn-sm reset" @click="resetFilters">Сбросить</button>
    </section>

    <p v-if="error" class="error">{{ error }}</p>

    <!-- Плитки KPI. В Power BI под каждый показатель три плитки (текущий, прошлый
         месяц, прошлый год); здесь сравнения — строками внутри одной плитки,
         чтобы «54% против 47% год назад» читалось одним взглядом. -->
    <section class="tiles">
      <article v-for="t in tileList" :key="t.label" class="tile">
        <span class="tile-label">{{ t.label }}</span>
        <span class="tile-value" :class="{ empty: t.value === null }">{{ t.value === null ? '—' : t.value }}</span>
        <span v-if="t.note" class="tile-note">{{ t.note }}</span>
        <ul class="cmp">
          <li v-for="c in t.lines" :key="c.label" class="cmp-line">
            <span class="cmp-label">{{ c.label }}</span>
            <span class="cmp-value">{{ c.value ?? '—' }}</span>
            <span v-if="c.delta" class="cmp-delta" :class="c.good === null ? '' : (c.good ? 'pos' : 'neg')">{{ c.delta }}</span>
          </li>
        </ul>
      </article>
    </section>

    <h2 class="section-title">Маржа выпуска</h2>
    <section class="charts">
      <article class="card">
        <div class="card-head">
          <h3 class="card-title">Динамика маржи выпуска, {{ currency }}</h3>
          <span class="hint">клик по месяцу — фильтр</span>
        </div>
        <p class="card-note">
          Столбцы — маржа текущего года и того же месяца прошлого года, линия —
          темп роста к прошлому году. Фильтр «Месяц» на график не влияет, только «Год».
        </p>
        <div class="chart-box">
          <ClientOnly><Bar v-if="marginDynData" :data="marginDynData" :options="marginDynOptions" /></ClientOnly>
        </div>
      </article>

      <article class="card">
        <div class="card-head">
          <h3 class="card-title">Динамика маржинальности, %</h3>
          <span class="hint">клик по месяцу — фильтр</span>
        </div>
        <p class="card-note">
          Маржинальность = маржа / выпуск в отпускных ценах, взвешенная по тиражу.
          Пунктир — норма по Level 01 из таргетов раздела
          <span v-if="tiles.target_coverage_pct !== null && tiles.target_coverage_pct !== undefined">
            (норма задана для {{ fmtPct(tiles.target_coverage_pct, 0) }} выпуска)</span>.
        </p>
        <div class="chart-box">
          <ClientOnly><Line v-if="pctDynData" :data="pctDynData" :options="pctDynOptions" /></ClientOnly>
        </div>
      </article>

      <article class="card">
        <div class="card-head">
          <h3 class="card-title">Маржа по бренд-менеджерам, {{ currency }}</h3>
          <span class="hint">клик по столбцу — фильтр</span>
        </div>
        <p class="card-note">Текущий период против прошлого года. Пустой столбец прошлого года — выпуска тогда не было.</p>
        <div class="chart-box">
          <ClientOnly><Bar v-if="bmMoneyData" :data="bmMoneyData" :options="bmOptions(currency)" /></ClientOnly>
        </div>
      </article>

      <article class="card">
        <div class="card-head">
          <h3 class="card-title">Маржинальность по бренд-менеджерам, %</h3>
          <span class="hint">клик по столбцу — фильтр</span>
        </div>
        <p class="card-note">Взвешенная по выпуску, в BYN. Порядок — по марже текущего периода.</p>
        <div class="chart-box">
          <ClientOnly><Bar v-if="bmPctData" :data="bmPctData" :options="bmOptions('%')" /></ClientOnly>
        </div>
      </article>

      <article class="card">
        <div class="card-head">
          <h3 class="card-title">Маржа по Level 01, {{ currency }}</h3>
          <span class="hint">клик по сегменту — фильтр</span>
        </div>
        <p class="card-note">
          Доли — только положительная маржа: отрицательную в круг не положить.
          <span v-if="negativeLevels.length" class="warn">
            Отрицательная маржа: {{ negativeLevels.join(', ') }}.
          </span>
        </p>
        <div class="chart-box chart-box-tall">
          <ClientOnly><Doughnut v-if="donutData" :data="donutData" :options="donutOptions" /></ClientOnly>
        </div>
      </article>
    </section>

    <!-- Матрица с проваливанием: Level 01 → … → Level 05 → артикул → задание.
         Путь применяется только к матрице и водопадам — не к плиткам и графикам. -->
    <section class="card">
      <div class="card-head">
        <div>
          <h3 class="card-title">Матрица · {{ meta.matrix_label }}</h3>
          <nav class="crumbs">
            <button class="crumb" :disabled="!matrixPath.length" @click="drillTo(0)">Все</button>
            <template v-for="(c, i) in (meta.matrix_path || [])" :key="i">
              <span class="crumb-sep">›</span>
              <button class="crumb" :disabled="i === matrixPath.length - 1"
                      :title="c.label" @click="drillTo(i + 1)">{{ c.value || '(не указано)' }}</button>
            </template>
          </nav>
        </div>
        <div class="view-switch" role="group" aria-label="Набор колонок">
          <button v-for="v in matrixViews" :key="v.key" class="btn btn-sm"
                  :class="v.key === matrixView ? 'btn-primary' : 'btn-ghost'"
                  @click="matrixView = v.key">{{ v.label }}</button>
        </div>
      </div>
      <p class="card-note">
        <span v-if="meta.matrix_can_drill">Клик по строке — провалиться на уровень ниже. </span>
        Строки отсортированы по марже текущего периода.
        <span v-if="meta.matrix_truncated" class="warn">
          Показаны первые {{ meta.matrix_row_limit }} строк — сузьте фильтры.</span>
        <span v-if="matrixView === 'margin'">
          «Ниже нормы» — маржинальность строки меньше нормы её Level 01; без нормы — прочерк.</span>
      </p>
      <div class="table-wrap">
        <table class="matrix">
          <thead>
            <tr>
              <th class="col-label">{{ meta.matrix_label }}</th>
              <th v-for="c in matrixColumns" :key="c.key" :title="c.title">{{ c.label }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in matrix" :key="row.label"
                :class="{ drillable: meta.matrix_can_drill, 'below-target': row.below_target }"
                @click="drillInto(row.label)">
              <td class="col-label">
                <span class="row-label">{{ row.label || '(не указано)' }}</span>
                <span v-if="row.name" class="row-name">{{ row.name }}</span>
              </td>
              <td v-for="c in matrixColumns" :key="c.key" class="num" :class="c.cls?.(row)">
                {{ c.fmt(row) }}
              </td>
            </tr>
            <tr v-if="!matrix.length"><td :colspan="matrixColumns.length + 1" class="empty-row">Нет выпуска в выбранном периоде</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Норматив против факта. Сравнение только на строках с ГОДНЫМ фактом: там,
         где ставка минуты в Лисе не заведена, источник считает по ней ноль, и
         такой «факт» сравнивать не с чем (см. app/margin.py, _FACT_OK). -->
    <h2 class="section-title">Норматив против факта</h2>
    <p v-if="factCoverage === null || factCoverage === 0" class="card-note">
      В выбранном периоде фактической стоимости минуты нет — Лиса ведёт её с января 2026.
    </p>
    <template v-else>
      <p class="basis">
        Сравнение на строках с годным фактом — {{ fmtPct(factCoverage, 0) }} выпуска периода.
        Факт считается годным, если у каждой операции с нормативом есть фактическая ставка;
        иначе источник даёт по операции ноль, и такой «факт» — это норматив без пошива.
      </p>
      <section class="tiles">
        <article v-for="t in factTileList" :key="t.label" class="tile">
          <span class="tile-label">{{ t.label }}</span>
          <span class="tile-value" :class="{ empty: t.value === null }">{{ t.value === null ? '—' : t.value }}</span>
          <span v-if="t.note" class="tile-note">{{ t.note }}</span>
          <ul class="cmp">
            <li v-for="c in t.lines" :key="c.label" class="cmp-line">
              <span class="cmp-label">{{ c.label }}</span>
              <span class="cmp-value">{{ c.value ?? '—' }}</span>
              <span v-if="c.delta" class="cmp-delta" :class="c.good === null ? '' : (c.good ? 'pos' : 'neg')">{{ c.delta }}</span>
            </li>
          </ul>
        </article>
      </section>
      <section class="charts">
        <article class="card">
          <div class="card-head">
            <h3 class="card-title">Маржинальность по нормативу и по факту, %</h3>
            <span class="hint">клик по месяцу — фильтр</span>
          </div>
          <p class="card-note">Обе линии — по одним и тем же строкам с годным фактом. Разрыв — эффект фактической ставки минуты.</p>
          <div class="chart-box">
            <ClientOnly><Line v-if="factDynData" :data="factDynData" :options="pctDynOptions" /></ClientOnly>
          </div>
        </article>
        <article class="card">
          <div class="card-head">
            <h3 class="card-title">Отклонение маржинальности факт − норматив по бренд-менеджерам, пп</h3>
            <span class="hint">клик по столбцу — фильтр</span>
          </div>
          <p class="card-note">Ниже нуля — факт дороже норматива. Детализация до артикула — в матрице, колонки «Норматив / факт».</p>
          <div class="chart-box">
            <ClientOnly><Bar v-if="factBmData" :data="factBmData" :options="bmOptions('пп')" /></ClientOnly>
          </div>
        </article>
      </section>
    </template>

    <!-- Лист «Отклонения по артикулам» — просьба заказчика 25.08.2026. Формат
         повторяет страницу «Отклонение сс» его отчёта Power BI, плюс № плана. -->
    <h2 class="section-title">Отклонения по артикулам</h2>
    <section class="card">
      <div class="card-head">
        <div>
          <p class="card-note">
            Строка — план + модель + артикул. Три себестоимости <strong>единицы</strong> в BYN:
            плановая (справочник моделей Лисы), нормативная и фактическая (по стоимости минуты).
            Отклонение — «что сравниваем / база − 1».
            <span v-if="meta.deviations_truncated" class="warn">
              Показаны {{ meta.deviations_row_limit }} артикулов с наибольшим выпуском — сузьте фильтры.</span>
          </p>
          <p class="card-note">
            Плановая себестоимость ведётся не везде: в выбранном периоде она есть у
            <strong>{{ devPlanCoverage === null ? '—' : fmtPct(devPlanCoverage, 0) }}</strong>
            показанных артикулов<span v-if="devView === 'plan' && devPlanCoverage !== null && devPlanCoverage < 20">
              — для носков и колготок план в Лисе не заводится, для них смысл имеет вкладка «Норматив / факт»</span>.
          </p>
        </div>
        <div class="view-switch" role="group" aria-label="Что сравнивать">
          <button v-for="v in devViews" :key="v.key" class="btn btn-sm"
                  :class="v.key === devView ? 'btn-primary' : 'btn-ghost'"
                  @click="devView = v.key">{{ v.label }}</button>
        </div>
      </div>
      <div class="table-wrap">
        <table class="matrix">
          <thead>
            <tr>
              <th v-for="c in devColumns" :key="c.key" :class="{ 'col-label': c.text }"
                  :title="c.title" class="sortable" @click="sortDev(c.key)">
                {{ c.label }}<span v-if="devSort.key === c.key">{{ devSort.asc ? ' ▲' : ' ▼' }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in devRows" :key="i">
              <td v-for="c in devColumns" :key="c.key" :class="[c.text ? 'col-label' : 'num', c.cls?.(row)]">
                {{ c.fmt(row) }}
              </td>
            </tr>
            <tr v-if="!devRows.length">
              <td :colspan="devColumns.length" class="empty-row">
                {{ devView === 'plan' ? 'Плановая себестоимость не заведена ни у одного артикула выборки'
                                      : 'Нет артикулов с годной фактической себестоимостью' }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <h2 class="section-title">Динамика себестоимости</h2>
    <section class="charts">
      <article class="card">
        <div class="card-head">
          <h3 class="card-title">Себестоимость штуки по месяцам, {{ currency }}</h3>
          <span class="hint">клик по месяцу — фильтр</span>
        </div>
        <p class="card-note">Себестоимость выпуска / штуки выпуска; сплошная — текущий год, пунктир — тот же месяц прошлого года.</p>
        <div class="chart-box">
          <ClientOnly><Line v-if="unitCostDynData" :data="unitCostDynData" :options="unitCostOptions" /></ClientOnly>
        </div>
      </article>

      <article class="card">
        <div class="card-head">
          <h3 class="card-title">Прирост себестоимости штуки к прошлому году, %</h3>
          <span class="hint">клик по месяцу — фильтр</span>
        </div>
        <p class="card-note">Отрицательный прирост — штука подешевела. Месяцы без прошлогоднего выпуска пропущены.</p>
        <div class="chart-box">
          <ClientOnly><Bar v-if="unitCostDevData" :data="unitCostDevData" :options="devOptions" /></ClientOnly>
        </div>
      </article>
    </section>

    <h2 class="section-title">Отклонение маржи · по {{ meta.matrix_label }}</h2>
    <section class="charts">
      <article class="card">
        <h3 class="card-title">К прошлому месяцу, {{ currency }}</h3>
        <p class="card-note">Водопад: вклад каждой строки матрицы в изменение маржи; последний столбец — итог.</p>
        <div class="chart-box chart-box-tall">
          <ClientOnly><Bar v-if="waterfallPm" :data="waterfallPm" :options="waterfallOptions" /></ClientOnly>
        </div>
      </article>
      <article class="card">
        <h3 class="card-title">К прошлому году, {{ currency }}</h3>
        <p class="card-note">Тот же разрез, база — тот же набор месяцев год назад.</p>
        <div class="chart-box chart-box-tall">
          <ClientOnly><Bar v-if="waterfallPy" :data="waterfallPy" :options="waterfallOptions" /></ClientOnly>
        </div>
      </article>
    </section>

    <!-- Тот же макет в Superset — инструмент разработчика для сравнения реализаций. -->
    <section v-if="supersetUrl" class="card compare">
      <div class="card-head">
        <div>
          <h3 class="card-title">Тот же дашборд в Superset — для сравнения</h3>
          <p class="card-note">Встроен без guest-токенов: браузер уже залогинен в Superset на том же хосте.</p>
        </div>
        <div class="compare-actions">
          <button class="btn btn-ghost btn-sm" @click="showSuperset = !showSuperset">
            {{ showSuperset ? 'Скрыть' : 'Показать' }}
          </button>
          <a :href="supersetUrl" target="_blank" rel="noopener" class="btn btn-ghost btn-sm">Открыть отдельно</a>
        </div>
      </div>
      <iframe v-if="showSuperset" :src="supersetEmbedSrc" class="superset-frame" title="Дашборд Superset" loading="lazy" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Bar, Doughnut, Line } from 'vue-chartjs'
import {
  ArcElement, BarController, BarElement, CategoryScale, Chart as ChartJS, Legend,
  LineController, LineElement, LinearScale, PointElement, Tooltip,
} from 'chart.js'
import { useChartTheme } from '~/composables/useChartTheme'

// LineController и BarController — явно: смешанный график (столбцы + линия на
// второй оси) собирается компонентом Bar с датасетом type: 'line'.
ChartJS.register(ArcElement, BarController, BarElement, CategoryScale, Legend,
                 LineController, LineElement, LinearScale, PointElement, Tooltip)

const config = useRuntimeConfig()
const apiBase = computed(() =>
  config.public.costOnly ? '' : ((config.public.apiBase as string) || '')
)

/** Бэкенд берёт пользователя из X-Cost-User — заголовок каждая страница раздела
 * шлёт сама, см. commercial.vue. */
const { user } = useAuth()
const fetchHeaders = computed(() => {
  const email = user.value?.email || ''
  return email ? { 'X-Cost-User': email } : {}
})

/** Ключи совпадают с белым списком в app/margin.py — лишнее сервер игнорирует.
 * Признака калькуляции нет: выпуск всегда по одному признаку. */
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
]

const selected = reactive<Record<string, string[]>>(
  Object.fromEntries(filterConfig.map(f => [f.key, [] as string[]])) as any
)
const filterOptions = ref<Record<string, string[]>>({})
const truncatedFilters = ref<string[]>([])

const currency = ref<'BYN' | 'USD'>('BYN')
const cur = computed(() => (currency.value === 'BYN' ? 'byn' : 'usd'))
/** Путь проваливания по матрице — применяется только к ней и к водопадам. */
const matrixPath = ref<string[]>([])
const matrixView = ref<'margin' | 'cost' | 'fact'>('margin')
const matrixViews = [
  { key: 'margin' as const, label: 'Маржа' },
  { key: 'cost' as const, label: 'Себестоимость' },
  { key: 'fact' as const, label: 'Норматив / факт' },
]
/** База себестоимости — см. COST_BASES в app/margin.py. Ходит на сервер. */
const costBasis = ref<'fact' | 'norm'>('fact')
function setBasis(key: string) {
  if (key !== 'fact' && key !== 'norm') return
  costBasis.value = key
  reload()
}

const loading = ref(false)
const error = ref('')

const supersetUrl = computed(() => (config.public.supersetMarginEmbedUrl as string) || '')
// Раскрыт по умолчанию, как на коммерческом дашборде: свёрнутый блок читался как
// «в Superset ничего не собрано». Ключ supersetMarginEmbedUrl появляется в
// runtimeConfig только после перезапуска dev-сервера nuxt-cost.
const showSuperset = ref(true)
const supersetEmbedSrc = computed(() => {
  const u = supersetUrl.value
  return u ? u + (u.includes('?') ? '&' : '?') + 'standalone=1' : ''
})

const tiles = ref<Record<string, any>>({})
const months = ref<any[]>([])
const byBm = ref<any[]>([])
const byLevel01 = ref<any[]>([])
const matrix = ref<any[]>([])
const meta = ref<Record<string, any>>({ matrix_path: [], period: { year: [], month: [] } })

// ── Загрузка ────────────────────────────────────────────────────────────────
// Один запрос на всё; обе валюты в ответе — переключатель на сервер не ходит.

function buildParams(defaultPeriod: boolean): string {
  const p = new URLSearchParams()
  for (const f of filterConfig) for (const v of selected[f.key] || []) p.append(f.key, v)
  for (const v of matrixPath.value) p.append('matrix_path', v)
  if (defaultPeriod) p.set('default_period', '1')
  p.set('cost_basis', costBasis.value)
  return p.toString()
}

/** defaultPeriod — только первая загрузка: без выбранного года сравнения «с
 * прошлым месяцем/годом» определены, но бессмысленны, поэтому сервер подставит
 * последний год с выпуском, а мы покажем его чипом в фильтре. Дальше пустой
 * год означает «все годы» — пользователь снял его сам. */
async function reload(defaultPeriod = false) {
  loading.value = true
  error.value = ''
  try {
    const res = await $fetch<any>(`${apiBase.value}/api/cost/margin?${buildParams(defaultPeriod)}`,
                                  { headers: fetchHeaders.value })
    tiles.value = res.tiles || {}
    months.value = res.months || []
    byBm.value = res.by_bm || []
    byLevel01.value = res.by_level01 || []
    matrix.value = res.matrix || []
    deviations.value = res.deviations || []
    meta.value = res.meta || { matrix_path: [], period: { year: [], month: [] } }
    filterOptions.value = res.options || {}
    truncatedFilters.value = res.meta?.options_truncated || []
    if (res.meta?.year_defaulted && !(selected.year || []).length) {
      selected.year = [...(res.meta.period?.year || [])]
    }
  } catch (e: any) {
    console.error('[cost] margin dashboard failed', e)
    error.value = e?.data?.detail || e?.message || 'Не удалось загрузить дашборд'
  } finally {
    loading.value = false
  }
}

function resetFilters() {
  for (const f of filterConfig) selected[f.key] = []
  matrixPath.value = []
  reload(true)
}

onMounted(() => reload(true))

// ── Фильтры и подписи ───────────────────────────────────────────────────────

const MONTHS_FULL = ['январь', 'февраль', 'март', 'апрель', 'май', 'июнь',
                     'июль', 'август', 'сентябрь', 'октябрь', 'ноябрь', 'декабрь']
const MONTHS_SHORT = ['янв', 'фев', 'мар', 'апр', 'май', 'июн',
                      'июл', 'авг', 'сен', 'окт', 'ноя', 'дек']

function optionsFor(key: string): any[] {
  const raw = filterOptions.value[key] || []
  if (key !== 'month') return raw
  return raw.map(v => ({ value: v, label: MONTHS_FULL[Number(v) - 1] || v }))
}

const periodLabel = computed(() => {
  const p = meta.value.period || {}
  const years: string[] = p.year || []
  const ms: string[] = (p.month || []).map((m: string) => MONTHS_FULL[Number(m) - 1] || m)
  if (!years.length && !ms.length) return 'все годы'
  const y = years.length ? years.join(', ') : 'все годы'
  return ms.length ? `${ms.join(', ')} ${y}` : `${y}, все месяцы`
})

// ── Проваливание и кросфильтрация ───────────────────────────────────────────

function drillInto(value: string) {
  if (!meta.value.matrix_can_drill) return
  matrixPath.value = [...matrixPath.value, value ?? '']
  reload()
}

function drillTo(depth: number) {
  if (depth >= matrixPath.value.length) return
  matrixPath.value = matrixPath.value.slice(0, depth)
  reload()
}

/** Клик по элементу графика — обычный фильтр панели: выбор виден чипом и
 * снимается штатно. Повторный клик снимает. */
function toggleFilter(key: string, value: string) {
  if (value === undefined || value === null || !(key in selected)) return
  const cur = selected[key] || []
  selected[key] = cur.includes(value) ? cur.filter(v => v !== value) : [...cur, value]
  reload()
}

/** Клик по месяцу → год + месяц (без года «июнь» означал бы все июни). */
function crossFilterMonth(index: number) {
  const ym = months.value[index]?.ym
  if (!ym) return
  const [year, month] = String(ym).split('-')
  const already = (selected.year || []).includes(year) && (selected.month || []).includes(month)
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

const nf0 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 0 })
const nf1 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 1, maximumFractionDigits: 1 })
const nf2 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const isNum = (v: any) => v !== null && v !== undefined && Number.isFinite(Number(v))
const fmtInt = (v: any) => (isNum(v) ? nf0.format(Number(v)) : '—')
const fmtMoney = (v: any) => (isNum(v) ? nf2.format(Number(v)) : '—')
const fmtPct = (v: any, d = 2) => (isNum(v) ? `${(d === 0 ? nf0 : d === 1 ? nf1 : nf2).format(Number(v))}%` : '—')
/** Со знаком: отклонения читаются только со знаком. */
const fmtSigned = (v: any, f: (x: number) => string) =>
  isNum(v) ? `${Number(v) > 0 ? '+' : Number(v) < 0 ? '−' : ''}${f(Math.abs(Number(v)))}` : ''
const fmtPp = (v: any) => (isNum(v) ? `${fmtSigned(v, x => nf1.format(x))} пп` : '—')
const fmtSignedPct = (v: any) => (isNum(v) ? `${fmtSigned(v, x => nf1.format(x))}%` : '—')
/** Крупные суммы в плитках — компактно, полное число в подсказке. */
const fmtCompact = (v: any) => {
  if (!isNum(v)) return '—'
  const n = Number(v), a = Math.abs(n)
  if (a >= 1e9) return `${nf2.format(n / 1e9)} млрд`
  if (a >= 1e6) return `${nf2.format(n / 1e6)} млн`
  if (a >= 1e4) return `${nf1.format(n / 1e3)} тыс.`
  return nf0.format(n)
}
const fmtDateTime = (iso: string) =>
  new Date(iso).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })

const coveragePct = computed(() => {
  const a = Number(tiles.value.calc_count || 0), b = Number(meta.value.calc_total || 0)
  return b ? Math.round((100 * a) / b) : 0
})

// ── Плитки ──────────────────────────────────────────────────────────────────

type CmpLine = { label: string; value: string | null; delta: string; good: boolean | null }

/** Строка сравнения: значение прошлого периода и отклонение текущего от него.
 * higherIsBetter=false — для себестоимости: рост это плохо. */
function cmp(label: string, prev: any, dev: any, fmtVal: (v: any) => string,
             fmtDev: (v: any) => string, higherIsBetter = true): CmpLine {
  const good = isNum(dev) && Number(dev) !== 0 ? (Number(dev) > 0) === higherIsBetter : null
  return { label, value: isNum(prev) ? fmtVal(prev) : null, delta: isNum(dev) ? fmtDev(dev) : '', good }
}

const tileList = computed(() => {
  const t = tiles.value, c = cur.value, C = currency.value
  const targetNote = isNum(t.target_pct)
    ? `норма по Level 01 задана для ${fmtPct(t.target_coverage_pct, 0)} выпуска`
    : 'норма по Level 01 не задана для выбранного выпуска'
  return [
    {
      label: 'Маржинальность выпуска, %', value: isNum(t.margin_pct) ? fmtPct(t.margin_pct) : null,
      note: targetNote,
      lines: [
        cmp('норма', t.target_pct, t.margin_pct_dev_target, fmtPct, fmtPp),
        cmp('пред. месяц', t.margin_pct_pm, t.margin_pct_dev_pm, fmtPct, fmtPp),
        cmp('пред. год', t.margin_pct_py, t.margin_pct_dev_py, fmtPct, fmtPp),
      ],
    },
    {
      label: `Маржа выпуска, ${C}`, value: isNum(t[`margin_${c}`]) ? fmtCompact(t[`margin_${c}`]) : null,
      note: isNum(t[`margin_${c}`]) ? `${fmtMoney(t[`margin_${c}`])} · выпуск в отпускных ценах − себестоимость` : '',
      lines: [
        cmp('пред. месяц', t[`margin_${c}_pm`], t.growth_pct_pm, fmtCompact, fmtSignedPct),
        cmp('пред. год', t[`margin_${c}_py`], t.growth_pct_py, fmtCompact, fmtSignedPct),
      ],
    },
    {
      label: 'Рентабельность выпуска, %', value: isNum(t.profit_pct) ? fmtPct(t.profit_pct) : null,
      note: 'маржа / себестоимость выпуска',
      lines: [
        cmp('пред. месяц', t.profit_pct_pm, isNum(t.profit_pct) && isNum(t.profit_pct_pm) ? t.profit_pct - t.profit_pct_pm : null, fmtPct, fmtPp),
        cmp('пред. год', t.profit_pct_py, isNum(t.profit_pct) && isNum(t.profit_pct_py) ? t.profit_pct - t.profit_pct_py : null, fmtPct, fmtPp),
      ],
    },
    {
      label: 'Выпуск, шт', value: isNum(t.vol) ? fmtInt(t.vol) : null,
      note: `сумма тиражей · ${meta.value.volume_sign || 'ФКСС'}`,
      lines: [
        cmp('пред. месяц', t.vol_pm, isNum(t.vol) && t.vol_pm ? 100 * (t.vol / t.vol_pm - 1) : null, fmtInt, fmtSignedPct),
        cmp('пред. год', t.vol_py, isNum(t.vol) && t.vol_py ? 100 * (t.vol / t.vol_py - 1) : null, fmtInt, fmtSignedPct),
      ],
    },
    {
      label: `Себестоимость штуки, ${C}`, value: isNum(t[`unit_cost_${c}`]) ? fmtMoney(t[`unit_cost_${c}`]) : null,
      note: 'себестоимость выпуска / штуки',
      lines: [
        cmp('пред. месяц', t[`unit_cost_${c}_pm`], t[`unit_cost_dev_pct_${c}_pm`], fmtMoney, fmtSignedPct, false),
        cmp('пред. год', t[`unit_cost_${c}_py`], t[`unit_cost_dev_pct_${c}_py`], fmtMoney, fmtSignedPct, false),
      ],
    },
    {
      label: `Сырьё на штуку, ${C}`, value: isNum(t[`unit_raw_${c}`]) ? fmtMoney(t[`unit_raw_${c}`]) : null,
      note: 'основные + вспомогательные материалы / штуки',
      lines: [
        cmp('пред. год', t[`unit_raw_${c}_py`], t[`unit_raw_dev_pct_${c}_py`], fmtMoney, fmtSignedPct, false),
        cmp('минут пошива на штуку', t.min_per_unit, null, (v: any) => nf2.format(Number(v)), () => ''),
      ],
    },
  ]
})

// ── Отклонения по артикулам ─────────────────────────────────────────────────
//
// Две вкладки, как просил заказчик: «фактическая от плановой» и «нормативная от
// фактической». Обе — на одних строках (план + модель + артикул), меняется
// только набор колонок и то, по чему строки отбираются: показывать в отчёте
// «Факт / план» артикул без плана бессмысленно.

const deviations = ref<any[]>([])
const devView = ref<'plan' | 'fact'>('plan')
const devViews = [
  { key: 'plan' as const, label: 'Факт / план' },
  { key: 'fact' as const, label: 'Норматив / факт' },
]
/** Сортировка листа. По умолчанию — по отклонению, по убыванию: заказчик
 * смотрит его как список «где разошлось сильнее всего» (так и в его Power BI). */
const devSort = reactive<{ key: string; asc: boolean }>({ key: 'dev', asc: false })

function sortDev(key: string) {
  if (devSort.key === key) devSort.asc = !devSort.asc
  else { devSort.key = key; devSort.asc = false }
}

/** Ключевая колонка отклонения для текущей вкладки — по ней сортируем и её
 * подсвечиваем. */
const devKey = computed(() => (devView.value === 'plan' ? 'dev_fact_plan_pct' : 'dev_fact_norm_pct'))

const devRows = computed(() => {
  // Во вкладке «Факт / план» строки без плана скрываем: пустая колонка сравнения
  // — не информация, а шум на весь экран.
  const rows = deviations.value.filter(r =>
    devView.value === 'plan' ? isNum(r.unit_plan_byn) : isNum(r.unit_fact_byn))
  const k = devSort.key === 'dev' ? devKey.value : devSort.key
  const dir = devSort.asc ? 1 : -1
  return [...rows].sort((a, b) => {
    const x = a[k], y = b[k]
    // Пустые всегда внизу, независимо от направления.
    if (!isNum(x) && !isNum(y)) return 0
    if (!isNum(x)) return 1
    if (!isNum(y)) return -1
    if (typeof x === 'string' || typeof y === 'string') return dir * String(x).localeCompare(String(y), 'ru')
    return dir * (Number(x) - Number(y))
  })
})

/** Доля показанных артикулов, у которых есть плановая себестоимость. */
const devPlanCoverage = computed(() => {
  const all = deviations.value.length
  if (!all) return null
  return (100 * deviations.value.filter(r => isNum(r.unit_plan_byn)).length) / all
})

const devColumns = computed<Col[]>(() => {
  const devCls = (v: any, higherIsBetter = true) =>
    !isNum(v) || Number(v) === 0 ? '' : (Number(v) > 0) === higherIsBetter ? 'pos' : 'neg'
  const base: Col[] = [
    { key: 'plan_id', label: '№ плана', text: true, fmt: r => r.plan_id || '—' },
    { key: 'name', label: 'Наименование', text: true, fmt: r => r.name || '—' },
    { key: 'articul', label: 'Артикул', text: true, fmt: r => r.articul || '—' },
    { key: 'vol', label: 'Выпуск, шт', fmt: r => fmtInt(r.vol) },
  ]
  if (devView.value === 'plan') {
    return [...base,
      { key: 'unit_fact_byn', label: 'С/с штуки факт', title: 'по фактической стоимости минуты; где её нет — прочерк',
        fmt: r => fmtMoney(r.unit_fact_byn) },
      { key: 'unit_norm_byn', label: 'С/с штуки норматив', fmt: r => fmtMoney(r.unit_norm_byn) },
      { key: 'unit_plan_byn', label: 'С/с штуки плановая', title: 'PLAN_PRICE справочника моделей Лисы',
        fmt: r => fmtMoney(r.unit_plan_byn) },
      { key: 'dev_fact_plan_pct', label: 'Отклонение факт / план, %', title: 'факт / план − 1; выше нуля — дороже плана',
        fmt: r => fmtSignedPct(r.dev_fact_plan_pct), cls: r => devCls(r.dev_fact_plan_pct, false) },
      { key: 'dev_norm_plan_pct', label: 'Норматив / план, %', fmt: r => fmtSignedPct(r.dev_norm_plan_pct),
        cls: r => devCls(r.dev_norm_plan_pct, false) },
    ]
  }
  return [...base,
    { key: 'unit_norm_byn', label: 'С/с штуки норматив', fmt: r => fmtMoney(r.unit_norm_byn) },
    { key: 'unit_fact_byn', label: 'С/с штуки факт', fmt: r => fmtMoney(r.unit_fact_byn) },
    { key: 'dev_fact_norm_pct', label: 'Отклонение факт / норматив, %', title: 'факт / норматив − 1; выше нуля — факт дороже',
      fmt: r => fmtSignedPct(r.dev_fact_norm_pct), cls: r => devCls(r.dev_fact_norm_pct, false) },
    { key: 'fact_coverage_pct', label: 'Факт годен, % выпуска', title: 'у остальных строк артикула фактическая ставка минуты не заведена',
      fmt: r => fmtPct(r.fact_coverage_pct, 0), cls: r => (isNum(r.fact_coverage_pct) && r.fact_coverage_pct < 100 ? 'neg' : '') },
    { key: 'unit_price_byn', label: 'Отпускная цена', fmt: r => fmtMoney(r.unit_price_byn) },
  ]
})

// ── Норматив против факта ───────────────────────────────────────────────────
// Всё — на строках с годным фактом (fact_coverage_pct). См. _FACT_OK в app/margin.py.

const factCoverage = computed<number | null>(() =>
  isNum(tiles.value.fact_coverage_pct) ? Number(tiles.value.fact_coverage_pct) : null)

const factTileList = computed(() => {
  const t = tiles.value, c = cur.value, C = currency.value
  return [
    {
      label: 'Маржинальность по факту, %', value: isNum(t.margin_pct_fact) ? fmtPct(t.margin_pct_fact) : null,
      note: 'на строках с годным фактом',
      lines: [
        cmp('по нормативу', t.margin_pct_norm, t.fact_dev_pp, fmtPct, fmtPp),
        cmp('пред. месяц, факт', t.margin_pct_fact_pm,
            isNum(t.margin_pct_fact) && isNum(t.margin_pct_fact_pm) ? t.margin_pct_fact - t.margin_pct_fact_pm : null, fmtPct, fmtPp),
      ],
    },
    {
      label: `Себестоимость выпуска по факту, ${C}`, value: isNum(t[`cost_fact_${c}`]) ? fmtCompact(t[`cost_fact_${c}`]) : null,
      note: isNum(t[`cost_fact_${c}`]) ? fmtMoney(t[`cost_fact_${c}`]) : '',
      lines: [
        cmp('по нормативу', t[`cost_norm_${c}`],
            isNum(t[`cost_fact_${c}`]) && t[`cost_norm_${c}`] ? 100 * (t[`cost_fact_${c}`] / t[`cost_norm_${c}`] - 1) : null,
            fmtCompact, fmtSignedPct, false),
      ],
    },
    {
      label: `Себестоимость штуки по факту, ${C}`, value: isNum(t[`unit_cost_fact_${c}`]) ? fmtMoney(t[`unit_cost_fact_${c}`]) : null,
      note: 'себестоимость выпуска / штуки, строки с годным фактом',
      lines: [
        cmp('по нормативу', t[`unit_cost_norm_${c}`], t[`unit_cost_fact_dev_pct_${c}`], fmtMoney, fmtSignedPct, false),
      ],
    },
    {
      label: `Δ маржи факт − норматив, ${C}`, value: isNum(t[`fact_dev_${c}`]) ? fmtSigned(t[`fact_dev_${c}`], fmtCompact) : null,
      note: 'отрицательная — факт дороже норматива',
      lines: [
        cmp('маржа по факту', t[`margin_fact_${c}`], null, fmtCompact, () => ''),
        cmp('маржа по нормативу', t[`margin_norm_${c}`], null, fmtCompact, () => ''),
      ],
    },
    {
      label: 'Факт годен, % выпуска', value: factCoverage.value === null ? null : fmtPct(factCoverage.value, 1),
      note: 'у остальных строк фактическая ставка минуты в Лисе не заведена',
      lines: [
        cmp('пред. месяц', t.fact_coverage_pct_pm, null, (v: any) => fmtPct(v, 1), () => ''),
        cmp('пред. год', t.fact_coverage_pct_py, null, (v: any) => fmtPct(v, 1), () => ''),
      ],
    },
  ]
})

const factDynData = computed(() => {
  if (!months.value.length) return null
  return {
    labels: monthLabels.value,
    datasets: [
      { label: 'По факту, %', data: months.value.map(m => num(m.margin_pct_fact)),
        borderColor: accent.value, backgroundColor: accent.value, tension: 0.25 },
      { label: 'По нормативу, %', data: months.value.map(m => num(m.margin_pct_norm)),
        borderColor: withAlpha(accent.value, 0.5), backgroundColor: withAlpha(accent.value, 0.5), tension: 0.25, borderDash: [4, 4] },
    ],
  }
})

const factBmData = computed(() => {
  // Строки не фильтруем: клик по столбцу берёт byBm[idx] (см. bmOptions), и при
  // выкинутых строках индексы разъехались бы. Без факта — пустой столбец.
  if (!byBm.value.length || !byBm.value.some(r => isNum(r.fact_dev_pp))) return null
  const vals = byBm.value.map(r => num(r.fact_dev_pp))
  return {
    labels: bmLabels.value,
    datasets: [{ label: 'Δ маржинальности факт − норматив, пп', data: vals,
                 backgroundColor: vals.map(v => (v !== null && v >= 0 ? pos.value : neg.value)) }],
  }
})

// ── Тема графиков ───────────────────────────────────────────────────────────

const { ink, grid, accent, info, pos, neg, color, withAlpha, pointerOnHover } = useChartTheme()

// ── Данные графиков ─────────────────────────────────────────────────────────

function monthLabel(ym: string, multiYear: boolean): string {
  const [y, m] = String(ym).split('-')
  const name = MONTHS_SHORT[Number(m) - 1] || m
  return multiYear ? `${name} ${y}` : name
}
const multiYear = computed(() => new Set(months.value.map(m => String(m.ym).slice(0, 4))).size > 1)
const monthLabels = computed(() => months.value.map(m => monthLabel(m.ym, multiYear.value)))
const num = (v: any) => (isNum(v) ? Number(v) : null)

const marginDynData = computed(() => {
  if (!months.value.length) return null
  const c = cur.value
  return {
    labels: monthLabels.value,
    datasets: [
      { type: 'bar' as const, label: `Маржа, ${currency.value}`, data: months.value.map(m => num(m[`margin_${c}`])),
        backgroundColor: accent.value, order: 2 },
      { type: 'bar' as const, label: 'Маржа прошлого года', data: months.value.map(m => num(m[`margin_${c}_py`])),
        backgroundColor: withAlpha(accent.value, 0.35), order: 3 },
      { type: 'line' as const, label: 'Темп роста к прошлому году, %', data: months.value.map(m => num(m.growth_pct_py)),
        borderColor: pos.value, backgroundColor: pos.value, yAxisID: 'y2', tension: 0.25, order: 1 },
    ],
  }
})

const pctDynData = computed(() => {
  if (!months.value.length) return null
  const target = num(tiles.value.target_pct)
  const ds: any[] = [
    { label: 'Маржинальность, %', data: months.value.map(m => num(m.margin_pct)),
      borderColor: accent.value, backgroundColor: accent.value, tension: 0.25 },
    { label: 'Прошлый год, %', data: months.value.map(m => num(m.margin_pct_py)),
      borderColor: withAlpha(accent.value, 0.5), backgroundColor: withAlpha(accent.value, 0.5), tension: 0.25, borderDash: [4, 4] },
  ]
  if (target !== null) {
    ds.push({ label: 'Норма, %', data: months.value.map(() => target),
              borderColor: neg.value, backgroundColor: neg.value, borderDash: [2, 4], pointRadius: 0 })
  }
  return { labels: monthLabels.value, datasets: ds }
})

const bmLabels = computed(() => byBm.value.map(r => r.label || '(не указан)'))
const bmMoneyData = computed(() => {
  if (!byBm.value.length) return null
  const c = cur.value
  return {
    labels: bmLabels.value,
    datasets: [
      { label: `Маржа, ${currency.value}`, data: byBm.value.map(r => num(r[`margin_${c}`])), backgroundColor: accent.value },
      { label: 'Прошлый год', data: byBm.value.map(r => num(r[`margin_${c}_py`])), backgroundColor: withAlpha(accent.value, 0.35) },
    ],
  }
})
const bmPctData = computed(() => {
  if (!byBm.value.length) return null
  return {
    labels: bmLabels.value,
    datasets: [
      { label: 'Маржинальность, %', data: byBm.value.map(r => num(r.margin_pct)), backgroundColor: info.value },
      { label: 'Прошлый год, %', data: byBm.value.map(r => num(r.margin_pct_py)), backgroundColor: withAlpha(info.value, 0.35) },
    ],
  }
})

/** Донат — только положительная маржа; отрицательные группы перечислены под графиком. */
const positiveLevels = computed(() => byLevel01.value.filter(r => Number(r[`margin_${cur.value}`]) > 0))
const negativeLevels = computed(() =>
  byLevel01.value.filter(r => Number(r[`margin_${cur.value}`]) < 0).map(r => r.label || '(не указано)'))
const donutData = computed(() => {
  if (!positiveLevels.value.length) return null
  return {
    labels: positiveLevels.value.map(r => r.label || '(не указано)'),
    datasets: [{ data: positiveLevels.value.map(r => Number(r[`margin_${cur.value}`])),
                 backgroundColor: positiveLevels.value.map((_, i) => color(i)), borderWidth: 0 }],
  }
})

const unitCostDynData = computed(() => {
  if (!months.value.length) return null
  const c = cur.value
  return {
    labels: monthLabels.value,
    datasets: [
      { label: `Себестоимость штуки, ${currency.value}`, data: months.value.map(m => num(m[`unit_cost_${c}`])),
        borderColor: accent.value, backgroundColor: accent.value, tension: 0.25 },
      { label: 'Прошлый год', data: months.value.map(m => num(m[`unit_cost_${c}_py`])),
        borderColor: withAlpha(accent.value, 0.5), backgroundColor: withAlpha(accent.value, 0.5), tension: 0.25, borderDash: [4, 4] },
      { label: `Сырьё на штуку, ${currency.value}`, data: months.value.map(m => num(m[`unit_raw_${c}`])),
        borderColor: info.value, backgroundColor: info.value, tension: 0.25 },
    ],
  }
})

const unitCostDevData = computed(() => {
  if (!months.value.length) return null
  const vals = months.value.map(m => num(m[`unit_cost_dev_pct_${cur.value}_py`]))
  return {
    labels: monthLabels.value,
    datasets: [{ label: 'Прирост себестоимости штуки к прошлому году, %', data: vals,
                 backgroundColor: vals.map(v => (v !== null && v > 0 ? neg.value : pos.value)) }],
  }
})

/** Водопад на плавающих столбцах: каждая строка матрицы — отрезок от
 * накопленного значения до накопленного + вклад; последний столбец — итог. */
function waterfall(devKey: string) {
  const rows = matrix.value.filter(r => isNum(r[devKey]))
  if (!rows.length) return null
  const labels: string[] = [], data: [number, number][] = [], colors: string[] = []
  let acc = 0
  for (const r of rows) {
    const d = Number(r[devKey])
    labels.push(r.label || '(не указано)')
    data.push([acc, acc + d])
    colors.push(d >= 0 ? pos.value : neg.value)
    acc += d
  }
  labels.push('Итого')
  data.push([0, acc])
  colors.push(accent.value)
  return { labels, datasets: [{ label: 'Изменение маржи', data, backgroundColor: colors, borderSkipped: false }] }
}
const waterfallPm = computed(() => waterfall(`margin_dev_${cur.value}_pm`))
const waterfallPy = computed(() => waterfall(`margin_dev_${cur.value}_py`))

// ── Матрица ─────────────────────────────────────────────────────────────────

type Col = { key: string; label: string; title?: string; text?: boolean; fmt: (r: any) => string; cls?: (r: any) => string }
const matrixColumns = computed<Col[]>(() => {
  const c = cur.value, C = currency.value
  const devCls = (v: any, higherIsBetter = true) =>
    !isNum(v) || Number(v) === 0 ? '' : (Number(v) > 0) === higherIsBetter ? 'pos' : 'neg'
  if (matrixView.value === 'margin') {
    return [
      { key: 'vol', label: 'Выпуск, шт', fmt: r => fmtInt(r.vol) },
      { key: 'unit_cost', label: `С/с шт, ${C}`, fmt: r => fmtMoney(r[`unit_cost_${c}`]) },
      { key: 'cost', label: `С/с выпуска, ${C}`, fmt: r => fmtInt(r[`cost_${c}`]) },
      { key: 'rev', label: `Выпуск в ценах, ${C}`, title: 'в отпускных ценах', fmt: r => fmtInt(r[`rev_${c}`]) },
      { key: 'margin', label: `Маржа, ${C}`, fmt: r => fmtInt(r[`margin_${c}`]) },
      { key: 'margin_py', label: 'Маржа пред. год', fmt: r => fmtInt(r[`margin_${c}_py`]) },
      { key: 'margin_pct', label: 'Маржин., %', fmt: r => fmtPct(r.margin_pct, 1) },
      { key: 'dev_pm', label: 'Δ к пред. мес, пп', fmt: r => fmtPp(r.margin_pct_dev_pm), cls: r => devCls(r.margin_pct_dev_pm) },
      { key: 'dev_py', label: 'Δ к пред. году, пп', fmt: r => fmtPp(r.margin_pct_dev_py), cls: r => devCls(r.margin_pct_dev_py) },
      { key: 'target', label: 'Норма, %', title: 'норма маржинальности по Level 01', fmt: r => fmtPct(r.target_pct, 0),
        cls: r => (r.below_target ? 'neg' : '') },
    ]
  }
  if (matrixView.value === 'fact') {
    // Только строки с годным фактом: объём и суммы здесь — по ним, а не по всему выпуску.
    return [
      { key: 'cov', label: 'Факт годен, % выпуска', fmt: r => fmtPct(r.fact_coverage_pct, 0),
        cls: r => (isNum(r.fact_coverage_pct) && r.fact_coverage_pct < 100 ? 'neg' : '') },
      { key: 'unit_norm', label: `С/с шт норматив, ${C}`, fmt: r => fmtMoney(r[`unit_cost_norm_${c}`]) },
      { key: 'unit_fact', label: `С/с шт факт, ${C}`, fmt: r => fmtMoney(r[`unit_cost_fact_${c}`]) },
      { key: 'unit_dev', label: 'Δ с/с, %', title: 'факт / норматив − 1', fmt: r => fmtSignedPct(r[`unit_cost_fact_dev_pct_${c}`]),
        cls: r => devCls(r[`unit_cost_fact_dev_pct_${c}`], false) },
      { key: 'margin_norm', label: `Маржа норматив, ${C}`, fmt: r => fmtInt(r[`margin_norm_${c}`]) },
      { key: 'margin_fact', label: `Маржа факт, ${C}`, fmt: r => fmtInt(r[`margin_fact_${c}`]) },
      { key: 'margin_dev', label: `Δ маржи, ${C}`, fmt: r => fmtSigned(r[`fact_dev_${c}`], x => nf0.format(x)) || '—',
        cls: r => devCls(r[`fact_dev_${c}`]) },
      { key: 'pct_norm', label: 'Маржин. норматив, %', fmt: r => fmtPct(r.margin_pct_norm, 1) },
      { key: 'pct_fact', label: 'Маржин. факт, %', fmt: r => fmtPct(r.margin_pct_fact, 1) },
      { key: 'pct_dev', label: 'Δ, пп', fmt: r => fmtPp(r.fact_dev_pp), cls: r => devCls(r.fact_dev_pp) },
    ]
  }
  return [
    { key: 'vol', label: 'Выпуск, шт', fmt: r => fmtInt(r.vol) },
    { key: 'unit_cost', label: `С/с шт, ${C}`, fmt: r => fmtMoney(r[`unit_cost_${c}`]) },
    { key: 'unit_cost_py', label: 'С/с шт пред. год', fmt: r => fmtMoney(r[`unit_cost_${c}_py`]) },
    { key: 'unit_cost_dev', label: 'Δ, %', fmt: r => fmtSignedPct(r[`unit_cost_dev_pct_${c}_py`]),
      cls: r => devCls(r[`unit_cost_dev_pct_${c}_py`], false) },
    { key: 'unit_raw', label: `Сырьё/шт, ${C}`, fmt: r => fmtMoney(r[`unit_raw_${c}`]) },
    { key: 'unit_raw_py', label: 'Сырьё/шт пред. год', fmt: r => fmtMoney(r[`unit_raw_${c}_py`]) },
    { key: 'unit_raw_dev', label: 'Δ, %', fmt: r => fmtSignedPct(r[`unit_raw_dev_pct_${c}_py`]),
      cls: r => devCls(r[`unit_raw_dev_pct_${c}_py`], false) },
    { key: 'ops', label: `Операции/шт, ${C}`, title: 'себестоимость штуки минус сырьё на штуку',
      fmt: r => (isNum(r[`unit_cost_${c}`]) && isNum(r[`unit_raw_${c}`]) ? fmtMoney(r[`unit_cost_${c}`] - r[`unit_raw_${c}`]) : '—') },
    { key: 'min', label: 'Мин. пошива/шт', fmt: r => (isNum(r.min_per_unit) ? nf2.format(r.min_per_unit) : '—') },
  ]
})

// ── Настройки графиков ──────────────────────────────────────────────────────

const baseOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: { position: 'bottom' as const, labels: { color: ink.value, boxWidth: 10, font: { size: 11 } } },
  },
}))
const axis = (title?: string) => ({
  ticks: { color: ink.value }, grid: { color: grid.value },
  ...(title ? { title: { display: true, text: title } } : {}),
})
const monthClick = {
  interaction: { mode: 'index' as const, intersect: false },
  onClick: (_e: any, elements: any[]) => {
    const idx = elements?.[0]?.index
    if (idx !== undefined && idx !== null) crossFilterMonth(idx)
  },
  onHover: pointerOnHover,
}

const marginDynOptions = computed(() => ({
  ...baseOptions.value, ...monthClick,
  scales: {
    x: axis(),
    y: axis(currency.value),
    y2: { position: 'right' as const, ticks: { color: ink.value }, grid: { drawOnChartArea: false }, title: { display: true, text: '%' } },
  },
}))
const pctDynOptions = computed(() => ({ ...baseOptions.value, ...monthClick, scales: { x: axis(), y: axis('%') } }))
const unitCostOptions = computed(() => ({ ...baseOptions.value, ...monthClick, scales: { x: axis(), y: axis(currency.value) } }))
const devOptions = computed(() => ({ ...baseOptions.value, ...monthClick, scales: { x: axis(), y: axis('%') } }))

const bmOptions = (unit: string) => ({
  ...baseOptions.value,
  onClick: (_e: any, elements: any[]) => {
    const idx = elements?.[0]?.index
    if (idx !== undefined && idx !== null) toggleFilter('brand_manager', byBm.value[idx]?.label)
  },
  onHover: pointerOnHover,
  scales: { x: { ticks: { color: ink.value, maxRotation: 45, minRotation: 30 }, grid: { display: false } }, y: axis(unit) },
})

const donutOptions = computed(() => ({
  ...baseOptions.value,
  cutout: '55%',
  onClick: (_e: any, elements: any[]) => {
    const idx = elements?.[0]?.index
    if (idx !== undefined && idx !== null) toggleFilter('level01', positiveLevels.value[idx]?.label)
  },
  onHover: pointerOnHover,
  plugins: {
    ...baseOptions.value.plugins,
    legend: { position: 'right' as const, labels: { color: ink.value, boxWidth: 10, font: { size: 11 } } },
    tooltip: {
      callbacks: {
        label: (ctx: any) => {
          const v = Number(ctx.parsed) || 0
          const total = (ctx.dataset?.data || []).reduce((s: number, x: any) => s + Number(x || 0), 0)
          return ` ${ctx.label}: ${nf0.format(v)}${total ? ` · ${nf1.format((100 * v) / total)}%` : ''}`
        },
      },
    },
  },
}))

const waterfallOptions = computed(() => ({
  ...baseOptions.value,
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (ctx: any) => {
          const [a, b] = ctx.raw as [number, number]
          return ` ${ctx.label === 'Итого' ? 'итого' : 'вклад'}: ${fmtSigned(b - a, x => nf0.format(x))}`
        },
      },
    },
  },
  scales: {
    x: { ticks: { color: ink.value, maxRotation: 45, minRotation: 30 }, grid: { display: false } },
    y: axis(currency.value),
  },
}))
</script>

<style scoped>
/* Только токены дизайн-системы. */
.page-margin { display: flex; flex-direction: column; gap: var(--sp-5); }

.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-4); flex-wrap: wrap; }
.page-title { font-size: var(--fs-xl); font-weight: var(--fw-semibold); margin: 0; }
.page-sub { margin: var(--sp-1) 0 0; font-size: var(--fs-xs); color: var(--text-muted); }
.dot { margin: 0 var(--sp-2); opacity: .5; }
.warn { color: var(--neg); }
.header-actions { display: flex; gap: var(--sp-3); align-items: center; }
.currency-switch, .view-switch { display: flex; gap: var(--sp-1); }

.basis { margin: 0; font-size: var(--fs-2xs); color: var(--text-muted); }

.filters {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
  gap: var(--sp-3); align-items: end;
}
.filter-item { display: flex; flex-direction: column; gap: var(--sp-1); }
.filter-item label { font-size: var(--fs-2xs); color: var(--text-muted); display: flex; gap: var(--sp-1); align-items: baseline; }
.trunc { color: var(--neg); font-size: var(--fs-2xs); }
.reset { align-self: end; }
.error { color: var(--neg); font-size: var(--fs-sm); }

.section-title { font-size: var(--fs-lg); font-weight: var(--fw-medium); margin: var(--sp-2) 0 0; }

/* Шесть плиток в ряд на 1440px; при 210px шестая уезжала на отдельную строку. */
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
.cmp { list-style: none; margin: var(--sp-1) 0 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
.cmp-line { display: grid; grid-template-columns: 1fr auto auto; gap: var(--sp-2); font-size: var(--fs-2xs); align-items: baseline; }
.cmp-label { color: var(--text-muted); }
.cmp-value, .cmp-delta { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.cmp-delta { min-width: 5.5em; text-align: right; }
.pos { color: var(--pos); }
.neg { color: var(--neg); }

.charts { display: grid; grid-template-columns: repeat(auto-fit, minmax(360px, 1fr)); gap: var(--sp-4); }
.card {
  border: 1px solid var(--border); border-radius: var(--rd-3);
  background: var(--bg-surface); padding: var(--sp-4);
  display: flex; flex-direction: column; gap: var(--sp-2); min-width: 0;
}
.card-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-3); flex-wrap: wrap; }
.card-title { font-size: var(--fs-md); font-weight: var(--fw-medium); margin: 0; }
.card-note { font-size: var(--fs-2xs); color: var(--text-muted); margin: 0; }
.hint { font-size: var(--fs-2xs); color: var(--text-muted); font-style: italic; }

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
.chart-box-tall { height: 320px; }
.chart-box :deep(canvas) { max-height: 100%; }

/* Матрица: hairline-сетка токеном, числа моно, без внешней рамки — таблица уже в карточке. */
.table-wrap { overflow-x: auto; }
.matrix { width: 100%; border-collapse: collapse; font-size: var(--fs-xs); }
.matrix th, .matrix td { padding: var(--sp-1) var(--sp-2); border-bottom: 1px solid var(--border); white-space: nowrap; }
.matrix th.sortable { cursor: pointer; user-select: none; }
.matrix th.sortable:hover { color: var(--accent); }
.matrix th { text-align: right; font-weight: var(--fw-medium); color: var(--text-muted); font-size: var(--fs-2xs); }
.matrix th.col-label, .matrix td.col-label { text-align: left; }
.matrix td.num { text-align: right; font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.matrix tbody tr.drillable { cursor: pointer; }
.matrix tbody tr.drillable:hover { background: var(--bg-surface-2); }
.matrix tbody tr.below-target td.col-label .row-label { color: var(--neg); }
.row-label { display: block; }
.row-name { display: block; font-size: var(--fs-2xs); color: var(--text-muted); }
.empty-row { text-align: center; color: var(--text-muted); padding: var(--sp-4) !important; }

.compare-actions { display: flex; gap: var(--sp-2); flex-shrink: 0; }
.superset-frame {
  width: 100%; height: 1600px; border: 1px solid var(--border);
  border-radius: var(--rd-3); background: var(--bg-surface-2);
}
</style>
