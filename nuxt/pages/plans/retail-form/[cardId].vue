<!--
  Форма ввода «Тактический план продаж, Розница» (TPL-TO-RETAIL).
  Закрывает ТЗ Розница §3 (что вводится), §4 (что выводится и как считается),
  §4.5 (массовые операции с обязательным предпросмотром diff), §4.6 (переключение
  представления и пресеты), §6/§8 (валидации перед отправкой).

  Ключевая идея экрана: руками заполняется ТОЛЬКО месячная ячейка плана продаж
  (плюс комментарий и, для финансиста, переопределение LFL-статуса). Всё
  остальное — атрибуты магазина из справочника и расчёт. Поэтому редактируемые
  ячейки визуально отделены от read-only колонок, а не свалены в одну таблицу.

  Почему индикаторы пересчитываются на клиенте: авторитет расчёта — сервер, но
  ждать ответа на каждое нажатие клавиши нельзя (375 строк × 12 месяцев), а без
  мгновенной подсветки «LFL −40 %» человек узнаёт о проблеме только после
  сохранения. Поэтому здесь повторены ровно те формулы §4.4, которые выводятся
  из уже полученных операндов; всё, что пересчитать точно нельзя (ожидание года,
  отклонение к факту ПГ), помечается как «пересчитается при сохранении», а не
  показывается выдуманным числом.
