<!--
  Множественный выбор ЦФО ИЗ СПРАВОЧНИКА dir_cfo (вместо ввода кодов запятыми).
  Поиск по наименованию/коду, фильтр по стране; выбранные — чипами (коды ЦФО).
  Это «задания» ответственного: какие ЦФО он заполняет на этапе.
  v-model — number[] кодов ЦФО.
-->
<template>
  <div class="cfo-picker">
    <div class="cp-chips" v-if="modelValue.length">
      <span v-for="code in modelValue" :key="code" class="cp-chip">
        {{ labelOf(code) }}
        <button class="cp-x" @click="remove(code)"><Icon name="lucide:x" /></button>
      </span>
    </div>
    <div ref="anchor" class="cp-input">
      <Icon name="lucide:search" class="cp-ico" />
      <input v-model="q" class="cp-field" placeholder="ЦФО: наименование или код…" @focus="onFocus" @blur="onBlur" @input="updatePos" />
    </div>
    <Teleport to="body">
      <div v-if="open && filtered.length" class="cp-drop" :style="dropStyle">
        <button v-for="r in filtered" :key="r.code" type="button" class="cp-item" @mousedown.prevent="add(r.code)">
          <span class="cp-name">{{ r.name }}</span>
          <span class="cp-meta">{{ r.code }} · {{ r.group }} · {{ r.country }}</span>
        </button>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { useDirectories } from "~/composables/useDirectories";

const props = defineProps<{ modelValue: number[]; country?: string }>();
const emit = defineEmits<{ "update:modelValue": [number[]] }>();

type Row = { code: number; name: string; group: string; country: string };
const all = ref<Row[]>([]);
const q = ref("");
const open = ref(false);
const anchor = ref<HTMLElement | null>(null);
const dropStyle = ref<Record<string, string>>({});

onMounted(async () => {
  try {
    const rows = await useDirectories().rows("dir_cfo");
    all.value = rows.map((r) => ({
      code: Number(r.payload.code_cfo),
      name: String(r.payload.name_cfo ?? ""),
      group: String(r.payload.group_cfo1 ?? ""),
      country: String(r.payload.country ?? "")
    })).filter((r) => !Number.isNaN(r.code) && r.code !== 0);
  } catch {
    all.value = [];
  }
});

const labelOf = (code: number) => all.value.find((r) => r.code === code)?.name || `ЦФО ${code}`;

const filtered = computed(() => {
  const term = q.value.trim().toLowerCase();
  const sel = new Set(props.modelValue);
  return all.value
    .filter((r) => !sel.has(r.code))
    .filter((r) => !props.country || r.country === props.country)
    .filter((r) => !term || r.name.toLowerCase().includes(term) || String(r.code).includes(term))
    .slice(0, 30);
});

const add = (code: number) => { emit("update:modelValue", [...props.modelValue, code]); q.value = ""; };
const remove = (code: number) => emit("update:modelValue", props.modelValue.filter((c) => c !== code));

const updatePos = () => {
  const el = anchor.value;
  if (!el) return;
  const r = el.getBoundingClientRect();
  dropStyle.value = { position: "fixed", top: `${r.bottom + 4}px`, left: `${r.left}px`, width: `${r.width}px` };
};
const onFocus = () => { open.value = true; updatePos(); };
const onBlur = () => setTimeout(() => (open.value = false), 150);
</script>

<style scoped>
.cfo-picker { display: flex; flex-direction: column; gap: var(--sp-2); }
.cp-chips { display: flex; flex-wrap: wrap; gap: var(--sp-2); }
.cp-chip { display: inline-flex; align-items: center; gap: 4px; background: var(--accent-soft); color: var(--accent); border-radius: var(--rd-3); padding: 2px var(--sp-2); font-size: var(--fs-xs); }
.cp-x { border: none; background: none; cursor: pointer; color: inherit; display: inline-flex; padding: 0; }
.cp-input { display: flex; align-items: center; gap: var(--sp-2); border: 1px solid var(--border); border-radius: var(--rd-4); padding: 0 var(--sp-3); background: var(--bg-surface); }
.cp-ico { color: var(--text-muted); flex-shrink: 0; }
.cp-field { border: none; background: none; outline: none; padding: 6px 0; font: inherit; font-size: var(--fs-sm); flex: 1; min-width: 0; }
</style>

<style>
.cp-drop { z-index: 1000; background: var(--bg-surface); border: 1px solid var(--border); border-radius: var(--rd-4); box-shadow: 0 10px 30px rgba(0,0,0,0.18); max-height: 300px; overflow-y: auto; padding: 4px; }
.cp-drop .cp-item { display: flex; flex-direction: column; gap: 1px; width: 100%; text-align: left; border: none; background: none; cursor: pointer; padding: var(--sp-2) var(--sp-3); border-radius: var(--rd-3); }
.cp-drop .cp-item:hover { background: var(--bg-tonal); }
.cp-drop .cp-name { font-size: var(--fs-sm); }
.cp-drop .cp-meta { font-size: var(--fs-2xs); color: var(--text-muted); font-family: var(--font-mono); }
</style>
