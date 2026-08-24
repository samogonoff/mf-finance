package plans

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Хранилище карточек форм, их листа согласования, версий-снапшотов и маршрута
// (миграция 0031). Отдельно от pgStore: карточки — общая оболочка для всех форм,
// а pgStore заточен под метрики TPL-MP.

// CardVersion — версия-снапшот карточки.
type CardVersion struct {
	VersionNo int             `json:"version_no"`
	StepFrom  string          `json:"step_from"`
	StepTo    string          `json:"step_to"`
	Action    string          `json:"action"`
	Reason    string          `json:"reason"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedBy int64           `json:"created_by"`
	CreatedAt time.Time       `json:"created_at"`
}

// CardApproval — запись листа согласования карточки.
type CardApproval struct {
	StepCode      string    `json:"step_code"`
	UserID        int64     `json:"user_id"`
	UserName      string    `json:"user_name"`
	Decision      string    `json:"decision"`
	TargetStep    string    `json:"target_step"`
	Comment       string    `json:"comment"`
	VersionNo     int       `json:"version_no"`
	Revoked       bool      `json:"revoked"`
	RevokedReason string    `json:"revoked_reason"`
	DecidedAt     time.Time `json:"decided_at"`
}

// CardStore — доступ к карточкам форм.
type CardStore interface {
	EnsureCard(ctx context.Context, c Card) (Card, error)
	Card(ctx context.Context, id int64) (Card, error)
	CardByScope(ctx context.Context, plID int64, formCode, scopeKey string) (Card, error)
	Cards(ctx context.Context, plID int64) ([]Card, error)
	// SaveTransition применяет переход: статус/шаг/lock, версия-снапшот, лист
	// согласования и аннулирование — одной транзакцией.
	SaveTransition(ctx context.Context, cardID int64, tr CardTransition, snapshot []byte, actorID int64) (Card, error)
	SetCalcMode(ctx context.Context, cardID int64, mode string) error
	SetFxSnapshot(ctx context.Context, cardID int64, fx map[string]any) error
	MarkPublished(ctx context.Context, cardID int64, ok bool, note string) error
	CardApprovals(ctx context.Context, cardID int64) ([]CardApproval, error)
	CardVersions(ctx context.Context, cardID int64) ([]CardVersion, error)
	CardVersionPayload(ctx context.Context, cardID int64, versionNo int) (json.RawMessage, error)
	// Route — шаги маршрута формы (включённые и выключенные, по порядку).
	Route(ctx context.Context, formCode string) ([]RouteStep, error)
	UpsertRouteStep(ctx context.Context, s RouteStep, actorID int64) error
}

type pgCardStore struct{ pool *pgxpool.Pool }

// NewCardStore — конструктор.
func NewCardStore(pool *pgxpool.Pool) CardStore { return &pgCardStore{pool: pool} }

// Период карточки (year/month) живёт в pl_instance — тянем джойном: провайдерам
// формы нужен период, а карточка без него бесполезна.
const cardCols = `c.id, c.pl_id, c.form_code, c.scope_key, c.title,
	i.period_year, i.period_month,
	c.country, c.legal_entity, c.currency,
	c.calc_mode, c.status, c.step_code, c.current_version, COALESCE(c.fx_snapshot,'{}'), c.locked,
	c.due_at, COALESCE(c.created_by,0), c.updated_at`

// cardFrom — источник для cardCols.
const cardFrom = ` FROM form_card c JOIN pl_instance i ON i.id = c.pl_id`

// cardReturning — тот же набор полей для RETURNING после INSERT/UPDATE.
//
// Почему не `WITH up AS (INSERT … RETURNING id) SELECT … WHERE id = (SELECT id FROM up)`:
// в Postgres изменяющий CTE и основной запрос работают на ОДНОМ снимке, поэтому
// основной SELECT не видит вставленную/обновлённую строку (для INSERT — «no rows
// in result set», для UPDATE — старые значения). Период берём подзапросами:
// в RETURNING они разрешены и видят уже существующую pl_instance.
const cardReturning = `id, pl_id, form_code, scope_key, title,
	(SELECT period_year FROM pl_instance WHERE id = form_card.pl_id),
	(SELECT period_month FROM pl_instance WHERE id = form_card.pl_id),
	country, legal_entity, currency,
	calc_mode, status, step_code, current_version, COALESCE(fx_snapshot,'{}'), locked,
	due_at, COALESCE(created_by,0), updated_at`

func scanCard(row interface {
	Scan(dest ...any) error
}) (Card, error) {
	var c Card
	var fx []byte
	if err := row.Scan(&c.ID, &c.PlID, &c.FormCode, &c.ScopeKey, &c.Title,
		&c.Year, &c.Month, &c.Country,
		&c.LegalEntity, &c.Currency, &c.CalcMode, &c.Status, &c.StepCode, &c.CurrentVersion,
		&fx, &c.Locked, &c.DueAt, &c.CreatedBy, &c.UpdatedAt); err != nil {
		return Card{}, err
	}
	if len(fx) > 0 {
		_ = json.Unmarshal(fx, &c.FxSnapshot)
	}
	c.StatusLabel = CardStatusLabel(c.Status, c.StepCode)
	return c, nil
}

func (s *pgCardStore) EnsureCard(ctx context.Context, in Card) (Card, error) {
	if in.CalcMode == "" {
		in.CalcMode = CalcLegacy
	}
	if in.StepCode == "" {
		in.StepCode = "1.1"
	}
	var createdBy any
	if in.CreatedBy != 0 {
		createdBy = in.CreatedBy
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO form_card (pl_id, form_code, scope_key, title, country, legal_entity,
			currency, calc_mode, status, step_code, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'draft',$9,$10)
		ON CONFLICT (pl_id, form_code, scope_key) DO UPDATE
			SET title = EXCLUDED.title,
			    country = EXCLUDED.country,
			    legal_entity = EXCLUDED.legal_entity,
			    currency = COALESCE(NULLIF(EXCLUDED.currency,''), form_card.currency)
		RETURNING `+cardReturning,
		in.PlID, in.FormCode, in.ScopeKey, in.Title, in.Country, in.LegalEntity,
		in.Currency, in.CalcMode, in.StepCode, createdBy)
	return scanCard(row)
}

