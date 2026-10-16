package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/muninn"
)

func TestTriageProxyForwardsToMuninn(t *testing.T) {
	var gotPath, gotType, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotType = r.URL.Path, r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":"bad"}`))
	}))
	defer upstream.Close()

	h := TriageProxy(muninn.New(upstream.URL))
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/api/v1/triage", bytes.NewBufferString(`{"a":1}`)))

	if gotPath != "/triage" || gotType != "application/json" || gotBody != `{"a":1}` {
		t.Fatalf("encaminhamento errado: path=%q type=%q body=%q", gotPath, gotType, gotBody)
	}
	if rec.Code != http.StatusUnprocessableEntity || rec.Body.String() != `{"detail":"bad"}` {
		t.Fatalf("resposta do muninn não repassada: %d %s", rec.Code, rec.Body.String())
	}
}

func TestTriageProxyMuninnDown(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	url := upstream.URL
	upstream.Close() // porta fechada => erro de conexão

	rec := httptest.NewRecorder()
	TriageProxy(muninn.New(url))(rec, httptest.NewRequest(http.MethodPost, "/api/v1/triage",
		bytes.NewBufferString(`{}`)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("esperado 502, veio %d", rec.Code)
	}
}

func TestTriageProxyRejectsLargeBody(t *testing.T) {
	rec := httptest.NewRecorder()
	TriageProxy(muninn.New("http://127.0.0.1:1"))(rec, httptest.NewRequest(http.MethodPost,
		"/api/v1/triage", bytes.NewReader(make([]byte, maxBodyBytes+1))))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("esperado 413, veio %d", rec.Code)
	}
}
