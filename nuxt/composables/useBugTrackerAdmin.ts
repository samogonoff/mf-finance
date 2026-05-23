/**
 * Админский API над /api/bugtracker/*.
 * Эталон: mp/nuxt/composables/useBugTrackerAdmin.ts.
 */

import type { Notification } from "~/composables/useNotifications";

export interface BugReport {
  id: number;
  user_id?: number;
  section: string;
  status: string;
  type: string;
  title: string;
  description: string;
  steps: string;
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
  b24_task_id?: string;
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

  const metrics = () =>
    $fetch<BugMetrics>(`${base}/api/bugtracker/metrics`, { headers: authHeader() });

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
