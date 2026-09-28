//go:build linux

package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

func TestLinuxCgroupV2KillMarksCanceledWriteAsNotAttempted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (*LinuxCgroupV2Leaf)(nil).Kill(ctx)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, errLinuxCgroupKillWriteNotAttempted) {
		t.Fatalf("Kill() error = %v, want preserved cancellation and no-write classification", err)
	}
}

func TestLinuxCgroupV2KillMarksInvalidPinAsNotAttempted(t *testing.T) {
	err := (&LinuxCgroupV2Leaf{parentFD: -1, leafFD: -1}).Kill(context.Background())
	if !errors.Is(err, ErrLinuxCgroupV2Unresolved) || !errors.Is(err, errLinuxCgroupKillWriteNotAttempted) {
		t.Fatalf("Kill() with invalid exact pin = %v, want unresolved no-write classification", err)
	}
}

func TestLinuxCgroupKillWriteFailureRemainsUnknown(t *testing.T) {
	err := writeLinuxCgroupKill(-1)
	if errors.Is(err, errLinuxCgroupKillWriteNotAttempted) || !errors.Is(err, ErrLinuxCgroupV2Unresolved) || !errors.Is(err, unix.EBADF) {
		t.Fatalf("writeLinuxCgroupKill() error = %v, want unresolved attempted write preserving EBADF and no no-write marker", err)
	}
}

func TestLinuxCgroupV2Openat2RejectsTraversalAndSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "inside"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)

	for _, name := range []string{"../" + filepath.Base(outside), "escape"} {
		if fd, err := openLinuxCgroupPath(rootFD, name, true); err == nil {
			_ = unix.Close(fd)
			t.Fatalf("openLinuxCgroupPath(%q) followed a traversal or symlink", name)
		}
	}
	fd, err := openLinuxCgroupPath(rootFD, "inside", true)
	if err != nil {
		t.Fatalf("open safe child directory: %v", err)
	}
	_ = unix.Close(fd)

	if _, err := OpenLinuxCgroupV2Parent(filepath.Join(root, "escape"), ""); err == nil {
		t.Fatal("OpenLinuxCgroupV2Parent accepted a symlinked mount point")
	}
	if _, err := OpenLinuxCgroupV2Parent(root, "../outside"); err == nil {
		t.Fatal("OpenLinuxCgroupV2Parent accepted a delegated path traversal")
	}
}

