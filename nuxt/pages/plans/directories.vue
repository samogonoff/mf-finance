<template>
  <div class="page-dirs">
    <header class="page-header">
      <div>
        <h1 class="page-title">Справочники</h1>
        <p class="page-subtitle">НСИ модуля — наши (редактируемые) и из Лисы/1С (синхронизация)</p>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>

    <div class="dir-layout">
      <aside class="dir-list">
        <div class="dir-group-label">Наши (редактируемые)</div>
        <button
          v-for="d in editableDirs"
          :key="d.code"
          type="button"
          class="dir-item"
          :class="{ active: active === d.code }"
          @click="open(d.code)"
        >
          <span class="dir-code">{{ d.code }}</span>
          <span class="dir-meta">{{ d.row_count }} строк · <span class="badge badge-pos">manual</span></span>
        </button>

        <div class="dir-group-label">Из Лисы / 1С (синхронизация)</div>
        <button
          v-for="d in syncedDirs"
          :key="d.code"
          type="button"
          class="dir-item"
          :class="{ active: active === d.code }"
          @click="open(d.code)"
        >
          <span class="dir-code">{{ d.code }}</span>
          <span class="dir-meta"><span class="badge badge-info">{{ d.source }}</span> · {{ d.sync_status }}</span>
        </button>
      </aside>

      <section class="dir-content card">
        <div v-if="!active" class="empty-state">Выберите справочник слева.</div>

        <template v-else>
          <div class="dir-head">
            <div>
              <span class="dir-title">{{ active }}</span>
              <span class="badge" :class="activeEditable ? 'badge-pos' : 'badge-info'">
                {{ activeEditable ? "редактируемый" : "read-only (синхронизация)" }}
              </span>
            </div>
            <button v-if="activeEditable" class="btn btn-sm btn-primary" @click="addRow">
              <Icon name="lucide:plus" /> Добавить строку
            </button>
          </div>

          <div v-if="!activeEditable && !rows.length" class="empty-state">
            Источник «{{ active }}» — {{ activeSource }}. Данные приходят синхронизацией (read-only); ручное редактирование недоступно.
          </div>

          <div v-else class="table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th v-for="c in columns" :key="c">{{ c }}</th>
                  <th v-if="activeEditable" class="col-act"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!rows.length"><td :colspan="columns.length + 1" class="empty-cell">Нет строк.</td></tr>
                <tr v-for="row in rows" :key="row.id">
                  <td v-for="c in columns" :key="c" :class="{ 'col-num': isNum(row.payload[c]) }">
                    <input
                      v-if="activeEditable"
                      v-model="row.payload[c]"
                      class="cell-input"
                      :class="{ 'cell-num': isNum(row.payload[c]) }"
                    />
                    <span v-else>{{ row.payload[c] }}</span>
                  </td>
                  <td v-if="activeEditable" class="col-act">
                    <button class="btn btn-icon btn-sm" title="Сохранить" @click="saveRow(row)"><Icon name="lucide:check" /></button>
                    <button class="btn btn-icon btn-sm" title="Удалить" @click="delRow(row)"><Icon name="lucide:trash-2" /></button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { usePlans, type PlanDirectory, type DirRow } from "~/composables/usePlans";

definePageMeta({ middleware: "scope-guard" });

const { directories, dirRows, dirUpsert, dirDelete } = usePlans();

const dirs = ref<PlanDirectory[]>([]);
const active = ref("");
const rows = ref<DirRow[]>([]);
const error = ref("");

const editableDirs = computed(() => dirs.value.filter((d) => d.editable));
const syncedDirs = computed(() => dirs.value.filter((d) => !d.editable));
const activeDir = computed(() => dirs.value.find((d) => d.code === active.value));
const activeEditable = computed(() => !!activeDir.value?.editable);
const activeSource = computed(() => (activeDir.value?.source === "1c" ? "1С" : "Лиса"));
const columns = computed(() => {
  const keys = new Set<string>();
  rows.value.forEach((r) => Object.keys(r.payload).forEach((k) => keys.add(k)));
  return [...keys];
});
const isNum = (v: unknown) => typeof v === "number";

const open = async (code: string) => {
  active.value = code;
  error.value = "";
  try {
    rows.value = await dirRows(code);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки справочника";
    rows.value = [];
  }
};

const addRow = () => {
  const tmpl: Record<string, unknown> = {};
  columns.value.forEach((c) => (tmpl[c] = ""));
  rows.value.unshift({ id: 0, payload: tmpl });
};

const saveRow = async (row: DirRow) => {
  error.value = "";
  try {
    const res = await dirUpsert(active.value, row.id, row.payload);
    row.id = res.id;
    await refreshCounts();
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка сохранения";
  }
};

const delRow = async (row: DirRow) => {
  if (row.id === 0) {
    rows.value = rows.value.filter((r) => r !== row);
    return;
  }
  try {
    await dirDelete(active.value, row.id);
    rows.value = rows.value.filter((r) => r.id !== row.id);
    await refreshCounts();
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка удаления";
  }
};

const refreshCounts = async () => {
  dirs.value = await directories();
};

onMounted(async () => {
  try {
    dirs.value = await directories();
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки";
  }
});
</script>

<style scoped>
.banner {
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-4, 6px);
  margin-bottom: var(--sp-5);
  font-size: var(--fs-sm);
}
.banner-neg {
  background: var(--neg-soft);
  color: var(--neg-strong);
}
.dir-layout {
  display: grid;
  grid-template-columns: 240px 1fr;
  gap: var(--sp-5);
  align-items: start;
}
.dir-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.dir-group-label {
  font-size: var(--fs-2xs);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-muted);
  margin: var(--sp-4) 0 var(--sp-3);
}
.dir-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  text-align: left;
  padding: var(--sp-4);
  border: 1px solid var(--border);
  border-radius: var(--rd-4, 6px);
  background: var(--bg-surface);
  cursor: pointer;
}
.dir-item.active {
  border-color: var(--accent);
  box-shadow: var(--shadow-focus);
}
.dir-code {
  font-family: var(--font-mono);
  font-weight: var(--fw-semibold);
  font-size: var(--fs-sm);
}
.dir-meta {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
}
.dir-content {
  min-height: 200px;
  padding: var(--sp-5);
}
.dir-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--sp-4);
}
.dir-title {
  font-family: var(--font-mono);
  font-weight: var(--fw-semibold);
  margin-right: var(--sp-4);
}
.empty-state {
  color: var(--text-muted);
  padding: var(--sp-7);
  text-align: center;
}
.empty-cell {
  color: var(--text-muted);
  text-align: center;
  padding: var(--sp-6) 0;
}
.cell-input {
  width: 100%;
  min-width: 90px;
  border: 1px solid transparent;
  border-radius: var(--rd-3, 4px);
  padding: 2px 6px;
  background: transparent;
  font: inherit;
}
.cell-input:hover,
.cell-input:focus {
  border-color: var(--border);
  background: var(--bg-surface);
}
.cell-num {
  text-align: right;
  font-family: var(--font-mono);
}
.col-act {
  white-space: nowrap;
  width: 1%;
}
</style>
