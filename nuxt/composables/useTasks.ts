/**
 * Движок заданий процесса. Задание = форма × ЦФО-срез + роль; исполнитель авто =
 * ТОП ЦФО (Должность) / Директор ЮЛ. Делегирование 2-ступ. (start → delegate →
 * submit → review → accept → done). Владелец этапа двигает этап.
 */

import type { MpConditions } from "./useMpCascade";

export interface CfoFilter {
  entity_type?: string;
  group_cfo1?: string;
  group_cfo2?: string;
  country?: string;
  legal_entity?: string;
}

export interface TaskTemplate {
  id: number;
  stage_code: string;
  form_code: string;
  title: string;
  cfo_filter: CfoFilter;
  task_role: string;
  group_by: string;
  sort_order: number;
}

export interface Task {
  id: number;
  pl_id: number;
  stage_code: string;
  form_code: string;
  title: string;
  cfo_codes: number[];
  cfo_count: number;
  task_role: string;
  position_id: number | null;
  legal_entity: string;
  assignee_user_id: number | null;
  assignee_name: string;
  delegate_user_id: number | null;
  delegate_name: string;
  // Кто передал задание, когда, с каким пояснением и сроком. Без этих полей в
  // списке виден только конечный держатель — вопрос «почему задание у него»
  // остаётся без ответа.
  delegated_by?: number | null;
  delegated_by_name?: string;
  delegated_at?: string | null;
  delegate_note?: string;
  due_at?: string | null;
  status: string;
}

export interface TaskDataRow {
  code_cfo: number;
  name_cfo: string;
  line_code: number;
  expense_name: string;
  block_type: string;
  fact: number | null;
  strategy: number | null;
  tactic: number | null;
  calc: number | null;
  is_manual: boolean;
  reason: string;
  original: number | null;
}
/** Запись истории задания (pl_task_event): одно действие с исполнителями. */
export interface TaskEvent {
  id: number;
  action: string;
  actor_id?: number | null;
  actor_name: string;
  target_id?: number | null;
  target_name?: string;
  status_from: string;
  status_to: string;
  comment: string;
  due_at?: string | null;
  created_at: string;
}

/** Человеческие подписи действий: списки заданий читают исполнители. */
export const TASK_ACTION_LABEL: Record<string, string> = {
  start: "взял(а) в работу",
  delegate: "передал(а) в работу",
  submit: "сдал(а)",
  accept: "принял(а)",
  return: "вернул(а) на доработку",
  reopen: "переоткрыл(а)",
  assign: "назначил(а) исполнителя"
};

export interface TaskData {
  task: Task;
  rows: TaskDataRow[];
  has_data: boolean;
}

export interface MpFormPlatform {
  code_cfo: number; name: string; country: string;
  segment: string; legal_entity: string;
}
export interface MpLine {
  block_type: string; code_pl: number; name: string; section: string;
  kind: "input" | "calc" | "calc_editable" | "header";
  value_kind: "money" | "pct"; scope: "platform" | "total";
  formula: string; editable: boolean; cost_line: boolean;
}
export interface MpFormCell {
  code_cfo: number; block_type: string; value: number;
  fact: number | null; fact_prev: number | null; strategy: number | null;
  target: number | null; tactic: number | null;
  is_manual: boolean; reason: string;
}
export interface MpTaskForm {
  task: Task;
  segment: string;
  year: number;
  month: number;
  currency: string;
  vat: number;
  platforms: MpFormPlatform[];
  lines: MpLine[];
  cells: MpFormCell[];
  /**
   * Направление расчёта карточки: legacy («суммы → доли») либо inverse
   * («условия → суммы», ТЗ МП §3.1). Клиент обязан его учитывать: в inverse
   * живой пересчёт идёт из условий площадки, а не из введённых сумм.
   */
  calc_mode: string;
  /** Карточка процесса этой формы; 0/undefined — старый период без карточки. */
  card_id?: number;
  /**
   * Условия площадок (приходят только в inverse): ключ — code_cfo. Нужны и для
   * пересчёта, и чтобы показать долю статьи рядом с суммой.
   */
  conditions?: Record<number, MpConditions>;
}
export interface MpSaveRow { code_cfo: number; block_type: string; code_pl: number; amount: number; is_manual: boolean; comment: string }

