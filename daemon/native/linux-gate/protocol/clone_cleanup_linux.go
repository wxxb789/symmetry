//go:build linux && amd64

package protocol

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

const (
	cloneCleanupOperation       = "-clone-cleanup"
	cloneCleanupWireVersion     = 1
	cloneCleanupWireKindRequest = "cleanup-request"
	cloneCleanupWireKindResult  = "cleanup-result"
	cloneCleanupMaxFrame        = 64 << 10
	cloneCleanupReadyTimeout    = 10 * time.Second
	cloneCleanupWorkTimeout     = 5 * time.Minute
	cloneCleanupReapTimeout     = 5 * time.Second
)

var ErrCloneCleanupOutcomeUnknown = errors.New("Linux clone target cleanup outcome is unknown; retain CleanupPending and do not perform A-side cleanup")

// CloneCleanupWireRequest is private local IPC for the installed root helper.
// Its exported spelling is only for the separate launcher-core package; this
// request is never accepted from a network or B-owned socket.
type CloneCleanupWireRequest struct {
	Version           int                                     `json:"version"`
	Kind              string                                  `json:"kind"`
	Nonce             string                                  `json:"nonce"`
	Grant             authority.LinuxCloneGrant               `json:"grant"`
	Cleanup           authority.LinuxCloneCleanupIntent       `json:"cleanup"`
	PriorRemovalProof *authority.LinuxCloneTargetRemovalProof `json:"prior_removal_proof,omitempty"`
}

// CloneCleanupWireResult is the root worker's bounded observation. The A
// client adds RootDeleterExit only after exact pidfd wait/reap and an
// independent root-relative absence readback.
type CloneCleanupWireResult struct {
	Version            int                               `json:"version"`
	Kind               string                            `json:"kind"`
	Nonce              string                            `json:"nonce"`
	Cleanup            authority.LinuxCloneCleanupIntent `json:"cleanup"`
	RootDevice         uint64                            `json:"root_device"`
	RootInode          uint64                            `json:"root_inode"`
	TargetDevice       uint64                            `json:"target_device"`
	TargetInode        uint64                            `json:"target_inode"`
	Disposition        string                            `json:"disposition"`
	PathRewalkVerified bool                              `json:"path_rewalk_verified"`
	// BOwnedMountConfined summarizes per-entry B-owner/device and nofollow/no-XDEV checks;
	// it is not an atomic no-sibling guarantee against concurrent same-UID B renames.
	BOwnedMountConfined bool `json:"b_owned_mount_confined"`
	// TargetNlinkZero is true only after the fresh target FD observed the
	// original checkout unlinked after parent-directory fsync.
	TargetNlinkZero bool                                 `json:"target_nlink_zero"`
	RootDeleterUID  uint32                               `json:"root_deleter_uid"`
	RootDeleter     authority.LinuxCgroupProcessIdentity `json:"root_deleter"`
}

// EncodeCloneCleanupRequest encodes one fixed, bounded request for the root
// cleanup core. Grant and cleanup are duplicated intentionally so the core
// can reject any A-side mismatch before acting; neither carries a raw lease
// token or any B-supplied path/argv.
func EncodeCloneCleanupRequest(nonce string, grant authority.LinuxCloneGrant, cleanup authority.LinuxCloneCleanupIntent,
	priorRemovalProof *authority.LinuxCloneTargetRemovalProof) ([]byte, error) {
	if !canonicalLowerHex(nonce, 32) || grant.Validate() != nil || cleanup.Validate() != nil ||
		!grant.Intent.Equal(cleanup.Intent) || !grant.Seed.Equal(cleanup.SeedReceipt) ||
		priorRemovalProof != nil && validatePriorRemovalProofIntent(*priorRemovalProof, cleanup) != nil {
		return nil, errors.New("clone cleanup request identity is invalid")
	}
	var priorCopy *authority.LinuxCloneTargetRemovalProof
	if priorRemovalProof != nil {
		cloned := priorRemovalProof.Clone()
		priorCopy = &cloned
	}
	packet, err := json.Marshal(CloneCleanupWireRequest{
		Version: cloneCleanupWireVersion, Kind: cloneCleanupWireKindRequest,
		Nonce: nonce, Grant: grant.Clone(), Cleanup: cleanup.Clone(), PriorRemovalProof: priorCopy,
	})
	if err != nil {
		return nil, fmt.Errorf("encode fixed clone cleanup request: %w", err)
	}
	if len(packet) == 0 || len(packet) > cloneCleanupMaxFrame {
		clear(packet)
		return nil, errors.New("clone cleanup request exceeds its fixed bound")
	}
	return packet, nil
}

