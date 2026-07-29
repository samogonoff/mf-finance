<!--
  Свод периода — общий просмотр всех данных карточки (для проверяющих/согласующих).
  Строки — те же, что в форме ввода (спека TPL-MP приходит с сервера), поэтому
  проверяющий сверяет 1:1 с тем, что видел заполняющий. Детализация по площадкам —
  раскрытием строки (как в шаблоне финансов), сценарии — колонками.
  Данные: GET /api/plans/instances/{id}/board.
-->
<template>
  <div class="mp-board">
    <div class="filters">
      <label class="f">
        <span class="f-lbl">Валюта</span>
        <select v-model="filter.currency" class="select select-sm" @change="load">
          <option value="RUB">RUB</option>
          <option value="BYN">BYN</option>
          <option value="USD">USD</option>
        </select>
      </label>
      <label class="f">
        <span class="f-lbl">Сегмент</span>
        <select v-model="filter.segment" class="select select-sm" @change="load">
          <option value="">все МП</option>
          <option value="large">крупные (large)</option>
          <option value="small">мелкие (small)</option>
        </select>
      </label>
      <label class="f">
        <span class="f-lbl">Юридическое лицо</span>
        <select v-model="filter.legal_entity" class="select select-sm" @change="load">
          <option value="">все ЮЛ</option>
          <option v-for="le in legalEntities" :key="le" :value="le">{{ le }}</option>
        </select>
      </label>
      <label class="f">
        <span class="f-lbl">Страна</span>
        <select v-model="filter.country" class="select select-sm" @change="load">
          <option value="">все</option>
          <option v-for="c in countries" :key="c" :value="c">{{ c }}</option>
        </select>
      </label>
      <label class="f">
        <span class="f-lbl">Маркет</span>
        <select v-model="platform" class="select select-sm" @change="load">
          <option value="0">все</option>
          <option v-for="p in board?.platforms || []" :key="p.code_cfo" :value="String(p.code_cfo)">{{ p.name }}</option>
        </select>
      </label>
      <div class="f f-cols">
        <span class="f-lbl">Колонки</span>
        <div class="col-chips">
          <button
            v-for="c in BOARD_COLUMNS"
            :key="c.key"
            type="button"
            class="chip"
            :class="{ active: columns.includes(c.key) }"
            :title="c.hint"
            @click="toggleColumn(c.key)"
          >{{ c.label }}</button>
        </div>
      </div>
      <label class="f f-check">
        <input v-model="onlyManual" type="checkbox" /> только с корректировками
      </label>
      <label class="f f-check">
        <input v-model="onlyInput" type="checkbox" /> только строки ввода
      </label>
    </div>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>

    <!-- Первая загрузка: контур будущей таблицы вместо пустоты (свод тянет
         факт/стратегию/таргет по всем площадкам — это секунды). -->
    <div v-if="!board && loading" class="sk-wrap">
      <p class="hint-line"><Icon name="lucide:loader-circle" class="spin" /> Собираю свод периода…</p>
      <SkeletonTable :rows="14" :cols="5" :section-every="5" label="Загружаю свод периода" />
    </div>

    <p v-else-if="board && !visibleRows.length && !loading" class="empty-state">
      Нет данных под фильтры. {{ hasAnyTactic ? "Ослабьте фильтры." : "Тактику наполняют задания — откройте вкладку «Задания»." }}
    </p>

    <div v-else-if="board" class="table-wrap" :class="{ busy: loading }">
      <table class="data-table board-table">
        <thead>
          <tr>
            <th class="col-line">Показатель</th>
            <th v-for="c in activeColumns" :key="c.key" class="num" :title="c.hint">{{ c.label }}</th>
            <th class="col-who">Кто / статус</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="grp in groupedRows" :key="grp.section">
            <tr class="sec-row"><td :colspan="activeColumns.length + 2">{{ grp.section }}</td></tr>
            <template v-for="row in grp.rows" :key="row.line.block_type">
              <tr class="line-row" :class="{ strong: isStrong(row.line.block_type) }">
                <td class="col-line">
                  <button
                    v-if="(row.children || []).length"
                    type="button"
                    class="exp"
                    :title="expanded[row.line.block_type] ? 'Свернуть площадки' : 'Показать площадки'"
                    @click="toggle(row.line.block_type)"
                  >
                    <Icon :name="expanded[row.line.block_type] ? 'lucide:chevron-down' : 'lucide:chevron-right'" />
                  </button>
                  <span v-else class="exp exp-none"></span>
                  <span class="line-name">{{ row.line.name }}</span>
                  <span v-if="row.line.code_pl" class="cp">PL {{ row.line.code_pl }}</span>
                  <Icon v-if="row.line.formula" name="lucide:info" class="fi" :title="row.line.formula" />
                </td>
                <td v-for="c in activeColumns" :key="c.key" class="num" :class="deltaClass(c.key, boardCell(row.total, c.key))">
                  {{ fmt(row.line, c.key, boardCell(row.total, c.key)) }}
                </td>
                <td class="col-who"></td>
              </tr>

              <tr
                v-for="ch in expanded[row.line.block_type] ? visibleChildren(row) : []"
                :key="row.line.block_type + ':' + ch.code_cfo"
                class="child-row"
                :class="{ adj: ch.is_manual }"
              >
                <td class="col-line child-name">
                  <span class="tree">└</span>
                  {{ ch.name }}
                  <span class="cfo">ЦФО {{ ch.code_cfo }}</span>
                  <span class="le">{{ ch.legal_entity }}</span>
                  <Icon
                    v-if="ch.is_manual"
                    name="lucide:pencil"
                    class="adj-mark"
                    :title="ch.reason ? 'ручная корректировка: ' + ch.reason : 'ручная корректировка'"
                  />
                </td>
                <td v-for="c in activeColumns" :key="c.key" class="num" :class="deltaClass(c.key, boardCell(ch.values, c.key))">
                  {{ fmt(row.line, c.key, boardCell(ch.values, c.key)) }}
                </td>
                <td class="col-who">
                  <NuxtLink v-if="ch.task_id" :to="`/plans/mp-form/${ch.task_id}`" class="who-link" :title="'Открыть форму задания'">
                    <span class="badge badge-dot" :class="st.badge(ch.task_status || 'pending')">{{ st.label(ch.task_status || "pending") }}</span>
                    <span class="who-name">{{ ch.assignee || "не назначен" }}</span>
                  </NuxtLink>
                  <span v-else class="who-none">задания нет</span>
                </td>
              </tr>
            </template>
          </template>
        </tbody>
      </table>
    </div>

    <p class="prov-note">
      Строки и порядок — те же, что в форме ввода (единая спека TPL-MP).
      «Тактика» пустая → показано значение каскада. Факт прошлого года и таргет — из Budgeting.
    </p>
  </div>
