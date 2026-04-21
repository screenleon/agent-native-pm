package connector

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/planning/wire"
)

func TestExecJSONInputRoundTripsPlanningContext(t *testing.T) {
	now := time.Now().UTC()
	input := ExecJSONInput{
		Run:                    &models.PlanningRun{ID: "r1"},
		Requirement:            &models.Requirement{ID: "req1", Title: "demo"},
		RequestedMaxCandidates: 5,
		PlanningContext: &wire.PlanningContextV1{
			SchemaVersion:    wire.ContextSchemaV1,
			GeneratedAt:      now,
			GeneratedBy:      wire.GeneratedByServer,
			SanitizerVersion: wire.SanitizerVersion,
			Limits:           wire.DefaultLimits(),
			Sources: wire.PlanningContextSources{
				OpenTasks: []wire.WireTask{
					{ID: "t1", Title: "fix bug", Status: "open", Priority: "high", UpdatedAt: now},
				},
			},
			Meta: wire.PlanningContextMeta{
				Ranking:       wire.DefaultRanking(),
				DroppedCounts: map[string]int{"open_tasks": 0},
				SourcesBytes:  123,
			},
		},
	}

	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ExecJSONInput
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PlanningContext == nil {
		t.Fatalf("planning context lost during round-trip")
	}
	if decoded.PlanningContext.SchemaVersion != wire.ContextSchemaV1 {
		t.Fatalf("schema_version mismatch: %q", decoded.PlanningContext.SchemaVersion)
	}
	if len(decoded.PlanningContext.Sources.OpenTasks) != 1 || decoded.PlanningContext.Sources.OpenTasks[0].Priority != "high" {
		t.Fatalf("sources mangled on round-trip: %+v", decoded.PlanningContext.Sources)
	}
}

func TestExecJSONInputOmitsPlanningContextWhenNil(t *testing.T) {
	input := ExecJSONInput{
		Run:         &models.PlanningRun{ID: "r1"},
		Requirement: &models.Requirement{ID: "req1"},
	}
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if containsKey(payload, "planning_context") {
		t.Fatalf("expected planning_context key to be omitted; got %s", payload)
	}
}

func TestExecJSONInputDecodesPayloadWithoutPlanningContext(t *testing.T) {
	body := []byte(`{"run": {"id": "r1"}, "requirement": {"id": "req1"}, "requested_max_candidates": 3}`)
	var input ExecJSONInput
	if err := json.Unmarshal(body, &input); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if input.PlanningContext != nil {
		t.Fatalf("expected nil planning context; got %+v", input.PlanningContext)
	}
}

func containsKey(payload []byte, key string) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(payload, &probe); err != nil {
		return false
	}
	_, ok := probe[key]
	return ok
}
