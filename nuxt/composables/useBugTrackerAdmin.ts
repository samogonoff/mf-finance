/**
 * Админский API над /api/bugtracker/*.
 * Эталон: mp/nuxt/composables/useBugTrackerAdmin.ts.
 */

export interface BugReport {
  id: number;
  user_id?: number;
  section: string;
  status: string;
  type: string;
  title: string;
  description: string;
  steps: string;
  url: string;
  signature: string;
  route: Record<string, any>;
  entity_ref: Record<string, any>;
  context_snapshot: Record<string, any>;
  tech_context: Record<string, any>;
  console_logs: any[];
  network_errors: any[];
  js_errors: any[];
  screenshots: string[];
  admin_comment: string;
  source_id?: number;
  source: { id: number; name: string } | null;
  b24_task_id?: string;
  user: { id: number; name: string; email: string } | null;
  // Автоматический разбор: замок (кто взял) и подготовленный merge request.
  claimed_by: string | null;
  claimed_at: string | null;
  mr_iid: number | null;
  mr_url: string | null;
  created_at: string;
  updated_at: string;
  resolved_at?: string;
}

export interface BugSource {
  id: number;
  name: string;
  is_active: boolean;
  created_at: string;
}

export interface BugMetrics {
  total: number;
  by_status: Record<string, number>;
  by_section: Record<string, number>;
  by_type: Record<string, number>;
  top_urls: { url: string; count: number }[];
  mttr_seconds: number;
}

export interface BugListFilter {
  section?: string;
  status?: string;
  type?: string;
  user_id?: number;
  date_from?: string;
  date_to?: string;
  search?: string;
  limit?: number;
  offset?: number;
}

// Подписи — общие для списка и карточки. Разделы совпадают с allowedSections в Go.
export const BT_STATUS_LABELS: Record<string, string> = {
  new: "Новый", in_progress: "В работе", resolved: "Решён",
  duplicate: "Дубликат", rejected: "Отклонён"
};
export const BT_TYPE_LABELS: Record<string, string> = {
  bug: "Ошибка", data: "Данные", ui: "Интерфейс",
  performance: "Производительность", other: "Другое"
};
export const BT_SECTION_LABELS: Record<string, string> = {
  finance: "Финансы", cost: "Себестоимость", operations: "Операции", reports: "Отчёты",
  plans: "Планы", counterparties: "Контрагенты", analytics: "Аналитика",
  account: "Аккаунт", admin: "Админка", ops: "Эксплуатация", other: "Другое"
};
export const btStatusBadge = (s: string) => ({
  new: "badge-info", in_progress: "badge-warn", resolved: "badge-pos",
  duplicate: "badge-accent", rejected: "badge-neg"
} as Record<string, string>)[s] ?? "badge-info";
export const btLabel = (map: Record<string, string>, k: string) => map[k] ?? k;

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const useBugTrackerAdmin = () => {
  const config = useRuntimeConfig();
  const base = config.public.apiBase;

  const list = (f: BugListFilter = {}) => {
    const params: Record<string, string> = {};
    Object.entries(f).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== "") params[k] = String(v);
    });
    return $fetch<{ data: BugReport[]; total: number }>(`${base}/api/bugtracker/list`, {
      params, headers: authHeader()
    });
  };

  const get = (id: number) =>
    $fetch<BugReport>(`${base}/api/bugtracker/report/${id}`, { headers: authHeader() });

  const patch = (id: number, body: Partial<Pick<BugReport, "status" | "source_id" | "b24_task_id" | "admin_comment">>) =>
    $fetch<BugReport>(`${base}/api/bugtracker/report/${id}`, {
      method: "PATCH", body, headers: authHeader()
    });

  const remove = (id: number) =>
    $fetch(`${base}/api/bugtracker/report/${id}`, { method: "DELETE", headers: authHeader() });

  const metrics = (range: { date_from?: string; date_to?: string } = {}) => {
    const params: Record<string, string> = {};
    if (range.date_from) params.date_from = range.date_from;
    if (range.date_to) params.date_to = range.date_to;
    return $fetch<BugMetrics>(`${base}/api/bugtracker/metrics`, { params, headers: authHeader() });
  };

  const sources = (activeOnly = false) =>
    $fetch<BugSource[]>(`${base}/api/bugtracker/sources`, {
      params: activeOnly ? { active: "1" } : {},
      headers: authHeader()
    });
  const sourceCreate = (name: string) =>
    $fetch<BugSource>(`${base}/api/bugtracker/sources`, {
      method: "POST", body: { name }, headers: authHeader()
    });
  const sourcePatch = (id: number, body: { name?: string; is_active?: boolean }) =>
    $fetch(`${base}/api/bugtracker/sources/${id}`, {
      method: "PATCH", body, headers: authHeader()
    });

  return { list, get, patch, remove, metrics, sources, sourceCreate, sourcePatch };
};
