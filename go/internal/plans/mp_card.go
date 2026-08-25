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
	store MetricStore
	rates *RateBook
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
		vat := map[string]float64{}
		for _, mp := range MarketplaceSeed() {
			if segments[mp.Segment] {
				vat[itoa(int64(mp.CodeCFO))] = p.rates.Vat(ctx, mp.Country, mp.CodeCFO)
			}
		}
		snap["vat"] = vat
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
	out := make([]PublishRow, 0, len(metrics))
	for _, m := range metrics {
		if m.LineCode <= 0 || m.ProfitCenter == 0 {
			continue // проценты и total-строки в приёмник не идут
		}
		mp := attrs[m.ProfitCenter]
		if mp.Segment != "" && !segments[mp.Segment] {
			continue
		}
		out = append(out, PublishRow{
			BlockType: m.BlockType,
			Country:   mp.Country,
			CodeCFO:   m.ProfitCenter,
			GroupCFO1: "4.Маркетплейсы",
			GroupCFO2: mp.Segment,
			CFOName:   mp.NameCFO,
			CodePL:    m.LineCode,
			Date:      time.Date(m.Year, time.Month(m.Month), 1, 0, 0, 0, 0, time.UTC),
			Value:     m.Amount,
			Currency:  m.Currency,
		})
	}
	return out, nil
}

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
