//go:build !windows

package state

import (
	"errors"
	"os"
	"syscall"
)

func acquireStoreLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, errors.New("open state directory lock")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrStoreInUse
		}
		return nil, errors.New("acquire state directory lock")
	}
	return file, nil
}

func releaseStoreLock(file *os.File) error {
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil || closeErr != nil {
		return errors.New("release state directory lock")
	}
	return nil
}

func secureDirectory(path string) error {
	if err := os.Chmod(path, 0o700); err != nil {
		return errors.New("set directory permissions")
	}
	return verifyPrivateDirectory(path)
}

func secureFile(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return errors.New("set file permissions")
	}
	return verifyPrivateFile(path)
}

func verifyPrivateDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("directory permissions are not private")
	}
	return nil
}

func verifyPrivateFile(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("file permissions are not private")
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
