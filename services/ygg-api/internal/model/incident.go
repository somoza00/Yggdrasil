package model

import "time"

// Veredito é o resultado da triagem de um alerta.
type Veredito string

const (
	VereditoTruePositive  Veredito = "true_positive"
	VereditoFalsePositive Veredito = "false_positive"
	VereditoReview        Veredito = "review"
)

// Incident é o recurso central exposto pela API (persistido em Postgres no futuro).
type Incident struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Severity    string    `json:"severity"`
	Source      string    `json:"source"`        // ex: edr, siem, webhook
	RawAlertRef string    `json:"raw_alert_ref"` // id do alerta na fonte
	Verdict     Veredito  `json:"verdict"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}
