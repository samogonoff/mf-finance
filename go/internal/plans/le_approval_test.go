package plans

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEvalLeScope_AllOrAny(t *testing.T) {
	approvers := []LeApprover{
		{StageCode: "3", LegalEntity: "F", UserID: 1, UserName: "А"},
		{StageCode: "3", LegalEntity: "F", UserID: 2, UserName: "Б"},
		{StageCode: "3", LegalEntity: "MF IT", UserID: 3},
	}
	dec := func(user int64, legal, kind string, revoked bool) LeDecision {
		return LeDecision{StageCode: "3", LegalEntity: legal, UserID: user, Decision: kind, Revoked: revoked}
	}
	// Согласился один из двух: «нужны все» — нет, «достаточно одного» — да.
	one := []LeDecision{dec(1, "F", LeApprove, false)}
	if sc := evalLeScope("3", "F", approvers, true, one, 0); sc.Satisfied || sc.NoApprovers {
		t.Errorf("все: один из двух не должен хватать: %+v", sc)
	}
	if sc := evalLeScope("3", "F", approvers, false, one, 0); !sc.Satisfied {
		t.Errorf("один: достаточно одного согласия: %+v", sc)
	}
	// Оба согласились.
	both := append(one, dec(2, "F", LeApprove, false))
	if sc := evalLeScope("3", "F", approvers, true, both, 2); !sc.Satisfied || !sc.Approvers[1].Mine {
		t.Errorf("оба согласились: %+v", sc)
	}
	// Снятое согласие и согласие чужого ЮЛ не считаются; возврат — не согласие.
	other := []LeDecision{dec(1, "F", LeApprove, true), dec(2, "MF IT", LeApprove, false), dec(2, "F", LeReturn, false)}
	if sc := evalLeScope("3", "F", approvers, false, other, 0); sc.Satisfied {
		t.Errorf("снятое/чужое/возврат не считаются: %+v", sc)
	}
	// Согласие человека, которого убрали из списка, не считается.
	removed := []LeApprover{{StageCode: "3", LegalEntity: "F", UserID: 2}}
	if sc := evalLeScope("3", "F", removed, false, one, 0); sc.Satisfied {
		t.Errorf("согласие исключённого из списка не считается: %+v", sc)
	}
	// Список пуст.
	if sc := evalLeScope("3", "Formelle", approvers, true, nil, 0); !sc.NoApprovers || sc.Satisfied {
		t.Errorf("без согласующих область не выполнена: %+v", sc)
	}
	if got := policyOf(nil, "3", "F"); !got {
		t.Error("по умолчанию нужны все")
	}
	if got := policyOf([]LePolicy{{StageCode: "3", LegalEntity: "F", RequireAll: false}}, "3", "F"); got {
		t.Error("явная политика «один»")
	}
}

type leFlowFx struct {
	ctx     context.Context
	pl      int64
	le      *LeSvodService
	appr    *LeApprovalService
	flow    *Service
	store   MetricStore
	users   [4]int64
	adminP  Principal
	setDone func(codes ...string)
}

