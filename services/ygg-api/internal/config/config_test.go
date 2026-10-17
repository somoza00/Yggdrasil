package config

import (
	"strings"
	"testing"
)

const strongSecret = "0123456789abcdef0123456789abcdef"

func TestLoadDefaultsToProduction(t *testing.T) {
	t.Setenv("ENV", "")
	t.Setenv("JWT_SECRET", "")
	cfg := Load()
	if cfg.Env != "production" || cfg.JWTSecret != DevJWTSecret {
		t.Fatalf("defaults inesperados: env=%q secret=%q", cfg.Env, cfg.JWTSecret)
	}
	// Esquecer ENV e JWT_SECRET não pode subir com o segredo de dev.
	if err := cfg.Validate(); err == nil {
		t.Fatal("sem ENV, o segredo de dev deveria ser recusado")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		env, secret string
		ok          bool
	}{
		{"development", DevJWTSecret, true},
		{"development", "short", true},
		{"production", DevJWTSecret, false},
		{"staging", DevJWTSecret, false},
		{"", DevJWTSecret, false},
		{"Development", DevJWTSecret, false},
		{"production", "too-short-secret", false},
		{"production", strongSecret, true},
	}
	for _, c := range cases {
		err := Config{Env: c.env, JWTSecret: c.secret}.Validate()
		if (err == nil) != c.ok {
			t.Errorf("env=%q secret=%q: ok esperado %v, erro %v", c.env, c.secret, c.ok, err)
		}
		if err != nil && strings.Contains(err.Error(), c.secret) && c.secret != DevJWTSecret {
			t.Errorf("mensagem de erro vaza o segredo: %v", err)
		}
	}
}
