// Зеркало серверного каскада TPL-MP (go/internal/plans/mpform_calc.go +
// mpform_inverse.go). Держим формулы синхронно с Go: сервер — авторитетный расчёт
// при загрузке/сохранении, клиент пересчитывает те же значения вживую при вводе.
// Порядок/имена block_type — из mpform_spec.go.
//
// Каскад существует в двух направлениях (ТЗ МП §3.1):
//   legacy  — «суммы → доли»: вводятся суммы, доли и наценки выводятся;
//   inverse — «условия → суммы»: вводятся продажи, %СПП, наценки и доли статей,
//             а вся расходная часть считается.
// Направление задаётся карточкой формы (calc_mode) и приходит в MpTaskForm.

export const B = {
  salesManagerGross: "sales_manager_price",
  salesManagerNet: "sales_manager_price_net",
  spp: "spp",
  salesPlatGross: "sales_platform_price",
  salesPlatNet: "sales_platform_price_net",
  shipments: "shipments",
  markup: "markup",
  discount: "discount",
  markdown: "markdown",
  retailMargin: "retail_margin",
  retailMarginPct: "retail_margin_pct",
  cogsTotal: "cogs_total",
  markupTotal: "markup_total",
  grossMargin: "gross_margin",
  grossMarginPct: "gross_margin_pct",
  commission: "commission",
  platformCosts: "platform_costs_total",
  directShare: "direct_share",
  plPlatform: "pl_platform",
  plPlatformPct: "pl_platform_pct",
  plPlatformTotalCost: "pl_platform_total_cost",
  plPlatformTotalCostPct: "pl_platform_total_cost_pct",
  directShareTurnover: "direct_share_turnover",
} as const;

// Статьи прямых затрат (кроме комиссии) — входят в «Итого прямые затраты».
export const COST_BLOCKS = [
  "cost_agent", "cost_freight", "cost_log_transport", "cost_log_warehouse",
  "cost_ads", "cost_ads_social", "cost_packaging", "cost_acquiring",
  "cost_it", "penalties", "cost_other",
];

// Ставка НДС: законодательная по стране — ФОЛБЭК на случай, когда сервер не
// прислал эффективную ставку площадки. Эффективная ставка своя у каждой площадки
// (20,36 % у WB/Lamoda/Ozon, 16,62 % у Yandex Market — ТЗ §3.4), поэтому она
// приходит с сервера из справочника dir_vat, а не считается здесь.
export const vatByCountry = (country: string): number =>
  country === "KZ" || country === "UZ" ? 0.12 : 0.20;

const div = (a: number, b: number): number => (b === 0 ? 0 : a / b);
const n = (v: number | null | undefined): number => (typeof v === "number" && !Number.isNaN(v) ? v : 0);

// Условия площадки (реестр §6.1): то, из чего считается расходная часть.
export interface MpConditions {
  code_cfo: number;
  spp_pct: number;
  markup_pct: number;
  markup_total_pct: number;
  vat_rate?: number | null;
  shares?: Record<string, number>;
}

// computePlatform — каскад legacy («суммы → доли»).
export function computePlatform(input: Record<string, number | null | undefined>, vat: number): Record<string, number> {
  const out: Record<string, number> = {};
  const managerGross = n(input[B.salesManagerGross]);
  const spp = n(input[B.spp]);
  let shipments = n(input[B.shipments]);
  let cogs = n(input[B.cogsTotal]);

  const managerNet = div(managerGross, 1 + vat);
  const platGross = managerGross * (1 - spp);
  const platNet = div(platGross, 1 + vat);

  out[B.salesManagerGross] = managerGross;
  out[B.salesManagerNet] = managerNet;
  out[B.spp] = spp;
  out[B.salesPlatGross] = platGross;
  out[B.salesPlatNet] = platNet;

  // Наценка, заданная вручную, ведёт себестоимость (ТЗ §3.4: СС = S_пл/(1+наценка)),
  // а не просто отображается — иначе маржа считалась бы по старой себестоимости.
  const mk = input[B.markup];
  if (typeof mk === "number" && mk !== 0) {
    out[B.markup] = mk;
    shipments = div(platNet, 1 + mk);
  } else {
    out[B.markup] = div(platNet, shipments) - (shipments !== 0 ? 1 : 0);
  }
  out[B.shipments] = shipments;

  out[B.retailMargin] = platNet - shipments;
  out[B.retailMarginPct] = div(platNet - shipments, platNet);

  const mkTotal = input[B.markupTotal];
  if (typeof mkTotal === "number" && mkTotal !== 0) {
    out[B.markupTotal] = mkTotal;
    cogs = div(platNet, 1 + mkTotal);
  } else {
    out[B.markupTotal] = div(platNet, cogs) - (cogs !== 0 ? 1 : 0);
  }
  out[B.cogsTotal] = cogs;
  out[B.grossMargin] = platNet - cogs;
  out[B.grossMarginPct] = div(platNet - cogs, platNet);

  const commission = managerNet - platNet;
  out[B.commission] = commission;

  let direct = commission;
  for (const b of COST_BLOCKS) {
    const v = n(input[b]);
    out[b] = v;
    direct += v;
  }
  out[B.platformCosts] = direct;
  out[B.directShare] = div(direct, managerNet);

  const pl = (platNet - cogs) - direct;
  out[B.plPlatform] = pl;
  out[B.plPlatformPct] = div(pl, platNet);
  out[B.plPlatformTotalCost] = out[B.grossMargin] - (direct - commission);
  out[B.plPlatformTotalCostPct] = div(out[B.plPlatformTotalCost], platNet);
  out[B.directShareTurnover] = div(direct - commission, platNet);
  return out;
}

