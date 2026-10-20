package plans

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Экспорт и импорт формы этапа 1.5 (ТЗ §7.9). Кодек тот же stdlib-минимум
// (writeXlsx/readXlsx в importexport.go), без внешних зависимостей.
//
// Требования ТЗ, которые определяют устройство:
//   - экспорт выгружает ТЕКУЩУЮ ВЫБОРКУ с учётом фильтров, то есть ровно то, что
//     на экране, — поэтому он берёт тот же ExpFilter, что и форма;
//   - импорт сопоставляет строки по ключу «код ЦФО + код PL», а НЕ по номеру
//     строки: файл правят руками, строки в нём переставляют и удаляют;
//   - строк, которых нет в форме, импорт НЕ СОЗДАЁТ — они выводятся отдельным
//     списком непринятых. Состав строк задаёт источник затрат, а не файл.

// expExportColumns — атрибуты строки в экспорте, в порядке колонок формы.
var expExportColumns = []string{
	"code_cfo", "cfo", "code_pl", "expense", "group_pl", "group_cfo2", "legal_entity",
}

// ExpImportIssue — строка файла, которую не удалось принять.
type ExpImportIssue struct {
	Line    int    `json:"line"`
	CodeCFO int    `json:"code_cfo,omitempty"`
	CodePL  int    `json:"code_pl,omitempty"`
	Message string `json:"message"`
}

// ExpImportChange — что изменится в ячейке (для предпросмотра ТЗ §7.9).
type ExpImportChange struct {
	CodeCFO int      `json:"code_cfo"`
	CodePL  int      `json:"code_pl"`
	Month   int      `json:"month"`
	Old     *float64 `json:"old"`
	New     *float64 `json:"new"`
}

// ExpImportResult — отчёт импорта. В режиме предпросмотра ничего не сохраняется:
// ТЗ §7.9 требует показать «сколько строк будет обновлено, сколько не найдено по
// ключу, какие значения изменятся» ДО применения.
type ExpImportResult struct {
	Preview     bool              `json:"preview"`
	RowsInFile  int               `json:"rows_in_file"`
	RowsMatched int               `json:"rows_matched"`
	RowsUnknown int               `json:"rows_unknown"`
	CellsSaved  int               `json:"cells_saved"`
	Changes     []ExpImportChange `json:"changes,omitempty"`
	Issues      []ExpImportIssue  `json:"issues"`
}

// ExportForm — текущая выборка формы в .xlsx (ТЗ §7.9).
func (s *ExpService) ExportForm(ctx context.Context, p Principal, cardID int64, f ExpFilter) ([]byte, string, error) {
	if f.Limit <= 0 {
		f.Limit = 5000
	}
	form, err := s.Form(ctx, p, cardID, f)
	if err != nil {
		return nil, "", err
	}
	header := append([]string{}, expExportColumns...)
	for m := 1; m <= 12; m++ {
		header = append(header, fmt.Sprintf("%d-%02d", form.Instance.PlanYear, m))
	}

	rows := make([][]string, 0, len(form.Rows))
	for _, r := range form.Rows {
		line := []string{
			strconv.Itoa(r.CodeCFO), r.CFO, strconv.Itoa(r.CodePL), r.Expense,
			r.GroupPL, r.GroupCFO2, r.LegalEntity,
		}
		for m := 1; m <= 12; m++ {
			line = append(line, expCellText(r, m))
		}
		rows = append(rows, line)
	}
	buf, err := writeXlsx("Расходы ЦП", header, rows)
	if err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("expenses_%s_%d.xlsx", form.Instance.Country, form.Instance.PlanYear)
	return buf, name, nil
}

