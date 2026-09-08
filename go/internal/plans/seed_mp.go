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
// LegalEntity — провизорный маппинг площадка→ЮЛ для свода TPL-08 (Q-ЮЛ:
// уточнить у аналитика; SPEC Приложение B — ЮЛ продаж: TD Mark Formelle,
// Formelle, MF Kazakhstan, MF Tex).
type MarketplaceRow struct {
	CodeCFO     int    `json:"code_cfo"`
	NameCFO     string `json:"name_cfo"`
	Segment     string `json:"segment"`  // large | small
	Country     string `json:"country"`  // RU | KZ | UZ
	GroupCFO    int    `json:"group_cfo"` // 250 (large) | 480 (small)
	CodePL      int    `json:"code_pl"`  // 1046 — товарооборот
	LegalEntity string `json:"legal_entity"`
	// Норматив удержаний площадки живёт в отдельном справочнике dir_mp_norm
	// (миграция 0050): он зависит от ПЕРИОДА, а строка площадки периода не имеет.
}

// PLLineRow — строка dir_pl_line (статья PL).
//
// Колонки после Budgeting.dbo.CodePL (правки ТЗ 02.09.2026, часть 5): справочник
// статей несёт не только подпись, но и НОРМАТИВНЫЙ НДС по странам, статью ДДС и
// флаг особого доступа. Раньше пять НДС-колонок в ТЗ были описаны как
// «коэффициенты индексации» — это оказалось неверно (правка МП §18), никакой
// индексации в справочнике нет.
type PLLineRow struct {
	CodePL    int    `json:"code_pl"`
	Name      string `json:"expense_name"`
	GroupPL   string `json:"group_pl,omitempty"`
	BlockType string `json:"block_type,omitempty"`
	// Нормативный НДС по странам. ВНИМАНИЕ: в источнике это КОЭФФИЦИЕНТ «1 + ставка»
	// (BY 1,20 · RU 1,22 · KZ 1,16 · UZ 1,12 · TR 1,00), а не сама ставка;
	// 1,00 означает «НДС по этой статье в этой стране не начисляется».
	// Формат сохраняем как в источнике — иначе сверка со справочником перестанет
	// быть механической. Ставку отдаёт RateBook.NormativeVat.
	VatBY float64 `json:"vat_by,omitempty"`
	VatRU float64 `json:"vat_ru,omitempty"`
	VatKZ float64 `json:"vat_kz,omitempty"`
	VatUZ float64 `json:"vat_uz,omitempty"`
	VatTR float64 `json:"vat_tr,omitempty"`
	// CFItem — статья движения денежных средств. Строка, а не число: в источнике
	// встречаются 'инвест' и '-99999' (ВГО-обороты).
	CFItem string `json:"cf_item,omitempty"`
	// SpecialAccess — тот же флаг 0/1, что выбирается в ALL_view.
	SpecialAccess int `json:"special_access,omitempty"`
	// Note — служебная пометка (расхождения с ТЗ, требующие сверки).
	Note string `json:"note,omitempty"`
}

