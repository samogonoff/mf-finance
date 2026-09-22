<!--
  Реестры формы этапа 1.5 (ТЗ §5): ФОТ, командировки, логистика по ЦФО 250 и
  ИТ-расходы. Четыре вкладки одного экрана, а не четыре страницы: реестры
  наполняют статьи ОДНОЙ формы, и переключаться между ними приходится постоянно.

  Уточнения ответов аналитика 10.09.2026:
   - 38: у реестра ФОТ НЕТ колонок налогов и ставки соцвзносов — он содержит
     только «ФОТ чистый», налоги от выплат это самостоятельные статьи;
   - Н3: в командировках билеты считаются по нормативу перелёта, а проживание
     публикуется отдельной статьёй 30;
   - Н4: реестр открывается ПУСТЫМ, кнопки «скопировать строки прошлого года»
     здесь нет намеренно — нормативы меняются ежегодно.
-->
<template>
  <div class="page-reg">
    <header class="page-header">
      <div>
        <h1 class="page-title">Реестры расходов · {{ cardTitle }}</h1>
        <p class="page-subtitle">
          Этап 1.5 · итоги реестров подставляются в статьи формы и там не редактируются
          <span v-if="readOnly" class="lock-note"><Icon name="lucide:lock" /> реестр закрыт вместе с формой</span>
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="`/plans/exp-form/${cardId}`" class="btn btn-ghost">
          <Icon name="lucide:arrow-left" /> К форме
        </NuxtLink>
        <button class="btn btn-sm btn-primary" :disabled="readOnly || busy" @click="addRow">
          <Icon name="lucide:plus" /> Добавить строку
        </button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>

    <div class="segmented" role="tablist">
      <button
        v-for="k in KINDS"
        :key="k"
        class="seg"
        :class="{ active: kind === k }"
        @click="switchKind(k)"
      >{{ EXP_REG_TITLES[k] }}</button>
    </div>

    <p class="banner banner-info">{{ kindHint }}</p>

    <SkeletonTable v-if="loading" :rows="8" :cols="14" label="Загружаю реестр" />

    <section v-else class="card reg-card">
      <div class="reg-scroll">
        <table class="data-table compact">
          <thead>
            <tr>
              <th v-for="c in columns" :key="c.key" :class="c.num ? 'col-num' : ''">{{ c.label }}</th>
              <template v-if="hasMonthColumns">
                <th v-for="m in MONTHS_SHORT" :key="m" class="col-num c-mon">{{ m }}</th>
              </template>
              <template v-else>
                <th v-for="c in calcColumns" :key="c.key" class="col-num c-calc">{{ c.label }}</th>
              </template>
              <th class="col-num c-total">Итого</th>
              <th class="c-act">Строка</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!rows.length">
              <td :colspan="columns.length + (hasMonthColumns ? 14 : calcColumns.length + 2)" class="reg-empty">
                Реестр пуст. Строки заводятся вручную — переноса из прошлого года нет намеренно:
                нормативы меняются ежегодно.
              </td>
            </tr>
            <tr v-for="row in rows" :key="row.id" :class="{ 'row-off': row.excluded }">
              <td v-for="c in columns" :key="c.key" :class="c.num ? 'col-num' : ''">
                <label v-if="c.type === 'flag'" class="flag">
                  <input
                    type="checkbox"
                    :checked="!!(row as any)[c.key]"
                    :disabled="readOnly"
                    @change="setFlag(row, c.key, ($event.target as HTMLInputElement).checked)"
                  />
                </label>
                <input
                  v-else
                  class="cell-input txt"
                  :class="{ num: c.num }"
                  :value="fieldValue(row, c.key)"
                  :disabled="readOnly"
                  :placeholder="c.hint || ''"
                  @change="setField(row, c.key, ($event.target as HTMLInputElement).value, c.num)"
                />
              </td>
              <template v-if="hasMonthColumns">
                <td v-for="m in 12" :key="m" class="col-num c-mon">
                  <input
                    class="cell-input num"
                    :value="monthValue(row, m)"
                    :disabled="readOnly"
                    inputmode="decimal"
                    @change="setMonth(row, m, ($event.target as HTMLInputElement).value)"
                  />
                </td>
              </template>
              <!-- Командировки: расчётные части строки (находка Н3), только чтение.
                   Считает сервер по справочникам нормативов — руками их не правят. -->
              <template v-else>
                <td v-for="c in calcColumns" :key="c.key" class="col-num c-calc calc-cell" :title="c.hint">
                  {{ money(Number(row.attrs?.[c.key] || 0)) }}
                </td>
              </template>
              <td class="col-num c-total strong">{{ money(rowTotal(row)) }}</td>
              <td class="c-act">
                <div class="row-acts">
                  <button class="btn btn-xs btn-ghost" :disabled="readOnly || busy" @click="copyRow(row)">копия</button>
                  <button class="btn btn-xs btn-ghost" :disabled="readOnly || busy" @click="removeRow(row)">удалить</button>
                </div>
              </td>
            </tr>
          </tbody>
          <tfoot v-if="rows.length">
            <tr>
              <td :colspan="columns.length" class="strong">Итого реестра</td>
              <template v-if="hasMonthColumns">
                <td v-for="m in 12" :key="m" class="col-num strong">{{ money(monthTotal(m)) }}</td>
              </template>
              <template v-else>
                <td v-for="c in calcColumns" :key="c.key" class="col-num strong">{{ money(calcTotal(c.key)) }}</td>
              </template>
              <td class="col-num strong">{{ money(yearTotal) }}</td>
              <td></td>
            </tr>
          </tfoot>
        </table>
      </div>

      <!-- Итоги ИТ-реестра (ТЗ §5.4): перенос, тотал и общие расходы — три
           разных числа, и путать их нельзя. -->
      <div v-if="kind === 'it' && registry" class="it-totals">
        <div class="itt"><span class="itt-l">Перенос на ЦФО 250</span><span class="itt-v">{{ money(registry.totals.transfer || 0) }}</span></div>
        <div class="itt"><span class="itt-l">Тотал</span><span class="itt-v">{{ money(registry.totals.total || 0) }}</span></div>
        <div class="itt strong"><span class="itt-l">Общие расходы</span><span class="itt-v">{{ money(registry.totals.common || 0) }}</span></div>
        <div class="itt muted"><span class="itt-l">Не учитывается</span><span class="itt-v">{{ money(registry.totals.excluded || 0) }}</span></div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { apiError } from "~/utils/apiError";
