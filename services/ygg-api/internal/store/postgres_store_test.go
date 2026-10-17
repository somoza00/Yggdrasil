package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
	"github.com/somoza00/yggdrasil/services/ygg-api/migrations"
)

// Testes de integração: precisam de um Postgres descartável em
// YGG_TEST_DATABASE_URL (as tabelas são truncadas). Sem a variável, pulam.
//
//	docker run -d --rm -p 55432:5432 -e POSTGRES_PASSWORD=test postgres:17-alpine
//	YGG_TEST_DATABASE_URL=postgres://postgres:test@127.0.0.1:55432/postgres?sslmode=disable go test ./...
func newPostgres(t *testing.T) (*Postgres, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("YGG_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("YGG_TEST_DATABASE_URL não definido")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	// Duas vezes: a segunda execução não pode falhar (startup de cada réplica).
	for range 2 {
		if err := migrations.Up(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `TRUNCATE incident_audit, incidents`); err != nil {
		t.Fatal(err)
	}
	return NewPostgres(pool), pool
}

func incident(eventID string) model.Incident {
	return model.TriageResult{
		EventID:    eventID,
		Title:      "Beacon to c2.evil.net",
		Source:     "cef",
		Severity:   "critical",
		IOCs:       []model.IOC{{Kind: "domain", Value: "c2.evil.net"}},
		Verdict:    model.VereditoTruePositive,
		Confidence: 0.9,
		Category:   "command_and_control",
		Engine:     "llm",
		Model:      "gpt-4o-mini",
		RawEvent:   json.RawMessage(`{"schema_version":"1.0","raw":"CEF:0|ACME"}`),
	}.Incident(time.Now())
}

func TestPostgresCreateGeneratesIDAndRoundTrips(t *testing.T) {
	st, _ := newPostgres(t)
	ctx := context.Background()
	in := incident("c9621933-88bf-5aac-afc8-9c3348befd2c")
	in.ID = "attacker-chosen" // nunca vai para o banco

	got, created, err := st.Create(ctx, in)
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	if !uuidRe.MatchString(got.ID) || got.ID == in.ID {
		t.Fatalf("id não gerado pelo banco: %q", got.ID)
	}
	if got.CreatedAt.IsZero() || !got.UpdatedAt.Equal(got.CreatedAt) {
		t.Fatalf("timestamps: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
	}
	var raw map[string]any
	if err := json.Unmarshal(got.RawEvent, &raw); err != nil || raw["raw"] != "CEF:0|ACME" {
		t.Fatalf("raw_event: %s (%v)", got.RawEvent, err)
	}
	if len(got.IOCs) != 1 || got.IOCs[0].Value != "c2.evil.net" || got.Verdict != model.VereditoTruePositive ||
		got.Status != model.StatusOpen || got.VerdictChangedBy != nil {
		t.Fatalf("incidente gravado errado: %+v", got)
	}

	again, err := st.Get(ctx, got.ID)
	if err != nil || again.ID != got.ID || again.Title != in.Title {
		t.Fatalf("get: %+v %v", again, err)
	}
}

func TestPostgresCreateIsIdempotentPerEventID(t *testing.T) {
	st, _ := newPostgres(t)
	ctx := context.Background()
	first, _, err := st.Create(ctx, incident("00000000-0000-4000-8000-000000000001"))
	if err != nil {
		t.Fatal(err)
	}
	dup := incident("00000000-0000-4000-8000-000000000001")
	dup.Title = "outro título"
	second, created, err := st.Create(ctx, dup)
	if err != nil || created || second.ID != first.ID || second.Title != first.Title {
		t.Fatalf("reenvio: created=%v id=%s title=%q err=%v", created, second.ID, second.Title, err)
	}
}

// N réplicas do muninn podem entregar o mesmo event_id ao mesmo tempo.
func TestPostgresConcurrentCreateSameEvent(t *testing.T) {
	st, pool := newPostgres(t)
	ctx := context.Background()
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
	)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, c, err := st.Create(ctx, incident("00000000-0000-4000-8000-000000000002"))
			if err != nil {
				t.Error(err)
				return
			}
			if c {
				mu.Lock()
				created++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM incidents`).Scan(&n)
	if created != 1 || n != 1 {
		t.Fatalf("esperado 1 criado e 1 linha, veio created=%d linhas=%d", created, n)
	}
}

func TestPostgresGetNotFound(t *testing.T) {
	st, _ := newPostgres(t)
	for _, id := range []string{"nao-existe", "", "00000000-0000-4000-8000-000000000000", "1' OR '1'='1"} {
		if _, err := st.Get(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q): esperado ErrNotFound, veio %v", id, err)
		}
	}
}

func TestPostgresListNewestFirst(t *testing.T) {
	st, _ := newPostgres(t)
	ctx := context.Background()
	for i := range 3 {
		if _, _, err := st.Create(ctx, incident(fmt.Sprintf("00000000-0000-4000-8000-%012d", 10+i))); err != nil {
			t.Fatal(err)
		}
	}
	items, err := st.List(ctx)
	if err != nil || len(items) != 3 {
		t.Fatalf("list: %d %v", len(items), err)
	}
	for i := 1; i < len(items); i++ {
		if items[i].CreatedAt.After(items[i-1].CreatedAt) {
			t.Fatalf("fora de ordem: %v depois de %v", items[i].CreatedAt, items[i-1].CreatedAt)
		}
	}
}

func TestPostgresListEmptyIsNotNil(t *testing.T) {
	st, _ := newPostgres(t)
	items, err := st.List(context.Background())
	if err != nil || items == nil {
		t.Fatalf("lista vazia deve ser [] (JSON), veio %v %v", items, err)
	}
}

type auditRow struct {
	old, new, by string
	changedAt    time.Time
}

func audit(t *testing.T, pool *pgxpool.Pool, id string) []auditRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT old_verdict, new_verdict, changed_by, changed_at
		FROM incident_audit WHERE incident_id = $1 ORDER BY id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.old, &r.new, &r.by, &r.changedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestPostgresUpdateVerdictWritesAudit(t *testing.T) {
	st, pool := newPostgres(t)
	ctx := context.Background()
	it, _, err := st.Create(ctx, incident("00000000-0000-4000-8000-000000000020"))
	if err != nil {
		t.Fatal(err)
	}

	upd, err := st.UpdateVerdict(ctx, it.ID, model.VereditoFalsePositive, "analyst-1")
	if err != nil {
		t.Fatal(err)
	}
	if upd.Verdict != model.VereditoFalsePositive || upd.VerdictChangedBy == nil ||
		*upd.VerdictChangedBy != "analyst-1" || upd.VerdictChangedAt == nil || !upd.UpdatedAt.After(it.UpdatedAt) {
		t.Fatalf("update: %+v", upd)
	}
	// Todo PATCH gera linha, mesmo sem mudar o valor.
	if _, err := st.UpdateVerdict(ctx, it.ID, model.VereditoFalsePositive, "analyst-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateVerdict(ctx, it.ID, model.VereditoOpen, "analyst-1"); err != nil {
		t.Fatal(err)
	}

	got := audit(t, pool, it.ID)
	want := []struct{ old, new, by string }{
		{"true_positive", "false_positive", "analyst-1"},
		{"false_positive", "false_positive", "analyst-2"},
		{"false_positive", "open", "analyst-1"},
	}
	if len(got) != len(want) {
		t.Fatalf("esperado %d linhas de auditoria, veio %d", len(want), len(got))
	}
	for i, w := range want {
		if got[i].old != w.old || got[i].new != w.new || got[i].by != w.by {
			t.Errorf("linha %d: %+v, esperado %+v", i, got[i], w)
		}
	}
	if !got[0].changedAt.Equal(*upd.VerdictChangedAt) {
		t.Errorf("changed_at %v != verdict_changed_at %v", got[0].changedAt, *upd.VerdictChangedAt)
	}
}

// PATCHes concorrentes: cada linha de auditoria aponta o verdict que de fato
// substituiu (encadeamento old -> new sem buracos).
func TestPostgresConcurrentUpdatesChainAudit(t *testing.T) {
	st, pool := newPostgres(t)
	ctx := context.Background()
	it, _, err := st.Create(ctx, incident("00000000-0000-4000-8000-000000000021"))
	if err != nil {
		t.Fatal(err)
	}
	verdicts := []model.Veredito{model.VereditoFalsePositive, model.VereditoReview, model.VereditoOpen, model.VereditoTruePositive}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.UpdateVerdict(ctx, it.ID, verdicts[i%len(verdicts)], fmt.Sprintf("a%d", i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	rows := audit(t, pool, it.ID)
	if len(rows) != 20 {
		t.Fatalf("esperado 20 linhas, veio %d", len(rows))
	}
	prev := string(model.VereditoTruePositive)
	for i, r := range rows {
		if r.old != prev {
			t.Fatalf("linha %d: old=%s, esperado %s (auditoria inconsistente)", i, r.old, prev)
		}
		prev = r.new
	}
	final, _ := st.Get(ctx, it.ID)
	if string(final.Verdict) != prev {
		t.Fatalf("verdict final %s != último new_verdict %s", final.Verdict, prev)
	}
}

func TestPostgresUpdateVerdictNotFound(t *testing.T) {
	st, pool := newPostgres(t)
	for _, id := range []string{"nao-existe", "00000000-0000-4000-8000-000000000000"} {
		if _, err := st.UpdateVerdict(context.Background(), id, model.VereditoReview, "a"); !errors.Is(err, ErrNotFound) {
			t.Errorf("UpdateVerdict(%q): esperado ErrNotFound, veio %v", id, err)
		}
	}
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM incident_audit`).Scan(&n)
	if n != 0 {
		t.Fatalf("auditoria gravada para incidente inexistente: %d linhas", n)
	}
}

// O banco é a última barreira: um verdict fora do enum não entra nem se a
// validação do handler for contornada.
func TestPostgresRejectsInvalidVerdict(t *testing.T) {
	st, pool := newPostgres(t)
	ctx := context.Background()
	it, _, err := st.Create(ctx, incident("00000000-0000-4000-8000-000000000022"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateVerdict(ctx, it.ID, "pwned", "a"); err == nil {
		t.Fatal("verdict inválido aceito pelo banco")
	}
	bad := incident("00000000-0000-4000-8000-000000000023")
	bad.Verdict = "pwned"
	if _, _, err := st.Create(ctx, bad); err == nil {
		t.Fatal("create com verdict inválido aceito pelo banco")
	}
	if rows := audit(t, pool, it.ID); len(rows) != 0 {
		t.Fatalf("transação não foi desfeita: %d linhas de auditoria", len(rows))
	}
}
