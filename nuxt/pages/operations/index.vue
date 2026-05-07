<template>
  <div class="page-operations">
    <header class="page-header">
      <div>
        <h1 class="page-title">Операции</h1>
        <p class="page-subtitle">
          <span class="ctx-flag">{{ ctxFlag }}</span>
          <span class="ctx-label">{{ entityLabel }}</span>
          <span class="ctx-sep">·</span>
          <span>{{ filtered.length }} операций · оборот {{ moneyFmt(turnover, "RUB", true) }}</span>
        </p>
      </div>
      <div class="page-actions">
        <button class="btn btn-ghost">
          <Icon name="lucide:download" /> Экспорт CSV
        </button>
        <button class="btn btn-ghost">
          <Icon name="lucide:upload" /> Импорт
        </button>
        <button class="btn btn-primary">
          <Icon name="lucide:plus" /> Новая операция
        </button>
      </div>
    </header>

    <!-- Метрики срез -->
    <section class="metrics-strip">
      <div class="metric">
        <div class="metric-label">Поступления</div>
        <div class="metric-value delta-pos">{{ moneyFmt(income, "RUB", true) }}</div>
        <div class="metric-sub">{{ incomeCount }} операций</div>
      </div>
      <div class="metric">
        <div class="metric-label">Списания</div>
        <div class="metric-value delta-neg">{{ moneyFmt(-Math.abs(outcome), "RUB", true) }}</div>
        <div class="metric-sub">{{ outcomeCount }} операций</div>
      </div>
      <div class="metric">
        <div class="metric-label">Сальдо</div>
        <div class="metric-value" :class="net >= 0 ? 'delta-pos' : 'delta-neg'">
          {{ moneyFmt(net, "RUB", true) }}
        </div>
        <div class="metric-sub">приток − отток</div>
      </div>
      <div class="metric">
        <div class="metric-label">Средний чек</div>
        <div class="metric-value">{{ moneyFmt(avgCheck, "RUB", false) }}</div>
        <div class="metric-sub">по поступлениям</div>
      </div>
      <div class="metric">
        <div class="metric-label">Ожидают подтв.</div>
        <div class="metric-value">{{ pendingCount }}</div>
        <div class="metric-sub">требуют внимания</div>
      </div>
    </section>

    <!-- Фильтры -->
    <div class="filters-bar">
      <div class="search">
        <Icon name="lucide:search" class="search-icon" />
        <input v-model="q" placeholder="Поиск по назначению, контрагенту, ИНН, № документа…" />
      </div>

      <div class="segmented" role="tablist">
        <button v-for="t in typeTabs" :key="t.value" :aria-pressed="typeFilter === t.value" @click="typeFilter = t.value">
          {{ t.label }}
        </button>
      </div>

      <select v-model="statusFilter" class="select" style="width: 140px">
        <option value="">Все статусы</option>
        <option>Проведена</option>
        <option>Ожидает</option>
        <option>Отменена</option>
      </select>

      <select v-model="accountFilter" class="select" style="width: 200px">
        <option value="">Все счета</option>
        <option v-for="a in accounts" :key="a">{{ a }}</option>
      </select>

      <div class="toolbar-spacer"></div>

      <div class="segmented density" role="tablist">
        <button :aria-pressed="density === 'compact'" @click="density = 'compact'" title="Плотно">
          <Icon name="lucide:rows-3" />
        </button>
        <button :aria-pressed="density === 'normal'" @click="density = 'normal'" title="Обычно">
          <Icon name="lucide:rows-2" />
        </button>
        <button :aria-pressed="density === 'comfortable'" @click="density = 'comfortable'" title="Просторно">
          <Icon name="lucide:rows" />
        </button>
      </div>
    </div>

    <!-- Таблица -->
    <div class="table-wrap operations-table">
      <table class="data-table" :class="density">
        <thead>
          <tr>
            <th class="col-w-16">
              <input type="checkbox" :checked="allSelected" @change="toggleAll" />
            </th>
            <th class="sortable" :class="sortClass('date')" @click="sortBy('date')">
              Дата <span class="sort-arrow">▾</span>
            </th>
            <th class="sortable" @click="sortBy('docNo')">№ документа</th>
            <th>Тип</th>
            <th>Назначение</th>
            <th>Контрагент</th>
            <th>ИНН</th>
            <th>Счёт</th>
            <th>Статья</th>
            <th>Статус</th>
            <th class="col-num sortable" :class="sortClass('amount')" @click="sortBy('amount')">
              Сумма <span class="sort-arrow">▾</span>
            </th>
            <th class="col-num">Валюта</th>
            <th class="col-num">Сальдо</th>
            <th class="col-w-16"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="op in pageRows"
            :key="op.id"
            :class="{ selected: selected.has(op.id) }"
            @click="toggle(op.id)"
          >
            <td class="col-w-16">
              <input type="checkbox" :checked="selected.has(op.id)" @click.stop @change="toggle(op.id)" />
            </td>
            <td class="num">{{ dateFmt(op.date) }}</td>
            <td class="num">{{ op.docNo }}</td>
            <td>
              <span class="op-type" :class="op.amount > 0 ? 'pos' : 'neg'">
                <Icon :name="op.amount > 0 ? 'lucide:arrow-down-left' : 'lucide:arrow-up-right'" />
                {{ op.amount > 0 ? "Поступление" : "Списание" }}
              </span>
            </td>
            <td class="cell-purpose">{{ op.purpose }}</td>
            <td>{{ op.counterparty }}</td>
            <td class="num">{{ op.inn }}</td>
            <td>{{ op.account }}</td>
            <td>
              <span class="badge">{{ op.category }}</span>
            </td>
            <td>
              <span class="badge" :class="statusBadge(op.status)">
                <span v-if="op.status === 'Проведена'" class="badge-dot"></span>
                {{ op.status }}
              </span>
            </td>
            <td class="col-num num-strong" :class="op.amount > 0 ? 'delta-pos' : 'delta-neg'">
              {{ moneyFmt(op.amount, op.currency, false, true) }}
            </td>
            <td class="col-num">{{ op.currency }}</td>
            <td class="col-num">{{ moneyFmt(op.balance, "RUB") }}</td>
            <td class="col-w-16">
              <button class="btn btn-ghost btn-icon btn-sm" @click.stop>
                <Icon name="lucide:more-horizontal" />
              </button>
            </td>
          </tr>
        </tbody>
        <tfoot>
          <tr>
            <td colspan="10">{{ filtered.length }} операций</td>
            <td class="col-num">{{ moneyFmt(net, "RUB", false, true) }}</td>
            <td class="col-num">RUB</td>
            <td class="col-num">{{ moneyFmt(filtered.length ? filtered[filtered.length - 1].balance : 0, "RUB") }}</td>
            <td></td>
          </tr>
        </tfoot>
      </table>
    </div>

    <div class="pagination">
      <span class="pagination-info">
        {{ pageStart + 1 }}–{{ Math.min(pageStart + pageSize, filtered.length) }} из {{ filtered.length }}
      </span>
      <div class="pagination-controls">
        <button class="btn btn-sm btn-ghost" :disabled="page === 1" @click="page = Math.max(1, page - 1)">
          <Icon name="lucide:chevron-left" /> Назад
        </button>
        <span class="num pagination-page">{{ page }} / {{ pageCount }}</span>
        <button class="btn btn-sm btn-ghost" :disabled="page === pageCount" @click="page = Math.min(pageCount, page + 1)">
          Вперёд <Icon name="lucide:chevron-right" />
        </button>
      </div>
      <select v-model.number="pageSize" class="select select-sm" style="width: 90px">
        <option :value="25">25</option>
        <option :value="50">50</option>
        <option :value="100">100</option>
        <option :value="200">200</option>
      </select>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from "vue";
