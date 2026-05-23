<template>
  <div class="notif-root">
    <button class="notif-bell" :class="{ 'has-unread': unreadCount > 0 }"
            type="button" @click="toggle" :aria-label="`Уведомлений непрочитанных: ${unreadCount}`">
      <Icon name="lucide:bell" />
      <span v-if="unreadCount > 0" class="notif-badge">{{ unreadCount > 99 ? "99+" : unreadCount }}</span>
    </button>

    <Teleport to="body">
      <div v-if="drawerOpen" class="notif-overlay" @click="close" />
      <aside v-if="drawerOpen" class="notif-drawer" role="dialog" aria-label="Уведомления">
        <header class="notif-drawer-head">
          <h2>Уведомления</h2>
          <button class="btn btn-ghost btn-sm" @click="close" aria-label="Закрыть">
            <Icon name="lucide:x" />
          </button>
        </header>

        <div class="notif-toolbar">
          <label class="notif-check">
            <input type="checkbox" v-model="unreadOnly" @change="reload" />
            <span>Только непрочитанные</span>
          </label>
          <button
            class="btn btn-ghost btn-sm"
            :disabled="unreadCount === 0"
            @click="markAllAsRead"
          >
            <Icon name="lucide:check-check" /> Прочитать всё
          </button>
        </div>

        <div v-if="error" class="notif-error">{{ error }}</div>
        <div v-if="loading && items.length === 0" class="notif-empty">Загрузка…</div>
        <div v-else-if="items.length === 0" class="notif-empty">Уведомлений нет</div>

        <ul class="notif-list">
          <li
            v-for="n in items"
            :key="n.id"
            class="notif-item"
            :class="[`type-${n.type}`, { unread: !n.is_read }]"
            @click="onClick(n)"
          >
            <div class="notif-icon">
              <Icon :name="iconFor(n.type)" />
            </div>
            <div class="notif-body">
              <div class="notif-title">
                <span>{{ n.title }}</span>
                <span v-if="!n.is_read" class="notif-dot" />
              </div>
              <div v-if="n.message" class="notif-message">{{ n.message }}</div>
              <div class="notif-time">{{ formatTime(n.created_at) }}</div>
            </div>
          </li>
        </ul>
      </aside>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from "vue";
import { useNotifications, type Notification, type NotificationType } from "~/composables/useNotifications";

const {
  items, unreadCount, loading, error, drawerOpen,
  fetchList, markAsRead, markAllAsRead,
  startPolling, stopPolling, openDrawer, closeDrawer
} = useNotifications();

const unreadOnly = ref(false);

const reload = () => fetchList({ unreadOnly: unreadOnly.value });

const toggle = async () => {
  if (drawerOpen.value) {
    closeDrawer();
  } else {
    await openDrawer();
    await reload();
  }
};
const close = () => closeDrawer();

const onClick = async (n: Notification) => {
  if (!n.is_read) await markAsRead(n.id);
  const url = (n.data && typeof n.data.url === "string") ? n.data.url : null;
  if (!url) return;
  if (/^https?:\/\//i.test(url)) {
    window.open(url, "_blank", "noopener");
  } else {
    closeDrawer();
    await navigateTo(url);
  }
};

const iconFor = (t: NotificationType): string => {
  switch (t) {
    case "success": return "lucide:check-circle";
    case "warning": return "lucide:alert-triangle";
    case "error":   return "lucide:alert-octagon";
    default:        return "lucide:info";
  }
};

const formatTime = (iso: string): string => {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const diff = (Date.now() - d.getTime()) / 1000;
  if (diff < 60) return "только что";
  if (diff < 3600) return `${Math.floor(diff / 60)} мин назад`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} ч назад`;
  return d.toLocaleDateString("ru-RU") + " " + d.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
};

// Esc — закрытие.
const onKey = (e: KeyboardEvent) => {
  if (e.key === "Escape" && drawerOpen.value) close();
};

onMounted(() => {
  startPolling();
  document.addEventListener("keydown", onKey);
});
onBeforeUnmount(() => {
  stopPolling();
  document.removeEventListener("keydown", onKey);
});
</script>

<style scoped>
.notif-root { position: relative; display: inline-flex; }

.notif-bell {
  position: relative;
  width: 32px; height: 32px;
  display: inline-flex; align-items: center; justify-content: center;
  background: transparent;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  color: var(--text-secondary);
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.notif-bell:hover {
  background: var(--bg-hover);
  color: var(--text-strong);
}
.notif-bell.has-unread { color: var(--accent); border-color: var(--accent); }

.notif-badge {
  position: absolute;
  top: -4px; right: -4px;
  min-width: 16px; height: 16px;
  padding: 0 4px;
  background: var(--neg-strong);
  color: white;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: var(--fw-semibold);
  line-height: 16px;
  border-radius: 8px;
  text-align: center;
}

.notif-overlay {
  position: fixed; inset: 0;
  background: rgba(0, 0, 0, 0.32);
  z-index: 1100;
}
.notif-drawer {
  position: fixed; top: 0; right: 0; bottom: 0;
  width: 420px; max-width: 100vw;
  background: var(--bg-surface);
  border-left: 1px solid var(--border);
  z-index: 1101;
  display: flex; flex-direction: column;
  box-shadow: -2px 0 12px rgba(0, 0, 0, 0.08);
}
.notif-drawer-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: var(--sp-4) var(--sp-5);
  border-bottom: 1px solid var(--border);
}
.notif-drawer-head h2 { margin: 0; font-size: var(--fs-md); }

.notif-toolbar {
  display: flex; align-items: center; gap: var(--sp-4);
  padding: var(--sp-3) var(--sp-5);
  border-bottom: 1px solid var(--border);
  font-size: var(--fs-sm);
}
.notif-check { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
.notif-check input { margin: 0; }

.notif-error {
  padding: var(--sp-3) var(--sp-5);
  color: var(--neg-strong); background: var(--neg-soft);
  font-size: var(--fs-sm);
}
.notif-empty {
  padding: var(--sp-6) var(--sp-5);
  text-align: center; color: var(--text-muted); font-size: var(--fs-sm);
}

.notif-list { list-style: none; margin: 0; padding: 0; overflow-y: auto; flex: 1; }
.notif-item {
  display: flex; gap: var(--sp-3);
  padding: var(--sp-4) var(--sp-5);
  border-bottom: 1px solid var(--border);
  cursor: pointer;
  transition: background 0.12s;
}
.notif-item:hover { background: var(--bg-hover); }
.notif-item.unread { background: var(--bg-subtle); }

.notif-icon {
  flex: 0 0 auto;
  width: 28px; height: 28px;
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: var(--rd-3);
  color: var(--text-muted);
}
.notif-item.type-success .notif-icon { color: var(--pos-strong); }
.notif-item.type-warning .notif-icon { color: var(--warn-strong); }
.notif-item.type-error .notif-icon { color: var(--neg-strong); }
.notif-item.type-info .notif-icon { color: var(--accent); }

.notif-body { flex: 1; min-width: 0; }
.notif-title {
  display: flex; align-items: center; gap: 6px;
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
  font-size: var(--fs-sm);
}
.notif-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--accent); }
.notif-message {
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  margin-top: 2px;
  word-break: break-word;
}
.notif-time {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  margin-top: 4px;
}
</style>
