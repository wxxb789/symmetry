// Package state persists machine-local daemon recovery state.
package state

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	maxStateFileBytes      = 1 << 20
	maxJournalFileBytes    = 4 << 20
	identityFileName       = "identity.json"
	enrollmentFileName     = "enrollment.json"
	runsDirectoryName      = "runs"
	goalUsageDirectoryName = "goal-usage"
	lockFileName           = ".symmetry-daemon.lock"
	journalFilePrefix      = "journal-"
	journalFileSuffix      = ".json"
	atomicTempPrefix       = ".symmetry-state-"
)

// Durable budget for a Goal session's uncertain reason, enforced on write and read:
// callers composing the reason from a cause must bound it to this.
const GoalSessionUncertainReasonBytes = 4096

// ErrStoreInUse indicates that another daemon process currently owns this
// state directory.
var ErrStoreInUse = errors.New("state directory is already in use")

var (
	applyDirectorySecurity = secureDirectory
	applyFileSecurity      = secureFile
)

// Store owns state rooted in one configured machine-local directory.
// Its methods are serialized so related read-modify-write operations cannot
// overwrite each other within a daemon process.
type Store struct {
	dir         string
	mu          sync.Mutex
	lock        *os.File
	closed      bool
	atomicWrite func(string, []byte) error
}

// RunKey uniquely identifies one fenced execution generation.
type RunKey struct {
	RunID      string
	Generation int64
}

type notFoundError struct {
	Resource string
}

func (err *notFoundError) Error() string {
	return err.Resource + " not found"
}

// IsNotFound reports whether err represents an absent state record.
func IsNotFound(err error) bool {
	var target *notFoundError
	return errors.As(err, &target)
}

// New opens a machine-local state root, creating private directories as needed.
func New(directory string) (*Store, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, errors.New("state directory must not be empty")
	}
	if err := ensurePrivateDirectory(directory); err != nil {
		return nil, fmt.Errorf("initialize state directory: %w", err)
	}
	lockPath := filepath.Join(directory, lockFileName)
	lock, err := acquireStoreLock(lockPath)
	if err != nil {
		return nil, err
	}
	if err := applyFileSecurity(lockPath); err != nil {
		_ = releaseStoreLock(lock)
		return nil, fmt.Errorf("secure state lock: %w", err)
	}
	store := &Store{dir: directory, lock: lock, atomicWrite: writeAtomic}
	if err := ensurePrivateDirectory(store.runsDir()); err != nil {
		_ = releaseStoreLock(lock)
		return nil, fmt.Errorf("create run journal directory: %w", err)
	}
	return store, nil
}

// SetAtomicWriterForTesting replaces the final journal-write primitive for a
// focused fault-injection test. Production callers must not use this seam.
func (store *Store) SetAtomicWriterForTesting(writer func(string, []byte) error) func() {
	store.mu.Lock()
	previous := store.atomicWrite
	if writer == nil {
		writer = writeAtomic
	}
	store.atomicWrite = writer
	store.mu.Unlock()
	return func() {
		store.mu.Lock()
		store.atomicWrite = previous
		store.mu.Unlock()
	}
}

// Close releases the state directory's cross-process exclusive lock. It is
// idempotent and marks the store unusable for further reads or writes.
func (store *Store) Close() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return nil
	}
	store.closed = true
	if store.lock == nil {
		return nil
	}
	lock := store.lock
	store.lock = nil
	return releaseStoreLock(lock)
}

// LoadJournal returns exactly one run generation journal.
func (store *Store) LoadJournal(key RunKey) (RunJournal, error) {
	if err := validateKey(key); err != nil {
		return RunJournal{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return RunJournal{}, err
	}
	return store.loadJournalLocked(key)
}

func (store *Store) mutateJournal(key RunKey, mutate func(*RunJournal) error) (RunJournal, error) {
	if err := validateKey(key); err != nil {
		return RunJournal{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return RunJournal{}, err
	}
	journal, err := store.loadJournalLocked(key)
	if err != nil {
		return RunJournal{}, err
	}
	if err := mutate(&journal); err != nil {
		return RunJournal{}, err
	}
	if err := store.saveJournalWithCapacityGuardLocked(journal); err != nil {
		return RunJournal{}, err
	}
	return journal, nil
}

func (store *Store) mutateJournalIfChanged(key RunKey, mutate func(*RunJournal) (bool, error)) (RunJournal, error) {
	if err := validateKey(key); err != nil {
		return RunJournal{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return RunJournal{}, err
	}
	journal, err := store.loadJournalLocked(key)
	if err != nil {
		return RunJournal{}, err
	}
	changed, err := mutate(&journal)
	if err != nil {
		return RunJournal{}, err
	}
	if changed {
		if err := store.saveJournalWithCapacityGuardLocked(journal); err != nil {
			return RunJournal{}, err
		}
	}
	return journal, nil
}

// mutateJournalCAS applies mutate only while the loaded journal still carries
// the exact fence; stale stays each folded site's own base error. The key was
// already matched in loadJournalLocked.
func (store *Store) mutateJournalCAS(key RunKey, fence protocol.Fence, stale error, mutate func(*RunJournal) error) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.Fence() != fence {
			return stale
		}
		return mutate(journal)
	})
}

func validHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func (store *Store) loadJournalLocked(key RunKey) (RunJournal, error) {
	var journal RunJournal
	if err := store.readRunJournalJSONWithLimit(store.journalPath(key), &journal, maxJournalFileBytes); err != nil {
		return RunJournal{}, err
	}
	if err := validateJournal(journal); err != nil {
		return RunJournal{}, errors.New("invalid run journal")
	}
	if journal.Key() != key {
		return RunJournal{}, errors.New("run journal does not match requested key")
	}
	return journal, nil
}

func (store *Store) ensureOpenLocked() error {
	if store.closed || store.lock == nil {
		return errors.New("state store is closed")
	}
	return nil
}

func (store *Store) saveJournalLocked(journal RunJournal) error {
	if err := validateJournal(journal); err != nil {
		return err
	}
	data, err := serializeRunJournalWithinLimit(journal)
	if err != nil {
		return err
	}
	if err := store.atomicWrite(store.journalPath(journal.Key()), data); err != nil {
		return fmt.Errorf("write run journal: %w", err)
	}
	return nil
}

func (store *Store) saveJournalWithCapacityGuardLocked(journal RunJournal) error {
	if err := validateJournalSaveCapacity(journal); err != nil {
		return err
	}
	return store.saveJournalLocked(journal)
}

func (store *Store) readJSON(path string, resource string, destination any) error {
	return store.readJSONWithLimit(path, resource, destination, maxStateFileBytes)
}

func (store *Store) readJSONWithLimit(path string, resource string, destination any, limit int) error {
	return store.readJSONWithLimitAndTags(path, resource, destination, limit, nil)
}

func (store *Store) readRunJournalJSONWithLimit(path string, destination any, limit int) error {
	return store.readJSONWithLimitAndTags(path, "run journal", destination, limit, reflect.TypeOf(RunJournal{}))
}

func (store *Store) readJSONWithLimitAndTags(path string, resource string, destination any, limit int, rootType reflect.Type) error {
	data, err := readLimited(path, limit)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &notFoundError{Resource: resource}
		}
		return fmt.Errorf("read %s: %w", resource, err)
	}
	return decodeJSONWithLimitAndTags(data, resource, destination, limit, rootType)
}

func decodeJSONWithLimitAndTags(data []byte, resource string, destination any, limit int, rootType reflect.Type) error {
	if len(data) > limit {
		return errors.New("read " + resource)
	}
	if err := protocol.RejectDuplicateJSONMembers(data); err != nil {
		return fmt.Errorf("decode %s: %w", resource, err)
	}
	if rootType != nil {
		if err := rejectCaseInsensitiveJSONTagAliases(data, rootType); err != nil {
			return fmt.Errorf("decode %s: %w", resource, err)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode %s: %w", resource, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return fmt.Errorf("decode %s: %w", resource, err)
		}
		return errors.New("decode " + resource)
	}
	return nil
}

// rejectCaseInsensitiveJSONTagAliases closes encoding/json's compatibility
// behavior at the durable journal boundary. encoding/json accepts keys such as
// "PROVIDER_ACTION_INTENTS" for the exact "provider_action_intents" tag;
// durable authority must not let such an alias silently win over a later
// field. Unknown exact members remain the responsibility of
// Decoder.DisallowUnknownFields, while RawMessage and map fields stay open by
// design.
func rejectCaseInsensitiveJSONTagAliases(data []byte, rootType reflect.Type) error {
	var value json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return fmt.Errorf("trailing JSON data: %w", err)
		}
		return errors.New("trailing JSON data")
	}
	return rejectJSONTagAliases(value, rootType)
}

func rejectJSONTagAliases(value json.RawMessage, valueType reflect.Type) error {
	valueType = dereferenceJSONTagType(valueType)
	if valueType == nil || valueType == reflect.TypeOf(json.RawMessage(nil)) {
		return nil
	}
	switch valueType.Kind() {
	case reflect.Array, reflect.Slice:
		var items []json.RawMessage
		if err := json.Unmarshal(value, &items); err != nil {
			return nil
		}
		for _, item := range items {
			if err := rejectJSONTagAliases(item, valueType.Elem()); err != nil {
				return err
			}
		}
	case reflect.Struct:
		fields := exactJSONTagFields(valueType)
		if len(fields) == 0 {
			return nil
		}
		var members map[string]json.RawMessage
		if err := json.Unmarshal(value, &members); err != nil {
			return nil
		}
		for member, child := range members {
			fieldType, exact := fields[member]
			if !exact {
				for tag := range fields {
					if strings.EqualFold(member, tag) {
						return fmt.Errorf("JSON member %q aliases exact tag %q", member, tag)
					}
				}
				continue
			}
			if err := rejectJSONTagAliases(child, fieldType); err != nil {
				return err
			}
		}
	}
	return nil
}

