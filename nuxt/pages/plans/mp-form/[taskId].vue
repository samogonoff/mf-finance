<!--
  Полная форма ввода МП (task-driven), как прототип «Маркетплейсы_large»: строки =
  статьи P&L (продажи → %СПП → наценка → маржа розн/gross → COGS → прямые затраты →
  PL), колонки = площадки задания + Итого. Менеджер вводит ПРОДАЖИ с НДС (1046),
  правит %СПП/наценку и статьи затрат — производное считается ВЖИВУЮ (useMpCascade,
  зеркало go/internal/plans/mpform_calc.go). Факт/стратегия — read-only. ТЗ §7.2.3.
-->
<template>
  <div class="page-mpf">
    <header class="page-header">
      <div>
        <h1 class="page-title">Форма МП · {{ segmentTitle }}</h1>
        <p class="page-subtitle">
          {{ form?.task.title }} · {{ form?.year }}-{{ String(form?.month || 0).padStart(2, "0") }} ·
          показано {{ visiblePlatforms.length }} из {{ form?.platforms.length || 0 }} площадок ·
          НДС {{ Math.round((form?.vat || 0) * 100) }}% · {{ currency }}
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="backLink" class="btn btn-ghost"><Icon name="lucide:arrow-left" /> К процессу</NuxtLink>
        <label class="cur-switch" title="Валюта отображения (хранение — RUB)">
          <select v-model="currency" class="select select-sm" :disabled="dirty" @change="load">
            <option value="RUB">RUB</option>
            <option value="BYN">BYN</option>
            <option value="USD">USD</option>
          </select>
        </label>
        <button v-if="canEdit" class="btn btn-sm btn-ghost" :disabled="!hasStrategy" title="Копировать стратегический бюджет в тактику (TPL-09)" @click="copyStrategy"><Icon name="lucide:copy" /> Стратегия→тактика</button>
        <button v-if="canEdit" class="btn btn-sm btn-ghost" :disabled="!hasTarget" title="Копировать тактику-таргет (Budgeting) в форму" @click="copyTarget"><Icon name="lucide:copy-check" /> Таргет→тактика</button>
        <button class="btn btn-sm btn-ghost" @click="doExport"><Icon name="lucide:download" /> Экспорт</button>
        <button v-if="canEdit" class="btn btn-sm btn-ghost" @click="fileInput?.click()"><Icon name="lucide:upload" /> Импорт</button>
        <input ref="fileInput" type="file" accept=".xlsx" class="hidden-file" @change="doImport" />
        <button v-if="canEdit" class="btn btn-sm btn-primary" :disabled="saving" @click="save"><Icon name="lucide:save" :class="{ spin: saving }" /> Сохранить<span v-if="dirty" class="dirty-dot" title="есть несохранённые изменения">●</span></button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>
    <p v-if="form && !canEdit" class="banner banner-warn">Только просмотр — вы не исполнитель этого задания.</p>

    <!-- Одна форма на все МП: фильтры сужают показ, ввод по скрытым площадкам
         сохраняется как есть (фильтр — только представление). -->
    <div v-if="form && form.platforms.length > 1" class="mp-filters">
      <div class="mf">
        <span class="mf-lbl">Маркет</span>
        <select v-model="platformFilter" class="select select-sm">
          <option value="0">все площадки</option>
          <option v-for="p in form.platforms" :key="p.code_cfo" :value="String(p.code_cfo)">
            {{ p.name || p.code_cfo }}
          </option>
        </select>
      </div>
      <div v-if="segments.length > 1" class="mf">
        <span class="mf-lbl">Сегмент</span>
        <div class="chip-row">
          <button type="button" class="chip" :class="{ active: segmentFilter === '' }" @click="segmentFilter = ''">все</button>
          <button
            v-for="s in segments"
            :key="s"
            type="button"
            class="chip"
            :class="{ active: segmentFilter === s }"
            @click="segmentFilter = s"
          >{{ s === "large" ? "крупные" : "мелкие" }}</button>
        </div>
      </div>
      <div class="mf">
        <span class="mf-lbl">ЮЛ</span>
        <select v-model="legalFilter" class="select select-sm">
          <option value="">все ЮЛ</option>
          <option v-for="le in legalEntities" :key="le" :value="le">{{ le }}</option>
        </select>
      </div>
    </div>

    <div v-if="form" class="legend">
      <span class="lg lg-input">ввод</span>
      <span class="lg lg-calced">расчёт (правится)</span>
      <span class="lg lg-calc">расчёт</span>
      <span class="lg-hint">Менеджер задаёт «Продажи с НДС», %СПП и статьи затрат — остальное считается автоматически.</span>
    </div>

    <div v-if="form" class="card form-card">
      <div class="table-wrap">
        <table class="data-table mp-grid">
          <thead>
            <!-- Группировка колонок по сегменту — когда форма покрывает и крупные, и мелкие МП. -->
            <tr v-if="segmentGroups.length > 1" class="grp-row">
              <th class="col-line"></th>
              <th v-for="g in segmentGroups" :key="g.segment" :colspan="g.count" class="grp-head">
                {{ g.segment === "large" ? "Крупные МП" : "Мелкие МП" }}
              </th>
              <th></th>
            </tr>
            <tr>
              <th class="col-line">Показатель</th>
              <th v-for="p in visiblePlatforms" :key="p.code_cfo" class="col-plat num">
                {{ p.name || p.code_cfo }}<span class="cfo">ЦФО {{ p.code_cfo }}</span>
              </th>
              <th class="col-total num">Итого<span v-if="totalScoped" class="cfo">по фильтру</span></th>
            </tr>
          </thead>
          <tbody>
            <template v-for="sec in sections" :key="sec.name">
              <tr class="sec-row"><td :colspan="visiblePlatforms.length + 2">{{ sec.name }}</td></tr>
              <tr v-for="line in sec.lines" :key="line.block_type" class="line-row" :class="lineClass(line)">
                <td class="col-line">
                  <span class="line-name">{{ line.name }}</span>
                  <span v-if="line.code_pl" class="cp">PL {{ line.code_pl }}</span>
                  <Icon v-if="line.formula" name="lucide:info" class="fi" :title="line.formula" />
                </td>

                <!-- ячейки по площадкам -->
                <td v-for="p in visiblePlatforms" :key="p.code_cfo" class="cell" :class="cellClass(line, p.code_cfo)">
                  <template v-if="line.scope === 'total'">
                    <span class="muted">—</span>
                  </template>
                  <template v-else-if="editableCell(line)">
                    <div class="cell-row">
                      <!-- Проценты вводятся в процентах (31), хранятся долей (0.31). -->
                      <input
                        :value="displayInput(line, p.code_cfo)"
                        type="number" class="cell-input"
                        :class="{ pct: line.value_kind === 'pct' }"
                        :step="line.value_kind === 'pct' ? '0.1' : 'any'"
                        :disabled="!canEdit" :placeholder="line.value_kind === 'pct' ? '%' : '—'"
                        @input="onInput(line, p.code_cfo, ($event.target as HTMLInputElement).value)"
                      />
                      <span v-if="line.value_kind === 'pct'" class="pct-sign">%</span>
                      <button v-if="canEdit && line.kind === 'input'" class="corr-btn" :class="{ on: isCorr(p.code_cfo, line.block_type) }" title="Корректировка с причиной (ADJ-02)" @click="toggleCorr(p.code_cfo, line.block_type)"><Icon name="lucide:pencil" /></button>
                    </div>
                    <span v-if="line.kind === 'input' && factOf(p.code_cfo, line.block_type) != null" class="fact">факт: {{ fmtMoney(factOf(p.code_cfo, line.block_type)) }}</span>
                    <span v-if="prevOf(p.code_cfo, line.block_type) != null" class="prev">пр. год: {{ fmt(line, prevOf(p.code_cfo, line.block_type)) }}</span>
                    <span v-if="stratOf(p.code_cfo, line.block_type) != null" class="strat">страт: {{ fmt(line, stratOf(p.code_cfo, line.block_type)) }}</span>
                    <span v-if="targetOf(p.code_cfo, line.block_type) != null" class="target">таргет: {{ fmt(line, targetOf(p.code_cfo, line.block_type)) }}</span>
                    <input v-if="isCorr(p.code_cfo, line.block_type)" v-model="corr[key(p.code_cfo, line.block_type)]" class="corr-reason" :disabled="!canEdit" placeholder="причина корректировки…" />
                  </template>
                  <template v-else>
                    <span class="calc-val" :class="{ neg: cellValue(line, p.code_cfo) < 0 }">{{ fmt(line, cellValue(line, p.code_cfo)) }}</span>
                    <span v-if="line.cost_line" class="share">доля {{ fmtPct(shareOf(line, p.code_cfo)) }}</span>
                  </template>
                </td>

                <!-- Итого -->
                <td class="cell total-cell">
                  <template v-if="line.scope === 'total' && editableCell(line)">
                    <input v-model.number="totals[line.block_type]" type="number" class="cell-input" :disabled="!canEdit" placeholder="—" @input="dirty = true" />
                  </template>
                  <template v-else>
                    <span class="calc-val" :class="{ neg: totalValue(line) < 0 }">{{ fmt(line, totalValue(line)) }}</span>
                  </template>
                </td>
              </tr>
            </template>
            <tr v-if="!form.platforms.length"><td :colspan="2" class="empty-cell">У задания нет площадок (ЦФО не размечены).</td></tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useTasks, type MpTaskForm, type MpLine, type MpSaveRow, type MpFormCell } from "~/composables/useTasks";
