package plans

import (
	"context"
	"sort"
	"strings"
)

// Свод периода («общий просмотр») — единственный экран, где проверяющий и
// согласующий видят ВСЕ данные карточки сразу: те же строки спеки TPL-MP и в том
// же порядке, что у заполняющего (mpFormSpec + mpPlatformInputs + computeMpPlatform),
// но по всем площадкам, с детализацией по МП в строках и колонками сценариев:
// факт периода · факт того же месяца прошлого года · стратегия · таргет · тактика.
//
// Фильтры (запрос финансов): валюта, ЮЛ, сегмент large/small, страна, площадки.
// ABAC: не-админ видит только свой срез (allowed приходит из Service.AllowedCFOs).

// BoardValues — значения одной клетки свода по всем сценариям.
// Calc — значение каскада (то, что реально видно в форме), остальные — nil, если
// сценарий по этой строке не наполнен.
type BoardValues struct {
	Calc     float64  `json:"calc"`
	Tactic   *float64 `json:"tactic"`
	Fact     *float64 `json:"fact"`
	FactPrev *float64 `json:"fact_prev"`
	Strategy *float64 `json:"strategy"`
	Target   *float64 `json:"target"`
}

// BoardChild — разрез строки: одна площадка (ЦФО) со статусом её задания.
type BoardChild struct {
	CodeCFO     int         `json:"code_cfo"`
	Name        string      `json:"name"`
	Segment     string      `json:"segment"`
	Country     string      `json:"country"`
	LegalEntity string      `json:"legal_entity"`
	Values      BoardValues `json:"values"`
	IsManual    bool        `json:"is_manual"`
	Reason      string      `json:"reason,omitempty"`
	TaskID      int64       `json:"task_id,omitempty"`
	TaskStatus  string      `json:"task_status,omitempty"`
	Assignee    string      `json:"assignee,omitempty"`
}

// BoardRow — строка спеки формы + итог + детализация по площадкам.
type BoardRow struct {
	Line     MpLine       `json:"line"`
	Total    BoardValues  `json:"total"`
	Children []BoardChild `json:"children"`
}

// BoardPlatform — площадка для наполнения фильтров UI.
type BoardPlatform struct {
	CodeCFO     int    `json:"code_cfo"`
	Name        string `json:"name"`
	Segment     string `json:"segment"`
	Country     string `json:"country"`
	LegalEntity string `json:"legal_entity"`
}

// BoardFilter — применённый набор фильтров (эхо запроса, чтобы UI не гадал).
type BoardFilter struct {
	Currency    string `json:"currency"`
	Segment     string `json:"segment"`
	LegalEntity string `json:"legal_entity"`
	Country     string `json:"country"`
	CodeCFO     []int  `json:"code_cfo"`
}

// Board — свод периода.
type Board struct {
	PlID      int64           `json:"pl_id"`
	Year      int             `json:"year"`
	Month     int             `json:"month"`
	Filter    BoardFilter     `json:"filter"`
	Platforms []BoardPlatform `json:"platforms"` // ВСЕ доступные (после ABAC) — для фильтров
	Rows      []BoardRow      `json:"rows"`
}

// boardUniverse — площадки, доступные пользователю, с учётом фильтров.
// allowed == nil — админ (без ABAC-ограничения).
func boardUniverse(f BoardFilter, allowed map[int]bool) (all, filtered []BoardPlatform) {
	pick := map[int]bool{}
	for _, c := range f.CodeCFO {
		pick[c] = true
	}
	for _, m := range MarketplaceSeed() {
		if allowed != nil && !allowed[m.CodeCFO] {
			continue
		}
		p := BoardPlatform{CodeCFO: m.CodeCFO, Name: m.NameCFO, Segment: m.Segment, Country: m.Country, LegalEntity: m.LegalEntity}
		all = append(all, p)
		if f.Segment != "" && f.Segment != m.Segment {
			continue
		}
		if f.Country != "" && f.Country != m.Country {
			continue
		}
		if f.LegalEntity != "" && f.LegalEntity != m.LegalEntity {
			continue
		}
		if len(pick) > 0 && !pick[m.CodeCFO] {
			continue
		}
		filtered = append(filtered, p)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CodeCFO < all[j].CodeCFO })
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].CodeCFO < filtered[j].CodeCFO })
	return all, filtered
}

