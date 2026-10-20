<!--
  Форма этапа 1.5 «Расходы центров прибыли» (ТЗ §3, §4, §7).

  Строка формы — ПАРА «ЦФО × статья PL», период — плановый год целиком:
  двенадцать колонок ввода и двенадцать колонок факта прошлого года справочно
  (ТЗ §1.3, §2.5). Состав строк приходит из источника затрат, руками строки не
  добавляются.

  Реализовано: сетка и ввод (§7.8) с навигацией как в Excel и вставкой диапазона,
  фильтры и поиск (§7.1), группировка со свёрткой по группе PL (§7.6), порции по
  прокрутке (§7.10), размножение и очистка строки, блокировка ввода на закрытой
  карточке (§1.1).

  Ещё НЕ реализовано (следующие фазы плана): реестры §5, расчёты §6, свод и
  контрольные сверки §8, валидации §9, экспорт/импорт §7.9, публикация §10,
  перетаскивание и закрепление колонок §7.2/§7.5.
-->
<template>
  <div class="page-exp">
    <header class="page-header">
      <div>
        <h1 class="page-title">{{ card?.title || "Расходы ЦП" }} · {{ planYear }}</h1>
        <p class="page-subtitle">
          {{ form?.total ?? 0 }} строк · ввод в {{ card?.currency || "—" }} ·
          план {{ viewTotalLabel }} {{ money(shownPlanTotal) }}
          <span v-if="sourceNote" class="src-note" :title="sourceNoteTitle"> · {{ sourceNote }}</span>
          <span v-if="card?.locked" class="lock-note"><Icon name="lucide:lock" /> период закрыт на запись</span>
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink v-if="card" :to="`/plans/exp/${card.pl_id}`" class="btn btn-ghost">
          <Icon name="lucide:arrow-left" /> К комплекту
        </NuxtLink>
        <NuxtLink :to="`/plans/exp-registry/${cardId}`" class="btn btn-ghost">
          <Icon name="lucide:list-checks" /> Реестры
        </NuxtLink>
        <NuxtLink :to="`/plans/exp-summary/${cardId}`" class="btn btn-ghost">
          <Icon name="lucide:layout-list" /> Свод
        </NuxtLink>
        <button class="btn btn-sm btn-ghost" @click="columnsOpen = !columnsOpen">
          <Icon name="lucide:columns-3" /> Колонки
        </button>
        <a class="btn btn-sm btn-ghost" :href="exportHref" target="_blank" rel="noopener">
          <Icon name="lucide:download" /> Экспорт
        </a>
        <button v-if="editable" class="btn btn-sm btn-ghost" :disabled="syncRunning" title="Перечитать состав строк и факт из источника затрат (OLAP). Процедура работает несколько минут." @click="runSync">
          <Icon name="lucide:refresh-cw" :class="{ spin: syncRunning }" /> Обновить из источника
        </button>
        <button class="btn btn-sm btn-ghost" :disabled="!editable" @click="importInput?.click()">
          <Icon name="lucide:upload" /> Импорт
        </button>
        <input ref="importInput" type="file" accept=".xlsx" class="hidden-file" @change="onImportFile" />
        <button class="btn btn-sm btn-ghost" :disabled="checking" @click="check">
          <Icon name="lucide:shield-check" :class="{ spin: checking }" /> Проверить
        </button>
        <button class="btn btn-sm btn-primary" :disabled="!editable || busy || !dirty" @click="save">
          <Icon name="lucide:save" :class="{ spin: busy }" /> Сохранить
          <span v-if="dirty" class="dirty-dot" :title="`несохранённых ячеек: ${drafts.size}`">●</span>
        </button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>
    <p v-if="syncRunning" class="banner banner-info">
      <Icon name="lucide:loader-circle" class="spin" />
      Идёт загрузка состава строк и факта из источника затрат. Процедура работает несколько минут —
      форму можно не закрывать, она обновится сама{{ rows.length ? " (введённое не потеряется)" : "" }}.
    </p>
    <p v-else-if="form?.sync?.state === 'error'" class="banner banner-neg">
      Источник затрат не прочитан: {{ form.sync.error || "причина неизвестна" }}.
      Повторить — кнопка «Обновить из источника».
    </p>
    <p v-else-if="syncDoneNew" class="banner banner-pos">
      Загрузка из источника завершена.
      <button class="btn btn-sm btn-ghost" @click="reload({ keepFilter: true })">Показать новые строки</button>
    </p>
    <p v-if="!syncRunning && form?.sync?.warning" class="banner banner-warn">{{ form.sync.warning }}</p>
    <p v-if="loaded && !editable" class="banner banner-warn">
      Только просмотр: карточка не в работе или период закрыт. Изменение утверждённых сумм —
      через возврат или переоткрытие периода с причиной.
    </p>

    <!-- Плашка мягкого режима (ответ 41): показывается и заполняющему, и
         согласующему, с суммой расхождения. -->
    <p v-if="softWarning" class="banner banner-warn">{{ softWarning }}</p>

    <!-- ===== Параметры периода и расчёт ФОТ (ТЗ §6.1) ===== -->
    <section v-if="isRetail" class="card params-card">
      <div class="params-row">
        <span class="params-title">ФОТ от продаж</span>
        <label class="params-f">
          Удельный вес
          <input
            class="input params-in"
            :value="paramShare"
            :disabled="!editable"
            inputmode="decimal"
            title="доля ФОТ в продажах; задаётся на страну, может переопределяться по магазину"
            @change="setParam('payroll_share', ($event.target as HTMLInputElement).value)"
          />
        </label>
        <label class="params-f">
          Порог базы
          <input
            class="input params-in"
            :value="paramCap"
            :disabled="!editable"
            inputmode="decimal"
            title="порог ограничивает БАЗУ начисления, а не сумму выплат; по умолчанию 1,06"
            @change="setParam('payroll_cap', ($event.target as HTMLInputElement).value)"
          />
        </label>
        <button class="btn btn-sm btn-ghost" :disabled="calcBusy" @click="runCalc('payroll', true)">
          <Icon name="lucide:calculator" :class="{ spin: calcBusy }" /> ФОТ (предпросмотр)
        </button>
        <button
          class="btn btn-sm btn-ghost"
          :disabled="calcBusy"
          title="Аренда по факту предыдущего периода: расчёта от оборота нет — в эталоне помесячные суммы"
          @click="runCalc('rent', true)"
        ><Icon name="lucide:building" /> Аренда (предпросмотр)</button>
        <span class="params-hint">
          ФОТ = MIN(план продаж; база × порог) × удельный вес. База — факт того же месяца прошлого года.
        </span>
      </div>

      <!-- Предпросмотр расчёта: ТЗ §6.1 требует показать прежние и новые
           значения и число строк, где сработало ограничение. -->
      <div v-if="calcResult" class="calc-prev">
        <div class="calc-head">
          <span class="badge badge-muted">строк: {{ calcResult.changed }}</span>
          <span v-if="calcResult.capped" class="badge badge-warn">ограничено порогом: {{ calcResult.capped }}</span>
          <span v-if="calcResult.no_base" class="badge badge-warn">без базы: {{ calcResult.no_base }}</span>
          <button type="button" class="link-btn" @click="calcResult = null">закрыть</button>
        </div>
        <ul class="calc-ul">
          <li v-for="(d, i) in calcResult.diff.slice(0, 25)" :key="i">
            ЦФО {{ d.code_cfo }}<template v-if="d.cfo"> · {{ d.cfo }}</template> · месяц {{ d.month }}:
            {{ d.before === null ? "пусто" : money(d.before) }} → {{ money(d.after) }}
            <span v-if="d.note" class="calc-note">{{ d.note }}</span>
          </li>
        </ul>
        <p v-if="calcResult.diff.length > 25" class="val-more">…и ещё {{ calcResult.diff.length - 25 }}.</p>
        <div class="calc-actions">
          <button class="btn btn-sm btn-primary" :disabled="!editable || calcBusy || !calcResult.changed" @click="runCalc(calcResult.op, false)">
            Применить расчёт
          </button>
          <button class="btn btn-sm btn-ghost" @click="calcResult = null">Отмена</button>
        </div>
      </div>
    </section>

    <!-- ===== Колонки и раскладка (ТЗ §7.2, §7.4) ===== -->
    <section v-if="columnsOpen" class="card cols-card">
      <div class="card-header">
        <span class="card-title">Колонки и раскладка</span>
        <span class="cols-hint">перетаскиванием меняется порядок, флажком — видимость</span>
        <button type="button" class="link-btn" @click="columnsOpen = false">скрыть</button>
      </div>
      <ul class="cols-list">
        <li
          v-for="(c, i) in attrColumns"
          :key="c.key"
          class="cols-item"
          :class="{ locked: c.locked }"
          draggable="true"
          @dragstart="dragFrom = i"
          @dragover.prevent
          @drop="dropColumn(i)"
        >
          <Icon v-if="!c.locked" name="lucide:grip-vertical" class="cols-grip" />
          <Icon v-else name="lucide:lock" class="cols-grip" :title="'колонка не перемещается и не скрывается'" />
          <label class="cols-label">
            <input type="checkbox" :checked="c.visible" :disabled="c.locked" @change="toggleColumn(c.key)" />
            {{ c.label }}
          </label>
        </li>
      </ul>
      <div class="cols-actions">
        <input v-model="presetName" class="input cols-name" placeholder="Имя раскладки" />
        <button class="btn btn-sm btn-ghost" @click="storePreset(presetName)">Сохранить раскладку</button>
        <select class="input cols-sel" @change="applyPreset(($event.target as HTMLSelectElement).value)">
          <option value="">Выбрать сохранённую…</option>
          <option v-for="pr in presetList" :key="pr.name" :value="pr.name">{{ pr.name || "последняя" }}</option>
        </select>
      </div>
    </section>

    <!-- ===== Отчёт импорта (ТЗ §7.9: предпросмотр до применения) ===== -->
    <section v-if="importReport" class="card imp-card">
      <div class="card-header">
        <span class="card-title">Импорт — предпросмотр</span>
        <span class="badge badge-muted">строк в файле: {{ importReport.rows_in_file }}</span>
        <span class="badge badge-pos">найдено: {{ importReport.rows_matched }}</span>
        <span v-if="importReport.rows_unknown" class="badge badge-warn">не найдено: {{ importReport.rows_unknown }}</span>
        <button type="button" class="link-btn" @click="importReport = null">закрыть</button>
      </div>
      <p class="imp-note">
        Строки, которых нет в форме, не создаются: состав строк задаёт источник затрат.
      </p>
      <ul v-if="importReport.changes?.length" class="imp-ul">
        <li v-for="(ch, i) in importReport.changes.slice(0, 30)" :key="i">
          ЦФО {{ ch.code_cfo }} · статья {{ ch.code_pl }} · месяц {{ ch.month }}:
          {{ ch.old === null ? "пусто" : money(ch.old) }} → {{ ch.new === null ? "пусто" : money(ch.new) }}
        </li>
      </ul>
      <p v-if="(importReport.changes?.length || 0) > 30" class="val-more">
        …и ещё {{ (importReport.changes?.length || 0) - 30 }} изменений.
      </p>
      <ul v-if="importReport.issues.length" class="imp-ul bad">
        <li v-for="(is, i) in importReport.issues.slice(0, 20)" :key="i">
          строка {{ is.line }}: {{ is.message }}
        </li>
      </ul>
      <div class="imp-actions">
        <button class="btn btn-sm btn-primary" :disabled="!editable || busy" @click="applyImport">
          Применить импорт
        </button>
        <button class="btn btn-sm btn-ghost" @click="importReport = null">Отмена</button>
      </div>
    </section>

    <!-- ===== Отчёт проверок (§8 контроли + §9 валидации) ===== -->
    <section v-if="report" class="card val-card">
      <div class="card-header">
        <span class="card-title">Проверка формы</span>
        <span class="badge" :class="reportTone">{{ reportLabel }}</span>
        <span v-if="report.unclosed_comments" class="badge badge-warn">
          без комментария: {{ report.unclosed_comments }}
        </span>
        <button type="button" class="link-btn" @click="report = null">скрыть</button>
      </div>

      <!-- Контроли §8: каждый ненулевой обязан раскрываться до строк. -->
      <table class="data-table compact">
        <thead>
          <tr><th>Код</th><th>Контроль</th><th class="col-num">Слева</th><th class="col-num">Справа</th><th class="col-num">Разница</th></tr>
        </thead>
        <tbody>
          <template v-for="r in report.reconciliations" :key="r.code">
            <tr :class="r.skipped ? 'rec-skip' : r.ok ? '' : 'rec-bad'">
              <td><span class="badge" :class="r.skipped ? 'badge-muted' : r.ok ? 'badge-pos' : 'badge-neg'">{{ r.code }}</span></td>
              <td>
                {{ r.title }}
                <span v-if="r.note" class="rec-note">{{ r.note }}</span>
                <span v-if="r.skipped" class="rec-note">не проверено: {{ r.note || "нет данных" }}</span>
              </td>
              <td class="col-num">{{ r.skipped ? "—" : money(r.left) }}</td>
              <td class="col-num">{{ r.skipped ? "—" : money(r.right) }}</td>
              <td class="col-num" :class="!r.skipped && !r.ok ? 'bad' : ''">{{ r.skipped ? "—" : money(r.diff) }}</td>
            </tr>
            <!-- Расшифровка: ТЗ §8 — «контроль без расшифровки не принимается». -->
            <tr v-for="(d, di) in (r.ok ? [] : r.details || [])" :key="`${r.code}-${di}`" class="rec-detail">
              <td></td>
              <td>ЦФО {{ d.code_cfo || "—" }}<template v-if="d.code_pl"> · статья {{ d.code_pl }}</template><template v-if="d.title"> · {{ d.title }}</template></td>
              <td class="col-num">{{ money(d.left) }}</td>
              <td class="col-num">{{ money(d.right) }}</td>
              <td class="col-num bad">{{ money(d.diff) }}</td>
            </tr>
          </template>
        </tbody>
      </table>

      <div v-if="report.blocking.length || report.warnings.length" class="val-lists">
        <div v-if="report.blocking.length" class="val-col">
          <span class="val-h val-h-neg">Блокирующие ({{ report.blocking.length }})</span>
          <ul class="val-ul">
            <li v-for="(i, k) in report.blocking.slice(0, 40)" :key="k">
              <span class="badge badge-neg">{{ i.code }}</span>
              <template v-if="i.code_cfo"> ЦФО {{ i.code_cfo }} · статья {{ i.code_pl }} — </template>{{ i.message }}
            </li>
          </ul>
          <p v-if="report.blocking.length > 40" class="val-more">…и ещё {{ report.blocking.length - 40 }}.</p>
        </div>
        <div v-if="report.warnings.length" class="val-col">
          <span class="val-h val-h-warn">Предупреждения ({{ report.warnings.length }})</span>
          <ul class="val-ul">
            <li v-for="(i, k) in report.warnings.slice(0, 40)" :key="k">
              <span class="badge badge-warn">{{ i.code }}</span>
              <template v-if="i.code_cfo"> ЦФО {{ i.code_cfo }} · статья {{ i.code_pl }} — </template>{{ i.message }}
            </li>
          </ul>
        </div>
      </div>
      <p v-else class="val-ok">Замечаний нет.</p>
    </section>

    <!-- ===== Период формы и сравнение с фактом ===== -->
    <section class="card view-card">
      <div class="f-row view-row">
        <span class="view-lbl">Период формы</span>
        <div class="segmented" role="group" aria-label="Период формы">
          <button type="button" :aria-pressed="viewMode === 'month'" @click="setViewMode('month')">{{ monthName }} {{ planYear }}</button>
          <button type="button" :aria-pressed="viewMode === 'tail'" @click="setViewMode('tail')">{{ monthName }}–дек</button>
          <button type="button" :aria-pressed="viewMode === 'year'" @click="setViewMode('year')">весь год</button>
        </div>
        <label class="checkbox" title="Факт того же месяца прошлого года, прошлого месяца и текущего месяца рядом с вводом (только чтение)">
          <input v-model="showRef" type="checkbox" @change="saveViewPrefs" />
          Сравнение с фактом
        </label>
        <span v-if="viewMode !== 'year'" class="view-hint">ранние месяцы скрыты, их значения не удаляются</span>
      </div>
    </section>

    <!-- ===== Фильтры (ТЗ §7.1) ===== -->
    <section class="card filters">
      <div class="f-row">
        <input
          v-model="filter.search"
          class="input f-search"
          type="search"
          placeholder="Поиск: статья, код PL, код ЦФО, площадка или магазин"
          @keydown.enter="reload"
        />
        <select v-model="filter.group_pl" class="input f-sel" @change="reload">
          <option value="">Все группы PL</option>
          <option v-for="g in form?.groups || []" :key="g" :value="g">{{ g }}</option>
        </select>
        <select v-if="isRetail" v-model="filter.city" class="input f-sel" @change="reload">
          <option value="">Все города</option>
          <option v-for="c in cities" :key="c" :value="c">{{ c }}</option>
        </select>
        <select v-if="isRetail" v-model="filter.reg_manager" class="input f-sel" @change="reload">
          <option value="">Все РМ</option>
          <option v-for="m in regManagers" :key="m" :value="m">{{ m }}</option>
        </select>
        <button class="btn btn-sm btn-ghost" @click="reload"><Icon name="lucide:search" /> Применить</button>
        <button class="btn btn-sm btn-ghost" :disabled="!filterActive" @click="resetFilter">
          <Icon name="lucide:filter-x" /> Сбросить
        </button>
      </div>
    </section>

    <SkeletonTable v-if="loading" :rows="16" :cols="13" label="Загружаю строки формы" />

    <p v-else-if="!rows.length && !syncRunning" class="banner banner-info">
      <template v-if="form?.sync?.state === 'ok' && !form.sync.source_cells">
        Источник затрат ответил, но по этой форме (страна, группа ЦФО, 2025–{{ planYear }}) строк не вернул.
      </template>
      <template v-else-if="form?.sync?.state === 'error'">Строк нет: источник затрат не прочитан (причина выше).</template>
      <template v-else>
        Строк нет. Состав строк приходит из источника затрат по факту прошлого года;
        загрузка запускается при открытии формы или кнопкой «Обновить из источника».
      </template>
    </p>

    <!-- ===== Сетка ===== -->
    <section v-else class="card grid-card">
      <div ref="gridRoot" class="grid-scroll" @paste="nav.onPaste" @scroll="onScroll">
        <table class="data-table compact exp-table">
          <thead>
            <tr>
              <th
                v-for="c in visibleAttrColumns"
                :key="c.key"
                :class="[c.sticky ? 'sticky-c' : '', c.cls, 'th-sort']"
                :title="sortHint(c.key)"
                @click="toggleSort(c.key)"
              >
                {{ c.key === "cfo" && !isRetail ? "Площадка" : c.label }}{{ sortMark(c.key) }}
              </th>
              <th
                v-for="mm in shownMonths"
                :key="mm"
                class="col-num c-mon th-sort"
                :title="`План ${planYear}, ${MONTHS_SHORT[mm - 1]} · ${sortHint(monthKey(mm))}`"
                @click="toggleSort(monthKey(mm))"
              >{{ MONTHS_SHORT[mm - 1] }}{{ sortMark(monthKey(mm)) }}</th>
              <th class="col-num c-total" :title="`Сумма плана за показанные месяцы (${viewTotalLabel})`">Итого план</th>
              <th class="col-num c-fact" :title="`Факт ${planYear - 1} за год`">Факт {{ planYear - 1 }}</th>
              <template v-if="showRef">
                <th class="col-num c-ref" :title="`Факт ${monthName} ${planYear - 1} (тот же месяц прошлого года)`">Факт {{ monthName }} {{ planYear - 1 }}</th>
                <th class="col-num c-ref" :title="`Факт прошлого месяца: ${refPrev.label}`">Факт {{ refPrev.label }}</th>
                <th class="col-num c-ref" :title="`Факт текущего месяца плана (учтён на момент загрузки): ${monthName} ${planYear}`">Факт {{ monthName }} {{ planYear }}</th>
              </template>
              <th class="c-act">Строка</th>
            </tr>
          </thead>
          <tbody>
            <!-- Виртуализация (ТЗ §7.10): в DOM живёт только окно строк, а место
                 остальных занимают две распорки. Иначе 31 595 строк эталона
                 браузер не удержит. Список окна плоский — и группы, и строки
                 идут одним потоком, чтобы позиция считалась одним индексом. -->
            <tr v-if="padTop > 0" class="v-pad" :style="{ height: padTop + 'px' }"><td :colspan="colCount"></td></tr>

            <template v-for="item in windowItems" :key="item.key">
              <tr v-if="item.kind === 'group'" class="grp-row" @click="toggleGroup(item.group)">
                <td class="sticky-c grp-cell" :colspan="visibleAttrColumns.length">
                  <Icon :name="collapsed.has(item.group) ? 'lucide:chevron-right' : 'lucide:chevron-down'" />
                  {{ item.group }}
                  <span class="grp-count">{{ groupRows(item.group).length }}</span>
                </td>
                <td v-for="mm in shownMonths" :key="mm" class="col-num grp-num">{{ money(groupMonthTotal(item.group, mm)) }}</td>
                <td class="col-num grp-num strong">{{ money(groupTotal(item.group)) }}</td>
                <td class="col-num grp-num">{{ money(groupFactTotal(item.group)) }}</td>
                <template v-if="showRef">
                  <td class="col-num grp-num">{{ money(groupRefTotal(item.group, 'ly')) }}</td>
                  <td class="col-num grp-num">{{ money(groupRefTotal(item.group, 'prev')) }}</td>
                  <td class="col-num grp-num">{{ money(groupRefTotal(item.group, 'cur')) }}</td>
                </template>
                <td></td>
              </tr>

              <tr v-else-if="item.row" :class="{ 'row-dirty': rowDirty(item.row) }">
                <td
                  v-for="c in visibleAttrColumns"
                  :key="c.key"
                  :class="[c.sticky ? 'sticky-c' : '', c.cls, c.mono ? 'mono' : '']"
                  :title="String(attrValue(item.row, c.key) || '')"
                >{{ attrValue(item.row, c.key) || "—" }}</td>
                <td v-for="(mm, ci) in shownMonths" :key="mm" class="col-num c-mon">
                  <input
                    class="cell-input"
                    :class="{ 'is-dirty': isDirty(item.row, mm) }"
                    :data-grid-cell="`${item.index}:${ci}`"
                    :value="cellText(item.row, mm)"
                    :disabled="!editable"
                    inputmode="decimal"
                    @focus="nav.onFocusCell(item.index, ci)"
                    @keydown="nav.onKeydown($event, item.index, ci)"
                    @input="onInput(item.row, mm, ($event.target as HTMLInputElement).value)"
                  />
                </td>
                <td class="col-num c-total strong">{{ money(rowPlanTotal(item.row)) }}</td>
                <td class="col-num c-fact muted">{{ money(expRowTotal(item.row, 'fact')) }}</td>
                <template v-if="showRef">
                  <td class="col-num c-ref muted">{{ refText(item.row, 'ly') }}</td>
                  <td class="col-num c-ref muted">{{ refText(item.row, 'prev') }}</td>
                  <td class="col-num c-ref muted">{{ refText(item.row, 'cur') }}</td>
                </template>
                <td class="c-act">
                  <div class="row-acts">
                    <button
                      v-if="viewMode !== 'month'"
                      class="btn btn-xs btn-ghost"
                      :disabled="!editable || busy"
                      :title="viewMode === 'year' ? 'Размножить значение января на все двенадцать месяцев' : `Размножить значение месяца ${monthName} до декабря`"
                      @click="spread(item.row, viewMode === 'year' ? 'spread_year' : 'spread_to_end')"
                    >{{ viewMode === "year" ? "на год" : "до конца года" }}</button>
                    <button
                      class="btn btn-xs btn-ghost"
                      :disabled="!editable || busy"
                      :title="viewMode === 'year' ? 'Очистить строку: во всех месяцах станет пусто (пусто не равно нулю)' : 'Очистить показанные месяцы (пусто не равно нулю); скрытые не меняются'"
                      @click="spread(item.row, 'clear_row')"
                    >очистить</button>
                  </div>
                </td>
              </tr>
            </template>

            <tr v-if="padBottom > 0" class="v-pad" :style="{ height: padBottom + 'px' }"><td :colspan="colCount"></td></tr>
          </tbody>
        </table>
      </div>

      <!-- Порции по прокрутке (ТЗ §7.10): в эталоне МП 31 595 строк, разом их
           не отдаёт ни сервер, ни браузер. -->
      <div v-if="hasMore" class="grid-more">
        <button class="btn btn-sm btn-ghost" :disabled="loadingMore" @click="loadMore">
          <Icon name="lucide:chevrons-down" :class="{ spin: loadingMore }" />
          Показать ещё · загружено {{ rows.length }} из {{ form?.total }}
        </button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { apiError } from "~/utils/apiError";
