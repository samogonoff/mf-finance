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

export interface MpFormRow {
  code_cfo: number;
  name_cfo: string;
  fact: number;
  tactic: number | null;
}

export interface MpFormBlock {
  block_type: string;
  code_pl: number;
  name: string;
  editable: boolean;
  rows: MpFormRow[];
}

export interface MpFormData {
  header: { year: number; month: number; segment: string; currency: string; scenario: string };
  platforms: Array<{ code_cfo: number; name_cfo: string; country: string }>;
  blocks: MpFormBlock[];
}

export interface SaveMpRow {
  code_cfo: number;
  code_pl: number;
  block_type: string;
  amount: number;
  comment?: string;
  is_manual?: boolean;
}

export interface SaveMpFormPayload {
  template_code?: string;
  segment: "large" | "small";
  period: { year: number; month: number };
  header: { currency: string; scenario: string };
  rows: SaveMpRow[];
}

export interface ComputedRow {
  code_cfo: number;
  name_cfo: string;
  values: Record<string, number>;
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

  const mpForm = (q: MpFactQuery & { currency?: string }): Promise<MpFormData> =>
    $fetch<MpFormData>(`${base}/api/plans/mp/form`, {
      params: { year: q.year, month: q.month, segment: q.segment, currency: q.currency ?? "" },
      headers: authHeader()
    });

  const mpCompute = (q: MpFactQuery & { currency?: string }): Promise<ComputedRow[]> =>
    $fetch<ComputedRow[]>(`${base}/api/plans/mp/compute`, {
      params: { year: q.year, month: q.month, segment: q.segment, currency: q.currency ?? "" },
      headers: authHeader()
    });

  const saveMpForm = (payload: SaveMpFormPayload): Promise<{ pl_id: number }> =>
    $fetch<{ pl_id: number }>(`${base}/api/plans/mp/form`, {
      method: "PUT",
      body: payload,
      headers: authHeader()
    });

  // Экспорт .xlsx (TPL-06) — нативный fetch для blob.
  const mpExport = async (q: MpFactQuery & { currency?: string }): Promise<Blob> => {
    const params = new URLSearchParams({
      year: String(q.year),
      month: String(q.month),
      segment: q.segment,
      currency: q.currency ?? ""
    });
    const res = await fetch(`${base}/api/plans/mp/export?${params}`, { headers: authHeader() });
    if (!res.ok) throw new Error(`Экспорт: ${res.status}`);
    return res.blob();
  };

  // Импорт .xlsx (тело — файл); ошибка строки/ABAC/причины → текст ошибки.
  const mpImport = async (q: MpFactQuery, file: File | Blob): Promise<{ pl_id: number }> => {
    const params = new URLSearchParams({
      year: String(q.year),
      month: String(q.month),
      segment: q.segment
    });
    const res = await fetch(`${base}/api/plans/mp/import?${params}`, {
      method: "POST",
      headers: authHeader(),
      body: file
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error((data as { error?: string }).error || `Импорт: ${res.status}`);
    return data as { pl_id: number };
  };

  return { directories, directoryRows, mpFact, mpForm, mpCompute, saveMpForm, mpExport, mpImport };
};
