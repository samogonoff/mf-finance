// Универсальный плагин (выполняется и на SSR, и на клиенте).
//
// В режиме NUXT_PUBLIC_COST_ONLY=1 (запуск через docker-compose.cost.yml) нет
// ни Go-API, ни OAuth-обмена — авторизоваться разработчику негде. Кладём
// заглушку пользователя в общий useState("auth-user") до рендера, чтобы
// app.vue не показал <LoginCard /> на SSR (иначе при перезагрузке мелькает
// «текст без стилей» — мы видим SSR-разметку до загрузки CSS).
//
// В обычном (полном) контуре плагин — no-op: costOnly=false, ничего не происходит.
//
// Редирект "/" → "/cost" вынесен в middleware/cost-redirect.global.ts —
// миддлвара корректнее работает с навигацией на обоих сторонах.
export default defineNuxtPlugin(() => {
  const config = useRuntimeConfig();
  if (!config.public.costOnly) return;

  const user = useState<{
    id?: number;
    email?: string;
    name?: string;
    roles?: string[];
  } | null>("auth-user", () => null);

  if (!user.value) {
    user.value = {
      id: 0,
      email: "cost-dev@local",
      name: "Cost Dev",
      // ROLE_USER одной недостаточно: глобальный гард nuxt/middleware/scope-guard.ts
      // пускает на /cost* только по scope "cost", то есть при ROLE_ADMIN,
      // ROLE_COST_ADMIN или ROLE_COST_USER. С одной ROLE_USER контур, смысл
      // которого — работа над разделом без авторизации, редиректил /cost на
      // корень и показывал форму логина.
      //
      // Разъехалось давно и незаметно: плагин написан раньше, чем в гард
      // добавили ветку /cost (перенос из MP), а разработка идёт через основной
      // контур с реальной B24-сессией, где роль настоящая.
      //
      // ROLE_ADMIN — потому что права раздела и роли кабинета это ДВЕ РАЗНЫЕ
      // системы, и их легко перепутать:
      //   * роли раздела живут в БД cost (cost_user_roles → cost_roles) и дают
      //     права вида cost:view / cost:admin. Ими гейтятся кнопки ВНУТРИ
      //     раздела — см. useCostPermission.can();
      //   * роли кабинета (users.roles в БД finance, здесь — эта заглушка) дают
      //     ROLE_*. Ими гейтится всё ВОКРУГ раздела: сайдбар, блок «Админ»
      //     (nuxt/app.vue проверяет isAdmin, то есть ровно ROLE_ADMIN),
      //     скоупы в useScope.
      // Роль «Full Admin» в разделе (со всеми cost:*) на кабинет не влияет
      // никак — с ROLE_COST_ADMIN блок «Админ» в сайдбаре не показывался, и это
      // читалось как «полный админ, а кнопок нет».
      //
      // ВНИМАНИЕ: страницы кабинета за этим блоком (/admin/users,
      // /admin/bugtracker) ходят в Go-API, которого в cost-only контуре нет —
      // ссылки появятся, но сами страницы работать не будут. Разделы, скрытые
      // по isCostOnly (Дашборд, Операции, Отчёты, Планы, Контрагенты,
      // Аналитика), так и останутся скрыты — им тоже нужен Go-API.
      //
      // Плагин работает ТОЛЬКО при costOnly (см. ранний return), то есть в
      // изолированном контуре без Go-API и без пути к прод-данным.
      roles: ["ROLE_USER", "ROLE_ADMIN", "ROLE_COST_ADMIN"]
    };
  }
});
