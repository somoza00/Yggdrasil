package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
)

// ErrNotFound é retornado quando um incidente não existe.
var ErrNotFound = errors.New("incidente nao encontrado")

// ListFilter restringe e pagina a listagem de incidentes. O zero value não
// filtra e não pagina: quem já chamava List sem filtro continua vendo tudo.
type ListFilter struct {
	Verdict  model.Veredito // "" = qualquer veredito
	Severity string         // "" = qualquer severidade (case-insensitive)
	Limit    int            // <=0 = sem limite
	Offset   int            // <0 é tratado como 0
}

// Match informa se o incidente passa nos filtros do ListFilter (ignora
// Limit/Offset, que são aplicados depois da ordenação).
func (f ListFilter) Match(it *model.Incident) bool {
	if f.Verdict != "" && it.Verdict != f.Verdict {
		return false
	}
	return f.Severity == "" || strings.EqualFold(it.Severity, f.Severity)
}

// Paginate aplica Offset/Limit sobre uma lista já ordenada, sem reordenar.
// Devolve uma lista nova (nunca um sub-slice do argumento) para quem chama não
// compartilhar o array com a store.
func (f ListFilter) Paginate(items []*model.Incident) []*model.Incident {
	out := make([]*model.Incident, 0, len(items))
	start := f.Offset
	if start < 0 {
		start = 0
	}
	for i := start; i < len(items); i++ {
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
		out = append(out, items[i])
	}
	return out
}

// Store define o contrato de persistência. Postgres é a implementação real
// (ADR-003); Memory serve aos testes de handler.
type Store interface {
	// Create grava um incidente novo com ID gerado. Se já existe incidente
	// para o mesmo EventID, devolve o existente e created=false (idempotente).
	Create(ctx context.Context, in model.Incident) (it *model.Incident, created bool, err error)
	Get(ctx context.Context, id string) (*model.Incident, error)
	// List devolve os incidentes que casam com o filtro, do mais novo para o
	// mais antigo (created_at DESC, id como desempate: sem o desempate a
	// paginação por offset pula/duplica itens empatados no mesmo instante).
	List(ctx context.Context, f ListFilter) ([]*model.Incident, error)
	// UpdateVerdict troca o verdict e registra quem mudou. A implementação
	// Postgres grava uma linha em incident_audit por chamada.
	UpdateVerdict(ctx context.Context, id string, verdict model.Veredito, changedBy string) (*model.Incident, error)
	// TriageStats agrega a concordância entre a decisão da IA e o veredito
	// final do analista (incident_audit).
	TriageStats(ctx context.Context) (*model.TriageStats, error)
}

// Memory é uma Store em memória para dev/testes, segura para uso concorrente.
// Devolve cópias: quem chama não compartilha estado com a store.
type Memory struct {
	mu      sync.RWMutex
	items   map[string]model.Incident
	byEvent map[string]string // event_id -> id
	// Histórico de veredito para TriageStats: o original (antes do primeiro
	// PATCH) e o último aplicado — o equivalente a incident_audit.
	original map[string]model.Veredito
	last     map[string]model.Veredito
}

// NewMemory retorna uma Store em memória vazia.
func NewMemory() *Memory {
	return &Memory{
		items:    make(map[string]model.Incident),
		byEvent:  make(map[string]string),
		original: make(map[string]model.Veredito),
		last:     make(map[string]model.Veredito),
	}
}

func clone(it model.Incident) *model.Incident {
	it.IOCs = slices.Clone(it.IOCs)
	it.RawEvent = slices.Clone(it.RawEvent)
	return &it
}

func (m *Memory) Create(_ context.Context, in model.Incident) (*model.Incident, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.byEvent[in.EventID]; ok && in.EventID != "" {
		return clone(m.items[id]), false, nil
	}
	in.ID = newID()
	m.items[in.ID] = *clone(in)
	if in.EventID != "" {
		m.byEvent[in.EventID] = in.ID
	}
	return clone(in), true, nil
}

func (m *Memory) Get(_ context.Context, id string) (*model.Incident, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	it, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(it), nil
}

// List devolve os incidentes do mais recente para o mais antigo.
func (m *Memory) List(_ context.Context, f ListFilter) ([]*model.Incident, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*model.Incident, 0, len(m.items))
	for _, it := range m.items {
		if f.Match(&it) {
			out = append(out, clone(it))
		}
	}
	// Mesma ordem do SQL (`ORDER BY created_at DESC, id`): o id desempata
	// incidentes criados no mesmo instante, senão a página por offset varia
	// entre chamadas.
	slices.SortFunc(out, func(a, b *model.Incident) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return f.Paginate(out), nil
}

func (m *Memory) UpdateVerdict(_ context.Context, id string, verdict model.Veredito, changedBy string) (*model.Incident, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	if _, seen := m.original[id]; !seen {
		m.original[id] = it.Verdict
	}
	m.last[id] = verdict
	now := time.Now().UTC()
	it.Verdict = verdict
	it.UpdatedAt = now
	it.VerdictChangedBy = &changedBy
	it.VerdictChangedAt = &now
	m.items[id] = it
	return clone(it), nil
}

type engineKey struct{ engine, model string }

// TriageStats agrega em memória o mesmo que a consulta SQL do Postgres faz,
// mantendo a paridade entre a implementação de teste e a real.
func (m *Memory) TriageStats(_ context.Context) (*model.TriageStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	byEngine := make(map[engineKey]*model.EngineAccuracy)
	byVerdict := make(map[string]*model.VerdictAccuracy)
	stats := &model.TriageStats{}
	for id, it := range m.items {
		ai := it.Verdict
		if original, seen := m.original[id]; seen {
			ai = original
		}
		stats.TotalIncidents++
		ek := engineKey{it.Engine, it.Model}
		if byEngine[ek] == nil {
			byEngine[ek] = &model.EngineAccuracy{Engine: it.Engine, Model: it.Model}
		}
		vk := string(ai)
		if byVerdict[vk] == nil {
			byVerdict[vk] = &model.VerdictAccuracy{AIVerdict: vk}
		}
		final, reviewed := m.last[id]
		if !reviewed {
			continue
		}
		stats.Reviewed++
		byEngine[ek].Reviewed++
		byVerdict[vk].Reviewed++
		if final == ai {
			stats.Agreed++
			byEngine[ek].Agreed++
			byVerdict[vk].Agreed++
		}
	}
	stats.Accuracy = model.Rate(stats.Agreed, stats.Reviewed)

	stats.ByEngine = make([]model.EngineAccuracy, 0, len(byEngine))
	for _, e := range byEngine {
		e.Accuracy = model.Rate(e.Agreed, e.Reviewed)
		stats.ByEngine = append(stats.ByEngine, *e)
	}
	slices.SortFunc(stats.ByEngine, func(a, b model.EngineAccuracy) int {
		if c := strings.Compare(a.Engine, b.Engine); c != 0 {
			return c
		}
		return strings.Compare(a.Model, b.Model)
	})

	stats.ByAIVerdict = make([]model.VerdictAccuracy, 0, len(byVerdict))
	for _, v := range byVerdict {
		v.Accuracy = model.Rate(v.Agreed, v.Reviewed)
		stats.ByAIVerdict = append(stats.ByAIVerdict, *v)
	}
	slices.SortFunc(stats.ByAIVerdict, func(a, b model.VerdictAccuracy) int {
		return strings.Compare(a.AIVerdict, b.AIVerdict)
	})
	return stats, nil
}

// newID gera um UUIDv4, o mesmo formato do gen_random_uuid() do Postgres.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
