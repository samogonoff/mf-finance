// API формы «Тактический план продаж, Розница» (TPL-TO-RETAIL).
//
// Типизированные обёртки над всеми ручками розницы: сетка формы, сохранение,
// массовые операции, параметры периода, переопределение LFL, свод согласования,
// валидации, экспорт/импорт Excel и пресеты представлений.
//
// Здесь же — чистые хелперы, которые НЕ дублируют бизнес-логику сервера, а
// повторяют ровно те формулы, что нужны для мгновенной подсветки при вводе
// (§4.4): пересчитывать всю форму запросом на каждое нажатие клавиши нельзя —
// 375 строк × 12 месяцев, а согласующему нужно видеть отклонение сразу.
// Авторитет — всегда ответ сервера после сохранения.

export const RETAIL_FORM_CODE = "TPL-TO-RETAIL";

/** Метрики ячеек. Пользователь вводит только sales (§3); payroll/rent наполняют массовые операции §5. */
export type RetailMetric = "sales" | "payroll" | "rent";

/** LFL-статусы магазина (§2, §4.4). new/xxx — «базы сравнения нет». */
export type RetailLFL = "lfl" | "under1y" | "new" | "xxx" | "closed" | "reconstruction";

export interface RetailValue {
  metric: RetailMetric;
  year: number;
  month: number;
  amount: number | null;
  source: string;
  note?: string;
  editable: boolean;
  updated_at?: string;
}

/** Расчётные показатели строки (§4.4). null — «не рассчитывается», выводить «—», а не 0. */
export interface RetailIndicators {
  fact_prev_year_month: number | null;
  fact_prev_month: number | null;
  fact_cur_month: number | null;
  strategy: number | null;
  tactic_approved: number | null;
  tactic: number | null;
  plan_done_pct: number | null;
  lfl_tactic: number | null;
  lfm_tactic: number | null;
  vs_strategy_pct: number | null;
  period_total: number;
  avg_month_fact: number | null;
  year_expectation: number;
  year_exp_vs_prev_pct: number | null;
}

export interface RetailRow {
  id: number;
  code_cfo: number;
  klient_id: string;
  cfo: string;
  city: string;
  lfl_status: string;
  lfl_effective: string;
  lfl_override: boolean;
  lfl_reason?: string;
  store_type: string;
  category: string;
  reg_manager: string;
  legal_entity: string;
  ploschad: number;
  date_open?: string;
  date_close?: string;
  stage?: string;
  code_fox?: string;
  manager?: string;
  pl_analytic?: string;
  comment: string;
  row_version: number;
  values: RetailValue[];
  indicators: RetailIndicators;
  warnings?: ValidationIssue[];
}

export interface RetailParam {
  id?: number;
  scope_kind: "country" | "city" | "lfl" | "store_type" | "store";
  scope_value: string;
  param_code: string;
  value: number;
  note?: string;
  set_by?: number;
  set_by_name?: string;
  set_at?: string;
}

export interface ValidationIssue {
  code: string;
  blocking: boolean;
  code_cfo?: number;
  month?: number;
  message: string;
  value?: number;
}

export interface RetailForm {
  card_id: number;
  instance_id: number;
  form_code: string;
  country: string;
  legal_entity: string;
  /** Валюта ОТОБРАЖЕНИЯ (может отличаться от валюты ввода). */
  currency: string;
  /** Валюта ВВОДА — национальная валюта страны (§4.3, V-11). */
  nat_currency: string;
  year: number;
  month: number;
  /** Плановые месяцы экземпляра: от месяца карточки до декабря (§4.4 «ожидание года»). */
  months: number[];
  editable: boolean;
  status: string;
  step_code: string;
  rows: RetailRow[];
  params: RetailParam[];
  warnings?: ValidationIssue[];
  /** Курс тактики — им пересчитаны вводимые ячейки (см. fx_rates). */
  fx_rate: number;
  /**
   * Курсы отображения ПО НАЗНАЧЕНИЮ колонки. Одного курса не существует: в файле
   * ТО тактика пересчитывается курсом факта предыдущего месяца, факт — курсом
   * своего месяца, стратегия — плановым.
   */
  fx_rates?: RetailFxRates;
  /** Подсказки к полям ввода: для аренды — факт предыдущего месяца, справочно. */
  hints?: FieldHint[];
  /** Источники данных формы: строк, когда загружено, ошибка (ТЗ доработок п. 1.1). */
  sources?: RetailSourceInfo[];
}

