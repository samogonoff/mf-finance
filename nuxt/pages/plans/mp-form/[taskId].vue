<!--
  Новая форма ввода МП (task-driven). Площадки = ЦФО задания (large/small по
  сегменту). Матрица площадка × блок: тактика — ввод, факт — read-only. Сохранение
  пишет pl_metric под период задания; результат виден в «Данные» задания и проверяется
  на этапе. ТЗ §7.2.3.
-->
<template>
  <div class="page-mpf">
    <header class="page-header">
      <div>
        <h1 class="page-title">Форма МП · {{ form?.segment === "small" ? "small" : "large" }}</h1>
        <p class="page-subtitle">{{ form?.task.title }} · {{ form?.year }}-{{ String(form?.month || 0).padStart(2, "0") }} · {{ form?.platforms.length || 0 }} площадок</p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="backLink" class="btn btn-ghost"><Icon name="lucide:arrow-left" /> К процессу</NuxtLink>
        <button v-if="canEdit" class="btn btn-sm btn-ghost" :disabled="!hasStrategy" title="Копировать стратегический бюджет в тактику (TPL-09)" @click="copyStrategy"><Icon name="lucide:copy" /> Стратегия→тактика</button>
        <button class="btn btn-sm btn-ghost" @click="recompute"><Icon name="lucide:calculator" /> Пересчитать</button>
        <button class="btn btn-sm btn-ghost" @click="doExport"><Icon name="lucide:download" /> Экспорт</button>
        <button v-if="canEdit" class="btn btn-sm btn-ghost" @click="fileInput?.click()"><Icon name="lucide:upload" /> Импорт</button>
        <input ref="fileInput" type="file" accept=".xlsx" class="hidden-file" @change="doImport" />
        <button v-if="canEdit" class="btn btn-sm btn-primary" :disabled="saving" @click="save"><Icon name="lucide:save" :class="{ spin: saving }" /> Сохранить<span v-if="dirty" class="dirty-dot" title="есть несохранённые изменения">●</span></button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>
    <p v-if="!canEdit" class="banner banner-warn">Только просмотр — вы не исполнитель этого задания.</p>

    <!-- Превью пересчёта (CALC) -->
    <div v-if="computedRows.length" class="card compute-card">
      <div class="cc-head"><b>Расчёт (CALC) — предпросмотр</b><button class="btn btn-icon btn-sm" @click="computedRows = []"><Icon name="lucide:x" /></button></div>
      <div class="table-wrap">
        <table class="data-table">
          <thead><tr><th>Площадка</th><th v-for="k in computeKeys" :key="k" class="num">{{ k }}</th></tr></thead>
          <tbody>
            <tr v-for="r in computedRows" :key="r.code_cfo">
              <td>{{ r.name_cfo || r.code_cfo }}</td>
              <td v-for="k in computeKeys" :key="k" class="num">{{ numFmt(r.values[k]) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div v-if="form" class="card form-card">
      <div class="table-wrap">
        <table class="data-table mp-grid">
          <thead>
            <tr>
              <th class="col-plat">Площадка</th>
              <th v-for="b in form.blocks" :key="b.block_type" class="col-block">{{ b.name }}<span class="cp">CodePL {{ b.code_pl }}</span></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in form.platforms" :key="p.code_cfo">
              <td class="col-plat"><b>{{ p.name || p.code_cfo }}</b><span class="cfo">ЦФО {{ p.code_cfo }}</span></td>
              <td v-for="b in form.blocks" :key="b.block_type" class="cell" :class="{ 'is-corr': isCorr(p.code_cfo, b.block_type) }">
                <div class="cell-row">
                  <input
                    v-model.number="grid[key(p.code_cfo, b.block_type)]"
                    type="number"
                    class="cell-input"
                    :disabled="!canEdit"
                    placeholder="—"
                    @input="dirty = true"
                  />
                  <button v-if="canEdit" class="corr-btn" :class="{ on: isCorr(p.code_cfo, b.block_type) }" title="Корректировка с причиной" @click="toggleCorr(p.code_cfo, b.block_type)"><Icon name="lucide:pencil" /></button>
                </div>
                <span class="fact">факт: {{ factOf(p.code_cfo, b.block_type) }}</span>
                <span class="strat">страт: {{ stratOf(p.code_cfo, b.block_type) }}</span>
                <input
                  v-if="isCorr(p.code_cfo, b.block_type)"
                  v-model="corr[key(p.code_cfo, b.block_type)]"
                  class="corr-reason"
                  :disabled="!canEdit"
                  placeholder="причина корректировки…"
                />
              </td>
            </tr>
            <tr v-if="!form.platforms.length"><td :colspan="form.blocks.length + 1" class="empty-cell">У задания нет площадок (ЦФО не размечены).</td></tr>
          </tbody>
          <tfoot v-if="form.platforms.length">
            <tr class="totals-row">
              <td class="col-plat"><b>Итого (тактика)</b></td>
              <td v-for="b in form.blocks" :key="b.block_type" class="cell total-cell">{{ numFmt(blockTotal(b.block_type)) }}</td>
            </tr>
          </tfoot>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useTasks, type MpTaskForm, type MpSaveRow } from "~/composables/useTasks";
import { usePlans, type ComputedRow } from "~/composables/usePlans";
import { num } from "~/utils/format";

definePageMeta({ middleware: "scope-guard" });

const route = useRoute();
const taskId = Number(route.params.taskId);
const api = useTasks();
const { user } = useAuth();
const { hasRole, isAdmin } = useScope();

const form = ref<MpTaskForm | null>(null);
const grid = reactive<Record<string, number | null>>({});
const corr = reactive<Record<string, string>>({}); // ключ ячейки → причина корректировки
const error = ref("");
const note = ref("");
const saving = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);
const computedRows = ref<ComputedRow[]>([]);
const computeKeys = computed(() => (computedRows.value.length ? Object.keys(computedRows.value[0].values) : []));
const numFmt = (v: number | undefined) => (v === undefined ? "—" : num(v, 0));
const dirty = ref(false);

