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
                             :placeholder="`Все · ${f.label.toLowerCase()}`" @change="onFilterChange" />
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

          <!-- P11: верхний свод — реплика листа 1 деки «Бюджет ФОТ 2026»
               (KPI-лента + панель численности + таблица стран + карточки
               стран); разрезы вкладки — таблицами ниже. -->
          <template v-if="isP11">
            <section class="slide" aria-label="Свод по группе">
              <header class="slide-hdr">
                <h2 class="slide-title">ГК «МАРК ФОРМЭЛЬ»</h2>
                <span class="slide-subtitle">Бюджет ФОТ {{ yearPlan }} — направления и подразделения</span>
                <span class="slide-year" aria-hidden="true">{{ yearPlan }}</span>
                <span class="slide-unit">млн {{ currency }}</span>
              </header>

              <!-- KPI-лента: 6 показателей группы + панель численности (P12) -->
              <div class="ribbon">
                <div class="rb">
                  <span class="rb-label">Факт, год {{ yearPrev }}</span>
                  <span class="rb-value">{{ fmt1(kpiNum('fot_fact_2025')) }}<span class="rb-unit"> млн {{ currency }}</span></span>
                </div>
                <div class="rb">
                  <span class="rb-label">Доля ФОТ в выручке {{ yearPrev }}</span>
                  <span class="rb-value">{{ fmt1(kpiNum('fot_share_2025')) }}<span class="rb-unit"> %</span></span>
                </div>
                <div class="rb rb-big">
                  <span class="rb-label">Ожидание, год {{ yearPlan }}</span>
                  <span class="rb-value">{{ fmt1(kpiNum('fot_expect_2026')) }}<span class="rb-unit"> млн {{ currency }}</span></span>
                </div>
                <div class="rb">
                  <span class="rb-label">Динамика ФОТ к {{ yearPrev }}</span>
                  <span class="rb-value" :class="signCf(kpiNum('fot_dev_year'))">{{ fmtSigned(kpiNum('fot_dev_year')) }}<span class="rb-unit"> %</span></span>
                </div>
                <div class="rb rb-big">
                  <span class="rb-label">Доля ФОТ в выручке {{ yearPlan }}</span>
                  <span class="rb-value">{{ fmt1(kpiNum('fot_share_2026')) }}<span class="rb-unit"> %</span></span>
                  <span class="rb-sub" :class="signCf(kpiNum('fot_share_delta_pp'))">{{ fmtSigned(kpiNum('fot_share_delta_pp')) }} п.п. к факту {{ yearPrev }}</span>
                </div>
                <div class="rb">
                  <span class="rb-label">Динамика выручки к {{ yearPrev }}</span>
                  <span class="rb-value">{{ fmtSigned(kpiNum('revenue_dev_year')) }}<span class="rb-unit"> %</span></span>
                </div>
                <div v-if="headcount" class="rb-hc">
                  <div class="hc">
                    <span class="hc-label">ПЧ {{ yearPlan }}</span>
                    <span class="hc-value">{{ fmt1(headcount.plan_approved) }}</span>
                  </div>
                  <div class="hc">
                    <span class="hc-label">ФЧ 1ПГ{{ yr2(yearPrev) }}</span>
                    <span class="hc-value">{{ fmt1(headcount.fact_2025) }}</span>
                  </div>
                  <div class="hc">
                    <span class="hc-label">ФЧ 1ПГ{{ yr2(yearPlan) }}</span>
                    <span class="hc-value">{{ fmt1(headcount.fact_2026) }}</span>
                  </div>
                  <div class="hc">
                    <span class="hc-label">Δ план</span>
                    <span class="hc-value" :class="signCf(headcount.var_plan)">{{ fmtSigned(headcount.var_plan) }}</span>
                  </div>
                  <div class="hc">
                    <span class="hc-label">Δ {{ yr2(yearPrev) }}</span>
                    <span class="hc-value" :class="signCf(headcount.var_2025)">{{ fmtSigned(headcount.var_2025) }}</span>
                  </div>
                </div>
              </div>

              <!-- Таблица «страна × полугодия»: светофор «факт/тактика против
                   бюджета» и «ожидание против факта 2025», итог — тёмной строкой -->
              <section v-if="slideTable" class="card">
                <div class="card-head">
                  <h3 class="card-title">{{ slideTable.label }}</h3>
                  <button class="btn btn-ghost btn-sm" type="button"
                          title="Скачать CSV: разделитель «;», BOM — Excel откроет без мастера импорта"
                          @click="exportCsv(slideTable)">
                    <Icon name="lucide:download" /> CSV
                  </button>
                </div>
                <div class="table-wrap">
                  <table class="stable">
                    <thead>
                      <tr>
                        <th v-for="col in slideTable.columns" :key="col.key" :class="{ 'col-name': col.key === 'label' }">
                          {{ col.label }}<span v-if="col.unit && col.key !== 'label'" class="th-unit">, {{ col.unit }}</span>
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="(row, i) in (slideTable.rows || [])" :key="i" :class="{ total: row.total }">
                        <td v-for="col in slideTable.columns" :key="col.key"
                            :class="[col.key === 'label' ? 'col-name' : 'num', row.total ? slideCmpDk(row, col.key) : slideCmp(row, col.key)]">
                          {{ cellText(slideTable, row, col) }}
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </section>

              <!-- Карточки стран «Ожидание 2026 к факту 2025» -->
              <template v-if="countryCards.length">
                <h3 class="grid-caption">Ожидание {{ yearPlan }} к факту {{ yearPrev }}, ФОТ</h3>
                <section class="cc-grid">
                  <article v-for="c in countryCards" :key="c.cc" class="ccard">
                    <span class="cc-code">{{ c.cc }}</span>
                    <span class="cc-dyncap">Динамика, п.п. ({{ yearPlan }}/{{ yearPrev }}):</span>
                    <span class="cc-badge" :class="signCf(c.share_delta_pp)">{{ fmtSigned(c.share_delta_pp, 2) }}</span>
                    <span class="cc-big">{{ fmt1(c.exp26) }} млн</span>
                    <span class="cc-delta" :class="signCf(c.dev_year)">{{ fmtSigned(c.dev_year) }}% к {{ yearPrev }} ({{ fmtSigned(c.dev_abs) }} млн)</span>
                    <div class="cc-sep" role="presentation" />
                    <p class="cc-line">Доля в ФОТ группы: <b>{{ fmt1(c.fot_share) }}%</b></p>
                    <p class="cc-line">Доля ФОТ в выручке гр. ({{ yearPlan }}): <b>{{ fmt1(c.rev_share_2026) }}%</b></p>
                    <p class="cc-line">Доля ФОТ в выручке гр. ({{ yearPrev }}): <b>{{ fmt1(c.rev_share_2025) }}%</b></p>
                    <div class="cc-sep cc-sep-soft" role="presentation" />
                    <p class="cc-hc">
                      ПЧ: <b>{{ fmt1(c.plan_approved) }}</b>&nbsp;&nbsp;ФЧ 1ПГ {{ yearPlan }}:
                      <b :class="cmpCf(c.fact_h1_2026, c.plan_approved)">{{ fmt1(c.fact_h1_2026) }}</b><br>
                      Откл. (Ф-П): <b :class="signCf(c.hc_var)">{{ fmtSigned(c.hc_var) }}</b>
                    </p>
                  </article>
                </section>
              </template>
            </section>
          </template>

          <!-- P14 «ФОТ по направлениям бизнеса»: лист 4 деки — таблица
               «Направление × периоды» с донатом структуры, ряд мини-баров
               «доля ФОТ в выручке» и карточки направлений с ПЧ/ФЧ. -->
          <template v-if="isP14">
            <section class="slide" aria-label="ФОТ по направлениям бизнеса">
              <header class="slide-hdr">
                <h2 class="slide-title">ГК «МАРК ФОРМЭЛЬ»</h2>
                <span class="slide-subtitle">{{ slideCountryLabel }} — ФОТ по направлениям бизнеса: свод, структура и динамика</span>
                <span class="slide-year p14-geo" aria-hidden="true">{{ slideCountryCc }}</span>
                <span v-if="headcount" class="slide-note">
                  Числ. (ПЧ/ФЧ 1ПГ{{ yr2(yearPlan) }}):
                  <b>{{ fmt1(headcount.plan_approved) }}/{{ fmt1(headcount.fact_2026) }}</b>,
                  {{ fmt1(headcount.staffing) }}% укомпл.
                </span>
              </header>

              <!-- Таблица «Направление × периоды» + донат «Доля ФОТ» -->
              <div class="p14-top">
                <section v-if="slideTable14" class="card p14-table">
                  <div class="card-head">
                    <h3 class="card-title">{{ slideTable14.label }}, тыс {{ currency }}</h3>
                    <button class="btn btn-ghost btn-sm" type="button"
                            title="Скачать CSV: разделитель «;», BOM — Excel откроет без мастера импорта"
                            @click="exportCsv(slideTable14)">
                      <Icon name="lucide:download" /> CSV
                    </button>
                  </div>
                  <div class="table-wrap">
                    <table class="stable p14-stable">
                      <thead>
                        <tr>
                          <th v-for="col in slideTable14.columns" :key="col.key"
                              :class="{ 'col-name': col.key === 'label' }">
                            {{ col.label }}
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr v-for="(row, i) in (slideTable14.rows || [])" :key="i" :class="{ total: row.total }">
                          <td v-for="col in slideTable14.columns" :key="col.key"
                              :class="col.key === 'label' ? 'col-name' : 'num'">
                            {{ cellText(slideTable14, row, col) }}
                          </td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                </section>

                <section v-if="pieSegs.length" class="card p14-pie">
                  <h3 class="card-title">ДОЛЯ ФОТ ПО НАПРАВЛЕНИЯМ, ОЖИДАНИЕ {{ yearPlan }}</h3>
                  <svg class="p14-pie-svg" viewBox="0 0 340 182" role="img"
                       :aria-label="`Структура ФОТ ожидания ${yearPlan} по направлениям`">
                    <g class="p14-pie-segs">
                      <path v-for="(s, i) in pieSegs" :key="i" :d="s.path" :fill="s.color" />
                    </g>
                    <g fill="#FFFFFF" font-size="10.7" text-anchor="middle" class="p14-pie-num">
                      <text v-for="(s, i) in pieSegs" :key="i" v-show="s.labeled" :x="s.lx" :y="s.ly">
                        {{ fmt1(s.pct) }}%
                      </text>
                    </g>
                    <g class="p14-pie-legend" font-size="10">
                      <g v-for="(s, i) in pieSegs" :key="i" :transform="`translate(196, ${18 + i * 20})`">
                        <rect width="6.5" height="6.5" y="-6" :fill="s.color" />
                        <text x="11" dy="0">{{ s.label }}</text>
                      </g>
                    </g>
                  </svg>
                </section>
              </div>

              <!-- Ряд мини-чартов «Доля ФОТ в выручке по направлениям, %» -->
              <section v-if="directions.length" class="card">
                <h3 class="card-title">ДОЛЯ ФОТ В ВЫРУЧКЕ ПО НАПРАВЛЕНИЯМ, %</h3>
                <div class="p14-bars">
                  <div v-for="d in directions" :key="d.label" class="p14-bar">
                    <span class="p14-bar-title" :title="d.label">{{ d.label }}</span>
                    <svg viewBox="0 0 227 82" role="img" aria-hidden="true">
                      <g v-for="(b, i) in barGeom(d)" :key="i">
                        <rect :x="18 + i * 49.5" :y="60 - b.h" width="42" :height="b.h" :fill="b.color" />
                        <text :x="39 + i * 49.5" :y="b.v === null ? 57 : 60 - b.h - 5"
                              text-anchor="middle" class="p14-bar-val">{{ b.v === null ? '—' : fmt1(b.v) + '%' }}</text>
                        <text :x="39 + i * 49.5" y="74" text-anchor="middle" class="p14-bar-cap">{{ b.label }}</text>
                      </g>
                    </svg>
                  </div>
                </div>
              </section>

              <!-- Карточки направлений: три числа к факту прошлого года -->
              <template v-if="directions.length">
                <h3 class="grid-caption">Направления: {{ yearPlan }} к факту {{ yearPrev }}</h3>
                <section class="p14-cards">
                  <article v-for="d in directions" :key="d.label" class="p14card" :style="{ '--dcolor': d.color }">
                    <div class="p14c-block">
                      <span class="p14c-label">1 ПГ {{ yearPlan }}</span>
                      <span class="p14c-badge cc-badge" :class="signCf(d.pp_h1)"
                            :title="`Δ доли ФОТ в выручке к факту 1ПГ${yr2(yearPrev)}`">{{ fmtSigned(d.pp_h1, 2) }}</span>
                      <span class="p14c-value">{{ fmt0(d.f26_h1) }}<span class="p14c-unit"> тыс {{ currency }}</span></span>
                      <span class="p14c-delta" :class="signCf(d.dev_h1_pct)">{{ fmtSigned(d.dev_h1_pct) }}% к факту {{ yearPrev }} ({{ fmtSigned(d.dev_h1_abs) }})</span>
                    </div>
                    <div class="p14c-block">
                      <span class="p14c-label">ТАКТИКА 2 ПГ {{ yearPlan }}</span>
                      <span class="p14c-badge cc-badge" :class="signCf(d.pp_h2)"
                            :title="`Δ доли ФОТ в выручке к факту 2ПГ${yr2(yearPrev)}`">{{ fmtSigned(d.pp_h2, 2) }}</span>
                      <span class="p14c-value">{{ fmt0(d.t26_h2) }}<span class="p14c-unit"> тыс {{ currency }}</span></span>
                      <span class="p14c-delta" :class="signCf(d.dev_h2_pct)">{{ fmtSigned(d.dev_h2_pct) }}% к факту {{ yearPrev }} ({{ fmtSigned(d.dev_h2_abs) }})</span>
                    </div>
                    <div class="p14c-block">
                      <span class="p14c-label">ОЖИДАНИЕ, ГОД {{ yearPlan }}</span>
                      <span class="p14c-badge cc-badge" :class="signCf(d.pp_year)"
                            :title="`Δ доли ФОТ в выручке: ожидание ${yearPlan} к факту ${yearPrev}`">{{ fmtSigned(d.pp_year, 2) }}</span>
                      <span class="p14c-value">{{ fmt0(d.exp26) }}<span class="p14c-unit"> тыс {{ currency }}</span></span>
                      <span class="p14c-delta" :class="signCf(d.dev_year_pct)">{{ fmtSigned(d.dev_year_pct) }}% к факту {{ yearPrev }} ({{ fmtSigned(d.dev_year_abs) }})</span>
                    </div>
                    <div class="cc-sep" role="presentation" />
                    <p class="cc-hc">
                      ПЧ/ФЧ 1ПГ {{ yearPlan }}:
                      <b>{{ fmt1(d.hc_plan) }}</b>/<b :class="cmpCf(d.hc_fact, d.hc_plan)">{{ fmt1(d.hc_fact) }}</b>,
                      {{ fmt0(d.hc_staffing) }}% укомпл.
                    </p>
                    <p class="cc-hc">Откл. ФЧ к 1ПГ{{ yr2(yearPrev) }}: <b :class="signCf(d.hc_dev)">{{ fmtSigned(d.hc_dev) }}</b></p>
                  </article>
                </section>
              </template>
            </section>
          </template>

          <!-- P15 «Отделы: свод»: лист 7 деки — таблица «Отдел × периоды»,
               карточки «требующих внимания» и баннер «Тенденции и прогноз». -->
          <template v-if="isP15">
            <section class="slide" aria-label="Отделы: свод">
              <header class="slide-hdr">
                <h2 class="slide-title">ГК «МАРК ФОРМЭЛЬ»</h2>
                <span class="slide-subtitle">{{ slideCountryLabel }} — Отделы: свод по {{ deptCards.length }} подразделениям</span>
                <span v-if="headcount" class="slide-note">
                  Числ. направления (ПЧ/ФЧ 1ПГ{{ yr2(yearPlan) }}):
                  <b>{{ fmt1(headcount.plan_approved) }}/{{ fmt1(headcount.fact_2026) }}</b>,
                  {{ fmt0(headcount.staffing) }}% укомпл.
                </span>
                <span class="slide-year p15-badge" aria-hidden="true">
                  <span>{{ slideCountryCc }}</span><span>ОТДЕЛЫ</span>
                </span>
              </header>

              <!-- Таблица «Отдел × периоды»: итог — тёмной строкой, без светофора -->
              <section v-if="slideTable15" class="card">
                <div class="card-head">
                  <h3 class="card-title">{{ slideTable15.label }}, тыс {{ currency }}</h3>
                  <button class="btn btn-ghost btn-sm" type="button"
                          title="Скачать CSV: разделитель «;», BOM — Excel откроет без мастера импорта"
                          @click="exportCsv(slideTable15)">
                    <Icon name="lucide:download" /> CSV
                  </button>
                </div>
                <div class="table-wrap">
                  <table class="stable p15-stable">
                    <thead>
                      <tr>
                        <th v-for="col in slideTable15.columns" :key="col.key"
                            :class="{ 'col-name': col.key === 'label' }">
                          {{ col.label }}
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="(row, i) in (slideTable15.rows || [])" :key="i" :class="{ total: row.total }">
                        <td v-for="col in slideTable15.columns" :key="col.key"
                            :class="col.key === 'label' ? 'col-name' : 'num'">
                          {{ cellText(slideTable15, row, col) }}
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>
                <p class="card-note">
                  тыс. {{ currency }}. «Ожидание 2026» = факт 1ПГ + тактика 2ПГ (формула витрины);
                  CFO-свод деки даёт немного другие значения итога.
                </p>
              </section>

              <!-- Карточки «Отделы, требующие внимания» -->
              <template v-if="deptAttention.length">
                <h3 class="grid-caption">ОТДЕЛЫ, ТРЕБУЮЩИЕ ВНИМАНИЯ: ИСПОЛНЕНИЕ БЮДЖЕТА ФОТ И ДИНАМИКА К {{ yearPrev }}</h3>
                <section class="dpc-grid dp15-grid">
                  <article v-for="d in deptAttention" :key="d.label" class="dpcard">
                    <div class="dpc-title" :title="d.label">{{ d.label }}</div>
                    <div class="dpc-block">
                      <span class="dpc-label">1 ПГ {{ yearPlan }}</span>
                      <span class="dpc-ppwrap">
                        <span class="dpc-pp" :class="signCf(d.pp_h1)"
                              :title="`Δ доли ФОТ в выручке: факт 1ПГ${yr2(yearPlan)} к факту 1ПГ${yr2(yearPrev)}`">
                          <span class="pp-val">{{ ppText(d.pp_h1) }}</span><span class="pp-unit">п.п.</span>
                        </span>
                        <span class="dpc-ppcap">к факту 25</span>
                      </span>
                      <span class="dpc-value">{{ fmt0(d.f26_h1) }}<span class="dpc-unit"> тыс {{ currency }}</span></span>
                      <span class="dpc-delta" :class="ndCf(d.dev_h1_pct)">{{ deptDelta(d, 'h1', false) }}</span>
                    </div>
                    <div class="dpc-block">
                      <span class="dpc-label">ТАКТИКА 2 ПГ {{ yearPlan }}</span>
                      <span class="dpc-ppwrap">
                        <span class="dpc-pp" :class="signCf(d.pp_h2)"
                              :title="`Δ доли ФОТ в выручке: тактика 2ПГ${yr2(yearPlan)} к факту 2ПГ${yr2(yearPrev)}`">
                          <span class="pp-val">{{ ppText(d.pp_h2) }}</span><span class="pp-unit">п.п.</span>
                        </span>
                        <span class="dpc-ppcap">к факту 2П25</span>
                      </span>
                      <span class="dpc-value">{{ fmt0(d.t26_h2) }}<span class="dpc-unit"> тыс {{ currency }}</span></span>
                      <span class="dpc-delta" :class="ndCf(d.dev_h2_pct)">{{ deptDelta(d, 'h2', false) }}</span>
                    </div>
                    <div class="dpc-block">
                      <span class="dpc-label">ОЖИДАНИЕ, ГОД {{ yearPlan }}</span>
                      <span class="dpc-ppwrap">
                        <span class="dpc-pp pp3" :class="signCf(d.pp_year)"
                              :title="`Δ доли ФОТ в выручке: ожидание ${yearPlan} к факту ${yearPrev}`">
                          <span class="pp-val">{{ ppText(d.pp_year) }}</span><span class="pp-unit">п.п.</span>
                        </span>
                        <span class="dpc-ppcap">Год/факт25</span>
                      </span>
                      <span class="dpc-value">{{ fmt0(d.exp26) }}<span class="dpc-unit"> тыс {{ currency }}</span></span>
                      <span class="dpc-delta" :class="ndCf(d.dev_year_pct)">{{ deptDelta(d, 'year', false) }}</span>
                    </div>
                    <div class="cc-sep" role="presentation" />
                    <p class="cc-hc" v-if="isNum(d.hc_fact)">
                      ПЧ/ФЧ 1ПГ {{ yearPlan }}:
                      <b>{{ fmt1(d.hc_plan) }}</b>/<b :class="cmpCf(d.hc_fact, d.hc_plan)">{{ fmt1(d.hc_fact) }}</b>,
                      {{ fmt0(d.hc_staffing) }}% укомпл.
                    </p>
                    <p class="cc-hc" v-if="isNum(d.hc_dev)">
                      Откл. ФЧ к 1ПГ{{ yr2(yearPrev) }}: <b :class="signCf(d.hc_dev)">{{ fmtSigned(d.hc_dev) }}</b>
                    </p>
                  </article>
                </section>
              </template>

              <!-- Баннер «Тенденции и прогноз»: генерируется сервером по правилам -->
              <div v-if="deptTrends.length" class="dp-banner">
                <span class="dpb-text"><b>Тенденции и прогноз: </b>{{ deptTrends.join(' ') }}</span>
              </div>
            </section>
          </template>

          <!-- P16 «Отделы: анализ по подразделениям»: лист 8 деки — карточки
               остальных отделов и баннеры «Лучшая тенденция» / «Второй приоритет». -->
          <template v-if="isP16">
            <section class="slide" aria-label="Отделы: анализ по подразделениям">
              <header class="slide-hdr">
                <h2 class="slide-title">ГК «МАРК ФОРМЭЛЬ»</h2>
                <span class="slide-subtitle">{{ slideCountryLabel }} — Отделы: анализ по подразделениям (доля ФОТ в выручке и динамика)</span>
                <span v-if="headcount" class="slide-note">
                  Числ. направления (ПЧ/ФЧ 1ПГ{{ yr2(yearPlan) }}):
                  <b>{{ fmt1(headcount.plan_approved) }}/{{ fmt1(headcount.fact_2026) }}</b>,
                  {{ fmt0(headcount.staffing) }}% укомпл.
                </span>
                <span class="slide-year p15-badge" aria-hidden="true">
                  <span>{{ slideCountryCc }}</span><span>ОТДЕЛЫ</span>
                </span>
              </header>

              <template v-if="deptRest.length">
                <section class="dpc-grid dp16-grid">
                  <article v-for="d in deptRest" :key="d.label" class="dpcard dp16">
                    <div class="dpc-title" :title="d.label">{{ d.label }}</div>
                    <div class="dpc-block">
                      <span class="dpc-label">1 ПГ {{ yearPlan }}</span>
                      <span class="dpc-ppwrap">
                        <span class="dpc-pp" :class="signCf(d.pp_h1)"
                              :title="`Δ доли ФОТ в выручке: факт 1ПГ${yr2(yearPlan)} к факту 1ПГ${yr2(yearPrev)}`">
                          <span class="pp-val">{{ ppText(d.pp_h1) }}</span><span class="pp-unit">п.п.</span>
                        </span>
                        <span class="dpc-ppcap">к факту 25</span>
                      </span>
                      <span class="dpc-value">{{ fmt0(d.f26_h1) }}<span class="dpc-unit"> тыс {{ currency }}</span></span>
                      <span class="dpc-delta" :class="ndCf(d.dev_h1_pct)">{{ deptDelta(d, 'h1', true) }}</span>
                    </div>
                    <div class="dpc-block">
                      <span class="dpc-label">ТАКТИКА 2 ПГ {{ yearPlan }}</span>
                      <span class="dpc-ppwrap">
                        <span class="dpc-pp" :class="signCf(d.pp_h2)"
                              :title="`Δ доли ФОТ в выручке: тактика 2ПГ${yr2(yearPlan)} к факту 2ПГ${yr2(yearPrev)}`">
                          <span class="pp-val">{{ ppText(d.pp_h2) }}</span><span class="pp-unit">п.п.</span>
                        </span>
                        <span class="dpc-ppcap">факту 2П25</span>
                      </span>
                      <span class="dpc-value">{{ fmt0(d.t26_h2) }}<span class="dpc-unit"> тыс {{ currency }}</span></span>
                      <span class="dpc-delta" :class="ndCf(d.dev_h2_pct)">{{ deptDelta(d, 'h2', true) }}</span>
                    </div>
                    <div class="dpc-block">
                      <span class="dpc-label">ОЖИДАНИЕ, ГОД {{ yearPlan }}</span>
                      <span class="dpc-ppwrap">
                        <span class="dpc-pp" :class="signCf(d.pp_year)"
                              :title="`Δ доли ФОТ в выручке: ожидание ${yearPlan} к факту ${yearPrev}`">
                          <span class="pp-val">{{ ppText(d.pp_year) }}</span><span class="pp-unit">п.п.</span>
                        </span>
                        <span class="dpc-ppcap">Год/факт25</span>
                      </span>
                      <span class="dpc-value">{{ fmt0(d.exp26) }}<span class="dpc-unit"> тыс {{ currency }}</span></span>
                      <span class="dpc-delta" :class="ndCf(d.dev_year_pct)">{{ deptDelta(d, 'year', true) }}</span>
                    </div>
                    <div class="cc-sep" role="presentation" />
                    <p class="cc-hc" v-if="isNum(d.hc_fact)">
                      ПЧ/ФЧ: <b>{{ fmt1(d.hc_plan) }}</b>/<b :class="cmpCf(d.hc_fact, d.hc_plan)">{{ fmt1(d.hc_fact) }}</b>,
                      {{ fmt0(d.hc_staffing) }}%
                    </p>
                    <p class="cc-hc" v-if="isNum(d.hc_dev)">
                      Откл. к 1П{{ yr2(yearPrev) }}: <b :class="signCf(d.hc_dev)">{{ fmtSigned(d.hc_dev) }}</b>
                    </p>
                  </article>
                </section>
              </template>

              <!-- Итоговые баннеры листа 8: генерируются сервером по правилам -->
              <div class="dp-banners">
                <div v-if="deptBanners?.good?.items?.length" class="dp-banner dpb-good">
                  <span class="dpb-title">{{ deptBanners.good.lead }}</span>
                  <span class="dpb-text">
                    <template v-for="(it, i) in deptBanners.good.items" :key="it.name"><b class="nm">{{ it.name }}</b> ({{ it.note }}){{ i < deptBanners.good.items.length - 1 ? ', ' : '.' }} </template>
                  </span>
                </div>
                <div v-if="deptBanners?.warn?.items?.length" class="dp-banner dpb-warn">
                  <span class="dpb-title">{{ deptBanners.warn.lead }}</span>
                  <span class="dpb-text">
                    <template v-for="(it, i) in deptBanners.warn.items" :key="it.name"><b class="nm">{{ it.name }}</b> ({{ it.note }}){{ i < deptBanners.warn.items.length - 1 ? ', ' : '. ' }} </template>{{ deptBanners.warn.tail }}
                  </span>
                </div>
              </div>
            </section>
          </template>

          <!-- P17 «Численность ГК»: свод по группе компаний -->
          <template v-if="isP17">
            <section class="slide" aria-label="Численность ГК">
              <header class="slide-hdr">
                <h2 class="slide-title">ГК «МАРК ФОРМЭЛЬ»</h2>
                <span class="slide-subtitle">Численность — свод по странам</span>
                <span class="slide-year p15-badge" aria-hidden="true">
                  <span>ГК</span><span>ЧИСЛЕННОСТЬ</span>
                </span>
              </header>

              <!-- KPI-лента: 5 карточек укомплектованности -->
              <section class="fot-kpis">
                <article v-for="c in cards" :key="c.key" class="fot-kpi">
                  <span class="fot-kpi-label">{{ c.label }}</span>
                  <span class="fot-kpi-value">{{ fmtCard(c.value) }}<span class="fot-kpi-unit">{{ c.unit === '%' ? '%' : ` ${c.unit}` }}</span></span>
                </article>
              </section>

              <!-- Таблица «Численность ГК по странам»: светофор Δ, итог — тёмной строкой -->
              <section v-if="slideTable17" class="card">
                <div class="card-head">
                  <h3 class="card-title">{{ slideTable17.label }}</h3>
                </div>
                <div class="table-wrap">
                  <table class="stable">
                    <thead>
                      <tr>
                        <th v-for="col in slideTable17.columns" :key="col.key" :class="{ 'col-name': col.key === 'label' }">
                          {{ col.label }}<span v-if="col.unit && col.key !== 'label'" class="th-unit">, {{ col.unit }}</span>
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="(row, i) in (slideTable17.rows || [])" :key="i" :class="{ total: row.total }">
                        <td v-for="col in slideTable17.columns" :key="col.key"
                            :class="[col.key === 'label' ? 'col-name' : 'num', cellDeltaClass(row, col)]">
                          {{ cellText(slideTable17, row, col) }}
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </section>

              <!-- Слайд 4.2: численность по выбранной стране -->
              <div class="p17-divider" aria-hidden="true"></div>
              <section class="slide" aria-label="Численность по стране">
                <header class="slide-hdr">
                  <h2 class="slide-title">{{ p17CountryLabel }} — численность по подразделениям</h2>
                  <span class="slide-subtitle">На конец 1 ПГ {{ yearPlan }}</span>
                  <span class="slide-year p15-badge" aria-hidden="true">
                    <span>{{ p17CountryLabel }}</span><span>ПОДРАЗДЕЛЕНИЯ</span>
                  </span>
                </header>

                <!-- Селектор страны (мультивыбор) -->
                <div class="p17-country-sel">
                  <label v-for="c in p17CountryOptions" :key="c.key" class="p17-cc-chk">
                    <input type="checkbox" :value="c.key" v-model="p17Countries" />
                    {{ c.label }}
                  </label>
                </div>

                <!-- KPI-лента страны -->
                <section class="fot-kpis">
                  <article v-for="c in p17CountryCards" :key="c.key" class="fot-kpi">
                    <span class="fot-kpi-label">{{ c.label }}</span>
                    <span class="fot-kpi-value">{{ fmtCard(c.value) }}<span class="fot-kpi-unit">{{ c.unit === '%' ? '%' : ` ${c.unit}` }}</span></span>
                  </article>
                </section>

                <!-- Таблица подразделений -->
                <section v-if="p17CountryTable" class="card">
                  <div class="card-head">
                    <h3 class="card-title">{{ p17CountryTable.label }}</h3>
                  </div>
                  <div class="table-wrap">
                    <table class="stable">
                      <thead>
                        <tr>
                          <th v-for="col in p17CountryTable.columns" :key="col.key" :class="{ 'col-name': col.key === 'label' }">
                            {{ col.label }}<span v-if="col.unit && col.key !== 'label'" class="th-unit">, {{ col.unit }}</span>
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr v-for="(row, i) in (p17CountryTable.rows || [])" :key="i" :class="{ total: row.total }">
                          <td v-for="col in p17CountryTable.columns" :key="col.key"
                              :class="[col.key === 'label' ? 'col-name' : 'num', cellDeltaClass(row, col)]">
                            {{ cellText(p17CountryTable, row, col) }}
                          </td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                </section>
              </section>
            </section>
          </template>

          <!-- P18 «Исполнение бюджета»: ФОТ vs план, 3 разреза -->
          <template v-if="isP18">
            <section class="slide" aria-label="Исполнение бюджета">
              <header class="slide-hdr">
                <h2 class="slide-title">ГК «МАРК ФОРМЭЛЬ»</h2>
                <span class="slide-subtitle">Исполнение ФОТ — факт vs тактика/бюджет</span>
                <span class="slide-year p15-badge" aria-hidden="true">
                  <span>ГК</span><span>ИСПОЛНЕНИЕ</span>
                </span>
              </header>

              <!-- KPI-лента: 5 карточек исполнения -->
              <section class="fot-kpis">
                <article v-for="c in cards" :key="c.key" class="fot-kpi">
                  <span class="fot-kpi-label">{{ c.label }}</span>
                  <span class="fot-kpi-value">{{ fmtCard(c.value) }}<span class="fot-kpi-unit">{{ c.unit === '%' ? '%' : ` ${c.unit}` }}</span></span>
                </article>
              </section>

              <!-- Переключатель разрезов -->
              <div class="p18-tabs">
                <button v-for="t in p18SubTabs" :key="t.key" class="p18-tab"
                        :class="{ active: p18SubTab === t.key }"
                        @click="p18SubTab = t.key">{{ t.label }}</button>
              </div>

              <!-- Таблица выбранного разреза: светофор Δ, без итога -->
              <section v-if="p18ActiveTable" class="card">
                <div class="card-head">
                  <h3 class="card-title">{{ p18ActiveTable.label }}</h3>
                </div>
                <div class="table-wrap">
                  <table class="stable">
                    <thead>
                      <tr>
                        <th v-for="col in p18ActiveTable.columns" :key="col.key" :class="{ 'col-name': col.key === 'label' }">
                          {{ col.label }}<span v-if="col.unit && col.key !== 'label'" class="th-unit">, {{ col.unit }}</span>
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="(row, i) in (p18ActiveTable.rows || [])" :key="i">
                        <td v-for="col in p18ActiveTable.columns" :key="col.key"
                            :class="[col.key === 'label' ? 'col-name' : 'num', col.key.endsWith('_color') ? undefined : cellDeltaClass(row, col)]">
                          {{ cellText(p18ActiveTable, row, col) }}
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </section>
            </section>
          </template>

          <!-- KPI-карточки вкладки: у P10 пять; у P11/P14–P18 — слайд выше -->
          <section v-if="!isP11 && !isP14 && !isDeptTab && !isP17 && !isP18" class="fot-kpis">
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
               (подразделения, каналы сбыта, направления) — таблица стран у
               P11 рендерится в слайде выше. Дрилл, крошки и CSV-кнопка
               включаются признаками из meta таблицы и вкладки. -->
          <section v-for="t in shownTables" :key="t.key" class="card">
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

          <!-- Разбор ИИ вкладки: данные собирает сервер (fot_p9..fot_p14, до
               validate_charts), фронт отдаёт только срезы. charts компоненту
               нужен лишь как признак «есть что разбирать» — путь fot_* их
               игнорирует, поэтому на сводных вкладках передаём серию карточек.
               У P15/P16 серверного пути разбора нет — блок скрыт. -->
          <CostChartInsight v-if="!isDeptTab" :block="`fot_${tab}`" :block-title="`${tabLabel} (${tab.toUpperCase()})`"
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
  { key: 'p14', label: 'ФОТ по направлениям бизнеса' },
  { key: 'p15', label: 'Отделы: свод' },
  { key: 'p16', label: 'Отделы: анализ по подразделениям' },
  { key: 'p17', label: 'Численность ГК' },
  { key: 'p18', label: 'Исполнение бюджета' },
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

