/**
 * Выбор периода массовой операции (компонент BulkPeriodPicker): ОТКУДА копируем (год и месяц источника) и КУДА
 * вставляем (один месяц или все месяцы до конца года). Чистые функции — без Vue, чтобы правила проверялись тестом.
 */

export type TargetMode = "month" | "to_end";

export interface BulkPeriodPick {
  /** Год источника (копируется ОТКУДА). */
  sourceYear: number;
  /** Месяц источника; 0 — «тот же месяц» (каждый целевой месяц берёт свой). */
  sourceMonth: number;
  /** Куда вставляем: один месяц или все месяцы начиная с выбранного до конца года. */
  targetMode: TargetMode;
  targetMonth: number;
}

/**
 * Список целевых месяцев выбора среди открытых для ввода. Один месяц — только он (если открыт); «до конца года» — все
 * открытые месяцы начиная с выбранного.
 */
export function targetMonthsOf(p: BulkPeriodPick, open: number[]): number[] {
  return p.targetMode === "month" ? open.filter((m) => m === p.targetMonth) : open.filter((m) => m >= p.targetMonth);
}

/** Выбор по умолчанию: все месяцы до конца года, начиная с первого открытого. */
export function defaultPick(open: number[], sourceYear: number, sourceMonth = 0): BulkPeriodPick {
  return { sourceYear, sourceMonth, targetMode: "to_end", targetMonth: open[0] ?? 1 };
}

/** Выбор остаётся корректным при смене набора открытых месяцев: целевой месяц всегда один из открытых. */
export function normalizePick(p: BulkPeriodPick, open: number[]): BulkPeriodPick {
  if (!open.length || open.includes(p.targetMonth)) return p;
  const next = open.find((m) => m >= p.targetMonth) ?? open[open.length - 1];
  return { ...p, targetMonth: next };
}
