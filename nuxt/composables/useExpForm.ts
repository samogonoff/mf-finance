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
  scenario: "plan" | "fact";
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
}

export interface ExpForm {
  instance: ExpInstance;
  card: ExpCard;
  rows: ExpRow[];
  total: number;
  offset: number;
  groups: string[];
  read_only: boolean;
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
export const expCell = (row: ExpRow, scenario: "plan" | "fact", month: number): number | undefined => {
  const v = row.values.find((x) => x.scenario === scenario && x.month === month);
  return v && v.amount !== null ? v.amount : undefined;
};

/** Итог строки по сценарию: считаются только заполненные месяцы. */
export const expRowTotal = (row: ExpRow, scenario: "plan" | "fact"): number =>
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

  const save = (cardId: number, cells: ExpCell[], comments: { code_cfo: number; code_pl: number; comment: string }[] = []) =>
    $fetch<{ changed: number }>(`${base}/api/plans/exp/${cardId}/form`, {
      method: "PUT",
      body: { cells, comments },
      headers: authHeader()
    });

  const bulk = (cardId: number, body: { action: ExpBulkAction; code_cfo: number; code_pl: number; month?: number; amount?: number | null }) =>
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

  return { form, save, bulk, totals, cards };
};
