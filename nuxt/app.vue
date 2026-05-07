<template>
  <div class="app-shell">
    <!-- Боковая навигация (десктоп) -->
    <aside v-if="userValue" class="app-sidebar" :class="{ 'collapsed': sidebarCollapsed }">
      <div class="sidebar-brand">
        <div class="brand-mark">F</div>
        <div v-if="!sidebarCollapsed" class="brand-name">
          <span class="brand-name-strong">Finance</span>
          <span class="brand-name-sub">Cabinet</span>
        </div>
      </div>

      <nav class="nav-list">
        <NuxtLink to="/" class="nav-item" exact-active-class="router-link-active">
          <Icon name="lucide:layout-dashboard" class="nav-item-icon" />
          <span v-if="!sidebarCollapsed">Дашборд</span>
        </NuxtLink>
        <NuxtLink to="/operations" class="nav-item">
          <Icon name="lucide:arrow-right-left" class="nav-item-icon" />
          <span v-if="!sidebarCollapsed">Операции</span>
        </NuxtLink>
        <NuxtLink to="/reports" class="nav-item">
          <Icon name="lucide:file-bar-chart-2" class="nav-item-icon" />
          <span v-if="!sidebarCollapsed">Отчёты</span>
        </NuxtLink>
        <NuxtLink to="/counterparties" class="nav-item">
          <Icon name="lucide:users" class="nav-item-icon" />
          <span v-if="!sidebarCollapsed">Контрагенты</span>
        </NuxtLink>
        <NuxtLink to="/analytics" class="nav-item">
          <Icon name="lucide:line-chart" class="nav-item-icon" />
          <span v-if="!sidebarCollapsed">Аналитика</span>
        </NuxtLink>

        <template v-if="isAdmin">
          <div v-if="!sidebarCollapsed" class="nav-section">Админ</div>
          <div v-else class="nav-divider"></div>
          <NuxtLink to="/admin/users" class="nav-item">
            <Icon name="lucide:shield-check" class="nav-item-icon" />
            <span v-if="!sidebarCollapsed">Пользователи</span>
          </NuxtLink>
        </template>
      </nav>

      <div class="sidebar-footer">
        <button class="sidebar-collapse" @click="toggleSidebar" :aria-label="sidebarCollapsed ? 'Развернуть' : 'Свернуть'">
          <Icon :name="sidebarCollapsed ? 'lucide:chevron-right' : 'lucide:chevron-left'" class="nav-item-icon" />
        </button>
      </div>
    </aside>

    <!-- Контентная колонка -->
    <div class="app-body">
      <header v-if="userValue" class="app-topbar">
        <button
          class="topbar-burger"
          @click="toggleMobileMenu"
          :aria-expanded="showMobileMenu"
          aria-label="Меню"
        >
          <Icon name="lucide:menu" />
        </button>

        <EntitySwitcher />

        <div class="topbar-breadcrumbs">
          <span class="crumb-muted">Finance</span>
          <span class="crumb-sep">/</span>
          <span class="crumb-strong">{{ currentPageTitle }}</span>
        </div>

        <div class="topbar-actions">
          <button class="btn btn-ghost btn-icon" @click="toggleTheme" aria-label="Тема">
            <Icon :name="effective === 'dark' ? 'lucide:sun' : 'lucide:moon'" />
          </button>
          <NuxtLink to="/account" class="user-chip">
            <span class="user-dot"></span>
            <span class="user-name">{{ userValue?.name || userValue?.email }}</span>
          </NuxtLink>
          <button class="btn btn-ghost" @click="handleLogout" title="Выйти">
            <Icon name="lucide:log-out" />
          </button>
        </div>
      </header>

      <!-- Мобильное меню (drawer) -->
      <Transition name="drawer">
        <div v-if="showMobileMenu" class="mobile-drawer" @click.self="closeMobileMenu">
          <div class="mobile-drawer-panel">
            <div class="mobile-drawer-header">
              <span class="brand-name-strong">Finance</span>
              <button class="btn btn-ghost btn-icon" @click="closeMobileMenu" aria-label="Закрыть">
                <Icon name="lucide:x" />
              </button>
            </div>
            <nav class="nav-list">
              <NuxtLink to="/" exact-active-class="router-link-active" class="nav-item" @click="closeMobileMenu">
                <Icon name="lucide:layout-dashboard" class="nav-item-icon" />
                <span>Дашборд</span>
              </NuxtLink>
              <NuxtLink to="/operations" class="nav-item" @click="closeMobileMenu">
                <Icon name="lucide:arrow-right-left" class="nav-item-icon" />
                <span>Операции</span>
              </NuxtLink>
              <NuxtLink to="/reports" class="nav-item" @click="closeMobileMenu">
                <Icon name="lucide:file-bar-chart-2" class="nav-item-icon" />
                <span>Отчёты</span>
              </NuxtLink>
              <NuxtLink to="/counterparties" class="nav-item" @click="closeMobileMenu">
                <Icon name="lucide:users" class="nav-item-icon" />
                <span>Контрагенты</span>
              </NuxtLink>
              <NuxtLink to="/analytics" class="nav-item" @click="closeMobileMenu">
                <Icon name="lucide:line-chart" class="nav-item-icon" />
                <span>Аналитика</span>
              </NuxtLink>
              <div class="nav-section">Сервис</div>
              <NuxtLink to="/account" class="nav-item" @click="closeMobileMenu">
                <Icon name="lucide:user" class="nav-item-icon" />
                <span>Профиль</span>
              </NuxtLink>
              <button class="nav-item nav-logout" @click="handleLogout">
                <Icon name="lucide:log-out" class="nav-item-icon" />
                <span>Выйти</span>
              </button>
            </nav>
          </div>
        </div>
      </Transition>

      <main class="app-main">
        <div v-if="loadingValue && !userValue && hasToken" class="gate">
          <div class="loader"></div>
          <p class="muted">Проверка авторизации…</p>
        </div>

        <div v-else-if="errorValue && !userValue && hasToken" class="gate">
          <div class="auth-error-card">
            <p class="eyebrow">Авторизация</p>
            <h2>Не удалось подтвердить вход</h2>
            <p class="muted">
              {{ (errorValue as any)?.statusMessage || (errorValue as any)?.message || "Ошибка /api/auth/me" }}
            </p>
            <div class="error-actions">
              <button class="btn btn-primary" @click="goLogin">Войти снова</button>
            </div>
          </div>
        </div>

        <div v-else-if="!userValue" class="gate">
          <LoginCard />
        </div>

        <ClientOnly v-else>
          <NuxtPage />
          <template #fallback>
            <div class="gate"><div class="loader"></div></div>
          </template>
        </ClientOnly>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRuntimeConfig, navigateTo } from "nuxt/app";

