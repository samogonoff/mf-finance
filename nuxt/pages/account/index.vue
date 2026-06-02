<template>
  <div class="page-account">
    <header class="page-header">
      <div>
        <h1 class="page-title">Профиль</h1>
        <p class="page-subtitle">Данные пользователя из Bitrix24</p>
      </div>
    </header>

    <div class="account-grid">
      <div class="card">
        <div class="card-header">
          <div class="card-title">Учётные данные</div>
        </div>
        <div class="card-body account-info">
          <dl>
            <dt>Имя</dt><dd>{{ user?.name || "—" }}</dd>
            <dt>Email</dt><dd class="num">{{ user?.email || "—" }}</dd>
            <dt>ID</dt><dd class="num">{{ user?.id ?? "—" }}</dd>
            <dt>Роли</dt>
            <dd class="role-list">
              <span v-for="r in (user?.roles || [])" :key="r" class="badge badge-accent">{{ r }}</span>
            </dd>
          </dl>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <div class="card-title">Уведомления</div>
        </div>
        <div class="card-body">
          <label class="switch-row">
            <input type="checkbox" :checked="settings.notify_via_b24"
                   :disabled="!settings.b24_delivery_active || saving"
                   @change="toggleB24(($event.target as HTMLInputElement).checked)" />
            <span>
              Дублировать уведомления в Битрикс24
              <small v-if="!settings.b24_delivery_active" class="hint">
                (доставка выключена: B24_NOTIFY_WEBHOOK_URL не задан)
              </small>
              <small v-else-if="!settings.has_b24_id" class="hint">
                (вы не привязаны к Б24-пользователю — доставка пропускается)
              </small>
            </span>
          </label>
          <p v-if="msg" class="account-hint">{{ msg }}</p>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <div class="card-title">Сессия</div>
        </div>
        <div class="card-body">
          <p class="account-hint">Access-токен живёт 1 час, refresh — 30 дней.</p>
          <button class="btn btn-danger" @click="logout">Завершить сессию</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from "vue";

definePageMeta({ middleware: "scope-guard" });
const { user, logout } = useAuth();
const config = useRuntimeConfig();
const base = config.public.apiBase;

interface Settings {
  notify_via_b24: boolean;
  b24_delivery_active: boolean;
  has_b24_id: boolean;
}
const settings = ref<Settings>({ notify_via_b24: true, b24_delivery_active: false, has_b24_id: false });
const saving = ref(false);
const msg = ref<string | null>(null);

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

const load = async () => {
  try {
    settings.value = await $fetch<Settings>(`${base}/api/account/notification-settings`, {
      headers: authHeader()
    });
  } catch (e: any) {
    msg.value = e?.data?.error || e?.message || "Не удалось загрузить настройки";
  }
};

const toggleB24 = async (v: boolean) => {
  saving.value = true;
  msg.value = null;
  try {
    await $fetch(`${base}/api/account/notification-settings`, {
      method: "PATCH",
      body: { notify_via_b24: v },
      headers: authHeader()
    });
    settings.value.notify_via_b24 = v;
  } catch (e: any) {
    msg.value = e?.data?.error || e?.message || "Не удалось сохранить";
  } finally {
    saving.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.account-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--sp-5);
}
@media (max-width: 768px) {
  .account-grid { grid-template-columns: 1fr; }
}
.account-info dl {
  display: grid;
  grid-template-columns: 140px 1fr;
  gap: var(--sp-4) var(--sp-6);
  margin: 0;
  font-size: var(--fs-base);
}
.account-info dt {
  font-size: var(--fs-xs);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-muted);
  font-weight: var(--fw-semibold);
  align-self: center;
}
.account-info dd {
  margin: 0;
  color: var(--text-strong);
}
.role-list { display: flex; gap: var(--sp-3); flex-wrap: wrap; }
.account-hint {
  margin: 0 0 var(--sp-5);
  color: var(--text-secondary);
  font-size: var(--fs-sm);
}
.switch-row {
  display: flex; align-items: flex-start; gap: var(--sp-3);
  font-size: var(--fs-sm);
  cursor: pointer;
}
.switch-row input { margin-top: 4px; }
.switch-row .hint {
  display: block;
  color: var(--text-muted);
  font-size: var(--fs-xs);
  font-weight: var(--fw-normal);
  margin-top: 2px;
}
</style>
