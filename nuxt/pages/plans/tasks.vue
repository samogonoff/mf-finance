<!--
  Задания процесса. Обычный пользователь видит «Мои» (я исполнитель/делегат).
  Админ / админ планов может переключиться на «Все задания» всех карточек
  (сгруппированы по периоду карточки → этапу, со ссылкой в кокпит).
  Действия по статусу: Начать / Делегировать / Сдать / Принять / Вернуть.
-->
<template>
  <div class="page-tasks">
    <header class="page-header">
      <div>
        <h1 class="page-title">{{ view === "all" ? "Все задания" : "Мои задания" }}</h1>
        <p class="page-subtitle">{{ view === "all" ? "Задания всех карточек (админ)" : "Задания, где вы исполнитель или делегат" }}</p>
      </div>
      <div class="page-actions">
        <div v-if="canAll" class="segmented">
          <button class="seg" :class="{ active: view === 'mine' }" @click="switchView('mine')">Мои</button>
          <button class="seg" :class="{ active: view === 'all' }" @click="switchView('all')">Все</button>
        </div>
        <NuxtLink to="/plans" class="btn btn-ghost"><Icon name="lucide:arrow-left" /> К модулю</NuxtLink>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="!tasks.length && !error" class="empty-state">Заданий нет.</p>

    <!-- РЕЖИМ «ВСЕ»: группировка по карточке (периоду) → этапу -->
    <template v-if="view === 'all'">
      <div v-for="card in cards" :key="card.plId" class="card-group">
        <div class="cg-head">
          <h2 class="cg-title">Карточка {{ card.label }}</h2>
          <span class="cg-stat">{{ card.done }}/{{ card.total }} done</span>
          <NuxtLink :to="`/plans/process/${card.plId}`" class="btn btn-sm btn-ghost"><Icon name="lucide:external-link" /> В кокпит</NuxtLink>
        </div>
        <div v-for="t in card.tasks" :key="t.id" class="card task" :class="`st-${t.status}`">
          <component :is="'div'" class="t-row">
            <div class="t-main">
              <div class="t-head">
                <span class="badge badge-dot st-badge" :class="statusTone(t.status)">{{ t.stage_code }}</span>
                <span class="badge" :class="t.task_role === 'approve' ? 'badge-accent' : 'badge-info'">{{ t.task_role === "approve" ? "согл." : "ввод" }}</span>
                <b class="t-title">{{ t.title }}</b>
                <span class="assignee-chip" :class="{ none: !t.assignee_name }"><Icon name="lucide:user-round" /> {{ t.assignee_name || "не назначен" }}</span>
                <span v-if="t.delegate_name" class="assignee-chip del"><Icon name="lucide:share" /> {{ t.delegate_name }}</span>
                <span class="t-form">{{ t.form_code }}</span>
              </div>
              <div class="t-meta">
                <span v-if="t.cfo_count">{{ t.cfo_count }} ЦФО</span>
                <span>исп.: {{ t.assignee_name || "—" }}</span>
                <span v-if="t.delegate_name">делегат: {{ t.delegate_name }}</span>
              </div>
            </div>
            <span class="badge badge-dot" :class="statusTone(t.status)">{{ statusLabel(t.status) }}</span>
          </component>
        </div>
      </div>
    </template>

    <!-- РЕЖИМ «МОИ»: группировка по этапу + действия -->
    <template v-else>
      <div v-for="(group, stage) in byStage" :key="stage" class="stage-group">
        <h2 class="sg-title">Этап {{ stage }}</h2>
        <div v-for="t in group" :key="t.id" class="card task" :class="`st-${t.status}`">
          <div class="t-main">
            <div class="t-head">
              <span class="badge" :class="t.task_role === 'approve' ? 'badge-accent' : 'badge-info'">{{ t.task_role === "approve" ? "согласование" : "заполнение" }}</span>
              <b class="t-title">{{ t.title }}</b>
              <span class="t-form">{{ t.form_code }}</span>
            </div>
            <div class="t-meta">
              <span v-if="t.cfo_count">{{ t.cfo_count }} ЦФО</span>
              <span>исполнитель: {{ t.assignee_name || "—" }}</span>
              <span v-if="t.delegate_name">делегат: {{ t.delegate_name }}</span>
            </div>
          </div>
          <div class="t-side">
            <span class="badge badge-dot" :class="statusTone(t.status)">{{ statusLabel(t.status) }}</span>
            <div class="t-actions">
              <NuxtLink v-if="t.form_code === 'TPL-MP'" :to="`/plans/mp-form/${t.id}`" class="btn btn-sm btn-ghost"><Icon name="lucide:pencil" /> Форма</NuxtLink>
              <button v-for="a in actionsFor(t)" :key="a.act" class="btn btn-sm" :class="a.primary ? 'btn-primary' : 'btn-ghost'" @click="run(t, a.act)">{{ a.label }}</button>
            </div>
          </div>
        </div>
      </div>
    </template>

    <div v-if="delegateTask" class="modal-overlay" @click.self="delegateTask = null">
      <div class="modal">
        <h3>Делегировать · {{ delegateTask.title }}</h3>
        <div class="modal-body">
          <p class="hint">Делегат выполнит и сдаст на проверку — вы примете.</p>
          <ClientOnly><UserPicker placeholder="Фамилия делегата…" @picked="onDelegate" /></ClientOnly>
        </div>
        <div class="modal-foot"><button class="btn btn-ghost" @click="delegateTask = null">Отмена</button></div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useTasks, type Task } from "~/composables/useTasks";
import UserPicker from "~/components/plans/UserPicker.vue";

