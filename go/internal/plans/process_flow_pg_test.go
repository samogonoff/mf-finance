package plans

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// СКВОЗНОЙ ПРОГОН ПРОЦЕССА на живой БД: заполнение → согласование 1.2 → 1.3 →
// утверждение 1.4 → переоткрытие. Проверяется не отдельная функция, а то, что
// три сущности процесса (этап, задание, карточка) остаются согласованными на
// каждом шаге — ровно то, что расходилось до правок 04.09.2026.
//
// Тест требует БД, поэтому гейтован переменной окружения:
//
//	PLANS_TEST_PG_DSN='postgres://finance:finance@localhost:55432/finance' \
//	  go test ./internal/plans/ -run TestProcessFlow_PG -v
//
// Он работает на СВОЁМ периоде (год 2099), чтобы не задевать данные, которые
// смотрят руками, и убирает его за собой.

func TestProcessFlow_PG(t *testing.T) {
	dsn := os.Getenv("PLANS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PLANS_TEST_PG_DSN не задан — сквозной прогон процесса пропущен")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("подключение к БД: %v", err)
	}
	defer pool.Close()

	const testYear, testMonth = 2099, 7
	store := NewPgStore(pool)
	cardStore := NewCardStore(pool)
	tasks := NewTaskStore(pool, nil)

	// Период своего года: чужие данные не трогаем.
	cleanup := func() {
		var plID int64
		if err := pool.QueryRow(ctx,
			`SELECT id FROM pl_instance WHERE period_year=$1 AND period_month=$2`,
			testYear, testMonth).Scan(&plID); err == nil {
			pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, plID) //nolint:errcheck
		}
	}
	cleanup()
	defer cleanup()

	plID, err := store.EnsureInstance(ctx, testYear, testMonth)
	if err != nil {
		t.Fatalf("создание периода: %v", err)
	}

	svc := NewCardService(cardStore).
		WithStages(store).
		WithFillTasks(tasks)

	// ── Шаг 0: карточки форм и этапы периода ────────────────────────────────
	cards, err := svc.EnsureCards(ctx, plID, 0)
	if err != nil {
		t.Fatalf("создание карточек: %v", err)
	}
	if len(cards) == 0 {
		t.Fatal("карточки форм не созданы — процессу не с чем работать")
	}
	seed := initStages(testYear, testMonth, "BY", CalendarSeed(), RouteSeed())
	if err := store.StagesInit(ctx, plID, seed); err != nil {
		t.Fatalf("инициализация этапов: %v", err)
	}
	if _, err := tasks.Generate(ctx, plID); err != nil {
		t.Fatalf("генерация заданий: %v", err)
	}

	// Все карточки в черновиках → этапы согласования НЕ закрыты.
	stages := stagesNow(ctx, t, store, plID)
	if st := stageStatus(stages, "1.2"); st == "completed" {
		t.Fatalf("этап 1.2 не может быть завершён при черновиках, got %q", st)
	}

	// ── Шаг 1: заполняющий отправляет ОДНУ форму розницы ───────────────────
	target := cardByScope(t, cards, TemplateRetail, "BY")
	if _, err := svc.Action(ctx, target.ID, CardActionInput{Action: "submit", ActorID: 0}); err != nil {
		t.Fatalf("отправка формы на согласование: %v", err)
	}
	got := cardNow(ctx, t, cardStore, target.ID)
	if got.Status != CardOnApproval {
		t.Fatalf("после отправки статус: got %q, want %q", got.Status, CardOnApproval)
	}
	if got.StepCode == "1.1" {
		t.Fatal("после отправки карточка обязана уйти с шага заполнения")
	}
	// Этап 1.1 ещё в работе: остальные формы не отправлены.
	stages = stagesNow(ctx, t, store, plID)
	if st := stageStatus(stages, "1.1"); st != "in_progress" {
		t.Fatalf("этап 1.1 при частичной отправке: got %q, want in_progress", st)
	}
	// Задание страны, чью форму отправили, закрыто — а остальные нет.
	if st := taskStatus(ctx, t, pool, plID, "Товарооборот розницы · РБ"); st != TaskDone {
		t.Fatalf("задание отправленной формы: got %q, want %q", st, TaskDone)
	}
	if st := taskStatus(ctx, t, pool, plID, "Товарооборот розницы · РФ"); st == TaskDone {
		t.Fatal("задание другой страны не должно закрываться")
	}

	// ── Шаг 2: возврат руководителем на доработку ──────────────────────────
	if _, err := svc.Action(ctx, target.ID, CardActionInput{
		Action: "return", TargetStep: "1.1", Comment: "пересчитать август", ActorID: 0,
	}); err != nil {
		t.Fatalf("возврат формы: %v", err)
	}
	got = cardNow(ctx, t, cardStore, target.ID)
	if got.Status != CardReturned || got.StepCode != "1.1" {
		t.Fatalf("после возврата: got %q/%q, want returned/1.1", got.Status, got.StepCode)
	}
	// Возврат виден на таймлайне и вернул задание в работу.
	stages = stagesNow(ctx, t, store, plID)
	if st := stageStatus(stages, "1.1"); st != "returned" {
		t.Fatalf("этап 1.1 после возврата: got %q, want returned", st)
	}
	if st := taskStatus(ctx, t, pool, plID, "Товарооборот розницы · РБ"); st != TaskReturned {
		t.Fatalf("задание после возврата: got %q, want %q", st, TaskReturned)
	}

	// ── Шаг 3: все формы проходят маршрут до утверждения ───────────────────
	for _, c := range cards {
		walkCardToApproved(ctx, t, svc, cardStore, c.ID)
	}
	stages = stagesNow(ctx, t, store, plID)
	for _, code := range []string{"1.1", "1.2", "1.3", "1.4"} {
		if st := stageStatus(stages, code); st != "completed" {
			t.Fatalf("после утверждения всех форм этап %s: got %q, want completed", code, st)
		}
	}
	// Этап производства карточками не управляется — остался как был.
	if st := stageStatus(stages, "2.1"); st == "completed" {
		t.Fatal("этап производства не должен закрываться из-за форм продаж")
	}
	// Задания заполнения закрыты все.
	for _, title := range []string{
		"Товарооборот розницы · РБ", "Товарооборот розницы · РФ",
		"Товарооборот розницы · KZ", "Товарооборот розницы · UZ",
	} {
		if st := taskStatus(ctx, t, pool, plID, title); st != TaskDone {
			t.Fatalf("задание %q: got %q, want %q", title, st, TaskDone)
		}
	}

	// ── Шаг 4: утверждённая форма закрыта на запись, правится только reopen ─
	last := cardNow(ctx, t, cardStore, target.ID)
	if !last.Locked {
		t.Fatal("утверждённая карточка обязана быть закрыта на запись")
	}
	if _, err := svc.Action(ctx, target.ID, CardActionInput{Action: "submit", ActorID: 0}); err == nil {
		t.Fatal("отправка закрытой карточки должна быть отклонена")
	}
	if _, err := svc.Action(ctx, target.ID, CardActionInput{Action: "reopen", ActorID: 0}); err == nil {
		t.Fatal("переоткрытие без причины должно быть отклонено")
	}
	if _, err := svc.Action(ctx, target.ID, CardActionInput{
		Action: "reopen", Comment: "решение финблока", ActorID: 0,
	}); err != nil {
		t.Fatalf("переоткрытие с причиной: %v", err)
	}
	got = cardNow(ctx, t, cardStore, target.ID)
	if got.Status != CardDraft || got.Locked {
		t.Fatalf("после reopen: got %q locked=%v, want draft/false", got.Status, got.Locked)
	}
	// Таймлайн честно откатывается: этап 1.1 снова в работе.
	stages = stagesNow(ctx, t, store, plID)
	if st := stageStatus(stages, "1.1"); st == "completed" {
		t.Fatal("после переоткрытия этап 1.1 не может оставаться завершённым")
	}
}