import { computePlatform, vatByCountry, B } from "~/composables/useMpCascade";
import { num } from "~/utils/format";

definePageMeta({ middleware: "scope-guard" });

const route = useRoute();
const taskId = Number(route.params.taskId);
const api = useTasks();
const { user } = useAuth();
const { hasRole, isAdmin } = useScope();

const form = ref<MpTaskForm | null>(null);
const inputs = reactive<Record<string, number | null>>({});      // ключ cfo:block → ввод (площадочные editable)
const totals = reactive<Record<string, number | null>>({});      // block → ввод (тотал-строки)
const corr = reactive<Record<string, string>>({});               // ключ ячейки → причина корректировки
const error = ref("");
const note = ref("");
const saving = ref(false);
const dirty = ref(false);
// Валюта отображения; хранение тактики всегда в RUB (пересчёт делает сервер).
// Переключение заблокировано при несохранённых правках — иначе они потеряются.
const currency = ref("RUB");
const fileInput = ref<HTMLInputElement | null>(null);

const key = (cfo: number, block: string) => `${cfo}:${block}`;
const lineMap = computed<Record<string, MpLine>>(() => {
  const m: Record<string, MpLine> = {};
  for (const l of form.value?.lines || []) m[l.block_type] = l;
  return m;
});
// Строки, чей ввод сидируется из вычисленного значения (а не из факта): %СПП, наценка.
const SEED_FROM_VALUE = new Set([B.spp, B.markup]);

