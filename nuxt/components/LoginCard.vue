<template>
  <div class="login-card">
    <div class="login-brand">
      <div class="brand-mark">F</div>
      <div class="brand-name">
        <span class="brand-name-strong">Finance Cabinet</span>
        <span class="brand-name-sub">для финансистов</span>
      </div>
    </div>

    <h1>Войти в кабинет</h1>
    <p class="login-lead">
      Авторизация через Bitrix24. Кабинет работает на отдельном
      приложении B24 и изолирован от MP-портала.
    </p>

    <p v-if="errorRef" class="form-error">
      {{ (errorRef as any)?.statusMessage || (errorRef as any)?.message || "Ошибка авторизации" }}
    </p>

    <button
      v-if="!loadingRef"
      class="btn btn-primary btn-lg login-btn"
      :class="{ loading: redirecting }"
      :disabled="redirecting"
      @click="redirectToB24"
    >
      <span v-if="redirecting" class="login-spinner"></span>
      <span>{{ redirecting ? "Перенаправление…" : "Войти через B24" }}</span>
    </button>
    <div v-else class="btn btn-lg login-btn loading">
      <span class="login-spinner"></span>
      Проверяем сессию…
    </div>

    <div class="login-meta">
      <span>Защищено OAuth · CSRF state: <code>{{ state.slice(0, 8) }}…</code></span>
    </div>

    <a v-if="mpUrl" :href="mpUrl" class="login-cabinet-switch">
      <Icon name="lucide:arrow-left-right" />
      <span>Перейти в кабинет «Маркетплейсы»</span>
    </a>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useRoute, useRuntimeConfig } from "nuxt/app";

const redirecting = ref(false);
const config = useRuntimeConfig();
const { state, refresh } = useOauthState();
const { loading, error, fetchUser } = useAuth();
const route = useRoute();

const loadingRef = computed(() => loading.value);
const errorRef = computed(() => error.value);

const mpUrl = computed(() => (config.public as any).mpUrl as string | undefined);

const redirectToB24 = () => {
  if (redirecting.value) return;
  redirecting.value = true;
  const stateValue = refresh();
  const url = new URL(String(config.public.b24AuthUrl));
  url.searchParams.set("client_id", String(config.public.b24ClientId));
  url.searchParams.set("response_type", "code");
  url.searchParams.set("redirect_uri", String(config.public.authRedirect));
  url.searchParams.set("state", stateValue);
  window.location.href = url.toString();
};

onMounted(async () => {
  const authSuccess = route.query.auth_success === "1";
  const hasToken = typeof window !== "undefined" ? !!localStorage.getItem("auth_token") : false;
  if ((authSuccess || hasToken) && !loadingRef.value) {
    await fetchUser();
  }
});
</script>

<style scoped>
.login-card {
  width: min(420px, 100%);
  padding: var(--sp-9);
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-5);
}

.login-brand {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  margin-bottom: var(--sp-8);
}
.brand-mark {
  width: 36px;
  height: 36px;
  border-radius: var(--rd-4);
  background: var(--accent);
  color: var(--accent-fg);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: var(--fw-bold);
  font-size: 18px;
  letter-spacing: -0.02em;
}
.brand-name { display: flex; flex-direction: column; line-height: 1.2; }
.brand-name-strong {
  font-size: var(--fs-md);
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
}
.brand-name-sub {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

h1 {
  font-size: var(--fs-xl);
  font-weight: var(--fw-semibold);
  margin: 0 0 var(--sp-3);
  color: var(--text-strong);
  letter-spacing: -0.02em;
}
.login-lead {
  margin: 0 0 var(--sp-7);
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  line-height: var(--lh-relaxed);
}

.login-btn {
  width: 100%;
  height: 40px;
  font-size: var(--fs-md);
  gap: var(--sp-3);
}
.login-btn.loading {
  background: var(--bg-surface-3);
  border-color: var(--border);
  color: var(--text-secondary);
  cursor: default;
}

.login-spinner {
  width: 14px;
  height: 14px;
  border-radius: 50%;
  border: 2px solid currentColor;
  border-top-color: transparent;
  animation: spin 0.7s linear infinite;
  opacity: 0.6;
}
@keyframes spin { to { transform: rotate(360deg); } }

.login-meta {
  margin-top: var(--sp-7);
  padding-top: var(--sp-5);
  border-top: 1px dashed var(--border);
  font-size: var(--fs-xs);
  color: var(--text-muted);
  text-align: center;
  font-family: var(--font-mono);
}

.login-cabinet-switch {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--sp-3);
  margin-top: var(--sp-5);
  padding: var(--sp-4) var(--sp-5);
  font-size: var(--fs-sm);
  font-weight: var(--fw-medium);
  color: var(--text-secondary);
  background: var(--bg-surface-2);
  border: 1px dashed var(--border-strong);
  border-radius: var(--rd-4);
  text-decoration: none;
  transition: all var(--t-fast);
}
.login-cabinet-switch:hover {
  border-style: solid;
  border-color: var(--accent);
  color: var(--accent);
  background: var(--accent-soft);
}
.login-cabinet-switch :deep(svg) { width: 14px; height: 14px; }
</style>
