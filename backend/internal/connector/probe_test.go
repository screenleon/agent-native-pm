package connector

import "testing"

// evaluateProbeOutput is the OK-criterion unit hoisted out of ExecuteProbe.
// Copilot #1 flagged that "any non-empty stdout" was too lenient — these
// cases codify the intended semantics: we need the acknowledgement token
// ("ok") to appear, and we report anything else as a failure with the
// actual content so the operator can see what the CLI said.

func TestEvaluateProbeOutput_EmptyFails(t *testing.T) {
	ok, kind, msg := evaluateProbeOutput("")
	if ok {
		t.Fatal("empty output must be reported as failure")
	}
	if kind == "" || msg == "" {
		t.Fatalf("expected non-empty kind+msg, got kind=%q msg=%q", kind, msg)
	}
}

func TestEvaluateProbeOutput_BareOKPasses(t *testing.T) {
	ok, _, _ := evaluateProbeOutput("ok")
	if !ok {
		t.Fatal("bare 'ok' must be reported as success")
	}
}

func TestEvaluateProbeOutput_CaseInsensitive(t *testing.T) {
	ok, _, _ := evaluateProbeOutput("OK")
	if !ok {
		t.Fatal("uppercase 'OK' must still count as success")
	}
}

func TestEvaluateProbeOutput_PreamblePasses(t *testing.T) {
	ok, _, _ := evaluateProbeOutput("Sure — ok.")
	if !ok {
		t.Fatal("substring match with preamble must count as success")
	}
}

func TestEvaluateProbeOutput_UnrelatedResponseFails(t *testing.T) {
	ok, kind, msg := evaluateProbeOutput("Error: session expired")
	if ok {
		t.Fatal("response without 'ok' must be reported as failure")
	}
	if kind == "" || msg == "" {
		t.Fatalf("expected non-empty kind+msg, got kind=%q msg=%q", kind, msg)
	}
}

func TestEvaluateProbeOutput_OkAsSubstringInUnrelatedWordStillAccepted(t *testing.T) {
	// Known leniency: "book" contains "ok". The substring match is explicitly
	// intended to be lenient; this test pins the current behaviour so future
	// tightening is a conscious change rather than an accidental regression.
	ok, _, _ := evaluateProbeOutput("I'll book that for you")
	if !ok {
		t.Fatal("substring match currently accepts 'ok' inside another word; tightening is an intentional change, not an accidental regression")
	}
}
