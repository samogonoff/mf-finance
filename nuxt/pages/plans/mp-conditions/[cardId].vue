<!--
  Реестр «Условия площадки» (ТЗ МП §6.1) — новый ключевой экран скорректированного
  ТЗ. Здесь вводятся %СПП, две наценки и удельные веса статей прямых затрат; из
  них считается вся расходная часть формы (инверсия расчёта, §3.1).

  Экран отвечает на три требования ТЗ разом: копирование условий из предыдущего
  периода одним действием, видимость «какие доли изменились и на сколько» для
  согласующего и обязательное обоснование при отклонении сверх порога.
-->
<template>
  <div class="page-mpc">
    <header class="page-header">
      <div>
        <h1 class="page-title">Условия площадок · {{ periodLabel }}</h1>
        <p class="page-subtitle">
          {{ card?.title || "Маркетплейсы" }} ·
          <span :class="modeClass">{{ modeLabel }}</span> ·
          {{ view?.conditions.length || 0 }} площадок
          <span v-if="card?.locked" class="lock-note"><Icon name="lucide:lock" /> период закрыт на запись</span>
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink v-if="card" :to="`/plans/${card.pl_id}`" class="btn btn-ghost">
          <Icon name="lucide:arrow-left" /> К периоду
        </NuxtLink>
        <button class="btn btn-sm btn-ghost" :disabled="!editable || busy" title="Заполнить условия значениями предыдущего периода" @click="doCopy">
          <Icon name="lucide:copy" /> Копировать из прошлого месяца
        </button>
        <button class="btn btn-sm btn-ghost" :disabled="busy" @click="doPreview">
          <Icon name="lucide:calculator" /> Предпросмотр пересчёта
        </button>
        <button class="btn btn-sm btn-primary" :disabled="!editable || busy" @click="doRecalc">
          <Icon name="lucide:refresh-cw" :class="{ spin: busy }" /> Пересчитать расходную часть
        </button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>
    <p v-if="view && !editable" class="banner banner-warn">
      Только просмотр: период закрыт или вы не исполнитель. Изменение утверждённых условий — через переоткрытие периода с причиной.
    </p>

    <SkeletonTable v-if="loading" :rows="6" :cols="8" />

    <!-- ===== Что изменилось к прошлому периоду ===== -->
    <section v-if="view?.diff?.length" class="card diff-card">
      <div class="card-header">
        <span class="card-title">Изменения к прошлому периоду ({{ view.diff.length }})</span>
        <span v-if="needWhy.length" class="badge badge-warn">
          {{ needWhy.length }} сверх порога — нужно обоснование
        </span>
      </div>
      <table class="data-table">
        <thead>
          <tr>
            <th>Площадка</th><th>Показатель</th>
            <th class="col-num">Было</th><th class="col-num">Стало</th><th class="col-num">Δ</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(d, i) in view.diff" :key="i" :class="{ 'row-why': d.needs_why }">
            <td>{{ d.name_cfo || d.code_cfo }}</td>
            <td>{{ d.field_name }}</td>
            <td class="col-num">{{ pct(d.was) }}</td>
            <td class="col-num strong">{{ pct(d.now) }}</td>
            <td class="col-num" :class="d.delta > 0 ? 'up' : 'down'">
              {{ d.delta > 0 ? "+" : "" }}{{ pct(d.delta) }}
              <Icon v-if="d.needs_why" name="lucide:alert-triangle" title="сверх порога: требуется обоснование" />
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- ===== Реестр условий ===== -->
    <section v-if="view" class="card">
      <div class="card-header">
        <span class="card-title">Условия по площадкам</span>
        <span class="hdr-hint">
          Доли — от выручки по ценам менеджера без НДС. Отрицательная доля допустима только для компенсаций (статья 66).
        </span>
      </div>
      <div class="table-scroll">
        <table class="data-table cond-table">
          <thead>
            <tr>
              <th class="sticky-col">Площадка</th>
              <th class="col-num">% СПП</th>
              <th class="col-num">Наценка</th>
              <th class="col-num">Наценка от общей сс</th>
              <th class="col-num">НДС</th>
              <th v-for="b in costBlocks" :key="b.block" class="col-num" :title="b.name">
                {{ b.short }}
              </th>
              <th class="col-num">Σ долей</th>
              <th>Обоснование</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rows" :key="row.code_cfo" :class="{ 'row-dirty': isDirty(row) }">
              <td class="sticky-col">
                <span class="pl-name">{{ row.name_cfo || row.code_cfo }}</span>
                <span class="pl-meta">ЦФО {{ row.code_cfo }} · {{ row.segment === "large" ? "крупные" : "мелкие" }} · {{ row.currency || "—" }}</span>
              </td>
              <td class="col-num">
                <NumberField
                  :model-value="row.spp_pct_view"
                  class="cell-input pct" :disabled="!editable" placeholder="%"
                  @update:model-value="row.spp_pct_view = $event ?? 0"
                />
                <span v-if="hint(row, 'spp_pct') !== null" class="cell-hint">факт: {{ pct(hint(row, "spp_pct")!) }}</span>
              </td>
              <td class="col-num">
                <NumberField
                  :model-value="row.markup_pct_view"
                  class="cell-input pct" :disabled="!editable" placeholder="%"
                  @update:model-value="row.markup_pct_view = $event ?? 0"
                />
                <span v-if="hint(row, 'markup_pct') !== null" class="cell-hint">факт: {{ pct(hint(row, "markup_pct")!) }}</span>
              </td>
              <td class="col-num">
                <NumberField
                  :model-value="row.markup_total_pct_view"
                  class="cell-input pct" :disabled="!editable" placeholder="%"
                  @update:model-value="row.markup_total_pct_view = $event ?? 0"
                />
              </td>
              <td class="col-num">
                <span class="vat-cell" :title="'эффективная ставка площадки из справочника dir_vat'">
                  {{ row.vat_rate != null ? pct(row.vat_rate) : "справочник" }}
                </span>
              </td>
              <td v-for="b in costBlocks" :key="b.block" class="col-num">
                <NumberField
                  :model-value="row.shares_view[b.block] ?? 0"
                  class="cell-input pct" :disabled="!editable" placeholder="%"
                  @update:model-value="row.shares_view[b.block] = $event ?? 0"
                />
                <span v-if="hint(row, 'share:' + b.block) !== null" class="cell-hint">
                  факт: {{ pct(hint(row, "share:" + b.block)!) }}
                </span>
              </td>
              <td class="col-num strong" :class="{ 'over-limit': sharesSum(row) + toNum(row.spp_pct_view) / 100 > 1 }">
                {{ pct(sharesSum(row)) }}
              </td>
              <td>
                <input
                  v-model="row.change_reason"
                  class="select reason-input"
                  :disabled="!editable"
                  :placeholder="needsReason(row) ? 'обязательно: причина изменения' : 'необязательно'"
                  :class="{ 'need-why': needsReason(row) && !row.change_reason.trim() }"
                />
              </td>
              <td>
                <button class="btn btn-sm btn-primary" :disabled="!editable || busy || !isDirty(row)" @click="saveRow(row)">
                  <Icon name="lucide:save" /> v{{ row.version }}
                </button>
              </td>
            </tr>
            <tr v-if="!rows.length">
              <td :colspan="costBlocks.length + 8" class="empty-cell">
                Условия не заданы. Скопируйте их из прошлого месяца или заполните вручную —
                без условий расходная часть не считается (МП-01).
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- ===== Результат пересчёта / валидации ===== -->
    <section v-if="recalcResult" class="card">
      <div class="card-header">
        <span class="card-title">
          {{ recalcResult.preview ? "Предпросмотр пересчёта (не сохранено)" : "Расходная часть пересчитана" }}
        </span>
        <span v-if="recalcResult.blocking" class="badge badge-neg">есть блокирующие замечания</span>
      </div>
      <div class="totals-grid">
        <div class="tot"><span class="tot-lbl">Продажи менеджера без НДС</span><span class="tot-val">{{ money(recalcResult.totals.sales_manager_net) }}</span></div>
        <div class="tot"><span class="tot-lbl">Цена площадки без НДС</span><span class="tot-val">{{ money(recalcResult.totals.sales_plat_net) }}</span></div>
        <div class="tot"><span class="tot-lbl">Комиссия площадок</span><span class="tot-val">{{ money(recalcResult.totals.commission) }}</span></div>
        <div class="tot"><span class="tot-lbl">Прямые затраты</span><span class="tot-val">{{ money(recalcResult.totals.direct_costs) }}</span></div>
        <div class="tot"><span class="tot-lbl">Общие затраты</span><span class="tot-val">{{ money(recalcResult.common_cost) }}</span></div>
        <div class="tot tot-key"><span class="tot-lbl">PL (сумма)</span><span class="tot-val">{{ money(recalcResult.totals.pl) }}</span></div>
        <div class="tot"><span class="tot-lbl">PL, %</span><span class="tot-val">{{ pct(recalcResult.totals.pl_pct) }}</span></div>
        <div class="tot"><span class="tot-lbl">Доля ПЗ в обороте</span><span class="tot-val">{{ pct(recalcResult.totals.direct_share_turnover) }}</span></div>
      </div>

      <table v-if="recalcResult.issues?.length" class="data-table">
        <thead><tr><th>Код</th><th>Площадка</th><th>Замечание</th></tr></thead>
        <tbody>
          <tr v-for="(i, idx) in recalcResult.issues" :key="idx" :class="i.level === 'blocking' ? 'row-block' : 'row-warn'">
            <td><span class="badge" :class="i.level === 'blocking' ? 'badge-neg' : 'badge-warn'">{{ i.code }}</span></td>
            <td>{{ i.name_cfo || "—" }}</td>
            <td>
              {{ i.message }}
              <span v-if="i.needs_why" class="need-note">требуется комментарий</span>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="banner banner-pos">Замечаний нет — форму можно отправлять на согласование.</p>
    </section>
  </div>
