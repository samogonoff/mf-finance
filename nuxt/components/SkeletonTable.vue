<!--
  Контур таблицы на время загрузки. Формы и своды планов ходят в OLAP и отвечают
  за несколько секунд — пустой экран в это время читается как «страница зависла».
  Показываем ту же геометрию, что у настоящей таблицы: липкая колонка-подпись
  слева, числовые колонки справа.

  Ширины «текста» берутся из детерминированного паттерна (не Math.random) —
  иначе SSR и клиент отрисуют разное и Vue выдаст hydration mismatch.
-->
<template>
  <div class="sk-table" role="status" aria-busy="true" :aria-label="label">
    <div v-if="header" class="sk-row sk-head">
      <div class="sk-cell sk-first"><span class="skeleton skeleton-text" :style="{ width: '55%' }" /></div>
      <div v-for="c in cols" :key="'h' + c" class="sk-cell"><span class="skeleton skeleton-text" :style="{ width: '60%' }" /></div>
    </div>

    <div v-for="r in rows" :key="r" class="sk-row" :class="{ 'sk-section': isSection(r) }">
      <div class="sk-cell sk-first">
        <span class="skeleton skeleton-text" :style="{ width: labelWidth(r) }" />
      </div>
      <div v-for="c in cols" :key="r + ':' + c" class="sk-cell">
        <span class="skeleton skeleton-text" :style="{ width: cellWidth(r, c) }" />
      </div>
    </div>

    <span class="sr-only">{{ label }}</span>
  </div>
</template>

<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    rows?: number;
    cols?: number;
    header?: boolean;
    /** Каждая N-я строка рисуется как заголовок секции (0 — без секций). */
    sectionEvery?: number;
    label?: string;
  }>(),
  { rows: 8, cols: 4, header: true, sectionEvery: 4, label: "Загружаю данные…" }
);

// Детерминированные «случайные» ширины: стабильны между SSR и клиентом.
const LABEL_W = ["78%", "62%", "88%", "70%", "55%", "82%"];
const CELL_W = ["70%", "52%", "64%", "45%", "58%"];

const isSection = (r: number) => props.sectionEvery > 0 && r % props.sectionEvery === 1;
const labelWidth = (r: number) => (isSection(r) ? "34%" : LABEL_W[r % LABEL_W.length]);
const cellWidth = (r: number, c: number) => (isSection(r) ? "0%" : CELL_W[(r * 3 + c) % CELL_W.length]);
</script>

<style scoped>
.sk-table { width: 100%; }
.sk-row {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  padding: var(--sp-3) var(--sp-4);
  border-top: 1px solid var(--border);
}
.sk-row:first-child { border-top: none; }
.sk-head { background: var(--bg-tonal); }
.sk-section { background: var(--bg-surface-2); }
.sk-cell { flex: 1; display: flex; justify-content: flex-end; min-width: 60px; }
.sk-first { flex: 0 0 clamp(180px, 28%, 320px); justify-content: flex-start; }
.sr-only {
  position: absolute;
  width: 1px; height: 1px;
  padding: 0; margin: -1px;
  overflow: hidden; clip: rect(0 0 0 0);
  white-space: nowrap; border: 0;
}
</style>
