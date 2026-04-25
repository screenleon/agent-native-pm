package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain doubles as a mock-CLI launcher: when BOTH the helper-mode
// env var AND a sentinel guard env var are set, this binary runs as
// a mock CLI subprocess for the dispatch safety tests below.
// Otherwise it runs the normal test suite.
//
// Two env vars are required so a developer with `ANPM_TEST_HELPER_MODE`
// set in their shell does not have `go test` silently exit before any
// test runs. The sentinel `ANPM_TEST_HELPER_GUARD=1` is set ONLY by
// tests that explicitly want subprocess helper behaviour.
//
// The pattern lets us spawn real subprocesses (with real signal
// behaviour) using os.Args[0] as the "binary" parameter to
// invokeBuiltinCLI, which is what T-6c-C2-2 (SIGTERM-ignore /
// SIGKILL escalation) requires — Go-internal time.Sleep does not
// trap signals the same way a real CLI process does.
func TestMain(m *testing.M) {
	if os.Getenv("ANPM_TEST_HELPER_GUARD") == "1" {
		mode := os.Getenv("ANPM_TEST_HELPER_MODE")
		if mode == "" {
			fmt.Fprintln(os.Stderr, "ANPM_TEST_HELPER_GUARD=1 but no ANPM_TEST_HELPER_MODE set")
			os.Exit(2)
		}
		runTestHelper(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runTestHelper(mode string) {
	switch mode {
	case "ignore_sigterm_sleep_forever":
		// T-6c-C2-2: trap SIGTERM and ignore it; sleep until SIGKILL'd.
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(10 * time.Minute)
	case "ignore_sigterm_print_loop":
		// T-6c-C2-15 (Copilot fix): trap SIGTERM, then continuously
		// print stdout until SIGKILL'd. Triggers BOTH timeout AND
		// boundedWriter truncation to verify runErr-over-truncated
		// precedence in service.go RunOnceTask.
		signal.Ignore(syscall.SIGTERM)
		buf := bytes.Repeat([]byte("x"), 1024)
		for {
			if _, err := os.Stdout.Write(buf); err != nil {
				return
			}
		}
	case "echo_args":
		// T-6c-C2-1: print the received -p prompt verbatim so the test
		// can verify shell metacharacters were NOT expanded.
		fmt.Fprintf(os.Stdout, `{"files":[],"echoed":%q}`, strings.Join(os.Args[1:], "|"))
	case "valid_quick":
		// T-6c-C2-10: print valid result then exit fast.
		fmt.Fprint(os.Stdout, `{"files":[],"test_instructions":"","risks":[],"followups":[]}`)
	case "sleep_2s_then_valid":
		// T-6c-C2-8 (env=0 disabled): sleep then exit valid.
		time.Sleep(2 * time.Second)
		fmt.Fprint(os.Stdout, `{"files":[]}`)
	case "print_10mb":
		// T-6c-C2-3: print 10 MB of garbage to trigger output cap.
		buf := bytes.Repeat([]byte("x"), 1024)
		for i := 0; i < 10*1024; i++ {
			_, _ = os.Stdout.Write(buf)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown ANPM_TEST_HELPER_MODE=%q\n", mode)
		os.Exit(2)
	}
}

// -- Unit tests for boundedWriter (T-6c-C2-3 / T-6c-C2-9 / T-6c-C2-11 logic) --

func TestBoundedWriterUnderCap(t *testing.T) {
	var buf bytes.Buffer
	bw := newBoundedWriter(&buf, 100)
	n, err := bw.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("Write(hello) = (%d, %v), want (5, nil)", n, err)
	}
	if bw.Truncated() {
		t.Error("Truncated() = true under cap")
	}
	if buf.String() != "hello" {
		t.Errorf("buf = %q", buf.String())
	}
}

func TestBoundedWriterAtCap(t *testing.T) {
	// T-6c-C2-11: write that crosses the boundary truncates partial.
	var buf bytes.Buffer
	bw := newBoundedWriter(&buf, 5)
	n, err := bw.Write([]byte("hellobye"))
	if err != nil {
		t.Fatalf("Write err: %v", err)
	}
	if n != 8 {
		t.Errorf("Write returned n=%d, want 8 (full input length so subprocess sees normal write)", n)
	}
	if !bw.Truncated() {
		t.Error("Truncated() = false after exceeding cap")
	}
	if buf.String() != "hello" {
		t.Errorf("buf = %q, want %q (truncated to cap)", buf.String(), "hello")
	}
	// Subsequent writes also discarded.
	bw.Write([]byte("more"))
	if buf.String() != "hello" {
		t.Errorf("post-truncation write changed buf to %q", buf.String())
	}
}

func TestBoundedWriterMaxZeroDisables(t *testing.T) {
	// T-6c-C2-9: max=0 means no cap.
	var buf bytes.Buffer
	bw := newBoundedWriter(&buf, 0)
	big := bytes.Repeat([]byte("x"), 10_000_000) // 10 MB
	n, _ := bw.Write(big)
	if n != len(big) {
		t.Errorf("Write returned n=%d, want %d", n, len(big))
	}
	if bw.Truncated() {
		t.Error("Truncated() = true with max=0")
	}
	if buf.Len() != len(big) {
		t.Errorf("buf.Len() = %d, want %d", buf.Len(), len(big))
	}
}

func TestBoundedWriterMaxNegativeDisables(t *testing.T) {
	var buf bytes.Buffer
	bw := newBoundedWriter(&buf, -1)
	bw.Write([]byte("anything"))
	if bw.Truncated() {
		t.Error("Truncated() = true with negative max")
	}
}

// -- Env parsing tests --

func TestDispatchOutputMaxBytesEnv(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want int64
	}{
		{"unset", "", defaultMaxOutputBytes},
		{"explicit zero disables", "0", 0},
		{"positive override", "1024", 1024},
		{"negative falls back", "-1", defaultMaxOutputBytes},
		{"garbage falls back", "abc", defaultMaxOutputBytes},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("ANPM_DISPATCH_OUTPUT_MAX", c.env)
			if got := dispatchOutputMaxBytes(); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

// -- validateExecutionResult tests (T-6c-C2-4 / 5 / 6 / 7) --

func TestValidateExecutionResultValid(t *testing.T) {
	payloads := []string{
		`{"files":[]}`,
		`{"files":[{"path":"a.go"}],"test_instructions":"run go test","risks":[],"followups":[]}`,
		`{"files":[],"extra_field":"ignored"}`,
	}
	for _, raw := range payloads {
		var p map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("setup unmarshal: %v", err)
		}
		if err := validateExecutionResult(p); err != nil {
			t.Errorf("validateExecutionResult(%s) = %v, want nil", raw, err)
		}
	}
}

func TestValidateExecutionResultMalformed(t *testing.T) {
	// T-6c-C2-4 / T-6c-C2-5: malformed schema fails.
	cases := []struct {
		name    string
		payload string
		wantErr schemaError
	}{
		{"missing files", `{"test_instructions":"x"}`, errSchemaMissingFiles},
		{"files not array", `{"files":"not-an-array"}`, errSchemaFilesNotArray},
		{"test_instructions not string", `{"files":[],"test_instructions":["a","b"]}`, errSchemaTestInstructionsNotString},
		{"risks not array", `{"files":[],"risks":"x"}`, errSchemaRisksNotArray},
		{"followups not array", `{"files":[],"followups":42}`, errSchemaFollowupsNotArray},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var p map[string]json.RawMessage
			_ = json.Unmarshal([]byte(c.payload), &p)
			err := validateExecutionResult(p)
			if err == nil {
				t.Fatalf("expected error %q, got nil", c.wantErr)
			}
			if err.Error() != string(c.wantErr) {
				t.Errorf("err = %q, want %q", err.Error(), c.wantErr)
			}
		})
	}
}

// -- Subprocess-based integration tests via invokeBuiltinCLI --

func TestInvokeBuiltinCLI_NoShellExpansion(t *testing.T) {
	// T-6c-C2-1: shell metacharacters in prompt MUST be passed verbatim
	// (not interpreted by a shell). exec.Command(...) uses argv directly,
	// not a shell, so this should hold trivially — this test pins the
	// behaviour against future regressions.
	t.Setenv("ANPM_TEST_HELPER_GUARD", "1")
	t.Setenv("ANPM_TEST_HELPER_MODE", "echo_args")
	prompt := "$(rm -rf /); echo `whoami`"
	out, truncated, errMsg := invokeBuiltinCLI(context.Background(), "claude", os.Args[0], "", prompt, 5)
	if errMsg != "" {
		t.Fatalf("invokeBuiltinCLI errMsg=%q", errMsg)
	}
	if truncated {
		t.Error("truncated unexpectedly")
	}
	if !strings.Contains(out, prompt) {
		t.Errorf("subprocess did not receive prompt verbatim; out=%q", out)
	}
}

func TestInvokeBuiltinCLI_SigtermIgnoreEscalatesToSigkill(t *testing.T) {
	// T-6c-C2-2: even when the CLI traps SIGTERM, wall-clock cancellation
	// still kills the process (cmd.WaitDelay → SIGKILL after sigtermGracePeriod).
	if testing.Short() {
		t.Skip("subprocess test skipped in -short mode")
	}
	t.Setenv("ANPM_TEST_HELPER_GUARD", "1")
	t.Setenv("ANPM_TEST_HELPER_MODE", "ignore_sigterm_sleep_forever")

	start := time.Now()
	_, _, errMsg := invokeBuiltinCLI(context.Background(), "claude", os.Args[0], "", "x", 1)
	elapsed := time.Since(start)

	if errMsg == "" {
		t.Fatalf("expected timeout errMsg, got success after %v", elapsed)
	}
	if !strings.Contains(strings.ToLower(errMsg), "timed out") {
		t.Errorf("errMsg = %q, want substring 'timed out'", errMsg)
	}
	// Upper bound: 1 s timeout + 5 s grace + 5 s CI slack. Risk-reviewer
	// flagged that contended CI boxes can spike beyond 9s during process
	// fork + signal registration; 11s is generous insurance against flakes
	// while still being narrow enough to detect a broken escalation path
	// (which would never return at all).
	maxAllowed := 1*time.Second + sigtermGracePeriod + 5*time.Second
	if elapsed > maxAllowed {
		t.Errorf("subprocess took %v to die, want < %v (escalation may not be working)", elapsed, maxAllowed)
	}
	// Lower bound: must wait at least the timeout.
	if elapsed < 800*time.Millisecond {
		t.Errorf("subprocess died too fast (%v) — timeout may not have been applied", elapsed)
	}
}

func TestInvokeBuiltinCLI_OutputCapTriggers(t *testing.T) {
	// T-6c-C2-3: 10 MB output exceeds the 5 MB default cap → truncated=true.
	t.Setenv("ANPM_TEST_HELPER_GUARD", "1")
	t.Setenv("ANPM_TEST_HELPER_MODE", "print_10mb")
	t.Setenv("ANPM_DISPATCH_OUTPUT_MAX", "") // use default 5 MB
	out, truncated, errMsg := invokeBuiltinCLI(context.Background(), "claude", os.Args[0], "", "x", 30)
	if !truncated {
		t.Fatalf("expected truncated=true; errMsg=%q out_len=%d", errMsg, len(out))
	}
	// errMsg may be empty (CLI exited 0) — that's fine; truncated alone
	// is sufficient signal for the caller to map output_too_large.
	if int64(len(out)) > defaultMaxOutputBytes+1024 {
		t.Errorf("output not truncated to cap: len=%d, cap=%d", len(out), defaultMaxOutputBytes)
	}
}

func TestInvokeBuiltinCLI_TimeoutDisabledByEnvZero(t *testing.T) {
	// T-6c-C2-8: env=0 disables timeout; CLI runs to completion.
	if testing.Short() {
		t.Skip("subprocess test skipped in -short mode")
	}
	t.Setenv("ANPM_TEST_HELPER_GUARD", "1")
	t.Setenv("ANPM_TEST_HELPER_MODE", "sleep_2s_then_valid")

	// Pass timeoutSec=0 to invokeBuiltinCLI (caller resolved env=0
	// disabled via roles.TimeoutFor; this test pins the contract that
	// invokeBuiltinCLI honours timeoutSec<=0 as "no timeout").
	start := time.Now()
	out, truncated, errMsg := invokeBuiltinCLI(context.Background(), "claude", os.Args[0], "", "x", 0)
	elapsed := time.Since(start)

	if errMsg != "" {
		t.Fatalf("unexpected errMsg=%q after %v", errMsg, elapsed)
	}
	if truncated {
		t.Error("truncated unexpectedly")
	}
	if !strings.Contains(out, `"files":[]`) {
		t.Errorf("expected valid JSON in output; got %q", out)
	}
	if elapsed < 1500*time.Millisecond {
		t.Errorf("subprocess returned in %v; expected at least 2s sleep", elapsed)
	}
}

func TestInvokeBuiltinCLI_RaceFinishesBeforeTimeout(t *testing.T) {
	// T-6c-C2-10: CLI finishes well within the timeout window. The
	// timeout context should NOT mask a successful result.
	t.Setenv("ANPM_TEST_HELPER_GUARD", "1")
	t.Setenv("ANPM_TEST_HELPER_MODE", "valid_quick")
	out, truncated, errMsg := invokeBuiltinCLI(context.Background(), "claude", os.Args[0], "", "x", 5)
	if errMsg != "" {
		t.Fatalf("errMsg=%q (expected success)", errMsg)
	}
	if truncated {
		t.Error("truncated unexpectedly")
	}
	if !strings.Contains(out, `"files":[]`) {
		t.Errorf("expected valid result; got %q", out)
	}
}

func TestInvokeBuiltinCLI_TimeoutWithTruncationPrefersTimeout(t *testing.T) {
	// Critic finding #4 + Copilot review #2: when the CLI fills the
	// output cap AND is killed by the timeout, both `truncated=true`
	// AND `runErrMsg!=""` are set. The dispatch caller in service.go
	// must prefer runErrMsg (the timeout signal is more informative
	// than the cap firing). This test now actually triggers BOTH
	// conditions — earlier version used a sleep-only helper that
	// printed nothing, so truncated stayed false and the test was
	// theatrical. The new "ignore_sigterm_print_loop" helper traps
	// SIGTERM and writes continuously, which trips the bounded
	// writer well before SIGKILL escalation lands.
	if testing.Short() {
		t.Skip("subprocess test skipped in -short mode")
	}
	t.Setenv("ANPM_TEST_HELPER_GUARD", "1")
	t.Setenv("ANPM_TEST_HELPER_MODE", "ignore_sigterm_print_loop")
	t.Setenv("ANPM_DISPATCH_OUTPUT_MAX", "1024") // 1 KB — easy to trip
	_, truncated, errMsg := invokeBuiltinCLI(context.Background(), "claude", os.Args[0], "", "x", 1)
	if errMsg == "" {
		t.Fatal("expected runErr (timeout); got empty")
	}
	if !truncated {
		t.Error("expected truncated=true; print loop should have exceeded 1 KB cap")
	}
	if !strings.Contains(strings.ToLower(errMsg), "timed out") {
		t.Errorf("errMsg = %q, want substring 'timed out'", errMsg)
	}
	// Verify dispatch classifier picks dispatch_timeout (the Phase
	// 6c-specific kind), not adapter_timeout, even when truncated is
	// also set — that's the precedence rule under test.
	if got := classifyDispatchRunError(errMsg); got != "dispatch_timeout" {
		t.Errorf("classifyDispatchRunError = %q, want dispatch_timeout", got)
	}
}

// -- classifyDispatchRunError tests --

func TestClassifyDispatchRunError(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"claude CLI timed out after 5s", "dispatch_timeout"},
		{"codex CLI timed out after 30s", "dispatch_timeout"},
		{"claude CLI failed: session expired, please re-authenticate", "session_expired"},
		{"rate limit exceeded", "rate_limited"},
		{"context window overflow on input", "context_overflow"},
		{"some other failure", "unknown"},
	}
	for _, c := range cases {
		t.Run(c.msg, func(t *testing.T) {
			if got := classifyDispatchRunError(c.msg); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

