import type { ExpCard } from "~/composables/useExpForm";

const authHeader = (): Record<string, string> => {
  if (!import.meta.client) return {};
  const token = localStorage.getItem("auth_token");
  return token ? { Authorization: `Bearer ${token}` } : {};
};

export interface CfoExpenseConfig {
  code: number; country: string; fact_scenario: string; info_fact_scenario: string;
  plan_scenario: string; fact_years: number[]; plan_year: number;
  comparison_scenario?: string; comparison_years?: number[];
  has_legal_entity: boolean; half_year_input: boolean; has_expectation: boolean;
}
export interface CfoExpenseValue { scenario: string; year: number; month: number; amount: number | null }
export interface CfoExpenseRow {
  id: number; code_cfo: number; cfo?: string; code_pl: number; legal_entity: string;
  group_pl: string; expense: string; country?: string; country_company: string;
  comment: string; values: CfoExpenseValue[];
}
export interface CfoExpenseCell { code_cfo: number; code_pl: number; legal_entity: string; month: number; amount: number | null }
export interface CfoExpenseForm {
  card: ExpCard; config: CfoExpenseConfig; rows: CfoExpenseRow[]; total: number; offset: number;
  read_only: boolean; source_error?: string; open_months: boolean[];
  sales?: { year: number; scenario: string; amount: number }[];
  fx_rate?: number | null; fx_error?: string;
}
export interface CfoExpenseIssue { code: string; message: string; month?: number; blocking: boolean }
export interface CfoExpenseImport { preview: boolean; changes: CfoExpenseCell[]; issues: (CfoExpenseIssue | string)[]; saved: number }
export const useCfoExpense = () => {
  const base = useRuntimeConfig().public.apiBase;
  const url = (id: number, action: string) => `${base}/api/plans/cfo-exp/${id}/${action}`;
  return {
    form: (id: number, query: Record<string, string | number | undefined> = {}) => $fetch<CfoExpenseForm>(url(id, "form"), { query, headers: authHeader() }),
    save: (id: number, cells: CfoExpenseCell[], comments: { code_cfo: number; code_pl: number; legal_entity: string; comment: string }[]) =>
      $fetch<{ saved: number }>(url(id, "form"), { method: "PUT", body: { cells, comments }, headers: authHeader() }),
    validate: (id: number) => $fetch<{ issues: CfoExpenseIssue[]; open_months: boolean[] }>(url(id, "validate"), { headers: authHeader() }),
    bulk: (id: number, cell: CfoExpenseCell, op: "year" | "to_end" | "clear") => $fetch<{ saved: number }>(url(id, "bulk"), { method: "POST", body: { cell, op }, headers: authHeader() }),
    export: async (id: number, query: Record<string, string | number | undefined>) => {
      const blob = await $fetch<Blob>(url(id, "export"), { query, responseType: "blob", headers: authHeader() });
      const href = URL.createObjectURL(blob); const a = document.createElement("a");
      a.href = href; a.download = `cfo-exp-${id}.xlsx`; a.click(); URL.revokeObjectURL(href);
    },
    import: (id: number, file: File, preview: boolean) => {
      const body = new FormData(); body.append("file", file);
      return $fetch<CfoExpenseImport>(url(id, "import"), { method: "POST", query: { preview: preview ? 1 : 0 }, body, headers: authHeader() });
    }
  };
};
