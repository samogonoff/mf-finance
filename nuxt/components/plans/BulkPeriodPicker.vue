<!--
  Выбор периода массовой операции: ОТКУДА копируем (год и месяц источника) и КУДА вставляем (конкретный месяц или все
  оставшиеся до конца года). Один компонент на все формы с массовыми операциями (розница, расходы), чтобы выбор месяцев
  выглядел и работал одинаково, а не «только ноя» в одном месте и «все плановые» в другом.

  Компонент ничего не применяет: он хранит выбор (v-model) и отдаёт готовый список целевых месяцев через
  `targetMonthsOf`; страница сама решает, какие месяцы открыты и что отправлять на сервер.
-->
<template>
  <div class="bpp" :class="{ 'bpp-inline': inline }">
    <template v-if="showSource">
      <label class="bpp-field">
        <span class="bpp-label">Год источника</span>
        <select :class="selectClass" :value="modelValue.sourceYear" :disabled="disabled" @change="set({ sourceYear: Number(($event.target as HTMLSelectElement).value) })">
          <option v-for="y in years" :key="y" :value="y">{{ y }}</option>
        </select>
      </label>
      <label class="bpp-field">
        <span class="bpp-label">Месяц источника</span>
        <select :class="selectClass" :value="modelValue.sourceMonth" :disabled="disabled" @change="set({ sourceMonth: Number(($event.target as HTMLSelectElement).value) })">
          <option v-if="allowSameMonth" :value="0">тот же месяц (помесячно)</option>
          <option v-for="m in 12" :key="m" :value="m">{{ monthName(m) }}</option>
        </select>
      </label>
    </template>

    <label class="bpp-field">
      <span class="bpp-label">Куда вставляем</span>
      <select :class="selectClass" :value="modelValue.targetMode" :disabled="disabled" @change="set({ targetMode: ($event.target as HTMLSelectElement).value as TargetMode })">
        <option value="month">один месяц</option>
        <option value="to_end">все месяцы до конца года</option>
      </select>
    </label>
    <label class="bpp-field">
      <span class="bpp-label">{{ modelValue.targetMode === "month" ? "Месяц" : "Начиная с месяца" }}</span>
      <select :class="selectClass" :value="modelValue.targetMonth" :disabled="disabled || !months.length" @change="set({ targetMonth: Number(($event.target as HTMLSelectElement).value) })">
        <option v-for="m in months" :key="m" :value="m">{{ monthName(m) }}{{ m === cardMonth ? " · месяц плана" : "" }}</option>
      </select>
    </label>
    <span class="bpp-hint">{{ hint }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { targetMonthsOf, type BulkPeriodPick } from "~/utils/bulkPeriod";

const props = withDefaults(defineProps<{
  modelValue: BulkPeriodPick;
  /** Месяцы, в которые можно вставлять (открытые для ввода), по возрастанию. */
  months: number[];
  /** Месяц плана карточки — подписывается в списке. */
  cardMonth?: number;
  years?: number[];
  showSource?: boolean;
  /** В источнике разрешён вариант «тот же месяц (помесячно)». */
  allowSameMonth?: boolean;
  disabled?: boolean;
  inline?: boolean;
  selectClass?: string;
}>(), { cardMonth: 0, years: () => [], showSource: false, allowSameMonth: true, disabled: false, inline: true, selectClass: "select" });

const emit = defineEmits<{ (e: "update:modelValue", v: BulkPeriodPick): void }>();

const NAMES = ["янв", "фев", "мар", "апр", "май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"];
const monthName = (m: number) => NAMES[m - 1] || String(m);

const set = (patch: Partial<BulkPeriodPick>) => emit("update:modelValue", { ...props.modelValue, ...patch });

/** Целевые месяцы выбора среди открытых: один месяц или все начиная с выбранного. */
const targets = computed(() => targetMonthsOf(props.modelValue, props.months));
const hint = computed(() => {
  const t = targets.value;
  if (!t.length) return "нет открытых месяцев";
  return t.length === 1 ? `будет заполнен: ${monthName(t[0])}` : `будут заполнены: ${monthName(t[0])}–${monthName(t[t.length - 1])} (${t.length})`;
});
</script>



<style scoped>
.bpp { display: flex; align-items: flex-end; flex-wrap: wrap; gap: var(--sp-5); }
.bpp-field { display: flex; flex-direction: column; gap: var(--sp-2); }
.bpp-label { font-size: var(--fs-sm); color: var(--text-secondary); }
.bpp-hint { font-size: var(--fs-sm); color: var(--text-muted); align-self: center; }
</style>
