/**
 * Структура компании (ответственность по направлениям). Дерево узлов
 * (направление × страна) выводится из dir_cfo, ЦФО разносятся автоматически;
 * здесь — кто ответственный за узел + учредители (финальное утверждение).
 * Источник истины по ответственным за формы ввода; маршрут читает его.
 */

export interface OrgUnit {
  group: string;
  country: string;
  form_code: string;
  cfo_count: number;
  responsible_user_id: number | null;
  responsible_name: string;
  deputy_name: string;
  assigned: boolean;
}
export interface OrgDirection {
  group: string;
  units: OrgUnit[];
  cfo_count: number;
  assigned_cfo: number;
}
export interface OrgFounder {
  slot: string;
  responsible_user_id: number | null;
  responsible_name: string;
}
export interface OrgTree {
  founders: OrgFounder[];
  directions: OrgDirection[];
  total_cfo: number;
  assigned_cfo: number;
  unassigned_cfo: number;
}

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const useOrgStructure = () => {
  const base = useRuntimeConfig().public.apiBase;
  const h = () => authHeader();

  const tree = (): Promise<OrgTree> => $fetch<OrgTree>(`${base}/api/plans/org`, { headers: h() });

  const setResponsible = (group: string, country: string, userId: number, formCode = ""): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/org/responsible`, {
      method: "PUT",
      body: { group, country, user_id: userId, form_code: formCode },
      headers: h()
    });

  return { tree, setResponsible };
};
