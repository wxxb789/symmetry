//go:build linux && amd64

package linuxpreflight

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

type ACLSubject struct {
	UID    uint32
	GID    uint32
	Groups []uint32
}

func (subject ACLSubject) hasGroup(gid uint32) bool {
	if subject.GID == gid {
		return true
	}
	for _, group := range subject.Groups {
		if group == gid {
			return true
		}
	}
	return false
}

type linuxACLEntry struct {
	tag  uint16
	perm uint16
	id   uint32
}

const (
	linuxACLVersion  = 2
	linuxACLUserObj  = 0x01
	linuxACLUser     = 0x02
	linuxACLGroupObj = 0x04
	linuxACLGroup    = 0x08
	linuxACLMask     = 0x10
	linuxACLOther    = 0x20
	linuxACLNoID     = ^uint32(0)
)

func VerifyEffectiveAccess(path string, subject ACLSubject, required, forbidden uint16) error {
	permissions, err := effectiveLinuxPathPermissions(path, subject)
	if err != nil {
		return err
	}
	return validateEffectiveLinuxPermissions(permissions, required, forbidden, subject)
}

func effectiveLinuxPathPermissions(path string, subject ACLSubject) (uint16, error) {
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return 0, err
	}
	identity, err := FileIdentity(info)
	if err != nil {
		return 0, err
	}
	return effectiveLinuxACLPermissions(path, info.Mode(), identity, subject)
}

func validateEffectiveLinuxPermissions(permissions, required, forbidden uint16, subject ACLSubject) error {
	if permissions&required != required || permissions&forbidden != 0 {
		return fmt.Errorf("effective permissions for UID %d GID %d are %#o, require %#o and forbid %#o", subject.UID, subject.GID, permissions, required, forbidden)
	}
	return nil
}

func effectiveLinuxACLPermissions(path string, mode os.FileMode, identity StatIdentity, subject ACLSubject) (uint16, error) {
	acl, found, err := readLinuxPOSIXACL(path, "system.posix_acl_access")
	if err != nil {
		return 0, err
	}
	if !found {
		return modePermissionForSubject(mode, identity, subject), nil
	}
	return permissionsFromLinuxACL(acl, identity, subject)
}

func defaultLinuxACLPermissions(path string, subject ACLSubject) (uint16, error) {
	acl, found, err := readLinuxPOSIXACL(path, "system.posix_acl_default")
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, errors.New("directory has no default POSIX ACL for future worktree entries")
	}
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return 0, err
	}
	identity, err := FileIdentity(info)
	if err != nil {
		return 0, err
	}
	return permissionsFromLinuxACL(acl, identity, subject)
}

func VerifyDefaultAccess(path string, subject ACLSubject, required, forbidden uint16) error {
	acl, found, err := readLinuxPOSIXACL(path, "system.posix_acl_default")
	if err != nil || !found {
		return err
	}
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return err
	}
	identity, err := FileIdentity(info)
	if err != nil {
		return err
	}
	permissions, err := permissionsFromLinuxACL(acl, identity, subject)
	if err != nil {
		return err
	}
	return validateEffectiveLinuxPermissions(permissions, required, forbidden, subject)
}

func readLinuxPOSIXACL(path, name string) ([]linuxACLEntry, bool, error) {
	const maximumACLBytes = 64 << 10
	data := make([]byte, maximumACLBytes)
	n, err := unix.Getxattr(path, name, data)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s ACL for %s: %w", name, path, err)
	}
	if n > len(data) || n < 4 || (n-4)%8 != 0 || binary.LittleEndian.Uint32(data[:4]) != linuxACLVersion {
		return nil, false, fmt.Errorf("%s ACL for %s has an unsupported or malformed encoding", name, path)
	}
	entries := make([]linuxACLEntry, 0, (n-4)/8)
	for offset := 4; offset < n; offset += 8 {
		entry := linuxACLEntry{
			tag:  binary.LittleEndian.Uint16(data[offset : offset+2]),
			perm: binary.LittleEndian.Uint16(data[offset+2 : offset+4]),
			id:   binary.LittleEndian.Uint32(data[offset+4 : offset+8]),
		}
		if entry.perm&^uint16(7) != 0 {
			return nil, false, fmt.Errorf("%s ACL for %s contains invalid permission bits", name, path)
		}
		entries = append(entries, entry)
	}
	if err := validateLinuxACLShape(entries); err != nil {
		return nil, false, fmt.Errorf("validate %s ACL for %s: %w", name, path, err)
	}
	return entries, true, nil
}

func VerifyNoUntrustedACLWrite(path string, trustedUIDs, trustedGIDs []uint32, includeDefault bool) error {
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return err
	}
	identity, err := FileIdentity(info)
	if err != nil {
		return err
	}
	access, found, err := readLinuxPOSIXACL(path, "system.posix_acl_access")
	if err != nil {
		return err
	}
	if found {
		if err := validateLinuxACLWriteEntries(access, identity, trustedUIDs, trustedGIDs); err != nil {
			return fmt.Errorf("access ACL: %w", err)
		}
	} else {
		if info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && !linuxIdentityAllowed(trustedGIDs, identity.GID) {
			return errors.New("mode grants write access to an untrusted group or other user")
		}
	}
	if includeDefault && info.IsDir() {
		defaults, found, err := readLinuxPOSIXACL(path, "system.posix_acl_default")
		if err != nil {
			return err
		}
		if found {
			if err := validateLinuxACLWriteEntries(defaults, identity, trustedUIDs, trustedGIDs); err != nil {
				return fmt.Errorf("default ACL: %w", err)
			}
		}
	}
	return nil
}

