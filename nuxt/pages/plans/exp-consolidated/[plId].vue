<!--
  Свод расходов розницы по четырём странам (аналог свода продаж розницы). Тот же вид — для
  интернет-магазина (?channel=im) и опта (?channel=opt, колонки РБ / РФ / УЗ; опт и ИМ вводятся в BYN).

  Строки — группы PL (в порядке листа «Розница» Excel) и статьи внутри них,
  колонки — страны. Общая валюта свода — BYN: суммы стран пересчитаны по курсу
  тактики, поэтому число страны совпадает со сводом её карточки в BYN.
  Окно (месяц / месяц–дек / год) и «Сравнение с фактом» — как на форме расходов.
-->
<template>
  <div class="page-econs">
    <header class="page-header">
      <div>
        <h1 class="page-title">{{ pageTitle }}</h1>
        <strong v-if="data" class="tactic-period">{{ periodLabel }}</strong>
        <p class="page-subtitle">
          Расходы по группам PL и статьям, {{ channel === "im" ? "одна форма" : channel === "all" ? "по каналам" : "по странам" }} · валюта свода {{ data?.currency || "BYN" }}
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
    <p v-if="data && data.missing?.length" class="banner banner-warn">
      Нет карточки расходов: {{ data.missing.join(", ") }}. Итог неполный.
    </p>
    <p v-if="data && failedCountries.length" class="banner banner-warn">
      Не удалось прочитать форму: {{ failedCountries.map((c) => `${c.country} (${c.error})`).join("; ") }}. Итог неполный.
    </p>
    <p v-if="data?.syncing && !pollExhausted" class="banner banner-info">
      Идёт загрузка данных из источника — таблица обновится сама. Пока показано то, что уже загружено.
    </p>
    <p v-else-if="data?.syncing" class="banner banner-warn">
      Загрузка из источника идёт дольше обычного. Пока показано то, что уже загружено; обновите страницу чуть позже.
      <button type="button" class="btn btn-sm btn-ghost" @click="load()">Обновить сейчас</button>
    </p>
    <p v-for="w in data?.warnings || []" :key="w" class="banner banner-warn">{{ w }}</p>
    <p v-if="data?.absent_channels?.length" class="banner banner-info">
      В периоде нет карточек расходов: {{ data.absent_channels.join(", ") }}.
    </p>
    <p v-if="noRate.length" class="banner banner-warn">
      Нет курса к BYN для {{ noRate.map((c) => `${c.country} (${c.nat_currency})`).join(", ") }}: суммы этих стран не показаны и в итог не входят. Итог неполный.
    </p>
    <p v-if="data?.fact_basis === 'payments'" class="banner banner-info">
      Факт построен по платежам: «Факта по начислениям» в источнике расходов пока нет.
    </p>

    <section class="card controls">
      <span class="view-lbl">Период</span>
      <div class="segmented" role="group" aria-label="Период свода">
        <button type="button" :aria-pressed="win === 'month'" @click="setWin('month')">{{ monthLabel }}</button>
        <button type="button" :aria-pressed="win === 'tail'" @click="setWin('tail')">{{ monthLabel }}–дек</button>
        <button type="button" :aria-pressed="win === 'year'" @click="setWin('year')">весь год</button>
      </div>
      <label class="checkbox" title="Факт того же периода прошлого года, месяца перед началом окна и месяцев окна текущего года (только чтение)">
        <input v-model="showRef" type="checkbox" @change="savePrefs" />
        Сравнение с фактом
      </label>
    </section>

    <SkeletonTable v-if="loading && !data" :rows="8" :cols="8" />

    <section v-if="data" class="card">
      <div class="table-scroll" :class="{ busy: loading }">
        <table class="data-table">
          <thead>
            <tr>
              <th>Группа / статья</th>
              <th v-for="c in data.countries" :key="c.country" class="col-num" :class="{ dim: !!c.error || !c.card_id }">
                <NuxtLink v-if="c.card_id" :to="`/plans/exp-form/${c.card_id}`" class="head-link" :title="`Открыть форму расходов: ${c.title || c.country}`">{{ c.country === "—" ? "прочие" : c.country }}</NuxtLink>
                <template v-else>{{ c.country === "—" ? "прочие" : c.country }}</template>
              </th>
              <th class="col-num">Итого</th>
              <template v-if="showRef">
                <th class="col-num ref-h" :title="factTitle">{{ factWord }} {{ lyLabel }}</th>
                <th class="col-num ref-h" :title="factTitle">{{ factWord }} {{ prevLabel }}</th>
                <th class="col-num ref-h" :title="`${factTitle} В колонке текущего года — только месяцы окна, по которым факт уже есть.`">{{ factWord }} {{ curLabel }}</th>
              </template>
            </tr>
            <tr class="sub-head">
              <th class="muted">статус карточки · курс к BYN</th>
              <th v-for="c in data.countries" :key="c.country" class="col-num muted">
                {{ c.error ? "ошибка" : c.no_rate ? "нет курса" : c.status || "—" }}<template v-if="c.card_id && c.rate && c.rate !== 1"> · {{ c.rate.toFixed(4) }}</template>
              </th>
              <th />
              <template v-if="showRef"><th /><th /><th /></template>
            </tr>
          </thead>
          <tbody>
            <tr class="row-total">
              <td>{{ totalLabel }}</td>
              <td v-for="c in data.countries" :key="c.country" class="col-num">{{ money0(data.total.plan[c.country]) }}</td>
              <td class="col-num">{{ money0(data.total.plan_total) }}</td>
              <template v-if="showRef">
                <td class="col-num">{{ money0(data.total.fact_ly) }}</td>
                <td class="col-num">{{ money0(data.total.fact_prev) }}</td>
                <td class="col-num">{{ money0(data.total.fact_cur) }}</td>
              </template>
            </tr>
            <template v-for="g in data.groups" :key="g.group">
              <tr class="row-group" @click="toggle(g.group)">
                <td>
                  <Icon :name="open.has(g.group) ? 'lucide:chevron-down' : 'lucide:chevron-right'" />
                  <b>{{ g.group }}</b>
                  <span v-if="g.unassigned" class="muted"> · статьи без группы</span>
                  <span class="muted"> · {{ g.lines.length }}</span>
                </td>
                <td v-for="c in data.countries" :key="c.country" class="col-num">{{ money0(g.plan[c.country]) }}</td>
                <td class="col-num strong">{{ money0(g.plan_total) }}</td>
                <template v-if="showRef">
                  <td class="col-num">{{ money0(g.fact_ly) }}</td>
                  <td class="col-num">{{ money0(g.fact_prev) }}</td>
                  <td class="col-num">{{ money0(g.fact_cur) }}</td>
                </template>
              </tr>
              <template v-if="open.has(g.group)">
                <tr v-for="l in g.lines" :key="`${g.group}:${l.code_pl}`" class="row-line">
                  <td><span class="code">{{ l.code_pl }}</span> {{ l.expense }}</td>
                  <td v-for="c in data.countries" :key="c.country" class="col-num">{{ money0(l.plan[c.country]) }}</td>
                  <td class="col-num">{{ money0(l.plan_total) }}</td>
                  <template v-if="showRef">
                    <td class="col-num">{{ money0(l.fact_ly) }}</td>
                    <td class="col-num">{{ money0(l.fact_prev) }}</td>
                    <td class="col-num">{{ money0(l.fact_cur) }}</td>
                  </template>
                </tr>
              </template>
            </template>
            <tr v-if="!data.groups.length">
              <td :colspan="colCount" class="empty-cell">По выбранному окну расходов нет.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import SkeletonTable from "~/components/SkeletonTable.vue";
