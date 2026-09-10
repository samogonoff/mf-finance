<!--
  Свод формы этапа 1.5 (ТЗ §8) и его свёртка в укрупнённые группы P&L (§12.3).

  Экран согласующего: он не вводит суммы, он смотрит, сходится ли свод. Поэтому
  здесь сперва контроли E-01…E-08 с расшифровкой (ТЗ §8: «контроль без
  расшифровки не принимается»), и только потом сами числа.

  Сворачивается ТАКТИКА — то, что заполняет форма; факт прошлого года и стратегия
  планового лежат рядом для сравнения (находка Н1: сценариев три).
-->
<template>
  <div class="page-sum">
    <header class="page-header">
      <div>
        <h1 class="page-title">Свод расходов ЦП · {{ sum?.plan_year || "" }}</h1>
        <p class="page-subtitle">
          {{ sum?.rows.length || 0 }} строк свода · {{ sum?.currency }} ·
          тактика за год {{ money(planTotal) }}
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="`/plans/exp-form/${cardId}`" class="btn btn-ghost">
          <Icon name="lucide:arrow-left" /> К форме
        </NuxtLink>
        <NuxtLink :to="`/plans/exp-registry/${cardId}`" class="btn btn-ghost">
          <Icon name="lucide:list-checks" /> Реестры
        </NuxtLink>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <SkeletonTable v-if="loading" :rows="12" :cols="8" label="Считаю свод" />

    <template v-else-if="sum">
      <!-- ===== Контроли §8 ===== -->
      <section class="card">
        <div class="card-header">
          <span class="card-title">Контрольные сверки</span>
          <span class="badge" :class="badCount ? 'badge-neg' : 'badge-pos'">
            {{ badCount ? `не сошлось: ${badCount}` : "все сверки нулевые" }}
          </span>
          <span v-if="skippedCount" class="badge badge-muted">не проверено: {{ skippedCount }}</span>
        </div>
        <table class="data-table compact">
          <thead>
            <tr><th>Код</th><th>Контроль</th><th class="col-num">Слева</th><th class="col-num">Справа</th><th class="col-num">Разница</th></tr>
          </thead>
          <tbody>
            <template v-for="r in sum.reconciliations" :key="r.code">
              <tr :class="r.skipped ? 'rec-skip' : r.ok ? '' : 'rec-bad'">
                <td><span class="badge" :class="r.skipped ? 'badge-muted' : r.ok ? 'badge-pos' : 'badge-neg'">{{ r.code }}</span></td>
                <td>
                  {{ r.title }}
                  <span v-if="r.note" class="rec-note">{{ r.note }}</span>
                </td>
                <td class="col-num">{{ r.skipped ? "—" : money(r.left) }}</td>
                <td class="col-num">{{ r.skipped ? "—" : money(r.right) }}</td>
                <td class="col-num" :class="!r.skipped && !r.ok ? 'bad' : ''">{{ r.skipped ? "—" : money(r.diff) }}</td>
              </tr>
              <tr v-for="(d, di) in (r.ok ? [] : r.details || [])" :key="`${r.code}-${di}`" class="rec-detail">
                <td></td>
                <td>ЦФО {{ d.code_cfo || "—" }}<template v-if="d.code_pl"> · статья {{ d.code_pl }}</template></td>
                <td class="col-num">{{ money(d.left) }}</td>
                <td class="col-num">{{ money(d.right) }}</td>
                <td class="col-num bad">{{ money(d.diff) }}</td>
              </tr>
            </template>
          </tbody>
        </table>
      </section>

      <!-- ===== Свёртка по укрупнённым группам P&L (§12.3) ===== -->
      <section class="card">
        <div class="card-header">
          <span class="card-title">Коммерческие расходы по подгруппам</span>
          <span class="hdr-hint">строка раскрывается до кодов PL — видно, что группировка не потеряла статью</span>
        </div>
        <table class="data-table compact sum-table">
          <thead>
            <tr>
              <th>Подгруппа</th>
              <th v-for="m in MONTHS_SHORT" :key="m" class="col-num c-mon">{{ m }}</th>
              <th class="col-num c-total">Итого</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="g in sum.groups" :key="g.pnl_group">
              <tr class="grp-row" :class="{ 'grp-unassigned': g.unassigned }" @click="toggle(g.pnl_group)">
                <td>
                  <Icon :name="open.has(g.pnl_group) ? 'lucide:chevron-down' : 'lucide:chevron-right'" />
                  {{ g.pnl_group }}
                  <span v-if="g.unassigned" class="grp-warn" title="у этих кодов PL нет второго уровня справочника — ошибкой не считается">
                    нет второго уровня справочника
                  </span>
                </td>
                <td v-for="m in 12" :key="m" class="col-num">{{ money(g.by_month[String(m)] || 0) }}</td>
                <td class="col-num strong">{{ money(g.total) }}</td>
              </tr>
              <tr v-if="open.has(g.pnl_group)" class="grp-codes">
                <td :colspan="14">Коды PL: {{ g.codes_pl.join(", ") }}</td>
              </tr>
            </template>
          </tbody>
        </table>
      </section>

      <!-- ===== Свод §8: строки «группа PL + статья + код PL» ===== -->
      <section class="card">
        <div class="card-header">
          <span class="card-title">Свод по статьям</span>
          <div class="seg-mini">
            <button
              v-for="sc in SCENARIOS"
              :key="sc.key"
              class="seg"
              :class="{ active: scenario === sc.key }"
              @click="scenario = sc.key"
            >{{ sc.label }}</button>
          </div>
        </div>
        <div class="sum-scroll">
          <table class="data-table compact sum-table">
            <thead>
              <tr>
                <th class="c-grp">Группа PL</th>
                <th class="c-exp">Статья</th>
                <th class="c-pl">Код PL</th>
                <th v-for="m in MONTHS_SHORT" :key="m" class="col-num c-mon">{{ m }}</th>
                <th class="col-num c-total">Итого</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(r, i) in sum.rows" :key="i">
                <td class="c-grp">{{ r.group_pl }}</td>
                <td class="c-exp" :title="r.expense">{{ r.expense || "—" }}</td>
                <td class="c-pl mono">{{ r.code_pl }}</td>
                <td v-for="m in 12" :key="m" class="col-num">{{ cell(r, m) }}</td>
                <td class="col-num strong">{{ money(r.totals[scenario] || 0) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { apiError } from "~/utils/apiError";
import { money } from "~/utils/format";
import { useExpForm, MONTHS_SHORT, type ExpSummary, type ExpSummaryRow } from "~/composables/useExpForm";

const route = useRoute();
const cardId = Number(route.params.cardId);
const api = useExpForm();

const sum = ref<ExpSummary | null>(null);
const loading = ref(true);
const error = ref("");
const open = ref<Set<string>>(new Set());

// Сценариев три (находка Н1): форма заполняет тактику, факт и стратегия рядом.
const SCENARIOS = [
  { key: "plan", label: "Тактика" },
  { key: "strategy", label: "Стратегия" },
  { key: "fact", label: "Факт прошлого года" }
];
const scenario = ref("plan");

const planTotal = computed(() => (sum.value?.groups || []).reduce((s, g) => s + g.total, 0));
const badCount = computed(() => (sum.value?.reconciliations || []).filter((r) => !r.ok && !r.skipped).length);
const skippedCount = computed(() => (sum.value?.reconciliations || []).filter((r) => r.skipped).length);

const cell = (r: ExpSummaryRow, m: number): string => {
  const v = r.by_scenario?.[scenario.value]?.[String(m)];
  return v === undefined ? "—" : money(v);
};

const toggle = (g: string) => {
  const next = new Set(open.value);
  next.has(g) ? next.delete(g) : next.add(g);
  open.value = next;
};

onMounted(async () => {
  try {
    sum.value = await api.summary(cardId);
  } catch (e) {
    error.value = apiError(e, "Свод не посчитан");
  } finally {
    loading.value = false;
  }
});
</script>

<style scoped>
.page-sum { display: flex; flex-direction: column; gap: var(--sp-4); }
.hdr-hint { font-size: var(--fs-2xs); color: var(--text-muted); }
.sum-scroll { overflow: auto; max-height: 60vh; }
.sum-table { min-width: 1100px; }
.sum-table thead th { position: sticky; top: 0; z-index: 2; background: var(--bg-surface-2); }
.c-grp { width: 200px; }
.c-exp { width: 220px; max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.c-pl { width: 70px; }
.c-mon { width: 88px; }
.c-total { width: 110px; }
.mono { font-family: var(--font-mono); }
.strong { font-weight: var(--fw-semibold); color: var(--text-strong); }
.bad { color: var(--neg); font-weight: var(--fw-semibold); }
.grp-row { cursor: pointer; }
.grp-row:hover { background: var(--bg-tonal); }
/* «Не распределено» видно, но не красным: по ответу 36 это не ошибка. */
.grp-unassigned { background: var(--warn-soft); }
.grp-warn { font-size: var(--fs-2xs); color: var(--warn); margin-left: var(--sp-3); }
.grp-codes td { font-size: var(--fs-2xs); color: var(--text-secondary); background: var(--bg-surface-2); }
.rec-bad td { background: var(--neg-soft); }
.rec-skip td { color: var(--text-muted); }
.rec-detail td { font-size: var(--fs-2xs); color: var(--text-secondary); background: var(--bg-surface-2); }
.rec-note { display: block; font-size: var(--fs-2xs); color: var(--text-muted); }
.seg-mini { display: flex; gap: var(--sp-2); margin-left: auto; }
</style>
