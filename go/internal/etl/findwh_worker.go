package etl

import (
	"context"
	"log"
	"sync"
	"time"
)

// FinDWHWorker — фоновая реплика FinDWH → findwh. Тикает раз в interval; сама
// загрузка — раз в сутки на таблицу (RunFinDWHDaily пропускает уже загруженное
// и ещё не собранное в DWH), поэтому interval задаёт лишь, как быстро после
// готовности источника данные доедут до CH. Состояние — в finance.findwh_etl_runs, так
// что перезапуск go-api не вызывает повторной загрузки.
type FinDWHWorker struct {
	deps Deps
	opts FinDWHOpts

	mu    sync.Mutex
	state FinDWHWorkerState
}

// FinDWHWorkerState — что воркер делает сейчас. Живёт в памяти процесса go-api:
// журнал finance.findwh_etl_runs пишется только по окончании таблицы, а GLMF
// грузится часами — без этого страница статуса не отличила бы «идёт загрузка»
// от «ещё не начинали». Загрузку из CLI (cmd/findebt-etl) отсюда не видно.
type FinDWHWorkerState struct {
	Enabled        bool       `json:"enabled"`
	DisabledReason string     `json:"disabled_reason,omitempty"`
	IntervalSec    int        `json:"interval_sec"`
	TickStartedAt  *time.Time `json:"tick_started_at,omitempty"` // тик идёт сейчас (с этого момента)
	LastTickEndAt  *time.Time `json:"last_tick_end_at,omitempty"`
	LastTickError  string     `json:"last_tick_error,omitempty"`
	NextTickAt     *time.Time `json:"next_tick_at,omitempty"`
	Running        string     `json:"running,omitempty"` // таблица, которая грузится сейчас
	RunningSince   *time.Time `json:"running_since,omitempty"`
	Progress       string     `json:"progress,omitempty"`
}

// NewFinDWHWorker — конструктор.
func NewFinDWHWorker(deps Deps, opts FinDWHOpts) *FinDWHWorker {
	opts.TriggeredBy = "cron"
	opts.Force = false
	w := &FinDWHWorker{deps: deps, opts: opts}
	w.opts.progress = w.onProgress
	return w
}

// State — снимок состояния для страницы статуса.
func (w *FinDWHWorker) State() FinDWHWorkerState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *FinDWHWorker) onProgress(table, note string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case table == "":
		w.state.Running, w.state.RunningSince = "", nil
	case w.state.Running != table:
		now := time.Now()
		w.state.Running, w.state.RunningSince = table, &now
	}
	w.state.Progress = note
}

// Start запускает фоновый цикл. interval<=0 → воркер выключен (no-op).
func (w *FinDWHWorker) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		w.mu.Lock()
		w.state.DisabledReason = "FINDWH_SYNC_INTERVAL=0"
		w.mu.Unlock()
		log.Printf("findwh-worker: disabled (FINDWH_SYNC_INTERVAL=0)")
		return
	}
	log.Printf("findwh-worker: enabled, interval=%s", interval)
	first := time.Now().Add(time.Minute)
	w.mu.Lock()
	w.state.Enabled, w.state.IntervalSec, w.state.NextTickAt = true, int(interval/time.Second), &first
	w.mu.Unlock()
	go func() {
		t := time.NewTimer(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				started := time.Now()
				w.mu.Lock()
				w.state.TickStartedAt = &started
				w.mu.Unlock()
				_, err := RunFinDWHDaily(ctx, w.deps, w.opts)
				if err != nil {
					log.Printf("findwh-worker: %v", err)
				}
				now := time.Now()
				next := now.Add(interval)
				w.mu.Lock()
				w.state.TickStartedAt, w.state.LastTickEndAt, w.state.NextTickAt = nil, &now, &next
				w.state.LastTickError = ""
				if err != nil {
					w.state.LastTickError = cleanErr(err)
				}
				w.state.Running, w.state.RunningSince, w.state.Progress = "", nil, ""
				w.mu.Unlock()
				t.Reset(interval)
			}
		}
	}()
}
