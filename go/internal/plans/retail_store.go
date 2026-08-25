package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Хранилище формы «Розница» (таблицы tp_* миграции 0035). Только доступ к данным:
// формулы, валидации и массовые операции живут в retail_calc/validate/bulk и о БД
// не знают.

// RetailInstance — экземпляр формы (ТЗ §1).
type RetailInstance struct {
	ID          int64  `json:"id"`
	CardID      int64  `json:"card_id"`
	Country     string `json:"country"`
	Year        int    `json:"year"`
	Month       int    `json:"month"`
	Currency    string `json:"currency"`
	LegalEntity string `json:"legal_entity"`
}

// RetailCellWrite — запись одной ячейки. Amount=nil означает ОЧИСТИТЬ ячейку:
// «пусто ≠ 0» (V-01), поэтому очистка — удаление строки, а не запись нуля.
type RetailCellWrite struct {
	CodeCFO int      `json:"code_cfo"`
	Metric  string   `json:"metric"`
	Year    int      `json:"year"`
	Month   int      `json:"month"`
	Amount  *float64 `json:"amount"`
	Source  string   `json:"source"`
	Note    string   `json:"note"`
}

// RetailRepo — доступ к данным формы.
type RetailRepo interface {
	EnsureInstance(ctx context.Context, c Card, actorID int64) (RetailInstance, error)
	Instance(ctx context.Context, cardID int64) (RetailInstance, error)
	Rows(ctx context.Context, instanceID int64) ([]RetailRow, error)
	// SyncRows приводит строки экземпляра к справочнику: добавляет новые магазины,
	// обновляет снапшот атрибутов и УДАЛЯЕТ строки магазинов, закрытых до начала
	// периода (V-09). Возвращает added/updated/removed.
	SyncRows(ctx context.Context, inst RetailInstance, stores []RetailStore, klient map[int]string, actorID int64) (int, int, int, error)
	SaveValues(ctx context.Context, instanceID int64, cells []RetailCellWrite, actorID int64, op string) (int, error)
	SaveComment(ctx context.Context, instanceID int64, codeCFO int, comment string, actorID int64) error
	Params(ctx context.Context, instanceID int64) ([]RetailParam, error)
	SaveParams(ctx context.Context, instanceID int64, params []RetailParam, actorID int64) error
	DeleteParam(ctx context.Context, instanceID, paramID int64) error
	SetLFLOverride(ctx context.Context, instanceID int64, codeCFO int, status, reason string, actorID int64) error
	// RegManagers — значения RegManager, закреплённые за пользователем (ТЗ §9).
	RegManagers(ctx context.Context, userID int64) ([]string, error)
	// PrevMetric — значения метрики предыдущего периода той же страны (фикс-часть
	// аренды, ТЗ §5): code_cfo → сумма за месяц предыдущего экземпляра.
	PrevMetric(ctx context.Context, country, metric string, year, month int) (map[int]float64, error)
}

type pgRetailRepo struct{ pool *pgxpool.Pool }

// NewRetailRepo — конструктор.
func NewRetailRepo(pool *pgxpool.Pool) RetailRepo { return &pgRetailRepo{pool: pool} }

// EnsureInstance — экземпляр формы для карточки (идемпотентно). Период/валюта/ЮЛ
// денормализуются из карточки: утверждённый экземпляр не должен «поехать» при
// правке реестра форм.
func (r *pgRetailRepo) EnsureInstance(ctx context.Context, c Card, actorID int64) (RetailInstance, error) {
	if c.FormCode != TemplateRetail {
		return RetailInstance{}, fmt.Errorf("карточка %d не является формой розницы (%s)", c.ID, c.FormCode)
	}
	var actor any
	if actorID != 0 {
		actor = actorID
	}
	var inst RetailInstance
	err := r.pool.QueryRow(ctx, `
		INSERT INTO tp_instance (card_id, country, period_year, period_month, currency, legal_entity, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (card_id) DO UPDATE SET updated_at = NOW()
		RETURNING id, card_id, country, period_year, period_month, currency, legal_entity`,
		c.ID, c.Country, c.Year, c.Month, c.Currency, c.LegalEntity, actor).
		Scan(&inst.ID, &inst.CardID, &inst.Country, &inst.Year, &inst.Month, &inst.Currency, &inst.LegalEntity)
	return inst, err
}

func (r *pgRetailRepo) Instance(ctx context.Context, cardID int64) (RetailInstance, error) {
	var inst RetailInstance
	err := r.pool.QueryRow(ctx, `
		SELECT id, card_id, country, period_year, period_month, currency, legal_entity
		  FROM tp_instance WHERE card_id = $1`, cardID).
		Scan(&inst.ID, &inst.CardID, &inst.Country, &inst.Year, &inst.Month, &inst.Currency, &inst.LegalEntity)
	return inst, err
}

