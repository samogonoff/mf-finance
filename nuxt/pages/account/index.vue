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
definePageMeta({ middleware: "scope-guard" });
const { user, logout } = useAuth();
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
</style>
