package plans

import "context"

const TemplateCfoExpense = TemplateExp

type CfoExpenseValue struct {
	Scenario string   `json:"scenario"`
	Year     int      `json:"year"`
	Month    int      `json:"month"`
	Amount   *float64 `json:"amount"`
}
type CfoExpenseRow struct {
	ID             int64             `json:"id"`
	CodeCFO        int               `json:"code_cfo"`
	CFO            string            `json:"cfo"`
	CodePL         int               `json:"code_pl"`
	LegalEntity    string            `json:"legal_entity"`
	GroupPL        string            `json:"group_pl"`
	Expense        string            `json:"expense"`
	Country        string            `json:"country"`
	CountryCompany string            `json:"country_company"`
	Comment        string            `json:"comment,omitempty"`
	Values         []CfoExpenseValue `json:"values"`
}
type CfoExpenseForm struct {
	Card        Card            `json:"card"`
	Config      CfoExpSpec      `json:"config"`
	Rows        []CfoExpenseRow `json:"rows"`
	Total       int             `json:"total"`
	Offset      int             `json:"offset"`
	ReadOnly    bool            `json:"read_only"`
	SourceError string          `json:"source_error,omitempty"`
	OpenMonths  []bool          `json:"open_months"`
}
type CfoExpenseCell struct {
	CodeCFO     int      `json:"code_cfo"`
	CodePL      int      `json:"code_pl"`
	LegalEntity string   `json:"legal_entity"`
	Scenario    string   `json:"scenario,omitempty"`
	Year        int      `json:"year,omitempty"`
	Month       int      `json:"month"`
	Amount      *float64 `json:"amount"`
}
type CfoExpenseFilter struct {
	Search, GroupPL, CountryCompany, LegalEntity, Sort string
	CodePL, CodeCFO, Offset, Limit                     int
	Desc                                               bool
}
type CfoExpenseIssue struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Month    int    `json:"month,omitempty"`
	Blocking bool   `json:"blocking"`
}
type CfoExpenseImportResult struct {
	Preview     bool             `json:"preview"`
	RowsInFile  int              `json:"rows_in_file"`
	RowsMatched int              `json:"rows_matched"`
	RowsUnknown int              `json:"rows_unknown"`
	CellsSaved  int              `json:"cells_saved"`
	Changes     []CfoExpenseCell `json:"changes,omitempty"`
	Issues      []string         `json:"issues"`
}
type CfoExpenseComment struct {
	CodeCFO     int    `json:"code_cfo"`
	CodePL      int    `json:"code_pl"`
	LegalEntity string `json:"legal_entity"`
	Comment     string `json:"comment"`
}
type CfoExpenseCardStore interface {
	Card(ctx context.Context, id int64) (Card, error)
}
