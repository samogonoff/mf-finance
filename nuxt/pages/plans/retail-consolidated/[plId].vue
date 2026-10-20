<!--
  Свод «все четыре розницы» (ТЗ доработок кабинета, п. 3.2).

  Три уровня: итог по рознице → страна → магазин. Общая валюта свода — BYN:
  нац. валюты стран несопоставимы, а пересчёт делается теми же курсами по
  назначению показателя, что и в форме, поэтому числа страны совпадают со
  сводом её карточки в блоке BYN. Страна раскрывается по клику — как и экран
  согласования, свод открывается свёрнутым.

  Шлюз ФЗ 02.10 п. 18: на утверждение (1.4) розница уходит только когда все
  четыре страны пройдены. Кого ждём — наверху, до чисел.
-->
<template>
  <div class="page-rcons">
    <header class="page-header">
      <div>
        <h1 class="page-title">Свод розницы · все страны</h1>
        <strong v-if="data" class="tactic-period">Тактика: {{ monthName(data.month) }} {{ data.year }}</strong>
        <p class="page-subtitle">
          Итог по четырём розницам с раскрытием до страны и магазина · валюта свода BYN
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="`/plans/${plId}`" class="btn btn-sm btn-ghost">
          <Icon name="lucide:arrow-left" /> К периоду
        </NuxtLink>
        <button class="btn btn-sm btn-ghost" :disabled="!data" @click="toggleAll">
          <Icon :name="allOpen ? 'lucide:chevrons-down-up' : 'lucide:chevrons-up-down'" />
          {{ allOpen ? "Свернуть всё" : "Раскрыть всё" }}
        </button>
        <button class="btn btn-sm btn-ghost" :disabled="loading" @click="load">
          <Icon name="lucide:refresh-cw" :class="{ spin: loading }" /> Обновить
        </button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="data && data.pending.length" class="banner banner-warn">
      На утверждение розница уйдёт, когда пройдены все четыре страны. Ждём: {{ data.pending.join(", ") }}.
    </p>
    <p v-else-if="data" class="banner banner-pos">Все четыре страны пройдены — розница может уходить на утверждение.</p>

    <SkeletonTable v-if="loading && !data" :rows="6" :cols="9" />

    <section v-if="data" class="card">
      <div class="table-scroll">
        <table class="data-table">
          <thead>
            <tr>
              <th>Розница / страна / магазин</th>
              <th class="col-num">Магазинов</th>
              <th class="col-num">Факт пред. мес.</th>
              <th class="col-num">Факт ПГ</th>
              <th class="col-num">Стратегия</th>
              <th class="col-num">Тактика</th>
              <th class="col-num">Т / С</th>
              <th class="col-num">LFL</th>
              <th class="col-num">Итого за период</th>
              <th class="col-num">Ожидание года</th>
            </tr>
          </thead>
          <tbody>
            <tr class="row-total">
              <td>Розница · итого (BYN)</td>
              <td class="col-num">{{ totalRows }}</td>
              <MetricCells :m="data.total" />
            </tr>
            <template v-for="c in data.countries" :key="c.country">
              <tr class="row-country" :class="{ dim: !!c.error }" @click="!c.error && toggle(c.country)">
                <td>
                  <Icon v-if="!c.error" :name="open.has(c.country) ? 'lucide:chevron-down' : 'lucide:chevron-right'" />
                  <b>{{ c.country }}</b>
                  <span class="muted"> · {{ c.error || `${c.status} · нац. валюта ${c.nat_currency}` }}</span>
                  <NuxtLink v-if="c.card_id" :to="`/plans/retail-summary/${c.card_id}`" class="row-link" @click.stop>свод страны</NuxtLink>
                </td>
                <td class="col-num">{{ c.error ? "—" : c.rows }}</td>
                <MetricCells v-if="!c.error" :m="c.byn" />
                <td v-else colspan="8" />
              </tr>
              <template v-if="open.has(c.country)">
                <tr v-for="s in c.stores" :key="`${c.country}:${s.code_cfo}`" class="row-store">
                  <td>
                    <span class="code">{{ s.code_cfo }}</span> {{ s.cfo }}
                    <span class="muted"> · {{ s.city || "—" }} · {{ s.lfl || "—" }}</span>
                  </td>
                  <td class="col-num" />
                  <MetricCells :m="s.metrics" />
                </tr>
              </template>
            </template>
          </tbody>
        </table>
      </div>
    </section>

    <!-- ТЗ доработок п. 5.2: список LFL-статусов — из справочника, а не из файла. -->
    <section v-if="lfl" class="card">
      <div class="card-header">
        <span class="card-title">Статусы LFL в справочнике магазинов</span>
        <span class="muted">источник: {{ lfl.source }}</span>
        <button v-if="canAdmin" class="btn btn-sm btn-ghost" :disabled="refreshBusy" @click="refreshPreview">
          <Icon name="lucide:refresh-cw" :class="{ spin: refreshBusy }" /> Обновить статусы во всех периодах
        </button>
      </div>
      <p v-if="refreshError" class="banner banner-neg">{{ refreshError }}</p>
      <p v-if="refreshDone" class="banner banner-pos">{{ refreshDone }}</p>
      <div v-if="refresh" class="refresh-box">
        <p v-if="!refresh.rows_changed" class="banner banner-info">
          Менять нечего: устаревшего «нового» у уже сравнимых магазинов не осталось (магазинов в Лисе: {{ refresh.stores_in_lisa }}).
        </p>
        <p v-if="refresh.rows_skipped" class="muted">
          Не переносится в прошлые периоды: {{ refresh.rows_skipped }} строк. Статусы «закрыт», «реконструкция», «до года» и «новый»
          зависят от даты (магазин закрыли в сентябре — в январе он ещё работал), поэтому в прошлом исправляется только устаревшее
          «новый» у магазинов, которые в периоде уже были сравнимыми (LFL). В редактируемых карточках статусы обновляются при открытии формы.
        </p>
        <template v-else>
          <p class="banner banner-warn">
            Будет изменён статус у {{ refresh.rows_changed }} строк в {{ refresh.cards_changed }} карточках периодов, из них
            {{ refresh.locked_cards }} уже закрыты для правок (утверждены или на согласовании). Значения плана и листы
            согласования не меняются, но от статуса зависят LFL и % выполнения — они пересчитаются.
          </p>
          <table class="data-table compact">
            <thead><tr><th>Было</th><th>Станет</th><th class="col-num">Строк</th></tr></thead>
            <tbody>
              <tr v-for="t in refresh.transitions" :key="`${t.from}>${t.to}`">
                <td>{{ lflLabel(t.from) }}</td><td>{{ lflLabel(t.to) }}</td><td class="col-num">{{ t.rows }}</td>
              </tr>
            </tbody>
          </table>
          <div class="refresh-actions">
            <button class="btn btn-sm btn-primary" :disabled="refreshBusy" @click="refreshApply">Применить ко всем периодам</button>
            <button class="btn btn-sm btn-ghost" :disabled="refreshBusy" @click="refresh = null">Отмена</button>
          </div>
        </template>
      </div>
      <table class="data-table compact">
        <thead>
          <tr>
            <th>Статус в справочнике</th>
            <th>Как понимает форма</th>
            <th class="col-num">Магазинов</th>
            <th v-for="c in ['BY', 'RU', 'KZ', 'UZ']" :key="c" class="col-num">{{ c }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in lfl.statuses" :key="s.raw">
            <td>{{ s.raw }}</td>
            <td class="muted">{{ s.canonical || "—" }}</td>
            <td class="col-num">{{ s.stores }}</td>
            <td v-for="c in ['BY', 'RU', 'KZ', 'UZ']" :key="c" class="col-num">{{ s.by_country[c] || "" }}</td>
          </tr>
        </tbody>
      </table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { defineComponent, h } from "vue";
import SkeletonTable from "~/components/SkeletonTable.vue";
import { retailErrText, useRetail, type RetailConsolidated, type RetailLFLRefreshResult, type RetailLFLStatusCount, type RetailSummaryMetrics } from "~/composables/useRetail";
import { num as fmtNum } from "~/utils/format";

definePageMeta({ middleware: ["scope-guard"] });

const route = useRoute();
const plId = Number(route.params.plId);
const api = useRetail();

const data = ref<RetailConsolidated | null>(null);
const lfl = ref<{ source: string; statuses: RetailLFLStatusCount[] } | null>(null);
const loading = ref(false);
const error = ref("");
const open = reactive(new Set<string>());

// Обновление LFL-статусов во всех периодах — разовая операция администратора: сначала предпросмотр.
const { hasRole, isAdmin } = useScope();
const canAdmin = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));
const refresh = ref<RetailLFLRefreshResult | null>(null);
const refreshBusy = ref(false);
const refreshError = ref("");
const refreshDone = ref("");
const LFL_LABELS: Record<string, string> = {
  lfl: "LFL", under1y: "до года", new: "новый", closed: "закрыт", xxx: "ххх", reconstruction: "реконструкция"
};
const lflLabel = (code: string): string => LFL_LABELS[code] || code || "—";
const refreshPreview = async () => {
  refreshBusy.value = true;
  refreshError.value = "";
  refreshDone.value = "";
  try {
    refresh.value = await api.lflRefresh(true);
  } catch (e) {
    refresh.value = null;
    refreshError.value = retailErrText(e, "Не удалось посчитать обновление статусов");
  } finally {
    refreshBusy.value = false;
  }
};
const refreshApply = async () => {
  refreshBusy.value = true;
  refreshError.value = "";
  try {
    const r = await api.lflRefresh(false);
    refresh.value = null;
    refreshDone.value = `Статусы обновлены: ${r.rows_changed} строк в ${r.cards_changed} карточках.`;
    api.lflStatuses().then((x) => (lfl.value = x)).catch(() => undefined);
  } catch (e) {
    refreshError.value = retailErrText(e, "Не удалось обновить статусы");
  } finally {
    refreshBusy.value = false;
  }
};

