<!--
  Свод «все четыре розницы» (ТЗ доработок кабинета, п. 3.2).

  Три уровня: итог по рознице → страна → магазин. Общая валюта свода — BYN:
  нац. валюты стран несопоставимы, а пересчёт делается теми же курсами по
  назначению показателя, что и в форме, поэтому числа страны совпадают со
  сводом её карточки в блоке BYN. Страна раскрывается по клику — как и экран
  согласования, свод открывается свёрнутым.

  Шаг 1.4 (Сипарова) утверждает поток розницы целиком: директор принимает свою страну независимо
  от остальных, а решение шага 1.4 доступно, когда все четыре страны до него дошли. Кого ждём —
  наверху, до чисел. Свод видят все; кнопки «Утвердить» / «Вернуть» по каждой стране — только
  согласующему шага 1.4 (и админу) и только когда страны собрались.
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
    <p v-if="actionError" class="banner banner-neg">{{ actionError }}</p>
    <p v-if="actionNote" class="banner banner-pos">{{ actionNote }}</p>
    <p v-if="data && data.pending.length" class="banner banner-warn">
      Сипарова утвердит розницу, когда все четыре страны пройдут директора. Ждём: {{ data.pending.join(", ") }}.
    </p>
    <p v-else-if="data" class="banner banner-pos">Все четыре страны дошли до утверждения (шаг 1.4).</p>

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
              <th v-if="hasFinal" class="col-act">Шаг 1.4</th>
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
                <td v-if="hasFinal" class="col-act" @click.stop>
                  <template v-if="c.can_decide">
                    <button class="btn btn-xs btn-primary" :disabled="actBusy" @click="approve(c)">Утвердить</button>
                    <button class="btn btn-xs btn-ghost" :disabled="actBusy" @click="openReturn(c)">Вернуть</button>
                  </template>
                  <span v-else-if="c.at_final_step" class="muted" title="Решение доступно согласующему шага 1.4, когда все четыре страны дошли до него">
                    {{ data.pending.length ? "ждём другие страны" : "ждёт решения" }}
                  </span>
                  <span v-else class="muted">—</span>
                </td>
              </tr>
              <tr v-if="ret.country === c.country" class="row-return">
                <td :colspan="hasFinal ? 11 : 10">
                  <div class="return-box">
                    <label class="fld">
                      <span class="fld-lbl">Вернуть {{ c.country }} на шаг</span>
                      <select v-model="ret.target" class="input">
                        <option value="" disabled>выберите шаг</option>
                        <option v-for="s in data.return_steps" :key="s.code" :value="s.code">{{ s.code }} · {{ s.name }}</option>
                      </select>
                    </label>
                    <label class="fld return-comment">
                      <span class="fld-lbl">Причина (обязательна)</span>
                      <textarea v-model="ret.comment" class="textarea" rows="2" placeholder="что нужно исправить"></textarea>
                    </label>
                    <div class="return-actions">
                      <button class="btn btn-sm btn-primary" :disabled="actBusy || !ret.target || !ret.comment.trim()" @click="submitReturn(c)">Вернуть</button>
                      <button class="btn btn-sm btn-ghost" :disabled="actBusy" @click="ret.country = ''">Отмена</button>
                    </div>
                  </div>
                </td>
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
import { useMpConditions } from "~/composables/useMpConditions";
import { apiError } from "~/utils/apiError";
import SkeletonTable from "~/components/SkeletonTable.vue";
import { retailErrText, useRetail, type RetailConsolidated, type RetailConsolidatedCountry, type RetailLFLStatusCount, type RetailSummaryMetrics } from "~/composables/useRetail";
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

const monthName = (m: number): string =>
  new Intl.DateTimeFormat("ru-RU", { month: "long" }).format(new Date(2000, m - 1, 1));
// Решение шага 1.4 со свода: утвердить или вернуть страну. Права и «все четыре страны» проверяет сервер,
// кнопки показываются только тому, у кого они сработают.
const cardsApi = useMpConditions();
const hasFinal = computed(() => !!data.value?.countries.some((c) => c.at_final_step));
const actBusy = ref(false);
const actionError = ref("");
const actionNote = ref("");
const ret = reactive({ country: "", target: "", comment: "" });
const approve = async (c: RetailConsolidatedCountry) => {
  actBusy.value = true;
  actionError.value = "";
  actionNote.value = "";
  try {
    await cardsApi.cardAction(c.card_id, { action: "approve" });
    actionNote.value = `${c.country}: розница утверждена.`;
    await load();
  } catch (e) {
    actionError.value = apiError(e, "Не удалось утвердить");
  } finally {
    actBusy.value = false;
  }
};
const openReturn = (c: RetailConsolidatedCountry) => {
  ret.country = c.country;
  ret.target = "";
  ret.comment = "";
};
const submitReturn = async (c: RetailConsolidatedCountry) => {
  actBusy.value = true;
  actionError.value = "";
  actionNote.value = "";
  try {
    await cardsApi.cardAction(c.card_id, { action: "return", target_step: ret.target, comment: ret.comment.trim() });
    actionNote.value = `${c.country}: возвращено на шаг ${ret.target}.`;
    ret.country = "";
    await load();
  } catch (e) {
    actionError.value = apiError(e, "Не удалось вернуть");
  } finally {
    actBusy.value = false;
  }
};

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
.col-act { white-space: nowrap; text-align: right; }
.row-return td { background: var(--bg-surface-2); }
/* Форма возврата не уезжает при горизонтальной прокрутке широкой таблицы. */
.return-box { position: sticky; left: 0; box-sizing: border-box; width: min(920px, 92vw); display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--sp-3); padding: var(--sp-3) var(--sp-4); }
.return-box .fld { display: flex; flex-direction: column; gap: var(--sp-1); }
.return-comment { flex: 1 1 280px; }
.return-actions { display: flex; gap: var(--sp-2); }
.row-link { margin-left: var(--sp-3); font-size: var(--fs-xs); }
.code { font-family: var(--font-mono); color: var(--text-muted); }
.muted { color: var(--text-muted); font-weight: 400; }
.col-num { font-family: var(--font-mono); font-variant-numeric: tabular-nums; text-align: right; white-space: nowrap; }
.strong { font-weight: 600; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