func validateLinuxACLWriteEntries(entries []linuxACLEntry, identity StatIdentity, trustedUIDs, trustedGIDs []uint32) error {
	mask := uint16(7)
	for _, entry := range entries {
		if entry.tag == linuxACLMask {
			mask = entry.perm
			break
		}
	}
	for _, entry := range entries {
		permissions := entry.perm
		if entry.tag == linuxACLUser || entry.tag == linuxACLGroup || entry.tag == linuxACLGroupObj {
			permissions &= mask
		}
		if permissions&2 == 0 {
			continue
		}
		switch entry.tag {
		case linuxACLUserObj:
			if !linuxIdentityAllowed(trustedUIDs, identity.UID) {
				return fmt.Errorf("inode owner UID %d has write access but is not trusted", identity.UID)
			}
		case linuxACLGroupObj:
			if !linuxIdentityAllowed(trustedGIDs, identity.GID) {
				return fmt.Errorf("owning GID %d has untrusted effective write access", identity.GID)
			}
		case linuxACLUser:
			if !linuxIdentityAllowed(trustedUIDs, entry.id) {
				return fmt.Errorf("named UID %d has untrusted effective write access", entry.id)
			}
		case linuxACLGroup:
			if !linuxIdentityAllowed(trustedGIDs, entry.id) {
				return fmt.Errorf("named GID %d has untrusted effective write access", entry.id)
			}
		case linuxACLOther:
			return errors.New("other-class ACL grants write access")
		}
	}
	return nil
}

func linuxIdentityAllowed(identities []uint32, expected uint32) bool {
	for _, identity := range identities {
		if identity == expected {
			return true
		}
	}
	return false
}

func validateLinuxACLShape(entries []linuxACLEntry) error {
	counts := make(map[uint16]int, 6)
	for _, entry := range entries {
		if entry.tag < linuxACLUserObj || entry.tag > linuxACLOther {
			return errors.New("ACL contains an unknown entry tag")
		}
		counts[entry.tag]++
		if (entry.tag == linuxACLUserObj || entry.tag == linuxACLGroupObj || entry.tag == linuxACLMask || entry.tag == linuxACLOther) && entry.id != linuxACLNoID {
			return errors.New("ACL object, mask, or other entry has a named identity")
		}
		if (entry.tag == linuxACLUser || entry.tag == linuxACLGroup) && entry.id == linuxACLNoID {
			return errors.New("ACL named user/group entry has no identity")
		}
	}
	if counts[linuxACLUserObj] != 1 || counts[linuxACLGroupObj] != 1 || counts[linuxACLOther] != 1 || counts[linuxACLMask] > 1 {
		return errors.New("ACL lacks unique owner/group/other entries or has duplicate mask")
	}
	if counts[linuxACLUser]+counts[linuxACLGroup] != 0 && counts[linuxACLMask] != 1 {
		return errors.New("ACL with named entries lacks its mandatory mask")
	}
	seen := make(map[[2]uint32]struct{}, len(entries))
	for _, entry := range entries {
		if entry.tag != linuxACLUser && entry.tag != linuxACLGroup {
			continue
		}
		key := [2]uint32{uint32(entry.tag), entry.id}
		if _, ok := seen[key]; ok {
			return errors.New("ACL repeats a named user/group identity")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func permissionsFromLinuxACL(entries []linuxACLEntry, identity StatIdentity, subject ACLSubject) (uint16, error) {
	var userObj, other, mask uint16
	var hasMask bool
	var namedUser *uint16
	var groupPermissions uint16
	matchedGroup := false
	for i := range entries {
		entry := &entries[i]
		switch entry.tag {
		case linuxACLUserObj:
			userObj = entry.perm
		case linuxACLGroupObj:
			if subject.hasGroup(identity.GID) {
				matchedGroup = true
				groupPermissions |= entry.perm
			}
		case linuxACLUser:
			if entry.id == subject.UID {
				perms := entry.perm
				namedUser = &perms
			}
		case linuxACLGroup:
			if subject.hasGroup(entry.id) {
				matchedGroup = true
				groupPermissions |= entry.perm
			}
		case linuxACLMask:
			mask, hasMask = entry.perm, true
		case linuxACLOther:
			other = entry.perm
		}
	}
	if subject.UID == identity.UID {
		return userObj, nil
	}
	if namedUser != nil {
		if !hasMask {
			return 0, errors.New("named ACL user has no mask")
		}
		return *namedUser & mask, nil
	}
	if matchedGroup {
		if !hasMask {
			return groupPermissions, nil
		}
		return groupPermissions & mask, nil
	}
	return other, nil
}

func modePermissionForSubject(mode os.FileMode, identity StatIdentity, subject ACLSubject) uint16 {
	permissions := uint16(mode.Perm())
	if subject.UID == identity.UID {
		return permissions >> 6 & 7
	}
	if subject.hasGroup(identity.GID) {
		return permissions >> 3 & 7
	}
	return permissions & 7
}