import { money, formatDate } from "~/utils/format";

definePageMeta({ middleware: "scope-guard" });

const { label: entityLabel, countryLabel, selection } = useEntity();
const ctxFlag = computed(() => countryLabel.value?.flag || "🌐");

const moneyFmt = (v: number, ccy = "RUB", compact = false, signed = false) =>
  money(v, { currency: ccy as any, compact, signed });
const dateFmt = (v: string) => formatDate(v, "short");

// === Mock data ===
const purposes = [
  "Поступление по счёту",
  "Аренда офиса",
  "ФОТ — основной",
  "ФОТ — премии",
  "Налог на прибыль",
  "Налог УСН 6%",
  "Закупка товара",
  "Возврат покупателю",
  "Эквайринг WB",
  "Эквайринг Ozon",
  "Логистика — DPD",
  "Маркетинг — таргет",
  "Лизинг авто",
  "Подписка SaaS",
  "Связь / интернет",
  "Канцтовары",
  "Командировка"
];
const ctp = [
  { name: "ООО «Технополис-М»", inn: "7723456789" },
  { name: "АО «ЛогистикИнвест»", inn: "7714826401" },
  { name: "ИП Петров А.В.", inn: "504801234567" },
  { name: "ООО «Промкомплект»", inn: "5024118327" },
  { name: "АО «Стройсервис»", inn: "7706329104" },
  { name: "ООО «Электрон+»", inn: "7728164290" },
  { name: "АО «БЦ Орбита»", inn: "7705118200" },
  { name: "ФНС России", inn: "7707329152" },
  { name: "Тинькофф (эквайринг)", inn: "7710140679" },
  { name: "Wildberries", inn: "7721546864" }
];
const accounts = [
  "Альфа · 4827",
  "Сбер · 7711",
  "Райффайзен USD · 0193",
  "Tinkoff EUR · 3308",
  "Касса"
];
const categories = ["Реализация", "ФОТ", "Аренда", "Налоги", "Логистика", "Маркетинг", "Закупка", "Прочее"];
const statuses = ["Проведена", "Проведена", "Проведена", "Ожидает", "Отменена"] as const;

