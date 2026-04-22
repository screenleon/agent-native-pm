package store

import (
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

func TestLocalConnectorStorePairHeartbeatAndRevoke(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userStore := NewUserStore(db)
	user, err := userStore.Create(models.CreateUserRequest{
		Username: "connector-user",
		Email:    "connector@example.com",
		Password: "password123",
		Role:     "member",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	connectorStore := NewLocalConnectorStore(db, testutil.TestDialect())
	pairing, err := connectorStore.CreatePairingSession(user.ID, models.CreateLocalConnectorPairingSessionRequest{Label: "My Laptop"})
	if err != nil {
		t.Fatalf("create pairing session: %v", err)
	}
	if pairing.PairingCode == "" {
		t.Fatal("expected pairing code")
	}

	claim, err := connectorStore.ClaimPairingSession(models.PairLocalConnectorRequest{
		PairingCode:   pairing.PairingCode,
		Label:         "MacBook",
		Platform:      "macos",
		ClientVersion: "0.1.0",
		Capabilities: map[string]interface{}{
			"adapter": "exec-json",
		},
	})
	if err != nil {
		t.Fatalf("claim pairing session: %v", err)
	}
	if claim.ConnectorToken == "" {
		t.Fatal("expected connector token")
	}
	if claim.Connector.Status != models.LocalConnectorStatusPending {
		t.Fatalf("expected pending connector, got %s", claim.Connector.Status)
	}

	heartbeat, err := connectorStore.HeartbeatByToken(claim.ConnectorToken, models.LocalConnectorHeartbeatRequest{})
	if err != nil {
		t.Fatalf("heartbeat connector: %v", err)
	}
	if heartbeat.Status != models.LocalConnectorStatusOnline {
		t.Fatalf("expected online connector, got %s", heartbeat.Status)
	}
	if heartbeat.LastSeenAt == nil {
		t.Fatal("expected last_seen_at to be set")
	}

	connectors, err := connectorStore.ListByUser(user.ID)
	if err != nil {
		t.Fatalf("list connectors: %v", err)
	}
	if len(connectors) != 1 {
		t.Fatalf("expected 1 connector, got %d", len(connectors))
	}

	if err := connectorStore.Revoke(claim.Connector.ID, user.ID); err != nil {
		t.Fatalf("revoke connector: %v", err)
	}
	revoked, err := connectorStore.GetByID(claim.Connector.ID, user.ID)
	if err != nil {
		t.Fatalf("get revoked connector: %v", err)
	}
	if revoked == nil || revoked.Status != models.LocalConnectorStatusRevoked {
		t.Fatalf("expected revoked connector, got %+v", revoked)
	}
}