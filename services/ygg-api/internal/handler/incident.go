package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
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

// Defaults e teto da listagem. Sem teto, GET /api/v1/incidents devolvia a fila
// INTEIRA: numa base com histórico, a resposta passa de dezenas de MB e o
// painel do analista carrega tudo para mostrar a primeira tela.
const (
	defaultListLimit = 50
	maxListLimit     = 200
	maxSeverityChars = 40
)

// listFilter traduz a query string em store.ListFilter.
//
// Em erro devolve a mensagem para o cliente: valor inválido aqui NÃO é
// ignorado em silêncio (um `?limit=abc` respondendo a lista inteira é pior do
// que um 400 dizendo o formato certo).
func listFilter(r *http.Request) (store.ListFilter, error) {
	q := r.URL.Query()
	f := store.ListFilter{Limit: defaultListLimit}

	if raw := strings.TrimSpace(q.Get("verdict")); raw != "" {
		verdict := model.Veredito(strings.ToLower(raw))
		if !verdict.Valid() {
			return f, errors.New(
				"verdict inválido: use open, true_positive, false_positive ou review",
			)
		}
		f.Verdict = verdict
	}
	if raw := strings.TrimSpace(q.Get("severity")); raw != "" {
		if len(raw) > maxSeverityChars {
			return f, fmt.Errorf("severity inválida: máximo de %d caracteres", maxSeverityChars)
		}
		f.Severity = raw
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxListLimit {
			return f, fmt.Errorf("limit inválido: use um inteiro entre 1 e %d", maxListLimit)
		}
		f.Limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return f, errors.New("offset inválido: use um inteiro >= 0")
		}
		f.Offset = n
	}
	return f, nil
}

// List responde GET /api/v1/incidents?verdict=&severity=&limit=&offset=.
//
// Filtros e paginação existem para o painel do analista: antes só havia "tudo".
// A ordem é created_at DESC (id desempata), então `offset += limit` percorre a
// fila sem pular nem repetir incidente.
func (h *IncidentHandlers) List(w http.ResponseWriter, r *http.Request) {
	filter, err := listFilter(r)
	if err != nil {
		h.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := h.store.List(r.Context(), filter)
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
	// Rota literal: o ServeMux casa o padrão mais específico, então ela não é
	// capturada por {id}.
	mux.HandleFunc("GET /api/v1/incidents/stats/triage-accuracy", h.Stats)
	mux.HandleFunc("GET /api/v1/incidents/{id}", h.GetByID)
	mux.HandleFunc("PATCH /api/v1/incidents/{id}/verdict", h.PatchVerdict)
}

// Stats responde GET /api/v1/incidents/stats/triage-accuracy: concordância
// entre o veredito da IA e o veredito final do analista (fonte: incident_audit).
func (h *IncidentHandlers) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.TriageStats(r.Context())
	if err != nil {
		log.Printf("incidents stats: %v", err)
		h.writeErr(w, http.StatusInternalServerError, "falha ao calcular a acurácia da triagem")
		return
	}
	h.writeJSON(w, http.StatusOK, stats)
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
