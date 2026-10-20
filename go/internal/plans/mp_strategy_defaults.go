package plans

import (
	"context"
	"math"
)

// Условия площадки (%СПП, наценки) вводятся в реестре. Пока они не сохранены, форма считала бы по нулям:
// цена площадки равна цене менеджера, себестоимость равна выручке, маржа и PL «не считаются». Для
// несохранённых условий форма берёт значения из стратегии того же месяца — как это делает режим «от сумм»
// (наценка от общей сс по умолчанию из плана, прототип, строка 65) — и сообщает об этом (StrategyDefaults).

// mpStrategyDefaults — %СПП и наценки площадки по плановому слою (ключи layerKey(cfo, block)).
// Возвращает значения и признак наличия по каждой величине; нет базы (нулевые или отрицательные суммы) —
// значения нет, а не «ноль из деления».
func mpStrategyDefaults(plan map[string]float64, cfo int) (spp, markup, markupTotal float64, okSpp, okMarkup, okMarkupTotal bool) {
	platNet := plan[layerKey(cfo, BSalesPlatNet)]
	mgrNet := plan[layerKey(cfo, BSalesManagerNet)]
	if platNet > 0 && mgrNet > 0 {
		spp, okSpp = round6(1-platNet/mgrNet), true
	}
	if ship := plan[layerKey(cfo, BShipments)]; platNet > 0 && ship > 0 {
		markup, okMarkup = round6(platNet/ship-1), true
	}
	if m, ok := planMarkupTotal(plan, cfo); ok {
		markupTotal, okMarkupTotal = round6(m), true
	}
	return
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// mpApplyStrategyDefaults подставляет в НЕСОХРАНЁННЫЕ условия (ID = 0) %СПП и наценки из стратегии.
// Сохранённые условия не трогаются: их нули — решение планировщика, а не пропуск.
func mpApplyStrategyDefaults(c MpConditions, plan map[string]float64) MpConditions {
	if c.ID != 0 {
		return c
	}
	spp, mk, mkt, okS, okM, okT := mpStrategyDefaults(plan, c.CodeCFO)
	if okS && c.SppPct == 0 {
		c.SppPct = spp
		c.StrategyDefaults = append(c.StrategyDefaults, "spp")
	}
	if okM && c.MarkupPct == 0 {
		c.MarkupPct = mk
		c.StrategyDefaults = append(c.StrategyDefaults, "markup")
	}
	if okT && c.MarkupTotalPct == 0 {
		c.MarkupTotalPct = mkt
		c.StrategyDefaults = append(c.StrategyDefaults, "markup_total")
	}
	return c
}

// mpApplyStrategyDefaultsAll — тот же вывод значений из стратегии для всех площадок сразу, ОДНИМ местом для
// всех путей: форма, пересчёт карточки и реестр «Условия площадок». Иначе форма показывала бы ненулевые
// маржу и PL, а пересчёт записывал бы в pl_metric нулевые. conds — условия по ЦФО (может не содержать площадку);
// у площадок без значений в стратегии запись не создаётся.
func mpApplyStrategyDefaultsAll(ctx context.Context, src MpFactSource, year, month int, platforms []MarketplaceRow, conds map[int]MpConditions) map[int]MpConditions {
	if src == nil || len(platforms) == 0 {
		return conds
	}
	segments := make([]string, 0, 2)
	for _, mp := range platforms {
		segments = append(segments, mp.Segment)
	}
	plan := mpPlanByBlock(ctx, src, year, month, segments)
	if len(plan) == 0 {
		return conds
	}
	out := make(map[int]MpConditions, len(conds))
	for k, v := range conds {
		out[k] = v
	}
	for _, mp := range platforms {
		c, has := out[mp.CodeCFO]
		if !has {
			c = MpConditions{CodeCFO: mp.CodeCFO, NameCFO: mp.NameCFO, Segment: mp.Segment, Country: mp.Country,
				Year: year, Month: month, Currency: platformCurrency(mp.Country), Shares: map[string]float64{}}
		}
		applied := mpApplyStrategyDefaults(c, plan)
		if has || len(applied.StrategyDefaults) > 0 {
			out[mp.CodeCFO] = applied
		}
	}
	return out
}