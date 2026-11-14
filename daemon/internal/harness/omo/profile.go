package omo

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

var ErrResumeRejected = errors.New("OmO native resume rejected")

type ResumeRejectedError struct{ Cause error }

func (err *ResumeRejectedError) Error() string {
	if err == nil || err.Cause == nil {
		return ErrResumeRejected.Error()
	}
	return fmt.Sprintf("%s: %v", ErrResumeRejected, err.Cause)
}

func (err *ResumeRejectedError) Unwrap() []error {
	if err == nil || err.Cause == nil {
		return []error{ErrResumeRejected}
	}
	return []error{ErrResumeRejected, err.Cause}
}

func resumeRejected(err error) error { return &ResumeRejectedError{Cause: err} }

// ValidateRPCProfileArgs prevents profiles from changing transport, retained
// session identity, permission policy or extension loading. Directory provenance
// is explicit rather than dependent on native global or project settings.
func ValidateRPCProfileArgs(args []string) error {
	for _, value := range args {
		if strings.ContainsRune(value, 0) {
			return errors.New("OmO profile argument contains NUL")
		}
	}
	directoryCount := 0
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--provider", "--model", "--thinking", "--session-dir", "--name":
			if index+1 == len(args) || strings.TrimSpace(args[index+1]) == "" || strings.HasPrefix(args[index+1], "-") {
				return fmt.Errorf("OmO profile option at argument %d requires a value", index+1)
			}
			if args[index] == "--session-dir" {
				directoryCount++
				if !filepath.IsAbs(args[index+1]) {
					return errors.New("OmO session directory must be absolute")
				}
			}
			index++
		default:
			return fmt.Errorf("OmO profile argument %d is not allowed for classic RPC", index+1)
		}
	}
	if directoryCount != 1 {
		return errors.New("OmO profile requires exactly one absolute --session-dir")
	}
	return nil
}

func profileSessionDirectory(args []string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "--session-dir" {
			return args[index+1]
		}
	}
	return ""
}

func rpcArgs(profile []string, resume *SessionState) ([]string, error) {
	if err := ValidateRPCProfileArgs(profile); err != nil {
		return nil, err
	}
	args := make([]string, 0, len(profile)+14)
	args = append(args, "--mode", "rpc", "--no-extensions", "--no-skills", "--no-context-files", "--no-prompt-templates", "--no-themes", "--permission-preset", "workspace", "--tools", "read,write")
	if resume != nil {
		args = append(args, "--session", resume.SessionFile)
	}
	return append(args, profile...), nil
}

func resumeState(resume *harness.ResumeHandle, args []string, workspace string) (*SessionState, error) {
	if resume == nil {
		return nil, nil
	}
	reject := func(err error) (*SessionState, error) { return nil, resumeRejected(err) }
	if !validIdentity(resume.LocalHandleID) || !validIdentity(resume.NativeSessionID) || strings.TrimSpace(resume.WorkspaceFingerprint) == "" {
		return reject(errors.New("retained identity or workspace fingerprint is missing"))
	}
	if resume.NativeVersion != TestedVersion {
		return reject(harness.ErrUnsupportedVersion)
	}
	if err := ValidateRPCProfileArgs(args); err != nil {
		return reject(err)
	}
	if err := validateSessionPath(resume.NativeSessionFilename, profileSessionDirectory(args), false); err != nil {
		return reject(err)
	}
	if err := validateSessionHeader(resume.NativeSessionFilename, resume.NativeSessionID, workspace); err != nil {
		return reject(err)
	}
	return &SessionState{SessionID: resume.NativeSessionID, SessionFile: resume.NativeSessionFilename}, nil
}

// This validates path containment at admission and again after native Open.
// Native OmO reopens a pathname, not a held descriptor; filesystem writers can
// still replace it later. The OS execution owner remains the containment owner.
func validateSessionPath(filename, directory string, allowMissingLeaf bool) error {
	if !filepath.IsAbs(filename) || !filepath.IsAbs(directory) || strings.ContainsRune(filename, 0) {
		return errors.New("OmO session paths must be absolute")
	}
	resolvedDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return fmt.Errorf("resolve OmO session directory: %w", err)
	}
	directoryInfo, err := os.Stat(resolvedDirectory)
	if err != nil || !directoryInfo.IsDir() {
		return errors.New("OmO session directory is not available")
	}
	resolvedFile, err := filepath.EvalSymlinks(filename)
	if err != nil && allowMissingLeaf && errors.Is(err, os.ErrNotExist) {
		if _, leafErr := os.Lstat(filename); !errors.Is(leafErr, os.ErrNotExist) {
			return errors.New("OmO fresh session leaf is not an absent regular path")
		}
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(filename))
		if parentErr != nil {
			return parentErr
		}
		resolvedFile, err = filepath.Join(parent, filepath.Base(filename)), nil
	}
	if err != nil {
		return fmt.Errorf("resolve retained OmO session: %w", err)
	}
	relative, err := filepath.Rel(resolvedDirectory, resolvedFile)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("OmO session file escapes its configured session directory")
	}
	info, err := os.Stat(filename)
	if allowMissingLeaf && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("OmO retained session must be a regular file")
	}
	return nil
}

func validateSessionHeader(filename, sessionID, workspace string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, defaultMaxRecordBytes+1)
	line, err := reader.ReadSlice('\n')
	if err != nil || len(line) > defaultMaxRecordBytes {
		return errors.New("OmO retained session header is missing or unbounded")
	}
	header, err := strictObject(line)
	if err != nil {
		return err
	}
	var kind, id, cwd string
	var version int
	if json.Unmarshal(header["version"], &version) != nil || version != 3 {
		return errors.New("OmO retained session header version differs")
	}
	if !nonEmptyString(header["type"], &kind) || kind != "session" || !nonEmptyString(header["id"], &id) || id != sessionID || !nonEmptyString(header["cwd"], &cwd) || !filepath.IsAbs(cwd) {
		return errors.New("OmO retained session header identity differs")
	}
	nativeWorkspace, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return err
	}
	expectedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return err
	}
	nativeInfo, err := os.Stat(nativeWorkspace)
	if err != nil {
		return err
	}
	expectedInfo, err := os.Stat(expectedWorkspace)
	if err != nil {
		return err
	}
	if !os.SameFile(nativeInfo, expectedInfo) {
		return errors.New("OmO retained session workspace differs")
	}
	return nil
}
