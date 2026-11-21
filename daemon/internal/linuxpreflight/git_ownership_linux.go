//go:build linux && amd64

package linuxpreflight

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
)

func verifyLinuxCloneSource(repository string, owner linuxgate.OwnerConfig, a, b ACLSubject, excludedPaths ...string) error {
	if err := verifyLinuxTrustedGitExecutable(a, b); err != nil {
		return err
	}
	if err := verifyLinuxAControlledGitEntry(repository, owner.AUID, owner.AGID, b, 0, true); err != nil {
		return fmt.Errorf("A-owned Git source is not inaccessible to B: %w", err)
	}
	info, err := LstatCanonicalPath(repository)
	if err != nil {
		return fmt.Errorf("inspect A-owned Git source: %w", err)
	}
	identity, err := FileIdentity(info)
	if err != nil || identity.UID != owner.AUID {
		return errors.New("Git source root must be owned by A")
	}
	if err := VerifyPathTraversal(repository, a); err != nil {
		return fmt.Errorf("A cannot traverse its configured Git source: %w", err)
	}
	if err := VerifyEffectiveAccess(repository, a, 7, 0); err != nil {
		return fmt.Errorf("A cannot read/search/write its configured Git source: %w", err)
	}
	aRepositoryAccess, err := effectiveLinuxPathPermissions(repository, a)
	if err != nil {
		return err
	}
	bRepositoryAccess, err := effectiveLinuxPathPermissions(repository, b)
	if err != nil {
		return err
	}
	if err := validateLinuxCloneSourcePermissions(aRepositoryAccess, bRepositoryAccess); err != nil {
		return err
	}

	dotGit := filepath.Join(repository, ".git")
	gitEntry, err := LstatCanonicalPath(dotGit)
	if err != nil {
		return fmt.Errorf("inspect A-owned Git source .git entry: %w", err)
	}
	if gitEntry.IsDir() {
		if err := verifyLinuxAControlledGitEntry(dotGit, owner.AUID, owner.AGID, b, 0, true); err != nil {
			return fmt.Errorf("A-owned Git source .git directory is accessible to B or replaceable: %w", err)
		}
		if err := VerifyEffectiveAccess(dotGit, a, 7, 0); err != nil {
			return fmt.Errorf("A cannot manage its Git source metadata: %w", err)
		}
	} else if !gitEntry.Mode().IsRegular() {
		return errors.New("A-owned Git source .git entry must be a directory or regular linked-worktree file")
	} else {
		if err := verifyLinuxAControlledGitEntry(dotGit, owner.AUID, owner.AGID, b, 0, false); err != nil {
			return fmt.Errorf("A-owned Git source .git file is accessible to B or replaceable: %w", err)
		}
		if err := VerifyEffectiveAccess(dotGit, a, 4, 0); err != nil {
			return fmt.Errorf("A cannot read its Git source .git file: %w", err)
		}
	}

	commonDir, worktreeGitDir, err := resolveLinuxGitDirectories(repository)
	if err != nil {
		return fmt.Errorf("resolve A-owned Git source metadata: %w", err)
	}
	metadataDirectories := uniqueLinuxPaths(worktreeGitDir, commonDir)
	for _, directory := range metadataDirectories {
		for _, excluded := range excludedPaths {
			if pathsOverlapLinux(directory, excluded) {
				return fmt.Errorf("Git metadata directory %s overlaps protected path %s", directory, excluded)
			}
		}
		if err := verifyLinuxAControlledGitEntry(directory, owner.AUID, owner.AGID, b, 0, true); err != nil {
			return fmt.Errorf("A-owned Git metadata directory %s is accessible to B or replaceable: %w", directory, err)
		}
		metadataInfo, err := LstatCanonicalPath(directory)
		if err != nil {
			return fmt.Errorf("inspect A-owned Git metadata directory %s: %w", directory, err)
		}
		metadataIdentity, err := FileIdentity(metadataInfo)
		if err != nil || metadataIdentity.UID != owner.AUID {
			return fmt.Errorf("Git metadata directory %s must be owned by A", directory)
		}
		if err := VerifyEffectiveAccess(directory, a, 7, 0); err != nil {
			return fmt.Errorf("A cannot manage Git metadata directory %s: %w", directory, err)
		}
		visited := make(map[string]struct{})
		for _, configPath := range []string{filepath.Join(directory, "config"), filepath.Join(directory, "config.worktree")} {
			if _, err := os.Lstat(configPath); errors.Is(err, os.ErrNotExist) {
				if filepath.Base(configPath) == "config" {
					return fmt.Errorf("required Git configuration %s is missing", configPath)
				}
				continue
			} else if err != nil {
				return fmt.Errorf("inspect Git configuration %s: %w", configPath, err)
			}
			if err := verifyLinuxGitConfig(configPath, b, a, visited, 0); err != nil {
				return fmt.Errorf("verify A-owned executable Git configuration: %w", err)
			}
		}
		if err := verifyLinuxGitHookTree(filepath.Join(directory, "hooks"), b, a, 0, new(int)); err != nil {
			return fmt.Errorf("verify A-owned, non-B-writable Git hooks: %w", err)
		}
		coveredByCommon := false
		for _, candidate := range metadataDirectories {
			if candidate == directory {
				continue
			}
			relative, relErr := filepath.Rel(candidate, directory)
			if relErr == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				coveredByCommon = true
				break
			}
		}
		if !coveredByCommon {
			metadataEntries := new(int)
			if err := verifyLinuxGitMetadataTree(directory, b, a, owner.AUID, owner.AGID, 0, metadataEntries); err != nil {
				return fmt.Errorf("verify A-owned Git metadata tree %s: %w", directory, err)
			}
		}
	}
	return nil
}