import { money } from "~/utils/format";
import { useGridNav } from "~/composables/useGridNav";
import {
  useExpForm, expCell, expRowTotal, MONTHS_SHORT,
  type ExpBulkAction, type ExpCalcResult, type ExpCell, type ExpForm, type ExpImportResult,
  type ExpParamRow, type ExpRow, type ExpValidationReport
} from "~/composables/useExpForm";

const route = useRoute();
const cardId = Number(route.params.cardId);
const api = useExpForm();

const form = ref<ExpForm | null>(null);
const rows = ref<ExpRow[]>([]);
const loading = ref(true);
const loadingMore = ref(false);
const loaded = ref(false);
const busy = ref(false);
const error = ref("");
const note = ref("");
const collapsed = ref<Set<string>>(new Set());

/** Несохранённые правки: ключ «ЦФО:статья:месяц» → значение (null = очистить). */
const drafts = ref<Map<string, number | null>>(new Map());
const dirty = computed(() => drafts.value.size > 0);

const PAGE = 300;

// ── Колонки, сортировка и раскладка (ТЗ §7.2, §7.3, §7.4) ───────────────────
interface AttrCol { key: string; label: string; cls: string; sticky?: boolean; mono?: boolean; locked?: boolean; visible: boolean }

/**
 * Состав колонок атрибутов. Флажок выбора строки, код ЦФО и наименование
 * магазина/площадки по ТЗ §7.2 НЕ перемещаются и НЕ скрываются — они помечены
 * locked: без них строку не опознать.
 */