const config = useRuntimeConfig();
const route = useRoute();
const { user, loading, error, fetchUser, logout } = useAuth();
const { hasScope, isAdmin } = useScope();
const { effective, toggle: toggleTheme } = useTheme();

useHead({
  htmlAttrs: {
    class: computed(() => effective.value === "dark" ? "theme-dark" : "theme-light"),
    style: computed(() => `color-scheme: ${effective.value}`)
  }
});

const userValue = computed(() => user.value);
const loadingValue = computed(() => loading.value);
const errorValue = computed(() => error.value);

const hasToken = computed(() => {
  if (typeof window === "undefined") return false;
  return !!localStorage.getItem("auth_token");
});

const sidebarCollapsed = ref(false);
const showMobileMenu = ref(false);

const toggleSidebar = () => { sidebarCollapsed.value = !sidebarCollapsed.value; };
const toggleMobileMenu = () => { showMobileMenu.value = !showMobileMenu.value; };
const closeMobileMenu = () => { showMobileMenu.value = false; };

const titleByPath: Record<string, string> = {
  "/": "Дашборд",
  "/operations": "Операции",
  "/reports": "Отчёты",
  "/counterparties": "Контрагенты",
  "/analytics": "Аналитика",
  "/account": "Профиль",
  "/admin/users": "Пользователи"
};
const currentPageTitle = computed(() => {
  const path = route.path;
  if (titleByPath[path]) return titleByPath[path];
  const parts = path.split("/").filter(Boolean);
  return parts[0] ? parts[0][0].toUpperCase() + parts[0].slice(1) : "Дашборд";
});

const handleLogout = async () => { await logout(); };
const goLogin = () => { navigateTo("/login", { replace: true }); };

