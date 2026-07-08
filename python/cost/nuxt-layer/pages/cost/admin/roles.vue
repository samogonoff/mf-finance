<template>
  <div class="page-cost">
    <header class="page-header">
      <div>
        <h1 class="page-title">Управление ролями себестоимости</h1>
        <p class="page-subtitle muted">Администрирование доступов к разделу «Себестоимость»</p>
      </div>
      <div class="page-header-actions">
        <button class="btn btn-ghost btn-sm" @click="navigateTo('/cost')">
          <Icon name="lucide:arrow-left" /> Назад
        </button>
      </div>
    </header>

    <!-- Access denied -->
    <div v-if="permLoading" class="muted" style="text-align:center;padding:24px">
      <Icon name="lucide:loader" class="spinning" /> Проверка прав доступа…
    </div>
    <div v-else-if="!can('cost:admin')" class="cost-error">
      <Icon name="lucide:alert-triangle" />
      <div>
        <strong>Нет доступа</strong>
        <p>Для управления ролями требуется право <code>cost:admin</code>.</p>
      </div>
    </div>

    <!-- Main content -->
    <template v-else>
      <!-- Error banner -->
      <div v-if="lastError" class="cost-error">
        <Icon name="lucide:alert-triangle" />
        <div>
          <strong>Ошибка</strong>
          <p>{{ lastError }}</p>
        </div>
        <button class="cost-error-x" @click="lastError = ''" aria-label="Закрыть">×</button>
      </div>

      <!-- Actions -->
      <div class="cost-actions" style="justify-content:flex-start">
        <button class="btn btn-primary btn-sm" @click="openCreateModal">
          <Icon name="lucide:plus" /> Добавить роль
        </button>
      </div>

      <!-- Loading -->
      <div v-if="loading" class="muted" style="text-align:center;padding:24px">
        <Icon name="lucide:loader" class="spinning" /> Загрузка ролей…
      </div>

      <!-- Empty state -->
      <div v-else-if="!roles.length" class="card">
        <div class="card-body" style="text-align:center;padding:var(--sp-5)">
          <p class="muted">Роли не настроены. Нажмите «Добавить роль», чтобы создать первую роль.</p>
        </div>
      </div>

      <!-- Role cards -->
      <div v-else class="roles-list">
        <div v-for="role in roles" :key="role.id" class="card role-card">
          <div class="card-header">
            <div>
              <div class="card-title">
                {{ role.name }}
                <span v-if="role.is_system" class="system-badge">Системная</span>
              </div>
              <div class="card-subtitle">
                <span v-for="perm in role.permissions" :key="perm" class="perm-tag">
                  {{ permissionLabels[perm] || perm }}
                </span>
                <span v-if="!role.permissions.length" class="muted">Нет прав</span>
              </div>
            </div>
            <div class="card-actions">
              <button class="btn btn-ghost btn-xs" @click="openEditModal(role)">
                <Icon name="lucide:pencil" /> Изменить
              </button>
              <button
                class="btn btn-ghost btn-xs btn-danger"
                :disabled="role.is_system || deletingId === role.id"
                @click="deleteRole(role.id)"
              >
                <Icon name="lucide:trash-2" />
                {{ deletingId === role.id ? 'Удаление…' : 'Удалить' }}
              </button>
            </div>
          </div>

          <!-- User assignments -->
          <div class="card-body role-users">
            <div class="role-users-header">
              <strong>Пользователи</strong>
              <span class="muted">{{ getRoleUsers(role.id).length }} чел.</span>
            </div>
            <div v-if="getRoleUsers(role.id).length" class="user-tags">
              <span v-for="u in getRoleUsers(role.id)" :key="u.id" class="user-tag">
                {{ u.email }}
                <button
                  class="user-tag-remove"
                  :disabled="removingUserId === u.id"
                  @click="removeUser(u.id)"
                  title="Удалить"
                >
                  {{ removingUserId === u.id ? '…' : '×' }}
                </button>
              </span>
            </div>
            <div v-else class="muted" style="font-size:var(--fs-xs)">Нет назначенных пользователей</div>

            <div class="assign-user-row">
              <input
                v-model="newUserEmails[role.id]"
                type="email"
                placeholder="email@company.ru"
                class="form-input"
                style="flex:1"
                @keydown.enter="assignUser(role.id)"
              />
              <button
                class="btn btn-primary btn-sm"
                :disabled="!newUserEmails[role.id]?.trim() || assigningRoleId === role.id"
                @click="assignUser(role.id)"
              >
                {{ assigningRoleId === role.id ? 'Назначение…' : 'Назначить' }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </template>

    <!-- Create/Edit Modal -->
    <Teleport to="body">
      <div v-if="showModal" class="modal-overlay" @click.self="closeModal">
        <div class="modal-content role-modal" @click.stop>
          <div class="modal-header">
            <h2>{{ editingRole ? 'Изменить роль' : 'Новая роль' }}</h2>
            <button class="modal-close" @click="closeModal">×</button>
          </div>
          <div class="role-modal-body">
            <label class="role-field">
              <span>Название роли</span>
              <input v-model="modalName" type="text" class="form-input" placeholder="Например, Менеджер по ценам" />
            </label>

            <div class="role-perms">
              <div class="role-perms-title">Права доступа</div>
              <label v-for="(label, perm) in permissionLabels" :key="perm" class="colvis-item">
                <input type="checkbox" :value="perm" v-model="modalPermissions" />
                <span>{{ label }}</span>
              </label>
            </div>
          </div>
          <div class="role-modal-footer">
            <button class="btn btn-ghost btn-sm" @click="closeModal">Отмена</button>
            <button
              class="btn btn-primary btn-sm"
              :disabled="!modalName.trim() || modalSaving"
              @click="saveRole"
            >
              {{ modalSaving ? 'Сохранение…' : 'Сохранить' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from "vue";
import { useCostPermission } from "~/composables/useCostPermission";

interface Role {
  id: number;
  name: string;
  permissions: string[];
  is_system: boolean;
}

interface UserAssignment {
  id: number;
  email: string;
  role_id: number;
}

const { can, loading: permLoading } = useCostPermission();

const config = useRuntimeConfig();
const apiBase = computed(() =>
  config.public.costOnly ? "" : ((config.public.apiBase as string) || "")
);

const user = useState<any>("auth-user");
const email = computed(() => user.value?.email || "");

const fetchHeaders = computed(() => ({
  "X-Cost-User": email.value,
}));

const loading = ref(false);
const lastError = ref("");
const roles = ref<Role[]>([]);
const userAssignments = ref<UserAssignment[]>([]);

const showModal = ref(false);
const editingRole = ref<Role | null>(null);
const modalName = ref("");
const modalPermissions = ref<string[]>([]);
const modalSaving = ref(false);

const deletingId = ref<number | null>(null);
const assigningRoleId = ref<number | null>(null);
const removingUserId = ref<number | null>(null);
const newUserEmails = reactive<Record<number, string>>({});

const permissionLabels: Record<string, string> = {
  "cost:view": "Просмотр данных",
  "cost:edit_price": "Редактирование цен",
  "cost:approve": "Согласование",
  "cost:peo_mark": "Отметки ПЭО",
  "cost:edit_materials": "Редактирование материалов",
  "cost:export": "Экспорт в Excel",
  "cost:admin": "Администрирование",
};

const ALL_PERMISSIONS = Object.keys(permissionLabels);

function getRoleUsers(roleId: number): UserAssignment[] {
  return userAssignments.value.filter((u) => u.role_id === roleId);
}

async function loadRoles() {
  loading.value = true;
  lastError.value = "";
  try {
    const [rolesRes, usersRes, permsRes] = await Promise.all([
      $fetch<{ roles: Role[] }>(`${apiBase.value}/api/cost/roles`, {
        headers: fetchHeaders.value,
      }).catch(() => ({ roles: [] as Role[] })),
      $fetch<{ assignments: UserAssignment[] }>(`${apiBase.value}/api/cost/roles/users`, {
        headers: fetchHeaders.value,
      }).catch(() => ({ assignments: [] as UserAssignment[] })),
      $fetch<{ permissions: Record<string, string> }>(`${apiBase.value}/api/cost/permissions`, {
        headers: fetchHeaders.value,
      }).catch(() => ({ permissions: {} as Record<string, string> })),
    ]);
    roles.value = rolesRes.roles || [];
    userAssignments.value = usersRes.assignments || [];
    // Merge server-provided labels with local fallback
    if (permsRes.permissions) {
      for (const [k, v] of Object.entries(permsRes.permissions)) {
        if (v) permissionLabels[k] = v;
      }
    }
  } catch (e: any) {
    lastError.value = e?.data?.detail || e?.message || String(e);
    console.error("[cost] load roles failed", e);
  } finally {
    loading.value = false;
  }
}

function openCreateModal() {
  editingRole.value = null;
  modalName.value = "";
  modalPermissions.value = [];
  showModal.value = true;
}

function openEditModal(role: Role) {
  editingRole.value = role;
  modalName.value = role.name;
  modalPermissions.value = [...role.permissions];
  showModal.value = true;
}

function closeModal() {
  showModal.value = false;
  editingRole.value = null;
}

async function saveRole() {
  modalSaving.value = true;
  lastError.value = "";
  try {
    const body = {
      name: modalName.value.trim(),
      permissions: modalPermissions.value,
    };
    if (editingRole.value) {
      await $fetch(`${apiBase.value}/api/cost/roles/${editingRole.value.id}`, {
        method: "PUT",
        body,
        headers: fetchHeaders.value,
      });
    } else {
      await $fetch(`${apiBase.value}/api/cost/roles`, {
        method: "POST",
        body,
        headers: fetchHeaders.value,
      });
    }
    closeModal();
    await loadRoles();
  } catch (e: any) {
    lastError.value = e?.data?.detail || e?.message || String(e);
    console.error("[cost] save role failed", e);
  } finally {
    modalSaving.value = false;
  }
}

async function deleteRole(id: number) {
  if (!confirm("Удалить роль? Все назначения пользователей этой роли будут отозваны.")) return;
  deletingId.value = id;
  lastError.value = "";
  try {
    await $fetch(`${apiBase.value}/api/cost/roles/${id}`, {
      method: "DELETE",
      headers: fetchHeaders.value,
    });
    await loadRoles();
  } catch (e: any) {
    lastError.value = e?.data?.detail || e?.message || String(e);
    console.error("[cost] delete role failed", e);
  } finally {
    deletingId.value = null;
  }
}

async function assignUser(roleId: number) {
  const emailRaw = newUserEmails[roleId]?.trim();
  if (!emailRaw) return;
  assigningRoleId.value = roleId;
  lastError.value = "";
  try {
    await $fetch(`${apiBase.value}/api/cost/roles/users`, {
      method: "POST",
      body: {
        email: emailRaw,
        role_id: roleId,
        granted_by: email.value,
      },
      headers: fetchHeaders.value,
    });
    newUserEmails[roleId] = "";
    await loadRoles();
  } catch (e: any) {
    lastError.value = e?.data?.detail || e?.message || String(e);
    console.error("[cost] assign user failed", e);
  } finally {
    assigningRoleId.value = null;
  }
}

async function removeUser(assignmentId: number) {
  removingUserId.value = assignmentId;
  lastError.value = "";
  try {
    await $fetch(`${apiBase.value}/api/cost/roles/users/${assignmentId}`, {
      method: "DELETE",
      headers: fetchHeaders.value,
    });
    await loadRoles();
  } catch (e: any) {
    lastError.value = e?.data?.detail || e?.message || String(e);
    console.error("[cost] remove user failed", e);
  } finally {
    removingUserId.value = null;
  }
}

// Ждём загрузки permissions, и только тогда грузим список ролей
watch(permLoading, (loading) => {
  if (!loading && can("cost:admin")) {
    loadRoles();
  }
});
</script>

<style scoped>
.page-cost { padding-top: var(--sp-2); }

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: var(--sp-4);
}
.page-header-actions {
  display: flex;
  gap: var(--sp-3);
}

.page-subtitle {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-3);
}

/* Actions bar */
.cost-actions {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  padding: var(--sp-2) var(--sp-4);
  background: var(--bg-surface-2, var(--bg-surface));
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  margin-bottom: var(--sp-3);
  flex-wrap: wrap;
}

/* Error banner */
.cost-error {
  display: flex;
  align-items: flex-start;
  gap: var(--sp-3);
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-3);
  background: color-mix(in srgb, var(--neg) 8%, var(--bg-surface));
  border: 1px solid color-mix(in srgb, var(--neg) 30%, transparent);
  color: var(--text-strong);
  margin-bottom: var(--sp-5);
  position: relative;
}
.cost-error :deep(svg) { color: var(--neg); flex-shrink: 0; margin-top: 2px; }
.cost-error p { margin-top: 4px; font-size: var(--fs-sm); }
.cost-error .muted { color: var(--text-muted); font-size: var(--fs-xs); margin-top: 6px; }
.cost-error code {
  background: var(--bg-surface-3);
  padding: 1px 5px;
  border-radius: var(--rd-1);
  font-family: var(--font-mono);
  font-size: 90%;
}
.cost-error-x {
  position: absolute;
  top: 6px;
  right: 6px;
  border: none;
  background: transparent;
  font-size: 18px;
  cursor: pointer;
  color: var(--text-muted);
  padding: 4px 8px;
  border-radius: var(--rd-1);
}
.cost-error-x:hover { color: var(--text-strong); background: var(--bg-surface-3); }