</template>

<script setup lang="ts">
import NumberField from "~/components/NumberField.vue";
import SkeletonTable from "~/components/SkeletonTable.vue";
import { useMpConditions, type MpConditionsRow, type MpConditionsView, type MpRecalcResult, type PlanCard } from "~/composables/useMpConditions";
import { COST_BLOCKS } from "~/composables/useMpCascade";
import { money as fmtMoney } from "~/utils/format";

definePageMeta({ middleware: ["scope-guard"] });

const route = useRoute();
const cardId = Number(route.params.cardId);
const api = useMpConditions();

const view = ref<MpConditionsView | null>(null);
const card = ref<PlanCard | null>(null);
const rows = ref<EditableRow[]>([]);
const recalcResult = ref<MpRecalcResult | null>(null);
const loading = ref(true);
const busy = ref(false);
const error = ref("");
const note = ref("");

// Проценты в интерфейсе вводятся В ПРОЦЕНТАХ (31), в модели живут долями (0.31).
interface EditableRow extends MpConditionsRow {
  spp_pct_view: number;
  markup_pct_view: number;
  markup_total_pct_view: number;
  shares_view: Record<string, number>;
  _origin: string;
}

// Короткие подписи статей: полное название — в title колонки.
const COST_LABELS: Record<string, [string, string]> = {
  cost_agent: ["Агент.", "Агентское (комиссионное) вознаграждение"],
  cost_freight: ["Груз.экс", "Грузоперевозки экспорт"],
  cost_log_transport: ["Трансп.", "Транспортная логистика"],
  cost_log_warehouse: ["Склад", "Складская логистика"],
  cost_ads: ["Реклама", "РЕКЛАМА И МАРКЕТИНГ"],
  cost_ads_social: ["Соцсети", "Реклама — социальные сети"],
  cost_packaging: ["Упак.", "Расходы на упаковку (пакеты)"],
  cost_acquiring: ["Эквайр.", "Эквайринг"],
  cost_it: ["ИТ", "Расходы на IT обслуживание (ПО)"],
  penalties: ["Штрафы", "Штрафы (факт приходит из отдельного источника)"],
  cost_other: ["Прочие", "Прочие удержания и компенсации"]
};