func (s *pgCardStore) Card(ctx context.Context, id int64) (Card, error) {
	return scanCard(s.pool.QueryRow(ctx, `SELECT `+cardCols+cardFrom+` WHERE c.id = $1`, id))
}

func (s *pgCardStore) CardByScope(ctx context.Context, plID int64, formCode, scopeKey string) (Card, error) {
	return scanCard(s.pool.QueryRow(ctx, `SELECT `+cardCols+cardFrom+`
		WHERE c.pl_id = $1 AND c.form_code = $2 AND c.scope_key = $3`, plID, formCode, scopeKey))
}

func (s *pgCardStore) Cards(ctx context.Context, plID int64) ([]Card, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+cardCols+cardFrom+`
		WHERE c.pl_id = $1 ORDER BY c.form_code, c.scope_key`, plID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveTransition — переход одной транзакцией: снапшот версии (если VersionBump),
// новое состояние карточки, записи листа согласования и аннулирование решений.
func (s *pgCardStore) SaveTransition(ctx context.Context, cardID int64, tr CardTransition, snapshot []byte, actorID int64) (Card, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Card{}, err
	}
	defer tx.Rollback(ctx)

	var version int
	if err := tx.QueryRow(ctx, `SELECT current_version FROM form_card WHERE id = $1 FOR UPDATE`, cardID).Scan(&version); err != nil {
		return Card{}, err
	}
	var actor any
	if actorID != 0 {
		actor = actorID
	}

	if tr.VersionBump {
		version++
		if len(snapshot) == 0 {
			snapshot = []byte(`{}`)
		}
		reason := ""
		action := ""
		if len(tr.Approvals) > 0 {
			reason = tr.Approvals[0].Comment
			action = tr.Approvals[0].Decision
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO card_version (card_id, version_no, step_from, step_to, action, reason, payload, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (card_id, version_no) DO NOTHING`,
			cardID, version, currentStepOf(tr), tr.StepCode, action, reason, snapshot, actor); err != nil {
			return Card{}, err
		}
	}

	// Аннулирование решений от целевого шага и выше (ТЗ §2.3): решения остаются,
	// но помечаются revoked — согласующий видит историю.
	if tr.RevokeFrom != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE card_approval SET revoked = TRUE, revoked_reason = $3
			 WHERE card_id = $1 AND NOT revoked AND decision IN ('approve','auto_skipped')
			   AND step_code >= $2`,
			cardID, tr.RevokeFrom, "аннулировано возвратом на шаг "+tr.RevokeFrom); err != nil {
			return Card{}, err
		}
	}

	for _, a := range tr.Approvals {
		var uid any
		if a.UserID != 0 {
			uid = a.UserID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO card_approval (card_id, step_code, user_id, decision, target_step, comment, version_no)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			cardID, a.StepCode, uid, a.Decision, a.TargetStep, a.Comment, version); err != nil {
			return Card{}, err
		}
	}

	row := tx.QueryRow(ctx, `
		UPDATE form_card
		   SET status = $2, step_code = $3, locked = $4, current_version = $5, updated_at = NOW()
		 WHERE id = $1
		RETURNING `+cardReturning,
		cardID, tr.Status, tr.StepCode, tr.Locked, version)
	c, err := scanCard(row)
	if err != nil {
		return Card{}, err
	}
	return c, tx.Commit(ctx)
}

// currentStepOf — шаг, с которого произошёл переход (для card_version.step_from).
func currentStepOf(tr CardTransition) string {
	if len(tr.Approvals) > 0 {
		return tr.Approvals[0].StepCode
	}
	return ""
}

func (s *pgCardStore) SetCalcMode(ctx context.Context, cardID int64, mode string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE form_card SET calc_mode = $2, updated_at = NOW() WHERE id = $1`, cardID, mode)
	return err
}

func (s *pgCardStore) SetFxSnapshot(ctx context.Context, cardID int64, fx map[string]any) error {
	raw, err := json.Marshal(fx)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE form_card SET fx_snapshot = $2, updated_at = NOW() WHERE id = $1`, cardID, raw)
	return err
}