</template>

<script setup lang="ts">
import { num } from "~/utils/format";
import type { MpLine } from "~/composables/useTasks";
import {
  usePlanBoard, boardCell, BOARD_COLUMNS,
  type Board, type BoardRow, type BoardChild, type BoardColumn
} from "~/composables/usePlanBoard";

const props = defineProps<{ plId: number }>();

const api = usePlanBoard();
const st = usePlanStatus();

const board = ref<Board | null>(null);
const error = ref("");
const loading = ref(true); // сразу true: onMounted грузит свод, скелетон не должен мигать
const platform = ref("0");
const onlyManual = ref(false);
const onlyInput = ref(false);
const expanded = reactive<Record<string, boolean>>({});
const columns = ref<BoardColumn[]>(["fact_prev", "fact", "strategy", "tactic", "delta_strategy"]);

const filter = reactive({ currency: "RUB", segment: "", legal_entity: "", country: "" });

const activeColumns = computed(() => BOARD_COLUMNS.filter((c) => columns.value.includes(c.key)));
const toggleColumn = (k: BoardColumn) => {
  const i = columns.value.indexOf(k);
  if (i >= 0) columns.value.splice(i, 1);
  else columns.value.push(k);
};

const legalEntities = computed(() =>
  [...new Set((board.value?.platforms || []).map((p) => p.legal_entity).filter(Boolean))].sort()
);
const countries = computed(() =>
  [...new Set((board.value?.platforms || []).map((p) => p.country).filter(Boolean))].sort()
);

const rows = computed(() => board.value?.rows || []);
const hasAnyTactic = computed(() =>
  rows.value.some((r) => r.total.tactic != null || (r.children || []).some((c) => c.values.tactic != null))
);

// Фильтры уровня строки применяем на клиенте: серверу незачем знать про «показать
// только ввод» — это вопрос представления.
const visibleRows = computed<BoardRow[]>(() =>
  rows.value.filter((r) => {
    if (onlyInput.value && !r.line.editable) return false;
    if (onlyManual.value && !(r.children || []).some((c) => c.is_manual)) return false;
    return true;
  })
);

const groupedRows = computed(() => {
  const out: { section: string; rows: BoardRow[] }[] = [];
  for (const r of visibleRows.value) {
    let g = out.find((x) => x.section === r.line.section);
    if (!g) { g = { section: r.line.section, rows: [] }; out.push(g); }
    g.rows.push(r);
  }
  return out;
});

const visibleChildren = (row: BoardRow): BoardChild[] =>
  (row.children || []).filter((c) => !onlyManual.value || c.is_manual);