const costBlocks = COST_BLOCKS.map((b) => ({
  block: b,
  short: COST_LABELS[b]?.[0] || b,
  name: COST_LABELS[b]?.[1] || b
}));

const editable = computed(() => !!view.value?.editable);
const periodLabel = computed(() =>
  view.value ? `${view.value.period.year}-${String(view.value.period.month).padStart(2, "0")}` : ""
);
const modeLabel = computed(() =>
  card.value?.calc_mode === "inverse"
    ? "расчёт от условий (инверсия)"
    : "расчёт от сумм (прежний режим)"
);
const modeClass = computed(() => (card.value?.calc_mode === "inverse" ? "mode-inv" : "mode-legacy"));
const needWhy = computed(() => (view.value?.diff || []).filter((d) => d.needs_why));

const toNum = (v: unknown): number => (typeof v === "number" && !Number.isNaN(v) ? v : 0);
const pct = (v: number): string => `${(toNum(v) * 100).toFixed(2)} %`;
const money = (v: number): string => fmtMoney(toNum(v), { currency: "" });

const toRow = (r: MpConditionsRow): EditableRow => {
  const shares: Record<string, number> = {};
  for (const b of COST_BLOCKS) shares[b] = Number((((r.shares || {})[b] || 0) * 100).toFixed(4));
  const row: EditableRow = {
    ...r,
    change_reason: r.change_reason || "",
    spp_pct_view: Number(((r.spp_pct || 0) * 100).toFixed(4)),
    markup_pct_view: Number(((r.markup_pct || 0) * 100).toFixed(4)),
    markup_total_pct_view: Number(((r.markup_total_pct || 0) * 100).toFixed(4)),
    shares_view: shares,
    _origin: ""
  };
  row._origin = signature(row);
  return row;
};

