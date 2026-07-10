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
    </header>

    <!-- Фильтры -->
    <section class="card filters-card">
      <div class="card-header">
        <div>
          <div class="card-title">Фильтры</div>
          <div class="card-subtitle">Top-down каскад — вышестоящие фильтры ограничивают нижестоящие</div>
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

      <div class="card-body filters-body" :class="{ 'is-busy': cascadeBusy }">
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

        <label class="filter-checkbox">
          <input v-model="noWholesaleOnly" type="checkbox" @change="loadData" />
          <span>Только строки без оптовой цены</span>
        </label>

        <select v-model="peoFilter" class="peo-filter-select" @change="loadData">
          <option value="all">Все статусы ПЭО</option>
          <option value="approved">Согласовано</option>
          <option value="none">Не согласовано</option>
          <option value="rejected">Отклонено</option>
        </select>

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
        <button v-if="can('cost:approve')" class="btn btn-ghost" @click="openApprovalModal">
          <Icon name="lucide:check-square" /> Согласование
        </button>
        <button v-if="can('cost:approve')" class="btn btn-ghost" @click="navigateTo('/cost/approvals')">
          <Icon name="lucide:clipboard-check" /> Страница согласования
        </button>
        <button v-if="can('cost:export')" class="btn btn-ghost" :disabled="!totalAllRecords" @click="exportToExcel">
          <Icon name="lucide:download" /> Экспорт в Excel
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
        <div class="card-actions" v-if="totalPages > 1">
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
      <div class="table-wrap" @keydown="onCopyShortcut" tabindex="0">
        <table id="cost-table-1" class="data-table compact">
          <thead>
            <tr>
              <th></th>
              <th></th>
              <th v-if="isVisible('bm')" :class="{ sorted: sortField === 'Бренд-менеджер' }" @click="toggleSort('Бренд-менеджер')">
                Бренд-менеджер<span v-if="sortField === 'Бренд-менеджер'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('model')" :class="{ sorted: sortField === 'Модель' }" @click="toggleSort('Модель')">
                Модель<span v-if="sortField === 'Модель'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('articul')" :class="{ sorted: sortField === 'Артикул' }" @click="toggleSort('Артикул')">
                Артикул<span v-if="sortField === 'Артикул'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('model_name')" :class="{ sorted: sortField === 'Наименование модели' }" @click="toggleSort('Наименование модели')">
                Наименование модели<span v-if="sortField === 'Наименование модели'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('task_num')" :class="{ sorted: sortField === 'Номер задания производства' }" @click="toggleSort('Номер задания производства')">
                № задания<span v-if="sortField === 'Номер задания производства'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('plan_id')" :class="{ sorted: sortField === 'PLAN_ID' }" @click="toggleSort('PLAN_ID')">
                PLAN_ID<span v-if="sortField === 'PLAN_ID'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
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
              <th v-if="isVisible('calc_markup')" class="col-num" :class="{ sorted: sortField === 'calc_markup_rub' }" @click="toggleSort('calc_markup_rub')">
                Рентабельность <template v-if="showUSD">($)</template><template v-else>(руб)</template><span v-if="sortField === 'calc_markup_rub'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('calc_markup_pct')" class="col-num" :class="{ sorted: sortField === 'calc_markup_pct' }" @click="toggleSort('calc_markup_pct')">
                Рентабельность (%)<span v-if="sortField === 'calc_markup_pct'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('calc_margin_pct')" class="col-num" :class="{ sorted: sortField === 'calc_margin_pct' }" @click="toggleSort('calc_margin_pct')">
                Маржа (%)<span v-if="sortField === 'calc_margin_pct'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="isVisible('calc_margin_deviation')" class="col-num" :class="{ sorted: sortField === 'calc_margin_deviation' }" @click="toggleSort('calc_margin_deviation')">
                Откл. маржи (%)<span v-if="sortField === 'calc_margin_deviation'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="(can('cost:approve') || can('cost:peo_mark')) && isVisible('peo')" class="col-peo">ПЭО</th>
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
              <td>
                <span v-if="isRowLocked(row) && !row._has_pending && !row._has_audit" class="lock-icon" title="Строка заблокирована">🔒</span>
                <span v-if="row._has_pending" class="state-badge state-badge--pending" title="Ожидает согласования">⏳</span>
                <span v-if="row._has_audit" class="state-badge state-badge--audit" title="Записано в DWH">📤</span>
                <span v-if="row.version_status === 'draft'" class="draft-badge draft-badge--draft" title="Черновик">✎</span>
                <span v-if="row.version_status === 'pending'" class="draft-badge draft-badge--pending" title="Ожидает утверждения">⏳</span>
                <button class="btn-details" @click.stop="openDetails(row)">🔍</button>
                <button class="btn-edit" @click.stop="openVersionEditor(row)" title="Редактировать расчёт">🖊</button>
              </td>
              <td><button class="btn-details" @click.stop="openRawRows(row)" title="Исходные строки">📋</button></td>
              <td v-if="isVisible('bm')">{{ row['Бренд-менеджер'] || '—' }}</td>
              <td v-if="isVisible('model')">{{ row['Модель'] || '—' }}</td>
              <td v-if="isVisible('articul')">{{ row['Артикул'] || '—' }}</td>
              <td v-if="isVisible('model_name')">{{ row['Наименование модели'] || '—' }}</td>
              <td v-if="isVisible('task_num')">{{ row['Номер задания производства'] || '—' }}</td>
              <td v-if="isVisible('plan_id')">{{ row['PLAN_ID'] || '—' }}</td>
              <td v-if="isVisible('country')">{{ row['Страна пр-ва'] || '—' }}</td>
              <td v-if="isVisible('family')">{{ row['Семья'] || '—' }}</td>
              <td v-if="isVisible('season')">{{ row['Сезон'] || '—' }}</td>
              <td v-if="isVisible('date')" class="num">{{ formatDate(row['дата расчета']) }}</td>
              <td v-if="isVisible('calc_sign')">{{ row['Признак калькуляции'] || '—' }}</td>
              <td v-if="isVisible('planned_retail')" class="col-num num">{{ row.planned_retail != null ? fmt(row.planned_retail) : '—' }}</td>
              <td v-if="isVisible('planned_wholesale')" class="col-num num">{{ row.planned_wholesale != null ? fmt(row.planned_wholesale) : '—' }}</td>
              <td v-if="isVisible('avg_retail_rub')">
                <select
                  class="price-select"
                  :value="row['avg_Розничная цена по уровню, руб.'] || ''"
                  :disabled="row['Признак калькуляции'] === 'ФКСС' || !can('cost:edit_price') || isRowLocked(row)"
                  @click.stop
                  @change="onRetailPriceSelect(getOriginalIndex(row), ($event.target as HTMLSelectElement).value)"
                >
                  <option value="">—</option>
                  <option v-for="rp in uniqueRetailPrices" :key="rp" :value="rp">{{ fmt(rp) }}</option>
                </select>
              </td>
              <td v-if="isVisible('avg_rate')" class="col-num num">{{ row['avg_Курс на дату расчета'] != null ? fmt(row['avg_Курс на дату расчета']) : '—' }}</td>
              <td v-if="isVisible('retail_markup')">
                <select
                  class="price-select"
                  :value="markupSelections[getOriginalIndex(row)] || ''"
                  :disabled="row['Признак калькуляции'] === 'ФКСС' || !row['avg_Розничная цена по уровню, руб.'] || !can('cost:edit_price') || isRowLocked(row)"
                  @click.stop
                  @change="onMarkupSelect(getOriginalIndex(row), ($event.target as HTMLSelectElement).value)"
                >
                  <option value="">—</option>
                  <option v-for="opt in getMarkupOptions(row)" :key="opt.value" :value="opt.value">
                    {{ opt.label }}
                  </option>
                </select>
              </td>
              <td v-if="isVisible('price_rf')">
                 <input class="price-input" type="number"
                  :value="priceRF[getOriginalIndex(row)] ?? ''"
                  placeholder="Цена РФ"
                  :disabled="!can('cost:edit_price') || isRowLocked(row)"
                  @click.stop
                  @input="onPriceRFInput(getOriginalIndex(row), ($event.target as HTMLInputElement).value)"
                />
              </td>
              <td v-if="isVisible('price_kz')">
                 <input class="price-input" type="number"
                  :value="priceKZ[getOriginalIndex(row)] ?? ''"
                  placeholder="Цена КЗ"
                  :disabled="!can('cost:edit_price') || isRowLocked(row)"
                  @click.stop
                  @input="onPriceKZInput(getOriginalIndex(row), ($event.target as HTMLInputElement).value)"
                />
              </td>
              <td v-if="isVisible('price_uz')">
                 <input class="price-input" type="number"
                  :value="priceUZ[getOriginalIndex(row)] ?? ''"
                  placeholder="Цена УЗ"
                  :disabled="!can('cost:edit_price') || isRowLocked(row)"
                  @click.stop
                  @input="onPriceUZInput(getOriginalIndex(row), ($event.target as HTMLInputElement).value)"
                />
              </td>
              <td v-if="isVisible('comment')">
                <input class="comment-input" type="text"
                  :value="comments[getOriginalIndex(row)] ?? ''"
                  placeholder="..."
                  :disabled="isRowLocked(row)"
                  @click.stop
                  @input="onCommentInput(getOriginalIndex(row), ($event.target as HTMLInputElement).value)"
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
              <td v-if="isVisible('calc_markup')" class="col-num num">{{ fmt(calc(row, showUSD).markupRub) }}</td>
              <td v-if="isVisible('calc_markup_pct')" class="col-num num" :class="calc(row, showUSD).markupPct >= 0 ? 'delta-pos' : 'delta-neg'">
                {{ calc(row, showUSD).markupPct.toFixed(1) }}%
              </td>
              <td v-if="isVisible('calc_margin_pct')" class="col-num num">{{ calc(row, showUSD).marginPct.toFixed(1) }}%</td>
              <td v-if="isVisible('calc_margin_deviation')" class="col-num num" :class="marginDevClass(row, showUSD)">{{ marginDevText(row, showUSD) }}</td>
              <td v-if="(can('cost:approve') || can('cost:peo_mark')) && isVisible('peo')" class="col-peo">
                <span v-if="row.peo_status === 'approved'" class="peo-badge peo-approved" :title="'Согласовано: ' + (row.peo_approved_by || '—') + (row.peo_approved_at ? ' ' + new Date(row.peo_approved_at).toLocaleDateString('ru-RU') : '')" @click.stop="openApprovalPopup(row)">🟢</span>
                <span v-else-if="row.peo_status === 'rejected'" class="peo-badge peo-rejected" @click.stop="openApprovalPopup(row)">🔴</span>
                <span v-else class="peo-badge peo-none" @click.stop="openApprovalPopup(row)">⚪</span>
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
              <tr v-for="(r, ri) in rawRowsData" :key="ri">
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
            <div class="modal-header-actions">
              <button class="modal-close" @click="closeVersionEditor">×</button>
            </div>
          </div>
          <div class="version-editor-toolbar">
            <button class="btn btn-sm" @click="addVersionRow">+ Добавить строку</button>
            <button class="btn btn-sm btn-danger" @click="deleteSelectedRows">✕ Удалить</button>
            <span class="spacer"></span>
            <button class="btn btn-sm" :disabled="savingDraft" @click="saveDraft">{{ savingDraft ? 'Сохранение…' : '💾 Сохранить' }}</button>
            <button class="btn btn-sm btn-primary" :disabled="submittingDraft" @click="submitDraft">{{ submittingDraft ? 'Отправка…' : '📨 Отправить на утверждение' }}</button>
          </div>
          <div class="version-editor-table-wrap">
            <table class="version-editor-table">
              <thead>
                <tr>
                  <th class="col-chk"><input type="checkbox" @change="(e: any) => editingVersion?.rows.forEach(r => r._selected = (e.target as HTMLInputElement).checked)" /></th>
                  <th>Материал/операция</th>
                  <th>Наименование</th>
                  <th>Артикул материала</th>
                  <th class="col-num">Норма</th>
                  <th class="col-num">Цена, руб.</th>
                  <th class="col-num">Цена, USD</th>
                  <th class="col-num">Курс</th>
                  <th class="col-num">Сумма, руб.</th>
                  <th>Комментарий</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(vr, vi) in editingVersion.rows" :key="vi"
                  :class="{ 'row-added': vr.change_type === 'added', 'row-modified': vr.change_type === 'modified' }">
                  <td><input type="checkbox" v-model="vr._selected" /></td>
                  <td>
                    <select :value="vr['Материал/операция/декор(призн)']" class="editor-select" @change="onVersionRowEdit(vr, $event, 'Материал/операция/декор(призн)')">
                      <option value="материал">материал</option>
                      <option value="техоперация">техоперация</option>
                      <option value="декор">декор</option>
                    </select>
                  </td>
                  <td><input :value="vr['Наименование']" class="editor-input" @input="onVersionRowEdit(vr, $event, 'Наименование')" /></td>
                  <td><input :value="vr['артикул материала']" class="editor-input" @input="onVersionRowEdit(vr, $event, 'артикул материала')" /></td>
                  <td><input :value="vr['Норма']" type="number" step="0.01" class="editor-input col-num" @input="onVersionRowEdit(vr, $event, 'Норма')" /></td>
                  <td><input :value="vr['цена материала, руб.']" type="number" step="0.01" class="editor-input col-num" @input="onVersionRowEdit(vr, $event, 'цена материала, руб.')" /></td>
                  <td><input :value="vr['цена материала, USD.']" type="number" step="0.01" class="editor-input col-num" @input="onVersionRowEdit(vr, $event, 'цена материала, USD.')" /></td>
                  <td><input :value="vr['Курс на дату расчета']" type="number" step="0.0001" class="editor-input col-num" @input="onVersionRowEdit(vr, $event, 'Курс на дату расчета')" /></td>
                  <td class="col-num">{{ ((vr['Норма'] || 0) * (vr['цена материала, руб.'] || 0)).toLocaleString('ru-RU', {minimumFractionDigits:2}) }}</td>
                  <td><input :value="vr.row_comment" class="editor-input" placeholder="..." @input="onVersionRowEdit(vr, $event, 'row_comment')" /></td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- PEO approval popup modal -->
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

