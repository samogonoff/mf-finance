package plans

import "testing"

// Регресс, из-за которого этап периода вообще нельзя было закрыть.
//
// Форма расходов ЦП идёт своим маршрутом с ТАКИМИ ЖЕ кодами шагов (1.1 → 1.2 →
// 1.4). Пока этап периода агрегировался из ВСЕХ карточек, добавление формы 1.5
// означало, что этап 1.1 «план продаж» ждёт формы расходов, которые по схеме
// заполняются позже. Аналитик 10.09.2026 уже видел эту картину в другом виде:
// «двигаю этап — ошибка, потому что другие формы пустые?».
func TestStageStatusFromCards_ExpCardsDoNotBlockSalesStage(t *testing.T) {
	stages := []StageState{{Code: "1.1"}, {Code: "1.2"}, {Code: expPeriodStage}}
	cards := []Card{
		// Поток продаж прошёл заполнение и стоит на согласовании.
		{FormCode: TemplateRetail, Title: "Розница РБ", Status: CardOnApproval, StepCode: "1.2"},
		{FormCode: TemplateMP, Title: "Крупные МП", Status: CardOnApproval, StepCode: "1.2"},
		// Расходы ЦП ещё даже не начаты.
		{FormCode: TemplateExp, Title: "Расходы ЦП · Розница РБ", Status: CardDraft, StepCode: "1.1"},
		{FormCode: TemplateExp, Title: "Расходы ЦП · МП РФ", Status: CardDraft, StepCode: "1.1"},
	}
	out := StageStatusFromCards(stages, cards)
	if out[0].Status != "completed" {
		t.Fatalf("этап 1.1 = %q, ожидали completed: формы продаж шаг прошли, а расходы ЦП его не держат", out[0].Status)
	}
	if out[1].Status != "in_progress" {
		t.Fatalf("этап 1.2 = %q, ожидали in_progress", out[1].Status)
	}
	if out[2].Status == "completed" {
		t.Fatal("этап 1.5 не может быть completed: обе формы расходов в черновике")
	}
}

// Этап 1.5 закрывается утверждением своих форм, а не прохождением шага: шага с
// кодом «1.5» в маршруте формы нет.
func TestStageStatusFromCards_ExpStageClosesOnApproval(t *testing.T) {
	stages := []StageState{{Code: expPeriodStage}}
	exp := func(status string) Card {
		return Card{FormCode: TemplateExp, Status: status, StepCode: "1.4"}
	}

	if got := StageStatusFromCards(stages, []Card{exp(CardApproved), exp(CardPublished)})[0].Status; got != "completed" {
		t.Fatalf("все формы утверждены → %q, ожидали completed", got)
	}
	if got := StageStatusFromCards(stages, []Card{exp(CardApproved), exp(CardOnApproval)})[0].Status; got != "in_progress" {
		t.Fatalf("часть форм на согласовании → %q, ожидали in_progress", got)
	}
	if got := StageStatusFromCards(stages, []Card{exp(CardApproved), exp(CardReturned)})[0].Status; got != "returned" {
		t.Fatalf("возврат обязан быть виден на таймлайне → %q, ожидали returned", got)
	}
}

// Разделение форм по этапам: чьи карточки какой этап держат.
func TestFormDrivesStage(t *testing.T) {
	cases := []struct {
		form, stage string
		want        bool
	}{
		{TemplateRetail, "1.1", true},
		{TemplateMP, "1.4", true},
		{TemplateRetail, expPeriodStage, false},
		{TemplateExp, expPeriodStage, true},
		{TemplateExp, "1.1", false},
		{TemplateExp, "1.4", false},
	}
	for _, c := range cases {
		if got := formDrivesStage(c.form, c.stage); got != c.want {
			t.Fatalf("formDrivesStage(%s, %s) = %v, ожидали %v", c.form, c.stage, got, c.want)
		}
	}
}

// Карточки формы расходов ЦП должны существовать на период: без них у задания
// «Затраты ЦП» нет формы, а именно это аналитик и увидел на стенде.
func TestFormRegistry_HasExpForm(t *testing.T) {
	def, ok := formDef(TemplateExp)
	if !ok {
		t.Fatal("форма расходов ЦП не зарегистрирована в реестре форм")
	}
	// Legacy 1.5 has seven scopes; TЗ v2 adds six code-based CFO forms.
	if len(def.Scopes) != 13 {
		t.Fatalf("областей формы %d, ожидали тринадцать: legacy семь и CFO 31/32/33/34/37/962", len(def.Scopes))
	}
	seen := map[string]string{}
	for _, sc := range def.Scopes {
		if sc.Currency == "" || sc.Country == "" {
			t.Fatalf("область %q без страны или валюты ввода: ввод в нац. валюте обязателен (ТЗ §1.3)", sc.Key)
		}
		kind, country := expKindOf(sc.Key)
		if country != sc.Country {
			t.Fatalf("область %q: страна ключа %q не совпадает со страной карточки %q", sc.Key, country, sc.Country)
		}
		seen[sc.Key] = kind
	}
	for _, k := range []string{"R-BY", "R-RU", "R-KZ", "R-UZ"} {
		if seen[k] != ExpKindRetail {
			t.Fatalf("область %q должна быть розницей, получили %q", k, seen[k])
		}
	}
	if seen["R-BY"] != ExpKindRetail || seen["MP-RU"] != ExpKindMP {
		t.Fatalf("вид формы выводится из ключа неверно: %v", seen)
	}
	for _, code := range []CfoExpFormCode{CfoExp31, CfoExp32, CfoExp33, CfoExp34, CfoExp37, CfoExp962} {
		key := "CFO-" + itoa(int64(code)) + "-"
		found := false
		for scope, kind := range seen {
			if len(scope) >= len(key) && scope[:len(key)] == key {
				if kind != CfoExpKind(code) {
					t.Fatalf("область %q должна быть CFO kind %q, получили %q", scope, CfoExpKind(code), kind)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("область CFO %d отсутствует: %v", code, seen)
		}
	}
}
