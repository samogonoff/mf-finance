<!--
  Карточка одной площадки (ТЗ МП §3.2; требование встречи 10.07.2026 — «провалиться
  в площадку, заполнить её и вернуться к агрегату»).

  Зачем отдельный экран поверх сетки: в агрегате площадка — это узкая колонка, в
  которой не видно ни условий, из которых считалась расходная часть, ни сравнения
  со сценариями. Здесь тот же набор показателей развёрнут вертикально: сценарные
  слои рядом со значением, условия площадки и результат каскада.

  Данные берутся из УЖЕ ЗАГРУЖЕННОЙ формы (cells/conditions/каскад) — карточка не
  делает своих запросов, чтобы «провалиться и вернуться» не стоило секунд ожидания.
-->
<template>
  <PlansModal
    :open="open"
    :title="platform ? platform.name || `ЦФО ${platform.code_cfo}` : 'Площадка'"
    :subtitle="subtitle"
    width="860px"
    @close="emit('close')"
  >
    <div v-if="platform" class="pc">
      <!-- ===== Условия площадки: то, из чего считается расходная часть ===== -->
      <section class="pc-block">
        <div class="pc-bhead">
          <span class="pc-btitle">Условия площадки</span>
          <NuxtLink v-if="cardId" :to="`/plans/mp-conditions/${cardId}`" class="pc-link">
            <Icon name="lucide:sliders-horizontal" /> Реестр условий
          </NuxtLink>
        </div>
        <div v-if="conditions" class="pc-cond">
          <div class="cnd"><span class="cnd-l">% СПП</span><span class="cnd-v">{{ pctOf(conditions.spp_pct) }}</span></div>
          <div class="cnd"><span class="cnd-l">Наценка</span><span class="cnd-v">{{ pctOf(conditions.markup_pct) }}</span></div>
          <div class="cnd"><span class="cnd-l">Наценка от общей сс</span><span class="cnd-v">{{ pctOf(conditions.markup_total_pct) }}</span></div>
          <div class="cnd">
            <span class="cnd-l">НДС площадки</span>
            <span class="cnd-v">{{ conditions.vat_rate != null ? pctOf(conditions.vat_rate) : pctOf(vatFallback) }}</span>
          </div>
        </div>
        <p v-else class="pc-note">
          Условия по этой площадке не заданы — расходная часть считается только из введённых сумм.
        </p>

        <table v-if="shareRows.length" class="data-table compact pc-shares">
          <thead><tr><th>Статья прямых затрат</th><th class="col-num">Доля</th><th class="col-num">Сумма по доле</th></tr></thead>
          <tbody>
            <tr v-for="s in shareRows" :key="s.block">
              <td>{{ s.name }}</td>
              <td class="col-num">{{ pctOf(s.share) }}</td>
              <td class="col-num">{{ money(s.amount) }}</td>
            </tr>
          </tbody>
        </table>
      </section>

      <!-- ===== Показатели площадки вертикально ===== -->
      <section class="pc-block">
        <div class="pc-bhead">
          <span class="pc-btitle">Показатели площадки</span>
          <span class="pc-hint">Ввод сохраняется в общей форме — карточка правит те же значения.</span>
        </div>
        <div class="pc-scroll">
          <table class="data-table compact">
            <thead>
              <tr>
                <th>Показатель</th>
                <th class="col-num">Пр. год</th>
                <th class="col-num">Факт</th>
                <th class="col-num">Стратегия</th>
                <th class="col-num">Таргет</th>
                <th class="col-num">Тактика</th>
              </tr>
            </thead>
            <tbody>
              <template v-for="sec in sections" :key="sec.name">
                <tr class="pc-sec"><td colspan="6">{{ sec.name }}</td></tr>
                <tr v-for="l in sec.lines" :key="l.block_type">
                  <td>
                    <span class="pc-lname">{{ l.name }}</span>
                    <span v-if="l.code_pl" class="pc-pl">PL {{ l.code_pl }}</span>
                  </td>
                  <td class="col-num dim">{{ fmt(l, cellOf(l.block_type)?.fact_prev) }}</td>
                  <td class="col-num dim">{{ fmt(l, cellOf(l.block_type)?.fact) }}</td>
                  <td class="col-num dim">{{ fmt(l, cellOf(l.block_type)?.strategy) }}</td>
                  <td class="col-num dim">{{ fmt(l, cellOf(l.block_type)?.target) }}</td>
                  <td class="col-num">
                    <template v-if="editable && l.editable">
                      <NumberField
                        :model-value="viewInput(l)"
                        class="pc-input"
                        :disabled="!editable"
                        :placeholder="l.value_kind === 'pct' ? '%' : '—'"
                        @update:model-value="onInput(l, $event)"
                      />
                      <span v-if="inverse && l.cost_line" class="pc-sub">
                        по доле: {{ money(values[l.block_type]) }}
                      </span>
                    </template>
                    <span v-else class="pc-calc" :class="{ neg: (values[l.block_type] ?? 0) < 0 }">
                      {{ fmt(l, values[l.block_type]) }}
                    </span>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
        <!-- Слоя «предыдущий месяц» в /mp-form сервер пока не отдаёт (в ячейке есть
             факт периода и факт того же месяца прошлого года) — колонку не рисуем,
             чтобы не выдавать один слой за другой. -->
        <p class="pc-note">
          «Пр. год» — факт того же месяца прошлого года; отдельного слоя «предыдущий месяц»
          источник формы пока не отдаёт.
        </p>
      </section>

      <!-- ===== Результат каскада ===== -->
      <section class="pc-block">
        <div class="pc-bhead"><span class="pc-btitle">Результат по площадке</span></div>
        <div class="pc-res">
          <div class="res"><span class="res-l">Маржа розничная</span><span class="res-v">{{ money(values[B.retailMargin]) }}</span></div>
          <div class="res"><span class="res-l">Маржа gross</span><span class="res-v">{{ money(values[B.grossMargin]) }}</span></div>
          <div class="res"><span class="res-l">Комиссия площадки</span><span class="res-v">{{ money(values[B.commission]) }}</span></div>
          <div class="res"><span class="res-l">Прямые затраты</span><span class="res-v">{{ money(values[B.platformCosts]) }}</span></div>
          <div class="res key"><span class="res-l">PL площадки</span><span class="res-v">{{ money(values[B.plPlatform]) }}</span></div>
          <div class="res"><span class="res-l">PL, %</span><span class="res-v">{{ pctOf(values[B.plPlatformPct]) }}</span></div>
          <div class="res"><span class="res-l">Доля ПЗ в обороте</span><span class="res-v">{{ pctOf(values[B.directShareTurnover]) }}</span></div>
        </div>
      </section>
    </div>

    <template #footer>
      <NuxtLink v-if="cardId" :to="`/plans/mp-common-costs/${cardId}`" class="btn btn-ghost">
        <Icon name="lucide:layers" /> Общие затраты
      </NuxtLink>
      <button class="btn btn-primary" @click="emit('close')">
        <Icon name="lucide:arrow-left" /> Вернуться к агрегату
      </button>
    </template>
  </PlansModal>
