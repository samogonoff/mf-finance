package plans

import (
	"context"
	"time"
)

// Привязка формы «Розница» к карточке процесса: снимок версии и строки для
// публикации. Формо-специфичная часть общей оболочки (card_service.go) — по
// одному такому файлу на форму (ср. mp_card.go).

// RetailCardProvider — снимок и публикация формы TPL-TO-RETAIL.
type RetailCardProvider struct {
	repo  RetailRepo
	rates *RateBook
}

// NewRetailCardProvider — конструктор.
func NewRetailCardProvider(repo RetailRepo, rates *RateBook) *RetailCardProvider {
	return &RetailCardProvider{repo: repo, rates: rates}
}

// FormSnapshot — снимок версии карточки: значения + курсы + ПАРАМЕТРЫ ПЕРИОДА.
//
// Параметры в снимке обязательны: план розницы получается массовыми операциями
// (индекс роста, ФОТ, аренда — ТЗ §5), и без индекса/порогов утверждённые суммы
// невозможно воспроизвести. Курс фиксируется по той же причине, что у МП
// (ТЗ МП §3.3): справочник курсов меняется, утверждённая версия — нет.
func (p *RetailCardProvider) FormSnapshot(ctx context.Context, c Card) (map[string]any, error) {
	inst, err := p.repo.Instance(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	rows, err := p.repo.Rows(ctx, inst.ID)
	if err != nil {
		return nil, err
	}
	params, err := p.repo.Params(ctx, inst.ID)
	if err != nil {
		return nil, err
	}

	values := make([]map[string]any, 0, len(rows)*8)
	stores := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		// Снапшот атрибутов магазина — часть версии (ТЗ §2): по нему видно, с
		// каким LFL-статусом и РМ план согласовали.
		stores = append(stores, map[string]any{
			"code_cfo": r.CodeCFO, "klient_id": r.KlientID, "cfo": r.NameCFO,
			"city": r.City, "lfl_status": r.LFLStatus, "lfl_effective": r.LFLEffective,
			"lfl_override": r.LFLOverride, "store_type": r.StoreType, "category": r.Category,
			"reg_manager": r.RegManager, "legal_entity": r.LegalEntity,
			"date_open": r.DateOpen, "date_close": r.DateClose,
			"row_version": r.RowVersion, "comment": r.Comment,
		})
		for _, v := range r.Values {
			if v.Amount == nil {
				continue
			}
			values = append(values, map[string]any{
				"code_cfo": r.CodeCFO, "metric": v.Metric, "year": v.Year, "month": v.Month,
				"amount": *v.Amount, "source": v.Source, "note": v.Note,
			})
		}
	}

	paramRows := make([]map[string]any, 0, len(params))
	for _, pr := range params {
		paramRows = append(paramRows, map[string]any{
			"param_code": pr.ParamCode, "scope_kind": pr.ScopeKind, "scope_value": pr.ScopeValue,
			"value": pr.Value, "set_by": pr.SetBy, "set_at": pr.SetAt,
		})
	}

	snap := map[string]any{
		"form_code":    c.FormCode,
		"scope":        c.ScopeKey,
		"country":      inst.Country,
		"legal_entity": inst.LegalEntity,
		"currency":     inst.Currency,
		"period":       map[string]int{"year": inst.Year, "month": inst.Month},
		"stores":       stores,
		"values":       values,
		"params":       paramRows,
	}
	if p.rates != nil {
		snap["fx"] = p.rates.Snapshot(ctx, inst.Year, inst.Month, "BYN", "RUB", "USD", "KZT", "UZS")
	}
	return snap, nil
}

// PublishRows — строки формы для приёмника (ТЗ §7.2, §12).
//
// Публикуется ТОЛЬКО план продаж (Параметр 'ПРОДАЖИ'): ФОТ и аренда — расчётные
// статьи внутри формы, у них своих правил маппинга нет. Конкретный «Параметр» и
// КодPL подставляет publish_mapping (данные, а не код) — там же ждут ответов BI.
//
// ГруппыЦФО1 берём '3.Магазины' — значение ТАБЛИЦЫ ПЛАНА, а не справочника
// ('Магазины'): ТЗ §11 прямо предупреждает об этом расхождении.
func (p *RetailCardProvider) PublishRows(ctx context.Context, c Card) ([]PublishRow, error) {
	inst, err := p.repo.Instance(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	rows, err := p.repo.Rows(ctx, inst.ID)
	if err != nil {
		return nil, err
	}
	out := make([]PublishRow, 0, len(rows)*6)
	for _, r := range rows {
		for _, v := range r.Values {
			if v.Amount == nil || (v.Metric != "" && v.Metric != MetricSales) {
				continue
			}
			out = append(out, PublishRow{
				BlockType: "sales_plan",
				Param:     "ПРОДАЖИ",
				Country:   inst.Country,
				CodeCFO:   r.CodeCFO,
				GroupCFO1: retailPlanGroupCFO1,
				GroupCFO2: r.City,
				CFOName:   r.NameCFO,
				Date:      time.Date(v.Year, time.Month(v.Month), 1, 0, 0, 0, 0, time.UTC),
				Value:     *v.Amount,
				Currency:  inst.Currency,
			})
		}
	}
	return out, nil
}

// retailPlanGroupCFO1 — значение ГруппыЦФО1 в ТАБЛИЦЕ ПЛАНА. В справочнике
// магазинов та же группа называется 'Магазины' (см. dir_retail_group_map,
// миграция 0035): сопоставление идёт справочником, а не строковым сравнением.
const retailPlanGroupCFO1 = "3.Магазины"