// DecodeCloneCleanupRequest accepts exactly one strict bounded local request.
func DecodeCloneCleanupRequest(packet []byte) (CloneCleanupWireRequest, error) {
	var request CloneCleanupWireRequest
	if len(packet) == 0 || len(packet) > cloneCleanupMaxFrame {
		return request, errors.New("clone cleanup request has invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(packet))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return CloneCleanupWireRequest{}, fmt.Errorf("decode fixed clone cleanup request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return CloneCleanupWireRequest{}, errors.New("clone cleanup request has trailing JSON data")
	}
	canonical, err := json.Marshal(request)
	if err != nil || !bytes.Equal(packet, canonical) {
		clear(canonical)
		return CloneCleanupWireRequest{}, errors.New("clone cleanup request is not the canonical fixed encoding")
	}
	clear(canonical)
	if request.Version != cloneCleanupWireVersion || request.Kind != cloneCleanupWireKindRequest {
		return CloneCleanupWireRequest{}, errors.New("clone cleanup request version, shape, or authority is invalid")
	}
	validated, err := EncodeCloneCleanupRequest(request.Nonce, request.Grant, request.Cleanup, request.PriorRemovalProof)
	clear(validated)
	if err != nil {
		return CloneCleanupWireRequest{}, fmt.Errorf("validate decoded clone cleanup request: %w", err)
	}
	return request, nil
}

// EncodeCloneCleanupResult encodes the root worker's result before A constructs
// its separate exact wait/reap proof.
func EncodeCloneCleanupResult(result CloneCleanupWireResult) ([]byte, error) {
	if result.Version != cloneCleanupWireVersion || result.Kind != cloneCleanupWireKindResult ||
		!canonicalLowerHex(result.Nonce, 32) || result.Cleanup.Validate() != nil ||
		result.RootInode == 0 || result.RootDevice != result.TargetDevice || result.TargetDevice != result.Cleanup.CheckoutReceipt.TargetDevice ||
		result.TargetInode != result.Cleanup.CheckoutReceipt.TargetInode || result.TargetInode == 0 ||
		(result.Disposition != authority.LinuxCloneTargetDispositionRemoved && result.Disposition != authority.LinuxCloneTargetDispositionAlreadyAbsent) ||
		!result.PathRewalkVerified || !result.BOwnedMountConfined ||
		result.Disposition == authority.LinuxCloneTargetDispositionRemoved && !result.TargetNlinkZero ||
		result.Disposition == authority.LinuxCloneTargetDispositionAlreadyAbsent && result.TargetNlinkZero ||
		result.RootDeleterUID != 0 || result.RootDeleter.Validate() != nil {
		return nil, errors.New("root clone cleanup result is invalid")
	}
	packet, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode root clone cleanup result: %w", err)
	}
	if len(packet) == 0 || len(packet) > cloneCleanupMaxFrame {
		clear(packet)
		return nil, errors.New("root clone cleanup result exceeds its fixed bound")
	}
	return packet, nil
}

