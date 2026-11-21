package execution

import (
	"fmt"
	"os"
)

// startPipes owns the six standard-I/O pipe ends of one Start sequence. The
// bootstrap failure exits close every end created so far with Close (nil ends
// are skipped, so a partially created value closes exactly the prefix the base
// closeFiles calls closed). After a successful launch the child-owned ends are
// closed by closeChildEnds so the parent readers observe EOF.
type startPipes struct {
	stdinRead   *os.File
	stdinWrite  *os.File
	stdoutRead  *os.File
	stdoutWrite *os.File
	stderrRead  *os.File
	stderrWrite *os.File
}

// createStartPipes creates the three pipe pairs in the base order (stdin,
// stdout, stderr). A failure closes the ends created before it and keeps the
// exact base error text.
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

// Close closes every pipe end this value owns. It is safe on a partially
// created value. Close and closeChildEnds are the two terminal
// dispositions of a value: every Close caller returns from Start immediately
// (or abandons the value on creation failure), and closeChildEnds runs only on
// the success path, so no end is closed twice. A Close after closeChildEnds
// would also close the reader/write ends a live Process owns — do not add such
// a caller.
func (pipes *startPipes) Close() {
	closeFiles(pipes.stdinRead, pipes.stdinWrite, pipes.stdoutRead, pipes.stdoutWrite, pipes.stderrRead, pipes.stderrWrite)
}

// closeChildEnds closes the three ends that belong only to the child after
// launch.
func (pipes *startPipes) closeChildEnds() {
	closeFiles(pipes.stdinRead, pipes.stdoutWrite, pipes.stderrWrite)
}
