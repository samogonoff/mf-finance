<!--
  Предпросмотр массовой операции: счётчики + diff «было → будет» (ТЗ Розница §5,
  «Обязательно: предпросмотр diff до применения»).

  Компонент отделён от страницы формы, потому что требование предпросмотра стоит
  у всех массовых операций модуля, а не только у розницы: применение без показа
  «что именно изменится» — это ровно тот сценарий, из-за которого в прототипе
  теряли ручные правки на 375 строках.

  Компонент ничего не применяет сам: он показывает результат preview-запроса и
  эмитит apply. Решение «можно ли применять» остаётся у страницы — она знает,
  редактируем ли период и не изменились ли параметры операции после предпросмотра.
-->
<template>
  <section class="bdp card">
    <div class="card-header">
      <span class="card-title">
        Предпросмотр: {{ title }}
      </span>
      <span class="bdp-scope">{{ scopeLabel }}</span>
    </div>

    <!-- Счётчики читаются раньше таблицы: человеку сначала нужен масштаб
         («изменится 84 ячейки, 12 защищено»), и только потом построчный разбор. -->
    <div class="bdp-counts">
      <div class="bc">
        <span class="bc-val">{{ num(result.rows_in) }}</span>
        <span class="bc-lbl">строк в области</span>
      </div>
      <div class="bc bc-key">
        <span class="bc-val">{{ num(result.changed) }}</span>
        <span class="bc-lbl">ячеек изменится</span>
      </div>
      <div class="bc" :class="{ 'bc-warn': result.protected_manual > 0 }">
        <span class="bc-val">{{ num(result.protected_manual) }}</span>
        <span class="bc-lbl">защищено (введено вручную)</span>
      </div>
      <div class="bc" :class="{ 'bc-warn': result.skipped_no_base > 0 }">
        <span class="bc-val">{{ num(result.skipped_no_base) }}</span>
        <span class="bc-lbl">пропущено (нет базы)</span>
      </div>
    </div>

    <p v-if="result.protected_manual > 0" class="bdp-note">
      Ячейки, введённые вручную, операция не перезаписывает. Чтобы перекрыть их,
      включите «сбросить защиту ручных значений» и повторите предпросмотр.
    </p>
    <p v-for="(n, i) in result.notes || []" :key="i" class="bdp-note">{{ n }}</p>

    <p v-if="!result.diff?.length" class="bdp-empty">
      Изменений нет: операция ничего не поменяет в выбранной области.
    </p>

    <div v-else class="bdp-scroll">
      <table class="data-table compact">
        <thead>
          <tr>
            <th>Магазин</th>
            <th>Период</th>
            <th>Метрика</th>
            <th class="col-num">Было</th>
            <th class="col-num">Будет</th>
            <th class="col-num">Δ</th>
            <th>Пометка</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(d, i) in shown" :key="i">
            <td>
              <span class="d-name">{{ d.cfo || "—" }}</span>
              <span class="d-code">ЦФО {{ d.code_cfo }}</span>
            </td>
            <td class="d-per">{{ monthLabel(d.month) }} {{ d.year }}</td>
            <td class="d-metric">{{ metricLabel(d.metric) }}</td>
            <td class="col-num d-before">{{ d.before == null ? "пусто" : money(d.before) }}</td>
            <td class="col-num d-after">{{ money(d.after) }}</td>
            <td class="col-num" :class="deltaClass(d)">{{ deltaText(d) }}</td>
            <td class="d-note">
              <span v-if="d.after_source" class="d-src">{{ sourceLabel(d.after_source) }}</span>
              <span v-if="d.note" class="d-hint">{{ d.note }}</span>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="result.diff.length > shown.length" class="bdp-more">
        Показаны первые {{ num(shown.length) }} из {{ num(result.diff.length) }} изменений —
        остальные применятся по тем же правилам.
      </p>
    </div>

    <div class="bdp-actions">
      <button type="button" class="btn btn-ghost" :disabled="busy" @click="emit('close')">
        Отмена
      </button>
      <button
        type="button"
        class="btn btn-primary"
        :disabled="busy || !canApply || !result.changed"
        :title="applyHint"
        @click="emit('apply')"
      >
        <Icon name="lucide:check" /> Применить ({{ num(result.changed) }})
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { BULK_OP_LABELS, MONTH_LABELS, SOURCE_LABELS, type RetailBulkDiff, type RetailBulkOp, type RetailBulkResult } from "~/composables/useRetail";
import { num as fmtNum } from "~/utils/format";