const cellOf = (cfo: number, block: string) => form.value?.cells.find((x) => x.code_cfo === cfo && x.block_type === block);
const stratOf = (cfo: number, block: string) => { const c = cellOf(cfo, block); return c?.strategy != null ? num(c.strategy, 0) : "—"; };
const hasStrategy = computed(() => !!form.value?.cells.some((c) => c.strategy != null));
const blockTotal = (block: string) => {
  let sum = 0;
  for (const p of form.value?.platforms || []) {
    const v = grid[key(p.code_cfo, block)];
    if (typeof v === "number" && !Number.isNaN(v)) sum += v;
  }
  return sum;
};
const copyStrategy = () => {
  if (!form.value) return;
  let n = 0;
  for (const c of form.value.cells) {
    if (c.strategy != null) { grid[key(c.code_cfo, c.block_type)] = c.strategy; n++; }
  }
  if (n) { dirty.value = true; note.value = `Скопировано из стратегии: ${n} ячеек`; }
};

const key = (cfo: number, block: string) => `${cfo}:${block}`;
const canEdit = computed(() => {
  if (isAdmin.value || hasRole("ROLE_PLANS_ADMIN")) return true;
  const uid = user.value?.id;
  const t = form.value?.task;
  return !!t && (t.assignee_user_id === uid || t.delegate_user_id === uid);
});
const backLink = computed(() => form.value ? `/plans/process/${form.value.task.pl_id}` : "/plans");

const factOf = (cfo: number, block: string) => {
  const c = form.value?.cells.find((x) => x.code_cfo === cfo && x.block_type === block);
  return c?.fact != null ? num(c.fact, 0) : "—";
};
const isCorr = (cfo: number, block: string) => key(cfo, block) in corr;
const toggleCorr = (cfo: number, block: string) => {
  const k = key(cfo, block);
  if (k in corr) delete corr[k];
  else corr[k] = "";
};