const DEFAULT_COLUMNS: AttrCol[] = [
  { key: "code_cfo", label: "Код ЦФО", cls: "c-cfo", sticky: true, mono: true, locked: true, visible: true },
  { key: "cfo", label: "Магазин", cls: "c-name", sticky: true, locked: true, visible: true },
  { key: "code_pl", label: "Код PL", cls: "c-pl", mono: true, visible: true },
  { key: "expense", label: "Статья", cls: "c-exp", visible: true },
  { key: "group_pl", label: "Группа PL", cls: "c-grp", visible: false },
  { key: "group_cfo2", label: "Город", cls: "c-city", visible: false },
  { key: "legal_entity", label: "ЮЛ", cls: "c-le", visible: false },
  { key: "store_type", label: "Тип магазина", cls: "c-type", visible: false },
  { key: "reg_manager", label: "РМ", cls: "c-rm", visible: false }
];

const attrColumns = ref<AttrCol[]>(DEFAULT_COLUMNS.map((c) => ({ ...c })));
const visibleAttrColumns = computed(() => attrColumns.value.filter((c) => c.visible));
const columnsOpen = ref(false);
const dragFrom = ref(-1);
const presetName = ref("");
const presetList = ref<{ name: string; is_default: boolean; payload: Record<string, unknown> }[]>([]);