-->
<template>
  <div class="page-retail">
    <header class="page-header">
      <div>
        <h1 class="page-title">{{ card?.title || "Розница" }} · {{ periodLabel }}</h1>
        <strong v-if="form" class="tactic-period">Тактика: {{ monthLabel(form.month) }} {{ form.year }}</strong>
        <p class="page-subtitle">
          Форма ввода TPL-TO-RETAIL
          <span v-if="form"> · {{ form.country }} · {{ form.legal_entity || "ЮЛ не задано" }}</span>
          <span v-if="form"> · ввод в {{ form.nat_currency }}</span>
          <!-- Курсы разные по назначению колонки: тактика пересчитывается курсом
               ФАКТА ПРЕДЫДУЩЕГО МЕСЯЦА (файл ТО: V2 = Май_26!W2), факт — своим.
               Показываем оба, иначе непонятно, почему суммы не делятся на один. -->
          <span v-if="fxNote" class="fx-note" :title="fxHint">{{ fxNote }}</span>
          <span v-if="form"> · {{ filteredRows.length }} из {{ form.rows.length }} магазинов</span>
          <span v-if="form && !editable" class="lock-note">
            <Icon name="lucide:lock" /> период закрыт на запись
          </span>
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink v-if="card" :to="`/plans/${card.pl_id}`" class="btn btn-sm btn-ghost">
          <Icon name="lucide:arrow-left" /> К периоду
        </NuxtLink>
        <NuxtLink :to="`/plans/retail-summary/${cardId}`" class="btn btn-sm btn-ghost">
          <Icon name="lucide:layout-list" /> Свод
        </NuxtLink>
        <button class="btn btn-sm btn-ghost" :disabled="busy" @click="paramsOpen = true">
          <Icon name="lucide:sliders-horizontal" /> Параметры периода
        </button>
        <button class="btn btn-sm btn-ghost" :disabled="busy" @click="doExport">
          <Icon name="lucide:download" /> Экспорт
        </button>
        <button
          class="btn btn-sm btn-ghost"
          :disabled="busy"
          title="Скачать пример .xlsx: нужные колонки, месяцы периода и подсказки по заполнению"
          @click="doImportTemplate"
        >
          <Icon name="lucide:file-down" /> Пример импорта
        </button>
        <button class="btn btn-sm btn-ghost" :disabled="busy || !editable" @click="fileInput?.click()">
          <Icon name="lucide:upload" /> Импорт
        </button>
        <input ref="fileInput" type="file" accept=".xlsx" class="hidden-file" @change="doImport" />
        <button class="btn btn-sm btn-ghost" :disabled="busy" @click="runValidate()">
          <Icon name="lucide:shield-check" /> Проверить
        </button>
        <button class="btn btn-sm btn-primary" :disabled="busy || !editable || !dirty" @click="save">
          <Icon name="lucide:save" /> Сохранить<span v-if="dirtyCount"> ({{ dirtyCount }})</span>
        </button>
      </div>
    </header>

    <!-- Источники данных (ТЗ доработок п. 1.1): дата актуальности, источник,
         число строк. Пустой или упавший источник — явное сообщение. -->
    <p v-if="form?.sources?.length" class="src-line">
      <Icon name="lucide:database" />
      данные загружены {{ sourcesAsOf }} ·
      <span v-for="(s, i) in form.sources" :key="s.code" :class="{ 'src-bad': s.error || s.rows === 0 }" :title="s.error || ''">
        {{ s.label }}: {{ s.error ? "ошибка" : s.rows.toLocaleString("ru-RU") }}<template v-if="i < form.sources.length - 1">; </template>
      </span>
    </p>
    <p v-if="badSources.length" class="banner banner-warn">
      Нет данных из источников: {{ badSources.map((s) => s.label + (s.error ? ` (${s.error})` : " — 0 строк")).join("; ") }}.
      Соответствующие колонки пусты, это не нули.
    </p>
    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>
    <p v-if="draftNote" class="banner banner-info">
      {{ draftNote }}
      <button type="button" class="link-btn" @click="dropDraft">Отменить черновик</button>
    </p>
    <p v-if="form && !editable" class="banner banner-warn">
      Только просмотр: карточка на шаге «{{ form.step_code }}» ({{ form.status }}) или вы не исполнитель.
      Ввод сохранить не получится.
    </p>

    <SkeletonTable v-if="loading" :rows="10" :cols="10" />

    <template v-if="form">
      <!-- ===== Фильтры выборки =====
           Фильтр — не украшение: он задаёт область «отфильтрованные строки» для
           массовых операций §4.5, поэтому его состояние всегда на виду. -->
      <section class="card filter-card">
        <div class="flt">
          <input v-model="q" class="search flt-search" type="search" placeholder="Поиск: название, код ЦФО, KLIENT_ID" />
          <select v-model="fCity" class="select"><option value="">Все города</option><option v-for="v in optCity" :key="v" :value="v">{{ v }}</option></select>
          <select v-model="fLfl" class="select"><option value="">Все LFL-статусы</option><option v-for="v in optLfl" :key="v" :value="v">{{ lflLabel(v) }}</option></select>
          <select v-model="fType" class="select"><option value="">Все типы</option><option v-for="v in optType" :key="v" :value="v">{{ v }}</option></select>
          <select v-model="fCat" class="select"><option value="">Все категории</option><option v-for="v in optCat" :key="v" :value="v">{{ v }}</option></select>
          <select v-model="fRm" class="select"><option value="">Все РМ</option><option v-for="v in optRm" :key="v" :value="v">{{ v }}</option></select>
          <select v-model="fLe" class="select"><option value="">Все ЮЛ</option><option v-for="v in optLe" :key="v" :value="v">{{ v }}</option></select>
          <label class="checkbox flt-chk"><input v-model="showClosed" type="checkbox" /> показывать закрытые</label>
          <button v-if="hasFilters" type="button" class="btn btn-sm btn-ghost" @click="resetFilters">
            <Icon name="lucide:x" /> Сбросить
          </button>
        </div>

        <!-- ===== Представление (§4.6) ===== -->
        <div class="flt flt-view">
          <div class="segmented">
            <button type="button" :aria-pressed="mode === 'store_months'" @click="mode = 'store_months'">магазин → месяцы</button>
            <button type="button" :aria-pressed="mode === 'month_stores'" @click="mode = 'month_stores'">месяц → магазины</button>
          </div>
          <select v-if="mode === 'month_stores'" v-model.number="selMonth" class="select">
            <option v-for="m in form.months" :key="m" :value="m">{{ monthLabel(m) }} {{ form.year }}</option>
          </select>
          <div class="segmented">
            <button type="button" :aria-pressed="detail" @click="detail = true">с детализацией</button>
            <button type="button" :aria-pressed="!detail" @click="detail = false">без детализации</button>
          </div>
          <button type="button" class="btn btn-sm" :class="!detail && groupBy === 'lfl_status' ? 'btn-primary' : 'btn-ghost'" @click="detail = false; groupBy = 'lfl_status'">Группы LFL</button>
          <select v-if="!detail" v-model="groupBy" class="select">
            <option value="lfl_status">по: статусу LFL</option>
            <option v-for="g in GROUP_DIMS" :key="g.key" :value="g.key">по: {{ g.title }}</option>
          </select>

          <details class="cols-menu">
            <summary class="btn btn-sm btn-ghost"><Icon name="lucide:columns-3" /> Колонки</summary>
            <div class="cols-pop">
              <p class="cols-fixed">Код ЦФО, название магазина и город выводятся всегда (§4.2).</p>
              <div v-for="grp in COL_GROUPS" :key="grp.title" class="cols-grp">
                <span class="cols-grp-t">{{ grp.title }}</span>
                <label v-for="c in grp.cols" :key="c.key" class="checkbox">
                  <input v-model="cols[c.key]" type="checkbox" /> {{ c.title }}
                  <span v-if="c.hint" class="cols-hint">{{ c.hint }}</span>
                </label>
              </div>
            </div>
          </details>

          <div class="preset-box">
            <select class="select" :value="presetName" @change="applyPreset(($event.target as HTMLSelectElement).value)">
              <option value="">Представление: последнее</option>
              <option v-for="p in namedPresets" :key="p.name" :value="p.name">{{ p.name }}</option>
            </select>
            <button type="button" class="btn btn-sm btn-ghost" title="Сохранить текущее представление под именем" @click="savePresetAs">
              <Icon name="lucide:bookmark-plus" />
            </button>
            <button v-if="presetName" type="button" class="btn btn-sm btn-ghost" title="Удалить пресет" @click="removePreset">
              <Icon name="lucide:trash-2" />
            </button>
          </div>

          <span class="flt-spacer"></span>
          <span v-if="selected.size" class="sel-note">выделено: {{ selected.size }}
            <button type="button" class="link-btn" @click="selected.clear()">снять</button>
          </span>
          <button type="button" class="btn btn-sm" :class="bulkOpen ? 'btn-primary' : 'btn-ghost'" @click="bulkOpen = !bulkOpen">
            <Icon name="lucide:wand-sparkles" /> Массовые операции
          </button>
        </div>
      </section>

      <!-- ===== Панель массовых операций (§4.5) ===== -->
      <section v-if="bulkOpen" class="card bulk-card">
        <div class="card-header">
          <span class="card-title">Массовая операция</span>
          <span class="hdr-hint">
            Величины (индекс роста, удельный вес ФОТ, пороги аренды) берутся из параметров периода,
            а не из этой панели — задайте их в «Параметрах периода». Индекс роста и удельный вес ФОТ
            утверждаются на уровне страны, переопределения — уточнение к ним.
          </span>
        </div>
        <p v-if="!editable" class="banner banner-warn bulk-closed">
          Операции недоступны: карточка закрыта на запись (на согласовании или утверждена). Чтобы менять значения, её нужно вернуть на доработку.
        </p>
        <p class="bulk-help">
          <b>{{ BULK_OP_LABELS[bulkOp] }}.</b> {{ BULK_OP_HELP[bulkOp] }}
          Сначала «Предпросмотр» — он ничего не записывает и показывает, что изменится («было → станет»); применить можно после него.
        </p>
        <div class="bulk-form">
          <label class="form-field">
            <span class="form-label">Операция</span>
            <select v-model="bulkOp" class="select" @change="invalidatePreview">
              <option v-for="(t, k) in BULK_OP_LABELS" :key="k" :value="k">{{ t }}</option>
            </select>
          </label>
          <label class="form-field">
            <span class="form-label">Область применения</span>
            <select v-model="bulkScope" class="select" @change="invalidatePreview">
              <option value="all">вся выборка ({{ form.rows.length }})</option>
              <option value="filtered">отфильтрованные ({{ filteredRows.length }})</option>
              <option value="selected" :disabled="!selected.size">выделенные ({{ selected.size }})</option>
            </select>
          </label>
          <!-- Копирование сценария — к ОДНОМУ месяцу, по умолчанию месяц тактики
               (ТЗ доработок п. 2.3); предпросмотр показывает только его. -->
          <label v-if="bulkOp === 'copy_scenario'" class="form-field">
            <span class="form-label">Месяц применения</span>
            <select v-model.number="bulkApplyMonth" class="select" @change="invalidatePreview">
              <option v-for="m in form.months" :key="m" :value="m">{{ monthLabel(m) }} {{ form.year }}{{ m === form.month ? " · месяц тактики" : "" }}</option>
            </select>
          </label>
          <label v-else class="form-field">
            <span class="form-label">Месяц операции</span>
            <select v-model="bulkMonthMode" class="select" @change="invalidatePreview">
              <option value="all">все плановые</option>
              <option value="one">только {{ monthLabel(selMonth) }}</option>
            </select>
          </label>

          <template v-if="bulkOp === 'copy_scenario'">
            <label class="form-field">
              <span class="form-label">Сценарий</span>
              <select v-model="bulkSource" class="select" @change="invalidatePreview">
                <option value="strategy">Стратегия</option>
                <option value="tactic">Утверждённая тактика</option>
                <option value="new_strategy">Новая стратегия</option>
                <option value="fact">Факт прошлого года</option>
              </select>
            </label>
            <!-- Надбавка убрана (ТЗ доработок п. 2.2): рабочее поле — коэффициент,
                 допускается значение меньше 1. -->
            <label class="form-field">
              <span class="form-label">Коэффициент × k</span>
              <input v-model.number="bulkCoef" class="input" type="number" step="0.01" min="0" placeholder="1" @input="invalidatePreview" />
            </label>
          </template>

          <template v-if="bulkOp === 'distribute'">
            <label class="form-field">
              <span class="form-label">Целевой итог на область</span>
              <input v-model.number="bulkTarget" class="input" type="number" @input="invalidatePreview" />
            </label>
            <label class="form-field wide">
              <span class="form-label">Месяцы базы (через запятую)</span>
              <input v-model="bulkBaseMonths" class="input" placeholder="напр. 1,2,3,4,5" @input="invalidatePreview" />
            </label>
          </template>

          <label v-if="bulkOp === 'sales_index'" class="form-field">
            <span class="form-label">База индекса</span>
            <select v-model="bulkIndexBase" class="select" @change="invalidatePreview">
              <option value="fact_prev_month">факт предыдущего месяца</option>
              <option value="fact_prev_year">факт того же месяца прошлого года</option>
              <option value="approved_prev">ранее утверждённая тактика</option>
              <option value="strategy">стратегия</option>
            </select>
          </label>

          <template v-if="bulkOp === 'payroll'">
            <label class="form-field">
              <span class="form-label">База порога 106 %</span>
              <select v-model="bulkPayrollBase" class="select" @change="invalidatePreview">
                <option value="strategy">стратегия того же месяца</option>
                <option value="plan_current_month">план продаж текущего месяца</option>
                <option value="fact_prev_year">факт прошлого года</option>
                <option value="approved_prev">утверждённая тактика</option>
              </select>
            </label>
            <span class="form-field wide bulk-note">
              Удельный вес применяется к продажам без НДС; фонд страны распределяется по магазинам
              в пропорции среднего факта за последний закрытый квартал.
            </span>
          </template>

          <label v-if="bulkOp === 'rent'" class="checkbox bulk-chk">
            <input v-model="bulkRentTurnover" type="checkbox" @change="invalidatePreview" />
            считать оборотную часть (иначе — только факт аренды предыдущего месяца)
          </label>
          <!-- Правка ТЗ 02.09.2026 §30: этап 2 (расчёт по договору) отменён —
               реестра договоров в базах нет. Аренда вводится, а факт предыдущего
               месяца показывается справочно: сколько магазинов он покрывает,
               видно сразу, иначе операция выглядит как «посчитает сама». -->
          <p v-if="bulkOp === 'rent'" class="bulk-hint">
            <Icon name="lucide:lightbulb" />
            Аренда — поле ввода: реестра договоров аренды в базах нет, расчёт по договору не делаем.
            Справочно есть факт предыдущего месяца по
            {{ rentHintCount }} {{ rentHintCount === 1 ? "магазину" : "магазинам" }}
            (статьи 54 «Аренда помещений (стоянки)» и 55 «Аренда имущества»).
          </p>

          <label class="checkbox bulk-chk">
            <input v-model="bulkResetManual" type="checkbox" @change="invalidatePreview" />
            сбросить защиту ручных значений
          </label>

          <button
            class="btn btn-primary bulk-go"
            :disabled="busy || !editable"
            :title="editable ? 'Показать, что изменится, ничего не записывая' : 'Карточка закрыта на запись: операции недоступны'"
            @click="runPreview"
          >
            <Icon name="lucide:eye" /> Предпросмотр
          </button>
        </div>
      </section>

      <BulkDiffPreview
        v-if="bulkPreview"
        :result="bulkPreview"
        :busy="busy"
        :can-apply="editable && previewFresh"
        :apply-hint="previewFresh ? '' : 'Параметры операции изменились — сделайте предпросмотр заново'"
        @apply="applyBulk"
        @close="bulkPreview = null"
      />

      <!-- ===== Отчёт валидаций (§6) ===== -->
      <section v-if="report && reportOpen" class="card val-card">
        <div class="card-header">
          <span class="card-title">Проверка формы</span>
          <span class="badge" :class="reportTone">{{ reportLabel }}</span>
          <button type="button" class="link-btn" @click="reportOpen = false">скрыть</button>
        </div>
        <p v-if="!report.blocking.length && !report.warnings.length" class="val-ok">
          Замечаний нет.
        </p>
        <div v-else class="val-lists">
          <div v-if="report.blocking.length" class="val-col">
            <span class="val-h val-h-neg">Блокирующие ({{ report.blocking.length }})</span>
            <ul class="val-ul">
              <li v-for="(i, k) in report.blocking.slice(0, 60)" :key="k">
                <button type="button" class="val-link" @click="gotoRow(i.code_cfo)">
                  <span class="badge badge-neg">{{ i.code }}</span> {{ i.message }}
                </button>
              </li>
            </ul>
            <p v-if="report.blocking.length > 60" class="val-more">
              …и ещё {{ report.blocking.length - 60 }}. Чаще всего это незаполненные месяцы — заполните массовой операцией.
            </p>
          </div>
          <div v-if="report.warnings.length" class="val-col">
            <span class="val-h val-h-warn">Предупреждения ({{ report.warnings.length }})</span>
            <ul class="val-ul">
              <li v-for="(i, k) in report.warnings.slice(0, 60)" :key="k">
                <button type="button" class="val-link" @click="gotoRow(i.code_cfo)">
                  <span class="badge badge-warn">{{ i.code }}</span> {{ i.message }}
                </button>
              </li>
            </ul>
            <p class="val-more">Предупреждение не блокирует ввод, но требует комментария к строке (V-07).</p>
          </div>
        </div>
      </section>

      <!-- ===== Сетка ===== -->
      <section class="card grid-card">
        <div ref="scrollEl" class="grid-scroll" @paste="nav.onPaste">
          <table class="data-table compact grid-table">
            <thead>
              <tr>
                <th class="st st-1">
                  <input
                    type="checkbox"
                    :checked="allSelected"
                    :disabled="!detail"
                    title="выделить отфильтрованные"
                    @change="toggleAll"
                  />
                </th>
                <!-- Сортировка «от А до Я» и обратно по требованию «Фронт таблицы».
                     Первые три столбца не перемещаются (§4.2 требует выводить их
                     всегда), но сортировать по ним можно. -->
                <th class="st st-2 col-num sortable" :class="{ sorted: sortKey === 'code_cfo' }" @click="detail && toggleSort('code_cfo')">
                  ЦФО <Icon v-if="detail" :name="sortIcon('code_cfo')" class="s-ic" />
                </th>
                <th class="st st-3">LFL-статус</th>
                <th class="st st-4 sortable" :class="{ sorted: sortKey === 'cfo' }" @click="detail && toggleSort('cfo')">
                  {{ detail ? "Магазин" : groupTitle }} <Icon v-if="detail" :name="sortIcon('cfo')" class="s-ic" />
                </th>
                <th class="st st-5 sortable" :class="{ sorted: sortKey === 'city' }" @click="detail && toggleSort('city')">
                  {{ detail ? "Город" : "Магазинов" }} <Icon v-if="detail" :name="sortIcon('city')" class="s-ic" />
                </th>
                <th
                  v-for="c in visibleAttrCols"
                  :key="c.key"
                  :class="[c.num ? 'col-num' : '', { sortable: detail, sorted: sortKey === c.key, movable: MOVABLE_ATTR_KEYS.includes(c.key) }]"
                  :draggable="detail && MOVABLE_ATTR_KEYS.includes(c.key)"
                  :title="MOVABLE_ATTR_KEYS.includes(c.key) ? 'Перетащите, чтобы поменять порядок; клик — сортировка' : 'Клик — сортировка'"
                  @click="detail && toggleSort(c.key)"
                  @dragstart="onColDragStart(c.key)"
                  @dragover.prevent
                  @drop.prevent="onColDrop(c.key)"
                >
                  <Icon v-if="detail && MOVABLE_ATTR_KEYS.includes(c.key)" name="lucide:grip-vertical" class="g-ic" />
                  {{ c.title }}
                  <Icon v-if="detail" :name="sortIcon(c.key)" class="s-ic" />
                  <!-- Кнопки-стрелки — доступная альтернатива перетаскиванию:
                       drag не работает с клавиатуры и на тач-экранах. -->
                  <span v-if="detail && MOVABLE_ATTR_KEYS.includes(c.key)" class="mv-btns">
                    <button class="mv" title="Левее" @click.stop="moveAttrCol(c.key, -1)">‹</button>
                    <button class="mv" title="Правее" @click.stop="moveAttrCol(c.key, 1)">›</button>
                  </span>
                </th>
                <th v-for="c in visibleCmpCols" :key="c.key" class="col-num cmp-h" :title="c.hint">{{ cmpTitle(c) }}</th>
                <th v-for="m in monthCols" :key="'m' + m" class="col-num inp-h">
                  {{ monthLabel(m) }} {{ String(form.year).slice(2) }}
                </th>
                <th v-for="c in visibleIndCols" :key="c.key" class="col-num ind-h" :title="c.hint">{{ c.title }}</th>
                <th v-if="detail" class="cmt-h">Комментарий</th>
              </tr>
            </thead>
            <tbody v-if="detail">
              <tr v-if="padTop" class="pad-row" :style="{ height: padTop + 'px' }"><td :colspan="colCount"></td></tr>
              <tr
                v-for="vr in renderRows"
                :key="vr.row.code_cfo"
                :data-code="vr.row.code_cfo"
                :class="{ 'row-sel': selected.has(vr.row.code_cfo), 'row-hit': hitCode === vr.row.code_cfo, 'row-dirty': rowDirty(vr.row.code_cfo) }"
              >
                <td class="st st-1">
                  <input type="checkbox" :checked="selected.has(vr.row.code_cfo)" @change="toggleRow(vr.row.code_cfo)" />
                </td>
                <td class="st st-2 col-num">{{ vr.row.code_cfo }}</td>
                <td class="st st-3"><button type="button" class="lfl-chip" :class="'lfl-' + vr.row.lfl_effective" :disabled="!canOverride || !editable" @click="openLfl(vr.row)">{{ lflLabel(vr.row.lfl_effective) }}</button></td>
                <td class="st st-4">
                  <span class="s-name" :title="vr.row.cfo">{{ vr.row.cfo || "—" }}</span>
                  <span v-if="vr.row.lfl_override" class="s-ovr" :title="vr.row.lfl_reason">LFL переопределён</span>
                </td>
                <td class="st st-5">{{ vr.row.city || "—" }}</td>

                <td v-for="c in visibleAttrCols" :key="c.key" :class="c.num ? 'col-num' : ''">
                  <button
                    v-if="c.key === 'lfl_status'"
                    type="button"
                    class="lfl-chip"
                    :class="'lfl-' + vr.row.lfl_effective"
                    :title="canOverride ? 'Переопределить LFL-статус (нужна причина)' : 'LFL-статус из справочника'"
                    :disabled="!canOverride || !editable"
                    @click="openLfl(vr.row)"
                  >{{ lflLabel(vr.row.lfl_effective) }}</button>
                  <span v-else>{{ attrText(vr.row, c) }}</span>
                </td>

                <td v-for="c in visibleCmpCols" :key="c.key" class="col-num cmp-c">
                  {{ moneyOrDash(vr.ind[c.key] as number | null) }}
                </td>

                <td
                  v-for="(m, ci) in monthCols"
                  :key="'c' + m"
                  class="col-num inp-c"
                  :class="cellClass(vr.row, m)"
                >
                  <NumberField
                    :model-value="cellValue(vr.row.code_cfo, m)"
                    class="cell-input"
                    :disabled="!editable"
                    :data-grid-cell="`${vr.index}:${ci}`"
                    :title="cellTitle(vr.row, m)"
                    @update:model-value="setCell(vr.row.code_cfo, m, $event)"
                    @keydown="nav.onKeydown($event, vr.index, ci)"
                    @focus="nav.onFocusCell(vr.index, ci)"
                  />
                </td>

                <td v-for="c in visibleIndCols" :key="c.key" class="col-num ind-c" :class="indClass(vr, c.key)">
                  {{ indText(vr, c.key) }}
                </td>

                <td v-if="detail" class="cmt-c">
                  <input
                    :value="commentValue(vr.row)"
                    class="input cmt-input"
                    :disabled="!editable"
                    :class="{ 'need-why': needsComment(vr.row.code_cfo) && !commentValue(vr.row).trim() }"
                    :data-grid-cell="`${vr.index}:${monthCols.length}`"
                    :placeholder="needsComment(vr.row.code_cfo) ? 'обязателен: есть предупреждения' : ''"
                    @input="setComment(vr.row.code_cfo, ($event.target as HTMLInputElement).value)"
                    @keydown="nav.onKeydown($event, vr.index, monthCols.length)"
                    @focus="nav.onFocusCell(vr.index, monthCols.length)"
                  />
                </td>
              </tr>
              <tr v-if="padBottom" class="pad-row" :style="{ height: padBottom + 'px' }"><td :colspan="colCount"></td></tr>
              <tr v-if="!filteredRows.length">
                <td :colspan="colCount" class="empty-cell">Под фильтр не попало ни одного магазина.</td>
              </tr>
            </tbody>

            <!-- Свёрнутый вид (§4.6): итоги выбранного уровня, ячейки только читаются. -->
            <tbody v-else>
              <template v-for="g in groupRows" :key="g.key">
              <tr class="status-group" @click="toggleGroup(g.key)">
                <td class="st st-1"></td>
                <td class="st st-2 col-num">—</td>
                <td class="st st-3">{{ groupBy === 'lfl_status' ? lflLabel(g.key) : '—' }}</td>
                <td class="st st-4"><button type="button" class="group-toggle"><Icon :name="groupOpen.has(g.key) ? 'lucide:chevron-down' : 'lucide:chevron-right'" /> {{ groupBy === 'lfl_status' ? lflLabel(g.key) : (g.key || 'не задано') }}</button></td>
                <td class="st st-5 col-num">{{ g.rows.length }}</td>
                <td v-for="c in visibleAttrCols" :key="c.key" :class="c.num ? 'col-num' : ''">—</td>
                <td v-for="c in visibleCmpCols" :key="c.key" class="col-num cmp-c">{{ moneyOrDash(g.ind[c.key] as number | null) }}</td>
                <td v-for="m in monthCols" :key="'gm' + m" class="col-num ro-c">{{ moneyOrDash(g.months[m] ?? null) }}</td>
                <td v-for="c in visibleIndCols" :key="c.key" class="col-num ind-c">{{ indText(g, c.key) }}</td>
              </tr>
              <tr v-for="r in groupOpen.has(g.key) ? g.rows : []" :key="r.code_cfo" class="group-store">
                <td class="st st-1"><input type="checkbox" :checked="selected.has(r.code_cfo)" @change="toggleRow(r.code_cfo)" /></td>
                <td class="st st-2 col-num">{{ r.code_cfo }}</td>
                <td class="st st-3">{{ lflLabel(r.lfl_effective) }}</td>
                <td class="st st-4"><span class="s-name">{{ r.cfo }}</span><input :value="commentValue(r)" class="input cmt-input" :disabled="!editable" placeholder="Комментарий к строке" @input="setComment(r.code_cfo, ($event.target as HTMLInputElement).value)" /></td>
                <td class="st st-5">{{ r.city || '—' }}</td>
                <td v-for="c in visibleAttrCols" :key="c.key">{{ attrText(r, c) }}</td>
                <td v-for="c in visibleCmpCols" :key="c.key" class="col-num cmp-c">{{ moneyOrDash(liveInd(r)[c.key] as number | null) }}</td>
                <td v-for="m in monthCols" :key="m" class="col-num inp-c"><NumberField :model-value="cellValue(r.code_cfo, m)" class="cell-input" :disabled="!editable" @update:model-value="setCell(r.code_cfo, m, $event)" /></td>
                <td v-for="c in visibleIndCols" :key="c.key" class="col-num ind-c">{{ indText({ row: r, ind: liveInd(r), stale: rowDirty(r.code_cfo) }, c.key) }}</td>
              </tr>
              </template>
            </tbody>

            <tfoot>
              <tr>
                <td class="st st-1"></td>
                <td class="st st-2"></td>
                <td class="st st-3"></td>
                <td class="st st-4">Итого по выборке</td>
                <td class="st st-5 col-num">{{ filteredRows.length }}</td>
                <td v-for="c in visibleAttrCols" :key="c.key"></td>
                <td v-for="c in visibleCmpCols" :key="c.key" class="col-num">{{ moneyOrDash(totals.ind[c.key] as number | null) }}</td>
                <td v-for="m in monthCols" :key="'tm' + m" class="col-num">{{ moneyOrDash(totals.months[m] ?? null) }}</td>
                <td v-for="c in visibleIndCols" :key="c.key" class="col-num">{{ indText(totals, c.key) }}</td>
                <td v-if="detail"></td>
              </tr>
            </tfoot>
          </table>
        </div>
        <p class="grid-foot">
          <span v-if="!virtOn" class="grid-warn">
            <Icon name="lucide:triangle-alert" /> Виртуализация выключена — строки отрисованы целиком.
          </span>
          <span>Ввод «как в Excel»: стрелки и Tab/Enter переходят между ячейками, Ctrl+V вставляет диапазон из буфера.</span>
          <span v-if="dirty" class="grid-dirty"><Icon name="lucide:circle-dot" /> не сохранено: {{ dirtyCount }}</span>
        </p>
      </section>

      <!-- Панель процесса — здесь же кнопка отправки на согласование: она обязана
           быть недоступна, пока валидации §6 дают can_submit=false. -->
      <CardProcessPanel
        :card-id="cardId"
        :can-submit="(report ? report.can_submit : true) && totalsReviewed && !dirty"
        :retail-totals-reviewed="totalsReviewed"
        :submit-hint="dirty ? 'Сначала сохраните изменения' : !totalsReviewed ? 'Сверьте итоги по статусам' : report && !report.can_submit ? `блокирующих замечаний: ${report.blocking.length}` : ''"
        :soft-warning="softWarning"
        @changed="reload"
      >
        <template #before-submit>
          <div class="retail-review card">
            <strong>Итоги по статусам перед отправкой</strong>
            <div v-for="g in statusTotals" :key="g.key">{{ lflLabel(g.key) }} · {{ g.rows.length }} магазинов · тактика {{ moneyOrDash(g.ind.tactic) }} · итого период {{ moneyOrDash(g.ind.period_total) }}</div>
            <label class="checkbox"><input v-model="totalsReviewed" type="checkbox" :disabled="dirty || !editable" /> Сверил итоги по четырём статусам</label>
          </div>
        </template>
      </CardProcessPanel>
    </template>

    <!-- ===== Параметры периода (§4.5, §5) ===== -->
    <PlansModal :open="paramsOpen" title="Параметры периода" width="860px" @close="paramsOpen = false">
      <p class="pm-hint">
        Из этих величин массовые операции считают индекс роста, ФОТ и аренду. Приоритет — от частного
        к общему: магазин → тип → LFL-статус → город → страна.
      </p>
      <table class="data-table compact">
        <thead>
          <tr><th>Параметр</th><th>Область</th><th>Значение области</th><th class="col-num">Значение</th><th>Задал</th><th></th></tr>
        </thead>
        <tbody>
          <tr v-for="(p, i) in paramDraft" :key="i">
            <td>
              <select v-model="p.param_code" class="select"><option v-for="(t, k) in PARAM_LABELS" :key="k" :value="k">{{ t }}</option></select>
            </td>
            <td>
              <select v-model="p.scope_kind" class="select">
                <option value="country">страна</option><option value="city">город</option>
                <option value="lfl">LFL-статус</option><option value="store_type">тип магазина</option>
                <option value="store">магазин (код ЦФО)</option>
              </select>
            </td>
            <td><input v-model="p.scope_value" class="input" :placeholder="scopePlaceholder(p.scope_kind)" /></td>
            <td class="col-num"><NumberField :model-value="p.value" :digits="4" class="cell-input" @update:model-value="p.value = $event ?? 0" /></td>
            <td class="p-by">
              <span v-if="p.set_by_name">{{ p.set_by_name }}</span>
              <span v-if="p.set_at" class="p-at">{{ dt(p.set_at) }}</span>
              <span v-if="!p.set_by_name && !p.set_at" class="p-at">новый</span>
            </td>
            <td><button type="button" class="btn btn-sm btn-ghost" title="Убрать из списка сохранения" @click="paramDraft.splice(i, 1)"><Icon name="lucide:x" /></button></td>
          </tr>
          <tr v-if="!paramDraft.length"><td colspan="6" class="empty-cell">Параметры не заданы — операции используют значения по умолчанию.</td></tr>
        </tbody>
      </table>
      <p class="pm-hint">
        Доли задаются в долях единицы: 0,05 = 5 %; порог ФОТ 1,06 = 106 %. Удаление параметра через API
        не предусмотрено — чтобы отключить влияние, задайте нулевое значение.
      </p>
      <template #footer>
        <button class="btn btn-ghost" @click="addParam"><Icon name="lucide:plus" /> Добавить</button>
        <button class="btn btn-ghost" @click="paramsOpen = false">Закрыть</button>
        <button class="btn btn-primary" :disabled="busy || !editable" @click="saveParams">Сохранить</button>
      </template>
    </PlansModal>

    <!-- ===== Переопределение LFL-статуса (§4.3: причина обязательна) ===== -->
    <PlansModal :open="!!lflRow" :title="`LFL-статус: ${lflRow?.cfo || ''}`" width="480px" @close="lflRow = null">
      <label class="form-field">
        <span class="form-label">Статус периода планирования</span>
        <select v-model="lflNew" class="select">
          <option value="">снять переопределение (вернуть «{{ lflLabel(lflRow?.lfl_status || "") }}» из справочника)</option>
          <option v-for="(t, k) in LFL_LABELS" :key="k" :value="k">{{ t }}</option>
        </select>
      </label>
      <label class="form-field">
        <span class="form-label">Причина (обязательно)</span>
        <input v-model="lflReason" class="input" placeholder="напр.: переезжаем, увеличиваем площадь → до года" />
      </label>
      <p class="pm-hint">Справочник не перезатирается: переопределение живёт только в этом периоде планирования.</p>
      <template #footer>
        <button class="btn btn-ghost" @click="lflRow = null">Отмена</button>
        <button class="btn btn-primary" :disabled="busy || !lflReason.trim()" @click="saveLfl">Применить</button>
      </template>
    </PlansModal>
  </div>