const signature = (r: EditableRow): string =>
  JSON.stringify([r.spp_pct_view, r.markup_pct_view, r.markup_total_pct_view, r.shares_view, r.change_reason]);

const isDirty = (r: EditableRow): boolean => signature(r) !== r._origin;
const sharesSum = (r: EditableRow): number =>
  COST_BLOCKS.reduce((acc, b) => acc + toNum(r.shares_view[b]) / 100, 0);

// Подсказка «в прошлом месяце фактическая доля была такой» (ТЗ §3.1): показываем,
// но в реестр не переносим — иначе производные значения станут условиями.
const hint = (r: EditableRow, field: string): number | null => {
  const h = view.value?.hints?.[String(r.code_cfo)];
  if (!h || h[field] === undefined) return null;
  return h[field];
};

// Обоснование обязательно, если по этой площадке diff помечен needs_why.
const needsReason = (r: EditableRow): boolean =>
  needWhy.value.some((d) => d.code_cfo === r.code_cfo);

const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    const v = await api.conditions(cardId);
    view.value = v;
    card.value = v.card || null;
    rows.value = (v.conditions || []).map(toRow);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Не удалось загрузить условия";
  } finally {
    loading.value = false;
  }
};

const saveRow = async (r: EditableRow) => {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    const shares: Record<string, number> = {};
    for (const b of COST_BLOCKS) {
      const v = toNum(r.shares_view[b]);
      if (v !== 0) shares[b] = v / 100;
    }
    const saved = await api.saveConditions(cardId, {
      code_cfo: r.code_cfo,
      spp_pct: toNum(r.spp_pct_view) / 100,
      markup_pct: toNum(r.markup_pct_view) / 100,
      markup_total_pct: toNum(r.markup_total_pct_view) / 100,
      currency: r.currency,
      change_reason: r.change_reason,
      shares
    });
    note.value = `Условия «${r.name_cfo || r.code_cfo}» сохранены (версия ${saved.version}).`;
    await load();
  } catch (e: unknown) {
    error.value = errText(e, "Не удалось сохранить условия");
  } finally {
    busy.value = false;
  }
};