/** Сортировка: три состояния — по возрастанию, по убыванию, без сортировки (§7.3). */
const sort = ref<{ key: string; desc: boolean } | null>(null);
const monthKey = (m: number) => `m${String(m).padStart(2, "0")}`;
const sortMark = (key: string) => (sort.value?.key === key ? (sort.value.desc ? " ↓" : " ↑") : "");
const sortHint = (key: string) =>
  sort.value?.key !== key ? "сортировать по возрастанию" : sort.value.desc ? "снять сортировку" : "сортировать по убыванию";

const toggleSort = (key: string) => {
  if (sort.value?.key !== key) sort.value = { key, desc: false };
  else if (!sort.value.desc) sort.value = { key, desc: true };
  else sort.value = null;
  void reload({ keepFilter: true });
};

const attrValue = (row: ExpRow, key: string): string | number =>
  (row as unknown as Record<string, string | number>)[key] ?? "";

const toggleColumn = (key: string) => {
  const c = attrColumns.value.find((x) => x.key === key);
  if (!c || c.locked) return;
  c.visible = !c.visible;
  void storePreset("");
};

/** Перетаскивание порядка колонок. Закреплённые с места не двигаются (§7.2). */
const dropColumn = (to: number) => {
  const from = dragFrom.value;
  dragFrom.value = -1;
  if (from < 0 || from === to) return;
  if (attrColumns.value[from]?.locked || attrColumns.value[to]?.locked) return;
  const next = [...attrColumns.value];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  attrColumns.value = next;
  void storePreset("");
};

/**
 * Раскладка хранится на пару «пользователь + форма» (ТЗ §7.4). Имя "" — это
 * «последняя использованная»: она и восстанавливается при следующем входе.
 */
const storePreset = async (name: string) => {
  try {
    await api.savePreset(name, {
      columns: attrColumns.value.map((c) => ({ key: c.key, visible: c.visible })),
      sort: sort.value
    });
    if (name) {
      presetList.value = await api.presets();
      note.value = `Раскладка «${name}» сохранена`;
    }
  } catch (e) {
    error.value = apiError(e, "Раскладка не сохранена");
  }
};

const applyPreset = (name: string) => {
  const pr = presetList.value.find((x) => x.name === name);
  if (!pr) return;
  applyLayout(pr.payload);
  void reload({ keepFilter: true });
};

const applyLayout = (payload: Record<string, unknown>) => {
  const cols = payload?.columns as { key: string; visible: boolean }[] | undefined;
  if (Array.isArray(cols)) {
    const byKey = new Map(attrColumns.value.map((c) => [c.key, c]));
    const next: AttrCol[] = [];
    for (const c of cols) {
      const found = byKey.get(c.key);
      if (!found) continue;
      // Закреплённые колонки видимость не меняют, как бы её ни сохранили.
      next.push({ ...found, visible: found.locked ? true : !!c.visible });
      byKey.delete(c.key);
    }
    for (const rest of byKey.values()) next.push(rest);
    attrColumns.value = next;
  }
  const st = payload?.sort as { key: string; desc: boolean } | null | undefined;
  sort.value = st && st.key ? { key: st.key, desc: !!st.desc } : null;
};

