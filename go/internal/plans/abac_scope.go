package plans

import "strings"

// Полный ABAC-срез: страна, ЮЛ, шаг, площадка/магазин.
//
// В plans_user_scope колонки country/legal_entity/stage_code существуют с 0012,
// но в проверках участвовал ТОЛЬКО объединённый набор code_cfo (см. UserCodeCFOs).
// Новые ТЗ требуют остальных измерений: МП §11 («RBAC + ABAC по группе, площадке,
// стране, ЮЛ, периоду и этапу»), Розница §9 («ограничение по Country, Channel,
// CodeCFO, RegManager, периоду, этапу»).
//
// Семантика правила: пустое поле = «без ограничения по этому измерению».
// Пустой набор правил = доступа нет (как и раньше — незаполненный срез не даёт
// прав ни на что, кроме случая PlansAdmin).

// ScopeFilter — набор правил пользователя.
type ScopeFilter struct {
	Admin bool        `json:"admin"`
	Rules []UserScope `json:"rules"`
}

// NewScopeFilter — фильтр из правил.
func NewScopeFilter(admin bool, rules []UserScope) ScopeFilter {
	return ScopeFilter{Admin: admin, Rules: rules}
}

// ruleAllows — правило разрешает конкретную комбинацию.
func ruleAllows(r UserScope, codeCFO int, country, legalEntity string) bool {
	if len(r.CodeCFO) > 0 && codeCFO != 0 {
		found := false
		for _, c := range r.CodeCFO {
			if c == codeCFO {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if r.Country != "" && country != "" && !strings.EqualFold(r.Country, country) {
		return false
	}
	if r.LegalEntity != "" && legalEntity != "" && !strings.EqualFold(r.LegalEntity, legalEntity) {
		return false
	}
	return true
}

// Allows — доступ к объекту (ЦФО в стране/ЮЛ). codeCFO=0 — проверка только по
// стране и ЮЛ (например, доступ к карточке формы целиком).
func (f ScopeFilter) Allows(codeCFO int, country, legalEntity string) bool {
	if f.Admin {
		return true
	}
	for _, r := range f.Rules {
		if ruleAllows(r, codeCFO, country, legalEntity) {
			return true
		}
	}
	return false
}

// AllowsStep — право действовать на шаге маршрута. Правило с пустым stage_code
// разрешает любой шаг (обычная ситуация: исполнителю назначены площадки без
// привязки к этапу).
func (f ScopeFilter) AllowsStep(step string) bool {
	if f.Admin {
		return true
	}
	for _, r := range f.Rules {
		if r.StageCode == "" || r.StageCode == step {
			return true
		}
	}
	return false
}

// AllowsCard — доступ к карточке формы (по стране и ЮЛ карточки).
func (f ScopeFilter) AllowsCard(c Card) bool {
	return f.Allows(0, c.Country, c.LegalEntity)
}

// CodeCFOs — объединённый набор разрешённых ЦФО (для фильтрации сеток).
// Пустой результат при непустых правилах означает «ограничений по ЦФО нет»:
// проверять такие правила нужно через Allows, а не по набору.
func (f ScopeFilter) CodeCFOs() []int {
	seen := map[int]bool{}
	out := make([]int, 0)
	for _, r := range f.Rules {
		for _, c := range r.CodeCFO {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// Unrestricted — есть ли правило без ограничения по ЦФО (доступ ко всем ЦФО
// в рамках своей страны/ЮЛ).
func (f ScopeFilter) Unrestricted() bool {
	if f.Admin {
		return true
	}
	for _, r := range f.Rules {
		if len(r.CodeCFO) == 0 {
			return true
		}
	}
	return false
}
