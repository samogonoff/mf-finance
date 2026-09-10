package plans

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Хранилище реестров формы 1.5 (миграция 0054). Только SQL: формулы — в
// exp_registry.go, правила доступа — в exp_registry_service.go.

// ExpRegRepo — операции над строками реестров.
type ExpRegRepo interface {
	RegRows(ctx context.Context, instanceID int64, kind string) ([]ExpRegRow, error)
	SaveRegRow(ctx context.Context, instanceID int64, r ExpRegRow, actorID int64) (int64, error)
	DeleteRegRow(ctx context.Context, instanceID, rowID int64, actorID int64) error
	CopyRegRow(ctx context.Context, instanceID, rowID int64, actorID int64) (int64, error)
}

type expRegRepo struct{ pool *pgxpool.Pool }

// NewExpRegRepo — конструктор.
func NewExpRegRepo(pool *pgxpool.Pool) ExpRegRepo { return &expRegRepo{pool: pool} }

// RegRows — строки реестра с их месяцами.
func (s *expRegRepo) RegRows(ctx context.Context, instanceID int64, kind string) ([]ExpRegRow, error) {
	rs, err := s.pool.Query(ctx, `
		SELECT id, kind, sort_order, code_cfo, code_pl, move_to_250, excluded, attrs, note
		  FROM exp_reg_row
		 WHERE instance_id = $1 AND kind = $2
		 ORDER BY sort_order, id`, instanceID, kind)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	rows := make([]ExpRegRow, 0, 32)
	idx := map[int64]int{}
	for rs.Next() {
		var (
			r   ExpRegRow
			raw []byte
		)
		if err := rs.Scan(&r.ID, &r.Kind, &r.SortOrder, &r.CodeCFO, &r.CodePL,
			&r.MoveTo250, &r.Excluded, &raw, &r.Note); err != nil {
			return nil, err
		}
		r.Attrs = map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &r.Attrs)
		}
		r.Months = map[int]float64{}
		idx[r.ID] = len(rows)
		rows = append(rows, r)
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return rows, nil
	}

	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	vs, err := s.pool.Query(ctx, `
		SELECT row_id, month, amount FROM exp_reg_value
		 WHERE row_id = ANY($1) AND amount IS NOT NULL`, ids)
	if err != nil {
		return nil, err
	}
	defer vs.Close()
	for vs.Next() {
		var (
			rowID int64
			month int
			amt   float64
		)
		if err := vs.Scan(&rowID, &month, &amt); err != nil {
			return nil, err
		}
		if i, ok := idx[rowID]; ok {
			rows[i].Months[month] = amt
		}
	}
	return rows, vs.Err()
}