// ── Параметры периода и расчёт §6.1 ─────────────────────────────────────────
const paramRows = ref<ExpParamRow[]>([]);
const calcResult = ref<ExpCalcResult | null>(null);
const calcBusy = ref(false);

const paramOf = (code: string): string => {
  const p = paramRows.value.find((x) => x.param_code === code && x.scope_kind !== "cfo");
  return p ? String(p.value) : "";
};
const paramShare = computed(() => paramOf("payroll_share"));
const paramCap = computed(() => paramOf("payroll_cap") || "1.06");

const setParam = async (code: string, raw: string) => {
  const v = Number(raw.replace(",", "."));
  if (!Number.isFinite(v)) return;
  try {
    await api.saveParam(cardId, { param_code: code, scope_kind: "country", scope_value: "", value: v });
    paramRows.value = await api.params(cardId);
    note.value = "Параметр периода сохранён";
  } catch (e) {
    error.value = apiError(e, "Параметр не сохранён");
  }
};

/** Расчёт §6.1. Предпросмотр обязателен: сначала показываем, что изменится. */
const runCalc = async (op: string, preview: boolean) => {
  calcBusy.value = true;
  error.value = "";
  try {
    const r = await api.calc(cardId, op, preview);
    if (preview) {
      calcResult.value = r;
      if (!r.changed) note.value = "Расчёт ничего не меняет: значения уже совпадают";
    } else {
      calcResult.value = null;
      note.value = `Расчёт применён, ячеек изменено: ${r.changed}`;
      await reload({ keepFilter: true });
    }
  } catch (e) {
    error.value = apiError(e, "Расчёт не выполнен");
  } finally {
    calcBusy.value = false;
  }
};

// ── Импорт (ТЗ §7.9) ────────────────────────────────────────────────────────
const importInput = ref<HTMLInputElement | null>(null);
const importReport = ref<ExpImportResult | null>(null);
const importFileRef = ref<File | null>(null);

const onImportFile = async (e: Event) => {
  const file = (e.target as HTMLInputElement).files?.[0];
  (e.target as HTMLInputElement).value = "";
  if (!file) return;
  importFileRef.value = file;
  error.value = "";
  try {
    // Сначала ВСЕГДА предпросмотр: ТЗ §7.9 требует показать, что изменится,
    // до применения.
    importReport.value = await api.importFile(cardId, file, true);
  } catch (err) {
    error.value = apiError(err, "Файл не принят");
  }
};

const applyImport = async () => {
  if (!importFileRef.value) return;
  busy.value = true;
  try {
    const r = await api.importFile(cardId, importFileRef.value, false);
    note.value = `Импорт применён, изменённых ячеек: ${r.cells_saved}`;
    importReport.value = null;
    importFileRef.value = null;
    await reload({ keepFilter: true });
  } catch (err) {
    error.value = apiError(err, "Импорт не применён");
  } finally {
    busy.value = false;
  }
};

const report = ref<ExpValidationReport | null>(null);
const checking = ref(false);

/** Проверка формы: валидации §9 и контроли §8 считает сервер. */
const check = async () => {
  checking.value = true;
  error.value = "";
  try {
    report.value = await api.validate(cardId);
  } catch (e) {
    error.value = apiError(e, "Проверка не выполнена");
  } finally {
    checking.value = false;
  }
};

/**
 * Плашка мягкого режима (ответ 41). Пункты 3 и 4 ответа названы обязательными:
 * «свод не сходится» с суммой расхождения показывается и в самой форме рядом с
 * кнопкой отправки, и согласующему на следующем шаге. Согласующий открывает эту
 * же форму, поэтому плашка одна.
 */
const softWarning = computed(() => {
  const r = report.value;
  if (!r || r.strict) return "";
  const bad = r.reconciliations.filter((x) => !x.ok && !x.skipped).length;
  if (!bad && !r.blocking.length) return "";
  const parts: string[] = [];
  if (bad) parts.push(`несошедшихся контролей ${bad}, расхождение ${money(r.recon_diff)}`);
  if (r.blocking.length) parts.push(`блокирующих замечаний ${r.blocking.length}`);
  return `Свод не сходится: ${parts.join("; ")}. Отправка разрешена — режим блокировки выключен.`;
});

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
const filter = reactive({ search: "", group_pl: "", city: "", reg_manager: "" });

const card = computed(() => form.value?.card ?? null);
const planYear = computed(() => form.value?.instance.plan_year ?? card.value?.year ?? 0);

// ── Период формы и сравнение с фактом ───────────────────────────────────────
// Форма готовится на месяц плана или «до конца года»: все двенадцать месяцев сразу не нужны.
// Скрытые месяцы не удаляются и не правятся. Выбор помним в браузере.
type ViewMode = "month" | "tail" | "year";
const VIEW_KEY = "exp-form-view";
const viewMode = ref<ViewMode>("tail");
const showRef = ref(true);
const cardMonth = computed(() => Math.min(12, Math.max(1, card.value?.month || 1)));
const monthName = computed(() => MONTHS_SHORT[cardMonth.value - 1]);
const shownMonths = computed<number[]>(() => {
  if (viewMode.value === "year") return Array.from({ length: 12 }, (_, i) => i + 1);
  if (viewMode.value === "month") return [cardMonth.value];
  return Array.from({ length: 13 - cardMonth.value }, (_, i) => cardMonth.value + i);
});
const viewTotalLabel = computed(() =>
  viewMode.value === "year" ? "за год" : viewMode.value === "month" ? `за ${monthName.value}` : `${monthName.value}–дек`);
const loadViewPrefs = () => {
  try {
    const raw = JSON.parse(localStorage.getItem(VIEW_KEY) || "{}");
    if (raw.mode === "month" || raw.mode === "tail" || raw.mode === "year") viewMode.value = raw.mode;
    if (typeof raw.ref === "boolean") showRef.value = raw.ref;
  } catch {
    // Нет сохранённого вида — остаются значения по умолчанию.
  }
};
const saveViewPrefs = () => {
  try {
    localStorage.setItem(VIEW_KEY, JSON.stringify({ mode: viewMode.value, ref: showRef.value }));
  } catch {
    // Хранилище недоступно — вид просто не запомнится.
  }
};
const setViewMode = (m: ViewMode) => {
  viewMode.value = m;
  saveViewPrefs();
};
// Факт рядом с вводом: тот же месяц прошлого года, прошлый месяц, текущий месяц плана.
// «Факт» прошлого года — сценарий fact, факт планового года — fact_cur (прошлый месяц января —
// декабрь прошлого года из fact).
const refPrev = computed(() => {
  const m = cardMonth.value;
  return m > 1
    ? { scenario: "fact_cur" as const, month: m - 1, label: `${MONTHS_SHORT[m - 2]} ${planYear.value}` }
    : { scenario: "fact" as const, month: 12, label: `${MONTHS_SHORT[11]} ${planYear.value - 1}` };
});
type RefKind = "ly" | "prev" | "cur";
const refValue = (row: ExpRow, kind: RefKind): number | undefined => {
  if (kind === "ly") return expCell(row, "fact", cardMonth.value);
  if (kind === "prev") return expCell(row, refPrev.value.scenario, refPrev.value.month);
  return expCell(row, "fact_cur", cardMonth.value);
};
const refText = (row: ExpRow, kind: RefKind): string => {
  const v = refValue(row, kind);
  return v === undefined ? "—" : money(v);
};
const groupRefTotal = (g: string, kind: RefKind) =>
  rows.value.filter((r) => r.group_pl === g).reduce((s, r) => s + (refValue(r, kind) ?? 0), 0);
