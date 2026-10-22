package etl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Заливка потока FinDebt в CH (finance.fact_findebt). ВГО-контур мал (сотни тыс.
// строк-документов) — грузим одним потоком, без per-company цикла (в отличие от
// старого GLMF на 209M). Идемпотентно: bootstrap чистит снэпшоты >= @min и льёт
// заново; инкремент докатывает снэпшоты новее последнего в CH.

// FinDebtOpts — параметры заливки FinDebt.
type FinDebtOpts struct {
	Tables      FinDebtTables
	MinDate     string // нижняя граница снэпшотов 'YYYY-MM-DD' (bootstrap); пусто → '2021-01-01'
	BatchSize   int    // дефолт 5000
	TriggeredBy string // 'manual' | 'cli' | 'cron'
}

// chWatermark — max(snapshot_date) из CH → watermark: на ПУСТОЙ таблице max(Date) даёт
// 1970-01-01, а не пустую строку, поэтому оба значения означают «CH пуст».
func chWatermark(s string) string {
	if s == "" || strings.HasPrefix(s, "1970-01-01") {
		return ""
	}
	return s
}

// isoToYMD — 'YYYY-MM-DD' → 'YYYYMMDD' (стиль 112): логин MSSQL работает с DATEFORMAT dmy,
// и ISO-литерал в сравнении с datetime читался бы неверно.
func isoToYMD(s string) string { return strings.ReplaceAll(s, "-", "") }

// RunBootstrapFinDebt — полная заливка документов FinDebt3 в finance.fact_findebt
// начиная с MinDate. Чистит эти снэпшоты в CH и льёт заново. Возвращает (rows, error).
func RunBootstrapFinDebt(ctx context.Context, deps Deps, opts FinDebtOpts) (int64, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 5000
	}
	if opts.MinDate == "" {
		opts.MinDate = "2021-01-01"
	}
	if opts.TriggeredBy == "" {
		opts.TriggeredBy = "manual"
	}
	ch, err := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	if err != nil {
		return 0, fmt.Errorf("bootstrap-findebt: ch client: %w", err)
	}

	startedAt := time.Now()
	log.Printf("=== bootstrap-findebt min=%s batch=%d trigger=%s START ===",
		opts.MinDate, opts.BatchSize, opts.TriggeredBy)

	// Идемпотентность: сносим снэпшоты >= MinDate (повторная заливка).
	del := fmt.Sprintf("ALTER TABLE finance.fact_findebt_ccy DELETE WHERE snapshot_date >= toDate('%s')", sqlEscape(opts.MinDate))
	if err := ch.exec(ctx, del); err != nil {
		log.Printf("  warn: cleanup fact_findebt_ccy: %v (продолжаем)", err)
	}

	n, err := streamFinDebt(ctx, deps, ch, opts)
	if err != nil {
		return n, fmt.Errorf("bootstrap-findebt: %w", err)
	}

	// Обороты (продажи/оплаты) из FinDebt1 — отдельная таблица, та же идемпотентность.
	tn, err := syncFinDebtTurnover(ctx, deps, ch, opts, opts.MinDate)
	if err != nil {
		return n, fmt.Errorf("bootstrap-findebt turnover: %w", err)
	}
	log.Printf("=== bootstrap-findebt DONE rows=%d turnover=%d in %.1fs ===", n, tn, time.Since(startedAt).Seconds())
	return n, nil
}

