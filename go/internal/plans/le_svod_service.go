package plans

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
)

// LeSvodService собирает свод «Бюджет по ЮЛ» из готовых форм периода и ведёт корректировки итогов.
type LeSvodService struct {
	cards  *CardService
	exp    *ExpService
	sales  func(ctx context.Context, year, month int) ([]SvodRow, error)
	rates  *RateBook
	adj    LeAdjustRepo
	stages func(ctx context.Context, plID int64) ([]StageState, error)
	// approvals — согласующие этапов 3 и 4: кто может корректировать итог своего ЮЛ и чьи согласия сбрасывает корректировка.
	approvals *LeApprovalService
}

// WithApprovals подключает согласование этапов 3 и 4.
func (s *LeSvodService) WithApprovals(a *LeApprovalService) *LeSvodService {
	s.approvals = a
	return s
}

// CanAdjust — пользователь вправе корректировать итог ЮЛ: администратор процессов или согласующий этого ЮЛ (этап 3).
func (s *LeSvodService) CanAdjust(ctx context.Context, p Principal, legal string) bool {
	if p.PlansAdmin {
		return true
	}
	return s.approvals != nil && s.approvals.IsApprover(ctx, "3", legal, p.UserID)
}

// NewLeSvodService — конструктор. sales — свод товарооборота по ЮЛ × каналу (Service.Svod), stages — этапы периода.
func NewLeSvodService(cards *CardService, exp *ExpService, sales func(ctx context.Context, year, month int) ([]SvodRow, error),
	rates *RateBook, adj LeAdjustRepo, stages func(ctx context.Context, plID int64) ([]StageState, error)) *LeSvodService {
	return &LeSvodService{cards: cards, exp: exp, sales: sales, rates: rates, adj: adj, stages: stages}
}

// leClosed — согласование ЮЛ (этап 3) или итоговое утверждение (этап 4) завершено: итоги закрыты для корректировок.
// Чтобы скорректировать, период возвращают на предыдущий этап.
func (s *LeSvodService) leClosed(ctx context.Context, plID int64) bool {
	if s.stages == nil {
		return false
	}
	st, err := s.stages(ctx, plID)
	if err != nil {
		return false
	}
	for _, x := range st {
		if (x.Code == "3" || x.Code == "4") && x.Status == "completed" {
			return true
		}
	}
	return false
}

// Build собирает свод периода. window — month | tail | year (как у сводов расходов); продажи тактики относятся к месяцу периода,
// окно действует на расходы.
func (s *LeSvodService) Build(ctx context.Context, plID int64, window string) (LeSvod, error) {
	cards, err := s.cards.Cards(ctx, plID)
	if err != nil {
		return LeSvod{}, err
	}
	if len(cards) == 0 {
		return LeSvod{}, expNoCardErr{"в периоде нет карточек форм"}
	}
	year, month := cards[0].Year, cards[0].Month
	from, to, win := expWindowMonths(window, month)
	in := leSvodInput{PlID: plID, Year: year, Month: month, Window: win, From: from, To: to}

	// Продажи: товарооборот сохранённой тактики по ЮЛ × каналу, в валюте строки → BYN.
	if s.sales != nil {
		rows, err := s.sales(ctx, year, month)
		if err != nil {
			in.Warnings = append(in.Warnings, "Продажи не прочитаны: "+err.Error())
		}
		for _, r := range rows {
			byn, ok := s.toBYN(ctx, r.Amount, r.Currency, year, month)
			if !ok {
				in.Warnings = append(in.Warnings, fmt.Sprintf("Нет курса к BYN для %s: продажи «%s» (%s) не вошли в свод.", r.Currency, r.Channel, r.LegalEntity))
				continue
			}
			in.Amounts = append(in.Amounts, leAmount{Legal: NormLegalEntity(r.LegalEntity), Key: "sales:" + r.Channel, Label: r.Channel, BYN: byn})
		}
	}

	// Расходы: строки карточек расходов, ЮЛ — по ЦФО строки, плановые месяцы окна.
	pnlOrder := map[string]bool{}
	for _, c := range cards {
		if c.FormCode != TemplateExp || strings.HasPrefix(c.ScopeKey, "CFO-") {
			continue
		}
		card, inst, err := s.exp.access(ctx, c.ID)
		if err != nil {
			in.Warnings = append(in.Warnings, fmt.Sprintf("%s: форма расходов не прочитана (%v).", c.Title, err))
			continue
		}
		kind, _ := expKindOf(c.ScopeKey)
		cardFrom, cardTo, _ := expWindowMonths(window, card.Month)
		fx := s.exp.expFx(ctx, inst.Currency, card.Year, card.Month, cardFrom)
		if fx.NoRate {
			in.Warnings = append(in.Warnings, fmt.Sprintf("%s: нет курса %s к BYN, расходы формы не вошли в свод.", c.Title, inst.Currency))
			continue
		}
		pnl := map[int]string{}
		if m, err := s.exp.pnlGroups(ctx); err == nil {
			pnl = m
		}
		pnl = expApplyDefaultPnlGroups(kind, pnl)
		rows, err := expAllRows(ctx, s.exp.repo, inst.ID)
		if err != nil {
			in.Warnings = append(in.Warnings, fmt.Sprintf("%s: строки расходов не прочитаны (%v).", c.Title, err))
			continue
		}
		if len(rows) == 0 && !cardEditable(c) {
			in.Warnings = append(in.Warnings, svodEmptyClosedWarning(c.Title))
		}
		for _, r := range rows {
			var plan float64
			for _, v := range r.Values {
				if v.Scenario == ExpScenarioPlan && v.Amount != nil && v.Month >= cardFrom && v.Month <= cardTo {
					plan += *v.Amount
				}
			}
			if plan == 0 {
				continue
			}
			group := pnl[r.CodePL]
			if group == "" {
				group = leNoGroup
			}
			legal := r.LegalEntity
			if strings.TrimSpace(legal) == "" {
				legal = inst.LegalEntity
			}
			in.Amounts = append(in.Amounts, leAmount{Legal: NormLegalEntity(legal), Key: "exp:" + group, Label: group, BYN: plan * fx.Tactic})
			pnlOrder[group] = true
		}
	}
	in.GroupOrder = expRetailPnlOrder

	adj, err := s.adj.List(ctx, plID)
	if err != nil {
		return LeSvod{}, err
	}
	in.Adjustments = adj
	in.Closed = s.leClosed(ctx, plID)
	out := buildLeSvod(in)
	out.Readiness = leReadiness(cards, out)
	out.Ready = len(leBlockers(out.Readiness)) == 0
	return out, nil
}

