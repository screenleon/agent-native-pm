package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	stateSchemaVersion         = 1
	defaultStateDirName        = "agent-native-pm"
	defaultStateFileName       = "connector.json"
	defaultAdapterTimeoutSec   = 360 // 6 min — enough for large What's Next scans
	defaultAdapterMaxOutput    = 64 * 1024
	defaultRequestedCandidates = 3
)

type State struct {
	SchemaVersion  int                   `json:"schema_version"`
	ServerURL      string                `json:"server_url"`
	ConnectorID    string                `json:"connector_id"`
	ConnectorLabel string                `json:"connector_label"`
	ConnectorToken string                `json:"connector_token"`
	Adapter        ExecJSONAdapterConfig `json:"adapter"`
}

type ExecJSONAdapterConfig struct {
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	WorkingDir     string   `json:"working_dir"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	MaxOutputBytes int64    `json:"max_output_bytes"`
}

type AdapterOverrides struct {
	Command        string
	Args           []string
	WorkingDir     string
	TimeoutSeconds int
	MaxOutputBytes int64
}

func (c ExecJSONAdapterConfig) HasConfiguration() bool {
	return strings.TrimSpace(c.Command) != "" || strings.TrimSpace(c.WorkingDir) != "" || c.TimeoutSeconds > 0 || c.MaxOutputBytes > 0 || len(c.Args) > 0
}

func (o AdapterOverrides) HasValues() bool {
	return strings.TrimSpace(o.Command) != "" || strings.TrimSpace(o.WorkingDir) != "" || o.TimeoutSeconds > 0 || o.MaxOutputBytes > 0 || len(o.Args) > 0
}

func ResolveStatePath(override string) (string, error) {
	trimmed := strings.TrimSpace(override)
	if trimmed != "" {
		return filepath.Abs(trimmed)
	}
	if envPath := strings.TrimSpace(os.Getenv("ANPM_CONNECTOR_STATE_PATH")); envPath != "" {
		return filepath.Abs(envPath)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, defaultStateDirName, defaultStateFileName), nil
}

func LoadState(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("connector state not found at %s", path)
		}
		return nil, fmt.Errorf("read connector state: %w", err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("decode connector state: %w", err)
	}
	if state.SchemaVersion != stateSchemaVersion {
		return nil, fmt.Errorf("unsupported connector state schema version %d", state.SchemaVersion)
	}
	if err := state.Validate(); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *State) Save(path string) error {
	if s == nil {
		return fmt.Errorf("connector state is required")
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = stateSchemaVersion
	}
	if err := s.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create connector state dir: %w", err)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode connector state: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write connector state: %w", err)
	}
	return nil
}

func (s *State) Validate() error {
	if s == nil {
		return fmt.Errorf("connector state is required")
	}
	if strings.TrimSpace(s.ServerURL) == "" {
		return fmt.Errorf("connector state server_url is required")
	}
	if strings.TrimSpace(s.ConnectorID) == "" {
		return fmt.Errorf("connector state connector_id is required")
	}
	if strings.TrimSpace(s.ConnectorToken) == "" {
		return fmt.Errorf("connector state connector_token is required")
	}
	return nil
}

func (c ExecJSONAdapterConfig) Validate() error {
	if strings.TrimSpace(c.Command) == "" {
		return fmt.Errorf("adapter command is required")
	}
	if !filepath.IsAbs(c.Command) {
		return fmt.Errorf("adapter command must resolve to an absolute executable path")
	}
	if strings.TrimSpace(c.WorkingDir) == "" {
		return fmt.Errorf("adapter working_dir is required")
	}
	if !filepath.IsAbs(c.WorkingDir) {
		return fmt.Errorf("adapter working_dir must be absolute")
	}
	info, err := os.Stat(c.WorkingDir)
	if err != nil {
		return fmt.Errorf("stat adapter working_dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("adapter working_dir must be a directory")
	}
	if c.TimeoutSeconds <= 0 {
		return fmt.Errorf("adapter timeout_seconds must be > 0")
	}
	if c.MaxOutputBytes <= 0 {
		return fmt.Errorf("adapter max_output_bytes must be > 0")
	}
	return nil
}

func (c ExecJSONAdapterConfig) Normalized() (ExecJSONAdapterConfig, error) {
	command := strings.TrimSpace(c.Command)
	if command == "" {
		return ExecJSONAdapterConfig{}, fmt.Errorf("adapter command is required")
	}
	resolvedCommand := command
	if !filepath.IsAbs(command) {
		lookup, err := exec.LookPath(command)
		if err != nil {
			return ExecJSONAdapterConfig{}, fmt.Errorf("resolve adapter command: %w", err)
		}
		resolvedCommand = lookup
	}
	workingDir := strings.TrimSpace(c.WorkingDir)
	if workingDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return ExecJSONAdapterConfig{}, fmt.Errorf("resolve adapter working directory: %w", err)
		}
		workingDir = cwd
	}
	if !filepath.IsAbs(workingDir) {
		absDir, err := filepath.Abs(workingDir)
		if err != nil {
			return ExecJSONAdapterConfig{}, fmt.Errorf("resolve adapter working directory: %w", err)
		}
		workingDir = absDir
	}
	timeoutSeconds := c.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultAdapterTimeoutSec
	}
	maxOutputBytes := c.MaxOutputBytes
	if maxOutputBytes <= 0 {
		maxOutputBytes = defaultAdapterMaxOutput
	}
	config := ExecJSONAdapterConfig{
		Command:        resolvedCommand,
		Args:           append([]string(nil), c.Args...),
		WorkingDir:     filepath.Clean(workingDir),
		TimeoutSeconds: timeoutSeconds,
		MaxOutputBytes: maxOutputBytes,
	}
	if err := config.Validate(); err != nil {
		return ExecJSONAdapterConfig{}, err
	}
	return config, nil
}

func applyAdapterOverrides(current ExecJSONAdapterConfig, overrides AdapterOverrides) (ExecJSONAdapterConfig, bool, error) {
	updated := current
	changed := false
	if strings.TrimSpace(overrides.Command) != "" {
		updated.Command = strings.TrimSpace(overrides.Command)
		changed = true
	}
	if overrides.Args != nil {
		updated.Args = append([]string(nil), overrides.Args...)
		changed = true
	}
	if strings.TrimSpace(overrides.WorkingDir) != "" {
		updated.WorkingDir = strings.TrimSpace(overrides.WorkingDir)
		changed = true
	}
	if overrides.TimeoutSeconds > 0 {
		updated.TimeoutSeconds = overrides.TimeoutSeconds
		changed = true
	}
	if overrides.MaxOutputBytes > 0 {
		updated.MaxOutputBytes = overrides.MaxOutputBytes
		changed = true
	}
	normalized, err := updated.Normalized()
	if err != nil {
		return ExecJSONAdapterConfig{}, changed, err
	}
	return normalized, changed, nil
}