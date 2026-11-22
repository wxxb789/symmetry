package execution

import (
	"fmt"
	"os"
)

type startPipes struct {
	stdinRead   *os.File
	stdinWrite  *os.File
	stdoutRead  *os.File
	stdoutWrite *os.File
	stderrRead  *os.File
	stderrWrite *os.File
}

func createStartPipes() (*startPipes, error) {
	pipes := &startPipes{}
	var err error
	if pipes.stdinRead, pipes.stdinWrite, err = os.Pipe(); err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}
	if pipes.stdoutRead, pipes.stdoutWrite, err = os.Pipe(); err != nil {
		pipes.Close()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}
	if pipes.stderrRead, pipes.stderrWrite, err = os.Pipe(); err != nil {
		pipes.Close()
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}
	return pipes, nil
}

// Close and closeChildEnds are the two terminal dispositions of a value, so no
// end closes twice; a Close after closeChildEnds would close a live Process's ends.
func (pipes *startPipes) Close() {
	closeFiles(pipes.stdinRead, pipes.stdinWrite, pipes.stdoutRead, pipes.stdoutWrite, pipes.stderrRead, pipes.stderrWrite)
}

func (pipes *startPipes) closeChildEnds() {
	closeFiles(pipes.stdinRead, pipes.stdoutWrite, pipes.stderrWrite)
}
