package plans

import (
	"context"
	"fmt"
)

type CfoExpenseSource interface {
	Sync(context.Context, CfoExpFormCode, string, []int) ([]ExpCostCell, error)
}
type cfoExpenseSource struct{ src ExpDataSource }

func NewCfoExpenseSource(s ExpDataSource) CfoExpenseSource { return &cfoExpenseSource{s} }
func (s *cfoExpenseSource) Sync(ctx context.Context, code CfoExpFormCode, country string, years []int) ([]ExpCostCell, error) {
	if s == nil || s.src == nil {
		return nil, fmt.Errorf("source unavailable")
	}
	return s.src.Costs(ctx, CfoExpKind(code), country, years)
}
