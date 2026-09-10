package plans

import (
	"testing"
	"time"
)

func pubDate(y, m int) time.Time { return time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC) }

// Выключенное правило не публикуется, но попадает в «ожидает BI» — это и есть
// перечень открытых вопросов §12, видимый в отчёте dry-run.
func TestApplyPublishMapping_DisabledRuleGoesToPending(t *testing.T) {
	c := Card{FormCode: TemplateRetail, Country: "BY"}
	rows := []PublishRow{{BlockType: "sales_plan", Country: "BY", CodeCFO: 100, Date: pubDate(2026, 7), Value: 1000}}
	mappings := []PublishMapping{{
		FormCode: TemplateRetail, BlockType: "sales_plan", Country: "BY", ParamName: "ПРОДАЖИ",
		CodePL: 1001, TargetTable: "Budgeting.dbo.VFORMTOLOADTAKTTARGET", Enabled: false,
		Note: "ожидает подтверждения BI",
	}}
	out, rep := applyPublishMapping(c, rows, mappings)
	if len(out) != 0 {
		t.Fatalf("выключенное правило не должно публиковаться, получено %d строк", len(out))
	}
	if len(rep.pending) != 1 {
		t.Fatalf("ожидался 1 pending, получено %+v", rep.pending)
	}
}

// Строка формы без правила — в unmapped (её молча не теряем).
func TestApplyPublishMapping_UnmappedReported(t *testing.T) {
	c := Card{FormCode: TemplateMP}
	rows := []PublishRow{{BlockType: "spp", CodeCFO: 335, Date: pubDate(2026, 6), Value: 0.31}}
	out, rep := applyPublishMapping(c, rows, nil)
	if len(out) != 0 || len(rep.unmapped) != 1 || rep.unmapped[0] != "spp" {
		t.Fatalf("ожидался unmapped=[spp], получено out=%d rep=%+v", len(out), rep)
	}
}

// Правило подставляет Параметр/КодPL и приёмник; страна берётся с карточки.
func TestApplyPublishMapping_EnabledRuleMapsRow(t *testing.T) {
	c := Card{FormCode: TemplateRetail, Country: "BY"}
	rows := []PublishRow{{BlockType: "sales_plan", CodeCFO: 100, Date: pubDate(2026, 7), Value: 1234.5}}
	mappings := []PublishMapping{{
		FormCode: TemplateRetail, BlockType: "sales_plan", ParamName: "ПРОДАЖИ", CodePL: 1001,
		TargetTable: "Budgeting.dbo.VFORMTOLOADTAKTTARGET", Enabled: true,
	}}
	out, rep := applyPublishMapping(c, rows, mappings)
	if len(out) != 1 {
		t.Fatalf("ожидалась 1 строка, получено %d (%+v)", len(out), rep)
	}
	got := out[0]
	if got.Param != "ПРОДАЖИ" || got.CodePL != 1001 || got.Country != "BY" ||
		got.TargetTable != "Budgeting.dbo.VFORMTOLOADTAKTTARGET" {
		t.Fatalf("маппинг применён неверно: %+v", got)
	}
}

// Агрегация по группе (250/480) обязана СВЕРНУТЬ площадки в одну строку:
// у приёмника нет PK, две строки с одним логическим ключом = дубль.
func TestApplyPublishMapping_AggregateFoldsRows(t *testing.T) {
	c := Card{FormCode: TemplateMP, Country: "RU"}
	rows := []PublishRow{
		{BlockType: "sales_manager_price", CodeCFO: 335, Country: "RU", Date: pubDate(2026, 6), Value: 100},
		{BlockType: "sales_manager_price", CodeCFO: 336, Country: "RU", Date: pubDate(2026, 6), Value: 50},
		{BlockType: "sales_manager_price", CodeCFO: 337, Country: "RU", Date: pubDate(2026, 7), Value: 20},
	}
	mappings := []PublishMapping{{
		FormCode: TemplateMP, BlockType: "sales_manager_price", ParamName: "ПРОДАЖИ с НДС_СПП",
		TargetTable: "Budgeting.dbo.VFORMTOLOADTAKTTARGET", Aggregate: true, AggregateCFO: 250, Enabled: true,
	}}
	out, _ := applyPublishMapping(c, rows, mappings)
	if len(out) != 2 {
		t.Fatalf("ожидалось 2 строки (июнь+июль после свёртки), получено %d: %+v", len(out), out)
	}
	var june float64
	for _, r := range out {
		if r.CodeCFO != 250 {
			t.Fatalf("агрегат должен писаться на ЦФО 250, получено %d", r.CodeCFO)
		}
		if r.Date.Month() == time.June {
			june = r.Value
		}
	}
	if june != 150 {
		t.Fatalf("июнь должен сложиться в 150, получено %.2f", june)
	}
}

