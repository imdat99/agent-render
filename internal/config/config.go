package config

import (
	"log/slog"
	"os"
	"strconv"
)

type Config struct {
	// Server
	Port     string
	HTTPPort string

	// Database
	DatabaseURL string

	// Redis
	RedisURL string

	// Agent
	AgentSecret string

	// Logging
	LogLevel  slog.Level
	LogFormat string // "json" or "text"

	// Tracing
	OTLPEndpoint string
	ServiceName  string

	// Features
	EnableMetrics bool
	EnableTracing bool
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:          getEnv("PORT", "9000"),
		HTTPPort:      getEnv("HTTP_PORT", "8080"),
		DatabaseURL:   getEnv("DB_URL", "postgres://user:password@localhost:5432/picpic?sslmode=disable"),
		RedisURL:      getEnv("REDIS_URL", "redis://localhost:6379/0"),
		AgentSecret:   getEnv("AGENT_SECRET", "secret"),
		LogFormat:     getEnv("LOG_FORMAT", "text"),
		OTLPEndpoint:  getEnv("OTLP_ENDPOINT", ""),
		ServiceName:   getEnv("SERVICE_NAME", "picpic-render"),
		EnableMetrics: getEnvBool("ENABLE_METRICS", true),
		EnableTracing: getEnvBool("ENABLE_TRACING", false),
	}

	// Parse log level
	logLevelStr := getEnv("LOG_LEVEL", "info")
	switch logLevelStr {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "info":
		cfg.LogLevel = slog.LevelInfo
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		cfg.LogLevel = slog.LevelInfo
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return defaultValue
		}
		return parsed
	}
	return defaultValue
}
