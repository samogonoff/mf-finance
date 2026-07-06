package plans

// Полная спека формы TPL-MP (large) — как в прототипе «Маркетплейсы_large»
// (строки 11–178). Раньше форма показывала только 2 ввода (1046/8006); теперь —
// весь каскад: продажи (4 строки) → %СПП → наценка → маржа розн/gross → COGS →
// прямые затраты по статьям × площадка → комиссия/доли → PL по площадке → итоги.
//
// Спека — ЕДИНЫЙ источник истины: по ней собирается сетка (MpFormData), считается
// каскад (computeMpPlatform) и рисуется UI. Порядок строк = порядок прототипа.
// См. docs/reports/plans/SPEC.md §9.5, §11.1 и разбор аналитика var/plan/plan/.

// mpPenaltyPL — синтетический code_pl штрафов внутри формы (в источнике штрафы
// лежат под CodePL=66 вместе с «прочими удержаниями»; чтобы развести две строки 66,
// штрафам присваиваем 6600). Online-источник — FINDWHACCESSGROUP (Наименование
// LIKE '%Штраф%'), см. olap_mp.go + SPEC §10.2.
const mpPenaltyPL = 6600

// MpLineKind — тип строки формы.
type MpLineKind string

const (
	// KindInput — ввод тактики (подтягивается из источника/стратегии, редактируется).
	KindInput MpLineKind = "input"
	// KindCalc — расчёт по каскаду, read-only.
	KindCalc MpLineKind = "calc"
	// KindCalcEditable — расчёт, но финансист может переопределить (%СПП, Наценка%).
	KindCalcEditable MpLineKind = "calc_editable"
	// KindHeader — заголовок секции (без значений).
	KindHeader MpLineKind = "header"
)

// Единицы отображения.
const (
	ValueMoney = "money"
	ValuePct   = "pct"
)

// Область строки: значение по каждой площадке или одно на весь сегмент.
const (
	ScopePlatform = "platform"
	ScopeTotal    = "total"
)

// Ключи block_type каскада TPL-MP. Совместимы с ранее сохранёнными pl_metric
// (sales_manager_price/shipments/cogs_total/penalties уже использовались).
const (
	BSalesManagerGross = "sales_manager_price"     // 1046 — ПРОДАЖИ по ценам менеджера с НДС (ввод)
	BSalesManagerNet   = "sales_manager_price_net" // 1045 — …без НДС (расчёт)
	BSPP               = "spp"                      // % СПП (скидка) — расчёт/правка
	BSalesPlatGross    = "sales_platform_price"     // 1022 — по цене площадки с НДС (расчёт)
	BSalesPlatNet      = "sales_platform_price_net" // 1006 — по цене площадки без НДС (расчёт)
	BShipments         = "shipments"                // 8006 — себестоимость по отпускным ценам (ввод)
	BMarkup            = "markup"                    // Наценка, % — расчёт/правка
	BDiscount          = "discount"                  // Скидка (по всем площадкам) — ввод
	BMarkdown          = "markdown"                  // Уценка (по всем площадкам) — ввод
	BRetailMargin      = "retail_margin"             // Маржа розничная — расчёт
	BRetailMarginPct   = "retail_margin_pct"         // Маржинальность розничная, % — расчёт
	BCogsTotal         = "cogs_total"                // 2006/6006 — Себестоимость общая (ввод)
	BMarkupTotal       = "markup_total"              // Наценка от общей сс, % — расчёт
	BGrossMargin       = "gross_margin"              // Маржа (gross) — расчёт
	BGrossMarginPct    = "gross_margin_pct"          // Маржа (gross), % — расчёт
	BCommission        = "commission"                // Комиссия площадки — расчёт (1045−1006)
	BCostAgent         = "cost_agent"                // 64 — Агентское вознаграждение (ввод)
	BCostFreight       = "cost_freight"              // 51 — Грузоперевозки экспорт (ввод)
	BCostLogTransport  = "cost_log_transport"        // 52 — Транспортная логистика (ввод)
	BCostLogWarehouse  = "cost_log_warehouse"        // 54 — Складская логистика (ввод)
	BCostAds           = "cost_ads"                   // 13 — РЕКЛАМА И МАРКЕТИНГ (ввод)
	BCostAdsSocial     = "cost_ads_social"            // 15 — Реклама-соц.сети (ввод)
	BCostPackaging     = "cost_packaging"             // 45 — Расходы на упаковку (ввод)
	BCostAcquiring     = "cost_acquiring"             // 58 — Эквайринг (ввод)
	BCostIT            = "cost_it"                     // 48 — Расходы на IT обслуживание (ввод)
	BCostPenalties     = "penalties"                   // 66 — Штрафы (ввод, FINDWH)
	BCostOther         = "cost_other"                  // 66 — Прочие удержания и компенсации (ввод)
	BPlatformCosts     = "platform_costs_total"        // Итого прямые затраты по площадке — расчёт
	BDirectShare       = "direct_share"                // Доля прямых затрат по площадке, % — расчёт
	BPlPlatform        = "pl_platform"                 // PL по площадке — расчёт
	BPlPlatformPct     = "pl_platform_pct"             // PL по площадке, % — расчёт
)