func verifyLinuxTrustedGitExecutable(a, b ACLSubject) error {
	return verifyLinuxTrustedHostExecutable("/usr/bin/git", a, b)
}

func verifyLinuxTrustedHostExecutable(path string, a, b ACLSubject) error {
	if err := verifyLinuxServicePathAncestors(path, b, 0); err != nil {
		return fmt.Errorf("trusted host executable %s is replaceable or inaccessible: %w", path, err)
	}
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("inspect trusted host executable %s: %w", path, err)
	}
	identity, err := FileIdentity(info)
	if err != nil {
		return err
	}
	if err := validateLinuxTrustedHostExecutableMetadata(path, identity, info.Mode().IsRegular()); err != nil {
		return err
	}
	if err := VerifyNoUntrustedACLWrite(path, []uint32{0}, nil, false); err != nil {
		return fmt.Errorf("trusted host executable %s has an untrusted ACL writer: %w", path, err)
	}
	if err := VerifyEffectiveAccess(path, a, 5, 2); err != nil {
		return fmt.Errorf("A cannot execute trusted host binary %s safely: %w", path, err)
	}
	if err := VerifyEffectiveAccess(path, b, 0, 2); err != nil {
		return fmt.Errorf("B can modify trusted host binary %s: %w", path, err)
	}
	return nil
}

