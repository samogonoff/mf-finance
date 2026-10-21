/**
 * Форма этапа 1.5 «Расходы центров прибыли» (TPL-CFO-EXP).
 *
 * ТЗ: docs/reports/plans/tz-stage-1.5-2026-09-09.md, план работ:
 * docs/reports/plans/stage-1.5-plan.md. Реализованы фазы Ф1–Ф2: состав строк из
 * источника, сетка ввода на двенадцать плановых месяцев, факт прошлого года
 * справочно, размножение и очистка строки. Реестры (§5), расчёты (§6), свод и
 * сверки (§8), валидации (§9) и публикация (§10) — следующие фазы.
 */

export interface ExpValue {
  // fact — факт ПРОШЛОГО года, fact_cur — факт ПЛАНОВОГО года (прошлый и текущий месяц), strategy — стратегия.
  scenario: "plan" | "fact" | "fact_cur" | "strategy";
  month: number;
  /** null — ячейка НЕ заполнена. Ноль вводится явно и значит «расхода нет» (ТЗ §7.8). */
  amount: number | null;
  source: string;
  note?: string;
}

export interface ExpRow {
  id: number;
  code_cfo: number;
  code_pl: number;
  cfo: string;
  group_pl: string;
  expense: string;
  group_cfo1: string;
  group_cfo2: string;
  legal_entity: string;
  country_company: string;
  store_type: string;
  reg_manager: string;
  comment?: string;
  values: ExpValue[];
}

export interface ExpInstance {
  id: number;
  card_id: number;
  form_kind: "retail" | "mp";
  country: string;
  plan_year: number;
  currency: string;
  legal_entity: string;
}

export interface ExpCard {
  id: number;
  pl_id: number;
  form_code: string;
  scope_key: string;
  title: string;
  year: number;
  month: number;
  country: string;
  currency: string;
  status: string;
  step_code: string;
  status_label: string;
  current_version: number;
  locked: boolean;
  has_data?: boolean;
}

export interface ExpForm {
  instance: ExpInstance;
  card: ExpCard;
  rows: ExpRow[];
  total: number;
  offset: number;
  groups: string[];
  read_only: boolean;
  sync?: ExpSync;
}

// Загрузка состава строк из источника затрат идёт в фоне (процедура работает минуты).
export interface ExpSync {
  state: "idle" | "running" | "ok" | "error";
  error?: string;
  source_cells: number;
  started_at?: string;
  finished_at?: string;
  warning?: string; // загрузка прошла, но перезаливка снимка не удалась
  fact_basis?: "payments"; // факт построен по платежам (начислений в источнике нет); пусто — по начислениям
  source_kind?: "clickhouse" | "olap";
  source_as_of?: string; // когда залит ночной снимок затрат (только clickhouse)
}

// Свод расходов розницы по четырём странам (всё в BYN).
export type ExpConsWindow = "month" | "tail" | "year";
export interface ExpConsCountry {
  country: string;
  card_id?: number;
  title?: string;
  status?: string;
  nat_currency?: string;
  rows: number;
  rate: number;
  plan: number;
  no_rate?: boolean;
  error?: string;
}
export interface ExpConsAmounts {
  plan: Record<string, number>;
  plan_total: number;
  fact_ly: number;
  fact_prev: number;
  fact_cur: number;
}
export interface ExpConsLine extends ExpConsAmounts {
  code_pl: number;
  expense: string;
}
export interface ExpConsGroup extends ExpConsAmounts {
  group: string;
  unassigned?: boolean;
  lines: ExpConsLine[];
}
export type ExpConsChannel = "retail" | "im" | "opt" | "all";
export interface ExpConsolidated {
  channel?: ExpConsChannel;
  pl_id: number;
  year: number;
  month: number;
  window: ExpConsWindow;
  from_month: number;
  to_month: number;
  currency: string;
  countries: ExpConsCountry[];
  groups: ExpConsGroup[];
  total: ExpConsAmounts;
  fact_basis?: "payments";
  missing?: string[];
  /** Загрузка из источника по какой-то карточке ещё идёт: данные неполные, страница повторяет запрос. */
  syncing?: boolean;
  /** Пояснения к неполному итогу (страна не прочитана, нет курса, нет карточки, загрузка не удалась, карточка закрыта и пуста). */
  warnings?: string[];
  /** Общий свод: каналы, карточек которых в периоде нет (штатно). */
  absent_channels?: string[];
}