const isRetail = computed(() => form.value?.instance.form_kind !== "mp");
const editable = computed(() => !!form.value && !form.value.read_only);
const hasMore = computed(() => !!form.value && rows.value.length < form.value.total);
const filterActive = computed(() => !!(filter.search || filter.group_pl || filter.city || filter.reg_manager));

const key = (codeCFO: number, codePL: number, month: number) => `${codeCFO}:${codePL}:${month}`;

/**
 * Значения живут в привязке к СТРОКЕ, а не к её позиции в таблице (ТЗ §7.3):
 * поэтому ключ правки — «ЦФО + статья + месяц», и ни сортировка, ни смена
 * фильтра несохранённый ввод не теряют.
 */
const isDirty = (row: ExpRow, month: number) => drafts.value.has(key(row.code_cfo, row.code_pl, month));
const rowDirty = (row: ExpRow) => {
  for (let m = 1; m <= 12; m++) if (isDirty(row, m)) return true;
  return false;
};

const draftOrSaved = (row: ExpRow, month: number): number | undefined => {
  const k = key(row.code_cfo, row.code_pl, month);
  if (drafts.value.has(k)) {
    const v = drafts.value.get(k);
    return v === null ? undefined : v;
  }
  return expCell(row, "plan", month);
};

const cellText = (row: ExpRow, month: number): string => {
  const v = draftOrSaved(row, month);
  return v === undefined ? "" : String(v);
};

const rowPlanTotal = (row: ExpRow): number => {
  let t = 0;
  for (const m of shownMonths.value) {
    const v = draftOrSaved(row, m);
    if (v !== undefined) t += v;
  }
  return t;
};

const exportHref = computed(() => api.exportUrl(cardId, { ...query(0), limit: 5000 }));

const shownPlanTotal = computed(() => rows.value.reduce((s, r) => s + rowPlanTotal(r), 0));

// ── Группировка (ТЗ §7.6) ────────────────────────────────────────────────────
const visibleGroups = computed(() => {
  const present = new Set(rows.value.map((r) => r.group_pl));
  const ordered = (form.value?.groups || []).filter((g) => present.has(g));
  // Группа, которой нет в порядке ТЗ, всё равно должна быть видна: источник живой.
  for (const g of present) if (!ordered.includes(g)) ordered.push(g);
  return ordered;
});
const groupRows = (g: string) => rows.value.filter((r) => r.group_pl === g);
const groupTotal = (g: string) => groupRows(g).reduce((s, r) => s + rowPlanTotal(r), 0);
const groupFactTotal = (g: string) => groupRows(g).reduce((s, r) => s + expRowTotal(r, "fact"), 0);
const groupMonthTotal = (g: string, month: number) =>
  groupRows(g).reduce((s, r) => s + (draftOrSaved(r, month) ?? 0), 0);
const toggleGroup = (g: string) => {
  const next = new Set(collapsed.value);
  next.has(g) ? next.delete(g) : next.add(g);
  collapsed.value = next;
};

const cities = computed(() => [...new Set(rows.value.map((r) => r.group_cfo2).filter(Boolean))].sort());
const regManagers = computed(() => [...new Set(rows.value.map((r) => r.reg_manager).filter(Boolean))].sort());

// ── Виртуализация окна (ТЗ §7.10) ───────────────────────────────────────────
/**
 * Эталон маркетплейсов — 31 595 строк. Столько узлов таблицы браузер не держит, поэтому в
 * DOM живёт только окно, а место остальных занимают две распорки. Список окна
 * ПЛОСКИЙ: строки групп и строки данных идут одним потоком, чтобы позиция
 * считалась одним индексом и совпадала с координатами навигации по сетке.
 */
interface GridItem { key: string; kind: "group" | "row"; group: string; row?: ExpRow; index: number }

/** Высота строки. Держим её и в CSS: расхождение сдвинет распорки. */
const ROW_H = 30;
/** Запас строк сверху и снизу окна — чтобы при быстрой прокрутке не мигало. */
const OVERSCAN = 8;

const scrollTop = ref(0);
const viewportH = ref(600);

const displayItems = computed<GridItem[]>(() => {
  const out: GridItem[] = [];
  let rowIndex = 0;
  for (const g of visibleGroups.value) {
    out.push({ key: `g:${g}`, kind: "group", group: g, index: -1 });
    if (collapsed.value.has(g)) continue;
    for (const r of groupRows(g)) {
      out.push({ key: `r:${r.id}`, kind: "row", group: g, row: r, index: rowIndex++ });
    }
  }
  return out;
});

const windowStart = computed(() => Math.max(0, Math.floor(scrollTop.value / ROW_H) - OVERSCAN));
const windowEnd = computed(() =>
  Math.min(displayItems.value.length, Math.ceil((scrollTop.value + viewportH.value) / ROW_H) + OVERSCAN)
);
const windowItems = computed(() => displayItems.value.slice(windowStart.value, windowEnd.value));
const padTop = computed(() => windowStart.value * ROW_H);
const padBottom = computed(() => Math.max(0, (displayItems.value.length - windowEnd.value) * ROW_H));
/** Колонок всего: атрибуты + показанные месяцы + итог + факт + (сравнение) + действия. */
const colCount = computed(() => visibleAttrColumns.value.length + shownMonths.value.length + 3 + (showRef.value ? 3 : 0));

const onScroll = (e: Event) => {
  const el = e.target as HTMLElement;
  scrollTop.value = el.scrollTop;
  viewportH.value = el.clientHeight;
};

/**
 * Показать строку перед фокусом: строки вне окна в DOM нет, и focus() было бы
 * некуда ставить. Композабл навигации вызывает это перед переходом.
 */
const ensureRowVisible = (rowIndex: number) => {
  const pos = displayItems.value.findIndex((i) => i.kind === "row" && i.index === rowIndex);
  if (pos < 0) return;
  const el = gridRoot.value;
  if (!el) return;
  const top = pos * ROW_H;
  if (top < el.scrollTop) el.scrollTop = top;
  else if (top + ROW_H > el.scrollTop + el.clientHeight) el.scrollTop = top + ROW_H - el.clientHeight;
  scrollTop.value = el.scrollTop;
};

// ── Навигация по сетке как в Excel (ТЗ §7.8) ─────────────────────────────────
/** Плоский порядок строк для координат сетки: свёрнутые группы в него не входят. */
const flatRows = computed(() => {
  const out: ExpRow[] = [];
  for (const g of visibleGroups.value) {
    if (collapsed.value.has(g)) continue;
    out.push(...groupRows(g));
  }
  return out;
});
const rowIndex = (row: ExpRow) => flatRows.value.findIndex((r) => r.id === row.id);

const nav = useGridNav({
  rows: () => flatRows.value.length,
  cols: () => shownMonths.value.length,
  ensureVisible: ensureRowVisible,
  onPaste: (anchor, matrix) => {
    if (!editable.value) return;
    matrix.forEach((line, di) => {
      line.forEach((raw, dj) => {
        const row = flatRows.value[anchor.row + di];
        const month = shownMonths.value[anchor.col + dj];
        if (!row || month === undefined) return;
        const v = nav.parseCellNumber(raw);
        setDraft(row, month, v === null ? null : v);
      });
    });
  }
});
const gridRoot = nav.container;

// ── Ввод и сохранение ────────────────────────────────────────────────────────
const setDraft = (row: ExpRow, month: number, value: number | null) => {
  const next = new Map(drafts.value);
  next.set(key(row.code_cfo, row.code_pl, month), value);
  drafts.value = next;
};

