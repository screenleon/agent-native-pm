package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port               string
	DatabaseURL        string
	FrontendDir        string
	StaleDaysThreshold int
	RepoRoot           string
}

func Load() *Config {
	return &Config{
		Port:               getEnv("PORT", "18765"),
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://anpm:anpm@localhost:5432/anpm?sslmode=disable"),
		FrontendDir:        getEnv("FRONTEND_DIR", "./frontend/dist"),
		StaleDaysThreshold: getEnvInt("STALE_DAYS_THRESHOLD", 30),
		RepoRoot:           getEnv("REPO_ROOT", "/app/data/repos"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
