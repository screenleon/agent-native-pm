package connector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "connector.json")
	state := &State{
		SchemaVersion:  stateSchemaVersion,
		ServerURL:      "http://localhost:18765",
		ConnectorID:    "connector-1",
		ConnectorLabel: "Laptop",
		ConnectorToken: "secret-token",
		Adapter: ExecJSONAdapterConfig{
			Command:        "/bin/echo",
			WorkingDir:     tempDir,
			TimeoutSeconds: 30,
			MaxOutputBytes: 2048,
		},
	}
	if err := state.Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded.ConnectorToken != state.ConnectorToken {
		t.Fatalf("expected token %q, got %q", state.ConnectorToken, loaded.ConnectorToken)
	}
	if loaded.Adapter.Command != state.Adapter.Command {
		t.Fatalf("expected command %q, got %q", state.Adapter.Command, loaded.Adapter.Command)
	}
}