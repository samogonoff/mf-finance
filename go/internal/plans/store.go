package plans

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MetricStore — хранилище экземпляров PL, метрик и снимков формы.
// Реализации: pgStore (pgx, рантайм) и in-memory (тесты round-trip).
type MetricStore interface {
	// EnsureInstance возвращает id экземпляра PL на период (создаёт при отсутствии).
	EnsureInstance(ctx context.Context, year, month int) (int64, error)
	// ListInstances — все экземпляры PL с числом метрик (для списка/дашборда).
	ListInstances(ctx context.Context) ([]InstanceSummary, error)
	// Metrics — сохранённая тактика по сегменту/периоду.
	Metrics(ctx context.Context, plID int64, segment string, year, month int) ([]MetricRow, error)
	// MetricsAll — тактика по периоду без фильтра сегмента (для свода ЮЛ).
	MetricsAll(ctx context.Context, plID int64, year, month int) ([]MetricRow, error)
	// UpsertMetrics — upsert editable-ячеек тактики.
	UpsertMetrics(ctx context.Context, plID int64, segment string, rows []MetricRow) error
	// CopyTactic — копирование тактики из периода в период (TPL-09). При restrict
	// копируются только profit_center из allowed. Возвращает число строк.
	CopyTactic(ctx context.Context, fromPlID, toPlID int64, fromY, fromM, toY, toM int, allowed []int, restrict bool) (int, error)
	// SaveSubmission — полный снимок формы (json_payload).
	SaveSubmission(ctx context.Context, plID int64, payload []byte) error
	// SaveAdjustments — аудит ручных корректировок (ADJ-03).
	SaveAdjustments(ctx context.Context, plID int64, adj []AdjustmentRow) error
	// AddComment — комментарий к экземпляру PL (COM-01).
	AddComment(ctx context.Context, plID int64, c CommentInput) (int64, error)
	// Comments — комментарии экземпляра PL.
	Comments(ctx context.Context, plID int64) ([]Comment, error)
	// FormulaOverrides — актуальные override формул экземпляра (code → выражение).
	FormulaOverrides(ctx context.Context, plID int64) (map[string]string, error)
	// UpsertOverride — добавить переопределение формулы (версионируется, D11).
	UpsertOverride(ctx context.Context, plID int64, ov FormulaOverride) error
	// StagesInit — создать этапы экземпляра при отсутствии (идемпотентно).
	StagesInit(ctx context.Context, plID int64, stages []StageState) error
	// Stages — этапы экземпляра.
	Stages(ctx context.Context, plID int64) ([]StageState, error)
	// StagesSave — сохранить статусы этапов.
	StagesSave(ctx context.Context, plID int64, stages []StageState) error
	// RecordApproval — лист согласования (pl_approval).
	RecordApproval(ctx context.Context, plID int64, code string, userID int64, decision, legalEntity string) error
	// RouteConfig — ответственные по этапам (stage_code → метка).
	RouteConfig(ctx context.Context) (map[string]string, error)
	// UpsertRouteConfig — назначить ответственных этапа (админ процессов).
	UpsertRouteConfig(ctx context.Context, code, responsible string) error
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

func (s *pgStore) ListInstances(ctx context.Context) ([]InstanceSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.period_year, i.period_month, i.status, COUNT(m.id)
		FROM pl_instance i
		LEFT JOIN pl_metric m ON m.pl_id = i.id
		GROUP BY i.id
		ORDER BY i.period_year DESC, i.period_month DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]InstanceSummary, 0)
	for rows.Next() {
		var s InstanceSummary
		if err := rows.Scan(&s.ID, &s.PeriodYear, &s.PeriodMonth, &s.Status, &s.MetricCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
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

func (s *pgStore) MetricsAll(ctx context.Context, plID int64, year, month int) ([]MetricRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT line_code, block_type, profit_center, country, scenario,
		       period_year, period_month, currency, COALESCE(amount, 0), is_manual
		FROM pl_metric
		WHERE pl_id = $1 AND template_code = $2
		  AND period_year = $3 AND period_month = $4 AND scenario = $5`,
		plID, TemplateMP, year, month, ScenarioTactic)
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

func (s *pgStore) CopyTactic(ctx context.Context, fromPlID, toPlID int64, fromY, fromM, toY, toM int, allowed []int, restrict bool) (int, error) {
	if restrict && len(allowed) == 0 {
		return 0, nil
	}
	q := `
		INSERT INTO pl_metric (pl_id, template_code, segment, line_code, block_type,
		    profit_center, cost_center, country, legal_entity, channel, scenario,
		    period_year, period_month, currency, amount, is_manual)
		SELECT $1, template_code, segment, line_code, block_type,
		       profit_center, cost_center, country, legal_entity, channel, scenario,
		       $2, $3, currency, amount, is_manual
		FROM pl_metric
		WHERE pl_id = $4 AND template_code = $5 AND scenario = $6
		  AND period_year = $7 AND period_month = $8`
	args := []any{toPlID, toY, toM, fromPlID, TemplateMP, ScenarioTactic, fromY, fromM}
	if restrict {
		q += ` AND profit_center = ANY($9)`
		args = append(args, allowed)
	}
	q += `
		ON CONFLICT (pl_id, template_code, segment, line_code, block_type,
		    profit_center, scenario, period_year, period_month, currency)
		DO UPDATE SET amount = EXCLUDED.amount, is_manual = EXCLUDED.is_manual`
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *pgStore) SaveSubmission(ctx context.Context, plID int64, payload []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO form_submission (pl_id, template_code, json_payload)
		VALUES ($1, $2, $3)`, plID, TemplateMP, payload)
	return err
}

func (s *pgStore) SaveAdjustments(ctx context.Context, plID int64, adj []AdjustmentRow) error {
	if len(adj) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, a := range adj {
		if _, err := tx.Exec(ctx, `
			INSERT INTO pl_adjustment (pl_id, profit_center, line_code, block_type,
			    period_year, period_month, currency, adjusted_value, reason)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			plID, a.ProfitCenter, a.LineCode, a.BlockType, a.Year, a.Month,
			a.Currency, a.AdjustedValue, a.Reason); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *pgStore) FormulaOverrides(ctx context.Context, plID int64) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (code) code, formula_expr
		FROM pl_formula_override WHERE pl_id = $1
		ORDER BY code, created_at DESC, id DESC`, plID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var code, expr string
		if err := rows.Scan(&code, &expr); err != nil {
			return nil, err
		}
		out[code] = expr
	}
	return out, rows.Err()
}

func (s *pgStore) UpsertOverride(ctx context.Context, plID int64, ov FormulaOverride) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pl_formula_override (pl_id, scope_code_cfo, code, block_type, formula_expr, reason)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		plID, ov.ScopeCodeCFO, ov.Code, ov.BlockType, ov.FormulaExpr, ov.Reason)
	return err
}

