<!--
  Рабочий стол модуля. Раньше здесь были метрики базы («экземпляров PL: 3,
  ячеек: 412») — они не отвечают на вопрос, с которым сюда заходят: «что сейчас
  от меня требуется». Теперь первым экраном идёт персональный чек-лист с прямыми
  действиями, ниже — периоды (вход в карточку) и админ-инструменты.
-->
<template>
  <div class="page-plans">
    <header class="page-header">
      <div>
        <h1 class="page-title">Тактические планы</h1>
        <p class="page-subtitle">Формирование и согласование тактических планов P&amp;L</p>
      </div>
      <div class="page-actions">
        <NuxtLink to="/plans/tasks" class="btn btn-ghost">
          <Icon name="lucide:list-checks" /> Все задания
        </NuxtLink>
        <NuxtLink to="/plans/directories" class="btn btn-ghost">
          <Icon name="lucide:book" /> Справочники
        </NuxtLink>
        <NuxtLink v-if="canAdmin" to="/plans/admin/users" class="btn btn-ghost">
          <Icon name="lucide:users" /> Пользователи и права
        </NuxtLink>
        <details v-if="canAdmin" class="more">
          <summary class="btn btn-ghost"><Icon name="lucide:settings" /> Настройки</summary>
          <div class="more-menu">
            <NuxtLink to="/plans/admin/route" class="more-item"><Icon name="lucide:git-branch" /> Маршрут</NuxtLink>
            <NuxtLink to="/plans/admin/positions" class="more-item"><Icon name="lucide:briefcase" /> Должности</NuxtLink>
            <NuxtLink to="/plans/admin/task-templates" class="more-item"><Icon name="lucide:wrench" /> Конструктор заданий</NuxtLink>
            <NuxtLink to="/plans/audit" class="more-item"><Icon name="lucide:scroll-text" /> Аудит</NuxtLink>
          </div>
        </details>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>

    <!-- ===== ВАШИ ДЕЙСТВИЯ ===== -->
    <section class="card block">
      <div class="card-head">
        <h2 class="card-title">Сейчас от вас ждут</h2>
        <span v-if="myOpen.length" class="count">{{ myOpen.length }}</span>
      </div>

      <p v-if="!loading && !myOpen.length" class="empty">
        Открытых заданий нет.
        <template v-if="myDone.length">Все ваши задания ({{ myDone.length }}) приняты.</template>
        <template v-else>Задания появятся, когда админ сгенерирует их на период.</template>
      </p>

      <ul v-else class="todo">
        <li v-for="t in myOpen" :key="t.id" class="todo-item" :class="`st-${t.status}`">
          <span class="ti-mark"><Icon :name="ps.icon(t.status)" /></span>
          <div class="ti-body">
            <div class="ti-title">
              <b>{{ t.title }}</b>
              <span class="badge" :class="t.task_role === 'approve' ? 'badge-accent' : 'badge-info'">
                {{ t.task_role === "approve" ? "согласование" : "заполнение" }}
              </span>
              <span class="badge badge-dot" :class="ps.badge(t.status)">{{ ps.label(t.status) }}</span>
            </div>
            <div class="ti-meta">
              <span v-if="t.year">период {{ t.year }}-{{ String(t.month).padStart(2, "0") }}</span>
              <span>этап {{ t.stage_code }}</span>
              <span v-if="t.cfo_count">{{ t.cfo_count }} ЦФО</span>
              <span v-if="t.delegate_name">делегировано: {{ t.delegate_name }}</span>
            </div>
          </div>
          <div class="ti-act">
            <NuxtLink v-if="t.form_code === 'TPL-MP'" :to="`/plans/mp-form/${t.id}`" class="btn btn-sm btn-primary">
              <Icon name="lucide:pencil" /> {{ t.task_role === "approve" ? "Проверить" : "Заполнить" }}
            </NuxtLink>
            <NuxtLink :to="`/plans/${t.pl_id}`" class="btn btn-sm btn-ghost">Карточка</NuxtLink>
          </div>
        </li>
      </ul>
    </section>

    <!-- ===== ДЕЛЕГИРОВАНО ВАМИ ===== -->
    <section v-if="delegated.length" class="card block">
      <div class="card-head"><h2 class="card-title">Вы делегировали</h2></div>
      <ul class="todo">
        <li v-for="t in delegated" :key="t.id" class="todo-item">
          <span class="ti-mark"><Icon name="lucide:share" /></span>
          <div class="ti-body">
            <div class="ti-title"><b>{{ t.title }}</b>
              <span class="badge badge-dot" :class="ps.badge(t.status)">{{ ps.label(t.status) }}</span>
            </div>
            <div class="ti-meta"><span>делегат: {{ t.delegate_name }}</span><span>этап {{ t.stage_code }}</span></div>
          </div>
          <div class="ti-act"><NuxtLink :to="`/plans/process/${t.pl_id}`" class="btn btn-sm btn-ghost">К заданиям</NuxtLink></div>
        </li>
      </ul>
    </section>

    <!-- ===== ПЕРИОДЫ ===== -->
    <section class="card block">
      <div class="card-head">
        <h2 class="card-title">Периоды</h2>
        <div v-if="canAdmin" class="create-period">
          <input v-model.number="newYear" type="number" class="select select-sm" min="2024" max="2030" />
          <input v-model.number="newMonth" type="number" class="select select-sm" min="1" max="12" />
          <button class="btn btn-primary" :disabled="creating" @click="create">
            <Icon name="lucide:plus" /> Создать период
          </button>
        </div>
      </div>

      <table class="data-table report-table">
        <thead>
          <tr>
            <th>Период</th>
            <th>Статус</th>
            <th class="col-num">Заполнено ячеек</th>
            <th>Открыть</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!list.length">
            <td colspan="4" class="empty">Периодов нет. {{ canAdmin ? "Создайте период." : "Обратитесь к администратору планов." }}</td>
          </tr>
          <tr v-for="it in list" :key="it.id">
            <td class="period-cell">{{ it.period_year }}-{{ String(it.period_month).padStart(2, "0") }}</td>
            <td><span class="badge" :class="ps.badge(it.status)">{{ ps.label(it.status) }}</span></td>
            <td class="col-num">{{ it.metric_count }}</td>
            <td class="row-links">
              <NuxtLink :to="`/plans/${it.id}?year=${it.period_year}&month=${it.period_month}`" class="link">Карточка</NuxtLink>
              <NuxtLink :to="`/plans/process/${it.id}`" class="link">Задания</NuxtLink>
            </td>
          </tr>
        </tbody>
      </table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { usePlans, type PlanInstance } from "~/composables/usePlans";