export interface RetailSourceInfo {
  code: "stores" | "fact" | "comparison_fact" | "strategy" | "approved" | "new_strategy";
  label: string;
  rows: number;
  loaded_at: string;
  error?: string;
}

/** Курсы отображения по назначению колонки (зеркало plans.RetailFxRates). */
export interface RetailFxRates {
  /** Тактика, план периода, ожидание года — курс факта ПРЕДЫДУЩЕГО месяца. */
  tactic: number;
  /** Факт текущего года и производные от него. */
  fact_current: number;
  /** Факт того же месяца прошлого года. */
  fact_prev_year: number;
  /** Стратегия — плановый курс. */
  strategy: number;
}

/**
 * Подсказка к полю ввода (правки ТЗ 02.09.2026, часть 1). Значение НЕ
 * подставляется в поле — показывается рядом с ним вместе с происхождением и
 * периодом, чтобы решение осталось за человеком.
 */
export interface FieldHint {
  kind: string;
  code_cfo: number;
  block_type: string;
  value: number;
  source: string;
  note?: string;
  as_of?: string;
}

export interface RetailCellWrite {
  code_cfo: number;
  metric: RetailMetric;
  year: number;
  month: number;
  amount: number | null;
  source?: string;
  note?: string;
}

export interface RetailSaveRequest {
  cells?: RetailCellWrite[];
  comments?: { code_cfo: number; comment: string }[];
}

export type RetailBulkOp = "copy_scenario" | "distribute" | "sales_index" | "payroll" | "rent";
export type RetailBulkScope = "all" | "filtered" | "selected";

export interface RetailBulkRequest {
  op: RetailBulkOp;
  scope: RetailBulkScope;
  preview: boolean;
  code_cfos?: number[];
  months?: number[];
  reset_manual?: boolean;
  // copy_scenario
  // ТЗ доработок п. 2.2/2.3: только коэффициент (надбавки нет), один месяц,
  // источник — тот же месяц сценария (факт — прошлого года).
  source?: "strategy" | "tactic" | "fact" | "new_strategy";
  coefficient?: number;
  coefficient_set?: boolean;
  // distribute
  target_total?: number;
  base_months?: number[];
  // sales_index
  index_base?: "fact_prev_month" | "fact_prev_year" | "approved_prev" | "strategy";
  // payroll: дефолт — план текущего месяца (ответ финблока §12 п.12);
  // остальные базы оставлены для периодов, утверждённых до ответа.
  payroll_base?: "plan_current_month" | "strategy" | "fact_prev_year" | "approved_prev";
  // rent: оборотная часть на этапе 1 не считается (§12 п.13) — только по запросу.
  rent_turnover?: boolean;
}

export interface RetailBulkDiff {
  code_cfo: number;
  cfo: string;
  metric: RetailMetric;
  year: number;
  month: number;
  before: number | null;
  after: number;
  base?: number | null;
  before_source?: string;
  after_source: string;
  note?: string;
}

export interface RetailBulkResult {
  op: string;
  scope: string;
  preview: boolean;
  rows_in: number;
  changed: number;
  protected_manual: number;
  skipped_no_base: number;
  diff: RetailBulkDiff[];
  notes?: string[];
}

export interface RetailReconDetail {
  code_cfo: number;
  cfo: string;
  left: number;
  right: number;
  diff: number;
  note?: string;
}

export interface RetailReconciliation {
  code: string;
  title: string;
  left: number;
  right: number;
  diff: number;
  ok: boolean;
  /** Сверку выполнить не удалось (нет источника) — это НЕ «расхождение 0». */
  skipped?: boolean;
  note?: string;
  details?: RetailReconDetail[];
}

export interface RetailValidationReport {
  card_id: number;
  blocking: ValidationIssue[];
  warnings: ValidationIssue[];
  can_submit: boolean;
  /**
   * Жёсткий ли режим гейта (ответ 41 от 10.09.2026). В мягком режиме
   * can_submit остаётся true при непустом blocking: замечания показываются,
   * но отправку не держат — и подписи обязаны это различать.
   */
  strict: boolean;
  reconciliations: RetailReconciliation[];
}

export type RetailSummaryMetrics = Record<string, number | boolean>;

export interface RetailSummarySection {
  key: string;
  title: string;
  rows: number;
  metrics: RetailSummaryMetrics;
}

export interface RetailCurrencyBlock {
  currency: string;
  /** true — полный набор из 11 показателей (нац. валюта), false — сокращённый (§8). */
  full: boolean;
  fields: string[];
  fx_rate: number;
  total: RetailSummaryMetrics;
  sections: RetailSummarySection[];
}