export interface PnlRow {
  code_cfo: number; name_cfo: string; line_code: number; expense_name: string; block_type: string;
  fact: number | null; strategy: number | null; tactic: number | null; calc: number | null;
  is_manual: boolean; prev_tactic: number | null;
}
export interface PnlSummary { year: number; month: number; prev_year: number; prev_month: number; rows: PnlRow[] }

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export const useTasks = () => {
  const base = useRuntimeConfig().public.apiBase;
  const h = () => authHeader();

  const mine = (): Promise<Task[]> => $fetch<Task[]>(`${base}/api/plans/tasks/mine`, { headers: h() });

  const all = (): Promise<Task[]> => $fetch<Task[]>(`${base}/api/plans/tasks/all`, { headers: h() });

  const pnl = (plId: number): Promise<PnlSummary> => $fetch<PnlSummary>(`${base}/api/plans/instances/${plId}/pnl`, { headers: h() });
  const importStrategy = async (plId: number, file: File | Blob): Promise<{ imported: number }> => {
    const res = await fetch(`${base}/api/plans/instances/${plId}/strategy/import`, { method: "POST", headers: h(), body: file });
    const d = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error((d as { error?: string }).error || `Импорт: ${res.status}`);
    return d as { imported: number };
  };

  const data = (taskId: number): Promise<TaskData> => $fetch<TaskData>(`${base}/api/plans/tasks/${taskId}/data`, { headers: h() });

  // currency — валюта ОТОБРАЖЕНИЯ (хранение всегда RUB): сервер пересчитывает
  // денежные строки туда и обратно, проценты не трогает.
  const mpForm = (taskId: number, currency = "RUB"): Promise<MpTaskForm> =>
    $fetch<MpTaskForm>(`${base}/api/plans/tasks/${taskId}/mp-form`, { params: { currency }, headers: h() });
  const saveMpForm = (taskId: number, rows: MpSaveRow[], currency = "RUB"): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/tasks/${taskId}/mp-form`, { method: "PUT", body: { rows, currency }, headers: h() });

  const exportMpForm = async (taskId: number): Promise<Blob> => {
    const res = await fetch(`${base}/api/plans/tasks/${taskId}/mp-form/export`, { headers: h() });
    if (!res.ok) throw new Error(`Экспорт: ${res.status}`);
    return res.blob();
  };
  const importMpForm = async (taskId: number, file: File | Blob): Promise<{ imported: number }> => {
    const res = await fetch(`${base}/api/plans/tasks/${taskId}/mp-form/import`, { method: "POST", headers: h(), body: file });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error((data as { error?: string }).error || `Импорт: ${res.status}`);
    return data as { imported: number };
  };

  const byInstance = (plId: number): Promise<Task[]> =>
    $fetch<Task[]>(`${base}/api/plans/instances/${plId}/tasks`, { headers: h() });

  const generate = (plId: number): Promise<{ generated: number }> =>
    $fetch(`${base}/api/plans/instances/${plId}/tasks/generate`, { method: "POST", headers: h() });

  const setAssignee = (taskId: number, userId: number): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/tasks/${taskId}/assignee`, { method: "PUT", body: { user_id: userId }, headers: h() });

  /**
   * Действие над заданием. Комментарий обязателен при `delegate` и `return` —
   * сервер отклонит запрос без него: передача работы без объяснения теряет
   * контекст, ради которого процесс и переносили из Excel.
   */
  const action = (
    taskId: number,
    act: string,
    opts: { delegateUserId?: number; comment?: string; dueAt?: string } = {}
  ): Promise<{ ok: boolean; events?: TaskEvent[] }> =>
    $fetch(`${base}/api/plans/tasks/${taskId}/action`, {
      method: "POST",
      body: {
        action: act,
        delegate_user_id: opts.delegateUserId ?? 0,
        comment: opts.comment ?? "",
        due_at: opts.dueAt ?? ""
      },
      headers: h()
    });

  /** История задания: кто взял, кто кому передал, с каким пояснением и сроком. */
  const taskEvents = (taskId: number): Promise<TaskEvent[]> =>
    $fetch(`${base}/api/plans/tasks/${taskId}/events`, { headers: h() });

  const setOwner = (plId: number, stageCode: string, userId: number): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/instances/${plId}/stages/${stageCode}/owner`, {
      method: "PUT",
      body: { user_id: userId },
      headers: h()
    });

  const readiness = (plId: number, stageCode: string): Promise<{ ready: boolean; done: number; total: number }> =>
    $fetch(`${base}/api/plans/instances/${plId}/stages/${stageCode}/readiness`, { headers: h() });

  const stageOwners = (plId: number): Promise<{ stage_code: string; owner_user_id: number | null; owner_name: string }[]> =>
    $fetch(`${base}/api/plans/instances/${plId}/stage-owners`, { headers: h() });

  const advanceStage = (plId: number, code: string, body: { action: string; year: number; month: number; country: string }): Promise<unknown> =>
    $fetch(`${base}/api/plans/instances/${plId}/stages/${code}/action`, { method: "POST", body, headers: h() });

  const templates = (): Promise<TaskTemplate[]> => $fetch<TaskTemplate[]>(`${base}/api/plans/task-templates`, { headers: h() });
  const saveTemplate = (t: Partial<TaskTemplate>): Promise<{ id: number }> =>
    $fetch(`${base}/api/plans/task-templates`, { method: "PUT", body: t, headers: h() });
  const deleteTemplate = (id: number): Promise<{ ok: boolean }> =>
    $fetch(`${base}/api/plans/task-templates/${id}`, { method: "DELETE", headers: h() });

  return { mine, all, pnl, importStrategy, data, mpForm, saveMpForm, exportMpForm, importMpForm, byInstance, generate, action, taskEvents, setAssignee, setOwner, readiness, stageOwners, advanceStage, templates, saveTemplate, deleteTemplate };
};