const doCopy = async () => {
  busy.value = true;
  error.value = "";
  try {
    const res = await api.copyConditions(cardId);
    note.value = res.copied
      ? `Скопировано условий: ${res.copied}. Проверьте доли и при изменениях укажите обоснование.`
      : "В прошлом месяце условий нет — заполните вручную.";
    await load();
  } catch (e: unknown) {
    error.value = errText(e, "Не удалось скопировать условия");
  } finally {
    busy.value = false;
  }
};

const doPreview = () => runRecalc(true);
const doRecalc = () => runRecalc(false);

const runRecalc = async (preview: boolean) => {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    recalcResult.value = await api.recalc(cardId, preview);
    if (!preview) note.value = "Расходная часть пересчитана и сохранена в тактике.";
  } catch (e: unknown) {
    error.value = errText(e, "Пересчёт не выполнен");
  } finally {
    busy.value = false;
  }
};

const errText = (e: unknown, fallback: string): string => {
  if (typeof e === "object" && e && "data" in e) {
    const d = (e as { data?: { error?: string } }).data;
    if (d?.error) return d.error;
  }
  return e instanceof Error ? e.message : fallback;
};

onMounted(load);
</script>

<style scoped>
.page-mpc {
  display: flex;
  flex-direction: column;
  gap: var(--sp-5);
}
.lock-note {
  margin-left: var(--sp-3);
  color: var(--neg);
}
.mode-inv {
  color: var(--accent);
  font-weight: 600;
}
.mode-legacy {
  color: var(--text-muted);
}
.hdr-hint {
  color: var(--text-muted);
  font-size: var(--fs-sm);
}
.table-scroll {
  overflow-x: auto;
}
.cond-table {
  min-width: 100%;
}
.sticky-col {
  position: sticky;
  left: 0;
  background: var(--bg-surface);
  z-index: 1;
  min-width: 200px;
}
.pl-name {
  display: block;
  font-weight: 600;
}
.pl-meta {
  display: block;
  font-size: var(--fs-xs);
  color: var(--text-muted);
}
.cell-hint {
  display: block;
  font-size: var(--fs-xs);
  color: var(--text-muted);
}
.vat-cell {
  color: var(--text-secondary);
}
.reason-input {
  min-width: 220px;
}
.cell-input {
  width: 92px;
  padding: var(--sp-2);
  text-align: right;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  background: var(--bg-surface);
  color: var(--text-primary);
}
.cell-input.pct {
  width: 78px;
}
.cell-input:disabled {
  background: var(--bg-surface-3);
  color: var(--text-muted);
}
.need-why {
  border-color: var(--neg);
}
.over-limit {
  color: var(--neg);
}
.row-dirty {
  background: var(--accent-soft);
}
.row-why td {
  background: var(--warn-soft);
}
.row-block td {
  background: var(--neg-soft);
}
.row-warn td {
  background: var(--warn-soft);
}
.up {
  color: var(--pos);
}
.down {
  color: var(--neg);
}
.need-note {
  margin-left: var(--sp-2);
  font-size: var(--fs-xs);
  color: var(--neg);
}
.totals-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: var(--sp-4);
  padding: var(--sp-4);
}
.tot {
  display: flex;
  flex-direction: column;
  gap: var(--sp-1);
}
.tot-lbl {
  font-size: var(--fs-sm);
  color: var(--text-secondary);
}
.tot-val {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--fs-lg);
}
.tot-key .tot-val {
  color: var(--accent);
  font-weight: 600;
}
.diff-card .data-table {
  font-size: var(--fs-sm);
}
.spin {
  animation: spin 1s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
