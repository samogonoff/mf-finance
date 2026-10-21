package plans

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Свод «Бюджет по ЮЛ» на реальных формах: ЮЛ — по ЦФО строки (Company), продажи берутся из свода товарооборота,
// корректировка меняет итог, снятая возвращает расчёт, после завершения этапа 4 корректировать нельзя.
func TestLeSvod_BuildAdjustRevoke_PG(t *testing.T) {
	cell := func(cfo int, company string, amt float64) ExpCostCell {
		return ExpCostCell{Scenario: ExpSrcTactic, Year: 2090, Month: 8, CodePL: 24, CodeCFO: cfo, Amount: amt, Company: company,
			Country: "BY", GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ", Expense: "ЗП", CFO: "Магазин"}
	}
	src := &stubExpSource{cells: []ExpCostCell{
		cell(101, "TDMF", 400),
		cell(102, "F", 100),
		{Scenario: ExpSrcTactic, Year: 2090, Month: 9, CodePL: 24, CodeCFO: 101, Amount: 999, Company: "TDMF", Country: "BY", GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ", Expense: "ЗП", CFO: "Магазин"},
	}}
	svc, cards, pool, pl, ctx := svodSyncFixture(t, src)
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM pl_le_adjust WHERE pl_id=$1`, pl) })

	list, err := cards.Cards(ctx, pl)
	if err != nil {
		t.Fatal(err)
	}
	var byCard int64
	for _, c := range list {
		if c.FormCode == TemplateExp && c.ScopeKey == "R-BY" {
			byCard = c.ID
		}
	}
	if byCard == 0 {
		t.Fatal("нет карточки расходов R-BY")
	}
	// Открытие формы запускает загрузку из источника; ждём, пока строки появятся.
	if _, err := svc.Form(ctx, Principal{UserID: 1, PlansAdmin: true}, byCard, ExpFilter{}); err != nil {
		t.Fatal(err)
	}
	_, inst, _ := svc.access(ctx, byCard)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if rows, _ := expAllRows(ctx, svc.repo, inst.ID); len(rows) >= 2 {
			break
		}
	}

	stageDone := false
	salesRows := []SvodRow{{LegalEntity: "F", Channel: ChannelRetailBY, Currency: "BYN", Amount: 10000}}
	le := NewLeSvodService(cards, svc, func(context.Context, int, int) ([]SvodRow, error) { return salesRows, nil },
		NewRateBook(nil), NewLeAdjustRepo(pool), func(context.Context, int64) ([]StageState, error) {
			st := "in_progress"
			if stageDone {
				st = "completed"
			}
			return []StageState{{Code: "4", Status: st}}, nil
		})

	s, err := le.Build(ctx, pl, ExpWindowMonth)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.Legals, "|"); got != "F|TD Mark Formelle" {
		t.Fatalf("колонки ЮЛ: %s (ЮЛ строки берётся по ЦФО: F и TDMF → TD Mark Formelle)", got)
	}
	if r := leRow(t, s, "sales:"+ChannelRetailBY); !leNear(r.Total["F"], 10000) {
		t.Errorf("продажи розницы РБ: %+v", r)
	}
	zp := leRow(t, s, "exp:Заработная плата")
	if !leNear(zp.Total["TD Mark Formelle"], 400) || !leNear(zp.Total["F"], 100) {
		t.Errorf("расходы по ЮЛ за август (сентябрь в окно «месяц» не входит): %+v", zp)
	}
	tail, err := le.Build(ctx, pl, ExpWindowTail)
	if err != nil {
		t.Fatal(err)
	}
	if zpTail := leRow(t, tail, "exp:Заработная плата"); !leNear(zpTail.Total["TD Mark Formelle"], 1399) {
		t.Errorf("окно «до конца года»: %+v", zpTail)
	}

	// Корректировка: расчёт остаётся, итог меняется.
	id, err := le.Adjust(ctx, pl, LeAdjust{LegalEntity: "F", LineKey: "exp:Заработная плата", AmountBYN: 50, Reason: "уточнение ФОТ", UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	s, _ = le.Build(ctx, pl, ExpWindowMonth)
	zp = leRow(t, s, "exp:Заработная плата")
	if !leNear(zp.Calc["F"], 100) || !leNear(zp.Total["F"], 150) || len(s.Adjustments) != 1 {
		t.Errorf("после корректировки: %+v / %d", zp, len(s.Adjustments))
	}
	if err := le.RevokeAdjust(ctx, pl, id, 1, ""); err == nil {
		t.Error("снятие без причины должно отклоняться")
	}
	if err := le.RevokeAdjust(ctx, pl, id, 1, "ошибочно"); err != nil {
		t.Fatal(err)
	}
	s, _ = le.Build(ctx, pl, ExpWindowMonth)
	if zp = leRow(t, s, "exp:Заработная плата"); !leNear(zp.Total["F"], 100) || len(s.Adjustments) != 1 || !s.Adjustments[0].Revoked {
		t.Errorf("после снятия: %+v / %d", zp, len(s.Adjustments))
	}
	if err := le.RevokeAdjust(ctx, pl, id, 1, "повторно"); err == nil {
		t.Error("повторное снятие должно давать ошибку")
	}

	// Период утверждён (этап 4 завершён) — корректировки закрыты.
	stageDone = true
	if _, err := le.Adjust(ctx, pl, LeAdjust{LegalEntity: "F", LineKey: "exp:Заработная плата", AmountBYN: 1, Reason: "поздно", UserID: 1}); err == nil || !strings.Contains(err.Error(), "утверждён") {
		t.Errorf("после этапа 4 корректировать нельзя: %v", err)
	}
	if s, _ = le.Build(ctx, pl, ExpWindowMonth); !s.Closed {
		t.Error("признак Closed должен быть выставлен")
	}
}
