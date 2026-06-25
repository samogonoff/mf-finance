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

	// Premaster1C — MSSQL-витрина для отчёта «Задолженность ВГО».
	// На OLAP-сервере 10.10.6.15 это ТАБЛИЦА [FinDWH].[dbo].[Premaster1C]
	// (а не отдельная БД). PremasterDatabase = "FinDWH", PremasterTable = "Premaster1C".
	// Имена вынесены в env, чтобы можно было быстро переключиться на снэпшот
	// (например, [FinDWH].[dbo].[Premaster1C_20260514]) без правки кода.
	PremasterServer        string
	PremasterPort          string
	PremasterDatabase      string
	PremasterSchema        string
	PremasterTable         string
	PremasterObjectsTable  string
	// Counterparty1C — обогащение отчёта именем/каналом/менеджером контрагента.
	// Пусто → не джойнить (имя падает на seed/ИНН как раньше).
	PremasterCounterpartyTable string
	PremasterUser              string
	PremasterPassword          string
	DebtMock                   bool

	// Payments-витрина (опционально) — отдельная БД [Payments] на ТОМ ЖЕ OLAP-сервере.
	// Таблица Docs несёт PaymentDate/Delay → даёт просрочку в drill-down
	// (мост FinDWH↔Payments: Premaster1C.DocID = Payments.dbo.Docs.ID,
	// подтверждён аналитиком, см. docs/reports/debt/payments-source-map.md §5).
	// Джойн опциональный и LEFT: если PaymentsDatabase пуст — drill-down работает
	// как раньше, поля payment_due_date/overdue_days остаются пустыми.
	PremasterPaymentsDatabase string
	PremasterDocsSchema       string
	PremasterDocsTable        string

	// Revenue-оверлей из P&L-матриц (опционально, opt-in). Если включён, при старте
	// читаем [001 Mapping PL by BK] ⋈ [002 CodePL] и дозаполняем классификацию
	// revenue-счетами (GroupPL='ПРОДАЖИ') — закрывает «какие субсчета = выручка»
	// (analyst-handoff §4). При любой ошибке загрузки — фолбэк на хардкод-chart.
	DebtRevenueOverlay      bool
	PremasterMappingPLTable string
	PremasterCodePLTable    string
	PremasterCompaniesTable string

	// DEBT_BACKEND — какой источник дёргает отчёт «Задолженность ВГО».
	//   "mssql" (default) → repo_premaster.go, ходит в Premaster1C напрямую.
	//   "finpl"           → repo_finpl.go, каноническая ОПУ-витрина Table_Fin_PL
	//                       (выручка/ВГО) + Premaster для ДЗ/КЗ/договора/просрочки.
	//                       Реализован, но не дефолт: ждёт сверки на наполненной
	//                       витрине (CHECKPOINT C). Включается явно finpl.
	//   "ch"              → repo_clickhouse.go, ходит в локальный CH-снэпшот.
	// Drilldown в ch-режиме пока не реализован — falls back to mssql.
	DebtBackend       string
	ClickHouseHTTPURL string
	ClickHouseUser    string
	ClickHousePass    string

	// DEBT_CH_SOURCE — какую CH-таблицу читает ch-бэкенд отчёта «Задолженность ВГО».
	//   "premaster" (default) → finance.fact_premaster (текущий, из Premaster1C).
	//   "glmf"                → finance.fact_glmf + dim_contract (поток GLMF, полнее,
	//                           каноничная классификация, договоры отдельным потоком).
	// glmf — opt-in до сверки чисел на наполненном CH (CHECKPOINT B/D, SPEC §10).
	DebtCHSource string

	// Table_Fin_PL — каноническая месячная ОПУ-витрина на том же OLAP (FinDWH.dbo),
	// первоисточник отчёта при DEBT_BACKEND=finpl. Имя таблицы вынесено в env,
	// чтобы переключаться на тестовую копию без правки кода. DebtFinPLMinMonth —
	// нижняя граница периода (раньше неё данных нет): фильтр клампится к ней.
	DebtFinPLTable    string
	DebtFinPLMinMonth string

	// Модуль «Тактические планы» (docs/reports/plans/SPEC.md §10).
	// PlansMock=1 → факт МП из фикстур (sources/mock_mp.go), как DEBT_MOCK.
	// Онлайн-источник факта — тот же сервер FinDWH, что у ВГО-отчёта
	// (переиспользуем MSSQL_PREMASTER_*); PlansMpFactView — имя вьюхи/таблицы
	// факта МП (Источник_МП → ALL_view_МП), уточняется через cmd/mssql-probe.
	PlansMock       bool
	PlansMpFactView string
}

func Load() Config {
	return Config{
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
		PostgresURL: env("POSTGRES_URL", "postgres://finance:finance@postgres:5432/finance?sslmode=disable"),
		RedisAddr:   env("REDIS_ADDR", "redis:6379"),
		CORSOrigins: splitCSV(env("CORS_ORIGINS", "*")),

		PremasterServer:       env("MSSQL_PREMASTER_SERVER", ""),
		PremasterPort:         env("MSSQL_PREMASTER_PORT", "1433"),
		PremasterDatabase:     env("MSSQL_PREMASTER_DB", "FinDWH"),
		PremasterSchema:       env("MSSQL_PREMASTER_SCHEMA", "dbo"),
		PremasterTable:        env("MSSQL_PREMASTER_TABLE", "Premaster1C"),
		PremasterObjectsTable:      env("MSSQL_PREMASTER_OBJECTS_TABLE", "Objects"),
		PremasterCounterpartyTable: env("MSSQL_PREMASTER_COUNTERPARTY_TABLE", "Counterparty1C"),
		PremasterUser:              env("MSSQL_PREMASTER_USER", ""),
		PremasterPassword:          env("MSSQL_PREMASTER_PASSWORD", ""),
		DebtMock:                   env("DEBT_MOCK", "0") == "1",

		PremasterPaymentsDatabase: env("MSSQL_PAYMENTS_DB", "Payments"),
		PremasterDocsSchema:       env("MSSQL_PAYMENTS_DOCS_SCHEMA", "dbo"),
		PremasterDocsTable:        env("MSSQL_PAYMENTS_DOCS_TABLE", "Docs"),

		DebtRevenueOverlay:      env("DEBT_REVENUE_OVERLAY", "0") == "1",
		PremasterMappingPLTable: env("MSSQL_PREMASTER_MAPPING_PL_TABLE", "001 Mapping PL by BK"),
		PremasterCodePLTable:    env("MSSQL_PREMASTER_CODEPL_TABLE", "002 CodePL"),
		PremasterCompaniesTable: env("MSSQL_PREMASTER_COMPANIES_TABLE", "CompaniesMF"),

		DebtBackend:       strings.ToLower(env("DEBT_BACKEND", "mssql")),
		ClickHouseHTTPURL: env("CLICKHOUSE_HTTP_URL", "http://clickhouse:8123"),
		ClickHouseUser:    env("CLICKHOUSE_USER", "finance"),
		ClickHousePass:    env("CLICKHOUSE_PASSWORD", "finance"),

		DebtFinPLTable:    env("MSSQL_FINPL_TABLE", "Table_Fin_PL"),
		DebtFinPLMinMonth: env("DEBT_FINPL_MIN_MONTH", "2025-01-01"),

		DebtCHSource: strings.ToLower(env("DEBT_CH_SOURCE", "premaster")),

		PlansMock:       env("PLANS_MOCK", "0") == "1",
		PlansMpFactView: env("PLANS_MP_FACT_VIEW", "ALL_view_МП"),
	}
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