// DecodeCloneCleanupResult accepts one strict root response tied to the exact
// request nonce and Store-issued cleanup intent.
func DecodeCloneCleanupResult(packet []byte, nonce string, cleanup authority.LinuxCloneCleanupIntent) (CloneCleanupWireResult, error) {
	var result CloneCleanupWireResult
	if len(packet) == 0 || len(packet) > cloneCleanupMaxFrame || !canonicalLowerHex(nonce, 32) || cleanup.Validate() != nil {
		return result, errors.New("root clone cleanup result inputs are invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(packet))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return CloneCleanupWireResult{}, fmt.Errorf("decode root clone cleanup result: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return CloneCleanupWireResult{}, errors.New("root clone cleanup result has trailing JSON data")
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(packet, canonical) {
		clear(canonical)
		return CloneCleanupWireResult{}, errors.New("root clone cleanup result is not the canonical fixed encoding")
	}
	clear(canonical)
	if result.Nonce != nonce || !result.Cleanup.Equal(cleanup) {
		return CloneCleanupWireResult{}, errors.New("root clone cleanup result differs from the exact request")
	}
	validated, err := EncodeCloneCleanupResult(result)
	clear(validated)
	if err != nil {
		return CloneCleanupWireResult{}, err
	}
	return result, nil
}

// RemoveBCloneAfterReceipt requests one bounded cleanup from the installed
// root/A setuid launcher. rootFD and targetFD are borrowed O_PATH descriptors;
// targetFD may be -1 only with the exact Store-persisted Removed proof for this
// cleanup intent and checkout. The root helper independently verifies the
// derived name remains absent; after exact child pidfd reap, this method returns
// the prior proof unchanged. ENOENT alone never proves the original inode was
// removed. The absent mode inherits a validated O_PATH /dev/null sentinel in
// the fixed target slot and never closes either borrowed descriptor.
func RemoveBCloneAfterReceipt(ctx context.Context, rootFD, targetFD int, grant authority.LinuxCloneGrant,
	cleanup authority.LinuxCloneCleanupIntent, priorRemovalProof *authority.LinuxCloneTargetRemovalProof) (authority.LinuxCloneTargetRemovalProof, error) {
	if ctx == nil || rootFD < 0 || targetFD < -1 || grant.Validate() != nil || cleanup.Validate() != nil ||
		!grant.Intent.Equal(cleanup.Intent) || !grant.Seed.Equal(cleanup.SeedReceipt) {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("Linux clone cleanup caller context or authority is invalid")
	}
	if targetFD >= 0 && priorRemovalProof != nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("fresh Linux clone cleanup cannot carry a prior target-removal proof")
	}
	if targetFD < 0 && priorRemovalProof == nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown,
			errors.New("absent target cannot authorize cleanup without a prior durable Removed proof"))
	}
	if err := ctx.Err(); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	config, err := ReadInstalledOwnerConfig()
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	if config.StateDir == "" {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown,
			errors.New("installed legacy owner config has no trusted StateDir for root cleanup authorization"))
	}
	if uint32(os.Getuid()) != config.AUID || uint32(os.Getgid()) != config.AGID {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("Linux clone cleanup is restricted to the configured A identity")
	}
	helperIdentity, err := CaptureLauncherIdentity(os.Getpid())
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, fmt.Errorf("capture exact A cleanup anchor identity: %w", err)
	}
	expectedAnchor := authority.LinuxCgroupProcessIdentity{
		PID: cleanup.NormalReleaseProof.AnchorPID, Identity: cleanup.NormalReleaseProof.AnchorIdentity,
	}
	if helperIdentity.PID != expectedAnchor.PID || helperIdentity.Identity != expectedAnchor.Identity {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("root cleanup must be called by the exact A anchor in the normal release proof")
	}
	if err := verifyInstalledLauncher(config); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	rootIdentity, err := verifyCleanupPathFD(rootFD, config.BUID, config.BGID, "B clone root")
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	rootPath, err := readPinnedDirectoryPath(rootFD)
	if err != nil || rootPath != cleanup.RootPath {
		return authority.LinuxCloneTargetRemovalProof{}, fmt.Errorf("pinned B root differs from the Store cleanup intent: path=%q want=%q err=%v", rootPath, cleanup.RootPath, err)
	}
	relative, err := CloneCleanupTargetRelativePath(grant.Intent)
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	expectedTarget := filepath.Join(cleanup.RootPath, relative)
	if grant.Intent.Target != expectedTarget {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("RunKey-derived target differs from the exact clone intent")
	}
	targetDevice, targetInode := cleanup.CheckoutReceipt.TargetDevice, cleanup.CheckoutReceipt.TargetInode
	if targetDevice != rootIdentity.Device {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("durable checkout device differs from the pinned B root filesystem")
	}
	if priorRemovalProof != nil {
		if err := validatePriorRemovalProofIntent(*priorRemovalProof, cleanup); err != nil ||
			priorRemovalProof.RootDevice != rootIdentity.Device || priorRemovalProof.RootInode != rootIdentity.Inode {
			return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown,
				fmt.Errorf("absent-target replay lacks the exact Store-bound root removal proof: %w", errOr(err, errors.New("pinned root identity differs"))))
		}
		cloned := priorRemovalProof.Clone()
		priorRemovalProof = &cloned
	}
	if targetFD >= 0 {
		targetIdentity, err := verifyCleanupPathFD(targetFD, config.BUID, config.BGID, "B clone target")
		if err != nil {
			return authority.LinuxCloneTargetRemovalProof{}, err
		}
		if targetIdentity.Device != targetDevice || targetIdentity.Inode != targetInode || targetIdentity.Device != rootIdentity.Device {
			return authority.LinuxCloneTargetRemovalProof{}, errors.New("pinned B target FD differs from checkout or root device/inode")
		}
		path, err := readPinnedDirectoryPath(targetFD)
		if err != nil || path != expectedTarget {
			return authority.LinuxCloneTargetRemovalProof{}, fmt.Errorf("pinned B target differs from the RunKey-derived path: path=%q want=%q err=%v", path, expectedTarget, err)
		}
		if err := verifyTargetNameIdentity(rootFD, relative, targetDevice, targetInode, config, rootIdentity.Device); err != nil {
			return authority.LinuxCloneTargetRemovalProof{}, fmt.Errorf("verify pinned target name before root cleanup: %w", err)
		}
	} else if err := verifyTargetAbsentUnderRoot(rootFD, relative, config, rootIdentity.Device); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, fmt.Errorf("absence replay requires the exact target absent under the pinned root: %w", err)
	}

	nonce, err := randomCleanupNonce()
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	requestPacket, err := EncodeCloneCleanupRequest(nonce, grant, cleanup, priorRemovalProof)
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	defer clear(requestPacket)
	return runCloneCleanupChild(ctx, rootFD, targetFD, cleanup, relative, rootIdentity, targetDevice, targetInode,
		nonce, helperIdentity, config, priorRemovalProof, requestPacket)
}

