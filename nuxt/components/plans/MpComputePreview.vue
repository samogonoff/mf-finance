<template>
  <section class="compute-preview">
    <div class="preview-head">
      <span class="preview-title">Расчёт каскада (превью)</span>
      <span class="badge badge-info">расчёт</span>
    </div>
    <table class="data-table report-table">
      <thead>
        <tr>
          <th class="col-sticky">Площадка</th>
          <th class="col-num">Продажи без НДС</th>
          <th class="col-num">Маржа (gross)</th>
          <th class="col-num">Наценка, %</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="!rows.length">
          <td colspan="4" class="empty">Нажмите «Рассчитать каскад».</td>
        </tr>
        <tr v-for="r in rows" :key="r.code_cfo">
          <td class="col-sticky">{{ r.name_cfo }}</td>
          <td class="col-num">{{ fmt(r.values.sales_net) }}</td>
          <td class="col-num">{{ fmt(r.values.gross_margin) }}</td>
          <td class="col-num">{{ pct(r.values.markup_pct) }}</td>
        </tr>
      </tbody>
    </table>
    <p class="note">Производные показатели по формулам каскада; формулу можно переопределить для этого среза (кнопка «Формула каскада…»).</p>
  </section>
</template>

<script setup lang="ts">
import { money, num } from "~/utils/format";
import type { ComputedRow } from "~/composables/usePlans";

defineProps<{ rows: ComputedRow[] }>();

const fmt = (v: number | undefined) => (v === undefined ? "—" : money(v, { currency: "" }));
const pct = (v: number | undefined) => (v === undefined ? "—" : `${num(v * 100, 1)} %`);
</script>

<style scoped>
.compute-preview {
  margin-top: var(--sp-6);
  border-top: 1px solid var(--border, #e5e7eb);
  padding-top: var(--sp-5);
}
.preview-head {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  margin-bottom: var(--sp-4);
}
.preview-title {
  font-weight: 600;
}
.badge-calc {
  background: var(--bg-muted, #f3f4f6);
  color: var(--fg-muted, #6b7280);
  border-radius: var(--radius-sm, 4px);
  padding: 0 6px;
  font-size: var(--fs-sm, 12px);
}
.empty {
  text-align: center;
  color: var(--fg-muted, #6b7280);
  padding: var(--sp-5) 0;
}
.note {
  margin-top: var(--sp-3, 6px);
  font-size: var(--fs-sm, 12px);
  color: var(--fg-muted, #6b7280);
}
</style>
