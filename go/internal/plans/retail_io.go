package plans

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Импорт/экспорт Excel формы «Розница» (ТЗ §5). Кодек — тот же stdlib-минимум,
// что у МП (writeXlsx/readXlsx в importexport.go), без внешних зависимостей.
//
// Ключ импорта — CodeCFO + Дата (ТЗ §5). Главное требование: строки вне
// справочника попадают в ОТЧЁТ ОБ ОШИБКАХ, а не игнорируются молча. Поэтому
// импорт не падает на первой плохой строке (как у МП), а собирает все проблемы:
// файл на 375 магазинов иначе пришлось бы чинить по одной строке за прогон.

// retailExportColumns — порядок колонок экспорта. Первые четыре — ключ и
// атрибуты, дальше — по одной колонке на месяц планового периода.
var retailExportColumns = []string{
	"code_cfo", "klient_id", "cfo", "city", "lfl_status", "store_type", "category", "reg_manager",
}

// RetailImportIssue — проблема строки файла.
type RetailImportIssue struct {
	Line    int    `json:"line"`
	CodeCFO int    `json:"code_cfo,omitempty"`
	Message string `json:"message"`
}

// RetailImportResult — отчёт импорта (ТЗ §5).
type RetailImportResult struct {
	RowsInFile int                 `json:"rows_in_file"`
	CellsSaved int                 `json:"cells_saved"`
	Skipped    int                 `json:"skipped"`
	Issues     []RetailImportIssue `json:"issues"`
}

// ExportForm — текущее представление формы в .xlsx (ТЗ §5).
// Экспортируется то же, что видит пользователь: строки его среза, значения в
// валюте отображения и расчётные индикаторы месяца карточки.
func (s *RetailService) ExportForm(ctx context.Context, p Principal, cardID int64, currency string) ([]byte, string, error) {
	form, err := s.Form(ctx, p, cardID, currency)
	if err != nil {
		return nil, "", err
	}
	header := append([]string{}, retailExportColumns...)
	for _, m := range form.Months {
		header = append(header, fmt.Sprintf("%d-%02d", form.Year, m))
	}
	// Индикаторы месяца карточки — read-only колонки: при обратном импорте они
	// игнорируются (в parseRetailImport разбираются только колонки-месяцы).
	header = append(header,
		"факт_ПГ", "факт_пред_мес", "стратегия", "lfl_тактич", "lfm_тактич",
		"к_стратегии", "итого_за_период", "ожидание_года", "комментарий")

	rows := make([][]string, 0, len(form.Rows))
	for _, r := range form.Rows {
		line := []string{
			strconv.Itoa(r.CodeCFO), r.KlientID, r.NameCFO, r.City,
			r.LFLEffective, r.StoreType, r.Category, r.RegManager,
		}
		for _, m := range form.Months {
			line = append(line, fmtOptional(r.valueOf(form.Year, m)))
		}
		ind := r.Indicators
		line = append(line,
			fmtOptional(ind.FactPrevYearMonth), fmtOptional(ind.FactPrevMonth), fmtOptional(ind.Strategy),
			fmtOptional(ind.LFLTactic), fmtOptional(ind.LFMTactic), fmtOptional(ind.VsStrategyPct),
			fmtFloat(ind.PeriodTotal), fmtFloat(ind.YearExpectation), r.Comment)
		rows = append(rows, line)
	}
	data, err := writeXlsx("Розница "+form.Country, header, rows)
	if err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("retail_%s_%d-%02d.xlsx", form.Country, form.Year, form.Month)
	return data, name, nil
}

// ImportForm — импорт значений из .xlsx по ключу CodeCFO + Дата (ТЗ §5).
func (s *RetailService) ImportForm(ctx context.Context, p Principal, cardID int64, data []byte) (RetailImportResult, error) {
	var res RetailImportResult
	a, err := s.access(ctx, cardID, p)
	if err != nil {
		return res, err
	}
	if !cardEditable(a.card) {
		return res, errors.New("период закрыт на запись: импорт недоступен")
	}
	rows, err := s.repo.Rows(ctx, a.inst.ID)
	if err != nil {
		return res, err
	}
	allowed := map[int]bool{}
	for _, r := range rows {
		if a.allowsRow(r) {
			allowed[r.CodeCFO] = true
		}
	}
	planMonths := map[int]bool{}
	for _, m := range retailPlanMonths(a.card.Month) {
		planMonths[m] = true
	}

	cells, issues, inFile, err := parseRetailImport(data, a.card.Year, planMonths, allowed, s.limits[a.card.Country])
	res.RowsInFile, res.Issues = inFile, issues
	if err != nil {
		return res, err
	}
	if res.Issues == nil {
		res.Issues = []RetailImportIssue{}
	}
	if len(cells) == 0 {
		res.Skipped = inFile
		return res, nil
	}
	saved, err := s.repo.SaveValues(ctx, a.inst.ID, cells, p.UserID, "import")
	if err != nil {
		return res, err
	}
	res.CellsSaved = saved
	res.Skipped = len(issues)
	s.rec(ctx, p.UserID, "retail_import", cardID)
	return res, nil
}

