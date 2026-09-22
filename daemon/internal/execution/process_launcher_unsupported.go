//go:build !windows

package execution

import (
	"context"
	"os"
	"os/exec"
)

func defaultProcessLauncher(
	_ context.Context,
	_ *exec.Cmd,
	_ Invocation,
	_ *os.File,
	_ *os.File,
	_ *os.File,
) (*startedProcess, error) {
	return nil, errNativeProcessLauncherUnavailable
}
