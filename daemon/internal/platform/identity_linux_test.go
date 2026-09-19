//go:build linux

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProcessIdentityUsesStrictLinuxV2Shape(t *testing.T) {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}

	parts := strings.Split(identity, ":")
	if len(parts) != 6 || parts[0] != "linux" || parts[1] != "v2" {
		t.Fatalf("identity = %q, want linux:v2:<boot>:<pidns inode>:<pid>:<starttime>", identity)
	}
	if !isCanonicalLinuxBootID(parts[2]) {
		t.Fatalf("identity boot ID = %q, want canonical lowercase UUID", parts[2])
	}
	namespace, err := parseCanonicalPositiveUint(parts[3], "pid namespace inode")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := parseCanonicalPositiveUint(parts[4], "pid")
	if err != nil {
		t.Fatal(err)
	}
	if pid != uint64(os.Getpid()) {
		t.Fatalf("identity pid = %d, want %d", pid, os.Getpid())
	}
	if _, err := parseCanonicalPositiveUint(parts[5], "start time"); err != nil {
		t.Fatal(err)
	}

	link, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		t.Fatalf("read self PID namespace: %v", err)
	}
	linkInode, err := parseLinuxPIDNamespaceLink(link)
	if err != nil {
		t.Fatal(err)
	}
	if namespace != linkInode {
		t.Fatalf("identity pid namespace inode = %d, symlink inode = %d", namespace, linkInode)
	}
	var stat unix.Stat_t
	if err := unix.Stat(filepath.Clean("/proc/self/ns/pid"), &stat); err != nil {
		t.Fatalf("stat self PID namespace: %v", err)
	}
	if uint64(stat.Ino) != namespace {
		t.Fatalf("identity pid namespace inode = %d, stat inode = %d", namespace, stat.Ino)
	}
}

func TestProcessIdentityRequiresTargetNamespaceToMatchSelfAndStat(t *testing.T) {
	originalBootID := readLinuxBootID
	originalNamespace := readLinuxPIDNamespaceInfo
	originalStartTime := readLinuxProcessStartTime
	t.Cleanup(func() {
		readLinuxBootID = originalBootID
		readLinuxPIDNamespaceInfo = originalNamespace
		readLinuxProcessStartTime = originalStartTime
	})

	readLinuxBootID = func() (string, error) { return "01234567-89ab-cdef-0123-456789abcdef", nil }
	readLinuxProcessStartTime = func(int) (uint64, error) { return 42, nil }

	t.Run("namespace mismatch", func(t *testing.T) {
		readLinuxPIDNamespaceInfo = func(pid int) (linuxPIDNamespaceInfo, error) {
			if pid == 0 {
				return linuxPIDNamespaceInfo{link: "pid:[11]", inode: 11}, nil
			}
			return linuxPIDNamespaceInfo{link: "pid:[12]", inode: 12}, nil
		}
		if _, err := ProcessIdentity(123); err == nil || !strings.Contains(err.Error(), "PID namespace") {
			t.Fatalf("ProcessIdentity() error = %v, want namespace mismatch", err)
		}
	})

	t.Run("symlink and stat mismatch", func(t *testing.T) {
		readLinuxPIDNamespaceInfo = func(pid int) (linuxPIDNamespaceInfo, error) {
			if pid == 0 {
				return linuxPIDNamespaceInfo{link: "pid:[11]", inode: 11}, nil
			}
			return linuxPIDNamespaceInfo{link: "pid:[11]", inode: 12}, nil
		}
		if _, err := ProcessIdentity(123); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("ProcessIdentity() error = %v, want inode mismatch", err)
		}
	})
}

func TestParseLinuxProcessIdentityRecognizesV1ButProofRejectsIt(t *testing.T) {
	identity, err := parseLinuxProcessIdentity("linux:01234567-89ab-cdef-0123-456789abcdef:123:42")
	if err != nil {
		t.Fatalf("parseLinuxProcessIdentity() error = %v", err)
	}
	if identity.version != 1 || identity.pid != 123 || identity.startTime != 42 {
		t.Fatalf("parsed v1 identity = %#v", identity)
	}

	originalPriority := readLinuxProcessGroupPriority
	t.Cleanup(func() { readLinuxProcessGroupPriority = originalPriority })
	called := false
	readLinuxProcessGroupPriority = func(int, int) (int, error) {
		called = true
		return 0, unix.ESRCH
	}
	err = ProvePersistedProcessGroupAbsent(context.Background(), 123, "linux:01234567-89ab-cdef-0123-456789abcdef:123:42")
	if !errors.Is(err, ErrPersistedProcessGroupUnproven) || called {
		t.Fatalf("v1 proof error = %v, getpriority called = %t; want unproven without syscall", err, called)
	}
}

