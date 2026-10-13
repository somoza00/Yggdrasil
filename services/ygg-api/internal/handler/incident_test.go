package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/store"
)

func newTestRouter() *IncidentHandlers {
	return NewIncidentHandlers(store.NewMemory())
}

func TestCreateAndGetIncident(t *testing.T) {
	h := newTestRouter()

	body := bytes.NewBufferString(`{"title":"Alerta EDR","severity":"high","source":"edr"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/incidents", body)
	rec := httptest.NewRecorder()
	h.Route(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, veio %d: %s", rec.Code, rec.Body.String())
	}
	var created model.Incident
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created sem ID")
	}
	if created.Verdict != model.VereditoReview {
		t.Fatalf("verdict default deveria ser review, veio %q", created.Verdict)
	}

	// GET com o id gerado
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/"+created.ID, nil)
	rec2 := httptest.NewRecorder()
	h.RouteID(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET por id esperado 200, veio %d", rec2.Code)
	}
}

func TestCreateRejectsEmptyTitle(t *testing.T) {
	h := newTestRouter()
	body := bytes.NewBufferString(`{"severity":"high"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/incidents", body)
	rec := httptest.NewRecorder()
	h.Route(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, veio %d", rec.Code)
	}
}

func TestPatchVerdict(t *testing.T) {
	h := newTestRouter()
	body := bytes.NewBufferString(`{"title":"X"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/incidents", body)
	rec := httptest.NewRecorder()
	h.Route(rec, req)

	var created model.Incident
	_ = json.NewDecoder(rec.Body).Decode(&created)

	patch := bytes.NewBufferString(`{"verdict":"true_positive"}`)
	req2 := httptest.NewRequest(http.MethodPatch, "/api/v1/incidents/"+created.ID, patch)
	rec2 := httptest.NewRecorder()
	h.RouteID(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("PATCH esperado 200, veio %d", rec2.Code)
	}
	var updated model.Incident
	_ = json.NewDecoder(rec2.Body).Decode(&updated)
	if updated.Verdict != model.VereditoTruePositive {
		t.Fatalf("esperado true_positive, veio %q", updated.Verdict)
	}
}

func TestGetNotFound(t *testing.T) {
	h := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/incidents/nao-existe", nil)
	rec := httptest.NewRecorder()
	h.RouteID(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, veio %d", rec.Code)
	}
}
