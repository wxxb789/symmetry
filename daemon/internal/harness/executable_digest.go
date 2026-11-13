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

// ExecutableHasher is implemented by runners that can supply the SHA-256 of the
// exact executable bytes. The production runners hash the resolved file; tests
// may supply a recorded digest so a promotion gate stays deterministic.
type ExecutableHasher interface {
	ExecutableDigest(context.Context, string) (string, error)
}

// ResolveExecutableDigest returns the SHA-256 of the exact executable bytes, or
// false when the bytes cannot be verified. An unverifiable executable never
// promotes a native capability, so callers fail closed instead of failing the
// probe. A runner that cannot supply a digest is hashed through
// HashExecutableFile.
func ResolveExecutableDigest(ctx context.Context, executable string, runner any) (string, bool) {
	if ctx == nil || ctx.Err() != nil {
		return "", false
	}
	if hasher, ok := runner.(ExecutableHasher); ok {
		digest, err := hasher.ExecutableDigest(ctx, executable)
		if err != nil || ctx.Err() != nil {
			return "", false
		}
		return digest, digest != ""
	}
	digest, err := HashExecutableFile(ctx, executable)
	if err != nil {
		return "", false
	}
	return digest, true
}