func TestProvePersistedProcessGroupAbsentDoesNotReadTargetProcess(t *testing.T) {
	const (
		pid    = 12345
		bootID = "01234567-89ab-cdef-0123-456789abcdef"
		inode  = uint64(77)
	)
	originalBootID := readLinuxBootID
	originalNamespace := readLinuxPIDNamespaceInfo
	originalStartTime := readLinuxProcessStartTime
	originalPriority := readLinuxProcessGroupPriority
	t.Cleanup(func() {
		readLinuxBootID = originalBootID
		readLinuxPIDNamespaceInfo = originalNamespace
		readLinuxProcessStartTime = originalStartTime
		readLinuxProcessGroupPriority = originalPriority
	})

	readLinuxBootID = func() (string, error) { return bootID, nil }
	readLinuxPIDNamespaceInfo = func(lookedUpPID int) (linuxPIDNamespaceInfo, error) {
		if lookedUpPID != 0 {
			t.Fatalf("proof read target PID namespace %d, want self namespace only", lookedUpPID)
		}
		return linuxPIDNamespaceInfo{link: "pid:[77]", inode: inode}, nil
	}
	readLinuxProcessStartTime = func(lookedUpPID int) (uint64, error) {
		t.Fatalf("proof read target process start time for PID %d", lookedUpPID)
		return 0, nil
	}
	readLinuxProcessGroupPriority = func(which, who int) (int, error) {
		if which != unix.PRIO_PGRP || who != pid {
			t.Fatalf("getpriority arguments = (%d, %d), want (%d, %d)", which, who, unix.PRIO_PGRP, pid)
		}
		return 0, unix.ESRCH
	}

	expected := fmt.Sprintf("linux:v2:%s:%d:%d:%d", bootID, inode, pid, 456)
	if err := ProvePersistedProcessGroupAbsent(context.Background(), pid, expected); err != nil {
		t.Fatalf("synthetic absent proof = %v", err)
	}
}

func TestProvePersistedProcessGroupAbsentRejectsEnvelopeBeforeGetpriority(t *testing.T) {
	const (
		pid    = 12345
		bootID = "01234567-89ab-cdef-0123-456789abcdef"
		inode  = uint64(77)
	)
	originalBootID := readLinuxBootID
	originalNamespace := readLinuxPIDNamespaceInfo
	originalStartTime := readLinuxProcessStartTime
	originalPriority := readLinuxProcessGroupPriority
	t.Cleanup(func() {
		readLinuxBootID = originalBootID
		readLinuxPIDNamespaceInfo = originalNamespace
		readLinuxProcessStartTime = originalStartTime
		readLinuxProcessGroupPriority = originalPriority
	})

	readLinuxBootID = func() (string, error) { return bootID, nil }
	readLinuxPIDNamespaceInfo = func(lookedUpPID int) (linuxPIDNamespaceInfo, error) {
		if lookedUpPID != 0 {
			t.Fatalf("proof read target PID namespace %d", lookedUpPID)
		}
		return linuxPIDNamespaceInfo{link: "pid:[77]", inode: inode}, nil
	}
	readLinuxProcessStartTime = func(int) (uint64, error) {
		t.Fatal("proof read target process start time")
		return 0, nil
	}
	priorityCalls := 0
	readLinuxProcessGroupPriority = func(int, int) (int, error) {
		priorityCalls++
		return 0, unix.ESRCH
	}

	cases := []struct {
		name     string
		expected string
	}{
		{name: "boot mismatch", expected: fmt.Sprintf("linux:v2:fedcba98-7654-3210-fedc-ba9876543210:%d:%d:%d", inode, pid, 456)},
		{name: "namespace mismatch", expected: fmt.Sprintf("linux:v2:%s:%d:%d:%d", bootID, inode+1, pid, 456)},
		{name: "pid mismatch", expected: fmt.Sprintf("linux:v2:%s:%d:%d:%d", bootID, inode, pid+1, 456)},
		{name: "v1", expected: fmt.Sprintf("linux:%s:%d:%d", bootID, pid, 456)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			priorityCalls = 0
			err := ProvePersistedProcessGroupAbsent(context.Background(), pid, test.expected)
			if !errors.Is(err, ErrPersistedProcessGroupUnproven) {
				t.Fatalf("proof error = %v, want unproven category", err)
			}
			if priorityCalls != 0 {
				t.Fatalf("getpriority calls = %d, want 0", priorityCalls)
			}
		})
	}
}

