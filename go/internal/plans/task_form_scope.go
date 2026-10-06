package plans

import (
	"fmt"
	"net/http"
)

// scopedMpForm applies one complete ABAC rule to each platform. Authorizing a
// whole multi-segment task from one matching CFO leaks other platforms in JSON
// and in Excel; both endpoints use the same filtered form.
func (h *Handler) scopedMpForm(r *http.Request, f MpTaskForm) (MpTaskForm, error) {
	p, ok := h.principal(r)
	if !ok {
		return MpTaskForm{}, fmt.Errorf("пользователь не определён")
	}
	if p.PlansAdmin {
		return f, nil
	}
	if h.form == nil || h.form.scope == nil {
		return MpTaskForm{}, fmt.Errorf("срез доступа недоступен")
	}
	scope, err := scopeFilterFor(r.Context(), h.form.scope, p)
	if err != nil {
		return MpTaskForm{}, fmt.Errorf("срез доступа недоступен: %w", err)
	}
	filtered, err := filterMpTaskForm(f, scope)
	if err != nil {
		return MpTaskForm{}, err
	}
	// A partial task may leave just one card. Rebuild that card's mode/snapshot
	// rather than returning the original (usually large) card's approval state.
	if h.tasks != nil && f.Segment != filtered.Segment && (filtered.Segment == "large" || filtered.Segment == "small") {
		narrowed, err := h.tasks.MpFormData(r.Context(), f.Task.ID, f.Currency, filtered.Segment)
		if err != nil {
			return MpTaskForm{}, err
		}
		filtered, err = filterMpTaskForm(narrowed, scope)
		if err != nil {
			return MpTaskForm{}, err
		}
	}
	// Covered-card metadata follows the same visible platform slice.
	if len(filtered.Task.CoveredCardIDs) > 0 && h.cards != nil {
		cards, err := h.cards.Cards(r.Context(), f.Task.PlID)
		if err != nil {
			return MpTaskForm{}, err
		}
		segments := map[string]bool{}
		for _, p := range filtered.Platforms {
			segments[p.Segment] = true
		}
		filtered.Task.CoveredCardIDs = nil
		for _, c := range cards {
			if c.FormCode == TemplateMP && segments[c.ScopeKey] {
				filtered.Task.CoveredCardIDs = append(filtered.Task.CoveredCardIDs, c.ID)
			}
		}
		if len(filtered.Task.CoveredCardIDs) == 1 {
			filtered.Task.CardID = filtered.Task.CoveredCardIDs[0]
		}
	}
	return filtered, nil
}

func filterMpTaskForm(f MpTaskForm, scope ScopeFilter) (MpTaskForm, error) {
	if scope.Admin {
		return f, nil
	}
	allowed := map[int]bool{}
	platforms := make([]MpFormPlatform, 0, len(f.Platforms))
	for _, p := range f.Platforms {
		for _, rule := range scope.Rules {
			if ownerRuleAllows(rule, p) {
				allowed[p.CodeCFO] = true
				platforms = append(platforms, p)
				break
			}
		}
	}
	if len(platforms) == 0 {
		return MpTaskForm{}, fmt.Errorf("задание вне вашего среза доступа")
	}
	f.Platforms = platforms
	cells := make([]MpFormCell, 0, len(f.Cells))
	for _, c := range f.Cells {
		if allowed[c.CodeCFO] {
			cells = append(cells, c)
		}
	}
	f.Cells = cells
	hints := make([]FieldHint, 0, len(f.Hints))
	for _, h := range f.Hints {
		if allowed[h.CodeCFO] {
			hints = append(hints, h)
		}
	}
	f.Hints = hints
	conditions := map[int]MpConditions{}
	for c, v := range f.Conditions {
		if allowed[c] {
			conditions[c] = v
		}
	}
	f.Conditions = conditions
	vats := map[int]MpVatBasis{}
	for c, v := range f.VatByCfo {
		if allowed[c] {
			vats[c] = v
		}
	}
	f.VatByCfo = vats
	missing := make([]int, 0, len(f.VatMissing))
	for _, c := range f.VatMissing {
		if allowed[c] {
			missing = append(missing, c)
		}
	}
	f.VatMissing = missing
	codes := make([]int, 0, len(f.Platforms))
	segments := map[string]bool{}
	for _, p := range f.Platforms {
		codes = append(codes, p.CodeCFO)
		segments[p.Segment] = true
	}
	f.Task.CfoCodes = codes
	f.Task.CfoCount = len(codes)
	if len(segments) == 1 {
		for seg := range segments {
			f.Segment = seg
		}
	}
	// The header must reflect the visible subset as well.
	f.VatMixed = false
	f.VatFromPlan = true
	for i, p := range f.Platforms {
		v, ok := f.VatByCfo[p.CodeCFO]
		if !ok {
			f.VatFromPlan = false
			continue
		}
		if i == 0 {
			f.Vat = v.Rate()
		} else if v.Rate() != f.Vat {
			f.VatMixed = true
		}
		if v.Missing {
			f.VatFromPlan = false
		}
	}
	return f, nil
}
