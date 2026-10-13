package config

import (
	"net"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr    string
	PostgresURL string
	RedisAddr   string
	CORSOrigins []string

	// OLAP-сервер (10.10.6.15): на нём живут и FinDWH (Premaster — для модуля
	// «Тактические планы»), и БД Payments с вьюхами FinDebt (отчёт «Задолженность
	// ВГО»). Учётки одни на весь сервер. PremasterDatabase = "FinDWH".
	PremasterServer   string
	PremasterPort     string
	PremasterDatabase string
	PremasterUser     string
	PremasterPassword string
	DebtMock          bool

	// БД Payments на том же OLAP — там лежат вьюхи FinDebt (схема report).
	PremasterPaymentsDatabase string

	// DEBT_BACKEND — источник отчёта «Задолженность ВГО». Единственный поток —
	// готовый расчётный слой FinDebt (Payments.report.FinDebt1/3), сверенный с 1С
	// копейка-в-копейку (docs/reports/debt/findebt-verification.md).
	//   "findebt" (default) → отчёт читает CH finance.fact_findebt/_docs
	//                         (залито cmd/findebt-etl из FinDebt-вьюх).
	//   "findebt-live"      → прямое чтение FinDebt-вьюх из MSSQL (фолбэк/дебаг).
	//   "findebt-docdate"   → второй поток: та же свёртка, но BYN/USD пересчитаны
	//                         на дату документа (Doc_Date) через dim_valuta +
	//                         currency_daily (ASOF). Для сверки с findebt.
	DebtBackend       string
	ClickHouseHTTPURL string
	ClickHouseUser    string
	ClickHousePass    string

	// Пересчёт валют на Doc_Date (второй поток findebt-docdate). FQN linked-server
	// справочников курса на OLAP; настраиваемы, т.к. доступность [SRV-SQL] — вопрос
	// интеграции. CurrencySyncInterval — период фонового обновления курсов (сек),
	// 0 → воркер выключен (заливаем вручную через cmd/findebt-etl MODE=currency).
	DebtValutaFQN        string
	DebtCurrencyDailyFQN string
	CurrencySyncInterval int

	// Сырые таблицы метода аналитика (findebt-docdate): Debt_arh (остаток) и
	// Wholesales_arh (движения). DebtArhCpartyCol — имя колонки УНП контрагента,
	// если она есть в источнике (пусто → контрагент только по Name, без УНП в ключе).
	// DebtArhSyncInterval — период фонового reload'а debt_facts/turnover_facts (сек).
	DebtArhFQN          string
	WholesalesArhFQN    string
	DebtArhCpartyCol    string
	DebtArhSyncInterval int

	// FinDebt-вьюхи в БД Payments (PremasterPaymentsDatabase), схема report.
	// FinDebt1 — свод остатков ДЗ/КЗ, FinDebt3 — документная детализация с
	// просрочкой. Имена в env, чтобы переключаться на копию без правки кода.
	DebtFinDebtSchema string
	DebtFinDebt1Table string
	DebtFinDebt3Table string
	// FINDEBT_SYNC_INTERVAL — период фонового инкремента FinDebt → CH (секунды).
	// 0 → воркер выключен (dev). Прод: несколько часов (вьюхи суточные).
	DebtFinDebtSyncInterval int

	// Модуль «Тактические планы» (docs/reports/plans/SPEC.md §10).
	// PlansMock=1 → факт МП из фикстур (sources/mock_mp.go), как DEBT_MOCK.
	// Онлайн-источник — тот же сервер FinDWH (переиспользуем MSSQL_PREMASTER_*).
	// Факт/план МП живут в БД Budgeting (плоские таблицы FormToLoad*, значения в BYN);
	// штрафы — в FinDWH.dbo.FINDWHACCESSGROUP. Подтверждено probe'ом 2026-07-05.
	PlansMock          bool
	PlansMpFactTable   string // факт МП: Budgeting.dbo.FormToLoadFact (КодЦФО/КодPL/Дата/Значение, BYN)
	PlansMpPlanTable   string // план/стратегия МП: Budgeting.dbo.FormToLoadPlan
	PlansMpTaktTable   string // тактика-таргеты МП: Budgeting.dbo.FormToLoaTaktTarget (2025-01…2026-12)
	PlansMpPenaltyView string // вью штрафов МП (FINDWHACCESSGROUP, Наименование LIKE '%Штраф%')
	// PlansMpArticleFactTable — факт по статьям прямых затрат площадок для
	// первичного заполнения долей (ОПУ-витрина, сценарий «факт»). Пусто → mock.
	PlansMpArticleFactTable string
	// PlansMpStrategyScenario — сценарий витрины, из которого берутся
	// стратегические доли статей по умолчанию (ТЗ доработок п. 2.1).
	PlansMpStrategyScenario string
	PlansAuditEnabled       bool
	// PlansLegacyMpAPI=1 возвращает к жизни устаревшую ветку /api/plans/mp/{form,
	// compute,copy,formula,export,import} — форма из двух editable-строк (1046/8006)
	// со своим движком calc.go. Актуальная форма живёт на /api/plans/tasks/{id}/mp-form.
	// По умолчанию 0 → эти ручки отвечают 410 Gone (заморожены до удаления).
	PlansLegacyMpAPI bool
	// Публикация утверждённого плана в приёмники Budgeting (ТЗ МП §9.3 / Розница §7.2).
	// PlansPublishEnabled=0 (дефолт) → доступен только dry-run: сервис считает, что
	// ушло бы, и сверяет с приёмником. Включать после ответов BI по §12 (какой
	// «Параметр», агрегат vs детализация, BYN-пара, уникальный индекс приёмника).
	// PlansPublishTargets — белый список таблиц: имя приёмника приходит из данных
	// маппинга, поэтому произвольная таблица записи недопустима.
	// PlansPublishSandbox=1 → при недоступном приёмнике (нет MSSQL / выключен
	// PlansPublishEnabled) запись идёт в PG-песочницу publish_sandbox тем же
	// алгоритмом (DELETE+INSERT в транзакции, обратное чтение суммы, контроль
	// расхождения, идемпотентность). Нужна, чтобы путь записи можно было проверить
	// целиком без Budgeting. ТОЛЬКО dev/тест: карточка получает статус published,
	// хотя в приёмнике ничего нет. Прод-приёмник всегда приоритетнее песочницы.
	PlansPublishEnabled bool
	PlansPublishSandbox bool
	PlansPublishTargets []string

	// Справочник статей PL (Budgeting.dbo.CodePL, 423 записи) — источник подписей,
	// НОРМАТИВНОГО НДС по странам (колонки VAT_*) и статьи ДДС. Пусто или нет
	// MSSQL → справочник остаётся на сиде PLLineSeed (нормативная проверка НДС
	// работает только по статьям сида).
	PlansCodePLTable string

	// Подсказки формы МП (правки ТЗ 02.09.2026, часть 1 и МП §9/§14). Значения в
	// форму НЕ подставляются — показываются рядом с полем ввода. Пусто или нет
	// MSSQL → подсказок не будет, поля остаются полями ввода.
	// Снимок факта МП (правка ТЗ 02.09.2026 МП §12). 1 (дефолт) → форма читает
	// таблицу снимка с отметкой «данные актуальны на …»; живое чтение OLAP
	// запрещено, потому что таблицы источников перезаливаются через TRUNCATE и
	// суммы плавают в динамике. 0 — аварийный откат на прямое чтение.
	// Интервал регламента в секундах, дефолт сутки.
	PlansFactSnapshot         bool
	PlansFactSnapshotInterval int

	PlansMpSppTable     string // MP.dbo.WB_Price_SPP_Analysis (spp + цена)
	PlansMpSalesTable   string // MP.dbo.sales (веса SALES/SALES_RUB, ключ EAN)
	PlansMpPenaltyTable string // DWH.dbo.wb_dimensions_penalty (НЕ копия в MP)

	// PlansSubmitStrict — жёсткий режим гейта отправки форм (ответ аналитика 41
	// от 10.09.2026): замечания и несошедшиеся сверки БЛОКИРУЮТ отправку на
	// согласование. По умолчанию выключено — мягкий режим, в котором проверки
	// считаются и показываются, но процесс не держат. Включать к промышленной
	// эксплуатации.
	PlansSubmitStrict bool
	// Four-country retail gate is enabled unless explicitly disabled for a test run.
	PlansRetailCountryGate bool

	// Форма «Расходы ЦП» (TPL-CFO-EXP, этап 1.5). Источники — тот же OLAP,
	// что у остальных форм модуля. Пусто или PlansMock=1 → фикстуры exp_mock.go.
	PlansExpCostsProc string // процедура затрат: ZATRATY_DLYA_OKSANY_BYSTRYE (база BUDGETING)
	PlansExpCfoTable  string // справочник ЦФО этапа 1.5: FinDWH.dbo.[001 CodeCFO]

	// Форма «Розница» (TPL-TO-RETAIL, ТЗ Розница §11). Источники — тот же сервер
	// FinDWH/Budgeting/Checks, что у МП; при PlansMock=1 или без MSSQL работают
	// фикстуры (plans/retail_mock.go), форма и тесты не зависят от VPN.
	PlansRetailStoreTable          string // справочник магазинов: FinDWH.dbo.[001 CodeCFO]
	PlansRetailStoreGroup          string // значение GroupCFO1 справочника ('Магазины')
	PlansRetailFactTable           string // факт продаж (выручка с НДС) из FOX
	PlansRetailNewStrategyTable    string // необязательный источник НовСтрат с полем Scenario
	PlansRetailComparisonFactTable string // факт Budgeting в национальной валюте
	PlansRetailApprovedTable       string // утверждённая тактика, национальная валюта
	PlansRetailStrategyTable       string // стратегия в нац. валюте: Budgeting.dbo.VFORMTOLOADPLAN
	PlansRetailStrategyGroup       string // ГруппыЦФО1 таблицы плана ('3.Магазины' — НЕ равно группе справочника)
	PlansRetailPlanHistTable       string // история плана: Checks.dbo.plan_saler_st
	PlansRetailPlanHistNew         string // история плана новых магазинов: Checks.dbo.plan_saler_st_new_stores
	// PlansRetailValueLimits — верхняя граница значения ячейки по стране (V-02),
	// формат «BY=5000000,RU=50000000». Страна без записи → границы нет.
	PlansRetailValueLimits map[string]float64
	// PlansRetailReconBlocking — запрещать ли отправку формы розницы, когда
	// контрольная сверка V-05 не сошлась. По умолчанию НЕТ (решение 08.09.2026):
	// расхождение остаётся предупреждением в отчёте, но не держит работу над
	// формой, пока причину ищут в данных факта.
	PlansRetailReconBlocking bool

	// Справочники Лисы (ТЗ §«Справочники из Лисы»): MSSQL-БД Gpartner (FOX_*).
	// LisaMock=1 → синхронизация из фикстур (как PLANS_MOCK), без сети к FOX.
	// PlansSyncInterval — период cron-синхронизации; PlansDirCacheTTL — дефолтный
	// TTL Redis-кэша строк справочника (переопределяется per-dir в БД).
	LisaHost          string
	LisaPort          string
	LisaDB            string
	LisaUser          string
	LisaPassword      string
	LisaMock          bool
	PlansSyncInterval int // секунды
	PlansDirCacheTTL  int // секунды

	// B24 inbound-вебхук с правом user.get — для админ-импорта пользователей по ID
	// (догрузка сотрудников, ещё не заходивших). Пусто → импорт отдаёт 503.
	B24UserGetWebhook string

	// Трансляция структурных логов (slog JSON) в Logstash по TCP (кодек
	// json_lines). Собирается из LOGSTASH_HOST + LOGSTASH_PORT. Пусто в HOST →
	// пустой адрес → трансляция выключена, остаётся только stdout
	// (см. internal/logship). Порт по умолчанию — 5044.
	LogstashAddr string
}

