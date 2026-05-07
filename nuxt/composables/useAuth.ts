import {readonly} from "vue";

export const useAuth = () => {
  const config = useRuntimeConfig();

  const getAuthToken = (): string | null => {
    if (process.client) {
      return localStorage.getItem("auth_token");
    }
    return null;
  };

  const getRefreshToken = (): string | null => {
    if (process.client) {
      return localStorage.getItem("refresh_token");
    }
    return null;
  };

  const user = useState<{
    id?: number;
    email?: string;
    name?: string;
    roles?: string[];
  } | null>("auth-user", () => null);
  const loading = useState<boolean>("auth-loading", () => false);
  const error = useState<unknown>("auth-error", () => null);

  let isFetching = false;
  let fetchPromise: Promise<void> | null = null;
  let lastFetchTime = 0;
  const FETCH_COOLDOWN = 1000;

  const fetchUser = async (force = false) => {
    const now = Date.now();
    if (!force && now - lastFetchTime < FETCH_COOLDOWN && user.value !== null) {
      return;
    }
    if (isFetching && fetchPromise) return fetchPromise;

    const token = getAuthToken();
    if (!token) {
      user.value = null;
      error.value = null;
      return;
    }

    isFetching = true;
    loading.value = true;
    error.value = null;
    lastFetchTime = now;

    fetchPromise = (async () => {
      try {
        const data = await $fetch<{
          id?: number;
          email?: string;
          name?: string;
          roles?: string[];
        }>(`${config.public.apiBase}/api/auth/me`, {
          server: false,
          headers: { Authorization: `Bearer ${token}` }
        });
        user.value = data;
        error.value = null;
      } catch (err: any) {
        if (err?.status === 401 || err?.statusCode === 401) {
          error.value = null;
          user.value = null;
          if (process.client) {
            localStorage.removeItem("auth_token");
            localStorage.removeItem("refresh_token");
          }
        } else {
          error.value = err;
        }
      } finally {
        loading.value = false;
        isFetching = false;
        fetchPromise = null;
      }
    })();

    return fetchPromise;
  };

  const refreshToken = async () => {
    const currentRefreshToken = getRefreshToken();
    if (!currentRefreshToken) return false;

    try {
      const response = await $fetch<{
        access_token?: string;
        refresh_token?: string;
        expires_in?: number;
        error?: string;
      }>(`${config.public.apiBase}/api/auth/refresh`, {
        method: "POST",
        body: { refresh_token: currentRefreshToken }
      });

      if (response.access_token) {
        localStorage.setItem("auth_token", response.access_token);
        if (response.refresh_token) {
          localStorage.setItem("refresh_token", response.refresh_token);
        }
        await fetchUser(true);
        return true;
      }
      if (process.client) {
        localStorage.removeItem("auth_token");
        localStorage.removeItem("refresh_token");
      }
      user.value = null;
      return false;
    } catch {
      if (process.client) {
        localStorage.removeItem("auth_token");
        localStorage.removeItem("refresh_token");
      }
      user.value = null;
      return false;
    }
  };

  const logout = async () => {
    const token = getAuthToken();
    try {
      await $fetch(`${config.public.apiBase}/api/auth/logout`, {
        method: "POST",
        headers: token ? { Authorization: `Bearer ${token}` } : {}
      });
    } catch {
      /* ignore */
    }
    if (process.client) {
      localStorage.removeItem("auth_token");
      localStorage.removeItem("refresh_token");
    }
    user.value = null;
    await navigateTo("/?logout=1");
  };

  return {
    user: readonly(user),
    loading: readonly(loading),
    error: readonly(error),
    fetchUser,
    logout,
    refreshToken
  };
};
