package plans

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Экспорт/импорт формы МП задания (TPL-06), task-scoped: только площадки/блоки
// задания. Экспорт — .xlsx с фактом+тактикой; импорт — читает тактику обратно и
// сохраняет через SaveMpForm (права/срез задания соблюдаются).

func fmtNum(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// MpFormExport — .xlsx формы задания (Площадка | Код ЦФО | Блок | КодPL | Факт | Тактика).
func (s *TaskStore) MpFormExport(ctx context.Context, taskID int64) ([]byte, error) {
	// Excel-обмен ведётся в валюте хранения (RUB) — так round-trip export→import
	// не зависит от того, в какой валюте пользователь смотрел форму (CHECKPOINT B).
	f, err := s.MpFormData(ctx, taskID, "RUB")
	if err != nil {
		return nil, err
	}
	blockName := map[string]string{}
	blockPL := map[string]int{}
	for _, b := range f.Lines {
		blockName[b.BlockType] = b.Name
		blockPL[b.BlockType] = b.CodePL
	}
	pname := map[int]string{}
	for _, p := range f.Platforms {
		pname[p.CodeCFO] = p.Name
	}
	header := []string{"Площадка", "Код ЦФО", "Блок", "КодPL", "Факт", "Стратегия", "Тактика"}
	rows := make([][]string, 0, len(f.Cells))
	for _, c := range f.Cells {
		rows = append(rows, []string{
			pname[c.CodeCFO], strconv.Itoa(c.CodeCFO), blockName[c.BlockType],
			strconv.Itoa(blockPL[c.BlockType]), fmtNum(c.Fact), fmtNum(c.Strategy), fmtNum(c.Tactic),
		})
	}
	return writeXlsx("МП", header, rows)
}

// MpFormImport читает тактику из .xlsx и сохраняет в задание (SaveMpForm).
func (s *TaskStore) MpFormImport(ctx context.Context, taskID, actorID int64, isAdmin bool, data []byte) (int, error) {
	wb, err := openXLSX(data)
	if err != nil {
		return 0, fmt.Errorf("xlsx: %w", err)
	}
	sheetName := "МП"
	rows, err := wb.Sheet(sheetName)
	if err != nil {
		// fallback — первый лист
		for name := range wb.rel {
			if r, e := wb.Sheet(name); e == nil {
				rows = r
				break
			}
		}
	}
	if len(rows) < 2 {
		return 0, fmt.Errorf("в файле нет строк")
	}
	// Индексы колонок по заголовку.
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	ci := func(name string) int {
		if v, ok := col[strings.ToLower(name)]; ok {
			return v
		}
		return -1
	}
	cfoCol, plCol, tacCol := ci("Код ЦФО"), ci("КодPL"), ci("Тактика")
	if cfoCol < 0 || plCol < 0 || tacCol < 0 {
		return 0, fmt.Errorf("нет колонок «Код ЦФО»/«КодPL»/«Тактика»")
	}
	// code_pl → block_type.
	plToBlock := map[int]string{}
	for _, l := range mpFormSpec() {
		if l.Editable && l.CodePL > 0 {
			plToBlock[l.CodePL] = l.BlockType
		}
	}
	at := func(r []string, i int) string {
		if i >= 0 && i < len(r) {
			return strings.TrimSpace(r[i])
		}
		return ""
	}
	out := make([]MpSaveRow, 0)
	for _, r := range rows[1:] {
		cfo, _ := strconv.Atoi(at(r, cfoCol))
		pl, _ := strconv.Atoi(at(r, plCol))
		ts := strings.ReplaceAll(at(r, tacCol), " ", "")
		ts = strings.ReplaceAll(ts, ",", ".")
		if cfo == 0 || pl == 0 || ts == "" {
			continue
		}
		amt, err := strconv.ParseFloat(ts, 64)
		if err != nil {
			continue
		}
		bt := plToBlock[pl]
		if bt == "" {
			continue
		}
		out = append(out, MpSaveRow{CodeCFO: cfo, BlockType: bt, CodePL: pl, Amount: amt})
	}
	if len(out) == 0 {
		return 0, fmt.Errorf("не найдено строк с тактикой")
	}
	if err := s.SaveMpForm(ctx, taskID, actorID, isAdmin, out, nil, "RUB"); err != nil {
		return 0, err
	}
	return len(out), nil
}

// ImportStrategy грузит стратегический бюджет («МП_стратегия_26») в pl_metric
// (scenario «Стратегический бюджет», amount_strategy) для всей карточки периода.
// Колонки .xlsx: «Код ЦФО», «КодPL», «Стратегия». Сегмент — per-ЦФО из dir_marketplace.
func (s *TaskStore) ImportStrategy(ctx context.Context, plID int64, data []byte) (int, error) {
	wb, err := openXLSX(data)
	if err != nil {
		return 0, fmt.Errorf("xlsx: %w", err)
	}
	rows, err := wb.Sheet("МП")
	if err != nil {
		for name := range wb.rel {
			if r, e := wb.Sheet(name); e == nil {
				rows = r
				break
			}
		}
	}
	if len(rows) < 2 {
		return 0, fmt.Errorf("в файле нет строк")
	}
	var year, month int
	_ = s.pool.QueryRow(ctx, `SELECT period_year, period_month FROM pl_instance WHERE id=$1`, plID).Scan(&year, &month)
	if year == 0 {
		return 0, fmt.Errorf("карточка %d не найдена", plID)
	}
	// Сегмент per-ЦФО из dir_marketplace.
	segByCfo := map[int]string{}
	if mr, e := s.pool.Query(ctx, `
		SELECT (payload_json->>'code_cfo')::int, COALESCE(payload_json->>'segment','')
		FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id
		WHERE d.code='dir_marketplace' AND payload_json->>'code_cfo' ~ '^[0-9]+$'`); e == nil {
		for mr.Next() {
			var c int
			var sg string
			if mr.Scan(&c, &sg) == nil {
				segByCfo[c] = sg
			}
		}
		mr.Close()
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	ci := func(name string) int {
		if v, ok := col[strings.ToLower(name)]; ok {
			return v
		}
		return -1
	}
	cfoCol, plCol, stratCol := ci("Код ЦФО"), ci("КодPL"), ci("Стратегия")
	if cfoCol < 0 || plCol < 0 || stratCol < 0 {
		return 0, fmt.Errorf("нет колонок «Код ЦФО»/«КодPL»/«Стратегия»")
	}
	plToBlock := map[int]string{}
	for _, l := range mpFormSpec() {
		if l.Editable && l.CodePL > 0 {
			plToBlock[l.CodePL] = l.BlockType
		}
	}
	at := func(r []string, i int) string {
		if i >= 0 && i < len(r) {
			return strings.TrimSpace(r[i])
		}
		return ""
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	n := 0
	for _, r := range rows[1:] {
		cfo, _ := strconv.Atoi(at(r, cfoCol))
		pl, _ := strconv.Atoi(at(r, plCol))
		vs := strings.ReplaceAll(strings.ReplaceAll(at(r, stratCol), " ", ""), ",", ".")
		if cfo == 0 || pl == 0 || vs == "" {
			continue
		}
		val, err := strconv.ParseFloat(vs, 64)
		if err != nil {
			continue
		}
		bt := plToBlock[pl]
		if bt == "" {
			continue
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO pl_metric (pl_id, template_code, segment, line_code, block_type, profit_center, country, scenario, period_year, period_month, currency, amount_strategy)
			VALUES ($1,'TPL-MP',$2,$3,$4,$5,'','Стратегический бюджет',$6,$7,'RUB',$8)
			ON CONFLICT (pl_id, template_code, segment, line_code, block_type, profit_center, scenario, period_year, period_month, currency)
			DO UPDATE SET amount_strategy=EXCLUDED.amount_strategy`,
			plID, segByCfo[cfo], pl, bt, cfo, year, month, val)
		if err != nil {
			return 0, err
		}
		n++
	}
	if n == 0 {
		return 0, fmt.Errorf("не найдено строк со стратегией")
	}
	return n, tx.Commit(ctx)
}
