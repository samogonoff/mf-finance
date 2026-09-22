/**
 * Текст ошибки API для показа человеку.
 *
 * Go-хендлеры отдают тело `{"error": "…"}` с осмысленным объяснением: «этап 1.1
 * закрывается сам, когда формы пройдут шаг 1.1», «период закрыт на запись» и т.п.
 * У ofetch в `Error.message` при этом лежит только `[POST] "<url>": 400` — тело
 * ответа в него не попадает. Пока страницы читали `e.message`, пользователь
 * видел голый код без причины (замечание аналитика 10.09.2026 по «Двигать этап»).
 *
 * Поэтому сначала смотрим в тело ответа, потом в сообщение исключения (там
 * оседают сетевые сбои), и только затем — в подсказку страницы.
 */
export const apiError = (e: unknown, fallback = "Ошибка"): string => {
  const body = (e as { data?: { error?: unknown } } | null | undefined)?.data;
  if (body && typeof body.error === "string" && body.error.trim()) {
    return body.error.trim();
  }
  if (e instanceof Error && e.message) {
    return e.message;
  }
  return fallback;
};
