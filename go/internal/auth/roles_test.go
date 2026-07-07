package auth

import (
	"reflect"
	"sort"
	"testing"
)

func TestExpandRoles_AlwaysIncludesUser(t *testing.T) {
	got := ExpandRoles(nil)
	if !contains(got, RoleUser) {
		t.Errorf("ExpandRoles(nil) must include %q, got %v", RoleUser, got)
	}

	got = ExpandRoles([]string{})
	if !contains(got, RoleUser) {
		t.Errorf("ExpandRoles([]) must include %q, got %v", RoleUser, got)
	}

	if len(got) != 1 || got[0] != RoleUser {
		t.Errorf("ExpandRoles([]) must be exactly [%q], got %v", RoleUser, got)
	}
}

func TestExpandRoles_AdminExpandsFullTree(t *testing.T) {
	got := ExpandRoles([]string{RoleAdmin})
	want := []string{
		RoleAdmin, RoleCostAdmin, RoleCostUser, RoleFinanceAdmin,
		RolePlansAdmin, RolePlansUser, RoleUser,
	}
	sort.Strings(want)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ROLE_ADMIN should expand to all descendants + ROLE_USER\n want %v\n  got %v", want, got)
	}
}

func TestExpandRoles_PlansAdminExpandsToPlansUser(t *testing.T) {
	got := ExpandRoles([]string{RolePlansAdmin})
	want := []string{RolePlansAdmin, RolePlansUser, RoleUser}
	sort.Strings(want)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ROLE_PLANS_ADMIN should expand to ROLE_PLANS_USER + ROLE_USER\n want %v\n  got %v", want, got)
	}
}

func TestExpandRoles_PlansUserStandalone(t *testing.T) {
	got := ExpandRoles([]string{RolePlansUser})
	want := []string{RolePlansUser, RoleUser}
	sort.Strings(want)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ROLE_PLANS_USER should expand to itself + ROLE_USER\n want %v\n  got %v", want, got)
	}
	if contains(got, RolePlansAdmin) {
		t.Errorf("ROLE_PLANS_USER must NOT expand upward to ROLE_PLANS_ADMIN, got %v", got)
	}
}

func TestExpandRoles_FinanceAdminDoesNotImplyPlans(t *testing.T) {
	got := ExpandRoles([]string{RoleFinanceAdmin})
	if contains(got, RolePlansAdmin) || contains(got, RolePlansUser) {
		t.Errorf("ROLE_FINANCE_ADMIN must NOT expand to any plans roles, got %v", got)
	}
}

func TestExpandRoles_CostAdminExpandsToCostUser(t *testing.T) {
	got := ExpandRoles([]string{RoleCostAdmin})
	want := []string{RoleCostAdmin, RoleCostUser, RoleUser}
	sort.Strings(want)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ROLE_COST_ADMIN should expand to ROLE_COST_USER + ROLE_USER\n want %v\n  got %v", want, got)
	}
}

func TestExpandRoles_FinanceAdminDoesNotImplyCost(t *testing.T) {
	got := ExpandRoles([]string{RoleFinanceAdmin})
	if contains(got, RoleCostAdmin) || contains(got, RoleCostUser) {
		t.Errorf("ROLE_FINANCE_ADMIN must NOT expand to any cost roles, got %v", got)
	}
	if !contains(got, RoleFinanceAdmin) || !contains(got, RoleUser) {
		t.Errorf("ROLE_FINANCE_ADMIN must contain itself and ROLE_USER, got %v", got)
	}
}

func TestExpandRoles_DeduplicatesDuplicates(t *testing.T) {
	got := ExpandRoles([]string{RoleAdmin, RoleAdmin, RoleCostUser, RoleAdmin})
	seen := map[string]int{}
	for _, r := range got {
		seen[r]++
	}
	for r, n := range seen {
		if n > 1 {
			t.Errorf("ExpandRoles must dedup; role %q appears %d times in %v", r, n, got)
		}
	}
}

func TestExpandRoles_UnknownRolePassThrough(t *testing.T) {
	got := ExpandRoles([]string{"ROLE_FROM_OUTSIDE"})
	if !contains(got, "ROLE_FROM_OUTSIDE") {
		t.Errorf("Unknown role must pass through unchanged, got %v", got)
	}
	if !contains(got, RoleUser) {
		t.Errorf("ROLE_USER still must be added, got %v", got)
	}
}

func TestExpandRoles_OutputIsSorted(t *testing.T) {
	// Сортировка — стабильный контракт: позволяет сравнивать наборы ролей
	// побайтово (например, в кэше) и упрощает диффы в логах.
	got := ExpandRoles([]string{RoleFinanceAdmin, RoleAdmin})
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("ExpandRoles output must be sorted, got %v", got)
			break
		}
	}
}

func TestHasRole_RespectsHierarchy(t *testing.T) {
	admin := &User{Roles: []string{RoleAdmin}}
	if !HasRole(admin, RoleCostUser) {
		t.Errorf("ROLE_ADMIN must have ROLE_COST_USER (via hierarchy)")
	}
	if !HasRole(admin, RoleUser) {
		t.Errorf("ROLE_ADMIN must have ROLE_USER")
	}

	cost := &User{Roles: []string{RoleCostUser}}
	if HasRole(cost, RoleAdmin) {
		t.Errorf("ROLE_COST_USER must NOT have ROLE_ADMIN")
	}
	if HasRole(cost, RoleCostAdmin) {
		t.Errorf("ROLE_COST_USER must NOT have ROLE_COST_ADMIN (hierarchy goes top-down)")
	}
}

func TestHasRole_NilUserIsFalse(t *testing.T) {
	if HasRole(nil, RoleAdmin) {
		t.Errorf("HasRole(nil, _) must be false")
	}
}

func TestFilterAllowed_RejectsRoleUserAndUnknown(t *testing.T) {
	in := []string{RoleUser, RoleAdmin, "ROLE_BOGUS", RoleCostUser}
	got := FilterAllowed(in)
	if contains(got, RoleUser) {
		t.Errorf("FilterAllowed must reject ROLE_USER (it is implicit), got %v", got)
	}
	if contains(got, "ROLE_BOGUS") {
		t.Errorf("FilterAllowed must reject unknown roles, got %v", got)
	}
	if !contains(got, RoleAdmin) || !contains(got, RoleCostUser) {
		t.Errorf("FilterAllowed must keep ROLE_ADMIN and ROLE_COST_USER, got %v", got)
	}
}

func TestFilterAllowed_KeepsPlansRoles(t *testing.T) {
	got := FilterAllowed([]string{RolePlansAdmin, RolePlansUser})
	if !contains(got, RolePlansAdmin) || !contains(got, RolePlansUser) {
		t.Errorf("FilterAllowed must keep ROLE_PLANS_ADMIN and ROLE_PLANS_USER, got %v", got)
	}
}

func TestFilterAllowed_DeduplicatesInput(t *testing.T) {
	got := FilterAllowed([]string{RoleAdmin, RoleAdmin, RoleAdmin})
	if len(got) != 1 || got[0] != RoleAdmin {
		t.Errorf("FilterAllowed must dedup, got %v", got)
	}
}

func TestFilterAllowed_EmptyInputEmptyOutput(t *testing.T) {
	got := FilterAllowed(nil)
	if len(got) != 0 {
		t.Errorf("FilterAllowed(nil) must be empty, got %v", got)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
