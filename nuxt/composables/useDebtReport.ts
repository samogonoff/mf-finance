/**
 * Клиент Go-API для отчёта «Задолженность ВГО».
 *
 * Все запросы идут через NUXT_PUBLIC_API_BASE с Bearer-токеном из localStorage.
 * В DEBT_MOCK=1 на Go-стороне отдаются фикстуры — фронт ничего не знает о моках.
 */

export type Country = "РБ" | "РФ" | "КЗ" | "УЗ" | "Турция" | "Чехия" | "Великобритания" | "Китай" | "Кыргызстан";

export interface DebtEntity {
  inn: string;
  name: string;
  country: Country;
}

export interface DebtAccount {
  code: string;
  name: string;
  country: Country | "";
}

export interface DebtFilterOptions {
  entities: DebtEntity[];
  accounts: DebtAccount[];
  // Линзы представления суммы (CUR_FILTER): «В валюте договора» / «В бел. рублях»
  // / «В долларах США». Это НЕ фильтр валют, а способ пересчёта одной суммы.
  lenses: string[];
}

// DEBT_LENS_DEFAULT — дефолтная линза (native-валюта договора). Совпадает с
// LensDefault на бэке (go/internal/reports/debt/lens.go).
export const DEBT_LENS_DEFAULT = "В валюте договора";

export interface DebtRow {
  country: Country;
  company: string;
  company_inn: string;
  partner: string;
  partner_inn?: string;
  account: string;
  account_name: string;
  subaccount: string;
  subaccount_name: string;
  contract: string;
  contract_ref?: string;       // сырая 1С-ссылка договора (для точного drill-down)
  payment_term_days: number;
  payment_due_date?: string;   // срок оплаты по договору (пусто, если не заведён)
  overdue_days?: number;       // просрочка в днях на дату отчёта
  currency: string;

  opening_dz: number;
  opening_kz: number;
  turnover_dz: number;
  turnover_kz: number;
  closing_dz: number;
  closing_kz: number;

  revenue_period: number;
  revenue_last_month: number;
}

export interface DebtReportResponse {
  rows: DebtRow[];
  generated_at: string;
  report_date: string;
}

export interface DebtDocumentRow {
  doc_date: string;
  doc_number: string;
  doc_kind: string;
  trans_group: string;     // тип операции для группировки внутри drill-down (M5)
  amount: number;          // суммарный модуль проводок документа
  description: string;     // operation_description / trans_description
  dz_change: number;
  kz_change: number;
  payment_due_date: string;
  overdue_days: number;
}

export interface DebtReportFilters {
  date_from: string;
  date_to: string;
  entity_inns: string[];
  accounts: string[];
  // lens — линза представления суммы (см. DebtFilterOptions.lenses). Ровно одна.
  lens: string;
  // currencies — устаревший мультивыбор валют; оставлен для совместимости старых
  // пресетов, в запрос больше не уходит.
  currencies?: string[];
  // only_ico — фильтр «только внутригрупповые операции» (Premaster.ICO=1).
  // По дефолту true в UI Level 1 MVP (см. docs/reports/debt/open-questions.md §A3).
  only_ico: boolean;
}

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

const csv = (arr: string[]) => arr.join(",");

export const useDebtReport = () => {
  const config = useRuntimeConfig();
  const base = config.public.apiBase;

  const filterOptions = (): Promise<DebtFilterOptions> =>
    $fetch<DebtFilterOptions>(`${base}/api/reports/debt/filter-options`, {
      headers: authHeader()
    });

  const report = (f: DebtReportFilters): Promise<DebtReportResponse> => {
    const params: Record<string, string> = {
      date_from: f.date_from,
      date_to: f.date_to
    };
    if (f.entity_inns.length) params.entity_inns = csv(f.entity_inns);
    if (f.accounts.length) params.accounts = csv(f.accounts);
    if (f.lens) params.lens = f.lens;
    params.only_ico = f.only_ico ? "1" : "0";
    return $fetch<DebtReportResponse>(`${base}/api/reports/debt/report`, {
      params,
      headers: authHeader()
    });
  };

  const drilldown = (q: {
    company_inn?: string;
    partner_inn?: string;
    account: string;
    contract: string;
    currency: string;
    lens?: string;
    date_from: string;
    date_to: string;
  }): Promise<DebtDocumentRow[]> =>
    $fetch<DebtDocumentRow[]>(`${base}/api/reports/debt/drilldown`, {
      params: q,
      headers: authHeader()
    });

  return { filterOptions, report, drilldown };
};
