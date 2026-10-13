//go:build linux && amd64

package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"golang.org/x/sys/unix"
)

// LinuxCloneCleanupReader pins an existing A-owned StateDir and its runs
// directory for read-only verification by the trusted root cleanup command.
// It never opens a Store, creates files, or writes journal state.
type LinuxCloneCleanupReader struct {
	mu       sync.Mutex
	stateDir string
	stateFD  int
	runsFD   int
	lockFD   int
	opened   bool
	stateID  linuxCloneCleanupFileIdentity
	runsID   linuxCloneCleanupFileIdentity
	lockID   linuxCloneCleanupFileIdentity
	closed   bool
}

type linuxCloneCleanupFileIdentity struct {
	device uint64
	inode  uint64
	uid    uint32
	mode   uint32
	nlink  uint64
	size   int64
}

// OpenLinuxCloneCleanupReader opens an existing canonical StateDir read-only.
// pinnedStateDir must come only from the root-installed owner configuration;
// this API intentionally cannot create an alternate StateDir or Store.
func OpenLinuxCloneCleanupReader(pinnedStateDir string) (*LinuxCloneCleanupReader, error) {
	stateFD, err := openLinuxCloneCleanupStateDir(pinnedStateDir)
	if err != nil {
		return nil, err
	}
	stateID, err := linuxCloneCleanupDirectoryIdentity(stateFD)
	if err != nil {
		_ = unix.Close(stateFD)
		return nil, err
	}
	if stateID.uid != uint32(os.Geteuid()) {
		_ = unix.Close(stateFD)
		return nil, errors.New("pinned StateDir is not owned by the verifier's daemon UID")
	}
	runsFD, err := unix.Openat(stateFD, runsDirectoryName, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		_ = unix.Close(stateFD)
		return nil, errors.New("open pinned Linux clone cleanup runs directory")
	}
	runsID, err := linuxCloneCleanupDirectoryIdentity(runsFD)
	if err != nil || runsID.uid != stateID.uid {
		_ = unix.Close(runsFD)
		_ = unix.Close(stateFD)
		return nil, errors.New("Linux clone cleanup runs directory identity differs from StateDir")
	}
	lockFD, err := unix.Openat(stateFD, lockFileName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		_ = unix.Close(runsFD)
		_ = unix.Close(stateFD)
		return nil, errors.New("open existing daemon State lock read-only")
	}
	lockID, err := linuxCloneCleanupRegularFileIdentity(lockFD, stateID.uid, 0, 0o600)
	if err != nil {
		_ = unix.Close(lockFD)
		_ = unix.Close(runsFD)
		_ = unix.Close(stateFD)
		return nil, errors.New("existing daemon State lock has unsafe identity")
	}
	return &LinuxCloneCleanupReader{
		stateDir: pinnedStateDir, stateFD: stateFD, runsFD: runsFD, lockFD: lockFD, opened: true,
		stateID: stateID, runsID: runsID, lockID: lockID,
	}, nil
}

func openLinuxCloneCleanupStateDir(pinnedStateDir string) (int, error) {
	if strings.TrimSpace(pinnedStateDir) == "" || !filepath.IsAbs(pinnedStateDir) ||
		filepath.Clean(pinnedStateDir) != pinnedStateDir || pinnedStateDir == string(filepath.Separator) {
		return -1, errors.New("Linux clone cleanup StateDir is not a canonical absolute path")
	}
	rootFD, err := unix.Open(string(filepath.Separator), unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, errors.New("open root for pinned Linux clone cleanup StateDir")
	}
	defer unix.Close(rootFD)
	relative := strings.TrimPrefix(pinnedStateDir, string(filepath.Separator))
	stateFD, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
	})
	if err != nil {
		return -1, errors.New("open pinned Linux clone cleanup StateDir without symlinks")
	}
	return stateFD, nil
}

