//go:build windows

package state

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"syscall"
	"unsafe"
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
	lockViolation           = syscall.Errno(33)

	daclSecurityInformation       = 0x00000004
	securityDescriptorRevision    = 1
	securityDescriptorDACLProtect = 0x1000
	aclRevision                   = 2
	accessAllowedACEType          = 0
	genericAll                    = 0x10000000
	fileAllAccess                 = 0x001f01ff
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx         = kernel32.NewProc("LockFileEx")
	procUnlockFileEx       = kernel32.NewProc("UnlockFileEx")
	advapi32               = syscall.NewLazyDLL("advapi32.dll")
	procInitializeSecurity = advapi32.NewProc("InitializeSecurityDescriptor")
	procSetSecurityDACL    = advapi32.NewProc("SetSecurityDescriptorDacl")
	procSetSecurityControl = advapi32.NewProc("SetSecurityDescriptorControl")
	procSetFileSecurity    = advapi32.NewProc("SetFileSecurityW")
	procGetFileSecurity    = advapi32.NewProc("GetFileSecurityW")
	procGetSecurityDACL    = advapi32.NewProc("GetSecurityDescriptorDacl")
	procGetACE             = advapi32.NewProc("GetAce")
)

type windowsSecurityDescriptor struct {
	revision byte
	sbz1     byte
	control  uint16
	owner    unsafe.Pointer
	group    unsafe.Pointer
	sacl     unsafe.Pointer
	dacl     unsafe.Pointer
}

func acquireStoreLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, errors.New("open state directory lock")
	}
	var overlapped syscall.Overlapped
	result, _, callErr := procLockFileEx.Call(
		file.Fd(),
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result != 0 {
		return file, nil
	}
	_ = file.Close()
	if errors.Is(callErr, lockViolation) {
		return nil, ErrStoreInUse
	}
	return nil, errors.New("acquire state directory lock")
}

func releaseStoreLock(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, _ := procUnlockFileEx.Call(file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	closeErr := file.Close()
	if result == 0 || closeErr != nil {
		return errors.New("release state directory lock")
	}
	return nil
}

func secureDirectory(path string) error {
	return secureWindowsPath(path)
}

func secureFile(path string) error {
	return secureWindowsPath(path)
}

func verifyPrivateDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return errors.New("directory permissions are not private")
	}
	return verifyWindowsPrivatePath(path)
}

func verifyPrivateFile(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("file permissions are not private")
	}
	return verifyWindowsPrivatePath(path)
}

func secureWindowsPath(path string) error {
	sid, err := currentUserSID()
	if err != nil {
		return errors.New("get current account SID")
	}
	acl, err := accountOnlyDACL(sid)
	if err != nil {
		return errors.New("create account-only DACL")
	}
	var descriptor windowsSecurityDescriptor
	if err := windowsBool(procInitializeSecurity, uintptr(unsafe.Pointer(&descriptor)), securityDescriptorRevision); err != nil {
		return errors.New("initialize security descriptor")
	}
	if err := windowsBool(procSetSecurityDACL, uintptr(unsafe.Pointer(&descriptor)), 1, uintptr(unsafe.Pointer(&acl[0])), 0); err != nil {
		return errors.New("set security descriptor DACL")
	}
	if err := windowsBool(procSetSecurityControl, uintptr(unsafe.Pointer(&descriptor)), securityDescriptorDACLProtect, securityDescriptorDACLProtect); err != nil {
		return errors.New("protect security descriptor DACL")
	}
	pathPointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return errors.New("encode state path")
	}
	if err := windowsBool(procSetFileSecurity, uintptr(unsafe.Pointer(pathPointer)), daclSecurityInformation, uintptr(unsafe.Pointer(&descriptor))); err != nil {
		return errors.New("apply account-only DACL")
	}
	return verifyWindowsPrivatePath(path)
}