export interface ExpCell {
  code_cfo: number;
  code_pl: number;
  month: number;
  amount: number | null;
  source?: string;
  note?: string;
}

export interface ExpFilterQuery {
  /** Колонка сортировки: атрибут или месяц вида "m07" (ТЗ §7.3). */
  sort?: string;
  dir?: "asc" | "desc";
  search?: string;
  group_pl?: string;
  code_pl?: number;
  code_cfo?: number;
  city?: string;
  legal_entity?: string;
  store_type?: string;
  reg_manager?: string;
  offset?: number;
  limit?: number;
}

export interface ExpIssue {
  code: string;
  blocking: boolean;
  code_cfo?: number;
  code_pl?: number;
  month?: number;
  message: string;
  value?: number;
}

export interface ExpRecon {
  code: string;
  title: string;
  left: number;
  right: number;
  diff: number;
  ok: boolean;
  /** Контроль не считался: нет источника или реестра. Не то же, что «сошёлся». */
  skipped?: boolean;
  note?: string;
  details?: { code_cfo?: number; code_pl?: number; month?: number; title?: string; left: number; right: number; diff: number }[];
}

export interface ExpParamRow {
  param_code: string;
  scope_kind: string;
  scope_value: string;
  value: number;
}

/** Подстановка плана из факта: откуда брать факт, как раскладывать по месяцам и к каким строкам применять. */
export interface ExpFactFill {
  source: "prev_month" | "same_month_ly" | "ym";
  year?: number;
  month?: number;
  mode?: "same_month" | "one_month";
  coef?: number;
}
export interface ExpCalcExtra {
  months?: number[];
  fill?: ExpFactFill;
  filter?: { search?: string; group_pl?: string; city?: string; reg_manager?: string };
}

export interface ExpCalcDiff {
  code_cfo: number;
  code_pl: number;
  cfo?: string;
  month: number;
  before: number | null;
  after: number;
  note?: string;
}

export interface ExpCalcResult {
  op: string;
  preview: boolean;
  rows: number;
  changed: number;
  /** В скольких строках сработало ограничение порогом (ТЗ §6.1). */
  capped: number;
  /** В скольких строках базы не нашлось: «порог не применён, нет базы». */
  no_base: number;
  diff: ExpCalcDiff[];
}

export interface ExpSummaryRow {
  group_pl: string;
  expense: string;
  code_pl: number;
  /** Укрупнённая группа P&L (§12.3). «Не распределено» — кода нет во втором уровне. */
  pnl_group: string;
  by_scenario: Record<string, Record<string, number>>;
  totals: Record<string, number>;
}

export interface ExpSummaryGroup {
  pnl_group: string;
  by_month: Record<string, number>;
  total: number;
  codes_pl: number[];
  unassigned?: boolean;
}

export interface ExpSummary {
  card_id: number;
  plan_year: number;
  currency: string;
  rows: ExpSummaryRow[];
  groups: ExpSummaryGroup[];
  reconciliations: ExpRecon[];
}

export interface ExpImportResult {
  preview: boolean;
  rows_in_file: number;
  rows_matched: number;
  rows_unknown: number;
  cells_saved: number;
  changes?: { code_cfo: number; code_pl: number; month: number; old: number | null; new: number | null }[];
  issues: { line: number; code_cfo?: number; code_pl?: number; message: string }[];
}

export interface ExpValidationReport {
  card_id: number;
  blocking: ExpIssue[];
  warnings: ExpIssue[];
  reconciliations: ExpRecon[];
  can_submit: boolean;
  /** Жёсткий ли режим гейта: в мягком can_submit остаётся true при замечаниях. */
  strict: boolean;
  recon_diff: number;
  unclosed_comments: number;
}

/** Виды реестров формы (ТЗ §5). */
export type ExpRegKind = "payroll" | "travel" | "logistics" | "it";

