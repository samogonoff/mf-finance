package plans

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
)

type CfoExpenseRepo interface {
	Sync(context.Context, int64, []CfoExpenseRow) error
	Rows(context.Context, int64, CfoExpenseFilter) ([]CfoExpenseRow, int, error)
	Save(context.Context, int64, []CfoExpenseCell, int64) (int, error)
	Comment(context.Context, int64, CfoExpenseCell, string) error
	Bulk(context.Context, int64, CfoExpenseCell, string, int64) (int, error)
}
type cfoExpenseRepo struct{ pool *pgxpool.Pool }

func NewCfoExpenseRepo(p *pgxpool.Pool) CfoExpenseRepo { return &cfoExpenseRepo{p} }

func (s *cfoExpenseRepo) Sync(ctx context.Context, card int64, rows []CfoExpenseRow) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, r := range rows {
		var id int64
		err := tx.QueryRow(ctx, `INSERT INTO cfo_exp_row(card_id,code_cfo,code_pl,legal_entity,cfo,group_pl,expense,country,country_company)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT(card_id,code_cfo,code_pl,legal_entity) DO UPDATE SET cfo=EXCLUDED.cfo,group_pl=EXCLUDED.group_pl,expense=EXCLUDED.expense,country=EXCLUDED.country,country_company=EXCLUDED.country_company
			RETURNING id`, card, r.CodeCFO, r.CodePL, r.LegalEntity, r.CFO, r.GroupPL, r.Expense, r.Country, r.CountryCompany).Scan(&id)
		if err != nil {
			return err
		}
		for _, v := range r.Values {
			if v.Scenario == "plan" {
				continue
			}
			_, err = tx.Exec(ctx, `INSERT INTO cfo_exp_value(row_id,scenario,year,month,amount) VALUES($1,$2,$3,$4,$5)
				ON CONFLICT(row_id,scenario,year,month) DO UPDATE SET amount=EXCLUDED.amount,updated_at=now()`, id, v.Scenario, v.Year, v.Month, v.Amount)
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
func (s *cfoExpenseRepo) Rows(ctx context.Context, card int64, f CfoExpenseFilter) ([]CfoExpenseRow, int, error) {
	where := []string{"r.card_id=$1"}
	args := []any{card}
	add := func(q string, v any) { args = append(args, v); where = append(where, fmt.Sprintf(q, len(args))) }
	if f.CodeCFO > 0 {
		add("r.code_cfo=$%d", f.CodeCFO)
	}
	if f.CodePL > 0 {
		add("r.code_pl=$%d", f.CodePL)
	}
	if f.GroupPL != "" {
		add("r.group_pl=$%d", f.GroupPL)
	}
	if f.LegalEntity != "" {
		add("r.legal_entity=$%d", f.LegalEntity)
	}
	if f.CountryCompany != "" {
		add("r.country_company=$%d", f.CountryCompany)
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		where = append(where, fmt.Sprintf("(r.expense ILIKE $%d OR r.group_pl ILIKE $%d OR r.cfo ILIKE $%d OR r.code_pl::text ILIKE $%d)", len(args), len(args), len(args), len(args)))
	}
	order := "r.group_pl,r.code_cfo,r.code_pl,r.legal_entity"
	if columns := map[string]string{"code_cfo": "r.code_cfo", "code_pl": "r.code_pl", "group_pl": "r.group_pl", "expense": "r.expense", "cfo": "r.cfo", "legal_entity": "r.legal_entity", "country_company": "r.country_company"}; columns[f.Sort] != "" {
		order = columns[f.Sort]
	}
	if f.Desc {
		order += " DESC"
	}
	limit := f.Limit
	if limit <= 0 || limit > 50000 {
		limit = 50000
	}
	rows, err := s.pool.Query(ctx, "SELECT r.id,r.code_cfo,COALESCE(r.cfo,''),r.code_pl,r.legal_entity,r.group_pl,r.expense,r.country,r.country_company,r.comment FROM cfo_exp_row r WHERE "+strings.Join(where, " AND ")+" ORDER BY "+order+" LIMIT "+fmt.Sprint(limit)+" OFFSET "+fmt.Sprint(max(0, f.Offset)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []CfoExpenseRow{}
	for rows.Next() {
		var x CfoExpenseRow
		if err := rows.Scan(&x.ID, &x.CodeCFO, &x.CFO, &x.CodePL, &x.LegalEntity, &x.GroupPL, &x.Expense, &x.Country, &x.CountryCompany, &x.Comment); err != nil {
			return nil, 0, err
		}
		out = append(out, x)
	}
	for i := range out {
		vs, err := s.pool.Query(ctx, "SELECT scenario,year,month,amount FROM cfo_exp_value WHERE row_id=$1 ORDER BY year,month,scenario", out[i].ID)
		if err != nil {
			return nil, 0, err
		}
		for vs.Next() {
			var v CfoExpenseValue
			if err := vs.Scan(&v.Scenario, &v.Year, &v.Month, &v.Amount); err != nil {
				vs.Close()
				return nil, 0, err
			}
			out[i].Values = append(out[i].Values, v)
		}
		vs.Close()
	}
	var total int
	_ = s.pool.QueryRow(ctx, "SELECT count(*) FROM cfo_exp_row r WHERE "+strings.Join(where, " AND "), args...).Scan(&total)
	return out, total, rows.Err()
}
func (s *cfoExpenseRepo) Save(ctx context.Context, card int64, cells []CfoExpenseCell, actor int64) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	n := 0
	for _, c := range cells {
		if c.Month < 1 || c.Month > 12 {
			return 0, fmt.Errorf("invalid month")
		}
		var id int64
		if err := tx.QueryRow(ctx, "SELECT id FROM cfo_exp_row WHERE card_id=$1 AND code_cfo=$2 AND code_pl=$3 AND legal_entity=$4", card, c.CodeCFO, c.CodePL, c.LegalEntity).Scan(&id); err != nil {
			return 0, err
		}
		_, err = tx.Exec(ctx, "INSERT INTO cfo_exp_value(row_id,scenario,year,month,amount,updated_by) VALUES($1,'plan',$2,$3,$4,$5) ON CONFLICT(row_id,scenario,year,month) DO UPDATE SET amount=EXCLUDED.amount,updated_by=EXCLUDED.updated_by", id, c.Year, c.Month, c.Amount, actor)
		if err != nil {
			return 0, err
		}
		n++
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, nil
}
func (s *cfoExpenseRepo) Comment(ctx context.Context, card int64, c CfoExpenseCell, comment string) error {
	_, e := s.pool.Exec(ctx, "UPDATE cfo_exp_row SET comment=$1 WHERE card_id=$2 AND code_cfo=$3 AND code_pl=$4 AND legal_entity=$5", comment, card, c.CodeCFO, c.CodePL, c.LegalEntity)
	return e
}
func (s *cfoExpenseRepo) Bulk(ctx context.Context, card int64, c CfoExpenseCell, op string, actor int64) (int, error) {
	cells := []CfoExpenseCell{}
	from, to := c.Month, 12
	switch op {
	case "year":
		from = 1
	case "to_end":
		if from < 1 || from > 12 {
			return 0, fmt.Errorf("invalid month")
		}
	case "clear":
		from = 1
	default:
		return 0, fmt.Errorf("unknown bulk operation %q", op)
	}
	for m := from; m <= to; m++ {
		v := c
		v.Month = m
		if op == "clear" {
			v.Amount = nil
		}
		cells = append(cells, v)
	}
	return s.Save(ctx, card, cells, actor)
}
