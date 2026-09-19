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
	if len(os.Args) > 1 && os.Args[1] == "-containment-supervisor" {
		if err := platform.RunContainmentSupervisor(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "run containment supervisor: %v\n", err)
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