const processB24Callback = async () => {
  if (typeof window === "undefined") return;
  const url = new URLSearchParams(window.location.search);
  const b24Auth = url.get("b24_auth");
  const dataParam = url.get("data");
  if (b24Auth !== "1" || !dataParam) return;

  try {
    const userData = JSON.parse(atob(dataParam));
    const response = await fetch(`${config.public.apiBase}/api/auth/b24/callback`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify(userData),
      credentials: "include"
    }).then((r) => r.json());

    if (response.access_token) localStorage.setItem("auth_token", response.access_token);
    if (response.refresh_token) localStorage.setItem("refresh_token", response.refresh_token);

    await fetchUser(true);
    await navigateTo("/", { replace: true });
  } catch (e) {
    console.error("[Finance] b24 callback failed", e);
  }
};

onMounted(async () => {
  await processB24Callback();
  if (hasToken.value && !userValue.value) await fetchUser(true);
});
</script>

<style>
/* ============================================================
 * App-shell — sidebar + content (full-width)
 * Используем flex (а не grid), чтобы дочерние таблицы не расталкивали
 * правую колонку через intrinsic min-content. min-width:0 на .app-body
 * — обязателен, без него flex-ребёнок не сжимается ниже своих детей.
 * ============================================================ */
.app-shell {
  min-height: 100vh;
  display: flex;
}

.app-sidebar {
  position: sticky;
  top: 0;
  height: 100vh;
  display: flex;
  flex-direction: column;
  background: var(--bg-surface);
  border-right: 1px solid var(--border);
  transition: flex-basis var(--t-base), width var(--t-base);
  flex: 0 0 220px;
  width: 220px;
  overflow: hidden;
}
.app-sidebar.collapsed {
  flex-basis: 56px;
  width: 56px;
}

