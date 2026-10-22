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
    <section v-if="data" class="card ready-card" :class="{ 'ready-bad': appr?.stages['1.6'] !== 'completed' && !data.ready }">
      <div class="card-header">
        <span class="card-title">Готовность к этапу 1.6</span>
        <span v-if="appr?.stages['1.6'] === 'completed'" class="badge badge-pos">Согласовано</span>
        <span v-else class="badge" :class="data.ready ? 'badge-pos' : 'badge-neg'">{{ data.ready ? "можно завершать" : "есть блокирующие замечания" }}</span>
      </div>
      <ul v-if="appr?.stages['1.6'] !== 'completed' && data.readiness?.length" class="ready-list">
        <li v-for="(i, k) in data.readiness" :key="k">
          <span class="badge" :class="i.level === 'block' ? 'badge-neg' : 'badge-warn'">{{ i.level === "block" ? "блокирует" : "предупреждение" }}</span>
          {{ i.message }}
        </li>
      </ul>
      <p v-else-if="appr?.stages['1.6'] !== 'completed'" class="ready-ok">Все формы продаж и расходов утверждены, замечаний нет.</p>
    </section>

    <!-- ===== Согласование форм по каналам (этап 1.6) ===== -->
    <section v-if="appr" class="card appr-card">
      <div class="card-header">
        <span class="card-title">Согласование форм по каналам · этап 1.6</span>
        <span class="badge" :class="stageBadge('1.6')">{{ stageText("1.6") }}</span>
        <button v-if="appr.can_configure" type="button" class="btn btn-sm btn-ghost" @click="openConfig">
          <Icon name="lucide:settings-2" /> Настроить согласующих
        </button>
      </div>
      <p v-if="appr.stages['1.5'] !== 'completed'" class="appr-hint">
        Согласование форм откроется после завершения этапа 1.5. Для каждого канала назначается один согласующий.
      </p>
      <table class="data-table">
        <thead><tr><th>Канал</th><th>Согласующий</th><th>Статус</th><th></th></tr></thead>
        <tbody>
          <tr v-if="!appr.stage16.length"><td colspan="4" class="empty-cell">В своде нет каналов продаж.</td></tr>
          <tr v-for="sc in appr.stage16" :key="sc.legal_entity">
            <td>{{ sc.legal_entity }}</td>
            <td><PlansApproverChips :scope="sc" /></td>
            <td>
              <span class="badge" :class="sc.satisfied ? 'badge-pos' : sc.no_approvers ? 'badge-neg' : 'badge-warn'">
                {{ sc.satisfied ? "согласовано" : sc.no_approvers ? "нет согласующего" : "ждём" }}
              </span>
            </td>
            <td class="appr-act">
              <template v-if="sc.can_decide">
                <button type="button" class="btn btn-sm btn-primary" @click="openDecide(sc, 'approve')">Согласовано</button>
                <button type="button" class="btn btn-sm btn-ghost" @click="openDecide(sc, 'return')">Вернуть</button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- ===== Согласование ЮЛ (этап 3) и итоговое утверждение (этап 4) ===== -->
    <details v-if="appr" class="card appr-card approval-fold" :open="stage3Open" @toggle="syncStage3Open">
      <summary>Согласование бюджетов ЮЛ · этап 3 <span class="badge" :class="stageBadge('3')">{{ stageText('3') }}</span></summary>
      <div class="approval-fold-body">
      <div v-if="appr.can_configure" class="card-header">
        <button v-if="appr.can_configure" type="button" class="btn btn-sm btn-ghost" @click="openConfig">
          <Icon name="lucide:settings-2" /> Настроить согласующих
        </button>
      </div>
      <p v-if="appr.stages['1.6'] !== 'completed'" class="appr-hint">
        Согласование ЮЛ откроется после завершения этапа 1.6 «Формирование по ЮЛ и каналам».
      </p>
      <table class="data-table">
        <thead><tr><th>ЮЛ</th><th>Согласующие</th><th>Правило</th><th>Статус</th><th></th></tr></thead>
        <tbody>
          <tr v-if="!appr.stage3.length"><td colspan="5" class="empty-cell">В своде нет юридических лиц.</td></tr>
          <tr v-for="sc in appr.stage3" :key="sc.legal_entity">
            <td>{{ sc.legal_entity }}</td>
            <td><PlansApproverChips :scope="sc" /></td>
            <td>{{ sc.require_all ? "нужны все" : "достаточно одного" }}</td>
            <td>
              <span class="badge" :class="sc.satisfied ? 'badge-pos' : sc.no_approvers ? 'badge-neg' : 'badge-warn'">
                {{ sc.satisfied ? "Согласовано" : sc.no_approvers ? "нет согласующих" : "ждём" }}
              </span>
            </td>
            <td class="appr-act">
              <template v-if="sc.can_decide">
                <button type="button" class="btn btn-sm btn-primary" @click="openDecide(sc, 'approve')">Согласовано</button>
                <button type="button" class="btn btn-sm btn-ghost" @click="openDecide(sc, 'return')">Вернуть</button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
      </div>
    </details>

    <details v-if="appr" class="card appr-card approval-fold" :open="stage4Open" @toggle="syncStage4Open">
      <summary>Итоговое утверждение · этап 4 <span class="badge" :class="stageBadge('4')">{{ stageText('4') }}</span></summary>
      <div class="approval-fold-body">
      <p v-if="appr.stages['3'] !== 'completed'" class="appr-hint">Итоговое утверждение откроется после завершения этапа 3.</p>
      <table class="data-table">
        <tbody>
          <tr>
            <td><PlansApproverChips :scope="appr.stage4" /></td>
            <td>{{ appr.stage4.require_all ? "нужны все" : "достаточно одного" }}</td>
            <td>
              <span class="badge" :class="appr.stage4.satisfied ? 'badge-pos' : appr.stage4.no_approvers ? 'badge-neg' : 'badge-warn'">
                {{ appr.stage4.satisfied ? "Согласовано" : appr.stage4.no_approvers ? "нет согласующих" : "ждём" }}
              </span>
            </td>
            <td class="appr-act">
              <template v-if="appr.stage4.can_decide">
                <button type="button" class="btn btn-sm btn-primary" @click="openDecide(appr.stage4, 'approve')">Согласовано</button>
                <button type="button" class="btn btn-sm btn-ghost" @click="openDecide(appr.stage4, 'return')">Вернуть</button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
      <details v-if="appr.decisions.length" class="appr-log">
        <summary>Журнал решений ({{ appr.decisions.length }})</summary>
        <ul>
          <li v-for="d in appr.decisions" :key="d.id" :class="{ revoked: d.revoked }">
            этап {{ d.stage_code }}<template v-if="d.legal_entity"> · {{ d.legal_entity }}</template> ·
            {{ d.user_name || d.user_id }} · {{ d.decision === "approve" ? "согласовал(а)" : "вернул(а)" }}
            <template v-if="d.comment"> — «{{ d.comment }}»</template> · {{ d.created_at.slice(0, 16).replace("T", " ") }}
            <template v-if="d.revoked"> · снято: {{ d.revoke_reason }}</template>
          </li>
        </ul>
      </details>
      </div>
    </details>

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
              <th>Строка · валюта</th>
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
                <td>
                  <span>{{ r.label }}</span>
                  <small class="currency-note">{{ rowCurrency(r.key) }}</small>
                </td>
                <template v-if="r.key === 'total:ratio'">
                  <td v-for="lg in data.legals" :key="lg" class="col-num">{{ pct(r.ratio?.[lg]) }}</td>
                  <td class="col-num strong">{{ pct(r.ratio_all) }}</td>
                </template>
                <template v-else>
                  <td v-for="lg in data.legals" :key="lg" class="col-num" :class="{ adj: r.adj[lg] }">
                    <button
                      v-if="canAdjustFor(lg) && r.editable && !data.closed"
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
              <button v-if="canAdjustFor(a.legal_entity) && !data.closed && !a.revoked" type="button" class="btn btn-sm btn-ghost" @click="askRevoke(a)">Снять</button>
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

    <!-- Решение согласующего -->
    <PlansModal :open="!!dec" :title="dec ? decTitle : ''" width="520px" @close="dec = null">
      <template v-if="dec">
        <p class="pm-hint" v-if="dec.kind === 'return'">
          Возврат отправит период {{ dec.scope.stage_code === "1.6" ? "на этап 1.5" : dec.scope.stage_code === "3" ? "на этап 1.6" : "на этап 3" }} и снимет согласия. Причина обязательна.
        </p>
        <p class="pm-hint" v-else>
          {{ dec.scope.require_all ? "Для завершения этапа нужно согласие каждого из списка." : "Для завершения этапа достаточно согласия одного из списка." }}
        </p>
        <label class="form-field">
          <span class="form-label">Комментарий{{ dec.kind === "return" ? " (обязательно)" : "" }}</span>
          <textarea v-model="decComment" class="textarea" rows="3"></textarea>
        </label>
        <p v-if="decError" class="banner banner-neg">{{ decError }}</p>
        <div class="modal-foot">
          <button class="btn btn-ghost" @click="dec = null">Отмена</button>
          <button class="btn" :class="dec.kind === 'return' ? 'btn-danger' : 'btn-primary'" :disabled="busy || (dec.kind === 'return' && !decComment.trim())" @click="saveDecision">
            {{ dec.kind === "return" ? "Вернуть" : "Согласовано" }}
          </button>
        </div>
      </template>
    </PlansModal>

    <!-- Настройка согласующих (администратор процессов) -->
    <PlansModal :open="cfgOpen" title="Согласующие этапов 1.6, 3 и 4" width="760px" @close="cfgOpen = false">
      <p class="pm-hint">
        Для каждого канала (этап 1.6) назначьте одного согласующего. По каждому ЮЛ (этап 3) и для итогового утверждения (этап 4)
        задайте список и правило: нужны все или достаточно одного. Изменения применяются сразу после «Сохранить» в строке.
      </p>
      <p v-if="cfgError" class="banner banner-neg">{{ cfgError }}</p>
      <div v-for="row in cfgRows" :key="row.key" class="cfg-row">
        <div class="cfg-head">
          <b>{{ row.stage === "4" ? "Итоговое утверждение (этап 4)" : row.stage === "1.6" ? `Этап 1.6 · канал ${row.legal}` : `Этап 3 · ${row.legal}` }}</b>
          <label v-if="row.stage !== '1.6'" class="checkbox">
            <input v-model="row.requireAll" type="checkbox" /> нужны все (иначе достаточно одного)
          </label>
          <button type="button" class="btn btn-sm btn-primary" :disabled="busy" @click="saveRow(row)">Сохранить</button>
        </div>
        <div class="cfg-users">
          <span v-for="u in row.users" :key="u.id" class="cfg-user">
            {{ u.name || `#${u.id}` }}
            <button type="button" class="cfg-x" title="Убрать" @click="row.users = row.users.filter((x) => x.id !== u.id)">×</button>
          </span>
          <span v-if="!row.users.length" class="cfg-none">нет согласующих</span>
          <ClientOnly>
            <UserPicker placeholder="добавить согласующего…" @picked="(u: { id: number; name: string }) => addUser(row, u)" />
          </ClientOnly>
        </div>
      </div>
      <div class="modal-foot"><button class="btn btn-ghost" @click="cfgOpen = false">Закрыть</button></div>
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
import UserPicker from "~/components/plans/UserPicker.vue";
import { MONTHS_SHORT } from "~/composables/useExpForm";
import { useLeSvod, type LeAdjust, type LeApprovalStatus, type LeScope, type LeSvod, type LeSvodRow, type LeWindow } from "~/composables/useLeSvod";
import { apiError } from "~/utils/apiError";
import { num as fmtNum } from "~/utils/format";

