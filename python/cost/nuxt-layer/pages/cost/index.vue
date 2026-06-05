<template>
  <div class="page-cost">
    <header class="page-header">
      <div>
        <h1 class="page-title">Себестоимость</h1>
        <p class="page-subtitle">
          <span>Cost History · агрегаты с {{ apiHostLabel }}</span>
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
              :options="filterOptions[f.key] || []"
              :placeholder="isLocked(f.key) ? '—' : `Все · ${f.label.toLowerCase()}`"
              :disabled="isLocked(f.key)"
              @change="onFilterChange(f.key)"
            />
          </div>
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
      </div>
      <div class="cost-actions-buttons">
        <button class="btn btn-ghost" @click="openMarginModal">
          <Icon name="lucide:target" /> Таргеты маржинальности
        </button>
        <button class="btn btn-ghost" :disabled="!totalAllRecords" @click="exportToExcel">
          <Icon name="lucide:download" /> Экспорт в Excel
        </button>
        <button
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
            :options="columnFilterOptions[cfg.key] || []"
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
              <th :class="{ sorted: sortField === 'Бренд-менеджер' }" @click="toggleSort('Бренд-менеджер')">
                Бренд-менеджер<span v-if="sortField === 'Бренд-менеджер'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th :class="{ sorted: sortField === 'Модель' }" @click="toggleSort('Модель')">
                Модель<span v-if="sortField === 'Модель'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th :class="{ sorted: sortField === 'Артикул' }" @click="toggleSort('Артикул')">
                Артикул<span v-if="sortField === 'Артикул'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th :class="{ sorted: sortField === 'Наименование модели' }" @click="toggleSort('Наименование модели')">
                Наименование модели<span v-if="sortField === 'Наименование модели'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th :class="{ sorted: sortField === 'PLAN_ID' }" @click="toggleSort('PLAN_ID')">
                PLAN_ID<span v-if="sortField === 'PLAN_ID'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th :class="{ sorted: sortField === 'Страна пр-ва' }" @click="toggleSort('Страна пр-ва')">
                Страна<span v-if="sortField === 'Страна пр-ва'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th :class="{ sorted: sortField === 'Семья' }" @click="toggleSort('Семья')">
                Семья<span v-if="sortField === 'Семья'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th :class="{ sorted: sortField === 'Сезон' }" @click="toggleSort('Сезон')">
                Сезон<span v-if="sortField === 'Сезон'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th :class="{ sorted: sortField === 'дата расчета' }" @click="toggleSort('дата расчета')">
                Дата<span v-if="sortField === 'дата расчета'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th :class="{ sorted: sortField === 'Признак калькуляции' }" @click="toggleSort('Признак калькуляции')">
                Пр.кальк<span v-if="sortField === 'Признак калькуляции'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th :class="{ sorted: sortField === 'Уровень цен' }" @click="toggleSort('Уровень цен')">
                Уровень цен<span v-if="sortField === 'Уровень цен'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="!showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Розничная цена по уровню, руб.' }" @click="toggleSort('avg_Розничная цена по уровню, руб.')">
                Сред. розница (руб)<span v-if="sortField === 'avg_Розничная цена по уровню, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="!showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Отпускная цена по уровню, руб' }" @click="toggleSort('avg_Отпускная цена по уровню, руб')">
                Сред. опт (руб)<span v-if="sortField === 'avg_Отпускная цена по уровню, руб'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Розничная цена по уровню, USD.' }" @click="toggleSort('avg_Розничная цена по уровню, USD.')">
                Сред. розница ($)<span v-if="sortField === 'avg_Розничная цена по уровню, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Отпускная цена по уровню, USD.' }" @click="toggleSort('avg_Отпускная цена по уровню, USD.')">
                Сред. опт ($)<span v-if="sortField === 'avg_Отпускная цена по уровню, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="!showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Основные материалы, руб.' }" @click="toggleSort('sum_Основные материалы, руб.')">
                Осн. материалы (руб)<span v-if="sortField === 'sum_Основные материалы, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Основные материалы, USD.' }" @click="toggleSort('sum_Основные материалы, USD.')">
                Осн. материалы ($)<span v-if="sortField === 'sum_Основные материалы, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="!showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Вспомогательные материалы, руб.' }" @click="toggleSort('sum_Вспомогательные материалы, руб.')">
                Вспом. (руб)<span v-if="sortField === 'sum_Вспомогательные материалы, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Вспомогательные материалы, USD.' }" @click="toggleSort('sum_Вспомогательные материалы, USD.')">
                Вспом. ($)<span v-if="sortField === 'sum_Вспомогательные материалы, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="!showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Пошив, руб.' }" @click="toggleSort('avg_Пошив, руб.')">
                Пошив (руб)<span v-if="sortField === 'avg_Пошив, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Пошив, USD.' }" @click="toggleSort('avg_Пошив, USD.')">
                Пошив ($)<span v-if="sortField === 'avg_Пошив, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="!showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Раскрой, руб.' }" @click="toggleSort('avg_Раскрой, руб.')">
                Раскрой (руб)<span v-if="sortField === 'avg_Раскрой, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Раскрой, USD.' }" @click="toggleSort('avg_Раскрой, USD.')">
                Раскрой ($)<span v-if="sortField === 'avg_Раскрой, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="!showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Декоры, руб.' }" @click="toggleSort('sum_Декоры, руб.')">
                Декоры (руб)<span v-if="sortField === 'sum_Декоры, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Декоры, USD.' }" @click="toggleSort('sum_Декоры, USD.')">
                Декоры ($)<span v-if="sortField === 'sum_Декоры, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="!showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Вязание, руб.' }" @click="toggleSort('avg_Вязание, руб.')">
                Вязание (руб)<span v-if="sortField === 'avg_Вязание, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="showUSD" class="col-num" :class="{ sorted: sortField === 'avg_Вязание, USD.' }" @click="toggleSort('avg_Вязание, USD.')">
                Вязание ($)<span v-if="sortField === 'avg_Вязание, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="!showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Себестоимость, руб.' }" @click="toggleSort('sum_Себестоимость, руб.')">
                Себест. (руб)<span v-if="sortField === 'sum_Себестоимость, руб.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th v-if="showUSD" class="col-num" :class="{ sorted: sortField === 'sum_Себестоимость, USD.' }" @click="toggleSort('sum_Себестоимость, USD.')">
                Себест. ($)<span v-if="sortField === 'sum_Себестоимость, USD.'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th class="col-num" :class="{ sorted: sortField === 'calc_markup_rub' }" @click="toggleSort('calc_markup_rub')">
                Наценка <template v-if="showUSD">($)</template><template v-else>(руб)</template><span v-if="sortField === 'calc_markup_rub'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th class="col-num" :class="{ sorted: sortField === 'calc_markup_pct' }" @click="toggleSort('calc_markup_pct')">
                Наценка (%)<span v-if="sortField === 'calc_markup_pct'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th class="col-num" :class="{ sorted: sortField === 'calc_margin_pct' }" @click="toggleSort('calc_margin_pct')">
                Маржа (%)<span v-if="sortField === 'calc_margin_pct'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
              <th class="col-num" :class="{ sorted: sortField === 'calc_margin_deviation' }" @click="toggleSort('calc_margin_deviation')">
                Откл. маржи (%)<span v-if="sortField === 'calc_margin_deviation'" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="25" class="muted" style="text-align: center; padding: 24px">
                Загрузка данных…
              </td>
            </tr>
            <tr v-else-if="!pageRows.length">
              <td colspan="25" class="muted" style="text-align: center; padding: 24px">
                Нет данных. Загрузите данные кнопкой выше.
              </td>
            </tr>
            <tr
              v-for="(row, idx) in pageRows"
              :key="idx"
              :class="{ selected: selectedRowIndex === getOriginalIndex(row), ...marginRowClass(row) }"
              @click="selectRow(getOriginalIndex(row))"
            >
              <td><button class="btn-details" @click.stop="openDetails(row)">🔍</button></td>
              <td>{{ row['Бренд-менеджер'] || '—' }}</td>
              <td>{{ row['Модель'] || '—' }}</td>
              <td>{{ row['Артикул'] || '—' }}</td>
              <td>{{ row['Наименование модели'] || '—' }}</td>
              <td>{{ row['PLAN_ID'] || '—' }}</td>
              <td>{{ row['Страна пр-ва'] || '—' }}</td>
              <td>{{ row['Семья'] || '—' }}</td>
              <td>{{ row['Сезон'] || '—' }}</td>
              <td class="num">{{ formatDate(row['дата расчета']) }}</td>
              <td>{{ row['Признак калькуляции'] || '—' }}</td>
              <td>
                <select
                  class="price-select"
                  :value="row['Уровень цен'] || ''"
                  @click.stop
                  @change="onPriceLevelChange(getOriginalIndex(row), ($event.target as HTMLSelectElement).value)"
                >
                  <option value="">—</option>
                  <option v-for="lvl in priceLevels" :key="lvl.name" :value="lvl.name">
                    {{ lvl.name }}
                  </option>
                </select>
              </td>
              <td v-if="!showUSD" class="col-num num">{{ fmt(row['avg_Розничная цена по уровню, руб.']) }}</td>
              <td v-if="!showUSD" class="col-num num">{{ fmt(row['avg_Отпускная цена по уровню, руб']) }}</td>
              <td v-if="showUSD" class="col-num num">{{ fmt(row['avg_Розничная цена по уровню, USD.']) }}</td>
              <td v-if="showUSD" class="col-num num">{{ fmt(row['avg_Отпускная цена по уровню, USD.']) }}</td>
              <td v-if="!showUSD" class="col-num num">{{ fmt(row['sum_Основные материалы, руб.']) }}</td>
              <td v-if="showUSD" class="col-num num">{{ fmt(row['sum_Основные материалы, USD.']) }}</td>
              <td v-if="!showUSD" class="col-num num">{{ fmt(row['sum_Вспомогательные материалы, руб.']) }}</td>
              <td v-if="showUSD" class="col-num num">{{ fmt(row['sum_Вспомогательные материалы, USD.']) }}</td>
              <td v-if="!showUSD" class="col-num num">{{ fmt(row['avg_Пошив, руб.']) }}</td>
              <td v-if="showUSD" class="col-num num">{{ fmt(row['avg_Пошив, USD.']) }}</td>
              <td v-if="!showUSD" class="col-num num">{{ fmt(row['avg_Раскрой, руб.']) }}</td>
              <td v-if="showUSD" class="col-num num">{{ fmt(row['avg_Раскрой, USD.']) }}</td>
              <td v-if="!showUSD" class="col-num num">{{ fmt(row['sum_Декоры, руб.']) }}</td>
              <td v-if="showUSD" class="col-num num">{{ fmt(row['sum_Декоры, USD.']) }}</td>
              <td v-if="!showUSD" class="col-num num">{{ fmt(row['avg_Вязание, руб.']) }}</td>
              <td v-if="showUSD" class="col-num num">{{ fmt(row['avg_Вязание, USD.']) }}</td>
              <td v-if="!showUSD" class="col-num num-strong">{{ fmt(row['sum_Себестоимость, руб.']) }}</td>
              <td v-if="showUSD" class="col-num num-strong">{{ fmt(row['sum_Себестоимость, USD.']) }}</td>
              <td class="col-num num">{{ fmt(calc(row, showUSD).markupRub) }}</td>
              <td class="col-num num" :class="calc(row, showUSD).markupPct >= 0 ? 'delta-pos' : 'delta-neg'">
                {{ calc(row, showUSD).markupPct.toFixed(1) }}%
              </td>
              <td class="col-num num">{{ calc(row, showUSD).marginPct.toFixed(1) }}%</td>
              <td class="col-num num" :class="marginDevClass(row, showUSD)">{{ marginDevText(row, showUSD) }}</td>
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
              :options="filterOptions['calc_sign'] || []"
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
              :options="detailsColumnFilterOptions[cfg.key] || []"
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
                <th class="col-num">Розница (руб)</th>
                <th class="col-num">Опт (руб)</th>
                <th class="col-num">Осн. мат.</th>
                <th class="col-num">Вспом.</th>
                <th class="col-num">Пошив</th>
                <th class="col-num">Раскрой</th>
                <th class="col-num">Декор</th>
                <th class="col-num">Вязание</th>
                <th class="col-num">Себест.</th>
                <th class="col-num">Наценка</th>
                <th class="col-num">Наценка %</th>
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
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";

