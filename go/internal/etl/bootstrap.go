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

	"github.com/jackc/pgx/v5/pgxpool"
)

// CountryByINN — копия справочника стран из internal/reports/debt/seed.go;
// дублируем, чтобы пакет etl не зависел от пакета reports/debt
// (иначе цикл импорта — debt уже знает про clickhouse repo).
var CountryByINN = map[string]string{
	"690591512":    "РБ",
	"690719790":    "РБ",
	"6950135110":   "РФ",
	"5031159833":   "РФ",
	"9909349268":   "РФ",
	"9731039708":   "РФ",
	"695018688905": "РФ",
}

// Deps — общие зависимости ETL-операций.
type Deps struct {
	MSSQL  *sql.DB
	PG     *pgxpool.Pool
	CHURL  string // http://host:port
	CHUser string
	CHPass string
}

// BootstrapOpts — параметры одного запуска bootstrap'а.
type BootstrapOpts struct {
	CompanyID    string
	BatchSize    int    // дефолт 5000
	TriggeredBy  string // 'manual' | 'admin-ui' | 'cron'
}

// row — одна строка для CH insert'а.
type row struct {
	CompanyID            string `json:"company_id"`
	CounterpartyID       string `json:"counterparty_id"`
	DocID                string `json:"doc_id"`
	RwNm                 int64  `json:"rw_nm"`
	Date                 string `json:"date"`
	DrAcc                string `json:"dr_acc"`
	CrAcc                string `json:"cr_acc"`
	DrAccRoot            string `json:"dr_acc_root"`
	CrAccRoot            string `json:"cr_acc_root"`
	Amount               string `json:"amount"`
	ICO                  uint8  `json:"ico"`
	Country              string `json:"country"`
	DocName1C            string `json:"doc_name_1c"`
	Mapping              string `json:"mapping"`
	TransDescription     string `json:"trans_description"`
	OperationDescription string `json:"operation_description"`
	DateOfChange         string `json:"date_of_change"`
}

// RunBootstrap — полная заливка одного ЮЛ. Идемпотентна: чистит CH от
// предыдущих строк этого company_id и заливает с нуля.
//
// Возвращает (rows_loaded, error). При ошибке rows_loaded = сколько успели до сбоя.
func RunBootstrap(ctx context.Context, deps Deps, opts BootstrapOpts) (int64, error) {
	if opts.CompanyID == "" {
		return 0, fmt.Errorf("bootstrap: company_id required")
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 5000
	}
	if opts.TriggeredBy == "" {
		opts.TriggeredBy = "manual"
	}

	ch, err := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	if err != nil {
		return 0, fmt.Errorf("bootstrap: ch client: %w", err)
	}

	startedAt := time.Now()
	log.Printf("=== bootstrap company=%s batch=%d trigger=%s START ===",
		opts.CompanyID, opts.BatchSize, opts.TriggeredBy)

	if _, err := deps.PG.Exec(ctx, `
		INSERT INTO debt_etl_checkpoint (source, phase, company_id, started_at, updated_at, rows_loaded)
		VALUES ('premaster','bootstrap',$1,NOW(),NOW(),0)
		ON CONFLICT (source, phase, company_id) DO UPDATE
		   SET started_at = NOW(), updated_at = NOW(), rows_loaded = 0,
		       finished_at = NULL, error_text = NULL
	`, opts.CompanyID); err != nil {
		return 0, fmt.Errorf("bootstrap: checkpoint init: %w", err)
	}

	delSQL := fmt.Sprintf(
		"ALTER TABLE finance.fact_premaster DELETE WHERE company_id = '%s'",
		strings.ReplaceAll(opts.CompanyID, "'", "''"))
	if err := ch.exec(ctx, delSQL); err != nil {
		log.Printf("  warn: cleanup CH for %s: %v (продолжаем)", opts.CompanyID, err)
	}

	rowsLoaded, runErr := streamPremaster(ctx, deps, ch, opts, startedAt)

	// финализация: пишем в PG чекпоинт и журнал CH (журнал в любом случае — успех или ошибка).
	finishedAt := time.Now()
	durSec := finishedAt.Sub(startedAt).Seconds()
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}

	if runErr == nil {
		if _, err := deps.PG.Exec(ctx, `
			UPDATE debt_etl_checkpoint
			   SET finished_at = NOW(), updated_at = NOW()
			 WHERE source='premaster' AND phase='bootstrap' AND company_id=$1`, opts.CompanyID); err != nil {
			log.Printf("  warn: checkpoint finalize: %v", err)
		}
	} else {
		if _, err := deps.PG.Exec(ctx, `
			UPDATE debt_etl_checkpoint
			   SET error_text = $1, updated_at = NOW()
			 WHERE source='premaster' AND phase='bootstrap' AND company_id=$2`,
			errText, opts.CompanyID); err != nil {
			log.Printf("  warn: checkpoint error update: %v", err)
		}
	}

	writeRunLog(ctx, ch, runLogEntry{
		CompanyID:    opts.CompanyID,
		Phase:        "bootstrap",
		TriggeredBy:  opts.TriggeredBy,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		DurationSec:  float32(durSec),
		RowsLoaded:   uint64(rowsLoaded),
		LastChangeAt: nil,
		ErrorText:    errText,
	})

	if runErr != nil {
		return rowsLoaded, runErr
	}
	log.Printf("=== bootstrap company=%s — DONE %d rows in %.1fs ===",
		opts.CompanyID, rowsLoaded, durSec)
	return rowsLoaded, nil
}

