/**
 * Глобальный 401-перехват: если любой /api запрос вернул 401 — чистим токены
 * и редиректим на /login.
 */
export default defineNuxtPlugin((nuxtApp) => {
  if (!process.client) return;

  const apiFetch = $fetch.create({
    onResponseError({ response, request }) {
      if (response.status !== 401) return;
      const url = request.toString() || "";
      if (!url.includes("/api/")) return;

      localStorage.removeItem("auth_token");
      localStorage.removeItem("refresh_token");

      const route = useRoute();
      if (route.path !== "/login" && route.path !== "/") {
        navigateTo("/login", { replace: true });
      }
    }
  });

  nuxtApp.hook("app:created", () => {
    // @ts-ignore
    globalThis.$fetch = apiFetch;
  });
});