const { can, loading: permLoading } = useCostPermission();
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
  { key: 'task_num', label: '№ задания' },
  { key: 'plan_id', label: 'PLAN_ID' },
  { key: 'country', label: 'Страна' },
  { key: 'family', label: 'Семья' },
  { key: 'season', label: 'Сезон' },
  { key: 'date', label: 'Дата' },
  { key: 'calc_sign', label: 'Пр.кальк' },
  { key: 'planned_retail', label: 'План. розница' },
  { key: 'planned_wholesale', label: 'План. опт' },
  { key: 'avg_retail_rub', label: 'Сред. розница (руб)' },
  { key: 'avg_rate', label: 'Курс (руб)' },
  { key: 'retail_markup', label: 'Розничная наценка' },
  { key: 'price_rf', label: 'Цена РФ' },
  { key: 'price_kz', label: 'Цена КЗ' },
  { key: 'price_uz', label: 'Цена УЗ' },
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
const mainColumnKeys = ['bm','model','articul','model_name','task_num','plan_id'];
const infoColumnKeys = ['country','family','season','date','calc_sign','planned_retail','planned_wholesale','avg_retail_rub','avg_rate','retail_markup','price_rf','price_kz','price_uz','comment'];
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

const STICKY_COL_KEYS = ['actions','raw_rows','bm','model','articul','model_name','task_num','plan_id'];
const STICKY_COL_WIDTHS = [70, 32, 160, 110, 90, 200, 120, 90];
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
  nextTick(() => recalcStickyColumns());
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
  for (const c of COLUMNS_CONFIG) {
    if (columnVisibility[c.key]) count++;
  }
  return count;
});