// Нормативные коэффициенты НДС по странам (правки ТЗ 02.09.2026, часть 5).
// vatNone — статья, по которой НДС не начисляется (в источнике ровно 1,00).
const (
	vatNone = 1.00
	vatBY   = 1.20
	vatRU   = 1.22
	vatKZ   = 1.16
	vatUZ   = 1.12
	vatTR   = 1.00 // Турция: в справочнике всегда 1,00 — НДС не применяется
)

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
		// large (RU) → TD Mark Formelle
		{CodeCFO: 335, NameCFO: "Wildberries", Segment: "large", Country: "RU", GroupCFO: 250, CodePL: 1046, LegalEntity: "TD Mark Formelle"},
		{CodeCFO: 336, NameCFO: "Lamoda", Segment: "large", Country: "RU", GroupCFO: 250, CodePL: 1046, LegalEntity: "TD Mark Formelle"},
		{CodeCFO: 337, NameCFO: "Ozon", Segment: "large", Country: "RU", GroupCFO: 250, CodePL: 1046, LegalEntity: "TD Mark Formelle"},
		{CodeCFO: 954, NameCFO: "Yandex Market", Segment: "large", Country: "RU", GroupCFO: 250, CodePL: 1046, LegalEntity: "TD Mark Formelle"},
		// small (RU) — общие площадки → TD Mark Formelle
		{CodeCFO: 953, NameCFO: "Детский мир", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046, LegalEntity: "TD Mark Formelle"},
		{CodeCFO: 955, NameCFO: "Золотое яблоко", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046, LegalEntity: "TD Mark Formelle"},
		// small (RU) — ТЕКС-бренды → MF Tex
		{CodeCFO: 957, NameCFO: "Yandex Market ТЕКС", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046, LegalEntity: "MF Tex"},
		// 959 в списке площадок правок 02.09.2026 (МП §4) отсутствует — там 14 ЦФО.
		// Не удаляем: строка пришла из данных, и молча терять площадку хуже, чем
		// показать её с пометкой «сверить со справочником».
		{CodeCFO: 959, NameCFO: "Детский мир ТЕКС", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046, LegalEntity: "MF Tex"},
		{CodeCFO: 990, NameCFO: "Wildberries ТЕКС", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046, LegalEntity: "MF Tex"},
		{CodeCFO: 991, NameCFO: "Ozon ТЕКС", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046, LegalEntity: "MF Tex"},
		{CodeCFO: 958, NameCFO: "Lamoda ТЕКС", Segment: "small", Country: "RU", GroupCFO: 480, CodePL: 1046, LegalEntity: "MF Tex"},
		// small (KZ) → MF Kazakhstan
		{CodeCFO: 475, NameCFO: "Wildberries KZ", Segment: "small", Country: "KZ", GroupCFO: 480, CodePL: 1046, LegalEntity: "MF Kazakhstan"},
		{CodeCFO: 474, NameCFO: "Ozon KZ", Segment: "small", Country: "KZ", GroupCFO: 480, CodePL: 1046, LegalEntity: "MF Kazakhstan"},
		{CodeCFO: 338, NameCFO: "Kaspi", Segment: "small", Country: "KZ", GroupCFO: 480, CodePL: 1046, LegalEntity: "MF Kazakhstan"},
		// small (UZ) → MFUz. Провизорное «MF Kazakhstan» держалось с первого сида и
		// было найдено на экране 07.09.2026: в своде Uzmarket стоял на казахском
		// ЮЛ, то есть узбекские продажи ушли бы на чужую компанию (УНП 305554644,
		// dir_legal_entity UZ-305554644).
		{CodeCFO: 339, NameCFO: "Uzmarket", Segment: "small", Country: "UZ", GroupCFO: 480, CodePL: 1046, LegalEntity: "MFUz"},
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
		{CodePL: 10, Name: "Реклама-наружная (борды, метро)", GroupPL: "РЕКЛАМА И МАРКЕТИНГ", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "4"},
		{CodePL: 12, Name: "Реклама-тв, радио", GroupPL: "РЕКЛАМА И МАРКЕТИНГ", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "4"},
		{CodePL: 13, Name: "Реклама-интернет", GroupPL: "РЕКЛАМА И МАРКЕТИНГ", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "4"},
		{CodePL: 14, Name: "Реклама-SEO, контекстная, ремаркетинг, e-mail-рассылка", GroupPL: "РЕКЛАМА И МАРКЕТИНГ", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "4"},
		{CodePL: 15, Name: "Реклама-соц.сети", GroupPL: "РЕКЛАМА И МАРКЕТИНГ", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "4"},
		{CodePL: 16, Name: "Реклама-транспорт, лифты", GroupPL: "РЕКЛАМА И МАРКЕТИНГ", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "4"},
		{CodePL: 24, Name: "Заработная плата МФ (РБ)/ ТД (РФ)", GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ", VatBY: vatNone, VatRU: vatNone, VatKZ: vatNone, VatUZ: vatNone, VatTR: vatNone, CFItem: "7"},
		{CodePL: 26, Name: "Налоги от выплат МФ(РБ)/ ТД (РФ)", GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ", VatBY: vatNone, VatRU: vatNone, VatKZ: vatNone, VatUZ: vatNone, VatTR: vatNone, CFItem: "11"},
		{CodePL: 45, Name: "Расходы на упаковку (пакеты)", GroupPL: "ОСНОВНЫЕ РАСХОДЫ", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "3"},
		{CodePL: 48, Name: "Расходы на IT обслуживание (программное обеспечение)", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "3"},
		{CodePL: 50, Name: "Грузоперевозки доставка сырья", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", VatBY: vatNone, VatRU: vatNone, VatKZ: vatNone, VatUZ: vatNone, VatTR: vatNone, CFItem: "3"},
		{CodePL: 51, Name: "Грузоперевозки экспорт", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", VatBY: vatNone, VatRU: vatNone, VatKZ: vatNone, VatUZ: vatNone, VatTR: vatNone, CFItem: "3"},
		{CodePL: 52, Name: "Грузоперевозки развоз и перемещение", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "3"},
		{CodePL: 53, Name: "Расходы на охрану и страхование", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "3"},
		{CodePL: 54, Name: "Аренда помещений (стоянки)", GroupPL: "АРЕНДА", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "6"},
		{CodePL: 55, Name: "Аренда имущества", GroupPL: "АРЕНДА", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "6"},
		{CodePL: 58, Name: "Банковские расходы / эквайринг", GroupPL: "ПРОЧИЕ РАСХОДЫ", VatBY: vatNone, VatRU: vatNone, VatKZ: vatNone, VatUZ: vatNone, VatTR: vatNone, CFItem: "10"},
		{CodePL: 64, Name: "Агентское (комиссионное) вознаграждение", GroupPL: "ПРОЧИЕ РАСХОДЫ", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "3"},
		// Отдельного кода под штрафы в справочнике нет (правка ТЗ МП §5): 66 —
		// это «Прочие расходы», и штрафы площадок вводятся в этой строке формы.
		{CodePL: 66, Name: "Прочие расходы", GroupPL: "ПРОЧИЕ РАСХОДЫ", BlockType: "penalties", VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "3"},
		{CodePL: 67, Name: "Амортизация", GroupPL: "АМОРТИЗАЦИЯ", VatBY: vatNone, VatRU: vatNone, VatKZ: vatNone, VatUZ: vatNone, VatTR: vatNone},
		// Статьи, на которых аналитик проверил трактовку НДС-колонок (часть 5 ТЗ).
		// Держим их в сиде именно поэтому: на них ломается любая попытка
		// «упростить» НДС до одной ставки на страну.
		{CodePL: 49, Name: "Расходы на телефонную связь и интернет", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ",
			// В Беларуси услуги электросвязи облагаются по отдельной ставке 25 %.
			VatBY: 1.25, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatUZ, VatTR: vatTR, CFItem: "3"},
		{CodePL: 62, Name: "Информационные услуги", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ",
			VatBY: vatBY, VatRU: vatRU, VatKZ: vatNone, VatUZ: vatNone, VatTR: vatNone, CFItem: "3"},
		{CodePL: 63, Name: "Таможенные услуги", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ",
			VatBY: vatBY, VatRU: vatRU, VatKZ: vatNone, VatUZ: vatNone, VatTR: vatNone, CFItem: "9"},
		{CodePL: 68, Name: "Приобретение имущества", GroupPL: "ИНВЕСТИЦИИ",
			VatBY: vatBY, VatRU: vatRU, VatKZ: vatKZ, VatUZ: vatNone, VatTR: vatTR, CFItem: "инвест"},
		{CodePL: 93, Name: "Грузоперевозки импорт товара", GroupPL: "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", VatBY: vatNone, VatRU: vatNone, VatKZ: vatNone, VatUZ: vatNone, VatTR: vatNone, CFItem: "3"},
	}
}

// CFOSeed — MP-подмножество dir_cfo: площадки (как ЦФО) + групповые коды 250/480.
func CFOSeed() []CFORow {
	out := []CFORow{
		// 250 — «Отдел маркетплейса» (правка ТЗ МП §3): группа 2.Отделы, страна BY.
		// В форме он остаётся групповым кодом крупных площадок, но наименование и
		// страна берутся из справочника, иначе свод по ЦФО не сойдётся с отчётами.
		{CodeCFO: 250, NameCFO: "Отдел маркетплейса", Country: "BY", GroupCFO1: "2.Отделы"},
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
		{Code: "dir_fx_rate", Source: "manual", SyncStatus: "seed", RowCount: len(FxRateSeed())},
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
	case "dir_fx_rate":
		return FxRateSeed(), nil
	default:
		return nil, ErrUnknownDirectory
	}
}
