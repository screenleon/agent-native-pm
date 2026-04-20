package handlers

import (
	"strings"
	"testing"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
	"github.com/screenleon/agent-native-pm/internal/testutil"
)

// TestNotifyPlanningRunTerminalEmitsSuccessAndFailure exercises the
// notification-emission helper that fires when a local-connector planning
// run reaches a terminal state. It verifies:
//   - a success terminal emits a kind=info notification with candidate count
//   - a failure terminal emits a kind=error notification with the error body
//   - the notification is scoped to the run requester's user id
//   - both notifications show up in UnreadCount for that user
func TestNotifyPlanningRunTerminalEmitsSuccessAndFailure(t *testing.T) {
	db := testutil.OpenTestDB(t)

	users := store.NewUserStore(db)
	projects := store.NewProjectStore(db)
	notifications := store.NewNotificationStore(db)

	user, err := users.Create(models.CreateUserRequest{
		Username: "notify-test-user",
		Email:    "notify@example.com",
		Password: "supersecret",
		Role:     "member",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	project, err := projects.Create(models.CreateProjectRequest{
		Name:        "Notify Project",
		Description: "test project",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	h := &LocalConnectorHandler{}
	h = h.WithNotificationStore(notifications)

	connector := &models.LocalConnector{
		ID:     "connector-id",
		UserID: user.ID,
		Label:  "Test Box",
		Status: models.LocalConnectorStatusOnline,
	}
	run := &models.PlanningRun{
		ID:                "run-id",
		ProjectID:         project.ID,
		RequestedByUserID: user.ID,
	}
	requirement := &models.Requirement{ID: "req-id", Title: "Demo Requirement"}

	// Success path
	h.notifyPlanningRunTerminal(connector, run, requirement, true, 3, "")
	// Failure path
	h.notifyPlanningRunTerminal(connector, run, requirement, false, 0, "adapter exited with status 2")

	list, total, err := notifications.ListByUser(user.ID, false, 1, 50)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 notifications, got %d (list len=%d)", total, len(list))
	}

	var sawSuccess, sawFailure bool
	for _, n := range list {
		if n.UserID != user.ID {
			t.Fatalf("notification scoped to wrong user: got %s want %s", n.UserID, user.ID)
		}
		if n.ProjectID == nil || *n.ProjectID != project.ID {
			t.Fatalf("notification project id mismatch: got %v want %s", n.ProjectID, project.ID)
		}
		if !strings.Contains(n.Link, project.ID) {
			t.Fatalf("notification link missing project id: %q", n.Link)
		}
		switch n.Kind {
		case "info":
			sawSuccess = true
			if !strings.Contains(n.Title, "Demo Requirement") {
				t.Fatalf("success title missing requirement: %q", n.Title)
			}
			if !strings.Contains(n.Body, "3 backlog candidates") {
				t.Fatalf("success body missing candidate count: %q", n.Body)
			}
		case "error":
			sawFailure = true
			if !strings.Contains(n.Body, "adapter exited with status 2") {
				t.Fatalf("error body missing failure message: %q", n.Body)
			}
		default:
			t.Fatalf("unexpected notification kind %q", n.Kind)
		}
	}
	if !sawSuccess || !sawFailure {
		t.Fatalf("expected both success+failure notifications; success=%v failure=%v", sawSuccess, sawFailure)
	}

	unread, err := notifications.CountUnread(user.ID)
	if err != nil {
		t.Fatalf("unread count: %v", err)
	}
	if unread != 2 {
		t.Fatalf("expected unread=2, got %d", unread)
	}
}

// TestNotifyPlanningRunTerminalNoStoreIsSafe verifies the helper is a no-op
// when no notification store is wired (preserving the optional dependency).
func TestNotifyPlanningRunTerminalNoStoreIsSafe(t *testing.T) {
	h := &LocalConnectorHandler{}
	// Should not panic / error with nil store.
	h.notifyPlanningRunTerminal(
		&models.LocalConnector{ID: "c", UserID: "u", Label: "x"},
		&models.PlanningRun{ID: "r", ProjectID: "p", RequestedByUserID: "u"},
		&models.Requirement{ID: "req", Title: "t"},
		true, 1, "",
	)
}
