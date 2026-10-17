package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// TriageResult é o payload que o muninn envia em POST /api/v1/incidents após
// triar um NormalizedEvent. Fixture do contrato:
// proto/triage/fixtures/triage_result.json. Campos desconhecidos são ignorados.
type TriageResult struct {
	EventID           string   `json:"event_id"`
	Title             string   `json:"title"`
	Source            string   `json:"source"`
	Severity          string   `json:"severity"`
	IOCs              []IOC    `json:"iocs"`
	Verdict           Veredito `json:"verdict"`
	Confidence        float64  `json:"confidence"`
	Category          string   `json:"category"`
	Rationale         string   `json:"rationale"`
	RecommendedAction string   `json:"recommended_action"`
	Engine            string   `json:"engine"`
	Model             string   `json:"model"`
	// NormalizedEvent triado (opcional; ausente vira {}).
	RawEvent json.RawMessage `json:"raw_event,omitempty"`
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Severidades do ADR-005 (severity_hint).
var severities = map[string]bool{
	"unknown": true, "info": true, "low": true, "medium": true, "high": true, "critical": true,
}

// Validate confere o payload. Erros viram 400 para o muninn.
func (t TriageResult) Validate() error {
	var errs []error
	if !uuidRe.MatchString(t.EventID) {
		errs = append(errs, errors.New("event_id deve ser UUID"))
	}
	if t.Title == "" {
		errs = append(errs, errors.New("title obrigatório"))
	}
	if t.Source == "" {
		errs = append(errs, errors.New("source obrigatório"))
	}
	if !severities[t.Severity] {
		errs = append(errs, fmt.Errorf("severity inválida: %q", t.Severity))
	}
	if !t.Verdict.Decided() {
		errs = append(errs, fmt.Errorf("verdict inválido: %q", t.Verdict))
	}
	if t.Confidence < 0 || t.Confidence > 1 {
		errs = append(errs, errors.New("confidence fora de [0,1]"))
	}
	if t.Category == "" {
		errs = append(errs, errors.New("category obrigatória"))
	}
	if t.Engine != "llm" && t.Engine != "heuristic" {
		errs = append(errs, fmt.Errorf("engine inválido: %q", t.Engine))
	}
	if t.Model == "" {
		errs = append(errs, errors.New("model obrigatório"))
	}
	for i, ioc := range t.IOCs {
		if ioc.Kind == "" || ioc.Value == "" {
			errs = append(errs, fmt.Errorf("iocs[%d]: kind e value obrigatórios", i))
		}
	}
	if raw := bytes.TrimSpace(t.RawEvent); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) && raw[0] != '{' {
		errs = append(errs, errors.New("raw_event deve ser objeto JSON"))
	}
	return errors.Join(errs...)
}

// Incident converte o resultado num incidente novo. ID é gerado pela store.
func (t TriageResult) Incident(now time.Time) Incident {
	iocs := t.IOCs
	if iocs == nil {
		iocs = []IOC{}
	}
	raw := json.RawMessage(bytes.TrimSpace(t.RawEvent))
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		raw = json.RawMessage(`{}`)
	}
	return Incident{
		EventID:           t.EventID,
		Title:             t.Title,
		Severity:          t.Severity,
		Source:            t.Source,
		Category:          t.Category,
		Verdict:           t.Verdict,
		Confidence:        t.Confidence,
		Rationale:         t.Rationale,
		RecommendedAction: t.RecommendedAction,
		Engine:            t.Engine,
		Model:             t.Model,
		IOCs:              iocs,
		Status:            StatusOpen,
		RawEvent:          raw,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}