const monthName = (m: number): string =>
  new Intl.DateTimeFormat("ru-RU", { month: "long" }).format(new Date(2000, m - 1, 1));
const totalRows = computed(() => (data.value?.countries || []).reduce((a, c) => a + (c.rows || 0), 0));
const allOpen = computed(() => !!data.value && data.value.countries.every((c) => !!c.error || open.has(c.country)));

const toggle = (country: string) => (open.has(country) ? open.delete(country) : open.add(country));
const toggleAll = () => {
  const target = !allOpen.value;
  open.clear();
  if (target) for (const c of data.value?.countries || []) if (!c.error) open.add(c.country);
};

// Ячейки показателей — одна разметка для итога, страны и магазина.
const money = (v: unknown): string => (typeof v === "number" && v !== 0 ? fmtNum(v, 0) : "—");
const pct = (v: unknown): string => (typeof v === "number" && v !== 0 ? `${(v * 100).toFixed(1)} %` : "—");
const MetricCells = defineComponent({
  props: { m: { type: Object as () => RetailSummaryMetrics, required: true } },
  setup(props) {
    return () => [
      h("td", { class: "col-num" }, money(props.m.fact_prev_month)),
      h("td", { class: "col-num" }, money(props.m.fact_prev_year)),
      h("td", { class: "col-num" }, money(props.m.strategy)),
      h("td", { class: "col-num strong" }, money(props.m.tactic)),
      h("td", { class: "col-num" }, pct(props.m.tactic_vs_strategy)),
      h("td", { class: "col-num" }, pct(props.m.lfl)),
      h("td", { class: "col-num" }, money(props.m.period_total)),
      h("td", { class: "col-num" }, money(props.m.year_expectation))
    ];
  }
});

