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
  if (path.startsWith("/analytics")) {
    if (!hasScope("analytics")) return navigateTo("/", { replace: true });
    return;
  }
  if (path.startsWith("/cost")) {
    if (!hasScope("cost")) return navigateTo("/", { replace: true });
    return;
  }
});
