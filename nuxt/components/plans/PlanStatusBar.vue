<!--
  Состояние плана одной строкой: где период сейчас, до какого числа, что его
  тормозит и насколько он готов. Раньше это было размазано по трём экранам
  (прогресс — на карточке, статусы заданий — в кокпите, сроки не показывались
  вообще). Компонент один и ставится на все экраны периода.

  Готовность считается ДВОЙНАЯ: по заданиям (сколько принято) и по данным
  (сколько заданий реально имеет введённую тактику) — «12/18 заданий» без второго
  числа врёт: задание может висеть «в работе» с одной заполненной ячейкой.
-->
<template>
  <div class="status-bar" :class="{ alarm: overdue > 0 }">
    <div class="sb-main">
      <span class="sb-period">{{ periodLabel }}</span>
      <span class="sb-sep">·</span>
      <span v-if="current" class="sb-stage">
        Этап {{ current.stage_code }} <span class="sb-stage-name">{{ current.name }}</span>
      </span>
      <span v-else class="sb-stage muted">этапы не инициализированы</span>

      <template v-if="current?.due_at">
        <span class="sb-sep">·</span>
        <span class="sb-due" :class="dueClass">
          <Icon name="lucide:calendar" /> {{ current.due_at }}
          <b v-if="dueText">· {{ dueText }}</b>
        </span>
      </template>
    </div>

    <div class="sb-progress">
      <div class="pbar" :title="`Заданий принято: ${doneTasks} из ${totalTasks}`">
        <div class="pfill" :style="{ width: pctTasks + '%' }"></div>
      </div>
      <span class="pnum">{{ doneTasks }}/{{ totalTasks }} заданий</span>
      <span class="pnum sub">{{ filledTasks }}/{{ totalTasks }} с данными</span>
    </div>

    <div class="sb-flags">
      <span v-if="overdue > 0" class="flag flag-neg"><Icon name="lucide:alarm-clock" /> просрочено: {{ overdue }}</span>
      <span v-if="blocked > 0" class="flag flag-mut"><Icon name="lucide:lock" /> заблокировано: {{ blocked }}</span>
      <span v-if="unassigned > 0" class="flag flag-warn"><Icon name="lucide:user-x" /> без исполнителя: {{ unassigned }}</span>
      <span v-if="!overdue && !blocked && !unassigned && totalTasks" class="flag flag-pos"><Icon name="lucide:check" /> без блокеров</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { usePlans, type StageState } from "~/composables/usePlans";
import { useTasks, type Task } from "~/composables/useTasks";
import { toPlanState } from "~/composables/usePlanStatus";

const props = defineProps<{ plId: number; year: number; month: number; country?: string }>();

const { stages: loadStages } = usePlans();
const { byInstance } = useTasks();
const st = usePlanStatus();

const stages = ref<StageState[]>([]);
const tasks = ref<Task[]>([]);
/** Задания, где уже есть введённая тактика (заполнены, а не просто «в работе»). */
const filled = ref<Set<number>>(new Set());

const periodLabel = computed(() => `${props.year}-${String(props.month).padStart(2, "0")}`);

// Текущий этап: первый незавершённый по порядку кода.
const current = computed<StageState | null>(() => {
  const open = stages.value.filter((s) => toPlanState(s.status) !== "done");
  const sorted = [...open].sort((a, b) => a.stage_code.localeCompare(b.stage_code, undefined, { numeric: true }));
  return sorted[0] || stages.value[stages.value.length - 1] || null;
});

const dueText = computed(() => st.dueLabel(current.value?.due_at));
const dueClass = computed(() => {
  const n = st.daysLeft(current.value?.due_at);
  if (n === null) return "";
  return n < 0 ? "due-neg" : n <= 1 ? "due-warn" : "";
});

const totalTasks = computed(() => tasks.value.length);
const doneTasks = computed(() => tasks.value.filter((t) => t.status === "done").length);
const filledTasks = computed(() => tasks.value.filter((t) => filled.value.has(t.id)).length);
const pctTasks = computed(() => (totalTasks.value ? Math.round((doneTasks.value / totalTasks.value) * 100) : 0));
const unassigned = computed(() => tasks.value.filter((t) => !t.assignee_user_id && !t.delegate_user_id).length);
const blocked = computed(() => stages.value.filter((s) => st.meta(s.status).label === "заблокировано").length);
const overdue = computed(() => stages.value.filter((s) => st.isOverdue(s.due_at, s.status)).length);

const load = async () => {
  try {
    [stages.value, tasks.value] = await Promise.all([
      loadStages(props.plId, props.year, props.month, props.country || "RU").catch(() => []),
      byInstance(props.plId).catch(() => [])
    ]);
  } catch {
    // Статус-бар — вспомогательный: молча деградирует до пустого, не роняя экран.
  }
};

// «С данными» определяем по статусу: задание, которое хоть раз сохранили, уходит
// из pending (см. SaveMpForm). Отдельного запроса на каждую форму не делаем.
watch(tasks, (list) => {
  filled.value = new Set(list.filter((t) => t.status !== "pending").map((t) => t.id));
});

onMounted(load);
</script>

<style scoped>
.status-bar {
  display: flex; flex-wrap: wrap; align-items: center; gap: var(--sp-4);
  padding: var(--sp-3) var(--sp-4); margin-bottom: var(--sp-5);
  border: 1px solid var(--border); border-left: 3px solid var(--accent);
  border-radius: var(--rd-4, 6px); background: var(--bg-surface);
  font-size: var(--fs-sm);
}
.status-bar.alarm { border-left-color: var(--neg-strong); }
.sb-main { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.sb-period { font-family: var(--font-mono); font-weight: var(--fw-bold); }
.sb-sep { color: var(--text-muted); }
.sb-stage-name { color: var(--text-secondary); }
.muted { color: var(--text-muted); }
.sb-due { display: inline-flex; align-items: center; gap: 4px; color: var(--text-secondary); font-family: var(--font-mono); font-size: var(--fs-xs); }
.sb-due.due-warn { color: var(--warn); }
.sb-due.due-neg { color: var(--neg-strong); }

.sb-progress { display: flex; align-items: center; gap: var(--sp-3); margin-left: auto; }
.pbar { width: 140px; height: 6px; background: var(--bg-tonal); border-radius: 999px; overflow: hidden; }
.pfill { height: 100%; background: var(--accent); }
.pnum { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-secondary); }
.pnum.sub { color: var(--text-muted); }

.sb-flags { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.flag { display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-2xs); padding: 1px 8px; border-radius: 999px; }
.flag-neg { background: var(--neg-soft); color: var(--neg-strong); }
.flag-warn { background: var(--warn-soft); color: var(--warn); }
.flag-pos { background: var(--pos-soft); color: var(--pos-strong); }
.flag-mut { background: var(--bg-tonal); color: var(--text-secondary); }
</style>