function recalcStickyColumns() {
  const table = document.getElementById('cost-table-1');
  if (!table) return;
  const headers = table.querySelectorAll('thead tr th');
  const rows = table.querySelectorAll('tbody tr');
  let left = 0;
  const positions: number[] = [];
  for (let i = 0; i < STICKY_COL_KEYS.length; i++) {
    const key = STICKY_COL_KEYS[i];
    const th = headers[i] as HTMLElement;
    if (!th) { positions.push(left); continue; }
    if (isVisible(key)) {
      positions.push(left);
      left += STICKY_COL_WIDTHS[i];
      th.style.left = positions[i] + 'px';
    } else {
      positions.push(-9999);
      th.style.left = '-9999px';
    }
  }
  for (const row of rows) {
    const cells = row.querySelectorAll('td');
    for (let i = 0; i < Math.min(cells.length, STICKY_COL_KEYS.length); i++) {
      const td = cells[i] as HTMLElement;
      if (isVisible(STICKY_COL_KEYS[i])) {
        td.style.left = positions[i] + 'px';
      } else {
        td.style.left = '-9999px';
      }
    }
  }
}

watch(showUSD, () => {
  nextTick(() => recalcStickyColumns());
});

// ── Margin targets state ─────────────────────────────────────────────────────

const showMarginModal = ref(false);
const marginTargetsLoading = ref(false);
const marginSaving = ref(false);
const marginSaveStatus = ref('');
const marginTargetsList = ref<{ level1: string; target_margin_pct: number | null }[]>([]);

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

