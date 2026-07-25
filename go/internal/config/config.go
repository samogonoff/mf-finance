package config

import (
	"os"
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
	PlansAuditEnabled  bool

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

		PlansMock:          env("PLANS_MOCK", "0") == "1",
		PlansMpFactTable:   env("PLANS_MP_FACT_TABLE", "Budgeting.dbo.FormToLoadFact"),
		PlansMpPlanTable:   env("PLANS_MP_PLAN_TABLE", "Budgeting.dbo.FormToLoadPlan"),
		PlansMpTaktTable:   env("PLANS_MP_TAKT_TABLE", "Budgeting.dbo.FormToLoaTaktTarget"),
		PlansMpPenaltyView: env("PLANS_MP_PENALTIES_VIEW", "FINDWHACCESSGROUP"),
		PlansAuditEnabled:  env("PLANS_AUDIT_ENABLED", "0") == "1",

		LisaHost:          env("FOX_HOST", ""),
		LisaPort:          env("FOX_PORT", "1433"),
		LisaDB:            env("FOX_DB", ""),
		LisaUser:          env("FOX_USER", ""),
		LisaPassword:      env("FOX_PASSWORD", ""),
		LisaMock:          env("LISA_MOCK", "0") == "1",
		PlansSyncInterval: atoiDef(env("PLANS_SYNC_INTERVAL", "3600"), 3600),
		PlansDirCacheTTL:  atoiDef(env("PLANS_DIR_CACHE_TTL", "3600"), 3600),

		B24UserGetWebhook: env("B24_USERGET_WEBHOOK", ""),
	}
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
