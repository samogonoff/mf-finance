package etl

import (
	"context"
	"log"
	"time"
)

// FinDWHWorker — фоновая реплика FinDWH → findwh. Тикает раз в interval; сама
// загрузка — раз в сутки на таблицу (RunFinDWHDaily пропускает уже загруженное
// и ещё не собранное в DWH), поэтому interval задаёт лишь, как быстро после
// готовности источника данные доедут до CH. Состояние — в findwh.etl_runs, так
// что перезапуск go-api не вызывает повторной загрузки.
type FinDWHWorker struct {
	deps Deps
	opts FinDWHOpts
}

// NewFinDWHWorker — конструктор.
func NewFinDWHWorker(deps Deps, opts FinDWHOpts) *FinDWHWorker {
	opts.TriggeredBy = "cron"
	opts.Force = false
	return &FinDWHWorker{deps: deps, opts: opts}
}

// Start запускает фоновый цикл. interval<=0 → воркер выключен (no-op).
func (w *FinDWHWorker) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		log.Printf("findwh-worker: disabled (FINDWH_SYNC_INTERVAL=0)")
		return
	}
	log.Printf("findwh-worker: enabled, interval=%s", interval)
	go func() {
		t := time.NewTimer(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := RunFinDWHDaily(ctx, w.deps, w.opts); err != nil {
					log.Printf("findwh-worker: %v", err)
				}
				t.Reset(interval)
			}
		}
	}()
}
