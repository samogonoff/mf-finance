package plans

import (
	"context"
	"fmt"
)

// Фикстуры формы 1.5 для PLANS_MOCK=1 и для разработки без VPN к OLAP.
//
// Данные синтетические, но структура — настоящая: те же группы PL и коды статей,
// что в ТЗ §7.3 и §5, и те же атрибуты ЦФО, что даёт справочник §2.4. Этого
// достаточно, чтобы на стенде проверялись сетка, фильтры, группировка и итоги.
// Числа подогнаны так, чтобы факт по месяцам различался: одинаковые суммы
// маскируют ошибки раскладки значений по колонкам.

type expMockSource struct{}

// expMockStatement — статья: код PL, группа и наименование.
type expMockStatement struct {
	CodePL  int
	GroupPL string
	Expense string
}

// expMockStatements — статьи фикстуры. Коды взяты из ТЗ (§5, §6): 24 и 26 — ФОТ,
// 27 — командировки, 48 — ИТ, 51/52 — логистика, 54/55 — аренда.
var expMockStatements = []expMockStatement{
	{24, "ВЫПЛАТЫ РАБОТНИКАМ", "Заработная плата"},
	{26, "ВЫПЛАТЫ РАБОТНИКАМ", "Отчисления от заработной платы"},
	{27, "ПРОЧИЕ РАСХОДЫ", "Расходы на командировки"},
	{48, "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", "ИТ-расходы"},
	{54, "АРЕНДА", "Аренда помещений (стоянки)"},
	{55, "АРЕНДА", "Аренда имущества"},
	{61, "ОСНОВНЫЕ РАСХОДЫ", "Коммунальные услуги"},
	{72, "РЕКЛАМА И МАРКЕТИНГ", "Реклама и продвижение"},
	{90, "АМОРТИЗАЦИЯ", "Амортизация основных средств"},
}

// expMockMPStatements — у маркетплейсов групп меньше (ТЗ §7.3), плюс логистика
// по ЦФО 250 (ТЗ §5.3).
var expMockMPStatements = []expMockStatement{
	{24, "ВЫПЛАТЫ РАБОТНИКАМ", "Заработная плата"},
	{26, "ВЫПЛАТЫ РАБОТНИКАМ", "Отчисления от заработной платы"},
	{48, "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", "ИТ-расходы"},
	{51, "ОСНОВНЫЕ РАСХОДЫ", "Грузоперевозки экспорт"},
	{52, "ОСНОВНЫЕ РАСХОДЫ", "Грузоперевозки развоз и перемещение"},
	{72, "РЕКЛАМА И МАРКЕТИНГ", "Реклама и продвижение"},
	{90, "АМОРТИЗАЦИЯ", "Амортизация основных средств"},
}

// expMockCfo — ЦФО фикстуры по виду формы и стране.
func expMockCfo(kind, country string) []ExpCfoAttrs {
	if kind == ExpKindMP {
		switch country {
		case "KZ":
			return []ExpCfoAttrs{
				{CodeCFO: 261, CFO: "Wildberries KZ", GroupCFO2: "Маркетплейсы KZ", Country: "KZ", LegalEntity: "MFKaz"},
				{CodeCFO: 262, CFO: "Ozon KZ", GroupCFO2: "Маркетплейсы KZ", Country: "KZ", LegalEntity: "MFKaz"},
			}
		case "UZ":
			return []ExpCfoAttrs{
				{CodeCFO: 271, CFO: "Uzum", GroupCFO2: "Маркетплейсы UZ", Country: "UZ", LegalEntity: "MFUz"},
			}
		default:
			return []ExpCfoAttrs{
				{CodeCFO: 250, CFO: "Отдел маркетплейсов", GroupCFO2: "Отдел", Country: "RU", LegalEntity: "TDMF"},
				{CodeCFO: 251, CFO: "Wildberries", GroupCFO2: "Крупные МП", Country: "RU", LegalEntity: "TDMF"},
				{CodeCFO: 252, CFO: "Ozon", GroupCFO2: "Крупные МП", Country: "RU", LegalEntity: "TDMF"},
				{CodeCFO: 253, CFO: "Lamoda", GroupCFO2: "Крупные МП", Country: "RU", LegalEntity: "TDMF"},
				{CodeCFO: 254, CFO: "Яндекс.Маркет", GroupCFO2: "Мелкие МП", Country: "RU", LegalEntity: "TDMF"},
			}
		}
	}
	return []ExpCfoAttrs{
		{CodeCFO: 101, CFO: "Магазин Минск, Немига", GroupCFO2: "Минск", Country: "BY", LegalEntity: "F", StoreType: "флагман", RegManager: "Ковалёва И."},
		{CodeCFO: 102, CFO: "Магазин Минск, Каменная Горка", GroupCFO2: "Минск", Country: "BY", LegalEntity: "F", StoreType: "стандарт", RegManager: "Ковалёва И."},
		{CodeCFO: 103, CFO: "Магазин Гомель, Центр", GroupCFO2: "Гомель", Country: "BY", LegalEntity: "F", StoreType: "стандарт", RegManager: "Пашкевич А."},
		{CodeCFO: 104, CFO: "Магазин Брест, Мега", GroupCFO2: "Брест", Country: "BY", LegalEntity: "F", StoreType: "компакт", RegManager: "Пашкевич А."},
		{CodeCFO: 105, CFO: "Магазин Витебск, Эвиком", GroupCFO2: "Витебск", Country: "BY", LegalEntity: "F", StoreType: "стандарт", RegManager: "Сидоренко В."},
	}
}

func expMockStatementsFor(kind string) []expMockStatement {
	if kind == ExpKindMP {
		return expMockMPStatements
	}
	return expMockStatements
}

// Costs — факт фикстуры: по каждой паре «ЦФО × статья» двенадцать месяцев года.
// Сценарий только «Факт»: план — это ввод пользователя, подставлять его в
// фикстуре нельзя, иначе форма будет выглядеть заполненной сама собой.
func (expMockSource) Costs(_ context.Context, kind, country string, years []int) ([]ExpCostCell, error) {
	cfos := expMockCfo(kind, country)
	stmts := expMockStatementsFor(kind)
	out := make([]ExpCostCell, 0, len(cfos)*len(stmts)*12)
	for _, y := range years {
		for _, c := range cfos {
			for si, st := range stmts {
				for m := 1; m <= 12; m++ {
					// Разброс без случайности: тест и стенд должны видеть одно и то же.
					amt := float64((st.CodePL*100+c.CodeCFO)%900+120) * (1 + float64(m)/24) * (1 + float64(si)/10)
					out = append(out, ExpCostCell{
						Scenario: "Факт", Year: y, Month: m,
						CodePL: st.CodePL, GroupPL: st.GroupPL, Expense: st.Expense,
						CodeCFO: c.CodeCFO, CFO: c.CFO,
						GroupCFO1: expKindGroup(kind), GroupCFO2: c.GroupCFO2,
						Country: c.Country, CountryCompany: c.Country,
						Company: c.LegalEntity,
						Amount:  float64(int(amt*100)) / 100,
					})
				}
			}
		}
	}
	return out, nil
}

// Cfo — справочник ЦФО фикстуры.
func (expMockSource) Cfo(_ context.Context, kind, country string) (map[int]ExpCfoAttrs, error) {
	out := map[int]ExpCfoAttrs{}
	for _, c := range expMockCfo(kind, country) {
		c.GroupCFO1 = expKindGroup(kind)
		out[c.CodeCFO] = c
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("фикстур ЦФО нет для %s/%s", kind, country)
	}
	return out, nil
}
