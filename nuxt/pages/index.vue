<template>
  <div class="page-dashboard">
    <header class="page-header">
      <div>
        <h1 class="page-title">Дашборд</h1>
        <p class="page-subtitle">
          <span v-if="ctxFlag" class="ctx-flag">{{ ctxFlag }}</span>
          <span class="ctx-label">{{ entityLabel }}</span>
          <span class="ctx-sep">·</span>
          <span>{{ subtitle }}</span>
        </p>
      </div>
      <div class="page-actions">
        <div class="segmented" role="tablist">
          <button
            v-for="r in periods"
            :key="r.value"
            :aria-pressed="period === r.value"
            @click="period = r.value"
          >
            {{ r.label }}
          </button>
        </div>
        <button class="btn btn-ghost">
          <Icon name="lucide:download" />
          Экспорт
        </button>
        <button class="btn btn-primary">
          <Icon name="lucide:plus" />
          Новая операция
        </button>
      </div>
    </header>

    <!-- Главные KPI -->
    <section class="kpi-grid">
      <KpiTile
        label="Выручка"
        :value="kpis.revenue.value"
        format="money"
        :compact="true"
        :delta-pct="kpis.revenue.delta"
        :spark="kpis.revenue.spark"
        caption="vs прошл. период"
      />
      <KpiTile
        label="Расходы"
        :value="kpis.expenses.value"
        format="money"
        :compact="true"
        :delta-pct="kpis.expenses.delta"
        :spark="kpis.expenses.spark"
        caption="vs прошл. период"
      />
      <KpiTile
        label="Чистая прибыль"
        :value="kpis.netProfit.value"
        format="money"
        :compact="true"
        :delta-pct="kpis.netProfit.delta"
        :spark="kpis.netProfit.spark"
        badge="Маржа 24,8%"
        badge-kind="pos"
      />
      <KpiTile
        label="Денежный поток"
        :value="kpis.cashflow.value"
        format="money"
        :compact="true"
        :delta-pct="kpis.cashflow.delta"
        :spark="kpis.cashflow.spark"
        caption="приток − отток"
      />
      <KpiTile
        label="Дебиторка"
        :value="kpis.receivables.value"
        format="money"
        :compact="true"
        :delta-pct="kpis.receivables.delta"
        badge="3 просрочки"
        badge-kind="warn"
        caption="к получению"
      />
      <KpiTile
        label="Кредиторка"
        :value="kpis.payables.value"
        format="money"
        :compact="true"
        :delta-pct="kpis.payables.delta"
        caption="к оплате"
      />
    </section>

    <!-- Двухколоночный блок: cashflow chart + остатки по счетам -->
    <section class="dashboard-grid-2">
      <div class="card">
        <div class="card-header">
          <div>
            <div class="card-title">Денежный поток</div>
            <div class="card-subtitle">приток / отток · {{ periodLabel }}</div>
          </div>
          <div class="legend">
            <span class="legend-item"><span class="dot pos"></span>Приток</span>
            <span class="legend-item"><span class="dot neg"></span>Отток</span>
            <span class="legend-item"><span class="dot accent"></span>Сальдо</span>
          </div>
        </div>
        <div class="card-body cashflow-chart">
          <svg viewBox="0 0 600 200" preserveAspectRatio="none" class="cashflow-svg">
            <g v-for="(bar, i) in cashflowBars" :key="i">
              <rect
                :x="bar.x"
                :y="bar.posY"
                :width="bar.w"
                :height="bar.posH"
                class="bar pos"
                rx="1"
              />
              <rect
                :x="bar.x"
                :y="bar.negY"
                :width="bar.w"
                :height="bar.negH"
                class="bar neg"
                rx="1"
              />
            </g>
            <path :d="cashflowLine" class="line-balance" />
          </svg>
          <div class="chart-axis">
            <span v-for="m in months" :key="m">{{ m }}</span>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <div>
            <div class="card-title">Остатки по счетам</div>
            <div class="card-subtitle">на {{ todayStr }}</div>
          </div>
          <button class="btn btn-ghost btn-sm">
            <Icon name="lucide:refresh-cw" />
          </button>
        </div>
        <div class="accounts-list">
          <div v-for="acc in accounts" :key="acc.id" class="account-row">
            <div class="account-meta">
              <div class="account-name">{{ acc.name }}</div>
              <div class="account-bank">{{ acc.bank }} · {{ acc.number }}</div>
            </div>
            <div class="account-balance">
              <div class="num num-strong">{{ moneyFmt(acc.balance, acc.currency) }}</div>
              <div class="num kpi-delta" :class="acc.delta > 0 ? 'pos' : 'neg'">
                {{ pctFmt(acc.delta) }}
              </div>
            </div>
          </div>
        </div>
        <div class="card-footer">
          <span>Всего по счетам</span>
          <span class="num num-strong">{{ moneyFmt(totalBalance, "RUB") }}</span>
        </div>
      </div>
    </section>

    <!-- Топ контрагентов + последние операции -->
    <section class="dashboard-grid-2">
      <div class="card">
        <div class="card-header">
          <div>
            <div class="card-title">Топ контрагентов</div>
            <div class="card-subtitle">по обороту за {{ periodLabel }}</div>
          </div>
          <NuxtLink class="btn btn-ghost btn-sm" to="/counterparties">
            Все →
          </NuxtLink>
        </div>
        <div class="table-wrap no-bd">
          <table class="data-table compact">
            <thead>
              <tr>
                <th>Контрагент</th>
                <th>ИНН</th>
                <th class="col-num">Оборот</th>
                <th class="col-num">Доля</th>
                <th class="col-num">Δ</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in topCounterparties" :key="c.id">
                <td>{{ c.name }}</td>
                <td class="num">{{ c.inn }}</td>
                <td class="col-num">{{ moneyFmt(c.turnover, "RUB", true) }}</td>
                <td class="col-num">{{ pctFmt(c.share, 1) }}</td>
                <td class="col-num" :class="c.delta > 0 ? 'delta-pos' : 'delta-neg'">
                  {{ pctFmt(c.delta) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <div>
            <div class="card-title">Последние операции</div>
            <div class="card-subtitle">{{ recentOps.length }} последних</div>
          </div>
          <NuxtLink class="btn btn-ghost btn-sm" to="/operations">
            Все →
          </NuxtLink>
        </div>
        <div class="table-wrap no-bd">
          <table class="data-table compact">
            <thead>
              <tr>
                <th>Дата</th>
                <th>Назначение</th>
                <th>Контрагент</th>
                <th>Статус</th>
                <th class="col-num">Сумма</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="op in recentOps" :key="op.id">
                <td class="num">{{ dateFmt(op.date) }}</td>
                <td>{{ op.purpose }}</td>
                <td>{{ op.counterparty }}</td>
                <td>
                  <span class="badge" :class="statusBadge(op.status)">{{ op.status }}</span>
                </td>
                <td class="col-num" :class="op.amount > 0 ? 'delta-pos' : 'delta-neg'">
                  {{ moneyFmt(op.amount, "RUB", false, true) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </section>

    <!-- Структура доходов / расходов -->
    <section class="dashboard-grid-2">
      <div class="card">
        <div class="card-header">
          <div class="card-title">Структура доходов</div>
          <div class="card-subtitle">{{ periodLabel }}</div>
        </div>
        <div class="card-body">
          <div v-for="row in incomeStructure" :key="row.label" class="bar-row">
            <div class="bar-row-label">
              <span>{{ row.label }}</span>
              <span class="num">{{ moneyFmt(row.value, "RUB", true) }}</span>
            </div>
            <div class="bar-track">
              <div class="bar-fill pos" :style="{ width: `${row.pct}%` }"></div>
            </div>
            <div class="bar-row-meta">{{ pctFmt(row.pct, 1) }}</div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <div class="card-title">Структура расходов</div>
          <div class="card-subtitle">{{ periodLabel }}</div>
        </div>
        <div class="card-body">
          <div v-for="row in expenseStructure" :key="row.label" class="bar-row">
            <div class="bar-row-label">
              <span>{{ row.label }}</span>
              <span class="num">{{ moneyFmt(row.value, "RUB", true) }}</span>
            </div>
            <div class="bar-track">
              <div class="bar-fill neg" :style="{ width: `${row.pct}%` }"></div>
            </div>
            <div class="bar-row-meta">{{ pctFmt(row.pct, 1) }}</div>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { money, pct as pctFmt, formatDate } from "~/utils/format";

definePageMeta({ middleware: "scope-guard" });

// Контекст выбранного юр.лица — все цифры зависят от него.
const { label: entityLabel, countryLabel, mockMultiplier } = useEntity();
const ctxFlag = computed(() => countryLabel.value?.flag || "🌐");
const k = computed(() => mockMultiplier.value); // короткое имя для шаблона

const periods = [
  { label: "Сегодня", value: "d" },
  { label: "Неделя", value: "w" },
  { label: "Месяц", value: "m" },
  { label: "Квартал", value: "q" },
  { label: "Год", value: "y" }
];
const period = ref<"d" | "w" | "m" | "q" | "y">("m");
const periodLabel = computed(() => periods.find((p) => p.value === period.value)?.label || "Месяц");

const todayStr = formatDate(new Date(), "long");
const subtitle = computed(() => `Сводка финансовой деятельности · ${todayStr}`);

const moneyFmt = (v: number, ccy: any = "RUB", compact = false, signed = false) =>
  money(v, { currency: ccy, compact, signed });
const dateFmt = (v: string) => formatDate(v, "short");

// === Mock data — реактивно зависит от выбранного юр.лица ===
const kpis = computed(() => {
  const m = k.value;
  return {
    revenue:     { value: Math.round(18_420_000 * m), delta: 12.4, spark: [12, 14, 13, 16, 15, 18, 17, 20, 19, 22, 21, 24] },
    expenses:    { value: Math.round(13_850_000 * m), delta: -3.2, spark: [11, 12, 12, 13, 12, 14, 13, 14, 13, 13, 12, 13] },
    netProfit:   { value: Math.round( 4_570_000 * m), delta: 28.1, spark: [2, 3, 4, 3, 5, 6, 5, 7, 6, 8, 9, 11] },
    cashflow:    { value: Math.round( 2_180_000 * m), delta: 8.6,  spark: [1, 2, 1, 3, 2, 4, 3, 5, 4, 5, 4, 6] },
    receivables: { value: Math.round( 6_240_000 * m), delta: -4.1 },
    payables:    { value: Math.round( 3_910_000 * m), delta: 6.2  }
  };
});

const months = ["Янв", "Фев", "Мар", "Апр", "Май", "Июн", "Июл", "Авг", "Сен", "Окт", "Ноя", "Дек"];
const cashflowData = [
  { pos: 12, neg: 8 },
  { pos: 14, neg: 9 },
  { pos: 13, neg: 11 },
  { pos: 16, neg: 12 },
  { pos: 15, neg: 13 },
  { pos: 18, neg: 14 },
  { pos: 17, neg: 13 },
  { pos: 20, neg: 14 },
  { pos: 19, neg: 13 },
  { pos: 22, neg: 14 },
  { pos: 21, neg: 13 },
  { pos: 24, neg: 14 }
];
const cashflowBars = computed(() => {
  const w = 600;
  const h = 200;
  const mid = h / 2;
  const colW = w / cashflowData.length;
  const max = 26;
  return cashflowData.map((d, i) => {
    const barW = colW * 0.55;
    const x = i * colW + (colW - barW) / 2;
    const posH = (d.pos / max) * (mid - 4);
    const negH = (d.neg / max) * (mid - 4);
    return {
      x,
      w: barW,
      posY: mid - posH,
      posH,
      negY: mid,
      negH
    };
  });
});
const cashflowLine = computed(() => {
  const w = 600;
  const h = 200;
  const mid = h / 2;
  const colW = w / cashflowData.length;
  const max = 16;
  return cashflowData
    .map((d, i) => {
      const cx = i * colW + colW / 2;
      const balance = d.pos - d.neg;
      const cy = mid - (balance / max) * (mid - 12);
      return `${i === 0 ? "M" : "L"}${cx.toFixed(1)},${cy.toFixed(1)}`;
    })
    .join(" ");
});

const accounts = [
  { id: 1, name: "Расчётный счёт ОАО", bank: "Альфа-Банк", number: "·· 4827", balance: 8_420_000, delta: 2.1, currency: "RUB" },
  { id: 2, name: "Транзитный USD", bank: "Райффайзен", number: "·· 0193", balance: 124_500, delta: -1.4, currency: "USD" },
  { id: 3, name: "Депозитный счёт", bank: "Сбер", number: "·· 7711", balance: 12_000_000, delta: 0.4, currency: "RUB" },
  { id: 4, name: "EUR-счёт", bank: "Tinkoff", number: "·· 3308", balance: 56_700, delta: -0.8, currency: "EUR" },
  { id: 5, name: "Касса (наличные)", bank: "—", number: "—", balance: 184_000, delta: 0, currency: "RUB" }
];
const totalBalance = computed(() => {
  const rates: Record<string, number> = { RUB: 1, USD: 92, EUR: 100, BYN: 28 };
  return accounts.reduce((s, a) => s + a.balance * (rates[a.currency] || 1), 0);
});

const topCounterparties = [
  { id: 1, name: "ООО «Технополис-М»", inn: "7723456789", turnover: 4_820_000, share: 14.2, delta: 8.4 },
  { id: 2, name: "АО «ЛогистикИнвест»", inn: "7714826401", turnover: 3_140_000, share: 9.3, delta: -2.1 },
  { id: 3, name: "ИП Петров А.В.", inn: "504801234567", turnover: 2_680_000, share: 7.9, delta: 12.7 },
  { id: 4, name: "ООО «Промкомплект»", inn: "5024118327", turnover: 1_950_000, share: 5.8, delta: 0 },
  { id: 5, name: "АО «Стройсервис»", inn: "7706329104", turnover: 1_480_000, share: 4.4, delta: -1.4 },
  { id: 6, name: "ООО «Электрон+»", inn: "7728164290", turnover: 1_120_000, share: 3.3, delta: 4.2 }
];

const recentOps = [
  { id: 1, date: "2026-05-07", purpose: "Поступление по счёту 1284", counterparty: "ООО «Технополис-М»", status: "Проведена", amount: 1_240_000 },
  { id: 2, date: "2026-05-07", purpose: "Аренда офиса (май)", counterparty: "АО «БЦ Орбита»", status: "Проведена", amount: -380_000 },
  { id: 3, date: "2026-05-06", purpose: "ФОТ — аванс", counterparty: "Зарплатный реестр", status: "Проведена", amount: -2_140_000 },
  { id: 4, date: "2026-05-06", purpose: "Возврат предоплаты", counterparty: "ИП Петров А.В.", status: "Ожидает", amount: 84_000 },
  { id: 5, date: "2026-05-05", purpose: "Налог УСН 6%", counterparty: "ФНС", status: "Проведена", amount: -612_000 },
  { id: 6, date: "2026-05-05", purpose: "Поступление по счёту 1283", counterparty: "АО «ЛогистикИнвест»", status: "Проведена", amount: 940_000 },
  { id: 7, date: "2026-05-04", purpose: "Эквайринг WB", counterparty: "Тинькофф", status: "Проведена", amount: 1_680_000 }
];
const statusBadge = (s: string) =>
  s === "Проведена" ? "badge-pos" : s === "Ожидает" ? "badge-warn" : "badge-info";

const incomeStructure = [
  { label: "Реализация — основная", value: 12_400_000, pct: 67.3 },
  { label: "Эквайринг маркетплейсы", value: 3_840_000, pct: 20.8 },
  { label: "Аренда субсчетов", value: 1_280_000, pct: 6.9 },
  { label: "Прочие", value: 900_000, pct: 5.0 }
];
const expenseStructure = [
  { label: "ФОТ + налоги", value: 5_120_000, pct: 37.0 },
  { label: "Закупка товара", value: 4_240_000, pct: 30.6 },
  { label: "Логистика", value: 1_680_000, pct: 12.1 },
  { label: "Аренда / коммуналка", value: 920_000, pct: 6.6 },
  { label: "Маркетинг", value: 740_000, pct: 5.3 },
  { label: "Прочие", value: 1_150_000, pct: 8.4 }
];
</script>

<style scoped>
/* Контекст текущего юр.лица в подзаголовке страницы */
.page-subtitle {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
}
.ctx-flag { font-size: 14px; line-height: 1; }
.ctx-label {
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
}
.ctx-sep { color: var(--text-muted); }

/* auto-fit + minmax — KPI сами разместятся: на 1920px ≈ 8 в ряд,
   на 1366px ≈ 6, на узких экранах — 2. Не нужны ручные breakpoint'ы,
   и нет «прыжка» при первом рендере, когда viewport ещё не стабилен. */
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: var(--sp-5);
  margin-bottom: var(--sp-7);
}

.dashboard-grid-2 {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(420px, 1fr));
  gap: var(--sp-5);
  margin-bottom: var(--sp-7);
}


/* Cashflow chart */
.legend {
  display: flex;
  gap: var(--sp-5);
  font-size: var(--fs-xs);
  color: var(--text-muted);
}
.legend-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 2px;
  background: var(--text-muted);
}
.dot.pos { background: var(--pos); }
.dot.neg { background: var(--neg); }
.dot.accent { background: var(--accent); }

.cashflow-chart { padding: var(--sp-6) var(--sp-6) var(--sp-4); }
.cashflow-svg {
  width: 100%;
  height: 200px;
  display: block;
}
.cashflow-svg .bar.pos { fill: var(--pos); opacity: 0.85; }
.cashflow-svg .bar.neg { fill: var(--neg); opacity: 0.7; }
.cashflow-svg .line-balance {
  stroke: var(--accent);
  stroke-width: 1.6;
  fill: none;
}
.chart-axis {
  display: grid;
  grid-template-columns: repeat(12, 1fr);
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  margin-top: var(--sp-3);
  letter-spacing: 0.04em;
}
.chart-axis span {
  text-align: center;
  font-family: var(--font-mono);
  text-transform: uppercase;
}

/* Accounts */
.accounts-list { display: flex; flex-direction: column; }
.account-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--sp-5) var(--sp-6);
  border-bottom: 1px solid var(--border);
  gap: var(--sp-5);
}
.account-row:last-child { border-bottom: none; }
.account-meta { min-width: 0; }
.account-name {
  font-size: var(--fs-base);
  font-weight: var(--fw-medium);
  color: var(--text-strong);
}
.account-bank {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  font-family: var(--font-mono);
  margin-top: 2px;
}
.account-balance {
  text-align: right;
  display: flex;
  flex-direction: column;
  gap: 2px;
  align-items: flex-end;
}
.account-balance .kpi-delta {
  font-size: var(--fs-xs);
}
.card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

/* Bar chart by category */
.bar-row {
  display: grid;
  grid-template-columns: 1fr 60px;
  align-items: center;
  gap: var(--sp-4);
  padding: var(--sp-3) 0;
  border-bottom: 1px solid var(--border);
}
.bar-row:last-child { border-bottom: none; }
.bar-row-label {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: var(--fs-sm);
  margin-bottom: var(--sp-2);
  grid-column: 1 / -1;
}
.bar-row-meta {
  text-align: right;
  font-family: var(--font-mono);
  font-size: var(--fs-xs);
  color: var(--text-muted);
  grid-column: 2;
  grid-row: 2;
}
.bar-track {
  height: 6px;
  background: var(--bg-surface-3);
  border-radius: var(--rd-pill);
  overflow: hidden;
  grid-column: 1;
  grid-row: 2;
}
.bar-fill {
  height: 100%;
  border-radius: var(--rd-pill);
  background: var(--accent);
  transition: width var(--t-slow);
}
.bar-fill.pos { background: var(--pos); }
.bar-fill.neg { background: var(--neg); }
</style>