</template>

<script setup lang="ts">
import PlansModal from "~/components/plans/PlansModal.vue";
import NumberField from "~/components/NumberField.vue";
import { B, COST_BLOCKS, type MpConditions } from "~/composables/useMpCascade";
import type { MpFormPlatform, MpLine, MpFormCell } from "~/composables/useTasks";
import { num } from "~/utils/format";

const props = withDefaults(
  defineProps<{
    open: boolean;
    platform: MpFormPlatform | null;
    lines: MpLine[];
    /** Ячейки ВСЕЙ формы — фильтруем по площадке сами, чтобы не копировать массивы. */
    cells: MpFormCell[];
    /** Результат каскада по этой площадке (считает форма). */
    values: Record<string, number>;
    /** Ввод по этой площадке: block_type → значение модели (доли для процентов). */
    inputs: Record<string, number | null>;
    conditions?: MpConditions | null;
    inverse?: boolean;
    editable?: boolean;
    currency?: string;
    vatFallback?: number;
    cardId?: number;
  }>(),
  { conditions: null, inverse: false, editable: false, currency: "RUB", vatFallback: 0.2, cardId: 0 }
);
const emit = defineEmits<{ (e: "close"): void; (e: "input", block: string, v: number | null): void }>();

// Короткие имена статей берём из спеки формы (line.name) — второго словаря не держим.
const nameOf = (block: string) => props.lines.find((l) => l.block_type === block)?.name || block;

const subtitle = computed(() => {
  const p = props.platform;
  if (!p) return "";
  const seg = p.segment === "small" ? "мелкие МП" : "крупные МП";
  return `ЦФО ${p.code_cfo} · ${seg}${p.legal_entity ? " · " + p.legal_entity : ""}${p.country ? " · " + p.country : ""} · ${props.currency}`;
});