// ImportTemplate — файл-пример импорта с ОДНОЙ заполненной строкой (ТЗ §7.9).
// Берём первую строку формы: пример на выдуманных кодах ЦФО бесполезен — его
// нельзя загрузить обратно.
func (s *ExpService) ImportTemplate(ctx context.Context, p Principal, cardID int64) ([]byte, string, error) {
	form, err := s.Form(ctx, p, cardID, ExpFilter{Limit: 1})
	if err != nil {
		return nil, "", err
	}
	header := append([]string{}, expExportColumns...)
	for m := 1; m <= 12; m++ {
		header = append(header, fmt.Sprintf("%d-%02d", form.Instance.PlanYear, m))
	}
	rows := [][]string{}
	if len(form.Rows) > 0 {
		r := form.Rows[0]
		line := []string{
			strconv.Itoa(r.CodeCFO), r.CFO, strconv.Itoa(r.CodePL), r.Expense,
			r.GroupPL, r.GroupCFO2, r.LegalEntity,
		}
		for m := 1; m <= 12; m++ {
			line = append(line, "1000")
		}
		rows = append(rows, line)
	}
	buf, err := writeXlsx("Пример импорта", header, rows)
	if err != nil {
		return nil, "", err
	}
	return buf, "expenses_import_example.xlsx", nil
}

// expCellText — значение плановой ячейки для файла. Пустая ячейка остаётся
// пустой: экспорт, подставляющий нули, при обратном импорте заполнил бы форму
// нулями, и E-11 перестал бы что-либо ловить.
func expCellText(r ExpRow, month int) string {
	for _, v := range r.Values {
		if v.Scenario == ExpScenarioPlan && v.Month == month && v.Amount != nil {
			return strconv.FormatFloat(*v.Amount, 'f', 2, 64)
		}
	}
	return ""
}

// ImportForm — импорт файла. preview=true ничего не сохраняет.
func (s *ExpService) ImportForm(ctx context.Context, p Principal, cardID int64, data []byte, preview bool) (ExpImportResult, error) {
	res := ExpImportResult{Preview: preview, Issues: []ExpImportIssue{}}
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return res, err
	}
	if !preview && !cardEditable(c) {
		return res, fmt.Errorf("период закрыт на запись: импорт недоступен")
	}
	header, table, err := readXlsx(data)
	if err != nil {
		return res, err
	}
	if len(table) == 0 {
		return res, fmt.Errorf("в файле нет строк данных")
	}

	monthCol, err := expMonthColumns(header, in.PlanYear)
	if err != nil {
		return res, err
	}

	known, byName, err := s.rowIndex(ctx, in.ID)
	if err != nil {
		return res, err
	}

	cells := make([]ExpCell, 0, 256)
	pastSkipped := 0
	for i, line := range table {
		lineNo := i + 2
		res.RowsInFile++
		key, err := expRowKey(line)
		if err != nil {
			res.Issues = append(res.Issues, ExpImportIssue{Line: lineNo, Message: err.Error()})
			continue
		}
		cfo, pl := key.CodeCFO, key.CodePL
		cur, ok := known[[2]int{cfo, pl}]
		if !ok && key.Name != "" {
			// Сопоставление по наименованию ЦФО (ответ И-8): так устроен лист МП_UZ.
			if m, found := byName[expNameKey{strings.ToLower(key.Name), pl}]; found {
				cfo, cur, ok = m.codeCFO, m.months, true
			}
		}
		if !ok {
			// Строки, которых нет в форме, НЕ создаются (ТЗ §7.9).
			res.RowsUnknown++
			res.Issues = append(res.Issues, ExpImportIssue{
				Line: lineNo, CodeCFO: cfo, CodePL: pl,
				Message: "строки с таким ЦФО и статьёй в форме нет — не принято",
			})
			continue
		}
		res.RowsMatched++
		for month, col := range monthCol {
			if col >= len(line) {
				continue
			}
			raw := strings.TrimSpace(line[col])
			if raw == "" {
				continue // пустая клетка файла ячейку не трогает
			}
			if expPastMonth(c, month) {
				pastSkipped++ // прошедший месяц: по нему факт, значение файла не принимается
				continue
			}
			v, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(raw, " ", ""), ",", "."), 64)
			if err != nil {
				res.Issues = append(res.Issues, ExpImportIssue{
					Line: lineNo, CodeCFO: cfo, CodePL: pl,
					Message: fmt.Sprintf("месяц %d: «%s» не число", month, raw),
				})
				continue
			}
			old := cur[month]
			if old != nil && *old == v {
				continue // значение не меняется — в отчёт не попадает
			}
			nv := v
			res.Changes = append(res.Changes, ExpImportChange{
				CodeCFO: cfo, CodePL: pl, Month: month, Old: old, New: &nv,
			})
			cells = append(cells, ExpCell{
				CodeCFO: cfo, CodePL: pl, Month: month, Amount: &nv, Source: ExpSourceImport,
			})
		}
	}

	if pastSkipped > 0 {
		res.Issues = append(res.Issues, ExpImportIssue{
			Message: fmt.Sprintf("значения за прошедшие месяцы не приняты (ячеек: %d): по ним подтягивается факт", pastSkipped),
		})
	}
	if preview {
		return res, nil
	}
	n, err := s.saveValues(ctx, c, in, p, cells)
	if err != nil {
		return res, err
	}
	res.CellsSaved = n
	return res, nil
}

