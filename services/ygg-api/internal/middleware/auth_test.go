package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var secret = []byte("test-secret-with-at-least-32-bytes!!")

// echo responde 200 com o sub que o middleware colocou no contexto.
var echo = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(Subject(r.Context())))
})

func sign(t *testing.T, method jwt.SigningMethod, key any, claims jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(method, path, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	Auth(secret, echo).ServeHTTP(rec, req)
	return rec
}

func TestHealthzIsPublic(t *testing.T) {
	if rec := do(http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz sem token esperado 200, veio %d", rec.Code)
	}
}

func TestOnlyGetHealthzIsPublic(t *testing.T) {
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/healthz"},
		{http.MethodGet, "/healthz/"},
		{http.MethodGet, "/api/v1/incidents"},
		{http.MethodPost, "/api/v1/incidents"},
		{http.MethodPatch, "/api/v1/incidents/x/verdict"},
		{http.MethodPost, "/api/v1/triage"},
	} {
		rec := do(c.method, c.path, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s sem token: esperado 401, veio %d", c.method, c.path, rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s %s: 401 sem WWW-Authenticate", c.method, c.path)
		}
	}
}

func TestValidTokenPassesSubject(t *testing.T) {
	tok, err := NewToken(secret, "muninn", time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, scheme := range []string{"Bearer ", "bearer "} {
		rec := do(http.MethodGet, "/api/v1/incidents", scheme+tok)
		if rec.Code != http.StatusOK || rec.Body.String() != "muninn" {
			t.Fatalf("%q: esperado 200 com sub=muninn, veio %d %q", scheme, rec.Code, rec.Body.String())
		}
	}
}

func TestRejectsInvalidTokens(t *testing.T) {
	now := time.Now()
	valid := jwt.MapClaims{"sub": "muninn", "exp": now.Add(time.Hour).Unix()}
	cases := map[string]string{
		"sem esquema":   sign(t, jwt.SigningMethodHS256, secret, valid),
		"esquema basic": "Basic " + sign(t, jwt.SigningMethodHS256, secret, valid),
		"bearer vazio":  "Bearer ",
		"lixo":          "Bearer not.a.jwt",
		"segredo errado": "Bearer " + sign(t, jwt.SigningMethodHS256,
			[]byte("another-secret-with-32-bytes-or-more"), valid),
		"alg none": "Bearer " + sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, valid),
		// Mesmo segredo, outro algoritmo: o método é fixado em HS256.
		"hs512":    "Bearer " + sign(t, jwt.SigningMethodHS512, secret, valid),
		"expirado": "Bearer " + sign(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{"sub": "muninn", "exp": now.Add(-time.Hour).Unix()}),
		"sem exp":  "Bearer " + sign(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{"sub": "muninn"}),
		"sem sub":  "Bearer " + sign(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{"exp": now.Add(time.Hour).Unix()}),
		"nbf futuro": "Bearer " + sign(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{
			"sub": "muninn", "exp": now.Add(2 * time.Hour).Unix(), "nbf": now.Add(time.Hour).Unix()}),
	}
	for name, auth := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := do(http.MethodGet, "/api/v1/incidents", auth); rec.Code != http.StatusUnauthorized {
				t.Fatalf("esperado 401, veio %d", rec.Code)
			}
		})
	}
}

func TestToleratesSmallClockSkew(t *testing.T) {
	tok := sign(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{
		"sub": "muninn", "exp": time.Now().Add(-10 * time.Second).Unix()})
	if rec := do(http.MethodGet, "/api/v1/incidents", "Bearer "+tok); rec.Code != http.StatusOK {
		t.Fatalf("expirado há 10s (< %s de folga) esperado 200, veio %d", clockSkew, rec.Code)
	}
}

func TestNewTokenRequiresSubject(t *testing.T) {
	if _, err := NewToken(secret, "", time.Hour, time.Now()); err == nil {
		t.Fatal("esperado erro sem subject")
	}
}