type cleanupFDIdentity struct {
	Device uint64
	Inode  uint64
}

func validatePriorRemovalProofIntent(prior authority.LinuxCloneTargetRemovalProof, cleanup authority.LinuxCloneCleanupIntent) error {
	if prior.Validate() != nil || !prior.Intent.Equal(cleanup) ||
		prior.Disposition != authority.LinuxCloneTargetDispositionRemoved || !prior.TargetNlinkZero ||
		prior.RootDevice != cleanup.CheckoutReceipt.TargetDevice || prior.RootInode == 0 ||
		prior.TargetDevice != cleanup.CheckoutReceipt.TargetDevice || prior.TargetInode != cleanup.CheckoutReceipt.TargetInode {
		return errors.New("prior root removal proof is not the exact Removed proof for this checkout")
	}
	return nil
}

func verifyCleanupPathFD(fd int, uid, gid uint32, label string) (cleanupFDIdentity, error) {
	if fd < 0 {
		return cleanupFDIdentity{}, fmt.Errorf("%s O_PATH descriptor is unavailable", label)
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_PATH == 0 {
		return cleanupFDIdentity{}, fmt.Errorf("%s descriptor is not O_PATH: flags=%#x err=%v", label, flags, err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return cleanupFDIdentity{}, fmt.Errorf("stat pinned %s: %w", label, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != uid || stat.Gid != gid || stat.Mode&07000 != 0 || stat.Mode&0022 != 0 {
		return cleanupFDIdentity{}, fmt.Errorf("pinned %s is not the exact B-owned directory", label)
	}
	return cleanupFDIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

func readPinnedDirectoryPath(fd int) (string, error) {
	path, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return "", fmt.Errorf("read pinned directory path: %w", err)
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.Contains(path, " (deleted)") {
		return "", errors.New("pinned directory has a noncanonical or deleted path")
	}
	return path, nil
}

// CloneCleanupTargetRelativePath derives the only child path the root cleanup
// worker may open from the immutable BindingKey and RunKey.
func CloneCleanupTargetRelativePath(intent authority.LinuxCloneIntent) (string, error) {
	if err := intent.Validate(); err != nil {
		return "", err
	}
	return filepath.Join("binding-"+intent.BindingKey, "run-"+intent.RunID, fmt.Sprintf("generation-%d", intent.Generation)), nil
}

func openCleanupRelativeDirectory(rootFD int, relative string, flags int, config OwnerConfig, rootDevice uint64) (int, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative ||
		relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return -1, errors.New("cleanup relative directory escapes its pinned root")
	}
	components := strings.Split(filepath.ToSlash(relative), "/")
	currentFD := rootFD
	ownsCurrent := false
	for index, component := range components {
		if component == "" || component == "." || component == ".." || strings.ContainsRune(component, '\x00') {
			if ownsCurrent {
				_ = unix.Close(currentFD)
			}
			return -1, errors.New("cleanup relative path contains an unsafe component")
		}
		openFlags := unix.O_PATH | unix.O_DIRECTORY
		if index == len(components)-1 {
			openFlags = flags
		}
		how := &unix.OpenHow{
			Flags:   uint64(openFlags | unix.O_CLOEXEC | unix.O_NOFOLLOW),
			Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
		}
		nextFD, err := unix.Openat2(currentFD, component, how)
		if err != nil {
			if ownsCurrent {
				_ = unix.Close(currentFD)
			}
			return -1, err
		}
		if ownsCurrent {
			_ = unix.Close(currentFD)
		}
		currentFD = nextFD
		ownsCurrent = true
		var stat unix.Stat_t
		if err := unix.Fstat(currentFD, &stat); err != nil {
			_ = unix.Close(currentFD)
			return -1, err
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(stat.Dev) != rootDevice || stat.Uid != config.BUID ||
			stat.Gid != config.BGID || stat.Mode&07000 != 0 || stat.Mode&0022 != 0 {
			_ = unix.Close(currentFD)
			return -1, errors.New("cleanup ancestor is unsafe or crosses the pinned B root filesystem")
		}
		if index == len(components)-1 {
			return currentFD, nil
		}
	}
	return -1, errors.New("cleanup relative path has no components")
}

func verifyTargetNameIdentity(rootFD int, relative string, device, inode uint64, config OwnerConfig, rootDevice uint64) error {
	fd, err := openCleanupRelativeDirectory(rootFD, relative, unix.O_PATH|unix.O_DIRECTORY, config, rootDevice)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if uint64(stat.Dev) != device || stat.Ino != inode || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("RunKey target name was replaced or crossed a mount boundary")
	}
	return nil
}

func verifyTargetAbsentUnderRoot(rootFD int, relative string, config OwnerConfig, rootDevice uint64) error {
	parentRelative, name := filepath.Split(relative)
	parentRelative = strings.TrimSuffix(parentRelative, string(filepath.Separator))
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00\r\n") {
		return errors.New("RunKey target basename is invalid")
	}
	if parentRelative == "" {
		return errors.New("RunKey target parent is empty")
	}
	parentFD, err := openCleanupRelativeDirectory(rootFD, parentRelative, unix.O_RDONLY|unix.O_DIRECTORY, config, rootDevice)
	if err != nil {
		return fmt.Errorf("reopen exact RunKey target ancestors for absence proof: %w", err)
	}
	defer unix.Close(parentFD)
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		if err != nil {
			return err
		}
		return errors.New("RunKey target name remains or was replaced")
	}
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		if err != nil {
			return err
		}
		return errors.New("RunKey target name reappeared during absence readback")
	}
	return nil
}

func randomCleanupNonce() (string, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("generate root cleanup correlation nonce: %w", err)
	}
	return hex.EncodeToString(nonce[:]), nil
}

