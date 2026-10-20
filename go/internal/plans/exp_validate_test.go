package plans

import (
	"strings"
	"testing"
)

func pv(v float64) *float64 { return &v }

// Строка с полностью заполненным годом.
func fullRow(cfo, pl int, group, expense string, amount float64) ExpRow {
	r := ExpRow{CodeCFO: cfo, CodePL: pl, GroupPL: group, Expense: expense}
	for m := 1; m <= 12; m++ {
		r.Values = append(r.Values, ExpValue{Scenario: ExpScenarioPlan, Month: m, Amount: pv(amount)})
	}
	return r
}

func hasCode(issues []ExpIssue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// E-11: заполнены ВСЕ плановые месяцы, и пусто не равно нулю. Явный ноль —
// заполненная ячейка, пропуск — незаполненная.
func TestValidateExpForm_E11_EmptyIsNotZero(t *testing.T) {
	full := fullRow(101, 24, "ВЫПЛАТЫ РАБОТНИКАМ", "Заработная плата", 1000)
	if got := ValidateExpForm(ExpValidationInput{Rows: []ExpRow{full}}); hasCode(got, "E-11") {
		t.Fatalf("полностью заполненная строка не должна давать E-11: %+v", got)
	}

	// Явные нули — тоже заполнено.
	zeros := fullRow(101, 24, "ВЫПЛАТЫ РАБОТНИКАМ", "Заработная плата", 0)
	if got := ValidateExpForm(ExpValidationInput{Rows: []ExpRow{zeros}}); hasCode(got, "E-11") {
		t.Fatal("явный ноль — это заполненная ячейка, E-11 срабатывать не должен")
	}

	// Пропущенный месяц — срабатывает.
	partial := full
	partial.Values = full.Values[:11]
	if got := ValidateExpForm(ExpValidationInput{Rows: []ExpRow{partial}}); !hasCode(got, "E-11") {
		t.Fatal("пропущенный месяц обязан давать E-11")
	}
}

// E-12 после ответа И-2: минус допустим В ЛЮБОЙ статье, но требует комментария.
// Белого списка групп больше нет — аналитик нашёл отрицательные суммы в семи
// группах из восьми, включая те, где прежняя реализация их блокировала.
func TestValidateExpForm_E12_NegativeIsWarningEverywhere(t *testing.T) {
	// Коды, на которых прежняя версия блокировала законный ввод: 10, 54, 66, 99, 100.
	for _, c := range []struct{ group, expense string }{
		{"ОСНОВНЫЕ РАСХОДЫ", "Коммунальные услуги"},
		{"РЕКЛАМА И МАРКЕТИНГ", "Трейд. Реклама в ТРЦ"},
		{"АРЕНДА", "Аренда помещений (стоянки)"},
		{"ПРОЧИЕ РАСХОДЫ", "Курсовые разницы, переоценка валюта"},
	} {
		got := ValidateExpForm(ExpValidationInput{Rows: []ExpRow{fullRow(101, 54, c.group, c.expense, -500)}})
		var found *ExpIssue
		for i := range got {
			if got[i].Code == "E-12" {
				found = &got[i]
			}
		}
		if found == nil {
			t.Fatalf("минус в группе %q обязан давать предупреждение", c.group)
		}
		if found.Blocking {
			t.Fatalf("E-12 больше не блокирует (ответ И-2): %+v", *found)
		}
	}

	// Одно предупреждение на строку, а не двенадцать: минус во всех месяцах —
	// одна ситуация.
	got := ValidateExpForm(ExpValidationInput{Rows: []ExpRow{fullRow(101, 54, "АРЕНДА", "Аренда", -500)}})
	n := 0
	for _, i := range got {
		if i.Code == "E-12" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("предупреждений E-12 по строке %d, ожидали одно", n)
	}

	// Комментарий к строке меняет формулировку — он и есть то, чего требует ТЗ.
	withComment := ValidateExpForm(ExpValidationInput{
		Rows:         []ExpRow{fullRow(101, 54, "АРЕНДА", "Аренда", -500)},
		CommentByRow: map[[2]int]string{{101, 54}: "корректировка по акту"},
	})
	for _, i := range withComment {
		if i.Code == "E-12" && !strings.Contains(i.Message, "комментарий дан") {
			t.Fatalf("с комментарием формулировка должна меняться: %q", i.Message)
		}
	}
}

// E-13: все строки формы отнесены на одно юридическое лицо страны.
func TestValidateExpForm_E13_SingleLegalEntity(t *testing.T) {
	own := fullRow(101, 24, "ВЫПЛАТЫ РАБОТНИКАМ", "Заработная плата", 100)
	own.LegalEntity = "F"
	alien := fullRow(102, 24, "ВЫПЛАТЫ РАБОТНИКАМ", "Заработная плата", 100)
	alien.LegalEntity = "TDMF"

	got := ValidateExpForm(ExpValidationInput{Rows: []ExpRow{own, alien}, LegalEntity: "F"})
	if !hasCode(got, "E-13") {
		t.Fatal("чужое ЮЛ в форме обязано блокировать")
	}
	for _, i := range got {
		if i.Code == "E-13" && i.CodeCFO != 102 {
			t.Fatalf("E-13 должен указывать на строку 102: %+v", i)
		}
	}
}

// E-15: валюта ввода равна национальной валюте страны формы.
func TestValidateExpForm_E15_Currency(t *testing.T) {
	in := ExpValidationInput{
		Instance: ExpInstance{Currency: "RUB"}, Currency: "BYN",
		Rows: []ExpRow{fullRow(101, 24, "ВЫПЛАТЫ РАБОТНИКАМ", "Зарплата", 1)},
	}
	if got := ValidateExpForm(in); !hasCode(got, "E-15") {
		t.Fatal("несовпадение валюты обязано блокировать")
	}
}

// E-17 после ответа И-10: расхождение наименования — предупреждение, а вот
// отсутствие кода в справочнике целиком блокирует: строку не на что отнести.
// Появление НОВОГО кода PL при этом останавливать отправку не должно только в
// части второго уровня — он уводит код в «Не распределено» и публикацию не держит.
func TestValidateExpForm_E17_MissingCodeBlocksNameMismatchDoesNot(t *testing.T) {
	dir := map[int]string{24: "Заработная плата"}

	mismatch := ValidateExpForm(ExpValidationInput{
		Rows:            []ExpRow{fullRow(101, 24, "ВЫПЛАТЫ РАБОТНИКАМ", "Зарплата", 100)},
		ExpenseByCodePL: dir,
	})
	for _, i := range mismatch {
		if i.Code == "E-17" && i.Blocking {
			t.Fatal("расхождение наименования по ответу И-10 не блокирует")
		}
	}
	if !hasCode(mismatch, "E-17") {
		t.Fatal("расхождение наименования обязано быть видно")
	}

	unknown := ValidateExpForm(ExpValidationInput{
		Rows:            []ExpRow{fullRow(101, 777, "ПРОЧИЕ РАСХОДЫ", "Неизвестная", 100)},
		ExpenseByCodePL: dir,
	})
	blocked := false
	for _, i := range unknown {
		if i.Code == "E-17" && i.Blocking {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("кода, которого нет в справочнике, быть не должно — это блокирует")
	}

	// Справочник не залит — контроль молчит целиком, иначе форма не откроется
	// на чистой базе.
	empty := ValidateExpForm(ExpValidationInput{
		Rows: []ExpRow{fullRow(101, 777, "ПРОЧИЕ РАСХОДЫ", "Неизвестная", 100)},
	})
	if hasCode(empty, "E-17") {
		t.Fatal("без справочника E-17 срабатывать не должен")
	}
}

// E-18: отклонение больше 30 % — предупреждение с требованием комментария;
// комментарий его не снимает, но меняет формулировку и снимает из «незакрытых».
func TestValidateExpForm_E18_Deviation(t *testing.T) {
	r := fullRow(101, 61, "ОСНОВНЫЕ РАСХОДЫ", "Коммуналка", 100) // итог 1200
	key := [2]int{101, 61}

	in := ExpValidationInput{Rows: []ExpRow{r}, PrevYearByRow: map[[2]int]float64{key: 800}}
	got := ValidateExpForm(in)
	if !hasCode(got, "E-18") {
		t.Fatal("отклонение 50 % обязано давать предупреждение")
	}
	if n := ExpUnclosedComments(got, nil); n != 1 {
		t.Fatalf("незакрытых комментариев %d, ожидали 1", n)
	}

	in.CommentByRow = map[[2]int]string{key: "открыли второй магазин"}
	got = ValidateExpForm(in)
	if n := ExpUnclosedComments(got, in.CommentByRow); n != 0 {
		t.Fatalf("с комментарием незакрытых быть не должно, получили %d", n)
	}

	// Отклонение в пределах порога предупреждения не даёт.
	in.PrevYearByRow[key] = 1100
	if got := ValidateExpForm(in); hasCode(got, "E-18") {
		t.Fatal("отклонение меньше 30 % предупреждением не является")
	}
}

// Контроли E-01…E-08: несошедшийся обязан нести расшифровку — ТЗ §8 говорит
// «контроль без расшифровки не принимается».
func TestExpReconcile_DetailsRequired(t *testing.T) {
	rows := []ExpRow{
		fullRow(101, 24, "ВЫПЛАТЫ РАБОТНИКАМ", "Заработная плата", 100), // итог 1200
		fullRow(102, 24, "ВЫПЛАТЫ РАБОТНИКАМ", "Заработная плата", 200), // итог 2400
	}
	got := ExpReconcile(ExpReconInput{
		Rows: rows,
		// Источник говорит про 101 другое число — контроль E-02 обязан упасть.
		SourceByCFO:    map[int]float64{101: 999, 102: 2400},
		SourceCFOCount: 2,
	})
	byCode := map[string]ExpRecon{}
	for _, r := range got {
		byCode[r.Code] = r
	}
	e02 := byCode["E-02"]
	if e02.OK {
		t.Fatal("E-02 обязан не сойтись")
	}
	if len(e02.Details) == 0 {
		t.Fatal("несошедшийся контроль без расшифровки по ТЗ §8 не принимается")
	}
	if e02.Details[0].CodeCFO != 101 {
		t.Fatalf("расшифровка должна указывать на ЦФО 101: %+v", e02.Details)
	}
	if !byCode["E-03"].OK {
		t.Fatalf("сумма групп обязана сходиться с итогом формы: %+v", byCode["E-03"])
	}
	if !byCode["E-07"].OK {
		t.Fatalf("число ЦФО совпадает — контроль обязан сойтись: %+v", byCode["E-07"])
	}
	// Реестров нет — контроли пропущены, а не «сошлись».
	for _, code := range []string{"E-04", "E-05", "E-06"} {
		if !byCode[code].Skipped {
			t.Fatalf("%s без реестра обязан быть пропущен, а не сойтись: %+v", code, byCode[code])
		}
	}
}

// Пропущенный контроль не считается расхождением: недоступный источник это
// «не проверено», а не «не сходится».
func TestExpReconBlocking_SkippedIsNotADiff(t *testing.T) {
	rs := []ExpRecon{
		{Code: "E-02", OK: false, Skipped: true, Diff: 0},
		{Code: "E-03", OK: false, Diff: -12.5},
	}
	blocking := ExpReconBlocking(rs)
	if len(blocking) != 1 {
		t.Fatalf("расхождений %d, ожидали одно: %v", len(blocking), blocking)
	}
	if got := ExpReconDiffSum(rs); !near(got, 12.5) {
		t.Fatalf("сумма расхождений %v, ожидали 12,5 по модулю", got)
	}
}

// E-04 сверяется со статьёй 24 (ответ 40), E-05 — только билеты и
// командировочные, потому что проживание идёт статьёй 30 (находка Н3).
func TestExpReconcile_RegistryControls(t *testing.T) {
	form := []ExpRow{
		fullRow(101, ExpPLSalary, "ВЫПЛАТЫ РАБОТНИКАМ", "Заработная плата", 100), // 1200
		fullRow(101, ExpPLTravel, "ПРОЧИЕ РАСХОДЫ", "Командировки", 100),         // 1200
	}
	regs := map[string][]ExpRegRow{
		ExpRegPayroll: {{Kind: ExpRegPayroll, Months: twelve(100)}}, // 1200 — сойдётся
		ExpRegTravel: {{Kind: ExpRegTravel, Months: map[int]float64{2: 1930},
			Attrs: map[string]any{"tickets": 600.0, "per_diem": 280.0, "lodging": 1050.0}}},
	}
	byCode := map[string]ExpRecon{}
	for _, r := range ExpReconcile(ExpReconInput{Rows: form, Registries: regs}) {
		byCode[r.Code] = r
	}
	if !byCode["E-04"].OK {
		t.Fatalf("итог реестра ФОТ равен статье 24 — контроль обязан сойтись: %+v", byCode["E-04"])
	}
	// Билеты 600 + командировочные 280 = 880 против статьи 27 = 1200: не сходится,
	// и проживание 1 050 в этот контроль не входит.
	e05 := byCode["E-05"]
	if e05.OK {
		t.Fatal("E-05 обязан поймать расхождение")
	}
	if !near(e05.Left, 880) {
		t.Fatalf("слева %v — ожидали билеты + командировочные = 880 без проживания", e05.Left)
	}
}

// Свод §8: строки складываются по ключу «группа PL + статья + код PL», сценарии
// не смешиваются. Свёртка §12.3 идёт по ТАКТИКЕ — по тому, что заполняет форма.
func TestExpSummaryFold_ByPnlGroup(t *testing.T) {
	rows := []ExpSummaryRow{
		{GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ", CodePL: 24, PnlGroup: "Заработная плата",
			ByScenario: map[string]map[int]float64{
				ExpScenarioPlan: {1: 100, 2: 200},
				ExpScenarioFact: {1: 999}, // факт в свёртку не идёт
			}},
		{GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ", CodePL: 26, PnlGroup: "Заработная плата",
			ByScenario: map[string]map[int]float64{ExpScenarioPlan: {1: 50}}},
		{GroupPL: "ПРОЧИЕ РАСХОДЫ", CodePL: 777, PnlGroup: ExpSummaryUnassigned,
			ByScenario: map[string]map[int]float64{ExpScenarioPlan: {1: 7}}},
	}
	got := ExpFoldToPnlGroups(rows)
	if len(got) != 2 {
		t.Fatalf("групп %d, ожидали две: %+v", len(got), got)
	}
	if got[0].PnlGroup != "Заработная плата" || !near(got[0].Total, 350) {
		t.Fatalf("зарплата свернулась неверно: %+v", got[0])
	}
	if !near(got[0].ByMonth[1], 150) {
		t.Fatalf("январь зарплаты %v, ожидали 150 (100 + 50) без факта", got[0].ByMonth[1])
	}
	if len(got[0].CodesPL) != 2 {
		t.Fatalf("группа обязана раскрываться до кодов PL: %+v", got[0].CodesPL)
	}
	// «Не распределено» — всегда последней и помечена: это остаток, а не
	// подгруппа наравне (ответ 36: ошибкой не считается, публикацию не блокирует).
	last := got[len(got)-1]
	if last.PnlGroup != ExpSummaryUnassigned || !last.Unassigned {
		t.Fatalf("нераспределённые коды должны идти последними и быть помечены: %+v", last)
	}
}

// E-11 с месяцем карточки: обязателен только месяц плана, последующие месяцы — предупреждение, прошедшие не
// проверяются (они закрыты, заполнить их нельзя — «зачем заполнять всё в октябре»).
func TestValidateExpForm_E11_OnlyPlanMonthBlocks(t *testing.T) {
	onlyOct := ExpRow{CodeCFO: 101, CodePL: 61, Expense: "Коммуналка",
		Values: []ExpValue{{Scenario: ExpScenarioPlan, Month: 10, Amount: pv(500)}}}
	got := ValidateExpForm(ExpValidationInput{Rows: []ExpRow{onlyOct}, CardMonth: 10})
	if len(ExpBlocking(got)) != 0 {
		t.Fatalf("заполнен месяц плана — блокировать нечего: %+v", got)
	}
	var warned bool
	for _, i := range got {
		warned = warned || i.Code == "E-11" && !i.Blocking && strings.Contains(i.Message, "после планового: 2")
	}
	if !warned {
		t.Fatalf("ноябрь и декабрь не заполнены — предупреждение: %+v", got)
	}

	empty := ExpRow{CodeCFO: 101, CodePL: 61, Expense: "Коммуналка"}
	got = ValidateExpForm(ExpValidationInput{Rows: []ExpRow{empty}, CardMonth: 10})
	if b := ExpBlocking(got); len(b) != 1 || b[0].Code != "E-11" || b[0].Month != 10 {
		t.Fatalf("месяц плана пуст — блокирует: %+v", got)
	}
	// Явный ноль в месяце плана — заполнено.
	zero := ExpRow{CodeCFO: 101, CodePL: 61, Values: []ExpValue{{Scenario: ExpScenarioPlan, Month: 10, Amount: pv(0)}}}
	if got := ValidateExpForm(ExpValidationInput{Rows: []ExpRow{zero}, CardMonth: 10}); len(ExpBlocking(got)) != 0 {
		t.Fatalf("явный ноль заполнен: %+v", got)
	}
	// Без месяца карточки — прежнее правило.
	if got := ValidateExpForm(ExpValidationInput{Rows: []ExpRow{onlyOct}}); len(ExpBlocking(got)) != 1 {
		t.Fatalf("без месяца карточки обязательны все двенадцать: %+v", got)
	}
}

// E-18 с месяцем карточки сравнивает те же месяцы: один месяц плана против всего прошлого года не даёт ложных отклонений.
func TestValidateExpForm_E18_SameMonthsOnly(t *testing.T) {
	r := ExpRow{CodeCFO: 101, CodePL: 61, Expense: "Коммуналка", Values: []ExpValue{
		{Scenario: ExpScenarioPlan, Month: 10, Amount: pv(100)},
		{Scenario: ExpScenarioFact, Month: 10, Amount: pv(95)}, // октябрь прошлого года
		{Scenario: ExpScenarioFact, Month: 1, Amount: pv(5000)},
		{Scenario: ExpScenarioFact, Month: 2, Amount: pv(5000)},
	}}
	in := ExpValidationInput{Rows: []ExpRow{r}, CardMonth: 10, PrevYearByRow: map[[2]int]float64{{101, 61}: 10095}}
	if got := ValidateExpForm(in); hasCode(got, "E-18") {
		t.Fatalf("октябрь к октябрю (100 к 95) — не отклонение, а весь год в сравнение не идёт: %+v", got)
	}
	r.Values[0].Amount = pv(300) // октябрь 300 против 95
	in.Rows = []ExpRow{r}
	if got := ValidateExpForm(in); !hasCode(got, "E-18") {
		t.Fatal("октябрь 300 против 95 — отклонение обязано дать предупреждение")
	}
}