import { num as fmtNum } from "~/utils/format";
import {
  useExpForm, EXP_REG_TITLES, MONTHS_SHORT,
  type ExpRegKind, type ExpRegRow, type ExpRegistry
} from "~/composables/useExpForm";

const route = useRoute();
const cardId = Number(route.params.cardId);
const api = useExpForm();

const KINDS: ExpRegKind[] = ["payroll", "travel", "logistics", "it"];
const kind = ref<ExpRegKind>((route.query.kind as ExpRegKind) || "payroll");
const registry = ref<ExpRegistry | null>(null);
const loading = ref(true);
const busy = ref(false);
const error = ref("");
const note = ref("");
const cardTitle = ref("");

const rows = computed(() => registry.value?.rows || []);
const readOnly = computed(() => registry.value?.read_only !== false);
const money = (v: number) => fmtNum(v, 2);

interface Col { key: string; label: string; num?: boolean; type?: "flag"; hint?: string }

/**
 * Колонки атрибутов по виду реестра (ТЗ §5.1–§5.4). У ФОТ колонок налогов НЕТ:
 * ответ 38 отменил их — реестр содержит только «ФОТ чистый».
 */
const columns = computed<Col[]>(() => {
  switch (kind.value) {
    case "payroll":
      return [
        { key: "attrs.legal_entity", label: "Юрлицо", hint: "ТД Марк Формэль" },
        { key: "attrs.employee_id", label: "Таб. №", num: true, hint: "из справочника" },
        { key: "attrs.employee_name", label: "ФИО", hint: "выбирается из справочника" },
        { key: "attrs.position", label: "Должность" }
      ];
    case "travel":
      // Строка командировки — ОДНА поездка со своим месяцем (ТЗ §5.2), а не
      // двенадцать месяцев: колонка «Месяц» это поле ввода.
      return [
        { key: "attrs.month", label: "Месяц", num: true, hint: "1–12" },
        { key: "attrs.country", label: "Страна", hint: "RU" },
        { key: "attrs.city", label: "Город", hint: "Москва" },
        { key: "attrs.purpose", label: "Цель" },
        { key: "attrs.days", label: "Дней", num: true },
        { key: "attrs.nights", label: "Ночей", num: true },
        { key: "attrs.people", label: "Человек", num: true },
        { key: "attrs.one_way", label: "В одну сторону", type: "flag" },
        { key: "attrs.lodging_rate", label: "Проживание, USD/ночь", num: true, hint: "не нормировано" }
      ];
    case "logistics":
      return [
        { key: "code_pl", label: "Код PL", num: true, hint: "51 или 52" },
        { key: "attrs.expense", label: "Статья" },
        { key: "code_cfo", label: "Код ЦФО", num: true, hint: "250" }
      ];
    default:
      return [
        { key: "code_cfo", label: "Код ЦФО", num: true },
        { key: "code_pl", label: "Код статьи", num: true },
        { key: "attrs.title", label: "Наименование строки" },
        { key: "move_to_250", label: "На ЦФО 250", type: "flag" },
        { key: "excluded", label: "Не учитывать", type: "flag" }
      ];
  }
});

