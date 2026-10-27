package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
)

func patchVerdict(t *testing.T, h http.Handler, id string, verdict model.Veredito) {
	t.Helper()
	body := strings.NewReader(`{"verdict":"` + string(verdict) + `"}`)
	rec := call(t, h, http.MethodPatch, "/api/v1/incidents/"+id+"/verdict", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH verdict esperado 200, veio %d: %s", rec.Code, rec.Body.String())
	}
}

// stats GET a rota de acurácia e devolve o corpo cru (para checar o shape JSON).
func stats(t *testing.T, h http.Handler) (model.TriageStats, map[string]any) {
	t.Helper()
	rec := call(t, h, http.MethodGet, "/api/v1/incidents/stats/triage-accuracy", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET stats esperado 200, veio %d: %s", rec.Code, rec.Body.String())
	}
	var out model.TriageStats
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	rec = call(t, h, http.MethodGet, "/api/v1/incidents/stats/triage-accuracy", nil)
	var raw map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode cru: %v", err)
	}
	return out, raw
}

// A rota literal não pode ser capturada por /{id}: responde com agregados, não
// com "incidente não encontrado".
func TestTriageAccuracyStatsRoute(t *testing.T) {
	h := newTestRouter()

	// A IA disse true_positive (fixture) e o analista confirmou.
	confirmed := decode(t, post(t, h, fixture(t)))
	patchVerdict(t, h, confirmed.ID, model.VereditoTruePositive)

	// A IA disse true_positive e o analista corrigiu para false_positive.
	body := fixture(t)
	body["event_id"] = "11111111-1111-4111-8111-111111111111"
	corrected := decode(t, post(t, h, body))
	patchVerdict(t, h, corrected.ID, model.VereditoFalsePositive)

	// Incidente que ninguém revisou.
	body = fixture(t)
	body["event_id"] = "22222222-2222-4222-8222-222222222222"
	post(t, h, body)

	got, raw := stats(t, h)
	if got.TotalIncidents != 3 || got.Reviewed != 2 || got.Agreed != 1 {
		t.Fatalf("totais errados: %+v", got)
	}
	if got.Accuracy == nil || *got.Accuracy != 0.5 {
		t.Fatalf("acurácia esperada 0.5, veio %v", got.Accuracy)
	}
	if len(got.ByEngine) != 1 {
		t.Fatalf("esperado 1 engine: %+v", got.ByEngine)
	}
	if e := got.ByEngine[0]; e.Engine != "llm" || e.Model != "gpt-4o-mini" || e.Reviewed != 2 || e.Agreed != 1 {
		t.Fatalf("recorte por engine errado: %+v", e)
	}
	if len(got.ByAIVerdict) != 1 {
		t.Fatalf("esperado 1 veredito de IA: %+v", got.ByAIVerdict)
	}
	if v := got.ByAIVerdict[0]; v.AIVerdict != "true_positive" || v.Reviewed != 2 || v.Agreed != 1 {
		t.Fatalf("recorte por veredito errado: %+v", v)
	}
	if _, ok := raw["by_engine"].([]any); !ok {
		t.Fatalf("by_engine deve ser lista, veio %T", raw["by_engine"])
	}
}

// Sem revisão humana não existe acurácia: null (e não 0%, que mentiria).
func TestTriageAccuracyStatsWithoutReview(t *testing.T) {
	h := newTestRouter()
	post(t, h, fixture(t))

	got, raw := stats(t, h)
	if got.TotalIncidents != 1 || got.Reviewed != 0 || got.Agreed != 0 {
		t.Fatalf("totais errados: %+v", got)
	}
	if got.Accuracy != nil {
		t.Fatalf("sem revisão a acurácia deve ser null, veio %v", *got.Accuracy)
	}
	if raw["accuracy"] != nil {
		t.Fatalf("JSON deve trazer accuracy: null, veio %v", raw["accuracy"])
	}
	if list, ok := raw["by_ai_verdict"].([]any); !ok || len(list) != 1 {
		t.Fatalf("by_ai_verdict deve listar o veredito da IA: %v", raw["by_ai_verdict"])
	}
}

func TestTriageAccuracyStatsEmpty(t *testing.T) {
	got, raw := stats(t, newTestRouter())
	if got.TotalIncidents != 0 || got.Accuracy != nil {
		t.Fatalf("repo vazio: %+v", got)
	}
	for _, key := range []string{"by_engine", "by_ai_verdict"} {
		if list, ok := raw[key].([]any); !ok || len(list) != 0 {
			t.Fatalf("%s deve ser lista vazia, veio %v", key, raw[key])
		}
	}
}
