package plans

import (
	"context"
	"log"
	"strconv"
	"strings"
)

// Состав розницы — ПРАВИЛО, а не список магазинов (правка ТЗ Розница §11).
//
// Формулировка аналитика: розница = группа ЦФО «3.Магазины» без ЦФО 40, плюс
// восемь ЦФО-отделов (36, 102, 203, 205, 304, 305, 412, 435); исключаются 40
// (интернет-магазин) и 722 (техническая запись переезда магазина в Солигорске).
// Ровно это правило применяет ALL_view, поэтому свой список магазинов, каким он
// был раньше (фильтр по одной группе из конфига), давал другой состав — и
// цифры формы не сходились с отчётами.
//
// Правило живёт СТРОКАМИ справочника dir_retail_scope (миграция 0042): добавить
// ЦФО-отдел или исключение можно данными, без релиза. Справочник недоступен →
// работаем на RetailScopeSeed: состав розницы не должен зависеть от того,
// поднялся ли справочник, иначе форма молча покажет не тех магазинов.
//
// Отдельно от состава стоит ОТБОР ПО СТАТУСУ (правка §12): список магазинов
// берётся из справочника по статусу, а не по наличию факта, иначе новые точки
// в план не попадут. Поэтому фильтр статуса здесь — не «есть ли факт», а
// «строка справочника не закрыта».

// Виды правил состава.
const (
	ScopeIncludeGroup = "include_group" // группа ЦФО целиком
	ScopeIncludeCFO   = "include_cfo"   // отдельный ЦФО
	ScopeExcludeCFO   = "exclude_cfo"   // исключение (сильнее включения)
)

// RetailScopeRule — строка правила состава розницы.
type RetailScopeRule struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Note  string `json:"note"`
}

// RetailScopeSeed — правило по ТЗ на случай недоступного справочника.
func RetailScopeSeed() []RetailScopeRule {
	rules := []RetailScopeRule{
		{Kind: ScopeIncludeGroup, Value: "3.Магазины", Note: "правило ALL_view: группа ЦФО магазинов"},
	}
	for _, cfo := range []int{36, 102, 203, 205, 304, 305, 412, 435} {
		rules = append(rules, RetailScopeRule{
			Kind: ScopeIncludeCFO, Value: strconv.Itoa(cfo), Note: "ЦФО-отдел розницы (ALL_view)",
		})
	}
	return append(rules,
		RetailScopeRule{Kind: ScopeExcludeCFO, Value: "40", Note: "интернет-магазин — не розница"},
		RetailScopeRule{Kind: ScopeExcludeCFO, Value: "722", Note: "техническая запись переезда магазина в Солигорске"},
	)
}

// RetailScope — разобранное правило: что включаем и что исключаем.
type RetailScope struct {
	groups     map[string]bool
	includeCFO map[int]bool
	excludeCFO map[int]bool
}

// NewRetailScope — правило из строк справочника.
func NewRetailScope(rules []RetailScopeRule) *RetailScope {
	sc := &RetailScope{
		groups:     map[string]bool{},
		includeCFO: map[int]bool{},
		excludeCFO: map[int]bool{},
	}
	for _, r := range rules {
		v := strings.TrimSpace(r.Value)
		switch r.Kind {
		case ScopeIncludeGroup:
			sc.groups[retailGroupKey(v)] = true
		case ScopeIncludeCFO:
			if n, err := strconv.Atoi(v); err == nil {
				sc.includeCFO[n] = true
			}
		case ScopeExcludeCFO:
			if n, err := strconv.Atoi(v); err == nil {
				sc.excludeCFO[n] = true
			}
		}
	}
	return sc
}

// retailFormStores — магазины, из которых строятся строки формы: без
// строк-итогов источника и вне правила состава розницы (ЦФО 40, 722).
func retailFormStores(stores []RetailStore, scope *RetailScope) []RetailStore {
	out := make([]RetailStore, 0, len(stores))
	for _, st := range stores {
		if isRetailTotalRow(st) || !scope.Includes(st.CodeCFO, st.GroupCFO1) {
			continue
		}
		out = append(out, st)
	}
	if len(out) > 0 || len(stores) == 0 {
		return out
	}
	// Правило не совпало ни с одним магазином — значит, разошлись названия
	// групп в справочнике и источнике. Пустая форма хуже: оставляем всё, кроме
	// явных исключений, и пишем в лог — это надо чинить в справочнике.
	log.Printf("plans retail: правило состава розницы не совпало ни с одним из %d магазинов — применены только исключения ЦФО", len(stores))
	for _, st := range stores {
		if isRetailTotalRow(st) || (scope != nil && scope.excludeCFO[st.CodeCFO]) {
			continue
		}
		out = append(out, st)
	}
	return out
}

// retailGroupKey — сравнение групп ЦФО без завязки на префикс-номер: в
// справочнике группа называется «Магазины», а в таблице плана «3.Магазины»
// (ТЗ §6 п.1 прямо предупреждает, что это одно и то же разными строками).
func retailGroupKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.Index(s, "."); i >= 0 && i <= 2 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

// Includes — входит ли ЦФО в розницу. Исключение сильнее включения.
func (sc *RetailScope) Includes(codeCFO int, groupCFO1 string) bool {
	if sc == nil {
		return true
	}
	if sc.excludeCFO[codeCFO] {
		return false
	}
	if sc.includeCFO[codeCFO] {
		return true
	}
	return sc.groups[retailGroupKey(groupCFO1)]
}

// Groups — группы ЦФО правила (для SQL-фильтра источника).
func (sc *RetailScope) Groups() []string {
	if sc == nil {
		return nil
	}
	out := make([]string, 0, len(sc.groups))
	for g := range sc.groups {
		out = append(out, g)
	}
	return out
}

// retailStoreClosed — закрытая точка справочника (правка §12: отбор по статусу,
// а не по наличию факта). Закрытым считаем магазин с заполненной датой закрытия
// или со стадией/РМ-меткой закрытия — источник использует и то, и другое.
func retailStoreClosed(st RetailStore) bool {
	if strings.TrimSpace(st.DateClose) != "" {
		return true
	}
	stage := strings.ToLower(strings.TrimSpace(st.Stage))
	if stage == "closed" || strings.Contains(stage, "закрыт") {
		return true
	}
	rm := strings.ToLower(strings.TrimSpace(st.RegManager))
	return rm == "closed"
}

// scopeRulesFromDir — правило из справочника с фолбэком на сид.
func scopeRulesFromDir(ctx context.Context, repo dirRowsSource) []RetailScopeRule {
	if repo == nil {
		return RetailScopeSeed()
	}
	rows, err := repo.Rows(ctx, "dir_retail_scope")
	if err != nil || len(rows) == 0 {
		if err != nil {
			log.Printf("plans retail: справочник состава розницы недоступен, работаем по правилу ТЗ: %v", err)
		}
		return RetailScopeSeed()
	}
	out := make([]RetailScopeRule, 0, len(rows))
	for _, r := range rows {
		out = append(out, RetailScopeRule{
			Kind:  strAt(r.Payload, "kind"),
			Value: strAt(r.Payload, "value"),
			Note:  strAt(r.Payload, "note"),
		})
	}
	return out
}
