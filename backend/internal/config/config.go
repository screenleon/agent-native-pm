package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                     string
	DatabaseURL              string
	FrontendDir              string
	StaleDaysThreshold       int
	RepoRoot                 string
	AppSettingsMasterKey     string
	PlanningRequestTimeout   time.Duration
	PlanningMaxResponseBytes int64
	ClaudeModels             []string
	CodexModels              []string
	// CORSAllowedOrigins is an explicit allowlist consumed by the HTTP
	// router. Defaults are safe local development origins; production
	// deployments MUST set CORS_ALLOWED_ORIGINS to the canonical UI host.
	// Setting it to "*" disables credentialed CORS (cookies/Authorization)
	// because browsers reject the wildcard + credentials combination.
	CORSAllowedOrigins []string

	// LocalMode is true when the server auto-detected a git repo and is
	// operating in single-project, no-auth mode backed by SQLite.
	LocalMode        bool
	LocalRepoRoot    string
	LocalProjectName string
	// AnpmDir is the .anpm/ directory for the current workspace (local mode only).
	AnpmDir string
}

func Load() *Config {
	cfg := &Config{
		Port:                     getEnv("PORT", "18765"),
		DatabaseURL:              getEnv("DATABASE_URL", ""),
		FrontendDir:              getEnv("FRONTEND_DIR", "./frontend/dist"),
		StaleDaysThreshold:       getEnvInt("STALE_DAYS_THRESHOLD", 30),
		RepoRoot:                 getEnv("REPO_ROOT", "/app/data/repos"),
		AppSettingsMasterKey:     getEnv("APP_SETTINGS_MASTER_KEY", ""),
		PlanningRequestTimeout:   getEnvDurationSeconds("PLANNING_REQUEST_TIMEOUT_SECONDS", 45),
		PlanningMaxResponseBytes: int64(getEnvInt("PLANNING_MAX_RESPONSE_BYTES", 131072)),
		ClaudeModels: getEnvStringSlice("ANPM_CLAUDE_MODELS", []string{
			"claude-opus-4-7",
			"claude-sonnet-4-6",
			"claude-haiku-4-5-20251001",
		}),
		CodexModels: getEnvStringSlice("ANPM_CODEX_MODELS", []string{
			"codex-5.4",
			"codex-5.3",
			"codex-mini",
			"o4-mini",
			"o3",
		}),
		CORSAllowedOrigins: getEnvStringSlice("CORS_ALLOWED_ORIGINS", []string{
			"http://localhost:5173",
			"http://localhost:18765",
			"http://127.0.0.1:5173",
			"http://127.0.0.1:18765",
		}),
	}

	// If DATABASE_URL is not explicitly set, try workspace auto-detection.
	if cfg.DatabaseURL == "" {
		if ws, err := FindWorkspace(); err == nil {
			cfg.LocalMode = true
			cfg.DatabaseURL = "sqlite://" + ws.DataDB
			cfg.LocalRepoRoot = ws.RepoRoot
			cfg.LocalProjectName = ws.ProjectName
			cfg.AnpmDir = ws.AnpmDir
			if os.Getenv("PORT") == "" {
				cfg.Port = strconv.Itoa(ws.Port)
			}
		} else {
			// No git repo found; fall back to PostgreSQL default.
			cfg.DatabaseURL = "postgres://anpm:anpm@localhost:5432/anpm?sslmode=disable"
		}
	}

	return cfg
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

func getEnvDurationSeconds(key string, fallback int) time.Duration {
	seconds := getEnvInt(key, fallback)
	if seconds < 1 {
		seconds = fallback
	}
	return time.Duration(seconds) * time.Second
}

func getEnvStringSlice(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var out []string
	for _, s := range strings.Split(v, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
