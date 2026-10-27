package store

import (
	"context"
	"testing"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
)

// O veredito da IA é o original (primeiro old_verdict da auditoria) e o final é
// o último new_verdict: um analista que muda e volta atrás conta como
// concordância, e o incidente sem revisão não entra na conta.
func TestPostgresTriageStats(t *testing.T) {
	st, _ := newPostgres(t)
	ctx := context.Background()

	create := func(eventID string) *model.Incident {
		t.Helper()
		it, created, err := st.Create(ctx, incident(eventID))
		if err != nil || !created {
			t.Fatalf("create %s: created=%v err=%v", eventID, created, err)
		}
		return it
	}
	patch := func(id string, v model.Veredito) {
		t.Helper()
		if _, err := st.UpdateVerdict(ctx, id, v, "analyst-1"); err != nil {
			t.Fatalf("patch %s: %v", id, err)
		}
	}

	// 1) analista confirmou o veredito da IA.
	confirmed := create("00000000-0000-4000-8000-000000000011")
	patch(confirmed.ID, model.VereditoTruePositive)

	// 2) analista discordou.
	corrected := create("00000000-0000-4000-8000-000000000012")
	patch(corrected.ID, model.VereditoFalsePositive)

	// 3) analista mudou e voltou atrás: o final coincide com o da IA.
	reverted := create("00000000-0000-4000-8000-000000000013")
	patch(reverted.ID, model.VereditoFalsePositive)
	patch(reverted.ID, model.VereditoTruePositive)

	// 4) sem revisão: conta no total, não na acurácia.
	create("00000000-0000-4000-8000-000000000014")

	// 5) triagem heurística, confirmada pelo analista (recorte por engine).
	heuristicIncident := incident("00000000-0000-4000-8000-000000000015")
	heuristicIncident.Engine = "heuristic"
	heuristicIncident.Model = "heuristic-v1"
	heuristic, created, err := st.Create(ctx, heuristicIncident)
	if err != nil || !created {
		t.Fatalf("create heurístico: created=%v err=%v", created, err)
	}
	patch(heuristic.ID, model.VereditoTruePositive)

	stats, err := st.TriageStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalIncidents != 5 || stats.Reviewed != 4 || stats.Agreed != 3 {
		t.Fatalf("totais errados: %+v", stats)
	}
	if stats.Accuracy == nil || *stats.Accuracy != 0.75 {
		t.Fatalf("acurácia esperada 0.75, veio %v", stats.Accuracy)
	}

	if len(stats.ByEngine) != 2 {
		t.Fatalf("esperado 2 engines, veio %+v", stats.ByEngine)
	}
	if e := stats.ByEngine[0]; e.Engine != "heuristic" || e.Model != "heuristic-v1" ||
		e.Reviewed != 1 || e.Agreed != 1 || e.Accuracy == nil || *e.Accuracy != 1 {
		t.Fatalf("recorte heuristic errado: %+v", e)
	}
	if e := stats.ByEngine[1]; e.Engine != "llm" || e.Model != "gpt-4o-mini" ||
		e.Reviewed != 3 || e.Agreed != 2 || e.Accuracy == nil || *e.Accuracy != 0.6667 {
		t.Fatalf("recorte llm errado: %+v", e)
	}

	if len(stats.ByAIVerdict) != 1 {
		t.Fatalf("esperado 1 veredito de IA, veio %+v", stats.ByAIVerdict)
	}
	if v := stats.ByAIVerdict[0]; v.AIVerdict != "true_positive" || v.Reviewed != 4 || v.Agreed != 3 {
		t.Fatalf("recorte por veredito errado: %+v", v)
	}
}

func TestPostgresTriageStatsWithoutIncidents(t *testing.T) {
	st, _ := newPostgres(t)
	stats, err := st.TriageStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalIncidents != 0 || stats.Reviewed != 0 || stats.Accuracy != nil {
		t.Fatalf("sem incidentes a acurácia não existe: %+v", stats)
	}
	if stats.ByEngine == nil || stats.ByAIVerdict == nil {
		t.Fatalf("recortes devem ser listas vazias, não null: %+v", stats)
	}
}