// leFlowFixture — период с готовым сводом (два ЮЛ: F и TD Mark Formelle), стек сервисов с проверками этапов и четыре пользователя.
func leFlowFixture(t *testing.T) *leFlowFx {
	t.Helper()
	cell := func(cfo int, company string, amt float64) ExpCostCell {
		return ExpCostCell{Scenario: ExpSrcTactic, Year: 2090, Month: 8, CodePL: 24, CodeCFO: cfo, Amount: amt, Company: company,
			Country: "BY", GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ", Expense: "ЗП", CFO: "Магазин"}
	}
	src := &stubExpSource{cells: []ExpCostCell{cell(101, "TDMF", 400), cell(102, "F", 100)}}
	svc, cards, pool, pl, ctx := svodSyncFixture(t, src)
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
	if _, err := svc.Form(ctx, Principal{UserID: 1, PlansAdmin: true}, byCard, ExpFilter{}); err != nil {
		t.Fatal(err)
	}
	_, inst, _ := svc.access(ctx, byCard)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if rows, _ := expAllRows(ctx, svc.repo, inst.ID); len(rows) >= 2 {
			break
		}
	}
	fx := &leFlowFx{ctx: ctx, pl: pl}
	for i := range fx.users {
		if err := pool.QueryRow(ctx, `INSERT INTO users(email) VALUES($1) RETURNING id`, fmt.Sprintf("le-approval-%d-%d@test.local", time.Now().UnixNano(), i)).Scan(&fx.users[i]); err != nil {
			t.Fatal(err)
		}
		id := fx.users[i]
		t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, id) })
	}
	t.Cleanup(func() {
		bg := context.Background()
		pool.Exec(bg, `DELETE FROM pl_final_approver WHERE user_id = ANY($1)`, fx.users[:])
		pool.Exec(bg, `DELETE FROM pl_final_policy WHERE legal_entity IN ('F','TD Mark Formelle','')`)
		pool.Exec(bg, `DELETE FROM pl_final_decision WHERE pl_id=$1`, pl)
		pool.Exec(bg, `DELETE FROM pl_le_adjust WHERE pl_id=$1`, pl)
	})
	store := NewPgStore(pool)
	fx.store = store
	fx.flow = NewService(store, nil, nil)
	sales := func(context.Context, int, int) ([]SvodRow, error) {
		return []SvodRow{{LegalEntity: "F", Channel: ChannelRetailBY, Currency: "BYN", Amount: 10000}}, nil
	}
	fx.le = NewLeSvodService(cards, svc, sales, NewRateBook(nil), NewLeAdjustRepo(pool), store.Stages)
	fx.appr = NewLeApprovalService(NewLeApprovalRepo(pool), fx.le, fx.flow, cards, store.Stages)
	fx.le.WithApprovals(fx.appr)
	fx.flow.WithStageGate(func(ctx context.Context, plID int64, code, action string) error {
		if err := fx.le.Gate(ctx, plID, code, action); err != nil {
			return err
		}
		return fx.appr.Gate(ctx, plID, code, action)
	})
	fx.adminP = Principal{UserID: fx.users[0], PlansAdmin: true}
	// Этапы периода: отмечаем выбранные завершёнными (зависимости соблюдены), остальные остаются ожидающими.
	fx.setDone = func(codes ...string) {
		st, err := fx.flow.Stages(ctx, pl, 2090, 8, "BY")
		if err != nil {
			t.Fatal(err)
		}
		for i := range st {
			st[i].Status = "pending"
			for _, c := range codes {
				if st[i].Code == c {
					st[i].Status = "completed"
				}
			}
		}
		if err := store.StagesSave(ctx, pl, st); err != nil {
			t.Fatal(err)
		}
	}
	return fx
}

func (f *leFlowFx) stage(t *testing.T, code string) string {
	t.Helper()
	st, err := f.store.Stages(f.ctx, f.pl)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range st {
		if s.Code == code {
			return s.Status
		}
	}
	return ""
}

func (f *leFlowFx) user(i int) Principal { return Principal{UserID: f.users[i]} }

