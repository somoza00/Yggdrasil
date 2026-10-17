// Package config carrega a configuração do serviço a partir de variáveis de
// ambiente, com defaults seguros para dev. Não há comportamento em docs.
package config

import (
	"errors"
	"fmt"
	"os"
)

// DevJWTSecret é o segredo padrão de dev. Só é aceito com ENV=development.
const DevJWTSecret = "dev-jwt-secret"

// minJWTSecretLen: HS256 com segredo curto é quebrável offline por força
// bruta a partir de qualquer token capturado.
const minJWTSecretLen = 32

// Config reúne os parâmetros do ygg-api.
type Config struct {
	Env          string
	Port         string
	DBURL        string
	RedisURL     string
	JWTSecret    string
	MunninURL    string
	LangfuseHost string
}

// Load lê o ambiente. Valores ausentes caem nos defaults de dev.
func Load() Config {
	return Config{
		// Sem ENV o serviço se comporta como produção (falha fechada).
		Env:          getenv("ENV", "production"),
		Port:         getenv("YGG_API_PORT", "8080"),
		DBURL:        getenv("DATABASE_URL", "postgres://ygg:changeme@127.0.0.1:5432/yggdrasil"),
		RedisURL:     getenv("REDIS_URL", "redis://127.0.0.1:6379"),
		JWTSecret:    getenv("JWT_SECRET", DevJWTSecret),
		MunninURL:    getenv("MUNNIN_URL", "http://127.0.0.1:8090"),
		LangfuseHost: getenv("LANGFUSE_HOST", "http://127.0.0.1:4000"),
	}
}

// Validate recusa configurações inseguras fora de ENV=development: o
// segredo JWT padrão de dev ou um segredo curto demais.
func (c Config) Validate() error {
	if c.Env == "development" {
		return nil
	}
	if c.JWTSecret == DevJWTSecret {
		return errors.New("JWT_SECRET=" + DevJWTSecret + " só é permitido com ENV=development")
	}
	if len(c.JWTSecret) < minJWTSecretLen {
		return fmt.Errorf("JWT_SECRET precisa de pelo menos %d bytes fora de ENV=development", minJWTSecretLen)
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
