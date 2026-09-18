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

type Scope = "finance" | "cost" | "analytics" | "admin" | "plans" | "budget";

const SCOPE_ROLES: Record<Scope, string[]> = {
  admin: ["ROLE_ADMIN"],
  cost: ["ROLE_ADMIN", "ROLE_COST_ADMIN", "ROLE_COST_USER"],
  finance: ["ROLE_ADMIN", "ROLE_FINANCE_ADMIN"],
  analytics: ["ROLE_ADMIN", "ROLE_FINANCE_ADMIN"],
  // Тактические планы — отдельный гейт; ROLE_PLANS_ADMIN ⊇ ROLE_PLANS_USER
  // разворачивается на бэке (auth.ExpandRoles), /api/auth/me отдаёт эффективный набор.
  plans: ["ROLE_ADMIN", "ROLE_PLANS_ADMIN", "ROLE_PLANS_USER"],
  // Бюджетирование — обособленный контур в своём репозитории (/budget/).
  // Здесь только видимость пункта в шапке; доступ к API раздела решает
  // nginx-гейт auth_request → GET /api/auth/check?role=ROLE_BUDGET.
  budget: ["ROLE_ADMIN", "ROLE_BUDGET_ADMIN", "ROLE_BUDGET"]
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
