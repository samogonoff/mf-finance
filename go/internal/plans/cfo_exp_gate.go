package plans

import (
	"context"
	"fmt"
)

// routedExpSubmitGate keeps the legacy 1.5 validation out of code-based CFO
// cards. Both card families share TemplateExp but have different row stores and
// validation contracts.
type routedExpSubmitGate struct {
	legacy SubmitGate
	cfo    SubmitGate
}

func NewRoutedExpSubmitGate(legacy, cfo SubmitGate) SubmitGate {
	return &routedExpSubmitGate{legacy: legacy, cfo: cfo}
}

func (g *routedExpSubmitGate) CanSubmit(ctx context.Context, c Card) ([]string, error) {
	if _, ok := CfoExpSpecForScope(c.ScopeKey); ok {
		if g.cfo == nil {
			return nil, nil
		}
		return g.cfo.CanSubmit(ctx, c)
	}
	if g.legacy == nil {
		return nil, nil
	}
	return g.legacy.CanSubmit(ctx, c)
}

type cfoSubmitGate struct {
	svc *CfoExpenseService
}

func NewCfoSubmitGate(svc *CfoExpenseService) SubmitGate { return &cfoSubmitGate{svc: svc} }

func (g *cfoSubmitGate) CanSubmit(ctx context.Context, c Card) ([]string, error) {
	if g.svc == nil {
		return nil, nil
	}
	issues, err := g.svc.Validate(ctx, Principal{PlansAdmin: true}, c.ID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		if i.Blocking {
			out = append(out, fmt.Sprintf("%s: %s", i.Code, i.Message))
		}
	}
	return out, nil
}
