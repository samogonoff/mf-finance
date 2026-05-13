import {createError, defineEventHandler, getCookie, getHeader, getQuery, setCookie} from "h3";

/**
 * OAuth-колбэк B24:
 *   1. Получаем code, обмениваем на B24 access_token.
 *   2. Тащим из user.current данные пользователя.
 *   3. POST на Go-API → выдача наших access/refresh токенов.
 *   4. Возвращаем HTML, кладущий токены в localStorage и редиректящий в /.
 */
export default defineEventHandler(async (event) => {
  const config = useRuntimeConfig();
  const query = getQuery(event);
  const code = query.code as string | undefined;
  const incomingState = query.state as string | undefined;
  const domain = query.domain as string | undefined;
  const serverDomain = query.server_domain as string | undefined;

  if (!code) {
    throw createError({ statusCode: 400, statusMessage: "Missing auth code" });
  }

  const storedState = getCookie(event, "b24_oauth_state");
  if (storedState && incomingState && incomingState !== storedState) {
    throw createError({ statusCode: 400, statusMessage: "Invalid oauth state" });
  }

  // В новой схеме B24 обмен code → token идёт на server_domain из колбэка,
  // а не на домен портала. server_domain выглядит как oauth.bitrix24.tech.
  const tokenUrl = serverDomain
    ? new URL(`https://${serverDomain}/oauth/token/`)
    : new URL(config.b24TokenUrl);
  tokenUrl.searchParams.set("client_id", String(config.public.b24ClientId));
  tokenUrl.searchParams.set("client_secret", String(config.b24ClientSecret));
  tokenUrl.searchParams.set("code", code);
  tokenUrl.searchParams.set("grant_type", "authorization_code");

  let response: {
    access_token?: string;
    refresh_token?: string;
    expires_in?: number;
    error?: string;
    error_description?: string;
  };
  try {
    response = await $fetch(tokenUrl.toString());
  } catch (err: any) {
    const body = err?.data ?? err?.response?._data;
    console.error("[b24/callback] token exchange failed", {
      url: tokenUrl.toString().replace(/client_secret=[^&]+/, "client_secret=***"),
      status: err?.statusCode || err?.response?.status,
      body
    });
    throw createError({
      statusCode: 400,
      statusMessage:
        (body && (body.error_description || body.error)) ||
        err?.message ||
        "Token exchange failed"
    });
  }

  if (response.error || !response.access_token) {
    console.error("[b24/callback] token exchange returned error", response);
    throw createError({
      statusCode: 400,
      statusMessage: response.error_description || response.error || "No access token received"
    });
  }

  let domainToSave = domain;
  if (!domainToSave) {
    try {
      const authUrl = new URL(String(config.public.b24AuthUrl));
      domainToSave = authUrl.hostname;
    } catch {
      domainToSave = String(config.b24DefaultDomain);
    }
  }

  const userInfoUrl = `https://${domainToSave}/rest/user.current?auth=${response.access_token}`;
  const userInfo = await $fetch<{
    result?: {
      ID?: string;
      NAME?: string;
      LAST_NAME?: string;
      EMAIL?: string;
      PERSONAL_PHOTO?: string;
      WORK_POSITION?: string;
    };
    error?: string;
  }>(userInfoUrl);

  if (!userInfo.result || userInfo.error) {
    throw createError({ statusCode: 401, statusMessage: "Failed to fetch B24 user" });
  }

  const userData = {
    email: userInfo.result.EMAIL,
    name: userInfo.result.NAME,
    lastName: userInfo.result.LAST_NAME,
    photo: userInfo.result.PERSONAL_PHOTO,
    position: userInfo.result.WORK_POSITION,
    b24Id: userInfo.result.ID,
    b24Domain: domainToSave
  };

  const internalApiUrl = process.env.NUXT_INTERNAL_API_BASE;
  const apiUrl = internalApiUrl
    ? `${internalApiUrl}/api/auth/b24/callback`
    : `${config.public.apiBase || "http://api.local"}/api/auth/b24/callback`;

  const apiResponse = await $fetch<{
    access_token?: string;
    refresh_token?: string;
    expires_in?: number;
    user?: { id: number; email: string; name: string; roles: string[] };
  }>(apiUrl, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: userData
  });

  setCookie(event, "b24_oauth_state", "", { httpOnly: true, maxAge: 0 });

  const host =
    getHeader(event, "host") || getHeader(event, "x-forwarded-host") || "nuxt.local:3000";
  const protocol = getHeader(event, "x-forwarded-proto") || "http";
  const frontendUrl = `${protocol}://${host}`;

  const html = `<!DOCTYPE html>
<html>
<head><title>Авторизация…</title><meta charset="UTF-8"></head>
<body>
  <script>
    ${
      apiResponse.access_token
        ? `localStorage.setItem("auth_token", ${JSON.stringify(apiResponse.access_token)});`
        : ""
    }
    ${
      apiResponse.refresh_token
        ? `localStorage.setItem("refresh_token", ${JSON.stringify(apiResponse.refresh_token)});`
        : ""
    }
    window.location.href = ${JSON.stringify(frontendUrl + "/?auth_success=1")};
  </script>
  <p>Авторизация завершена. Перенаправление…</p>
</body>
</html>`;

  event.node.res.setHeader("Content-Type", "text/html; charset=utf-8");
  return html;
});
