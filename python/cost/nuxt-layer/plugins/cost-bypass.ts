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
      roles: ["ROLE_USER"]
    };
  }
});
