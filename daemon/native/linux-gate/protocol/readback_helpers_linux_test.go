//go:build linux && amd64

package protocol

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/linuxreadback"
)

const nativeReadbackTimeout = 60 * time.Second

// captureNativeBProcess asks the fixture's root reader for the exact parked
// B process: PID/start identity, approved image, environment and descriptors.
func captureNativeBProcess(identity ProcessIdentity, executable, leaf string) (linuxreadback.Capture, error) {
	channel, err := linuxreadback.ChannelFromEnv()
	if err != nil {
		return linuxreadback.Capture{}, err
	}
	info, err := os.Stat(executable)
	if err != nil {
		return linuxreadback.Capture{}, fmt.Errorf("stat approved native image: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino == 0 {
		return linuxreadback.Capture{}, errors.New("approved native image has no stable device/inode identity")
	}
	want := linuxreadback.Want{
		PID: identity.PID, StartTime: identity.StartTime, BootID: identity.BootID,
		Leaf: leaf, Executable: executable, ExeDevice: uint64(stat.Dev), ExeInode: stat.Ino,
		Environ: true, Descriptors: true,
	}
	capture, err := linuxreadback.Read(channel, want, nativeReadbackTimeout)
	if err != nil {
		return linuxreadback.Capture{}, err
	}
	// The reader's gate binds the approved file by dev/ino; the returned path is
	// the kernel's canonical /proc/<pid>/exe readlink, which usrmerge may rewrite.
	if capture.ExeDevice != want.ExeDevice || capture.ExeInode != want.ExeInode {
		return linuxreadback.Capture{}, errors.New("root readback did not bind the approved same-PID image")
	}
	return capture, nil
}

func scratchLeafPath() (string, error) {
	leaf := os.Getenv("SYMMETRY_LINUX_OWNER_TEST_LEAF")
	if !filepath.IsAbs(leaf) || filepath.Clean(leaf) != leaf || leaf == "/" ||
		!strings.HasPrefix(leaf, "/sys/fs/cgroup/") {
		return "", errors.New("root readback requires the exact operator-created scratch leaf in SYMMETRY_LINUX_OWNER_TEST_LEAF")
	}
	return leaf, nil
}

func readbackDescriptorTarget(capture linuxreadback.Capture, number int) (string, bool) {
	for _, descriptor := range capture.Descriptors {
		if descriptor.Number == number {
			return descriptor.Target, true
		}
	}
	return "", false
}
