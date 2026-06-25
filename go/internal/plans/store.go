package plans

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MetricStore — хранилище экземпляров PL, метрик и снимков формы.
// Реализации: pgStore (pgx, рантайм) и in-memory (тесты round-trip).
type MetricStore interface {
	// EnsureInstance возвращает id экземпляра PL на период (создаёт при отсутствии).
	EnsureInstance(ctx context.Context, year, month int) (int64, error)
	// Metrics — сохранённая тактика по сегменту/периоду.
	Metrics(ctx context.Context, plID int64, segment string, year, month int) ([]MetricRow, error)
	// UpsertMetrics — upsert editable-ячеек тактики.
	UpsertMetrics(ctx context.Context, plID int64, segment string, rows []MetricRow) error
	// SaveSubmission — полный снимок формы (json_payload).
	SaveSubmission(ctx context.Context, plID int64, payload []byte) error
}

// pgStore — pgx-реализация MetricStore.
type pgStore struct{ pool *pgxpool.Pool }

// NewPgStore — конструктор.
func NewPgStore(pool *pgxpool.Pool) *pgStore { return &pgStore{pool: pool} }

func (s *pgStore) EnsureInstance(ctx context.Context, year, month int) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO pl_instance (period_year, period_month, status)
		VALUES ($1, $2, 'in_progress')
		ON CONFLICT (period_year, period_month)
		DO UPDATE SET status = pl_instance.status
		RETURNING id`, year, month).Scan(&id)
	return id, err
}

func (s *pgStore) Metrics(ctx context.Context, plID int64, segment string, year, month int) ([]MetricRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT line_code, block_type, profit_center, country, scenario,
		       period_year, period_month, currency, COALESCE(amount, 0), is_manual
		FROM pl_metric
		WHERE pl_id = $1 AND template_code = $2 AND segment = $3
		  AND period_year = $4 AND period_month = $5 AND scenario = $6`,
		plID, TemplateMP, segment, year, month, ScenarioTactic)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]MetricRow, 0)
	for rows.Next() {
		var m MetricRow
		if err := rows.Scan(&m.LineCode, &m.BlockType, &m.ProfitCenter, &m.Country, &m.Scenario,
			&m.Year, &m.Month, &m.Currency, &m.Amount, &m.IsManual); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *pgStore) UpsertMetrics(ctx context.Context, plID int64, segment string, rows []MetricRow) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, m := range rows {
		if _, err := tx.Exec(ctx, `
			INSERT INTO pl_metric (pl_id, template_code, segment, line_code, block_type,
			    profit_center, country, scenario, period_year, period_month, currency, amount, is_manual)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (pl_id, template_code, segment, line_code, block_type,
			    profit_center, scenario, period_year, period_month, currency)
			DO UPDATE SET amount = EXCLUDED.amount, is_manual = EXCLUDED.is_manual,
			              country = EXCLUDED.country`,
			plID, TemplateMP, segment, m.LineCode, m.BlockType, m.ProfitCenter, m.Country,
			m.Scenario, m.Year, m.Month, m.Currency, m.Amount, m.IsManual); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *pgStore) SaveSubmission(ctx context.Context, plID int64, payload []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO form_submission (pl_id, template_code, json_payload)
		VALUES ($1, $2, $3)`, plID, TemplateMP, payload)
	return err
}