func TestProvePersistedProcessGroupAbsentPropagatesContextBeforeReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	originalBootID := readLinuxBootID
	originalNamespace := readLinuxPIDNamespaceInfo
	originalPriority := readLinuxProcessGroupPriority
	t.Cleanup(func() {
		readLinuxBootID = originalBootID
		readLinuxPIDNamespaceInfo = originalNamespace
		readLinuxProcessGroupPriority = originalPriority
	})
	readLinuxBootID = func() (string, error) {
		t.Fatal("canceled proof read boot ID")
		return "", nil
	}
	readLinuxPIDNamespaceInfo = func(int) (linuxPIDNamespaceInfo, error) {
		t.Fatal("canceled proof read namespace")
		return linuxPIDNamespaceInfo{}, nil
	}
	readLinuxProcessGroupPriority = func(int, int) (int, error) {
		t.Fatal("canceled proof called getpriority")
		return 0, unix.ESRCH
	}

	err := ProvePersistedProcessGroupAbsent(ctx, 12345, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:12345:456")
	if !errors.Is(err, ErrPersistedProcessGroupUnproven) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled proof error = %v, want unproven + context.Canceled", err)
	}
}

func TestProvePersistedProcessGroupAbsentAcceptsOnlyESRCH(t *testing.T) {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}
	originalPriority := readLinuxProcessGroupPriority
	t.Cleanup(func() { readLinuxProcessGroupPriority = originalPriority })

	t.Run("ESRCH proves absence", func(t *testing.T) {
		calls := 0
		readLinuxProcessGroupPriority = func(which, who int) (int, error) {
			calls++
			if which != unix.PRIO_PGRP || who != os.Getpid() {
				t.Fatalf("getpriority arguments = (%d, %d)", which, who)
			}
			return 0, unix.ESRCH
		}
		if err := ProvePersistedProcessGroupAbsent(context.Background(), os.Getpid(), identity); err != nil {
			t.Fatalf("ESRCH proof error = %v", err)
		}
		if calls != 1 {
			t.Fatalf("getpriority calls = %d, want 1", calls)
		}
	})

	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "group exists", err: nil},
		{name: "permission failure", err: unix.EPERM},
	} {
		t.Run(test.name, func(t *testing.T) {
			readLinuxProcessGroupPriority = func(int, int) (int, error) { return 7, test.err }
			proofErr := ProvePersistedProcessGroupAbsent(context.Background(), os.Getpid(), identity)
			if !errors.Is(proofErr, ErrPersistedProcessGroupUnproven) {
				t.Fatalf("proof error = %v, want unproven category", proofErr)
			}
		})
	}
}

func TestProvePersistedProcessGroupAbsentRejectsIdentityMismatchBeforeGetpriority(t *testing.T) {
	identity, err := ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}
	parts := strings.Split(identity, ":")
	parts[4] = strconv.FormatInt(int64(os.Getpid()+1), 10)
	originalPriority := readLinuxProcessGroupPriority
	t.Cleanup(func() { readLinuxProcessGroupPriority = originalPriority })
	called := false
	readLinuxProcessGroupPriority = func(int, int) (int, error) {
		called = true
		return 0, unix.ESRCH
	}
	proofErr := ProvePersistedProcessGroupAbsent(context.Background(), os.Getpid(), strings.Join(parts, ":"))
	if !errors.Is(proofErr, ErrPersistedProcessGroupUnproven) || called {
		t.Fatalf("mismatched proof error = %v, getpriority called = %t", proofErr, called)
	}
}

func TestParseLinuxProcessIdentityRejectsLegacyAndNonCanonicalV2Shapes(t *testing.T) {
	for _, value := range []string{
		"linux:v2:01234567-89ab-cdef-0123-456789abcdef:pid:[7]:123:42",
		"linux:v2:01234567-89ab-cdef-0123-456789abcdef:7:123:042",
		"linux:v2:01234567-89ab-cdef-0123-456789abcdef:7:123:0",
		"linux:v2:01234567-89ab-cdef-0123-456789abcdef:7:123",
	} {
		if _, err := parseLinuxProcessIdentity(value); err == nil {
			t.Fatalf("parseLinuxProcessIdentity(%q) = nil error, want rejection", value)
		}
	}
}

func TestProcessIdentityStartTimeParserCanonicalizesOnlyPositiveDecimal(t *testing.T) {
	valid := syntheticLinuxProcessStatLine(123, "name", 'S', 1, 2, 99)
	start, err := linuxProcessStartTime(valid)
	if err != nil || start != "99" {
		t.Fatalf("linuxProcessStartTime() = (%q, %v), want (99, nil)", start, err)
	}
	for _, value := range []string{
		"123 (name) S",
		"123 (name) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 0",
	} {
		if _, err := linuxProcessStartTime(value); err == nil {
			t.Fatalf("linuxProcessStartTime(%q) = nil error, want rejection", value)
		}
	}
}
