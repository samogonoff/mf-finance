import {createError, defineEventHandler, getCookie, getHeader, getQuery, setCookie} from "h3";

/**
 * Поиск сотрудников в Bitrix24 по фамилии — для прозрачной догрузки пользователей
 * и выбора директоров/ТОПов. Использует ТОТ ЖЕ OAuth-app, что и авторизация:
 * токен админа из httpOnly-cookie (сохранён в callback). При истечении токена —
 * молча обновляем через refresh_token + client_id/secret (отдельный вебхук не нужен).
 *
 * GET /api/b24/search-users?q=Иванов → [{ id, name, last_name, position, email }]
 */
type B24User = {
  ID?: string;
  NAME?: string;
  LAST_NAME?: string;
  EMAIL?: string;
  WORK_POSITION?: string;
};

export default defineEventHandler(async (event) => {
  const config = useRuntimeConfig();
  const q = String(getQuery(event).q ?? "").trim();
  if (q.length < 2) return [];

  const domain = getCookie(event, "b24_domain") || String(config.b24DefaultDomain);
  let token = getCookie(event, "b24_access_token");
  const refresh = getCookie(event, "b24_refresh_token");
  if (!token && !refresh) {
    throw createError({ statusCode: 401, statusMessage: "Нет B24-сессии — войдите через Bitrix24" });
  }

  const callUserGet = async (auth: string) =>
    $fetch<{ result?: B24User[]; error?: string; error_description?: string }>(
      `https://${domain}/rest/user.get.json`,
      { params: { auth, "FILTER[%LAST_NAME]": q, "FILTER[ACTIVE]": "Y" } }
    );

  const refreshToken = async (): Promise<string | null> => {
    if (!refresh) return null;
    const url = new URL(config.b24TokenUrl);
    url.searchParams.set("grant_type", "refresh_token");
    url.searchParams.set("client_id", String(config.public.b24ClientId));
    url.searchParams.set("client_secret", String(config.b24ClientSecret));
    url.searchParams.set("refresh_token", refresh);
    try {
      const r = await $fetch<{ access_token?: string; refresh_token?: string; expires_in?: number }>(url.toString());
      if (!r.access_token) return null;
      const secure = (getHeader(event, "x-forwarded-proto") || "http") === "https";
      const base = { httpOnly: true, sameSite: "lax" as const, secure, path: "/" };
      setCookie(event, "b24_access_token", r.access_token, { ...base, maxAge: r.expires_in || 3600 });
      if (r.refresh_token) setCookie(event, "b24_refresh_token", r.refresh_token, { ...base, maxAge: 60 * 60 * 24 * 30 });
      return r.access_token;
    } catch {
      return null;
    }
  };

  let data: { result?: B24User[]; error?: string } | null = null;
  if (token) {
    data = await callUserGet(token).catch(() => null);
  }
  // Истёкший/невалидный токен → обновляем и повторяем.
  if (!data || data.error) {
    const fresh = await refreshToken();
    if (!fresh) throw createError({ statusCode: 401, statusMessage: "B24-токен истёк — войдите заново" });
    token = fresh;
    data = await callUserGet(fresh).catch(() => null);
  }
  if (!data || data.error || !data.result) return [];

  return data.result
    .filter((u) => u.ID)
    .slice(0, 20)
    .map((u) => ({
      id: Number(u.ID),
      name: u.NAME || "",
      last_name: u.LAST_NAME || "",
      position: u.WORK_POSITION || "",
      email: u.EMAIL || ""
    }));
});