func verifyWindowsPrivatePath(path string) error {
	sid, err := currentUserSID()
	if err != nil {
		return errors.New("get current account SID")
	}
	descriptor, err := readWindowsDACL(path)
	if err != nil {
		return errors.New("read state DACL")
	}
	var present uint32
	var defaulted uint32
	var dacl unsafe.Pointer
	if err := windowsBool(procGetSecurityDACL, uintptr(unsafe.Pointer(&descriptor[0])), uintptr(unsafe.Pointer(&present)), uintptr(unsafe.Pointer(&dacl)), uintptr(unsafe.Pointer(&defaulted))); err != nil || present == 0 || dacl == nil {
		return errors.New("state DACL is not private")
	}
	aclHeader := unsafe.Slice((*byte)(dacl), 8)
	if aclHeader[0] != aclRevision || binary.LittleEndian.Uint16(aclHeader[4:6]) != 1 {
		return errors.New("state DACL header is not account-only")
	}
	var ace unsafe.Pointer
	if err := windowsBool(procGetACE, uintptr(dacl), 0, uintptr(unsafe.Pointer(&ace))); err != nil || ace == nil {
		return errors.New("state DACL ACE is unavailable")
	}
	aceHeader := unsafe.Slice((*byte)(ace), 8)
	sidLength := sid.Len()
	if aceHeader[0] != accessAllowedACEType {
		return errors.New("state DACL ACE type is not account-only")
	}
	if binary.LittleEndian.Uint16(aceHeader[2:4]) != uint16(8+sidLength) {
		return errors.New("state DACL ACE size is not account-only")
	}
	accessMask := binary.LittleEndian.Uint32(aceHeader[4:8])
	if accessMask != genericAll && accessMask != fileAllAccess {
		return errors.New("state DACL ACE access is not account-only")
	}
	aceSID := unsafe.Slice((*byte)(unsafe.Add(ace, 8)), sidLength)
	currentSID := unsafe.Slice((*byte)(unsafe.Pointer(sid)), sidLength)
	if !bytes.Equal(aceSID, currentSID) {
		return errors.New("state DACL account is not current daemon account")
	}
	return nil
}

func currentUserSID() (*syscall.SID, error) {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid.Copy()
}

func accountOnlyDACL(sid *syscall.SID) ([]byte, error) {
	sidLength := sid.Len()
	aclLength := 8 + 8 + sidLength
	if aclLength > int(^uint16(0)) {
		return nil, errors.New("ACL is too large")
	}
	acl := make([]byte, aclLength)
	acl[0] = aclRevision
	binary.LittleEndian.PutUint16(acl[2:4], uint16(aclLength))
	binary.LittleEndian.PutUint16(acl[4:6], 1)
	acl[8] = accessAllowedACEType
	binary.LittleEndian.PutUint16(acl[10:12], uint16(8+sidLength))
	binary.LittleEndian.PutUint32(acl[12:16], genericAll)
	copy(acl[16:], unsafe.Slice((*byte)(unsafe.Pointer(sid)), sidLength))
	return acl, nil
}

func readWindowsDACL(path string) ([]byte, error) {
	pathPointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var needed uint32
	result, _, _ := procGetFileSecurity.Call(uintptr(unsafe.Pointer(pathPointer)), daclSecurityInformation, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if result != 0 || needed == 0 {
		return nil, errors.New("query state DACL size")
	}
	descriptor := make([]byte, needed)
	result, _, _ = procGetFileSecurity.Call(uintptr(unsafe.Pointer(pathPointer)), daclSecurityInformation, uintptr(unsafe.Pointer(&descriptor[0])), uintptr(len(descriptor)), uintptr(unsafe.Pointer(&needed)))
	if result == 0 {
		return nil, errors.New("query state DACL")
	}
	return descriptor, nil
}

func windowsBool(procedure *syscall.LazyProc, arguments ...uintptr) error {
	result, _, _ := procedure.Call(arguments...)
	if result == 0 {
		return errors.New("Windows API call failed")
	}
	return nil
}

func syncDirectory(string) error {
	// Windows does not support flushing a directory handle through os.File.Sync.
	return nil
}
