//go:build windows

package platform

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const (
	directoryPathInitialUTF16Units = 260
	directoryPathMaximumUTF16Units = 32768
)

func rawDirectoryPathIsAbsolute(path string) bool {
	// Classify separators without changing the raw path sent to CreateFile.
	path = strings.ReplaceAll(path, "/", `\`)
	if strings.HasPrefix(path, `\\?\`) {
		path = path[len(`\\?\`):]
		if strings.HasPrefix(path, `UNC\`) {
			return isCompleteWindowsUNCPath(path[len(`UNC\`):])
		}
		return len(path) >= 3 && isWindowsDriveLetter(path[0]) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
	}
	if strings.HasPrefix(path, `\\`) {
		return isCompleteWindowsUNCPath(path[len(`\\`):])
	}
	return len(path) >= 3 && isWindowsDriveLetter(path[0]) && path[1] == ':' && path[2] == '\\'
}

func resolveExistingDirectory(path string) (resolved string, err error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", fmt.Errorf("encode directory path: %w", err)
	}

	handle, err := windows.CreateFile(
		pathPointer,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", fmt.Errorf("open directory: %w", err)
	}
	defer func() {
		if closeErr := windows.CloseHandle(handle); closeErr != nil {
			resolved = ""
			err = errors.Join(err, fmt.Errorf("close directory handle: %w", closeErr))
		}
	}()

	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		return "", fmt.Errorf("inspect directory handle: %w", err)
	}
	if information.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return "", errors.New("resolved path is not a directory")
	}

	resolved, err = finalWindowsDirectoryPath(handle)
	if err != nil {
		return "", err
	}
	if !isKnownFinalWindowsDirectoryPath(resolved) {
		return "", errors.New("resolved directory uses an unsupported Windows namespace")
	}
	return resolved, nil
}

func finalWindowsDirectoryPath(handle windows.Handle) (string, error) {
	for size := uint32(directoryPathInitialUTF16Units); size <= directoryPathMaximumUTF16Units; {
		buffer := make([]uint16, size)
		// FILE_NAME_NORMALIZED | VOLUME_NAME_DOS is zero.
		count, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], size, 0)
		if err != nil || count == 0 {
			if err == nil {
				err = errors.New("GetFinalPathNameByHandle returned zero length")
			}
			return "", fmt.Errorf("resolve directory handle path: %w", err)
		}
		if count < size {
			path, err := strictUTF16String(buffer[:count])
			if err != nil {
				return "", fmt.Errorf("decode directory handle path: %w", err)
			}
			return path, nil
		}
		if count <= size || count > directoryPathMaximumUTF16Units {
			return "", errors.New("GetFinalPathNameByHandle returned an invalid required buffer length")
		}
		size = count
	}
	return "", errors.New("GetFinalPathNameByHandle path exceeds the maximum supported length")
}

func strictUTF16String(units []uint16) (string, error) {
	var builder strings.Builder
	builder.Grow(len(units))
	for index := 0; index < len(units); index++ {
		unit := units[index]
		switch {
		case unit == 0:
			return "", errors.New("unexpected NUL")
		case 0xD800 <= unit && unit <= 0xDBFF:
			if index+1 == len(units) || units[index+1] < 0xDC00 || units[index+1] > 0xDFFF {
				return "", errors.New("unpaired high surrogate")
			}
			builder.WriteRune(utf16.DecodeRune(rune(unit), rune(units[index+1])))
			index++
		case 0xDC00 <= unit && unit <= 0xDFFF:
			return "", errors.New("unpaired low surrogate")
		default:
			builder.WriteRune(rune(unit))
		}
	}
	return builder.String(), nil
}

func isKnownFinalWindowsDirectoryPath(path string) bool {
	if strings.HasPrefix(path, `\\?\UNC\`) {
		return isCompleteWindowsUNCPath(path[len(`\\?\UNC\`):])
	}
	return len(path) >= len(`\\?\C:\`) &&
		strings.HasPrefix(path, `\\?\`) &&
		isWindowsDriveLetter(path[4]) && path[5] == ':' && path[6] == '\\'
}

func isCompleteWindowsUNCPath(path string) bool {
	serverEnd := strings.IndexByte(path, '\\')
	if serverEnd <= 0 {
		return false
	}
	share := path[serverEnd+1:]
	if share == "" {
		return false
	}
	if shareEnd := strings.IndexByte(share, '\\'); shareEnd >= 0 {
		return shareEnd > 0
	}
	return true
}

func isWindowsDriveLetter(value byte) bool {
	return 'a' <= value && value <= 'z' || 'A' <= value && value <= 'Z'
}
