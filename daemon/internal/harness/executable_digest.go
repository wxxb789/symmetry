package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// HashExecutableFile returns the SHA-256 of the exact executable bytes reached
// through PATH resolution. Native capability promotion binds a claim to one
// tested blob, so a resolved file that cannot be read or hashed stays
// unverifiable and therefore never promotes a capability.
func HashExecutableFile(ctx context.Context, executable string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("executable digest context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	buffer := make([]byte, 1<<20)
	for {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", contextErr
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			if _, writeErr := hasher.Write(buffer[:count]); writeErr != nil {
				return "", writeErr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}