// Rows — строки экземпляра со значениями и переопределением LFL.
func (r *pgRetailRepo) Rows(ctx context.Context, instanceID int64) ([]RetailRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.code_cfo, t.klient_id, t.name_cfo, t.city, t.lfl_status,
		       t.store_type, t.category, t.reg_manager, t.legal_entity, t.ploschad,
		       t.date_open, t.date_close, t.stage_of_store, t.code_fox, t.manager,
		       t.pl_analytic, t.comment, t.row_version,
		       o.lfl_status, o.reason
		  FROM tp_row t
		  LEFT JOIN tp_lfl_override o ON o.instance_id = t.instance_id AND o.code_cfo = t.code_cfo
		 WHERE t.instance_id = $1
		 ORDER BY t.city, t.code_cfo`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RetailRow, 0, 400)
	byID := map[int64]int{}
	for rows.Next() {
		var rr RetailRow
		var open, close_ *time.Time
		var ovStatus, ovReason *string
		if err := rows.Scan(&rr.ID, &rr.CodeCFO, &rr.KlientID, &rr.NameCFO, &rr.City, &rr.LFLStatus,
			&rr.StoreType, &rr.Category, &rr.RegManager, &rr.LegalEntity, &rr.Ploschad,
			&open, &close_, &rr.Stage, &rr.CodeFOX, &rr.Manager,
			&rr.PLAnalytic, &rr.Comment, &rr.RowVersion, &ovStatus, &ovReason); err != nil {
			return nil, err
		}
		rr.DateOpen = retailSnapshotDate(open)
		rr.DateClose = retailSnapshotDate(close_)
		// Эффективный LFL: переопределение финансиста поверх снапшота (ТЗ §3).
		rr.LFLEffective = rr.LFLStatus
		if ovStatus != nil && *ovStatus != "" {
			rr.LFLEffective, rr.LFLOverride = *ovStatus, true
			if ovReason != nil {
				rr.LFLReason = *ovReason
			}
		}
		rr.Values = []RetailValue{}
		byID[rr.ID] = len(out)
		out = append(out, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	vals, err := r.pool.Query(ctx, `
		SELECT v.row_id, v.metric, v.period_year, v.period_month, v.amount, v.source, v.note, v.updated_at
		  FROM tp_value v
		  JOIN tp_row t ON t.id = v.row_id
		 WHERE t.instance_id = $1
		 ORDER BY v.period_year, v.period_month`, instanceID)
	if err != nil {
		return nil, err
	}
	defer vals.Close()
	for vals.Next() {
		var rowID int64
		var v RetailValue
		var amt float64
		var updated time.Time
		if err := vals.Scan(&rowID, &v.Metric, &v.Year, &v.Month, &amt, &v.Source, &v.Note, &updated); err != nil {
			return nil, err
		}
		idx, ok := byID[rowID]
		if !ok {
			continue
		}
		v.Amount, v.UpdatedAt = fptr(amt), &updated
		out[idx].Values = append(out[idx].Values, v)
	}
	return out, vals.Err()
}

// SyncRows — привести строки экземпляра к справочнику магазинов (ТЗ §2).
//
// Закрытые до начала периода магазины в форму ввода НЕ попадают (V-09) — их
// строки удаляются вместе со значениями. Это безопасно: по закрытому магазину
// план обязан быть пустым или нулевым (V-03), терять нечего.
func (r *pgRetailRepo) SyncRows(ctx context.Context, inst RetailInstance, stores []RetailStore,
	klient map[int]string, actorID int64) (int, int, int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var actor any
	if actorID != 0 {
		actor = actorID
	}
	added, updated := 0, 0
	keep := make([]int, 0, len(stores))
	for _, st := range stores {
		if st.CodeCFO == 0 {
			continue
		}
		// V-09: магазин, закрытый до начала периода, в форму не попадает.
		if retailStoreClosedBefore(st.DateClose, inst.Year, inst.Month) {
			continue
		}
		keep = append(keep, st.CodeCFO)
		kl := st.KlientID
		if kl == "" {
			kl = klient[st.CodeCFO]
		}
		attrs, _ := json.Marshal(map[string]any{
			"group_cfo1": st.GroupCFO1, "channel": st.Channel, "cfo_old": st.CFOold,
		})
		var isNew bool
		if err := tx.QueryRow(ctx, `
			INSERT INTO tp_row (instance_id, code_cfo, klient_id, name_cfo, city, lfl_status,
			                    store_type, category, reg_manager, legal_entity, ploschad,
			                    date_open, date_close, stage_of_store, code_fox, manager,
			                    pl_analytic, attrs)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,
			        NULLIF($12,'')::date, NULLIF($13,'')::date, $14,$15,$16,$17,$18)
			ON CONFLICT (instance_id, code_cfo) DO UPDATE SET
			    klient_id = EXCLUDED.klient_id, name_cfo = EXCLUDED.name_cfo,
			    city = EXCLUDED.city, lfl_status = EXCLUDED.lfl_status,
			    store_type = EXCLUDED.store_type, category = EXCLUDED.category,
			    reg_manager = EXCLUDED.reg_manager, legal_entity = EXCLUDED.legal_entity,
			    ploschad = EXCLUDED.ploschad, date_open = EXCLUDED.date_open,
			    date_close = EXCLUDED.date_close, stage_of_store = EXCLUDED.stage_of_store,
			    code_fox = EXCLUDED.code_fox, manager = EXCLUDED.manager,
			    pl_analytic = EXCLUDED.pl_analytic, attrs = EXCLUDED.attrs,
			    row_version = tp_row.row_version + 1, updated_at = NOW()
			RETURNING (xmax = 0) AS inserted`,
			inst.ID, st.CodeCFO, kl, st.NameCFO, st.City, retailLFLStatus(st.LFLStatus),
			st.StoreType, st.Category, st.RegManager, st.CompanyMF, st.Ploschad,
			st.DateOpen, st.DateClose, st.Stage, st.CodeFOX, st.Manager,
			st.PLAnalytic, attrs).Scan(&isNew); err != nil {
			return 0, 0, 0, fmt.Errorf("магазин %d: %w", st.CodeCFO, err)
		}
		if isNew {
			added++
		} else {
			updated++
		}
	}
	_ = actor

	var removed int64
	if len(keep) > 0 {
		tag, err := tx.Exec(ctx, `DELETE FROM tp_row WHERE instance_id = $1 AND code_cfo <> ALL($2)`, inst.ID, keep)
		if err != nil {
			return 0, 0, 0, err
		}
		removed = tag.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, 0, err
	}
	return added, updated, int(removed), nil
}

// SaveValues — запись ячеек с историей (tp_value_version). Amount=nil очищает
// ячейку. op — что именно писало (ручная правка / код массовой операции).
func (r *pgRetailRepo) SaveValues(ctx context.Context, instanceID int64,
	cells []RetailCellWrite, actorID int64, op string) (int, error) {
	if len(cells) == 0 {
		return 0, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// code_cfo → row_id: ячейки приходят с ключом строки формы (V-04), а не с
	// внутренним id, чтобы импорт и массовые операции говорили на языке ТЗ.
	rowIDs := map[int]int64{}
	rows, err := tx.Query(ctx, `SELECT code_cfo, id FROM tp_row WHERE instance_id = $1`, instanceID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var code int
		var id int64
		if err := rows.Scan(&code, &id); err != nil {
			rows.Close()
			return 0, err
		}
		rowIDs[code] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var actor any
	if actorID != 0 {
		actor = actorID
	}
	saved := 0
	for _, c := range cells {
		rowID, ok := rowIDs[c.CodeCFO]
		if !ok {
			return 0, fmt.Errorf("магазина %d нет в форме — значение не сохранено", c.CodeCFO)
		}
		metric := c.Metric
		if metric == "" {
			metric = MetricSales
		}
		source := c.Source
		if source == "" {
			source = ValueManual
		}

		var oldAmt *float64
		var oldSrc string
		if err := tx.QueryRow(ctx, `
			SELECT amount, source FROM tp_value
			 WHERE row_id = $1 AND metric = $2 AND period_year = $3 AND period_month = $4`,
			rowID, metric, c.Year, c.Month).Scan(&oldAmt, &oldSrc); err != nil && err != pgx.ErrNoRows {
			return 0, err
		}

		if c.Amount == nil {
			if _, err := tx.Exec(ctx, `
				DELETE FROM tp_value
				 WHERE row_id = $1 AND metric = $2 AND period_year = $3 AND period_month = $4`,
				rowID, metric, c.Year, c.Month); err != nil {
				return 0, err
			}
		} else {
			if _, err := tx.Exec(ctx, `
				INSERT INTO tp_value (row_id, metric, period_year, period_month, amount, source, note, updated_by)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
				ON CONFLICT (row_id, metric, period_year, period_month) DO UPDATE SET
				    amount = EXCLUDED.amount, source = EXCLUDED.source, note = EXCLUDED.note,
				    updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
				rowID, metric, c.Year, c.Month, *c.Amount, source, c.Note, actor); err != nil {
				return 0, err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO tp_value_version (row_id, period_year, period_month, metric,
			                              amount_old, amount_new, source_old, source_new, op, changed_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			rowID, c.Year, c.Month, metric, oldAmt, c.Amount, oldSrc, source, op, actor); err != nil {
			return 0, err
		}
		saved++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return saved, nil
}