/** Ключи — белый список срезов app/fot.py; лишнее сервер игнорирует.
 *  Порядок панели: страна → юрлицо → иерархия ЦФО (направление → группа →
 *  подгруппа → ЦФО). Опции каскадятся: выбор в одном фильтре сужает списки
 *  остальных (см. loadFilters / onFilterChange). */
const MULTI_FILTERS = [
  { key: 'country', label: 'Страна', opt: 'countries' },
  { key: 'legal_entity', label: 'Юрлицо', opt: 'legal_entities' },
  { key: 'direction', label: 'Направление', opt: 'directions' },
  { key: 'cfo_group', label: 'Группа ЦФО', opt: 'cfo_groups' },
  { key: 'cfo_subgroup', label: 'Подгруппа ЦФО', opt: 'cfo_subgroups' },
  { key: 'cfo', label: 'ЦФО', opt: 'cfos' },
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

/** Опции фильтра — каскад от бэкенда плюс выбранные значения, которых каскад
 *  не вернул: выбор, исключённый другим фильтром, остаётся в списке и на чипе
 *  (компонент незнакомые значения показывает сырым id — добавляем их явно). */
const multiOptions = (f: { key: string; opt: string }) => {
  const opts = asOptions(filterOptions.value[f.opt])
  const have = new Set(opts.map((o) => o.value))
  for (const v of selected[f.key] || []) {
    if (!have.has(v)) opts.push({ value: v, label: v })
  }
  return opts
}
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
    // Каскад: выбранные срезы уходят в /fot/filters повторяющимися
    // параметрами — опции каждого фильтра сужаются остальными выборами
    // (свой выбор себя не сужает). Год тоже каскадит все списки.
    const query: Record<string, any> = year.value ? { year: year.value } : {}
    for (const f of MULTI_FILTERS) {
      if ((selected[f.key] || []).length) query[f.key] = [...selected[f.key]]
    }
    const res = await api<any>('/fot/filters', { query })
    filterOptions.value = res || {}
    filtersMeta.value = res?.meta || {}
    applyDefaultsFromClosed()
  } catch (e) {
    // Сбой справочника не должен прятать дашборд — сервер подставит свои дефолты.
    console.error('[fot] filters failed', e)
  }
}