// walkCardToApproved — провести карточку по маршруту до утверждения.
func walkCardToApproved(ctx context.Context, t *testing.T, svc *CardService, store CardStore, cardID int64) {
	t.Helper()
	for i := 0; i < 10; i++ {
		c, err := store.Card(ctx, cardID)
		if err != nil {
			t.Fatalf("карточка %d: %v", cardID, err)
		}
		switch c.Status {
		case CardApproved, CardPublished, CardPublishFailed:
			return
		}
		action := "approve"
		if c.StepCode == "1.1" {
			action = "submit"
		}
		if _, err := svc.Action(ctx, cardID, CardActionInput{Action: action, ActorID: 0}); err != nil {
			t.Fatalf("карточка %d, действие %s на шаге %s: %v", cardID, action, c.StepCode, err)
		}
	}
	t.Fatalf("карточка %d не дошла до утверждения за 10 переходов", cardID)
}

func cardByScope(t *testing.T, cards []Card, form, scope string) Card {
	t.Helper()
	for _, c := range cards {
		if c.FormCode == form && c.ScopeKey == scope {
			return c
		}
	}
	t.Fatalf("карточка %s/%s не найдена", form, scope)
	return Card{}
}

func cardNow(ctx context.Context, t *testing.T, store CardStore, id int64) Card {
	t.Helper()
	c, err := store.Card(ctx, id)
	if err != nil {
		t.Fatalf("чтение карточки %d: %v", id, err)
	}
	return c
}

func stagesNow(ctx context.Context, t *testing.T, store *pgStore, plID int64) []StageState {
	t.Helper()
	st, err := store.Stages(ctx, plID)
	if err != nil {
		t.Fatalf("чтение этапов: %v", err)
	}
	return st
}

func stageStatus(stages []StageState, code string) string {
	for _, s := range stages {
		if s.Code == code {
			return s.Status
		}
	}
	return ""
}

func taskStatus(ctx context.Context, t *testing.T, pool *pgxpool.Pool, plID int64, title string) string {
	t.Helper()
	var status string
	err := pool.QueryRow(ctx,
		`SELECT status FROM pl_task WHERE pl_id=$1 AND title=$2 LIMIT 1`, plID, title).Scan(&status)
	if err != nil {
		t.Fatalf("задание %q: %v", title, err)
	}
	return status
}
