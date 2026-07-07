package plans

import (
	"context"
	"errors"
	"net/http"
)

// ABAC модуля «Тактические планы» (docs/reports/plans/SPEC.md §5.1).
// Глобальная роль ROLE_PLANS_ADMIN/ROLE_ADMIN → полный доступ (обход ABAC).
// Иначе — срез из plans_user_scope: разрешённый набор code_cfo (площадок).
// Чужие площадки не видны (фильтр формы) и не записываются (отказ на PUT).

// Principal — кто делает запрос: id + признак админа планов.
type Principal struct {
	UserID     int64
	PlansAdmin bool
}

// PrincipalFunc извлекает Principal из запроса (ставится в main.go поверх auth).
type PrincipalFunc func(r *http.Request) (Principal, bool)

// UserScope — запись «Пользователь ↔ роль ↔ объект».
type UserScope struct {
	UserID      int64  `json:"user_id"`
	Role        string `json:"role"`
	StageCode   string `json:"stage_code"`
	Country     string `json:"country"`
	LegalEntity string `json:"legal_entity"`
	CodeCFO     []int  `json:"code_cfo"`
}

// ScopeStore — доступ к ABAC-срезам.
type ScopeStore interface {
	// UserCodeCFOs — объединённый набор разрешённых code_cfo пользователя.
	UserCodeCFOs(ctx context.Context, userID int64) ([]int, error)
	// UpsertScope — назначение/обновление среза (админ процессов).
	UpsertScope(ctx context.Context, sc UserScope) error
}

func allowedSet(codes []int) map[int]bool {
	m := make(map[int]bool, len(codes))
	for _, c := range codes {
		m[c] = true
	}
	return m
}

// applyScope фильтрует форму до разрешённых площадок (admin — без фильтра).
func applyScope(form MpForm, allowed map[int]bool, admin bool) MpForm {
	if admin {
		return form
	}
	platforms := make([]MarketplaceRow, 0, len(form.Platforms))
	for _, p := range form.Platforms {
		if allowed[p.CodeCFO] {
			platforms = append(platforms, p)
		}
	}
	blocks := make([]FormBlock, 0, len(form.Blocks))
	for _, b := range form.Blocks {
		rows := make([]FormRow, 0, len(b.Rows))
		for _, r := range b.Rows {
			if allowed[r.CodeCFO] {
				rows = append(rows, r)
			}
		}
		b.Rows = rows
		blocks = append(blocks, b)
	}
	form.Platforms = platforms
	form.Blocks = blocks
	return form
}

// checkScopeRows отклоняет PUT, если хоть одна ячейка вне ABAC-среза (admin — ок).
func checkScopeRows(req SaveMpFormRequest, allowed map[int]bool, admin bool) error {
	if admin {
		return nil
	}
	for _, r := range req.Rows {
		if !allowed[r.CodeCFO] {
			return errors.New("code_cfo вне ABAC-среза пользователя")
		}
	}
	return nil
}
