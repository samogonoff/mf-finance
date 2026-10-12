package plans

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

func (s *CfoExpenseService) Export(ctx context.Context, p Principal, id int64, f CfoExpenseFilter) ([]byte, string, error) {
	form, e := s.Form(ctx, p, id, f)
	if e != nil {
		return nil, "", e
	}
	h := []string{"code_cfo", "cfo", "code_pl", "legal_entity", "group_pl", "expense", "country_company"}
	for m := 1; m <= 12; m++ {
		h = append(h, fmt.Sprintf("%d-%02d", form.Config.PlanYear, m))
	}
	rows := [][]string{}
	for _, r := range form.Rows {
		line := []string{fmt.Sprint(r.CodeCFO), r.CFO, fmt.Sprint(r.CodePL), r.LegalEntity, r.GroupPL, r.Expense, r.CountryCompany}
		for m := 1; m <= 12; m++ {
			value := ""
			for _, v := range r.Values {
				if v.Scenario == "plan" && v.Year == form.Config.PlanYear && v.Month == m && v.Amount != nil {
					value = strconv.FormatFloat(*v.Amount, 'f', 2, 64)
					break
				}
			}
			line = append(line, value)
		}
		rows = append(rows, line)
	}
	b, e := writeXlsx("CFO expenses", h, rows)
	return b, fmt.Sprintf("cfo_exp_%d.xlsx", form.Config.Code), e
}

// Import reads the same key-based format produced by Export. Unknown rows are
// reported and never create new grid rows. Preview performs no writes.
func (s *CfoExpenseService) Import(ctx context.Context, p Principal, id int64, data []byte, preview bool) (CfoExpenseImportResult, error) {
	form, err := s.Form(ctx, p, id, CfoExpenseFilter{Limit: 50000})
	if err != nil {
		return CfoExpenseImportResult{}, err
	}
	wb, err := openXLSX(data)
	if err != nil {
		return CfoExpenseImportResult{}, err
	}
	rows, err := wb.Sheet("CFO expenses")
	if err != nil {
		return CfoExpenseImportResult{}, err
	}
	result := CfoExpenseImportResult{Preview: preview}
	if len(rows) == 0 {
		return result, nil
	}
	byKey := map[string]CfoExpenseRow{}
	for _, r := range form.Rows {
		byKey[fmt.Sprintf("%d:%d:%s", r.CodeCFO, r.CodePL, r.LegalEntity)] = r
	}
	for lineNo, line := range rows[1:] {
		result.RowsInFile++
		if len(line) < 3 {
			result.Issues = append(result.Issues, fmt.Sprintf("строка %d: недостаточно колонок", lineNo+2))
			continue
		}
		codeCFO, _ := strconv.Atoi(strings.TrimSpace(line[0]))
		codePL, _ := strconv.Atoi(strings.TrimSpace(line[2]))
		legal := ""
		if len(line) > 3 {
			legal = strings.TrimSpace(line[3])
		}
		row, ok := byKey[fmt.Sprintf("%d:%d:%s", codeCFO, codePL, legal)]
		if !ok {
			result.RowsUnknown++
			result.Issues = append(result.Issues, fmt.Sprintf("строка %d: ЦФО %d, статья %d, ЮЛ %q не найдены", lineNo+2, codeCFO, codePL, legal))
			continue
		}
		result.RowsMatched++
		for m := 1; m <= 12; m++ {
			col := 6 + m
			if len(line) <= col {
				break
			}
			raw := strings.TrimSpace(line[col])
			var amount *float64
			if raw != "" {
				v, e := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
				if e != nil {
					result.Issues = append(result.Issues, fmt.Sprintf("строка %d, месяц %d: не число", lineNo+2, m))
					continue
				}
				amount = &v
			}
			result.Changes = append(result.Changes, CfoExpenseCell{CodeCFO: codeCFO, CodePL: codePL, LegalEntity: legal, Scenario: "plan", Year: form.Config.PlanYear, Month: m, Amount: amount})
		}
		_ = row
	}
	if preview || len(result.Issues) > 0 {
		return result, nil
	}
	saved, err := s.Save(ctx, p, id, result.Changes, nil)
	result.CellsSaved = saved
	return result, err
}
