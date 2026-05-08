export default defineNuxtConfig({
  devtools: { enabled: false },
  // Раздел «Себестоимость» — отдельный модуль. Шаблоны и плагины раздела
  // лежат в python/cost/nuxt-layer (там же, где FastAPI и миграции этого
  // раздела), чтобы разработчик правил всё в одном каталоге.
  extends: ["../python/cost/nuxt-layer"],
  modules: ["@nuxt/icon"],
  icon: {
    mode: "svg",
    customCollections: [],
    clientBundle: { scan: true }
  },
  vite: {
    server: {
      allowedHosts: ["finance.local", "api.finance.local", "localhost", ".local"],
      hmr: {
        // HMR должен работать через nginx-фасад на 80-м, и напрямую на 3001.
        clientPort: process.env.NUXT_HMR_CLIENT_PORT ? Number(process.env.NUXT_HMR_CLIENT_PORT) : undefined
      }
    }
  },
  css: [
    "~/assets/styles/design-system.css",
    "~/assets/styles/components.css",
    "~/assets/styles/forms.css"
  ],
  runtimeConfig: {
    public: {
      apiBase: process.env.NUXT_PUBLIC_API_BASE || "http://api.finance.local",
      mpUrl: process.env.NUXT_PUBLIC_MP_URL || "https://mp.local",
      b24AuthUrl:
        process.env.NUXT_PUBLIC_B24_AUTH_URL ||
        "https://mfportal.by/oauth/authorize/",
      b24ClientId: process.env.NUXT_PUBLIC_B24_CLIENT_ID || "local.finance.xxx",
      authRedirect:
        process.env.NUXT_PUBLIC_AUTH_REDIRECT ||
        "http://finance.local:3001/api/auth/b24/callback",
      // Поднят docker-compose.cost.yml: бэк только cost, auth-гейт скипается.
      costOnly: process.env.NUXT_PUBLIC_COST_ONLY === "1"
    },
    b24ClientSecret: process.env.B24_CLIENT_SECRET || "",
    b24TokenUrl: process.env.B24_TOKEN_URL || "https://mfportal.by/oauth/token/",
    b24DefaultDomain: process.env.B24_DEFAULT_DOMAIN || "mfportal.by"
  },
  app: {
    head: {
      title: "Finance Cabinet",
      meta: [
        { name: "viewport", content: "width=device-width, initial-scale=1" },
        { name: "color-scheme", content: "light dark" },
        { name: "theme-color", content: "#4338ca" }
      ],
      link: [
        { rel: "icon", type: "image/svg+xml", href: "/favicon.svg" },
        { rel: "mask-icon", href: "/favicon-mask.svg", color: "#4338ca" },
        { rel: "apple-touch-icon", href: "/favicon.svg" }
      ]
    }
  }
});
