package plans

import (
	"context"
	"encoding/json"
	"fmt"
)

// Форма ввода МП — TASK-DRIVEN и ПОЛНАЯ (весь каскад как в прототипе large):
// площадки = ЦФО задания, строки = mpFormSpec() (продажи→СПП→наценка→маржа→COGS→
// прямые затраты→PL). Ввод — 1046 + %СПП + себестоимость + статьи затрат; всё
// производное считает computeMpPlatform (сервер) и зеркалит клиент (живой пересчёт).
// Факт/стратегия — read-only. Права: assignee/delegate/admin.

// MpFormPlatform — площадка МП (ЦФО задания).
type MpFormPlatform struct {
	CodeCFO int    `json:"code_cfo"`
	Name    string `json:"name"`
	Country string `json:"country"`
}

// MpFormCell — ячейка (площадка × строка): вычисленное значение + ввод/факт/стратегия.
type MpFormCell struct {
	CodeCFO   int      `json:"code_cfo"`
	BlockType string   `json:"block_type"`
	Value     float64  `json:"value"`             // вычисленное значение каскада (отображение)
	Fact      *float64 `json:"fact"`              // из источника (для input-строк)
	Strategy  *float64 `json:"strategy"`          // стратегический бюджет (read-only)
	Tactic    *float64 `json:"tactic"`            // сохранённая тактика (editable); nil если нет
	IsManual  bool     `json:"is_manual"`         // ручная корректировка (ADJ-02)
	Reason    string   `json:"reason,omitempty"`  // причина корректировки
}

// MpTaskForm — структура формы задания МП.
type MpTaskForm struct {
	Task      Task             `json:"task"`
	Segment   string           `json:"segment"`
	Year      int              `json:"year"`
	Month     int              `json:"month"`
	Currency  string           `json:"currency"`
	Vat       float64          `json:"vat"`
	Platforms []MpFormPlatform `json:"platforms"`
	Lines     []MpLine         `json:"lines"`
	Cells     []MpFormCell     `json:"cells"`
}

