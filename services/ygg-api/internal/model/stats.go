package model

import "math"

// TriageStats agrega a concordância entre a decisão da IA e o veredito final
// do analista (tabela incident_audit).
//
// "Veredito da IA" é o veredito original do incidente: o primeiro `old_verdict`
// registrado, ou o veredito atual quando o analista nunca mexeu nele (a coluna
// `verdict` passa a guardar o veredito do analista depois de um PATCH).
type TriageStats struct {
	// TotalIncidents é o número de incidentes considerados (todos).
	TotalIncidents int `json:"total_incidents"`
	// Reviewed é quantos tiveram o veredito revisado por um analista.
	Reviewed int `json:"reviewed"`
	// Agreed é quantos, entre os revisados, o analista manteve igual ao da IA.
	Agreed int `json:"agreed"`
	// Accuracy é Agreed/Reviewed; nil enquanto não houver revisão (sem amostra
	// não existe acurácia — devolver 0% mentiria).
	Accuracy    *float64          `json:"accuracy"`
	ByEngine    []EngineAccuracy  `json:"by_engine"`
	ByAIVerdict []VerdictAccuracy `json:"by_ai_verdict"`
}

// EngineAccuracy é a concordância de uma combinação engine+model.
type EngineAccuracy struct {
	Engine   string   `json:"engine"`
	Model    string   `json:"model"`
	Reviewed int      `json:"reviewed"`
	Agreed   int      `json:"agreed"`
	Accuracy *float64 `json:"accuracy"`
}

// VerdictAccuracy é a concordância por veredito proposto pela IA. Serve para
// ver, por exemplo, se os `review` da IA costumam ser confirmados ou
// substituídos pelo analista.
type VerdictAccuracy struct {
	AIVerdict string   `json:"ai_verdict"`
	Reviewed  int      `json:"reviewed"`
	Agreed    int      `json:"agreed"`
	Accuracy  *float64 `json:"accuracy"`
}

// Rate devolve agreed/reviewed com 4 casas decimais, ou nil sem revisão.
func Rate(agreed, reviewed int) *float64 {
	if reviewed <= 0 {
		return nil
	}
	r := math.Round(float64(agreed)/float64(reviewed)*10000) / 10000
	return &r
}