func runCloneCleanupChild(ctx context.Context, rootFD, targetFD int,
	cleanup authority.LinuxCloneCleanupIntent, relative string, rootIdentity cleanupFDIdentity, targetDevice, targetInode uint64,
	nonce string, helperIdentity LauncherIdentity, config OwnerConfig, priorRemovalProof *authority.LinuxCloneTargetRemovalProof,
	requestPacket []byte) (authority.LinuxCloneTargetRemovalProof, error) {
	if ctx == nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("clone cleanup context is nil")
	}
	parentChildFDs, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, fmt.Errorf("create root cleanup control channel: %w", err)
	}
	parentFD, childFD := parentChildFDs[0], parentChildFDs[1]
	parentSocket := os.NewFile(uintptr(parentFD), "symmetry-linux-clone-cleanup-parent")
	childSocket := os.NewFile(uintptr(childFD), "symmetry-linux-clone-cleanup-child")
	if parentSocket == nil || childSocket == nil {
		_ = unix.Close(parentFD)
		_ = unix.Close(childFD)
		return authority.LinuxCloneTargetRemovalProof{}, errors.New("wrap root cleanup control channel")
	}
	defer parentSocket.Close()
	defer func() {
		if childSocket != nil {
			_ = childSocket.Close()
		}
	}()

	rootDuplicate, err := duplicateCleanupFD(rootFD, "B clone root")
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, err
	}
	var targetDuplicate *os.File
	if targetFD >= 0 {
		targetDuplicate, err = duplicateCleanupFD(targetFD, "B clone target")
		if err != nil {
			return authority.LinuxCloneTargetRemovalProof{}, err
		}
	} else {
		targetDuplicate, err = openCleanupAbsentSentinel()
		if err != nil {
			return authority.LinuxCloneTargetRemovalProof{}, err
		}
	}
	defer func() {
		if rootDuplicate != nil {
			_ = rootDuplicate.Close()
		}
		if targetDuplicate != nil {
			_ = targetDuplicate.Close()
		}
	}()
	extraFiles := []*os.File{childSocket, rootDuplicate, targetDuplicate}
	command := exec.Command(launcherPath, cloneCleanupOperation)
	command.Args = []string{launcherPath, cloneCleanupOperation}
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	command.Dir = "/"
	command.ExtraFiles = extraFiles
	launcherPIDFD := -1
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, PidFD: &launcherPIDFD}
	if err := command.Start(); err != nil {
		if command.Process != nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
		return authority.LinuxCloneTargetRemovalProof{}, fmt.Errorf("start fixed root clone cleanup worker: %w", err)
	}
	_ = childSocket.Close()
	childSocket = nil
	_ = rootDuplicate.Close()
	rootDuplicate = nil
	_ = targetDuplicate.Close()
	targetDuplicate = nil
	if launcherPIDFD < 0 {
		return authority.LinuxCloneTargetRemovalProof{}, reapCleanupChildWithoutPIDFD(command, errors.New("root cleanup did not return a pidfd"))
	}
	defer unix.Close(launcherPIDFD)
	if pid, err := readPIDFDTarget(launcherPIDFD); err != nil || pid != command.Process.Pid {
		return authority.LinuxCloneTargetRemovalProof{}, reapFailedCleanupChild(command, launcherPIDFD,
			fmt.Errorf("root cleanup pidfd differs from direct A child: pid=%d err=%v", pid, err))
	}
	workerStartTime, err := readProcessStartTime(command.Process.Pid)
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, reapFailedCleanupChild(command, launcherPIDFD,
			fmt.Errorf("capture transient root cleanup start time: %w", err))
	}
	if workerStartTime == 0 {
		return authority.LinuxCloneTargetRemovalProof{}, reapFailedCleanupChild(command, launcherPIDFD,
			errors.New("transient root cleanup start time is zero"))
	}
	workerIdentity := LauncherIdentity{PID: command.Process.Pid, StartTime: workerStartTime, BootID: helperIdentity.BootID}
	readyPacket, err := receiveCleanupPacket(ctx, parentFD, time.Now().Add(cloneCleanupReadyTimeout))
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, reapFailedCleanupChild(command, launcherPIDFD,
			fmt.Errorf("receive root cleanup readiness: %w", err))
	}
	ready, err := DecodeLauncherReady(readyPacket)
	clear(readyPacket)
	if err != nil || ready.PID != workerIdentity.PID || ready.StartTime != workerIdentity.StartTime || ready.Identity == "" {
		return authority.LinuxCloneTargetRemovalProof{}, reapFailedCleanupChild(command, launcherPIDFD,
			fmt.Errorf("root cleanup READY differs from exact direct child: %v", errOr(err, errors.New("identity mismatch"))))
	}
	workerIdentity.Identity = ready.Identity
	if err := ctx.Err(); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, reapFailedCleanupChild(command, launcherPIDFD, err)
	}
	if err := sendCleanupPacket(parentFD, requestPacket); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown,
			reapFailedCleanupChild(command, launcherPIDFD, fmt.Errorf("send single-use root cleanup request: %w", err)))
	}
	resultPacket, err := receiveCleanupPacket(ctx, parentFD, time.Now().Add(cloneCleanupWorkTimeout))
	if err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown,
			reapFailedCleanupChild(command, launcherPIDFD, fmt.Errorf("receive root cleanup result: %w", err)))
	}
	result, decodeErr := DecodeCloneCleanupResult(resultPacket, nonce, cleanup)
	clear(resultPacket)
	if decodeErr != nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown,
			reapFailedCleanupChild(command, launcherPIDFD, decodeErr))
	}
	if result.RootDevice != rootIdentity.Device || result.RootInode != rootIdentity.Inode ||
		result.TargetDevice != targetDevice || result.TargetInode != targetInode ||
		result.RootDeleter.PID != workerIdentity.PID || result.RootDeleter.Identity != workerIdentity.Identity ||
		result.RootDeleterUID != 0 ||
		result.Disposition == authority.LinuxCloneTargetDispositionRemoved && (targetFD < 0 || !result.TargetNlinkZero) ||
		result.Disposition == authority.LinuxCloneTargetDispositionAlreadyAbsent && (targetFD >= 0 || result.TargetNlinkZero || priorRemovalProof == nil) {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown,
			reapFailedCleanupChild(command, launcherPIDFD, errors.New("root cleanup result differs from the pinned request identity")))
	}
	if err := waitProcessFD(context.Background(), launcherPIDFD, time.Now().Add(cloneCleanupReapTimeout)); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown,
			reapFailedCleanupChild(command, launcherPIDFD, fmt.Errorf("root cleanup result is unresolved without exact pidfd wait: %w", err)))
	}
	if err := waitReapCleanupChild(launcherPIDFD); err != nil {
		releaseErr := command.Process.Release()
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown,
			fmt.Errorf("root cleanup child did not exit successfully: %w", err), releaseErr)
	}
	if err := command.Process.Release(); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown, fmt.Errorf("release reaped root cleanup child handle: %w", err))
	}
	if err := verifyCleanupPeerEOF(parentFD); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown, fmt.Errorf("verify root cleanup channel retirement: %w", err))
	}
	if err := verifyCleanupRootAndTargetAfter(rootFD, relative, cleanup, rootIdentity, targetFD, targetDevice, targetInode, config); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown, fmt.Errorf("independent A readback after root cleanup: %w", err))
	}
	if targetFD < 0 {
		return priorRemovalProof.Clone(), nil
	}
	process := authority.LinuxCgroupProcessIdentity{PID: workerIdentity.PID, Identity: workerIdentity.Identity}
	witness := authority.LinuxCgroupProcessIdentity{PID: helperIdentity.PID, Identity: helperIdentity.Identity}
	proof := authority.LinuxCloneTargetRemovalProof{
		Version: authority.LinuxCloneVersion, Intent: cleanup.Clone(),
		RootDevice: rootIdentity.Device, RootInode: rootIdentity.Inode,
		TargetDevice: targetDevice, TargetInode: targetInode,
		Disposition: result.Disposition, PathRewalkVerified: result.PathRewalkVerified,
		BOwnedMountConfined: result.BOwnedMountConfined, TargetNlinkZero: result.TargetNlinkZero,
		RootDeleterUID: 0, RootDeleter: process,
		RootDeleterExit: authority.LinuxCgroupExitProof{
			Version: authority.LinuxCgroupIdentityVersion,
			Process: process, Witness: witness, Waited: true, Reaped: true,
		},
	}
	if !proof.RootDeleterExit.ValidFor(process, witness) {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown, errors.New("root cleanup exit proof does not bind its exact worker and A witness"))
	}
	if err := proof.Validate(); err != nil {
		return authority.LinuxCloneTargetRemovalProof{}, errors.Join(ErrCloneCleanupOutcomeUnknown, fmt.Errorf("validate exact root cleanup wait/reap proof: %w", err))
	}
	return proof, nil
}

