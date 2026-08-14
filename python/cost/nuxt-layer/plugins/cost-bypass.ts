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
      // Берём ROLE_COST_ADMIN: под ней доступен весь раздел, включая
      // согласование и админку ролей, иначе половину страниц в этом контуре
      // не проверить. Плагин работает ТОЛЬКО при costOnly (см. ранний return),
      // то есть в изолированном контуре без Go-API и без пути к прод-данным.
      roles: ["ROLE_USER", "ROLE_COST_ADMIN"]
    };
  }
});
