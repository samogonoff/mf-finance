<template>
  <div class="mp-fact">
    <table class="data-table report-table">
      <thead>
        <tr>
          <th class="col-sticky">Площадка</th>
          <th class="col-num">Код ЦФО</th>
          <th class="col-num">Код PL</th>
          <th class="col-num">Факт ({{ currency }})</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="!rows.length">
          <td colspan="4" class="empty">Нет данных факта за выбранный период.</td>
        </tr>
        <tr v-for="r in rows" :key="`${r.code_cfo}-${r.code_pl}`">
          <td class="col-sticky">{{ r.name_cfo }}</td>
          <td class="col-num">{{ r.code_cfo }}</td>
          <td class="col-num">{{ r.code_pl }}</td>
          <td class="col-num">{{ money(r.amount, { currency: "" }) }}</td>
        </tr>
      </tbody>
    </table>
    <p class="note">Факт — read-only (источник: OLAP/FinDWH). Ввод тактики — на странице формы.</p>
  </div>
</template>

<script setup lang="ts">
import { money } from "~/utils/format";
import type { PlanFactRow } from "~/composables/usePlans";

withDefaults(
  defineProps<{
    rows: PlanFactRow[];
    currency?: string;
  }>(),
  { currency: "RUB" }
);
</script>

<style scoped>
.empty {
  text-align: center;
  color: var(--fg-muted, #6b7280);
  padding: var(--sp-6) 0;
}
.note {
  margin-top: var(--sp-4);
  font-size: var(--fs-sm, 12px);
  color: var(--fg-muted, #6b7280);
}
</style>