// boardTactic читает сохранённую тактику карточки (RUB) и приводит к валюте свода;
// проценты валютой не пересчитываются. Возвращает также причины корректировок.
func (s *TaskStore) boardTactic(ctx context.Context, plID int64, currency string) (tactic map[string]float64, manual map[string]bool, reason map[string]string) {
	tactic, manual, reason = map[string]float64{}, map[string]bool{}, map[string]string{}
	spec := mpLineByBlock()
	rows, err := s.pool.Query(ctx, `
		SELECT profit_center, block_type, amount, is_manual
		FROM pl_metric WHERE pl_id=$1 AND template_code='TPL-MP' AND amount IS NOT NULL`, plID)
	if err != nil {
		return
	}
	for rows.Next() {
		var pc int
		var bt string
		var amt *float64
		var im bool
		if rows.Scan(&pc, &bt, &amt, &im) == nil && amt != nil {
			v := *amt
			if spec[bt].ValueKind != ValuePct {
				v = convertAmount(v, "RUB", currency)
			}
			tactic[layerKey(pc, bt)] = v
			manual[layerKey(pc, bt)] = im
		}
	}
	rows.Close()

	ar, err := s.pool.Query(ctx, `
		SELECT profit_center, block_type, reason FROM pl_adjustment
		WHERE pl_id=$1 ORDER BY adjusted_at`, plID)
	if err != nil {
		return
	}
	defer ar.Close()
	for ar.Next() {
		var pc int
		var bt, rs string
		if ar.Scan(&pc, &bt, &rs) == nil {
			reason[layerKey(pc, bt)] = rs
		}
	}
	return
}

// boardTasksByCfo — задание TPL-MP, покрывающее площадку (для колонки «кто/статус»).
func (s *TaskStore) boardTasksByCfo(ctx context.Context, plID int64) map[int]Task {
	out := map[int]Task{}
	tasks, err := s.ListByInstance(ctx, plID)
	if err != nil {
		return out
	}
	for _, t := range tasks {
		if t.FormCode != "TPL-MP" {
			continue
		}
		for _, c := range t.CfoCodes {
			// Первое найденное задание ввода выигрывает у согласующего — на своде
			// важнее «кто заполняет», чем «кто потом смотрит».
			if prev, ok := out[c]; ok && prev.Role != "approve" {
				continue
			}
			out[c] = t
		}
	}
	return out
}

// aggValue — сумма ненулевых указателей; nil, если ни одного значения нет.
func aggValue(vals []*float64) *float64 {
	var sum float64
	found := false
	for _, v := range vals {
		if v != nil {
			sum += *v
			found = true
		}
	}
	if !found {
		return nil
	}
	return &sum
}

// boardTotalPct — итог по процентной строке: пересчёт из агрегатов (сумма
// процентов смысла не имеет). Зеркалит totalValue() фронта.
func boardTotalPct(block string, agg map[string]float64) float64 {
	net, mgr := agg[BSalesPlatNet], agg[BSalesManagerNet]
	switch block {
	case BSPP:
		return safeDiv(mgr-net, mgr)
	case BMarkup:
		return safeDiv(net, agg[BShipments]) - boolToF(agg[BShipments] != 0)
	case BMarkupTotal:
		return safeDiv(net, agg[BCogsTotal]) - boolToF(agg[BCogsTotal] != 0)
	case BRetailMarginPct:
		return safeDiv(agg[BRetailMargin], net)
	case BGrossMarginPct:
		return safeDiv(agg[BGrossMargin], net)
	case BPlPlatformPct:
		return safeDiv(agg[BPlPlatform], net)
	case BDirectShare:
		return safeDiv(agg[BPlatformCosts], mgr)
	default:
		return 0
	}
}

