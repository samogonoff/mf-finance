<!--
  «Общие затраты по МП» (ТЗ МП §4.3) — 7 групп статей P&L, которые НЕ считаются
  от продаж: у них нет удельного веса в выручке, они вводятся руками или
  копируются из предыдущего периода/стратегии. Поэтому это отдельный экран, а не
  строки сетки формы: в форме каждая статья — доля от оборота площадки, здесь —
  сумма на всю группу МП.

  Экран закрывает: ввод суммы по статье, подытоги по группам и общий итог (он
  входит в PL формы), блокировку ввода после утверждения периода (§2.4).
-->
<template>
  <div class="page-cc">
    <header class="page-header">
      <div>
        <h1 class="page-title">Общие затраты по МП · {{ periodLabel }}</h1>
        <p class="page-subtitle">
          {{ card?.title || "Маркетплейсы" }} · {{ groups.length }} групп статей ·
          итого {{ money(grandTotal) }} {{ currency }}
          <span v-if="card?.locked" class="lock-note"><Icon name="lucide:lock" /> период закрыт на запись</span>
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink v-if="card" :to="`/plans/${card.pl_id}`" class="btn btn-ghost">
          <Icon name="lucide:arrow-left" /> К периоду
        </NuxtLink>
        <NuxtLink v-if="cardId" :to="`/plans/mp-conditions/${cardId}`" class="btn btn-ghost">
          <Icon name="lucide:sliders-horizontal" /> Условия площадок
        </NuxtLink>
        <button class="btn btn-sm btn-primary" :disabled="!editable || busy || !dirty" @click="save">
          <Icon name="lucide:save" :class="{ spin: busy }" /> Сохранить
          <span v-if="dirty" class="dirty-dot" title="есть несохранённые изменения">●</span>
        </button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>
    <p class="banner banner-info">
      Эти статьи не считаются от продаж и не имеют доли в обороте: сумма вводится или
      копируется. Итог входит в PL формы как «Общие затраты».
    </p>
    <p v-if="loaded && !editable" class="banner banner-warn">
      Только просмотр: период закрыт или карточка не в работе. Изменение утверждённых сумм —
      через переоткрытие периода с причиной.
    </p>

    <SkeletonTable v-if="loading" :rows="14" :cols="2" :section-every="5" label="Загружаю статьи общих затрат" />

    <section v-for="g in groups" :key="g.group" class="card grp">
      <div class="card-header grp-head">
        <span class="card-title">{{ g.group }}</span>
        <span class="grp-sum col-num">{{ money(groupTotal(g)) }}</span>
      </div>
      <table class="data-table compact">
        <thead>
          <tr>
            <th class="th-code">Код PL</th>
            <th>Статья</th>
            <th class="col-num th-amt">Сумма</th>
            <th class="th-src">Источник</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="l in g.lines" :key="l.code_pl">
            <td class="cd">{{ l.code_pl }}</td>
            <td>{{ lineName(l) }}</td>
            <td class="col-num">
              <NumberField
                :model-value="amounts[l.code_pl] ?? null"
                class="amt-input"
                :disabled="!editable"
                placeholder="—"
                @update:model-value="onInput(l.code_pl, $event)"
              />
            </td>
            <td class="src">{{ sourceOf(l.code_pl) }}</td>
          </tr>
        </tbody>
        <tfoot>
          <tr>
            <td colspan="2">Итого «{{ g.group }}»</td>
            <td class="col-num strong">{{ money(groupTotal(g)) }}</td>
            <td></td>
          </tr>
        </tfoot>
      </table>
    </section>

    <section v-if="loaded" class="card total-card">
      <div class="tc-row">
        <span class="tc-lbl">Общие затраты по МП, итого</span>
        <span class="tc-val">{{ money(grandTotal) }} {{ currency }}</span>
      </div>
      <p class="tc-hint">
        Заполнено статей: {{ filledCount }} из {{ lineCount }}. Пустая статья сохраняется как ноль
        только если раньше по ней была сумма — иначе строка в период не пишется.
      </p>
    </section>
  </div>
</template>

<script setup lang="ts">
import NumberField from "~/components/NumberField.vue";
import SkeletonTable from "~/components/SkeletonTable.vue";
import {
  useMpConditions,
  type MpCommonCostGroup,
  type MpCommonCostLine,
  type MpCommonCostValue,
  type PlanCard
} from "~/composables/useMpConditions";
import { num } from "~/utils/format";

definePageMeta({ middleware: ["scope-guard"] });

const route = useRoute();
const cardId = Number(route.params.cardId);
const api = useMpConditions();

const card = ref<PlanCard | null>(null);
const groups = ref<MpCommonCostGroup[]>([]);
const values = ref<MpCommonCostValue[]>([]);
const amounts = reactive<Record<number, number | null>>({});
const origin = reactive<Record<number, number | null>>({});
const loading = ref(true);
const loaded = ref(false);
const busy = ref(false);
const dirty = ref(false);
const error = ref("");
const note = ref("");

const editable = computed(() => !!card.value && !card.value.locked && ["draft", "returned"].includes(card.value.status));
const periodLabel = computed(() =>
  card.value ? `${card.value.year}-${String(card.value.month).padStart(2, "0")}` : ""
);
const currency = computed(() => card.value?.currency || values.value[0]?.currency || "RUB");