// ── Data loading ────────────────────────────────────────────────────────────

const allAggregated = ref<any[]>([]);
const totalAllRecords = ref(0);
const pageSize = 25;

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
  if (currentPage.value * pageSize >= newLen && newLen > 0) {
    currentPage.value = 0;
  }
});

const currentPage = ref(0);
const totalPages = computed(() => Math.max(1, Math.ceil(sortedRows.value.length / pageSize)));
const pageStart = computed(() => currentPage.value * pageSize);
const pageRows = computed(() => sortedRows.value.slice(pageStart.value, pageStart.value + pageSize));
const pageRange = computed(() => {
  const filteredCount = sortedRows.value.length;
  if (!filteredCount) return "0";
  const a = pageStart.value + 1;
  const b = Math.min(pageStart.value + pageSize, filteredCount);
  return `${a}–${b}`;
});

async function loadData() {
  loading.value = true;
  try {
    const payload = buildFilters();
    if (peoFilter.value !== 'all') payload.peo_filter = peoFilter.value;
    const result = await $fetch<{ data: any[]; count: number }>(
      `${apiBase.value}/api/cost/aggregated`,
      { method: "POST", body: payload, headers: fetchHeaders.value }
    );
    allAggregated.value = result.data || [];
    totalAllRecords.value = result.count || 0;
    currentPage.value = 0;
    selectedRowIndex.value = -1;
    changedRows.clear();
    lastError.value = "";
    // Populate price_rf/kz/uz, comments and markup selections from response
    for (let i = 0; i < allAggregated.value.length; i++) {
      const r = allAggregated.value[i];
      if (r.price_rf != null) priceRF[i] = r.price_rf;
      if (r.price_kz != null) priceKZ[i] = r.price_kz;
      if (r.price_uz != null) priceUZ[i] = r.price_uz;
      if (r.comment != null) comments[i] = r.comment;
      // Авто-вычисляем наценку из розничной и оптовой цен строки
      const markup = computeMarkupFromRow(r);
      if (markup) markupSelections[i] = markup;
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

async function refreshCache() {
  try {
    const result = await $fetch<{ status: string; message?: string }>(
      `${apiBase.value}/api/cost/refresh-cache`,
      { method: "POST", headers: fetchHeaders.value }
    );
    if (result.status === "already_refreshing") {
      showCacheNotification(result.message || 'Обновление уже запущено');
      return;
    }
    if (result.status === "mock") return;

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
          }
        } catch {
          cacheRefreshing.value = false;
        }
      }
    };
    poll();
  } catch (e: any) {
    console.error("[cost] refresh-cache failed", e);
    cacheRefreshing.value = false;
  }
}

