package plans

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Обновление статусов по всем периодам меняет только tp_row.lfl_status: утверждённая карточка, ячейки и
// переопределение LFL остаются как были.
func TestRetailLFLRefresh_RepoPG(t *testing.T) {
	dsn := os.Getenv("PLANS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PLANS_TEST_PG_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	plID, err := NewPgStore(pool).EnsureInstance(ctx, 2099, 6)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM pl_instance WHERE id=$1`, plID)
	cards, err := NewCardService(NewCardStore(pool)).EnsureCards(ctx, plID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var card Card
	for _, c := range cards {
		if c.FormCode == TemplateRetail && c.ScopeKey == "BY" {
			card = c
		}
	}
	if card.ID == 0 {
		t.Fatal("карточка розницы BY не создана")
	}
	repo := NewRetailRepo(pool)
	inst, err := repo.EnsureInstance(ctx, card, 0)
	if err != nil {
		t.Fatal(err)
	}
	const cfo = 9900002
	if _, _, _, err := repo.SyncRows(ctx, inst, []RetailStore{{CodeCFO: cfo, NameCFO: "тест", City: "Минск", Country: "BY", LFLStatus: LFLNew}}, nil, 0); err != nil {
		t.Fatal(err)
	}
	// Период уже закрыт для правок: карточка утверждена.
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status='approved', locked=true WHERE id=$1`, card.ID); err != nil {
		t.Fatal(err)
	}
	var cardTS time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM form_card WHERE id=$1`, card.ID).Scan(&cardTS); err != nil {
		t.Fatal(err)
	}

	rows, err := repo.LFLRefreshRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mine *RetailLFLRefreshRow
	for i := range rows {
		if rows[i].CodeCFO == cfo && rows[i].CardID == card.ID {
			mine = &rows[i]
		}
	}
	if mine == nil || mine.Editable || mine.Status != LFLNew || mine.Year != 2099 || mine.Month != 6 || mine.Country != "BY" {
		t.Fatalf("строка периода не найдена или описана неверно: %+v", mine)
	}

	changes, res := planRetailLFLRefresh(rows, map[int]string{cfo: LFLYes})
	if res.RowsChanged != 1 || res.LockedCards != 1 {
		t.Fatalf("план: %+v", res)
	}
	if n, err := repo.ApplyLFLStatuses(ctx, changes); err != nil || n != 1 {
		t.Fatalf("запись: %d %v", n, err)
	}
	var st string
	var ts time.Time
	var status string
	if err := pool.QueryRow(ctx, `SELECT r.lfl_status, c.updated_at, c.status FROM tp_row r JOIN tp_instance i ON i.id=r.instance_id JOIN form_card c ON c.id=i.card_id WHERE r.code_cfo=$1 AND c.id=$2`, cfo, card.ID).Scan(&st, &ts, &status); err != nil {
		t.Fatal(err)
	}
	if st != LFLYes {
		t.Fatalf("статус строки: %q", st)
	}
	if !ts.Equal(cardTS) || status != "approved" {
		t.Fatalf("карточка не должна меняться: updated_at %v→%v, статус %q", cardTS, ts, status)
	}
	// Повтор — ничего не меняет.
	if n, err := repo.ApplyLFLStatuses(ctx, changes); err != nil || n != 0 {
		t.Fatalf("повторное применение: %d %v", n, err)
	}
}
