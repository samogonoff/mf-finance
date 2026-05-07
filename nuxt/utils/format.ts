/**
 * Форматтеры для финансового UI. Используем во всех местах, где выводятся числа.
 *
 * - money(v) — '1 234 567,89 ₽' (без копеек если они нули и compact=false)
 * - money(v, { compact: true }) — '1,23 млн ₽'
 * - signed(v) — добавляет ведущий + для положительных
 * - pct(v) — '+12,3%' / '-4,5%'
 * - num(v) — '1 234 567' (без валюты)
 * - delta(curr, prev) — { abs, pct, dir: 'pos'|'neg'|'zero' }
 *
 * Локаль ru-RU: пробел в качестве разделителя тысяч, запятая для дробной части.
 */

const NBSP = " ";

export function num(v: number | null | undefined, fractionDigits = 0): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  return v.toLocaleString("ru-RU", {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits
  });
}

export interface MoneyOpts {
  currency?: "RUB" | "USD" | "EUR" | "BYN";
  compact?: boolean;
  signed?: boolean;
  fractionDigits?: number;
}

const CURRENCY_SIGN: Record<string, string> = {
  RUB: "₽",
  USD: "$",
  EUR: "€",
  BYN: "Br"
};

export function money(v: number | null | undefined, opts: MoneyOpts = {}): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  const sign = opts.currency ? CURRENCY_SIGN[opts.currency] || opts.currency : "₽";
  const sgn = opts.signed && v > 0 ? "+" : "";
  if (opts.compact) {
    const compact = compactNumber(v);
    return `${sgn}${compact}${NBSP}${sign}`;
  }
  const fd = opts.fractionDigits ?? (Number.isInteger(v) ? 0 : 2);
  const formatted = v.toLocaleString("ru-RU", {
    minimumFractionDigits: fd,
    maximumFractionDigits: fd
  });
  return `${sgn}${formatted}${NBSP}${sign}`;
}

export function signed(v: number | null | undefined, fractionDigits = 0): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  const sgn = v > 0 ? "+" : "";
  return `${sgn}${num(v, fractionDigits)}`;
}

export function pct(v: number | null | undefined, fractionDigits = 1): string {
  if (v === null || v === undefined || Number.isNaN(v)) return "—";
  const sgn = v > 0 ? "+" : "";
  return `${sgn}${v.toLocaleString("ru-RU", {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits
  })}%`;
}

export function compactNumber(v: number): string {
  const abs = Math.abs(v);
  const sgn = v < 0 ? "-" : "";
  if (abs >= 1e9) return `${sgn}${(abs / 1e9).toLocaleString("ru-RU", { maximumFractionDigits: 2 })}${NBSP}млрд`;
  if (abs >= 1e6) return `${sgn}${(abs / 1e6).toLocaleString("ru-RU", { maximumFractionDigits: 2 })}${NBSP}млн`;
  if (abs >= 1e3) return `${sgn}${(abs / 1e3).toLocaleString("ru-RU", { maximumFractionDigits: 1 })}${NBSP}тыс`;
  return `${sgn}${abs.toLocaleString("ru-RU")}`;
}

export function direction(v: number | null | undefined): "pos" | "neg" | "zero" {
  if (!v) return "zero";
  return v > 0 ? "pos" : "neg";
}

export interface Delta {
  abs: number;
  pct: number;
  dir: "pos" | "neg" | "zero";
}

export function delta(curr: number, prev: number): Delta {
  const abs = curr - prev;
  const pctVal = prev === 0 ? 0 : ((curr - prev) / Math.abs(prev)) * 100;
  return { abs, pct: pctVal, dir: direction(abs) };
}

export function formatDate(date: Date | string, fmt: "short" | "long" | "month" = "short"): string {
  const d = typeof date === "string" ? new Date(date) : date;
  if (Number.isNaN(d.getTime())) return "—";
  if (fmt === "long") {
    return d.toLocaleDateString("ru-RU", { day: "numeric", month: "long", year: "numeric" });
  }
  if (fmt === "month") {
    return d.toLocaleDateString("ru-RU", { month: "short", year: "numeric" });
  }
  return d.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit", year: "numeric" });
}