// ── Price levels & save ─────────────────────────────────────────────────────

const priceLevels = ref<PriceLevel[]>([]);
const changedRows = reactive<Set<number>>(new Set());
const saving = ref(false);

/** Уникальные розничные цены из справочника уровней цен (для datalist). */
const uniqueRetailPrices = computed(() => {
  const prices = new Set(priceLevels.value.map(l => l.price_type3));
  return Array.from(prices).sort((a, b) => a - b);
});

/** Выбранное значение «Розничная наценка» по строке (индекс → value). */
const markupSelections = reactive<Record<number, string>>({});

/** Реактивные значения цен РФ, КЗ, УЗ по строке (индекс → value). Заполняются из price_type4/5/6 при выборе наценки, переопределяются пользователем. */
const priceRF = reactive<Record<number, number>>({});
const priceKZ = reactive<Record<number, number>>({});
const priceUZ = reactive<Record<number, number>>({});

const editingVersion = ref<{version_id: number | null, rows: any[], model: string, articul: string} | null>(null);
const savingDraft = ref(false);
const submittingDraft = ref(false);

/** Реактивные значения комментариев по строке (индекс → строка). */
const comments = reactive<Record<number, string>>({});

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

/** Обработчики ввода цен РФ, КЗ, УЗ. */
const onPriceRFInput = (absoluteIdx: number, value: string) => {
  const v = parseFloat(value);
  priceRF[absoluteIdx] = isNaN(v) ? 0 : v;
  changedRows.add(absoluteIdx);
};
const onPriceKZInput = (absoluteIdx: number, value: string) => {
  const v = parseFloat(value);
  priceKZ[absoluteIdx] = isNaN(v) ? 0 : v;
  changedRows.add(absoluteIdx);
};
const onPriceUZInput = (absoluteIdx: number, value: string) => {
  const v = parseFloat(value);
  priceUZ[absoluteIdx] = isNaN(v) ? 0 : v;
  changedRows.add(absoluteIdx);
};

const onCommentInput = (absoluteIdx: number, value: string) => {
  comments[absoluteIdx] = value || "";
  changedRows.add(absoluteIdx);
};

const openVersionEditor = async (row: any) => {
  const idx = getOriginalIndex(row);
  const r = allAggregated.value[idx];
  if (!r) return;
  try {
    const params = new URLSearchParams({
      model: r['Модель'] || '',
      articul: r['Артикул'] || '',
      calc_sign: r['Признак калькуляции'] || '',
      plan_id: r['PLAN_ID'] || '',
      date: r['дата расчета'] || '',
      username: username || 'system',
    });
    const data = await $fetch<{ version_id: number; rows: any[] }>(
      `${apiBase.value}/api/cost/checkout-calculation?${params}`,
      { headers: fetchHeaders.value }
    );
    editingVersion.value = {
      version_id: data.version_id,
      rows: (data.rows || []).map((rr: any) => ({...rr, _selected: false})),
      model: r['Модель'],
      articul: r['Артикул'],
    };
  } catch (e: any) {
    console.error('[cost] checkout failed', e);
    lastError.value = e?.data?.detail || e?.message || String(e);
  }
};

const saveDraft = async () => {
  if (!editingVersion.value) return;
  savingDraft.value = true;
  try {
    const resp = await fetch(`${apiBase.value}/api/cost/save-calculation-draft`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json', ...fetchHeaders.value},
      body: JSON.stringify({
        version_id: editingVersion.value.version_id,
        rows: editingVersion.value.rows,
      }),
    });
    if (!resp.ok) {
      const errData = await resp.json().catch(() => ({}));
      throw new Error(errData?.detail || `HTTP ${resp.status}`);
    }
    alert('Черновик сохранён');
  } catch (e) {
    alert('Ошибка при сохранении: ' + (e?.message || String(e)));
  } finally {
    savingDraft.value = false;
  }
};

const submitDraft = async () => {
  if (!editingVersion.value) return;
  if (!confirm('Отправить расчёт на утверждение?')) return;
  submittingDraft.value = true;
  try {
    const resp = await fetch(`${apiBase.value}/api/cost/submit-calculation-draft`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json', ...fetchHeaders.value},
      body: JSON.stringify({version_id: editingVersion.value.version_id}),
    });
    if (!resp.ok) {
      const errData = await resp.json().catch(() => ({}));
      throw new Error(errData?.detail || `HTTP ${resp.status}`);
    }
    alert('Расчёт отправлен на утверждение');
    editingVersion.value = null;
  } catch (e) {
    alert('Ошибка при отправке: ' + (e?.message || String(e)));
  } finally {
    submittingDraft.value = false;
  }
};

