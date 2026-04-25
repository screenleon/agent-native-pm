package connector

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Phase 6c L0 safety boundary helpers — see docs/phase6c-plan.md §3 C2.
//
// These are shared by the role-dispatch loop in service.go AND by the
// existing planning-run / probe paths in builtin_adapter.go. The two
// safety knobs (signal-escalation kill, output cap) are applied at the
// invokeBuiltinCLI level so every subprocess the connector spawns gets
// the same defense — not just dispatch.

const (
	// defaultMaxOutputBytes caps CLI stdout at 5 MB. Generous enough that
	// realistic structured output never trips it; tight enough that a
	// runaway CLI cannot exhaust memory.
	defaultMaxOutputBytes = 5 * 1024 * 1024

	// sigtermGracePeriod is how long we wait between SIGTERM and SIGKILL
	// when canceling a CLI subprocess. exec.CommandContext alone sends
	// SIGKILL by default — that gives well-behaved CLIs no chance to
	// flush output and clean up. We give them 5 s.
	sigtermGracePeriod = 5 * time.Second
)

// boundedWriter wraps an io.Writer with a maximum byte cap. Writes
// beyond the cap are silently discarded (Write returns len(p), nil) so
// the subprocess sees a normal write and keeps running until it exits
// on its own — this avoids the case where a CLI sees write errors and
// crashes mid-output.
//
// Concurrency contract: Write is called from at most one goroutine at
// a time (the io.Copy goroutine that drains the subprocess output).
// Truncated() may be read from any goroutine after that copy goroutine
// has signalled completion. Both `written` and `truncated` use atomic
// types as defense-in-depth so that a future refactor that introduces
// concurrent reads or multiple writers cannot silently introduce a
// data race. max <= 0 disables the cap entirely (every write delegates).
type boundedWriter struct {
	target    io.Writer
	max       int64
	written   atomic.Int64
	truncated atomic.Bool
}

func newBoundedWriter(target io.Writer, max int64) *boundedWriter {
	return &boundedWriter{target: target, max: max}
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	if b.max <= 0 {
		return b.target.Write(p)
	}
	if b.truncated.Load() {
		return len(p), nil
	}
	written := b.written.Load()
	remaining := b.max - written
	if int64(len(p)) <= remaining {
		n, err := b.target.Write(p)
		b.written.Add(int64(n))
		return n, err
	}
	if remaining > 0 {
		n, _ := b.target.Write(p[:remaining])
		b.written.Add(int64(n))
	}
	b.truncated.Store(true)
	return len(p), nil
}

func (b *boundedWriter) Truncated() bool { return b.truncated.Load() }

// dispatchOutputMaxBytes resolves the CLI stdout cap from environment.
// Resolution:
//   - unset / unparseable → defaultMaxOutputBytes (5 MB)
//   - 0                  → 0 (caller treats as "disabled")
//   - negative           → defaultMaxOutputBytes
//   - positive           → that many bytes
func dispatchOutputMaxBytes() int64 {
	v := strings.TrimSpace(os.Getenv("ANPM_DISPATCH_OUTPUT_MAX"))
	if v == "" {
		return defaultMaxOutputBytes
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return defaultMaxOutputBytes
	}
	if n == 0 {
		return 0
	}
	if n < 0 {
		return defaultMaxOutputBytes
	}
	return n
}

// applyDispatchKillEscalation wires cmd.Cancel and cmd.WaitDelay so
// that context cancellation sends SIGTERM first and waits sigtermGracePeriod
// before forcing SIGKILL. exec.CommandContext on its own sends SIGKILL
// immediately, which is too aggressive for CLIs that need a moment to
// flush output. CLIs that intentionally trap or ignore SIGTERM will
// still be killed after sigtermGracePeriod.
//
// Must be called BEFORE cmd.Start / pty.Start.
func applyDispatchKillEscalation(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Best-effort: ignore the error from Signal because the
		// process may have already exited between Cancel firing and
		// us getting the lock.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		return os.ErrProcessDone
	}
	cmd.WaitDelay = sigtermGracePeriod
}

// validateExecutionResult enforces the minimum role-result schema: the
// payload MUST contain a "files" array (may be empty), and the optional
// fields "test_instructions" / "risks" / "followups" must have the
// expected types when present. Forward-compat: extra fields are
// ignored. This is the C2(c) JSON schema minimum check.
//
// Returns nil on success, or an error describing the first violation.
func validateExecutionResult(payload map[string]json.RawMessage) error {
	rawFiles, hasFiles := payload["files"]
	if !hasFiles {
		return errSchemaMissingFiles
	}
	if !isJSONArray(rawFiles) {
		return errSchemaFilesNotArray
	}
	if raw, ok := payload["test_instructions"]; ok && !isJSONString(raw) {
		return errSchemaTestInstructionsNotString
	}
	if raw, ok := payload["risks"]; ok && !isJSONArray(raw) {
		return errSchemaRisksNotArray
	}
	if raw, ok := payload["followups"]; ok && !isJSONArray(raw) {
		return errSchemaFollowupsNotArray
	}
	return nil
}

// schemaError is a sentinel-style error type so callers can attach a
// stable string to ErrorKindInvalidResultSchema without parsing.
type schemaError string

func (e schemaError) Error() string { return string(e) }

const (
	errSchemaMissingFiles              schemaError = "execution result missing required `files` array"
	errSchemaFilesNotArray             schemaError = "execution result `files` must be a JSON array"
	errSchemaTestInstructionsNotString schemaError = "execution result `test_instructions` must be a string"
	errSchemaRisksNotArray             schemaError = "execution result `risks` must be a JSON array"
	errSchemaFollowupsNotArray         schemaError = "execution result `followups` must be a JSON array"
)

func isJSONArray(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "[")
}

func isJSONString(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "\"")
}