/** Смена среза: пересобираем каскад опций и сам дашборд. */
function onFilterChange() {
  loadFilters()
  loadDash()
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
/** Все таблицы вкладки — по порядку API: у P9 матрица, у P10 одна, у P11
 *  таблица стран слайда + три разреза. */
const tables = computed<any[]>(() => dash.value?.tables || [])
/** CSV-кнопка в заголовке каждой таблицы — только сводным вкладкам P10/P11. */
const canExport = computed(() => tab.value === 'p10' || tab.value === 'p11')

// ── P11: верхний свод — реплика листа 1 деки «Бюджет ФОТ 2026» ───────────────

const isP11 = computed(() => tab.value === 'p11')

// ── P14 «ФОТ по направлениям бизнеса»: лист 4 деки ───────────────────────────

const isP14 = computed(() => tab.value === 'p14')

// ── P15/P16 «Отделы»: листы 7–8 деки ─────────────────────────────────────────

const isP15 = computed(() => tab.value === 'p15')
const isP16 = computed(() => tab.value === 'p16')
const isP17 = computed(() => tab.value === 'p17')
const isP18 = computed(() => tab.value === 'p18')
const p18SubTab = ref<string>('direction')
const p18SubTabs = [
  { key: 'direction', label: 'Направление' },
  { key: 'group', label: 'Группа ЦФО' },
  { key: 'dept', label: 'Подразделение' },
]
const p18ActiveTable = computed<any>(() => {
  if (!isP18.value) return null
  const keyMap: Record<string, string> = { direction: 'p18_by_direction', group: 'p18_by_group', dept: 'p18_by_dept' }
  return tables.value.find((t: any) => t.key === keyMap[p18SubTab.value]) || null
})
/** Обе вкладки «Отделы» живут на одном слайде: KPI-лента и блок ИИ скрыты. */
const isDeptTab = computed(() => isP15.value || isP16.value)
/** Направления листа и шапка страны — отдельные блоки ответа p14. */
const directions = computed<any[]>(() => dash.value?.directions || [])
const slideMeta = computed<any>(() => meta.value.slide || {})
const slideCountryLabel = computed(() => slideMeta.value.label || 'Группа')
const slideCountryCc = computed(() => slideCountryLabel.value === 'Группа' ? 'ГК' : (slideMeta.value.cc || 'ГК'))
/** Таблица направлений рендерится в слайде; в общем списке таблиц её нет. */
const slideTable14 = computed<any>(() =>
  isP14.value ? tables.value.find((t: any) => t.key === 'by_direction') || null : null)

/** Отделы листов 7–8 — отдельный блок ответа (cards_by_department): топ-6
 *  «требующих внимания» — лист 7 (P15), остальные — лист 8 (P16). */
const deptCards = computed<any[]>(() => dash.value?.cards_by_department || [])
const deptAttention = computed<any[]>(() => deptCards.value.filter((d: any) => d.attention))
const deptRest = computed<any[]>(() => deptCards.value.filter((d: any) => !d.attention))
/** Таблица отделов рендерится в слайде P15; в общем списке таблиц её нет. */
const slideTable15 = computed<any>(() =>
  isP15.value ? tables.value.find((t: any) => t.key === 'departments') || null : null)
/** Таблица численности ГК рендерится в слайде; в общем списке таблиц её нет. */
const slideTable17 = computed<any>(() =>
  isP17.value ? tables.value.find((t: any) => t.key === 'hc_countries') || null : null)
/** Слайд 4.2: мультивыбор стран для детализации численности. */
const p17Countries = ref<string[]>(['BY'])
const p17CountryOptions = [
  { key: 'BY', label: 'Беларусь' },
  { key: 'RU', label: 'Россия' },
  { key: 'KZ', label: 'Казахстан' },
  { key: 'UZ', label: 'Узбекистан' },
]
const p17CountryDetails = computed<any>(() => {
  if (!isP17.value) return null
  const details = dash.value?.country_details || {}
  const selected = p17Countries.value.filter(c => details[c])
  if (!selected.length) return null
  // Агрегация карточек: суммы по всем выбранным странам
  const allCards = selected.flatMap(cc => details[cc].cards || [])
  const merged: Record<string, any> = {}
  for (const c of allCards) {
    const existing = merged[c.key]
    if (!existing) {
      merged[c.key] = { ...c }
    } else if (c.value !== null && existing.value !== null) {
      existing.value = Number(c.value) + Number(existing.value)
    }
  }
  // Агрегация таблиц: объединение строк
  const allRows = selected.flatMap(cc => (details[cc].table?.rows || []))
  const mergedRows: any[] = []
  const seen = new Set<string>()
  for (const row of allRows) {
    if (row.total) continue // пропускаем итоги отдельных стран
    if (!seen.has(row.label)) {
      seen.add(row.label)
      mergedRows.push({ ...row })
    }
  }
  // Итоговая строка
  if (mergedRows.length) {
    const total: any = { label: 'Итого', total: true }
    for (const row of mergedRows) {
      for (const [k, v] of Object.entries(row)) {
        if (k === 'label' || k === 'total') continue
        if (typeof v === 'number') total[k] = (total[k] || 0) + v
      }
    }
    mergedRows.push(total)
  }
  const firstTable = details[selected[0]]?.table
  const label = selected.map(cc => p17CountryOptions.find(o => o.key === cc)?.label || cc).join(', ')
  return {
    cards: Object.values(merged),
    table: firstTable ? { ...firstTable, label: `Численность — ${label} (по подразделениям)`, rows: mergedRows } : null,
    label,
  }
})
const p17CountryCards = computed<any[]>(() => p17CountryDetails.value?.cards || [])
const p17CountryTable = computed<any>(() => p17CountryDetails.value?.table || null)
const p17CountryLabel = computed(() => p17CountryDetails.value?.label || '')
/** Баннеры-выводы: P15 — «Тенденции и прогноз», P16 — пара листа 8. */
const deptTrends = computed<string[]>(() => dash.value?.dept_trends || [])
const deptBanners = computed<any>(() => dash.value?.dept_banners || null)

/** Сектора пирога «Доля ФОТ по направлениям» (ожидание года плана): от 12
 *  часов по часовой, подписи — только у секторов ≥1 % (best-fit Excel). */
const pieSegs = computed<any[]>(() => {
  const R = 75, CX = 91, CY = 91
  let a0 = -Math.PI / 2
  const segs: any[] = []
  for (const d of directions.value) {
    const p = isNum(d.pie_pct) ? Number(d.pie_pct) : 0
    if (p <= 0) continue
    const ang = (p / 100) * Math.PI * 2
    const a1 = a0 + ang
    const mid = (a0 + a1) / 2
    const pt = (r: number, a: number) => [CX + r * Math.cos(a), CY + r * Math.sin(a)]
    const [x0, y0] = pt(R, a0)
    const [x1, y1] = pt(R, a1)
    segs.push({
      path: `M ${CX} ${CY} L ${x0.toFixed(2)} ${y0.toFixed(2)}`
           + ` A ${R} ${R} 0 ${ang > Math.PI ? 1 : 0} 1 ${x1.toFixed(2)} ${y1.toFixed(2)} Z`,
      color: d.color, label: d.label, pct: p, labeled: p >= 1,
      lx: (CX + 48 * Math.cos(mid)).toFixed(1),
      ly: (CY + 48 * Math.sin(mid) + 3.5).toFixed(1),
    })
    a0 = a1
  }
  return segs
})

/** Мини-бары «доля ФОТ в выручке направления»: 4 периода, шкала своя на
 *  направление (линейная), цвета периодов — палитра chart2..6 деки. */
const BAR_COLORS = ['#C9AEB6', '#6B3347', '#9B5A70', '#C27B92']
const barSpecs = computed(() => [
  { key: 'share_h1_25', label: `Факт 1ПГ${yr2(yearPrev.value)}` },
  { key: 'share_h1_26', label: `Факт 1ПГ${yr2(yearPlan.value)}` },
  { key: 'share_h2_tac', label: `Такт.2ПГ${yr2(yearPlan.value)}` },
  { key: 'share_exp', label: `Ожид.${yr2(yearPlan.value)}` },
])
const barGeom = (d: any): any[] => {
  const vals = barSpecs.value.map((s) => ({ ...s, v: isNum(d[s.key]) ? Number(d[s.key]) : null }))
  const max = Math.max(1, ...vals.map((x) => x.v ?? 0)) * 1.18
  return vals.map((x, i) => ({ ...x, color: BAR_COLORS[i], h: x.v === null ? 0 : Math.max(2, (x.v / max) * 56) }))
}
const yearPlan = computed(() => Number(meta.value.periods?.year_plan) || 2026)
const yearPrev = computed(() => Number(meta.value.periods?.year_prev) || 2025)
const yr2 = (y: any) => String(y).slice(2)
/** Панель численности (правый край KPI-ленты) и карточки стран — отдельные
 *  блоки ответа p11 (headcount, cards_by_country), карточек они не касаются. */
const headcount = computed<any>(() => dash.value?.headcount || null)
const countryCards = computed<any[]>(() => dash.value?.cards_by_country || [])
/** Таблица стран рендерится внутри слайда; в общем списке таблиц её нет. */
const slideTable = computed<any>(() =>
  isP11.value ? tables.value.find((t: any) => t.key === 'by_country') || null : null)
const shownTables = computed<any[]>(() => {
  if (isP11.value) return tables.value.filter((t: any) => t.key !== 'by_country')
  if (isP14.value) return tables.value.filter((t: any) => t.key !== 'by_direction')
  if (isP15.value) return tables.value.filter((t: any) => t.key !== 'departments')
  if (isP17.value) return tables.value.filter((t: any) => t.key !== 'hc_countries')
  if (isP18.value) return tables.value.filter((t: any) => !t.key.startsWith('p18_'))
  return tables.value
})
/** Значение карточки по ключу — для KPI-ленты слайда. */
const kpiNum = (key: string): number | null => {
  const c = cards.value.find((x: any) => x.key === key)
  return c && isNum(c.value) ? Number(c.value) : null
}
/** P10–P16 — фиксированные полугодия: срезы дат и «План» сервер игнорирует. */
const isHalfTab = computed(() => ['p10', 'p11', 'p12', 'p13', 'p14', 'p15', 'p16', 'p17', 'p18'].includes(tab.value))
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
const nf1 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 1, maximumFractionDigits: 1 })
const nf2 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const isNum = (v: any) => v !== null && v !== undefined && v !== '' && Number.isFinite(Number(v))

