<template>
  <div ref="root" class="ms" :class="{ 'is-open': open, 'is-empty': !options.length }">
    <div class="ms-control" @click="toggle">
      <div class="ms-tags">
        <span v-for="v in modelValue" :key="v" class="ms-tag">
          <span class="ms-tag-label">{{ v }}</span>
          <button
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
          :key="opt"
          class="ms-option"
          :class="{ selected: modelValue.includes(opt) }"
          @click.stop="toggleOption(opt)"
        >
          <span class="ms-check">{{ modelValue.includes(opt) ? "✓" : "" }}</span>
          <span class="ms-label">{{ opt }}</span>
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

const props = withDefaults(
  defineProps<{
    modelValue: string[];
    options: string[];
    placeholder?: string;
  }>(),
  { placeholder: "Все значения" }
);

const emit = defineEmits<{
  (e: "update:modelValue", v: string[]): void;
  (e: "change"): void;
}>();

const root = ref<HTMLElement | null>(null);
const searchInput = ref<HTMLInputElement | null>(null);
const open = ref(false);
const search = ref("");

const filtered = computed(() => {
  const q = search.value.toLowerCase().trim();
  if (!q) return props.options;
  return props.options.filter((o) => o.toLowerCase().includes(q));
});

const toggle = () => {
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
  emit("update:modelValue", props.modelValue.filter((v) => v !== opt));
  emit("change");
};
const clear = () => {
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
  align-items: center;
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
}
.ms-label {
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
</style>