</template>

<script setup lang="ts">
import BulkDiffPreview from "~/components/plans/BulkDiffPreview.vue";
import CardProcessPanel from "~/components/plans/CardProcessPanel.vue";

/**
 * Плашка мягкого режима (ответ аналитика 41 от 10.09.2026). Проверки считаются
 * всегда; когда блокировка выключена, замечания обязаны быть видны текстом —
 * и заполняющему, и согласующему, — иначе форма уезжает дальше молча.
 * Сумма расхождения берётся из несошедшихся сверок; пропущенные (источник
 * недоступен) в сумму не входят — это не расхождение, а отсутствие данных.
 */
const softWarning = computed(() => {
  const r = report.value;
  if (!r || r.strict || !r.blocking.length) return "";
  const diff = (r.reconciliations || [])
    .filter((x) => !x.ok && !x.skipped)
    .reduce((s, x) => s + Math.abs(x.diff), 0);
  const head = `Свод не сходится: блокирующих замечаний ${r.blocking.length}`;
  const tail = diff ? `, расхождение ${fmtNum(diff, 0)}` : "";
  return `${head}${tail}. Отправка разрешена — режим блокировки выключен.`;
});

/** Подпись и тон плашки отчёта: мягкий режим — не «заблокирована», а «не сходится». */
const reportTone = computed(() => {
  const r = report.value;
  if (!r) return "badge-muted";
  if (!r.can_submit) return "badge-neg";
  return r.blocking.length ? "badge-warn" : "badge-pos";
});
const reportLabel = computed(() => {
  const r = report.value;
  if (!r) return "";
  if (!r.can_submit) return "отправка заблокирована";
  return r.blocking.length ? "свод не сходится, отправка разрешена" : "можно отправлять на согласование";
});
import NumberField from "~/components/NumberField.vue";
import PlansModal from "~/components/plans/PlansModal.vue";
import SkeletonTable from "~/components/SkeletonTable.vue";
import { useGridNav } from "~/composables/useGridNav";
import { useMpConditions, type PlanCard } from "~/composables/useMpConditions";
import { useScope } from "~/composables/useScope";
import {
  BULK_OP_HELP, BULK_OP_LABELS, LFL_LABELS, MONTH_LABELS, PARAM_LABELS, RETAIL_FORM_CODE,
  cellKey, iferrorDiv, iferrorRatioMinus1, noBaseline, resolveParam, retailErrText, useRetail,
  type RetailBulkRequest, type RetailBulkResult, type RetailForm, type RetailIndicators,
  type RetailParam, type RetailRow, type RetailValidationReport, type ViewPreset,
  type FieldHint as RetailFieldHint
} from "~/composables/useRetail";
import { num as fmtNum, formatDate } from "~/utils/format";

