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

    <!-- Загрузка долей из раздела «Стратегические планы» и сохранение всех правок разом. -->
    <section v-if="view" class="card toolbar-card">
      <div class="toolbar">
        <div class="toolbar-group">
          <span class="toolbar-label">Доли из стратегии</span>
          <select v-model.number="strategyYear" class="select" :disabled="!editable || busy" aria-label="Год стратегии">
            <option v-for="y in strategyYears" :key="y" :value="y">{{ y }}</option>
          </select>
          <select v-model.number="strategyMonth" class="select" :disabled="!editable || busy" aria-label="Месяц стратегии">
            <option v-for="m in 12" :key="m" :value="m">{{ monthName(m) }}</option>
          </select>
          <button
            class="btn btn-sm btn-ghost"
            :disabled="!editable || busy"
            title="Подставить в сетку доли статей из стратегии за выбранный месяц. Значения можно править, в реестр они попадут после сохранения."
            @click="doLoadStrategy"
          >
            <Icon name="lucide:download" /> Загрузить из стратегии
          </button>
        </div>
        <div class="toolbar-group toolbar-save">
          <span v-if="dirtyRows.length" class="dirty-note">Изменено строк: {{ dirtyRows.length }}</span>
          <button class="btn btn-sm btn-primary" :disabled="!editable || busy || !dirtyRows.length" @click="doSaveAll">
            <Icon name="lucide:save" /> Сохранить условия
          </button>
        </div>
      </div>
    </section>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>
    <p v-if="view && !editable" class="banner banner-warn">
      Только просмотр: период закрыт или вы не исполнитель. Изменение утверждённых условий — через переоткрытие периода с причиной.
    </p>

    <SkeletonTable v-if="loading" :rows="6" :cols="8" />

    <!-- ===== Что изменилось к прошлому периоду ===== -->
    <section v-if="view?.diff?.length" class="card diff-card">
      <div class="card-header diff-header">
        <button
          type="button"
          class="diff-toggle"
          :aria-expanded="diffOpen"
          :title="diffOpen ? 'Свернуть изменения' : 'Развернуть изменения'"
          @click="diffOpen = !diffOpen"
        >
          <Icon :name="diffOpen ? 'lucide:chevron-down' : 'lucide:chevron-right'" />
          <span class="card-title">Изменения к прошлому периоду ({{ view.diff.length }})</span>
        </button>
        <span v-if="needWhy.length" class="badge badge-warn">
          {{ needWhy.length }} сверх порога — нужно обоснование
        </span>
      </div>
      <!-- Много правок не должны занимать весь экран: внутри блока своя прокрутка. -->
      <div v-show="diffOpen" class="diff-scroll">
        <table class="data-table">
          <thead>
            <tr>
              <th>Площадка</th><th>Показатель</th>
              <th class="col-num">Было</th><th class="col-num">Стало</th><th class="col-num">Δ</th>
              <th>Кто изменил</th><th>Когда</th>
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
              <td>{{ d.changed_by || "—" }}</td>
              <td class="muted-cell">{{ when(d.changed_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- ===== Реестр условий ===== -->
    <section v-if="view" class="card">
      <div class="card-header">
        <span class="card-title">Условия по площадкам</span>
        <span class="hdr-hint">
          Доли — от выручки по цене площадки без НДС. Отрицательная доля допустима только для компенсаций (статья 66).
        </span>
      </div>
      <div class="table-scroll">
        <table class="data-table cond-table">
          <thead>
            <tr>
              <th class="sticky-col">Площадка</th>
			  <th>Действует с</th><th>Действует по</th>
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
                <span class="pl-name">
                  {{ row.name_cfo || row.code_cfo }}
                  <button
                    type="button"
                    class="fi-btn"
                    :class="{ on: openInfo.has(row.code_cfo) }"
                    :aria-expanded="openInfo.has(row.code_cfo)"
                    :title="openInfo.has(row.code_cfo) ? 'Скрыть' : 'Показать код ЦФО'"
                    @click="toggleInfo(row.code_cfo)"
                  >
                    <Icon name="lucide:info" class="fi" />
                  </button>
                </span>
                <span v-if="openInfo.has(row.code_cfo)" class="pl-info">Код ЦФО: {{ row.code_cfo }}</span>
                <span class="pl-meta">{{ row.segment === "large" ? "крупные" : "мелкие" }} · {{ row.currency || "—" }}</span>
              </td>
			  <td><input v-model="row.valid_from" type="date" class="select" :disabled="!editable" /></td>
			  <td><input v-model="row.valid_to" type="date" class="select" :disabled="!editable" /></td>
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
				  :model-value="row.shares_view[b.block] ?? null"
                  class="cell-input pct" :disabled="!editable"
                  :placeholder="defaultView(row, b.block) ?? '%'"
				  @update:model-value="row.shares_view[b.block] = $event"
                />
                <!-- Пустое поле = доля по умолчанию из стратегии (ТЗ доработок п. 2.1);
                     введённое значение — переопределение, только оно пишется в реестр. -->
                <span v-if="row.shares_view[b.block] == null && defaultView(row, b.block) != null" class="cell-hint default" :title="row.default_source">
                  стратегия
                </span>
                <span v-else-if="row.shares_view[b.block] != null && defaultView(row, b.block) != null" class="cell-hint manual" :title="row.default_source">
                  вручную · страт. {{ defaultView(row, b.block) }} %
                </span>
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
              <td class="ver-cell">
                <!-- Версия и автор — справочно; сохраняется экран целиком кнопкой «Сохранить условия». -->
                <span class="ver">v{{ row.version }}</span>
                <span v-if="isDirty(row)" class="ver-dirty">не сохранено</span>
                <span v-else-if="row.version > 0" class="ver-who">
                  {{ row.author_name || "—" }} · {{ when(row.updated_at) }}
                </span>
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
import { apiError } from "~/utils/apiError";
import NumberField from "~/components/NumberField.vue";
import SkeletonTable from "~/components/SkeletonTable.vue";
import { useMpConditions, type MpConditionsRow, type MpConditionsView, type MpRecalcResult, type PlanCard } from "~/composables/useMpConditions";
import { COST_BLOCKS } from "~/composables/useMpCascade";
import { useRetail } from "~/composables/useRetail";
import { money as fmtMoney } from "~/utils/format";

definePageMeta({ middleware: ["scope-guard"] });

const route = useRoute();
const cardId = Number(route.params.cardId);
const api = useMpConditions();
const retail = useRetail();

const view = ref<MpConditionsView | null>(null);
const card = ref<PlanCard | null>(null);
const rows = ref<EditableRow[]>([]);
const recalcResult = ref<MpRecalcResult | null>(null);
const loading = ref(true);
const busy = ref(false);
const error = ref("");
const note = ref("");

// Блок «Изменения к прошлому периоду»: при большом числе правок он занимал весь
// экран — по умолчанию свёрнут, если строк больше порога.
const DIFF_AUTO_COLLAPSE = 8;
const diffOpen = ref(true);
// Значок «i» у названия площадки раскрывает код ЦФО.
const openInfo = ref(new Set<number>());
const toggleInfo = (cfo: number) => {
  const next = new Set(openInfo.value);
  if (next.has(cfo)) next.delete(cfo);
  else next.add(cfo);
  openInfo.value = next;
};
// Месяц стратегии, из которого подгружаются доли (по умолчанию — месяц карточки).
const strategyYear = ref(new Date().getFullYear());
const strategyMonth = ref(new Date().getMonth() + 1);
const strategyYears = computed(() => {
  const y = card.value?.year ?? new Date().getFullYear();
  return [y - 1, y, y + 1];
});
const monthName = (m: number): string =>
  new Intl.DateTimeFormat("ru-RU", { month: "long" }).format(new Date(2000, m - 1, 1));
const when = (iso?: string): string => {
  if (!iso) return "—";
  const d = new Date(iso);
  // Нулевое время Go (0001-01-01) означает «не сохранялось».
  return Number.isNaN(d.getTime()) || d.getFullYear() < 2000
    ? "—"
    : d.toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit" });
};

// Проценты в интерфейсе вводятся В ПРОЦЕНТАХ (31), в модели живут долями (0.31).
interface EditableRow extends MpConditionsRow {
  spp_pct_view: number;
  markup_pct_view: number;
  markup_total_pct_view: number;
  shares_view: Record<string, number | null>;
  _origin: string;
}

// Короткие подписи статей: полное название — в title колонки.
const COST_LABELS: Record<string, [string, string]> = {
  cost_agent: ["Агент.", "Агентское (комиссионное) вознаграждение"],
  cost_freight: ["Груз.экс", "Грузоперевозки экспорт"],
  cost_log_transport: ["Развоз", "Грузоперевозки развоз и перемещение"],
  cost_log_warehouse: ["Аренда", "Аренда помещений (стоянки)"],
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
  const shares: Record<string, number | null> = {};
  for (const b of COST_BLOCKS) shares[b] = b in (r.shares || {}) ? Number((r.shares[b] * 100).toFixed(4)) : null;
  const row: EditableRow = {
    ...r,
	valid_from: r.period_year === view.value?.period.year && r.period_month === view.value?.period.month
	  ? (r.valid_from || `${view.value?.period.year}-${String(view.value?.period.month || 1).padStart(2, "0")}-01`)
	  : `${view.value?.period.year}-${String(view.value?.period.month || 1).padStart(2, "0")}-01`,
	valid_to: r.period_year === view.value?.period.year && r.period_month === view.value?.period.month ? (r.valid_to || "") : "",
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
  JSON.stringify([r.valid_from, r.valid_to, r.spp_pct_view, r.markup_pct_view, r.markup_total_pct_view, r.shares_view, r.change_reason]);

const isDirty = (r: EditableRow): boolean => signature(r) !== r._origin;
// Сумма считается по ЭФФЕКТИВНЫМ долям — тем, от которых считается форма:
// пустое поле берёт стратегию.
const sharesSum = (r: EditableRow): number =>
  COST_BLOCKS.reduce((acc, b) => acc + (r.shares_view[b] != null ? toNum(r.shares_view[b]) / 100 : r.default_shares?.[b] ?? 0), 0);

// Доля по умолчанию в процентах для подсказки в пустом поле.
const defaultView = (r: EditableRow, b: string): string | null => {
  const v = r.default_shares?.[b];
  return v == null ? null : (v * 100).toFixed(2);
};

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
    if (v.card) {
      strategyYear.value = v.card.year;
      strategyMonth.value = v.card.month;
    }
    diffOpen.value = (v.diff?.length || 0) <= DIFF_AUTO_COLLAPSE;
  } catch (e: unknown) {
    error.value = apiError(e, "Не удалось загрузить условия");
  } finally {
    loading.value = false;
  }
};

// Строка экрана → тело условий для API (проценты → доли). Один формат и для
// сохранения, и для черновика предпросмотра.
const rowPayload = (r: EditableRow) => {
  const shares: Record<string, number> = {};
  for (const b of COST_BLOCKS) {
    const v = r.shares_view[b];
    if (v != null && !Number.isNaN(v)) shares[b] = v / 100;
  }
  return {
    code_cfo: r.code_cfo,
    spp_pct: toNum(r.spp_pct_view) / 100,
    markup_pct: toNum(r.markup_pct_view) / 100,
    markup_total_pct: toNum(r.markup_total_pct_view) / 100,
    currency: r.currency,
    valid_from: r.valid_from,
    valid_to: r.valid_to,
    change_reason: r.change_reason,
    shares
  };
};

const dirtyRows = computed(() => rows.value.filter(isDirty));

// Сохраняет ВСЕ изменённые строки одним действием (по одной площадке на запрос —
// такова серверная модель версий). Сохранённые строки сразу помечаются чистыми;
// при ошибке на строке остановка БЕЗ перезагрузки: несохранённые правки других
// строк не пропадают. После полного успеха экран перечитывается (версии, автор).
const saveDirty = async (): Promise<{ saved: number; error: string }> => {
  let saved = 0;
  for (const r of [...dirtyRows.value]) {
    try {
      const res = await api.saveConditions(cardId, rowPayload(r));
      r.version = res.version;
      r._origin = signature(r);
      saved++;
    } catch (e: unknown) {
      return { saved, error: `«${r.name_cfo || r.code_cfo}»: ${apiError(e, "не удалось сохранить условия")}` };
    }
  }
  if (saved) await load();
  return { saved, error: "" };
};

const doSaveAll = async () => {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    const res = await saveDirty();
    if (res.error) {
      error.value = `${res.error}. Сохранено строк: ${res.saved}, остальные правки сохранены на экране, но не в реестре.`;
    } else {
      note.value = `Условия сохранены (строк: ${res.saved}).`;
    }
  } finally {
    busy.value = false;
  }
};

// Доли из раздела «Стратегические планы» за выбранный месяц. Подставляются в
// сетку как ручные значения (их можно править) и попадают в реестр только после
// «Сохранить условия». Пустая доля в стратегии оставляет поле пустым (= стратегия).
const doLoadStrategy = async () => {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    const data = await retail.strategyMpShares(strategyYear.value);
    const monthData = data.months.find((m) => m.month === strategyMonth.value);
    if (!monthData?.shares || !Object.keys(monthData.shares).length) {
      error.value = `В стратегии нет долей за ${monthName(strategyMonth.value)} ${strategyYear.value}.`;
      return;
    }
    let filled = 0;
    for (const r of rows.value) {
      const sh = monthData.shares[String(r.code_cfo)];
      if (!sh) continue;
      for (const b of COST_BLOCKS) {
        r.shares_view[b] = b in sh ? Number((sh[b] * 100).toFixed(4)) : null;
      }
      filled++;
    }
    note.value = filled
      ? `Доли из стратегии за ${monthName(strategyMonth.value)} ${strategyYear.value} подставлены для площадок: ${filled}. Проверьте и нажмите «Сохранить условия».`
      : "В стратегии нет данных по площадкам этой карточки.";
  } catch (e: unknown) {
    error.value = apiError(e, "Не удалось загрузить стратегию");
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
    error.value = apiError(e, "Не удалось скопировать условия");
  } finally {
    busy.value = false;
  }
};

const doPreview = () => runRecalc(true);
const doRecalc = () => runRecalc(false);

// Расчёт идёт по СОХРАНЁННЫМ условиям. Поэтому «Пересчитать» сначала сохраняет
// изменённые строки (при ошибке — например, не указано обоснование — пересчёта
// не будет и в тактике останутся прежние суммы), а «Предпросмотр» отправляет
// несохранённые правки как черновик и ничего не пишет.
const runRecalc = async (preview: boolean) => {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    const pending = [...dirtyRows.value];
    let saved = 0;
    if (!preview && pending.length) {
      const res = await saveDirty();
      saved = res.saved;
      if (res.error) {
        // Пересчёт по частично сохранённым условиям запутал бы: останавливаемся.
        error.value = `${res.error}. Пересчёт не выполнен.`;
        return;
      }
    }
    const drafts = preview ? pending.map(rowPayload) : [];
    recalcResult.value = await api.recalc(cardId, preview, drafts);
    if (preview) {
      note.value = drafts.length
        ? `Предпросмотр учитывает несохранённые правки (${drafts.length}) и ничего не записывает.`
        : "";
    } else {
      note.value = `${saved ? `Сохранено строк условий: ${saved}. ` : ""}Расходная часть пересчитана и сохранена в тактике за ${periodLabel.value}.`;
    }
  } catch (e: unknown) {
    error.value = apiError(e, "Пересчёт не выполнен");
  } finally {
    busy.value = false;
  }
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
.cell-hint.default {
  color: var(--accent);
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
.toolbar-card { padding: var(--sp-4); }
.toolbar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--sp-4); }
.toolbar-group { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.toolbar-label { color: var(--text-secondary); font-size: var(--fs-sm); }
.dirty-note { color: var(--warn, var(--text-secondary)); font-size: var(--fs-sm); }
/* Блок изменений: сворачивается, а при большом числе строк прокручивается внутри. */
.diff-header { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-3); }
.diff-toggle { display: inline-flex; align-items: center; gap: var(--sp-2); border: 0; background: none; padding: 0; cursor: pointer; color: inherit; }
.diff-scroll { max-height: 320px; overflow: auto; }
.muted-cell { color: var(--text-muted); white-space: nowrap; }
.fi-btn { border: 0; background: none; padding: 2px; margin-left: 4px; cursor: pointer; color: var(--text-muted); line-height: 1; vertical-align: middle; }
.fi-btn:hover, .fi-btn.on { color: var(--accent); }
.fi { width: 13px; height: 13px; }
.pl-info { display: block; font-size: var(--fs-xs); color: var(--text-secondary); }
.ver-cell { white-space: nowrap; }
.ver { font-family: var(--font-mono); font-weight: 600; }
.ver-who, .ver-dirty { display: block; font-size: var(--fs-xs); color: var(--text-muted); }
.ver-dirty { color: var(--accent); }
.spin {
  animation: spin 1s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
