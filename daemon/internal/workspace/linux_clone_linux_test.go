//go:build linux && amd64

package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"golang.org/x/sys/unix"
)

func TestLinuxCloneSeedAndStandaloneCommitKeepSourceUnchanged(t *testing.T) {
	fixture := newLinuxCloneWorkspaceFixture(t)
	baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := os.ReadFile(fixture.intent.SeedPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(bundle)
	if got, want := fixture.grant.Seed.Digest, "sha256:"+hex.EncodeToString(digest[:]); got != want {
		t.Fatalf("seed digest = %q, want exact bundle digest %q", got, want)
	}
	if fixture.grant.Seed.Size != int64(len(bundle)) || fixture.grant.Seed.BaseCommit != fixture.intent.BaseCommit {
		t.Fatalf("seed receipt = %#v, want exact size and admitted OID", fixture.grant.Seed)
	}
	info, err := os.Stat(fixture.intent.SeedPath)
	if err != nil {
		t.Fatal(err)
	}
	identity := info.Sys().(*syscall.Stat_t)
	if info.Mode().Perm()&0o222 != 0 || uint64(identity.Dev) != fixture.grant.Seed.Device || uint64(identity.Ino) != fixture.grant.Seed.Inode {
		t.Fatalf("seed permissions/inode do not match immutable receipt: mode=%#o stat=%#v receipt=%#v", info.Mode().Perm(), identity, fixture.grant.Seed)
	}
	if got, want := gitOutput(t, fixture.repository, "bundle", "list-heads", fixture.intent.SeedPath), fixture.intent.BaseCommit+" "+baseRef; got != want {
		t.Fatalf("bundle heads = %q, want exactly named base ref %q", got, want)
	}
	if _, err := linuxCloneGitOutput(context.Background(), fixture.repository, 64<<10, errors.New("unexpected bundle verification limit"), "bundle", "verify", fixture.intent.SeedPath); err != nil {
		t.Fatalf("verify full seed bundle: %v", err)
	}

	prepared, err := fixture.manager.PrepareLinuxClone(context.Background(), fixture.grant)
	if err != nil {
		t.Fatalf("PrepareLinuxClone() error = %v", err)
	}
	if prepared.Path != fixture.intent.Target || !prepared.linuxClone {
		t.Fatalf("prepared clone = %#v, want exact target and clone mode", prepared)
	}
	if _, err := os.Lstat(fixture.intent.Target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("A created or inspected B target before gate checkout: lstat error = %v", err)
	}
	initialFingerprint, err := Fingerprint(context.Background(), prepared)
	if err != nil {
		t.Fatalf("Fingerprint() before B checkout: %v", err)
	}

	refsBefore := gitOutput(t, fixture.repository, "for-each-ref", "--format=%(refname) %(objectname)")
	fixture.createBCloneAndCommit(t)
	if err := fixture.manager.ValidateLinuxCloneCheckout(fixture.grant, fixture.checkout); err != nil {
		t.Fatalf("ValidateLinuxCloneCheckout() error = %v", err)
	}
	if got := gitOutput(t, fixture.target, "rev-parse", "--is-inside-work-tree"); got != "true" {
		t.Fatalf("B clone is not a standalone Git checkout: is-inside-work-tree = %q", got)
	}
	gitMetadata, err := os.Lstat(filepath.Join(fixture.target, ".git"))
	if err != nil || !gitMetadata.IsDir() {
		t.Fatalf("B clone .git metadata is not a standalone directory: info=%v error=%v", gitMetadata, err)
	}
	bCommon := gitOutput(t, fixture.target, "rev-parse", "--git-common-dir")
	aCommon := gitOutput(t, fixture.repository, "rev-parse", "--git-common-dir")
	if same, err := sameDirectory(resolveLinuxCloneGitPath(fixture.target, bCommon), resolveLinuxCloneGitPath(fixture.repository, aCommon)); err != nil || same {
		t.Fatalf("B .git common directory aliases A source: same=%t error=%v", same, err)
	}
	if got := gitOutput(t, fixture.target, "rev-parse", "HEAD"); got == fixture.intent.BaseCommit {
		t.Fatal("B git add/commit did not advance the standalone clone")
	}
	if got := gitOutput(t, fixture.repository, "rev-parse", "HEAD"); got != fixture.intent.BaseCommit {
		t.Fatalf("B commit changed A source HEAD: got %s, want %s", got, fixture.intent.BaseCommit)
	}
	if got := gitOutput(t, fixture.repository, "for-each-ref", "--format=%(refname) %(objectname)"); got != refsBefore {
		t.Fatalf("B commit changed A source refs: before=%q after=%q", refsBefore, got)
	}
	fingerprintAfterClone, err := Fingerprint(context.Background(), prepared)
	if err != nil || fingerprintAfterClone != initialFingerprint {
		t.Fatalf("clone fingerprint changed after B checkout: before=%q after=%q error=%v", initialFingerprint, fingerprintAfterClone, err)
	}
}

func TestLinuxCloneSeedReplayAndChangedBytes(t *testing.T) {
	fixture := newLinuxCloneWorkspaceFixture(t)
	first := fixture.grant.Seed
	second, err := fixture.manager.PrepareLinuxCloneSeed(context.Background(), fixture.intent)
	if err != nil {
		t.Fatalf("same-run PrepareLinuxCloneSeed() error = %v", err)
	}
	if !first.Equal(second) {
		t.Fatalf("same-run seed replay = %#v, want original inode/digest receipt %#v", second, first)
	}

	if err := os.Chmod(fixture.intent.SeedPath, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(fixture.intent.SeedPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("X"), 0); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.intent.SeedPath, 0o440); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.PrepareLinuxClone(context.Background(), fixture.grant); err == nil {
		t.Fatal("PrepareLinuxClone() accepted seed bytes changed after State receipt")
	}
	if _, err := fixture.manager.PrepareLinuxCloneSeed(context.Background(), fixture.intent); err == nil {
		t.Fatal("PrepareLinuxCloneSeed() overwrote a changed same-run seed")
	}
}

func TestLinuxCloneSeedRecoversOnlyExactIntentTemporary(t *testing.T) {
	t.Run("valid temp is atomically published", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		temporaryName, err := linuxCloneSeedTempName(fixture.intent.Nonce)
		if err != nil {
			t.Fatal(err)
		}
		temporaryPath := filepath.Join(filepath.Dir(fixture.intent.SeedPath), temporaryName)
		bundle, err := os.ReadFile(fixture.intent.SeedPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(fixture.intent.SeedPath); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(temporaryPath, bundle, 0o600); err != nil {
			t.Fatal(err)
		}
		recovered, err := fixture.manager.PrepareLinuxCloneSeed(context.Background(), fixture.intent)
		if err != nil {
			t.Fatalf("recover exact Linux clone seed temp: %v", err)
		}
		if err := recovered.Validate(); err != nil || recovered.Digest != fixture.grant.Seed.Digest || recovered.Size != fixture.grant.Seed.Size {
			t.Fatalf("recovered seed receipt = %#v, validation error=%v", recovered, err)
		}
		info, err := os.Stat(fixture.intent.SeedPath)
		if err != nil || info.Mode().Perm() != 0o440 {
			t.Fatalf("recovered seed mode = %v, stat error=%v; want immutable 0440", info, err)
		}
		if _, err := os.Lstat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("published seed temp remains: %v", err)
		}
	})

	t.Run("partial exact temp remains unresolved", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		temporaryName, err := linuxCloneSeedTempName(fixture.intent.Nonce)
		if err != nil {
			t.Fatal(err)
		}
		temporaryPath := filepath.Join(filepath.Dir(fixture.intent.SeedPath), temporaryName)
		if err := os.Remove(fixture.intent.SeedPath); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(temporaryPath, []byte("partial bundle"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.PrepareLinuxCloneSeed(context.Background(), fixture.intent); !errors.Is(err, ErrLinuxCloneProcessUnresolved) {
			t.Fatalf("PrepareLinuxCloneSeed(partial temp) error = %v, want unresolved", err)
		}
		if _, err := os.Lstat(temporaryPath); err != nil {
			t.Fatalf("failed recovery removed the exact partial temp: %v", err)
		}
		if _, err := os.Lstat(fixture.intent.SeedPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed recovery published a partial seed: %v", err)
		}
	})
}

func TestLinuxCloneRejectsStaleIntentAndInaccessibleSeed(t *testing.T) {
	fixture := newLinuxCloneWorkspaceFixture(t)
	stale := fixture.grant
	stale.Intent.Generation++
	if _, err := fixture.manager.PrepareLinuxClone(context.Background(), stale); err == nil {
		t.Fatal("PrepareLinuxClone() accepted a stale generation")
	}

	if os.Geteuid() == 0 {
		t.Skip("root can bypass directory permission denial")
	}
	if err := os.Chmod(fixture.seedRoot, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(fixture.seedRoot, 0o700)
	if _, err := fixture.manager.PrepareLinuxClone(context.Background(), fixture.grant); err == nil {
		t.Fatal("PrepareLinuxClone() accepted an inaccessible A-owned seed exchange")
	}
}

func TestLinuxCloneAbortProofRequiresAbsentTargetAndReplays(t *testing.T) {
	t.Run("target exists", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		if err := os.MkdirAll(filepath.Dir(fixture.target), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(fixture.target, 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := fixture.manager.AbortLinuxCloneBeforeCheckout(context.Background(), fixture.intent, &fixture.grant.Seed, nil, nil, nil)
		if err == nil {
			t.Fatal("abort produced a no-target proof while an unreceipted B target existed")
		}
		if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
			t.Fatalf("abort removed the A seed despite a hostile target: %v", err)
		}
		baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyLinuxCloneRef(context.Background(), fixture.repository, baseRef, fixture.intent.BaseCommit); err != nil {
			t.Fatalf("abort removed the A base ref despite a hostile target: %v", err)
		}
	})

	t.Run("safe replay", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		first, err := fixture.manager.AbortLinuxCloneBeforeCheckout(context.Background(), fixture.intent, &fixture.grant.Seed, nil, nil, nil)
		if err != nil {
			t.Fatalf("AbortLinuxCloneBeforeCheckout() error = %v", err)
		}
		if err := first.Validate(); err != nil || first.SeedReceipt == nil || !first.SeedReceipt.Equal(fixture.grant.Seed) ||
			!first.TargetAbsent || !first.SeedAbsent || !first.BaseRefAbsent || !first.PromotionRefAbsent {
			t.Fatalf("abort proof = %#v, validation error=%v", first, err)
		}
		if _, err := os.Lstat(fixture.intent.SeedPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("aborted seed remains: %v", err)
		}
		second, err := fixture.manager.AbortLinuxCloneBeforeCheckout(context.Background(), fixture.intent, &fixture.grant.Seed, nil, nil, nil)
		if err != nil || !first.Equal(second) {
			t.Fatalf("abort replay = %#v, %v; want exact proof %#v", second, err, first)
		}
	})
}

func TestLinuxCloneAbortAfterNormalReleaseRetainsPossiblyExecutedBClone(t *testing.T) {
	release := linuxCloneNormalReleaseProof(t)
	t.Run("B clone renamed before checkout-ready retains A material", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		fixture.createBCloneAndCommit(t)
		displaced := fixture.target + "-renamed-before-ready"
		if err := os.Rename(fixture.target, displaced); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.AbortLinuxCloneBeforeCheckout(context.Background(), fixture.intent, &fixture.grant.Seed, nil, nil, &release); err == nil {
			t.Fatal("normal release proved no B execution after checkout was renamed before ready")
		}
		if _, err := os.Lstat(fixture.target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("renamed B target unexpectedly remains at its original path: %v", err)
		}
		displacedInfo, err := os.Stat(displaced)
		if err != nil {
			t.Fatalf("renamed original B checkout was lost: %v", err)
		}
		displacedStat := displacedInfo.Sys().(*syscall.Stat_t)
		if uint64(displacedStat.Dev) != fixture.checkout.TargetDevice || uint64(displacedStat.Ino) != fixture.checkout.TargetInode {
			t.Fatalf("renamed B inode = %d:%d, want original checkout %d:%d", displacedStat.Dev, displacedStat.Ino, fixture.checkout.TargetDevice, fixture.checkout.TargetInode)
		}
		if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
			t.Fatalf("normal-release abort removed A seed despite possible B execution: %v", err)
		}
		baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyLinuxCloneRef(context.Background(), fixture.repository, baseRef, fixture.intent.BaseCommit); err != nil {
			t.Fatalf("normal-release abort removed A base ref despite possible B execution: %v", err)
		}
	})
	t.Run("fresh target absence is not no-checkout proof", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		if _, err := fixture.manager.AbortLinuxCloneBeforeCheckout(context.Background(), fixture.intent, &fixture.grant.Seed, nil, nil, &release); err == nil {
			t.Fatal("normal release plus fresh ENOENT produced a no-target abort proof")
		}
		if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
			t.Fatalf("normal-release abort removed A seed after fresh ENOENT: %v", err)
		}
		baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyLinuxCloneRef(context.Background(), fixture.repository, baseRef, fixture.intent.BaseCommit); err != nil {
			t.Fatalf("normal-release abort removed A base ref after fresh ENOENT: %v", err)
		}
	})
}

