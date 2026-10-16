package model

import "time"

// Veredito é o resultado da triagem de um alerta.
type Veredito string

const (
	VereditoTruePositive  Veredito = "true_positive"
	VereditoFalsePositive Veredito = "false_positive"
	VereditoReview        Veredito = "review"
)

// Valid informa se o veredito é um dos valores conhecidos.
func (v Veredito) Valid() bool {
	switch v {
	case VereditoTruePositive, VereditoFalsePositive, VereditoReview:
		return true
	}
	return false
}

// StatusOpen é o status de todo incidente recém-criado.
const StatusOpen = "open"

// IOC é um indicador associado ao incidente (ADR-005: kind ip|domain|hash|url).
type IOC struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Incident é o recurso central exposto pela API (persistido em Postgres no futuro).
type Incident struct {
	ID                string    `json:"id"`
	EventID           string    `json:"event_id"` // NormalizedEvent que originou (ADR-005)
	Title             string    `json:"title"`
	Severity          string    `json:"severity"`
	Source            string    `json:"source"` // ex: cef, syslog, edr
	Category          string    `json:"category"`
	Verdict           Veredito  `json:"verdict"`
	Confidence        float64   `json:"confidence"`
	Rationale         string    `json:"rationale"`
	RecommendedAction string    `json:"recommended_action"`
	Engine            string    `json:"engine"` // llm | heuristic
	Model             string    `json:"model"`
	IOCs              []IOC     `json:"iocs"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}
