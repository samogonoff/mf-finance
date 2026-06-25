package plans

import (
	"errors"
	"sort"
)

// editableBlock — editable-блок формы TPL-MP (ввод тактики на этапе 1.1).
type editableBlock struct {
	BlockType string
	CodePL    int
	Name      string
}

// mpEditableBlocks — блоки ввода тактики (VS3). Расчётные (1045/1022/наценка/маржа)
// — этап 2 (CALC). Порядок — как в прототипе.
func mpEditableBlocks() []editableBlock {
	return []editableBlock{
		{BlockType: "sales_manager_price", CodePL: 1046, Name: "ПРОДАЖИ по ценам менеджера с НДС"},
		{BlockType: "shipments", CodePL: 8006, Name: "Себестоимость по отпускным ценам"},
	}
}

// buildMpForm собирает форму из факта (read-only) и сохранённой тактики (editable).
// Чистая функция — без БД; на ней держится корректность round-trip.
func buildMpForm(segment string, year, month int, currency string, fact, tactic []MetricRow) MpForm {
	// Площадки сегмента, отсортированные по code_cfo.
	platMap := segmentPlatforms(segment)
	platforms := make([]MarketplaceRow, 0, len(platMap))
	for _, m := range MarketplaceSeed() {
		if m.Segment == segment {
			platforms = append(platforms, m)
		}
	}
	sort.Slice(platforms, func(i, j int) bool { return platforms[i].CodeCFO < platforms[j].CodeCFO })

	// Индексы фактов и тактики по (code_pl, code_cfo).
	factIdx := indexByLineAndCFO(fact)
	tacticIdx := indexByLineAndCFO(tactic)

	blocks := make([]FormBlock, 0, len(mpEditableBlocks()))
	for _, b := range mpEditableBlocks() {
		rows := make([]FormRow, 0, len(platforms))
		for _, p := range platforms {
			fr := FormRow{CodeCFO: p.CodeCFO, NameCFO: p.NameCFO}
			if m, ok := factIdx[lineCFO{b.CodePL, p.CodeCFO}]; ok {
				fr.Fact = m.Amount
			}
			if m, ok := tacticIdx[lineCFO{b.CodePL, p.CodeCFO}]; ok {
				v := m.Amount
				fr.Tactic = &v
			}
			rows = append(rows, fr)
		}
		blocks = append(blocks, FormBlock{
			BlockType: b.BlockType,
			CodePL:    b.CodePL,
			Name:      b.Name,
			Editable:  true,
			Rows:      rows,
		})
	}

	return MpForm{
		Header: MpFormHeader{
			Year: year, Month: month, Segment: segment,
			Currency: currency, Scenario: ScenarioTactic,
		},
		Platforms: platforms,
		Blocks:    blocks,
	}
}

type lineCFO struct{ line, cfo int }

func indexByLineAndCFO(rows []MetricRow) map[lineCFO]MetricRow {
	idx := make(map[lineCFO]MetricRow, len(rows))
	for _, r := range rows {
		idx[lineCFO{r.LineCode, r.ProfitCenter}] = r
	}
	return idx
}

// metricsFromRequest валидирует payload и превращает editable-ячейки в строки
// pl_metric. Тактика — единственный редактируемый сценарий; чужие code_cfo
// (вне сегмента) отклоняются. Страна берётся из dir_marketplace.
func metricsFromRequest(req SaveMpFormRequest) ([]MetricRow, error) {
	if req.Period.Year <= 0 || req.Period.Month < 1 || req.Period.Month > 12 {
		return nil, errors.New("invalid period")
	}
	if req.Segment != "large" && req.Segment != "small" {
		return nil, errors.New("invalid segment")
	}
	currency := req.Header.Currency
	if currency == "" {
		currency = "RUB"
	}
	platforms := segmentPlatforms(req.Segment)
	editable := map[string]bool{}
	for _, b := range mpEditableBlocks() {
		editable[b.BlockType] = true
	}
	countryOf := map[int]string{}
	for _, m := range MarketplaceSeed() {
		countryOf[m.CodeCFO] = m.Country
	}

	out := make([]MetricRow, 0, len(req.Rows))
	for _, r := range req.Rows {
		if _, ok := platforms[r.CodeCFO]; !ok {
			return nil, errors.New("code_cfo вне сегмента")
		}
		if !editable[r.BlockType] {
			return nil, errors.New("блок не редактируется: " + r.BlockType)
		}
		out = append(out, MetricRow{
			LineCode:     r.CodePL,
			BlockType:    r.BlockType,
			ProfitCenter: r.CodeCFO,
			Country:      countryOf[r.CodeCFO],
			Scenario:     ScenarioTactic,
			Year:         req.Period.Year,
			Month:        req.Period.Month,
			Currency:     currency,
			Amount:       r.Amount,
			IsManual:     r.IsManual,
		})
	}
	return out, nil
}
