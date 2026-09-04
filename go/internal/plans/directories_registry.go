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
			{Key: "short", Label: "Код", Type: ColCode, Width: "80px", Hint: "F / TDMF / MFKaz / MFUz"},
			// УНП — то, что реально пишется в приёмник (правка Розница §15):
			// план идёт на реальное ЮЛ, а не на фиктивный код.
			{Key: "unp", Label: "УНП", Type: ColCode, Search: true, Width: "130px"},
			{Key: "country", Label: "Страна", Type: ColCountry, Filterable: true, BadgeTones: countryTones, Ref: "dir_country"},
			{Key: "director", Label: "Директор", Type: ColText, Search: true, Hint: "согласующий ЮЛ (этап 3)", Ref: "user"},
			{Key: "historical_only", Label: "Только история", Type: ColBool, Filterable: true,
				Hint: "фиктивные коды (1 BY, 2 RU, 4 CN, 7 KZ, 8 UZ, 9 TR) — читаем исторические строки, на запись не берём"},
			{Key: "alias_raw", Label: "Сырые алиасы (xlsx)", Type: ColText},
			{Key: "uncertain", Label: "Сверить", Type: ColBool, Filterable: true},
			{Key: "note", Label: "Примечание", Type: ColText},
		},
	},
	"dir_scenario": {
		Code: "dir_scenario", Name: "Сценарии", Group: GroupManual, Icon: "lucide:git-branch",
		Description: "Сценарии планирования и факта, как они лежат в приёмниках. Их ВОСЕМЬ (правка ТЗ Розница §23): −1 и 0 — факт, 1/2/3 — бюджеты, 9/10 — ожидаемый факт, 11 — тактика (в этом сценарии работают наши формы). Форма обязана понимать любое значение, которое встретит в приёмнике.",
		Stage: "все",
		Columns: []DirColumn{
			{Key: "code", Label: "Код", Type: ColNumber, Primary: true, Search: true, Width: "70px"},
			{Key: "name", Label: "Сценарий", Type: ColText, Search: true},
			{Key: "kind", Label: "Тип", Type: ColBadge, Filterable: true,
				BadgeTones: map[string]string{"fact": "muted", "budget": "info", "expected": "warn", "tactic": "pos"}},
		},
	},
	"dir_retail_scope": {
		Code: "dir_retail_scope", Name: "Состав розницы", Group: GroupManual, Icon: "lucide:filter",
		Description: "ПРАВИЛО, что считается розницей (правка ТЗ Розница §11, по ALL_view): группа ЦФО «3.Магазины» плюс восемь ЦФО-отделов, минус ЦФО 40 (интернет-магазин) и 722 (техническая запись переезда магазина в Солигорске). Правило живёт строками, поэтому отдел добавляется без релиза. Исключение сильнее включения.",
		Stage: "1.1",
		Columns: []DirColumn{
			{Key: "kind", Label: "Правило", Type: ColBadge, Primary: true, Filterable: true,
				BadgeTones: map[string]string{"include_group": "pos", "include_cfo": "info", "exclude_cfo": "neg"}},
			{Key: "value", Label: "Значение", Type: ColCode, Search: true},
			{Key: "note", Label: "Почему", Type: ColText, Search: true},
		},
	},
	"dir_mp_spp_hint": {
		Code: "dir_mp_spp_hint", Name: "Подсказка %СПП", Group: Group1C, Icon: "lucide:percent",
		Description: "Фактический %СПП площадки за прошлый месяц — ПОДСКАЗКА к полю ввода, а не значение формы (правки ТЗ 02.09.2026, часть 1 и МП §8/§9). Считается взвешенным средним SUM(spp × SALES_RUB)/SUM(SALES_RUB) по MP.dbo.WB_Price_SPP_Analysis и весам MP.dbo.sales (ключ EAN); spp вне 0…100 отбрасывается. Регламент раз в сутки: в истории 112 млн строк.",
		Stage: "1.1",
		Columns: []DirColumn{
			{Key: "code_cfo", Label: "Код ЦФО", Type: ColCode, Primary: true, Search: true, Width: "90px"},
			{Key: "spp_pct", Label: "%СПП (факт)", Type: ColNumber, Hint: "доля 0…1"},
			{Key: "as_of", Label: "Период", Type: ColCode, Filterable: true},
			{Key: "source", Label: "Источник", Type: ColText},
			{Key: "note", Label: "Примечание", Type: ColText},
		},
	},
	"dir_mp_penalty_hint": {
		Code: "dir_mp_penalty_hint", Name: "Подсказка по штрафам", Group: Group1C, Icon: "lucide:gavel",
		Description: "Габаритные штрафы Wildberries за прошлый месяц из DWH.dbo.wb_dimensions_penalty — ПОДСКАЗКА к полю ввода (правка ТЗ МП §14). Это ЧАСТЬ штрафов, а не итог: общего реестра штрафов в базах нет, поэтому в форме штрафы вводятся руками. Копия в MP не используется — она встала 24.04.2026.",
		Stage: "1.1",
		Columns: []DirColumn{
			{Key: "code_cfo", Label: "Код ЦФО", Type: ColCode, Primary: true, Search: true, Width: "90px"},
			{Key: "amount", Label: "Штрафы (факт)", Type: ColMoney},
			{Key: "as_of", Label: "Период", Type: ColCode, Filterable: true},
			{Key: "source", Label: "Источник", Type: ColText},
			{Key: "note", Label: "Примечание", Type: ColText},
		},
	},
	"dir_pl_line": {
		Code: "dir_pl_line", Name: "Статьи PL", Group: GroupManual, Icon: "lucide:list-tree",
		Description: "Статьи отчёта о прибылях/убытках (Budgeting.dbo.CodePL, 423 записи). Кроме подписи несут НОРМАТИВНЫЙ НДС по странам, статью ДДС и флаг особого доступа. НДС хранится коэффициентом «1 + ставка» как в источнике: 1,00 = налог по статье не начисляется.",
		Stage: "1.5, 2.3",
		Columns: []DirColumn{
			{Key: "code_pl", Label: "Код PL", Type: ColNumber, Primary: true, Search: true, Width: "90px"},
			{Key: "expense_name", Label: "Статья", Type: ColText, Search: true},
			{Key: "group_pl", Label: "Группа PL", Type: ColBadge, Filterable: true},
			{Key: "block_type", Label: "Тип блока", Type: ColCode},
			// Пять колонок НДС. В ТЗ они были описаны как «коэффициенты индексации»
			// — это оказалось неверно (правка МП §18), индексации в справочнике нет.
			{Key: "vat_by", Label: "НДС BY", Type: ColNumber, Width: "80px", Hint: "коэффициент 1+ставка; 1,20 = 20 %, 1,00 = не начисляется"},
			{Key: "vat_ru", Label: "НДС RU", Type: ColNumber, Width: "80px", Hint: "коэффициент 1+ставка; 1,22 = 22 %"},
			{Key: "vat_kz", Label: "НДС KZ", Type: ColNumber, Width: "80px", Hint: "коэффициент 1+ставка; 1,16 = 16 %"},
			{Key: "vat_uz", Label: "НДС UZ", Type: ColNumber, Width: "80px", Hint: "коэффициент 1+ставка; 1,12 = 12 %"},
			{Key: "vat_tr", Label: "НДС TR", Type: ColNumber, Width: "80px", Hint: "в справочнике всегда 1,00 — НДС не применяется"},
			{Key: "cf_item", Label: "Статья ДДС", Type: ColCode, Filterable: true, Hint: "значения не только числовые: «инвест» — вложения, «-99999» — ВГО-обороты"},
			{Key: "special_access", Label: "Особый доступ", Type: ColNumber, Width: "90px", Hint: "флаг 0/1, тот же, что выбирается в ALL_view"},
			{Key: "note", Label: "Примечание", Type: ColText},
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
