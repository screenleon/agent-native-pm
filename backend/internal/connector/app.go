package connector

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/screenleon/agent-native-pm/internal/models"
)

var Version = "dev"

type stringListFlag []string

type adapterFlagValues struct {
	command        *string
	args           *stringListFlag
	workingDir     *string
	timeoutSeconds *int
	maxOutputBytes *int64
}

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch args[0] {
	case "pair":
		if err := runPair(ctx, args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "pair failed: %s\n", err)
			return 1
		}
		return 0
	case "doctor":
		if err := runDoctor(ctx, args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "doctor failed: %s\n", err)
			return 1
		}
		return 0
	case "serve":
		if err := runServe(ctx, args[1:], stdout, stderr); err != nil && err != context.Canceled {
			fmt.Fprintf(stderr, "serve failed: %s\n", err)
			return 1
		}
		return 0
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return 1
	}
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, "anpm-connector commands:")
	fmt.Fprintln(output, "  pair   --server <url> --code <pairing-code> [adapter flags]")
	fmt.Fprintln(output, "  doctor [--state-path <path>] [adapter flags]")
	fmt.Fprintln(output, "  serve  [--state-path <path>] [adapter flags]")
	fmt.Fprintln(output, "adapter flags:")
	fmt.Fprintln(output, "  --adapter-command <path-or-command>")
	fmt.Fprintln(output, "  --adapter-arg <value> (repeatable)")
	fmt.Fprintln(output, "  --adapter-working-dir <absolute-or-relative-dir>")
	fmt.Fprintln(output, "  --adapter-timeout <seconds>")
	fmt.Fprintln(output, "  --adapter-max-output-bytes <bytes>")
}

func runPair(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	statePathFlag := fs.String("state-path", "", "state file path")
	serverURL := fs.String("server", "", "server URL")
	pairingCode := fs.String("code", "", "pairing code")
	label := fs.String("label", "", "connector label")
	platform := fs.String("platform", runtime.GOOS, "connector platform")
	clientVersion := fs.String("client-version", Version, "connector client version")
	adapterFlags := bindAdapterFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	adapterOverrides := adapterFlags.Overrides()
	if strings.TrimSpace(*serverURL) == "" {
		return fmt.Errorf("--server is required")
	}
	if strings.TrimSpace(*pairingCode) == "" {
		return fmt.Errorf("--code is required")
	}
	statePath, err := ResolveStatePath(*statePathFlag)
	if err != nil {
		return err
	}
	adapterConfig, _, err := applyAdapterOverrides(ExecJSONAdapterConfig{}, adapterOverrides)
	if err != nil && strings.TrimSpace(adapterOverrides.Command) != "" {
		return err
	}
	client := NewClient(*serverURL, "")
	client.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	response, err := client.Pair(ctx, models.PairLocalConnectorRequest{
		PairingCode:   *pairingCode,
		Label:         strings.TrimSpace(*label),
		Platform:      strings.TrimSpace(*platform),
		ClientVersion: strings.TrimSpace(*clientVersion),
		Capabilities:  buildCapabilities(adapterConfig),
		// Path B S2: declare wire-protocol awareness so the dispatcher
		// will hand us CLI-bound runs (account_binding_id != NULL). Old
		// connectors that don't send the field default to 0 server-side
		// and silently skip those runs (R3 mitigation, design §6.2).
		ProtocolVersion: ConnectorProtocolVersion,
	})
	if err != nil {
		return err
	}
	state := &State{
		SchemaVersion:  stateSchemaVersion,
		ServerURL:      strings.TrimRight(strings.TrimSpace(*serverURL), "/"),
		ConnectorID:    response.Connector.ID,
		ConnectorLabel: response.Connector.Label,
		ConnectorToken: response.ConnectorToken,
		Adapter:        adapterConfig,
	}
	if err := state.Save(statePath); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "paired connector %s (%s)\n", response.Connector.Label, response.Connector.ID)
	fmt.Fprintf(stdout, "state saved to %s\n", statePath)
	return nil
}