func dereferenceJSONTagType(valueType reflect.Type) reflect.Type {
	for valueType != nil && (valueType.Kind() == reflect.Pointer || valueType.Kind() == reflect.Interface) {
		valueType = valueType.Elem()
	}
	return valueType
}

func exactJSONTagFields(valueType reflect.Type) map[string]reflect.Type {
	valueType = dereferenceJSONTagType(valueType)
	if valueType == nil || valueType.Kind() != reflect.Struct {
		return nil
	}
	fields := make(map[string]reflect.Type)
	for index := 0; index < valueType.NumField(); index++ {
		field := valueType.Field(index)
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if name == "" && field.Anonymous {
			nested := exactJSONTagFields(field.Type)
			if len(nested) != 0 {
				for nestedName, nestedType := range nested {
					fields[nestedName] = nestedType
				}
				continue
			}
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
}

func (store *Store) writeJSON(path string, value any, resource string) error {
	return store.writeJSONWithLimit(path, value, resource, maxStateFileBytes)
}

func (store *Store) writeJSONWithLimit(path string, value any, resource string, limit int) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", resource, err)
	}
	if len(data) > limit {
		return errors.New("encode " + resource)
	}
	if err := store.atomicWrite(path, data); err != nil {
		return fmt.Errorf("write %s: %w", resource, err)
	}
	return nil
}

// serializeRunJournal is the sole durable representation used for run journal
// writes and byte-capacity checks. Other state files intentionally retain the
// default json.Marshal representation.
func serializeRunJournal(journal RunJournal) ([]byte, error) {
	return serializeJSONWithoutHTMLEscaping(journal)
}

func serializeJSONWithoutHTMLEscaping(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
}

func (store *Store) removeOwnedTempsLocked(entries []os.DirEntry) error {
	for _, entry := range entries {
		if entry.IsDir() || !isOwnedTemp(entry.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(store.runsDir(), entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove run journal temporary file: %w", err)
		}
	}
	return nil
}

func (store *Store) runsDir() string {
	return filepath.Join(store.dir, runsDirectoryName)
}

func (store *Store) journalPath(key RunKey) string {
	input := key.RunID + "\x00" + strconv.FormatInt(key.Generation, 10)
	digest := sha256.Sum256([]byte(input))
	return filepath.Join(store.runsDir(), journalFilePrefix+hex.EncodeToString(digest[:])+journalFileSuffix)
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return applyDirectorySecurity(path)
}

func readLimited(path string, limit int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("state file exceeds size limit")
	}
	return data, nil
}

func writeAtomic(path string, data []byte) (err error) {
	directory := filepath.Dir(path)
	var temporary *os.File
	var temporaryPath string
	for range 16 {
		suffix, randomErr := randomHex(16)
		if randomErr != nil {
			return randomErr
		}
		temporaryPath = filepath.Join(directory, atomicTempPrefix+suffix+".tmp")
		temporary, err = os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if temporary == nil {
		return errors.New("create temporary state file")
	}
	defer func() {
		if temporary != nil {
			_ = temporary.Close()
		}
		if temporaryPath != "" {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := applyFileSecurity(temporaryPath); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	temporary = nil
	if err := renameStateFile(temporaryPath, path); err != nil {
		return err
	}
	temporaryPath = ""
	return syncDirectory(directory)
}

func randomHex(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func isJournalFile(name string) bool {
	if !strings.HasPrefix(name, journalFilePrefix) || !strings.HasSuffix(name, journalFileSuffix) {
		return false
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(name, journalFilePrefix), journalFileSuffix)
	if len(digest) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func isOwnedTemp(name string) bool {
	if !strings.HasPrefix(name, atomicTempPrefix) || !strings.HasSuffix(name, ".tmp") {
		return false
	}
	suffix := strings.TrimSuffix(strings.TrimPrefix(name, atomicTempPrefix), ".tmp")
	if len(suffix) != 32 {
		return false
	}
	_, err := hex.DecodeString(suffix)
	return err == nil
}

func validateKey(key RunKey) error {
	if strings.TrimSpace(key.RunID) == "" || len(key.RunID) > 4096 || key.Generation <= 0 {
		return errors.New("run key is invalid")
	}
	return nil
}

func validRequiredString(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit
}

func validWork(work protocol.Work) bool {
	return len(work.Goal) <= 65536 && len(work.AgentProfile) <= 4096 && len(work.Workspace) <= 4096 && validRawMessage(work.Input)
}

func sameWork(left, right protocol.Work) bool {
	return left.Goal == right.Goal && left.AgentProfile == right.AgentProfile && left.Workspace == right.Workspace &&
		bytes.Equal(left.Input, right.Input)
}

func validRawMessage(message json.RawMessage) bool {
	return len(message) == 0 || (json.Valid(message) && protocol.RejectDuplicateJSONMembers(message) == nil)
}

func isZeroFence(fence protocol.Fence) bool {
	return fence == (protocol.Fence{})
}

func sameFence(left, right protocol.Fence) bool {
	return left == right
}

func makeStringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