import { useTasks, type Task } from "~/composables/useTasks";

definePageMeta({ middleware: "scope-guard" });

const { instances, createInstance } = usePlans();
const { mine } = useTasks();
const { hasRole, isAdmin } = useScope();
const { user } = useAuth();
const ps = usePlanStatus();
const canAdmin = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const list = ref<PlanInstance[]>([]);
const tasks = ref<Task[]>([]);
const error = ref("");
const loading = ref(true);
const creating = ref(false);
const now = new Date();
const newYear = ref(now.getFullYear());
const newMonth = ref(now.getMonth() + 1);

// Порядок «что горит»: возвращённое → на проверке → в работе → не начато.
const URGENCY: Record<string, number> = { returned: 0, review: 1, in_progress: 2, pending: 3 };
const myOpen = computed(() =>
  tasks.value
    .filter((t) => t.status !== "done")
    .sort((a, b) => (URGENCY[a.status] ?? 9) - (URGENCY[b.status] ?? 9))
);
const myDone = computed(() => tasks.value.filter((t) => t.status === "done"));
// Задания, которые я отдал делегату (я исполнитель, работает другой).
const delegated = computed(() =>
  tasks.value.filter((t) => t.delegate_user_id && t.assignee_user_id === user.value?.id && t.status !== "done")
);

const load = async () => {
  loading.value = true;
  try {
    [list.value, tasks.value] = await Promise.all([instances(), mine().catch(() => [])]);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки";
  } finally {
    loading.value = false;
  }
};

const create = async () => {
  creating.value = true;
  error.value = "";
  try {
    await createInstance(newYear.value, newMonth.value);
    await load();
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка создания периода";
  } finally {
    creating.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.block { margin-bottom: var(--sp-5); }
.card-head {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: var(--sp-4); flex-wrap: wrap; gap: var(--sp-4);
}
.count { font-family: var(--font-mono); font-size: var(--fs-sm); color: var(--text-muted); }
.create-period { display: flex; align-items: center; gap: var(--sp-3, 6px); }
.select-sm { height: 30px; width: 76px; }

.todo { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--sp-2); }
.todo-item {
  display: flex; align-items: center; gap: var(--sp-4);
  padding: var(--sp-3) var(--sp-4); border-radius: var(--rd-3);
  background: var(--bg-tonal); border-left: 3px solid var(--border);
}
.todo-item.st-returned { border-left-color: var(--neg-strong); }
.todo-item.st-review { border-left-color: var(--warn); }
.todo-item.st-in_progress { border-left-color: var(--info); }
.ti-mark { color: var(--text-muted); display: inline-flex; }
.ti-body { min-width: 0; flex: 1; }
.ti-title { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: wrap; }
.ti-meta { display: flex; gap: var(--sp-4); flex-wrap: wrap; margin-top: 2px; font-size: var(--fs-2xs); color: var(--text-secondary); }
.ti-act { display: flex; gap: var(--sp-2); flex-shrink: 0; }

.more { position: relative; }
.more summary { list-style: none; cursor: pointer; }
.more summary::-webkit-details-marker { display: none; }
.more-menu {
  position: absolute; right: 0; top: calc(100% + 4px); z-index: 20;
  display: flex; flex-direction: column; min-width: 210px;
  background: var(--bg-surface); border: 1px solid var(--border);
  border-radius: var(--rd-4); padding: var(--sp-2);
}
.more-item { display: flex; align-items: center; gap: 6px; padding: 6px 8px; border-radius: var(--rd-3); font-size: var(--fs-sm); color: var(--text-secondary); }
.more-item:hover { background: var(--bg-tonal); }

.link { color: var(--accent, #4338ca); }
.row-links { display: flex; gap: var(--sp-4); }
.empty { text-align: center; color: var(--text-muted); padding: var(--sp-6) 0; }
.banner { padding: var(--sp-3) var(--sp-4); border-radius: var(--rd-4, 6px); margin-bottom: var(--sp-4); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
</style>