const closeVersionEditor = () => { editingVersion.value = null; };

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
    template['Материал/операция/декор(призн)'] = 'материал';
    template['Наименование'] = '';
    template['артикул материала'] = '';
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
  // Parse numeric fields
  if (field === 'Норма' || field === 'цена материала, руб.' || field === 'цена материала, USD.' || field === 'Курс на дату расчета') {
    val = target.value === '' ? null : parseFloat(target.value);
  }
  row[field] = val;
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
  // Recalculate Основные материалы from Норма × цена материала
  if (field === 'Норма' || field === 'цена материала, руб.' || field === 'цена материала, USD.' || field === 'Курс на дату расчета') {
    const norm = row['Норма'] || 0;
    const priceRub = row['цена материала, руб.'] || 0;
    const priceUsd = row['цена материала, USD.'] || 0;
    row['Основные материалы, руб.'] = norm * priceRub;
    row['Основные материалы, USD.'] = norm * priceUsd;
  }
};

/** Найти индексы строк с тем же Модель+Артикул+PLAN_ID+Признак калькуляции (исключая excludeIdx). */
function findSiblingIndices(row: any, excludeIdx: number): number[] {
  const model = row['Модель'];
  const articul = row['Артикул'];
  const planId = row['PLAN_ID'];
  const calcSign = row['Признак калькуляции'];
  if (!model || !articul || !planId || !calcSign) return [];
  const key = `${model}|${articul}|${planId}|${calcSign}`;
  return allAggregated.value.reduce<number[]>((acc, r, i) => {
    if (i === excludeIdx) return acc;
    if (`${r['Модель']}|${r['Артикул']}|${r['PLAN_ID']}|${r['Признак калькуляции']}` === key) {
      acc.push(i);
    }
    return acc;
  }, []);
}

/** Выбор розничной цены из выпадающего списка: синхронизируем по всем строкам с тем же model+articul+plan_id+calc_sign. */
const onRetailPriceSelect = (absoluteIdx: number, value: string) => {
  const row = allAggregated.value[absoluteIdx];
  if (!row) return;
  const siblings = findSiblingIndices(row, absoluteIdx);
  const allIndices = [absoluteIdx, ...siblings];
  const numVal = parseFloat(value);

  const clearRow = (idx: number) => {
    const r = allAggregated.value[idx];
    r["avg_Розничная цена по уровню, руб."] = 0;
    r["avg_Отпускная цена по уровню, руб"] = 0;
    r["Уровень цен"] = "";
    r["avg_Розничная цена по уровню, USD."] = 0;
    r["avg_Отпускная цена по уровню, USD."] = 0;
    markupSelections[idx] = "";
    changedRows.add(idx);
  };
  const updateRow = (idx: number, val: number) => {
    const r = allAggregated.value[idx];
    r["avg_Розничная цена по уровню, руб."] = val;
    r["avg_Отпускная цена по уровню, руб"] = 0;
    r["Уровень цен"] = "";
    r["avg_Розничная цена по уровню, USD."] = 0;
    r["avg_Отпускная цена по уровню, USD."] = 0;
    markupSelections[idx] = "";
    changedRows.add(idx);
  };

  if (isNaN(numVal) || numVal <= 0) {
    allIndices.forEach(i => clearRow(i));
    return;
  }
  allIndices.forEach(i => updateRow(i, numVal));

  // Автовыбор если ровно один вариант наценки (только для текущей строки, onMarkupSelect синхронизирует сам)
  const options = getMarkupOptions(row);
  if (options.length === 1) {
    onMarkupSelect(absoluteIdx, options[0].value);
  }
};

/** Выбор наценки: находим соответствующий уровень цен, обновляем строку и сохраняем. */
const onMarkupSelect = async (absoluteIdx: number, markupValue: string) => {
  if (!markupValue) return;
  const row = allAggregated.value[absoluteIdx];
  if (!row) return;

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
  const siblings = findSiblingIndices(row, absoluteIdx);
  const allIndices = [absoluteIdx, ...siblings];
  for (const idx of allIndices) {
    const r = allAggregated.value[idx];
    r["avg_Розничная цена по уровню, руб."] = retailVal;
    r["avg_Отпускная цена по уровню, руб"] = wholesaleVal;
    r["Уровень цен"] = matchedLevel ? matchedLevel.name : "";
    r["avg_Розничная цена по уровню, USD."] = retailUsd;
    r["avg_Отпускная цена по уровню, USD."] = wholesaleUsd;
    if (matchedLevel) {
      priceRF[idx] = matchedLevel.price_type4;
      priceKZ[idx] = matchedLevel.price_type5;
      priceUZ[idx] = matchedLevel.price_type6;
    }
    markupSelections[idx] = markupValue;
    changedRows.add(idx);
  }

  if (matchedLevel) {
    try {
      const r = await $fetch<{ mock?: boolean }>(`${apiBase.value}/api/cost/save-changes`, {
        method: "POST",
        body: {
          model: row["Модель"],
          articul: row["Артикул"],
          price_level: matchedLevel.name,
          retail_rub: matchedLevel.price_type3,
          wholesale_rub: matchedLevel.price_type1,
          retail_usd: retailUsd,
          wholesale_usd: wholesaleUsd,
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
          price_rf: priceRF[absoluteIdx] || 0,
          price_kz: priceKZ[absoluteIdx] || 0,
          price_uz: priceUZ[absoluteIdx] || 0,
          comment: comments[absoluteIdx] || "",
          author_name: user.value?.name || '',
        },
        headers: fetchHeaders.value,
      });
      if (r?.mock) mockMode.value = true;
    } catch (e: any) {
      console.error("[cost] save-changes failed", e);
      lastError.value = e?.data?.detail || e?.message || String(e);
    }
  }
};

