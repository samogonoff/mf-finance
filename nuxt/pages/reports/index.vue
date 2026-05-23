<template>
  <div class="page-reports">
    <header class="page-header">
      <div>
        <h1 class="page-title">Отчёты</h1>
        <p class="page-subtitle">P&L, Cash Flow, баланс — по периодам</p>
      </div>
      <div class="page-actions">
        <button class="btn btn-ghost"><Icon name="lucide:printer" /> Печать</button>
        <button class="btn btn-ghost"><Icon name="lucide:download" /> Excel</button>
        <button class="btn btn-primary"><Icon name="lucide:share-2" /> Поделиться</button>
      </div>
    </header>

    <div class="reports-tabs">
      <button v-for="t in tabs" :key="t" class="reports-tab" :class="{ active: activeTab === t }" @click="activeTab = t">
        {{ t }}
      </button>
      <div class="reports-tab-spacer"></div>
      <template v-if="activeTab !== 'Задолженность ВГО'">
        <select v-model="year" class="select" style="width: 110px">
          <option v-for="y in [2024, 2025, 2026]" :key="y" :value="y">{{ y }}</option>
        </select>
        <select v-model="grain" class="select" style="width: 130px">
          <option value="month">По месяцам</option>
          <option value="quarter">По кварталам</option>
        </select>
        <select v-model="comparison" class="select" style="width: 180px">
          <option value="none">Без сравнения</option>
          <option value="prev">с прошлым годом</option>
          <option value="plan">с планом</option>
        </select>
      </template>
    </div>

    <!-- P&L таблица -->
    <div v-if="activeTab === 'P&L'" class="table-wrap report-table-wrap">
      <table class="data-table report-table">
        <thead>
          <tr>
            <th class="col-sticky col-article">Статья</th>
            <th v-for="m in periodLabels" :key="m" class="col-num">{{ m }}</th>
            <th class="col-num">Итого</th>
            <th class="col-num">Доля</th>
            <th class="col-num">Δ vs план</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="group in pl" :key="group.label">
            <tr class="row-group" @click="toggleGroup(group.label)">
              <td class="col-sticky col-article">
                <Icon :name="isOpen(group.label) ? 'lucide:chevron-down' : 'lucide:chevron-right'" class="row-chevron" />
                <span class="row-group-label">{{ group.label }}</span>
              </td>
              <td v-for="(_, i) in periodLabels" :key="i" class="col-num num-strong" :class="signClass(group.values[i], group.kind)">
                {{ moneyFmt(group.values[i], "RUB", true, group.kind === 'profit') }}
              </td>
              <td class="col-num num-strong" :class="signClass(sum(group.values), group.kind)">
                {{ moneyFmt(sum(group.values), "RUB", true, group.kind === 'profit') }}
              </td>
              <td class="col-num">{{ pctFmt(group.share || 0, 1) }}</td>
              <td class="col-num" :class="(group.delta || 0) >= 0 ? 'delta-pos' : 'delta-neg'">
                {{ pctFmt(group.delta || 0, 1) }}
              </td>
            </tr>
            <template v-if="isOpen(group.label)">
              <tr v-for="row in group.rows" :key="row.label" class="row-leaf">
                <td class="col-sticky col-article col-leaf">{{ row.label }}</td>
                <td v-for="(v, i) in row.values" :key="i" class="col-num">
                  {{ moneyFmt(v, "RUB", true) }}
                </td>
                <td class="col-num num-strong">{{ moneyFmt(sum(row.values), "RUB", true) }}</td>
                <td class="col-num delta-zero">{{ pctFmt(row.share || 0, 1) }}</td>
                <td class="col-num" :class="(row.delta || 0) >= 0 ? 'delta-pos' : 'delta-neg'">
                  {{ pctFmt(row.delta || 0, 1) }}
                </td>
              </tr>
            </template>
          </template>
        </tbody>
        <tfoot>
          <tr>
            <td class="col-sticky col-article">Чистая прибыль</td>
            <td v-for="(v, i) in netProfitByMonth" :key="i" class="col-num num-strong" :class="v >= 0 ? 'delta-pos' : 'delta-neg'">
              {{ moneyFmt(v, "RUB", true, true) }}
            </td>
            <td class="col-num num-strong delta-pos">{{ moneyFmt(sum(netProfitByMonth), "RUB", true, true) }}</td>
            <td class="col-num">100,0%</td>
            <td class="col-num delta-pos">+8,4%</td>
          </tr>
        </tfoot>
      </table>
    </div>

    <!-- Задолженность ВГО — параметризованный отчёт с многоуровневой группировкой -->
    <DebtReport v-else-if="activeTab === 'Задолженность ВГО'" />

    <!-- Cash Flow / Balance: упрощённые stub'ы -->
    <div v-else class="card">
      <div class="card-body empty">
        <Icon name="lucide:hourglass" class="empty-icon" />
        <div class="empty-title">Раздел в разработке</div>
        <div class="empty-hint">Отчёт «{{ activeTab }}» будет подключён к Go-API после первого релиза.</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { money, pct as pctFmt } from "~/utils/format";
import DebtReport from "~/components/reports/DebtReport.vue";

definePageMeta({ middleware: "scope-guard" });

const tabs = ["P&L", "Cash Flow", "Баланс", "Налоги", "Задолженность ВГО"];
const activeTab = ref("P&L");
const year = ref(2026);
const grain = ref<"month" | "quarter">("month");
const comparison = ref<"none" | "prev" | "plan">("plan");

const periodLabels = computed(() =>
  grain.value === "month"
    ? ["Янв", "Фев", "Мар", "Апр", "Май", "Июн", "Июл", "Авг", "Сен", "Окт", "Ноя", "Дек"]
    : ["Q1", "Q2", "Q3", "Q4"]
);

