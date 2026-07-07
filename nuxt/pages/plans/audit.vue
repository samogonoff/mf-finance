<template>
  <div class="page-audit">
    <header class="page-header">
      <div>
        <h1 class="page-title">Журнал аудита</h1>
        <p class="page-subtitle">События модуля «Тактические планы» (PLANS_AUDIT_ENABLED)</p>
      </div>
      <div class="page-actions">
        <button class="btn btn-ghost" @click="exportCsv">
          <Icon name="lucide:download" /> CSV
        </button>
      </div>
    </header>

    <div class="filters-bar">
      <div class="filter">
        <label class="filter-label">Пользователь (id)</label>
        <input v-model.number="userId" type="number" class="select select-sm" />
      </div>
      <div class="filter">
        <label class="filter-label">С</label>
        <input v-model="from" type="date" class="select" />
      </div>
      <div class="filter">
        <label class="filter-label">По</label>
        <input v-model="to" type="date" class="select" />
      </div>
      <button class="btn btn-primary" @click="load"><Icon name="lucide:filter" /> Применить</button>
    </div>

    <p v-if="error" class="error-banner">{{ error }}</p>
    <table class="data-table report-table">
      <thead>
        <tr>
          <th>Время</th>
          <th class="col-num">Пользователь</th>
          <th>Действие</th>
          <th>Объект</th>
          <th class="col-num">ID</th>
          <th>IP</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="!events.length">
          <td colspan="6" class="empty">Нет событий (или аудит выключен).</td>
        </tr>
        <tr v-for="e in events" :key="e.id">
          <td>{{ e.ts }}</td>
          <td class="col-num">{{ e.user_id || "—" }}</td>
          <td><span class="action-chip">{{ e.action }}</span></td>
          <td>{{ e.entity_type }}</td>
          <td class="col-num">{{ e.entity_id || "—" }}</td>
          <td>{{ e.ip }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { usePlans, type AuditEvent } from "~/composables/usePlans";

definePageMeta({ middleware: "scope-guard" });

const { audit, auditCsv } = usePlans();

const events = ref<AuditEvent[]>([]);
const userId = ref<number | undefined>();
const from = ref("");
const to = ref("");
const error = ref("");

const filter = () => ({ user_id: userId.value, from: from.value, to: to.value });

const load = async () => {
  error.value = "";
  try {
    events.value = await audit(filter());
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки (нужна роль ROLE_PLANS_ADMIN)";
  }
};

const exportCsv = async () => {
  try {
    const blob = await auditCsv(filter());
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "plans-audit.csv";
    a.click();
    URL.revokeObjectURL(url);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка экспорта";
  }
};

onMounted(load);
</script>

<style scoped>
.filters-bar {
  display: flex;
  align-items: flex-end;
  gap: var(--sp-5);
  margin-bottom: var(--sp-6);
  flex-wrap: wrap;
}
.select-sm {
  width: 120px;
}
.action-chip {
  font-size: var(--fs-sm, 12px);
  background: var(--bg-muted, #f3f4f6);
  border-radius: var(--radius-sm, 4px);
  padding: 1px 8px;
  font-family: var(--font-mono);
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