definePageMeta({ middleware: ["scope-guard"] });

const route = useRoute();
const cardId = Number(route.params.cardId);
const api = useRetail();
const cardApi = useMpConditions();
const { hasRole, isAdmin } = useScope();

// ─────────── состояние ───────────
const form = ref<RetailForm | null>(null);
const card = ref<PlanCard | null>(null);
const report = ref<RetailValidationReport | null>(null);
// Видимость панели отделена от самого результата: «скрыть отчёт» не должно
// разблокировать отправку на согласование — иначе запрет обходится одним кликом.
const reportOpen = ref(true);
const loading = ref(true);
const busy = ref(false);
const error = ref("");
const note = ref("");
const draftNote = ref("");

// Курсы отображения. Их несколько, и это не избыточность: тактика в файле ТО
// пересчитывается курсом факта ПРЕДЫДУЩЕГО месяца (V2 = Май_26!W2), факт — курсом
// своего месяца, стратегия — плановым. Раньше кабинет пересчитывал всё одним
// справочным курсом (3,2), и суммы расходились с файлом незаметно для глаза.
const fxNum = (v: number): string => (v > 0 ? (1 / v).toFixed(4).replace(".", ",") : "—");
const fxNote = computed(() => {
  const f = form.value;
  if (!f || !f.fx_rates || f.currency === f.nat_currency) return "";
  return ` · курс тактики ${fxNum(f.fx_rates.tactic)}`;
});
const fxHint = computed(() => {
  const r = form.value?.fx_rates;
  if (!r) return "";
  return (
    `курс тактики — курс факта предыдущего месяца (${fxNum(r.tactic)}); ` +
    `факт пересчитан по ${fxNum(r.fact_current)}, факт прошлого года — ${fxNum(r.fact_prev_year)}, ` +
    `стратегия — по плановому ${fxNum(r.strategy)}`
  );
});

/** Несохранённый ввод: ключ ячейки → значение (null = «пусто», это не 0 — см. V-01). */
const edits = reactive(new Map<string, number | null>());
/** Несохранённые комментарии: код ЦФО → текст. */
const commentEdits = reactive(new Map<number, string>());

const editable = computed(() => !!form.value?.editable && route.query.readonly !== "1");
const canOverride = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN") || hasRole("ROLE_FINANCE_ADMIN"));

const periodLabel = computed(() =>
  form.value ? `${String(form.value.month).padStart(2, "0")}.${form.value.year}` : ""
);

// ─────────── фильтры ───────────
const q = ref("");
const fCity = ref("");
const fLfl = ref("");
const fType = ref("");
const fCat = ref("");
const fRm = ref("");
const fLe = ref("");
const showClosed = ref(false);

const hasFilters = computed(() =>
  !!(q.value || fCity.value || fLfl.value || fType.value || fCat.value || fRm.value || fLe.value || showClosed.value)
);
const resetFilters = () => {
  q.value = fCity.value = fLfl.value = fType.value = fCat.value = fRm.value = fLe.value = "";
  showClosed.value = false;
};

const uniq = (pick: (r: RetailRow) => string) =>
  computed(() => [...new Set((form.value?.rows || []).map(pick).filter(Boolean))].sort((a, b) => a.localeCompare(b, "ru")));
const optCity = uniq((r) => r.city);
const optLfl = uniq((r) => r.lfl_effective);
const optType = uniq((r) => r.store_type);
const optCat = uniq((r) => r.category);
const optRm = uniq((r) => r.reg_manager);
const optLe = uniq((r) => r.legal_entity);

