/**
 * Финансовые роли и scope-проверки.
 *
 * Иерархия:
 *   ROLE_FINANCE_ADMIN → ROLE_FINANCE → ROLE_USER
 *   ROLE_ANALYST       → ROLE_USER  (доступ только к Python-аналитике)
 *
 * scope:
 *   - "finance"   — основной кабинет (операции, отчёты, контрагенты)
 *   - "analytics" — Python-песочница аналитика
 *   - "admin"     — управление пользователями
 */

type Scope = "finance" | "analytics" | "admin";

export const useScope = () => {
  const { user } = useAuth();

  const roles = computed(() => user.value?.roles ?? []);

  const isAdmin = computed(
    () => roles.value.includes("ROLE_FINANCE_ADMIN") || roles.value.includes("ROLE_ADMIN")
  );

  const hasScope = (scope: Scope): boolean => {
    const r = roles.value;
    if (r.includes("ROLE_ADMIN") || r.includes("ROLE_FINANCE_ADMIN")) return true;
    if (scope === "finance") return r.includes("ROLE_FINANCE");
    if (scope === "analytics") return r.includes("ROLE_ANALYST") || r.includes("ROLE_FINANCE");
    if (scope === "admin") return false;
    return false;
  };

  return { roles, isAdmin, hasScope };
};