.sidebar-brand {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  padding: var(--sp-6) var(--sp-5);
  border-bottom: 1px solid var(--border);
  min-height: 56px;
}
.brand-mark {
  width: 28px;
  height: 28px;
  border-radius: var(--rd-3);
  background: var(--accent);
  color: var(--accent-fg);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: var(--fw-bold);
  font-size: 14px;
  flex-shrink: 0;
  letter-spacing: -0.02em;
}
.brand-name {
  display: flex;
  flex-direction: column;
  line-height: 1.1;
  min-width: 0;
}
.brand-name-strong {
  font-size: var(--fs-md);
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
}
.brand-name-sub {
  font-size: var(--fs-2xs);
  font-weight: var(--fw-medium);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.app-sidebar .nav-list {
  flex: 1;
  padding: var(--sp-4) var(--sp-3);
  overflow-y: auto;
}
.app-sidebar.collapsed .nav-item {
  justify-content: center;
  padding: var(--sp-4);
}
.app-sidebar.collapsed .nav-item span:not(.nav-item-icon) {
  display: none;
}

.nav-divider {
  height: 1px;
  background: var(--border);
  margin: var(--sp-4) var(--sp-3);
}

.sidebar-footer {
  border-top: 1px solid var(--border);
  padding: var(--sp-3);
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.sidebar-collapse {
  background: transparent;
  border: 1px solid var(--border);
  border-radius: var(--rd-4);
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: var(--text-muted);
  transition: all var(--t-fast);
}
.sidebar-collapse:hover {
  background: var(--bg-surface-3);
  color: var(--text-strong);
}

/* ============================================================
 * Topbar
 * ============================================================ */
.app-body {
  /* flex: 1 1 0 + min-width:0 — флекс-ребёнок будет занимать всё оставшееся
     место и при этом может сжиматься ниже своего intrinsic-min-content.
     width:0 раньше давал «ужатый» первый рендер: на момент гидратации Vue
     внутри .app-main ещё нет реального контента, basis оставался нулевым. */
  flex: 1 1 0;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 100vh;
}

.app-topbar {
  display: flex;
  align-items: center;
  gap: var(--sp-5);
  height: 56px;
  padding: 0 var(--sp-7);
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
  position: sticky;
  top: 0;
  z-index: var(--z-sticky);
}
.topbar-burger {
  display: none;
  background: transparent;
  border: none;
  width: 32px;
  height: 32px;
  border-radius: var(--rd-4);
  cursor: pointer;
  color: var(--text-secondary);
  align-items: center;
  justify-content: center;
}
.topbar-burger:hover {
  background: var(--bg-surface-3);
  color: var(--text-strong);
}
.topbar-breadcrumbs {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  font-size: var(--fs-sm);
  flex: 1;
  min-width: 0;
}
.crumb-muted { color: var(--text-muted); }
.crumb-sep   { color: var(--text-muted); }
.crumb-strong {
  color: var(--text-strong);
  font-weight: var(--fw-semibold);
}
.topbar-actions {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
}

.user-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
  padding: 0 var(--sp-4);
  height: 32px;
  border: 1px solid var(--border);
  border-radius: var(--rd-4);
  font-size: var(--fs-sm);
  color: var(--text-primary);
  text-decoration: none;
  background: var(--bg-surface);
  transition: all var(--t-fast);
}
.user-chip:hover {
  border-color: var(--border-strong);
  color: var(--text-strong);
}
.user-dot {
  width: 6px;
  height: 6px;
  background: var(--pos);
  border-radius: 50%;
  box-shadow: 0 0 0 2px var(--pos-soft);
}
.user-name {
  max-width: 200px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* ============================================================
 * Main / gates
 * ============================================================ */
.app-main {
  flex: 1 1 auto;
  padding: var(--sp-7);
  background: var(--bg-app);
  min-width: 0;
  /* Скролл по горизонтали запрещаем на уровне страницы — за горизонталь
     отвечают сами .table-wrap внутри. */
  overflow-x: hidden;
  /* container-type: inline-size — позволяет дочерним элементам
     использовать @container queries (см. .kpi-grid в страницах).
     Это надёжнее media-queries, потому что считается реальная
     ширина main, а не viewport. */
  container-type: inline-size;
  container-name: main;
}
@media (min-width: 1920px) {
  .app-main { padding: var(--sp-9); }
}

.gate {
  width: 100%;
  min-height: calc(100vh - 96px);
  display: flex;
  align-items: center;
  justify-content: center;
}

.loader {
  width: 28px;
  height: 28px;
  border: 3px solid var(--border);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }
.muted { color: var(--text-muted); font-size: var(--fs-sm); margin-top: var(--sp-4); }

.auth-error-card {
  width: min(420px, 100%);
  padding: var(--sp-7);
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-5);
}
.auth-error-card .eyebrow {
  font-size: var(--fs-2xs);
  font-weight: var(--fw-bold);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--text-muted);
  margin: 0 0 var(--sp-3);
}
.auth-error-card h2 {
  font-size: var(--fs-lg);
  margin-bottom: var(--sp-3);
}
.error-actions {
  margin-top: var(--sp-6);
}

/* ============================================================
 * Mobile drawer
 * ============================================================ */
.mobile-drawer {
  position: fixed;
  inset: 0;
  background: var(--bg-overlay);
  z-index: var(--z-drawer);
  display: flex;
}
.mobile-drawer-panel {
  width: 280px;
  background: var(--bg-surface);
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow-y: auto;
}
.mobile-drawer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--sp-5) var(--sp-6);
  border-bottom: 1px solid var(--border);
}
.mobile-drawer-panel .nav-list {
  flex: 1;
  padding: var(--sp-4) var(--sp-3);
}
.nav-logout {
  background: transparent;
  border: none;
  text-align: left;
  font-family: inherit;
  width: 100%;
  color: var(--neg);
}
.nav-logout:hover {
  background: var(--neg-soft);
  color: var(--neg-strong);
}

.drawer-enter-active,
.drawer-leave-active { transition: opacity var(--t-fast); }
.drawer-enter-from,
.drawer-leave-to { opacity: 0; }
.drawer-enter-active .mobile-drawer-panel,
.drawer-leave-active .mobile-drawer-panel { transition: transform var(--t-base); }
.drawer-enter-from .mobile-drawer-panel,
.drawer-leave-to .mobile-drawer-panel { transform: translateX(-100%); }

/* ============================================================
 * Адаптив
 * ============================================================ */
@media (max-width: 1024px) {
  .app-sidebar { display: none; }
  .topbar-burger { display: inline-flex; }
}

@media (max-width: 768px) {
  .app-topbar { padding: 0 var(--sp-5); }
  .app-main { padding: var(--sp-5); }
  .user-name { display: none; }
}
</style>