// Close releases the pinned, read-only StateDir descriptors.
func (reader *LinuxCloneCleanupReader) Close() error {
	if reader == nil {
		return nil
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.closed {
		return nil
	}
	reader.closed = true
	if !reader.opened {
		return nil
	}
	reader.opened = false
	var result error
	for _, fd := range []int{reader.lockFD, reader.runsFD, reader.stateFD} {
		if fd >= 0 {
			result = errors.Join(result, unix.Close(fd))
		}
	}
	reader.lockFD, reader.runsFD, reader.stateFD = -1, -1, -1
	if result != nil {
		return errors.New("close pinned Linux clone cleanup StateDir")
	}
	return nil
}

// Verify proves the current atomic RunJournal contains the exact pending
// cleanup authority. targetFDPresent selects the root worker's fresh-target
// mode; an absent target is accepted only with the exact Store-persisted
// Removed receipt supplied as prior.
func (reader *LinuxCloneCleanupReader) Verify(
	key RunKey,
	grant authority.LinuxCloneGrant,
	intent authority.LinuxCloneCleanupIntent,
	prior *authority.LinuxCloneTargetRemovalProof,
	targetFDPresent bool,
) error {
	return reader.verify(key, grant, intent, prior, targetFDPresent, nil)
}

func (reader *LinuxCloneCleanupReader) verify(
	key RunKey,
	grant authority.LinuxCloneGrant,
	intent authority.LinuxCloneCleanupIntent,
	prior *authority.LinuxCloneTargetRemovalProof,
	targetFDPresent bool,
	beforeSync func() error,
) error {
	if reader == nil {
		return errors.New("Linux clone cleanup reader is nil")
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.closed || !reader.opened || reader.stateFD < 0 || reader.runsFD < 0 || reader.lockFD < 0 {
		return errors.New("Linux clone cleanup reader is closed")
	}
	if err := validateKey(key); err != nil || requireLinuxClonePlatform() != nil ||
		grant.Validate() != nil || intent.Validate() != nil || !grant.Intent.Equal(intent.Intent) ||
		!grant.Seed.Equal(intent.SeedReceipt) || targetFDPresent == (prior != nil) {
		return errors.New("Linux clone cleanup verification request is invalid")
	}
	if err := reader.verifyPinnedDirectoriesAndDaemonLock(); err != nil {
		return err
	}
	name := filepath.Base((&Store{dir: reader.stateDir}).journalPath(key))
	firstFD, firstBytes, firstID, err := reader.openAndReadJournal(name)
	if err != nil {
		return err
	}
	defer unix.Close(firstFD)
	first, err := decodeLinuxCloneCleanupJournal(firstBytes)
	if err != nil || !reader.linuxCloneCleanupAuthorityMatches(first, key, grant, intent, prior, targetFDPresent) {
		return errors.Join(errors.New("initial State snapshot lacks exact pending Linux clone cleanup authority"), err)
	}
	if beforeSync != nil {
		if err := beforeSync(); err != nil {
			return errors.New("injected Linux clone cleanup journal rewrite failed")
		}
	}
	if err := unix.Fsync(firstFD); err != nil {
		return errors.New("fsync opened Linux clone cleanup journal")
	}
	if err := unix.Fsync(reader.runsFD); err != nil {
		return errors.New("fsync pinned Linux clone cleanup runs directory")
	}
	if err := reader.verifyPinnedDirectoriesAndDaemonLock(); err != nil {
		return err
	}
	secondFD, secondBytes, secondID, err := reader.openAndReadJournal(name)
	if err != nil {
		return err
	}
	defer unix.Close(secondFD)
	if !firstID.equal(secondID) || !bytes.Equal(firstBytes, secondBytes) {
		return errors.New("Linux clone cleanup journal changed across the fsync readback barrier")
	}
	second, err := decodeLinuxCloneCleanupJournal(secondBytes)
	if err != nil || !reader.linuxCloneCleanupAuthorityMatches(second, key, grant, intent, prior, targetFDPresent) {
		return errors.Join(errors.New("reopened State snapshot lacks exact pending Linux clone cleanup authority"), err)
	}
	return nil
}

func (reader *LinuxCloneCleanupReader) linuxCloneCleanupAuthorityMatches(
	journal RunJournal,
	key RunKey,
	grant authority.LinuxCloneGrant,
	intent authority.LinuxCloneCleanupIntent,
	prior *authority.LinuxCloneTargetRemovalProof,
	targetFDPresent bool,
) bool {
	if journal.Key() != key || journal.LinuxCloneIntent == nil || !journal.LinuxCloneIntent.Equal(grant.Intent) ||
		journal.LinuxCloneSeedReceipt == nil || !journal.LinuxCloneSeedReceipt.Equal(grant.Seed) ||
		journal.LinuxCloneCheckoutReceipt == nil || !journal.LinuxCloneCheckoutReceipt.Equal(intent.CheckoutReceipt) ||
		journal.LinuxClonePromotionIntent == nil || !journal.LinuxClonePromotionIntent.Equal(intent.PromotionIntent) ||
		journal.LinuxClonePromotionReceipt == nil || !journal.LinuxClonePromotionReceipt.Equal(intent.PromotionReceipt) ||
		journal.ContainmentCgroupReleaseProof == nil || !journal.ContainmentCgroupReleaseProof.Equal(intent.NormalReleaseProof) ||
		!journal.LinuxCloneCleanupPending || journal.LinuxCloneCleanupIntent == nil ||
		!journal.LinuxCloneCleanupIntent.Equal(intent) || journal.LinuxCloneCleanupProof != nil ||
		!linuxCloneCleanupIntentMatchesJournal(journal, intent) || !journal.LinuxCloneCleanupBeginEligible(key, journal.Fence()) {
		return false
	}
	if targetFDPresent {
		return prior == nil && journal.LinuxCloneTargetRemovalProof == nil
	}
	return prior != nil && prior.Validate() == nil && prior.Disposition == authority.LinuxCloneTargetDispositionRemoved &&
		prior.TargetNlinkZero && journal.LinuxCloneTargetRemovalProof != nil &&
		journal.LinuxCloneTargetRemovalProof.Equal(*prior)
}

func decodeLinuxCloneCleanupJournal(data []byte) (RunJournal, error) {
	var journal RunJournal
	if err := decodeJSONWithLimitAndTags(data, "run journal", &journal, maxJournalFileBytes, reflect.TypeOf(RunJournal{})); err != nil {
		return RunJournal{}, err
	}
	if err := validateJournal(journal); err != nil {
		return RunJournal{}, errors.New("invalid run journal")
	}
	return journal, nil
}

func (reader *LinuxCloneCleanupReader) verifyPinnedDirectoriesAndDaemonLock() error {
	stateID, err := linuxCloneCleanupDirectoryIdentity(reader.stateFD)
	if err != nil || !reader.stateID.sameDirectory(stateID) {
		return errors.Join(errors.New("pinned Linux clone cleanup StateDir changed identity"), err)
	}
	namedStateFD, err := openLinuxCloneCleanupStateDir(reader.stateDir)
	if err != nil {
		return errors.Join(errors.New("reopen configured Linux clone cleanup StateDir"), err)
	}
	namedStateID, statErr := linuxCloneCleanupDirectoryIdentity(namedStateFD)
	_ = unix.Close(namedStateFD)
	if statErr != nil || !reader.stateID.sameDirectory(namedStateID) {
		return errors.Join(errors.New("configured StateDir no longer names the pinned directory"), statErr)
	}
	runsID, err := linuxCloneCleanupDirectoryIdentity(reader.runsFD)
	if err != nil || !reader.runsID.sameDirectory(runsID) || runsID.uid != stateID.uid {
		return errors.Join(errors.New("pinned Linux clone cleanup runs directory changed identity"), err)
	}
	namedRunsFD, err := unix.Openat(reader.stateFD, runsDirectoryName, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("reopen named Linux clone cleanup runs directory")
	}
	namedRunsID, statErr := linuxCloneCleanupDirectoryIdentity(namedRunsFD)
	_ = unix.Close(namedRunsFD)
	if statErr != nil || !reader.runsID.sameDirectory(namedRunsID) {
		return errors.Join(errors.New("runs name no longer identifies the pinned directory"), statErr)
	}
	lockID, err := linuxCloneCleanupRegularFileIdentity(reader.lockFD, stateID.uid, 0, 0o600)
	if err != nil || !reader.lockID.equal(lockID) {
		return errors.Join(errors.New("daemon State lock changed identity"), err)
	}
	pathFD, err := unix.Openat(reader.stateFD, lockFileName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return errors.New("reopen daemon State lock read-only")
	}
	pathID, statErr := linuxCloneCleanupRegularFileIdentity(pathFD, stateID.uid, 0, 0o600)
	_ = unix.Close(pathFD)
	if statErr != nil || !reader.lockID.equal(pathID) {
		return errors.Join(errors.New("named daemon State lock differs from pinned lock"), statErr)
	}
	if !linuxCloneCleanupExclusiveFlockHeld(lockID) {
		return errors.New("daemon does not hold the exclusive State lock")
	}
	return nil
}

func (reader *LinuxCloneCleanupReader) openAndReadJournal(name string) (int, []byte, linuxCloneCleanupFileIdentity, error) {
	fd, err := unix.Openat(reader.runsFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return -1, nil, linuxCloneCleanupFileIdentity{}, errors.New("open RunKey-derived State journal read-only")
	}
	identity, err := linuxCloneCleanupRegularFileIdentity(fd, reader.stateID.uid, maxJournalFileBytes, 0o600)
	if err != nil {
		_ = unix.Close(fd)
		return -1, nil, linuxCloneCleanupFileIdentity{}, errors.New("RunKey-derived State journal has unsafe file identity")
	}
	data := make([]byte, identity.size)
	for offset := 0; offset < len(data); {
		count, readErr := unix.Pread(fd, data[offset:], int64(offset))
		if readErr != nil {
			_ = unix.Close(fd)
			return -1, nil, linuxCloneCleanupFileIdentity{}, errors.New("read pinned RunKey-derived State journal")
		}
		if count == 0 {
			_ = unix.Close(fd)
			return -1, nil, linuxCloneCleanupFileIdentity{}, errors.New("State journal changed size while reading")
		}
		offset += count
	}
	afterRead, err := linuxCloneCleanupRegularFileIdentity(fd, reader.stateID.uid, maxJournalFileBytes, 0o600)
	if err != nil || !identity.equal(afterRead) {
		_ = unix.Close(fd)
		return -1, nil, linuxCloneCleanupFileIdentity{}, errors.New("opened State journal changed while reading")
	}
	return fd, data, identity, nil
}

func linuxCloneCleanupDirectoryIdentity(fd int) (linuxCloneCleanupFileIdentity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0777 != 0o700 {
		return linuxCloneCleanupFileIdentity{}, errors.Join(errors.New("State directory is not a protected directory"), err)
	}
	return linuxCloneCleanupFileIdentity{
		device: uint64(stat.Dev), inode: stat.Ino, uid: stat.Uid, mode: stat.Mode,
		nlink: uint64(stat.Nlink), size: stat.Size,
	}, nil
}

func linuxCloneCleanupRegularFileIdentity(fd int, expectedUID uint32, maxSize int64, expectedMode uint32) (linuxCloneCleanupFileIdentity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != expectedUID || stat.Mode&0777 != expectedMode || stat.Nlink != 1 ||
		(maxSize > 0 && (stat.Size < 0 || stat.Size > maxSize)) {
		return linuxCloneCleanupFileIdentity{}, errors.Join(errors.New("State regular file identity is invalid"), err)
	}
	return linuxCloneCleanupFileIdentity{
		device: uint64(stat.Dev), inode: stat.Ino, uid: stat.Uid, mode: stat.Mode,
		nlink: uint64(stat.Nlink), size: stat.Size,
	}, nil
}

func (identity linuxCloneCleanupFileIdentity) equal(other linuxCloneCleanupFileIdentity) bool {
	return identity.device == other.device && identity.inode == other.inode && identity.uid == other.uid &&
		identity.mode == other.mode && identity.nlink == other.nlink && identity.size == other.size
}

func (identity linuxCloneCleanupFileIdentity) sameDirectory(other linuxCloneCleanupFileIdentity) bool {
	return identity.device == other.device && identity.inode == other.inode && identity.uid == other.uid && identity.mode == other.mode
}

func linuxCloneCleanupExclusiveFlockHeld(identity linuxCloneCleanupFileIdentity) bool {
	data, err := readLimited("/proc/locks", maxStateFileBytes)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[1] != "FLOCK" || fields[2] != "ADVISORY" || fields[3] != "WRITE" {
			continue
		}
		device := strings.Split(fields[5], ":")
		if len(device) != 3 {
			continue
		}
		major, majorErr := strconv.ParseUint(device[0], 16, 32)
		minor, minorErr := strconv.ParseUint(device[1], 16, 32)
		inode, inodeErr := strconv.ParseUint(device[2], 10, 64)
		if majorErr == nil && minorErr == nil && inodeErr == nil &&
			major == uint64(unix.Major(identity.device)) && minor == uint64(unix.Minor(identity.device)) && inode == identity.inode {
			return true
		}
	}
	return false
}
