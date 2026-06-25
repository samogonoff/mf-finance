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

// ContractSource — таблица-источник субконто для dim_contract.
const (
	contractTablePremaster = "[FinDWH].[dbo].[Premaster1C]"
	contractTableHistory   = "[FinDWH].[dbo].[Premaster1CHistory]"
)

// RunBootstrapContract — заливка договоров одного ЮЛ в finance.dim_contract из
// субконто Premaster1C. Имена фильтруются эвристикой isContractName (в Go).
// Идемпотентна по doc_id (ReplacingMergeTree). История (Premaster1CHistory) —
// отдельный прогон с тем же кодом (table=contractTableHistory) при необходимости.
func RunBootstrapContract(ctx context.Context, deps Deps, opts BootstrapOpts) (int64, error) {
	if opts.CompanyID == "" {
		return 0, fmt.Errorf("bootstrap-contract: company_id required")
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 5000
	}
	ch, err := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	if err != nil {
		return 0, fmt.Errorf("bootstrap-contract: ch client: %w", err)
	}
	startedAt := time.Now()
	log.Printf("=== bootstrap-contract company=%s START ===", opts.CompanyID)

	// Заливаем из Premaster1C И Premaster1CHistory: Premaster1C неполон по части
	// периодов, History покрывает старые. doc_id-дубли схлопывает ReplacingMergeTree.
	var total int64
	for _, table := range []string{contractTablePremaster, contractTableHistory} {
		n, err := streamContract(ctx, deps, ch, opts, table)
		if err != nil {
			log.Printf("  warn: contract from %s for %s: %v (продолжаем)", table, opts.CompanyID, err)
			continue
		}
		total += n
	}
	log.Printf("=== bootstrap-contract company=%s — DONE %d договоров in %.1fs ===",
		opts.CompanyID, total, time.Since(startedAt).Seconds())
	return total, nil
}

// streamContract — SELECT субконто (62/60/76) → эвристика-фильтр → dim_contract.
func streamContract(ctx context.Context, deps Deps, ch *chClient, opts BootstrapOpts, table string) (int64, error) {
	q := extractContractSelectFrom(table) + `
  AND p.CompanyID = @inn`
	rows, err := deps.MSSQL.QueryContext(ctx, q, sql.Named("inn", opts.CompanyID))
	if err != nil {
		return 0, fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()

	batch := make([]contractRow, 0, opts.BatchSize)
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
		if err := ch.insertJSON(ctx, "finance.dim_contract", &buf); err != nil {
			return err
		}
		total += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		var r contractRow
		var ref, kind, delay sql.NullString
		if err := rows.Scan(&r.DocID, &r.CompanyID, &ref, &r.ContractName, &kind, &delay); err != nil {
			return total, fmt.Errorf("scan: %w", err)
		}
		// Эвристика-фильтр: оставляем только похожее на договор.
		if !isContractName(r.ContractName) {
			continue
		}
		r.ContractRef = strings.TrimSpace(ref.String)
		r.AccountKind = strings.TrimSpace(kind.String)
		r.PaymentDelay = strings.TrimSpace(delay.String)
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