const onInput = (row: ExpRow, month: number, raw: string) => {
  const s = raw.trim().replace(/\s/g, "").replace(",", ".");
  // Пустая строка — это ОЧИСТКА ячейки, а не ноль (ТЗ §7.8): ноль вводится явно.
  if (s === "") {
    setDraft(row, month, null);
    return;
  }
  const v = Number(s);
  if (Number.isFinite(v)) setDraft(row, month, v);
};

const save = async () => {
  if (!drafts.value.size) return;
  busy.value = true;
  error.value = "";
  note.value = "";
  const cells: ExpCell[] = [];
  for (const [k, v] of drafts.value) {
    const [cfo, pl, m] = k.split(":").map(Number);
    cells.push({ code_cfo: cfo, code_pl: pl, month: m, amount: v });
  }
  try {
    const r = await api.save(cardId, cells);
    drafts.value = new Map();
    note.value = `Сохранено, изменённых ячеек: ${r.changed}`;
    await reload({ keepFilter: true });
  } catch (e) {
    error.value = apiError(e, "Не удалось сохранить значения");
  } finally {
    busy.value = false;
  }
};

/** Размножение и очистка по строке (ТЗ §7.8). Идут на сервер: правило одно. */
const spread = async (row: ExpRow, action: ExpBulkAction) => {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    // База размножения — первый показанный месяц (январь только в виде «весь год»).
    const baseMonth = shownMonths.value[0];
    const amount = action === "clear_row" ? null : (draftOrSaved(row, baseMonth) ?? null);
    if (action !== "clear_row" && amount === null) {
      error.value = `Размножать нечего: сначала заполните ${MONTHS_SHORT[baseMonth - 1]} этой строки`;
      return;
    }
    const win = viewMode.value === "year" ? {} : { month: baseMonth, month_to: shownMonths.value[shownMonths.value.length - 1] };
    await api.bulk(cardId, { action, code_cfo: row.code_cfo, code_pl: row.code_pl, amount, ...win });
    // Правки по этой строке уже не актуальны — их перезаписал сервер.
    const next = new Map(drafts.value);
    for (let m = 1; m <= 12; m++) next.delete(key(row.code_cfo, row.code_pl, m));
    drafts.value = next;
    await reload({ keepFilter: true });
  } catch (e) {
    error.value = apiError(e, "Массовая операция не выполнена");
  } finally {
    busy.value = false;
  }
};

// ── Загрузка ─────────────────────────────────────────────────────────────────
const query = (offset: number) => ({
  search: filter.search || undefined,
  group_pl: filter.group_pl || undefined,
  city: filter.city || undefined,
  reg_manager: filter.reg_manager || undefined,
  offset,
  limit: PAGE,
  sort: sort.value?.key,
  dir: sort.value?.desc ? ("desc" as const) : undefined
});

const syncRunning = computed(() => form.value?.sync?.state === "running");
// Откуда данные: ночной снимок в ClickHouse (с датой) или процедура напрямую (свежо, но долго).
const sourceNote = computed(() => {
  const s = form.value?.sync;
  if (!s || s.state === "idle" || s.state === "running") return "";
  if (s.source_kind === "clickhouse" && s.source_as_of) {
    const d = new Date(s.source_as_of);
    return `затраты на ${d.toLocaleDateString("ru-RU")} ${d.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" })}`;
  }
  return s.source_kind === "olap" ? "затраты напрямую из источника" : "";
});
const sourceNoteTitle = computed(() =>
  form.value?.sync?.source_kind === "clickhouse"
    ? "Ночной снимок процедуры затрат (ClickHouse). Свежее — кнопка «Обновить из источника»."
    : "Снимка в ClickHouse ещё нет — строки прочитаны из процедуры на OLAP.");
// Загрузка закончилась, пока на экране уже есть строки: подменять их самим нельзя
// (там может быть несохранённый ввод), поэтому предлагаем обновить вручную.
const syncDoneNew = ref(false);
let syncTimer: ReturnType<typeof setInterval> | null = null;
const stopSyncPoll = () => {
  if (syncTimer) clearInterval(syncTimer);
  syncTimer = null;
};
const pollSync = async () => {
  try {
    const f = await api.form(cardId, { limit: 1 });
    const wasRunning = form.value?.sync?.state === "running";
    if (form.value) form.value = { ...form.value, sync: f.sync };
    if (f.sync?.state !== "running") {
      stopSyncPoll();
      if (wasRunning && f.sync?.state === "ok") {
        if (rows.value.length === 0) await reload({ keepFilter: true });
        else syncDoneNew.value = true;
      }
    }
  } catch {
    // Сеть моргнула — следующий опрос повторит.
  }
};
const startSyncPoll = () => {
  if (!syncTimer) syncTimer = setInterval(() => void pollSync(), 10_000);
};
const runSync = async () => {
  error.value = "";
  syncDoneNew.value = false;
  try {
    const st = await api.syncNow(cardId);
    if (form.value) form.value = { ...form.value, sync: st };
    startSyncPoll();
  } catch (e) {
    error.value = apiError(e, "Не удалось запустить загрузку из источника");
  }
};

const reload = async (opts: { keepFilter?: boolean } = {}) => {
  if (!opts.keepFilter) collapsed.value = new Set();
  loading.value = rows.value.length === 0;
  error.value = "";
  try {
    const f = await api.form(cardId, query(0));
    form.value = f;
    rows.value = f.rows;
    loaded.value = true;
    syncDoneNew.value = false;
    if (f.sync?.state === "running") startSyncPoll();
  } catch (e) {
    error.value = apiError(e, "Не удалось загрузить форму");
  } finally {
    loading.value = false;
  }
};

const loadMore = async () => {
  loadingMore.value = true;
  try {
    const f = await api.form(cardId, query(rows.value.length));
    form.value = { ...f, rows: [] };
    rows.value = [...rows.value, ...f.rows];
  } catch (e) {
    error.value = apiError(e, "Не удалось загрузить следующую порцию");
  } finally {
    loadingMore.value = false;
  }
};

const resetFilter = () => {
  filter.search = "";
  filter.group_pl = "";
  filter.city = "";
  filter.reg_manager = "";
  void reload();
};

// Предупреждение о несохранённом вводе (ТЗ §7.8).
const beforeUnload = (e: BeforeUnloadEvent) => {
  if (!dirty.value) return;
  e.preventDefault();
  e.returnValue = "";
};
onMounted(async () => {
  window.addEventListener("beforeunload", beforeUnload);
  loadViewPrefs();
  // Раскладка восстанавливается ДО первой загрузки: иначе форма моргнёт
  // дефолтным порядком колонок и чужой сортировкой.
  try {
    presetList.value = await api.presets();
    const last = presetList.value.find((p) => p.name === "");
    if (last) applyLayout(last.payload);
  } catch {
    // Нет пресетов — работаем на составе по умолчанию.
  }
  void reload();
});
onBeforeUnmount(() => {
  window.removeEventListener("beforeunload", beforeUnload);
  stopSyncPoll();
});
onBeforeRouteLeave(() => {
  if (!dirty.value) return true;
  return window.confirm("Есть несохранённые изменения. Уйти со страницы и потерять их?");
});
</script>

<style scoped>
.page-exp { display: flex; flex-direction: column; gap: var(--sp-4); }
.lock-note { color: var(--neg); display: inline-flex; align-items: center; gap: 3px; margin-left: var(--sp-3); }
.dirty-dot { color: var(--warn); margin-left: 4px; }

.filters { padding: var(--sp-4); }
.f-row { display: flex; flex-wrap: wrap; gap: var(--sp-3); align-items: center; }
.f-search { flex: 1 1 320px; min-width: 200px; }
.f-sel { flex: 0 1 180px; min-width: 140px; }