func verifyLinuxGitHookTree(path string, b, a ACLSubject, depth int, entries *int) error {
	if depth > 16 {
		return fmt.Errorf("Git hook tree %s exceeds the static depth bound", path)
	}
	info, err := LstatCanonicalPath(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	identity, err := FileIdentity(info)
	if err != nil {
		return err
	}
	if err := validateLinuxGitHookMetadata(identity, info.Mode(), a.UID, a.GID); err != nil {
		return fmt.Errorf("Git hook entry %s is unsafe: %w", path, err)
	}
	if err := VerifyNoUntrustedACLWrite(path, []uint32{0, a.UID}, []uint32{a.GID}, info.IsDir()); err != nil {
		return fmt.Errorf("Git hook entry %s has an untrusted ACL writer: %w", path, err)
	}
	if err := VerifyEffectiveAccess(path, b, 0, 2); err != nil {
		return fmt.Errorf("B can modify Git hook entry %s: %w", path, err)
	}
	if info.IsDir() {
		if err := VerifyEffectiveAccess(path, a, 5, 0); err != nil {
			return fmt.Errorf("A cannot read/search Git hook directory %s: %w", path, err)
		}
		children, err := readBoundedLinuxDirectory(path, entries)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := verifyLinuxGitHookTree(filepath.Join(path, child.Name()), b, a, depth+1, entries); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Git hook entry %s is not a regular file", path)
	}
	return VerifyEffectiveAccess(path, a, 4, 0)
}

func validateLinuxTrustedGitExecutableMetadata(identity StatIdentity, regular bool) error {
	return validateLinuxTrustedHostExecutableMetadata("/usr/bin/git", identity, regular)
}

func validateLinuxTrustedHostExecutableMetadata(path string, identity StatIdentity, regular bool) error {
	if !regular || identity.UID != 0 || identity.GID != 0 || identity.Links != 1 || identity.Mode&06022 != 0 || identity.Mode&0111 == 0 {
		return fmt.Errorf("trusted executable %s must be a root:root-owned, singly-linked, executable, non-writable regular file", path)
	}
	return nil
}

func validateLinuxGitHookMetadata(identity StatIdentity, mode os.FileMode, aUID, aGID uint32) error {
	if identity.UID != aUID || !mode.IsDir() && identity.Links != 1 || mode&os.ModeSymlink != 0 || !mode.IsDir() && !mode.IsRegular() ||
		mode.Perm()&0002 != 0 || mode.Perm()&0020 != 0 && identity.GID != aGID {
		return errors.New("hook entries must be A-owned regular files/directories and protected from untrusted writers")
	}
	return nil
}

func verifyLinuxCloneSeedRoot(seedRoot string, owner linuxgate.OwnerConfig, a, b ACLSubject) error {
	if err := verifyLinuxAControlledGitParents(seedRoot, b, owner.AUID, owner.AGID); err != nil {
		return fmt.Errorf("A-owned Linux clone SeedRoot ancestor policy is unsafe: %w", err)
	}
	if err := VerifyPathTraversal(seedRoot, a); err != nil {
		return fmt.Errorf("A cannot traverse Linux clone SeedRoot: %w", err)
	}
	if err := VerifyPathTraversal(seedRoot, b); err != nil {
		return fmt.Errorf("B cannot traverse Linux clone SeedRoot: %w", err)
	}
	info, err := LstatCanonicalPath(seedRoot)
	if err != nil {
		return fmt.Errorf("inspect A-owned Linux clone SeedRoot: %w", err)
	}
	identity, err := FileIdentity(info)
	if err != nil || !info.IsDir() || identity.UID != owner.AUID || identity.GID != owner.AGID || info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && identity.GID != owner.AGID {
		return errors.New("Linux clone SeedRoot must be an A-owned directory without untrusted group/other write access")
	}
	if err := verifyLinuxCloneSeedDirectory(seedRoot, owner, a, b, 0, new(int)); err != nil {
		return fmt.Errorf("verify immutable Linux clone SeedRoot exchange ACLs: %w", err)
	}
	return nil
}

func verifyLinuxCloneSeedDirectory(path string, owner linuxgate.OwnerConfig, a, b ACLSubject, depth int, entries *int) error {
	if depth > 2 {
		return fmt.Errorf("seed directory %s exceeds the exact binding/run layout", path)
	}
	if err := verifyLinuxAControlledGitEntry(path, owner.AUID, owner.AGID, b, 5, true); err != nil {
		return err
	}
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return err
	}
	identity, err := FileIdentity(info)
	if err != nil || identity.UID != owner.AUID || identity.GID != owner.AGID || info.Mode().Perm() != 0o750 {
		return fmt.Errorf("seed directory %s must be A-owned with mode 0750", path)
	}
	aAccess, err := effectiveLinuxPathPermissions(path, a)
	if err != nil {
		return fmt.Errorf("read A seed directory access ACL %s: %w", path, err)
	}
	bAccess, err := effectiveLinuxPathPermissions(path, b)
	if err != nil {
		return fmt.Errorf("read B seed directory access ACL %s: %w", path, err)
	}
	aDefault, err := defaultLinuxACLPermissions(path, a)
	if err != nil {
		return fmt.Errorf("read future A seed entry ACL at %s: %w", path, err)
	}
	bDefault, err := defaultLinuxACLPermissions(path, b)
	if err != nil {
		return fmt.Errorf("read future B seed entry ACL at %s: %w", path, err)
	}
	if err := validateLinuxCloneSeedPermissions(aAccess, bAccess, aDefault, bDefault); err != nil {
		return err
	}
	children, err := readBoundedLinuxDirectory(path, entries)
	if err != nil {
		return fmt.Errorf("list A-owned Linux clone seed directory %s: %w", path, err)
	}
	for _, child := range children {
		childPath := filepath.Join(path, child.Name())
		childInfo, err := LstatCanonicalPath(childPath)
		if err != nil {
			return fmt.Errorf("inspect A-owned clone seed entry %s: %w", childPath, err)
		}
		if childInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("A-owned clone seed entry %s is a symlink", childPath)
		}
		if childInfo.IsDir() {
			if depth == 2 || !validLinuxCloneSeedDirectoryName(child.Name(), depth) {
				return fmt.Errorf("unexpected directory %s in A-owned clone seed layout", childPath)
			}
			if err := verifyLinuxCloneSeedDirectory(childPath, owner, a, b, depth+1, entries); err != nil {
				return err
			}
			continue
		}
		if depth != 2 {
			return fmt.Errorf("unexpected non-directory %s in A-owned clone seed layout", childPath)
		}
		identity, err := FileIdentity(childInfo)
		if err != nil || !childInfo.Mode().IsRegular() || identity.UID != owner.AUID || identity.Links != 1 {
			return fmt.Errorf("seed file %s must be a singly-linked A-owned regular file", childPath)
		}
		if err := VerifyNoUntrustedACLWrite(childPath, []uint32{0, owner.AUID}, []uint32{owner.AGID}, false); err != nil {
			return fmt.Errorf("seed file %s has an untrusted ACL writer: %w", childPath, err)
		}
		if strings.HasPrefix(child.Name(), ".clone-seed-") && validLinuxCloneSeedTempName(child.Name()) {
			if childInfo.Mode().Perm() != 0o600 && childInfo.Mode().Perm() != 0o440 || childInfo.Size() < 0 || childInfo.Size() > authority.LinuxCloneMaxSeedBytes+1 {
				return fmt.Errorf("in-progress seed temporary %s must remain A-owned and non-writable", childPath)
			}
			aRead := uint16(6)
			bRead, bForbidden := uint16(0), uint16(7)
			if childInfo.Mode().Perm() == 0o440 {
				aRead, bRead, bForbidden = 4, 4, 2
			}
			if err := VerifyEffectiveAccess(childPath, a, aRead, 0); err != nil {
				return fmt.Errorf("A cannot read in-progress seed temporary %s: %w", childPath, err)
			}
			if err := VerifyEffectiveAccess(childPath, b, bRead, bForbidden); err != nil {
				return fmt.Errorf("B can modify in-progress seed temporary %s: %w", childPath, err)
			}
			continue
		}
		if childInfo.Size() <= 0 || childInfo.Size() > authority.LinuxCloneMaxSeedBytes || !validLinuxCloneBundleName(child.Name()) || childInfo.Mode().Perm() != 0o440 {
			return fmt.Errorf("unexpected or mutable seed bundle entry %s", childPath)
		}
		if err := VerifyEffectiveAccess(childPath, a, 4, 0); err != nil {
			return fmt.Errorf("A cannot read immutable seed bundle %s: %w", childPath, err)
		}
		if err := VerifyEffectiveAccess(childPath, b, 4, 2); err != nil {
			return fmt.Errorf("B cannot read the immutable seed bundle without write access %s: %w", childPath, err)
		}
	}
	return nil
}

