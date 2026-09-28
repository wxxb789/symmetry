//go:build linux && amd64

package main

import (
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

type nativeExecVector struct {
	path [1]byte
	argv []*byte
	env  []*byte
}

func prepareNativeExecVector(argv, env []string) (nativeExecVector, error) {
	var vector nativeExecVector
	if len(argv) == 0 {
		return vector, syscall.EINVAL
	}
	var err error
	vector.argv, err = syscall.SlicePtrFromStrings(argv)
	if err != nil {
		return nativeExecVector{}, err
	}
	vector.env, err = syscall.SlicePtrFromStrings(env)
	if err != nil {
		return nativeExecVector{}, err
	}
	return vector, nil
}

func execNativeImage(fd int, vector *nativeExecVector) error {
	if vector == nil || len(vector.argv) == 0 {
		return syscall.EINVAL
	}
	_, _, errno := unix.RawSyscall6(
		unix.SYS_EXECVEAT,
		uintptr(fd),
		uintptr(unsafe.Pointer(&vector.path[0])),
		uintptr(unsafe.Pointer(&vector.argv[0])),
		uintptr(unsafe.Pointer(&vector.env[0])),
		uintptr(unix.AT_EMPTY_PATH),
		0,
	)
	runtime.KeepAlive(vector)
	if errno != 0 {
		return errno
	}
	return syscall.EIO
}
