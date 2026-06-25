package plans

import "errors"

// Справочники TPL-MP как каноничные seed-данные (VS1). Источник — Excel-прототипы
// var/plan/Тактический план_*_МП (листы «Маркетплейсы_*», «Списки_для_фильтров»)
// и «Справочник ЦФО и ЦЗ» → «Расходы Code PL». См. docs/reports/plans/SPEC.md §9, §26.
//
// Для MVP это статичная НСИ (mp_платформы и статьи PL не синхронизируются ниоткуда),
// поэтому держим её в коде и отдаём через API напрямую — без зависимости от БД.
// Таблицы plans_directory* (миграция 0010) создаются для будущей синхронизации и
// ABAC-джойнов; метаданные справочников засеяны в самой миграции.

// ErrUnknownDirectory — запрошен справочник, которого нет в реестре.
var ErrUnknownDirectory = errors.New("unknown directory")

// MarketplaceRow — строка dir_marketplace (площадка МП).
type MarketplaceRow struct {
	CodeCFO  int    `json:"code_cfo"`
	NameCFO  string `json:"name_cfo"`
	Segment  string `json:"segment"`  // large | small
	Country  string `json:"country"`  // RU | KZ | UZ
	GroupCFO int    `json:"group_cfo"` // 250 (large) | 480 (small)
	CodePL   int    `json:"code_pl"`  // 1046 — товарооборот
}

// PLLineRow — строка dir_pl_line (статья PL).
type PLLineRow struct {
	CodePL    int    `json:"code_pl"`
	Name      string `json:"expense_name"`
	GroupPL   string `json:"group_pl,omitempty"`
	BlockType string `json:"block_type,omitempty"`
}

// CFORow — строка dir_cfo (MP-подмножество: площадки + групповые коды).
type CFORow struct {
	CodeCFO   int    `json:"code_cfo"`
	NameCFO   string `json:"name_cfo"`
	Country   string `json:"country,omitempty"`
	GroupCFO1 string `json:"group_cfo1,omitempty"`
}

// MarketplaceSeed — площадки large (группа 250) и small (группа 480).
func MarketplaceSeed() []MarketplaceRow {
	return []MarketplaceRow{
		// large (RU)
		{CodeCFO: 335, NameCFO: "Wildberries", Segment: "large", Country: "RU", GroupCFO: 250, CodePL: 1046},
		{CodeCFO: 336, NameCFO: "Lamoda", Segment: "large", Country: "RU", GroupCFO: 250, CodePL: 1046},
		{CodeCFO: 337, NameCFO: "Ozon", Segment: "large", Country: "RU", GroupCFO: 250, CodePL: 1046},
		{CodeCFO: 954, NameCFO: "Yandex Market", Segment: "large", Country: "RU", GroupCFO: 250, CodePL: 1046},
		// small (RU)
		{CodeCFO: 953, NameCFO: "Детский мир", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046},
		{CodeCFO: 955, NameCFO: "Золотое яблоко", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046},
		{CodeCFO: 957, NameCFO: "Yandex Market ТЕКС", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046},
		{CodeCFO: 959, NameCFO: "Детский мир ТЕКС", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046},
		{CodeCFO: 990, NameCFO: "Wildberries ТЕКС", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046},
		{CodeCFO: 991, NameCFO: "Ozon ТЕКС", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046},
		{CodeCFO: 958, NameCFO: "Lamoda ТЕКС", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046},
		// small (KZ/UZ)
		{CodeCFO: 475, NameCFO: "Wildberries KZ", Segment: "small", Country: "KZ", GroupCFO: 480, CodePL: 1046},
		{CodeCFO: 474, NameCFO: "Ozon KZ", Segment: "small", Country: "KZ", GroupCFO: 480, CodePL: 1046},
		{CodeCFO: 338, NameCFO: "Kaspi", Segment: "small", Country: "KZ", GroupCFO: 480, CodePL: 1046},
		{CodeCFO: 339, NameCFO: "Uzmarket", Segment: "small", Country: "UZ", GroupCFO: 480, CodePL: 1046},
	}
}

