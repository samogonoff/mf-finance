package plans

import (
	"context"
	"encoding/json"
	"fmt"
)

// Новая форма ввода МП — TASK-DRIVEN: площадки берутся из ЦФО задания (а не из
// сегмента целиком). Исполнитель/делегат вводит тактику по блокам × площадкам;
// факт read-only; сохранение → pl_metric (scenario «Тактика бюджет») +
// form_submission (снимок). Авторизация — assignee/delegate/admin.

// MpFormPlatform — площадка МП (ЦФО задания).
type MpFormPlatform struct {
	CodeCFO int    `json:"code_cfo"`
	Name    string `json:"name"`
}

// MpFormBlockDef — блок метрик формы (строка ввода).
type MpFormBlockDef struct {
	BlockType string `json:"block_type"`
	CodePL    int    `json:"code_pl"`
	Name      string `json:"name"`
}

// MpFormCell — введённое значение (тактика) + факт по (площадка × блок).
type MpFormCell struct {
	CodeCFO   int      `json:"code_cfo"`
	BlockType string   `json:"block_type"`
	Fact      *float64 `json:"fact"`
	Strategy  *float64 `json:"strategy"` // стратегический бюджет (read-only, для копирования TPL-09)
	Tactic    *float64 `json:"tactic"`
	IsManual  bool     `json:"is_manual"`
	Reason    string   `json:"reason"` // причина корректировки (ADJ-02), если is_manual
}

// MpTaskForm — структура формы задания МП.
type MpTaskForm struct {
	Task      Task             `json:"task"`
	Segment   string           `json:"segment"`
	Year      int              `json:"year"`
	Month     int              `json:"month"`
	Currency  string           `json:"currency"`
	Platforms []MpFormPlatform `json:"platforms"`
	Blocks    []MpFormBlockDef `json:"blocks"`
	Cells     []MpFormCell     `json:"cells"`
}

func mpFormBlocks() []MpFormBlockDef {
	out := make([]MpFormBlockDef, 0)
	for _, b := range mpEditableBlocks() {
		out = append(out, MpFormBlockDef{BlockType: b.BlockType, CodePL: b.CodePL, Name: b.Name})
	}
	return out
}