const sections = computed(() => {
  const out: { name: string; lines: MpLine[] }[] = [];
  for (const l of form.value?.lines || []) {
    if (l.kind === "header") continue;
    let s = out.find((x) => x.name === l.section);
    if (!s) { s = { name: l.section, lines: [] }; out.push(s); }
    s.lines.push(l);
  }
  return out;
});

// Одна форма на все МП (миграция 0029): площадки обоих сегментов приходят одним
// заданием, фильтры ниже — только представление, они не влияют на сохранение.
const platformFilter = ref("0");
const segmentFilter = ref("");
const legalFilter = ref("");

const segments = computed(() =>
  [...new Set((form.value?.platforms || []).map((p) => p.segment).filter(Boolean))].sort()
);
const legalEntities = computed(() =>
  [...new Set((form.value?.platforms || []).map((p) => p.legal_entity).filter(Boolean))].sort()
);
const visiblePlatforms = computed(() =>
  (form.value?.platforms || []).filter((p) => {
    if (platformFilter.value !== "0" && String(p.code_cfo) !== platformFilter.value) return false;
    if (segmentFilter.value && p.segment !== segmentFilter.value) return false;
    if (legalFilter.value && p.legal_entity !== legalFilter.value) return false;
    return true;
  })
);
const totalScoped = computed(() => visiblePlatforms.value.length !== (form.value?.platforms.length || 0));
const segmentTitle = computed(() => {
  if (segments.value.length > 1) return "все площадки";
  return segments.value[0] === "small" ? "мелкие МП" : "крупные МП";
});
// Порядок колонок = порядок площадок; группы считаем по соседним одинаковым сегментам.
const segmentGroups = computed(() => {
  const out: { segment: string; count: number }[] = [];
  for (const p of visiblePlatforms.value) {
    const last = out[out.length - 1];
    if (last && last.segment === p.segment) last.count++;
    else out.push({ segment: p.segment, count: 1 });
  }
  return out;
});

const editableCell = (line: MpLine) => canEdit.value && line.editable;
const canEdit = computed(() => {
  if (isAdmin.value || hasRole("ROLE_PLANS_ADMIN")) return true;
  const uid = user.value?.id;
  const t = form.value?.task;
  return !!t && (t.assignee_user_id === uid || t.delegate_user_id === uid);
});
const backLink = computed(() => (form.value ? `/plans/process/${form.value.task.pl_id}` : "/plans"));

