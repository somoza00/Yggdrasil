package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
)

// Postgres é a Store sobre as tabelas incidents e incident_audit
// (migrations/). Segura para uso concorrente (pgxpool).
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres usa um pool já conectado e com as migrations aplicadas.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

// Connect abre o pool e espera o banco responder, com backoff até o fim de
// ctx. No compose e no K8s o Postgres pode subir depois do ygg-api.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("postgres: config inválida: %w", err)
	}
	delay := 250 * time.Millisecond
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}
		log.Printf("postgres indisponível, nova tentativa em %s: %v", delay, err)
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, fmt.Errorf("postgres: %w", err)
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Second)
	}
}

// Ids que não são UUID não existem: responder ErrNotFound evita mandar ao
// banco um valor que ele rejeitaria com erro de sintaxe (viraria 500).
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const incidentCols = `id::text, event_id::text, title, severity, source, category, verdict,
	confidence, rationale, recommended_action, engine, model, iocs, status, raw_event,
	created_at, updated_at, verdict_changed_by, verdict_changed_at`

func scanIncident(row pgx.Row) (*model.Incident, error) {
	var (
		it        model.Incident
		iocs, raw []byte
	)
	err := row.Scan(&it.ID, &it.EventID, &it.Title, &it.Severity, &it.Source, &it.Category,
		&it.Verdict, &it.Confidence, &it.Rationale, &it.RecommendedAction, &it.Engine, &it.Model,
		&iocs, &it.Status, &raw, &it.CreatedAt, &it.UpdatedAt, &it.VerdictChangedBy,
		&it.VerdictChangedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(iocs, &it.IOCs); err != nil {
		return nil, fmt.Errorf("iocs inválidos no banco: %w", err)
	}
	it.RawEvent = raw
	it.CreatedAt, it.UpdatedAt = it.CreatedAt.UTC(), it.UpdatedAt.UTC()
	if it.VerdictChangedAt != nil {
		t := it.VerdictChangedAt.UTC()
		it.VerdictChangedAt = &t
	}
	return &it, nil
}

// Create insere o incidente; id, created_at e updated_at vêm do banco. Se o
// event_id já existe, devolve o incidente existente com created=false.
func (p *Postgres) Create(ctx context.Context, in model.Incident) (*model.Incident, bool, error) {
	iocs := in.IOCs
	if iocs == nil {
		iocs = []model.IOC{}
	}
	iocsJSON, err := json.Marshal(iocs)
	if err != nil {
		return nil, false, err
	}
	raw := in.RawEvent
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	row := p.pool.QueryRow(ctx, `
		INSERT INTO incidents (event_id, title, severity, source, category, verdict, confidence,
			rationale, recommended_action, engine, model, iocs, status, raw_event)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (event_id) DO NOTHING
		RETURNING `+incidentCols,
		in.EventID, in.Title, in.Severity, in.Source, in.Category, in.Verdict, in.Confidence,
		in.Rationale, in.RecommendedAction, in.Engine, in.Model, string(iocsJSON), in.Status,
		string(raw))
	it, err := scanIncident(row)
	if err == nil {
		return it, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	// Conflito: o event_id já virou incidente (reentrega at-least-once).
	it, err = scanIncident(p.pool.QueryRow(ctx,
		`SELECT `+incidentCols+` FROM incidents WHERE event_id = $1`, in.EventID))
	if err != nil {
		return nil, false, err
	}
	return it, false, nil
}

func (p *Postgres) Get(ctx context.Context, id string) (*model.Incident, error) {
	if !uuidRe.MatchString(id) {
		return nil, ErrNotFound
	}
	it, err := scanIncident(p.pool.QueryRow(ctx,
		`SELECT `+incidentCols+` FROM incidents WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return it, err
}

// List devolve os incidentes do mais recente para o mais antigo.
func (p *Postgres) List(ctx context.Context) ([]*model.Incident, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+incidentCols+` FROM incidents ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Incident{}
	for rows.Next() {
		it, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// UpdateVerdict troca o verdict e grava a linha de auditoria na mesma
// transação. O FOR UPDATE serializa PATCHes concorrentes no mesmo incidente,
// para que old_verdict de cada linha seja o verdict que ela substituiu.
func (p *Postgres) UpdateVerdict(ctx context.Context, id string, verdict model.Veredito, changedBy string) (*model.Incident, error) {
	if !uuidRe.MatchString(id) {
		return nil, ErrNotFound
	}
	var it *model.Incident
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var old string
		err := tx.QueryRow(ctx, `SELECT verdict FROM incidents WHERE id = $1 FOR UPDATE`, id).Scan(&old)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		it, err = scanIncident(tx.QueryRow(ctx, `
			UPDATE incidents
			SET verdict = $2, updated_at = now(), verdict_changed_by = $3, verdict_changed_at = now()
			WHERE id = $1
			RETURNING `+incidentCols, id, verdict, changedBy))
		if err != nil {
			return err
		}
		// now() é o início da transação: changed_at == verdict_changed_at.
		_, err = tx.Exec(ctx, `
			INSERT INTO incident_audit (incident_id, old_verdict, new_verdict, changed_by, changed_at)
			VALUES ($1, $2, $3, $4, now())`, id, old, verdict, changedBy)
		return err
	})
	if err != nil {
		return nil, err
	}
	return it, nil
}