func validLinuxCloneSeedDirectoryName(name string, depth int) bool {
	if depth == 0 {
		return strings.HasPrefix(name, "binding-") && len(name) > len("binding-")
	}
	return strings.HasPrefix(name, "run-") && len(name) > len("run-")
}

func validLinuxCloneBundleName(name string) bool {
	if !strings.HasPrefix(name, "generation-") || !strings.HasSuffix(name, ".bundle") {
		return false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(name, "generation-"), ".bundle")
	generation, err := strconv.ParseUint(value, 10, 63)
	return err == nil && generation > 0 && strconv.FormatUint(generation, 10) == value
}

func validLinuxCloneSeedTempName(name string) bool {
	if len(name) != len(".clone-seed-")+24 || !strings.HasPrefix(name, ".clone-seed-") {
		return false
	}
	for _, value := range strings.TrimPrefix(name, ".clone-seed-") {
		if !(value >= '0' && value <= '9' || value >= 'a' && value <= 'f') {
			return false
		}
	}
	return true
}

func verifyLinuxCloneRoot(root string, owner linuxgate.OwnerConfig, a, b ACLSubject) error {
	if err := verifyLinuxAControlledGitParents(root, b, owner.AUID, owner.AGID); err != nil {
		return fmt.Errorf("B-owned Linux clone Root ancestor policy is unsafe: %w", err)
	}
	if err := VerifyPathTraversal(root, a); err != nil {
		return fmt.Errorf("A cannot traverse B-owned Linux clone Root: %w", err)
	}
	if err := VerifyPathTraversal(root, b); err != nil {
		return fmt.Errorf("B cannot traverse Linux clone Root: %w", err)
	}
	info, err := LstatCanonicalPath(root)
	if err != nil {
		return fmt.Errorf("inspect B-owned Linux clone Root: %w", err)
	}
	identity, err := FileIdentity(info)
	if err != nil || !info.IsDir() || identity.UID != owner.BUID || identity.GID != owner.BGID || info.Mode().Perm()&0022 != 0 {
		return errors.New("Linux clone Root must be a B-owned directory not writable through group/other mode bits")
	}
	if err := VerifyNoUntrustedACLWrite(root, []uint32{0, owner.BUID}, nil, true); err != nil {
		return fmt.Errorf("B-owned clone Root has an untrusted ACL writer: %w", err)
	}
	aAccess, err := effectiveLinuxPathPermissions(root, a)
	if err != nil {
		return fmt.Errorf("read A clone Root access ACL: %w", err)
	}
	bAccess, err := effectiveLinuxPathPermissions(root, b)
	if err != nil {
		return fmt.Errorf("read B clone Root access ACL: %w", err)
	}
	aDefault, err := defaultLinuxACLPermissions(root, a)
	if err != nil {
		return fmt.Errorf("read future A clone target ACL: %w", err)
	}
	bDefault, err := defaultLinuxACLPermissions(root, b)
	if err != nil {
		return fmt.Errorf("read future B clone target ACL: %w", err)
	}
	if err := validateLinuxCloneRootPermissions(aAccess, bAccess, aDefault, bDefault); err != nil {
		return err
	}
	return nil
}

