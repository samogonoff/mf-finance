package etl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// RunBootstrapGLMF — полная заливка одного ЮЛ из GLMF в finance.fact_glmf.
// Идемпотентна: чистит CH от прежних строк этого company_id и льёт с нуля.
// Чекпоинты — source='glmf' (отдельно от premaster). Зеркалит RunBootstrap.
func RunBootstrapGLMF(ctx context.Context, deps Deps, opts BootstrapOpts) (int64, error) {
	if opts.CompanyID == "" {
		return 0, fmt.Errorf("bootstrap-glmf: company_id required")
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 5000
	}
	if opts.TriggeredBy == "" {
		opts.TriggeredBy = "manual"
	}

	ch, err := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	if err != nil {
		return 0, fmt.Errorf("bootstrap-glmf: ch client: %w", err)
	}

	startedAt := time.Now()
	log.Printf("=== bootstrap-glmf company=%s batch=%d trigger=%s START ===",
		opts.CompanyID, opts.BatchSize, opts.TriggeredBy)

	if _, err := deps.PG.Exec(ctx, `
		INSERT INTO debt_etl_checkpoint (source, phase, company_id, started_at, updated_at, rows_loaded)
		VALUES ('glmf','bootstrap',$1,NOW(),NOW(),0)
		ON CONFLICT (source, phase, company_id) DO UPDATE
		   SET started_at = NOW(), updated_at = NOW(), rows_loaded = 0,
		       finished_at = NULL, error_text = NULL
	`, opts.CompanyID); err != nil {
		return 0, fmt.Errorf("bootstrap-glmf: checkpoint init: %w", err)
	}

	delSQL := fmt.Sprintf(
		"ALTER TABLE finance.fact_glmf DELETE WHERE company_id = '%s'",
		strings.ReplaceAll(opts.CompanyID, "'", "''"))
	if err := ch.exec(ctx, delSQL); err != nil {
		log.Printf("  warn: cleanup CH for %s: %v (продолжаем)", opts.CompanyID, err)
	}

	rowsLoaded, runErr := streamGLMF(ctx, deps, ch, opts, startedAt)

	finishedAt := time.Now()
	durSec := finishedAt.Sub(startedAt).Seconds()
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}

	if runErr == nil {
		if _, err := deps.PG.Exec(ctx, `
			UPDATE debt_etl_checkpoint SET finished_at = NOW(), updated_at = NOW()
			 WHERE source='glmf' AND phase='bootstrap' AND company_id=$1`, opts.CompanyID); err != nil {
			log.Printf("  warn: checkpoint finalize: %v", err)
		}
	} else {
		if _, err := deps.PG.Exec(ctx, `
			UPDATE debt_etl_checkpoint SET error_text = $1, updated_at = NOW()
			 WHERE source='glmf' AND phase='bootstrap' AND company_id=$2`,
			errText, opts.CompanyID); err != nil {
			log.Printf("  warn: checkpoint error update: %v", err)
		}
	}

	writeRunLog(ctx, ch, runLogEntry{
		CompanyID:   opts.CompanyID,
		Phase:       "bootstrap-glmf",
		TriggeredBy: opts.TriggeredBy,
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
		DurationSec: float32(durSec),
		RowsLoaded:  uint64(rowsLoaded),
		ErrorText:   errText,
	})

	if runErr != nil {
		return rowsLoaded, runErr
	}
	log.Printf("=== bootstrap-glmf company=%s — DONE %d rows in %.1fs ===",
		opts.CompanyID, rowsLoaded, durSec)
	return rowsLoaded, nil
}

// streamGLMF — SELECT cursor (GLMF, ВГО-фильтр) → batches → fact_glmf.
func streamGLMF(ctx context.Context, deps Deps, ch *chClient, opts BootstrapOpts, startedAt time.Time) (int64, error) {
	// Фильтр по Company-коду (кластерный индекс GLMF) — иначе full scan 209M строк.
	// БЕЗ ORDER BY: сортировка результата заставляет MSSQL ждать до первой строки
	// (батчи не идут); ReplacingMergeTree схлопнёт дубли и так, порядок не важен.
	coClause, coArgs := glmfCompanyWhere(opts.CompanyID)
	vgoClause, vgoArgs := vgoFilter()
	q := extractGLMFSelectFrom() + `
WHERE ` + coClause + vgoClause

	args := append(coArgs, vgoArgs...)
	rows, err := deps.MSSQL.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()

	batch := make([]glmfRow, 0, opts.BatchSize)
	var totalLoaded int64

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
		if err := ch.insertJSON(ctx, "finance.fact_glmf", &buf); err != nil {
			return err
		}
		totalLoaded += int64(len(batch))
		batch = batch[:0]

		if _, err := deps.PG.Exec(ctx, `
			UPDATE debt_etl_checkpoint SET rows_loaded = $1, updated_at = NOW()
			 WHERE source='glmf' AND phase='bootstrap' AND company_id=$2`,
			totalLoaded, opts.CompanyID); err != nil {
			log.Printf("  warn: checkpoint update: %v", err)
		}
		elapsed := time.Since(startedAt).Seconds()
		log.Printf("  glmf loaded=%d elapsed=%.1fs rate=%.0f rows/s", totalLoaded, elapsed, float64(totalLoaded)/elapsed)
		return nil
	}

	for rows.Next() {
		r, err := scanGLMFRow(rows)
		if err != nil {
			return totalLoaded, err
		}
		batch = append(batch, r)
		if len(batch) >= opts.BatchSize {
			if err := flush(); err != nil {
				return totalLoaded, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return totalLoaded, fmt.Errorf("rows: %w", err)
	}
	if err := flush(); err != nil {
		return totalLoaded, err
	}
	return totalLoaded, nil
}
