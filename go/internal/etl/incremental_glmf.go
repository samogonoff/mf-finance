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

// Инкремент fact_glmf. В GLMF нет DateOfChange (как в Premaster) — вместо неё
// watermark по DateOfLoad (унаследован из Premaster при сборке GLMF_exec; при
// ретро-правке строка приходит с бОльшим DateOfLoad). ReplacingMergeTree
// (date_of_load) схлопывает версии. Чекпоинт — source='glmf' phase='incremental'.

// incrementalGLMFWhere — фильтр дельты по watermark.
func incrementalGLMFWhere() string {
	return " AND p.DateOfLoad > @last"
}

// RunIncrementalGLMF — один дельта-pull одного ЮЛ: строки с DateOfLoad > последнего
// загруженного. Обновляет чекпоинт. Возвращает (loaded, error).
func RunIncrementalGLMF(ctx context.Context, deps Deps, inn string) (int64, error) {
	ch, err := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	if err != nil {
		return 0, fmt.Errorf("incremental-glmf: ch client: %w", err)
	}
	startedAt := time.Now()

	last, err := lastGLMFLoad(ctx, deps, ch, inn)
	if err != nil {
		return 0, fmt.Errorf("incremental-glmf: watermark: %w", err)
	}

	// Фильтр по Company-коду (кластерный индекс GLMF) + watermark DateOfLoad.
	// БЕЗ ORDER BY (maxLoad считаем в цикле, порядок не нужен).
	coClause, coArgs := glmfCompanyWhere(inn)
	vgoClause, vgoArgs := vgoFilter()
	q := extractGLMFSelectFrom() + `
WHERE ` + coClause + incrementalGLMFWhere() + vgoClause
	args := append(coArgs, append([]interface{}{sql.Named("last", last)}, vgoArgs...)...)
	rows, err := deps.MSSQL.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("incremental-glmf: mssql: %w", err)
	}
	defer rows.Close()

	const batchSize = 5000
	batch := make([]glmfRow, 0, batchSize)
	var total int64
	maxLoad := last

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		for i := range batch {
			if err := enc.Encode(&batch[i]); err != nil {
				return err
			}
		}
		if err := ch.insertJSON(ctx, "finance.fact_glmf", &buf); err != nil {
			return err
		}
		total += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		r, err := scanGLMFRow(rows)
		if err != nil {
			return total, err
		}
		if t, err := time.Parse("2006-01-02", r.DateOfLoad); err == nil && t.After(maxLoad) {
			maxLoad = t
		}
		batch = append(batch, r)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, fmt.Errorf("incremental-glmf: rows: %w", err)
	}
	if err := flush(); err != nil {
		return total, err
	}

	finishedAt := time.Now()
	if _, err := deps.PG.Exec(ctx, `
		INSERT INTO debt_etl_checkpoint
		    (source, phase, company_id, last_change_at, rows_loaded, started_at, updated_at, finished_at)
		VALUES ('glmf','incremental',$1,$2,$3,$4,$4,$4)
		ON CONFLICT (source, phase, company_id) DO UPDATE
		   SET last_change_at = EXCLUDED.last_change_at, rows_loaded = EXCLUDED.rows_loaded,
		       updated_at = EXCLUDED.updated_at, finished_at = EXCLUDED.finished_at, error_text = NULL`,
		inn, maxLoad, total, finishedAt); err != nil {
		log.Printf("incremental-glmf: checkpoint %s: %v", inn, err)
	}

	if total > 0 {
		log.Printf("etl: incremental-glmf company=%s delta=%d dur=%.1fs last_load=%s",
			inn, total, finishedAt.Sub(startedAt).Seconds(), maxLoad.Format("2006-01-02"))
	}
	return total, nil
}

// lastGLMFLoad — watermark из PG checkpoint (source='glmf'); fallback — max(date_of_load) из CH.
func lastGLMFLoad(ctx context.Context, deps Deps, ch *chClient, inn string) (time.Time, error) {
	var t sql.NullTime
	err := deps.PG.QueryRow(ctx, `
		SELECT last_change_at FROM debt_etl_checkpoint
		 WHERE source='glmf' AND phase='incremental' AND company_id=$1`, inn).Scan(&t)
	if err == nil && t.Valid {
		return t.Time, nil
	}
	q := fmt.Sprintf(
		"SELECT toUnixTimestamp(coalesce(max(date_of_load), toDate('1970-01-01'))) FROM finance.fact_glmf WHERE company_id = '%s'",
		strings.ReplaceAll(inn, "'", "''"))
	s, err := ch.queryString(ctx, q)
	if err != nil {
		return time.Time{}, err
	}
	var ts int64
	if _, err := fmt.Sscanf(s, "%d", &ts); err != nil {
		return time.Time{}, fmt.Errorf("parse max(date_of_load): %w", err)
	}
	return time.Unix(ts, 0).UTC(), nil
}
