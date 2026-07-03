<!--
  Рендер одной ячейки справочника по типу колонки из схемы (ТЗ §«UI справочников»).
  Типы: text | code | number | money | country | badge | bool | date.
  Числа — JetBrains Mono + tabular-nums (дизайн-система). Бейджи — по тону из схемы.
-->
<template>
  <span v-if="value === null || value === undefined || value === ''" class="cell-empty">—</span>

  <span v-else-if="col.type === 'badge'" class="badge" :class="badgeClass">{{ value }}</span>

  <span v-else-if="col.type === 'country'" class="country">
    <span class="badge badge-dot" :class="badgeClass">{{ countryLabel }}</span>
  </span>

  <span v-else-if="col.type === 'bool'" class="cell-bool" :class="boolVal ? 'is-yes' : 'is-no'">
    <Icon :name="boolVal ? 'lucide:check' : 'lucide:minus'" />
  </span>

  <span v-else-if="col.type === 'money'" class="cell-num">{{ num(Number(value), 2) }}</span>

  <span v-else-if="col.type === 'number'" class="cell-num">{{ formatNumber }}</span>

  <span v-else-if="col.type === 'code'" class="cell-code">{{ value }}</span>

  <span v-else>{{ value }}</span>
</template>

<script setup lang="ts">
import { num } from "~/utils/format";
import type { DirColumn } from "~/composables/useDirectories";

const props = defineProps<{ col: DirColumn; value: unknown }>();

const COUNTRY_LABELS: Record<string, string> = {
  RU: "Россия", BY: "Беларусь", KZ: "Казахстан", UZ: "Узбекистан",
  РФ: "Россия", РБ: "Беларусь", КЗ: "Казахстан", УЗ: "Узбекистан"
};

const boolVal = computed(() => props.value === true || props.value === "true" || props.value === 1);

const countryLabel = computed(() => COUNTRY_LABELS[String(props.value)] || String(props.value));

const formatNumber = computed(() => {
  const n = Number(props.value);
  if (Number.isNaN(n)) return String(props.value);
  // дробные (площадь, коэффициент) — 1 знак; целые — без дробной части.
  return Number.isInteger(n) ? num(n, 0) : num(n, 1);
});

const TONE_CLASS: Record<string, string> = {
  pos: "badge-pos", neg: "badge-neg", warn: "badge-warn", info: "badge-info",
  accent: "badge-accent", muted: ""
};

const badgeClass = computed(() => {
  const tone = props.col.badge_tones?.[String(props.value)];
  return tone ? TONE_CLASS[tone] ?? "" : "";
});
</script>

<style scoped>
.cell-empty { color: var(--text-muted); }
.cell-num {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  text-align: right;
  display: block;
}
.cell-code {
  font-family: var(--font-mono);
  font-size: var(--fs-xs);
  color: var(--text-secondary);
}
.cell-bool { display: inline-flex; }
.cell-bool.is-yes { color: var(--pos-strong); }
.cell-bool.is-no { color: var(--text-muted); }
.country .badge { font-weight: var(--fw-medium); }
</style>
