/**
 * Клиент Go-API модуля «Тактические планы».
 *
 * Все запросы — через NUXT_PUBLIC_API_BASE с Bearer-токеном из localStorage.
 * Доступ гейтится ролью ROLE_PLANS_USER/ADMIN на бэке. При PLANS_MOCK=1 факт МП
 * отдаётся из фикстур — фронт о моках не знает. См. docs/reports/plans/SPEC.md.
 */

export interface PlanDirectory {
  code: string;
  source: string;
  sync_status: string;
  row_count: number;
}

export interface PlanFactRow {
  code_cfo: number;
  name_cfo: string;
  code_pl: number;
  year: number;
  month: number;
  scenario: string;
  currency: string;
  amount: number;
}

export interface MpFactQuery {
  year: number;
  month: number;
  segment: "large" | "small";
}

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const usePlans = () => {
  const config = useRuntimeConfig();
  const base = config.public.apiBase;

  const directories = (): Promise<PlanDirectory[]> =>
    $fetch<PlanDirectory[]>(`${base}/api/plans/directories`, { headers: authHeader() });

  const directoryRows = <T = Record<string, unknown>>(code: string): Promise<T[]> =>
    $fetch<T[]>(`${base}/api/plans/directories/${code}/rows`, { headers: authHeader() });

  const mpFact = (q: MpFactQuery): Promise<PlanFactRow[]> =>
    $fetch<PlanFactRow[]>(`${base}/api/plans/mp/fact`, {
      params: { year: q.year, month: q.month, segment: q.segment },
      headers: authHeader()
    });

  return { directories, directoryRows, mpFact };
};
