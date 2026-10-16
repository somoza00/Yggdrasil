package store

import (
	"context"
	"errors"
	"slices"
	"sync"

	"crypto/rand"
	"encoding/hex"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
)

// ErrNotFound é retornado quando um incidente não existe.
var ErrNotFound = errors.New("incidente nao encontrado")

// Store define o contrato de persistência. A implementação atual é em memória
// (dev); Postgres será a impl real deste contrato (ver ADR-003).
type Store interface {
	// Create grava um incidente novo com ID gerado. Se já existe incidente
	// para o mesmo EventID, devolve o existente e created=false (idempotente).
	Create(ctx context.Context, in model.Incident) (it *model.Incident, created bool, err error)
	Get(ctx context.Context, id string) (*model.Incident, error)
	List(ctx context.Context) ([]*model.Incident, error)
	UpdateVerdict(ctx context.Context, id string, verdict model.Veredito) (*model.Incident, error)
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

func (m *Memory) UpdateVerdict(_ context.Context, id string, verdict model.Veredito) (*model.Incident, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	it.Verdict = verdict
	m.items[id] = it
	return clone(it), nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
