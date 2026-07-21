package etl

import (
	"context"
	"log"
	"time"
)

// DebtArhWorker — фоновый полный reload debt_facts/turnover_facts (метод аналитика,
// DEBT_BACKEND=findebt-docdate). Сырые таблицы Payments обновляются посуточно.
type DebtArhWorker struct {
	deps    Deps
	tables  DebtArhTables
	minDate string
}

// NewDebtArhWorker — конструктор.
func NewDebtArhWorker(deps Deps, tables DebtArhTables, minDate string) *DebtArhWorker {
	return &DebtArhWorker{deps: deps, tables: tables, minDate: minDate}
}

// Start запускает фоновый цикл. interval<=0 → выключен. Первый прогон — через минуту.
func (w *DebtArhWorker) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		log.Printf("debtarh-worker: disabled (DEBTARH_SYNC_INTERVAL=0)")
		return
	}
	log.Printf("debtarh-worker: enabled, interval=%s", interval)
	go func() {
		t := time.NewTimer(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := RunDebtArhSync(ctx, w.deps, w.tables, w.minDate); err != nil {
					log.Printf("debtarh-worker: sync: %v", err)
				} else if n > 0 {
					log.Printf("debtarh-worker: synced %d rows", n)
				}
				t.Reset(interval)
			}
		}
	}()
}