const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    data.value = await api.consolidated(plId);
    // Справочник статусов — второстепенный блок: его недоступность не мешает своду.
    api.lflStatuses().then((r) => (lfl.value = r)).catch(() => (lfl.value = null));
  } catch (e) {
    error.value = retailErrText(e, "Не удалось загрузить свод розницы");
  } finally {
    loading.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.page-rcons { display: flex; flex-direction: column; gap: var(--sp-4); }
.tactic-period { display: block; font-size: var(--fs-lg); margin: var(--sp-1) 0; }
.table-scroll { overflow-x: auto; }
.row-total td { font-weight: 600; border-bottom: 1px solid var(--border-strong); }
.row-country { cursor: pointer; }
.row-country td { font-weight: 500; }
.row-country.dim { cursor: default; color: var(--text-muted); }
.row-store td:first-child { padding-left: var(--sp-6); }
.row-store td { font-size: var(--fs-sm); }
.row-link { margin-left: var(--sp-3); font-size: var(--fs-xs); }
.code { font-family: var(--font-mono); color: var(--text-muted); }
.muted { color: var(--text-muted); font-weight: 400; }
.col-num { font-family: var(--font-mono); font-variant-numeric: tabular-nums; text-align: right; white-space: nowrap; }
.strong { font-weight: 600; }
.refresh-box { padding: var(--sp-3) var(--sp-4); display: flex; flex-direction: column; gap: var(--sp-3); }
.refresh-actions { display: flex; gap: var(--sp-2); }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
