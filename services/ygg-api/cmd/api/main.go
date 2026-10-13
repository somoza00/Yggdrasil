package main

import (
	"log"
	"net/http"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/config"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/handler"
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

	log.Printf("ygg-api ouvindo em :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatal(err)
	}
}
