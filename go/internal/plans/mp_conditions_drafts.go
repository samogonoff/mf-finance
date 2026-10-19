package plans

import (
	"fmt"
	"regexp"
)

var mpShareSourceRe = regexp.MustCompile(`^strategy:\d{4}-(0[1-9]|1[0-2])$`)

// mpShareSource — допустимый признак источника доли: пусто (вручную) или
// «strategy:YYYY-MM». Всё остальное (в т.ч. присланное клиентом) сбрасывается в
// «вручную»: источник не должен превращаться в произвольный текст в БД.
func mpShareSource(s string) string {
	if mpShareSourceRe.MatchString(s) {
		return s
	}
	return ""
}

// Несохранённые правки реестра условий и эффективный %СПП для проверок.

// applyConditionDrafts накладывает черновые условия (то, что введено на экране,
// но ещё не сохранено) на условия из БД — только для предпросмотра пересчёта:
// ничего не пишется. Площадка обязана входить в срез и сегмент карточки, значения
// проходят ту же проверку диапазонов, что и сохранение (validateConditions).
func applyConditionDrafts(base map[int]MpConditions, drafts []MpConditions, platforms []MarketplaceRow) (map[int]MpConditions, error) {
	allowed := make(map[int]bool, len(platforms))
	for _, p := range platforms {
		allowed[p.CodeCFO] = true
	}
	out := make(map[int]MpConditions, len(base)+len(drafts))
	for k, v := range base {
		out[k] = v
	}
	for _, d := range drafts {
		if !allowed[d.CodeCFO] {
			return nil, fmt.Errorf("площадка %d вне вашего среза или сегмента карточки", d.CodeCFO)
		}
		if err := validateConditions(d); err != nil {
			return nil, fmt.Errorf("площадка %d: %w", d.CodeCFO, err)
		}
		cur := out[d.CodeCFO] // сохраняет НДС, валюту и версию сохранённых условий
		cur.CodeCFO = d.CodeCFO
		cur.SppPct, cur.MarkupPct, cur.MarkupTotalPct = d.SppPct, d.MarkupPct, d.MarkupTotalPct
		cur.Shares = make(map[string]float64, len(d.Shares))
		for b, v := range d.Shares {
			cur.Shares[b] = v
		}
		out[d.CodeCFO] = cur
	}
	return out, nil
}

// mpMissingShare — у статьи формы нет доли в условиях площадки (ни ручной, ни из
// стратегии). Комиссия площадки исключена: это не доля, а продажи менеджера без
// НДС × %СПП (computeMpPlatformInverse), поэтому «доля не задана» к ней не
// относится — иначе форма прятала бы посчитанную комиссию за «—».
func mpMissingShare(l MpLine, cond MpConditions) bool {
	if !l.CostLine || l.BlockType == BCommission {
		return false
	}
	_, has := cond.EffectiveShares()[l.BlockType]
	return !has
}

// conditionsWithFormSpp — условия для проверок формы с эффективным %СПП. В расчёте
// СПП площадки берётся из формы (ввод исполнителя, ФЗ 02.10 п. 3) и перекрывает
// значение реестра; проверка МП-01 «не задан %СПП» обязана смотреть на то же
// значение, иначе форма с введённым СПП блокировалась бы нулём в реестре.
//
// Правило то же, что в расчёте (Recalc, MpFormData): если СПП в форме есть —
// берётся он, в том числе ноль. Иначе нулевой СПП формы считался бы комиссией 0,
// а проверка смотрела бы на СПП реестра и пропускала форму с неверными суммами.
func conditionsWithFormSpp(conds map[int]MpConditions, spp map[int]float64) map[int]MpConditions {
	out := make(map[int]MpConditions, len(conds))
	for k, v := range conds {
		out[k] = v
	}
	for cfo, v := range spp {
		if c, ok := out[cfo]; ok {
			c.SppPct = v
			out[cfo] = c
		}
	}
	return out
}
