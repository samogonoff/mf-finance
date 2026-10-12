package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CfoExpenseRegistryStore persists only registry rows. The form grid is never
// touched by this store; transfer remains an explicit user operation elsewhere.
type CfoExpenseRegistryStore interface {
	Rows(context.Context, int64, string, string) ([]CfoExpenseRegistryRow, error)
	Save(context.Context, int64, string, string, CfoExpenseRegistryRow, int64) (int64, error)
	Delete(context.Context, int64, string, string, int64, int64) error
	Copy(context.Context, int64, string, string, int64, int64) (int64, error)
}
type pgCfoExpenseRegistryStore struct{ pool *pgxpool.Pool }

func NewCfoExpenseRegistryStore(pool *pgxpool.Pool) CfoExpenseRegistryStore {
	return &pgCfoExpenseRegistryStore{pool: pool}
}
func (s *pgCfoExpenseRegistryStore) Rows(ctx context.Context, cardID int64, scope, sheet string) ([]CfoExpenseRegistryRow, error) {
	rs, err := s.pool.Query(ctx, `SELECT id,attrs,body FROM cfo_exp_registry WHERE card_id=$1 AND book_scope=$2 AND sheet=$3 ORDER BY sort_order,id`, cardID, scope, sheet)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	out := []CfoExpenseRegistryRow{}
	for rs.Next() {
		var r CfoExpenseRegistryRow
		var attrs, body []byte
		if err := rs.Scan(&r.ID, &attrs, &body); err != nil {
			return nil, err
		}
		r.CardID = cardID
		r.BookScope = scope
		r.Sheet = sheet
		_ = json.Unmarshal(attrs, &r.Attrs)
		if r.Attrs == nil {
			r.Attrs = map[string]any{}
		}
		var x struct {
			Months map[string]*float64 `json:"months"`
		}
		_ = json.Unmarshal(body, &x)
		r.Months = map[int]*float64{}
		for k, v := range x.Months {
			var m int
			_, e := fmt.Sscan(k, &m)
			if e == nil {
				r.Months[m] = v
			}
		}
		out = append(out, r)
	}
	return out, rs.Err()
}
func (s *pgCfoExpenseRegistryStore) Save(ctx context.Context, cardID int64, scope, sheet string, r CfoExpenseRegistryRow, actor int64) (int64, error) {
	attrs, e := json.Marshal(r.Attrs)
	if e != nil {
		return 0, e
	}
	months := map[string]*float64{}
	for m, v := range r.Months {
		months[fmt.Sprint(m)] = v
	}
	body, e := json.Marshal(map[string]any{"months": months})
	if e != nil {
		return 0, e
	}
	var id int64
	if r.ID == 0 {
		e = s.pool.QueryRow(ctx, `INSERT INTO cfo_exp_registry(card_id,book_scope,sheet,attrs,body,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$6,$6) RETURNING id`, cardID, scope, sheet, attrs, body, actor).Scan(&id)
	} else {
		id = r.ID
		e = s.pool.QueryRow(ctx, `UPDATE cfo_exp_registry SET attrs=$4,body=$5,updated_by=$6,updated_at=now() WHERE id=$1 AND card_id=$2 AND book_scope=$3 AND sheet=$7 RETURNING id`, id, cardID, scope, attrs, body, actor, sheet).Scan(&id)
	}
	return id, e
}
func (s *pgCfoExpenseRegistryStore) Delete(ctx context.Context, cardID int64, scope, sheet string, id, actor int64) error {
	ct, e := s.pool.Exec(ctx, `DELETE FROM cfo_exp_registry WHERE id=$1 AND card_id=$2 AND book_scope=$3 AND sheet=$4`, id, cardID, scope, sheet)
	if e != nil {
		return e
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("registry row %d not found", id)
	}
	return nil
}
func (s *pgCfoExpenseRegistryStore) Copy(ctx context.Context, cardID int64, scope, sheet string, id, actor int64) (int64, error) {
	var n int64
	e := s.pool.QueryRow(ctx, `INSERT INTO cfo_exp_registry(card_id,book_scope,sheet,attrs,body,created_by,updated_by) SELECT card_id,book_scope,sheet,attrs,body,$5,$5 FROM cfo_exp_registry WHERE id=$1 AND card_id=$2 AND book_scope=$3 AND sheet=$4 RETURNING id`, id, cardID, scope, sheet, actor).Scan(&n)
	return n, e
}
