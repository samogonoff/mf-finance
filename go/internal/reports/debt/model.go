package debt

import "time"

// Country — страна юрлица, влияет на правила свёртки счетов и выручки.
type Country string

const (
	CountryRB Country = "РБ"
	CountryRF Country = "РФ"
	CountryKZ Country = "КЗ"
	CountryUZ Country = "УЗ"
	CountryTR Country = "Турция"
	CountryCZ Country = "Чехия"
	CountryGB Country = "Великобритания"
	CountryCN Country = "Китай"
	CountryKG Country = "Кыргызстан"
)

// Entity — юрлицо ГК «Марк Формэль» из ТЗ-приложения.
//
// Code — короткий код компании из Table_Fin_PL.Компания (MF/F/TDMF/…). В финальной
// PL-витрине ИНН нет, только код — он и резолвится в ИНН/страну через EntityByCode
// (см. seed.go, справочник codeIndex). Пусто для ЮЛ, не встречающихся в PL-данных
// (зарубежные ЮЛ, ИП).
type Entity struct {
	INN     string  `json:"inn"`
	Name    string  `json:"name"`
	Country Country `json:"country"`
	Code    string  `json:"code,omitempty"`
}

// Account — счёт БУ из ТЗ-приложения (sheet «счета БУ»).
type Account struct {
	Code    string  `json:"code"`
	Name    string  `json:"name"`
	Country Country `json:"country"`
}

// Filters — параметры запроса отчёта.
type Filters struct {
	DateFrom   time.Time
	DateTo     time.Time
	EntityINNs []string
	Accounts   []string
	Currencies []string
	// OnlyICO — фильтр «только внутригрупповые операции» (Premaster.ICO = 1).
	// По дефолту true в handler: см. open-questions.md §A3 (ВГО-сценарий по
	// умолчанию до подтверждения от автора ТЗ).
	OnlyICO bool
}

// DebtRow — плоская строка отчёта; UI сам группирует по
// (Company → Partner → Account → Contract → Currency).
type DebtRow struct {
	Country    Country `json:"country"`
	Company    string  `json:"company"`
	CompanyINN string  `json:"company_inn"`
	Partner    string  `json:"partner"`
	PartnerINN string  `json:"partner_inn,omitempty"`
	// Manager/Channel — обогащение из Counterparty1C (менеджер пары и канал продаж).
	// Пустые, если справочник не подключён или контрагента в нём нет.
	Manager        string `json:"manager,omitempty"`
	Channel        string `json:"channel,omitempty"`
	Account        string `json:"account"`
	AccountName    string `json:"account_name"`
	Subaccount     string `json:"subaccount"`
	SubaccountName string `json:"subaccount_name"`
	Contract       string `json:"contract"`
	// ContractRef — сырая 1С-ссылка договора (субконто). Непрозрачна для UI, нужна
	// только чтобы drill-down точно отфильтровал документы этого договора.
	ContractRef     string `json:"contract_ref,omitempty"`
	PaymentTermDays int    `json:"payment_term_days"`
	// Срок/просрочка по договору (из Payments.Docs). Пусто, если срок не заведён.
	PaymentDueDate time.Time `json:"payment_due_date,omitempty"`
	OverdueDays    int       `json:"overdue_days,omitempty"`
	Currency       string    `json:"currency"`

	OpeningDZ  float64 `json:"opening_dz"`
	OpeningKZ  float64 `json:"opening_kz"`
	TurnoverDZ float64 `json:"turnover_dz"`
	TurnoverKZ float64 `json:"turnover_kz"`
	ClosingDZ  float64 `json:"closing_dz"`
	ClosingKZ  float64 `json:"closing_kz"`

	RevenuePeriod    float64 `json:"revenue_period"`
	RevenueLastMonth float64 `json:"revenue_last_month"`
}

// DocumentRow — детализация по документу (drill-down под договором).
// "Дата оплаты по договору" и "Задолженность в днях" заполняются ТОЛЬКО здесь
// (по ТЗ — на уровне группировки они пустые).
//
// Amount — суммарный modulus по проводкам документа (показатель «вес» документа,
// независимо от того, DZ/KZ это или revenue-счёт).
//
// Description — operation_description первой проводки документа (если нет —
// trans_description). Текстовая расшифровка «что произошло».
//
// TransGroup — trans_description первой проводки документа. Используется UI
// для группировки документов внутри drill-down (M5): когда контракт пуст,
// документы разделяются по типу операции: «Реализация материалов», «Аренда»
// и т.п. Если пусто — UI ставит fallback на DocKind.
type DocumentRow struct {
	DocDate        time.Time `json:"doc_date"`
	DocNumber      string    `json:"doc_number"`
	DocKind        string    `json:"doc_kind"`
	TransGroup     string    `json:"trans_group"`
	Amount         float64   `json:"amount"`
	Description    string    `json:"description"`
	DZChange       float64   `json:"dz_change"`
	KZChange       float64   `json:"kz_change"`
	PaymentDueDate time.Time `json:"payment_due_date"`
	OverdueDays    int       `json:"overdue_days"`
}

// SavedFilter — сохранённый пресет фильтров пользователя.
type SavedFilter struct {
	ID        int64         `json:"id"`
	UserID    int64         `json:"-"`
	Name      string        `json:"name"`
	Payload   FilterPayload `json:"payload"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// FilterPayload — JSON-форма фильтра, хранится в jsonb-колонке.
//
// OnlyICO — указатель, чтобы отличать «явно не задано» (старые пресеты, до Level 1
// MVP) от «явно false». При nil — handler подставит дефолт true.
type FilterPayload struct {
	DateFrom   string   `json:"date_from"`
	DateTo     string   `json:"date_to"`
	EntityINNs []string `json:"entity_inns"`
	Accounts   []string `json:"accounts"`
	Currencies []string `json:"currencies"`
	OnlyICO    *bool    `json:"only_ico,omitempty"`
}

// FilterOptions — ответ /filter-options.
type FilterOptions struct {
	Entities   []Entity  `json:"entities"`
	Accounts   []Account `json:"accounts"`
	Currencies []string  `json:"currencies"`
}

// ReportResponse — ответ /report.
type ReportResponse struct {
	Rows        []DebtRow `json:"rows"`
	GeneratedAt time.Time `json:"generated_at"`
	ReportDate  time.Time `json:"report_date"`
}

// Currencies — закрытый перечень валют, который видит UI.
var Currencies = []string{"BYN", "RUB", "USD", "EUR", "CNY"}