// toBYN переводит сумму в BYN по курсу сценария «тактика»; false — курса нет.
func (s *LeSvodService) toBYN(ctx context.Context, amount float64, cur string, year, month int) (float64, bool) {
	cur = strings.ToUpper(strings.TrimSpace(cur))
	if cur == "" || cur == "BYN" || s.rates == nil {
		return amount, true
	}
	if !s.rates.FxKnown(ctx, cur, year, month) {
		return 0, false
	}
	return s.rates.ConvertFor(ctx, amount, cur, "BYN", year, month, FxScenarioTactic), true
}

// Adjust добавляет корректировку итога. Закрытый период (этап 4 завершён) корректировать нельзя.
func (s *LeSvodService) Adjust(ctx context.Context, plID int64, a LeAdjust) (int64, error) {
	a.PlID = plID
	a.LegalEntity = strings.TrimSpace(a.LegalEntity)
	a.Reason = strings.TrimSpace(a.Reason)
	if err := validateLeAdjust(a); err != nil {
		return 0, err
	}
	if s.leClosed(ctx, plID) {
		return 0, errors.New("согласование ЮЛ уже завершено: корректировки закрыты (верните период на этап 3 или 1.6)")
	}
	id, err := s.adj.Add(ctx, a)
	if err != nil {
		return 0, err
	}
	if s.approvals != nil {
		s.approvals.OnAdjust(ctx, plID, a.LegalEntity)
	}
	log.Printf("plans/le-svod: корректировка pl=%d юл=%q строка=%q сумма=%.2f пользователь=%d", plID, a.LegalEntity, a.LineKey, a.AmountBYN, a.UserID)
	return id, nil
}

// RevokeAdjust снимает корректировку (остаётся в журнале с пометкой).
func (s *LeSvodService) RevokeAdjust(ctx context.Context, plID, id, userID int64, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("укажите причину снятия")
	}
	if s.leClosed(ctx, plID) {
		return errors.New("согласование ЮЛ уже завершено: корректировки закрыты (верните период на этап 3 или 1.6)")
	}
	a, err := s.adj.Revoke(ctx, plID, id, userID, strings.TrimSpace(reason))
	if err != nil {
		return err
	}
	if s.approvals != nil {
		s.approvals.OnAdjust(ctx, plID, a.LegalEntity)
	}
	return nil
}

// adjustLegal — ЮЛ корректировки (для проверки прав на её снятие).
func (s *LeSvodService) adjustLegal(ctx context.Context, plID, id int64) string {
	list, err := s.adj.List(ctx, plID)
	if err != nil {
		return ""
	}
	for _, a := range list {
		if a.ID == id {
			return a.LegalEntity
		}
	}
	return ""
}