// Этапы 3 и 4 завершаются только решениями согласующих: политика «все / один», возврат, корректировка сбрасывает согласия.
func TestLeApproval_Flow_PG(t *testing.T) {
	f := leFlowFixture(t)
	ctx := f.ctx
	// Настройка: по F согласуют двое (нужны оба), по TD Mark Formelle — один; итоговое утверждение — двое, достаточно любого.
	for _, c := range []struct {
		stage, legal string
		users        []int64
		all          bool
	}{
		{"3", "F", []int64{f.users[0], f.users[1]}, true},
		{"3", "TD Mark Formelle", []int64{f.users[0]}, true},
		{"4", "", []int64{f.users[2], f.users[3]}, false},
	} {
		if err := f.appr.SetApprovers(ctx, f.adminP, c.stage, c.legal, c.users, c.all); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.appr.SetApprovers(ctx, f.user(1), "3", "F", nil, true); err == nil {
		t.Error("список согласующих настраивает только администратор")
	}

	// Пока 1.6 не завершён — решений нет.
	f.setDone()
	if err := f.appr.Decide(ctx, f.pl, f.user(0), "3", "F", LeApprove, ""); err == nil || !strings.Contains(err.Error(), "ещё не завершён") {
		t.Fatalf("до завершения 1.6 решение недоступно: %v", err)
	}
	f.setDone("1.1", "1.2", "1.3", "1.4", "1.5", "1.6")

	// Чужой пользователь не согласует.
	if err := f.appr.Decide(ctx, f.pl, f.user(3), "3", "F", LeApprove, ""); err == nil || !strings.Contains(err.Error(), "не назначены") {
		t.Fatalf("не согласующий: %v", err)
	}
	// Ручное завершение этапа 3 без решений отклоняется.
	if _, err := f.flow.StageAction(ctx, f.adminP, f.pl, 2090, 8, "BY", "3", "approve", "", ""); err == nil || !strings.Contains(err.Error(), "решениями согласующих") {
		t.Fatalf("этап 3 вручную: %v", err)
	}

	// u0 согласует F и TD Mark Formelle: у F нужен ещё u1.
	if err := f.appr.Decide(ctx, f.pl, f.user(0), "3", "F", LeApprove, "ок"); err != nil {
		t.Fatal(err)
	}
	if err := f.appr.Decide(ctx, f.pl, f.user(0), "3", "TD Mark Formelle", LeApprove, ""); err != nil {
		t.Fatal(err)
	}
	if got := f.stage(t, "3"); got == "completed" {
		t.Fatal("F согласовано одним из двух — этап 3 завершён рано")
	}
	status, err := f.appr.Status(ctx, f.pl, f.user(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Stage3) != 2 || status.Stage3[0].LegalEntity != "F" || status.Stage3[0].Satisfied || !status.Stage3[0].CanDecide || status.Stage3[1].CanDecide {
		t.Fatalf("статус этапа 3 для u1: %+v", status.Stage3)
	}

	// Корректировка итога F сбрасывает согласие u0 по F (но не по TD Mark Formelle).
	if _, err := f.le.Adjust(ctx, f.pl, LeAdjust{LegalEntity: "F", LineKey: "exp:Заработная плата", AmountBYN: 10, Reason: "уточнение", UserID: f.users[1]}); err != nil {
		t.Fatalf("согласующий F вправе корректировать итог: %v", err)
	}
	status, _ = f.appr.Status(ctx, f.pl, f.user(1))
	approvedF := 0
	for _, a := range status.Stage3[0].Approvers {
		if a.Approved {
			approvedF++
		}
	}
	if approvedF != 0 || !status.Stage3[1].Satisfied {
		t.Fatalf("корректировка F должна снять согласия F и не трогать TDMF: %+v", status.Stage3)
	}
	if f.le.CanAdjust(ctx, f.user(3), "F") || !f.le.CanAdjust(ctx, f.user(1), "F") || f.le.CanAdjust(ctx, f.user(1), "TD Mark Formelle") {
		t.Error("права на корректировку: только администратор и согласующие этого ЮЛ")
	}

	// Оба согласуют F — этап 3 завершается сам.
	for _, u := range []int{0, 1} {
		if err := f.appr.Decide(ctx, f.pl, f.user(u), "3", "F", LeApprove, ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.stage(t, "3"); got != "completed" {
		t.Fatalf("этап 3 должен завершиться после всех согласий, сейчас %q", got)
	}
	// После завершения этапа 3 итоги закрыты.
	if _, err := f.le.Adjust(ctx, f.pl, LeAdjust{LegalEntity: "F", LineKey: "exp:Заработная плата", AmountBYN: 1, Reason: "поздно", UserID: f.users[1]}); err == nil {
		t.Error("после этапа 3 корректировать нельзя")
	}

	// Этап 4: достаточно любого из двух.
	if err := f.appr.Decide(ctx, f.pl, f.user(0), "4", "", LeApprove, ""); err == nil {
		t.Error("u0 не входит в список итогового утверждения")
	}
	if err := f.appr.Decide(ctx, f.pl, f.user(2), "4", "", LeReturn, ""); err == nil || !strings.Contains(err.Error(), "без причины") {
		t.Errorf("возврат без причины: %v", err)
	}
	// Возврат итогового утверждения: этап 4 на доработку, этап 3 снова в работе, согласия по ЮЛ сняты.
	if err := f.appr.Decide(ctx, f.pl, f.user(2), "4", "", LeReturn, "не сходится с Excel"); err != nil {
		t.Fatal(err)
	}
	if f.stage(t, "4") != "returned" || f.stage(t, "3") != "in_progress" {
		t.Fatalf("после возврата этапы: 3=%q 4=%q", f.stage(t, "3"), f.stage(t, "4"))
	}
	status, _ = f.appr.Status(ctx, f.pl, f.user(1))
	if status.Stage3[0].Satisfied || status.Stage3[1].Satisfied {
		t.Fatalf("после возврата с этапа 4 согласия ЮЛ сняты: %+v", status.Stage3)
	}

	// Второй проход: согласуем этап 3 заново и затем этап 4 одним человеком.
	for _, c := range []struct {
		u     int
		legal string
	}{{0, "F"}, {1, "F"}, {0, "TD Mark Formelle"}} {
		if err := f.appr.Decide(ctx, f.pl, f.user(c.u), "3", c.legal, LeApprove, ""); err != nil {
			t.Fatal(err)
		}
	}
	if f.stage(t, "3") != "completed" {
		t.Fatalf("этап 3 после повторных согласий: %q", f.stage(t, "3"))
	}
	if err := f.appr.Decide(ctx, f.pl, f.user(3), "4", "", LeApprove, "утверждаю"); err != nil {
		t.Fatal(err)
	}
	if f.stage(t, "4") != "completed" {
		t.Fatalf("этап 4 при политике «достаточно одного»: %q", f.stage(t, "4"))
	}
	if err := f.appr.Decide(ctx, f.pl, f.user(2), "4", "", LeApprove, ""); err == nil || !strings.Contains(err.Error(), "уже завершён") {
		t.Errorf("решение после завершения этапа: %v", err)
	}
}

// Возврат согласующим ЮЛ отправляет период на этап 1.6 и снимает согласия только этого ЮЛ.
func TestLeApproval_ReturnStage3_PG(t *testing.T) {
	f := leFlowFixture(t)
	ctx := f.ctx
	if err := f.appr.SetApprovers(ctx, f.adminP, "3", "F", []int64{f.users[0]}, true); err != nil {
		t.Fatal(err)
	}
	if err := f.appr.SetApprovers(ctx, f.adminP, "3", "TD Mark Formelle", []int64{f.users[1]}, true); err != nil {
		t.Fatal(err)
	}
	f.setDone("1.1", "1.2", "1.3", "1.4", "1.5", "1.6")
	if err := f.appr.Decide(ctx, f.pl, f.user(1), "3", "TD Mark Formelle", LeApprove, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.appr.Decide(ctx, f.pl, f.user(0), "3", "F", LeReturn, "нет продаж ИМ"); err != nil {
		t.Fatal(err)
	}
	if f.stage(t, "3") != "returned" || f.stage(t, "1.6") != "in_progress" {
		t.Fatalf("после возврата: 3=%q 1.6=%q", f.stage(t, "3"), f.stage(t, "1.6"))
	}
	status, _ := f.appr.Status(ctx, f.pl, f.user(0))
	if !status.Stage3[1].Satisfied {
		t.Error("согласие другого ЮЛ после возврата F сохраняется")
	}
	// 1.6 не завершён — решения этапа 3 снова недоступны.
	if err := f.appr.Decide(ctx, f.pl, f.user(0), "3", "F", LeApprove, ""); err == nil {
		t.Error("пока 1.6 не завершён заново, согласовывать нельзя")
	}
}
