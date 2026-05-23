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
  currencies: string[];
}

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
  payment_term_days: number;
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
  currencies: string[];
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
    if (f.currencies.length) params.currencies = csv(f.currencies);
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
    date_from: string;
    date_to: string;
  }): Promise<DebtDocumentRow[]> =>
    $fetch<DebtDocumentRow[]>(`${base}/api/reports/debt/drilldown`, {
      params: q,
      headers: authHeader()
    });

  return { filterOptions, report, drilldown };
};
