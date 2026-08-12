package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// Форма ввода МП — TASK-DRIVEN и ПОЛНАЯ (весь каскад как в прототипе large):
// площадки = ЦФО задания, строки = mpFormSpec() (продажи→СПП→наценка→маржа→COGS→
// прямые затраты→PL). Ввод — 1046 + %СПП + себестоимость + статьи затрат; всё
// производное считает computeMpPlatform (сервер) и зеркалит клиент (живой пересчёт).
// Факт/стратегия — read-only. Права: assignee/delegate/admin.

// MpFormPlatform — площадка МП (ЦФО задания).
type MpFormPlatform struct {
	CodeCFO     int    `json:"code_cfo"`
	Name        string `json:"name"`
	Country     string `json:"country"`
	Segment     string `json:"segment"`      // large|small — группировка колонок в объединённой форме
	LegalEntity string `json:"legal_entity"` // для фильтра «по ЮЛ» в форме и своде
}

// distinctSegments — набор сегментов, встречающихся среди площадок задания.
func distinctSegments(segmentOf map[int]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 2)
	for _, seg := range segmentOf {
		if seg != "" && !seen[seg] {
			seen[seg] = true
			out = append(out, seg)
		}
	}
	sort.Strings(out)
	return out
}

// MpFormCell — ячейка (площадка × строка): вычисленное значение + ввод/факт/стратегия.
type MpFormCell struct {
	CodeCFO   int      `json:"code_cfo"`
	BlockType string   `json:"block_type"`
	Value     float64  `json:"value"`             // вычисленное значение каскада (отображение)
	Fact      *float64 `json:"fact"`              // из источника (для input-строк)
	FactPrev  *float64 `json:"fact_prev"`         // факт того же месяца прошлого года (SPEC §9.6)
	Strategy  *float64 `json:"strategy"`          // стратегический бюджет (read-only)
	Target    *float64 `json:"target"`            // тактика-таргет (FormToLoaTaktTarget, read-only)
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

// MpFormData собирает полную форму МП задания (каскад по спеке) в валюте currency
// (BYN/RUB/USD; пусто → RUB). Тактика хранится в RUB, пересчёт — только отображение.
func (s *TaskStore) MpFormData(ctx context.Context, taskID int64, currency string) (MpTaskForm, error) {
	var f MpTaskForm
	t, year, month, segment, err := s.taskBrief(ctx, taskID)
	if err != nil {
		return f, err
	}
	f.Task, f.Year, f.Month, f.Segment, f.Currency = t, year, month, segment, normalizeCurrency(currency)
	f.Lines = mpFormSpec()

	// Площадки задания + страна/сегмент. Задание может покрывать оба сегмента
	// (объединённая форма МП) — тогда шапка помечается «all», а разделение
	// остаётся внутри формы: колонки группируются по сегменту.
	countryOf := map[int]string{}
	leOf := map[int]string{}
	for _, m := range MarketplaceSeed() {
		countryOf[m.CodeCFO] = m.Country
		leOf[m.CodeCFO] = m.LegalEntity
	}
	segmentOf := s.segmentsByCfo(ctx, t.CfoCodes)
	mpNames := s.nameMap(ctx, "dir_marketplace", "code_cfo", "name_cfo")
	cfoNames := s.nameMap(ctx, "dir_cfo", "code_cfo", "name_cfo")
	dirLE := s.nameMap(ctx, "dir_marketplace", "code_cfo", "legal_entity")
	for _, c := range t.CfoCodes {
		name := mpNames[c]
		if name == "" {
			name = cfoNames[c]
		}
		le := dirLE[c]
		if le == "" {
			le = leOf[c] // справочник может быть не наполнен — берём из seed
		}
		f.Platforms = append(f.Platforms, MpFormPlatform{
			CodeCFO: c, Name: name, Country: countryOf[c], Segment: segmentOf[c], LegalEntity: le,
		})
	}
	if segments := distinctSegments(segmentOf); len(segments) > 1 {
		f.Segment = "all"
	}
	// НДС шапки — по стране первой площадки (для отображения; расчёт — per-площадка).
	if len(f.Platforms) > 0 {
		f.Vat = vatByCountry(f.Platforms[0].Country)
	} else {
		f.Vat = 0.20
	}

	// Read-only сценарии из источника, уже в валюте формы: факт, факт прошлого
	// года, стратегия (FormToLoadPlan), таргет (FormToLoaTaktTarget).
	allowedCfo := map[int]bool{}
	for _, c := range t.CfoCodes {
		allowedCfo[c] = true
	}
	layerSet := s.mpLayersFor(ctx, year, month, segmentOf, allowedCfo, f.Currency)

	// Сохранённая тактика + стратегия + причины корректировок.
	tacByKey := map[string]float64{}
	manualByKey := map[string]bool{}
	stratByKey := map[string]float64{}
	reasonByKey := map[string]string{}
	// pl_metric хранит суммы в RUB; проценты (%СПП, наценка) валютой не пересчитываются.
	specByBlock := mpLineByBlock()
	toDisplay := func(block string, v float64) float64 {
		if specByBlock[block].ValueKind == ValuePct {
			return v
		}
		return convertAmount(v, "RUB", f.Currency)
	}
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
					k := layerKey(pc, bt)
					if amt != nil {
						tacByKey[k] = toDisplay(bt, *amt)
						manualByKey[k] = im
					}
					if strat != nil {
						stratByKey[k] = toDisplay(bt, *strat)
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

	// Онлайн-стратегия (FormToLoadPlan) перекрывает импортированную в pl_metric (TPL-09).
	for _, l := range layerSet.bySegment {
		for k, v := range l.strategy {
			stratByKey[k] = v
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
		// Слои берутся по сегменту КАЖДОЙ площадки: в объединённой форме
		// large и small приходят из разных срезов источника.
		pl := layerSet.forCfo(p.CodeCFO)
		values := computeMpPlatform(mpPlatformInputs(p.CodeCFO, tacByKey, pl.fact), vatByCountry(p.Country))

		for _, l := range spec {
			if l.Kind == KindHeader {
				continue
			}
			k := layerKey(p.CodeCFO, l.BlockType)
			cell := MpFormCell{CodeCFO: p.CodeCFO, BlockType: l.BlockType}
			if l.Scope == ScopeTotal {
				cell.Value = totalInputs[l.BlockType]
			} else {
				cell.Value = values[l.BlockType]
			}
			// Факт (для input-строк) — из источника; рядом факт прошлого года.
			if editable[l.BlockType] {
				if fv, ok := factSeed(pl.fact, l.BlockType, p.CodeCFO); ok {
					vv := fv
					cell.Fact = &vv
				}
				if pv, ok := factSeed(pl.factPrev, l.BlockType, p.CodeCFO); ok {
					vv := pv
					cell.FactPrev = &vv
				}
			}
			if sv, ok := stratByKey[k]; ok {
				vv := sv
				cell.Strategy = &vv
			}
			if tv, ok := pl.target[k]; ok {
				vv := tv
				cell.Target = &vv
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

// mpPlatformInputs — входы каскада по площадке: сохранённая тактика ?? факт из
// источника ?? деривация (%СПП). Наценка по умолчанию расчётная. Одна функция на
// форму ввода и на свод — иначе проверяющий увидит не те числа, что заполняющий.
func mpPlatformInputs(cfo int, tactic map[string]float64, factIdx map[[2]int]float64) map[string]float64 {
	in := map[string]float64{}
	for _, l := range mpFormSpec() {
		if l.Scope != ScopePlatform || !l.Editable {
			continue
		}
		if v, ok := tactic[layerKey(cfo, l.BlockType)]; ok {
			in[l.BlockType] = v
			continue
		}
		switch l.BlockType {
		case BSPP:
			in[BSPP] = deriveSPP(factIdx, cfo)
		case BMarkup:
			// расчётная по умолчанию — не сидируем
		default:
			if v, ok := factSeed(factIdx, l.BlockType, cfo); ok {
				in[l.BlockType] = v
			}
		}
	}
	return in
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
// currency — валюта, в которой пользователь ВВОДИЛ суммы; хранение всегда в RUB,
// поэтому денежные строки пересчитываются обратно (проценты — как есть).
func (s *TaskStore) SaveMpForm(ctx context.Context, taskID, actorID int64, isAdmin bool, rows []MpSaveRow, payload []byte, currency string) error {
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
	src := normalizeCurrency(currency)

	// Сегмент пишем по КАЖДОЙ площадке (объединённая форма покрывает оба);
	// тотал-строки (скидка/уценка) остаются под сегментом задания.
	segmentOf := s.segmentsByCfo(ctx, t.CfoCodes)
	segmentFor := func(cfo int) string {
		if seg, ok := segmentOf[cfo]; ok && seg != "" {
			return seg
		}
		return segment
	}
	// Значение «до корректировки» (ADJ-03) — факт в валюте хранения.
	layerSet := s.mpLayersFor(ctx, year, month, segmentOf, allowed, "RUB")
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
		rowSegment := segment
		if line.Scope == ScopeTotal {
			pc = 0 // тотал-строки (скидка/уценка) — по всему заданию
		} else if !allowed[pc] {
			return fmt.Errorf("ЦФО %d вне задания", pc)
		} else {
			rowSegment = segmentFor(pc)
		}
		if r.IsManual && r.Comment == "" {
			return fmt.Errorf("причина корректировки обязательна (ADJ-02)")
		}
		if line.ValueKind == ValueMoney {
			r.Amount = convertAmount(r.Amount, src, "RUB")
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO pl_metric (pl_id, template_code, segment, line_code, block_type, profit_center, country, scenario, period_year, period_month, currency, amount, is_manual)
			VALUES ($1,'TPL-MP',$2,$3,$4,$5,'',$6,$7,$8,'RUB',$9,$10)
			ON CONFLICT (pl_id, template_code, segment, line_code, block_type, profit_center, scenario, period_year, period_month, currency)
			DO UPDATE SET amount=EXCLUDED.amount, is_manual=EXCLUDED.is_manual`,
			t.PlID, rowSegment, line.CodePL, r.BlockType, pc, ScenarioTactic, year, month, r.Amount, r.IsManual)
		if err != nil {
			return err
		}
		if r.IsManual {
			var orig *float64
			if v, ok := layerSet.forCfo(pc).fact[[2]int{pc, line.CodePL}]; ok {
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
