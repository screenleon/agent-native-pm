package config

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type WorkspaceConfig struct {
	ProjectName string `json:"project_name,omitempty"`
	Port        int    `json:"port,omitempty"`
}

type Workspace struct {
	RepoRoot    string
	AnpmDir     string
	DataDB      string
	ProjectName string
	Port        int
}

// FindWorkspace walks up from cwd to find the git root, creates .anpm/,
// patches .gitignore, and returns workspace metadata.
func FindWorkspace() (*Workspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}

	root, err := findGitRoot(cwd)
	if err != nil {
		return nil, err
	}

	anpmDir := filepath.Join(root, ".anpm")
	if err := os.MkdirAll(anpmDir, 0o755); err != nil {
		return nil, fmt.Errorf("create .anpm dir: %w", err)
	}

	patchGitignore(root)

	wcfg := loadWorkspaceConfig(anpmDir)

	projectName := wcfg.ProjectName
	if projectName == "" {
		projectName = filepath.Base(root)
	}

	port := wcfg.Port
	if port == 0 {
		port = derivePort(root)
	}

	return &Workspace{
		RepoRoot:    root,
		AnpmDir:     anpmDir,
		DataDB:      filepath.Join(anpmDir, "data.db"),
		ProjectName: projectName,
		Port:        port,
	}, nil
}

func findGitRoot(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .git directory found from %s", dir)
		}
		dir = parent
	}
}

// derivePort maps an absolute repo path to a stable port in [3100, 3999].
// The port is deterministic — the same repo always gets the same port.
// If that port is occupied at startup, FindAvailablePort should be used.
func derivePort(repoPath string) int {
	h := sha256.Sum256([]byte(repoPath))
	n := binary.BigEndian.Uint64(h[:8])
	return 3100 + int(n%900)
}

// FindAvailablePort returns the first free port starting at base, scanning
// forward within [3100, 3999]. It writes the chosen port to .anpm/port so
// that "anpm status" can discover which port the server actually uses.
func FindAvailablePort(base int, anpmDir string) int {
	const (
		rangeMin  = 3100
		rangeSize = 900
	)
	offset := base - rangeMin
	for i := 0; i < rangeSize; i++ {
		port := rangeMin + (offset+i)%rangeSize
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		ln.Close()
		// Persist so clients can discover the chosen port without probing.
		_ = os.WriteFile(filepath.Join(anpmDir, "port"), []byte(fmt.Sprintf("%d", port)), 0o644)
		return port
	}
	return base // all occupied — server bind will emit a clear error
}

// ReadPersistedPort returns the port written by FindAvailablePort, or 0 if
// the file does not exist (server has not started yet).
func ReadPersistedPort(anpmDir string) int {
	data, err := os.ReadFile(filepath.Join(anpmDir, "port"))
	if err != nil {
		return 0
	}
	var port int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &port); err != nil {
		return 0
	}
	return port
}

func loadWorkspaceConfig(anpmDir string) WorkspaceConfig {
	var cfg WorkspaceConfig
	data, err := os.ReadFile(filepath.Join(anpmDir, "config.json"))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

func patchGitignore(root string) {
	gitignorePath := filepath.Join(root, ".gitignore")
	data, _ := os.ReadFile(gitignorePath)
	if strings.Contains(string(data), ".anpm") {
		return
	}
	entry := []byte("\n# anpm local data\n.anpm/\n")
	_ = os.WriteFile(gitignorePath, append(data, entry...), 0o644)
}
