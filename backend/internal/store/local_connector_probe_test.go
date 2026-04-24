package store

import (
	"errors"
	"testing"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// Helper: pair + heartbeat a connector and return its id + token, which is
// the minimum setup needed to exercise the probe lifecycle.
func pairTestConnector(t *testing.T, connectorStore *LocalConnectorStore, userID, label string) (connectorID, token string) {
	t.Helper()
	pairing, err := connectorStore.CreatePairingSession(userID, models.CreateLocalConnectorPairingSessionRequest{Label: label})
	if err != nil {
		t.Fatalf("create pairing session: %v", err)
	}
	claim, err := connectorStore.ClaimPairingSession(models.PairLocalConnectorRequest{
		PairingCode:   pairing.PairingCode,
		Label:         label,
		Platform:      "linux",
		ClientVersion: "test",
	})
	if err != nil {
		t.Fatalf("claim pairing session: %v", err)
	}
	return claim.Connector.ID, claim.ConnectorToken
}

// T-P4-4-11: a second enqueue for the same binding while one is in-flight
// must return the existing probe_id (dedup invariant).
func TestEnqueueCliProbeDedupsPerBinding(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userStore := NewUserStore(db)
	user, err := userStore.Create(models.CreateUserRequest{Username: "probe-dedup", Email: "probe-dedup@example.com", Password: "password123", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	connectorStore := NewLocalConnectorStore(db, testutil.TestDialect())
	connectorID, _ := pairTestConnector(t, connectorStore, user.ID, "Laptop")

	first, err := connectorStore.EnqueueCliProbe(connectorID, user.ID, models.PendingCliProbeRequest{
		BindingID:  "binding-A",
		ProviderID: "cli:claude",
		ModelID:    "claude-sonnet-4-5",
		CliCommand: "claude",
	})
	if err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if first == "" {
		t.Fatal("expected non-empty probe_id")
	}
	second, err := connectorStore.EnqueueCliProbe(connectorID, user.ID, models.PendingCliProbeRequest{
		BindingID:  "binding-A",
		ProviderID: "cli:claude",
		ModelID:    "claude-sonnet-4-5",
		CliCommand: "claude",
	})
	if err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	if second != first {
		t.Fatalf("expected dedup to return same probe_id, got first=%s second=%s", first, second)
	}
}

// S7: once the previous probe has a completed result, a new enqueue must
// mint a fresh probe_id AND evict the cached result (so the next poll does
// not surface stale content).
func TestEnqueueCliProbeAllocatesFreshIdAfterCompletion(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userStore := NewUserStore(db)
	user, err := userStore.Create(models.CreateUserRequest{Username: "probe-fresh", Email: "probe-fresh@example.com", Password: "password123", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	connectorStore := NewLocalConnectorStore(db, testutil.TestDialect())
	connectorID, token := pairTestConnector(t, connectorStore, user.ID, "Laptop")

	probe1, err := connectorStore.EnqueueCliProbe(connectorID, user.ID, models.PendingCliProbeRequest{
		BindingID: "binding-A", ProviderID: "cli:claude", ModelID: "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the connector reporting a completed result via heartbeat.
	if _, err := connectorStore.HeartbeatByToken(token, models.LocalConnectorHeartbeatRequest{
		CliProbeResults: []models.CliProbeResult{{ProbeID: probe1, BindingID: "binding-A", OK: true, LatencyMS: 42, Content: "ok"}},
	}); err != nil {
		t.Fatal(err)
	}

	probe2, err := connectorStore.EnqueueCliProbe(connectorID, user.ID, models.PendingCliProbeRequest{
		BindingID: "binding-A", ProviderID: "cli:claude", ModelID: "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if probe2 == probe1 {
		t.Fatalf("expected fresh probe_id after previous completion, still got %s", probe1)
	}
	// Cached result for probe1 must be evicted; polling for it returns not_found.
	status, _, err := connectorStore.GetCliProbeResult(connectorID, user.ID, probe1)
	if err != nil {
		t.Fatal(err)
	}
	if status != "not_found" {
		t.Fatalf("expected stale probe1 result evicted, got status=%s", status)
	}
}

// S1: heartbeat results that do NOT match a pending probe must be silently
// dropped (spoofing guard). A connector compromised or buggy could otherwise
// inject arbitrary keys into metadata.cli_probe_results.
func TestHeartbeatRejectsUnsolicitedProbeResults(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userStore := NewUserStore(db)
	user, err := userStore.Create(models.CreateUserRequest{Username: "probe-s1", Email: "probe-s1@example.com", Password: "password123", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	connectorStore := NewLocalConnectorStore(db, testutil.TestDialect())
	connectorID, token := pairTestConnector(t, connectorStore, user.ID, "Laptop")

	// No pending probe exists, but the connector claims a completed result.
	if _, err := connectorStore.HeartbeatByToken(token, models.LocalConnectorHeartbeatRequest{
		CliProbeResults: []models.CliProbeResult{{ProbeID: "fabricated", BindingID: "whatever", OK: true, Content: "gotcha"}},
	}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	status, result, err := connectorStore.GetCliProbeResult(connectorID, user.ID, "fabricated")
	if err != nil {
		t.Fatalf("get result: %v", err)
	}
	if status != "not_found" || result != nil {
		t.Fatalf("expected fabricated probe_id to be ignored, got status=%s result=%v", status, result)
	}
}

// T-P4-4-10: deleting a binding removes pending + completed probe entries
// for that binding across all the user's connectors.
func TestScrubCliProbesForBindingRemovesEntries(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userStore := NewUserStore(db)
	user, err := userStore.Create(models.CreateUserRequest{Username: "probe-scrub", Email: "probe-scrub@example.com", Password: "password123", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	connectorStore := NewLocalConnectorStore(db, testutil.TestDialect())
	connectorID, token := pairTestConnector(t, connectorStore, user.ID, "Laptop")

	probeID, err := connectorStore.EnqueueCliProbe(connectorID, user.ID, models.PendingCliProbeRequest{
		BindingID: "binding-delete-me", ProviderID: "cli:claude", ModelID: "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connectorStore.HeartbeatByToken(token, models.LocalConnectorHeartbeatRequest{
		CliProbeResults: []models.CliProbeResult{{ProbeID: probeID, BindingID: "binding-delete-me", OK: true, Content: "ok"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Confirm the result exists before scrubbing.
	if status, _, _ := connectorStore.GetCliProbeResult(connectorID, user.ID, probeID); status != "completed" {
		t.Fatalf("precondition: expected completed, got %s", status)
	}
	if err := connectorStore.ScrubCliProbesForBinding(user.ID, "binding-delete-me"); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := connectorStore.GetCliProbeResult(connectorID, user.ID, probeID); status != "not_found" {
		t.Fatalf("expected scrub to evict completed entry, got %s", status)
	}
}

// Cross-user isolation: user A's scrub does not touch user B's connectors.
func TestScrubCliProbesForBindingIsUserScoped(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userStore := NewUserStore(db)
	userA, err := userStore.Create(models.CreateUserRequest{Username: "probe-isolateA", Email: "probe-isolateA@example.com", Password: "password123", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	userB, err := userStore.Create(models.CreateUserRequest{Username: "probe-isolateB", Email: "probe-isolateB@example.com", Password: "password123", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	connectorStore := NewLocalConnectorStore(db, testutil.TestDialect())
	connectorB, _ := pairTestConnector(t, connectorStore, userB.ID, "B-Laptop")
	probeB, err := connectorStore.EnqueueCliProbe(connectorB, userB.ID, models.PendingCliProbeRequest{
		BindingID: "shared-binding-id", ProviderID: "cli:claude", ModelID: "m",
	})
	if err != nil {
		t.Fatal(err)
	}

	// User A scrubs a binding with the same id string — must NOT affect user B's connector.
	if err := connectorStore.ScrubCliProbesForBinding(userA.ID, "shared-binding-id"); err != nil {
		t.Fatal(err)
	}
	status, _, _ := connectorStore.GetCliProbeResult(connectorB, userB.ID, probeB)
	if status != "pending" {
		t.Fatalf("expected user B's probe untouched after user A's scrub, got status=%s", status)
	}
}

// gcOldProbeResults drops entries whose completed_at is older than 24h, so
// repeated heartbeats do not let metadata grow unboundedly.
func TestHeartbeatGCsOldProbeResults(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userStore := NewUserStore(db)
	user, err := userStore.Create(models.CreateUserRequest{Username: "probe-gc", Email: "probe-gc@example.com", Password: "password123", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	connectorStore := NewLocalConnectorStore(db, testutil.TestDialect())
	connectorID, token := pairTestConnector(t, connectorStore, user.ID, "Laptop")

	old, err := connectorStore.EnqueueCliProbe(connectorID, user.ID, models.PendingCliProbeRequest{BindingID: "b", ProviderID: "cli:claude", ModelID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	// Land the result with an old completed_at timestamp.
	if _, err := connectorStore.HeartbeatByToken(token, models.LocalConnectorHeartbeatRequest{
		CliProbeResults: []models.CliProbeResult{{ProbeID: old, BindingID: "b", OK: true, CompletedAt: time.Now().Add(-30 * time.Hour)}},
	}); err != nil {
		t.Fatal(err)
	}
	// Any subsequent heartbeat runs gcOldProbeResults and drops the stale entry.
	if _, err := connectorStore.HeartbeatByToken(token, models.LocalConnectorHeartbeatRequest{}); err != nil {
		t.Fatal(err)
	}
	status, _, _ := connectorStore.GetCliProbeResult(connectorID, user.ID, old)
	if status != "not_found" {
		t.Fatalf("expected 30h-old probe result to be GC'd, got status=%s", status)
	}
}

// S-3: EnqueueCliProbe must return ErrPendingProbeCapReached once the
// pending list hits maxPendingProbesPerConnector. This defends against an
// unbounded pending list if dedup ever regresses or if a user has an
// unreasonable number of bindings all in-flight at once.
func TestEnqueueCliProbeEnforcesPendingCap(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userStore := NewUserStore(db)
	user, err := userStore.Create(models.CreateUserRequest{Username: "probe-cap", Email: "probe-cap@example.com", Password: "password123", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	connectorStore := NewLocalConnectorStore(db, testutil.TestDialect())
	connectorID, _ := pairTestConnector(t, connectorStore, user.ID, "Laptop")

	// Fill the pending list to the cap. Each binding is unique so dedup does
	// not fold them together.
	for i := 0; i < maxPendingProbesPerConnector; i++ {
		bid := "binding-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		if _, err := connectorStore.EnqueueCliProbe(connectorID, user.ID, models.PendingCliProbeRequest{
			BindingID: bid, ProviderID: "cli:claude", ModelID: "m",
		}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	// One more must be rejected with the sentinel error.
	_, err = connectorStore.EnqueueCliProbe(connectorID, user.ID, models.PendingCliProbeRequest{
		BindingID: "one-too-many", ProviderID: "cli:claude", ModelID: "m",
	})
	if !errors.Is(err, ErrPendingProbeCapReached) {
		t.Fatalf("expected ErrPendingProbeCapReached, got %v", err)
	}
}

// EnqueueCliProbe must refuse connector_id / user_id mismatch (cross-tenant).
func TestEnqueueCliProbeRejectsCrossUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userStore := NewUserStore(db)
	userA, err := userStore.Create(models.CreateUserRequest{Username: "probe-authA", Email: "probe-authA@example.com", Password: "password123", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	userB, err := userStore.Create(models.CreateUserRequest{Username: "probe-authB", Email: "probe-authB@example.com", Password: "password123", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	connectorStore := NewLocalConnectorStore(db, testutil.TestDialect())
	connectorA, _ := pairTestConnector(t, connectorStore, userA.ID, "A-Laptop")

	// User B tries to enqueue against user A's connector.
	_, err = connectorStore.EnqueueCliProbe(connectorA, userB.ID, models.PendingCliProbeRequest{
		BindingID: "x", ProviderID: "cli:claude", ModelID: "m",
	})
	if err == nil {
		t.Fatal("expected cross-user enqueue to fail")
	}
}