const props = withDefaults(
  defineProps<{
    /** Результат preview-запроса. */
    result: RetailBulkResult;
    /** Идёт запрос — кнопки заблокированы. */
    busy?: boolean;
    /** Разрешено ли применение (период открыт, параметры не менялись после предпросмотра). */
    canApply?: boolean;
    /** Почему применение недоступно — подсказка на кнопке. */
    applyHint?: string;
    /** Сколько строк diff показывать. Остальное — по тем же правилам, смотреть незачем. */
    limit?: number;
  }>(),
  { busy: false, canApply: true, applyHint: "", limit: 300 }
);

const emit = defineEmits<{ (e: "apply"): void; (e: "close"): void }>();

const title = computed(() => BULK_OP_LABELS[props.result.op as RetailBulkOp] || props.result.op);

const scopeLabel = computed(() => {
  const s: Record<string, string> = {
    all: "вся выборка",
    filtered: "отфильтрованные строки",
    selected: "выделенные строки"
  };
  return `область: ${s[props.result.scope] || props.result.scope}`;
});

const shown = computed(() => (props.result.diff || []).slice(0, props.limit));

const num = (v: number) => fmtNum(v);
const money = (v: number) => fmtNum(v, 2);
const monthLabel = (m: number) => MONTH_LABELS[m - 1] || String(m);
const sourceLabel = (s: string) => SOURCE_LABELS[s] || s;

const METRICS: Record<string, string> = { sales: "продажи", payroll: "ФОТ", rent: "аренда" };
const metricLabel = (m: string) => METRICS[m] || m;

// Дельта показывается только там, где было с чем сравнивать: «пусто → 100 000»
// это не рост на 100 000, а первое заполнение, и знак здесь дезинформирует.
const deltaText = (d: RetailBulkDiff): string => {
  if (d.before == null) return "новое";
  const v = d.after - d.before;
  return (v > 0 ? "+" : "") + fmtNum(v, 2);
};
const deltaClass = (d: RetailBulkDiff): string => {
  if (d.before == null) return "d-new";
  const v = d.after - d.before;
  if (v > 0) return "delta-pos";
  if (v < 0) return "delta-neg";
  return "delta-zero";
};
</script>

<style scoped>
.bdp {
  display: flex;
  flex-direction: column;
}
.bdp-scope {
  font-size: var(--fs-sm);
  color: var(--text-muted);
}
.bdp-counts {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: var(--sp-4);
  padding: var(--sp-5);
  border-bottom: 1px solid var(--border);
}
.bc {
  display: flex;
  flex-direction: column;
  gap: var(--sp-1);
}
.bc-val {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--fs-xl);
  color: var(--text-strong);
}
.bc-lbl {
  font-size: var(--fs-xs);
  color: var(--text-secondary);
}
.bc-key .bc-val {
  color: var(--accent);
  font-weight: var(--fw-semibold);
}
.bc-warn .bc-val {
  color: var(--warn);
}
.bdp-note {
  margin: 0;
  padding: var(--sp-3) var(--sp-5);
  font-size: var(--fs-sm);
  color: var(--warn);
  background: var(--warn-soft);
}
.bdp-empty {
  margin: 0;
  padding: var(--sp-6) var(--sp-5);
  color: var(--text-muted);
  font-size: var(--fs-sm);
}
.bdp-scroll {
  max-height: 46vh;
  overflow: auto;
}
.d-name {
  display: block;
}
.d-code,
.d-hint {
  display: block;
  font-size: var(--fs-xs);
  color: var(--text-muted);
}
.d-per,
.d-metric {
  color: var(--text-secondary);
  white-space: nowrap;
}
.d-before {
  color: var(--text-muted);
}
.d-after {
  color: var(--text-strong);
  font-weight: var(--fw-semibold);
}
.d-new {
  color: var(--info);
}
.d-src {
  font-size: var(--fs-xs);
  color: var(--text-secondary);
}
.bdp-more {
  margin: 0;
  padding: var(--sp-4) var(--sp-5);
  font-size: var(--fs-xs);
  color: var(--text-muted);
}
.bdp-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--sp-4);
  padding: var(--sp-5);
  border-top: 1px solid var(--border);
  background: var(--bg-surface-2);
}
</style>