/* Roles list */
.roles-list {
  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
}

.role-card .card-title {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
}

.system-badge {
  display: inline-flex;
  align-items: center;
  font-size: var(--fs-2xs);
  padding: 2px 8px;
  border-radius: var(--rd-pill);
  background: color-mix(in srgb, var(--accent) 14%, var(--bg-surface));
  color: var(--accent);
  font-weight: var(--fw-semibold);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.perm-tag {
  display: inline-flex;
  align-items: center;
  font-size: var(--fs-2xs);
  padding: 2px 6px;
  border-radius: var(--rd-1);
  background: var(--bg-surface-3);
  border: 1px solid var(--border);
  color: var(--text-muted);
  margin-right: var(--sp-1);
  margin-bottom: var(--sp-1);
}

/* Role users */
.role-users {
  padding-top: var(--sp-3);
  border-top: 1px solid var(--border);
}
.role-users-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: var(--sp-2);
  font-size: var(--fs-sm);
}
.user-tags {
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-2);
  margin-bottom: var(--sp-3);
}
.user-tag {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-1);
  font-size: var(--fs-xs);
  padding: 2px 6px;
  border-radius: var(--rd-2);
  background: var(--bg-surface-3);
  border: 1px solid var(--border);
  color: var(--text-strong);
}
.user-tag-remove {
  border: none;
  background: transparent;
  cursor: pointer;
  color: var(--text-muted);
  font-size: 14px;
  line-height: 1;
  padding: 0 2px;
}
.user-tag-remove:hover { color: var(--neg); }

