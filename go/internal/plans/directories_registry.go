package plans

// Реестр справочников — единый источник истины по ПРЕЗЕНТАЦИИ НСИ (ТЗ §«UI
// справочников», экран №5). Каждая запись описывает человекочитаемое имя,
// группу, иконку и ТИПИЗИРОВАННЫЕ колонки, по которым фронт строит таблицу и
// карточку записи (а не «сырой дамп SQL»). Состояние синхронизации/кэша/TTL
// живёт в БД (plans_directory) — здесь только схема отображения.

// ColType — тип колонки для рендера ячейки на фронте.
type ColType string

const (
	ColText    ColType = "text"    // строка
	ColNumber  ColType = "number"  // целое/моноширинное, выравнивание вправо
	ColMoney   ColType = "money"   // денежное, tabular-nums, 2 знака
	ColCountry ColType = "country" // код страны → флаг/метка (RU/BY/KZ/UZ)
	ColBadge   ColType = "badge"   // значение → цветной бейдж (BadgeTones)
	ColBool    ColType = "bool"    // да/нет иконкой
	ColDate    ColType = "date"    // дата
	ColCode    ColType = "code"    // технический код (моно, без форматирования)
)

// DirColumn — описание колонки справочника.
type DirColumn struct {
	Key        string            `json:"key"`                  // ключ в payload_json
	Label      string            `json:"label"`                // заголовок колонки (рус.)
	Type       ColType           `json:"type"`                 // тип ячейки
	Primary    bool              `json:"primary,omitempty"`    // главный идентификатор записи
	Filterable bool              `json:"filterable,omitempty"` // выпадающий фильтр в тулбаре
	Search     bool              `json:"search,omitempty"`     // участвует в полнотекстовом поиске
	Width      string            `json:"width,omitempty"`      // CSS-ширина (опц.)
	Hint       string            `json:"hint,omitempty"`       // подсказка в карточке записи
	BadgeTones map[string]string `json:"badge_tones,omitempty"`// значение → тон бейджа (pos/neg/info/warn/muted)
	// Ref — связь «через справочник», а не текстом: при редактировании карточки
	// поле рендерится селектом/пикером. "user" → UserPicker (пишет имя в key и id
	// в key+"_user_id"); код справочника (dir_*) → select его значений.
	Ref string `json:"ref,omitempty"`
	// Readonly — поле не редактируется в карточке (значение производное, напр.
	// директор ЦФО = директор его ЮЛ).
	Readonly bool `json:"readonly,omitempty"`
}

// DirGroup — группировка справочников в левом меню.
type DirGroup string

const (
	GroupManual     DirGroup = "manual"     // наши, редактируемые
	GroupLisa       DirGroup = "lisa"       // из Лисы (синхронизация)
	Group1C         DirGroup = "1c"         // из 1С (синхронизация)
	GroupCalculated DirGroup = "calculated" // расчётные
)

// DirSchema — презентационная схема справочника.
type DirSchema struct {
	Code        string      `json:"code"`
	Name        string      `json:"name"`        // человекочитаемое имя (в UI вместо code)
	Description string      `json:"description"` // назначение в PL (из ТЗ §7.1)
	Group       DirGroup    `json:"group"`
	Icon        string      `json:"icon"`        // lucide-имя
	Stage       string      `json:"stage"`       // этап(ы) процесса по ТЗ
	Columns     []DirColumn `json:"columns"`
	GroupBy     []string    `json:"group_by,omitempty"` // ключи иерархии для tree-вида (вложенные справочники)
}

var countryTones = map[string]string{"RU": "info", "BY": "pos", "KZ": "warn", "UZ": "muted", "РФ": "info", "РБ": "pos", "КЗ": "warn", "УЗ": "muted"}

