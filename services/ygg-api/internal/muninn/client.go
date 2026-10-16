// Package muninn é o cliente da única chamada síncrona ygg-api → muninn
// (ADR-002: REST, POST /triage).
package muninn

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Timeout da triagem: cobre a chamada ao LLM feita pelo muninn.
const requestTimeout = 60 * time.Second

// maxResponseBytes limita a resposta lida do muninn.
const maxResponseBytes = 1 << 20

// Client chama o muninn em MUNNIN_URL.
type Client struct {
	baseURL string
	http    *http.Client
}

// New cria o cliente. baseURL sem barra final (ex.: http://muninn:8090).
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// Triage envia o alerta (JSON) para POST /triage e devolve status e corpo da
// resposta do muninn sem interpretá-los.
func (c *Client) Triage(ctx context.Context, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/triage", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("muninn indisponível: %w", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("lendo resposta do muninn: %w", err)
	}
	return resp.StatusCode, out, nil
}
