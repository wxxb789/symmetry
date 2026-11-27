//go:build linux && amd64

package protocol

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreparedGateReleaseLabelsTheNativeImagePinRefusal(t *testing.T) {
	config := OwnerConfig{AUID: uint32(os.Getuid()), AGID: uint32(os.Getgid())}
	t.Run("top-level image path", func(t *testing.T) {
		assertReleasePinRefusalLabel(t, config, fmt.Sprintf("/symmetry-release-pin-%d", os.Getpid()),
			"must have an A-controlled parent directory")
	})
	t.Run("symlinked parent component", func(t *testing.T) {
		staging := stageSymlinkedNativeImageParent(t, config, "symmetry-release-pin-")
		assertReleasePinRefusalLabel(t, config, filepath.Join(staging, "linked", "image"), `parent component "linked"`)
	})
}

func assertReleasePinRefusalLabel(t *testing.T, config OwnerConfig, path, wantDetail string) {
	t.Helper()
	pipes := newScratchNativePipes(t)
	guard, _ := newScratchReleaseGuard(t, time.Minute)
	gate := &PreparedGate{state: gatePrepared, config: config}
	command := NativeCommand{
		Path: path, Args: []string{"symmetry-release-pin-label"}, Env: []string{}, Dir: "/",
		Stdin: pipes.stdinRead, Stdout: pipes.stdoutWrite, Stderr: pipes.stderrWrite,
	}
	err := gate.Release(context.Background(), 0, command, guard, nil)
	if err == nil {
		t.Fatalf("PreparedGate.Release accepted the unapprovable native image %q", path)
	}
	var refusal *ReleaseSubStepError
	if !errors.As(err, &refusal) {
		t.Fatalf("the pin refusal for %q is unlabelled, so the helper renders stage=none: %v", path, err)
	}
	if refusal.SubStep != "native_image" || refusal.Arm != "image_pin" {
		t.Fatalf("pin refusal for %q labelled stage=%q arm=%q, want stage=native_image arm=image_pin",
			path, refusal.SubStep, refusal.Arm)
	}
	if refusal.Errno != 0 || refusal.WaitClass != "" || refusal.ReadClass != "" || refusal.GuestStage != "" {
		t.Fatalf("pin refusal for %q carries detail outside the stage and arm: %+v", path, *refusal)
	}
	if !strings.Contains(err.Error(), "pin A-approved native executable before RELEASE") ||
		!strings.Contains(err.Error(), wantDetail) {
		t.Fatalf("labelled pin refusal for %q dropped its detail %q: %v", path, wantDetail, err)
	}
}
