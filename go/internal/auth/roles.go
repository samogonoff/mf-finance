package auth

// Роли Finance Cabinet. Перечисляются явно; всё, что приходит снаружи
// (PUT /api/admin/users/{id}/roles) — валидируется по белому списку Allowed.
//
// Иерархия:
//   ROLE_ADMIN        ⊇ ROLE_COST_ADMIN, ROLE_FINANCE_ADMIN
//   ROLE_COST_ADMIN   ⊇ ROLE_COST_USER
//
// ROLE_USER — техническая, всегда добавляется в выпускаемый набор; задавать
// её снаружи нельзя (фильтруется в Allowed).

const (
	RoleUser         = "ROLE_USER"
	RoleAdmin        = "ROLE_ADMIN"
	RoleCostAdmin    = "ROLE_COST_ADMIN"
	RoleCostUser     = "ROLE_COST_USER"
	RoleFinanceAdmin = "ROLE_FINANCE_ADMIN"
)

// Allowed — роли, которые админ может присваивать через API.
// ROLE_USER сюда не входит — она ставится автоматически.
var Allowed = []string{RoleAdmin, RoleCostAdmin, RoleCostUser, RoleFinanceAdmin}

var hierarchy = map[string][]string{
	RoleAdmin:     {RoleCostAdmin, RoleFinanceAdmin},
	RoleCostAdmin: {RoleCostUser},
}

// ExpandRoles разворачивает прямые роли по иерархии и всегда добавляет
// ROLE_USER. Результат — отсортированный уникальный список.
func ExpandRoles(direct []string) []string {
	seen := map[string]struct{}{RoleUser: {}}
	queue := append([]string(nil), direct...)
	for len(queue) > 0 {
		r := queue[0]
		queue = queue[1:]
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		queue = append(queue, hierarchy[r]...)
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sortStrings(out)
	return out
}

// HasRole — проверяет наличие роли у пользователя с учётом иерархии.
func HasRole(u *User, role string) bool {
	if u == nil {
		return false
	}
	for _, r := range ExpandRoles(u.Roles) {
		if r == role {
			return true
		}
	}
	return false
}

// FilterAllowed — оставляет из входного списка только роли из Allowed,
// дедуплицирует. Используется при валидации входящего PUT /roles.
func FilterAllowed(in []string) []string {
	allow := map[string]struct{}{}
	for _, r := range Allowed {
		allow[r] = struct{}{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, r := range in {
		if _, ok := allow[r]; !ok {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
