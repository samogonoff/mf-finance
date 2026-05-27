/**
 * Уведомления Finance (один общий поток).
 * Эталон: mp/nuxt/composables/useNotifications.ts (adaptive polling).
 */

export type NotificationType = "info" | "success" | "warning" | "error";

export interface Notification {
  id: number;
  user_id: number;
  title: string;
  message: string;
  type: NotificationType;
  is_read: boolean;
  created_at: string;
  read_at?: string;
  data: Record<string, any>;
  object_type?: string;
}

const POLL_NORMAL_MS = 30_000;
const POLL_FOCUSED_MS = 10_000;

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const useNotifications = () => {
  const config = useRuntimeConfig();
  const base = config.public.apiBase;

  const items = useState<Notification[]>("notifications-items", () => []);
  const unreadCount = useState<number>("notifications-unread", () => 0);
  const loading = useState<boolean>("notifications-loading", () => false);
  const error = useState<string | null>("notifications-error", () => null);
  const drawerOpen = useState<boolean>("notifications-drawer", () => false);

  let pollTimer: ReturnType<typeof setTimeout> | null = null;
  let pollActive = false;

  const fetchUnreadCount = async () => {
    try {
      const r = await $fetch<{ count: number }>(
        `${base}/api/notifications/unread-count`,
        { headers: authHeader() }
      );
      unreadCount.value = r.count;
      error.value = null;
    } catch (e: any) {
      if (e?.status === 401 || e?.statusCode === 401) {
        stopPolling();
        return;
      }
      error.value = e?.data?.error || e?.message || "Ошибка";
    }
  };

  const fetchList = async (opts: { unreadOnly?: boolean; limit?: number; offset?: number } = {}) => {
    loading.value = true;
    try {
      const params: Record<string, string> = {};
      if (opts.unreadOnly) params.unread_only = "1";
      if (opts.limit != null) params.limit = String(opts.limit);
      if (opts.offset != null) params.offset = String(opts.offset);
      const r = await $fetch<{ data: Notification[]; total: number }>(
        `${base}/api/notifications`,
        { params, headers: authHeader() }
      );
      items.value = r.data;
      error.value = null;
    } catch (e: any) {
      if (e?.status === 401 || e?.statusCode === 401) {
        stopPolling();
        return;
      }
      error.value = e?.data?.error || e?.message || "Ошибка";
    } finally {
      loading.value = false;
    }
  };

  const markAsRead = async (id: number) => {
    try {
      await $fetch(`${base}/api/notifications/${id}/read`, {
        method: "POST",
        headers: authHeader()
      });
      const it = items.value.find((n) => n.id === id);
      if (it && !it.is_read) {
        it.is_read = true;
        it.read_at = new Date().toISOString();
        unreadCount.value = Math.max(0, unreadCount.value - 1);
      }
    } catch (e: any) {
      error.value = e?.data?.error || e?.message || "Ошибка";
    }
  };

  const markAllAsRead = async () => {
    try {
      await $fetch(`${base}/api/notifications/read-all`, {
        method: "POST",
        headers: authHeader()
      });
      items.value.forEach((n) => {
        if (!n.is_read) {
          n.is_read = true;
          n.read_at = new Date().toISOString();
        }
      });
      unreadCount.value = 0;
    } catch (e: any) {
      error.value = e?.data?.error || e?.message || "Ошибка";
    }
  };

  const pollOnce = async () => {
    await fetchUnreadCount();
    if (drawerOpen.value) {
      await fetchList();
    }
  };

  const scheduleNext = () => {
    if (!pollActive) return;
    const delay = drawerOpen.value ? POLL_FOCUSED_MS : POLL_NORMAL_MS;
    pollTimer = setTimeout(async () => {
      await pollOnce();
      scheduleNext();
    }, delay);
  };

  const startPolling = async () => {
    if (pollActive) return;
    pollActive = true;
    await pollOnce();
    scheduleNext();
  };

  const stopPolling = () => {
    pollActive = false;
    if (pollTimer) {
      clearTimeout(pollTimer);
      pollTimer = null;
    }
  };

  const openDrawer = async () => {
    drawerOpen.value = true;
    await fetchList();
  };
  const closeDrawer = () => {
    drawerOpen.value = false;
  };

  return {
    items,
    unreadCount,
    loading,
    error,
    drawerOpen,
    fetchUnreadCount,
    fetchList,
    markAsRead,
    markAllAsRead,
    startPolling,
    stopPolling,
    openDrawer,
    closeDrawer
  };
};