// BoardData собирает свод периода. allowed == nil → без ABAC-ограничения (админ).
func (s *TaskStore) BoardData(ctx context.Context, plID int64, f BoardFilter, allowed map[int]bool) (Board, error) {
	b := Board{PlID: plID}
	f.Currency = normalizeCurrency(f.Currency)
	f.Segment = strings.ToLower(strings.TrimSpace(f.Segment))
	b.Filter = f
	if err := s.pool.QueryRow(ctx, `SELECT period_year, period_month FROM pl_instance WHERE id=$1`, plID).
		Scan(&b.Year, &b.Month); err != nil {
		return b, err
	}

	all, platforms := boardUniverse(f, allowed)
	b.Platforms = all

	// Слои источника — по сегментам, которые реально попали в выборку.
	segments := map[string]bool{}
	for _, p := range platforms {
		segments[p.Segment] = true
	}
	inScope := map[int]bool{}
	for _, p := range platforms {
		inScope[p.CodeCFO] = true
	}
	layersBySegment := map[string]mpLayers{}
	for seg := range segments {
		layersBySegment[seg] = s.mpSourceLayers(ctx, b.Year, b.Month, seg, inScope, f.Currency)
	}

	tactic, manual, reason := s.boardTactic(ctx, plID, f.Currency)
	tasksByCfo := s.boardTasksByCfo(ctx, plID)

	// Каскад по каждой площадке — ровно как в форме ввода.
	values := map[int]map[string]float64{}
	for _, p := range platforms {
		l := layersBySegment[p.Segment]
		values[p.CodeCFO] = computeMpPlatform(mpPlatformInputs(p.CodeCFO, tactic, l.fact), vatByCountry(p.Country))
	}
	// Агрегаты денежных строк — база для процентных итогов.
	agg := map[string]float64{}
	for _, p := range platforms {
		for block, v := range values[p.CodeCFO] {
			if mpLineByBlock()[block].ValueKind == ValueMoney {
				agg[block] += v
			}
		}
	}

	editable := mpEditableSet()
	for _, line := range mpFormSpec() {
		if line.Kind == KindHeader {
			continue
		}
		row := BoardRow{Line: line}
		if line.Scope == ScopeTotal {
			// Тотал-строки (скидки/уценки) ведутся одним значением на весь сегмент.
			if v, ok := tactic[layerKey(0, line.BlockType)]; ok {
				vv := v
				row.Total = BoardValues{Calc: v, Tactic: &vv}
			}
			b.Rows = append(b.Rows, row)
			continue
		}

		var facts, prevs, strats, targets, tactics []*float64
		for _, p := range platforms {
			l := layersBySegment[p.Segment]
			k := layerKey(p.CodeCFO, line.BlockType)
			ch := BoardChild{
				CodeCFO: p.CodeCFO, Name: p.Name, Segment: p.Segment,
				Country: p.Country, LegalEntity: p.LegalEntity,
				Values: BoardValues{Calc: values[p.CodeCFO][line.BlockType]},
			}
			if editable[line.BlockType] {
				if v, ok := factSeed(l.fact, line.BlockType, p.CodeCFO); ok {
					vv := v
					ch.Values.Fact = &vv
				}
				if v, ok := factSeed(l.factPrev, line.BlockType, p.CodeCFO); ok {
					vv := v
					ch.Values.FactPrev = &vv
				}
			}
			if v, ok := l.strategy[k]; ok {
				vv := v
				ch.Values.Strategy = &vv
			}
			if v, ok := l.target[k]; ok {
				vv := v
				ch.Values.Target = &vv
			}
			if v, ok := tactic[k]; ok {
				vv := v
				ch.Values.Tactic = &vv
				ch.IsManual = manual[k]
				if ch.IsManual {
					ch.Reason = reason[k]
				}
			}
			if t, ok := tasksByCfo[p.CodeCFO]; ok {
				ch.TaskID, ch.TaskStatus, ch.Assignee = t.ID, t.Status, t.AssigneeName
				if t.DelegateName != "" {
					ch.Assignee = t.DelegateName
				}
			}
			facts = append(facts, ch.Values.Fact)
			prevs = append(prevs, ch.Values.FactPrev)
			strats = append(strats, ch.Values.Strategy)
			targets = append(targets, ch.Values.Target)
			tactics = append(tactics, ch.Values.Tactic)
			row.Children = append(row.Children, ch)
		}

		if line.ValueKind == ValuePct {
			row.Total = BoardValues{Calc: boardTotalPct(line.BlockType, agg)}
		} else {
			row.Total = BoardValues{
				Calc:     agg[line.BlockType],
				Fact:     aggValue(facts),
				FactPrev: aggValue(prevs),
				Strategy: aggValue(strats),
				Target:   aggValue(targets),
				Tactic:   aggValue(tactics),
			}
		}
		b.Rows = append(b.Rows, row)
	}
	return b, nil
}
