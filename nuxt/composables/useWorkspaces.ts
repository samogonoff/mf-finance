/**
 * Реестр разделов кабинета для переключателя в шапке.
 *
 * Тот же контракт, что у селектора сервисов в MP (SHELL-CONTRACT.md §2.1):
 * один список, из него берутся и подпись текущего раздела, и выпадающее меню.
 * Разделы фильтруются по ролям — список видимых собирает `visible`.
 *
 * Правило навигации (важное, из MP): переход в ДРУГОЙ раздел — всегда жёсткий
 * (`window.location.assign`), если раздел `external`. Обособленный контур живёт
 * за тем же origin, но это другой бэкенд: клиентский роутинг Nuxt попытался бы
 * отрисовать чужой путь своим приложением и отдал бы 404.
 *
 * Пока реестр объявлен на фронте. Когда обособленных контуров станет больше
 * одного, он переезжает в Go (`GET /api/app/header`, как в MP), и добавление
 * раздела перестаёт требовать передеплоя фронта.
 */

export interface Workspace {
  key: string;
  label: string;
  hint: string;
  icon: string;
  /** Адрес «домашней» страницы раздела на том же origin. */
  home: string;
  /** true — раздел вне Nuxt (обособленный контур за nginx). */
  external: boolean;
  /** Кому показывать; null — всем аутентифицированным. */
  scope: "cost" | "budget" | null;
}

export const WORKSPACES: Workspace[] = [
  {
    key: "finance",
    label: "Кабинет финансиста",
    hint: "Отчёты, операции, тактические планы",
    icon: "lucide:wallet",
    home: "/",
    external: false,
    scope: null
  },
  {
    key: "cost",
    label: "Себестоимость",
    hint: "Калькуляции, цены, согласования",
    icon: "lucide:package",
    home: "/cost",
    external: false,
    scope: "cost"
  },
  {
    key: "budget",
    label: "Бюджетирование",
    hint: "Бюджеты и план-факт",
    icon: "lucide:landmark",
    home: "/budget/",
    external: true,
    scope: "budget"
  }
];

export const useWorkspaces = () => {
  const route = useRoute();
  const config = useRuntimeConfig();
  const { hasScope } = useScope();

  // Раздел «Бюджетирование» — отдельный контур в своём репозитории. Пока он не
  // поднят, пункт прятать: иначе единственное, что он даёт, — 502 от nginx.
  const budgetEnabled = computed(() => !!config.public.budgetEnabled);

  const visible = computed<Workspace[]>(() =>
    WORKSPACES.filter((w) => {
      if (w.key === "budget" && !budgetEnabled.value) return false;
      return w.scope === null || hasScope(w.scope);
    })
  );

  /** Текущий раздел — по самому длинному совпавшему `home` (кроме корневого). */
  const current = computed<Workspace>(() => {
    const path = route.path;
    const match = WORKSPACES.filter((w) => w.home !== "/")
      .filter((w) => path === w.home.replace(/\/$/, "") || path.startsWith(w.home))
      .sort((a, b) => b.home.length - a.home.length)[0];
    return match ?? WORKSPACES[0];
  });

  const go = (w: Workspace) => {
    if (w.key === current.value.key) return;
    if (w.external) {
      window.location.assign(w.home);
      return;
    }
    navigateTo(w.home);
  };

  return { workspaces: visible, current, go };
};