func validateLinuxCloneSourcePermissions(a, b uint16) error {
	if a&7 != 7 || b&7 != 0 {
		return fmt.Errorf("source access must grant A rwx and deny B completely (A=%#o, B=%#o)", a, b)
	}
	return nil
}

func validateLinuxCloneSeedPermissions(a, b, aDefault, bDefault uint16) error {
	if a&7 != 7 || b&5 != 5 || b&2 != 0 || aDefault&7 != 7 || bDefault&5 != 5 || bDefault&2 != 0 {
		return fmt.Errorf("SeedRoot and inherited entries must be A-rwx/B-r-x without B write (access A=%#o B=%#o default A=%#o B=%#o)", a, b, aDefault, bDefault)
	}
	return nil
}

func validateLinuxCloneRootPermissions(a, b, aDefault, bDefault uint16) error {
	if a&5 != 5 || a&2 != 0 || b&7 != 7 || aDefault&5 != 5 || aDefault&2 != 0 || bDefault&7 != 7 {
		return fmt.Errorf("Root and inherited entries must grant B rwx and A r-x without A write (access A=%#o B=%#o default A=%#o B=%#o)", a, b, aDefault, bDefault)
	}
	return nil
}

func verifyLinuxGitMetadataTree(path string, b, a ACLSubject, ownerUID, ownerGID uint32, depth int, entries *int) error {
	if depth > linuxClonePreflightMaxDepth {
		return fmt.Errorf("Git metadata tree %s exceeds the static depth bound", path)
	}
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return err
	}
	identity, err := FileIdentity(info)
	if err != nil || identity.UID != 0 && identity.UID != ownerUID || !info.IsDir() && identity.Links != 1 || info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && identity.GID != ownerGID {
		return fmt.Errorf("Git metadata entry %s is not a protected root/A-owned inode", path)
	}
	if err := VerifyNoUntrustedACLWrite(path, []uint32{0, ownerUID}, []uint32{ownerGID}, info.IsDir()); err != nil {
		return fmt.Errorf("Git metadata entry %s has an untrusted ACL writer: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Git metadata entry %s is a symlink", path)
	}
	if err := VerifyEffectiveAccess(path, b, 0, 7); err != nil {
		return fmt.Errorf("Git metadata entry %s is accessible to B: %w", path, err)
	}
	if info.IsDir() {
		if err := VerifyEffectiveAccess(path, a, 5, 0); err != nil {
			return fmt.Errorf("A cannot read/search Git metadata directory %s: %w", path, err)
		}
		children, err := readBoundedLinuxDirectory(path, entries)
		if err != nil {
			return err
		}
		for _, child := range children {
			childPath := filepath.Join(path, child.Name())
			if child.Name() == "alternates" {
				return errors.New("Git object alternates are not permitted by clone-v2 preflight")
			}
			if err := verifyLinuxGitMetadataTree(childPath, b, a, ownerUID, ownerGID, depth+1, entries); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Git metadata entry %s is not a regular file", path)
	}
	if err := VerifyEffectiveAccess(path, a, 4, 0); err != nil {
		return fmt.Errorf("A cannot read Git metadata file %s: %w", path, err)
	}
	return nil
}

