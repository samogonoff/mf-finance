package etl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Справочники пересчёта валют на дату документа (Doc_Date) для второго потока
// отчёта ВГО (DEBT_BACKEND=findebt-docdate, docs/reports/debt/
// vgo-doc-date-methodology.md). Источники — linked-server справочники OLAP:
//   dim_valuta      ← [SRV-SQL].Gpartner.dbo.valuta      (NAIM→KOD)
//   currency_daily  ← [SRV-SQL].Checks.dbo.CurrencyDaily (курсы к BYN по датам)
//
// Джойн к курсам построчно через linked server = удалённый nested loop (десятки
// минут). Поэтому НЕ джойним на стороне MSSQL: выгружаем оба справочника целиком
// в CH, а подбор курса на Doc_Date делает уже отчёт локально через ASOF JOIN.
//
// valuta мал (десятки строк) → truncate + полный reload. CurrencyDaily растёт по
// одной строке на (валюта, дата) → инкремент по watermark max(date) из CH.

// CurrencyTables — FQN linked-server источников курса на OLAP. Полные имена (с
// [SRV-SQL].БД.dbo.таблица) настраиваемы через env: доступность linked server
// [SRV-SQL] с нашего OLAP-коннекта — открытый вопрос интеграции (Фаза 0), при
// недоступности FQN подменяется на прямой коннект без правки кода.
type CurrencyTables struct {
	ValutaFQN        string // [SRV-SQL].Gpartner.dbo.valuta
	CurrencyDailyFQN string // [SRV-SQL].Checks.dbo.CurrencyDaily
}

type valutaRow struct {
	KOD  int32  `json:"kod"`
	NAIM string `json:"naim"`
}

type currencyDailyRow struct {
	KOD      int32  `json:"kod"`
	Date     string `json:"date"`
	CurrRate string `json:"curr_rate"`
}

// RunCurrencySync — обновляет оба справочника курса в CH. Возвращает суммарное
// число залитых строк (valuta + currency_daily).
func RunCurrencySync(ctx context.Context, deps Deps, t CurrencyTables) (int64, error) {
	if t.ValutaFQN == "" || t.CurrencyDailyFQN == "" {
		return 0, fmt.Errorf("currency-sync: ValutaFQN/CurrencyDailyFQN required")
	}
	ch, err := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	if err != nil {
		return 0, fmt.Errorf("currency-sync: ch client: %w", err)
	}
	startedAt := time.Now()

	nv, err := syncValuta(ctx, deps, ch, t.ValutaFQN)
	if err != nil {
		return 0, fmt.Errorf("currency-sync: valuta: %w", err)
	}
	nc, err := syncCurrencyDaily(ctx, deps, ch, t.CurrencyDailyFQN)
	if err != nil {
		return nv, fmt.Errorf("currency-sync: currency_daily: %w", err)
	}
	log.Printf("etl: currency-sync valuta=%d currency_daily=%d in %.1fs",
		nv, nc, time.Since(startedAt).Seconds())
	return nv + nc, nil
}

// syncValuta — полный reload справочника валют (мал → truncate + insert).
// RTRIM(NAIM) на стороне MSSQL: в источнике наименование с паддинг-пробелами,
// а отчёт джойнит его с (уже trimmed) FinDebt3.Currency.
func syncValuta(ctx context.Context, deps Deps, ch *chClient, fqn string) (int64, error) {
	q := "SELECT KOD, RTRIM(NAIM) AS NAIM FROM " + fqn
	rows, err := deps.MSSQL.QueryContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()

	batch := make([]valutaRow, 0, 256)
	for rows.Next() {
		var r valutaRow
		if err := rows.Scan(&r.KOD, &r.NAIM); err != nil {
			return 0, fmt.Errorf("scan valuta: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rows: %w", err)
	}
	// Полный reload: справочник маленький, TRUNCATE перед вставкой (иначе
	// ReplacingMergeTree держал бы устаревшие naim до мержа).
	if err := ch.exec(ctx, "TRUNCATE TABLE IF EXISTS finance.dim_valuta"); err != nil {
		return 0, fmt.Errorf("truncate dim_valuta: %w", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := range batch {
		if err := enc.Encode(&batch[i]); err != nil {
			return 0, fmt.Errorf("encode: %w", err)
		}
	}
	if buf.Len() > 0 {
		if err := ch.insertJSON(ctx, "finance.dim_valuta", &buf); err != nil {
			return 0, err
		}
	}
	return int64(len(batch)), nil
}

// syncCurrencyDaily — инкремент курсов по watermark. Берёт max(date) из CH, чистит
// и переливает начиная с неё (последняя дата могла быть неполной). CH пуст →
// полный догон с 1900-01-01.
func syncCurrencyDaily(ctx context.Context, deps Deps, ch *chClient, fqn string) (int64, error) {
	last, err := ch.queryString(ctx,
		"SELECT ifNull(toString(max(date)), '') FROM finance.currency_daily")
	if err != nil {
		return 0, fmt.Errorf("watermark: %w", err)
	}
	if last == "" {
		last = "1900-01-01"
	}
	del := fmt.Sprintf("ALTER TABLE finance.currency_daily DELETE WHERE date >= toDate('%s')", sqlEscape(last))
	if err := ch.exec(ctx, del); err != nil {
		log.Printf("  warn: cleanup currency_daily: %v (продолжаем)", err)
	}

	// CONVERT(DATE,...) — CurrencyDaily.date хранится как datetime; отчётный ASOF
	// джойнит по чистой дате, приводим здесь.
	q := "SELECT KOD, CONVERT(CHAR(10), date, 23) AS date, CONVERT(VARCHAR(40), curr_rate) AS curr_rate " +
		"FROM " + fqn + " WHERE CONVERT(DATE, date) >= @min"
	rows, err := deps.MSSQL.QueryContext(ctx, q, sql.Named("min", last))
	if err != nil {
		return 0, fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()

	batch := make([]currencyDailyRow, 0, 5000)
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
		if err := ch.insertJSON(ctx, "finance.currency_daily", &buf); err != nil {
			return err
		}
		total += int64(len(batch))
		batch = batch[:0]
		return nil
	}
	for rows.Next() {
		var r currencyDailyRow
		if err := rows.Scan(&r.KOD, &r.Date, &r.CurrRate); err != nil {
			return total, fmt.Errorf("scan currency_daily: %w", err)
		}
		batch = append(batch, r)
		if len(batch) >= 5000 {
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
