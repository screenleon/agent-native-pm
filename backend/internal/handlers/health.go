package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"runtime"
	"syscall"
	"time"

	"github.com/screenleon/agent-native-pm/internal/database"
)

type HealthHandler struct {
	db        *sql.DB
	startTime time.Time
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db, startTime: time.Now()}
}

type healthResponse struct {
	Status     string `json:"status"`
	Uptime     string `json:"uptime"`
	DB         string `json:"db"`
	Migrations int    `json:"migrations_applied"`
	DiskFreeGB string `json:"disk_free_gb,omitempty"`
	GoRoutines int    `json:"goroutines"`
}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:     "ok",
		Uptime:     time.Since(h.startTime).Round(time.Second).String(),
		GoRoutines: runtime.NumGoroutine(),
	}

	// DB ping
	if err := h.db.PingContext(r.Context()); err != nil {
		resp.Status = "degraded"
		resp.DB = "ping failed: " + err.Error()
		writeJSON(w, http.StatusServiceUnavailable, resp)
		return
	}
	resp.DB = "ok"

	// Migration count
	if n, err := database.AppliedMigrationCount(h.db); err == nil {
		resp.Migrations = n
	}

	// Disk free (best-effort, platform-specific)
	if gb, ok := diskFreeGB("."); ok {
		resp.DiskFreeGB = gb
	}

	writeJSON(w, http.StatusOK, resp)
}

// diskFreeGB returns the free disk space in GB for the filesystem containing
// the given path. Returns ("", false) if the call is unavailable.
func diskFreeGB(path string) (string, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return "", false
	}
	free := float64(stat.Bavail) * float64(stat.Bsize) / 1e9
	return formatGB(free), true
}

func formatGB(gb float64) string {
	if gb >= 10 {
		return fmt.Sprintf("%.0f", gb)
	}
	return fmt.Sprintf("%.1f", gb)
}

// HealthCheck is the legacy single-function handler kept for backward
// compatibility with existing Docker health-check probes.
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

