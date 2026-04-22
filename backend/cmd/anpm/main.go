// anpm is the command-line interface for Agent Native PM.
//
// Usage:
//
//	anpm serve             — build (if needed) and start the server
//	anpm status            — show whether the server is running for the current repo
//	anpm version           — print build version
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/screenleon/agent-native-pm/internal/config"
)

// Version is set at build time via -ldflags "-X main.Version=v1.2.3".
var Version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe()
	case "status":
		cmdStatus()
	case "version", "--version", "-v":
		cmdVersion()
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "anpm: unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`anpm — Agent Native PM

Usage:
  anpm serve      Build (if needed) and start the server for the current git repo
  anpm status     Show whether the server is already running for this repo
  anpm version    Print version information

Run "anpm serve" from inside any git repository. The server creates
.anpm/data.db in the git root and listens on a stable port derived from the
repository path.
`)
}

// cmdServe delegates to the serve script bundled alongside the binary.
// When run from the installed binary we expect serve.sh to live next to it;
// if it doesn't we fall back to running the server directly via config.Load().
func cmdServe() {
	binDir := filepath.Dir(selfPath())
	script := filepath.Join(binDir, "serve.sh")

	if _, err := os.Stat(script); err == nil {
		cmd := exec.Command("bash", script)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			fmt.Fprintf(os.Stderr, "anpm serve: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// No script found — run the server in-process using config auto-detection.
	// This is the path used when running a self-contained binary.
	cfg := config.Load()
	if !cfg.LocalMode {
		fmt.Fprintln(os.Stderr, "anpm serve: not inside a git repository (no .git found)")
		os.Exit(1)
	}

	serverBin := filepath.Join(binDir, "server")
	if runtime.GOOS == "windows" {
		serverBin += ".exe"
	}
	if _, err := os.Stat(serverBin); err != nil {
		fmt.Fprintf(os.Stderr, "anpm serve: server binary not found at %s\n", serverBin)
		os.Exit(1)
	}

	cmd := exec.Command(serverBin)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
	}
}

// cmdStatus checks whether a server is already listening on the derived port
// for the current git repo and prints a one-line summary.
func cmdStatus() {
	cfg := config.Load()
	if !cfg.LocalMode {
		fmt.Println("not inside a git repository")
		return
	}

	// Prefer the persisted port written by the server at startup; fall back to
	// the config-derived port (which may differ if the server probed for a free port).
	port := cfg.Port
	if cfg.AnpmDir != "" {
		if p := config.ReadPersistedPort(cfg.AnpmDir); p > 0 {
			port = fmt.Sprintf("%d", p)
		}
	}

	url := "http://127.0.0.1:" + port + "/api/meta"
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("stopped  (port %s — server not reachable)\n", port)
		return
	}
	defer resp.Body.Close()

	var envelope struct {
		Data struct {
			LocalMode   bool   `json:"local_mode"`
			ProjectName string `json:"project_name"`
			Port        string `json:"port"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		fmt.Printf("running  (port %s — could not decode response)\n", cfg.Port)
		return
	}

	d := envelope.Data
	if d.LocalMode {
		fmt.Printf("running  project=%q  url=http://127.0.0.1:%s\n", d.ProjectName, port)
	} else {
		fmt.Printf("running  url=http://127.0.0.1:%s\n", port)
	}
}

func cmdVersion() {
	v := Version
	if v == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
			v = info.Main.Version
		}
	}
	fmt.Printf("anpm %s  go%s  %s/%s\n", v, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func selfPath() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return os.Args[0]
}
