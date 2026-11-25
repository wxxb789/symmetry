//go:build !(linux && amd64)

package linuxreadback

import (
	"context"
	"errors"
)

// Serve never substitutes a non-Linux readback for the Linux root reader.
func Serve(context.Context, ServeOptions) error {
	return errors.ErrUnsupported
}
