// API реестра «Условия площадки», общих затрат МП, пересчёта расходной части и
// валидаций (ТЗ МП §6.1, §4.3, §7, §10). Отдельный composable, потому что это
// новая сущность формы, а не часть заданий: условия живут на карточке периода.

import type { MpConditions } from "./useMpCascade";

export interface MpConditionsRow extends MpConditions {
	valid_from: string;
  valid_to: string;
	period_year: number;
	period_month: number;
  id: number;
  name_cfo: string;
  segment: string;
  country: string;
  legal_entity: string;
  currency: string;
  version: number;
  change_reason: string;
  status: string;
  updated_at?: string;
  author_name?: string;
}

export interface MpConditionsDiff {
  code_cfo: number;
  name_cfo: string;
  field: string;
  field_name: string;
  was: number;
  now: number;
  delta: number;
  needs_why: boolean;
  /** Кто и когда сохранил текущие условия площадки. */
  changed_by?: string;
  changed_at?: string;
}

export interface MpConditionsView {
  period: { year: number; month: number };
  conditions: MpConditionsRow[];
  diff: MpConditionsDiff[];
  hints: Record<string, Record<string, number>>;
  editable: boolean;
  card?: PlanCard;
}

export interface PlanCard {
  id: number;
  pl_id: number;
  form_code: string;
  scope_key: string;
  title: string;
  year: number;
  month: number;
  country: string;
  legal_entity: string;
  currency: string;
  calc_mode: string;
  status: string;
  step_code: string;
  status_label: string;
  has_data?: boolean;
  missing_countries?: string[];
  current_version: number;
  locked: boolean;
}

export interface RouteStep {
  form_code: string;
  step_code: string;
  step_name: string;
  sort_order: number;
  kind: string;
  responsible: string;
  due_rd: number;
  enabled: boolean;
  skip_if_same_user: boolean;
  publish_on_approve: boolean;
}

export interface CardApproval {
  step_code: string;
  user_name: string;
  decision: string;
  target_step: string;
  comment: string;
  version_no: number;
  revoked: boolean;
  revoked_reason: string;
  decided_at: string;
}

export interface CardVersion {
  version_no: number;
  step_from: string;
  step_to: string;
  action: string;
  reason: string;
  created_at: string;
}

export interface MpIssue {
  code: string;
  level: "blocking" | "warning";
  code_cfo?: number;
  name_cfo?: string;
  message: string;
  needs_why?: boolean;
}

export interface MpCommonCostLine {
  code_pl: number;
  // Сервер отдаёт статью структурой PLLineRow, где название сериализуется как
  // expense_name (справочник «Расходы Code PL»). Оставляем оба имени: name —
  // на случай, если строка пришла из значений (MpCommonCostValue), а не из спеки.
  expense_name?: string;
  name?: string;
  group_pl?: string;
}

export interface MpCommonCostGroup {
  group: string;
  lines: MpCommonCostLine[];
}

export interface MpCommonCostValue {
  segment: string;
  code_pl: number;
  name: string;
  group: string;
  period_year: number;
  period_month: number;
  currency: string;
  amount: number;
  source: string;
}

export interface MpRecalcResult {
  period: { year: number; month: number };
  calc_mode: string;
  platforms: Record<string, Record<string, number>>;
  totals: Record<string, number>;
  issues: MpIssue[];
  blocking: boolean;
  common_cost: number;
  preview: boolean;
}

export interface PublishResult {
  card_id: number;
  version_no: number;
  mode: string;
  status: string;
  target: string;
  rows_total: number;
  rows_written: number;
  sum_input: number;
  sum_target: number;
  diff: number;
  /** Запись ушла в PG-песочницу (publish_sandbox), а не в Budgeting. */
  sandbox?: boolean;
  error?: string;
  unmapped?: string[];
  pending?: string[];
}

/** Запись журнала публикаций карточки (и предпросмотры, и записи). */
export interface PublishLogItem {
  id: number;
  version_no: number;
  mode: string;
  target: string;
  status: string;
  rows_total: number;
  rows_written: number;
  diff: number;
  sandbox?: boolean;
  error?: string;
  started_at: string;
}

/** Строка, реально лежащая в приёмнике-песочнице (девять колонок FormToLoad*). */
export interface SandboxRow {
  target_table: string;
  param: string;
  country: string;
  code_cfo: number;
  code_pl: number;
  cfo: string;
  date: string;
  value: number;
}

