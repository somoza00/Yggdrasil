package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/config"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/handler"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/middleware"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/muninn"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/store"
	"github.com/somoza00/yggdrasil/services/ygg-api/migrations"
)

// startupTimeout limita a espera pelo Postgres e as migrations.
const startupTimeout = 60 * time.Second

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config inválida: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connectAndMigrate(ctx, cfg.DBURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.Healthz())
	handler.NewIncidentHandlers(store.NewPostgres(pool)).Register(mux)
	// Chamada síncrona ygg-api → muninn (ADR-002).
	mux.HandleFunc("POST /api/v1/triage", handler.TriageProxy(muninn.New(cfg.MunninURL)))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           middleware.Auth([]byte(cfg.JWTSecret), mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// POST /api/v1/triage estende o próprio prazo (handler.TriageProxy).
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Printf("ygg-api ouvindo em :%s (env=%s muninn=%s)", cfg.Port, cfg.Env, cfg.MunninURL)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("servidor: %v", err)
		}
	case <-ctx.Done():
		log.Print("encerrando")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}
}

// connectAndMigrate espera o Postgres e aplica as migrations antes de o
// servidor HTTP subir: sem schema, nenhuma rota funciona.
func connectAndMigrate(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	pool, err := store.Connect(ctx, dbURL)
	if err != nil {
		return nil, err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	if err := migrations.Up(ctx, db); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
