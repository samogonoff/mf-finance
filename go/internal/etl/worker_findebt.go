package etl

import (
	"context"
	"log"
	"time"
)

// FinDebtWorker — фоновый инкремент FinDebt → finance.fact_findebt по тикеру.
// FinDebt-вьюхи обновляются посуточно, поэтому частить не нужно (типовой интервал
// на проде — несколько часов). Инкремент сам берёт watermark из CH (последний
// снэпшот) и докатывает новые, поэтому воркер stateless.
type FinDebtWorker struct {
	deps Deps
	opts FinDebtOpts
}

// NewFinDebtWorker — конструктор. opts.Tables должен нести Fin3FQN; MinDate/др.
// для инкремента не используются (берётся из CH).
func NewFinDebtWorker(deps Deps, opts FinDebtOpts) *FinDebtWorker {
	opts.TriggeredBy = "cron"
	return &FinDebtWorker{deps: deps, opts: opts}
}

// Start запускает фоновый цикл. interval<=0 → воркер выключен (no-op).
// Первый прогон — через минуту после старта (даём подняться зависимостям).
func (w *FinDebtWorker) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		log.Printf("findebt-worker: disabled (FINDEBT_SYNC_INTERVAL=0)")
		return
	}
	log.Printf("findebt-worker: enabled, interval=%s", interval)
	go func() {
		t := time.NewTimer(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := RunIncrementalFinDebt(ctx, w.deps, w.opts); err != nil {
					log.Printf("findebt-worker: incremental: %v", err)
				} else if n > 0 {
					log.Printf("findebt-worker: incremental loaded %d rows", n)
				}
				t.Reset(interval)
			}
		}
	}()
}
