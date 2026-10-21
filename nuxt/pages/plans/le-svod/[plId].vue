<!--
  Бюджет по ЮЛ (этапы 1.6, 3, 4; TPL-08). Колонки — юридические лица (ЮЛ определяется по ЦФО строки), строки — продажи по
  каналам и расходы по группам P&L, суммы в BYN. Итог ячейки = расчёт из форм + корректировки; корректировка — отдельная
  запись с причиной (администратор процессов), расчёт не переписывается.
-->
<template>
  <div class="page-lesvod">
    <header class="page-header">
      <div>
        <h1 class="page-title">Бюджет по ЮЛ</h1>
        <strong v-if="data" class="tactic-period">{{ periodLabel }}</strong>
        <p class="page-subtitle">Продажи и расходы по юридическим лицам · валюта {{ data?.currency || "BYN" }}</p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="`/plans/${plId}`" class="btn btn-sm btn-ghost"><Icon name="lucide:arrow-left" /> К периоду</NuxtLink>
        <button class="btn btn-sm btn-ghost" :disabled="loading" @click="load">
          <Icon name="lucide:refresh-cw" :class="{ spin: loading }" /> Обновить
        </button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="data?.closed" class="banner banner-info">
      Период утверждён (этап 4): корректировки закрыты. Изменить итоги можно только после переоткрытия периода.
    </p>
    <p v-for="w in data?.warnings || []" :key="w" class="banner banner-warn">{{ w }}</p>

    <section class="card controls">
      <span class="view-lbl">Период расходов</span>
      <div class="segmented" role="group" aria-label="Период расходов">
        <button type="button" :aria-pressed="win === 'month'" @click="setWin('month')">{{ monthLabel }}</button>
        <button type="button" :aria-pressed="win === 'tail'" @click="setWin('tail')">{{ monthLabel }}–дек</button>
        <button type="button" :aria-pressed="win === 'year'" @click="setWin('year')">весь год</button>
      </div>
      <span class="hint">Продажи — тактика месяца периода; окно действует на расходы.</span>
    </section>

    <SkeletonTable v-if="loading && !data" :rows="8" :cols="6" />

    <section v-if="data" class="card">
      <div class="table-scroll" :class="{ busy: loading }">
        <table class="data-table">
          <thead>
            <tr>
              <th>Строка</th>
              <th v-for="lg in data.legals" :key="lg" class="col-num">{{ lg }}</th>
              <th class="col-num">Итого</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!data.rows.length"><td :colspan="data.legals.length + 2" class="empty-cell">Нет данных за период: заполните формы продаж и расходов.</td></tr>
            <template v-for="(r, i) in data.rows" :key="r.key">
              <tr v-if="r.section === 'exp' && data.rows[i - 1]?.section !== 'exp'" class="sec-row">
                <td :colspan="data.legals.length + 2">Расходы</td>
              </tr>
              <tr v-else-if="r.section === 'sales' && i === 0" class="sec-row">
                <td :colspan="data.legals.length + 2">Продажи</td>
              </tr>
              <tr :class="{ 'row-total': r.section === 'total' }">
                <td>{{ r.label }}</td>
                <template v-if="r.key === 'total:ratio'">
                  <td v-for="lg in data.legals" :key="lg" class="col-num">{{ pct(r.ratio?.[lg]) }}</td>
                  <td class="col-num strong">{{ pct(r.ratio_all) }}</td>
                </template>
                <template v-else>
                  <td v-for="lg in data.legals" :key="lg" class="col-num" :class="{ adj: r.adj[lg] }">
                    <button
                      v-if="canAdjust && r.editable && !data.closed"
                      type="button"
                      class="cell-btn"
                      :title="cellTitle(r, lg)"
                      @click="openAdjust(r, lg)"
                    >{{ money0(r.total[lg]) }}</button>
                    <span v-else :title="cellTitle(r, lg)">{{ money0(r.total[lg]) }}</span>
                    <sup v-if="r.adj[lg]" class="adj-mark">корр.</sup>
                  </td>
                  <td class="col-num strong" :class="{ adj: r.adj_all }" :title="r.adj_all ? `расчёт ${money0(r.calc_all)}, корректировка ${money0(r.adj_all)}` : ''">{{ money0(r.total_all) }}</td>
                </template>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
      <p class="prov-note">
        Юридическое лицо берётся из справочника ЦФО (поле «компания»); разные написания одного ЮЛ сводятся в одну колонку,
        незнакомые названия показаны отдельной колонкой. Колонка «(без ЮЛ)» — строки, у ЦФО которых ЮЛ не указано.
      </p>
    </section>

    <section v-if="data?.adjustments?.length" class="card">
      <div class="card-header"><span class="card-title">Корректировки итогов</span></div>
      <table class="data-table">
        <thead><tr><th>ЮЛ</th><th>Строка</th><th class="col-num">Сумма, BYN</th><th>Причина</th><th>Кто, когда</th><th></th></tr></thead>
        <tbody>
          <tr v-for="a in data.adjustments" :key="a.id" :class="{ 'adj-revoked': a.revoked }">
            <td>{{ a.legal_entity }}</td>
            <td>{{ lineLabel(a.line_key) }}</td>
            <td class="col-num">{{ money0(a.amount_byn) }}</td>
            <td>{{ a.reason }}</td>
            <td>
              {{ a.user_name || a.user_id }} · {{ a.created_at.slice(0, 16).replace("T", " ") }}
              <template v-if="a.revoked"><br />снято{{ a.revoked_at ? ` ${a.revoked_at.slice(0, 16).replace("T", " ")}` : "" }}: {{ a.revoke_reason }}</template>
            </td>
            <td>
              <button v-if="canAdjust && !data.closed && !a.revoked" type="button" class="btn btn-sm btn-ghost" @click="askRevoke(a)">Снять</button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- Корректировка итога ячейки -->
    <PlansModal :open="!!adj" :title="adj ? `Корректировка · ${adj.row.label} · ${adj.legal}` : ''" width="520px" @close="adj = null">
      <template v-if="adj">
        <p class="pm-hint">
          Расчёт {{ money0(adj.row.calc[adj.legal]) }}, уже скорректировано {{ money0(adj.row.adj[adj.legal]) }}, итог
          {{ money0(adj.row.total[adj.legal]) }} BYN. Введите сумму, которую нужно ДОБАВИТЬ к итогу (минус — уменьшить).
        </p>
        <label class="form-field">
          <span class="form-label">Сумма корректировки, BYN</span>
          <input v-model="adjAmount" class="input" inputmode="decimal" placeholder="например, -15000" />
        </label>
        <label class="form-field">
          <span class="form-label">Причина (обязательно)</span>
          <textarea v-model="adjReason" class="textarea" rows="3"></textarea>
        </label>
        <p v-if="adjError" class="banner banner-neg">{{ adjError }}</p>
        <div class="modal-foot">
          <button class="btn btn-ghost" @click="adj = null">Отмена</button>
          <button class="btn btn-primary" :disabled="busy || !adjReason.trim() || !adjNumber" @click="saveAdjust">Сохранить</button>
        </div>
      </template>
    </PlansModal>

    <PlansModal :open="!!rev" :title="rev ? 'Снять корректировку' : ''" width="480px" @close="rev = null">
      <template v-if="rev">
        <p class="pm-hint">{{ rev.legal_entity }} · {{ lineLabel(rev.line_key) }} · {{ money0(rev.amount_byn) }} BYN</p>
        <label class="form-field">
          <span class="form-label">Причина снятия (обязательно)</span>
          <textarea v-model="revReason" class="textarea" rows="3"></textarea>
        </label>
        <div class="modal-foot">
          <button class="btn btn-ghost" @click="rev = null">Отмена</button>
          <button class="btn btn-danger" :disabled="busy || !revReason.trim()" @click="doRevoke">Снять</button>
        </div>
      </template>
    </PlansModal>
  </div>
