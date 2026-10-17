package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/middleware"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
	"github.com/somoza00/yggdrasil/services/ygg-api/internal/store"
)

// Fixture compartilhada com o muninn (contrato do POST /api/v1/incidents).
const triageFixture = "../../../../proto/triage/fixtures/triage_result.json"

var testSecret = []byte("handler-test-secret-at-least-32-bytes")

// testSubject é o sub do token usado nas requisições dos testes.
const testSubject = "analyst-1"

// newTestRouter monta as rotas como no main: mux de incidents atrás do Auth.
func newTestRouter() http.Handler {
	mux := http.NewServeMux()
	NewIncidentHandlers(store.NewMemory()).Register(mux)
	return middleware.Auth(testSecret, mux)
}

// call faz a requisição autenticada como testSubject.
func call(t *testing.T, h http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	tok, err := middleware.NewToken(testSecret, testSubject, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
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

func post(t *testing.T, h http.Handler, body any) *httptest.ResponseRecorder {
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
	return call(t, h, http.MethodPost, "/api/v1/incidents", &buf)
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
	got.ID, got.CreatedAt, got.UpdatedAt, got.IOCs, got.RawEvent = "", want.CreatedAt, want.UpdatedAt, nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("incidente mapeado errado:\n got  %+v\n want %+v", got, want)
	}
	if len(created.IOCs) != 3 || created.IOCs[0] != (model.IOC{Kind: "domain", Value: "c2.evil.net"}) {
		t.Fatalf("iocs mapeados errado: %+v", created.IOCs)
	}

	var raw map[string]any
	if err := json.Unmarshal(created.RawEvent, &raw); err != nil || raw["event_id"] != want.EventID {
		t.Fatalf("raw_event não preservado: %s (%v)", created.RawEvent, err)
	}
	if created.VerdictChangedBy != nil || created.VerdictChangedAt != nil {
		t.Fatalf("incidente novo não tem mudança de verdict: %+v", created)
	}

	rec2 := call(t, h, http.MethodGet, "/api/v1/incidents/"+created.ID, nil)
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
	list := call(t, h, http.MethodGet, "/api/v1/incidents", nil)
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
		"verdict open":       func(m map[string]any) { m["verdict"] = "open" },
		"raw_event array":    func(m map[string]any) { m["raw_event"] = []any{1} },
		"raw_event string":   func(m map[string]any) { m["raw_event"] = "x" },
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

	for _, v := range []model.Veredito{model.VereditoFalsePositive, model.VereditoOpen} {
		rec := call(t, h, http.MethodPatch, "/api/v1/incidents/"+created.ID+"/verdict",
			bytes.NewBufferString(fmt.Sprintf(`{"verdict":%q}`, v)))
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH %s esperado 200, veio %d: %s", v, rec.Code, rec.Body.String())
		}
		updated := decode(t, rec)
		if updated.Verdict != v {
			t.Fatalf("esperado %s, veio %q", v, updated.Verdict)
		}
		// Quem mudou vem do sub do JWT, nunca do body.
		if updated.VerdictChangedBy == nil || *updated.VerdictChangedBy != testSubject ||
			updated.VerdictChangedAt == nil {
			t.Fatalf("changed_by/changed_at não registrados: %+v", updated)
		}
	}
}

func TestPatchIgnoresChangedByFromBody(t *testing.T) {
	h := newTestRouter()
	created := decode(t, post(t, h, fixture(t)))
	rec := call(t, h, http.MethodPatch, "/api/v1/incidents/"+created.ID+"/verdict",
		bytes.NewBufferString(`{"verdict":"review","verdict_changed_by":"someone-else"}`))
	if got := decode(t, rec); got.VerdictChangedBy == nil || *got.VerdictChangedBy != testSubject {
		t.Fatalf("changed_by deveria ser %q: %+v", testSubject, got.VerdictChangedBy)
	}
}

func TestPatchOldRouteIsGone(t *testing.T) {
	h := newTestRouter()
	created := decode(t, post(t, h, fixture(t)))
	rec := call(t, h, http.MethodPatch, "/api/v1/incidents/"+created.ID,
		bytes.NewBufferString(`{"verdict":"review"}`))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PATCH sem /verdict esperado 405, veio %d", rec.Code)
	}
}

func TestPatchUnknownIncident(t *testing.T) {
	rec := call(t, newTestRouter(), http.MethodPatch,
		"/api/v1/incidents/00000000-0000-4000-8000-000000000000/verdict",
		bytes.NewBufferString(`{"verdict":"review"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, veio %d", rec.Code)
	}
}

// raw_event é opcional no contrato: ausente ou null vira {}.
func TestCreateWithoutRawEvent(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"ausente": func(m map[string]any) { delete(m, "raw_event") },
		"null":    func(m map[string]any) { m["raw_event"] = nil },
	} {
		t.Run(name, func(t *testing.T) {
			body := fixture(t)
			mutate(body)
			rec := post(t, newTestRouter(), body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("esperado 201, veio %d: %s", rec.Code, rec.Body.String())
			}
			if got := decode(t, rec); string(got.RawEvent) != "{}" {
				t.Fatalf("raw_event esperado {}, veio %s", got.RawEvent)
			}
		})
	}
}

func TestRoutesRequireToken(t *testing.T) {
	h := newTestRouter()
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/incidents"},
		{http.MethodPost, "/api/v1/incidents"},
		{http.MethodGet, "/api/v1/incidents/00000000-0000-4000-8000-000000000000"},
		{http.MethodPatch, "/api/v1/incidents/00000000-0000-4000-8000-000000000000/verdict"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, bytes.NewBufferString(`{}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s sem token: esperado 401, veio %d", c.method, c.path, rec.Code)
		}
	}
}

func TestPatchRejectsInvalidVerdict(t *testing.T) {
	h := newTestRouter()
	created := decode(t, post(t, h, fixture(t)))
	rec := call(t, h, http.MethodPatch, "/api/v1/incidents/"+created.ID+"/verdict",
		bytes.NewBufferString(`{"verdict":"whatever"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, veio %d", rec.Code)
	}
}

func TestGetNotFound(t *testing.T) {
	h := newTestRouter()
	rec := call(t, h, http.MethodGet, "/api/v1/incidents/nao-existe", nil)
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
			call(t, h, http.MethodGet, "/api/v1/incidents", nil)
		}()
	}
	wg.Wait()
}
