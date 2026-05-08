// В cost-only контуре дашборда и других страниц нет — на корне делать нечего.
// Редиректим "/" → "/cost" на обоих сторонах (SSR + client), чтобы при
// прямом заходе на http://localhost:8088/ сразу падать на рабочую страницу.
export default defineNuxtRouteMiddleware((to) => {
  const config = useRuntimeConfig();
  if (!config.public.costOnly) return;
  if (to.path === "/") return navigateTo("/cost", { replace: true });
});
