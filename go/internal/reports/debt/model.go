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
type Entity struct {
	INN     string  `json:"inn"`
	Name    string  `json:"name"`
	Country Country `json:"country"`
}

// Account — счёт БУ из ТЗ-приложения (sheet «счета БУ»).
type Account struct {
	Code    string  `json:"code"`
	Name    string  `json:"name"`
	Country Country `json:"country"`
}

// Filters — параметры запроса отчёта.
type Filters struct {
	DateFrom    time.Time
	DateTo      time.Time
	EntityINNs  []string
	Accounts    []string
	Currencies  []string
}

// DebtRow — плоская строка отчёта; UI сам группирует по
// (Company → Partner → Account → Contract → Currency).
type DebtRow struct {
	Country         Country `json:"country"`
	Company         string  `json:"company"`
	CompanyINN      string  `json:"company_inn"`
	Partner         string  `json:"partner"`
	PartnerINN      string  `json:"partner_inn,omitempty"`
	Account         string  `json:"account"`
	AccountName     string  `json:"account_name"`
	Subaccount      string  `json:"subaccount"`
	SubaccountName  string  `json:"subaccount_name"`
	Contract        string  `json:"contract"`
	PaymentTermDays int     `json:"payment_term_days"`
	Currency        string  `json:"currency"`

	OpeningDZ float64 `json:"opening_dz"`
	OpeningKZ float64 `json:"opening_kz"`
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
type DocumentRow struct {
	DocDate         time.Time `json:"doc_date"`
	DocNumber       string    `json:"doc_number"`
	DocKind         string    `json:"doc_kind"`
	DZChange        float64   `json:"dz_change"`
	KZChange        float64   `json:"kz_change"`
	PaymentDueDate  time.Time `json:"payment_due_date"`
	OverdueDays     int       `json:"overdue_days"`
}

// SavedFilter — сохранённый пресет фильтров пользователя.
type SavedFilter struct {
	ID        int64           `json:"id"`
	UserID    int64           `json:"-"`
	Name      string          `json:"name"`
	Payload   FilterPayload   `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// FilterPayload — JSON-форма фильтра, хранится в jsonb-колонке.
type FilterPayload struct {
	DateFrom   string   `json:"date_from"`
	DateTo     string   `json:"date_to"`
	EntityINNs []string `json:"entity_inns"`
	Accounts   []string `json:"accounts"`
	Currencies []string `json:"currencies"`
}

// FilterOptions — ответ /filter-options.
type FilterOptions struct {
	Entities   []Entity `json:"entities"`
	Accounts   []Account `json:"accounts"`
	Currencies []string `json:"currencies"`
}

// ReportResponse — ответ /report.
type ReportResponse struct {
	Rows        []DebtRow `json:"rows"`
	GeneratedAt time.Time `json:"generated_at"`
	ReportDate  time.Time `json:"report_date"`
}

// Currencies — закрытый перечень валют, который видит UI.
var Currencies = []string{"BYN", "RUB", "USD", "EUR", "CNY"}
