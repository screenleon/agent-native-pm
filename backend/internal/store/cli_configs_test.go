package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// seedCliConfigConnector returns a fresh user + paired connector ready
// to host cli_configs. Extracted because every test in this file needs
// the same pre-state.
func seedCliConfigConnector(t *testing.T) (*LocalConnectorStore, string, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	userStore := NewUserStore(db)
	user, err := userStore.Create(models.CreateUserRequest{
		Username: "cli-cfg-u-" + t.Name(), Email: "cfg-" + strings.ToLower(t.Name()) + "@example.com",
		Password: "password123", Role: "member",
	})
	if err != nil {
		t.Fatal(err)
	}
	cs := NewLocalConnectorStore(db, testutil.TestDialect())
	pairing, err := cs.CreatePairingSession(user.ID, models.CreateLocalConnectorPairingSessionRequest{Label: "Laptop"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := cs.ClaimPairingSession(models.PairLocalConnectorRequest{
		PairingCode: pairing.PairingCode, Label: "Laptop", Platform: "linux", ClientVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return cs, claim.Connector.ID, user.ID
}

// T-6a-B1-1: POST creates with valid body; first config auto-primary.
func TestAddCliConfig_FirstConfigAutoPrimary(t *testing.T) {
	cs, connID, userID := seedCliConfigConnector(t)
	got, err := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "claude-sonnet-4-6", CliCommand: "/usr/bin/claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsPrimary {
		t.Fatal("first config should auto-become primary")
	}
	if got.Label != "My Claude" {
		t.Fatalf("default label = %q, want 'My Claude'", got.Label)
	}
}

// T-6a-B1-4: second config does NOT auto-primary.
func TestAddCliConfig_SecondConfigNotPrimaryByDefault(t *testing.T) {
	cs, connID, userID := seedCliConfigConnector(t)
	if _, err := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "claude-sonnet-4-6",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:codex", ModelID: "codex-mini",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IsPrimary {
		t.Fatal("second config should NOT auto-become primary")
	}
}

// T-6a-B1-2: duplicate (provider_id, label) on same connector → 409.
func TestAddCliConfig_DuplicateLabelRejected(t *testing.T) {
	cs, connID, userID := seedCliConfigConnector(t)
	if _, err := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "m1", Label: "Duplicate",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "m2", Label: "Duplicate",
	})
	if !errors.Is(err, ErrCliConfigDuplicateLabel) {
		t.Fatalf("want ErrCliConfigDuplicateLabel, got %v", err)
	}
}

// T-6a-B1-5: PATCH with is_primary=true demotes others atomically.
func TestUpdateCliConfig_SetPrimaryDemotesOthers(t *testing.T) {
	cs, connID, userID := seedCliConfigConnector(t)
	first, _ := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "m1",
	})
	second, _ := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:codex", ModelID: "m2",
	})
	trueP := true
	if _, err := cs.UpdateCliConfig(connID, userID, second.ID, models.UpdateCliConfigRequest{
		IsPrimary: &trueP,
	}); err != nil {
		t.Fatal(err)
	}
	configs, _ := cs.ListCliConfigs(connID, userID)
	for _, c := range configs {
		if c.ID == second.ID && !c.IsPrimary {
			t.Fatal("second should be primary")
		}
		if c.ID == first.ID && c.IsPrimary {
			t.Fatal("first should have been demoted")
		}
	}
}

