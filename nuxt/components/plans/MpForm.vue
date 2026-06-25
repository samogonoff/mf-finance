<template>
  <div class="mp-form">
    <div v-for="block in form.blocks" :key="block.code_pl" class="form-block">
      <div class="block-head">
        <span class="block-name">{{ block.name }}</span>
        <span class="block-code">code_pl {{ block.code_pl }}</span>
        <span v-if="block.editable" class="badge badge-edit">тактика</span>
      </div>
      <table class="data-table report-table">
        <thead>
          <tr>
            <th class="col-sticky">Площадка</th>
            <th class="col-num">Факт ({{ form.header.currency }})</th>
            <th class="col-num">Тактика ({{ form.header.currency }})</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in block.rows" :key="row.code_cfo">
            <td class="col-sticky">{{ row.name_cfo }}</td>
            <td class="col-num fact">{{ money(row.fact, { currency: "" }) }}</td>
            <td class="col-num" :class="{ 'cell-manual': row.manual }">
              <div class="tactic-cell">
                <input
                  v-model.number="row.tactic"
                  type="number"
                  class="tactic-input"
                  :disabled="!block.editable"
                  placeholder="—"
                />
                <CellComment :manual="row.manual" />
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p class="hint">Факт — read-only (OLAP/FinDWH). Редактируется только «Тактика бюджет (таргеты)».</p>
  </div>
</template>

<script setup lang="ts">
import { money } from "~/utils/format";
import CellComment from "~/components/plans/CellComment.vue";
import type { MpFormData } from "~/composables/usePlans";

defineProps<{ form: MpFormData }>();
</script>

<style scoped>
.form-block {
  margin-bottom: var(--sp-6);
}
.block-head {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  margin-bottom: var(--sp-3, 6px);
}
.block-name {
  font-weight: 600;
}
.block-code {
  font-family: var(--font-mono);
  font-size: var(--fs-sm, 12px);
  color: var(--fg-muted, #6b7280);
}
.badge-edit {
  background: var(--accent, #4338ca);
  color: #fff;
  border-radius: var(--radius-sm, 4px);
  padding: 0 6px;
  font-size: var(--fs-sm, 12px);
}
.fact {
  color: var(--fg-muted, #6b7280);
}
.tactic-input {
  width: 100%;
  text-align: right;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  border: 1px solid var(--border, #d1d5db);
  border-radius: var(--radius-sm, 4px);
  padding: 2px 6px;
  background: var(--bg-input, #fff);
}
.tactic-input:disabled {
  background: var(--bg-muted, #f3f4f6);
  color: var(--fg-muted, #6b7280);
}
.tactic-cell {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 2px;
}
.cell-manual .tactic-input {
  border-color: var(--accent, #4338ca);
  background: var(--accent-soft, #eef2ff);
}
.hint {
  font-size: var(--fs-sm, 12px);
  color: var(--fg-muted, #6b7280);
}
</style>