export interface ExpRegRow {
  id: number;
  kind: ExpRegKind;
  sort_order: number;
  code_cfo: number;
  code_pl: number;
  /** ТЗ §5.4: признаки строки ИТ — флажки, а не текст в примечании. */
  move_to_250: boolean;
  excluded: boolean;
  attrs: Record<string, unknown>;
  note?: string;
  /** Месяц → сумма. Отсутствующий ключ — не заполнено. */
  months: Record<string, number>;
}

export interface ExpRegTotals {
  by_month?: Record<string, number>;
  year?: number;
  /** Итоги реестра ИТ (ТЗ §5.4). */
  transfer?: number;
  total?: number;
  common?: number;
  excluded?: number;
}

export interface ExpRegistry {
  kind: ExpRegKind;
  rows: ExpRegRow[];
  read_only: boolean;
  totals: ExpRegTotals;
}

export const EXP_REG_TITLES: Record<ExpRegKind, string> = {
  payroll: "ФОТ",
  travel: "Командировки",
  logistics: "Логистика ЦФО 250",
  it: "ИТ-расходы"
};

/** Действия по строке (ТЗ §7.8). */
export type ExpBulkAction = "spread_year" | "spread_to_end" | "clear_row";

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const MONTHS_SHORT = [
  "янв", "фев", "мар", "апр", "май", "июн",
  "июл", "авг", "сен", "окт", "ноя", "дек"
];

/**
 * Значение ячейки строки. Возвращает undefined для незаполненной: отличать
 * «пусто» от нуля обязаны и API, и интерфейс — на этом стоит валидация E-11.
 */
export const expCell = (row: ExpRow, scenario: ExpValue["scenario"], month: number): number | undefined => {
  const v = row.values.find((x) => x.scenario === scenario && x.month === month);
  return v && v.amount !== null ? v.amount : undefined;
};

/** Итог строки по сценарию: считаются только заполненные месяцы. */
export const expRowTotal = (row: ExpRow, scenario: ExpValue["scenario"]): number =>
  row.values.reduce((s, v) => (v.scenario === scenario && v.amount !== null ? s + v.amount : s), 0);