func duplicateCleanupFD(fd int, name string) (*os.File, error) {
	duplicate, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 10)
	if err != nil {
		return nil, fmt.Errorf("duplicate borrowed %s descriptor: %w", name, err)
	}
	file := os.NewFile(uintptr(duplicate), name)
	if file == nil {
		_ = unix.Close(duplicate)
		return nil, fmt.Errorf("wrap duplicated %s descriptor", name)
	}
	return file, nil
}

func openCleanupAbsentSentinel() (*os.File, error) {
	devFD, err := unix.Open("/dev", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open pinned /dev for absent-target sentinel: %w", err)
	}
	var devStat unix.Stat_t
	if err := unix.Fstat(devFD, &devStat); err != nil || devStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		devStat.Uid != 0 || devStat.Gid != 0 || devStat.Mode&0022 != 0 {
		_ = unix.Close(devFD)
		return nil, fmt.Errorf("/dev is not a root-owned non-writable directory: %v", err)
	}
	fd, err := unix.Openat(devFD, "null", unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	devCloseErr := unix.Close(devFD)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open fixed absent-target /dev/null sentinel: %w", err), devCloseErr)
	}
	if devCloseErr != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("close pinned /dev descriptor: %w", devCloseErr)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFCHR || stat.Uid != 0 || stat.Gid != 0 ||
		unix.Major(uint64(stat.Rdev)) != 1 || unix.Minor(uint64(stat.Rdev)) != 3 {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("fixed absent-target descriptor is not root-owned /dev/null: %v", err)
	}
	file := os.NewFile(uintptr(fd), "symmetry-linux-clone-cleanup-absent-target")
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("wrap fixed absent-target descriptor sentinel")
	}
	return file, nil
}