// expNameKey — ключ сопоставления по наименованию ЦФО (ответ И-8).
type expNameKey struct {
	name   string
	codePL int
}

type expNameRow struct {
	codeCFO int
	months  map[int]*float64
}

// rowIndex — текущие значения формы по ключу «ЦФО + статья» и по ключу
// «наименование + статья». Первый нужен для предпросмотра «было → стало», второй
// — для листов без колонки CodeCFO (ответ И-8).
func (s *ExpService) rowIndex(ctx context.Context, instanceID int64) (
	map[[2]int]map[int]*float64, map[expNameKey]expNameRow, error) {
	rows, err := expAllRows(ctx, s.repo, instanceID)
	if err != nil {
		return nil, nil, err
	}
	out := make(map[[2]int]map[int]*float64, len(rows))
	byName := make(map[expNameKey]expNameRow, len(rows))
	for _, r := range rows {
		m := map[int]*float64{}
		for _, v := range r.Values {
			if v.Scenario == ExpScenarioPlan {
				m[v.Month] = v.Amount
			}
		}
		out[[2]int{r.CodeCFO, r.CodePL}] = m
		if r.CFO != "" {
			byName[expNameKey{strings.ToLower(r.CFO), r.CodePL}] = expNameRow{codeCFO: r.CodeCFO, months: m}
		}
	}
	return out, byName, nil
}

// expMonthColumns — какие колонки файла соответствуют месяцам планового года.
func expMonthColumns(header []string, year int) (map[int]int, error) {
	out := map[int]int{}
	for i, h := range header {
		h = strings.TrimSpace(h)
		for m := 1; m <= 12; m++ {
			if h == fmt.Sprintf("%d-%02d", year, m) {
				out[m] = i
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("в шапке файла нет колонок месяцев вида %d-01…%d-12", year, year)
	}
	return out, nil
}

// expFileKey — ключ строки файла: код ЦФО ЛИБО наименование ЦФО, плюс код PL.
type expFileKey struct {
	CodeCFO int
	Name    string
	CodePL  int
}

// expRowKey — ключ строки файла (ТЗ §7.9).
//
// Ответ И-8 (11.09.2026): в листе МП_UZ колонки CodeCFO НЕТ — привязка идёт по
// названию ЦФО. Поэтому ключ допускает оба варианта: если в колонке кода не
// число, берём наименование. Отказываться от строки нельзя — иначе узбекская
// карточка не импортируется вовсе.
func expRowKey(line []string) (expFileKey, error) {
	if len(line) < 3 {
		return expFileKey{}, fmt.Errorf("в строке меньше трёх колонок — ключ не прочитать")
	}
	pl, err := strconv.Atoi(strings.TrimSpace(line[2]))
	if err != nil || pl <= 0 {
		return expFileKey{}, fmt.Errorf("код PL «%s» не число", strings.TrimSpace(line[2]))
	}
	raw := strings.TrimSpace(line[0])
	if cfo, err := strconv.Atoi(raw); err == nil && cfo > 0 {
		return expFileKey{CodeCFO: cfo, CodePL: pl}, nil
	}
	// Кода нет — пробуем наименование из первой или второй колонки.
	name := raw
	if name == "" && len(line) > 1 {
		name = strings.TrimSpace(line[1])
	}
	if name == "" {
		return expFileKey{}, fmt.Errorf("ни кода ЦФО, ни наименования — строку не сопоставить")
	}
	return expFileKey{Name: name, CodePL: pl}, nil
}
