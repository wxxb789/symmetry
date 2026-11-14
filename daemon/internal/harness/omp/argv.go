package omp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

var ErrResumeRejected = errors.New("OMP resume rejected")

// ValidateRPCProfileArgs accepts only audited configuration selectors. The
// private session directory must be explicit; profiles cannot select transport,
// native commands, credentials, permission bypasses, or executable extensions.
func ValidateRPCProfileArgs(args []string) error {
	_, err := profileDirectory(args)
	return err
}

func profileDirectory(args []string) (string, error) {
	seen := make(map[string]bool)
	directory := ""
	for index := 0; index < len(args); index++ {
		name, value, equals := strings.Cut(args[index], "=")
		if seen[name] {
			return "", fmt.Errorf("duplicate OMP profile option %q", name)
		}
		seen[name] = true
		switch name {
		case "--no-extensions", "--no-skills", "--no-rules", "--no-context-files", "--no-lsp", "--no-pty", "--no-prewalk", "--no-title", "--no-ui":
			if equals {
				return "", fmt.Errorf("OMP option %q takes no value", name)
			}
			continue
		case "--model", "--thinking", "--session-dir", "--tools":
		default:
			return "", fmt.Errorf("OMP profile option %q is not audited", name)
		}
		if !equals {
			index++
			if index >= len(args) {
				return "", fmt.Errorf("OMP option %q requires a value", name)
			}
			value = args[index]
		}
		if strings.TrimSpace(value) == "" || strings.HasPrefix(value, "-") || strings.ContainsAny(value, "\x00\r\n") {
			return "", fmt.Errorf("invalid OMP option %q value", name)
		}
		switch name {
		case "--session-dir":
			if !filepath.IsAbs(value) || filepath.Clean(value) == filepath.VolumeName(value)+string(filepath.Separator) {
				return "", errors.New("OMP session-dir must be an absolute private directory")
			}
			directory = filepath.Clean(value)
		case "--tools":
			tools := make(map[string]bool)
			for _, tool := range strings.Split(value, ",") {
				if tools[tool] || (tool != "read" && tool != "edit" && tool != "write" && tool != "bash") {
					return "", errors.New("OMP tools must be a subset of read,edit,write,bash")
				}
				tools[tool] = true
			}
		case "--thinking":
			switch value {
			case "auto", "off", "minimal", "low", "medium", "high", "xhigh", "max":
			default:
				return "", errors.New("invalid OMP thinking selector")
			}
		}
	}
	if directory == "" {
		return "", errors.New("OMP profile requires explicit absolute private --session-dir")
	}
	return directory, nil
}

func privateDirectory(directory string) (string, error) {
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", fmt.Errorf("resolve OMP session directory: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return "", errors.New("OMP session directory must be private")
	}
	return resolved, nil
}

// A fresh native session file may not exist until the first transcript write.
// The retained-resume boundary, unlike fresh Open, requires the file to exist.
func sessionFilename(directory, filename string, retained bool) (string, error) {
	if !filepath.IsAbs(filename) {
		return "", errors.New("OMP returned a non-absolute session filename")
	}
	path := filepath.Clean(filename)
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	path = filepath.Join(parent, filepath.Base(path))
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return "", errors.New("OMP session file must be a regular retained file")
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
	} else if retained || !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	relative, err := filepath.Rel(directory, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("OMP session filename is outside configured session-dir")
	}
	return path, nil
}

func resumeFilename(resume *harness.ResumeHandle, directory string) (string, error) {
	if resume == nil {
		return "", nil
	}
	if resume.NativeVersion != TestedVersion || strings.TrimSpace(resume.NativeSessionID) == "" {
		return "", fmt.Errorf("%w: incompatible retained identity", ErrResumeRejected)
	}
	filename, err := sessionFilename(directory, resume.NativeSessionFilename, true)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrResumeRejected, err)
	}
	return filename, nil
}

func rpcArgs(profile []string, retained string) []string {
	args := append([]string(nil), profile...)
	for _, flag := range []string{"--no-ui", "--no-prewalk"} {
		if !slices.Contains(profile, flag) {
			args = append(args, flag)
		}
	}
	args = append(args, "--mode", "rpc")
	if retained != "" {
		args = append(args, "--resume", retained)
	}
	return args
}