import { MONTHS_SHORT, useExpForm, type ExpConsChannel, type ExpConsolidated, type ExpConsWindow } from "~/composables/useExpForm";
import { apiError } from "~/utils/apiError";
import { num as fmtNum } from "~/utils/format";

definePageMeta({ middleware: ["scope-guard"] });

const route = useRoute();
const plId = Number(route.params.plId);
const api = useExpForm();

// Канал свода: розница (4 страны), интернет-магазин или опт (одна карточка, колонки — страны ЦФО).
const channel = computed<ExpConsChannel>(() => {
  const q = route.query.channel;
  return q === "opt" || q === "im" || q === "all" ? q : "retail";
});
const pageTitle = computed(() => {
  switch (channel.value) {
    case "opt": return "Свод расходов опта · РБ, РФ, УЗ";
    case "im": return "Свод расходов интернет-магазина";
    case "all": return "Свод расходов · розница, ИМ, опт";
    default: return "Свод расходов розницы · все страны";
  }
});
const totalLabel = computed(() => {
  switch (channel.value) {
    case "opt": return "Расходы опта · итого";
    case "im": return "Расходы ИМ · итого";
    case "all": return "Расходы · итого по каналам";
    default: return "Расходы розницы · итого";
  }
});

const PREFS_KEY = "exp-cons-view";
const data = ref<ExpConsolidated | null>(null);
const loading = ref(false);
const error = ref("");
const win = ref<ExpConsWindow>("tail");
const showRef = ref(true);
const open = reactive(new Set<string>());