// RunIncrementalFinDebt — докат снэпшотов новее последнего в CH. Берёт
// max(snapshot_date) из fact_findebt как нижнюю границу (инклюзивно — последний
// снэпшот мог быть неполным, ReplacingMergeTree обновит).
func RunIncrementalFinDebt(ctx context.Context, deps Deps, opts FinDebtOpts) (int64, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 5000 // иначе flush на каждую строку (воркер BatchSize не задаёт)
	}
	ch, err := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	if err != nil {
		return 0, fmt.Errorf("incremental-findebt: ch client: %w", err)
	}
	last, err := ch.queryString(ctx,
		"SELECT ifNull(toString(max(snapshot_date)), '') FROM finance.fact_findebt_ccy")
	last = chWatermark(last)
	if err != nil {
		return 0, fmt.Errorf("incremental-findebt: watermark: %w", err)
	}
	if last == "" {
		last = "2021-01-01" // CH пуст → полный догон
	}
	opts.MinDate = last
	del := fmt.Sprintf("ALTER TABLE finance.fact_findebt_ccy DELETE WHERE snapshot_date >= toDate('%s')", sqlEscape(last))
	if err := ch.exec(ctx, del); err != nil {
		log.Printf("  warn: incr cleanup fact_findebt_ccy: %v", err)
	}
	n, err := streamFinDebt(ctx, deps, ch, opts)
	if err != nil {
		return n, fmt.Errorf("incremental-findebt: %w", err)
	}
	if n > 0 {
		log.Printf("etl: incremental-findebt from=%s rows=%d", last, n)
	}

	// Обороты: свой watermark (таблица может отставать, если миграция 014 накатили
	// позже). Пустая таблица → полный догон с 2021.
	tlast, err := ch.queryString(ctx,
		"SELECT ifNull(toString(max(snapshot_date)), '') FROM finance.fact_findebt_turnover")
	tlast = chWatermark(tlast)
	if err != nil {
		return n, fmt.Errorf("incremental-findebt: turnover watermark: %w", err)
	}
	if tlast == "" {
		tlast = "2021-01-01"
	}
	tn, err := syncFinDebtTurnover(ctx, deps, ch, opts, tlast)
	if err != nil {
		return n, fmt.Errorf("incremental-findebt turnover: %w", err)
	}
	if tn > 0 {
		log.Printf("etl: incremental-findebt turnover from=%s rows=%d", tlast, tn)
	}
	return n, nil
}

// syncFinDebtTurnover — снести оборотные снэпшоты >= min и залить заново из FinDebt1.
// Fin1FQN пуст → поток выключен (no-op), документный поток от него не зависит.
func syncFinDebtTurnover(ctx context.Context, deps Deps, ch *chClient, opts FinDebtOpts, min string) (int64, error) {
	if opts.Tables.Fin1FQN == "" {
		return 0, nil
	}
	del := fmt.Sprintf("ALTER TABLE finance.fact_findebt_turnover DELETE WHERE snapshot_date >= toDate('%s')", sqlEscape(min))
	if err := ch.exec(ctx, del); err != nil {
		log.Printf("  warn: cleanup fact_findebt_turnover: %v (продолжаем)", err)
	}
	return streamFinDebtTurnover(ctx, deps, ch, opts, min)
}

// streamFinDebtTurnover — курсор FinDebt1 (оборотные строки) → батчи → fact_findebt_turnover.
func streamFinDebtTurnover(ctx context.Context, deps Deps, ch *chClient, opts FinDebtOpts, min string) (int64, error) {
	rows, err := deps.MSSQL.QueryContext(ctx, extractFinDebtTurnoverSQL(opts.Tables),
		sql.Named("grp", finDebtVGOFolder), sql.Named("min", isoToYMD(min)))
	if err != nil {
		return 0, fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()

	batch := make([]factFinDebtTurnoverRow, 0, opts.BatchSize)
	var total int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		for i := range batch {
			if err := enc.Encode(&batch[i]); err != nil {
				return fmt.Errorf("encode: %w", err)
			}
		}
		if err := ch.insertJSON(ctx, "finance.fact_findebt_turnover", &buf); err != nil {
			return err
		}
		total += int64(len(batch))
		batch = batch[:0]
		return nil
	}
	for rows.Next() {
		r, err := scanFinDebtTurnoverRow(rows)
		if err != nil {
			return total, err
		}
		batch = append(batch, r)
		if len(batch) >= opts.BatchSize {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, fmt.Errorf("rows: %w", err)
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, nil
}

// streamFinDebt — курсор FinDebt3 → батчи → finance.fact_findebt.
func streamFinDebt(ctx context.Context, deps Deps, ch *chClient, opts FinDebtOpts) (int64, error) {
	rows, err := deps.MSSQL.QueryContext(ctx, extractFinDebtSQL(opts.Tables),
		sql.Named("grp", finDebtVGOFolder), sql.Named("min", isoToYMD(opts.MinDate)))
	if err != nil {
		return 0, fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()

	batch := make([]factFinDebtRow, 0, opts.BatchSize)
	var total int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		for i := range batch {
			if err := enc.Encode(&batch[i]); err != nil {
				return fmt.Errorf("encode: %w", err)
			}
		}
		if err := ch.insertJSON(ctx, "finance.fact_findebt_ccy", &buf); err != nil {
			return err
		}
		total += int64(len(batch))
		batch = batch[:0]
		return nil
	}
	for rows.Next() {
		r, err := scanFinDebtRow(rows)
		if err != nil {
			return total, err
		}
		batch = append(batch, r)
		if len(batch) >= opts.BatchSize {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, fmt.Errorf("rows: %w", err)
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, nil
}
