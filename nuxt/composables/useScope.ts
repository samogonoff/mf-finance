/**
 * Финансовые роли и scope-проверки.
 *
 * Иерархия (разворачивается на бэке через auth.ExpandRoles):
 *   ROLE_ADMIN        ⊇ ROLE_COST_ADMIN, ROLE_FINANCE_ADMIN
 *   ROLE_COST_ADMIN   ⊇ ROLE_COST_USER
 *   ROLE_USER         — всегда присутствует у любого залогиненного
 *
 * Поскольку /api/auth/me возвращает уже эффективный набор ролей,
 * проверки сводятся к простому includes().
 */

type Scope = "finance" | "cost" | "analytics" | "admin";

const SCOPE_ROLES: Record<Scope, string[]> = {
  admin: ["ROLE_ADMIN"],
  cost: ["ROLE_ADMIN", "ROLE_COST_ADMIN", "ROLE_COST_USER"],
  finance: ["ROLE_ADMIN", "ROLE_FINANCE_ADMIN"],
  analytics: ["ROLE_ADMIN", "ROLE_FINANCE_ADMIN"]
};

export const useScope = () => {
  const { user } = useAuth();

  const roles = computed(() => user.value?.roles ?? []);

  const isAdmin = computed(() => roles.value.includes("ROLE_ADMIN"));

  const hasRole = (role: string): boolean => roles.value.includes(role);

  const hasScope = (scope: Scope): boolean => {
    const allowed = SCOPE_ROLES[scope] ?? [];
    return allowed.some((r) => roles.value.includes(r));
  };

  return { roles, isAdmin, hasRole, hasScope };
};