func TestLinuxCloneAbortRemovesOnlyExactSeedTemporary(t *testing.T) {
	t.Run("exact partial temp is removed", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		temporaryName, err := linuxCloneSeedTempName(fixture.intent.Nonce)
		if err != nil {
			t.Fatal(err)
		}
		temporaryPath := filepath.Join(filepath.Dir(fixture.intent.SeedPath), temporaryName)
		if err := os.WriteFile(temporaryPath, []byte("partial bundle"), 0o600); err != nil {
			t.Fatal(err)
		}
		proof, err := fixture.manager.AbortLinuxCloneBeforeCheckout(context.Background(), fixture.intent, &fixture.grant.Seed, nil, nil, nil)
		if err != nil {
			t.Fatalf("AbortLinuxCloneBeforeCheckout() error = %v", err)
		}
		if err := proof.Validate(); err != nil || !proof.SeedAbsent || !proof.TargetAbsent {
			t.Fatalf("abort proof = %#v, validation error=%v", proof, err)
		}
		if _, err := os.Lstat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("exact fenced seed temp remains: %v", err)
		}
	})

	t.Run("unknown temp retains seed", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		temporaryName := ".clone-seed-" + strings.Repeat("0", 24)
		if exactName, err := linuxCloneSeedTempName(fixture.intent.Nonce); err != nil {
			t.Fatal(err)
		} else if temporaryName == exactName {
			temporaryName = ".clone-seed-" + strings.Repeat("1", 24)
		}
		temporaryPath := filepath.Join(filepath.Dir(fixture.intent.SeedPath), temporaryName)
		if err := os.WriteFile(temporaryPath, []byte("unowned temp"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.AbortLinuxCloneBeforeCheckout(context.Background(), fixture.intent, &fixture.grant.Seed, nil, nil, nil); err == nil {
			t.Fatal("abort removed an unrecognized seed temp")
		}
		if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
			t.Fatalf("abort removed the seed despite an unrecognized temp: %v", err)
		}
		if _, err := os.Lstat(temporaryPath); err != nil {
			t.Fatalf("abort removed the unrecognized temp: %v", err)
		}
	})
}

