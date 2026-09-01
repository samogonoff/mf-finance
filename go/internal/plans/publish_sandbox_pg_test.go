package plans

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Единственный тест модуля, которому нужна живая БД: SQL песочницы (типы numeric,
// DATE, nullable SUM) на фейке не проверяется, а ошибка здесь всплыла бы ровно в
// тот момент, когда песочницей решили что-то проверить. Без переменной окружения
// PLANS_TEST_PG_DSN тест пропускается — общий прогон остаётся без БД.
//
//	PLANS_TEST_PG_DSN='postgres://finance:finance@localhost:55432/finance' \
//	  go test ./internal/plans/ -run TestSandboxPublisher_PG -v
func TestSandboxPublisher_PG(t *testing.T) {
	dsn := os.Getenv("PLANS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PLANS_TEST_PG_DSN не задан — тест песочницы на живой БД пропущен")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("подключение к БД: %v", err)
	}
	defer pool.Close()

	target := "Budgeting.dbo.VFORMTOLOADTAKTTARGET"
	p := NewSandboxPublisher(pool, []string{target})
	// Тестовый параметр — свой, чтобы прогон не мешал данным, которые смотрят руками.
	param := "ТЕСТ-ПЕСОЧНИЦА"
	if _, err := pool.Exec(ctx, `DELETE FROM publish_sandbox WHERE param = $1`, param); err != nil {
		t.Fatalf("очистка: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM publish_sandbox WHERE param = $1`, param) //nolint:errcheck

	rows := []PublishRow{
		{Param: param, Country: "BY", CodeCFO: 100, CodePL: 1001, CFOName: "Магазин 100",
			Date: pubDate(2026, 7), Value: 1234.5678, TargetTable: target},
		{Param: param, Country: "BY", CodeCFO: 101, CodePL: 1001, CFOName: "Магазин 101",
			Date: pubDate(2026, 7), Value: 765.4322, TargetTable: target},
	}

	written, sum, err := p.Write(ctx, target, rows)
	if err != nil {
		t.Fatalf("запись в песочницу: %v", err)
	}
	if written != 2 || sum != 2000 {
		t.Fatalf("ожидалось 2 строки и сумма 2000, получено %d / %.4f", written, sum)
	}

	// Повторная запись той же пачки не должна удваивать: DELETE+INSERT по ключу.
	if _, sum2, err := p.Write(ctx, target, rows); err != nil || sum2 != 2000 {
		t.Fatalf("повторная запись должна дать ту же сумму: %.4f (%v)", sum2, err)
	}
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM publish_sandbox WHERE param = $1`, param).Scan(&cnt); err != nil {
		t.Fatalf("подсчёт строк: %v", err)
	}
	if cnt != 2 {
		t.Fatalf("после повтора в песочнице должно остаться 2 строки, найдено %d", cnt)
	}

	// Probe по ключу, которого нет: NULL из SUM обязан стать нулём, а не ошибкой.
	empty := []PublishRow{{Param: param, Country: "BY", CodeCFO: 999, CodePL: 1001, Date: pubDate(2026, 8)}}
	if s, err := p.Probe(ctx, target, empty); err != nil || s != 0 {
		t.Fatalf("Probe по пустому ключу: %.4f (%v)", s, err)
	}

	reader, ok := p.(SandboxReader)
	if !ok {
		t.Fatal("песочница должна реализовывать SandboxReader")
	}
	got, err := reader.Rows(ctx, target, 10)
	if err != nil {
		t.Fatalf("чтение песочницы: %v", err)
	}
	found := 0
	for _, r := range got {
		if r.Param == param {
			found++
			if r.CodeCFO != 100 && r.CodeCFO != 101 {
				t.Fatalf("КодЦФО прочитан неверно: %+v", r)
			}
			if r.CodePL != 1001 {
				t.Fatalf("КодPL прочитан неверно: %+v", r)
			}
		}
	}
	if found != 2 {
		t.Fatalf("в срезе песочницы ожидались 2 наши строки, найдено %d", found)
	}
}
