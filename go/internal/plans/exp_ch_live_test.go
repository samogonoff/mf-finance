package plans

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"
)

// Живая проверка чтения затрат из ClickHouse (finance.exp_costs) и подстановки бюджета на реальном снимке.
// Включается PLANS_TEST_CH_URL (например, http://localhost:18123 у локального finance-ch-test), PLANS_TEST_CH_USER и
// PLANS_TEST_CH_PASSWORD; без них тест пропускается. Снимок заливается: MODE=expcosts go run ./cmd/findebt-etl.
func TestZZLiveExpCH_FormsAndBudgetPrefill(t *testing.T) {
	url := os.Getenv("PLANS_TEST_CH_URL")
	if url == "" {
		t.Skip("PLANS_TEST_CH_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	src := NewExpCHSource(expMockSource{}, url, os.Getenv("PLANS_TEST_CH_USER"), os.Getenv("PLANS_TEST_CH_PASSWORD"), nil)
	if src == nil {
		t.Fatal("источник ClickHouse не создан")
	}

	type form struct {
		name, kind, country string
	}
	forms := []form{
		{"розница BY", ExpKindRetail, "BY"},
		{"ИМ BY", ExpKindIM, "BY"},
		{"опт (РБ, РФ, УЗ)", ExpKindOpt, ""},
	}
	const year, month = 2026, 10
	for _, f := range forms {
		cells, err := src.Costs(ctx, f.kind, f.country, []int{year - 1, year})
		if err != nil {
			t.Fatalf("%s: чтение из ClickHouse: %v", f.name, err)
		}
		if len(cells) == 0 {
			t.Errorf("%s: снимок пуст", f.name)
			continue
		}
		for _, c := range cells {
			if expIsSalesGroupPL(c.GroupPL) {
				t.Fatalf("%s: в форму попала строка продаж/себестоимости/маржи: код %d, группа %q", f.name, c.CodePL, c.GroupPL)
			}
		}
		byScenario := map[string]int{}
		cfos := map[int]bool{}
		countries := map[string]bool{}
		for _, c := range cells {
			byScenario[c.Scenario]++
			cfos[c.CodeCFO] = true
			countries[c.Country] = true
		}
		var ids []int
		for id := range cfos {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		t.Logf("%s: строк %d, сценарии %v, ЦФО %d (первые %v), страны %v", f.name, len(cells), byScenario, len(ids), head(ids, 6), countries)
		if f.kind == ExpKindOpt {
			for _, want := range []int{35, 254, 303, 414} {
				if !cfos[want] {
					t.Errorf("опт: в снимке нет ЦФО %d", want)
				}
			}
			for _, c := range []string{"BY", "RU", "UZ"} {
				if !countries[c] {
					t.Errorf("опт: нет страны %s", c)
				}
			}
		}

		// Подстановка бюджета на реальных данных: тот же код, что в форме, пишем в память.
		repo := &recordingExpRepo{fakeExpRepo: fakeExpRepo{inst: ExpInstance{ID: 1, FormKind: f.kind, Country: f.country, PlanYear: year, Currency: "BYN"}}}
		svc := NewExpService(repo, nil, src)
		card := Card{ID: 1, FormCode: TemplateExp, Status: CardDraft, Year: year, Month: month}
		if _, err := svc.syncFromSource(ctx, card, repo.inst, true); err != nil {
			t.Fatalf("%s: синхронизация: %v", f.name, err)
		}
		months := map[int]int{}
		var sum float64
		for _, c := range repo.budget {
			months[c.Month]++
			sum += *c.Amount
			if c.Month < month {
				t.Errorf("%s: бюджет подставлен в прошедший месяц %d", f.name, c.Month)
			}
		}
		t.Logf("%s: подставлено бюджетных ячеек %d (по месяцам %v), сумма %.0f", f.name, len(repo.budget), months, sum)
		if len(repo.budget) == 0 {
			t.Errorf("%s: бюджет из источника не подставился ни в одну ячейку", f.name)
		}
	}
}

func head(a []int, n int) []int {
	if len(a) > n {
		return a[:n]
	}
	return a
}