const moneyFmt = (v: number, ccy = "RUB", compact = false, signed = false) =>
  money(v, { currency: ccy as any, compact, signed });

interface PLRow {
  label: string;
  values: number[];
  share?: number;
  delta?: number;
}
interface PLGroup {
  label: string;
  kind: "income" | "expense" | "profit";
  values: number[];
  share?: number;
  delta?: number;
  rows: PLRow[];
}

function gen(seed: number, base: number, growth = 0.05): number[] {
  let s = seed;
  const r = () => {
    s = (s * 9301 + 49297) % 233280;
    return s / 233280;
  };
  return Array.from({ length: 12 }, (_, i) => Math.round(base * (1 + growth * i / 12 + (r() - 0.5) * 0.18)));
}

const pl: PLGroup[] = [
  {
    label: "Выручка",
    kind: "income",
    values: gen(1, 18_000_000, 0.18),
    share: 100,
    delta: 6.2,
    rows: [
      { label: "Реализация — основная", values: gen(2, 12_000_000, 0.15), share: 67.3, delta: 5.4 },
      { label: "Эквайринг маркетплейсы", values: gen(3, 4_000_000, 0.32), share: 22.1, delta: 18.6 },
      { label: "Аренда субсчетов", values: gen(4, 1_200_000, 0.05), share: 6.8, delta: -1.2 },
      { label: "Прочие доходы", values: gen(5, 800_000, 0.12), share: 3.8, delta: 2.4 }
    ]
  },
  {
    label: "Себестоимость",
    kind: "expense",
    values: gen(6, -4_500_000, 0.08),
    share: 25.4,
    delta: -3.1,
    rows: [
      { label: "Закупка товара", values: gen(7, -3_500_000, 0.07), share: 19.5, delta: -2.4 },
      { label: "Логистика", values: gen(8, -800_000, 0.12), share: 4.4, delta: 5.8 },
      { label: "Упаковка", values: gen(9, -200_000, 0.05), share: 1.5, delta: 0.4 }
    ]
  },
  {
    label: "Операционные расходы",
    kind: "expense",
    values: gen(10, -8_500_000, 0.06),
    share: 47.4,
    delta: -1.8,
    rows: [
      { label: "ФОТ + налоги", values: gen(11, -5_100_000, 0.08), share: 28.6, delta: 4.2 },
      { label: "Аренда / коммуналка", values: gen(12, -920_000, 0.03), share: 5.2, delta: 0 },
      { label: "Маркетинг", values: gen(13, -740_000, 0.18), share: 4.1, delta: 12.3 },
      { label: "ИТ / SaaS", values: gen(14, -380_000, 0.10), share: 2.1, delta: 8.4 },
      { label: "Прочие операционные", values: gen(15, -1_360_000, 0.05), share: 7.4, delta: -2.8 }
    ]
  },
  {
    label: "Налоги",
    kind: "expense",
    values: gen(16, -800_000, 0.10),
    share: 4.6,
    delta: 6.0,
    rows: [
      { label: "УСН 6%", values: gen(17, -540_000, 0.18), share: 3.0, delta: 12.4 },
      { label: "НДФЛ", values: gen(18, -180_000, 0.05), share: 1.0, delta: 0.8 },
      { label: "Имущество", values: gen(19, -80_000, 0.0), share: 0.6, delta: 0 }
    ]
  }
];

const sum = (a: number[]) => a.reduce((s, v) => s + v, 0);

const netProfitByMonth = computed(() => {
  const out = Array.from({ length: 12 }, () => 0);
  for (const g of pl) {
    g.values.forEach((v, i) => { out[i] += v; });
  }
  return out;
});

const open = ref<Set<string>>(new Set(["Выручка", "Операционные расходы"]));
const isOpen = (k: string) => open.value.has(k);
const toggleGroup = (k: string) => {
  const s = new Set(open.value);
  s.has(k) ? s.delete(k) : s.add(k);
  open.value = s;
};

const signClass = (v: number, kind: "income" | "expense" | "profit") => {
  if (kind === "income") return "delta-pos";
  if (kind === "expense") return "delta-neg";
  return v >= 0 ? "delta-pos" : "delta-neg";
};
</script>

<style scoped>
.reports-tabs {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  padding: 0 0 var(--sp-5);
  border-bottom: 1px solid var(--border);
  margin-bottom: var(--sp-5);
}
.reports-tab {
  background: transparent;
  border: none;
  padding: var(--sp-4) var(--sp-5);
  font-size: var(--fs-base);
  font-weight: var(--fw-medium);
  color: var(--text-secondary);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  font-family: inherit;
}
.reports-tab:hover { color: var(--text-strong); }
.reports-tab.active {
  color: var(--text-strong);
  border-bottom-color: var(--accent);
  font-weight: var(--fw-semibold);
}
.reports-tab-spacer { flex: 1; }

.report-table-wrap {
  max-height: calc(100vh - 240px);
}
.report-table {
  font-variant-numeric: tabular-nums;
}
.report-table .col-article {
  min-width: 240px;
  font-weight: var(--fw-medium);
}
.report-table .col-leaf {
  padding-left: var(--sp-9);
  font-weight: var(--fw-regular);
  color: var(--text-secondary);
}
.row-group {
  background: var(--bg-surface-2);
  cursor: pointer;
}
.row-group td { font-weight: var(--fw-semibold); }
.row-group:hover td { background: var(--bg-surface-3); }
.row-group:hover .col-sticky { background: var(--bg-surface-3); }
.row-chevron {
  width: 14px;
  height: 14px;
  margin-right: var(--sp-3);
  vertical-align: middle;
  color: var(--text-muted);
}
.row-leaf td {
  font-size: var(--fs-sm);
}
</style>