// MpLine — строка спеки формы.
type MpLine struct {
	BlockType string     `json:"block_type"`
	CodePL    int        `json:"code_pl"`
	Name      string     `json:"name"`
	Section   string     `json:"section"`    // группа-заголовок для UI
	Kind      MpLineKind `json:"kind"`
	ValueKind string     `json:"value_kind"` // money | pct
	Scope     string     `json:"scope"`      // platform | total
	Formula   string     `json:"formula"`    // человекочитаемая формула (для drill-down в UI)
	Editable  bool       `json:"editable"`
	CostLine  bool       `json:"cost_line"`  // статья прямых затрат (для «доли в выручке, %»)
}

// mpFormSpec — полная упорядоченная спека формы large (и small — те же статьи).
func mpFormSpec() []MpLine {
	sales := "Продажи"
	cost := "Себестоимость и наценка"
	marginR := "Маржа розничная"
	cogs := "Себестоимость общая (COGS) и маржа gross"
	direct := "Прямые затраты по площадкам"
	return []MpLine{
		// --- Продажи ---
		{BlockType: BSalesManagerGross, CodePL: 1046, Name: "ПРОДАЖИ по ценам менеджера с НДС", Section: sales, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, Formula: "ручной ввод (тактика)"},
		{BlockType: BSalesManagerNet, CodePL: 1045, Name: "ПРОДАЖИ по ценам менеджера без НДС", Section: sales, Kind: KindCalc, ValueKind: ValueMoney, Scope: ScopePlatform, Formula: "= с НДС / (1 + НДС)"},
		{BlockType: BSPP, Name: "% СПП (скидка)", Section: sales, Kind: KindCalcEditable, ValueKind: ValuePct, Scope: ScopePlatform, Editable: true, Formula: "= 1 − площадка_без_НДС / менеджер_без_НДС"},
		{BlockType: BSalesPlatGross, CodePL: 1022, Name: "ПРОДАЖИ по цене площадки с НДС", Section: sales, Kind: KindCalc, ValueKind: ValueMoney, Scope: ScopePlatform, Formula: "= менеджер_с_НДС × (1 − %СПП)"},
		{BlockType: BSalesPlatNet, CodePL: 1006, Name: "ПРОДАЖИ по цене площадки без НДС", Section: sales, Kind: KindCalc, ValueKind: ValueMoney, Scope: ScopePlatform, Formula: "= площадка_с_НДС / (1 + НДС)"},

		// --- Себестоимость и наценка ---
		{BlockType: BShipments, CodePL: 8006, Name: "Себестоимость по отпускным ценам", Section: cost, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, Formula: "подтягивается из источника, редактируется"},
		{BlockType: BMarkup, Name: "Наценка, %", Section: cost, Kind: KindCalcEditable, ValueKind: ValuePct, Scope: ScopePlatform, Editable: true, Formula: "= площадка_без_НДС / себестоимость − 1"},

		// --- Маржа розничная ---
		{BlockType: BDiscount, Name: "Скидки по всем площадкам", Section: marginR, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopeTotal, Editable: true, Formula: "ручной ввод (по всем МП)"},
		{BlockType: BMarkdown, Name: "Уценки по всем площадкам", Section: marginR, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopeTotal, Editable: true, Formula: "ручной ввод (по всем МП)"},
		{BlockType: BRetailMargin, Name: "Маржа розничная", Section: marginR, Kind: KindCalc, ValueKind: ValueMoney, Scope: ScopePlatform, Formula: "= площадка_без_НДС − себестоимость"},
		{BlockType: BRetailMarginPct, Name: "Маржинальность розничная, %", Section: marginR, Kind: KindCalc, ValueKind: ValuePct, Scope: ScopePlatform, Formula: "= маржа розничная / площадка_без_НДС"},

		// --- COGS и маржа gross ---
		{BlockType: BCogsTotal, CodePL: 2006, Name: "Себестоимость общая (COGS) осн+пошив", Section: cogs, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, Formula: "подтягивается из источника (осн+пошив), редактируется"},
		{BlockType: BMarkupTotal, Name: "Наценка от общей сс, %", Section: cogs, Kind: KindCalc, ValueKind: ValuePct, Scope: ScopePlatform, Formula: "= площадка_без_НДС / COGS − 1"},
		{BlockType: BGrossMargin, Name: "Маржа (gross)", Section: cogs, Kind: KindCalc, ValueKind: ValueMoney, Scope: ScopePlatform, Formula: "= площадка_без_НДС − COGS"},
		{BlockType: BGrossMarginPct, Name: "Маржа (gross), %", Section: cogs, Kind: KindCalc, ValueKind: ValuePct, Scope: ScopePlatform, Formula: "= маржа (gross) / площадка_без_НДС"},

		// --- Прямые затраты по площадкам ---
		{BlockType: BCommission, Name: "Комиссия площадки", Section: direct, Kind: KindCalc, ValueKind: ValueMoney, Scope: ScopePlatform, CostLine: true, Formula: "= менеджер_без_НДС − площадка_без_НДС"},
		{BlockType: BCostAgent, CodePL: 64, Name: "Агентское (комиссионное) вознаграждение", Section: direct, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, CostLine: true, Formula: "статья 64 (источник), редактируется"},
		{BlockType: BCostFreight, CodePL: 51, Name: "Грузоперевозки экспорт", Section: direct, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, CostLine: true, Formula: "статья 51 (источник), редактируется"},
		{BlockType: BCostLogTransport, CodePL: 52, Name: "Транспортная логистика", Section: direct, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, CostLine: true, Formula: "статья 52 (источник), редактируется"},
		{BlockType: BCostLogWarehouse, CodePL: 54, Name: "Складская логистика", Section: direct, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, CostLine: true, Formula: "статья 54 (источник), редактируется"},
		{BlockType: BCostAds, CodePL: 13, Name: "РЕКЛАМА И МАРКЕТИНГ", Section: direct, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, CostLine: true, Formula: "статья 13 (источник), редактируется"},
		{BlockType: BCostAdsSocial, CodePL: 15, Name: "Реклама-соц.сети", Section: direct, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, CostLine: true, Formula: "статья 15 (источник), редактируется"},
		{BlockType: BCostPackaging, CodePL: 45, Name: "Расходы на упаковку (пакеты)", Section: direct, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, CostLine: true, Formula: "статья 45 (источник), редактируется"},
		{BlockType: BCostAcquiring, CodePL: 58, Name: "Эквайринг", Section: direct, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, CostLine: true, Formula: "статья 58 (источник), редактируется"},
		{BlockType: BCostIT, CodePL: 48, Name: "Расходы на IT обслуживание (ПО)", Section: direct, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, CostLine: true, Formula: "статья 48 (источник), редактируется"},
		{BlockType: BCostPenalties, CodePL: 66, Name: "Штрафы", Section: direct, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, CostLine: true, Formula: "штрафы FINDWH (LIKE '%Штраф%'), редактируется"},
		{BlockType: BCostOther, CodePL: 66, Name: "Прочие удержания и компенсации", Section: direct, Kind: KindInput, ValueKind: ValueMoney, Scope: ScopePlatform, Editable: true, CostLine: true, Formula: "статья 66 (источник), редактируется"},
		{BlockType: BPlatformCosts, Name: "Итого прямые затраты по площадке", Section: direct, Kind: KindCalc, ValueKind: ValueMoney, Scope: ScopePlatform, Formula: "= Σ (комиссия + все статьи затрат)"},
		{BlockType: BDirectShare, Name: "Доля прямых затрат по площадке, %", Section: direct, Kind: KindCalc, ValueKind: ValuePct, Scope: ScopePlatform, Formula: "= прямые затраты / менеджер_без_НДС"},
		{BlockType: BPlPlatform, Name: "PL по площадке", Section: direct, Kind: KindCalc, ValueKind: ValueMoney, Scope: ScopePlatform, Formula: "= маржа (gross) − прямые затраты"},
		{BlockType: BPlPlatformPct, Name: "PL по площадке, %", Section: direct, Kind: KindCalc, ValueKind: ValuePct, Scope: ScopePlatform, Formula: "= PL по площадке / площадка_без_НДС"},
	}
}