const saveAllChanges = async () => {
  if (!changedRows.size) return;
  saving.value = true;
  try {
    const changes = Array.from(changedRows).map((idx) => {
      const row = allAggregated.value[idx];
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
        price_rf: priceRF[idx] || 0,
        price_kz: priceKZ[idx] || 0,
        price_uz: priceUZ[idx] || 0,
        comment: comments[idx] || "",
      };
    });
    const result = await $fetch<{ success: boolean; count: number; error?: string; mock?: boolean }>(
      `${apiBase.value}/api/cost/save-batch`,
      { method: "POST", body: { changes, author_name: user.value?.name || '' }, headers: fetchHeaders.value }
    );
    if (result.mock) mockMode.value = true;
    if (result.success) {
      alert(`Сохранено ${result.count} записей${result.mock ? " (mock-режим)" : ""}`);
      changedRows.clear();
    } else {
      alert("Ошибка: " + (result.error || "unknown"));
    }
  } catch (e: any) {
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

const isRowLocked = (row: any): boolean => {
  if (user.value?.email === 'cost-dev@local') return false;
  if (row._lock_reason) return true;
  const bm = (row['Бренд-менеджер'] || '').trim().toLowerCase();
  const email = (user.value?.email || '').trim().toLowerCase();
  if (bm && email && bm === email && row.peo_status !== 'approved') return true;
  return false;
};

const openApprovalPopup = (row: any) => { if (!can('cost:approve') && !can('cost:peo_mark')) return; approvalTarget.value = row; approvalComment.value = ''; };
const closeApprovalPopup = () => { approvalTarget.value = null; };

const setApproval = async (status: 'approved' | 'rejected') => {
  if (!approvalTarget.value) return;
  approving.value = true;
  try {
    const idx = getOriginalIndex(approvalTarget.value);
    const r = allAggregated.value[idx];
    await fetch(`${apiBase.value}/api/cost/approve-calculation`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json', ...fetchHeaders.value},
      body: JSON.stringify({
        approvals: [{
          model: r['Модель'], articul: r['Артикул'],
          calc_sign: r['Признак калькуляции'], plan_id: r['PLAN_ID'],
          status, comment: status === 'rejected' ? approvalComment.value : '',
        }],
      }),
    });
    if (approvalTarget.value) approvalTarget.value.peo_status = status;
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
      body: {
        model: row['Модель'],
        articul: row['Артикул'],
        calc_sign: row['Признак калькуляции'],
        plan_id: row['PLAN_ID'],
      },
    });
    if (approvalTarget.value) approvalTarget.value.peo_status = null;
    closeApprovalPopup();
  } catch (e: any) {
    alert('Ошибка при снятии согласования: ' + (e?.data?.detail || e?.message || String(e)));
  } finally {
    approving.value = false;
  }
};

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
  if (!totalAllRecords.value) return;
  let html = '<table border="1"><tr>';
  headers.forEach((h) => (html += `<th>${h}</th>`));
  html += "</tr>";
  for (let ei = 0; ei < allAggregated.value.length; ei++) {
    const row = allAggregated.value[ei];
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
      fmt(priceRF[ei] ?? ''),
      fmt(priceKZ[ei] ?? ''),
      fmt(priceUZ[ei] ?? ''),
      comments[ei] || "",
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
    const cells = r.querySelectorAll("td, th");
    const line: string[] = [];
    cells.forEach((c) => line.push((c as HTMLElement).innerText.replace(/\n/g, " ").trim()));
    tsv += line.join("\t") + "\n";
  });
  navigator.clipboard?.writeText(tsv);
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
    const procPayload = selected.map((pc: any) => ({
      model: pc['Модель'] ?? pc.model ?? '',
      articul: pc['Артикул'] ?? pc.articul ?? '',
      plan_id: String(pc['PLAN_ID'] ?? pc.plan_id ?? ''),
      wholesale_rub: Number(pc['Отпускная цена по уровню, руб'] ?? pc.wholesale_rub ?? 0),
      calc_sign: pc['Признак калькуляции'] ?? pc.calc_sign ?? '',
      author_name: user.value?.name || 'system',
    }));
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