func (s *pgStore) StagesInit(ctx context.Context, plID int64, stages []StageState) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, st := range stages {
		dep, _ := json.Marshal(st.DependsOn)
		var due any
		if st.DueDate != "" {
			due = st.DueDate
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO pl_stage_instance (pl_id, stage_code, track, status, due_at, depends_on)
			VALUES ($1, $2, $3, $4, $5::date, $6)
			ON CONFLICT (pl_id, stage_code) DO NOTHING`,
			plID, st.Code, st.Track, st.Status, due, dep); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *pgStore) Stages(ctx context.Context, plID int64) ([]StageState, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT stage_code, track, status, COALESCE(to_char(due_at, 'YYYY-MM-DD'), ''), depends_on
		FROM pl_stage_instance WHERE pl_id = $1`, plID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]StageState, 0)
	for rows.Next() {
		var st StageState
		var dep []byte
		if err := rows.Scan(&st.Code, &st.Track, &st.Status, &st.DueDate, &dep); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(dep, &st.DependsOn)
		if d, ok := stageDefByCode(st.Code); ok {
			st.Name = d.Name
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *pgStore) StagesSave(ctx context.Context, plID int64, stages []StageState) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, st := range stages {
		if _, err := tx.Exec(ctx, `
			UPDATE pl_stage_instance SET status = $3
			WHERE pl_id = $1 AND stage_code = $2`, plID, st.Code, st.Status); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *pgStore) RecordApproval(ctx context.Context, plID int64, code string, userID int64, decision, legalEntity string) error {
	var uid any
	if userID != 0 {
		uid = userID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pl_approval (pl_id, stage_id, legal_entity, user_id, decision)
		VALUES ($1, (SELECT id FROM pl_stage_instance WHERE pl_id = $1 AND stage_code = $2), $3, $4, $5)`,
		plID, code, legalEntity, uid, decision)
	return err
}

func (s *pgStore) RouteConfig(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT stage_code, responsible FROM plans_route_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var code, resp string
		if err := rows.Scan(&code, &resp); err != nil {
			return nil, err
		}
		out[code] = resp
	}
	return out, rows.Err()
}

func (s *pgStore) UpsertRouteConfig(ctx context.Context, code, responsible string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plans_route_config (stage_code, responsible, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (stage_code) DO UPDATE SET responsible = EXCLUDED.responsible, updated_at = NOW()`,
		code, responsible)
	return err
}

func (s *pgStore) AddComment(ctx context.Context, plID int64, c CommentInput) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO pl_comment (pl_id, metric_ref, body)
		VALUES ($1, $2, $3) RETURNING id`, plID, c.MetricRef, c.Body).Scan(&id)
	return id, err
}

func (s *pgStore) Comments(ctx context.Context, plID int64) ([]Comment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, metric_ref, body, status, created_at
		FROM pl_comment WHERE pl_id = $1 ORDER BY created_at DESC`, plID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Comment, 0)
	for rows.Next() {
		var c Comment
		var ts time.Time
		if err := rows.Scan(&c.ID, &c.MetricRef, &c.Body, &c.Status, &ts); err != nil {
			return nil, err
		}
		c.CreatedAt = ts.Format(time.RFC3339)
		out = append(out, c)
	}
	return out, rows.Err()
}
