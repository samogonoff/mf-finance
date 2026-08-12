<!--
  Кокпит процесса периода: задания по этапам, владелец этапа двигает этап.
  Генерация заданий из шаблонов (админ). На этапе: готовность (done/total),
  владелец (финансист), «Двигать этап» (когда все задания done). Делегирование
  2-ступ. по заданию.
-->
<template>
  <div class="page-cockpit">
    <header class="page-header">
      <div>
        <h1 class="page-title">Процесс · {{ periodLabel }}</h1>
        <p class="page-subtitle">Задания по этапам · владелец двигает этап</p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="`/plans/${plId}`" class="btn btn-ghost"><Icon name="lucide:arrow-left" /> Карточка PL</NuxtLink>
        <button v-if="canAdmin" class="btn btn-sm btn-primary" :disabled="busy" @click="regen">
          <Icon name="lucide:refresh-cw" :class="{ spin: busy }" /> Генерировать задания
        </button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>
    <p v-if="!tasks.length && !error" class="empty-state">Заданий нет. {{ canAdmin ? "Нажмите «Генерировать задания»." : "" }}</p>

    <div v-for="stage in stages" :key="stage" class="card stage">
      <div class="s-head">
        <div class="s-id">
          <span class="s-code">Этап {{ stage }}</span>
          <span class="s-ready badge" :class="readyOf(stage) ? 'badge-pos' : 'badge-warn'">{{ doneOf(stage) }}/{{ totalOf(stage) }} готово</span>
        </div>
        <div class="s-owner">
          <span class="so-lbl">Владелец:</span>
          <span v-if="ownerName(stage)" class="so-name">{{ ownerName(stage) }}<button v-if="canAdmin" class="ac-del" @click="setOwner(stage, 0)"><Icon name="lucide:x" /></button></span>
          <ClientOnly v-else-if="canAdmin"><UserPicker placeholder="назначить…" @picked="(u) => setOwner(stage, u.id)" /></ClientOnly>
          <span v-else class="so-none">—</span>
          <button v-if="canAdmin && readyOf(stage)" class="btn btn-sm btn-primary" @click="advance(stage)"><Icon name="lucide:chevrons-right" /> Двигать этап</button>
        </div>
      </div>

      <div class="tasks">
        <div v-for="t in tasksOf(stage)" :key="t.id" class="task" :class="`st-${t.status}`">
          <div class="t-main">
            <div class="t-head">
              <span class="badge" :class="t.task_role === 'approve' ? 'badge-accent' : 'badge-info'">{{ t.task_role === "approve" ? "согл." : "ввод" }}</span>
              <b>{{ t.title }}</b>
              <span class="assignee-chip" :class="{ none: !t.assignee_name }">
                <Icon name="lucide:user-round" /> {{ t.assignee_name || "не назначен" }}
                <button v-if="canAdmin" class="chip-edit" title="Переназначить исполнителя" @click="assignTask = t"><Icon name="lucide:pencil" /></button>
              </span>
              <span v-if="t.delegate_name" class="assignee-chip del"><Icon name="lucide:share" /> {{ t.delegate_name }}</span>
              <span class="t-form">{{ t.form_code }}</span>
            </div>
            <div class="t-meta">
              <span v-if="t.cfo_count">{{ t.cfo_count }} ЦФО</span>
            </div>
          </div>
          <div class="t-side">
            <span class="badge badge-dot" :class="statusTone(t.status)">{{ statusLabel(t.status) }}</span>
            <div class="t-actions">
              <NuxtLink v-if="t.form_code === 'TPL-MP'" :to="`/plans/mp-form/${t.id}`" class="btn btn-sm btn-ghost"><Icon name="lucide:pencil" /> Форма</NuxtLink>
              <button class="btn btn-sm btn-ghost" @click="openData(t)"><Icon name="lucide:table" /> Данные</button>
              <button v-for="a in actionsFor(t)" :key="a.act" class="btn btn-sm" :class="a.primary ? 'btn-primary' : 'btn-ghost'" @click="run(t, a.act)">{{ a.label }}</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- МОДАЛ: данные задания (read-only review) -->
    <div v-if="dataModal" class="modal-overlay" @click.self="dataModal = null">
      <div class="modal modal-data">
        <div class="dm-head">
          <h3>Данные · {{ dataModal.task.title }}</h3>
          <button class="btn btn-icon btn-sm" @click="dataModal = null"><Icon name="lucide:x" /></button>
        </div>
        <p class="dm-sub">{{ dataModal.task.form_code }} · {{ dataModal.task.cfo_count }} ЦФО · факт/стратегия read-only, тактика — ввод</p>
        <p v-if="!dataModal.has_data" class="empty-state">Данных пока нет — форма ещё не заполнена.</p>
        <div v-else class="table-wrap">
          <table class="data-table">
            <thead><tr><th>ЦФО</th><th>Статья</th><th class="num">Факт</th><th class="num">Стратегия</th><th class="num">Тактика</th><th class="num">Расчёт</th></tr></thead>
            <tbody>
              <tr v-for="(r, i) in dataModal.rows" :key="i" :class="{ adj: r.is_manual }">
                <td>{{ r.name_cfo || r.code_cfo }}</td>
                <td>{{ r.expense_name || r.line_code }}</td>
                <td class="num">{{ fmt(r.fact) }}</td>
                <td class="num">{{ fmt(r.strategy) }}</td>
                <td class="num tac">
                  {{ fmt(r.tactic) }}
                  <span v-if="r.is_manual" class="adj-mark" :title="diffTitle(r)"><Icon name="lucide:pencil" /></span>
                </td>
                <td class="num">{{ fmt(r.calc) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="dm-legend"><span class="adj-mark"><Icon name="lucide:pencil" /></span> — ручная корректировка (наведите для diff «было → стало» и причины)</p>
      </div>
    </div>

    <!-- МОДАЛ: переназначить исполнителя (админ) -->
    <div v-if="assignTask" class="modal-overlay" @click.self="assignTask = null">
      <div class="modal">
        <h3>Исполнитель · {{ assignTask.title }}</h3>
        <div class="modal-body">
          <p class="hint">Текущий: <b>{{ assignTask.assignee_name || "не назначен" }}</b>. По умолчанию исполнитель = ТОП ЦФО; здесь можно переопределить (например, для заданий «без ТОПа»).</p>
          <ClientOnly><UserPicker placeholder="Фамилия исполнителя…" @picked="onAssign" /></ClientOnly>
        </div>
        <div class="modal-foot">
          <button v-if="assignTask.assignee_user_id" class="btn btn-ghost" @click="onAssign({ id: 0, name: '' })">Снять исполнителя</button>
          <button class="btn btn-ghost" @click="assignTask = null">Отмена</button>
        </div>
      </div>
    </div>

    <div v-if="delegateTask" class="modal-overlay" @click.self="delegateTask = null">
      <div class="modal">
        <h3>Делегировать · {{ delegateTask.title }}</h3>
        <div class="modal-body">
          <p class="hint">Делегат выполнит и сдаст на проверку — исполнитель примет.</p>
          <ClientOnly><UserPicker placeholder="Фамилия делегата…" @picked="onDelegate" /></ClientOnly>
        </div>
        <div class="modal-foot"><button class="btn btn-ghost" @click="delegateTask = null">Отмена</button></div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useTasks, type Task, type TaskData, type TaskDataRow } from "~/composables/useTasks";
import { num } from "~/utils/format";
import { usePlans } from "~/composables/usePlans";
import UserPicker from "~/components/plans/UserPicker.vue";

definePageMeta({ middleware: "scope-guard" });

const route = useRoute();
const plId = Number(route.params.id);
const api = useTasks();
const { instances, stages: loadStageRows } = usePlans();
const { hasRole, isAdmin } = useScope();
const canAdmin = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const tasks = ref<Task[]>([]);
const owners = ref<{ stage_code: string; owner_user_id: number | null; owner_name: string }[]>([]);
const period = ref<{ year: number; month: number }>({ year: 0, month: 0 });
const error = ref("");
const note = ref("");
const busy = ref(false);
const delegateTask = ref<Task | null>(null);
const assignTask = ref<Task | null>(null);
const dataModal = ref<TaskData | null>(null);
const fmt = (v: number | null) => (v === null || v === undefined ? "—" : num(v, 0));
const diffTitle = (r: TaskDataRow) => r.original != null ? `было ${fmt(r.original)} → стало ${fmt(r.tactic)}${r.reason ? " · " + r.reason : ""}` : (r.reason || "корректировка");
const openData = async (t: Task) => {
  try { dataModal.value = await api.data(t.id); } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка данных"; }
};

const periodLabel = computed(() => period.value.year ? `${period.value.year}-${String(period.value.month).padStart(2, "0")}` : `#${plId}`);
const stages = computed(() => [...new Set(tasks.value.map((t) => t.stage_code))].sort());
const tasksOf = (s: string) => tasks.value.filter((t) => t.stage_code === s);
const totalOf = (s: string) => tasksOf(s).length;
const doneOf = (s: string) => tasksOf(s).filter((t) => t.status === "done").length;
const readyOf = (s: string) => totalOf(s) > 0 && doneOf(s) === totalOf(s);
const ownerName = (s: string) => owners.value.find((o) => o.stage_code === s)?.owner_name || "";

// Подписи состояний — из общего словаря (usePlanStatus).
const planStatus = usePlanStatus();
const statusLabel = (s: string) => planStatus.label(s);
const statusTone = (s: string) => planStatus.badge(s);

type Act = { act: string; label: string; primary?: boolean };
const actionsFor = (t: Task): Act[] => {
  switch (t.status) {
    case "pending": case "returned": return [{ act: "start", label: "Начать", primary: true }, { act: "delegate", label: "Делегировать" }];
    case "in_progress": return [{ act: "submit", label: "Сдать", primary: true }, { act: "delegate", label: "Делегировать" }];
    case "review": return [{ act: "accept", label: "Принять", primary: true }, { act: "return", label: "Вернуть" }];
    case "done": return [{ act: "reopen", label: "Переоткрыть" }];
    default: return [];
  }
};

const load = async () => {
  try {
    const inst = (await instances()).find((i) => i.id === plId);
    if (inst) {
      period.value = { year: inst.period_year, month: inst.period_month };
      // Инициализируем строки этапов (для владельцев), если ещё не созданы.
      await loadStageRows(plId, inst.period_year, inst.period_month, "RU").catch(() => {});
    }
    [tasks.value, owners.value] = await Promise.all([api.byInstance(plId), api.stageOwners(plId)]);
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка"; }
};
const errShow = (e: unknown) => { error.value = e instanceof Error ? e.message : "Ошибка"; };

const regen = async () => {
  busy.value = true; error.value = ""; note.value = "";
  try { const r = await api.generate(plId); note.value = `Сгенерировано заданий: ${r.generated}`; await load(); } catch (e) { errShow(e); } finally { busy.value = false; }
};
const setOwner = async (stage: string, userId: number) => {
  try { await api.setOwner(plId, stage, userId); owners.value = await api.stageOwners(plId); } catch (e) { errShow(e); }
};
const advance = async (stage: string) => {
  error.value = ""; note.value = "";
  try { await api.advanceStage(plId, stage, { action: "submit", year: period.value.year, month: period.value.month, country: "RU" }); note.value = `Этап ${stage} продвинут`; } catch (e) { errShow(e); }
};
const run = async (t: Task, act: string) => {
  if (act === "delegate") { delegateTask.value = t; return; }
  try { await api.action(t.id, act); await load(); } catch (e) { errShow(e); }
};
const onDelegate = async (u: { id: number; name: string }) => {
  if (!delegateTask.value) return;
  try { await api.action(delegateTask.value.id, "delegate", u.id); delegateTask.value = null; await load(); } catch (e) { errShow(e); }
};
const onAssign = async (u: { id: number; name: string }) => {
  if (!assignTask.value) return;
  try { await api.setAssignee(assignTask.value.id, u.id); assignTask.value = null; await load(); } catch (e) { errShow(e); }
};

onMounted(load);
</script>

<style scoped>
.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-4, 6px); margin-bottom: var(--sp-5); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.banner-pos { background: var(--pos-soft); color: var(--pos-strong); }
.page-actions { display: flex; gap: var(--sp-3); }
.empty-state { color: var(--text-muted); padding: var(--sp-7); text-align: center; }

.stage { padding: var(--sp-4) var(--sp-5); margin-bottom: var(--sp-4); }
.s-head { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-4); margin-bottom: var(--sp-3); flex-wrap: wrap; }
.s-id { display: flex; align-items: center; gap: var(--sp-3); }
.s-code { font-family: var(--font-mono); font-weight: var(--fw-bold); color: var(--accent); }
.s-owner { display: flex; align-items: center; gap: var(--sp-3); font-size: var(--fs-sm); }
.so-lbl { color: var(--text-muted); font-size: var(--fs-xs); }
.so-name { display: inline-flex; align-items: center; gap: 4px; }
.so-none { color: var(--text-muted); }
.ac-del { border: none; background: none; cursor: pointer; color: var(--text-muted); display: inline-flex; }
.ac-del:hover { color: var(--neg-strong); }

