/**
 * Глобальный 401-перехват: при 401 на /api/* пробуем refresh access-токена;
 * на успех — retry оригинального запроса с новым Bearer, на неуспех — чистим
 * токены и редиректим на /login.
 *
 * Single-flight: параллельные 401 ждут одного refresh-промиса (иначе несколько
 * одновременных запросов на странице каждый дёрнули бы /api/auth/refresh).
 */
export default defineNuxtPlugin((nuxtApp) => {
  if (!process.client) return;

  const config = useRuntimeConfig();
  const baseFetch = globalThis.$fetch;

  let refreshPromise: Promise<boolean> | null = null;

  const tryRefresh = (): Promise<boolean> => {
    if (refreshPromise) return refreshPromise;
    const refresh_token = localStorage.getItem("refresh_token");
    if (!refresh_token) return Promise.resolve(false);

    refreshPromise = (async () => {
      try {
        const res = await baseFetch<{
          access_token?: string;
          refresh_token?: string;
        }>(`${config.public.apiBase}/api/auth/refresh`, {
          method: "POST",
          body: { refresh_token }
        });
        if (res?.access_token) {
          localStorage.setItem("auth_token", res.access_token);
          if (res.refresh_token) {
            localStorage.setItem("refresh_token", res.refresh_token);
          }
          return true;
        }
        return false;
      } catch {
        return false;
      } finally {
        queueMicrotask(() => {
          refreshPromise = null;
        });
      }
    })();
    return refreshPromise;
  };

  const forceLogout = () => {
    localStorage.removeItem("auth_token");
    localStorage.removeItem("refresh_token");
    const route = useRoute();
    if (route.path !== "/login" && route.path !== "/") {
      navigateTo("/login", { replace: true });
    }
  };

  const wrappedFetch = (async (request: any, options: any = {}) => {
    try {
      return await baseFetch(request, options);
    } catch (err: any) {
      const status =
        err?.response?.status ?? err?.status ?? err?.statusCode;
      const url =
        typeof request === "string"
          ? request
          : request?.url || String(request);

      if (status !== 401 || !url.includes("/api/")) throw err;
      // не рефрешим сам refresh и не зацикливаемся на ретрае
      if (url.includes("/api/auth/refresh") || options.__authRetry) {
        forceLogout();
        throw err;
      }

      const ok = await tryRefresh();
      if (!ok) {
        forceLogout();
        throw err;
      }

      const newToken = localStorage.getItem("auth_token");
      const headers: Record<string, string> = { ...(options.headers || {}) };
      // переписываем Authorization (с учётом любого регистра ключа)
      for (const k of Object.keys(headers)) {
        if (k.toLowerCase() === "authorization") delete headers[k];
      }
      if (newToken) headers.Authorization = `Bearer ${newToken}`;
      return baseFetch(request, {
        ...options,
        headers,
        __authRetry: true
      });
    }
  }) as typeof baseFetch;

  nuxtApp.hook("app:created", () => {
    // @ts-ignore
    globalThis.$fetch = wrappedFetch;
  });
});
