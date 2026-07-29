<!--
  Поле ввода числа с ru-форматированием (финансовый ввод).

  Вне фокуса показывает «1 123 773,51» — разделители тысяч + запятая, ровно
  `digits` знаков; в фокусе показывает сырое «1123773,51», чтобы при вводе не
  прыгал курсор. Значение округляется до `digits` знаков при потере фокуса,
  наружу отдаётся обычным number (точка, без пробелов).

  type=text, а не number: браузер не даёт ввести в number ни пробел-разделитель,
  ни запятую, и не позволяет форматировать value. Ввод ограничен маской
  цифр/разделителей, вставка «1 234,56» из Excel парсится корректно.
-->
<template>
  <input
    ref="el"
    :value="display"
    type="text"
    inputmode="decimal"
    autocomplete="off"
    @focus="onFocus"
    @input="onInput"
    @blur="onBlur"
    @keydown.enter="el?.blur()"
  />
</template>

<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    /** Значение модели; null = пусто. */
    modelValue: number | null;
    /** Знаков после запятой при отображении и округлении. */
    digits?: number;
  }>(),
  { digits: 2 }
);
const emit = defineEmits<{ (e: "update:modelValue", v: number | null): void }>();

const el = ref<HTMLInputElement | null>(null);
const focused = ref(false);
const draft = ref("");

const round = (v: number) => Number(v.toFixed(props.digits));

// \s в JS покрывает и NBSP, и узкий пробел — именно ими toLocaleString('ru-RU')
// разделяет тысячи, так что вставка отформатированного числа парсится.
const parse = (raw: string): number | null => {
  const s = raw.replace(/\s/g, "").replace(",", ".");
  if (s === "" || s === "-" || s === "." || s === "-.") return null;
  const n = Number(s);
  return Number.isFinite(n) ? n : null;
};

const format = (v: number) =>
  v.toLocaleString("ru-RU", {
    minimumFractionDigits: props.digits,
    maximumFractionDigits: props.digits
  });

const display = computed(() => {
  if (focused.value) return draft.value;
  const v = props.modelValue;
  if (v == null || Number.isNaN(v)) return "";
  return format(v);
});

const onFocus = () => {
  const v = props.modelValue;
  draft.value = v == null || Number.isNaN(v) ? "" : v.toFixed(props.digits).replace(".", ",");
  focused.value = true;
};

const onInput = (ev: Event) => {
  const input = ev.target as HTMLInputElement;
  // Оставляем только цифры, разделители и ведущий минус; лишний десятичный
  // разделитель отбрасываем, иначе «1,2,3» превратится в NaN.
  let s = input.value.replace(/[^\d.,\-\s]/g, "");
  const neg = s.trimStart().startsWith("-");
  s = s.replace(/-/g, "");
  const parts = s.split(/[.,]/);
  if (parts.length > 1) s = parts[0] + "," + parts.slice(1).join("");
  if (neg) s = "-" + s;
  draft.value = s;
  // Vue не перерисует input, если draft не изменился, — отфильтрованный символ
  // остался бы в DOM. Синхронизируем сами, сохраняя позицию курсора.
  if (input.value !== s) {
    const pos = Math.max(0, (input.selectionStart ?? s.length) - (input.value.length - s.length));
    input.value = s;
    input.setSelectionRange(pos, pos);
  }
  // Во время набора не округляем — иначе нельзя допечатать знак.
  emit("update:modelValue", parse(s));
};

const onBlur = () => {
  focused.value = false;
  const n = parse(draft.value);
  const next = n == null ? null : round(n);
  if (next !== props.modelValue) emit("update:modelValue", next);
};
</script>