const toggle = (block: string) => { expanded[block] = !expanded[block]; };

const isStrong = (block: string) => block === "pl_platform" || block === "platform_costs_total";

const fmtPct = (v: number) => (v * 100).toFixed(1) + "%";
const fmt = (line: MpLine, col: BoardColumn, v: number | null): string => {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  const delta = col === "delta_strategy" || col === "delta_fact_prev";
  if (line.value_kind === "pct") return delta ? (v * 100).toFixed(1) + " п.п." : fmtPct(v);
  return num(v, 0);
};
const deltaClass = (col: BoardColumn, v: number | null): string => {
  if (v === null || (col !== "delta_strategy" && col !== "delta_fact_prev")) return "";
  return v > 0 ? "d-pos" : v < 0 ? "d-neg" : "";
};

const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    const cfo = Number(platform.value) > 0 ? [Number(platform.value)] : [];
    board.value = await api.board(props.plId, { ...filter, code_cfo: cfo });
  } catch (e) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки свода";
  } finally {
    loading.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.filters { display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--sp-4); margin-bottom: var(--sp-4); }
.f { display: flex; flex-direction: column; gap: 2px; }
.f-lbl { font-size: var(--fs-2xs); color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.03em; }
.f-check { flex-direction: row; align-items: center; gap: 6px; font-size: var(--fs-sm); color: var(--text-secondary); }
.select-sm { height: 30px; }
.col-chips { display: flex; flex-wrap: wrap; gap: 4px; }
.chip { border: 1px solid var(--border); background: var(--bg-surface); color: var(--text-secondary); border-radius: 999px; padding: 2px 10px; font-size: var(--fs-2xs); cursor: pointer; }
.chip.active { background: var(--accent-soft); color: var(--accent); border-color: var(--accent); }

.banner { padding: var(--sp-3) var(--sp-4); border-radius: var(--rd-4, 6px); margin-bottom: var(--sp-4); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.hint-line, .empty-state { color: var(--text-muted); font-size: var(--fs-sm); padding: var(--sp-5) 0; }
.hint-line { display: flex; align-items: center; gap: 6px; padding: 0 0 var(--sp-3); }
.empty-state { text-align: center; }
.sk-wrap { border: 1px solid var(--border); border-radius: var(--rd-4, 6px); padding: var(--sp-4); }
/* Перезагрузка по фильтру: таблицу оставляем на месте, но гасим — так видно,
   что числа уже неактуальны, и не прыгает верстка. */
.table-wrap.busy { opacity: 0.45; pointer-events: none; transition: opacity 0.15s ease; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

.board-table { width: 100%; border-collapse: collapse; }
.board-table .num { text-align: right; font-family: var(--font-mono); font-variant-numeric: tabular-nums; white-space: nowrap; }
.col-line { min-width: 320px; position: sticky; left: 0; background: var(--bg-surface); }
.col-who { white-space: nowrap; }
.sec-row td { background: var(--bg-tonal); font-weight: var(--fw-bold); font-size: var(--fs-xs); text-transform: uppercase; letter-spacing: .04em; color: var(--text-secondary); padding: var(--sp-2) var(--sp-4); }
.line-row td { border-top: 1px solid var(--border); padding: var(--sp-2) var(--sp-4); }
.line-row.strong td { border-top: 2px solid var(--border-strong); font-weight: var(--fw-bold); }
.child-row td { padding: 2px var(--sp-4); font-size: var(--fs-sm); color: var(--text-secondary); background: var(--bg-tonal); }
.child-row.adj td { background: var(--warn-soft); }
.child-name { padding-left: var(--sp-5) !important; }
.tree { color: var(--text-muted); margin-right: 4px; }
.exp { border: none; background: none; cursor: pointer; color: var(--text-muted); padding: 0 4px 0 0; }
.exp-none { display: inline-block; width: 17px; }
.line-name { font-size: var(--fs-sm); }
.cp, .cfo, .le { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); margin-left: 6px; }
.fi { margin-left: 4px; color: var(--text-muted); width: 13px; height: 13px; cursor: help; vertical-align: middle; }
.adj-mark { color: var(--warn); margin-left: 4px; width: 13px; height: 13px; cursor: help; vertical-align: middle; }
.d-pos { color: var(--pos-strong); }
.d-neg { color: var(--neg-strong); }
.who-link { display: inline-flex; align-items: center; gap: 6px; color: inherit; }
.who-name { font-size: var(--fs-2xs); color: var(--text-secondary); }
.who-none { font-size: var(--fs-2xs); color: var(--text-muted); }
.prov-note { margin-top: var(--sp-3); font-size: var(--fs-2xs); color: var(--text-muted); }
</style>
