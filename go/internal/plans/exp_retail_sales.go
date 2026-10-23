package plans

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RetailSalesPlanSource reads entered retail sales from the same tactical plan.
// An absent month is distinct from an explicitly entered zero.
type RetailSalesPlanSource interface {
	RetailSalesPlan(ctx context.Context, plID int64, country, currency string, year int) (map[int]map[int]float64, error)
}

type pgRetailSalesPlan struct {
	pool *pgxpool.Pool
}

func NewPGRetailSalesPlan(pool *pgxpool.Pool) RetailSalesPlanSource {
	return &pgRetailSalesPlan{pool: pool}
}

func (s *pgRetailSalesPlan) RetailSalesPlan(ctx context.Context, plID int64, country, currency string, year int) (map[int]map[int]float64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.code_cfo, v.period_month, SUM(v.amount)
		  FROM tp_value v
		  JOIN tp_row w ON w.id = v.row_id
		  JOIN tp_instance i ON i.id = w.instance_id
		  JOIN form_card c ON c.id = i.card_id
		 WHERE c.pl_id = $1 AND i.country = $2 AND i.currency = $3
		   AND v.period_year = $4 AND v.metric = 'sales'
		 GROUP BY w.code_cfo, v.period_month`, plID, country, currency, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int]map[int]float64{}
	for rows.Next() {
		var cfo, month int
		var amount float64
		if err := rows.Scan(&cfo, &month, &amount); err != nil {
			return nil, err
		}
		if out[cfo] == nil {
			out[cfo] = map[int]float64{}
		}
		out[cfo][month] = amount
	}
	return out, rows.Err()
}
