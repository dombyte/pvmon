// Command pvmon polls PV devices and serves the dashboard. Without a subcommand it runs the
// server; the other subcommands are the CLI jobs of spec section 13.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/dombyte/pvmon/internal/buildinfo"
)

// Set by the linker (-X main.version=… -X main.commit=… -X main.date=…).
var (
	version = "dev"
	commit  = "" //nolint:gochecknoglobals // set by the linker
	date    = "" //nolint:gochecknoglobals // set by the linker
)

// Exit codes (spec 13).
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// errUsage marks a command line error; run maps it to exit code 2.
var errUsage = errors.New("usage error")

// env is what every subcommand gets from main.
type env struct {
	ctx    context.Context
	args   []string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	build  buildinfo.Info
}

type command func(e env) error

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run dispatches the subcommand and maps its result to an exit code. A panic is reported
// and exits 1 (no panics are expected; this is the last line of defence).
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer,
) (code int) {
	defer func() {
		if r := recover(); r != nil {
			writeLine(stderr, fmt.Sprintf("pvmon: panic: %v", r))
			code = exitFailure
		}
	}()
	name, rest := splitCommand(args)
	cmd, ok := commands()[name]
	if !ok {
		writeLine(stderr, fmt.Sprintf("pvmon: unknown command %q\n%s", name, usage))
		return exitUsage
	}
	err := cmd(env{
		ctx: ctx, args: rest, stdin: stdin, stdout: stdout, stderr: stderr,
		build: buildinfo.New(version, commit, date),
	})
	return exitCode(err, stderr)
}

func exitCode(err error, stderr io.Writer) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errUsage):
		writeLine(stderr, fmt.Sprintf("pvmon: %v\n%s", err, usage))
		return exitUsage
	default:
		writeLine(stderr, fmt.Sprintf("pvmon: %v", err))
		return exitFailure
	}
}

// splitCommand returns the subcommand name; flags first or no args mean "serve".
func splitCommand(args []string) (string, []string) {
	if len(args) == 0 || (len(args[0]) > 0 && args[0][0] == '-' && args[0] != "-version") {
		return "serve", args
	}
	if args[0] == "-version" {
		return "version", args[1:]
	}
	return args[0], args[1:]
}

func commands() map[string]command {
	return map[string]command{
		"serve":        runServe,
		"version":      runVersion,
		"profile":      runProfile,
		"dump":         runDump,
		"simulate":     runSimulate,
		"import-solis": runImportSolis,
		"user":         runUser,
		"token":        runToken,
		"config":       runConfig,
	}
}

const usage = `usage: pvmon [serve] [-config path] | version | profile validate <file>… |
  dump --device id [--count N --interval D] [-o file] [-config path] |
  simulate [--setup R1|R3|R4] [-config path] |
  import-solis --db solis.db --map map.yaml [-config path] |
  user add|reset-password <name> [--lang de|en] | user list [-config path] |
  token create --name n [--expires 90d] | token list | token revoke <id> [-config path] |
  config validate|export [-config path]`

func runVersion(e env) error {
	if len(e.args) > 0 {
		return fmt.Errorf("version takes no arguments: %w", errUsage)
	}
	writeLine(e.stdout, e.build.String())
	return nil
}

// writeLine writes s to w; a failing terminal write cannot be reported anywhere else.
func writeLine(w io.Writer, s string) {
	_, _ = fmt.Fprintln(w, s) // nowhere left to report a failed terminal write
}