// taskBrief читает задание (cfo_codes, pl_id, форма) + период/сегмент.
func (s *TaskStore) taskBrief(ctx context.Context, taskID int64) (Task, int, int, string, error) {
	var t Task
	var raw []byte
	err := s.pool.QueryRow(ctx, taskSelect+` WHERE t.id=$1`, taskID).Scan(
		&t.ID, &t.PlID, &t.StageCode, &t.FormCode, &t.Title, &raw, &t.Role,
		&t.PositionID, &t.LegalEntity, &t.AssigneeID, &t.AssigneeName,
		&t.DelegateID, &t.DelegateName, &t.Status)
	if err != nil {
		return t, 0, 0, "", err
	}
	_ = json.Unmarshal(raw, &t.CfoCodes)
	t.CfoCount = len(t.CfoCodes)
	var year, month int
	_ = s.pool.QueryRow(ctx, `SELECT period_year, period_month FROM pl_instance WHERE id=$1`, t.PlID).Scan(&year, &month)
	// Сегмент — по первой площадке из dir_marketplace.
	var segment string
	if len(t.CfoCodes) > 0 {
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(payload_json->>'segment','') FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id
			WHERE d.code='dir_marketplace' AND (payload_json->>'code_cfo')::int = $1 LIMIT 1`, t.CfoCodes[0]).Scan(&segment)
	}
	return t, year, month, segment, nil
}

// MpFormData собирает форму МП задания (площадки = ЦФО задания, факт + тактика).
func (s *TaskStore) MpFormData(ctx context.Context, taskID int64) (MpTaskForm, error) {
	var f MpTaskForm
	t, year, month, segment, err := s.taskBrief(ctx, taskID)
	if err != nil {
		return f, err
	}
	f.Task, f.Year, f.Month, f.Segment, f.Currency = t, year, month, segment, "RUB"
	f.Blocks = mpFormBlocks()

	// Площадки = ЦФО задания, имена из dir_marketplace (fallback dir_cfo).
	mpNames := s.nameMap(ctx, "dir_marketplace", "code_cfo", "name_cfo")
	cfoNames := s.nameMap(ctx, "dir_cfo", "code_cfo", "name_cfo")
	for _, c := range t.CfoCodes {
		name := mpNames[c]
		if name == "" {
			name = cfoNames[c]
		}
		f.Platforms = append(f.Platforms, MpFormPlatform{CodeCFO: c, Name: name})
	}

	// Факт из источника МП (read-only): индекс по (code_cfo, code_pl).
	factIdx := map[[2]int]float64{}
	if s.fact != nil && len(t.CfoCodes) > 0 {
		if fr, e := s.fact.MpFact(ctx, year, month, segment); e == nil {
			allowed := map[int]bool{}
			for _, c := range t.CfoCodes {
				allowed[c] = true
			}
			for _, r := range fr {
				if allowed[r.CodeCFO] {
					factIdx[[2]int{r.CodeCFO, r.CodePL}] = r.Amount
				}
			}
		}
	}
	// Сохранённая тактика + стратегия из pl_metric.
	type tac struct {
		v        float64
		isManual bool
	}
	tacByKey := map[string]tac{}
	stratByKey := map[string]float64{}
	if len(t.CfoCodes) > 0 {
		rows, err := s.pool.Query(ctx, `
			SELECT profit_center, block_type, amount, is_manual, amount_strategy
			FROM pl_metric WHERE pl_id=$1 AND template_code='TPL-MP' AND profit_center = ANY($2)`, t.PlID, t.CfoCodes)
		if err == nil {
			for rows.Next() {
				var pc int
				var bt string
				var amt *float64
				var im bool
				var strat *float64
				if rows.Scan(&pc, &bt, &amt, &im, &strat) == nil {
					k := fmt.Sprintf("%d:%s", pc, bt)
					if amt != nil {
						tacByKey[k] = tac{*amt, im}
					}
					if strat != nil {
						stratByKey[k] = *strat
					}
				}
			}
			rows.Close()
		}
	}
	// Причины корректировок из pl_adjustment (последняя по ячейке).
	reasonByKey := map[string]string{}
	if len(t.CfoCodes) > 0 {
		ar, e := s.pool.Query(ctx, `
			SELECT profit_center, block_type, reason FROM pl_adjustment
			WHERE pl_id=$1 AND profit_center = ANY($2) ORDER BY adjusted_at`, t.PlID, t.CfoCodes)
		if e == nil {
			for ar.Next() {
				var pc int
				var bt, rs string
				if ar.Scan(&pc, &bt, &rs) == nil {
					reasonByKey[fmt.Sprintf("%d:%s", pc, bt)] = rs
				}
			}
			ar.Close()
		}
	}
	// Полная сетка площадка × блок: факт (источник) + тактика (pl_metric) + причина.
	for _, p := range f.Platforms {
		for _, b := range f.Blocks {
			k := fmt.Sprintf("%d:%s", p.CodeCFO, b.BlockType)
			cell := MpFormCell{CodeCFO: p.CodeCFO, BlockType: b.BlockType}
			if v, ok := factIdx[[2]int{p.CodeCFO, b.CodePL}]; ok {
				vv := v
				cell.Fact = &vv
			}
			if sv, ok := stratByKey[k]; ok {
				vv := sv
				cell.Strategy = &vv
			}
			if tv, ok := tacByKey[k]; ok {
				vv := tv.v
				cell.Tactic = &vv
				cell.IsManual = tv.isManual
			}
			if cell.IsManual {
				cell.Reason = reasonByKey[k]
			}
			f.Cells = append(f.Cells, cell)
		}
	}
	return f, nil
}

// MpSaveRow — строка сохранения формы МП задания.
type MpSaveRow struct {
	CodeCFO   int     `json:"code_cfo"`
	BlockType string  `json:"block_type"`
	CodePL    int     `json:"code_pl"`
	Amount    float64 `json:"amount"`
	IsManual  bool    `json:"is_manual"`
	Comment   string  `json:"comment"`
}

// SaveMpForm сохраняет тактику задания МП в pl_metric + снимок form_submission.
// Права: assignee/delegate/admin. ЦФО вне задания отклоняются.
func (s *TaskStore) SaveMpForm(ctx context.Context, taskID, actorID int64, isAdmin bool, rows []MpSaveRow, payload []byte) error {
	t, year, month, segment, err := s.taskBrief(ctx, taskID)
	if err != nil {
		return err
	}
	// Право: исполнитель/делегат/админ.
	if !isAdmin && !(t.AssigneeID != nil && *t.AssigneeID == actorID) && !(t.DelegateID != nil && *t.DelegateID == actorID) {
		return fmt.Errorf("нет прав на заполнение задания")
	}
	allowed := map[int]bool{}
	for _, c := range t.CfoCodes {
		allowed[c] = true
	}
	// Факт для diff корректировки (original = факт, ADJ-05).
	factIdx := map[[2]int]float64{}
	if s.fact != nil {
		if fr, e := s.fact.MpFact(ctx, year, month, segment); e == nil {
			for _, r := range fr {
				if allowed[r.CodeCFO] {
					factIdx[[2]int{r.CodeCFO, r.CodePL}] = r.Amount
				}
			}
		}
	}
	const scenario = "Тактика бюджет (таргеты)"
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, r := range rows {
		if !allowed[r.CodeCFO] {
			return fmt.Errorf("ЦФО %d вне задания", r.CodeCFO)
		}
		if r.IsManual && r.Comment == "" {
			return fmt.Errorf("причина корректировки обязательна (ADJ-02)")
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO pl_metric (pl_id, template_code, segment, line_code, block_type, profit_center, country, scenario, period_year, period_month, currency, amount, is_manual)
			VALUES ($1,'TPL-MP',$2,$3,$4,$5,'',$6,$7,$8,'RUB',$9,$10)
			ON CONFLICT (pl_id, template_code, segment, line_code, block_type, profit_center, scenario, period_year, period_month, currency)
			DO UPDATE SET amount=EXCLUDED.amount, is_manual=EXCLUDED.is_manual`,
			t.PlID, segment, r.CodePL, r.BlockType, r.CodeCFO, scenario, year, month, r.Amount, r.IsManual)
		if err != nil {
			return err
		}
		if r.IsManual {
			var orig *float64
			if v, ok := factIdx[[2]int{r.CodeCFO, r.CodePL}]; ok {
				vv := v
				orig = &vv
			}
			_, _ = tx.Exec(ctx, `
				INSERT INTO pl_adjustment (pl_id, profit_center, line_code, block_type, period_year, period_month, currency, original_calculated, adjusted_value, reason, adjusted_by, adjusted_at)
				VALUES ($1,$2,$3,$4,$5,$6,'RUB',$7,$8,$9,$10,NOW())`,
				t.PlID, r.CodeCFO, r.CodePL, r.BlockType, year, month, orig, r.Amount, r.Comment, actorID)
		}
	}
	// Снимок формы.
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	_, _ = tx.Exec(ctx, `INSERT INTO form_submission (pl_id, template_code, json_payload, submitted_by, submitted_at) VALUES ($1,'TPL-MP',$2,$3,NOW())`, t.PlID, payload, actorID)
	// Задание двинулось в работу.
	_, _ = tx.Exec(ctx, `UPDATE pl_task SET status=CASE WHEN status='pending' THEN 'in_progress' ELSE status END, updated_at=NOW() WHERE id=$1`, taskID)
	return tx.Commit(ctx)
}