interface PriceLevel { name: string; price_type1: number; price_type3: number }
interface FilterOption { id: string; text: string }
type FilterKey =
  | "brand_manager" | "level01" | "level02" | "level03" | "level04" | "level05"
  | "calc_sign";

const filterConfig: { key: FilterKey; label: string }[] = [
  { key: "brand_manager", label: "Бренд-менеджер" },
  { key: "level01", label: "Level 01" },
  { key: "level02", label: "Level 02" },
  { key: "level03", label: "Level 03" },
  { key: "level04", label: "Level 04" },
  { key: "level05", label: "Level 05" },
  { key: "calc_sign", label: "Признак калькуляции" },
];

const LEVEL_KEYS = ["level01", "level02", "level03", "level04", "level05"];

const config = useRuntimeConfig();
const apiBase = computed(() =>
  config.public.costOnly ? "" : ((config.public.apiBase as string) || "")
);
const apiHostLabel = computed(() => apiBase.value || "локального API");

const mockMode = ref(false);
const showUSD = ref(true);

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
  for (const k of filterConfig.map((c) => c.key)) {
    const v = selected[k];
    if (v?.length) f[k] = v;
  }
  return f;
}

const fetchHeaders = computed(() => ({}));

// ── Data loading ────────────────────────────────────────────────────────────

