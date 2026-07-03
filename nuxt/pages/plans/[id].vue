<template>
  <div class="page-pl-card">
    <header class="page-header">
      <div>
        <h1 class="page-title">Тактический план · {{ year }}-{{ String(month).padStart(2, "0") }}</h1>
        <p class="page-subtitle">Формирование тактических планов Profit &amp; Loss — согласование по схеме</p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="`/plans/process/${id}`" class="btn btn-primary">
          <Icon name="lucide:list-checks" /> Процесс · задания
        </NuxtLink>
      </div>
    </header>

    <!-- Прогресс по этапам -->
    <div class="progress-row">
      <div class="progress-bar">
        <div class="progress-fill" :style="{ width: pct + '%' }"></div>
      </div>
      <span class="progress-label">{{ done }} из {{ list.length }} этапов завершено · {{ pct }}%</span>
    </div>

    <div class="segmented" role="tablist">
      <button class="seg" :class="{ active: tab === 'process' }" @click="tab = 'process'">Процесс</button>
      <button class="seg" :class="{ active: tab === 'pnl' }" @click="openPnl">План · Тактика</button>
      <button class="seg" :class="{ active: tab === 'svod' }" @click="tab = 'svod'">Свод по ЮЛ</button>
    </div>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>

    <!-- ===== ПРОЦЕСС: календарный таймлайн ===== -->
    <section v-show="tab === 'process'" class="timeline card">
      <div class="tl-scroll">
        <div class="tl-grid" :style="{ gridTemplateColumns: `var(--tl-track) repeat(${COLS.length}, minmax(150px, 1fr))` }">
          <!-- шапка календаря -->
          <div class="tl-corner">Календарь</div>
          <div v-for="c in COLS" :key="c.key" class="tl-colhead">
            <span class="tl-col-label">{{ c.label }}</span>
            <span class="tl-col-sub">{{ c.sub }}</span>
            <span v-if="colDue(c)" class="tl-col-date">{{ colDue(c) }}</span>
          </div>

          <!-- треки -->
          <template v-for="tr in TRACKS" :key="tr.key">
            <div class="tl-track" :class="`track-${tr.key}`">{{ tr.label }}</div>
            <div
              v-for="c in COLS"
              :key="tr.key + c.key"
              class="tl-cell"
            >
              <article
                v-for="st in cellStages(tr, c)"
                :key="st.stage_code"
                class="stage-card"
                :class="`s-${st.status}`"
              >
                <div class="sc-top">
                  <span class="sc-code">Этап {{ st.stage_code }}</span>
                  <span class="badge" :class="statusBadge(st.status)">{{ statusLabel(st.status) }}</span>
                </div>
                <div class="sc-name">{{ st.name }}</div>
                <button type="button" class="sc-tasks" :title="`Открыть задания этапа ${st.stage_code}`" @click="stageModal = st">
                  <span class="sct-count" :class="{ ok: stageTasks(st.stage_code).length && stageDone(st.stage_code) === stageTasks(st.stage_code).length }">
                    <Icon name="lucide:list-checks" /> {{ stageDone(st.stage_code) }}/{{ stageTasks(st.stage_code).length }} заданий
                  </span>
                  <span class="sct-people" :title="stageAssignees(st.stage_code).join(', ')">
                    <Icon name="lucide:users" /> {{ peopleLabel(st.stage_code) }}
                  </span>
                </button>
                <div class="sc-meta">
                  <span v-if="depHint(st)" class="sc-dep"><Icon name="lucide:lock" /> {{ depHint(st) }}</span>
                  <span v-else-if="st.due_at" class="sc-due"><Icon name="lucide:calendar" /> {{ st.due_at }}</span>
                </div>
                <div class="sc-actions">
                  <button
                    v-if="!isApproval(st.stage_code)"
                    class="btn btn-sm btn-primary"
                    :disabled="!!depHint(st) || st.status === 'completed'"
                    @click="doAct(st.stage_code, 'submit')"
                  >Отправить</button>
                  <button
                    v-if="isApproval(st.stage_code)"
                    class="btn btn-sm btn-primary"
                    :disabled="!!depHint(st) || st.status === 'completed'"
                    @click="doAct(st.stage_code, 'approve')"
                  >Согласовать</button>
                  <button
                    v-if="canReturn(st)"
                    class="btn btn-sm btn-ghost"
                    @click="openReturn(st.stage_code)"
                  >Вернуть</button>
                </div>
                <div v-if="stageForms(st.stage_code).length" class="sc-forms">
                  <NuxtLink
                    v-for="f in stageForms(st.stage_code)"
                    :key="f.to"
                    :to="f.to"
                    class="sc-formlink"
                  ><Icon name="lucide:file-input" /> {{ f.label }}</NuxtLink>
                </div>
              </article>
            </div>
          </template>
        </div>
      </div>

      <div class="tl-legend">
        <span><i class="dot s-pending"></i> ожидает</span>
        <span><i class="dot s-in_progress"></i> в работе</span>
        <span><i class="dot s-completed"></i> завершён</span>
        <span><i class="dot s-returned"></i> возвращён</span>
      </div>
    </section>

    <!-- ===== СВОДНОЕ ОКНО: План · Тактика · Стратегия ===== -->
    <section v-show="tab === 'pnl'" class="card">
      <div class="card-header pnl-head">
        <span class="card-title">Сводный P&amp;L — единое окно (read-only)</span>
        <div class="pnl-actions">
          <span v-if="pnl" class="pnl-prev">сравнение с {{ pnl.prev_year ? `${pnl.prev_year}-${String(pnl.prev_month).padStart(2, "0")}` : "—" }}</span>
          <button v-if="canAdmin" class="btn btn-sm btn-ghost" @click="stratInput?.click()"><Icon name="lucide:upload" /> Импорт стратегии</button>
          <input ref="stratInput" type="file" accept=".xlsx" class="hidden-file" @change="doStrategyImport" />
        </div>
      </div>
      <p v-if="pnlNote" class="banner banner-pos">{{ pnlNote }}</p>
      <div class="table-wrap">
        <table class="data-table pnl-table">
          <thead>
            <tr>
              <th>ЦФО</th><th>Статья</th>
              <th class="num">Факт</th><th class="num">Стратегия</th><th class="num">Тактика</th>
              <th class="num">Расчёт</th><th class="num">Δ такт−страт</th><th class="num">Прошл. период</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="pnl && !pnl.rows.length"><td colspan="8" class="empty-cell">Нет данных. Заполните формы заданий (тактика) и импортируйте стратегию.</td></tr>
            <tr v-for="(r, i) in pnl?.rows || []" :key="i" :class="{ adj: r.is_manual }">
              <td>{{ r.name_cfo || r.code_cfo }}</td>
              <td class="exp">{{ r.expense_name || r.line_code }}</td>
              <td class="num">{{ pf(r.fact) }}</td>
              <td class="num strat">{{ pf(r.strategy) }}</td>
              <td class="num tac">{{ pf(r.tactic) }}</td>
              <td class="num">{{ pf(r.calc) }}</td>
              <td class="num" :class="deltaCls(r)">{{ deltaTS(r) }}</td>
              <td class="num prev">{{ pf(r.prev_tactic) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="prov-note">Тактику наполняют задания этапов; стратегия — импорт «МП_стратегия_26»; факт — источник МП; Δ = тактика − стратегия.</p>
    </section>

    <!-- ===== СВОД ===== -->
    <section v-show="tab === 'svod'" class="card">
      <div class="card-header"><span class="card-title">Свод по ЮЛ × каналам (TPL-08, товарооборот)</span></div>
      <table class="data-table">
        <thead>
          <tr><th>Юридическое лицо</th><th>Канал</th><th class="col-num">Товарооборот</th><th>Валюта</th></tr>
        </thead>
        <tbody>
          <tr v-if="!svod.length"><td colspan="4" class="empty-cell">Нет данных тактики за период — заполните форму МП.</td></tr>
          <tr v-for="(s, i) in svod" :key="i">
            <td class="le-cell">{{ s.legal_entity }}</td>
            <td>{{ s.channel }}</td>
            <td class="col-num strong">{{ money(s.amount, { currency: "" }) }}</td>
            <td>{{ s.currency }}</td>
          </tr>
        </tbody>
      </table>
      <p class="prov-note">Маппинг площадка → ЮЛ провизорный (на уточнении у аналитика).</p>
    </section>

    <!-- ===== Модалка возврата ===== -->
    <PlansModal
      :open="ret.open"
      :title="`Вернуть этап ${ret.from}`"
      subtitle="Выберите этап для доработки и укажите причину"
      @close="ret.open = false"
    >
      <label class="field">
        <span class="field-label">Вернуть на этап</span>
        <select v-model="ret.target" class="select">
          <option value="" disabled>— выберите —</option>
          <option v-for="o in returnTargets(ret.from)" :key="o.code" :value="o.code">
            {{ o.code }} — {{ o.name }}
          </option>
        </select>
      </label>
      <label class="field">
        <span class="field-label">Причина возврата</span>
        <textarea v-model="ret.reason" class="select textarea" rows="3" placeholder="что доработать"></textarea>
      </label>
      <template #footer>
        <button class="btn btn-ghost" @click="ret.open = false">Отмена</button>
        <button class="btn btn-danger" :disabled="!ret.target || busy" @click="submitReturn">
          <Icon name="lucide:corner-up-left" /> Вернуть
        </button>
      </template>
    </PlansModal>

    <!-- ===== Попап этапа: полная информация ===== -->
    <PlansModal
      :open="!!stageModal"
      :title="stageModal ? `Этап ${stageModal.stage_code} · ${stageModal.name}` : ''"
      :subtitle="stageModal ? `${stageDone(stageModal.stage_code)} из ${stageTasks(stageModal.stage_code).length} заданий выполнено · статус: ${statusLabel(stageModal.status)}` : ''"
      @close="stageModal = null"
    >
      <div v-if="stageModal" class="sm-body">
        <p v-if="!stageTasks(stageModal.stage_code).length" class="sm-empty">
          Задания не сгенерированы.
          <NuxtLink :to="`/plans/process/${id}`" class="link">Открыть кокпит</NuxtLink> и нажать «Генерировать задания».
        </p>
        <div v-else class="sm-tasks">
          <div v-for="t in stageTasks(stageModal.stage_code)" :key="t.id" class="sm-task" :class="`st-${t.status}`">
            <div class="smt-head">
              <span class="badge" :class="t.task_role === 'approve' ? 'badge-accent' : 'badge-info'">{{ t.task_role === "approve" ? "согл." : "ввод" }}</span>
              <b class="smt-title">{{ t.title }}</b>
              <span class="badge badge-dot" :class="taskStTone(t.status)">{{ taskStLabel(t.status) }}</span>
            </div>
            <div class="smt-meta">
              <span class="smt-asg"><Icon name="lucide:user-round" /> отв.: {{ t.assignee_name || "не назначен" }}</span>
              <span v-if="t.delegate_name" class="smt-del"><Icon name="lucide:share" /> делегат: {{ t.delegate_name }}</span>
              <span v-if="filledBy(t)" class="smt-filled"><Icon name="lucide:check" /> заполнил: {{ filledBy(t) }}</span>
              <span v-if="t.cfo_count" class="smt-cfo">{{ t.cfo_count }} ЦФО</span>
              <span class="smt-form">{{ t.form_code }}</span>
            </div>
            <div class="smt-act">
              <NuxtLink v-if="t.form_code === 'TPL-MP'" :to="`/plans/mp-form/${t.id}`" class="btn btn-sm btn-ghost"><Icon name="lucide:pencil" /> Форма</NuxtLink>
            </div>
          </div>
        </div>
      </div>
      <template #footer>
        <NuxtLink :to="`/plans/process/${id}`" class="btn btn-primary"><Icon name="lucide:list-checks" /> В кокпит процесса</NuxtLink>
      </template>
    </PlansModal>
  </div>
</template>

<script setup lang="ts">
import { money, num } from "~/utils/format";
import PlansModal from "~/components/plans/PlansModal.vue";
import { usePlans, type StageState, type SvodRow } from "~/composables/usePlans";
import { useTasks, type Task, type PnlSummary, type PnlRow } from "~/composables/useTasks";

definePageMeta({ middleware: "scope-guard" });

const route = useRoute();
const id = Number(route.params.id);
const year = ref(Number(route.query.year) || 2026);
const month = ref(Number(route.query.month) || 6);

const { stages, stageAction, mpSvod, addComment } = usePlans();
const { byInstance, pnl: loadPnl, importStrategy } = useTasks();
const { hasRole, isAdmin } = useScope();
const canAdmin = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const pnl = ref<PnlSummary | null>(null);
const pnlNote = ref("");
const stratInput = ref<HTMLInputElement | null>(null);
const pf = (v: number | null) => (v === null || v === undefined ? "—" : num(v, 0));
const deltaTS = (r: PnlRow) => (r.tactic != null && r.strategy != null ? num(r.tactic - r.strategy, 0) : "—");
const deltaCls = (r: PnlRow) => {
  if (r.tactic == null || r.strategy == null) return "";
  const d = r.tactic - r.strategy;
  return d > 0 ? "d-pos" : d < 0 ? "d-neg" : "";
};
const openPnl = async () => {
  tab.value = "pnl";
  try { pnl.value = await loadPnl(id); } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка свода"; }
};
const doStrategyImport = async (ev: Event) => {
  const f = (ev.target as HTMLInputElement).files?.[0];
  if (!f) return;
  pnlNote.value = ""; error.value = "";
  try {
    const r = await importStrategy(id, f);
    pnlNote.value = `Импортировано стратегии: ${r.imported} строк`;
    pnl.value = await loadPnl(id);
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка импорта стратегии"; }
  finally { if (stratInput.value) stratInput.value.value = ""; }
};

const list = ref<StageState[]>([]);
const tasks = ref<Task[]>([]);
const svod = ref<SvodRow[]>([]);
const error = ref("");
const busy = ref(false);
const tab = ref<"process" | "pnl" | "svod">("process");
const ret = reactive({ open: false, from: "", target: "", reason: "" });
const stageModal = ref<StageState | null>(null);

// Реальные задания этапа (из движка), а не статичные имена маршрута.
const TASK_ST: Record<string, { l: string; t: string }> = {
  pending: { l: "не начато", t: "badge-dot" }, in_progress: { l: "в работе", t: "badge-info" },
  review: { l: "на проверке", t: "badge-warn" }, done: { l: "сделано", t: "badge-pos" }, returned: { l: "возвращено", t: "badge-neg" }
};
const taskStLabel = (s: string) => TASK_ST[s]?.l ?? s;
const taskStTone = (s: string) => TASK_ST[s]?.t ?? "badge-dot";
const stageTasks = (code: string) => tasks.value.filter((t) => t.stage_code === code);
const stageAssignees = (code: string) => [...new Set(stageTasks(code).map((t) => t.assignee_name).filter(Boolean))];
const stageDone = (code: string) => stageTasks(code).filter((t) => t.status === "done").length;
const peopleLabel = (code: string) => {
  const a = stageAssignees(code);
  if (!a.length) return "исполнители не назначены";
  return a.slice(0, 3).join(", ") + (a.length > 3 ? ` +${a.length - 3}` : "");
};
// Кто реально заполнил: делегат (если делегировано) или исполнитель — для сдано/проверки/готово.
const filledBy = (t: Task) => (["review", "done"].includes(t.status) ? (t.delegate_name || t.assignee_name) : "");

const COLS = [
  { key: "prev25", label: "25-е число", sub: "месяц М−1", codes: ["2.1", "2.2"] },
  { key: "rd2", label: "2-й р.д.", sub: "заполнение", codes: ["1.1"] },
  { key: "rd3", label: "3-й р.д.", sub: "согласование", codes: ["1.2", "1.3", "2.3"] },
  { key: "rd4", label: "4-й р.д.", sub: "финал/свод", codes: ["1.4", "2.4"] },
  { key: "rd5", label: "5-й р.д.", sub: "расходы/ЮЛ", codes: ["1.5", "1.6"] },
  { key: "rd6", label: "6-й р.д.", sub: "утверждение", codes: ["3", "4"] }
];
const TRACKS = [
  { key: "sales", label: "Поток продаж", codes: ["1.1", "1.2", "1.3", "1.4", "1.5", "1.6"] },
  { key: "production", label: "Поток производства", codes: ["2.1", "2.2", "2.3", "2.4"] },
  { key: "final", label: "Финал — ЮЛ", codes: ["3", "4"] }
];
const APPROVAL = new Set(["1.2", "1.3", "1.4", "2.2", "3", "4"]);

const byCode = computed<Record<string, StageState>>(() =>
  Object.fromEntries(list.value.map((s) => [s.stage_code, s]))
);
const done = computed(() => list.value.filter((s) => s.status === "completed").length);
const pct = computed(() => (list.value.length ? Math.round((done.value / list.value.length) * 100) : 0));

type Col = (typeof COLS)[number];
type Track = (typeof TRACKS)[number];

const cellStages = (tr: Track, c: Col): StageState[] =>
  tr.codes
    .filter((code) => c.codes.includes(code) && byCode.value[code])
    .map((code) => byCode.value[code]);

const colDue = (c: Col): string => {
  for (const code of c.codes) {
    const s = byCode.value[code];
    if (s?.due_at) return s.due_at;
  }
  return "";
};

const isApproval = (code: string) => APPROVAL.has(code);
const canReturn = (st: StageState) => st.status !== "pending";

const depHint = (st: StageState): string => {
  const unmet = (st.depends_on || []).filter((d) => byCode.value[d]?.status !== "completed");
  return unmet.length ? "ждёт " + unmet.join(", ") : "";
};

const statusLabel = (s: string) =>
  ({ pending: "ожидает", in_progress: "в работе", completed: "завершён", returned: "возвращён", blocked: "блок" } as Record<string, string>)[s] || s;
const statusBadge = (s: string) =>
  ({ completed: "badge-pos", in_progress: "badge-accent", returned: "badge-neg", blocked: "badge-warn", pending: "badge-dot" } as Record<string, string>)[s] || "badge-dot";

const returnTargets = (from: string) => list.value.filter((s) => s.stage_code !== from).map((s) => ({ code: s.stage_code, name: s.name }));

// Формы/задания этапа — теперь через движок процесса (task-driven), а не старую
// segment-форму. Кнопка ведёт в кокпит процесса с заданиями этапа.
const stageForms = (code: string): Array<{ label: string; to: string }> => {
  if (code === "1.1") {
    return [{ label: "Задания этапа", to: `/plans/process/${id}` }];
  }
  return [];
};

const load = async () => {
  error.value = "";
  try {
    list.value = await stages(id, year.value, month.value);
    [svod.value, tasks.value] = await Promise.all([
      mpSvod(year.value, month.value),
      byInstance(id).catch(() => [])
    ]);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки";
  }
};

const doAct = async (code: string, action: string) => {
  error.value = "";
  busy.value = true;
  try {
    list.value = await stageAction(id, code, { action, year: year.value, month: month.value });
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка перехода";
  } finally {
    busy.value = false;
  }
};

const openReturn = (code: string) => {
  ret.from = code;
  ret.target = "";
  ret.reason = "";
  ret.open = true;
};

const submitReturn = async () => {
  if (!ret.target) return;
  busy.value = true;
  error.value = "";
  try {
    list.value = await stageAction(id, ret.from, { action: "return", target: ret.target, year: year.value, month: month.value });
    if (ret.reason.trim()) {
      await addComment(id, `Возврат ${ret.from} → ${ret.target}: ${ret.reason}`, `stage:${ret.from}`);
    }
    ret.open = false;
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка возврата";
  } finally {
    busy.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.progress-row {
  display: flex;
  align-items: center;
  gap: var(--sp-5);
  margin-bottom: var(--sp-6);
}
.progress-bar {
  flex: 1;
  height: 8px;
  background: var(--bg-surface-3);
  border-radius: 999px;
  overflow: hidden;
}
.progress-fill {
  height: 100%;
  background: var(--accent);
  transition: width 0.3s ease;
}
.progress-label {
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  white-space: nowrap;
}
.segmented {
  display: inline-flex;
  gap: 2px;
  background: var(--bg-surface-3);
  border-radius: var(--rd-4, 6px);
  padding: 2px;
  margin-bottom: var(--sp-5);
}
.seg {
  border: 0;
  background: transparent;
  padding: 4px 14px;
  border-radius: var(--rd-3, 4px);
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  cursor: pointer;
}
.seg.active {
  background: var(--bg-surface);
  color: var(--text-strong);
  box-shadow: var(--shadow-pop);
}
.banner {
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-4, 6px);
  margin-bottom: var(--sp-5);
  font-size: var(--fs-sm);
}
.banner-neg {
  background: var(--neg-soft);
  color: var(--neg-strong);
}

/* ── таймлайн ── */
.timeline {
  padding: var(--sp-5);
  --tl-track: 150px;
}
.tl-scroll {
  overflow-x: auto;
}
.tl-grid {
  display: grid;
  gap: var(--sp-3);
  min-width: 920px;
}
.tl-corner {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  align-self: end;
  padding-bottom: var(--sp-3);
}
.tl-colhead {
  display: flex;
  flex-direction: column;
  gap: 1px;
  padding: var(--sp-3) var(--sp-4);
  border-bottom: 2px solid var(--border-strong);
}
.tl-col-label {
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
  font-size: var(--fs-sm);
}
.tl-col-sub {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  text-transform: uppercase;
}
.tl-col-date {
  font-family: var(--font-mono);
  font-size: var(--fs-2xs);
  color: var(--text-secondary);
  margin-top: 2px;
}
.tl-track {
  display: flex;
  align-items: center;
  font-weight: var(--fw-semibold);
  font-size: var(--fs-sm);
  color: var(--text-inverse);
  padding: 0 var(--sp-4);
  border-radius: var(--rd-4, 6px);
}
.track-sales {
  background: #312e81;
}
.track-production {
  background: #0b5cad;
}
.track-final {
  background: #6b3410;
}
.tl-cell {
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
  background: var(--bg-surface-2);
  border-radius: var(--rd-4, 6px);
  padding: var(--sp-3);
  min-height: 56px;
}
.stage-card {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-left: 3px solid var(--border-strong);
  border-radius: var(--rd-4, 6px);
  padding: var(--sp-4);
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
}
.stage-card.s-completed {
  border-left-color: var(--pos);
}
.stage-card.s-in_progress {
  border-left-color: var(--accent);
}
.stage-card.s-returned {
  border-left-color: var(--neg);
}
.stage-card.s-blocked {
  border-left-color: var(--warn);
}
.sc-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
}
.sc-code {
  font-family: var(--font-mono);
  font-size: var(--fs-xs);
  font-weight: var(--fw-semibold);
  color: var(--text-secondary);
}
.sc-name {
  font-size: var(--fs-sm);
  color: var(--text-strong);
  line-height: var(--lh-tight);
}
.sc-tasks {
  display: flex;
  flex-direction: column;
  gap: 2px;
  align-items: flex-start;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  background: var(--bg-surface-2);
  padding: 4px var(--sp-3);
  cursor: pointer;
  width: 100%;
  text-align: left;
}
.sc-tasks:hover { border-color: var(--accent); }
.sct-count {
  font-size: var(--fs-2xs);
  font-family: var(--font-mono);
  color: var(--text-secondary);
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.sct-count.ok { color: var(--pos-strong); }
.sct-people {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* попап этапа */
.sm-body { display: flex; flex-direction: column; gap: var(--sp-3); }
.sm-empty { color: var(--text-muted); padding: var(--sp-4); text-align: center; }
.sm-tasks { display: flex; flex-direction: column; gap: var(--sp-3); max-height: 60vh; overflow-y: auto; }
.sm-task {
  border: 1px solid var(--border);
  border-left: 3px solid var(--border-strong);
  border-radius: var(--rd-4, 6px);
  padding: var(--sp-3) var(--sp-4);
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
}
.sm-task.st-in_progress { border-left-color: var(--info); }
.sm-task.st-review { border-left-color: var(--warn); }
.sm-task.st-done { border-left-color: var(--pos-strong); }
.sm-task.st-returned { border-left-color: var(--neg-strong); }
.smt-head { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: wrap; }
.smt-title { font-size: var(--fs-sm); }
.smt-meta { display: flex; flex-wrap: wrap; gap: var(--sp-3); font-size: var(--fs-2xs); color: var(--text-secondary); }
.smt-meta > span { display: inline-flex; align-items: center; gap: 3px; }
.smt-filled { color: var(--pos-strong); }
.smt-form { font-family: var(--font-mono); color: var(--text-muted); }
.smt-act { display: flex; gap: var(--sp-2); }
.link { color: var(--accent); }
.sc-meta {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  display: flex;
  align-items: center;
  gap: 4px;
}
.sc-dep {
  color: var(--warn);
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.sc-due {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.sc-actions {
  display: flex;
  gap: var(--sp-3);
}
.sc-forms {
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-3);
  padding-top: var(--sp-2);
  border-top: 1px dashed var(--border);
}
.sc-formlink {
  font-size: var(--fs-2xs);
  color: var(--accent);
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.tl-legend {
  display: flex;
  gap: var(--sp-6);
  margin-top: var(--sp-5);
  font-size: var(--fs-xs);
  color: var(--text-secondary);
}
.tl-legend .dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 999px;
  margin-right: 4px;
  background: var(--border-strong);
}
.dot.s-in_progress {
  background: var(--accent);
}
.dot.s-completed {
  background: var(--pos);
}
.dot.s-returned {
  background: var(--neg);
}

/* ── свод ── */
.empty-cell {
  text-align: center;
  color: var(--text-muted);
  padding: var(--sp-7) 0;
}
.le-cell {
  font-weight: var(--fw-medium);
}
.strong {
  font-weight: var(--fw-semibold);
}
.prov-note {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  margin-top: var(--sp-4);
}
.pnl-head { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-4); flex-wrap: wrap; }
.pnl-actions { display: flex; align-items: center; gap: var(--sp-3); }
.pnl-prev { font-size: var(--fs-xs); color: var(--text-muted); font-family: var(--font-mono); }
.hidden-file { display: none; }
.banner-pos { background: var(--pos-soft); color: var(--pos-strong); }
.pnl-table .num { text-align: right; font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.pnl-table .exp { color: var(--text-secondary); }
.pnl-table .strat { color: var(--accent); }
.pnl-table .tac { font-weight: var(--fw-semibold); }
.pnl-table .prev { color: var(--text-muted); }
.pnl-table tr.adj .tac { background: var(--warn-soft); }
.pnl-table .d-pos { color: var(--pos-strong); }
.pnl-table .d-neg { color: var(--neg-strong); }

/* ── модалка поля ── */
.field {
  display: block;
  margin-bottom: var(--sp-5);
}
.field-label {
  display: block;
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  margin-bottom: var(--sp-3);
}
.textarea {
  width: 100%;
  resize: vertical;
  font-family: var(--font-sans);
}
</style>
