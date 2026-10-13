/**
 * Глобальный гард: для любых страниц вне /login требуем ROLE_FINANCE
 * (или совместимые). На SSR пропускаем — токен живёт в localStorage,
 * проверка имеет смысл только на клиенте.
 */
export default defineNuxtRouteMiddleware(async (to) => {
  const path = to.path;

  if (path === "/login" || path.startsWith("/api/auth/")) return;
  if (process.server) return;

  const { fetchUser, user } = useAuth();
  if (!user.value) await fetchUser(true);

  if (!user.value) {
    return navigateTo("/login", { replace: true });
  }

  const { hasScope } = useScope();

  if (path.startsWith("/admin")) {
    if (!hasScope("admin")) return navigateTo("/", { replace: true });
    return;
  }
  if (path.startsWith("/reports/fot")) {
    // ФОТ-дашборд — страница cost-раздела, живущая под /reports (внутренний
    // гейт — право cost:fot через useCostPermission, не роли кабинета).
    // Без ветки cost страница не открывалась в изолированном cost-контуре:
    // заглушка cost-bypass даёт только ROLE_COST_ADMIN, и дев-проверка без
    // Б24-логина (НУЖНОГО ТОЛЬКО НА ПРОДЕ) редиректила на корень.
    if (!hasScope("cost") && !hasScope("finance")) return navigateTo("/", { replace: true });
    return;
  }
  if (path.startsWith("/reports")) {
    // Отчётность пока закрыта только для ROLE_FINANCE_ADMIN (или ROLE_ADMIN
    // через иерархию). До расширения модели ролей шире не пускаем.
    if (!hasScope("finance")) return navigateTo("/", { replace: true });
    return;
  }
  if (path.startsWith("/analytics")) {
    if (!hasScope("analytics")) return navigateTo("/", { replace: true });
    return;
  }
  if (path.startsWith("/cost")) {
    if (!hasScope("cost")) return navigateTo("/", { replace: true });
    return;
  }
  if (path.startsWith("/plans")) {
    // Тактические планы — для ROLE_PLANS_USER/ADMIN (или ROLE_ADMIN через иерархию).
    if (!hasScope("plans")) return navigateTo("/", { replace: true });
    return;
  }
});
