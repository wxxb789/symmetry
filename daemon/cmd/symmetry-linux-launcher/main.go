//go:build linux && amd64

// symmetry-linux-launcher-core is the static, root-owned stage entered only
// through the small setuid UID/FD/environment sanitizer in wrapper.c. It is
// intentionally transient and has no daemon or journal authority.
package main

import (
	"errors"
	"os"

	"github.com/wxxb789/symmetry/daemon/internal/launchercore"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "-clone-cleanup" {
		return launchercore.RunCloneCleanup()
	}
	if len(os.Args) != 1 {
		return errors.New("transient launcher accepts no arbitrary arguments")
	}
	return launchercore.RunLaunch()
}
