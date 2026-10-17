package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
)

// ErrNotFound é retornado quando um incidente não existe.
var ErrNotFound = errors.New("incidente nao encontrado")

// Store define o contrato de persistência. Postgres é a implementação real
// (ADR-003); Memory serve aos testes de handler.
type Store interface {
	// Create grava um incidente novo com ID gerado. Se já existe incidente
	// para o mesmo EventID, devolve o existente e created=false (idempotente).
	Create(ctx context.Context, in model.Incident) (it *model.Incident, created bool, err error)
	Get(ctx context.Context, id string) (*model.Incident, error)
	List(ctx context.Context) ([]*model.Incident, error)
	// UpdateVerdict troca o verdict e registra quem mudou. A implementação
	// Postgres grava uma linha em incident_audit por chamada.
	UpdateVerdict(ctx context.Context, id string, verdict model.Veredito, changedBy string) (*model.Incident, error)
}

// Memory é uma Store em memória para dev/testes, segura para uso concorrente.
// Devolve cópias: quem chama não compartilha estado com a store.
type Memory struct {
	mu      sync.RWMutex
	items   map[string]model.Incident
	byEvent map[string]string // event_id -> id
}

// NewMemory retorna uma Store em memória vazia.
func NewMemory() *Memory {
	return &Memory{items: make(map[string]model.Incident), byEvent: make(map[string]string)}
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
func (m *Memory) List(_ context.Context) ([]*model.Incident, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*model.Incident, 0, len(m.items))
	for _, it := range m.items {
		out = append(out, clone(it))
	}
	slices.SortFunc(out, func(a, b *model.Incident) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}

func (m *Memory) UpdateVerdict(_ context.Context, id string, verdict model.Veredito, changedBy string) (*model.Incident, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	it.Verdict = verdict
	it.UpdatedAt = now
	it.VerdictChangedBy = &changedBy
	it.VerdictChangedAt = &now
	m.items[id] = it
	return clone(it), nil
}

// newID gera um UUIDv4, o mesmo formato do gen_random_uuid() do Postgres.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