func runDoctor(ctx context.Context, args []string, stdout io.Writer) error {
	state, client, statePath, changed, err := loadRuntimeState(args, false)
	if err != nil {
		return err
	}
	if changed {
		if err := state.Save(statePath); err != nil {
			return err
		}
	}
	conn, hbErr := client.Heartbeat(ctx, models.LocalConnectorHeartbeatRequest{Capabilities: buildCapabilities(state.Adapter)})
	fmt.Fprintf(stdout, "state path: %s\n", statePath)
	fmt.Fprintf(stdout, "server: %s\n", state.ServerURL)
	fmt.Fprintf(stdout, "connector: %s (%s)\n", state.ConnectorLabel, state.ConnectorID)
	if strings.TrimSpace(state.Adapter.Command) == "" {
		fmt.Fprintf(stdout, "adapter command: (not configured)\n")
		fmt.Fprintf(stdout, "adapter working dir: (not configured)\n")
	} else {
		fmt.Fprintf(stdout, "adapter command: %s\n", state.Adapter.Command)
		fmt.Fprintf(stdout, "adapter working dir: %s\n", state.Adapter.WorkingDir)
	}
	if hbErr != nil {
		return fmt.Errorf("heartbeat verification failed: %w", hbErr)
	}
	fmt.Fprintf(stdout, "server status: %s\n", conn.Status)
	fmt.Fprintf(stdout, "last seen: %v\n", conn.LastSeenAt)
	if strings.TrimSpace(state.Adapter.Command) == "" {
		fmt.Fprintln(stdout, "note: no --adapter-command configured; serve will use the built-in adapter (claude or codex on PATH).")
	}
	return nil
}

func runServe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	statePathFlag := fs.String("state-path", "", "state file path")
	cliHealthIntervalSec := fs.Int("cli-health-interval", 300, "seconds between CLI health probes (0 to use default)")
	cliHealthDisabled := fs.Bool("cli-health-disabled", false, "disable CLI health probe loop")
	adapterFlags := bindAdapterFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	adapterOverrides := adapterFlags.Overrides()
	statePath, err := ResolveStatePath(*statePathFlag)
	if err != nil {
		return err
	}
	state, err := LoadState(statePath)
	if err != nil {
		return err
	}
	changed := false
	if state.Adapter.HasConfiguration() || adapterOverrides.HasValues() {
		adapterConfig, adapterChanged, err := applyAdapterOverrides(state.Adapter, adapterOverrides)
		if err != nil {
			return err
		}
		state.Adapter = adapterConfig
		changed = adapterChanged
	}
	if changed {
		if err := state.Save(statePath); err != nil {
			return err
		}
	}
	client := NewClient(state.ServerURL, state.ConnectorToken)
	client.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	cliHealthInterval := time.Duration(*cliHealthIntervalSec) * time.Second

	// Phase 6c PR-4: start the activity reporter so the server receives
	// execution-phase updates in real time. Best-effort: a failed POST is
	// logged and dropped so the main service loop is never blocked.
	reporter := NewActivityReporter(client)
	reporter.Start(ctx)

	service := &Service{
		Client:            client,
		State:             state,
		HeartbeatInterval: 30 * time.Second,
		PollInterval:      5 * time.Second,
		CliHealthInterval: cliHealthInterval,
		CliHealthDisabled: *cliHealthDisabled,
		Stdout:            stdout,
		Stderr:            stderr,
		ActivityReporter:  reporter,
	}
	fmt.Fprintf(stdout, "serving connector %s against %s\n", state.ConnectorLabel, state.ServerURL)
	emitAdapterDiagnostics(stdout, stderr, state.Adapter)
	return service.Run(ctx)
}

