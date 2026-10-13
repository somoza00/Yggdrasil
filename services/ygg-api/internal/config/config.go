// Package config carrega a configuração do serviço a partir de variáveis de
// ambiente, com defaults seguros para dev. Não há comportamento em docs.
package config

import (
	"os"
)

// Config reúne os parâmetros do ygg-api.
type Config struct {
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
		Port:         getenv("YGG_API_PORT", "8080"),
		DBURL:        getenv("DATABASE_URL", "postgres://ygg:changeme@127.0.0.1:5432/yggdrasil"),
		RedisURL:     getenv("REDIS_URL", "redis://127.0.0.1:6379"),
		JWTSecret:    getenv("JWT_SECRET", "dev-jwt-secret"),
		MunninURL:    getenv("MUNNIN_URL", "http://127.0.0.1:8090"),
		LangfuseHost: getenv("LANGFUSE_HOST", "http://127.0.0.1:4000"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
