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

	loaded, err := streamContract(ctx, deps, ch, opts, contractTablePremaster)
	if err != nil {
		return loaded, err
	}
	log.Printf("=== bootstrap-contract company=%s — DONE %d договоров in %.1fs ===",
		opts.CompanyID, loaded, time.Since(startedAt).Seconds())
	return loaded, nil
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
		var ref, kind sql.NullString
		if err := rows.Scan(&r.DocID, &ref, &r.ContractName, &kind); err != nil {
			return total, fmt.Errorf("scan: %w", err)
		}
		// Эвристика-фильтр: оставляем только похожее на договор.
		if !isContractName(r.ContractName) {
			continue
		}
		r.ContractRef = strings.TrimSpace(ref.String)
		r.AccountKind = strings.TrimSpace(kind.String)
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