// dirSchemas — реестр всех справочников модуля.
var dirSchemas = map[string]DirSchema{
	"dir_cfo": {
		Code: "dir_cfo", Name: "ЦФО и ЦЗ", Group: GroupManual, Icon: "lucide:building-2",
		Description: "Центры финансовой ответственности и затрат — разрез PL и матрица прав. Иерархия: страна → группа → ЦФО. Источник: «Справочник ЦФО и ЦЗ».",
		Stage:   "все",
		GroupBy: []string{"country", "group_cfo1", "group_cfo2"},
		Columns: []DirColumn{
			{Key: "code_cfo", Label: "Код ЦФО", Type: ColCode, Primary: true, Search: true, Width: "90px"},
			{Key: "name_cfo", Label: "Наименование", Type: ColText, Search: true},
			{Key: "country", Label: "Страна", Type: ColCountry, Filterable: true, BadgeTones: countryTones, Ref: "dir_country"},
			{Key: "group_cfo1", Label: "Группа", Type: ColBadge, Filterable: true, Ref: "dir_cfo_group"},
			{Key: "group_cfo2", Label: "Подгруппа", Type: ColText, Filterable: true, Ref: "dir_cfo_subgroup"},
			{Key: "entity_type", Label: "Тип", Type: ColBadge, Filterable: true, BadgeTones: map[string]string{"магазин": "pos", "производство": "info", "отдел": "warn"}, Ref: "dir_cfo_type"},
			{Key: "legal_entity", Label: "ЮЛ", Type: ColCode, Filterable: true, Ref: "dir_legal_entity"},
			{Key: "city", Label: "Город", Type: ColText, Search: true},
			{Key: "trade_area", Label: "Площадь, м²", Type: ColNumber},
			{Key: "lisa_id", Label: "ID Лисы", Type: ColCode, Width: "90px"},
			{Key: "top", Label: "ТОП", Type: ColText, Hint: "топ-менеджер ЦФО (согласует планы)"},
			{Key: "director", Label: "Директор (от ЮЛ)", Type: ColText, Readonly: true, Hint: "автоматически = директор ЮЛ"},
		},
	},
	"dir_cfo_group": {
		Code: "dir_cfo_group", Name: "Группы ЦФО", Group: GroupManual, Icon: "lucide:layers",
		Description: "Под-справочник: группы ЦФО (group_cfo1) — Розница, Производство, Отдел, Маркетплейсы. Стандартизуется централизованно.",
		Stage: "все",
		Columns: []DirColumn{
			{Key: "code", Label: "Группа", Type: ColText, Primary: true, Search: true},
			{Key: "name", Label: "Наименование", Type: ColText, Search: true},
		},
	},
	"dir_cfo_subgroup": {
		Code: "dir_cfo_subgroup", Name: "Подгруппы ЦФО", Group: GroupManual, Icon: "lucide:layers-2",
		Description: "Под-справочник: подгруппы ЦФО (group_cfo2) — категории магазинов, площадки МП, отделы.",
		Stage: "все",
		Columns: []DirColumn{
			{Key: "code", Label: "Подгруппа", Type: ColText, Primary: true, Search: true},
			{Key: "name", Label: "Наименование", Type: ColText, Search: true},
		},
	},
	"dir_cfo_type": {
		Code: "dir_cfo_type", Name: "Типы ЦФО", Group: GroupManual, Icon: "lucide:shapes",
		Description: "Под-справочник: тип ЦФО (entity_type) — магазин, производство, отдел.",
		Stage: "все",
		Columns: []DirColumn{
			{Key: "code", Label: "Тип", Type: ColBadge, Primary: true, Search: true,
				BadgeTones: map[string]string{"магазин": "pos", "производство": "info", "отдел": "warn"}},
			{Key: "name", Label: "Наименование", Type: ColText},
		},
	},
	"dir_legal_entity": {
		Code: "dir_legal_entity", Name: "Юридические лица", Group: GroupManual, Icon: "lucide:landmark",
		Description: "Глобальный справочник ЮЛ (Приложение B) с директором и ТОПом (ссылки на пользователей). Сырые алиасы из xlsx — для маппинга. «(?)» — требует сверки.",
		Stage: "1.6, 2.4, 3",
		Columns: []DirColumn{
			{Key: "name", Label: "Юр. лицо", Type: ColText, Primary: true, Search: true},
			{Key: "country", Label: "Страна", Type: ColCountry, Filterable: true, BadgeTones: countryTones, Ref: "dir_country"},
			{Key: "director", Label: "Директор", Type: ColText, Search: true, Hint: "согласующий ЮЛ (этап 3)", Ref: "user"},
			{Key: "alias_raw", Label: "Сырые алиасы (xlsx)", Type: ColText},
			{Key: "uncertain", Label: "Сверить", Type: ColBool, Filterable: true},
		},
	},
	"dir_pl_line": {
		Code: "dir_pl_line", Name: "Статьи PL", Group: GroupManual, Icon: "lucide:list-tree",
		Description: "Статьи отчёта о прибылях/убытках (строки шаблонов затрат). Маппинг в расчёты ФОТ, аренды, логистики.",
		Stage: "1.5, 2.3",
		Columns: []DirColumn{
			{Key: "code_pl", Label: "Код PL", Type: ColNumber, Primary: true, Search: true, Width: "90px"},
			{Key: "expense_name", Label: "Статья", Type: ColText, Search: true},
			{Key: "group_pl", Label: "Группа PL", Type: ColBadge, Filterable: true},
			{Key: "block_type", Label: "Тип блока", Type: ColCode},
		},
	},
	"dir_marketplace": {
		Code: "dir_marketplace", Name: "Маркетплейсы", Group: GroupManual, Icon: "lucide:store",
		Description: "Площадки маркетплейсов (канал Marketplaces, этап 1.1). Сегменты large/small.",
		Stage: "1.1",
		Columns: []DirColumn{
			{Key: "code_cfo", Label: "Код ЦФО", Type: ColNumber, Primary: true, Search: true, Width: "100px"},
			{Key: "name_cfo", Label: "Площадка", Type: ColText, Search: true},
			{Key: "segment", Label: "Сегмент", Type: ColBadge, Filterable: true, BadgeTones: map[string]string{"large": "info", "small": "muted"}},
			{Key: "country", Label: "Страна", Type: ColCountry, Filterable: true, BadgeTones: countryTones},
			{Key: "legal_entity", Label: "ЮЛ", Type: ColText, Filterable: true},
			{Key: "code_pl", Label: "Код PL", Type: ColNumber, Width: "90px"},
		},
	},
	"dir_fx_rate": {
		Code: "dir_fx_rate", Name: "Курсы валют", Group: GroupManual, Icon: "lucide:coins",
		Description: "Курсы валют к BYN для пересчёта тактики при своде по ЮЛ. Прод — из Лисы (valuta1).",
		Stage: "1.6, 3",
		Columns: []DirColumn{
			{Key: "currency", Label: "Валюта", Type: ColBadge, Primary: true, Search: true, Width: "100px",
				BadgeTones: map[string]string{"USD": "pos", "RUB": "info", "BYN": "muted", "KZT": "warn", "UZS": "muted"}},
			{Key: "rate_byn", Label: "Курс к BYN", Type: ColMoney},
		},
	},
	"dir_store_to": {
		Code: "dir_store_to", Name: "Магазины (ТО)", Group: GroupLisa, Icon: "lucide:shopping-bag",
		Description: "Мастер-справочник магазинов для товарооборота розницы (этап 1.1). Источник: Лиса s_klient.",
		Stage: "1.1",
		Columns: []DirColumn{
			{Key: "lisa_id", Label: "ID Лисы", Type: ColCode, Primary: true, Width: "90px"},
			{Key: "store_name", Label: "Магазин", Type: ColText, Search: true},
			{Key: "code_cfo", Label: "Код ЦФО", Type: ColNumber, Search: true, Width: "100px"},
			{Key: "country", Label: "Страна", Type: ColCountry, Filterable: true, BadgeTones: countryTones},
			{Key: "lfl_status", Label: "LFL", Type: ColBadge, Filterable: true, BadgeTones: map[string]string{"LFL": "pos", "NEW": "info", "CLOSED": "neg"}},
			{Key: "trade_area", Label: "Площадь, м²", Type: ColNumber},
			{Key: "closed", Label: "Закрыт", Type: ColBool, Filterable: true},
		},
	},
	"dir_lfl": {
		Code: "dir_lfl", Name: "LFL-группы", Group: GroupLisa, Icon: "lucide:git-compare",
		Description: "Классификация магазинов по сопоставимости год-к-году (LFL/новые/закрытые). Источник: Лиса abrvtr.",
		Stage: "1.1",
		Columns: []DirColumn{
			{Key: "year", Label: "Год", Type: ColNumber, Filterable: true, Width: "80px"},
			{Key: "lisa_id", Label: "ID Лисы", Type: ColCode, Primary: true, Width: "90px"},
			{Key: "lfl", Label: "LFL", Type: ColBool},
			{Key: "type", Label: "Тип", Type: ColBadge, Filterable: true},
			{Key: "name_1c", Label: "Имя 1С", Type: ColText, Search: true},
		},
	},
	"dir_lease": {
		Code: "dir_lease", Name: "Договоры аренды", Group: Group1C, Icon: "lucide:file-text",
		Description: "Договоры аренды и условия (площадь, ставка, индексация). Источник: 1С. Вход для расчёта аренды.",
		Stage: "1.1, 1.5",
		Columns: []DirColumn{
			{Key: "contract_no", Label: "Договор", Type: ColCode, Primary: true, Search: true},
			{Key: "store_name", Label: "Объект", Type: ColText, Search: true},
			{Key: "country", Label: "Страна", Type: ColCountry, Filterable: true, BadgeTones: countryTones},
			{Key: "rent_type", Label: "Тип", Type: ColBadge, Filterable: true, BadgeTones: map[string]string{"fixed": "info", "percent_revenue": "warn", "mixed": "muted"}},
			{Key: "area", Label: "Площадь, м²", Type: ColNumber},
			{Key: "rent_rate", Label: "Ставка", Type: ColMoney},
		},
	},
	"dir_payroll_rate": {
		Code: "dir_payroll_rate", Name: "Ставки ФОТ", Group: Group1C, Icon: "lucide:badge-percent",
		Description: "Ставки ФОТ, коэффициенты, соцвзносы. Источник: 1С. Вход для расчёта ФОТ производства и розницы.",
		Stage: "1.1, 2.1",
		Columns: []DirColumn{
			{Key: "code_cfo", Label: "Код ЦФО", Type: ColNumber, Primary: true, Search: true, Width: "100px"},
			{Key: "rate", Label: "Ставка", Type: ColMoney},
			{Key: "coef", Label: "Коэффициент", Type: ColNumber},
			{Key: "soc_pct", Label: "Соцвзносы, %", Type: ColNumber},
		},
	},
	"dir_country": {
		Code: "dir_country", Name: "Страны", Group: GroupLisa, Icon: "lucide:globe",
		Description: "Единый справочник стран — коды, ISO и сокращения в одном месте. Сведение и маппинг идут через него. Источник: Лиса s_country.",
		Stage: "все",
		Columns: []DirColumn{
			{Key: "domain", Label: "Код", Type: ColCountry, Primary: true, Filterable: true, Search: true, Width: "110px", BadgeTones: countryTones},
			{Key: "abbr_ru", Label: "Сокр.", Type: ColBadge, Width: "80px"},
			{Key: "name", Label: "Страна", Type: ColText, Search: true},
			{Key: "iso_code", Label: "ISO", Type: ColCode, Width: "80px"},
			{Key: "full_name", Label: "Полное наименование", Type: ColText},
			{Key: "lisa_id", Label: "ID Лисы", Type: ColCode, Width: "90px"},
		},
	},
	"dir_lisa_prod_units": {
		Code: "dir_lisa_prod_units", Name: "Производственные подразделения", Group: GroupLisa, Icon: "lucide:factory",
		Description: "Производственные подразделения и линии (привязка минут и ФОТ пр-ва). Источник: Лиса. Этап 2.1.",
		Stage: "2.1",
		Columns: []DirColumn{
			{Key: "unit_code", Label: "Код", Type: ColCode, Primary: true, Search: true},
			{Key: "unit_name", Label: "Подразделение / линия", Type: ColText, Search: true},
			{Key: "country", Label: "Страна", Type: ColCountry, Filterable: true, BadgeTones: countryTones},
		},
	},
	"dir_lisa_norms": {
		Code: "dir_lisa_norms", Name: "Нормы времени", Group: GroupLisa, Icon: "lucide:timer",
		Description: "Нормы времени (мин/ед., мин/операция) — база для расчёта ФОТ производства. Источник: Лиса. Этап 2.1.",
		Stage: "2.1",
		Columns: []DirColumn{
			{Key: "operation", Label: "Операция", Type: ColText, Primary: true, Search: true},
			{Key: "minutes_per_unit", Label: "Мин/ед.", Type: ColNumber},
			{Key: "unit", Label: "Ед.", Type: ColBadge},
		},
	},
	"dir_lisa_shipments": {
		Code: "dir_lisa_shipments", Name: "Объёмы отгрузки", Group: GroupLisa, Icon: "lucide:package",
		Description: "Объёмы отгрузки по SKU/группам/складам/странам — вход для логистики. Источник: Лиса. Этапы 1.5, 2.3.",
		Stage: "1.5, 2.3",
		Columns: []DirColumn{
			{Key: "sku_group", Label: "Группа / SKU", Type: ColText, Primary: true, Search: true},
			{Key: "country", Label: "Страна", Type: ColCountry, Filterable: true, BadgeTones: countryTones},
			{Key: "volume", Label: "Объём", Type: ColNumber},
			{Key: "unit", Label: "Ед.", Type: ColBadge},
		},
	},
	"dir_lisa_routes": {
		Code: "dir_lisa_routes", Name: "Маршруты доставки", Group: GroupLisa, Icon: "lucide:route",
		Description: "Маршруты доставки (связка отгрузка → маршрут → тариф). Источник: Лиса. Этапы 1.5, 2.3.",
		Stage: "1.5, 2.3",
		Columns: []DirColumn{
			{Key: "route_code", Label: "Маршрут", Type: ColCode, Primary: true, Search: true},
			{Key: "route_name", Label: "Наименование", Type: ColText, Search: true},
			{Key: "country", Label: "Страна", Type: ColCountry, Filterable: true, BadgeTones: countryTones},
		},
	},
	"dir_lisa_tariffs": {
		Code: "dir_lisa_tariffs", Name: "Тарифы логистики", Group: GroupLisa, Icon: "lucide:truck",
		Description: "Тарифы логистики / справочник цен перевозки (за единицу, за рейс, ступени объёма). Источник: Лиса. Этапы 1.5, 2.3.",
		Stage: "1.5, 2.3",
		Columns: []DirColumn{
			{Key: "route_code", Label: "Маршрут", Type: ColCode, Primary: true, Search: true},
			{Key: "tier", Label: "Ступень", Type: ColBadge},
			{Key: "unit_price", Label: "Тариф", Type: ColMoney},
		},
	},
	"dir_headcount": {
		Code: "dir_headcount", Name: "Численность", Group: GroupLisa, Icon: "lucide:users",
		Description: "Численность (FTE) по ЦФО/периодам. Источник: Лиса. Для расчётов ФОТ и отчётов на сотрудника.",
		Stage: "2",
		Columns: []DirColumn{
			{Key: "code_cfo", Label: "Код ЦФО", Type: ColNumber, Primary: true, Search: true, Width: "100px"},
			{Key: "period", Label: "Период", Type: ColCode, Filterable: true},
			{Key: "headcount_fte", Label: "FTE", Type: ColNumber},
		},
	},
}

// dirGroupForSource — дефолтная группа по источнику для справочников без схемы.
func dirGroupForSource(source string) DirGroup {
	switch source {
	case "lisa":
		return GroupLisa
	case "1c":
		return Group1C
	case "calculated":
		return GroupCalculated
	default:
		return GroupManual
	}
}

// SchemaFor возвращает схему справочника (или сгенерированную заглушку, если код
// не зарегистрирован — чтобы UI не падал на новом справочнике).
func SchemaFor(code, source string) DirSchema {
	if s, ok := dirSchemas[code]; ok {
		return s
	}
	return DirSchema{Code: code, Name: code, Group: dirGroupForSource(source), Icon: "lucide:table", Columns: []DirColumn{}}
}

// AllSchemas — все зарегистрированные схемы (для отдачи фронту одним вызовом).
func AllSchemas() map[string]DirSchema { return dirSchemas }
