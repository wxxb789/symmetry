// symmetry-daemon runs one configured Symmetry execution daemon.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/wxxb789/symmetry/daemon/internal/app"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-linux-owner-helper-v2" {
		if err := platform.RunLinuxOwnerHelperV2(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "run Linux owner helper v2: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "-linux-owner-anchor" {
		if err := platform.RunLinuxOwnerAnchor(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "run Linux owner anchor: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "-containment-supervisor" {
		if err := platform.RunContainmentSupervisor(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "run containment supervisor: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "-verify-legacy-owner-drained" ||
		os.Args[1] == "-verify-linux-owner-install" || os.Args[1] == "-recover-containment-only") {
		if err := runConfiguredOwnerCommand(os.Args[1], os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", os.Args[1], err)
			os.Exit(1)
		}
		return
	}

	configPath := flag.String("config", "", "path to the daemon JSON configuration")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "-config is required")
		os.Exit(2)
	}
	if err := platform.EnsureLinuxChildSubreaper(); err != nil {
		fmt.Fprintf(os.Stderr, "enable Linux child subreaper: %v\n", err)
		os.Exit(1)
	}

	value, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, value, app.WithLogWriter(os.Stdout)); err != nil {
		fmt.Fprintf(os.Stderr, "run daemon: %v\n", err)
		os.Exit(1)
	}
}

func runConfiguredOwnerCommand(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "", "path to the daemon JSON configuration")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *configPath == "" {
		return fmt.Errorf("%s requires only -config <path>", command)
	}
	value, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	switch command {
	case "-verify-legacy-owner-drained":
		if err := app.VerifyLegacyLinuxOwnersDrained(value); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "legacy Linux owners are drained; pending Goal deliveries may remain")
		return nil
	case "-verify-linux-owner-install":
		if err := app.VerifyLinuxOwnerInstallation(value); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, app.LinuxOwnerPreflightRuntimeBoundary)
		return nil
	case "-recover-containment-only":
		if err := platform.EnsureLinuxChildSubreaper(); err != nil {
			return fmt.Errorf("enable Linux child subreaper: %w", err)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := app.RecoverContainmentOnly(ctx, value); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "containment recovery completed without registration, claims, scheduling, or native launch")
		return nil
	default:
		return fmt.Errorf("unsupported owner command %q", command)
	}
}
