<template>
  <div class="page-counterparties">
    <header class="page-header">
      <div>
        <h1 class="page-title">Контрагенты</h1>
        <p class="page-subtitle">
          <span class="ctx-flag">{{ ctxFlag }}</span>
          <span class="ctx-label">{{ entityLabel }}</span>
          <span class="ctx-sep">·</span>
          <span>{{ filtered.length }} активных · оборот {{ moneyFmt(totalTurnover, "RUB", true) }}</span>
        </p>
      </div>
      <div class="page-actions">
        <button class="btn btn-ghost"><Icon name="lucide:download" /> Экспорт</button>
        <button class="btn btn-primary"><Icon name="lucide:plus" /> Добавить</button>
      </div>
    </header>

    <div class="filters-bar">
      <div class="search">
        <Icon name="lucide:search" class="search-icon" />
        <input v-model="q" placeholder="Поиск по названию, ИНН…" />
      </div>
      <div class="segmented">
        <button :aria-pressed="kind === 'all'" @click="kind = 'all'">Все</button>
        <button :aria-pressed="kind === 'customer'" @click="kind = 'customer'">Покупатели</button>
        <button :aria-pressed="kind === 'supplier'" @click="kind = 'supplier'">Поставщики</button>
      </div>
    </div>

    <div class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>Контрагент</th>
            <th>ИНН</th>
            <th>Тип</th>
            <th class="col-num">Оборот, год</th>
            <th class="col-num">Поступлений</th>
            <th class="col-num">Выплат</th>
            <th class="col-num">Сальдо</th>
            <th class="col-num">Δ vs прошл.</th>
            <th>Последняя операция</th>
            <th>Статус</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in filtered" :key="c.inn">
            <td class="ctp-name">
              <div class="ctp-avatar">{{ initials(c.name) }}</div>
              <div>
                <div class="ctp-name-strong">{{ c.name }}</div>
                <div class="ctp-name-sub">{{ c.legalForm }}</div>
              </div>
            </td>
            <td class="num">{{ c.inn }}</td>
            <td>
              <span class="badge" :class="c.kind === 'customer' ? 'badge-info' : 'badge-accent'">
                {{ c.kind === "customer" ? "Покупатель" : "Поставщик" }}
              </span>
            </td>
            <td class="col-num num-strong">{{ moneyFmt(c.turnover, "RUB", true) }}</td>
            <td class="col-num delta-pos">{{ moneyFmt(c.income, "RUB", true) }}</td>
            <td class="col-num delta-neg">{{ moneyFmt(-Math.abs(c.outcome), "RUB", true) }}</td>
            <td class="col-num" :class="c.balance >= 0 ? 'delta-pos' : 'delta-neg'">
              {{ moneyFmt(c.balance, "RUB", false, true) }}
            </td>
            <td class="col-num" :class="c.delta >= 0 ? 'delta-pos' : 'delta-neg'">{{ pctFmt(c.delta) }}</td>
            <td class="num">{{ dateFmt(c.lastDate) }}</td>
            <td>
              <span class="badge" :class="c.status === 'Активен' ? 'badge-pos' : 'badge-warn'">
                <span class="badge-dot"></span>{{ c.status }}
              </span>
            </td>
            <td>
              <button class="btn btn-ghost btn-icon btn-sm">
                <Icon name="lucide:more-horizontal" />
              </button>
            </td>
          </tr>
        </tbody>
        <tfoot>
          <tr>
            <td colspan="3">{{ filtered.length }} контрагентов</td>
            <td class="col-num">{{ moneyFmt(totalTurnover, "RUB", true) }}</td>
            <td class="col-num delta-pos">{{ moneyFmt(totalIncome, "RUB", true) }}</td>
            <td class="col-num delta-neg">{{ moneyFmt(-Math.abs(totalOutcome), "RUB", true) }}</td>
            <td class="col-num">{{ moneyFmt(totalBalance, "RUB", true, true) }}</td>
            <td colspan="4"></td>
          </tr>
        </tfoot>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { money, pct as pctFmt, formatDate } from "~/utils/format";

definePageMeta({ middleware: "scope-guard" });

const { label: entityLabel, countryLabel, mockMultiplier } = useEntity();
const ctxFlag = computed(() => countryLabel.value?.flag || "🌐");

const moneyFmt = (v: number, ccy = "RUB", compact = false, signed = false) =>
  money(v, { currency: ccy as any, compact, signed });
const dateFmt = (v: string) => formatDate(v, "short");

