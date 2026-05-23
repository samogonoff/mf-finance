<template>
  <div ref="root" class="ms" :class="{ 'is-open': open, 'is-empty': !options.length, 'is-disabled': disabled }">
    <div class="ms-control" @click="toggle">
      <div class="ms-tags">
        <span v-for="v in modelValue" :key="v" class="ms-tag">
          <span class="ms-tag-label">{{ labelFor(v) }}</span>
          <button
            v-if="!disabled"
            type="button"
            class="ms-x"
            aria-label="Удалить"
            @click.stop="remove(v)"
          >×</button>
        </span>
        <span v-if="!modelValue.length" class="ms-placeholder">{{ placeholder }}</span>
      </div>
      <span class="ms-caret" :class="{ open }" aria-hidden="true">▾</span>
    </div>

    <div v-if="open" class="ms-dropdown">
      <input
        ref="searchInput"
        v-model="search"
        type="text"
        class="ms-search"
        placeholder="Поиск…"
        @click.stop
        @keydown.escape.prevent="close"
      />
      <div class="ms-list">
        <div
          v-for="opt in filtered"
          :key="opt.value"
          class="ms-option"
          :class="{ selected: modelValue.includes(opt.value) }"
          @click.stop="toggleOption(opt.value)"
        >
          <span class="ms-check">{{ modelValue.includes(opt.value) ? "✓" : "" }}</span>
          <span class="ms-label">{{ opt.label }}</span>
        </div>
        <div v-if="!filtered.length" class="ms-empty">
          {{ options.length ? "Нет совпадений" : "Нет данных" }}
        </div>
      </div>
      <div v-if="modelValue.length" class="ms-actions">
        <button type="button" class="ms-clear" @click.stop="clear">
          Очистить все
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";

interface Option {
  value: string;
  label: string;
}

const props = withDefaults(
  defineProps<{
    modelValue: string[];
    options: Option[];
    placeholder?: string;
    disabled?: boolean;
  }>(),
  { placeholder: "Все значения", disabled: false }
);

const emit = defineEmits<{
  (e: "update:modelValue", v: string[]): void;
  (e: "change"): void;
}>();

const root = ref<HTMLElement | null>(null);
const searchInput = ref<HTMLInputElement | null>(null);
const open = ref(false);
const search = ref("");

const labelFor = (value: string) =>
  props.options.find((o) => o.value === value)?.label ?? value;

const filtered = computed(() => {
  const q = search.value.toLowerCase().trim();
  if (!q) return props.options;
  return props.options.filter(
    (o) => o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q)
  );
});

const toggle = () => {
  if (props.disabled) return;
  open.value = !open.value;
  if (open.value) nextTick(() => searchInput.value?.focus());
};
const close = () => {
  open.value = false;
  search.value = "";
};
const toggleOption = (v: string) => {
  const next = props.modelValue.includes(v)
    ? props.modelValue.filter((x) => x !== v)
    : [...props.modelValue, v];
  emit("update:modelValue", next);
  emit("change");
};
const remove = (v: string) => {
  emit("update:modelValue", props.modelValue.filter((x) => x !== v));
  emit("change");
};
const clear = () => {
  emit("update:modelValue", []);
  emit("change");
};

const onDocClick = (e: MouseEvent) => {
  if (!root.value) return;
  if (!root.value.contains(e.target as Node)) close();
};

onMounted(() => document.addEventListener("click", onDocClick));
onUnmounted(() => document.removeEventListener("click", onDocClick));
</script>

<style scoped>
.ms {
  position: relative;
  min-width: 180px;
  font-size: var(--fs-sm);
}
.ms-control {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  min-height: 32px;
  padding: 2px 8px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  cursor: pointer;
}
.ms.is-disabled .ms-control { opacity: 0.6; cursor: not-allowed; }
.ms-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  flex: 1;
  align-items: center;
}
.ms-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 6px;
  background: var(--bg-surface-2);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  font-size: var(--fs-xs);
  max-width: 220px;
}
.ms-tag-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ms-x {
  background: transparent;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
}
.ms-x:hover { color: var(--text-strong); }
.ms-placeholder {
  color: var(--text-muted);
}
.ms-caret {
  color: var(--text-muted);
  transition: transform 0.15s;
}
.ms-caret.open { transform: rotate(180deg); }
.ms-dropdown {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  box-shadow: 0 4px 12px rgba(0,0,0,0.08);
  z-index: 50;
  max-height: 320px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.ms-search {
  border: none;
  border-bottom: 1px solid var(--border);
  padding: 6px 10px;
  font-size: var(--fs-sm);
  outline: none;
  background: transparent;
  color: var(--text-strong);
  font-family: inherit;
}
.ms-list {
  flex: 1;
  overflow: auto;
}
.ms-option {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  cursor: pointer;
}
.ms-option:hover { background: var(--bg-surface-2); }
.ms-option.selected { color: var(--accent); }
.ms-check {
  width: 14px;
  color: var(--accent);
  font-weight: var(--fw-semibold);
}
.ms-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ms-empty {
  padding: 12px 10px;
  color: var(--text-muted);
  text-align: center;
}
.ms-actions {
  border-top: 1px solid var(--border);
  padding: 6px 8px;
  text-align: right;
}
.ms-clear {
  background: transparent;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: var(--fs-xs);
  padding: 4px 6px;
  font-family: inherit;
}
.ms-clear:hover { color: var(--accent); }
</style>
