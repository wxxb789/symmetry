// symmetry-daemon runs one configured Symmetry execution daemon.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"syscall"

	"github.com/wxxb789/symmetry/daemon/internal/app"
	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-print-state-dir" {
		if err := runPrintStateDirCommand(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "-print-state-dir: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "-linux-v2-native-owner-witness" {
		if err := runLinuxV2NativeOwnerWitnessCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "-linux-v2-native-owner-witness: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "-linux-owner-helper-v2" {
		if err := platform.RunLinuxOwnerHelperV2(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "run Linux owner helper v2: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "-linux-owner-anchor" {
		if err := platform.RunLinuxOwnerAnchor(os.Args[2:], openLinuxOwnerAnchorCleanupVerifier); err != nil {
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

type daemonLinuxOwnerAnchorCleanupVerifier struct {
	reader *state.LinuxCloneCleanupReader
}

func (verifier *daemonLinuxOwnerAnchorCleanupVerifier) Verify(
	runID string,
	generation int64,
	grant authority.LinuxCloneGrant,
	intent authority.LinuxCloneCleanupIntent,
	prior *authority.LinuxCloneTargetRemovalProof,
	targetFDPresent bool,
) error {
	if verifier == nil || verifier.reader == nil {
		return errors.New("Linux owner anchor State cleanup reader is unavailable")
	}
	return verifier.reader.Verify(state.RunKey{RunID: runID, Generation: generation}, grant, intent, prior, targetFDPresent)
}

func (verifier *daemonLinuxOwnerAnchorCleanupVerifier) Close() error {
	if verifier == nil || verifier.reader == nil {
		return nil
	}
	err := verifier.reader.Close()
	verifier.reader = nil
	return err
}

func openLinuxOwnerAnchorCleanupVerifier(stateDir string) (platform.LinuxOwnerAnchorCleanupVerifier, error) {
	reader, err := state.OpenLinuxCloneCleanupReader(stateDir)
	if err != nil {
		return nil, err
	}
	return &daemonLinuxOwnerAnchorCleanupVerifier{reader: reader}, nil
}

func runPrintStateDirCommand(args []string, stdout io.Writer) error {
	if stdout == nil {
		return errors.New("StateDir print command requires stdout")
	}
	if runtime.GOOS != "linux" {
		return errors.New("StateDir print command is supported only on Linux")
	}
	flags := flag.NewFlagSet("-print-state-dir", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "path to the daemon JSON configuration")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *configPath == "" {
		return errors.New("requires only -config <daemon.json>")
	}
	value, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load daemon config: %w", err)
	}
	stateDir, err := normalizedLinuxStateDir(value.StateDir)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, stateDir)
	return err
}

func normalizedLinuxStateDir(configured string) (string, error) {
	if configured == "" {
		return "", errors.New("state_dir must be a nonempty path")
	}
	for _, character := range configured {
		if character < 0x20 || character == 0x7f {
			return "", errors.New("state_dir must not contain ASCII control characters")
		}
	}
	if path.Clean(configured) != configured || configured == "." || configured == ".." || strings.HasPrefix(configured, "../") {
		return "", errors.New("state_dir must not contain non-clean dot or parent path components")
	}
	stateDir := configured
	if !path.IsAbs(stateDir) {
		stateDir = path.Join("/", stateDir)
	}
	if !path.IsAbs(stateDir) || stateDir == "/" || path.Clean(stateDir) != stateDir {
		return "", errors.New("state_dir does not resolve to a canonical absolute directory")
	}
	if len(stateDir) > 4095 {
		return "", errors.New("state_dir exceeds the installed owner configuration path limit")
	}
	if err := rejectLinuxStateDirSymlinks(stateDir); err != nil {
		return "", err
	}
	return stateDir, nil
}

func rejectLinuxStateDirSymlinks(stateDir string) error {
	if stateDir == "" || !path.IsAbs(stateDir) || path.Clean(stateDir) != stateDir || stateDir == "/" {
		return errors.New("state_dir is not a canonical absolute path")
	}
	components := strings.Split(strings.TrimPrefix(stateDir, "/"), "/")
	current := "/"
	for index, component := range components {
		if component == "" || component == "." || component == ".." {
			return errors.New("state_dir contains an ambiguous path component")
		}
		current = path.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect state_dir component: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("state_dir contains a symlink component")
		}
		if index != len(components)-1 && !info.IsDir() {
			return errors.New("state_dir has a non-directory ancestor")
		}
		if index == len(components)-1 && !info.IsDir() {
			return errors.New("state_dir exists but is not a directory")
		}
	}
	return nil
}

func runLinuxV2NativeOwnerWitnessCommand(args []string) error {
	if os.Getenv(app.LinuxV2NativeOwnerWitnessEnvironment) != "1" {
		return errors.New("production Linux v2 native owner witness opt-in is required")
	}
	flags := flag.NewFlagSet("-linux-v2-native-owner-witness", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "", "path to the root-controlled witness daemon config")
	requestPath := flags.String("request", "", "path to the root-controlled authentic claim witness request")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *configPath == "" || *requestPath == "" {
		return errors.New("requires only -config <fixture> -request <root-controlled fixture>")
	}
	if err := platform.EnsureLinuxChildSubreaper(); err != nil {
		return fmt.Errorf("enable Linux child subreaper: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return app.RunLinuxV2NativeOwnerWitness(ctx, *configPath, *requestPath, os.Stdout)
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
