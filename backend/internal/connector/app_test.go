package connector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRuntimeStateAllowsDoctorWithoutAdapter(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "connector.json")
	state := &State{
		SchemaVersion:  stateSchemaVersion,
		ServerURL:      "http://localhost:18765",
		ConnectorID:    "connector-1",
		ConnectorLabel: "My Machine",
		ConnectorToken: "secret-token",
	}
	if err := state.Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	loadedState, client, resolvedPath, changed, err := loadRuntimeState([]string{"--state-path", statePath}, false)
	if err != nil {
		t.Fatalf("load runtime state for doctor: %v", err)
	}
	if loadedState == nil || client == nil {
		t.Fatal("expected state and client")
	}
	if resolvedPath != statePath {
		t.Fatalf("expected state path %q, got %q", statePath, resolvedPath)
	}
	if changed {
		t.Fatal("expected no adapter normalization changes without adapter configuration")
	}
	if loadedState.Adapter.Command != "" {
		t.Fatalf("expected empty adapter command, got %q", loadedState.Adapter.Command)
	}
}

// TestLoadRuntimeStateRequiresAdapterForServe verifies that calling
// loadRuntimeState with requireAdapter=true still fails when no adapter is
// configured. This tests the raw loadRuntimeState function; runServe itself now
// passes requireAdapter=false to allow zero-flag startup via the built-in adapter.
func TestLoadRuntimeStateRequiresAdapterForServe(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "connector.json")
	state := &State{
		SchemaVersion:  stateSchemaVersion,
		ServerURL:      "http://localhost:18765",
		ConnectorID:    "connector-1",
		ConnectorLabel: "My Machine",
		ConnectorToken: "secret-token",
	}
	if err := state.Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	_, _, _, _, err := loadRuntimeState([]string{"--state-path", statePath}, true)
	if err == nil {
		t.Fatal("expected requireAdapter=true to require adapter configuration")
	}
	if err.Error() != "adapter command is required" {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(statePath); statErr != nil {
		t.Fatalf("expected state file to remain present: %v", statErr)
	}
}

// TestLoadRuntimeStateServeNoAdapterRequired verifies that loadRuntimeState with
// requireAdapter=false (the mode used by runServe after the built-in adapter
// change) succeeds even when no adapter command is configured.
func TestLoadRuntimeStateServeNoAdapterRequired(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "connector.json")
	state := &State{
		SchemaVersion:  stateSchemaVersion,
		ServerURL:      "http://localhost:18765",
		ConnectorID:    "connector-1",
		ConnectorLabel: "My Machine",
		ConnectorToken: "secret-token",
	}
	if err := state.Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	// This is what runServe now calls — requireAdapter=false.
	loadedState, client, _, _, err := loadRuntimeState([]string{"--state-path", statePath}, false)
	if err != nil {
		t.Fatalf("runServe path should not require adapter: %v", err)
	}
	if loadedState == nil || client == nil {
		t.Fatal("expected state and client")
	}
	if loadedState.Adapter.Command != "" {
		t.Fatalf("expected empty adapter command, got %q", loadedState.Adapter.Command)
	}
}