// mpLineByBlock — быстрый доступ к строке спеки по block_type.
func mpLineByBlock() map[string]MpLine {
	out := map[string]MpLine{}
	for _, l := range mpFormSpec() {
		out[l.BlockType] = l
	}
	return out
}

// plToBlock — обратная карта code_pl → block_type для сидирования факта/стратегии
// из источника (Budgeting). COGS осн(2006)+пошив(6006) оба → cogs_total (суммируются
// вызывающим); штрафы 6600 → penalties.
func plToBlock() map[int]string {
	return map[int]string{
		1046:        BSalesManagerGross,
		1045:        BSalesManagerNet,
		1022:        BSalesPlatGross,
		1006:        BSalesPlatNet,
		8006:        BShipments,
		2006:        BCogsTotal,
		6006:        BCogsTotal,
		64:          BCostAgent,
		51:          BCostFreight,
		52:          BCostLogTransport,
		54:          BCostLogWarehouse,
		13:          BCostAds,
		15:          BCostAdsSocial,
		45:          BCostPackaging,
		58:          BCostAcquiring,
		48:          BCostIT,
		66:          BCostOther,
		mpPenaltyPL: BCostPenalties,
	}
}

// mpEditableSet — множество block_type, которые допустимо сохранять (ввод + правка расчёта).
func mpEditableSet() map[string]bool {
	out := map[string]bool{}
	for _, l := range mpFormSpec() {
		if l.Editable {
			out[l.BlockType] = true
		}
	}
	return out
}