func sendCleanupPacket(fd int, packet []byte) error {
	if len(packet) == 0 || len(packet) > cloneCleanupMaxFrame {
		return errors.New("clone cleanup packet has invalid size")
	}
	n, err := unix.SendmsgN(fd, packet, nil, nil, unix.MSG_NOSIGNAL)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("clone cleanup seqpacket short write: %d/%d", n, len(packet))
	}
	return nil
}

func receiveCleanupPacket(ctx context.Context, fd int, deadline time.Time) ([]byte, error) {
	if err := waitForStatus(ctx, fd, deadline); err != nil {
		return nil, err
	}
	buffer := make([]byte, cloneCleanupMaxFrame+1)
	oob := make([]byte, unix.CmsgSpace(4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, buffer, oob, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, err
	}
	if oobn != 0 || n == 0 || n > cloneCleanupMaxFrame || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return nil, errors.New("clone cleanup socket carried empty, oversized, truncated, or ancillary data")
	}
	return buffer[:n], nil
}

func verifyCleanupPeerEOF(fd int) error {
	var data [1]byte
	oob := make([]byte, unix.CmsgSpace(4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data[:], oob, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if err != nil || n != 0 || oobn != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC|unix.MSG_EOR) != 0 {
		return fmt.Errorf("root cleanup peer did not close after its single result: n=%d oob=%d flags=%#x err=%v", n, oobn, flags, err)
	}
	return nil
}

func waitReapCleanupChild(pidfd int) error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PIDFD, pidfd, &info, unix.WEXITED, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reap exact root cleanup child by pidfd: %w", err)
		}
		break
	}
	if info.Signo != int32(unix.SIGCHLD) || info.Code != 1 {
		return errors.New("root cleanup child did not exit normally")
	}
	status, err := cleanupSiginfoStatus(&info)
	if err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("root cleanup child exited with status %d", status)
	}
	return nil
}