// Правило со страной перебивает общее правило формы.
func TestPickMapping_CountrySpecificWins(t *testing.T) {
	mappings := []PublishMapping{
		{BlockType: "sales_plan", Country: "", ParamName: "ОБЩЕЕ", Enabled: true},
		{BlockType: "sales_plan", Country: "RU", ParamName: "ПО-РОССИИ", Enabled: true},
	}
	m, ok := pickMapping(mappings, "sales_plan", "RU")
	if !ok || m.ParamName != "ПО-РОССИИ" {
		t.Fatalf("ожидалось правило по стране, получено %+v", m)
	}
	m, ok = pickMapping(mappings, "sales_plan", "KZ")
	if !ok || m.ParamName != "ОБЩЕЕ" {
		t.Fatalf("для страны без своего правила ожидалось общее, получено %+v", m)
	}
}

// Белый список таблиц: имя приёмника приходит из данных маппинга, поэтому запись
// в произвольную таблицу должна быть невозможна.
func TestMssqlPublisher_TargetWhitelist(t *testing.T) {
	p := &mssqlPublisher{allowed: map[string]bool{"budgeting.dbo.vformtoloadtakttarget": true}}
	if err := p.checkTarget("Budgeting.dbo.VFORMTOLOADTAKTTARGET"); err != nil {
		t.Fatalf("разрешённая таблица отклонена: %v", err)
	}
	if err := p.checkTarget("Budgeting.dbo.Users"); err == nil {
		t.Fatal("таблица вне белого списка должна быть отклонена")
	}
}

// Ответ 28 (10.09.2026): «публиковать код 66 одной строкой… в приёмник идёт сумма».
// В форме штрафы и прочие удержания — ДВА поля ввода, и обе строки несут CodePL=66.
// Приёмник обязан получить одну строку с суммой, иначе на одном логическом ключе
// окажется два INSERT'а и идемпотентность записи ломается.
func TestFoldPublishRows_PenaltiesAndOtherMergeIntoOne66(t *testing.T) {
	date := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	rows := []PublishRow{
		{BlockType: "penalties", Param: "ЗАТРАТЫ", Country: "RU", CodeCFO: 335, CodePL: 66, Date: date, Value: 1200.50},
		{BlockType: "cost_other", Param: "ЗАТРАТЫ", Country: "RU", CodeCFO: 335, CodePL: 66, Date: date, Value: 800.25},
		// Другая площадка тем же кодом не сливается: ключ включает ЦФО.
		{BlockType: "penalties", Param: "ЗАТРАТЫ", Country: "RU", CodeCFO: 336, CodePL: 66, Date: date, Value: 10},
	}
	got := foldPublishRows(rows)
	if len(got) != 2 {
		t.Fatalf("строк после свёртки %d, ожидали 2 (две площадки): %+v", len(got), got)
	}
	if got[0].CodeCFO != 335 || got[0].Value != 2000.75 {
		t.Fatalf("штрафы и прочие удержания должны сложиться в 2000,75: %+v", got[0])
	}
	if got[1].CodeCFO != 336 || got[1].Value != 10 {
		t.Fatalf("вторая площадка не должна была слиться: %+v", got[1])
	}
}

// Синтетического кода 6600 в приёмнике быть не может: ключ штрафов в индексе
// факта отрицательный, а публикация неположительные коды отбрасывает.
func TestPenaltyFactKey_IsNotAPublishableCode(t *testing.T) {
	if mpPenaltyFactKey > 0 {
		t.Fatalf("ключ штрафов %d выглядит как код PL — он может уехать в приёмник", mpPenaltyFactKey)
	}
}
