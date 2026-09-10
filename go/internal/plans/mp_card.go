package plans

import (
	"context"
	"time"
)

// Привязка формы МП к карточке процесса: снимок версии и строки для публикации.
// Формо-специфичная часть общей оболочки (card_service.go) — по одному такому
// файлу на форму.

// MpCardProvider — снимок и публикация формы TPL-MP.
type MpCardProvider struct {
	store  MetricStore
	rates  *RateBook
	fact   MpFactSource
	common MpConditionsStore
}

// WithCommonCosts подключает реестр общих затрат: они публикуются под ЦФО 250 и
// живут отдельно от метрик формы.
func (p *MpCardProvider) WithCommonCosts(s MpConditionsStore) *MpCardProvider {
	p.common = s
	return p
}

// WithFactSource подключает источник плана: из него берутся отношения
// «план с НДС ÷ план без НДС», которыми считалась форма. Без источника снимок
// фиксирует ставку справочника — это хуже, но не ломает утверждение карточки.
func (p *MpCardProvider) WithFactSource(src MpFactSource) *MpCardProvider {
	p.fact = src
	return p
}

// NewMpCardProvider — конструктор.
func NewMpCardProvider(store MetricStore, rates *RateBook) *MpCardProvider {
	return &MpCardProvider{store: store, rates: rates}
}

// FormSnapshot — снимок значений карточки МП: сохранённая тактика по площадкам
// сегмента + применённые курсы. Курс фиксируется вместе с версией (ТЗ МП §3.3),
// иначе утверждённые суммы «уезжают» при следующем изменении справочника.
func (p *MpCardProvider) FormSnapshot(ctx context.Context, c Card) (map[string]any, error) {
	metrics, err := p.store.MetricsAll(ctx, c.PlID, c.Year, c.Month)
	if err != nil {
		return nil, err
	}
	segments := mpSegmentsOf(c)
	segOf := mpSegmentByCfo()
	rows := make([]map[string]any, 0, len(metrics))
	for _, m := range metrics {
		if seg := segOf[m.ProfitCenter]; seg != "" && !segments[seg] {
			continue
		}
		rows = append(rows, map[string]any{
			"code_cfo":   m.ProfitCenter,
			"code_pl":    m.LineCode,
			"block_type": m.BlockType,
			"year":       m.Year,
			"month":      m.Month,
			"currency":   m.Currency,
			"amount":     m.Amount,
			"is_manual":  m.IsManual,
			"segment":    segOf[m.ProfitCenter],
		})
	}
	snap := map[string]any{
		"form_code": c.FormCode,
		"scope":     c.ScopeKey,
		"period":    map[string]int{"year": c.Year, "month": c.Month},
		"values":    rows,
		"calc_mode": c.CalcMode,
	}
	if p.rates != nil {
		snap["fx"] = p.rates.Snapshot(ctx, c.Year, c.Month, "RUB", "BYN", "USD", "KZT", "UZS")
		// Фиксируем ИМЕННО ТО, чем считалась форма: отношения плановых сумм, а
		// не ставку справочника. Иначе следующая заливка плана изменит ставку, и
		// утверждённые суммы перестанут воспроизводиться из версии.
		segList := make([]string, 0, len(segments))
		for seg := range segments {
			segList = append(segList, seg)
		}
		planLayer := mpPlanByBlock(ctx, p.fact, c.Year, c.Month, segList)
		prevY, prevM := prevMonth(c.Year, c.Month)
		planPrev := mpPlanByBlock(ctx, p.fact, prevY, prevM, segList)
		vat := map[string]float64{}
		basis := map[string]MpVatBasis{}
		for _, mp := range MarketplaceSeed() {
			if !segments[mp.Segment] {
				continue
			}
			b := mpVatBasis(planLayer, planPrev, mp.CodeCFO, c.Year, c.Month, p.rates.Vat(ctx, mp.Country, mp.CodeCFO))
			key := itoa(int64(mp.CodeCFO))
			vat[key] = b.Rate()
			basis[key] = b
		}
		snap["vat"] = vat
		snap["vat_basis"] = basis
	}
	return snap, nil
}