func resolveLinuxGitDirectories(repository string) (commonDir, worktreeGitDir string, err error) {
	dotGit := filepath.Join(repository, ".git")
	info, err := os.Lstat(dotGit)
	if err != nil {
		return "", "", fmt.Errorf("stat .git: %w", err)
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "", "", errors.New("repository .git entry must not be a symlink")
	case info.IsDir():
		worktreeGitDir = dotGit
	case info.Mode().IsRegular():
		data, readErr := readBoundedLinuxFile(dotGit, 4096)
		if readErr != nil {
			return "", "", readErr
		}
		line := strings.TrimSuffix(string(data), "\n")
		if strings.Contains(line, "\n") || !strings.HasPrefix(line, "gitdir: ") || strings.TrimSpace(strings.TrimPrefix(line, "gitdir: ")) == "" {
			return "", "", errors.New("repository .git file has an unsupported shape")
		}
		worktreeGitDir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir: "))
		if !filepath.IsAbs(worktreeGitDir) {
			worktreeGitDir = filepath.Join(repository, worktreeGitDir)
		}
		worktreeGitDir = filepath.Clean(worktreeGitDir)
	default:
		return "", "", errors.New("repository .git entry is neither a directory nor a regular linked-worktree file")
	}
	gitInfo, err := LstatCanonicalPath(worktreeGitDir)
	if err != nil || !gitInfo.IsDir() {
		return "", "", errors.New("resolved repository Git directory is unavailable")
	}
	commonDir = worktreeGitDir
	commondirPath := filepath.Join(worktreeGitDir, "commondir")
	if commondirInfo, err := os.Lstat(commondirPath); err == nil {
		if !commondirInfo.Mode().IsRegular() {
			return "", "", errors.New("Git commondir entry must be a regular file")
		}
		data, readErr := readBoundedLinuxFile(commondirPath, 4096)
		if readErr != nil {
			return "", "", readErr
		}
		commonText := strings.TrimSuffix(string(data), "\n")
		if commonText == "" || strings.Contains(commonText, "\n") {
			return "", "", errors.New("Git commondir file has an unsupported shape")
		}
		if !filepath.IsAbs(commonText) {
			commonText = filepath.Join(worktreeGitDir, commonText)
		}
		commonDir = filepath.Clean(commonText)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	commonInfo, err := LstatCanonicalPath(commonDir)
	if err != nil || !commonInfo.IsDir() {
		return "", "", errors.New("resolved common Git directory is unavailable")
	}
	return commonDir, worktreeGitDir, nil
}

func verifyLinuxGitConfig(path string, b, a ACLSubject, visited map[string]struct{}, depth int) error {
	if depth > 8 {
		return errors.New("Git configuration include depth exceeds the read-only preflight bound")
	}
	path = filepath.Clean(path)
	if _, ok := visited[path]; ok {
		return nil
	}
	visited[path] = struct{}{}
	if err := verifyLinuxAControlledGitParents(path, b, a.UID, a.GID); err != nil {
		return fmt.Errorf("Git config %s is beneath a replaceable ancestor: %w", path, err)
	}
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("Git configuration %s is unavailable: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Git configuration %s is not a regular file", path)
	}
	identity, err := FileIdentity(info)
	if err != nil || identity.UID != a.UID || info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && identity.GID != a.GID {
		return fmt.Errorf("Git configuration %s is not A-owned and protected from untrusted writers", path)
	}
	if err := VerifyNoUntrustedACLWrite(path, []uint32{0, a.UID}, []uint32{a.GID}, false); err != nil {
		return fmt.Errorf("Git configuration %s has an untrusted ACL writer: %w", path, err)
	}
	if err := VerifyEffectiveAccess(path, b, 0, 7); err != nil {
		return fmt.Errorf("B can access executable Git configuration %s: %w", path, err)
	}
	if err := VerifyEffectiveAccess(path, a, 4, 0); err != nil {
		return fmt.Errorf("A cannot read Git configuration %s: %w", path, err)
	}
	contents, err := readBoundedLinuxFile(path, linuxGitConfigMaxBytes)
	if err != nil {
		return fmt.Errorf("read executable Git configuration %s: %w", path, err)
	}
	section := ""
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), linuxGitConfigMaxBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line, err := stripGitConfigComment(scanner.Text())
		if err != nil {
			return fmt.Errorf("parse Git configuration %s:%d: %w", path, lineNumber, err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return fmt.Errorf("Git configuration %s:%d has an incomplete section", path, lineNumber)
			}
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if section == "" {
				return fmt.Errorf("Git configuration %s:%d has an empty section", path, lineNumber)
			}
			continue
		}
		key, value, hasValue := strings.Cut(line, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || strings.ContainsAny(key, " \t") {
			return fmt.Errorf("Git configuration %s:%d has an invalid key", path, lineNumber)
		}
		if !hasValue {
			value = "true"
		}
		value, err = parseGitConfigScalar(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("Git configuration %s:%d value is not safely understood: %w", path, lineNumber, err)
		}
		if reason := linuxGitExternalCommandSetting(section, key, value); reason != "" {
			return fmt.Errorf("Git configuration %s:%d has unsupported executable setting %s: %s", path, lineNumber, key, reason)
		}
		switch {
		case strings.HasPrefix(section, "filter ") && (key == "clean" || key == "smudge" || key == "process") && value != "" && value != "false":
			return fmt.Errorf("Git filter command in executable config %s can consume B-controlled workspace content", path)
		case (section == "include" || strings.HasPrefix(section, "includeif ")) && key == "path":
			if value == "" || strings.HasPrefix(value, "~") || strings.Contains(value, "%(") {
				return fmt.Errorf("Git config include in %s uses an unresolved path form", path)
			}
			if !filepath.IsAbs(value) {
				value = filepath.Join(filepath.Dir(path), value)
			}
			if err := verifyLinuxGitConfig(filepath.Clean(value), b, a, visited, depth+1); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan Git configuration %s: %w", path, err)
	}
	return nil
}

