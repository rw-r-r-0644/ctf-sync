package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/rw-r-r-0644/ctf-sync/jeopardy"
)

type kvFlag map[string]string

func (k kvFlag) String() string {
	var parts []string
	for key, val := range k {
		parts = append(parts, fmt.Sprintf("%s=%s", key, val))
	}
	return strings.Join(parts, ",")
}

func (k kvFlag) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("must be key=value")
	}
	k[parts[0]] = parts[1]
	return nil
}

func main() {
	var (
		backendID  string
		configPath string
		settings   = make(kvFlag)
	)

	fs := flag.NewFlagSet("ctf-sync", flag.ExitOnError)
	fs.StringVar(&backendID, "backend", "", "Backend ID (e.g. ctfd_token, rctf)")
	fs.StringVar(&configPath, "config", "ctf-sync.json", "Path to config file")
	fs.Var(settings, "S", "Backend settings (key=value), can be repeated")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [global options] object [args...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nGlobal Options:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nCommands:\n")
		fmt.Fprintf(os.Stderr, "  list             List all challenges\n")
		fmt.Fprintf(os.Stderr, "  info <id>        Show challenge info\n")
		fmt.Fprintf(os.Stderr, "  get <id>         Download challenge files and info\n")
		fmt.Fprintf(os.Stderr, "  get-file <id> <file> Download a specific file\n")
	}

	if len(os.Args) < 2 {
		fs.Usage()
		os.Exit(1)
	}

	// We need to parse global flags manually or rely on users putting them before subcommand.
	// Standard flag package requires flags before non-flags.
	// So `ctfsync -backend ctfd list` works. `ctfsync list -backend ctfd` does not.
	// This is acceptable for a simple CLI.

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}

	cmdName := fs.Arg(0)
	cmdArgs := fs.Args()[1:]

	// Load config
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Merge flags into config
	if backendID != "" {
		cfg.Backend = backendID
	}
	for k, v := range settings {
		cfg.Config[k] = v
	}

	if cfg.Backend == "" {
		fmt.Fprintf(os.Stderr, "Error: backend type is required (via -backend or config file)\n")
		os.Exit(1)
	}

	// Create backend
	b, err := jeopardy.Build(cfg.Backend, cfg.Config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating backend: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	var cmdErr error

	switch cmdName {
	case "list":
		cmdErr = runList(ctx, b)
	case "info":
		if len(cmdArgs) < 1 {
			cmdErr = fmt.Errorf("usage: info <chall-id>")
		} else {
			cmdErr = runInfo(ctx, b, cmdArgs[0])
		}
	case "get":
		if len(cmdArgs) < 1 {
			cmdErr = fmt.Errorf("usage: get <chall-id>")
		} else {
			cmdErr = runGet(ctx, b, cmdArgs[0])
		}
	case "get-file":
		if len(cmdArgs) < 2 {
			cmdErr = fmt.Errorf("usage: get-file <chall-id> <file-name>")
		} else {
			cmdErr = runGetFile(ctx, b, cmdArgs)
		}
	case "submit":
		if len(cmdArgs) < 2 {
			cmdErr = fmt.Errorf("usage: submit <chall-id> <flag>")
		} else {
			cmdErr = runSubmit(ctx, b, cmdArgs)
		}
	default:
		cmdErr = fmt.Errorf("unknown command: %s", cmdName)
	}

	if cmdErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", cmdErr)
		os.Exit(1)
	}
}
