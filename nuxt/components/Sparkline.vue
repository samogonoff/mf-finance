<template>
  <svg
    class="sparkline"
    :class="trendClass"
    :viewBox="`0 0 ${width} ${height}`"
    preserveAspectRatio="none"
    role="img"
  >
    <path :d="areaPath" class="area" />
    <path :d="linePath" class="line" />
  </svg>
</template>

<script setup lang="ts">
/**
 * Простой sparkline без зависимостей. Принимает массив чисел.
 * Тренд (pos / neg / neutral) можно задать явно или вычислить
 * по сравнению последнего и первого значения.
 */
const props = defineProps<{
  values: number[];
  width?: number;
  height?: number;
  trend?: "pos" | "neg" | "neutral";
}>();

const width = computed(() => props.width ?? 120);
const height = computed(() => props.height ?? 36);

const trendClass = computed(() => {
  if (props.trend) return props.trend;
  if (props.values.length < 2) return "neutral";
  const first = props.values[0];
  const last = props.values[props.values.length - 1];
  if (last > first) return "pos";
  if (last < first) return "neg";
  return "neutral";
});

const points = computed(() => {
  const v = props.values;
  if (!v.length) return [];
  const min = Math.min(...v);
  const max = Math.max(...v);
  const range = max - min || 1;
  const stepX = v.length > 1 ? width.value / (v.length - 1) : width.value;
  return v.map((value, i) => ({
    x: i * stepX,
    y: height.value - ((value - min) / range) * (height.value - 4) - 2
  }));
});

const linePath = computed(() => {
  const pts = points.value;
  if (!pts.length) return "";
  return pts.map((p, i) => `${i === 0 ? "M" : "L"}${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(" ");
});

const areaPath = computed(() => {
  const pts = points.value;
  if (!pts.length) return "";
  const line = pts.map((p, i) => `${i === 0 ? "M" : "L"}${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(" ");
  return `${line} L${pts[pts.length - 1].x.toFixed(2)},${height.value} L0,${height.value} Z`;
});
</script>