func TestLinuxCloneCleanupTargetPinsOnlyExactNoFollowInode(t *testing.T) {
	t.Run("existing target uses O_PATH and checkout identity", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		if err := os.MkdirAll(filepath.Dir(fixture.target), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(fixture.target, 0o700); err != nil {
			t.Fatal(err)
		}
		checkout := linuxCloneCheckoutReceipt(t, fixture.intent, fixture.grant, fixture.target)
		target, err := openLinuxCloneCleanupTarget(context.Background(), fixture.root, fixture.intent.Target, checkout)
		if err != nil {
			t.Fatalf("pin exact B cleanup target: %v", err)
		}
		defer target.close()
		rootFlags, err := unix.FcntlInt(uintptr(target.rootFD), unix.F_GETFL, 0)
		if err != nil || rootFlags&unix.O_PATH == 0 {
			t.Fatalf("root fd flags = %#x, error=%v; want O_PATH", rootFlags, err)
		}
		flags, err := unix.FcntlInt(uintptr(target.targetFD), unix.F_GETFL, 0)
		if err != nil || flags&unix.O_PATH == 0 {
			t.Fatalf("target fd flags = %#x, error=%v; want O_PATH", flags, err)
		}
		var opened unix.Stat_t
		if err := unix.Fstat(target.targetFD, &opened); err != nil ||
			uint64(opened.Dev) != checkout.TargetDevice || uint64(opened.Ino) != checkout.TargetInode {
			t.Fatalf("pinned target = %#v, error=%v; want checkout inode", opened, err)
		}
		if err := target.verifyRootPinned(context.Background()); err != nil {
			t.Fatalf("configured root identity readback: %v", err)
		}
		if err := target.verifyAbsent(context.Background()); err == nil {
			t.Fatal("cleanup target absence accepted a still-present target")
		}
	})

	t.Run("already absent is only a filesystem observation", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		if err := os.MkdirAll(filepath.Dir(fixture.target), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(fixture.target, 0o700); err != nil {
			t.Fatal(err)
		}
		checkout := linuxCloneCheckoutReceipt(t, fixture.intent, fixture.grant, fixture.target)
		if err := os.Remove(fixture.target); err != nil {
			t.Fatal(err)
		}
		target, err := openLinuxCloneCleanupTarget(context.Background(), fixture.root, fixture.intent.Target, checkout)
		if err != nil {
			t.Fatalf("open absent checkout for exact replay: %v", err)
		}
		defer target.close()
		if target.targetFD != -1 {
			t.Fatalf("absent target fd = %d, want absent observation", target.targetFD)
		}
		if err := target.verifyAbsent(context.Background()); err != nil {
			t.Fatalf("exact absent path readback: %v", err)
		}
	})

	t.Run("replacement inode and final symlink are rejected", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		if err := os.MkdirAll(filepath.Dir(fixture.target), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(fixture.target, 0o700); err != nil {
			t.Fatal(err)
		}
		checkout := linuxCloneCheckoutReceipt(t, fixture.intent, fixture.grant, fixture.target)
		displaced := fixture.target + "-displaced"
		if err := os.Rename(fixture.target, displaced); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(fixture.target, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := openLinuxCloneCleanupTarget(context.Background(), fixture.root, fixture.intent.Target, checkout); err == nil {
			t.Fatal("cleanup opened a replacement target inode")
		}
		if err := os.Remove(fixture.target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(displaced, fixture.target); err != nil {
			t.Fatal(err)
		}
		if _, err := openLinuxCloneCleanupTarget(context.Background(), fixture.root, fixture.intent.Target, checkout); err == nil {
			t.Fatal("cleanup followed a symlink at the exact target path")
		}
	})
}

func TestLinuxCloneOPathRootAllowsMountedRootButForbidsNestedMount(t *testing.T) {
	if rootFD, _, err := openLinuxCloneOPathDirPath("/"); err == nil {
		_ = unix.Close(rootFD)
		t.Fatal("filesystem root was accepted as a B clone root")
	}
	shm := "/dev/shm"
	rootStat, err := os.Stat("/")
	if err != nil {
		t.Fatal(err)
	}
	shmStat, err := os.Lstat(shm)
	if err != nil || !shmStat.IsDir() || shmStat.Mode()&os.ModeSymlink != 0 {
		t.Skip("/dev/shm is unavailable for a mounted-root traversal test")
	}
	if uint64(rootStat.Sys().(*syscall.Stat_t).Dev) == uint64(shmStat.Sys().(*syscall.Stat_t).Dev) {
		t.Skip("/dev/shm is not a distinct mount in this test environment")
	}
	root, err := os.MkdirTemp(shm, "linux-clone-mounted-root-")
	if err != nil {
		t.Skipf("cannot create a private root on /dev/shm: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	rootFD, rootInfo, err := openLinuxCloneOPathDirPath(root)
	if err != nil {
		t.Fatalf("pin configured root on a separate mount: %v", err)
	}
	defer unix.Close(rootFD)
	if uint64(rootInfo.Dev) != uint64(shmStat.Sys().(*syscall.Stat_t).Dev) {
		t.Fatalf("pinned root device = %d, want /dev/shm device %d", rootInfo.Dev, shmStat.Sys().(*syscall.Stat_t).Dev)
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if nestedFD, err := openLinuxCloneOPathDirAt(rootFD, "nested"); err != nil {
		t.Fatalf("open same-mount subtree under pinned root: %v", err)
	} else {
		_ = unix.Close(nestedFD)
	}
	if err := unix.Mount("/proc", nested, "", unix.MS_BIND, ""); err != nil {
		t.Skipf("bind mounts unavailable; mounted root traversal was verified: %v", err)
	}
	defer unix.Unmount(nested, 0)
	if nestedFD, err := openLinuxCloneOPathDirAt(rootFD, "nested"); err == nil {
		_ = unix.Close(nestedFD)
		t.Fatal("pinned B root traversal crossed a nested mount")
	}
}

func TestLinuxCloneQuarantineRejectsForeignScratchPrefix(t *testing.T) {
	fixture := newLinuxCloneWorkspaceFixture(t)
	fixture.createBCloneAndCommit(t)
	foreignName := ".symmetry-clone-quarantine-" + strings.Repeat("f", 24)
	_, expectedName, _, err := linuxCloneScratchNames(fixture.intent.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	if foreignName == expectedName {
		foreignName = ".symmetry-clone-quarantine-" + strings.Repeat("e", 24)
	}
	foreignPath := filepath.Join(filepath.Dir(fixture.intent.SeedPath), foreignName)
	if err := os.WriteFile(foreignPath, []byte("unrecognized scratch"), 0o600); err != nil {
		t.Fatal(err)
	}
	target, err := openLinuxCloneTarget(fixture.intent, fixture.checkout, fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	defer target.close()
	if _, err := createLinuxCloneQuarantine(context.Background(), target, filepath.Dir(fixture.intent.SeedPath), fixture.intent, len(fixture.intent.BaseCommit)); !errors.Is(err, ErrLinuxCloneProcessUnresolved) {
		t.Fatalf("quarantine creation with foreign scratch = %v, want unresolved", err)
	}
	if _, err := os.Lstat(foreignPath); err != nil {
		t.Fatalf("quarantine creation removed foreign scratch: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(fixture.intent.SeedPath), expectedName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("quarantine creation replaced or adopted scratch: %v", err)
	}
}

func TestLinuxCloneNoTargetRewalkRejectsReplacedParent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "b-root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "binding-native", "run-race")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "generation-1")
	observed := make(chan struct{})
	swapped := make(chan struct{})
	var swapErr error
	go func() {
		<-observed
		swapErr = os.Rename(parent, parent+"-old")
		if swapErr == nil {
			swapErr = os.Mkdir(parent, 0o700)
		}
		close(swapped)
	}()
	err := verifyLinuxCloneTargetAbsentUnderRootWithReadback(context.Background(), root, target, func() {
		close(observed)
		<-swapped
	})
	if swapErr != nil {
		t.Fatalf("replace target parent at absence barrier: %v", swapErr)
	}
	if err == nil {
		t.Fatal("no-target check accepted absence observed through a detached parent inode")
	}
}

func TestLinuxCloneRejectsAlternatesSymlinksChangedAndOversizedObjects(t *testing.T) {
	t.Run("alternates", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		fixture.createBCloneAndCommit(t)
		info := filepath.Join(fixture.target, ".git", "objects", "info")
		if err := os.MkdirAll(info, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(info, "alternates"), []byte(filepath.Join(fixture.repository, ".git", "objects")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.quarantine(t); err == nil {
			t.Fatal("A quarantine accepted B Git object alternates")
		}
	})

	t.Run("shallow", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		fixture.createBCloneAndCommit(t)
		shallow := filepath.Join(fixture.target, ".git", "shallow")
		if err := os.WriteFile(shallow, []byte(fixture.intent.BaseCommit+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.quarantine(t); err == nil {
			t.Fatal("A quarantine accepted a shallow B clone")
		}
	})

	t.Run("symlink", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		fixture.createBCloneAndCommit(t)
		objects := filepath.Join(fixture.target, ".git", "objects")
		entries, err := os.ReadDir(objects)
		if err != nil {
			t.Fatal(err)
		}
		used := make(map[string]bool)
		for _, entry := range entries {
			used[entry.Name()] = true
		}
		fanout := ""
		for first := '0'; first <= 'f'; first++ {
			if first > '9' && first < 'a' {
				first = 'a'
			}
			for second := '0'; second <= 'f'; second++ {
				if second > '9' && second < 'a' {
					second = 'a'
				}
				candidate := string([]rune{first, second})
				if !used[candidate] {
					fanout = candidate
					break
				}
			}
			if fanout != "" {
				break
			}
		}
		if fanout == "" {
			t.Fatal("could not find unused loose-object fanout")
		}
		if err := os.Symlink(filepath.Join(fixture.repository, ".git", "objects"), filepath.Join(objects, fanout)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := fixture.quarantine(t); err == nil {
			t.Fatal("A quarantine traversed a B object-store symlink")
		}
	})

	t.Run("changed object", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		fixture.createBCloneAndCommit(t)
		objectPath := filepath.Join(fixture.target, ".git", "objects", fixture.candidate[:2], fixture.candidate[2:])
		if _, err := os.Stat(objectPath); err != nil {
			t.Fatalf("new B commit is not a loose object: %v", err)
		}
		if err := os.Chmod(objectPath, 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(objectPath, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		var one [1]byte
		if _, err := file.ReadAt(one[:], 0); err != nil {
			file.Close()
			t.Fatal(err)
		}
		one[0] ^= 0xff
		if _, err := file.WriteAt(one[:], 0); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(objectPath, 0o444); err != nil {
			t.Fatal(err)
		}
		quarantine, err := fixture.quarantine(t)
		if err != nil {
			t.Fatal(err)
		}
		defer fixture.removeQuarantine(t, quarantine)
		if err := verifyLinuxCloneCandidate(context.Background(), quarantine, fixture.intent.BaseCommit, fixture.candidate); err == nil {
			t.Fatal("A Git accepted a modified B commit object")
		}
	})

	t.Run("oversized object", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		fixture.createBCloneAndCommit(t)
		objects := filepath.Join(fixture.target, ".git", "objects")
		objectIDBytes := make([]byte, len(fixture.intent.BaseCommit)/2)
		if _, err := rand.Read(objectIDBytes); err != nil {
			t.Fatal(err)
		}
		objectID := hex.EncodeToString(objectIDBytes)
		fanout := filepath.Join(objects, objectID[:2])
		if err := os.Mkdir(fanout, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
		path := filepath.Join(fanout, objectID[2:])
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(maxLinuxCloneObjectBytes + 1); err != nil {
			file.Close()
			t.Skipf("sparse oversized files unavailable: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.quarantine(t); !errors.Is(err, errLinuxCloneObjectsTooLarge) {
			t.Fatalf("A quarantine error = %v, want bounded object-store rejection", err)
		}
	})
}

func TestLinuxCloneAOnlyImportIgnoresBConfigAndPromotionRefReplays(t *testing.T) {
	fixture := newLinuxCloneWorkspaceFixture(t)
	fixture.createBCloneAndCommit(t)
	if os.Geteuid() == 0 {
		t.Skip("Git fsmonitor command test is not meaningful as root")
	}
	marker := filepath.Join(t.TempDir(), "fsmonitor-ran")
	fsmonitor := filepath.Join(t.TempDir(), "fsmonitor.sh")
	if err := os.WriteFile(fsmonitor, []byte("#!/bin/sh\nprintf invoked > "+marker+"\nprintf '### 1\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, fixture.target, "config", "core.fsmonitor", fsmonitor)
	_ = os.Remove(marker)
	probe := exec.Command("git", "-C", fixture.target, "status", "--short")
	_ = probe.Run()
	if _, err := os.Stat(marker); err != nil {
		t.Skip("installed Git did not invoke the configured fsmonitor probe")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	target, err := openLinuxCloneTarget(fixture.intent, fixture.checkout, fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	defer target.close()
	quarantine, err := createLinuxCloneQuarantine(context.Background(), target, filepath.Dir(fixture.intent.SeedPath), fixture.intent, len(fixture.intent.BaseCommit))
	if err != nil {
		t.Fatalf("copy B object bytes: %v", err)
	}
	defer fixture.removeQuarantine(t, quarantine)
	if err := verifyLinuxCloneCandidate(context.Background(), quarantine, fixture.intent.BaseCommit, fixture.candidate); err != nil {
		t.Fatalf("verify candidate in A-only quarantine: %v", err)
	}
	treeDigest, err := linuxCloneTreeDigest(context.Background(), quarantine, fixture.candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := importLinuxCloneCandidate(context.Background(), fixture.repository, quarantine, fixture.candidate); err != nil {
		t.Fatalf("import validated quarantine: %v", err)
	}
	first, err := promoteLinuxCloneRef(context.Background(), fixture.repository, fixture.intent, fixture.candidate, treeDigest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := promoteLinuxCloneRef(context.Background(), fixture.repository, fixture.intent, fixture.candidate, treeDigest)
	if err != nil || !first.Equal(second) {
		t.Fatalf("same-run promotion replay = %#v, %v; want %#v", second, err, first)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("A-only quarantine/source Git executed B core.fsmonitor: stat error = %v", err)
	}
	if got := gitOutput(t, fixture.repository, "rev-parse", "HEAD"); got != fixture.intent.BaseCommit {
		t.Fatalf("candidate import/promotion changed A source HEAD: %s", got)
	}
	candidate := authority.LinuxClonePromotionIntent{
		Version: authority.LinuxCloneVersion, ResourceID: fixture.intent.ResourceID,
		Commit: fixture.candidate, TreeDigest: treeDigest,
	}
	if _, err := fixture.manager.PromoteLinuxClone(context.Background(), fixture.grant, fixture.checkout, candidate, platform.LinuxCgroupV2Release{}); err == nil {
		t.Fatal("PromoteLinuxClone accepted an unissued release capability")
	}
}

func TestLinuxCloneExistingPromotionReadbackSurvivesTargetLoss(t *testing.T) {
	fixture, journal := newLinuxCloneCleanupFixture(t, "00000000-0000-4000-8000-000000000105")
	if err := os.Remove(fixture.target); err != nil {
		t.Fatal(err)
	}
	scratchPaths := createLinuxCloneTestScratch(t, fixture)
	expected := journal.LinuxClonePromotionReceipt.Clone()
	candidate := journal.LinuxClonePromotionIntent.Clone()
	journal.LinuxClonePromotionReceipt = nil
	journal.LinuxCloneCleanupPending = false
	journal.LinuxCloneCleanupIntent = nil
	journal.LinuxCloneCleanupProof = nil
	recovered, err := fixture.manager.ReadExistingLinuxClonePromotion(context.Background(), fixture.intent, candidate, journal, platform.LinuxCgroupV2Release{})
	if err != nil {
		t.Fatalf("ReadExistingLinuxClonePromotion() after target loss: %v", err)
	}
	if !recovered.Equal(expected) {
		t.Fatalf("promotion readback = %#v, want exact A promotion", recovered)
	}
	if _, err := os.Lstat(fixture.target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("promotion readback recreated or changed B target: %v", err)
	}
	for _, scratchPath := range scratchPaths {
		if _, err := os.Lstat(scratchPath); err != nil {
			t.Fatalf("promotion readback changed A scratch %q: %v", scratchPath, err)
		}
	}
}

func TestLinuxCloneCleanupRequiresBeginReadback(t *testing.T) {
	fixture := newLinuxCloneWorkspaceFixture(t)
	fixture.createBCloneAndCommit(t)
	if _, err := fixture.manager.CleanupLinuxClone(context.Background(), fixture.grant, fixture.checkout, state.RunJournal{}); err == nil {
		t.Fatal("CleanupLinuxClone accepted an absent Store readback")
	}
	if _, err := os.Stat(fixture.target); err != nil {
		t.Fatalf("cleanup without Begin readback changed the B target: %v", err)
	}
}

func TestLinuxCloneCleanupUsesRootReceiptAndReplaysReadOnly(t *testing.T) {
	fixture, journal := newLinuxCloneCleanupFixture(t, "00000000-0000-4000-8000-000000000101")
	cleanupIntent := journal.LinuxCloneCleanupIntent.Clone()
	scratchPaths := createLinuxCloneTestScratch(t, fixture)
	rootCalls := 0
	deleter := linuxCloneTestRootDeleter(t, fixture, cleanupIntent, &rootCalls)
	if _, err := fixture.manager.CleanupLinuxClone(context.Background(), fixture.grant, fixture.checkout, journal); err == nil {
		t.Fatal("A cleanup proceeded before target-removal proof was stored")
	}
	proof, err := fixture.manager.RemoveLinuxCloneTarget(context.Background(), fixture.grant, fixture.checkout, journal, deleter)
	if err != nil {
		t.Fatalf("RemoveLinuxCloneTarget() error = %v", err)
	}
	if err := proof.Validate(); err != nil || !proof.Intent.Equal(cleanupIntent) ||
		proof.Disposition != authority.LinuxCloneTargetDispositionRemoved || !proof.TargetNlinkZero || rootCalls != 1 {
		t.Fatalf("cleanup proof = %#v, validation error=%v, root calls=%d", proof, err, rootCalls)
	}
	if _, err := os.Lstat(fixture.target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ROOT did not remove exact B target: %v", err)
	}
	if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
		t.Fatalf("ROOT target-removal phase touched A seed before State CAS: %v", err)
	}
	for _, scratchPath := range scratchPaths {
		if _, err := os.Lstat(scratchPath); err != nil {
			t.Fatalf("ROOT target-removal phase touched A scratch %q before State CAS: %v", scratchPath, err)
		}
	}
	baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyLinuxCloneRef(context.Background(), fixture.repository, baseRef, fixture.intent.BaseCommit); err != nil {
		t.Fatalf("ROOT target-removal phase touched A base ref before State CAS: %v", err)
	}
	if _, err := fixture.manager.CleanupLinuxClone(context.Background(), fixture.grant, fixture.checkout, journal); err == nil {
		t.Fatal("A cleanup proceeded after ROOT success but before target proof persistence")
	}
	if _, err := fixture.manager.RemoveLinuxCloneTarget(context.Background(), fixture.grant, fixture.checkout, journal, deleter); err == nil {
		t.Fatal("lost target-removal CAS was inferred from the now-absent B path")
	}
	if rootCalls != 1 {
		t.Fatalf("lost target-removal replay invoked ROOT for fresh ENOENT: calls=%d", rootCalls)
	}
	if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
		t.Fatalf("lost target-removal CAS removed the A seed: %v", err)
	}
	if err := verifyLinuxCloneRef(context.Background(), fixture.repository, baseRef, fixture.intent.BaseCommit); err != nil {
		t.Fatalf("lost target-removal CAS removed the A base ref: %v", err)
	}
	for _, scratchPath := range scratchPaths {
		if _, err := os.Stat(scratchPath); err != nil {
			t.Fatalf("lost target-removal CAS removed A scratch %q: %v", scratchPath, err)
		}
	}
	journalWithProof := journal
	storedProof := proof.Clone()
	journalWithProof.LinuxCloneTargetRemovalProof = &storedProof
	replayedRemoval, err := fixture.manager.RemoveLinuxCloneTarget(context.Background(), fixture.grant, fixture.checkout, journalWithProof, nil)
	if err != nil || !proof.Equal(replayedRemoval) || rootCalls != 1 {
		t.Fatalf("stored target-removal replay = %#v, %v, root calls=%d", replayedRemoval, err, rootCalls)
	}

	cleanupProof, err := fixture.manager.CleanupLinuxClone(context.Background(), fixture.grant, fixture.checkout, journalWithProof)
	if err != nil {
		t.Fatalf("CleanupLinuxClone() after durable target proof: %v", err)
	}
	if err := cleanupProof.Validate(); err != nil || !cleanupProof.TargetRemovalProof.Equal(proof) ||
		!cleanupProof.SeedAbsent || !cleanupProof.BaseRefAbsent || !cleanupProof.ScratchAbsent || !cleanupProof.PromotionRefPresent {
		t.Fatalf("A cleanup proof = %#v, validation error=%v", cleanupProof, err)
	}
	cleanupReplay, err := fixture.manager.CleanupLinuxClone(context.Background(), fixture.grant, fixture.checkout, journalWithProof)
	if err != nil || !cleanupProof.Equal(cleanupReplay) || rootCalls != 1 {
		t.Fatalf("A cleanup replay = %#v, %v, root calls=%d", cleanupReplay, err, rootCalls)
	}
	if _, err := os.Lstat(fixture.intent.SeedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace did not remove exact A seed: %v", err)
	}
	for _, scratchPath := range scratchPaths {
		if _, err := os.Lstat(scratchPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cleanup left exact A scratch %q: %v", scratchPath, err)
		}
	}
	if _, err := linuxCloneRefValue(context.Background(), fixture.repository, baseRef); !errors.Is(err, errLinuxCloneRefMissing) {
		t.Fatalf("cleanup base ref readback = %v, want absent", err)
	}
	if err := verifyLinuxClonePromotion(context.Background(), fixture.repository, fixture.intent, cleanupIntent.PromotionReceipt); err != nil {
		t.Fatalf("cleanup removed or changed the retained promotion ref: %v", err)
	}

	completed := journalWithProof
	completedProof := cleanupProof.Clone()
	completed.LinuxCloneCleanupPending = false
	completed.LinuxCloneCleanupProof = &completedProof
	completed.WorkspacePath = ""
	completed.WorkspaceRecoveryRequired = false
	replayed, err := fixture.manager.CleanupLinuxClone(context.Background(), fixture.grant, fixture.checkout, completed)
	if err != nil || !cleanupProof.Equal(replayed) || rootCalls != 1 {
		t.Fatalf("completed cleanup replay = %#v, %v, root calls=%d; want unchanged read-only proof", replayed, err, rootCalls)
	}
}

func TestLinuxCloneCleanupRootFailureRetainsAState(t *testing.T) {
	fixture, journal := newLinuxCloneCleanupFixture(t, "00000000-0000-4000-8000-000000000102")
	scratchPaths := createLinuxCloneTestScratch(t, fixture)
	want := errors.New("root helper wait is unresolved")
	if _, err := fixture.manager.RemoveLinuxCloneTarget(context.Background(), fixture.grant, fixture.checkout, journal,
		func(context.Context, int, int, authority.LinuxCloneGrant, authority.LinuxCloneCleanupIntent, *authority.LinuxCloneTargetRemovalProof) (authority.LinuxCloneTargetRemovalProof, error) {
			return authority.LinuxCloneTargetRemovalProof{}, want
		}); !errors.Is(err, want) {
		t.Fatalf("RemoveLinuxCloneTarget(root failure) error = %v, want %v", err, want)
	}
	if _, err := os.Stat(fixture.target); err != nil {
		t.Fatalf("root failure changed B target: %v", err)
	}
	if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
		t.Fatalf("root failure removed A seed: %v", err)
	}
	baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyLinuxCloneRef(context.Background(), fixture.repository, baseRef, fixture.intent.BaseCommit); err != nil {
		t.Fatalf("root failure removed A base ref: %v", err)
	}
	for _, scratchPath := range scratchPaths {
		if _, err := os.Lstat(scratchPath); err != nil {
			t.Fatalf("root failure removed A scratch %q: %v", scratchPath, err)
		}
	}
}

func TestLinuxCloneCleanupRejectsInvalidRootRemovalReceipt(t *testing.T) {
	fixture, journal := newLinuxCloneCleanupFixture(t, "00000000-0000-4000-8000-000000000104")
	cleanupIntent := journal.LinuxCloneCleanupIntent.Clone()
	rootCalls := 0
	validRoot := linuxCloneTestRootDeleter(t, fixture, cleanupIntent, &rootCalls)
	invalidNlinkProof := func(ctx context.Context, rootFD, targetFD int, grant authority.LinuxCloneGrant, cleanup authority.LinuxCloneCleanupIntent, prior *authority.LinuxCloneTargetRemovalProof) (authority.LinuxCloneTargetRemovalProof, error) {
		proof, err := validRoot(ctx, rootFD, targetFD, grant, cleanup, prior)
		if err == nil {
			proof.TargetNlinkZero = false
		}
		return proof, err
	}
	if _, err := fixture.manager.RemoveLinuxCloneTarget(context.Background(), fixture.grant, fixture.checkout, journal, invalidNlinkProof); err == nil {
		t.Fatal("workspace accepted ROOT success without original target nlink=0 proof")
	}
	if _, err := os.Lstat(fixture.target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ROOT test effect did not remove target before invalid proof: %v", err)
	}
	if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
		t.Fatalf("invalid root proof removed A seed: %v", err)
	}
	baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyLinuxCloneRef(context.Background(), fixture.repository, baseRef, fixture.intent.BaseCommit); err != nil {
		t.Fatalf("invalid root proof removed A base ref: %v", err)
	}
	if rootCalls != 1 {
		t.Fatalf("invalid nlink proof ROOT calls=%d, want one", rootCalls)
	}
	if _, err := fixture.manager.RemoveLinuxCloneTarget(context.Background(), fixture.grant, fixture.checkout, journal, invalidNlinkProof); err == nil {
		t.Fatal("unrecorded root effect became success on fresh ENOENT replay")
	}
	if rootCalls != 1 {
		t.Fatalf("fresh ENOENT replay retried ROOT without a stored proof, calls=%d", rootCalls)
	}
}

func TestLinuxCloneTargetRenameDoesNotBecomeRemovalProof(t *testing.T) {
	fixture, journal := newLinuxCloneCleanupFixture(t, "00000000-0000-4000-8000-000000000106")
	cleanupIntent := journal.LinuxCloneCleanupIntent.Clone()
	displaced := fixture.target + "-renamed-by-B"
	_, err := fixture.manager.RemoveLinuxCloneTarget(context.Background(), fixture.grant, fixture.checkout, journal,
		func(_ context.Context, rootFD, targetFD int, grant authority.LinuxCloneGrant, cleanup authority.LinuxCloneCleanupIntent, prior *authority.LinuxCloneTargetRemovalProof) (authority.LinuxCloneTargetRemovalProof, error) {
			if prior != nil || targetFD < 0 || !grant.Equal(fixture.grant) || !cleanup.Equal(cleanupIntent) {
				return authority.LinuxCloneTargetRemovalProof{}, errors.New("unexpected root request for rename scenario")
			}
			var rootInfo, targetInfo unix.Stat_t
			if err := unix.Fstat(rootFD, &rootInfo); err != nil {
				return authority.LinuxCloneTargetRemovalProof{}, err
			}
			if err := unix.Fstat(targetFD, &targetInfo); err != nil {
				return authority.LinuxCloneTargetRemovalProof{}, err
			}
			if err := os.Rename(fixture.target, displaced); err != nil {
				return authority.LinuxCloneTargetRemovalProof{}, err
			}
			worker := authority.LinuxCgroupProcessIdentity{PID: 9010, Identity: "root-cleanup-worker:9010:start"}
			witness := authority.LinuxCgroupProcessIdentity{
				PID: cleanup.NormalReleaseProof.AnchorPID, Identity: cleanup.NormalReleaseProof.AnchorIdentity,
			}
			return authority.LinuxCloneTargetRemovalProof{
				Version: authority.LinuxCloneVersion, Intent: cleanup.Clone(),
				RootDevice: uint64(rootInfo.Dev), RootInode: uint64(rootInfo.Ino),
				TargetDevice: uint64(targetInfo.Dev), TargetInode: uint64(targetInfo.Ino),
				Disposition:        authority.LinuxCloneTargetDispositionRemoved,
				PathRewalkVerified: true, BOwnedMountConfined: true, TargetNlinkZero: true,
				RootDeleterUID: 0, RootDeleter: worker,
				RootDeleterExit: authority.LinuxCgroupExitProof{
					Version: authority.LinuxCgroupIdentityVersion, Process: worker, Witness: witness, Waited: true, Reaped: true,
				},
			}, nil
		})
	if err == nil {
		t.Fatal("B rename was accepted as destruction of the exact checkout inode")
	}
	if _, err := os.Lstat(fixture.target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rename simulation left the original target name present: %v", err)
	}
	info, err := os.Stat(displaced)
	if err != nil {
		t.Fatalf("renamed original checkout was lost: %v", err)
	}
	identity := info.Sys().(*syscall.Stat_t)
	if uint64(identity.Dev) != fixture.checkout.TargetDevice || uint64(identity.Ino) != fixture.checkout.TargetInode {
		t.Fatalf("renamed sibling identity = %d:%d, want original %d:%d", identity.Dev, identity.Ino, fixture.checkout.TargetDevice, fixture.checkout.TargetInode)
	}
	if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
		t.Fatalf("rename did not retain A seed: %v", err)
	}
	baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyLinuxCloneRef(context.Background(), fixture.repository, baseRef, fixture.intent.BaseCommit); err != nil {
		t.Fatalf("rename did not retain A base ref: %v", err)
	}
}

func TestLinuxCloneCleanupResumesAfterPartialAEffects(t *testing.T) {
	fixture, journal := newLinuxCloneCleanupFixture(t, "00000000-0000-4000-8000-000000000107")
	createLinuxCloneTestScratch(t, fixture)
	cleanupIntent := journal.LinuxCloneCleanupIntent.Clone()
	removalProof, err := fixture.manager.RemoveLinuxCloneTarget(context.Background(), fixture.grant, fixture.checkout, journal,
		linuxCloneTestRootDeleter(t, fixture, cleanupIntent, new(int)))
	if err != nil {
		t.Fatalf("remove exact B target: %v", err)
	}
	journal.LinuxCloneTargetRemovalProof = &removalProof
	parentFD, seedName, err := openLinuxCloneSeedParent(fixture.seedRoot, fixture.intent.SeedPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeLinuxCloneCleanupSeed(context.Background(), fixture.repository, parentFD, seedName, cleanupIntent); err != nil {
		unix.Close(parentFD)
		t.Fatalf("simulate crash after exact A seed removal: %v", err)
	}
	if err := removeLinuxCloneBaseRefExact(context.Background(), fixture.repository, cleanupIntent.BaseRef, fixture.intent.BaseCommit); err != nil {
		unix.Close(parentFD)
		t.Fatalf("simulate crash after exact A base-ref removal: %v", err)
	}
	seedTemp, _, _, err := linuxCloneScratchNames(fixture.intent.Nonce)
	if err != nil {
		unix.Close(parentFD)
		t.Fatal(err)
	}
	if err := removeLinuxCloneSeedTempChecked(parentFD, seedTemp, nil); err != nil {
		unix.Close(parentFD)
		t.Fatalf("simulate crash after exact A seed-temp removal: %v", err)
	}
	if err := unix.Close(parentFD); err != nil {
		t.Fatal(err)
	}
	proof, err := fixture.manager.CleanupLinuxClone(context.Background(), fixture.grant, fixture.checkout, journal)
	if err != nil {
		t.Fatalf("resume A cleanup after exact earlier effects: %v", err)
	}
	if err := proof.Validate(); err != nil || !proof.TargetRemovalProof.Equal(removalProof) ||
		!proof.SeedAbsent || !proof.BaseRefAbsent || !proof.ScratchAbsent || !proof.PromotionRefPresent {
		t.Fatalf("resumed cleanup proof = %#v, validation error=%v", proof, err)
	}
	if err := verifyLinuxClonePromotion(context.Background(), fixture.repository, fixture.intent, cleanupIntent.PromotionReceipt); err != nil {
		t.Fatalf("resumed cleanup failed to preserve the promotion ref: %v", err)
	}
}

func TestLinuxCloneCleanupDoesNotInferRemovalFromFreshAbsence(t *testing.T) {
	fixture, journal := newLinuxCloneCleanupFixture(t, "00000000-0000-4000-8000-000000000103")
	scratchPaths := createLinuxCloneTestScratch(t, fixture)
	if err := os.Remove(fixture.target); err != nil {
		t.Fatal(err)
	}
	rootCalls := 0
	if _, err := fixture.manager.RemoveLinuxCloneTarget(context.Background(), fixture.grant, fixture.checkout, journal,
		func(context.Context, int, int, authority.LinuxCloneGrant, authority.LinuxCloneCleanupIntent, *authority.LinuxCloneTargetRemovalProof) (authority.LinuxCloneTargetRemovalProof, error) {
			rootCalls++
			return authority.LinuxCloneTargetRemovalProof{}, nil
		}); err == nil {
		t.Fatal("fresh target absence was accepted without a Store-persisted Removed proof")
	}
	if rootCalls != 0 {
		t.Fatalf("ROOT was called with a fresh absent path, calls=%d", rootCalls)
	}
	if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
		t.Fatalf("fresh absent path removed A seed without a stored proof: %v", err)
	}
	baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyLinuxCloneRef(context.Background(), fixture.repository, baseRef, fixture.intent.BaseCommit); err != nil {
		t.Fatalf("fresh absent path removed A base ref without a stored proof: %v", err)
	}
	for _, scratchPath := range scratchPaths {
		if _, err := os.Lstat(scratchPath); err != nil {
			t.Fatalf("fresh absent path removed A scratch %q without a stored proof: %v", scratchPath, err)
		}
	}
}

func TestLinuxCloneCleanupRetainsAStateForReplacedOrSymlinkTarget(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, *linuxCloneWorkspaceFixture)
		install func(*testing.T, *linuxCloneWorkspaceFixture)
	}{
		{
			name: "reappeared replacement inode",
			prepare: func(t *testing.T, fixture *linuxCloneWorkspaceFixture) {
				if err := os.Mkdir(fixture.target+"-replacement", 0o700); err != nil {
					t.Fatal(err)
				}
			},
			install: func(t *testing.T, fixture *linuxCloneWorkspaceFixture) {
				if err := os.Rename(fixture.target+"-replacement", fixture.target); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "reappeared bind mount",
			install: func(t *testing.T, fixture *linuxCloneWorkspaceFixture) {
				if err := os.Mkdir(fixture.target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mount("/proc", fixture.target, "", unix.MS_BIND, ""); err != nil {
					t.Skipf("bind mounts unavailable for target replacement test: %v", err)
				}
				t.Cleanup(func() { _ = unix.Unmount(fixture.target, 0) })
			},
		},
		{
			name: "symlink",
			install: func(t *testing.T, fixture *linuxCloneWorkspaceFixture) {
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.Mkdir(outside, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, fixture.target); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, journal := newLinuxCloneCleanupFixture(t, "00000000-0000-4000-8000-000000000105")
			cleanupIntent := journal.LinuxCloneCleanupIntent.Clone()
			scratchPaths := createLinuxCloneTestScratch(t, fixture)
			if test.prepare != nil {
				test.prepare(t, fixture)
			}
			rootCalls := 0
			removalProof, err := fixture.manager.RemoveLinuxCloneTarget(context.Background(), fixture.grant, fixture.checkout,
				journal, linuxCloneTestRootDeleter(t, fixture, cleanupIntent, &rootCalls))
			if err != nil {
				t.Fatalf("record exact ROOT removal before replacement scenario: %v", err)
			}
			if rootCalls != 1 {
				t.Fatalf("root removal calls = %d, want one", rootCalls)
			}
			journal.LinuxCloneTargetRemovalProof = &removalProof
			test.install(t, fixture)
			if _, err := fixture.manager.CleanupLinuxClone(context.Background(), fixture.grant, fixture.checkout, journal); err == nil {
				t.Fatal("cleanup accepted a changed B target path after the exact removal proof")
			}
			if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
				t.Fatalf("changed B path removed the A seed: %v", err)
			}
			baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyLinuxCloneRef(context.Background(), fixture.repository, baseRef, fixture.intent.BaseCommit); err != nil {
				t.Fatalf("changed B path removed the A base ref: %v", err)
			}
			for _, scratchPath := range scratchPaths {
				if _, err := os.Lstat(scratchPath); err != nil {
					t.Fatalf("changed B path removed A scratch %q: %v", scratchPath, err)
				}
			}
		})
	}
}

func newLinuxCloneCleanupFixture(t *testing.T, runID string) (*linuxCloneWorkspaceFixture, state.RunJournal) {
	t.Helper()
	fixture := newLinuxCloneWorkspaceFixtureWithRunID(t, runID)
	if err := os.MkdirAll(filepath.Dir(fixture.target), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(fixture.target, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.checkout = linuxCloneCheckoutReceipt(t, fixture.intent, fixture.grant, fixture.target)
	candidate := authority.LinuxClonePromotionIntent{
		Version: authority.LinuxCloneVersion, ResourceID: fixture.intent.ResourceID,
		Commit: fixture.intent.BaseCommit, TreeDigest: fixture.intent.BaseTreeDigest,
	}
	promotion, err := promoteLinuxCloneRef(context.Background(), fixture.repository, fixture.intent, candidate.Commit, candidate.TreeDigest)
	if err != nil {
		t.Fatalf("pin cleanup fixture promotion: %v", err)
	}
	release := linuxCloneNormalReleaseProof(t)
	terminalAt := time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC)
	journal := state.RunJournal{
		RunID: fixture.intent.RunID, Generation: fixture.intent.Generation,
		RuntimeKey: "local", RuntimeID: "cleanup-runtime", ClaimedRuntimeEpoch: 1,
		ClaimID: "cleanup-claim", LeaseToken: "cleanup-lease", LeaseExpiresAt: terminalAt.Add(time.Hour),
		LocalState:        "cleanup_pending",
		TerminalPendingAt: terminalAt.Add(-time.Minute), TerminalState: "completed",
		TerminalTaskResultKind: protocol.TaskResultCandidateCompletion,
		TerminalVerdict:        state.TerminalVerdictAccepted, TerminalResolvedAt: terminalAt,
		Work: protocol.Work{
			Goal: "implement clone candidate", AgentProfile: "test", Workspace: fixture.intent.BindingKey,
			Input: linuxCloneCleanupAdmissionInput(t, protocol.Subject{
				ResourceID: fixture.intent.ResourceID, Commit: fixture.intent.BaseCommit, TreeDigest: fixture.intent.BaseTreeDigest,
			}),
		},
		WorkspacePath: fixture.intent.Target, WorkspaceRecoveryRequired: true, WorkspaceBindingKey: fixture.intent.BindingKey,
		LinuxCloneIntent: &fixture.intent, LinuxCloneSeedReceipt: &fixture.grant.Seed,
		LinuxCloneCheckoutReceipt: &fixture.checkout, LinuxClonePromotionIntent: &candidate,
		LinuxClonePromotionReceipt: &promotion, ContainmentCgroupReleaseProof: &release,
		GoalDeliveryEnabled: true,
	}
	evidence := linuxCloneCleanupArtifactEvidence(t, fixture.intent.RunID, protocol.Subject{
		ResourceID: candidate.ResourceID, Commit: candidate.Commit, TreeDigest: candidate.TreeDigest,
	})
	journal.DeliveredGoalDeliveries = persistLinuxCloneCleanupArtifactDelivery(t, journal, evidence)
	journal.GoalDeliveryEnabled = true
	cleanupIntent, err := state.LinuxCloneCleanupIntentForJournal(journal)
	if err != nil {
		t.Fatalf("derive State cleanup intent: %v", err)
	}
	journal.LinuxCloneCleanupPending = true
	cleanupCopy := cleanupIntent.Clone()
	journal.LinuxCloneCleanupIntent = &cleanupCopy
	if !journal.LinuxCloneCleanupBeginEligible(journal.Key(), journal.Fence()) {
		t.Fatal("cleanup fixture does not satisfy the durable Begin predicate")
	}
	return fixture, journal
}

func persistLinuxCloneCleanupArtifactDelivery(t *testing.T, journal state.RunJournal, evidence protocol.Evidence) []state.GoalDelivery {
	t.Helper()
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := state.RunJournal{
		RunID: journal.RunID, Generation: journal.Generation,
		RuntimeKey: journal.RuntimeKey, RuntimeID: journal.RuntimeID,
		ClaimedRuntimeEpoch: journal.ClaimedRuntimeEpoch, ClaimID: journal.ClaimID,
		LeaseToken: journal.LeaseToken, LeaseExpiresAt: journal.LeaseExpiresAt,
		LocalState: "running", Work: journal.Work,
		WorkspacePath: journal.WorkspacePath, WorkspaceBindingKey: journal.WorkspaceBindingKey,
	}
	if err := store.SaveJournal(base); err != nil {
		t.Fatalf("save artifact-delivery RunJournal: %v", err)
	}
	queued, err := store.QueueGoalEvidence(base.Key(), evidence)
	if err != nil || len(queued.PendingGoalDeliveries) != 1 {
		t.Fatalf("queue actual artifact evidence delivery: pending=%d error=%v", len(queued.PendingGoalDeliveries), err)
	}
	delivery := queued.PendingGoalDeliveries[0]
	delivered, err := store.MarkGoalDeliveryDelivered(base.Key(), state.GoalDeliveryEvidence, delivery.DeliveryID, delivery.PayloadDigest)
	if err != nil || len(delivered.DeliveredGoalDeliveries) != 1 {
		t.Fatalf("record actual artifact delivery receipt: delivered=%d error=%v", len(delivered.DeliveredGoalDeliveries), err)
	}
	return delivered.DeliveredGoalDeliveries
}

func linuxCloneCleanupAdmissionInput(t *testing.T, subject protocol.Subject) json.RawMessage {
	t.Helper()
	workItemID := "00000000-0000-4000-8000-000000000112"
	admission := protocol.Admission{
		SchemaVersion: protocol.AdmissionSchemaVersion,
		AdmissionID:   "00000000-0000-4000-8000-000000000113", GoalID: "00000000-0000-4000-8000-000000000114",
		GoalRevision: 1, WorkItemID: &workItemID, Purpose: protocol.AdmissionPurposeImplement,
		ContextSnapshotID: "00000000-0000-4000-8000-000000000115", ContextHash: "sha256:" + strings.Repeat("a", 64),
		ModelProfile: "default", SessionMode: protocol.SessionModeFresh, Subject: subject,
		Limits: protocol.AdmissionLimits{MaxTurns: 1, DeadlineAt: "2026-10-01T00:00:00Z"},
	}
	encoded, err := json.Marshal(admission)
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := protocol.ParseAdmissionInput(encoded); err != nil || !present {
		t.Fatalf("cleanup admission input is invalid: present=%t error=%v", present, err)
	}
	return encoded
}

func linuxCloneCleanupArtifactEvidence(t *testing.T, runID string, subject protocol.Subject) protocol.Evidence {
	t.Helper()
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	resourceID, commit, path := subject.ResourceID, subject.Commit, "result.txt"
	predicateID, profile := "artifact-publish", "artifact-publisher"
	contentDigest := "sha256:" + strings.Repeat("b", 64)
	evidence := protocol.Evidence{
		SchemaVersion: protocol.EvidenceSchemaVersion,
		EvidenceID:    "00000000-0000-4000-8000-000000000116", RunID: runID,
		EvidenceKey: "candidate-artifact", Kind: protocol.EvidenceArtifact,
		Subject: subject, SubjectHash: subjectHash,
		SourceRef: protocol.SourceRef{
			Kind: protocol.EvidenceArtifact, Ref: "artifact-publisher", SubjectHash: &subjectHash,
			ResourceID: &resourceID, Commit: &commit, Path: &path,
		},
		SourceRevision: "artifact-publisher", ValidatorProfile: &profile,
		Verdict: protocol.EvidencePassed,
		Payload: protocol.Receipt{
			PredicateID: &predicateID, Subject: &subject, SubjectHash: &subjectHash,
			ResourceID: &resourceID, Commit: &commit, Path: &path, ContentDigest: &contentDigest,
		},
		ObservedAt: "2026-09-28T01:02:03Z",
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := protocol.ParseEvidence(encoded)
	if err != nil {
		t.Fatalf("cleanup artifact evidence is invalid: %v", err)
	}
	return parsed
}

func linuxCloneTestRootDeleter(t *testing.T, fixture *linuxCloneWorkspaceFixture, expected authority.LinuxCloneCleanupIntent, calls *int) LinuxCloneTargetDeleter {
	t.Helper()
	return func(ctx context.Context, rootFD, targetFD int, grant authority.LinuxCloneGrant, cleanup authority.LinuxCloneCleanupIntent, prior *authority.LinuxCloneTargetRemovalProof) (authority.LinuxCloneTargetRemovalProof, error) {
		*calls = *calls + 1
		if !grant.Equal(fixture.grant) || !cleanup.Equal(expected) {
			return authority.LinuxCloneTargetRemovalProof{}, errors.New("test root deleter received changed cleanup authority")
		}
		if targetFD < 0 || prior != nil {
			return authority.LinuxCloneTargetRemovalProof{}, errors.New("test ROOT fresh removal requires a live target FD and nil prior proof")
		}
		var rootInfo unix.Stat_t
		if err := unix.Fstat(rootFD, &rootInfo); err != nil {
			return authority.LinuxCloneTargetRemovalProof{}, err
		}
		var targetInfo unix.Stat_t
		if err := unix.Fstat(targetFD, &targetInfo); err != nil ||
			uint64(targetInfo.Dev) != cleanup.CheckoutReceipt.TargetDevice || uint64(targetInfo.Ino) != cleanup.CheckoutReceipt.TargetInode {
			return authority.LinuxCloneTargetRemovalProof{}, errors.New("test root target FD differs from checkout")
		}
		parentFD, name, err := openLinuxCloneTargetParent(cleanup.RootPath, grant.Intent.Target)
		if err != nil {
			return authority.LinuxCloneTargetRemovalProof{}, err
		}
		defer unix.Close(parentFD)
		var named unix.Stat_t
		if err := unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameLinuxCloneDirectoryIdentity(&targetInfo, &named) {
			return authority.LinuxCloneTargetRemovalProof{}, errors.New("test root path no longer names the pinned target")
		}
		if err := unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR); err != nil {
			return authority.LinuxCloneTargetRemovalProof{}, err
		}
		if err := unix.Fsync(parentFD); err != nil {
			return authority.LinuxCloneTargetRemovalProof{}, err
		}
		var afterRemoval unix.Stat_t
		if err := unix.Fstat(targetFD, &afterRemoval); err != nil || afterRemoval.Nlink != 0 {
			return authority.LinuxCloneTargetRemovalProof{}, errors.New("test ROOT target inode did not reach link count zero")
		}
		if err := verifyLinuxCloneTargetAbsentUnderRoot(ctx, cleanup.RootPath, grant.Intent.Target); err != nil {
			return authority.LinuxCloneTargetRemovalProof{}, err
		}
		worker := authority.LinuxCgroupProcessIdentity{PID: 9001, Identity: "root-cleanup-worker:9001:start"}
		witness := authority.LinuxCgroupProcessIdentity{
			PID: cleanup.NormalReleaseProof.AnchorPID, Identity: cleanup.NormalReleaseProof.AnchorIdentity,
		}
		return authority.LinuxCloneTargetRemovalProof{
			Version: authority.LinuxCloneVersion, Intent: cleanup.Clone(),
			RootDevice: uint64(rootInfo.Dev), RootInode: uint64(rootInfo.Ino),
			TargetDevice: cleanup.CheckoutReceipt.TargetDevice, TargetInode: cleanup.CheckoutReceipt.TargetInode,
			Disposition: authority.LinuxCloneTargetDispositionRemoved, PathRewalkVerified: true,
			BOwnedMountConfined: true, TargetNlinkZero: true,
			RootDeleterUID: 0, RootDeleter: worker,
			RootDeleterExit: authority.LinuxCgroupExitProof{
				Version: authority.LinuxCgroupIdentityVersion, Process: worker, Witness: witness, Waited: true, Reaped: true,
			},
		}, nil
	}
}

func createLinuxCloneTestScratch(t *testing.T, fixture *linuxCloneWorkspaceFixture) []string {
	t.Helper()
	seedTemp, quarantine, template, err := linuxCloneScratchNames(fixture.intent.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(fixture.intent.SeedPath)
	seedTempPath := filepath.Join(parent, seedTemp)
	quarantinePath := filepath.Join(parent, quarantine)
	templatePath := filepath.Join(parent, template)
	if err := os.WriteFile(seedTempPath, []byte("fenced partial temp"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(quarantinePath, 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(quarantinePath, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "object"), []byte("A-owned quarantine object"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(templatePath, 0o700); err != nil {
		t.Fatal(err)
	}
	return []string{seedTempPath, quarantinePath, templatePath}
}

type linuxCloneWorkspaceFixture struct {
	manager    *Manager
	repository string
	root       string
	seedRoot   string
	intent     authority.LinuxCloneIntent
	grant      authority.LinuxCloneGrant
	target     string
	checkout   authority.LinuxCloneCheckoutReceipt
	candidate  string
}

func newLinuxCloneWorkspaceFixture(t *testing.T) *linuxCloneWorkspaceFixture {
	return newLinuxCloneWorkspaceFixtureWithRunID(t, "clone-test-run")
}

func newLinuxCloneWorkspaceFixtureWithRunID(t *testing.T, runID string) *linuxCloneWorkspaceFixture {
	t.Helper()
	repository := newRepository(t)
	root := filepath.Join(t.TempDir(), "b-clones")
	seedRoot := filepath.Join(t.TempDir(), "a-seeds")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(seedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	treeDigest, err := gitTreeDigest(context.Background(), repository, commit)
	if err != nil {
		t.Fatal(err)
	}
	manager := New(map[string]config.Workspace{
		"native": {
			Policy: config.WorkspacePolicyGitCloneV2, Repository: repository, Root: root,
			SeedRoot: seedRoot, Cleanup: config.CleanupNever,
		},
	})
	intent, err := manager.PlanLinuxCloneIntent("native", RunRef{RunID: runID, Generation: 1}, protocol.Subject{
		ResourceID: "00000000-0000-4000-8000-000000000001", Commit: commit, TreeDigest: treeDigest,
	})
	if err != nil {
		t.Fatalf("PlanLinuxCloneIntent() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(intent.SeedPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PlanLinuxCloneIntent() created its A seed parent: %v", err)
	}
	if _, err := os.Lstat(intent.Target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PlanLinuxCloneIntent() created its B target: %v", err)
	}
	seed, err := manager.PrepareLinuxCloneSeed(context.Background(), intent)
	if err != nil {
		t.Fatalf("PrepareLinuxCloneSeed() error = %v", err)
	}
	grant := authority.LinuxCloneGrant{Intent: intent, Seed: seed}
	if _, err := manager.PrepareLinuxClone(context.Background(), grant); err != nil {
		t.Fatalf("PrepareLinuxClone() error = %v", err)
	}
	return &linuxCloneWorkspaceFixture{manager: manager, repository: repository, root: root, seedRoot: seedRoot, intent: intent, grant: grant, target: intent.Target}
}

func (fixture *linuxCloneWorkspaceFixture) createBCloneAndCommit(t *testing.T) {
	t.Helper()
	if fixture.checkout.TargetInode != 0 {
		t.Fatalf("fixture B clone already exists at %q", fixture.target)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.target), 0o750); err != nil {
		t.Fatal(err)
	}
	baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "clone", "--branch", strings.TrimPrefix(baseRef, "refs/heads/"), fixture.intent.SeedPath, fixture.target)
	if err := platform.ConfigureHeadlessProcess(command); err != nil {
		t.Fatal(err)
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("B git clone from A bundle: %v\n%s", err, output)
	}
	runGit(t, fixture.target, "config", "user.email", "b@example.test")
	runGit(t, fixture.target, "config", "user.name", "B Native Agent")
	if err := os.WriteFile(filepath.Join(fixture.target, "b-change.txt"), []byte("committed by B\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, fixture.target, "add", "b-change.txt")
	runGit(t, fixture.target, "commit", "-m", "B clone change")
	fixture.candidate = gitOutput(t, fixture.target, "rev-parse", "HEAD")
	fixture.checkout = linuxCloneCheckoutReceipt(t, fixture.intent, fixture.grant, fixture.target)
}

func (fixture *linuxCloneWorkspaceFixture) quarantine(t *testing.T) (string, error) {
	t.Helper()
	if fixture.checkout.TargetInode == 0 {
		fixture.createBCloneAndCommit(t)
	}
	target, err := openLinuxCloneTarget(fixture.intent, fixture.checkout, fixture.root)
	if err != nil {
		return "", err
	}
	defer target.close()
	quarantine, err := createLinuxCloneQuarantine(context.Background(), target, filepath.Dir(fixture.intent.SeedPath), fixture.intent, len(fixture.intent.BaseCommit))
	if err != nil {
		return "", err
	}
	_, expectedName, _, err := linuxCloneScratchNames(fixture.intent.Nonce)
	if err != nil {
		return "", err
	}
	if filepath.Base(quarantine) != expectedName {
		return "", errors.New("clone quarantine is not bound to the intent nonce")
	}
	return quarantine, nil
}

func (fixture *linuxCloneWorkspaceFixture) removeQuarantine(t *testing.T, quarantine string) {
	t.Helper()
	if filepath.Dir(quarantine) != filepath.Dir(fixture.intent.SeedPath) {
		t.Fatalf("test quarantine %q escaped the intent scratch parent", quarantine)
	}
	parentFD, _, err := openLinuxCloneSeedParent(fixture.seedRoot, fixture.intent.SeedPath, false)
	if err != nil {
		t.Fatalf("open exact test quarantine parent: %v", err)
	}
	defer unix.Close(parentFD)
	if err := removeLinuxClonePromotionScratch(context.Background(), parentFD, fixture.intent); err != nil {
		t.Fatalf("remove exact test quarantine: %v", err)
	}
}

func linuxCloneCheckoutReceipt(t *testing.T, intent authority.LinuxCloneIntent, grant authority.LinuxCloneGrant, target string) authority.LinuxCloneCheckoutReceipt {
	t.Helper()
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("test B target is not a real directory: %#v", info)
	}
	identity := info.Sys().(*syscall.Stat_t)
	return authority.LinuxCloneCheckoutReceipt{
		Version: authority.LinuxCloneVersion, RunID: intent.RunID, Generation: intent.Generation,
		BindingKey: intent.BindingKey, ResourceID: intent.ResourceID, Target: intent.Target, Nonce: intent.Nonce,
		SeedDigest: grant.Seed.Digest, BaseCommit: intent.BaseCommit,
		TargetDevice: uint64(identity.Dev), TargetInode: uint64(identity.Ino),
	}
}

func resolveLinuxCloneGitPath(repository, value string) string {
	if !filepath.IsAbs(value) {
		value = filepath.Join(repository, value)
	}
	resolved, _ := filepath.EvalSymlinks(value)
	return resolved
}

func linuxCloneNormalReleaseProof(t *testing.T) authority.SupervisorHandoffReleaseProof {
	t.Helper()
	parent := authority.LinuxCgroupParentIdentity{
		BootID: "00000000-0000-4000-8000-000000000001", MountID: 42, Device: 84, Inode: 126,
		Path: "/system.slice/symmetry.slice/delegated",
	}
	cgroup := authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: parent,
		LeafName: strings.Repeat("e", authority.TokenBytes*2), LeafDevice: 168, LeafInode: 210,
	}
	anchor := authority.LinuxCgroupProcessIdentity{PID: 10, Identity: "anchor:10:start"}
	helper := authority.LinuxCgroupProcessIdentity{PID: 20, Identity: "helper:20:start"}
	launcher := authority.LinuxCgroupProcessIdentity{PID: 30, Identity: "launcher:30:start"}
	gate := authority.LinuxCgroupProcessIdentity{PID: 40, Identity: "gate:40:start"}
	mirror := authority.LinuxCgroupProcessIdentity{PID: 50, Identity: "daemon:50:start"}
	launcherExit := &authority.LinuxCgroupExitProof{
		Version: authority.LinuxCgroupIdentityVersion, Process: launcher, Witness: helper, Waited: true, Reaped: true,
	}
	handoff := authority.SupervisorHandoff{
		Version: authority.SupervisorHandoffVersionV2, LaunchToken: strings.Repeat("a", authority.TokenBytes*2),
		Secret: strings.Repeat("b", authority.SecretBytes*2), OwnerKind: authority.OwnerKindLinuxHelper,
		OwnerContext: "/run/symmetry/recovery.sock", TargetPID: gate.PID, TargetIdentity: gate.Identity,
		OwnerUID: 1000, LauncherUID: 0, PipeToken: strings.Repeat("c", authority.TokenBytes*2),
		JobID: strings.Repeat("d", authority.TokenBytes*2), TargetBirthKind: authority.LinuxCgroupTargetBirthTrustedAGate,
		CgroupIdentity: &cgroup, AnchorPID: anchor.PID, AnchorIdentity: anchor.Identity,
		LauncherPID: launcher.PID, LauncherIdentity: launcher.Identity, LauncherExit: launcherExit,
		SupervisorPID: helper.PID, SupervisorIdentity: helper.Identity, Mirror: &mirror,
	}
	value, err := handoff.ToSupervisor()
	if err != nil {
		t.Fatalf("construct release authority: %v", err)
	}
	stopProof := authority.LinuxCgroupStopProof{
		Version: authority.LinuxCgroupIdentityVersion, Kind: authority.LinuxCgroupStopProofHelper,
		Issuer: helper, Reaper: helper, PinnedLeafFD: true, CgroupKillAcknowledged: true,
		CgroupPopulatedZero: true, NoMigrationObserved: true, ProducerFenced: true, DescendantsReaped: true,
		LauncherExit: *launcherExit,
		GateExit:     authority.LinuxCgroupExitProof{Version: authority.LinuxCgroupIdentityVersion, Process: gate, Witness: helper, Waited: true, Reaped: true},
	}
	receipt := authority.StopReceipt{
		Version: authority.SupervisorVersionV2, Status: "stopped", OwnerKind: handoff.OwnerKind, OwnerContext: handoff.OwnerContext,
		TargetPID: handoff.TargetPID, TargetIdentity: handoff.TargetIdentity, OwnerUID: handoff.OwnerUID, LauncherUID: handoff.LauncherUID,
		PipeToken: handoff.PipeToken, JobID: handoff.JobID, SupervisorPID: handoff.SupervisorPID,
		SupervisorIdentity: handoff.SupervisorIdentity, ActiveProcesses: 0, TargetBirthKind: handoff.TargetBirthKind,
		CgroupIdentity: &cgroup, LinuxProof: &stopProof,
	}
	if !receipt.ValidForHandoff(handoff) {
		t.Fatal("normal-release stop receipt fixture is invalid")
	}
	handoff.StopReceipt = &receipt
	value, err = handoff.ToSupervisor()
	if err != nil || value.Validate() != nil {
		t.Fatalf("bind stop receipt to release authority: value=%#v error=%v", value, err)
	}
	release := authority.SupervisorHandoffReleaseProof{
		Version: authority.SupervisorHandoffVersionV2, Status: "released", LaunchToken: handoff.LaunchToken,
		OwnerKind: handoff.OwnerKind, OwnerContext: handoff.OwnerContext, TargetPID: handoff.TargetPID,
		TargetIdentity: handoff.TargetIdentity, OwnerUID: handoff.OwnerUID, LauncherUID: handoff.LauncherUID,
		PipeToken: handoff.PipeToken, JobID: handoff.JobID, SupervisorPID: handoff.SupervisorPID,
		SupervisorIdentity: handoff.SupervisorIdentity, TargetBirthKind: handoff.TargetBirthKind,
		CgroupIdentity: &cgroup, AnchorPID: handoff.AnchorPID, AnchorIdentity: handoff.AnchorIdentity,
		LauncherPID: handoff.LauncherPID, LauncherIdentity: handoff.LauncherIdentity,
		LauncherExit: launcherExit, Mirror: &mirror, LeafDisposition: authority.LinuxCgroupLeafRemoved,
		StopReceipt: &receipt,
	}
	if err := release.Validate(); err != nil || !release.ValidForSupervisor(value) {
		t.Fatalf("normal-release proof fixture is invalid: proof=%#v authority=%#v error=%v", release, value, err)
	}
	return release
}