const filteredRows = computed<RetailRow[]>(() => {
  const rows = form.value?.rows || [];
  const needle = q.value.trim().toLowerCase();
  return rows.filter((r) => {
    // Закрытые магазины по умолчанию скрыты: их план не заполняют, а в списке
    // из 375 строк они только мешают искать живые точки.
    if (!showClosed.value && r.lfl_effective === "closed") return false;
    if (fCity.value && r.city !== fCity.value) return false;
    if (fLfl.value && r.lfl_effective !== fLfl.value) return false;
    if (fType.value && r.store_type !== fType.value) return false;
    if (fCat.value && r.category !== fCat.value) return false;
    if (fRm.value && r.reg_manager !== fRm.value) return false;
    if (fLe.value && r.legal_entity !== fLe.value) return false;
    if (needle) {
      const hay = `${r.cfo} ${r.code_cfo} ${r.klient_id} ${r.city}`.toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });
});

/**
 * Строки в порядке сортировки (требование «Фронт таблицы»: «от А до Я» и
 * наоборот). Сортируем КОПИЮ: form.rows — источник для сохранения и для
 * массовых операций, и менять его порядок значит менять то, что уедет на сервер.
 * Строки сравниваются локалью ru: иначе «Ё» уезжает за «Я», а регистр начинает
 * влиять на порядок.
 */
const sortedRows = computed<RetailRow[]>(() => {
  const rows = filteredRows.value;
  if (!sortKey.value) return rows;
  const dir = sortDir.value === "asc" ? 1 : -1;
  return [...rows].sort((a, b) => {
    const va = sortValue(a, sortKey.value);
    const vb = sortValue(b, sortKey.value);
    if (typeof va === "number" && typeof vb === "number") return (va - vb) * dir;
    return String(va).localeCompare(String(vb), "ru", { sensitivity: "base" }) * dir;
  });
});

// ─────────── выделение ───────────
const selected = reactive(new Set<number>());
const toggleRow = (code: number) => {
  if (selected.has(code)) selected.delete(code);
  else selected.add(code);
};
const allSelected = computed(() => filteredRows.value.length > 0 && filteredRows.value.every((r) => selected.has(r.code_cfo)));
const toggleAll = () => {
  if (allSelected.value) filteredRows.value.forEach((r) => selected.delete(r.code_cfo));
  else filteredRows.value.forEach((r) => selected.add(r.code_cfo));
};

// ─────────── представление (§4.6) ───────────
type ViewMode = "store_months" | "month_stores";
const mode = ref<ViewMode>("store_months");
const detail = ref(false);
const groupBy = ref("lfl_status");
const groupOpen = reactive(new Set<string>());
const toggleGroup = (key: string) => groupOpen.has(key) ? groupOpen.delete(key) : groupOpen.add(key);
const totalsReviewed = ref(false);
const STATUS_ORDER = ["lfl", "under1y", "new", "reconstruction"];
const statusKey = (r: RetailRow) => r.lfl_effective === "xxx" ? "new" : r.lfl_effective;
// Set from the loaded tactic period, not an arbitrary calendar month.
const selMonth = ref(0);

const GROUP_DIMS = [
  { key: "city", title: "город" },
  { key: "reg_manager", title: "РМ" },
  { key: "store_type", title: "тип магазина" },
  { key: "legal_entity", title: "ЮЛ" },
  { key: "category", title: "категория" }
];
const groupTitle = computed(() => GROUP_DIMS.find((g) => g.key === groupBy.value)?.title || "разрез");

interface ColDef { key: string; title: string; hint?: string; num?: boolean }

// Код ЦФО, название и город в этот список НЕ входят: §4.2 требует, чтобы они
// выводились всегда и не скрывались пресетом.
const ATTR_COLS: ColDef[] = [
  { key: "lfl_status", title: "LFL" },
  { key: "store_type", title: "Тип" },
  { key: "category", title: "Кат." },
  { key: "reg_manager", title: "РМ" },
  { key: "legal_entity", title: "ЮЛ" },
  { key: "klient_id", title: "KLIENT_ID" },
  { key: "ploschad", title: "Метраж", num: true },
  { key: "date_open", title: "Открыт" },
  { key: "date_close", title: "Закрыт" },
  { key: "stage", title: "Стадия" },
  { key: "manager", title: "Менеджер" },
  { key: "code_fox", title: "ID лисы" },
  { key: "pl_analytic", title: "Аналитика PL" }
];
const CMP_COLS: ColDef[] = [
  { key: "fact_prev_year_month", title: "Факт ПГ", hint: "Факт того же месяца прошлого года" },
  { key: "fact_prev_month", title: "Факт пред.мес", hint: "Факт предыдущего месяца" },
  { key: "fact_cur_month", title: "Факт тек.мес", hint: "Факт текущего месяца плана на момент загрузки (месяц ещё идёт)" },
  { key: "strategy", title: "Стратегия", hint: "Стратегический план на месяц" },
  { key: "tactic_approved", title: "Утв. тактика", hint: "Ранее утверждённая тактика на месяц" }
];
const IND_COLS: ColDef[] = [
  { key: "plan_done_pct", title: "% вып.", hint: "Факт / тактика месяца" },
  { key: "lfl_tactic", title: "LFL тактика", hint: "LFL тактика = тактика / факт того же месяца прошлого года − 1" },
  { key: "lfm_tactic", title: "LFM такт.", hint: "Тактика / факт предыдущего месяца − 1" },
  { key: "vs_strategy_pct", title: "Тактика / стратегия, %", hint: "Тактика / стратегия, % = тактика / стратегия − 1" },
  { key: "period_total", title: "Итого период", hint: "Итого период = сумма месяцев горизонта формы" },
  { key: "year_expectation", title: "Ожидание года", hint: "Факт закрытых месяцев + тактика остальных" },
  { key: "year_exp_vs_prev_pct", title: "Откл. к ПГ", hint: "Ожидание года / факт прошлого года − 1" }
];
const COL_GROUPS = [
  { title: "Атрибуты магазина", cols: ATTR_COLS },
  { title: "Сравнение", cols: CMP_COLS },
  { title: "Показатели", cols: IND_COLS }
];

const DEFAULT_COLS: Record<string, boolean> = {};
for (const c of ATTR_COLS) DEFAULT_COLS[c.key] = ["lfl_status", "store_type", "category", "reg_manager"].includes(c.key);
for (const c of CMP_COLS) DEFAULT_COLS[c.key] = true;
for (const c of IND_COLS) DEFAULT_COLS[c.key] = true;
const cols = reactive<Record<string, boolean>>({ ...DEFAULT_COLS });

// ─────────── подсказки к полям ввода (правки ТЗ 02.09.2026, часть 1 и §30) ───────────
//
// Аренда стала полем ввода: реестра договоров в базах нет, этап 2 расчёта по
// договору отменён. Факт предыдущего месяца по тому же ЦФО показывается СПРАВОЧНО
// — значение не подставляется, решение принимает финансист.
const hintByCell = computed(() => {
  const m: Record<string, RetailFieldHint> = {};
  for (const h of form.value?.hints || []) m[`${h.code_cfo}:${h.block_type}`] = h;
  return m;
});

/** Сколько магазинов покрыто подсказкой по аренде (для панели операции). */
const rentHintCount = computed(
  () => new Set((form.value?.hints || []).filter((h) => h.kind === "rent_prev").map((h) => h.code_cfo)).size
);

/** Подсказка по аренде для магазина: текст с источником и периодом. */
const rentHint = (codeCfo: number, metric: string): string => {
  const h = hintByCell.value[`${codeCfo}:${metric}`];
  if (!h) return "";
  return `${fmtNum(h.value, 2)} — ${h.source}${h.note ? ` · ${h.note}` : ""}`;
};

// ─────────── порядок и сортировка столбцов (требование «Фронт таблицы») ───────────
//
// Требование правок 02.09.2026: столбцы ЦФО, Магазин, Город, LFL, Тип, Кат., РМ
// должны перемещаться между собой (настройка У КАЖДОГО ПОЛЬЗОВАТЕЛЯ своя) и
// сортироваться «от А до Я» и наоборот.
//
// Два решения, которые стоит понимать:
//   1. Порядок хранится СПИСКОМ КЛЮЧЕЙ в localStorage, а не индексами: набор
//      столбцов со временем меняется, и индексы после релиза начали бы
//      переставлять не те колонки. Незнакомые ключи игнорируются, отсутствующие
//      добавляются в конец — старая настройка не ломает новую таблицу.
//   2. Перемещаются ТОЛЬКО перечисленные в требовании столбцы. Первые три (код
//      ЦФО, магазин, город) остаются на месте, потому что §4.2 требует выводить
//      их всегда; месяцы, сравнение и показатели живут своими группами и
//      смешивать их с атрибутами нельзя — это разные разделы шапки.
const MOVABLE_ATTR_KEYS = ["lfl_status", "store_type", "category", "reg_manager"];
const attrOrder = ref<string[]>(ATTR_COLS.map((c) => c.key));

/** Ключ настройки — на пользователя и на форму: у РМ и финансиста разные таблицы. */
const colPrefsKey = computed(() => `retail-cols:${useAuth().user.value?.id ?? "anon"}`);

const loadColPrefs = () => {
  if (!process.client) return;
  try {
    const raw = localStorage.getItem(colPrefsKey.value);
    if (!raw) return;
    const saved = JSON.parse(raw) as { order?: string[]; sortKey?: string; sortDir?: "asc" | "desc" };
    if (Array.isArray(saved.order)) {
      const known = saved.order.filter((k) => ATTR_COLS.some((c) => c.key === k));
      const missing = ATTR_COLS.map((c) => c.key).filter((k) => !known.includes(k));
      attrOrder.value = [...known, ...missing];
    }
    if (saved.sortKey) sortKey.value = saved.sortKey;
    if (saved.sortDir) sortDir.value = saved.sortDir;
  } catch {
    // Повреждённая настройка не должна мешать открыть форму: берём порядок по умолчанию.
  }
};

const saveColPrefs = () => {
  if (!process.client) return;
  try {
    localStorage.setItem(
      colPrefsKey.value,
      JSON.stringify({ order: attrOrder.value, sortKey: sortKey.value, sortDir: sortDir.value })
    );
  } catch {
    // Квота переполнена — настройка не критична, порядок остаётся в памяти сессии.
  }
};

/** Перемещение столбца влево/вправо среди перемещаемых (drag + кнопки). */
const moveAttrCol = (key: string, dir: -1 | 1) => {
  const movable = attrOrder.value.filter((k) => MOVABLE_ATTR_KEYS.includes(k));
  const i = movable.indexOf(key);
  const j = i + dir;
  if (i < 0 || j < 0 || j >= movable.length) return;
  [movable[i], movable[j]] = [movable[j], movable[i]];
  let m = 0;
  attrOrder.value = attrOrder.value.map((k) => (MOVABLE_ATTR_KEYS.includes(k) ? movable[m++] : k));
  saveColPrefs();
};

/** Перетаскивание: источник запоминается на dragstart, порядок меняется на drop. */
const dragKey = ref("");
const onColDragStart = (key: string) => {
  dragKey.value = MOVABLE_ATTR_KEYS.includes(key) ? key : "";
};
const onColDrop = (targetKey: string) => {
  const from = dragKey.value;
  dragKey.value = "";
  if (!from || from === targetKey || !MOVABLE_ATTR_KEYS.includes(targetKey)) return;
  const order = [...attrOrder.value];
  const fi = order.indexOf(from);
  const ti = order.indexOf(targetKey);
  if (fi < 0 || ti < 0) return;
  order.splice(fi, 1);
  order.splice(ti, 0, from);
  attrOrder.value = order;
  saveColPrefs();
};

// ─────────── сортировка «от А до Я» и наоборот ───────────
type SortDir = "asc" | "desc";
const sortKey = ref("");
const sortDir = ref<SortDir>("asc");

/** Значение строки для сортировки по ключу столбца (включая фиксированные три). */
const sortValue = (r: RetailRow, key: string): string | number => {
  switch (key) {
    case "code_cfo": return r.code_cfo;
    case "cfo": return r.cfo || "";
    case "city": return r.city || "";
    case "lfl_status": return lflLabel(r.lfl_effective);
    case "store_type": return r.store_type || "";
    case "category": return r.category || "";
    case "reg_manager": return r.reg_manager || "";
    default: return "";
  }
};

const toggleSort = (key: string) => {
  if (sortKey.value === key) {
    // Третий клик снимает сортировку: вернуться к порядку справочника надо уметь.
    if (sortDir.value === "asc") sortDir.value = "desc";
    else {
      sortKey.value = "";
      sortDir.value = "asc";
    }
  } else {
    sortKey.value = key;
    sortDir.value = "asc";
  }
  saveColPrefs();
};

const sortIcon = (key: string) => {
  if (sortKey.value !== key) return "lucide:arrow-up-down";
  return sortDir.value === "asc" ? "lucide:arrow-down-a-z" : "lucide:arrow-up-a-z";
};

const visibleAttrCols = computed(() =>
  attrOrder.value.map((k) => ATTR_COLS.find((c) => c.key === k)).filter((c): c is ColDef => !!c && c.key !== "lfl_status" && cols[c.key])
);
const visibleCmpCols = computed(() => CMP_COLS.filter((c) => cols[c.key]));
// Подпись колонки факта называет период явно («Факт окт 2025»), а не «ПГ»: иначе непонятно, о каком месяце речь.
const cmpTitle = (c: ColDef): string => {
  const f = form.value;
  if (!f) return c.title;
  const m = f.month;
  const lab = (mm: number, yy: number) => `${MONTH_LABELS[mm - 1] || mm} ${yy}`;
  if (c.key === "fact_prev_year_month") return `Факт ${lab(m, f.year - 1)}`;
  if (c.key === "fact_prev_month") return m === 1 ? `Факт ${lab(12, f.year - 1)}` : `Факт ${lab(m - 1, f.year)}`;
  if (c.key === "fact_cur_month") return `Факт ${lab(m, f.year)}`;
  return c.title;
};
const visibleIndCols = computed(() => IND_COLS.filter((c) => cols[c.key]));

/** Колонки ввода: в режиме «месяц → магазины» заполняется один месяц по всем магазинам. */
const monthCols = computed<number[]>(() => {
  const f = form.value;
  if (!f) return [];
  return mode.value === "month_stores" ? [selMonth.value] : f.months;
});
const colCount = computed(
  () => 5 + visibleAttrCols.value.length + visibleCmpCols.value.length + monthCols.value.length + visibleIndCols.value.length + (detail.value ? 1 : 0)
);

// ─────────── доступ к ячейкам ───────────
const baseCells = computed(() => {
  const m = new Map<string, { amount: number | null; source: string; note?: string }>();
  for (const r of form.value?.rows || []) {
    for (const v of r.values || []) {
      m.set(cellKey(r.code_cfo, v.metric || "sales", v.year, v.month), { amount: v.amount, source: v.source, note: v.note });
    }
  }
  return m;
});

const key = (code: number, month: number) => cellKey(code, "sales", form.value?.year || 0, month);

const cellValue = (code: number, month: number): number | null => {
  const k = key(code, month);
  if (edits.has(k)) return edits.get(k) ?? null;
  return baseCells.value.get(k)?.amount ?? null;
};
const cellSource = (code: number, month: number): string => {
  const k = key(code, month);
  if (edits.has(k)) return "manual";
  return baseCells.value.get(k)?.source || "";
};

const setCell = (code: number, month: number, v: number | null) => {
  const k = key(code, month);
  const base = baseCells.value.get(k)?.amount ?? null;
  // Возврат к исходному значению снимает «грязь»: иначе счётчик несохранённого
  // растёт от простого проклика по форме и перестаёт что-либо значить.
  if (v === base) edits.delete(k);
  else edits.set(k, v);
};

const commentValue = (r: RetailRow): string =>
  commentEdits.has(r.code_cfo) ? (commentEdits.get(r.code_cfo) as string) : r.comment || "";
const setComment = (code: number, v: string) => {
  const row = form.value?.rows.find((r) => r.code_cfo === code);
  if (row && (row.comment || "") === v) commentEdits.delete(code);
  else commentEdits.set(code, v);
};

const dirtyCount = computed(() => edits.size + commentEdits.size);
const dirty = computed(() => dirtyCount.value > 0);
const rowDirty = (code: number): boolean => {
  if (commentEdits.has(code)) return true;
  for (const k of edits.keys()) if (k.startsWith(`${code}|`)) return true;
  return false;
};

// ─────────── индикаторы (§4.4) ───────────
interface LiveRow { row: RetailRow; index: number; ind: Record<string, number | null>; stale: boolean }

/** Пороги подсветки — параметры периода; дефолт 30 % (§4.4). */
const warnThreshold = (row: RetailRow, code: string): number =>
  resolveParam(form.value?.params || [], code, row, form.value?.country || "", 0.3);

const liveInd = (row: RetailRow): Record<string, number | null> => {
  const f = form.value!;
  const base: RetailIndicators = row.indicators;
  const tactic = cellValue(row.code_cfo, f.month);
  const t = tactic ?? 0;
  const nb = noBaseline(row.lfl_effective);
  let periodTotal = 0;
  for (const m of f.months) periodTotal += cellValue(row.code_cfo, m) ?? 0;
  return {
    fact_prev_year_month: base.fact_prev_year_month,
    fact_prev_month: base.fact_prev_month,
    fact_cur_month: base.fact_cur_month,
    strategy: base.strategy,
    tactic_approved: base.tactic_approved,
    tactic,
    // Для «нового» и «ххх» базы сравнения нет — выводим «—», а не 0 (§4.4).
    plan_done_pct: nb ? null : iferrorDiv(base.fact_cur_month ?? 0, t),
    lfl_tactic: nb ? null : iferrorRatioMinus1(t, base.fact_prev_year_month ?? 0),
    lfm_tactic: iferrorRatioMinus1(t, base.fact_prev_month ?? 0),
    vs_strategy_pct: iferrorRatioMinus1(t, base.strategy ?? 0),
    period_total: periodTotal,
    // Ожидание года и отклонение к факту ПГ считает сервер: клиенту неизвестно,
    // какие месяцы года закрыты календарём (§4.4 — признак закрытого месяца
    // берётся ТОЛЬКО из календаря). Показываем серверные значения и помечаем их
    // как устаревшие, пока строка не сохранена.
    year_expectation: base.year_expectation,
    year_exp_vs_prev_pct: base.year_exp_vs_prev_pct
  };
};

/** Строки к отрисовке с индексом (индекс — координата для навигации по сетке). */
const liveRows = computed<LiveRow[]>(() =>
  // Рендерим ОТСОРТИРОВАННЫЙ список (требование «Фронт таблицы»). index — позиция
  // в видимом списке: он же используется навигацией по ячейкам, поэтому брать его
  // из неотсортированного набора нельзя — стрелки уводили бы курсор не туда.
  sortedRows.value.map((row, index) => ({ row, index, ind: liveInd(row), stale: rowDirty(row.code_cfo) }))
);

// ─────────── свёрнутый вид и итоги ───────────
interface AggRow { key: string; rows: RetailRow[]; ind: Record<string, number | null>; months: Record<number, number>; stale: boolean }

/**
 * Агрегат по набору строк. Проценты считаются НА СУММАХ — так же, как их считает
 * сервер в своде (§8): среднее из процентов дало бы другое число, и свод не
 * сошёлся бы с формой.
 */
const aggregate = (rows: RetailRow[], gkey: string): AggRow => {
  const f = form.value!;
  const s = { fpy: 0, fpm: 0, fcur: 0, strat: 0, appr: 0, tactic: 0, period: 0, yexp: 0, hasFpy: false, hasFpm: false, hasStrat: false };
  const months: Record<number, number> = {};
  for (const m of f.months) months[m] = 0;
  for (const r of rows) {
    const i = liveInd(r);
    s.fpy += i.fact_prev_year_month ?? 0;
    s.hasFpy ||= i.fact_prev_year_month != null;
    s.fpm += i.fact_prev_month ?? 0;
    s.hasFpm ||= i.fact_prev_month != null;
    s.fcur += i.fact_cur_month ?? 0;
    s.strat += i.strategy ?? 0;
    s.hasStrat ||= i.strategy != null;
    s.appr += i.tactic_approved ?? 0;
    s.tactic += i.tactic ?? 0;
    s.period += i.period_total ?? 0;
    s.yexp += i.year_expectation ?? 0;
    for (const m of f.months) months[m] += cellValue(r.code_cfo, m) ?? 0;
  }
  return {
    key: gkey,
    rows,
    months,
    stale: rows.some((r) => rowDirty(r.code_cfo)),
    ind: {
      fact_prev_year_month: s.hasFpy ? s.fpy : null,
      fact_prev_month: s.hasFpm ? s.fpm : null,
      fact_cur_month: s.fcur,
      strategy: s.hasStrat ? s.strat : null,
      tactic_approved: s.appr,
      tactic: s.tactic,
      plan_done_pct: iferrorDiv(s.fcur, s.tactic),
      lfl_tactic: iferrorRatioMinus1(s.tactic, s.fpy),
      lfm_tactic: iferrorRatioMinus1(s.tactic, s.fpm),
      vs_strategy_pct: iferrorRatioMinus1(s.tactic, s.strat),
      period_total: s.period,
      year_expectation: s.yexp,
      year_exp_vs_prev_pct: null
    }
  };
};

const groupRows = computed<AggRow[]>(() => {
  const map = new Map<string, RetailRow[]>();
  for (const r of filteredRows.value) {
    const k = groupBy.value === "lfl_status" ? statusKey(r) : String((r as unknown as Record<string, unknown>)[groupBy.value] ?? "");
    if (!map.has(k)) map.set(k, []);
    map.get(k)!.push(r);
  }
  return [...map.entries()]
    .sort((a, b) => groupBy.value === "lfl_status" ? (STATUS_ORDER.indexOf(a[0]) < 0 ? 99 : STATUS_ORDER.indexOf(a[0])) - (STATUS_ORDER.indexOf(b[0]) < 0 ? 99 : STATUS_ORDER.indexOf(b[0])) : a[0].localeCompare(b[0], "ru"))
    .map(([k, rows]) => aggregate(rows, k));
});

const totals = computed<AggRow>(() => aggregate(filteredRows.value, "Итого"));
const statusTotals = computed<AggRow[]>(() => STATUS_ORDER.map((key) => aggregate((form.value?.rows || []).filter((r) => statusKey(r) === key), key)));
watch([() => form.value?.rows, dirty], () => { totalsReviewed.value = false; });

// ─────────── виртуализация ───────────
// 375 строк × 12 месяцев — это 4500 полей ввода; без виртуализации браузер
// умирает на первой же прокрутке. Отрисовываем окно видимых строк, а место
// остальных занимают две пустые строки-распорки (padTop/padBottom): с <table>
// это единственный способ виртуализации, не ломающий вёрстку колонок.
//
// Импорт динамический и обёрнут в try/catch: если пакет по какой-то причине не
// поднялся (образ собран до появления зависимости, сбой чанка), форма обязана
// открыться и работать — просто отрисует все строки и скажет об этом в консоль.

/** Минимальный контракт виртуализатора — ровно то, что здесь используется. */
interface VirtualItem { index: number; start: number; end: number }
interface VirtualizerLike {
  getVirtualItems: () => VirtualItem[];
  getTotalSize: () => number;
  scrollToIndex: (i: number, o?: unknown) => void;
}

const ROW_H_FALLBACK = 33;
const scrollEl = ref<HTMLElement | null>(null);
/** Высота строки: измеряется по факту — оценка мимо приводит к дрожанию прокрутки. */
const rowH = ref(ROW_H_FALLBACK);
/**
 * Источник — тот самый shallowRef, который вернул useVirtualizer. Держим именно
 * ref, а не его содержимое: vue-virtual сообщает об изменениях через triggerRef,
 * то есть по ссылке объект тот же. Если скопировать значение наружу, computed'ы
 * ниже больше никогда не пересчитаются, и окно рендера замрёт на первом кадре.
 */
const virtSource = shallowRef<{ value: VirtualizerLike } | null>(null);
const virtOn = computed(() => !!virtSource.value);

// Каждый computed читает virtSource.value.value САМ — так все они подписаны на
// внутренний ref и инвалидируются при прокрутке.
const virtualItems = computed<VirtualItem[]>(() => virtSource.value?.value.getVirtualItems() ?? []);
const virtTotalSize = computed(() => virtSource.value?.value.getTotalSize() ?? 0);

const renderRows = computed<LiveRow[]>(() => {
  const all = liveRows.value;
  if (!virtOn.value) return all;
  return virtualItems.value.map((vi) => all[vi.index]).filter(Boolean);
});
const padTop = computed(() => (virtualItems.value.length ? virtualItems.value[0].start : 0));
const padBottom = computed(() => {
  const items = virtualItems.value;
  if (!items.length) return 0;
  return Math.max(0, virtTotalSize.value - items[items.length - 1].end);
});

const scrollToRow = (row: number) => virtSource.value?.value.scrollToIndex(row, { align: "auto" });

let virtScope: ReturnType<typeof effectScope> | null = null;
const initVirtualizer = async () => {
  try {
    const mod = await import("@tanstack/vue-virtual");
    // useVirtualizer заводит watch'и и onScopeDispose. onMounted он не использует,
    // поэтому его можно поднять в собственном effectScope уже после монтирования —
    // нам это и нужно, ведь сам импорт асинхронный.
    virtScope = effectScope();
    virtScope.run(() => {
      virtSource.value = mod.useVirtualizer(
        computed(() => ({
          count: liveRows.value.length,
          getScrollElement: () => scrollEl.value,
          estimateSize: () => rowH.value,
          overscan: 14
        }))
      ) as unknown as { value: VirtualizerLike };
    });
    await nextTick();
    measureRowHeight();
  } catch (e) {
    console.warn(
      "[розница] Пакет @tanstack/vue-virtual не поднялся — форма отрисует все строки без виртуализации. " +
        "Проверьте зависимость в nuxt/package.json и пересоберите образ nuxt.",
      e
    );
  }
};

/** Реальная высота строки сетки: зависит от темы, плотности и набора колонок. */
const measureRowHeight = () => {
  const tr = scrollEl.value?.querySelector<HTMLElement>("tbody tr:not(.pad-row)");
  if (tr && tr.offsetHeight > 0) rowH.value = tr.offsetHeight;
};

// ─────────── навигация «как в Excel» (§4.3) ───────────
const nav = useGridNav({
  rows: () => (detail.value ? liveRows.value.length : 0),
  // Комментарий — последняя колонка сетки: стрелками и Tab в него надо попадать
  // так же, как в месячные ячейки (просьба аналитика 08.09.2026). Отдельная
  // колонка, а не «ещё один месяц»: вставка из буфера её не касается.
  cols: () => monthCols.value.length + (detail.value ? 1 : 0),
  ensureVisible: (row) => scrollToRow(row),
  onPaste: (anchor, matrix) => {
    const rows = liveRows.value;
    const months = monthCols.value;
    if (!editable.value) return;
    let applied = 0;
    for (let i = 0; i < matrix.length; i++) {
      const r = rows[anchor.row + i];
      if (!r) break;
      for (let j = 0; j < matrix[i].length; j++) {
        // Колонка комментария в matrix не участвует: за месяцами вставлять нечего.
        const m = months[anchor.col + j];
        if (m === undefined) break;
        setCell(r.row.code_cfo, m, nav.parseCellNumber(matrix[i][j]));
        applied++;
      }
    }
    note.value = `Вставлено значений из буфера: ${applied}.`;
  }
});
// Контейнер прокрутки он же контейнер сетки — ячейки ищутся внутри него.
watch(scrollEl, (el) => (nav.container.value = el));

// ─────────── форматирование ───────────
const monthLabel = (m: number) => MONTH_LABELS[m - 1] || String(m);
const lflLabel = (v: string) => LFL_LABELS[v] || v || "—";
const dt = (s?: string) => (s ? formatDate(s) : "");

const moneyOrDash = (v: number | null | undefined): string =>
  v === null || v === undefined ? "—" : fmtNum(v, 0);

/** Коэффициент выполнения: 0,87 → «87,0 %». Это доля, а не отклонение. */
const ratioPct = (v: number | null): string => (v === null ? "—" : `${(v * 100).toLocaleString("ru-RU", { minimumFractionDigits: 1, maximumFractionDigits: 1 })} %`);
/** Отклонение: −1 → «−100,0 %». Знак несёт смысл, поэтому выводим явно. */
const deltaPct = (v: number | null): string => {
  if (v === null) return "—";
  const p = v * 100;
  return `${p > 0 ? "+" : ""}${p.toLocaleString("ru-RU", { minimumFractionDigits: 1, maximumFractionDigits: 1 })} %`;
};

const DELTA_KEYS = new Set(["lfl_tactic", "lfm_tactic", "vs_strategy_pct", "year_exp_vs_prev_pct"]);
const RATIO_KEYS = new Set(["plan_done_pct"]);

const indText = (r: { ind: Record<string, number | null> }, k: string): string => {
  const v = r.ind[k];
  if (DELTA_KEYS.has(k)) return deltaPct(v);
  if (RATIO_KEYS.has(k)) return ratioPct(v);
  return moneyOrDash(v);
};

const WARN_PARAM: Record<string, string> = {
  lfl_tactic: "lfl_warn",
  lfm_tactic: "lfm_warn",
  vs_strategy_pct: "strategy_warn"
};

/** Подсветка отклонения сверх порога (§4.4): зелёный вверх, красный вниз. */
const indClass = (r: { row?: RetailRow; ind: Record<string, number | null>; stale?: boolean }, k: string): string => {
  const cls: string[] = [];
  if (r.stale && (k === "year_expectation" || k === "year_exp_vs_prev_pct")) cls.push("ind-stale");
  const v = r.ind[k];
  const param = WARN_PARAM[k];
  if (v !== null && param && r.row) {
    const th = warnThreshold(r.row, param);
    if (th > 0 && Math.abs(v) > th) cls.push(v > 0 ? "delta-pos" : "delta-neg");
  }
  return cls.join(" ");
};

const attrText = (r: RetailRow, c: ColDef): string => {
  const raw = (r as unknown as Record<string, unknown>)[c.key];
  if (c.key === "date_open" || c.key === "date_close") return raw ? formatDate(String(raw)) : "—";
  if (c.num) return raw ? fmtNum(Number(raw), 1) : "—";
  return raw ? String(raw) : "—";
};

/** Значение из массовой операции визуально отличается от ручного (§5, source-признак). */
const cellClass = (r: RetailRow, m: number): string => {
  const src = cellSource(r.code_cfo, m);
  if (edits.has(key(r.code_cfo, m))) return "cell-edited";
  if (src && src !== "manual") return "cell-derived";
  return "";
};
const cellTitle = (r: RetailRow, m: number): string => {
  const src = cellSource(r.code_cfo, m);
  const n = baseCells.value.get(key(r.code_cfo, m))?.note;
  return [src ? `источник: ${src}` : "", n || ""].filter(Boolean).join(" · ");
};

const warnCodes = computed(() => {
  const s = new Set<number>();
  for (const w of report.value?.warnings || []) if (w.code_cfo) s.add(w.code_cfo);
  return s;
});
const needsComment = (code: number) => warnCodes.value.has(code);

// ─────────── переход к строке из отчёта валидаций ───────────
const hitCode = ref(0);
const gotoRow = async (code?: number) => {
  if (!code) return;
  // Строка могла быть отфильтрована — иначе «перейти» ничего бы не сделало.
  const inList = filteredRows.value.some((r) => r.code_cfo === code);
  if (!inList) {
    resetFilters();
    showClosed.value = true;
    await nextTick();
  }
  const idx = filteredRows.value.findIndex((r) => r.code_cfo === code);
  if (idx < 0) return;
  detail.value = true;
  hitCode.value = code;
  await nav.focusCell(idx, 0);
  setTimeout(() => (hitCode.value = 0), 2500);
};

// ─────────── черновик (§4.3: потерять ввод на 375 строках нельзя) ───────────
const draftKey = computed(() => `retail-draft:${cardId}`);
let draftTimer: ReturnType<typeof setTimeout> | null = null;

const persistDraft = () => {
  if (!process.client) return;
  try {
    if (!dirty.value) {
      localStorage.removeItem(draftKey.value);
      return;
    }
    localStorage.setItem(
      draftKey.value,
      JSON.stringify({ cells: [...edits], comments: [...commentEdits], at: new Date().toISOString() })
    );
  } catch {
    // Квота localStorage переполнена — черновик не критичен, ввод остаётся в памяти.
  }
};

watch(
  () => [edits.size, commentEdits.size, [...edits.values()].join("|"), [...commentEdits.values()].join("|")].join("#"),
  () => {
    if (draftTimer) clearTimeout(draftTimer);
    draftTimer = setTimeout(persistDraft, 800);
  }
);

const restoreDraft = () => {
  if (!process.client) return;
  try {
    const raw = localStorage.getItem(draftKey.value);
    if (!raw) return;
    const d = JSON.parse(raw) as { cells: [string, number | null][]; comments: [number, string][]; at: string };
    for (const [k, v] of d.cells || []) edits.set(k, v);
    for (const [k, v] of d.comments || []) commentEdits.set(Number(k), v);
    if (dirty.value) draftNote.value = `Восстановлен несохранённый черновик от ${formatDate(d.at)} (${dirtyCount.value} изм.).`;
  } catch {
    /* повреждённый черновик просто игнорируем */
  }
};

const dropDraft = () => {
  edits.clear();
  commentEdits.clear();
  draftNote.value = "";
  if (process.client) localStorage.removeItem(draftKey.value);
};

// ─────────── пресеты представления (§4.6) ───────────
const presetList = ref<ViewPreset[]>([]);
const presetName = ref("");
const namedPresets = computed(() => presetList.value.filter((p) => p.name));
let presetTimer: ReturnType<typeof setTimeout> | null = null;

const viewPayload = (): Record<string, unknown> => ({
  mode: mode.value, detail: detail.value, groupBy: groupBy.value,
  selMonth: selMonth.value, cols: { ...cols }, showClosed: showClosed.value
});

// Применение пресета меняет ровно те же поля, за которыми следит автосохранение
// «последнего представления». Без этого флага загрузка пресета сразу писала бы
// его обратно на сервер — лишний PUT на каждом открытии формы.
const applyingPreset = ref(false);

const applyPayload = (p: Record<string, unknown>) => {
  if (!p) return;
  applyingPreset.value = true;
  nextTick(() => (applyingPreset.value = false));
  if (p.mode === "store_months" || p.mode === "month_stores") mode.value = p.mode;
  if (typeof p.detail === "boolean") detail.value = p.detail;
  if (typeof p.groupBy === "string") groupBy.value = p.groupBy;
  if (typeof p.selMonth === "number") selMonth.value = p.selMonth;
  if (typeof p.showClosed === "boolean") showClosed.value = p.showClosed;
  if (p.cols && typeof p.cols === "object") {
    for (const k of Object.keys(cols)) cols[k] = !!(p.cols as Record<string, boolean>)[k];
  }
};

const applyPreset = (name: string) => {
  presetName.value = name;
  const p = presetList.value.find((x) => x.name === name);
  if (p) applyPayload(p.payload);
};

const savePresetAs = async () => {
  const name = window.prompt("Название представления:", presetName.value || "");
  if (!name || !name.trim()) return;
  try {
    presetList.value = await api.savePreset({ name: name.trim(), is_default: false, payload: viewPayload() }, RETAIL_FORM_CODE);
    presetName.value = name.trim();
    note.value = `Представление «${name.trim()}» сохранено.`;
  } catch (e) {
    error.value = retailErrText(e, "Не удалось сохранить представление");
  }
};

const removePreset = async () => {
  if (!presetName.value) return;
  try {
    presetList.value = await api.deletePreset(presetName.value, RETAIL_FORM_CODE);
    presetName.value = "";
  } catch (e) {
    error.value = retailErrText(e, "Не удалось удалить представление");
  }
};

// Имя "" — «последнее использованное представление» (§4.6): пишется молча при
// каждом изменении вида. Фильтры и поиск в него НЕ входят: восстановленный при
// следующем входе фильтр читался бы как «половина магазинов пропала».
watch(
  () => JSON.stringify(viewPayload()),
  () => {
    if (loading.value || applyingPreset.value) return;
    if (presetTimer) clearTimeout(presetTimer);
    presetTimer = setTimeout(() => {
      api.savePreset({ name: "", is_default: false, payload: viewPayload() }, RETAIL_FORM_CODE).catch(() => {
        /* пресет — удобство, его недоступность не должна мешать вводу */
      });
    }, 1200);
  }
);

// ─────────── параметры периода ───────────
const paramsOpen = ref(false);
const paramDraft = ref<RetailParam[]>([]);
const addParam = () =>
  paramDraft.value.push({ scope_kind: "country", scope_value: form.value?.country || "", param_code: "sales_index", value: 0 });
const scopePlaceholder = (kind: string): string =>
  ({ country: "BY", city: "Минск", lfl: "lfl", store_type: "ТЦ", store: "114" })[kind] || "";

const saveParams = async () => {
  busy.value = true;
  error.value = "";
  try {
    const saved = await api.saveParams(cardId, paramDraft.value);
    paramDraft.value = saved.map((p) => ({ ...p }));
    if (form.value) form.value.params = saved;
    note.value = "Параметры периода сохранены. Массовые операции возьмут новые значения.";
    paramsOpen.value = false;
  } catch (e) {
    error.value = retailErrText(e, "Не удалось сохранить параметры");
  } finally {
    busy.value = false;
  }
};

// ─────────── переопределение LFL ───────────
const lflRow = ref<RetailRow | null>(null);
const lflNew = ref("");
const lflReason = ref("");
const openLfl = (r: RetailRow) => {
  if (!canOverride.value || !editable.value) return;
  lflRow.value = r;
  lflNew.value = r.lfl_override ? r.lfl_effective : "";
  lflReason.value = "";
};
const saveLfl = async () => {
  if (!lflRow.value) return;
  busy.value = true;
  error.value = "";
  try {
    form.value = await api.lflOverride(cardId, {
      code_cfo: lflRow.value.code_cfo,
      lfl_status: lflNew.value,
      reason: lflReason.value.trim()
    });
    note.value = "LFL-статус периода обновлён. Справочник не изменён.";
    lflRow.value = null;
  } catch (e) {
    error.value = retailErrText(e, "Не удалось переопределить LFL-статус");
  } finally {
    busy.value = false;
  }
};

// ─────────── источники данных (ТЗ доработок п. 1.1) ───────────
const sourcesAsOf = computed(() => {
  const at = form.value?.sources?.map((s) => s.loaded_at).sort()[0];
  return at ? new Date(at).toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" }) : "";
});
// «Новая стратегия» и факт по начислениям — необязательные колонки: их пустота
// не повод для предупреждения, а ошибка — повод.
const badSources = computed(() =>
  (form.value?.sources || []).filter((s) => !!s.error || (s.rows === 0 && ["stores", "fact", "strategy"].includes(s.code)))
);

// ─────────── массовые операции ───────────
const bulkOpen = ref(false);
const bulkOp = ref<RetailBulkRequest["op"]>("copy_scenario");
const bulkScope = ref<RetailBulkRequest["scope"]>("filtered");
const bulkMonthMode = ref<"all" | "one">("one");
const bulkSource = ref<"strategy" | "tactic" | "fact" | "new_strategy">("strategy");
/** Месяц применения копирования сценария (ТЗ доработок п. 2.3), дефолт — месяц тактики. */
const bulkApplyMonth = ref(0);
const bulkCoef = ref<number | null>(null);
const bulkTarget = ref<number | null>(null);
const bulkBaseMonths = ref("");
const bulkIndexBase = ref<"fact_prev_month" | "fact_prev_year" | "approved_prev" | "strategy">("fact_prev_month");
// Дефолт — стратегия того же месяца (правка 08.09.2026 п. 20). Прежний вариант
// «план текущего месяца» остался в списке: по нему считались утверждённые периоды.
const bulkPayrollBase = ref<NonNullable<RetailBulkRequest["payroll_base"]>>("strategy");
/** Оборотная часть аренды — переходный режим до расчёта от условий договора (§12 п.13). */
const bulkRentTurnover = ref(false);
const bulkResetManual = ref(false);
const bulkPreview = ref<RetailBulkResult | null>(null);
/** Подпись параметров, на которых сделан предпросмотр: применять можно только её. */
const previewSig = ref("");

const bulkRequest = (preview: boolean): RetailBulkRequest => {
  const req: RetailBulkRequest = {
    op: bulkOp.value,
    scope: bulkScope.value,
    preview,
    reset_manual: bulkResetManual.value,
    months: bulkOp.value === "copy_scenario" ? [bulkApplyMonth.value || form.value?.month || selMonth.value] : bulkMonthMode.value === "one" ? [selMonth.value] : []
  };
  if (bulkScope.value === "filtered") req.code_cfos = filteredRows.value.map((r) => r.code_cfo);
  if (bulkScope.value === "selected") req.code_cfos = [...selected];
  if (bulkOp.value === "copy_scenario") {
    req.source = bulkSource.value;
    req.coefficient = bulkCoef.value ?? 0;
    req.coefficient_set = bulkCoef.value != null;
  }
  if (bulkOp.value === "distribute") {
    req.target_total = bulkTarget.value ?? 0;
    req.base_months = bulkBaseMonths.value.split(/[,;\s]+/).map(Number).filter((n) => n >= 1 && n <= 12);
  }
  if (bulkOp.value === "sales_index") req.index_base = bulkIndexBase.value;
  if (bulkOp.value === "payroll") req.payroll_base = bulkPayrollBase.value;
  if (bulkOp.value === "rent") req.rent_turnover = bulkRentTurnover.value;
  return req;
};

const sig = () => JSON.stringify(bulkRequest(true));
const previewFresh = computed(() => !!bulkPreview.value && previewSig.value === sig());
const invalidatePreview = () => {
  // Предпросмотр устаревает при любом изменении параметров: применить diff,
  // посчитанный для других настроек, — ровно тот способ потерять данные,
  // от которого §5 и требует предпросмотр.
  if (bulkPreview.value) previewSig.value = "";
};

const runPreview = async () => {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    const s = sig();
    bulkPreview.value = await api.bulk(cardId, bulkRequest(true));
    previewSig.value = s;
  } catch (e) {
    error.value = retailErrText(e, "Предпросмотр не выполнен");
  } finally {
    busy.value = false;
  }
};

