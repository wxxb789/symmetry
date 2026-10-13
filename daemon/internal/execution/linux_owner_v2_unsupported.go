//go:build !linux || !amd64

package execution

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

func launchLinuxV2Process(
	context.Context,
	*exec.Cmd,
	Invocation,
	*os.File,
	*os.File,
	*os.File,
) (*startedProcess, error) {
	return nil, errors.New("Linux v2 process launch is supported only on Linux x86-64")
}