.tasks { display: flex; flex-direction: column; gap: var(--sp-2); }
.task { display: flex; justify-content: space-between; align-items: center; gap: var(--sp-4); padding: var(--sp-3) var(--sp-4); border-radius: var(--rd-3); border-left: 3px solid var(--border); background: var(--bg-tonal); }
.task.st-in_progress { border-left-color: var(--info); }
.task.st-review { border-left-color: var(--warn); }
.task.st-done { border-left-color: var(--pos-strong); }
.task.st-returned { border-left-color: var(--neg-strong); }
.t-head { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: wrap; }
.t-form { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.t-meta { display: flex; flex-wrap: wrap; gap: var(--sp-3); margin-top: 2px; font-size: var(--fs-2xs); color: var(--text-secondary); }
.assignee-chip { display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-2xs); background: var(--accent-soft); color: var(--accent); border-radius: var(--rd-3); padding: 2px var(--sp-2); }
.assignee-chip.none { background: var(--warn-soft); color: var(--warn); }
.assignee-chip.del { background: var(--bg-tonal); color: var(--text-secondary); }
.chip-edit { border: none; background: none; cursor: pointer; color: inherit; display: inline-flex; padding: 0 0 0 2px; opacity: 0.7; }
.chip-edit:hover { opacity: 1; }
.t-side { display: flex; flex-direction: column; align-items: flex-end; gap: var(--sp-2); flex-shrink: 0; }
.t-actions { display: flex; gap: var(--sp-2); }
.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.32); display: flex; align-items: center; justify-content: center; z-index: 60; }
.modal { width: 440px; max-width: 94vw; background: var(--bg-surface); border-radius: var(--rd-5, 10px); box-shadow: 0 20px 60px rgba(0,0,0,0.25); padding: var(--sp-5); }
.modal h3 { margin: 0 0 var(--sp-4); font-size: var(--fs-md); }
.modal-body { display: flex; flex-direction: column; gap: var(--sp-3); }
.hint { color: var(--text-secondary); font-size: var(--fs-sm); margin: 0; }
.modal-foot { display: flex; justify-content: flex-end; margin-top: var(--sp-5); }
.modal-data { width: 760px; max-width: 96vw; }
.dm-head { display: flex; align-items: center; justify-content: space-between; }
.dm-sub { color: var(--text-muted); font-size: var(--fs-xs); margin: 0 0 var(--sp-3); }
.modal-data .table-wrap { max-height: 56vh; overflow: auto; }
.modal-data .num { text-align: right; font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.modal-data tr.adj { background: var(--warn-soft); }
.modal-data .tac { position: relative; }
.adj-mark { color: var(--warn); display: inline-flex; cursor: help; }
.dm-legend { margin-top: var(--sp-3); font-size: var(--fs-2xs); color: var(--text-muted); display: flex; align-items: center; gap: 4px; }
.empty-state { color: var(--text-muted); padding: var(--sp-6); text-align: center; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