const month = computed(() => Math.min(12, Math.max(1, data.value?.month || 1)));
const monthLabel = computed(() => (data.value ? MONTHS_SHORT[month.value - 1] : "месяц"));
const periodLabel = computed(() => {
  if (!data.value) return "";
  const { year, from_month: from, to_month: to } = data.value;
  return from === to ? `План: ${MONTHS_SHORT[from - 1]} ${year}` : `План: ${MONTHS_SHORT[from - 1]}–${MONTHS_SHORT[to - 1]} ${year}`;
});
const factWord = computed(() => (data.value?.fact_basis === "payments" ? "Факт по платежам" : "Факт"));
// Колонки факта следуют за окном свода (from_month–to_month из ответа): тот же период прошлого года, месяц перед началом
// окна и месяцы окна текущего года (по которым факт уже есть). Для окна «месяц» — один месяц во всех трёх колонках.
const windowLabel = (year: number) => {
  if (!data.value) return "";
  const { from_month: from, to_month: to } = data.value;
  return from === to ? `${MONTHS_SHORT[from - 1]} ${year}` : `${MONTHS_SHORT[from - 1]}–${MONTHS_SHORT[to - 1]} ${year}`;
};
// Месяцы окна в колонке пересчитываются в BYN по одному курсу (по-месячных курсов в справочнике сводов нет).
const factTitle = "Суммы месяцев окна пересчитаны в BYN по одному курсу колонки.";
const lyLabel = computed(() => (data.value ? windowLabel(data.value.year - 1) : ""));
const prevLabel = computed(() => {
  if (!data.value) return "";
  const from = data.value.from_month;
  return from <= 1 ? `${MONTHS_SHORT[11]} ${data.value.year - 1}` : `${MONTHS_SHORT[from - 2]} ${data.value.year}`;
});
const curLabel = computed(() => (data.value ? windowLabel(data.value.year) : ""));
const failedCountries = computed(() => (data.value?.countries || []).filter((c) => !!c.error));
// Курса валюты страны к BYN в справочнике нет: сервер не складывает такие суммы с BYN.
const noRate = computed(() => (data.value?.countries || []).filter((c) => !!c.no_rate));
const colCount = computed(() => (data.value?.countries.length || 0) + 2 + (showRef.value ? 3 : 0));
const allOpen = computed(() => !!data.value && data.value.groups.length > 0 && data.value.groups.every((g) => open.has(g.group)));

const money0 = (v: number | null | undefined): string => (typeof v === "number" && v !== 0 ? fmtNum(v, 0) : "—");
const toggle = (g: string) => (open.has(g) ? open.delete(g) : open.add(g));
const toggleAll = () => {
  const target = !allOpen.value;
  open.clear();
  if (target) for (const g of data.value?.groups || []) open.add(g.group);
};