function rng(seed: number) {
  let s = seed;
  return () => {
    s = (s * 9301 + 49297) % 233280;
    return s / 233280;
  };
}
const r = rng(7);
// Распределение операций по юр.лицам — 7 сущностей с разной плотностью.
const entityIds = ["ru-1", "ru-1", "ru-2", "ru-3", "ru-4", "by-1", "by-2", "kz-1"];
const operations = Array.from({ length: 320 }, (_, i) => {
  const isIncome = r() > 0.45;
  const c = ctp[Math.floor(r() * ctp.length)];
  const day = 60 - Math.floor(r() * 60);
  const date = new Date(2026, 4, 7);
  date.setDate(date.getDate() - day);
  const amt = Math.round((isIncome ? 1 : -1) * (5_000 + r() * 4_000_000));
  const entityId = entityIds[Math.floor(r() * entityIds.length)];
  return {
    id: i + 1,
    entityId,
    date: date.toISOString().slice(0, 10),
    docNo: `№ ${1200 + i}`,
    purpose: purposes[Math.floor(r() * purposes.length)],
    counterparty: c.name,
    inn: c.inn,
    account: accounts[Math.floor(r() * accounts.length)],
    category: categories[Math.floor(r() * categories.length)],
    status: statuses[Math.floor(r() * statuses.length)],
    amount: amt,
    currency: r() > 0.92 ? "USD" : r() > 0.96 ? "EUR" : "RUB",
    balance: 0
  };
});
operations.sort((a, b) => (a.date > b.date ? 1 : -1));
let running = 5_000_000;
operations.forEach((op) => {
  running += op.currency === "RUB" ? op.amount : op.amount * (op.currency === "USD" ? 92 : 100);
  op.balance = running;
});
operations.reverse();

// === Filters / sort / page ===
const q = ref("");
const typeFilter = ref<"all" | "in" | "out">("all");
const statusFilter = ref("");
const accountFilter = ref("");
const density = ref<"compact" | "normal" | "comfortable">("normal");
const sortKey = ref<"date" | "docNo" | "amount">("date");
const sortDir = ref<"asc" | "desc">("desc");
const page = ref(1);
const pageSize = ref(50);
const selected = ref<Set<number>>(new Set());

const typeTabs = [
  { label: "Все", value: "all" },
  { label: "Поступления", value: "in" },
  { label: "Списания", value: "out" }
];

// Предикат «операция принадлежит выбранному контексту».
const matchesEntity = (entityId: string): boolean => {
  const s = selection.value;
  if (s.kind === "all") return true;
  if (s.kind === "country") {
    // ru-1, by-2 → префикс до '-' = код страны (lowercase).
    return entityId.startsWith(s.code.toLowerCase() + "-");
  }
  return entityId === s.id;
};

const filtered = computed(() => {
  const ql = q.value.trim().toLowerCase();
  let rows = operations.filter((op) => {
    if (!matchesEntity(op.entityId)) return false;
    if (typeFilter.value === "in" && op.amount <= 0) return false;
    if (typeFilter.value === "out" && op.amount >= 0) return false;
    if (statusFilter.value && op.status !== statusFilter.value) return false;
    if (accountFilter.value && op.account !== accountFilter.value) return false;
    if (ql) {
      const hay = `${op.purpose} ${op.counterparty} ${op.inn} ${op.docNo}`.toLowerCase();
      if (!hay.includes(ql)) return false;
    }
    return true;
  });
  rows = [...rows].sort((a, b) => {
    const k = sortKey.value;
    const av = (a as any)[k];
    const bv = (b as any)[k];
    const cmp = av === bv ? 0 : av > bv ? 1 : -1;
    return sortDir.value === "asc" ? cmp : -cmp;
  });
  return rows;
});

