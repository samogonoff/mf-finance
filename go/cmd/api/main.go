package main

import (
	"context"
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
	"github.com/company/finance-api/internal/internalapi"
	"github.com/company/finance-api/internal/notifications"
	"github.com/company/finance-api/internal/redisx"
	"github.com/company/finance-api/internal/reports/debt"
	"github.com/company/finance-api/internal/users"
)

func main() {
	cfg := config.Load()

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
	mux.HandleFunc("GET /api/auth/me", auth.RequireBearer(authSvc, authH.Me))
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
	// Скриншоты — открытая раздача (только по UUID-имени).
	mux.HandleFunc("GET /uploads/bugtracker/", bugH.ServeUpload)

	// Reports — Задолженность ВГО.
	// В DEBT_MOCK=1 отдаются фикстуры. В live-режиме нужны MSSQL_PREMASTER_*; по
	// умолчанию ходим в снэпшот Premaster1C_20260514, к живой Premaster1C не лезем.
	var premasterRepo debt.PremasterRepo
	if !cfg.DebtMock {
		mssqlDB, err := debt.NewPremasterRepo(cfg.PremasterServer, cfg.PremasterDatabase, cfg.PremasterUser, cfg.PremasterPassword)
		if err != nil {
			log.Fatalf("debt: mssql open: %v", err)
		}
		premasterRepo = debt.WrapPremasterRepo(mssqlDB)
		if mssqlDB != nil {
			defer mssqlDB.Close()
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
	mux.HandleFunc("GET /api/reports/debt/filter-options", auth.RequireBearer(authSvc, debtH.FilterOptions))
	mux.HandleFunc("GET /api/reports/debt/report", auth.RequireBearer(authSvc, debtH.Report))
	mux.HandleFunc("GET /api/reports/debt/drilldown", auth.RequireBearer(authSvc, debtH.Drilldown))
	mux.HandleFunc("GET /api/reports/debt/saved-filters", auth.RequireBearer(authSvc, debtH.ListSavedFilters))
	mux.HandleFunc("POST /api/reports/debt/saved-filters", auth.RequireBearer(authSvc, debtH.CreateSavedFilter))
	mux.HandleFunc("DELETE /api/reports/debt/saved-filters", auth.RequireBearer(authSvc, debtH.DeleteSavedFilter))

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           withCORS(mux, cfg.CORSOrigins),
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
