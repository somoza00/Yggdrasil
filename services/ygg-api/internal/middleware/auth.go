// Package middleware tem os middlewares HTTP do ygg-api.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// clockSkew tolera relógios levemente fora de sincronia entre serviços.
const clockSkew = 30 * time.Second

type subjectKey struct{}

// Subject devolve o "sub" do token validado pelo Auth ("" se não houver).
func Subject(ctx context.Context) string {
	sub, _ := ctx.Value(subjectKey{}).(string)
	return sub
}

// Auth exige um JWT HS256 válido (Authorization: Bearer) em todas as rotas,
// exceto GET /healthz (probes do K8s). O token precisa de "sub" e "exp".
// O "sub" segue no contexto da requisição (ver Subject).
func Auth(secret []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		sub, err := verify(secret, r.Header.Get("Authorization"))
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="ygg-api"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "não autenticado"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), subjectKey{}, sub)))
	})
}

func verify(secret []byte, header string) (string, error) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", errors.New("Authorization Bearer ausente")
	}
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, claims,
		func(*jwt.Token) (any, error) { return secret, nil },
		// Fixar o algoritmo impede alg=none e confusão de algoritmo.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(clockSkew),
	)
	if err != nil {
		return "", err
	}
	if claims.Subject == "" {
		return "", errors.New("token sem sub")
	}
	return claims.Subject, nil
}

// NewToken assina um JWT HS256 com sub, iat e exp. Usado pelo cmd/ygg-token
// para emitir o token de serviço do muninn, e nos testes.
func NewToken(secret []byte, subject string, ttl time.Duration, now time.Time) (string, error) {
	if subject == "" {
		return "", errors.New("subject obrigatório")
	}
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}
