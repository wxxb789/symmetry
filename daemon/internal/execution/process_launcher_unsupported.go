//go:build !windows

package execution

import (
	"os"
	"os/exec"
)

func defaultProcessLauncher(
	*exec.Cmd,
	Invocation,
	*os.File,
	*os.File,
	*os.File,
) (*startedProcess, error) {
	return nil, errNativeProcessLauncherUnavailable
}
