//go:build linux

package execution

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

func TestStartExposesLinuxProcessCreationIdentity(t *testing.T) {
	process := startHelper(t, &recordingSink{}, "wait")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = process.Terminate(ctx, 0)
	})

	pid, identity := process.ProcessDetails()
	if pid != process.PID {
		t.Fatalf("PID = %d, want %d", pid, process.PID)
	}
	parts := strings.Split(identity, ":")
	if len(parts) != 6 || parts[0] != "linux" || parts[1] != "v2" {
		t.Fatalf("Identity = %q, want linux:v2:boot-id:pidns:pid:starttime", identity)
	}
	bootIDBytes, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatalf("read boot ID: %v", err)
	}
	bootID := strings.TrimSpace(string(bootIDBytes))
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(bootID) {
		t.Fatalf("boot ID = %q, want canonical lowercase UUID", bootID)
	}
	if parts[2] != bootID {
		t.Fatalf("Identity boot ID = %q, want canonical boot ID %q", parts[2], bootID)
	}

	actualPIDNamespace, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", pid))
	if err != nil {
		t.Fatalf("read PID namespace: %v", err)
	}
	if !strings.HasPrefix(actualPIDNamespace, "pid:[") || !strings.HasSuffix(actualPIDNamespace, "]") {
		t.Fatalf("PID namespace = %q, want pid:[positive-inode]", actualPIDNamespace)
	}
	pidNamespace := strings.TrimSuffix(strings.TrimPrefix(actualPIDNamespace, "pid:["), "]")
	parsedPIDNamespace, err := strconv.ParseUint(pidNamespace, 10, 64)
	if err != nil || parsedPIDNamespace == 0 || parts[3] != pidNamespace {
		t.Fatalf("Identity PID namespace = %q, want positive inode %q", parts[3], pidNamespace)
	}

	parsedPID, err := strconv.ParseUint(parts[4], 10, 64)
	if err != nil || parsedPID == 0 || parsedPID != uint64(pid) {
		t.Fatalf("Identity = %q, want positive PID %d", identity, pid)
	}
	startTime, err := strconv.ParseUint(parts[5], 10, 64)
	if err != nil || startTime == 0 {
		t.Fatalf("Identity = %q, want positive process starttime", identity)
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		t.Fatalf("read process stat: %v", err)
	}
	closingParenthesis := strings.LastIndex(string(stat), ")")
	if closingParenthesis < 0 {
		t.Fatalf("process stat = %q, want a closing comm delimiter", string(stat))
	}
	statFields := strings.Fields(string(stat)[closingParenthesis+1:])
	if len(statFields) <= 19 {
		t.Fatalf("process stat has %d fields after comm, want at least 20", len(statFields))
	}
	if statFields[19] != parts[5] {
		t.Fatalf("Identity starttime = %q, want %q", parts[5], statFields[19])
	}
	if identity != fmt.Sprintf("linux:v2:%s:%d:%d:%d", bootID, parsedPIDNamespace, parsedPID, startTime) {
		t.Fatalf("Identity = %q, want canonical v2 shape", identity)
	}
	current, err := platform.ProcessIdentity(pid)
	if err != nil {
		t.Fatalf("ProcessIdentity() error = %v", err)
	}
	if identity != current {
		t.Fatalf("Identity = %q, current identity = %q", identity, current)
	}
}