// PublishRows — строки формы МП для приёмника. Отдаём ВСЕ денежные строки с
// code_pl: какие из них публиковать и под каким «Параметром», решает маппинг
// (publish_mapping) — он же ждёт ответов BI по §12.
func (p *MpCardProvider) PublishRows(ctx context.Context, c Card) ([]PublishRow, error) {
	metrics, err := p.store.MetricsAll(ctx, c.PlID, c.Year, c.Month)
	if err != nil {
		return nil, err
	}
	segments := mpSegmentsOf(c)
	attrs := map[int]MarketplaceRow{}
	for _, mp := range MarketplaceSeed() {
		attrs[mp.CodeCFO] = mp
	}
	segList := make([]string, 0, len(segments))
	for seg := range segments {
		segList = append(segList, seg)
	}
	planByPL := mpPlanByPL(ctx, p.fact, c.Year, c.Month, segList)

	out := make([]PublishRow, 0, len(metrics))
	row := func(m MetricRow, mp MarketplaceRow, codePL int, value float64) PublishRow {
		return PublishRow{
			BlockType: m.BlockType,
			Country:   mp.Country,
			CodeCFO:   m.ProfitCenter,
			GroupCFO1: "4.Маркетплейсы",
			GroupCFO2: mp.Segment,
			CFOName:   mp.NameCFO,
			CodePL:    codePL,
			Date:      time.Date(m.Year, time.Month(m.Month), 1, 0, 0, 0, 0, time.UTC),
			Value:     value,
			Currency:  m.Currency,
		}
	}
	for _, m := range metrics {
		if m.LineCode <= 0 || m.ProfitCenter == 0 {
			continue // проценты и total-строки в приёмник не идут
			// комиссия площадки сюда же: у неё нет CodePL, и она уже сидит в
			// разнице между ценой менеджера и ценой площадки (правка п. 5)
		}
		mp := attrs[m.ProfitCenter]
		if mp.Segment != "" && !segments[mp.Segment] {
			continue
		}
		// Себестоимость общая показана в форме ОДНОЙ строкой, а в приёмник идёт
		// двумя: 2006 основная и 6006 пошив (правка 08.09.2026 п. 4).
		if m.BlockType == BCogsTotal {
			main, sewing := splitCogsByPlan(m.Amount, planByPL, m.ProfitCenter)
			out = append(out, row(m, mp, 2006, main))
			if sewing != 0 {
				out = append(out, row(m, mp, 6006, sewing))
			}
			continue
		}
		out = append(out, row(m, mp, m.LineCode, m.Amount))
	}

	// Общие затраты по МП под ЦФО 250 отсюда БОЛЬШЕ НЕ публикуются.
	//
	// Ответ аналитика 34 (10.09.2026): `mp_common_cost` остаётся хранилищем, но
	// читает и публикует его форма этапа 1.5 — вместе со статьями 51 и 52 реестра
	// логистики и строками ИТ с признаком «перенести на ЦФО 250». Ответ 11 про то
	// же с другой стороны: статьи 51, 52 и 54 по ЦФО 250 публикуются только из
	// формы 1.5, а в форме МП остаются расчётным индикатором для PL продаж и в
	// приёмник затрат не пишут.
	//
	// Если публиковать их и здесь, и там, под одним логическим ключом окажутся
	// два источника: свёртка сложит их в одну сумму молча, и приёмник получит
	// двойные затраты. Реестр при этом продолжает наполняться в форме МП — сменился
	// владелец публикации, а не владелец данных.
	_ = p.common
	return out, nil
}

// ЦФО общих затрат МП — «Отдел маркетплейса» (250), страна BY: подразделение
// белорусское, хотя площадки в нём российские (справочник ЦФО, правка 28.08.2026).
const (
	mpCommonCostCFO     = 250
	mpCommonCostCountry = "BY"
)

// mpSegmentByCfo — площадка → сегмент (из справочника): MetricRow сегмента не несёт.
func mpSegmentByCfo() map[int]string {
	out := map[int]string{}
	for _, mp := range MarketplaceSeed() {
		out[mp.CodeCFO] = mp.Segment
	}
	return out
}

// mpSegmentsOf — какие сегменты входят в карточку: scope_key='large'|'small',
// пустой/'all' — оба (карточка на весь блок МП).
func mpSegmentsOf(c Card) map[string]bool {
	switch c.ScopeKey {
	case "large":
		return map[string]bool{"large": true}
	case "small":
		return map[string]bool{"small": true}
	default:
		return map[string]bool{"large": true, "small": true}
	}
}

// mpTaskStepOwners — владельцы шагов маршрута для skip_if_same_user.
// Источник — владельцы этапов процесса (pl_stage_instance.owner_user_id):
// шаг карточки '1.2' соответствует этапу '1.2'. Шаг 'fin' (финансист) владельца
// в схеме этапов не имеет — его назначают через ABAC-срез.
type stageOwnerResolver struct{ tasks stageOwnerReader }

// stageOwnerReader — минимальный доступ к владельцам этапов (реализует TaskStore).
type stageOwnerReader interface {
	StageOwners(ctx context.Context, plID int64) ([]StageOwnerInfo, error)
}

// NewStageOwnerResolver — резолвер владельцев шагов из этапов процесса.
func NewStageOwnerResolver(tasks stageOwnerReader) StepOwnerResolver {
	return &stageOwnerResolver{tasks: tasks}
}

func (r *stageOwnerResolver) StepOwners(ctx context.Context, plID int64, formCode, scopeKey string) (map[string]int64, error) {
	if r.tasks == nil {
		return map[string]int64{}, nil
	}
	owners, err := r.tasks.StageOwners(ctx, plID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(owners))
	for _, o := range owners {
		if o.UserID != nil {
			out[o.StageCode] = *o.UserID
		}
	}
	return out, nil
}