definePageMeta({ middleware: "scope-guard" });

const api = useTasks();
const { hasRole, isAdmin } = useScope();
const canAll = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const view = ref<"mine" | "all">("mine");
const tasks = ref<Task[]>([]);
const error = ref("");
const delegateTask = ref<Task | null>(null);

const byStage = computed(() => {
  const m: Record<string, Task[]> = {};
  for (const t of tasks.value) (m[t.stage_code] ??= []).push(t);
  return m;
});

// Группировка «Все» по карточке (периоду).
const cards = computed(() => {
  const m = new Map<number, { plId: number; label: string; tasks: Task[]; done: number; total: number }>();
  for (const t of tasks.value) {
    if (!m.has(t.pl_id)) {
      const label = t.year ? `${t.year}-${String(t.month).padStart(2, "0")}` : `#${t.pl_id}`;
      m.set(t.pl_id, { plId: t.pl_id, label, tasks: [], done: 0, total: 0 });
    }
    const c = m.get(t.pl_id)!;
    c.tasks.push(t); c.total++; if (t.status === "done") c.done++;
  }
  return [...m.values()];
});

const STATUS: Record<string, { l: string; t: string }> = {
  pending: { l: "не начато", t: "" }, in_progress: { l: "в работе", t: "badge-info" },
  review: { l: "на проверке", t: "badge-warn" }, done: { l: "сделано", t: "badge-pos" }, returned: { l: "возвращено", t: "badge-neg" }
};
const statusLabel = (s: string) => STATUS[s]?.l ?? s;
const statusTone = (s: string) => STATUS[s]?.t ?? "";

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
  try { tasks.value = view.value === "all" ? await api.all() : await api.mine(); }
  catch (e) { error.value = e instanceof Error ? e.message : "Ошибка"; }
};
const switchView = (v: "mine" | "all") => { view.value = v; load(); };
const run = async (t: Task, act: string) => {
  if (act === "delegate") { delegateTask.value = t; return; }
  try { await api.action(t.id, act); await load(); } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка действия"; }
};
const onDelegate = async (u: { id: number; name: string }) => {
  if (!delegateTask.value) return;
  try { await api.action(delegateTask.value.id, "delegate", u.id); delegateTask.value = null; await load(); } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка делегирования"; }
};

onMounted(() => { if (canAll.value) view.value = "all"; load(); });
</script>

<style scoped>
.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-4, 6px); margin-bottom: var(--sp-5); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.empty-state { color: var(--text-muted); padding: var(--sp-7); text-align: center; }
.page-actions { display: flex; align-items: center; gap: var(--sp-3); }
.segmented { display: inline-flex; border: 1px solid var(--border); border-radius: var(--rd-4); overflow: hidden; }
.seg { border: none; background: var(--bg-surface); color: var(--text-secondary); padding: 5px 14px; cursor: pointer; font-size: var(--fs-sm); }
.seg.active { background: var(--accent-soft); color: var(--accent); font-weight: var(--fw-semibold); }

.card-group { margin-bottom: var(--sp-5); }
.cg-head { display: flex; align-items: center; gap: var(--sp-3); margin-bottom: var(--sp-3); }
.cg-title { font-size: var(--fs-sm); color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.03em; margin: 0; }
.cg-stat { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.cg-head .btn { margin-left: auto; }

.stage-group { margin-bottom: var(--sp-5); }
.sg-title { font-size: var(--fs-sm); color: var(--text-secondary); margin-bottom: var(--sp-3); text-transform: uppercase; letter-spacing: 0.03em; }
.task { display: flex; justify-content: space-between; align-items: center; gap: var(--sp-4); padding: var(--sp-3) var(--sp-5); margin-bottom: var(--sp-2); border-left: 3px solid var(--border); }
.task.st-in_progress { border-left-color: var(--info); }
.task.st-review { border-left-color: var(--warn); }
.task.st-done { border-left-color: var(--pos-strong); }
.task.st-returned { border-left-color: var(--neg-strong); }
.t-row { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-4); width: 100%; }
.t-main { min-width: 0; }
.t-head { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: wrap; }
.st-badge { font-family: var(--font-mono); }
.t-title { font-size: var(--fs-md); }
.t-form { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.assignee-chip { display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-2xs); background: var(--accent-soft); color: var(--accent); border-radius: var(--rd-3); padding: 2px var(--sp-2); }
.assignee-chip.none { background: var(--warn-soft); color: var(--warn); }
.assignee-chip.del { background: var(--bg-tonal); color: var(--text-secondary); }
.t-meta { display: flex; flex-wrap: wrap; gap: var(--sp-4); margin-top: var(--sp-2); font-size: var(--fs-xs); color: var(--text-secondary); }
.t-side { display: flex; flex-direction: column; align-items: flex-end; gap: var(--sp-2); flex-shrink: 0; }
.t-actions { display: flex; gap: var(--sp-2); }
.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.32); display: flex; align-items: center; justify-content: center; z-index: 60; }
.modal { width: 440px; max-width: 94vw; background: var(--bg-surface); border-radius: var(--rd-5, 10px); box-shadow: 0 20px 60px rgba(0,0,0,0.25); padding: var(--sp-5); }
.modal h3 { margin: 0 0 var(--sp-4); font-size: var(--fs-md); }
.modal-body { display: flex; flex-direction: column; gap: var(--sp-3); }
.hint { color: var(--text-secondary); font-size: var(--fs-sm); margin: 0; }
.modal-foot { display: flex; justify-content: flex-end; margin-top: var(--sp-5); }
</style>
