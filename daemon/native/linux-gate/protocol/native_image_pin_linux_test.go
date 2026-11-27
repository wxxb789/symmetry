//go:build linux && amd64

package protocol

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestNativeImagePinRefusesSymlinkedComponents(t *testing.T) {
	config := OwnerConfig{AUID: uint32(os.Getuid()), AGID: uint32(os.Getgid())}
	staging := stageSymlinkedNativeImageParent(t, config, "symmetry-pin-")
	image := filepath.Join(staging, "real", "image")
	if err := os.WriteFile(image, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pinned, err := pinNativeImage(image, config)
	if err != nil {
		t.Fatalf("a symlink-free approved image must pin: %v", err)
	}
	t.Logf("native image pin accepted the symlink-free image %q", image)
	if err := pinned.close(); err != nil {
		t.Fatal(err)
	}
	assertUnlabelledNativeImagePinRefusal(t, filepath.Join(staging, "linked", "image"), config,
		`native-image parent component "linked"`)
	linkedImage := filepath.Join(staging, "linked-image")
	if err := os.Symlink(filepath.Join("real", "image"), linkedImage); err != nil {
		t.Fatal(err)
	}
	assertUnlabelledNativeImagePinRefusal(t, linkedImage, config, "is not A-approved")
}

func stageSymlinkedNativeImageParent(t *testing.T, config OwnerConfig, prefix string) string {
	t.Helper()
	staging, err := os.MkdirTemp(trustedNativeImagePinBase(t, config), prefix)
	if err != nil {
		t.Fatalf("create a trusted native-image staging directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(staging) })
	if err := os.Mkdir(filepath.Join(staging, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(staging, "linked")); err != nil {
		t.Fatal(err)
	}
	return staging
}

func TestNativeImagePinVerdictOnTheSystemShell(t *testing.T) {
	config := OwnerConfig{AUID: uint32(os.Getuid()), AGID: uint32(os.Getgid())}
	resolved, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Skipf("this environment has no resolvable system /bin/sh to pin: %v", err)
	}
	if resolved == "/bin/sh" {
		t.Skip("this environment's /bin/sh is not a symlink; there is no unresolved-path verdict to pin")
	}
	if err := trustedNativeImagePinChain(filepath.Dir(resolved), config); err != nil {
		t.Skipf("resolved system shell %q is not A-approvable in this environment: %v", resolved, err)
	}
	if err := trustedNativeImagePinImage(resolved, config); err != nil {
		t.Skipf("resolved system shell %q is not A-approvable in this environment: %v", resolved, err)
	}
	pinned, err := pinNativeImage(resolved, config)
	if err != nil {
		t.Fatalf("resolved system shell %q must pin: %v", resolved, err)
	}
	t.Logf("native image pin accepted the resolved system shell %q", resolved)
	if err := pinned.close(); err != nil {
		t.Fatal(err)
	}
	assertUnlabelledNativeImagePinRefusal(t, "/bin/sh", config, "")
}

func assertUnlabelledNativeImagePinRefusal(t *testing.T, path string, config OwnerConfig, wantDetail string) {
	t.Helper()
	pinned, err := pinNativeImage(path, config)
	if err == nil {
		_ = pinned.close()
		t.Fatalf("native image pin accepted %q, want a refusal", path)
	}
	if wantDetail != "" && !strings.Contains(err.Error(), wantDetail) {
		t.Fatalf("native image pin refusal for %q = %q, want detail %q", path, err, wantDetail)
	}
	t.Logf("native image pin refusal for %q: %v", path, err)
	var subStep *ReleaseSubStepError
	if errors.As(err, &subStep) {
		t.Fatalf("native image pin refusal for %q is labelled as a release sub-step, so the helper would report its stage: %v", path, err)
	}
}

func trustedNativeImagePinBase(t *testing.T, config OwnerConfig) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) || filepath.Clean(home) != home {
		t.Skipf("no canonical absolute HOME to hold a trusted native-image staging directory: %v", err)
	}
	if err := trustedNativeImagePinChain(home, config); err != nil {
		t.Skipf("HOME %q is not a trusted native-image pin base in this environment: %v", home, err)
	}
	probe, err := os.MkdirTemp(home, "symmetry-pin-base-")
	if err != nil {
		t.Skipf("HOME %q is a trusted native-image pin base but not writable for staging: %v", home, err)
	}
	_ = os.RemoveAll(probe)
	return home
}

func trustedNativeImagePinChain(directory string, config OwnerConfig) error {
	current := "/"
	for _, component := range strings.Split(strings.TrimPrefix(directory, "/"), "/") {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() {
			return fmt.Errorf("%s is not a directory", current)
		}
		if stat.Uid != 0 && uint32(stat.Uid) != config.AUID {
			return fmt.Errorf("%s is not root/A-owned", current)
		}
		if info.Mode().Perm()&0o022 != 0 || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("%s is group/world writable or not traversable", current)
		}
	}
	return nil
}

func trustedNativeImagePinImage(path string, config OwnerConfig) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || (stat.Uid != 0 && uint32(stat.Uid) != config.AUID) ||
		info.Mode().Perm()&0o022 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 ||
		stat.Nlink == 0 || info.Size() <= 0 || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("image %q must be a root/A-owned regular executable without set-ID bits or B write access", path)
	}
	return nil
}
