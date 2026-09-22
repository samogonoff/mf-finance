package plans

import "testing"

// V-05: несошедшаяся контрольная сверка (§7) отмечается всегда, а блокирует ли
// она отправку — решает флаг (по умолчанию НЕТ, решение 08.09.2026). Пропущенная
// сверка (источник недоступен) не отмечается вовсе: это отсутствие связи, а не
// расхождение данных.
func TestReconciliationIssues_BlocksOnMismatch(t *testing.T) {
	recs := []RetailReconciliation{
		{Code: "R-01", Title: "Свод тактики = сумма строк формы", Left: 100, Right: 100, Diff: 0, OK: true},
		{Code: "R-02", Title: "Свод факта по ЦФО = итог источника", Left: 100, Right: 90, Diff: 10, OK: false},
		{Code: "R-03", Title: "Свод = данные мастер-представления", Skipped: true, Note: "источник недоступен"},
	}
	issues := ReconciliationIssues(recs, true)
	if len(issues) != 1 {
		t.Fatalf("ожидалось одно блокирующее замечание, получено %d: %+v", len(issues), issues)
	}
	if issues[0].Code != "V-05" || !issues[0].Blocking {
		t.Fatalf("сверка должна давать блокирующее V-05: %+v", issues[0])
	}
	if issues[0].Value != 10 {
		t.Fatalf("в замечании должно быть расхождение 10, получено %.2f", issues[0].Value)
	}

	// По умолчанию то же расхождение отправку НЕ блокирует, но остаётся видимым:
	// на стенде сверки расходились из-за данных факта, и запрет останавливал
	// работу над формой, пока причину ищут.
	soft := ReconciliationIssues(recs, false)
	if len(soft) != 1 || soft[0].Blocking {
		t.Fatalf("без флага сверка не должна блокировать: %+v", soft)
	}
	if soft[0].Value != 10 {
		t.Fatalf("расхождение должно сохраняться в предупреждении: %.2f", soft[0].Value)
	}
}

// Все сверки нулевые → замечаний нет, форму можно отправлять.
func TestReconciliationIssues_AllZeroPasses(t *testing.T) {
	recs := []RetailReconciliation{
		{Code: "R-01", OK: true},
		{Code: "R-02", OK: true},
		{Code: "R-04", Skipped: true},
	}
	if got := ReconciliationIssues(recs, true); len(got) != 0 {
		t.Fatalf("при нулевых сверках замечаний быть не должно: %+v", got)
	}
}
