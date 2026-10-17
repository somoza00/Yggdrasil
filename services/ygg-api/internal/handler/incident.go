package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/middleware"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/store"
)

// IncidentHandlers agrupa os handlers REST de incidents sobre uma Store.
type IncidentHandlers struct {
	store store.Store
}

// NewIncidentHandlers constrói os handlers de incidents.
func NewIncidentHandlers(s store.Store) *IncidentHandlers {
	return &IncidentHandlers{store: s}
}

// maxBodyBytes limita o body das requisições (proteção contra payloads enormes).
const maxBodyBytes = 1 << 20

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (h *IncidentHandlers) writeJSON(w http.ResponseWriter, code int, v any) { writeJSON(w, code, v) }

func (h *IncidentHandlers) writeErr(w http.ResponseWriter, code int, msg string) {
	writeErr(w, code, msg)
}

// decodeBody lê JSON com limite de tamanho. Campos desconhecidos são
// ignorados (compatibilidade com versões futuras do muninn). Em erro já
// responde e devolve false.
func (h *IncidentHandlers) decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(v)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		h.writeErr(w, http.StatusRequestEntityTooLarge, "body grande demais")
		return false
	case err != nil:
		h.writeErr(w, http.StatusBadRequest, "body JSON inválido")
		return false
	}
	return true
}

// List responde GET /api/v1/incidents.
func (h *IncidentHandlers) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.List(r.Context())
	if err != nil {
		log.Printf("incidents list: %v", err)
		h.writeErr(w, http.StatusInternalServerError, "falha ao listar incidentes")
		return
	}
	h.writeJSON(w, http.StatusOK, items)
}

// Create responde POST /api/v1/incidents com um TriageResult do muninn.
// Idempotente por event_id: reenvio devolve 200 com o incidente existente.
// id, status e created_at são sempre definidos pelo servidor.
func (h *IncidentHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var in model.TriageResult
	if !h.decodeBody(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		h.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	it, created, err := h.store.Create(r.Context(), in.Incident(time.Now().UTC()))
	if err != nil {
		log.Printf("incidents create: %v", err)
		h.writeErr(w, http.StatusInternalServerError, "falha ao criar incidente")
		return
	}
	code := http.StatusCreated
	if !created {
		code = http.StatusOK
	}
	h.writeJSON(w, code, it)
}

// Register registra as rotas de incidents no mux. A autenticação fica no
// middleware.Auth em volta do mux.
func (h *IncidentHandlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/incidents", h.List)
	mux.HandleFunc("POST /api/v1/incidents", h.Create)
	mux.HandleFunc("GET /api/v1/incidents/{id}", h.GetByID)
	mux.HandleFunc("PATCH /api/v1/incidents/{id}/verdict", h.PatchVerdict)
}

// GetByID responde GET /api/v1/incidents/{id}.
func (h *IncidentHandlers) GetByID(w http.ResponseWriter, r *http.Request) {
	it, err := h.store.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		h.writeErr(w, http.StatusNotFound, "incidente não encontrado")
		return
	}
	if err != nil {
		log.Printf("incidents get: %v", err)
		h.writeErr(w, http.StatusInternalServerError, "erro ao buscar incidente")
		return
	}
	h.writeJSON(w, http.StatusOK, it)
}

// PatchVerdict responde PATCH /api/v1/incidents/{id}/verdict com body
// {verdict}. Quem mudou é o sub do JWT; a store grava a auditoria.
func (h *IncidentHandlers) PatchVerdict(w http.ResponseWriter, r *http.Request) {
	changedBy := middleware.Subject(r.Context())
	if changedBy == "" {
		h.writeErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	var body struct {
		Verdict model.Veredito `json:"verdict"`
	}
	if !h.decodeBody(w, r, &body) {
		return
	}
	if !body.Verdict.Valid() {
		h.writeErr(w, http.StatusBadRequest, "verdict inválido")
		return
	}
	it, err := h.store.UpdateVerdict(r.Context(), r.PathValue("id"), body.Verdict, changedBy)
	if errors.Is(err, store.ErrNotFound) {
		h.writeErr(w, http.StatusNotFound, "incidente não encontrado")
		return
	}
	if err != nil {
		log.Printf("incidents patch verdict: %v", err)
		h.writeErr(w, http.StatusInternalServerError, "erro ao atualizar incidente")
		return
	}
	h.writeJSON(w, http.StatusOK, it)
}
