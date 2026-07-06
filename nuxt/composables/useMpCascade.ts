// Зеркало серверного каскада TPL-MP (go/internal/plans/mpform_calc.go). Держим
// формулы синхронно с Go: сервер — авторитетный расчёт при загрузке/сохранении,
// клиент пересчитывает те же значения вживую при вводе. Порядок/имена block_type —
// из mpform_spec.go. См. разбор аналитика var/plan/plan/.

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
} as const;

// Статьи прямых затрат (кроме комиссии) — входят в «Итого прямые затраты».
export const COST_BLOCKS = [
  "cost_agent", "cost_freight", "cost_log_transport", "cost_log_warehouse",
  "cost_ads", "cost_ads_social", "cost_packaging", "cost_acquiring",
  "cost_it", "penalties", "cost_other",
];

export const vatByCountry = (country: string): number =>
  country === "KZ" || country === "UZ" ? 0.12 : 0.20;

const div = (a: number, b: number): number => (b === 0 ? 0 : a / b);
const n = (v: number | null | undefined): number => (typeof v === "number" && !Number.isNaN(v) ? v : 0);

// computePlatform — весь каскад по площадке из введённых значений (in: block→число).
export function computePlatform(input: Record<string, number | null | undefined>, vat: number): Record<string, number> {
  const out: Record<string, number> = {};
  const managerGross = n(input[B.salesManagerGross]);
  const spp = n(input[B.spp]);
  const shipments = n(input[B.shipments]);
  const cogs = n(input[B.cogsTotal]);

  const managerNet = div(managerGross, 1 + vat);
  const platGross = managerGross * (1 - spp);
  const platNet = div(platGross, 1 + vat);

  out[B.salesManagerGross] = managerGross;
  out[B.salesManagerNet] = managerNet;
  out[B.spp] = spp;
  out[B.salesPlatGross] = platGross;
  out[B.salesPlatNet] = platNet;
  out[B.shipments] = shipments;

  const mk = input[B.markup];
  out[B.markup] = typeof mk === "number" && mk !== 0 ? mk : div(platNet, shipments) - (shipments !== 0 ? 1 : 0);

  out[B.retailMargin] = platNet - shipments;
  out[B.retailMarginPct] = div(platNet - shipments, platNet);

  out[B.cogsTotal] = cogs;
  out[B.markupTotal] = div(platNet, cogs) - (cogs !== 0 ? 1 : 0);
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
  return out;
}