// SaveComment — комментарий к строке (ТЗ §3; обязателен при предупреждении, V-07).
func (r *pgRetailRepo) SaveComment(ctx context.Context, instanceID int64, codeCFO int, comment string, actorID int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tp_row SET comment = $3, updated_at = NOW()
		 WHERE instance_id = $1 AND code_cfo = $2`, instanceID, codeCFO, comment)
	return err
}

func (r *pgRetailRepo) Params(ctx context.Context, instanceID int64) ([]RetailParam, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.id, p.scope_kind, p.scope_value, p.param_code, p.param_value, p.note,
		       COALESCE(p.set_by, 0), TRIM(COALESCE(u.name, '') || ' ' || COALESCE(u.last_name, '')), p.set_at
		  FROM tp_period_param p
		  LEFT JOIN users u ON u.id = p.set_by
		 WHERE p.instance_id = $1
		 ORDER BY p.param_code, p.scope_kind, p.scope_value`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RetailParam, 0)
	for rows.Next() {
		var p RetailParam
		if err := rows.Scan(&p.ID, &p.ScopeKind, &p.ScopeValue, &p.ParamCode, &p.Value,
			&p.Note, &p.SetBy, &p.SetByName, &p.SetAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveParams — upsert параметров периода. «Кто задал и когда» пишется всегда
// (ТЗ §5): без этого нельзя объяснить, почему план пересчитался.
func (r *pgRetailRepo) SaveParams(ctx context.Context, instanceID int64, params []RetailParam, actorID int64) error {
	if len(params) == 0 {
		return nil
	}
	var actor any
	if actorID != 0 {
		actor = actorID
	}
	batch := &pgx.Batch{}
	for _, p := range params {
		batch.Queue(`
			INSERT INTO tp_period_param (instance_id, scope_kind, scope_value, param_code, param_value, note, set_by, set_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
			ON CONFLICT (instance_id, param_code, scope_kind, scope_value) DO UPDATE SET
			    param_value = EXCLUDED.param_value, note = EXCLUDED.note,
			    set_by = EXCLUDED.set_by, set_at = NOW()`,
			instanceID, p.ScopeKind, p.ScopeValue, p.ParamCode, p.Value, p.Note, actor)
	}
	res := r.pool.SendBatch(ctx, batch)
	defer res.Close()
	for range params {
		if _, err := res.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (r *pgRetailRepo) DeleteParam(ctx context.Context, instanceID, paramID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM tp_period_param WHERE instance_id = $1 AND id = $2`, instanceID, paramID)
	return err
}

// SetLFLOverride — переопределение LFL-статуса на период (ТЗ §3). Пустой status
// снимает переопределение; причина обязательна (проверка в сервисе).
func (r *pgRetailRepo) SetLFLOverride(ctx context.Context, instanceID int64, codeCFO int,
	status, reason string, actorID int64) error {
	if strings.TrimSpace(status) == "" {
		_, err := r.pool.Exec(ctx, `DELETE FROM tp_lfl_override WHERE instance_id = $1 AND code_cfo = $2`,
			instanceID, codeCFO)
		return err
	}
	var actor any
	if actorID != 0 {
		actor = actorID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tp_lfl_override (instance_id, code_cfo, lfl_status, reason, set_by)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (instance_id, code_cfo) DO UPDATE SET
		    lfl_status = EXCLUDED.lfl_status, reason = EXCLUDED.reason,
		    set_by = EXCLUDED.set_by, set_at = NOW()`,
		instanceID, codeCFO, status, reason, actor)
	return err
}

// RegManagers — значения RegManager пользователя (ТЗ §9). Пустой результат =
// пользователь не РМ; права тогда решает только ABAC-срез.
func (r *pgRetailRepo) RegManagers(ctx context.Context, userID int64) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT reg_manager FROM tp_reg_manager_map WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, 2)
	for rows.Next() {
		var rm string
		if err := rows.Scan(&rm); err != nil {
			return nil, err
		}
		if !retailRegManagerExcluded(rm) {
			out = append(out, rm)
		}
	}
	return out, rows.Err()
}

// PrevMetric — значения метрики за (year, month) из ЛЮБОГО экземпляра той же
// страны: фикс-часть аренды берётся из предыдущего периода (ТЗ §5, этап 1).
func (r *pgRetailRepo) PrevMetric(ctx context.Context, country, metric string, year, month int) (map[int]float64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.code_cfo, SUM(v.amount)
		  FROM tp_value v
		  JOIN tp_row t ON t.id = v.row_id
		  JOIN tp_instance i ON i.id = t.instance_id
		 WHERE i.country = $1 AND v.metric = $2 AND v.period_year = $3 AND v.period_month = $4
		 GROUP BY t.code_cfo`, country, metric, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]float64{}
	for rows.Next() {
		var code int
		var amt float64
		if err := rows.Scan(&code, &amt); err != nil {
			return nil, err
		}
		out[code] = amt
	}
	return out, rows.Err()
}
