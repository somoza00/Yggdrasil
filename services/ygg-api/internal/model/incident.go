package model

import (
	"encoding/json"
	"time"
)

// Veredito é o resultado da triagem de um alerta. "open" significa ainda
// sem decisão (só um analista define via PATCH; a triagem sempre decide).
type Veredito string

const (
	VereditoOpen          Veredito = "open"
	VereditoTruePositive  Veredito = "true_positive"
	VereditoFalsePositive Veredito = "false_positive"
	VereditoReview        Veredito = "review"
)

// Valid informa se o veredito é um dos valores conhecidos.
func (v Veredito) Valid() bool {
	switch v {
	case VereditoOpen, VereditoTruePositive, VereditoFalsePositive, VereditoReview:
		return true
	}
	return false
}

// Decided informa se o veredito é uma decisão de triagem (não "open").
func (v Veredito) Decided() bool {
	return v.Valid() && v != VereditoOpen
}

// StatusOpen é o status de todo incidente recém-criado.
const StatusOpen = "open"

// IOC é um indicador associado ao incidente (ADR-005: kind ip|domain|hash|url).
type IOC struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Incident é o recurso central exposto pela API (tabela incidents no Postgres).
type Incident struct {
	ID                string   `json:"id"`
	EventID           string   `json:"event_id"` // NormalizedEvent que originou (ADR-005)
	Title             string   `json:"title"`
	Severity          string   `json:"severity"`
	Source            string   `json:"source"` // ex: cef, syslog, edr
	Category          string   `json:"category"`
	Verdict           Veredito `json:"verdict"`
	Confidence        float64  `json:"confidence"`
	Rationale         string   `json:"rationale"`
	RecommendedAction string   `json:"recommended_action"`
	Engine            string   `json:"engine"` // llm | heuristic
	Model             string   `json:"model"`
	IOCs              []IOC    `json:"iocs"`
	Status            string   `json:"status"`
	// NormalizedEvent que o muninn triou, como recebido (objeto JSON).
	RawEvent         json.RawMessage `json:"raw_event"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	VerdictChangedBy *string         `json:"verdict_changed_by"` // sub do JWT
	VerdictChangedAt *time.Time      `json:"verdict_changed_at"`
}
