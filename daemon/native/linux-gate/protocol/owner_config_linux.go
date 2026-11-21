//go:build linux && amd64

package protocol

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	OwnerConfigMaxBytes   = 8192
	ownerConfigPath       = "/etc/symmetry/linux-owner.conf"
	maxOwnerConfigBytes   = OwnerConfigMaxBytes
	maxOwnerStateDirBytes = 4095
)

// ReadInstalledOwnerConfig reads the strict, root-owned, non-writable
// operator configuration. Its IDs and optional canonical StateDir are not secrets.
func ReadInstalledOwnerConfig() (OwnerConfig, error) {
	for _, path := range []string{"/etc", "/etc/symmetry"} {
		fd, err := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return OwnerConfig{}, fmt.Errorf("open fixed Linux owner config directory %q: %w", path, err)
		}
		var directory unix.Stat_t
		statErr := unix.Fstat(fd, &directory)
		closeErr := unix.Close(fd)
		if statErr != nil || closeErr != nil || directory.Mode&unix.S_IFMT != unix.S_IFDIR || directory.Uid != 0 || directory.Gid != 0 || directory.Mode&0022 != 0 {
			return OwnerConfig{}, errors.Join(fmt.Errorf("fixed Linux owner config directory %q is not root-owned and non-writable", path), statErr, closeErr)
		}
	}
	fd, err := unix.Open(ownerConfigPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return OwnerConfig{}, fmt.Errorf("open fixed Linux owner config: %w", err)
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return OwnerConfig{}, fmt.Errorf("stat fixed Linux owner config: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Gid != 0 ||
		stat.Mode&0022 != 0 || stat.Nlink != 1 || stat.Size <= 0 || stat.Size > maxOwnerConfigBytes {
		return OwnerConfig{}, errors.New("fixed Linux owner config has unsafe ownership, mode, link count, or size")
	}
	content := make([]byte, stat.Size+1)
	total := 0
	for total < len(content) {
		n, readErr := unix.Read(fd, content[total:])
		if readErr != nil {
			return OwnerConfig{}, fmt.Errorf("read fixed Linux owner config: %w", readErr)
		}
		if n == 0 {
			break
		}
		total += n
	}
	if total != int(stat.Size) {
		return OwnerConfig{}, errors.New("fixed Linux owner config changed while reading")
	}
	return parseOwnerConfig(content[:total])
}

func parseOwnerConfig(data []byte) (OwnerConfig, error) {
	lines := strings.Split(string(data), "\n")
	legacy := len(lines) == 6 && lines[5] == "" && lines[0] == "version=1"
	withStateDir := len(lines) == 7 && lines[6] == "" && lines[0] == "version=2"
	if !legacy && !withStateDir {
		return OwnerConfig{}, errors.New("fixed Linux owner config has an invalid version or shape")
	}
	values := [4]uint32{}
	for index, key := range []string{"a_uid", "a_gid", "b_uid", "b_gid"} {
		prefix := key + "="
		if !strings.HasPrefix(lines[index+1], prefix) {
			return OwnerConfig{}, fmt.Errorf("fixed Linux owner config field %q is missing or reordered", key)
		}
		text := strings.TrimPrefix(lines[index+1], prefix)
		parsed, err := strconv.ParseUint(text, 10, 32)
		if err != nil || text == "" || strconv.FormatUint(parsed, 10) != text {
			return OwnerConfig{}, fmt.Errorf("fixed Linux owner config field %q is not a canonical uint32", key)
		}
		values[index] = uint32(parsed)
	}
	config := OwnerConfig{AUID: values[0], AGID: values[1], BUID: values[2], BGID: values[3]}
	if withStateDir {
		const prefix = "state_dir="
		if !strings.HasPrefix(lines[5], prefix) {
			return OwnerConfig{}, errors.New("fixed Linux owner config state_dir field is missing or reordered")
		}
		config.StateDir = strings.TrimPrefix(lines[5], prefix)
		if err := validateOwnerStateDir(config.StateDir); err != nil {
			return OwnerConfig{}, err
		}
	}
	if err := config.Validate(); err != nil {
		return OwnerConfig{}, err
	}
	return config, nil
}

// Validate rejects identities which would collapse the host-UID or primary
// group boundary.
func (config OwnerConfig) Validate() error {
	if config.AUID == 0 || config.BUID == 0 || config.AUID == config.BUID {
		return errors.New("Linux owner A and fixed native B must be distinct non-root UIDs")
	}
	if config.AGID == 0 || config.BGID == 0 || config.AGID == config.BGID {
		return errors.New("Linux owner A and fixed native B must be distinct non-root primary GIDs")
	}
	if config.StateDir != "" {
		if err := validateOwnerStateDir(config.StateDir); err != nil {
			return err
		}
	}
	return nil
}

func validateOwnerStateDir(stateDir string) error {
	if stateDir == "" || len(stateDir) > maxOwnerStateDirBytes || !filepath.IsAbs(stateDir) ||
		filepath.Clean(stateDir) != stateDir || stateDir == string(filepath.Separator) {
		return errors.New("fixed Linux owner StateDir must be a canonical absolute non-root path")
	}
	for index := 0; index < len(stateDir); index++ {
		if stateDir[index] < 0x20 || stateDir[index] == 0x7f {
			return errors.New("fixed Linux owner StateDir contains a control character")
		}
	}
	for _, component := range strings.Split(strings.TrimPrefix(stateDir, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" || component == "." || component == ".." {
			return errors.New("fixed Linux owner StateDir contains an unsafe path component")
		}
	}
	return nil
}