export interface RetailSummaryGroup {
  key: string;
  title: string;
  sections: RetailSummarySection[];
}

export interface RetailSummary {
  card_id: number;
  country: string;
  nat_currency: string;
  year: number;
  month: number;
  title: string;
  rows: number;
  blocks: RetailCurrencyBlock[];
  groups: RetailSummaryGroup[];
  reconciliations: RetailReconciliation[];
}

export interface RetailImportIssue {
  row?: number;
  code_cfo?: number;
  message: string;
}

export interface RetailImportResult {
  rows_in_file: number;
  cells_saved: number;
  skipped: number;
  issues: RetailImportIssue[];
}

/** Пресет представления формы (§4.6). Имя "" — «последнее использованное». */
export interface ViewPreset {
  name: string;
  is_default: boolean;
  payload: Record<string, unknown>;
}

// Токен там же, где у остальных composables модуля: localStorage на клиенте,
// на сервере запросов не делаем.
const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

/** Текст ошибки $fetch: сервер отвечает {error}, и его сообщение осмысленнее «500». */
export const retailErrText = (e: unknown, fallback: string): string => {
  if (typeof e === "object" && e && "data" in e) {
    const d = (e as { data?: { error?: string } }).data;
    if (d?.error) return d.error;
  }
  return e instanceof Error ? e.message : fallback;
};

export function useRetail() {
  const base = useRuntimeConfig().public.apiBase;
  const url = (cardId: number, tail: string) => `${base}/api/plans/retail/${cardId}/${tail}`;

  const form = (cardId: number, currency = ""): Promise<RetailForm> =>
    $fetch<RetailForm>(url(cardId, "form"), {
      params: currency ? { currency } : {},
      headers: authHeader()
    });

  const saveForm = (cardId: number, body: RetailSaveRequest): Promise<RetailForm> =>
    $fetch<RetailForm>(url(cardId, "form"), { method: "PUT", body, headers: authHeader() });

  const bulk = (cardId: number, body: RetailBulkRequest): Promise<RetailBulkResult> =>
    $fetch<RetailBulkResult>(url(cardId, "bulk"), { method: "POST", body, headers: authHeader() });

  const params = (cardId: number): Promise<RetailParam[]> =>
    $fetch<RetailParam[]>(url(cardId, "params"), { headers: authHeader() });

  // Сервер принимает и голый массив, и {params}. Шлём {params} — тело
  // самоописательное, и в логах видно, что это за запрос.
  const saveParams = (cardId: number, list: RetailParam[]): Promise<RetailParam[]> =>
    $fetch<RetailParam[]>(url(cardId, "params"), {
      method: "PUT",
      body: { params: list },
      headers: authHeader()
    });

  /** Пустой lfl_status снимает переопределение. Причина обязательна (§4.3). */
  const lflOverride = (
    cardId: number,
    body: { code_cfo: number; lfl_status: string; reason: string }
  ): Promise<RetailForm> =>
    $fetch<RetailForm>(url(cardId, "lfl-override"), { method: "PUT", body, headers: authHeader() });

  const summary = (cardId: number): Promise<RetailSummary> =>
    $fetch<RetailSummary>(url(cardId, "summary"), { headers: authHeader() });

  /** Свод «все четыре розницы» периода (ТЗ доработок п. 3.2). */
  const consolidated = (plId: number): Promise<RetailConsolidated> =>
    $fetch<RetailConsolidated>(`${base}/api/plans/retail/period/${plId}/consolidated`, { headers: authHeader() });

  /** Статусы LFL из справочника [001 CodeCFO] (ТЗ доработок п. 5.2). */
  const lflStatuses = (): Promise<{ source: string; statuses: RetailLFLStatusCount[] }> =>
    $fetch(`${base}/api/plans/retail/lfl-statuses`, { headers: authHeader() });

  /** Обновление LFL-статусов магазинов из Лисы во всех периодах (только админ). dryRun — предпросмотр без записи. */
  const lflRefresh = (dryRun: boolean): Promise<RetailLFLRefreshResult> =>
    $fetch(`${base}/api/plans/retail/lfl-refresh${dryRun ? "?dry_run=1" : ""}`, { method: "POST", headers: authHeader() });

  /** Схема согласования карточек розницы и МП периода (ТЗ доработок п. 4.3). */
  const routeScheme = (plId: number): Promise<{ pl_id: number; cards: RouteSchemeCard[] }> =>
    $fetch(`${base}/api/plans/period/${plId}/route-scheme`, { headers: authHeader() });

  /** Стратегические доли статей МП по месяцам года (раздел «Стратегические планы», п. 5.1). */
  const strategyMpShares = (year: number): Promise<StrategyMpShares> =>
    $fetch(`${base}/api/plans/strategy/mp-shares`, { params: { year }, headers: authHeader() });

  const validate = (cardId: number): Promise<RetailValidationReport> =>
    $fetch<RetailValidationReport>(url(cardId, "validate"), { headers: authHeader() });

  /** Экспорт .xlsx: ответ бинарный, поэтому responseType: blob. */
  const exportXlsx = (cardId: number, currency = ""): Promise<Blob> =>
    $fetch<Blob>(url(cardId, "export"), {
      params: currency ? { currency } : {},
      headers: authHeader(),
      responseType: "blob"
    });

  /** Пример .xlsx для импорта: колонки-ключи, месяцы периода и подсказки. */
  const importTemplateXlsx = (cardId: number, currency = ""): Promise<Blob> =>
    $fetch<Blob>(url(cardId, "import-template"), {
      params: currency ? { currency } : {},
      headers: authHeader(),
      responseType: "blob"
    });

  const importXlsx = (cardId: number, file: File): Promise<RetailImportResult> => {
    const fd = new FormData();
    fd.append("file", file);
    return $fetch<RetailImportResult>(url(cardId, "import"), {
      method: "POST",
      body: fd,
      headers: authHeader()
    });
  };

  // ── Пресеты представлений (общие для форм модуля, §4.6) ──
  const presets = (formCode = RETAIL_FORM_CODE): Promise<ViewPreset[]> =>
    $fetch<ViewPreset[]>(`${base}/api/plans/forms/${formCode}/presets`, { headers: authHeader() });

  const savePreset = (p: ViewPreset, formCode = RETAIL_FORM_CODE): Promise<ViewPreset[]> =>
    $fetch<ViewPreset[]>(`${base}/api/plans/forms/${formCode}/presets`, {
      method: "PUT",
      body: p,
      headers: authHeader()
    });

  const deletePreset = (name: string, formCode = RETAIL_FORM_CODE): Promise<ViewPreset[]> =>
    $fetch<ViewPreset[]>(`${base}/api/plans/forms/${formCode}/presets`, {
      method: "DELETE",
      params: { name },
      headers: authHeader()
    });

  return {
    form, saveForm, bulk, params, saveParams, lflOverride,
    summary, consolidated, lflStatuses, lflRefresh, routeScheme, strategyMpShares, validate, exportXlsx, importXlsx, importTemplateXlsx,
    presets, savePreset, deletePreset
  };
}

