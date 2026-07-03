/**
 * Клиент справочников модуля «Тактические планы» (ТЗ §«UI справочников»).
 *
 * Реестр справочников с типизированной схемой колонок (драйвит визуал таблицы),
 * чтение строк через Redis-кэш (бэк), ручная правка manual-НСИ, синхронизация
 * Лиса/1С, журнал синхронизаций, настройки кэша. Доступ — ROLE_PLANS_USER (чтение)
 * / ROLE_PLANS_ADMIN (мутации).
 */

export type DirColType =
  | "text" | "number" | "money" | "country" | "badge" | "bool" | "date" | "code";

export interface DirColumn {
  key: string;
  label: string;
  type: DirColType;
  primary?: boolean;
  filterable?: boolean;
  search?: boolean;
  width?: string;
  hint?: string;
  badge_tones?: Record<string, string>;
  ref?: string; // "user" → пикер; код справочника → селект его значений
  readonly?: boolean; // не редактируется (производное, напр. директор от ЮЛ)
}

export interface DirMeta {
  code: string;
  name: string;
  description: string;
  group: "manual" | "lisa" | "1c" | "calculated";
  icon: string;
  stage: string;
  source: string;
  editable: boolean;
  syncable: boolean;
  sync_status: "ok" | "stale" | "error" | "never" | "seed";
  synced_at: string | null;
  last_error: string;
  version: number;
  row_count: number;
  cache_ttl_seconds: number;
  stale_after_seconds: number;
  retry_count: number;
  last_sync_added: number;
  last_sync_changed: number;
  last_sync_removed: number;
  columns: DirColumn[];
  group_by?: string[];
}

export interface DirectoryRow {
  id: number;
  payload: Record<string, unknown>;
}

export interface SyncLogEntry {
  id: number;
  started_at: string;
  finished_at: string | null;
  status: string;
  triggered_by: string;
  rows_in: number;
  rows_added: number;
  rows_changed: number;
  rows_removed: number;
  duration_ms: number;
  error: string;
}

export interface DirVersion {
  id: number;
  version: number;
  changed_at: string;
  source: string;
  changed_by: number | null;
  by_name: string;
  added: number;
  changed: number;
  removed: number;
  summary: string;
}

export interface SyncResult {
  code: string;
  status: string;
  rows_in: number;
  added: number;
  changed: number;
  removed: number;
  duration_ms: number;
  error?: string;
}

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const useDirectories = () => {
  const base = useRuntimeConfig().public.apiBase;
  const h = () => authHeader();

  const list = (): Promise<DirMeta[]> =>
    $fetch<DirMeta[]>(`${base}/api/plans/dir`, { headers: h() });

  const rows = (code: string): Promise<DirectoryRow[]> =>
    $fetch<DirectoryRow[]>(`${base}/api/plans/dir/${code}/rows`, { headers: h() });

  const upsert = (code: string, id: number, payload: Record<string, unknown>): Promise<{ id: number }> =>
    $fetch(`${base}/api/plans/dir/${code}/rows`, { method: "PUT", body: { id, payload }, headers: h() });

  const remove = (code: string, id: number): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/dir/${code}/rows/${id}`, { method: "DELETE", headers: h() });

  const sync = (code: string): Promise<SyncResult> =>
    $fetch(`${base}/api/plans/dir/${code}/sync`, { method: "POST", headers: h() });

  const warm = (code: string): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/dir/${code}/warm`, { method: "POST", headers: h() });

  const log = (code: string): Promise<SyncLogEntry[]> =>
    $fetch<SyncLogEntry[]>(`${base}/api/plans/dir/${code}/log`, { headers: h() });

  const versions = (code: string): Promise<DirVersion[]> =>
    $fetch<DirVersion[]>(`${base}/api/plans/dir/${code}/versions`, { headers: h() });

  const saveSettings = (code: string, cacheTtl: number, staleAfter: number): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/dir/${code}/settings`, {
      method: "PUT",
      body: { cache_ttl_seconds: cacheTtl, stale_after_seconds: staleAfter },
      headers: h()
    });

  return { list, rows, upsert, remove, sync, warm, log, versions, saveSettings };
};
