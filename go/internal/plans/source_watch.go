package plans

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Контроль источников после выката (ТЗ доработок кабинета, п. 1.1).
//
// Источники факта/плана «перестали отвечать» незаметно: форма показывала пустую
// сетку, а в логах были только строки «недоступен». Здесь каждая загрузка
// источника пишется в plans_source_run и сравнивается с предыдущим ЗДОРОВЫМ
// прогоном того же источника и среза. Ошибка или падение числа строк больше
// порога — алерт: warn в лог (уходит в ELK) и уведомление администраторам.
//
// Алерт поднимается на ПЕРЕХОДЕ «здоров → нет»: розница перечитывает источники
// каждые 10 минут, и без этого один сломанный источник засыпал бы админов.

// SourceAlertFunc — доставка алерта (уведомление админам). Может быть nil.
type SourceAlertFunc func(ctx context.Context, title, message string)

// SourceWatch — журнал прогонов источников.
type SourceWatch struct {
	pool      *pgxpool.Pool
	threshold float64 // доля падения строк, после которой — алерт (0.3 = −30 %)
	alert     SourceAlertFunc
}

// NewSourceWatch — конструктор. thresholdPct — порог в процентах (≤0 → 30).
func NewSourceWatch(pool *pgxpool.Pool, thresholdPct float64, alert SourceAlertFunc) *SourceWatch {
	if thresholdPct <= 0 {
		thresholdPct = 30
	}
	return &SourceWatch{pool: pool, threshold: thresholdPct / 100, alert: alert}
}

// sourceVerdict — здоров ли прогон относительно предыдущего здорового.
// prev < 0 — сравнивать не с чем (первый прогон).
func sourceVerdict(rows, prev int, loadErr error, threshold float64) (healthy bool, reason string) {
	if loadErr != nil {
		return false, "источник не отвечает: " + loadErr.Error()
	}
	if prev <= 0 {
		return true, ""
	}
	if float64(rows) < float64(prev)*(1-threshold) {
		return false, fmt.Sprintf("строк %d против %d в предыдущем успешном прогоне (−%.0f %%, порог %.0f %%)",
			rows, prev, (1-float64(rows)/float64(prev))*100, threshold*100)
	}
	return true, ""
}

// Record — записать прогон и при переходе в нездоровое состояние поднять алерт.
// Ошибки журнала только логируются: контроль не должен ронять загрузку данных.
func (w *SourceWatch) Record(ctx context.Context, source, scope string, rows int, loadErr error) {
	if w == nil || w.pool == nil {
		return
	}
	prev, lastHealthy := -1, true
	var prevRows *int
	err := w.pool.QueryRow(ctx, `
		SELECT (SELECT rows_total FROM plans_source_run
		         WHERE source = $1 AND scope = $2 AND healthy ORDER BY created_at DESC LIMIT 1),
		       COALESCE((SELECT healthy FROM plans_source_run
		         WHERE source = $1 AND scope = $2 ORDER BY created_at DESC LIMIT 1), TRUE)`,
		source, scope).Scan(&prevRows, &lastHealthy)
	if err != nil && err != pgx.ErrNoRows {
		log.Printf("plans source-watch: журнал недоступен (%s %s): %v", source, scope, err)
		return
	}
	if prevRows != nil {
		prev = *prevRows
	}
	healthy, reason := sourceVerdict(rows, prev, loadErr, w.threshold)
	// Алерт — только на переходе: предыдущий прогон был здоров (или его не было).
	alert := !healthy && lastHealthy
	errText := ""
	if loadErr != nil {
		errText = loadErr.Error()
	}
	if _, err := w.pool.Exec(ctx, `
		INSERT INTO plans_source_run (source, scope, rows_total, error_text, healthy, alerted, prev_rows)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		source, scope, rows, errText, healthy, alert, prevRows); err != nil {
		log.Printf("plans source-watch: прогон не записан (%s %s): %v", source, scope, err)
	}
	if !alert {
		return
	}
	title := fmt.Sprintf("Источник планов: %s", source)
	msg := fmt.Sprintf("%s (%s): %s", source, scope, reason)
	slog.Warn("plans source-watch: источник деградировал",
		"source", source, "scope", scope, "rows", rows, "prev_rows", prev, "reason", reason)
	if w.alert != nil {
		// Доставка — вне горячего пути загрузки: уведомления пишутся в БД и Б24.
		go func() {
			actx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			w.alert(actx, title, msg)
		}()
	}
}