func linuxGitExternalCommandSetting(section, key, value string) string {
	if value == "" {
		return ""
	}
	switch {
	case section == "core" && (key == "pager" || key == "editor" || key == "sshcommand" || key == "fsmonitor" || key == "askpass" || key == "attributesfile"):
		return "core setting can invoke an external program or executable attribute source"
	case strings.HasPrefix(section, "filter ") && (key == "clean" || key == "smudge" || key == "process"):
		return "filter command can execute while A checks out a B-controlled worktree"
	case strings.HasPrefix(section, "credential ") && key == "helper":
		return "credential helper can execute under A"
	case strings.HasPrefix(section, "diff ") && (key == "command" || key == "textconv"):
		return "diff driver can execute under A"
	case section == "diff" && key == "external":
		return "external diff command can execute under A"
	case strings.HasPrefix(section, "merge ") && key == "driver":
		return "merge driver can execute under A"
	case strings.HasPrefix(section, "difftool ") && key == "cmd":
		return "diff tool can execute under A"
	case strings.HasPrefix(section, "mergetool ") && key == "cmd":
		return "merge tool can execute under A"
	case section == "alias" && strings.HasPrefix(value, "!"):
		return "shell alias can execute under A"
	default:
		return ""
	}
}

func parseGitConfigScalar(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if value[0] == '"' {
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", err
		}
		return parsed, nil
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("Git config value contains a control character")
	}
	return strings.TrimSpace(value), nil
}