const fmtCard = (v: any) => (isNum(v) ? nf2.format(Number(v)) : '—')
/** Один знак — KPI-лента, таблица стран и карточки слайда P11 (млн/%). */
const fmt1 = (v: any) => (isNum(v) ? nf1.format(Number(v)) : '—')
/** Целое — тыс. на листе P14 и % укомплектованности (лист деки показывает целые). */
const fmt0 = (v: any) => (isNum(v) ? nf0.format(Number(v)) : '—')
/** Со знаком: «+» для положительных — динамики и отклонения слайда P11. */
const fmtSigned = (v: any, dp: 1 | 2 = 1): string => {
  if (!isNum(v)) return '—'
  const n = Number(v)
  const s = dp === 2 ? nf2.format(n) : nf1.format(n)
  return n > 0 ? `+${s}` : s
}

// ── Светофор верхнего свода P11 — реплика деки, по знаку (без порога +10 %,
//    как у Δ-колонок таблиц разрезов): рост ФОТ/доли и превышение базы —
//    красный (cf-neg), снижение/экономия — зелёный (cf-pos). ──────────────────

const signCf = (v: any): string => (!isNum(v) ? '' : Number(v) > 0 ? 'cf-neg' : Number(v) < 0 ? 'cf-pos' : '')
const cmpCf = (v: any, base: any): string => {
  if (!isNum(v) || !isNum(base)) return ''
  const a = Number(v)
  const b = Number(base)
  return a > b ? 'cf-neg' : a < b ? 'cf-pos' : ''
}
/** Базы светофора таблицы стран: колонка → колонка-база той же строки
 *  (факт против бюджета, ожидание против факта 2025 — как на листе деки). */