// seedPL — code_pl для подтяжки input-строк из источника (SUMIFS прототипа).
var seedPL = map[string]int{
	BSalesManagerGross: 1046,
	BShipments:         8006,
	BCostAgent:         64,
	BCostFreight:       51,
	BCostLogTransport:  52,
	BCostLogWarehouse:  54,
	BCostAds:           13,
	BCostAdsSocial:     15,
	BCostPackaging:     45,
	BCostAcquiring:     58,
	BCostIT:            48,
	BCostOther:         66,
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
	var segment string
	if len(t.CfoCodes) > 0 {
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(payload_json->>'segment','') FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id
			WHERE d.code='dir_marketplace' AND (payload_json->>'code_cfo')::int = $1 LIMIT 1`, t.CfoCodes[0]).Scan(&segment)
	}
	return t, year, month, segment, nil
}

// factSeed — сумма из источника для input-строки (block) по площадке (cfo).
func factSeed(factIdx map[[2]int]float64, block string, cfo int) (float64, bool) {
	switch block {
	case BCogsTotal:
		a, ok1 := factIdx[[2]int{cfo, 2006}]
		b, ok2 := factIdx[[2]int{cfo, 6006}]
		if !ok1 && !ok2 {
			return 0, false
		}
		return a + b, true
	case BCostPenalties:
		v, ok := factIdx[[2]int{cfo, mpPenaltyPL}] // штрафы FINDWH (FINDWHACCESSGROUP)
		return v, ok
	default:
		pl, has := seedPL[block]
		if !has {
			return 0, false
		}
		v, ok := factIdx[[2]int{cfo, pl}]
		return v, ok
	}
}

// deriveSPP — начальный %СПП из источника (если есть цены площадки и менеджера).
func deriveSPP(factIdx map[[2]int]float64, cfo int) float64 {
	if net, ok := factIdx[[2]int{cfo, 1006}]; ok {
		if mgr, ok2 := factIdx[[2]int{cfo, 1045}]; ok2 && mgr != 0 {
			return 1 - net/mgr
		}
	}
	if g, ok := factIdx[[2]int{cfo, 1022}]; ok {
		if mgr, ok2 := factIdx[[2]int{cfo, 1046}]; ok2 && mgr != 0 {
			return 1 - g/mgr
		}
	}
	return 0
}

// MpFormData собирает полную форму МП задания (каскад по спеке).
func (s *TaskStore) MpFormData(ctx context.Context, taskID int64) (MpTaskForm, error) {
	var f MpTaskForm
	t, year, month, segment, err := s.taskBrief(ctx, taskID)
	if err != nil {
		return f, err
	}
	f.Task, f.Year, f.Month, f.Segment, f.Currency = t, year, month, segment, "RUB"
	f.Lines = mpFormSpec()

	// Площадки задания + страна из dir_marketplace seed.
	countryOf := map[int]string{}
	for _, m := range MarketplaceSeed() {
		countryOf[m.CodeCFO] = m.Country
	}
	mpNames := s.nameMap(ctx, "dir_marketplace", "code_cfo", "name_cfo")
	cfoNames := s.nameMap(ctx, "dir_cfo", "code_cfo", "name_cfo")
	for _, c := range t.CfoCodes {
		name := mpNames[c]
		if name == "" {
			name = cfoNames[c]
		}
		f.Platforms = append(f.Platforms, MpFormPlatform{CodeCFO: c, Name: name, Country: countryOf[c]})
	}
	// НДС шапки — по стране первой площадки (для отображения; расчёт — per-площадка).
	if len(f.Platforms) > 0 {
		f.Vat = vatByCountry(f.Platforms[0].Country)
	} else {
		f.Vat = 0.20
	}

	// Факт из источника: индекс (code_cfo, code_pl) → сумма.
	factIdx := map[[2]int]float64{}
	if s.fact != nil && len(t.CfoCodes) > 0 {
		if fr, e := s.fact.MpFact(ctx, year, month, segment); e == nil {
			allowed := map[int]bool{}
			for _, c := range t.CfoCodes {
				allowed[c] = true
			}
			for _, r := range fr {
				if allowed[r.CodeCFO] {
					// Budgeting отдаёт BYN, штрафы — RUB → приводим к валюте формы.
					factIdx[[2]int{r.CodeCFO, r.CodePL}] = convertAmount(r.Amount, r.Currency, f.Currency)
				}
			}
		}
	}

	// Сохранённая тактика + стратегия + причины корректировок.
	tacByKey := map[string]float64{}
	manualByKey := map[string]bool{}
	stratByKey := map[string]float64{}
	reasonByKey := map[string]string{}
	if len(t.CfoCodes) > 0 {
		if rows, e := s.pool.Query(ctx, `
			SELECT profit_center, block_type, amount, is_manual, amount_strategy
			FROM pl_metric WHERE pl_id=$1 AND template_code='TPL-MP' AND profit_center = ANY($2)`, t.PlID, append(t.CfoCodes, 0)); e == nil {
			for rows.Next() {
				var pc int
				var bt string
				var amt, strat *float64
				var im bool
				if rows.Scan(&pc, &bt, &amt, &im, &strat) == nil {
					k := fmt.Sprintf("%d:%s", pc, bt)
					if amt != nil {
						tacByKey[k] = *amt
						manualByKey[k] = im
					}
					if strat != nil {
						stratByKey[k] = *strat
					}
				}
			}
			rows.Close()
		}
		if ar, e := s.pool.Query(ctx, `
			SELECT profit_center, block_type, reason FROM pl_adjustment
			WHERE pl_id=$1 AND profit_center = ANY($2) ORDER BY adjusted_at`, t.PlID, append(t.CfoCodes, 0)); e == nil {
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

	// Онлайн-стратегия (FormToLoadPlan, BYN) — перекрывает pl_metric-стратегию (TPL-09).
	if s.fact != nil && len(t.CfoCodes) > 0 {
		allowedCfo := map[int]bool{}
		for _, c := range t.CfoCodes {
			allowedCfo[c] = true
		}
		if sr, e := s.fact.MpStrategy(ctx, year, month, segment); e == nil {
			pb := plToBlock()
			online := map[string]float64{}
			for _, r := range sr {
				if !allowedCfo[r.CodeCFO] {
					continue
				}
				bt := pb[r.CodePL]
				if bt == "" {
					continue
				}
				// COGS осн(2006)+пошив(6006) складываются в одну строку cogs_total.
				online[fmt.Sprintf("%d:%s", r.CodeCFO, bt)] += convertAmount(r.Amount, r.Currency, f.Currency)
			}
			for k, v := range online {
				stratByKey[k] = v // онлайн-план перекрывает импортированную стратегию
			}
		}
	}

	spec := mpFormSpec()
	editable := mpEditableSet()

	// Тотал-строки (скидка/уценка) — по всем площадкам, profit_center=0.
	totalInputs := map[string]float64{}
	for _, l := range spec {
		if l.Scope == ScopeTotal && editable[l.BlockType] {
			if v, ok := tacByKey[fmt.Sprintf("0:%s", l.BlockType)]; ok {
				totalInputs[l.BlockType] = v
			}
		}
	}

	for _, p := range f.Platforms {
		vat := vatByCountry(p.Country)
		// Входы каскада: сохранённая тактика ?? источник ?? 0; %СПП — деривация.
		in := map[string]float64{}
		for _, l := range spec {
			if l.Scope != ScopePlatform || !editable[l.BlockType] {
				continue
			}
			k := fmt.Sprintf("%d:%s", p.CodeCFO, l.BlockType)
			if v, ok := tacByKey[k]; ok {
				in[l.BlockType] = v
				continue
			}
			if l.BlockType == BSPP {
				in[BSPP] = deriveSPP(factIdx, p.CodeCFO)
				continue
			}
			if l.BlockType == BMarkup {
				continue // расчётная по умолчанию
			}
			if v, ok := factSeed(factIdx, l.BlockType, p.CodeCFO); ok {
				in[l.BlockType] = v
			}
		}
		values := computeMpPlatform(in, vat)

		for _, l := range spec {
			if l.Kind == KindHeader {
				continue
			}
			k := fmt.Sprintf("%d:%s", p.CodeCFO, l.BlockType)
			cell := MpFormCell{CodeCFO: p.CodeCFO, BlockType: l.BlockType}
			if l.Scope == ScopeTotal {
				cell.Value = totalInputs[l.BlockType]
			} else {
				cell.Value = values[l.BlockType]
			}
			// Факт (для input-строк) — из источника.
			if editable[l.BlockType] {
				if fv, ok := factSeed(factIdx, l.BlockType, p.CodeCFO); ok {
					vv := fv
					cell.Fact = &vv
				}
			}
			if sv, ok := stratByKey[k]; ok {
				vv := sv
				cell.Strategy = &vv
			}
			if tv, ok := tacByKey[k]; ok {
				vv := tv
				cell.Tactic = &vv
				cell.IsManual = manualByKey[k]
				if cell.IsManual {
					cell.Reason = reasonByKey[k]
				}
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

// SaveMpForm сохраняет ввод тактики задания МП (только editable-строки спеки) в
// pl_metric + снимок form_submission. Права: assignee/delegate/admin.
func (s *TaskStore) SaveMpForm(ctx context.Context, taskID, actorID int64, isAdmin bool, rows []MpSaveRow, payload []byte) error {
	t, year, month, segment, err := s.taskBrief(ctx, taskID)
	if err != nil {
		return err
	}
	if !isAdmin && !(t.AssigneeID != nil && *t.AssigneeID == actorID) && !(t.DelegateID != nil && *t.DelegateID == actorID) {
		return fmt.Errorf("нет прав на заполнение задания")
	}
	allowed := map[int]bool{}
	for _, c := range t.CfoCodes {
		allowed[c] = true
	}
	editable := mpEditableSet()
	lineByBlock := mpLineByBlock()

	factIdx := map[[2]int]float64{}
	if s.fact != nil {
		if fr, e := s.fact.MpFact(ctx, year, month, segment); e == nil {
			for _, r := range fr {
				if allowed[r.CodeCFO] {
					factIdx[[2]int{r.CodeCFO, r.CodePL}] = convertAmount(r.Amount, r.Currency, "RUB")
				}
			}
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, r := range rows {
		line, ok := lineByBlock[r.BlockType]
		if !ok || !editable[r.BlockType] {
			return fmt.Errorf("строка не редактируется: %s", r.BlockType)
		}
		pc := r.CodeCFO
		if line.Scope == ScopeTotal {
			pc = 0 // тотал-строки (скидка/уценка) — по всему сегменту
		} else if !allowed[pc] {
			return fmt.Errorf("ЦФО %d вне задания", pc)
		}
		if r.IsManual && r.Comment == "" {
			return fmt.Errorf("причина корректировки обязательна (ADJ-02)")
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO pl_metric (pl_id, template_code, segment, line_code, block_type, profit_center, country, scenario, period_year, period_month, currency, amount, is_manual)
			VALUES ($1,'TPL-MP',$2,$3,$4,$5,'',$6,$7,$8,'RUB',$9,$10)
			ON CONFLICT (pl_id, template_code, segment, line_code, block_type, profit_center, scenario, period_year, period_month, currency)
			DO UPDATE SET amount=EXCLUDED.amount, is_manual=EXCLUDED.is_manual`,
			t.PlID, segment, line.CodePL, r.BlockType, pc, ScenarioTactic, year, month, r.Amount, r.IsManual)
		if err != nil {
			return err
		}
		if r.IsManual {
			var orig *float64
			if v, ok := factIdx[[2]int{pc, line.CodePL}]; ok {
				vv := v
				orig = &vv
			}
			_, _ = tx.Exec(ctx, `
				INSERT INTO pl_adjustment (pl_id, profit_center, line_code, block_type, period_year, period_month, currency, original_calculated, adjusted_value, reason, adjusted_by, adjusted_at)
				VALUES ($1,$2,$3,$4,$5,$6,'RUB',$7,$8,$9,$10,NOW())`,
				t.PlID, pc, line.CodePL, r.BlockType, year, month, orig, r.Amount, r.Comment, actorID)
		}
	}
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	_, _ = tx.Exec(ctx, `INSERT INTO form_submission (pl_id, template_code, json_payload, submitted_by, submitted_at) VALUES ($1,'TPL-MP',$2,$3,NOW())`, t.PlID, payload, actorID)
	_, _ = tx.Exec(ctx, `UPDATE pl_task SET status=CASE WHEN status='pending' THEN 'in_progress' ELSE status END, updated_at=NOW() WHERE id=$1`, taskID)
	return tx.Commit(ctx)
}