// streamPremaster — основная петля: SELECT cursor → batches → CH.
func streamPremaster(ctx context.Context, deps Deps, ch *chClient, opts BootstrapOpts, startedAt time.Time) (int64, error) {
	country := CountryByINN[opts.CompanyID]
	if country == "" {
		log.Printf("  warn: unknown country for %s — leaving empty", opts.CompanyID)
	}

	q := `
SELECT
    p.CompanyID,
    ISNULL(p.CounterpartyID, ''),
    CONVERT(NVARCHAR(MAX), p.DocID, 1),
    p.RwNm,
    CONVERT(CHAR(10), p.[Date], 23),
    p.DrAcc, p.CrAcc,
    LEFT(p.DrAcc, CHARINDEX('.', p.DrAcc + '.') - 1),
    LEFT(p.CrAcc, CHARINDEX('.', p.CrAcc + '.') - 1),
    CONVERT(VARCHAR(40), p.AmountWithVATCurrency),
    ISNULL(p.ICO, 0),
    ISNULL(o.[Name], ''),
    ISNULL(p.Mapping, ''),
    ISNULL(p.TransDescription, ''),
    ISNULL(p.OperationDescription, ''),
    CONVERT(VARCHAR(19), ISNULL(p.DateOfChange, p.[Date]), 120)
FROM [FinDWH].[dbo].[Premaster1C] AS p WITH (NOLOCK)
LEFT JOIN [FinDWH].[dbo].[Objects] AS o WITH (NOLOCK) ON o.ID = p.DocID
WHERE p.CompanyID = @inn
ORDER BY p.[Date], p.DocID, p.RwNm`

	rows, err := deps.MSSQL.QueryContext(ctx, q, sql.Named("inn", opts.CompanyID))
	if err != nil {
		return 0, fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()

	batch := make([]row, 0, opts.BatchSize)
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
		if err := ch.insertJSON(ctx, "finance.fact_premaster", &buf); err != nil {
			return err
		}
		totalLoaded += int64(len(batch))
		batch = batch[:0]

		if _, err := deps.PG.Exec(ctx, `
			UPDATE debt_etl_checkpoint
			   SET rows_loaded = $1, updated_at = NOW()
			 WHERE source='premaster' AND phase='bootstrap' AND company_id=$2`,
			totalLoaded, opts.CompanyID); err != nil {
			log.Printf("  warn: checkpoint update: %v", err)
		}
		elapsed := time.Since(startedAt).Seconds()
		log.Printf("  loaded=%d elapsed=%.1fs rate=%.0f rows/s", totalLoaded, elapsed, float64(totalLoaded)/elapsed)
		return nil
	}

	for rows.Next() {
		var r row
		if err := rows.Scan(
			&r.CompanyID, &r.CounterpartyID, &r.DocID, &r.RwNm, &r.Date,
			&r.DrAcc, &r.CrAcc, &r.DrAccRoot, &r.CrAccRoot,
			&r.Amount, &r.ICO,
			&r.DocName1C, &r.Mapping, &r.TransDescription, &r.OperationDescription,
			&r.DateOfChange,
		); err != nil {
			return totalLoaded, fmt.Errorf("scan: %w", err)
		}
		r.Country = country
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