const SLIDE_CMP_BASE: Record<string, string> = { f26_h1: 'b26_h1', t26_h2: 'b26_h2', exp26: 'f25' }
const slideCmp = (row: any, colKey: string): string => {
  const base = SLIDE_CMP_BASE[colKey]
  return base ? cmpCf(row?.[colKey], row?.[base]) : ''
}
/** В тёмной итоговой строке — светлые варианты цветов для контраста. */
const slideCmpDk = (row: any, colKey: string): string => {
  const cls = slideCmp(row, colKey)
  return cls ? `${cls}-dk` : ''
}

/** Динамика карточки отдела к факту прошлого года (правила листов 7–8 деки):
 *  P15 — «+9,1% к факту 25 (+606)», P16 — короткая «+13,5% факт25 (+71)»;
 *  без базы 2025 — «н/д …» (nd-growth деки — всегда зелёный). */
const deptDelta = (d: any, per: 'h1' | 'h2' | 'year', short: boolean): string => {
  const pct = d['dev_' + per + '_pct']
  const abs = d['dev_' + per + '_abs']
  const base = short ? `факт${yr2(yearPrev.value)}` : `к факту ${yr2(yearPrev.value)}`
  const head = isNum(pct) ? `${fmtSigned(pct)}% ${base}` : `н/д ${base}`
  return `${head} (${fmtSigned(abs)})`
}
/** Светофор строки динамики: н/д (нет базы) — всегда зелёный, как в деке. */
const ndCf = (v: any): string => (!isNum(v) ? 'cf-pos' : signCf(v))
/** Круг Δ доли ФОТ в выручке: «−0,35 п.п.» / «н/д п.п.» (0 и н/д — нейтральные;
 *  околонулевые после округления — беззнаковый «0,00», как на листе деки). */