// parseRetailImport — разбор файла. Колонки-месяцы распознаются по заголовку
// вида "2026-07" (год-месяц): это и есть «Дата» из ключа ТЗ §5. Возвращает
// ячейки к записи и ПОЛНЫЙ список проблем — импорт частичный, но объяснимый.
func parseRetailImport(data []byte, year int, planMonths, allowed map[int]bool, limit float64) (
	[]RetailCellWrite, []RetailImportIssue, int, error) {
	header, lines, err := readXlsx(data)
	if err != nil {
		return nil, nil, 0, err
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(strings.ToLower(h))] = i
	}
	codeIdx, ok := col["code_cfo"]
	if !ok {
		return nil, nil, 0, errors.New("в файле нет колонки code_cfo (ключ строки — CodeCFO + Дата)")
	}
	// Колонки-месяцы: "YYYY-MM".
	type monthCol struct {
		idx   int
		year  int
		month int
	}
	months := make([]monthCol, 0, 12)
	for h, i := range col {
		y, m, ok := parseYearMonthHeader(h)
		if !ok {
			continue
		}
		months = append(months, monthCol{idx: i, year: y, month: m})
	}
	if len(months) == 0 {
		return nil, nil, 0, errors.New("в файле нет колонок-месяцев вида «2026-07»")
	}

	cells := make([]RetailCellWrite, 0, len(lines)*len(months))
	issues := make([]RetailImportIssue, 0)
	for n, line := range lines {
		ln := n + 2 // +1 заголовок, +1 в 1-индексацию
		if codeIdx >= len(line) || strings.TrimSpace(line[codeIdx]) == "" {
			continue // полностью пустая строка — не ошибка
		}
		code, err := strconv.Atoi(strings.TrimSpace(line[codeIdx]))
		if err != nil {
			issues = append(issues, RetailImportIssue{Line: ln, Message: "code_cfo не число"})
			continue
		}
		// Строка вне справочника/среза — в отчёт, а не молча мимо (ТЗ §5).
		if !allowed[code] {
			issues = append(issues, RetailImportIssue{Line: ln, CodeCFO: code,
				Message: fmt.Sprintf("магазина %d нет в форме или он вне вашего среза доступа", code)})
			continue
		}
		for _, mc := range months {
			if mc.idx >= len(line) {
				continue
			}
			raw := strings.TrimSpace(line[mc.idx])
			if raw == "" {
				continue // пусто в файле = «не меняем ячейку» (очистка — отдельное действие)
			}
			if mc.year != year || !planMonths[mc.month] {
				issues = append(issues, RetailImportIssue{Line: ln, CodeCFO: code,
					Message: fmt.Sprintf("колонка %d-%02d вне планового периода", mc.year, mc.month)})
				continue
			}
			v, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
			if err != nil {
				issues = append(issues, RetailImportIssue{Line: ln, CodeCFO: code,
					Message: fmt.Sprintf("значение %q за %d-%02d не число", raw, mc.year, mc.month)})
				continue
			}
			if v < 0 {
				issues = append(issues, RetailImportIssue{Line: ln, CodeCFO: code,
					Message: fmt.Sprintf("значение за %d-%02d отрицательное (V-02)", mc.year, mc.month)})
				continue
			}
			if limit > 0 && v > limit {
				issues = append(issues, RetailImportIssue{Line: ln, CodeCFO: code,
					Message: fmt.Sprintf("значение %.2f за %d-%02d выше допустимой границы %.2f (V-02)",
						v, mc.year, mc.month, limit)})
				continue
			}
			amount := v
			cells = append(cells, RetailCellWrite{
				CodeCFO: code, Metric: MetricSales, Year: mc.year, Month: mc.month,
				Amount: &amount, Source: ValueImport, Note: "импорт из Excel",
			})
		}
	}
	return cells, issues, len(lines), nil
}

// parseYearMonthHeader — заголовок колонки-месяца «2026-07».
func parseYearMonthHeader(h string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(h), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	y, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || y < 2000 || y > 2100 || m < 1 || m > 12 {
		return 0, 0, false
	}
	return y, m, true
}

func fmtOptional(v *float64) string {
	if v == nil {
		return ""
	}
	return fmtFloat(*v)
}

func fmtFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