func TestLinuxCgroupV2ReplacedLeafDoesNotMatchPinnedIdentity(t *testing.T) {
	root := t.TempDir()
	leafName := "0123456789abcdef0123456789abcdef"
	leafPath := filepath.Join(root, leafName)
	if err := os.Mkdir(leafPath, 0700); err != nil {
		t.Fatal(err)
	}
	parentFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(parentFD)
	leafFD, err := openLinuxCgroupPath(parentFD, leafName, true)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(leafFD)
	original, err := statLinuxCgroupFD(leafFD)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(leafPath, leafPath+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(leafPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := requireLinuxCgroupNameAt(parentFD, leafName, original); !errors.Is(err, ErrLinuxCgroupV2Unresolved) {
		t.Fatalf("replacement path check error = %v, want unresolved identity mismatch", err)
	}
	replacementFD, err := openLinuxCgroupPath(parentFD, leafName, true)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(replacementFD)
	replacement, err := statLinuxCgroupFD(replacementFD)
	if err != nil {
		t.Fatal(err)
	}
	if original.device == replacement.device && original.inode == replacement.inode && original.mount == replacement.mount {
		t.Fatal("replacement directory unexpectedly reused the still-pinned original identity")
	}
}

func TestLinuxCgroupV2RejectsPathOnlyAdoptionAndNonCgroupControls(t *testing.T) {
	if _, err := OpenLinuxCgroupV2Parent(t.TempDir(), ""); err == nil {
		t.Fatal("ordinary filesystem was accepted as a delegated cgroup2 parent")
	}
	if validLinuxCgroupLeafName("../0123456789abcdef0123456789abcdef") || validLinuxCgroupLeafName("0123456789abcdef0123456789abcdeF") {
		t.Fatal("non-random-shape or noncanonical leaf name was accepted")
	}

	root := t.TempDir()
	leafName := "fedcba9876543210fedcba9876543210"
	if err := os.Mkdir(filepath.Join(root, leafName), 0700); err != nil {
		t.Fatal(err)
	}
	parentFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(parentFD)
	leafFD, err := openLinuxCgroupPath(parentFD, leafName, true)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(leafFD)
	parentStat, err := statLinuxCgroupFD(parentFD)
	if err != nil {
		t.Fatal(err)
	}
	leafStat, err := statLinuxCgroupFD(leafFD)
	if err != nil {
		t.Fatal(err)
	}
	identity := authority.LinuxCgroupParentIdentity{
		BootID: readLinuxCgroupBootID(), MountID: parentStat.mount,
		Device: parentStat.device, Inode: parentStat.inode, Path: "/delegated",
	}
	parent := &LinuxCgroupV2Parent{fd: parentFD, identity: identity}
	expected := authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: identity,
		LeafName: leafName, LeafDevice: leafStat.device, LeafInode: leafStat.inode,
	}
	if _, err := parent.AdoptLeaf(leafName, -1, expected); err == nil {
		t.Fatal("path/inode identity without a caller-supplied live FD was adopted")
	}
	if _, err := parent.bindOpenedLeaf(leafName, leafFD, &expected); !errors.Is(err, ErrLinuxCgroupV2NotCgroup2) {
		t.Fatalf("ordinary directory adoption error = %v, want cgroup2 negative-control rejection", err)
	}
}

func TestParseLinuxCgroupV2MountInfoAndEventsRejectMalformedInputs(t *testing.T) {
	mounts, err := parseLinuxCgroupV2MountInfo(strings.NewReader("29 20 0:28 /delegated /sys/fs/cgroup rw,nosuid - cgroup2 cgroup rw\n"))
	if err != nil || len(mounts) != 1 || mounts[0].id != 29 || mounts[0].root != "/delegated" {
		t.Fatalf("parsed cgroup2 mount = %#v, error = %v", mounts, err)
	}
	if _, err := parseLinuxCgroupV2MountInfo(strings.NewReader("bad entry\n")); err == nil {
		t.Fatal("malformed mountinfo line was accepted")
	}
	if populated, err := parseLinuxCgroupPopulated("populated 0\nfrozen 0\n"); err != nil || populated {
		t.Fatalf("empty cgroup.events = populated %v, error %v", populated, err)
	}
	if _, err := parseLinuxCgroupPopulated("populated 0\npopulated 1\n"); err == nil {
		t.Fatal("duplicate populated values were accepted")
	}
}

func TestLinuxCgroupV2ReleaseZeroValueIsUnissued(t *testing.T) {
	proof, issued := (LinuxCgroupV2Release{}).Snapshot()
	if issued || proof.Version != 0 || proof.Status != "" || proof.StopReceipt != nil {
		t.Fatalf("zero LinuxCgroupV2Release snapshot = %#v, issued=%v; want no proof and unissued", proof, issued)
	}
	preGateProof, issued := (LinuxCgroupV2Release{}).SnapshotPreGate()
	if issued || preGateProof.Version != 0 || preGateProof.Receipt.Version != 0 {
		t.Fatalf("zero LinuxCgroupV2Release pre-gate snapshot = %#v, issued=%v; want no proof and unissued", preGateProof, issued)
	}
}

func TestLinuxCgroupGateProductionAuthenticatorBindsSecretAndLauncher(t *testing.T) {
	secret := strings.Repeat("ab", authority.SecretBytes)
	launcher := authority.LinuxCgroupProcessIdentity{
		PID: 42, Identity: "linux:v2:00000000-0000-0000-0000-000000000000:1:42:987654",
	}
	launchToken := strings.Repeat("cd", authority.TokenBytes)
	authenticator, err := linuxgate.SignGateNeverProducedAuthenticator(secret, launchToken, launcher)
	if err != nil {
		t.Fatalf("sign no-gate launcher result: %v", err)
	}
	result := authority.LinuxCgroupGateProductionResult{
		Version: authority.LinuxCgroupIdentityVersion, LaunchToken: launchToken, Launcher: launcher,
		Outcome: authority.LinuxCgroupGateOutcomeNeverCreated, Authenticator: authenticator,
	}
	if err := verifyLinuxCgroupGateProductionAuthenticator(secret, result); err != nil {
		t.Fatalf("verify authentic no-gate result: %v", err)
	}
	wrongSecret := strings.Repeat("ef", authority.SecretBytes)
	if err := verifyLinuxCgroupGateProductionAuthenticator(wrongSecret, result); err == nil {
		t.Fatal("no-gate result authenticated with a different per-run secret")
	}

	mutations := []struct {
		name   string
		mutate func(*authority.LinuxCgroupGateProductionResult)
	}{
		{name: "launch token", mutate: func(candidate *authority.LinuxCgroupGateProductionResult) {
			candidate.LaunchToken = strings.Repeat("ef", authority.TokenBytes)
		}},
		{name: "launcher PID", mutate: func(candidate *authority.LinuxCgroupGateProductionResult) { candidate.Launcher.PID++ }},
		{name: "launcher identity", mutate: func(candidate *authority.LinuxCgroupGateProductionResult) { candidate.Launcher.Identity += ":changed" }},
		{name: "authenticator", mutate: func(candidate *authority.LinuxCgroupGateProductionResult) {
			if candidate.Authenticator[0] == '0' {
				candidate.Authenticator = "1" + candidate.Authenticator[1:]
			} else {
				candidate.Authenticator = "0" + candidate.Authenticator[1:]
			}
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := result
			mutation.mutate(&candidate)
			if err := verifyLinuxCgroupGateProductionAuthenticator(secret, candidate); err == nil {
				t.Fatal("tampered no-gate launcher result authenticated")
			}
		})
	}
}