func cleanupSiginfoStatus(info *unix.Siginfo) (int32, error) {
	if info == nil || unsafe.Sizeof(*info) != 128 {
		return 0, errors.New("Linux amd64 siginfo_t layout is invalid for root cleanup wait")
	}
	data := (*[128]byte)(unsafe.Pointer(info))
	return int32(binary.LittleEndian.Uint32(data[24:28])), nil
}

func reapFailedCleanupChild(command *exec.Cmd, pidfd int, cause error) error {
	if command == nil || command.Process == nil {
		return cause
	}
	_ = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
	reapCtx, cancel := context.WithTimeout(context.Background(), cloneCleanupReapTimeout)
	reapErr := waitProcessFD(reapCtx, pidfd, time.Now().Add(cloneCleanupReapTimeout))
	cancel()
	if reapErr == nil {
		reapErr = waitReapCleanupChild(pidfd)
	}
	releaseErr := command.Process.Release()
	return errors.Join(cause, reapErr, releaseErr)
}

func reapCleanupChildWithoutPIDFD(command *exec.Cmd, cause error) error {
	if command == nil || command.Process == nil {
		return cause
	}
	killErr := command.Process.Kill()
	_, waitErr := command.Process.Wait()
	return errors.Join(cause, killErr, waitErr)
}

func verifyCleanupRootAndTargetAfter(rootFD int, relative string, cleanup authority.LinuxCloneCleanupIntent,
	expectedRoot cleanupFDIdentity, targetFD int, targetDevice, targetInode uint64, config OwnerConfig) error {
	rootIdentity, err := fstatCleanupDirectory(rootFD)
	if err != nil || rootIdentity != expectedRoot {
		return fmt.Errorf("pinned B root changed during cleanup: got=%+v want=%+v err=%v", rootIdentity, expectedRoot, err)
	}
	rootPath, err := readPinnedDirectoryPath(rootFD)
	if err != nil || rootPath != cleanup.RootPath {
		return fmt.Errorf("pinned B root path changed during cleanup: got=%q want=%q err=%v", rootPath, cleanup.RootPath, err)
	}
	if targetFD >= 0 {
		var pinnedTarget unix.Stat_t
		if err := unix.Fstat(targetFD, &pinnedTarget); err != nil ||
			pinnedTarget.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(pinnedTarget.Dev) != targetDevice ||
			pinnedTarget.Ino != targetInode || pinnedTarget.Nlink != 0 {
			return fmt.Errorf("original pinned B target was not unlinked: dev=%d ino=%d nlink=%d want=%d:%d:0 err=%v",
				pinnedTarget.Dev, pinnedTarget.Ino, pinnedTarget.Nlink, targetDevice, targetInode, err)
		}
	}
	return verifyTargetAbsentUnderRoot(rootFD, relative, config, expectedRoot.Device)
}

func fstatCleanupDirectory(fd int) (cleanupFDIdentity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return cleanupFDIdentity{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return cleanupFDIdentity{}, errors.New("pinned cleanup root is no longer a directory")
	}
	return cleanupFDIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}