// computePlatformInverse — каскад inverse («условия → суммы», ТЗ §3.4).
// input содержит только продажи по ценам менеджера с НДС и ручные переопределения
// сумм статей; всё остальное берётся из условий площадки.
export function computePlatformInverse(
  input: Record<string, number | null | undefined>,
  cond: MpConditions,
  vatFallback: number
): Record<string, number> {
  const out: Record<string, number> = {};
  const vat = typeof cond.vat_rate === "number" && cond.vat_rate > 0 ? cond.vat_rate : vatFallback;
  const spp = n(cond.spp_pct);
  const shares = cond.shares ?? {};

  const managerGross = n(input[B.salesManagerGross]);
  const managerNet = div(managerGross, 1 + vat);
  out[B.salesManagerGross] = managerGross;
  out[B.salesManagerNet] = managerNet;
  out[B.spp] = spp;
  out[B.salesPlatGross] = managerGross * (1 - spp);
  const platNet = managerNet * (1 - spp);
  out[B.salesPlatNet] = platNet;

  out[B.markup] = n(cond.markup_pct);
  out[B.shipments] = div(platNet, 1 + out[B.markup]);
  out[B.retailMargin] = platNet - out[B.shipments];
  out[B.retailMarginPct] = div(out[B.retailMargin], platNet);

  out[B.markupTotal] = n(cond.markup_total_pct);
  out[B.cogsTotal] = div(platNet, 1 + out[B.markupTotal]);
  out[B.grossMargin] = platNet - out[B.cogsTotal];
  out[B.grossMarginPct] = div(out[B.grossMargin], platNet);

  const commission = managerNet * spp;
  out[B.commission] = commission;

  let direct = commission;
  for (const b of COST_BLOCKS) {
    // Ручное переопределение суммы статьи имеет приоритет над расчётом по доле
    // (ТЗ §7.2: автопересчёт не перезатирает ручной ввод).
    const manual = input[b];
    const v = typeof manual === "number" && !Number.isNaN(manual) ? manual : managerNet * n(shares[b]);
    out[b] = v;
    direct += v;
  }
  out[B.platformCosts] = direct;
  out[B.directShare] = div(direct, managerNet);

  // PL считается от маржи за вычетом прямых затрат БЕЗ комиссии: комиссия уже
  // удержана переходом к цене площадки, второй раз её вычитать нельзя.
  out[B.plPlatform] = out[B.retailMargin] - (direct - commission);
  out[B.plPlatformPct] = div(out[B.plPlatform], platNet);
  out[B.plPlatformTotalCost] = out[B.grossMargin] - (direct - commission);
  out[B.plPlatformTotalCostPct] = div(out[B.plPlatformTotalCost], platNet);
  out[B.directShareTurnover] = div(direct - commission, platNet);
  return out;
}

// Итоги формы по площадкам. PL (сумма) — по формуле файла F274: комиссии всех
// площадок прибавляются обратно, иначе СПП вычитается дважды. Итоговая доля
// прямых затрат в обороте считается по ВСЕМ площадкам (дефект прототипа, дающий
// 27,92 % вместо 26,32 %, не воспроизводится — ТЗ §1 п.25).
export interface MpTotals {
  salesManagerGross: number;
  salesManagerNet: number;
  salesPlatNet: number;
  sppPct: number;
  shipments: number;
  cogsTotal: number;
  retailMargin: number;
  grossMargin: number;
  commission: number;
  directCosts: number;
  commonCosts: number;
  pl: number;
  plPct: number;
  directShareTurnover: number;
}

export function computeFormTotals(
  platforms: Record<string, number>[],
  commonCosts = 0
): MpTotals {
  const t: MpTotals = {
    salesManagerGross: 0, salesManagerNet: 0, salesPlatNet: 0, sppPct: 0,
    shipments: 0, cogsTotal: 0, retailMargin: 0, grossMargin: 0, commission: 0,
    directCosts: 0, commonCosts, pl: 0, plPct: 0, directShareTurnover: 0,
  };
  for (const v of platforms) {
    t.salesManagerGross += n(v[B.salesManagerGross]);
    t.salesManagerNet += n(v[B.salesManagerNet]);
    t.salesPlatNet += n(v[B.salesPlatNet]);
    t.shipments += n(v[B.shipments]);
    t.cogsTotal += n(v[B.cogsTotal]);
    t.retailMargin += n(v[B.retailMargin]);
    t.grossMargin += n(v[B.grossMargin]);
    t.commission += n(v[B.commission]);
    t.directCosts += n(v[B.platformCosts]);
  }
  t.sppPct = div(t.salesManagerNet - t.salesPlatNet, t.salesManagerNet);
  t.pl = t.salesPlatNet - t.shipments - commonCosts - t.directCosts + t.commission;
  t.plPct = div(t.pl, t.salesPlatNet);
  t.directShareTurnover = div(t.directCosts - t.commission, t.salesPlatNet);
  return t;
}