const allAggregated = ref<any[]>([]);
const totalAllRecords = ref(0);
const pageSize = 50;

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
    const result = await $fetch<{ data: any[]; count: number }>(
      `${apiBase.value}/api/cost/aggregated`,
      { method: "POST", body: buildFilters(), headers: fetchHeaders.value }
    );
    allAggregated.value = result.data || [];
    totalAllRecords.value = result.count || 0;
    currentPage.value = 0;
    selectedRowIndex.value = -1;
    changedRows.clear();
    lastError.value = "";
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
    const username = 'system';
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
    const cells = [
      dv,
      r['\u041F\u0440\u0438\u0437\u043D\u0430\u043A \u043A\u0430\u043B\u044C\u043A\u0443\u043B\u044F\u0446\u0438\u0438'] || '\u2014',
      r['\u0410\u0440\u0442\u0438\u043A\u0443\u043B'] || '\u2014',
      r['\u041D\u0430\u0438\u043C\u0435\u043D\u043E\u0432\u0430\u043D\u0438\u0435 \u043C\u043E\u0434\u0435\u043B\u0438'] || '\u2014',
      r['\u041D\u043E\u043C\u0435\u0440 \u0437\u0430\u0434\u0430\u043D\u0438\u044F \u043F\u0440\u043E\u0438\u0437\u0432\u043E\u0434\u0441\u0442\u0432\u0430'] || '\u2014',
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
    for (let ci = 0; ci < 5; ci++) tableHtml += '<td>' + escHtml(String(cells[ci])) + '<\/td>';
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
function gv(r,f){var v=(r[f]||"").toString().trim();if(f==="\\u0434\\u0430\\u0442\\u0430 \\u0440\\u0430\\u0441\\u0447\\u0435\\u0442\\u0430"&&v.indexOf("T")>=0)v=v.split("T")[0];return v}
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
    var csOpts=["\\u041F\\u041A\\u041F\\u0421\\u0421","\\u041A\\u041F\\u0421\\u0421","\\u041F\\u0424\\u041A\\u0421\\u0421","\\u0424\\u041A\\u0421\\u0421"];
    var h="";
    for(var oi=0;oi<csOpts.length;oi++)h+='<label><input type="checkbox" value="'+csOpts[oi]+'">'+csOpts[oi]+"<\\/label>";
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
    'td:nth-child(-n+5){text-align:left}' +
    'tbody tr:hover{background:#f1f3f5}' +
    '.count{padding:8px 24px;font-size:12px;color:#6c757d;border-top:1px solid #eee}' +
    '<\/style><\/head><body>' +
    '<div class="container">' +
      '<div class="header"><h1>\u0414\u0435\u0442\u0430\u043B\u0438\u0437\u0430\u0446\u0438\u044F: ' + escHtml(model) + '<\/h1><\/div>' +
      '<div class="filters">' + filterHtml + '<\/div>' +
      '<div class="content">' +
        '<table><thead><tr>' +
          '<th>\u0414\u0430\u0442\u0430<\/th><th>\u041F\u0440.\u043A\u0430\u043B\u044C\u043A<\/th><th>\u0410\u0440\u0442\u0438\u043A\u0443\u043B<\/th><th>\u041D\u0430\u0438\u043C\u0435\u043D\u043E\u0432\u0430\u043D\u0438\u0435<\/th><th>\u2116 \u0437\u0430\u0434\u0430\u043D\u0438\u044F<\/th>' +
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

async function refreshCache() {
  try {
    const result = await $fetch<{ status: string }>(
      `${apiBase.value}/api/cost/refresh-cache`,
      { method: "POST", headers: fetchHeaders.value }
    );
    if (result.status === "already_refreshing") return;
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

const onPriceLevelChange = async (absoluteIdx: number, levelName: string) => {
  if (!levelName) return;
  const level = priceLevels.value.find((l) => l.name === levelName);
  if (!level) return;
  const row = allAggregated.value[absoluteIdx];
  if (!row) return;

  // Derive exchange rate RUB→USD for computing USD prices.
  // Try: current row's wholesale → retail → any row in dataset → hard default.
  let rate = _deriveRate(row);
  if (rate === 0) {
    for (const r of allAggregated.value) {
      rate = _deriveRate(r);
      if (rate > 0) break;
    }
  }
  if (rate === 0) rate = 92; // last-resort fallback

  const r2 = (v: number) => Math.round(v * 100) / 100;

  row["Уровень цен"] = levelName;
  row["avg_Розничная цена по уровню, руб."] = level.price_type3;
  row["avg_Отпускная цена по уровню, руб"] = level.price_type1;
  row["avg_Розничная цена по уровню, USD."] = r2(level.price_type3 / rate);
  row["avg_Отпускная цена по уровню, USD."] = r2(level.price_type1 / rate);

  changedRows.add(absoluteIdx);

  try {
    const r = await $fetch<{ mock?: boolean }>(`${apiBase.value}/api/cost/save-changes`, {
      method: "POST",
      body: {
        model: row["Модель"],
        articul: row["Артикул"],
        price_level: levelName,
        retail_rub: level.price_type3,
        wholesale_rub: level.price_type1,
      },
      headers: fetchHeaders.value,
    });
    if (r?.mock) mockMode.value = true;
  } catch (e: any) {
    console.error("[cost] save-changes failed", e);
    lastError.value = e?.data?.detail || e?.message || String(e);
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
      };
    });
    const result = await $fetch<{ success: boolean; count: number; error?: string; mock?: boolean }>(
      `${apiBase.value}/api/cost/save-batch`,
      { method: "POST", body: { changes }, headers: fetchHeaders.value }
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

// ── Excel export ────────────────────────────────────────────────────────────

const headers = [
  "", "Бренд-менеджер", "Модель", "Артикул", "Наименование модели", "PLAN_ID", "Страна", "Семья", "Сезон",
  "Дата", "Пр.кальк", "Уровень цен",
  "Сред. розница (руб)", "Сред. опт (руб)", "Сред. розница ($)", "Сред. опт ($)",
  "Осн. материалы (руб)", "Осн. материалы ($)",
  "Вспом. материалы (руб)", "Вспом. материалы ($)",
  "Пошив (руб)", "Пошив ($)", "Раскрой (руб)", "Раскрой ($)",
  "Декоры (руб)", "Декоры ($)",
  "Вязание (руб)", "Вязание ($)",
  "Себест. (руб)", "Себест. ($)",
  "Наценка (руб)", "Наценка (%)", "Маржа (%)", "Откл. маржи (%)",
];

const exportToExcel = () => {
  if (!totalAllRecords.value) return;
  let html = '<table border="1"><tr>';
  headers.forEach((h) => (html += `<th>${h}</th>`));
  html += "</tr>";
  for (const row of allAggregated.value) {
    const c = calc(row);
    const cells = [
      "",
      row["Бренд-менеджер"] || "",
      row["Модель"] || "",
      row["Артикул"] || "",
      row["Наименование модели"] || "",
      row["PLAN_ID"] || "",
      row["Страна пр-ва"] || "",
      row["Семья"] || "",
      row["Сезон"] || "",
      formatDate(row["дата расчета"]),
      row["Признак калькуляции"] || "",
      row["Уровень цен"] || "",
      fmt(row["avg_Розничная цена по уровню, руб."]),
      fmt(row["avg_Отпускная цена по уровню, руб"]),
      fmt(row["avg_Розничная цена по уровню, USD."]),
      fmt(row["avg_Отпускная цена по уровню, USD."]),
      fmt(row["sum_Основные материалы, руб."]),
      fmt(row["sum_Основные материалы, USD."]),
      fmt(row["sum_Вспомогательные материалы, руб."]),
      fmt(row["sum_Вспомогательные материалы, USD."]),
      fmt(row["avg_Пошив, руб."]),
      fmt(row["avg_Пошив, USD."]),
      fmt(row["avg_Раскрой, руб."]),
      fmt(row["avg_Раскрой, USD."]),
      fmt(row["sum_Декоры, руб."]),
      fmt(row["sum_Декоры, USD."]),
      fmt(row["avg_Вязание, руб."]),
      fmt(row["avg_Вязание, USD."]),
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
  'avg_Пошив, USD.',
  'avg_Раскрой, USD.',
  'sum_Декоры, USD.',
  'avg_Вязание, USD.',
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

onMounted(async () => {
  await Promise.all([loadFilters(), loadPriceLevels(), loadCacheStatus()]);
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
.filters-body { position: relative; padding: var(--sp-5) var(--sp-6); }
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
  gap: var(--sp-4);
  margin-bottom: var(--sp-5);
}
.dates-row label {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
  font-size: var(--fs-sm);
  color: var(--text-muted);
}
.dates-row input {
  height: 32px;
  padding: 0 var(--sp-3);
  border: 1px solid var(--border);
  background: var(--bg-surface);
  border-radius: var(--rd-2);
}

.filters-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: var(--sp-4);
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
  gap: var(--sp-4);
  padding: var(--sp-3) var(--sp-5);
  background: var(--bg-surface-2, var(--bg-surface));
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  margin-bottom: var(--sp-5);
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

.details-filters { padding: var(--sp-4) var(--sp-5); background: var(--bg-surface-2); border-bottom: 1px solid var(--border); display: flex; gap: var(--sp-4); align-items: flex-end; flex-wrap: wrap; flex-shrink: 0; }
.details-filters label { font-size: var(--fs-xs); font-weight: var(--fw-medium); display: flex; flex-direction: column; gap: 4px; color: var(--text-muted); }
.details-filters input[type="date"] { height: 32px; padding: 0 var(--sp-3); border: 1px solid var(--border); border-radius: var(--rd-2); background: var(--bg-surface); }

.details-col-filters { padding: var(--sp-3) var(--sp-5); border-bottom: 1px solid var(--border); display: flex; gap: var(--sp-3); align-items: flex-start; flex-wrap: wrap; flex-shrink: 0; }
.details-col-filter-item { display: flex; flex-direction: column; gap: 2px; min-width: 140px; max-width: 200px; flex: 1; }
.details-col-filter-item.locked { opacity: 0.5; }
.details-col-filter-item label { font-size: 10px; color: var(--text-muted); font-weight: var(--fw-medium); }
.details-col-filter-reset { font-size: var(--fs-xs); color: var(--text-muted); padding-top: 16px; white-space: nowrap; }
.details-col-filter-reset:hover { color: var(--text-strong); }

.details-table-wrap { overflow: auto; flex: 1; }
.details-count { padding: var(--sp-2) var(--sp-5); font-size: var(--fs-xs); color: var(--text-muted); border-top: 1px solid var(--border); flex-shrink: 0; }
.btn-details { background: none; border: none; cursor: pointer; padding: 2px 6px; font-size: 14px; }
.btn-details:hover { opacity: 0.7; }

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
.data-table th { cursor: pointer; user-select: none; }
.data-table th:hover { background: var(--bg-surface-3); }
.data-table th.sorted { background: color-mix(in srgb, var(--accent) 10%, var(--bg-surface)); }
.sort-arrow { font-size: 10px; color: var(--accent); }
.data-table tr.selected { background: var(--bg-surface-3); }
.data-table tbody tr { cursor: pointer; }
.data-table .price-select {
  height: 26px;
  border: 1px solid var(--border);
  background: var(--bg-surface);
  border-radius: var(--rd-2);
  font-size: var(--fs-xs);
  padding: 0 4px;
  max-width: 180px;
  color: var(--text-strong);
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
  gap: var(--sp-3);
  align-items: flex-start;
  flex-wrap: wrap;
  padding: var(--sp-3) var(--sp-5);
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
</style>
