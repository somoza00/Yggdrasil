package main

import (
	"log"
	"net/http"
	"time"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/config"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/handler"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/muninn"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/store"
)

func main() {
	cfg := config.Load()

	st := store.NewMemory()
	incidents := handler.NewIncidentHandlers(st)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.Healthz())
	mux.HandleFunc("GET /api/v1/incidents", incidents.Route)
	mux.HandleFunc("POST /api/v1/incidents", incidents.Route)
	mux.HandleFunc("GET /api/v1/incidents/{id}", incidents.RouteID)
	mux.HandleFunc("PATCH /api/v1/incidents/{id}", incidents.RouteID)
	// Única chamada síncrona ygg-api → muninn (ADR-002).
	mux.HandleFunc("POST /api/v1/triage", handler.TriageProxy(muninn.New(cfg.MunninURL)))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// Acima do timeout do cliente muninn (60s) para o proxy de triagem.
		WriteTimeout: 75 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	log.Printf("ygg-api ouvindo em :%s (muninn=%s)", cfg.Port, cfg.MunninURL)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