// SaveRegRow — создание или правка строки вместе с её месяцами.
//
// Месяцы переписываются ЦЕЛИКОМ: строка реестра — единица правки, и частичное
// обновление оставило бы месяцы от прошлой версии строки (в эталоне их правят
// разом, копируя соседнюю строку).
func (s *expRegRepo) SaveRegRow(ctx context.Context, instanceID int64, r ExpRegRow, actorID int64) (int64, error) {
	if !ExpRegKindKnown(r.Kind) {
		return 0, fmt.Errorf("неизвестный реестр %q", r.Kind)
	}
	attrs, err := json.Marshal(r.Attrs)
	if err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	id := r.ID
	if id == 0 {
		err = tx.QueryRow(ctx, `
			INSERT INTO exp_reg_row (instance_id, kind, sort_order, code_cfo, code_pl,
			                         move_to_250, excluded, attrs, note, created_by)
			VALUES ($1,$2,COALESCE(NULLIF($3,0), (SELECT COALESCE(MAX(sort_order),0)+10 FROM exp_reg_row WHERE instance_id=$1 AND kind=$2)),
			        $4,$5,$6,$7,$8,$9,$10)
			RETURNING id`,
			instanceID, r.Kind, r.SortOrder, r.CodeCFO, r.CodePL,
			r.MoveTo250, r.Excluded, attrs, r.Note, nullID(actorID)).Scan(&id)
		if err != nil {
			return 0, err
		}
	} else {
		ct, err := tx.Exec(ctx, `
			UPDATE exp_reg_row
			   SET sort_order = $3, code_cfo = $4, code_pl = $5, move_to_250 = $6,
			       excluded = $7, attrs = $8, note = $9, updated_at = NOW()
			 WHERE id = $1 AND instance_id = $2`,
			id, instanceID, r.SortOrder, r.CodeCFO, r.CodePL, r.MoveTo250, r.Excluded, attrs, r.Note)
		if err != nil {
			return 0, err
		}
		if ct.RowsAffected() == 0 {
			return 0, fmt.Errorf("строка реестра %d не найдена в форме", id)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM exp_reg_value WHERE row_id = $1`, id); err != nil {
			return 0, err
		}
	}

	b := &pgx.Batch{}
	n := 0
	for m := 1; m <= 12; m++ {
		v, ok := r.Months[m]
		if !ok {
			continue // не заполнено; ноль вводится явно и сохраняется
		}
		b.Queue(`INSERT INTO exp_reg_value (row_id, month, amount) VALUES ($1,$2,$3)
		         ON CONFLICT (row_id, month) DO UPDATE SET amount = EXCLUDED.amount`, id, m, v)
		n++
	}
	if n > 0 {
		br := tx.SendBatch(ctx, b)
		for i := 0; i < n; i++ {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return 0, err
			}
		}
		if err := br.Close(); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit(ctx)
}

// DeleteRegRow — удаление строки С ЗАПИСЬЮ В ЖУРНАЛ вместе со значениями
// (ТЗ §5.5): без значений на вопрос «куда делись эти суммы» ответить нечем.
func (s *expRegRepo) DeleteRegRow(ctx context.Context, instanceID, rowID int64, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var kind string
	var payload []byte
	err = tx.QueryRow(ctx, `
		SELECT r.kind,
		       jsonb_build_object(
		         'id', r.id, 'kind', r.kind, 'code_cfo', r.code_cfo, 'code_pl', r.code_pl,
		         'move_to_250', r.move_to_250, 'excluded', r.excluded,
		         'attrs', r.attrs, 'note', r.note,
		         'months', COALESCE((SELECT jsonb_object_agg(v.month::text, v.amount)
		                               FROM exp_reg_value v WHERE v.row_id = r.id), '{}'::jsonb))
		  FROM exp_reg_row r
		 WHERE r.id = $1 AND r.instance_id = $2`, rowID, instanceID).Scan(&kind, &payload)
	if err != nil {
		return fmt.Errorf("строка реестра %d не найдена в форме", rowID)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO exp_reg_deleted (instance_id, kind, payload, deleted_by)
		VALUES ($1,$2,$3,$4)`, instanceID, kind, payload, nullID(actorID)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM exp_reg_row WHERE id = $1`, rowID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CopyRegRow — копия строки со всеми месяцами (ТЗ §5.5): так заводят несколько
// похожих командировок или ИТ-позиций.
func (s *expRegRepo) CopyRegRow(ctx context.Context, instanceID, rowID int64, actorID int64) (int64, error) {
	var newID int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO exp_reg_row (instance_id, kind, sort_order, code_cfo, code_pl,
		                         move_to_250, excluded, attrs, note, created_by)
		SELECT r.instance_id, r.kind, r.sort_order + 1, r.code_cfo, r.code_pl,
		       r.move_to_250, r.excluded, r.attrs, r.note, $3
		  FROM exp_reg_row r
		 WHERE r.id = $1 AND r.instance_id = $2
		RETURNING id`, rowID, instanceID, nullID(actorID)).Scan(&newID)
	if err != nil {
		return 0, fmt.Errorf("строка реестра %d не найдена в форме", rowID)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO exp_reg_value (row_id, month, amount)
		SELECT $1, v.month, v.amount FROM exp_reg_value v WHERE v.row_id = $2`, newID, rowID); err != nil {
		return 0, err
	}
	return newID, nil
}