.assign-user-row {
  display: flex;
  gap: var(--sp-2);
  align-items: center;
}

/* Modal */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 10000;
  display: flex;
  justify-content: center;
  align-items: center;
}
.role-modal {
  background: var(--bg-surface);
  border-radius: var(--rd-3);
  max-width: 520px;
  width: 90vw;
  max-height: 90vh;
  display: flex;
  flex-direction: column;
}
.modal-header {
  padding: var(--sp-4) var(--sp-5);
  border-bottom: 1px solid var(--border);
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-shrink: 0;
}
.modal-header h2 { margin: 0; font-size: var(--fs-lg); }
.modal-close {
  border: none;
  background: transparent;
  font-size: 24px;
  cursor: pointer;
  color: var(--text-muted);
  padding: 0 4px;
}
.modal-close:hover { color: var(--text-strong); }

.role-modal-body {
  padding: var(--sp-4) var(--sp-5);
  overflow-y: auto;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
}
.role-field {
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
  font-size: var(--fs-sm);
  color: var(--text-muted);
}
.role-field input {
  height: 36px;
  padding: 0 var(--sp-3);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  background: var(--bg-surface);
  color: var(--text-strong);
  font-size: var(--fs-sm);
}
.role-perms {
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
}
.role-perms-title {
  font-size: var(--fs-xs);
  font-weight: var(--fw-semibold, 600);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.role-modal-footer {
  padding: var(--sp-3) var(--sp-5);
  border-top: 1px solid var(--border);
  display: flex;
  justify-content: flex-end;
  gap: var(--sp-3);
  flex-shrink: 0;
}

/* Checkbox item (same as colvis-item) */
.colvis-item {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  font-size: var(--fs-sm);
  cursor: pointer;
  padding: 2px 0;
}
.colvis-item input[type="checkbox"] {
  width: 16px;
  height: 16px;
  cursor: pointer;
}

/* Form input */
.form-input {
  height: 36px;
  padding: 0 var(--sp-3);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  background: var(--bg-surface);
  color: var(--text-strong);
  font-size: var(--fs-sm);
}

/* Spinning loader icon */
.spinning { animation: cost-spin 0.9s linear infinite; }
@keyframes cost-spin {
  to { transform: rotate(360deg); }
}

/* Button danger variant */
.btn-danger {
  color: var(--neg);
}
.btn-danger:hover {
  background: color-mix(in srgb, var(--neg) 10%, transparent);
}
</style>
