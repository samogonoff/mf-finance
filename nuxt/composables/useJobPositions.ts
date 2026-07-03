/**
 * Должности (job positions) — настоящие должности с носителем (IT-Директор =
 * Серяков). Плоские: 1 должность = 1 носитель. Должность покрывает набор ЦФО и
 * является их ТОПом (живая связь: сменил носителя — ТОП обновился у всех ЦФО).
 * Замы носителя — из механики замещения (страница «Пользователи и права»).
 */

export interface JobPosition {
  id: number;
  title: string;
  holder_user_id: number | null;
  holder_name: string;
  description: string;
  cfo_count: number;
  deputy_name: string;
  stage_deputies: number;
}

export interface CfoFilter {
  entity_type?: string;
  group_cfo1?: string;
  group_cfo2?: string;
  country?: string;
  legal_entity?: string;
  segment?: string;
}

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const useJobPositions = () => {
  const base = useRuntimeConfig().public.apiBase;
  const h = () => authHeader();

  const list = (): Promise<JobPosition[]> => $fetch<JobPosition[]>(`${base}/api/plans/jobpos`, { headers: h() });

  const upsert = (p: { id?: number; title: string; holder_user_id: number; description: string }): Promise<{ id: number }> =>
    $fetch(`${base}/api/plans/jobpos`, { method: "PUT", body: p, headers: h() });

  const remove = (id: number): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/jobpos/${id}`, { method: "DELETE", headers: h() });

  const cfo = (id: number): Promise<string[]> => $fetch<string[]>(`${base}/api/plans/jobpos/${id}/cfo`, { headers: h() });

  const assignCfo = (id: number, codes: number[]): Promise<{ assigned: number }> =>
    $fetch(`${base}/api/plans/jobpos/${id}/cfo`, { method: "PUT", body: { codes: codes.map(String) }, headers: h() });

  const assignByFilter = (id: number, filter: CfoFilter): Promise<{ assigned: number }> =>
    $fetch(`${base}/api/plans/jobpos/${id}/cfo`, { method: "PUT", body: { filter }, headers: h() });

  const unassignCfo = (code: string): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/jobpos/cfo/${code}`, { method: "DELETE", headers: h() });

  return { list, upsert, remove, cfo, assignCfo, assignByFilter, unassignCfo };
};