</template>

<script setup lang="ts">
import SkeletonTable from "~/components/SkeletonTable.vue";
import { MONTHS_SHORT } from "~/composables/useExpForm";
import { useLeSvod, type LeAdjust, type LeSvod, type LeSvodRow, type LeWindow } from "~/composables/useLeSvod";
import { apiError } from "~/utils/apiError";
import { num as fmtNum } from "~/utils/format";

definePageMeta({ middleware: ["scope-guard"] });

const route = useRoute();
const plId = Number(route.params.plId);
const api = useLeSvod();
const { hasRole, isAdmin } = useScope();
const canAdjust = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const data = ref<LeSvod | null>(null);
const loading = ref(false);
const busy = ref(false);
const error = ref("");
const win = ref<LeWindow>("month");

const monthLabel = computed(() => (data.value ? MONTHS_SHORT[Math.min(12, Math.max(1, data.value.month)) - 1] : "месяц"));
const periodLabel = computed(() => {
  if (!data.value) return "";
  const { year, from_month: from, to_month: to } = data.value;
  const exp = from === to ? `${MONTHS_SHORT[from - 1]} ${year}` : `${MONTHS_SHORT[from - 1]}–${MONTHS_SHORT[to - 1]} ${year}`;
  return `Продажи: ${MONTHS_SHORT[data.value.month - 1]} ${year} · расходы: ${exp}`;
});

