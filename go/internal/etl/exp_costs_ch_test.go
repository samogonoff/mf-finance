package etl

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Решение «грузить ли сейчас» на настоящем ClickHouse (EXP_CH_TEST_URL=http://host:8123,
// пользователь finance/finance, применена миграция 013). ТОЛЬКО ВЫДЕЛЕННЫЙ CH ДЛЯ ТЕСТОВ:
// тест очищает журнал finance.exp_costs_etl_runs (иначе реальные записи текущего дня и
// свежие ошибки меняют решение). Без переменной — пропуск.
func TestExpCostsNeededOnRealCH(t *testing.T) {
	url := os.Getenv("EXP_CH_TEST_URL")
	if url == "" {
		t.Skip("EXP_CH_TEST_URL не задан")
	}
	ctx := context.Background()
	ch, err := newCHClient(url, "finance", "finance")
	if err != nil {
		t.Fatal(err)
	}
	clean := func() {
		_ = ch.exec(ctx, "TRUNCATE TABLE finance.exp_costs_etl_runs")
	}
	clean()
	defer clean()
	put := func(date, status string, ago time.Duration) {
		t.Helper()
		started := time.Now().Add(-ago).Unix()
		q := fmt.Sprintf("INSERT INTO finance.exp_costs_etl_runs VALUES (toDate('%s'), toDateTime(%d), toDateTime(%d), '%s', 1, 0, 'TEST', 'cli')", date, started, started, status)
		if err := ch.exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	opts := ExpCostsOpts{}
	opts.normalize() // окно 5–9, повтор после ошибки через час
	today := time.Now().Format("2006-01-02")

	// Снимок уже заливали сегодня — не грузим, даже в окне.
	put(today, "ok", time.Minute)
	if need, err := expCostsNeeded(ctx, ch, today, 6, opts); err != nil || need {
		t.Fatalf("после успешной заливки сегодня: need=%v err=%v", need, err)
	}
	clean()
	// В окне и сегодня не грузили — грузим. Вне окна при непустой таблице — нет.
	if need, err := expCostsNeeded(ctx, ch, today, 6, opts); err != nil || !need {
		t.Fatalf("в окне 5–9: need=%v err=%v", need, err)
	}
	have, _ := chCount(ctx, ch, "SELECT count() FROM "+expCostsTarget)
	need, err := expCostsNeeded(ctx, ch, today, 14, opts)
	if err != nil {
		t.Fatal(err)
	}
	if want := have == 0; need != want {
		t.Fatalf("вне окна (таблица %d строк): need=%v, want %v", have, need, want)
	}
	// Свежая ошибка — не долбим источник, даже в окне.
	put(today, "error", 10*time.Minute)
	if need, err := expCostsNeeded(ctx, ch, today, 6, opts); err != nil || need {
		t.Fatalf("после свежей ошибки: need=%v err=%v", need, err)
	}
	clean()
	// Старая ошибка (больше часа назад) — повторяем.
	put(today, "error", 2*time.Hour)
	if need, err := expCostsNeeded(ctx, ch, today, 7, opts); err != nil || !need {
		t.Fatalf("после старой ошибки: need=%v err=%v", need, err)
	}
}