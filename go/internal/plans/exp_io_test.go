package plans

import (
	"context"
	"testing"
)

// Экспорт → импорт того же файла ничего не меняет: значения совпадают, и в
// отчёте нет изменений. Это главная проверка формата — если экспорт пишет не то,
// что понимает импорт, круг не сходится.
func TestExpExportImport_RoundTrip(t *testing.T) {
	repo := &fakeExpRepo{
		inst: ExpInstance{ID: 1, FormKind: ExpKindRetail, Country: "BY", PlanYear: 2026, Currency: "BYN"},
		rows: []ExpRow{{
			CodeCFO: 101, CodePL: 24, CFO: "Магазин", Expense: "Заработная плата",
			GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ",
			Values: []ExpValue{
				{Scenario: ExpScenarioPlan, Month: 1, Amount: pv(1000)},
				{Scenario: ExpScenarioPlan, Month: 2, Amount: pv(0)}, // явный ноль
			},
		}},
	}
	svc := &ExpService{repo: repo, cards: nil}

	// Экспорт умеет работать без карточки: собираем файл прямо из строк.
	header := []string{"code_cfo", "cfo", "code_pl", "expense", "group_pl", "group_cfo2", "legal_entity",
		"2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06",
		"2026-07", "2026-08", "2026-09", "2026-10", "2026-11", "2026-12"}
	line := []string{"101", "Магазин", "24", "Заработная плата", "ВЫПЛАТЫ РАБОТНИКАМ", "", "",
		"1000.00", "0.00", "", "", "", "", "", "", "", "", "", ""}
	buf, err := writeXlsx("Расходы ЦП", header, [][]string{line})
	if err != nil {
		t.Fatal(err)
	}

	idx, _, err := svc.rowIndex(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if idx[[2]int{101, 24}][1] == nil || *idx[[2]int{101, 24}][1] != 1000 {
		t.Fatalf("индекс строк собран неверно: %+v", idx)
	}

	// Разбор файла: ключ читается по «ЦФО + статья», месяцы — по шапке.
	gotHeader, table, err := readXlsx(buf)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := expMonthColumns(gotHeader, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 12 {
		t.Fatalf("колонок месяцев %d, ожидали 12", len(cols))
	}
	key, err := expRowKey(table[0])
	if err != nil {
		t.Fatal(err)
	}
	if key.CodeCFO != 101 || key.CodePL != 24 {
		t.Fatalf("ключ строки %d/%d, ожидали 101/24", key.CodeCFO, key.CodePL)
	}
}

// Ключ импорта — «ЦФО + статья», а не номер строки: файл правят руками, строки
// в нём переставляют.
func TestExpRowKey(t *testing.T) {
	if _, err := expRowKey([]string{"101"}); err == nil {
		t.Fatal("короткая строка не даёт ключа")
	}
	if _, err := expRowKey([]string{"101", "Магазин", "статья"}); err == nil {
		t.Fatal("нечисловой код PL должен отклоняться")
	}

	// Ответ И-8: в листе МП_UZ колонки CodeCFO нет — ключом становится
	// наименование ЦФО. Отказываться от строки нельзя, иначе узбекская карточка
	// не импортируется вовсе.
	key, err := expRowKey([]string{"Uzum", "", "48"})
	if err != nil {
		t.Fatalf("строка без кода ЦФО должна сопоставляться по наименованию: %v", err)
	}
	if key.CodeCFO != 0 || key.Name != "Uzum" || key.CodePL != 48 {
		t.Fatalf("ключ по наименованию собран неверно: %+v", key)
	}

	if _, err := expRowKey([]string{"", "", "48"}); err == nil {
		t.Fatal("без кода и без наименования строку сопоставить не с чем")
	}
}

// Шапка без колонок месяцев планового года — файл не от этой формы.
func TestExpMonthColumns_WrongYear(t *testing.T) {
	if _, err := expMonthColumns([]string{"code_cfo", "2025-01"}, 2026); err == nil {
		t.Fatal("колонки чужого года приниматься не должны")
	}
}

// Пустая ячейка экспорта остаётся пустой: подстановка нулей при обратном импорте
// заполнила бы форму нулями, и E-11 перестал бы что-либо ловить.
func TestExpCellText_EmptyStaysEmpty(t *testing.T) {
	r := ExpRow{Values: []ExpValue{
		{Scenario: ExpScenarioPlan, Month: 1, Amount: pv(0)},
		{Scenario: ExpScenarioPlan, Month: 2, Amount: nil},
	}}
	if got := expCellText(r, 1); got != "0.00" {
		t.Fatalf("явный ноль обязан выгружаться нулём, получили %q", got)
	}
	if got := expCellText(r, 2); got != "" {
		t.Fatalf("пустая ячейка обязана остаться пустой, получили %q", got)
	}
	if got := expCellText(r, 3); got != "" {
		t.Fatalf("отсутствующий месяц — пусто, получили %q", got)
	}
}