/**
 * Месяцы-колонки есть у всех реестров, кроме командировок: там строка это одна
 * поездка, и её итог ложится в месяц поездки (ТЗ §5.2).
 */
const hasMonthColumns = computed(() => kind.value !== "travel");

/** Расчётная часть строки командировки — считает сервер по справочникам (Н3). */
const calcColumns = computed<Col[]>(() =>
  kind.value === "travel"
    ? [
        { key: "tickets", label: "Билеты", hint: "норматив перелёта × плечи × человек" },
        { key: "lodging", label: "Проживание", hint: "ставка × ночей × человек × курс" },
        { key: "per_diem", label: "Командировочные", hint: "суточные × дней × человек × курс" }
      ]
    : []
);
const calcTotal = (key: string): number =>
  rows.value.reduce((s, r) => (r.excluded ? s : s + Number(r.attrs?.[key] || 0)), 0);

const kindHint = computed(() => {
  switch (kind.value) {
    case "payroll":
      return "Только «ФОТ чистый»: налоги от выплат — самостоятельные статьи 26 и 86–89 со своими суммами, ставки соцвзносов здесь нет. Итог реестра идёт на статью 24.";
    case "travel":
      return "Строка — одна поездка со своим месяцем. Билеты, проживание и командировочные считает сервер по справочникам суточных, нормативов перелёта и курса; руками вводятся только условия поездки и ставка проживания за ночь (она не нормирована). Билеты и командировочные публикуются статьёй 27, проживание — статьёй 30.";
    case "logistics":
      return "Статьи 51 и 52 по ЦФО 250. План месяца = доля статьи в факте продаж × план продаж того же месяца.";
    default:
      return "Признаки строки — флажки, а не текст в примечании. «На ЦФО 250» меняет ЦФО публикации, «Не учитывать» снимает строку с публикации и из итога. Одновременно они стоять не могут.";
  }
});

const fieldValue = (row: ExpRegRow, key: string): string => {
  if (key.startsWith("attrs.")) {
    const v = row.attrs?.[key.slice(6)];
    return v === undefined || v === null ? "" : String(v);
  }
  const v = (row as unknown as Record<string, unknown>)[key];
  return v === undefined || v === null || v === 0 ? "" : String(v);
};
const monthValue = (row: ExpRegRow, m: number): string => {
  const v = row.months?.[String(m)];
  return v === undefined ? "" : String(v);
};
const rowTotal = (row: ExpRegRow): number =>
  Object.values(row.months || {}).reduce((s, v) => s + (Number(v) || 0), 0);
const monthTotal = (m: number): number =>
  rows.value.reduce((s, r) => (r.excluded ? s : s + (Number(r.months?.[String(m)]) || 0)), 0);
const yearTotal = computed(() => rows.value.reduce((s, r) => (r.excluded ? s : s + rowTotal(r)), 0));

const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    registry.value = await api.registry(cardId, kind.value);
  } catch (e) {
    error.value = apiError(e, "Не удалось загрузить реестр");
  } finally {
    loading.value = false;
  }
};

const switchKind = (k: ExpRegKind) => {
  kind.value = k;
  void load();
};

/** Сохранение строки целиком: единица правки реестра — строка, а не ячейка. */
const push = async (row: Partial<ExpRegRow>) => {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    await api.saveRegRow(cardId, kind.value, { ...row, kind: kind.value });
    await load();
  } catch (e) {
    error.value = apiError(e, "Строка не сохранена");
  } finally {
    busy.value = false;
  }
};