// PLLineSeed — статьи PL для формы TPL-MP: блоки продаж/себестоимости (code_pl 1xxx/8xxx),
// штрафы (66) и статьи прямых/общих затрат (10–98) из «Расходы Code PL».
func PLLineSeed() []PLLineRow {
	return []PLLineRow{
		// Блоки продаж/себестоимости (строки-итоги формы)
		{CodePL: 1046, Name: "ПРОДАЖИ по ценам менеджера с НДС", GroupPL: "ПРОДАЖИ", BlockType: "sales_manager_price"},
		{CodePL: 1045, Name: "ПРОДАЖИ по ценам менеджера без НДС", GroupPL: "ПРОДАЖИ", BlockType: "sales_manager_price_net"},
		{CodePL: 1022, Name: "ПРОДАЖИ по цене площадки с НДС", GroupPL: "ПРОДАЖИ", BlockType: "sales_platform_price"},
		{CodePL: 1006, Name: "ПРОДАЖИ по цене площадки без НДС", GroupPL: "ПРОДАЖИ", BlockType: "sales_platform_price_net"},
		{CodePL: 8006, Name: "Себестоимость по отпускным ценам", GroupPL: "СЕБЕСТОИМОСТЬ", BlockType: "shipments"},
		{CodePL: 2006, Name: "Себестоимость общая (Cost of goods) осн+пошив", GroupPL: "СЕБЕСТОИМОСТЬ", BlockType: "cogs_total"},
		{CodePL: 6006, Name: "Себестоимость общая (Cost of goods) осн+пошив", GroupPL: "СЕБЕСТОИМОСТЬ", BlockType: "cogs_total"},
		// Прямые затраты по площадкам / общие затраты (Расходы Code PL)
		{CodePL: 10, Name: "Реклама-наружная (борды, метро)", GroupPL: "РЕКЛАМА И МАРКЕТИНГ"},
		{CodePL: 12, Name: "Реклама-тв, радио", GroupPL: "РЕКЛАМА И МАРКЕТИНГ"},
		{CodePL: 13, Name: "Реклама-интернет", GroupPL: "РЕКЛАМА И МАРКЕТИНГ"},
		{CodePL: 14, Name: "Реклама-SEO, контекстная, ремаркетинг, e-mail-рассылка", GroupPL: "РЕКЛАМА И МАРКЕТИНГ"},
		{CodePL: 15, Name: "Реклама-соц.сети", GroupPL: "РЕКЛАМА И МАРКЕТИНГ"},
		{CodePL: 16, Name: "Реклама-транспорт, лифты", GroupPL: "РЕКЛАМА И МАРКЕТИНГ"},
		{CodePL: 24, Name: "Заработная плата МФ (РБ)/ ТД (РФ)", GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ"},
		{CodePL: 26, Name: "Налоги от выплат МФ(РБ)/ ТД (РФ)", GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ"},
		{CodePL: 45, Name: "Расходы на упаковку (пакеты)", GroupPL: "ОСНОВНЫЕ РАСХОДЫ"},
		{CodePL: 48, Name: "Расходы на IT обслуживание (программное обеспечение)", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ"},
		{CodePL: 51, Name: "Грузоперевозки экспорт", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ"},
		{CodePL: 52, Name: "Грузоперевозки развоз и перемещение", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ"},
		{CodePL: 54, Name: "Аренда помещений (складская логистика)", GroupPL: "АРЕНДА"},
		{CodePL: 58, Name: "Банковские расходы / эквайринг", GroupPL: "ПРОЧИЕ РАСХОДЫ"},
		{CodePL: 64, Name: "Агентское (комиссионное) вознаграждение", GroupPL: "ПРОЧИЕ РАСХОДЫ"},
		{CodePL: 66, Name: "Штрафы / прочие удержания и компенсации", GroupPL: "ПРОЧИЕ РАСХОДЫ", BlockType: "penalties"},
		{CodePL: 67, Name: "Амортизация", GroupPL: "АМОРТИЗАЦИЯ"},
	}
}

// CFOSeed — MP-подмножество dir_cfo: площадки (как ЦФО) + групповые коды 250/480.
func CFOSeed() []CFORow {
	out := []CFORow{
		{CodeCFO: 250, NameCFO: "Маркетплейсы (large)", GroupCFO1: "4.Маркетплейсы"},
		{CodeCFO: 480, NameCFO: "Маркетплейсы (small)", GroupCFO1: "4.Маркетплейсы"},
	}
	for _, m := range MarketplaceSeed() {
		out = append(out, CFORow{CodeCFO: m.CodeCFO, NameCFO: m.NameCFO, Country: m.Country, GroupCFO1: "4.Маркетплейсы"})
	}
	return out
}

// Directory — метаданные справочника для реестра (GET /api/plans/directories).
type Directory struct {
	Code       string `json:"code"`
	Source     string `json:"source"`      // manual|1c|olap|calculated
	SyncStatus string `json:"sync_status"` // seed — статичная НСИ в коде (MVP)
	RowCount   int    `json:"row_count"`
}

// DirSource — источник справочников. Для MVP — SeedSource (в памяти);
// этап 2 — синхронизация из 1С/OLAP в plans_directory*.
type DirSource interface {
	Directories() []Directory
	Rows(code string) (any, error)
}

// SeedSource отдаёт справочники из seed-данных в коде (без БД).
type SeedSource struct{}

// NewSeedSource — конструктор.
func NewSeedSource() *SeedSource { return &SeedSource{} }

// Directories — реестр доступных справочников.
func (s *SeedSource) Directories() []Directory {
	return []Directory{
		{Code: "dir_marketplace", Source: "manual", SyncStatus: "seed", RowCount: len(MarketplaceSeed())},
		{Code: "dir_pl_line", Source: "manual", SyncStatus: "seed", RowCount: len(PLLineSeed())},
		{Code: "dir_cfo", Source: "manual", SyncStatus: "seed", RowCount: len(CFOSeed())},
	}
}

// Rows — строки справочника по коду; ErrUnknownDirectory для неизвестного.
func (s *SeedSource) Rows(code string) (any, error) {
	switch code {
	case "dir_marketplace":
		return MarketplaceSeed(), nil
	case "dir_pl_line":
		return PLLineSeed(), nil
	case "dir_cfo":
		return CFOSeed(), nil
	default:
		return nil, ErrUnknownDirectory
	}
}
