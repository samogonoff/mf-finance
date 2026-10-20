package plans

import "testing"

// Формы отделов скрыты флагом: реестр и списки их не отдают, но разбор ключей и
// route-валидация (formDef) продолжают их знать — скрытие обратимо.
func TestCfoDeptFormsHiddenByDefault(t *testing.T) {
	defer SetCfoDeptFormsEnabled(false)

	count := func(defs []FormDef) (dept, other int) {
		for _, f := range defs {
			for _, sc := range f.Scopes {
				if isCfoDeptScope(f.Code, sc.Key) {
					dept++
				} else {
					other++
				}
			}
		}
		return
	}

	SetCfoDeptFormsEnabled(false)
	dept, other := count(FormRegistry())
	if dept != 0 {
		t.Fatalf("формы отделов должны быть скрыты, видно %d", dept)
	}
	if other == 0 {
		t.Fatal("розница, МП и расходы R-/MP- должны остаться")
	}
	if !cfoDeptHidden(TemplateExp, "CFO-32-BY") || cfoDeptHidden(TemplateExp, "R-BY") || cfoDeptHidden(TemplateRetail, "BY") {
		t.Fatal("скрываются только CFO-* формы расходов")
	}
	if _, ok := formScope(TemplateExp, "CFO-32-BY"); !ok {
		t.Fatal("скрытая область должна по-прежнему находиться по ключу")
	}

	SetCfoDeptFormsEnabled(true)
	if dept, _ := count(FormRegistry()); dept != 6 {
		t.Fatalf("с флагом должны вернуться 6 форм отделов, получили %d", dept)
	}
}