/** Свод «все четыре розницы»: итог и страны в BYN, магазины — в BYN. */
export interface RetailConsolidatedStore {
  code_cfo: number;
  cfo: string;
  city: string;
  lfl: string;
  metrics: RetailSummaryMetrics;
}
export interface RetailConsolidatedCountry {
  country: string;
  card_id: number;
  title: string;
  status: string;
  step_code: string;
  nat_currency: string;
  rows: number;
  nat: RetailSummaryMetrics;
  byn: RetailSummaryMetrics;
  stores: RetailConsolidatedStore[];
  error?: string;
}
export interface RetailConsolidated {
  pl_id: number;
  year: number;
  month: number;
  currency: string;
  total: RetailSummaryMetrics;
  countries: RetailConsolidatedCountry[];
  /** Страны, которые ещё не дошли до шага 1.3: пока список не пуст, на 1.4 не уходит. */
  pending: string[];
}

/** Итог обновления LFL-статусов по всем периодам (предпросмотр или применение). */
export interface RetailLFLRefreshResult {
  dry_run: boolean;
  stores_in_lisa: number;
  rows_scanned: number;
  /** Статус Лисы отличается, но зависит от времени и в прошлые периоды не переносится. */
  rows_skipped: number;
  rows_changed: number;
  cards_changed: number;
  locked_cards: number;
  transitions: { from: string; to: string; rows: number }[];
  periods: { card_id: number; country: string; year: number; month: number; card_status: string; editable: boolean; changed: number }[];
}

export interface RetailLFLStatusCount {
  raw: string;
  canonical: string;
  stores: number;
  by_country: Record<string, number>;
}

