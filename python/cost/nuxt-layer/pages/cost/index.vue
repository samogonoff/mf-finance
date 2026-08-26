<template>
  <div class="page-cost">
    <header class="page-header">
      <div>
        <h1 class="page-title">{{ pageTitle }}</h1>
        <p class="page-subtitle">
          <span>{{ pageSubtitle }}</span>
          <span v-if="mockMode" class="ctx-sep">·</span>
          <span v-if="mockMode" class="mock-pill">MOCK данные</span>
        </p>
      </div>
      <!-- Навигация кабинета живёт в nuxt/, за границей раздела, поэтому вход
           в дашборд — отсюда, из шапки самого раздела. -->
      <div class="header-actions">
        <NuxtLink to="/cost/commercial" class="btn btn-ghost btn-sm">
          <Icon name="lucide:chart-pie" /> Коммерческая эффективность
        </NuxtLink>
      </div>
    </header>

    <!-- Фильтры -->
    <section class="card filters-card">
      <div class="card-header">
        <!-- По клику на заголовок панель складывается: на широкой таблице она
             занимает половину первого экрана, а после загрузки данных нужна
             редко. Кнопки «Сбросить» и «Загрузить данные» остаются доступны и в
             свёрнутом виде — за ними панель разворачивать не нужно. -->
        <div class="filters-toggle" role="button" tabindex="0"
          :aria-expanded="filtersOpen ? 'true' : 'false'"
          :title="filtersOpen ? 'Свернуть фильтры' : 'Развернуть фильтры'"
          @click="toggleFilters" @keydown.enter.prevent="toggleFilters" @keydown.space.prevent="toggleFilters">
          <Icon :name="filtersOpen ? 'lucide:chevron-down' : 'lucide:chevron-right'" class="filters-chevron" />
          <div>
            <div class="card-title">
              Фильтры<span v-if="!filtersOpen && activeFilterCount" class="filters-badge">{{ activeFilterCount }}</span>
            </div>
            <div class="card-subtitle">
              {{ filtersOpen
                ? 'Top-down каскад — вышестоящие фильтры ограничивают нижестоящие'
                : (activeFilterCount ? 'Свёрнуто, фильтры применены' : 'Свёрнуто') }}
            </div>
          </div>
        </div>
        <div class="card-actions">
          <button class="btn btn-ghost btn-sm" @click="resetFilters">
            <Icon name="lucide:x" /> Сбросить
          </button>
          <button class="btn btn-primary btn-sm" :disabled="loading" @click="loadData">
            <Icon name="lucide:refresh-cw" />
            {{ loading ? "Загрузка…" : "Загрузить данные" }}
          </button>
        </div>
      </div>

      <div v-show="filtersOpen" class="card-body filters-body" :class="{ 'is-busy': cascadeBusy }">
        <div class="dates-row">
          <label>Период с
            <input v-model="dateFrom" type="date" />
          </label>
          <label>по
            <input v-model="dateTo" type="date" />
          </label>
        </div>

        <div class="filters-grid">
          <div v-for="f in filterConfig" :key="f.key" class="filter-item" :class="{ locked: isLocked(f.key) }">
            <label>{{ f.label }}</label>
            <CostMultiSelect
              v-model="selected[f.key]"
              :options="enrichFilterOptions(f.key, filterOptions[f.key] || [])"
              :placeholder="isLocked(f.key) ? '—' : `Все · ${f.label.toLowerCase()}`"
              :disabled="isLocked(f.key)"
              @change="onFilterChange(f.key)"
            />
          </div>
        </div>

        <div class="filter-checkbox-row">
          <label class="filter-checkbox">
            <input v-model="noWholesaleOnly" type="checkbox" @change="loadData" />
            <span>Только строки без оптовой цены</span>
          </label>

          <label class="filter-checkbox"
                 :title="hideDwhSentDefault
                   ? 'Для роли Калькулятор и ПЭО включён по умолчанию: в отправленных в DWH калькуляциях править и согласовывать нечего. Снять можно'
                   : 'Скрыть калькуляции, уже отправленные в DWH (📤)'">
            <input
              type="checkbox"
              :checked="hideDwhSent"
              @change="setHideDwhSent(($event.target as HTMLInputElement).checked)"
            />
            <span>
              Скрыть отправленные в DWH
              <template v-if="dwhSentHiddenCount"> ({{ dwhSentHiddenCount }})</template>
              <template v-if="hideDwhSentDefault && hideDwhSent"> · по умолчанию для вашей роли</template>
            </span>
          </label>

          <label class="peo-filter-label">
            Статус согласования
            <select v-model="peoFilter" class="peo-filter-select" @change="onPeoFilterChange">
              <option value="all">Все</option>
              <option value="approved">🟢 Согласовано</option>
              <option value="none">Не согласовано</option>
              <option value="rejected">🔴 Отклонено</option>
              <option value="returned">🟠 Возврат на корректировку</option>
            </select>
          </label>
        </div>

        <div v-if="cascadeBusy" class="filters-overlay">
          <div class="loader"></div>
          <span>Обновление фильтров…</span>
        </div>
      </div>
    </section>

    <!-- Action bar -->
    <div class="cost-actions">
      <div class="cost-info">
        <template v-if="loading">
          <Icon name="lucide:loader" class="spinning" />
          <span>Загрузка данных…</span>
        </template>
        <template v-else-if="totalAllRecords">
          <Icon name="lucide:check-circle-2" class="info-ok" />
          <span>Агрегировано записей: <strong>{{ totalAllRecords.toLocaleString("ru-RU") }}</strong></span>
        </template>
        <template v-else>
          <Icon name="lucide:info" class="info-muted" />
          <span class="muted">Выберите фильтры и нажмите «Загрузить данные»</span>
        </template>
      </div>
      <label class="usd-toggle" title="Показать/скрыть $ колонки">
        <input type="checkbox" v-model="showUSD" />
        <span class="usd-toggle-track">
          <span class="usd-toggle-thumb">$</span>
        </span>
      </label>
      <button class="btn btn-ghost btn-sm" @click="openColumnSettings" title="Настройка видимости колонок">
        <Icon name="lucide:settings" /> Колонки
      </button>
      <div class="cost-cache-status">
        <button class="btn btn-ghost btn-sm" :disabled="cacheRefreshing" @click="refreshCache">
          <Icon name="lucide:refresh-cw" />
          {{ cacheRefreshing ? 'Обновление…' : 'Обновить кеш' }}
        </button>
        <!-- Полное обновление — только админу раздела: TRUNCATE + перезалив всей
             CostHistory, ~25 минут, и на это время запросы к кэшу встают.
             Обычная кнопка рядом берёт только последние 2 месяца. -->
        <button v-if="can('cost:admin')" class="btn btn-ghost btn-sm"
                :disabled="cacheRefreshing" title="TRUNCATE + перезалив всей CostHistory"
                @click="refreshCacheFull">
          <Icon name="lucide:database-backup" />
          Полное обновление
        </button>
        <span v-if="cacheRefreshing" class="cache-spinner">
          <Icon name="lucide:loader" class="spinning" /> обновление данных…
        </span>
        <span v-else-if="cacheInfo?.refreshed_at" class="cache-info">
          <Icon name="lucide:database" />
          Кеш: {{ (cacheInfo.row_count || 0).toLocaleString("ru-RU") }} записей · {{ formatDateTime(cacheInfo.refreshed_at) }}
        </span>
        <Transition name="fade">
          <span v-if="cacheNotification" class="cache-notification">
            {{ cacheNotification }}
          </span>
        </Transition>
      </div>
      <div class="cost-actions-buttons">
        <button class="btn btn-ghost" @click="openMarginModal">
          <Icon name="lucide:target" /> Таргеты маржинальности
        </button>
        <button class="btn btn-ghost" @click="openMpConstantsModal">
          <Icon name="lucide:percent" /> Константы МП
        </button>
        <!-- Только Калькулятор, ПЭО и админ: cost:edit_materials есть ровно у этих
             ролей (у Бренд-менеджера и Просмотра его нет), а функционал правит
             материалы плана. Бэкенд гейтит те же эндпоинты, кнопка — лишь UI. -->
        <button v-if="can('cost:edit_materials') || can('cost:admin')" class="btn btn-ghost" @click="openPlanPricesModal">
          <Icon name="lucide:layers" /> Цены материалов по плану
        </button>
        <button v-if="can('cost:approve')" class="btn btn-ghost" @click="openApprovalModal">
          <Icon name="lucide:check-square" /> Согласование
        </button>
        <button v-if="can('cost:approve')" class="btn btn-ghost" @click="navigateTo('/cost/approvals')">
          <Icon name="lucide:clipboard-check" /> Страница согласования
        </button>
        <!-- Считаем по sortedRows, а не по totalAllRecords: выгружается
             отфильтрованный диапазон, и кнопка должна гаснуть, когда фильтры
             колонок не оставили ни одной строки. Число в подписи показываем
             только когда выгрузка уже, чем загруженная выдача, — чтобы было
             видно, что уедет подмножество. -->
        <button v-if="can('cost:export')" class="btn btn-ghost" :disabled="!sortedRows.length"
          :title="`Выгрузить строки с учётом всех фильтров: ${sortedRows.length}`" @click="exportToExcel">
          <Icon name="lucide:download" /> Экспорт в Excel<template v-if="sortedRows.length && sortedRows.length !== totalAllRecords"> ({{ sortedRows.length }})</template>
        </button>
        <button v-if="can('cost:admin')" class="btn btn-ghost" @click="navigateTo('/cost/admin/roles')">
          <Icon name="lucide:settings" /> Администрирование
        </button>
        <button
          v-if="can('cost:approve') || can('cost:peo_mark') || can('cost:edit_price')"
          class="btn btn-primary"
          :disabled="!changedRows.size || saving"
          @click="saveAllChanges"
        >
          <Icon name="lucide:save" />
          <template v-if="changedRows.size">
            {{ saving ? "Сохранение…" : `Сохранить изменения (${changedRows.size})` }}
          </template>
          <template v-else>Сохранить изменения</template>
        </button>
        <button
          v-if="can('cost:edit_price') || can('cost:admin') && changedRows.size"
          class="btn btn-ghost"
          @click="discardChanges"
        >
          <Icon name="lucide:undo-2" />
          Сбросить введённые значения
        </button>
      </div>
    </div>

    <!-- Error banner -->
    <div v-if="lastError" class="cost-error">
      <Icon name="lucide:alert-triangle" />
      <div>
        <strong>Не удалось получить данные с API</strong>
        <p>{{ lastError }}</p>
        <p class="muted">
          Проверьте подключение к MSSQL/OLAP в <code>python/cost/.env</code>
          либо включите <code>COST_MOCK=1</code> и пересоберите контур
          (<code>make cost-up</code>).
        </p>
      </div>
      <button class="cost-error-x" @click="lastError = ''" aria-label="Закрыть">×</button>
    </div>

    <!-- Таблица 1: агрегаты -->
    <section class="card">
      <div class="card-header">
        <div>
          <div class="card-title">Агрегированные данные</div>
          <div class="card-subtitle">
            Показано {{ pageRange }} из {{ filteredAggregated.length.toLocaleString("ru-RU") }}{{ columnFiltersActive ? ' (отфильтровано из ' + totalAllRecords.toLocaleString("ru-RU") + ')' : '' }}
          </div>
        </div>
        <div class="card-actions">
          <!-- Размер страницы доступен всегда, даже когда страница одна: иначе
               из 25 строк нельзя было бы «раскрыть» таблицу на 200. -->
          <label class="page-size">
            <span class="muted">Строк:</span>
            <select
              class="page-size-select"
              :value="pageSize"
              @change="onPageSizeChange(($event.target as HTMLSelectElement).value)"
            >
              <option v-for="n in PAGE_SIZE_OPTIONS" :key="n" :value="n">{{ n }}</option>
            </select>
          </label>
          <template v-if="totalPages > 1">
            <button class="btn btn-ghost btn-sm" :disabled="currentPage === 0" @click="prevPage">
              <Icon name="lucide:chevron-left" /> Назад
            </button>
            <span class="muted">Стр. {{ currentPage + 1 }} / {{ totalPages }}</span>
            <button
              class="btn btn-ghost btn-sm"
              :disabled="currentPage >= totalPages - 1"
              @click="nextPage"
            >
              Вперёд <Icon name="lucide:chevron-right" />
            </button>
          </template>
        </div>
      </div>
      <div v-if="allAggregated.length > 0" class="col-filters" :class="{ 'is-active': columnFiltersActive }">
        <div v-for="cfg in columnFilterConfig" :key="cfg.key" class="col-filter-item" :class="{ locked: isColFilterLocked(cfg.key) }">
          <label>{{ cfg.label }}</label>
          <CostMultiSelect
            v-model="columnFilters[cfg.key]"
            :options="enrichFilterOptions(cfg.key, columnFilterOptions[cfg.key] || [])"
            :placeholder="isColFilterLocked(cfg.key) ? '—' : `Все · ${cfg.label.toLowerCase()}`"
            :disabled="isColFilterLocked(cfg.key)"
            @change="onColFilterChange(cfg.key)"
          />
        </div>
        <a href="#" class="col-filter-reset" @click.prevent="resetColumnFilters">Сбросить фильтры колонок</a>
      </div>

      <!-- Массовое согласование ПЭО: панель появляется, когда что-то выбрано -->
      <div v-if="peoBulkEnabled && peoSelectedItems.length" class="peo-bulk-bar">
        <div class="peo-bulk-info">
          <strong>Выбрано: {{ peoSelectedItems.length }}</strong>
          <span class="muted">калькуляций · строк таблицы: {{ peoSelectedRowCount }}</span>
          <button
            v-if="peoSelectedItems.length < peoSelectableRows.length"
            class="btn btn-ghost btn-xs"
            :disabled="peoBulkBusy"
            @click="selectAllFilteredForPeo"
          >
            Выбрать все отфильтрованные ({{ peoSelectableRows.length }})
          </button>
          <button class="btn btn-ghost btn-xs" :disabled="peoBulkBusy" @click="clearPeoSelection">
            Снять выбор
          </button>
        </div>
        <div class="peo-bulk-actions">
          <input
            v-model="peoBulkComment"
            type="text"
            class="peo-bulk-comment"
            placeholder="Комментарий (для отклонения)"
            :disabled="peoBulkBusy"
          />
          <button class="btn btn-primary btn-sm" :disabled="peoBulkBusy" @click="runPeoBulk('approved')">
            ✓ {{ peoBulkBusy && peoBulkAction === 'approved' ? 'Согласование…' : `Согласовать (${peoSelectedItems.length})` }}
          </button>
          <button class="btn btn-danger btn-sm" :disabled="peoBulkBusy" @click="runPeoBulk('rejected')">
            ✗ {{ peoBulkBusy && peoBulkAction === 'rejected' ? 'Отклонение…' : 'Отклонить' }}
          </button>
          <button class="btn btn-ghost btn-sm" :disabled="peoBulkBusy" @click="runPeoBulk('revoke')">
            ↩ {{ peoBulkBusy && peoBulkAction === 'revoke' ? 'Снятие…' : 'Снять согласование' }}
          </button>
        </div>
        <div v-if="peoBulkBusy" class="peo-bulk-progress">
          Обработано {{ peoBulkProgress.done }} из {{ peoBulkProgress.total }}…
        </div>
      </div>
      <div v-if="peoBulkStatus || peoBulkError" class="peo-bulk-result">
        <span v-if="peoBulkStatus" class="peo-bulk-ok">{{ peoBulkStatus }}</span>
        <span v-if="peoBulkError" class="peo-bulk-err">{{ peoBulkError }}</span>
        <button class="peo-bulk-x" aria-label="Закрыть" @click="peoBulkStatus = ''; peoBulkError = ''">×</button>
      </div>

      <div class="table-wrap" @keydown="onTableKeydown" @focusin="onGridFocusIn" tabindex="0">
        <table id="cost-table-1" class="data-table compact">
          <thead>
            <tr>
              <th v-if="isVisible('peo_sel')" :class="stickyClasses('peo_sel')" :style="stickyStyle('peo_sel')" class="col-peo-sel">
                <input
                  type="checkbox"
                  :checked="peoPageAllSelected"
                  :indeterminate.prop="peoPageSomeSelected"
                  :disabled="!peoPageSelectableRows.length"
                  title="Выбрать все строки страницы"
                  @change="togglePeoPage(($event.target as HTMLInputElement).checked)"
                />
              </th>
              <th :class="stickyClasses('actions')" :style="stickyStyle('actions')"><span class="col-resize-handle" @mousedown.stop.prevent="startColResize('actions', $event)" @dblclick.stop.prevent="resetColWidth('actions')" title="Изменить ширину · двойной клик — сброс"></span></th>
              <th :class="stickyClasses('raw_rows')" :style="stickyStyle('raw_rows')"><span class="col-resize-handle" @mousedown.stop.prevent="startColResize('raw_rows', $event)" @dblclick.stop.prevent="resetColWidth('raw_rows')" title="Изменить ширину · двойной клик — сброс"></span></th>
              <th v-if="isVisible('bm')" :class="[{ sorted: sortField === 'Бренд-менеджер' }, ...stickyClasses('bm')]" :style="stickyStyle('bm')" @click="toggleSort('Бренд-менеджер')">
                Бренд-менеджер<span v-if="sortField === 'Бренд-менеджер'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
                <span class="col-resize-handle" @mousedown.stop.prevent="startColResize('bm', $event)" @dblclick.stop.prevent="resetColWidth('bm')" title="Изменить ширину · двойной клик — сброс"></span>
              </th>
              <th v-if="isVisible('model')" :class="[{ sorted: sortField === 'Модель' }, ...stickyClasses('model')]" :style="stickyStyle('model')" @click="toggleSort('Модель')">
                Модель<span v-if="sortField === 'Модель'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
                <span class="col-resize-handle" @mousedown.stop.prevent="startColResize('model', $event)" @dblclick.stop.prevent="resetColWidth('model')" title="Изменить ширину · двойной клик — сброс"></span>
              </th>
              <th v-if="isVisible('articul')" :class="[{ sorted: sortField === 'Артикул' }, ...stickyClasses('articul')]" :style="stickyStyle('articul')" @click="toggleSort('Артикул')">
                Артикул<span v-if="sortField === 'Артикул'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
                <span class="col-resize-handle" @mousedown.stop.prevent="startColResize('articul', $event)" @dblclick.stop.prevent="resetColWidth('articul')" title="Изменить ширину · двойной клик — сброс"></span>
              </th>
              <th v-if="isVisible('model_name')" :class="[{ sorted: sortField === 'Наименование модели' }, ...stickyClasses('model_name')]" :style="stickyStyle('model_name')" @click="toggleSort('Наименование модели')">
                Наименование модели<span v-if="sortField === 'Наименование модели'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
                <span class="col-resize-handle" @mousedown.stop.prevent="startColResize('model_name', $event)" @dblclick.stop.prevent="resetColWidth('model_name')" title="Изменить ширину · двойной клик — сброс"></span>
              </th>
              <th v-if="isVisible('color')" :class="[{ sorted: sortField === 'color' }, ...stickyClasses('color')]" :style="stickyStyle('color')" @click="toggleSort('color')">
                Цвет<span v-if="sortField === 'color'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
                <span class="col-resize-handle" @mousedown.stop.prevent="startColResize('color', $event)" @dblclick.stop.prevent="resetColWidth('color')" title="Изменить ширину · двойной клик — сброс"></span>
              </th>
              <th v-if="isVisible('task_num')" :class="[{ sorted: sortField === 'Номер задания производства' }, ...stickyClasses('task_num')]" :style="stickyStyle('task_num')" @click="toggleSort('Номер задания производства')">
                № задания<span v-if="sortField === 'Номер задания производства'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
                <span class="col-resize-handle" @mousedown.stop.prevent="startColResize('task_num', $event)" @dblclick.stop.prevent="resetColWidth('task_num')" title="Изменить ширину · двойной клик — сброс"></span>
              </th>
              <th v-if="isVisible('plan_id')" :class="[{ sorted: sortField === 'PLAN_ID' }, ...stickyClasses('plan_id')]" :style="stickyStyle('plan_id')" @click="toggleSort('PLAN_ID')">
                PLAN_ID<span v-if="sortField === 'PLAN_ID'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
                <span class="col-resize-handle" @mousedown.stop.prevent="startColResize('plan_id', $event)" @dblclick.stop.prevent="resetColWidth('plan_id')" title="Изменить ширину · двойной клик — сброс"></span>
              </th>
              <th v-if="isVisible('country')" :class="{ sorted: sortField === 'Страна пр-ва' }" @click="toggleSort('Страна пр-ва')">
                Страна<span v-if="sortField === 'Страна пр-ва'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('family')" :class="{ sorted: sortField === 'Семья' }" @click="toggleSort('Семья')">
                Семья<span v-if="sortField === 'Семья'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('season')" :class="{ sorted: sortField === 'Сезон' }" @click="toggleSort('Сезон')">
                Сезон<span v-if="sortField === 'Сезон'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('date')" :class="{ sorted: sortField === 'дата расчета' }" @click="toggleSort('дата расчета')">
                Дата<span v-if="sortField === 'дата расчета'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('calc_sign')" :class="{ sorted: sortField === 'Признак калькуляции' }" @click="toggleSort('Признак калькуляции')">
                Пр.кальк<span v-if="sortField === 'Признак калькуляции'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('planned_retail')" class="col-num">План. розница</th>
              <th v-if="isVisible('planned_wholesale')" class="col-num">План. опт</th>
              <th v-if="isVisible('planned_cost')" class="col-num">План. с/с</th>
              <th v-if="isVisible('planned_profitability')" class="col-num col-metric col-metric-hl" title="План. опт / План. с/с − 1">План. рентаб. (%)</th>
              <th v-if="isVisible('avg_retail_rub')" class="col-num" :class="{ sorted: sortField === 'avg_Розничная цена по уровню, руб.' }" @click="toggleSort('avg_Розничная цена по уровню, руб.')">
                Сред. розница (руб)<span v-if="sortField === 'avg_Розничная цена по уровню, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('avg_rate')" class="col-num" :class="{ sorted: sortField === 'avg_Курс на дату расчета' }" @click="toggleSort('avg_Курс на дату расчета')">
                Курс (руб)<span v-if="sortField === 'avg_Курс на дату расчета'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('retail_markup')" class="col-num">
                Розничная наценка
              </th>
              <th v-if="isVisible('price_rf')" class="col-num">Цена РФ</th>
              <th v-if="isVisible('price_kz')" class="col-num">Цена КЗ</th>
              <th v-if="isVisible('price_uz')" class="col-num">Цена УЗ</th>
              <th v-if="isVisible('mp_price_rub')" class="col-num">Цена для МП, рос. руб.</th>
              <th v-if="isVisible('comment')">Комментарий</th>
              <th v-if="isVisible('avg_wholesale') && !showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Отпускная цена по уровню, руб' }" @click="toggleSort('avg_Отпускная цена по уровню, руб')">
                Сред. опт (руб)<span v-if="sortField === 'avg_Отпускная цена по уровню, руб'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('price_level')" :class="{ sorted: sortField === 'Уровень цен' }" @click="toggleSort('Уровень цен')">
                Уровень цен<span v-if="sortField === 'Уровень цен'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('avg_retail_usd') && showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Розничная цена по уровню, USD.' }" @click="toggleSort('avg_Розничная цена по уровню, USD.')">
                Сред. розница ($)<span v-if="sortField === 'avg_Розничная цена по уровню, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('avg_retail_usd') && showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Отпускная цена по уровню, USD.' }" @click="toggleSort('avg_Отпускная цена по уровню, USD.')">
                Сред. опт ($)<span v-if="sortField === 'avg_Отпускная цена по уровню, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_materials') && !showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Основные материалы, руб.' }" @click="toggleSort('sum_Основные материалы, руб.')">
                Осн. материалы (руб)<span v-if="sortField === 'sum_Основные материалы, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_materials') && showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Основные материалы, USD.' }" @click="toggleSort('sum_Основные материалы, USD.')">
                Осн. материалы ($)<span v-if="sortField === 'sum_Основные материалы, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_aux_materials') && !showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Вспомогательные материалы, руб.' }" @click="toggleSort('sum_Вспомогательные материалы, руб.')">
                Вспом. (руб)<span v-if="sortField === 'sum_Вспомогательные материалы, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_aux_materials') && showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Вспомогательные материалы, USD.' }" @click="toggleSort('sum_Вспомогательные материалы, USD.')">
                Вспом. ($)<span v-if="sortField === 'sum_Вспомогательные материалы, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('avg_sewing_min')" class="col-num" :class="{ sorted: sortField === 'avg_Пошив, минуты' }" @click="toggleSort('avg_Пошив, минуты')">
                Пошив (мин)<span v-if="sortField === 'avg_Пошив, минуты'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_sewing') && !showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Пошив, руб.' }" @click="toggleSort('sum_Пошив, руб.')">
                Пошив (руб)<span v-if="sortField === 'sum_Пошив, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_sewing') && showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Пошив, USD.' }" @click="toggleSort('sum_Пошив, USD.')">
                Пошив ($)<span v-if="sortField === 'sum_Пошив, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('avg_cutting_min')" class="col-num" :class="{ sorted: sortField === 'avg_Раскрой, минуты' }" @click="toggleSort('avg_Раскрой, минуты')">
                Раскрой (мин)<span v-if="sortField === 'avg_Раскрой, минуты'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_cutting') && !showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Раскрой, руб.' }" @click="toggleSort('sum_Раскрой, руб.')">
                Раскрой (руб)<span v-if="sortField === 'sum_Раскрой, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_cutting') && showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Раскрой, USD.' }" @click="toggleSort('sum_Раскрой, USD.')">
                Раскрой ($)<span v-if="sortField === 'sum_Раскрой, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_decors') && !showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Декоры, руб.' }" @click="toggleSort('sum_Декоры, руб.')">
                Декоры (руб)<span v-if="sortField === 'sum_Декоры, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_decors') && showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Декоры, USD.' }" @click="toggleSort('sum_Декоры, USD.')">
                Декоры ($)<span v-if="sortField === 'sum_Декоры, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_knitting') && !showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Вязание, руб.' }" @click="toggleSort('sum_Вязание, руб.')">
                Вязание (руб)<span v-if="sortField === 'sum_Вязание, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_knitting') && showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Вязание, USD.' }" @click="toggleSort('sum_Вязание, USD.')">
                Вязание ($)<span v-if="sortField === 'sum_Вязание, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_cost') && !showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Себестоимость, руб.' }" @click="toggleSort('sum_Себестоимость, руб.')">
                Себест. (руб)<span v-if="sortField === 'sum_Себестоимость, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('sum_cost') && showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Себестоимость, USD.' }" @click="toggleSort('sum_Себестоимость, USD.')">
                Себест. ($)<span v-if="sortField === 'sum_Себестоимость, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('calc_markup')" class="col-num col-metric" :class="{ sorted: sortField === 'calc_markup_rub' }" @click="toggleSort('calc_markup_rub')">
                Рентабельность <template v-if="showUSD">($)</template><template v-else>(руб)</template><span v-if="sortField === 'calc_markup_rub'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('calc_markup_pct')" class="col-num col-metric col-metric-hl" :class="{ sorted: sortField === 'calc_markup_pct' }" @click="toggleSort('calc_markup_pct')">
                Рентабельность (%)<span v-if="sortField === 'calc_markup_pct'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('calc_margin_pct')" class="col-num col-metric col-metric-hl" :class="{ sorted: sortField === 'calc_margin_pct' }" @click="toggleSort('calc_margin_pct')">
                Маржа (%)<span v-if="sortField === 'calc_margin_pct'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('calc_margin_deviation')" class="col-num col-metric" :class="{ sorted: sortField === 'calc_margin_deviation' }" @click="toggleSort('calc_margin_deviation')">
                Откл. маржи (%)<span v-if="sortField === 'calc_margin_deviation'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('peo')" class="col-peo">ПЭО</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td :colspan="visibleColumnCount" class="muted" style="text-align: center; padding: 24px">
                Загрузка данных…
              </td>
            </tr>
            <tr v-else-if="!pageRows.length">
              <td :colspan="visibleColumnCount" class="muted" style="text-align: center; padding: 24px">
                Нет данных. Загрузите данные кнопкой выше.
              </td>
            </tr>
            <tr
              v-for="(row, idx) in pageRows"
              :key="idx"
              :class="{ locked: isRowLocked(row), 'row-pending': row._has_pending, 'row-audit': row._has_audit, selected: selectedRowIndex === getOriginalIndex(row), ...marginRowClass(row) }"
              @click="selectRow(getOriginalIndex(row))"
            >
              <td v-if="isVisible('peo_sel')" :class="stickyClasses('peo_sel')" :style="stickyStyle('peo_sel')" class="col-peo-sel">
                <input
                  type="checkbox"
                  :checked="isPeoSelected(row)"
                  :disabled="!canSelectForPeo(row)"
                  :title="canSelectForPeo(row) ? 'Выбрать для массового согласования (Shift — диапазон)' : 'Строка недоступна для согласования'"
                  @click.stop="onPeoCheckboxClick"
                  @change="onPeoCheckboxChange(row, idx, ($event.target as HTMLInputElement).checked)"
                />
              </td>
              <td :class="stickyClasses('actions')" :style="stickyStyle('actions')">
                <span v-if="isRowLocked(row) && !row._has_pending && !row._has_audit" class="lock-icon" title="Строка заблокирована">🔒</span>
                <span v-if="row._has_pending" class="state-badge state-badge--pending" title="Ожидает согласования">⏳</span>
                <!-- Значок «в DWH» показываем по факту записи (_in_dwh), а не по
                     блокировке: переоткрытая калькуляция всё ещё записана, но
                     правку уже разрешили. Админу значок кликабелен — открывает
                     переоткрытие/отзыв. -->
                <span v-if="row._in_dwh ?? row._has_audit"
                  class="state-badge"
                  :class="[row._reopened ? 'state-badge--reopened' : 'state-badge--audit', { 'state-badge--action': can('cost:admin') }]"
                  :title="row._reopened
                    ? 'Записано в DWH, открыто на исправление' + (can('cost:admin') ? ' — клик, чтобы отозвать' : '')
                    : 'Записано в DWH' + (can('cost:admin') ? ' — клик, чтобы вернуть на корректировку' : '')"
                  @click.stop="can('cost:admin') ? openReopenModal(row) : null">{{ row._reopened ? '🔓' : '📤' }}</span>
                <button class="btn-details" @click.stop="openDetails(row)">🔍</button>
                <button v-if="!isRowLocked(row) && !row._has_audit" class="btn-edit" @click.stop="openVersionEditor(row)" title="Редактировать расчёт">🖊</button>
              </td>
              <td :class="stickyClasses('raw_rows')" :style="stickyStyle('raw_rows')"><button class="btn-details" @click.stop="openRawRows(row)" title="Исходные строки">📋</button></td>
              <td v-if="isVisible('bm')" :class="stickyClasses('bm')" :style="stickyStyle('bm')">{{ row['Бренд-менеджер'] || '—' }}</td>
              <td v-if="isVisible('model')" :class="stickyClasses('model')" :style="stickyStyle('model')">{{ row['Модель'] || '—' }}</td>
              <td v-if="isVisible('articul')" :class="stickyClasses('articul')" :style="stickyStyle('articul')">{{ row['Артикул'] || '—' }}</td>
              <td v-if="isVisible('model_name')" :class="stickyClasses('model_name')" :style="stickyStyle('model_name')">{{ row['Наименование модели'] || '—' }}</td>
              <td v-if="isVisible('color')" :class="stickyClasses('color')" :style="stickyStyle('color')">{{ row['color'] || '—' }}</td>
              <td v-if="isVisible('task_num')" :class="stickyClasses('task_num')" :style="stickyStyle('task_num')">{{ row['Номер задания производства'] || '—' }}</td>
              <td v-if="isVisible('plan_id')" :class="stickyClasses('plan_id')" :style="stickyStyle('plan_id')">{{ row['PLAN_ID'] || '—' }}</td>
              <td v-if="isVisible('country')">{{ row['Страна пр-ва'] || '—' }}</td>
              <td v-if="isVisible('family')">{{ row['Семья'] || '—' }}</td>
              <td v-if="isVisible('season')">{{ row['Сезон'] || '—' }}</td>
              <td v-if="isVisible('date')" class="num">{{ formatDate(row['дата расчета']) }}</td>
              <td v-if="isVisible('calc_sign')">{{ row['Признак калькуляции'] || '—' }}</td>
              <td v-if="isVisible('planned_retail')" class="col-num num">{{ row.planned_retail != null ? fmt(row.planned_retail) : '—' }}</td>
              <td v-if="isVisible('planned_wholesale')" class="col-num num">{{ row.planned_wholesale != null ? fmt(row.planned_wholesale) : '—' }}</td>
              <td v-if="isVisible('planned_cost')" class="col-num num">{{ row.planned_cost != null ? fmt(row.planned_cost) : '—' }}</td>
              <td v-if="isVisible('planned_profitability')" class="col-num num col-metric col-metric-hl"
                  :class="plannedProfitabilityPct(row) == null ? '' : (plannedProfitabilityPct(row)! >= 0 ? 'delta-pos' : 'delta-neg')">
                {{ plannedProfitabilityPct(row) == null ? '—' : plannedProfitabilityPct(row)!.toFixed(1) + '%' }}
              </td>
              <td v-if="isVisible('avg_retail_rub')">
                <select
                  class="price-select"
                  :value="Number(row['avg_Розничная цена по уровню, руб.']) || ''"
                  :disabled="isFkssRow(row) || !can('cost:edit_price') || isRowLocked(row)"
                  :title="isFkssRow(row) ? FKSS_HINT : ''"
                  @click.stop
                  @change="onRetailPriceSelect(row, ($event.target as HTMLSelectElement).value)"
                >
                  <option value="">—</option>
                  <option v-for="rp in uniqueRetailPrices" :key="rp" :value="rp">{{ fmt(rp) }}</option>
                </select>
              </td>
              <td v-if="isVisible('avg_rate')" class="col-num num">{{ row['avg_Курс на дату расчета'] != null ? fmt(row['avg_Курс на дату расчета']) : '—' }}</td>
              <td v-if="isVisible('retail_markup')">
                <select
                  class="price-select"
                  :value="markupSelections[calcRowKey(row)] || ''"
                  :disabled="isFkssRow(row) || !row['avg_Розничная цена по уровню, руб.'] || !can('cost:edit_price') || isRowLocked(row)"
                  :title="isFkssRow(row) ? FKSS_HINT : ''"
                  @click.stop
                  @change="onMarkupSelect(row, ($event.target as HTMLSelectElement).value)"
                >
                  <option value="">—</option>
                  <option v-for="opt in getMarkupOptions(row)" :key="opt.value" :value="opt.value">
                    {{ opt.label }}
                  </option>
                </select>
              </td>
              <td v-if="isVisible('price_rf')">
                 <input class="price-input" type="number"
                  :value="priceRF[calcRowKey(row)] ?? ''"
                  placeholder="Цена РФ"
                  :disabled="isFkssRow(row) || !can('cost:edit_price') || isRowLocked(row)"
                  :title="isFkssRow(row) ? FKSS_HINT : ''"
                  @click.stop
                  @input="onPriceRFInput(row, ($event.target as HTMLInputElement).value)"
                />
              </td>
              <td v-if="isVisible('price_kz')">
                 <input class="price-input" type="number"
                  :value="priceKZ[calcRowKey(row)] ?? ''"
                  placeholder="Цена КЗ"
                  :disabled="isFkssRow(row) || !can('cost:edit_price') || isRowLocked(row)"
                  :title="isFkssRow(row) ? FKSS_HINT : ''"
                  @click.stop
                  @input="onPriceKZInput(row, ($event.target as HTMLInputElement).value)"
                />
              </td>
              <td v-if="isVisible('price_uz')">
                 <input class="price-input" type="number"
                  :value="priceUZ[calcRowKey(row)] ?? ''"
                  placeholder="Цена УЗ"
                  :disabled="isFkssRow(row) || !can('cost:edit_price') || isRowLocked(row)"
                  :title="isFkssRow(row) ? FKSS_HINT : ''"
                  @click.stop
                  @input="onPriceUZInput(row, ($event.target as HTMLInputElement).value)"
                />
              </td>
              <td v-if="isVisible('mp_price_rub')" class="col-num num">{{ fmt(row['mp_price_rub']) }}</td>
              <td v-if="isVisible('comment')">
                <input class="comment-input" type="text"
                  :value="comments[calcRowKey(row)] ?? ''"
                  placeholder="..."
                  :disabled="isFkssRow(row) || isRowLocked(row)"
                  :title="isFkssRow(row) ? FKSS_HINT : ''"
                  @click.stop
                  @input="onCommentInput(row, ($event.target as HTMLInputElement).value)"
                />
              </td>
              <td v-if="isVisible('avg_wholesale') && !showUSD" class="col-num num">{{ fmt(row['avg_Отпускная цена по уровню, руб']) }}</td>
              <td v-if="isVisible('price_level')">{{ row['Уровень цен'] || '—' }}</td>
              <td v-if="isVisible('avg_retail_usd') && showUSD" class="col-num num">{{ fmt(row['avg_Розничная цена по уровню, USD.']) }}</td>
              <td v-if="isVisible('avg_retail_usd') && showUSD" class="col-num num">{{ fmt(row['avg_Отпускная цена по уровню, USD.']) }}</td>
              <td v-if="isVisible('sum_materials') && !showUSD" class="col-num num">{{ fmt(row['sum_Основные материалы, руб.']) }}</td>
              <td v-if="isVisible('sum_materials') && showUSD" class="col-num num">{{ fmt(row['sum_Основные материалы, USD.']) }}</td>
              <td v-if="isVisible('sum_aux_materials') && !showUSD" class="col-num num">{{ fmt(row['sum_Вспомогательные материалы, руб.']) }}</td>
              <td v-if="isVisible('sum_aux_materials') && showUSD" class="col-num num">{{ fmt(row['sum_Вспомогательные материалы, USD.']) }}</td>
              <td v-if="isVisible('avg_sewing_min')" class="col-num num">{{ fmt(row['avg_Пошив, минуты']) }}</td>
              <td v-if="isVisible('sum_sewing') && !showUSD" class="col-num num">{{ fmt(row['sum_Пошив, руб.']) }}</td>
              <td v-if="isVisible('sum_sewing') && showUSD" class="col-num num">{{ fmt(row['sum_Пошив, USD.']) }}</td>
              <td v-if="isVisible('avg_cutting_min')" class="col-num num">{{ fmt(row['avg_Раскрой, минуты']) }}</td>
              <td v-if="isVisible('sum_cutting') && !showUSD" class="col-num num">{{ fmt(row['sum_Раскрой, руб.']) }}</td>
              <td v-if="isVisible('sum_cutting') && showUSD" class="col-num num">{{ fmt(row['sum_Раскрой, USD.']) }}</td>
              <td v-if="isVisible('sum_decors') && !showUSD" class="col-num num">{{ fmt(row['sum_Декоры, руб.']) }}</td>
              <td v-if="isVisible('sum_decors') && showUSD" class="col-num num">{{ fmt(row['sum_Декоры, USD.']) }}</td>
              <td v-if="isVisible('sum_knitting') && !showUSD" class="col-num num">{{ fmt(row['sum_Вязание, руб.']) }}</td>
              <td v-if="isVisible('sum_knitting') && showUSD" class="col-num num">{{ fmt(row['sum_Вязание, USD.']) }}</td>
              <td v-if="isVisible('sum_cost') && !showUSD" class="col-num num-strong">{{ fmt(row['sum_Себестоимость, руб.']) }}</td>
              <td v-if="isVisible('sum_cost') && showUSD" class="col-num num-strong">{{ fmt(row['sum_Себестоимость, USD.']) }}</td>
              <td v-if="isVisible('calc_markup')" class="col-num num col-metric">{{ fmt(calc(row, showUSD).markupRub) }}</td>
              <td v-if="isVisible('calc_markup_pct')" class="col-num num col-metric col-metric-hl" :class="calc(row, showUSD).markupPct >= 0 ? 'delta-pos' : 'delta-neg'">
                {{ calc(row, showUSD).markupPct.toFixed(1) }}%
              </td>
              <td v-if="isVisible('calc_margin_pct')" class="col-num num col-metric col-metric-hl">{{ calc(row, showUSD).marginPct.toFixed(1) }}%</td>
              <td v-if="isVisible('calc_margin_deviation')" class="col-num num col-metric" :class="marginDevClass(row, showUSD)">{{ marginDevText(row, showUSD) }}</td>
              <td v-if="isVisible('peo')" class="col-peo" :class="{ 'peo-readonly': !can('cost:approve') && !can('cost:peo_mark'), 'peo-active': approvalTarget === row }">
                <span v-if="row.peo_status === 'approved'" class="peo-badge peo-approved" :class="{ 'peo-readonly': isRowLocked(row) || row._has_audit }" :title="'Согласовано: ' + (row.peo_approved_by || '—') + (row.peo_approved_at ? ' ' + new Date(row.peo_approved_at).toLocaleDateString('ru-RU') : '')" @click.stop="(isRowLocked(row) || row._has_audit) ? null : openApprovalPopup(row)">🟢</span>
                <span v-else-if="row.peo_status === 'rejected'" class="peo-badge peo-rejected" :class="{ 'peo-readonly': isRowLocked(row) || row._has_audit }" @click.stop="(isRowLocked(row) || row._has_audit) ? null : openApprovalPopup(row)">🔴</span>
                <!-- Возврат на корректировку: не «отклонено», а «жду исправленную
                     цену». Введённое бренд-менеджером значение при возврате
                     сохраняется, поэтому строка остаётся с заполненной ценой. -->
                <span v-else-if="row.peo_status === 'returned'" class="peo-badge peo-returned" :class="{ 'peo-readonly': isRowLocked(row) || row._has_audit }" :title="'Возврат на корректировку' + (row.peo_approved_by ? ': ' + row.peo_approved_by : '')" @click.stop="(isRowLocked(row) || row._has_audit) ? null : openApprovalPopup(row)">🟠</span>
                <span v-else class="peo-badge peo-none" :class="{ 'peo-readonly': isRowLocked(row) || row._has_audit }" @click.stop="(isRowLocked(row) || row._has_audit) ? null : openApprovalPopup(row)">⚪</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Details Modal -->
    <div v-if="showDetailsModal" class="modal-overlay" @click.self="showDetailsModal = false">
      <div class="modal-content modal-wide" @click.stop>
        <div class="modal-header">
          <h2>Детализация: {{ detailsModel }}</h2>
          <div class="modal-header-actions">
            <button class="btn btn-ghost btn-sm" @click="openDetailsInNewTab">Открыть в новом окне <Icon name="lucide:external-link" /></button>
            <button class="modal-close" @click="showDetailsModal = false">×</button>
          </div>
        </div>
        <!-- Filter panel -->
        <div class="details-filters">
          <label>Дата от <input v-model="detailDateFrom" type="date" class="form-input" /></label>
          <label>Дата до <input v-model="detailDateTo" type="date" class="form-input" /></label>
          <label>Признак калькуляции
            <CostMultiSelect
              v-model="detailCalcSign"
              :options="enrichFilterOptions('calc_sign', filterOptions['calc_sign'] || [])"
              placeholder="Все · признак калькуляции"
              @change="loadDetailsData"
            />
          </label>
          <button class="btn btn-primary btn-sm" @click="loadDetailsData">Применить</button>
          <button class="btn btn-ghost btn-sm" @click="resetDetailsFilters">Сбросить</button>
        </div>
        <!-- Column filters row -->
        <div class="details-col-filters" v-if="detailsAllData.length">
          <div v-for="cfg in detailsFilterConfig" :key="cfg.key" class="details-col-filter-item" :class="{ locked: isDetFilterLocked(cfg.key) }">
            <label>{{ cfg.label }}</label>
            <CostMultiSelect
              v-model="detailsColumnFilters[cfg.key]"
              :options="enrichFilterOptions(cfg.key, detailsColumnFilterOptions[cfg.key] || [])"
              :placeholder="isDetFilterLocked(cfg.key) ? '—' : `Все · ${cfg.label.toLowerCase()}`"
              :disabled="isDetFilterLocked(cfg.key)"
              @change="onDetFilterChange(cfg.key)"
            />
          </div>
          <a href="#" class="details-col-filter-reset" @click.prevent="resetDetailsColumnFilters">Сбросить фильтры колонок</a>
        </div>
        <!-- Details table -->
        <div class="table-wrap details-table-wrap">
          <div v-if="detailsLoading" class="muted" style="text-align:center;padding:24px">Загрузка детализации…</div>
          <div v-else-if="!detailsFilteredData.length" class="muted" style="text-align:center;padding:24px">Нет данных</div>
          <table v-else class="data-table compact">
            <thead>
              <tr>
                <th>Дата</th>
                <th>Пр.кальк</th>
                <th>Артикул</th>
                <th>Наименование</th>
                <th>№ задания</th>
                <th>Дата выпуска</th>
                <th class="col-num">Розница (руб)</th>
                <th class="col-num">Опт (руб)</th>
                <th class="col-num">Осн. мат.</th>
                <th class="col-num">Вспом.</th>
                <th class="col-num">Пошив</th>
                <th class="col-num">Раскрой</th>
                <th class="col-num">Декор</th>
                <th class="col-num">Вязание</th>
                <th class="col-num">Себест.</th>
                <th class="col-num">Рентабельность</th>
                <th class="col-num">Рентабельность %</th>
                <th class="col-num">Маржа %</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(d, i) in detailsFilteredData" :key="i">
                <td>{{ formatDate(d['дата расчета']) }}</td>
                <td>{{ d['Признак калькуляции'] || '—' }}</td>
                <td>{{ d['Артикул'] || '—' }}</td>
                <td>{{ d['Наименование модели'] || '—' }}</td>
                <td>{{ d['Номер задания производства'] || '—' }}</td>
                <td>{{ formatDate(d['Дата выпуска']) }}</td>
                <td class="col-num num" :style="heatBg(d['Розничная цена, руб.'], 'Розничная цена, руб.')">{{ fmt(d['Розничная цена, руб.']) }}</td>
                <td class="col-num num" :style="heatBg(d['Оптовая цена, руб.'], 'Оптовая цена, руб.')">{{ fmt(d['Оптовая цена, руб.']) }}</td>
                <td class="col-num num" :style="heatBg(d['Осн. материалы, руб.'], 'Осн. материалы, руб.')">{{ fmt(d['Осн. материалы, руб.']) }}</td>
                <td class="col-num num" :style="heatBg(d['Вспом. материалы, руб.'], 'Вспом. материалы, руб.')">{{ fmt(d['Вспом. материалы, руб.']) }}</td>
                <td class="col-num num" :style="heatBg(d['Пошив, руб.'], 'Пошив, руб.')">{{ fmt(d['Пошив, руб.']) }}</td>
                <td class="col-num num" :style="heatBg(d['Раскрой, руб.'], 'Раскрой, руб.')">{{ fmt(d['Раскрой, руб.']) }}</td>
                <td class="col-num num" :style="heatBg(d['Декор, руб.'], 'Декор, руб.')">{{ fmt(d['Декор, руб.']) }}</td>
                <td class="col-num num" :style="heatBg(d['Вязание, руб.'], 'Вязание, руб.')">{{ fmt(d['Вязание, руб.']) }}</td>
                <td class="col-num num-strong" :style="heatBg(d['Себестоимость, руб.'], 'Себестоимость, руб.')">{{ fmt(d['Себестоимость, руб.']) }}</td>
                <td class="col-num num" :style="heatBg(d['Наценка, руб.'], 'Наценка, руб.')">{{ fmt(d['Наценка, руб.']) }}</td>
                <td class="col-num num" :style="heatBg(d['Наценка, %'], 'Наценка, %')">{{ d['Наценка, %'] }}%</td>
                <td class="col-num num" :style="heatBg(d['Маржинальность, %'], 'Маржинальность, %')">{{ d['Маржинальность, %'] }}%</td>
              </tr>
            </tbody>
          </table>
          <div v-if="detailsFilteredData.length" class="details-count">Найдено строк: {{ detailsFilteredData.length }}</div>
        </div>
      </div>
    </div>
    <!-- Raw rows modal -->
    <div v-if="showRawRowsModal" class="modal-overlay" @click.self="showRawRowsModal = false">
      <div class="modal-content modal-wide" @click.stop>
        <div class="modal-header">
          <h2>Исходные строки</h2>
          <div class="modal-header-actions">
            <button class="modal-close" @click="showRawRowsModal = false">×</button>
          </div>
        </div>
        <div class="raw-rows-banner" v-if="rawRowsData.length">
          <strong>Фильтры:</strong>
          <span v-for="(val, fld) in rawRowsFilterSummary" :key="fld" class="raw-rows-tag">
            {{ fld }}: {{ val }}
          </span>
          <span class="raw-rows-count">
            Найдено строк: {{ rawRowsData.length }}
          </span>
        </div>
        <div v-if="lastError && showRawRowsModal" class="cost-error" style="margin:var(--sp-3) var(--sp-5);flex-shrink:0">
          <Icon name="lucide:alert-triangle" />
          <div>
            <strong>Ошибка загрузки исходных строк</strong>
            <p>{{ lastError }}</p>
          </div>
          <button class="cost-error-x" @click="lastError = ''" aria-label="Закрыть">×</button>
        </div>
        <div class="table-wrap raw-rows-table-wrap">
          <div v-if="rawRowsLoading" class="muted" style="text-align:center;padding:24px">Загрузка исходных строк…</div>
          <div v-else-if="!rawRowsData.length" class="muted" style="text-align:center;padding:24px">Нет данных</div>
          <table v-else class="data-table compact">
            <thead>
              <tr>
                <th v-for="col in rawRowsColumns" :key="col" :class="{ 'col-num': isRawRowsNumeric(col) }">{{ rawRowsColumnLabel(col) }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(r, ri) in rawRowsData" :key="ri" :class="{ 'row-zero-cost': isZeroCostRow(r) }">
                <td v-for="col in rawRowsColumns" :key="col" :class="{ num: isRawRowsNumeric(col) }">{{ formatRawRowsCell(r[col], col) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
    <!-- Margin targets modal -->
    <div v-if="showMarginModal" class="modal-overlay" @click.self="showMarginModal = false">
      <div class="modal-content" style="max-width:600px" @click.stop>
        <div class="modal-header">
          <h2>Таргеты маржинальности</h2>
          <button class="modal-close" @click="showMarginModal = false">×</button>
        </div>
        <div class="margin-targets-body" style="padding:var(--sp-4) var(--sp-5);overflow:auto;flex:1">
          <div v-if="marginTargetsLoading" class="muted" style="text-align:center;padding:24px">Загрузка…</div>
          <table v-else class="data-table compact" style="width:100%">
            <thead>
              <tr>
                <th style="text-align:left">Level 01</th>
                <th class="col-num">Таргет маржинальности, %</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="mt in marginTargetsList" :key="mt.level1">
                <td>{{ mt.level1 }}</td>
                <td class="col-num">
                  <input
                    v-model.number="mt.target_margin_pct"
                    type="number"
                    step="0.01"
                    min="0"
                    max="100"
                    class="form-input"
                    style="width:100px;text-align:right"
                    placeholder="—"
                  />
                </td>
              </tr>
              <tr v-if="!marginTargetsList.length">
                <td colspan="2" class="muted" style="text-align:center;padding:16px">
                  Нет данных Level 01
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div style="padding:var(--sp-3) var(--sp-5);border-top:1px solid var(--border);display:flex;justify-content:space-between;align-items:center;flex-shrink:0">
          <span style="font-size:var(--fs-xs);color:var(--text-muted)">{{ marginSaveStatus }}</span>
          <div style="display:flex;gap:var(--sp-3)">
            <button class="btn btn-ghost btn-sm" @click="showMarginModal = false">Отмена</button>
            <button class="btn btn-primary btn-sm" :disabled="marginSaving" @click="saveMarginTargets">
              {{ marginSaving ? 'Сохранение…' : 'Сохранить' }}
            </button>
          </div>
        </div>
      </div>
    </div>
    <!-- MP constants modal -->
    <div v-if="showMpConstantsModal" class="modal-overlay" @click.self="showMpConstantsModal = false">
      <div class="modal-content" style="max-width:700px" @click.stop>
        <div class="modal-header">
          <h2>Константы МП</h2>
          <button class="modal-close" @click="showMpConstantsModal = false">×</button>
        </div>
        <div style="padding:var(--sp-4) var(--sp-5);overflow:auto;flex:1">
          <p class="muted" style="margin-bottom:var(--sp-3)">
            История значений для формулы «Цена для МП, рос. руб.». Применяется всегда самая свежая запись.
          </p>
          <div v-if="mpConstantsLoading" class="muted" style="text-align:center;padding:24px">Загрузка…</div>
          <table v-else class="data-table compact" style="width:100%;margin-bottom:var(--sp-4)">
            <thead>
              <tr>
                <th>Дата</th>
                <th class="col-num">Наценка МП</th>
                <th class="col-num">% расходов МП</th>
                <th class="col-num">Скидка СПП</th>
                <th>Кто добавил</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in mpConstantsList" :key="c.id">
                <td>{{ formatDate(c.effective_date) }}</td>
                <td class="col-num num">{{ c.markup_mp }}</td>
                <td class="col-num num">{{ c.expense_pct_mp }}</td>
                <td class="col-num num">{{ c.spp_discount }}</td>
                <td>{{ c.created_by }}</td>
              </tr>
              <tr v-if="!mpConstantsList.length">
                <td colspan="5" class="muted" style="text-align:center;padding:16px">Константы ещё не задавались</td>
              </tr>
            </tbody>
          </table>
          <div style="border-top:1px solid var(--border);padding-top:var(--sp-3)">
            <strong style="display:block;margin-bottom:var(--sp-2)">Добавить новое значение</strong>
            <div style="display:flex;gap:var(--sp-3);flex-wrap:wrap;align-items:flex-end">
              <label>Дата<br/>
                <input v-model="mpConstantsForm.effective_date" type="date" class="form-input" />
              </label>
              <label>Наценка МП<br/>
                <input v-model.number="mpConstantsForm.markup_mp" type="number" step="0.0001" class="form-input" style="width:110px" />
              </label>
              <label>% расходов МП<br/>
                <input v-model.number="mpConstantsForm.expense_pct_mp" type="number" step="0.0001" class="form-input" style="width:110px" />
              </label>
              <label>Скидка СПП<br/>
                <input v-model.number="mpConstantsForm.spp_discount" type="number" step="0.0001" class="form-input" style="width:110px" />
              </label>
              <button class="btn btn-primary btn-sm" :disabled="mpConstantsSaving" @click="addMpConstants">
                {{ mpConstantsSaving ? 'Сохранение…' : 'Добавить' }}
              </button>
            </div>
            <span v-if="mpConstantsSaveStatus" style="font-size:var(--fs-xs);color:var(--text-muted);display:block;margin-top:var(--sp-2)">{{ mpConstantsSaveStatus }}</span>
          </div>
        </div>
        <div style="padding:var(--sp-3) var(--sp-5);border-top:1px solid var(--border);display:flex;justify-content:flex-end;flex-shrink:0">
          <button class="btn btn-ghost btn-sm" @click="showMpConstantsModal = false">Закрыть</button>
        </div>
      </div>
    </div>
    <!-- Plan material prices modal -->
    <div v-if="showPlanPricesModal" class="modal-overlay" @click.self="showPlanPricesModal = false">
      <div class="modal-content" style="max-width:1200px" @click.stop>
        <div class="modal-header">
          <h2>Цены материалов по плану</h2>
          <button class="modal-close" @click="showPlanPricesModal = false">×</button>
        </div>
        <div style="padding:var(--sp-4) var(--sp-5);overflow:auto;flex:1">
          <p class="muted" style="margin-bottom:var(--sp-3)">
            Массовая правка цен материалов по номеру плана целиком, без привязки к модели, артикулу
            и заданию. Только калькуляции с признаком КПСС. Приоритет в расчёте себестоимости:
            версия калькуляции → применённый набор цен → исходные данные.
          </p>

          <div style="display:flex;gap:var(--sp-3);align-items:flex-end;margin-bottom:var(--sp-4)">
            <label style="display:flex;flex-direction:column;gap:4px">
              <span style="font-size:var(--fs-xs);color:var(--text-muted)">Номер плана</span>
              <input v-model="planPricesPlanId" class="editor-input" style="width:160px"
                     placeholder="например 9272" @keyup.enter="loadPlanPrices" />
            </label>
            <button class="btn btn-primary btn-sm" :disabled="!planPricesPlanId || planPricesLoading"
                    @click="loadPlanPrices">Показать</button>
            <span v-if="planPricesStatus" style="font-size:var(--fs-xs);color:var(--text-muted)">{{ planPricesStatus }}</span>
          </div>

          <!-- Ошибка операций показывается ЗДЕСЬ, внутри модалки: баннер
               страницы она перекрывает, и раньше отказ выглядел как
               «кнопка не нажимается». -->
          <div v-if="planPricesError" class="plan-prices-error">
            <span>{{ planPricesError }}</span>
            <button class="cost-error-x" aria-label="Закрыть" @click="planPricesError = ''">×</button>
          </div>

          <div v-if="planPricesLoading" class="muted" style="text-align:center;padding:24px">Загрузка…</div>

          <template v-else-if="planPricesLoaded">
            <!-- Наборы-документы -->
            <h3 style="font-size:var(--fs-sm);margin:0 0 var(--sp-2)">Наборы для плана {{ planPricesPlanId }}</h3>
            <table class="data-table compact" style="width:100%;margin-bottom:var(--sp-4)">
              <thead>
                <tr>
                  <th>Название</th>
                  <th class="col-num">Строк</th>
                  <th class="col-num">Курс</th>
                  <th>Статус</th>
                  <th>Автор</th>
                  <th>Создан</th>
                  <th>Применил</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!planPriceSets.length">
                  <td colspan="8" class="muted" style="text-align:center">
                    Наборов пока нет — все версии исключены из расчёта, действуют исходные данные
                  </td>
                </tr>
                <tr v-for="s in planPriceSets" :key="s.id"
                    :class="{ 'plan-set-applied': s.status === 'applied' }">
                  <td>{{ s.title || '—' }}</td>
                  <td class="col-num num">{{ s.rows_count }}</td>
                  <td class="col-num num">{{ fmtPrice4(s.rate) }}</td>
                  <td>
                    <span v-if="s.status === 'applied'" class="plan-set-badge">● применён к расчёту</span>
                    <span v-else-if="s.status === 'archived'" class="muted">архив</span>
                    <span v-else class="muted">черновик</span>
                  </td>
                  <td>{{ s.created_by }}</td>
                  <td>{{ formatDate(s.created_at) }}</td>
                  <td>{{ s.applied_by || '—' }}</td>
                  <td style="white-space:nowrap">
                    <button class="btn btn-ghost btn-sm" @click="openPlanPriceSet(s.id)">Открыть</button>
                    <button v-if="s.status !== 'applied'" class="btn btn-primary btn-sm"
                            :disabled="planPricesSaving" @click="applyPlanPriceSet(s.id)">Применить</button>
                    <button v-else class="btn btn-ghost btn-sm"
                            :disabled="planPricesSaving" @click="unapplyPlanPriceSet(s.id)">Снять</button>
                    <button v-if="s.status !== 'applied'" class="btn btn-ghost btn-sm"
                            :disabled="planPricesSaving" @click="deletePlanPriceSet(s.id)">Удалить</button>
                  </td>
                </tr>
              </tbody>
            </table>

            <!-- Редактор набора -->
            <div style="display:flex;gap:var(--sp-3);align-items:flex-end;margin-bottom:var(--sp-3)">
              <label style="display:flex;flex-direction:column;gap:4px">
                <span style="font-size:var(--fs-xs);color:var(--text-muted)">Название набора</span>
                <input v-model="planPriceForm.title" class="editor-input" style="width:240px"
                       :disabled="planPriceFormLocked" placeholder="например «пересчёт по курсу 3.2»" />
              </label>
              <label style="display:flex;flex-direction:column;gap:4px">
                <span style="font-size:var(--fs-xs);color:var(--text-muted)">Курс (на весь набор)</span>
                <input v-model.number="planPriceForm.rate" type="number" step="0.0001"
                       class="editor-input col-num" style="width:120px"
                       :disabled="planPriceFormLocked"
                       @input="onPlanRateChange(($event.target as HTMLInputElement).value)" />
              </label>
              <button class="btn btn-primary btn-sm" :disabled="planPricesSaving || planPriceFormLocked"
                      :title="`В набор уйдут только строки с ценой, отличной от источника: ${planPriceOverriddenCount}`"
                      @click="savePlanPriceSet">
                <template v-if="planPricesSaving">Сохранение…</template>
                <template v-else>
                  {{ planPriceForm.set_id ? 'Сохранить набор' : 'Создать набор' }}
                  <template v-if="planPriceOverriddenCount"> ({{ planPriceOverriddenCount }})</template>
                </template>
              </button>
              <button v-if="planPriceForm.set_id" class="btn btn-ghost btn-sm"
                      :disabled="planPricesSaving" @click="resetPlanPriceForm">Новый набор</button>
              <button class="btn btn-ghost btn-sm"
                      :disabled="!planPriceRows.length && !planDecorRows.length"
                      title="Выгрузить материалы и декоры плана с ценами текущего набора"
                      @click="exportPlanPricesToExcel">
                <Icon name="lucide:download" /> Экспорт в Excel
              </button>
            </div>
            <p v-if="planPriceFormLocked" class="muted" style="margin-bottom:var(--sp-3)">
              Набор применён к расчёту — чтобы менять цены, сначала снимите применение.
            </p>

            <!-- table-layout: fixed + colgroup — иначе текстовые колонки
                 растягиваются на всю длину содержимого (у .data-table td стоит
                 white-space: nowrap), и на цены места почти не остаётся. -->
            <table class="data-table compact plan-prices-table">
              <!-- Сумма ровно 100%. Текстовым колонкам отдано меньше, чем они
                   заняли бы сами: длинные наименования переносятся по словам,
                   полный текст доступен в подсказке. -->
              <colgroup>
                <col style="width:21%" />
                <col style="width:11%" />
                <col style="width:17%" />
                <col style="width:5%" />
                <col style="width:11%" />
                <col style="width:12%" />
                <col style="width:12%" />
                <col style="width:11%" />
              </colgroup>
              <thead>
                <tr>
                  <th>Наименование</th>
                  <th>Артикул мат.</th>
                  <th>Свойства</th>
                  <th class="col-num">Строк</th>
                  <th class="col-num">Исх. цена, руб</th>
                  <th class="col-num">Цена, руб</th>
                  <th class="col-num">Цена, $</th>
                  <th>Перекрыто версией</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!planPriceRows.length">
                  <td colspan="8" class="muted" style="text-align:center">
                    Материалов с признаком КПСС в этом плане не найдено
                  </td>
                </tr>
                <tr v-for="(r, ri) in planPriceRows" :key="ri"
                    :class="{ 'plan-row-overridden': r.overridden_rows >= r.rows_count && r.rows_count > 0 }">
                  <!-- title — полный текст во всплывающей подсказке: наименования
                       материалов и свойства бывают длиннее любой разумной колонки. -->
                  <td class="plan-cell-text" :title="r['Наименование'] || ''">{{ r['Наименование'] || '—' }}</td>
                  <td class="plan-cell-text" :title="r['артикул материала'] || ''">{{ r['артикул материала'] || '—' }}</td>
                  <td class="plan-cell-text muted" :title="planRowProps(r)">{{ planRowProps(r) }}</td>
                  <td class="col-num num">{{ r.rows_count }}</td>
                  <td class="col-num num">
                    {{ fmtPrice4(r.source_price_rub) }}
                    <span v-if="r.distinct_prices > 1" class="plan-spread-warn"
                          :title="'Внутри группы было ' + r.distinct_prices + ' разных цен (' + fmtPrice4(r.min_price_rub) + '…' + fmtPrice4(r.max_price_rub) + '). Применение набора поставит одну цену на все строки.'">⚠</span>
                  </td>
                  <td class="col-num">
                    <input v-model.number="r.price_rub" type="number" step="0.0001"
                           class="editor-input col-num" :disabled="planPriceFormLocked"
                           @input="onPlanPriceEdit(r, 'rub')" />
                  </td>
                  <td class="col-num">
                    <input v-model.number="r.price_usd" type="number" step="0.0001"
                           class="editor-input col-num" :disabled="planPriceFormLocked"
                           @input="onPlanPriceEdit(r, 'usd')" />
                  </td>
                  <td>
                    <span v-if="!r.overridden_rows" class="muted">—</span>
                    <span v-else :class="r.overridden_rows >= r.rows_count ? 'plan-ovr-full' : 'plan-ovr-part'">
                      {{ r.overridden_rows }} из {{ r.rows_count }}
                    </span>
                  </td>
                </tr>
              </tbody>
            </table>

            <!-- Декоры. Отдельной таблицей, а не строками в предыдущей: у них
                 нет ни нормы, ни цены материала — стоимость задана суммой
                 напрямую, и колонки «артикул/свойства» к ним неприменимы. -->
            <h3 style="font-size:var(--fs-sm);margin:var(--sp-4) 0 var(--sp-2)">
              Декоры плана
              <span class="muted" style="font-weight:normal;font-size:var(--fs-xs)">
                — цена задаётся суммой; правки уходят в «Декоры, руб.» и «Декоры, USD.»
              </span>
            </h3>
            <table class="data-table compact plan-prices-table">
              <colgroup>
                <col style="width:38%" />
                <col style="width:8%" />
                <col style="width:14%" />
                <col style="width:14%" />
                <col style="width:14%" />
                <col style="width:12%" />
              </colgroup>
              <thead>
                <tr>
                  <th>Наименование декора</th>
                  <th class="col-num">Строк</th>
                  <th class="col-num">Исх. сумма, руб</th>
                  <th class="col-num">Сумма, руб</th>
                  <th class="col-num">Сумма, $</th>
                  <th>Перекрыто версией</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!planDecorRows.length">
                  <td colspan="6" class="muted" style="text-align:center">
                    Декоров с признаком КПСС в этом плане не найдено
                  </td>
                </tr>
                <tr v-for="(r, ri) in planDecorRows" :key="'d' + ri"
                    :class="{ 'plan-row-overridden': r.overridden_rows >= r.rows_count && r.rows_count > 0 }">
                  <td class="plan-cell-text" :title="r['Декоры, наименование'] || ''">{{ r['Декоры, наименование'] || '—' }}</td>
                  <td class="col-num num">{{ r.rows_count }}</td>
                  <td class="col-num num">
                    {{ fmtPrice4(r.source_price_rub) }}
                    <span v-if="r.distinct_prices > 1" class="plan-spread-warn"
                          :title="'Внутри группы было ' + r.distinct_prices + ' разных сумм (' + fmtPrice4(r.min_price_rub) + '…' + fmtPrice4(r.max_price_rub) + '). Применение набора поставит одну на все строки.'">⚠</span>
                  </td>
                  <td class="col-num">
                    <input v-model.number="r.price_rub" type="number" step="0.0001"
                           class="editor-input col-num" :disabled="planPriceFormLocked"
                           @input="onPlanPriceEdit(r, 'rub')" />
                  </td>
                  <td class="col-num">
                    <input v-model.number="r.price_usd" type="number" step="0.0001"
                           class="editor-input col-num" :disabled="planPriceFormLocked"
                           @input="onPlanPriceEdit(r, 'usd')" />
                  </td>
                  <td>
                    <span v-if="!r.overridden_rows" class="muted">—</span>
                    <span v-else :class="r.overridden_rows >= r.rows_count ? 'plan-ovr-full' : 'plan-ovr-part'">
                      {{ r.overridden_rows }} из {{ r.rows_count }}
                    </span>
                  </td>
                </tr>
              </tbody>
            </table>
          </template>
        </div>
        <div style="padding:var(--sp-3) var(--sp-5);border-top:1px solid var(--border);display:flex;justify-content:flex-end;flex-shrink:0">
          <button class="btn btn-ghost btn-sm" @click="showPlanPricesModal = false">Закрыть</button>
        </div>
      </div>
    </div>
    <!-- Approval popup modal -->
    <div v-if="showApprovalModal" class="modal-overlay" @click.self="showApprovalModal = false">
      <div class="modal-content approval-modal-content" @click.stop>
        <div class="modal-header">
          <h2>Согласование цен</h2>
          <button class="modal-close" @click="showApprovalModal = false">×</button>
        </div>
        <div class="approval-modal-body">
          <!-- Filter bar -->
          <div class="approval-filters" v-if="!approvalLoading">
            <div class="approval-fields">
              <div class="af-search">
                <input v-model="approvalQuery" type="text" placeholder="Поиск по модели, артикулу…" @input="onApprovalFilterChangeDelayed" />
              </div>
              <div v-for="fk in approvalFilterKeys" :key="fk.key" class="af-item" :class="{ locked: isApprovalLocked(fk.key) }">
              <CostMultiSelect
                v-model="approvalSelected[fk.key]"
                :options="enrichFilterOptions(fk.key, approvalFilterOptions[fk.key] || [])"
                :placeholder="isApprovalLocked(fk.key) ? '—' : fk.label"
                :disabled="isApprovalLocked(fk.key)"
                @change="onApprovalFilterChange(fk.key)"
              />
              </div>
              <button class="btn btn-ghost btn-xs" @click="resetApprovalFilters" :disabled="approvalFilterBusy">Сбросить</button>
            </div>
            <div v-if="approvalFilterBusy" class="af-busy">Обновление…</div>
          </div>

          <div v-if="approvalLoading" class="muted" style="text-align:center;padding:24px">Загрузка…</div>
          <div v-else-if="!approvalPendingChanges.length" class="muted" style="text-align:center;padding:24px">
            Нет ожидающих согласования изменений
          </div>
          <div class="approval-table-scroll" v-else>
            <table class="data-table compact approval-table">
              <thead>
                <tr>
                  <th><input type="checkbox" :checked="selectedPendingIds.length === approvalPendingChanges.length && approvalPendingChanges.length > 0" @change="toggleSelectAllPending" /></th>
                  <th>Модель</th>
                  <th>Артикул</th>
                  <th>План</th>
                  <th>Уровень цен</th>
                  <th class="col-num">Розн., руб</th>
                  <th class="col-num">Опт., руб</th>
                  <th class="col-num">Цена РФ</th>
                  <th class="col-num">Цена КЗ</th>
                  <th class="col-num">Цена УЗ</th>
                  <th class="col-num">Цена для МП, рос. руб.</th>
                  <th>Комментарий</th>
                  <th class="col-num">Рентабельность, руб</th>
                  <th class="col-num">Рентабельность, %</th>
                  <th class="col-num">Маржа, %</th>
                  <th class="col-num">Откл. %</th>
                  <th>Признак калькуляции</th>
                  <th>Автор</th>
                  <th>Дата</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="pc in approvalPendingChanges" :key="pc.id" :class="modalApprovalRowClass(pc)">
                  <td><input type="checkbox" :value="pc.id" v-model="selectedPendingIds" /></td>
                  <td>{{ pc['Модель'] || pc.model || '—' }}</td>
                  <td>{{ pc['Артикул'] || pc.articul || '—' }}</td>
                  <td>{{ pc['PLAN_ID'] || '—' }}</td>
                  <td>{{ pc['Уровень цен'] || pc.price_level || '—' }}</td>
                  <td class="col-num num">{{ fmt(pc['Розничная цена по уровню, руб.'] || pc.retail_rub) }}</td>
                  <td class="col-num num">{{ fmt(pc['Отпускная цена по уровню, руб'] || pc.wholesale_rub) }}</td>
                  <td class="col-num num">{{ fmt(pc['Цена РФ']) }}</td>
                  <td class="col-num num">{{ fmt(pc['Цена КЗ']) }}</td>
                  <td class="col-num num">{{ fmt(pc['Цена УЗ']) }}</td>
                  <td class="col-num num">{{ fmt(pc['mp_price_rub']) }}</td>
                  <td>{{ pc['Комментарий'] || '—' }}</td>
                  <td class="col-num num">{{ fmt(calcApprovalModal(pc).markupRub) }}</td>
                  <td class="col-num num">{{ calcApprovalModal(pc).markupPct.toFixed(1) }}%</td>
                  <td class="col-num num">{{ calcApprovalModal(pc).marginPct.toFixed(1) }}%</td>
                  <td class="col-num num">{{ modalMarginDevText(pc) }}</td>
                  <td>{{ pc['Признак калькуляции'] || pc.calc_sign || '—' }}</td>
                  <td>{{ pc['username'] || pc.author || '—' }}</td>
                  <td>{{ formatDate(pc['created_at'] || pc.created_at) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <div class="approval-modal-footer">
          <span style="font-size:var(--fs-xs);color:var(--text-muted)">
            Выбрано: {{ selectedPendingIds.length }} / {{ approvalPendingChanges.length }}
          </span>
          <div style="display:flex;gap:var(--sp-3)">
            <button class="btn btn-ghost btn-sm" @click="showApprovalModal = false">Закрыть</button>
            <button class="btn btn-ghost btn-sm" :disabled="!approvalPendingChanges.length || approvalClearing" @click="clearAllPendingChanges">
              <Icon name="lucide:trash-2" /> {{ approvalClearing ? 'Очистка…' : 'Очистить таблицу' }}
            </button>
            <button class="btn btn-ghost btn-sm" :disabled="!selectedPendingIds.length || approvalRejecting" @click="rejectPendingChanges">
              {{ approvalRejecting ? 'Отклонение…' : 'Отклонить выбранные' }}
            </button>
            <button class="btn btn-primary btn-sm" :disabled="!selectedPendingIds.length || approvalApplying" @click="applyPendingChanges">
              {{ approvalApplying ? 'Установка…' : `Установить цены (${selectedPendingIds.length})` }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Version editor modal -->
    <Teleport to="body">
      <div v-if="editingVersion" class="modal-overlay modal-overlay--solid" @click.self="closeVersionEditor">
        <div class="modal-content modal-wide" @click.stop>
          <div class="modal-header">
            <h2>Редактирование расчёта: {{ editingVersion.model }} / {{ editingVersion.articul }}</h2>
            <span v-if="editingVersion._locked" class="version-status-badge" style="background:#fef3c7;color:#b45309;">🔒 Заблокировано</span>
            <div class="modal-header-actions">
              <button class="modal-close" @click="closeVersionEditor">×</button>
            </div>
          </div>
          <div class="version-selector-bar">
            <label>Версия: </label>
            <select class="version-select editor-select"
                    :disabled="editingVersion.isEditing || loadingVersionData"
                    :value="editingVersion.selectedVersionId ?? '__raw__'"
                    @change="onVersionSelectChange($event)">
              <option value="__raw__">Исходные данные</option>
              <option v-for="v in editingVersion.versions" :key="v.id" :value="v.id">
                Версия {{ v.version }} · {{ v.status }}<template v-if="v.created_at"> · {{ v.created_at.slice(0, 16) }}</template>
              </option>
            </select>
            <span v-if="currentVersionStatus" class="version-status-badge">{{ currentVersionStatus }}</span>
          </div>
          <div class="version-editor-toolbar">
            <button v-if="!editingVersion.isEditing" class="btn btn-sm btn-primary" :disabled="loadingVersionData || editingVersion._locked" @click="startEditing">✏️ Редактировать</button>
            <template v-else>
              <button class="btn btn-sm" @click="addVersionRow">+ Добавить строку</button>
              <button class="btn btn-sm btn-danger" @click="deleteSelectedRows">✕ Удалить</button>
              <span class="spacer"></span>
              <button class="btn btn-sm" :disabled="savingDraft || editingVersion._locked" @click="saveDraft">{{ savingDraft ? 'Сохранение…' : '💾 Сохранить' }}</button>
              <button class="btn btn-sm btn-primary" :disabled="submittingDraft || editingVersion._locked" @click="submitDraft">{{ submittingDraft ? 'Отправка…' : '📨 Отправить на утверждение' }}</button>
              <button class="btn btn-sm btn-ghost" @click="cancelEditing">Отмена</button>
            </template>
          </div>
          <!-- Явно говорим, что с чем сравниваем: иначе отсутствие колонок
               прошлых этапов не отличить от «функция не работает». -->
          <div class="stage-note">
            <template v-if="stagePricesLoading">Цены предыдущих этапов: загрузка…</template>
            <template v-else-if="stagePrices.length">
              Цены предыдущих этапов:
              <span v-for="(sp, si) in stagePrices" :key="'n-' + sp.stage">
                <template v-if="si"> · </template>
                <b>{{ sp.stage }}</b> — {{ sp.rows.length }} материал(ов),
                {{ sp.scope === 'model+articul' ? 'по модели и артикулу' : 'по модели, артикулу, плану и заданию' }}
              </span>
            </template>
            <template v-else-if="editingVersion.calc_sign === 'ПКПСС'">
              ПКПСС — первый этап калькулирования, сравнивать не с чем.
            </template>
            <template v-else>
              Расчётов на предыдущих этапах для этой модели и артикула не найдено — сравнивать не с чем.
            </template>
          </div>
          <div class="version-editor-table-wrap">
            <table class="version-editor-table">
              <thead>
                <tr>
                  <th v-if="editingVersion.isEditing" class="col-chk"><input type="checkbox" @change="(e: any) => editingVersion?.rows.forEach(r => r._selected = (e.target as HTMLInputElement).checked)" /></th>
                  <th>Материал/операция</th>
                  <th>Наименование</th>
                  <th>Артикул материала</th>
                  <th>Свойство</th>
                  <!-- Цены предыдущих этапов калькулирования: слева от текущих,
                       только для чтения. Ключ сопоставления — материал
                       (наименование + артикул + три свойства). -->
                  <th v-for="sp in stagePrices" :key="'h-' + sp.stage" class="col-num stage-col"
                      :title="stageColHint(sp)">Цена {{ sp.stage }}, руб.</th>
                  <th class="col-num">Норма</th>
                  <th class="col-num">Цена, руб.</th>
                  <th class="col-num">Цена, USD</th>
                  <th class="col-num">Курс</th>
                  <th class="col-num">Сумма, руб.</th>
                  <th class="col-num">Сумма, USD</th>
                  <th>Комментарий</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(vr, vi) in editingVersion.rows" :key="vi"
                  :class="{ 'row-added': vr.change_type === 'added', 'row-modified': vr.change_type === 'modified', 'row-zero-cost': isZeroCostRow(vr) }">
                  <td v-if="editingVersion.isEditing"><input type="checkbox" v-model="vr._selected" /></td>
                  <td>
                    <select v-if="editingVersion.isEditing && editingTypeCell === vi" :value="vr['Материал/операция/декор(призн)']" class="editor-select" autofocus @change="onVersionRowEdit(vr, $event, 'Материал/операция/декор(призн)')" @blur="editingTypeCell = -1">
                      <option value="Материал основной">Материал основной</option>
                      <option value="Материал вспомогательный">Материал вспомогательный</option>
                      <option value="Декор">Декор</option>
                      <option value="Пошив">Пошив</option>
                      <option value="Раскрой">Раскрой</option>
                      <option value="Вязание">Вязание</option>
                    </select>
                    <span v-else-if="editingVersion.isEditing" class="type-tag clickable" @click="editingTypeCell = vi">{{ typeDisplayValue(vr) || '—' }}</span>
                    <span v-else>{{ typeDisplayValue(vr) }}</span>
                  </td>
                  <td><input v-if="editingVersion.isEditing" :value="nameDisplayValue(vr)" class="editor-input" @input="onVersionRowEdit(vr, $event, 'Наименование')" /><span v-else>{{ nameDisplayValue(vr) || '—' }}</span></td>
                  <td><input v-if="editingVersion.isEditing" :value="vr['артикул материала']" class="editor-input" @input="onVersionRowEdit(vr, $event, 'артикул материала')" /><span v-else>{{ vr['артикул материала'] }}</span></td>
                  <td>
                    <input
                      v-if="editingVersion.isEditing"
                      :value="vr._propRaw"
                      class="editor-input"
                      placeholder="свойства через запятую"
                      title="До трёх свойств через запятую — пишутся в свойство1/2/3"
                      @input="onVersionPropertyEdit(vr, $event)"
                    />
                    <span v-else>{{ vr['Свойство'] }}</span>
                  </td>
                  <td v-for="sp in stagePrices" :key="'c-' + sp.stage" class="col-num num stage-col">
                    <template v-if="stagePriceOf(sp, vr) !== null">
                      {{ fmtPrice4(stagePriceOf(sp, vr)) }}
                      <span v-if="stagePriceDelta(sp, vr) !== null" class="stage-delta"
                            :class="stagePriceDelta(sp, vr)! >= 0 ? 'delta-pos' : 'delta-neg'"
                            :title="'Отличие текущей цены от ' + sp.stage">
                        {{ (stagePriceDelta(sp, vr)! >= 0 ? '+' : '') + stagePriceDelta(sp, vr)!.toFixed(1) + '%' }}
                      </span>
                    </template>
                    <span v-else class="muted" title="На этом этапе такого материала не было">—</span>
                  </td>
                  <!-- У декоров нормы и цены материала в источнике нет: их стоимость
                       задаётся суммой в колонках «Сумма» ниже. Поля скрыты намеренно —
                       если их заполнить, произведение затрёт сумму декора. -->
                  <td class="col-num"><span v-if="isDecorRow(vr)" class="muted" title="У декора нет нормы — стоимость задаётся суммой">—</span><input v-else-if="editingVersion.isEditing" :value="vr['Норма']" type="number" step="0.000001" class="editor-input col-num" @input="onVersionRowEdit(vr, $event, 'Норма')" /><span v-else>{{ fmtNorm(vr['Норма']) }}</span></td>
                  <td class="col-num"><span v-if="isDecorRow(vr)" class="muted">—</span><input v-else-if="editingVersion.isEditing" :value="vr['цена материала, руб.']" type="number" step="0.0001" class="editor-input col-num" @input="onVersionRowEdit(vr, $event, 'цена материала, руб.')" /><span v-else>{{ fmtPrice4(vr['цена материала, руб.']) }}</span></td>
                  <td class="col-num"><span v-if="isDecorRow(vr)" class="muted">—</span><input v-else-if="editingVersion.isEditing" :value="vr['цена материала, USD.']" type="number" step="0.0001" class="editor-input col-num" @input="onVersionRowEdit(vr, $event, 'цена материала, USD.')" /><span v-else>{{ fmtPrice4(vr['цена материала, USD.']) }}</span></td>
                  <td class="col-num"><input v-if="editingVersion.isEditing" :value="vr['Курс на дату расчета']" type="number" step="0.0001" class="editor-input col-num" @input="onVersionRowEdit(vr, $event, 'Курс на дату расчета')" /><span v-else>{{ vr['Курс на дату расчета'] }}</span></td>
                  <!-- Для декора сумма редактируется напрямую, для материала считается. -->
                  <td class="col-num"><input v-if="isDecorRow(vr) && editingVersion.isEditing" :value="vr['Декоры, руб.']" type="number" step="0.0001" class="editor-input col-num" title="Стоимость декора — задаётся суммой" @input="onVersionRowEdit(vr, $event, 'Декоры, руб.')" /><span v-else>{{ fmtPrice4(versionRowSum(vr, 'руб.')) }}</span></td>
                  <td class="col-num"><input v-if="isDecorRow(vr) && editingVersion.isEditing" :value="vr['Декоры, USD.']" type="number" step="0.0001" class="editor-input col-num" @input="onVersionRowEdit(vr, $event, 'Декоры, USD.')" /><span v-else>{{ fmtPrice4(versionRowSum(vr, 'USD.')) }}</span></td>
                  <td><input v-if="editingVersion.isEditing" :value="vr.row_comment" class="editor-input" placeholder="..." @input="onVersionRowEdit(vr, $event, 'row_comment')" /><span v-else>{{ vr.row_comment }}</span></td>
                </tr>
              </tbody>
            </table>

            <!-- Материалы, которые были на предыдущем этапе, но в текущем
                 расчёте отсутствуют: в таблицу их не поставить (там строки
                 текущего расчёта), а знать о них нужно. -->
            <div v-if="stageOrphans.length" class="stage-orphans">
              <div v-for="so in stageOrphans" :key="'o-' + so.stage" class="stage-orphan-block">
                <div class="stage-orphan-head">
                  На этапе {{ so.stage }} было ещё {{ so.rows.length }} материал(ов), которых нет в этом расчёте
                </div>
                <table class="version-editor-table stage-orphan-table">
                  <thead>
                    <tr>
                      <th>Наименование</th>
                      <th>Артикул материала</th>
                      <th>Свойство</th>
                      <th class="col-num">Цена, руб.</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="(r, ri) in so.rows" :key="ri">
                      <td>{{ r['Наименование'] || '—' }}</td>
                      <td>{{ r['артикул материала'] || '—' }}</td>
                      <td class="muted">{{ [r['свойство1'], r['свойство2'], r['свойство3']].filter(Boolean).join(' / ') || '—' }}</td>
                      <td class="col-num num">{{ r.price_rub !== null ? fmtPrice4(r.price_rub) : '—' }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- PEO approval popup modal -->
    <!-- Возврат калькуляции на корректировку после записи в DWH.
         Только админ раздела: операция нежелательная, требует причины и не
         откатывает прейскурант в учётной системе — новая установка цен создаст
         новый прейскурант, он перекроет прежний. -->
    <Teleport to="body">
      <div v-if="reopenTarget" class="modal-overlay" @click.self="closeReopenModal">
        <div class="modal approval-modal">
          <div class="modal-header">
            <span>{{ reopenTarget._reopened ? 'Отозвать разрешение на правку' : 'Вернуть на корректировку' }}</span>
            <span class="modal-subtitle">{{ reopenTarget['Модель'] || '—' }} / {{ reopenTarget['Артикул'] || '—' }}</span>
            <button class="modal-close" @click="closeReopenModal">✕</button>
          </div>
          <div class="approval-body">
            <div class="approval-info-row">
              <span class="approval-label">Калькуляция:</span>
              <span>{{ reopenTarget['Признак калькуляции'] || '—' }}, план {{ reopenTarget['PLAN_ID'] || '—' }}</span>
            </div>

            <template v-if="reopenTarget._reopened">
              <p class="reopen-note">
                Калькуляция открыта на исправление. Отзыв вернёт блокировку —
                записи в DWH при этом не меняются.
              </p>
              <div class="approval-actions">
                <button class="btn btn-sm btn-ghost" :disabled="reopenBusy" @click="closeReopenModal">Отмена</button>
                <button class="btn btn-sm btn-danger" :disabled="reopenBusy" @click="submitRevokeReopen">
                  {{ reopenBusy ? 'Отзыв…' : 'Отозвать' }}
                </button>
              </div>
            </template>

            <template v-else>
              <p class="reopen-note reopen-note--warn">
                Цены этой калькуляции уже переданы в учётную систему. Отменить их
                нельзя: правка создаст <b>новый прейскурант</b>, который перекроет
                прежний. Прошлые значения останутся в истории цен.
              </p>
              <div class="approval-comment-row">
                <label>Причина (обязательно):</label>
                <textarea v-model="reopenReason" class="approval-comment" rows="2"
                  placeholder="Например: ошибка в цене материала, пересчёт по требованию ПЭО"></textarea>
              </div>
              <div v-if="reopenError" class="reopen-error">{{ reopenError }}</div>
              <div class="approval-actions">
                <button class="btn btn-sm btn-ghost" :disabled="reopenBusy" @click="closeReopenModal">Отмена</button>
                <button class="btn btn-sm btn-primary" :disabled="reopenBusy || reopenReason.trim().length < 5"
                  @click="submitReopen">
                  {{ reopenBusy ? 'Открытие…' : 'Вернуть на корректировку' }}
                </button>
              </div>
            </template>
          </div>
        </div>
      </div>
    </Teleport>

    <Teleport to="body">
      <div v-if="approvalTarget" class="modal-overlay" @click.self="closeApprovalPopup">
        <div class="modal approval-modal">
          <div class="modal-header">
            <span>Согласование расчёта</span>
            <span class="modal-subtitle">{{ approvalTarget['Модель'] || '—' }} / {{ approvalTarget['Артикул'] || '—' }}</span>
            <button class="modal-close" @click="closeApprovalPopup">✕</button>
          </div>
          <div class="approval-body">
            <div class="approval-status-row">
              <span class="approval-label">Статус ПЭО:</span>
              <span v-if="approvalTarget.peo_status === 'approved'" class="peo-badge peo-approved">🟢 Согласовано</span>
              <span v-else-if="approvalTarget.peo_status === 'rejected'" class="peo-badge peo-rejected">🔴 Отклонено</span>
              <span v-else-if="approvalTarget.peo_status === 'returned'" class="peo-badge peo-returned">🟠 Возврат на корректировку</span>
              <span v-else class="peo-badge peo-none">⚪ Нет статуса</span>
            </div>
            <div v-if="approvalTarget.peo_approved_by" class="approval-info-row">
              <span class="approval-label">Кто:</span>
              <span>{{ approvalTarget.peo_approved_by }}</span>
            </div>
            <div class="approval-actions">
              <template v-if="approvalTarget.peo_status === 'approved' && (can('cost:approve') || can('cost:peo_mark'))">
                <button class="btn btn-sm btn-ghost" :disabled="approving" @click="revokeApproval(approvalTarget)">↩ Снять согласование</button>
              </template>
              <template v-else>
                <button class="btn btn-sm btn-primary" :disabled="approving" @click="setApproval('approved')">✓ Согласовать</button>
                <button class="btn btn-sm btn-danger" :disabled="approving" @click="setApproval('rejected')">✗ Отклонить</button>
              </template>
            </div>
            <div v-if="approvalTarget.peo_status === 'rejected' || approvalTarget.peo_status === 'pending' || !approvalTarget.peo_status" class="approval-comment-row">
              <label>Комментарий:</label>
              <textarea v-model="approvalComment" class="approval-comment" rows="2" placeholder="Причина отклонения…"></textarea>
            </div>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Column visibility settings modal -->
    <Teleport to="body">
      <div v-if="showColumnSettings" class="modal-overlay" @click.self="cancelColumnVisibility">
        <div class="modal-content colvis-modal" @click.stop>
          <div class="modal-header">
            <h2>Настройка колонок</h2>
            <button class="modal-close" @click="cancelColumnVisibility">×</button>
          </div>
          <div class="colvis-body">
            <div class="colvis-group">
              <div class="colvis-group-title">Основные</div>
              <label v-for="c in mainColumns" :key="c.key" class="colvis-item">
                <input type="checkbox" v-model="pendingVisibility[c.key]" />
                <span>{{ c.label }}</span>
              </label>
            </div>
            <div class="colvis-group">
              <div class="colvis-group-title">Информационные колонки</div>
              <label v-for="c in infoColumns" :key="c.key" class="colvis-item">
                <input type="checkbox" v-model="pendingVisibility[c.key]" />
                <span>{{ c.label }}</span>
              </label>
            </div>
            <div class="colvis-group">
              <div class="colvis-group-title">Цены (рубли)</div>
              <label v-for="c in rubColumns" :key="c.key" class="colvis-item">
                <input type="checkbox" v-model="pendingVisibility[c.key]" />
                <span>{{ c.label }}</span>
              </label>
            </div>
            <div class="colvis-group">
              <div class="colvis-group-title">Цены (доллары)</div>
              <label v-for="c in usdColumns" :key="c.key" class="colvis-item">
                <input type="checkbox" v-model="pendingVisibility[c.key]" />
                <span>{{ c.label }}</span>
              </label>
            </div>
            <div class="colvis-group">
              <div class="colvis-group-title">Затраты</div>
              <label v-for="c in costColumns" :key="c.key" class="colvis-item">
                <input type="checkbox" v-model="pendingVisibility[c.key]" />
                <span>{{ c.label }}</span>
              </label>
            </div>
            <div class="colvis-group">
              <div class="colvis-group-title">Расчётные колонки</div>
              <label v-for="c in calcColumns" :key="c.key" class="colvis-item">
                <input type="checkbox" v-model="pendingVisibility[c.key]" />
                <span>{{ c.label }}</span>
              </label>
            </div>
          </div>
          <div class="colvis-footer">
            <div class="colvis-footer-actions">
              <button class="btn btn-ghost btn-xs" @click="selectAllColumns">Все</button>
              <button class="btn btn-ghost btn-xs" @click="deselectAllColumns">Снять все</button>
              <button class="btn btn-ghost btn-xs" @click="resetColumnVisibility">Сбросить</button>
            </div>
            <div class="colvis-footer-buttons">
              <button class="btn btn-ghost btn-sm" @click="cancelColumnVisibility">Отмена</button>
              <button class="btn btn-primary btn-sm" @click="applyColumnVisibility">Применить</button>
            </div>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from "vue";
import { useCostPermission } from "~/composables/useCostPermission";

interface PriceLevel { name: string; price_type1: number; price_type3: number; price_type4: number; price_type5: number; price_type6: number }
interface FilterOption { id: string; text: string }
type FilterKey =
  | "brand_manager" | "level01" | "level02" | "level03" | "level04" | "level05"
  | "calc_sign" | "plan_id";

const filterConfig: { key: FilterKey; label: string }[] = [
  { key: "brand_manager", label: "Бренд-менеджер" },
  { key: "level01", label: "Level 01" },
  { key: "level02", label: "Level 02" },
  { key: "level03", label: "Level 03" },
  { key: "level04", label: "Level 04" },
  { key: "level05", label: "Level 05" },
  { key: "calc_sign", label: "Признак калькуляции" },
  { key: "plan_id", label: "План" },
];

const LEVEL_KEYS = ["level01", "level02", "level03", "level04", "level05"];

/** Расшифровки признаков калькуляции */
const CALC_SIGN_DESCRIPTIONS: Record<string, string> = {
  'ПКПСС': 'новая разработка',
  'КПСС': 'плановая калькуляция',
  'ПФКСС': 'фактическая расценка ассортимента',
  'ФКСС': 'история себестоимости',
};

/** Обогатить плоский список опций расшифровками для calc_sign */
function enrichFilterOptions<T>(key: string, raw: T[]): T[] {
  if (key !== 'calc_sign' && key !== 'col_calc_sign') return raw;
  return raw.map((v) => {
    const val = String(v);
    return { value: val, label: val, description: CALC_SIGN_DESCRIPTIONS[val] } as any;
  });
}

const config = useRuntimeConfig();
const apiBase = computed(() =>
  config.public.costOnly ? "" : ((config.public.apiBase as string) || "")
);
const apiHostLabel = computed(() => apiBase.value || "локального API");

const { can, roles, loading: permLoading } = useCostPermission();
const user = useState<any>("auth-user");

const pageTitle = computed(() => {
  const base = 'Установка цен';
  const signs = selected.calc_sign as string[] | undefined;
  if (signs && signs.length > 0) {
    const descs = signs.map(s => CALC_SIGN_DESCRIPTIONS[s]).filter(Boolean);
    if (descs.length > 0) {
      return `${base} ${descs.join(', ')}`;
    }
  }
  return base;
});

const pageSubtitle = computed(() => {
  return `Cost History · агрегаты с ${apiHostLabel.value}`;
});

const mockMode = ref(false);
const showUSD = ref(true);
const username = 'system';

// ── Column visibility ───────────────────────────────────────────────────────

interface ColumnDef { key: string; label: string }

const COLUMNS_CONFIG: ColumnDef[] = [
  { key: 'bm', label: 'Бренд-менеджер' },
  { key: 'model', label: 'Модель' },
  { key: 'articul', label: 'Артикул' },
  { key: 'model_name', label: 'Наименование модели' },
  { key: 'color', label: 'Цвет' },
  { key: 'task_num', label: '№ задания' },
  { key: 'plan_id', label: 'PLAN_ID' },
  { key: 'country', label: 'Страна' },
  { key: 'family', label: 'Семья' },
  { key: 'season', label: 'Сезон' },
  { key: 'date', label: 'Дата' },
  { key: 'calc_sign', label: 'Пр.кальк' },
  { key: 'planned_retail', label: 'План. розница' },
  { key: 'planned_wholesale', label: 'План. опт' },
  { key: 'planned_cost', label: 'План. с/с' },
  { key: 'planned_profitability', label: 'План. рентабельность (%)' },
  { key: 'avg_retail_rub', label: 'Сред. розница (руб)' },
  { key: 'avg_rate', label: 'Курс (руб)' },
  { key: 'retail_markup', label: 'Розничная наценка' },
  { key: 'price_rf', label: 'Цена РФ' },
  { key: 'price_kz', label: 'Цена КЗ' },
  { key: 'price_uz', label: 'Цена УЗ' },
  { key: 'mp_price_rub', label: 'Цена для МП, рос. руб.' },
  { key: 'comment', label: 'Комментарий' },
  { key: 'avg_wholesale', label: 'Сред. опт' },
  { key: 'price_level', label: 'Уровень цен' },
  { key: 'avg_retail_usd', label: 'Сред. розница ($)' },
  { key: 'sum_materials', label: 'Осн. материалы' },
  { key: 'sum_aux_materials', label: 'Вспом. материалы' },
  { key: 'avg_sewing_min', label: 'Пошив (мин)' },
  { key: 'sum_sewing', label: 'Пошив' },
  { key: 'avg_cutting_min', label: 'Раскрой (мин)' },
  { key: 'sum_cutting', label: 'Раскрой' },
  { key: 'sum_decors', label: 'Декоры' },
  { key: 'sum_knitting', label: 'Вязание' },
  { key: 'sum_cost', label: 'Себестоимость' },
  { key: 'calc_markup', label: 'Рентабельность' },
  { key: 'calc_markup_pct', label: 'Рентабельность (%)' },
  { key: 'calc_margin_pct', label: 'Маржа (%)' },
  { key: 'calc_margin_deviation', label: 'Откл. маржи (%)' },
  { key: 'peo', label: 'ПЭО' },
];

// Column groupings for the settings modal
const mainColumnKeys = ['bm','model','articul','model_name','color','task_num','plan_id'];
const infoColumnKeys = ['country','family','season','date','calc_sign','planned_retail','planned_wholesale','planned_cost','planned_profitability','avg_retail_rub','avg_rate','retail_markup','price_rf','price_kz','price_uz','mp_price_rub','comment'];
const rubColumnKeys = ['avg_wholesale','price_level'];
const usdColumnKeys = ['avg_retail_usd','sum_materials','sum_aux_materials'];
const costColumnKeys = ['avg_sewing_min','sum_sewing','avg_cutting_min','sum_cutting','sum_decors','sum_knitting','sum_cost'];
const calcColumnKeys = ['calc_markup','calc_markup_pct','calc_margin_pct','calc_margin_deviation','peo'];

const mainColumns = computed(() => COLUMNS_CONFIG.filter(c => mainColumnKeys.includes(c.key)));
const infoColumns = computed(() => COLUMNS_CONFIG.filter(c => infoColumnKeys.includes(c.key)));
const rubColumns = computed(() => COLUMNS_CONFIG.filter(c => rubColumnKeys.includes(c.key)));
const usdColumns = computed(() => COLUMNS_CONFIG.filter(c => usdColumnKeys.includes(c.key)));
const costColumns = computed(() => COLUMNS_CONFIG.filter(c => costColumnKeys.includes(c.key)));
const calcColumns = computed(() => COLUMNS_CONFIG.filter(c => calcColumnKeys.includes(c.key)));

const STICKY_COL_KEYS = ['peo_sel','actions','raw_rows','bm','model','articul','model_name','color','task_num','plan_id'];
const STICKY_DEFAULT_WIDTHS: Record<string, number> = { peo_sel: 34, actions: 110, raw_rows: 40, bm: 160, model: 110, articul: 90, model_name: 200, color: 120, task_num: 120, plan_id: 90 };
const STICKY_MIN_WIDTHS: Record<string, number> = { peo_sel: 30, actions: 70, raw_rows: 32, bm: 80, model: 70, articul: 60, model_name: 90, color: 60, task_num: 60, plan_id: 50 };
const STICKY_MAX_WIDTH = 600;
const WIDTH_STORAGE_KEY = 'cost_sticky_col_widths';
const STORAGE_KEY = 'cost_column_visibility';

function loadColumnVisibility(): Record<string, boolean> {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored) {
    try {
      const parsed = JSON.parse(stored);
      const valid: Record<string, boolean> = {};
      for (const c of COLUMNS_CONFIG) {
        valid[c.key] = parsed[c.key] !== false;
      }
      return valid;
    } catch { /* fall through */ }
  }
  const defaults: Record<string, boolean> = {};
  for (const c of COLUMNS_CONFIG) defaults[c.key] = true;
  return defaults;
}

const columnVisibility = reactive<Record<string, boolean>>(loadColumnVisibility());
const showColumnSettings = ref(false);
const pendingVisibility = ref<Record<string, boolean>>({});

function isVisible(key: string): boolean {
  // Колонка выбора для массового согласования — не в COLUMNS_CONFIG: её нельзя
  // скрыть настройками, она есть ровно у тех, кто вправе ставить статус ПЭО.
  if (key === 'peo_sel') return peoBulkEnabled.value;
  return columnVisibility[key] !== false;
}

function openColumnSettings() {
  pendingVisibility.value = { ...columnVisibility };
  showColumnSettings.value = true;
}

function applyColumnVisibility() {
  Object.assign(columnVisibility, pendingVisibility.value);
  localStorage.setItem(STORAGE_KEY, JSON.stringify(columnVisibility));
  showColumnSettings.value = false;
}

function cancelColumnVisibility() {
  showColumnSettings.value = false;
}

function resetColumnVisibility() {
  for (const c of COLUMNS_CONFIG) pendingVisibility.value[c.key] = true;
}

function selectAllColumns() {
  for (const c of COLUMNS_CONFIG) pendingVisibility.value[c.key] = true;
}

function deselectAllColumns() {
  for (const c of COLUMNS_CONFIG) pendingVisibility.value[c.key] = false;
}

const visibleColumnCount = computed(() => {
  let count = 2; // actions + raw_rows (always visible)
  if (peoBulkEnabled.value) count++; // колонка выбора для массового согласования
  for (const c of COLUMNS_CONFIG) {
    if (columnVisibility[c.key]) count++;
  }
  return count;
});

function clampStickyWidth(key: string, w: number): number {
  return Math.max(STICKY_MIN_WIDTHS[key] ?? 40, Math.min(STICKY_MAX_WIDTH, Math.round(w)));
}
function loadStickyWidths(): Record<string, number> {
  const out = { ...STICKY_DEFAULT_WIDTHS };
  try {
    const parsed = JSON.parse(localStorage.getItem(WIDTH_STORAGE_KEY) || '{}');
    for (const k of STICKY_COL_KEYS) {
      const w = Number(parsed[k]);
      if (Number.isFinite(w) && w > 0) out[k] = clampStickyWidth(k, w);
    }
  } catch { /* defaults */ }
  return out;
}
const stickyWidths = reactive<Record<string, number>>(loadStickyWidths());

/** left = сумма ширин ВИДИМЫХ закреплённых колонок до этой (по порядку STICKY_COL_KEYS) */
function stickyStyle(key: string): Record<string, string> {
  let left = 0;
  for (const k of STICKY_COL_KEYS) {
    if (k === key) break;
    if (isVisible(k)) left += stickyWidths[k];
  }
  const w = stickyWidths[key];
  return { left: left + 'px', width: w + 'px', minWidth: w + 'px' };
}
/** последняя ВИДИМАЯ закреплённая колонка получает тень-разделитель */
function isLastSticky(key: string): boolean {
  const i = STICKY_COL_KEYS.indexOf(key);
  for (let j = i + 1; j < STICKY_COL_KEYS.length; j++) {
    if (isVisible(STICKY_COL_KEYS[j])) return false;
  }
  return true;
}
function stickyClasses(key: string): (string | Record<string, boolean>)[] {
  return ['sticky-col', { 'is-last-sticky': isLastSticky(key) }];
}

let resizing: { key: string; startX: number; startW: number } | null = null;
function startColResize(key: string, e: MouseEvent) {
  resizing = { key, startX: e.clientX, startW: stickyWidths[key] };
  document.body.style.userSelect = 'none';
  document.body.style.cursor = 'col-resize';
  window.addEventListener('mousemove', onColResizeMove);
  window.addEventListener('mouseup', stopColResize);
}
function onColResizeMove(e: MouseEvent) {
  if (!resizing) return;
  stickyWidths[resizing.key] = clampStickyWidth(resizing.key, resizing.startW + (e.clientX - resizing.startX));
}
function stopColResize() {
  if (!resizing) return;
  resizing = null;
  document.body.style.userSelect = '';
  document.body.style.cursor = '';
  window.removeEventListener('mousemove', onColResizeMove);
  window.removeEventListener('mouseup', stopColResize);
  try { localStorage.setItem(WIDTH_STORAGE_KEY, JSON.stringify(stickyWidths)); } catch { /* ignore */ }
}
function resetColWidth(key: string) {
  stickyWidths[key] = STICKY_DEFAULT_WIDTHS[key];
  try { localStorage.setItem(WIDTH_STORAGE_KEY, JSON.stringify(stickyWidths)); } catch { /* ignore */ }
}

// ── Margin targets state ─────────────────────────────────────────────────────

const showMarginModal = ref(false);
const marginTargetsLoading = ref(false);
const marginSaving = ref(false);
const marginSaveStatus = ref('');
const marginTargetsList = ref<{ level1: string; target_margin_pct: number | null }[]>([]);

type MpFormulaInputs = { internal_rate: number; markup_mp: number; expense_pct_mp: number; spp_discount: number };
const mpFormulaInputs = ref<MpFormulaInputs | null>(null);

/** Цена для МП, рос. руб. — та же формула, что и на бэкенде (см. db.compute_mp_price),
 * пересчитывается на клиенте при ручном выборе цены/наценки — тогда
 * "avg_Отпускная цена по уровню, руб" меняется ещё до сохранения и бэкенд
 * этого не видит. */
function computeMpPriceJs(row: any): number | null {
  const f = mpFormulaInputs.value;
  const wholesale = Number(row['avg_Отпускная цена по уровню, руб']);
  const ruNds = row['mp_ru_nds'];
  if (!f || !wholesale || ruNds == null || !f.internal_rate || !f.spp_discount) return null;
  const ndsMultiplier = 1 + Number(ruNds) / 100;
  return (wholesale / f.internal_rate) * f.markup_mp * f.expense_pct_mp * ndsMultiplier / f.spp_discount;
}

const showMpConstantsModal = ref(false);
const mpConstantsLoading = ref(false);
const mpConstantsSaving = ref(false);
const mpConstantsSaveStatus = ref('');
const mpConstantsList = ref<{ id: number; effective_date: string; markup_mp: number; expense_pct_mp: number; spp_discount: number; created_by: string }[]>([]);
const mpConstantsForm = ref<{ effective_date: string; markup_mp: number | null; expense_pct_mp: number | null; spp_discount: number | null }>({
  effective_date: '',
  markup_mp: null,
  expense_pct_mp: null,
  spp_discount: null,
});

// ── Filter state ────────────────────────────────────────────────────────────

const filterOptions = ref<Record<string, any[]>>({} as any);
const selected = reactive<Record<string, string[]>>(
  Object.fromEntries(filterConfig.map((f) => [f.key, [] as string[]])) as any
);
const dateFrom = ref("");
const dateTo = ref("");
const cascadeBusy = ref(false);
const loading = ref(false);
const lastError = ref("");

/** Свёрнута ли верхняя панель фильтров. Состояние запоминается: у кого таблица
 *  открыта весь день, тому панель после загрузки данных только мешает. */
const FILTERS_OPEN_STORAGE_KEY = 'cost_filters_open';
const filtersOpen = ref(true);
onMounted(() => {
  try {
    if (localStorage.getItem(FILTERS_OPEN_STORAGE_KEY) === '0') filtersOpen.value = false;
  } catch { /* приватный режим — оставляем развёрнутой */ }
});
function toggleFilters() {
  filtersOpen.value = !filtersOpen.value;
  try {
    localStorage.setItem(FILTERS_OPEN_STORAGE_KEY, filtersOpen.value ? '1' : '0');
  } catch { /* ignore */ }
}

/** Сколько условий задано в верхней панели — показываем счётчик в свёрнутом
 *  виде, чтобы «пустая» выдача не выглядела загадкой при спрятанных фильтрах.
 *  Период считаем одним условием, даже если заданы обе даты. */
const activeFilterCount = computed(() => {
  let n = filterConfig.reduce((acc, f) => acc + (selected[f.key]?.length ? 1 : 0), 0);
  if (dateFrom.value || dateTo.value) n += 1;
  if (noWholesaleOnly.value) n += 1;
  if (peoFilter.value !== 'all') n += 1;
  return n;
});


// ── Helper: normalize level options ─────────────────────────────────────────
// API returns {id, text} for levels; CostMultiSelect needs string[]
// We store text values; cascade sends id values

const levelTextToId = reactive<Record<string, Record<string, string>>>({});
for (const k of LEVEL_KEYS) {
  levelTextToId[k] = {};
}

function normalizeFilterOptions(raw: Record<string, any>): Record<string, string[]> {
  const result: Record<string, string[]> = {};
  for (const key of Object.keys(raw)) {
    const vals = raw[key];
    if (LEVEL_KEYS.includes(key) && Array.isArray(vals) && vals.length > 0 && typeof vals[0] === "object") {
      // {id, text} → string[] of text, store mapping
      const map: Record<string, string> = {};
      const texts = vals.map((v: FilterOption) => {
        map[v.id] = v.text;
        return v.text;
      });
      levelTextToId[key] = map;
      // Also store reverse mapping
      result[key] = texts;
    } else {
      result[key] = vals as string[];
    }
  }
  return result;
}

// ── Filters ─────────────────────────────────────────────────────────────────

async function loadFilters() {
  cascadeBusy.value = true;
  try {
    const raw = await $fetch<Record<string, any>>(
      `${apiBase.value}/api/cost/filter-options`,
      { headers: fetchHeaders.value }
    );
    filterOptions.value = normalizeFilterOptions(raw);
    lastError.value = "";
  } catch (e: any) {
    lastError.value = e?.data?.detail || e?.message || String(e);
    console.error("[cost] filter-options failed", e);
  } finally {
    cascadeBusy.value = false;
  }
}

function isLocked(key: FilterKey): boolean {
  if (key === 'calc_sign') return false;
  // Находим самый нижний уровень (максимальный индекс) с выбранными значениями
  let lowestIdx = -1;
  for (let i = LEVEL_KEYS.length - 1; i >= 0; i--) {
    if (selected[LEVEL_KEYS[i]]?.length > 0) {
      lowestIdx = i;
      break;
    }
  }
  if (lowestIdx === -1) return false;
  // brand_manager выше всех уровней — блокируется если любой уровень выбран
  if (key === 'brand_manager') return true;
  const keyIdx = LEVEL_KEYS.indexOf(key as any);
  if (keyIdx === -1) return false;
  return keyIdx < lowestIdx;
}

async function onFilterChange(changedKey: FilterKey) {
  // При изменении вышестоящего уровня — сбрасываем все нижестоящие
  if (changedKey === 'brand_manager') {
    for (const k of LEVEL_KEYS) selected[k] = [];
  } else if (LEVEL_KEYS.includes(changedKey)) {
    const changedIdx = LEVEL_KEYS.indexOf(changedKey);
    for (let i = changedIdx + 1; i < LEVEL_KEYS.length; i++) {
      selected[LEVEL_KEYS[i]] = [];
    }
  }

  const params = new URLSearchParams();

  // For level filters, send id values for cascade
  for (const [key, vals] of Object.entries(selected)) {
    if (!(vals as string[]).length) continue;
    if (LEVEL_KEYS.includes(key)) {
      // Convert text → id via reverse lookup
      const idMap: Record<string, string> = {};
      for (const [id, text] of Object.entries(levelTextToId[key] || {})) {
        idMap[text] = id;
      }
      for (const v of vals as string[]) {
        const id = idMap[v] || v;
        params.append(key, id);
      }
    } else {
      for (const v of vals as string[]) {
        params.append(key, v);
      }
    }
  }

  cascadeBusy.value = true;
  try {
    const raw = await $fetch<Record<string, any>>(
      `${apiBase.value}/api/cost/filter-options?${params}`,
      { headers: fetchHeaders.value }
    );
    const normalized = normalizeFilterOptions(raw);
    for (const k of filterConfig.map((c) => c.key)) {
      if (k === changedKey) continue;
      filterOptions.value[k] = normalized[k] || [];
      selected[k] = (selected[k] || []).filter((v: string) =>
        filterOptions.value[k] && filterOptions.value[k].includes(v)
      );
    }
    lastError.value = "";
  } catch (e: any) {
    lastError.value = e?.data?.detail || e?.message || String(e);
    console.error("[cost] cascade failed", e);
  } finally {
    cascadeBusy.value = false;
  }
}

const resetFilters = async () => {
  dateFrom.value = "";
  dateTo.value = "";
  for (const k of filterConfig.map((c) => c.key)) selected[k] = [];
  allAggregated.value = [];
  totalAllRecords.value = 0;
  currentPage.value = 0;
  selectedRowIndex.value = -1;
  changedRows.clear();
  await loadFilters();
};

// ── Build filter payload ───────────────────────────────────────────────────

function buildFilters(): Record<string, any> {
  const f: Record<string, any> = {};
  if (dateFrom.value) f.date_from = dateFrom.value;
  if (dateTo.value) f.date_to = dateTo.value;
  if (noWholesaleOnly.value) f.no_wholesale_only = true;
  for (const k of filterConfig.map((c) => c.key)) {
    const v = selected[k];
    if (v?.length) f[k] = v;
  }
  return f;
}

const fetchHeaders = computed(() => {
  const email = user.value?.email || "";
  return email ? { "X-Cost-User": email } : {};
});
const noWholesaleOnly = ref(false);

const approvalTarget = ref<any>(null);
const approvalComment = ref('');
const approving = ref(false);
const peoFilter = ref<string>('all');

/** Бренд-менеджеру по умолчанию показываем только согласованные ПЭО калькуляции.
 * Фильтр остаётся обычным — пользователь волен переключить его на любой другой.
 *
 * Роль определяем по имени, а не по набору прав: права у ролей администратор
 * может переназначить, а «Бренд-менеджер» — системная роль (is_system=true),
 * её не переименовывают. Ключ в ответе /roles/my — role_name.
 *
 * Дефолт применяется ровно один раз и только если пользователь ещё не трогал
 * фильтр сам: роли приходят асинхронно (composable грузит их в своём onMounted),
 * и без этой защиты поздний ответ мог бы затереть уже сделанный выбор. */
const BRAND_MANAGER_ROLE = 'Бренд-менеджер';
const peoFilterTouched = ref(false);
let peoDefaultApplied = false;

function onPeoFilterChange() {
  peoFilterTouched.value = true;
  loadData();
}

watch(roles, (list) => {
  if (peoDefaultApplied || peoFilterTouched.value) return;
  const isBrandManager = (list || []).some(
    (r: any) => String(r?.role_name || '').trim() === BRAND_MANAGER_ROLE
  );
  if (!isBrandManager) return;
  peoDefaultApplied = true;
  peoFilter.value = 'approved';
  // Роли могли догрузиться уже после автозагрузки по URL-фильтрам — тогда
  // таблица показывает данные без учёта дефолта, перезапрашиваем.
  if (totalAllRecords.value > 0) loadData();
}, { immediate: true, deep: true });

/** Скрыть калькуляции, уже отправленные в DWH (значок 📤, флаг `_has_audit`).
 *
 * Калькулятору и ПЭО такие строки в работе только мешают: править в них нечего
 * и статус ПЭО уже не поставить, — поэтому у этих ролей фильтр включён ПО
 * УМОЛЧАНИЮ. Но снять его можно: сначала он был жёстко зафиксирован и чекбокс
 * стоял `disabled`, и это оказалось лишним — иногда нужно посмотреть и
 * отправленное (замечание заказчика 26.08.2026). Остальным ролям фильтр по
 * умолчанию выключен, чтобы картина базы не менялась у них незаметно.
 *
 * Выбор пользователя запоминается и перекрывает роль: `null` означает «человек
 * ещё не трогал переключатель, действует значение по роли».
 *
 * Роль, как и у дефолта бренд-менеджера, определяем по имени системной роли:
 * права админ может переназначить, а имя не меняют. */
const DWH_HIDDEN_ROLES = ['Калькулятор', 'ПЭО'];
const HIDE_DWH_STORAGE_KEY = 'cost_hide_dwh_sent';

/** Значение по умолчанию для текущей роли. */
const hideDwhSentDefault = computed(() =>
  (roles.value || []).some((r: any) => DWH_HIDDEN_ROLES.includes(String(r?.role_name || '').trim()))
);

const hideDwhSentManual = ref<boolean | null>(null);
onMounted(() => {
  try {
    const stored = localStorage.getItem(HIDE_DWH_STORAGE_KEY);
    if (stored === '1') hideDwhSentManual.value = true;
    else if (stored === '0') hideDwhSentManual.value = false;
  } catch { /* приватный режим — остаётся значение по роли */ }
});

const hideDwhSent = computed(() =>
  hideDwhSentManual.value === null ? hideDwhSentDefault.value : hideDwhSentManual.value
);

function setHideDwhSent(checked: boolean) {
  hideDwhSentManual.value = checked;
  try { localStorage.setItem(HIDE_DWH_STORAGE_KEY, checked ? '1' : '0'); } catch { /* ignore */ }
}

/** Сколько строк текущей выборки скрыто фильтром — иначе «пропажа» строк выглядит
 * как потеря данных. */
const dwhSentHiddenCount = computed(() =>
  hideDwhSent.value ? allAggregated.value.filter((r: any) => r._has_audit).length : 0
);

// ── Data loading ────────────────────────────────────────────────────────────

const allAggregated = ref<any[]>([]);
const totalAllRecords = ref(0);

/** Сколько строк показывать на странице — выбор пользователя, запоминается.
 *
 * Пагинация здесь клиентская: выдача уже в памяти, поэтому смена размера
 * страницы ничего не перезапрашивает. Верхнее значение держим на 500 — таблица
 * широкая, и на больших числах отрисовка заметно тяжелеет. */
const PAGE_SIZE_OPTIONS = [10, 25, 50, 100, 200, 500];
const PAGE_SIZE_STORAGE_KEY = 'cost_page_size';

function loadPageSize(): number {
  try {
    const stored = Number(localStorage.getItem(PAGE_SIZE_STORAGE_KEY));
    if (PAGE_SIZE_OPTIONS.includes(stored)) return stored;
  } catch { /* приватный режим — просто берём значение по умолчанию */ }
  return 25;
}

const pageSize = ref<number>(loadPageSize());

function onPageSizeChange(value: string | number) {
  const next = Number(value);
  if (!PAGE_SIZE_OPTIONS.includes(next)) return;
  pageSize.value = next;
  // Страница сбрасывается на первую: иначе после укрупнения строк текущий
  // номер мог указывать за пределы выборки.
  currentPage.value = 0;
  try { localStorage.setItem(PAGE_SIZE_STORAGE_KEY, String(next)); } catch { /* ignore */ }
}

// ── Column filters (client-side, top-down cascade) ──────────────────────────
type ColFilterKey = 'col_bm' | 'col_model' | 'col_articul' | 'col_model_name' | 'col_plan_id' | 'col_calc_sign' | 'col_country' | 'col_season' | 'col_date';

const COL_FILTER_KEYS: ColFilterKey[] = ['col_bm', 'col_model', 'col_articul', 'col_model_name', 'col_plan_id', 'col_calc_sign', 'col_country', 'col_season', 'col_date'];

const columnFilterConfig: { key: ColFilterKey; label: string; field: string }[] = [
  { key: 'col_bm', label: 'Бренд-менеджер', field: 'Бренд-менеджер' },
  { key: 'col_model', label: 'Модель', field: 'Модель' },
  { key: 'col_articul', label: 'Артикул', field: 'Артикул' },
  { key: 'col_model_name', label: 'Наименование модели', field: 'Наименование модели' },
  { key: 'col_plan_id', label: 'PLAN_ID', field: 'PLAN_ID' },
  { key: 'col_calc_sign', label: 'Пр.кальк', field: 'Признак калькуляции' },
  { key: 'col_country', label: 'Страна пр-ва', field: 'Страна пр-ва' },
  { key: 'col_season', label: 'Сезон', field: 'Сезон' },
  { key: 'col_date', label: 'Дата', field: 'дата расчета' },
];

const columnFilters = reactive<Record<string, string[]>>(
  Object.fromEntries(columnFilterConfig.map((c) => [c.key, [] as string[]])) as any
);

function _colFilterValue(row: any, field: string): string {
  return (row[field] ?? '').toString().trim();
}

const columnFilterOptions = computed(() => {
  const opts: Record<string, string[]> = {};
  for (let i = 0; i < columnFilterConfig.length; i++) {
    const cfg = columnFilterConfig[i];
    // Каскад: только вышестоящие фильтры (меньший индекс) ограничивают опции
    let available = allAggregated.value;
    for (let j = 0; j < i; j++) {
      const higher = columnFilterConfig[j];
      const sel = columnFilters[higher.key];
      if (sel && sel.length > 0) {
        available = available.filter((r: any) => sel.includes(_colFilterValue(r, higher.field)));
      }
    }
    const vals = new Set<string>();
    for (const row of available) {
      let v = _colFilterValue(row, cfg.field);
      if (cfg.key === 'col_date') v = v.split('T')[0];
      if (v) vals.add(v);
    }
    opts[cfg.key] = Array.from(vals).sort();
  }
  return opts;
});

function isColFilterLocked(key: string): boolean {
  const idx = COL_FILTER_KEYS.indexOf(key as ColFilterKey);
  if (idx === -1) return false;
  for (let i = COL_FILTER_KEYS.length - 1; i > idx; i--) {
    if (columnFilters[COL_FILTER_KEYS[i]]?.length > 0) return true;
  }
  return false;
}

function onColFilterChange(changedKey: string) {
  const idx = COL_FILTER_KEYS.indexOf(changedKey as ColFilterKey);
  if (idx >= 0) {
    for (let i = idx + 1; i < COL_FILTER_KEYS.length; i++) {
      columnFilters[COL_FILTER_KEYS[i]] = [];
    }
  }
}

const filteredAggregated = computed(() => {
  return allAggregated.value.filter((row: any) => {
    // Отправленные в DWH — вне работы: править и согласовывать в них нечего.
    if (hideDwhSent.value && row._has_audit) return false;
    return columnFilterConfig.every((cfg) => {
      const sel = columnFilters[cfg.key];
      if (!sel || sel.length === 0) return true;
      let val = _colFilterValue(row, cfg.field);
      if (cfg.key === 'col_date') val = val.split('T')[0];
      return sel.includes(val);
    });
  });
});

const columnFiltersActive = computed(() => {
  return columnFilterConfig.some((cfg) => columnFilters[cfg.key].length > 0);
});

function resetColumnFilters() {
  for (const cfg of columnFilterConfig) {
    columnFilters[cfg.key] = [];
  }
  currentPage.value = 0;
}

// ── Sorting ────────────────────────────────────────────────────────────────────
const sortField = ref<string>('');
const sortDir = ref<'asc' | 'desc'>('asc');

function toggleSort(field: string) {
  if (sortField.value === field) {
    if (sortDir.value === 'asc') sortDir.value = 'desc';
    else { sortField.value = ''; sortDir.value = 'asc'; }
  } else {
    sortField.value = field;
    sortDir.value = 'asc';
  }
  currentPage.value = 0;
}

const sortedRows = computed(() => {
  const data = filteredAggregated.value;
  if (!sortField.value) return data;
  const field = sortField.value;
  const dir = sortDir.value === 'asc' ? 1 : -1;
  return [...data].sort((a, b) => {
    let va: any, vb: any;
    // Вычисляемые поля (наценка/маржа/отклонение)
    if (field === 'calc_markup_rub' || field === 'calc_markup_pct' || field === 'calc_margin_pct' || field === 'calc_margin_deviation') {
      const ca = calc(a, showUSD.value), cb = calc(b, showUSD.value);
      if (field === 'calc_markup_rub') { va = ca.markupRub; vb = cb.markupRub; }
      else if (field === 'calc_markup_pct') { va = ca.markupPct; vb = cb.markupPct; }
      else if (field === 'calc_margin_pct') { va = ca.marginPct; vb = cb.marginPct; }
      else {
        va = marginDeviation(a, showUSD.value);
        vb = marginDeviation(b, showUSD.value);
        if (va === null) va = -Infinity;
        if (vb === null) vb = -Infinity;
      }
    } else {
      va = a[field]; vb = b[field];
    }
    // Numeric comparison
    const na = Number(va), nb = Number(vb);
    if (!isNaN(na) && !isNaN(nb)) return (na - nb) * dir;
    // String comparison
    return String(va ?? '').localeCompare(String(vb ?? '')) * dir;
  });
});

function getOriginalIndex(row: any): number {
  return allAggregated.value.indexOf(row);
}

watch(() => sortedRows.value.length, (newLen) => {
  if (currentPage.value * pageSize.value >= newLen && newLen > 0) {
    currentPage.value = 0;
  }
});

const currentPage = ref(0);
const totalPages = computed(() => Math.max(1, Math.ceil(sortedRows.value.length / pageSize.value)));
const pageStart = computed(() => currentPage.value * pageSize.value);
const pageRows = computed(() => sortedRows.value.slice(pageStart.value, pageStart.value + pageSize.value));
const pageRange = computed(() => {
  const filteredCount = sortedRows.value.length;
  if (!filteredCount) return "0";
  const a = pageStart.value + 1;
  const b = Math.min(pageStart.value + pageSize.value, filteredCount);
  return `${a}–${b}`;
});

async function clearMyPendingChanges() {
  try {
    await $fetch(`${apiBase.value}/api/cost/pending-changes/clear-my`, {
      method: "POST",
      headers: fetchHeaders.value,
    });
  } catch { /* best-effort */ }
}

async function loadData() {
  loading.value = true;
  try {
    const payload = buildFilters();
    if (peoFilter.value !== 'all') payload.peo_filter = peoFilter.value;
    const result = await $fetch<{ data: any[]; count: number; mp_formula_inputs: MpFormulaInputs | null }>(
      `${apiBase.value}/api/cost/aggregated`,
      { method: "POST", body: payload, headers: fetchHeaders.value }
    );
    allAggregated.value = result.data || [];
    totalAllRecords.value = result.count || 0;
    mpFormulaInputs.value = result.mp_formula_inputs || null;
    currentPage.value = 0;
    selectedRowIndex.value = -1;
    clearPeoSelection();
    lastError.value = "";
    // Значения с сервера раскладываем по ключу калькуляции, а не по номеру
    // строки. Раньше здесь оставались значения от предыдущей выдачи: очистки
    // не было, а перезапись шла только там, где сервер вернул непустое поле, —
    // из-за этого чужой комментарий «прилипал» к строке, попавшей на тот же
    // номер. Несохранённый ввод не трогаем: он остаётся при своей калькуляции,
    // даже если та ушла под фильтр.
    for (const r of allAggregated.value) {
      const k = calcRowKey(r);
      if (changedRows.has(k)) {
        // Правка ещё не сохранена — обновляем только ссылку на свежую строку,
        // чтобы сохранение ушло с актуальными суммами.
        changedRows.set(k, r);
        continue;
      }
      // Отсутствующее значение именно УДАЛЯЕМ, а не пишем нулём: пустое поле
      // ввода в таблице отличается от введённого нуля.
      if (r.price_rf != null) priceRF[k] = r.price_rf; else delete priceRF[k];
      if (r.price_kz != null) priceKZ[k] = r.price_kz; else delete priceKZ[k];
      if (r.price_uz != null) priceUZ[k] = r.price_uz; else delete priceUZ[k];
      if (r.comment != null) comments[k] = r.comment; else delete comments[k];
      // Авто-вычисляем наценку из розничной и оптовой цен строки
      const markup = computeMarkupFromRow(r);
      if (markup) markupSelections[k] = markup; else delete markupSelections[k];
    }
    // Ключи калькуляций, которых в новой выдаче нет, выбрасываем — иначе за
    // сессию с десятком фильтров они накапливаются, а при возврате строки
    // показали бы значение, устаревшее относительно БД. Несохранённый ввод
    // (`changedRows`) остаётся: его ещё не с чем сверять.
    const liveKeys = new Set(allAggregated.value.map(calcRowKey));
    for (const store of [priceRF, priceKZ, priceUZ, comments, markupSelections] as Record<string, any>[]) {
      for (const k of Object.keys(store)) {
        if (!liveKeys.has(k) && !changedRows.has(k)) delete store[k];
      }
    }
    // Досчитываем USD-цены, если API вернуло их нулями
    const r2 = (v: number) => Math.round(v * 100) / 100;
    for (const r of allAggregated.value) {
      const rubW = Number(r['avg_Отпускная цена по уровню, руб'] || 0);
      const rubR = Number(r['avg_Розничная цена по уровню, руб.'] || 0);
      const usdW = Number(r['avg_Отпускная цена по уровню, USD.'] || 0);
      const usdR = Number(r['avg_Розничная цена по уровню, USD.'] || 0);
      if (usdW > 0 && usdR > 0) continue; // оба USD уже есть — ничего не делаем
      // Выбираем курс
      let rate = Number(r['avg_Курс на дату расчета'] || 0);
      if (rate <= 0) rate = _deriveRate(r);
      if (rate <= 0) {
        for (const rr of allAggregated.value) { rate = _deriveRate(rr); if (rate > 0) break; }
      }
      if (rate <= 0) rate = 92; // fallback
      if (usdW <= 0 && rubW > 0) r['avg_Отпускная цена по уровню, USD.'] = r2(rubW / rate);
      if (usdR <= 0 && rubR > 0) r['avg_Розничная цена по уровню, USD.'] = r2(rubR / rate);
    }
  } catch (e: any) {
    console.error("[cost] aggregated load failed", e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    loading.value = false;
  }
}

const prevPage = () => { if (currentPage.value > 0) currentPage.value -= 1; };
const nextPage = () => { if (currentPage.value < totalPages.value - 1) currentPage.value += 1; };

const selectedRowIndex = ref<number>(-1);

const selectRow = (absoluteIdx: number) => {
  selectedRowIndex.value = absoluteIdx;
};

// ── Margin targets modal ─────────────────────────────────────────────────────

async function openMarginModal() {
  showMarginModal.value = true;
  marginTargetsLoading.value = true;
  marginSaveStatus.value = '';
  try {
    // Fetch all level01 options (no cascade filters)
    const raw = await $fetch<Record<string, any>>(
      `${apiBase.value}/api/cost/filter-options`,
      { headers: fetchHeaders.value }
    );
    const level1Texts: string[] = [];
    if (Array.isArray(raw.level01) && raw.level01.length > 0 && typeof raw.level01[0] === 'object') {
      level1Texts.push(...raw.level01.map((v: any) => v.text));
    } else if (Array.isArray(raw.level01)) {
      level1Texts.push(...raw.level01);
    }

    // Fetch existing targets
    const targets = await $fetch<{ level1: string; target_margin_pct: number | null }[]>(
      `${apiBase.value}/api/cost/margin-targets`,
      { headers: fetchHeaders.value }
    );
    const targetMap: Record<string, number | null> = {};
    for (const t of targets) {
      targetMap[t.level1] = t.target_margin_pct;
    }

    // Merge: all level01 values + existing targets (default 0)
    marginTargetsList.value = level1Texts.map((l1) => ({
      level1: l1,
      target_margin_pct: targetMap[l1] ?? 0,
    }));
  } catch (e: any) {
    console.error('[cost] load margin targets failed', e);
    marginSaveStatus.value = 'Ошибка загрузки';
  } finally {
    marginTargetsLoading.value = false;
  }
}

async function saveMarginTargets() {
  marginSaving.value = true;
  marginSaveStatus.value = '';
  try {
    const result = await $fetch<{ success: boolean; count: number }>(
      `${apiBase.value}/api/cost/margin-targets`,
      {
        method: 'POST',
        body: {
          targets: marginTargetsList.value.map((t) => ({
            level1: t.level1,
            target_margin_pct: Number(t.target_margin_pct) || 0,
          })),
          username,
        },
        headers: fetchHeaders.value,
      }
    );
    if (result.success) {
      marginSaveStatus.value = `Сохранено: ${result.count} таргетов`;
    } else {
      marginSaveStatus.value = 'Ошибка сохранения';
    }
  } catch (e: any) {
    console.error('[cost] save margin targets failed', e);
    marginSaveStatus.value = 'Ошибка: ' + (e?.data?.detail || e?.message || String(e));
  } finally {
    marginSaving.value = false;
  }
}

// ── MP constants modal ───────────────────────────────────────────────────────

async function openMpConstantsModal() {
  showMpConstantsModal.value = true;
  mpConstantsSaveStatus.value = '';
  await loadMpConstants();
}

async function loadMpConstants() {
  mpConstantsLoading.value = true;
  try {
    mpConstantsList.value = await $fetch<any[]>(
      `${apiBase.value}/api/cost/mp-constants`,
      { headers: fetchHeaders.value }
    );
  } catch (e: any) {
    console.error('[cost] load mp constants failed', e);
    mpConstantsSaveStatus.value = 'Ошибка загрузки';
  } finally {
    mpConstantsLoading.value = false;
  }
}

async function addMpConstants() {
  const f = mpConstantsForm.value;
  if (f.markup_mp == null || f.expense_pct_mp == null || f.spp_discount == null) {
    mpConstantsSaveStatus.value = 'Заполните все три значения';
    return;
  }
  mpConstantsSaving.value = true;
  mpConstantsSaveStatus.value = '';
  try {
    await $fetch(`${apiBase.value}/api/cost/mp-constants`, {
      method: 'POST',
      body: {
        effective_date: f.effective_date || undefined,
        markup_mp: Number(f.markup_mp),
        expense_pct_mp: Number(f.expense_pct_mp),
        spp_discount: Number(f.spp_discount),
        username,
      },
      headers: fetchHeaders.value,
    });
    mpConstantsSaveStatus.value = 'Добавлено';
    await loadMpConstants();
  } catch (e: any) {
    console.error('[cost] add mp constants failed', e);
    mpConstantsSaveStatus.value = 'Ошибка: ' + (e?.data?.detail || e?.message || String(e));
  } finally {
    mpConstantsSaving.value = false;
  }
}

// ── Цены материалов по плану (миграция 0033) ─────────────────────────────────
//
// Массовая правка цен материалов по PLAN_ID целиком. Наборы — «документы» с
// историей: применён к расчёту может быть только один на план, либо ни один.
// Приоритет в расчёте себестоимости: версия калькуляции → набор цен → источник,
// поэтому в гриде показываем, сколько строк материала уже перекрыто версией —
// там цена из набора не подействует.

type PlanPriceSet = {
  id: number; plan_id: string; title: string; status: string;
  rate: number | null; rows_count: number; created_by: string;
  created_at: string; applied_by: string | null; applied_at: string | null;
};

type PlanPriceRow = {
  'Наименование': string; 'артикул материала': string;
  'свойство1': string; 'свойство2': string; 'свойство3': string;
  rows_count: number; overridden_rows: number; distinct_prices: number;
  min_price_rub: number | null; max_price_rub: number | null;
  source_price_rub: number | null; source_price_usd: number | null;
  price_rub: number | null; price_usd: number | null;
};

const showPlanPricesModal = ref(false);
const planPricesPlanId = ref('');
const planPricesLoading = ref(false);
const planPricesSaving = ref(false);
const planPricesLoaded = ref(false);
const planPricesStatus = ref('');

/** Ошибка операций модалки цен по плану — показывается ВНУТРИ модалки.
 *
 * Раньше всё писалось в `lastError`, чей баннер отрисован на странице и
 * перекрыт модалкой: любой отказ (403, 503, обрыв запроса) выглядел как
 * «нажал — ничего не произошло». Именно так пришла жалоба 26.08.2026 про
 * кнопку «Создать набор». */
const planPricesError = ref('');
const planPriceSets = ref<PlanPriceSet[]>([]);
const planPriceRows = ref<PlanPriceRow[]>([]);
/** Декоры набора. Отдельным списком, потому что ключ у них другой
 * («Декоры, наименование» вместо пяти полей материала) и цена — это сама
 * сумма, а не множитель к норме (см. миграцию 0035). */
const planDecorRows = ref<any[]>([]);
const planPriceForm = ref<{ set_id: number | null; title: string; rate: number | null; status: string }>({
  set_id: null, title: '', rate: null, status: 'draft',
});

/** Применённый набор править нельзя: его цены уже в расчёте, и правка «под ногами»
 * рассинхронизировала бы кэш с набором (бэкенд это тоже запрещает). */
const planPriceFormLocked = computed(() => planPriceForm.value.status === 'applied');

function planRowProps(r: PlanPriceRow): string {
  return [r['свойство1'], r['свойство2'], r['свойство3']].filter(Boolean).join(' / ') || '—';
}

async function openPlanPricesModal() {
  showPlanPricesModal.value = true;
  planPricesStatus.value = '';
  // Если в фильтрах выбран ровно один план — подставляем его, это типичный сценарий.
  // Фильтры лежат в `selected` (см. filterConfig): здесь раньше стояло
  // `filters.value?.plan_id` — такой переменной нет, и открытие модалки падало
  // с ReferenceError, не дойдя до загрузки цен.
  const selectedPlans = (selected.plan_id || []).filter((v: string) => v && v !== 'all');
  if (selectedPlans.length === 1 && !planPricesPlanId.value) {
    planPricesPlanId.value = String(selectedPlans[0]);
  }
  if (planPricesPlanId.value) await loadPlanPrices();
}

async function loadPlanPrices() {
  const plan = planPricesPlanId.value.trim();
  if (!plan) return;
  planPricesLoading.value = true;
  planPricesStatus.value = '';
  try {
    const [matsResp, setsResp] = await Promise.all([
      $fetch<{ data: PlanPriceRow[]; decors: any[] }>(
        `${apiBase.value}/api/cost/plan-materials?plan_id=${encodeURIComponent(plan)}`,
        { headers: fetchHeaders.value }),
      $fetch<{ data: PlanPriceSet[] }>(
        `${apiBase.value}/api/cost/plan-price-sets?plan_id=${encodeURIComponent(plan)}`,
        { headers: fetchHeaders.value }),
    ]);
    planPriceSets.value = setsResp.data || [];
    // Грид заполняем средними ценами из источника — их и правит пользователь.
    planPriceRows.value = (matsResp.data || []).map((m: any) => ({
      ...m,
      source_price_rub: m.avg_price_rub,
      source_price_usd: m.avg_price_usd,
      price_rub: m.avg_price_rub,
      price_usd: m.avg_price_usd,
    }));
    planDecorRows.value = (matsResp.decors || []).map((d: any) => ({
      ...d,
      row_kind: 'decor',
      source_price_rub: d.avg_price_rub,
      source_price_usd: d.avg_price_usd,
      price_rub: d.avg_price_rub,
      price_usd: d.avg_price_usd,
    }));
    resetPlanPriceForm();
    // Курс по умолчанию — средний по плану, если он один и тот же.
    const rates = (matsResp.data || []).map((m: any) => m.avg_rate).filter((v: any) => v != null);
    if (rates.length) planPriceForm.value.rate = Number(rates[0]);
    planPricesLoaded.value = true;
    const applied = planPriceSets.value.find(s => s.status === 'applied');
    planPricesStatus.value = applied
      ? `Применён набор «${applied.title || applied.id}»`
      : 'Ни один набор не применён — действуют исходные данные';
  } catch (e: any) {
    console.error('[cost] load plan prices failed', e);
    planPricesError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    planPricesLoading.value = false;
  }
}

function resetPlanPriceForm() {
  planPriceForm.value = { set_id: null, title: '', rate: planPriceForm.value.rate, status: 'draft' };
  for (const r of [...planPriceRows.value, ...planDecorRows.value]) {
    r.price_rub = r.source_price_rub;
    r.price_usd = r.source_price_usd;
    (r as any)._usdManual = false; // доллары снова исходные, не заданные вручную
  }
}

async function openPlanPriceSet(setId: number) {
  planPricesLoading.value = true;
  try {
    const data = await $fetch<{ set: PlanPriceSet; rows: any[] }>(
      `${apiBase.value}/api/cost/plan-price-sets/${setId}`, { headers: fetchHeaders.value });
    planPriceForm.value = {
      set_id: data.set.id,
      title: data.set.title || '',
      rate: data.set.rate != null ? Number(data.set.rate) : null,
      status: data.set.status,
    };
    // Накладываем цены набора на грид по ключу материала; материалы, которых в
    // наборе нет, остаются с исходной ценой.
    const byKey = new Map<string, any>();
    const byDecor = new Map<string, any>();
    for (const r of data.rows || []) {
      if (r.row_kind === 'decor') byDecor.set(String(r['Наименование'] ?? '').trim(), r);
      else byKey.set(planRowKey(r), r);
    }
    // Доллар в наборе может быть пустым: производную от курса считает сервер, и
    // фронт её не сохраняет. В гриде показываем то же значение, что ляжет в
    // расчёт, — иначе колонка «Цена, $» выглядела бы пустой.
    const setRate = Number(data.set.rate || 0);
    const showUsd = (saved: any, fallback: any) => {
      if (saved.price_usd !== null && saved.price_usd !== undefined) return saved.price_usd;
      const rub = Number(saved.price_rub);
      if (setRate > 0 && Number.isFinite(rub)) return Number((rub / setRate).toFixed(4));
      return fallback;
    };
    for (const r of planPriceRows.value) {
      const saved = byKey.get(planRowKey(r));
      r.price_rub = saved ? saved.price_rub : r.source_price_rub;
      r.price_usd = saved ? showUsd(saved, r.source_price_usd) : r.source_price_usd;
      // доллар в наборе задан явно — значит его правили вручную
      (r as any)._usdManual = !!(saved && saved.price_usd !== null && saved.price_usd !== undefined);
    }
    for (const r of planDecorRows.value) {
      const saved = byDecor.get(String(r['Декоры, наименование'] ?? '').trim());
      r.price_rub = saved ? saved.price_rub : r.source_price_rub;
      r.price_usd = saved ? showUsd(saved, r.source_price_usd) : r.source_price_usd;
      (r as any)._usdManual = !!(saved && saved.price_usd !== null && saved.price_usd !== undefined);
    }
    planPricesStatus.value = `Открыт набор «${data.set.title || data.set.id}» (${data.set.status})`;
  } catch (e: any) {
    console.error('[cost] open plan price set failed', e);
    planPricesError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    planPricesLoading.value = false;
  }
}

/** Цена строки отличается от источника, то есть строка реально переопределяет цену.
 *
 * По этому признаку набор и отправляется на сервор: строки с ценой, равной
 * источнику, при применении набора ничего не меняют (наложение идёт через
 * COALESCE по price_rub/price_usd), а в теле запроса занимали основной объём —
 * на плане с 669 материалами это 250 КБ, и такой POST отбивался на прод-фасаде
 * ещё до FastAPI (ответ прокси без CORS-заголовков → «Failed to fetch»).
 *
 * Сравниваем с точностью до 4 знаков — столько же хранит база и отдаёт
 * `round(avg(...), 4)` в plan-materials, иначе строки «менялись» бы из-за
 * плавающей точки. */
function planPriceNorm(v: any): number | null {
  if (v === null || v === undefined || v === '') return null;
  const n = Number(v);
  return Number.isFinite(n) ? Math.round(n * 10000) / 10000 : null;
}

/** Задан ли доллар строки вручную. Флаг ставится там, где пользователь правит
 * колонку «Цена, $», и снимается при правке рубля или пересчёте по курсу.
 *
 * Раньше это выводилось из чисел (совпадает ли доллар с рубль÷курс), но такая
 * эвристика врала: после правки рубля доллар уже пересчитан по текущему курсу
 * формы, и «производный» он или нет — по значению не отличить от заданного. */
function planUsdIsManual(r: any): boolean {
  return r._usdManual === true;
}

/** Доллар — производная от рублёвой цены по курсу набора.
 *
 * Такую цену на сервер не отправляем: он считает её сам тем же правилом
 * (см. `_apply_plan_price_set_to_cache`). Иначе ввод курса менял бы price_usd во
 * всех строках плана, все они выглядели бы переопределёнными, и тело запроса
 * снова разрасталось бы до сотен килобайт. */
function planUsdIsDerived(r: any): boolean {
  return Number(planPriceForm.value.rate || 0) > 0 && !planUsdIsManual(r);
}

function planPriceOverridden(r: any): boolean {
  if (planPriceNorm(r.price_rub) !== planPriceNorm(r.source_price_rub)) return true;
  if (planUsdIsDerived(r)) return false;
  return planPriceNorm(r.price_usd) !== planPriceNorm(r.source_price_usd);
}

/** Сколько строк набора переопределяют цену — показываем на кнопке сохранения. */
const planPriceOverriddenCount = computed(() =>
  planPriceRows.value.filter(planPriceOverridden).length
  + planDecorRows.value.filter(planPriceOverridden).length
);

function planRowKey(r: any): string {
  return [r['Наименование'], r['артикул материала'], r['свойство1'], r['свойство2'], r['свойство3']]
    .map((v: any) => String(v ?? '').trim()).join('');
}

/** Взаимный пересчёт руб ↔ $ по курсу набора — как в редакторе версий, но курс
 * один на весь набор (решение пользователя). */
function onPlanPriceEdit(r: PlanPriceRow, changed: 'rub' | 'usd') {
  // Правку доллара помним: при смене курса такую строку не пересчитываем, и на
  // сервер её доллар уходит явным значением, а не как производная от курса.
  (r as any)._usdManual = changed === 'usd';
  const rate = Number(planPriceForm.value.rate || 0);
  if (rate <= 0) return;
  if (changed === 'rub') {
    const v = Number(r.price_rub);
    r.price_usd = Number.isFinite(v) ? Number((v / rate).toFixed(4)) : null;
  } else {
    const v = Number(r.price_usd);
    r.price_rub = Number.isFinite(v) ? Number((v * rate).toFixed(4)) : null;
  }
}

/** Смена курса пересчитывает $ из рублей — рубль считаем ведущим.
 *
 * Вручную заданные доллары не трогаем. При очистке курса производные
 * возвращаются к исходным: иначе в гриде остались бы доллары по уже
 * несуществующему курсу, и каждая строка выглядела бы переопределённой. */
function onPlanRateChange(raw?: string) {
  // Курс берём из события, а не из формы: при вводе этот обработчик срабатывает
  // раньше, чем v-model успевает записать новое значение, и пересчёт шёл по
  // предыдущему курсу — доллары в гриде отставали на одну правку.
  const parsed = raw === undefined
    ? Number(planPriceForm.value.rate || 0)
    : Number(String(raw).replace(',', '.'));
  const rate = Number.isFinite(parsed) ? parsed : 0;
  for (const r of [...planPriceRows.value, ...planDecorRows.value]) {
    if (planUsdIsManual(r)) continue;
    const v = Number(r.price_rub);
    r.price_usd = rate > 0 && Number.isFinite(v)
      ? Number((v / rate).toFixed(4))
      : (r.source_price_usd ?? null);
  }
}

async function savePlanPriceSet() {
  const plan = planPricesPlanId.value.trim();
  // Раньше здесь стоял молчаливый return: без номера плана кнопка «работала»,
  // но ничего не делала и ничего не говорила.
  if (!plan) {
    planPricesError.value = 'Укажите номер плана и нажмите «Показать»';
    return;
  }
  planPricesError.value = '';
  planPricesSaving.value = true;
  try {
    const resp = await $fetch<{ set_id: number }>(`${apiBase.value}/api/cost/plan-price-sets`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...fetchHeaders.value },
      body: {
        plan_id: plan,
        set_id: planPriceForm.value.set_id,
        title: planPriceForm.value.title,
        // пустое поле курса — это отсутствие курса, а не пустая строка:
        // такая строка уходила в numeric-колонку и сохранение падало с 500
        rate: Number(planPriceForm.value.rate) > 0 ? Number(planPriceForm.value.rate) : null,
        username: user.value?.email || 'system',
        rows: [
          ...planPriceRows.value.filter(planPriceOverridden).map(r => ({
            row_kind: 'material',
            'Наименование': r['Наименование'],
            'артикул материала': r['артикул материала'],
            'свойство1': r['свойство1'],
            'свойство2': r['свойство2'],
            'свойство3': r['свойство3'],
            price_rub: r.price_rub,
            // null → сервер посчитает доллар из рубля по курсу набора
            price_usd: planUsdIsDerived(r) ? null : r.price_usd,
            source_price_rub: r.source_price_rub,
            source_price_usd: r.source_price_usd,
            rows_count: r.rows_count,
          })),
          // Декоры: ключ один — наименование декора; бэкенд кладёт его в
          // колонку "Наименование" (см. миграцию 0035).
          ...planDecorRows.value.filter(planPriceOverridden).map(r => ({
            row_kind: 'decor',
            'Декоры, наименование': r['Декоры, наименование'],
            price_rub: r.price_rub,
            price_usd: r.price_usd,
            source_price_rub: r.source_price_rub,
            source_price_usd: r.source_price_usd,
            rows_count: r.rows_count,
          })),
        ],
      },
    });
    planPriceForm.value.set_id = resp.set_id;
    planPricesStatus.value = planPriceOverriddenCount.value
      ? `Набор сохранён: строк с переопределённой ценой ${planPriceOverriddenCount.value}`
      : 'Набор сохранён пустым — ни одна цена не отличается от источника';
    await reloadPlanPriceSets();
  } catch (e: any) {
    console.error('[cost] save plan price set failed', e);
    planPricesError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    planPricesSaving.value = false;
  }
}

async function reloadPlanPriceSets() {
  const plan = planPricesPlanId.value.trim();
  if (!plan) return;
  const setsResp = await $fetch<{ data: PlanPriceSet[] }>(
    `${apiBase.value}/api/cost/plan-price-sets?plan_id=${encodeURIComponent(plan)}`,
    { headers: fetchHeaders.value });
  planPriceSets.value = setsResp.data || [];
}

async function applyPlanPriceSet(setId: number) {
  if (!confirm('Применить набор к расчёту себестоимости плана? Прежний применённый набор уйдёт в архив.')) return;
  planPricesSaving.value = true;
  try {
    const resp = await $fetch<any>(`${apiBase.value}/api/cost/plan-price-sets/${setId}/apply`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...fetchHeaders.value },
      body: { username: user.value?.email || 'system' },
    });
    planPricesStatus.value = `Применено: строк с ценами ${resp.price_rows_applied}, версий переналожено ${resp.versions_reapplied}`;
    await loadPlanPrices();
    await loadData();
  } catch (e: any) {
    console.error('[cost] apply plan price set failed', e);
    planPricesError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    planPricesSaving.value = false;
  }
}

async function unapplyPlanPriceSet(setId: number) {
  if (!confirm('Исключить набор из расчёта? Цены материалов вернутся к исходным данным источника.')) return;
  planPricesSaving.value = true;
  try {
    await $fetch<any>(`${apiBase.value}/api/cost/plan-price-sets/${setId}/unapply`, {
      method: 'POST', headers: { 'Content-Type': 'application/json', ...fetchHeaders.value },
    });
    planPricesStatus.value = 'Набор исключён из расчёта';
    await loadPlanPrices();
    await loadData();
  } catch (e: any) {
    console.error('[cost] unapply plan price set failed', e);
    planPricesError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    planPricesSaving.value = false;
  }
}

async function deletePlanPriceSet(setId: number) {
  if (!confirm('Удалить набор?')) return;
  planPricesSaving.value = true;
  try {
    await $fetch(`${apiBase.value}/api/cost/plan-price-sets/${setId}`, {
      method: 'DELETE', headers: fetchHeaders.value,
    });
    planPricesStatus.value = 'Набор удалён';
    if (planPriceForm.value.set_id === setId) resetPlanPriceForm();
    await reloadPlanPriceSets();
  } catch (e: any) {
    console.error('[cost] delete plan price set failed', e);
    planPricesError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    planPricesSaving.value = false;
  }
}

// ── Details modal ────────────────────────────────────────────────────────────
const showDetailsModal = ref(false);
const detailsModel = ref('');
const detailDateFrom = ref('');
const detailDateTo = ref('');
const detailCalcSign = ref<string[]>([]);
const detailsAllData = ref<any[]>([]);
const detailsFilteredData = ref<any[]>([]);
const detailsLoading = ref(false);

const AGG_GROUP_FIELDS = [
  'Бренд-менеджер', 'Модель', 'Артикул', 'Наименование модели', 'PLAN_ID',
  'Признак калькуляции', 'дата расчета', 'Уровень цен', 'Страна пр-ва',
  'Семья', 'Сезон', 'Level 01', 'Level 02', 'Level 03', 'Level 04', 'Level 05',
];

const showRawRowsModal = ref(false);
const detailsAggregatedRow = ref<any>(null);
const rawRowsData = ref<any[]>([]);
const rawRowsLoading = ref(false);

const detailsFilterConfig = [
  { key: 'col_date', label: 'Дата', field: 'дата расчета' },
  { key: 'calc_sign', label: 'Пр.кальк', field: 'Признак калькуляции' },
  { key: 'articul', label: 'Артикул', field: 'Артикул' },
  { key: 'name', label: 'Наименование', field: 'Наименование модели' },
  { key: 'task_num', label: '№ задания', field: 'Номер задания производства' },
];

const DET_COL_FILTER_KEYS = detailsFilterConfig.map((c) => c.key);

const detailsColumnFilters = reactive<Record<string, string[]>>(
  Object.fromEntries(detailsFilterConfig.map((c) => [c.key, [] as string[]])) as any
);

function _detColFilterValue(row: any, cfg: { key: string; field: string }): string {
  let v = (row[cfg.field] ?? '').toString().trim();
  if (cfg.key === 'col_date') v = v.split('T')[0];
  return v;
}

const detailsColumnFilterOptions = computed(() => {
  const opts: Record<string, string[]> = {};
  for (let i = 0; i < detailsFilterConfig.length; i++) {
    const cfg = detailsFilterConfig[i];
    let available = detailsAllData.value;
    for (let j = 0; j < i; j++) {
      const higher = detailsFilterConfig[j];
      const sel = detailsColumnFilters[higher.key];
      if (sel && sel.length > 0) {
        available = available.filter((r: any) => sel.includes(_detColFilterValue(r, higher)));
      }
    }
    const vals = new Set<string>();
    for (const row of available) {
      const v = _detColFilterValue(row, cfg);
      if (v) vals.add(v);
    }
    opts[cfg.key] = Array.from(vals).sort();
  }
  return opts;
});

function isDetFilterLocked(key: string): boolean {
  const idx = DET_COL_FILTER_KEYS.indexOf(key);
  if (idx === -1) return false;
  for (let i = DET_COL_FILTER_KEYS.length - 1; i > idx; i--) {
    if (detailsColumnFilters[DET_COL_FILTER_KEYS[i]]?.length > 0) return true;
  }
  return false;
}

function onDetFilterChange(changedKey: string) {
  const idx = DET_COL_FILTER_KEYS.indexOf(changedKey);
  if (idx >= 0) {
    for (let i = idx + 1; i < DET_COL_FILTER_KEYS.length; i++) {
      detailsColumnFilters[DET_COL_FILTER_KEYS[i]] = [];
    }
  }
  applyDetailsFilters();
}

function applyDetailsFilters() {
  detailsFilteredData.value = detailsAllData.value.filter((row: any) => {
    return detailsFilterConfig.every((cfg) => {
      const sel = detailsColumnFilters[cfg.key];
      if (!sel || sel.length === 0) return true;
      const val = _detColFilterValue(row, cfg);
      return sel.includes(val);
    });
  });
}

function resetDetailsColumnFilters() {
  for (const key of Object.keys(detailsColumnFilters)) {
    detailsColumnFilters[key] = [];
  }
  applyDetailsFilters();
}

async function openDetails(row: any) {
  const model = String(row['Модель'] || '').trim();
  if (!model) return;
  detailsModel.value = model;

  // Copy current main filters as defaults
  detailDateFrom.value = dateFrom.value;
  detailDateTo.value = dateTo.value;
  detailCalcSign.value = [...(selected.calc_sign || [])];

  showDetailsModal.value = true;
  await loadDetailsData();
}

async function loadDetailsData() {
  detailsLoading.value = true;
  try {
    const body: Record<string, any> = { model: detailsModel.value };
    if (detailDateFrom.value) body.date_from = detailDateFrom.value;
    if (detailDateTo.value) body.date_to = detailDateTo.value;
    if (detailCalcSign.value.length) body.calc_sign = detailCalcSign.value;

    const result = await $fetch<{ data: any[] }>(
      `${apiBase.value}/api/cost/details`,
      { method: 'POST', body }
    );
    detailsAllData.value = result.data || [];
    resetDetailsColumnFilters();
  } catch (e: any) {
    console.error('[cost] details load failed', e);
    detailsAllData.value = [];
    detailsFilteredData.value = [];
  } finally {
    detailsLoading.value = false;
  }
}

async function loadRawRows() {
  const row = detailsAggregatedRow.value;
  if (!row) return;
  rawRowsLoading.value = true;
  rawRowsData.value = [];
  try {
    const body: Record<string, any> = {};
    for (const field of AGG_GROUP_FIELDS) {
      const val = row[field];
      if (val !== null && val !== undefined && val !== '' && val !== '—') {
        body[field] = val;
      }
    }
    console.debug('[cost] raw-rows body', body);
    const result = await $fetch<{ data: any[]; count: number }>(
      `${apiBase.value}/api/cost/raw-rows`,
      { method: 'POST', body, headers: fetchHeaders.value }
    );
    console.debug('[cost] raw-rows result', result);
    rawRowsData.value = result.data || [];
  } catch (e: any) {
    console.error('[cost] raw-rows failed', e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    rawRowsLoading.value = false;
  }
}

async function openRawRows(row: any) {
  detailsAggregatedRow.value = row;
  showRawRowsModal.value = true;
  lastError.value = '';
  await loadRawRows();
}

function resetDetailsFilters() {
  detailDateFrom.value = '';
  detailDateTo.value = '';
  detailCalcSign.value = [];
  loadDetailsData();
}

function openDetailsInNewTab() {
  const w = window.open('', '_blank');
  if (!w) return;

  const rows = detailsFilteredData.value;
  const model = detailsModel.value;
  const hasData = rows && rows.length > 0;

  function nf(v) {
    if (v == null || v === '') return '\u2014';
    const n = Number(v);
    if (Number.isNaN(n)) return String(v);
    return n.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  }

  function escHtml(s) { return String(s).replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }

  function hbStyleStr(v: any, field: string): string {
    const n = Number(v);
    if (isNaN(n)) return '';
    const rng = detailsRanges.value[field];
    if (!rng || rng.max === rng.min) return '';
    const t = (n - rng.min) / (rng.max - rng.min);
    return `background-color:rgb(${Math.round(240 - 190 * t)},${Math.round(245 - 145 * t)},${Math.round(255 - 35 * t)});text-align:right;`;
  }

  // Build static table rows (always visible, JS-overridable)
  let tableHtml = '';
  for (let i = 0; i < (hasData ? rows.length : 0); i++) {
    const r = rows[i];
    let dv = r['\u0434\u0430\u0442\u0430 \u0440\u0430\u0441\u0447\u0435\u0442\u0430'];
    if (dv) {
      const s = String(dv);
      dv = s.includes('T') ? s.split('T')[0] : s;
    } else { dv = '\u2014'; }
    let dvRel = r['\u0414\u0430\u0442\u0430 \u0432\u044B\u043F\u0443\u0441\u043A\u0430'];
    if (dvRel) {
      const s = String(dvRel);
      dvRel = s.includes('T') ? s.split('T')[0] : s;
    } else { dvRel = '\u2014'; }
    const cells = [
      dv,
      r['\u041F\u0440\u0438\u0437\u043D\u0430\u043A \u043A\u0430\u043B\u044C\u043A\u0443\u043B\u044F\u0446\u0438\u0438'] || '\u2014',
      r['\u0410\u0440\u0442\u0438\u043A\u0443\u043B'] || '\u2014',
      r['\u041D\u0430\u0438\u043C\u0435\u043D\u043E\u0432\u0430\u043D\u0438\u0435 \u043C\u043E\u0434\u0435\u043B\u0438'] || '\u2014',
      r['\u041D\u043E\u043C\u0435\u0440 \u0437\u0430\u0434\u0430\u043D\u0438\u044F \u043F\u0440\u043E\u0438\u0437\u0432\u043E\u0434\u0441\u0442\u0432\u0430'] || '\u2014',
      dvRel,
      nf(r['\u0420\u043E\u0437\u043D\u0438\u0447\u043D\u0430\u044F \u0446\u0435\u043D\u0430, \u0440\u0443\u0431.']),
      nf(r['\u041E\u043F\u0442\u043E\u0432\u0430\u044F \u0446\u0435\u043D\u0430, \u0440\u0443\u0431.']),
      nf(r['\u041E\u0441\u043D. \u043C\u0430\u0442\u0435\u0440\u0438\u0430\u043B\u044B, \u0440\u0443\u0431.']),
      nf(r['\u0412\u0441\u043F\u043E\u043C. \u043C\u0430\u0442\u0435\u0440\u0438\u0430\u043B\u044B, \u0440\u0443\u0431.']),
      nf(r['\u041F\u043E\u0448\u0438\u0432, \u0440\u0443\u0431.']),
      nf(r['\u0420\u0430\u0441\u043A\u0440\u043E\u0439, \u0440\u0443\u0431.']),
      nf(r['\u0414\u0435\u043A\u043E\u0440, \u0440\u0443\u0431.']),
      nf(r['\u0412\u044F\u0437\u0430\u043D\u0438\u0435, \u0440\u0443\u0431.']),
      nf(r['\u0421\u0435\u0431\u0435\u0441\u0442\u043E\u0438\u043C\u043E\u0441\u0442\u044C, \u0440\u0443\u0431.']),
      nf(r['\u041D\u0430\u0446\u0435\u043D\u043A\u0430, \u0440\u0443\u0431.']),
      r['\u041D\u0430\u0446\u0435\u043D\u043A\u0430, %'] ?? '',
      r['\u041C\u0430\u0440\u0436\u0438\u043D\u0430\u043B\u044C\u043D\u043E\u0441\u0442\u044C, %'] ?? '',
    ];
    const POPUP_NUM_FIELDS = [
      'Розничная цена, руб.', 'Оптовая цена, руб.', 'Осн. материалы, руб.', 'Вспом. материалы, руб.',
      'Пошив, руб.', 'Раскрой, руб.', 'Декор, руб.', 'Вязание, руб.',
      'Себестоимость, руб.', 'Наценка, руб.', 'Наценка, %', 'Маржинальность, %',
    ];
    tableHtml += '<tr>';
    for (let ci = 0; ci < 6; ci++) tableHtml += '<td>' + escHtml(String(cells[ci])) + '<\/td>';
    for (let ci = 0; ci < POPUP_NUM_FIELDS.length; ci++) {
      const field = POPUP_NUM_FIELDS[ci];
      const rawVal = r[field];
      const style = hbStyleStr(rawVal, field);
      const display = ci >= 10 ? (rawVal ?? '') : nf(rawVal);
      tableHtml += '<td style="' + style + '">' + escHtml(String(display)) + '<\/td>';
    }
    tableHtml += '<\/tr>';
  }

  // Filter config
  const filterFields = [
    { key: 'col_date', label: '\u0414\u0430\u0442\u0430', field: '\u0434\u0430\u0442\u0430 \u0440\u0430\u0441\u0447\u0435\u0442\u0430' },
    { key: 'calc_sign', label: '\u041F\u0440.\u043A\u0430\u043B\u044C\u043A', field: '\u041F\u0440\u0438\u0437\u043D\u0430\u043A \u043A\u0430\u043B\u044C\u043A\u0443\u043B\u044F\u0446\u0438\u0438' },
    { key: 'articul', label: '\u0410\u0440\u0442\u0438\u043A\u0443\u043B', field: '\u0410\u0440\u0442\u0438\u043A\u0443\u043B' },
    { key: 'name', label: '\u041D\u0430\u0438\u043C\u0435\u043D\u043E\u0432\u0430\u043D\u0438\u0435', field: '\u041D\u0430\u0438\u043C\u0435\u043D\u043E\u0432\u0430\u043D\u0438\u0435 \u043C\u043E\u0434\u0435\u043B\u0438' },
    { key: 'task_num', label: '\u2116 \u0437\u0430\u0434\u0430\u043D\u0438\u044F', field: '\u041D\u043E\u043C\u0435\u0440 \u0437\u0430\u0434\u0430\u043D\u0438\u044F \u043F\u0440\u043E\u0438\u0437\u0432\u043E\u0434\u0441\u0442\u0432\u0430' },
    { key: 'release_date', label: '\u0414\u0430\u0442\u0430 \u0432\u044B\u043F\u0443\u0441\u043A\u0430', field: '\u0414\u0430\u0442\u0430 \u0432\u044B\u043F\u0443\u0441\u043A\u0430' },
  ];

  // Build API-level filter HTML (date range, calc_sign multiselect, load button)
  let filterHtml = '<div class="filter-section-api" id="filterApi">';
  filterHtml += '<div class="fi-item"><label>Дата с</label><input type="date" id="f_dateFrom" value="' + escHtml(detailDateFrom.value) + '" class="fi-date" /><\/div>';
  filterHtml += '<div class="fi-item"><label>Дата по</label><input type="date" id="f_dateTo" value="' + escHtml(detailDateTo.value) + '" class="fi-date" /><\/div>';
  filterHtml += '<div class="fi-item"><label>Пр.кальк</label><div class="ms-wrap" style="min-width:100px">';
  filterHtml += '<div class="ms-trigger" onclick="msToggle(\'api_cs\',event)"><span class="ms-label" id="msl_api_cs">—<\/span><span class="ms-arrow">▾<\/span><\/div>';
  filterHtml += '<div class="ms-drop" id="msd_api_cs"><\/div>';
  filterHtml += '<div class="ms-tags" id="mst_api_cs"><\/div><\/div><\/div>';
  filterHtml += '<button id="loadBtn" class="btn-load">Загрузить данные<\/button>';
  filterHtml += '<\/div>';
  // Build client-side column filter HTML (multiselect checkboxes, populated by ap())
  filterHtml += '<div class="filter-section-cols" id="filterCols">';
  for (const ff of filterFields) {
    filterHtml += '<div class="fi-item ms-wrap" data-key="' + ff.key + '"><label>' + ff.label + '<\/label>';
    filterHtml += '<div class="ms-trigger" onclick="msToggle(\'' + ff.key + '\',event)"><span class="ms-label" id="msl_' + ff.key + '">—<\/span><span class="ms-arrow">▾<\/span><\/div>';
    filterHtml += '<div class="ms-drop" id="msd_' + ff.key + '"><\/div>';
    filterHtml += '<div class="ms-tags" id="mst_' + ff.key + '"><\/div><\/div>';
  }
  filterHtml += '<a href="#" class="filter-reset" id="resetFilters">Сбросить фильтры колонок<\/a>';
  filterHtml += '<\/div>';

  // JS to embed in popup (completely self-contained, no toString() serialization)
  const popupScript = `try{
var R=${JSON.stringify(rows)};
var MODEL=${JSON.stringify(detailsModel.value)};
var API_BASE=${JSON.stringify(apiBase.value)};
var FK=${JSON.stringify(filterFields.map(f => f.key))};
var FF=${JSON.stringify(filterFields.map(f => f.field))};
var NF=${JSON.stringify(DETAILS_NUMERIC_FIELDS)};
// Compute per-column ranges for heatmap
var RG={};
for(var fi=0;fi<NF.length;fi++){
  var f=NF[fi],mn=Infinity,mx=-Infinity;
  for(var ri=0;ri<R.length;ri++){
    var nv=Number(R[ri][f]);
    if(!isNaN(nv)){if(nv<mn)mn=nv;if(nv>mx)mx=nv;}
  }
  RG[f]=mn===Infinity?{m:0,M:0}:{m:mn,M:mx};
}
// Multi-select state: for column filters + API calc_sign
var MS_SEL={};
for(var mi=0;mi<FK.length;mi++)MS_SEL[FK[mi]]=[];
var MS_API_CS=[];

function escHtml(s){return String(s).replace(/"/g,"&quot;").replace(/</g,"&lt;").replace(/>/g,"&gt;")}
function gv(r,f){var v=(r[f]||"").toString().trim();if((f==="\\u0434\\u0430\\u0442\\u0430 \\u0440\\u0430\\u0441\\u0447\\u0435\\u0442\\u0430"||f==="\\u0414\\u0430\\u0442\\u0430 \\u0432\\u044B\\u043F\\u0443\\u0441\\u043A\\u0430")&&v.indexOf("T")>=0)v=v.split("T")[0];return v}
function nf(v){if(v==null||v==="")return"\\u2014";var n=Number(v);if(isNaN(n))return String(v);return n.toLocaleString("ru-RU",{minimumFractionDigits:2,maximumFractionDigits:2})}
function hb(v,f){
  var n=Number(v);if(isNaN(n))return"";
  var rg=RG[f];if(!rg||rg.M===rg.m)return"";
  var t=(n-rg.m)/(rg.M-rg.m);
  return"background-color:rgb("+Math.round(240-190*t)+","+Math.round(245-145*t)+","+Math.round(255-35*t)+");text-align:right;";
}

// Multiselect dropdown toggle
function msToggle(k,ev){
  if(ev)ev.stopPropagation();
  var d=document.getElementById("msd_"+k);
  if(!d)return;
  var isVis=d.style.display==="block";
  msCloseAll();
  if(!isVis)d.style.display="block";
}
function msCloseAll(){
  var all=document.querySelectorAll(".ms-drop");
  for(var i=0;i<all.length;i++)all[i].style.display="none";
}
function msUpdateUI(k){
  var sel=k==="api_cs"?MS_API_CS:(MS_SEL[k]||[]);
  var lbl=document.getElementById("msl_"+k);
  var tags=document.getElementById("mst_"+k);
  if(!lbl)return;
  if(sel.length===0){
    lbl.textContent="\\u2014";
    if(tags)tags.innerHTML="";
  }else{
    lbl.textContent=sel.length+" \\u0432\\u044B\\u0431\\u0440\\u0430\\u043D\\u043E";
    if(tags){
      var th="";
      for(var i=0;i<sel.length;i++)th+='<span class="ms-tag">'+escHtml(sel[i])+'<span class="ms-tag-x" data-mskey="'+k+'" data-msval="'+escHtml(sel[i])+'">\\u00D7<\\/span><\\/span>';
      tags.innerHTML=th;
    }
  }
}

// Build table rows
function rt(rr){
  var h="";
  for(var i=0;i<rr.length;i++){
    var r=rr[i];
    h+="<tr>"
      +"<td>"+gv(r,FF[0])+"<\\/td>"
      +"<td>"+(r[FF[1]]||"\\u2014")+"<\\/td>"
      +"<td>"+(r[FF[2]]||"\\u2014")+"<\\/td>"
      +"<td>"+(r[FF[3]]||"\\u2014")+"<\\/td>"
      +"<td>"+(r[FF[4]]||"\\u2014")+"<\\/td>"
      +"<td>"+gv(r,FF[5])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u0420\\u043E\\u0437\\u043D\\u0438\\u0447\\u043D\\u0430\\u044F \\u0446\\u0435\\u043D\\u0430, \\u0440\\u0443\\u0431."],"\\u0420\\u043E\\u0437\\u043D\\u0438\\u0447\\u043D\\u0430\\u044F \\u0446\\u0435\\u043D\\u0430, \\u0440\\u0443\\u0431.")+"\\">"+nf(r["\\u0420\\u043E\\u0437\\u043D\\u0438\\u0447\\u043D\\u0430\\u044F \\u0446\\u0435\\u043D\\u0430, \\u0440\\u0443\\u0431."])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u041E\\u043F\\u0442\\u043E\\u0432\\u0430\\u044F \\u0446\\u0435\\u043D\\u0430, \\u0440\\u0443\\u0431."],"\\u041E\\u043F\\u0442\\u043E\\u0432\\u0430\\u044F \\u0446\\u0435\\u043D\\u0430, \\u0440\\u0443\\u0431.")+"\\">"+nf(r["\\u041E\\u043F\\u0442\\u043E\\u0432\\u0430\\u044F \\u0446\\u0435\\u043D\\u0430, \\u0440\\u0443\\u0431."])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u041E\\u0441\\u043D. \\u043C\\u0430\\u0442\\u0435\\u0440\\u0438\\u0430\\u043B\\u044B, \\u0440\\u0443\\u0431."],"\\u041E\\u0441\\u043D. \\u043C\\u0430\\u0442\\u0435\\u0440\\u0438\\u0430\\u043B\\u044B, \\u0440\\u0443\\u0431.")+"\\">"+nf(r["\\u041E\\u0441\\u043D. \\u043C\\u0430\\u0442\\u0435\\u0440\\u0438\\u0430\\u043B\\u044B, \\u0440\\u0443\\u0431."])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u0412\\u0441\\u043F\\u043E\\u043C. \\u043C\\u0430\\u0442\\u0435\\u0440\\u0438\\u0430\\u043B\\u044B, \\u0440\\u0443\\u0431."],"\\u0412\\u0441\\u043F\\u043E\\u043C. \\u043C\\u0430\\u0442\\u0435\\u0440\\u0438\\u0430\\u043B\\u044B, \\u0440\\u0443\\u0431.")+"\\">"+nf(r["\\u0412\\u0441\\u043F\\u043E\\u043C. \\u043C\\u0430\\u0442\\u0435\\u0440\\u0438\\u0430\\u043B\\u044B, \\u0440\\u0443\\u0431."])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u041F\\u043E\\u0448\\u0438\\u0432, \\u0440\\u0443\\u0431."],"\\u041F\\u043E\\u0448\\u0438\\u0432, \\u0440\\u0443\\u0431.")+"\\">"+nf(r["\\u041F\\u043E\\u0448\\u0438\\u0432, \\u0440\\u0443\\u0431."])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u0420\\u0430\\u0441\\u043A\\u0440\\u043E\\u0439, \\u0440\\u0443\\u0431."],"\\u0420\\u0430\\u0441\\u043A\\u0440\\u043E\\u0439, \\u0440\\u0443\\u0431.")+"\\">"+nf(r["\\u0420\\u0430\\u0441\\u043A\\u0440\\u043E\\u0439, \\u0440\\u0443\\u0431."])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u0414\\u0435\\u043A\\u043E\\u0440, \\u0440\\u0443\\u0431."],"\\u0414\\u0435\\u043A\\u043E\\u0440, \\u0440\\u0443\\u0431.")+"\\">"+nf(r["\\u0414\\u0435\\u043A\\u043E\\u0440, \\u0440\\u0443\\u0431."])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u0412\\u044F\\u0437\\u0430\\u043D\\u0438\\u0435, \\u0440\\u0443\\u0431."],"\\u0412\\u044F\\u0437\\u0430\\u043D\\u0438\\u0435, \\u0440\\u0443\\u0431.")+"\\">"+nf(r["\\u0412\\u044F\\u0437\\u0430\\u043D\\u0438\\u0435, \\u0440\\u0443\\u0431."])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u0421\\u0435\\u0431\\u0435\\u0441\\u0442\\u043E\\u0438\\u043C\\u043E\\u0441\\u0442\\u044C, \\u0440\\u0443\\u0431."],"\\u0421\\u0435\\u0431\\u0435\\u0441\\u0442\\u043E\\u0438\\u043C\\u043E\\u0441\\u0442\\u044C, \\u0440\\u0443\\u0431.")+"\\">"+nf(r["\\u0421\\u0435\\u0431\\u0435\\u0441\\u0442\\u043E\\u0438\\u043C\\u043E\\u0441\\u0442\\u044C, \\u0440\\u0443\\u0431."])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u041D\\u0430\\u0446\\u0435\\u043D\\u043A\\u0430, \\u0440\\u0443\\u0431."],"\\u041D\\u0430\\u0446\\u0435\\u043D\\u043A\\u0430, \\u0440\\u0443\\u0431.")+"\\">"+nf(r["\\u041D\\u0430\\u0446\\u0435\\u043D\\u043A\\u0430, \\u0440\\u0443\\u0431."])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u041D\\u0430\\u0446\\u0435\\u043D\\u043A\\u0430, %"],"\\u041D\\u0430\\u0446\\u0435\\u043D\\u043A\\u0430, %")+"\\">"+nf(r["\\u041D\\u0430\\u0446\\u0435\\u043D\\u043A\\u0430, %"])+"<\\/td>"
      +"<td style=\\""+hb(r["\\u041C\\u0430\\u0440\\u0436\\u0438\\u043D\\u0430\\u043B\\u044C\\u043D\\u043E\\u0441\u0442\u044C, %"],"\\u041C\\u0430\\u0440\\u0436\\u0438\\u043D\\u0430\\u043B\\u044C\\u043D\\u043E\\u0441\u0442\u044C, %")+"\\">"+nf(r["\\u041C\\u0430\\u0440\\u0436\\u0438\\u043D\\u0430\\u043B\\u044C\\u043D\\u043E\\u0441\u0442\u044C, %"])+"<\\/td>"
    +"<\\/tr>";
  }
  return h;
}

// Column filter cascade + render
function ap(){
  // Read current selections from checkboxes
  for(var i=0;i<FK.length;i++){
    var dd=document.getElementById("msd_"+FK[i]);
    if(dd){
      var cbs=dd.querySelectorAll("input[type=checkbox]:checked");
      MS_SEL[FK[i]]=[];
      for(var ci=0;ci<cbs.length;ci++)MS_SEL[FK[i]].push(cbs[ci].value);
    }
  }
  var sel={};for(var i=0;i<FK.length;i++)sel[FK[i]]=MS_SEL[FK[i]];
  // Cascade: restrict each filter's options based on higher-level selections only
  for(var fi=0;fi<FK.length;fi++){
    var fd=R;
    for(var j=0;j<fi;j++){
      var s=sel[FK[j]];
      if(s&&s.length)fd=fd.filter(function(rr){var vv=gv(rr,FF[j]);return s.indexOf(vv)>=0});
    }
    // Rebuild checkbox panel
    var dd=document.getElementById("msd_"+FK[fi]);
    if(dd){
      var opts=[];var seen={};
      for(var ri=0;ri<fd.length;ri++){
        var vv=gv(fd[ri],FF[fi]);
        if(vv&&!seen[vv]){seen[vv]=true;opts.push(vv);}
      }
      opts.sort();
      var curSel=MS_SEL[FK[fi]]||[];
      var h="";
      for(var oi=0;oi<opts.length;oi++){
        var checked=curSel.indexOf(opts[oi])>=0?" checked":"";
        h+='<label><input type="checkbox" value="'+escHtml(opts[oi])+'"'+checked+">"+escHtml(opts[oi])+"<\\/label>";
      }
      dd.innerHTML=h;
      // Clean up MS_SEL — remove values no longer in available options
      var valid={};
      var newCbs=dd.querySelectorAll("input[type=checkbox]");
      for(var nc=0;nc<newCbs.length;nc++)valid[newCbs[nc].value]=true;
      MS_SEL[FK[fi]]=MS_SEL[FK[fi]].filter(function(x){return valid[x]});
    }
  }
  // Filter all data by every selection
  var fd2=R;
  for(var fi=0;fi<FK.length;fi++){
    var s=sel[FK[fi]];
    if(s&&s.length)fd2=fd2.filter(function(rr){var vv=gv(rr,FF[fi]);return s.indexOf(vv)>=0});
  }
  document.getElementById("popupBody").innerHTML=rt(fd2);
  document.getElementById("popupCount").textContent="\\u041D\\u0430\\u0439\\u0434\\u0435\\u043D\\u043E \\u0441\\u0442\\u0440\\u043E\\u043A: "+fd2.length;
  // Update UI labels/tags for all filters
  for(var ui=0;ui<FK.length;ui++)msUpdateUI(FK[ui]);
}

// Populate API calc_sign checkboxes (static options)
(function(){
  var dd=document.getElementById("msd_api_cs");
  if(dd){
    var CS_DESC={};
    CS_DESC["\\u041F\\u041A\\u041F\\u0421\\u0421"]="\\u043D\\u043E\\u0432\\u0430\\u044F \\u0440\\u0430\\u0437\\u0440\\u0430\\u0431\\u043E\\u0442\\u043A\\u0430";
    CS_DESC["\\u041A\\u041F\\u0421\\u0421"]="\\u043F\\u043B\\u0430\\u043D\\u043E\\u0432\\u0430\\u044F \\u043A\\u0430\\u043B\\u044C\\u043A\\u0443\\u043B\\u044F\\u0446\\u0438\\u044F";
    CS_DESC["\\u041F\\u0424\\u041A\\u0421\\u0421"]="\\u0444\\u0430\\u043A\\u0442\\u0438\\u0447\\u0435\\u0441\\u043A\\u0430\\u044F \\u0440\\u0430\\u0441\\u0446\\u0435\\u043D\\u043A\\u0430 \\u0430\\u0441\\u0441\\u043E\\u0440\\u0442\\u0438\\u043C\\u0435\\u043D\\u0442\\u0430";
    CS_DESC["\\u0424\\u041A\\u0421\\u0421"]="\\u0438\\u0441\\u0442\\u043E\\u0440\\u0438\\u044F \\u0441\\u0435\\u0431\\u0435\\u0441\\u0442\\u043E\\u0438\\u043C\\u043E\\u0441\\u0442\\u0438";
    var csOpts=["\\u041F\\u041A\\u041F\\u0421\\u0421","\\u041A\\u041F\\u0421\\u0421","\\u041F\\u0424\\u041A\\u0421\\u0421","\\u0424\\u041A\\u0421\\u0421"];
    var h="";
    for(var oi=0;oi<csOpts.length;oi++){
      var desc=CS_DESC[csOpts[oi]]?' title="'+CS_DESC[csOpts[oi]]+'"':"";
      h+='<label'+desc+'><input type="checkbox" value="'+csOpts[oi]+'">'+csOpts[oi]+"<\\/label>";
    }
    dd.innerHTML=h;
  }
})();

// Delegate change on column filter checkboxes
document.getElementById("filterCols").addEventListener("change",function(e){
  if(e.target&&e.target.type==="checkbox"){
    var dd=e.target.closest(".ms-drop");
    if(!dd)return;
    var k=dd.id.replace("msd_","");
    var cbs=dd.querySelectorAll("input[type=checkbox]:checked");
    MS_SEL[k]=[];
    for(var ci=0;ci<cbs.length;ci++)MS_SEL[k].push(cbs[ci].value);
    msUpdateUI(k);
    ap();
  }
});

// Delegate change on API calc_sign checkboxes
document.getElementById("filterApi").addEventListener("change",function(e){
  if(e.target&&e.target.type==="checkbox"){
    var dd=e.target.closest(".ms-drop");
    if(!dd||dd.id!=="msd_api_cs")return;
    var cbs=dd.querySelectorAll("input[type=checkbox]:checked");
    MS_API_CS=[];
    for(var ci=0;ci<cbs.length;ci++)MS_API_CS.push(cbs[ci].value);
    msUpdateUI("api_cs");
  }
});

// Tag remove: delegate click on × buttons
document.addEventListener("click",function(e){
  var x=e.target.closest(".ms-tag-x");
  if(x){
    var k=x.getAttribute("data-mskey");
    var v=x.getAttribute("data-msval");
    if(k==="api_cs"){
      MS_API_CS=MS_API_CS.filter(function(xx){return xx!==v});
      var dd=document.getElementById("msd_api_cs");
      if(dd){var cbs=dd.querySelectorAll("input[type=checkbox]");for(var ci=0;ci<cbs.length;ci++){if(cbs[ci].value===v)cbs[ci].checked=false;}}
      msUpdateUI("api_cs");
    }else{
      MS_SEL[k]=MS_SEL[k].filter(function(xx){return xx!==v});
      var dd=document.getElementById("msd_"+k);
      if(dd){var cbs=dd.querySelectorAll("input[type=checkbox]");for(var ci=0;ci<cbs.length;ci++){if(cbs[ci].value===v)cbs[ci].checked=false;}}
      msUpdateUI(k);
      ap();
    }
  }
});

// Close dropdowns on outside click (delegated)
document.addEventListener("click",function(e){
  if(!e.target.closest||!e.target.closest(".ms-wrap"))msCloseAll();
});

// Load fresh data from API
async function loadData(){
  try{
    var df=document.getElementById("f_dateFrom").value;
    var dt=document.getElementById("f_dateTo").value;
    var cs=MS_API_CS;
    var body={model:MODEL};
    if(df)body.date_from=df;
    if(dt)body.date_to=dt;
    if(cs.length)body.calc_sign=cs;
    var resp=await fetch(API_BASE+"/api/cost/details",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});
    if(!resp.ok)throw new Error("HTTP "+resp.status);
    var json=await resp.json();
    R=json.data||[];
    // Recompute heatmap ranges
    for(var fi=0;fi<NF.length;fi++){
      var f=NF[fi],mn=Infinity,mx=-Infinity;
      for(var ri=0;ri<R.length;ri++){var nv=Number(R[ri][f]);if(!isNaN(nv)){if(nv<mn)mn=nv;if(nv>mx)mx=nv;}}
      RG[f]=mn===Infinity?{m:0,M:0}:{m:mn,M:mx};
    }
    // Reset all selections
    for(var mi=0;mi<FK.length;mi++)MS_SEL[FK[mi]]=[];
    MS_API_CS=[];
    for(var mi=0;mi<FK.length;mi++){var dd=document.getElementById("msd_"+FK[mi]);if(dd){var cbs=dd.querySelectorAll("input[type=checkbox]");for(var ci=0;ci<cbs.length;ci++)cbs[ci].checked=false;}}
    var dd2=document.getElementById("msd_api_cs");if(dd2){var cbs2=dd2.querySelectorAll("input[type=checkbox]");for(var ci=0;ci<cbs2.length;ci++)cbs2[ci].checked=false;}
    ap();
    msUpdateUI("api_cs");
  }catch(e){
    console.error("[cost-popup] load failed",e);
    alert("\\u041E\\u0448\\u0438\\u0431\\u043A\\u0430 \\u0437\\u0430\\u0433\\u0440\\u0443\\u0437\\u043A\\u0438 \\u0434\\u0430\\u043D\\u043D\\u044B\\u0445");
  }
}

// Initial render
ap();

// Reset handler
document.getElementById("resetFilters").onclick=function(){
  for(var mi=0;mi<FK.length;mi++)MS_SEL[FK[mi]]=[];
  for(var mi=0;mi<FK.length;mi++){var dd=document.getElementById("msd_"+FK[mi]);if(dd){var cbs=dd.querySelectorAll("input[type=checkbox]");for(var ci=0;ci<cbs.length;ci++)cbs[ci].checked=false;}}
  for(var ui=0;ui<FK.length;ui++)msUpdateUI(FK[ui]);
  ap();
  return false;
};

// Load button
document.getElementById("loadBtn").onclick=loadData;
}catch(e){console.error("[cost-popup]",e)}`;

  const html =
    '<!DOCTYPE html>' +
    '<html lang="ru"><head><meta charset="utf-8">' +
    '<title>\u0414\u0435\u0442\u0430\u043B\u0438\u0437\u0430\u0446\u0438\u044F: ' + escHtml(model) + '<\/title>' +
    '<style>' +
    '*{box-sizing:border-box;margin:0;padding:0}' +
    'body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;padding:20px;background:#f5f5f5}' +
    '.container{max-width:100%;margin:0 auto;background:#fff;border-radius:8px;box-shadow:0 2px 12px rgba(0,0,0,.1)}' +
    '.header{padding:16px 24px;border-bottom:1px solid #eee;display:flex;justify-content:space-between;align-items:center}' +
    '.header h1{font-size:18px}' +
    '.filters{padding:0;border-bottom:1px solid #eee}' +
    '.filter-section-api{padding:10px 24px;display:flex;gap:10px;align-items:flex-end;flex-wrap:wrap;background:#f0f4ff;border-bottom:1px solid #dde4f0}' +
    '.filter-section-cols{padding:10px 24px;display:flex;gap:10px;align-items:flex-start;flex-wrap:wrap;background:#fafafa}' +
    '.fi-item{display:flex;flex-direction:column;gap:2px}' +
    '.fi-item label{font-size:10px;color:#555;font-weight:600}' +
    '.fi-item select{min-width:120px;max-width:180px;border:1px solid #ddd;border-radius:4px;background:#fff;font-size:10px;padding:2px 4px;height:24px}' +
    'input.fi-date{height:24px;border:1px solid #ddd;border-radius:4px;padding:2px 6px;font-size:11px;background:#fff}' +
    'select.fi-cs{min-width:100px;height:24px}' +
    '.btn-load{height:28px;padding:0 14px;border:1px solid #4a6cf7;border-radius:4px;background:#4a6cf7;color:#fff;font-size:11px;font-weight:600;cursor:pointer;white-space:nowrap}' +
    '.btn-load:hover{background:#3b5de7}' +
    '.filter-reset{font-size:11px;color:#888;padding-top:8px;cursor:pointer;white-space:nowrap;text-decoration:none}' +
    '.filter-reset:hover{color:#333}' +
    '.ms-wrap{position:relative;display:inline-block;min-width:120px;max-width:180px}' +
    '.ms-trigger{display:flex;align-items:center;justify-content:space-between;border:1px solid #ddd;border-radius:4px;background:#fff;font-size:10px;padding:3px 6px;height:24px;cursor:pointer;gap:4px;user-select:none}' +
    '.ms-trigger:hover{border-color:#aaa}' +
    '.ms-label{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:#555}' +
    '.ms-arrow{font-size:8px;color:#999}' +
    '.ms-drop{display:none;position:absolute;top:100%;left:0;right:0;z-index:999;background:#fff;border:1px solid #ddd;border-radius:4px;max-height:200px;overflow-y:auto;margin-top:2px;box-shadow:0 2px 8px rgba(0,0,0,.12)}' +
    '.ms-drop label{display:block;padding:4px 8px;font-size:11px;cursor:pointer;white-space:nowrap}' +
    '.ms-drop label:hover{background:#f0f4ff}' +
    '.ms-drop input[type=checkbox]{margin-right:6px}' +
    '.ms-tags{display:flex;flex-wrap:wrap;gap:2px;margin-top:2px}' +
    '.ms-tag{display:inline-flex;align-items:center;gap:2px;background:#e8eefb;border-radius:3px;padding:1px 5px;font-size:9px;color:#333;max-width:120px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}' +
    '.ms-tag-x{margin-left:2px;cursor:pointer;font-size:11px;color:#888;line-height:1}' +
    '.ms-tag-x:hover{color:#c00}' +
    '.content{padding:16px 24px;overflow-x:auto}' +
    'table{width:100%;border-collapse:collapse;font-size:12px;white-space:nowrap}' +
    'th{background:#f8f9fa;border-bottom:2px solid #dee2e6;padding:8px;text-align:center;font-weight:700;font-size:11px;position:sticky;top:0}' +
    'td{padding:6px 8px;border-bottom:1px solid #eee;text-align:right}' +
    'td:nth-child(-n+6){text-align:left}' +
    'tbody tr:hover{background:#f1f3f5}' +
    '.count{padding:8px 24px;font-size:12px;color:#6c757d;border-top:1px solid #eee}' +
    '<\/style><\/head><body>' +
    '<div class="container">' +
      '<div class="header"><h1>\u0414\u0435\u0442\u0430\u043B\u0438\u0437\u0430\u0446\u0438\u044F: ' + escHtml(model) + '<\/h1><\/div>' +
      '<div class="filters">' + filterHtml + '<\/div>' +
      '<div class="content">' +
        '<table><thead><tr>' +
          '<th>\u0414\u0430\u0442\u0430<\/th><th>\u041F\u0440.\u043A\u0430\u043B\u044C\u043A<\/th><th>\u0410\u0440\u0442\u0438\u043A\u0443\u043B<\/th><th>\u041D\u0430\u0438\u043C\u0435\u043D\u043E\u0432\u0430\u043D\u0438\u0435<\/th><th>\u2116 \u0437\u0430\u0434\u0430\u043D\u0438\u044F<\/th><th>\u0414\u0430\u0442\u0430 \u0432\u044B\u043F\u0443\u0441\u043A\u0430<\/th>' +
          '<th>\u0420\u043E\u0437\u043D\u0438\u0446\u0430<\/th><th>\u041E\u043F\u0442<\/th><th>\u041E\u0441\u043D.\u043C\u0430\u0442<\/th><th>\u0412\u0441\u043F\u043E\u043C.<\/th><th>\u041F\u043E\u0448\u0438\u0432<\/th><th>\u0420\u0430\u0441\u043A\u0440\u043E\u0439<\/th><th>\u0414\u0435\u043A\u043E\u0440<\/th><th>\u0412\u044F\u0437\u0430\u043D\u0438\u0435<\/th>' +
          '<th>\u0421\u0435\u0431\u0435\u0441\u0442.<\/th><th>\u041D\u0430\u0446\u0435\u043D\u043A\u0430<\/th><th>\u041D\u0430\u0446\u0435\u043D\u043A\u0430%<\/th><th>\u041C\u0430\u0440\u0436\u0430%<\/th>' +
        '<\/tr><\/thead>' +
        '<tbody id="popupBody">' + tableHtml + '<\/tbody><\/table>' +
      '<\/div>' +
      '<div class="count" id="popupCount">\u041D\u0430\u0439\u0434\u0435\u043D\u043E \u0441\u0442\u0440\u043E\u043A: ' + rows.length + '<\/div>' +
    '<\/div>' +
    '<script>' + popupScript + '<\/script>' +
    '<\/body><\/html>';

  w.document.write(html);
  w.document.close();
}

// ── Cache refresh & status ──────────────────────────────────────────────────

const cacheRefreshing = ref(false);
const cacheInfo = ref<{ refreshed_at: string | null; row_count: number; is_refreshing: boolean; error_message: string | null } | null>(null);

async function loadCacheStatus() {
  try {
    cacheInfo.value = await $fetch<typeof cacheInfo.value>(
      `${apiBase.value}/api/cost/cache-status`,
      { headers: fetchHeaders.value }
    );
  } catch (e: any) {
    console.error("[cost] cache-status failed", e);
  }
}

const cacheNotification = ref('');

function showCacheNotification(msg: string) {
  cacheNotification.value = msg;
  setTimeout(() => { cacheNotification.value = ''; }, 4000);
}

/** Запустить обновление указанной ручкой и дождаться его в опросе статуса. */
async function startCacheRefresh(endpoint: string) {
  try {
    const result = await $fetch<{ status: string; message?: string }>(
      `${apiBase.value}/api/cost/${endpoint}`,
      { method: "POST", headers: fetchHeaders.value }
    );
    if (result.status === "already_refreshing") {
      showCacheNotification(result.message || 'Обновление уже запущено');
      return;
    }
    if (result.status === "mock") return;
    if (result.message) showCacheNotification(result.message);

    cacheRefreshing.value = true;
    // Poll until refresh completes
    const poll = async () => {
      while (cacheRefreshing.value) {
        await new Promise((r) => setTimeout(r, 5000));
        try {
          const s = await $fetch<typeof cacheInfo.value>(
            `${apiBase.value}/api/cost/cache-status`,
            { headers: fetchHeaders.value }
          );
          cacheInfo.value = s;
          if (!s?.is_refreshing) {
            cacheRefreshing.value = false;
            if (s?.error_message) {
              showCacheNotification('Обновление завершилось с ошибкой — см. статус кеша');
            }
          }
        } catch {
          cacheRefreshing.value = false;
        }
      }
    };
    poll();
  } catch (e: any) {
    console.error(`[cost] ${endpoint} failed`, e);
    // Код ответа обязателен в тексте. Без него «не удалось запустить обновление»
    // одинаково выглядит и когда прав нет (403), и когда ручки нет в
    // задеплоенном образе (404), и когда сервис перезапускается (502) — а
    // лечится это тремя разными способами.
    const status = e?.statusCode || e?.status || e?.response?.status;
    const detail = e?.data?.detail || e?.data?.message || e?.message;
    showCacheNotification(
      `Не удалось запустить обновление${status ? ` (HTTP ${status})` : ''}` +
      `${detail ? `: ${detail}` : ''}`
    );
    cacheRefreshing.value = false;
  }
}

function refreshCache() {
  return startCacheRefresh('refresh-cache');
}

/** Полное обновление: TRUNCATE + перезалив всей CostHistory.
 *
 * Спрашиваем подтверждение не для вида: операция идёт ~25 минут и держит
 * ACCESS EXCLUSIVE на cost_data_cache до конца транзакции — все запросы раздела
 * к кэшу на это время встают. Нужна для чистки (дубли, смена набора колонок),
 * для свежести достаточно обычной кнопки рядом.
 */
function refreshCacheFull() {
  if (!confirm(
    'Полное обновление кеша: таблица очищается и перезаливается из источника целиком.\n\n' +
    '· идёт около 25 минут на ~1 млн строк;\n' +
    '· на это время запросы раздела к кешу будут ждать;\n' +
    '· согласованные версии цен и применённые наборы накладываются заново автоматически.\n\n' +
    'Обычной кнопке «Обновить кеш» это не нужно — она берёт последние 2 месяца. Запустить полное?'
  )) return;
  return startCacheRefresh('refresh-cache-full');
}

// ── Price levels & save ─────────────────────────────────────────────────────

const priceLevels = ref<PriceLevel[]>([]);

/** Ключ цены/комментария: модель + артикул + план + признак калькуляции.
 *
 * Ввод пользователя (цены РФ/КЗ/УЗ, комментарий, выбранная наценка) раньше
 * лежал в объектах, где ключом был НОМЕР строки в `allAggregated`. Любая смена
 * фильтра перезагружает выдачу — состав и порядок строк меняются, а номера в
 * этих объектах остаются прежними и начинают указывать на чужие строки. Отсюда
 * жалобы 24.08.2026: комментарий «испарился» из своей строки, «появился в
 * другой модели» и «перекочевал к другому БМ». Хуже того, `changedRows` тоже
 * держал номера, поэтому сохранение могло записать правку в чужой артикул.
 *
 * Состав ключа — ровно `uq_pending_row` из миграции 0005, то есть то, по чему
 * бэкенд действительно хранит цену и комментарий (`ON CONFLICT` в
 * `upsert_pending_changes_batch`). Номер задания в ключ НЕ входит, хотя
 * агрегация главной таблицы группирует и по нему: у одной калькуляции может
 * быть несколько заданий, а цена и комментарий у них общие — иначе UI показывал
 * бы по заданиям разные значения там, где в БД лежит одна запись. Тем же
 * составом синхронизирует строки `findSiblingRows`. Ключ ПЭО шире (там есть
 * задание) — это отдельная сущность, см. `peoKeyFields`.
 *
 * Разделитель U+0001 взят потому, что в данных источника он не встречается:
 * склейка без разделителя дала бы коллизии на соседних значениях. */
const calcRowKey = (row: any): string => [
  (row?.['Модель'] ?? '').toString().trim(),
  (row?.['Артикул'] ?? '').toString().trim(),
  (row?.['PLAN_ID'] ?? '').toString().trim(),
  (row?.['Признак калькуляции'] ?? '').toString().trim(),
].join('');

/** Несохранённые правки: ключ калькуляции → строка выдачи.
 *
 * Строка хранится вместе с ключом, чтобы сохранение собирало payload даже
 * когда правленая калькуляция ушла из текущей выдачи под фильтр. */
const changedRows = reactive<Map<string, any>>(new Map());
const saving = ref(false);

function discardChanges() {
  changedRows.clear();
  loadData();
}

/** Уникальные розничные цены из справочника уровней цен (для datalist). */
const uniqueRetailPrices = computed(() => {
  const prices = new Set(priceLevels.value.map(l => l.price_type3));
  return Array.from(prices).sort((a, b) => a - b);
});

/** Выбранное значение «Розничная наценка» по строке (ключ калькуляции → value). */
const markupSelections = reactive<Record<string, string>>({});

/** Реактивные значения цен РФ, КЗ, УЗ по строке (ключ калькуляции → value). Заполняются из price_type4/5/6 при выборе наценки, переопределяются пользователем. */
const priceRF = reactive<Record<string, number>>({});
const priceKZ = reactive<Record<string, number>>({});
const priceUZ = reactive<Record<string, number>>({});

const editingVersion = ref<{
  model: string;
  articul: string;
  calc_sign: string;
  plan_id: string;
  date: string;
  rows: any[];
  versions: any[];
  selectedVersionId: number | null;
  isEditing: boolean;
  version_id: number | null;
  _locked: boolean;
} | null>(null);
const savingDraft = ref(false);
const submittingDraft = ref(false);
const loadingVersionData = ref(false);

/** Индекс строки, для которой открыт dropdown типа (-1 = ни одна). */
const editingTypeCell = ref<number>(-1);

/** Комментарий по строке (ключ калькуляции → текст). */
const comments = reactive<Record<string, string>>({});

async function loadPriceLevels() {
  try {
    priceLevels.value = await $fetch<PriceLevel[]>(
      `${apiBase.value}/api/cost/price-levels`,
      { headers: fetchHeaders.value }
    );
  } catch (e: any) {
    console.error("[cost] price-levels load failed", e);
  }
}

/** Derive RUB→USD exchange rate from a row. Tries wholesale first, then retail. */
function _deriveRate(row: any): number {
  const rub = Number(row["avg_Отпускная цена по уровню, руб"] || 0);
  const usd = Number(row["avg_Отпускная цена по уровню, USD."] || 0);
  if (rub > 0 && usd > 0) return rub / usd;
  const rubR = Number(row["avg_Розничная цена по уровню, руб."] || 0);
  const usdR = Number(row["avg_Розничная цена по уровню, USD."] || 0);
  if (rubR > 0 && usdR > 0) return rubR / usdR;
  return 0;
}

/** Вычислить «Розничная наценка» из собственных данных строки (retail / wholesale / НДС). */
function computeMarkupFromRow(row: any): string {
  const retailPrice = Number(row['avg_Розничная цена по уровню, руб.']);
  const wholesalePrice = Number(row['avg_Отпускная цена по уровню, руб']);
  const avgVat = Number(row['avg_Ставка НДС'] || 0);
  if (!retailPrice || !wholesalePrice || !avgVat || wholesalePrice <= 0) return '';
  const markupPct = ((retailPrice / (100 + avgVat) * 100) / wholesalePrice - 1) * 100;
  return markupPct.toFixed(2);
}

/** Рассчитать варианты «Розничная наценка» для строки на основе выбранной розничной цены и средней ставки НДС. */
function getMarkupOptions(row: any): { value: string; label: string }[] {
  const retailPrice = Number(row['avg_Розничная цена по уровню, руб.']);
  const avgVat = Number(row['avg_Ставка НДС'] || 0);
  if (!retailPrice || isNaN(retailPrice) || retailPrice <= 0) return [];

  const resultsMap = new Map<string, { value: string; label: string }>();
  for (const level of priceLevels.value) {
    if (String(level.price_type3) !== String(retailPrice)) continue;
    if (!level.price_type1 || level.price_type1 <= 0) continue;
    const markupPct = ((retailPrice / (100 + avgVat) * 100) / level.price_type1 - 1) * 100;
    const value = markupPct.toFixed(2);
    if (!resultsMap.has(value)) {
      resultsMap.set(value, {
        value,
        label: `${markupPct.toFixed(1)}%`,
      });
    }
  }

  // Если ни один уровень цен не совпал — вычисляем наценку из собственных данных строки
  if (resultsMap.size === 0) {
    const markup = computeMarkupFromRow(row);
    if (markup && !resultsMap.has(markup)) {
      resultsMap.set(markup, {
        value: markup,
        label: `${Number(markup).toFixed(1)}%`,
      });
    }
  }
  return Array.from(resultsMap.values());
}

/** Целевая наценка по category level01: Мальчикам/Девочкам/Ясли → 30%, остальное → 40%.
 * (Ясли добавлены 19.08.2026 по просьбе заказчика.) */
const TARGET_MARKUP_30 = new Set(['мальчикам', 'девочкам', 'ясли']);
function getTargetMarkup(level01: string): number {
  return TARGET_MARKUP_30.has((level01 || '').trim().toLowerCase()) ? 30 : 40;
}

/** Найти наценку из списка, максимально близкую к целевой. */
function findClosestMarkup(
  options: { value: string; label: string }[],
  targetPct: number
): { value: string; label: string } {
  return options.reduce((best, opt) => {
    const diff = Math.abs(parseFloat(opt.value) - targetPct);
    const bestDiff = Math.abs(parseFloat(best.value) - targetPct);
    return diff < bestDiff ? opt : best;
  });
}

/** Обработчики ввода цен РФ, КЗ, УЗ.
 *
 * Принимают саму строку, а не её номер: номер живёт только внутри текущей
 * выдачи и после смены фильтра указывает на чужую калькуляцию. */
const onPriceRFInput = (row: any, value: string) => {
  if (!row || isRowLocked(row)) return;
  const v = parseFloat(value);
  priceRF[calcRowKey(row)] = isNaN(v) ? 0 : v;
  changedRows.set(calcRowKey(row), row);
};
const onPriceKZInput = (row: any, value: string) => {
  if (!row || isRowLocked(row)) return;
  const v = parseFloat(value);
  priceKZ[calcRowKey(row)] = isNaN(v) ? 0 : v;
  changedRows.set(calcRowKey(row), row);
};
const onPriceUZInput = (row: any, value: string) => {
  if (!row || isRowLocked(row)) return;
  const v = parseFloat(value);
  priceUZ[calcRowKey(row)] = isNaN(v) ? 0 : v;
  changedRows.set(calcRowKey(row), row);
};

const onCommentInput = (row: any, value: string) => {
  if (!row || isRowLocked(row)) return;
  comments[calcRowKey(row)] = value || "";
  changedRows.set(calcRowKey(row), row);
};

// ── Цены предыдущих этапов калькулирования ──────────────────────────────────
// ПКПСС — первый этап, сравнивать не с чем. На КПСС показываем цены ПКПСС, на
// ПФКСС — КПСС и ПКПСС. Сопоставление идёт по ключу материала, поэтому колонки
// живут прямо в таблице расчёта (закладки не нужны, и ввод в режиме
// редактирования ничем не перебивается). Состав материалов между этапами
// совпадает не полностью — непарные строки предыдущего этапа показываем отдельным
// блоком под таблицей, иначе они бы просто потерялись.

type StagePrices = {
  stage: string;
  scope: string;
  rows: any[];
  byKey: Map<string, any>;
};

const stagePrices = ref<StagePrices[]>([]);
const stagePricesLoading = ref(false);

/** Ключ материала: те же пять полей, что у наборов цен по плану. */
const stageMatKey = (r: any): string =>
  ['Наименование', 'артикул материала', 'свойство1', 'свойство2', 'свойство3']
    .map((f) => (r?.[f] ?? '').toString().trim())
    .join('');

const stageColHint = (sp: StagePrices): string => {
  const scope = sp.scope === 'model+articul'
    ? 'по модели и артикулу'
    : 'по модели, артикулу, плану и заданию';
  // Берём цену из кэша, то есть фактическую цену расчёта: применённый набор цен
  // по плану и активная версия (pending/approved) в неё уже наложены, черновики
  // версий — нет. Это то же значение, что показывает главная таблица.
  return `Цена материала на этапе ${sp.stage} (${scope}) — как в расчёте, `
    + 'с учётом применённого набора цен и активной версии; '
    + '«—» — на том этапе такого материала не было';
};

const stagePriceOf = (sp: StagePrices, vr: any): number | null => {
  const hit = sp.byKey.get(stageMatKey(vr));
  const v = hit ? Number(hit.price_rub) : NaN;
  return Number.isFinite(v) ? v : null;
};

/** Насколько текущая цена отличается от цены этапа, в процентах.
 *
 * Пустая текущая цена — это «цены нет», а не ноль: иначе строка без цены
 * показывала бы −100 % к прошлому этапу. */
const stagePriceDelta = (sp: StagePrices, vr: any): number | null => {
  const prev = stagePriceOf(sp, vr);
  const raw = vr['цена материала, руб.'];
  if (raw === null || raw === undefined || raw === '') return null;
  const cur = Number(raw);
  if (prev === null || !Number.isFinite(cur) || prev === 0) return null;
  const delta = ((cur - prev) / prev) * 100;
  return Math.abs(delta) < 0.05 ? null : delta;
};

/** Материалы предыдущего этапа, которых в текущем расчёте нет. */
const stageOrphans = computed(() => {
  const ev = editingVersion.value;
  if (!ev) return [] as { stage: string; rows: any[] }[];
  const present = new Set(ev.rows.map(stageMatKey));
  return stagePrices.value
    .map((sp) => ({ stage: sp.stage, rows: sp.rows.filter((r) => !present.has(stageMatKey(r))) }))
    .filter((s) => s.rows.length);
});

async function loadStagePrices(row: any) {
  stagePrices.value = [];
  const model = (row['Модель'] ?? '').toString().trim();
  const articul = (row['Артикул'] ?? '').toString().trim();
  const calcSign = (row['Признак калькуляции'] ?? '').toString().trim();
  if (!model || !articul || !calcSign) return;
  stagePricesLoading.value = true;
  try {
    const params = new URLSearchParams({ model, articul, calc_sign: calcSign });
    const plan = (row['PLAN_ID'] ?? '').toString().trim();
    const task = (row['Номер задания производства'] ?? '').toString().trim();
    if (plan) params.set('plan_id', plan);
    if (task) params.set('task_number', task);
    const res = await $fetch<{ stages: { stage: string; scope: string; rows: any[] }[] }>(
      `${apiBase.value}/api/cost/calc-stage-prices?${params.toString()}`,
      { headers: fetchHeaders.value },
    );
    stagePrices.value = (res.stages || []).map((s) => ({
      ...s,
      byKey: new Map(s.rows.map((r) => [stageMatKey(r), r])),
    }));
  } catch (e: any) {
    // Цены прошлых этапов — справочная информация: если не отдались, редактор
    // всё равно должен работать.
    console.error('[cost] load stage prices failed', e);
    stagePrices.value = [];
  } finally {
    stagePricesLoading.value = false;
  }
}

const openVersionEditor = async (row: any) => {
  const r = row;
  if (!r) return;
  // Номер задания входит в ключ версии (миграция 0032): главная таблица
  // группирует с ним, поэтому и версия должна относиться к конкретному заданию,
  // а не ко всем заданиям модели+артикула в плане. У ПКПСС задания в источнике
  // нет — там уходит пустая строка, и ключ вырождается, как и раньше.
  const params = new URLSearchParams({
    model: r['Модель'] || '',
    articul: r['Артикул'] || '',
    calc_sign: r['Признак калькуляции'] || '',
    plan_id: r['PLAN_ID'] || '',
    date: r['дата расчета'] || '',
    task_number: r['Номер задания производства'] || '',
  });
  try {
    const [rawResp, versionsResp] = await Promise.all([
      $fetch<{ version_id: number | null; rows: any[] }>(
        `${apiBase.value}/api/cost/raw-data?${params}`,
        { headers: fetchHeaders.value }
      ),
      $fetch<any[]>(
        `${apiBase.value}/api/cost/versions?${params}`,
        { headers: fetchHeaders.value }
      ),
    ]);
    const versions = versionsResp || [];
    // Редактор всегда открывается на текущей pending-версии, если есть —
    // иначе на "Исходных данных" (raw-data сам решит, отдать ли замороженный
    // 'original'-снимок или живой cost_data_cache).
    const pending = versions.find((v: any) => v.status === 'pending');
    let initialRows = rawResp.rows || [];
    let selectedVersionId: number | null = null;
    if (pending) {
      const verData = await $fetch<{ rows: any[] }>(
        `${apiBase.value}/api/cost/version-rows/${pending.id}`,
        { headers: fetchHeaders.value }
      );
      initialRows = verData.rows || [];
      selectedVersionId = pending.id;
    }
    editingVersion.value = {
      model: r['Модель'],
      articul: r['Артикул'],
      calc_sign: r['Признак калькуляции'] || '',
      plan_id: r['PLAN_ID'] || '',
      date: r['дата расчета'] || '',
      task_number: r['Номер задания производства'] || '',
      rows: initialRows.map((rr: any) => normalizeVersionRow(rr)),
      versions,
      selectedVersionId,
      isEditing: false,
      version_id: null,
      _locked: isRowLocked(r),
    };
    // Справочные цены прошлых этапов — отдельным запросом и без await в общей
    // цепочке: редактор открывается сразу, колонки появляются по готовности.
    void loadStagePrices(r);
  } catch (e: any) {
    console.error('[cost] load version editor failed', e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  }
};

const refreshVersions = async () => {
  if (!editingVersion.value) return;
  const ev = editingVersion.value;
  const params = new URLSearchParams({
    model: ev.model,
    articul: ev.articul,
    calc_sign: ev.calc_sign,
    plan_id: ev.plan_id,
    date: ev.date,
    task_number: ev.task_number || '',
  });
  try {
    const versionsResp = await $fetch<any[]>(
      `${apiBase.value}/api/cost/versions?${params}`,
      { headers: fetchHeaders.value }
    );
    ev.versions = versionsResp || [];
  } catch (e) {
    console.error('[cost] refresh versions failed', e);
  }
};

const selectRawData = async () => {
  if (!editingVersion.value) return;
  const ev = editingVersion.value;
  ev.selectedVersionId = null;
  ev.isEditing = false;
  ev.version_id = null;
  const params = new URLSearchParams({
    model: ev.model,
    articul: ev.articul,
    calc_sign: ev.calc_sign,
    plan_id: ev.plan_id,
    date: ev.date,
    task_number: ev.task_number || '',
  });
  loadingVersionData.value = true;
  try {
    const rawResp = await $fetch<{ version_id: number | null; rows: any[] }>(
      `${apiBase.value}/api/cost/raw-data?${params}`,
      { headers: fetchHeaders.value }
    );
    ev.rows = (rawResp.rows || []).map((rr: any) => normalizeVersionRow(rr));
  } catch (e: any) {
    console.error('[cost] load raw data failed', e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    loadingVersionData.value = false;
  }
};

const selectVersion = async (versionId: number) => {
  if (!editingVersion.value || loadingVersionData.value) return;
  const ev = editingVersion.value;
  ev.selectedVersionId = versionId;
  ev.isEditing = false;
  ev.version_id = null;
  loadingVersionData.value = true;
  try {
    const data = await $fetch<{ version_id: number; rows: any[] }>(
      `${apiBase.value}/api/cost/version-rows/${versionId}`,
      { headers: fetchHeaders.value }
    );
    ev.rows = (data.rows || []).map((rr: any) => normalizeVersionRow(rr));
  } catch (e: any) {
    console.error('[cost] load version rows failed', e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    loadingVersionData.value = false;
  }
};

const onVersionSelectChange = (event: Event) => {
  editingTypeCell.value = -1;
  const value = (event.target as HTMLSelectElement).value;
  if (value === '__raw__') {
    selectRawData();
  } else {
    selectVersion(parseInt(value, 10));
  }
};

const currentVersionStatus = computed(() => {
  if (!editingVersion.value) return '';
  if (editingVersion.value.selectedVersionId === null) return 'Исходные данные';
  const v = editingVersion.value.versions.find(vv => vv.id === editingVersion.value!.selectedVersionId);
  return v ? v.status : '';
});

const startEditing = () => {
  if (!editingVersion.value) return;
  editingVersion.value.isEditing = true;
  editingTypeCell.value = -1;
  if (editingVersion.value.selectedVersionId !== null) {
    editingVersion.value.version_id = editingVersion.value.selectedVersionId;
  } else {
    editingVersion.value.version_id = null;
  }
};

const cancelEditing = async () => {
  if (!editingVersion.value) return;
  editingVersion.value.isEditing = false;
  editingTypeCell.value = -1;
  if (editingVersion.value.selectedVersionId !== null) {
    await selectVersion(editingVersion.value.selectedVersionId);
  } else {
    await selectRawData();
  }
};

const saveDraft = async () => {
  if (!editingVersion.value) return;
  const ev = editingVersion.value;
  savingDraft.value = true;
  try {
    if (ev.version_id !== null) {
      const resp = await fetch(`${apiBase.value}/api/cost/save-calculation-draft`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json', ...fetchHeaders.value},
        body: JSON.stringify({
          version_id: ev.version_id,
          rows: ev.rows,
        }),
      });
      if (!resp.ok) {
        const errData = await resp.json().catch(() => ({}));
        throw new Error(errData?.detail || `HTTP ${resp.status}`);
      }
      alert('Черновик сохранён');
    } else {
      const resp = await fetch(`${apiBase.value}/api/cost/create-version`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json', ...fetchHeaders.value},
        body: JSON.stringify({
          model: ev.model,
          articul: ev.articul,
          calc_sign: ev.calc_sign || null,
          plan_id: ev.plan_id || null,
          date: ev.date,
          task_number: ev.task_number || null,
          username: username || 'system',
          rows: ev.rows,
          status: 'draft',
        }),
      });
      if (!resp.ok) {
        const errData = await resp.json().catch(() => ({}));
        throw new Error(errData?.detail || `HTTP ${resp.status}`);
      }
      const data = await resp.json();
      ev.version_id = data.version_id;
      ev.selectedVersionId = data.version_id;
      await refreshVersions();
      alert('Новая версия сохранена как черновик');
    }
  } catch (e) {
    alert('Ошибка при сохранении: ' + (e?.message || String(e)));
  } finally {
    savingDraft.value = false;
  }
};

const submitDraft = async () => {
  if (!editingVersion.value) return;
  if (!confirm('Отправить расчёт на утверждение?')) return;
  const ev = editingVersion.value;
  submittingDraft.value = true;
  try {
    if (ev.version_id !== null) {
      const resp = await fetch(`${apiBase.value}/api/cost/submit-calculation-draft`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json', ...fetchHeaders.value},
        body: JSON.stringify({version_id: ev.version_id}),
      });
      if (!resp.ok) {
        const errData = await resp.json().catch(() => ({}));
        throw new Error(errData?.detail || `HTTP ${resp.status}`);
      }
    } else {
      const resp = await fetch(`${apiBase.value}/api/cost/create-version`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json', ...fetchHeaders.value},
        body: JSON.stringify({
          model: ev.model,
          articul: ev.articul,
          calc_sign: ev.calc_sign || null,
          plan_id: ev.plan_id || null,
          date: ev.date,
          task_number: ev.task_number || null,
          username: username || 'system',
          rows: ev.rows,
          status: 'pending',
        }),
      });
      if (!resp.ok) {
        const errData = await resp.json().catch(() => ({}));
        throw new Error(errData?.detail || `HTTP ${resp.status}`);
      }
    }
    alert('Расчёт отправлен на утверждение');
    editingVersion.value = null;
    await loadData();
  } catch (e) {
    alert('Ошибка при отправке: ' + (e?.message || String(e)));
  } finally {
    submittingDraft.value = false;
  }
};

const closeVersionEditor = () => {
  editingVersion.value = null;
  editingTypeCell.value = -1;
  stagePrices.value = [];
};

const addVersionRow = () => {
  if (!editingVersion.value) return;
  const template: any = {};
  if (editingVersion.value.rows.length > 0) {
    const first = editingVersion.value.rows[0];
    for (const key of Object.keys(first)) {
      if (key !== 'id' && key !== 'version_id' && key !== 'sort_order' && key !== '_selected') {
        template[key] = first[key];
      }
    }
    // Reset only user-editable material fields — preserve dates, model info, prices, etc.
    template['Материал/операция/декор(призн)'] = 'Материал основной';
    template['Наименование'] = '';
    template['артикул материала'] = '';
    // Свойства — тоже поля материала, их нельзя тащить из первой строки:
    // они входят в ключ материала (свойство1..3), и новая строка «наследовала»
    // бы чужие характеристики.
    template['свойство1'] = '';
    template['свойство2'] = '';
    template['свойство3'] = '';
    template['Свойство'] = '';
    template._propRaw = '';
    template['Норма'] = 0;
    template['цена материала, руб.'] = 0;
    template['цена материала, USD.'] = 0;
    template['row_comment'] = '';
    // Zero out cost components — user fills norm + price, these get recalculated on save
    template['Основные материалы, руб.'] = 0;
    template['Основные материалы, USD.'] = 0;
    template['Вспомогательные материалы, руб.'] = 0;
    template['Вспомогательные материалы, USD.'] = 0;
    template['Пошив, руб.'] = 0;
    template['Пошив, USD.'] = 0;
    template['Раскрой, руб.'] = 0;
    template['Раскрой, USD.'] = 0;
    template['Декоры, руб.'] = 0;
    template['Декоры, USD.'] = 0;
    template['Вязание, руб.'] = 0;
    template['Вязание, USD.'] = 0;
  }
  template.change_type = 'added';
  template._selected = false;
  editingVersion.value.rows.push(template);
};

const deleteSelectedRows = () => {
  if (!editingVersion.value) return;
  editingVersion.value.rows = editingVersion.value.rows.filter((r: any) => !r._selected);
};

const onVersionRowEdit = (row: any, event: Event, field: string) => {
  const target = event.target as HTMLInputElement | HTMLSelectElement;
  let val: any = target.value;
  // Parse numeric fields. «Декоры, руб./USD.» — стоимость декора, задаётся
  // суммой напрямую: нормы и цены материала у декоров в источнике нет.
  if (field === 'Норма' || field === 'цена материала, руб.' || field === 'цена материала, USD.'
      || field === 'Курс на дату расчета' || field === 'Декоры, руб.' || field === 'Декоры, USD.') {
    val = target.value === '' ? null : parseFloat(target.value);
  }
  row[field] = val;

  // Сумма декора: пересчитываем парную валюту по курсу строки, как это делают
  // цены материала ниже.
  if (field === 'Декоры, руб.' || field === 'Декоры, USD.') {
    const decorRate = Number(row['Курс на дату расчета'] || 0);
    if (decorRate > 0) {
      if (field === 'Декоры, руб.') row['Декоры, USD.'] = Number(val || 0) / decorRate;
      else row['Декоры, руб.'] = Number(val || 0) * decorRate;
    }
    if (row.change_type === 'original') row.change_type = 'modified';
    return;
  }
  // Mark row as modified (unless it's already 'added')
  if (row.change_type === 'original') {
    row.change_type = 'modified';
  }
  // Auto-convert RUB ↔ USD via exchange rate
  const rate = Number(row['Курс на дату расчета'] || 0);
  if (rate > 0) {
    if (field === 'цена материала, USD.') {
      // USD changed → recalc RUB
      const usd = Number(val || 0);
      row['цена материала, руб.'] = usd * rate;
    } else if (field === 'цена материала, руб.') {
      // RUB changed → recalc USD
      const rub = Number(val || 0);
      row['цена материала, USD.'] = rub / rate;
    } else if (field === 'Курс на дату расчета') {
      // Rate changed → recalc RUB from USD (if USD set), or USD from RUB (if RUB set)
      const usd = Number(row['цена материала, USD.'] || 0);
      const rub = Number(row['цена материала, руб.'] || 0);
      if (usd > 0) {
        row['цена материала, руб.'] = usd * rate;
      } else if (rub > 0) {
        row['цена материала, USD.'] = rub / rate;
      }
    }
  }
  // Материал/операция/декор(призн) → денежный бакет (зеркалит _recalc_cost_buckets в db.py)
  // Зеркалит _BUCKET_BY_MAT_TYPE в db.py: канонические значения дропдауна плюс
  // легаси-коды источника, которые могут остаться в строке несмигрированной
  // версии. Без легаси-кодов сумма по строке не пересчитывалась на живом вводе.
  const BUCKET_BY_MAT_TYPE: Record<string, string> = {
    'Материал основной': 'Основные материалы',
    'Материал вспомогательный': 'Вспомогательные материалы',
    'Декор': 'Декоры',
    'Пошив': 'Пошив',
    'Раскрой': 'Раскрой',
    'Вязание': 'Вязание',
    'Всп': 'Вспомогательные материалы',
    'Осн': 'Основные материалы',
    'себестоимость лиса всп': 'Вспомогательные материалы',
    'себестоимость лиса осн': 'Основные материалы',
    'декор': 'Декоры',
    'материал': 'Основные материалы',
    'шт': 'Декоры',
    'Декоры лиса': 'Декоры',
    'пошив': 'Пошив',
  };
  const MANAGED_BUCKETS = ['Основные материалы', 'Вспомогательные материалы', 'Декоры', 'Пошив', 'Раскрой', 'Вязание'];
  // When type field changes, zero out ALL managed buckets first
  if (field === 'Материал/операция/декор(призн)') {
    for (const b of MANAGED_BUCKETS) {
      row[`${b}, руб.`] = 0;
      row[`${b}, USD.`] = 0;
    }
  }
  // Recalculate cost bucket based on material/operation type
  if (field === 'Норма' || field === 'цена материала, руб.' || field === 'цена материала, USD.' || field === 'Курс на дату расчета' || field === 'Материал/операция/декор(призн)') {
    const norm = row['Норма'] || 0;
    const priceRub = row['цена материала, руб.'] || 0;
    const priceUsd = row['цена материала, USD.'] || 0;
    const sumRub = norm * priceRub;
    const sumUsd = norm * priceUsd;
    const matType = row['Материал/операция/декор(призн)'] || '';
    const target = BUCKET_BY_MAT_TYPE[matType];
    if (target) {
      for (const b of MANAGED_BUCKETS) {
        if (b !== target) {
          row[`${b}, руб.`] = 0;
          row[`${b}, USD.`] = 0;
        }
      }
      row[`${target}, руб.`] = sumRub;
      row[`${target}, USD.`] = sumUsd;
    }
  }
};

/** Нормализует строку версии: маппит старые значения типа и вычисляет USD цену. */
function normalizeVersionRow(rr: any): any {
  const row = { ...rr, _selected: false };
  // Normalize old material type values → new dropdown values
  const typeMap: Record<string, string> = {
    'материал': 'Материал основной',
    'декор': 'Декор',
    // Коды этапа ПКПСС (ключи здесь в нижнем регистре — см. raw ниже).
    // Зеркалят _LEGACY_TYPE_ALIASES в db.py.
    'всп': 'Материал вспомогательный',
    'осн': 'Материал основной',
    'себестоимость лиса всп': 'Материал вспомогательный',
    'себестоимость лиса осн': 'Материал основной',
    // 'техоперация' — легаси-тип без разбиения на Пошив/Раскрой/Вязание,
    // не мапим: строки с ним мигрируются на бэкенде (см. migrate_split_technoperation),
    // а не-мигрированные остатки должны остаться нетронутыми, а не тихо стать одним из трёх.
  };
  const raw = (row['Материал/операция/декор(призн)'] || '').trim().toLowerCase();
  if (typeMap[raw]) {
    row['Материал/операция/декор(призн)'] = typeMap[raw];
  }
  // Fill USD price from RUB price × exchange rate if USD is empty/zero
  const rate = Number(row['Курс на дату расчета'] || 0);
  const priceRub = Number(row['цена материала, руб.'] || 0);
  const priceUsd = Number(row['цена материала, USD.'] || 0);
  if (priceUsd === 0 && priceRub > 0 && rate > 0) {
    row['цена материала, USD.'] = priceRub / rate;
  }
  // Merged property from свойство1/2/3
  const parts = [row['свойство1'], row['свойство2'], row['свойство3']]
    .map((v: any) => (v || '').toString().trim())
    .filter((v: string) => v && v !== '-');
  row['Свойство'] = parts.join(', ') || '—';
  // Сырой текст для поля ввода «Свойство». Держим отдельно от собранного
  // `Свойство`, чтобы при вводе не подставлять пересобранное значение обратно
  // в input — иначе курсор прыгал бы в конец на каждом символе.
  row._propRaw = parts.join(', ');
  return row;
}

/** Разбирает введённое «Свойство» обратно в свойство1/2/3 — в БД лежат они,
 * колонки «Свойство» там нет. Больше трёх частей склеиваем в третье поле,
 * чтобы введённое не потерялось. */
const onVersionPropertyEdit = (row: any, event: Event) => {
  const raw = (event.target as HTMLInputElement).value;
  row._propRaw = raw;
  const parts = raw.split(',').map((v) => v.trim());
  row['свойство1'] = parts[0] || '';
  row['свойство2'] = parts[1] || '';
  row['свойство3'] = parts.length > 3 ? parts.slice(2).join(', ') : (parts[2] || '');
  const filled = [row['свойство1'], row['свойство2'], row['свойство3']].filter(Boolean);
  row['Свойство'] = filled.join(', ') || '—';
  if (row.change_type === 'original') row.change_type = 'modified';
};

/** Определяет, вносит ли строка нулевой вклад в себестоимость (нет нормы или нет цены). */
/** Сумма по строке. У материалов это Норма × цена, у декоров — их собственная
 * сумма из «Декоры, руб./USD.»: нормы и цены у декоров в источнике нет вообще.
 * Раньше здесь всегда считалось произведение, и строки декоров показывали 0,
 * хотя в себестоимость входили. */
function versionRowSum(row: any, cur: 'руб.' | 'USD.'): number {
  if (isDecorRow(row)) return Number(row[`Декоры, ${cur}`] || 0);
  const price = Number(row[cur === 'руб.' ? 'цена материала, руб.' : 'цена материала, USD.'] || 0);
  return Number(row['Норма'] || 0) * price;
}

function isZeroCostRow(row: any): boolean {
  const norm = Number(row['Норма'] || 0);
  const priceRub = Number(row['цена материала, руб.'] || 0);
  const priceUsd = Number(row['цена материала, USD.'] || 0);
  return norm === 0 || (priceRub === 0 && priceUsd === 0);
}

/** Тип строки — декор, если тип ∈ {шт, Декоры лиса, декор}. */
const DECOR_TYPE_OVERRIDE = new Set(['шт', 'Декоры лиса', 'декор', 'Декор']);
const DECOR_EMPTY_VALUES = new Set(['', '-', '0', '0.0000', '0.00', '0.0', '--']);

function isDecorRow(row: any): boolean {
  return DECOR_TYPE_OVERRIDE.has(row['Материал/операция/декор(призн)'] || '');
}

/** В колонке «Материал/operация» для декоров всегда «Декор». */
function typeDisplayValue(row: any): string {
  if (isDecorRow(row)) return 'Декор';
  return row['Материал/операция/декор(призн)'] || '';
}

/** В колонке «Наименование»: для декоров — название декора, для остальных — Наименование. */
function nameDisplayValue(row: any): string {
  if (isDecorRow(row)) {
    const decorName = (row['Декоры, наименование'] || '').trim();
    if (decorName && !DECOR_EMPTY_VALUES.has(decorName)) return decorName;
    return '';
  }
  return row['Наименование'] || '';
}

/** Найти индексы строк с тем же Модель+Артикул+PLAN_ID+Признак калькуляции (исключая excludeIdx). */
/** Строки той же калькуляции (модель+артикул+план+признак), кроме самой `row`.
 *
 * Возвращает строки, а не их номера: номер после смены фильтра указывает на
 * другую калькуляцию, а ссылка на строку остаётся верной. */
function findSiblingRows(row: any): any[] {
  const model = row['Модель'];
  const articul = row['Артикул'];
  const planId = row['PLAN_ID'];
  const calcSign = row['Признак калькуляции'];
  if (!model || !articul || !planId || !calcSign) return [];
  const key = `${model}|${articul}|${planId}|${calcSign}`;
  return allAggregated.value.filter(
    (r) => r !== row
      && `${r['Модель']}|${r['Артикул']}|${r['PLAN_ID']}|${r['Признак калькуляции']}` === key
  );
}

/** Выбор розничной цены из выпадающего списка: синхронизируем по всем строкам с тем же model+articul+plan_id+calc_sign. */
const onRetailPriceSelect = (row: any, value: string) => {
  if (!row || isRowLocked(row)) return;
  const targets = [row, ...findSiblingRows(row)];
  const numVal = parseFloat(value);

  const clearRow = (r: any) => {
    r["avg_Розничная цена по уровню, руб."] = 0;
    r["avg_Отпускная цена по уровню, руб"] = 0;
    r["Уровень цен"] = "";
    r["avg_Розничная цена по уровню, USD."] = 0;
    r["avg_Отпускная цена по уровню, USD."] = 0;
    r["mp_price_rub"] = null;
    markupSelections[calcRowKey(r)] = "";
    changedRows.set(calcRowKey(r), r);
  };
  const updateRow = (r: any, val: number) => {
    r["avg_Розничная цена по уровню, руб."] = val;
    r["avg_Отпускная цена по уровню, руб"] = 0;
    r["Уровень цен"] = "";
    r["avg_Розничная цена по уровню, USD."] = 0;
    r["avg_Отпускная цена по уровню, USD."] = 0;
    r["mp_price_rub"] = null;
    markupSelections[calcRowKey(r)] = "";
    changedRows.set(calcRowKey(r), r);
  };

  if (isNaN(numVal) || numVal <= 0) {
    targets.forEach(clearRow);
    return;
  }
  targets.forEach(r => updateRow(r, numVal));

  // Автовыбор наценки по category level01 (только для текущей строки, onMarkupSelect синхронизирует сам)
  const options = getMarkupOptions(row);
  if (options.length === 1) {
    onMarkupSelect(row, options[0].value);
  } else if (options.length > 1) {
    const target = getTargetMarkup(row["Level 01"]);
    const closest = findClosestMarkup(options, target);
    onMarkupSelect(row, closest.value);
  }
};

/** Выбор наценки: находим соответствующий уровень цен, обновляем строку и сохраняем. */
const onMarkupSelect = async (row: any, markupValue: string) => {
  if (!markupValue) return;
  if (!row || isRowLocked(row)) return;

  const retailPrice = Number(row['avg_Розничная цена по уровню, руб.']);
  const avgVat = Number(row['avg_Ставка НДС'] || 0);
  if (!retailPrice) return;

  // Ищем уровень цен, чья расчётная наценка совпадает с выбранной
  let matchedLevel: PriceLevel | null = null;
  for (const level of priceLevels.value) {
    if (String(level.price_type3) !== String(retailPrice)) continue;
    if (!level.price_type1 || level.price_type1 <= 0) continue;
    const computedPct = ((retailPrice / (100 + avgVat) * 100) / level.price_type1 - 1) * 100;
    if (computedPct.toFixed(2) === markupValue) {
      matchedLevel = level;
      break;
    }
  }
  // Курс RUB→USD
  let rate = Number(row["avg_Курс на дату расчета"] || 0);
  if (rate === 0) rate = _deriveRate(row);
  if (rate === 0) {
    for (const r of allAggregated.value) { rate = _deriveRate(r); if (rate > 0) break; }
  }
  if (rate === 0) rate = 92;

  const r2 = (v: number) => Math.round(v * 100) / 100;

  let retailVal: number, wholesaleVal: number;
  if (matchedLevel) {
    // Используем цены из найденного уровня цен
    retailVal = matchedLevel.price_type3;
    wholesaleVal = matchedLevel.price_type1;
  } else {
    // Уровень цен не найден — вычисляем оптовую цену из наценки
    const retailExclVat = retailPrice / (100 + avgVat) * 100;
    wholesaleVal = r2(retailExclVat / (1 + Number(markupValue) / 100));
    retailVal = retailPrice;
  }
  const retailUsd = r2(retailVal / rate);
  const wholesaleUsd = r2(wholesaleVal / rate);

  // Синхронизируем все строки с тем же model+articul+plan_id+calc_sign
  for (const r of [row, ...findSiblingRows(row)]) {
    const k = calcRowKey(r);
    r["avg_Розничная цена по уровню, руб."] = retailVal;
    r["avg_Отпускная цена по уровню, руб"] = wholesaleVal;
    r["Уровень цен"] = matchedLevel ? matchedLevel.name : "";
    r["avg_Розничная цена по уровню, USD."] = retailUsd;
    r["avg_Отпускная цена по уровню, USD."] = wholesaleUsd;
    r["mp_price_rub"] = computeMpPriceJs(r);
    if (matchedLevel) {
      priceRF[k] = matchedLevel.price_type4;
      priceKZ[k] = matchedLevel.price_type5;
      priceUZ[k] = matchedLevel.price_type6;
    }
    markupSelections[k] = markupValue;
    changedRows.set(k, r);
  }
};

const saveAllChanges = async () => {
  if (!changedRows.size) return;
  saving.value = true;
  try {
    const changes = Array.from(changedRows.entries()).map(([key, row]) => {
      return {
        model: row["Модель"],
        articul: row["Артикул"],
        price_level: row["Уровень цен"],
        retail_rub: row["avg_Розничная цена по уровню, руб."],
        wholesale_rub: row["avg_Отпускная цена по уровню, руб"],
        retail_usd: row["avg_Розничная цена по уровню, USD."],
        wholesale_usd: row["avg_Отпускная цена по уровню, USD."],
        calc_sign: row["Признак калькуляции"],
        plan_id: row["PLAN_ID"],
        brand_manager: row["Бренд-менеджер"],
        model_name: row["Наименование модели"],
        task_number: row["Номер задания производства"],
        date: row["дата расчета"],
        country: row["Страна пр-ва"],
        family: row["Семья"],
        season: row["Сезон"],
        level01: row["Level 01"],
        level02: row["Level 02"],
        level03: row["Level 03"],
        level04: row["Level 04"],
        level05: row["Level 05"],
        materials_rub: row["sum_Основные материалы, руб."],
        materials_usd: row["sum_Основные материалы, USD."],
        aux_materials_rub: row["sum_Вспомогательные материалы, руб."],
        aux_materials_usd: row["sum_Вспомогательные материалы, USD."],
        sewing_rub: row["sum_Пошив, руб."],
        sewing_usd: row["sum_Пошив, USD."],
        cutting_rub: row["sum_Раскрой, руб."],
        cutting_usd: row["sum_Раскрой, USD."],
        decors_rub: row["sum_Декоры, руб."],
        decors_usd: row["sum_Декоры, USD."],
        knitting_rub: row["sum_Вязание, руб."],
        knitting_usd: row["sum_Вязание, USD."],
        cost_rub: row["sum_Себестоимость, руб."],
        cost_usd: row["sum_Себестоимость, USD."],
        price_rf: priceRF[key] || 0,
        price_kz: priceKZ[key] || 0,
        price_uz: priceUZ[key] || 0,
        comment: comments[key] || "",
      };
    });
    const result = await $fetch<{ success: boolean; count: number; error?: string; mock?: boolean }>(
      `${apiBase.value}/api/cost/save-batch`,
      { method: "POST", body: { changes, author_name: user.value?.email || '' }, headers: fetchHeaders.value }
    );
    if (result.mock) mockMode.value = true;
    if (result.success) {
      alert(`Сохранено ${result.count} записей${result.mock ? " (mock-режим)" : ""}`);
      changedRows.clear();
    } else {
      changedRows.clear();
      alert("Ошибка: " + (result.error || "unknown"));
    }
  } catch (e: any) {
    changedRows.clear();
    console.error("[cost] save-batch failed", e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    saving.value = false;
  }
};

// ── Utilities ───────────────────────────────────────────────────────────────

const fmt = (v: any): string => {
  if (v === null || v === undefined || v === "") return "—";
  const n = Number(v);
  if (Number.isNaN(n)) return String(v);
  return n.toLocaleString("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
};

/** Специфика расчёта калькуляции: нормы в источнике доходят до 6 знаков, а цены
 * материалов — до 4 (money(19,4)). Округление до копеек на этом уровне съедает
 * реальные деньги: цена 0.0005 превращалась в 0.00 и стоимость строки исчезала
 * целиком (см. миграцию 0030). Поэтому в модалках исходных строк и редактора
 * версий показываем нативную точность источника, а на главной таблице —
 * по-прежнему копейки, но уже посчитанные из неокруглённых слагаемых. */
const fmtNorm = (v: any): string => {
  if (v === null || v === undefined || v === "") return "—";
  const n = Number(v);
  if (Number.isNaN(n)) return String(v);
  return n.toLocaleString("ru-RU", { minimumFractionDigits: 0, maximumFractionDigits: 6 });
};

/** Цены материалов в руб./USD. и суммы по строке — до 4 знаков, но не меньше
 * копеек, чтобы колонка читалась как денежная. */
const fmtPrice4 = (v: any): string => {
  if (v === null || v === undefined || v === "") return "—";
  const n = Number(v);
  if (Number.isNaN(n)) return String(v);
  return n.toLocaleString("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 4 });
};

const formatDate = (v: any): string => {
  if (!v) return "—";
  const s = String(v);
  return s.includes("T") ? s.split("T")[0] : s;
};

const formatDateTime = (v: string | null): string => {
  if (!v) return "—";
  const d = new Date(v);
  return d.toLocaleString("ru-RU", {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
};

/** Плановая рентабельность, % — план. опт / план. с/с − 1.
 *
 * Считается по той же логике, что и фактическая рентабельность в `calc`, но на
 * плановых цифрах, поэтому валюта не переключается: `planned_*` приходят с
 * бэкенда только в рублях. Возвращает null, если плановой себестоимости нет
 * или она нулевая — делить не на что, и в таблице честнее показать «—», чем 0%.
 */
const plannedProfitabilityPct = (row: any): number | null => {
  const wholesale = Number(row?.planned_wholesale ?? NaN);
  const cost = Number(row?.planned_cost ?? NaN);
  if (!isFinite(wholesale) || !isFinite(cost) || cost === 0) return null;
  return (wholesale / cost - 1) * 100;
};

const calc = (row: any, useUsd: boolean = false) => {
  const wholesale = useUsd
    ? Number(row["avg_Отпускная цена по уровню, USD."] || 0)
    : Number(row["avg_Отпускная цена по уровню, руб"] || 0);
  const cost = useUsd
    ? Number(row["sum_Себестоимость, USD."] || 0)
    : Number(row["sum_Себестоимость, руб."] || 0);
  const markup = wholesale - cost;
  const markupPct = cost > 0 ? (markup / cost) * 100 : 0;
  const marginPct = wholesale > 0 ? (markup / wholesale) * 100 : 0;
  return { markupRub: markup, markupPct, marginPct };
};

// ── Margin deviation helpers ────────────────────────────────────────────────

const marginDeviation = (row: any, useUsd: boolean = false): number | null => {
  const target = row.target_margin_pct;
  if (target === null || target === undefined) return null;
  const { marginPct } = calc(row, useUsd);
  return marginPct - target;
};

const marginDevText = (row: any, useUsd: boolean = false): string => {
  const dev = marginDeviation(row, useUsd);
  if (dev === null) return '—';
  return (dev >= 0 ? '+' : '') + dev.toFixed(1) + '%';
};

const marginDevClass = (row: any, useUsd: boolean = false): Record<string, boolean> => {
  const dev = marginDeviation(row, useUsd);
  if (dev === null) return {};
  return { 'delta-pos': dev >= 0, 'delta-neg': dev < 0 };
};

const marginRowClass = (row: any): Record<string, boolean> => {
  const dev = marginDeviation(row, showUSD.value);
  if (dev === null) return {};
  return { 'row-margin-ok': dev >= 0, 'row-margin-bad': dev < 0 };
};

/** ФКСС — история себестоимости, в DWH такие цены не пишутся.
 *
 * Бэкенд их и не принимал (`/save-changes` отвечает 400, `/save-batch` молча
 * фильтрует), но поля цен РФ/КЗ/УЗ и комментарий в таблице оставались
 * доступными: пользователь вводил значения, а сохранение их выбрасывало без
 * объяснения. Теперь ввод закрыт там же, где он бессмыслен. */
const FKSS_HINT = 'ФКСС — история себестоимости: цены не редактируются и в DWH не пишутся';
const isFkssRow = (row: any): boolean =>
  (row?.['Признак калькуляции'] ?? '').toString().trim() === 'ФКСС';

const isRowLocked = (row: any): boolean => {
  if (can('cost:admin')) return false;
  if (row._lock_reason) return true;
  if (row._has_audit) return true;
  if (can('cost:edit_price') && !can('cost:approve') && !can('cost:peo_mark')) {
    if (!row._group_approved) return true;
  }
  return false;
};

/** Возврат калькуляции на корректировку после записи в DWH (только cost:admin).
 *
 * Ничего не удаляет: снимает блокировку, чтобы прошёл второй цикл
 * «правка → согласование ПЭО → установка цен». Разрешение самоистекающее —
 * после повторной установки цен блокировка возвращается сама. */
const reopenTarget = ref<any | null>(null);
const reopenReason = ref('');
const reopenBusy = ref(false);
const reopenError = ref('');

const openReopenModal = (row: any) => {
  if (!can('cost:admin')) return;
  reopenTarget.value = row;
  reopenReason.value = '';
  reopenError.value = '';
};
const closeReopenModal = () => { reopenTarget.value = null; reopenError.value = ''; };

/** Ключ калькуляции для админских операций с DWH — те же 4 поля, что в
 *  cost_dwh_reopen и в ключе записи цен. */
const reopenPayload = (row: any) => ({
  model: (row['Модель'] ?? '').toString().trim(),
  articul: (row['Артикул'] ?? '').toString().trim(),
  calc_sign: (row['Признак калькуляции'] ?? '').toString().trim(),
  plan_id: (row['PLAN_ID'] ?? '').toString().trim(),
});

/** Локально снимаем/возвращаем блокировку у всех строк той же калькуляции —
 *  иначе до перезагрузки данных таблица показывала бы прежнее состояние. */
const applyReopenLocally = (row: any, reopened: boolean) => {
  const k = reopenPayload(row);
  for (const r of allAggregated.value) {
    if ((r['Модель'] ?? '').toString().trim() !== k.model) continue;
    if ((r['Артикул'] ?? '').toString().trim() !== k.articul) continue;
    if ((r['Признак калькуляции'] ?? '').toString().trim() !== k.calc_sign) continue;
    if ((r['PLAN_ID'] ?? '').toString().trim() !== k.plan_id) continue;
    r._reopened = reopened;
    r._in_dwh = true;
    r._has_audit = !reopened;
  }
};

async function submitReopen() {
  if (!reopenTarget.value) return;
  reopenBusy.value = true;
  reopenError.value = '';
  try {
    await $fetch(`${apiBase.value}/api/cost/admin/dwh-reopen`, {
      method: 'POST',
      body: { ...reopenPayload(reopenTarget.value), reason: reopenReason.value.trim() },
      headers: fetchHeaders.value,
    });
    applyReopenLocally(reopenTarget.value, true);
    closeReopenModal();
  } catch (e: any) {
    reopenError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    reopenBusy.value = false;
  }
}

async function submitRevokeReopen() {
  if (!reopenTarget.value) return;
  reopenBusy.value = true;
  reopenError.value = '';
  try {
    await $fetch(`${apiBase.value}/api/cost/admin/dwh-reopen/revoke`, {
      method: 'POST',
      body: reopenPayload(reopenTarget.value),
      headers: fetchHeaders.value,
    });
    applyReopenLocally(reopenTarget.value, false);
    closeReopenModal();
  } catch (e: any) {
    reopenError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    reopenBusy.value = false;
  }
}

const openApprovalPopup = (row: any) => { if (!can('cost:approve') && !can('cost:peo_mark')) return; approvalTarget.value = row; approvalComment.value = ''; };
const closeApprovalPopup = () => { approvalTarget.value = null; };

const recomputeGroupApproved = (row: any) => {
  const model = row['Модель'];
  const articul = row['Артикул'];
  const planId = row['PLAN_ID'];
  const calcSign = row['Признак калькуляции'];
  if (!model || !articul || !planId || !calcSign) return;
  const siblings = allAggregated.value.filter(
    (r) => r['Модель'] === model && r['Артикул'] === articul && r['PLAN_ID'] === planId && r['Признак калькуляции'] === calcSign
  );
  const allApproved = siblings.length > 0 && siblings.every((r) => r.peo_status === 'approved');
  for (const r of siblings) r._group_approved = allApproved;
};

/** Ключ согласования одной строки агрегата: (модель, артикул, признак, PLAN_ID, № задания).
 *
 * Значения тримим: в `/aggregated` JOIN к `cost_calc_approvals` идёт по
 * `TRIM(cd."Модель") = ca.model` и т.д., поэтому запись с пробелами по краям
 * просто не найдётся обратно и статус не подтянется. */
const peoKeyFields = (row: any) => ({
  model: (row['Модель'] ?? '').toString().trim(),
  articul: (row['Артикул'] ?? '').toString().trim(),
  calc_sign: (row['Признак калькуляции'] ?? '').toString().trim(),
  plan_id: (row['PLAN_ID'] ?? '').toString().trim(),
  // Пустое задание шлём пустой строкой, а НЕ null: в cost_calc_approvals
  // task_number NOT NULL DEFAULT '' (миграция 0038). Пока сюда уходил null,
  // UNIQUE не срабатывал (NULLS DISTINCT) и каждое согласование плодило новую
  // запись, а join в /aggregated её не находил — зелёная отметка ПЭО пропадала
  // после перезагрузки данных.
  task_number: (row['Номер задания производства'] ?? '').toString().trim(),
});

const setApproval = async (status: 'approved' | 'rejected') => {
  if (!approvalTarget.value) return;
  approving.value = true;
  try {
    const r = approvalTarget.value;
    await fetch(`${apiBase.value}/api/cost/approve-calculation`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json', ...fetchHeaders.value},
      body: JSON.stringify({
        approved_by: user.value?.email || 'system',
        approvals: [{
          ...peoKeyFields(r),
          status, comment: status === 'rejected' ? approvalComment.value : '',
        }],
      }),
    });
    if (approvalTarget.value) {
      approvalTarget.value.peo_status = status;
      recomputeGroupApproved(approvalTarget.value);
    }
    closeApprovalPopup();
  } catch (e) {
    alert('Ошибка при сохранении статуса ПЭО');
  } finally {
    approving.value = false;
  }
};

const revokeApproval = async (row: any) => {
  if (!row) return;
  approving.value = true;
  try {
    await $fetch(`${apiBase.value}/api/cost/revoke-approval`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...fetchHeaders.value },
      body: peoKeyFields(row),
    });
    if (approvalTarget.value) {
      approvalTarget.value.peo_status = null;
      recomputeGroupApproved(approvalTarget.value);
    }
    closeApprovalPopup();
  } catch (e: any) {
    alert('Ошибка при снятии согласования: ' + (e?.data?.detail || e?.message || String(e)));
  } finally {
    approving.value = false;
  }
};

// ── Массовое согласование ПЭО ───────────────────────────────────────────────
// Согласование пер-заданное, а строк агрегата с одним ключом может быть несколько
// (разные даты расчёта и уровни цен). Поэтому выбор храним по ключу: «двойники»
// отмечаются вместе, а в запрос ключ уходит ровно один раз.

/** Пачка на один запрос. На бэке потолок 1000 (APPROVALS_BATCH_LIMIT). */
const PEO_BULK_CHUNK = 200;

const peoBulkEnabled = computed(() => can('cost:approve') || can('cost:peo_mark'));

const peoKey = (row: any): string => {
  const k = peoKeyFields(row);
  return [k.model, k.articul, k.calc_sign ?? '', k.plan_id ?? '', k.task_number ?? ''].join('');
};
const peoItemKey = (it: any): string =>
  [it.model ?? '', it.articul ?? '', it.calc_sign ?? '', it.plan_id ?? '', it.task_number ?? ''].join('');

/** Те же условия, при которых открывается одиночный попап ПЭО. */
const canSelectForPeo = (row: any): boolean =>
  peoBulkEnabled.value && !isRowLocked(row) && !row._has_audit;

const peoSelectedKeys = ref<Set<string>>(new Set());
const peoBulkBusy = ref(false);
const peoBulkAction = ref<'' | 'approved' | 'rejected' | 'revoke'>('');
const peoBulkProgress = reactive({ done: 0, total: 0 });
const peoBulkStatus = ref('');
const peoBulkError = ref('');
const peoBulkComment = ref('');

const isPeoSelected = (row: any): boolean => peoSelectedKeys.value.has(peoKey(row));

const peoSelectableRows = computed(() => sortedRows.value.filter(canSelectForPeo));
const peoPageSelectableRows = computed(() => pageRows.value.filter(canSelectForPeo));

const peoPageAllSelected = computed(
  () => peoPageSelectableRows.value.length > 0 && peoPageSelectableRows.value.every(isPeoSelected)
);
const peoPageSomeSelected = computed(
  () => !peoPageAllSelected.value && peoPageSelectableRows.value.some(isPeoSelected)
);

/** Уникальные ключи выбранного — ровно то, что уходит на бэкенд. */
const peoSelectedItems = computed(() => {
  const seen = new Set<string>();
  const out: ReturnType<typeof peoKeyFields>[] = [];
  for (const r of peoSelectableRows.value) {
    const key = peoKey(r);
    if (!peoSelectedKeys.value.has(key) || seen.has(key)) continue;
    seen.add(key);
    out.push(peoKeyFields(r));
  }
  return out;
});
/** Сколько строк таблицы затронет операция — обычно больше, чем калькуляций. */
const peoSelectedRowCount = computed(
  () => peoSelectableRows.value.filter(isPeoSelected).length
);

// Shift-клик выделяет диапазон строк страницы — как в привычных таблицах.
let peoShiftPressed = false;
let peoLastClickedIdx: number | null = null;

const onPeoCheckboxClick = (e: MouseEvent) => { peoShiftPressed = e.shiftKey; };

const onPeoCheckboxChange = (row: any, pageIdx: number, checked: boolean) => {
  const next = new Set(peoSelectedKeys.value);
  const apply = (r: any) => { if (checked) next.add(peoKey(r)); else next.delete(peoKey(r)); };
  if (peoShiftPressed && peoLastClickedIdx !== null) {
    const from = Math.min(peoLastClickedIdx, pageIdx);
    const to = Math.max(peoLastClickedIdx, pageIdx);
    for (let i = from; i <= to; i++) {
      const r = pageRows.value[i];
      if (r && canSelectForPeo(r)) apply(r);
    }
  } else {
    apply(row);
  }
  peoShiftPressed = false;
  peoLastClickedIdx = pageIdx;
  peoSelectedKeys.value = next;
};

const togglePeoPage = (checked: boolean) => {
  const next = new Set(peoSelectedKeys.value);
  for (const r of peoPageSelectableRows.value) {
    if (checked) next.add(peoKey(r)); else next.delete(peoKey(r));
  }
  peoLastClickedIdx = null;
  peoSelectedKeys.value = next;
};

const selectAllFilteredForPeo = () => {
  peoLastClickedIdx = null;
  peoSelectedKeys.value = new Set(peoSelectableRows.value.map(peoKey));
};

const clearPeoSelection = () => {
  peoLastClickedIdx = null;
  peoSelectedKeys.value = new Set();
  peoBulkStatus.value = '';
  peoBulkError.value = '';
};

/** Проставляет статус во все строки с этими ключами и пересчитывает _group_approved. */
const applyPeoStatusLocally = (items: any[], status: 'approved' | 'rejected' | null) => {
  const keys = new Set(items.map(peoItemKey));
  const email = user.value?.email || '';
  const now = new Date().toISOString();
  const touched: any[] = [];
  for (const r of allAggregated.value) {
    if (!keys.has(peoKey(r))) continue;
    r.peo_status = status;
    r.peo_approved_by = status === 'approved' ? email : null;
    r.peo_approved_at = status === 'approved' ? now : null;
    touched.push(r);
  }
  // recomputeGroupApproved сканирует весь массив — на группу зовём один раз.
  const doneGroups = new Set<string>();
  for (const r of touched) {
    const g = [r['Модель'], r['Артикул'], r['PLAN_ID'], r['Признак калькуляции']].join('');
    if (doneGroups.has(g)) continue;
    doneGroups.add(g);
    recomputeGroupApproved(r);
  }
};

async function runPeoBulk(action: 'approved' | 'rejected' | 'revoke') {
  if (peoBulkBusy.value) return;
  const items = peoSelectedItems.value;
  if (!items.length) return;
  if (action === 'revoke' && !confirm(`Снять согласование с ${items.length} калькуляций?`)) return;

  const comment = action === 'rejected' ? peoBulkComment.value.trim() : '';
  peoBulkBusy.value = true;
  peoBulkAction.value = action;
  peoBulkStatus.value = '';
  peoBulkError.value = '';
  peoBulkProgress.done = 0;
  peoBulkProgress.total = items.length;

  const okKeys = new Set<string>();
  let failedCount = 0;
  let lastErr = '';

  try {
    for (let i = 0; i < items.length; i += PEO_BULK_CHUNK) {
      const chunk = items.slice(i, i + PEO_BULK_CHUNK);
      try {
        if (action === 'revoke') {
          await $fetch(`${apiBase.value}/api/cost/revoke-approvals-batch`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', ...fetchHeaders.value },
            body: { approvals: chunk },
          });
        } else {
          await $fetch(`${apiBase.value}/api/cost/approve-calculation`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', ...fetchHeaders.value },
            body: {
              approved_by: user.value?.email || 'system',
              approvals: chunk.map((it) => ({ ...it, status: action, comment })),
            },
          });
        }
        applyPeoStatusLocally(chunk, action === 'revoke' ? null : action);
        for (const it of chunk) okKeys.add(peoItemKey(it));
      } catch (e: any) {
        failedCount += chunk.length;
        lastErr = e?.data?.detail || e?.message || String(e);
      }
      peoBulkProgress.done = Math.min(i + chunk.length, items.length);
    }
  } finally {
    // Обработанное снимаем с выбора, неудачное оставляем — можно повторить.
    const rest = new Set(peoSelectedKeys.value);
    for (const k of okKeys) rest.delete(k);
    peoSelectedKeys.value = rest;
    peoBulkBusy.value = false;
    peoBulkAction.value = '';
  }

  const verb = action === 'approved' ? 'Согласовано' : action === 'rejected' ? 'Отклонено' : 'Снято согласование';
  peoBulkStatus.value = `${verb}: ${okKeys.size} из ${items.length} калькуляций`;
  if (failedCount) peoBulkError.value = `Не удалось обработать ${failedCount}: ${lastErr}`;
  if (action === 'rejected') peoBulkComment.value = '';
}

// ── Excel export ────────────────────────────────────────────────────────────

const headers = [
  "", "Бренд-менеджер", "Модель", "Артикул", "Наименование модели", "Номер задания производства", "PLAN_ID", "Страна", "Семья", "Сезон",
  "Дата", "Пр.кальк", "Уровень цен",
  "Цена РФ", "Цена КЗ", "Цена УЗ",
  "Комментарий",
  "Сред. розница (руб)", "Сред. опт (руб)", "Сред. розница ($)", "Сред. опт ($)",
  "Осн. материалы (руб)", "Осн. материалы ($)",
  "Вспом. материалы (руб)", "Вспом. материалы ($)",
  "Пошив (руб)", "Пошив ($)", "Раскрой (руб)", "Раскрой ($)",
  "Декоры (руб)", "Декоры ($)",
  "Вязание (руб)", "Вязание ($)",
  "Себест. (руб)", "Себест. ($)",
  "Рентабельность (руб)", "Рентабельность (%)", "Маржа (%)", "Откл. маржи (%)",
];

const exportToExcel = () => {
  // Выгружаем ровно то, что видит пользователь в таблице, а не всю загруженную
  // выдачу. `sortedRows` — конец цепочки: верхняя панель фильтров уходит в
  // запрос и уже отсечена сервером, `filteredAggregated` добавляет фильтры
  // колонок (нижняя панель) и «скрыть отправленные в DWH», а сортировка даёт
  // тот же порядок строк, что на экране. Раньше здесь стоял `allAggregated`,
  // поэтому фильтры колонок в файл не попадали: отфильтровав до одного плана,
  // пользователь всё равно получал выгрузку по всем (замечание заказчика
  // 25.08.2026).
  const rows = sortedRows.value;
  if (!rows.length) return;
  let html = '<table border="1"><tr>';
  headers.forEach((h) => (html += `<th>${h}</th>`));
  html += "</tr>";
  for (const row of rows) {
    const c = calc(row);
    const cells = [
      "",
      row["Бренд-менеджер"] || "",
      row["Модель"] || "",
      row["Артикул"] || "",
      row["Наименование модели"] || "",
      row["Номер задания производства"] || "",
      row["PLAN_ID"] || "",
      row["Страна пр-ва"] || "",
      row["Семья"] || "",
      row["Сезон"] || "",
      formatDate(row["дата расчета"]),
      row["Признак калькуляции"] || "",
      row["Уровень цен"] || "",
      fmt(priceRF[calcRowKey(row)] ?? ''),
      fmt(priceKZ[calcRowKey(row)] ?? ''),
      fmt(priceUZ[calcRowKey(row)] ?? ''),
      comments[calcRowKey(row)] || "",
      fmt(row["avg_Розничная цена по уровню, руб."]),
      fmt(row["avg_Отпускная цена по уровню, руб"]),
      fmt(row["avg_Розничная цена по уровню, USD."]),
      fmt(row["avg_Отпускная цена по уровню, USD."]),
      fmt(row["sum_Основные материалы, руб."]),
      fmt(row["sum_Основные материалы, USD."]),
      fmt(row["sum_Вспомогательные материалы, руб."]),
      fmt(row["sum_Вспомогательные материалы, USD."]),
      fmt(row["sum_Пошив, руб."]),
      fmt(row["sum_Пошив, USD."]),
      fmt(row["sum_Раскрой, руб."]),
      fmt(row["sum_Раскрой, USD."]),
      fmt(row["sum_Декоры, руб."]),
      fmt(row["sum_Декоры, USD."]),
      fmt(row["sum_Вязание, руб."]),
      fmt(row["sum_Вязание, USD."]),
      fmt(row["sum_Себестоимость, руб."]),
      fmt(row["sum_Себестоимость, USD."]),
      fmt(c.markupRub),
      `${c.markupPct.toFixed(1)}%`,
      `${c.marginPct.toFixed(1)}%`,
      marginDevText(row),
    ];
    html += "<tr>" + cells.map((v) => `<td>${v}</td>`).join("") + "</tr>";
  }
  html += "</table>";
  const blob = new Blob([html], { type: "application/vnd.ms-excel" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `CostHistory_${new Date().toISOString().slice(0, 10)}.xls`;
  a.click();
  URL.revokeObjectURL(url);
};

/** Выгрузка таблицы цен по плану — материалы и декоры вместе с ценами текущего
 * набора. Формат тот же, что у экспорта главной таблицы: HTML-таблица под
 * `application/vnd.ms-excel` (настоящий .xlsx в разделе не собирается нигде).
 *
 * Выгружается ровно то, что видно в модалке: цены открытого набора, исходная
 * цена источника, разброс внутри группы и перекрытие версиями — по ним видно,
 * почему цена набора могла не доехать до расчёта. */
function exportPlanPricesToExcel() {
  const plan = planPricesPlanId.value.trim();
  if (!plan || (!planPriceRows.value.length && !planDecorRows.value.length)) return;

  const esc = (v: any) => String(v ?? '')
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  const cell = (v: any) => `<td>${esc(v)}</td>`;
  const numCell = (v: any) => `<td>${v === null || v === undefined || v === '' ? '' : esc(fmtPrice4(v))}</td>`;
  const head = (cols: string[]) => '<tr>' + cols.map(c => `<th>${esc(c)}</th>`).join('') + '</tr>';

  const setTitle = planPriceForm.value.set_id
    ? `${planPriceForm.value.title || planPriceForm.value.set_id} (${planPriceForm.value.status})`
    : 'новый набор (не сохранён)';

  let html = '<table border="1">';
  html += `<tr><td colspan="10"><b>Цены материалов по плану ${esc(plan)}</b></td></tr>`;
  html += `<tr><td colspan="10">Набор: ${esc(setTitle)}; курс: ${esc(planPriceForm.value.rate ?? '—')}; `
    + `строк с переопределённой ценой: ${planPriceOverriddenCount.value}</td></tr>`;
  html += '<tr><td colspan="10"></td></tr>';

  html += '<tr><td colspan="10"><b>Материалы</b></td></tr>';
  html += head([
    'Наименование', 'Артикул мат.', 'Свойство 1', 'Свойство 2', 'Свойство 3',
    'Строк', 'Исх. цена, руб', 'Цена, руб', 'Цена, $',
    'Разных цен в группе', 'Мин. цена, руб', 'Макс. цена, руб',
    'Перекрыто версией', 'Цена переопределена',
  ]);
  for (const r of planPriceRows.value) {
    html += '<tr>'
      + cell(r['Наименование']) + cell(r['артикул материала'])
      + cell(r['свойство1']) + cell(r['свойство2']) + cell(r['свойство3'])
      + cell(r.rows_count)
      + numCell(r.source_price_rub) + numCell(r.price_rub) + numCell(r.price_usd)
      + cell(r.distinct_prices ?? '') + numCell(r.min_price_rub) + numCell(r.max_price_rub)
      + cell(r.overridden_rows ? `${r.overridden_rows} из ${r.rows_count}` : '')
      + cell(planPriceOverridden(r) ? 'да' : '')
      + '</tr>';
  }

  html += '<tr><td colspan="10"></td></tr>';
  html += '<tr><td colspan="10"><b>Декоры</b> — цена задаётся суммой</td></tr>';
  html += head([
    'Наименование декора', 'Строк', 'Исх. сумма, руб', 'Сумма, руб', 'Сумма, $',
    'Перекрыто версией', 'Цена переопределена',
  ]);
  for (const r of planDecorRows.value) {
    html += '<tr>'
      + cell(r['Декоры, наименование'])
      + cell(r.rows_count)
      + numCell(r.source_price_rub) + numCell(r.price_rub) + numCell(r.price_usd)
      + cell(r.overridden_rows ? `${r.overridden_rows} из ${r.rows_count}` : '')
      + cell(planPriceOverridden(r) ? 'да' : '')
      + '</tr>';
  }
  html += '</table>';

  const blob = new Blob(['﻿' + html], { type: 'application/vnd.ms-excel' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `PlanPrices_${plan}_${new Date().toISOString().slice(0, 10)}.xls`;
  a.click();
  URL.revokeObjectURL(url);
}

// Ctrl+C при пустом выделении — копируем всю таблицу как TSV
const onCopyShortcut = (e: KeyboardEvent) => {
  if (!(e.ctrlKey && (e.key === "c" || e.key === "C"))) return;
  if (window.getSelection()?.toString()) return;
  const table = document.getElementById("cost-table-1");
  if (!table) return;
  e.preventDefault();
  const rows = table.querySelectorAll("tr");
  let tsv = "";
  rows.forEach((r) => {
    // Колонку выбора для массового согласования в буфер не тащим — она служебная.
    const cells = r.querySelectorAll("td:not(.col-peo-sel), th:not(.col-peo-sel)");
    const line: string[] = [];
    cells.forEach((c) => line.push((c as HTMLElement).innerText.replace(/\n/g, " ").trim()));
    tsv += line.join("\t") + "\n";
  });
  navigator.clipboard?.writeText(tsv);
};

// ── Навигация по ячейкам как в Excel ───────────────────────────────────────
// Активная ячейка — это та, чей input/select в фокусе: отдельного слоя выделения
// нет, поэтому ввод работает ровно как раньше, а подсветку даёт :focus-within.
// Маршрут считается по DOM (`cellIndex` ячейки), а не по списку ключей колонок —
// тогда скрытие колонок и любые правки шаблона учитываются сами.

/** Поля ввода строки. Чекбокс выбора для согласования исключён: он служебный,
 * и пробел на нём должен работать нативно. */
const GRID_FOCUSABLE = 'input:not([disabled]):not([readonly]):not([type="checkbox"]), select:not([disabled])';

type GridCell = HTMLInputElement | HTMLSelectElement;

const gridFocusableIn = (scope: Element | null): GridCell[] =>
  scope ? Array.from(scope.querySelectorAll<GridCell>(GRID_FOCUSABLE)) : [];

/** Ячейка той же колонки в строке, если она доступна для ввода. */
const gridCellAt = (tr: Element | null, cellIndex: number): GridCell | null => {
  if (!tr) return null;
  const td = (tr as HTMLTableRowElement).cells?.[cellIndex];
  return td ? gridFocusableIn(td)[0] ?? null : null;
};

/** Ближайшая доступная ячейка колонки при движении по строкам: заблокированные
 * (ФКСС, нет прав, локи) пропускаем, иначе фокус застревал бы в столбце. */
const gridSeekInColumn = (fromRow: Element, cellIndex: number, dir: 1 | -1): GridCell | null => {
  let tr: Element | null = dir === 1 ? fromRow.nextElementSibling : fromRow.previousElementSibling;
  while (tr) {
    const cell = gridCellAt(tr, cellIndex);
    if (cell) return cell;
    tr = dir === 1 ? tr.nextElementSibling : tr.previousElementSibling;
  }
  return null;
};

/** Соседняя ячейка ввода в пределах строки. */
const gridSeekInRow = (tr: Element, cellIndex: number, dir: 1 | -1): GridCell | null => {
  const cells = Array.from((tr as HTMLTableRowElement).cells || []);
  const from = cellIndex + dir;
  for (let i = from; dir === 1 ? i < cells.length : i >= 0; i += dir) {
    const cell = gridFocusableIn(cells[i])[0];
    if (cell) return cell;
  }
  return null;
};

/** Доводит ячейку в видимую область: focus() сам скроллит, но закреплённые
 * колонки слева перекрывают результат — их суммарную ширину компенсируем. */
const gridRevealCell = (cell: GridCell) => {
  cell.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  const wrap = cell.closest('.table-wrap') as HTMLElement | null;
  if (!wrap) return;
  const stickyWidth = STICKY_COL_KEYS
    .filter((k) => isVisible(k))
    .reduce((sum, k) => sum + (stickyWidths[k] || 0), 0);
  const overlap = (wrap.getBoundingClientRect().left + stickyWidth) - cell.getBoundingClientRect().left;
  if (overlap > 0) wrap.scrollLeft -= overlap + 4;
};

const gridFocus = (cell: GridCell | null): boolean => {
  if (!cell) return false;
  cell.focus();
  if (cell instanceof HTMLInputElement && cell.type !== 'checkbox') cell.select();
  gridRevealCell(cell);
  return true;
};

/** Первая доступная ячейка страницы в нужной колонке — точка входа после
 * перелистывания. */
const gridFocusOnPage = (cellIndex: number, fromTop: boolean) => {
  const body = document.querySelector('#cost-table-1 tbody');
  if (!body) return;
  const rows = Array.from((body as HTMLTableSectionElement).rows);
  const ordered = fromTop ? rows : [...rows].reverse();
  for (const tr of ordered) {
    if (gridFocus(gridCellAt(tr, cellIndex))) return;
  }
  // в этой колонке на новой странице вводить негде — берём любую первую
  for (const tr of ordered) {
    if (gridFocus(gridFocusableIn(tr)[0] ?? null)) return;
  }
};

// Значение на момент входа в ячейку — для отката по Escape. Снимок привязан к
// элементу, а не к событию фокуса: focusin приходит не всегда (например, когда
// окно браузера неактивно), а keydown срабатывает до применения символа к value —
// значит на первом нажатии в ячейке мы ещё видим исходное значение.
let gridSnapshot: { el: GridCell | null; value: string } = { el: null, value: '' };

const gridRemember = (el: GridCell) => {
  if (gridSnapshot.el !== el) gridSnapshot = { el, value: el.value };
};

const onGridFocusIn = (e: FocusEvent) => {
  const el = e.target as GridCell;
  if (el && (el.tagName === 'INPUT' || el.tagName === 'SELECT')) gridRemember(el);
};

/** Внутри текстового поля ←/→ должны двигать каретку, и только на краю текста
 * уводить в соседнюю ячейку — как в Google Sheets. */
const gridCaretAtEdge = (el: GridCell, dir: 1 | -1): boolean => {
  if (el.tagName === 'SELECT') return true;
  const input = el as HTMLInputElement;
  // у type=number selectionStart недоступен — считаем, что край всегда достигнут
  let start: number | null = null;
  let end: number | null = null;
  try { start = input.selectionStart; end = input.selectionEnd; } catch { return true; }
  if (start === null || end === null) return true;
  if (start !== end) return false;
  return dir === 1 ? start >= input.value.length : start <= 0;
};

const onGridKeydown = (e: KeyboardEvent) => {
  const el = e.target as GridCell;
  if (!el || (el.tagName !== 'INPUT' && el.tagName !== 'SELECT')) return;
  if ((el as HTMLInputElement).type === 'checkbox') return;
  const td = el.closest('td');
  const tr = el.closest('tr');
  if (!td || !tr || !tr.closest('#cost-table-1')) return;
  // Alt+↓ — нативное раскрытие списка в селектах, не перехватываем
  if (e.altKey || e.ctrlKey || e.metaKey) return;

  gridRemember(el);

  const cellIndex = (td as HTMLTableCellElement).cellIndex;
  const moveRow = (dir: 1 | -1) => {
    if (gridFocus(gridSeekInColumn(tr, cellIndex, dir))) return;
    // границы страницы: продолжаем ввод на соседней, не трогая пагинацию руками
    if (dir === 1 && currentPage.value < totalPages.value - 1) {
      currentPage.value++;
      nextTick(() => gridFocusOnPage(cellIndex, true));
    } else if (dir === -1 && currentPage.value > 0) {
      currentPage.value--;
      nextTick(() => gridFocusOnPage(cellIndex, false));
    }
  };
  const moveCol = (dir: 1 | -1) => {
    if (gridFocus(gridSeekInRow(tr, cellIndex, dir))) return;
    // конец строки — переходим на начало следующей (поведение Tab в Excel)
    const nextRow = dir === 1 ? tr.nextElementSibling : tr.previousElementSibling;
    if (!nextRow) return moveRow(dir);
    const cells = Array.from((nextRow as HTMLTableRowElement).cells);
    const pool = dir === 1 ? cells : [...cells].reverse();
    for (const c of pool) {
      if (gridFocus(gridFocusableIn(c)[0] ?? null)) return;
    }
  };

  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault(); // иначе number-поле изменит значение, а select — выбор
      moveRow(1);
      break;
    case 'ArrowUp':
      e.preventDefault();
      moveRow(-1);
      break;
    case 'Enter':
      e.preventDefault();
      moveRow(e.shiftKey ? -1 : 1);
      break;
    case 'Tab':
      e.preventDefault();
      moveCol(e.shiftKey ? -1 : 1);
      break;
    case 'ArrowRight':
      if (!gridCaretAtEdge(el, 1)) return;
      e.preventDefault();
      moveCol(1);
      break;
    case 'ArrowLeft':
      if (!gridCaretAtEdge(el, -1)) return;
      e.preventDefault();
      moveCol(-1);
      break;
    case 'Escape': {
      e.preventDefault();
      // откат ввода: гоним значение через тот же @input/@change, что и обычную правку
      if (gridSnapshot.el === el && el.value !== gridSnapshot.value) {
        el.value = gridSnapshot.value;
        el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }));
      }
      gridSnapshot = { el: null, value: '' };
      el.blur();
      break;
    }
  }
};

const onTableKeydown = (e: KeyboardEvent) => {
  onCopyShortcut(e);
  if (!e.defaultPrevented) onGridKeydown(e);
};

// ── Lifecycle ───────────────────────────────────────────────────────────────

const USD_SORT_FIELDS = [
  'avg_Розничная цена по уровню, USD.',
  'avg_Отпускная цена по уровню, USD.',
  'sum_Основные материалы, USD.',
  'sum_Вспомогательные материалы, USD.',
  'sum_Пошив, USD.',
  'sum_Раскрой, USD.',
  'sum_Декоры, USD.',
  'sum_Вязание, USD.',
  'sum_Себестоимость, USD.',
];

watch(showUSD, (val) => {
  if (!val && USD_SORT_FIELDS.includes(sortField.value)) {
    sortField.value = '';
  }
});

watch(currentPage, () => {
  selectedRowIndex.value = -1;
});

// ── Approval popup modal ────────────────────────────────────────────────────

const showApprovalModal = ref(false);
const approvalPendingChanges = ref<any[]>([]);
const selectedPendingIds = ref<number[]>([]);
const approvalLoading = ref(false);
const approvalApplying = ref(false);
const approvalClearing = ref(false);
const approvalRejecting = ref(false);

async function openApprovalModal() {
  showApprovalModal.value = true;
  await Promise.all([
    loadApprovalFilterOptions(),
    loadApprovalPendingChanges(),
    loadApprovalMarginTargets(),
  ]);
}

async function loadApprovalPendingChanges() {
  approvalLoading.value = true;
  try {
    const params = buildApprovalFilterParams();
    const res = await $fetch<{ data: any[] }>(
      `${apiBase.value}/api/cost/pending-changes?${params}`,
      { headers: fetchHeaders.value }
    );
    approvalPendingChanges.value = res.data ?? [];
    selectedPendingIds.value = [];
  } catch (e: any) {
    console.error("[cost] load approval pending changes failed", e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    approvalLoading.value = false;
  }
}

function toggleSelectAllPending(e: Event) {
  const checked = (e.target as HTMLInputElement).checked;
  if (checked) {
    selectedPendingIds.value = approvalPendingChanges.value.map((p) => p.id);
  } else {
    selectedPendingIds.value = [];
  }
}

async function applyPendingChanges() {
  if (!selectedPendingIds.value.length) return;
  approvalApplying.value = true;
  try {
    // Build JSON for selected rows → SQL procedure
    const selected = approvalPendingChanges.value.filter((pc: any) =>
      selectedPendingIds.value.includes(pc.id)
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
          author_name: user.value?.email || 'system',
          cost_rub: Number(pc['Себестоимость, руб.'] ?? pc.cost_rub ?? 0),
        };
      })
      .filter(Boolean);
    console.log('[cost] SQL procedure payload:', JSON.stringify(procPayload));

    const res = await $fetch<{ success: boolean; applied: number; procPayload?: any[] }>(
      `${apiBase.value}/api/cost/pending-changes/apply`,
      {
        method: "POST",
        body: {
          ids: selectedPendingIds.value,
          reviewed_by: user.value?.name || 'system',
          proc_payload: procPayload,
        },
        headers: fetchHeaders.value,
      }
    );
    await loadApprovalPendingChanges();
  } catch (e: any) {
    console.error("[cost] apply pending changes failed", e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    approvalApplying.value = false;
  }
}

async function clearAllPendingChanges() {
  if (!confirm('Очистить таблицу согласования? Все необработанные изменения будут удалены.')) return;
  approvalClearing.value = true;
  try {
    await $fetch(`${apiBase.value}/api/cost/pending-changes/clear`, {
      method: "POST",
      headers: fetchHeaders.value,
    });
    await loadApprovalPendingChanges();
  } catch (e: any) {
    console.error("[cost] clear pending changes failed", e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    approvalClearing.value = false;
  }
}

async function rejectPendingChanges() {
  if (!selectedPendingIds.value.length) return;
  approvalRejecting.value = true;
  try {
    const selected = approvalPendingChanges.value.filter((pc: any) =>
      selectedPendingIds.value.includes(pc.id)
    );
    for (const pc of selected) {
      await $fetch(`${apiBase.value}/api/cost/reject-price`, {
        method: "POST",
        body: {
          model: pc['Модель'] ?? pc.model ?? '',
          articul: pc['Артикул'] ?? pc.articul ?? '',
          calc_sign: pc['Признак калькуляции'] ?? pc.calc_sign ?? null,
          plan_id: pc['PLAN_ID'] ?? pc.plan_id ?? null,
          date: pc['дата расчета'] ?? pc.date ?? null,
        },
        headers: fetchHeaders.value,
      });
    }
    await loadApprovalPendingChanges();
  } catch (e: any) {
    console.error("[cost] reject pending changes failed", e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  } finally {
    approvalRejecting.value = false;
  }
}

// ── Approval modal: calculated fields ─────────────────────────────────────────

const approvalMarginTargets = ref<Record<string, number | null>>({});

const calcApprovalModal = (row: any) => {
  const wholesale = Number(row['Отпускная цена по уровню, руб'] ?? row.wholesale_rub ?? 0);
  const cost = Number(row['Себестоимость, руб.'] ?? row.cost_rub ?? 0);
  const markup = wholesale - cost;
  const markupPct = cost > 0 ? (markup / cost) * 100 : 0;
  const marginPct = wholesale > 0 ? (markup / wholesale) * 100 : 0;
  return { markupRub: markup, markupPct, marginPct };
};

const marginDeviationModal = (row: any): number => {
  const target = approvalMarginTargets.value[row['Level 01'] ?? ''] ?? 0;
  const { marginPct } = calcApprovalModal(row);
  return marginPct - target;
};

const modalMarginDevText = (row: any): string => {
  const dev = marginDeviationModal(row);
  return (dev >= 0 ? '+' : '') + dev.toFixed(1) + '%';
};

const modalApprovalRowClass = (row: any): Record<string, boolean> => ({
  'modal-row-ok': marginDeviationModal(row) >= 0,
  'modal-row-bad': marginDeviationModal(row) < 0,
});

async function loadApprovalMarginTargets() {
  try {
    const targets = await $fetch<{ level1: string; target_margin_pct: number | null }[]>(
      `${apiBase.value}/api/cost/margin-targets`,
      { headers: fetchHeaders.value }
    );
    const map: Record<string, number | null> = {};
    for (const t of targets) {
      map[t.level1] = t.target_margin_pct;
    }
    approvalMarginTargets.value = map;
  } catch {
    approvalMarginTargets.value = {};
  }
}

// ── Approval modal: filters & cascade ──────────────────────────────────────────

const APPROVAL_LEVEL_KEYS = ["level01", "level02", "level03", "level04", "level05"];

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

const approvalFilterOptions = ref<Record<string, string[]>>({});
const approvalSelected = reactive<Record<string, string[]>>(
  Object.fromEntries(approvalFilterKeys.map((f) => [f.key, [] as string[]])) as any
);
const approvalQuery = ref("");
const approvalFilterBusy = ref(false);
let approvalFilterTimeout: ReturnType<typeof setTimeout> | null = null;

function isApprovalLocked(key: string): boolean {
  let lowestIdx = -1;
  for (let i = APPROVAL_LEVEL_KEYS.length - 1; i >= 0; i--) {
    if (approvalSelected[APPROVAL_LEVEL_KEYS[i]]?.length > 0) {
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

function buildApprovalFilterParams(): URLSearchParams {
  const params = new URLSearchParams();
  for (const [key, vals] of Object.entries(approvalSelected)) {
    if (!(vals as string[]).length) continue;
    for (const v of vals as string[]) params.append(key, v);
  }
  if (approvalQuery.value.trim()) params.set("q", approvalQuery.value.trim());
  return params;
}

async function loadApprovalFilterOptions() {
  approvalFilterBusy.value = true;
  try {
    const params = buildApprovalFilterParams();
    const raw = await $fetch<Record<string, string[]>>(
      `${apiBase.value}/api/cost/pending-changes/filter-options?${params}`,
      { headers: fetchHeaders.value }
    );
    approvalFilterOptions.value = raw;
  } catch (e: any) {
    console.error("[cost] load approval filter-options failed", e);
  } finally {
    approvalFilterBusy.value = false;
  }
}

async function onApprovalFilterChange(changedKey: string) {
  if (changedKey === "brand_manager") {
    for (const k of APPROVAL_LEVEL_KEYS) approvalSelected[k] = [];
  } else if (APPROVAL_LEVEL_KEYS.includes(changedKey)) {
    const idx = APPROVAL_LEVEL_KEYS.indexOf(changedKey);
    for (let i = idx + 1; i < APPROVAL_LEVEL_KEYS.length; i++) {
      approvalSelected[APPROVAL_LEVEL_KEYS[i]] = [];
    }
  }
  await loadApprovalFilterOptions();
  await loadApprovalPendingChanges();
}

function onApprovalFilterChangeDelayed() {
  if (approvalFilterTimeout) clearTimeout(approvalFilterTimeout);
  approvalFilterTimeout = setTimeout(async () => {
    await loadApprovalPendingChanges();
  }, 300);
}

async function resetApprovalFilters() {
  approvalQuery.value = "";
  for (const k of approvalFilterKeys) approvalSelected[k.key] = [];
  await loadApprovalFilterOptions();
  await loadApprovalPendingChanges();
}

/** Применить фильтры из URL-параметров (?calc_sign=...&plan_id=...) */
async function applyUrlFilters() {
  const route = useRoute();
  const calcSignParam = route.query.calc_sign;
  const planIdParam = route.query.plan_id;

  let hasUrlFilters = false;

  if (calcSignParam) {
    const vals = Array.isArray(calcSignParam) ? calcSignParam : [calcSignParam];
    const valid = vals.filter((v: string) => (filterOptions.value.calc_sign || []).includes(v));
    if (valid.length > 0) {
      selected.calc_sign = valid;
      hasUrlFilters = true;
    }
  }

  if (planIdParam) {
    const vals = Array.isArray(planIdParam) ? planIdParam : [planIdParam];
    const valid = vals.filter((v: string) => (filterOptions.value.plan_id || []).includes(v));
    if (valid.length > 0) {
      selected.plan_id = valid;
      hasUrlFilters = true;
    }
  }

  if (hasUrlFilters) {
    await loadData();
  }
}

onMounted(async () => {
  await Promise.all([loadFilters(), loadPriceLevels(), loadCacheStatus()]);
  await applyUrlFilters();
});

// ── Raw rows helpers ────────────────────────────────────────────────────────

/** Служебные колонки, которые не показываем в модалке исходных строк:
 * cost_factor_* — внутренний коэффициент связи «Норма × цена» с реальной
 * стоимостью статьи (миграция 0031), пользователю он не нужен. */
const RAW_ROWS_HIDDEN = new Set(['id', 'cost_factor_rub', 'cost_factor_usd']);

const rawRowsColumns = computed(() => {
  if (!rawRowsData.value.length) return [];
  return Object.keys(rawRowsData.value[0]).filter(k => !RAW_ROWS_HIDDEN.has(k));
});

const RAW_ROWS_NUMERIC = new Set([
  'Розничная цена по уровню, руб.', 'Отпускная цена по уровню, руб',
  'Розничная цена по уровню, USD.', 'Отпускная цена по уровню, USD.',
  'Пошив, руб.', 'Пошив, USD.', 'Раскрой, руб.', 'Раскрой, USD.',
  'Декоры, руб.', 'Декоры, USD.', 'Вязание, руб.', 'Вязание, USD.',
  'Основные материалы, руб.', 'Основные материалы, USD.',
  'Вспомогательные материалы, руб.', 'Вспомогательные материалы, USD.',
  'Курс на дату расчета',
  'Норма',
  'цена материала, руб.', 'цена материала, USD.',
]);

function isRawRowsNumeric(col: string): boolean {
  return RAW_ROWS_NUMERIC.has(col);
}

function rawRowsColumnLabel(col: string): string {
  // Shorten some long names for table headers
  const labels: Record<string, string> = {
    'Бренд-менеджер': 'Бренд-менеджер',
    'Наименование модели': 'Наименование',
    'Признак калькуляции': 'Пр.кальк',
    'дата расчета': 'Дата',
    'дата производства': 'Дата выпуска',
    'Номер задания производства': '№ задания',
    'Уровень цен': 'Уровень цен',
    'Страна пр-ва': 'Страна',
    'Материал/техоперация/декор(признак)': 'Тип',
    'Наименование': 'Материал',
    'артикул материала': 'Арт. материала',
    'свойство1': 'Св-во 1',
    'свойство2': 'Св-во 2',
    'свойство3': 'Св-во 3',
    'Норма': 'Норма',
    'цена материала, руб.': 'Цена мат, руб.',
    'цена материала, USD.': 'Цена мат, USD.',
    'Розничная цена по уровню, руб.': 'Розница, руб.',
    'Отпускная цена по уровню, руб': 'Опт, руб.',
    'Розничная цена по уровню, USD.': 'Розница, USD.',
    'Отпускная цена по уровню, USD.': 'Опт, USD.',
    'Основные материалы, руб.': 'Осн.мат, руб.',
    'Основные материалы, USD.': 'Осн.мат, USD.',
    'Вспомогательные материалы, руб.': 'Вспом.мат, руб.',
    'Вспомогательные материалы, USD.': 'Вспом.мат, USD.',
    'Пошив, руб.': 'Пошив, руб.',
    'Пошив, USD.': 'Пошив, USD.',
    'Раскрой, руб.': 'Раскрой, руб.',
    'Раскрой, USD.': 'Раскрой, USD.',
    'Декоры, руб.': 'Декоры, руб.',
    'Декоры, USD.': 'Декоры, USD.',
    'Вязание, руб.': 'Вязание, руб.',
    'Вязание, USD.': 'Вязание, USD.',
    'Курс на дату расчета': 'Курс',
  };
  return labels[col] || col;
}

/** Колонки, которые показываем с нативной точностью источника, а не в копейках:
 * Норма — до 6 знаков, цены материалов и стоимости статей — до 4. Иначе строки
 * с ценой 0.0005 выглядят как 0.00 (см. миграцию 0030). */
const RAW_ROWS_NORM_COLS = new Set(['Норма']);
const RAW_ROWS_PRICE4_COLS = new Set([
  'цена материала, руб.', 'цена материала, USD.',
  'Основные материалы, руб.', 'Основные материалы, USD.',
  'Вспомогательные материалы, руб.', 'Вспомогательные материалы, USD.',
  'Пошив, руб.', 'Пошив, USD.', 'Раскрой, руб.', 'Раскрой, USD.',
  'Декоры, руб.', 'Декоры, USD.', 'Вязание, руб.', 'Вязание, USD.',
  'Курс на дату расчета',
]);

function formatRawRowsCell(val: any, col: string): string {
  if (val === null || val === undefined) return '—';
  if (isRawRowsNumeric(col)) {
    if (RAW_ROWS_NORM_COLS.has(col)) return fmtNorm(val);
    if (RAW_ROWS_PRICE4_COLS.has(col)) return fmtPrice4(val);
    const n = Number(val);
    if (isNaN(n)) return String(val);
    return n.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  }
  const s = String(val);
  return s.includes('T') ? s.split('T')[0] : s;
}

const rawRowsFilterSummary = computed(() => {
  const row = detailsAggregatedRow.value;
  if (!row) return {};
  const summary: Record<string, string> = {};
  for (const field of AGG_GROUP_FIELDS) {
    const val = row[field];
    if (val !== null && val !== undefined && val !== '' && val !== '—') {
      let display = String(val);
      if (display.includes('T')) display = display.split('T')[0];
      summary[field] = display;
    }
  }
  return summary;
});

// ── Heatmap (details table conditional formatting) ─────────────────────────

const DETAILS_NUMERIC_FIELDS = [
  'Розничная цена, руб.',
  'Оптовая цена, руб.',
  'Осн. материалы, руб.',
  'Вспом. материалы, руб.',
  'Пошив, руб.',
  'Раскрой, руб.',
  'Декор, руб.',
  'Вязание, руб.',
  'Себестоимость, руб.',
  'Наценка, руб.',
  'Наценка, %',
  'Маржинальность, %',
];

const detailsRanges = computed(() => {
  const ranges: Record<string, { min: number; max: number }> = {};
  const data = detailsFilteredData.value;
  if (!data || data.length === 0) return ranges;

  for (const field of DETAILS_NUMERIC_FIELDS) {
    let min = Infinity;
    let max = -Infinity;
    for (const row of data) {
      const v = Number(row[field]);
      if (!isNaN(v)) {
        if (v < min) min = v;
        if (v > max) max = v;
      }
    }
    ranges[field] = min === Infinity
      ? { min: 0, max: 0 }
      : { min, max };
  }
  return ranges;
});

function heatBg(value: any, field: string): { backgroundColor?: string } {
  const n = Number(value);
  if (isNaN(n)) return {};

  const range = detailsRanges.value[field];
  if (!range || range.max === range.min) return {};

  // Blue gradient: white (low) → blue (high)
  // rgb(240, 245, 255) → rgb(50, 100, 220)
  const t = (n - range.min) / (range.max - range.min);
  const r = Math.round(240 - 190 * t);
  const g = Math.round(245 - 145 * t);
  const b = Math.round(255 - 35 * t);

  return { backgroundColor: `rgb(${r}, ${g}, ${b})` };
}
</script>

<style scoped>
.page-cost { padding-top: var(--sp-2); }

.page-subtitle {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
}
.ctx-sep { color: var(--text-muted); }
.mock-pill {
  display: inline-flex;
  align-items: center;
  font-size: var(--fs-2xs);
  padding: 2px 8px;
  border-radius: var(--rd-pill);
  background: color-mix(in srgb, var(--warn, #d97706) 14%, var(--bg-surface));
  color: var(--warn, #b45309);
  font-weight: var(--fw-semibold);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.filters-card {
  margin-bottom: var(--sp-4);
  overflow: visible;
}
.card-actions {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
}
.filters-body { position: relative; padding: var(--sp-4) var(--sp-5); }
.filters-body.is-busy { pointer-events: none; opacity: 0.6; }

/* Заголовок «Фильтры» работает как кнопка сворачивания — вся зона вместе с
   подписью кликабельна, чтобы не искать мелкую иконку. */
.filters-toggle {
  display: flex;
  align-items: flex-start;
  gap: var(--sp-2);
  cursor: pointer;
  user-select: none;
  border-radius: var(--radius-sm, 4px);
}
.filters-toggle:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.filters-chevron {
  flex: 0 0 auto;
  margin-top: 2px;
  color: var(--text-muted);
}
/* Счётчик заданных условий — виден только когда панель свёрнута, иначе
   непонятно, почему выдача сузилась. */
.filters-badge {
  display: inline-block;
  margin-left: var(--sp-2);
  padding: 0 6px;
  border-radius: 999px;
  background: var(--accent);
  color: #fff;
  font-size: var(--fs-2xs, 10px);
  font-weight: var(--fw-bold, 700);
  line-height: 16px;
  vertical-align: middle;
}

/* Выбор числа строк на странице — рядом с постраничной навигацией. */
.page-size {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  font-size: var(--fs-xs);
}
.page-size-select {
  padding: 2px 6px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm, 4px);
  background: var(--bg-surface);
  color: var(--text);
  font: inherit;
  font-variant-numeric: tabular-nums;
}
.filters-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--sp-3);
  background: rgba(255, 255, 255, 0.55);
  backdrop-filter: blur(2px);
  font-size: var(--fs-sm);
  color: var(--text-muted);
}

.dates-row {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  margin-bottom: var(--sp-3);
}
.dates-row label {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  font-size: var(--fs-xs);
  color: var(--text-muted);
}
.dates-row input {
  height: 28px;
  padding: 0 var(--sp-2);
  border: 1px solid var(--border);
  background: var(--bg-surface);
  border-radius: var(--rd-2);
  font-size: var(--fs-2xs, 11px);
}

.filters-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: var(--sp-3);
}
.filter-item label {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  display: block;
  margin-bottom: var(--sp-2);
}
.filter-item.locked {
  opacity: 0.5;
}

/* Action bar */
.cost-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-2);
  padding: var(--sp-2) var(--sp-4);
  background: var(--bg-surface-2, var(--bg-surface));
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  margin-bottom: var(--sp-3);
  flex-wrap: wrap;
}
.cost-info {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  font-size: var(--fs-sm);
}
.cost-info strong { color: var(--text-strong); font-weight: var(--fw-semibold); }
.cost-info .info-ok { color: var(--pos); }
.cost-info .info-muted { color: var(--text-muted); }
.cost-info .spinning { animation: cost-spin 0.9s linear infinite; }

/* USD toggle */
.usd-toggle {
  display: inline-flex;
  align-items: center;
  cursor: pointer;
  gap: 6px;
  user-select: none;
}
.usd-toggle input {
  display: none;
}
.usd-toggle-track {
  width: 36px;
  height: 20px;
  background: var(--border, #d1d5db);
  border-radius: 10px;
  position: relative;
  transition: background 0.2s;
}
.usd-toggle input:checked + .usd-toggle-track {
  background: var(--accent, #4f46e5);
}
.usd-toggle-thumb {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  background: #fff;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 8px;
  font-weight: 700;
  color: var(--text-muted, #6b7280);
  transition: left 0.2s, color 0.2s;
}
.usd-toggle input:checked + .usd-toggle-track .usd-toggle-thumb {
  left: 18px;
  color: var(--accent, #4f46e5);
}

.cost-actions-buttons {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
}

/* Modal */
.modal-wide { width: 95vw; max-width: 1600px; }
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 10000;
  display: flex;
  justify-content: center;
  align-items: center;
}
.modal-overlay--solid {
  background: rgba(30, 30, 30, 0.92);
}
.modal-content {
  background: var(--bg-surface);
  border-radius: var(--rd-3);
  max-height: 95vh;
  display: flex;
  flex-direction: column;
}
.modal-header {
  padding: var(--sp-4) var(--sp-5);
  border-bottom: 1px solid var(--border);
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-shrink: 0;
}
.modal-header h2 { margin: 0; font-size: var(--fs-lg); }
.modal-header-actions { display: flex; gap: var(--sp-3); align-items: center; }
.modal-close {
  border: none;
  background: transparent;
  font-size: 24px;
  cursor: pointer;
  color: var(--text-muted);
  padding: 0 4px;
}
.modal-close:hover { color: var(--text-strong); }
.form-input {
  height: 36px;
  padding: 0 var(--sp-3);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  background: var(--bg-surface);
  color: var(--text-strong);
  font-size: var(--fs-sm);
}

.details-filters { padding: var(--sp-3) var(--sp-4); background: var(--bg-surface-2); border-bottom: 1px solid var(--border); display: flex; gap: var(--sp-3); align-items: flex-end; flex-wrap: wrap; flex-shrink: 0; }
.details-filters label { font-size: var(--fs-2xs, 10px); font-weight: var(--fw-medium); display: flex; flex-direction: column; gap: 2px; color: var(--text-muted); }
.details-filters input[type="date"] { height: 28px; padding: 0 var(--sp-2); border: 1px solid var(--border); border-radius: var(--rd-2); background: var(--bg-surface); font-size: var(--fs-2xs, 11px); }

.details-col-filters { padding: var(--sp-3) var(--sp-5); border-bottom: 1px solid var(--border); display: flex; gap: var(--sp-3); align-items: flex-start; flex-wrap: wrap; flex-shrink: 0; }
.details-col-filter-item { display: flex; flex-direction: column; gap: 2px; min-width: 140px; max-width: 200px; flex: 1; }
.details-col-filter-item.locked { opacity: 0.5; }
.details-col-filter-item label { font-size: 10px; color: var(--text-muted); font-weight: var(--fw-medium); }
.details-col-filter-reset { font-size: var(--fs-xs); color: var(--text-muted); padding-top: 16px; white-space: nowrap; }
.details-col-filter-reset:hover { color: var(--text-strong); }

.table-wrap { overflow-x: auto; max-width: 100%; }
.details-table-wrap { overflow: auto; flex: 1; }
.details-count { padding: var(--sp-2) var(--sp-5); font-size: var(--fs-xs); color: var(--text-muted); border-top: 1px solid var(--border); flex-shrink: 0; }
.btn-details { background: none; border: none; cursor: pointer; padding: 2px 4px; font-size: 12px; }
.btn-details:hover { opacity: 0.7; }
/* Кнопки строк не наезжают друг на друга */
.data-table td:first-child { white-space: nowrap; }
.btn-edit { cursor:pointer; background:none; border:none; font-size:16px; padding:2px 4px; vertical-align:middle; margin-left:4px; color:var(--accent,#4338ca); }
.btn-edit:hover { color:var(--accent-hover,#6366f1); text-decoration:underline; }

/* Error banner */
.cost-error {
  display: flex;
  align-items: flex-start;
  gap: var(--sp-3);
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-3);
  background: color-mix(in srgb, var(--neg) 8%, var(--bg-surface));
  border: 1px solid color-mix(in srgb, var(--neg) 30%, transparent);
  color: var(--text-strong);
  margin-bottom: var(--sp-5);
  position: relative;
}
.cost-error :deep(svg) { color: var(--neg); flex-shrink: 0; margin-top: 2px; }
.cost-error p { margin-top: 4px; font-size: var(--fs-sm); }
.cost-error .muted { color: var(--text-muted); font-size: var(--fs-xs); margin-top: 6px; }
.cost-error code {
  background: var(--bg-surface-3);
  padding: 1px 5px;
  border-radius: var(--rd-1);
  font-family: var(--font-mono);
  font-size: 90%;
}
.cost-error-x {
  position: absolute;
  top: 6px;
  right: 6px;
  border: none;
  background: transparent;
  font-size: 18px;
  cursor: pointer;
  color: var(--text-muted);
  padding: 4px 8px;
  border-radius: var(--rd-1);
}
.cost-error-x:hover { color: var(--text-strong); background: var(--bg-surface-3); }

/* Tables */
.data-table {
  width: 100%;
  border-collapse: collapse;
  table-layout: auto;
}
.data-table th {
  cursor: pointer;
  user-select: none;
  white-space: normal;
  /* Не `break-word`: он разрешал рвать заголовок ПО БУКВАМ. Когда в колонке нет
     данных, браузер сжимал её до минимума контента — то есть до одного символа,
     — а заголовок вытягивался в вертикальный столбик и вся шапка вырастала на
     высоту самого длинного слова. `normal` рвёт только по пробелам. */
  word-break: normal;
  overflow-wrap: break-word;
  padding: 4px 6px;
  font-size: var(--fs-2xs, 10px);
  line-height: 1.25;
  vertical-align: bottom;
  text-align: left;
}
/* Высота шапки фиксирована: она больше не «дышит» при смене фильтров и набора
   колонок. Расчёт: шрифт 10px при line-height 1.25 даёт 12.5px на строку, три
   строки — 37.5px, плюс паддинги 4+4 = 45.5px. Берём 48px, чтобы самые длинные
   заголовки («Вспом. материалы (руб)») укладывались в три строки с запасом. */
.data-table thead th {
  height: 48px;
  box-sizing: border-box;
}
/* Минимальная ширина, чтобы пустая колонка не съезжала в один символ. На
   закреплённые колонки не распространяется — их ширина задаётся inline и
   настраивается ресайзом (служебные вроде «ПЭО-выбор» и вовсе уже 34px). */
.data-table th:not(.sticky-col),
.data-table td:not(.sticky-col) {
  min-width: 68px;
}
.data-table th.col-num { text-align: right; }
.data-table th:hover { background: var(--bg-surface-3); }
.data-table th.sorted { background: color-mix(in srgb, var(--accent) 10%, var(--bg-surface)); }
.sort-arrow { font-size: 10px; color: var(--accent); white-space: nowrap; }
.data-table td {
  padding: 3px 6px;
  font-size: var(--fs-2xs, 11px);
  line-height: 1.3;
  white-space: nowrap;
  min-width: 0;
}
.data-table.compact td,
.data-table.compact th {
  padding: 3px 5px;
}
.data-table tr.selected { background: var(--bg-surface-3); }
.data-table tbody tr { cursor: pointer; }
.data-table .price-select {
  height: 24px;
  border: 1px solid var(--border);
  background: var(--bg-surface);
  border-radius: var(--rd-2);
  font-size: var(--fs-2xs, 10px);
  padding: 0 2px;
  max-width: 130px;
  min-width: 80px;
  width: 100%;
  color: var(--text-strong);
}
.data-table .price-input {
  width: 100%;
  min-width: 80px;
  max-width: 130px;
  height: 24px;
  padding: 0 4px;
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  background: var(--bg-surface);
  font-size: var(--fs-2xs, 10px);
  text-align: right;
  font-variant-numeric: tabular-nums;
  font-family: 'JetBrains Mono', 'Consolas', monospace;
}
.data-table .num {
  font-variant-numeric: tabular-nums;
  font-family: 'JetBrains Mono', 'Consolas', monospace;
  text-align: right;
  white-space: nowrap;
}
.data-table .num-strong {
  font-variant-numeric: tabular-nums;
  font-family: 'JetBrains Mono', 'Consolas', monospace;
  text-align: right;
  font-weight: var(--fw-semibold, 600);
  white-space: nowrap;
}

/* Сетка. Hairline по дизайн-системе: тонкая линия токеном --border, без теней.
   Внешнюю рамку не рисуем — таблица и так лежит в карточке с рамкой. */
.data-table th,
.data-table td {
  border-right: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
}
.data-table th:last-child,
.data-table td:last-child { border-right: none; }
.data-table tbody tr:last-child td { border-bottom: none; }

/* Рентабельность и маржинальность — ключевые метрики строки, поэтому они
   заметно жирнее остальных чисел. */
.data-table .col-metric {
  font-weight: var(--fw-semibold, 600);
}
.data-table th.col-metric {
  font-weight: var(--fw-bold, 700);
}

/* Рентабельность, маржа и плановая рентабельность — то, на что смотрят в первую
   очередь, поэтому помимо жирного шрифта у них мягкая жёлтая заливка: колонку
   видно сразу, без пересчёта столбцов глазами (просьба заказчика 25.08.2026).
   Цвет берём из токена --warn-soft, а не хардкодом, — он определён и для тёмной
   темы. Фон ставим на саму ячейку: подсветка строк по отклонению маржи красит
   <tr> с !important, но фон <td> рисуется поверх, поэтому заливка сохраняется и
   на подсвеченных строках, и под курсором. */
.data-table .col-metric-hl {
  background: var(--warn-soft, #fdf3e3);
  font-weight: var(--fw-bold, 700);
}
.data-table th.col-metric-hl {
  background: var(--warn-soft, #fdf3e3);
}
/* Знаковая раскраска процентов внутри залитой ячейки должна оставаться
   читаемой, поэтому цифры там тоже жирные. */
.data-table td.col-metric-hl.delta-pos,
.data-table td.col-metric-hl.delta-neg {
  font-weight: var(--fw-bold, 700);
}

/* Sticky/frozen columns for the main aggregated table (positioning via inline styles) */
#cost-table-1.data-table .sticky-col {
  position: sticky;
  background: var(--bg-surface);
  z-index: 4;
}
/* На закреплённых колонках collapsed-границы не рисуются (ячейка уезжает из
   потока), поэтому линии сетки им даём внутренней тенью. */
#cost-table-1.data-table th.sticky-col,
#cost-table-1.data-table td.sticky-col {
  border-right: none;
  box-shadow: inset -1px 0 0 var(--border), inset 0 -1px 0 var(--border);
}
/* Последняя закреплённая колонка сохраняет тень-разделитель — и линию сетки
   тоже, иначе стык с прокручиваемой частью выглядит пустым. */
#cost-table-1.data-table .sticky-col.is-last-sticky {
  box-shadow: inset -1px 0 0 var(--border), inset 0 -1px 0 var(--border), 3px 0 6px rgba(0, 0, 0, 0.06);
}
/* Restore selected/hover/sorted backgrounds on sticky cells */
#cost-table-1.data-table tr.selected td.sticky-col {
  background: var(--bg-surface-3);
}
#cost-table-1.data-table th.sticky-col:hover {
  background: var(--bg-surface-3);
}
#cost-table-1.data-table th.sticky-col.sorted {
  background: color-mix(in srgb, var(--accent) 10%, var(--bg-surface));
}
/* Resize handle on pinned column headers */
#cost-table-1.data-table .col-resize-handle {
  position: absolute;
  top: 0;
  right: -3px;
  width: 7px;
  height: 100%;
  cursor: col-resize;
  z-index: 5;
  touch-action: none;
}
#cost-table-1.data-table .col-resize-handle:hover {
  background: color-mix(in srgb, var(--accent) 30%, transparent);
}

.loader {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  border: 2px solid var(--border);
  border-top-color: var(--accent);
  animation: cost-spin 0.8s linear infinite;
}
@keyframes cost-spin {
  to { transform: rotate(360deg); }
}

/* Column filters */
.col-filters {
  display: flex;
  gap: var(--sp-2);
  align-items: flex-start;
  flex-wrap: wrap;
  padding: var(--sp-2) var(--sp-4);
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface-2, var(--bg-surface));
  flex-shrink: 0;
}
.col-filters.is-active {
  background: color-mix(in srgb, var(--accent) 6%, var(--bg-surface-2, var(--bg-surface)));
  border-left: 3px solid var(--accent);
}
.col-filter-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 140px;
  max-width: 200px;
  flex: 1;
}
.col-filter-item.locked {
  opacity: 0.5;
}
.col-filter-item label {
  font-size: 10px;
  color: var(--text-muted);
  font-weight: var(--fw-medium);
}
.col-filter-reset {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  padding-top: 16px;
  white-space: nowrap;
  text-decoration: none;
}
.col-filter-reset:hover {
  color: var(--accent);
  text-decoration: underline;
}

/* Column numeric alignment */
.data-table .col-num { text-align: right; }
.delta-pos { color: var(--pos, #16a34a); font-weight: var(--fw-medium, 500); }
.delta-neg { color: var(--neg, #dc2626); font-weight: var(--fw-medium, 500); }

/* Row-level margin deviation conditional formatting */
.row-margin-ok {
  background-color: color-mix(in srgb, var(--pos, #16a34a) 8%, transparent) !important;
}
.row-margin-ok:hover {
  background-color: color-mix(in srgb, var(--pos, #16a34a) 14%, transparent) !important;
}
.row-margin-bad {
  background-color: color-mix(in srgb, var(--neg, #dc2626) 8%, transparent) !important;
}
.row-margin-bad:hover {
  background-color: color-mix(in srgb, var(--neg, #dc2626) 14%, transparent) !important;
}

/* Cache status */
.cost-cache-status {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
  font-size: var(--fs-sm);
}
.cache-spinner {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  color: var(--accent);
  font-size: var(--fs-xs);
}
.cache-info {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  color: var(--text-muted);
  font-size: var(--fs-xs);
}
.cache-notification {
  display: inline-flex;
  align-items: center;
  padding: var(--sp-1) var(--sp-3);
  background: #fef3cd;
  color: #856404;
  border-radius: var(--radius-md);
  font-size: var(--fs-xs);
  white-space: nowrap;
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

/* Approval modal — full-screen */
.approval-modal-content {
  width: 98vw;
  height: 96vh;
  max-width: 98vw;
  max-height: 96vh;
  display: flex;
  flex-direction: column;
}
.approval-modal-body {
  padding: var(--sp-3) var(--sp-4);
  overflow: auto;
  flex: 1;
}
.approval-modal-footer {
  padding: var(--sp-3) var(--sp-5);
  border-top: 1px solid var(--border);
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-shrink: 0;
}

/* Approval modal — filter bar */
.approval-filters {
  margin-bottom: var(--sp-3);
  padding: var(--sp-2) var(--sp-3);
  background: var(--bg-surface-2, var(--bg-surface));
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
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
.approval-table-scroll {
  overflow-x: auto;
}

/* Approval table — grid + formatting */
.approval-table {
  width: 100%;
  border-collapse: collapse;
  border: 1px solid var(--border);
  font-size: var(--fs-2xs, 11px);
}
.approval-table th,
.approval-table td {
  padding: 3px 5px;
  border: 1px solid var(--border);
  vertical-align: middle;
  background: var(--bg-surface);
}
.approval-table th {
  background: var(--bg-surface-2, var(--bg-surface));
  font-weight: 600;
  white-space: normal;
  word-break: break-word;
  position: sticky;
  top: 0;
  z-index: 1;
  font-size: var(--fs-2xs, 10px);
  line-height: 1.2;
}
.approval-table td {
  white-space: nowrap;
}
.approval-table .num {
  font-variant-numeric: tabular-nums;
  font-family: 'JetBrains Mono', 'Consolas', monospace;
  text-align: right;
  white-space: nowrap;
}
.approval-table th input[type="checkbox"],
.approval-table td input[type="checkbox"] {
  width: 14px;
  height: 14px;
  cursor: pointer;
}

/* Row-level conditional formatting */
.modal-row-ok {
  background-color: color-mix(in srgb, var(--pos, #16a34a) 8%, transparent) !important;
}
.modal-row-ok:hover {
  background-color: color-mix(in srgb, var(--pos, #16a34a) 14%, transparent) !important;
}
.modal-row-bad {
  background-color: color-mix(in srgb, var(--neg, #dc2626) 8%, transparent) !important;
}
.modal-row-bad:hover {
  background-color: color-mix(in srgb, var(--neg, #dc2626) 14%, transparent) !important;
}
.modal-row-ok td,
.modal-row-bad td {
  background-color: transparent !important;
}

/* Filter checkbox */
.filter-checkbox-row {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  margin-top: var(--sp-2);
  flex-wrap: wrap;
}
.filter-checkbox {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  font-size: var(--fs-xs);
  cursor: pointer;
  user-select: none;
}
.filter-checkbox input[type="checkbox"] {
  width: 16px;
  height: 16px;
  cursor: pointer;
}
/* Фильтр, включённый ролью: видно, что он не выключается, но выглядит не
   «сломанным», а обязательным. */
.peo-filter-label {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  font-size: var(--fs-xs);
  color: var(--text-muted);
  cursor: pointer;
  user-select: none;
}
.peo-filter-select { padding:4px 8px; border:1px solid var(--border-color, #d1d5db); border-radius:4px; font-size:13px; }

.raw-rows-banner {
  padding: var(--sp-3) var(--sp-5);
  background: color-mix(in srgb, var(--accent) 6%, var(--bg-surface-2, var(--bg-surface)));
  border-bottom: 1px solid var(--border);
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-2);
  align-items: center;
  font-size: var(--fs-xs);
  flex-shrink: 0;
}
.raw-rows-banner strong {
  color: var(--text-muted);
  margin-right: var(--sp-1);
}
.raw-rows-tag {
  display: inline-flex;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-1);
  padding: 1px 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  white-space: nowrap;
}
.raw-rows-count {
  margin-left: auto;
  color: var(--text-muted);
  font-weight: var(--fw-medium);
}
.raw-rows-table-wrap {
  overflow: auto;
  flex: 1;
}

tr.locked { opacity:0.55; }
tr.row-pending { background-color: color-mix(in srgb, #d97706 10%, transparent) !important; }
tr.row-audit { background-color: color-mix(in srgb, #059669 10%, transparent) !important; }
.lock-icon { display:inline-flex; align-items:center; justify-content:center; width:18px; height:18px; font-size:12px; margin-right:2px; vertical-align:middle; cursor:help; }
.state-badge { display:inline-flex; align-items:center; justify-content:center; width:20px; height:20px; border-radius:4px; font-size:12px; margin-right:2px; vertical-align:middle; cursor:help; }
.state-badge--pending { background:#fef3c7; color:#92400e; }
.state-badge--audit { background:#d1fae5; color:#065f46; }
/* Переоткрытая калькуляция: в DWH записана, но правка разрешена. */
.state-badge--reopened { background: var(--warn-soft, #fdf3e3); color: var(--warn, #b76e00); }
.state-badge--action { cursor: pointer; }
.state-badge--action:hover { filter: brightness(0.95); }
.reopen-note { margin: var(--sp-2) 0; font-size: var(--fs-xs); color: var(--text-muted); }
.reopen-note--warn { color: var(--warn, #b76e00); }
.reopen-error { margin-top: var(--sp-2); font-size: var(--fs-xs); color: var(--neg, #dc2626); }
.version-editor-toolbar { display:flex; gap:8px; align-items:center; padding:8px 16px; border-bottom:1px solid var(--border-color, #e5e7eb); }
.version-editor-toolbar .spacer { flex:1; }
.version-selector-bar { display:flex; gap:8px; align-items:center; padding:8px 16px; border-bottom:1px solid var(--border-color, #e5e7eb); font-size:13px; }
.version-selector-bar label { color: var(--text-secondary, #6b7280); }
.version-select { width: auto; min-width: 280px; border:1px solid var(--border-color, #e5e7eb); padding:4px 8px; font-size:13px; background:#fff; border-radius:4px; }
.version-select:disabled { background: var(--bg-tonal, #f3f4f6); color: var(--text-muted, #9ca3af); }
.version-select:focus { border-color:var(--accent-color, #4338ca); outline:none; }
.version-status-badge { padding:2px 8px; background: var(--bg-tonal, #f3f4f6); color: var(--text-secondary, #6b7280); border-radius:999px; font-size:11px; text-transform:uppercase; letter-spacing:0.04em; }
.version-editor-table-wrap { flex:1; overflow:auto; padding:0 16px 16px; }
.version-editor-table { width:100%; border-collapse:collapse; font-size:13px; }
.version-editor-table th, .version-editor-table td { padding:4px 6px; border:1px solid var(--border-color, #e5e7eb); text-align:left; white-space:nowrap; }
.version-editor-table .col-chk { width:32px; text-align:center; }

/* Цены предыдущих этапов — справочные, поэтому визуально отделены от текущих */
.version-editor-table .stage-col {
  background: color-mix(in srgb, var(--accent) 6%, transparent);
  color: var(--text-muted);
}
.version-editor-table th.stage-col { color: var(--text); font-style: italic; }
.stage-delta { font-size: var(--fs-2xs, 10px); margin-left: 4px; }

.stage-note {
  padding: 6px 16px;
  font-size: var(--fs-xs, 12px);
  color: var(--text-muted);
  border-bottom: 1px solid var(--border-color, #e5e7eb);
}
.stage-note b { color: var(--text); }

.stage-orphans { margin-top: var(--sp-4); display: flex; flex-direction: column; gap: var(--sp-3); }
.stage-orphan-head { font-size: var(--fs-xs); color: var(--text-muted); margin-bottom: 4px; }
.stage-orphan-table { width: auto; min-width: 480px; }
.version-editor-table .col-num { text-align:right; }
.editor-input { width:100%; border:1px solid transparent; padding:2px 4px; font-size:13px; background:transparent; }
.editor-input:focus { border-color:var(--accent-color, #4338ca); outline:none; background:#fff; }
.editor-select { width:100%; border:1px solid transparent; padding:2px 4px; font-size:13px; background:transparent; }
.editor-select:focus { border-color:var(--accent-color, #4338ca); outline:none; }
.row-added { background:#ecfdf5; }
.row-modified { background:#fefce8; }
.row-zero-cost { background: color-mix(in srgb, #ef4444 10%, transparent) !important; }
.type-tag.clickable { cursor:pointer; padding:2px 6px; border-radius:4px; background:var(--bg-tonal, #f3f4f6); border:1px solid var(--border-color, #e5e7eb); }
.type-tag.clickable:hover { background:var(--bg-hover, #e5e7eb); }
/* Активная ячейка Excel-навигации — это ячейка с полем в фокусе */
#cost-table-1 tbody td:focus-within {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
  background: color-mix(in srgb, var(--accent) 7%, transparent);
}
#cost-table-1 tbody td:focus-within .price-input,
#cost-table-1 tbody td:focus-within .comment-input,
#cost-table-1 tbody td:focus-within .price-select { outline: none; }

.col-peo { width:48px; text-align:center; }
.col-peo-sel { text-align:center; padding-left:0; padding-right:0; }
.col-peo-sel input { cursor:pointer; }
.col-peo-sel input:disabled { cursor:default; opacity:.35; }

/* Массовое согласование ПЭО */
.peo-bulk-bar {
  display:flex;
  flex-wrap:wrap;
  align-items:center;
  gap: var(--sp-3);
  padding: var(--sp-2) var(--sp-4);
  border-bottom:1px solid var(--border);
  border-left:3px solid var(--accent);
  background: color-mix(in srgb, var(--accent) 8%, var(--bg-surface-2, var(--bg-surface)));
  flex-shrink:0;
}
.peo-bulk-info { display:flex; align-items:center; gap: var(--sp-2); flex-wrap:wrap; }
.peo-bulk-actions { display:flex; align-items:center; gap: var(--sp-2); margin-left:auto; flex-wrap:wrap; }
.peo-bulk-comment {
  padding:4px 8px;
  border:1px solid var(--border-color, #d1d5db);
  border-radius:4px;
  font-size:var(--fs-xs, 12px);
  min-width:200px;
}
.peo-bulk-progress { flex-basis:100%; font-size:var(--fs-xs, 12px); color:var(--text-muted); }
.peo-bulk-result {
  display:flex;
  align-items:center;
  gap: var(--sp-3);
  padding: var(--sp-2) var(--sp-4);
  border-bottom:1px solid var(--border);
  font-size:var(--fs-sm, 13px);
}
.peo-bulk-ok { color: var(--pos, #16a34a); }
.peo-bulk-err { color: var(--neg, #dc2626); }
.peo-bulk-x { margin-left:auto; background:none; border:none; cursor:pointer; font-size:16px; color:var(--text-muted); }

.peo-badge { cursor:pointer; font-size:16px; }
.peo-readonly .peo-badge { cursor:default; }
.peo-active .peo-badge { background:rgba(99,102,241,0.15); border-radius:4px; }
.peo-filter-select { padding:4px 8px; border:1px solid var(--border-color, #d1d5db); border-radius:4px; font-size:13px; }
.approval-modal { width:400px; background:#fff; color:#1f2937; }
.approval-modal .modal-header { background:#fff; color:#1f2937; border-bottom:1px solid #e5e7eb; }
.approval-modal .approval-label { color:#374151; }
.approval-body { padding:16px; display:flex; flex-direction:column; gap:12px; }
.approval-status-row, .approval-info-row { display:flex; gap:8px; align-items:center; }
.approval-label { font-weight:500; color:#374151; min-width:80px; }
.approval-actions { display:flex; gap:8px; margin-top:4px; }
.approval-comment-row { display:flex; flex-direction:column; gap:4px; }
.approval-comment { width:100%; padding:6px 8px; border:1px solid var(--border-color, #d1d5db); border-radius:4px; font-size:13px; resize:vertical; font-family:inherit; }

/* Column visibility settings modal */
.colvis-modal { max-width: 640px; max-height: 80vh; display: flex; flex-direction: column; }
.colvis-body { padding: var(--sp-3) var(--sp-5); overflow-y: auto; flex: 1; display: flex; flex-direction: column; gap: var(--sp-4); }
.colvis-group { display: flex; flex-direction: column; gap: var(--sp-1); }
.colvis-group-title { font-size: var(--fs-xs); font-weight: var(--fw-semibold, 600); color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: var(--sp-1); }
.colvis-item { display: flex; align-items: center; gap: var(--sp-2); font-size: var(--fs-sm); cursor: pointer; padding: 2px 0; }
.colvis-item input[type="checkbox"] { width: 16px; height: 16px; cursor: pointer; }
.colvis-footer { padding: var(--sp-3) var(--sp-5); border-top: 1px solid var(--border); display: flex; justify-content: space-between; align-items: center; flex-shrink: 0; }
.colvis-footer-actions { display: flex; gap: var(--sp-2); }
.colvis-footer-buttons { display: flex; gap: var(--sp-3); }

/* Цены материалов по плану (миграция 0033). Цвета — только токены дизайн-системы. */
.plan-set-applied { background: var(--surface-accent, rgba(67, 56, 202, 0.06)); }
.plan-set-badge { color: var(--accent); font-weight: var(--fw-semibold, 600); font-size: var(--fs-xs); white-space: nowrap; }
/* Грид материалов набора. Фиксированная раскладка: ширины задаёт colgroup,
   иначе длинные наименования съедают место у колонок с ценами. */
/* Ошибка операций модалки цен по плану — заметная, но не модальная поверх
   модалки: пользователь должен увидеть причину, не теряя введённые цены. */
.plan-prices-error {
  display: flex;
  align-items: flex-start;
  gap: var(--sp-2);
  margin: 0 0 var(--sp-3);
  padding: 8px 10px;
  border: 1px solid var(--neg, #dc2626);
  border-radius: var(--radius-sm, 4px);
  background: color-mix(in srgb, var(--neg, #dc2626) 8%, transparent);
  color: var(--neg, #dc2626);
  font-size: var(--fs-xs);
}
.plan-prices-error .cost-error-x { margin-left: auto; }
.plan-prices-table { width: 100%; table-layout: fixed; }
/* Текстовые колонки переносятся по словам вместо растягивания в одну строку
   (общий стиль .data-table td ставит nowrap). Полный текст — в title. */
.plan-prices-table td.plan-cell-text {
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: break-word;
  line-height: 1.25;
}
.plan-prices-table th { white-space: normal; line-height: 1.2; }
/* Поля ввода цен занимают всю ширину своей колонки — в numeric(18,4)
   помещается до 4 знаков после запятой, они должны быть видны целиком. */
.plan-prices-table td.col-num input.editor-input {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
/* Строка, полностью перекрытая версией: цена из набора на неё не подействует. */
.plan-row-overridden { opacity: 0.55; }
.plan-ovr-full { color: var(--text-muted); font-size: var(--fs-xs); white-space: nowrap; }
.plan-ovr-part { color: var(--accent); font-size: var(--fs-xs); white-space: nowrap; }
/* Внутри группы были разные цены — применение набора поставит одну на все строки. */
.plan-spread-warn { color: var(--accent); cursor: help; margin-left: 4px; }

</style>
