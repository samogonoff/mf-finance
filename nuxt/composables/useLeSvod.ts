/**
 * Свод «Бюджет по ЮЛ» (этапы 1.6, 3, 4; TPL-08): колонки — юридические лица, строки — продажи по каналам и расходы
 * по группам P&L, суммы в BYN. Корректировки итогов — отдельные записи с причиной.
 */

export type LeWindow = "month" | "tail" | "year";

export interface LeSvodRow {
  key: string;
  section: "sales" | "exp" | "total";
  label: string;
  calc: Record<string, number>;
  adj: Record<string, number>;
  total: Record<string, number>;
  calc_all: number;
  adj_all: number;
  total_all: number;
  ratio?: Record<string, number | null>;
  ratio_all?: number | null;
  editable: boolean;
}

export interface LeAdjust {
  id: number;
  pl_id: number;
  legal_entity: string;
  line_key: string;
  amount_byn: number;
  reason: string;
  user_id: number;
  user_name?: string;
  created_at: string;
  revoked: boolean;
}

export interface LeSvod {
  pl_id: number;
  year: number;
  month: number;
  window: LeWindow;
  from_month: number;
  to_month: number;
  currency: string;
  legals: string[];
  rows: LeSvodRow[];
  adjustments: LeAdjust[];
  warnings?: string[];
  closed: boolean;
  /** Замечания готовности к этапу 1.6; ready — блокирующих нет. */
  readiness?: LeIssue[];
  ready: boolean;
}

export interface LeIssue {
  level: "block" | "warn";
  code: string;
  message: string;
}

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const useLeSvod = () => {
  const base = useRuntimeConfig().public.apiBase;

  const svod = (plId: number, window: LeWindow): Promise<LeSvod> =>
    $fetch<LeSvod>(`${base}/api/plans/le-svod/${plId}?window=${window}`, { headers: authHeader() });

  const adjust = (plId: number, body: { legal_entity: string; line_key: string; amount_byn: number; reason: string }) =>
    $fetch<{ id: number }>(`${base}/api/plans/le-svod/${plId}/adjust`, {
      method: "POST",
      headers: { ...authHeader(), "Content-Type": "application/json" },
      body
    });

  const revoke = (plId: number, id: number, reason: string) =>
    $fetch(`${base}/api/plans/le-svod/${plId}/adjust/${id}?reason=${encodeURIComponent(reason)}`, {
      method: "DELETE",
      headers: authHeader()
    });

  return { svod, adjust, revoke };
};