const load = async () => {
  try {
    form.value = await api.mpForm(taskId);
    for (const c of form.value.cells) {
      grid[key(c.code_cfo, c.block_type)] = c.tactic;
      if (c.is_manual) corr[key(c.code_cfo, c.block_type)] = c.reason || "";
    }
    dirty.value = false;
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка загрузки формы"; }
};

const save = async () => {
  if (!form.value) return;
  saving.value = true; error.value = ""; note.value = "";
  const rows: MpSaveRow[] = [];
  for (const p of form.value.platforms) {
    for (const b of form.value.blocks) {
      const k = key(p.code_cfo, b.block_type);
      const v = grid[k];
      if (v !== null && v !== undefined && !Number.isNaN(v)) {
        const manual = k in corr;
        if (manual && !corr[k].trim()) {
          error.value = `Укажите причину корректировки (${p.name || p.code_cfo})`;
          saving.value = false;
          return;
        }
        rows.push({ code_cfo: p.code_cfo, block_type: b.block_type, code_pl: b.code_pl, amount: Number(v), is_manual: manual, comment: manual ? corr[k].trim() : "" });
      }
    }
  }
  try {
    await api.saveMpForm(taskId, rows);
    note.value = `Сохранено ${rows.length} значений`;
    dirty.value = false;
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка сохранения"; }
  finally { saving.value = false; }
};

const recompute = async () => {
  if (!form.value) return;
  error.value = "";
  try {
    const seg = form.value.segment === "small" ? "small" : "large";
    const all = await usePlans().mpCompute({ year: form.value.year, month: form.value.month, segment: seg, currency: form.value.currency });
    const codes = new Set(form.value.platforms.map((p) => p.code_cfo));
    computedRows.value = all.filter((r) => codes.has(r.code_cfo));
    if (!computedRows.value.length) note.value = "Расчёт пуст (нет данных/правил для этих ЦФО)";
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка пересчёта"; }
};
const doExport = async () => {
  try {
    const blob = await api.exportMpForm(taskId);
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url; a.download = `TPL-MP_task${taskId}.xlsx`; a.click();
    URL.revokeObjectURL(url);
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка экспорта"; }
};
const doImport = async (ev: Event) => {
  const f = (ev.target as HTMLInputElement).files?.[0];
  if (!f) return;
  error.value = ""; note.value = "";
  try {
    const r = await api.importMpForm(taskId, f);
    note.value = `Импортировано ${r.imported} значений`;
    await load();
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка импорта"; }
  finally { if (fileInput.value) fileInput.value.value = ""; }
};

onMounted(load);
</script>

<style scoped>
.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-4, 6px); margin-bottom: var(--sp-5); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.banner-pos { background: var(--pos-soft); color: var(--pos-strong); }
.banner-warn { background: var(--warn-soft); color: var(--warn); }
.page-actions { display: flex; gap: var(--sp-3); }
.form-card { padding: var(--sp-4) var(--sp-5); }
.mp-grid th, .mp-grid td { vertical-align: top; }
.col-plat { min-width: 200px; }
.col-block { text-align: center; }
.cp, .cfo { display: block; font-size: var(--fs-2xs); color: var(--text-muted); font-family: var(--font-mono); font-weight: var(--fw-normal); }
.cell { text-align: right; }
.cell.is-corr { background: var(--warn-soft); }
.cell-row { display: flex; align-items: center; gap: 4px; justify-content: flex-end; }
.cell-input { width: 130px; text-align: right; font-family: var(--font-mono); border: 1px solid var(--border); border-radius: var(--rd-3); padding: 4px 8px; background: var(--bg-surface); }
.cell-input:focus { border-color: var(--accent); outline: none; }
.cell-input:disabled { background: var(--bg-tonal); color: var(--text-secondary); }
.corr-btn { border: 1px solid var(--border); background: var(--bg-surface); border-radius: var(--rd-3); padding: 4px; cursor: pointer; color: var(--text-muted); display: inline-flex; }
.corr-btn.on { background: var(--warn-soft); color: var(--warn); border-color: var(--warn); }
.corr-reason { width: 100%; margin-top: 4px; font-size: var(--fs-2xs); border: 1px solid var(--warn); border-radius: var(--rd-3); padding: 3px 6px; background: var(--bg-surface); text-align: left; }
.fact { display: block; font-size: var(--fs-2xs); color: var(--text-muted); font-family: var(--font-mono); margin-top: 2px; }
.strat { display: block; font-size: var(--fs-2xs); color: var(--accent); font-family: var(--font-mono); }
.totals-row td { border-top: 2px solid var(--border-strong); font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.total-cell { text-align: right; font-weight: var(--fw-bold); }
.dirty-dot { color: var(--warn); margin-left: 4px; font-size: 10px; vertical-align: middle; }
.empty-cell { text-align: center; color: var(--text-muted); padding: var(--sp-6); }
.hidden-file { display: none; }
.compute-card { padding: var(--sp-4) var(--sp-5); margin-bottom: var(--sp-4); }
.cc-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: var(--sp-3); }
.compute-card .num { text-align: right; font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
