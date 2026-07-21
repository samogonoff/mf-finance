package etl

import (
	"context"
	"log"
	"time"
)

// CurrencyWorker — фоновая синхронизация справочников курса (dim_valuta,
// currency_daily) для второго потока отчёта ВГО (findebt-docdate). Курсы
// выгружаются посуточно, частить не нужно. Синхронизация stateless: valuta —
// полный reload, currency_daily — по watermark из CH.
type CurrencyWorker struct {
	deps   Deps
	tables CurrencyTables
}

// NewCurrencyWorker — конструктор.
func NewCurrencyWorker(deps Deps, tables CurrencyTables) *CurrencyWorker {
	return &CurrencyWorker{deps: deps, tables: tables}
}

// Start запускает фоновый цикл. interval<=0 → воркер выключен (no-op).
// Первый прогон — через минуту после старта (даём подняться зависимостям).
func (w *CurrencyWorker) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		log.Printf("currency-worker: disabled (CURRENCY_SYNC_INTERVAL=0)")
		return
	}
	log.Printf("currency-worker: enabled, interval=%s", interval)
	go func() {
		t := time.NewTimer(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := RunCurrencySync(ctx, w.deps, w.tables); err != nil {
					log.Printf("currency-worker: sync: %v", err)
				} else if n > 0 {
					log.Printf("currency-worker: synced %d rows", n)
				}
				t.Reset(interval)
			}
		}
	}()
}
