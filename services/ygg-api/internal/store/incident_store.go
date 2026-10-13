package store

import (
	"context"
	"errors"

	"crypto/rand"
	"encoding/hex"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/model"
)

// ErrNotFound é retornado quando um incidente não existe.
var ErrNotFound = errors.New("incidente nao encontrado")

// Store define o contrato de persistência. A implementação atual é em memória
// (dev); Postgres será a impl real deste contrato (ver ADR-003).
type Store interface {
	Create(ctx context.Context, in *model.Incident) (*model.Incident, error)
	Get(ctx context.Context, id string) (*model.Incident, error)
	List(ctx context.Context) ([]*model.Incident, error)
	UpdateVerdict(ctx context.Context, id string, verdict model.Veredito) (*model.Incident, error)
}

// Memory é uma Store em memória para dev/testes. NÃO thread-safe (simples).
type Memory struct {
	items map[string]*model.Incident
}

// NewMemory retorna uma Store em memória vazia.
func NewMemory() *Memory {
	return &Memory{items: make(map[string]*model.Incident)}
}

func (m *Memory) Create(_ context.Context, in *model.Incident) (*model.Incident, error) {
	if in.ID == "" {
		in.ID = newID()
	}
	m.items[in.ID] = in
	return in, nil
}

func (m *Memory) Get(_ context.Context, id string) (*model.Incident, error) {
	it, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	return it, nil
}

func (m *Memory) List(_ context.Context) ([]*model.Incident, error) {
	out := make([]*model.Incident, 0, len(m.items))
	for _, it := range m.items {
		out = append(out, it)
	}
	return out, nil
}

func (m *Memory) UpdateVerdict(_ context.Context, id string, verdict model.Veredito) (*model.Incident, error) {
	it, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	it.Verdict = verdict
	return it, nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