const ppText = (v: any): string => {
  if (!isNum(v)) return 'н/д'
  const n = Number(v)
  return Math.abs(n) < 0.005 ? '0,00' : fmtSigned(n, 2)
}

/** Единицы с двумя знаками — проценты, п.п. и ставки; деньги (в col.unit
 *  валюта витрины: BYN/USD/RUB, суммы в тыс.) — целые. Список снят с
 *  фактических col.unit API (app/fot.py), фронт ничего не угадывает. */
const UNITS_2DP = new Set(['%', 'п.п.', 'ставок'])

/** Таблицы с деньгами в копейках: HR-бюджет P13 бэкенд округляет до двух
 *  знаков (_r2) при unit=валюта — точность в col.unit не выражена, поэтому
 *  исключение держим картой по ключу таблицы (контроль деки — «592,29»). */
const MONEY_2DP_TABLES = new Set(['hr_budget'])

/** Ячейка таблицы: пустое значение — прочерк, никогда не NaN.
 *  Точность — по col.unit (млн-колонки — один знак); отрицательные Δ
 *  ru-RU пишет со знаком минус. */
function cellText(t: any, row: any, col: any): string {
  const v = row?.[col.key]
  if (v === null || v === undefined || v === '') return '—'
  if (col.key === 'label' || !isNum(v)) return String(v)
  const n = Number(v)
  const unit = String(col.unit || '').trim()
  if (unit.startsWith('млн')) return nf1.format(n)
  if (UNITS_2DP.has(unit)) return nf2.format(n)
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

/* ── P11: верхний свод — реплика листа 1 деки «Бюджет ФОТ 2026» ──────────── */
.slide { display: flex; flex-direction: column; gap: var(--sp-4); }
.slide-hdr { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.slide-title {
  font-family: 'Cambria', 'Times New Roman', serif;
  font-size: 24px; font-weight: var(--fw-bold, 700);
  color: var(--fot-accent-light); margin: 0; white-space: nowrap;
}
.slide-subtitle { font-size: 15px; color: var(--fot-muted); }
.slide-year {
  margin-left: auto; width: 44px; height: 44px; border-radius: 50%;
  background: var(--fot-accent-light); color: #fff;
  display: inline-flex; align-items: center; justify-content: center;
  font-size: 12px; font-weight: var(--fw-bold, 700); flex-shrink: 0;
}
.fot-dark .slide-year { color: #2A1E22; }
.slide-unit { font-size: 12px; color: var(--fot-muted); }

/* KPI-лента: ячейки через вертикальные разделители, численность — справа */
.ribbon { display: flex; align-items: stretch; gap: var(--sp-4) var(--sp-5); flex-wrap: wrap; }
.rb {
  display: flex; flex-direction: column; gap: 3px; min-width: 0;
  padding-left: var(--sp-5); border-left: 1px solid var(--fot-border);
}
.ribbon > .rb:first-child { padding-left: 0; border-left: none; }
.rb-label {
  font-size: 11px; font-weight: var(--fw-bold, 700);
  color: var(--fot-accent-light); letter-spacing: .2px; white-space: nowrap;
}
.rb-value {
  font-family: 'Cambria', 'Times New Roman', serif; font-size: 21px;
  font-weight: var(--fw-bold, 700); color: var(--fot-ink); white-space: nowrap;
}
.rb-big .rb-value { font-size: 30px; }
.rb-unit { font-size: 12px; font-weight: var(--fw-regular); color: var(--fot-muted); margin-left: 2px; }
.rb-sub { font-size: 12px; font-weight: var(--fw-bold, 700); color: var(--fot-ink); white-space: nowrap; }
.rb-hc { margin-left: auto; display: flex; gap: var(--sp-3); padding-left: var(--sp-4); flex-wrap: wrap; }
.hc {
  display: flex; flex-direction: column; gap: 2px;
  padding-left: var(--sp-3); border-left: 1px solid var(--fot-border);
}
.rb-hc > .hc:first-child { padding-left: 0; border-left: none; }
.hc-label { font-size: 10px; font-weight: var(--fw-bold, 700); color: var(--fot-accent-light); white-space: nowrap; }
.hc-value {
  font-family: 'Cambria', 'Times New Roman', serif; font-size: 15px;
  font-weight: var(--fw-bold, 700); color: var(--fot-ink); white-space: nowrap;
}

/* Светофор слайда: по знаку; *-dk — светлые варианты для тёмной строки итога */
.cf-pos { color: var(--fot-pos); }
.cf-neg { color: var(--fot-neg); }
.cf-pos-dk { color: #8FD9B0; }
.cf-neg-dk { color: #F2A69D; }

/* Таблица стран слайда: рамочная, как на листе деки, итог — тёмной строкой */
.stable { width: 100%; border-collapse: collapse; font-size: 13px; color: var(--fot-ink); }
.stable th, .stable td {
  border: 1px solid var(--fot-border); text-align: center;
  padding: 4px 6px; white-space: nowrap;
}
.stable th { background: var(--fot-accent); color: var(--fot-accent-ink); font-weight: var(--fw-bold, 700); font-size: 12px; }
.fot-dark .stable th { color: #2A1E22; }
.stable th.col-name, .stable td.col-name { text-align: left; padding-left: 10px; }
.stable td.num { font-variant-numeric: tabular-nums; }
.stable tbody tr:nth-child(odd):not(.total) td { background: var(--fot-card); }
.stable tr.total td {
  /* винный итог листа деки — тёмный в обеих темах, светлые цифры */
  background: #6B3347; color: #fff;
  font-weight: var(--fw-bold, 700); border-color: #6B3347;
}
/* светофор в тёмной итоговой строке — светлые варианты, специфичнее белого */
.stable tr.total td.cf-pos-dk { color: #8FD9B0; }
.stable tr.total td.cf-neg-dk { color: #F2A69D; }

/* Заголовок и карточки стран «Ожидание 2026 к факту 2025» */
.grid-caption { font-size: 16px; font-weight: var(--fw-bold, 700); color: var(--fot-accent-light); margin: 0; }
.cc-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); gap: var(--sp-3); }
.ccard {
  position: relative; background: var(--fot-card);
  border: 1px solid var(--fot-border); border-radius: 8px;
  padding: var(--sp-3) var(--sp-4);
  display: flex; flex-direction: column; gap: var(--sp-2); min-width: 0;
}
.cc-code { font-size: 16px; font-weight: var(--fw-bold, 700); color: var(--fot-accent-light); }
.cc-dyncap {
  position: absolute; right: 58px; top: 16px;
  font-size: 10px; color: var(--fot-muted); white-space: nowrap;
}
.cc-badge {
  position: absolute; right: var(--sp-4); top: 10px; width: 32px; height: 32px;
  border-radius: 50%; background: #fff; border: 1.3px solid currentColor;
  color: var(--fot-ink); font-size: 10px; font-weight: var(--fw-bold, 700);
  display: inline-flex; align-items: center; justify-content: center;
}
.cc-badge.cf-pos { color: var(--fot-pos); }
.cc-badge.cf-neg { color: var(--fot-neg); }
.fot-dark .cc-badge { background: #1E1418; }
.cc-big {
  font-family: 'Cambria', 'Times New Roman', serif; font-size: 25px;
  font-weight: var(--fw-bold, 700); color: var(--fot-ink); white-space: nowrap;
}
.cc-delta { font-size: 13.5px; font-weight: var(--fw-bold, 700); color: var(--fot-ink); white-space: nowrap; }
/* светофор строки «% к 2025»: специфичнее дефолтного цвета .cc-delta */
.cc-delta.cf-pos { color: var(--fot-pos); }
.cc-delta.cf-neg { color: var(--fot-neg); }
.cc-sep { border: none; border-top: 1px solid var(--fot-border); margin: 0; }
.cc-sep-soft { border-top-color: color-mix(in srgb, var(--fot-border) 55%, transparent); }
.cc-line { margin: 0; font-size: 13px; color: var(--fot-muted); white-space: nowrap; }
.cc-line b { color: var(--fot-ink); font-weight: var(--fw-bold, 700); }
.cc-hc { margin: 0; font-size: 12px; color: var(--fot-muted); line-height: 1.55; white-space: nowrap; }
.cc-hc b { color: var(--fot-ink); font-weight: var(--fw-bold, 700); }
/* светофор внутри .cc-hc: специфичнее дефолтного цвета b */
.cc-hc b.cf-pos { color: var(--fot-pos); }
.cc-hc b.cf-neg { color: var(--fot-neg); }

/* ── P14 «ФОТ по направлениям бизнеса»: лист 4 деки ─────────────────────── */
.slide-note { font-size: 12px; color: var(--fot-muted); white-space: nowrap; }
.slide-note b { color: var(--fot-ink); font-weight: var(--fw-bold, 700); }
.p14-geo { font-size: 13px; letter-spacing: .3px; }

/* Верхний ряд: таблица (шире) + донат структуры */
.p14-top { display: grid; grid-template-columns: minmax(0, 3fr) minmax(260px, 2fr); gap: var(--sp-4); align-items: start; }
@media (max-width: 980px) { .p14-top { grid-template-columns: 1fr; } }
.p14-stable th { font-size: 11px; }
.p14-stable td { font-size: 12px; }
.p14-stable td.num { font-family: var(--font-mono); }

/* Пирог «Доля ФОТ по направлениям»: зазор секторов — цвет подложки */
.p14-pie-svg { width: 100%; height: auto; max-width: 420px; --fot-pie-gap: #F9F9F9; }
.fot-dark .p14-pie-svg { --fot-pie-gap: #241A1E; }
.p14-pie-segs path { stroke: var(--fot-pie-gap); stroke-width: 1; }
.p14-pie-legend text { fill: var(--fot-muted); }
.p14-pie-num text { font-family: var(--font-mono); }

/* Ряд мини-чартов «доля ФОТ в выручке» */
.p14-bars { display: grid; grid-template-columns: repeat(auto-fit, minmax(210px, 1fr)); gap: var(--sp-3); }
.p14-bar { min-width: 0; }
.p14-bar-title {
  display: block; font-size: 12.7px; font-weight: var(--fw-bold, 700);
  color: var(--fot-accent-light); text-align: center; white-space: nowrap;
  overflow: hidden; text-overflow: ellipsis; margin-bottom: var(--sp-1);
}
.p14-bar svg { width: 100%; height: auto; }
.p14-bar-val { fill: var(--fot-ink); font-size: 8.4px; font-family: var(--font-mono); }
.p14-bar-cap { fill: var(--fot-muted); font-size: 8.4px; }

/* Карточки направлений: три блока чисел + численность; слева — полоска
   цвета сектора доната (визуальная связка структуры и карточки) */
.p14-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: var(--sp-3); }
.p14card {
  position: relative; background: var(--fot-card);
  border: 1px solid var(--fot-border); border-left: 3px solid var(--dcolor, var(--fot-accent-light));
  border-radius: 8px; padding: var(--sp-3) var(--sp-4);
  display: flex; flex-direction: column; gap: var(--sp-2); min-width: 0;
}
.p14c-block { position: relative; display: flex; flex-direction: column; gap: 2px; padding-right: 44px; }
.p14c-block + .p14c-block { border-top: 1px solid color-mix(in srgb, var(--fot-border) 60%, transparent); padding-top: var(--sp-2); }
.p14c-label { font-size: 10.4px; font-weight: var(--fw-bold, 700); color: var(--fot-accent-light); letter-spacing: .2px; }
.p14c-value {
  font-family: 'Cambria', 'Times New Roman', serif; font-size: 17px;
  font-weight: var(--fw-bold, 700); color: var(--fot-ink); white-space: nowrap;
}
.p14c-unit { font-size: 11px; font-weight: var(--fw-regular); color: var(--fot-muted); }
.p14c-delta { font-size: 12px; font-weight: var(--fw-bold, 700); color: var(--fot-ink); white-space: nowrap; }
/* светофор строк «% к 2025» — специфичнее дефолтного цвета .p14c-delta */
.p14c-delta.cf-pos { color: var(--fot-pos); }
.p14c-delta.cf-neg { color: var(--fot-neg); }
/* бейдж Δ доли, п.п. — справа от блока (классы цвета у cc-badge уже есть) */
.p14c-badge { position: absolute; right: 0; top: 50%; transform: translateY(-50%); }

/* ── P15/P16 «Отделы»: листы 7–8 деки ───────────────────────────────────── */
/* Круглый бейдж шапки «BY ОТДЕЛЫ» — как печать отдела на листах деки. */
.p15-badge {
  flex-direction: column; width: 50px; height: 50px;
  font-size: 8px; line-height: 1.15; letter-spacing: .4px;
}
.fot-dark .p15-badge { color: #2A1E22; }

/* Таблица «Отдел × периоды»: плотнее базовой stable — строк много */
.p15-stable th { font-size: 10.5px; }
.p15-stable td { font-size: 11.5px; padding: 3px 5px; }
.p15-stable td.num { font-family: var(--font-mono); }
.p15-stable td.col-name { font-weight: var(--fw-bold, 700); font-size: 11px; }

/* Карточки отделов (лист 7 — «требующие внимания», лист 8 — остальные) */
.dpc-grid { display: grid; gap: var(--sp-3); }
.dp15-grid { grid-template-columns: repeat(auto-fit, minmax(235px, 1fr)); }
.dp16-grid { grid-template-columns: repeat(auto-fit, minmax(225px, 1fr)); }
.dpcard {
  position: relative; background: var(--fot-card);
  border: 1px solid var(--fot-accent-light); border-radius: 5px;
  padding: var(--sp-2) var(--sp-3);
  display: flex; flex-direction: column; gap: var(--sp-2); min-width: 0;
}
.dp16 .dpcard { padding: var(--sp-2); gap: 5px; }
.dpc-title {
  font-size: 12px; font-weight: var(--fw-bold, 700); color: var(--fot-accent-light);
  text-align: center; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  padding-bottom: var(--sp-1); border-bottom: 1px solid var(--fot-border);
}
.dp16 .dpc-title { font-size: 11px; }
.dpc-block { position: relative; display: flex; flex-direction: column; gap: 1px; padding-right: 48px; }
.dpc-block + .dpc-block {
  border-top: 1px solid color-mix(in srgb, var(--fot-border) 60%, transparent);
  padding-top: var(--sp-2);
}
.dp16 .dpc-block + .dpc-block { padding-top: 5px; }
.dpc-label {
  font-size: 9.5px; font-weight: var(--fw-bold, 700);
  color: var(--fot-accent-light); letter-spacing: .2px; white-space: nowrap;
}
.dp16 .dpc-label { font-size: 9px; }
.dpc-value {
  font-family: 'Cambria', 'Times New Roman', serif; font-size: 17px;
  font-weight: var(--fw-bold, 700); color: var(--fot-ink); white-space: nowrap;
}
.dp16 .dpc-value { font-size: 15px; }
.dpc-unit { font-size: 10px; font-weight: var(--fw-regular); color: var(--fot-muted); margin-left: 2px; }
.dpc-delta { font-size: 11px; font-weight: var(--fw-bold, 700); color: var(--fot-ink); white-space: nowrap; }
/* светофор динамики — специфичнее дефолтного цвета .dpc-delta */
.dpc-delta.cf-pos { color: var(--fot-pos); }
.dpc-delta.cf-neg { color: var(--fot-neg); }

/* Круг «Δ доли ФОТ в выручке, п.п.» + подпись базы (листы 7–8 деки):
   число и рамка — currentColor, «п.п.» остаётся серым; 0 и н/д — нейтральные */
.dpc-ppwrap {
  position: absolute; right: 0; top: 50%; transform: translateY(-50%);
  display: flex; flex-direction: column; align-items: center; gap: 2px;
}
.dpc-pp {
  width: 40px; height: 40px; border-radius: 50%;
  background: #fff; border: 1.5px solid currentColor;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  line-height: 1.05; color: var(--fot-muted);
}
.dpc-pp.pp3 { border-width: 1.7px; }
.dpc-pp .pp-val { font-size: 9px; font-weight: var(--fw-bold, 700); color: var(--fot-ink); }
.dpc-pp .pp-unit { font-size: 8px; color: var(--fot-muted); }
/* светофор круга — специфичнее базовых цветов .dpc-pp/.pp-val */
.dpc-pp.cf-pos { color: var(--fot-pos); }
.dpc-pp.cf-neg { color: var(--fot-neg); }
.dpc-pp.cf-pos .pp-val { color: var(--fot-pos); }
.dpc-pp.cf-neg .pp-val { color: var(--fot-neg); }
.fot-dark .dpc-pp { background: #1E1418; }
.dpc-ppcap { font-size: 7.5px; color: var(--fot-muted); white-space: nowrap; }

/* Баннеры-выводы: «Тенденции и прогноз» (лист 7) и пара листа 8.
   Светлой теме — точные цвета деки, тёмной — подмес к фону карточки. */
.dp-banner {
  border-radius: 5px; background: #F7ECEF; border: 1px solid #C27B92;
  padding: 7px 14px; font-size: 12px; color: var(--fot-ink); line-height: 1.45;
}
.dp-banner b, .dp-banner .nm { font-weight: var(--fw-bold, 700); }
.dp-banner .nm { color: var(--fot-accent); }
.dpb-good { background: #EAF3EC; border-color: var(--fot-pos); }
.dpb-title {
  display: block; font-size: 12.5px; font-weight: var(--fw-bold, 700);
  margin-bottom: 5px; color: var(--fot-ink);
}
.dpb-good .dpb-title { color: var(--fot-pos); }
.dpb-warn .dpb-title { color: var(--fot-mid); }
.dp-banners { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: var(--sp-3); }
.fot-dark .dp-banner { background: color-mix(in srgb, #C27B92 14%, var(--fot-card)); }
.fot-dark .dpb-good { background: color-mix(in srgb, var(--fot-pos) 14%, var(--fot-card)); }

@media (max-width: 520px) {
  .dpc-value { font-size: 15px; }
  .dp16 .dpc-value { font-size: 13.5px; }
  .dpc-block { padding-right: 44px; }
}

@media (max-width: 520px) {
  .cc-dyncap { display: none; }
  .rb, .rb-hc { border-left: none; padding-left: 0; }
}

.spinning { animation: fot-spin 0.9s linear infinite; }
@keyframes fot-spin { to { transform: rotate(360deg); } }

@media (max-width: 720px) {
  .fot-kpi-value { font-size: 18px; }
}

/* P17 — разделитель слайдов и селектор страны */
.p17-divider { height: 1px; background: var(--border, #e2e8f0); margin: 16px 0; }
.p17-country-sel { display: flex; gap: 12px; margin-bottom: 8px; flex-wrap: wrap; }
.p17-cc-chk { font: inherit; font-size: 13px; cursor: pointer; display: flex; align-items: center; gap: 4px; }
.p17-cc-chk input { accent-color: var(--accent, #4338ca); }

/* P18 — подтабы разрезов */
.p18-tabs { display: flex; gap: 6px; margin: 8px 0 4px; }
.p18-tab {
  font: inherit; font-size: 13px; padding: 4px 12px; border-radius: 6px;
  border: 1px solid var(--border, #e2e8f0); background: transparent;
  color: var(--text-muted, #64748b); cursor: pointer; transition: all 0.15s;
}
.p18-tab.active { background: var(--accent, #4338ca); color: #fff; border-color: var(--accent, #4338ca); }
.p18-tab:hover:not(.active) { border-color: var(--accent, #4338ca); color: var(--text, #1e293b); }

@media (max-width: 720px) {
  .p18-tabs { flex-wrap: wrap; }
  .p18-tab { font-size: 12px; padding: 3px 8px; }
}

</style>
