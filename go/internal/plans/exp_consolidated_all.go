package plans

import (
	"context"
	"errors"
	"sort"
)

// Общий свод расходов «розница + интернет-магазин + опт» одной таблицей: строки — те же группы PL и статьи, колонки —
// каналы (суммы каждого канала уже в BYN). Собирается из готовых сводов каналов, поэтому правила окна, курсов и
// групп у него те же, что у каждого из них.

// ExpChannelAll — канал «все»: общий свод по каналам.
const ExpChannelAll = "all"

// Подписи колонок общего свода.
const (
	expAllLabelRetail = "Розница"
	expAllLabelIM     = "ИМ"
	expAllLabelOpt    = "Опт"
)

// expAllPart — свод одного канала на входе слияния; Err — почему канала нет в своде (нет карточки, нет доступа).
type expAllPart struct {
	Label string
	Cons  ExpConsolidated
	Err   string
}

// mergeExpConsolidatedChannels складывает своды каналов в один: колонка — канал, итог — сумма каналов. Канал с
// ошибкой остаётся в списке колонок с пояснением и в итог не входит.
func mergeExpConsolidatedChannels(plID int64, parts []expAllPart, order []string) ExpConsolidated {
	out := ExpConsolidated{Channel: ExpChannelAll, PlID: plID, Currency: "BYN", Total: newExpConsAmounts()}
	type key struct {
		group string
		code  int
	}
	lines := map[key]*ExpConsLine{}
	groups := map[string]*ExpConsGroup{}
	factBasisPayments := false
	for _, p := range parts {
		row := ExpConsCountry{Country: p.Label, NatCurrency: "BYN"}
		if p.Err != "" {
			row.Error = p.Err
			out.Countries = append(out.Countries, row)
			continue
		}
		if out.Year == 0 {
			out.Year, out.Month, out.Window, out.FromMonth, out.ToMonth = p.Cons.Year, p.Cons.Month, p.Cons.Window, p.Cons.FromMonth, p.Cons.ToMonth
		}
		factBasisPayments = factBasisPayments || p.Cons.FactBasis == ExpFactBasisPayments
		for _, c := range p.Cons.Countries {
			row.Rows += c.Rows
			if c.NoRate {
				row.NoRate = true
			}
		}
		row.Plan = p.Cons.Total.PlanTotal
		out.Countries = append(out.Countries, row)

		for _, g := range p.Cons.Groups {
			dst := groups[g.Group]
			if dst == nil {
				dst = &ExpConsGroup{Group: g.Group, Unassigned: g.Unassigned, ExpConsAmounts: newExpConsAmounts()}
				groups[g.Group] = dst
			}
			addChannelAmounts(&dst.ExpConsAmounts, g.ExpConsAmounts, p.Label)
			for _, l := range g.Lines {
				k := key{g.Group, l.CodePL}
				dl := lines[k]
				if dl == nil {
					dl = &ExpConsLine{CodePL: l.CodePL, Expense: l.Expense, ExpConsAmounts: newExpConsAmounts()}
					lines[k] = dl
				}
				if dl.Expense == "" {
					dl.Expense = l.Expense
				}
				addChannelAmounts(&dl.ExpConsAmounts, l.ExpConsAmounts, p.Label)
			}
		}
		addChannelAmounts(&out.Total, p.Cons.Total, p.Label)
	}
	if factBasisPayments {
		out.FactBasis = ExpFactBasisPayments
	}
	for k, l := range lines {
		groups[k.group].Lines = append(groups[k.group].Lines, *l)
	}
	pos := map[string]int{}
	for i, g := range order {
		pos[g] = i
	}
	for _, g := range groups {
		sort.SliceStable(g.Lines, func(a, b int) bool { return g.Lines[a].CodePL < g.Lines[b].CodePL })
		out.Groups = append(out.Groups, *g)
	}
	sort.SliceStable(out.Groups, func(a, b int) bool {
		ga, gb := out.Groups[a], out.Groups[b]
		if ga.Unassigned != gb.Unassigned {
			return !ga.Unassigned
		}
		pa, oka := pos[ga.Group]
		pb, okb := pos[gb.Group]
		if oka != okb {
			return oka
		}
		if oka && okb {
			return pa < pb
		}
		return ga.Group < gb.Group
	})
	return out
}

// addChannelAmounts кладёт итоги канала в колонку label и в общие суммы.
func addChannelAmounts(dst *ExpConsAmounts, src ExpConsAmounts, label string) {
	dst.Plan[label] += src.PlanTotal
	dst.PlanTotal += src.PlanTotal
	dst.FactLY += src.FactLY
	dst.FactPrev += src.FactPrev
	dst.FactCur += src.FactCur
}

// AllChannelsConsolidated — общий свод расходов по каналам периода. Канал без карточки или без доступа показан
// колонкой с пояснением и в итог не входит; если нет ни одного канала — ошибка.
func (s *ExpService) AllChannelsConsolidated(ctx context.Context, p Principal, plID int64, window string) (ExpConsolidated, error) {
	var parts []expAllPart
	add := func(label string, c ExpConsolidated, err error) {
		part := expAllPart{Label: label, Cons: c}
		if err != nil {
			part.Err = err.Error()
		}
		parts = append(parts, part)
	}
	rc, err := s.RetailConsolidated(ctx, p, plID, window)
	add(expAllLabelRetail, rc, err)
	ic, err := s.ChannelConsolidated(ctx, p, plID, ExpChannelIM, window)
	add(expAllLabelIM, ic, err)
	oc, err := s.ChannelConsolidated(ctx, p, plID, ExpChannelOpt, window)
	add(expAllLabelOpt, oc, err)

	ok := false
	for _, pt := range parts {
		ok = ok || pt.Err == ""
	}
	if !ok {
		return ExpConsolidated{}, errors.New("в периоде нет карточек расходов розницы, интернет-магазина и опта")
	}
	out := mergeExpConsolidatedChannels(plID, parts, expRetailPnlOrder)
	// Розница без части стран даёт неполный итог канала: переносим предупреждение.
	if rc.Channel != "" || len(rc.Missing) > 0 {
		out.Missing = rc.Missing
	}
	return out, nil
}
