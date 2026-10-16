package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/store"
)

// Fixture compartilhada com o muninn (contrato do POST /api/v1/incidents).
const triageFixture = "../../../../proto/triage/fixtures/triage_result.json"

func newTestRouter() *IncidentHandlers {
	return NewIncidentHandlers(store.NewMemory())
}

func fixture(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(triageFixture)
	if err != nil {
		t.Fatalf("lendo fixture: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("fixture inválida: %v", err)
	}
	return m
}

func post(t *testing.T, h *IncidentHandlers, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	switch b := body.(type) {
	case string:
		buf.WriteString(b)
	default:
		if err := json.NewEncoder(&buf).Encode(b); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	h.Route(rec, httptest.NewRequest(http.MethodPost, "/api/v1/incidents", &buf))
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) model.Incident {
	t.Helper()
	var it model.Incident
	if err := json.NewDecoder(rec.Body).Decode(&it); err != nil {
		t.Fatalf("decode falhou: %v", err)
	}
	return it
}

func TestCreateFromTriageResult(t *testing.T) {
	h := newTestRouter()
	rec := post(t, h, fixture(t))
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, veio %d: %s", rec.Code, rec.Body.String())
	}
	created := decode(t, rec)
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatalf("id/created_at não gerados: %+v", created)
	}
	want := model.Incident{
		EventID:           "c9621933-88bf-5aac-afc8-9c3348befd2c",
		Title:             "Beacon to c2.evil.net",
		Severity:          "critical",
		Source:            "cef",
		Category:          "command_and_control",
		Verdict:           model.VereditoTruePositive,
		Confidence:        0.9,
		Engine:            "llm",
		Model:             "gpt-4o-mini",
		Status:            model.StatusOpen,
		Rationale:         "Outbound beacon to a known C2 domain from an internal host.",
		RecommendedAction: "Isolate 10.0.0.5 and block c2.evil.net.",
	}
	got := created
	got.ID, got.CreatedAt, got.IOCs = "", want.CreatedAt, nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("incidente mapeado errado:\n got  %+v\n want %+v", got, want)
	}
	if len(created.IOCs) != 3 || created.IOCs[0] != (model.IOC{Kind: "domain", Value: "c2.evil.net"}) {
		t.Fatalf("iocs mapeados errado: %+v", created.IOCs)
	}

	rec2 := httptest.NewRecorder()
	h.RouteID(rec2, httptest.NewRequest(http.MethodGet, "/api/v1/incidents/"+created.ID, nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET por id esperado 200, veio %d", rec2.Code)
	}
}

// A entrega huginn→muninn é at-least-once (ADR-005): o mesmo event_id pode
// chegar duas vezes e não pode gerar dois incidentes.
func TestCreateIsIdempotentPerEventID(t *testing.T) {
	h := newTestRouter()
	first := decode(t, post(t, h, fixture(t)))
	rec := post(t, h, fixture(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("reenvio esperado 200, veio %d", rec.Code)
	}
	if again := decode(t, rec); again.ID != first.ID {
		t.Fatalf("reenvio criou outro incidente: %s != %s", again.ID, first.ID)
	}
	list := httptest.NewRecorder()
	h.Route(list, httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil))
	var items []model.Incident
	_ = json.NewDecoder(list.Body).Decode(&items)
	if len(items) != 1 {
		t.Fatalf("esperado 1 incidente, veio %d", len(items))
	}
}

func TestCreateIgnoresClientIDAndUnknownFields(t *testing.T) {
	h := newTestRouter()
	body := fixture(t)
	body["id"] = "attacker-chosen"
	body["status"] = "closed"
	body["future_field"] = map[string]any{"x": 1}
	rec := post(t, h, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, veio %d: %s", rec.Code, rec.Body.String())
	}
	created := decode(t, rec)
	if created.ID == "attacker-chosen" || created.Status != model.StatusOpen {
		t.Fatalf("campos do cliente não podem definir id/status: %+v", created)
	}
}

func TestCreateRejectsInvalidTriageResult(t *testing.T) {
	cases := map[string]func(m map[string]any){
		"sem title":          func(m map[string]any) { delete(m, "title") },
		"event_id inválido":  func(m map[string]any) { m["event_id"] = "nope" },
		"verdict inválido":   func(m map[string]any) { m["verdict"] = "pwned" },
		"engine inválido":    func(m map[string]any) { m["engine"] = "magic" },
		"severity inválida":  func(m map[string]any) { m["severity"] = "urgent" },
		"confidence > 1":     func(m map[string]any) { m["confidence"] = 1.5 },
		"confidence < 0":     func(m map[string]any) { m["confidence"] = -0.1 },
		"sem category":       func(m map[string]any) { delete(m, "category") },
		"sem model":          func(m map[string]any) { m["model"] = "" },
		"ioc com kind vazio": func(m map[string]any) { m["iocs"] = []any{map[string]any{"kind": "", "value": "x"}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			body := fixture(t)
			mutate(body)
			if rec := post(t, newTestRouter(), body); rec.Code != http.StatusBadRequest {
				t.Fatalf("esperado 400, veio %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
	t.Run("json inválido", func(t *testing.T) {
		if rec := post(t, newTestRouter(), "{"); rec.Code != http.StatusBadRequest {
			t.Fatalf("esperado 400, veio %d", rec.Code)
		}
	})
	t.Run("body grande demais", func(t *testing.T) {
		body := fixture(t)
		body["rationale"] = strings.Repeat("a", maxBodyBytes)
		if rec := post(t, newTestRouter(), body); rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("esperado 413, veio %d", rec.Code)
		}
	})
}

func TestPatchVerdict(t *testing.T) {
	h := newTestRouter()
	created := decode(t, post(t, h, fixture(t)))

	patch := bytes.NewBufferString(`{"verdict":"false_positive"}`)
	rec := httptest.NewRecorder()
	h.RouteID(rec, httptest.NewRequest(http.MethodPatch, "/api/v1/incidents/"+created.ID, patch))
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH esperado 200, veio %d", rec.Code)
	}
	if updated := decode(t, rec); updated.Verdict != model.VereditoFalsePositive {
		t.Fatalf("esperado false_positive, veio %q", updated.Verdict)
	}
}

func TestPatchRejectsInvalidVerdict(t *testing.T) {
	h := newTestRouter()
	created := decode(t, post(t, h, fixture(t)))
	rec := httptest.NewRecorder()
	h.RouteID(rec, httptest.NewRequest(http.MethodPatch, "/api/v1/incidents/"+created.ID,
		bytes.NewBufferString(`{"verdict":"whatever"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, veio %d", rec.Code)
	}
}

func TestGetNotFound(t *testing.T) {
	h := newTestRouter()
	rec := httptest.NewRecorder()
	h.RouteID(rec, httptest.NewRequest(http.MethodGet, "/api/v1/incidents/nao-existe", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, veio %d", rec.Code)
	}
}

// muninn (N réplicas) cria incidentes enquanto clientes listam: com -race
// isso pega acesso concorrente ao map da store.
func TestConcurrentCreateAndList(t *testing.T) {
	h := newTestRouter()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			body := fixture(t)
			body["event_id"] = fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
			if rec := post(t, h, body); rec.Code != http.StatusCreated {
				t.Errorf("create %d: %d", i, rec.Code)
			}
		}()
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.Route(rec, httptest.NewRequest(http.MethodGet, "/api/v1/incidents", nil))
		}()
	}
	wg.Wait()
}
