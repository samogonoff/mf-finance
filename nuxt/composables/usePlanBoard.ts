/**
 * Свод периода — общий просмотр всех данных карточки для проверяющих и
 * согласующих. Те же строки, что в форме ввода (спека TPL-MP приходит с сервера),
 * но по всем площадкам и со всеми сценариями в колонках.
 * Бэкенд: GET /api/plans/instances/{id}/board (go/internal/plans/board.go).
 */
import type {MpLine} from "~/composables/useTasks";

export interface BoardValues {
  calc: number;
  tactic: number | null;
  fact: number | null;
  fact_prev: number | null;
  strategy: number | null;
  target: number | null;
}

export interface BoardChild {
  code_cfo: number;
  name: string;
  segment: string;
  country: string;
  legal_entity: string;
  values: BoardValues;
  is_manual: boolean;
  reason?: string;
  task_id?: number;
  task_status?: string;
  assignee?: string;
}

export interface BoardRow {
  line: MpLine;
  total: BoardValues;
  children: BoardChild[] | null;
}

export interface BoardPlatform {
  code_cfo: number;
  name: string;
  segment: string;
  country: string;
  legal_entity: string;
}

export interface BoardFilter {
  currency: string;
  segment: string;
  legal_entity: string;
  country: string;
  code_cfo: number[] | null;
}

export interface Board {
  pl_id: number;
  year: number;
  month: number;
  filter: BoardFilter;
  platforms: BoardPlatform[] | null;
  rows: BoardRow[] | null;
}

/** Колонки сценариев свода: набор переключается пресетом в шапке. */
export type BoardColumn = "fact" | "fact_prev" | "strategy" | "target" | "tactic" | "delta_strategy" | "delta_fact_prev";

export const BOARD_COLUMNS: { key: BoardColumn; label: string; hint: string }[] = [
  { key: "fact_prev", label: "Факт пр. года", hint: "тот же месяц прошлого года (Budgeting.FormToLoadFact)" },
  { key: "fact", label: "Факт", hint: "факт текущего периода" },
  { key: "strategy", label: "Стратегия", hint: "стратегический бюджет (FormToLoadPlan)" },
  { key: "target", label: "Таргет", hint: "тактика-таргет (FormToLoaTaktTarget)" },
  { key: "tactic", label: "Тактика", hint: "введено в форме; пусто — значение каскада" },
  { key: "delta_strategy", label: "Δ к стратегии", hint: "тактика − стратегия" },
  { key: "delta_fact_prev", label: "Δ к пр. году", hint: "тактика − факт прошлого года" }
];

/** Значение колонки: сценарий или производная дельта. Null — данных нет. */
export const boardCell = (v: BoardValues, col: BoardColumn): number | null => {
  const tac = v.tactic ?? v.calc;
  switch (col) {
    case "delta_strategy":
      return v.strategy == null ? null : tac - v.strategy;
    case "delta_fact_prev":
      return v.fact_prev == null ? null : tac - v.fact_prev;
    case "tactic":
      return tac;
    default:
      return v[col];
  }
};

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const usePlanBoard = () => {
  const base = useRuntimeConfig().public.apiBase;

  const board = (plId: number, f: Partial<BoardFilter> = {}): Promise<Board> =>
    $fetch<Board>(`${base}/api/plans/instances/${plId}/board`, {
      params: {
        currency: f.currency || "RUB",
        segment: f.segment || "",
        legal_entity: f.legal_entity || "",
        country: f.country || "",
        cfo: (f.code_cfo || []).join(",")
      },
      headers: authHeader()
    });

  return { board };
};