const loadPrefs = () => {
  try {
    const raw = JSON.parse(localStorage.getItem(PREFS_KEY) || "{}");
    if (raw.win === "month" || raw.win === "tail" || raw.win === "year") win.value = raw.win;
    if (typeof raw.ref === "boolean") showRef.value = raw.ref;
  } catch {
    // Нет сохранённого вида — остаются значения по умолчанию.
  }
};
const savePrefs = () => {
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify({ win: win.value, ref: showRef.value }));
  } catch {
    // Хранилище недоступно — вид просто не запомнится.
  }
};
const setWin = (w: ExpConsWindow) => {
  if (win.value === w) return;
  win.value = w;
  savePrefs();
  load();
};

// Ответы приходят не по порядку, если окно переключают быстро: учитываем только последний запрос.
let seq = 0;
// Пока по какой-то карточке идёт загрузка из источника (свод сам её запускает), страница молча перечитывает свод.
let pollTimer: ReturnType<typeof setTimeout> | null = null;
let polls = 0;
// Опрос исчерпан, а загрузка всё ещё идёт: баннер перестаёт обещать автообновление.
const pollExhausted = ref(false);
const POLL_MS = 5000;
const POLL_MAX = 60;
const stopPoll = () => {
  if (pollTimer) clearTimeout(pollTimer);
  pollTimer = null;
};
const load = async (opts?: { poll?: boolean }) => {
  const my = ++seq;
  const silent = opts?.poll === true;
  if (!silent) {
    stopPoll();
    polls = 0;
    pollExhausted.value = false;
    loading.value = true;
  }
  error.value = "";
  try {
    const r = await api.consolidated(plId, win.value, channel.value);
    if (my === seq) {
      data.value = r;
      if (r.syncing && polls < POLL_MAX) {
        polls += 1;
        pollTimer = setTimeout(() => load({ poll: true }), POLL_MS);
      } else if (r.syncing) {
        pollExhausted.value = true;
      }
    }
  } catch (e) {
    if (my === seq) error.value = apiError(e, "Не удалось загрузить свод расходов");
  } finally {
    if (my === seq && !silent) loading.value = false;
  }
};

onBeforeUnmount(stopPoll);

onMounted(() => {
  loadPrefs();
  load();
});

// Переход между каналами не пересоздаёт страницу: перечитываем свод сами.
watch(channel, () => {
  data.value = null;
  open.clear();
  load();
});
</script>

<style scoped>
.page-econs { display: flex; flex-direction: column; gap: var(--sp-4); }
.tactic-period { display: block; font-size: var(--fs-lg); margin: var(--sp-1) 0; }
.controls { display: flex; align-items: center; flex-wrap: wrap; gap: var(--sp-4); padding: var(--sp-3) var(--sp-4); }
.view-lbl { color: var(--text-muted); font-size: var(--fs-sm); }
.table-scroll { overflow-x: auto; transition: opacity 0.15s; }
.table-scroll.busy { opacity: 0.6; }
.head-link { color: inherit; text-decoration: underline; text-decoration-color: var(--border-strong); }
.dim { color: var(--text-muted); }
.ref-h { color: var(--text-muted); }
.sub-head th { font-weight: 400; font-size: var(--fs-xs); border-top: 0; padding-top: 0; }
.row-total td { font-weight: 600; border-bottom: 1px solid var(--border-strong); }
.row-group { cursor: pointer; }
.row-group td { font-weight: 500; }
.row-line td { font-size: var(--fs-sm); }
.row-line td:first-child { padding-left: var(--sp-6); }
.code { font-family: var(--font-mono); color: var(--text-muted); }
.muted { color: var(--text-muted); font-weight: 400; }
.col-num { font-family: var(--font-mono); font-variant-numeric: tabular-nums; text-align: right; white-space: nowrap; }
.strong { font-weight: 600; }
.empty-cell { text-align: center; color: var(--text-muted); padding: var(--sp-6); }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
