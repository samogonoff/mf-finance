package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/company/finance-api/internal/auth"
	"github.com/company/finance-api/internal/bugtracker"
	"github.com/company/finance-api/internal/config"
	"github.com/company/finance-api/internal/db"
	"github.com/company/finance-api/internal/etl"
	"github.com/company/finance-api/internal/internalapi"
	"github.com/company/finance-api/internal/notifications"
	"github.com/company/finance-api/internal/plans"
	"github.com/company/finance-api/internal/redisx"
	"github.com/company/finance-api/internal/reports/debt"
	"github.com/company/finance-api/internal/users"
)

func main() {
	cfg := config.Load()

	// Логи: JSON в stdout + (если задан LOGSTASH_HOST) дубль в Logstash/ELK.
	// Отправка асинхронная и никогда не блокирует обработку запросов.
	logShipper := setupLogging(cfg.LogstashAddr)
	defer logShipper.Close()

	pool, err := db.Open(context.Background(), cfg.PostgresURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	rdb := redisx.Open(cfg.RedisAddr)
	defer rdb.Close()

	userRepo := auth.NewUserRepo(pool)
	tokens := auth.NewTokenRepo(rdb)
	authSvc := auth.NewService(userRepo, tokens)
	authH := auth.NewHandler(authSvc)
	technicalGate, err := auth.NewTechnicalReadonlyGate(userRepo.ExistingByEmail)
	if err != nil {
		// The embedded registry contains public digests only. Fail closed without
		// ever echoing credential material into logs.
		log.Fatal("invalid technical read-only configuration")
	}
	technicalRead := func(pattern, handler string, h http.HandlerFunc) http.HandlerFunc {
		return auth.TechnicalReadHandler(pattern, handler, h)
	}

	// Notifications: репозиторий → сервис (с фоновой доставкой в Б24 при
	// заданном B24_NOTIFY_WEBHOOK_URL) → handler. Welcome-уведомление
	// вешаем на пост-логин-хук auth.Service.
	notifRepo := notifications.NewRepo(pool)
	notifSvc := notifications.NewService(notifRepo, userRepo)
	notifH := notifications.NewHandler(notifSvc, notifRepo)
	authSvc.SetPostLoginHook(func(ctx context.Context, u *auth.User) {
		if err := notifSvc.CreateWelcome(ctx, u); err != nil {
			log.Printf("notifications: welcome failed for user %d: %v", u.ID, err)
		}
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Public auth
	mux.HandleFunc("POST /api/auth/b24/callback", authH.B24Callback)
	mux.HandleFunc("POST /api/auth/refresh", authH.Refresh)

	// Authenticated
	mux.HandleFunc("GET /api/auth/me", technicalRead("GET /api/auth/me", "auth.Me", auth.RequireBearer(authSvc, authH.Me)))
	// Гейт обособленных контуров: зовёт nginx (auth_request), не браузер.
	mux.HandleFunc("GET /api/auth/check", auth.RequireBearer(authSvc, authH.Check))
	mux.HandleFunc("POST /api/auth/logout", auth.RequireBearer(authSvc, authH.Logout))

	// Admin — управление пользователями (ROLE_ADMIN).
	usersH := users.NewHandler(userRepo, authSvc, tokens)
	mux.HandleFunc("GET /api/admin/users", auth.RequireRole(authSvc, auth.RoleAdmin, usersH.List))
	mux.HandleFunc("GET /api/admin/users/roles", auth.RequireRole(authSvc, auth.RoleAdmin, usersH.AllowedRoles))
	mux.HandleFunc("POST /api/admin/users/{id}/block", auth.RequireRole(authSvc, auth.RoleAdmin, usersH.Block))
	mux.HandleFunc("POST /api/admin/users/{id}/unblock", auth.RequireRole(authSvc, auth.RoleAdmin, usersH.Unblock))
	mux.HandleFunc("PUT /api/admin/users/{id}/roles", auth.RequireRole(authSvc, auth.RoleAdmin, usersH.UpdateRoles))

	// Notifications (один общий поток).
	mux.HandleFunc("GET /api/notifications", auth.RequireBearer(authSvc, notifH.List))
	mux.HandleFunc("GET /api/notifications/unread-count", auth.RequireBearer(authSvc, notifH.UnreadCount))
	mux.HandleFunc("POST /api/notifications/{id}/read", auth.RequireBearer(authSvc, notifH.Read))
	mux.HandleFunc("POST /api/notifications/read-all", auth.RequireBearer(authSvc, notifH.ReadAll))
	mux.HandleFunc("GET /api/account/notification-settings", auth.RequireBearer(authSvc, notifH.GetAccountSettings))
	mux.HandleFunc("PATCH /api/account/notification-settings", auth.RequireBearer(authSvc, notifH.PatchAccountSettings(userRepo)))

	// Internal API (для python-cost и других сервисов контура).
	internalH := internalapi.NewHandler(userRepo, notifSvc)
	_ = internalH.Verify(context.Background())
	mux.HandleFunc("POST /internal/notifications", internalH.RequireToken(internalH.Notify))

	// Bug tracker.
	uploadsDir := os.Getenv("BUGTRACKER_UPLOADS_DIR")
	bugStorage := bugtracker.NewStorage(uploadsDir)
	bugRepo := bugtracker.NewRepo(pool)
	bugSvc := bugtracker.NewService(bugRepo, bugStorage, notifSvc)
	bugH := bugtracker.NewHandler(bugSvc, bugRepo, bugStorage)
	mux.HandleFunc("POST /api/bugtracker/report", auth.RequireBearer(authSvc, bugH.CreateReport))
	// Admin (ROLE_ADMIN).
	mux.HandleFunc("GET /api/bugtracker/list", auth.RequireRole(authSvc, auth.RoleAdmin, bugH.AdminList))
	mux.HandleFunc("GET /api/bugtracker/report/{id}", auth.RequireRole(authSvc, auth.RoleAdmin, bugH.AdminGet))
	mux.HandleFunc("PATCH /api/bugtracker/report/{id}", auth.RequireRole(authSvc, auth.RoleAdmin, bugH.AdminPatch))
	mux.HandleFunc("DELETE /api/bugtracker/report/{id}", auth.RequireRole(authSvc, auth.RoleAdmin, bugH.AdminDelete))
	mux.HandleFunc("GET /api/bugtracker/metrics", auth.RequireRole(authSvc, auth.RoleAdmin, bugH.AdminMetrics))
	mux.HandleFunc("GET /api/bugtracker/sources", auth.RequireRole(authSvc, auth.RoleAdmin, bugH.SourcesList))
	mux.HandleFunc("POST /api/bugtracker/sources", auth.RequireRole(authSvc, auth.RoleAdmin, bugH.SourcesCreate))
	mux.HandleFunc("PATCH /api/bugtracker/sources/{id}", auth.RequireRole(authSvc, auth.RoleAdmin, bugH.SourcesPatch))
	// Сервисный API разбора багов автоматизацией (Orca/Hermes) — за X-Internal-Token.
	mux.HandleFunc("GET /internal/bugtracker/queue", internalH.RequireToken(bugH.InternalQueue))
	mux.HandleFunc("GET /internal/bugtracker/sources", internalH.RequireToken(bugH.InternalSources))
	mux.HandleFunc("GET /internal/bugtracker/pending-mr", internalH.RequireToken(bugH.InternalPendingMR))
	mux.HandleFunc("GET /internal/bugtracker/by-mr/{iid}", internalH.RequireToken(bugH.InternalByMR))
	mux.HandleFunc("POST /internal/bugtracker/report", internalH.RequireToken(bugH.InternalCreate))
	mux.HandleFunc("GET /internal/bugtracker/report/{id}", internalH.RequireToken(bugH.InternalGet))
	mux.HandleFunc("POST /internal/bugtracker/report/{id}/claim", internalH.RequireToken(bugH.InternalClaim))
	mux.HandleFunc("POST /internal/bugtracker/report/{id}/mr", internalH.RequireToken(bugH.InternalMR))
	mux.HandleFunc("POST /internal/bugtracker/report/{id}/release", internalH.RequireToken(bugH.InternalRelease))
	mux.HandleFunc("POST /internal/bugtracker/report/{id}/resolve", internalH.RequireToken(bugH.InternalResolve))
	// Скриншоты — открытая раздача (только по UUID-имени).
	mux.HandleFunc("GET /uploads/bugtracker/", bugH.ServeUpload)

	// Reports — Задолженность ВГО. Единственный источник — готовый расчётный слой
	// FinDebt (Payments.report.FinDebt1/3), сверенный с 1С копейка-в-копейку.
	//   DEBT_MOCK=1        → фикстуры (dev без OLAP).
	//   DEBT_BACKEND=findebt (default) → отчёт читает CH finance.fact_findebt/_docs
	//                        (залито cmd/findebt-etl). Развязывает отчёт с живым OLAP.
	//   DEBT_BACKEND=findebt-live → прямое чтение FinDebt-вьюх из MSSQL (фолбэк/дебаг).
	var premasterRepo debt.PremasterRepo
	var mssqlDB *sql.DB // OLAP-пул: нужен findebt-live и модулю «Тактические планы»
	if !cfg.DebtMock {
		mssqlDB, err = debt.NewOLAPPool(cfg.PremasterServer, cfg.PremasterPort, cfg.PremasterDatabase, cfg.PremasterUser, cfg.PremasterPassword)
		if err != nil {
			log.Fatalf("debt: mssql open: %v", err)
		}
		if mssqlDB != nil {
			defer mssqlDB.Close()
		}

		switch cfg.DebtBackend {
		case "findebt-live":
			// Прямое чтение вьюх Payments.report.FinDebt1/3 из MSSQL (без CH).
			fdRepo, ferr := debt.NewFinDebtRepo(mssqlDB, cfg.PremasterPaymentsDatabase,
				cfg.DebtFinDebtSchema, cfg.DebtFinDebt1Table, cfg.DebtFinDebt3Table)
			if ferr != nil {
				log.Fatalf("debt: findebt-live init: %v", ferr)
			}
			if fdRepo == nil {
				log.Fatalf("debt: DEBT_BACKEND=findebt-live but MSSQL_PREMASTER_* not set")
			}
			premasterRepo = fdRepo
			log.Printf("debt: backend=findebt-live (MSSQL Payments.report.FinDebt1/3)")
		case "findebt-docdate":
			// Второй поток (метод аналитика): суммы из сырых Debt_arh/Wholesales_arh
			// (CH debt_facts/turnover_facts), BYN/USD пересчитаны на дату документа
			// через currency_daily (ASOF). Для сверки с findebt (дефолт, FinDebt3).
			ddRepo, derr := debt.NewFinDebtDocDateCHRepo(cfg.ClickHouseHTTPURL, cfg.ClickHouseUser, cfg.ClickHousePass)
			if derr != nil {
				log.Fatalf("debt: findebt-docdate init: %v", derr)
			}
			if ddRepo == nil {
				log.Fatalf("debt: DEBT_BACKEND=findebt-docdate but CLICKHOUSE_HTTP_URL/USER not set")
			}
			premasterRepo = ddRepo
			log.Printf("debt: backend=findebt-docdate (ClickHouse debt_facts/turnover_facts + currency_daily, пересчёт на дату документа)")
		default:
			// findebt (default): отчёт читает CH-снэпшот finance.fact_findebt/_docs.
			fdRepo, ferr := debt.NewFinDebtCHRepo(cfg.ClickHouseHTTPURL, cfg.ClickHouseUser, cfg.ClickHousePass)
			if ferr != nil {
				log.Fatalf("debt: findebt-ch init: %v", ferr)
			}
			if fdRepo == nil {
				log.Fatalf("debt: DEBT_BACKEND=findebt but CLICKHOUSE_HTTP_URL/USER not set")
			}
			premasterRepo = fdRepo
			log.Printf("debt: backend=findebt (ClickHouse finance.fact_findebt_ccy)")
		}
	}
	debtSvc := debt.NewService(cfg.DebtMock, premasterRepo)
	debtFilters := debt.NewFiltersRepo(pool)
	debtH := debt.NewHandler(debtSvc, debtFilters, func(r *http.Request) (int64, bool) {
		u, ok := auth.UserFromCtx(r.Context())
		if !ok || u == nil {
			return 0, false
		}
		return u.ID, true
	})
	// Доступ к отчёту «Задолженность ВГО» — только ROLE_FINANCE_ADMIN (или ROLE_ADMIN
	// через иерархию ExpandRoles). До широкого внедрения отчётность закрыта от
	// обычных пользователей; расширим RoleFinanceUser, когда определимся с моделью.
	mux.HandleFunc("GET /api/reports/debt/filter-options", technicalRead("GET /api/reports/debt/filter-options", "debt.FilterOptions", auth.RequireRole(authSvc, auth.RoleFinanceAdmin, debtH.FilterOptions)))
	mux.HandleFunc("GET /api/reports/debt/report", technicalRead("GET /api/reports/debt/report", "debt.Report", auth.RequireRole(authSvc, auth.RoleFinanceAdmin, debtH.Report)))
	mux.HandleFunc("GET /api/reports/debt/drilldown", technicalRead("GET /api/reports/debt/drilldown", "debt.Drilldown", auth.RequireRole(authSvc, auth.RoleFinanceAdmin, debtH.Drilldown)))
	mux.HandleFunc("GET /api/reports/debt/saved-filters", technicalRead("GET /api/reports/debt/saved-filters", "debt.ListSavedFilters", auth.RequireRole(authSvc, auth.RoleFinanceAdmin, debtH.ListSavedFilters)))
	mux.HandleFunc("POST /api/reports/debt/saved-filters", auth.RequireRole(authSvc, auth.RoleFinanceAdmin, debtH.CreateSavedFilter))
	mux.HandleFunc("DELETE /api/reports/debt/saved-filters", auth.RequireRole(authSvc, auth.RoleFinanceAdmin, debtH.DeleteSavedFilter))

	// Фоновый инкремент FinDebt → CH (finance.fact_findebt). Активен только в
	// live-режиме (нужен MSSQL-пул) и при FINDEBT_SYNC_INTERVAL>0. Инкремент сам
	// берёт watermark из CH; на findebt-live (без CH) воркер бессмысленен.
	if mssqlDB != nil && cfg.DebtBackend != "findebt-live" {
		fdTables := etl.FinDebtTables{
			Fin3FQN: "[" + cfg.PremasterPaymentsDatabase + "].[" + cfg.DebtFinDebtSchema + "].[" + cfg.DebtFinDebt3Table + "]",
		}
		etl.NewFinDebtWorker(etl.Deps{
			MSSQL:  mssqlDB,
			CHURL:  cfg.ClickHouseHTTPURL,
			CHUser: cfg.ClickHouseUser,
			CHPass: cfg.ClickHousePass,
		}, etl.FinDebtOpts{Tables: fdTables}).
			Start(context.Background(), time.Duration(cfg.DebtFinDebtSyncInterval)*time.Second)

		// Фоновая синхронизация курсов (dim_valuta, currency_daily) для второго
		// потока findebt-docdate. CURRENCY_SYNC_INTERVAL=0 → выключен (заливаем
		// вручную cmd/findebt-etl MODE=currency).
		etl.NewCurrencyWorker(etl.Deps{
			MSSQL:  mssqlDB,
			CHURL:  cfg.ClickHouseHTTPURL,
			CHUser: cfg.ClickHouseUser,
			CHPass: cfg.ClickHousePass,
		}, etl.CurrencyTables{
			ValutaFQN:        cfg.DebtValutaFQN,
			CurrencyDailyFQN: cfg.DebtCurrencyDailyFQN,
		}).Start(context.Background(), time.Duration(cfg.CurrencySyncInterval)*time.Second)

		// Фоновый reload сырых фактов метода аналитика (debt_facts/turnover_facts)
		// для findebt-docdate. DEBTARH_SYNC_INTERVAL=0 → выключен (заливаем вручную
		// cmd/findebt-etl MODE=debtarh).
		etl.NewDebtArhWorker(etl.Deps{
			MSSQL:  mssqlDB,
			CHURL:  cfg.ClickHouseHTTPURL,
			CHUser: cfg.ClickHouseUser,
			CHPass: cfg.ClickHousePass,
		}, etl.DebtArhTables{
			DebtArhFQN:    cfg.DebtArhFQN,
			WholesalesFQN: cfg.WholesalesArhFQN,
			Fin1FQN:       "[" + cfg.PremasterPaymentsDatabase + "].[" + cfg.DebtFinDebtSchema + "].[" + cfg.DebtFinDebt1Table + "]",
			CpartyCol:     cfg.DebtArhCpartyCol,
		}, "").Start(context.Background(), time.Duration(cfg.DebtArhSyncInterval)*time.Second)
	}

	// Реплика FinDWH → ClickHouse findwh (GLMF, Table_Fin_PL, ExRates) для
	// аналитиков, docs/bi/findwh-etl.md. Свой пул на 2 соединения: поток GLMF
	// держит соединение часами и не должен отнимать его у общего OLAP-пула (4).
	fwFrom, ferr := etl.ParseFinDWHDate(cfg.FinDWHGLMFFrom)
	if ferr != nil && cfg.FinDWHSyncInterval > 0 {
		log.Fatalf("findwh: FINDWH_GLMF_FROM: %v", ferr)
	}
	fwDeps := etl.Deps{
		CHURL:  cfg.ClickHouseHTTPURL,
		CHUser: cfg.ClickHouseUser,
		CHPass: cfg.ClickHousePass,
	}
	fwOpts := etl.FinDWHOpts{
		DB:             cfg.FinDWHDB,
		GLMFFrom:       fwFrom,
		ChunkMonths:    cfg.FinDWHChunkMonths,
		BatchSize:      cfg.FinDWHBatch,
		ReadyCheck:     cfg.FinDWHReadyCheck,
		ReadyAfterHour: cfg.FinDWHReadyAfterHour,
	}
	var (
		fwWorker   *etl.FinDWHWorker
		fwStatusDB *sql.DB
		fwOff      string
	)
	switch {
	case cfg.DebtMock:
		fwOff = "DEBT_MOCK=1 — живого MSSQL нет"
	case cfg.PremasterServer == "" || cfg.PremasterUser == "" || cfg.PremasterPassword == "":
		fwOff = "MSSQL_PREMASTER_* не заданы"
	default:
		if cfg.FinDWHSyncInterval > 0 {
			fwDB, err := debt.NewOLAPPool(cfg.PremasterServer, cfg.PremasterPort, cfg.FinDWHDB, cfg.PremasterUser, cfg.PremasterPassword)
			if err != nil {
				log.Fatalf("findwh: mssql open: %v", err)
			}
			defer fwDB.Close()
			fwDB.SetMaxOpenConns(2)
			fwDB.SetConnMaxIdleTime(5 * time.Minute)
			wDeps := fwDeps
			wDeps.MSSQL = fwDB
			fwWorker = etl.NewFinDWHWorker(wDeps, fwOpts)
			fwWorker.Start(context.Background(), time.Duration(cfg.FinDWHSyncInterval)*time.Second)
		} else {
			fwOff = "FINDWH_SYNC_INTERVAL=0"
		}
		// Страница статуса ходит в MSSQL своим пулом на одно соединение и с
		// таймаутами подключения: она не должна отнимать слоты у воркера, а
		// повисший TDS-логин (10.10.6.15 так умеет) — держать соединение вечно.
		fwStatusDB, err = debt.NewOLAPPoolDSN(cfg.PremasterServer, cfg.PremasterPort, cfg.FinDWHDB,
			cfg.PremasterUser, cfg.PremasterPassword, "dial+timeout=5&connection+timeout=8")
		if err != nil {
			log.Fatalf("findwh: mssql open (status): %v", err)
		}
		defer fwStatusDB.Close()
		fwStatusDB.SetMaxOpenConns(1)
		fwStatusDB.SetMaxIdleConns(1)
		fwStatusDB.SetConnMaxIdleTime(5 * time.Minute)
	}
	if fwWorker == nil {
		log.Printf("findwh-worker: disabled (%s)", fwOff)
	}
	stDeps := fwDeps
	stDeps.MSSQL = fwStatusDB
	fwStatus := etl.NewFinDWHStatus(stDeps, fwOpts, cfg.FinDWHSyncInterval, fwWorker, fwOff)
	if ferr != nil {
		fwStatus.NoteSettingError("FINDWH_GLMF_FROM", "не разобрано «"+cfg.FinDWHGLMFFrom+"», взят дефолт")
	}
	mux.HandleFunc("GET /api/admin/etl/findwh", auth.RequireRole(authSvc, auth.RoleAdmin, fwStatus.ServeHTTP))

	// Тактические планы (VS0 каркас + VS1 справочники + VS2 факт МП).
	// docs/reports/plans/SPEC.md. Факт: PLANS_MOCK=1 → фикстуры; иначе online
	// FinDWH (переиспользуем mssqlDB ВГО-отчёта; при nil — fallback на mock).
	// Факт МП: форма читает СНИМОК, а не OLAP вживую (правка ТЗ 02.09.2026 МП §12).
	// Причина — больше двухсот процедур DWH/FinDWH/MP перезаливают таблицы через
	// TRUNCATE, и в динамике суммы плавают: согласующий увидел бы не то, что
	// заполняющий. Снимок питается регламентом (раз в сутки) и ручкой админа.
	// PLANS_FACT_SNAPSHOT=0 — аварийный откат на прямое чтение источника.
	plansFactOrigin := plans.NewMpFactSource(cfg.PlansMock, mssqlDB, cfg.PlansMpFactTable, cfg.PlansMpPlanTable, cfg.PlansMpTaktTable, cfg.PlansMpPenaltyView)
	// Журнал прогонов источников (ТЗ доработок п. 1.1): падение числа строк
	// против прошлого успешного прогона сверх порога — warn в лог и уведомление
	// администраторам.
	plansSourceWatch := plans.NewSourceWatch(pool, cfg.PlansSourceDropAlertPct,
		func(ctx context.Context, title, message string) {
			if _, err := notifSvc.CreateForAllAdmins(ctx, notifications.Input{
				Title: title, Message: message, Type: "warning", ObjectType: "plans_source",
			}); err != nil {
				log.Printf("plans source-watch: уведомление админам не отправлено: %v", err)
			}
		})
	plansFactSnap := plans.NewMpFactSnapshot(pool, plansFactOrigin).WithWatch(plansSourceWatch)
	plansFact := plansFactOrigin
	if cfg.PlansFactSnapshot {
		plansFact = plans.NewSnapshotFactSource(plansFactSnap)
		plans.NewSnapshotRefresher(plansFactSnap, []string{"large", "small"},
			time.Duration(cfg.PlansFactSnapshotInterval)*time.Second).Start(context.Background())
	}
	plansScope := plans.NewPgScopeStore(pool)
	plansStore := plans.NewPgStore(pool)
	plansCalendar := plans.NewCalendarStore(pool)
	plansSvc := plans.NewService(plansStore, plansFact, plansScope).WithCalendar(plansCalendar)
	// Principal для ABAC: id пользователя + признак админа планов (обходит ABAC).
	plansPrincipal := func(r *http.Request) (plans.Principal, bool) {
		u := auth.CurrentUser(r)
		if u == nil {
			return plans.Principal{}, false
		}
		return plans.Principal{UserID: u.ID, PlansAdmin: auth.HasRole(u, auth.RolePlansAdmin)}, true
	}
	plansAudit := plans.NewAuditor(cfg.PlansAuditEnabled, pool)
	plansDir := plans.NewDirRepo(pool)
	if err := plansDir.EnsureSeed(context.Background()); err != nil {
		log.Printf("plans: dir seed: %v", err) // не фатально
	}
	// Под-справочники ЦФО (Группа/Подгруппа/Тип) из загруженного dir_cfo (если пусты).
	_ = plans.SeedCFOSubdirs(context.Background(), pool, false)
	plansH := plans.NewHandler(plans.NewSeedSource(), plansDir, plansFact, plansSvc, plansPrincipal, plansAudit)

	// Справочники Лиса/1С: коннектор + кэш + движок синхронизации + каталог должностей.
	var lisa *plans.LisaDB
	if !cfg.LisaMock {
		lisa, err = plans.OpenLisa(cfg.LisaHost, cfg.LisaPort, cfg.LisaDB, cfg.LisaUser, cfg.LisaPassword)
		if err != nil {
			log.Printf("plans: Lisa connect: %v (синхронизация Лисы недоступна)", err)
		}
	}
	plansCache := plans.NewDirCache(rdb, pool, time.Duration(cfg.PlansDirCacheTTL)*time.Second)
	// Источники формы «Розница» (ТЗ Розница §11) — тот же MSSQL, что у МП.
	// PLANS_MOCK=1 или отсутствие соединения → фикстуры (18 магазинов РБ): форма
	// и синхронизация справочника работают в dev без VPN.
	plansRetailSource := plans.NewRetailSource(cfg.PlansMock, mssqlDB, plans.RetailTables{
		StoreTable:          cfg.PlansRetailStoreTable,
		StoreGroup:          cfg.PlansRetailStoreGroup,
		LFLTable:            cfg.PlansRetailLFLTable,
		FactTable:           cfg.PlansRetailFactTable,
		StrategyTable:       cfg.PlansRetailStrategyTable,
		ApprovedTable:       cfg.PlansRetailApprovedTable,
		ComparisonFactTable: cfg.PlansRetailComparisonFactTable,
		NewStrategyTable:    cfg.PlansRetailNewStrategyTable,
		StrategyGroup:       cfg.PlansRetailStrategyGroup,
		PlanHistTable:       cfg.PlansRetailPlanHistTable,
		PlanHistNewTable:    cfg.PlansRetailPlanHistNew,
	})
	// Справочник магазинов розницы синхронизируется общим движком (версионирование
	// строк, раз в сутки + кнопка) — ТЗ Розница §11. Провайдер регистрируется ДО
	// Start: после старта фоновой синхронизации карту провайдеров не меняем.
	plansMpHints := plans.MpHintTables{
		SppAnalysisTable: cfg.PlansMpSppTable,
		SalesTable:       cfg.PlansMpSalesTable,
		PenaltyTable:     cfg.PlansMpPenaltyTable,
	}
	// Справочник статей PL синхронизируется из Budgeting.dbo.CodePL: кроме подписей
	// он несёт нормативный НДС по странам (правки ТЗ 02.09.2026, часть 5).
	// Без MSSQL провайдер вернёт ошибку синхронизации, а справочник останется на
	// сиде — форма от этого не падает.
	plansProviders := append(plans.BuildProviders(cfg.LisaMock, lisa),
		plans.NewRetailStoreProvider(plansRetailSource).WithScopeSource(plansDir),
		plans.NewPLLineProvider(mssqlDB, plans.PLLineTables{CodePLTable: cfg.PlansCodePLTable}),
		// Подсказки к полям ввода МП (%СПП и штрафы): считаются регламентом раз в
		// сутки тем же движком синхронизации — история цен слишком велика, чтобы
		// считать подсказку в момент открытия формы (правки ТЗ МП §9/§14).
		plans.NewSppHintProvider(mssqlDB, plansMpHints),
		plans.NewPenaltyHintProvider(mssqlDB, plansMpHints))
	plansSyncer := plans.NewSyncer(pool, plansProviders, plansCache)
	plansPositions := plans.NewPositionStore(pool)
	plansUsers := plans.NewUsersStore(pool)
	plansDeputies := plans.NewDeputyStore(pool)
	plansH.SetDirSubsystems(plansSyncer, plansCache, plansPositions, plansScope, plansUsers, plansDeputies)
	plansH.SetB24Importer(plans.NewB24Importer(cfg.B24UserGetWebhook, plansUsers))
	plansH.SetOrgStore(plans.NewOrgStore(pool))
	plansH.SetJobPositions(plans.NewJobPositionStore(pool))
	// Уведомления участникам заданий идут в общий поток кабинета (колокольчик +
	// опциональное дублирование в B24) — тот же notifSvc, что у баг-трекера.
	plansTasks := plans.NewTaskStore(pool, plansFact).WithScope(plansScope).WithNotifier(notifSvc).
		// Подсказки показываются РЯДОМ с полями ввода и в значения не попадают
		// (правки ТЗ 02.09.2026, часть 1).
		WithHints(plans.NewHintSource(plansDir)).
		WithFactSnapshot(plansFactSnap)
	plansH.SetTaskStore(plansTasks)
	plansH.SetFactSnapshot(plansFactSnap)
	// Фоновая синхронизация + прогрев кэша (DIR-03). Интервал из PLANS_SYNC_INTERVAL.
	plansSyncer.Start(context.Background(), time.Duration(cfg.PlansSyncInterval)*time.Second)

	// ── Общая оболочка процесса: карточки форм, версии, публикация ──
	// Ставки НДС и курсы — из справочников (dir_vat/dir_fx_rate), не из кода:
	// ТЗ МП §3.3/§3.4 прямо запрещают прошивать их в формулу.
	plansRates := plans.NewRateBook(plansDir)
	// Публикация: пишем только в таблицы из белого списка PLANS_PUBLISH_TARGETS и
	// только когда PLANS_PUBLISH_ENABLED=1 (до ответов BI по §12 — лишь dry-run).
	// Приоритет — реальный приёмник; PLANS_PUBLISH_SANDBOX=1 подставляет вместо него
	// PG-песочницу (publish_sandbox), чтобы путь записи можно было прогнать без
	// Budgeting. В проде обе переменные вместе не задаются: песочница — dev/тест.
	var plansPublisher plans.Publisher = plans.NewNopPublisher()
	switch {
	case cfg.PlansPublishEnabled && mssqlDB != nil:
		plansPublisher = plans.NewMssqlPublisher(mssqlDB, cfg.PlansPublishTargets)
	case cfg.PlansPublishSandbox:
		log.Printf("plans: публикация идёт в ПЕСОЧНИЦУ publish_sandbox (PLANS_PUBLISH_SANDBOX=1) — в Budgeting записи не будет")
		plansPublisher = plans.NewSandboxPublisher(pool, cfg.PlansPublishTargets)
	}
	// Исполнители шагов маршрута — один резолвер и для переходов карточек, и для
	// схемы согласования (ТЗ доработок п. 4.3): схема показывает ровно тех, кому
	// карточка реально придёт.
	plansStepOwners := plans.NewCardStepOwnerResolver(pool, plansTasks)
	plansCards := plans.NewCardService(plans.NewCardStore(pool)).
		WithPublisher(plansPublisher, plans.NewPublishStore(pool)).
		WithOwners(plansStepOwners).
		// План пишется на РЕАЛЬНОЕ ЮЛ: колонка «Компания» приёмника тактики
		// заполняется УНП из справочника (правки ТЗ 02.09.2026 §15/§16).
		WithLegalEntities(plans.NewLegalEntityResolver(plansDir)).
		// Уведомления по карточкам: согласующий узнаёт, что форма ждёт его
		// решения. Тот же канал, что у заданий (колокольчик + Bitrix).
		WithNotifier(notifSvc).
		// Этапы таймлайна считаются по карточкам: «этап 1.2 готово» при формах в
		// черновиках — то, из-за чего процесс выглядел рассинхронизированным.
		WithStages(plansStore).
		// Задание «заполни форму» закрывается отправкой формы, а не отдельной
		// кнопкой: иначе задание и форма живут своей жизнью.
		WithFillTasks(plansTasks).
		WithAudit(plansAudit)
	// Этапы знают про карточки: закрыть этап 1.х руками, пока формы не прошли
	// шаг, нельзя — решение принимается в карточке формы.
	plansSvc.WithCards(plansCards)
	// Карточки форм заводятся вместе с периодом: до этого процесс выглядел
	// незапущенным (этапы и задания есть, а формы «не созданы»).
	plansH.SetCards(plansCards)
	// Реестр условий площадки нужен и провайдеру карточки (общие затраты идут в
	// приёмник под ЦФО 250), и сервису формы — заводим до обоих.
	plansMpCond := plans.NewMpConditionsStore(pool)
	// Стратегические доли статей по умолчанию (ТЗ доработок п. 2.1): ОПУ-витрина,
	// сценарий стратегии на месяц тактики. Общий экземпляр — общий кэш.
	plansMpShareDefaults := plans.NewMpShareDefaults(
		plans.NewMpArticleSource(cfg.PlansMock, mssqlDB, cfg.PlansMpArticleFactTable),
		cfg.PlansMpStrategyScenario)
	mpCardProvider := plans.NewMpCardProvider(plansStore, plansRates).
		WithFactSource(plansFact).WithCommonCosts(plansMpCond).WithShareDefaults(plansMpShareDefaults)
	plansCards.RegisterForm(plans.TemplateMP, mpCardProvider, mpCardProvider)

	// Форма «Розница» (TPL-TO-RETAIL): карточка + сервис сетки/расчёта/валидаций.
	plansRetailRepo := plans.NewRetailRepo(pool)
	// Свод по ЮЛ показывает весь комплект: маркетплейсы из pl_metric, розницу —
	// из её таблиц (замечание владельца процесса 09.09.2026).
	if src, ok := plansRetailRepo.(plans.RetailSvodSource); ok {
		plansSvc.WithRetailSvod(src)
	}
	plansRetailProvider := plans.NewRetailCardProvider(plansRetailRepo, plansRates)
	plansCards.RegisterForm(plans.TemplateRetail, plansRetailProvider, plansRetailProvider)
	plansRetail := plans.NewRetailService(plansRetailRepo, plansCards, plansRetailSource).
		WithRates(plansRates).
		WithScopeSource(plansDir).
		WithWatch(plansSourceWatch).
		WithCalendar(plansCalendar).
		WithScope(plansScope).
		WithAudit(plansAudit).
		WithValueLimits(cfg.PlansRetailValueLimits).
		WithReconBlocking(cfg.PlansRetailReconBlocking)
	// Гейт отправки: карточка не уходит на согласование, если форма не проходит
	// блокирующие проверки. До этого проверки жили только в интерфейсе, и форму
	// можно было отправить мимо них — согласующий обязан был вернуть её, и
	// процесс делал лишний круг.
	// Режим гейта (ответ 41 от 10.09.2026): проверки считаются всегда, блокировка
	// переключаемая. Дефолт — мягкий: на стенде форма не должна вставать из-за
	// расхождения, о котором согласующий и так увидит предупреждение.
	plansCards.WithSubmitStrict(cfg.PlansSubmitStrict)
	plansCards.WithRetailCountryGate(cfg.PlansRetailCountryGate)
	plans.SetCfoDeptFormsEnabled(cfg.PlansCfoDeptForms)
	plansCards.WithSubmitGate(plans.TemplateRetail, plans.NewRetailSubmitGate(plansRetail))
	plansRetailH := plans.NewRetailHandler(plansRetail, plansPrincipal)

	// Форма «Расходы ЦП» (TPL-CFO-EXP, этап 1.5). Пока это сетка ввода и её
	// источник: реестры, сверки E-01…E-08 и публикация — следующие фазы плана
	// docs/reports/plans/stage-1.5-plan.md, публикация до ответов по В-1/В-2
	// намеренно выключена в маршруте (миграция 0051).
	plansExpSource := plans.NewExpSource(cfg.PlansMock, mssqlDB, plans.ExpTables{
		CostsProc:  cfg.PlansExpCostsProc,
		CfoTable:   cfg.PlansExpCfoTable,
		SalesTable: cfg.PlansRetailStrategyTable,
	})
	// Затраты формы читаются из ночного снимка в ClickHouse (finance.exp_costs): процедура
	// на OLAP работает 4–5 минут. Снимок заливает воркер раз в сутки (по умолчанию в окне
	// 5:00–9:00 по часам MSSQL); при пустой таблице или недоступном CH форма читает процедуру
	// напрямую, как раньше. Кнопка «Обновить из источника» запускает внеплановую заливку.
	var expCostsRefresh func(context.Context) error
	if !cfg.PlansMock && !cfg.DebtMock && cfg.PremasterServer != "" && cfg.PremasterUser != "" && cfg.PremasterPassword != "" {
		expDB, err := debt.NewOLAPPool(cfg.PremasterServer, cfg.PremasterPort, cfg.FinDWHDB, cfg.PremasterUser, cfg.PremasterPassword)
		if err != nil {
			log.Fatalf("expcosts: mssql open: %v", err)
		}
		defer expDB.Close()
		expDB.SetMaxOpenConns(2)
		expDB.SetConnMaxIdleTime(5 * time.Minute)
		expWorker := etl.NewExpCostsWorker(etl.Deps{
			MSSQL: expDB, CHURL: cfg.ClickHouseHTTPURL, CHUser: cfg.ClickHouseUser, CHPass: cfg.ClickHousePass,
		}, etl.ExpCostsOpts{
			Proc: cfg.ExpCostsProc, AfterHour: cfg.ExpCostsAfterHour, BeforeHour: cfg.ExpCostsBeforeHour, BatchSize: cfg.FinDWHBatch,
		})
		expWorker.Start(context.Background(), time.Duration(cfg.ExpCostsSyncInterval)*time.Second)
		expCostsRefresh = expWorker.RefreshNow
	} else {
		log.Printf("expcosts-worker: disabled (PLANS_MOCK/DEBT_MOCK или MSSQL_PREMASTER_* не заданы)")
	}
	if cfg.PlansExpCostsFromCH && !cfg.PlansMock {
		plansExpSource = plans.NewExpCHSource(plansExpSource, cfg.ClickHouseHTTPURL, cfg.ClickHouseUser, cfg.ClickHousePass, expCostsRefresh)
		plansExpSource = plans.ExpCHSourceAccrualFact(plansExpSource, cfg.PlansExpFactFromFinPL)
	}
	plansExp := plans.NewExpService(plans.NewExpRepo(pool), plansCards, plansExpSource).
		WithRegistries(plans.NewExpRegRepo(pool)).
		WithTravelNorms(plans.NewExpTravelNorms(pool, plansRates)).
		WithSalesPlan(plansExpSource).
		WithRates(plansRates).
		WithScope(plansScope)
	// Публикация формы 1.5 (ТЗ §10). Ответы 34 и 11 перенесли сюда публикацию
	// общих затрат МП и статей 51/52/54 под ЦФО 250 — из формы продаж они убраны
	// в этом же релизе, иначе приёмник получил бы двойные затраты.
	plansExpProvider := plans.NewExpCardProvider(plans.NewExpRepo(pool), plans.NewExpRegRepo(pool)).
		WithCommonCosts(plansMpCond)
	// TЗ v2 adds six code-based department forms under the same route code.
	// Route snapshots/publication by card scope so legacy 1.5 cards keep their
	// existing provider and CFO cards use the year/scenario-aware model.
	plansCfoRepo := plans.NewCfoExpenseRepo(pool)
	plansCfoSvc := plans.NewCfoExpenseService(plansCfoRepo, plansCards,
		plans.NewCfoExpenseSource(plansExpSource), plansScope)
	plansCfoProvider := plans.NewCfoExpenseProvider(plansCfoRepo)
	plansCards.RegisterForm(plans.TemplateExp,
		plans.NewRoutedCfoExpenseProvider(plansExpProvider, plansExpProvider, plansCfoProvider, plansCfoProvider),
		plans.NewRoutedCfoExpenseProvider(plansExpProvider, plansExpProvider, plansCfoProvider, plansCfoProvider))

	// Гейт отправки формы 1.5: считает валидации §9 и контроли §8. Блокирует он
	// или только предупреждает — решает PLANS_SUBMIT_STRICT (ответ 41).
	plansCards.WithSubmitGate(plans.TemplateExp,
		plans.NewRoutedExpSubmitGate(plans.NewExpSubmitGate(plansExp), plans.NewCfoSubmitGate(plansCfoSvc)))
	plansExpH := plans.NewExpHandler(plansExp, plansPrincipal)
	// Свод «Бюджет по ЮЛ» (этапы 1.6, 3, 4): продажи и расходы по ЮЛ ЦФО, корректировки итогов.
	plansLeSvc := plans.NewLeSvodService(plansCards, plansExp, plansSvc.Svod, plansRates,
		plans.NewLeAdjustRepo(pool), plansStore.Stages)
	plansLeH := plans.NewLeHandler(plansLeSvc, plansPrincipal)
	// Этап 1.6 нельзя завершить, пока формы продаж и расходов не утверждены.
	plansSvc.WithStageGate(plansLeSvc.Gate)
	plansCfoH := plans.NewCfoExpenseHandler(plansCfoSvc, plansPrincipal)
	plansCfoRegistryH := plans.NewCfoExpenseRegistryHandler(pool, plansCards, plansScope, plansPrincipal)

	plansCardsH := plans.NewCardHandler(plansCards, plansScope, plansPrincipal).
		WithPresets(plans.NewPresetStore(pool))

	// Форма МП по скорректированному ТЗ: реестр условий площадки + инверсия
	// расчёта. TaskStore получает контекст расчёта, чтобы сетка формы считалась
	// тем же направлением, что указано в карточке (calc_mode).
	plansMpCondSvc := plans.NewMpConditionsService(plansMpCond, plansStore, plans.NewCardStore(pool),
		plansRates, plansFact, plansScope).WithAudit(plansAudit).
		WithShareDefaults(plansMpShareDefaults)
	plansMpCondH := plans.NewMpConditionsHandler(plansMpCondSvc, plansPrincipal)
	plansTasks.WithCalcContext(plans.NewCardStore(pool), plansMpCond, plansRates).
		WithShareDefaults(plansMpShareDefaults)

	mux.HandleFunc("GET /api/plans/health", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.Health))
	mux.HandleFunc("GET /api/plans/directories", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.Directories))
	mux.HandleFunc("GET /api/plans/directories/{code}/rows", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.DirectoryRows))
	mux.HandleFunc("GET /api/plans/mp/fact", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.MpFact))
	// Legacy-ветка формы МП (2 editable-строки, движок calc.go) заморожена:
	// актуальная форма — /api/plans/tasks/{id}/mp-form. Включается PLANS_LEGACY_MP_API=1.
	legacyMp := plans.LegacyGate(cfg.PlansLegacyMpAPI)
	mux.HandleFunc("GET /api/plans/mp/form", auth.RequireRole(authSvc, auth.RolePlansUser, legacyMp(plansH.MpFormGet)))
	mux.HandleFunc("PUT /api/plans/mp/form", auth.RequireRole(authSvc, auth.RolePlansUser, legacyMp(plansH.MpFormSave)))
	mux.HandleFunc("GET /api/plans/instances", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.InstancesList))
	mux.HandleFunc("POST /api/plans/instances", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.CreateInstance))
	mux.HandleFunc("GET /api/plans/mp/compute", auth.RequireRole(authSvc, auth.RolePlansUser, legacyMp(plansH.MpCompute)))
	mux.HandleFunc("GET /api/plans/mp/svod", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.MpSvod))
	mux.HandleFunc("POST /api/plans/mp/copy", auth.RequireRole(authSvc, auth.RolePlansUser, legacyMp(plansH.MpCopy)))
	mux.HandleFunc("PUT /api/plans/mp/formula", auth.RequireRole(authSvc, auth.RolePlansUser, legacyMp(plansH.FormulaOverride)))
	mux.HandleFunc("GET /api/plans/mp/export", auth.RequireRole(authSvc, auth.RolePlansUser, legacyMp(plansH.MpExport)))
	mux.HandleFunc("POST /api/plans/mp/import", auth.RequireRole(authSvc, auth.RolePlansUser, legacyMp(plansH.MpImport)))
	mux.HandleFunc("GET /api/plans/instances/{id}/approvals", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.ApprovalsList))
	// Карточки форм: единая оболочка процесса (статусы, возвраты, версии, публикация).
	mux.HandleFunc("GET /api/plans/forms", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.Forms))
	mux.HandleFunc("GET /api/plans/instances/{id}/cards", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.CardsList))
	mux.HandleFunc("GET /api/plans/cards/{cardId}", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.CardGet))
	mux.HandleFunc("POST /api/plans/cards/{cardId}/action", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.CardAction))
	mux.HandleFunc("GET /api/plans/cards/{cardId}/versions/{version}", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.CardVersionPayload))
	mux.HandleFunc("PUT /api/plans/cards/{cardId}/calc-mode", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansCardsH.CardCalcMode))
	mux.HandleFunc("POST /api/plans/cards/{cardId}/publish", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.CardPublish))
	mux.HandleFunc("GET /api/plans/cards/{cardId}/publish-log", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.CardPublishLog))
	// Содержимое PG-песочницы приёмника (PLANS_PUBLISH_SANDBOX=1) — проверка записи.
	mux.HandleFunc("GET /api/plans/publish-sandbox", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansCardsH.PublishSandbox))
	mux.HandleFunc("GET /api/plans/forms/{formCode}/route", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.FormRoute))
	mux.HandleFunc("GET /api/plans/period/{plId}/route-scheme", auth.RequireRole(authSvc, auth.RolePlansUser,
		plans.NewRouteSchemeHandler(plansCards, plansStepOwners, pool, plansScope, plansPrincipal).Get))
	mux.HandleFunc("PUT /api/plans/forms/{formCode}/route", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansCardsH.FormRouteSave))
	mux.HandleFunc("GET /api/plans/forms/{formCode}/publish-mapping", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.PublishMappings))
	mux.HandleFunc("PUT /api/plans/forms/{formCode}/publish-mapping", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansCardsH.PublishMappingSave))
	// Форма МП по скорректированному ТЗ: реестр «Условия площадки» (§6.1), общие
	// затраты по 7 группам статей (§4.3), пересчёт расходной части от условий и
	// отчёт валидаций МП-01..11 / W1..W8 (§10). Пересчёт с preview=1 ничего не
	// сохраняет — это обязательный предпросмотр diff при изменении условий (§7.2).
	mux.HandleFunc("GET /api/plans/mp/cards/{cardId}/conditions", auth.RequireRole(authSvc, auth.RolePlansUser, plansMpCondH.ConditionsGet))
	mux.HandleFunc("PUT /api/plans/mp/cards/{cardId}/conditions", auth.RequireRole(authSvc, auth.RolePlansUser, plansMpCondH.ConditionsSave))
	mux.HandleFunc("POST /api/plans/mp/cards/{cardId}/conditions/copy", auth.RequireRole(authSvc, auth.RolePlansUser, plansMpCondH.ConditionsCopy))
	mux.HandleFunc("POST /api/plans/mp/cards/{cardId}/recalc", auth.RequireRole(authSvc, auth.RolePlansUser, plansMpCondH.Recalc))
	mux.HandleFunc("GET /api/plans/mp/cards/{cardId}/validate", auth.RequireRole(authSvc, auth.RolePlansUser, plansMpCondH.Validate))
	mux.HandleFunc("GET /api/plans/mp/cards/{cardId}/common-costs", auth.RequireRole(authSvc, auth.RolePlansUser, plansMpCondH.CommonCostsGet))
	mux.HandleFunc("PUT /api/plans/mp/cards/{cardId}/common-costs", auth.RequireRole(authSvc, auth.RolePlansUser, plansMpCondH.CommonCostsSave))
	// Форма «Розница» (ТЗ Розница §12). Чтение — RolePlansUser (ABAC/РМ §9
	// фильтрует магазины внутри сервиса), параметры периода и LFL-переопределение
	// требуют финансиста и проверяются в сервисе, а не ролью на маршруте.
	mux.HandleFunc("GET /api/plans/exp/{cardId}/form", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.FormGet))
	mux.HandleFunc("PUT /api/plans/exp/{cardId}/form", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.FormSave))
	mux.HandleFunc("GET /api/plans/exp-consolidated/{plId}", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.ConsolidatedGet))
	mux.HandleFunc("GET /api/plans/le-svod/{plId}", auth.RequireRole(authSvc, auth.RolePlansUser, plansLeH.Get))
	mux.HandleFunc("POST /api/plans/le-svod/{plId}/adjust", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansLeH.Adjust))
	mux.HandleFunc("DELETE /api/plans/le-svod/{plId}/adjust/{id}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansLeH.Revoke))
	mux.HandleFunc("POST /api/plans/exp/{cardId}/sync", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.SyncPost))
	mux.HandleFunc("POST /api/plans/exp/{cardId}/bulk", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.BulkApply))
	mux.HandleFunc("GET /api/plans/exp/{cardId}/totals", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.TotalsGet))
	mux.HandleFunc("GET /api/plans/exp/{cardId}/export", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.Export))
	mux.HandleFunc("GET /api/plans/exp/{cardId}/import-template", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.ImportTemplate))
	mux.HandleFunc("POST /api/plans/exp/{cardId}/import", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.Import))
	mux.HandleFunc("GET /api/plans/exp/{cardId}/params", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.ParamsGet))
	mux.HandleFunc("PUT /api/plans/exp/{cardId}/params", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansExpH.ParamsSave))
	mux.HandleFunc("POST /api/plans/exp/{cardId}/calc", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.CalcApply))
	mux.HandleFunc("GET /api/plans/exp/{cardId}/summary", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.SummaryGet))
	mux.HandleFunc("GET /api/plans/exp/{cardId}/validate", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.ValidateGet))
	mux.HandleFunc("GET /api/plans/exp/{cardId}/registry/{kind}", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.RegistryGet))
	mux.HandleFunc("PUT /api/plans/exp/{cardId}/registry/{kind}", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.RegistrySave))
	mux.HandleFunc("POST /api/plans/exp/{cardId}/registry/{kind}/{rowId}/copy", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.RegistryCopy))
	mux.HandleFunc("DELETE /api/plans/exp/{cardId}/registry/{kind}/{rowId}", auth.RequireRole(authSvc, auth.RolePlansUser, plansExpH.RegistryDelete))
	// Code-based CFO expense forms (ТЗ v2, cards CFO-{code}-{country}).
	mux.HandleFunc("GET /api/plans/cfo-exp/{cardId}/form", technicalRead("GET /api/plans/cfo-exp/{cardId}/form", "plans.CfoExpense.FormGet", auth.RequireRole(authSvc, auth.RolePlansUser, plansCfoH.FormGetReadOnly)))
	mux.HandleFunc("PUT /api/plans/cfo-exp/{cardId}/form", auth.RequireRole(authSvc, auth.RolePlansUser, plansCfoH.FormSave))
	mux.HandleFunc("POST /api/plans/cfo-exp/{cardId}/bulk", auth.RequireRole(authSvc, auth.RolePlansUser, plansCfoH.Bulk))
	mux.HandleFunc("GET /api/plans/cfo-exp/{cardId}/validate", technicalRead("GET /api/plans/cfo-exp/{cardId}/validate", "plans.CfoExpense.Validate", auth.RequireRole(authSvc, auth.RolePlansUser, plansCfoH.ValidateReadOnly)))
	mux.HandleFunc("GET /api/plans/cfo-exp/{cardId}/export", technicalRead("GET /api/plans/cfo-exp/{cardId}/export", "plans.CfoExpense.Export", auth.RequireRole(authSvc, auth.RolePlansUser, plansCfoH.ExportReadOnly)))
	mux.HandleFunc("POST /api/plans/cfo-exp/{cardId}/import", auth.RequireRole(authSvc, auth.RolePlansUser, plansCfoH.Import))
	mux.HandleFunc("GET /api/plans/cfo-exp/{cardId}/registry/{sheet}", technicalRead("GET /api/plans/cfo-exp/{cardId}/registry/{sheet}", "plans.CfoExpenseRegistry.RegistryGet", auth.RequireRole(authSvc, auth.RolePlansUser, plansCfoRegistryH.RegistryGet)))
	mux.HandleFunc("PUT /api/plans/cfo-exp/{cardId}/registry/{sheet}", auth.RequireRole(authSvc, auth.RolePlansUser, plansCfoRegistryH.RegistrySave))
	mux.HandleFunc("DELETE /api/plans/cfo-exp/{cardId}/registry/{sheet}/{rowId}", auth.RequireRole(authSvc, auth.RolePlansUser, plansCfoRegistryH.RegistryDelete))
	mux.HandleFunc("POST /api/plans/cfo-exp/{cardId}/registry/{sheet}/{rowId}/copy", auth.RequireRole(authSvc, auth.RolePlansUser, plansCfoRegistryH.RegistryCopy))
	mux.HandleFunc("GET /api/plans/retail/{cardId}/form", auth.RequireRole(authSvc, auth.RolePlansUser, plansRetailH.FormGet))
	mux.HandleFunc("PUT /api/plans/retail/{cardId}/form", auth.RequireRole(authSvc, auth.RolePlansUser, plansRetailH.FormSave))
	mux.HandleFunc("POST /api/plans/retail/{cardId}/bulk", auth.RequireRole(authSvc, auth.RolePlansUser, plansRetailH.BulkApply))
	mux.HandleFunc("GET /api/plans/retail/{cardId}/params", auth.RequireRole(authSvc, auth.RolePlansUser, plansRetailH.ParamsGet))
	mux.HandleFunc("PUT /api/plans/retail/{cardId}/params", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansRetailH.ParamsSave))
	mux.HandleFunc("PUT /api/plans/retail/{cardId}/lfl-override", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansRetailH.LFLOverride))
	mux.HandleFunc("GET /api/plans/retail/{cardId}/summary", auth.RequireRole(authSvc, auth.RolePlansUser, plansRetailH.SummaryGet))
	mux.HandleFunc("GET /api/plans/retail/period/{plId}/consolidated", auth.RequireRole(authSvc, auth.RolePlansUser, plansRetailH.ConsolidatedGet))
	mux.HandleFunc("GET /api/plans/retail/lfl-statuses", auth.RequireRole(authSvc, auth.RolePlansUser, plansRetailH.LFLStatusesGet))
	mux.HandleFunc("POST /api/plans/retail/lfl-refresh", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansRetailH.LFLRefresh))
	// Раздел «Стратегические планы» (ТЗ доработок п. 5.1).
	mux.HandleFunc("GET /api/plans/strategy/mp-shares", auth.RequireRole(authSvc, auth.RolePlansUser,
		plans.NewStrategyHandler(plansMpShareDefaults).MpShares))
	mux.HandleFunc("GET /api/plans/retail/{cardId}/validate", auth.RequireRole(authSvc, auth.RolePlansUser, plansRetailH.ValidateGet))
	mux.HandleFunc("GET /api/plans/retail/{cardId}/export", auth.RequireRole(authSvc, auth.RolePlansUser, plansRetailH.Export))
	mux.HandleFunc("GET /api/plans/retail/{cardId}/import-template", auth.RequireRole(authSvc, auth.RolePlansUser, plansRetailH.ImportTemplate))
	mux.HandleFunc("POST /api/plans/retail/{cardId}/import", auth.RequireRole(authSvc, auth.RolePlansUser, plansRetailH.Import))
	// Соответствие «пользователь ↔ RegManager» (ТЗ Розница §9). Без него ни один
	// РМ не увидит своих магазинов: в справочнике хранится ФИО, а не логин, а
	// автосвязка через DimEmployee не подтверждена (§12 п.3).
	plansRegMgrH := plans.NewRegManagerHandler(plans.NewRegManagerStore(pool), plansPrincipal)
	mux.HandleFunc("GET /api/plans/retail/reg-managers", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansRegMgrH.List))
	mux.HandleFunc("PUT /api/plans/retail/reg-managers", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansRegMgrH.Save))
	mux.HandleFunc("DELETE /api/plans/retail/reg-managers/{id}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansRegMgrH.Delete))

	mux.HandleFunc("GET /api/plans/forms/{formCode}/presets", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.PresetsGet))
	mux.HandleFunc("PUT /api/plans/forms/{formCode}/presets", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.PresetsSave))
	mux.HandleFunc("DELETE /api/plans/forms/{formCode}/presets", auth.RequireRole(authSvc, auth.RolePlansUser, plansCardsH.PresetsDelete))
	// Движок заданий процесса: список/генерация/действия/владелец этапа.
	mux.HandleFunc("GET /api/plans/instances/{id}/tasks", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.TasksList))
	mux.HandleFunc("POST /api/plans/instances/{id}/tasks/generate", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.TasksGenerate))
	mux.HandleFunc("GET /api/plans/tasks/mine", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.MyTasks))
	mux.HandleFunc("GET /api/plans/tasks/all", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.AllTasks))
	mux.HandleFunc("GET /api/plans/instances/{id}/pnl", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.PnlView))
	mux.HandleFunc("GET /api/plans/instances/{id}/board", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.BoardView))
	mux.HandleFunc("POST /api/plans/instances/{id}/strategy/import", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.StrategyImport))
	mux.HandleFunc("GET /api/plans/tasks/{taskId}/data", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.TaskDataView))
	mux.HandleFunc("PUT /api/plans/tasks/{taskId}/assignee", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.TaskAssign))
	// Ручной прогон регламента снимка факта МП (правка ТЗ МП §12) — только админ.
	mux.HandleFunc("POST /api/plans/admin/mp-fact-snapshot", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.MpFactSnapshotRefresh))
	mux.HandleFunc("GET /api/plans/tasks/{taskId}/mp-form", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.MpTaskFormGet))
	mux.HandleFunc("PUT /api/plans/tasks/{taskId}/mp-form", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.MpTaskFormSave))
	mux.HandleFunc("GET /api/plans/tasks/{taskId}/mp-form/export", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.MpTaskFormExport))
	mux.HandleFunc("POST /api/plans/tasks/{taskId}/mp-form/import", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.MpTaskFormImport))
	mux.HandleFunc("POST /api/plans/tasks/{taskId}/action", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.TaskAction))
	// История задания: кто взял, кто кому передал, с каким пояснением и сроком.
	// Доступна исполнителю, а не только аудитору — иначе «почему это задание у
	// меня» остаётся без ответа (прозрачность передачи работы).
	mux.HandleFunc("GET /api/plans/tasks/{taskId}/events", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.TaskEvents))
	mux.HandleFunc("GET /api/plans/instances/{id}/stage-owners", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.StageOwnersList))
	mux.HandleFunc("PUT /api/plans/instances/{id}/stages/{code}/owner", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.StageOwnerSet))
	mux.HandleFunc("GET /api/plans/instances/{id}/stages/{code}/readiness", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.StageReadiness))
	// Конструктор шаблонов заданий (админ процессов).
	mux.HandleFunc("GET /api/plans/task-templates", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.TaskTemplatesList))
	mux.HandleFunc("PUT /api/plans/task-templates", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.TaskTemplateUpsert))
	mux.HandleFunc("DELETE /api/plans/task-templates/{id}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.TaskTemplateDelete))
	mux.HandleFunc("GET /api/plans/instances/{id}/stages", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.StagesList))
	mux.HandleFunc("POST /api/plans/instances/{id}/stages/{code}/action", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.StageAction))
	mux.HandleFunc("GET /api/plans/instances/{id}/comments", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.CommentsList))
	mux.HandleFunc("POST /api/plans/instances/{id}/comments", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.CommentCreate))
	// Назначение ABAC-среза — только админ процессов (ROLE_PLANS_ADMIN).
	mux.HandleFunc("PUT /api/plans/scope/{user_id}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.ScopeUpsert))
	// Журнал аудита — просмотр только админ процессов (view_audit).
	mux.HandleFunc("GET /api/plans/audit", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.AuditList))
	// Справочники: реестр+схема (чтение — участник), правка/синхронизация — админ.
	mux.HandleFunc("GET /api/plans/dir", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.DirRegistry))
	mux.HandleFunc("GET /api/plans/dir/{code}/rows", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.DirRowsCached))
	mux.HandleFunc("GET /api/plans/dir/{code}/log", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.DirSyncLog))
	mux.HandleFunc("GET /api/plans/dir/{code}/versions", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.DirVersions))
	mux.HandleFunc("PUT /api/plans/dir/{code}/rows", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.DirRowUpsert))
	mux.HandleFunc("DELETE /api/plans/dir/{code}/rows/{id}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.DirRowDelete))
	mux.HandleFunc("POST /api/plans/dir/{code}/sync", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.DirSync))
	mux.HandleFunc("POST /api/plans/dir/{code}/warm", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.DirWarm))
	mux.HandleFunc("PUT /api/plans/dir/{code}/settings", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.DirSettings))
	// Каталог должностей + назначения (страница «Пользователи и права»): админ процессов.
	mux.HandleFunc("GET /api/plans/positions", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.PositionsList))
	mux.HandleFunc("PUT /api/plans/positions/{code}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.PositionUpsert))
	mux.HandleFunc("PUT /api/plans/positions/{code}/stages", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.PositionStages))
	mux.HandleFunc("DELETE /api/plans/positions/{code}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.PositionDelete))
	// Заместители (глобально + по этапу) и метка отсутствия пользователя.
	mux.HandleFunc("GET /api/plans/deputies", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.DeputiesList))
	mux.HandleFunc("PUT /api/plans/deputies", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.DeputyUpsert))
	mux.HandleFunc("DELETE /api/plans/deputies/{id}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.DeputyDelete))
	mux.HandleFunc("PUT /api/plans/users/{id}/absence", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.UserAbsence))
	mux.HandleFunc("POST /api/plans/users/import-b24", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.PlanUsersImportB24))
	mux.HandleFunc("GET /api/plans/users/search", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.PlanUsersSearch))
	// Структура компании (ответственность по направлениям + учредители).
	mux.HandleFunc("GET /api/plans/org", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.OrgTree))
	mux.HandleFunc("PUT /api/plans/org/responsible", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.OrgSetResponsible))
	// Должности (job positions): носитель + покрытие ЦФО (ТОП).
	mux.HandleFunc("GET /api/plans/jobpos", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.JobPositionsList))
	mux.HandleFunc("PUT /api/plans/jobpos", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.JobPositionUpsert))
	mux.HandleFunc("DELETE /api/plans/jobpos/{id}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.JobPositionDelete))
	mux.HandleFunc("GET /api/plans/jobpos/{id}/cfo", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.JobPositionCfo))
	mux.HandleFunc("PUT /api/plans/jobpos/{id}/cfo", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.JobPositionAssignCfo))
	mux.HandleFunc("DELETE /api/plans/jobpos/cfo/{code}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.JobPositionUnassignCfo))
	mux.HandleFunc("POST /api/plans/users/upsert", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.PlanUserUpsert))
	mux.HandleFunc("GET /api/plans/assignments", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.AssignmentsList))
	mux.HandleFunc("DELETE /api/plans/assignments/{id}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.AssignmentDelete))
	// Пользователи модуля + их системные ТП-роли (страница «Пользователи и права»).
	mux.HandleFunc("GET /api/plans/users", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.PlanUsersList))
	mux.HandleFunc("PUT /api/plans/users/{id}/roles", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.PlanUserRoles))
	// Маршрут процесса (ответственные по этапам): чтение — участник, правка — админ.
	mux.HandleFunc("GET /api/plans/route", auth.RequireRole(authSvc, auth.RolePlansUser, plansH.RouteList))
	mux.HandleFunc("PUT /api/plans/route/{code}", auth.RequireRole(authSvc, auth.RolePlansAdmin, plansH.RouteUpsert))

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           technicalGate.Middleware(mux, withLogging(withCORS(mux, cfg.CORSOrigins))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("finance-api listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Println("finance-api stopped")
}