// emitAdapterDiagnostics prints a startup banner that surfaces common
// misconfigurations causing exit-126/exec-format errors before the first
// run is claimed. It checks: (a) adapter command is on PATH or is an
// absolute/relative path that exists and is executable, and (b) python3
// availability when the adapter looks like a Python script.
func emitAdapterDiagnostics(stdout, stderr io.Writer, adapter ExecJSONAdapterConfig) {
	cmd := strings.TrimSpace(adapter.Command)
	if cmd == "" {
		fmt.Fprintln(stdout, "using built-in adapter; CLI and adapter type are read from each run's configuration")
		return
	}
	resolved, lookErr := exec.LookPath(cmd)
	if lookErr != nil {
		fmt.Fprintf(stderr, "warn: adapter command %q not found on PATH: %v\n", cmd, lookErr)
	} else {
		fmt.Fprintf(stdout, "adapter command resolved: %s\n", resolved)
		if info, err := os.Stat(resolved); err == nil && info.Mode()&0o111 == 0 {
			fmt.Fprintf(stderr, "warn: adapter command %q is not executable; run 'chmod +x %s'\n", resolved, resolved)
		}
	}
	if strings.HasSuffix(strings.ToLower(cmd), ".py") || hasPythonAdapterArg(adapter.Args) {
		if py, err := exec.LookPath("python3"); err != nil {
			fmt.Fprintln(stderr, "warn: python3 not found on PATH; the reference adapters require it")
		} else {
			fmt.Fprintf(stdout, "python3 resolved: %s\n", py)
		}
	}
}

func hasPythonAdapterArg(args []string) bool {
	for _, a := range args {
		if strings.HasSuffix(strings.ToLower(a), ".py") {
			return true
		}
	}
	return false
}

func loadRuntimeState(args []string, requireAdapter bool) (*State, *Client, string, bool, error) {
	fs := flag.NewFlagSet("runtime", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	statePathFlag := fs.String("state-path", "", "state file path")
	adapterFlags := bindAdapterFlags(fs)
	if err := fs.Parse(args); err != nil {
		return nil, nil, "", false, err
	}
	adapterOverrides := adapterFlags.Overrides()
	statePath, err := ResolveStatePath(*statePathFlag)
	if err != nil {
		return nil, nil, "", false, err
	}
	state, err := LoadState(statePath)
	if err != nil {
		return nil, nil, "", false, err
	}
	changed := false
	if requireAdapter || state.Adapter.HasConfiguration() || adapterOverrides.HasValues() {
		adapterConfig, adapterChanged, err := applyAdapterOverrides(state.Adapter, adapterOverrides)
		if err != nil {
			return nil, nil, "", false, err
		}
		state.Adapter = adapterConfig
		changed = adapterChanged
	}
	client := NewClient(state.ServerURL, state.ConnectorToken)
	client.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	return state, client, statePath, changed, nil
}

func bindAdapterFlags(fs *flag.FlagSet) adapterFlagValues {
	var adapterArgs stringListFlag
	fs.Var(&adapterArgs, "adapter-arg", "repeatable adapter argument")
	command := fs.String("adapter-command", "", "exec-json adapter command")
	workingDir := fs.String("adapter-working-dir", "", "adapter working directory")
	timeoutSeconds := fs.Int("adapter-timeout", 0, "adapter timeout seconds")
	maxOutputBytes := fs.Int64("adapter-max-output-bytes", 0, "adapter max output bytes")
	return adapterFlagValues{
		command:        command,
		args:           &adapterArgs,
		workingDir:     workingDir,
		timeoutSeconds: timeoutSeconds,
		maxOutputBytes: maxOutputBytes,
	}
}

func (f adapterFlagValues) Overrides() AdapterOverrides {
	args := []string(nil)
	if f.args != nil {
		args = append(args, (*f.args)...)
	}
	command := ""
	if f.command != nil {
		command = *f.command
	}
	workingDir := ""
	if f.workingDir != nil {
		workingDir = *f.workingDir
	}
	timeoutSeconds := 0
	if f.timeoutSeconds != nil {
		timeoutSeconds = *f.timeoutSeconds
	}
	maxOutputBytes := int64(0)
	if f.maxOutputBytes != nil {
		maxOutputBytes = *f.maxOutputBytes
	}
	return AdapterOverrides{
		Command:        command,
		Args:           args,
		WorkingDir:     workingDir,
		TimeoutSeconds: timeoutSeconds,
		MaxOutputBytes: maxOutputBytes,
	}
}