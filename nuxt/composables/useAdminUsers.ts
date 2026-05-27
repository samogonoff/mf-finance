/**
 * Админский API над /api/admin/users. Использует Bearer-токен из useAuth.
 * Ходит напрямую в Go-API через NUXT_PUBLIC_API_BASE.
 */

export interface AdminUser {
  id: number;
  email: string;
  name: string;
  last_name: string;
  roles: string[];
  effective_roles: string[];
  is_blocked: boolean;
  notify_via_b24: boolean;
  welcome_notification_sent: boolean;
  created_at: string;
}

export interface UsersFilter {
  email?: string;
  name?: string;
  is_blocked?: boolean | null;
  limit?: number;
  offset?: number;
}

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const useAdminUsers = () => {
  const config = useRuntimeConfig();
  const base = config.public.apiBase;

  const list = async (
    f: UsersFilter = {}
  ): Promise<{ data: AdminUser[]; total: number }> => {
    const params: Record<string, string> = {};
    if (f.email) params.email = f.email;
    if (f.name) params.name = f.name;
    if (f.is_blocked === true) params.is_blocked = "1";
    if (f.is_blocked === false) params.is_blocked = "0";
    if (f.limit != null) params.limit = String(f.limit);
    if (f.offset != null) params.offset = String(f.offset);
    return await $fetch<{ data: AdminUser[]; total: number }>(
      `${base}/api/admin/users`,
      { params, headers: authHeader() }
    );
  };

  const block = (id: number) =>
    $fetch(`${base}/api/admin/users/${id}/block`, {
      method: "POST",
      headers: authHeader()
    });

  const unblock = (id: number) =>
    $fetch(`${base}/api/admin/users/${id}/unblock`, {
      method: "POST",
      headers: authHeader()
    });

  const updateRoles = (id: number, roles: string[]) =>
    $fetch<AdminUser>(`${base}/api/admin/users/${id}/roles`, {
      method: "PUT",
      body: { roles },
      headers: authHeader()
    });

  const allowedRoles = async (): Promise<string[]> => {
    const r = await $fetch<{ roles: string[] }>(
      `${base}/api/admin/users/roles`,
      { headers: authHeader() }
    );
    return r.roles;
  };

  return { list, block, unblock, updateRoles, allowedRoles };
};