const applyBulk = async () => {
  busy.value = true;
  error.value = "";
  try {
    const res = await api.bulk(cardId, bulkRequest(false));
    note.value = `Операция применена: изменено ячеек ${res.changed}, защищено ${res.protected_manual}, пропущено ${res.skipped_no_base}.`;
    bulkPreview.value = null;
    // Массовая операция пишет в БД напрямую — локальные правки к тем же ячейкам
    // после неё бессмысленны, перечитываем форму целиком.
    dropDraft();
    await reload();
  } catch (e) {
    error.value = retailErrText(e, "Операция не выполнена");
  } finally {
    busy.value = false;
  }
};

// ─────────── загрузка / сохранение ───────────
const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    const [f, c] = await Promise.all([
      api.form(cardId),
      cardApi.card(cardId).then((r) => r.card).catch(() => null)
    ]);
    form.value = f;
    card.value = c;
    if (route.query.readonly === "1" || !f.editable || c?.status === "on_approval") {
      detail.value = false;
      groupBy.value = "lfl_status";
      groupOpen.clear();
      bulkOpen.value = false;
    }
    if (!selMonth.value || !f.months.includes(selMonth.value)) selMonth.value = f.month;
    if (!bulkApplyMonth.value || !f.months.includes(bulkApplyMonth.value)) bulkApplyMonth.value = f.month;
    paramDraft.value = (f.params || []).map((p) => ({ ...p }));
  } catch (e) {
    error.value = retailErrText(e, "Не удалось загрузить форму");
  } finally {
    loading.value = false;
  }
};