// Сервер отдаёт статью структурой PLLineRow, где название сериализуется как
// expense_name. Читаем оба имени: значения (MpCommonCostValue) несут name.
const lineName = (l: MpCommonCostLine): string =>
  l.expense_name || l.name || `Статья ${l.code_pl}`;

const SOURCE_LABEL: Record<string, string> = {
  manual: "ввод",
  copy: "копия прошлого периода",
  strategy: "стратегия",
  import: "импорт"
};
const sourceOf = (codePL: number): string => {
  const v = values.value.find((x) => x.code_pl === codePL);
  if (!v) return "—";
  return SOURCE_LABEL[v.source] || v.source || "ввод";
};

const money = (v: number | null | undefined) => (v == null || Number.isNaN(v) ? "—" : num(v, 2));

const groupTotal = (g: MpCommonCostGroup): number =>
  g.lines.reduce((s, l) => s + (amounts[l.code_pl] ?? 0), 0);
const grandTotal = computed(() => groups.value.reduce((s, g) => s + groupTotal(g), 0));
const lineCount = computed(() => groups.value.reduce((s, g) => s + g.lines.length, 0));
const filledCount = computed(() =>
  groups.value.reduce((s, g) => s + g.lines.filter((l) => amounts[l.code_pl] != null).length, 0)
);

const onInput = (codePL: number, v: number | null) => {
  amounts[codePL] = v;
  dirty.value = true;
};

const seed = () => {
  for (const k of Object.keys(amounts)) delete amounts[Number(k)];
  for (const k of Object.keys(origin)) delete origin[Number(k)];
  for (const v of values.value) {
    amounts[v.code_pl] = v.amount;
    origin[v.code_pl] = v.amount;
  }
  dirty.value = false;
};

const errText = (e: unknown, fallback: string): string => {
  if (typeof e === "object" && e && "data" in e) {
    const d = (e as { data?: { error?: string } }).data;
    if (d?.error) return d.error;
  }
  return e instanceof Error ? e.message : fallback;
};

const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    // Карточка нужна ради периода/валюты и признака «закрыт на запись»:
    // блокировать ввод обязан клиент, не только сервер (иначе человек вводит
    // суммы, а сохранение отбивается на последнем шаге).
    const [state, cc] = await Promise.all([api.card(cardId), api.commonCosts(cardId)]);
    card.value = state.card;
    groups.value = cc.groups || [];
    values.value = cc.values || [];
    seed();
    loaded.value = true;
  } catch (e: unknown) {
    error.value = errText(e, "Не удалось загрузить общие затраты");
  } finally {
    loading.value = false;
  }
};

const save = async () => {
  busy.value = true;
  error.value = "";
  note.value = "";
  const payload: Partial<MpCommonCostValue>[] = [];
  for (const g of groups.value) {
    for (const l of g.lines) {
      const v = amounts[l.code_pl];
      if (v != null && !Number.isNaN(v)) {
        payload.push({ code_pl: l.code_pl, amount: v });
      } else if (origin[l.code_pl] != null) {
        // Статью очистили — пишем ноль, иначе прежняя сумма осталась бы в периоде.
        payload.push({ code_pl: l.code_pl, amount: 0 });
      }
    }
  }
  try {
    const res = await api.saveCommonCosts(cardId, payload);
    groups.value = res.groups || groups.value;
    values.value = res.values || [];
    seed();
    note.value = `Сохранено статей: ${payload.length}. Итого ${money(grandTotal.value)} ${currency.value}.`;
  } catch (e: unknown) {
    error.value = errText(e, "Не удалось сохранить общие затраты");
  } finally {
    busy.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.page-cc { display: flex; flex-direction: column; gap: var(--sp-5); }
.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-4); font-size: var(--fs-sm); margin: 0; }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.banner-pos { background: var(--pos-soft); color: var(--pos-strong); }
.banner-warn { background: var(--warn-soft); color: var(--warn); }
.banner-info { background: var(--info-soft); color: var(--info); }
.lock-note { margin-left: var(--sp-3); color: var(--neg); }
.page-actions { display: flex; gap: var(--sp-3); flex-wrap: wrap; }

.grp { overflow: hidden; }
.grp-head { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-4); }
.grp-sum { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: var(--fs-md); color: var(--text-strong); }
.th-code { width: 80px; }
.th-amt { width: 180px; }
.th-src { width: 200px; }
.cd { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.src { font-size: var(--fs-2xs); color: var(--text-muted); }
.strong { font-weight: var(--fw-semibold); }
.amt-input {
  width: 150px;
  text-align: right;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  padding: 3px 7px;
  background: var(--bg-surface);
  color: var(--text-primary);
}
.amt-input:focus { border-color: var(--accent); outline: none; }
.amt-input:disabled { background: var(--bg-surface-3); color: var(--text-muted); }

.total-card { padding: var(--sp-5); display: flex; flex-direction: column; gap: var(--sp-2); }
.tc-row { display: flex; align-items: baseline; justify-content: space-between; gap: var(--sp-4); }
.tc-lbl { font-size: var(--fs-sm); color: var(--text-secondary); }
.tc-val { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: var(--fs-xl); color: var(--accent); }
.tc-hint { font-size: var(--fs-2xs); color: var(--text-muted); margin: 0; }
.dirty-dot { color: var(--warn); margin-left: 4px; font-size: 10px; vertical-align: middle; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
