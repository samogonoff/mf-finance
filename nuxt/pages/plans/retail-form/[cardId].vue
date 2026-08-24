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
        <p class="page-subtitle">
          Форма ввода TPL-TO-RETAIL
          <span v-if="form"> · {{ form.country }} · {{ form.legal_entity || "ЮЛ не задано" }}</span>
          <span v-if="form"> · ввод в {{ form.nat_currency }}</span>
          <span v-if="form"> · {{ filteredRows.length }} из {{ form.rows.length }} магазинов</span>
          <span v-if="form && !form.editable" class="lock-note">
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

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>
    <p v-if="draftNote" class="banner banner-info">
      {{ draftNote }}
      <button type="button" class="link-btn" @click="dropDraft">Отменить черновик</button>
    </p>
    <p v-if="form && !form.editable" class="banner banner-warn">
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
          <select v-if="!detail" v-model="groupBy" class="select">
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
            а не из этой панели — задайте их в «Параметрах периода».
          </span>
        </div>
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
          <label class="form-field">
            <span class="form-label">Месяцы</span>
            <select v-model="bulkMonthMode" class="select" @change="invalidatePreview">
              <option value="all">все плановые</option>
              <option value="one">только {{ monthLabel(selMonth) }}</option>
            </select>
          </label>

          <template v-if="bulkOp === 'copy_scenario'">
            <label class="form-field">
              <span class="form-label">Источник</span>
              <select v-model="bulkSource" class="select" @change="invalidatePreview">
                <option value="strategy">Стратегия</option>
                <option value="tactic">Утверждённая тактика</option>
                <option value="fact">Факт</option>
              </select>
            </label>
            <label class="form-field">
              <span class="form-label">Год источника</span>
              <input v-model.number="bulkSourceYear" class="input" type="number" @input="invalidatePreview" />
            </label>
            <label class="form-field">
              <span class="form-label">Коэффициент × k</span>
              <input v-model.number="bulkCoef" class="input" type="number" step="0.01" placeholder="1" @input="invalidatePreview" />
            </label>
            <label class="form-field">
              <span class="form-label">Надбавка, %</span>
              <input v-model.number="bulkPctDelta" class="input" type="number" step="0.1" placeholder="0" @input="invalidatePreview" />
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

          <label v-if="bulkOp === 'payroll'" class="form-field">
            <span class="form-label">База ограничения ФОТ</span>
            <select v-model="bulkPayrollBase" class="select" @change="invalidatePreview">
              <option value="strategy">стратегия</option>
              <option value="fact_prev_year">факт прошлого года</option>
              <option value="approved_prev">утверждённая тактика</option>
            </select>
          </label>

          <label class="checkbox bulk-chk">
            <input v-model="bulkResetManual" type="checkbox" @change="invalidatePreview" />
            сбросить защиту ручных значений
          </label>

          <button class="btn btn-primary bulk-go" :disabled="busy || !editable" @click="runPreview">
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
          <span class="badge" :class="report.can_submit ? 'badge-pos' : 'badge-neg'">
            {{ report.can_submit ? "можно отправлять на согласование" : "отправка заблокирована" }}
          </span>
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
                <th class="st st-2 col-num">ЦФО</th>
                <th class="st st-3">{{ detail ? "Магазин" : groupTitle }}</th>
                <th class="st st-4">{{ detail ? "Город" : "Магазинов" }}</th>
                <th v-for="c in visibleAttrCols" :key="c.key" :class="c.num ? 'col-num' : ''">{{ c.title }}</th>
                <th v-for="c in visibleCmpCols" :key="c.key" class="col-num cmp-h" :title="c.hint">{{ c.title }}</th>
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
                <td class="st st-3">
                  <span class="s-name" :title="vr.row.cfo">{{ vr.row.cfo || "—" }}</span>
                  <span v-if="vr.row.lfl_override" class="s-ovr" :title="vr.row.lfl_reason">LFL переопределён</span>
                </td>
                <td class="st st-4">{{ vr.row.city || "—" }}</td>

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
                    :placeholder="needsComment(vr.row.code_cfo) ? 'обязателен: есть предупреждения' : ''"
                    @input="setComment(vr.row.code_cfo, ($event.target as HTMLInputElement).value)"
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
              <tr v-for="g in groupRows" :key="g.key">
                <td class="st st-1"></td>
                <td class="st st-2 col-num">—</td>
                <td class="st st-3"><span class="s-name">{{ g.key || "не задано" }}</span></td>
                <td class="st st-4 col-num">{{ g.rows.length }}</td>
                <td v-for="c in visibleAttrCols" :key="c.key" :class="c.num ? 'col-num' : ''">—</td>
                <td v-for="c in visibleCmpCols" :key="c.key" class="col-num cmp-c">{{ moneyOrDash(g.ind[c.key] as number | null) }}</td>
                <td v-for="m in monthCols" :key="'gm' + m" class="col-num ro-c">{{ moneyOrDash(g.months[m] ?? null) }}</td>
                <td v-for="c in visibleIndCols" :key="c.key" class="col-num ind-c">{{ indText(g, c.key) }}</td>
              </tr>
            </tbody>

            <tfoot>
              <tr>
                <td class="st st-1"></td>
                <td class="st st-2"></td>
                <td class="st st-3">Итого по выборке</td>
                <td class="st st-4 col-num">{{ filteredRows.length }}</td>
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
        :can-submit="report ? report.can_submit : true"
        :submit-hint="report && !report.can_submit ? `блокирующих замечаний: ${report.blocking.length}` : ''"
        @changed="reload"
      />
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
import NumberField from "~/components/NumberField.vue";
import PlansModal from "~/components/plans/PlansModal.vue";
import SkeletonTable from "~/components/SkeletonTable.vue";
import { useGridNav } from "~/composables/useGridNav";
import { useMpConditions, type PlanCard } from "~/composables/useMpConditions";
import { useScope } from "~/composables/useScope";
import {
  BULK_OP_LABELS, LFL_LABELS, MONTH_LABELS, PARAM_LABELS, RETAIL_FORM_CODE,
  cellKey, iferrorDiv, iferrorRatioMinus1, noBaseline, resolveParam, retailErrText, useRetail,
  type RetailBulkRequest, type RetailBulkResult, type RetailForm, type RetailIndicators,
  type RetailParam, type RetailRow, type RetailValidationReport, type ViewPreset
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

/** Несохранённый ввод: ключ ячейки → значение (null = «пусто», это не 0 — см. V-01). */
const edits = reactive(new Map<string, number | null>());
/** Несохранённые комментарии: код ЦФО → текст. */
const commentEdits = reactive(new Map<number, string>());

const editable = computed(() => !!form.value?.editable);
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
const detail = ref(true);
const groupBy = ref("city");
const selMonth = ref(1);

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
  { key: "strategy", title: "Стратегия", hint: "Стратегический план на месяц" },
  { key: "tactic_approved", title: "Утв. тактика", hint: "Ранее утверждённая тактика на месяц" }
];
const IND_COLS: ColDef[] = [
  { key: "plan_done_pct", title: "% вып.", hint: "Факт / тактика месяца" },
  { key: "lfl_tactic", title: "LFL такт.", hint: "Тактика / факт того же месяца ПГ − 1" },
  { key: "lfm_tactic", title: "LFM такт.", hint: "Тактика / факт предыдущего месяца − 1" },
  { key: "vs_strategy_pct", title: "% к страт.", hint: "Тактика / стратегия − 1" },
  { key: "period_total", title: "Итого период", hint: "Сумма плановых месяцев" },
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

const visibleAttrCols = computed(() => ATTR_COLS.filter((c) => cols[c.key]));
const visibleCmpCols = computed(() => CMP_COLS.filter((c) => cols[c.key]));
const visibleIndCols = computed(() => IND_COLS.filter((c) => cols[c.key]));

/** Колонки ввода: в режиме «месяц → магазины» заполняется один месяц по всем магазинам. */
const monthCols = computed<number[]>(() => {
  const f = form.value;
  if (!f) return [];
  return mode.value === "month_stores" ? [selMonth.value] : f.months;
});
const colCount = computed(
  () => 4 + visibleAttrCols.value.length + visibleCmpCols.value.length + monthCols.value.length + visibleIndCols.value.length + (detail.value ? 1 : 0)
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
  filteredRows.value.map((row, index) => ({ row, index, ind: liveInd(row), stale: rowDirty(row.code_cfo) }))
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
  const s = { fpy: 0, fpm: 0, fcur: 0, strat: 0, appr: 0, tactic: 0, period: 0, yexp: 0 };
  const months: Record<number, number> = {};
  for (const m of f.months) months[m] = 0;
  for (const r of rows) {
    const i = liveInd(r);
    s.fpy += i.fact_prev_year_month ?? 0;
    s.fpm += i.fact_prev_month ?? 0;
    s.fcur += i.fact_cur_month ?? 0;
    s.strat += i.strategy ?? 0;
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
      fact_prev_year_month: s.fpy,
      fact_prev_month: s.fpm,
      fact_cur_month: s.fcur,
      strategy: s.strat,
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
    const k = String((r as unknown as Record<string, unknown>)[groupBy.value] ?? "");
    if (!map.has(k)) map.set(k, []);
    map.get(k)!.push(r);
  }
  return [...map.entries()]
    .sort((a, b) => a[0].localeCompare(b[0], "ru"))
    .map(([k, rows]) => aggregate(rows, k));
});