const reload = async () => {
  try {
    form.value = await api.form(cardId);
    paramDraft.value = (form.value.params || []).map((p) => ({ ...p }));
  } catch (e) {
    error.value = retailErrText(e, "Не удалось перечитать форму");
  }
};

/**
 * Проверка формы. silent=true — фоновый прогон при открытии: результат нужен,
 * чтобы кнопка отправки сразу была в правильном состоянии, но разворачивать
 * список из 119 «не заполнен план» никто не просил.
 */
const runValidate = async (silent = false) => {
  if (!silent) busy.value = true;
  try {
    report.value = await api.validate(cardId);
    if (!silent) reportOpen.value = true;
  } catch (e) {
    if (!silent) error.value = retailErrText(e, "Проверка не выполнена");
  } finally {
    busy.value = false;
  }
};

const save = async () => {
  if (!form.value || !dirty.value) return;
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    const cells = [...edits.entries()].map(([k, amount]) => {
      const [code, metric, year, month] = k.split("|");
      return {
        code_cfo: Number(code),
        metric: metric as "sales",
        year: Number(year),
        month: Number(month),
        amount,
        source: "manual"
      };
    });
    const comments = [...commentEdits.entries()].map(([code_cfo, comment]) => ({ code_cfo, comment }));
    form.value = await api.saveForm(cardId, { cells, comments });
    dropDraft();
    note.value = `Сохранено: ячеек ${cells.length}, комментариев ${comments.length}.`;
    // Пересчёт валидаций после сохранения: отчёт должен относиться к тому, что
    // в базе, а не к тому, что было до сохранения (от него зависит can_submit).
    await runValidate(!reportOpen.value);
  } catch (e) {
    error.value = retailErrText(e, "Не удалось сохранить ввод");
  } finally {
    busy.value = false;
  }
};

// ─────────── экспорт / импорт ───────────
const fileInput = ref<HTMLInputElement | null>(null);

const doExport = async () => {
  busy.value = true;
  try {
    const blob = await api.exportXlsx(cardId);
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `retail-${form.value?.country || cardId}-${form.value?.year}-${String(form.value?.month).padStart(2, "0")}.xlsx`;
    a.click();
    URL.revokeObjectURL(url);
  } catch (e) {
    error.value = retailErrText(e, "Экспорт не выполнен");
  } finally {
    busy.value = false;
  }
};

// Пример файла для импорта: те же колонки, что ждёт разбор, плюс подсказки
// строками-комментариями. Загрузка примера без правок ничего не меняет.
const doImportTemplate = async () => {
  busy.value = true;
  error.value = "";
  try {
    const blob = await api.importTemplateXlsx(cardId);
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `retail-import-example-${form.value?.country || cardId}-${form.value?.year}-${String(form.value?.month).padStart(2, "0")}.xlsx`;
    a.click();
    URL.revokeObjectURL(url);
    note.value = "Пример скачан: заполните колонки месяцев по своим магазинам и загрузите файл кнопкой «Импорт».";
  } catch (e) {
    error.value = retailErrText(e, "Не удалось скачать пример файла");
  } finally {
    busy.value = false;
  }
};

const doImport = async (e: Event) => {
  const input = e.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  busy.value = true;
  error.value = "";
  try {
    const res = await api.importXlsx(cardId, file);
    note.value = `Импорт: строк в файле ${res.rows_in_file}, сохранено ячеек ${res.cells_saved}, пропущено ${res.skipped}.`;
    if (res.issues?.length) error.value = res.issues.slice(0, 5).map((i) => i.message).join("; ");
    await reload();
  } catch (err) {
    error.value = retailErrText(err, "Импорт не выполнен");
  } finally {
    busy.value = false;
  }
};