// Живой пересчёт каскада по каждой площадке из введённых значений.
const platformValues = computed<Record<number, Record<string, number>>>(() => {
  const map: Record<number, Record<string, number>> = {};
  for (const p of form.value?.platforms || []) {
    const inp: Record<string, number | null | undefined> = {};
    for (const l of form.value?.lines || []) {
      if (l.scope === "platform" && l.editable) inp[l.block_type] = inputs[key(p.code_cfo, l.block_type)];
    }
    map[p.code_cfo] = computePlatform(inp, vatByCountry(p.country));
  }
  return map;
});

const cellValue = (line: MpLine, cfo: number): number => platformValues.value[cfo]?.[line.block_type] ?? 0;
// Итог считается по ВИДИМЫМ площадкам: отфильтровав срез, пользователь ждёт итог
// именно по нему. Каскад при этом считается по всем — ввод скрытых не теряется.
const sumBlock = (block: string): number =>
  visiblePlatforms.value.reduce((s, p) => s + (platformValues.value[p.code_cfo]?.[block] ?? 0), 0);

// Итого по строке: сумма денег; проценты пересчитываются из агрегатов.
const totalValue = (line: MpLine): number => {
  if (line.scope === "total") return Number(totals[line.block_type]) || 0;
  if (line.value_kind === "money") return sumBlock(line.block_type);
  const net = sumBlock(B.salesPlatNet);
  switch (line.block_type) {
    case B.spp: { const mgr = sumBlock(B.salesManagerNet); return mgr === 0 ? 0 : 1 - net / mgr; }
    case B.markup: { const c = sumBlock(B.shipments); return c === 0 ? 0 : net / c - 1; }
    case B.markupTotal: { const c = sumBlock(B.cogsTotal); return c === 0 ? 0 : net / c - 1; }
    case B.retailMarginPct: return net === 0 ? 0 : sumBlock(B.retailMargin) / net;
    case B.grossMarginPct: return net === 0 ? 0 : sumBlock(B.grossMargin) / net;
    case B.plPlatformPct: return net === 0 ? 0 : sumBlock(B.plPlatform) / net;
    case B.directShare: { const mgr = sumBlock(B.salesManagerNet); return mgr === 0 ? 0 : sumBlock(B.platformCosts) / mgr; }
    default: return 0;
  }
};

// Доля статьи затрат в выручке по ценам менеджера (без НДС).
const shareOf = (line: MpLine, cfo: number): number => {
  const mgrNet = platformValues.value[cfo]?.[B.salesManagerNet] ?? 0;
  return mgrNet === 0 ? 0 : cellValue(line, cfo) / mgrNet;
};

const cellRef = (cfo: number, block: string) => form.value?.cells.find((c) => c.code_cfo === cfo && c.block_type === block);
const factOf = (cfo: number, block: string): number | null => cellRef(cfo, block)?.fact ?? null;
const prevOf = (cfo: number, block: string): number | null => cellRef(cfo, block)?.fact_prev ?? null;
const stratOf = (cfo: number, block: string): number | null => cellRef(cfo, block)?.strategy ?? null;
const targetOf = (cfo: number, block: string): number | null => cellRef(cfo, block)?.target ?? null;
const hasStrategy = computed(() => !!form.value?.cells.some((c) => c.strategy != null));
const hasTarget = computed(() => !!form.value?.cells.some((c) => c.target != null));

// Ввод процентов — в процентах: в поле 31, в модели 0.31 (финансисты вводят «31»,
// а не долю). Округление гасит артефакты float при ×100.
const displayInput = (line: MpLine, cfo: number): number | null => {
  const v = inputs[key(cfo, line.block_type)];
  if (v == null || Number.isNaN(v)) return null;
  return line.value_kind === "pct" ? Number((v * 100).toFixed(4)) : v;
};
const onInput = (line: MpLine, cfo: number, raw: string) => {
  const k = key(cfo, line.block_type);
  if (raw === "") { inputs[k] = null; dirty.value = true; return; }
  const n = Number(raw);
  if (Number.isNaN(n)) return;
  inputs[k] = line.value_kind === "pct" ? Number((n / 100).toFixed(6)) : n;
  dirty.value = true;
};