const totals = computed<AggRow>(() => aggregate(filteredRows.value, "Итого"));

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
  cols: () => monthCols.value.length,
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

// ─────────── массовые операции ───────────
const bulkOpen = ref(false);
const bulkOp = ref<RetailBulkRequest["op"]>("copy_scenario");
const bulkScope = ref<RetailBulkRequest["scope"]>("filtered");
const bulkMonthMode = ref<"all" | "one">("all");
const bulkSource = ref<"strategy" | "tactic" | "fact">("strategy");
const bulkSourceYear = ref(0);
const bulkCoef = ref<number | null>(null);
const bulkPctDelta = ref<number | null>(null);
const bulkTarget = ref<number | null>(null);
const bulkBaseMonths = ref("");
const bulkIndexBase = ref<"fact_prev_month" | "fact_prev_year" | "approved_prev" | "strategy">("fact_prev_month");
const bulkPayrollBase = ref<"strategy" | "fact_prev_year" | "approved_prev">("strategy");
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
    months: bulkMonthMode.value === "one" ? [selMonth.value] : []
  };
  if (bulkScope.value === "filtered") req.code_cfos = filteredRows.value.map((r) => r.code_cfo);
  if (bulkScope.value === "selected") req.code_cfos = [...selected];
  if (bulkOp.value === "copy_scenario") {
    req.source = bulkSource.value;
    req.source_year = bulkSourceYear.value || form.value?.year || 0;
    req.coefficient = bulkCoef.value ?? 0;
    req.percent_delta = (bulkPctDelta.value ?? 0) / 100;
  }
  if (bulkOp.value === "distribute") {
    req.target_total = bulkTarget.value ?? 0;
    req.base_months = bulkBaseMonths.value.split(/[,;\s]+/).map(Number).filter((n) => n >= 1 && n <= 12);
  }
  if (bulkOp.value === "sales_index") req.index_base = bulkIndexBase.value;
  if (bulkOp.value === "payroll") req.payroll_base = bulkPayrollBase.value;
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
    if (!selMonth.value || !f.months.includes(selMonth.value)) selMonth.value = f.month;
    if (!bulkSourceYear.value) bulkSourceYear.value = f.year;
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
.page-retail {
  display: flex;
  flex-direction: column;
  gap: var(--sp-5);
}
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

.cols-menu { position: relative; }
.cols-menu > summary { list-style: none; cursor: pointer; }
.cols-menu > summary::-webkit-details-marker { display: none; }
.cols-pop {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
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
.grid-table tbody td { height: 32px; }

.st { position: sticky; background: var(--bg-surface); z-index: 1; }
.grid-table thead th.st { z-index: 4; background: var(--bg-surface-2); }
.grid-table tfoot td.st { z-index: 3; background: var(--bg-surface-2); }
.st-1 { left: 0; width: 32px; min-width: 32px; padding-left: var(--sp-3) !important; padding-right: 0 !important; }
.st-2 { left: 32px; width: 60px; min-width: 60px; }
.st-3 { left: 92px; width: 230px; min-width: 230px; max-width: 230px; }
.st-4 { left: 322px; width: 120px; min-width: 120px; box-shadow: 1px 0 0 var(--border); }
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