// Токен там же, где у остальных composables модуля (usePlans.ts): localStorage
// на клиенте, на сервере запросов не делаем.
const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export function useMpConditions() {
  const config = useRuntimeConfig();
  const base = config.public.apiBase;

  // ── Карточка процесса ──
  const cards = (plId: number): Promise<PlanCard[]> =>
    $fetch<PlanCard[]>(`${base}/api/plans/instances/${plId}/cards`, { headers: authHeader() });

  const card = (cardId: number): Promise<{
    card: PlanCard;
    route: RouteStep[];
    approvals: CardApproval[];
    versions: CardVersion[];
    editable: boolean;
  }> => $fetch(`${base}/api/plans/cards/${cardId}`, { headers: authHeader() });

  const cardAction = (
    cardId: number,
    body: { action: string; target_step?: string; comment?: string; retail_totals_reviewed?: boolean }
  ): Promise<PlanCard> =>
    $fetch<PlanCard>(`${base}/api/plans/cards/${cardId}/action`, {
      method: "POST",
      body,
      headers: authHeader()
    });

  const setCalcMode = (cardId: number, mode: string): Promise<PlanCard> =>
    $fetch<PlanCard>(`${base}/api/plans/cards/${cardId}/calc-mode`, {
      method: "PUT",
      body: { mode },
      headers: authHeader()
    });

  const versionPayload = (cardId: number, version: number): Promise<Record<string, unknown>> =>
    $fetch(`${base}/api/plans/cards/${cardId}/versions/${version}`, { headers: authHeader() });

  const publish = (cardId: number, mode: "dry_run" | "write" = "dry_run"): Promise<PublishResult> =>
    $fetch<PublishResult>(`${base}/api/plans/cards/${cardId}/publish`, {
      method: "POST",
      params: { mode },
      headers: authHeader()
    });

  const publishLog = (cardId: number): Promise<PublishLogItem[]> =>
    $fetch<PublishLogItem[]>(`${base}/api/plans/cards/${cardId}/publish-log`, { headers: authHeader() });

  // Что реально записано в песочницу приёмника (PLANS_PUBLISH_SANDBOX=1). Только
  // админ процессов: это срез «внешнего контура», в обычном процессе не нужен.
  const publishSandbox = (target = "", limit = 50): Promise<SandboxRow[]> =>
    $fetch<SandboxRow[]>(`${base}/api/plans/publish-sandbox`, {
      params: { target, limit },
      headers: authHeader()
    });

  // ── Маршрут формы (шаг «Финансист» включается данными) ──
  const formRoute = (formCode: string): Promise<RouteStep[]> =>
    $fetch<RouteStep[]>(`${base}/api/plans/forms/${formCode}/route`, { headers: authHeader() });

  const saveFormRoute = (formCode: string, step: Partial<RouteStep>): Promise<RouteStep[]> =>
    $fetch<RouteStep[]>(`${base}/api/plans/forms/${formCode}/route`, {
      method: "PUT",
      body: step,
      headers: authHeader()
    });

  // ── Условия площадки ──
  const conditions = (cardId: number): Promise<MpConditionsView> =>
    $fetch<MpConditionsView>(`${base}/api/plans/mp/cards/${cardId}/conditions`, { headers: authHeader() });

  const saveConditions = (cardId: number, row: Partial<MpConditionsRow>): Promise<MpConditionsRow> =>
    $fetch<MpConditionsRow>(`${base}/api/plans/mp/cards/${cardId}/conditions`, {
      method: "PUT",
      body: row,
      headers: authHeader()
    });

  const copyConditions = (cardId: number): Promise<{ copied: number }> =>
    $fetch<{ copied: number }>(`${base}/api/plans/mp/cards/${cardId}/conditions/copy`, {
      method: "POST",
      headers: authHeader()
    });

  // ── Пересчёт и валидации ──
  // drafts — несохранённые правки экрана: учитываются только в предпросмотре и
  // ничего не пишут. Пересчёт с записью идёт по сохранённым условиям.
  const recalc = (cardId: number, preview = false, drafts: Partial<MpConditionsRow>[] = []): Promise<MpRecalcResult> =>
    $fetch<MpRecalcResult>(`${base}/api/plans/mp/cards/${cardId}/recalc`, {
      method: "POST",
      params: preview ? { preview: 1 } : {},
      body: preview && drafts.length ? { conditions: drafts } : undefined,
      headers: authHeader()
    });

  const validate = (cardId: number): Promise<{
    issues: MpIssue[];
    blocking: boolean;
    needs_reason: MpIssue[];
    can_submit: boolean;
  }> => $fetch(`${base}/api/plans/mp/cards/${cardId}/validate`, { headers: authHeader() });

  // ── Общие затраты (7 групп статей PL) ──
  const commonCosts = (cardId: number): Promise<{ groups: MpCommonCostGroup[]; values: MpCommonCostValue[] }> =>
    $fetch(`${base}/api/plans/mp/cards/${cardId}/common-costs`, { headers: authHeader() });

  const saveCommonCosts = (
    cardId: number,
    values: Partial<MpCommonCostValue>[]
  ): Promise<{ groups: MpCommonCostGroup[]; values: MpCommonCostValue[] }> =>
    $fetch(`${base}/api/plans/mp/cards/${cardId}/common-costs`, {
      method: "PUT",
      body: { values },
      headers: authHeader()
    });

  return {
    cards, card, cardAction, setCalcMode, versionPayload, publish, publishLog, publishSandbox,
    formRoute, saveFormRoute,
    conditions, saveConditions, copyConditions,
    recalc, validate, commonCosts, saveCommonCosts
  };
}