const pageCount = computed(() => Math.max(1, Math.ceil(filtered.value.length / pageSize.value)));
const pageStart = computed(() => (page.value - 1) * pageSize.value);
const pageRows = computed(() => filtered.value.slice(pageStart.value, pageStart.value + pageSize.value));

watch([q, typeFilter, statusFilter, accountFilter, selection], () => { page.value = 1; });

const sortBy = (k: "date" | "docNo" | "amount") => {
  if (sortKey.value === k) {
    sortDir.value = sortDir.value === "asc" ? "desc" : "asc";
  } else {
    sortKey.value = k;
    sortDir.value = "desc";
  }
};
const sortClass = (k: string) => ({
  sorted: sortKey.value === k,
  [`sort-${sortDir.value}`]: sortKey.value === k
});

const allSelected = computed(() =>
  pageRows.value.length > 0 && pageRows.value.every((op) => selected.value.has(op.id))
);
const toggle = (id: number) => {
  const s = new Set(selected.value);
  s.has(id) ? s.delete(id) : s.add(id);
  selected.value = s;
};
const toggleAll = () => {
  const s = new Set(selected.value);
  if (allSelected.value) {
    pageRows.value.forEach((op) => s.delete(op.id));
  } else {
    pageRows.value.forEach((op) => s.add(op.id));
  }
  selected.value = s;
};

const statusBadge = (s: string) =>
  s === "Проведена" ? "badge-pos" : s === "Ожидает" ? "badge-warn" : "badge-info";

const income = computed(() => filtered.value.filter((o) => o.amount > 0).reduce((s, o) => s + o.amount, 0));
const outcome = computed(() => filtered.value.filter((o) => o.amount < 0).reduce((s, o) => s + o.amount, 0));
const net = computed(() => income.value + outcome.value);
const turnover = computed(() => income.value + Math.abs(outcome.value));
const incomeCount = computed(() => filtered.value.filter((o) => o.amount > 0).length);
const outcomeCount = computed(() => filtered.value.filter((o) => o.amount < 0).length);
const avgCheck = computed(() => incomeCount.value ? income.value / incomeCount.value : 0);
const pendingCount = computed(() => filtered.value.filter((o) => o.status === "Ожидает").length);
</script>

<style scoped>
/* Контекст текущего юр.лица в подзаголовке */
.page-subtitle {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
  flex-wrap: wrap;
}
.ctx-flag { font-size: 14px; line-height: 1; }
.ctx-label {
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
}
.ctx-sep { color: var(--text-muted); }

.metrics-strip {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 0;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-5);
  margin-bottom: var(--sp-6);
  overflow: hidden;
}
.metric {
  padding: var(--sp-5) var(--sp-6);
  border-right: 1px solid var(--border);
}
.metric:last-child { border-right: none; }
.metric-label {
  font-size: var(--fs-xs);
  font-weight: var(--fw-semibold);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: var(--sp-3);
}
.metric-value {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--fs-xl);
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
  letter-spacing: -0.02em;
  line-height: 1.1;
}
.metric-sub {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  margin-top: 2px;
}

.filters-bar {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  padding: var(--sp-4) 0;
  margin-bottom: var(--sp-5);
  flex-wrap: wrap;
}
.filters-bar .search { max-width: 360px; flex: 1 1 300px; }
.density button {
  padding: 4px 8px;
}

.operations-table { max-height: calc(100vh - 320px); }
.col-w-16 { width: 32px; }
.cell-purpose {
  max-width: 240px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.op-type {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: var(--fs-xs);
  font-weight: var(--fw-semibold);
  padding: 2px 6px;
  border-radius: var(--rd-3);
}
.op-type.pos { color: var(--pos-strong); background: var(--pos-soft); }
.op-type.neg { color: var(--neg-strong); background: var(--neg-soft); }
.op-type :deep(svg) { width: 12px; height: 12px; }

th.sort-asc .sort-arrow { transform: rotate(180deg); display: inline-block; }

.pagination {
  display: flex;
  align-items: center;
  gap: var(--sp-5);
  padding: var(--sp-5) 0;
  font-size: var(--fs-sm);
}
.pagination-info { color: var(--text-muted); }
.pagination-controls {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  margin-left: auto;
}
.pagination-page {
  font-family: var(--font-mono);
  color: var(--text-secondary);
}
.select-sm { height: 28px; font-size: var(--fs-sm); }
</style>