const money0 = (v: number | null | undefined): string => (typeof v === "number" && v !== 0 ? fmtNum(v, 0) : "—");
const pct = (v: number | null | undefined): string => (typeof v === "number" ? `${fmtNum(v, 1)} %` : "—");
const lineLabel = (key: string) => key.replace(/^(sales|exp):/, "");
const cellTitle = (r: LeSvodRow, lg: string) =>
  r.adj[lg] ? `расчёт ${money0(r.calc[lg])}, корректировка ${money0(r.adj[lg])}` : r.editable && canAdjust.value ? "Нажмите, чтобы скорректировать итог" : "";

let seq = 0;
const load = async () => {
  const my = ++seq;
  loading.value = true;
  error.value = "";
  try {
    const r = await api.svod(plId, win.value);
    if (my === seq) data.value = r;
  } catch (e) {
    if (my === seq) error.value = apiError(e, "Свод не загружен");
  } finally {
    if (my === seq) loading.value = false;
  }
};
const setWin = (w: LeWindow) => {
  if (win.value === w) return;
  win.value = w;
  load();
};

const adj = ref<{ row: LeSvodRow; legal: string } | null>(null);
const adjAmount = ref("");
const adjReason = ref("");
const adjError = ref("");
const adjNumber = computed(() => {
  const n = Number(adjAmount.value.replace(/\s/g, "").replace(",", "."));
  return Number.isFinite(n) && n !== 0 ? n : 0;
});
const openAdjust = (row: LeSvodRow, legal: string) => {
  adj.value = { row, legal };
  adjAmount.value = "";
  adjReason.value = "";
  adjError.value = "";
};
const saveAdjust = async () => {
  if (!adj.value) return;
  busy.value = true;
  adjError.value = "";
  try {
    await api.adjust(plId, { legal_entity: adj.value.legal, line_key: adj.value.row.key, amount_byn: adjNumber.value, reason: adjReason.value.trim() });
    adj.value = null;
    await load();
  } catch (e) {
    adjError.value = apiError(e, "Корректировка не сохранена");
  } finally {
    busy.value = false;
  }
};

const rev = ref<LeAdjust | null>(null);
const revReason = ref("");
const askRevoke = (a: LeAdjust) => {
  rev.value = a;
  revReason.value = "";
};
const doRevoke = async () => {
  if (!rev.value) return;
  busy.value = true;
  try {
    await api.revoke(plId, rev.value.id, revReason.value.trim());
    rev.value = null;
    await load();
  } catch (e) {
    error.value = apiError(e, "Не удалось снять корректировку");
    rev.value = null;
  } finally {
    busy.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.page-lesvod { display: flex; flex-direction: column; gap: var(--sp-5); }
.controls { display: flex; align-items: center; gap: var(--sp-4); flex-wrap: wrap; padding: var(--sp-3) var(--sp-4); }
.view-lbl { color: var(--text-muted); font-size: var(--fs-sm); }
.hint { color: var(--text-muted); font-size: var(--fs-sm); }
.table-scroll { overflow-x: auto; }
.table-scroll.busy { opacity: 0.6; }
.sec-row td { background: var(--bg-surface-3); font-weight: 600; color: var(--text-secondary); }
.row-total td { font-weight: 600; background: var(--bg-surface-3); }
.strong { font-weight: 600; }
.empty-cell { text-align: center; color: var(--text-muted); padding: var(--sp-6); }
.adj { background: var(--warn-soft); }
.adj-revoked td { color: var(--text-muted); text-decoration: line-through; }
.adj-revoked td:nth-child(5) { text-decoration: none; }
.adj-mark { margin-left: var(--sp-1); color: var(--warn); font-size: 0.7em; }
.cell-btn { background: none; border: none; padding: 0; font: inherit; color: inherit; cursor: pointer; text-decoration: underline dotted; }
.prov-note { margin: var(--sp-3) var(--sp-4); color: var(--text-muted); font-size: var(--fs-sm); }
.pm-hint { margin: 0 0 var(--sp-3); color: var(--text-muted); font-size: var(--fs-sm); }
.form-field { display: flex; flex-direction: column; gap: var(--sp-1); margin-bottom: var(--sp-3); }
.form-label { font-size: var(--fs-sm); color: var(--text-secondary); }
.modal-foot { display: flex; justify-content: flex-end; gap: var(--sp-2); margin-top: var(--sp-3); }
</style>