export const useExpForm = () => {
  const base = useRuntimeConfig().public.apiBase;

  const form = (cardId: number, q: ExpFilterQuery = {}): Promise<ExpForm> => {
    const params = new URLSearchParams();
    for (const [k, v] of Object.entries(q)) {
      if (v !== undefined && v !== null && v !== "" && v !== 0) params.set(k, String(v));
    }
    const qs = params.toString();
    return $fetch<ExpForm>(`${base}/api/plans/exp/${cardId}/form${qs ? `?${qs}` : ""}`, {
      headers: authHeader()
    });
  };

  const consolidated = (plId: number, window: ExpConsWindow = "tail", channel: ExpConsChannel = "retail"): Promise<ExpConsolidated> =>
    $fetch<ExpConsolidated>(`${base}/api/plans/exp-consolidated/${plId}?window=${window}&channel=${channel}`, { headers: authHeader() });

  const syncNow = (cardId: number): Promise<ExpSync> =>
    $fetch<ExpSync>(`${base}/api/plans/exp/${cardId}/sync`, { method: "POST", headers: authHeader() });

  const save = (cardId: number, cells: ExpCell[], comments: { code_cfo: number; code_pl: number; comment: string }[] = []) =>
    $fetch<{ changed: number }>(`${base}/api/plans/exp/${cardId}/form`, {
      method: "PUT",
      body: { cells, comments },
      headers: authHeader()
    });

  const bulk = (cardId: number, body: { action: ExpBulkAction; code_cfo: number; code_pl: number; month?: number; month_to?: number; amount?: number | null }) =>
    $fetch<{ changed: number }>(`${base}/api/plans/exp/${cardId}/bulk`, {
      method: "POST",
      body,
      headers: authHeader()
    });

  const totals = (cardId: number): Promise<Record<string, number>> =>
    $fetch(`${base}/api/plans/exp/${cardId}/totals`, { headers: authHeader() });

  /** Карточки периода — для экрана комплекта форм 1.5. */
  const cards = (plId: number): Promise<ExpCard[]> =>
    $fetch<ExpCard[]>(`${base}/api/plans/instances/${plId}/cards`, { headers: authHeader() });

  /** Раскладка формы: состав, порядок и видимость колонок + сортировка (ТЗ §7.4). */
  const presets = (): Promise<{ name: string; is_default: boolean; payload: Record<string, unknown> }[]> =>
    $fetch(`${base}/api/plans/forms/TPL-CFO-EXP/presets`, { headers: authHeader() });

  const savePreset = (name: string, payload: Record<string, unknown>) =>
    $fetch(`${base}/api/plans/forms/TPL-CFO-EXP/presets`, {
      method: "PUT",
      body: { name, payload },
      headers: authHeader()
    });

  const exportUrl = (cardId: number, q: ExpFilterQuery = {}): string => {
    const params = new URLSearchParams();
    for (const [k, v] of Object.entries(q)) {
      if (v !== undefined && v !== null && v !== "" && v !== 0) params.set(k, String(v));
    }
    const qs = params.toString();
    return `${base}/api/plans/exp/${cardId}/export${qs ? `?${qs}` : ""}`;
  };

  /** Пример .xlsx для импорта: колонки-ключи, месяцы плана и одна заполненная строка формы. */
  const importTemplateXlsx = (cardId: number): Promise<Blob> =>
    $fetch<Blob>(`${base}/api/plans/exp/${cardId}/import-template`, { headers: authHeader(), responseType: "blob" });

  const importFile = (cardId: number, file: File, preview: boolean) =>
    $fetch<ExpImportResult>(`${base}/api/plans/exp/${cardId}/import?preview=${preview ? 1 : 0}`, {
      method: "POST",
      body: file,
      headers: authHeader()
    });

  const params = (cardId: number): Promise<ExpParamRow[]> =>
    $fetch(`${base}/api/plans/exp/${cardId}/params`, { headers: authHeader() });

  const saveParam = (cardId: number, row: ExpParamRow) =>
    $fetch<{ ok: boolean }>(`${base}/api/plans/exp/${cardId}/params`, {
      method: "PUT",
      body: row,
      headers: authHeader()
    });

  const calc = (cardId: number, op: string, preview: boolean, extra: ExpCalcExtra = {}) =>
    $fetch<ExpCalcResult>(`${base}/api/plans/exp/${cardId}/calc`, {
      method: "POST",
      body: { op, preview, ...extra },
      headers: authHeader()
    });

  const summary = (cardId: number): Promise<ExpSummary> =>
    $fetch(`${base}/api/plans/exp/${cardId}/summary`, { headers: authHeader() });

  const validate = (cardId: number): Promise<ExpValidationReport> =>
    $fetch(`${base}/api/plans/exp/${cardId}/validate`, { headers: authHeader() });

  const registry = (cardId: number, kind: ExpRegKind): Promise<ExpRegistry> =>
    $fetch(`${base}/api/plans/exp/${cardId}/registry/${kind}`, { headers: authHeader() });

  const saveRegRow = (cardId: number, kind: ExpRegKind, row: Partial<ExpRegRow>) =>
    $fetch<{ id: number }>(`${base}/api/plans/exp/${cardId}/registry/${kind}`, {
      method: "PUT",
      body: row,
      headers: authHeader()
    });

  const copyRegRow = (cardId: number, kind: ExpRegKind, rowId: number) =>
    $fetch<{ id: number }>(`${base}/api/plans/exp/${cardId}/registry/${kind}/${rowId}/copy`, {
      method: "POST",
      headers: authHeader()
    });

  const deleteRegRow = (cardId: number, kind: ExpRegKind, rowId: number) =>
    $fetch<{ ok: boolean }>(`${base}/api/plans/exp/${cardId}/registry/${kind}/${rowId}`, {
      method: "DELETE",
      headers: authHeader()
    });

  return {
    form, save, bulk, totals, cards, validate, summary, params, saveParam, calc,
    registry, saveRegRow, copyRegRow, deleteRegRow,
    presets, savePreset, exportUrl, importFile, importTemplateXlsx, syncNow, consolidated
  };
};
