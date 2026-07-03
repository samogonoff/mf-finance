/**
 * Клиент страницы «Пользователи и права» модуля «Тактические планы».
 *
 * Две группы ролей (ТЗ §«Разграничение прав»):
 *   • Системные роли — гейт доступа (ROLE_ADMIN / ROLE_PLANS_ADMIN / ROLE_PLANS_USER),
 *     назначают всех остальных; правятся через /api/admin/users/{id}/roles.
 *   • Назначаемые должности — редактируемый каталог (plans_position) + ABAC-срез
 *     (этап × ЦФО × страна × ЮЛ) в plans_user_scope.
 * Всё доступно только системным админам (ROLE_PLANS_ADMIN / ROLE_ADMIN).
 */

export interface Position {
  code: string;
  name: string;
  description: string;
  default_stage_code: string;
  track: string;
  kind: string; // filler|approver|coordinator|observer
  sort_order: number;
  stage_codes: string[];
}

export interface Deputy {
  id: number;
  principal_user_id: number;
  principal_name: string;
  deputy_user_id: number;
  deputy_name: string;
  stage_code: string;
  note: string;
}

export interface ScopeAssignment {
  id: number;
  user_id: number;
  user_name: string;
  user_email: string;
  role: string;
  stage_code: string;
  country: string;
  legal_entity: string;
  code_cfo: number[];
}

export interface ScopeUpsert {
  role: string;
  stage_code?: string;
  country?: string;
  legal_entity?: string;
  code_cfo: number[];
}

export interface B24Candidate {
  id: number;
  name: string;
  last_name: string;
  position: string;
  email: string;
}

export interface ImportResult {
  b24_id: number;
  name: string;
  email: string;
  status: string;
  error?: string;
}

export interface PlanUser {
  id: number;
  name: string;
  last_name: string;
  email: string;
  roles: string[];
  plans_admin: boolean;
  plans_user: boolean;
  global_admin: boolean;
  absence_status: string;
  absence_until: string;
}

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const usePlansRights = () => {
  const base = useRuntimeConfig().public.apiBase;
  const h = () => authHeader();

  const positions = (): Promise<Position[]> =>
    $fetch<Position[]>(`${base}/api/plans/positions`, { headers: h() });

  const savePosition = (p: Position): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/positions/${p.code}`, { method: "PUT", body: p, headers: h() });

  const setPositionStages = (code: string, stages: string[]): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/positions/${code}/stages`, { method: "PUT", body: { stages }, headers: h() });

  const deletePosition = (code: string): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/positions/${code}`, { method: "DELETE", headers: h() });

  const assignments = (): Promise<ScopeAssignment[]> =>
    $fetch<ScopeAssignment[]>(`${base}/api/plans/assignments`, { headers: h() });

  const assign = (userId: number, body: ScopeUpsert): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/scope/${userId}`, { method: "PUT", body, headers: h() });

  const unassign = (id: number): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/assignments/${id}`, { method: "DELETE", headers: h() });

  const users = (): Promise<PlanUser[]> =>
    $fetch<PlanUser[]>(`${base}/api/plans/users`, { headers: h() });

  const setUserRoles = (id: number, plansAdmin: boolean, plansUser: boolean): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/users/${id}/roles`, {
      method: "PUT",
      body: { plans_admin: plansAdmin, plans_user: plansUser },
      headers: h()
    });

  const deputies = (): Promise<Deputy[]> =>
    $fetch<Deputy[]>(`${base}/api/plans/deputies`, { headers: h() });

  const saveDeputy = (b: { principal_user_id: number; deputy_user_id: number; stage_code?: string; note?: string }): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/deputies`, { method: "PUT", body: b, headers: h() });

  const deleteDeputy = (id: number): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/deputies/${id}`, { method: "DELETE", headers: h() });

  const setAbsence = (id: number, status: string, until: string): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/users/${id}/absence`, { method: "PUT", body: { status, until }, headers: h() });

  const importB24 = (b24Ids: number[]): Promise<ImportResult[]> =>
    $fetch<ImportResult[]>(`${base}/api/plans/users/import-b24`, { method: "POST", body: { b24_ids: b24Ids }, headers: h() });

  // Поиск уже добавленных пользователей (локальная БД).
  const searchLocal = (q: string): Promise<PlanUser[]> =>
    $fetch<PlanUser[]>(`${base}/api/plans/users/search`, { params: { q }, headers: h() });

  // Поиск сотрудников в B24 по фамилии (nitro-роут, тот же OAuth-app, cookie-токен).
  const searchB24 = (q: string): Promise<B24Candidate[]> =>
    $fetch<B24Candidate[]>(`/api/b24/search-users`, { params: { q } });

  // Догрузить выбранного из B24 сотрудника. Возвращает локальный id.
  const upsertUser = (c: B24Candidate): Promise<{ id: number; status: string }> =>
    $fetch(`${base}/api/plans/users/upsert`, {
      method: "POST",
      body: { b24_id: c.id, name: c.name, last_name: c.last_name, email: c.email },
      headers: h()
    });

  return {
    positions, savePosition, setPositionStages, deletePosition,
    assignments, assign, unassign, users, setUserRoles,
    deputies, saveDeputy, deleteDeputy, setAbsence, importB24,
    searchLocal, searchB24, upsertUser
  };
};
