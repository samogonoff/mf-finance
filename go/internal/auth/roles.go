package auth

// Роли Finance Cabinet. Перечисляются явно; всё, что приходит снаружи
// (PUT /api/admin/users/{id}/roles) — валидируется по белому списку Allowed.
//
// Иерархия:
//   ROLE_ADMIN        ⊇ ROLE_COST_ADMIN, ROLE_FINANCE_ADMIN, ROLE_PLANS_ADMIN,
//                       ROLE_BUDGET_ADMIN
//   ROLE_COST_ADMIN   ⊇ ROLE_COST_USER
//   ROLE_PLANS_ADMIN  ⊇ ROLE_PLANS_USER
//   ROLE_BUDGET_ADMIN ⊇ ROLE_BUDGET
//
// BUDGET-роли — гейт обособленного контура «Бюджетирование» (/budget/, свой
// репозиторий). Их проверяет nginx подзапросом auth_request к
// GET /api/auth/check?role=ROLE_BUDGET; сам контур ролями не распоряжается,
// см. docs/budget/PLAN-budget-contour.md.
//
// PLANS-роли — гейт доступа к модулю «Тактические планы» (docs/reports/plans/SPEC.md §5.1).
// PLANS_ADMIN — конфигуратор (НСИ, маршруты, назначение ответственных);
// PLANS_USER — участник процесса. Функциональные роли процесса (filler, approver…)
// и привязка к объекту — НЕ здесь, а в данных (plans_user_scope), см. SPEC §5.1.
//
// ROLE_USER — техническая, всегда добавляется в выпускаемый набор; задавать
// её снаружи нельзя (фильтруется в Allowed).

const (
	RoleUser         = "ROLE_USER"
	RoleAdmin        = "ROLE_ADMIN"
	RoleCostAdmin    = "ROLE_COST_ADMIN"
	RoleCostUser     = "ROLE_COST_USER"
	RoleFinanceAdmin = "ROLE_FINANCE_ADMIN"
	RolePlansAdmin   = "ROLE_PLANS_ADMIN"
	RolePlansUser    = "ROLE_PLANS_USER"
	RoleBudgetAdmin  = "ROLE_BUDGET_ADMIN"
	RoleBudgetUser   = "ROLE_BUDGET"
)

// Allowed — роли, которые админ может присваивать через API.
// ROLE_USER сюда не входит — она ставится автоматически.
var Allowed = []string{RoleAdmin, RoleCostAdmin, RoleCostUser, RoleFinanceAdmin, RolePlansAdmin, RolePlansUser, RoleBudgetAdmin, RoleBudgetUser}

var hierarchy = map[string][]string{
	RoleAdmin:       {RoleCostAdmin, RoleFinanceAdmin, RolePlansAdmin, RoleBudgetAdmin},
	RoleCostAdmin:   {RoleCostUser},
	RolePlansAdmin:  {RolePlansUser},
	RoleBudgetAdmin: {RoleBudgetUser},
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

// KnownRole — знает ли система такую роль (Allowed + техническая ROLE_USER).
//
// Нужна гейту обособленных контуров: опечатка в `?role=` — ошибка конфига, и
// отвечать на неё надо 403 («не пущу»), а не 200. Отдавать именно 4xx важно и
// по второй причине: наружу nginx пропускает как есть только 401 и 403, любой
// иной ответ auth_request он превращает в 500 для каждого пользователя раздела.
func KnownRole(role string) bool {
	if role == RoleUser {
		return true
	}
	for _, r := range Allowed {
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
