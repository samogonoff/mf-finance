package plans

import (
	"strings"
	"testing"
)

func TestLeReadiness_BlockersAndWarnings(t *testing.T) {
	cards := []Card{
		{ID: 1, Title: "Розница РБ", FormCode: TemplateRetail, Status: CardApproved},
		{ID: 2, Title: "Розница РФ", FormCode: TemplateRetail, Status: CardOnApproval, StepCode: "1.2"},
		{ID: 3, Title: "Крупные МП", FormCode: TemplateMP, Status: CardPublished},
		{ID: 4, Title: "Расходы ЦП · Розница РБ", FormCode: TemplateExp, ScopeKey: "R-BY", Status: CardDraft, StepCode: "1.1"},
		{ID: 5, Title: "Форма 31", FormCode: TemplateExp, ScopeKey: "CFO-31-BY", Status: CardDraft, StepCode: "1.1"}, // скрытые формы отделов не считаются
		{ID: 6, Title: "Опт", FormCode: TemplateExp, ScopeKey: "OPT", Status: CardPublishFailed},
	}
	svod := buildLeSvod(leSvodInput{
		Amounts: []leAmount{
			{Legal: leNoEntity, Key: "exp:Аренда", Label: "Аренда", BYN: 70},
			{Legal: "F", Key: "sales:Розница РБ", Label: "Розница РБ", BYN: 1000},
		},
		Warnings: []string{"Нет курса KZT"},
	})
	issues := leReadiness(cards, svod)
	byCode := map[string]LeIssue{}
	for _, i := range issues {
		byCode[i.Code] = i
	}
	if i, ok := byCode["sales_not_approved"]; !ok || i.Level != LeIssueBlock || !strings.Contains(i.Message, "Розница РФ") || strings.Contains(i.Message, "Розница РБ —") {
		t.Errorf("неутверждённые продажи: %+v", i)
	}
	if i, ok := byCode["expenses_not_approved"]; !ok || i.Level != LeIssueBlock || !strings.Contains(i.Message, "Расходы ЦП · Розница РБ") || strings.Contains(i.Message, "Форма 31") {
		t.Errorf("неутверждённые расходы: %+v", i)
	}
	for _, code := range []string{"publish_failed", "svod_warning", "no_legal_entity"} {
		if i, ok := byCode[code]; !ok || i.Level != LeIssueWarn {
			t.Errorf("ждали предупреждение %s: %+v", code, i)
		}
	}
	if _, ok := byCode["no_sales"]; ok {
		t.Error("продажи в своде есть — предупреждения no_sales быть не должно")
	}
	if got := len(leBlockers(issues)); got != 2 {
		t.Errorf("блокирующих %d, ждали 2", got)
	}

	// Архивная карточка (форму решили не утверждать и убрали из периода) этап 1.6 не блокирует.
	arch := leReadiness([]Card{
		{Title: "Розница РБ", FormCode: TemplateRetail, Status: CardApproved},
		{Title: "Расходы ЦП · Интернет-магазин РБ", FormCode: TemplateExp, ScopeKey: "IM-BY", Status: CardArchived},
	}, LeSvod{})
	if len(leBlockers(arch)) != 0 {
		t.Errorf("архивная карточка не должна блокировать: %+v", arch)
	}

	// Все формы утверждены, продаж нет — только предупреждение.
	ok := leReadiness([]Card{{Title: "Розница РБ", FormCode: TemplateRetail, Status: CardApproved}}, LeSvod{})
	if len(leBlockers(ok)) != 0 {
		t.Errorf("все формы утверждены: блокировок быть не должно: %+v", ok)
	}
	found := false
	for _, i := range ok {
		found = found || i.Code == "no_sales"
	}
	if !found {
		t.Error("пустой свод — предупреждение no_sales")
	}
}

// Список названий в сообщении ограничен: «и ещё N».
func TestLeListTitles(t *testing.T) {
	got := leListTitles([]string{"a", "b", "c", "d", "e", "f"}, 4)
	if got != "a; b; c; d и ещё 2" {
		t.Errorf("leListTitles: %q", got)
	}
	if leListTitles([]string{"a"}, 4) != "a" {
		t.Error("короткий список без хвоста")
	}
}