interface Ctp {
  name: string;
  legalForm: string;
  inn: string;
  kind: "customer" | "supplier";
  turnover: number;
  income: number;
  outcome: number;
  balance: number;
  delta: number;
  lastDate: string;
  status: "Активен" | "Просрочка";
}
const STATIC_LIST: Ctp[] = [
  { name: "Технополис-М", legalForm: "ООО", inn: "7723456789", kind: "customer", turnover: 4_820_000, income: 4_820_000, outcome: 0, balance: 240_000, delta: 8.4, lastDate: "2026-05-07", status: "Активен" },
  { name: "ЛогистикИнвест", legalForm: "АО", inn: "7714826401", kind: "supplier", turnover: 3_140_000, income: 0, outcome: 3_140_000, balance: -120_000, delta: -2.1, lastDate: "2026-05-06", status: "Активен" },
  { name: "Петров А.В.", legalForm: "ИП", inn: "504801234567", kind: "customer", turnover: 2_680_000, income: 2_680_000, outcome: 0, balance: 84_000, delta: 12.7, lastDate: "2026-05-06", status: "Активен" },
  { name: "Промкомплект", legalForm: "ООО", inn: "5024118327", kind: "supplier", turnover: 1_950_000, income: 0, outcome: 1_950_000, balance: -80_000, delta: 0, lastDate: "2026-05-04", status: "Активен" },
  { name: "Стройсервис", legalForm: "АО", inn: "7706329104", kind: "supplier", turnover: 1_480_000, income: 0, outcome: 1_480_000, balance: -340_000, delta: -1.4, lastDate: "2026-05-02", status: "Просрочка" },
  { name: "Электрон+", legalForm: "ООО", inn: "7728164290", kind: "customer", turnover: 1_120_000, income: 1_120_000, outcome: 0, balance: 56_000, delta: 4.2, lastDate: "2026-05-03", status: "Активен" },
  { name: "БЦ Орбита", legalForm: "АО", inn: "7705118200", kind: "supplier", turnover: 920_000, income: 0, outcome: 920_000, balance: 0, delta: 0, lastDate: "2026-05-01", status: "Активен" },
  { name: "Тинькофф (эквайринг)", legalForm: "Банк", inn: "7710140679", kind: "customer", turnover: 8_240_000, income: 8_240_000, outcome: 0, balance: 12_000, delta: 18.4, lastDate: "2026-05-07", status: "Активен" },
  { name: "Wildberries", legalForm: "Платформа", inn: "7721546864", kind: "customer", turnover: 6_140_000, income: 6_140_000, outcome: 80_000, balance: 320_000, delta: 22.0, lastDate: "2026-05-07", status: "Активен" },
  { name: "ФНС России", legalForm: "Гос.", inn: "7707329152", kind: "supplier", turnover: 720_000, income: 0, outcome: 720_000, balance: 0, delta: 6.0, lastDate: "2026-04-28", status: "Активен" }
];
// Реактивный список — на смену entity масштабируем суммы. На реальном API
// просто будут уходить разные contractor-ы по выбранному юр.лицу.
const list = computed<Ctp[]>(() => {
  const m = mockMultiplier.value;
  return STATIC_LIST.map((c) => ({
    ...c,
    turnover: Math.round(c.turnover * m),
    income: Math.round(c.income * m),
    outcome: Math.round(c.outcome * m),
    balance: Math.round(c.balance * m)
  }));
});

const q = ref("");
const kind = ref<"all" | "customer" | "supplier">("all");
const filtered = computed(() => {
  const ql = q.value.trim().toLowerCase();
  return list.value.filter((c) => {
    if (kind.value !== "all" && c.kind !== kind.value) return false;
    if (ql && !`${c.name} ${c.inn}`.toLowerCase().includes(ql)) return false;
    return true;
  });
});

const totalTurnover = computed(() => filtered.value.reduce((s, c) => s + c.turnover, 0));
const totalIncome = computed(() => filtered.value.reduce((s, c) => s + c.income, 0));
const totalOutcome = computed(() => filtered.value.reduce((s, c) => s + c.outcome, 0));
const totalBalance = computed(() => filtered.value.reduce((s, c) => s + c.balance, 0));

const initials = (n: string) =>
  n.split(/\s+/).slice(0, 2).map((w) => w[0]).join("").toUpperCase();
</script>

<style scoped>
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

.filters-bar {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  margin-bottom: var(--sp-5);
}

.ctp-name {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  min-width: 0;
}
.ctp-avatar {
  width: 28px;
  height: 28px;
  border-radius: var(--rd-3);
  background: var(--bg-surface-3);
  color: var(--text-secondary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--fs-xs);
  font-weight: var(--fw-bold);
  flex-shrink: 0;
  letter-spacing: 0.02em;
}
.ctp-name-strong {
  font-weight: var(--fw-medium);
  color: var(--text-strong);
}
.ctp-name-sub {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  font-family: var(--font-mono);
}
</style>