// Строки площадки: заголовки и тотал-строки в карточке не нужны — тотал считается
// по всем площадкам, здесь одна.
const sections = computed(() => {
  const out: { name: string; lines: MpLine[] }[] = [];
  for (const l of props.lines) {
    if (l.kind === "header" || l.scope === "total") continue;
    let s = out.find((x) => x.name === l.section);
    if (!s) { s = { name: l.section, lines: [] }; out.push(s); }
    s.lines.push(l);
  }
  return out;
});

const cellOf = (block: string): MpFormCell | undefined =>
  props.cells.find((c) => c.code_cfo === props.platform?.code_cfo && c.block_type === block);

const shareRows = computed(() => {
  const sh = props.conditions?.shares || {};
  const mgrNet = props.values[B.salesManagerNet] ?? 0;
  return COST_BLOCKS.filter((b) => sh[b] !== undefined && sh[b] !== 0).map((b) => ({
    block: b,
    name: nameOf(b),
    share: sh[b],
    amount: mgrNet * sh[b]
  }));
});

const money = (v: number | null | undefined) => (v == null || Number.isNaN(v) ? "—" : num(v, 2));
const pctOf = (v: number | null | undefined) => (v == null || Number.isNaN(v) ? "—" : num(v * 100, 2) + " %");
const fmt = (l: MpLine, v: number | null | undefined) => (l.value_kind === "pct" ? pctOf(v) : money(v));

// Проценты вводятся в процентах (31), в модели живут долями (0.31) — как в реестре
// условий и в сетке формы.
const viewInput = (l: MpLine): number | null => {
  const v = props.inputs[l.block_type];
  if (v == null || Number.isNaN(v)) return null;
  return l.value_kind === "pct" ? Number((v * 100).toFixed(4)) : v;
};
const onInput = (l: MpLine, v: number | null) => {
  emit("input", l.block_type, v == null ? null : l.value_kind === "pct" ? Number((v / 100).toFixed(6)) : v);
};
</script>

<style scoped>
.pc { display: flex; flex-direction: column; gap: var(--sp-5); }
.pc-block { display: flex; flex-direction: column; gap: var(--sp-3); }
.pc-bhead { display: flex; align-items: baseline; justify-content: space-between; gap: var(--sp-4); flex-wrap: wrap; }
.pc-btitle { font-size: var(--fs-sm); font-weight: var(--fw-semibold); color: var(--text-strong); text-transform: uppercase; letter-spacing: 0.04em; }
.pc-hint, .pc-note { font-size: var(--fs-2xs); color: var(--text-muted); margin: 0; }
.pc-link { font-size: var(--fs-2xs); color: var(--accent); display: inline-flex; align-items: center; gap: 3px; }

.pc-cond { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: var(--sp-3); }
.cnd { display: flex; flex-direction: column; gap: 1px; background: var(--bg-surface-2); border-radius: var(--rd-3); padding: var(--sp-2) var(--sp-3); }
.cnd-l { font-size: var(--fs-2xs); color: var(--text-secondary); }
.cnd-v { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: var(--fs-md); }
.pc-shares { font-size: var(--fs-sm); }

.pc-scroll { max-height: 46vh; overflow: auto; }
.pc-sec td {
  background: var(--bg-tonal);
  font-size: var(--fs-2xs);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-secondary);
  font-weight: var(--fw-bold);
}
.pc-lname { font-size: var(--fs-sm); }
.pc-pl { margin-left: 6px; font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.dim { color: var(--text-muted); }
.pc-calc { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.pc-calc.neg { color: var(--neg-strong); }
.pc-input {
  width: 128px;
  text-align: right;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  padding: 3px 7px;
  background: var(--bg-surface);
  color: var(--text-primary);
}
.pc-input:focus { border-color: var(--accent); outline: none; }
.pc-sub { display: block; font-size: var(--fs-2xs); color: var(--text-muted); font-family: var(--font-mono); }

.pc-res { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: var(--sp-3); }
.res { display: flex; flex-direction: column; gap: 1px; }
.res-l { font-size: var(--fs-2xs); color: var(--text-secondary); }
.res-v { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: var(--fs-md); }
.res.key .res-v { color: var(--accent); font-weight: var(--fw-semibold); }
</style>