func (s *pgCardStore) MarkPublished(ctx context.Context, cardID int64, ok bool, note string) error {
	status := CardPublished
	if !ok {
		status = CardPublishFailed
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE form_card SET status = $2, locked = TRUE, updated_at = NOW() WHERE id = $1`,
		cardID, status); err != nil {
		return err
	}
	decision := "publish"
	if !ok {
		decision = "publish_failed"
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO card_approval (card_id, step_code, decision, comment)
		VALUES ($1, (SELECT step_code FROM form_card WHERE id = $1), $2, $3)`,
		cardID, decision, note); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *pgCardStore) CardApprovals(ctx context.Context, cardID int64) ([]CardApproval, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.step_code, COALESCE(a.user_id,0), COALESCE(u.name,''), a.decision,
		       a.target_step, a.comment, a.version_no, a.revoked, a.revoked_reason, a.decided_at
		  FROM card_approval a
		  LEFT JOIN users u ON u.id = a.user_id
		 WHERE a.card_id = $1
		 ORDER BY a.decided_at, a.id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CardApproval
	for rows.Next() {
		var a CardApproval
		if err := rows.Scan(&a.StepCode, &a.UserID, &a.UserName, &a.Decision, &a.TargetStep,
			&a.Comment, &a.VersionNo, &a.Revoked, &a.RevokedReason, &a.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *pgCardStore) CardVersions(ctx context.Context, cardID int64) ([]CardVersion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT version_no, step_from, step_to, action, reason, COALESCE(created_by,0), created_at
		  FROM card_version WHERE card_id = $1 ORDER BY version_no DESC`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CardVersion
	for rows.Next() {
		var v CardVersion
		if err := rows.Scan(&v.VersionNo, &v.StepFrom, &v.StepTo, &v.Action, &v.Reason,
			&v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *pgCardStore) CardVersionPayload(ctx context.Context, cardID int64, versionNo int) (json.RawMessage, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx,
		`SELECT payload FROM card_version WHERE card_id = $1 AND version_no = $2`, cardID, versionNo).Scan(&raw)
	return raw, err
}

func (s *pgCardStore) Route(ctx context.Context, formCode string) ([]RouteStep, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT form_code, step_code, step_name, sort_order, kind, responsible, position_code,
		       COALESCE(due_rd,0), enabled, skip_if_same_user, publish_on_approve
		  FROM plans_form_route WHERE form_code = $1 ORDER BY sort_order`, formCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RouteStep
	for rows.Next() {
		var s RouteStep
		if err := rows.Scan(&s.FormCode, &s.StepCode, &s.StepName, &s.SortOrder, &s.Kind,
			&s.Responsible, &s.PositionCode, &s.DueRD, &s.Enabled, &s.SkipIfSameUser,
			&s.PublishOnApprove); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *pgCardStore) UpsertRouteStep(ctx context.Context, st RouteStep, actorID int64) error {
	var actor any
	if actorID != 0 {
		actor = actorID
	}
	var due any
	if st.DueRD > 0 {
		due = st.DueRD
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plans_form_route (form_code, step_code, step_name, sort_order, kind,
			responsible, position_code, due_rd, enabled, skip_if_same_user, publish_on_approve, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW())
		ON CONFLICT (form_code, step_code) DO UPDATE SET
			step_name = EXCLUDED.step_name, sort_order = EXCLUDED.sort_order,
			kind = EXCLUDED.kind, responsible = EXCLUDED.responsible,
			position_code = EXCLUDED.position_code, due_rd = EXCLUDED.due_rd,
			enabled = EXCLUDED.enabled, skip_if_same_user = EXCLUDED.skip_if_same_user,
			publish_on_approve = EXCLUDED.publish_on_approve,
			updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		st.FormCode, st.StepCode, st.StepName, st.SortOrder, st.Kind, st.Responsible,
		st.PositionCode, due, st.Enabled, st.SkipIfSameUser, st.PublishOnApprove, actor)
	return err
}
