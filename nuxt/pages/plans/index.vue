<template>
  <div class="page-plans">
    <header class="page-header">
      <div>
        <h1 class="page-title">Тактические планы</h1>
        <p class="page-subtitle">Формирование и согласование тактических планов P&amp;L</p>
      </div>
      <div class="page-actions">
        <NuxtLink to="/plans/mp/large" class="btn btn-ghost">
          <Icon name="lucide:store" /> МП large
        </NuxtLink>
        <NuxtLink to="/plans/mp/small" class="btn btn-ghost">
          <Icon name="lucide:store" /> МП small
        </NuxtLink>
        <NuxtLink to="/plans/directories" class="btn btn-ghost">
          <Icon name="lucide:book" /> Справочники
        </NuxtLink>
        <NuxtLink v-if="canAudit" to="/plans/audit" class="btn btn-ghost">
          <Icon name="lucide:scroll-text" /> Аудит
        </NuxtLink>
      </div>
    </header>

    <!-- KPI-плитки по экземплярам PL. -->
    <div class="kpi-row">
      <div class="kpi">
        <span class="kpi-label">Экземпляров PL</span>
        <span class="kpi-value">{{ list.length }}</span>
      </div>
      <div class="kpi">
        <span class="kpi-label">Заполнено ячеек</span>
        <span class="kpi-value">{{ totalMetrics }}</span>
      </div>
      <div class="kpi">
        <span class="kpi-label">Активный период</span>
        <span class="kpi-value">{{ latestPeriod }}</span>
      </div>
    </div>

    <section class="card">
      <div class="card-head">
        <h2 class="card-title">Список тактических PL</h2>
        <div class="create-period">
          <input v-model.number="newYear" type="number" class="select select-sm" min="2024" max="2030" />
          <input v-model.number="newMonth" type="number" class="select select-sm" min="1" max="12" />
          <button class="btn btn-primary" :disabled="creating" @click="create">
            <Icon name="lucide:plus" /> Создать период
          </button>
        </div>
      </div>

      <p v-if="error" class="error-banner">{{ error }}</p>
      <table class="data-table report-table">
        <thead>
          <tr>
            <th>Период</th>
            <th>Статус</th>
            <th class="col-num">Ячеек</th>
            <th>Открыть</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!list.length">
            <td colspan="4" class="empty">Нет экземпляров. Создайте период.</td>
          </tr>
          <tr v-for="it in list" :key="it.id">
            <td>{{ it.period_year }}-{{ String(it.period_month).padStart(2, "0") }}</td>
            <td><span class="status-chip">{{ it.status }}</span></td>
            <td class="col-num">{{ it.metric_count }}</td>
            <td class="row-links">
              <NuxtLink
                :to="`/plans/${it.id}?year=${it.period_year}&month=${it.period_month}`"
                class="link"
              >Карточка</NuxtLink>
              <NuxtLink
                :to="`/plans/mp/large?year=${it.period_year}&month=${it.period_month}`"
                class="link"
              >МП large</NuxtLink>
            </td>
          </tr>
        </tbody>
      </table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { usePlans, type PlanInstance } from "~/composables/usePlans";

definePageMeta({ middleware: "scope-guard" });

const { instances, createInstance } = usePlans();
const { hasRole, isAdmin } = useScope();
const canAudit = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const list = ref<PlanInstance[]>([]);
const error = ref("");
const creating = ref(false);
const newYear = ref(2026);
const newMonth = ref(6);

const totalMetrics = computed(() => list.value.reduce((s, i) => s + i.metric_count, 0));
const latestPeriod = computed(() => {
  if (!list.value.length) return "—";
  const it = list.value[0];
  return `${it.period_year}-${String(it.period_month).padStart(2, "0")}`;
});

const load = async () => {
  try {
    list.value = await instances();
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки";
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
.kpi-row {
  display: flex;
  gap: var(--sp-5);
  margin-bottom: var(--sp-6);
  flex-wrap: wrap;
}
.kpi {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: var(--sp-4) var(--sp-5);
  border: 1px solid var(--border, #e5e7eb);
  border-radius: var(--radius, 8px);
  min-width: 140px;
}
.kpi-label {
  font-size: var(--fs-sm, 12px);
  color: var(--fg-muted, #6b7280);
}
.kpi-value {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: 20px;
}
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--sp-4);
  flex-wrap: wrap;
  gap: var(--sp-4);
}
.create-period {
  display: flex;
  align-items: center;
  gap: var(--sp-3, 6px);
}
.select-sm {
  height: 30px;
  width: 76px;
}
.status-chip {
  font-size: var(--fs-sm, 12px);
  background: var(--bg-muted, #f3f4f6);
  border-radius: var(--radius-sm, 4px);
  padding: 1px 8px;
}
.link {
  color: var(--accent, #4338ca);
}
.row-links {
  display: flex;
  gap: var(--sp-4);
}
.empty {
  text-align: center;
  color: var(--fg-muted, #6b7280);
  padding: var(--sp-6) 0;
}
.error-banner {
  color: var(--neg, #b42318);
  padding: var(--sp-4) 0;
}
</style>
