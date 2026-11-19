package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
)

func listItem(severity string, verdict model.Veredito, at time.Time) *model.Incident {
	return &model.Incident{Severity: severity, Verdict: verdict, CreatedAt: at}
}

func TestListFilterMatch(t *testing.T) {
	it := listItem("Critical", model.VereditoTruePositive, time.Now())
	cases := []struct {
		name string
		f    ListFilter
		want bool
	}{
		{"sem filtro", ListFilter{}, true},
		{"veredito igual", ListFilter{Verdict: model.VereditoTruePositive}, true},
		{"veredito diferente", ListFilter{Verdict: model.VereditoReview}, false},
		{"severidade com outra caixa", ListFilter{Severity: "critical"}, true},
		{"severidade diferente", ListFilter{Severity: "low"}, false},
		{"veredito igual e severidade diferente", ListFilter{Verdict: model.VereditoTruePositive, Severity: "low"}, false},
	}
	for _, tc := range cases {
		if got := tc.f.Match(it); got != tc.want {
			t.Errorf("%s: Match = %v, esperado %v", tc.name, got, tc.want)
		}
	}
}

func TestListFilterPaginate(t *testing.T) {
	base := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	items := []*model.Incident{}
	for i := range 5 {
		it := &model.Incident{ID: fmt.Sprintf("id-%d", i), CreatedAt: base.Add(time.Duration(-i) * time.Minute)}
		items = append(items, it)
	}

	if got := (ListFilter{}).Paginate(items); len(got) != 5 {
		t.Errorf("sem filtro: %d itens, esperado 5", len(got))
	}
	if got := (ListFilter{Limit: 2}).Paginate(items); len(got) != 2 || got[0].ID != "id-0" {
		t.Errorf("limit=2: %d itens a partir de %s", len(got), got[0].ID)
	}
	if got := (ListFilter{Offset: 3}).Paginate(items); len(got) != 2 || got[0].ID != "id-3" {
		t.Errorf("offset=3: %d itens a partir de %s", len(got), got[0].ID)
	}
	if got := (ListFilter{Offset: 99}).Paginate(items); len(got) != 0 {
		t.Errorf("offset além do fim devia ser vazio, veio %d", len(got))
	}
	if got := (ListFilter{Offset: -5, Limit: 1}).Paginate(items); len(got) != 1 || got[0].ID != "id-0" {
		t.Errorf("offset negativo devia ser 0, veio %+v", got)
	}
}

func TestMemoryListFiltersAndTiebreaks(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	// Mesmo instante de propósito: a ordem tem de vir do desempate por id, senão
	// a página por offset muda entre chamadas.
	for _, spec := range []struct {
		event, severity string
		verdict         model.Veredito
	}{
		{"11111111-0000-4000-8000-000000000001", "critical", model.VereditoTruePositive},
		{"22222222-0000-4000-8000-000000000002", "low", model.VereditoFalsePositive},
		{"33333333-0000-4000-8000-000000000003", "critical", model.VereditoReview},
	} {
		in := model.Incident{
			EventID:   spec.event,
			Severity:  spec.severity,
			Verdict:   spec.verdict,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, _, err := m.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
	}

	all, err := m.List(ctx, ListFilter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("list: %d %v", len(all), err)
	}
	for i := 1; i < len(all); i++ {
		prev, cur := all[i-1], all[i]
		if cur.CreatedAt.After(prev.CreatedAt) || (cur.CreatedAt.Equal(prev.CreatedAt) && cur.ID < prev.ID) {
			t.Fatalf("ordem instável: %s (%s) antes de %s (%s)", prev.ID, prev.CreatedAt, cur.ID, cur.CreatedAt)
		}
	}

	crit, err := m.List(ctx, ListFilter{Severity: "critical"})
	if err != nil || len(crit) != 2 {
		t.Fatalf("filtro de severidade: %d %v", len(crit), err)
	}
	review, err := m.List(ctx, ListFilter{Verdict: model.VereditoReview})
	if err != nil || len(review) != 1 || review[0].Verdict != model.VereditoReview {
		t.Fatalf("filtro de veredito: %+v %v", review, err)
	}
	page, err := m.List(ctx, ListFilter{Limit: 1, Offset: 1})
	if err != nil || len(page) != 1 || page[0].ID != all[1].ID {
		t.Fatalf("página: %+v %v", page, err)
	}
}
