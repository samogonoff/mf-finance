/**
 * Права ВНУТРИ раздела «Себестоимость» (cost:view, cost:admin, …).
 *
 * Не путать с ролями кабинета (ROLE_*, см. nuxt/composables/useScope.ts): это
 * две разные системы. Права раздела живут в БД cost (cost_user_roles →
 * cost_roles) и приезжают из GET /api/cost/roles/my. Роль «Full Admin» в
 * разделе не делает пользователя админом кабинета, и наоборот.
 *
 * Состояние ОБЩЕЕ (useState), а не локальное для каждого компонента: раньше
 * каждый вызывающий компонент держал свой ref и дёргал свой запрос — на одной
 * странице это несколько одинаковых запросов и разное время появления кнопок.
 */
export const useCostPermission = () => {
  const user = useState<any>("auth-user");
  const config = useRuntimeConfig();
  const apiBase = computed(() =>
    config.public.costOnly ? "" : ((config.public.apiBase as string) || "")
  );

  const permissions = useState<string[]>("cost-permissions", () => []);
  const roles = useState<any[]>("cost-roles", () => []);
  const loading = useState<boolean>("cost-permissions-loading", () => true);
  /** Текст ошибки загрузки прав. Пустая строка — ошибки нет. */
  const error = useState<string>("cost-permissions-error", () => "");
  // Флаг «права уже загружены». Проверять непустой список нельзя: у
  // пользователя без прав список пуст законно, и каждый монтируемый компонент
  // дёргал бы запрос заново. Ставится только при успехе — после ошибки
  // следующий компонент повторит попытку.
  const loaded = useState<boolean>("cost-permissions-loaded", () => false);
  // Один запрос на страницу: пока он в полёте, остальные вызывающие ждут его,
  // а не заводят свой.
  const inFlight = useState<Promise<void> | null>("cost-permissions-inflight", () => null);

  async function load(): Promise<void> {
    loading.value = true;
    error.value = "";
    try {
      const email = user.value?.email;
      if (!email) {
        // Пользователя ещё нет — это не ошибка прав, а незавершённая
        // авторизация. Права оставляем пустыми и пробуем позже.
        permissions.value = [];
        roles.value = [];
        return;
      }
      const data: any = await $fetch(`${apiBase.value}/api/cost/roles/my`, {
        headers: { "X-Cost-User": email },
      });
      permissions.value = data.permissions || [];
      roles.value = data.roles || [];
      loaded.value = true;
    } catch (e: any) {
      // МОЛЧА ГЛОТАТЬ ЗДЕСЬ НЕЛЬЗЯ. Пустой список прав и упавший запрос
      // выглядят на странице одинаково — кнопок нет, — и «у меня полный админ,
      // а кнопок не видно» превращается в неотлаживаемое. Пишем и в консоль, и
      // в error, чтобы причину было видно.
      permissions.value = [];
      roles.value = [];
      error.value = e?.data?.detail || e?.message || "Не удалось загрузить права раздела";
      console.error("[cost] не удалось загрузить права раздела", e);
    } finally {
      loading.value = false;
    }
  }

  async function fetchMyPermissions(): Promise<void> {
    if (inFlight.value) return inFlight.value;
    const p = load().finally(() => {
      inFlight.value = null;
    });
    inFlight.value = p;
    return p;
  }

  function can(perm: string): boolean {
    return permissions.value.includes(perm);
  }

  onMounted(() => {
    // Уже загружено другим компонентом — второй запрос не нужен.
    if (loaded.value || inFlight.value) return;
    fetchMyPermissions();
  });

  return { permissions, roles, loading, error, can, fetchMyPermissions };
};
