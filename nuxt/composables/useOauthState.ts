/**
 * CSRF-state для OAuth-флоу B24. Кладём UUID в cookie перед редиректом
 * на провайдер; колбэк-роут сверит cookie со state из query.
 */
export const useOauthState = () => {
  const stateCookie = useCookie<string>("b24_oauth_state", {
    sameSite: "lax",
    secure: false
  });

  const refresh = () => {
    const random = crypto.randomUUID
      ? crypto.randomUUID()
      : Math.random().toString(36).slice(2);
    stateCookie.value = random;
    return random;
  };

  return {
    state: computed(() => stateCookie.value || refresh()),
    refresh
  };
};