definePageMeta({ middleware: ["scope-guard"] });

const route = useRoute();
const plId = Number(route.params.plId);
const api = useLeSvod();
const { hasRole, isAdmin } = useScope();
const isPlansAdmin = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));
// Корректировать итог ЮЛ может администратор процессов или согласующий этого ЮЛ (этап 3).
const canAdjustFor = (legal: string): boolean =>
  isPlansAdmin.value || !!appr.value?.stage3.some((s) => s.legal_entity === legal && s.approvers.some((a) => a.mine));
const canAdjust = computed(() => isPlansAdmin.value || !!appr.value?.stage3.some((s) => s.approvers.some((a) => a.mine)));

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
const rowCurrency = (key: string): string => {
  const source = data.value?.source_currencies?.[key] || [];
  return source.length ? `${source.join(", ")} → BYN` : "BYN";
};
const cellTitle = (r: LeSvodRow, lg: string) =>
  r.adj[lg] ? `расчёт ${money0(r.calc[lg])}, корректировка ${money0(r.adj[lg])}` : r.editable && canAdjustFor(lg) ? "Нажмите, чтобы скорректировать итог" : "";

let seq = 0;
const load = async () => {
  const my = ++seq;
  loading.value = true;
  error.value = "";
  try {
    const r = await api.svod(plId, win.value);
    if (my === seq) {
      // Пустой период допустим. Go может сериализовать пустые срезы как null,
      // а шаблону таблицы нужны массивы для colspan и перебора.
      data.value = {
        ...r,
        legals: Array.isArray(r.legals) ? r.legals : [],
        rows: Array.isArray(r.rows) ? r.rows : [],
        adjustments: Array.isArray(r.adjustments) ? r.adjustments : [],
        readiness: Array.isArray(r.readiness) ? r.readiness : [],
        warnings: Array.isArray(r.warnings) ? r.warnings : []
      };
    }
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

// ───────── Согласование ЮЛ (этап 3) и итоговое утверждение (этап 4) ─────────
const appr = ref<LeApprovalStatus | null>(null);
const loadAppr = async () => {
  try {
    const result = await api.approval(plId);
    // Для периода без ЮЛ API может вернуть null вместо пустых срезов Go.
    // Нормализуем ответ, чтобы страница оставалась рабочей.
    appr.value = {
      ...result,
      stages: result.stages || {},
      stage16: Array.isArray(result.stage16) ? result.stage16 : [],
      stage3: Array.isArray(result.stage3) ? result.stage3 : [],
      stage4: {
        ...result.stage4,
        approvers: Array.isArray(result.stage4?.approvers) ? result.stage4.approvers : []
      },
      decisions: Array.isArray(result.decisions) ? result.decisions : []
    };
  } catch (e) {
    error.value = apiError(e, "Состояние согласования не загружено");
  }
};
const STAGE_TEXT: Record<string, string> = { completed: "Согласовано", in_progress: "в работе", returned: "возвращён", pending: "ожидает" };
const stageText = (code: string) => STAGE_TEXT[appr.value?.stages[code] || "pending"] || "ожидает";
const stageBadge = (code: string) => {
  const s = appr.value?.stages[code];
  return s === "completed" ? "badge-pos" : s === "returned" ? "badge-neg" : s === "in_progress" ? "badge-warn" : "badge-muted";
};
const stage3NeedsMe = computed(() => !!appr.value?.stage3.some((scope) => scope.can_decide));
const stage4NeedsMe = computed(() => !!appr.value?.stage4.can_decide);
const stage3Open = ref(false);
const stage4Open = ref(false);
// Открываем блок при появлении нового доступного решения, в том числе после
// завершения этапа 3. Обратный переход не закрывает результат принятого решения.
watch(stage3NeedsMe, (needsMe, wasNeeded) => {
  if (needsMe && !wasNeeded) stage3Open.value = true;
});
watch(stage4NeedsMe, (needsMe, wasNeeded) => {
  if (needsMe && !wasNeeded) stage4Open.value = true;
});
const syncStage3Open = (event: Event) => { stage3Open.value = (event.target as HTMLDetailsElement).open; };
const syncStage4Open = (event: Event) => { stage4Open.value = (event.target as HTMLDetailsElement).open; };

const dec = ref<{ scope: LeScope; kind: "approve" | "return" } | null>(null);
const decComment = ref("");
const decError = ref("");
const decTitle = computed(() => {
  if (!dec.value) return "";
  const s = dec.value.scope;
  const what = dec.value.kind === "return" ? "Возврат" : s.stage_code === "4" ? "Итоговое утверждение" : "Согласование";
  return s.stage_code === "3" ? `${what} · ЮЛ ${s.legal_entity}` : s.stage_code === "1.6" ? `${what} · канал ${s.legal_entity}` : what;
});
const openDecide = (scope: LeScope, kind: "approve" | "return") => {
  dec.value = { scope, kind };
  decComment.value = "";
  decError.value = "";
};
const saveDecision = async () => {
  if (!dec.value) return;
  busy.value = true;
  decError.value = "";
  try {
    await api.decide(plId, {
      stage_code: dec.value.scope.stage_code,
      legal_entity: dec.value.scope.legal_entity,
      decision: dec.value.kind,
      comment: decComment.value.trim()
    });
    dec.value = null;
    await Promise.all([load(), loadAppr()]);
  } catch (e) {
    decError.value = apiError(e, "Решение не сохранено");
  } finally {
    busy.value = false;
  }
};

// Настройка списков согласующих (администратор процессов).
interface CfgRow {
  key: string;
  stage: "1.6" | "3" | "4";
  legal: string;
  requireAll: boolean;
  users: { id: number; name: string }[];
}
const cfgOpen = ref(false);
const cfgError = ref("");
const cfgRows = ref<CfgRow[]>([]);
const openConfig = async () => {
  cfgError.value = "";
  try {
    const cfg = await api.approvalConfig();
    // Старые версии API могут вернуть null вместо пустых массивов.
    const approvers = Array.isArray(cfg?.approvers) ? cfg.approvers : [];
    const policies = Array.isArray(cfg?.policies) ? cfg.policies : [];
    const rowFor = (stage: "1.6" | "3" | "4", legal: string): CfgRow => {
      const pol = policies.find((p) => p.stage_code === stage && p.legal_entity === legal);
      return {
        key: `${stage}|${legal}`,
        stage,
        legal,
        requireAll: stage === "1.6" ? true : pol ? pol.require_all : true,
        users: approvers.filter((a) => a.stage_code === stage && a.legal_entity === legal).map((a) => ({ id: a.user_id, name: a.user_name || "" }))
      };
    };
    const legals = (data.value?.legals || []).filter((l) => l !== "(без ЮЛ)");
    const channels = (appr.value?.stage16 || []).map((s) => s.legal_entity);
    cfgRows.value = [
      ...channels.map((channel) => rowFor("1.6", channel)),
      rowFor("4", ""),
      ...legals.map((l) => rowFor("3", l))
    ];
    cfgOpen.value = true;
  } catch (e) {
    error.value = apiError(e, "Настройки согласующих не загружены");
  }
};
const addUser = (row: CfgRow, u: { id: number; name: string }) => {
  if (row.stage === "1.6") row.users = [{ id: u.id, name: u.name }];
  else if (!row.users.some((x) => x.id === u.id)) row.users = [...row.users, { id: u.id, name: u.name }];
};
const saveRow = async (row: CfgRow) => {
  busy.value = true;
  cfgError.value = "";
  try {
    await api.setApprovers({ stage_code: row.stage, legal_entity: row.legal, user_ids: row.users.map((u) => u.id), require_all: row.requireAll });
    await loadAppr();
  } catch (e) {
    cfgError.value = apiError(e, "Список не сохранён");
  } finally {
    busy.value = false;
  }
};

onMounted(() => {
  load();
  loadAppr();
});
</script>

<style scoped>
.page-lesvod { display: flex; flex-direction: column; gap: var(--sp-5); }
.ready-card { padding-bottom: var(--sp-3); }
.appr-card { padding-bottom: var(--sp-2); }
.approval-fold > summary { display: flex; align-items: center; gap: var(--sp-3); cursor: pointer; list-style: none; padding: var(--sp-4); font-weight: 600; }
.approval-fold > summary::-webkit-details-marker { display: none; }
.approval-fold > summary::before { content: '▸'; color: var(--text-muted); }
.approval-fold[open] > summary::before { content: '▾'; }
.approval-fold-body { padding-top: var(--sp-1); }
.appr-hint { margin: 0 var(--sp-4) var(--sp-2); color: var(--text-muted); font-size: var(--fs-sm); }
.appr-act { display: flex; gap: var(--sp-2); justify-content: flex-end; flex-wrap: wrap; }
.appr-log { margin: var(--sp-2) var(--sp-4); font-size: var(--fs-sm); color: var(--text-secondary); }
.appr-log li.revoked { text-decoration: line-through; color: var(--text-muted); }
.currency-note { display: block; color: var(--text-muted); font-size: var(--fs-xs); font-weight: var(--fw-normal); }
.cfg-row { border: 1px solid var(--border, var(--bg-tonal)); border-radius: var(--rd-4, 6px); padding: var(--sp-3); margin-bottom: var(--sp-3); }
.cfg-head { display: flex; align-items: center; gap: var(--sp-4); flex-wrap: wrap; margin-bottom: var(--sp-2); }
.cfg-users { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: wrap; }
.cfg-user { background: var(--bg-surface-3); border-radius: var(--rd-4, 6px); padding: 2px var(--sp-2); font-size: var(--fs-sm); }
.cfg-x { background: none; border: none; cursor: pointer; color: var(--text-muted); font-size: 1em; padding: 0 0 0 var(--sp-1); }
.cfg-none { color: var(--text-muted); font-size: var(--fs-sm); }
.ready-list { margin: 0; padding: 0 var(--sp-4) var(--sp-2); list-style: none; display: flex; flex-direction: column; gap: var(--sp-2); font-size: var(--fs-sm); }
.ready-ok { margin: 0; padding: 0 var(--sp-4) var(--sp-2); color: var(--text-muted); font-size: var(--fs-sm); }
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
