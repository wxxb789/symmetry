//go:build linux && amd64

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

func TestInstalledLinuxOwnerCleanupRejectsBCallerScratch(t *testing.T) {
	if os.Getenv("SYMMETRY_LINUX_OWNER_TEST_SCRATCH") != "1" {
		t.Skip("requires an operator-installed root:A cleanup wrapper")
	}
	if os.Geteuid() != 0 {
		t.Skip("B caller denial probe must be launched by root with a real B credential")
	}
	owner, err := protocol.ReadInstalledOwnerConfig()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/usr/libexec/symmetry/symmetry-linux-launcher", "-clone-cleanup")
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	command.Dir = "/"
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid: owner.BUID, Gid: owner.BGID, Groups: []uint32{},
	}}
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("B was allowed to invoke the setuid cleanup wrapper: output=%q", output)
	}
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
		return
	}
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 126 {
		t.Fatalf("B wrapper denial did not fail closed with fixed exit 126: output=%q err=%v", output, err)
	}
}
