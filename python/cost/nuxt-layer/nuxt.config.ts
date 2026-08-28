// Nuxt-layer раздела «Себестоимость».
// Главный nuxt.config.ts расширяется этим слоем (см. nuxt/nuxt.config.ts:extends).
// В рамках раздела разработчик правит только этот каталог.
export default defineNuxtConfig({
  runtimeConfig: {
    public: {
      // Адрес дашборда Superset, который встраивается под нашим на
      // /cost/commercial — ДЛЯ СРАВНЕНИЯ двух реализаций одного макета.
      //
      // Это dev-инструмент, а не архитектура встраивания: guest-токенов здесь
      // нет, iframe работает лишь потому, что браузер уже залогинен в Superset
      // на том же localhost. Прод-схема (guest token из Go-API, RLS из ABAC)
      // описана в docs/bi/superset-prod-plan.md и здесь не используется.
      //
      // Пустая строка — блок не показывается вовсе. Переопределяется
      // NUXT_PUBLIC_SUPERSET_EMBED_URL (см. python/cost/.env.example).
      supersetEmbedUrl:
        process.env.NUXT_PUBLIC_SUPERSET_EMBED_URL ??
        "http://localhost:8093/superset/dashboard/cost-commercial/",
      // Тот же макет «Маржа выпуска» в Superset — блок сравнения на /cost/margin.
      // Пустая строка → блока нет.
      supersetMarginEmbedUrl:
        process.env.NUXT_PUBLIC_SUPERSET_MARGIN_EMBED_URL ??
        "http://localhost:8093/superset/dashboard/cost-margin/",
    },
  },
});
