package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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

func (h *IncidentHandlers) writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *IncidentHandlers) writeErr(w http.ResponseWriter, code int, msg string) {
	h.writeJSON(w, code, map[string]string{"error": msg})
}

// List responde GET /api/v1/incidents.
func (h *IncidentHandlers) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.List(r.Context())
	if err != nil {
		h.writeErr(w, http.StatusInternalServerError, "falha ao listar notificações")
		return
	}
	h.writeJSON(w, http.StatusOK, items)
}

// Create responde POST /api/v1/incidents.
func (h *IncidentHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var in model.Incident
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		h.writeErr(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	if in.Title == "" {
		h.writeErr(w, http.StatusBadRequest, "campo 'title' obrigatório")
		return
	}
	if in.Verdict == "" {
		in.Verdict = model.VereditoReview
	}
	created, err := h.store.Create(r.Context(), &in)
	if err != nil {
		h.writeErr(w, http.StatusInternalServerError, "falha ao criar incidente")
		return
	}
	h.writeJSON(w, http.StatusCreated, created)
}

// Route enruta o handler conforme método, num padrão simples estilo mux.
// Uso: router.HandleFunc("/api/v1/incidents", h.Route)
func (h *IncidentHandlers) Route(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.List(w, r)
	case http.MethodPost:
		h.Create(w, r)
	default:
		h.writeErr(w, http.StatusMethodNotAllowed, "método não suportado")
	}
}

// RouteID enruta /api/v1/incidents/{id} por método (GET).
func (h *IncidentHandlers) RouteID(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.GetByID(w, r)
	case http.MethodPatch:
		h.PatchVerdict(w, r)
	default:
		h.writeErr(w, http.StatusMethodNotAllowed, "método não suportado")
	}
}

func idFromPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// GetByID responde GET /api/v1/incidents/{id}.
func (h *IncidentHandlers) GetByID(w http.ResponseWriter, r *http.Request) {
	it, err := h.store.Get(r.Context(), idFromPath(r.URL.Path))
	if errors.Is(err, store.ErrNotFound) {
		h.writeErr(w, http.StatusNotFound, "incidente não encontrado")
		return
	}
	if err != nil {
		h.writeErr(w, http.StatusInternalServerError, "erro ao buscar incidente")
		return
	}
	h.writeJSON(w, http.StatusOK, it)
}

// PatchVerdict responde PATCH /api/v1/incidents/{id} com body {verdict}.
func (h *IncidentHandlers) PatchVerdict(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Verdict model.Veredito `json:"verdict"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeErr(w, http.StatusBadRequest, "body JSON inválido")
		return
	}
	it, err := h.store.UpdateVerdict(r.Context(), idFromPath(r.URL.Path), body.Verdict)
	if errors.Is(err, store.ErrNotFound) {
		h.writeErr(w, http.StatusNotFound, "incidente não encontrado")
		return
	}
	if err != nil {
		h.writeErr(w, http.StatusInternalServerError, "erro ao atualizar incidente")
		return
	}
	h.writeJSON(w, http.StatusOK, it)
}
