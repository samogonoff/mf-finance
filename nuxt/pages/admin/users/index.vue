<template>
  <div class="page-admin-users">
    <header class="page-header">
      <div>
        <h1 class="page-title">Пользователи</h1>
        <p class="page-subtitle">
          <span>{{ total }} записей</span>
          <span v-if="loading" class="ctx-sep">·</span>
          <span v-if="loading">загрузка…</span>
        </p>
      </div>
    </header>

    <div class="filters-bar">
      <div class="search">
        <Icon name="lucide:search" class="search-icon" />
        <input v-model="qEmail" placeholder="Email…" @keyup.enter="reload" />
      </div>
      <div class="search">
        <Icon name="lucide:user" class="search-icon" />
        <input v-model="qName" placeholder="Имя / фамилия…" @keyup.enter="reload" />
      </div>
      <select v-model="qBlocked" class="select" style="width: 180px" @change="reload">
        <option :value="null">Все</option>
        <option :value="false">Активные</option>
        <option :value="true">Заблокированные</option>
      </select>
      <button class="btn btn-ghost" @click="reload">
        <Icon name="lucide:refresh-cw" /> Обновить
      </button>
    </div>

    <div v-if="error" class="error-banner">{{ error }}</div>

    <div class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th class="col-w-16">ID</th>
            <th>Email</th>
            <th>Имя</th>
            <th>Роли</th>
            <th>Статус</th>
            <th class="col-num">Создан</th>
            <th class="col-w-16"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in users" :key="u.id" :class="{ 'row-blocked': u.is_blocked }">
            <td class="num">{{ u.id }}</td>
            <td>{{ u.email }}</td>
            <td>{{ displayName(u) }}</td>
            <td>
              <div class="roles-row">
                <label v-for="r in allowedRoles" :key="r" class="role-chip">
                  <input
                    type="checkbox"
                    :checked="u.roles.includes(r)"
                    :disabled="saving === u.id"
                    @change="toggleRole(u, r, ($event.target as HTMLInputElement).checked)"
                  />
                  <span>{{ shortRole(r) }}</span>
                </label>
              </div>
            </td>
            <td>
              <span class="badge" :class="u.is_blocked ? 'badge-warn' : 'badge-pos'">
                <span v-if="!u.is_blocked" class="badge-dot"></span>
                {{ u.is_blocked ? "Заблокирован" : "Активен" }}
              </span>
            </td>
            <td class="num">{{ formatDate(u.created_at) }}</td>
            <td>
              <button
                v-if="!u.is_blocked"
                class="btn btn-sm btn-ghost"
                :disabled="u.id === selfId"
                :title="u.id === selfId ? 'Нельзя заблокировать себя' : 'Заблокировать'"
                @click="setBlocked(u, true)"
              >
                <Icon name="lucide:ban" /> Блок
              </button>
              <button v-else class="btn btn-sm btn-ghost" @click="setBlocked(u, false)">
                <Icon name="lucide:check" /> Разблок
              </button>
            </td>
          </tr>
          <tr v-if="!users.length && !loading">
            <td colspan="7" class="cell-empty">Пользователей нет</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="pagination">
      <span class="pagination-info">
        {{ offset + 1 }}–{{ Math.min(offset + users.length, total) }} из {{ total }}
      </span>
      <div class="pagination-controls">
        <button class="btn btn-sm btn-ghost" :disabled="offset === 0" @click="prev">
          <Icon name="lucide:chevron-left" /> Назад
        </button>
        <button class="btn btn-sm btn-ghost" :disabled="offset + limit >= total" @click="next">
          Вперёд <Icon name="lucide:chevron-right" />
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useAdminUsers, type AdminUser } from "~/composables/useAdminUsers";

definePageMeta({ middleware: "scope-guard" });

const api = useAdminUsers();
const { user: me } = useAuth();
const selfId = computed(() => me.value?.id ?? -1);

const users = ref<AdminUser[]>([]);
const allowedRoles = ref<string[]>([]);
const total = ref(0);
const limit = ref(50);
const offset = ref(0);
const loading = ref(false);
const error = ref<string | null>(null);
const saving = ref<number | null>(null);

const qEmail = ref("");
const qName = ref("");
const qBlocked = ref<boolean | null>(null);

const reload = async () => {
  loading.value = true;
  error.value = null;
  try {
    const res = await api.list({
      email: qEmail.value || undefined,
      name: qName.value || undefined,
      is_blocked: qBlocked.value,
      limit: limit.value,
      offset: offset.value
    });
    users.value = res.data;
    total.value = res.total;
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Ошибка загрузки";
  } finally {
    loading.value = false;
  }
};

const prev = () => {
  offset.value = Math.max(0, offset.value - limit.value);
  reload();
};
const next = () => {
  offset.value += limit.value;
  reload();
};

const toggleRole = async (u: AdminUser, role: string, on: boolean) => {
  const next = on ? [...u.roles, role] : u.roles.filter((r) => r !== role);
  saving.value = u.id;
  try {
    const updated = await api.updateRoles(u.id, next);
    Object.assign(u, updated);
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Ошибка сохранения ролей";
  } finally {
    saving.value = null;
  }
};

const setBlocked = async (u: AdminUser, blocked: boolean) => {
  try {
    if (blocked) await api.block(u.id);
    else await api.unblock(u.id);
    u.is_blocked = blocked;
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Ошибка";
  }
};

const displayName = (u: AdminUser) => {
  const parts = [u.name, u.last_name].filter(Boolean);
  return parts.length ? parts.join(" ") : "—";
};
const shortRole = (r: string) =>
  r.replace(/^ROLE_/, "").toLowerCase().replace(/_/g, " ");
const formatDate = (s: string) => {
  if (!s) return "";
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  return d.toLocaleDateString("ru-RU");
};

onMounted(async () => {
  allowedRoles.value = await api.allowedRoles();
  await reload();
});
</script>

<style scoped>
.filters-bar {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  padding: var(--sp-4) 0;
  margin-bottom: var(--sp-5);
  flex-wrap: wrap;
}
.filters-bar .search { max-width: 260px; flex: 0 1 260px; }

.roles-row { display: flex; gap: var(--sp-2); flex-wrap: wrap; }
.role-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 6px;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  font-size: var(--fs-xs);
  cursor: pointer;
  user-select: none;
}
.role-chip input { margin: 0; }

.row-blocked { opacity: 0.55; }

.cell-empty {
  text-align: center;
  color: var(--text-muted);
  padding: var(--sp-6);
}

.error-banner {
  background: var(--neg-soft);
  color: var(--neg-strong);
  padding: var(--sp-3) var(--sp-4);
  border-radius: var(--rd-3);
  margin-bottom: var(--sp-4);
  font-size: var(--fs-sm);
}

.pagination {
  display: flex;
  align-items: center;
  gap: var(--sp-5);
  padding: var(--sp-5) 0;
  font-size: var(--fs-sm);
}
.pagination-info { color: var(--text-muted); }
.pagination-controls { display: flex; gap: var(--sp-3); margin-left: auto; }
</style>