func VerifyPathTraversal(path string, subject ACLSubject) error {
	if path == "/" {
		return nil
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		if err := VerifyEffectiveAccess(parent, subject, 1, 0); err != nil {
			return fmt.Errorf("directory %s is not searchable: %w", parent, err)
		}
		if parent == "/" {
			return nil
		}
	}
}

func verifyLinuxAControlledGitParents(path string, b ACLSubject, ownerUID, ownerGID uint32) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Git control path is not canonical and absolute")
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := LstatCanonicalPath(parent)
		if err != nil {
			return err
		}
		identity, err := FileIdentity(info)
		groupWritableByA := identity.GID == ownerGID
		if err != nil || !info.IsDir() || identity.UID != 0 && identity.UID != ownerUID || info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && !groupWritableByA {
			return fmt.Errorf("Git control ancestor %s is not root/A-owned and non-writable", parent)
		}
		if err := VerifyNoUntrustedACLWrite(parent, []uint32{0, ownerUID}, []uint32{ownerGID}, true); err != nil {
			return fmt.Errorf("Git control ancestor %s has an untrusted ACL writer: %w", parent, err)
		}
		if err := VerifyEffectiveAccess(parent, b, 0, 2); err != nil {
			return fmt.Errorf("Git control ancestor %s is B-writable: %w", parent, err)
		}
		if parent == "/" {
			return nil
		}
	}
}

func verifyLinuxAControlledGitEntry(path string, ownerUID, ownerGID uint32, b ACLSubject, bPermissions uint16, wantDirectory bool) error {
	if err := verifyLinuxAControlledGitParents(path, b, ownerUID, ownerGID); err != nil {
		return err
	}
	if bPermissions != 0 {
		if err := VerifyPathTraversal(path, b); err != nil {
			return err
		}
	}
	info, err := LstatCanonicalPath(path)
	if err != nil {
		return err
	}
	identity, err := FileIdentity(info)
	if err != nil {
		return err
	}
	if wantDirectory && !info.IsDir() || !wantDirectory && !info.Mode().IsRegular() {
		return errors.New("Git metadata entry has the wrong file type")
	}
	if identity.UID != 0 && identity.UID != ownerUID || info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && identity.GID != ownerGID {
		return errors.New("Git metadata entry is not root/A-owned and non-writable by untrusted groups")
	}
	if err := VerifyNoUntrustedACLWrite(path, []uint32{0, ownerUID}, []uint32{ownerGID}, wantDirectory); err != nil {
		return fmt.Errorf("Git metadata entry has an untrusted ACL writer: %w", err)
	}
	forbidden := uint16(2)
	if bPermissions == 0 {
		forbidden = 7
	}
	if err := VerifyEffectiveAccess(path, b, bPermissions, forbidden); err != nil {
		return err
	}
	return nil
}