const isCorr = (cfo: number, block: string) => key(cfo, block) in corr;
const toggleCorr = (cfo: number, block: string) => {
  const k = key(cfo, block);
  if (k in corr) delete corr[k]; else corr[k] = "";
};

const lineClass = (line: MpLine) => `k-${line.kind}` + (line.block_type === B.plPlatform || line.block_type === B.platformCosts ? " strong" : "");
const cellClass = (line: MpLine, cfo: number) => (isCorr(cfo, line.block_type) ? "is-corr" : "");

// Форматирование по типу строки.
const fmtMoney = (v: number | null | undefined) => (v == null ? "—" : num(v, 0));
const fmtPct = (v: number | null | undefined) => (v == null || Number.isNaN(v) ? "—" : (v * 100).toFixed(1) + "%");
const fmt = (line: MpLine, v: number | null | undefined) => (line.value_kind === "pct" ? fmtPct(v) : fmtMoney(v));

const seedFromCells = () => {
  for (const k of Object.keys(inputs)) delete inputs[k];
  for (const k of Object.keys(totals)) delete totals[k];
  for (const c of form.value?.cells || []) {
    const line = lineMap.value[c.block_type];
    if (!line?.editable) continue;
    if (line.scope === "total") {
      totals[c.block_type] = c.tactic ?? c.value ?? 0;
    } else {
      const seed = c.tactic ?? (SEED_FROM_VALUE.has(c.block_type) ? c.value : c.fact) ?? 0;
      inputs[key(c.code_cfo, c.block_type)] = seed;
      if (c.is_manual) corr[key(c.code_cfo, c.block_type)] = c.reason || "";
    }
  }
};

// Перенос read-only сценария в тактику: стратегия (TPL-09) или таргет из Budgeting.
const copyScenario = (pick: (c: MpFormCell) => number | null, what: string) => {
  if (!form.value) return;
  let cnt = 0;
  for (const c of form.value.cells) {
    const v = pick(c);
    if (v == null) continue;
    const line = lineMap.value[c.block_type];
    if (!line?.editable) continue;
    if (line.scope === "total") totals[c.block_type] = v;
    else inputs[key(c.code_cfo, c.block_type)] = v;
    cnt++;
  }
  if (cnt) { dirty.value = true; note.value = `Скопировано из «${what}»: ${cnt} значений`; }
};
const copyStrategy = () => copyScenario((c) => c.strategy, "стратегия");
const copyTarget = () => copyScenario((c) => c.target, "таргет");

