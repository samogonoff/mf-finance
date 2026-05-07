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
	"github.com/company/finance-api/internal/config"
	"github.com/company/finance-api/internal/db"
	"github.com/company/finance-api/internal/redisx"
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

	users := auth.NewUserRepo(pool)
	tokens := auth.NewTokenRepo(rdb)
	authSvc := auth.NewService(users, tokens)
	authH := auth.NewHandler(authSvc)

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
