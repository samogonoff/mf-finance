<template>
  <div ref="root" class="ms" :class="{ 'is-open': open, 'is-empty': !normalized.length, 'is-disabled': disabled }">
    <div class="ms-control" @click="toggle">
      <div class="ms-tags">
        <span v-for="v in modelValue" :key="v" class="ms-tag" :title="getDesc(v)">
          <span class="ms-tag-label">{{ getLabel(v) }}</span>
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
          v-for="opt in visible"
          :key="opt.value"
          class="ms-option"
          :class="{ selected: modelValue.includes(opt.value) }"
          @click.stop="toggleOption(opt.value)"
        >
          <span class="ms-check">{{ modelValue.includes(opt.value) ? "✓" : "" }}</span>
          <div class="ms-opt-body">
            <span class="ms-label">{{ opt.label }}</span>
            <span v-if="opt.description" class="ms-desc">{{ opt.description }}</span>
          </div>
        </div>
        <div v-if="!filtered.length" class="ms-empty">
          {{ normalized.length ? "Нет совпадений" : "Нет данных" }}
        </div>
        <!-- Список обрезан для отрисовки — сообщаем об этом. Молчаливое
             усечение читалось бы как «других значений нет». -->
        <div v-else-if="filtered.length > visible.length" class="ms-empty">
          Показаны первые {{ visible.length }} из {{ filtered.length }} —
          уточните поиск
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

export interface SelectOption {
  value: string;
  label: string;
  description?: string;
}

type OptionItem = string | SelectOption;

const props = withDefaults(
  defineProps<{
    modelValue: string[];
    options: OptionItem[];
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

/** Normalize options: strings become { value, label } */
const normalized = computed<SelectOption[]>(() =>
  props.options.map((o) =>
    typeof o === "string" ? { value: o, label: o } : o
  )
);

/** Lookup table for quick description retrieval */
const descMap = computed(() => {
  const m: Record<string, string | undefined> = {};
  for (const opt of normalized.value) {
    m[opt.value] = opt.description;
  }
  return m;
});

function getDesc(value: string): string | undefined {
  return descMap.value[value];
}

function getLabel(value: string): string {
  // show original value if option isn't in the list (edge case)
  return normalized.value.find((o) => o.value === value)?.label ?? value;
}

const filtered = computed(() => {
  const q = search.value.toLowerCase().trim();
  if (!q) return normalized.value;
  return normalized.value.filter(
    (o) =>
      o.label.toLowerCase().includes(q) ||
      (o.description && o.description.toLowerCase().includes(q))
  );
});

/** Сколько опций реально попадает в DOM. Каждая опция — три-четыре узла, и на
 * длинных справочниках (артикулов 12 020, моделей 4 255) открытие дропдауна
 * вешало страницу целиком. Отрисовываем начало списка, остальное ищется
 * поиском — он и так стоит первым элементом дропдауна. */
const RENDER_LIMIT = 200;

const visible = computed(() => filtered.value.slice(0, RENDER_LIMIT));

const toggle = () => {
  if (props.disabled) return;
  open.value = !open.value;
  if (open.value) {
    nextTick(() => searchInput.value?.focus());
  } else {
    search.value = "";
  }
};
const close = () => {
  open.value = false;
  search.value = "";
};
const toggleOption = (opt: string) => {
  const next = props.modelValue.includes(opt)
    ? props.modelValue.filter((v) => v !== opt)
    : [...props.modelValue, opt];
  emit("update:modelValue", next);
  emit("change");
};
const remove = (opt: string) => {
  if (props.disabled) return;
  emit("update:modelValue", props.modelValue.filter((v) => v !== opt));
  emit("change");
};
const clear = () => {
  if (props.disabled) return;
  emit("update:modelValue", []);
  emit("change");
  close();
};

const onClickOutside = (e: MouseEvent) => {
  if (!root.value) return;
  if (!root.value.contains(e.target as Node)) close();
};

onMounted(() => document.addEventListener("click", onClickOutside));
onUnmounted(() => document.removeEventListener("click", onClickOutside));
</script>

<style scoped>
.ms {
  position: relative;
  width: 100%;
  font-size: var(--fs-sm);
}

.ms-control {
  min-height: 36px;
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  background: var(--bg-surface);
  padding: 4px 28px 4px 6px;
  cursor: pointer;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px;
  position: relative;
  transition: border-color var(--t-fast);
}
.ms-control:hover { border-color: color-mix(in srgb, var(--accent) 40%, var(--border)); }
.ms.is-open .ms-control { border-color: var(--accent); }

.ms-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  flex: 1;
  min-width: 0;
  align-items: center;
}
.ms-placeholder {
  color: var(--text-muted);
  padding: 0 4px;
  font-size: var(--fs-xs);
}

.ms-tag {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  background: color-mix(in srgb, var(--accent) 12%, var(--bg-surface));
  border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
  color: var(--text-strong);
  padding: 1px 2px 1px 6px;
  border-radius: var(--rd-2);
  font-size: var(--fs-2xs);
  max-width: 200px;
  line-height: 1.4;
}
.ms-tag-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ms-x {
  border: none;
  background: transparent;
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
  padding: 0 4px;
  color: var(--text-muted);
  border-radius: var(--rd-1);
}
.ms-x:hover { color: var(--neg); background: rgba(0,0,0,0.04); }

.ms-caret {
  position: absolute;
  right: 8px;
  top: 50%;
  transform: translateY(-50%);
  font-size: 12px;
  color: var(--text-muted);
  transition: transform var(--t-base);
  pointer-events: none;
}
.ms-caret.open { transform: translateY(-50%) rotate(180deg); }

.ms-dropdown {
  position: absolute;
  z-index: 30;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.15);
  display: flex;
  flex-direction: column;
  max-height: 280px;
}
.ms-search {
  width: 100%;
  border: none;
  border-bottom: 1px solid var(--border);
  padding: 8px 10px;
  background: transparent;
  font-size: var(--fs-sm);
  outline: none;
  color: var(--text-strong);
}
.ms-list {
  overflow-y: auto;
  max-height: 200px;
  padding: 4px 0;
}
.ms-option {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 6px 10px;
  cursor: pointer;
  font-size: var(--fs-sm);
  color: var(--text-strong);
}
.ms-option:hover { background: var(--bg-surface-3); }
.ms-option.selected {
  background: color-mix(in srgb, var(--accent) 10%, var(--bg-surface));
}
.ms-check {
  width: 14px;
  text-align: center;
  color: var(--accent);
  font-weight: var(--fw-bold);
  flex-shrink: 0;
  margin-top: 2px;
}
.ms-opt-body {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
  flex: 1;
}
.ms-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: var(--fw-medium, 500);
}
.ms-desc {
  font-size: var(--fs-2xs, 10px);
  color: var(--text-muted);
  line-height: 1.3;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ms-empty {
  padding: 12px;
  color: var(--text-muted);
  text-align: center;
  font-size: var(--fs-xs);
}
.ms-actions {
  border-top: 1px solid var(--border);
  padding: 4px;
  display: flex;
  justify-content: flex-end;
}
.ms-clear {
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: var(--fs-xs);
  cursor: pointer;
  padding: 4px 8px;
  border-radius: var(--rd-1);
}
.ms-clear:hover { color: var(--accent); background: var(--bg-surface-3); }

/* Disabled state */
.ms.is-disabled .ms-control {
  opacity: 0.5;
  cursor: not-allowed;
  background: var(--bg-surface-2, #f8f9fa);
}
.ms.is-disabled .ms-caret { display: none; }
.ms.is-disabled .ms-placeholder { color: var(--text-muted); }
</style>