onMounted(async () => {
  // Порядок и сортировка столбцов — настройка пользователя, поэтому читается до
  // первого рендера сетки: иначе таблица дёрнется сразу после открытия.
  loadColPrefs();
  await load();
  restoreDraft();
  // Сетка появляется по v-if="form" — виртуализатору нужен уже существующий
  // контейнер прокрутки, иначе getScrollElement() вернёт null на старте.
  await nextTick();
  await initVirtualizer();
  api.presets(RETAIL_FORM_CODE)
    .then((list) => {
      presetList.value = list;
      const last = list.find((p) => p.name === "");
      if (last) applyPayload(last.payload);
      if (form.value && (!selMonth.value || !form.value.months.includes(selMonth.value))) selMonth.value = form.value.month;
      // Approval opens on the required collapsed LFL totals even when an old
      // personal preset previously saved a detailed store view.
      if (route.query.readonly === "1" || !form.value?.editable || card.value?.status === "on_approval") {
        detail.value = false;
        groupBy.value = "lfl_status";
        groupOpen.clear();
      }
    })
    .catch(() => {
      /* пресетов может не быть — открываемся на дефолтном представлении */
    });
  reportOpen.value = false;
  runValidate(true);
});

onBeforeUnmount(() => {
  if (draftTimer) clearTimeout(draftTimer);
  if (presetTimer) clearTimeout(presetTimer);
  persistDraft();
  virtScope?.stop();
});
</script>

<style scoped>
/* Подпись курса в шапке: справочная величина, поэтому приглушена. */
.fx-note { color: var(--text-muted); }

.page-retail {
  display: flex;
  flex-direction: column;
  gap: var(--sp-5);
}
.page-retail > .page-header { position: sticky; top: 0; z-index: 8; background: var(--bg-surface); padding-top: var(--sp-3); }
.banner {
  margin: 0;
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-4);
  font-size: var(--fs-sm);
}
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.banner-pos { background: var(--pos-soft); color: var(--pos-strong); }
.banner-warn { background: var(--warn-soft); color: var(--warn); }
.banner-info { background: var(--info-soft); color: var(--info); }
.src-line { display: flex; flex-wrap: wrap; align-items: center; gap: 4px; margin: 0; font-size: var(--fs-xs); color: var(--text-muted); font-family: var(--font-mono); }
.src-bad { color: var(--warn); font-weight: 600; }
.tactic-period { display: block; font-size: 1.3rem; color: var(--accent); }
.status-group { cursor: pointer; background: var(--bg-surface-2); font-weight: 600; }
.group-toggle { border: 0; background: transparent; color: inherit; cursor: pointer; font: inherit; display: inline-flex; align-items: center; gap: var(--sp-2); }
.group-store .cmt-input { display: block; max-width: 220px; margin-top: 2px; }
.retail-review { padding: var(--sp-4); display: grid; gap: var(--sp-2); margin-bottom: var(--sp-4); }
.lock-note { margin-left: var(--sp-3); color: var(--neg); }
.hidden-file { display: none; }
.link-btn {
  background: none;
  border: none;
  padding: 0 0 0 var(--sp-3);
  color: var(--text-link);
  font: inherit;
  cursor: pointer;
  text-decoration: underline;
}

/* ─── фильтры и представление ─── */
.filter-card { padding: var(--sp-4) var(--sp-5); display: flex; flex-direction: column; gap: var(--sp-4); }
.flt { display: flex; flex-wrap: wrap; align-items: center; gap: var(--sp-3); }
.flt-view { border-top: 1px solid var(--border); padding-top: var(--sp-4); }
.flt-search { min-width: 260px; flex: 0 1 320px; width: auto; }
/* Глобальный .select растянут на 100 % — в панели фильтров это дало бы столбик
   из семи полей во всю ширину вместо одной строки. */
.flt > .select,
.preset-box .select { width: auto; min-width: 132px; max-width: 210px; flex: 0 0 auto; }
.flt-chk { white-space: nowrap; }
.flt-spacer { flex: 1 1 auto; }
.sel-note { font-size: var(--fs-sm); color: var(--accent); white-space: nowrap; }
.preset-box { display: flex; align-items: center; gap: var(--sp-2); }

/* Выпадающее меню «Колонки» раскрывается за пределы карточки фильтров: с overflow:hidden оно обрезалось. */
/* z-index выше липких заголовков таблицы (3 и 4) и ниже липкой шапки страницы (8): иначе строка заголовков рисуется поверх меню. */
.filter-card { overflow: visible; position: relative; z-index: 5; }
.cols-menu { position: relative; }
.bulk-help { margin: 0; padding: var(--sp-3) var(--sp-5); font-size: var(--fs-sm); color: var(--text-secondary); }
.bulk-closed { margin: var(--sp-3) var(--sp-5) 0; }
.cols-hint { display: block; margin-left: calc(var(--sp-5) + var(--sp-2)); font-size: var(--fs-xs); color: var(--text-muted); }
.cols-menu > summary { list-style: none; cursor: pointer; }
.cols-menu > summary::-webkit-details-marker { display: none; }
.cols-pop {
  position: absolute;
  top: calc(100% + 4px);
  /* Раскрывается влево от кнопки: справа от неё на узком экране не хватает места. */
  right: 0;
  z-index: var(--z-dropdown);
  width: 260px;
  max-height: 60vh;
  overflow: auto;
  padding: var(--sp-5);
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--rd-5);
  box-shadow: var(--shadow-pop);
}
.cols-fixed { margin: 0 0 var(--sp-4); font-size: var(--fs-xs); color: var(--text-muted); }
.cols-grp { display: flex; flex-direction: column; gap: var(--sp-2); margin-bottom: var(--sp-5); }
.cols-grp-t {
  font-size: var(--fs-2xs);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-muted);
}

/* ─── массовые операции ─── */
.hdr-hint { font-size: var(--fs-sm); color: var(--text-muted); flex: 1 1 300px; }
.bulk-form {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--sp-4);
  padding: var(--sp-5);
}
.bulk-form .form-field { min-width: 170px; }
.bulk-form .form-field.wide { min-width: 240px; }
.bulk-chk { white-space: nowrap; }
/* Пояснение к формуле ФОТ: подпись в потоке панели, не поле ввода. */
.bulk-note { font-size: var(--fs-sm); color: var(--text-muted); max-width: 420px; }
/* Подсказка по аренде: правка ТЗ §30 — поле ввода, справочный факт рядом. */
.bulk-hint {
  margin: 0;
  font-size: var(--fs-2xs);
  color: var(--warn);
  display: flex;
  align-items: flex-start;
  gap: 4px;
  max-width: 520px;
}
.bulk-go { margin-left: auto; }

/* ─── валидации ─── */
.val-ok { margin: 0; padding: var(--sp-5); color: var(--pos); font-size: var(--fs-sm); }
.val-lists { display: grid; grid-template-columns: repeat(auto-fit, minmax(340px, 1fr)); gap: var(--sp-5); padding: var(--sp-5); }
.val-col { display: flex; flex-direction: column; gap: var(--sp-3); min-width: 0; }
.val-h { font-size: var(--fs-sm); font-weight: var(--fw-semibold); }
.val-h-neg { color: var(--neg); }
.val-h-warn { color: var(--warn); }
.val-ul { margin: 0; padding: 0; list-style: none; max-height: 240px; overflow: auto; display: flex; flex-direction: column; gap: 2px; }
.val-link {
  display: block;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  padding: 2px var(--sp-2);
  font: inherit;
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  cursor: pointer;
  border-radius: var(--rd-3);
}
.val-link:hover { background: var(--bg-surface-3); color: var(--text-strong); }
.val-more { margin: 0; font-size: var(--fs-xs); color: var(--text-muted); }

/* ─── сетка ─── */
.grid-card { display: flex; flex-direction: column; }
/* Горизонтальный скролл живёт ВНУТРИ контейнера: страница по горизонтали не
   двигается, иначе шапка и боковая навигация уезжают вместе с таблицей. */
.grid-scroll {
  overflow: auto;
  max-height: calc(100vh - 260px);
  overscroll-behavior-x: contain;
}
.grid-table { min-width: max-content; }
.grid-table thead th { z-index: 3; }
/* Порядок и сортировка столбцов (требование «Фронт таблицы»). Иконка сортировки
   держится приглушённой, пока столбец не выбран: иначе шапка рябит стрелками. */
.sortable { cursor: pointer; user-select: none; }
.sortable:hover { background: var(--bg-tonal); }
.sortable .s-ic { opacity: 0.35; width: 12px; height: 12px; vertical-align: -1px; }
.sortable.sorted .s-ic { opacity: 1; color: var(--accent); }
.sortable.sorted { color: var(--text-strong); }
.movable { cursor: grab; }
.movable .g-ic { opacity: 0.3; width: 11px; height: 11px; vertical-align: -1px; }
.movable:hover .g-ic { opacity: 0.7; }
.mv-btns { display: none; margin-left: 2px; white-space: nowrap; }
.movable:hover .mv-btns { display: inline-flex; }
.mv {
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-secondary);
  border-radius: var(--rd-2);
  line-height: 1;
  padding: 0 3px;
  cursor: pointer;
}
.mv:hover { color: var(--text-strong); border-color: var(--accent); }
.grid-table tbody td { height: 32px; }

.st { position: sticky; background: var(--bg-surface); z-index: 1; }
.grid-table thead th.st { z-index: 4; background: var(--bg-surface-2); }
.grid-table tfoot td.st { z-index: 3; background: var(--bg-surface-2); }
.st-1 { left: 0; width: 32px; min-width: 32px; padding-left: var(--sp-3) !important; padding-right: 0 !important; }
.st-2 { left: 32px; width: 60px; min-width: 60px; }
.st-3 { left: 92px; width: 125px; min-width: 125px; }
.st-4 { left: 217px; width: 230px; min-width: 230px; max-width: 230px; }
.st-5 { left: 447px; width: 120px; min-width: 120px; box-shadow: 1px 0 0 var(--border); }
.grid-table tbody tr:hover td.st { background: var(--bg-surface-2); }
.row-sel td.st, .grid-table tbody tr.row-sel:hover td.st { background: var(--accent-soft); }
.row-sel td { background: var(--accent-soft); }
.row-dirty td.st-2 { color: var(--accent); font-weight: var(--fw-semibold); }
.row-hit td { background: var(--warn-soft); }

.s-name { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.s-ovr { display: block; font-size: var(--fs-2xs); color: var(--warn); }
.pad-row td { padding: 0 !important; border: none !important; }
.empty-cell { padding: var(--sp-8) var(--sp-5); color: var(--text-muted); text-align: center; }

.cmp-h, .cmp-c { color: var(--text-secondary); }
.cmp-c { background: var(--bg-surface-2); }
.inp-h { color: var(--accent); }
.inp-c { padding: 1px 2px !important; }
.ind-h, .ind-c { white-space: nowrap; }
.ind-stale { color: var(--text-muted); font-style: italic; }
.ro-c { color: var(--text-secondary); }

.cell-input {
  width: 100px;
  padding: 3px var(--sp-3);
  text-align: right;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--fs-sm);
  border: 1px solid transparent;
  border-radius: var(--rd-3);
  background: var(--bg-surface);
  color: var(--text-primary);
}
.cell-input:hover:not(:disabled) { border-color: var(--border); }
.cell-input:focus {
  outline: none;
  border-color: var(--border-focus);
  box-shadow: var(--shadow-focus);
  background: var(--bg-surface);
}
.cell-input:disabled { background: var(--bg-surface-3); color: var(--text-muted); }
.cell-edited .cell-input { background: var(--accent-soft); border-color: var(--accent); }
.cell-derived .cell-input { background: var(--info-soft); }

.lfl-chip {
  border: 1px solid var(--border);
  background: var(--bg-surface-2);
  color: var(--text-secondary);
  border-radius: var(--rd-pill);
  padding: 0 var(--sp-3);
  font-size: var(--fs-2xs);
  font-family: inherit;
  cursor: pointer;
  white-space: nowrap;
}
.lfl-chip:disabled { cursor: default; }
.lfl-lfl { color: var(--pos); border-color: var(--pos); }
.lfl-new, .lfl-xxx { color: var(--info); border-color: var(--info); }
.lfl-under1y { color: var(--warn); border-color: var(--warn); }
.lfl-closed { color: var(--text-muted); }

.cmt-h, .cmt-c { min-width: 220px; }
.cmt-input { width: 100%; padding: 2px var(--sp-3); font-size: var(--fs-sm); }
.cmt-input.need-why { border-color: var(--neg); }

.grid-foot {
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-5);
  margin: 0;
  padding: var(--sp-3) var(--sp-5);
  border-top: 1px solid var(--border);
  font-size: var(--fs-xs);
  color: var(--text-muted);
}
.grid-warn { color: var(--warn); }
.grid-dirty { color: var(--accent); margin-left: auto; }

/* ─── модалки ─── */
.pm-hint { margin: 0 0 var(--sp-4); font-size: var(--fs-sm); color: var(--text-muted); }
.p-by { font-size: var(--fs-xs); }
.p-at { display: block; color: var(--text-muted); }
</style>
