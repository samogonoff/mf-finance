/**
 * Статус реплики FinDWH → ClickHouse (GET /api/admin/etl/findwh, только ROLE_ADMIN).
 * Контракт — go/internal/etl/findwh_status.go.
 */

export type EtlSeverity = "ok" | "info" | "warn" | "error";

export interface EtlRun {
  table: string;
  source_date: string;
  started_at: string;
  finished_at: string;
  duration_sec: number;
  status: "ok" | "error";
  rows: number;
  details: string;
  triggered_by: string;
}

export interface EtlTableStatus {
  table: string;
  title: string;
  source: string;
  target: string;
  method: string;
  ready_rule: string;
  state: string;
  severity: EtlSeverity;
  state_text: string;
  source_ready?: boolean;
  source_ready_error?: string;
  loaded_for?: string;
  lag_days?: number;
  last_run?: EtlRun;
  last_ok?: EtlRun;
  typical_duration_sec?: number;
  data: {
    rows?: number;
    from?: string;
    to?: string;
    loaded_at?: string;
    error?: string;
    fp_rows?: number;
    days?: number;
    companies?: number;
  };
}

export interface EtlWorkerState {
  enabled: boolean;
  disabled_reason?: string;
  interval_sec: number;
  tick_started_at?: string; // тик идёт сейчас
  last_tick_end_at?: string;
  last_tick_error?: string;
  next_tick_at?: string;
  running?: string;
  running_since?: string;
  progress?: string;
}

export interface EtlStatusReport {
  checked_at: string;
  overall: EtlSeverity;
  settings: { env: string; value: string; hint: string; error?: string }[];
  worker: EtlWorkerState;
  source: { checked: boolean; today: string; hour: number; error?: string };
  ch_error?: string;
  tables: EtlTableStatus[];
  runs: EtlRun[];
}

export const ETL_SEVERITY_BADGE: Record<EtlSeverity, string> = {
  ok: "badge-pos", info: "badge-info", warn: "badge-warn", error: "badge-neg"
};

export const ETL_STATE_LABELS: Record<string, string> = {
  ok: "Загружено", running: "Загружается", waiting: "Ждём DWH", queued: "В очереди",
  late: "DWH опаздывает", error: "Ошибка", stale: "Устарело", off: "Воркер выключен",
  source_unknown: "Источник не проверен", ch_down: "ClickHouse недоступен", never: "Не загружалась"
};

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const useEtlStatus = () => {
  const config = useRuntimeConfig();
  const base = config.public.apiBase;

  const findwh = () =>
    $fetch<EtlStatusReport>(`${base}/api/admin/etl/findwh`, { headers: authHeader() });

  return { findwh };
};