const rawRowsColumns = computed(() => {
  if (!rawRowsData.value.length) return [];
  return Object.keys(rawRowsData.value[0]).filter(k => k !== 'id');
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

function formatRawRowsCell(val: any, col: string): string {
  if (val === null || val === undefined) return '—';
  if (isRawRowsNumeric(col)) {
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
  word-break: break-word;
  padding: 4px 6px;
  font-size: var(--fs-2xs, 10px);
  line-height: 1.25;
  vertical-align: bottom;
  text-align: left;
  min-width: 0;
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

/* Sticky/frozen columns for the main aggregated table (cols 1-8) */
#cost-table-1.data-table th:nth-child(-n+8),
#cost-table-1.data-table td:nth-child(-n+8) {
  position: sticky;
  background: var(--bg-surface);
}
#cost-table-1.data-table th:nth-child(1),
#cost-table-1.data-table td:nth-child(1) { left: 0; width: 110px; min-width: 110px; z-index: 4; }
#cost-table-1.data-table th:nth-child(2),
#cost-table-1.data-table td:nth-child(2) { left: 110px; width: 40px; min-width: 40px; max-width: 40px; z-index: 4; }
#cost-table-1.data-table th:nth-child(3),
#cost-table-1.data-table td:nth-child(3) { left: 150px; width: 160px; min-width: 160px; z-index: 4; }
#cost-table-1.data-table th:nth-child(4),
#cost-table-1.data-table td:nth-child(4) { left: 310px; width: 110px; min-width: 110px; z-index: 4; }
#cost-table-1.data-table th:nth-child(5),
#cost-table-1.data-table td:nth-child(5) { left: 420px; width: 90px; min-width: 90px; z-index: 4; }
#cost-table-1.data-table th:nth-child(6),
#cost-table-1.data-table td:nth-child(6) { left: 510px; width: 200px; min-width: 200px; z-index: 4; }
#cost-table-1.data-table th:nth-child(7),
#cost-table-1.data-table td:nth-child(7) { left: 710px; width: 120px; min-width: 120px; z-index: 4; }
#cost-table-1.data-table th:nth-child(8),
#cost-table-1.data-table td:nth-child(8) { left: 830px; width: 90px; min-width: 90px; z-index: 4; box-shadow: 3px 0 6px rgba(0,0,0,0.06); }
/* Restore selected/hover/sorted backgrounds on sticky cells */
#cost-table-1.data-table tr.selected td:nth-child(-n+8) {
  background: var(--bg-surface-3);
}
#cost-table-1.data-table th:nth-child(-n+8):hover {
  background: var(--bg-surface-3);
}
#cost-table-1.data-table th.sorted:nth-child(-n+8) {
  background: color-mix(in srgb, var(--accent) 10%, var(--bg-surface));
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
.filter-checkbox {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  margin-top: var(--sp-2);
  font-size: var(--fs-xs);
  cursor: pointer;
  user-select: none;
}
.filter-checkbox input[type="checkbox"] {
  width: 16px;
  height: 16px;
  cursor: pointer;
}

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
.draft-badge { display:inline-flex; align-items:center; justify-content:center; width:18px; height:18px; border-radius:50%; font-size:11px; margin-right:2px; vertical-align:middle; }
.draft-badge--draft { background:#dbeafe; color:#1d4ed8; }
.draft-badge--pending { background:#fef3c7; color:#b45309; animation:pulse 2s infinite; }
@keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.6} }
.version-editor-toolbar { display:flex; gap:8px; align-items:center; padding:8px 16px; border-bottom:1px solid var(--border-color, #e5e7eb); }
.version-editor-toolbar .spacer { flex:1; }
.version-editor-table-wrap { flex:1; overflow:auto; padding:0 16px 16px; }
.version-editor-table { width:100%; border-collapse:collapse; font-size:13px; }
.version-editor-table th, .version-editor-table td { padding:4px 6px; border:1px solid var(--border-color, #e5e7eb); text-align:left; white-space:nowrap; }
.version-editor-table .col-chk { width:32px; text-align:center; }
.version-editor-table .col-num { text-align:right; }
.editor-input { width:100%; border:1px solid transparent; padding:2px 4px; font-size:13px; background:transparent; }
.editor-input:focus { border-color:var(--accent-color, #4338ca); outline:none; background:#fff; }
.editor-select { width:100%; border:1px solid transparent; padding:2px 4px; font-size:13px; background:transparent; }
.editor-select:focus { border-color:var(--accent-color, #4338ca); outline:none; }
.row-added { background:#ecfdf5; }
.row-modified { background:#fefce8; }
.col-peo { width:48px; text-align:center; }
.peo-badge { cursor:pointer; font-size:16px; }
.peo-filter-select { padding:4px 8px; border:1px solid var(--border-color, #d1d5db); border-radius:4px; font-size:13px; }
.approval-modal { width:400px; }
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

</style>
