/**
 * Сохранённые пресеты фильтров отчёта «Задолженность ВГО».
 * Привязаны к пользователю Go-API через Bearer-токен.
 */

import type { DebtReportFilters } from "~/composables/useDebtReport";

export interface DebtSavedFilter {
  id: number;
  name: string;
  payload: DebtReportFilters;
  created_at: string;
  updated_at: string;
}

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const useDebtFilters = () => {
  const config = useRuntimeConfig();
  const base = config.public.apiBase;

  const list = (): Promise<DebtSavedFilter[]> =>
    $fetch<DebtSavedFilter[]>(`${base}/api/reports/debt/saved-filters`, {
      headers: authHeader()
    });

  const create = (name: string, payload: DebtReportFilters): Promise<DebtSavedFilter> =>
    $fetch<DebtSavedFilter>(`${base}/api/reports/debt/saved-filters`, {
      method: "POST",
      body: { name, payload },
      headers: authHeader()
    });

  const remove = (id: number): Promise<void> =>
    $fetch(`${base}/api/reports/debt/saved-filters`, {
      method: "DELETE",
      params: { id },
      headers: authHeader()
    }) as Promise<void>;

  return { list, create, remove };
};
