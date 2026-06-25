package plans

// Модель формы TPL-MP (VS3). Матрица: площадка × блок (code_pl) × месяц.
// Факт — read-only (источник OLAP/FinDWH); тактика — editable. Сценарий фиксирован
// «Тактика бюджет (таргеты)». Расчётные блоки (наценка/маржа) и горизонт M±1 —
// этап 2/последующие срезы. См. docs/reports/plans/SPEC.md §9.

// ScenarioTactic — единственный редактируемый сценарий на этапе 1.1.
const ScenarioTactic = "Тактика бюджет (таргеты)"

// TemplateMP — код шаблона.
const TemplateMP = "TPL-MP"

// MetricRow — значение метрики (строка pl_metric) для записи/чтения тактики.
type MetricRow struct {
	LineCode     int     `json:"line_code"`     // code_pl
	BlockType    string  `json:"block_type"`
	ProfitCenter int     `json:"profit_center"` // code_cfo (площадка)
	Country      string  `json:"country"`
	Scenario     string  `json:"scenario"`
	Year         int     `json:"period_year"`
	Month        int     `json:"period_month"`
	Currency     string  `json:"currency"`
	Amount       float64 `json:"amount"`
	IsManual     bool    `json:"is_manual"`
}

// MpForm — собранная форма для UI.
type MpForm struct {
	Header    MpFormHeader     `json:"header"`
	Platforms []MarketplaceRow `json:"platforms"`
	Blocks    []FormBlock      `json:"blocks"`
}

// MpFormHeader — шапка формы.
type MpFormHeader struct {
	Year     int    `json:"year"`
	Month    int    `json:"month"`
	Segment  string `json:"segment"`
	Currency string `json:"currency"`
	Scenario string `json:"scenario"`
}

// FormBlock — блок метрики (строка-итог code_pl) + строки по площадкам.
type FormBlock struct {
	BlockType string    `json:"block_type"`
	CodePL    int       `json:"code_pl"`
	Name      string    `json:"name"`
	Editable  bool      `json:"editable"`
	Rows      []FormRow `json:"rows"`
}

// FormRow — площадка в блоке: факт (read-only) + тактика (editable, nil если не задана).
// Manual — тактика введена ручной корректировкой (ADJ-04, для подсветки в UI).
type FormRow struct {
	CodeCFO int      `json:"code_cfo"`
	NameCFO string   `json:"name_cfo"`
	Fact    float64  `json:"fact"`
	Tactic  *float64 `json:"tactic"`
	Manual  bool     `json:"manual"`
}

// SaveMpFormRequest — payload PUT /api/plans/mp/form (снимок editable-ячеек,
// зеркало form_submission.json_payload, SPEC §9.7).
type SaveMpFormRequest struct {
	TemplateCode string       `json:"template_code"`
	Segment      string       `json:"segment"`
	Period       PeriodRef    `json:"period"`
	Header       SaveHeader   `json:"header"`
	Rows         []SaveRow    `json:"rows"`
}

// PeriodRef — период формы.
type PeriodRef struct {
	Year  int `json:"year"`
	Month int `json:"month"`
}

// SaveHeader — шапка снимка.
type SaveHeader struct {
	Currency string `json:"currency"`
	Scenario string `json:"scenario"`
}

// SaveRow — одна editable-ячейка тактики.
type SaveRow struct {
	CodeCFO   int     `json:"code_cfo"`
	CodePL    int     `json:"code_pl"`
	BlockType string  `json:"block_type"`
	Amount    float64 `json:"amount"`
	Comment   string  `json:"comment"`
	IsManual  bool    `json:"is_manual"`
}

// AdjustmentRow — аудит ручной корректировки (ADJ-02/03).
type AdjustmentRow struct {
	ProfitCenter int     `json:"profit_center"`
	LineCode     int     `json:"line_code"`
	BlockType    string  `json:"block_type"`
	Year         int     `json:"period_year"`
	Month        int     `json:"period_month"`
	Currency     string  `json:"currency"`
	AdjustedValue float64 `json:"adjusted_value"`
	Reason        string  `json:"reason"`
}

// CommentInput — новый комментарий к экземпляру PL (COM-01).
type CommentInput struct {
	MetricRef string `json:"metric_ref"`
	Body      string `json:"body"`
}

// Comment — комментарий из БД.
type Comment struct {
	ID        int64  `json:"id"`
	MetricRef string `json:"metric_ref"`
	Body      string `json:"body"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// InstanceSummary — строка списка экземпляров PL (экран «Список тактических PL»).
type InstanceSummary struct {
	ID          int64  `json:"id"`
	PeriodYear  int    `json:"period_year"`
	PeriodMonth int    `json:"period_month"`
	Status      string `json:"status"`
	MetricCount int    `json:"metric_count"`
}

// ComputedRow — расчётные показатели каскада по площадке (CALC, превью).
type ComputedRow struct {
	CodeCFO int                `json:"code_cfo"`
	NameCFO string             `json:"name_cfo"`
	Values  map[string]float64 `json:"values"` // sales_net|gross_margin|markup_pct|…
}