export interface RouteSchemeStep {
  step_code: string;
  step_name: string;
  kind: "fill" | "approve" | "final";
  responsible: string;
  owner_user_id?: number;
  owner_name?: string;
  gap: boolean;
  current: boolean;
  skip_if_same_user: boolean;
  due_rd: number;
}
export interface RouteSchemeCard {
  card_id: number;
  form_code: string;
  scope_key: string;
  title: string;
  status: string;
  steps: RouteSchemeStep[];
  gaps: number;
  error?: string;
}

export interface StrategyMpShares {
  year: number;
  source: string;
  platforms: { code_cfo: number; name_cfo: string; segment: string; country: string }[];
  months: { month: number; shares: Record<string, Record<string, number>> | null }[];
}

// ───────────────────────── чистые хелперы ─────────────────────────

/** Ключ ячейки в локальной модели ввода. */
export const cellKey = (codeCfo: number, metric: RetailMetric, year: number, month: number): string =>
  `${codeCfo}|${metric}|${year}|${month}`;

/** IFERROR(a/b; 0) — точное соответствие формулам прототипа (§4.4). */
export const iferrorDiv = (a: number, b: number): number => (b === 0 ? 0 : a / b);

/** IFERROR(a/b − 1; 0): при b=0 ноль целиком, а не −1 (IFERROR ловит деление). */
export const iferrorRatioMinus1 = (a: number, b: number): number => (b === 0 ? 0 : a / b - 1);

/** У магазина нет базы сравнения: LFL тактич. и % вып. не считаются (§4.4). */
export const noBaseline = (lfl: string): boolean => lfl === "new" || lfl === "xxx";

/**
 * Значение параметра периода для строки — от частного к общему (§5).
 * Порядок повторяет RetailParamSet.Resolve на сервере; расхождение порядка
 * означало бы, что подсветка в форме и расчёт массовой операции спорят.
 */
export function resolveParam(
  params: RetailParam[],
  code: string,
  row: Pick<RetailRow, "code_cfo" | "store_type" | "lfl_effective" | "city">,
  country: string,
  fallback: number
): number {
  const scopes: [RetailParam["scope_kind"], string][] = [
    ["store", String(row.code_cfo)],
    ["store_type", row.store_type],
    ["lfl", row.lfl_effective],
    ["city", row.city],
    ["country", country]
  ];
  for (const [kind, want] of scopes) {
    if (!want) continue;
    const hit = params.find(
      (p) => p.param_code === code && p.scope_kind === kind && p.scope_value.toLowerCase() === want.toLowerCase()
    );
    if (hit) return hit.value;
  }
  // Страновой параметр мог быть задан с пустым scope_value («на всю форму»).
  const any = params.find((p) => p.param_code === code && p.scope_kind === "country" && p.scope_value === "");
  return any ? any.value : fallback;
}

/** Человекочитаемые подписи LFL-статусов. */
export const LFL_LABELS: Record<string, string> = {
  lfl: "LFL",
  under1y: "до года",
  new: "новый",
  xxx: "ххх",
  closed: "закрыт",
  reconstruction: "реконструкция"
};

/** Подписи источников значения ячейки (§5: «результат — значения с source-признаком»). */
export const SOURCE_LABELS: Record<string, string> = {
  manual: "введено вручную",
  from_strategy: "из стратегии",
  from_prev_period: "из прошлого периода",
  index_applied: "индекс роста",
  payroll_share: "ФОТ от продаж",
  rent_carryover: "аренда прошлого периода",
  distributed: "распределение суммы",
  import: "импорт из Excel"
};

/** Названия массовых операций (§5). */
export const BULK_OP_LABELS: Record<RetailBulkOp, string> = {
  copy_scenario: "Копирование сценария",
  distribute: "Распределение целевой суммы",
  sales_index: "Индекс роста продаж",
  payroll: "ФОТ от продаж",
  rent: "Аренда"
};

/** Названия параметров периода (§5, §10). */
export const PARAM_LABELS: Record<string, string> = {
  sales_index: "Индекс роста продаж",
  payroll_share: "ФОТ: удельный вес от продаж",
  payroll_cap: "ФОТ: верхняя граница выполнения",
  rent_share: "Аренда: удельный вес",
  rent_turnover_threshold: "Аренда: порог оборота",
  rent_rate: "Аренда: ставка переменной части",
  lfl_warn: "Порог подсветки LFL тактич.",
  lfm_warn: "Порог подсветки LFM тактич.",
  strategy_warn: "Порог подсветки откл. от стратегии"
};

export const MONTH_LABELS = [
  "янв", "фев", "мар", "апр", "май", "июн",
  "июл", "авг", "сен", "окт", "ноя", "дек"
];