func Load() Config {
	return Config{
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
		PostgresURL: env("POSTGRES_URL", "postgres://finance:finance@postgres:5432/finance?sslmode=disable"),
		RedisAddr:   env("REDIS_ADDR", "redis:6379"),
		CORSOrigins: splitCSV(env("CORS_ORIGINS", "*")),

		PremasterServer:   env("MSSQL_PREMASTER_SERVER", ""),
		PremasterPort:     env("MSSQL_PREMASTER_PORT", "1433"),
		PremasterDatabase: env("MSSQL_PREMASTER_DB", "FinDWH"),
		PremasterUser:     env("MSSQL_PREMASTER_USER", ""),
		PremasterPassword: env("MSSQL_PREMASTER_PASSWORD", ""),
		DebtMock:          env("DEBT_MOCK", "0") == "1",

		PremasterPaymentsDatabase: env("MSSQL_PAYMENTS_DB", "Payments"),

		DebtBackend:       strings.ToLower(env("DEBT_BACKEND", "findebt")),
		ClickHouseHTTPURL: env("CLICKHOUSE_HTTP_URL", "http://clickhouse:8123"),
		ClickHouseUser:    env("CLICKHOUSE_USER", "finance"),
		ClickHousePass:    env("CLICKHOUSE_PASSWORD", "finance"),

		DebtFinDebtSchema:       env("MSSQL_FINDEBT_SCHEMA", "report"),
		DebtFinDebt1Table:       env("MSSQL_FINDEBT1_TABLE", "FinDebt1"),
		DebtFinDebt3Table:       env("MSSQL_FINDEBT3_TABLE", "FinDebt3"),
		DebtFinDebtSyncInterval: atoiDef(env("FINDEBT_SYNC_INTERVAL", "0"), 0),

		DebtValutaFQN:        env("MSSQL_VALUTA_FQN", "[SRV-SQL].Gpartner.dbo.valuta"),
		DebtCurrencyDailyFQN: env("MSSQL_CURRENCY_DAILY_FQN", "[SRV-SQL].Checks.dbo.CurrencyDaily"),
		CurrencySyncInterval: atoiDef(env("CURRENCY_SYNC_INTERVAL", "0"), 0),

		DebtArhFQN:          env("MSSQL_DEBT_ARH_FQN", "[Payments].[dbo].[Debt_arh]"),
		WholesalesArhFQN:    env("MSSQL_WHOLESALES_ARH_FQN", "[Payments].[dbo].[Wholesales_arh]"),
		DebtArhCpartyCol:    env("MSSQL_DEBT_ARH_CPARTY_COL", ""),
		DebtArhSyncInterval: atoiDef(env("DEBTARH_SYNC_INTERVAL", "0"), 0),

		PlansMock:                 env("PLANS_MOCK", "0") == "1",
		PlansMpFactTable:          env("PLANS_MP_FACT_TABLE", "Budgeting.dbo.FormToLoadFact"),
		PlansMpPlanTable:          env("PLANS_MP_PLAN_TABLE", "Budgeting.dbo.FormToLoadPlan"),
		PlansMpTaktTable:          env("PLANS_MP_TAKT_TABLE", "Budgeting.dbo.FormToLoaTaktTarget"),
		PlansMpPenaltyView:        env("PLANS_MP_PENALTIES_VIEW", "FINDWHACCESSGROUP"),
		PlansMpArticleFactTable:   env("PLANS_MP_ARTICLE_FACT_TABLE", "FinDWH.dbo.Table_Fin_PL"),
		PlansMpStrategyScenario:   env("PLANS_MP_STRATEGY_SCENARIO", "НовСтрат"),
		PlansAuditEnabled:         env("PLANS_AUDIT_ENABLED", "0") == "1",
		PlansLegacyMpAPI:          env("PLANS_LEGACY_MP_API", "0") == "1",
		PlansPublishEnabled:       env("PLANS_PUBLISH_ENABLED", "0") == "1",
		PlansPublishSandbox:       env("PLANS_PUBLISH_SANDBOX", "0") == "1",
		PlansCodePLTable:          env("PLANS_CODEPL_TABLE", "Budgeting.dbo.CodePL"),
		PlansFactSnapshot:         env("PLANS_FACT_SNAPSHOT", "1") == "1",
		PlansFactSnapshotInterval: atoiDef(env("PLANS_FACT_SNAPSHOT_INTERVAL", "86400"), 86400),

		PlansMpSppTable:     env("PLANS_MP_SPP_TABLE", "MP.dbo.WB_Price_SPP_Analysis"),
		PlansMpSalesTable:   env("PLANS_MP_SALES_TABLE", "MP.dbo.sales"),
		PlansMpPenaltyTable: env("PLANS_MP_PENALTY_TABLE", "DWH.dbo.wb_dimensions_penalty"),
		PlansPublishTargets: splitCSV(env("PLANS_PUBLISH_TARGETS",
			"Budgeting.dbo.VFORMTOLOADTAKTTARGET,Budgeting.dbo.FormToLoaTaktTarget")),

		PlansSubmitStrict:      env("PLANS_SUBMIT_STRICT", "") == "1",
		PlansRetailCountryGate: env("PLANS_RETAIL_COUNTRY_GATE", "1") != "0",

		PlansExpCostsProc: env("PLANS_EXP_COSTS_PROC", "ZATRATY_DLYA_OKSANY_BYSTRYE"),
		PlansExpCfoTable:  env("PLANS_EXP_CFO_TABLE", "FinDWH.dbo.[001 CodeCFO]"),

		PlansRetailStoreTable:          env("PLANS_RETAIL_STORE_TABLE", "FinDWH.dbo.[001 CodeCFO]"),
		PlansRetailStoreGroup:          env("PLANS_RETAIL_STORE_GROUP", "Магазины"),
		PlansRetailFactTable:           env("PLANS_RETAIL_FACT_TABLE", "FinDWH.dbo.sales_and_COGG_from_FOX_offline_retail"),
		PlansRetailNewStrategyTable:    env("PLANS_RETAIL_NEW_STRATEGY_TABLE", ""),
		PlansRetailComparisonFactTable: env("PLANS_RETAIL_COMPARISON_FACT_TABLE", "Budgeting.dbo.VFORMTOLOADFACT"),
		PlansRetailApprovedTable:       env("PLANS_RETAIL_APPROVED_TABLE", "Budgeting.dbo.VFORMTOLOADTAKTTARGET"),
		PlansRetailStrategyTable:       env("PLANS_RETAIL_STRATEGY_TABLE", "Budgeting.dbo.VFORMTOLOADPLAN"),
		PlansRetailStrategyGroup:       env("PLANS_RETAIL_STRATEGY_GROUP", "3.Магазины"),
		PlansRetailPlanHistTable:       env("PLANS_RETAIL_PLAN_HISTORY_TABLE", "Checks.dbo.plan_saler_st"),
		PlansRetailPlanHistNew:         env("PLANS_RETAIL_PLAN_HISTORY_NEW_TABLE", "Checks.dbo.plan_saler_st_new_stores"),
		PlansRetailValueLimits:         parseCountryLimits(env("PLANS_RETAIL_VALUE_LIMITS", "")),
		PlansRetailReconBlocking:       env("PLANS_RETAIL_RECON_BLOCKING", "") == "1",

		LisaHost:          env("FOX_HOST", ""),
		LisaPort:          env("FOX_PORT", "1433"),
		LisaDB:            env("FOX_DB", ""),
		LisaUser:          env("FOX_USER", ""),
		LisaPassword:      env("FOX_PASSWORD", ""),
		LisaMock:          env("LISA_MOCK", "0") == "1",
		PlansSyncInterval: atoiDef(env("PLANS_SYNC_INTERVAL", "3600"), 3600),
		PlansDirCacheTTL:  atoiDef(env("PLANS_DIR_CACHE_TTL", "3600"), 3600),

		B24UserGetWebhook: env("B24_USERGET_WEBHOOK", ""),

		LogstashAddr: hostPort(env("LOGSTASH_HOST", ""), env("LOGSTASH_PORT", "")),
	}
}

// hostPort склеивает host:port для Logstash. Пустой host → "" (трансляция
// выключена), пустой порт при заданном хосте → 5044 (дефолт TCP-input'а).
func hostPort(host, port string) string {
	if host == "" {
		return ""
	}
	if port == "" {
		port = "5044"
	}
	return net.JoinHostPort(host, port)
}

func atoiDef(s string, def int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return def
	}
	return n
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// parseCountryLimits — «BY=5000000,RU=50000000» → map[страна]граница (V-02
// формы «Розница»). Нераспознанная запись игнорируется: неверная граница не
// должна ронять старт сервиса, отсутствие границы — это просто «без границы».
func parseCountryLimits(s string) map[string]float64 {
	out := map[string]float64{}
	for _, part := range splitCSV(s) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64)
		if err != nil || v <= 0 {
			continue
		}
		out[strings.ToUpper(strings.TrimSpace(kv[0]))] = v
	}
	return out
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
