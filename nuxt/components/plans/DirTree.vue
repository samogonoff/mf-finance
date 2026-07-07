<!--
  Древовидный рендер вложенного справочника (ТЗ-запрос: уникальный интерфейс под
  сложные справочники). Группирует строки по иерархии group_by (страна → группа →
  подгруппа), сворачиваемые узлы; листья — компактная таблица типизированных ячеек.
  Рекурсивный: ссылается на себя по имени DirTree.
-->
<template>
  <div class="tree">
    <div v-for="g in groups" :key="g.key" class="tree-node">
      <button class="tree-head" :style="{ paddingLeft: depth * 18 + 8 + 'px' }" @click="toggle(g.key)">
        <Icon :name="isOpen(g.key) ? 'lucide:chevron-down' : 'lucide:chevron-right'" class="tw" />
        <DirCell v-if="headerCol" :col="headerCol" :value="g.value" />
        <span v-else class="g-val">{{ g.value || "—" }}</span>
        <span class="tree-count">{{ g.count }}</span>
      </button>

      <div v-show="isOpen(g.key)" class="tree-body">
        <DirTree
          v-if="rest.length"
          :rows="g.rows"
          :group-by="rest"
          :all-columns="allColumns"
          :leaf-columns="leafColumns"
          :depth="depth + 1"
          @row="$emit('row', $event)"
        />
        <table v-else class="data-table compact leaf" :style="{ marginLeft: (depth + 1) * 18 + 8 + 'px' }">
          <tbody>
            <tr v-for="r in g.rows" :key="r.id" class="leaf-row" @click="$emit('row', r)">
              <td v-for="c in leafColumns" :key="c.key" :class="{ 'td-num': c.type === 'number' || c.type === 'money' }">
                <DirCell :col="c" :value="r.payload[c.key]" />
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import DirCell from "~/components/plans/DirCell.vue";
import type { DirColumn, DirectoryRow } from "~/composables/useDirectories";

defineOptions({ name: "DirTree" });
defineEmits<{ row: [DirectoryRow] }>();

const props = withDefaults(defineProps<{
  rows: DirectoryRow[];
  groupBy: string[];
  allColumns: DirColumn[];
  leafColumns: DirColumn[];
  depth?: number;
}>(), { depth: 0 });

const headerCol = computed(() => props.allColumns.find((c) => c.key === props.groupBy[0]));
const rest = computed(() => props.groupBy.slice(1));

const groups = computed(() => {
  const key = props.groupBy[0];
  const map = new Map<string, DirectoryRow[]>();
  for (const r of props.rows) {
    const v = String(r.payload[key] ?? "");
    if (!map.has(v)) map.set(v, []);
    map.get(v)!.push(r);
  }
  return [...map.entries()]
    .sort((a, b) => a[0].localeCompare(b[0], "ru"))
    .map(([value, rows]) => ({ key: value || "∅", value, rows, count: rows.length }));
});

// Состояние раскрытия: верхний уровень открыт по умолчанию.
const openSet = reactive<Record<string, boolean>>({});
const isOpen = (k: string) => openSet[k] ?? props.depth === 0;
const toggle = (k: string) => { openSet[k] = !isOpen(k); };
</script>

<style scoped>
.tree-node { border-bottom: 1px solid var(--border); }
.tree-head {
  display: flex; align-items: center; gap: var(--sp-2);
  width: 100%; text-align: left; border: none; background: none; cursor: pointer;
  padding: var(--sp-2) var(--sp-3); font-size: var(--fs-sm); color: var(--text-primary);
}
.tree-head:hover { background: var(--bg-tonal); }
.tw { color: var(--text-muted); flex-shrink: 0; }
.g-val { font-weight: var(--fw-medium); }
.tree-count { margin-left: auto; font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.tree-body { }
.leaf { width: auto; }
.leaf-row { cursor: pointer; }
.leaf-row:hover { background: var(--bg-tonal); }
.td-num { text-align: right; }
.data-table.compact td { padding: var(--sp-2) var(--sp-3); }
</style>
