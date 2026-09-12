package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

var (
	// ErrInvalidArtifactPath means that an artifact path is not a safe,
	// repository-relative path.
	ErrInvalidArtifactPath = errors.New("invalid subject artifact path")
	// ErrArtifactNotFound means that the exact path is not a committed blob in
	// the requested Subject commit.
	ErrArtifactNotFound = errors.New("subject artifact not found")
	// ErrArtifactNotBlob means that the exact committed path is not a blob.
	ErrArtifactNotBlob = errors.New("subject artifact is not a blob")
)

// SubjectArtifact is content read from the Git object database for one exact
// Subject commit. Content is returned as-is, including binary bytes and any
// missing trailing newline.
type SubjectArtifact struct {
	Path          string
	Content       []byte
	ContentDigest string
}

// ReadSubjectArtifact reads one repository-relative artifact from the exact
// Git object database bound by subject and returns its SHA-256 digest. It does
// not read the configured checkout, so mutable working-tree changes cannot
// affect the result.
func (manager *Manager) ReadSubjectArtifact(ctx context.Context, bindingKey string, subject protocol.Subject, path string) (SubjectArtifact, error) {
	if manager == nil {
		return SubjectArtifact{}, errors.New("workspace manager is nil")
	}
	if err := validateSubjectContext(ctx); err != nil {
		return SubjectArtifact{}, err
	}
	if err := validateSubjectArtifactPath(path); err != nil {
		return SubjectArtifact{}, fmt.Errorf("validate artifact path %q: %w", path, err)
	}
	if err := subject.Validate(); err != nil {
		return SubjectArtifact{}, &SubjectError{
			Code:       SubjectErrorInvalid,
			BindingKey: bindingKey,
			Path:       path,
			Commit:     subject.Commit,
			Cause:      err,
		}
	}

	_, repository, err := manager.subjectBinding(ctx, bindingKey)
	if err != nil {
		return SubjectArtifact{}, err
	}
	verified, err := manager.materializeAtRepository(ctx, bindingKey, repository, subject.ResourceID, subject.Commit)
	if err != nil {
		return SubjectArtifact{}, err
	}
	if verified != subject {
		return SubjectArtifact{}, subjectMismatch(bindingKey, subject.Commit, subjectDigestDescription(subject), subjectDigestDescription(verified))
	}

	objectID, err := gitSubjectArtifactObjectID(ctx, repository, subject.Commit, path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SubjectArtifact{}, ctxErr
		}
		return SubjectArtifact{}, &SubjectError{
			Code:       SubjectErrorMismatch,
			BindingKey: bindingKey,
			Path:       path,
			Commit:     subject.Commit,
			Cause:      err,
		}
	}
	content, err := gitBlobContent(ctx, repository, objectID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SubjectArtifact{}, ctxErr
		}
		return SubjectArtifact{}, fmt.Errorf("read committed artifact %q: %w", path, err)
	}
	digest := sha256.Sum256(content)
	return SubjectArtifact{
		Path:          path,
		Content:       content,
		ContentDigest: "sha256:" + hex.EncodeToString(digest[:]),
	}, nil
}

func validateSubjectArtifactPath(path string) error {
	// SourceRef.Validate is the existing daemon protocol boundary for the v1
	// CommitPath contract. Keep this helper aligned with that contract instead
	// of applying host filepath or Git pathspec rules to the wire value.
	source := protocol.SourceRef{
		Kind: protocol.EvidenceArtifact,
		Ref:  "artifact",
		Path: &path,
	}
	if err := source.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidArtifactPath, err)
	}
	return nil
}

func gitSubjectArtifactObjectID(ctx context.Context, repository, commit, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Enumerate the complete committed tree and compare raw record paths. A
	// caller path must never be passed to Git as a pathspec: pathspec semantics
	// can normalize or reinterpret values such as ./x, x/, and colon-prefixed
	// paths before the exact artifact identity check.
	command := gitNoReplaceObjectsCommand(ctx, repository, "ls-tree", "--full-tree", "-r", "-z", commit)
	output, err := gitCommandOutputLimited(ctx, command, maxSubjectTreeOutputBytes, ErrSubjectTreeTooLarge)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("list committed artifact %q: %w", path, err)
	}

	var objectID string
	records := bytes.Split(output, []byte{0})
	for index, record := range records {
		if len(record) == 0 {
			if index == len(records)-1 {
				continue
			}
			return "", fmt.Errorf("committed artifact listing contains an empty entry at index %d", index)
		}
		tab := bytes.IndexByte(record, '\t')
		if tab <= 0 || tab == len(record)-1 {
			return "", fmt.Errorf("committed artifact entry %d has invalid NUL record", index)
		}
		fields := bytes.Fields(record[:tab])
		if len(fields) != 3 || len(fields[0]) == 0 || len(fields[1]) == 0 || len(fields[2]) == 0 {
			return "", fmt.Errorf("committed artifact entry %d has invalid header", index)
		}
		if !bytes.Equal(record[tab+1:], []byte(path)) {
			continue
		}
		if string(fields[1]) != "blob" {
			return "", fmt.Errorf("committed artifact %q: %w", path, ErrArtifactNotBlob)
		}
		if objectID != "" {
			return "", fmt.Errorf("committed artifact %q has duplicate tree entries", path)
		}
		objectID = string(fields[2])
	}
	if objectID == "" {
		return "", fmt.Errorf("committed artifact %q: %w", path, ErrArtifactNotFound)
	}
	return objectID, nil
}