.grid-card { padding: 0; overflow: hidden; }
.view-card { padding: var(--sp-4) var(--sp-5); }
.view-row { display: flex; align-items: center; gap: var(--sp-5); flex-wrap: wrap; }
.view-lbl { color: var(--text-muted); font-size: var(--fs-2xs); text-transform: uppercase; letter-spacing: 0.03em; }
.view-hint { color: var(--text-muted); font-size: var(--fs-xs); }
.c-ref { min-width: 96px; color: var(--text-secondary); background: var(--bg-surface-2); }

/* Сетка шире экрана всегда: 12 месяцев плюс атрибуты. Прокрутка живёт внутри
   карточки, страница по горизонтали не едет. */
.grid-scroll { overflow: auto; max-height: 70vh; }
.exp-table { min-width: 1180px; }
.exp-table thead th {
  position: sticky;
  top: 0;
  z-index: 2;
  background: var(--bg-surface-2);
}
/* Закрепление блока атрибутов при горизонтальной прокрутке (ТЗ §7.5): без него
   на 24 колонках непонятно, к какому ЦФО относится ячейка. */
.sticky-c { position: sticky; background: var(--bg-surface); z-index: 1; }
.exp-table thead .sticky-c { z-index: 3; background: var(--bg-surface-2); }
.c-cfo { left: 0; width: 74px; }
.c-name { left: 74px; width: 200px; max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.c-pl { width: 62px; }
.c-exp { width: 220px; max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.c-mon { width: 86px; }
.c-total, .c-fact { width: 104px; }
.c-act { width: 128px; }
.mono { font-family: var(--font-mono); }
.strong { font-weight: var(--fw-semibold); color: var(--text-strong); }
.muted { color: var(--text-muted); }

.grp-row { cursor: pointer; background: var(--bg-surface-3); }
.grp-row:hover { background: var(--bg-tonal); }
.grp-cell {
  left: 0;
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
  display: flex;
  align-items: center;
  gap: 4px;
  background: var(--bg-surface-3);
}
.grp-count { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.grp-num { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-secondary); }

.exp-table tbody tr { height: 30px; }
/* Распорки виртуализации: занимают место строк вне окна рендера. Высота строки
   продублирована в ROW_H — расхождение сдвинуло бы прокрутку. */
.v-pad td { padding: 0; border: 0; }

.cell-input {
  width: 100%;
  border: 1px solid transparent;
  background: transparent;
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: var(--fs-sm);
  font-variant-numeric: tabular-nums;
  text-align: right;
  padding: 2px var(--sp-2);
  border-radius: var(--rd-2);
}
.cell-input:hover:not(:disabled) { border-color: var(--border); }
.cell-input:focus { outline: none; border-color: var(--border-focus); box-shadow: var(--shadow-focus); }
.cell-input:disabled { color: var(--text-muted); }
.cell-input.is-dirty { border-color: var(--warn); background: var(--warn-soft); }
.row-dirty .sticky-c { border-left: 2px solid var(--warn); }

.row-acts { display: flex; flex-wrap: wrap; gap: var(--sp-2); }
.btn-xs { height: 22px; padding: 0 var(--sp-3); font-size: var(--fs-2xs); }
/* Колонки и раскладка (§7.2, §7.4). */
/* Параметры периода и расчёт §6.1. */
.params-card { padding: var(--sp-4) var(--sp-5); display: flex; flex-direction: column; gap: var(--sp-3); }
.params-row { display: flex; flex-wrap: wrap; gap: var(--sp-4); align-items: center; }
.params-title { font-weight: var(--fw-semibold); color: var(--text-strong); }
.params-f { display: inline-flex; align-items: center; gap: 6px; font-size: var(--fs-sm); }
.params-in { width: 90px; font-family: var(--font-mono); text-align: right; }
.params-hint { font-size: var(--fs-2xs); color: var(--text-muted); flex: 1 1 260px; min-width: 0; }
.calc-prev { display: flex; flex-direction: column; gap: var(--sp-2); border-top: 1px solid var(--border); padding-top: var(--sp-3); }
.calc-head { display: flex; flex-wrap: wrap; gap: var(--sp-3); align-items: center; }
.calc-ul { margin: 0; padding-left: var(--sp-5); font-size: var(--fs-sm); display: flex; flex-direction: column; gap: 2px; }
.calc-note { color: var(--warn); font-size: var(--fs-2xs); margin-left: 6px; }
.calc-actions { display: flex; flex-wrap: wrap; gap: var(--sp-3); }

.cols-card { padding: var(--sp-4) var(--sp-5); display: flex; flex-direction: column; gap: var(--sp-3); }
.cols-hint { font-size: var(--fs-2xs); color: var(--text-muted); }
.cols-list { list-style: none; margin: 0; padding: 0; display: flex; flex-wrap: wrap; gap: var(--sp-2); }
.cols-item {
  display: flex; align-items: center; gap: 6px;
  border: 1px solid var(--border); border-radius: var(--rd-3);
  padding: 2px var(--sp-3); font-size: var(--fs-sm); cursor: grab; background: var(--bg-surface);
}
.cols-item.locked { cursor: default; color: var(--text-muted); background: var(--bg-surface-2); }
.cols-grip { color: var(--text-muted); }
.cols-label { display: inline-flex; align-items: center; gap: 4px; }
.cols-actions { display: flex; flex-wrap: wrap; gap: var(--sp-3); align-items: center; }
.cols-name { flex: 0 1 220px; }
.cols-sel { flex: 0 1 240px; }
.hidden-file { display: none; }

/* Заголовок-сортировка: три состояния по клику (§7.3). */
.th-sort { cursor: pointer; user-select: none; }
.th-sort:hover { color: var(--accent); }

/* Отчёт импорта (§7.9). */
.imp-card { padding: var(--sp-4) var(--sp-5); display: flex; flex-direction: column; gap: var(--sp-3); }
.imp-note { font-size: var(--fs-sm); color: var(--text-muted); margin: 0; }
.imp-ul { margin: 0; padding-left: var(--sp-5); font-size: var(--fs-sm); display: flex; flex-direction: column; gap: 2px; }
.imp-ul.bad { color: var(--neg); }
.imp-actions { display: flex; flex-wrap: wrap; gap: var(--sp-3); }
.c-grp, .c-city, .c-le, .c-type, .c-rm { width: 150px; max-width: 150px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.val-card { display: flex; flex-direction: column; gap: var(--sp-3); padding: var(--sp-4) var(--sp-5); }
.val-card .data-table { width: 100%; }
.rec-bad td { background: var(--neg-soft); }
.rec-skip td { color: var(--text-muted); }
.rec-detail td { font-size: var(--fs-2xs); color: var(--text-secondary); background: var(--bg-surface-2); }
.rec-note { display: block; font-size: var(--fs-2xs); color: var(--text-muted); }
.bad { color: var(--neg); font-weight: var(--fw-semibold); }
.val-lists { display: flex; flex-wrap: wrap; gap: var(--sp-6); }
.val-col { display: flex; flex-direction: column; gap: var(--sp-2); min-width: 260px; flex: 1 1 320px; }
.val-h { font-size: var(--fs-2xs); text-transform: uppercase; letter-spacing: 0.04em; }
.val-h-neg { color: var(--neg); }
.val-h-warn { color: var(--warn); }
.val-ul { margin: 0; padding-left: var(--sp-5); display: flex; flex-direction: column; gap: 2px; font-size: var(--fs-sm); }
.val-more, .val-ok { font-size: var(--fs-sm); color: var(--text-muted); margin: 0; }
.link-btn { background: none; border: 0; color: var(--accent); font-size: var(--fs-2xs); cursor: pointer; margin-left: auto; }
.grid-more { display: flex; justify-content: center; padding: var(--sp-4); border-top: 1px solid var(--border); }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