// T-6a-B1-6: cross-user PATCH/DELETE/primary returns 404 (ErrCliConfigNotFound).
func TestCliConfig_CrossUserIsolation(t *testing.T) {
	cs, connID, userA := seedCliConfigConnector(t)
	added, err := cs.AddCliConfig(connID, userA, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "m",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a second user.
	userStore := NewUserStore(cs.db)
	other, err := userStore.Create(models.CreateUserRequest{
		Username: "cli-cfg-other", Email: "other@example.com", Password: "password123", Role: "member",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := cs.UpdateCliConfig(connID, other.ID, added.ID, models.UpdateCliConfigRequest{}); !errors.Is(err, ErrCliConfigNotFound) {
		t.Fatalf("cross-user Update should return NotFound, got %v", err)
	}
	if err := cs.DeleteCliConfig(connID, other.ID, added.ID); !errors.Is(err, ErrCliConfigNotFound) {
		t.Fatalf("cross-user Delete should return NotFound, got %v", err)
	}
	if err := cs.SetPrimaryCliConfig(connID, other.ID, added.ID); !errors.Is(err, ErrCliConfigNotFound) {
		t.Fatalf("cross-user SetPrimary should return NotFound, got %v", err)
	}
}

// T-6a-B1-7: DELETE removes from metadata; if deleted was primary, promotes first remaining.
func TestDeleteCliConfig_PromotesSuccessorWhenPrimaryDeleted(t *testing.T) {
	cs, connID, userID := seedCliConfigConnector(t)
	primary, _ := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "m1",
	})
	second, _ := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:codex", ModelID: "m2",
	})
	if err := cs.DeleteCliConfig(connID, userID, primary.ID); err != nil {
		t.Fatal(err)
	}
	configs, _ := cs.ListCliConfigs(connID, userID)
	if len(configs) != 1 {
		t.Fatalf("want 1 config left, got %d", len(configs))
	}
	if !configs[0].IsPrimary {
		t.Fatalf("successor should have been auto-promoted to primary; got %+v", configs[0])
	}
	if configs[0].ID != second.ID {
		t.Fatalf("unexpected surviving config: %+v", configs[0])
	}
}

// T-6a-B1-9: invalid cli_command rejected (regex sanity).
func TestAddCliConfig_RejectsInvalidCliCommand(t *testing.T) {
	cs, connID, userID := seedCliConfigConnector(t)
	_, err := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "m", CliCommand: "; rm -rf /",
	})
	if !errors.Is(err, ErrCliConfigInvalidCliCommand) {
		t.Fatalf("want ErrCliConfigInvalidCliCommand, got %v", err)
	}
}

// Invalid provider_id rejected.
func TestAddCliConfig_RejectsUnknownProvider(t *testing.T) {
	cs, connID, userID := seedCliConfigConnector(t)
	_, err := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:unknown", ModelID: "m",
	})
	if !errors.Is(err, ErrCliConfigInvalidProvider) {
		t.Fatalf("want ErrCliConfigInvalidProvider, got %v", err)
	}
}

// Missing model_id rejected.
func TestAddCliConfig_RejectsMissingModel(t *testing.T) {
	cs, connID, userID := seedCliConfigConnector(t)
	_, err := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "   ",
	})
	if !errors.Is(err, ErrCliConfigModelIDRequired) {
		t.Fatalf("want ErrCliConfigModelIDRequired, got %v", err)
	}
}

// Cap enforcement.
func TestAddCliConfig_EnforcesCap(t *testing.T) {
	cs, connID, userID := seedCliConfigConnector(t)
	for i := 0; i < models.MaxCliConfigsPerConnector; i++ {
		label := "config-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		if _, err := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
			ProviderID: "cli:claude", ModelID: "m", Label: label,
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	_, err := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "m", Label: "one-too-many",
	})
	if !errors.Is(err, ErrCliConfigCapReached) {
		t.Fatalf("want ErrCliConfigCapReached, got %v", err)
	}
}

// Get returns a single config, user-scoped.
func TestGetCliConfig_Found(t *testing.T) {
	cs, connID, userID := seedCliConfigConnector(t)
	created, _ := cs.AddCliConfig(connID, userID, models.CreateCliConfigRequest{
		ProviderID: "cli:claude", ModelID: "m",
	})
	got, err := cs.GetCliConfig(connID, userID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID {
		t.Fatalf("id mismatch: %v", got)
	}
}
