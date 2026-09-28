//go:build linux && amd64

package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
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

func TestLinuxCloneAbortAfterNormalReleaseRequiresAbsentTarget(t *testing.T) {
	// Workspace validates and echoes this DTO; App/Store bind it to the exact
	// RunKey/fence before calling the no-target abort API.
	release := linuxCloneNormalReleaseProof(t)
	t.Run("target present retains seed", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		if err := os.MkdirAll(filepath.Dir(fixture.target), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(fixture.target, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.AbortLinuxCloneBeforeCheckout(context.Background(), fixture.intent, &fixture.grant.Seed, nil, nil, &release); err == nil {
			t.Fatal("normal stop receipt inferred no B checkout while Target existed")
		}
		if _, err := os.Stat(fixture.intent.SeedPath); err != nil {
			t.Fatalf("normal-release abort removed the seed before target absence: %v", err)
		}
	})

	t.Run("target absent echoes stop proof", func(t *testing.T) {
		fixture := newLinuxCloneWorkspaceFixture(t)
		proof, err := fixture.manager.AbortLinuxCloneBeforeCheckout(context.Background(), fixture.intent, &fixture.grant.Seed, nil, nil, &release)
		if err != nil {
			t.Fatalf("AbortLinuxCloneBeforeCheckout(normal release) error = %v", err)
		}
		if err := proof.Validate(); err != nil || proof.NormalReleaseProof == nil || !proof.NormalReleaseProof.Equal(release) ||
			proof.PreGateReleaseProof != nil || proof.CgroupAbortProof != nil || !proof.TargetAbsent || !proof.SeedAbsent {
			t.Fatalf("normal-release abort proof = %#v, validation error=%v", proof, err)
		}
		if _, err := os.Lstat(fixture.intent.SeedPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("normal-release abort left A seed: %v", err)
		}
		baseRef, err := authority.LinuxCloneBaseRef(fixture.intent.RunID, fixture.intent.Generation)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := linuxCloneRefValue(context.Background(), fixture.repository, baseRef); !errors.Is(err, errLinuxCloneRefMissing) {
			t.Fatalf("normal-release abort base ref readback = %v, want absent", err)
		}
	})
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
		defer os.RemoveAll(quarantine)
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
	quarantine, err := createLinuxCloneQuarantine(context.Background(), target, filepath.Dir(fixture.intent.SeedPath), len(fixture.intent.BaseCommit))
	if err != nil {
		t.Fatalf("copy B object bytes: %v", err)
	}
	defer os.RemoveAll(quarantine)
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
	if _, err := fixture.manager.PromoteLinuxClone(context.Background(), fixture.grant, fixture.checkout, fixture.candidate, platform.LinuxCgroupV2Release{}); err == nil {
		t.Fatal("PromoteLinuxClone accepted an unissued release capability")
	}
}

func TestLinuxCloneFailedCleanupRetainsHostileTarget(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can bypass directory permission denial")
	}
	fixture := newLinuxCloneWorkspaceFixture(t)
	if err := os.MkdirAll(filepath.Dir(fixture.target), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(fixture.target, 0o700); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(fixture.target, "a-blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(blocked, "artifact.txt")
	if err := os.WriteFile(keep, []byte("retain on unresolved cleanup"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkout := linuxCloneCheckoutReceipt(t, fixture.intent, fixture.grant, fixture.target)
	target, err := openLinuxCloneTarget(fixture.intent, checkout, fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		target.close()
		t.Fatal(err)
	}
	err = removeLinuxCloneTarget(context.Background(), target)
	_ = target.close()
	if err == nil {
		t.Fatal("cleanup removed an inaccessible hostile B entry")
	}
	if _, err := os.Stat(fixture.target); err != nil {
		t.Fatalf("failed cleanup removed the clone root: %v", err)
	}
	if err := os.Chmod(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(keep); err != nil || string(content) != "retain on unresolved cleanup" {
		t.Fatalf("failed cleanup lost retained artifact: content=%q error=%v", content, err)
	}
	if err := fixture.manager.CleanupLinuxClone(context.Background(), fixture.grant, checkout, state.RunJournal{}); err == nil {
		t.Fatal("CleanupLinuxClone accepted an absent Store readback")
	}
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
	intent, err := manager.PlanLinuxCloneIntent("native", RunRef{RunID: "clone-test-run", Generation: 1}, protocol.Subject{
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
	quarantine, err := createLinuxCloneQuarantine(context.Background(), target, filepath.Dir(fixture.intent.SeedPath), len(fixture.intent.BaseCommit))
	if err != nil {
		return "", err
	}
	return quarantine, nil
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