const load = async () => {
  try {
    form.value = await api.mpForm(taskId, currency.value);
    seedFromCells();
    dirty.value = false;
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка загрузки формы"; }
};

const save = async () => {
  if (!form.value) return;
  saving.value = true; error.value = ""; note.value = "";
  const rows: MpSaveRow[] = [];
  for (const line of form.value.lines) {
    if (!line.editable) continue;
    if (line.scope === "total") {
      const v = totals[line.block_type];
      if (v != null && !Number.isNaN(v)) rows.push({ code_cfo: 0, block_type: line.block_type, code_pl: line.code_pl, amount: Number(v), is_manual: false, comment: "" });
      continue;
    }
    for (const p of form.value.platforms) {
      const k = key(p.code_cfo, line.block_type);
      const v = inputs[k];
      if (v == null || Number.isNaN(v)) continue;
      const manual = k in corr;
      if (manual && !corr[k].trim()) { error.value = `Укажите причину корректировки (${p.name || p.code_cfo} · ${line.name})`; saving.value = false; return; }
      rows.push({ code_cfo: p.code_cfo, block_type: line.block_type, code_pl: line.code_pl, amount: Number(v), is_manual: manual, comment: manual ? corr[k].trim() : "" });
    }
  }
  try {
    await api.saveMpForm(taskId, rows, currency.value);
    note.value = `Сохранено ${rows.length} значений`;
    dirty.value = false;
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка сохранения"; }
  finally { saving.value = false; }
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
.page-actions { display: flex; gap: var(--sp-3); flex-wrap: wrap; }

.legend { display: flex; align-items: center; gap: var(--sp-3); margin-bottom: var(--sp-4); font-size: var(--fs-2xs); flex-wrap: wrap; }
.lg { padding: 1px 8px; border-radius: 999px; font-weight: var(--fw-medium); }
.lg-input { background: var(--accent-soft, #eef); color: var(--accent); }
.lg-calced { background: var(--warn-soft); color: var(--warn); }
.lg-calc { background: var(--bg-tonal); color: var(--text-secondary); }
.lg-hint { color: var(--text-muted); }

.form-card { padding: 0; overflow: hidden; }
.mp-grid { border-collapse: collapse; width: 100%; }
.mp-grid th, .mp-grid td { vertical-align: top; }
.col-line { min-width: 300px; position: sticky; left: 0; background: var(--bg-surface); z-index: 1; }
.col-plat, .col-total { min-width: 150px; text-align: right; }
.cfo { display: block; font-size: var(--fs-2xs); color: var(--text-muted); font-family: var(--font-mono); font-weight: var(--fw-normal); }

.sec-row td { background: var(--bg-tonal); font-weight: var(--fw-bold); font-size: var(--fs-xs); text-transform: uppercase; letter-spacing: .04em; color: var(--text-secondary); padding: var(--sp-2) var(--sp-4); position: sticky; left: 0; }
.line-row td { border-top: 1px solid var(--border); padding: var(--sp-2) var(--sp-4); }
.line-row.strong td { border-top: 2px solid var(--border-strong); font-weight: var(--fw-bold); }
.line-name { font-size: var(--fs-sm); }
.cp { display: inline-block; margin-left: 6px; font-size: var(--fs-2xs); color: var(--text-muted); font-family: var(--font-mono); }
.fi { margin-left: 4px; color: var(--text-muted); width: 13px; height: 13px; cursor: help; vertical-align: middle; }

.cell { text-align: right; }
.cell.is-corr { background: var(--warn-soft); }
.cell-row { display: flex; align-items: center; gap: 4px; justify-content: flex-end; }
.cell-input { width: 120px; text-align: right; font-family: var(--font-mono); font-variant-numeric: tabular-nums; border: 1px solid var(--border); border-radius: var(--rd-3); padding: 3px 7px; background: var(--bg-surface); }
.cell-input:focus { border-color: var(--accent); outline: none; }
.cell-input:disabled { background: var(--bg-tonal); color: var(--text-secondary); }
.cell-input.pct { width: 70px; }
.corr-btn { border: 1px solid var(--border); background: var(--bg-surface); border-radius: var(--rd-3); padding: 3px; cursor: pointer; color: var(--text-muted); display: inline-flex; }
.corr-btn.on { background: var(--warn-soft); color: var(--warn); border-color: var(--warn); }
.corr-reason { width: 100%; margin-top: 4px; font-size: var(--fs-2xs); border: 1px solid var(--warn); border-radius: var(--rd-3); padding: 3px 6px; background: var(--bg-surface); text-align: left; }

.calc-val { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: var(--fs-sm); }
.calc-val.neg { color: var(--neg-strong); }
.k-calc .calc-val { color: var(--text-secondary); }
.hint-val, .fact, .prev, .strat, .target, .share { display: block; font-size: var(--fs-2xs); font-family: var(--font-mono); margin-top: 2px; }
.hint-val { color: var(--warn); }
.fact { color: var(--text-muted); }
.prev { color: var(--text-muted); }
.strat { color: var(--accent); }
.target { color: var(--pos-strong); }
.share { color: var(--text-muted); }
.pct-sign { font-size: var(--fs-2xs); color: var(--text-muted); }
.cur-switch .select-sm { height: 30px; }

.mp-filters { display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--sp-4); margin-bottom: var(--sp-4); }
.mf { display: flex; flex-direction: column; gap: 2px; }
.mf-lbl { font-size: var(--fs-2xs); color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.03em; }
.mf .select-sm { height: 30px; }
.chip-row { display: flex; gap: 4px; }
.chip { border: 1px solid var(--border); background: var(--bg-surface); color: var(--text-secondary); border-radius: 999px; padding: 3px 12px; font-size: var(--fs-2xs); cursor: pointer; }
.chip.active { background: var(--accent-soft); color: var(--accent); border-color: var(--accent); }
.grp-row .grp-head { text-align: center; font-size: var(--fs-2xs); text-transform: uppercase; letter-spacing: .04em; color: var(--text-secondary); background: var(--bg-tonal); border-bottom: 1px solid var(--border); }
.muted { color: var(--text-muted); }

.total-cell { text-align: right; background: var(--bg-tonal); font-weight: var(--fw-medium); }
.dirty-dot { color: var(--warn); margin-left: 4px; font-size: 10px; vertical-align: middle; }
.empty-cell { text-align: center; color: var(--text-muted); padding: var(--sp-6); }
.hidden-file { display: none; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