const setField = (row: ExpRegRow, key: string, raw: string, isNum?: boolean) => {
  const value = isNum ? Number(raw.replace(",", ".")) || 0 : raw;
  const next: ExpRegRow = { ...row, attrs: { ...row.attrs }, months: { ...row.months } };
  if (key.startsWith("attrs.")) next.attrs[key.slice(6)] = value;
  else (next as unknown as Record<string, unknown>)[key] = value;
  void push(next);
};

const setFlag = (row: ExpRegRow, key: string, on: boolean) => {
  const next: ExpRegRow = { ...row, attrs: { ...row.attrs }, months: { ...row.months } };
  (next as unknown as Record<string, unknown>)[key] = on;
  void push(next);
};

const setMonth = (row: ExpRegRow, m: number, raw: string) => {
  const next: ExpRegRow = { ...row, attrs: { ...row.attrs }, months: { ...row.months } };
  const s = raw.trim().replace(/\s/g, "").replace(",", ".");
  if (s === "") delete next.months[String(m)];
  else next.months[String(m)] = Number(s) || 0;
  void push(next);
};

const addRow = () => push({ id: 0, attrs: {}, months: {}, code_cfo: 0, code_pl: 0 });

const copyRow = async (row: ExpRegRow) => {
  busy.value = true;
  try {
    await api.copyRegRow(cardId, kind.value, row.id);
    await load();
  } catch (e) {
    error.value = apiError(e, "Копия не создана");
  } finally {
    busy.value = false;
  }
};

const removeRow = async (row: ExpRegRow) => {
  // ТЗ §5.5: строка с введёнными суммами удаляется только после подтверждения.
  if (rowTotal(row) !== 0 && !window.confirm("В строке есть суммы. Удалить её вместе со значениями?")) return;
  busy.value = true;
  try {
    await api.deleteRegRow(cardId, kind.value, row.id);
    note.value = "Строка удалена — она сохранена в журнале вместе со значениями.";
    await load();
  } catch (e) {
    error.value = apiError(e, "Строка не удалена");
  } finally {
    busy.value = false;
  }
};

onMounted(async () => {
  await load();
  try {
    const f = await api.form(cardId, { limit: 1 });
    cardTitle.value = f.card?.title || "";
  } catch {
    // Заголовок — украшение: без него экран работает.
  }
});
</script>

<style scoped>
.page-reg { display: flex; flex-direction: column; gap: var(--sp-4); }
.lock-note { color: var(--neg); display: inline-flex; align-items: center; gap: 3px; margin-left: var(--sp-3); }
.reg-card { padding: 0; overflow: hidden; }
.reg-scroll { overflow: auto; max-height: 68vh; }
.reg-scroll table { min-width: 1180px; }
.reg-scroll thead th { position: sticky; top: 0; z-index: 2; background: var(--bg-surface-2); }
.reg-empty { color: var(--text-muted); font-size: var(--fs-sm); padding: var(--sp-6); text-align: center; }
.c-mon { width: 86px; }
.c-calc { width: 130px; }
/* Расчётные части строки командировки не редактируются: их считает сервер по
   справочникам нормативов. Показываем моноширинно, как остальные числа. */
.calc-cell { font-family: var(--font-mono); font-variant-numeric: tabular-nums; color: var(--text-secondary); }
.c-total { width: 110px; }
.c-act { width: 132px; }
.strong { font-weight: var(--fw-semibold); color: var(--text-strong); }
.muted { color: var(--text-muted); }
.row-off { opacity: 0.55; }
.row-acts { display: flex; flex-wrap: wrap; gap: var(--sp-2); }
.btn-xs { height: 22px; padding: 0 var(--sp-3); font-size: var(--fs-2xs); }
.flag { display: flex; justify-content: center; }
.cell-input {
  width: 100%;
  border: 1px solid transparent;
  background: transparent;
  color: var(--text-primary);
  font-size: var(--fs-sm);
  padding: 2px var(--sp-2);
  border-radius: var(--rd-2);
}
.cell-input.num { font-family: var(--font-mono); font-variant-numeric: tabular-nums; text-align: right; }
.cell-input:hover:not(:disabled) { border-color: var(--border); }
.cell-input:focus { outline: none; border-color: var(--border-focus); box-shadow: var(--shadow-focus); }
.cell-input:disabled { color: var(--text-muted); }
.it-totals {
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-6);
  padding: var(--sp-4) var(--sp-5);
  border-top: 1px solid var(--border);
}
.itt { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.itt-l { font-size: var(--fs-2xs); color: var(--text-muted); }
.itt-v { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
</style>
