package handler

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
)

// Triager é a chamada síncrona ao muninn (implementada por muninn.Client).
type Triager interface {
	Triage(ctx context.Context, body []byte) (int, []byte, error)
}

// TriageProxy responde POST /api/v1/triage repassando o alerta ao muninn
// (POST /triage) e devolvendo a resposta dele como veio.
func TriageProxy(m Triager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeErr(w, http.StatusRequestEntityTooLarge, "body grande demais")
				return
			}
			writeErr(w, http.StatusBadRequest, "falha ao ler body")
			return
		}
		status, out, err := m.Triage(r.Context(), body)
		if err != nil {
			log.Printf("triage: %v", err)
			writeErr(w, http.StatusBadGateway, "muninn indisponível")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(out)
	}
}
