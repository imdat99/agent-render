package config

import (
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
	RedisURL    string
	AgentSecret string
}

func Load() *Config {
	return &Config{
		Port:        getEnv("PORT", "9000"),
		DatabaseURL: getEnv("DB_URL", "postgres://user:password@localhost:5432/picpic?sslmode=disable"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379/0"),
		AgentSecret: getEnv("WOODPECKER_AGENT_SECRET", "secret"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
